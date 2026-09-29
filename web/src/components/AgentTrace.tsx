import type { AgentRun } from '../types'
import { AGENT_ICON } from './Icons'

// The trace is the evidence that the answer was computed rather than asserted,
// so each row names the agent, its status, its timing and the sources it read.
// A row for an agent that has been announced but has not reported yet shows a
// spinner, so the pipeline is visible while it runs instead of appearing all at
// once at the end.
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
        {runs.map((r) => {
          const Icon = AGENT_ICON[r.name as keyof typeof AGENT_ICON]
          return (
            <li key={r.name} className={`trace-row s-${r.status}`}>
              <span className="trace-icon" aria-hidden="true">
                {Icon ? <Icon /> : '•'}
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
          )
        })}
        {pending.map((p) => {
          const Icon = AGENT_ICON[p as keyof typeof AGENT_ICON]
          return (
            <li key={`pending-${p}`} className="trace-row s-running">
              <span className="trace-icon" aria-hidden="true">
                {Icon ? <Icon /> : '•'}
              </span>
              <div className="trace-body">
                <div className="trace-head">
                  <strong>{p}</strong>
                  <span className="badge s-running">
                    <span className="spin" aria-hidden="true">
                      ⟳
                    </span>
                    running
                  </span>
                </div>
              </div>
            </li>
          )
        })}
      </ol>
    </section>
  )
}
