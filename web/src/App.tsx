import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { fetchHealth, fetchMeta, streamAsk } from './api'
import { EXAMPLES, makeT } from './i18n'
import { copy } from './tour'
import type { AgentRun, Findings, Health, Meta, VerdictLevel } from './types'
import { AgentTrace } from './components/AgentTrace'
import { AnswerPanel } from './components/AnswerPanel'
import { Boundary } from './components/Boundary'
import { LoadingResults } from './components/Loading'
import { Tutorial } from './components/Tutorial'
import { GlobeIcon } from './components/Icons'
import { TopBar } from './components/TopBar'
import {
  DegradedBanner,
  HazardGrid,
  Observations,
  RulesPanel,
  SourcesPanel,
  VerdictCard,
  ZoneCard,
} from './components/Panels'

// Two heavy, optional visualisations, both kept out of the critical path. The
// verdict is text and must render on its own: a fisher on a 2G connection in a
// boat is the person this is for, and a 620 kB WebGL bundle must never stand
// between them and the answer.
const Globe3D = lazy(() =>
  import('./components/Globe3D').then((m) => ({ default: m.Globe3D })),
)
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

// Which narration stage each agent's report implies. The pipeline reports
// agents, not stages, and the user cares about stages.
const STAGE_OF: Record<string, number> = {
  planner: 0,
  geo: 1,
  ocean: 2,
  weather: 2,
  incois: 3,
  domain: 4,
  narrator: 5,
}

const TOUR_KEY = 'orca.tour.v1'

function detectLang(text: string): string {
  if (/[ऀ-ॿ]/.test(text)) return /\b(marathi|मराठी)\b/i.test(text) ? 'mr' : 'hi'
  if (/[ঀ-৿]/.test(text)) return 'bn'
  if (/[஀-௿]/.test(text)) return 'ta'
  if (/[ఀ-౿]/.test(text)) return 'te'
  if (/[ಀ-೿]/.test(text)) return 'kn'
  if (/[ഀ-ൿ]/.test(text)) return 'ml'
  if (/[઀-૿]/.test(text)) return 'gu'
  if (/[଀-ୀ]/.test(text)) return 'or'
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
  const [globeFailed, setGlobeFailed] = useState(false)
  const [tour, setTour] = useState(false)
  const abort = useRef<AbortController | null>(null)
  const resultRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  const t = useMemo(() => makeT(lang), [lang])
  const c = useMemo(() => copy(lang), [lang])

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

  // First visit gets the tour. Offered once, never again, and always available
  // from the header — a modal that reappears every session gets dismissed
  // without being read, which is worse than not showing it.
  useEffect(() => {
    if (!localStorage.getItem(TOUR_KEY)) setTour(true)
  }, [])

  const closeTour = useCallback(() => {
    localStorage.setItem(TOUR_KEY, 'seen')
    setTour(false)
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
            if (f.answer_lang) setLang(f.answer_lang)
          },
          onError: (m) => setError(m),
        },
        ac.signal,
      ).finally(() => setBusy(false))
    },
    [busy],
  )

  useEffect(() => {
    if (findings) resultRef.current?.focus()
  }, [findings])

  const reported = new Set(runs.map((r) => r.name))
  const pending = ORDER.filter((n) => !reported.has(n)).slice(0, busy ? 2 : 0)
  const stage = runs.reduce((s, r) => Math.max(s, STAGE_OF[r.name] ?? 0), 0)

  const towns = meta?.towns ?? []
  const showHero = !findings && !busy

  return (
    <div className="shell">
      <div className="app">
        <TopBar
          t={t}
          c={c}
          meta={meta}
          health={health}
          lang={lang}
          onLang={setLang}
          onTour={() => setTour(true)}
          hasResults={Boolean(findings)}
        />
        {/* Takes the height of the fixed bar out of flow, so the hero is not
            pushed under it. Kept adjacent to the bar so the pairing is obvious. */}
        <div className="top-spacer" aria-hidden="true" />

        {showHero ? (
          <section className="hero">
            <div className="hero-copy">
              <p className="eyebrow">{t.tagline}</p>
              <h2>
                {t.ask} <span className="grad">ORCA</span>
              </h2>
              <p className="hero-lead">{c.heroLead}</p>
              <ul className="trust">
                {c.trust.map((item, i) => (
                  <li key={item}>
                    {i === 0 ? <GlobeIcon /> : null}
                    {item}
                  </li>
                ))}
              </ul>
            </div>
            <div className="hero-globe">
              <Boundary fallback={<div className="sk-orbit" />} label="hero globe">
                <Suspense fallback={<div className="sk-orbit" />}>
                  <Globe3D
                    towns={towns}
                    geo={null}
                    pfz={null}
                    level={null}
                    onFail={() => setGlobeFailed(true)}
                  />
                </Suspense>
              </Boundary>
              <span className="globe-hint">{c.globeHint}</span>
            </div>
          </section>
        ) : null}

        <section className="ask">
          <form
            onSubmit={(e) => {
              e.preventDefault()
              submit(query)
            }}
          >
            <label className="sr-only" htmlFor="q">
              {c.searchLabel}
            </label>
            <input
              id="q"
              ref={inputRef}
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
              {busy ? <span className="btn-spinner" aria-hidden="true" /> : null}
              {busy ? t.thinking : t.submit}
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
                title={ex.q}
              >
                {ex.lang.toUpperCase()}
              </button>
            ))}
          </div>
        </section>

        {error ? (
          <div className="error" role="alert">
            {error}
          </div>
        ) : null}

        {showHero ? (
          <div className="empty">
            {meta ? (
              <p className="muted">
                {meta.places.length} coastal locations · {meta.sources.length} public data
                sources
                {meta.llm ? '' : ' · deterministic narration (no model key configured)'}
              </p>
            ) : (
              <p className="muted">{t.empty}</p>
            )}
            <p className="muted">
              <button className="btn ghost" onClick={() => setTour(true)}>
                {c.newHere}
              </button>
            </p>
          </div>
        ) : null}

        {busy && !findings ? <LoadingResults c={c} title={t.thinking} stage={stage} /> : null}

        {findings || (busy && runs.length > 0) ? (
          <div className="results" ref={resultRef} tabIndex={-1}>
            <div className="col-left">
              {globeFailed ? (
                <Suspense fallback={<div className="sk-globe" />}>
                  <MapPanel geo={findings?.geo ?? null} pfz={findings?.pfz ?? null} />
                </Suspense>
              ) : (
                <div className="globe-panel">
                  <Boundary
                    fallback={
                      <Suspense fallback={<div className="sk-globe" />}>
                        <MapPanel geo={findings?.geo ?? null} pfz={findings?.pfz ?? null} />
                      </Suspense>
                    }
                    label="result globe"
                  >
                    <Suspense fallback={<div className="sk-globe" />}>
                      <Globe3D
                        towns={towns}
                        geo={findings?.geo ?? null}
                        pfz={findings?.pfz ?? null}
                        level={findings?.verdict.level ?? null}
                        onFail={() => setGlobeFailed(true)}
                      />
                    </Suspense>
                  </Boundary>
                  {findings ? (
                    <div className="globe-caption">
                      <span className="globe-place">{findings.geo.name}</span>
                      <span className="globe-hint inline">{c.globeHint}</span>
                    </div>
                  ) : null}
                </div>
              )}
            </div>
            <div className="col-main">
              {findings ? <DegradedBanner f={findings} t={t} /> : null}
              {findings ? <VerdictCard f={findings} t={t} /> : null}
              {answer ? (
                <AnswerPanel answer={answer} lang={findings?.answer_lang ?? lang} t={t} />
              ) : null}
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
          <span>
            <strong>ORCA</strong> · SIH26176
          </span>
          <span>
            Verdict computed in Go from published thresholds. The language model never
            decides whether it is safe to go to sea.
          </span>
        </footer>
      </div>

      {tour ? <Tutorial c={c} onClose={closeTour} /> : null}
    </div>
  )
}

export type { VerdictLevel }
