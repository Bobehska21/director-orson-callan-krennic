package ai

import (
	"fmt"
	"strings"

	"github.com/acme/krennic/internal/model"
)

// maxPromptBytes bounds how much diff text we send (cost/latency control).
const maxPromptBytes = 24_000

const triageSystemPrompt = `Jsi interní code-review triage agent. Hodnotíš POUZE změny dodané v diffu.
Nikdy nespekuluj o neviděném kódu. Rozhodni rychle relevanci a zda eskalovat na hlubší review.
Vrať POUZE JSON přesně v tomto schématu, bez prose textu:
{
  "relevance": "trivial|minor|notable|risky",
  "categories": ["logic|security|style|test-gap|perf"],
  "escalate": true|false,
  "reason": "krátké zdůvodnění",
  "confidence": 0.0-1.0
}`

const reviewSystemPrompt = `Jsi interní code-review agent. Hodnotíš POUZE změny dodané v diffu.
Nikdy nespekuluj o neviděném kódu; pokud chybí kontext, řekni to v missing_context přes finding message.
Najdi konkrétní rizika, urči dopad na API/kontrakt/autorizaci/datové toky/error handling a navrhni testy.
Vrať POUZE JSON přesně v tomto schématu, bez prose textu:
{
  "verdict": "pass|comment|request-changes",
  "summary": "stručné shrnutí změny a hlavního rizika",
  "findings": [
    {
      "path": "cesta/k/souboru",
      "line": 0,
      "severity": "low|medium|high|critical",
      "type": "logic|security|style|test-gap|perf",
      "message": "co je špatně",
      "suggestion": "jak to opravit",
      "confidence": 0.0-1.0
    }
  ]
}`

// renderEventPrompt renders metadata + hunks for triage (bounded).
func renderEventPrompt(ev model.ChangeEvent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<metadata>\nrepo=%s\nbranch=%s\nbase_sha=%s\nfiles_changed=%d\nlines_added=%d\nlines_removed=%d\nlanguages=%s\n",
		ev.Repo.Name, ev.Repo.Branch, ev.Repo.HeadSHA,
		ev.Summary.FilesChanged, ev.Summary.LinesAdded, ev.Summary.LinesRemoved,
		strings.Join(ev.Summary.Languages, ","))
	if len(ev.Summary.RedactedPaths) > 0 {
		fmt.Fprintf(&b, "redacted_paths=%s\n", strings.Join(ev.Summary.RedactedPaths, ","))
	}
	b.WriteString("</metadata>\n\n<changed_files>\n")
	writeHunks(&b, ev.Diff.Hunks)
	b.WriteString("</changed_files>\n")
	return b.String()
}

// renderReviewPrompt adds the triage verdict as context for the deeper stage.
func renderReviewPrompt(ev model.ChangeEvent, tr *model.TriageResult) string {
	var b strings.Builder
	if tr != nil {
		fmt.Fprintf(&b, "<triage>\nrelevance=%s\ncategories=%s\nreason=%s\n</triage>\n\n",
			tr.Relevance, strings.Join(tr.Categories, ","), tr.Reason)
	}
	b.WriteString(renderEventPrompt(ev))
	return b.String()
}

func writeHunks(b *strings.Builder, hunks []model.Hunk) {
	written := 0
	for _, h := range hunks {
		block := fmt.Sprintf("--- %s (%s%s)\n%s\n", h.Path, h.ChangeType,
			funcSuffix(h.FunctionContext), strings.TrimRight(h.Patch, "\n"))
		if written+len(block) > maxPromptBytes {
			b.WriteString(fmt.Sprintf("... [zkráceno, %d dalších hunků vynecháno]\n", len(hunks)))
			return
		}
		b.WriteString(block)
		written += len(block)
	}
}

func funcSuffix(fn string) string {
	if fn == "" {
		return ""
	}
	return " @ " + fn
}
