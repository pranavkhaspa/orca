// Perceived-lag harness.
//
// "Feels slow" is not a single number, and the two that used to be measured
// here were both wrong. Frames-per-second in headless Chrome is throttled to
// about 1 regardless of what the page does, so it says nothing about the
// machine. And a long-task observer started immediately before a click also
// catches the work still finishing from the render before it, which is how a
// 4.2 second task that belonged to the verdict render got blamed on the rail.
//
// What this measures instead is main-thread blocking after the page has
// settled, because that is what a person feels: input latency, and whether a
// scroll arrives when it was asked for.

import { spawn } from 'node:child_process'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const WEB = process.env.WEB || 'http://127.0.0.1:5199'
const CHROME = process.env.CHROME || '/usr/bin/google-chrome'

const profile = mkdtempSync(join(tmpdir(), 'orca-perf-'))
const chrome = spawn(
  CHROME,
  [
    '--headless=new',
    '--remote-debugging-port=9501',
    `--user-data-dir=${profile}`,
    '--no-sandbox',
    '--enable-unsafe-swiftshader',
    '--use-angle=swiftshader',
    '--ignore-gpu-blocklist',
    '--window-size=1440,900',
    'about:blank',
  ],
  { stdio: 'ignore' },
)

let wsUrl, ws, id = 0
const pending = new Map()
const sent = []
const send = (m, p = {}, s) =>
  new Promise((res, rej) => {
    const msg = { id: ++id, method: m, params: p }
    if (s) msg.sessionId = s
    pending.set(msg.id, { res, rej })
    ws.send(JSON.stringify(msg))
  })
for (let i = 0; i < 100; i++) {
  try {
    const r = await fetch('http://127.0.0.1:9501/json/version')
    wsUrl = (await r.json()).webSocketDebuggerUrl
    if (wsUrl) break
  } catch {}
  await new Promise((r) => setTimeout(r, 200))
}
ws = new WebSocket(wsUrl)
await new Promise((r) => (ws.onopen = r))
ws.onmessage = (e) => {
  const m = JSON.parse(e.data)
  if (m.method === 'Network.loadingFinished') sent.push(m.params.encodedDataLength)
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
await send('Network.enable', {}, sessionId)
const ev = async (x) => {
  const r = await send(
    'Runtime.evaluate',
    { expression: x, returnByValue: true, awaitPromise: true },
    sessionId,
  )
  if (r.exceptionDetails)
    throw new Error(r.exceptionDetails.exception?.description ?? 'eval failed')
  return r.result.value
}
await send('Emulation.setDeviceMetricsOverride',
  { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false }, sessionId)

let failures = 0
const fail = (m) => { failures++; console.log('  FAIL  ' + m) }
const pass = (m) => console.log('  ok    ' + m)

await send('Page.navigate', { url: WEB }, sessionId)
for (let i = 0; i < 150; i++) {
  if (await ev(`!!document.querySelector('#q')`)) break
  await new Promise((r) => setTimeout(r, 100))
}
await ev(`document.querySelector('.tour-backdrop')?.click()`)

const payload = await ev(`(() => {
  const r = performance.getEntriesByType('resource')
  const js = r.filter(x => x.name.endsWith('.js'))
  return {
    totalKB: Math.round(r.reduce((a, x) => a + x.transferSize, 0) / 1024),
    jsKB: Math.round(js.reduce((a, x) => a + x.transferSize, 0) / 1024),
    scripts: js.length,
  }
})()`)
console.log(`\npayload  ${payload.totalKB}KB total, ${payload.jsKB}KB JS in ${payload.scripts} files`)

// Ask, then let everything settle before measuring anything. Work that is still
// in flight when the observer opens belongs to the previous render.
const verdictMs = await ev(`(async () => {
  const t = performance.now()
  const i = document.querySelector('#q')
  const s = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set
  s.call(i, 'Is it safe to fish off Puri tomorrow morning?')
  i.dispatchEvent(new Event('input', { bubbles: true }))
  i.closest('form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  for (let k = 0; k < 300; k++) {
    if (document.querySelector('#verdict')) return Math.round(performance.now() - t)
    await new Promise(r => setTimeout(r, 50))
  }
  return -1
})()`)
console.log(`\ntime to verdict  ${verdictMs}ms`)
if (verdictMs < 0) fail('no verdict appeared')
else if (verdictMs > 6000) fail(`time to verdict ${verdictMs}ms; the answer should not take this long`)
else pass(`time to verdict ${verdictMs}ms`)

// The globe is built during idle time on purpose, so waiting for it to exist
// is what "settled" means here. Without this the one-off build is counted as
// steady-state idleness, which is the opposite of what the check below asserts.
const settled = await ev(`(async () => {
  for (let i = 0; i < 200; i++) {
    if (document.querySelector('canvas')) break
    await new Promise(r => setTimeout(r, 50))
  }
  await new Promise(r => setTimeout(r, 2500))
  return !!document.querySelector('canvas')
})()`)
if (!settled) console.log('note: no globe canvas; measuring the page without it')
await new Promise((r) => setTimeout(r, 2000))

// The settled page must be idle. A decorative globe that renders forever keeps
// the main thread warm and every interaction inherits the wait.
const idle = await ev(`(async () => {
  const tasks = []
  const po = new PerformanceObserver(l => { for (const e of l.getEntries()) tasks.push(Math.round(e.duration)) })
  po.observe({ entryTypes: ['longtask'] })
  await new Promise(r => setTimeout(r, 3000))
  po.disconnect()
  return tasks
})()`)
const worstIdle = idle.length ? Math.max(...idle) : 0
console.log(`idle long tasks  ${JSON.stringify(idle)}`)
if (worstIdle > 250) fail(`idle page blocked for ${worstIdle}ms; nothing is changing, so nothing should be running`)
else pass(`idle page is not blocking the main thread (worst ${worstIdle}ms)`)

const click = await ev(`(async () => {
  const tasks = []
  const po = new PerformanceObserver(l => { for (const e of l.getEntries()) tasks.push(Math.round(e.duration)) })
  po.observe({ entryTypes: ['longtask'] })
  const link = [...document.querySelectorAll('.rail-link')].pop()
  const t = performance.now()
  link.click()
  let settle = -1
  await new Promise(res => {
    let last = -1, still = 0
    const iv = setInterval(() => {
      if (Math.abs(scrollY - last) < 0.5) still++
      else { still = 0; last = scrollY }
      if (still > 4) { clearInterval(iv); settle = Math.round(performance.now() - t); res() }
    }, 100)
  })
  po.disconnect()
  return { settle, tasks, maxTask: tasks.length ? Math.max(...tasks) : 0 }
})()`)
console.log(`\nrail click  settled in ${click.settle}ms, long tasks ${JSON.stringify(click.tasks)}`)
if (click.settle > 1500) fail(`a rail click took ${click.settle}ms to arrive; navigation should be immediate or quick`)
else pass(`rail click arrives in ${click.settle}ms`)
if (click.maxTask > 500) fail(`rail click blocked the main thread for ${click.maxTask}ms`)
else pass(`rail click blocks for at most ${click.maxTask}ms`)

console.log(`\ntotal transferred  ${Math.round(sent.reduce((a, b) => a + b, 0) / 1024)}KB`)
console.log(failures === 0 ? '\nALL CHECKS PASSED' : `\n${failures} CHECK(S) FAILED`)
ws.close()
chrome.kill('SIGKILL')
process.exit(failures === 0 ? 0 : 1)
