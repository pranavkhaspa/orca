package data

import (
	"errors"
	"testing"
	"time"

	"orca/internal/domain"
)

// entryFor stands in for the agents package's marineEntry and weatherEntry. The
// cache is generic over its values, so what matters is only that it is a struct
// carrying an observation that could legitimately read zero.
type entryFor struct {
	m domain.Marine
	w domain.Weather
	c domain.Citations
}

// A cached failure must never be readable as a value. An earlier version of the
// cache stored the zero-valued observation under the ordinary key, so a later
// request found it, saw a hit, and published a verdict derived from wind and
// wave heights of exactly zero. The refusal is now cached as an error, and these
// tests hold that line.
func TestCacheFailureIsNeverAValue(t *testing.T) {
	c := NewCache(time.Minute)
	const k = "mar:1,2:6"

	// A measurement that legitimately reads zero, so the test is not simply
	// asserting that zeros cannot be stored.
	c.Put(k, entryFor{m: domain.Marine{WaveHeightM: 0}, c: domain.Citations{"open-meteo": {}}})
	v, hit := c.Get(k)
	if !hit {
		t.Fatal("a real measurement of zero was dropped from the cache")
	}
	if got := v.(entryFor).m.WaveHeightM; got != 0 {
		t.Fatalf("real zero measurement was altered: %v", got)
	}

	// Now the failing case: the upstream refused, and the caller cached the
	// refusal. The value read back must be a miss, never a zeroed struct.
	c.PutFailure(k, errors.New("upstream returned 429"))
	if v, hit := c.Get(k); hit {
		t.Fatalf("a remembered failure was served as a value: %+v", v)
	}
	if _, remembered := c.GetFailure(k); !remembered {
		t.Fatal("the failure was not remembered")
	}
}

// A real measurement must not be mistaken for a remembered failure, or the
// retrieval path would skip live data and answer from a stale refusal.
func TestCacheValueIsNotAFailure(t *testing.T) {
	c := NewCache(time.Minute)
	const k = "wx:3,4:6"
	c.Put(k, entryFor{w: domain.Weather{WindKmh: 12}})

	if _, remembered := c.GetFailure(k); remembered {
		t.Fatal("a cached value was reported as a failure")
	}
	v, hit := c.Get(k)
	if !hit || v.(entryFor).w.WindKmh != 12 {
		t.Fatalf("cached value not returned: %+v %v", v, hit)
	}
}

// PutFailure with a nil error is a no-op. Otherwise a caller that treats a
// successful fetch as a failure would poison its own cache key.
func TestPutFailureIgnoresNil(t *testing.T) {
	c := NewCache(time.Minute)
	c.PutFailure("k", nil)
	if _, remembered := c.GetFailure("k"); remembered {
		t.Fatal("a nil error was cached as a failure")
	}
	if _, hit := c.Get("k"); hit {
		t.Fatal("a nil error produced a cache hit")
	}
}

// A failure must expire. The whole point of the short TTL is that a recovered
// upstream is picked up almost immediately, so a permanent one would mean never
// recovering at all.
func TestCacheFailureExpires(t *testing.T) {
	if staleTTL > 15*time.Minute {
		t.Fatalf("failure TTL of %s is long enough to look like a permanent outage", staleTTL)
	}
	if staleTTL >= time.Hour {
		t.Fatal("failure TTL must be shorter than a normal entry, or a failure " +
			"outlives the data it replaced")
	}

	c := NewCache(time.Hour)
	c.PutFailure("k", errors.New("429"))

	// Force expiry rather than sleeping for it.
	c.mu.Lock()
	e := c.data["k"]
	e.expires = time.Now().Add(-time.Second)
	c.data["k"] = e
	c.mu.Unlock()

	if _, remembered := c.GetFailure("k"); remembered {
		t.Fatal("an expired failure was still returned")
	}
}

// A failure must be overwritten by a later success, otherwise a single throttle
// would suppress live data for the rest of the TTL.
func TestSuccessOverwritesFailure(t *testing.T) {
	c := NewCache(time.Minute)
	const k = "wx:5,6:6"

	c.PutFailure(k, errors.New("429"))
	c.Put(k, entryFor{w: domain.Weather{WindKmh: 9}})

	v, hit := c.Get(k)
	if !hit {
		t.Fatal("a later success did not clear the remembered failure")
	}
	if v.(entryFor).w.WindKmh != 9 {
		t.Fatalf("wrong value after recovery: %+v", v)
	}
	if _, remembered := c.GetFailure(k); remembered {
		t.Fatal("failure still remembered after a successful refetch")
	}
}

// A remembered failure must expire on its original schedule.
//
// PutFailure always starts a fresh staleTTL, so any caller that writes back a
// failure it merely read has pushed the expiry forward. The ocean agent's search
// did exactly that, and because every question re-read and re-wrote the same
// keys, a single rate limit became a permanent refusal: the entry's deadline
// moved with each request and never arrived. This pins the contract at the cache
// so the mistake is caught here rather than as a silent outage weeks later.
func TestRememberedFailureKeepsItsOriginalExpiry(t *testing.T) {
	c := NewCache(5 * time.Minute)
	key := "cell:off:0"
	boom := errors.New("upstream 429")

	c.PutFailure(key, boom)
	first := c.expiry(key)

	// Simulate the agent reading the failure and writing it back unchanged.
	if _, remembered := c.GetFailure(key); !remembered {
		t.Fatal("expected the failure to be remembered")
	}

	if second := c.expiry(key); !second.Equal(first) {
		t.Errorf("reading a failure must not extend it: expiry moved from %v to %v", first, second)
	}
	if second := c.expiry(key); !second.Before(time.Now().Add(staleTTL)) {
		t.Errorf("expiry %v should still be inside the original %v window", second, staleTTL)
	}
}

// And a genuinely new failure is still recorded, so the guard above cannot be
// satisfied by simply never writing a failure.
func TestFreshFailureIsStillRecorded(t *testing.T) {
	c := NewCache(5 * time.Minute)
	c.PutFailure("cell:off:1", errors.New("upstream 503"))
	if _, ok := c.GetFailure("cell:off:1"); !ok {
		t.Error("a new failure must be remembered")
	}
	if _, ok := c.Get("cell:off:1"); ok {
		t.Error("a failure must not be readable as a value")
	}
}
