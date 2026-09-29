package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestClient points a Client at a test server.
func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New("test-key", 5*time.Second, "")
	c.baseURL = srv.URL
	return c
}

// TestExtractJSONObject covers the recovery of a JSON payload from a response
// that wraps it in prose or fences. Models do this constantly; without recovery
// the planner silently degrades to the router, which is a quality loss the user
// would never see.
func TestExtractJSONObject(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bare object", `{"a":1}`, `{"a":1}`},
		{"fenced", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"leading prose", `Sure! Here you go: {"a":1}`, `{"a":1}`},
		{"trailing prose", `{"a":1} Hope that helps!`, `{"a":1}`},
		{"nested object", `{"a":{"b":2},"c":3}`, `{"a":{"b":2},"c":3}`},
		{"brace inside string", `{"a":"} not the end"}`, `{"a":"} not the end"}`},
		{"escaped quote in string", `{"a":"say \"hi\""}`, `{"a":"say \"hi\""}`},
		// The contract is a single object, so the first opening brace wins. A
		// model that wraps its answer in an array still yields a usable object.
		{"wrapped in an array", `[{"a":1}]`, `{"a":1}`},
		{"first object of several wins", `{"first":1} {"second":2}`, `{"first":1}`},
		{"no object at all", `no json here`, `no json here`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractJSONObject(tc.in); got != tc.want {
				t.Errorf("extractJSONObject(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCompleteJSONParsesFencedResponse(t *testing.T) {
	// The fence is a backtick, which cannot appear inside a Go raw string, so
	// the response body is assembled from pieces.
	fence := strings.Repeat("`", 3)
	body := `{"choices":[{"message":{"role":"assistant","content":"` + fence +
		`json\n{\"place\":\"Kochi\"}\n` + fence + `"}}]}`

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization header = %q", got)
		}
		w.Write([]byte(body))
	})
	var out struct {
		Place string `json:"place"`
	}
	if err := c.CompleteJSON(context.Background(), "m", "sys", "user", &out); err != nil {
		t.Fatalf("CompleteJSON: %v", err)
	}
	if out.Place != "Kochi" {
		t.Errorf("place = %q, want Kochi", out.Place)
	}
}

// TestCompleteRetriesMalformedJSON confirms a single bad response is retried
// rather than abandoned, and that a persistent failure surfaces as an error the
// caller can fall back from.
func TestCompleteRetriesMalformedJSON(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Write([]byte(`{"choices":[{"message":{"content":"not json"}}]}`))
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"{\"place\":\"Goa\"}"}}]}`))
	})
	var out struct {
		Place string `json:"place"`
	}
	if err := c.CompleteJSON(context.Background(), "m", "sys", "user", &out); err != nil {
		t.Fatalf("CompleteJSON should have recovered on retry: %v", err)
	}
	if out.Place != "Goa" {
		t.Errorf("place = %q, want Goa", out.Place)
	}
	if calls != 2 {
		t.Errorf("expected exactly one retry, got %d calls", calls)
	}

	calls = 0
	c2 := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"choices":[{"message":{"content":"still not json"}}]}`))
	})
	if err := c2.CompleteJSON(context.Background(), "m", "sys", "user", &out); err == nil {
		t.Error("a persistently malformed response must surface as an error")
	}
	if calls != 2 {
		t.Errorf("expected 2 attempts before giving up, got %d", calls)
	}
}

func TestCompleteReportsUpstreamErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"rate limited", http.StatusTooManyRequests, `{"error":{"message":"Rate limit exceeded"}}`, "429"},
		{"unauthorized", http.StatusUnauthorized, `{"error":{"message":"No auth credentials found"}}`, "401"},
		{"server error", http.StatusInternalServerError, `boom`, "500"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			})
			_, err := c.Complete(context.Background(), "m", "s", "u", false)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention status %s", err, tc.wantErr)
			}
		})
	}
}

func TestCompleteRejectsEmptyChoices(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[]}`))
	})
	if _, err := c.Complete(context.Background(), "m", "s", "u", false); err == nil {
		t.Error("a response with no choices must be an error, not an empty string")
	}
}

func TestUnavailableWithoutKey(t *testing.T) {
	c := New("", time.Second, "")
	if c.Available() {
		t.Error("a client with no key must report unavailable")
	}
	if _, err := c.Complete(context.Background(), "m", "s", "u", false); err == nil {
		t.Error("a call with no key must fail cleanly rather than panic or hang")
	}
	// Whitespace is not a key.
	if New("   ", time.Second, "").Available() {
		t.Error("whitespace must not count as a configured key")
	}
}

func TestCompleteSendsRequestedShape(t *testing.T) {
	var body string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	})
	if _, err := c.Complete(context.Background(), "some/model", "sys", "user", true); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"model":"some/model"`, `"response_format"`, `"type":"json_object"`} {
		if !strings.Contains(body, want) {
			t.Errorf("request body missing %s: %s", want, body)
		}
	}
}
