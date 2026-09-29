package lang

import (
	"fmt"
	"strings"
	"testing"

	"orca/internal/domain"
)

// findings builds a representative result used to exercise every template.
func findings() domain.Findings {
	return domain.Findings{
		Geo: domain.Geo{Name: "Kochi"},
		PFZ: domain.PFZ{Score: 0.82, Lat: 10.08, Lon: 75.93, DistanceKm: 40, Confidence: "high", Reasoning: []string{"Water is 28.5°C, inside the productive band."}},
		Verdict: domain.Verdict{
			Level:     "caution",
			Severity:  "caution",
			Rationale: []string{"Forecast gusts of 26 km/h are elevated."},
		},
	}
}

var levels = []string{"go", "caution", "no-go"}

// TestRenderEveryLanguage renders a verdict in all ten languages. A panic here
// — a missing case, a bad format verb — would take down the request handler, so
// this test is as much about not crashing as about the text.
func TestRenderEveryLanguage(t *testing.T) {
	for _, c := range All {
		for _, lvl := range levels {
			f := findings()
			f.Verdict.Level = lvl
			got := Render(f, c)
			if strings.TrimSpace(got) == "" {
				t.Errorf("Render(%q, %q) is empty", c, lvl)
			}
			// The place is named in the language's own script where a
			// translation is known, and in English otherwise.
			if !strings.Contains(got, LocalisePlace("Kochi", c)) {
				t.Errorf("Render(%q, %q) does not name the location: %q", c, lvl, got)
			}
			// The zone coordinates must always be present and correct, because
			// they are what a user actually steers to.
			if !strings.Contains(got, "10.0800") {
				t.Errorf("Render(%q, %q) is missing the zone latitude: %q", c, lvl, got)
			}
		}
	}
}

// TestRenderVerdictIsLanguageSpecific confirms the verdict is actually
// translated rather than left in English.
func TestRenderVerdictIsLanguageSpecific(t *testing.T) {
	seen := map[Code]string{}
	for _, c := range All {
		f := findings()
		f.Verdict.Level = "no-go"
		got := Render(f, c)
		seen[c] = got
		if c != EN && strings.Contains(got, "NO-GO") {
			t.Errorf("Render(%q) left the verdict level in English", c)
		}
		// The reasoning must also be localised, not merely the headline.
		if c != EN && strings.Contains(got, "elevated") {
			t.Errorf("Render(%q) left the safety reasoning in English: %q", c, got)
		}
	}
	// Every language must produce a different string; identical output would mean
	// a language silently fell through to another.
	uniq := map[string]bool{}
	for _, v := range seen {
		uniq[v] = true
	}
	if len(uniq) != len(seen) {
		t.Errorf("only %d distinct renderings for %d languages", len(uniq), len(seen))
	}
}

func TestRenderFallsBackForUnknownLanguage(t *testing.T) {
	f := findings()
	f.Verdict.Level = "no-go"
	got := Render(f, Code("xx"))
	if !strings.Contains(got, "Kochi") {
		t.Error("an unknown language must fall back to English rather than failing")
	}
	if !strings.Contains(got, "Where to fish") {
		t.Errorf("English fallback lost its section headings: %q", got)
	}
	if !strings.Contains(got, "NO-GO") {
		t.Errorf("English fallback should render the verdict level, got %q", got)
	}
}

// TestRenderHandlesMissingReasoning checks the template does not print a bare
// separator when the engine produced nothing to explain.
func TestRenderHandlesMissingReasoning(t *testing.T) {
	for _, c := range All {
		f := findings()
		f.PFZ.Reasoning = nil
		f.Verdict.Rationale = nil
		got := Render(f, c)
		if strings.Contains(got, "[]") {
			t.Errorf("Render(%q) leaked a Go slice literal: %q", c, got)
		}
		if !strings.Contains(got, LocalisePlace("Kochi", c)) {
			t.Errorf("Render(%q) lost the location in the empty-reasoning case: %q", c, got)
		}
	}
}

// TestRationaleIsLocalisedNotEmpty checks the safety reasoning — the part a
// fisher actually relies on — is translated for every supported language. An
// English rationale inside a Tamil answer is worse than no answer, because it
// looks like the system understood the language.
func TestRationaleIsLocalisedNotEmpty(t *testing.T) {
	f := findings()
	f.Verdict.Hazards = []domain.Hazard{
		{Kind: "waves", Severity: "caution", Value: "3.10 m", Limit: "2.5 m caution"},
		{Kind: "wind", Severity: "critical", Value: "62 km/h gusts", Limit: "55 km/h critical"},
	}
	f.Verdict.Level, f.Verdict.Severity = "no-go", "critical"
	english := strings.Join(Rationale(f, EN), " ")

	for _, c := range All {
		if c == EN {
			continue
		}
		got := Rationale(f, c)
		if len(got) == 0 {
			t.Errorf("Rationale(%q) is empty for a no-go verdict", c)
			continue
		}
		joined := strings.Join(got, " ")
		if strings.TrimSpace(joined) == english {
			t.Errorf("Rationale(%q) is the untranslated English text", c)
		}
		// The figures must survive translation untouched. This is the whole
		// point of localising the frame rather than the sentence.
		for _, figure := range []string{"3.10 m", "62 km/h", "2.5 m", "55 km/h"} {
			if !strings.Contains(joined, figure) {
				t.Errorf("Rationale(%q) lost the figure %q: %q", c, figure, joined)
			}
		}
	}
}

// TestCalmVerdictIsFullyLocalised checks the reassuring case too, not just the
// dangerous one. "Go" is the answer most users will get, and it must not be an
// English sentence in a translated interface.
func TestCalmVerdictIsFullyLocalised(t *testing.T) {
	f := findings()
	f.Verdict.Level, f.Verdict.Severity = "go", "ok"
	f.Verdict.Hazards = []domain.Hazard{
		{Kind: "waves", Severity: "ok", Value: "0.80 m", Limit: "2.5 m caution"},
	}
	f.Verdict.Rationale = nil

	for _, c := range All {
		if c == EN {
			continue
		}
		got := Render(f, c)
		if strings.Contains(got, "No elevated hazard") {
			t.Errorf("Render(%q) used the English all-clear: %q", c, got)
		}
	}
}

func TestVerdictAndConfidenceWordsAreTranslated(t *testing.T) {
	seenLevel := map[string]bool{}
	seenConf := map[string]bool{}
	for _, c := range All {
		if c == EN {
			continue
		}
		lvl := VerdictWord(c, "no-go")
		if lvl == "" || lvl == "no-go" {
			t.Errorf("VerdictWord(%q, no-go) = %q, want a translation", c, lvl)
		}
		seenLevel[lvl] = true

		cf := ConfidenceWord(c, "low")
		if cf == "" || cf == "low" {
			t.Errorf("ConfidenceWord(%q, low) = %q, want a translation", c, cf)
		}
		seenConf[cf] = true
	}
	if len(seenLevel) < 8 {
		t.Errorf("only %d distinct translations of the verdict word", len(seenLevel))
	}
	if len(seenConf) < 8 {
		t.Errorf("only %d distinct translations of the confidence word", len(seenConf))
	}
}

// TestZoneIsJustifiedInEveryLanguage checks that a non-English reader is told
// *why* the zone was chosen, not merely where it is. The engine's prose
// reasoning is English-only, so the localized path assembles the same claim
// from the same numbers.
func TestZoneIsJustifiedInEveryLanguage(t *testing.T) {
	for _, c := range All {
		f := findings()
		f.Marine.SSTC = 28.4
		got := Render(f, c)
		// The band boundaries are the published rule and must appear as digits
		// in every language, because a number is never translated.
		if !strings.Contains(got, "24") || !strings.Contains(got, "30") {
			t.Errorf("Render(%q) omits the PFZ band: %q", c, got)
		}
		if !strings.Contains(got, "28.4") {
			t.Errorf("Render(%q) omits the observed SST: %q", c, got)
		}
	}
}

// TestSnapshotIsDisclosed pins the most safety-critical presentation rule: if
// every figure came from the baked snapshot, the answer itself must say so, in
// the reader's own language. A badge in the UI is not enough, because the answer
// is what gets read aloud and acted on.
func TestSnapshotIsDisclosed(t *testing.T) {
	for _, c := range All {
		snap := findings()
		snap.Prov = []domain.Citation{{Source: "Open-Meteo Marine", Dataset: "wave_height", Live: false}}
		if got := Render(snap, c); !strings.Contains(got, frames[c].snapshotNote) {
			t.Errorf("Render(%q) does not disclose snapshot data: %q", c, got)
		}

		live := findings()
		live.Prov = []domain.Citation{{Source: "Open-Meteo Marine", Dataset: "wave_height", Live: true}}
		if got := Render(live, c); strings.Contains(got, frames[c].snapshotNote) {
			t.Errorf("Render(%q) claims snapshot data while live: %q", c, got)
		}
	}
}

// TestSnapshotNoticeSkippedWithoutProvenance guards the other direction: with no
// provenance at all we must not assert staleness we cannot prove.
func TestSnapshotNoticeSkippedWithoutProvenance(t *testing.T) {
	if got := snapshotNotice(findings(), frames[EN]); got != "" {
		t.Errorf("snapshotNotice with no citations = %q, want empty", got)
	}
}

// TestZoneBandClaimFollowsTheMeasurement is a correctness test, not a coverage
// one. An unconditional "inside the band" sentence would tell a fisher that
// 30.4 C water is inside a 24-30 C band.
func TestZoneBandClaimFollowsTheMeasurement(t *testing.T) {
	for _, c := range All {
		for _, sst := range []float64{21.0, 24.0, 27.5, 30.0, 30.4, 33.0} {
			f := findings()
			f.Marine.SSTC = sst
			got := Render(f, c)
			inBand := sst >= domain.PFZBandLoC && sst <= domain.PFZBandHiC
			want := frames[c].zoneWhyOut
			if inBand {
				want = frames[c].zoneWhy
			}
			// The frame holds a format string; render it the same way Render does.
			want = fmt.Sprintf(want, sst, domain.PFZBandLoC, domain.PFZBandHiC)
			otherTpl := frames[c].zoneWhyOut
			if inBand {
				otherTpl = frames[c].zoneWhy
			}
			other := fmt.Sprintf(otherTpl, sst, domain.PFZBandLoC, domain.PFZBandHiC)
			if !strings.Contains(got, want) {
				t.Errorf("Render(%q) at %.1f C: want the %s claim", c, sst,
					map[bool]string{true: "in-band", false: "out-of-band"}[inBand])
			}
			// The wrong claim must not also appear.
			if other != want && strings.Contains(got, other) {
				t.Errorf("Render(%q) at %.1f C: also emitted the opposite claim", c, sst)
			}
		}
	}
}

// TestZoneSectionReportsTheZoneNotTheCoast covers the case the I15 follow-up
// exposed: the zone is a fan cell 85 km offshore, the top-level sea state is
// read at the coast, and the two genuinely differ. "Where to fish" describes
// the zone, so every figure in it has to come from the zone. Reading the
// coastal temperature there put a coastal SST next to the cell's wave height,
// so the paragraph described two different places.
func TestZoneSectionReportsTheZoneNotTheCoast(t *testing.T) {
	f := domain.Findings{
		Geo:    domain.Geo{Name: "Puri", DistanceKm: 85, Lat: 20.25, Lon: 86.49},
		Marine: domain.Marine{SSTC: 30.4, WaveHeightM: 1.34},
		PFZ: domain.PFZ{
			Lat: 20.25, Lon: 86.49, DistanceKm: 85, Confidence: "high",
			SampleSSTC: 28.1, SampleWaveHeightM: 0.52,
			Reasoning: []string{"Sea state is workable: 0.52 m waves."},
		},
		AnswerLang: "en",
	}
	got := Render(f, EN)

	// The zone is 28.1 C, so the band sentence must say inside 28.1, not the
	// coastal 30.4 which is out of band.
	if !strings.Contains(got, "28.1") {
		t.Errorf("the zone section does not report the zone's own temperature:\n%s", got)
	}
	if strings.Contains(got, "30.4") {
		t.Errorf("the zone section reports the coastal temperature:\n%s", got)
	}
	if !strings.Contains(got, "inside the 24–30°C band") {
		t.Errorf("28.1 C must be described as inside the band:\n%s", got)
	}
	if !strings.Contains(got, "0.52 m waves") {
		t.Errorf("the cell's sea state is missing:\n%s", got)
	}
}

// TestZoneSectionFallsBackToTheCoastalReading checks the fan found nothing, so
// the zone is the waypoint and the section must not lose its band sentence.
func TestZoneSectionFallsBackToTheCoastalReading(t *testing.T) {
	f := domain.Findings{
		Geo:        domain.Geo{Name: "Puri", DistanceKm: 45, Lat: 20.25, Lon: 86.49},
		Marine:     domain.Marine{SSTC: 27.9},
		PFZ:        domain.PFZ{Lat: 20.25, Lon: 86.49, DistanceKm: 45, Reasoning: []string{"A."}},
		AnswerLang: "en",
	}
	if got := Render(f, EN); !strings.Contains(got, "27.9") {
		t.Errorf("with no fan sample the section lost its temperature:\n%s", got)
	}
}
