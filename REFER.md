# ORCA — Technical Reference

The document the README points to. Everything here is checked against the
source; where a number appears, it is the number the code uses.

- **Project:** ORCA — Marine EcOsystem Reasoning with Collaborative Agents
- **Problem:** SIH26176, ISRO, Smart India Hackathon 2026
- **What it is:** a safety-and-fishing-zone advisor for India's coasts that
  answers in 10 Indian languages and never lets a language model decide
  whether it is safe to go out on the water.

---

## 1. Reading order

| If you want to… | Read |
|---|---|
| pitch in 60 seconds | README, then §2 here |
| understand the safety argument | §5, §15 |
| run it | §7, §8 |
| change the science | §5, §6 |
| add a language | §9 |
| deploy it | §11 |
| know what is broken | §14, `INSTR.md` |
| defend it in Q&A | §16 |

---

## 2. The problem, and the one idea that follows from it

The problem statement asks for a system that reasons over the marine
ecosystem and tells an ordinary person whether it is safe to fish. Two
requirements are in tension, and resolving them is the whole project:

1. **The answer must be in the user's language.** India does not have one
   maritime language. A Marathi-speaking fisher off Ratnagiri gets nothing
   usable from an English bulletin.
2. **The decision must not be made by a language model.** A model that
   hallucinates "conditions are calm" is not a safety system. It is worse
   than no system, because it is confident.

The resolution: **the model may change the words and nothing else.**

```
    language model                      Go standard library
    ---------------                      -------------------
    may rephrase                        must be right
    may be absent, offline              must never be
    may be wrong about wording          has no way to be wrong
                                        -------------------
                                        owns: place, time, sea state,
                                              zone, hazards, verdict
```

Every guarantee in this repository follows from that split. The model is wired
to two points only — refining the router's structured plan, and rewriting the
final narrative. Both are downstream of, or cosmetic over, a verdict that
already exists in code.

The second requirement is **agent collaboration**, which we satisfy literally
and visibly: seven named agents, three running concurrently, each streaming its
own status into the response, each visible in the browser trace. The
collaboration is not a diagram on a slide; it is in the SSE frames a judge can
watch arrive.

---

## 3. Agent graph

```
                    user query (any of 10 languages)
                                 |
                    +------------v------------+
                    |  PlannerAgent           |  deterministic router first,
                    |  place·window·intent·lang   model only refines
                    +------------+------------+
                                 |
                    +------------v------------+
                    |  GeoAgent               |  coastal table -> coordinates
                    |  -> offshore waypoint   |  -> bearing -> 45 km offshore
                    +------------+------------+
                                 |
        +------------------------+------------------------+
        |                        |                        |
+-------+--------+      +--------v-------+      +---------v--------+
| OceanAgent     |      | WeatherAgent   |      | IncoisAgent      |
| wave·swell·SST |      | wind·gust·rain |      | corroboration    |
| ·tide          |      | ·cloud·WMO     |      | (never a gate)   |
| + PFZ fan-out  |      |                |      |                  |
+-------+--------+      +--------+-------+      +---------+--------+
        |                        |                        |
        +------------------------+------------------------+
                                 |
                    +------------v------------+
                    |  Risk Engine            |  worst case wins across
                    |  waves·wind·convection  |  five checks -> one verdict
                    |  ·tide·zone-confidence  |  PURE FUNCTION
                    +------------+------------+
                                 |
                    +------------v------------+
                    |  NarratorAgent          |  10-language template,
                    |                         |  model narration optional
                    +-------------------------+
```

Ocean, Weather and INCOIS run **concurrently**. A panic in any one is contained
by a `recover`, recorded as a failed agent, and answered from the baked
snapshot — a single dead upstream degrades one input rather than the request.

### The seven agents

| Run ID | Display name | Responsibility | Can it change the verdict? |
|---|---|---|---|
| `planner` | Planner | place · window · intent · language | no — it only reads the question |
| `geo` | Geo Resolution | coastal name -> coordinates -> offshore waypoint | no |
| `ocean` | Ocean Analytics | wave, swell, SST, tide; PFZ fan-out | no |
| `weather` | Weather Intelligence | wind, gusts, rain, cloud, WMO code | no |
| `incois` | INCOIS Corroboration | official bulletin cross-check | **never** |
| `domain` | Risk Engine (deterministic) | the five hazard checks and the verdict | **this is the verdict** |
| `narrator` | Narrator | localized prose, optional model narration | no |

`internal/agents/orchestrator.go:112` onward. The trace lists agents in the
order they start; the concurrent trio is emitted as it completes.

---

## 4. Repository map

```
api/                          the service
  cmd/orca/                   the binary
  cmd/mksnapshot/             regenerates the embedded offline snapshot
  internal/engine/            the verdict - pure functions, no I/O  (639 lines)
  internal/agents/            the seven agents and the orchestrator (1669)
  internal/data/              Open-Meteo, INCOIS WFS, cache, snapshot (2311)
  internal/domain/            types and the JSON contract           (378)
  internal/lang/              detection, skeletons, templates       (1088)
  internal/eval/              the 108-case suite
  internal/httpapi/           REST + SSE                            (829)
  internal/llm/               OpenRouter client, optional           (385)
  internal/config/            the whole environment surface         (347)
  internal/data/files/        coastal_towns.json, snapshot.json
web/                          the interface (1805 lines TS/TSX)
scripts/e2e.mjs               44 browser checks, serves the bundle itself
architecture.md               design rationale
INSTR.md                      the 28-issue log
REFER.md                      this file
ps.md                         the problem statements
render.yaml                   the Render blueprint
```

Totals: **8,741 lines of Go** across 10 test files (**100 tests**), **3,163
lines** of TypeScript and TSX. **Zero third-party Go dependencies.**

---

## 5. The Risk Engine

`internal/engine/`. Pure functions. No I/O, no clock, no randomness, no
configuration read. This is what makes the determinism metric pass and what
makes the safety argument short.

### 5.1 Fishing-zone score

The score is a product of two terms, each in [0, 1].

**SST band term** — the band is **24–30 °C**, echoed by `/api/meta`:

```
bandTerm = 1.0                         if 24 <= sst <= 30
         = 1 - (distanceOutside / 3.0)    clamped to [0, 1]
```

3 °C of distance is the full decay. The band is the published product rule,
not a parameter tuned against the eval corpus.

**Sea-state term** — `internal/engine/pfz.go:68`:

```go
func SeaStateScore(waveH, swellPeriodS float64) float64 {
	s := 1.0
	if waveH > 1.0 {
		s -= (waveH - 1.0) * 0.18
	}
	if swellPeriodS > 0 && swellPeriodS < LongSwellPeriodS {   // 8.0 s
		s -= (LongSwellPeriodS - swellPeriodS) * 0.05
	}
	if s < 0 {
		return 0
	}
	return s
}
```

`score = bandTerm × seaStateTerm`. Constant: `LongSwellPeriodS = 8.0`.

### 5.2 Zone confidence

`internal/engine/pfz.go:145` — how much weight the zone reading deserves:

| Confidence | Condition |
|---|---|
| high | score ≥ 0.6 **and** −1.0 < anomaly < 1.5 °C |
| medium | score ≥ 0.3 **and** anomaly < 2.5 °C |
| low | otherwise |

The anomaly is the difference between the current SST and the 30-day baseline
from the Open-Meteo Archive endpoint.

### 5.3 The five hazard checks

| Check | Caution | Critical | Source fields |
|---|---|---|---|
| `waves` | ≥ 2.5 m | ≥ 4.0 m | `marine.wave_height_m` |
| `wind` | ≥ 40 km/h | ≥ 55 km/h | `weather.wind_gust_kmh` |
| `convection` | any convective signature | — | precip + cloud + WMO code |
| `tide` | ≥ 2.0 m | — | `marine.tide_m` |
| `zone` | low zone confidence | — | PFZ score and anomaly |

`internal/engine/hazard.go`. Severities are `ok`, `caution`, `critical`.

**Worst case wins** (`hazard.go:171`):

```
any critical  ->  level = "no-go",    severity = critical
any caution   ->  level = "caution",  severity = caution
otherwise     ->  level = "go",       severity = ok
```

Note what is *not* a hazard: a caution is never averaged against a quiet signal.
Two metres of swell and a low zone confidence cannot cancel out, because a
person going out on that basis would be exposed to both, and ORCA does not get
to choose which one they actually meet.

Every threshold here is returned by `GET /api/meta`, so any verdict can be
recomputed by hand from the same response.

### 5.4 Grounding the zone

`GeoAgent` resolves the coastal name to coordinates and projects a waypoint
**45 km** offshore along a reference bearing (`WAYPOINT_KM`). `OceanAgent`
fans a search grid over up to `PFZ_SEARCH_KM` (default **120 km**), and the
best cell becomes the zone.

The bearing table is **coarse reference data**, one bearing per coastal town.
That is a stated limitation (§14), not a routing solution: the position is that
a zone's distance and direction are what the user needs, and precision beyond
that is the job of a chart, not a chat answer.

### 5.5 Refusing a place that has no sea

`GeoAgent` resolves a name through the coastal table first and the geocoder
second, and a resolved place is only accepted if it is within
`MAX_COAST_DISTANCE_KM` (default **120 km**) of the coast. Beyond that the
result carries `geo.Source == "inland"` and the orchestrator answers with a
refusal in the reader's language instead of a verdict.

This is not hypothetical. `Hyderabad` geocodes cleanly to 17.384, 78.456 and is
**314 km** from the nearest water; `Jaipur` is 845 km out. Both are large
cities with large populations, both are plausible things to type into a marine
advisor, and neither has a fishing zone. Before this check they produced a
confident verdict computed from a waypoint projected 45 km offshore a bearing
they had no right to. `Kanyakumari` is the opposite case and is why the
distance test is the gate rather than a lookup: it failed to resolve at all
until it was added to the table, and a tool that refused a real fishing town
while answering for a landlocked city had the priorities exactly backwards.

---

## 6. Data

Four sources, all keyless, all with terms permitting use.

| Source | Used for | Timeout | Failure behaviour |
|---|---|---|---|
| Open-Meteo Marine | wave height, period, direction, swell, SST, tide | `UPSTREAM_TIMEOUT_SEC` (20 s) | snapshot for that place |
| Open-Meteo Forecast | wind, gusts, precipitation, cloud, WMO code | same | snapshot for that place |
| Open-Meteo Archive | 30-day SST baseline -> anomaly | same | anomaly unknown -> medium/low confidence |
| INCOIS | corroboration only | same | advisory marked unavailable, verdict unchanged |

### 6.1 INCOIS

```text
endpoint : https://geoserver.incois.gov.in/geoserver/PFZ_Automation/ows
layer    : PFZ_Automation:pfzlines
service  : WFS, GetFeature
srs      : EPSG:4326
bbox     : waypoint +- a margin wide enough for the search radius
output   : GML, parsed for LineString / MultiLineString
```

Design decisions, each of which was a bug first:

- **The geometry is kept, not just the nearest attribute.** Matching a line by
  its name and then reporting the distance from the *coast* produced answers
  wrong by tens of kilometres. The returned geometry is now measured locally,
  haversine, against the actual zone point. Verified distances: Puri 63.6 km,
  Kochi 13.9 km, Porbandar 31.3 km, Chennai 1.7 km.
- **The bbox is widened** past the search radius, because a line whose bounding
  box misses the query box is never returned even when the line itself passes
  close to the point.
- **Results are cached**, successes and negatives alike, for `CACHE_TTL_SEC`.
- **INCOIS cannot fail the request.** A DNS failure, a 502, a timeout or an
  unparseable body produces `advisory.available = false` with an explanatory
  `status`, and the answer says the bulletin could not be reached. The computed
  zone is ORCA's own and the response says so in as many words.

The host does not resolve from every network. When it does not, that is the
graceful path, and the suite exercises it.

### 6.2 The embedded snapshot

`api/internal/data/files/snapshot.json`, **16 places**, 120 KB, built by
`cmd/mksnapshot` and compiled into the binary with `embed`. The 16:

> chennai, kakinada, karwar, kavaratti, kochi, kolkata, kollam, mumbai,
> paradip, porbandar, port blair, rameswaram, ratnagiri, udupi, veraval,
> visakhapatnam

Regenerate with `cd api && go run ./cmd/mksnapshot`.

**Every figure that comes from the snapshot is marked non-live in the answer
itself**, in the reader's language. Serving stale numbers silently is the one
thing a safety product must not do, so the distinction is made in the
narrative and not only in the JSON.

### 6.3 Caching and concurrency

| Knob | Default | Effect |
|---|---|---|
| `CACHE_TTL_SEC` | 900 (15 min) | per place, per source family |
| `MAX_CONCURRENT` | 6 | bounds parallel upstream fetches |
| `UPSTREAM_TIMEOUT_SEC` | 20 | per upstream call |
| `REQUEST_TIMEOUT_SEC` | 60 | whole request |
| `LLM_TIMEOUT_SEC` | 25 | per model call |
| `RATE_LIMIT_PER_MIN` | 12 | per client IP, `/api/ask*` |

---

## 7. Configuration

The entire surface. Everything is optional; `PORT` has a default.

| Variable | Default | Meaning |
|---|---|---|
| `PORT` | `8080` | listen port |
| `ORCA_OFFLINE` | unset | `1` forbids all network calls; snapshot only |
| `OPENROUTER_API_KEY` | unset | enables model planning and narration |
| `PLAN_MODEL` | `deepseek/deepseek-v3.2` | router refinement model |
| `NARRATE_MODEL` | `deepseek/deepseek-v3.2` | narrative model |
| `OPENROUTER_BASE_URL` | `https://openrouter.ai/api/v1` | override for proxies |
| `LLM_TIMEOUT_SEC` | `25` | per model call |
| `UPSTREAM_TIMEOUT_SEC` | `20` | per data call |
| `REQUEST_TIMEOUT_SEC` | `60` | whole request |
| `CACHE_TTL_SEC` | `900` | data cache |
| `MARINE_DAYS` | `5` | forecast days requested |
| `WAYPOINT_KM` | `45` | offshore projection distance |
| `PFZ_SEARCH_KM` | `120` | zone search radius |
| `MAX_COAST_DISTANCE_KM` | `120` | a resolved place further inland than this is refused, not answered |
| `MAX_CONCURRENT` | `6` | parallel upstream cap |
| `RATE_LIMIT_PER_MIN` | `12` | per-IP limit |
| `ALLOWED_ORIGINS` | `*` | CORS origins |
| `COASTAL_PATH` | `embed:coastal_towns.json` | coastal table; also accepts a path |
| `SNAPSHOT_PATH` | `embed:snapshot.json` | offline snapshot; also accepts a path |

`COASTAL_PATH` and `SNAPSHOT_PATH` accept an `embed:` prefix or a filesystem
path, which is how the snapshot generator and the container build work without
a data volume.

**The system is required to be fully functional with all of these unset except
`PORT`.** `INSTR.md` states this as standing rule 1, and the eval suite runs in
exactly that configuration as its primary mode.

---

## 8. API

Five endpoints. `Content-Type: application/json` throughout. Errors carry a
short JSON object with a meaningful status: `{"error": "..."}` on the ask
endpoints, `{"answer": "..."}` on `/api/eval`.

### `GET /healthz`

Liveness plus a real status report, so a deployment can be checked for
degradation and not merely for liveness.

```json
{
  "status": "ok",
  "uptime": "7m18s",
  "places": 32,
  "languages": 10,
  "llm": false,
  "llm_degraded": true,
  "plan_model": "deepseek/deepseek-v3.2",
  "narrate_model": "deepseek/deepseek-v3.2",
  "offline": true,
  "cache_entries": 0,
  "snapshot_age": "4h0m0s",
  "verdict_source": "deterministic Go (no model involvement)"
}
```

`verdict_source` is a deliberate field: it states in the health check itself
that no model is involved in the verdict.

### `GET /api/meta`

Everything a client needs to explain or verify a verdict, plus the four data
sources and their use.

```bash
curl -s localhost:8080/api/meta | jq
```

```json
{
  "places": ["Alibag", "Car Nicobar", "Chennai", "..."],
  "towns": [
    {"name": "Veraval", "lat": 20.852, "lon": 70.367, "coast": "Arabian Sea"},
    {"name": "Kanyakumari", "lat": 8.088, "lon": 77.541, "coast": "Laccadive Sea"}
  ],
  "languages": [{"code": "en", "native": "English"}, {"code": "hi", "native": "हिन्दी"}],
  "rules": {
    "wave_caution_m": 2.5, "wave_critical_m": 4,
    "gust_caution_kmh": 40, "gust_critical_kmh": 55,
    "tide_caution_m": 2, "anomaly_caution_c": 1.5, "pfz_band_c": [24, 30]
  },
  "sources": [
    {"name": "Open-Meteo Marine", "use": "wave height, period, swell, sea-surface temperature, tide"},
    {"name": "Open-Meteo Forecast", "use": "wind, gusts, precipitation, cloud cover, weather code"},
    {"name": "Open-Meteo Archive", "use": "seasonal sea-surface temperature baseline"},
    {"name": "INCOIS", "use": "corroboration only — never a gate on the verdict"}
  ],
  "llm": false
}
```

`llm` reports whether a key is present. `rules` is the same struct the engine
reads, so client and server cannot disagree about a threshold.

36 places, 10 languages, 4 sources.

### `POST /api/ask`

```bash
curl -s localhost:8080/api/ask -H 'Content-Type: application/json' \
  -d '{"query":"Is it safe to fish off Kochi tomorrow morning?"}' | jq
```

Response shape — `internal/domain/types.go:192`:

| Field | Type | Notes |
|---|---|---|
| `plan` | object | `place`, `window_hours`, `intent`, `lang` |
| `geo` | object | coastal + waypoint coordinates, `distance_km`, `bearing_deg` |
| `marine` | object | time, wave height, period, swell, SST, tide |
| `weather` | object | wind, gusts, precipitation, cloud, WMO code |
| `advisory` | object | `available`, `official_found`, `distance_km`, `bearing_deg` |
| `pfz` | object | score, confidence, band, and the `sample_*` zone values |
| `verdict` | object | `level` (`go`/`caution`/`no-go`), `severity`, `hazards[]`, `rationale[]` |
| `trace` | array | one `AgentRun` per agent |
| `answer` | string | the localized narrative |
| `answer_lang` | string | BCP-47 code actually used |
| `degraded` | array | which inputs fell back to the snapshot |
| `prov` | array | flat, de-duplicated provenance for the whole response |

`pfz.sample_*` (`sample_sst_c`, `sample_wave_height_m`, `sample_swell_period_s`)
are the values from the **winning zone cell**, exposed because the narrative
asserts things about the zone. They are deliberately not the same numbers as
the coastal observation: the coast and an offshore fishing zone are different
places, and conflating them is how a localized answer came to assert that
30.4 °C was inside the 24–30 °C band.

**Every list is always an array, never `null`** — enforced by
`Findings.MarshalJSON` (`internal/domain/types.go:210`). A clarification
response, the one a user is most likely to need, has no hazards, no rationale,
no degradation and no provenance, and a Go nil slice marshals to `null` in all
four. The browser built the clarification correctly, received it, and crashed
iterating `null`. The fix is in the contract, not the client, because the API
is public and a JavaScript consumer should not have to guard every array to
survive a legitimate response.

Nested `Citations` maps serialize as `-` (hidden) to keep the payload
predictable; `prov` carries the same information flat and de-duplicated.

### `GET /api/ask/stream`

Server-sent events. **The query is a `q` query parameter, not a JSON body** —
`EventSource` cannot send a body, so the streaming endpoint is shaped to what
the browser API can actually do.

```bash
curl -sN "localhost:8080/api/ask/stream?q=Is%20it%20safe%20to%20fish%20off%20Kochi%20tomorrow%20morning%3F"
```

Observed event order, one request:

```
7 x  event: agent      one per agent, as it starts and as it completes
1 x  event: answer     the final localized narrative
1 x  event: done       the full findings payload
1 x  event: end        stream terminator
```

A clarification — a question ORCA cannot ground, like *"is it safe to go out
today?"* with no location — emits `agent`, `answer`, `done`, `end` and **no
verdict**. The reliable signal is the empty `verdict`: `level: ""`,
`severity: ""`, `hazards: []`, `rationale: []`, together with `plan.place: ""`
and a `trace` containing only the planner. A client can therefore distinguish
"no analysis was performed" from "the analysis found nothing wrong" without
inspecting prose.

Do **not** use `plan.planned` for this. It means "the model refined the plan",
so it is `false` for every request served by the deterministic router —
including successful ones. `false` is the normal, healthy case.

The browser renders the clarifying question and shows no verdict or hazard
panel, because there is no analysis behind one.

The interface cancels the previous stream when a new question is submitted, so
an in-flight request from a superseded question can never overwrite the
current answer on screen.

### `GET /api/eval` (also accepts POST)

```bash
curl -s 'localhost:8080/api/eval?repeat=3' | jq '.metrics'   # offline
curl -s 'localhost:8080/api/eval?live=1'  | jq '.passed'      # live upstreams
curl -s 'localhost:8080/api/eval?group=puri' | jq '.passed'   # one group
```

| Param | Values | Effect |
|---|---|---|
| `live` | `1` / `true` | hit real upstreams instead of the snapshot |
| `repeat` | 1–3 | run each case N times; out-of-range values clamp to 1 |
| `group` | case ID or group name | run only matching cases |

See §12. The report shape:

```json
{
  "mode": "offline",
  "started_at": "2026-09-29T…Z",
  "duration": "…",
  "cases": 108,
  "passed": true,
  "metrics": [
    {"name": "language_accuracy", "good": 108, "total": 108, "pct": 100,
     "why": "answer written in the language the query was asked in"},
    {"name": "determinism", "good": 108, "total": 108, "pct": 100,
     "why": "the same query produces the same verdict every time"}
  ]
}
```

`passed` is the top-level gate: a report is only passed when every metric is
100%. A metric that could not be measured carries a `note` instead of a `pct`.

---

## 9. Languages

10: English, Hindi, Marathi, Tamil, Telugu, Kannada, Malayalam, Gujarati,
Bengali, Odia. `internal/lang/`, 1,088 lines.

**Detection is script-first, not model-first.** Devanagari is ambiguous across
Hindi, Marathi and Nepali, so script alone is insufficient; the router scores
candidate languages by the vocabulary each one matches. A weighted scheme
replaced a set-intersection approach which, among other failures, treated the
word for "sea" as disambiguating — it is written identically in Hindi and
Marathi, so a Hindi question was answered in Marathi.

**Transliteration.** Romanised queries match against a consonantal-skeleton
form, so `kochi`, `Kochi` and misspelt variants all resolve; native-script
queries match directly. Matches are anchored at word starts so short
substrings do not produce hits.

**Numeric values are never translated.** The prose is localized; digits and
units are not. A mistranslated number in a safety message is worse than an
untranslated one.

**The engine's sea-state reasoning stays in English; the safety sentences are
fully localized.** Localizing the domain agent's own descriptive prose was
judged a worse trade than stating the rule with digits, because a bad
translation of the reasoning is a safety defect, while English reasoning with
localized numbers and localized conclusions is merely less elegant. Recorded
in `INSTR.md` as an accepted limitation, not hidden.

**Plan parity is safety-critical.** The router's vocabulary is the same in all
10 languages and `TestPlanDoesNotDependOnTheLanguage` enforces it. When it was
English-only, the same Puri question produced a 6-hour general request in Odia
against 30 hours in English — a window that stops short of the weather that
made the call. English said *caution*; Hindi said *go*. Both were correctly
localized, which is why nothing looked wrong. The evaluation suite found it,
not code review, and that is the argument for the suite.

Adding a language: templates in `internal/lang`, UI strings in
`web/src/i18n.ts`, and a matching set of verified sentences in the eval corpus.
The parity test will tell you if the router vocabulary is incomplete.

---

## 10. The interface

React 19, TypeScript 7, Vite 7, three 0.186 + globe.gl 2.46, MapLibre GL 5.6 as
a fallback, no UI framework. 3,163 lines.

- **Globe.** globe.gl draws a rotating sphere that flies to the place you asked
  about and colours the result by verdict. The ocean is generated at runtime in a
  `<canvas>` — a gradient and a graticule — rather than being fetched from a
  texture host, because an ad blocker in this project's own browser ate one CDN
  asset and a safety tool should not depend on a third party staying reachable.
  The land is real and equally self-hosted: Natural Earth 50m (public domain),
  converted to GeoJSON at build time by `web/scripts/build-geo.mjs` and
  committed to `web/public/geo/world.json` at 988 kB raw, 300 kB gzipped, with
  `labels.json` alongside it. The resolution is a build argument:
  `npm run build:geo -- 110m` for a smaller file, `10m` for a larger one. The
  default is 50m because 110m loses small peninsulas and 10m is several
  megabytes for detail the globe cannot show. The 36 supported ports are
  plotted on top of it, sourced from the same table the backend resolves
  against, so the picture shows real coverage instead of a decorative sphere.
  `vercel.json` excludes `/geo` from the SPA rewrite — the rewrite would
  otherwise answer the geometry request with `index.html`, which is the same
  failure as the CDN this avoids.
- **Map.** MapLibre remains as the 2D fallback, with ORCA's zone point, the
  offshore waypoint and the bearing between them. The base style loads from a
  CDN; if the style or the tiles are unavailable, the map degrades to a
  coordinate readout rather than a broken panel.
- **Both visualisations are lazy chunks behind an error boundary.** The verdict
  is text and must render on its own, because the person reading it may be on a
  boat on a 2G connection. A phone without WebGL gets the 2D chart; a rendering
  failure in either keeps the verdict on screen. The globe also probes for a
  live context before constructing, since a driver that refuses one can hand
  back a renderer that silently draws nothing.
- **Progressive streaming.** Agent events render as they arrive, so the
  collaboration is visible while it happens. The verdict panel appears only when
  a verdict exists.
- **A narrated wait.** Stages are named and ticked off as they complete, because
  a query takes long enough that a spinner reads as a hang.
- **A first-visit tour**, reopenable from the header, which takes arrow keys,
  closes on Escape and moves focus into itself.
- **Localized throughout**, including the language selector, verdict labels and
  provenance rows. Hero, tour and loading copy are translated into all 10
  languages.
- **Provenance visible.** Every figure on screen traces to its source and
  timestamp from the panel.
- **Zero console errors** is an enforced property of the e2e check, not an
  aspiration.

---

## 11. Deployment

### Backend — Render

`render.yaml` is a complete, committed blueprint. New -> Blueprint -> select
the repository. It builds `api/Dockerfile`, a distroless image of **16.2 MB**.

Set `OPENROUTER_API_KEY` in the Render dashboard if you want model narration.
It is optional; the verdict is identical without it, and the eval suite's
primary mode is the no-key one.

### Frontend — Vercel

Import the same repository, framework **Vite**. Set **`VITE_API_BASE`** to the
Render URL in the build environment.

`VITE_API_BASE` is a **build-time** variable — Vite inlines it at build. Set it
in the project's environment variables and redeploy; setting it in the deployed
container's runtime environment does nothing.

Leaving it unset means **same-origin**: the deployed page requests `/api/ask`
from its own host, which is not the API, so the map stays empty and the request
404s in the network tab with no error in the interface. Set the variable, or put
both behind one host with a reverse proxy. The client now also refuses to treat
a non-event-stream response as a live trace, because the same mistake otherwise
shows as a spinner that never finishes rather than as an error.

`web/vercel.json` carries the SPA rewrite so client-side routes resolve.

The remote is `git@github.com:pranavkhaspa/orca.git`; the API is deployed from
`render.yaml` and the interface from `web/`.

---

## 12. Evaluation and testing

### 12.1 The corpus

`internal/eval/`. **108 cases**, each with a query, an expected location or
refusal, an expected language, and expectations for consistency, clarification
and injection behaviour. The corpus covers all 10 languages, all 36 ports,
adversarial phrasings, and questions that name no place.

### 12.2 The seven metrics

| Metric | What 100% means |
|---|---|
| `language_accuracy` | the answer came back in the language asked in |
| `place_accuracy` | the right location, or an honest refusal |
| `answer_consistency` | the same question in 10 languages gets the same verdict |
| `groundedness` | **no number in the narrative that no observation produced** |
| `clarification_accuracy` | an unanswerable question refuses instead of guessing |
| `injection_resistance` | an instruction hidden in the question is not obeyed |
| `determinism` | the same query produces the same answer every time |

`groundedness` extracts every number in the narrative and requires each to
trace to a real observation or a published threshold. This is the metric that
caught the zone/coast SST band bug, and it is the one that matters most in a
safety product: a number in a safety message that no measurement produced is a
fabrication with a life attached.

### 12.3 Two rules about the metrics themselves

**A consistency group with an invalid plan gets no credit, not partial
credit.** If the router parsed one language's version of a question
differently, the group is testing the router, not consistency, and scoring it
as a consistency failure hides the real bug. The plan is reported per case, and
groups are compared on plan identity as well as verdict.

**An unmeasured metric is reported as unmeasured.** Live mode does not run the
determinism check, because each live case is a fresh network observation and
measuring its stability would measure the provider. It reports a note instead
of `0.0%`, because "we got this wrong" and "we did not test this" are different
findings and only one of them is a defect.

### 12.4 Current results

| Mode | Result |
|---|---|
| Offline, `repeat=3` | 108/108 cases; **all 7 metrics 100%**, determinism included (108/108) |
| Live upstreams | 108/108 cases; **all 6 measurable metrics 100%**; determinism not measured |
| Browser e2e | **44/44** against the production bundle |

### 12.5 Running everything

```bash
cd api
gofmt -l .                 # no output
go vet ./...
go test -race ./...        # 100 tests
go test -cover ./...

cd ../web
npx tsc --noEmit
npm run build
node ../scripts/e2e.mjs    # serves web/dist itself, 44 checks
```

`scripts/e2e.mjs` is deliberately not a unit test. It drives the real built
bundle in a real browser against the real service with no mocking, and its
assertions are about **what a person would see** — the rendered answer, the
verdict, the panel text, the console, the network — so a passing run means the
product works and not merely that a function returned a value. It serves
`web/dist` itself, so it is one command with nothing to start by hand.

---

## 13. Container

```bash
cd api
docker build -t orca:final .
docker run --rm -p 8080:8080 orca:final                    # live
docker run --rm -p 8080:8080 -e ORCA_OFFLINE=1 orca:final  # snapshot only
```

Distroless, non-root, **16.2 MB**. No shell, no package manager, no libc to
drift. The image is the deployment artefact for Render, and nothing else is
needed to run it.

---

## 14. Limitations

Stated plainly, because a project that overstates itself is easy to disprove
and impossible to trust.

**Scientific and geographic**

- Offshore bearings are coarse per-town reference values. Not a routing
  solution; A* pathfinding was cut deliberately as out of scope.
- Lightning is a **proxy** derived from WMO weather code, precipitation and
  cloud cover. There is no keyless lightning source. The substitution is stated
  in the output rather than presented as detection.
- The PFZ score is ORCA's own SST x sea-state product. It is **not** INCOIS's
  chlorophyll-and-SST product, and the response says so in those terms.
- Coastal coordinates are town and port centroids, not slipways.
- Forecast skill beyond roughly 48 hours is the provider's, not ours.

**Router**

- The tide, sea-condition and evening vocabulary has no verified sentence in the
  repository, so it is not covered by the spelling test the other five
  categories have. A question hitting only those terms falls back to a shorter
  window.

**Architecture**

- The marine-engine prose is not localized (§9). Localized numbers, localized
  conclusions, English reasoning.
- INCOIS search: the initial WFS query is waypoint-centred with a 90 km data
  radius while `PFZ_SEARCH_KM` defaults to 120 km, and only the initially
  winning line is retained for final remeasurement. In a rare case the nearest
  official line may be missed, or a retained line replaced. The advisory is
  corroboration and cannot change the verdict, so the consequence is a slightly
  less accurate cross-check, never an unsafe call.
- Transient INCOIS errors are cached like successful negatives.

**Not implemented**

- AIS. No free source; commercial providers only. Not simulated, not faked.
- Global Fishing Watch. An account and API key are available at no cost, and a
  real integration, but out of scope for this build.
- Push notifications, saved locations, multi-day planning.
- The configured OpenRouter model slug is not live-verified, since no key has
  been available in this environment.

---

## 15. Invariants

These are the properties the rest of the system exists to protect. Each has a
test.

1. **The verdict is computed by a pure function.** No I/O, no clock, no
   randomness, no config.
2. **The model cannot change the verdict.** Its only two attachment points are
   plan refinement and narrative rewriting. Removing `OPENROUTER_API_KEY`
   changes the wording, never the level.
3. **Every list in the wire format is an array.** `Findings.MarshalJSON`.
4. **Every number in a narrative is grounded.** Enforced by `groundedness`.
5. **The plan is language-independent.**
   `TestPlanDoesNotDependOnTheLanguage`.
6. **The system is fully functional with no key and no network.**
   `ORCA_OFFLINE=1` is the suite's primary mode.
7. **INCOIS never gates the verdict.** A dead bulletin degrades one field.
8. **Localization cannot alter the analysis.** Same query, same verdict, in all
   10 languages.

---

## 16. Use cases

**Who this is for, concretely:**

1. **The small-boat fisher before leaving.** The primary user. Asks "is it safe
   to go out off Mangaluru this evening?" in Malayalam and gets a verdict, the
   numbers behind it, and the zone to aim for. Replaces a phone call to someone
   who may not know either.
2. **The harbour master or panchayat notice board.** The same answer in ten
   languages for display, no account, no app install.
3. **A coastal school or training centre.** Runs `ORCA_OFFLINE=1` on a laptop
   with no internet and teaches how the thresholds produce the verdict, because
   `/api/meta` publishes them.
4. **A fisheries department officer.** Compares ORCA's computed zone against
   the INCOIS bulletin and sees the measured distance between them, rather than
   trusting either.
5. **A journalist or NGO writing about a closure.** Every number in an ORCA
   answer is traceable to a source and a timestamp, so it can be checked rather
   than retyped.

**What it deliberately is not for:** navigation, route planning, or anything
requiring a chart, a tide table for a specific berth, or live vessel traffic.
Those are chart and AIS products, and ORCA says so rather than improvising.

---

## 17. Demo script

Roughly five minutes. Have the browser open with a built bundle and the service
running; the offline mode is safer to present on.

1. **Ask in the user's language, not yours.** Type
   `कल सुबह कोच्चि से मछली पकड़ना सुरक्षित है?` — watch the language selector
   move to हिन्दी on its own as you type. Point out that no model was asked
   which language this is.
2. **Watch the agents work.** The trace fills in as `planner`, `geo`, then the
   concurrent `ocean` / `weather` / `incois`, then `domain`, then `narrator`.
   Say the word "concurrent" while three arrive together.
3. **Show the verdict and the numbers behind it.** Point at the wave height
   and gusts in the panel, then at the same numbers in the provenance rows.
4. **Change one input and show it is not the model.** `curl /api/meta | jq
   .rules`, and restate the thresholds. Ask whether they would trust a chatbot
   to apply those five numbers. That is the pitch.
5. **Ask something impossible.** `is it safe to go out today?` — no location.
   ORCA asks a clarifying question and shows **no verdict**. Say: *the dangerous
   failure is a confident answer to a question nobody asked.*
6. **Ask the same question in three more languages.** Same verdict, different
   script. This is the §9 bug: it used to differ, and the eval suite found it.
7. **Kill the network.** Present offline mode, or stop the service and restart
   with `ORCA_OFFLINE=1`. The answer still comes, and the answer says the
   figures are from the snapshot.
8. **Run the tests in front of them, if there is time.**
   `node scripts/e2e.mjs` — 44 checks against the real bundle, ending in a
   green line.

Backup demo if the venue network is hostile: everything above works with
`ORCA_OFFLINE=1` except the live INCOIS corroboration, which degrades to an
honest "bulletin unreachable". That degradation *is* part of the demo.

---

## 18. Anticipated questions

**"Isn't a deterministic rule too simplistic? Real fishing is complex."**
Yes, and the claim is not that it is complete — it is that it is right. A
safety verdict needs a defensible failure mode. ORCA's is "refuse and explain";
a model's is "confidently invent a swell height". The thresholds are published
so a domain expert can argue with them, and the zone score is a published
product, not a black box.

**"Why not just use a bigger model?"**
A bigger model is still not a safety instrument. The model here is used where a
model is genuinely good — rewriting prose for ten languages — and excluded where
correctness is non-negotiable. The argument is not that models are useless; it
is that their failure mode is silent.

**"How do you know it works in Odia if you tested it in English?"**
The eval corpus is 108 cases across all 10 languages and all 36 places, and
`answer_consistency` requires the same verdict across every language of the same
question. A language-specific failure in the router produces a metric failure,
not a silent divergence. That metric is how the Puri bug was caught.

**"What if the marine data is wrong?"**
Then ORCA is wrong, and it is honest about its inputs: every number carries its
source and timestamp, the snapshot path is labelled in the answer text, and
INCOIS is shown alongside as an independent cross-check with the measured
distance between the two. It cannot detect bad data it has never been shown.

**"Is it live?"**
When it can be, yes, and the answer says which. When it cannot, the answer says
that too, in the reader's language. There is no third mode.

**"Why no AIS?"**
No free source exists. Commercial AIS is a paid product, and faking a vessel
position would be exactly the kind of confident invention this project exists to
avoid. It is absent rather than simulated, and that is listed as a limitation.

**"You reviewed other projects — did you use any?"**
No. `REFER.md` §19. None carried a licence permitting reuse, so none of that
code is here. Every technique is implemented from published specifications.

**"How would this scale to all of India's coastline?"**
The snapshot is the only place-bound artefact and is a build-time input. Adding
coastal towns is a data edit, not a code change; adding a language is a
vocabulary edit plus verified eval sentences. The engine is O(1) in coastline
length.

---

## 19. Prior art, licensing, and attribution

Public fishing-zone and marine-advisory projects were reviewed during
development. **None carried a licence permitting reuse of their code**, so none
of it is present in this repository. What was taken from the field is public
domain knowledge: the INCOIS bulletin format, the OGC WFS specification, the
Open-Meteo parameter names, and the published SST band rule. Every line here is
original.

Attribution retained in the source table and in `/api/meta`:

- **Open-Meteo** — free, no key, CC BY 4.0.
- **INCOIS** — Government of India public bulletin, used as corroboration.
- **MapLibre GL** — BSD-3.

If any reviewer believes a specific technique here derives from prior art, that
is a conversation worth having with the licence text in hand; the absence of
code reuse is a deliberate, checkable property, not an accident.

---

## 20. Security and privacy

- **No secrets in the repository.** `.env` is ignored. `.env.example` files
  list variable names with no values. Nothing committed contains a key.
- **No user accounts, no database, no tracking, no analytics.** A query is held
  in memory for the length of one request and is not logged with its content.
- **No PII.** Locations are public coastal town names. The rate limit is per
  IP and in-memory only.
- **The rate limit** is `RATE_LIMIT_PER_MIN`, per client IP, on `/api/ask*`.
  It protects the public free API from a single client and from a naive loop. It
  is not a DDoS defence; put a real one in front of it for a public launch.
- **CORS** defaults to `*` for convenience in a demo. Set `ALLOWED_ORIGINS` to
  the deployed frontend origin before a public launch.
- **The model is optional and its absence changes nothing**, which is also a
  privacy property: with no key, no query text ever leaves the machine.
- **Upstream queries are location only.** A place name and a coordinate go to
  Open-Meteo and INCOIS. Free-text questions go nowhere unless
  `OPENROUTER_API_KEY` is set, and even then only to OpenRouter for narration.

---

## 21. Operations

**Health.** `GET /healthz`. Degradation is visible per request in
`degraded[]` and in the answer text, so "it is working but one source is down"
is distinguishable from "it is working".

**Regenerating the snapshot.** `cd api && go run ./cmd/mksnapshot`, then
rebuild. The snapshot is a build input; it is not fetched at runtime.

**When INCOIS stops resolving**, the symptom is
`advisory.available = false` with a status naming the failure, and an answer
saying the bulletin could not be reached. This is expected, non-fatal, and
exercised by the suite. It requires no code change and no redeploy.

**When a source starts returning garbage**, the containment path is
`ORCA_OFFLINE=1` plus a snapshot refresh. The service keeps answering with
labelled data while the upstream is investigated.

**Changing a threshold** means editing `internal/engine`, and the new value
flows to clients through `/api/meta` automatically, since the endpoint returns
the same struct the engine reads.

---

## 22. Quick index

| To change… | Go to |
|---|---|
| a hazard threshold or the zone score | `api/internal/engine/hazard.go`, `pfz.go` |
| the agent graph or concurrency | `api/internal/agents/orchestrator.go` |
| router vocabulary or language detection | `api/internal/agents/planner.go`, `api/internal/lang/` |
| the JSON contract | `api/internal/domain/types.go` |
| any upstream, the cache, or INCOIS | `api/internal/data/` |
| the narrative text | `api/internal/lang/`, `api/internal/agents/narrator.go` |
| a new endpoint | `api/internal/httpapi/server.go` |
| eval cases and metrics | `api/internal/eval/eval.go` |
| the interface | `web/src/` |
| coastal towns | `api/internal/data/files/coastal_towns.json` |
| deployment | `render.yaml`, `api/Dockerfile`, `web/vercel.json` |

---

## Closing note

The interesting part of this repository is not that it returns a verdict. It is
`INSTR.md`: 24 logged issues, each with what it broke, why it was hard to see,
and the test that now prevents it. Four of those bugs would have produced an
answer that looked completely correct, in the right language, with the right
kind of numbers, and told someone the wrong thing about the sea.

That is the standard the project is built to: not "it works", but "here is the
evidence that it works, and here is the list of the ways it used not to."
