// Package status publishes AI verdicts as commit statuses on GitHub/GitLab.
// It uses a dedicated status-publish identity (repo:status scope) and is
// non-blocking by default — a failing verdict only blocks a PR if the team
// explicitly makes the check required server-side.
package status

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/acme/krennic/internal/model"
)

// Publisher posts a status for a change's analysis result.
type Publisher interface {
	Publish(ctx context.Context, ev model.ChangeEvent, tr *model.TriageResult, rv *model.ReviewResult) error
}

// New returns a Publisher for the provider ("github"|"gitlab").
func New(provider, token string) (Publisher, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	switch provider {
	case "github":
		return &githubPublisher{token: token, client: client, base: "https://api.github.com"}, nil
	case "gitlab":
		return &gitlabPublisher{token: token, client: client, base: "https://gitlab.com/api/v4"}, nil
	default:
		return nil, fmt.Errorf("unknown status provider %q", provider)
	}
}

// verdict maps analysis results into a coarse state + human description.
func verdict(tr *model.TriageResult, rv *model.ReviewResult) (state, desc string) {
	if rv != nil {
		switch rv.Verdict {
		case "request-changes":
			return "failure", trunc(rv.Summary, 140)
		default:
			return "success", trunc(rv.Summary, 140)
		}
	}
	if tr != nil {
		return "success", trunc(fmt.Sprintf("triage: %s — %s", tr.Relevance, tr.Reason), 140)
	}
	return "pending", "krennic analyzing"
}

func trunc(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// --- GitHub ---

type githubPublisher struct {
	token  string
	client *http.Client
	base   string
}

func (p *githubPublisher) Publish(ctx context.Context, ev model.ChangeEvent, tr *model.TriageResult, rv *model.ReviewResult) error {
	owner, repo, err := parseGitHub(ev.Repo.Remote)
	if err != nil {
		return err
	}
	sha := shaFor(ev)
	state, desc := verdict(tr, rv)
	body, _ := json.Marshal(map[string]string{
		"state": state, "description": desc, "context": "krennic/ai-review",
	})
	endpoint := fmt.Sprintf("%s/repos/%s/%s/statuses/%s", p.base, owner, repo, sha)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	req.Header.Set("authorization", "Bearer "+p.token)
	req.Header.Set("accept", "application/vnd.github+json")
	req.Header.Set("content-type", "application/json")
	return do(p.client, req, "github")
}

// --- GitLab ---

type gitlabPublisher struct {
	token  string
	client *http.Client
	base   string
}

func (p *gitlabPublisher) Publish(ctx context.Context, ev model.ChangeEvent, tr *model.TriageResult, rv *model.ReviewResult) error {
	project, err := parseGitLab(ev.Repo.Remote)
	if err != nil {
		return err
	}
	sha := shaFor(ev)
	state, desc := verdict(tr, rv)
	if state == "failure" {
		state = "failed" // GitLab uses "failed"
	}
	body, _ := json.Marshal(map[string]string{
		"state": state, "description": desc, "name": "krennic/ai-review",
	})
	endpoint := fmt.Sprintf("%s/projects/%s/statuses/%s", p.base, url.PathEscape(project), sha)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	req.Header.Set("private-token", p.token)
	req.Header.Set("content-type", "application/json")
	return do(p.client, req, "gitlab")
}

func shaFor(ev model.ChangeEvent) string {
	if ev.Repo.HeadSHA != "" {
		return ev.Repo.HeadSHA
	}
	return ev.Repo.ShadowSHA
}

func do(c *http.Client, req *http.Request, who string) error {
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s status publish failed: %d", who, resp.StatusCode)
	}
	return nil
}

// parseGitHub extracts owner/repo from an SSH or HTTPS remote URL.
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

// parseGitLab extracts the full project path (may be nested groups).
func parseGitLab(remote string) (string, error) {
	path, err := remotePath(remote)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(path, ".git"), nil
}

// remotePath normalizes git@host:path and https://host/path to "path".
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
