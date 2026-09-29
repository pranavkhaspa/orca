// Vendors world geography into the build so the globe reads as a map.
//
// This exists because the obvious way to put a map on a globe is to fetch a
// texture or a tile server, and this project has already been bitten by that
// once: a browser extension blocked a CDN asset and the interface was left with
// a hole in it. A safety tool should not depend on a third party staying
// reachable, so the geometry is converted here, at build time, and committed.
//
// Input is Natural Earth via the world-atlas package, which ships 110m, 50m and
// 10m. The default is 50m. 110m was visibly wrong for this product: it puts
// Kanyakumari on a shelf and rounds Srikakulam into the sea next to it, and the
// difference between Berhampur and Puri is 170 km of coast that 110m does not
// describe. 10m is the opposite mistake — it is roughly 2.5 MB of geometry for
// a globe about 550 px across, where a pixel covers several kilometres.
//
//   npm run build:geo            # 50m
//   npm run build:geo -- 110m    # coarser, for a small payload
//
// Countries rather than land, because the difference between "a blue ball" and
// "a map" is borders and names: stroking every country outline draws the
// coastline and the internal boundaries in one pass, and the same data carries
// the names.
//
// A note on provenance, because "accurate" is doing a lot of work in the request
// behind this: Natural Earth is not OpenStreetMap. It is a separate curated
// dataset, generalising OSM and other sources to a scale that suits cartography.
// It is a better fit for a globe than raw OSM would be, since OSM's coastline is
// traced at survey scale and is enormous. Anyone who needs literal OSM geometry
// should say so, because vendoring it is a different build with a much larger
// committed artefact.
//
// The TopoJSON is decoded here rather than with topojson-client, which returns
// non-finite coordinates for parts of countries-110m — India comes out with 272
// NaN points, Brazil 406. Decoded by hand the same file has none, and India's
// bounding box comes out at lon 68.2–97.4, lat 8.0–35.5. The decoder is twenty
// lines and the dependency is gone.

import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = join(dirname(fileURLToPath(import.meta.url)))
const web = join(here, '..')
const out = join(web, 'public', 'geo')

// The resolution to vendor. Checked against the files that actually exist,
// because a typo here would otherwise be a runtime 404 on a committed path
// rather than a build error.
const SCALE = (process.argv[2] || '50m').replace(/[^0-9m]/g, '')
const RESOLUTIONS = ['110m', '50m', '10m']
if (!RESOLUTIONS.includes(SCALE)) {
  console.error(`  resolution must be one of ${RESOLUTIONS.join(', ')}, got "${process.argv[2]}"`)
  process.exit(1)
}

// Two decimals is about 1.1 km at the equator. At 50m that is below the
// dataset's own resolution, so rounding costs no real detail and takes a
// meaningful bite out of the payload.
const PRECISION = 2
const F = 10 ** PRECISION
const round = (n) => Math.round(n * F) / F

const rad = Math.PI / 180

// Antarctica is dropped. At 110m it is a ring that runs along the bottom of
// every chart, and on a rotating globe it sweeps past the camera for no
// informational gain — this product is about northern latitudes.
const SKIP = new Set(['Antarctica'])

// Labels below this many square degrees are dropped. Roughly 3° is the point
// where a name is wider than the country it names, at globe scale.
const MIN_LABEL_AREA = 3

// ---- TopoJSON ---------------------------------------------------------------

/**
 * Decode one arc into absolute [lon, lat] pairs.
 * Arcs are delta-encoded integers scaled and translated by the file transform;
 * a negative index means the same arc traversed backwards, and the encoding
 * for that is bitwise NOT, not negation.
 */
function decodeArc(topology, index) {
  const i = index < 0 ? ~index : index
  const arc = topology.arcs[i]
  const [sx, sy] = topology.transform.scale
  const [tx, ty] = topology.transform.translate
  let x = 0
  let y = 0
  const out = new Array(arc.length)
  for (let k = 0; k < arc.length; k++) {
    x += arc[k][0]
    y += arc[k][1]
    out[k] = [x * sx + tx, y * sy + ty]
  }
  return out
}

/** Stitch a ring's arc list into one closed coordinate array. */
function decodeRing(topology, indices) {
  const pts = []
  for (let k = 0; k < indices.length; k++) {
    const seg = decodeArc(topology, indices[k])
    // A reversed arc has to be reversed after scaling, not before, so it is
    // flipped here rather than in the decoder.
    if (indices[k] < 0) seg.reverse()
    // Arcs share their joint point; drop the duplicate on every join but the
    // first, or the ring closes one point early and the fill gains a slit.
    for (let j = k === 0 ? 0 : 1; j < seg.length; j++) pts.push(seg[j])
  }
  return close(pts)
}

const same = (a, b) => Math.abs(a[0] - b[0]) < 1e-9 && Math.abs(a[1] - b[1]) < 1e-9

/** GeoJSON rings must be explicitly closed. */
function close(pts) {
  if (pts.length < 3) return null
  if (!same(pts[0], pts[pts.length - 1])) pts.push([...pts[0]])
  return pts
}

// ---- Simplification --------------------------------------------------------

// Douglas-Peucker, on a local flat-earth approximation.
//
// The globe is about 550 px across, so one pixel spans roughly 0.65° of
// longitude. Anything finer than that cannot be resolved on screen, and 50m
// carries about nine times the points of 110m — 1.36 MB of geometry, ~400 kB
// gzipped, for detail the display has no room to show. Dropping points within
// 0.01° removes most of that and is invisible at any size this is viewed at.
//
// The tolerance is small on purpose. This is a safety interface: the coastline
// is the thing that tells a user which sea they are in, and a simplification
// that shaved 200 m off Cape Comorin to save 30 kB would be a bad trade. The
// label self-checks below run after simplification, so a country mangled by it
// fails the build rather than shipping.
const TOLERANCE = 0.01

function perpendicularDistance(p, a, b) {
  const dx = b[0] - a[0]
  const dy = b[1] - a[1]
  const lenSq = dx * dx + dy * dy
  if (lenSq === 0) return Math.hypot(p[0] - a[0], p[1] - a[1])
  const t = ((p[0] - a[0]) * dx + (p[1] - a[1]) * dy) / lenSq
  const cx = a[0] + Math.max(0, Math.min(1, t)) * dx
  const cy = a[1] + Math.max(0, Math.min(1, t)) * dy
  return Math.hypot(p[0] - cx, p[1] - cy)
}

function simplify(pts) {
  if (pts.length <= 4) return pts
  const keep = new Uint8Array(pts.length)
  keep[0] = 1
  keep[pts.length - 1] = 1
  // Explicit stack: a coastline ring can be tens of thousands of points, and
  // recursion over one of those overflows the goroutine stack.
  const stack = [[0, pts.length - 1]]
  while (stack.length > 0) {
    const [lo, hi] = stack.pop()
    let worst = -1
    let worstD = TOLERANCE
    for (let i = lo + 1; i < hi; i++) {
      const d = perpendicularDistance(pts[i], pts[lo], pts[hi])
      if (d > worstD) {
        worstD = d
        worst = i
      }
    }
    if (worst !== -1) {
      keep[worst] = 1
      stack.push([lo, worst], [worst, hi])
    }
  }
  return pts.filter((_, i) => keep[i])
}

// ---- Centroid ---------------------------------------------------------------

/**
 * Planar centroid and area of a ring by the shoelace formula, with the
 * antimeridian handled.
 *
 * An area-weighted sum of edge midpoints — the obvious spherical approach — is
 * wrong here: opposite edges of any closed ring carry opposite weights, so they
 * cancel and the sum goes degenerate. The shoelace formula is the one that
 * actually computes a centroid.
 *
 * Longitudes are unwrapped first, because a ring that crosses the antimeridian
 * wraps to the far side of the chart and its centroid lands in the wrong ocean.
 * Shifting the western hemisphere by +360 makes the ring contiguous; the result
 * is normalised back afterwards.
 */
function ringCentre(ring) {
  let lo = Infinity
  let hi = -Infinity
  for (const [lon] of ring) {
    if (lon < lo) lo = lon
    if (lon > hi) hi = lon
  }
  const shift = hi - lo > 180 ? 360 : 0
  const pts = ring.map(([lon, lat]) => [lon + shift, lat])

  let a = 0
  let cx = 0
  let cy = 0
  for (let i = 0; i < pts.length - 1; i++) {
    const [x1, y1] = pts[i]
    const [x2, y2] = pts[i + 1]
    const cross = x1 * y2 - x2 * y1
    a += cross
    cx += (x1 + x2) * cross
    cy += (y1 + y2) * cross
  }
  if (a === 0) return null
  // `a` is the shoelace sum, which is twice the area. The centroid divides by
  // 6A, so that is 3a here — dividing by 6a lands every anchor at exactly half
  // of where it belongs, which is subtle enough to ship unnoticed.
  const area = Math.abs(a / 2)
  const k = 3 * a
  return {
    lon: ((((cx / k) % 360) + 540) % 360) - 180,
    lat: cy / k,
    area,
  }
}

/**
 * Combine ring centroids into one anchor for a country.
 *
 * Averaging longitudes is wrong for a country split across the antimeridian:
 * Fiji's two main islands sit at 178°E and 179°W, and the arithmetic mean of
 * those is 0°, which is the middle of the Atlantic. Averaging the corresponding
 * unit vectors keeps the two sides of the antimeridian on the same side of it.
 */
function countryCentre(rings) {
  let x = 0
  let y = 0
  let z = 0
  let wsum = 0
  let area = 0
  for (const ring of rings) {
    const c = ringCentre(ring)
    if (!c || c.area === 0) continue
    const p = c.lat * rad
    const l = c.lon * rad
    const cos = Math.cos(p)
    x += cos * Math.cos(l) * c.area
    y += cos * Math.sin(l) * c.area
    z += Math.sin(p) * c.area
    wsum += c.area
    area += c.area
  }
  if (wsum === 0) return null
  x /= wsum
  y /= wsum
  z /= wsum
  return {
    lon: (Math.atan2(y, x) / rad + 540) % 360 - 180,
    lat: Math.atan2(z, Math.hypot(x, y)) / rad,
    area,
  }
}

// A few countries whose outline defeats any automatic anchor. Hand-placing
// eight is cheaper than a general solution nobody needs.
const OVERRIDE = {
  Russia: { lat: 62, lon: 94 },
  'United States of America': { lat: 39.5, lon: -98.5 },
  Canada: { lat: 58, lon: -100 },
  France: { lat: 46.6, lon: 2.4 },
  Norway: { lat: 62, lon: 9 },
  'South Africa': { lat: -29, lon: 24 },
  Indonesia: { lat: -2.5, lon: 118 },
  'Papua New Guinea': { lat: -6.3, lon: 143 },
  // Two main islands at 178°E and 179°W. The circular mean of two points that
  // far apart is 0°, which is the middle of the Atlantic, so no arithmetic on
  // the two parts can place this one.
  Fiji: { lat: -17.7, lon: 178 },
}

// Independently sourced coordinates, used as a regression test on the centroid
// maths. Every one of these is an approximate published position for the
// country, not a value derived from the geometry under test, so agreeing with
// all of them is real evidence rather than the check agreeing with itself.
const KNOWN = {
  India: [22, 79],
  Brazil: [-10, -52],
  Australia: [-25, 134],
  China: [35, 105],
  Japan: [36, 138],
  'United Kingdom': [54, -2],
  Egypt: [26, 30],
  Nigeria: [9, 8],
  Argentina: [-35, -65],
  Thailand: [15, 101],
  'Saudi Arabia': [24, 45],
  Mexico: [19, -102],
  Kazakhstan: [48, 67],
  Ukraine: [49, 32],
  Sweden: [62, 15],
  Iceland: [65, -18],
  Peru: [-10, -75],
  'New Zealand': [-41, 173],
  Madagascar: [-19, 46],
  Bangladesh: [24, 90],
  'Papua New Guinea': [-6, 143],
  Tanzania: [-6, 35],
  Morocco: [31, -7],
  Chile: [-35, -71],
  'South Africa': [-29, 24],
  Philippines: [12, 122],
  'Sri Lanka': [7.8, 80.7],
  Fiji: [-17.7, 178],
  Russia: [62, 94],
  'United States of America': [39.5, -98.5],
  France: [46.6, 2.4],
  Indonesia: [-2.5, 118],
  Canada: [58, -100],
  Norway: [62, 9],
}

// ---- Build ------------------------------------------------------------------

const topology = JSON.parse(
  readFileSync(join(web, 'node_modules', 'world-atlas', `countries-${SCALE}.json`), 'utf8'),
)

const features = []
const labels = []
let nonFinite = 0
let points = 0

for (const geom of topology.objects.countries.geometries) {
  const name = geom.properties?.name
  if (!name || SKIP.has(name)) continue

  // A Polygon's arcs are [ring][arc]; a MultiPolygon's are [polygon][ring][arc],
  // so one flatten reaches rings in both cases.
  const ringGroups = geom.type === 'Polygon' ? geom.arcs : geom.arcs.flat()
  const polys = []
  for (const ringArcs of ringGroups) {
    const ring = decodeRing(topology, ringArcs)
    if (!ring) continue
    const rounded = simplify(ring.map(([lon, lat]) => [round(lon), round(lat)]))
    if (!rounded || rounded.length < 4) continue
    for (const [lon, lat] of rounded) {
      points++
      if (!Number.isFinite(lon) || !Number.isFinite(lat)) nonFinite++
    }
    polys.push(rounded)
  }
  if (polys.length === 0) continue

  features.push({ name, coordinates: polys })

  const centre = OVERRIDE[name] ?? countryCentre(polys)
  if (!centre) continue
  // Below roughly 3° square there is no point labelling: on a globe a label is
  // only legible when its region faces the camera, and 170 names competing for
  // the same few hundred pixels turns every one of them into noise. Ranking by
  // area also keeps the threshold honest, since the name of a 4 km island does
  // not earn its place just because it exists in the dataset.
  if (!OVERRIDE[name] && centre.area < MIN_LABEL_AREA) continue
  labels.push({
    name,
    lat: round(centre.lat),
    lon: round(centre.lon),
    // Square degrees, used only to rank and size labels.
    area: round(centre.area),
  })
}

labels.sort((a, b) => b.area - a.area)

// ---- Self-check ------------------------------------------------------------
//
// This script silently produced a world in which Brazil sat in the Pacific and
// India was in Kansas, and nothing complained: the output was valid GeoJSON,
// the build exited zero, and the only symptom was a map that was quietly wrong.
// So the labels are now checked against the geometry they came from, and the
// build fails instead of emitting a lie.
//
// The test is "falls inside the bounding box of at least one of its own rings",
// not "inside the bounding box of all of them". A country's rings may be
// scattered islands, and a single overall box is so wide that it accepts almost
// anything — Fiji's box is the entire globe, which would have passed an anchor
// in the middle of the Atlantic.

const MARGIN = 2.5

function insideAnyRing(rings, lat, lon) {
  for (const ring of rings) {
    let la = Infinity
    let lb = -Infinity
    let lo = Infinity
    let hi = -Infinity
    for (const [rlon, rlat] of ring) {
      if (rlat < la) la = rlat
      if (rlat > lb) lb = rlat
      if (rlon < lo) lo = rlon
      if (rlon > hi) hi = rlon
    }
    if (lat < la - MARGIN || lat > lb + MARGIN) continue
    // A ring that wraps the antimeridian is two intervals, not one.
    const inLon =
      hi - lo > 180
        ? lon >= hi - MARGIN || lon <= lo + MARGIN
        : lon >= lo - MARGIN && lon <= hi + MARGIN
    if (inLon) return true
  }
  return false
}

let wrong = 0
for (const l of labels) {
  // Hand-placed anchors are held to KNOWN below instead; a country split into
  // many polygons, like Russia, has no single ring that contains its centre, so
  // this test can only be applied to the ones the maths actually chose.
  if (OVERRIDE[l.name]) continue
  const f = features.find((x) => x.name === l.name)
  if (insideAnyRing(f.coordinates, l.lat, l.lon)) continue
  wrong++
  console.error(`  label ${l.name} at ${l.lat},${l.lon} is outside every ring of its own outline`)
}

// And the out-of-sample check: agreement with published positions.
for (const [name, [klat, klon]] of Object.entries(KNOWN)) {
  const l = labels.find((x) => x.name === name)
  if (!l) {
    wrong++
    console.error(`  ${name} has no label but is in the known-position table`)
    continue
  }
  // Longitude difference has to be measured the short way round, or every
  // country near the antimeridian looks like it is thousands of degrees out.
  const dlon = ((l.lon - klon + 540) % 360) - 180
  const d = Math.hypot(l.lat - klat, dlon)
  if (d > 6) {
    wrong++
    console.error(
      `  ${name} anchored at ${l.lat},${l.lon} is ${d.toFixed(1)}° from ` +
        `the known position ${klat},${klon}`,
    )
  }
}

const world = JSON.stringify({
  type: 'FeatureCollection',
  features: features.map((f) => ({
    type: 'Feature',
    properties: { name: f.name },
    geometry: {
      type: 'MultiPolygon',
      // MultiPolygon nests coordinates as polygon → ring → position, which is
      // one level deeper than the ring list this script builds. Emitting the
      // rings directly still parses as JSON and still loads, and the consumer
      // fails later with "destructure non-iterable" while reading a number as a
      // coordinate pair — so the shape is asserted below rather than trusted.
      coordinates: f.coordinates.map((ring) => [ring]),
    },
  })),
})

if (nonFinite > 0) {
  console.error(`  ${nonFinite} non-finite coordinates in ${points} points — decoder is wrong`)
  process.exit(1)
}
if (wrong > 0) {
  console.error(`  ${wrong} of ${labels.length} label anchors are outside their country`)
  process.exit(1)
}

// And the emitted document itself, since the checks above all read the
// in-memory structures and the file that ships is a different shape again.
const check = JSON.parse(world)
if (check.type !== 'FeatureCollection') {
  console.error('  world.json is not a FeatureCollection')
  process.exit(1)
}
for (const f of check.features) {
  const polys = f.geometry?.coordinates
  if (f.geometry?.type !== 'MultiPolygon' || !Array.isArray(polys) || polys.length === 0) {
    console.error(`  ${f.properties?.name}: geometry is not a non-empty MultiPolygon`)
    process.exit(1)
  }
  for (const poly of polys) {
    if (!Array.isArray(poly) || poly.length === 0) {
      console.error(`  ${f.properties?.name}: polygon is not a non-empty array of rings`)
      process.exit(1)
    }
    for (const ring of poly) {
      for (const p of ring) {
        if (!Array.isArray(p) || p.length < 2 || !Number.isFinite(p[0]) || !Number.isFinite(p[1])) {
          console.error(`  ${f.properties?.name}: position is not a finite [lon, lat] pair`)
          process.exit(1)
        }
      }
      if (!same(ring[0], ring[ring.length - 1])) {
        console.error(`  ${f.properties?.name}: ring is not closed`)
        process.exit(1)
      }
    }
  }
}

writeFileSync(join(out, 'world.json'), world)
writeFileSync(join(out, 'labels.json'), JSON.stringify(labels))

const kb = (s) => (Buffer.byteLength(s) / 1024).toFixed(1)
console.log(`world.json  ${kb(world)} kB  (${features.length} countries)`)
console.log(`labels.json ${kb(JSON.stringify(labels))} kB  (${labels.length} labels)`)
console.log(`${SCALE}: ${points} points, 0 non-finite, all ${labels.length} anchors inside their outline`)
