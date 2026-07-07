// Package gitxport handles all Git interaction: reading repo state, extracting
// the working-tree diff, and publishing a shadow snapshot ref WITHOUT ever
// touching the developer's branch, index, working tree, or stash.
package gitxport

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Git wraps operations against a single repository root.
type Git struct {
	Root string
	// SSHKey, when set, is used via GIT_SSH_COMMAND for push (shadow identity).
	SSHKey string
}

// New returns a Git for repoRoot.
func New(repoRoot string) *Git { return &Git{Root: repoRoot} }

// run executes `git -C <root> <args...>` and returns trimmed stdout.
func (g *Git) run(args ...string) (string, error) {
	return g.runEnv(nil, args...)
}

func (g *Git) runEnv(env []string, args ...string) (string, error) {
	full := append([]string{"-C", g.Root}, args...)
	cmd := exec.Command("git", full...)
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// IsRepo reports whether Root is inside a git work tree.
func (g *Git) IsRepo() bool {
	out, err := g.run("rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

// TopLevel returns the absolute repo root.
func (g *Git) TopLevel() (string, error) { return g.run("rev-parse", "--show-toplevel") }

// CurrentBranch returns the branch name, or "detached" when detached.
func (g *Git) CurrentBranch() string {
	out, err := g.run("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || out == "" {
		return "detached"
	}
	return out
}

// HeadSHA returns the current HEAD commit, or "" for an unborn branch.
func (g *Git) HeadSHA() string {
	out, err := g.run("rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

// RevParse resolves a ref to a SHA.
func (g *Git) RevParse(ref string) (string, error) {
	return g.run("rev-parse", "--verify", ref)
}

// RepoName returns the basename of the repo root.
func (g *Git) RepoName() string { return filepath.Base(g.Root) }

// RemoteURL returns the URL of the named remote (usually origin).
func (g *Git) RemoteURL(remote string) string {
	out, _ := g.run("remote", "get-url", remote)
	return out
}

// GitIdentity returns the configured commit author name and email for this repo
// (the same identity that would appear on a real commit), for accurate attribution.
func (g *Git) GitIdentity() (name, email string) {
	name, _ = g.run("config", "user.name")
	email, _ = g.run("config", "user.email")
	return name, email
}

// Version returns the parsed git version, e.g. (2, 39).
func (g *Git) Version() (major, minor int) {
	out, err := g.run("version")
	if err != nil {
		return 0, 0
	}
	// "git version 2.39.5 (Apple Git-154)"
	fields := strings.Fields(out)
	if len(fields) < 3 {
		return 0, 0
	}
	parts := strings.Split(fields[2], ".")
	if len(parts) >= 2 {
		major, _ = strconv.Atoi(parts[0])
		minor, _ = strconv.Atoi(parts[1])
	}
	return major, minor
}

// IsClean reports whether the working tree and index are clean.
func (g *Git) IsClean() bool {
	out, err := g.run("status", "--porcelain")
	return err == nil && out == ""
}

// Fetch updates remote refs for a specific branch.
func (g *Git) Fetch(remote, branch string) error {
	_, err := g.run("fetch", "--quiet", remote, branch)
	return err
}

// FastForward merges the given ref into the current branch using --ff-only.
func (g *Git) FastForward(ref string) error {
	_, err := g.run("merge", "--ff-only", ref)
	return err
}

// IsAncestor reports whether ancestor is reachable from descendant.
func (g *Git) IsAncestor(ancestor, descendant string) bool {
	_, err := g.run("merge-base", "--is-ancestor", ancestor, descendant)
	return err == nil
}

// CreateBranch creates and checks out a new branch at the current HEAD.
func (g *Git) CreateBranch(name string) error {
	_, err := g.run("switch", "-c", name)
	return err
}

// AddAllCommit stages all changes and creates a commit.
func (g *Git) AddAllCommit(message string) (string, error) {
	if _, err := g.run("add", "-A"); err != nil {
		return "", err
	}
	if _, err := g.run("commit", "-m", message); err != nil {
		return "", err
	}
	return g.HeadSHA(), nil
}

// PushBranch pushes a branch to a remote, setting upstream.
func (g *Git) PushBranch(remote, branch string) error {
	_, err := g.run("push", "-u", remote, branch)
	return err
}

// ChangedPaths returns all modified, staged, and untracked repo-relative paths.
func (g *Git) ChangedPaths() []string {
	out, err := g.run("status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return nil
	}
	var paths []string
	for _, ln := range splitLines(out) {
		if len(ln) < 4 {
			continue
		}
		p := strings.TrimSpace(ln[3:])
		// Handle rename "old -> new" by taking the new path.
		if i := strings.Index(p, " -> "); i >= 0 {
			p = p[i+4:]
		}
		paths = append(paths, p)
	}
	return paths
}

// StashCount returns the number of stash entries (used by tests to prove the
// stash reflog is never mutated by the shadow snapshot).
func (g *Git) StashCount() int {
	out, err := g.run("stash", "list")
	if err != nil || out == "" {
		return 0
	}
	return len(strings.Split(out, "\n"))
}
