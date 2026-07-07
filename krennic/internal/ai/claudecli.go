package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/acme/krennic/internal/secrets"
)

const claudeOAuthTokenSecret = "claude-oauth-token"
const claudePermissionMode = "bypassPermissions"

// ClaudeOAuthTokenSecret is the Krennic keychain entry used by claude-cli.
func ClaudeOAuthTokenSecret() string {
	return claudeOAuthTokenSecret
}

// ClaudeCLIProvider shells out to the local `claude` CLI in headless print mode.
// Useful when developers already have Claude Code and want subscription-based
// auth / local repo context instead of a raw API key.
type ClaudeCLIProvider struct {
	Bin string // defaults to "claude"
}

// NewClaudeCLI builds a CLI provider.
func NewClaudeCLI() *ClaudeCLIProvider { return &ClaudeCLIProvider{Bin: "claude"} }

func (p *ClaudeCLIProvider) Name() string { return "claude-cli" }

// Available reports whether the claude binary is on PATH.
func (p *ClaudeCLIProvider) Available() bool {
	_, err := exec.LookPath(p.bin())
	return err == nil
}

func (p *ClaudeCLIProvider) bin() string {
	if p.Bin == "" {
		return "claude"
	}
	return p.Bin
}

func (p *ClaudeCLIProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	args := []string{"-p", req.User,
		"--output-format", "json",
		"--allowedTools", "Read",
		"--permission-mode", claudePermissionMode,
	}
	if req.System != "" {
		args = append(args, "--append-system-prompt", req.System)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	cmd := exec.CommandContext(ctx, p.bin(), args...)
	if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" {
		if tok, err := secrets.Resolve(claudeOAuthTokenSecret); err == nil && tok != "" {
			cmd.Env = append(cmd.Environ(), "CLAUDE_CODE_OAUTH_TOKEN="+tok)
		}
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		if msg := claudeErrorMessage(out.Bytes()); msg != "" {
			return CompletionResponse{}, fmt.Errorf("claude cli: %v: %s", err, truncate(msg, 300))
		}
		return CompletionResponse{}, fmt.Errorf("claude cli: %v: %s", err, truncate(strings.TrimSpace(errb.String()), 300))
	}

	var env struct {
		Result       string  `json:"result"`
		TotalCostUSD float64 `json:"total_cost_usd"`
		Usage        struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	text := out.String()
	if err := json.Unmarshal(out.Bytes(), &env); err == nil && env.Result != "" {
		cost := env.TotalCostUSD
		if cost == 0 {
			cost = costUSD(req.Model, env.Usage.InputTokens, env.Usage.OutputTokens)
		}
		return CompletionResponse{
			Text:    env.Result,
			Tokens:  tokens(env.Usage.InputTokens, env.Usage.OutputTokens),
			CostUSD: cost,
		}, nil
	}
	// Fallback: treat raw stdout as the text (e.g. --output-format text).
	return CompletionResponse{Text: text}, nil
}

func claudeErrorMessage(data []byte) string {
	var env struct {
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
	}
	if err := json.Unmarshal(data, &env); err == nil && env.IsError {
		return env.Result
	}
	return ""
}
