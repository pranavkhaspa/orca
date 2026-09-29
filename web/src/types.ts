// Types mirror api/internal/domain/types.go. Kept hand-written rather than
// generated so the frontend contract is reviewable in one screen; the server
// test suite pins the JSON field names this file depends on.

export type Severity = 'ok' | 'caution' | 'critical'
export type VerdictLevel = 'go' | 'caution' | 'no-go'
export type AgentStatus = 'pending' | 'running' | 'done' | 'failed' | 'skipped'

export interface Citation {
  Source: string
  Dataset: string
  URL: string
  Retrieved: string
  Live: boolean
  Err?: string
}

export interface Plan {
  place: string
  window_hours: number
  intent: string
  lang: string
  planned: boolean
}

export interface Geo {
  query: string
  name: string
  lat: number
  lon: number
  way_lat: number
  way_lon: number
  distance_km: number
  bearing_deg: number
  source: string
}

export interface Marine {
  time: string
  wave_height_m: number
  wave_period_s: number
  wave_dir_deg: number
  swell_height_m: number
  swell_period_s: number
  wind_wave_m: number
  sst_c: number
  tide_m: number
}

export interface Weather {
  time: string
  wind_kmh: number
  gust_kmh: number
  precip_mm: number
  cloud_pct: number
  wx_code: number
  lightning_risk: string
}

export interface Advisory {
  /** The bulletin query completed. A completed check that found nothing is
   *  a different fact from a check that could not run. */
  available: boolean
  status: string
  sector: string
  source: string
  official_found: boolean
  /** Distance from our computed zone to the nearest published line, in km. */
  distance_km: number
  bearing_deg: number
  official_label: string
  official_state: string
  /** The day the advisory describes, which is not the day it was fetched. */
  bulletin: string | null
  checked_at: string
}

export interface PFZ {
  score: number
  lat: number
  lon: number
  distance_km: number
  sst_band: string
  anomaly_c: number
  confidence: string
  reasoning: string[]
}

export interface Hazard {
  kind: string
  severity: Severity
  value: string
  limit: string
  note: string
}

export interface Verdict {
  level: VerdictLevel
  severity: Severity
  hazards: Hazard[]
  rationale: string[]
}

export interface AgentRun {
  name: string
  label: string
  status: AgentStatus
  started: string
  ended: string
  summary: string
  sources: Citation[] | null
  err?: string
}

export interface Findings {
  plan: Plan
  geo: Geo
  marine: Marine
  weather: Weather
  advisory: Advisory
  pfz: PFZ
  verdict: Verdict
  trace: AgentRun[]
  answer: string
  answer_lang: string
  degraded: string[] | null
  prov: Citation[] | null
}

export interface LangOpt {
  code: string
  native: string
}

export interface Rules {
  wave_caution_m: number
  wave_critical_m: number
  gust_caution_kmh: number
  gust_critical_kmh: number
  tide_caution_m: number
  pfz_band_c: [number, number]
  anomaly_caution_c: number
}

export interface Meta {
  languages: LangOpt[]
  places: string[]
  llm: boolean
  rules: Rules
  sources: { name: string; use: string }[]
}

export interface Health {
  status: string
  uptime: string
  llm: boolean
  llm_degraded: boolean
  offline: boolean
  plan_model: string
  narrate_model: string
  languages: number
  places: number
  cache_entries: number
  snapshot_age: string
  verdict_source: string
}

export type StreamEvent =
  | { type: 'agent'; run: AgentRun }
  | { type: 'answer'; answer: string; answer_lang: string }
  | { type: 'done'; final: Findings; answer?: string; answer_lang?: string }
  | { type: 'error'; error: string }
