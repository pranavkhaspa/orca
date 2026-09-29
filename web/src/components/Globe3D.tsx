import { useEffect, useRef, useState } from 'react'
import Globe, { type GlobeInstance } from 'globe.gl'
import * as THREE from 'three'
import type { Geo, PFZ, VerdictLevel } from '../types'

// A rotating globe, not a slippy map, and the reason is the question this app
// answers. "Is it safe to fish off Kochi?" is a question about a point on a
// sphere, and a 2D chart of the Indian coast hides the two facts that make the
// answer trustworthy: which sea the point is actually in, and that the
// suggested zone is genuinely offshore of it, at a bearing that differs by
// port. The globe makes both obvious before a single number is read.
//
// It carries no basemap texture on purpose. The obvious move is
// globeImageUrl('...blue-marble.jpg'), and on a machine with an ad blocker the
// request never completes and the globe is either blank or absent — and the
// person who reported it cannot tell you why their screen is different from
// yours. The texture here is drawn into a canvas at runtime instead, and the
// coastline is the reference table itself: thirty-two ports plotted as points.
// The shape of India appears because the ports are where they are.

const IDLE_POV = { lat: 14, lng: 80, altitude: 2.35 }
const FOCUS_ALTITUDE = 0.85

const VERDICT_COLOR: Record<VerdictLevel, string> = {
  go: '#34d399',
  caution: '#fbbf24',
  'no-go': '#f87171',
}

const COAST_COLOR = '#5eead4'
const PORT_COLOR = '#38bdf8'

export interface PortDot {
  name: string
  lat: number
  lon: number
  coast: string
}

/**
 * Draws the globe's surface: deep ocean, a soft photic gradient toward the
 * equator, and a graticule every 15 degrees.
 *
 * The graticule is doing real work. Without it a smooth sphere reads as a
 * sphere and gives no sense of scale, so the 45 km between a port and its
 * recommended zone — the single distance the whole product is about — is
 * invisible. A grid makes rotation legible and makes the zoom meaningful.
 */
function oceanTexture(): string {
  const w = 2048
  const h = 1024
  const c = document.createElement('canvas')
  c.width = w
  c.height = h
  const ctx = c.getContext('2d')
  if (!ctx) return ''

  const base = ctx.createLinearGradient(0, 0, 0, h)
  base.addColorStop(0, '#071a2b')
  base.addColorStop(0.5, '#0b2c45')
  base.addColorStop(1, '#071a2b')
  ctx.fillStyle = base
  ctx.fillRect(0, 0, w, h)

  // A faint band of warm water either side of the equator, which is where the
  // PFZ band actually sits. It hints at the thing the scoring is looking for
  // without pretending to be sea-surface temperature.
  const gyre = ctx.createRadialGradient(w / 2, h / 2, 0, w / 2, h / 2, h * 0.62)
  gyre.addColorStop(0, 'rgba(45,212,191,0.10)')
  gyre.addColorStop(1, 'rgba(45,212,191,0)')
  ctx.fillStyle = gyre
  ctx.fillRect(0, 0, w, h)

  ctx.strokeStyle = 'rgba(125,211,252,0.10)'
  ctx.lineWidth = 1
  for (let x = 0; x <= w; x += w / 24) {
    ctx.beginPath()
    ctx.moveTo(x, 0)
    ctx.lineTo(x, h)
    ctx.stroke()
  }
  for (let y = 0; y <= h; y += h / 12) {
    ctx.beginPath()
    ctx.moveTo(0, y)
    ctx.lineTo(w, y)
    ctx.stroke()
  }

  // The equator is the one line worth naming.
  ctx.strokeStyle = 'rgba(94,234,212,0.22)'
  ctx.lineWidth = 2
  ctx.beginPath()
  ctx.moveTo(0, h / 2)
  ctx.lineTo(w, h / 2)
  ctx.stroke()

  return c.toDataURL('image/png')
}

export function Globe3D({
  towns,
  geo,
  pfz,
  level,
  onFail,
}: {
  towns: PortDot[]
  geo: Geo | null
  pfz: PFZ | null
  level: VerdictLevel | null
  onFail?: () => void
}) {
  const host = useRef<HTMLDivElement>(null)
  const globe = useRef<GlobeInstance | null>(null)
  const [ready, setReady] = useState(false)

  useEffect(() => {
    const el = host.current
    if (!el || globe.current) return

    // A failed WebGL context does not always throw — some drivers return null
    // from getContext and hand back a renderer that silently draws nothing. A
    // probe is the only way to tell "no WebGL" apart from "a very dark globe",
    // and the difference matters, because one of them should fall back to the
    // 2D chart and the other should not.
    try {
      const probe = document.createElement('canvas')
      const gl = probe.getContext('webgl2') ?? probe.getContext('webgl')
      if (!gl) {
        onFail?.()
        return
      }
      // Free the probe context immediately; browsers cap live WebGL contexts
      // and an extra one left open can cost the real globe its slot.
      gl.getExtension('WEBGL_lose_context')?.loseContext()
    } catch {
      onFail?.()
      return
    }

    let g: GlobeInstance
    try {
      g = new Globe(el, { animateIn: true })
    } catch {
      // No WebGL, a blocked context, or a driver that refuses one. The parent
      // swaps in the 2D chart rather than showing an empty box.
      onFail?.()
      return
    }

    const material = new THREE.MeshPhongMaterial({
      map: (() => {
        const url = oceanTexture()
        if (!url) return null
        const tex = new THREE.TextureLoader().load(url)
        tex.colorSpace = THREE.SRGBColorSpace
        return tex
      })(),
      color: 0xffffff,
      emissive: 0x06121f,
      emissiveIntensity: 0.7,
      shininess: 11,
      specular: new THREE.Color(0x2a6f8f),
    })

    g.backgroundColor('rgba(0,0,0,0)')
      .globeMaterial(material)
      .showAtmosphere(true)
      .atmosphereColor('#4cc9f0')
      .atmosphereAltitude(0.17)
      .globeOffset?.([0, 0])

    const controls = g.controls()
    controls.autoRotate = true
    controls.autoRotateSpeed = 0.45
    controls.enableZoom = true
    controls.enablePan = false
    controls.minDistance = 130
    controls.maxDistance = 620

    g.pointOfView(IDLE_POV, 0)
    globe.current = g

    // globe.gl sizes itself from the container once, so a container that is
    // hidden or zero-height at construction (a lazy chunk arriving during the
    // first paint, a phone rotating) leaves the canvas stuck at the old size.
    // Observing the element keeps it honest.
    const ro = new ResizeObserver(() => {
      g.width(el.clientWidth)
      g.height(el.clientHeight)
    })
    ro.observe(el)

    let cancelled = false
    // onGlobeReady fires after the surface has been uploaded. Deferring the
    // first data write until then avoids a frame where points float over an
    // empty sphere.
    g.onGlobeReady(() => {
      if (!cancelled) setReady(true)
    })

    return () => {
      cancelled = true
      ro.disconnect()
      globe.current = null
      setReady(false)
      try {
        g._destructor()
      } catch {
        /* already torn down */
      }
      el.replaceChildren()
    }
  }, [onFail])

  // Idle layer: every supported port, drawn small. This is the coastline.
  useEffect(() => {
    const g = globe.current
    if (!g || !ready) return
    if (geo) {
      g.pointsData([])
      return
    }
    g.pointsData(towns)
      .pointLat('lat')
      .pointLng('lon')
      .pointColor(() => PORT_COLOR)
      .pointAltitude(0.008)
      .pointRadius(0.16)
      .pointResolution(12)
      .pointLabel((d) => {
        const t = d as PortDot
        return `<div class="globe-tip"><strong>${t.name}</strong><br/><span>${t.coast}</span></div>`
      })
    g.ringsData([])
    g.arcsData([])
    g.labelsData([])
  }, [towns, ready, geo])

  // Result layer: fly to the resolved coast, then draw what was decided.
  useEffect(() => {
    const g = globe.current
    if (!g || !ready) return

    if (!geo) {
      g.controls().autoRotate = true
      g.pointOfView(IDLE_POV, 1200)
      return
    }

    const color = level ? VERDICT_COLOR[level] : COAST_COLOR
    const zLat = pfz?.lat ?? geo.way_lat
    const zLon = pfz?.lon ?? geo.way_lon
    const hasZone = Boolean(pfz && (pfz.lat !== 0 || pfz.lon !== 0))

    g.controls().autoRotate = false
    g.pointOfView({ lat: geo.lat, lng: geo.lon, altitude: FOCUS_ALTITUDE }, 1600)

    const coast = {
      name: geo.name || 'Requested point',
      lat: geo.lat,
      lon: geo.lon,
      coast: 'Coast',
    }
    const zone = { name: 'Suggested zone', lat: zLat, lon: zLon, coast: 'Offshore' }

    g.pointsData(hasZone ? [coast, zone] : [coast])
      .pointLat('lat')
      .pointLng('lon')
      .pointColor((d) => ((d as PortDot).name === 'Suggested zone' ? color : COAST_COLOR))
      .pointAltitude((d) => ((d as PortDot).name === 'Suggested zone' ? 0.02 : 0.012))
      .pointRadius((d) => ((d as PortDot).name === 'Suggested zone' ? 0.42 : 0.3))
      .pointResolution(20)
      .pointLabel((d) => {
        const t = d as PortDot
        return `<div class="globe-tip"><strong>${t.name}</strong><br/><span>${t.coast}</span></div>`
      })

    g.labelsData([{ ...coast, text: geo.name || '' }])
      .labelLat('lat')
      .labelLng('lon')
      .labelText('text')
      .labelSize(0.62)
      .labelDotRadius(0)
      .labelColor(() => COAST_COLOR)
      .labelAltitude(0.014)
      .labelResolution(2)

    g.arcsData(hasZone ? [{ startLat: geo.lat, startLng: geo.lon, endLat: zLat, endLng: zLon }] : [])
      .arcColor(() => [COAST_COLOR, color])
      .arcStroke(0.55)
      .arcAltitudeAutoScale(0.35)
      .arcDashLength(0.4)
      .arcDashGap(0.18)
      .arcDashAnimateTime(2400)

    // A pulse on the zone. The animation is the cheapest way to say "this is
    // the thing the answer is about" without a legend.
    g.ringsData(hasZone ? [{ lat: zLat, lng: zLon }] : [])
      .ringLat('lat')
      .ringLng('lng')
      .ringColor(() => (t: number) => {
        const c = new THREE.Color(color)
        return `rgba(${Math.round(c.r * 255)},${Math.round(c.g * 255)},${Math.round(c.b * 255)},${1 - t})`
      })
      .ringMaxRadius(3.4)
      .ringPropagationSpeed(1.1)
      .ringRepeatPeriod(1400)
  }, [geo, pfz, level, ready])

  return <div ref={host} className="globe-host" aria-hidden="true" />
}
