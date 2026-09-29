package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"orca/internal/domain"
)

func at(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 12, 0, 0, 0, IST)
}

// The regression this file exists for: a total upstream outage used to produce
// a confident all-clear. Every assertion below is a distinct way that could
// reopen it.
func TestAssessNeverReportsGoWhenObservationsAreAbsent(t *testing.T) {
	// Exactly what the orchestrator holds when both upstreams refused and no
	// snapshot was available.
	v := Assess(domain.Marine{}, domain.Weather{}, domain.PFZ{})
	if v.Level == "go" {
		t.Fatalf("absent observations must never produce an all-clear, got %q", v.Level)
	}
	if v.Level != "caution" || v.Severity != SevCaution {
		t.Errorf("got %s/%s, want caution/caution: absence is not danger, but it is not safety either",
			v.Level, v.Severity)
	}
	var found bool
	for _, h := range v.Hazards {
		if h.Kind == "observations" {
			found = true
			if !strings.Contains(h.Note, "not an all-clear") {
				t.Errorf("the gap must be stated in the hazard note: %q", h.Note)
			}
		}
	}
	if !found {
		t.Error("a missing observation set must be reported as its own hazard")
	}
	// A rationale line is what the answer actually reads, so the disclaimer has
	// to reach the prose and not only the structured field.
	var inProse bool
	for _, r := range v.Rationale {
		if strings.Contains(r, "not an all-clear") {
			inProse = true
		}
	}
	if !inProse {
		t.Errorf("the disclaimer must appear in the rationale: %v", v.Rationale)
	}
}

func TestAssessWithOneSetMissingIsNotGo(t *testing.T) {
	good := domain.PFZ{Score: 0.8, Confidence: "high"}
	m := mustMarine()
	w := mustWeather()

	if v := Assess(m, domain.Weather{}, good); v.Level == "go" {
		t.Error("a missing weather set must not produce an all-clear")
	}
	if v := Assess(domain.Marine{}, w, good); v.Level == "go" {
		t.Error("a missing marine set must not produce an all-clear")
	}
	// Both present is unchanged.
	if v := Assess(m, w, good); v.Level != "go" {
		t.Errorf("a complete observation set should still be able to return go, got %q", v.Level)
	}
}

// A forecast of 0 km/h gusts is legal and is a real all-clear. The gate must key
// on the observation timestamp, not on a value that can legitimately be zero,
// or it will cry wolf on calm days and destroy trust in the cautions.
func TestGenuineDeadCalmIsNotTreatedAsMissing(t *testing.T) {
	m, w := mustMarine(), mustWeather()
	w.GustKmh, w.WindKmh = 0, 0
	w.LightningRisk = "low"

	if !WeatherPresent(w) {
		t.Error("a timestamped forecast of 0 km/h gusts is present, not missing")
	}
	if v := Assess(m, w, domain.PFZ{Score: 0.8, Confidence: "high"}); v.Level != "go" {
		t.Errorf("a genuine dead calm should be able to return go, got %q (%v)", v.Level, v.Rationale)
	}
}

// An unreported convective risk is unknown, not "low". It previously printed an
// empty value beside a note asserting calm weather.
func TestUnreportedConvectiveRiskIsNotCalm(t *testing.T) {
	m, w := mustMarine(), mustWeather()
	w.LightningRisk = ""

	v := Assess(m, w, domain.PFZ{Score: 0.8, Confidence: "high"})
	if v.Level == "go" {
		t.Error("an unreported convective risk must not yield an all-clear")
	}
	var h *domain.Hazard
	for i := range v.Hazards {
		if v.Hazards[i].Kind == "convection" {
			h = &v.Hazards[i]
		}
	}
	if h == nil {
		t.Fatal("no convection hazard was reported")
	}
	if h.Value == "" {
		t.Error("the convection hazard must never carry an empty value on the wire")
	}
	if h.Value != "not reported" {
		t.Errorf("convection value = %q, want %q", h.Value, "not reported")
	}
	if h.Severity != SevCaution {
		t.Errorf("an unreported risk has severity %s, want caution", h.Severity)
	}
	if strings.Contains(strings.ToLower(h.Note), "no significant") {
		t.Errorf("an unreported risk must not be described as absent activity: %q", h.Note)
	}
}

// Every hazard on the wire needs a populated value, limit and note. A blank in
// any of them is a row a user cannot act on, and a blank note is a claim the
// engine did not make.
func TestNoHazardHasAnEmptyField(t *testing.T) {
	sets := map[string]struct {
		m domain.Marine
		w domain.Weather
	}{
		"all absent":     {domain.Marine{}, domain.Weather{}},
		"marine only":    {domain.Marine{WaveHeightM: 0.8, SSTC: 28, Time: at(2026, 6, 15)}, domain.Weather{}},
		"weather only":   {domain.Marine{}, domain.Weather{GustKmh: 12, LightningRisk: "low", Time: at(2026, 6, 15)}},
		"unreported wx":  {domain.Marine{WaveHeightM: 0.8, SSTC: 28, Time: at(2026, 6, 15)}, domain.Weather{GustKmh: 12, Time: at(2026, 6, 15)}},
		"complete calm":  {mustMarine(), mustWeather()},
		"complete rough": {domain.Marine{WaveHeightM: 5, SSTC: 28, Time: at(2026, 6, 15)}, domain.Weather{GustKmh: 70, LightningRisk: "high", Time: at(2026, 6, 15)}},
	}
	for name, s := range sets {
		v := Assess(s.m, s.w, domain.PFZ{Score: 0.7, Confidence: "high"})
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var got struct {
			Level   string `json:"level"`
			Hazards []struct {
				Kind, Severity, Value, Limit, Note string
			} `json:"hazards"`
		}
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if got.Level == "" {
			t.Errorf("%s: the verdict has no level on the wire", name)
		}
		for _, h := range got.Hazards {
			for _, f := range []struct{ k, v string }{
				{"kind", h.Kind}, {"severity", h.Severity},
				{"value", h.Value}, {"limit", h.Limit}, {"note", h.Note},
			} {
				if strings.TrimSpace(f.v) == "" {
					t.Errorf("%s: hazard %q has an empty %s: %s", name, h.Kind, f.k, b)
				}
			}
		}
	}
}

func mustMarine() domain.Marine {
	m, _ := calmConditions()
	m.Time = at(2026, time.June, 15)
	return m
}

func mustWeather() domain.Weather {
	_, w := calmConditions()
	w.Time = at(2026, time.June, 15)
	return w
}

// The gate and the verdict must be a pure function of their inputs: a retry
// after a transient outage failure must be able to reproduce the answer exactly.
func TestObservationGateIsDeterministic(t *testing.T) {
	first := Assess(domain.Marine{}, domain.Weather{}, domain.PFZ{})
	for i := 0; i < 100; i++ {
		got := Assess(domain.Marine{}, domain.Weather{}, domain.PFZ{})
		if got.Level != first.Level || len(got.Hazards) != len(first.Hazards) {
			t.Fatalf("run %d differed from the first", i)
		}
	}
}

// The gate stopped the verdict, but the rows underneath it kept saying the sea
// was fine. This asserts on the rows, because a reader skimming the hazard list
// never reaches the disclosure at the top of it.
func TestNoRowClaimsSafetyFromAbsentData(t *testing.T) {
	benign := []string{"within safe limits", "no significant convective", "moderate"}
	v := Assess(domain.Marine{}, domain.Weather{}, domain.PFZ{})
	if v.Level != "caution" {
		t.Fatalf("absent data must not reach go, got %q", v.Level)
	}
	if len(v.Hazards) == 0 {
		t.Fatal("expected hazards")
	}
	for _, h := range v.Hazards {
		if h.Kind == "observations" {
			continue
		}
		for _, b := range benign {
			if strings.Contains(strings.ToLower(h.Note), b) {
				t.Errorf("%s: absent data produced a reassuring row: %q", h.Kind, h.Note)
			}
		}
		if h.Severity == SevOK {
			t.Errorf("%s: absent data must not be reported as ok, got %q", h.Kind, h.Severity)
		}
		if strings.TrimSpace(h.Value) == "" || h.Value == "0.00 m" || h.Value == "0 km/h gusts" {
			t.Errorf("%s: absent data must not be shown as a zero reading, got %q", h.Kind, h.Value)
		}
	}
}

// And the fix must not disarm the real all-clear: a retrieved, calm, timestamped
// set is still allowed to say everything is fine.
func TestRetrievedCalmSetStillReportsOK(t *testing.T) {
	m := domain.Marine{WaveHeightM: 0.4, SSTC: 28, TideM: 0.5, Time: time.Now()}
	w := domain.Weather{GustKmh: 8, WindKmh: 12, LightningRisk: "none", Time: time.Now()}
	v := Assess(m, w, domain.PFZ{Score: 0.1, DistanceKm: 90, Confidence: "high"})
	if v.Level != "go" {
		t.Fatalf("a retrieved calm set should still be go, got %q (%s)", v.Level, v.Severity)
	}
	found := false
	for _, h := range v.Hazards {
		if h.Kind == "waves" {
			found = true
			if h.Severity != SevOK || !strings.Contains(h.Note, "within safe limits") {
				t.Errorf("a measured calm sea should be reported ok, got %q / %q", h.Severity, h.Note)
			}
		}
	}
	if !found {
		t.Error("no waves row in a verdict that had wave data")
	}
}
