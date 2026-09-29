package agents

import (
	"context"
	"time"

	"orca/internal/data"
	"orca/internal/domain"
)

// incoisAgent checks the point against the official INCOIS potential fishing
// zone bulletin, which is published as an OGC Web Feature Service.
//
// The important change from an earlier version of this agent is that it measures
// rather than pings. It used to fetch the INCOIS home page and look for the
// words "pfz", which could only ever answer "is the site up" — never "how far
// is the official advisory from the zone we computed". Reading the bulletin as
// geometry answers the useful question, and produces numbers that carry the
// bulletin as their source.
//
// It remains corroboration only, never a gate. The verdict and the suggested
// zone are computed from observation by architecture.md §2; if this agent
// returns nothing the answer is unchanged, and architecture.md §4 records why
// that has to stay true.
// It returns the raw zone alongside the advisory so the orchestrator can
// re-measure against the computed PFZ point once that exists, rather than
// spending a second round trip waiting for it.
func (o *Orchestrator) incoisAgent(ctx context.Context, geo domain.Geo) (domain.Advisory, data.OfficialZone) {
	if geo.Lat == 0 && geo.Lon == 0 {
		return domain.Advisory{
			Source:    "INCOIS",
			Sector:    sectorFor(geo),
			CheckedAt: time.Now(),
			Status:    "no location resolved, so the official bulletin was not consulted",
		}, data.OfficialZone{}
	}

	// Respect the offline kill switch so the agent degrades the same way it
	// would in a real outage.
	if data.Offline() {
		return domain.Advisory{
			Source:    "INCOIS",
			Sector:    sectorFor(geo),
			CheckedAt: time.Now(),
			Status:    "official bulletin check suppressed in offline mode; zone remains ORCA's own computation",
		}, data.OfficialZone{}
	}

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	// The search is centred on the offshore waypoint, not on the coastal
	// reference point. The waypoint is what the zone is fanned from, so it is
	// the point whose neighbourhood the bulletin has to be searched around.
	lat, lon := geo.WayLat, geo.WayLon
	if lat == 0 && lon == 0 {
		lat, lon = geo.Lat, geo.Lon
	}
	z := data.OfficialPFZ(ctx, lat, lon, data.PFZSearchRadiusKm)
	return incoisAdvisory(geo, z), z
}

// incoisAdvisory renders a fetched zone into the user-facing advisory. It is
// separate from the fetch so the same zone can be rendered again after being
// re-measured against the final PFZ point.
func incoisAdvisory(geo domain.Geo, z data.OfficialZone) domain.Advisory {
	a := domain.Advisory{
		Source:    "INCOIS",
		Sector:    sectorFor(geo),
		CheckedAt: time.Now(),
		Available: z.Checked,
	}
	if !z.Checked {
		a.Status = "official bulletin could not be reached; zone remains ORCA's own computation"
		return a
	}

	a.OfficialFound = z.Found
	a.DistanceKm = z.DistanceKm
	a.BearingDeg = z.BearingDeg
	a.OfficialState = z.State
	a.OfficialLabel = z.Label()
	a.Bulletin = z.Bulletin
	if z.Citation.Dataset != "" {
		a.Citations = domain.Citations{"incois_pfz": z.Citation}
	}

	switch {
	case !z.Found:
		a.Status = "official bulletin checked; no published zone near this coast today"
	case z.DistanceKm < 15:
		a.Status = "our computed zone is close to the officially published zone"
	default:
		a.Status = "official bulletin checked; the published zone is well away from ours"
	}
	return a
}

// incoisAgainstZone re-renders the advisory with the distance taken between the
// officially published line and ORCA's own computed zone.
func incoisAgainstZone(geo domain.Geo, z data.OfficialZone, pfz domain.PFZ) domain.Advisory {
	if z.HasGeometry() && pfz.Lat != 0 && pfz.Lon != 0 {
		z = z.MeasureTo(pfz.Lat, pfz.Lon)
	}
	return incoisAdvisory(geo, z)
}

// sectorFor reports the sea sector for a resolved location, used only to label
// the corroboration attempt.
func sectorFor(geo domain.Geo) string {
	if geo.Lat == 0 && geo.Lon == 0 {
		return "unknown"
	}
	// The Indian coastline runs roughly NW–SE; longitude 80°E separates the
	// Arabian Sea sector from the Bay of Bengal sector well enough for a label.
	if geo.Lon < 79.9 {
		return "Arabian Sea"
	}
	if geo.Lon > 85.5 {
		return "Bay of Bengal (north)"
	}
	return "Bay of Bengal"
}
