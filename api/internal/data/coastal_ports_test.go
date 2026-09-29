package data

import (
	"fmt"
	"testing"
)

func TestNewPortsProjectSeaward(t *testing.T) {
	places := []struct {
		name string
		lat  float64
		lon  float64
		brg  float64
	}{
		{"Srikakulam", 18.298, 83.896, 100},
		{"Vizianagaram", 18.113, 83.606, 100},
		{"Gopalpur", 19.009, 84.608, 105},
		{"Berhampur", 19.315, 84.794, 105},
	}
	for _, p := range places {
		// 45 km is the base offshore distance the orchestrator projects to.
		wlat, wlon := ProjectWaypoint(p.lat, p.lon, p.brg, 45)
		dLatKm := (wlat - p.lat) * 111.32
		dLonKm := (wlon - p.lon) * 111.32 * 0.93 // cos(19N) ish
		// The waypoint must be offshore, i.e. east/south-east of the town on
		// this coast, not back across dry land.
		if dLonKm <= 0 {
			t.Errorf("%s: waypoint is not offshore (dLon %.1f km)", p.name, dLonKm)
		}
		dist := haversineKm(p.lat, p.lon, wlat, wlon)
		if dist < 30 || dist > 60 {
			t.Errorf("%s: waypoint %.1f km away, want 30-60", p.name, dist)
		}
		fmt.Printf("%-12s town %.3f,%.3f -> waypoint %.3f,%.3f (%.1f km, dLon %+.1f, dLat %+.1f)\n",
			p.name, p.lat, p.lon, wlat, wlon, dist, dLonKm, dLatKm)
	}
}

func TestNewPortsMatchByEveryName(t *testing.T) {
	cases := map[string]string{
		"srikakulam":   "Srikakulam",
		"vizianagaram": "Vizianagaram",
		"vzm":          "Vizianagaram",
		"gopalpur":     "Gopalpur",
		"gopal pur":    "Gopalpur",
		"berhampur":    "Berhampur",
		"brahmapur":    "Berhampur",
		"vizag":        "Visakhapatnam",
		"puri":         "Puri",
	}
	for in, want := range cases {
		tn, ok := MatchTown(in, coastal)
		if !ok {
			t.Errorf("%q did not match, want %s", in, want)
			continue
		}
		if tn.Name != want {
			t.Errorf("%q matched %s, want %s", in, tn.Name, want)
		}
	}
}
