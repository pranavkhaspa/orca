package agents

import (
	"context"
	"fmt"
	"sync"

	"orca/internal/data"
	"orca/internal/domain"
	"orca/internal/engine"
)

type marineEntry struct {
	m domain.Marine
	c domain.Citations
}

// oceanAgent reads the ocean state at the offshore waypoint.
func (o *Orchestrator) oceanAgent(ctx context.Context, geo domain.Geo, window int) (domain.Marine, domain.Citations, error) {
	key := fmt.Sprintf("mar:%.3f,%.3f:%d", geo.WayLat, geo.WayLon, window)
	if v, ok := o.cache.Get(key); ok {
		e := v.(marineEntry)
		return e.m, e.c, nil
	}
	m, c, err := data.MarineWindow(ctx, geo.WayLat, geo.WayLon, window)
	if err == nil {
		o.cache.Put(key, marineEntry{m, c})
	}
	return m, c, err
}

// searchOffsets are the bearings, relative to the coast bearing, and radii
// probed around the waypoint when hunting for a productive zone.
var searchOffsets = []struct {
	dBear float64
	dist  float64
}{
	{0, 40}, {-45, 40}, {45, 40},
	{0, 85}, {-45, 85}, {45, 85},
}

// pfzAgent locates the best fishing zone near the waypoint.
//
// The search samples a small fan of points around the waypoint rather than
// reporting conditions at a single arbitrary spot. Each sample is scored by the
// pure engine, so the choice of zone is reproducible and inspectable. Sampling
// is done concurrently and cached, so the cost is a one-off burst against a
// generous free upstream.
func (o *Orchestrator) pfzAgent(ctx context.Context, geo domain.Geo, marine domain.Marine) domain.PFZ {
	type sample struct {
		cell  engine.Cell
		cites domain.Citations
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		results  []sample
		anomaly  float64
		baseline domain.Citation
	)

	// The seasonal baseline is a slow-moving signal. Fetch it in parallel with
	// the search and treat failure as "unknown", never as a query failure.
	wg.Add(1)
	go func() {
		defer wg.Done()
		base, cite, err := data.SSTBaseline(ctx, geo.WayLat, geo.WayLon, 30)
		mu.Lock()
		defer mu.Unlock()
		baseline = cite
		if err == nil && marine.SSTC > 0 {
			anomaly = round1(marine.SSTC - base)
		}
	}()

	// Samples beyond the configured radius are skipped. This is the knob an
	// operator turns when the free upstream is slow: six marine calls per query
	// is a one-off burst, and PFZ_SEARCH_KM=0 keeps the fishing zone pinned to
	// the waypoint if a deployment cannot afford the fan at all.
	maxKm := o.cfg.PFZSearchKm
	for _, off := range searchOffsets {
		off := off
		if maxKm > 0 && off.dist > maxKm {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			lat, lon := data.ProjectWaypoint(geo.Lat, geo.Lon, geo.BearingDeg+off.dBear, off.dist)
			key := fmt.Sprintf("mar:%.3f,%.3f:6", lat, lon)
			var (
				m     domain.Marine
				cites domain.Citations
				ok    bool
			)
			if v, hit := o.cache.Get(key); hit {
				e := v.(marineEntry)
				m, cites, ok = e.m, e.c, true
			} else {
				// A remembered refusal short-circuits before the network call.
				// Without this the search offsets below each retry the same
				// exhausted upstream on every question, so a rate limit sustains
				// itself; the cell simply contributes no sample and the search
				// carries on with the offsets that did resolve.
				//
				// remembered distinguishes a failure we already hold from one
				// just learned. Only a newly learned failure may be written back,
				// because PutFailure starts a fresh staleTTL: re-writing a
				// remembered one would push its expiry forward on every request
				// and turn a three-minute backoff into a permanent refusal that
				// outlives any recovery.
				var err error
				var remembered bool
				if ferr, hit := o.cache.GetFailure(key); hit {
					err, remembered = ferr, true
				} else if m, cites, err = data.MarineWindow(ctx, lat, lon, 6); err == nil {
					o.cache.Put(key, marineEntry{m, cites})
					ok = true
				}
				if err != nil && !remembered {
					o.cache.PutFailure(key, err)
				}
			}
			if !ok {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			results = append(results, sample{
				cell: engine.Cell{
					Lat: lat, Lon: lon, DistanceKm: off.dist, BearingDeg: geo.BearingDeg + off.dBear,
					SSTC: m.SSTC, WaveHeightM: m.WaveHeightM, SwellPerS: m.SwellPeriodS,
				},
				cites: cites,
			})
		}()
	}
	wg.Wait()

	// The waypoint itself is always a candidate: if the fan samples nothing
	// (all upstream failures), we still report where conditions were read.
	// The waypoint candidate carries the main marine citations, because the
	// figures shown for it are the ones already on screen as the sea state.
	results = append(results, sample{
		cell: engine.Cell{
			Lat: geo.WayLat, Lon: geo.WayLon, DistanceKm: geo.DistanceKm, BearingDeg: geo.BearingDeg,
			SSTC: marine.SSTC, WaveHeightM: marine.WaveHeightM, SwellPerS: marine.SwellPeriodS,
		},
		cites: marine.Citations,
	})

	best := domain.PFZ{Lat: geo.WayLat, Lon: geo.WayLon, DistanceKm: geo.DistanceKm, AnomalyC: anomaly}
	bestScore := -1.0
	var bestReason []string
	for _, s := range results {
		if s.cell.SSTC <= 0 {
			continue
		}
		score, band, reasoning := engine.Evaluate(s.cell, anomaly)
		if score > bestScore {
			bestScore = score
			best = domain.PFZ{
				Score: round2(score), Lat: s.cell.Lat, Lon: s.cell.Lon,
				DistanceKm: s.cell.DistanceKm, SSTBand: band, AnomalyC: anomaly,
				SampleSSTC: s.cell.SSTC, SampleWaveHeightM: s.cell.WaveHeightM,
				SampleSwellPerS: s.cell.SwellPerS,
				Citations:       s.cites,
			}
			bestReason = reasoning
		}
	}
	if bestScore < 0 {
		best.Reasoning = []string{
			"No sea-surface temperature was available at any sampled point, so no productive zone could be identified.",
		}
		best.Confidence = "low"
		return best
	}
	best.Confidence = engine.Confidence(best.Score, anomaly)
	best.Reasoning = bestReason
	// Attach the archive baseline so the anomaly, which is displayed, is
	// attributed. A failed baseline contributes an error citation rather than
	// silently disappearing.
	if baseline.Dataset != "" {
		if best.Citations == nil {
			best.Citations = domain.Citations{}
		}
		best.Citations["sst_baseline_30d"] = baseline
	}
	return best
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }
func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
