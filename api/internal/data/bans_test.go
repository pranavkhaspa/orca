package data

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"orca/internal/domain"
)

func TestSeasonalBansEmbeddedTableValidates(t *testing.T) {
	win, meta, err := SeasonalBans(SeasonalBansPath, time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("the embedded ban calendar must load and validate: %v", err)
	}
	if len(win) == 0 {
		t.Fatal("the embedded ban calendar is empty")
	}
	if meta.Source == "" {
		t.Error("the calendar must record where its framework comes from")
	}
	if meta.CompiledOn == "" {
		t.Error("the calendar must record when it was compiled")
	}
	// The table is compiled from recurring practice, not read out of gazettes.
	// If this ever flips true, the notifications must actually be present.
	if meta.VerifiedAgainstGazettes {
		t.Error("the calendar claims gazette verification, but no gazette is in the repository; " +
			"that claim must not be made until the notifications behind it exist")
	}
	// Every row must name the state department that issues the binding
	// notification, or the calendar is not auditable.
	for _, w := range win {
		if !strings.Contains(strings.ToLower(w.Authority), "fisher") {
			t.Errorf("%s: authority %q does not name a fisheries department", w.State, w.Authority)
		}
	}
}

// The citation renders _source_url as an href, so the two must be separate
// fields. A regression here ships a paragraph as a link: the user taps a
// citation and the browser tries to resolve a sentence.
func TestSeasonalBansSourceURLIsALinkAndSourceIsNot(t *testing.T) {
	_, meta, err := SeasonalBans(SeasonalBansPath, time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	u, err := url.Parse(meta.SourceURL)
	if err != nil {
		t.Fatalf("_source_url %q does not parse: %v", meta.SourceURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		t.Errorf("_source_url %q must be an http(s) URL, got scheme %q", meta.SourceURL, u.Scheme)
	}
	if u.Host == "" {
		t.Errorf("_source_url %q has no host", meta.SourceURL)
	}
	// The prose field is allowed to mention the URL inline, which is exactly
	// why it must never be the one placed in href.
	if strings.TrimSpace(meta.Source) == "" {
		t.Error("_source must keep the prose describing the framework")
	}
}

func TestSeasonalBansRejectsNonURLSourceURL(t *testing.T) {
	// Guard the loader itself, not just the shipped table: a future edit that
	// points _source_url back at the prose has to be rejected at load time.
	for _, bad := range []string{
		"Department of Fisheries, Government of India (https://www.dof.gov.in/) for the national framework",
		"javascript:alert(1)",
		"/relative/path",
		"www.dof.gov.in",
	} {
		if err := validateSourceURL(bad); err == nil {
			t.Errorf("_source_url %q must be rejected, it is not an openable http(s) URL", bad)
		}
	}
	if err := validateSourceURL("https://www.dof.gov.in/"); err != nil {
		t.Errorf("a real https URL must be accepted: %v", err)
	}
	if err := validateSourceURL(""); err != nil {
		t.Errorf("an absent link must be allowed: %v", err)
	}
}

// The calendar must cover every state the coastal reference table can resolve,
// or a query for that place silently gets no ban conclusion. This is the test
// that keeps the two tables from drifting apart.
func TestSeasonalBansCoversEveryCoastalState(t *testing.T) {
	towns, err := Towns(CoastalPath)
	if err != nil {
		t.Fatalf("load coastal table: %v", err)
	}
	win, _, err := SeasonalBans(SeasonalBansPath, time.Now())
	if err != nil {
		t.Fatalf("load ban calendar: %v", err)
	}
	covered := make(map[string]bool, len(win))
	for _, w := range win {
		covered[strings.ToLower(strings.TrimSpace(w.State))] = true
	}
	for _, town := range towns {
		st := strings.ToLower(strings.TrimSpace(town.State))
		if !covered[st] {
			t.Errorf("%s resolves to state %q, which has no ban calendar; that query would draw no conclusion",
				town.Name, town.State)
		}
	}
}

// A defective table must be rejected at load, not tolerated at request time.
func TestValidateBansRejectsIncompleteRows(t *testing.T) {
	base := func() domain.BanWindow {
		return domain.BanWindow{
			State: "Kerala", Authority: "Dept of Fisheries, Government of Kerala",
			StartMD: "06-10", EndMD: "07-15", AppliesTo: "mechanised boats",
			Basis: "recurring pattern", Confidence: "high", Caveat: "confirm the gazette",
		}
	}
	cases := map[string]func(w *domain.BanWindow){
		"no state":       func(w *domain.BanWindow) { w.State = "" },
		"no authority":   func(w *domain.BanWindow) { w.Authority = "" },
		"no basis":       func(w *domain.BanWindow) { w.Basis = "" },
		"no caveat":      func(w *domain.BanWindow) { w.Caveat = "" },
		"no craft scope": func(w *domain.BanWindow) { w.AppliesTo = "" },
		"bad confidence": func(w *domain.BanWindow) { w.Confidence = "certain" },
		"no start":       func(w *domain.BanWindow) { w.StartMD = "" },
		"unparseable":    func(w *domain.BanWindow) { w.EndMD = "soon" },
		"feb 29":         func(w *domain.BanWindow) { w.StartMD = "02-29" },
		"one day":        func(w *domain.BanWindow) { w.EndMD = w.StartMD },
	}
	for name, mutate := range cases {
		w := base()
		mutate(&w)
		if err := validateBans([]domain.BanWindow{w}, 2026); err == nil {
			t.Errorf("%s: a row with %s should have been rejected", name, name)
		}
	}
	// The unmodified row must pass, or the test above proves nothing.
	if err := validateBans([]domain.BanWindow{base()}, 2026); err != nil {
		t.Errorf("a complete row was rejected: %v", err)
	}
}

func TestValidateBansRejectsDuplicateState(t *testing.T) {
	w := domain.BanWindow{
		State: "Kerala", Authority: "Dept of Fisheries, Government of Kerala",
		StartMD: "06-10", EndMD: "07-15", AppliesTo: "mechanised boats",
		Basis: "recurring pattern", Confidence: "high", Caveat: "confirm the gazette",
	}
	dup := w
	dup.StartMD, dup.EndMD = "07-01", "08-01"
	if err := validateBans([]domain.BanWindow{w, dup}, 2026); err == nil {
		t.Error("two rows for the same state would make the applicable one ambiguous")
	}
}

func TestBanMetaProvenanceDisclosesItIsNotGazetted(t *testing.T) {
	got := BanMeta{CompiledOn: "2026-09-30"}.Provenance()
	for _, want := range []string{"2026-09-30", "not checked", "gazetted separately"} {
		if !strings.Contains(got, want) {
			t.Errorf("provenance is missing %q: %q", want, got)
		}
	}
	// The verified form may only be produced by a table that really was checked.
	v := BanMeta{CompiledOn: "2026-09-30", VerifiedAgainstGazettes: true}.Provenance()
	if strings.Contains(v, "not checked") {
		t.Errorf("a verified table should not carry the unverified wording: %q", v)
	}
	// An unrecorded compile date must not be dressed up as one.
	if strings.Contains((BanMeta{}).Provenance(), "compiled  ") {
		t.Errorf("a missing compile date must not read as a real one: %q", (BanMeta{}).Provenance())
	}
}
