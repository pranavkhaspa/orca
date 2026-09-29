# ORCA — architecture

**SIH26176 · Marine EcOsystem Reasoning with Collaborative Agents**

ORCA answers one question from an artisanal fisher: *should I go out, and where?*
It is a multi-agent system whose safety verdict is computed by deterministic Go
code, never by a language model.

---

## 1. The problem this solves

Artisanal fishers on the Indian coast make a go/no-go decision several times a
week, with no access to the forecast and satellite products that an agency would
use. The decision depends on two very different kinds of knowledge:

- **Is it safe?** wave height, gusts, convection, tidal range.
- **Is it worth going?** where the water is warm enough and settled enough to
  fish, which is what a Potential Fishing Zone represents.

ORCA answers both, in the fisher's own language, with every number traceable to
a named source and a timestamp.

## 2. The one architectural rule

> **The language model never determines whether it is safe to go to sea.**

This is enforced structurally, not by convention:

| Concern | Owner | LLM involvement |
|---|---|---|
| Intent, place, time window, language | `PlannerAgent` | one optional call |
| Coastal geometry, waypoint projection | `GeoAgent` (Go) | none |
| Sea state and atmosphere | `OceanAgent`, `WeatherAgent` (Go) | none |
| Official corroboration | `IncoisAgent` (Go) | none |
| **Safety verdict** | **`engine.Assess` (pure Go)** | **none, ever** |
| Fishing-zone score | `engine.Evaluate` (pure Go) | none |
| Wording of the answer | `NarratorAgent` | one optional call |

`engine.Assess` takes observations in and returns a verdict out. It performs no
I/O, reads no clock, and holds no randomness. A test asserts that two hundred
consecutive calls with identical inputs return identical verdicts.

The model's only power is *wording*. It receives a fact sheet containing every
figure it is permitted to use and is instructed to invent nothing; the system
detects empty, oversized, or malformed output and falls back to a deterministic
template. Because the verdict is rendered as its own authoritative block in the
UI rather than being embedded in the prose, a narrator that rambles or
misphrases cannot alter the decision.

## 3. Agent graph

```
                    user query (any of 10 languages)
                                 │
                    ┌────────────▼────────────┐
                    │  PlannerAgent           │  deterministic router first,
                    │  place·window·intent·lang   model only refines
                    └────────────┬────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │  GeoAgent               │  coastal table → coordinates
                    │  → offshore waypoint    │  → bearing → 45 km offshore
                    └────────────┬────────────┘
                                 │
        ┌────────────────────────┼────────────────────────┐
        │                        │                        │
┌───────▼────────┐      ┌────────▼───────┐      ┌─────────▼────────┐
│ OceanAgent     │      │ WeatherAgent   │      │ IncoisAgent      │
│ wave·swell·SST │      │ wind·gust·rain │      │ corroboration    │
│ ·tide          │      │ ·cloud·WMO     │      │ (never a gate)   │
│ + PFZ fan-out  │      │                │      │                  │
└───────┬────────┘      └────────┬───────┘      └─────────┬────────┘
        │                        │                        │
        └────────────────────────┼────────────────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │  Risk Engine            │  worst case wins across
                    │  waves·wind·convection  │  five checks → one verdict
                    │  ·tide·zone-confidence  │  PURE FUNCTION
                    └────────────┬────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │  NarratorAgent          │  10-language template,
                    │                         │  model narration optional
                    └─────────────────────────┘
```

Ocean, Weather and INCOIS run **concurrently**. A panic in any one is contained
by a `recover`, recorded as a failed agent, and answered from the baked
snapshot — a single dead upstream degrades one input rather than the request.

## 4. Data sources

All keyless and free. No database, no API keys required to start.

| Source | Supplies | Endpoint |
|---|---|---|
| Open-Meteo Marine | wave height/period/direction, swell, SST, tide | `marine-api.open-meteo.com/v1/marine` |
| Open-Meteo Forecast | wind, gusts, precipitation, cloud, WMO code | `api.open-meteo.com/v1/forecast` |
| Open-Meteo Archive | seasonal SST baseline for the anomaly | `archive-api.open-meteo.com/v1/archive` |
| Open-Meteo Geocoding | place → coordinates (fallback) | `geocoding-api.open-meteo.com/v1/search` |
| INCOIS | official fishing-zone geometry, **corroboration only** | `geoserver.incois.gov.in/…/PFZ_Automation` |

Two details that are easy to get wrong and were verified against the live APIs:

- The tide variable is `sea_level_height_msl`. `sea_level_height` returns an
  invalid-value error from the model, and a marine forecast with no tide is not a
  marine forecast.
- Forecast windows are anchored on **the current hour**, not the first hour of
  the returned day. A question asked at 17:20 about "the next 6 hours" must
  describe 18:00–23:00. Aggregating from midnight silently reports the morning.

### The INCOIS bulletin

`erddap.incois.gov.in` is unreachable from the build environment and
`coastwatch.pfeg.noaa.gov` times out after 30 s, so neither is used.

The INCOIS advisory used to be read by scraping a home page that loads its
bulletin over XHR, so the served HTML contained no advisory text. It is now read
from the GeoServer instance the bulletin itself is published on, as an OGC Web
Feature Service:

```
https://geoserver.incois.gov.in/geoserver/PFZ_Automation/ows
  ?service=WFS&version=2.0.0&request=GetFeature
  &typeNames=PFZ_Automation:pfzlines
  &outputFormat=application/json&srsName=EPSG:4326
  &bbox=<minLon>,<minLat>,<maxLon>,<maxLat>
```

An earlier version of this document recorded the layer as
`incois.gov.in/geoserver/…`; the host is `geoserver.incois.gov.in`. The layer
returns `MultiLineString` geometry with `State_Name`, `SECTORBOUN`, `Julian_day`
and `Year`, so the advisory is measured rather than guessed.

Three things about that endpoint shape the implementation:

- **The full bulletin is ~1.9 MB and 113 features.** A `bbox` around the waypoint
  returns one to five features, so every request is a box query.
- **The box has to widen.** A tight box around a Porbandar waypoint contains no
  line at all, and reporting "INCOIS has no advisory here" when the nearest
  published sector is 40 km away is a false negative on a safety-adjacent
  feature. The search widens twice before giving up.
- **The answers are cached for 30 minutes.** The bulletin is republished once a
  day, and this is a public government endpoint that other tools also depend on.

The geometry is fetched in parallel with the marine and weather agents, but the
zone it is measured against does not exist until those agents return, so the
distance is recomputed locally against the computed PFZ point. Measuring against
the waypoint and calling the result "our zone" would misreport by however far
the zone was fanned — tens of kilometres.

The zone is a *fan cell* 85 km offshore, not the point the top-level sea state
is read at, so the two sets of numbers legitimately differ: the same Puri answer
reported 1.28 m waves at the coast and 0.52 m at the zone. Both figures are
therefore on the wire, under `pfz.sample_*`, because the "Where to fish"
sentences quote the cell and a figure a reader can see has to be one a client
can check. Before that was true, the section stated the *coastal* temperature
next to the *cell's* wave height; the evaluation suite caught it as a groundedness
failure, because the 35% of narratives it rejected were exactly the numbers the
API did not expose.

ORCA still computes its own fishing-zone score from SST and sea state; the
INCOIS geometry only corroborates it. INCOIS is attempted on every query and
reported honestly when it cannot corroborate — its absence never blocks a
verdict.

### Deliberately not used

`erddap.incois.gov.in` and `coastwatch.pfeg.noaa.gov` for the reasons above.
AIS vessel traffic and Global Fishing Watch effort, which both need an account
token and so would make a keyless demo key-dependent. Any chlorophyll product
that is not published by the authority that owns the fishery.

## 5. Resilience

The demo is the deliverable, so a dead upstream must not produce a dead demo.

| Failure | Behaviour |
|---|---|
| No `OPENROUTER_API_KEY` | deterministic router + 10-language templates; verdict unchanged |
| Model rate-limited / timeout / malformed | one retry, then the template |
| Open-Meteo unreachable | baked `snapshot.json` figures, labelled and timestamped |
| All upstreams down | snapshot within 400 km of the query, substitution shown |
| INCOIS unreachable | agent marked `skipped`; verdict unaffected |
| Place not resolvable | ask for a location rather than guessing a coast |
| `ORCA_OFFLINE=1` | suppress all outbound calls — the demo switch |

`ORCA_OFFLINE` deserves emphasis: it makes the resilience claim *demonstrable*
rather than merely asserted. Setting it during a presentation shows the system
continuing to answer from baked data, and it is what the test suite uses to
prove the guarantee holds in CI.

Every snapshot figure is stamped non-live centrally at load time, so no code path
can present baked data as live. The coastal table and snapshot are compiled into
the binary, removing any dependency on filesystem layout in deployment.

## 6. Languages

English, Hindi, Tamil, Telugu, Kannada, Malayalam, Gujarati, Odia, Bengali,
Marathi.

Detection is Unicode script-range analysis, not a model call: it sits at the
front of the pipeline, and the front of the pipeline must not be the step that
fails. Devanagari is disambiguated into Hindi or Marathi lexically.

Place names carry native-script and transliterated aliases (231 across 31
locations), so a Tamil question resolves through the deterministic router with
no model call at all. The verdict and its reasoning are localised; **numeric
values and thresholds are never translated**, because a mistranslated figure in
a safety message is worse than an untranslated one.

An alias table is not enough on its own. The same place is written many ways —
`కోచీ`, `కోచి`, `ಕೊಚ್ಚಿ`, `ಕೊಚಿ`, `कोची`, `कोच्ची` are all Kochi — and no table
can enumerate every transliteration a user might type. After exact matching
fails, a match is made on the **consonantal skeleton** of each word: vowel signs
in Indic scripts are combining marks, so dropping them and collapsing repeated
consonants reduces all six spellings above to the same key.

### The router has to read the language too

The router does more than find a place. It decides what the question is
**about** and how far ahead to look, which makes its vocabulary a safety
property rather than a typing convenience: a question it misreads is a
different question, answered from a different window. Its intent and time
keywords are therefore data covering all 10 languages, and
`TestPlanDoesNotDependOnTheLanguage` runs one question through all 10 and
compares the resulting plan.

This was not hypothetical. The table was English plus a few borrowed words, so
the same Puri question came back `intent=safety, window=30h` in English and
`intent=general, window=6h` in Odia, Malayalam, Kannada, Gujarati, Bengali and
Marathi. A 6-hour window stops short of the weather that made the call a
`caution`, so the English question was a `caution` and the Hindi one a `go`.
The answer still came back in the right language, so nothing looked wrong.

The table is also held to a spelling standard that catches the failure mode
that matters here. Every non-Latin term must occur in a sentence already in the
repository — the evaluation corpus or the interface examples — because a
misspelled Gujarati `સુરક્ಷિત` and a correct one are indistinguishable to
anyone who does not read the script, and the only symptom is that Gujarati
questions get planned as fishing questions. Tides, conditions and evening have
no verified sentence anywhere in the repository; those terms are a documented
gap rather than a claim being made.

Two details keep the place skeleton from becoming a source of false matches, and
both were found by the evaluation suite rather than by reading the code:

- **The skeleton is only used as a fallback.** Latin text keeps its vowels, so
  `Kochi` and `Koch` stay distinct, and an exact match is always preferred.
- **A match must start a word.** A bare substring search over the reduced query
  resolved a Telugu question about Kochi to **Diu**, because `ఉదయం` (morning)
  reduces to `ఉదయ`, which contains `దయ`, which is exactly what `దియు` (Diu)
  reduces to. No word in that sentence *begins* `దయ`. The same anchoring is
  what makes attached forms — `कोच्चीहून`, `கொச்சியில்` — still resolve.

## 7. Interfaces

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/healthz` | liveness, degraded mode, snapshot age, cache size |
| `GET` | `/api/meta` | languages, places, published thresholds, source list |
| `POST` | `/api/ask` | full `Findings` as JSON |
| `GET` | `/api/ask/stream` | the same as server-sent events, agent by agent |
| `GET` | `/api/eval` | the query suite as a scored report; `?live=1`, `?repeat=N` |

The stream emits one event per agent as it completes, then `answer`, then `done`
carrying the complete result, then `end`. The UI renders the trace as it arrives,
which is what makes the collaboration visible rather than asserted.

`done` is emitted exactly once. An earlier version drained a result channel when
the stream closed and produced two, each carrying the entire findings object; a
test now pins the count, because "the answer rendered twice" is a subtle failure
that only shows up on stage.

### Evaluation

`GET /api/eval` runs the query suite and returns a report. It is offline unless
`?live=1` is passed, so it is deterministic and runnable with no network. The
default corpus is 103 cases: every snapshot place in English, the same places in
the other nine languages, paraphrases and translations of a few questions that
must agree with each other, and a set of adversarial queries.

| Metric | What a failure means |
|---|---|
| `language_accuracy` | the answer came back in a different language than the question |
| `place_accuracy` | the router resolved the wrong location, or refused one it should not have |
| `answer_consistency` | two phrasings or translations of one question disagreed, or were not the same question to begin with |
| `groundedness` | the narrative contains a figure no observation produced |
| `clarification_accuracy` | an unanswerable question produced a verdict instead of a refusal |
| `injection_resistance` | an adversarial claim in the question was repeated back as fact |
| `determinism` | the same query produced a different answer on a repeat run |

Two of these metrics are easy to get wrong in a way that hides a defect, and
both were wrong here first.

`answer_consistency` has to distinguish a product bug from a broken test. A
verdict disagreement is a bug: one question, two answers. A *plan* disagreement
is not: the router read two different questions, so there was nothing to
compare. Reporting both as the same number points whoever reads the report at
the wrong component — the corpus had drifted exactly that way, with Puri's
English case asking about tomorrow morning and its Hindi and Odia cases naming
no time at all. A group whose members were planned differently now scores
nothing rather than partial credit, because members that happened to agree did
so by coincidence and an invalid test must not look like a passing one.

A metric that was not measured is not a metric that scored zero. Determinism
needs a repeat, so a run without one used to print a confident `0.0%` — the
worst-looking number in the report, for something that never ran. Unmeasured
metrics now carry a note, and a live run says so outright rather than implying
that the sea does not change between calls.

`groundedness` is the one that makes §2 falsifiable. It scans the narrative for
every number and requires each one to be a value some observation produced, to
the precision the answer chose to print. Two rules about that tolerance are
worth stating because getting them wrong hides real defects:

- A figure printed to one decimal may be within 0.05 of an observation; a figure
  printed as a whole number may be within 0.5. That is ordinary rounding, and
  wind speeds in particular are reported as whole numbers.
- A figure printed to one decimal must not pass as a rounded whole number. 3.8 m
  is not the correct rounding of the 4.0 m critical wave threshold, and an
  earlier version of this check accepted it — which would have let a narrator
  invent a wave height right up to the line that decides the verdict.

The suite found three real bugs while it was being written, all of which are now
regression tests: a Telugu question resolved to Diu because the town's Telugu
name occurs inside the word for "morning"; a Kannada and a Marathi spelling of
Kochi were not in the alias table, because exact-string matching cannot cover
transliteration variance; and the refusal metric was being fed place-resolution
failures, so a routing bug presented as a refusal bug.

`injection_resistance` exists because "answered the question" and "obeyed the
instruction" are different properties. Both injection cases name a real place,
and refusing them would be the wrong behaviour — a user is entitled to an answer
about Puri even while wrapping the question in an instruction. What is checked
is that the injected claim does not come back as fact.

## 8. Deployment

- **Backend** — Go 1.24, standard library only, zero third-party dependencies.
  Render web service, built from `api/Dockerfile` into a distroless image
  (16 MB). No database; the snapshot is embedded in the binary.
- **Frontend** — React 19 + TypeScript + Vite + MapLibre GL JS on Vercel, with
  `web/vercel.json` declaring the build and cache headers.
- **Configuration** — every setting has a working default. `PORT`,
  `OPENROUTER_API_KEY`, `ALLOWED_ORIGINS`, `ORCA_OFFLINE`, and the concurrency,
  rate-limit, timeout and search-geometry knobs. `api/.env.example` lists the
  complete set; it is the whole configuration surface.

A zero-dependency backend is a deliberate choice for a safety-adjacent service:
the supply chain is empty, the binary is self-contained, and there is no
dependency to be stale at demo time.

### Running it locally

```bash
# API — listens on :8080, no configuration required
cd api && go run ./cmd/orca

# Frontend — dev server on :5173, proxying /api to the API above
cd web && ORCA_API=http://localhost:8080 npm run dev
```

With the API in offline mode, the frontend answers entirely from the baked
snapshot:

```bash
cd api && ORCA_OFFLINE=1 go run ./cmd/orca
```

### Deploying

1. **Render.** Push the repository, create a Blueprint from `render.yaml`, and
   deploy. The service builds from `api/Dockerfile` and health-checks on
   `/healthz`. No secrets are required; add `OPENROUTER_API_KEY` as a secret only
   if you want model narration. Set `ALLOWED_ORIGINS` to the Vercel origin once
   it exists.
2. **Vercel.** Import the repository with the root directory set to `web`, add
   `VITE_API_BASE=https://<render-host>` as an environment variable, and deploy.
   Vite inlines the value at build time, so it must be set before the build.
3. **Verify.** `curl https://<render-host>/healthz` should report
   `"verdict_source":"deterministic Go (no model involvement)"`, and
   `curl "https://<render-host>/api/ask/stream?q=is+it+safe+off+Puri"` should
   emit seven `agent` events, one `answer`, one `done`, one `end`.

### Operating notes

- The free Render plan sleeps when idle and its container has one CPU. That is
  why `MAX_CONCURRENT` defaults to 6 and `RATE_LIMIT_PER_MIN` to 12: each query
  issues up to nine upstream calls — the marine fan, the forecast, the archive
  baseline, geocoding, and the INCOIS bulletin — and an uncapped service would
  exhaust the Open-Meteo quota from a single enthusiastic client.
- `PFZ_SEARCH_KM` is the cheapest lever if the upstream is slow. Lowering it
  cuts the fan from six marine calls to fewer; `0` pins the zone to the waypoint
  and still answers.
- `VITE_API_BASE` is a **build**-time variable, so it must be set in the host's
  build environment rather than at runtime. Unset means *same origin*, which is
  correct only behind a reverse proxy that forwards `/api`; without one the
  deployed site requests itself, gets HTML where it expected JSON, and shows an
  empty map with no error anywhere. `npm run dev` works with it unset because
  Vite does the forwarding, and a static build has nothing doing that.
- The CORS default is `*`. That is correct here: the API is keyless, holds no
  cookies, and exposes only public derived data. Tighten it once the frontend
  origin is fixed.
- Nothing in the request path writes to disk, so the service is safe on an
  ephemeral filesystem and horizontally scalable without coordination.

## 9. Layout

```
api/
  Dockerfile          multi-stage build into a distroless image
  .env.example        the complete configuration surface
  cmd/orca/           service entrypoint
  cmd/mksnapshot/      regenerates the baked fallback dataset
  internal/
    config/           environment parsing and the documented defaults
    domain/           shared contracts; citations enforced by the type system
    engine/           PURE marine reasoning — no I/O, no clock, no randomness
    data/             the only package that performs network I/O
    agents/           planner, geo, ocean, weather, incois, narrator, orchestrator
    lang/             detection, localisation, fallback templates
    llm/              the only package that calls a model
    httpapi/          HTTP, SSE, CORS, rate limiting
    data/files/       embedded coastal table + snapshot
web/                  React + Vite + MapLibre
  vercel.json         build config and cache headers
render.yaml           Render blueprint for the API
architecture.md       this file
INSTR.md              build instructions and the live phase log
ps.md                 the selected problem statement
```

## 10. Success criteria

1. A fisherman asks in his own language and gets a verdict, a zone, and reasons.
2. Every displayed figure carries a source and a timestamp.
3. The verdict is identical with the model enabled, disabled, or rate-limited.
4. With `ORCA_OFFLINE=1` the system still answers, labelled as snapshot data.
5. The reasoning is visible: six named agents, each with its own status.
6. The marine science is inspectable — thresholds are published by `/api/meta`
   and every finding states the rule it applied.

## 11. Honest limitations

- Offshore bearings are coarse reference values, not a routing solution. A*
  pathfinding was cut deliberately.
- Lightning is a **proxy** derived from WMO code, precipitation and cloud cover.
  Real lightning detection is not available from a keyless source, and the
  substitution is stated in the output rather than hidden.
- The PFZ score is ORCA's own product of an SST-band term and a sea-state term.
  It is not INCOIS's chlorophyll-and-SST product, and the response says so.
- Coastal coordinates are town/port centroids, not slipways.
- Forecast skill beyond roughly 48 hours is the provider's, not ours.
- The deterministic router's tide, sea-condition and evening vocabulary has no
  verified sentence in the repository, so a misspelling there would not be
  caught by the table test the other five categories are held to. A question
  about only those falls back to a shorter window, which is the safer of the
  two failure modes but is still a wrong window.
- The `INCOIS` GeoServer host does not resolve from every network. It was
  verified live while building this (Puri's zone 63.6 km from the published
  line, Kochi 13.9 km, Porbandar 31.3 km, Chennai 1.7 km) and has also been seen
  fail to resolve, at which point the advisory reports that it could not be
  reached and the verdict is unaffected. That is the designed behaviour, and it
  is also a reminder that the corroboration is a network dependency.
