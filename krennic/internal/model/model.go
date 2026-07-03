// Package model defines the internal data contract shared by every component:
// the Change Event produced by the watcher/change-builder, and the Triage /
// Review results produced by the AI adapters. Every producer and every provider
// adapter speaks these types, which is what makes providers swappable.
package model

import "time"

// SchemaVersion is bumped when the wire contract changes.
const SchemaVersion = "1.0"

// Developer identifies who produced the change and on which machine.
type Developer struct {
	UserSlug string `json:"user_slug"`
	GitName  string `json:"git_name"`
	GitEmail string `json:"git_email"`
	Machine  string `json:"machine"`
	OSUser   string `json:"os_user"`
}

// Repo describes the repository and the shadow ref the snapshot was published to.
type Repo struct {
	Name      string `json:"name"`
	RootHint  string `json:"root_hint"`
	Remote    string `json:"remote"`
	Branch    string `json:"branch"`
	HeadSHA   string `json:"head_sha"`
	ShadowRef string `json:"shadow_ref"`
	ShadowSHA string `json:"shadow_sha"`
	// LocalPath is the on-disk repo root. Local-only bookkeeping (never rendered
	// into an AI prompt); persisted so work survives a restart.
	LocalPath string `json:"local_path,omitempty"`
}

// Summary is a cheap-to-compute overview of the change.
type Summary struct {
	FilesChanged  int      `json:"files_changed"`
	LinesAdded    int      `json:"lines_added"`
	LinesRemoved  int      `json:"lines_removed"`
	Languages     []string `json:"languages"`
	RedactedPaths []string `json:"redacted_paths"`
	Truncated     bool     `json:"truncated"`
}

// Hunk is a single unified-diff hunk for one file.
type Hunk struct {
	Path            string `json:"path"`
	Language        string `json:"language"`
	ChangeType      string `json:"change_type"` // added|modified|deleted|renamed
	FunctionContext string `json:"function_context,omitempty"`
	Patch           string `json:"patch"`
}

// Diff bundles the hunks with the context settings used to produce them.
type Diff struct {
	Format       string `json:"format"`
	ContextLines int    `json:"context_lines"`
	Hunks        []Hunk `json:"hunks"`
}

// RoutingHints let a producer force a stage or budget tier.
type RoutingHints struct {
	ForceStage string `json:"force_stage,omitempty"` // "" | "triage" | "review"
	BudgetTier string `json:"budget_tier,omitempty"` // "normal" | "high"
}

// ChangeEvent is THE internal contract. One is produced per debounced change.
type ChangeEvent struct {
	SchemaVersion string       `json:"schema_version"`
	ChangeID      string       `json:"change_id"`
	TraceID       string       `json:"trace_id"`
	CreatedAt     time.Time    `json:"created_at"`
	Developer     Developer    `json:"developer"`
	Repo          Repo         `json:"repo"`
	Summary       Summary      `json:"summary"`
	Diff          Diff         `json:"diff"`
	RoutingHints  RoutingHints `json:"routing_hints"`
	// ContentHash is used for dedup (formatter-revert = no-op).
	ContentHash string `json:"content_hash"`
}

// Relevance is the triage verdict.
type Relevance string

const (
	Trivial Relevance = "trivial"
	Minor   Relevance = "minor"
	Notable Relevance = "notable"
	Risky   Relevance = "risky"
)

// TokenUsage / cost accounting, normalized across providers.
type TokenUsage struct {
	In  int `json:"in"`
	Out int `json:"out"`
}

// TriageResult is the cheap first-stage output.
type TriageResult struct {
	ChangeID   string     `json:"change_id"`
	Relevance  Relevance  `json:"relevance"`
	Categories []string   `json:"categories"`
	Escalate   bool       `json:"escalate"`
	Reason     string     `json:"reason"`
	Confidence float64    `json:"confidence"`
	Provider   string     `json:"provider"`
	Model      string     `json:"model"`
	Tokens     TokenUsage `json:"tokens"`
	CostUSD    float64    `json:"cost_usd"`
}

// Finding is one issue reported by deep review.
type Finding struct {
	Path       string  `json:"path"`
	Line       int     `json:"line"`
	Severity   string  `json:"severity"` // low|medium|high|critical
	Type       string  `json:"type"`     // logic|security|style|test-gap|perf
	Message    string  `json:"message"`
	Suggestion string  `json:"suggestion"`
	Confidence float64 `json:"confidence"`
}

// ReviewResult is the expensive second-stage output.
type ReviewResult struct {
	ChangeID string     `json:"change_id"`
	Verdict  string     `json:"verdict"` // pass|comment|request-changes
	Summary  string     `json:"summary"`
	Findings []Finding  `json:"findings"`
	Provider string     `json:"provider"`
	Model    string     `json:"model"`
	Tokens   TokenUsage `json:"tokens"`
	CostUSD  float64    `json:"cost_usd"`
}

// Record is the persisted, unified view stored per change and shown in the UI.
type Record struct {
	Event     ChangeEvent   `json:"event"`
	Triage    *TriageResult `json:"triage,omitempty"`
	Review    *ReviewResult `json:"review,omitempty"`
	Status    string        `json:"status"` // pending|publishing|analyzing|done|failed
	Error     string        `json:"error,omitempty"`
	UpdatedAt time.Time     `json:"updated_at"`
}
