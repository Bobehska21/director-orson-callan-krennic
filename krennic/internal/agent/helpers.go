package agent

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/acme/krennic/internal/config"
	"github.com/acme/krennic/internal/gitxport"
	"github.com/acme/krennic/internal/watcher"
)

var errNoProviders = errors.New("no AI providers configured (set anthropic/gemini keys or install claude CLI)")

// dbPath returns the SQLite file location next to the config dir.
func dbPath(cfg config.Config) string {
	dir := filepath.Dir(config.DefaultPath())
	_ = os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, "krennic.db")
}

// userSlug derives a stable per-user identifier for shadow ref namespacing.
func userSlug() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		name := u.Username
		if i := strings.LastIndexAny(name, `\/`); i >= 0 {
			name = name[i+1:] // strip DOMAIN\ on Windows
		}
		return sanitize(name)
	}
	if v := os.Getenv("USER"); v != "" {
		return sanitize(v)
	}
	return "user"
}

func sanitize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}

// resolveRepos returns the effective list of repo roots to watch: explicit
// enabled repos plus auto-discovered ones under watch_roots.
func resolveRepos(cfg config.Config) []string {
	seen := map[string]bool{}
	var repos []string
	add := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if !seen[abs] {
			seen[abs] = true
			repos = append(repos, abs)
		}
	}
	for _, r := range cfg.Repos {
		if r.Enabled {
			add(r.Path)
		}
	}
	for _, d := range watcher.DiscoverRepos(cfg.Agent.WatchRoots) {
		add(d)
	}
	return repos
}

// resolveRemotes maps each repo root to the ai-remote push URL: explicit
// per-repo override, else global git_transport.remote_url, else the repo's
// own origin URL.
func resolveRemotes(cfg config.Config, repos []string) map[string]string {
	override := map[string]string{}
	for _, r := range cfg.Repos {
		if r.RemoteURL != "" {
			if abs, err := filepath.Abs(r.Path); err == nil {
				override[abs] = r.RemoteURL
			}
		}
	}
	out := map[string]string{}
	for _, root := range repos {
		switch {
		case override[root] != "":
			out[root] = override[root]
		case cfg.Git.RemoteURL != "":
			out[root] = cfg.Git.RemoteURL
		default:
			out[root] = gitxport.New(root).RemoteURL("origin")
		}
	}
	return out
}

// osName is exposed for the doctor command / diagnostics.
func osName() string { return runtime.GOOS }

// Exported wrappers for the CLI (doctor / gc / status).

// ResolveRepos returns the effective repo roots for the config.
func ResolveRepos(cfg config.Config) []string { return resolveRepos(cfg) }

// ResolveRemotes maps repo roots to their ai-remote push URLs.
func ResolveRemotes(cfg config.Config, repos []string) map[string]string {
	return resolveRemotes(cfg, repos)
}

// DBPath returns the SQLite database path for the config.
func DBPath(cfg config.Config) string { return dbPath(cfg) }

// UserSlug returns the per-user shadow-ref identifier.
func UserSlug() string { return userSlug() }
