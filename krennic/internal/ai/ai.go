// Package ai is the provider-agnostic analysis layer. A single Provider
// interface abstracts Anthropic, Gemini, and the local Claude Code CLI; the
// Gateway builds prompts, runs the two-stage triage→review routing, and
// normalizes every provider's output into the internal result types.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/acme/krennic/internal/config"
	"github.com/acme/krennic/internal/model"
)

// CompletionRequest is what every provider adapter receives.
type CompletionRequest struct {
	System     string
	User       string
	Model      string
	MaxTokens  int
	JSONOutput bool
}

// CompletionResponse is the normalized provider reply.
type CompletionResponse struct {
	Text    string
	Tokens  model.TokenUsage
	CostUSD float64
}

// Provider is one AI backend. Adapters implement only Complete; prompt
// construction and JSON parsing live in the Gateway, so backends stay swappable.
type Provider interface {
	Name() string
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

// Sink receives results as they are produced (store + telemetry).
type Sink interface {
	SaveTriage(changeID string, tr *model.TriageResult) error
	SaveReview(changeID string, rv *model.ReviewResult) error
	AddCost(day string, usd float64) error
	SpendForDay(day string) (float64, error)
}

// Gateway orchestrates the two-stage analysis.
type Gateway struct {
	providers map[string]Provider
	triage    config.StageConfig
	review    config.StageConfig
	routing   config.RoutingConfig
	fallback  string
	dailyUSD  float64
	sink      Sink
	nowFn     func() time.Time
}

// NewGateway wires providers and config into a Gateway.
func NewGateway(providers map[string]Provider, ai config.AIConfig, dailyUSD float64, sink Sink) *Gateway {
	return &Gateway{
		providers: providers,
		triage:    ai.Triage,
		review:    ai.Review,
		routing:   ai.Routing,
		fallback:  ai.Fallback,
		dailyUSD:  dailyUSD,
		sink:      sink,
		nowFn:     time.Now,
	}
}

// Result bundles what an analysis produced.
type Result struct {
	Triage    *model.TriageResult
	Review    *model.ReviewResult
	Escalated bool
	BudgetHit bool
}

// Analyze runs triage, decides escalation, and optionally runs deep review.
func (g *Gateway) Analyze(ctx context.Context, ev model.ChangeEvent) (Result, error) {
	var res Result

	tr, err := g.runTriage(ctx, ev)
	if err != nil {
		return res, fmt.Errorf("triage: %w", err)
	}
	res.Triage = tr
	if g.sink != nil {
		_ = g.sink.SaveTriage(ev.ChangeID, tr)
		g.charge(tr.CostUSD)
	}

	if !g.shouldEscalate(ev, tr) {
		return res, nil
	}
	res.Escalated = true

	// Budget gate: when the daily budget is exhausted, downgrade to triage-only.
	if g.dailyUSD > 0 && g.sink != nil {
		day := g.nowFn().UTC().Format("2006-01-02")
		if spent, _ := g.sink.SpendForDay(day); spent >= g.dailyUSD {
			res.BudgetHit = true
			return res, nil
		}
	}

	rv, err := g.runReview(ctx, ev, tr)
	if err != nil {
		return res, fmt.Errorf("review: %w", err)
	}
	res.Review = rv
	if g.sink != nil {
		_ = g.sink.SaveReview(ev.ChangeID, rv)
		g.charge(rv.CostUSD)
	}
	return res, nil
}

func (g *Gateway) charge(usd float64) {
	if usd <= 0 || g.sink == nil {
		return
	}
	_ = g.sink.AddCost(g.nowFn().UTC().Format("2006-01-02"), usd)
}

// shouldEscalate is the deterministic routing rule.
func (g *Gateway) shouldEscalate(ev model.ChangeEvent, tr *model.TriageResult) bool {
	if ev.RoutingHints.ForceStage == "review" {
		return true
	}
	if ev.RoutingHints.ForceStage == "triage" {
		return false
	}
	if tr.Relevance == model.Trivial {
		return false // primary noise/cost control
	}
	if tr.Escalate || tr.Relevance == model.Notable || tr.Relevance == model.Risky {
		return true
	}
	for _, c := range tr.Categories {
		for _, want := range g.routing.EscalateCategories {
			if strings.EqualFold(c, want) {
				return true
			}
		}
	}
	if th := g.routing.EscalateLineThreshold; th > 0 &&
		(ev.Summary.LinesAdded+ev.Summary.LinesRemoved) > th &&
		!isPureStyle(tr) {
		return true
	}
	return false
}

func isPureStyle(tr *model.TriageResult) bool {
	if len(tr.Categories) == 0 {
		return false
	}
	for _, c := range tr.Categories {
		if !strings.EqualFold(c, "style") {
			return false
		}
	}
	return true
}

func (g *Gateway) runTriage(ctx context.Context, ev model.ChangeEvent) (*model.TriageResult, error) {
	resp, provName, err := g.complete(ctx, g.triage, CompletionRequest{
		System:     triageSystemPrompt,
		User:       renderEventPrompt(ev),
		Model:      g.triage.Model,
		MaxTokens:  512,
		JSONOutput: true,
	})
	if err != nil {
		return nil, err
	}
	var tr model.TriageResult
	if err := parseJSON(resp.Text, &tr); err != nil {
		return nil, fmt.Errorf("parse triage json from %s: %w", provName, err)
	}
	tr.ChangeID = ev.ChangeID
	tr.Provider = provName
	tr.Model = g.triage.Model
	tr.Tokens = resp.Tokens
	tr.CostUSD = resp.CostUSD
	if tr.Relevance == "" {
		tr.Relevance = model.Minor
	}
	return &tr, nil
}

func (g *Gateway) runReview(ctx context.Context, ev model.ChangeEvent, tr *model.TriageResult) (*model.ReviewResult, error) {
	resp, provName, err := g.complete(ctx, g.review, CompletionRequest{
		System:     reviewSystemPrompt,
		User:       renderReviewPrompt(ev, tr),
		Model:      g.review.Model,
		MaxTokens:  2048,
		JSONOutput: true,
	})
	if err != nil {
		return nil, err
	}
	var rv model.ReviewResult
	if err := parseJSON(resp.Text, &rv); err != nil {
		return nil, fmt.Errorf("parse review json from %s: %w", provName, err)
	}
	rv.ChangeID = ev.ChangeID
	rv.Provider = provName
	rv.Model = g.review.Model
	rv.Tokens = resp.Tokens
	rv.CostUSD = resp.CostUSD
	if rv.Verdict == "" {
		rv.Verdict = "comment"
	}
	return &rv, nil
}

// complete runs a stage against its provider, falling back on error.
func (g *Gateway) complete(ctx context.Context, stage config.StageConfig, req CompletionRequest) (CompletionResponse, string, error) {
	prov, ok := g.providers[stage.Provider]
	if !ok {
		return CompletionResponse{}, "", fmt.Errorf("provider %q not configured", stage.Provider)
	}
	resp, err := prov.Complete(ctx, req)
	if err == nil {
		return resp, prov.Name(), nil
	}
	if g.fallback != "" && g.fallback != stage.Provider {
		if fb, ok := g.providers[g.fallback]; ok {
			if r2, e2 := fb.Complete(ctx, req); e2 == nil {
				return r2, fb.Name(), nil
			}
		}
	}
	return CompletionResponse{}, prov.Name(), err
}

// parseJSON extracts the first JSON object from text (providers occasionally
// wrap JSON in prose) and unmarshals it into v.
func parseJSON(text string, v any) error {
	s := strings.TrimSpace(text)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return fmt.Errorf("no JSON object found in response")
	}
	return json.Unmarshal([]byte(s[start:end+1]), v)
}
