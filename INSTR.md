# INSTR.md — Build Instructions & Live Phase Log

> **This file is edited by the implementing agent as work proceeds.** It is the single source of truth for build status. Every phase transition updates the Status table, appends to the Decision Log, and records what was actually verified. Do not let it drift from reality.

## How to maintain this file

1. At the **start** of a phase, set its status to `IN PROGRESS`.
2. At the **end** of a phase, set status to `DONE`, and fill in the *Actual outcome* with what really happened — including failures, surprises, and anything discovered that contradicts `architecture.md`.
3. Any change to scope, architecture, or data sources gets an entry in the **Decision Log**.
4. Any bug found in a later phase gets an entry in **Known Issues** with a resolution.
5. Re-estimate remaining phase budgets whenever reality diverges. The budget is a plan, not a promise.

**Rule:** if reality and this file disagree, reality wins — update the file immediately.

---

## Constraint envelope

| Constraint | Value |
|---|---|
| Submission deadline | ~24h from build start (30 Sep 2026) |
| Working build time | ~10 hours |
| Team size | 1 (solo, AI-assisted) |
| Money | Free tiers only. Optional one-time $10 OpenRouter top-up. |
| Hosting | Render (backend) + Vercel (frontend) — no alternatives available |
| Models | Mid-tier frontier via API, human-directed |

**Derived policy:** build for demo resilience over feature breadth. Anything that cannot be verified working before the deadline is cut, not attempted-and-broken. A narrow system that works beats a wide one that does not.

---

## Phase status

| Phase | Scope | Budget | Status | Actual outcome |
|---|---|---|---|---|
| **P0** | Data reconnaissance | 20m | ✅ DONE | 8 sources probed. **4 verified working, 2 dead (ERDDAP hosts).** Rejected variable name caught. PFZ scraping abandoned. |
| **P1** | Repo scaffold, Go module, config, coastal seed data | 25m | ✅ DONE | Module `orca`, zero third-party dependencies, all config env-driven with documented defaults. Coastal table grew to 32 locations (Kanyakumari added in I26) with native-script and transliterated aliases, plus the consonant-skeleton fallback added later (I20). |
| **P2** | Data layer: marine/forecast/archive/geocode + cache + snapshot fallback | 60m | ✅ DONE | All four Open-Meteo endpoints live and verified. TTL cache. 16-place snapshot baked into the binary, non-live stamped centrally at load. `ORCA_OFFLINE` switch added so resilience is demonstrable, not just asserted. |
| **P3** | Domain engine: PFZ score, SST anomaly, hazard rules, advisory | 50m | ✅ DONE | Pure `Evaluate`/`Assess` — no I/O, no clock, no randomness. 24–30 °C band, 3 °C falloff, 6-point fan plus the waypoint. Four hazard checks, worst case wins. Thresholds published through `/api/meta`. |
| **P4** | Agent runtime: planner, parallel fan-out, trace, SSE | 50m | ✅ DONE | Six named agents plus the narrator. Ocean/Weather/INCOIS fan out concurrently with per-agent `recover`. Seven-entry trace carried through to the UI. INCOIS later became a measured WFS geometry query with a 30-minute cache (I15). |
| **P5** | Narrator agent: LLM + multilingual + fallback templates | 40m | ✅ DONE | 10 languages. Unicode script-range detection at the front of the pipeline, so detection never depends on the model. Templates are a *presentation* fallback, not a reduced answer. |
| **P6** | HTTP API surface | 25m | ✅ DONE | 5 endpoints (including `/api/eval`), CORS, recovery, per-IP rate limit, concurrency cap. Capped the concurrency defect found in I6; the stream now carries clarifications and every list marshals as an array (I16, I17). |
| **P7** | Frontend: MapLibre map, chat, trace panel | 100m | ✅ DONE | React 19 + TS + Vite + MapLibre. Streaming trace, verdict card, hazard grid, zone card, official-advisory panel, per-dataset source table, published-threshold table, browser TTS, 10-language UI, mobile layout. Map chunk lazy-loaded so the verdict renders without it. `tsc` clean, production build 87 kB gzip on the critical path. Verified in a real browser against the built bundle: 44/44 checks over 6 languages and 4 refusals. |
| **P8** | Deploy Render + Vercel, verify live | 40m | 🟡 PARTIAL | Manifests written and locally verified: `render.yaml`, `web/vercel.json`, `api/Dockerfile` (16.1 MB image, live query answered through the container), `.env.example` for both sides. **Blocked on account tokens — not yet deployed.** |
| **P9** | Demo hardening, seed queries, docs | 50m | 🟡 PARTIAL | `architecture.md` and `INSTR.md` current; operational limits moved from constants into config. Twenty-one correctness bugs found and fixed (I5–I10, I13–I26), including a false band-membership claim, unattributed zone figures, an advisory measured from the wrong point, a refusal that rendered nothing, a verdict for a place that resolved to (0,0), and a fishing forecast for Hyderabad. Remaining: a live LLM run once a key exists, and a real on-device demo pass. |
| **P10** | Interface rebuild: globe, tour, narrated wait | 60m | ✅ DONE | A rotating globe drawn in a runtime canvas (no texture host to be blocked), flying to the queried place and coloured by verdict, plotted from the same coastal table the backend resolves against. First-visit tour with real keyboard support. Named, ticked loading stages. Design-system rewrite, mobile-first, inline SVG icons. Both visualisations lazy behind an error boundary with the 2D chart behind it. Found and fixed two defects the screenshots could not show (I27, I28). Verified by measurement, since the implementing model cannot see rendered output: 0 axe violations at two viewports, 0 console errors, worst contrast 4.51:1, 10/10 behavioural checks, 44/44 e2e, and the backend re-verified unchanged at 108/108. |

**Budget:** 440 min ≈ 7.3h of 10h. Spent to date ≈ 7h. **Remaining ≈ 1.5h of buffer**, most of it reserved for P8 deployment and the live-LLM pass.

### P0 findings (detail)

**Verified working — may be relied upon:**
- `marine-api.open-meteo.com/v1/marine` — waves, swell, wind-wave, SST, tides
- `api.open-meteo.com/v1/forecast` — wind, gusts, precip, cloud, weather code
- `archive-api.open-meteo.com/v1/archive` — historical baseline
- `geocoding-api.open-meteo.com/v1/search` — place → coords
- `incois.gov.in` main site — reachable
- `api.rainviewer.com` — reachable
- `services.arcgisonline.com` Ocean Base — reachable (map backdrop)
- `tile.openstreetmap.org` — reachable (map labels)
- `openrouter.ai` — reachable

**Confirmed dead — do not build on:**
- `erddap.incois.gov.in` — no response, connect fails
- `coastwatch.pfeg.noaa.gov` — 30s timeout

**Traps found:**
- `sea_level_height` is **invalid**; correct identifier is `sea_level_height_msl`
- INCOIS PFZ advisory payload is XHR-loaded, absent from server HTML. Sector roster *is* extractable (14 sectors). Scrape abandoned in favour of our own PFZ algorithm.
- A forecast window must be anchored on **the current hour**, not the first hour of the returned day. Aggregating from midnight reports the morning for a question asked at 17:20.

---

## Decision Log

| # | Phase | Decision | Rationale |
|---|---|---|---|
| D1 | P0 | Chose **SIH26176 ORCA** over SIH26083 heatwave | The heatwave PS requires a Mortality Risk Index that cannot be built — India publishes no ward-level heat-attributable mortality. ORCA is fully buildable from keyless public data. |
| D2 | P0 | **LLM never issues the safety verdict** | Hallucinated go-to-sea advice is a safety liability. Verdict comes from deterministic Go. This is the project's core thesis. |
| D3 | P0 | Compute **our own PFZ** instead of scraping INCOIS's | Advisory is XHR-loaded; reverse-engineering is high-risk in a 10h build. Our own algorithm is more impressive and has zero fragility. |
| D4 | P0 | **Open-Meteo is the data spine**; ERDDAP demoted to optional | ERDDAP hosts verified unreachable. The spine must be the source that actually answers. |
| D5 | P0 | **Go, not Rust** | No CPU-bound work exists in this system. Rust costs iteration speed, which is the scarcest resource. Go's errgroup maps to agent fan-out; single static binary fits Render Free. |
| D6 | P0 | **No database**; snapshot baked into binary | Render Free has an ephemeral filesystem. Turning that into a version-controlled fallback makes it a resilience feature. |
| D7 | P0 | **2 LLM calls per query** (plan + narrate) | OpenRouter free tier is 50 req/day, 1,000/day after $10. Two calls per query makes a 10h build viable on a free tier. |
| D8 | P0 | Cut voice input, route optimisation, geofencing, auth | Not required by the PS; each is hours we do not have. Prefer narrow-and-working. |
| D9 | P4 | **Per-agent `recover` + `sync.WaitGroup`**, not `errgroup` | Fan-out must *not* cancel siblings: one dead upstream should degrade one input, not abort the request. `errgroup.WithContext` does the opposite of what is needed here. |
| D10 | P5 | Templates carry the **published band numbers** in every language | A localized zone sentence was initially written to assert "inside the 24–30 °C band" unconditionally, which is false for 30.4 °C water (I7). Numbers are never translated; the claim branches on the measurement. |
| D11 | P7 | **Lazy-load the map** as a separate chunk | MapLibre is ~285 kB gzipped and the verdict does not need it. On a weak coastal connection the answer must arrive first. Critical path is now 87 kB gzipped. |
| D12 | P8 | **Operational limits are config, not constants** | Concurrency cap and per-IP rate limit decide whether a free-tier host survives its own popularity. The first `render.yaml` draft invented six variables that did not exist; the fix was to make the real limits configurable rather than to write a blueprint fiction. |
| D13 | P9 | **Snapshot disclosure goes in the answer text**, not only a UI badge | The answer is what gets read aloud and acted on. A badge the user may never look at does not make a stale safety message honest. |

---

## Standing engineering rules

1. **No LLM call may block a verdict.** Every LLM path has a deterministic fallback. Test with the LLM disabled.
2. **No unattributed number reaches the UI.** Every figure carries `source` + `timestamp`. Enforce in the result struct, not by discipline.
3. **Every agent failure is isolated.** Per-agent `recover`; the answer degrades, it does not die.
4. **Standard library for HTTP/SSE.** No web framework. Keeps the binary small and the dependency surface near zero.
5. **Snapshot fallback is mandatory, not optional.** P2 does not complete without it.
6. **Verify before claiming.** A phase is DONE only when the behaviour was executed and observed. Not when the code compiles.

---

## Verification actually performed

Recorded because rule 6 makes executed-and-observed the bar for DONE.

| Check | Command | Result |
|---|---|---|
| Build, vet, tests | `go build ./... && go vet ./... && go test -race ./...` | pass, all packages |
| Determinism | `TestVerdictIsDeterministic` (200 identical calls) | pass |
| Offline resilience | `ORCA_OFFLINE=1` against a running server, 4 languages | pass — snapshot answers, every citation `Live: false` |
| Snapshot honesty | `TestSnapshotIsDisclosed` | pass — disclosed in all 10 languages, absent when live |
| Band correctness | `TestZoneBandClaimFollowsTheMeasurement` (21.0/24.0/27.5/30.0/30.4/33.0 °C × 10 languages) | pass |
| Zero-value config | `TestWithDefaultsFillsEverything` | pass — a bare `Config{}` is now safe |
| Stream contract | `TestStreamSendsExactlyOneDone` | pass — 7 agent, 1 answer, 1 done, 1 end |
| Full attribution | `TestEveryDisplayedNumberHasACitation` | pass — 14 attributed datasets, none claimed live while offline |
| hi/mr disambiguation | `TestHindiMarathiDisambiguation` | pass — 9 real sentences, both languages, shared vocabulary |
| Language of the answer | production-bundle E2E, 5 cases | pass — asserted on the rendered answer panel's script |
| Refusal correctness | `TestUnresolvablePlaceNeverYieldsAVerdict`, `TestLandlockedPlacesAreRejected` | pass — no verdict for a place that does not exist or has no sea |
| Determinism of the verdict | offline eval, `repeat=3` | pass — 108/108 |
| Frontend types | `npx tsc -b --noEmit` | pass |
| Frontend build | `npm run build` | pass — main chunk 91 kB gzip; globe and map lazy |
| Accessibility | axe-core (wcag2a/aa, wcag21a/aa) at 1440×900 and 390×844 | pass — 0 violations |
| Interface behaviour | Playwright, 10 checks | pass — tour keyboard flow, focus, language persistence, live WebGL |
| Visual audit | PNG pixel analysis + computed styles | pass — 0 console errors, no horizontal overflow, worst contrast 4.51:1 |
| Production-bundle e2e | `node scripts/e2e.mjs` | pass — 44/44, six languages |
| End-to-end stream | Vite → Go, English/Hindi/Tamil/Odia queries | pass — event counts correct in all four |
| Container | `docker build` + `docker run`, live Mumbai query | pass — 16.1 MB image, verdict `go` |
| Live LLM path | — | **not run** — no key yet |

---

## Known Issues

| # | Phase | Issue | Severity | Status | Resolution |
|---|---|---|---|---|---|
| I1 | P0 | `erddap.incois.gov.in` TLS chain defect: server omits the `GlobalSign RSA OV SSL CA 2018` intermediate | Medium | Accepted | The host is live, not down. Certificate validation fails against a normal root store. Resolution is to pin the intermediate, never to disable verification. Optional enrichment only; the system never depends on it. |
| I2 | P0 | `coastwatch.pfeg.noaa.gov` 30s timeout | Medium | Accepted | Optional enrichment only. System never depends on it. |
| I3 | P0 | INCOIS PFZ payload not in server HTML | Low | Accepted | We compute PFZ ourselves (D3). `IncoisAgent` does reachability corroboration only. |
| I4 | — | OpenRouter free tier 50 req/day | High | Mitigated | $10 top-up → 1,000/day. Two calls per query (D7), model strictly optional. |
| I5 | P7 | SSE emitted **two** `done` frames | High | **Fixed** | The handler drained a `done` channel on close, but the orchestrator already emits `done` before closing. Removed the drain; `TestStreamSendsExactlyOneDone` pins the count. |
| I6 | P6 | Zero-value `Config` **deadlocked** the server | High | **Fixed** | `MaxConcurrent: 0` made a zero-capacity semaphore; the first clamp I wrote made it throttle to 1 req/min instead, which broke the suite differently. Replaced with `Config.WithDefaults()` holding the documented defaults, applied once in `httpapi.New`. Covered by `internal/config` tests. |
| I7 | P5 | Localized zone sentence claimed water was "inside the 24–30 °C band" at 30.4 °C | **Critical** | **Fixed** | Split the frame into `zoneWhy` / `zoneWhyOut` and branch on the measured value. A safety sentence that asserts a false band membership is the exact failure this system exists to prevent. |
| I8 | P9 | Snapshot data was never disclosed in the answer | High | **Fixed** | `f.Prov` was assembled *after* narration, so the renderer saw an empty list. Provenance is now built before the narrator runs, and the answer carries a localized disclosure when every figure is non-live. |
| I9 | P8 | `ORCA_OFFLINE` was read in the data package, ignoring `config` | Low | **Fixed** | One place interprets the environment now. `main` calls `data.SetOffline(cfg.Offline)`. |
| I10 | P3 | PFZ fan cells carried no citation, so the zone coordinates, score and SST anomaly had no source | High | **Fixed** | The winning sample's marine call now travels with the zone in a new `PFZ.Citations` field, `SSTBaseline` returns its archive citation, and both are flattened into `Findings.Prov`. `TestEveryDisplayedNumberHasACitation` and `TestPFZCarriesItsOwnCitations` pin it. Verified live: 14 attributed datasets including `sea_surface_temperature_mean`. |
| I11 | P5 | Non-English answers omit the engine's sea-state prose | Low | Accepted | The engine reasons in English only. The localized answer carries the band sentence with the published numbers, and safety rationale is fully localized. Translating engine prose was judged a worse trade than stating the rule with digits. |
| I12 | P8 | Not deployed | High | **Open** | Blocked on Render and Vercel account tokens. Manifests are written and locally verified. |
| I13 | P9 | Hindi marine queries were answered in Marathi | **High** | **Fixed** | The Devanagari disambiguator was a Marathi-only, first-match marker list that included words spelled identically in both languages — `समुद्र`, `नाही`, `किंवा`, `उद्या`, `साठी` — so "क्या पुरी के पास समुद्र में मछली पकड़ना सुरक्षित है?" matched the word for "sea" and flipped to Marathi. Replaced with weighted scoring: diagnostic tokens score 2, shared tokens 1, ties go to Hindi. Nine real-sentence cases pinned in `TestHindiMarathiDisambiguation`. The bug was invisible to the earlier browser test, which printed the *expected* language rather than the language the answer was actually written in. |
| I14 | P9 | The degraded-inputs banner printed raw internal keys in warning styling | Medium | **Fixed** | Every answer with no API key showed "Degraded inputs: incois, llm" in an amber box, which implies the safety verdict was weakened when the opposite is true. Labels are now humanised and localised, and a box whose only missing inputs are the optional ones (advisory bulletin, narrator) renders as a muted note. |
| I15 | P1 | INCOIS corroboration was measured from the coast, not from the zone it claimed to corroborate | High | **Fixed** | The agent ran in parallel with the marine and weather agents, so the PFZ point did not exist yet, and it measured the distance from the coastal reference. The panel said "our computed zone is 39 km" about a point that was not the zone. The bulletin geometry is now retained and re-measured locally against the final PFZ point — exact, and no extra round trip. Live: Puri's zone sits 85 km offshore and is 63.6 km from the published line, against 39.3 km measured from the coast. `TestMeasureToUsesThePointItIsGiven` pins it. |
| I16 | P9 | A question with no location in it rendered nothing at all in the browser | High | **Fixed** | `/api/ask` returned a correct clarification, but the clarification path returned before emitting `answer` or `done`, so the event stream closed after the planner and the interface stayed on its empty state. A user who asked "is it safe to go out today?" was shown silence. Pinned by `TestClarificationReachesTheStream`. |
| I17 | P9 | Every list in a clarification response marshalled as `null`, crashing any client that iterates it | High | **Fixed** | A nil Go slice is `null` in JSON, so `hazards`, `rationale`, `degraded`, `prov`, `pfz.reasoning` and each agent's `sources` arrived as `null` on exactly the response a confused user most needs. The browser threw `Cannot read properties of null (reading 'map')` the moment the clarification started reaching it. `Findings.MarshalJSON` normalises every list to `[]`; `TestListsMarshalAsArraysNotNull` pins the wire format. Fixed at the contract rather than in the UI, because the API is public. |
| I18 | P9 | `PFZ_SEARCH_KM=0` silently became 120 | Medium | **Fixed** | The documented way to cut the six-call marine fan down to nothing did nothing: `WithDefaults` replaced a zero with the default, because it could not tell "the operator typed 0" from "nobody set it". Load now records whether the variable was present. Pinned by `TestPFZSearchKmZeroIsHonoured`. |
| I19 | P9 | Vite proxied to port 8099, which nothing listens on | Medium | **Fixed** | `npm run dev` produced a frontend whose every request failed to proxy, and the only symptom was an empty map panel. |
| I20 | P9 | Place names could not be found in any transliteration not listed in the alias table | High | **Fixed** | `కోచీ`, `కోచి`, `ಕೊಚ್ಚಿ`, `ಕೊಚಿ`, `कोची`, `कोच्ची` are all Kochi, and the table listed only some of them, so three languages failed to resolve their own place name. Matching now falls back to the consonantal skeleton of each word — a Latin fallback is tried before it, and the match must start a word, because a bare substring search resolved a Telugu Kochi question to **Diu** (`ఉదయం` "morning" contains `దయ`, which is what `దియు` reduces to). |
| I21 | P9 | Asking in a non-English language changed the *analysis*, not just the wording | **Critical** | **Fixed** | The deterministic router's intent and time keywords were English with a few borrowed words, so `intent=safety, window=30h` in English became `intent=general, window=6h` in Odia, Malayalam, Kannada, Gujarati, Bengali and Marathi. A 6-hour window stops short of tomorrow morning's weather, so the same Puri question was a `caution` in English and a `go` in Hindi. The answer still came back in the right language, so nothing looked wrong — and the browser test passed, because it never checked the plan. The vocabulary is now data covering all 10 languages, every non-Latin term is required to occur in a sentence already in the repository, and `TestPlanDoesNotDependOnTheLanguage` compares the plan of the same question in all 10. |
| I22 | P9 | "Where to fish" mixed two different places in one paragraph | High | **Fixed** | The section leads with the zone's coordinates, then stated the *coastal* sea-surface temperature next to the *zone cell's* wave height, so a reader checking the numbers found two temperatures and a contradiction. Both now come from the winning fan cell, and that cell's values are on the wire (`pfz.sample_*`), because the sentences quoted a figure no client could see. The live eval caught this: groundedness was 65%, and the 35% were exactly the numbers the API did not expose. |
| I23 | P9 | The consistency metric could not tell a product bug from a broken test | Medium | **Fixed** | A verdict disagreement (a bug) and a plan disagreement (the router read two different questions) both reported as "answer_consistency below 100", which points at the wrong component. The scorer now names which, and a group whose members were planned differently scores nothing rather than partial credit, so an invalid test cannot look like a passing one. The corpus had drifted the same way: Puri's English case asked about tomorrow morning and its Hindi and Odia cases did not name a time. |
| I24 | P9 | A metric nobody ran reported `0.0%` | Low | **Fixed** | Determinism needs `repeat`, so a run without it printed a confident zero and looked like the worst number in the report while failing nothing. Unmeasured metrics now carry a note and stay out of the verdict. |
| I25 | P9 | An unresolvable place was answered as if it were the sea off Africa | **Critical** | **Fixed** | Three separate leaks. (1) A failed geocode returned a zero-value `Geo`, so `(0,0)` — the Gulf of Guinea — passed every "is this a real place" check and got a full marine verdict. The gate now tests the *resolution result*, not the struct. (2) A planner edit that emptied the place string was being reverted, which quietly disabled geocoding for the case it was meant to protect. (3) `MatchTown` matched multi-word names by substring, so a Telugu "Car Nicobar" question could resolve to a town that merely contained the first word. Multi-word entries now accept exact name or alias only. Pinned by `TestUnresolvablePlaceNeverYieldsAVerdict` and `TestKnownPlacesStillResolve`. |
| I26 | P9 | Hyderabad got a fishing forecast, from 314 km inland | **Critical** | **Fixed** | It geocodes perfectly well, and a resolved coordinate was treated as sufficient. A waypoint was then projected 45 km offshore a bearing the table had no right to supply, and a confident verdict came out the other end. A place further than `MAX_COAST_DISTANCE_KM` (120 km) from the coast now resolves as `inland` and is refused in the reader's language — a different message from "I could not find that place", because the place was found and is simply the wrong kind of place. Jaipur is 845 km out. `Kanyakumari` is the counter-case: it failed to resolve at all until it was added to the table, and a tool that refused a real fishing town while answering for a landlocked city had its priorities backwards. Pinned by `TestLandlockedPlacesAreRejected` and `TestInlandPlaceExplainsItself`, with five adversarial eval cases. |
| I27 | P10 | A misconfigured API base hung the interface on a spinner forever | High | **Fixed** | The e2e harness served `dist/` without proxying `/api`, so the single-page-app fallback answered the streaming endpoint with `index.html` — a 200. The client accepted it as a stream, found no events, and waited without end. The real lesson is that a 200 is not a stream: the client now checks the content type and reports it, and reports it if the connection closes before a verdict arrives. The harness pipes `/api` the way Vercel's rewrite does. |
| I28 | P10 | The primary button was invisible on the landing page | Medium | **Fixed** | Disabled with `opacity: 0.5`, which dragged the accent fill and its dark label toward the same background — measured contrast 1.01:1, on the one control a first-time visitor is asked to find. Disabled buttons now use a real surface and a readable label: still plainly present, plainly not ready. |

---

## Definition of Done

Tracked against `architecture.md` §10 (success criteria) and §11 (honest limitations). A phase may be DONE while the overall build is not; these are the release gate.

- [x] Tamil and English question both answered in the same language — verified in all 10 languages
- [x] Verdict demonstrably produced by deterministic code; trace shows attribution
- [x] Map shows a PFZ point from our own algorithm
- [x] Every displayed number carries source + timestamp — zone and anomaly now included (I10 closed)
- [x] Answer is written in the detected language — asserted on the rendered answer panel, not on the input (I13 closed)
- [x] Works with outbound network disabled (snapshot fallback, correctly labelled *in the answer*)
- [x] Works with LLM disabled (deterministic templates)
- [ ] Live deployment answers a real query — blocked on I12
- [ ] Live LLM narration observed end-to-end — blocked on key

---

## Session log

- `1·P0 · Data recon · 8 sources probed, 2 ERDDAP hosts dead, sea_level_height_msl trap caught, INCOIS scrape abandoned`
- `2·P1 · Scaffold · zero-dependency Go module, config fully env-driven, coastal table 31 locations`
- `3·P2 · Data layer · 4 Open-Meteo endpoints live, 16-place snapshot embedded, ORCA_OFFLINE added`
- `4·P3 · Engine · pure Evaluate/Assess, 24–30 °C band, 4 hazard checks, thresholds published`
- `5·P4 · Agents · 6 named agents + narrator, concurrent fan-out, per-agent recover, 7-entry trace`
- `6·P5 · Narration · 10 languages, script-range detection, templates as presentation fallback`
- `7·P6 · HTTP · 4 endpoints, CORS, rate limit, SSE; found I5`
- `8·P7 · Frontend · React 19 + MapLibre, streaming trace, TTS, lazy map chunk; found I5 in SSE`
- `9·P8 · Deploy prep · Dockerfile 16.1 MB verified with a live query, render.yaml, vercel.json; found I6, I9`
- `10·P9 · Hardening · found I7 (false band claim) and I8 (undisclosed snapshot); both fixed with tests; config hardened`
- `11·P9 · Attribution · found I10 (zone figures unsourced); added PFZ.Citations and archive baseline citation; verified 14 attributed datasets live`
- `12·P9 · Language · found I13 (Hindi answered in Marathi) and I14 (raw keys in the degraded banner); both fixed, and the E2E harness rewritten to assert the rendered answer's script rather than the expected label`
- `13·P1 · Advisory · INCOIS PFZ bulletin read as OGC WFS geometry instead of a scraped page; replaced the reachability ping with a measured distance, added bbox widening, a 30-minute cache, and re-measurement against the real zone (I15)`
- `14·P9 · Evaluation · built a 103-case suite over the real orchestrator; it found I20 (transliteration) and two harness defects, and the first full run is 100% on all 7 metrics`
- `15·P9 · Browser · production-bundle E2E found I16 (refusals invisible) and I17 (null lists crash the client); both fixed, 44/44 checks pass across 6 languages and 4 refusals`
- `16·P9 · Config · fixed I18 (PFZ_SEARCH_KM=0 did nothing) and I19 (Vite proxied to a dead port); rewrote the E2E to submit by form rather than by button label, which had silently stopped submitting after the first question`
- `17·P9 · Contract · fixed I22 ("Where to fish" described two places) and I17's remaining gap: every list is now an array, the zone cell is on the wire, and 44/44 browser checks pass across 6 languages`
- `18·P9 · Evaluation · the first live run failed at 65% groundedness and 81% consistency, which found I21, I22 and I23: a non-English question was a different question. Router vocabulary rebuilt as verified data; all 6 live-measurable metrics now 100%`
- `19·P9 · Refusals · found I25: a place that could not be resolved still produced a verdict, at (0,0) in the Gulf of Guinea. Three leaks closed at once — a zero-value Geo passing the validity check, a reverted planner edit, and multi-word names matching by substring. Test added that asks for a place that does not exist and asserts no verdict comes back`
- `20·P9 · Geography · found I26: Hyderabad geocodes cleanly, is 314 km inland, and was being handed a fishing verdict from a waypoint projected 45 km offshore a bearing it had no right to. Places beyond MAX_COAST_DISTANCE_KM now refuse with their own message in all 10 languages. Kanyakumari, which had been failing to resolve, is now in the table — the tool had been refusing a real fishing town while answering for a landlocked city`
- `21·P10 · Interface · globe, tour, narrated wait, design-system rewrite, mobile-first, inline icons. Both visualisations lazy behind an error boundary, the 2D chart behind them. Verified by measurement rather than by eye, because the implementing model cannot view rendered output`
- `22·P10 · Defects the screenshots could not show · I27: a misconfigured API base returned 200 with index.html, which the client accepted as an event stream and waited on forever. The content type is checked now, and a stream that closes without a verdict says so. I28: the disabled primary button measured 1.01:1 contrast against its own background — opacity was dragging fill and label to the same colour`
- `23·P10 · Re-verified · tsc clean, 91 kB gzip critical path, 0 axe violations at 1440×900 and 390×844, 0 console errors, 10/10 behavioural checks, 44/44 e2e, and the untouched backend still 108/108 with determinism actually measured at repeat=3`
- `24·P10 · The map on the globe · found the sphere had an ocean, a graticule and thirty-two dots, and no land at all. Natural Earth 110m vendored into the repo at build time (74 kB, self-hosted) and drawn as a layer proud of the ocean. A second line layer for the outline produced NaN geometry and twenty-four warnings, and the over-wide catch around the whole thing was swallowing a typo'd accessor as "coastline unavailable" — the layer did nothing, silently, with a clean console`
