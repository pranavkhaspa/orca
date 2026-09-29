package data

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"orca/internal/domain"
	"orca/internal/engine"
)

// Seasonal ban calendar loading.
//
// The table is embedded with the rest of the reference data so the service
// cannot lose it in a bad deploy, on the same reasoning as coastal_towns.json:
// a verdict engine that cannot read its own calendar must not silently fall
// back to "no ban".

// SeasonalBansPath is the canonical location of the embedded ban calendar. It
// shares the coastal table's embed prefix, so the calendar is served from the
// binary rather than from disk and cannot be lost in a bad deploy.
const SeasonalBansPath = "embed:seasonal_bans.json"

var (
	bansOnce sync.Once
	bansWin  []domain.BanWindow
	bansErr  error
	bansMeta BanMeta
)

// BanMeta is the provenance recorded in the calendar file itself.
//
// It is carried separately from the windows because it describes the table as a
// whole, and it is what lets a client state that the table is a record of
// recurring patterns rather than a set of current-year notifications.
type BanMeta struct {
	SchemaVersion int    `json:"schema_version"`
	Source        string `json:"_source"`
	// SourceURL is the link a client should render for this table. It is
	// separate from Source, which is a sentence describing where the framework
	// came from and is not itself a link.
	SourceURL string `json:"_source_url"`
	// CompiledOn is when this table was written.
	CompiledOn string `json:"_compiled_on"`
	// VerifiedAgainstGazettes records whether any row has been checked against a
	// real gazette notification.
	//
	// It is false, and a test keeps it false, because the rows were compiled from
	// recurring statutory practice rather than read out of individual gazettes.
	// Setting it true is not a matter of flipping a flag: it would mean the
	// notifications behind these dates are on hand, and they are not in this
	// repository. The flag exists so a client can tell the user which kind of
	// claim the dates are without having to infer it from prose.
	VerifiedAgainstGazettes bool `json:"_verified_against_gazettes"`
}

// Provenance describes the table in the form a user can act on.
//
// It deliberately reports that the dates are compiled patterns rather than
// gazetted facts, and it does so whether or not anyone has been reading the
// documentation, because this string is what reaches the user.
func (m BanMeta) Provenance() string {
	when := m.CompiledOn
	if when == "" {
		when = "an unrecorded date"
	}
	if m.VerifiedAgainstGazettes {
		return fmt.Sprintf("closed-season dates compiled %s and checked against gazette notifications", when)
	}
	return fmt.Sprintf("closed-season dates compiled %s from recurring statutory practice, "+
		"not checked against any year's gazette notification; the binding dates for a "+
		"given year are gazetted separately by the state department", when)
}

// SeasonalBans loads and caches the ban calendar, validating it on the way in.
//
// Validation happens at load rather than at evaluation so that a malformed row
// is a testable, startup-time failure instead of a per-request surprise. The
// engine still defends itself when it evaluates; this is the earlier gate.
func SeasonalBans(path string, now time.Time) ([]domain.BanWindow, BanMeta, error) {
	bansOnce.Do(func() {
		b, err := readData(path)
		if err != nil {
			bansErr = fmt.Errorf("read ban calendar: %w", err)
			return
		}
		// Unknown keys, including the file's _comment documentation block, are
		// ignored by the decoder, so one pass is enough.
		var doc struct {
			SchemaVersion int                `json:"schema_version"`
			Source        string             `json:"_source"`
			SourceURL     string             `json:"_source_url"`
			CompiledOn    string             `json:"_compiled_on"`
			Verified      bool               `json:"_verified_against_gazettes"`
			Windows       []domain.BanWindow `json:"windows"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			bansErr = fmt.Errorf("parse ban calendar: %w", err)
			return
		}
		// Source is prose and SourceURL is a link, and they are separate fields
		// precisely because the prose is not a link: the citation renders
		// SourceURL as an href, and a sentence with a URL buried mid-string makes
		// an href the browser cannot resolve. Reject a URL that is not one, rather
		// than shipping a citation whose link is a paragraph.
		if err := validateSourceURL(doc.SourceURL); err != nil {
			bansErr = err
			return
		}
		bansMeta = BanMeta{
			SchemaVersion: doc.SchemaVersion, Source: doc.Source, SourceURL: doc.SourceURL,
			CompiledOn: doc.CompiledOn, VerifiedAgainstGazettes: doc.Verified,
		}

		if len(doc.Windows) == 0 {
			bansErr = fmt.Errorf("ban calendar is empty")
			return
		}
		if doc.Verified {
			bansErr = fmt.Errorf("ban calendar claims verification against gazette notifications, " +
				"but the notifications are not in this repository; refusing to load a table that " +
				"overstates its own provenance")
			return
		}
		if err := validateBans(doc.Windows, now.Year()); err != nil {
			bansErr = err
			return
		}
		bansWin = doc.Windows
	})
	return bansWin, bansMeta, bansErr
}

// validateSourceURL rejects a _source_url that a browser could not open.
//
// An empty value is allowed: a table with no link is a smaller problem than a
// table whose link is a sentence. Anything present has to be an absolute http(s)
// URL with a host.
func validateSourceURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("ban calendar _source_url %q does not parse: %w", raw, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("ban calendar _source_url %q is not an absolute http(s) URL", raw)
	}
	return nil
}

// validateBans rejects a table that would produce a silently wrong verdict.
//
// A missing authority, caveat, basis, confidence grade or craft scope is an
// error rather than a warning, because every one of those is what stands between
// an inferred date and a user treating it as a gazetted one. A table that
// cannot carry those fields is not safe to ship.
func validateBans(win []domain.BanWindow, year int) error {
	seen := make(map[string]bool, len(win))
	for i, w := range win {
		switch {
		case w.State == "":
			return fmt.Errorf("ban calendar row %d: no state", i)
		case w.Authority == "":
			return fmt.Errorf("ban calendar row %d (%s): no authority named", i, w.State)
		case w.Basis == "":
			return fmt.Errorf("ban calendar row %d (%s): no basis recorded", i, w.State)
		case w.Caveat == "":
			return fmt.Errorf("ban calendar row %d (%s): no verification caveat", i, w.State)
		case w.AppliesTo == "":
			return fmt.Errorf("ban calendar row %d (%s): does not say which craft it covers", i, w.State)
		}
		switch strings.ToLower(strings.TrimSpace(w.Confidence)) {
		case "high", "moderate", "low":
		default:
			return fmt.Errorf("ban calendar row %d (%s): confidence %q is not high, moderate or low",
				i, w.State, w.Confidence)
		}
		if _, _, err := engine.ValidWindow(w, year); err != nil {
			return fmt.Errorf("ban calendar row %d (%s): %w", i, w.State, err)
		}
		key := strings.ToLower(strings.TrimSpace(w.State))
		if seen[key] {
			return fmt.Errorf("ban calendar row %d: duplicate state %q", i, w.State)
		}
		seen[key] = true
	}
	return nil
}
