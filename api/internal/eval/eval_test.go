package eval

import (
	"context"
	"strings"
	"testing"
	"time"

	"orca/internal/agents"
	"orca/internal/config"
	"orca/internal/data"
	"orca/internal/domain"
	"orca/internal/engine"
	"orca/internal/lang"
)

func newOrchestrator(t *testing.T) *agents.Orchestrator {
	t.Helper()
	return agents.New(config.Config{}.WithDefaults(), nil, data.NewCache(time.Minute))
}

func TestGroundednessRejectsAFabricatedNumber(t *testing.T) {
	// The check exists to make the project's central claim falsifiable, so it
	// has to fail on exactly the failure it exists to catch: a figure the
	// narrator invented.
	f := domain.Findings{}
	f.Marine.WaveHeightM = 1.34
	f.Marine.SSTC = 28.6

	if bad := groundFailures("Waves reach 1.3 m with sea surface temperature 28.6°C.", f); len(bad) != 0 {
		t.Errorf("a faithful retelling was flagged: %v", bad)
	}
	if bad := groundFailures("Waves reach 3.8 m today.", f); len(bad) != 1 || bad[0] != "3.8" {
		t.Errorf("a fabricated wave height was not caught, got %v", bad)
	}
	// A threshold the engine actually published is legitimate to restate.
	if bad := groundFailures("Waves above 2.5 m are treated as hazardous.", f); len(bad) != 0 {
		t.Errorf("a published threshold was flagged: %v", bad)
	}
	// Rounding to the last displayed digit is not a new claim.
	if bad := groundFailures("Waves are about 1.3 m.", f); len(bad) != 0 {
		t.Errorf("a one-decimal restatement was flagged: %v", bad)
	}
	// 4.0 m is the critical wave threshold, so the answer may state it exactly.
	if bad := groundFailures("Waves above 4 m are critical.", f); len(bad) != 0 {
		t.Errorf("a published threshold echoed as 4 should be allowed, got %v", bad)
	}
	// A gust of 40.8 km/h printed as 41 is rounding, not a new measurement, and
	// whole-number wind speeds are how they are conventionally reported.
	f3 := f
	f3.Weather.GustKmh = 40.8
	if bad := groundFailures("Forecast gusts of 41 km/h are elevated.", f3); len(bad) != 0 {
		t.Errorf("a faithful integer rounding was flagged: %v", bad)
	}
	// 3.8 is not the correct rounding of the 4.0 m critical threshold, so a
	// narrator that invents it has moved the number that decides the verdict.
	f2 := f
	f2.Marine.WaveHeightM = engine.WaveCriticalM
	if bad := groundFailures("Waves reach 3.8 m.", f2); len(bad) != 1 || bad[0] != "3.8" {
		t.Errorf("3.8 passed as the 4.0 m critical threshold; got %v", bad)
	}
}

func TestCorpusCarriesTheAdversarialCases(t *testing.T) {
	// The two injection cases name a real place on purpose. An earlier version
	// expected them to be refused, which would have been satisfied by a system
	// that simply stopped answering anyone who pushed back.
	var inj int
	for _, c := range Corpus(nil) {
		for _, bad := range c.MustNotContain {
			inj++
			if c.WantClarification {
				t.Errorf("%s: an injection case must still be answered, not refused", c.ID)
			}
			if c.WantPlace == "" {
				t.Errorf("%s: an injection case must still resolve its place", c.ID)
			}
			if !strings.Contains(c.Query, strings.Split(bad, " ")[0]) {
				t.Errorf("%s: forbidden phrase %q is not actually in the query, so it tests nothing", c.ID, bad)
			}
		}
	}
	if inj < 3 {
		t.Errorf("expected the corpus to carry several injection phrases, found %d", inj)
	}
}

func TestCorpusCoversEveryLanguage(t *testing.T) {
	places, err := data.SnapshotPlaces(config.DefaultSnapshotPath)
	if err != nil {
		t.Fatalf("reading the snapshot: %v", err)
	}
	want := map[string]bool{}
	for _, l := range lang.All {
		want[string(l)] = false
	}
	for _, c := range Corpus(places) {
		if _, ok := want[c.WantLang]; ok {
			want[c.WantLang] = true
		}
	}
	for l, seen := range want {
		if !seen {
			t.Errorf("no corpus case exercises %s", l)
		}
	}
}

func TestRunIsDeterministicAndFailsLoudly(t *testing.T) {
	o := newOrchestrator(t)
	ctx := context.Background()

	rep := Run(ctx, o, []Case{
		{ID: "kochi/en", Query: "Is it safe to fish off Kochi tomorrow?", WantPlace: "Kochi", WantLang: "en", Group: "kochi"},
		{ID: "kochi/hi", Query: "कल कोची में मछली पकड़ना सुरक्षित है?", WantPlace: "Kochi", WantLang: "hi", Group: "kochi"},
		{ID: "none", Query: "Is it safe to go out today?", WantPlace: "", WantLang: "en", WantClarification: true},
	}, Config{Live: false, Repeat: 3})

	if len(rep.Cases) != 3 {
		t.Fatalf("expected 3 cases, got %d", len(rep.Cases))
	}
	if !rep.Passed {
		t.Error("a correct run was reported as failed")
	}
	det, ok := rep.Metric("determinism")
	if !ok || det.Total != 3 || det.Pct != 100 {
		t.Errorf("determinism should cover all 3 cases at 100%%, got %+v", det)
	}
	// The two phrasings are one question and must not disagree.
	con, ok := rep.Metric("answer_consistency")
	if !ok || con.Pct != 100 {
		t.Errorf("translations of one question disagreed: %+v", con)
	}
	for _, c := range rep.Cases {
		if c.WantLang == "hi" && c.GotLang != "hi" {
			t.Errorf("Hindi question answered in %s", c.GotLang)
		}
	}
}

func TestRunReportsAFailureRatherThanPassingQuietly(t *testing.T) {
	// A suite that cannot fail is worse than no suite, so the harness is
	// pointed at a claim the engine does not make and must say so.
	o := newOrchestrator(t)
	rep := Run(context.Background(), o, []Case{
		{ID: "wrong-place", Query: "Is it safe to fish off Kochi tomorrow?", WantPlace: "Chennai", WantLang: "en"},
	}, Config{Live: false})

	if rep.Passed {
		t.Fatal("the harness passed a case whose expected place is wrong")
	}
	place, ok := rep.Metric("place_accuracy")
	if !ok || place.Pct != 0 {
		t.Errorf("place_accuracy should be 0, got %+v", place)
	}
}

// TestConsistencyGroupsAskTheSameQuestion catches the corpus drifting out of
// what a language group is for. A group exists to prove that the language does
// not change the verdict, which is only true if every member asked about the
// same place in the same window. Two members once differed only by one carrying
// "tomorrow morning" and the other not, so they were scored as a verdict
// disagreement when they were simply different questions.
func TestConsistencyGroupsAskTheSameQuestion(t *testing.T) {
	seen := map[string]map[string]int{}
	for _, c := range Corpus(nil) {
		if c.Group == "" {
			continue
		}
		if seen[c.Group] == nil {
			seen[c.Group] = map[string]int{}
		}
		seen[c.Group][c.Query]++
	}
	if len(seen) == 0 {
		t.Fatal("no consistency groups in the corpus")
	}
	for group, queries := range seen {
		if len(queries) < 2 {
			t.Errorf("group %q has a single member, so it tests nothing", group)
		}
	}
}

// TestGroupPlanMismatchIsReportedAsSuch pins the distinction the scorer has to
// make. A verdict disagreement is a defect in the product; a plan disagreement
// means the router read two different questions, so there was nothing to
// compare, and reporting the second as the first sends someone hunting a
// language bug that does not exist. It is driven directly because offline the
// deterministic router reads every query the same way.
func TestGroupPlanMismatchIsReportedAsSuch(t *testing.T) {
	results := []Result{
		{Case: Case{ID: "g/en", Group: "g"}, Verdict: "caution", PlanIntent: "safety", PlanWindowHours: 30},
		{Case: Case{ID: "g/ta", Group: "g"}, Verdict: "go", PlanIntent: "general", PlanWindowHours: 6},
	}
	scoreGroups(results,
		map[string]string{"g": "caution"}, map[string]bool{}, map[string]string{"g": "safety over 30h"})

	if results[1].GroupOK {
		t.Error("a case planned differently reported agreement")
	}
	if !strings.Contains(results[1].Err, "general over 6h") {
		t.Errorf("the mismatch does not name the plan this case made: %q", results[1].Err)
	}
	if !strings.Contains(results[1].Err, "safety over 30h") {
		t.Errorf("the mismatch does not name the plan the group made: %q", results[1].Err)
	}
	// The member that set the group's plan is not credited either: the two were
	// never comparable, so agreement between them is a coincidence and must not
	// be reported as a passing consistency case.
	if results[0].GroupOK {
		t.Error("an invalid group must not credit the case that happened to agree")
	}
	if !strings.Contains(results[0].Err, "safety over 30h") {
		t.Errorf("the first case is not told the group is invalid: %q", results[0].Err)
	}
}

// TestGroupVerdictConflictIsNotBlamedOnTheLastCase checks that a genuine
// product disagreement is reported as such, and attributed to the group rather
// than to whichever case happened to run last.
func TestGroupVerdictConflictIsNotBlamedOnTheLastCase(t *testing.T) {
	results := []Result{
		{Case: Case{ID: "g/en", Group: "g"}, Verdict: "caution", PlanIntent: "safety", PlanWindowHours: 30},
		{Case: Case{ID: "g/ta", Group: "g"}, Verdict: "caution", PlanIntent: "safety", PlanWindowHours: 30},
	}
	scoreGroups(results,
		map[string]string{"g": "go"}, map[string]bool{"g": true}, map[string]string{"g": "safety over 30h"})

	if results[1].GroupOK {
		t.Error("a group that conflicted must not report agreement")
	}
	if !strings.Contains(results[1].Err, "another case in this group") {
		t.Errorf("a verdict conflict must be named as a conflict, got %q", results[1].Err)
	}
	if strings.Contains(results[1].Err, "planned") && strings.Contains(results[1].Err, "another case") {
		t.Errorf("a verdict conflict was misattributed to the window: %q", results[1].Err)
	}
}

// TestGroupAgreementNeedsMatchingPlans is the positive control: a group that
// really is the same question in two languages still passes.
func TestGroupAgreementNeedsMatchingPlans(t *testing.T) {
	results := []Result{
		{Case: Case{ID: "g/en", Group: "g"}, Verdict: "caution", PlanIntent: "safety", PlanWindowHours: 30},
		{Case: Case{ID: "g/hi", Group: "g"}, Verdict: "caution", PlanIntent: "safety", PlanWindowHours: 30},
	}
	scoreGroups(results,
		map[string]string{"g": "caution"}, map[string]bool{}, map[string]string{"g": "safety over 30h"})

	for _, r := range results {
		if !r.GroupOK {
			t.Errorf("%s should have agreed: %q", r.ID, r.Err)
		}
	}
}

// TestAnUnmeasuredMetricIsNotReportedAsZeroPercent covers a reporting lie: a
// metric nobody ran used to print 0%, which reads as a total failure and, in a
// terminal, as something to fix. It failed nothing, but it looked like the
// worst number in the report.
func TestAnUnmeasuredMetricIsNotReportedAsZeroPercent(t *testing.T) {
	rep := Run(context.Background(), newOrchestrator(t),
		Corpus(nil), Config{Repeat: 1})

	var det Metric
	for _, m := range rep.Metrics {
		if m.Name == "determinism" {
			det = m
		}
	}
	if det.Total != 0 {
		t.Fatalf("determinism ran without a repeat: %+v", det)
	}
	if det.Note == "" {
		t.Error("an unmeasured metric must say so rather than print 0%")
	}
}
