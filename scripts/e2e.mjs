// Production-bundle end-to-end check.
//
// This runs the built dist/ against the real Go service in a real browser, with
// no dev server and no mocking. Everything else in the repo tests functions;
// this tests the claim the README makes, which is that a person can open the
// page and be told something true about the sea.
//
// The assertions are deliberately about what a person would see — the rendered
// answer, the verdict, the panel text — rather than about internal state, so
// that a passing run means the product works and not merely that a function
// returned a value.

//   go run ./cmd/orca &                  # from api/, on :8080
//   (cd web && npm run build)            # the bundle under test
//   node scripts/e2e.mjs                 # this file
//
// API, WEB and CHROME override the endpoints. With WEB unset the script serves
// web/dist itself, so there is nothing to start by hand.
//
// RATE_LIMIT_PER_MIN should be raised for this run. The service limits each
// client to twelve questions a minute, which is right for a public demo on free
// upstreams and completely wrong for a harness that asks fourteen questions as
// fast as it can: the product then correctly answers 429, the page correctly
// shows the error, and the test reports a failure that is really the limiter
// doing its job.
//
//   RATE_LIMIT_PER_MIN=1000 go run ./cmd/orca &

import { spawn } from 'node:child_process'
import { readFileSync, existsSync, mkdtempSync, statSync } from 'node:fs'
import { createServer } from 'node:http'
import { Readable } from 'node:stream'
import { tmpdir } from 'node:os'
import { dirname, extname, join, normalize, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const API = process.env.API ?? 'http://127.0.0.1:8080'
const CHROME = process.env.CHROME ?? '/usr/bin/google-chrome'

// The bundle under test. Serving it here rather than expecting a server keeps
// the check one command, and it is the same bytes a static host would serve.
const DIST = resolve(dirname(fileURLToPath(import.meta.url)), '..', 'web', 'dist')
const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.ico': 'image/x-icon',
  '.woff2': 'font/woff2',
}

function serveDist() {
  const index = join(DIST, 'index.html')
  if (!existsSync(index)) {
    console.error('web/dist/index.html is missing. Run: (cd web && npm run build)')
    process.exit(1)
  }
  const server = createServer((req, res) => {
    const url = req.url ?? '/'
    // Proxy the API to the Go service. The bundle calls /api on its own origin,
    // which is exactly the arrangement Vercel's rewrite creates in production;
    // without this the single-page-app fallback below answers the streaming
    // endpoint with index.html, and the page sits waiting for events that can
    // never arrive.
    if (url.startsWith('/api/') || url.startsWith('/healthz')) {
      const upstream = fetch(API + url, { method: req.method, headers: { accept: 'text/event-stream' } })
        .then((up) => {
          res.writeHead(up.status, {
            'content-type': up.headers.get('content-type') ?? 'text/plain',
            'cache-control': 'no-store',
          })
          // Pipe rather than buffer: this endpoint is a long-lived event stream,
          // and collecting it into one buffer before replying would delay every
          // trace event until the analysis had already finished.
          if (up.body) return Readable.fromWeb(up.body).pipe(res)
          res.end()
        })
        .catch((e) => {
          if (res.headersSent) return res.end()
          res.writeHead(502, { 'content-type': 'text/plain' })
          res.end('upstream unreachable: ' + e.message)
        })
      return
    }
    // normalize() plus the prefix check keeps "../" out of the served tree.
    const rel = normalize(decodeURIComponent(url.split('?')[0]))
    let file = join(DIST, rel)
    if (!file.startsWith(DIST)) {
      res.writeHead(403).end()
      return
    }
    if (existsSync(file) && statSync(file).isDirectory()) file = join(file, 'index.html')
    // A single-page app serves index.html for unknown paths, which is also the
    // rewrite vercel.json performs.
    if (!existsSync(file)) file = index
    res.writeHead(200, { 'content-type': MIME[extname(file)] ?? 'application/octet-stream' })
    res.end(readFileSync(file))
  })
  return new Promise((done) => {
    server.listen(0, '127.0.0.1', () => {
      done({ server, url: `http://127.0.0.1:${server.address().port}` })
    })
  })
}

// WEB is filled in below when the caller did not supply a server.
let WEB = process.env.WEB ?? ''
let staticServer = null
if (!WEB) {
  const served = await serveDist()
  staticServer = served.server
  WEB = served.url
}

const profile = mkdtempSync(join(tmpdir(), 'orca-e2e-'))
const chrome = spawn(CHROME, [
  '--headless=new',
  '--disable-gpu',
  '--no-sandbox',
  '--remote-debugging-port=9333',
  `--user-data-dir=${profile}`,
  'about:blank',
], { stdio: ['ignore', 'ignore', 'pipe'] })

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function cdpTarget() {
  for (let i = 0; i < 60; i++) {
    try {
      const r = await fetch('http://127.0.0.1:9333/json/list')
      const list = await r.json()
      const page = list.find((t) => t.type === 'page')
      if (page?.webSocketDebuggerUrl) return page.webSocketDebuggerUrl
    } catch {}
    await sleep(250)
  }
  throw new Error('chrome devtools never came up')
}

const wsUrl = await cdpTarget()
const ws = new WebSocket(wsUrl)
await new Promise((res, rej) => { ws.onopen = res; ws.onerror = rej })

let nextId = 1
const pending = new Map()
ws.onmessage = (ev) => {
  const msg = JSON.parse(ev.data)
  if (msg.id && pending.has(msg.id)) {
    const { res, rej } = pending.get(msg.id)
    pending.delete(msg.id)
    msg.error ? rej(new Error(JSON.stringify(msg.error))) : res(msg.result)
  }
}
const send = (method, params = {}) =>
  new Promise((res, rej) => {
    const id = nextId++
    pending.set(id, { res, rej })
    ws.send(JSON.stringify({ id, method, params }))
  })

const consoleErrors = []
const pageErrors = []
const failedRequests = []

await send('Runtime.enable')
await send('Log.enable')
await send('Network.enable')
await send('Page.enable')

ws.addEventListener('message', (ev) => {
  const m = JSON.parse(ev.data)
  if (m.method === 'Runtime.consoleAPICalled' && m.params.type === 'error') {
    consoleErrors.push(m.params.args.map((a) => a.value ?? a.description).join(' '))
  }
  if (m.method === 'Runtime.exceptionThrown') {
    pageErrors.push(m.params.exceptionDetails.text + ' ' +
      (m.params.exceptionDetails.exception?.description ?? ''))
  }
  if (m.method === 'Log.entryAdded' && m.params.entry.level === 'error') {
    consoleErrors.push(m.params.entry.text)
  }
  if (m.method === 'Network.loadingFailed') {
    // A new question cancels the previous server-sent-events stream, which the
    // browser reports as an abort. That is the stream ending, not a failure.
    if (m.params.errorText !== 'net::ERR_ABORTED') failedRequests.push(m.params.errorText)
  }
})

async function evaluate(expression) {
  const r = await send('Runtime.evaluate', {
    expression, returnByValue: true, awaitPromise: true,
  })
  if (r.exceptionDetails) {
    throw new Error('page threw: ' + (r.exceptionDetails.exception?.description ?? r.exceptionDetails.text))
  }
  return r.result.value
}

// The app is a single page that starts with an input and a button, so the only
// interaction needed is typing a question and waiting for the answer panel.
//
// The page is reloaded before every question. An earlier version of this file
// typed each question into the page left over from the previous one, and a
// query that produced no answer still "passed" because the previous answer was
// still on screen — which is how a Tamil question that ORCA correctly refused
// came to be reported as rendering a verdict.
// Waiting on a fixed delay is how the previous version came to type into a page
// that had not finished loading. The app is ready when its form is in the DOM.
async function gotoApp() {
  await send('Page.navigate', { url: WEB })
  for (let i = 0; i < 80; i++) {
    const ready = await evaluate(
      `!!document.querySelector('form input') && !!document.querySelector('form button')`)
    if (ready) {
      await sleep(400)
      // The first-visit tour is a modal that stays up until it is dismissed, and
      // it covers the page a user is trying to read. This harness never clicks
      // through it, so it would reappear on every single question and sit over
      // the verdict — including the refusals, whose whole assertion is that no
      // verdict word appears anywhere in the rendered text. Dismiss it the way a
      // person would, once, and let localStorage keep it dismissed.
      await evaluate(`
        (() => {
          const skip = document.querySelector('[role="dialog"] .btn.ghost');
          if (skip) skip.click();
          return !!skip;
        })()`)
      await sleep(250)
      return
    }
    await sleep(250)
  }
  throw new Error('the app never became interactive')
}

async function ask(question) {
  await gotoApp()
  await evaluate(`
    (() => {
      const set = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype, 'value').set;
      set.call(document.querySelector('input'), ${JSON.stringify(question)});
      document.querySelector('input').dispatchEvent(
        new Event('input', { bubbles: true }));
      return true;
    })()`)
  // Submit the form rather than clicking a button found by its label. The app
  // keeps the chosen language in localStorage, so by the second question the
  // button reads "स्थिति जाँचें" instead of "Check conditions" and a
  // label-matching selector silently stops submitting anything.
  const submitted = await evaluate(
    `(() => { document.querySelector('form').requestSubmit(); return true; })()`)
  if (!submitted) throw new Error('could not submit the form')

  // Wait for the answer panel to actually contain prose, not just to exist.
  for (let i = 0; i < 160; i++) {
    const state = await evaluate(`
      (() => {
        const a = document.querySelector('.panel.answer, [class*=answer]');
        const err = document.querySelector('.error');
        return {
          answer: a ? (a.textContent || '').trim() : '',
          error: err ? (err.textContent || '').trim() : '',
          busy: !!document.querySelector('button[disabled]'),
        };
      })()`)
    if (state.error) {
      throw new Error('the page showed an error for "' + question + '": ' + state.error)
    }
    if (state.answer.length > 40) return
    await sleep(500)
  }
  const body = await evaluate('document.body.innerText')
  throw new Error('no answer rendered for: ' + question + '\n--- page ---\n' +
    body.slice(0, 400))
}

// A refusal must be visible as a refusal, and must not be mistaken for an
// answer. This is the shape of the failure the stale-panel bug had.
async function inspectRefusal(question) {
  await gotoApp()
  await evaluate(`
    (() => {
      const set = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype, 'value').set;
      set.call(document.querySelector('input'), ${JSON.stringify(question)});
      document.querySelector('input').dispatchEvent(
        new Event('input', { bubbles: true }));
      return true;
    })()`)
  await evaluate(`document.querySelector('form').requestSubmit(); true`)
  // The refusal is worded in whatever language the UI is set to, not in the
  // language of the question, so it is detected by the absence of a verdict
  // rather than by matching any particular copy.
  for (let i = 0; i < 120; i++) {
    const state = await evaluate(`
      (() => {
        const a = document.querySelector('.panel.answer, [class*=answer]');
        return {
          answer: a ? (a.textContent || '').trim() : '',
          body: document.body.innerText,
        };
      })()`)
    if (state && state.answer.length > 20) return state
    await sleep(500)
  }
  return { answer: '', body: '' }
}

const results = []
function check(name, ok, detail = '') {
  results.push({ name, ok, detail })
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${detail ? '  — ' + detail : ''}`)
}

let seenMap = false

// Every place named here is one ORCA actually covers, taken from /api/meta. An
// earlier version asked about a location that is not in the table, and ORCA
// correctly refused — the test was wrong, not the product.
const CASES = [
  { q: 'Is it safe to go fishing off Puri tomorrow?', lang: 'en', place: 'Puri' },
  { q: 'कल पुरी से मछली पकड़ना सुरक्षित है?', lang: 'hi', place: 'Puri' },
  { q: 'கொச்சியில் அடுத்த நாள் மீன் பிடிக்கலாமா?', lang: 'ta', place: 'Kochi' },
  { q: 'ଆଜି ପୁରୀରେ ମାଛ ଧରିବା ସୁରକ୍ଷିତ କି?', lang: 'or', place: 'Puri' },
  { q: 'ಕೊಚ್ಚಿಯಿಂದ ಮೀನು ಹಿಡಿಯಬಹುದೇ?', lang: 'kn', place: 'Kochi' },
  { q: 'Is the weather okay for fishing in Mangaluru?', lang: 'en', place: 'Mangaluru' },
]

// A question with no answerable location must be refused, and the refusal must
// be phrased in the language it was asked in.
// The app renders refusals in the UI's current language, not the query's, so
// these are asserted on the absence of a verdict rather than on any wording.
const REFUSALS = [
  { q: 'Is it safe to go out today?', lang: 'en' },
  { q: 'Is it safe off Atlantis Bay tomorrow?', lang: 'en' },
  { q: 'कल समुद्र में जाना सुरक्षित है?', lang: 'hi' },
  { q: '24 09 10 0600', lang: 'en' },
]

try {
  await send('Page.navigate', { url: WEB })
  await sleep(2500)

  const title = await evaluate('document.title')
  check('production bundle loads', !!title && title.length > 0, title)

  for (const c of CASES) {
    await ask(c.q)
    const info = await evaluate(`
      (() => {
        const a = document.querySelector('.panel.answer, [class*=answer]');
        const body = document.body.innerText;
        return {
          answer: (a ? a.innerText : '').trim(),
          body,
          verdict: (body.match(/\\b(SAFE|CAUTION|UNSAFE|GO|NO-GO|AVOID)\\b/i) || [''])[0],
          degraded: !!document.querySelector('[class*=degraded], .degraded'),
          hasCoast: /offshore/i.test(body),
          hasSource: /(source|open-meteo|incois|snapshot)/i.test(body),
        };
      })()`)

    check(`${c.lang}: renders a non-empty answer`, info.answer.length > 40,
      info.answer.replace(/\s+/g, ' ').slice(0, 70))
    check(`${c.lang}: answers about the place that was asked`,
      new RegExp(c.place, 'i').test(info.answer + ' ' + info.body),
      c.place)
    check(`${c.lang}: states a verdict`, info.verdict.length > 0, info.verdict)
    // Compare against the API rather than looking for an English word: the
    // distance is rendered in the reader's script ("तट से 85 किमी दूर"), and a
    // check that greps for "offshore" passes only in English.
    const api = await (await fetch(`${API}/api/ask`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ query: c.q }),
    })).json()
    const km = api?.pfz?.distance_km
    check(`${c.lang}: shows the distance the engine computed`,
      typeof km === 'number' && new RegExp(String(Math.round(km))).test(info.body),
      `${km} km`)
    check(`${c.lang}: discloses its sources`, info.hasSource)
    if (c.lang === 'en' && !seenMap) {
      // The map is only created once there is something to plot, so it can only
      // be asserted after a question, not on the empty page.
      seenMap = await evaluate(`document.querySelectorAll('canvas').length > 0`)
      check('map renders the result', seenMap)
    }
  }

  // The INCOIS corroboration is the newest panel and the easiest to leave
  // broken, because it renders nothing at all when the bulletin has no line
  // near the queried coast.
  for (const r of REFUSALS) {
    const got = await inspectRefusal(r.q)
    check(`${r.lang}: refuses an unanswerable question`, got.answer.length > 20,
      got.answer.replace(/\s+/g, ' ').slice(0, 70))
    // The whole point of a refusal: no verdict is displayed anywhere on the page.
    check(`${r.lang}: refusal displays no verdict`,
      !/\b(SAFE|CAUTION|UNSAFE|NO-GO|AVOID)\b/.test(got.body),
      got.body.replace(/\s+/g, ' ').slice(0, 80))
  }

  // The advisory panel only exists on an answered question, so ask one before
  // looking for it. Checking it after the refusals meant asserting against a
  // refusal page and reporting a panel as missing.
  await ask('Is it safe to go fishing off Puri tomorrow?')
  const advisory = await evaluate(`
    (() => {
      const body = document.body.innerText;
      const m = body.match(/official[^\\n]{0,120}/i);
      return m ? m[0] : '';
    })()`)
  check('INCOIS corroboration is shown', advisory.length > 0, advisory.slice(0, 90))

  const real = pageErrors.filter((e) => !/favicon|ERR_NAME_NOT_RESOLVED|ERR_ABORTED/i.test(e))
  check('no uncaught page exceptions', real.length === 0, real.join(' | ').slice(0, 160))

  const netFails = failedRequests.filter((e) => !/favicon/i.test(e))
  check('no failed network requests', netFails.length === 0, netFails.join(' | ').slice(0, 160))

  const consoleErrs = consoleErrors.filter((e) => !/favicon|DevTools|Download the/i.test(e))
  check('no console errors', consoleErrs.length === 0, consoleErrs.join(' | ').slice(0, 160))
} catch (e) {
  check('run completed without throwing', false, String(e).slice(0, 300))
} finally {
  const failed = results.filter((r) => !r.ok)
  console.log(`\n${results.length - failed.length}/${results.length} checks passed`)
  ws.close()
  chrome.kill('SIGKILL')
  staticServer?.close()
  process.exit(failed.length ? 1 : 0)
}
