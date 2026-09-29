package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestListsMarshalAsArraysNotNull guards the wire format for the one response
// that breaks a client written against every other one.
//
// A question with no location in it returns a clarification: no hazards, no
// rationale, no degraded inputs, no provenance. A nil Go slice marshals to JSON
// null, so those fields arrived as null and the browser — which iterates them
// without checking — threw on exactly the answer a confused user most needs to
// read. The bug sat dormant for as long as the clarification never reached the
// interface at all.
func TestListsMarshalAsArraysNotNull(t *testing.T) {
	// The zero Findings is the shape of a clarification: everything unset.
	b, err := json.Marshal(Findings{})
	if err != nil {
		t.Fatalf("marshalling a zero findings: %v", err)
	}
	if strings.Contains(string(b), "null") {
		t.Fatalf("a clarification marshals a null: %s", b)
	}

	var got struct {
		Trace    []AgentRun `json:"trace"`
		Degraded []string   `json:"degraded"`
		Prov     []Citation `json:"prov"`
		Verdict  struct {
			Hazards   []Hazard `json:"hazards"`
			Rationale []string `json:"rationale"`
		} `json:"verdict"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshalling: %v", err)
	}
	if got.Trace == nil || got.Degraded == nil || got.Prov == nil {
		t.Errorf("top-level lists must be []: %s", b)
	}
	if got.Verdict.Hazards == nil || got.Verdict.Rationale == nil {
		t.Errorf("verdict lists must be []: %s", b)
	}

	// The per-agent source list is rendered next to every step of the trace, so
	// it has to be an array too. It is the one nested list that is null on a
	// refusal, because the planner reports no sources when it cannot plan.
	withTrace, err := json.Marshal(Findings{Trace: []AgentRun{{Name: "planner"}}})
	if err != nil {
		t.Fatalf("marshalling a trace: %v", err)
	}
	if strings.Contains(string(withTrace), `"sources":null`) {
		t.Errorf("an agent with no sources marshals a null: %s", withTrace)
	}

	// A populated response must be unchanged by the normalisation.
	full := Findings{
		Trace:      []AgentRun{{Name: "planner"}},
		Degraded:   []string{"marine"},
		Prov:       []Citation{{Source: "Open-Meteo"}},
		Verdict:    Verdict{Level: "go", Hazards: []Hazard{{Kind: "wave"}}, Rationale: []string{"clear"}},
		Advisory:   Advisory{Citations: Citations{"incois_pfz": {Source: "INCOIS"}}},
		Marine:     Marine{Citations: Citations{"sst": {Source: "Open-Meteo"}}},
		Answer:     "safe",
		AnswerLang: "en",
	}
	b2, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshalling a full findings: %v", err)
	}
	var back Findings
	if err := json.Unmarshal(b2, &back); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if len(back.Trace) != 1 || len(back.Degraded) != 1 || len(back.Prov) != 1 ||
		len(back.Verdict.Hazards) != 1 || len(back.Verdict.Rationale) != 1 {
		t.Errorf("normalisation dropped data: %s", b2)
	}
	// The nested citation maps are deliberately not on the wire: the response
	// carries one flat prov list instead, so a client renders provenance without
	// walking a tree. What must survive is that flat list.
	if len(back.Prov) != 1 || back.Prov[0].Source != "Open-Meteo" {
		t.Errorf("the flat provenance list was lost: %s", b2)
	}
}

// TestTheZoneCellIsOnTheWire checks the numbers the "Where to fish" section
// quotes are exposed. The zone is found at a fan cell, not at the point the
// top-level sea state is read at, so the two differ legitimately. The answer
// quotes the cell; a client that cannot see the cell cannot check any of it.
func TestTheZoneCellIsOnTheWire(t *testing.T) {
	f := Findings{
		Geo: Geo{Name: "Puri", DistanceKm: 85},
		PFZ: PFZ{
			DistanceKm: 85, SampleSSTC: 28.1, SampleWaveHeightM: 0.52, SampleSwellPerS: 9.4,
		},
	}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	var got struct {
		PFZ struct {
			SampleSSTC        float64 `json:"sample_sst_c"`
			SampleWaveHeightM float64 `json:"sample_wave_height_m"`
			SampleSwellPerS   float64 `json:"sample_swell_period_s"`
		} `json:"pfz"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshalling: %v", err)
	}
	if got.PFZ.SampleSSTC != 28.1 || got.PFZ.SampleWaveHeightM != 0.52 || got.PFZ.SampleSwellPerS != 9.4 {
		t.Errorf("the zone cell is not on the wire: %s", b)
	}
}
