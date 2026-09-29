// Sampled-pixel contrast check for the navigation bar.
//
//   WEB=http://127.0.0.1:5174 node scripts/check-navbar-contrast.mjs
//
// This reads actual rendered pixels rather than computed styles. That distinction
// matters here: the active nav link is dark ink on a cyan *gradient*, so its
// background-color is transparent and any checker that only reads
// backgroundColor concludes the text is dark-on-dark and fails it. Reading the
// framebuffer cannot be fooled that way, and it also catches the case where a
// backdrop-filter makes the effective backdrop something CSS does not describe.
//
// The method: for each element, sample a grid of pixels across its box, then take
// the brightest and darkest clusters. Those are treated as the worst-case
// background for the text's own colour, and the lower of the two ratios is
// reported. Text is assumed to be the minority cluster.
import { spawn } from 'node:child_process'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { PNG } from './png.mjs'

const WEB = process.env.WEB ?? 'http://127.0.0.1:5174'
const CHROME = process.env.CHROME ?? '/usr/bin/google-chrome'
const profile = mkdtempSync(join(tmpdir(), 'navct-'))
const chrome = spawn(CHROME, [
  '--headless=new', '--remote-debugging-port=9388', `--user-data-dir=${profile}`,
  '--no-sandbox', '--disable-gpu', '--hide-scrollbars', '--force-device-scale-factor=1',
  '--window-size=1440,900', 'about:blank',
], { stdio: 'ignore' })
const cleanup = () => { try { chrome.kill('SIGKILL') } catch {} ; rmSync(profile, { recursive: true, force: true }) }
process.on('exit', cleanup)

let wsUrl, ws, id = 0
const pending = new Map()
const send = (m, p = {}, s) => new Promise((res, rej) => {
  const msg = { id: ++id, method: m, params: p }
  if (s) msg.sessionId = s
  pending.set(msg.id, { res, rej })
  ws.send(JSON.stringify(msg))
})
for (let i = 0; i < 60; i++) {
  try { const r = await fetch('http://127.0.0.1:9388/json/version'); wsUrl = (await r.json()).webSocketDebuggerUrl; if (wsUrl) break } catch {}
  await new Promise((r) => setTimeout(r, 250))
}
ws = new WebSocket(wsUrl)
await new Promise((r) => (ws.onopen = r))
ws.onmessage = (e) => {
  const m = JSON.parse(e.data)
  if (m.id && pending.has(m.id)) {
    const { res, rej } = pending.get(m.id); pending.delete(m.id)
    m.error ? rej(new Error(JSON.stringify(m.error))) : res(m.result)
  }
}

const { targetId } = await send('Target.createTarget', { url: 'about:blank' })
const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true })
await send('Page.enable', {}, sessionId)
await send('Runtime.enable', {}, sessionId)
const evalJs = async (expression) => {
  const r = await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }, sessionId)
  if (r.exceptionDetails) throw new Error(r.exceptionDetails.exception?.description ?? 'eval failed')
  return r.result.value
}

await send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false }, sessionId)
await send('Page.navigate', { url: WEB }, sessionId)
await new Promise((r) => setTimeout(r, 2600))

// Dismiss the first-visit tutorial before measuring anything.
//
// This is the single most important line in this file. The tour opens over a
// dimming backdrop on a first visit, and a backdrop is a full-viewport overlay
// that composites over the whole page — so every element in the bar measures as
// a blend of its real colour and the scrim, and the cyan accent reads as a dark
// teal. The first version of this check reported the active nav pill as failing
// contrast at 1.08:1 when it is in fact dark ink on cyan at 10.5:1. Without this
// the harness produces confident, specific, entirely wrong results.
const dismissed = await evalJs(`(() => {
  const backdrop = document.querySelector('.tour-backdrop')
  if (!backdrop) return 'no tour'
  for (const sel of ['.tour-close', '[aria-label]', 'button']) {
    const b = document.querySelector(sel)
    if (b && /close|dismiss|×|✕/i.test(b.getAttribute('aria-label') || b.className || b.textContent || '')) {
      b.click()
      return 'closed via ' + sel
    }
  }
  backdrop.click()
  return 'backdrop clicked'
})()`)
if (dismissed !== 'no tour') {
  await new Promise((r) => setTimeout(r, 600))
  const still = await evalJs(`!!document.querySelector('.tour-backdrop')`)
  if (still) throw new Error('could not dismiss the tutorial; it dims every sampled pixel')
  console.log(`  (${dismissed})`)
}
// The rail only exists once there is a result.
const asked = await evalJs(`(async () => {
  const i = document.querySelector('#q')
  if (!i) return 'no input'
  const s = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set
  s.call(i, 'Is it safe to fish off Kochi tomorrow morning?')
  i.dispatchEvent(new Event('input', { bubbles: true }))
  i.closest('form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  for (let k = 0; k < 90; k++) { if (document.querySelector('#rail') || document.querySelector('.rail-link')) return 'ok'; await new Promise((r) => setTimeout(r, 400)) }
  return 'timeout'
})()`)
if (asked !== 'ok') { console.log('no result, rail not rendered:', asked); process.exit(1) }
await new Promise((r) => setTimeout(r, 1200))

// Boxes of the text elements, plus their declared colour, all in one round trip.
const targets = await evalJs(`(() => {
  // Deliberately absent: a closed <select>, and the .lang-row wrapper.
  //
  // The <select>'s visible face is painted by the operating system's widget, not
  // by this stylesheet, so its on-screen colour is a property of the platform.
  // The wrapper is dropped for the same reason: its box is mostly occupied by
  // that widget, so the most common pixel in it is the widget's own text, and
  // comparing our declared colour against the widget's foreground is meaningless.
  // The language control's own surface and the globe icon are checked instead.
  const sel = ['.wordmark', '.tagline', '.brand-tag', '.health', '.rail-link:not([aria-current])',
               '.rail-link[aria-current="true"]', '.rail-link:not([aria-current]) .rail-ico',
               '.rail-link[aria-current="true"] .rail-ico', '.tour-btn', '.lang-ico']
  return sel.map((s) => {
    const el = document.querySelector(s)
    if (!el) return { sel: s, missing: true }
    const b = el.getBoundingClientRect()
    const cs = getComputedStyle(el)
    const m = cs.color.match(/[\\d.]+/g).map(Number)
    return {
      sel: s,
      x: Math.round(b.x), y: Math.round(b.y), w: Math.round(b.width), h: Math.round(b.height),
      color: [m[0], m[1], m[2]],
      size: parseFloat(cs.fontSize),
      weight: Number(cs.fontWeight) || 400,
    }
  })
})()`)

const { data } = await send('Page.captureScreenshot', { format: 'png' }, sessionId)
const png = PNG.decode(Buffer.from(data, 'base64'))
if (process.env.DUMP) writeFileSync(process.env.DUMP, Buffer.from(data, 'base64'))

const lin = (c) => { c /= 255; return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4) }
const lum = ([r, g, b]) => 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
const ratio = (a, b) => { const l1 = lum(a), l2 = lum(b); return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05) }

// The most common exact pixel colour in the box.
//
// Antialiased text has no clean luminance split, so a sorted-clustering
// approach puts the boundary in the middle of the background and reports
// confident nonsense. The background wins a frequency count by a wide margin
// for any box larger than its text, which is the same reasoning a person uses
// looking at the element.
//
// Quantised to 4 bits per channel: the bar background is a gradient, so exact
// matching would name a different "mode" on every row. A Map keyed by a packed
// integer is also markedly faster than string keys over tens of thousands of
// pixels.
function sample(t) {
  const counts = new Map()
  let total = 0
  for (let y = t.y; y < Math.min(t.y + t.h, png.height); y++) {
    for (let x = t.x; x < Math.min(t.x + t.w, png.width); x++) {
      // Stride comes from the decoded image, not a hardcoded 4. Chrome omits
      // the alpha channel for an opaque screenshot, and assuming RGBA reads the
      // red channel of one pixel as the red, green and blue of the next, which
      // looks like a plausible-but-wrong background rather than an error.
      const i = (y * png.width + x) * png.channels
      const r = png.data[i] >> 4, g = png.data[i + 1] >> 4, b = png.data[i + 2] >> 4
      // r and b occupy the high nibbles, so they must be masked back out
      // separately — shifting them straight into place would let one channel
      // overwrite the other.
      const key = (r << 8) | (g << 4) | b
      counts.set(key, (counts.get(key) ?? 0) + 1)
      total++
    }
  }
  if (total === 0) return null
  let bestKey = -1, bestN = 0
  for (const [k, n] of counts) if (n > bestN) { bestN = n; bestKey = k }
  const bgRGB = [((bestKey >> 8) & 0xf) * 17, ((bestKey >> 4) & 0xf) * 17, (bestKey & 0xf) * 17]
  return { bgRGB, share: bestN / total, n: total }
}

let failures = 0
for (const t of targets) {
  if (t.missing) { console.log(`  MISSING  ${t.sel}`); failures++; continue }
  const s = sample(t)
  if (!s) { console.log(`  FAIL     ${t.sel} — no pixels`); failures++; continue }
  const large = t.size >= 24 || (t.size >= 18.66 && t.weight >= 700)
  const need = large ? 3 : 4.5
  // Worst case: the declared text colour against the sampled background.
  const r = ratio(t.color, s.bgRGB)
  const ok = r >= need
  if (!ok) failures++
  console.log(
    `  ${ok ? 'ok  ' : 'FAIL'}     ${t.sel.padEnd(42)} ${r.toFixed(2)} (need ${need})  ${t.size}px/${t.weight}` +
    `  bg=rgb(${s.bgRGB.join(',')}) bgShare=${(s.share * 100).toFixed(0)}%`,
  )
}

console.log(failures === 0 ? '\nAll navbar text meets WCAG AA against rendered pixels' : `\n${failures} contrast failure(s)`)
ws.close()
process.exit(failures === 0 ? 0 : 1)
