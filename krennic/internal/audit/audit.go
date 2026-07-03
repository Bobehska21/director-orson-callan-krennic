// Package audit defines the attributed change report that every Krennic agent
// sends to the central hub, plus the hash-chain used to make the hub's audit
// log tamper-evident (no entry can be silently altered or removed).
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/acme/krennic/internal/model"
)

// Report is one attributed change: who changed what, where, when, and how the
// AI judged it. It is the shared team-wide record.
type Report struct {
	ReportID      string          `json:"report_id"`
	ChangeID      string          `json:"change_id"`
	ReportedAt    time.Time       `json:"reported_at"`
	Developer     model.Developer `json:"developer"`
	Repo          string          `json:"repo"`
	Remote        string          `json:"remote"`
	Branch        string          `json:"branch"`
	HeadSHA       string          `json:"head_sha"`
	ShadowRef     string          `json:"shadow_ref"`
	ShadowSHA     string          `json:"shadow_sha"`
	Files         []string        `json:"files"`
	LinesAdded    int             `json:"lines_added"`
	LinesRemoved  int             `json:"lines_removed"`
	Languages     []string        `json:"languages"`
	RedactedPaths []string        `json:"redacted_paths"`
	// AI outcome (optional — a report may be sent even if AI failed).
	Relevance     string   `json:"relevance,omitempty"`
	Categories    []string `json:"categories,omitempty"`
	Escalated     bool     `json:"escalated"`
	Verdict       string   `json:"verdict,omitempty"`
	FindingsCount int      `json:"findings_count"`
	ReviewSummary string   `json:"review_summary,omitempty"`
	Status        string   `json:"status"`
}

// BuildReport turns a persisted record into an attributed report.
func BuildReport(rec model.Record, reportID string, reportedAt time.Time) Report {
	ev := rec.Event
	r := Report{
		ReportID:      reportID,
		ChangeID:      ev.ChangeID,
		ReportedAt:    reportedAt.UTC(),
		Developer:     ev.Developer,
		Repo:          ev.Repo.Name,
		Remote:        ev.Repo.Remote,
		Branch:        ev.Repo.Branch,
		HeadSHA:       ev.Repo.HeadSHA,
		ShadowRef:     ev.Repo.ShadowRef,
		ShadowSHA:     ev.Repo.ShadowSHA,
		Files:         uniqueFiles(ev),
		LinesAdded:    ev.Summary.LinesAdded,
		LinesRemoved:  ev.Summary.LinesRemoved,
		Languages:     ev.Summary.Languages,
		RedactedPaths: ev.Summary.RedactedPaths,
		Status:        rec.Status,
	}
	if rec.Triage != nil {
		r.Relevance = string(rec.Triage.Relevance)
		r.Categories = rec.Triage.Categories
		r.Escalated = rec.Triage.Escalate
	}
	if rec.Review != nil {
		r.Verdict = rec.Review.Verdict
		r.FindingsCount = len(rec.Review.Findings)
		r.ReviewSummary = rec.Review.Summary
	}
	return r
}

func uniqueFiles(ev model.ChangeEvent) []string {
	set := map[string]bool{}
	for _, h := range ev.Diff.Hunks {
		set[h.Path] = true
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// Payload returns the deterministic JSON used both for transport and hashing.
func (r Report) Payload() []byte {
	b, _ := json.Marshal(r)
	return b
}

// ChainHash computes entry_hash = sha256(prevHash || payload). An append-only
// log of these hashes lets anyone detect if a past entry was altered or deleted.
func ChainHash(prevHash string, payload []byte) string {
	h := sha256.New()
	h.Write([]byte(prevHash))
	h.Write([]byte{0})
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

// GenesisHash is the prev_hash of the very first entry.
const GenesisHash = "genesis"
