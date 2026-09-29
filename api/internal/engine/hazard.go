package engine

import (
	"fmt"

	"orca/internal/domain"
)

// Hazard thresholds. Exported so the UI can state the rule it is applying
// rather than showing a verdict whose basis is invisible.
//
// The values are calibrated against what artisanal fishing operations on the
// Indian coast actually tolerate, not against a generic weather scale. A
// system that cries wolf on a fresh breeze loses the trust of the people whose
// safety depends on it, so the caution band is set at the point of real
// operational difficulty and the critical band above it.
const (
	// WaveCautionM is where smaller craft begin to lose working speed and take
	// water on deck; INCOIS sector warnings are generally issued in this range.
	WaveCautionM = 2.5
	// WaveCriticalM is where small vessels should not operate at all.
	WaveCriticalM = 4.0
	// GustCautionKMH is Beaufort 6–7, where gusts begin to capsize small craft
	// and make handling a loaded boat dangerous.
	GustCautionKMH = 40.0
	// GustCriticalKMH is Beaufort 9, above which a small open boat is not
	// seaworthy in any practical sense.
	GustCriticalKMH = 55.0
	// TideCautionM indicates strong tidal current near the coast. The Indian
	// coastline is mostly mesotidal, so this is a genuine signal rather than a
	// routine one.
	TideCautionM = 2.0
	// AnomalyCautionC downgrades confidence in a productivity reading when the
	// water is anomalously warm.
	AnomalyCautionC = 1.5
)

const (
	SevOK      = "ok"
	SevCaution = "caution"
	SevCrit    = "critical"
)

// Assess produces the safety verdict from observations.
//
// This function is the safety authority of the entire system. No model output
// reaches it, it performs no I/O, and it is a pure function of its arguments.
// Every finding carries the observed value, the threshold it was compared
// against, and a note, so the verdict can be audited line by line.
func Assess(m domain.Marine, w domain.Weather, pfz domain.PFZ) domain.Verdict {
	haz := make([]domain.Hazard, 0, 5)
	rat := make([]string, 0, 6)

	// 1. Significant wave height — the dominant hazard for small craft.
	switch {
	case m.WaveHeightM >= WaveCriticalM:
		haz = append(haz, domain.Hazard{
			Kind: "waves", Severity: SevCrit,
			Value: fmt.Sprintf("%.2f m", m.WaveHeightM),
			Limit: fmt.Sprintf("%.1f m critical", WaveCriticalM),
			Note:  "Significant wave height is above the level at which small fishing vessels should not operate.",
		})
		rat = append(rat, fmt.Sprintf("Significant wave height of %.2f m exceeds the %.1f m critical threshold — the sea state alone is reason to stay ashore.",
			m.WaveHeightM, WaveCriticalM))
	case m.WaveHeightM >= WaveCautionM:
		haz = append(haz, domain.Hazard{
			Kind: "waves", Severity: SevCaution,
			Value: fmt.Sprintf("%.2f m", m.WaveHeightM),
			Limit: fmt.Sprintf("%.1f m caution", WaveCautionM),
			Note:  "Rough sea. Smaller boats should reconsider and check locally before departure.",
		})
		rat = append(rat, fmt.Sprintf("Significant wave height of %.2f m is elevated; treat with caution.",
			m.WaveHeightM))
	default:
		haz = append(haz, domain.Hazard{
			Kind: "waves", Severity: SevOK,
			Value: fmt.Sprintf("%.2f m", m.WaveHeightM),
			Limit: fmt.Sprintf("%.1f m caution", WaveCautionM),
			Note:  "Sea state is within safe limits for small craft.",
		})
	}

	// 2. Wind gusts. Gusts rather than sustained wind, because gusts are what
	// actually capsize small boats.
	switch {
	case w.GustKmh >= GustCriticalKMH:
		haz = append(haz, domain.Hazard{
			Kind: "wind", Severity: SevCrit,
			Value: fmt.Sprintf("%.0f km/h gusts", w.GustKmh),
			Limit: fmt.Sprintf("%.0f km/h critical", GustCriticalKMH),
			Note:  "Gusts exceed the limit for safe handling of a small vessel.",
		})
		rat = append(rat, fmt.Sprintf("Forecast gusts of %.0f km/h exceed the %.0f km/h critical threshold.",
			w.GustKmh, GustCriticalKMH))
	case w.GustKmh >= GustCautionKMH:
		haz = append(haz, domain.Hazard{
			Kind: "wind", Severity: SevCaution,
			Value: fmt.Sprintf("%.0f km/h gusts", w.GustKmh),
			Limit: fmt.Sprintf("%.0f km/h caution", GustCautionKMH),
			Note:  "Strong gusts. Secure gear and consider a smaller craft.",
		})
		rat = append(rat, fmt.Sprintf("Forecast gusts of %.0f km/h are elevated.", w.GustKmh))
	default:
		haz = append(haz, domain.Hazard{
			Kind: "wind", Severity: SevOK,
			Value: fmt.Sprintf("%.0f km/h gusts", w.GustKmh),
			Limit: fmt.Sprintf("%.0f km/h caution", GustCautionKMH),
			Note:  "Wind is within safe limits.",
		})
	}

	// 3. Convection / lightning proxy.
	switch w.LightningRisk {
	case "high":
		haz = append(haz, domain.Hazard{
			Kind: "convection", Severity: SevCrit,
			Value: "high",
			Limit: "WMO code 95–99 or heavy rain with high cloud",
			Note:  "Thunderstorm activity forecast. Lightning over water is the single most common cause of fishing fatalities.",
		})
		rat = append(rat, "Thunderstorm activity is forecast in the window; do not be at sea during convection.")
	case "moderate":
		haz = append(haz, domain.Hazard{
			Kind: "convection", Severity: SevCaution,
			Value: "moderate",
			Limit: "WMO code 95–99 or heavy rain with high cloud",
			Note:  "Convective showers possible. Keep a means of shelter to hand.",
		})
		rat = append(rat, "Some convective risk is present in the window.")
	default:
		haz = append(haz, domain.Hazard{
			Kind: "convection", Severity: SevOK,
			Value: string(w.LightningRisk),
			Limit: "WMO code 95–99 or heavy rain with high cloud",
			Note:  "No significant convective activity indicated.",
		})
	}

	// 4. Tidal range. Strong tidal flow restricts manoeuvreability near the coast.
	if m.TideM >= TideCautionM {
		haz = append(haz, domain.Hazard{
			Kind: "tide", Severity: SevCaution,
			Value: fmt.Sprintf("%.2f m", m.TideM),
			Limit: fmt.Sprintf("%.1f m", TideCautionM),
			Note:  "Large tidal range; tidal currents will be strong near the coast.",
		})
		rat = append(rat, fmt.Sprintf("Tidal range of %.2f m indicates strong tidal currents near shore.", m.TideM))
	} else {
		haz = append(haz, domain.Hazard{
			Kind: "tide", Severity: SevOK,
			Value: fmt.Sprintf("%.2f m", m.TideM),
			Limit: fmt.Sprintf("%.1f m", TideCautionM),
			Note:  "Tidal range is moderate.",
		})
	}

	// 5. Zone confidence. Not a hazard to life, so never escalates the verdict
	// beyond caution — it qualifies the fishing advice instead.
	if pfz.Confidence == "low" {
		haz = append(haz, domain.Hazard{
			Kind: "zone", Severity: SevCaution,
			Value: pfz.Confidence + " confidence",
			Limit: "high confidence",
			Note:  "The identified zone is a weak signal; productivity here is uncertain.",
		})
	}

	v := domain.Verdict{Hazards: haz, Rationale: rat}
	worst := SevOK
	for _, h := range haz {
		if h.Severity == SevCrit {
			worst = SevCrit
			break
		}
		if h.Severity == SevCaution {
			worst = SevCaution
		}
	}
	switch worst {
	case SevCrit:
		v.Level, v.Severity = "no-go", SevCrit
	case SevCaution:
		v.Level, v.Severity = "caution", SevCaution
	default:
		v.Level, v.Severity = "go", SevOK
	}

	if len(rat) == 0 {
		rat = append(rat, "No elevated hazard was detected in the requested window.")
	}
	v.Rationale = rat
	return v
}
