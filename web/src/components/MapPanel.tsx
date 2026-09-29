import { useEffect, useRef } from 'react'
import maplibregl from 'maplibre-gl'
import 'maplibre-gl/dist/maplibre-gl.css'
import type { Geo, PFZ } from '../types'

// A keyless raster style. Esri's Ocean Base is a bathymetric basemap, which is
// the honest backdrop for a marine question: it shows the shelf that makes a
// 45–120 km offset meaningful. OpenStreetMap is overlaid for place labels,
// because the base style carries no names.
const STYLE: maplibregl.StyleSpecification = {
  version: 8,
  sources: {
    ocean: {
      type: 'raster',
      tiles: [
        'https://services.arcgisonline.com/ArcGIS/rest/services/Ocean/World_Ocean_Base/MapServer/tile/{z}/{y}/{x}',
      ],
      tileSize: 256,
      attribution: 'Esri Ocean Base',
    },
    osm: {
      type: 'raster',
      tiles: ['https://tile.openstreetmap.org/{z}/{x}/{y}.png'],
      tileSize: 256,
      attribution: '© OpenStreetMap contributors',
    },
  },
  layers: [
    { id: 'ocean', type: 'raster', source: 'ocean' },
    { id: 'labels', type: 'raster', source: 'osm', paint: { 'raster-opacity': 0.6 } },
  ],
}

function pin(color: string): HTMLElement {
  const el = document.createElement('div')
  el.className = 'pin'
  el.style.background = color
  el.style.boxShadow = `0 0 0 4px ${color}33`
  return el
}

export function MapPanel({ geo, pfz }: { geo: Geo | null; pfz: PFZ | null }) {
  const host = useRef<HTMLDivElement>(null)
  const map = useRef<maplibregl.Map | null>(null)
  const loaded = useRef(false)
  // Held so a new query can clear the previous query's markers; MapLibre has no
  // built-in registry for them.
  const markers = useRef<maplibregl.Marker[]>([])

  useEffect(() => {
    if (!host.current || map.current) return
    const m = new maplibregl.Map({
      container: host.current,
      style: STYLE,
      center: [73.8, 15.5],
      zoom: 5,
      attributionControl: false,
    })
    m.addControl(new maplibregl.NavigationControl({ showCompass: false }), 'top-right')
    m.addControl(new maplibregl.AttributionControl({ compact: true }))
    m.on('load', () => {
      loaded.current = true
    })
    map.current = m
    return () => {
      markers.current.forEach((mk) => mk.remove())
      markers.current = []
      m.remove()
      map.current = null
      loaded.current = false
    }
  }, [])

  useEffect(() => {
    const m = map.current
    if (!m || !loaded.current || !geo) return

    // Redraw overlays from scratch so a new query never inherits old geometry.
    for (const id of ['route', 'zone']) {
      if (m.getLayer(id)) m.removeLayer(id)
      const src = m.getSource(id) as maplibregl.GeoJSONSource | undefined
      if (src) m.removeSource(id)
    }
    markers.current.forEach((mk) => mk.remove())
    markers.current = []

    const coast: [number, number] = [geo.lon, geo.lat]
    const zone: [number, number] = [pfz?.lon ?? geo.way_lon, pfz?.lat ?? geo.way_lat]
    const score = pfz?.score ?? 0

    m.addSource('route', {
      type: 'geojson',
      data: {
        type: 'Feature',
        properties: {},
        geometry: { type: 'LineString', coordinates: [coast, zone] },
      },
    })
    m.addLayer({
      id: 'route',
      type: 'line',
      source: 'route',
      paint: {
        'line-color': '#38bdf8',
        'line-width': 2,
        'line-dasharray': [2, 2],
        'line-opacity': 0.85,
      },
    })

    m.addSource('zone', {
      type: 'geojson',
      data: {
        type: 'Feature',
        properties: { score },
        geometry: { type: 'Point', coordinates: zone },
      },
    })
    m.addLayer({
      id: 'zone',
      type: 'circle',
      source: 'zone',
      // The zone marker encodes the score as its radius, so a weak zone looks
      // weaker instead of only being described as weaker.
      paint: {
        'circle-radius': ['interpolate', ['linear'], ['get', 'score'], 0, 10, 1, 26],
        'circle-color': [
          'interpolate', ['linear'], ['get', 'score'],
          0, '#f59e0b', 0.5, '#38bdf8', 1, '#22c55e',
        ],
        'circle-opacity': 0.35,
        'circle-stroke-color': '#e2e8f0',
        'circle-stroke-width': 1.5,
      },
    })

    markers.current = [
      new maplibregl.Marker({ element: pin('#94a3b8') }).setLngLat(coast).addTo(m),
      new maplibregl.Marker({ element: pin('#22c55e') }).setLngLat(zone).addTo(m),
    ]

    m.fitBounds(new maplibregl.LngLatBounds(coast, zone), {
      padding: 70,
      maxZoom: 8,
      duration: 900,
    })
  }, [geo, pfz])

  return <div ref={host} className="map" role="img" aria-label="Coast and suggested fishing zone" />
}
