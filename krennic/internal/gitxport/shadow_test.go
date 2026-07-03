package gitxport

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newTestRepo creates a git repo with one committed file and returns its path.
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	write(t, dir, "main.go", "package main\nfunc main(){}\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSnapshotNeverMutatesGitState is the load-bearing invariant: building a
// shadow snapshot must not change the working tree, index, HEAD, or stash.
func TestSnapshotNeverMutatesGitState(t *testing.T) {
	dir := newTestRepo(t)
	g := New(dir)

	// Dirty the tree: modify tracked, add untracked, add a secret.
	write(t, dir, "main.go", "package main\nfunc main(){ println(\"x\") }\n")
	write(t, dir, "new.txt", "brand new file\n")
	write(t, dir, ".env", "SECRET=topsecret\n")

	statusBefore := runGit(t, dir, "status", "--porcelain")
	headBefore := g.HeadSHA()
	stashBefore := g.StashCount()
	// capture the real index sha
	indexBefore := runGit(t, dir, "write-tree")

	deny := func(p string) bool { return p == ".env" }
	snap, err := g.CreateShadowSnapshot("test snapshot", deny)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.SHA == "" {
		t.Fatal("expected a snapshot sha")
	}

	// Nothing about the developer's state may have changed.
	if got := runGit(t, dir, "status", "--porcelain"); got != statusBefore {
		t.Errorf("status changed:\nbefore=%q\nafter =%q", statusBefore, got)
	}
	if got := g.HeadSHA(); got != headBefore {
		t.Errorf("HEAD changed: %s -> %s", headBefore, got)
	}
	if got := g.StashCount(); got != stashBefore {
		t.Errorf("stash count changed: %d -> %d", stashBefore, got)
	}
	if got := runGit(t, dir, "write-tree"); got != indexBefore {
		t.Errorf("real index changed: %s -> %s", indexBefore, got)
	}

	// The snapshot tree must contain the untracked file and the tracked change,
	// but NOT the denied secret.
	tree := runGit(t, dir, "ls-tree", "-r", "--name-only", snap.SHA)
	files := strings.Split(tree, "\n")
	assertContains(t, files, "main.go")
	assertContains(t, files, "new.txt")
	assertNotContains(t, files, ".env")
}

// TestSnapshotNoOpWhenClean returns NoOp on a clean tree.
func TestSnapshotNoOpWhenClean(t *testing.T) {
	dir := newTestRepo(t)
	g := New(dir)
	snap, err := g.CreateShadowSnapshot("noop", nil)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !snap.NoOp {
		t.Errorf("expected NoOp on clean tree, got sha=%s", snap.SHA)
	}
}

func assertContains(t *testing.T, list []string, want string) {
	t.Helper()
	for _, s := range list {
		if s == want {
			return
		}
	}
	t.Errorf("expected %q in %v", want, list)
}

func assertNotContains(t *testing.T, list []string, bad string) {
	t.Helper()
	for _, s := range list {
		if s == bad {
			t.Errorf("did not expect %q in snapshot tree %v", bad, list)
		}
	}
}
