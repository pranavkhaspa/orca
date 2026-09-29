import type { Citation, Findings, Rules, Severity } from '../types'
import type { T } from '../i18n'
import { RuleIcon, VERDICT_ICON } from './Icons'

const LEVEL_TEXT: Record<string, string> = {
  go: 'GO',
  caution: 'CAUTION',
  'no-go': 'NO-GO',
}

export function VerdictCard({ f, t }: { f: Findings; t: T }) {
  const v = f.verdict
  const LevelIcon = VERDICT_ICON[v.level] ?? RuleIcon
  return (
    <section className={`panel verdict v-${v.level}`}>
      <div className="verdict-inner">
        <h2>{t.verdict}</h2>
        <div className="verdict-head">
          <div className="verdict-level">
            <LevelIcon />
            {LEVEL_TEXT[v.level] ?? v.level}
          </div>
          <div className="verdict-where">
            <strong>{f.geo.name}</strong>
            <span className="muted">
              {f.plan.window_hours}h window ·{' '}
              {new Date(f.marine.time).toLocaleString(undefined, {
                hour: '2-digit',
                minute: '2-digit',
                day: 'numeric',
                month: 'short',
              })}
            </span>
          </div>
        </div>
        <p className="verdict-source">
          <RuleIcon />
          {t.verdictSource}
        </p>
        <ul className="rationale">
          {v.rationale.map((r, i) => (
            <li key={i}>{r}</li>
          ))}
        </ul>
      </div>
    </section>
  )
}

export function HazardGrid({ f, t }: { f: Findings; t: T }) {
  return (
    <section className="panel">
      <h2>{t.hazards}</h2>
      <div className="hazards">
        {f.verdict.hazards.map((h) => (
          <div key={h.kind} className={`hazard s-${h.severity as Severity}`}>
            <div className="hazard-head">
              <span className="hazard-kind">{h.kind}</span>
              <span className={`badge s-${h.severity}`}>{h.severity}</span>
            </div>
            <div className="hazard-value">{h.value}</div>
            <div className="hazard-limit">
              {t.rules}: {h.limit}
            </div>
            <p className="hazard-note">{h.note}</p>
          </div>
        ))}
      </div>
    </section>
  )
}

function Metric({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="metric">
      <span className="metric-label">{label}</span>
      <span className="metric-value">{value}</span>
      {hint ? <span className="metric-hint">{hint}</span> : null}
    </div>
  )
}

export function Observations({ f, t }: { f: Findings; t: T }) {
  const m = f.marine
  const w = f.weather
  const u = t.units
  return (
    <section className="panel">
      <h2>
        {t.sea} · {t.sky}
      </h2>
      <div className="metrics">
        <Metric label={t.metrics.wave} value={`${m.wave_height_m} ${u.m}`} hint={`@ ${m.wave_period_s} ${u.s}`} />
        <Metric label={t.metrics.swell} value={`${m.swell_height_m} ${u.m}`} hint={`@ ${m.swell_period_s} ${u.s}`} />
        <Metric label={t.metrics.tide} value={`${m.tide_m} ${u.m}`} />
        <Metric label={t.metrics.sst} value={`${m.sst_c} ${u.c}`} />
        <Metric label={t.metrics.anomaly} value={`${f.pfz.anomaly_c >= 0 ? '+' : ''}${f.pfz.anomaly_c} ${u.c}`} />
        <Metric label={t.metrics.wind} value={`${w.wind_kmh} ${u.kmh}`} />
        <Metric label={t.metrics.gust} value={`${w.gust_kmh} ${u.kmh}`} />
        <Metric label={t.metrics.rain} value={`${w.precip_mm} mm`} />
        <Metric label={t.metrics.cloud} value={`${w.cloud_pct}%`} />
        <Metric label={t.metrics.lightning} value={w.lightning_risk} hint={`WMO ${w.wx_code}`} />
      </div>
      <div className="advisory">
        <strong>{t.advisory}</strong>{' '}
        <span className={f.advisory.available ? 'ok' : 'muted'}>
          {f.advisory.available ? `✓ ${f.advisory.status}` : `— ${f.advisory.status}`}
        </span>
        <span className="muted"> · {f.advisory.sector}</span>
        {/* The official zone is drawn on the map, so it is worth stating
            plainly how far it sits from ours. The two are computed on
            different bases, and presenting either as the other would be a
            lie that a fisherman would pay for. */}
        {f.advisory.official_found ? (
          <div className="official">
            <span className="muted">{t.officialZone}:</span>{' '}
            <strong>{f.advisory.official_label}</strong>{' '}
            <span>
              {t.officialDistance} {f.advisory.distance_km.toFixed(0)} {u.km} ·{' '}
              {Math.round(f.advisory.bearing_deg)}°
            </span>
            {f.advisory.bulletin ? (
              <span className="muted">
                {' '}
                · {t.advisoryFor} {f.advisory.bulletin.slice(0, 10)}
              </span>
            ) : null}
          </div>
        ) : null}
      </div>
    </section>
  )
}

export function ZoneCard({ f, t }: { f: Findings; t: T }) {
  const p = f.pfz
  return (
    <section className="panel">
      <h2>{t.zone}</h2>
      <div className="zone-head">
        <div className="zone-score">{p.score.toFixed(2)}</div>
        <div className="zone-meta">
          <div>
            {p.lat.toFixed(3)}, {p.lon.toFixed(3)}
          </div>
          <div className="muted">
            {p.distance_km.toFixed(0)} km {t.zoneFrom} {f.geo.name}
          </div>
          <div className="muted">
            {t.confidence}: <strong className={`conf-${p.confidence}`}>{p.confidence}</strong>
          </div>
        </div>
      </div>
      <div className="zone-band">{p.sst_band}</div>
      <ul className="rationale">
        {p.reasoning.map((r, i) => (
          <li key={i}>{r}</li>
        ))}
      </ul>
    </section>
  )
}

export function SourcesPanel({ f, t }: { f: Findings; t: T }) {
  const prov: Citation[] = f.prov ?? []
  // One row per source+dataset pair; the same upstream URL is called for eight
  // datasets and listing it eight times would read as eight independent sources.
  const bySource = new Map<string, Citation[]>()
  for (const c of prov) {
    const arr = bySource.get(c.Source) ?? []
    if (!arr.some((x) => x.Dataset === c.Dataset && x.Retrieved === c.Retrieved)) arr.push(c)
    bySource.set(c.Source, arr)
  }
  return (
    <section className="panel">
      <h2>{t.sources}</h2>
      {prov.length === 0 ? <p className="muted">—</p> : null}
      {Array.from(bySource.entries()).map(([source, cites]) => {
        const live = cites.some((c) => c.Live)
        const failed = cites.some((c) => c.Err)
        return (
          <details key={source} className="src">
            <summary>
              <span className="src-name">{source}</span>
              <span className={`badge ${failed ? 's-critical' : live ? 's-ok' : 's-caution'}`}>
                {failed ? t.unavailable : live ? t.live : t.snapshot}
              </span>
              <span className="muted">
                {cites.length} dataset{cites.length === 1 ? '' : 's'}
              </span>
            </summary>
            <table className="src-table">
              <tbody>
                {cites.map((c) => (
                  <tr key={c.Dataset + c.Retrieved}>
                    <td className="src-dataset">{c.Dataset}</td>
                    <td className="src-time">
                      {t.retrieved} {new Date(c.Retrieved).toLocaleString()}
                    </td>
                    <td>
                      {c.URL ? (
                        <a href={c.URL} target="_blank" rel="noreferrer noopener">
                          ↗
                        </a>
                      ) : (
                        <span className="muted">—</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </details>
        )
      })}
    </section>
  )
}

export function RulesPanel({ rules, t }: { rules: Rules; t: T }) {
  const [lo, hi] = rules.pfz_band_c
  return (
    <section className="panel">
      <h2>{t.rules}</h2>
      <p className="muted">{t.rulesNote}</p>
      <table className="rules">
        <tbody>
          <tr>
            <td>{t.metrics.wave}</td>
            <td>
              ⚠ {rules.wave_caution_m} {t.units.m} · ⛔ {rules.wave_critical_m} {t.units.m}
            </td>
          </tr>
          <tr>
            <td>{t.metrics.gust}</td>
            <td>
              ⚠ {rules.gust_caution_kmh} {t.units.kmh} · ⛔ {rules.gust_critical_kmh} {t.units.kmh}
            </td>
          </tr>
          <tr>
            <td>{t.metrics.tide}</td>
            <td>
              ⚠ {rules.tide_caution_m} {t.units.m}
            </td>
          </tr>
          <tr>
            <td>{t.metrics.sst}</td>
            <td>
              {lo}–{hi} {t.units.c}
            </td>
          </tr>
          <tr>
            <td>{t.metrics.anomaly}</td>
            <td>
              ⚠ {rules.anomaly_caution_c} {t.units.c}
            </td>
          </tr>
        </tbody>
      </table>
    </section>
  )
}

// "Optional" inputs are the ones that cannot change the safety verdict. The API
// reports them alongside genuine data failures because honesty about what was
// consulted matters more than a clean-looking screen — but the box should not be
// styled like a warning when a fishing advisory and the narrator were the only
// things missing. That would tell a reader the safety answer was weakened, which
// is the opposite of what this system guarantees.
const OPTIONAL_INPUTS = new Set(['llm', 'incois'])

function degradedLabel(key: string, t: T): string {
  const snapshot = key.endsWith(':snapshot')
  const base = snapshot ? key.slice(0, key.indexOf(':')) : key
  if (base === 'marine') return `${t.sea}${snapshot ? ` (${t.snapshot})` : ''}`
  if (base === 'weather') return `${t.sky}${snapshot ? ` (${t.snapshot})` : ''}`
  const named = t.degradedItems as Record<string, string | undefined>
  return named[base] ?? key
}

export function DegradedBanner({ f, t }: { f: Findings; t: T }) {
  const d = f.degraded ?? []
  if (d.length === 0) return null
  const material = d.some((k) => !OPTIONAL_INPUTS.has(k))
  return (
    <div className={material ? 'degraded' : 'degraded note'} role="status">
      <strong>{t.degraded}:</strong> {d.map((k) => degradedLabel(k, t)).join(', ')}.{' '}
      {t.degradedNote}
    </div>
  )
}
