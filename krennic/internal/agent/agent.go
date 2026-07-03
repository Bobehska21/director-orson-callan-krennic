// Package agent wires every component into the running daemon: watcher →
// debounce → change builder → durable queue → shadow publisher + AI gateway →
// result store → status publisher, all observed via telemetry.
package agent

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/acme/krennic/internal/ai"
	"github.com/acme/krennic/internal/audit"
	"github.com/acme/krennic/internal/change"
	"github.com/acme/krennic/internal/config"
	"github.com/acme/krennic/internal/debounce"
	"github.com/acme/krennic/internal/gitxport"
	"github.com/acme/krennic/internal/hub"
	"github.com/acme/krennic/internal/issues"
	"github.com/acme/krennic/internal/model"
	"github.com/acme/krennic/internal/redact"
	"github.com/acme/krennic/internal/secrets"
	"github.com/acme/krennic/internal/status"
	"github.com/acme/krennic/internal/store"
	"github.com/acme/krennic/internal/telemetry"
	"github.com/acme/krennic/internal/watcher"
)

const dedupWindow = 10 * time.Second

// Agent is the running daemon.
type Agent struct {
	cfg     config.Config
	log     *slog.Logger
	store   *store.Store
	metrics *telemetry.Metrics

	builder   *change.Builder
	gateway   *ai.Gateway
	watcher   *watcher.Watcher
	deb       *debounce.Debouncer
	publisher status.Publisher
	reporter  issues.Reporter
	hubClient *hub.Client

	repos     []string
	remoteFor map[string]string
	sshKey    string

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	wake   chan struct{}

	mu          sync.Mutex
	pausedUntil time.Time
}

// Deps are the externally-constructed dependencies (lets tests inject fakes).
type Deps struct {
	Store     *store.Store
	Metrics   *telemetry.Metrics
	Builder   *change.Builder
	Gateway   *ai.Gateway
	Publisher status.Publisher
	Reporter  issues.Reporter
}

// New constructs an Agent from config, resolving providers/secrets as needed.
func New(cfg config.Config, log *slog.Logger) (*Agent, error) {
	st, err := store.Open(dbPath(cfg))
	if err != nil {
		return nil, err
	}
	metrics := telemetry.NewMetrics()

	dev := model.Developer{
		UserSlug: userSlug(),
		GitName:  userSlug(),
		Machine:  hostname(),
		OSUser:   userSlug(),
	}
	red := redact.New(cfg.Redaction.Deny, cfg.Redaction.ScanRegex)

	// Resolve shadow SSH key path (optional).
	sshKey := cfg.Git.SSHKeyPath

	builder := change.New(dev, red, cfg.Git.ShadowNamespace, sshKey)

	providers, err := buildProviders(cfg)
	if err != nil {
		log.Warn("some AI providers unavailable", "err", err)
	}
	gateway := ai.NewGateway(providers, cfg.AI, cfg.Budget.DailyUSD, st)

	var pub status.Publisher
	if cfg.Status.Enabled {
		if tok, err := secrets.Resolve(cfg.Status.Identity); err == nil {
			if p, err := status.New(cfg.Status.Provider, tok); err == nil {
				pub = p
			} else {
				log.Warn("status publisher init failed", "err", err)
			}
		} else {
			log.Warn("status token unavailable", "err", err)
		}
	}

	var issueReporter issues.Reporter
	if cfg.Issues.Enabled {
		if tok, err := secrets.Resolve(cfg.Issues.Identity); err == nil {
			if r, err := issues.New(cfg.Issues.Provider, tok); err == nil {
				issueReporter = r
			} else {
				log.Warn("issue reporter init failed", "err", err)
			}
		} else {
			log.Warn("issue token unavailable", "err", err)
		}
	}

	// Central hub reporting (optional). When configured, every change is
	// reported for team-wide, tamper-evident attribution.
	var hubClient *hub.Client
	if cfg.Hub.URL != "" {
		token, _ := secrets.Resolve(cfg.Hub.TokenIdentity)
		hubClient = hub.NewClient(cfg.Hub.URL, token)
		log.Info("hub reporting enabled", "url", cfg.Hub.URL)
	}

	repos := resolveRepos(cfg)
	remoteFor := resolveRemotes(cfg, repos)

	return &Agent{
		cfg: cfg, log: log, store: st, metrics: metrics,
		builder: builder, gateway: gateway, publisher: pub, reporter: issueReporter, hubClient: hubClient,
		repos: repos, remoteFor: remoteFor, sshKey: sshKey,
		wake: make(chan struct{}, 64),
	}, nil
}

// buildProviders constructs the enabled provider adapters from configured keys.
func buildProviders(cfg config.Config) (map[string]ai.Provider, error) {
	providers := map[string]ai.Provider{}
	// CLI needs no key.
	cli := ai.NewClaudeCLI()
	if cli.Available() {
		providers["claude-cli"] = cli
	}
	if key, err := secrets.Resolve("anthropic"); err == nil && key != "" {
		providers["anthropic"] = ai.NewAnthropic(key)
	}
	if key, err := secrets.Resolve("gemini"); err == nil && key != "" {
		providers["gemini"] = ai.NewGemini(key)
	}
	if len(providers) == 0 {
		return providers, errNoProviders
	}
	return providers, nil
}

// Run starts the watcher and workers and blocks until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx)
	defer a.cancel()

	if n, err := a.store.ResetInflight(); err == nil && n > 0 {
		a.log.Info("requeued interrupted work", "count", n)
	}

	a.deb = debounce.New(
		time.Duration(a.cfg.Agent.DebounceMS)*time.Millisecond,
		time.Duration(a.cfg.Agent.MaxWaitMS)*time.Millisecond,
		a.handleChange,
	)

	w, err := watcher.New(a.repos, a.log, func(root string) { a.deb.Trigger(root) })
	if err != nil {
		return err
	}
	a.watcher = w
	if err := w.Start(); err != nil {
		return err
	}
	a.log.Info("watching", "repos", len(a.repos))

	workers := a.cfg.Agent.AIWorkers
	if workers < 1 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		a.wg.Add(1)
		go a.worker()
	}

	// Background hub delivery (durable outbox → central audit).
	if a.hubClient != nil {
		a.wg.Add(1)
		go a.outboxSender()
	}

	// Start the local dashboard/control server.
	srv := a.newHTTPServer()
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			a.log.Debug("dashboard server stopped", "err", err)
		}
	}()

	<-a.ctx.Done()
	a.log.Info("shutting down")
	_ = a.watcher.Close()
	a.deb.Stop()
	_ = srv.Close()
	a.wg.Wait()
	return a.store.Close()
}

// handleChange runs on the debounced trigger: build event, dedup, enqueue.
func (a *Agent) handleChange(repoRoot string) {
	if a.isPaused() {
		return
	}
	t0 := time.Now()
	ev, ok, err := a.builder.Build(repoRoot)
	if err != nil {
		a.log.Warn("build change failed", "repo", repoRoot, "err", err)
		return
	}
	if !ok {
		return // nothing analyzable
	}

	if seen, _ := a.store.SeenRecently(ev.ContentHash, dedupWindow); seen {
		a.metrics.Inc(telemetry.CacheHits)
		return
	}
	if err := a.store.Enqueue(ev); err != nil {
		a.log.Warn("enqueue failed", "err", err)
		return
	}
	a.metrics.Inc(telemetry.ChangesProcessed)
	a.metrics.Observe(telemetry.EventLatencyMS, float64(time.Since(t0).Milliseconds()))
	a.updateQueueDepth()
	a.log.Info("change queued", "repo", ev.Repo.Name, "branch", ev.Repo.Branch,
		"files", ev.Summary.FilesChanged, "change_id", ev.ChangeID, "trace_id", ev.TraceID)

	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// worker claims queued events and processes them.
func (a *Agent) worker() {
	defer a.wg.Done()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-a.wake:
		case <-time.After(750 * time.Millisecond):
		}
		for {
			ev, ok, err := a.store.Claim()
			if err != nil {
				a.log.Warn("claim failed", "err", err)
				break
			}
			if !ok {
				break
			}
			a.processEvent(ev)
			a.updateQueueDepth()
		}
	}
}

// processEvent publishes the shadow ref (best-effort), runs AI, and publishes status.
func (a *Agent) processEvent(ev model.ChangeEvent) {
	a.publishShadow(ev) // best-effort; never blocks analysis

	a.metrics.Inc(telemetry.TriageTotal)
	tStart := time.Now()
	res, err := a.gateway.Analyze(a.ctx, ev)
	if err != nil {
		a.metrics.Inc(telemetry.ProviderErrors)
		a.log.Warn("analysis failed", "change_id", ev.ChangeID, "err", err)
		_ = a.store.Fail(ev.ChangeID, err.Error())
		return
	}
	a.metrics.Observe(telemetry.ModelLatencyMS, float64(time.Since(tStart).Milliseconds()))
	if res.Escalated {
		a.metrics.Inc(telemetry.TriageEscalations)
	}
	if res.Triage != nil {
		a.metrics.Add(telemetry.AICostUSD, res.Triage.CostUSD)
	}
	if res.Review != nil {
		a.metrics.Add(telemetry.AICostUSD, res.Review.CostUSD)
	}

	if a.publisher != nil {
		if err := a.publisher.Publish(a.ctx, ev, res.Triage, res.Review); err != nil {
			a.log.Warn("status publish failed", "change_id", ev.ChangeID, "err", err)
		}
	}
	if a.reporter != nil && res.Review != nil && res.Review.Verdict == "request-changes" {
		if err := a.reporter.Report(a.ctx, ev, res.Review); err != nil {
			a.log.Warn("issue create failed", "change_id", ev.ChangeID, "err", err)
		}
	}
	_ = a.store.Complete(ev.ChangeID)
	a.enqueueReport(ev.ChangeID)

	a.log.Info("analysis done", "change_id", ev.ChangeID, "trace_id", ev.TraceID,
		"escalated", res.Escalated, "budget_hit", res.BudgetHit)
}

// enqueueReport builds the attributed report for a finished change and queues it
// for durable delivery to the hub (never lost, even if the hub is down).
func (a *Agent) enqueueReport(changeID string) {
	if a.hubClient == nil {
		return
	}
	rec, err := a.store.GetRecord(changeID)
	if err != nil || rec == nil {
		return
	}
	// report_id == change_id makes hub delivery idempotent across retries.
	rep := audit.BuildReport(*rec, changeID, time.Now())
	if err := a.store.EnqueueOutbox(changeID, changeID, rep.Payload()); err != nil {
		a.log.Warn("outbox enqueue failed", "change_id", changeID, "err", err)
	}
}

// outboxSender continuously delivers queued reports to the hub with retry.
func (a *Agent) outboxSender() {
	defer a.wg.Done()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
		}
		items, err := a.store.ListOutbox(50)
		if err != nil {
			continue
		}
		for _, it := range items {
			if a.ctx.Err() != nil {
				return
			}
			if err := a.hubClient.Send(a.ctx, it.Payload); err != nil {
				_ = a.store.IncOutboxAttempts(it.ReportID)
				a.log.Debug("hub delivery failed; will retry", "change_id", it.ChangeID, "err", err)
				break // hub likely down; retry whole batch next tick
			}
			_ = a.store.DeleteOutbox(it.ReportID)
		}
		if n, err := a.store.OutboxDepth(); err == nil {
			a.metrics.SetGauge("krennic_hub_outbox_depth", float64(n))
		}
	}
}

// publishShadow force-pushes the snapshot commit to the ai-remote.
func (a *Agent) publishShadow(ev model.ChangeEvent) {
	a.metrics.Inc(telemetry.ShadowPushTotal)
	url := a.remoteFor[ev.Repo.LocalPath]
	if url == "" {
		a.log.Debug("no ai-remote url; skipping shadow push", "repo", ev.Repo.Name)
		return
	}
	g := gitxport.New(ev.Repo.LocalPath)
	g.SSHKey = a.sshKey
	if err := g.EnsureRemote("ai-remote", url); err != nil {
		a.metrics.Inc(telemetry.ShadowPushFailures)
		a.log.Warn("ensure ai-remote failed", "err", err)
		return
	}
	if err := g.PublishShadow("ai-remote", ev.Repo.ShadowRef, ev.Repo.ShadowSHA); err != nil {
		a.metrics.Inc(telemetry.ShadowPushFailures)
		a.log.Warn("shadow push failed", "change_id", ev.ChangeID, "err", err)
		return
	}
	a.log.Info("shadow pushed", "ref", ev.Repo.ShadowRef, "sha", short(ev.Repo.ShadowSHA))
}

func (a *Agent) updateQueueDepth() {
	if n, err := a.store.PendingCount(); err == nil {
		a.metrics.SetGauge(telemetry.QueueDepth, float64(n))
	}
}

// --- pause control ---

func (a *Agent) isPaused() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return time.Now().Before(a.pausedUntil)
}

func (a *Agent) pause(d time.Duration) {
	a.mu.Lock()
	a.pausedUntil = time.Now().Add(d)
	a.mu.Unlock()
}

func (a *Agent) resume() {
	a.mu.Lock()
	a.pausedUntil = time.Time{}
	a.mu.Unlock()
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown-host"
	}
	return h
}
