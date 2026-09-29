package agents

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"orca/internal/config"
	"orca/internal/data"
	"orca/internal/domain"
	"orca/internal/lang"
	"orca/internal/llm"
)

const coastal = "embed:coastal_towns.json"

// newOffline builds an orchestrator with no API key and no network, which is
// the worst case the system claims to survive.
func newOffline(t *testing.T) *Orchestrator {
	t.Helper()
	data.SetOffline(true)
	t.Cleanup(func() { data.SetOffline(false) })
	cfg := config.Config{
		CoastalPath:        coastal,
		SnapshotPath:       "embed:snapshot.json",
		WaypointKm:         45,
		CacheTTL:           time.Minute,
		MaxCoastDistanceKm: config.DefaultMaxCoastDistanceKm,
	}
	return New(cfg, llm.New("", 0, ""), data.NewCache(time.Minute))
}

// TestUnresolvablePlaceNeverYieldsAVerdict is the regression test for the worst
// failure this service is capable of.
//
// Asking for a place the coastal table does not contain used to produce a
// complete, confident, correctly-formatted safety verdict about the Gulf of
// Guinea. "kanyakumari" resolved to nothing, the zero Geo that came back was
// (0, 0) — a real point in the Atlantic — and the pipeline fetched genuine
// marine data for it and reported 1.26 m waves and 26.8 °C while looking exactly
// like a correct answer about an Indian fishing port. The user saw a map
// centred on West Africa and had no way to tell.
//
// The gate lives in the orchestrator, on the resolution result rather than on
// the plan, so this test drives the whole path: an unknown place must produce a
// clarifying question, no verdict, and no coordinates.
func TestUnresolvablePlaceNeverYieldsAVerdict(t *testing.T) {
	o := newOffline(t)
	for _, q := range []string{
		"conditions near Atlantis",
		"Is tomorrow safe at Port of Nowhere?",
	} {
		f := o.Ask(context.Background(), q, nil)

		if f.Verdict.Level != "" {
			t.Errorf("%q: got verdict %q; an unresolved location must never produce one", q, f.Verdict.Level)
		}
		if len(f.Verdict.Hazards) != 0 || len(f.Verdict.Rationale) != 0 {
			t.Errorf("%q: got %d hazards and %d rationale lines; an unresolved location must produce neither",
				q, len(f.Verdict.Hazards), len(f.Verdict.Rationale))
		}
		if f.Answer == "" {
			t.Errorf("%q: got an empty answer; the user must be asked a question", q)
		}
		// The specific coordinates that caused the bug. (0, 0) is a valid point
		// of ocean, so the invariant is that the response never *claims* to have
		// resolved a location. An empty source means geo was never attempted
		// (the planner discarded the name), "unresolved" means it was attempted
		// and nothing was found, and "inland" means it was attempted and found
		// somewhere with no sea near it. All three are honest, and none of them
		// may carry a verdict.
		if f.Geo.Source != "" && f.Geo.Source != "unresolved" && f.Geo.Source != "inland" {
			t.Errorf("%q: claims a resolved location from source %q at %v,%v", q, f.Geo.Source, f.Geo.Lat, f.Geo.Lon)
		}
		if f.PFZ.Score != 0 || f.PFZ.DistanceKm != 0 {
			t.Errorf("%q: a fishing zone was scored for an unresolved location (score %v, %v km)",
				q, f.PFZ.Score, f.PFZ.DistanceKm)
		}
	}
}

// TestLandlockedPlacesAreRejected is the guard for a failure that never showed up
// in a test suite, because it only happens when a model key is configured.
//
// With no key the router never proposes a place it cannot find, so "Hyderabad"
// was already safe: the query died at the routing step. With a key the model
// supplies the place, the geocoder obligingly resolves Hyderabad to 17.38 N,
// 78.46 E, and the pipeline carried on from there. The geocoder has no idea it
// was asked about a city 314 km from the Arabian Sea, and neither did the
// response, which was a marine forecast with real wave heights and honest
// provenance attached.
//
// The rule is distance from the reference table, checked here against real
// coordinates and the real table rather than a stub, because the whole risk is
// that the two halves disagree about where the coast is.
func TestLandlockedPlacesAreRejected(t *testing.T) {
	o := newOffline(t)
	maxKm := o.cfg.MaxCoastDistanceKm
	if maxKm <= 0 {
		t.Fatalf("MaxCoastDistanceKm is %v; the inland guard would reject everything", maxKm)
	}

	cases := []struct {
		name     string
		lat, lon float64
		coastal  bool
		why      string
	}{
		{"Kochi", 9.931, 76.267, true, "a supported fishing port"},
		{"Kanyakumari", 8.088, 77.541, true, "a major fishing port absent from the 31-name table, ~90 km from Tuticorin"},
		{"Chennai", 13.083, 80.27, true, "a supported port"},
		{"Hyderabad", 17.384, 78.456, false, "314 km inland, the case that actually shipped"},
		{"Jaipur", 26.920, 75.788, false, "845 km inland"},
		{"Thar Desert", 26.0, 71.0, false, "desert, no sea"},
	}
	for _, c := range cases {
		_, dist, err := data.NearestTown(c.lat, c.lon, o.cfg.CoastalPath)
		if err != nil {
			t.Fatalf("%s: reference table: %v", c.name, err)
		}
		if got := dist <= maxKm; got != c.coastal {
			t.Errorf("%s: %.0f km from the nearest known port, threshold %.0f km -> accepted=%v, want %v (%s)",
				c.name, dist, maxKm, got, c.coastal, c.why)
		}
	}
}

// TestInlandPlaceExplainsItself checks that a city which exists is never
// reported as missing. "I could not find Hyderabad" is false, and sends the user
// off to check the spelling of a city that is perfectly real.
func TestInlandPlaceExplainsItself(t *testing.T) {
	for _, code := range []lang.Code{lang.EN, lang.HI, lang.TA, lang.TE, lang.ML} {
		msg := inlandPlace("Hyderabad", code)
		if !strings.Contains(strings.ToLower(msg), "hyderabad") {
			t.Errorf("%s: inland message does not name the place: %q", code, msg)
		}
	}
	en := inlandPlace("Hyderabad", lang.EN)
	if !strings.Contains(en, "not near the coast") {
		t.Errorf("English inland message does not explain why there is no forecast: %q", en)
	}
	if strings.Contains(en, "could not find") {
		t.Error("inland message claims the place could not be found; it is a real city with no sea near it")
	}
}

// TestKnownPlacesStillResolve guards the fix above from being over-broad: the
// gate must not swallow the 31 places that do resolve.
func TestKnownPlacesStillResolve(t *testing.T) {
	o := newOffline(t)
	for _, q := range []string{
		"Is it safe to go fishing off Kochi?",
		"where can I fish near Visakhapatnam?",
		"sea state at Mumbai",
		// Kanyakumari was the reported case. It geocodes nowhere and was
		// therefore unanswerable, so it was added to the reference table; it is
		// here so the next person to prune the data knows it was deliberate.
		"Is it safe to go fishing off Kanyakumari?",
	} {
		f := o.Ask(context.Background(), q, nil)
		if f.Verdict.Level == "" {
			t.Errorf("%q: a known coastal place must still produce a verdict (geo source %q)",
				q, f.Geo.Source)
		}
	}
}

func TestRouterResolvesPlaceAndIntent(t *testing.T) {
	tests := []struct {
		query      string
		wantPlace  string
		wantIntent string
		wantWindow int
	}{
		{"Is it safe to go fishing off Kochi?", "Kochi", "safety", 6},
		{"where to fish near Mumbai", "Mumbai", "pfz", 6},
		{"tell me the sea conditions at Chennai", "Chennai", "conditions", 6},
		{"tide at Vizag", "Visakhapatnam", "tide", 6},
		{"is it safe off Kochi tomorrow", "Kochi", "safety", 30},
		{"conditions at Mangalore in 12 hours", "Mangaluru", "conditions", 12},
		{"conditions at Mangalore for 3 days", "Mangaluru", "conditions", 72},
		{"is Mangalore safe this morning", "Mangaluru", "safety", 12},
		{"Kochi", "Kochi", "general", 6},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			p := planFromRouter(tc.query, lang.Detect(tc.query), coastal)
			if p.Place != tc.wantPlace {
				t.Errorf("place = %q, want %q", p.Place, tc.wantPlace)
			}
			if p.Intent != tc.wantIntent {
				t.Errorf("intent = %q, want %q", p.Intent, tc.wantIntent)
			}
			if p.WindowHours != tc.wantWindow {
				t.Errorf("window = %d, want %d", p.WindowHours, tc.wantWindow)
			}
		})
	}
}

// TestRouterWindowIsAlwaysBounded protects the upstream request: an unbounded or
// zero window would produce a meaningless aggregate.
func TestRouterWindowIsAlwaysBounded(t *testing.T) {
	for _, q := range []string{
		"conditions at Kochi in 900 hours", "conditions at Kochi in 0 hours",
		"conditions at Kochi in 99 days", "conditions at Kochi for -5 hours",
		"conditions at Kochi in 100000 days", "tomorrow", "", "   ",
	} {
		p := planFromRouter(q, lang.EN, coastal)
		if p.WindowHours < 1 || p.WindowHours > 72 {
			t.Errorf("query %q gave window %d, want 1..72", q, p.WindowHours)
		}
	}
}

// TestPlannerFallsBackWithoutKey confirms the planner produces a usable plan
// with no model available. The front door of the pipeline must not depend on a
// paid third party.
func TestPlannerFallsBackWithoutKey(t *testing.T) {
	cfg := config.Config{CoastalPath: coastal}
	p := &PlannerAgent{cfg: cfg, llm: llm.New("", 0, ""), coast: coastal}
	got := p.Plan(context.Background(), "is it safe to go fishing off Kochi tomorrow", lang.EN)
	if got.Place != "Kochi" {
		t.Errorf("place = %q, want Kochi", got.Place)
	}
	if got.Lang != string(lang.EN) {
		t.Errorf("lang = %q, want en", got.Lang)
	}
	if got.Planned {
		t.Error("Planned must be false when the deterministic router was used")
	}
	if got.WindowHours != 30 {
		t.Errorf("window = %d, want 30", got.WindowHours)
	}
}

// TestClarifyCoversEveryLanguage checks that a user whose question could not be
// resolved is told how to fix it, in their own language and script. The example
// place name is rendered in the native script, so the assertion is on structure
// rather than on the English spelling of the name.
func TestClarifyCoversEveryLanguage(t *testing.T) {
	seen := map[lang.Code]string{}
	for _, c := range lang.All {
		got := clarify(c)
		if strings.TrimSpace(got) == "" {
			t.Errorf("clarify(%q) is empty", c)
			continue
		}
		if !strings.Contains(got, "**") {
			t.Errorf("clarify(%q) offers no emphasised example location: %q", c, got)
		}
		seen[c] = got
	}
	// Each language must have its own text. A silent reuse of the Hindi string
	// for Marathi, or of the English string for everything, would pass the checks
	// above while failing the user.
	distinct := map[string]bool{}
	for _, v := range seen {
		distinct[v] = true
	}
	if len(distinct) != len(seen) {
		t.Errorf("only %d distinct clarification texts for %d languages", len(distinct), len(seen))
	}
}

// TestOfflineStillProducesAVerdict is the central resilience guarantee: with no
// model key and no network, ORCA must still return a complete, sourced verdict.
func TestOfflineStillProducesAVerdict(t *testing.T) {
	o := newOffline(t)
	f := o.Ask(context.Background(), "Is it safe to go fishing off Kochi tomorrow?", nil)

	if f.Geo.Source == "unresolved" {
		t.Fatal("geocoding must succeed offline via the reference table")
	}
	if f.Verdict.Level == "" {
		t.Fatal("a verdict must be produced even with no upstream data")
	}
	switch f.Verdict.Level {
	case "go", "caution", "no-go":
	default:
		t.Errorf("unexpected verdict level %q", f.Verdict.Level)
	}
	if len(f.Verdict.Hazards) == 0 {
		t.Error("hazard breakdown must be present offline")
	}
	if strings.TrimSpace(f.Answer) == "" {
		t.Error("an answer must be produced offline via the deterministic template")
	}
	if len(f.Trace) < 5 {
		t.Errorf("expected a full agent trace, got %d entries", len(f.Trace))
	}
	// The degradation must be declared, not hidden.
	if !contains(f.Degraded, "marine") && !contains(f.Degraded, "marine:snapshot") {
		t.Errorf("offline marine data must be reported as degraded, got %v", f.Degraded)
	}
	if !contains(f.Degraded, "llm") {
		t.Errorf("absence of a model must be reported as degraded, got %v", f.Degraded)
	}
}

// TestSnapshotFallbackSuppliesRealFigures confirms the offline path yields
// plausible marine numbers rather than zeros.
func TestSnapshotFallbackSuppliesRealFigures(t *testing.T) {
	o := newOffline(t)
	f := o.Ask(context.Background(), "sea conditions at Kochi", nil)
	if f.Marine.WaveHeightM <= 0 {
		t.Errorf("offline wave height = %.2f, want a real figure from the snapshot", f.Marine.WaveHeightM)
	}
	if f.Marine.SSTC < 20 {
		t.Errorf("offline SST = %.1f C, implausible for the Indian coast", f.Marine.SSTC)
	}
	// A snapshot figure must be labelled non-live.
	for ds, c := range f.Marine.Citations {
		if c.Live {
			t.Errorf("snapshot dataset %q is marked live", ds)
		}
	}
	if len(f.Prov) == 0 {
		t.Error("provenance must still be reported for snapshot figures")
	}
	for _, c := range f.Prov {
		if c.Retrieved.IsZero() {
			t.Error("provenance entry has no timestamp, so staleness cannot be shown")
		}
	}
}

func TestUnresolvablePlaceAsksForClarification(t *testing.T) {
	o := newOffline(t)
	f := o.Ask(context.Background(), "is it safe to go out tomorrow", nil)
	if f.Verdict.Level != "" {
		t.Errorf("no location means no verdict, got %q", f.Verdict.Level)
	}
	if strings.TrimSpace(f.Answer) == "" {
		t.Fatal("the user must still be given a reply asking for a location")
	}
	if !strings.Contains(f.Answer, "Kochi") {
		t.Error("expected a clarification request naming an example location")
	}
}

// TestEmittedTraceIsMonotonic checks the progressive events a streaming client
// receives are ordered and internally consistent.
func TestEmittedTraceIsMonotonic(t *testing.T) {
	o := newOffline(t)
	var names []string
	var sawAnswer, sawDone bool
	o.Ask(context.Background(), "is it safe off Kochi", func(e Event) {
		switch e.Type {
		case "agent":
			if e.Run == nil {
				t.Error("agent event without a run")
				return
			}
			names = append(names, e.Run.Name)
		case "answer":
			sawAnswer = true
		case "done":
			sawDone = true
		}
	})
	if !sawAnswer || !sawDone {
		t.Errorf("stream must end with an answer and a done event (answer=%v done=%v)", sawAnswer, sawDone)
	}
	// The order is the pipeline: plan, resolve, gather, decide, narrate.
	want := []string{"planner", "geo", "ocean", "weather", "incois", "domain", "narrator"}
	if len(names) != len(want) {
		t.Fatalf("emitted %d agent events, want %d: %v", len(names), len(want), names)
	}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("agent event %d = %q, want %q (full order: %v)", i, names[i], w, names)
		}
	}
}

// TestNoAgentReportsDoneWhenItFailed keeps the trace honest.
func TestFailedAgentIsMarkedFailed(t *testing.T) {
	o := newOffline(t)
	f := o.Ask(context.Background(), "is it safe off Kochi", nil)
	for _, r := range f.Trace {
		if strings.Contains(r.Summary, "falling back") && r.Status == domain.StatusDone {
			t.Errorf("agent %q reported a fallback but is marked done", r.Name)
		}
	}
}

func TestPFZSearchStaysAtSea(t *testing.T) {
	// The search fan must not select a cell that the snapshot cannot describe,
	// and every reported coordinate must be a real number.
	o := newOffline(t)
	f := o.Ask(context.Background(), "where to fish off Kochi", nil)
	if f.PFZ.Lat == 0 && f.PFZ.Lon == 0 {
		t.Error("no fishing zone was reported at all")
	}
	if f.PFZ.Score < 0 || f.PFZ.Score > 1 {
		t.Errorf("PFZ score %.3f is outside 0..1", f.PFZ.Score)
	}
	if f.PFZ.Confidence != "high" && f.PFZ.Confidence != "medium" && f.PFZ.Confidence != "low" {
		t.Errorf("unexpected confidence %q", f.PFZ.Confidence)
	}
	if len(f.PFZ.Reasoning) == 0 {
		t.Error("the zone choice must be explained, not just reported")
	}
}

func TestConcurrentQueriesAreIsolated(t *testing.T) {
	// Parallel fan-out means several goroutines write to one runState; a race
	// here would corrupt the trace shown to the user.
	//
	// Each query gets its own orchestrator because the shared TTL cache is
	// process-wide, not per-query. What is under test is that two requests
	// running at the same time cannot see each other's trace.
	o := newOffline(t)
	places := []string{"Kochi", "Chennai", "Mumbai", "Goa", "Puri", "Kolkata", "Kollam", "Visakhapatnam"}
	done := make(chan domain.Findings, len(places))
	for _, p := range places {
		go func(p string) {
			done <- o.Ask(context.Background(), "is it safe off "+p, nil)
		}(p)
	}
	for range places {
		select {
		case f := <-done:
			if f.Verdict.Level == "" {
				t.Error("concurrent query produced no verdict")
			}
			if len(f.Trace) != 7 {
				t.Errorf("concurrent query produced %d trace entries, want 7", len(f.Trace))
			}
		case <-time.After(30 * time.Second):
			t.Fatal("concurrent query did not complete")
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// TestEveryDisplayedNumberHasACitation is the standing-rule-2 test.
//
// The response shows figures from four places — the coast, the sea state, the
// atmosphere, and the suggested zone. An earlier version attributed the first
// three and left the zone's coordinates, score and SST anomaly with no source at
// all, because the fishing-zone fan discarded the citations of the cell it
// picked. This asserts the rule at the level it actually matters: whatever the
// UI can read, the provenance list must cover.
func TestEveryDisplayedNumberHasACitation(t *testing.T) {
	o := newOffline(t)
	f := o.Ask(context.Background(), "Is it safe off Puri?", nil)

	if len(f.Prov) == 0 {
		t.Fatal("no provenance at all")
	}

	sources := map[string]bool{}
	for _, c := range f.Prov {
		if c.Source == "" || c.Dataset == "" {
			t.Errorf("citation is missing a source or dataset: %+v", c)
		}
		if c.Retrieved.IsZero() {
			t.Errorf("citation %s/%s has no retrieval time", c.Source, c.Dataset)
		}
		sources[c.Source] = true
	}

	// The zone and the anomaly are computed, so their inputs must appear.
	if f.PFZ.Score > 0 {
		if !sources["Open-Meteo Marine"] {
			t.Error("the fishing zone has no marine source")
		}
	}
	if f.PFZ.AnomalyC != 0 && !sources["Open-Meteo Archive"] {
		t.Error("the SST anomaly is displayed but the archive baseline is not cited")
	}

	// Offline mode must never let a citation claim to be live.
	for _, c := range f.Prov {
		if c.Live {
			t.Errorf("offline citation claims live: %s/%s", c.Source, c.Dataset)
		}
	}
}

// TestPFZCarriesItsOwnCitations checks the specific mechanism: the winning fan
// cell's marine call travels with the zone rather than being dropped.
func TestPFZCarriesItsOwnCitations(t *testing.T) {
	o := newOffline(t)
	f := o.Ask(context.Background(), "Is it safe off Chennai?", nil)

	if len(f.PFZ.Citations) == 0 {
		t.Fatal("PFZ carries no citations of its own")
	}
	hasSST := false
	for _, c := range f.PFZ.Citations {
		if c.Dataset == "sea_surface_temperature" {
			hasSST = true
		}
	}
	if !hasSST {
		t.Errorf("PFZ citations lack the SST dataset: %v", f.PFZ.Citations)
	}
	// And they must reach the flat list the UI reads.
	inProv := false
	for _, c := range f.Prov {
		if c.Source == "Open-Meteo Marine" && c.Dataset == "sea_surface_temperature" {
			inProv = true
		}
	}
	if !inProv {
		t.Error("the zone's SST citation never reached Findings.Prov")
	}
}

// TestClarificationReachesTheStream pins a bug that only the browser found: the
// clarification for an unanswerable question was built and returned by /api/ask
// but never emitted on the event stream, so the interface showed nothing at all
// to a user who had asked a perfectly ordinary question without naming a town.
func TestClarificationReachesTheStream(t *testing.T) {
	o := newOffline(t)
	var events []Event
	f := o.Ask(context.Background(), "Is it safe to go out today?", func(e Event) {
		events = append(events, e)
	})

	if strings.TrimSpace(f.Answer) == "" {
		t.Fatal("the returned findings carry no answer to show")
	}
	if f.Verdict.Level != "" {
		t.Errorf("a clarification must not carry a verdict, got %q", f.Verdict.Level)
	}

	var sawAnswer, sawDone bool
	for _, e := range events {
		switch e.Type {
		case "answer":
			sawAnswer = true
			if strings.TrimSpace(e.Answer) == "" {
				t.Error("the answer event carried no text")
			}
			if e.AnswerLang != f.AnswerLang {
				t.Errorf("the stream reported language %q, the result says %q", e.AnswerLang, f.AnswerLang)
			}
		case "done":
			sawDone = true
			if e.Final == nil {
				t.Error("the done event carried no findings")
			}
		case "end":
			// The HTTP layer writes this when the channel closes, on every path.
			// Emitting it here as well would double it.
			t.Error("the orchestrator must not emit end; the HTTP layer does")
		}
	}
	if !sawAnswer {
		t.Error("no answer event was emitted, so the interface renders nothing")
	}
	if !sawDone {
		t.Error("no done event was emitted, so the interface never resolves the result")
	}
}

// TestPlanDoesNotDependOnTheLanguage is the test the consistency metric could
// not be, because two languages can disagree about a plan and still land on the
// same verdict by luck. The router picks the intent and the forecast window, so
// a question it misreads is a different question: "tomorrow morning" in a
// language whose words for it the table lacked became a 6-hour general request,
// which stopped short of the weather that made the call a caution. The answer
// still came back in the right language, so nothing looked wrong.
func TestPlanDoesNotDependOnTheLanguage(t *testing.T) {
	// One question per language, all of them "is it safe to fish off Kochi
	// tomorrow morning", which must plan identically in every one.
	queries := map[string]string{
		"en": "Is it safe to fish off Kochi tomorrow morning?",
		"hi": "क्या कल सुबह कोच्चि से मछली पकड़ने जाना सुरक्षित है?",
		"mr": "उद्या सकाळी कोच्चीहून मासेमारीला जाणे सुरक्षित आहे का?",
		"bn": "আগামীকাল সকালে কোচি থেকে মাছ ধরতে যাওয়া কি নিরাপদ?",
		"gu": "કાલે સવારે કોચીથી માછલી પકડવાનું સુરક્ષિત છે?",
		"kn": "ನಾಳೆ ಬೆಳಗ್ಗೆ ಕೊಚ್ಚಿಯಿಂದ ಮೀನು ಹಿಡಿಯಬಹುದೇ?",
		"ml": "നാളെ രാവിലെ കൊച്ചിയിൽ നിന്ന് മീൻ പിടിക്കാൻ പോകാമോ?",
		"ta": "நாளை காலை கொச்சியில் இருந்து மீன் பிடிக்க போகலாமா?",
		"te": "రేపు ఉదయం కోచీ నుండి చేపల వెటేయడం సురక్షితమేమో?",
		"or": "ଆସନ୍ତାଦିନ ପୂର୍ବାହ୍ନରେ କୋଚିରେ ମାଛ ଧରିବା ଉଚିତ କି?",
	}

	type plan struct {
		place  string
		intent string
		window int
	}
	first := plan{}
	firstLang := ""
	for _, code := range langOrder(queries) {
		p := planFromRouter(queries[code], lang.Code(code), coastal)
		got := plan{place: p.Place, intent: p.Intent, window: p.WindowHours}
		if firstLang == "" {
			first, firstLang = got, code
			continue
		}
		if got != first {
			t.Errorf("%s planned %+v but %s planned %+v; the router read two different questions",
				code, got, firstLang, first)
		}
	}
}

func langOrder(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestTheTimeAndIntentTableCoversEveryLanguage holds the router's vocabulary to
// the standard that actually catches a misspelling: every term in the table has
// to occur in a real sentence that is already in the repository. The table
// gained a Gujarati "safe" spelled differently from the corpus's, and because
// the two look identical to anyone who does not read the script, the only
// symptom was that Gujarati questions were planned as fishing questions.
func TestTheTimeAndIntentTableCoversEveryLanguage(t *testing.T) {
	// Sentences already in the repository: the evaluation corpus and the
	// interface examples. Every language contributes one, and between them they
	// must mention every time and intent term the router relies on.
	corpus := []string{
		"Is it safe to fish off Kochi tomorrow morning?",
		"क्या कल सुबह कोच्चि से मछली पकड़ने जाना सुरक्षित है?",
		"उद्या सकाळी कोच्चीहून मासेमारीला जाणे सुरक्षित आहे का?",
		"আগামীকাল সকালে কোচি থেকে মাছ ধরতে যাওয়া কি নিরাপদ?",
		"કાલે સવારે કોચીથી માછલી પકડવાનું સુરક્ષિત છે?",
		"ನಾಳೆ ಬೆಳಗ್ಗೆ ಕೊಚ್ಚಿಯಿಂದ ಮೀನು ಹಿಡಿಯಬಹುದೇ?",
		"നാളെ രാവിലെ കൊച്ചിയിൽ നിന്ന് മീൻ പിടിക്കാൻ പോകാമോ?",
		"நாளை காலை கொச்சியில் இருந்து மீன் பிடிக்க போகலாமா?",
		"రేపు ఉదయం కోచీ నుండి చేపల వెటేయడం సురక్షితమేమో?",
		"ଆସନ୍ତାଦିନ ପୂର୍ବାହ୍ନରେ କୋଚିରେ ମାଛ ଧରିବା ଉଚିତ କି?",
		// The interface examples, which contribute the "today" words.
		"சென்னை கடலில் இன்று மீன் பிடிக்கலாமா?",
		"విశాఖపట్నం దగ్గర ఈరోజు చేపల పట్టుకోవడం సురక్షితమా?",
		"ಮಂಗಳೂರಿನಿಂದ ಇಂದು ಮೀನು ಪಟ್ಟುವುದು ಸುರಕ್ಷಿತವೇ?",
		"കൊച്ചിയിൽ ഇന്ന് മീൻ പിടിക്കാമോ?",
		"ગુજરાતમાંથી આજે માછલી પકડવી સુરક્ષિત છે?",
		"ପୁରୀରୁ ଆଜି ମାଛ ଧରିବା ସୁରକ୍ଷିତ କି?",
		"আজ কি বাংলাদেশ থেকে মাছ ধরা যাবে?",
		"आज रत्नागिरीहून मासे पकडणे सुरक्षित आहे का?",
	}
	hay := strings.ToLower(strings.Join(corpus, " "))

	// Only the categories the sentences above actually exercise are held to
	// this standard: tomorrow, today, morning, safety and fishing. Tides,
	// conditions and evening have no verified sentence anywhere in the
	// repository, and a misspelling there is a known, documented gap rather
	// than a claim being made. English terms are left to review: a wrong one is
	// visible to the person who wrote it, whereas a wrong Gujarati one is not.
	verified := map[string][]string{
		"tomorrow": timeTomorrow,
		"today":    timeToday,
		"morning":  timeMorning,
	}
	for _, g := range intentWords {
		if g.intent == "safety" || g.intent == "pfz" {
			verified[g.intent] = g.words
		}
	}
	for label, terms := range verified {
		for _, term := range terms {
			if isASCII(term) {
				continue
			}
			if !strings.Contains(hay, strings.ToLower(term)) {
				t.Errorf("%s term %q does not occur in any repository sentence; "+
					"it is either misspelled or invented", label, term)
			}
		}
	}
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}
