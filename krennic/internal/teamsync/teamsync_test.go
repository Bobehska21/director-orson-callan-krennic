package teamsync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acme/krennic/internal/config"
)

func TestSyncFastForwardsCleanMain(t *testing.T) {
	_, repo := setupRepoWithOrigin(t)
	cfg := config.Default().TeamSync
	cfg.Enabled = true
	m := New(cfg, []string{repo}, "")

	writeFile(t, repo, "README.md", "remote\n")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-m", "remote")
	git(t, repo, "push", "origin", "main")
	git(t, repo, "reset", "--hard", "HEAD~1")

	st, err := m.Sync(repo)
	if err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}
	if st.UpdatePending {
		t.Fatalf("expected no pending update after sync: %+v", st)
	}
	if got := readFile(t, repo, "README.md"); got != "remote\n" {
		t.Fatalf("expected fast-forwarded file, got %q", got)
	}
}

func TestSyncRefusesDirtyTree(t *testing.T) {
	_, repo := setupRepoWithOrigin(t)
	cfg := config.Default().TeamSync
	cfg.Enabled = true
	m := New(cfg, []string{repo}, "")

	writeFile(t, repo, "dirty.txt", "dirty\n")
	if _, err := m.Sync(repo); err == nil {
		t.Fatal("expected dirty tree sync to fail")
	}
}

func TestStatusDoesNotFlagBranchAheadAsPending(t *testing.T) {
	_, repo := setupRepoWithOrigin(t)
	cfg := config.Default().TeamSync
	cfg.Enabled = true
	m := New(cfg, []string{repo}, "")

	git(t, repo, "switch", "-c", "feature")
	writeFile(t, repo, "feature.txt", "local\n")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-m", "feature")

	st := m.Status(context.Background(), repo)
	if st.Error != "" {
		t.Fatalf("Status returned error: %s", st.Error)
	}
	if st.UpdatePending {
		t.Fatalf("expected branch ahead of origin/main not to be pending: %+v", st)
	}
}

func TestDoneRefusesBranchBehindMain(t *testing.T) {
	_, repo := setupRepoWithOrigin(t)
	cfg := config.Default().TeamSync
	cfg.Enabled = true
	m := New(cfg, []string{repo}, "")

	writeFile(t, repo, "remote.txt", "remote\n")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-m", "remote")
	git(t, repo, "push", "origin", "main")
	git(t, repo, "reset", "--hard", "HEAD~1")

	writeFile(t, repo, "local.txt", "local\n")
	if _, err := m.Done(context.Background(), repo, "done"); err == nil {
		t.Fatal("expected done to fail when branch is behind origin/main")
	}
	if got := git(t, repo, "branch", "--show-current"); got != "main" {
		t.Fatalf("expected branch to remain main, got %q", got)
	}
}

func setupRepoWithOrigin(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	repo := filepath.Join(root, "repo")
	git(t, root, "init", "--bare", origin)
	git(t, root, "clone", origin, repo)
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "Test")
	git(t, repo, "config", "commit.gpgsign", "false")
	git(t, repo, "switch", "-c", "main")
	writeFile(t, repo, "README.md", "initial\n")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-m", "initial")
	git(t, repo, "push", "-u", "origin", "main")
	return origin, repo
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runGit(dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
