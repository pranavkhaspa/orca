// Package llm is the only package permitted to call a language model.
//
// Every call is optional by construction: callers must handle an error, and the
// system is required to produce a complete answer without any of them
// succeeding.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to OpenRouter's OpenAI-compatible chat completions endpoint.
type Client struct {
	key     string
	baseURL string
	http    *http.Client
}

// New builds a client. base is the API root without the path; it is
// configurable so the same code can be pointed at a proxy or a second
// OpenAI-compatible provider without a code change.
func New(key string, timeout time.Duration, base string) *Client {
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	// Trim the key. A trailing newline is a routine accident when a key is
	// written by a shell script, and without trimming it every request would
	// fail authentication at the provider while the system believed a model was
	// configured — silently costing latency on a fallback it would otherwise
	// have used immediately.
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	if !strings.HasSuffix(base, "/chat/completions") {
		base += "/chat/completions"
	}
	return &Client{
		key:     strings.TrimSpace(key),
		baseURL: base,
		http:    &http.Client{Timeout: timeout},
	}
}

// Available reports whether a key is configured.
func (c *Client) Available() bool { return c != nil && c.key != "" }

type chatReq struct {
	Model       string       `json:"model"`
	Messages    []chatMsg    `json:"messages"`
	Temperature float64      `json:"temperature"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	ResponseFmt *responseFmt `json:"response_format,omitempty"`
}

type responseFmt struct {
	Type string `json:"type"`
}

type chatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResp struct {
	Choices []struct {
		Message chatMsg `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete performs a single chat completion and returns the text content.
func (c *Client) Complete(ctx context.Context, model, system, user string, jsonMode bool) (string, error) {
	if !c.Available() {
		return "", fmt.Errorf("no API key configured")
	}
	req := chatReq{
		Model:       model,
		Messages:    []chatMsg{{Role: "system", Content: system}, {Role: "user", Content: user}},
		Temperature: 0.2,
		MaxTokens:   700,
	}
	if jsonMode {
		req.ResponseFmt = &responseFmt{Type: "json_object"}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.key)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("HTTP-Referer", "https://github.com/orca-marine")
	httpReq.Header.Set("X-Title", "ORCA Marine Intelligence")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("call model: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("model status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var out chatResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if out.Error != nil && out.Error.Message != "" {
		return "", fmt.Errorf("model error: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("model returned no choices")
	}
	return out.Choices[0].Message.Content, nil
}

// CompleteJSON performs a completion constrained to a JSON object and unmarshals
// it. Retries once, because a single malformed response is a transient
// formatting failure rather than a reason to abandon the query.
func (c *Client) CompleteJSON(ctx context.Context, model, system, user string, out any) error {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		txt, err := c.Complete(ctx, model, system, user, true)
		if err != nil {
			lastErr = err
			continue
		}
		if err := json.Unmarshal([]byte(extractJSONObject(txt)), out); err != nil {
			lastErr = fmt.Errorf("decode json: %w", err)
			continue
		}
		return nil
	}
	return lastErr
}

// extractJSONObject recovers the first balanced JSON object from a response,
// tolerating models that wrap output in prose or code fences.
func extractJSONObject(s string) string {
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '{' {
			start = i
			break
		}
	}
	if start < 0 {
		return s
	}
	depth := 0
	inStr, esc := false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
