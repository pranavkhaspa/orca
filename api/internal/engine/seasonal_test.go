package engine

import (
	"strings"
	"testing"
	"time"

	"orca/internal/domain"
)

// ist builds an instant in IST from a date, so the tests exercise the same
// timezone conversion the engine uses on a request.
func ist(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 12, 0, 0, 0, IST)
}

func kerala(lo, hi string) []domain.BanWindow {
	return []domain.BanWindow{{
		State: "Kerala", Authority: "Department of Fisheries, Government of Kerala",
		StartMD: lo, EndMD: hi, AppliesTo: "mechanised boats",
		Basis: "test", Confidence: "high", Caveat: "test caveat",
	}}
}

func TestSeasonalBanInsideWindowIsCritical(t *testing.T) {
	got := SeasonalBan("Kerala", ist(2026, time.June, 20), kerala("06-10", "07-15"))
	if !got.Known || !got.InBan {
		t.Fatalf("20 June should be inside 10 Jun-15 Jul, got %+v", got)
	}
	if got.Severity != SevCrit || got.Level != "season closed" {
		t.Errorf("in-window severity = %s/%s, want critical/season closed", got.Severity, got.Level)
	}
	if !got.Indicative {
		t.Error("every status must be flagged indicative")
	}
	// The message has to name the authority and carry the verification caveat,
	// because that is the difference between advice and a gazetted fact.
	for _, want := range []string{"Government of Kerala", "mechanised boats", "test caveat"} {
		if !strings.Contains(got.Detail, want) {
			t.Errorf("detail is missing %q: %s", want, got.Detail)
		}
	}
}

func TestSeasonalBanBoundariesAreInclusive(t *testing.T) {
	table := kerala("06-10", "07-15")
	for _, c := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{"one day before start", ist(2026, time.June, 9), false},
		{"on start", ist(2026, time.June, 10), true},
		{"on end", ist(2026, time.July, 15), true},
		{"one day after end", ist(2026, time.July, 16), false},
	} {
		if got := SeasonalBan("Kerala", c.at, table); got.InBan != c.want {
			t.Errorf("%s: in_ban=%v, want %v (%s)", c.name, got.InBan, c.want, got.Level)
		}
	}
}

// A ban window is a local-calendar date range. A UTC instant late on the last
// evening of the window in IST must still read as inside it.
func TestSeasonalBanEvaluatesInISTNotUTC(t *testing.T) {
	table := kerala("06-10", "07-15")
	// 15 July 23:30 IST is 18:00 UTC on the same day.
	late := time.Date(2026, 7, 15, 23, 30, 0, 0, IST)
	if !SeasonalBan("Kerala", late, table).InBan {
		t.Error("23:30 IST on the final day must be inside the window")
	}
	// 15 July 19:00 UTC is 00:30 IST on 16 July, the day after the window.
	justAfter := time.Date(2026, 7, 15, 19, 0, 0, 0, time.UTC)
	if SeasonalBan("Kerala", justAfter, table).InBan {
		t.Error("00:30 IST on 16 July must be outside the window")
	}
}

func TestSeasonalBanWrapsYearBoundary(t *testing.T) {
	table := kerala("11-15", "02-15")
	for _, c := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{"in November", ist(2026, time.November, 20), true},
		{"in December", ist(2026, time.December, 25), true},
		{"in January", ist(2026, time.January, 5), true},
		{"in February", ist(2026, time.February, 10), true},
		{"just after end", ist(2026, time.February, 20), false},
		{"just before start", ist(2026, time.November, 10), false},
		{"in June", ist(2026, time.June, 1), false},
	} {
		if got := SeasonalBan("Kerala", c.at, table); got.InBan != c.want {
			t.Errorf("%s: in_ban=%v, want %v (%s)", c.name, got.InBan, c.want, got.Level)
		}
	}
}

// A date must mean the same day-of-year index in a leap year and a non-leap
// year, or the ban would drift by a day every four years.
func TestSeasonalBanIndexIsYearStable(t *testing.T) {
	nonLeap := banDayOfYear(ist(2026, time.July, 15))
	leap := banDayOfYear(ist(2024, time.July, 15))
	if nonLeap != leap {
		t.Errorf("15 July index drifted: 2026=%d 2024=%d", nonLeap, leap)
	}
	mar := func(y int) int { return banDayOfYear(ist(y, time.March, 1)) }
	if mar(2023) != mar(2024) || mar(2024) != mar(2025) {
		t.Errorf("1 March index is not year-stable: 2023=%d 2024=%d 2025=%d", mar(2023), mar(2024), mar(2025))
	}
	jan := func(y int) int { return banDayOfYear(ist(y, time.January, 1)) }
	if jan(2023) != 0 || jan(2024) != 0 {
		t.Errorf("1 January should be index 0 in every year: 2023=%d 2024=%d", jan(2023), jan(2024))
	}
	// A window boundary must not shift between a leap year and a non-leap one.
	if SeasonalBan("Kerala", ist(2024, time.June, 9), kerala("06-10", "07-15")).InBan {
		t.Error("9 June 2024 (leap year) must be outside a 10 June window")
	}
	if !SeasonalBan("Kerala", ist(2024, time.June, 10), kerala("06-10", "07-15")).InBan {
		t.Error("10 June 2024 (leap year) must be inside a 10 June window")
	}
}

// February 29 cannot recur, so a window bound on it is rejected rather than
// silently folded onto the 28th.
func TestParseMDRejectsNonRecurringDates(t *testing.T) {
	if _, err := parseMD("02-29"); err == nil {
		t.Error("02-29 must be rejected: it does not occur every year")
	}
	if _, err := parseMD("13-01"); err == nil {
		t.Error("month 13 must be rejected")
	}
	if _, err := parseMD("02-30"); err == nil {
		t.Error("30 February must be rejected")
	}
	if _, err := parseMD("June"); err == nil {
		t.Error("a non MM-DD string must be rejected")
	}
	if _, err := parseMD("02-28"); err != nil {
		t.Errorf("28 February is a real recurring date, got %v", err)
	}
}

// A state with no calendar entry must not be reported as an open season.
func TestSeasonalBanUnknownStateDrawsNoConclusion(t *testing.T) {
	got := SeasonalBan("Nowhere", ist(2026, time.June, 20), kerala("06-10", "07-15"))
	if got.Known || got.InBan {
		t.Fatalf("an unlisted state must not be known, got %+v", got)
	}
	if got.Severity != SevOK {
		t.Errorf("an unlisted state must not escalate, got %s", got.Severity)
	}
	if !strings.Contains(got.Detail, "no conclusion is drawn") {
		t.Errorf("detail must say no conclusion is drawn: %s", got.Detail)
	}
}

// A malformed row is a data defect. The engine must report the gap rather than
// guess at a date, because a guessed legal date is worse than none.
func TestSeasonalBanMalformedRowDrawsNoConclusion(t *testing.T) {
	for _, bad := range []domain.BanWindow{
		{State: "Kerala", Authority: "A", StartMD: "02-29", EndMD: "07-15", Confidence: "high"},
		{State: "Kerala", Authority: "A", StartMD: "06-10", EndMD: "not-a-date", Confidence: "high"},
	} {
		got := SeasonalBan("Kerala", ist(2026, time.June, 20), []domain.BanWindow{bad})
		if got.Known || got.InBan {
			t.Errorf("malformed row %+v was treated as a known window: %+v", bad, got)
		}
		if !strings.Contains(got.Detail, "could not be read") {
			t.Errorf("malformed row detail must disclose the defect: %s", got.Detail)
		}
	}
}

func TestSeasonalBanApproachingWindowIsCaution(t *testing.T) {
	// 1 June, five days before a 6 June opening.
	got := SeasonalBan("Kerala", ist(2026, time.June, 1), kerala("06-06", "07-15"))
	if got.Severity != SevCaution || got.Level != "season opening" {
		t.Errorf("5 days out: %s/%s, want caution/season opening", got.Severity, got.Level)
	}
	if got.DaysTo != 5 {
		t.Errorf("days_to = %d, want 5", got.DaysTo)
	}
	// Well outside the warning window.
	far := SeasonalBan("Kerala", ist(2026, time.March, 1), kerala("06-06", "07-15"))
	if far.Severity != SevOK || far.Level != "season open" {
		t.Errorf("far from the window: %s/%s, want ok/season open", far.Severity, far.Level)
	}
}

func TestSeasonalBanJustClosedIsCaution(t *testing.T) {
	got := SeasonalBan("Kerala", ist(2026, time.July, 20), kerala("06-10", "07-15"))
	if got.Severity != SevCaution || got.Level != "season recent" {
		t.Errorf("5 days after close: %s/%s, want caution/season recent", got.Severity, got.Level)
	}
}

// A low-confidence window must say so, or a state we know little about reads
// with the same authority as a state whose pattern is stable.
func TestSeasonalBanSurfacesLowConfidence(t *testing.T) {
	table := []domain.BanWindow{{
		State: "Lakshadweep", Authority: "Lakshadweep Administration",
		StartMD: "06-01", EndMD: "06-30", AppliesTo: "all craft",
		Basis: "set close to the season", Confidence: "low", Caveat: "verify",
	}}
	got := SeasonalBan("Lakshadweep", ist(2026, time.June, 10), table)
	if !strings.Contains(got.Detail, "weak indicator") {
		t.Errorf("low confidence must be disclosed in the message: %s", got.Detail)
	}
	// A missing grade must not be read as high.
	table[0].Confidence = ""
	if got := SeasonalBan("Lakshadweep", ist(2026, time.June, 10), table); !strings.Contains(got.Detail, "weak indicator") {
		t.Errorf("an ungraded window must be disclosed as uncertain: %s", got.Detail)
	}
}

func TestSeasonalBanIsCaseInsensitive(t *testing.T) {
	table := kerala("06-10", "07-15")
	for _, q := range []string{"Kerala", "kerala", "  KERALA  "} {
		if !SeasonalBan(q, ist(2026, time.June, 20), table).InBan {
			t.Errorf("state %q should match the calendar", q)
		}
	}
}

func TestSeasonalBanIsDeterministic(t *testing.T) {
	table := kerala("06-10", "07-15")
	at := ist(2026, time.June, 20)
	first := SeasonalBan("Kerala", at, table)
	for i := 0; i < 50; i++ {
		got := SeasonalBan("Kerala", at, table)
		if got != first {
			t.Fatalf("run %d differed:\n got %+v\nwant %+v", i, got, first)
		}
	}
}

// A closed season is a legal prohibition, so it has to be able to produce a
// no-go on its own, in calm weather.
func TestAssessWithBanClosedSeasonForcesNoGo(t *testing.T) {
	ban := SeasonalBan("Kerala", ist(2026, time.June, 20), kerala("06-10", "07-15"))
	v := AssessWithBan(
		domain.Marine{WaveHeightM: 0.2, SSTC: 26},
		domain.Weather{GustKmh: 5},
		domain.PFZ{Score: 0.9, Confidence: "high"},
		ban)
	if v.Level != "no-go" || v.Severity != SevCrit {
		t.Errorf("a closed season must force no-go, got %s/%s", v.Level, v.Severity)
	}
	var found bool
	for _, h := range v.Hazards {
		if h.Kind == "season" {
			found = true
			if h.Severity != SevCrit {
				t.Errorf("season hazard severity = %s, want critical", h.Severity)
			}
		}
	}
	if !found {
		t.Error("the season must appear as its own hazard, not folded into the weather")
	}
}

// An approaching season raises a caution but must not overrule a no-go that the
// weather already earned.
func TestAssessWithBanCautionDoesNotDowngradeNoGo(t *testing.T) {
	ban := SeasonalBan("Kerala", ist(2026, time.June, 1), kerala("06-06", "07-15"))
	v := AssessWithBan(
		domain.Marine{WaveHeightM: 5.0, SSTC: 26},
		domain.Weather{GustKmh: 5},
		domain.PFZ{Score: 0.9, Confidence: "high"},
		ban)
	if v.Level != "no-go" {
		t.Errorf("a bad sea must stay no-go, got %s", v.Level)
	}
}

func TestAssessWithBanUnknownCalendarAddsNoHazard(t *testing.T) {
	plain := Assess(
		domain.Marine{WaveHeightM: 0.2, SSTC: 26},
		domain.Weather{GustKmh: 5},
		domain.PFZ{Score: 0.9, Confidence: "high"})
	got := AssessWithBan(
		domain.Marine{WaveHeightM: 0.2, SSTC: 26},
		domain.Weather{GustKmh: 5},
		domain.PFZ{Score: 0.9, Confidence: "high"},
		UnknownBan)
	if got.Level != plain.Level || len(got.Hazards) != len(plain.Hazards) {
		t.Errorf("an unknown calendar must not change the verdict:\n got %s/%d\nwant %s/%d",
			got.Level, len(got.Hazards), plain.Level, len(plain.Hazards))
	}
}

func TestValidWindowRejectsSingleDay(t *testing.T) {
	if _, _, err := ValidWindow(domain.BanWindow{StartMD: "06-10", EndMD: "06-10"}, 2026); err == nil {
		t.Error("a one-day window should be rejected as implausible")
	}
	if _, _, err := ValidWindow(domain.BanWindow{StartMD: "06-10", EndMD: "07-15"}, 2026); err != nil {
		t.Errorf("a valid window was rejected: %v", err)
	}
}

func TestDaysForwardWraps(t *testing.T) {
	for _, c := range []struct{ from, to, want int }{
		{0, 0, 0}, {0, 1, 1}, {364, 0, 1}, {10, 5, 360}, {100, 100, 0},
	} {
		if got := daysForward(c.from, c.to); got != c.want {
			t.Errorf("daysForward(%d,%d) = %d, want %d", c.from, c.to, got, c.want)
		}
	}
}
