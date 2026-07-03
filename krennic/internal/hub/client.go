package hub

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client delivers reports from an agent to the central hub.
type Client struct {
	URL    string
	Token  string
	http   *http.Client
}

// NewClient builds a hub client. url is the hub base (e.g. http://hub:8787).
func NewClient(url, token string) *Client {
	return &Client{
		URL:   strings.TrimRight(url, "/"),
		Token: token,
		http:  &http.Client{Timeout: 15 * time.Second},
	}
}

// Send posts a pre-serialized report payload to the hub.
func (c *Client) Send(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/api/report", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("hub %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
