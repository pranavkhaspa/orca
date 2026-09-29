// Navbar behaviour once a result exists: the section rail, scroll-spy, and the
// mobile sheet.
//
//   node scripts/check-navbar-results.mjs
//
// Everything here needs a real answer on screen, because the rail is
// deliberately absent before one exists. It drives the built bundle against the
// real Go service: no mocks, since a mocked result would not reproduce the panel
// heights that the scroll-spy thresholds are tuned to.
import { spawn } from 'node:child_process'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const WEB = process.env.WEB ?? 'http://127.0.0.1:5174'
const CHROME = process.env.CHROME ?? '/usr/bin/google-chrome'
const profile = mkdtempSync(join(tmpdir(), 'navres-'))
const chrome = spawn(CHROME, [
  '--headless=new', '--remote-debugging-port=9334', `--user-data-dir=${profile}`,
  '--no-sandbox', '--disable-gpu', '--hide-scrollbars', '--window-size=1440,900',
  'about:blank',
], { stdio: 'ignore' })

const cleanup = () => { try { chrome.kill('SIGKILL') } catch {} ; rmSync(profile, { recursive: true, force: true }) }
process.on('exit', cleanup)

let wsUrl, ws, id = 0
const pending = new Map()
const send = (method, params = {}, sessionId) => new Promise((res, rej) => {
  const msg = { id: ++id, method, params }
  if (sessionId) msg.sessionId = sessionId
  pending.set(msg.id, { res, rej })
  ws.send(JSON.stringify(msg))
})
for (let i = 0; i < 60; i++) {
  try { const r = await fetch('http://127.0.0.1:9334/json/version'); wsUrl = (await r.json()).webSocketDebuggerUrl; if (wsUrl) break } catch {}
  await new Promise((r) => setTimeout(r, 250))
}
ws = new WebSocket(wsUrl)
await new Promise((r) => (ws.onopen = r))
const errors = []
ws.onmessage = (e) => {
  const m = JSON.parse(e.data)
  if (m.id && pending.has(m.id)) {
    const { res, rej } = pending.get(m.id); pending.delete(m.id)
    m.error ? rej(new Error(JSON.stringify(m.error))) : res(m.result)
  }
  if (m.method === 'Runtime.exceptionThrown') {
    errors.push(m.params.exceptionDetails?.exception?.description ?? 'exception')
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
const setViewport = (width, height) =>
  send('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile: width < 700 }, sessionId)

// Waiting for the app to mount by a fixed sleep is a race: the bundle is
// lazy-loaded and the globe competes for the main thread, so 2s is sometimes
// enough and sometimes not. A missing #q then read as "the navbar is broken".
const waitFor = async (sel, ms = 20000) => {
  const started = Date.now()
  for (;;) {
    if (await evalJs(`!!document.querySelector(${JSON.stringify(sel)})`)) return true
    if (Date.now() - started > ms) return false
    await new Promise((r) => setTimeout(r, 200))
  }
}

let failures = 0
const fail = (m) => { failures++; console.log('  FAIL  ' + m) }
const pass = (m) => console.log('  ok    ' + m)

// Ask a question and wait for the verdict, so the rail is in its real state.
const ASK = `(async () => {
  const input = document.querySelector('#q')
  if (!input) return 'no input'
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set
  setter.call(input, 'Is it safe to fish off Puri tomorrow morning?')
  input.dispatchEvent(new Event('input', { bubbles: true }))
  input.closest('form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  for (let i = 0; i < 90; i++) {
    if (document.querySelector('#verdict')) return 'ok'
    await new Promise((r) => setTimeout(r, 400))
  }
  return 'timeout'
})()`

// ---------------------------------------------------------------- desktop ----
console.log('\ndesktop 1440 — rail and scroll-spy')
await setViewport(1440, 900)
await send('Page.navigate', { url: WEB }, sessionId)
if (!(await waitFor('#q'))) fail('the app never mounted (no #q input)')
await new Promise((r) => setTimeout(r, 500))

if (await evalJs(`!!document.querySelector('.rail')`)) {
  fail('section rail is present before a question has been asked')
} else {
  pass('rail absent before a question is asked')
}

const asked = await evalJs(ASK)
if (asked !== 'ok') fail(`no verdict appeared: ${asked}`)
else pass('verdict rendered')

const rail = await evalJs(`(() => {
  const links = [...document.querySelectorAll('.rail-link')]
  return {
    count: links.length,
    labels: links.map((l) => l.textContent.trim()),
    active: links.findIndex((l) => l.getAttribute('aria-current') === 'true'),
    hasIcons: links.every((l) => !!l.querySelector('svg')),
    barH: Math.round(document.querySelector('.top').getBoundingClientRect().height),
    // Section ids the rail targets must exist, or clicking scrolls nowhere.
    targets: ['verdict', 'zone', 'sources'].map((s) => !!document.getElementById(s)),
  }
})()`)

if (rail.count !== 3) fail(`expected 3 rail links, found ${rail.count}`)
if (!rail.hasIcons) fail('a rail link is missing its icon')
if (rail.active !== 0) fail(`expected the first section active on load, got index ${rail.active}`)
if (rail.targets.some((t) => !t)) fail(`rail target missing: ${JSON.stringify(rail.targets)}`)
if (rail.count === 3 && rail.active === 0 && rail.targets.every(Boolean) && rail.hasIcons) {
  pass(`rail present with ${rail.labels.join(' / ')}, first active`)
}

// Clicking a section must scroll it clear of the fixed bar, not behind it.
const clicked = await evalJs(`(async () => {
  const link = [...document.querySelectorAll('.rail-link')].find((l) => l.textContent.trim() === 'Sources')
  link.click()
  // Wait for the scroll to settle rather than guessing a duration.
  //
  // The journey is ~3000px and the globe is animating the whole time, so under
  // software rendering the smooth scroll can still be in flight long after any
  // fixed timeout. A fixed wait made this check report a stale rail for a
  // reason that had nothing to do with the rail, which is worse than a flaky
  // test: it is a confident wrong answer.
  await new Promise((resolve) => {
    const started = performance.now()
    let last = -1
    let still = 0
    const t = setInterval(() => {
      if (Math.abs(window.scrollY - last) < 0.5) still++
      else { still = 0; last = window.scrollY }
      if (still > 5 || performance.now() - started > 12000) { clearInterval(t); resolve() }
    }, 100)
  })
  const el = document.getElementById('sources')
  const bar = document.querySelector('.top').getBoundingClientRect()
  const r = el.getBoundingClientRect()
  return {
    top: Math.round(r.top),
    barBottom: Math.round(bar.bottom),
    active: [...document.querySelectorAll('.rail-link')].findIndex((l) => l.getAttribute('aria-current') === 'true'),
    scrolled: window.scrollY,
  }
})()`)

if (clicked.top < 0) fail(`sources section is above the viewport after the click: top=${clicked.top}`)
if (clicked.top < clicked.barBottom) {
  fail(`sources section is hidden behind the bar: top=${clicked.top} bar=${clicked.barBottom}`)
} else {
  pass(`sources section lands clear of the bar (top=${clicked.top}, bar=${clicked.barBottom})`)
}
if (clicked.active === 2) pass('scroll-spy marks Sources active after scrolling to it')
else fail(`scroll-spy did not follow the scroll: active index ${clicked.active}, expected 2`)

// ---------------------------------------------------------------- mobile -----
console.log('\nphone 390 — sheet behaviour')
await setViewport(390, 844)
await new Promise((r) => setTimeout(r, 700))

const sheet = await evalJs(`(async () => {
  const btn = document.querySelector('.menu-btn')
  const sheetEl = document.getElementById('top-sheet')
  const before = getComputedStyle(sheetEl).display
  btn.click()
  await new Promise((r) => setTimeout(r, 400))
  const opened = getComputedStyle(sheetEl).display
  const expanded = btn.getAttribute('aria-expanded')
  // The bar must not change height when the sheet opens, or the page jumps.
  const barH1 = Math.round(document.querySelector('.top').getBoundingClientRect().height)
  const overflow = document.documentElement.scrollWidth - window.innerWidth
  return {
    before, opened, expanded, barH1, overflow,
    // Interactive controls only. The health chip is a status readout, so
    // counting it as a control would make this assert something untrue.
    controls: sheetEl.querySelectorAll('select,button').length,
    hasHealth: !!sheetEl.querySelector('.health'),
  }
})()`)

if (sheet.before !== 'none') fail(`sheet was already open: ${sheet.before}`)
if (sheet.opened === 'none') fail('the menu button did not open the sheet')
if (sheet.expanded !== 'true') fail(`aria-expanded is ${sheet.expanded} with the sheet open`)
if (sheet.overflow > 1) fail(`the open sheet causes horizontal overflow: ${sheet.overflow}px`)
if (sheet.controls < 2) fail(`sheet is missing controls: ${sheet.controls}`)
if (!sheet.hasHealth) fail('the health readout is missing from the sheet, so a phone user cannot see data status')
if (sheet.opened !== 'none' && sheet.overflow <= 1) pass(`sheet opens with ${sheet.controls} controls, no overflow`)

// Escape must close it again, and a jump to a section must not leave it open.
const closed = await evalJs(`(async () => {
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  await new Promise((r) => setTimeout(r, 350))
  return {
    display: getComputedStyle(document.getElementById('top-sheet')).display,
    expanded: document.querySelector('.menu-btn').getAttribute('aria-expanded'),
  }
})()`)
if (closed.display !== 'none') fail('Escape did not close the sheet')
else pass('Escape closes the sheet')

const railAfterClose = await evalJs(`(() => ({
  rail: getComputedStyle(document.querySelector('.rail')).display,
  menu: getComputedStyle(document.querySelector('.menu-btn')).display,
}))()`)
if (railAfterClose.rail !== 'none') fail('rail is visible on a phone')
if (railAfterClose.menu === 'none') fail('menu button is hidden on a phone')
if (railAfterClose.rail === 'none' && railAfterClose.menu !== 'none') {
  pass('rail hidden and menu button shown on a phone')
}

// ------------------------------------------------------- localization ----
//
// The rail and its controls are chrome the Go backend never produces, so
// unlike the safety answer they have no upstream fallback. They were hard-coded
// in English, which meant a Kannada user got a Kannada safety verdict next to
// English navigation. This switches the interface language and checks the
// chrome follows, because a build that renders does not prove it translates.
console.log('\nrail labels follow the interface language')
await setViewport(1440, 900)
await send('Page.navigate', { url: WEB }, sessionId)
if (!(await waitFor('#q'))) fail('the app never mounted before the localization checks')
// The result has to be checked. This file asks several questions in one run and
// the API is rate limited, so a throttled ask returns no verdict: ignored, that
// surfaced four paragraphs later as "Cannot read properties of null" on the rail
// selector, which points at the navbar instead of at the thing that failed.
const askedAgain = await evalJs(ASK)
if (askedAgain !== 'ok') {
  fail(`no verdict before the localization checks (${askedAgain}); the API is probably rate limited`)
  console.log(failures === 0 ? '\nALL CHECKS PASSED' : `\n${failures} CHECK(S) FAILED`)
  ws.close()
  process.exit(1)
}
await new Promise((r) => setTimeout(r, 800))

const enLabels = await evalJs(`[...document.querySelectorAll('.rail-link')].map(l => l.textContent.trim())`)

const switched = await evalJs(`(async () => {
  const sel = document.querySelector('.lang-row select') || document.querySelector('select')
  if (!sel) return 'no language control'
  const s = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set
  s.call(sel, 'kn')
  sel.dispatchEvent(new Event('change', { bubbles: true }))
  await new Promise(r => setTimeout(r, 600))
  return 'ok'
})()`)
if (switched !== 'ok') fail(`could not switch language: ${switched}`)

const kn = await evalJs(`({
  labels: [...document.querySelectorAll('.rail-link')].map(l => l.textContent.trim()),
  railLabel: document.querySelector('.rail')?.getAttribute('aria-label'),
  menuLabel: document.querySelector('.menu-btn')?.getAttribute('aria-label'),
  englishLeft: [...document.querySelectorAll('.rail-link')].filter(l => ['Verdict','Zone','Sources'].includes(l.textContent.trim())).length,
})`)
if (kn.labels.length !== 3) fail(`expected 3 rail links in Kannada, got ${kn.labels.length}`)
else if (JSON.stringify(kn.labels) === JSON.stringify(enLabels)) fail('rail labels did not change with the language')
else pass(`rail labels localised (${kn.labels.join(' / ')})`)

if (kn.englishLeft > 0) fail(`${kn.englishLeft} rail label(s) still in English after switching to Kannada`)
if (kn.railLabel && kn.railLabel === 'Result sections') fail('rail aria-label is still hard-coded English')
else pass(`rail aria-label localised ("${kn.railLabel}")`)
if (kn.menuLabel && /^(Open|Close) menu$/.test(kn.menuLabel)) fail('menu aria-label is still hard-coded English')
else pass(`menu aria-label localised ("${kn.menuLabel}")`)

// The rail's width is set by its longest label, and the labels change with the
// language. Reusing the full panel headings made the rail 606px wide in Tamil,
// 42% of a 1440px viewport spent on navigation, with nothing failing: the layout
// was technically valid and the results column was pushed off screen. So the
// guard is on width, not on overflow.
// Each step has to await a render: measuring in the same tick as the change
// event would just read the previous language's labels.
const widest = await evalJs(`(async () => {
  const sel = document.querySelector('.lang-row select') || document.querySelector('select')
  const sv = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set
  const codes = [...sel.options].map(o => o.value)
  let worst = { w: 0, lang: '' }
  for (const c of codes) {
    sv.call(sel, c)
    sel.dispatchEvent(new Event('change', { bubbles: true }))
    await new Promise(r => setTimeout(r, 250))
    const rail = document.querySelector('.rail')
    if (!rail) return { worst, vw: window.innerWidth, missingRail: c }
    const w = rail.getBoundingClientRect().width
    if (w > worst.w) worst = { w, lang: c }
  }
  return { worst, vw: window.innerWidth }
})()`)
if (widest.missingRail) {
  fail(`the rail disappeared while the language was ${widest.missingRail}`)
}
const worstPct = (widest.worst.w / widest.vw) * 100
if (widest.worst.w > 360) {
  fail(`rail is ${Math.round(widest.worst.w)}px wide in ${widest.worst.lang} (${worstPct.toFixed(0)}% of the viewport); keep the rail under 360px`)
} else {
  pass(`rail stays narrow in every language (widest ${Math.round(widest.worst.w)}px, ${widest.worst.lang}, ${worstPct.toFixed(0)}% of viewport)`)
}

if (errors.length) { console.log('\npage errors:'); for (const e of errors) fail(e) }
else pass('no page errors')

console.log(failures === 0 ? '\nALL CHECKS PASSED' : `\n${failures} CHECK(S) FAILED`)
ws.close()
process.exit(failures === 0 ? 0 : 1)
