package data

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"orca/internal/domain"
)

// INCOIS publishes its daily Potential Fishing Zone bulletin as an OGC Web
// Feature Service. The bulletin is not just a text notice: it is a set of
// MultiLineString sector boundaries with a state name, a sector number and the
// Julian day it describes.
//
// Reading it as geometry rather than as prose is what turns the official
// advisory from "somewhere near this coast" into a measured distance. That
// distance is a real number with a real source, and it is the only external
// ground truth available for scoring our own zone computation, so it is worth
// the extra precision.
//
// The bulletin is corroborated, never obeyed. Our verdict and our zone are
// computed from observation; this tells the user how far the official advice
// sits from what we computed, which is a different and safer claim.
const incoisWFS = "https://incois.gov.in/geoserver/PFZ_Automation/ows"

// OfficialZone is the nearest INCOIS-published zone line to a point.
//
// Found is separate from Available deliberately. A bulletin that loads and
// contains no line near the waypoint is a successful check with a negative
// answer, and that is information — the Gujarat coast has no published line on
// many days. Collapsing the two cases would report a working source as broken.
type OfficialZone struct {
	Checked    bool
	Found      bool
	DistanceKm float64
	BearingDeg float64
	State      string
	Sector     string
	Bulletin   time.Time // the day this advisory describes
	Retrieved  time.Time
	Live       bool
	Err        string
	Citation   domain.Citation

	// lines retains the geometry of the winning feature so the distance can be
	// re-measured against a point that was not known when the bulletin was
	// fetched. See MeasureTo.
	lines [][][]float64
}

// MeasureTo re-measures the retained bulletin line against a different point.
//
// This exists because of an ordering problem, not a cosmetic one. The bulletin
// is fetched while the marine and weather agents are still running, in order to
// overlap the network round trip, but the point the answer is really about —
// the zone ORCA computes — does not exist until those agents return. Measuring
// against the waypoint instead and calling the result "our zone" would put a
// number on screen that is wrong by however far the zone was fanned from the
// waypoint, which can be tens of kilometres. Re-measuring is exact and costs
// no extra request.
func (z OfficialZone) MeasureTo(lat, lon float64) OfficialZone {
	if len(z.lines) == 0 {
		return z
	}
	d, bearing, ok := nearestOnLines(z.lines, lat, lon)
	if !ok {
		return z
	}
	z.Found = true
	z.DistanceKm = round1(d)
	z.BearingDeg = round2(bearing)
	return z
}

// HasGeometry reports whether a bulletin line was actually retrieved, and so
// whether MeasureTo can refine the measurement.
func (z OfficialZone) HasGeometry() bool { return len(z.lines) > 0 }

// bulletinFeature is one PFZ_Automation:pfzlines feature, in the exact shape the
// GeoServer returns. Only the fields we report are decoded; the rest are ignored
// by encoding/json rather than being modelled.
type bulletinFeature struct {
	Geometry struct {
		Type        string          `json:"type"`
		Coordinates [][][]float64   `json:"coordinates"`
		Geometries  []bulletinPoint `json:"geometries"`
	} `json:"geometry"`
	Properties struct {
		StateName  string `json:"State_Name"`
		SectorNo   string `json:"SECTORBOUN"`
		JulianDay  string `json:"Julian_day"`
		Year       int    `json:"Year"`
		SectorNoDu string `json:"SECTORBO_1"`
	} `json:"properties"`
}

// bulletinPoint is unused by the LineString case but keeps the type reference
// valid if the server ever returns a GeometryCollection.
type bulletinPoint = struct {
	Coordinates [][]float64 `json:"coordinates"`
}

type bulletinResponse struct {
	Features []bulletinFeature `json:"features"`
}

// OfficialPFZ returns the nearest published zone line within radiusKm of the
// point, widening the search if the first box contains no lines.
//
// The widening matters: a tight box around a waypoint just outside every
// published line returns nothing, and reporting "INCOIS has no advisory here"
// when the nearest line is 40 km away would be a false negative on a
// safety-adjacent feature. Two doublings cover the gap between our search box
// and the neighbouring sector without ever pulling the whole 1.9 MB bulletin.
func OfficialPFZ(ctx context.Context, lat, lon, radiusKm float64) OfficialZone {
	if radiusKm <= 0 {
		radiusKm = 75
	}
	// The bulletin is republished once a day, and the service is a public
	// government endpoint that other people's tools also depend on. Caching for
	// half an hour is far shorter than the data can meaningfully change and long
	// enough that a busy deployment asks GeoServer for one request per coastal
	// area rather than one per user query.
	if z, ok := pfzCacheGet(lat, lon, radiusKm); ok {
		return z
	}
	z := OfficialPFZUncached(ctx, lat, lon, radiusKm)
	pfzCachePut(lat, lon, radiusKm, z)
	return z
}

// OfficialPFZUncached performs the WFS request without consulting the cache.
func OfficialPFZUncached(ctx context.Context, lat, lon, radiusKm float64) OfficialZone {
	z := OfficialZone{Retrieved: time.Now()}
	if radiusKm <= 0 {
		radiusKm = 75
	}

	// Widening attempts: 1x, 2x, 4x of the requested radius, capped so a
	// pathological request cannot ask GeoServer for the whole EEZ.
	radii := []float64{radiusKm, math.Min(radiusKm*2, 400), math.Min(radiusKm*4, 600)}

	for i, r := range radii {
		endpoint, err := pfzURL(lat, lon, r)
		if err != nil {
			z.Err = err.Error()
			break
		}

		var resp bulletinResponse
		if err := getJSON(ctx, endpoint, &resp); err != nil {
			z.Err = err.Error()
			break
		}
		z.Checked = true
		z.Live = true
		z.Citation = domain.Citation{
			Source:    "INCOIS",
			Dataset:   "PFZ_Automation:pfzlines",
			URL:       endpoint,
			Retrieved: time.Now(),
			Live:      true,
		}
		if len(resp.Features) == 0 {
			// Keep searching wider, but remember that this box was empty.
			continue
		}
		if err := z.consider(resp.Features, lat, lon); err != nil {
			z.Err = err.Error()
			break
		}
		if z.Found {
			return z
		}
		_ = i
	}

	if z.Citation.Dataset == "" {
		z.Citation = domain.Citation{
			Source:    "INCOIS",
			Dataset:   "PFZ_Automation:pfzlines",
			URL:       incoisWFS,
			Retrieved: time.Now(),
			Live:      z.Live,
			Err:       z.Err,
		}
	}
	if z.Err == "" && z.Checked && !z.Found {
		z.Err = "no published zone line within the searched area"
	}
	return z
}

// consider scans every line in the response and keeps the closest segment.
func (z *OfficialZone) consider(features []bulletinFeature, lat, lon float64) error {
	for _, f := range features {
		lines := f.Geometry.Coordinates
		if len(lines) == 0 {
			continue
		}
		state := f.Properties.StateName
		sector := f.Properties.SectorNo
		if sector == "" {
			sector = f.Properties.SectorNoDu
		}
		if d, bearing, ok := nearestOnLines(lines, lat, lon); ok && (!z.Found || d < z.DistanceKm) {
			z.Found = true
			z.DistanceKm = round1(d)
			z.BearingDeg = round2(bearing)
			z.State = state
			z.Sector = sector
			z.Bulletin = bulletinDate(f.Properties.Year, f.Properties.JulianDay)
			z.lines = lines
		}
	}
	return nil
}

// nearestOnLines returns the distance in km from the point to the closest
// segment of any polyline, and the bearing to it.
//
// The projection is equirectangular about the query point. Over the few hundred
// kilometres this function ever sees, that is accurate to well under a metre,
// and unlike a per-vertex haversine it does not overestimate the distance to
// the interior of a long segment — which matters because the bulletin draws
// each sector as hundreds of vertices.
func nearestOnLines(lines [][][]float64, lat, lon float64) (distKm, bearingDeg float64, ok bool) {
	lat0 := lat * math.Pi / 180
	// Kilometres per degree of longitude shrink with the cosine of latitude.
	kx := 111.32 * math.Cos(lat0)
	const ky = 110.57

	toXY := func(lo, la float64) (x, y float64) {
		return (lo - lon) * kx, (la - lat) * ky
	}
	best := math.Inf(1)
	for _, line := range lines {
		for i := 0; i+1 < len(line); i++ {
			ax, ay := toXY(line[i][0], line[i][1])
			bx, by := toXY(line[i+1][0], line[i+1][1])
			d, t := pointSegment(0, 0, ax, ay, bx, by)
			if d < best {
				best = d
				// The foot of the perpendicular is the closest point on the
				// line; take the bearing to it in real coordinates.
				fx, fy := ax+t*(bx-ax), ay+t*(by-ay)
				bearingDeg = math.Mod(math.Atan2(fx, fy)*180/math.Pi+360, 360)
			}
		}
	}
	if math.IsInf(best, 1) {
		return 0, 0, false
	}
	return best, bearingDeg, true
}

// pointSegment returns the distance from the origin to segment AB and the
// parameter of the closest point along it.
func pointSegment(px, py, ax, ay, bx, by float64) (dist, t float64) {
	dx, dy := bx-ax, by-ay
	den := dx*dx + dy*dy
	if den == 0 {
		return math.Hypot(px-ax, py-ay), 0
	}
	t = ((px-ax)*dx + (py-ay)*dy) / den
	switch {
	case t < 0:
		t = 0
	case t > 1:
		t = 1
	}
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy)), t
}

// bulletinDate converts the Julian day the bulletin carries into a calendar
// date. The bulletin is published for the day it describes, not the day it was
// fetched, so a query on 29 Sep can legitimately return the 28 Sep advisory and
// showing the right date is the whole point of reporting it.
func bulletinDate(year int, julian string) time.Time {
	d, err := strconv.Atoi(strings.TrimSpace(julian))
	if err != nil || year < 2000 || d < 1 || d > 366 {
		return time.Time{}
	}
	return time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, d-1)
}

func pfzURL(lat, lon, radiusKm float64) (string, error) {
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return "", fmt.Errorf("incois: point out of range (%.4f, %.4f)", lat, lon)
	}
	dLat := radiusKm / 110.57
	cosLat := math.Cos(lat * math.Pi / 180)
	if math.Abs(cosLat) < 1e-6 {
		cosLat = 1e-6
	}
	dLon := radiusKm / (111.32 * cosLat)

	q := url.Values{}
	q.Set("service", "WFS")
	q.Set("version", "2.0.0")
	q.Set("request", "GetFeature")
	q.Set("typeNames", "PFZ_Automation:pfzlines")
	q.Set("outputFormat", "application/json")
	q.Set("maxFeatures", "40")
	// A small pad avoids missing a line that clips the very corner of the box.
	q.Set("bbox", strings.Join([]string{
		fmt.Sprintf("%.4f", lon-dLon*1.15),
		fmt.Sprintf("%.4f", lat-dLat*1.15),
		fmt.Sprintf("%.4f", lon+dLon*1.15),
		fmt.Sprintf("%.4f", lat+dLat*1.15),
	}, ",")+",EPSG:4326")
	return incoisWFS + "?" + q.Encode(), nil
}

// Label renders the official sector for display, e.g. "ORISSA sector 3".
func (z OfficialZone) Label() string {
	switch {
	case z.State == "" && z.Sector == "":
		return ""
	case z.Sector == "":
		return z.State
	case z.State == "":
		return "sector " + z.Sector
	}
	return z.State + " sector " + z.Sector
}

// PFZSearchRadiusKm is how far around the waypoint the bulletin is searched.
// It is wider than the largest distance the zone can be fanned, so the nearest
// published line is always inside the first box.
const PFZSearchRadiusKm = 90

// pfzTTL is how long a bulletin answer may be reused.
const pfzTTL = 30 * time.Minute

var (
	pfzMu    sync.Mutex
	pfzStore = map[string]cachedZone{}
)

type cachedZone struct {
	z   OfficialZone
	exp time.Time
}

// pfzKey quantises the point so that two users a few hundred metres apart share
// one request. At this scale the rounding is far finer than the accuracy of
// either the bulletin or our own zone, so it costs nothing in correctness.
func pfzKey(lat, lon, radiusKm float64) string {
	return fmt.Sprintf("%.2f,%.2f,%.0f", lat, lon, radiusKm)
}

func pfzCacheGet(lat, lon, radiusKm float64) (OfficialZone, bool) {
	pfzMu.Lock()
	defer pfzMu.Unlock()
	c, ok := pfzStore[pfzKey(lat, lon, radiusKm)]
	if !ok || time.Now().After(c.exp) {
		if ok {
			delete(pfzStore, pfzKey(lat, lon, radiusKm))
		}
		return OfficialZone{}, false
	}
	return c.z, true
}

func pfzCachePut(lat, lon, radiusKm float64, z OfficialZone) {
	pfzMu.Lock()
	defer pfzMu.Unlock()
	pfzStore[pfzKey(lat, lon, radiusKm)] = cachedZone{z: z, exp: time.Now().Add(pfzTTL)}
}

// PFZCacheSize reports how many bulletin answers are currently held. It exists
// for tests and for the health endpoint.
func PFZCacheSize() int {
	pfzMu.Lock()
	defer pfzMu.Unlock()
	return len(pfzStore)
}
