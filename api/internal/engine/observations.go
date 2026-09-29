package engine

import (
	"fmt"
	"strings"

	"orca/internal/domain"
)

// Observation presence.
//
// This file closes a fail-open in the verdict. When every upstream refused and
// no snapshot was available, the orchestrator held a zero-valued Marine and
// Weather, and Assess read those zeroes as measurements: it reported a 0.00 m
// sea state as "within safe limits for small craft", a 0 km/h gust as "wind is
// within safe limits", an absent convective forecast as "no significant
// convective activity indicated", and returned a confident "go".
//
// The failure has exactly the same shape as the unresolved-location bug that
// Geocode's guard already exists to stop: every field is populated, the
// provenance looks real, the formatting is correct, and nothing in the response
// says the numbers are not measurements. A total upstream outage was
// indistinguishable from a calm day. For a service whose output is a decision
// about whether to take a boat out, that is the worst bug in the system.
//
// The rule is deliberately narrow, because a fix that fires on legitimate data
// is worse than no fix: a forecast of 0 km/h gusts is possible and would be a
// real all-clear, so a gust value alone cannot decide presence. Presence is
// decided on the observation timestamp together with the substantive fields, so
// a set counts as absent only when it is genuinely an unpopulated struct.
//
// Absence never escalates to critical. Nothing was measured, so nothing was
// found dangerous, and claiming a critical hazard would be as much of a
// fabrication as claiming an all-clear. Absence produces caution, and the one
// thing it must never produce is "go".

// MarinePresent reports whether the marine observation set was populated.
func MarinePresent(m domain.Marine) bool {
	return m.WaveHeightM > 0 || m.SSTC > 0 || m.TideM > 0 || !m.Time.IsZero()
}

// WeatherPresent reports whether the atmospheric observation set was populated.
func WeatherPresent(w domain.Weather) bool {
	return w.GustKmh > 0 || w.WindKmh > 0 || strings.TrimSpace(w.LightningRisk) != "" || !w.Time.IsZero()
}

// MissingObservations names the observation sets that were not retrieved, in the
// order a reader would care about them. An empty result means nothing is
// missing.
func MissingObservations(m domain.Marine, w domain.Weather) []string {
	var missing []string
	if !MarinePresent(m) {
		missing = append(missing, "sea state and water temperature")
	}
	if !WeatherPresent(w) {
		missing = append(missing, "wind and convective activity")
	}
	return missing
}

// observationHazard builds the hazard that discloses the gap.
//
// It is SevCaution by construction: the verdict is downgraded from "go" to
// "caution" without asserting that the sea is dangerous, because it is not
// known either way.
func observationHazard(missing []string) domain.Hazard {
	list := strings.Join(missing, " and ")
	plural := "were"
	if len(missing) == 1 {
		plural = "was"
	}
	return domain.Hazard{
		Kind:     "observations",
		Severity: SevCaution,
		Value:    "not retrieved",
		Limit:    "live or snapshot observation required",
		Note: fmt.Sprintf(
			"The %s %s not retrieved, so this is not an all-clear. The conditions below are "+
				"absent rather than favourable, and they must not be read as a safe sea. Check the "+
				"local forecast and the harbour master's advice before deciding to sail.",
			list, plural),
	}
}

// benignOrAbsent returns h, or a row that reports the absence instead.
//
// The gate above already stops the verdict reaching "go" when an observation
// set is missing, but the individual rows kept their reassuring wording: the
// same screen said "the conditions below are absent rather than favourable" and
// then, two lines under it, "sea state is within safe limits for small craft".
// A user scrolling the list would take away the second line.
//
// So an absent set must not produce a benign row. Only a measurement that was
// actually made and sat below every threshold is allowed to say the condition
// is fine; an unmeasured one has to say it is unmeasured. Severity stays
// SevCaution, matching the observations row, so a missing reading can never
// quieten the verdict.
func benignOrAbsent(kind string, absent bool, h domain.Hazard) domain.Hazard {
	if !absent {
		return h
	}
	return domain.Hazard{
		Kind:     kind,
		Severity: SevCaution,
		Value:    "not reported",
		Limit:    h.Limit,
		Note: "No reading was retrieved for this, so the figure above is an absent value " +
			"rather than a calm one. This is not evidence that the condition is safe; " +
			"check the local forecast before deciding to sail.",
	}
}
