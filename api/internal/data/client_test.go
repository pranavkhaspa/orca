package data

import (
	"net/http"
	"testing"
	"time"
)

func TestBreakerTripsAndClears(t *testing.T) {
	// The breaker is what stops one rate limit becoming an outage, so the
	// conditions under which it does and does not engage are worth pinning. An
	// earlier version evicted a host whose cool-off had not been set yet,
	// because a zero time reads as expired — and since getJSON consults the
	// breaker before every attempt, that reset the failure count each time and
	// the breaker could never reach its threshold. It passed a smoke test and
	// did nothing at all.
	if BreakerOpen("example.invalid") {
		t.Fatal("breaker should start closed")
	}
	tripBreaker("example.invalid", &UpstreamError{Host: "example.invalid", Status: http.StatusTooManyRequests})
	if BreakerOpen("example.invalid") {
		t.Fatal("one rate limit should not silence a host that may just be unlucky")
	}
	// Consulting it again must not reset the count.
	_ = BreakerOpen("example.invalid")
	tripBreaker("example.invalid", &UpstreamError{Host: "example.invalid", Status: http.StatusTooManyRequests})
	if !BreakerOpen("example.invalid") {
		t.Fatal("breaker should be open after two rate limits")
	}
	clearBreaker("example.invalid")
	if BreakerOpen("example.invalid") {
		t.Fatal("breaker should close after a success")
	}

	// A 500 is retried, not avoided: silencing a host for a dropped packet
	// would degrade every provider we have over a transient blip.
	for i := 0; i < 3; i++ {
		tripBreaker("flaky.invalid", &UpstreamError{Host: "flaky.invalid", Status: http.StatusInternalServerError})
	}
	if BreakerOpen("flaky.invalid") {
		t.Fatal("a 500 is transient and must not open the breaker")
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("7"); got != 7*time.Second {
		t.Errorf("seconds form: got %v", got)
	}
	if got := parseRetryAfter(""); got != 0 {
		t.Errorf("absent header: got %v", got)
	}
	if got := parseRetryAfter("garbage"); got != 0 {
		t.Errorf("unparseable header: got %v", got)
	}
	future := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got < 80*time.Second || got > 95*time.Second {
		t.Errorf("date form: got %v", got)
	}
}

func TestUpstreamErrorReportsRateLimit(t *testing.T) {
	var err error = &UpstreamError{Host: "h", Status: http.StatusTooManyRequests}
	if !err.(*UpstreamError).RateLimited() {
		t.Error("a 429 must be reported as a rate limit so callers can try another provider")
	}
	if (&UpstreamError{Host: "h", Breaker: true}).RateLimited() != true {
		t.Error("a breaker refusal is a rate limit for the same reason")
	}
	if (&UpstreamError{Host: "h", Status: http.StatusInternalServerError}).RateLimited() {
		t.Error("a 500 is not a rate limit; trying a second provider is not the answer")
	}
}

func TestBackoffGrowsAndRespectsHint(t *testing.T) {
	// The curve is exponential under a jitter band, so the ranges overlap
	// between adjacent attempts — what is asserted is the band for each, which
	// is what actually bounds how long a user waits.
	if d := backoffFor(1, 0); d < 200*time.Millisecond || d >= 600*time.Millisecond {
		t.Errorf("first backoff out of range: %v", d)
	}
	if d := backoffFor(3, 0); d < 800*time.Millisecond || d >= 2400*time.Millisecond {
		t.Errorf("third backoff out of range: %v", d)
	}
	if got := backoffFor(3, 9*time.Second); got != 9*time.Second {
		t.Errorf("a Retry-After hint must win over our own curve: %v", got)
	}
	if got := backoffFor(1, time.Hour); got != 30*time.Second {
		t.Errorf("a hint beyond the cap must be clamped, not obeyed: %v", got)
	}
}
