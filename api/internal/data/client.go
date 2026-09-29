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
	mathrand "math/rand"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const ist = "Asia/Kolkata"

// userAgent identifies the caller. MET Norway's terms require a real contact
// address in the User-Agent and refuse clients that omit one, so the fallback
// provider is the reason this is a variable rather than a literal.
const userAgent = "ORCA-Marine/1.0 (SIH26176; educational marine safety prototype)"

var client = &http.Client{Timeout: 20 * time.Second}

// UpstreamError is a failure attributed to a specific host, carrying the HTTP
// status when there was one. The distinction it preserves is the one the UI
// needs: "the upstream rate-limited us" and "we could not reach the network" are
// different sentences, and flattening both to a string loses the reason a
// deployment is degraded.
type UpstreamError struct {
	Host   string
	Status int
	Err    error
	// Breaker is set when the request was refused without being sent, because
	// the host was already in its cool-off.
	Breaker bool
}

func (e *UpstreamError) Error() string {
	if e.Breaker {
		return fmt.Sprintf("upstream %s: cooling down after rate limit", e.Host)
	}
	if e.Err == nil {
		return fmt.Sprintf("upstream %s: status %d", e.Host, e.Status)
	}
	return e.Err.Error()
}

func (e *UpstreamError) Unwrap() error { return e.Err }

// RateLimited reports whether the failure was a rate limit rather than anything
// else, so callers can prefer a different provider over giving up.
func (e *UpstreamError) RateLimited() bool {
	return e.Status == http.StatusTooManyRequests || e.Breaker
}

const maxAttempts = 3

// backoffFor returns the wait before the given attempt, growing exponentially
// from 400 ms and jittered over half the interval. When the upstream told us
// how long to wait, that wins — it knows its own quota window and our guess is
// usually shorter.
func backoffFor(attempt int, hint time.Duration) time.Duration {
	if hint > 0 {
		if hint > 30*time.Second {
			hint = 30 * time.Second
		}
		return hint
	}
	base := 400 * time.Millisecond << (attempt - 1)
	if base > 4*time.Second {
		base = 4 * time.Second
	}
	// rand.Int63n needs a positive bound; base/2 is at least 200ms so this
	// cannot panic, but the guard is here so a future change to base cannot make
	// it a runtime failure instead of a wrong number.
	half := int64(base / 2)
	if half < 1 {
		return base
	}
	return base/2 + time.Duration(mathrand.Int63n(half))
}

// ---- Per-host circuit breaker ----------------------------------------------
//
// The breaker is what stops one rate limit from becoming an outage. Open-Meteo's
// free tier is generous per minute and strict per day, and a conversation makes
// several distinct upstream calls; when the day quota is spent, every subsequent
// call would be refused individually. Tripping the breaker turns that queue into
// a single wait: calls during the cool-off fail immediately and the caller falls
// through to cache or snapshot without adding load to an upstream that has
// already said no.

type breakerState struct {
	until time.Time
	// failures is the count that must be reached before the host is considered
	// rate-limited rather than merely unlucky. A single 503 on a healthy host
	// should not silence it for a minute.
	failures int
}

var (
	breakerMu sync.Mutex
	breakers  = map[string]*breakerState{}
)

// breakAfter is how many consecutive retryable failures trip the breaker. Two,
// not one: a single network blip is common, and opening on it would degrade
// every provider we have over a dropped packet.
const breakAfter = 2

// breakFor is the cool-off. Long enough to matter for a per-minute limit, short
// enough that a conversation spanning it still recovers on its own.
const breakFor = 45 * time.Second

func breakerOpen(host string) (time.Time, bool) {
	breakerMu.Lock()
	defer breakerMu.Unlock()
	s, ok := breakers[host]
	if !ok {
		return time.Time{}, false
	}
	// `until` is only set once the host has actually tripped. A host with a
	// recorded-but-not-yet-sufficient failure count has a zero `until`, and
	// time.Now().After(zero) is true — so the expiry test below would evict it,
	// resetting the count on every check. Since getJSON consults the breaker
	// before each attempt, that made it impossible to ever reach the threshold:
	// the breaker counted nothing and the 429 cascade it exists to stop was
	// fully live. Testing against the zero value is what distinguishes "has not
	// tripped yet" from "tripped and the cool-off has expired".
	if !s.until.IsZero() && time.Now().After(s.until) {
		delete(breakers, host)
		return time.Time{}, false
	}
	return s.until, !s.until.IsZero()
}

func tripBreaker(host string, err error) {
	var ue *UpstreamError
	status := 0
	if errors.As(err, &ue) {
		status = ue.Status
	}
	// Only throttling trips the breaker. A 500 or a dropped packet is retried,
	// but the host is not silenced: those are usually transient and a host that
	// is genuinely broken will fail again anyway.
	if status != http.StatusTooManyRequests && status != http.StatusServiceUnavailable {
		return
	}
	breakerMu.Lock()
	defer breakerMu.Unlock()
	s, ok := breakers[host]
	if !ok {
		s = &breakerState{}
		breakers[host] = s
	}
	s.failures++
	if s.failures >= breakAfter {
		s.until = time.Now().Add(breakFor)
		s.failures = 0
	}
}

func clearBreaker(host string) {
	breakerMu.Lock()
	defer breakerMu.Unlock()
	delete(breakers, host)
}

// BreakerOpen reports whether an upstream host is currently in cool-off. It
// exists for the health endpoint, so the reason a deployment is degraded is
// visible from outside rather than only in the citation trail.
func BreakerOpen(host string) bool {
	_, ok := breakerOpen(host)
	return ok
}

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
//
// Rate limiting is handled here rather than at the call sites, because the
// failure is a property of the upstream and every call site would otherwise
// rediscover it. Three things happen on a 429 or a 5xx:
//
//   - the request is retried with exponential backoff, honouring Retry-After,
//     because a free tier's limit is usually a short window rather than a
//     refusal;
//   - the host is put in a circuit-break for a cool-off period, so one rate
//     limit does not turn into a queue of them. This is the part that matters
//     in production. Open-Meteo's free tier answers 429 to a burst, and without
//     the breaker every subsequent request in the conversation re-hit the same
//     exhausted quota, each adding load, so the outage prolonged itself. With
//     it, one 429 costs one wait and everything after it fails fast and falls
//     through to the cached or snapshot answer;
//   - the error is typed, so callers can tell "the upstream said no" apart from
//     "the network broke", which are different things to report to a user.
func getJSON(ctx context.Context, url string, out any) error {
	if offline {
		return errOffline
	}
	host := hostOf(url)
	if _, cooling := breakerOpen(host); cooling {
		return &UpstreamError{Host: host, Status: http.StatusTooManyRequests, Breaker: true}
	}
	var last error
	// retryAfterHint carries a Retry-After from the previous attempt into the
	// next backoff. It is a local, not a package variable: two concurrent
	// requests to the same host would otherwise read each other's hint and
	// retry against each other's backoff, and the race detector would rightly
	// complain. Only the breaker below is shared state, and it is mutex-guarded.
	var retryAfterHint time.Duration
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// Jitter matters more than the backoff curve here: without it every
			// concurrent request that was refused together retries together.
			d := backoffFor(attempt, retryAfterHint)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
			}
		}
		retryAfterHint = 0
		err, retryAfter := fetchOnce(ctx, url, out)
		if err == nil {
			clearBreaker(host)
			return nil
		}
		last = err
		if !retryableStatus(err) {
			return err
		}
		if retryAfter > 0 {
			retryAfterHint = retryAfter
		}
		tripBreaker(host, err)
	}
	return last
}

// retryableStatus reports whether another attempt could plausibly succeed.
func retryableStatus(err error) bool {
	var ue *UpstreamError
	if errors.As(err, &ue) {
		return ue.Status == http.StatusTooManyRequests ||
			ue.Status == http.StatusRequestTimeout ||
			ue.Status >= 500
	}
	// Transport errors are worth one more go; a malformed body is not, but it
	// arrives as a decode error and is retried once, which costs 200 ms.
	return true
}

// fetchOnce performs a single request and reports whether a retry is worthwhile.
func fetchOnce(ctx context.Context, url string, out any) (error, time.Duration) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err), 0
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return &UpstreamError{Host: hostOf(url), Status: 0, Err: err}, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		after := parseRetryAfter(resp.Header.Get("Retry-After"))
		return &UpstreamError{
			Host:   hostOf(url),
			Status: resp.StatusCode,
			Err:    fmt.Errorf("upstream %s: status %d", resp.Status, resp.StatusCode),
		}, after
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return &UpstreamError{Host: hostOf(url), Status: 0, Err: fmt.Errorf("read: %w", err)}, 0
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &UpstreamError{Host: hostOf(url), Status: 0, Err: err}, 0
	}
	return nil, 0
}

// parseRetryAfter understands both forms the header is allowed to take: a delay
// in seconds, or an HTTP date.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func hostOf(u string) string {
	h, err := neturl.Parse(u)
	if err != nil {
		return u
	}
	return h.Host
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
