// Package change turns a debounced file-change trigger into a ChangeEvent:
// it extracts the hunk-level diff, applies redaction, builds the shadow
// snapshot commit (locally), and computes a content hash for dedup.
package change

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/acme/krennic/internal/gitxport"
	"github.com/acme/krennic/internal/model"
	"github.com/acme/krennic/internal/redact"
	"github.com/google/uuid"
)

// Builder assembles ChangeEvents for a given developer identity.
type Builder struct {
	Developer       model.Developer
	Redactor        *redact.Redactor
	Namespace       string
	DiffOptions     gitxport.DiffOptions
	SSHKey          string
	nowFn           func() time.Time
}

// New returns a Builder.
func New(dev model.Developer, r *redact.Redactor, namespace, sshKey string) *Builder {
	return &Builder{
		Developer:   dev,
		Redactor:    r,
		Namespace:   namespace,
		DiffOptions: gitxport.DefaultDiffOptions(),
		SSHKey:      sshKey,
		nowFn:       time.Now,
	}
}

// Build produces a ChangeEvent for repoPath. ok=false means there was nothing
// analyzable (clean tree / only redacted files) and the caller should skip.
func (b *Builder) Build(repoPath string) (ev model.ChangeEvent, ok bool, err error) {
	g := gitxport.New(repoPath)
	g.SSHKey = b.SSHKey
	if !g.IsRepo() {
		return ev, false, fmt.Errorf("%s is not a git repo", repoPath)
	}

	deny := b.Redactor.IsDenied
	mask := func(s string) string { m, _ := b.Redactor.MaskLine(s); return m }

	diff, err := g.WorkingTreeDiff(b.DiffOptions, deny, mask)
	if err != nil {
		return ev, false, fmt.Errorf("diff: %w", err)
	}
	if len(diff.Hunks) == 0 {
		return ev, false, nil // nothing analyzable
	}

	now := b.nowFn().UTC()
	msg := fmt.Sprintf("krennic wip %s", now.Format(time.RFC3339))
	snap, err := g.CreateShadowSnapshot(msg, deny)
	if err != nil {
		return ev, false, fmt.Errorf("snapshot: %w", err)
	}

	branch := g.CurrentBranch()
	repoName := g.RepoName()
	shadowRef := gitxport.ShadowRefName(b.Namespace, b.Developer.UserSlug, repoName, branch)

	// Attribution: use the repo's real commit identity where available.
	dev := b.Developer
	if name, email := g.GitIdentity(); name != "" || email != "" {
		if name != "" {
			dev.GitName = name
		}
		dev.GitEmail = email
	}

	sort.Strings(diff.Languages)
	sort.Strings(diff.ChangedFiles)

	ev = model.ChangeEvent{
		SchemaVersion: model.SchemaVersion,
		ChangeID:      uuid.NewString(),
		TraceID:       uuid.NewString(),
		CreatedAt:     now,
		Developer:     dev,
		Repo: model.Repo{
			Name:      repoName,
			RootHint:  repoName,
			Remote:    g.RemoteURL("origin"),
			Branch:    branch,
			HeadSHA:   g.HeadSHA(),
			ShadowRef: shadowRef,
			ShadowSHA: snap.SHA,
			LocalPath: repoPath,
		},
		Summary: model.Summary{
			FilesChanged:  len(diff.ChangedFiles),
			LinesAdded:    diff.LinesAdded,
			LinesRemoved:  diff.LinesRemoved,
			Languages:     diff.Languages,
			RedactedPaths: b.redactedPaths(g),
			Truncated:     diff.Truncated,
		},
		Diff: model.Diff{
			Format:       "unified",
			ContextLines: b.DiffOptions.ContextLines,
			Hunks:        diff.Hunks,
		},
		RoutingHints: model.RoutingHints{BudgetTier: "normal"},
		ContentHash:  contentHash(diff.Hunks),
	}
	return ev, true, nil
}

// redactedPaths lists changed paths that were excluded by the deny list, so the
// developer can see transparently what was withheld from AI/transport.
func (b *Builder) redactedPaths(g *gitxport.Git) []string {
	var out []string
	for _, p := range g.ChangedPaths() {
		if b.Redactor.IsDenied(p) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func contentHash(hunks []model.Hunk) string {
	h := sha256.New()
	for _, hk := range hunks {
		h.Write([]byte(hk.Path))
		h.Write([]byte{0})
		h.Write([]byte(hk.Patch))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
