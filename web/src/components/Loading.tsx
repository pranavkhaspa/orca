import type { Copy } from '../tour'
import { CheckIcon } from './Icons'

// The wait is the most under-designed part of most tools, and here it is often
// fifteen seconds — the ocean and weather calls fan out across several upstream
// services and the model refines the plan. A spinner for fifteen seconds is
// indistinguishable from a hang, and a user who thinks a page is broken stops
// trusting the answer when it finally arrives.
//
// So the wait is narrated. Each stage is named, the reached ones are marked,
// and a bar advances even when the underlying work is not measurable, because
// the honest message is "this is progressing", not "this is 43% done".
export function LoadingResults({
  c,
  title,
  stage,
}: {
  c: Copy
  title: string
  stage: number
}) {
  const total = c.loading.length
  const pct = Math.min(96, Math.round(((stage + 0.5) / total) * 100))
  return (
    <div className="results">
      <div className="col-left">
        <div className="sk-globe" role="presentation">
          <div className="sk-orbit" />
        </div>
      </div>
      <div className="col-main">
        <section className="panel" aria-live="polite" aria-busy="true">
          <h2>{title}</h2>
          <ol className="stages">
            {c.loading.map((label, n) => (
              <li
                key={label}
                className={`stage ${n < stage ? 'done' : n === stage ? 'active' : ''}`}
              >
                <span className="stage-dot">
                  {n < stage ? <CheckIcon /> : n === stage ? '•' : ''}
                </span>
                {label}
              </li>
            ))}
          </ol>
          <div className="progress" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
            <div className="progress-bar" style={{ width: `${pct}%` }} />
          </div>
        </section>
        <div className="panel">
          <h2>…</h2>
          <div className="sk sk-verdict" />
        </div>
      </div>
    </div>
  )
}
