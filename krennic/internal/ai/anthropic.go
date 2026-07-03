package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AnthropicProvider calls the Claude Messages API.
type AnthropicProvider struct {
	APIKey  string
	BaseURL string
	Client  *http.Client
}

// NewAnthropic builds an Anthropic provider.
func NewAnthropic(apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		APIKey:  apiKey,
		BaseURL: "https://api.anthropic.com",
		Client:  &http.Client{Timeout: 90 * time.Second},
	}
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	body := map[string]any{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"system":     req.System,
		"messages": []map[string]any{
			{"role": "user", "content": req.User},
		},
	}
	buf, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return CompletionResponse{}, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", p.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return CompletionResponse{}, fmt.Errorf("anthropic %d: %s", resp.StatusCode, truncate(string(data), 300))
	}

	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return CompletionResponse{}, fmt.Errorf("decode anthropic response: %w", err)
	}
	var text string
	for _, c := range out.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	return CompletionResponse{
		Text:    text,
		Tokens:  tokens(out.Usage.InputTokens, out.Usage.OutputTokens),
		CostUSD: costUSD(req.Model, out.Usage.InputTokens, out.Usage.OutputTokens),
	}, nil
}
