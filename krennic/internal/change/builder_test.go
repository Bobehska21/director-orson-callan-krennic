package change

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acme/krennic/internal/model"
	"github.com/acme/krennic/internal/redact"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestBuildRedactsSecretsAndProducesEvent verifies the ChangeEvent excludes
// denied paths from the diff, reports them in redacted_paths, and captures
// tracked + untracked changes.
func TestBuildRedactsSecretsAndProducesEvent(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	writeFile(t, dir, "app.go", "package app\nfunc A(){}\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "init")

	// Dirty tree: modify tracked, add untracked, add a secret file.
	writeFile(t, dir, "app.go", "package app\nfunc A(){ println(1) }\n")
	writeFile(t, dir, "util.go", "package app\nfunc B(){}\n")
	writeFile(t, dir, ".env", "API_KEY=supersecret\n")

	dev := model.Developer{UserSlug: "tester", GitName: "Test", Machine: "host"}
	r := redact.New([]string{".env*", "*.pem"}, true)
	b := New(dev, r, "refs/ai", "")

	ev, ok, err := b.Build(dir)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !ok {
		t.Fatal("expected an analyzable event")
	}

	// Redacted path reported.
	if !contains(ev.Summary.RedactedPaths, ".env") {
		t.Errorf("expected .env in redacted_paths, got %v", ev.Summary.RedactedPaths)
	}
	// No hunk may reference the secret file or its content.
	for _, h := range ev.Diff.Hunks {
		if h.Path == ".env" {
			t.Errorf("secret file leaked into diff hunks")
		}
		if strings.Contains(h.Patch, "supersecret") {
			t.Errorf("secret value leaked into diff patch")
		}
	}
	// Both the tracked change and the untracked file are present.
	paths := map[string]bool{}
	for _, h := range ev.Diff.Hunks {
		paths[h.Path] = true
	}
	if !paths["app.go"] || !paths["util.go"] {
		t.Errorf("expected app.go and util.go in hunks, got %v", paths)
	}
	// Shadow ref is namespaced and a snapshot sha exists.
	if !strings.HasPrefix(ev.Repo.ShadowRef, "refs/ai/tester/") {
		t.Errorf("unexpected shadow ref %q", ev.Repo.ShadowRef)
	}
	if ev.Repo.ShadowSHA == "" {
		t.Error("expected a shadow snapshot sha")
	}
	if ev.ContentHash == "" {
		t.Error("expected a content hash for dedup")
	}
}

func TestBuildCommitTargetsHeadSHA(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	writeFile(t, dir, "app.go", "package app\nfunc A(){}\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "init")

	writeFile(t, dir, "app.go", "package app\nfunc A(){ println(1) }\n")
	writeFile(t, dir, ".env", "API_KEY=supersecret\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "change")

	head := gitOutput(t, dir, "rev-parse", "HEAD")
	dev := model.Developer{UserSlug: "tester", GitName: "Test", Machine: "host"}
	r := redact.New([]string{".env*", "*.pem"}, true)
	b := New(dev, r, "refs/ai", "")

	ev, ok, err := b.BuildCommit(dir, head)
	if err != nil {
		t.Fatalf("build commit: %v", err)
	}
	if !ok {
		t.Fatal("expected an analyzable commit event")
	}
	if ev.Repo.HeadSHA != head {
		t.Fatalf("HeadSHA = %q, want %q", ev.Repo.HeadSHA, head)
	}
	if ev.Repo.ShadowSHA != head {
		t.Fatalf("ShadowSHA = %q, want %q", ev.Repo.ShadowSHA, head)
	}
	if !contains(ev.Summary.RedactedPaths, ".env") {
		t.Errorf("expected .env in redacted_paths, got %v", ev.Summary.RedactedPaths)
	}
	for _, h := range ev.Diff.Hunks {
		if h.Path == ".env" || strings.Contains(h.Patch, "supersecret") {
			t.Fatalf("secret leaked into commit event: %#v", h)
		}
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
