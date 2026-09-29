# ORCA

**Marine safety and fishing-zone advisor for India's coastal states.**
Ask in your own language, get a verdict you can check.

> **ORCA** — Marine **Ec**o**O**system **R**easoning with **C**ollaborative **A**gents
> ISRO · Smart India Hackathon 2026 · Problem SIH26176

---

## The one-paragraph version

You ask *"कल सुबह कोच्चि से मछली पकड़ना सुरक्षित है?"* — ORCA works out which
place you mean, reads the live sea state there, scores a fishing zone, checks
five hazards, and answers in your language with a **go / caution / no-go** and a
reason. The verdict is computed by pure Go code, not by a language model. Every
number in the answer carries its source and timestamp. If the network is down,
ORCA still answers from a baked snapshot and says so.

---

## Try it in 30 seconds

```bash
# 1. the engine (no key, no network needed)
cd api && go run ./cmd/orca
#    -> listening on :8080

# 2. ask something
curl -s localhost:8080/api/ask -H 'Content-Type: application/json' \
  -d '{"query":"Is it safe to fish off Kochi tomorrow morning?"}' | jq .verdict

# 3. or read the questions it can answer
curl -s localhost:8080/api/meta | jq .places
```

```bash
# the interface, in a second terminal
cd web && npm install && npm run dev     # http://localhost:5173
```

No API key. No account. No signup.

---

## Why it exists

A fisherman in Kerala, Odia or Bengali does not read English weather
bulletins, and a forecast app that answers in English is not usable to them.
Meanwhile the decision — *is the sea safe today, and where should I go* — is
genuinely dangerous to get wrong, and "ask a chatbot" is not a safety system.

ORCA takes both halves seriously:

| | |
|---|---|
| **Answers in 10 Indian languages** | English, Hindi, Marathi, Tamil, Telugu, Kannada, Malayalam, Gujarati, Bengali, Odia — detected from the script, not guessed by a model |
| **Decides with pure code** | the safety verdict is a Go function with no I/O, no clock and no randomness. A model may reword the answer. It cannot change the verdict |
| **Shows its work** | thresholds are published via `/api/meta`; every figure carries a source and a timestamp; the agent trace is streamed to the browser |
| **Never blocks** | every upstream is optional. No network, no model key, no INCOIS — it still answers, and says what it used |
| **Refuses rather than guesses** | a place it cannot resolve, and a place with no sea in it, both get a question back instead of a verdict. Hyderabad geocodes fine and is 314 km inland; a marine forecast for it is a confident wrong answer |

---

## What it actually does

```
 user query (any of 10 languages)
            │
   ┌────────▼────────┐
   │  Planner        │  place · time window · intent · language
   └────────┬────────┘
   ┌────────▼────────┐
   │  Geo            │  coastal table → coordinates → a waypoint 45 km offshore
   └────────┬────────┘
   ┌─────────┬─────────┬──────────┐
   │ Ocean   │ Weather │  INCOIS  │   ← concurrent
   └─────────┴─────────┴──────────┘
            │
   ┌────────▼────────┐
   │  Risk Engine    │  5 hazard checks → go / caution / no-go
   │  PURE FUNCTION  │  no I/O · no clock · no randomness
   └────────┬────────┘
   ┌────────▼────────┐
   │  Narrator       │  10-language template, model narration optional
   └─────────────────┘
```

**Seven named agents** run per query and stream their status to the browser as
they finish, so the collaboration is visible rather than claimed.

### The interface

A rotating globe opens the page, and the place you ask about is where it goes.
The sphere is generated in a runtime `<canvas>` — an ocean gradient and a
graticule — rather than fetched from a texture host, and the 32 supported ports
are plotted on it, so the surface shows the coastline ORCA actually covers
instead of a decorative globe. Nothing about the picture is invented: it comes
from the same table the backend resolves against.

The wait is narrated rather than spun. A query fans out across several upstream
services and a model, which is long enough that a spinner reads as a hang, so
each stage is named, the completed ones are ticked, and the progress bar moves.

A short tour appears once, on a first visit, and stays reachable from the header.
It takes arrow keys, closes on Escape, and moves focus into itself.

Both are progressive enhancements that fail to nothing. The globe is a lazy
chunk behind an error boundary with a 2D chart behind it, so a phone without
WebGL still gets the answer. A misconfigured API base is reported as an error
rather than left as a spinner that never finishes.

### The marine science, in full

**Fishing-zone score** (0–1), a product of two terms:

- **SST band term** — sea surface temperature inside **24–30 °C** scores 1.0.
  Outside the band it decays linearly to 0 at **3 °C** of distance. Long-term
  product is tied to this band; it is the published rule, not a tuned guess.
- **Sea-state term** — starts at 1.0, loses 0.18 per metre of significant wave
  height above 1.0 m, and loses a further 0.05 per second that a swell period
  falls below the 8 s "settled swell" line.

Confidence in the zone reading: **high** at score ≥ 0.6 with a 30-day SST
anomaly between −1.0 and +1.5 °C; **medium** at score ≥ 0.3 with anomaly under
2.5 °C; otherwise **low**.

**Five hazard checks.** The worst one wins.

| Check | Caution | Critical |
|---|---|---|
| Waves (significant wave height) | ≥ 2.5 m | ≥ 4.0 m |
| Wind (gusts) | ≥ 40 km/h (Beaufort 6–7) | ≥ 55 km/h (Beaufort 9) |
| Convection (precip + cloud + WMO code) | any convective signature | — |
| Tide | ≥ 2.0 m | — |
| Zone confidence | low confidence in the PFZ | — |

Any critical → **no-go**. Any caution → **caution**. Otherwise **go**. All
thresholds are returned by `GET /api/meta`, so a reader can verify any verdict
by hand.

---

## Where the data comes from

Four keyless public sources, all with terms that permit use:

| Source | Used for | Key required |
|---|---|---|
| **Open-Meteo Marine** | wave height, period, direction, swell, sea-surface temperature, tide | no |
| **Open-Meteo Forecast** | wind, gusts, precipitation, cloud cover, WMO weather code | no |
| **Open-Meteo Archive** | 30-day sea-surface-temperature baseline, for the anomaly | no |
| **INCOIS** (India's National Centre for Ocean Information Services) | corroboration only — the official fishing-zone bulletin, read as OGC WFS geometry | no |

INCOIS **never** gates a verdict. ORCA computes its own zone and reports the
official zone alongside it as a cross-check, with the measured distance between
them. If INCOIS is unreachable, the advisory says so and the verdict is
unaffected.

---

## Try breaking it

The most interesting part of this project is that it is graded by evidence.

```bash
# 108 cases, 7 metrics, against the real orchestrator
curl -s 'localhost:8080/api/eval?repeat=3' | jq '.metrics'

# the same against live upstreams
curl -s 'localhost:8080/api/eval?live=1' | jq '.passed'
```

| Metric | 100% means |
|---|---|
| `language_accuracy` | the answer came back in the language the question was asked in |
| `place_accuracy` | the right location, or an honest refusal |
| `answer_consistency` | the same question in 10 languages gets the same verdict |
| `groundedness` | **no number in the narrative that no observation produced** |
| `clarification_accuracy` | an unanswerable question refuses instead of guessing |
| `injection_resistance` | an instruction hidden in the question is not obeyed |
| `determinism` | the same query produces the same answer every time |

And a real browser test, 44 checks against the production bundle:

```bash
cd web && npm run build
node scripts/e2e.mjs    # serves web/dist itself; 44 checks over 6 languages
```

---

## Twenty-eight bugs, and why the log is in the repository

`INSTR.md` records every defect found while building this, what it broke, and
the test that now pins it. It is in the repository because a project that
claims to be trustworthy should be able to show its work. Four of them changed
the design:

- **A localized answer claimed the water was inside the 24–30 °C band at
  30.4 °C.** A safety sentence that asserts a false reading is precisely the
  failure this project exists to prevent. The renderer now branches on the
  measured value, and the band sentence is written from the *zone's* reading,
  not the coast's.
- **A Hindi question was answered in Marathi**, because the disambiguator was
  a list of words spelled identically in both languages — including the word
  for "sea". Replaced with weighted scoring.
- **The official advisory was measured from the coast** and reported as if it
  were the distance to the computed zone — off by tens of kilometres. The
  bulletin geometry is now retained and re-measured locally against the actual
  zone point.
- **Asking in a non-English language changed the analysis, not just the
  wording.** The router's intent and time keywords were English, so the same
  Puri question became a 6-hour general request in Odia against 30 hours in
  English — and a 6-hour window stops short of the weather that made the call.
  English said *caution*; Hindi said *go*. Nothing looked wrong, because the
  answer still came back in the right language.

The last one was found by the evaluation suite, not by reading the code. That is
the reason the suite exists.

---

## Design choices worth arguing about

**Zero third-party Go dependencies.** The runtime image is **16 MB** on a
distroless base, and there is no dependency to go stale on stage. Everything is
the standard library, which is why it builds and runs anywhere Go does.

**The snapshot is embedded and labelled.** 16 places are baked into the binary
at build time. Serving stale numbers silently would be dishonest, so every
figure from the snapshot is marked non-live *in the answer itself*, in the
reader's language.

**Numeric values are never translated.** The prose is localised; the digits and
units are not. A mistranslated number in a safety message is worse than an
untranslated one.

**Latency is bounded everywhere.** Upstreams time out, fan-outs are capped,
and a panic in any one agent is contained and answered from the snapshot. One
dead upstream degrades one input, not the request.

---

## Honest limitations

Stated here so nobody is surprised on stage:

- Offshore bearings are coarse reference values, not a routing solution. A*
  pathfinding was cut deliberately.
- Lightning is a **proxy** from WMO weather code, precipitation and cloud
  cover. Real lightning detection has no keyless source. The substitution is
  stated in the output.
- The PFZ score is ORCA's own SST × sea-state product. It is **not** INCOIS's
  chlorophyll-and-SST product, and the response says so.
- Coastal coordinates are town/port centroids, not slipways.
- Forecast skill beyond ~48 hours is the provider's, not ours.
- The router's tide, sea-condition and evening vocabulary has no verified
  sentence in the repository, so it is not covered by the same spelling test as
  the other five categories. A question hitting only those falls back to a
  shorter window.
- Non-English answers use localized templates for the engine's sea-state
  reasoning. Numbers and the band sentence are in every language; the full
  rationale is fully localized for safety findings. Translating engine prose
  was judged a worse trade than stating the rule with digits.

---

## Deploy it

Backend is a Docker image; frontend is a static Vite build.

**Render** — `render.yaml` in this repository is a complete blueprint. New →
Blueprint → pick the repo. Set `OPENROUTER_API_KEY` if you want model narration
(optional; the verdict is unaffected).

**Vercel** — import the same repo, framework Vite. Set **`VITE_API_BASE`** to
the Render URL in the build environment. It is a build-time variable: unset
means same-origin, so the deployed site requests itself and shows an empty map
with no error anywhere.

**Locally, everything offline:**

```bash
cd api && ORCA_OFFLINE=1 go run ./cmd/orca   # snapshot only, no network calls
```

---

## Repository layout

```
api/                    the service — Go, standard library only
  internal/engine/      the verdict. pure functions, no I/O
  internal/agents/      the seven agents and the orchestrator
  internal/data/        Open-Meteo, INCOIS WFS, cache, embedded snapshot
  internal/domain/      types, and the JSON contract
  internal/lang/        10-language detection, templates, i18n
  internal/eval/        the 108-case suite
  internal/httpapi/     REST + SSE
  internal/config/      the entire environment surface
web/                    the interface — React 19, TypeScript, MapLibre, Vite
scripts/e2e.mjs         44 browser checks against the production bundle
architecture.md         the design and its reasoning
INSTR.md                the 24-issue log
REFER.md                the full technical reference, for the team
ps.md                   the problem statements this was built against
render.yaml             the complete Render blueprint
```

## Licencing and attribution

This code is original. Marine data is Open-Meteo (free, no key required, CC BY
4.0 attribution retained in the source table). INCOIS data is the Government
of India's public bulletin. Public fishing-zone projects were reviewed for
prior art during development; **none carried a licence permitting reuse of
their code**, so none of it is present here. Every technique in ORCA is
implemented from the published specifications.
