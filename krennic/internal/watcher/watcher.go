// Package watcher is the edit-time event plane: a native filesystem watcher
// (fsnotify) that maps each file change to its repo root and invokes a callback.
// This is the layer a git pre-push hook CANNOT provide — it fires on every save.
package watcher

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
)

// ignoredDirs are never watched or descended (watcher-storm control).
var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, "target": true, ".venv": true, "venv": true,
	"__pycache__": true, ".idea": true, ".vscode": true, ".next": true,
	".cache": true, "out": true, ".gradle": true, ".mvn": true,
}

// Watcher observes repo roots and reports the repo root of each change.
type Watcher struct {
	repos    []string
	onChange func(repoRoot string)
	fsw      *fsnotify.Watcher
	log      *slog.Logger
}

// New builds a Watcher for the given repo roots.
func New(repos []string, log *slog.Logger, onChange func(repoRoot string)) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{repos: repos, onChange: onChange, fsw: fsw, log: log}, nil
}

// Start registers watches and blocks processing events until Close.
func (w *Watcher) Start() error {
	for _, root := range w.repos {
		if err := w.addRecursive(root); err != nil {
			w.log.Warn("watch add failed", "root", root, "err", err)
		}
	}
	go w.loop()
	return nil
}

// addRecursive adds watches to root and all non-ignored subdirectories.
func (w *Watcher) addRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if !d.IsDir() {
			return nil
		}
		if ignoredDirs[d.Name()] && path != root {
			return filepath.SkipDir
		}
		if err := w.fsw.Add(path); err != nil {
			w.log.Debug("watch add", "path", path, "err", err)
		}
		return nil
	})
}

func (w *Watcher) loop() {
	for {
		select {
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handle(ev)
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			w.log.Warn("watcher error", "err", err)
		}
	}
}

func (w *Watcher) handle(ev fsnotify.Event) {
	if w.isIgnored(ev.Name) {
		return
	}
	// A newly created directory must be watched too (fsnotify is non-recursive).
	if ev.Op&fsnotify.Create != 0 {
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
			_ = w.addRecursive(ev.Name)
		}
	}
	if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
		return
	}
	if root := w.repoRootFor(ev.Name); root != "" {
		w.onChange(root)
	}
}

// isIgnored returns true when any path component is an ignored dir.
func (w *Watcher) isIgnored(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if ignoredDirs[part] {
			return true
		}
	}
	return false
}

// repoRootFor returns the configured repo root that contains path.
func (w *Watcher) repoRootFor(path string) string {
	best := ""
	for _, root := range w.repos {
		if path == root || strings.HasPrefix(path, root+string(os.PathSeparator)) {
			if len(root) > len(best) {
				best = root
			}
		}
	}
	return best
}

// Close stops the watcher.
func (w *Watcher) Close() error { return w.fsw.Close() }

// DiscoverRepos walks watch roots and returns directories that are git repos
// (contain a .git entry). Discovered repos are not descended into further.
func DiscoverRepos(roots []string) []string {
	var found []string
	seen := map[string]bool{}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if ignoredDirs[d.Name()] && path != root {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
				if !seen[path] {
					found = append(found, path)
					seen[path] = true
				}
				return filepath.SkipDir // don't descend into a repo
			}
			return nil
		})
	}
	return found
}
