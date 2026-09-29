package data

import (
	"context"
	"math"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

const coastal = "embed:coastal_towns.json"

func TestTownsLoad(t *testing.T) {
	towns, err := Towns(coastal)
	if err != nil {
		t.Fatalf("coastal table must be loadable from the binary: %v", err)
	}
	if len(towns) < 20 {
		t.Errorf("expected a reasonably complete coastal table, got %d entries", len(towns))
	}
	seen := map[string]bool{}
	for _, tn := range towns {
		if tn.Name == "" {
			t.Error("entry with empty name")
		}
		if seen[tn.Name] {
			t.Errorf("duplicate town %q", tn.Name)
		}
		seen[tn.Name] = true
		if tn.Lat < 5 || tn.Lat > 23 {
			t.Errorf("%s latitude %.3f is outside the Indian coastline range", tn.Name, tn.Lat)
		}
		if tn.Lon < 68 || tn.Lon > 98 {
			t.Errorf("%s longitude %.3f is outside the Indian coastline range", tn.Name, tn.Lon)
		}
		if tn.BearingDeg < 0 || tn.BearingDeg >= 360 {
			t.Errorf("%s has an out-of-range offshore bearing %.1f", tn.Name, tn.BearingDeg)
		}
	}
}

func TestMatchTown(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Kochi", "Kochi"},
		{"kochi", "Kochi"},
		{"KOCHI", "Kochi"},
		{"  Kochi  ", "Kochi"},
		{"cochi", "Kochi"}, // common misspelling
		{"Kochi, Kerala", "Kochi"},
		{"Mumbai", "Mumbai"},
		{"chennai", "Chennai"},
		{"Mangalore", "Mangaluru"},
		{"Cochin", "Kochi"},
		{"Calicut", "Kozhikode"},
		{"vizag", "Visakhapatnam"},
		{"madras", "Chennai"},
		{"bombay", "Mumbai"},
		{"quilon", "Kollam"},
		{"thoothukudi", "Tuticorin"},
		{"puducherry", "Pondicherry"},
		{"calcutta", "Kolkata"},
		{"Kochiii", "Kochi"}, // transposition
		{"Kochi1", "Kochi"},  // one substitution
		{"Koc", "Kochi"},     // short token, resolved by prefix matching
	}
	for _, c := range cases {
		tn, ok := MatchTown(c.in, coastal)
		if !ok {
			t.Errorf("MatchTown(%q) found nothing, want %q", c.in, c.want)
			continue
		}
		if tn.Name != c.want {
			t.Errorf("MatchTown(%q) = %q, want %q", c.in, tn.Name, c.want)
		}
	}
	if _, ok := MatchTown("atlantis", coastal); ok {
		t.Error("a nonsense place must not resolve")
	}
	if _, ok := MatchTown("", coastal); ok {
		t.Error("empty query must not resolve")
	}
	// The typo tier must not swallow an unrelated word.
	for _, q := range []string{"atlantis", "reykjavik", "zzzzzzzz"} {
		if _, ok := MatchTown(q, coastal); ok {
			t.Errorf("MatchTown(%q) must not resolve", q)
		}
	}
}

func TestProjectWaypoint(t *testing.T) {
	// Kochi coast, bearing 250° (WSW, out into the Arabian Sea).
	lat, lon := ProjectWaypoint(9.9658, 76.2422, 250, 45)
	if lat >= 9.9658 {
		t.Errorf("a westerly bearing should reduce latitude, got %.4f from 9.9658", lat)
	}
	if lon >= 76.2422 {
		t.Errorf("a westerly bearing should reduce longitude, got %.4f from 76.2422", lon)
	}
	// The projected point must be the requested distance away.
	d := HaversineKm(9.9658, 76.2422, lat, lon)
	if math.Abs(d-45) > 0.5 {
		t.Errorf("projected distance = %.2f km, want 45 km", d)
	}
	// Zero distance is a no-op.
	lat0, lon0 := ProjectWaypoint(9.9658, 76.2422, 250, 0)
	if math.Abs(lat0-9.9658) > 1e-9 || math.Abs(lon0-76.2422) > 1e-9 {
		t.Errorf("zero-distance projection moved the point: %.6f,%.6f", lat0, lon0)
	}
}

func TestProjectWaypointCrossesAntimeridian(t *testing.T) {
	// Longitude must stay in [-180, 180] even when a bearing crosses the
	// dateline, or the map layer and the API request both break.
	for _, bearing := range []float64{0, 45, 90, 135, 180, 225, 270, 315} {
		for _, lon := range []float64{-179.9, 179.9, 0} {
			_, out := ProjectWaypoint(10, lon, bearing, 500)
			if out < -180 || out > 180 {
				t.Errorf("bearing %.0f from lon %.1f produced out-of-range longitude %.4f", bearing, lon, out)
			}
		}
	}
}

func TestHaversineKm(t *testing.T) {
	// Kochi to Mumbai is roughly 1075 km by great-circle distance.
	d := HaversineKm(9.9658, 76.2422, 19.076, 72.8777)
	if math.Abs(d-1075) > 30 {
		t.Errorf("Kochi→Mumbai = %.1f km, want roughly 1075", d)
	}
	if d0 := HaversineKm(10, 76, 10, 76); d0 != 0 {
		t.Errorf("identical points gave distance %.6f", d0)
	}
}

func TestStartIndex(t *testing.T) {
	ist := mustLoadIST()
	mk := func(hours ...int) []string {
		base := time.Date(2026, 9, 29, 0, 0, 0, 0, ist)
		out := make([]string, len(hours))
		for i, h := range hours {
			out[i] = base.Add(time.Duration(h) * time.Hour).Format("2006-01-02T15:04")
		}
		return out
	}

	// At 17:20 the window must start at the 17:00 stamp, not at midnight.
	if got := startIndex(mk(0, 6, 12, 17, 18, 23), time.Date(2026, 9, 29, 17, 20, 0, 0, ist)); got != 3 {
		t.Errorf("startIndex at 17:20 = %d, want 3 (the 17:00 stamp)", got)
	}
	// A few minutes before the hour still belongs to the current hour, which is
	// the 30-minute grace period working.
	if got := startIndex(mk(0, 6, 17, 18), time.Date(2026, 9, 29, 16, 55, 0, 0, ist)); got != 2 {
		t.Errorf("startIndex at 16:55 = %d, want 2 (the 17:00 stamp, not 06:00)", got)
	}
	// Before the first stamp: use the beginning.
	if got := startIndex(mk(12, 13), time.Date(2026, 9, 29, 6, 0, 0, 0, ist)); got != 0 {
		t.Errorf("startIndex before the series = %d, want 0", got)
	}
	// After the last stamp: return the most recent hour rather than the oldest
	// or an out-of-range index.
	if got := startIndex(mk(0, 6), time.Date(2026, 9, 30, 6, 0, 0, 0, ist)); got != 1 {
		t.Errorf("startIndex past the series = %d, want 1 (the most recent stamp)", got)
	}
	if got := startIndex(nil, time.Now()); got != 0 {
		t.Errorf("startIndex on an empty series = %d, want 0", got)
	}
	// Malformed input must not panic.
	if got := startIndex([]string{"garbage", "also-garbage"}, time.Now()); got != 0 {
		t.Errorf("startIndex on malformed input = %d, want 0", got)
	}
}

func TestLightningRisk(t *testing.T) {
	tests := []struct {
		name          string
		wx            int
		precip, cloud float64
		want          string
	}{
		{"clear", 0, 0, 0, "low"},
		{"drizzle", 53, 0.4, 100, "low"},
		{"thunderstorm alone is moderate", 95, 0, 60, "moderate"},
		{"thunderstorm with rain is high", 95, 3, 80, "high"},
		{"heavy rain with cloud is moderate", 63, 6, 80, "moderate"},
		{"light rain with cloud is low", 61, 1.2, 60, "low"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := LightningRisk(tc.wx, tc.precip, tc.cloud); got != tc.want {
				t.Errorf("LightningRisk(%d, %.1f, %.0f) = %q, want %q",
					tc.wx, tc.precip, tc.cloud, got, tc.want)
			}
		})
	}
	// Lightning risk is a safety input, so it must never be an unrecognised
	// value that downstream comparisons silently treat as safe.
	for wx := 0; wx <= 100; wx++ {
		r := LightningRisk(wx, 12, 95)
		if r != "low" && r != "moderate" && r != "high" {
			t.Fatalf("LightningRisk(%d) returned unclassified value %q", wx, r)
		}
	}
}

func TestSnapshotFallback(t *testing.T) {
	snap, err := LoadSnapshot("embed:snapshot.json")
	if err != nil {
		t.Fatalf("a baked snapshot must ship with the binary: %v", err)
	}
	if snap.GeneratedAt.IsZero() {
		t.Error("snapshot has no generation timestamp; its age cannot be shown")
	}
	if len(snap.Places) < 8 {
		t.Errorf("snapshot covers only %d places; want broad coastal coverage", len(snap.Places))
	}
	// Every snapshot figure must be marked non-live, so the UI cannot present
	// baked data as live. This is enforced centrally at load time.
	for name, p := range snap.Places {
		for ds, c := range p.Marine.Citations {
			if c.Live {
				t.Errorf("%s/%s: snapshot citation marked live", name, ds)
			}
			if c.Retrieved.IsZero() {
				t.Errorf("%s/%s: snapshot citation has no timestamp", name, ds)
			}
		}
		for ds, c := range p.Weather.Citations {
			if c.Live {
				t.Errorf("%s/%s: snapshot citation marked live", name, ds)
			}
		}
		if p.Marine.WaveHeightM <= 0 {
			t.Errorf("%s: snapshot wave height is %.2f, which is not usable data", name, p.Marine.WaveHeightM)
		}
	}
}

func TestSnapshotForExactAndNearest(t *testing.T) {
	p, ok := SnapshotFor("Kochi", 9.9658, 76.2422, "embed:snapshot.json")
	if !ok {
		t.Fatal("Kochi must be present in the baked snapshot")
	}
	if p.Name != "Kochi" {
		t.Errorf("exact lookup returned %q", p.Name)
	}
	// A nearby-but-absent place should resolve to the nearest entry rather than
	// failing, because a labelled approximation beats a refusal.
	if q, ok := SnapshotFor("somewhere slightly off", 10.0, 76.3, "embed:snapshot.json"); ok {
		if q.Name == "" {
			t.Error("nearest match returned an empty place")
		}
	}
	// Halfway to the equator is beyond the 400 km tolerance.
	if _, ok := SnapshotFor("nowhere near", -20, 40, "embed:snapshot.json"); ok {
		t.Error("a location far from any snapshot entry must not silently substitute")
	}
}

func TestGeoNamesSorted(t *testing.T) {
	names := GeoNames(coastal)
	if len(names) == 0 {
		t.Fatal("GeoNames returned nothing")
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("GeoNames is not sorted: %q before %q", names[i-1], names[i])
		}
	}
}

func TestOfflineSwitch(t *testing.T) {
	was := Offline()
	defer SetOffline(was)

	SetOffline(true)
	if !Offline() {
		t.Fatal("SetOffline(true) did not take effect")
	}
	// A suppressed call must report an error rather than returning zero values
	// that would be indistinguishable from real calm conditions.
	var out map[string]any
	if err := getJSON(t.Context(), "https://example.invalid/x", &out); err == nil {
		t.Error("offline mode must fail outbound calls")
	}
	SetOffline(false)
}

// TestNearestOnLines checks the geometry against cases with known answers. The
// bulletin draws each sector as hundreds of vertices, so measuring to vertices
// instead of to segments would systematically overstate the distance; these
// cases pin the segment behaviour.
func TestNearestOnLines(t *testing.T) {
	// A short line just west of 76E on the equator. The query point sits due
	// east of its midpoint, so the answer must be the perpendicular distance
	// and not the distance to either end vertex.
	line := [][]float64{{75.9, 10.0}, {76.1, 10.0}}
	lines := [][][]float64{line}

	d, bearing, ok := nearestOnLines(lines, 10.0, 76.3)
	if !ok {
		t.Fatal("no distance computed")
	}
	// 0.2 degrees of longitude at 10N is 0.2 * 111.32 * cos(10) = 21.93 km.
	want := 0.2 * 111.32 * math.Cos(10*math.Pi/180)
	if math.Abs(d-want) > 0.2 {
		t.Errorf("perpendicular distance = %.3f km, want ~%.3f", d, want)
	}
	// The line lies to the west, so the bearing must point that way.
	if bearing < 260 || bearing > 280 {
		t.Errorf("bearing = %.1f, want ~270 (west)", bearing)
	}

	// A point ON the line must be reported as zero, not as the distance to the
	// nearest vertex.
	d, _, ok = nearestOnLines(lines, 10.0, 76.05)
	if !ok {
		t.Fatal("no distance computed for a point on the line")
	}
	if d > 0.2 {
		t.Errorf("point on the line reported as %.3f km away, want ~0", d)
	}

	// A single degenerate segment must not divide by zero.
	if _, _, ok := nearestOnLines([][][]float64{{{76.0, 10.0}, {76.0, 10.0}}}, 10.0, 76.1); !ok {
		t.Error("degenerate segment should still produce a distance")
	}

	// No lines at all must be reported as not found, never as distance zero.
	if _, _, ok := nearestOnLines(nil, 10.0, 76.0); ok {
		t.Error("empty geometry reported a distance; that would read as 'adjacent'")
	}
}

// TestBulletinDate checks the Julian-day conversion the advisory label depends
// on. A wrong date here would put a stale official advisory on screen under
// today's date, which is worse than showing none.
func TestBulletinDate(t *testing.T) {
	tests := []struct {
		year   int
		julian string
		want   string
	}{
		{2026, "271", "2026-09-28"},
		{2026, "1", "2026-01-01"},
		{2024, "60", "2024-02-29"}, // leap year
		{2026, "365", "2026-12-31"},
	}
	for _, tc := range tests {
		got := bulletinDate(tc.year, tc.julian)
		if got.IsZero() {
			t.Errorf("bulletinDate(%d, %q) = zero time", tc.year, tc.julian)
			continue
		}
		if s := got.Format("2006-01-02"); s != tc.want {
			t.Errorf("bulletinDate(%d, %q) = %s, want %s", tc.year, tc.julian, s, tc.want)
		}
	}
	for _, bad := range []struct {
		year int
		day  string
	}{{2026, ""}, {2026, "abc"}, {1999, "100"}, {2026, "400"}} {
		if d := bulletinDate(bad.year, bad.day); !d.IsZero() {
			t.Errorf("bulletinDate(%d, %q) = %v, want zero for unusable input", bad.year, bad.day, d)
		}
	}
}

// TestPFZBboxCoversTheRadius guards the search box. Too small and we report a
// false "no advisory here"; too large and every query pulls a large response
// from a free government server.
func TestPFZBboxCoversTheRadius(t *testing.T) {
	u, err := pfzURL(19.8, 85.8, 60)
	if err != nil {
		t.Fatalf("pfzURL: %v", err)
	}
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("pfzURL produced an unparseable URL: %v", err)
	}
	q := parsed.Query()
	if got := q.Get("typeNames"); got != "PFZ_Automation:pfzlines" {
		t.Errorf("layer = %q, want PFZ_Automation:pfzlines", got)
	}

	// The box must actually contain the point, with margin, and must be wide
	// enough to hold a 60 km search on every side.
	parts := strings.Split(q.Get("bbox"), ",")
	if len(parts) != 5 {
		t.Fatalf("bbox = %q, want 4 numbers plus the CRS", q.Get("bbox"))
	}
	var v [4]float64
	for i := 0; i < 4; i++ {
		f, err := strconv.ParseFloat(parts[i], 64)
		if err != nil {
			t.Fatalf("bbox value %q: %v", parts[i], err)
		}
		v[i] = f
	}
	if v[0] >= 85.8 || v[2] <= 85.8 || v[1] >= 19.8 || v[3] <= 19.8 {
		t.Errorf("bbox %v does not contain 19.8N 85.8E", v)
	}
	// 60 km is 0.542 degrees of latitude; with the 15% pad the box must be at
	// least that wide, or a line just outside the true radius is missed.
	if span := v[3] - v[1]; span < 1.08 {
		t.Errorf("latitude span %.3f is too small to contain a 60 km search", span)
	}
	if dLon := v[2] - v[0]; dLon < 0.60 {
		t.Errorf("longitude span %.3f is too small at this latitude", dLon)
	}
	if _, err := pfzURL(200, 0, 60); err == nil {
		t.Error("expected an error for a point outside the valid range")
	}
}

// TestOfficialPFZRespectsOfflineSwitch checks the degradation contract: with no
// network the caller must be able to tell "could not check" from "checked, and
// the nearest line is 12 km away". Collapsing the two would let the UI imply an
// official advisory exists when nothing was ever asked.
func TestOfficialPFZRespectsOfflineSwitch(t *testing.T) {
	SetOffline(true)
	defer SetOffline(false)

	z := OfficialPFZ(context.Background(), 19.8, 85.8, 60)
	if z.Checked {
		t.Error("offline mode reported a completed check")
	}
	if z.Found {
		t.Error("offline mode invented an official zone")
	}
	if z.Err == "" {
		t.Error("offline mode must explain itself rather than returning a silent empty result")
	}
	if z.Citation.Live {
		t.Error("offline citation claims to be live")
	}
}

// TestOfficialPFZLive is the only test that touches the network. It asserts the
// two properties the UI depends on: a negative result is distinguishable from a
// failure, and any positive result carries a sector, a date and a citation.
func TestOfficialPFZLive(t *testing.T) {
	if testing.Short() {
		t.Skip("network test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	// Off Odisha, inside the ORISSA sector on the current bulletin.
	z := OfficialPFZ(ctx, 19.8, 85.9, 60)
	if !z.Checked {
		t.Fatalf("INCOIS bulletin was not reachable: %v", z.Err)
	}
	if z.Err != "" {
		t.Errorf("unexpected error on a reachable bulletin: %v", z.Err)
	}
	if !z.Found {
		t.Fatal("expected a published zone near the Odisha coast")
	}
	if z.DistanceKm <= 0 || z.DistanceKm > 120 {
		t.Errorf("implausible distance to the nearest published zone: %.1f km", z.DistanceKm)
	}
	if z.Label() == "" {
		t.Error("a found zone must name its sector")
	}
	if z.Bulletin.IsZero() {
		t.Error("a found zone must carry the day its advisory describes")
	}
	if z.Citation.Source != "INCOIS" || z.Citation.Dataset == "" || z.Citation.URL == "" {
		t.Errorf("incomplete citation: %+v", z.Citation)
	}

	// The Gujarat coast frequently has no published line. That must arrive as
	// "checked, nothing near", never as a transport error.
	g := OfficialPFZ(ctx, 21.0, 69.6, 60)
	if !g.Checked {
		t.Fatalf("Gujarat check did not complete: %v", g.Err)
	}
	if g.Found && g.DistanceKm > 600 {
		t.Errorf("widened search returned a zone %.0f km away; the cap is not working", g.DistanceKm)
	}
}

// TestSkeletonFoldsTransliterationVariants pins the behaviour the multilingual
// evaluation found the hard way: the same place is spelled several ways across
// scripts and keyboards, and a tool that only accepts the exact string tells a
// user who named the place correctly that it could not be found.
func TestSkeletonFoldsTransliterationVariants(t *testing.T) {
	same := [][]string{
		{"కోచి", "కోచీ", "కోచి"},     // Telugu long vs short i
		{"ಕೊಚಿ", "ಕೊಚ್ಚಿ", "ಕೊಚಿ"},   // Kannada geminate
		{"कोची", "कोच्ची", "कोच्चि"}, // Devanagari geminate
		{"കൊച്ചി", "കൊചി"},           // Malayalam geminate
	}
	for _, group := range same {
		want := skeleton(group[0])
		for _, variant := range group[1:] {
			if got := skeleton(variant); got != want {
				t.Errorf("skeleton(%q) = %q, want %q (same as %q)", variant, got, want, group[0])
			}
		}
	}
	// Latin must keep its vowels, or "Kochi" and "Koch" would reduce to the
	// same key and a name that merely starts the same would match.
	if sk := skeleton("Kochi"); sk != "Kochi" {
		t.Errorf("Latin names must keep their vowels: skeleton(%q) = %q", "Kochi", sk)
	}
	if skeleton("Kochi") == skeleton("Kochi-Marine")[:4] {
		t.Error("a longer name must not share a skeleton prefix with a shorter one")
	}
}

// TestFindTownAcrossTransliterations is the behavioural test. Each spelling is
// one a real user would type; every one must resolve to the same town.
func TestFindTownAcrossTransliterations(t *testing.T) {
	cases := []struct {
		query string
		want  string
	}{
		{"Is it safe to fish off Kochi tomorrow?", "Kochi"},
		{"కోచీ నుండి చేపల వెటేయడం సురక్షితమేమో?", "Kochi"},
		{"ಕೊಚ್ಚಿಯಿಂದ ಮೀನು ಹಿಡಿಯಬಹುದೇ?", "Kochi"},
		{"कोच्चीहून मासेमारीला जाणे सुरक्षित आहे का?", "Kochi"},
		{"കൊച്ചിയിൽ നിന്ന് മീൻ പിടിക്കാൻ പോകാമോ?", "Kochi"},
		{"चेन्नई में मछली पकड़ना सुरक्षित है?", "Chennai"},
		{"சென்னையில் இருந்து மீன் பிடிக்கலாமா?", "Chennai"},
		{"Puri ਤੋਂ ਮੱਛੀ ਪਕੜਨਾ ਸੁਰੱਖਿਅਤ ਹੈ?", "Puri"},
	}
	for _, tc := range cases {
		tn, ok := FindTownIn(tc.query, coastal)
		if !ok {
			t.Errorf("FindTownIn(%q) found nothing, want %s", tc.query, tc.want)
			continue
		}
		if !strings.EqualFold(tn.Name, tc.want) {
			t.Errorf("FindTownIn(%q) = %s, want %s", tc.query, tn.Name, tc.want)
		}
	}

	// The fallback must not invent places. A query with no coastal name in it,
	// even after reduction, still has to be refused.
	for _, q := range []string{
		"Is it safe to go out today?",
		"क्या कल बहुत तेज़ हवा चलेगी?",
		"Atlantis Bay tomorrow",
	} {
		if tn, ok := FindTownIn(q, coastal); ok {
			t.Errorf("FindTownIn(%q) invented %s", q, tn.Name)
		}
	}
}

// TestSkeletonMatchDoesNotMatchInsideWords guards the false positive that the
// multilingual evaluation surfaced: a bare substring search over the reduced
// query resolved a Telugu question about Kochi to Diu, because the Telugu word
// for "morning" (ఉదయం) contains the two consonants that Diu's Telugu name
// reduces to. Word anchoring is what separates the two.
func TestSkeletonMatchDoesNotMatchInsideWords(t *testing.T) {
	// "tomorrow morning, is it safe to fish from Kochi?" in Telugu.
	q := "రేపు ఉదయం కోచీ నుండి చేపల వెటేయడం సురక్షితమేమో?"
	tn, ok := FindTownIn(q, coastal)
	if !ok {
		t.Fatal("the Telugu Kochi question stopped resolving at all")
	}
	if !strings.EqualFold(tn.Name, "Kochi") {
		t.Errorf("FindTownIn = %s, want Kochi (Diu matched inside ఉదయం)", tn.Name)
	}

	// The same word in a sentence that does not name Kochi must not drag in Diu.
	onlyMorning := "రేపు ఉదయం వేడిగా ఉంటుంది"
	if tn, ok := FindTownIn(onlyMorning, coastal); ok {
		t.Errorf("FindTownIn(%q) invented %s from an ordinary word", onlyMorning, tn.Name)
	}

	// Attached grammatical forms must still resolve: Indic case and postposition
	// endings glue onto the place name, and those are real user input.
	for _, q := range []string{
		"कोच्चीहून मासेमारीला जाणे सुरक्षित आहे का?",
		"கொச்சியில் இருந்து மீன் பிடிக்கலாமா?",
		"ಕೊಚ್ಚಿಯಿಂದ ಮೀನು ಹಿಡಿಯಬಹುದೇ?",
		"കൊച്ചിയിൽ നിന്ന് മീൻ പിടിക്കാൻ പോകാമോ?",
	} {
		if tn, ok := FindTownIn(q, coastal); !ok || !strings.EqualFold(tn.Name, "Kochi") {
			t.Errorf("FindTownIn(%q) = %s/%v, want Kochi", q, tn.Name, ok)
		}
	}
}

// TestMeasureToUsesThePointItIsGiven pins the re-measurement that lets the
// bulletin be fetched in parallel and still be compared against the zone ORCA
// actually computed. Measuring against the waypoint and calling the result
// "our zone" would misreport the distance by however far the zone was fanned.
func TestMeasureToUsesThePointItIsGiven(t *testing.T) {
	// A single vertical line at longitude 80.5, running north-south, so the
	// expected distances can be reasoned about by hand.
	z := OfficialZone{
		Found:    true,
		lines:    [][][]float64{{{80.5, 10.0}, {80.5, 20.0}}},
		State:    "TAMIL NADU",
		Sector:   "3",
		Bulletin: bulletinDate(2026, "272"),
	}
	if !z.HasGeometry() {
		t.Fatal("HasGeometry must report true for a zone that retained its line")
	}

	at := func(lat, lon float64) float64 {
		return OfficialZone{lines: z.lines}.MeasureTo(lat, lon).DistanceKm
	}
	// On the line itself.
	if d := at(15.0, 80.5); d > 0.001 {
		t.Errorf("a point on the line measured %v km, want 0", d)
	}
	// Equator-ish: 0.1 degrees of longitude at 15N is about 10.7 km.
	lon1 := at(15.0, 80.6)
	if lon1 < 10.0 || lon1 > 11.5 {
		t.Errorf("0.1 deg east of the line measured %.2f km, want about 10.7", lon1)
	}
	// Moving the query point must move the measurement, which is the entire
	// point of MeasureTo.
	if lat := at(16.0, 80.5); lat != 0 {
		t.Errorf("a point further along the same line measured %v km, want 0", lat)
	}
	if far, near := at(15.0, 82.0), at(15.0, 80.7); far <= near {
		t.Errorf("MeasureTo is not tracking its argument: %.2f at 2 deg away, %.2f at 0.2 deg", far, near)
	}

	// A zone with no retained geometry cannot be re-measured, and must say so
	// rather than silently reporting the old number as if it were new.
	bare := OfficialZone{Found: true, DistanceKm: 42}
	if bare.HasGeometry() {
		t.Error("HasGeometry must be false when no line was retained")
	}
	if got := bare.MeasureTo(15, 80.5); got.DistanceKm != 42 {
		t.Errorf("MeasureTo altered a zone with no geometry: %v", got.DistanceKm)
	}
}

// TestPFZCacheReusesTheBulletin protects a public government endpoint from the
// load of one request per user query. The bulletin changes once a day, so a
// half-hour reuse window cannot return stale advice.
func TestPFZCacheReusesTheBulletin(t *testing.T) {
	if testing.Short() {
		t.Skip("live network")
	}
	before := PFZCacheSize()
	a := OfficialPFZ(t.Context(), 19.8, 85.9, PFZSearchRadiusKm)
	if !a.Checked {
		t.Fatalf("live check failed: %s", a.Err)
	}
	if PFZCacheSize() != before+1 {
		t.Fatalf("cache did not grow: %d -> %d", before, PFZCacheSize())
	}
	b := OfficialPFZ(t.Context(), 19.8, 85.9, PFZSearchRadiusKm)
	if PFZCacheSize() != before+1 {
		t.Error("a repeated query went to the network instead of the cache")
	}
	if a.Bulletin != b.Bulletin || a.DistanceKm != b.DistanceKm {
		t.Errorf("cached answer differs: %v/%v vs %v/%v", a.Bulletin, a.DistanceKm, b.Bulletin, b.DistanceKm)
	}
	// A different point must not read another point's cached answer.
	_ = OfficialPFZ(t.Context(), 9.9, 76.2, PFZSearchRadiusKm)
	if PFZCacheSize() != before+2 {
		t.Errorf("distinct points must cache separately: size %d", PFZCacheSize())
	}
}
