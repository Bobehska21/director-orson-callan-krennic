package gitxport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/acme/krennic/internal/model"
)

// DiffOptions control diff granularity for the AI event.
type DiffOptions struct {
	ContextLines     int
	InterHunkContext int
	FunctionContext  bool
	MaxUntrackedLines int // cap synthetic untracked-file hunks
}

// DefaultDiffOptions mirrors the plan's -U3 --inter-hunk-context=1 --function-context.
func DefaultDiffOptions() DiffOptions {
	return DiffOptions{ContextLines: 3, InterHunkContext: 1, FunctionContext: true, MaxUntrackedLines: 400}
}

// DiffResult is the parsed working-tree diff plus rollup metadata.
type DiffResult struct {
	Hunks        []model.Hunk
	ChangedFiles []string
	Languages    []string
	LinesAdded   int
	LinesRemoved int
	Truncated    bool
}

// WorkingTreeDiff computes the hunk-level diff of the working tree vs HEAD,
// including untracked files rendered as synthetic added hunks. Files for which
// deny(path) is true are skipped entirely; surviving lines are secret-masked
// through mask (which returns the possibly-masked line).
func (g *Git) WorkingTreeDiff(opts DiffOptions, deny DenyFunc, mask func(string) string) (DiffResult, error) {
	var res DiffResult

	args := []string{"diff", "--no-color",
		fmt.Sprintf("-U%d", opts.ContextLines),
		fmt.Sprintf("--inter-hunk-context=%d", opts.InterHunkContext),
	}
	if opts.FunctionContext {
		args = append(args, "--function-context")
	}
	args = append(args, "HEAD")

	raw, err := g.run(args...)
	if err != nil {
		// A repo with an unborn HEAD has no HEAD to diff against; fall back to
		// diffing the empty tree.
		if g.HeadSHA() == "" {
			raw, err = g.run(append(args[:len(args)-1], g.emptyTree())...)
		}
		if err != nil {
			return res, err
		}
	}

	langSet := map[string]bool{}
	fileSet := map[string]bool{}
	for _, fd := range splitFileDiffs(raw) {
		if fd.path == "" || (deny != nil && deny(fd.path)) {
			continue
		}
		lang := languageOf(fd.path)
		for _, h := range fd.hunks {
			patch := h.body
			if mask != nil {
				patch = maskPatch(patch, mask)
			}
			res.Hunks = append(res.Hunks, model.Hunk{
				Path:            fd.path,
				Language:        lang,
				ChangeType:      fd.changeType,
				FunctionContext: h.funcCtx,
				Patch:           patch,
			})
			res.LinesAdded += h.added
			res.LinesRemoved += h.removed
		}
		fileSet[fd.path] = true
		if lang != "" {
			langSet[lang] = true
		}
	}

	// Untracked files: render as synthetic new-file hunks.
	if others, err := g.run("ls-files", "--others", "--exclude-standard"); err == nil {
		for _, path := range splitLines(others) {
			if deny != nil && deny(path) {
				continue
			}
			hunk, added, truncated, ok := g.untrackedHunk(path, opts, mask)
			if !ok {
				continue
			}
			res.Hunks = append(res.Hunks, hunk)
			res.LinesAdded += added
			res.Truncated = res.Truncated || truncated
			fileSet[path] = true
			if l := languageOf(path); l != "" {
				langSet[l] = true
			}
		}
	}

	for f := range fileSet {
		res.ChangedFiles = append(res.ChangedFiles, f)
	}
	for l := range langSet {
		res.Languages = append(res.Languages, l)
	}
	return res, nil
}

func (g *Git) emptyTree() string {
	// Well-known empty tree object; git computes it deterministically.
	out, err := g.run("hash-object", "-t", "tree", "/dev/null")
	if err != nil {
		return "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	}
	return out
}

func (g *Git) untrackedHunk(path string, opts DiffOptions, mask func(string) string) (model.Hunk, int, bool, bool) {
	full := filepath.Join(g.Root, path)
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		return model.Hunk{}, 0, false, false
	}
	data, err := os.ReadFile(full)
	if err != nil || isBinary(data) {
		return model.Hunk{}, 0, false, false
	}
	lines := strings.Split(string(data), "\n")
	truncated := false
	if opts.MaxUntrackedLines > 0 && len(lines) > opts.MaxUntrackedLines {
		lines = lines[:opts.MaxUntrackedLines]
		truncated = true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "@@ -0,0 +1,%d @@ (new file)\n", len(lines))
	for _, ln := range lines {
		if mask != nil {
			ln = mask(ln)
		}
		b.WriteString("+" + ln + "\n")
	}
	return model.Hunk{
		Path:       path,
		Language:   languageOf(path),
		ChangeType: "added",
		Patch:      b.String(),
	}, len(lines), truncated, true
}

// --- unified diff parsing ---

type fileDiff struct {
	path       string
	changeType string
	hunks      []hunkBlock
}

type hunkBlock struct {
	funcCtx string
	body    string
	added   int
	removed int
}

func splitFileDiffs(raw string) []fileDiff {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	var files []fileDiff
	var cur *fileDiff
	var hb *hunkBlock
	flushHunk := func() {
		if cur != nil && hb != nil {
			cur.hunks = append(cur.hunks, *hb)
			hb = nil
		}
	}
	flushFile := func() {
		flushHunk()
		if cur != nil {
			files = append(files, *cur)
			cur = nil
		}
	}
	for _, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "diff --git "):
			flushFile()
			cur = &fileDiff{changeType: "modified"}
			cur.path = parseGitDiffPath(ln)
		case cur == nil:
			continue
		case strings.HasPrefix(ln, "new file"):
			cur.changeType = "added"
		case strings.HasPrefix(ln, "deleted file"):
			cur.changeType = "deleted"
		case strings.HasPrefix(ln, "rename "):
			cur.changeType = "renamed"
		case strings.HasPrefix(ln, "+++ "):
			if p := strings.TrimPrefix(ln, "+++ b/"); p != "" && p != "/dev/null" && ln != "+++ /dev/null" {
				cur.path = p
			}
		case strings.HasPrefix(ln, "--- "):
			// header; ignore
		case strings.HasPrefix(ln, "index "):
			// header; ignore
		case strings.HasPrefix(ln, "@@"):
			flushHunk()
			hb = &hunkBlock{funcCtx: parseHunkFunc(ln), body: ln + "\n"}
		case hb != nil:
			hb.body += ln + "\n"
			if strings.HasPrefix(ln, "+") {
				hb.added++
			} else if strings.HasPrefix(ln, "-") {
				hb.removed++
			}
		}
	}
	flushFile()
	return files
}

func parseGitDiffPath(line string) string {
	// diff --git a/foo b/foo
	rest := strings.TrimPrefix(line, "diff --git ")
	fields := strings.SplitN(rest, " b/", 2)
	if len(fields) == 2 {
		return fields[1]
	}
	return strings.TrimPrefix(fields[0], "a/")
}

func parseHunkFunc(line string) string {
	// @@ -a,b +c,d @@ funcname
	idx := strings.Index(line, "@@")
	if idx < 0 {
		return ""
	}
	rest := line[idx+2:]
	if j := strings.Index(rest, "@@"); j >= 0 {
		return strings.TrimSpace(rest[j+2:])
	}
	return ""
}

func maskPatch(patch string, mask func(string) string) string {
	lines := strings.Split(patch, "\n")
	for i, ln := range lines {
		if strings.HasPrefix(ln, "+") || strings.HasPrefix(ln, "-") || strings.HasPrefix(ln, " ") {
			prefix := ln[:1]
			lines[i] = prefix + mask(ln[1:])
		}
	}
	return strings.Join(lines, "\n")
}

func isBinary(data []byte) bool {
	n := len(data)
	if n > 8000 {
		n = 8000
	}
	for i := 0; i < n; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

func languageOf(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".java":
		return "java"
	case ".rb":
		return "ruby"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".hpp":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".php":
		return "php"
	case ".sql":
		return "sql"
	case ".sh", ".bash":
		return "shell"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".md":
		return "markdown"
	default:
		return ""
	}
}
