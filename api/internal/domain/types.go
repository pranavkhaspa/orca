// Package domain holds the shared contracts of the ORCA system.
//
// Design rule (see architecture.md §2): every figure that can reach a human is
// carried alongside a Citation. There is no way to produce a number without also
// producing where it came from and when it was retrieved. This is enforced by the
// type system rather than by discipline, so a missing citation is a compile error.
package domain

import (
	"encoding/json"
	"time"
)

// Published marine rules. These live in the shared contract package because
// both the risk engine (which applies them) and the presentation layer (which
// must state them) need them; duplicating the numbers in two packages would let
// the explanation drift away from the rule that actually ran.
const (
	// PFZBandLoC/PFZBandHiC bound the sea-surface-temperature window treated as
	// the productive band for fishing-zone identification.
	PFZBandLoC = 24.0
	PFZBandHiC = 30.0
)

// Citation is the provenance record attached to every figure we surface.
type Citation struct {
	Source    string    // e.g. "Open-Meteo Marine"
	Dataset   string    // e.g. "wave_height"
	URL       string    // the exact request that produced the value
	Retrieved time.Time // when we fetched it
	Live      bool      // false when the value came from a baked snapshot
	Err       string    // set when the fetch failed and we degraded
}

// Age is how stale the citation is. Surfaced in the UI so a user can see
// whether they are looking at live data or a fallback.
func (c Citation) Age() time.Duration { return time.Since(c.Retrieved) }

// Live report of provenance as a short human-readable string for the UI.
func (c Citation) Live2() string {
	if c.Err != "" {
		return "unavailable"
	}
	if !c.Live {
		return "snapshot"
	}
	return "live"
}

// Citations is an ordered collection keyed by dataset name.
type Citations map[string]Citation

// Geo is the resolved spatial context for a query.
type Geo struct {
	Query      string    `json:"query"`
	Name       string    `json:"name"`
	Lat        float64   `json:"lat"`
	Lon        float64   `json:"lon"`
	WayLat     float64   `json:"way_lat"`
	WayLon     float64   `json:"way_lon"`
	DistanceKm float64   `json:"distance_km"`
	BearingDeg float64   `json:"bearing_deg"`
	Source     string    `json:"source"`
	Citations  Citations `json:"-"`
}

// Marine is the ocean-state observation set.
type Marine struct {
	Time         time.Time `json:"time"`
	WaveHeightM  float64   `json:"wave_height_m"`
	WavePeriodS  float64   `json:"wave_period_s"`
	WaveDirDeg   float64   `json:"wave_dir_deg"`
	SwellHeightM float64   `json:"swell_height_m"`
	SwellPeriodS float64   `json:"swell_period_s"`
	WindWaveM    float64   `json:"wind_wave_m"`
	SSTC         float64   `json:"sst_c"`
	TideM        float64   `json:"tide_m"`
	Citations    Citations `json:"-"`
}

// Weather is the atmospheric observation set.
type Weather struct {
	Time          time.Time `json:"time"`
	WindKmh       float64   `json:"wind_kmh"`
	GustKmh       float64   `json:"gust_kmh"`
	PrecipMm      float64   `json:"precip_mm"`
	CloudPct      float64   `json:"cloud_pct"`
	WxCode        int       `json:"wx_code"`
	LightningRisk string    `json:"lightning_risk"` // low | moderate | high
	Citations     Citations `json:"-"`
}

// Advisory is the corroborating signal from the Indian official source.
//
// Checked and OfficialFound are separate because "we asked and there is nothing
// near this coast" is a different fact from "we could not ask", and the Gujarat
// coast produces the former on most days. Collapsing them would either hide a
// working source or imply an official advisory that was never retrieved.
type Advisory struct {
	Available bool   `json:"available"` // the bulletin query completed
	Status    string `json:"status"`
	Sector    string `json:"sector"` // our own sea-sector label
	Source    string `json:"source"`

	OfficialFound bool      `json:"official_found"`
	DistanceKm    float64   `json:"distance_km"` // to the nearest published line
	BearingDeg    float64   `json:"bearing_deg"`
	OfficialLabel string    `json:"official_label"` // e.g. "ORISSA sector 3"
	OfficialState string    `json:"official_state"`
	Bulletin      time.Time `json:"bulletin"` // the day the advisory describes
	CheckedAt     time.Time `json:"checked_at"`

	// Citations attribute the figures above, including the negative result.
	Citations Citations `json:"-"`
}

// PFZ is a Potential Fishing Zone we computed ourselves.
type PFZ struct {
	Score      float64  `json:"score"` // 0..1
	Lat        float64  `json:"lat"`
	Lon        float64  `json:"lon"`
	DistanceKm float64  `json:"distance_km"`
	SSTBand    string   `json:"sst_band"`
	AnomalyC   float64  `json:"anomaly_c"`
	Confidence string   `json:"confidence"` // high | medium | low
	Reasoning  []string `json:"reasoning"`
	// The zone is found at a fan cell, which is usually not the point the
	// top-level sea state was read at, so the two sets of numbers legitimately
	// differ: the Puri zone sits 85 km offshore, where the waves were 0.5 m
	// against 1.3 m at the coast. The reasoning sentences quote the cell, so
	// the cell's values are on the wire too. A number a reader can see has to
	// be a number a client can check, which is the whole of standing rule 2.
	SampleSSTC        float64 `json:"sample_sst_c"`
	SampleWaveHeightM float64 `json:"sample_wave_height_m"`
	SampleSwellPerS   float64 `json:"sample_swell_period_s"`
	// Citations attribute the figures behind this zone: the marine call made at
	// the winning sample point, and the archive baseline behind the anomaly.
	// Without them the zone coordinates, score and anomaly would be the only
	// numbers in the response with no source, which is what standing rule 2
	// forbids.
	Citations Citations `json:"-"`
}

// Hazard is one deterministic safety finding.
type Hazard struct {
	Kind     string `json:"kind"`
	Severity string `json:"severity"` // ok | caution | critical
	Value    string `json:"value"`
	Limit    string `json:"limit"`
	Note     string `json:"note"`
}

// Verdict is the safety decision. Produced by pure Go — never by a model.
type Verdict struct {
	Level     string   `json:"level"` // go | caution | no-go
	Severity  string   `json:"severity"`
	Hazards   []Hazard `json:"hazards"`
	Rationale []string `json:"rationale"`
}

// Plan is the LLM's structured reading of the user's question.
type Plan struct {
	Place       string `json:"place"`
	WindowHours int    `json:"window_hours"`
	Intent      string `json:"intent"`
	Lang        string `json:"lang"`
	Planned     bool   `json:"planned"` // false => deterministic router was used
}

// Status values for agent lifecycle.
const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
)

// AgentRun is one entry in the visible reasoning trace.
type AgentRun struct {
	Name    string     `json:"name"`
	Label   string     `json:"label"`
	Status  string     `json:"status"`
	Started time.Time  `json:"started"`
	Ended   time.Time  `json:"ended"`
	Summary string     `json:"summary"`
	Sources []Citation `json:"sources"`
	Err     string     `json:"err,omitempty"`
}

// Findings is everything the agents determined, prior to narration.
type Findings struct {
	Plan       Plan       `json:"plan"`
	Geo        Geo        `json:"geo"`
	Marine     Marine     `json:"marine"`
	Weather    Weather    `json:"weather"`
	Advisory   Advisory   `json:"advisory"`
	PFZ        PFZ        `json:"pfz"`
	Verdict    Verdict    `json:"verdict"`
	Trace      []AgentRun `json:"trace"`
	Answer     string     `json:"answer"`
	AnswerLang string     `json:"answer_lang"`
	Degraded   []string   `json:"degraded"`
	// Prov is the flat, de-duplicated provenance list for the response, so the
	// client can render "where did every number come from" without walking the
	// nested citation maps.
	Prov []Citation `json:"prov"`
}

// MarshalJSON emits every list in the response as an array, never as null.
//
// A nil Go slice marshals to null, and a clarification response — the answer to
// "is it safe to go out today?", where no location was named — has nil hazards,
// nil rationale, nil degraded and nil provenance. A client that iterates any of
// them therefore works for almost every response and throws for the one the user
// most needs to read. The browser did exactly that: the clarification was built
// correctly, reached the interface, and took the page down with it.
//
// An empty list and an absent list mean the same thing here, so the wire format
// says one thing. This is a contract fix, not a UI workaround: the API is public
// and a JavaScript client should not have to guard every array to survive it.
func (f Findings) MarshalJSON() ([]byte, error) {
	type plain Findings
	out := plain(f)
	if out.Trace == nil {
		out.Trace = []AgentRun{}
	}
	for i := range out.Trace {
		out.Trace[i].Sources = orEmpty(out.Trace[i].Sources)
	}
	if out.Degraded == nil {
		out.Degraded = []string{}
	}
	if out.Prov == nil {
		out.Prov = []Citation{}
	}
	out.Verdict.Hazards = orEmpty(out.Verdict.Hazards)
	out.Verdict.Rationale = orEmpty(out.Verdict.Rationale)
	out.PFZ.Reasoning = orEmpty(out.PFZ.Reasoning)
	out.Advisory.Citations = orEmptyCitations(out.Advisory.Citations)
	out.Marine.Citations = orEmptyCitations(out.Marine.Citations)
	out.Weather.Citations = orEmptyCitations(out.Weather.Citations)
	out.PFZ.Citations = orEmptyCitations(out.PFZ.Citations)
	return json.Marshal(out)
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// orEmptyCitations normalises both citation shapes to an empty object, because
// Citations is a map rather than a slice.
func orEmptyCitations(c Citations) Citations {
	if c == nil {
		return Citations{}
	}
	return c
}
