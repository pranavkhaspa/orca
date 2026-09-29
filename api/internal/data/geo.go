package data

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"orca/internal/domain"
)

// ---- Coastal reference table ---------------------------------------------

// Town is an approximate coastal reference point with the bearing from the
// coast toward open sea. Used as a deterministic fallback for geocoding and to
// project an offshore waypoint. Bearings are coarse and disclosed as such.
type Town struct {
	Name       string   `json:"name"`
	Aliases    []string `json:"aliases"`
	State      string   `json:"state"`
	Lat        float64  `json:"lat"`
	Lon        float64  `json:"lon"`
	BearingDeg float64  `json:"bearing_deg"`
	Coast      string   `json:"coast"`
}

var (
	townsOnce sync.Once
	towns     []Town
	townsErr  error
)

// Towns loads and caches the coastal reference table.
func Towns(path string) ([]Town, error) {
	townsOnce.Do(func() {
		b, err := readData(path)
		if err != nil {
			townsErr = fmt.Errorf("read coastal table: %w", err)
			return
		}
		var doc struct {
			Towns []Town `json:"towns"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			townsErr = fmt.Errorf("parse coastal table: %w", err)
			return
		}
		if len(doc.Towns) == 0 {
			townsErr = fmt.Errorf("coastal table is empty")
			return
		}
		towns = doc.Towns
	})
	return towns, townsErr
}

// NearestTown returns the closest reference point to a coordinate, along with
// the distance. Used to derive an offshore bearing for arbitrary coordinates.
func NearestTown(lat, lon float64, path string) (Town, float64, error) {
	ts, err := Towns(path)
	if err != nil {
		return Town{}, 0, err
	}
	best, bestD := Town{}, math.Inf(1)
	for _, t := range ts {
		if d := haversineKm(lat, lon, t.Lat, t.Lon); d < bestD {
			best, bestD = t, d
		}
	}
	return best, bestD, nil
}

// MatchTown resolves a free-text place name to a coastal reference point by
// substring match, case-insensitive. This is the offline path: it requires no
// network and therefore cannot fail during a live demo.
//
// Matching widens progressively — exact, alias, substring, token prefix, then a
// single-character typo — because the cost of a miss is high (the query falls
// through to a network geocoder that may return a village on the other side of
// the country) while the cost of a near-miss is only that we resolve to a town
// a few kilometres away.
func MatchTown(q, path string) (Town, bool) {
	ts, err := Towns(path)
	if err != nil {
		return Town{}, false
	}
	n := normName(q)
	if n == "" {
		return Town{}, false
	}

	// Two of the 31 names are multi-word: Car Nicobar and Port Blair. For those
	// two, only an exact match against the whole name or one of its aliases is
	// accepted, and the loose passes below are skipped.
	//
	// The reason is a real answer to a question nobody asked. "Is it safe at
	// Port of Nowhere?" matched the token "port" against the alias "portblair"
	// and returned a confident go verdict for Port Blair, in the Andaman
	// Islands, with live marine data and correct provenance. Every layer of the
	// response looked right. A substring rule cannot tell "Port Blair" from the
	// first half of it, so for these two names it is not allowed to try.
	//
	// The cost is that a user has to name the place fully, or use a distinctive
	// alias such as "nicobar". Both are reasonable, and the alternative is a
	// wrong ocean.
	exactOnly := func(t Town) bool { return multiWord(t.Name) }
	matches := func(t Town, accept func(cand, whole string) bool) bool {
		for _, cand := range append([]string{t.Name}, t.Aliases...) {
			if accept(normName(cand), normName(t.Name)) {
				return true
			}
		}
		return false
	}

	// 1. Exact name or alias.
	for _, t := range ts {
		if matches(t, func(cand, _ string) bool { return cand == n }) {
			return t, true
		}
	}

	// 2. Substring in either direction. This is what lets a user type
	// "mangalore" for Mangaluru or "thoothukudi" for Tuticorin, so it stays for
	// the single-word names that are the overwhelming majority.
	for _, t := range ts {
		if exactOnly(t) {
			continue
		}
		if matches(t, func(cand, _ string) bool {
			return containsStr(cand, n) || containsStr(n, cand)
		}) {
			return t, true
		}
	}

	// 3. A token of the query prefixes a town name, so "koch" finds Kochi.
	for _, tok := range strings.Fields(n) {
		if len(tok) < 3 {
			continue
		}
		for _, t := range ts {
			if exactOnly(t) {
				continue
			}
			if matches(t, func(cand, _ string) bool { return strings.HasPrefix(cand, tok) }) {
				return t, true
			}
		}
	}

	// 4. One-character typos, which is how place names actually get typed. Only
	// for tokens long enough that a single edit is unlikely to collide, and
	// never for a multi-word name, where an edit in one half is indistinguishable
	// from a different place.
	for _, tok := range strings.Fields(n) {
		if len(tok) < 5 {
			continue
		}
		for _, t := range ts {
			if exactOnly(t) {
				continue
			}
			if matches(t, func(cand, _ string) bool { return withinOneEdit(cand, tok) }) {
				return t, true
			}
		}
	}
	return Town{}, false
}

// FindTownIn scans a whole free-text query for any known place name or alias and
// returns the best match.
//
// This exists because MatchTown compares normalised latin text, which cannot see
// a place name written in Tamil or Devanagari. A substring scan over the raw
// query works across every script, which is what makes the offline router able
// to resolve a question like "கொச்சி நாளை மீன் பிடிக்க போகலாமா" with no model call.
func FindTownIn(text, path string) (Town, bool) {
	ts, err := Towns(path)
	if err != nil {
		return Town{}, false
	}
	q := strings.ToLower(text)
	if strings.TrimSpace(q) == "" {
		return Town{}, false
	}
	// Pass 1: names of four or more characters, anchored to the start of a
	// word. This is what finds a native-script place name, where the same
	// characters also occur inside longer words.
	//
	// The anchor is what keeps this honest. Scanning for the alias anywhere in
	// the string resolved a Telugu sentence meaning "tomorrow morning it will
	// be hot" to Digha, because that town's Telugu name డిగా is also the tail of
	// the word for "hot" వేడిగా. Anchoring to a word start accepts the attached
	// forms that grammar produces — கொச்சியில், कोच्चीहून — while refusing a
	// place name that merely happens to end another word.
	best, bestLen := Town{}, 0
	for _, t := range ts {
		for _, cand := range append([]string{t.Name}, t.Aliases...) {
			c := strings.ToLower(strings.TrimSpace(cand))
			if len([]rune(c)) < 4 {
				continue
			}
			if startsWord(q, c) && len([]rune(c)) > bestLen {
				best, bestLen = t, len([]rune(c))
			}
		}
	}
	if bestLen > 0 {
		return best, true
	}

	// Pass 2: short names such as "Goa" or "Diu" are excluded from substring
	// matching because they occur inside ordinary words far too often. They are
	// accepted only as a whole word, which is still unambiguous for a real place
	// name typed into a question.
	for _, t := range ts {
		for _, cand := range append([]string{t.Name}, t.Aliases...) {
			c := strings.ToLower(strings.TrimSpace(cand))
			if c == "" {
				continue
			}
			if containsWord(q, c) {
				return t, true
			}
		}
	}
	// Pass 3: consonant-skeleton matching, anchored to word starts. The same
	// place name is written several ways in practice — కోచీ, కోచి, ಕೊಚ್ಚಿ,
	// ಕೊಚಿ, कोची, कोच्ची all mean Kochi, and a user types whichever
	// transliteration their keyboard or dictionary offers. Requiring the alias
	// to appear byte-for-byte answers "I could not find that place" to someone
	// who named it perfectly clearly, which is the worst possible failure for a
	// safety tool serving ten scripts.
	//
	// The anchor matters. An earlier version searched the whole reduced query as
	// a substring, and that matched Diu inside the Telugu word for "morning":
	// ఉదయం reduces to ఉదయ, which contains దయ, which is exactly what దియు (Diu)
	// reduces to. Matching a reduced form anywhere in a sentence is matching
	// noise. Requiring the alias to be a prefix of a whole word fixes it — no
	// word in that sentence begins దయ — while still matching the attached forms
	// that Indic grammar produces, such as कोच्चीहून or கொச்சியில்.
	tokens := skeletonTokens(q)
	skBest, skBestLen := Town{}, 0
	for _, t := range ts {
		for _, cand := range append([]string{t.Name}, t.Aliases...) {
			c := skeleton(strings.ToLower(strings.TrimSpace(cand)))
			if len(c) < minSkeleton || len(c) <= skBestLen {
				continue
			}
			for _, tok := range tokens {
				if strings.HasPrefix(tok, c) {
					skBest, skBestLen = t, len(c)
					break
				}
			}
		}
	}
	if skBestLen > 0 {
		return skBest, true
	}
	return Town{}, false
}

// minSkeleton is the shortest reduced form accepted as a place name. Two
// consonants is already generous — ఉదయం alone is three — and requiring more
// would reject the short but real names like కోచి, which reduces to కచ.
const minSkeleton = 2

// skeletonTokens reduces each whitespace- or punctuation-delimited word
// separately, so a match can be anchored to a word rather than to arbitrary
// letters inside a sentence.
func skeletonTokens(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	}) {
		if sk := skeleton(f); len(sk) > 0 {
			out = append(out, sk)
		}
	}
	return out
}

// skeleton reduces a name to its consonants, dropping the vowel signs that
// transliteration varies between.
//
// Vowel signs in Indic scripts are combining marks (categories Mn and Mc), so
// removing them leaves the consonantal frame: both కోచీ and కోచి reduce to కచ.
// Runs of an identical consonant are then collapsed, which folds the geminate
// in ಕೊಚ್ಚಿ or कोच्ची into the same key. Latin text is left alone, because
// stripping its vowels would make "Kochi" and "Koch" indistinguishable; the
// skeleton is only ever used as a fallback after exact matching has failed.
func skeleton(s string) string {
	var b strings.Builder
	var last rune
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) {
			continue
		}
		if !unicode.IsLetter(r) {
			r = ' '
		}
		if r == ' ' {
			last = 0
			b.WriteRune(r)
			continue
		}
		if r != last {
			b.WriteRune(r)
			last = r
		}
	}
	return b.String()
}

// containsWord reports whether needle appears in hay delimited by a non-letter,
// non-digit boundary. The needle may contain internal punctuation, as
// "port blair" does.
func containsWord(hay, needle string) bool {
	boundary := isBoundary
	runes := []rune(hay)
	n := []rune(needle)
	if len(n) == 0 || len(n) > len(runes) {
		return false
	}
	for i := 0; i+len(n) <= len(runes); i++ {
		match := true
		for j := range n {
			if runes[i+j] != n[j] {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		beforeOK := i == 0 || boundary(runes[i-1])
		after := i + len(n)
		afterOK := after == len(runes) || boundary(runes[after])
		if beforeOK && afterOK {
			return true
		}
	}
	return false
}

// startsWord reports whether needle occurs at the start of a word, allowing it
// to run into the letters that follow. That is the difference between "a place
// name, possibly with a grammatical ending" and "a place name standing alone":
// the first is what a typed question looks like, the second excludes the
// attached forms that Indic morphology produces.
func startsWord(hay, needle string) bool {
	runes := []rune(hay)
	n := []rune(needle)
	if len(n) == 0 || len(n) > len(runes) {
		return false
	}
	for i := 0; i+len(n) <= len(runes); i++ {
		if i > 0 && !isBoundary(runes[i-1]) {
			continue
		}
		match := true
		for j := range n {
			if runes[i+j] != n[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// isBoundary reports whether r separates words: it is not a letter and not a
// digit, in any of the scripts ORCA serves.
func isBoundary(r rune) bool {
	return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
		(r >= 'ऀ' && r <= 'ॿ') || // Devanagari
		(r >= 'ঀ' && r <= '৿') || // Bengali
		(r >= '਀' && r <= '੿') || // Gurmukhi
		(r >= '઀' && r <= '૿') || // Gujarati
		(r >= '଀' && r <= '୿') || // Oriya
		(r >= '஀' && r <= '௿') || // Tamil
		(r >= 'ఀ' && r <= '౿') || // Telugu
		(r >= 'ಀ' && r <= '೿') || // Kannada
		(r >= 'ഀ' && r <= 'ൿ')) // Malayalam
}

// normName lowercases and strips punctuation and spaces so that "Kochi, Kerala"
// and "kochi kerala" compare equal.
// multiWord reports whether a place name is more than one word, which is what
// makes a partial prefix ambiguous.
func multiWord(s string) bool {
	return len(strings.Fields(strings.TrimSpace(s))) > 1
}

func normName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func containsStr(hay, needle string) bool {
	return len(needle) > 0 && strings.Contains(hay, needle)
}

// withinOneEdit reports whether b can be reached from a by a single insertion,
// deletion, substitution or transposition. This is the classic Damerau-Levenshtein
// distance of 1, chosen because a single keystroke is the realistic error.
func withinOneEdit(a, b string) bool {
	la, lb := len(a), len(b)
	if abs(la-lb) > 1 {
		return false
	}
	switch {
	case la == lb:
		diff := 0
		for i := 0; i < la; i++ {
			if a[i] != b[i] {
				diff++
				if diff > 1 {
					return false
				}
			}
		}
		// One substitution, or one adjacent transposition.
		return diff <= 1 || isTransposition(a, b)
	case la == lb+1:
		return oneGap(a, b)
	default:
		return oneGap(b, a)
	}
}

func oneGap(long, short string) bool {
	i, j, skipped := 0, 0, false
	for i < len(long) && j < len(short) {
		if long[i] == short[j] {
			i++
			j++
			continue
		}
		if skipped {
			return false
		}
		skipped = true
		i++
	}
	return true
}

func isTransposition(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	i := -1
	for k := 0; k < len(a); k++ {
		if a[k] == b[k] {
			continue
		}
		if i >= 0 {
			// Exactly two positions differ, and they must be a swap.
			return k == i+1 && a[i] == b[k] && a[k] == b[i]
		}
		i = k
	}
	return false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ---- Geocoding ------------------------------------------------------------

const geocodeBase = "https://geocoding-api.open-meteo.com/v1/search"

// Geocode resolves a place name to coordinates.
//
// Ordering is deliberate: the coastal table is consulted first because marine
// reasoning is only meaningful near a coast, and a table hit gives us the
// offshore bearing for free. The network call is the fallback, which means the
// common case is both more accurate for our purpose and immune to network
// failure.
func Geocode(ctx context.Context, query, coastalPath string) domain.Geo {
	q := strings.TrimSpace(query)
	if q == "" {
		return domain.Geo{}
	}
	if t, ok := MatchTown(q, coastalPath); ok {
		return domain.Geo{
			Query: q, Name: t.Name, Lat: t.Lat, Lon: t.Lon,
			Source: "ORCA coastal reference table",
		}
	}

	qs := url.Values{}
	qs.Set("name", q)
	qs.Set("count", "1")
	qs.Set("language", "en")
	qs.Set("format", "json")
	endpoint := geocodeBase + "?" + qs.Encode()

	var resp struct {
		Results []struct {
			Name      string  `json:"name"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Country   string  `json:"country"`
		} `json:"results"`
	}
	if err := getJSON(ctx, endpoint, &resp); err != nil || len(resp.Results) == 0 {
		return domain.Geo{Query: q, Source: "unresolved"}
	}
	r := resp.Results[0]
	return domain.Geo{Query: q, Name: r.Name, Lat: r.Latitude, Lon: r.Longitude, Source: "Open-Meteo Geocoding"}
}

// ProjectWaypoint moves from a coastal origin out to open sea along a bearing.
// Sampling conditions 200 km inland produces nonsense, so the marine and
// weather agents are always pointed at the waypoint rather than the coast.
func ProjectWaypoint(lat, lon, bearingDeg, distKm float64) (float64, float64) {
	const earthKm = 6371.0
	br := bearingDeg * math.Pi / 180
	dr := distKm / earthKm
	lat1 := lat * math.Pi / 180
	lon1 := lon * math.Pi / 180
	lat2 := math.Asin(math.Sin(lat1)*math.Cos(dr) + math.Cos(lat1)*math.Sin(dr)*math.Cos(br))
	lon2 := lon1 + math.Atan2(math.Sin(br)*math.Sin(dr)*math.Cos(lat1), math.Cos(dr)-math.Sin(lat1)*math.Sin(lat2))
	return lat2 * 180 / math.Pi, math.Mod(lon2*180/math.Pi+540, 360) - 180
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371.0
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp := (lat2 - lat1) * math.Pi / 180
	dl := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * r * math.Asin(math.Sqrt(math.Min(1, a)))
}

// HaversineKm is exported for callers outside the package.
func HaversineKm(lat1, lon1, lat2, lon2 float64) float64 { return haversineKm(lat1, lon1, lat2, lon2) }

// GeoNames lists the known reference names, used by the UI to offer suggestions
// without a round trip.
func GeoNames(path string) []string {
	ts, err := Towns(path)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Name)
	}
	sort.Strings(out)
	return out
}

var _ = strconv.Itoa
