// MET Norway Locationforecast, used as the fallback atmospheric source.
//
// It is here because the primary provider is a free tier that returns 429 under
// any real usage, and a deployment that has already decided a question is
// answered from stored data the moment the primary refuses is not resilient so
// much as merely decorated. Two providers that fail for different reasons are
// worth more than one that fails predictably.
//
// The provider is free, keyless, and covers the whole world. Its terms require a
// descriptive User-Agent with a contact address, and they ask that responses be
// cached rather than re-fetched per view, which the existing TTL cache already
// does.
//
// What it does not carry is wave height — no keyless public source does — so this
// fallback is for the atmospheric half of the answer only. When it supplies the
// weather, that is stated in the citation rather than presented as a seamless
// swap, because a reader deciding whether to launch a boat is entitled to know
// which agency the number came from.

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

const metNoBase = "https://api.met.no/weatherapi/locationforecast/2.0/compact"

type metNoResponse struct {
	Properties struct {
		Timeseries []struct {
			Time time.Time `json:"time"`
			Data metNoData `json:"data"`
		} `json:"timeseries"`
	} `json:"properties"`
}

type metNoData struct {
	Instant struct {
		Details struct {
			AirTemperature    float64 `json:"air_temperature"`
			CloudAreaFraction float64 `json:"cloud_area_fraction"`
			WindSpeed         float64 `json:"wind_speed"`
			WindGust          float64 `json:"wind_gust"`
		} `json:"details"`
	} `json:"instant"`
	Next1Hours struct {
		Summary struct {
			SymbolCode string `json:"symbol_code"`
		} `json:"summary"`
		Details struct {
			PrecipitationAmount float64 `json:"precipitation_amount"`
		} `json:"details"`
	} `json:"next_1_hours"`
}

// ForecastWindowMETNo returns the worst-case atmospheric state over the window
// from MET Norway, in the same shape as ForecastWindow so the two are
// interchangeable to every caller above this layer.
func ForecastWindowMETNo(ctx context.Context, lat, lon float64, windowHours int) (domain.Weather, domain.Citations, error) {
	cits := domain.Citations{}
	q := url.Values{}
	q.Set("lat", strconv.FormatFloat(lat, 'f', 4, 64))
	q.Set("lon", strconv.FormatFloat(lon, 'f', 4, 64))
	endpoint := metNoBase + "?" + q.Encode()

	var resp metNoResponse
	if err := getJSON(ctx, endpoint, &resp); err != nil {
		cits["wind_gusts_10m"] = domain.Citation{
			Source: "MET Norway Locationforecast", Dataset: "all", URL: endpoint,
			Retrieved: time.Now(), Live: false, Err: err.Error(),
		}
		return domain.Weather{}, cits, err
	}
	ts := resp.Properties.Timeseries
	if len(ts) == 0 {
		err := fmt.Errorf("empty MET Norway payload")
		cits["wind_gusts_10m"] = domain.Citation{
			Source: "MET Norway Locationforecast", Dataset: "all", URL: endpoint,
			Retrieved: time.Now(), Live: false, Err: err.Error(),
		}
		return domain.Weather{}, cits, err
	}

	// The timeseries is already hourly and ordered, so the window is simply the
	// entries from the current hour onwards.
	now := time.Now()
	n := windowHours
	if n < 1 {
		n = 1
	}
	var (
		maxWind, maxGust, maxPrecip, maxCloud float64
		peakWx                                int
		first                                 time.Time
		used                                  int
	)
	for _, e := range ts {
		if e.Time.Before(now.Add(-30 * time.Minute)) {
			continue
		}
		if used >= n {
			break
		}
		d := e.Data
		if used == 0 {
			first = e.Time
		}
		// MET Norway reports wind in m/s; the domain is km/h throughout.
		maxWind = math.Max(maxWind, d.Instant.Details.WindSpeed*3.6)
		maxGust = math.Max(maxGust, d.Instant.Details.WindGust*3.6)
		maxPrecip = math.Max(maxPrecip, d.Next1Hours.Details.PrecipitationAmount)
		maxCloud = math.Max(maxCloud, d.Instant.Details.CloudAreaFraction)
		if code := metNoSymbolToWMO(d.Next1Hours.Summary.SymbolCode); code > peakWx {
			peakWx = code
		}
		used++
	}
	if used == 0 {
		err := fmt.Errorf("MET Norway payload has no current entries")
		cits["wind_gusts_10m"] = domain.Citation{
			Source: "MET Norway Locationforecast", Dataset: "all", URL: endpoint,
			Retrieved: time.Now(), Live: false, Err: err.Error(),
		}
		return domain.Weather{}, cits, err
	}

	// MET Norway's compact endpoint frequently omits wind_gust entirely, and
	// the engine treats gusts as the figure that decides a go/no-go for a small
	// vessel. Reporting zero for a field that was never published would make a
	// thunderstorm look calmer than it is, which is the one direction this code
	// must not err in. Sustained wind is used as a floor instead: it is the
	// same field from the same forecast at the same place, and it is always
	// present, so a vessel that leaves on this reading leaves expecting at least
	// as much wind as the source actually published.
	if maxGust < maxWind {
		maxGust = maxWind
	}

	w := domain.Weather{
		Time:          first,
		WindKmh:       round1(maxWind),
		GustKmh:       round1(maxGust),
		PrecipMm:      round1(maxPrecip),
		CloudPct:      round1(maxCloud),
		WxCode:        peakWx,
		LightningRisk: LightningRisk(peakWx, maxPrecip, maxCloud),
	}
	cits["wind_gusts_10m"] = domain.Citation{
		Source: "MET Norway Locationforecast", Dataset: "all", URL: endpoint,
		Retrieved: time.Now(), Live: true,
	}
	cits["wind_speed_10m"] = cits["wind_gusts_10m"]
	cits["precipitation"] = cits["wind_gusts_10m"]
	cits["cloud_cover"] = cits["wind_gusts_10m"]
	w.Citations = cits
	return w, cits, nil
}

// metNoSymbolToWMO maps MET Norway's symbol codes onto the WMO codes the rest of
// the system speaks.
//
// Only the distinctions that change a safety answer are preserved: thunder is
// thunder, rain is rain, and the cloud/clear distinction does not move a verdict
// on its own. Mapping everything to a single "cloudy" value would be simpler and
// would throw away the one signal the engine treats as a hazard.
func metNoSymbolToWMO(sym string) int {
	s := strings.ToLower(sym)
	// "_day"/"_night"/"_polartwilight" suffixes carry no meteorological weight.
	if i := strings.Index(s, "_"); i > 0 {
		if suffix := s[i:]; suffix == "_day" || suffix == "_night" || suffix == "_polartwilight" {
			s = s[:i]
		}
	}
	switch {
	case strings.Contains(s, "thunder"):
		// 95 is "th thunderstorm", the lowest thunder code, so a storm with less
		// precipitation than a 99 still registers as thunder.
		return 95
	case strings.Contains(s, "rainshowers"):
		return 80
	case strings.Contains(s, "sleatshowers"), strings.Contains(s, "snowshowers"):
		return 85
	case strings.Contains(s, "rain"):
		return 61
	case strings.Contains(s, "sleet"), strings.Contains(s, "snow"):
		return 71
	case strings.Contains(s, "fog"):
		return 45
	case strings.Contains(s, "cloudy"), strings.Contains(s, "overcast"):
		return 3
	}
	return 0
}
