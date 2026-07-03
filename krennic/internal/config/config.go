// Package config loads and validates the Krennic TOML config. Secrets are
// never stored here — the config only references keychain key *names*; the
// daemon resolves the actual secret values at runtime via internal/secrets.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the full agent configuration.
type Config struct {
	Agent     AgentConfig     `toml:"agent"`
	Repos     []RepoConfig    `toml:"repos"`
	Redaction RedactionConfig `toml:"redaction"`
	Git       GitConfig       `toml:"git_transport"`
	AI        AIConfig        `toml:"ai"`
	Budget    BudgetConfig    `toml:"budget"`
	Status    StatusConfig    `toml:"status"`
	Issues    IssuesConfig    `toml:"issues"`
	Telemetry TelemetryConfig `toml:"telemetry"`
	Hub       HubConfig       `toml:"hub"`
}

// HubConfig configures the central team hub. On agents, URL points at the hub
// and TokenIdentity names the keychain key holding the shared token. On the
// hub itself, ListenAddr/DBPath/TokenIdentity are used by `krennic hub`.
type HubConfig struct {
	URL           string `toml:"url"`            // agent → hub base URL (empty = reporting off)
	TokenIdentity string `toml:"token_identity"` // keychain key for the shared token
	ListenAddr    string `toml:"listen_addr"`    // hub server bind address
	DBPath        string `toml:"db_path"`        // hub audit database path
}

type AgentConfig struct {
	WatchRoots    []string `toml:"watch_roots"`
	DebounceMS    int      `toml:"debounce_ms"`
	MaxWaitMS     int      `toml:"max_wait_ms"`
	DashboardAddr string   `toml:"dashboard_addr"`
	AIWorkers     int      `toml:"ai_workers"`
	HeadPollMS    int      `toml:"head_poll_ms"`
}

type RepoConfig struct {
	Path    string `toml:"path"`
	Enabled bool   `toml:"enabled"`
	// RemoteURL overrides git_transport.remote_url for this repo (optional).
	RemoteURL string `toml:"remote_url"`
}

type RedactionConfig struct {
	Deny      []string `toml:"deny"`
	ScanRegex bool     `toml:"scan_regex"`
}

type GitConfig struct {
	Provider        string `toml:"provider"` // github|gitlab
	RemoteURL       string `toml:"remote_url"`
	ShadowNamespace string `toml:"shadow_namespace"`
	RetainSnapshots int    `toml:"retain_snapshots"`
	Identity        string `toml:"identity"` // keychain key name for shadow-write cred
	SSHKeyPath      string `toml:"ssh_key_path"`
}

type StageConfig struct {
	Provider string `toml:"provider"` // anthropic|gemini|claude-cli
	Model    string `toml:"model"`
}

type RoutingConfig struct {
	EscalateCategories    []string `toml:"escalate_categories"`
	EscalateLineThreshold int      `toml:"escalate_line_threshold"`
}

type AIConfig struct {
	Triage  StageConfig   `toml:"triage"`
	Review  StageConfig   `toml:"review"`
	Routing RoutingConfig `toml:"routing"`
	// Fallback provider used if the primary errors (optional).
	Fallback string `toml:"fallback"`
}

type BudgetConfig struct {
	DailyUSD float64 `toml:"daily_usd"`
}

type StatusConfig struct {
	Enabled  bool   `toml:"enabled"`
	Provider string `toml:"provider"`
	Identity string `toml:"identity"` // keychain key name, repo:status scope
}

type IssuesConfig struct {
	Enabled  bool   `toml:"enabled"`
	Provider string `toml:"provider"`
	Identity string `toml:"identity"` // keychain key name, issues:write scope
}

type TelemetryConfig struct {
	Enabled      bool   `toml:"enabled"`
	OTLPEndpoint string `toml:"otlp_endpoint"`
}

// DefaultPath returns the per-OS config file location.
func DefaultPath() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "Krennic", "config.toml")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Krennic", "config.toml")
	default: // linux and friends
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			home, _ := os.UserHomeDir()
			base = filepath.Join(home, ".config")
		}
		return filepath.Join(base, "krennic", "config.toml")
	}
}

// Default returns a config populated with sane defaults.
func Default() Config {
	return Config{
		Agent: AgentConfig{
			WatchRoots:    []string{expandHome("~/code")},
			DebounceMS:    800,
			MaxWaitMS:     5000,
			DashboardAddr: "127.0.0.1:7373",
			AIWorkers:     2,
			HeadPollMS:    5000,
		},
		Redaction: RedactionConfig{
			Deny:      []string{".env*", "*.pem", "*.key", "id_rsa*", "secrets/**"},
			ScanRegex: true,
		},
		Git: GitConfig{
			Provider:        "github",
			ShadowNamespace: "refs/ai",
			RetainSnapshots: 5,
			Identity:        "git-shadow",
		},
		AI: AIConfig{
			Triage: StageConfig{Provider: "anthropic", Model: "claude-haiku-4-5-20251001"},
			Review: StageConfig{Provider: "anthropic", Model: "claude-sonnet-5"},
			Routing: RoutingConfig{
				EscalateCategories:    []string{"security", "logic", "test-gap"},
				EscalateLineThreshold: 80,
			},
		},
		Budget:    BudgetConfig{DailyUSD: 5.0},
		Status:    StatusConfig{Enabled: false, Provider: "github", Identity: "status-token"},
		Issues:    IssuesConfig{Enabled: false, Provider: "github", Identity: "status-token"},
		Telemetry: TelemetryConfig{Enabled: true},
		Hub:       HubConfig{TokenIdentity: "hub-token", ListenAddr: ":8787"},
	}
}

// Load reads and validates the config at path, filling defaults for empties.
func Load(path string) (Config, error) {
	cfg := Default()
	if _, err := os.Stat(path); err != nil {
		return cfg, fmt.Errorf("config not found at %s: %w", path, err)
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.normalize()
	return cfg, cfg.Validate()
}

func (c *Config) normalize() {
	for i := range c.Agent.WatchRoots {
		c.Agent.WatchRoots[i] = expandHome(c.Agent.WatchRoots[i])
	}
	for i := range c.Repos {
		c.Repos[i].Path = expandHome(c.Repos[i].Path)
	}
	if c.Agent.DebounceMS == 0 {
		c.Agent.DebounceMS = 800
	}
	if c.Agent.MaxWaitMS == 0 {
		c.Agent.MaxWaitMS = 5000
	}
	if c.Agent.AIWorkers == 0 {
		c.Agent.AIWorkers = 2
	}
	if c.Agent.HeadPollMS == 0 {
		c.Agent.HeadPollMS = 5000
	}
	if c.Git.ShadowNamespace == "" {
		c.Git.ShadowNamespace = "refs/ai"
	}
	if c.Git.RetainSnapshots == 0 {
		c.Git.RetainSnapshots = 5
	}
	if c.Status.Provider == "" {
		c.Status.Provider = c.Git.Provider
	}
	if c.Issues.Provider == "" {
		c.Issues.Provider = c.Git.Provider
	}
}

// Validate checks required invariants.
func (c *Config) Validate() error {
	if len(c.Agent.WatchRoots) == 0 && len(c.Repos) == 0 {
		return fmt.Errorf("no watch_roots and no repos configured — nothing to watch")
	}
	if c.Git.Provider != "github" && c.Git.Provider != "gitlab" {
		return fmt.Errorf("git_transport.provider must be github or gitlab, got %q", c.Git.Provider)
	}
	if c.Issues.Enabled && c.Issues.Provider != "github" {
		return fmt.Errorf("issues.provider must be github when issues are enabled, got %q", c.Issues.Provider)
	}
	if !strings.HasPrefix(c.Git.ShadowNamespace, "refs/") {
		return fmt.Errorf("git_transport.shadow_namespace must start with refs/, got %q", c.Git.ShadowNamespace)
	}
	return nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
