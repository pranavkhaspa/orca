import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { fetchHealth, fetchMeta, streamAsk } from './api'
import { EXAMPLES, makeT } from './i18n'
import type { AgentRun, Findings, Health, Meta, VerdictLevel } from './types'
import { AgentTrace } from './components/AgentTrace'
import { AnswerPanel } from './components/AnswerPanel'
import {
  DegradedBanner,
  HazardGrid,
  Observations,
  RulesPanel,
  SourcesPanel,
  VerdictCard,
  ZoneCard,
} from './components/Panels'

// MapLibre is ~700 kB of the bundle and is not needed to read the verdict. It is
// loaded in parallel after the first render so a weak connection still shows the
// answer first — which is the part the fisher actually needs.
const MapPanel = lazy(() =>
  import('./components/MapPanel').then((m) => ({ default: m.MapPanel })),
)

const AGENT_LABELS: Record<string, string> = {
  planner: 'Planner',
  geo: 'Geo Resolution',
  ocean: 'Ocean Analytics',
  weather: 'Weather Intelligence',
  incois: 'INCOIS Corroboration',
  domain: 'Risk Engine (deterministic)',
  narrator: 'Narrator',
}
const ORDER = Object.keys(AGENT_LABELS)

function detectLang(text: string): string {
  if (/[ऀ-ॿ]/.test(text)) return /\b(marathi|मराठी)\b/i.test(text) ? 'mr' : 'hi'
  if (/[ঀ-৿]/.test(text)) return 'bn'
  if (/[஀-௿]/.test(text)) return 'ta'
  if (/[ఀ-౿]/.test(text)) return 'te'
  if (/[ಀ-೿]/.test(text)) return 'kn'
  if (/[ഀ-ൿ]/.test(text)) return 'ml'
  if (/[઀-૿]/.test(text)) return 'gu'
  if (/[଀-୿]/.test(text)) return 'or'
  return 'en'
}

export default function App() {
  const [meta, setMeta] = useState<Meta | null>(null)
  const [health, setHealth] = useState<Health | null>(null)
  const [lang, setLang] = useState<string>(
    () => localStorage.getItem('orca.lang') ?? navigator.language.slice(0, 2),
  )
  const [query, setQuery] = useState('')
  const [runs, setRuns] = useState<AgentRun[]>([])
  const [answer, setAnswer] = useState('')
  const [findings, setFindings] = useState<Findings | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const abort = useRef<AbortController | null>(null)
  const resultRef = useRef<HTMLDivElement>(null)

  const t = useMemo(() => makeT(lang), [lang])

  useEffect(() => {
    localStorage.setItem('orca.lang', lang)
    document.documentElement.lang = lang
  }, [lang])

  useEffect(() => {
    const ac = new AbortController()
    fetchMeta(ac.signal).then(setMeta).catch(() => undefined)
    fetchHealth(ac.signal).then(setHealth).catch(() => undefined)
    return () => ac.abort()
  }, [])

  const submit = useCallback(
    (q: string) => {
      const text = q.trim()
      if (!text || busy) return
      abort.current?.abort()
      const ac = new AbortController()
      abort.current = ac

      setRuns([])
      setAnswer('')
      setFindings(null)
      setError('')
      setBusy(true)

      streamAsk(
        text,
        {
          onAgent: (e) =>
            setRuns((prev) => {
              const next = prev.filter((r) => r.name !== e.run.name)
              return [...next, e.run].sort(
                (a, b) => ORDER.indexOf(a.name) - ORDER.indexOf(b.name),
              )
            }),
          onAnswer: (a) => setAnswer(a),
          onDone: (f) => {
            setFindings(f)
            setAnswer(f.answer)
            // The backend detected the language of the question; adopt it so the
            // interface matches the answer the user just received.
            if (f.answer_lang) setLang(f.answer_lang)
          },
          onError: (m) => setError(m),
        },
        ac.signal,
      ).finally(() => setBusy(false))
    },
    [busy],
  )

  // Announce the result to assistive technology and move focus to it, so a
  // screen-reader user is not left on the input after the answer arrives.
  useEffect(() => {
    if (findings) resultRef.current?.focus()
  }, [findings])

  const reported = new Set(runs.map((r) => r.name))
  const pending = ORDER.filter((n) => !reported.has(n)).slice(0, busy ? 2 : 0)

  return (
    <div className="app">
      <header className="top">
        <div className="brand">
          <div className="logo" aria-hidden="true">
            ⚓
          </div>
          <div>
            <h1>{t.title}</h1>
            <p className="tagline">{t.tagline}</p>
          </div>
        </div>
        <div className="top-right">
          <label className="lang-pick">
            <span className="sr-only">{t.language}</span>
            <select
              value={lang}
              onChange={(e) => setLang(e.target.value)}
              aria-label={t.language}
            >
              {(meta?.languages ?? [{ code: 'en', native: 'English' }]).map((l) => (
                <option key={l.code} value={l.code}>
                  {l.native}
                </option>
              ))}
            </select>
          </label>
          {health ? (
            <span
              className={`health ${health.offline ? 'offline' : ''}`}
              title={health.offline ? 'ORCA_OFFLINE is set: every value comes from the baked snapshot' : `snapshot ${health.snapshot_age} · ${health.cache_entries} cached`}
            >
              ● {health.offline ? 'offline mode' : health.status}
            </span>
          ) : null}
        </div>
      </header>

      <section className="ask">
        <form
          onSubmit={(e) => {
            e.preventDefault()
            submit(query)
          }}
        >
          <label className="sr-only" htmlFor="q">
            {t.ask}
          </label>
          <input
            id="q"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              if (findings) setLang(detectLang(e.target.value))
            }}
            placeholder={t.placeholder}
            autoComplete="off"
            enterKeyHint="send"
          />
          <button className="btn primary" type="submit" disabled={busy || !query.trim()}>
            {busy ? '…' : t.submit}
          </button>
        </form>
        <div className="examples">
          <span className="muted">{t.example}:</span>
          {EXAMPLES.map((ex) => (
            <button
              key={ex.q}
              className="chip"
              onClick={() => {
                setQuery(ex.q)
                setLang(ex.lang)
                submit(ex.q)
              }}
              disabled={busy}
            >
              {ex.lang.toUpperCase()}
            </button>
          ))}
        </div>
      </section>

      {error ? <div className="error" role="alert">{error}</div> : null}

      {!findings && !busy ? (
        <div className="empty">
          <p>{t.empty}</p>
          {meta ? (
            <p className="muted">
              {meta.places.length} coastal locations · {meta.sources.length} public data sources
              {meta.llm ? '' : ' · deterministic narration (no model key configured)'}
            </p>
          ) : null}
        </div>
      ) : null}

      {busy && !findings ? <p className="thinking">{t.thinking}</p> : null}

      {findings || busy ? (
        <div className="results" ref={resultRef} tabIndex={-1}>
          <div className="col-left">
            <Suspense
              fallback={<div className="map map-skeleton">◌</div>}
            >
              <MapPanel geo={findings?.geo ?? null} pfz={findings?.pfz ?? null} />
            </Suspense>
          </div>
          <div className="col-main">
            {findings ? <DegradedBanner f={findings} t={t} /> : null}
            {findings ? <VerdictCard f={findings} t={t} /> : null}
            {answer ? <AnswerPanel answer={answer} lang={findings?.answer_lang ?? lang} t={t} /> : null}
            {findings ? (
              <div className="two-up">
                <ZoneCard f={findings} t={t} />
                <HazardGrid f={findings} t={t} />
              </div>
            ) : null}
            {findings ? <Observations f={findings} t={t} /> : null}
            <AgentTrace runs={runs} pending={pending} t={t} />
            {findings ? <SourcesPanel f={findings} t={t} /> : null}
            {meta ? <RulesPanel rules={meta.rules} t={t} /> : null}
          </div>
        </div>
      ) : null}

      <footer className="foot">
        <span>ORCA · SIH26176</span>
        <span className="muted">
          Verdict computed in Go from published thresholds. The language model never decides
          whether it is safe to go to sea.
        </span>
      </footer>
    </div>
  )
}

export type { VerdictLevel }
