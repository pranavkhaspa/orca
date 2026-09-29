// Vendors world geography into the build so the globe has real coastlines.
//
// This exists because the obvious way to put a map on a globe is to fetch a
// texture or a tile server, and this project has already been bitten by that
// once: a browser extension blocked a CDN asset and the interface was left with
// a hole in it. A safety tool should not depend on a third party staying
// reachable, so the geometry is converted here, at build time, and committed.
//
// Input is Natural Earth 110m land via the world-atlas package (public domain),
// which is the right resolution for a globe: coarse enough to stay small,
// detailed enough to read as coastlines rather than as blobs.
//
//   node scripts/build-land.mjs

import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { feature } from 'topojson-client'

const here = dirname(fileURLToPath(import.meta.url))
const web = join(here, '..')
const out = join(web, 'public', 'geo')

// Two decimals is about 1.1 km at the equator. That is far more precision than
// a 110m coastline can honestly claim, and it roughly halves the payload
// against the source's full float precision.
const PRECISION = 2
const f = PRECISION

const round = (n) => Math.round(n * 10 ** f) / 10 ** f

function roundCoords(coords) {
  if (typeof coords[0] === 'number') return [round(coords[0]), round(coords[1])]
  return coords.map(roundCoords)
}

const src = JSON.parse(
  readFileSync(join(web, 'node_modules', 'world-atlas', 'land-110m.json'), 'utf8'),
)
const land = feature(src, src.objects.land)

// world-atlas stores land as a GeometryCollection wrapping one MultiPolygon,
// which topojson-client hands back as a FeatureCollection. Flatten whatever
// comes out into a single list of polygon coordinate arrays.
const polys =
  land.type === 'FeatureCollection'
    ? land.features.flatMap((f) =>
        f.geometry.type === 'Polygon' ? [f.geometry.coordinates] : f.geometry.coordinates,
      )
    : land.geometry.coordinates

mkdirSync(out, { recursive: true })

// GeoJSON MultiPolygon, so globe.gl can fill it and stroke it in one pass.
const multi = {
  type: 'Feature',
  properties: { name: 'land' },
  geometry: {
    type: 'MultiPolygon',
    coordinates: polys.map(roundCoords),
  },
}
const geojson = JSON.stringify(multi)
writeFileSync(join(out, 'land.json'), geojson)

// A coastline-only version as MultiLineString, for a stroke that can be drawn
// at a different colour and altitude from the fill. Antarctica is dropped: at
// 110m it is a ring that runs along the bottom of every map, and on a rotating
// globe it sweeps past the camera for no informational gain.
const lines = []
for (const poly of multi.geometry.coordinates) {
  for (const ring of poly) {
    if (ring.length < 4) continue
    const mid = ring.reduce((a, p) => a + p[1], 0) / ring.length
    if (mid < -55) continue // Antarctica
    lines.push(ring)
  }
}
const coast = JSON.stringify({
  type: 'Feature',
  properties: { name: 'coastline' },
  geometry: { type: 'MultiLineString', coordinates: lines },
})
writeFileSync(join(out, 'coastline.json'), coast)

const kb = (s) => (Buffer.byteLength(s) / 1024).toFixed(1)
console.log(`land.json      ${kb(geojson)} kB  (${multi.geometry.coordinates.length} polygons)`)
console.log(`coastline.json ${kb(coast)} kB  (${lines.length} rings)`)
