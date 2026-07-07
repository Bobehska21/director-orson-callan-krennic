package teamsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/acme/krennic/internal/config"
	"github.com/acme/krennic/internal/gitxport"
)

type RepoStatus struct {
	Path          string `json:"path"`
	Branch        string `json:"branch"`
	Dirty         bool   `json:"dirty"`
	LocalHead     string `json:"local_head"`
	RemoteMain    string `json:"remote_main"`
	MainBranch    string `json:"main_branch"`
	UpdatePending bool   `json:"update_pending"`
	Error         string `json:"error,omitempty"`
}

type DoneResult struct {
	RepoPath string
	Branch   string
	Commit   string
	PRURL    string
	LogPath  string
}

type Manager struct {
	cfg    config.TeamSyncConfig
	repos  []string
	token  string
	client *http.Client
}

func New(cfg config.TeamSyncConfig, repos []string, token string) *Manager {
	return &Manager{cfg: cfg, repos: repos, token: token, client: &http.Client{Timeout: 30 * time.Second}}
}

func (m *Manager) Enabled() bool { return m != nil && m.cfg.Enabled }

func (m *Manager) Interval() time.Duration {
	if m.cfg.FetchIntervalMS <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(m.cfg.FetchIntervalMS) * time.Millisecond
}

func (m *Manager) StatusAll(ctx context.Context) []RepoStatus {
	if m == nil {
		return nil
	}
	out := make([]RepoStatus, 0, len(m.repos))
	for _, repo := range m.repos {
		out = append(out, m.Status(ctx, repo))
	}
	return out
}

func (m *Manager) Status(ctx context.Context, repo string) RepoStatus {
	g := gitxport.New(repo)
	st := RepoStatus{
		Path:       repo,
		Branch:     g.CurrentBranch(),
		Dirty:      !g.IsClean(),
		LocalHead:  g.HeadSHA(),
		MainBranch: m.cfg.MainBranch,
	}
	if err := g.Fetch("origin", m.cfg.MainBranch); err != nil {
		st.Error = err.Error()
		return st
	}
	remote, err := g.RevParse("refs/remotes/origin/" + m.cfg.MainBranch)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.RemoteMain = remote
	st.UpdatePending = !g.IsAncestor("refs/remotes/origin/"+m.cfg.MainBranch, "HEAD")
	_ = ctx
	return st
}

func (m *Manager) Sync(repo string) (RepoStatus, error) {
	st := m.Status(context.Background(), repo)
	if st.Error != "" {
		return st, errors.New(st.Error)
	}
	if st.Dirty {
		return st, fmt.Errorf("working tree has local changes; finish or stash them before sync")
	}
	if st.Branch != m.cfg.MainBranch {
		return st, fmt.Errorf("sync only fast-forwards %s when it is the current branch; current branch is %s", m.cfg.MainBranch, st.Branch)
	}
	if !st.UpdatePending {
		return st, nil
	}
	if err := gitxport.New(repo).FastForward("origin/" + m.cfg.MainBranch); err != nil {
		return st, err
	}
	return m.Status(context.Background(), repo), nil
}

func (m *Manager) Done(ctx context.Context, repo, message string) (DoneResult, error) {
	g := gitxport.New(repo)
	if g.CurrentBranch() == "detached" {
		return DoneResult{}, fmt.Errorf("cannot run done on detached HEAD")
	}
	if g.IsClean() {
		return DoneResult{}, fmt.Errorf("working tree is clean; nothing to finish")
	}
	if err := g.Fetch("origin", m.cfg.MainBranch); err != nil {
		return DoneResult{}, err
	}
	mainRef := "origin/" + m.cfg.MainBranch
	if !g.IsAncestor(mainRef, "HEAD") {
		return DoneResult{}, fmt.Errorf("current branch is not based on latest %s; finish or save work, sync/rebase, then retry", mainRef)
	}
	branch := m.uniqueBranchName(repo)
	if err := g.CreateBranch(branch); err != nil {
		return DoneResult{}, err
	}
	if message == "" {
		message = "krennic done"
	}
	commit, err := g.AddAllCommit(message)
	if err != nil {
		return DoneResult{}, err
	}
	logPath, err := Validate(repo)
	if err != nil {
		return DoneResult{RepoPath: repo, Branch: branch, Commit: commit, LogPath: logPath}, err
	}
	if err := g.PushBranch("origin", branch); err != nil {
		return DoneResult{RepoPath: repo, Branch: branch, Commit: commit, LogPath: logPath}, err
	}
	prURL, err := m.createPR(ctx, repo, branch, message)
	if err != nil {
		return DoneResult{RepoPath: repo, Branch: branch, Commit: commit, LogPath: logPath}, err
	}
	return DoneResult{RepoPath: repo, Branch: branch, Commit: commit, PRURL: prURL, LogPath: logPath}, nil
}

func (m *Manager) uniqueBranchName(repo string) string {
	base := strings.Trim(strings.TrimRight(m.cfg.BranchPrefix, "/"), "/")
	if base == "" {
		base = "krennic/done"
	}
	slug := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-").Replace(filepath.Base(repo))
	return fmt.Sprintf("%s/%s-%s", base, slug, time.Now().UTC().Format("20060102-150405"))
}

func Validate(repo string) (string, error) {
	f, err := os.CreateTemp("", "krennic-validate-*.log")
	if err != nil {
		return "", err
	}
	defer f.Close()
	err = validateDir(repo, f)
	return f.Name(), err
}

func validateDir(dir string, log *os.File) error {
	if hasFile(dir, "Makefile") || hasFile(dir, "makefile") {
		targets := makeTargets(dir)
		for _, target := range []string{"test", "vet", "build"} {
			if targets[target] {
				if err := runLogged(log, dir, "make", target); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if hasFile(dir, "package.json") && look("npm") && look("node") {
		scripts := npmScripts(dir)
		for _, script := range []string{"test", "lint", "build"} {
			if scripts[script] {
				if err := runLogged(log, dir, "npm", "run", script); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if hasFile(dir, "go.mod") && look("go") {
		for _, args := range [][]string{{"test", "./..."}, {"vet", "./..."}, {"build", "./..."}} {
			if err := runLogged(log, dir, "go", args...); err != nil {
				return err
			}
		}
		return nil
	}
	if hasFile(dir, "Cargo.toml") && look("cargo") {
		for _, args := range [][]string{{"test"}, {"build"}} {
			if err := runLogged(log, dir, "cargo", args...); err != nil {
				return err
			}
		}
		return nil
	}
	if (hasFile(dir, "pyproject.toml") || hasFile(dir, "pytest.ini") || hasDir(dir, "tests")) && look("pytest") {
		return runLogged(log, dir, "pytest")
	}
	return nil
}

func runLogged(log *os.File, dir string, name string, args ...string) error {
	fmt.Fprintf(log, "== %s %s ==\n", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s failed; see %s", name, strings.Join(args, " "), log.Name())
	}
	return nil
}

func makeTargets(dir string) map[string]bool {
	cmd := exec.Command("make", "-qp")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	targets := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.Index(line, ":"); i > 0 && !strings.ContainsAny(line[:i], " \t") {
			targets[line[:i]] = true
		}
	}
	return targets
}

func npmScripts(dir string) map[string]bool {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil
	}
	var p struct {
		Scripts map[string]string `json:"scripts"`
	}
	_ = json.Unmarshal(data, &p)
	out := map[string]bool{}
	for k := range p.Scripts {
		out[k] = true
	}
	return out
}

func (m *Manager) createPR(ctx context.Context, repoRoot, branch, title string) (string, error) {
	if m.token == "" {
		return "", fmt.Errorf("team sync token is not configured")
	}
	owner, repo, err := parseGitHub(gitxport.New(repoRoot).RemoteURL("origin"))
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(map[string]any{
		"title": title,
		"head":  branch,
		"base":  m.cfg.MainBranch,
		"body":  "Created by Krennic team sync.",
	})
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls", owner, repo)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	m.githubHeaders(req)
	resp, err := m.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("github create PR failed: %d", resp.StatusCode)
	}
	var pr struct {
		URL     string `json:"html_url"`
		NodeID  string `json:"node_id"`
		Number  int    `json:"number"`
		HeadRef string `json:"head"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return "", err
	}
	if m.cfg.AutoMerge && pr.NodeID != "" {
		if err := m.enableAutoMerge(ctx, pr.NodeID); err != nil {
			return pr.URL, fmt.Errorf("created PR %s but auto-merge failed: %w", pr.URL, err)
		}
	}
	return pr.URL, nil
}

func (m *Manager) enableAutoMerge(ctx context.Context, nodeID string) error {
	body, _ := json.Marshal(map[string]any{
		"query":     `mutation($id:ID!){enablePullRequestAutoMerge(input:{pullRequestId:$id,mergeMethod:SQUASH}){pullRequest{id}}}`,
		"variables": map[string]string{"id": nodeID},
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/graphql", bytes.NewReader(body))
	m.githubHeaders(req)
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("github auto-merge failed: %d", resp.StatusCode)
	}
	var out struct {
		Errors []any `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if len(out.Errors) > 0 {
		return fmt.Errorf("github auto-merge returned errors")
	}
	return nil
}

func (m *Manager) githubHeaders(req *http.Request) {
	req.Header.Set("authorization", "Bearer "+m.token)
	req.Header.Set("accept", "application/vnd.github+json")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-github-api-version", "2022-11-28")
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
	if strings.HasPrefix(remote, "git@") {
		if i := strings.Index(remote, ":"); i >= 0 {
			return strings.TrimSuffix(remote[i+1:], ".git"), nil
		}
	}
	if u, err := url.Parse(remote); err == nil && u.Path != "" {
		return strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git"), nil
	}
	return "", fmt.Errorf("cannot parse remote %q", remote)
}

func hasFile(dir, name string) bool {
	st, err := os.Stat(filepath.Join(dir, name))
	return err == nil && !st.IsDir()
}

func hasDir(dir, name string) bool {
	st, err := os.Stat(filepath.Join(dir, name))
	return err == nil && st.IsDir()
}

func look(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func PickRepo(repos []string, requested string) (string, error) {
	if requested != "" {
		abs, err := filepath.Abs(requested)
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	if len(repos) == 1 {
		return repos[0], nil
	}
	cwd, _ := os.Getwd()
	candidates := append([]string(nil), repos...)
	sort.Slice(candidates, func(i, j int) bool { return len(candidates[i]) > len(candidates[j]) })
	for _, repo := range candidates {
		if cwd == repo || strings.HasPrefix(cwd, repo+string(filepath.Separator)) {
			return repo, nil
		}
	}
	return "", fmt.Errorf("multiple repositories are configured; pass --repo")
}
