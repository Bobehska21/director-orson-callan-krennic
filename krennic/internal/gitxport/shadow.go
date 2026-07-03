package gitxport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SnapshotResult reports the outcome of building a shadow snapshot.
type SnapshotResult struct {
	SHA  string // snapshot commit sha; "" when nothing changed vs HEAD
	NoOp bool   // true when working tree matched HEAD (nothing to snapshot)
}

// DenyFunc is a predicate: true means the repo-relative path must be excluded.
type DenyFunc func(relPath string) bool

// CreateShadowSnapshot builds a commit representing the full working tree
// (tracked modifications AND untracked files) using an ISOLATED temp index, so
// the developer's real index, HEAD, working tree, and stash reflog are never
// touched. Files for which deny(path) is true are excluded from the snapshot.
//
// Returns SHA=="" with NoOp=true when the working tree matches HEAD exactly.
func (g *Git) CreateShadowSnapshot(msg string, deny DenyFunc) (SnapshotResult, error) {
	tmpDir, err := os.MkdirTemp("", "krennic-idx-")
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("temp index dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// A non-existent index path makes git start from an EMPTY index — every
	// working-tree file then appears as an add, so write-tree yields the full
	// working-tree state. Crucially this never mutates the user's $GIT_DIR/index.
	idxPath := filepath.Join(tmpDir, "index")
	env := []string{"GIT_INDEX_FILE=" + idxPath}

	if _, err := g.runEnv(env, "add", "-A"); err != nil {
		return SnapshotResult{}, fmt.Errorf("stage into temp index: %w", err)
	}

	// Remove denied files from the temp index so secrets never enter the tree.
	if deny != nil {
		staged, err := g.runEnv(env, "ls-files", "--cached")
		if err != nil {
			return SnapshotResult{}, fmt.Errorf("list temp index: %w", err)
		}
		for _, path := range splitLines(staged) {
			if deny(path) {
				if _, err := g.runEnv(env, "rm", "--cached", "--quiet", "--", path); err != nil {
					return SnapshotResult{}, fmt.Errorf("exclude denied path %q: %w", path, err)
				}
			}
		}
	}

	tree, err := g.runEnv(env, "write-tree")
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("write-tree: %w", err)
	}

	// If the resulting tree matches HEAD's tree, there is nothing to snapshot.
	if headTree, err := g.run("rev-parse", "HEAD^{tree}"); err == nil && headTree == tree {
		return SnapshotResult{NoOp: true}, nil
	}

	// commit-tree runs in the normal env; it needs no index.
	var snap string
	if parent := g.HeadSHA(); parent != "" {
		snap, err = g.run("commit-tree", tree, "-p", parent, "-m", msg)
	} else {
		snap, err = g.run("commit-tree", tree, "-m", msg) // unborn branch
	}
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("commit-tree: %w", err)
	}
	return SnapshotResult{SHA: snap}, nil
}

// ShadowRefName builds the shadow ref: <namespace>/<user>/<repo>/<branch>.
func ShadowRefName(namespace, user, repo, branch string) string {
	b := strings.ReplaceAll(branch, "/", "-")
	return fmt.Sprintf("%s/%s/%s/%s", strings.TrimRight(namespace, "/"), user, repo, b)
}

// EnsureRemote makes sure a remote named `name` points at url.
func (g *Git) EnsureRemote(name, url string) error {
	if url == "" {
		return fmt.Errorf("empty remote url for %q", name)
	}
	if cur := g.RemoteURL(name); cur == "" {
		_, err := g.run("remote", "add", name, url)
		return err
	} else if cur != url {
		_, err := g.run("remote", "set-url", name, url)
		return err
	}
	return nil
}

// PublishShadow points the shadow ref at sha and force-pushes it to remoteName.
// The push uses the shadow SSH identity when SSHKey is set, so it never collides
// with the developer's personal git credentials.
func (g *Git) PublishShadow(remoteName, shadowRef, sha string) error {
	if _, err := g.run("update-ref", shadowRef, sha); err != nil {
		return fmt.Errorf("update-ref %s: %w", shadowRef, err)
	}
	var env []string
	if g.SSHKey != "" {
		env = []string{fmt.Sprintf("GIT_SSH_COMMAND=ssh -i %q -o IdentitiesOnly=yes", g.SSHKey)}
	}
	refspec := shadowRef + ":" + shadowRef
	if _, err := g.runEnv(env, "push", "--force", remoteName, refspec); err != nil {
		return fmt.Errorf("push shadow ref: %w", err)
	}
	return nil
}

// ListShadowRefs returns local shadow refs under the namespace.
func (g *Git) ListShadowRefs(namespace string) ([]string, error) {
	out, err := g.run("for-each-ref", "--format=%(refname)", namespace)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// DeleteShadowRef removes a shadow ref locally and on the remote.
func (g *Git) DeleteShadowRef(remoteName, ref string) error {
	_, _ = g.run("update-ref", "-d", ref)
	var env []string
	if g.SSHKey != "" {
		env = []string{fmt.Sprintf("GIT_SSH_COMMAND=ssh -i %q -o IdentitiesOnly=yes", g.SSHKey)}
	}
	_, err := g.runEnv(env, "push", remoteName, ":"+ref)
	return err
}

// BranchExists reports whether a local branch still exists.
func (g *Git) BranchExists(branch string) bool {
	_, err := g.run("rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// LocalBranches returns all local branch names.
func (g *Git) LocalBranches() []string {
	out, err := g.run("for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil
	}
	return splitLines(out)
}

func splitLines(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
