package data

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"orca/internal/domain"
)

// ---- In-memory TTL cache --------------------------------------------------

// Cache is a small TTL cache. Its job is to collapse a burst of identical agent
// calls within a single conversation turn, and to keep repeat demo questions
// from hammering a free upstream.
type Cache struct {
	mu   sync.Mutex
	ttl  time.Duration
	data map[string]entry
}

type entry struct {
	val     any
	failure error
	expires time.Time
}

func NewCache(ttl time.Duration) *Cache {
	return &Cache{ttl: ttl, data: make(map[string]entry)}
}

// Get returns a cached value and whether it was present and unexpired.
//
// A remembered failure is never returned as a value. That distinction is the
// whole reason failures are cached as errors and not as empty structs: an
// earlier version cached the zero-valued observation under the same key, and a
// later request found it, treated it as a hit, and published a Go verdict
// computed from measured wind and wave heights of exactly zero. A cache miss is
// recoverable. A cache hit that lies is not.
func (c *Cache) Get(k string) (any, bool) {
	v, ok, _ := c.get(k)
	return v, ok
}

// GetFailure returns a remembered failure for a key, if one is still fresh.
func (c *Cache) GetFailure(k string) (error, bool) {
	_, _, err := c.get(k)
	return err, err != nil
}

// Put stores a value with the cache's configured TTL.
func (c *Cache) Put(k string, v any) { c.put(k, entry{val: v}, c.ttl) }

// PutFailure remembers that k could not be resolved, for staleTTL.
//
// It exists because of a specific failure: a rate-limited upstream. A
// success-only cache means every subsequent request re-hits an upstream that
// has already refused, each adding load, which is how a momentary throttle
// becomes a sustained outage. Remembering the refusal for a few minutes costs
// one honest error and stops the hammering.
//
// Caching the error rather than an empty result is deliberate. Both stop the
// retry storm, but only one keeps the failure distinguishable from a
// measurement, and the trace has to be able to say "rate limited" instead of
// quoting a zero.
func (c *Cache) PutFailure(k string, err error) {
	if err == nil {
		return
	}
	c.put(k, entry{failure: err}, staleTTL)
}

// staleTTL is how long a failure is remembered. Long enough to cover a
// conversation, short enough that a recovered upstream is used almost
// immediately.
const staleTTL = 3 * time.Minute

// get returns the value (if any), whether a usable value is present, and any
// remembered failure. Exactly one of the first two results is ever non-nil.
func (c *Cache) get(k string) (any, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.data[k]
	if !ok || time.Now().After(e.expires) {
		if ok {
			delete(c.data, k)
		}
		return nil, false, nil
	}
	if e.failure != nil {
		return nil, false, e.failure
	}
	return e.val, true, nil
}

func (c *Cache) put(k string, e entry, ttl time.Duration) {
	e.expires = time.Now().Add(ttl)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[k] = e
}

// Stats exposes cache effectiveness for the health endpoint.
func (c *Cache) Stats() (entries int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.data)
}

// ---- Snapshot fallback ----------------------------------------------------

// Snapshot is the offline fallback. It is committed to the repository and baked
// into the binary, so the system still answers when every upstream is
// unreachable — with every figure labelled as snapshot data and its age shown.
//
// This is deliberately a first-class feature rather than a nicety: the demo is
// the deliverable, and a dead API must not produce a dead demo.
type Snapshot struct {
	GeneratedAt time.Time                `json:"generated_at"`
	Places      map[string]SnapshotPlace `json:"places"`
}

type SnapshotPlace struct {
	Name    string         `json:"name"`
	Lat     float64        `json:"lat"`
	Lon     float64        `json:"lon"`
	WayLat  float64        `json:"way_lat"`
	WayLon  float64        `json:"way_lon"`
	Marine  domain.Marine  `json:"marine"`
	Weather domain.Weather `json:"weather"`
	// Provenance is stored separately from the observation structs because
	// those hide their citations from the wire format. The baked file is the one
	// place we want them serialised, so it gets its own fields.
	MarineProv  domain.Citations `json:"marine_prov"`
	WeatherProv domain.Citations `json:"weather_prov"`
	Source      string           `json:"source"`
}

var (
	snapOnce sync.Once
	snap     Snapshot
	snapErr  error
)

// LoadSnapshot reads and caches the snapshot file.
func LoadSnapshot(path string) (Snapshot, error) {
	snapOnce.Do(func() {
		b, err := readData(path)
		if err != nil {
			snapErr = fmt.Errorf("read snapshot: %w", err)
			return
		}
		if err := json.Unmarshal(b, &snap); err != nil {
			snapErr = fmt.Errorf("parse snapshot: %w", err)
			return
		}
		// Every snapshot figure is marked non-live at load time. This is done
		// centrally so no code path can forget to do it.
		stamp := snap.GeneratedAt
		for k, p := range snap.Places {
			for ds := range p.MarineProv {
				c := p.MarineProv[ds]
				c.Live, c.Retrieved, c.Err = false, stamp, ""
				p.MarineProv[ds] = c
			}
			for ds := range p.WeatherProv {
				c := p.WeatherProv[ds]
				c.Live, c.Retrieved, c.Err = false, stamp, ""
				p.WeatherProv[ds] = c
			}
			// Re-attach provenance to the observations so downstream consumers
			// see citations the same way they do for live data.
			p.Marine.Citations = p.MarineProv
			p.Weather.Citations = p.WeatherProv
			snap.Places[k] = p
		}
	})
	return snap, snapErr
}

// SnapshotFor returns the snapshot entry for a place name, falling back to the
// nearest available entry by great-circle distance when the exact place is
// absent. Returning the nearest rather than failing is a deliberate choice:
// a snapshot from 80 km away is materially more useful to a fisherman than a
// refusal, provided it is labelled.
func SnapshotFor(name string, lat, lon float64, path string) (SnapshotPlace, bool) {
	s, err := LoadSnapshot(path)
	if err != nil {
		return SnapshotPlace{}, false
	}
	if p, ok := s.Places[lower(name)]; ok {
		return p, true
	}
	best, bestD := SnapshotPlace{}, 1e9
	for _, p := range s.Places {
		if d := haversineKm(lat, lon, p.Lat, p.Lon); d < bestD {
			best, bestD = p, d
		}
	}
	// 400 km is generous on purpose: along a coastline the sea state is far more
	// spatially correlated than the town names are, so a nearby coastal point is
	// a reasonable stand-in. The UI still shows the substitution.
	if bestD > 400 {
		return SnapshotPlace{}, false
	}
	return best, true
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}

// SnapshotPlaces lists the places the baked snapshot can answer for, sorted.
// The evaluation corpus is built from this list so that a case can never
// reference a location the offline path could not serve — such a case would
// measure the corpus, not the system.
func SnapshotPlaces(path string) ([]string, error) {
	s, err := LoadSnapshot(path)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(s.Places))
	for k := range s.Places {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// expiry reports when k's entry lapses, for tests that need to prove a deadline
// was not moved. It is unexported so production code cannot depend on it.
func (c *Cache) expiry(k string) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.data[k].expires
}
