// Package data is the outbound boundary of the system. Nothing outside this
// package performs network I/O.
//
// Every client here is keyless, free, and returns a domain.Citation alongside
// its data. On failure the caller receives an error *and* the citation records
// that failure, so degradation is visible rather than silent.
package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

const ist = "Asia/Kolkata"

var client = &http.Client{Timeout: 20 * time.Second}

// SetHTTPTimeout changes the per-request timeout on the shared client. It is
// called once at startup from configuration; the free upstreams occasionally
// take longer than the default on a cold cache, and on a free-tier host the
// operator needs to be able to trade latency for completeness without a
// rebuild.
func SetHTTPTimeout(d time.Duration) {
	if d <= 0 {
		return
	}
	client = &http.Client{Timeout: d}
}

// offline is the demo kill switch. When set, every outbound call fails
// immediately, which forces the system down its snapshot path.
//
// It exists for two reasons. It lets the resilience claim be demonstrated on
// stage rather than asserted — switching the service into forced-offline mode
// and watching it keep answering is a far better argument than a promise. And
// it makes the degraded path testable, so the guarantee that the system works
// without an LLM or a network is enforced by CI instead of by intention.
// Set from configuration at startup rather than read here, so that the
// environment is interpreted in exactly one place.
var offline bool

// SetOffline toggles the kill switch. Used by the offline test and exposed for
// future runtime control.
func SetOffline(v bool) { offline = v }

// Offline reports whether outbound calls are being suppressed.
func Offline() bool { return offline }

var errOffline = errors.New("offline mode: outbound requests suppressed")

// getJSON fetches a URL and decodes it, mapping any transport failure to error.
func getJSON(ctx context.Context, url string, out any) error {
	if offline {
		return errOffline
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "ORCA-Marine/1.0 (SIH26176; educational prototype)")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream %s: status %d", resp.Status, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	return json.Unmarshal(body, out)
}

// round2 keeps figures stable and readable in the UI.
func round2(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return math.Round(f*100) / 100
}

func round1(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return math.Round(f*10) / 10
}
