package engine

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"orca/internal/domain"
)

// Seasonal fishing-ban calendar.
//
// A ban is a legal prohibition, not a weather condition, and the two must never
// be confused: a calm sea during the closed season is still a closed season.
// This file is therefore separate from hazard.go rather than folded into it, and
// it is evaluated in state-local time because a ban boundary is a date on a
// local calendar, not an instant on a UTC one.
//
// The honesty constraint is the important part. Every state issues its ban
// window each year by gazette notification, and those dates are not always the
// same as the year before. This engine therefore stores the *recurring
// statutory pattern* for each state and labels every result as indicative. It
// is a reason to check the gazette, never a substitute for reading it. A system
// that told a fisher that a date was a closed season, when it had merely
// inferred one from last year's pattern, would be making a legal claim it
// cannot support — and getting it wrong costs a fine or a boat.
//
// The engine performs no I/O and calls no model. Given the same table and the
// same instant it always returns the same status.

// IST is India's standard time as a fixed offset.
//
// A fixed zone is used rather than time.LoadLocation("Asia/Kolkata") because a
// fixed zone needs no timezone database at run time. A container image built
// without tzdata would otherwise fail to load the location and take the verdict
// down with it, and the offset has not varied since 1945.
var IST = time.FixedZone("IST", 5*3600+30*60)

// BanStatus is the evaluated ban position for a state at an instant.
type BanStatus struct {
	// Known is false when the state has no entry in the table. Absence of a
	// table entry is never read as "no ban"; it is reported as unknown.
	Known bool `json:"known"`
	// InBan is true only when the instant falls inside a known window.
	InBan bool `json:"in_ban"`
	// Severity is SevCrit inside a window, SevCaution near one, else SevOK.
	Severity string `json:"severity"`
	// Level is a short human label for the UI.
	Level string `json:"level"`
	// Detail is the sentence shown to the user.
	Detail string `json:"detail"`
	// DaysTo is the days until the next window opens, or since it opened while
	// a window is running. It is 0 when Known is false.
	DaysTo int `json:"days_to"`
	// Window is the pattern that produced the status.
	Window domain.BanWindow `json:"window"`
	// Indicative is always true. It exists as an explicit field rather than
	// prose so a client can render the caveat without having to parse Detail.
	Indicative bool `json:"indicative"`
}

// UnknownBan is the status for a state with no table entry.
//
// It is a package-level value rather than a fresh allocation because it is
// immutable and used on every request for a place outside the table.
var UnknownBan = BanStatus{
	Known: false, Severity: SevOK, Level: "not covered",
	Detail: "No seasonal ban calendar is bundled for this state, so no conclusion " +
		"is drawn either way. Check the state fisheries department for the closed season.",
}

// BanWarnDays is how far outside a window the engine raises a heads-up.
//
// A fortnight is the right scale for a small fishing operation: it is long
// enough to plan a different trip around and short enough to avoid crying wolf
// for most of the year.
const BanWarnDays = 14

// banDayOfYear is the 0-based day index of t within its year, in IST.
func banDayOfYear(t time.Time) int {
	y, d := t.In(IST).Year(), t.In(IST).YearDay()
	return dayOfYearToIndex(y, d)
}

// dayOfYearToIndex converts a 1-based year-day to a 0-based index that means
// the same calendar date in every year.
//
// A raw year-day cannot be used, because it shifts by one after 29 February. In
// a non-leap year 1 March is day 60; in a leap year it is day 61, so a window
// stored as a raw day number would start a day late in leap years and slowly
// drift. The index is therefore "days since 1 January, ignoring 29 February",
// which keeps every date from 1 March onward at a constant index across both
// year types: 1 March is always 59, and 15 July is always the same number.
//
// 29 February itself shares an index with 1 March. That is deliberate and
// harmless, because no window bound can be 29 February (parseMD rejects it), so
// the only way to reach that index is a real 29 February instant, which is
// treated as 1 March. A one-day ambiguity on a date nobody can specify is a
// better failure than a year-dependent drift on dates that matter.
func dayOfYearToIndex(year, yearDay int) int {
	leap := year%4 == 0 && (year%100 != 0 || year%400 == 0)
	// 60 is 29 February; 61 is 1 March in a leap year. Subtracting the extra
	// day only once the year has actually passed it keeps March in step.
	if leap && yearDay >= 61 {
		return yearDay - 2
	}
	return yearDay - 1
}

// parseMD parses a "MM-DD" bound into a 0-based annual day index.
//
// February 29 cannot be expressed, so 02-29 is rejected rather than silently
// folded onto 28 February. A silently remapped date in a legal calendar is
// exactly the class of quiet wrongness this codebase is built to avoid.
func parseMD(s string) (int, error) {
	parts := strings.SplitN(strings.TrimSpace(s), "-", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("seasonal ban: %q is not MM-DD", s)
	}
	mon, err1 := strconv.Atoi(parts[0])
	day, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, fmt.Errorf("seasonal ban: %q is not MM-DD", s)
	}
	if mon < 1 || mon > 12 || day < 1 {
		return 0, fmt.Errorf("seasonal ban: %q has an out-of-range month or day", s)
	}
	if mon == 2 && day > 28 {
		return 0, fmt.Errorf("seasonal ban: %q is not a date that occurs every year", s)
	}
	if _, err := time.Parse("2006-01-02", fmt.Sprintf("2001-%02d-%02d", mon, day)); err != nil {
		return 0, fmt.Errorf("seasonal ban: %q is not a real date", s)
	}
	return dayOfYearToIndex(2001, time.Date(2001, time.Month(mon), day, 0, 0, 0, 0, IST).YearDay()), nil
}

// ValidWindow checks that a table row is usable, and reports the window it
// resolves to for a given year.
//
// The loader calls this so a defective row fails at startup, where a test can
// see it, rather than on a live request. The engine re-parses at evaluation time
// regardless, because a table could in principle be swapped at run time.
func ValidWindow(w domain.BanWindow, year int) (startDay, endDay int, err error) {
	start, err := parseMD(w.StartMD)
	if err != nil {
		return 0, 0, err
	}
	end, err := parseMD(w.EndMD)
	if err != nil {
		return 0, 0, err
	}
	if start == end {
		return 0, 0, fmt.Errorf("window %s to %s covers a single day; a closed season is not one day",
			w.StartMD, w.EndMD)
	}
	return start, end, nil
}

// daysForward returns the distance from one annual day index to another,
// wrapping at the year boundary, in 0..365.
func daysForward(from, to int) int {
	d := to - from
	if d < 0 {
		d += 365
	}
	return d
}

// inWindow reports whether day falls inside the inclusive window, and whether
// the window wraps the year boundary.
func inWindow(day, start, end int) (inside bool, wraps bool) {
	if start <= end {
		return day >= start && day <= end, false
	}
	// Wraps: e.g. 15 November to 15 February.
	return day >= start || day <= end, true
}

// SeasonalBan evaluates the ban position for a state at an instant.
//
// A nil or missing entry yields UnknownBan. The function is total: any state,
// any instant, and any table produce a status, so a malformed row degrades to
// "unknown" rather than taking the request down.
func SeasonalBan(state string, at time.Time, table []domain.BanWindow) BanStatus {
	if strings.TrimSpace(state) == "" {
		return UnknownBan
	}
	day := banDayOfYear(at)
	for _, w := range table {
		if !strings.EqualFold(strings.TrimSpace(w.State), strings.TrimSpace(state)) {
			continue
		}
		start, err1 := parseMD(w.StartMD)
		end, err2 := parseMD(w.EndMD)
		if err1 != nil || err2 != nil {
			// A bad row is a data defect, not a safety signal. Report the gap
			// instead of guessing at a date.
			s := UnknownBan
			s.Detail = fmt.Sprintf(
				"The bundled ban calendar for %s could not be read (%v), so no conclusion is drawn. "+
					"Check the state fisheries department for the closed season.", state, firstErr(err1, err2))
			return s
		}

		inside, wraps := inWindow(day, start, end)
		st := BanStatus{
			Known: true, InBan: inside, Window: w, Indicative: true,
		}

		if inside {
			st.Severity = SevCrit
			st.Level = "season closed"
			// While the window runs, the countdown describes how long it has
			// been running rather than how long until it reopens.
			st.DaysTo = daysForward(start, day)
			st.Detail = fmt.Sprintf(
				"Within the typical %s closed season for %s (%s to %s), which covers %s. %s %s",
				w.Authority, w.State, prettyMD(w.StartMD), prettyMD(w.EndMD), w.AppliesTo,
				confidenceClause(w.Confidence), w.Caveat)
			return st
		}

		st.DaysTo = daysForward(day, start)
		// daysFromEnd is measured from the window's end, which is the
		// meaningful direction when the window has just closed.
		since := daysForward(end, day)
		switch {
		case st.DaysTo <= BanWarnDays:
			st.Severity = SevCaution
			st.Level = "season opening"
			st.Detail = fmt.Sprintf(
				"The typical %s closed season for %s opens in %d day(s), on or about %s. %s",
				w.Authority, w.State, st.DaysTo, prettyMD(w.StartMD), w.Caveat)
		case since <= BanWarnDays && !wraps:
			st.Severity = SevCaution
			st.Level = "season recent"
			st.Detail = fmt.Sprintf(
				"The typical %s closed season for %s ended %d day(s) ago, on or about %s. %s",
				w.Authority, w.State, since, prettyMD(w.EndMD), w.Caveat)
		default:
			st.Severity = SevOK
			st.Level = "season open"
			st.Detail = fmt.Sprintf(
				"Outside the typical %s closed season for %s (%s to %s). %s %s",
				w.Authority, w.State, prettyMD(w.StartMD), prettyMD(w.EndMD),
				confidenceClause(w.Confidence), w.Caveat)
		}
		return st
	}
	return UnknownBan
}

// confidenceClause renders the confidence grade as prose, or nothing when the
// table omits it. A missing grade is not silently promoted to "high".
func confidenceClause(c string) string {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "low":
		return "This state's closed season is set close to the season and has varied between years, so treat this window as a weak indicator."
	case "moderate":
		return "This state's closed season has shifted in some years, so treat this window as indicative rather than fixed."
	case "high":
		return ""
	default:
		return "The confidence grade for this window is not recorded, so treat it as a weak indicator."
	}
}

// prettyMD renders a "MM-DD" bound for display.
func prettyMD(s string) string {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return s
	}
	d, err := strconv.Atoi(parts[1])
	if err != nil {
		return s
	}
	mon, err := strconv.Atoi(parts[0])
	if err != nil {
		return s
	}
	return fmt.Sprintf("%d %s", d, time.Month(mon).String()[:3])
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// ---- verdict integration ---------------------------------------------------

// AssessWithBan is the verdict authority with the seasonal calendar included.
//
// Assess is retained as the three-argument form and delegates here with no
// calendar, so existing callers and tests keep the behaviour they were written
// against. A ban inside a closed season is SevCrit and therefore a no-go: it is
// a legal prohibition, and a no-go is the only verdict that stops a trip. This
// is the one place the calendar is allowed to escalate, and it escalates on
// being inside a window rather than on merely approaching one.
func AssessWithBan(m domain.Marine, w domain.Weather, pfz domain.PFZ, ban BanStatus) domain.Verdict {
	v := Assess(m, w, pfz)
	if !ban.Known {
		return v
	}
	v.Hazards = append(v.Hazards, domain.Hazard{
		Kind:     "season",
		Severity: ban.Severity,
		Value:    ban.Level,
		Limit:    "closed season by gazette notification",
		Note:     ban.Detail,
	})
	if ban.Severity == SevCrit {
		v.Rationale = append(v.Rationale, ban.Detail)
		v.Level, v.Severity = "no-go", SevCrit
		return v
	}
	if ban.Severity == SevCaution {
		v.Rationale = append(v.Rationale, ban.Detail)
		if v.Level == "go" {
			v.Level, v.Severity = "caution", SevCaution
		}
	}
	return v
}
