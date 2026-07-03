package ai

import (
	"testing"

	"github.com/acme/krennic/internal/config"
	"github.com/acme/krennic/internal/model"
)

func testGateway() *Gateway {
	return &Gateway{
		routing: config.RoutingConfig{
			EscalateCategories:    []string{"security", "logic", "test-gap"},
			EscalateLineThreshold: 80,
		},
	}
}

func evWithLines(n int) model.ChangeEvent {
	return model.ChangeEvent{Summary: model.Summary{LinesAdded: n}}
}

func TestShouldEscalate(t *testing.T) {
	g := testGateway()
	cases := []struct {
		name string
		ev   model.ChangeEvent
		tr   model.TriageResult
		want bool
	}{
		{"trivial suppressed", evWithLines(5), model.TriageResult{Relevance: model.Trivial}, false},
		{"risky escalates", evWithLines(5), model.TriageResult{Relevance: model.Risky}, true},
		{"notable escalates", evWithLines(5), model.TriageResult{Relevance: model.Notable}, true},
		{"escalate flag", evWithLines(5), model.TriageResult{Relevance: model.Minor, Escalate: true}, true},
		{"security category", evWithLines(5), model.TriageResult{Relevance: model.Minor, Categories: []string{"security"}}, true},
		{"big non-style change", evWithLines(200), model.TriageResult{Relevance: model.Minor, Categories: []string{"logic"}}, true},
		{"big pure-style suppressed", evWithLines(200), model.TriageResult{Relevance: model.Minor, Categories: []string{"style"}}, false},
		{"minor small stays", evWithLines(5), model.TriageResult{Relevance: model.Minor, Categories: []string{"style"}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := c.tr
			if got := g.shouldEscalate(c.ev, &tr); got != c.want {
				t.Errorf("shouldEscalate=%v want %v", got, c.want)
			}
		})
	}
}

func TestForceStageOverrides(t *testing.T) {
	g := testGateway()
	ev := evWithLines(5)
	ev.RoutingHints.ForceStage = "review"
	if !g.shouldEscalate(ev, &model.TriageResult{Relevance: model.Trivial}) {
		t.Error("force review should escalate even trivial")
	}
	ev.RoutingHints.ForceStage = "triage"
	if g.shouldEscalate(ev, &model.TriageResult{Relevance: model.Risky}) {
		t.Error("force triage should suppress escalation")
	}
}
