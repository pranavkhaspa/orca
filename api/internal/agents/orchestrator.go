// Package agents implements the ORCA multi-agent runtime.
//
// Agents are deliberately shaped like actors: a typed input, a typed output, an
// identity in the visible trace, and independent failure. They fan out in
// parallel and the synthesis step is deterministic (see engine). The language
// model appears in exactly two places, Planner and Narrator, and neither is
// permitted to influence the safety verdict.
package agents

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"orca/internal/config"
	"orca/internal/data"
	"orca/internal/domain"
	"orca/internal/engine"
	"orca/internal/lang"
	"orca/internal/llm"
)

// runState is the mutable state of a single query, shared across goroutines and
// guarded by its mutex.
type runState struct {
	mu      sync.Mutex
	trace   []domain.AgentRun
	llmNote string
	now     time.Time
}

func (r *runState) start(name, label string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace = append(r.trace, domain.AgentRun{
		Name: name, Label: label, Status: domain.StatusRunning, Started: r.now,
	})
	return len(r.trace) - 1
}

func (r *runState) finish(i int, status, summary string, sources []domain.Citation, errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i < 0 || i >= len(r.trace) {
		return
	}
	r.trace[i].Status = status
	r.trace[i].Summary = summary
	r.trace[i].Sources = sources
	r.trace[i].Ended = time.Now()
	if errMsg != "" {
		r.trace[i].Err = errMsg
	}
}

func (r *runState) snapshot() []domain.AgentRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.AgentRun, len(r.trace))
	copy(out, r.trace)
	return out
}

// Event is a progressive update streamed to the client so the reasoning is
// visible while it happens rather than appearing all at once at the end.
type Event struct {
	Type       string           `json:"type"` // agent | answer | done
	Run        *domain.AgentRun `json:"run,omitempty"`
	Answer     string           `json:"answer,omitempty"`
	AnswerLang string           `json:"answer_lang,omitempty"`
	Final      *domain.Findings `json:"final,omitempty"`
}

// Orchestrator runs the agent graph for one query.
type Orchestrator struct {
	cfg   config.Config
	llm   *llm.Client
	cache *data.Cache
}

// New builds an orchestrator.
func New(cfg config.Config, c *llm.Client, cache *data.Cache) *Orchestrator {
	return &Orchestrator{cfg: cfg, llm: c, cache: cache}
}

// CacheEntries reports how many upstream responses are currently cached, for the
// health endpoint.
func (o *Orchestrator) CacheEntries() int {
	if o.cache == nil {
		return 0
	}
	return o.cache.Stats()
}

// Ask executes the full pipeline and returns the complete findings.
//
// The emit callback is optional and receives progressive events. It must not
// block: the HTTP layer writes to a buffered channel.
func (o *Orchestrator) Ask(ctx context.Context, query string, emit func(Event)) domain.Findings {
	if emit == nil {
		emit = func(Event) {}
	}
	rs := &runState{now: time.Now()}
	f := domain.Findings{}
	detected := lang.Detect(query)

	emitAgent := func(i int, name, label string) { emit(agentEvent(rs.snapshot(), i)) }

	// ---- 1. Plan -----------------------------------------------------------
	pi := rs.start("planner", "Planner")
	planner := &PlannerAgent{cfg: o.cfg, llm: o.llm, run: rs, coast: o.cfg.CoastalPath}
	plan := planner.Plan(ctx, query, detected)
	rs.finish(pi, domain.StatusDone,
		fmt.Sprintf("Resolved %q · intent=%s · window=%dh · language=%s (via %s)",
			orNone(plan.Place), plan.Intent, plan.WindowHours, plan.Lang, orModel(rs.llmNote)),
		nil, "")
	emitAgent(pi, "planner", "Planner")

	// Without a resolvable place we cannot produce marine observations, so ask
	// for clarification rather than inventing a location.
	if plan.Place == "" {
		f.Plan = plan
		f.Trace = rs.snapshot()
		f.AnswerLang = string(detected)
		f.Answer = clarify(detected)
		// The clarification has to be emitted on the stream, not merely returned.
		// This path returns before the usual emit, so the client used to receive
		// the planner event and then nothing at all: the browser stayed on its
		// empty state and a user who had asked an unanswerable question was
		// shown silence in place of the sentence explaining what was missing.
		// The same bug hid the clarification from the streaming path while
		// /api/ask returned it correctly, which is exactly the kind of
		// disagreement between two views of one result that is easy to ship.
		emit(Event{Type: "answer", Answer: f.Answer, AnswerLang: f.AnswerLang})
		emit(Event{Type: "done", Final: &f})
		// "end" is deliberately not emitted here. The HTTP layer writes it when
		// the event channel closes, on every path, so emitting it from the
		// orchestrator too would produce two.
		return f
	}
	f.Plan = plan

	// ---- 2. Geo (must precede marine: it produces the offshore waypoint) ---
	gi := rs.start("geo", "Geo Resolution")
	geo := o.resolveGeo(ctx, plan.Place)
	f.Geo = geo
	rs.finish(gi, statusOf(geo.Source != "unresolved"),
		fmt.Sprintf("%s → %.4f, %.4f · waypoint %.0f km at bearing %03.0f° · source: %s",
			geo.Name, geo.WayLat, geo.WayLon, geo.DistanceKm, geo.BearingDeg, geo.Source),
		nil, resolveErr(geo.Source))
	emitAgent(gi, "geo", "Geo Resolution")

	// ---- 3. Parallel fan-out ----------------------------------------------
	var (
		wg       sync.WaitGroup
		marine   domain.Marine
		mCites   domain.Citations
		mErr     error
		weather  domain.Weather
		wCites   domain.Citations
		wErr     error
		advisory domain.Advisory
		zone     data.OfficialZone
	)

	oi := rs.start("ocean", "Ocean Analytics")
	wi := rs.start("weather", "Weather Intelligence")
	ii := rs.start("incois", "INCOIS Corroboration")

	wg.Add(3)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				mErr = fmt.Errorf("ocean agent panic: %v", r)
			}
		}()
		marine, mCites, mErr = o.oceanAgent(ctx, geo, plan.WindowHours)
	}()
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				wErr = fmt.Errorf("weather agent panic: %v", r)
			}
		}()
		weather, wCites, wErr = o.weatherAgent(ctx, geo, plan.WindowHours)
	}()
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				advisory = domain.Advisory{Source: "INCOIS", Status: "official bulletin check failed"}
			}
		}()
		advisory, zone = o.incoisAgent(ctx, geo)
	}()
	wg.Wait()

	// Ocean summary includes the PFZ search result, which is the most
	// interesting thing this agent produced.
	oceanStatus, oceanSummary := domain.StatusDone, fmt.Sprintf(
		"waves %.2f m @ %.1f s · swell %.2f m · SST %.1f°C · tide %.2f m",
		marine.WaveHeightM, marine.WavePeriodS, marine.SwellHeightM, marine.SSTC, marine.TideM)
	if mErr != nil {
		oceanStatus = domain.StatusFailed
		oceanSummary = "live marine data unavailable; falling back to stored observation"
		f.Degraded = append(f.Degraded, "marine")
	}
	rs.finish(oi, oceanStatus, oceanSummary, citeList(mCites), errStr(mErr))

	wStatus, wSummary := domain.StatusDone, fmt.Sprintf(
		"wind %.0f km/h · gusts %.0f km/h · precip %.1f mm · cloud %.0f%% · WMO %d → lightning risk %s",
		weather.WindKmh, weather.GustKmh, weather.PrecipMm, weather.CloudPct, weather.WxCode, weather.LightningRisk)
	if wErr != nil {
		wStatus = domain.StatusFailed
		wSummary = "live atmospheric data unavailable; falling back to stored observation"
		f.Degraded = append(f.Degraded, "weather")
	}
	rs.finish(wi, wStatus, wSummary, citeList(wCites), errStr(wErr))

	iStatus, iSummary := domain.StatusDone, "consulted INCOIS for sector advisory status"
	if !advisory.Available {
		iStatus = domain.StatusSkipped
		iSummary = "INCOIS advisory unavailable; continued without corroboration"
		f.Degraded = append(f.Degraded, "incois")
	} else {
		iSummary = advisory.Status
	}
	rs.finish(ii, iStatus, iSummary, []domain.Citation{{
		Source: "INCOIS", Dataset: "sector advisory", Retrieved: time.Now(), Live: advisory.Available,
	}}, "")

	emitAgent(oi, "ocean", "Ocean Analytics")
	emitAgent(wi, "weather", "Weather Intelligence")
	emitAgent(ii, "incois", "INCOIS Corroboration")

	// ---- 4. Snapshot fallback ---------------------------------------------
	if mErr != nil {
		if p, ok := data.SnapshotFor(geo.Name, geo.WayLat, geo.WayLon, o.cfg.SnapshotPath); ok {
			marine = p.Marine
			mCites = p.Marine.Citations
			f.Degraded = append(f.Degraded, "marine:snapshot")
		}
	}
	if wErr != nil {
		if p, ok := data.SnapshotFor(geo.Name, geo.WayLat, geo.WayLon, o.cfg.SnapshotPath); ok {
			weather = p.Weather
			wCites = p.Weather.Citations
			f.Degraded = append(f.Degraded, "weather:snapshot")
		}
	}
	marine.Citations = mCites
	weather.Citations = wCites

	// ---- 5. PFZ + deterministic verdict -----------------------------------
	di := rs.start("domain", "Risk Engine (deterministic)")
	pfz := o.pfzAgent(ctx, geo, marine)
	// The INCOIS agent ran early and in parallel, so that its network round trip
	// overlapped the marine and weather fetches. The point it measured against
	// did not exist yet, though, so the advisory is re-rendered here against the
	// zone the answer is actually about. Re-measuring is exact and, unlike
	// waiting for the zone first, costs no extra round trip.
	advisory = incoisAgainstZone(geo, zone, pfz)
	f.Marine, f.Weather, f.Advisory = marine, weather, advisory
	f.PFZ = pfz
	f.Verdict = engine.Assess(marine, weather, pfz)
	rs.finish(di, domain.StatusDone,
		fmt.Sprintf("verdict=%s (%s) from %d hazard checks · PFZ %.2f at %.0f km, %s confidence",
			f.Verdict.Level, f.Verdict.Severity, len(f.Verdict.Hazards),
			pfz.Score, pfz.DistanceKm, pfz.Confidence), nil, "")
	emitAgent(di, "domain", "Risk Engine (deterministic)")

	// ---- 6. Narration -----------------------------------------------------
	// Provenance is assembled before narration, not after: the answer has to
	// disclose when its figures came from the snapshot rather than a live
	// fetch, and it cannot do that from a field that is still empty.
	f.Prov = provenance(f)

	ni := rs.start("narrator", "Narrator")
	answer, usedLLM := o.narrate(ctx, f, lang.Code(detected))
	f.Answer, f.AnswerLang = answer, string(detected)
	nStatus, nSummary := domain.StatusDone, "deterministic template in "+lang.Name(detected)
	if usedLLM {
		nSummary = "language model narrative in " + lang.Name(detected)
	} else {
		f.Degraded = append(f.Degraded, "llm")
	}
	rs.finish(ni, nStatus, nSummary, nil, "")
	emitAgent(ni, "narrator", "Narrator")

	f.Trace = rs.snapshot()
	emit(Event{Type: "answer", Answer: f.Answer, AnswerLang: f.AnswerLang})
	emit(Event{Type: "done", Final: &f})
	return f
}

// provenance flattens every citation attached to the findings into one ordered,
// de-duplicated list. The client renders "where did these numbers come from"
// from this, so it must contain every dataset the answer could be read from.
func provenance(f domain.Findings) []domain.Citation {
	seen := map[string]bool{}
	var out []domain.Citation
	add := func(c domain.Citations) {
		for _, v := range c {
			key := v.Source + "|" + v.Dataset + "|" + v.URL
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, v)
		}
	}
	add(f.Geo.Citations)
	add(f.Marine.Citations)
	add(f.Weather.Citations)
	add(f.PFZ.Citations)
	add(f.Advisory.Citations)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Dataset < out[j].Dataset
	})
	return out
}

func agentEvent(trace []domain.AgentRun, i int) Event {
	if i < 0 || i >= len(trace) {
		return Event{Type: "agent"}
	}
	r := trace[i]
	return Event{Type: "agent", Run: &r}
}

func (o *Orchestrator) resolveGeo(ctx context.Context, place string) domain.Geo {
	geo := data.Geocode(ctx, place, o.cfg.CoastalPath)
	if geo.Source == "unresolved" {
		return geo
	}
	bearing, townName := 250.0, geo.Name
	if t, d, err := data.NearestTown(geo.Lat, geo.Lon, o.cfg.CoastalPath); err == nil && d < 500 {
		bearing, townName = t.BearingDeg, t.Name
	}
	wl, wo := data.ProjectWaypoint(geo.Lat, geo.Lon, bearing, o.cfg.WaypointKm)
	geo.BearingDeg = bearing
	geo.WayLat, geo.WayLon = wl, wo
	geo.DistanceKm = o.cfg.WaypointKm
	if geo.Name == "" {
		geo.Name = townName
	}
	return geo
}

// weatherAgent reads the atmospheric window, with cache and snapshot fallback.
func (o *Orchestrator) weatherAgent(ctx context.Context, geo domain.Geo, window int) (domain.Weather, domain.Citations, error) {
	key := fmt.Sprintf("wx:%.3f,%.3f:%d", geo.WayLat, geo.WayLon, window)
	if v, ok := o.cache.Get(key); ok {
		c := v.(weatherEntry)
		return c.w, c.c, nil
	}
	w, c, err := data.ForecastWindow(ctx, geo.WayLat, geo.WayLon, window)
	if err == nil {
		o.cache.Put(key, weatherEntry{w, c})
	}
	return w, c, err
}

type weatherEntry struct {
	w domain.Weather
	c domain.Citations
}

// citeList flattens citations for the trace, ordered for stable rendering.
func citeList(c domain.Citations) []domain.Citation {
	out := make([]domain.Citation, 0, len(c))
	for _, v := range c {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dataset < out[j].Dataset })
	return out
}

func orNone(s string) string {
	if s == "" {
		return "unspecified"
	}
	return s
}
func orModel(s string) string {
	if s == "" {
		return "router"
	}
	return s
}
func statusOf(ok bool) string {
	if ok {
		return domain.StatusDone
	}
	return domain.StatusFailed
}
func resolveErr(src string) string {
	if src == "unresolved" {
		return "place could not be resolved to coordinates"
	}
	return ""
}
func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
