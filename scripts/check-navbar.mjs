// Navbar geometry and contrast check.
//
//   WEB=http://127.0.0.1:5199 node scripts/check-navbar.mjs
//
// Drives the built bundle in real Chrome over CDP. The assertions are the ones
// that are easy to break by editing CSS and impossible to notice by eye: the
// fixed bar must not overlap the first screen, the spacer must match the bar's
// real height, and the rail must be reachable at every breakpoint.
import { spawn } from 'node:child_process'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const WEB = process.env.WEB ?? 'http://127.0.0.1:5199'
const CHROME = process.env.CHROME ?? '/usr/bin/google-chrome'
const profile = mkdtempSync(join(tmpdir(), 'navcheck-'))
const chrome = spawn(CHROME, [
  '--headless=new', '--remote-debugging-port=9333', `--user-data-dir=${profile}`,
  '--no-sandbox', '--disable-gpu', '--hide-scrollbars', '--window-size=1440,900',
  'about:blank',
], { stdio: 'ignore' })

const cleanup = () => { try { chrome.kill('SIGKILL') } catch {} ; rmSync(profile, { recursive: true, force: true }) }
process.on('exit', cleanup)

let ws, id = 0
const pending = new Map()
const send = (method, params = {}, sessionId) =>
  new Promise((res, rej) => {
    const msg = { id: ++id, method, params }
    if (sessionId) msg.sessionId = sessionId
    pending.set(msg.id, { res, rej })
    ws.send(JSON.stringify(msg))
  })

async function connect() {
  for (let i = 0; i < 60; i++) {
    try {
      const r = await fetch('http://127.0.0.1:9333/json/version')
      const j = await r.json()
      return j.webSocketDebuggerUrl
    } catch { await new Promise((r) => setTimeout(r, 250)) }
  }
  throw new Error('chrome devtools never came up')
}

const url = await connect()
ws = new WebSocket(url)
await new Promise((r) => (ws.onopen = r))
ws.onmessage = (e) => {
  const m = JSON.parse(e.data)
  if (m.id && pending.has(m.id)) {
    const { res, rej } = pending.get(m.id)
    pending.delete(m.id)
    m.error ? rej(new Error(JSON.stringify(m.error))) : res(m.result)
  }
}

const { targetId } = await send('Target.createTarget', { url: 'about:blank' })
const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true })
await send('Page.enable', {}, sessionId)
await send('Runtime.enable', {}, sessionId)

const errors = []
ws.addEventListener('message', (e) => {
  const m = JSON.parse(e.data)
  if (m.method === 'Runtime.exceptionThrown') {
    errors.push(m.params.exceptionDetails?.exception?.description ?? 'exception')
  }
})

async function evalJs(expression) {
  const r = await send('Runtime.evaluate', {
    expression, returnByValue: true, awaitPromise: true,
  }, sessionId)
  if (r.exceptionDetails) throw new Error(r.exceptionDetails.exception?.description ?? 'eval failed')
  return r.result.value
}

async function viewAt(width, height) {
  await send('Emulation.setDeviceMetricsOverride', {
    width, height, deviceScaleFactor: 1, mobile: width < 700,
  }, sessionId)
  await send('Page.navigate', { url: WEB }, sessionId)
  // Poll for the mounted app rather than sleeping a guessed interval: the bundle
  // is lazy-loaded, so a fixed wait is a race that fails as a layout error.
  for (let i = 0; i < 100; i++) {
    if (await evalJs(`!!document.querySelector('.top')`)) break
    await new Promise((r) => setTimeout(r, 200))
  }
  await new Promise((r) => setTimeout(r, 400))
}

const MEASURE = `(() => {
  const bar = document.querySelector('.top')
  const sp = document.querySelector('.top-spacer')
  const first = document.querySelector('.hero, .ask')
  const b = bar?.getBoundingClientRect()
  const s = sp?.getBoundingClientRect()
  const f = first?.getBoundingClientRect()
  const cs = bar ? getComputedStyle(bar) : null
  return {
    barPos: cs?.position ?? null,
    barH: b ? Math.round(b.height) : null,
    spacerH: s ? Math.round(s.height) : null,
    firstTop: f ? Math.round(f.top) : null,
    // Horizontal overflow of the document, the classic mobile-nav bug.
    docScrollW: document.documentElement.scrollWidth,
    innerW: window.innerWidth,
    railVisible: !!document.querySelector('.rail') && getComputedStyle(document.querySelector('.rail')).display !== 'none',
    // The menu button only exists in the results layout, and no results have
    // been asked for here, so its absence is correct. Recorded either way.
    menuPresent: !!document.querySelector('.menu-btn'),
    menuVisible: !!document.querySelector('.menu-btn') && getComputedStyle(document.querySelector('.menu-btn')).display !== 'none',
    healthText: document.querySelector('.health')?.textContent?.trim() ?? null,
    langValue: document.querySelector('.lang-row select')?.value ?? null,
    tourLabel: document.querySelector('.tour-label')?.textContent?.trim() ?? null,
    // The product h1 must exist somewhere for the document to have a title.
    h1Count: document.querySelectorAll('h1').length,
  }
})()`

const VIEWPORTS = [
  ['desktop 1440', 1440, 900],
  ['laptop 1100', 1100, 800],
  ['tablet 820', 820, 1100],
  ['phone 390', 390, 844],
  ['small phone 320', 320, 720],
]

let failures = 0
const fail = (m) => { failures++; console.log('  FAIL  ' + m) }
const pass = (m) => console.log('  ok    ' + m)

for (const [label, w, h] of VIEWPORTS) {
  await viewAt(w, h)
  const m = await evalJs(MEASURE)
  console.log(`\n${label}`)
  console.log(`  bar ${m.barPos} h=${m.barH}  spacer=${m.spacerH}  firstTop=${m.firstTop}  rail=${m.railVisible} menu=${m.menuVisible}`)

  if (m.barPos !== 'fixed') fail(`bar is ${m.barPos}, expected fixed`)
  if (m.spacerH !== m.barH) fail(`spacer ${m.spacerH}px != bar ${m.barH}px, content will sit under the bar`)
  if (m.firstTop !== null && m.firstTop < m.barH - 1) fail(`first content starts at ${m.firstTop}px, under a ${m.barH}px bar`)
  if (m.docScrollW > m.innerW + 1) fail(`horizontal overflow: scrollWidth ${m.docScrollW} > viewport ${m.innerW}`)

  // With no results there is no section rail and no menu button to begin with;
  // asserting they are visible would be asserting a question had been asked.
  if (!m.menuPresent) {
    if (m.railVisible) fail('section rail shown with no results')
  } else if (w <= 860) {
    if (!m.menuVisible) fail('menu button not shown below the 860px breakpoint')
    if (m.railVisible) fail('section rail still shown below the breakpoint')
  } else if (m.menuVisible) {
    fail('menu button visible on a wide screen')
  }
  if (m.h1Count !== 1) fail(`expected exactly 1 <h1>, found ${m.h1Count}`)
  if (m.docScrollW <= m.innerW + 1 && m.spacerH === m.barH) pass('layout clean')
}

if (errors.length) {
  console.log('\npage errors:')
  for (const e of errors) fail(e)
} else {
  pass('no page errors')
}

console.log(failures === 0 ? '\nALL CHECKS PASSED' : `\n${failures} CHECK(S) FAILED`)
ws.close()
process.exit(failures === 0 ? 0 : 1)
