// Command mksnapshot bakes a dated fallback dataset from live upstreams.
//
// Run it before a demo and commit the result. The service serves this file when
// every upstream is unreachable, so the demo cannot be taken down by a provider
// outage, a rate limit, or a dead ERDDAP host (INSTR.md issue D2).
//
//	go run ./cmd/mksnapshot -out data/snapshot.json -places 14
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"orca/internal/data"
	"orca/internal/domain"
)

func main() {
	out := flag.String("out", "internal/data/"+data.SnapshotsPath, "output path")
	coastal := flag.String("coastal", "internal/data/"+data.CoastalPath, "coastal reference table")
	n := flag.Int("places", 12, "how many places to capture")
	waypoint := flag.Float64("waypoint-km", 45, "offshore distance to sample")
	window := flag.Int("window", 6, "forecast window in hours")
	flag.Parse()

	towns, err := data.Towns(*coastal)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
	// Spread the sample across both coasts rather than clustering, so the
	// nearest-entry fallback in SnapshotFor has coverage everywhere.
	picked := spread(towns, *n)

	snap := data.Snapshot{GeneratedAt: time.Now().UTC(), Places: map[string]data.SnapshotPlace{}}
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	sem := make(chan struct{}, 4) // stay well inside polite request rates

	for _, t := range picked {
		t := t
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			wl, wo := data.ProjectWaypoint(t.Lat, t.Lon, t.BearingDeg, *waypoint)
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()

			m, mc, mErr := data.MarineWindow(ctx, wl, wo, *window)
			w, wc, wErr := data.ForecastWindow(ctx, wl, wo, *window)
			if mErr != nil && wErr != nil {
				fmt.Fprintf(os.Stderr, "  skip %-16s marine: %v / weather: %v\n", t.Name, mErr, wErr)
				return
			}
			if mErr == nil {
				m.Citations = mc
			}
			if wErr == nil {
				w.Citations = wc
			}

			mu.Lock()
			snap.Places[lower(t.Name)] = data.SnapshotPlace{
				Name: t.Name, Lat: t.Lat, Lon: t.Lon, WayLat: wl, WayLon: wo,
				Marine: m, Weather: w,
				MarineProv: mc, WeatherProv: wc,
				Source: "Open-Meteo (baked fallback)",
			}
			mu.Unlock()
			fmt.Fprintf(os.Stderr, "  ok   %-16s waves %.2f m  sst %.1f C  gusts %.0f km/h\n",
				t.Name, m.WaveHeightM, m.SSTC, w.GustKmh)
		}()
	}
	wg.Wait()

	if len(snap.Places) == 0 {
		fmt.Fprintln(os.Stderr, "fatal: every upstream failed; refusing to write an empty snapshot")
		os.Exit(1)
	}

	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s: %d places, generated %s\n", *out, len(snap.Places), snap.GeneratedAt.Format(time.RFC3339))
}

// spread picks places evenly across the table so both coasts are represented.
func spread(towns []data.Town, n int) []data.Town {
	if n > len(towns) {
		n = len(towns)
	}
	step := float64(len(towns)) / float64(n)
	out := make([]data.Town, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, towns[int(float64(i)*step)])
	}
	return out
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

var _ = domain.Marine{}
