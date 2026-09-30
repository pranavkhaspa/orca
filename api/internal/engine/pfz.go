// Package engine holds the deterministic marine reasoning that produces the
// safety verdict. It performs no I/O and calls no model: given the same
// observations it always returns the same verdict, which is what makes the
// verdict auditable (architecture.md §2).
package engine

import (
	"fmt"

	"orca/internal/domain"
)

// PFZ band thresholds.
//
// INCOIS derives its Potential Fishing Zone advisories from satellite SST and
// chlorophyll, and the productive band for Indian coastal waters sits in the
// mid-twenties: warm-enough water with active upwelling. We use 24-30 C as that
// band. The constants are exported so the UI can explain the rule rather than
// present a bare number.
const (
	// Aliased from the shared contract so the engine and the renderer cannot
	// disagree about the band.
	PFZBandLoC = domain.PFZBandLoC
	PFZBandHiC = domain.PFZBandHiC
	// BandFalloffC is the distance outside the band at which the score reaches
	// zero. 3 C is chosen because productivity collapses quickly once the water
	// is well outside the upwelling-favourable window.
	BandFalloffC = 3.0
	// SafeWaveCeilingM is the wave height above which a small fishing vessel
	// should not operate, and the point at which the sea-state term starts to
	// dominate the PFZ score.
	SafeWaveCeilingM = 2.5
	// LongSwellPeriodS is the period above which swell is treated as settled
	// and predictable rather than confused and short-period.
	LongSwellPeriodS = 8.0
)

// SstBandScore scores how favourable a sea-surface temperature is for a fishing
// zone, on 0..1, peaking at 1.0 inside the productive band and decaying
// linearly outside it.
func SstBandScore(sstC float64) float64 {
	if sstC <= 0 {
		return 0 // missing or land
	}
	if sstC >= PFZBandLoC && sstC <= PFZBandHiC {
		return 1.0
	}
	var d float64
	if sstC < PFZBandLoC {
		d = PFZBandLoC - sstC
	} else {
		d = sstC - PFZBandHiC
	}
	s := 1.0 - d/BandFalloffC
	if s < 0 {
		return 0
	}
	return s
}

// SeaStateScore scores how workable the sea is, on 0..1.
//
// Two penalties are applied. Calm water scores full marks; above the safe
// ceiling the score falls away because an unfishable zone has no value. Short
// period swell is penalised separately: steep, confused water is hazardous to
// small craft even when the significant height looks tolerable, because the
// height statistic under-represents individual breaking waves.
func SeaStateScore(waveH, swellPeriodS float64) float64 {
	s := 1.0
	if waveH > 1.0 {
		s -= (waveH - 1.0) * 0.18
	}
	if swellPeriodS > 0 && swellPeriodS < LongSwellPeriodS {
		s -= (LongSwellPeriodS - swellPeriodS) * 0.05
	}
	if s < 0 {
		return 0
	}
	return s
}

// Cell is one candidate location in the offshore search.
type Cell struct {
	Lat         float64
	Lon         float64
	DistanceKm  float64
	BearingDeg  float64
	SSTC        float64
	WaveHeightM float64
	SwellPerS   float64
}

// Evaluate scores a single cell and explains the score in plain terms. The
// reasoning strings are surfaced directly in the answer, so a user can see why
// a location was chosen rather than being handed a number.
func Evaluate(c Cell, anomalyC float64) (score float64, band string, reasoning []string) {
	sstScore := SstBandScore(c.SSTC)
	seaScore := SeaStateScore(c.WaveHeightM, c.SwellPerS)
	score = sstScore * seaScore

	switch {
	case sstScore == 1.0:
		band = fmt.Sprintf("optimal (%.1f°C inside the %g–%g°C band)", c.SSTC, PFZBandLoC, PFZBandHiC)
		reasoning = append(reasoning, fmt.Sprintf(
			"Sea surface temperature %.1f°C sits inside the %g–%g°C productive band used for fishing-zone identification.",
			c.SSTC, PFZBandLoC, PFZBandHiC))
	case sstScore == 0:
		band = "unfavourable"
		reasoning = append(reasoning, fmt.Sprintf(
			"Sea surface temperature %.1f°C is outside the productive band by more than %g°C.", c.SSTC, BandFalloffC))
	default:
		band = fmt.Sprintf("marginal (%.1f°C)", c.SSTC)
		reasoning = append(reasoning, fmt.Sprintf(
			"Sea surface temperature %.1f°C is only partly inside the productive band.", c.SSTC))
	}

	if c.WaveHeightM > SafeWaveCeilingM {
		reasoning = append(reasoning, fmt.Sprintf(
			"Significant wave height %.2f m exceeds the %.1f m working ceiling, reducing fishing value.",
			c.WaveHeightM, SafeWaveCeilingM))
	} else {
		reasoning = append(reasoning, fmt.Sprintf(
			"Sea state is workable: %.2f m waves.", c.WaveHeightM))
	}

	if c.SwellPerS > 0 && c.SwellPerS < LongSwellPeriodS {
		reasoning = append(reasoning, fmt.Sprintf(
			"Swell period %.1f s is short, indicating confused water that is hazardous to small craft.", c.SwellPerS))
	}

	// A warm anomaly means weakened upwelling, which undermines a nominal
	// temperature reading. This is a real reason to distrust the score, so it
	// is stated rather than buried in a confidence field.
	if anomalyC >= 1.5 {
		reasoning = append(reasoning, fmt.Sprintf(
			"Water is %.1f°C warmer than the seasonal baseline, which indicates weak upwelling; treat this zone with caution.", anomalyC))
	} else if anomalyC <= -1.0 {
		reasoning = append(reasoning, fmt.Sprintf(
			"Water is %.1f°C cooler than the seasonal baseline, consistent with active upwelling.", anomalyC))
	}

	return score, band, reasoning
}

// Confidence grades how much weight the PFZ reading deserves.
func Confidence(score, anomalyC float64) string {
	if score >= 0.6 && anomalyC < 1.5 && anomalyC > -1.0 {
		return "high"
	}
	if score >= 0.3 && anomalyC < 2.5 {
		return "medium"
	}
	return "low"
}
