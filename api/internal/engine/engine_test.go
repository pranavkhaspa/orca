package engine

import (
	"math"
	"testing"

	"orca/internal/domain"
)

func approx(t *testing.T, got, want, tol float64, what string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.4f, want %.4f (±%.4f)", what, got, want, tol)
	}
}

func TestSstBandScore(t *testing.T) {
	tests := []struct {
		sst  float64
		want float64
	}{
		{24.0, 1.0}, // inclusive lower bound
		{27.0, 1.0}, // mid-band
		{30.0, 1.0}, // inclusive upper bound
		{23.0, 2.0 / 3.0},
		{31.0, 2.0 / 3.0},
		{21.0, 0.0}, // exactly at falloff distance
		{18.0, 0.0}, // well outside
		{33.0, 0.0},
		{0, 0.0}, // missing / land
		{-1, 0.0},
	}
	for _, tc := range tests {
		approx(t, SstBandScore(tc.sst), tc.want, 1e-9, "SstBandScore("+ftoa(tc.sst)+")")
	}
}

func TestSstBandScoreNeverExceedsOne(t *testing.T) {
	// A sweep guards against a future edit that lets a score escape 0..1.
	for sst := -5.0; sst <= 45.0; sst += 0.25 {
		s := SstBandScore(sst)
		if s < 0 || s > 1 {
			t.Fatalf("SstBandScore(%.2f) = %.3f, outside 0..1", sst, s)
		}
	}
}

func TestSeaStateScore(t *testing.T) {
	if got := SeaStateScore(0.5, 10); got != 1.0 {
		t.Errorf("calm long-period sea = %.3f, want 1.0", got)
	}
	// A tall but long-period swell is penalised less than a short chop.
	tall := SeaStateScore(2.0, 12)
	steep := SeaStateScore(2.0, 5)
	if steep >= tall {
		t.Errorf("short-period swell (%.3f) should score below long-period (%.3f)", steep, tall)
	}
	if got := SeaStateScore(6.0, 4); got != 0 {
		t.Errorf("very rough confused sea = %.3f, want 0 (clamped)", got)
	}
	for h := 0.0; h <= 10.0; h += 0.1 {
		for p := 0.0; p <= 20.0; p += 0.5 {
			if s := SeaStateScore(h, p); s < 0 || s > 1 {
				t.Fatalf("SeaStateScore(%.1f,%.1f) = %.3f, outside 0..1", h, p, s)
			}
		}
	}
}

func TestConfidence(t *testing.T) {
	tests := []struct {
		score, anomaly float64
		want           string
	}{
		{0.8, 0.0, "high"},
		{0.6, 1.0, "high"},
		{0.6, 1.6, "medium"}, // warm anomaly costs confidence
		{0.4, 0.0, "medium"},
		{0.2, 0.0, "low"},
		{0.9, 3.0, "low"},
	}
	for _, tc := range tests {
		if got := Confidence(tc.score, tc.anomaly); got != tc.want {
			t.Errorf("Confidence(%.2f, %.1f) = %q, want %q", tc.score, tc.anomaly, got, tc.want)
		}
	}
}

// calmConditions and stormConditions are the two canonical scenarios every
// verdict test is built from.
func calmConditions() (domain.Marine, domain.Weather) {
	return domain.Marine{
			WaveHeightM: 0.8, WavePeriodS: 9, SwellHeightM: 0.9, SwellPeriodS: 11,
			SSTC: 28.0, TideM: 1.0,
		}, domain.Weather{
			WindKmh: 12, GustKmh: 18, PrecipMm: 0, CloudPct: 20, WxCode: 1, LightningRisk: "low",
		}
}

func TestAssessVerdictLevels(t *testing.T) {
	goodPFZ := domain.PFZ{Score: 0.8, Confidence: "high"}
	poorPFZ := domain.PFZ{Score: 0.1, Confidence: "low"}

	tests := []struct {
		name    string
		mutate  func(*domain.Marine, *domain.Weather)
		pfz     domain.PFZ
		want    string
		wantSev string
	}{
		{
			name: "calm sea with good zone is a go", pfz: goodPFZ,
			want: "go", wantSev: SevOK,
		},
		{
			name: "elevated waves force caution", pfz: goodPFZ,
			mutate: func(m *domain.Marine, _ *domain.Weather) { m.WaveHeightM = WaveCautionM },
			want:   "caution", wantSev: SevCaution,
		},
		{
			name: "critical waves are a no-go", pfz: goodPFZ,
			mutate: func(m *domain.Marine, _ *domain.Weather) { m.WaveHeightM = WaveCriticalM + 0.1 },
			want:   "no-go", wantSev: SevCrit,
		},
		{
			name: "thunderstorm is a no-go even on a flat calm sea", pfz: goodPFZ,
			mutate: func(_ *domain.Marine, w *domain.Weather) { w.LightningRisk = "high" },
			want:   "no-go", wantSev: SevCrit,
		},
		{
			name: "critical gusts are a no-go", pfz: goodPFZ,
			mutate: func(_ *domain.Marine, w *domain.Weather) { w.GustKmh = GustCriticalKMH + 1 },
			want:   "no-go", wantSev: SevCrit,
		},
		{
			name: "large tide alone is caution, never no-go", pfz: goodPFZ,
			mutate: func(m *domain.Marine, _ *domain.Weather) { m.TideM = TideCautionM + 0.5 },
			want:   "caution", wantSev: SevCaution,
		},
		{
			name: "a low-confidence zone alone is caution", pfz: poorPFZ,
			want: "caution", wantSev: SevCaution,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, w := calmConditions()
			if tc.mutate != nil {
				tc.mutate(&m, &w)
			}
			v := Assess(m, w, tc.pfz)
			if v.Level != tc.want {
				t.Errorf("verdict = %q, want %q (hazards: %+v)", v.Level, tc.want, v.Hazards)
			}
			if v.Severity != tc.wantSev {
				t.Errorf("severity = %q, want %q", v.Severity, tc.wantSev)
			}
			if len(v.Rationale) == 0 {
				t.Error("verdict must carry at least one rationale line")
			}
			if len(v.Hazards) == 0 {
				t.Error("verdict must report every checked hazard, including the ones that passed")
			}
		})
	}
}

// TestAssessIsDeterministic is the core safety guarantee: identical inputs must
// yield an identical verdict, always. Nothing in Assess reads time, randomness,
// or any external state.
func TestAssessIsDeterministic(t *testing.T) {
	m, w := calmConditions()
	w.LightningRisk = "moderate"
	p := domain.PFZ{Score: 0.5, Confidence: "medium"}
	first := Assess(m, w, p)
	for i := 0; i < 200; i++ {
		got := Assess(m, w, p)
		if got.Level != first.Level || got.Severity != first.Severity {
			t.Fatalf("verdict changed between runs: %q/%q then %q/%q",
				first.Level, first.Severity, got.Level, got.Severity)
		}
		if len(got.Hazards) != len(first.Hazards) {
			t.Fatalf("hazard count changed between runs")
		}
	}
}

// TestAssessThresholdBoundaries pins the exact behaviour at each documented
// threshold, since these numbers are published to users as rules.
func TestAssessThresholdBoundaries(t *testing.T) {
	cases := []struct {
		waveM   float64
		wantSev string
	}{
		{WaveCautionM - 0.01, SevOK},
		{WaveCautionM, SevCaution},
		{WaveCriticalM - 0.01, SevCaution},
		{WaveCriticalM, SevCrit},
	}
	for _, c := range cases {
		m, w := calmConditions()
		m.WaveHeightM = c.waveM
		v := Assess(m, w, domain.PFZ{Score: 0.8, Confidence: "high"})
		if v.Severity != c.wantSev {
			t.Errorf("wave %.2f m → severity %q, want %q", c.waveM, v.Severity, c.wantSev)
		}
	}
}

// TestWorstCaseWins checks that a single critical hazard is never diluted by
// several passing ones.
func TestWorstCaseWins(t *testing.T) {
	m, w := calmConditions()
	m.WaveHeightM = 0.2
	w.GustKmh = 5
	w.LightningRisk = "low"
	m.TideM = 0.1
	w.PrecipMm = 0
	w.CloudPct = 0
	m.SSTC = 28
	// Wind is the only elevated factor.
	w.GustKmh = GustCautionKMH
	v := Assess(m, w, domain.PFZ{Score: 0.9, Confidence: "high"})
	if v.Level != "caution" {
		t.Fatalf("verdict = %q, want caution", v.Level)
	}
	// Now make wind critical while everything else stays ideal.
	w.GustKmh = GustCriticalKMH
	v = Assess(m, w, domain.PFZ{Score: 0.9, Confidence: "high"})
	if v.Level != "no-go" {
		t.Fatalf("verdict = %q, want no-go", v.Level)
	}
}

// TestEvaluateReportsAnomaly guards the honesty requirement: a warm anomaly must
// be surfaced to the user as a reason to distrust the zone.
func TestEvaluateReportsAnomaly(t *testing.T) {
	c := Cell{SSTC: 28, WaveHeightM: 1, SwellPerS: 12}
	_, _, cool := Evaluate(c, -1.5)
	_, _, warm := Evaluate(c, 2.5)
	if len(cool) == 0 {
		t.Fatal("active upwelling should be explained")
	}
	found := false
	for _, r := range warm {
		if containsFold(r, "warmer than the seasonal baseline") {
			found = true
		}
	}
	if !found {
		t.Error("a warm anomaly must appear in the reasoning shown to the user")
	}
}

func TestEvaluateExplainsEveryScore(t *testing.T) {
	_, _, r := Evaluate(Cell{SSTC: 26, WaveHeightM: 3.2, SwellPerS: 5}, 0)
	if len(r) < 3 {
		t.Errorf("expected temperature, sea-state and swell explanations, got %d", len(r))
	}
}

func containsFold(s, sub string) bool {
	return len(s) >= len(sub) && indexFold(s, sub) >= 0
}

func indexFold(s, sub string) int {
	ls, lsub := lower(s), lower(sub)
	for i := 0; i+len(lsub) <= len(ls); i++ {
		if ls[i:i+len(lsub)] == lsub {
			return i
		}
	}
	return -1
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}

func ftoa(f float64) string {
	if f == math.Trunc(f) {
		return string(rune('0' + int(f))) // good enough for test labels in 0..99
	}
	return "x"
}
