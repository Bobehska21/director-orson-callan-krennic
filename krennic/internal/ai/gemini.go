package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/acme/krennic/internal/model"
)

// GeminiProvider calls the Google Gemini generateContent API.
type GeminiProvider struct {
	APIKey  string
	BaseURL string
	Client  *http.Client
}

// NewGemini builds a Gemini provider.
func NewGemini(apiKey string) *GeminiProvider {
	return &GeminiProvider{
		APIKey:  apiKey,
		BaseURL: "https://generativelanguage.googleapis.com",
		Client:  &http.Client{Timeout: 90 * time.Second},
	}
}

func (p *GeminiProvider) Name() string { return "gemini" }

func (p *GeminiProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	genCfg := map[string]any{"maxOutputTokens": req.MaxTokens}
	if req.JSONOutput {
		genCfg["responseMimeType"] = "application/json"
	}
	body := map[string]any{
		"systemInstruction": map[string]any{"parts": []map[string]string{{"text": req.System}}},
		"contents":          []map[string]any{{"parts": []map[string]string{{"text": req.User}}}},
		"generationConfig":  genCfg,
	}
	buf, _ := json.Marshal(body)
	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s",
		p.BaseURL, url.PathEscape(req.Model), url.QueryEscape(p.APIKey))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return CompletionResponse{}, err
	}
	httpReq.Header.Set("content-type", "application/json")

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return CompletionResponse{}, fmt.Errorf("gemini %d: %s", resp.StatusCode, truncate(string(data), 300))
	}

	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return CompletionResponse{}, fmt.Errorf("decode gemini response: %w", err)
	}
	var text string
	if len(out.Candidates) > 0 {
		for _, part := range out.Candidates[0].Content.Parts {
			text += part.Text
		}
	}
	return CompletionResponse{
		Text:    text,
		Tokens:  tokens(out.UsageMetadata.PromptTokenCount, out.UsageMetadata.CandidatesTokenCount),
		CostUSD: costUSD(req.Model, out.UsageMetadata.PromptTokenCount, out.UsageMetadata.CandidatesTokenCount),
	}, nil
}

func tokens(in, out int) model.TokenUsage { return model.TokenUsage{In: in, Out: out} }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
