package data

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"orca/internal/domain"
)

// ---- Open-Meteo Marine -----------------------------------------------------

type omHourly struct {
	Time           []string  `json:"time"`
	WaveHeight     []float64 `json:"wave_height"`
	WavePeriod     []float64 `json:"wave_period"`
	WaveDirection  []float64 `json:"wave_direction"`
	SwellHeight    []float64 `json:"swell_wave_height"`
	SwellPeriod    []float64 `json:"swell_wave_period"`
	WindWaveHeight []float64 `json:"wind_wave_height"`
	SST            []float64 `json:"sea_surface_temperature"`
	SeaLevelMSL    []float64 `json:"sea_level_height_msl"`
}

type omResponse struct {
	Hourly omHourly `json:"hourly"`
}

const marineBase = "https://marine-api.open-meteo.com/v1/marine"

// MarineWindow is the windowed, safety-aggregated ocean state.
//
// Aggregation is deliberately asymmetric: wave height, swell and tide take the
// worst case in the window, because a safety verdict must not be flattered by
// averaging. SST takes the mean, since the PFZ band is a productivity signal
// rather than a hazard.
func MarineWindow(ctx context.Context, lat, lon float64, windowHours int) (domain.Marine, domain.Citations, error) {
	cits := domain.Citations{}
	vals := []string{
		"wave_height", "wave_period", "wave_direction", "swell_wave_height",
		"swell_wave_period", "wind_wave_height", "sea_surface_temperature",
		"sea_level_height_msl",
	}
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(lat, 'f', 4, 64))
	q.Set("longitude", strconv.FormatFloat(lon, 'f', 4, 64))
	q.Set("hourly", strings.Join(vals, ","))
	q.Set("forecast_days", "5")
	q.Set("timezone", ist)
	endpoint := marineBase + "?" + q.Encode()

	var resp omResponse
	if err := getJSON(ctx, endpoint, &resp); err != nil {
		cits["wave_height"] = domain.Citation{
			Source: "Open-Meteo Marine", Dataset: "all", URL: endpoint,
			Retrieved: time.Now(), Live: false, Err: err.Error(),
		}
		return domain.Marine{}, cits, err
	}
	h := resp.Hourly
	if len(h.Time) == 0 {
		err := fmt.Errorf("empty marine payload")
		cits["wave_height"] = domain.Citation{
			Source: "Open-Meteo Marine", Dataset: "all", URL: endpoint,
			Retrieved: time.Now(), Live: false, Err: err.Error(),
		}
		return domain.Marine{}, cits, err
	}

	n := windowHours
	if n < 1 {
		n = 1
	}
	// Anchor the window on the current hour, not the start of the returned day.
	// A query at 17:20 asking about "the next 6 hours" must describe 18:00–23:00;
	// aggregating from 00:00 would silently report the morning instead. Getting
	// this wrong would misinform someone about conditions at sea.
	start := startIndex(h.Time, time.Now())
	if start >= len(h.Time) {
		start = len(h.Time) - 1
	}
	if start+n > len(h.Time) {
		n = len(h.Time) - start
	}
	if n < 1 {
		n = 1
	}

	maxOf := func(s []float64) float64 {
		m := math.Inf(-1)
		for i := start; i < start+n && i < len(s); i++ {
			if !math.IsNaN(s[i]) {
				m = math.Max(m, s[i])
			}
		}
		if math.IsInf(m, -1) {
			return 0
		}
		return m
	}
	meanOf := func(s []float64) float64 {
		sum, cnt := 0.0, 0
		for i := start; i < start+n && i < len(s); i++ {
			if !math.IsNaN(s[i]) {
				sum += s[i]
				cnt++
			}
		}
		if cnt == 0 {
			return 0
		}
		return sum / float64(cnt)
	}
	at := func(s []float64, i int) float64 {
		if i < len(s) {
			return s[i]
		}
		return 0
	}

	// Wave direction at the peak-height hour, which is the direction that matters.
	peak := start
	for i := start + 1; i < start+n && i < len(h.WaveHeight); i++ {
		if h.WaveHeight[i] > h.WaveHeight[peak] {
			peak = i
		}
	}

	ts, _ := time.ParseInLocation("2006-01-02T15:04", h.Time[start], mustLoadIST())

	m := domain.Marine{
		Time:         ts,
		WaveHeightM:  round2(maxOf(h.WaveHeight)),
		WavePeriodS:  round1(meanOf(h.WavePeriod)),
		WaveDirDeg:   round1(at(h.WaveDirection, peak)),
		SwellHeightM: round2(maxOf(h.SwellHeight)),
		SwellPeriodS: round1(meanOf(h.SwellPeriod)),
		WindWaveM:    round2(maxOf(h.WindWaveHeight)),
		SSTC:         round1(meanOf(h.SST)),
		TideM:        round2(math.Abs(at(h.SeaLevelMSL, peak))),
		Citations:    cits,
	}

	now := time.Now()
	for _, ds := range vals {
		cits[ds] = domain.Citation{
			Source: "Open-Meteo Marine", Dataset: ds, URL: endpoint,
			Retrieved: now, Live: true,
		}
	}
	return m, cits, nil
}

// ---- Open-Meteo Forecast (atmospheric) ------------------------------------

type fcHourly struct {
	Time      []string  `json:"time"`
	WindSpeed []float64 `json:"wind_speed_10m"`
	WindGusts []float64 `json:"wind_gusts_10m"`
	Precip    []float64 `json:"precipitation"`
	Cloud     []float64 `json:"cloud_cover"`
	WxCode    []int     `json:"weather_code"`
}

type fcResponse struct {
	Hourly fcHourly `json:"hourly"`
}

const forecastBase = "https://api.open-meteo.com/v1/forecast"

// ForecastWindow returns the worst-case atmospheric state over the window.
// Gusts dominate the wind figure because gusts are what actually endanger a
// small fishing vessel, not sustained wind.
func ForecastWindow(ctx context.Context, lat, lon float64, windowHours int) (domain.Weather, domain.Citations, error) {
	cits := domain.Citations{}
	vals := []string{"wind_speed_10m", "wind_gusts_10m", "precipitation", "cloud_cover", "weather_code"}
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(lat, 'f', 4, 64))
	q.Set("longitude", strconv.FormatFloat(lon, 'f', 4, 64))
	q.Set("hourly", strings.Join(vals, ","))
	q.Set("forecast_days", "5")
	q.Set("timezone", ist)
	endpoint := forecastBase + "?" + q.Encode()

	var resp fcResponse
	if err := getJSON(ctx, endpoint, &resp); err != nil {
		cits["wind_gusts_10m"] = domain.Citation{
			Source: "Open-Meteo Forecast", Dataset: "all", URL: endpoint,
			Retrieved: time.Now(), Live: false, Err: err.Error(),
		}
		return domain.Weather{}, cits, err
	}
	h := resp.Hourly
	if len(h.Time) == 0 {
		err := fmt.Errorf("empty forecast payload")
		cits["wind_gusts_10m"] = domain.Citation{
			Source: "Open-Meteo Forecast", Dataset: "all", URL: endpoint,
			Retrieved: time.Now(), Live: false, Err: err.Error(),
		}
		return domain.Weather{}, cits, err
	}
	n := windowHours
	if n < 1 {
		n = 1
	}
	// Same current-hour anchoring as the marine window: a forecast asked for
	// "now" must not be answered with this morning's numbers.
	start := startIndex(h.Time, time.Now())
	if start >= len(h.Time) {
		start = len(h.Time) - 1
	}
	if start+n > len(h.Time) {
		n = len(h.Time) - start
	}
	if n < 1 {
		n = 1
	}
	maxOf := func(s []float64) float64 {
		m := math.Inf(-1)
		for i := start; i < start+n && i < len(s); i++ {
			if !math.IsNaN(s[i]) {
				m = math.Max(m, s[i])
			}
		}
		if math.IsInf(m, -1) {
			return 0
		}
		return m
	}
	// Peak thunderstorm code in window: higher WMO code means more severe.
	peakWx := 0
	for i := start; i < start+n && i < len(h.WxCode); i++ {
		if h.WxCode[i] > peakWx {
			peakWx = h.WxCode[i]
		}
	}
	ts, _ := time.ParseInLocation("2006-01-02T15:04", h.Time[start], mustLoadIST())

	w := domain.Weather{
		Time:          ts,
		WindKmh:       round1(maxOf(h.WindSpeed)),
		GustKmh:       round1(maxOf(h.WindGusts)),
		PrecipMm:      round1(maxOf(h.Precip)),
		CloudPct:      round1(maxOf(h.Cloud)),
		WxCode:        peakWx,
		LightningRisk: LightningRisk(peakWx, maxOf(h.Precip), maxOf(h.Cloud)),
		Citations:     cits,
	}
	now := time.Now()
	for _, ds := range vals {
		cits[ds] = domain.Citation{Source: "Open-Meteo Forecast", Dataset: ds, URL: endpoint, Retrieved: now, Live: true}
	}
	return w, cits, nil
}

// LightningRisk derives a convection proxy from the WMO weather code plus
// precipitation and cloud cover.
//
// This is a documented substitute for real lightning detection, which is not
// available from a keyless source. The substitution is surfaced in the output
// rather than hidden, because a safety system must not imply data it lacks.
func LightningRisk(wx int, precipMm, cloudPct float64) string {
	// WMO 95-99 are thunderstorm codes; 65-67 and 82 are heavy rain showers.
	thunder := wx >= 95
	heavy := precipMm >= 5 && cloudPct >= 70
	switch {
	case thunder && precipMm >= 2.5:
		return "high"
	case thunder || heavy:
		return "moderate"
	case precipMm >= 1 && cloudPct >= 50:
		return "low"
	default:
		return "low"
	}
}

// ---- Open-Meteo Archive (SST baseline) ------------------------------------

// SSTBaseline returns the mean sea-surface temperature over a historical window,
// used to compute an anomaly. A warm anomaly indicates weakened upwelling and
// is a legitimate reason to distrust a nominal productivity reading.
// SSTBaseline is the seasonal sea-surface-temperature mean used for the anomaly.
// It returns its citation alongside the value: the anomaly is a displayed number,
// and a displayed number without a source violates standing rule 2.
func SSTBaseline(ctx context.Context, lat, lon float64, days int) (float64, domain.Citation, error) {
	end := time.Now().AddDate(0, 0, -7)
	start := end.AddDate(0, 0, -days)
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(lat, 'f', 4, 64))
	q.Set("longitude", strconv.FormatFloat(lon, 'f', 4, 64))
	q.Set("start_date", start.Format("2006-01-02"))
	q.Set("end_date", end.Format("2006-01-02"))
	q.Set("daily", "sea_surface_temperature_mean")
	q.Set("timezone", ist)
	endpoint := "https://archive-api.open-meteo.com/v1/archive?" + q.Encode()

	var resp struct {
		Daily struct {
			SST []float64 `json:"sea_surface_temperature_mean"`
		} `json:"daily"`
	}
	citation := domain.Citation{
		Source:    "Open-Meteo Archive",
		Dataset:   "sea_surface_temperature_mean",
		URL:       endpoint,
		Retrieved: time.Now(),
		Live:      true,
	}
	if err := getJSON(ctx, endpoint, &resp); err != nil {
		citation.Live, citation.Err = false, err.Error()
		return 0, citation, err
	}
	sum, cnt := 0.0, 0
	for _, v := range resp.Daily.SST {
		if !math.IsNaN(v) {
			sum += v
			cnt++
		}
	}
	if cnt == 0 {
		citation.Live, citation.Err = false, "no archive baseline available"
		return 0, citation, fmt.Errorf("no archive baseline available")
	}
	return round1(sum / float64(cnt)), citation, nil
}

var istLoc *time.Location

// startIndex returns the index of the first hourly stamp at or after now.
//
// A grace period of 30 minutes absorbs clock skew and model run boundaries: we
// would rather include the current hour than skip to the next one.
//
// When every stamp lies in the past — a stale or short series — the last stamp
// is returned. The most recent observation is a far more useful answer than the
// oldest one, and this keeps the index in range without a special case at the
// call site.
func startIndex(times []string, now time.Time) int {
	if len(times) == 0 {
		return 0
	}
	cut := now.Add(-30 * time.Minute)
	last := 0
	for i, s := range times {
		t, err := time.ParseInLocation("2006-01-02T15:04", s, mustLoadIST())
		if err != nil {
			return i
		}
		last = i
		if t.After(cut) {
			return i
		}
	}
	return last
}

func mustLoadIST() *time.Location {
	if istLoc == nil {
		l, err := time.LoadLocation(ist)
		if err != nil {
			return time.UTC
		}
		istLoc = l
	}
	return istLoc
}
