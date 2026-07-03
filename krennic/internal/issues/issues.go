// Package issues opens tracker issues for blocking AI review findings.
package issues

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/acme/krennic/internal/model"
)

// Reporter creates issues for review results that should block a merge.
type Reporter interface {
	Report(ctx context.Context, ev model.ChangeEvent, rv *model.ReviewResult) error
}

// New returns an issue reporter for the provider. Only GitHub supports issue
// creation today because GitLab issue metadata differs enough to keep separate.
func New(provider, token string) (Reporter, error) {
	switch provider {
	case "github":
		return &githubReporter{
			token:  token,
			client: &http.Client{Timeout: 30 * time.Second},
			base:   "https://api.github.com",
		}, nil
	default:
		return nil, fmt.Errorf("unknown issues provider %q", provider)
	}
}

type githubReporter struct {
	token  string
	client *http.Client
	base   string
}

func (r *githubReporter) Report(ctx context.Context, ev model.ChangeEvent, rv *model.ReviewResult) error {
	if rv == nil || rv.Verdict != "request-changes" {
		return nil
	}
	owner, repo, err := parseGitHub(ev.Repo.Remote)
	if err != nil {
		return err
	}
	labels := labelsFor(ev)
	for _, label := range labels {
		_ = r.ensureLabel(ctx, owner, repo, label)
	}
	body, _ := json.Marshal(map[string]any{
		"title":  titleFor(ev, rv),
		"body":   bodyFor(ev, rv),
		"labels": labels,
	})
	endpoint := fmt.Sprintf("%s/repos/%s/%s/issues", r.base, owner, repo)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	req.Header.Set("authorization", "Bearer "+r.token)
	req.Header.Set("accept", "application/vnd.github+json")
	req.Header.Set("content-type", "application/json")
	return do(r.client, req, "github issue create")
}

func (r *githubReporter) ensureLabel(ctx context.Context, owner, repo, label string) error {
	payload := map[string]string{
		"name":        label,
		"color":       colorFor(label),
		"description": descriptionFor(label),
	}
	body, _ := json.Marshal(payload)
	endpoint := fmt.Sprintf("%s/repos/%s/%s/labels", r.base, owner, repo)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	req.Header.Set("authorization", "Bearer "+r.token)
	req.Header.Set("accept", "application/vnd.github+json")
	req.Header.Set("content-type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusUnprocessableEntity {
		return nil
	}
	return fmt.Errorf("github label create failed: %d", resp.StatusCode)
}

func titleFor(ev model.ChangeEvent, rv *model.ReviewResult) string {
	summary := strings.TrimSpace(rv.Summary)
	if summary == "" {
		summary = "AI review requested changes"
	}
	return trunc(fmt.Sprintf("[Krennic] %s", summary), 120)
}

func bodyFor(ev model.ChangeEvent, rv *model.ReviewResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Krennic našel blokující problém v AI review.\n\n")
	fmt.Fprintf(&b, "- Repo: `%s`\n", ev.Repo.Name)
	fmt.Fprintf(&b, "- Branch: `%s`\n", ev.Repo.Branch)
	fmt.Fprintf(&b, "- Commit: `%s`\n", ev.Repo.HeadSHA)
	fmt.Fprintf(&b, "- Change ID: `%s`\n", ev.ChangeID)
	fmt.Fprintf(&b, "- Autor: `%s <%s>`\n", ev.Developer.GitName, ev.Developer.GitEmail)
	if len(ev.Summary.Languages) > 0 {
		fmt.Fprintf(&b, "- Jazyky: `%s`\n", strings.Join(ev.Summary.Languages, "`, `"))
	}
	if len(ev.Diff.Hunks) > 0 {
		fmt.Fprintf(&b, "- Soubory: `%s`\n", strings.Join(changedPaths(ev), "`, `"))
	}
	fmt.Fprintln(&b)
	if rv.Summary != "" {
		fmt.Fprintf(&b, "## Shrnutí\n\n%s\n\n", rv.Summary)
	}
	if len(rv.Findings) > 0 {
		fmt.Fprintf(&b, "## Nálezy\n\n")
		for _, f := range rv.Findings {
			loc := f.Path
			if f.Line > 0 {
				loc = fmt.Sprintf("%s:%d", f.Path, f.Line)
			}
			fmt.Fprintf(&b, "- `%s` [%s/%s]: %s", loc, f.Severity, f.Type, f.Message)
			if f.Suggestion != "" {
				fmt.Fprintf(&b, " Návrh: %s", f.Suggestion)
			}
			fmt.Fprintln(&b)
		}
	}
	fmt.Fprintf(&b, "\n---\nVytvořeno automaticky Krennicem pro verdikt `request-changes`.\n")
	return b.String()
}

func changedPaths(ev model.ChangeEvent) []string {
	set := map[string]bool{}
	for _, h := range ev.Diff.Hunks {
		if h.Path != "" {
			set[h.Path] = true
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func labelsFor(ev model.ChangeEvent) []string {
	set := map[string]bool{"krennic": true, "bug": true}
	frontend, backend := false, false
	for _, h := range ev.Diff.Hunks {
		area := areaForPath(h.Path, h.Language)
		if area == "frontend" {
			frontend = true
		}
		if area == "backend" {
			backend = true
		}
	}
	if frontend {
		set["frontend"] = true
	}
	if backend {
		set["backend"] = true
	}
	if !frontend && !backend {
		set["area-unknown"] = true
	}
	out := make([]string, 0, len(set))
	for label := range set {
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

func areaForPath(path, lang string) string {
	p := strings.ToLower(filepath.ToSlash(path))
	ext := strings.ToLower(filepath.Ext(p))
	switch ext {
	case ".tsx", ".jsx", ".vue", ".svelte", ".css", ".scss", ".sass", ".less", ".html":
		return "frontend"
	case ".go", ".py", ".java", ".kt", ".rs", ".rb", ".php", ".cs", ".sql":
		return "backend"
	}
	for _, part := range []string{"/frontend/", "/client/", "/web/", "/ui/", "/static/", "/templates/"} {
		if strings.Contains("/"+p, part) {
			return "frontend"
		}
	}
	for _, part := range []string{"/backend/", "/server/", "/api/", "/internal/", "/cmd/", "/pkg/"} {
		if strings.Contains("/"+p, part) {
			return "backend"
		}
	}
	switch strings.ToLower(lang) {
	case "typescript", "javascript", "css", "html":
		return "frontend"
	case "go", "python", "java", "rust", "ruby", "php", "c#", "sql":
		return "backend"
	default:
		return ""
	}
}

func colorFor(label string) string {
	switch label {
	case "krennic":
		return "6f42c1"
	case "bug":
		return "d73a4a"
	case "frontend":
		return "0e8a16"
	case "backend":
		return "1d76db"
	default:
		return "ededed"
	}
}

func descriptionFor(label string) string {
	switch label {
	case "krennic":
		return "Created automatically by Krennic"
	case "bug":
		return "Blocking problem found by AI review"
	case "frontend":
		return "Frontend/UI related change"
	case "backend":
		return "Backend/server related change"
	default:
		return "Area could not be classified automatically"
	}
}

func do(c *http.Client, req *http.Request, who string) error {
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s failed: %d", who, resp.StatusCode)
	}
	return nil
}

func parseGitHub(remote string) (owner, repo string, err error) {
	path, err := remotePath(remote)
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("cannot parse github remote %q", remote)
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}

func remotePath(remote string) (string, error) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", fmt.Errorf("empty remote url")
	}
	if strings.HasPrefix(remote, "git@") || strings.HasPrefix(remote, "ssh://") {
		if i := strings.Index(remote, ":"); i >= 0 && !strings.HasPrefix(remote, "ssh://") {
			return strings.TrimSuffix(remote[i+1:], ".git"), nil
		}
		if u, err := url.Parse(remote); err == nil {
			return strings.Trim(u.Path, "/"), nil
		}
	}
	if u, err := url.Parse(remote); err == nil && u.Path != "" {
		return strings.Trim(u.Path, "/"), nil
	}
	return "", fmt.Errorf("cannot parse remote %q", remote)
}

func trunc(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
