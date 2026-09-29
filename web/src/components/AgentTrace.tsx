import type { AgentRun } from '../types'

const ICON: Record<string, string> = {
  planner: '🧭',
  geo: '📍',
  ocean: '🌊',
  weather: '🌤️',
  incois: '🏛️',
  domain: '⚖️',
  narrator: '📝',
}

// The trace is the point of the demo: six named agents, each with its own
// status, timing and inputs. A spinner row is shown for an agent that has been
// announced but has not reported yet, so the pipeline is visible while it runs.
export function AgentTrace({
  runs,
  pending,
  t,
}: {
  runs: AgentRun[]
  pending: string[]
  t: { trace: string }
}) {
  return (
    <section className="panel">
      <h2>{t.trace}</h2>
      <ol className="trace">
        {runs.map((r) => (
          <li key={r.name} className={`trace-row s-${r.status}`}>
            <span className="trace-icon" aria-hidden="true">
              {ICON[r.name] ?? '•'}
            </span>
            <div className="trace-body">
              <div className="trace-head">
                <strong>{r.label}</strong>
                <span className={`badge s-${r.status}`}>{r.status}</span>
                <time className="trace-time">
                  {r.started && r.ended
                    ? `${((Date.parse(r.ended) - Date.parse(r.started)) / 1000).toFixed(2)}s`
                    : ''}
                </time>
              </div>
              <p className="trace-summary">{r.summary}</p>
              {r.err ? <p className="trace-err">{r.err}</p> : null}
              {r.sources && r.sources.length > 0 ? (
                <p className="trace-sources">
                  {r.sources.length} source{r.sources.length === 1 ? '' : 's'} ·{' '}
                  {r.sources.filter((s) => s.Live).length} live
                </p>
              ) : null}
            </div>
          </li>
        ))}
        {pending.map((p) => (
          <li key={`pending-${p}`} className="trace-row s-running">
            <span className="trace-icon spin" aria-hidden="true">
              ⟳
            </span>
            <div className="trace-body">
              <div className="trace-head">
                <strong>{p}</strong>
                <span className="badge s-running">running</span>
              </div>
            </div>
          </li>
        ))}
      </ol>
    </section>
  )
}
