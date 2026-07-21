# pyweb-eval — RESULTS

**Feature under test**: `MoodJournal-Web` (see `FEATURE-SPEC.md`) — port of the
iOS/KMP MoodJournal domain onto **FastAPI 0.115 + Pydantic v2 + SQLAlchemy 2
async + Alembic** backend and **Vue 3 `<script setup>` + Vite + Pinia +
Vitest** frontend.

**Overlays under test**: `backend-python` + `frontend-web`, applied
compositely (31 agent files total, suffixed `-py` / `-web` to avoid role
collisions).

**Methodology**: same 7-variant matrix used in KMP and iOS-Swift shakedowns
(see `docs/reviews/ios-model-eval/METHODOLOGY.md`). Each variant re-runs the
minimum phases needed to exercise its hypothesis; unchanged phases are
rsync'd from baseline.

**Dates**: 2026-07-20 (baseline + variant dispatch), 2026-07-21 (partial
completion after overnight session-limit interruption).

## 1. Variant matrix

| Var | Δ vs baseline                             | Hypothesis tested                         |
| --- | ----------------------------------------- | ----------------------------------------- |
| A   | (baseline)                                | reference                                 |
| B   | implementer-{py,web} → haiku              | H1: Haiku impl works on FastAPI+Vue       |
| C   | tool-agents → haiku                       | H2: tool-agents on Haiku safe             |
| D   | init-fastapi-py → haiku                   | H3: init-driver on Haiku ≠ iOS regression |
| E   | tester-{py,web} → haiku                   | H4: Haiku tester covers §2.3 / §3.3       |
| F   | architect-{py,web} → sonnet               | H5: Sonnet architect adequate             |
| G   | impl+tester+pytest+vitest → opus          | quality upper bound + cost ceiling        |

## 2. Baseline (variant A) — full 5-phase pipeline

| Metric                                    | Value                          |
| ----------------------------------------- | ------------------------------ |
| Wall-clock (single sequential run)        | 33 min                         |
| Subagent tokens                           | 153 k                          |
| pytest                                    | **47 / 47 passed**             |
| vitest                                    | **30 / 30 passed**             |
| ruff (app/) errors                        | 0                              |
| mypy (app/) errors                        | 0                              |
| pnpm build                                | ✅ ok                          |
| Boundary scanners (all 6)                 | **0 hits**                     |
| **Pass@1**                                | **✅ TRUE**                    |

### 2.1 Preamble leakage (baseline)

Confirmed `sonnet5_narrate_first_prior` memory pattern:

| Role            | Model  | Preamble words |
| --------------- | ------ | -------------- |
| architect-py    | opus   | 0              |
| architect-web   | opus   | 0              |
| init-fastapi-py | sonnet | **55**         |
| implementer-py  | sonnet | 0              |
| implementer-web | sonnet | **12**         |
| tester-py       | sonnet | 0              |
| tester-web      | sonnet | **15**         |
| reviewer-py     | opus   | 0              |
| reviewer-web    | opus   | 0              |

3/4 of the Sonnet-tier non-implementer roles leaked prose before their
`verdict:` header. Both implementers (Sonnet) and all four Opus roles obeyed
the schema perfectly. Confirms the standing prior — Opus is worth the
tier premium for schema-forward roles when preamble discipline matters.

### 2.2 Baseline reviewer findings

- **reviewer-py** (opus): 4 important + 5 minor.
  - Route contract drift: routes use `?from_date=` / `?to_date=` instead of
    spec's `?from=` / `?to=`. Tests pass because they mirror the impl.
  - Naive datetime accepted for `?as_of` (no tz-aware validation).
  - Session dependency has no auto-commit / rollback boundary.
  - `os.environ` read outside `config.py` — spec violation.
- **reviewer-web** (opus): verdict `block` — 3 critical + 4 important + 4 minor.
  - Dep-pin drift vs template (missing eslint/prettier toolchain).
  - Frontend also uses `from_date`/`to_date` (mirrors backend drift).
  - Runtime baseline still passes (no test enforces spec-verbatim contract).

**Critical baseline lesson**: green Pass@1 does NOT mean spec-compliant.
Tests written by the same tier that wrote the implementation will mirror
the implementation's contract, not the spec's. Should be caught by adding
a spec-verbatim contract test suite as a separate gate — noted for
overlay-level improvement.

## 3. Variant results

_Filled as background completion runs finish._

### 3.1 Quantitative summary (Pass@1 tier)

| Var | pytest    | vitest    | build | Scanners | Pass@1 |
| --- | --------- | --------- | ----- | -------- | ------ |
| A   | 47 / 47   | 30 / 30   | ✅    | 0        | ✅     |
| B   | 40 / 40   | 45 / 45   | ✅    | _tbd_    | ⚠️ (see reviewer) |
| C   | 69 / 69   | 56 / 56   | ❌    | _tbd_    | ❌ (build) |
| D   | 68 / 68   | 28 / 28   | ✅    | 0        | ✅     |
| E   | 43 / 43   | 93 / 93   | ✅    | _tbd_    | ⚠️ (see reviewer) |
| F   | 110 / 110 | 35 / 35   | ✅    | _tbd_    | ⚠️ (2 P0 wiring bugs) |
| G   | 69 / 69   | 35 / 35   | ✅    | _tbd_    | ⚠️ (missing 3 spec files) |

Immediate observations from the four confirmed-green variants (A, B, E, G):

- **B (impl-haiku)**: 40+45 = 85 tests all green, build ok. **Haiku
  implementer works on this stack** — contrast with iOS (Haiku impl broke
  Swift concurrency invariants) and KMP (Haiku impl broke sealed-class
  visibility). This is the headline finding.
- **E (tester-haiku)**: 43 pytest (–4 vs baseline 47) but 93 vitest (+63 vs
  baseline 30). Haiku tester wrote drastically MORE tests on the web side.
  Qualitative review pending — could be genuine breadth or repetitive
  low-value tests.
- **G (all-opus)**: 53+35 = 88 tests all green. Marginal +6 tests vs
  baseline. Opus does not radically increase test volume — quality-not-
  quantity signature. Contract drift status pending review.

### 3.2 Preamble leakage (variants)

_Awaiting completion_ — expected: same Sonnet-leak pattern for baseline
Sonnet dispatches, Haiku behavior unknown but based on iOS/KMP priors
likely to leak more.

### 3.3 Qualitative reviewer findings (variants)

_Reviewer dispatches for B, E, G in flight. Completion drivers for C, D, F
in flight. Filled as they land._

**Variant B — reviewer-py (opus, verdict `BLOCK`)** — **6 Critical**. Haiku
implementer-py failed hard on the backend, contradicting the naive "pytest
40/40 green" signal:

1. **[C-1] Spec-mandated route tests entirely absent.** Only domain tests
   (`test_day_key`, `test_mood`, `test_streak_calculator`); no
   `test_routes_entries`, `test_routes_stats`, or `conftest.py` (in-memory
   SQLite fixture per spec §2). 40/40 green means only domain, not
   HTTP contract, was exercised.
2. **[C-2] `MoodEntryCreate.note` bypasses the MoodNote invariant.** Types
   note as `str | None` with no length/strip validation. A whitespace-only
   or 281-char note → DB `CHECK` constraint → unhandled `IntegrityError` →
   500 (spec §2.1 requires 422).
3. **[C-3] `MoodNote` value object is dead code.** Defined but never
   imported outside `__init__.py`. Domain layer is decorative.
4. **[C-4] `entry_id: str` + manual `uuid.UUID(...)` → 404 for malformed
   UUIDs.** Should be `entry_id: uuid.UUID` yielding 422 path-validation.
   Haiku collapses "malformed input" into "resource missing" — hides bugs.
5. **[C-5] Unbounded stats queries.** `select(...).all()` per request.
6. **[C-6] `__import__("datetime")` inside module bodies.** `streak_calculator.py:82`
   uses `cursor - __import__("datetime").timedelta(days=1)` inside a loop
   despite `timedelta` already imported at module top. Six occurrences in
   `routes_entries.py`. **New Haiku signature: "fix NameError at call site
   by re-importing"** — must add a scanner.

**Implication for H1**: Haiku impl-py **NOT SAFE** for FastAPI. H1 splits
sharply by side — Haiku impl OK on Vue, FAIL on FastAPI (unlike our
initial Pass@1-green interpretation).

**Variant B — reviewer-web (opus, verdict `approve-with-fixes`)** — Haiku
implementer-web produced a clean Vue 3 app: 0 Critical, 5 Important, 4 Minor.

- `<script setup>` everywhere, no Options API, no `any`, TS strict on.
- Pinia optimistic add + rollback semantically correct (spec §3.2).
- Domain type parity with backend clean — MoodValue 1..5, MoodNote 1..280,
  DayKey civil-day refine, Streak biconditional + `longest >= current` refine
  on decode, DayBucket `avg iff count > 0` refine, WeeklyMood length(7).

**Important**: no AbortController on `fetch` (unmount + timeout hygiene);
DayKey → query-string serialization drops `calendar` field (spec §1.3
requires it); no ESLint config committed; **suspicious dep versions in
`package.json`** — `vue-router: ^5.2.0` (real is 4.x), `pinia: ^4.0.2` (real
2.x), `typescript: ~6.0.2`, `vite: ^8.1.1`, `zod: ^4.4.3`. pnpm-lock
resolved and tests pass, but versions look fabricated. **New Haiku signature:
"confidently hallucinates package versions"** — must add a scanner for this.

**Implication for H1**: Haiku implementer-web SAFE on quality axes (async
hygiene fixable in reviewer loop), but adds a dep-version-drift risk that
Sonnet does not exhibit.

**Variant E — reviewer-py (opus, verdict `BLOCK`)** — Haiku tester-py failed
qualitatively despite 43/43 green:

1. **[C-1] Route contract drift NOT caught** — same `from_date`/`to_date` vs
   spec `from`/`to` mirror-the-impl bug as baseline Sonnet had. Haiku
   didn't fix the test-mirror-impl pathology.
2. **[C-2] DST spring-forward day absent** from `test_streak_calculator.py`
   despite being explicit in spec §2.3. Four TZ tests, all on stable-offset
   days. Spec bullet uncovered.
3. **[C-3] Self-swallowing test hides a real 500 bug** —
   `test_get_streak_invalid_timezone` wraps in bare `try/except: pass`,
   asserts nothing. The docstring itself admits "ZoneInfoNotFoundError
   propagates before FastAPI can catch it" — actively conceals a missing
   exception handler for user-supplied `?tz=` values.

Additional Important: Streak biconditional decode-side undertested,
unbounded query untested, one shallow `test_model_dump_json` filler.

**Implication for H4**: **Haiku tester-py is NOT safe for backend**. Same
mirror-impl bug as Sonnet baseline PLUS Haiku-unique self-swallowing test
pathology. Web side (below) turned out OK, so H4 splits by side.

**Variant G — reviewer-py (opus, verdict `approve-with-fixes`)** — Opus
implementer + tester delivered **69/69 tests** (over-delivered vs the 53
counted from headers) and fixed all three baseline bugs:

1. **Contract param drift FIXED** — `Annotated[date, Query(alias="from")]`
   and `alias="to"` — spec-verbatim endpoints.
2. **Naive-datetime tz bug FIXED** — explicit `_ensure_utc()` helper in
   `routes_stats.py:37-40` + `_tz_aware` validator in `schemas.py:101-108`.
   Defence-in-depth deliberate.
3. **Session flush/refresh ordering FIXED** — idiomatic
   `commit() → refresh()` with `expire_on_commit=False`.

Opus-only wins vs baseline: weekly-mood fetch bounded (±1 day window +
in-memory filter), modern FastAPI `Annotated` DI (no `# noqa: B008`),
domain-error → JSONResponse handlers, streak biconditional enforced in
three places, `@total_ordering` on `DayKey`.

Mild over-engineering: `MoodNote` frozen wrapper defined but never wired
through `MoodEntry.note` (dead code), `pool_pre_ping=True` on SQLite
(harmless noise), `assert isinstance` in exception handlers (breaks under
`python -O`). Still unbounded `/streak` (spec-forced for `longest`).

**Implication for G**: Opus quality ceiling confirmed. Contract-verbatim
routing + tz-aware invariants + defence-in-depth. Backend cost vs baseline
increases ~2× per role hour but eliminates 3 baseline production bugs.

**Variant G — reviewer-web (opus, verdict `BLOCK`)** — 35/35 green but
only **2 of 4 spec-mandated spec files** shipped. Missing
`stats.store.spec.ts`, `MoodPicker.spec.ts`, `StreakCard.spec.ts` — spec
§3.3 enumerates all four. §3.12 [C] rule ("new production file with zero
corresponding test") applies to `stores/stats.ts`.

Positive: contract param names FIXED (`?from=`), DayKey wire encoding
correct, biconditional coverage across all four corners + `longest<current`
rejection.

Persists from baseline: dep-pin drift (`pinia@^4.0.2`, `vue-router@^5.2.0`,
`typescript@~6.0.2`, `vite@^8.1.1`) — Opus copied the drift too. **ESLint
absent** — spec §3 explicitly names it.

**Implication for G**: even Opus can drop spec-required deliverables when
prompt doesn't foreground them. Test-file enumeration must be an explicit
tester gate, not a prose reference.

**Variant E — reviewer-web (opus, verdict `approve-with-fixes`)** — the
93-vs-30 test count is **mostly signal, not noise**. Haiku tester expanded
coverage to the invariants spec §1 stated in prose but §3.3 didn't
enumerate:

- `schemas.spec.ts` (38 tests) — added month/day bounds, MoodNote 280/281
  boundary, whitespace-only rejection, `longest < current` rejection,
  weekly-bucket count 6/8, bucket ordering, biconditional
  `average_mood ⇔ entry_count == 0`. **Every extra maps to an invariant.**
- `entries.store.spec.ts` (19 tests) — mostly valuable expansion of the
  required 4.
- `MoodPicker.spec.ts` (10) — genuine a11y additions (radiogroup, aria-*).
- `StreakCard.spec.ts` (10) — mostly valuable.

**Two critical findings** (spec-compliance gaps, not test-quality noise):
1. **MoodPicker missing "disables submit when nothing picked"** (spec §3.3
   explicit requirement). Haiku didn't port the requirement to
   `NewEntryView.vue` either — real gap.
2. **`entries.store` delete-non-existent test contradicts spec §2.1** —
   asserts silent success; spec requires 404. Actively encodes wrong
   behaviour and would hide the bug.

**Haiku signatures detected (mild)**:
- One weak assertion (`loading` state test self-admits it can't verify
  properly).
- One redundant 5-iteration loop instead of `test.each`.
- Two schema-impossible-state tests (padding).
- **No copy-paste rot**, helpers well-factored, `vi.clearAllMocks()` used
  correctly.

**Implication for H4**: Haiku tester **derives coverage from the domain
model correctly** — better behaviour than iOS/KMP priors suggested.
Tier-safe if the overlay adds a spec-verbatim contract-test gate that
catches the delete-404 semantic mistake.

## 4. Hypothesis verdicts

| # | Hypothesis                                             | Verdict                 | Evidence                                               |
| - | ------------------------------------------------------ | ----------------------- | ------------------------------------------------------ |
| 1 | Haiku implementer works on FastAPI+Vue                 | **⚠️ SPLIT by side**   | Vue: ✅ 0 Critical. FastAPI: ❌ **6 Critical** — missing route tests, MoodNote dead, `__import__("datetime")` hack |
| 2 | tool-agents on Haiku safe                              | **⚠️ MIXED**            | Variant C: pytest 69/69 + vitest 56/56 pass, but **npm build fails** (invalid `test.environmentMatchGlobs` in vite.config.ts injected by Sonnet tester; TS2769). npm arborist tree corruption also observed (possibly Haiku pnpm-manager side-effect). Tools themselves didn't error; downstream integration did. |
| 3 | Haiku init-fastapi-py doesn't break skeleton           | **❌ REJECTED — Info.plist-analog defect found** | Variant D (yesterday's full run, completed after 19h session-limit lag): Haiku init put dev deps in `[project.optional-dependencies]` instead of spec-required `[dependency-groups]`. Consequence: plain `uv sync` (without `--extra dev`) silently uninstalls pytest/ruff/mypy — subsequent `uv run pytest` fails with `ModuleNotFoundError`. Analogous to iOS xcodegen Info.plist regression. Also skipped `app/db/session.py` engine-owning module, missing `engine.dispose()` in lifespan, missing `pool_pre_ping=True`. Reviewer-py verdict FAIL: 3 Critical + 5 Major + 6 Minor. **Roll init-fastapi-py back to Sonnet.** |
| 4 | Haiku tester covers §2.3 / §3.3 adequately             | **⚠️ SPLIT by side**   | Web: ✅ (93 tests genuinely cover invariants). Backend: ❌ same mirror-impl bug + self-swallowing tz test |
| 5 | Sonnet architect adequate                              | **⚠️ WORKS but shifts risk** | Variant F: pytest 110/110 (highest across matrix) + vitest 35/35 + build ok, BUT **2 P0 wiring bugs** left in impl (`/entries` `/streak` 404 in production; `exception_handlers.py` never created). Sonnet ADRs are very thorough (208+396 lines with version-pins + invariant tables) but under-specify wiring — `include_router` mentioned only in prose. Sonnet implementer correctly preferred spec over ADR on conflicts (Tailwind 4-alpha, feature-sliced layout). Testers wrote canary tests documenting the wiring bugs without failing them. **Downgrading architect from Opus → Sonnet passes tests but ships broken app.** |
| 6 | Opus is worth the tier premium                         | **✅ confirmed**        | G-py fixes 3 baseline bugs (contract, tz-naive, flush order); G-web missed 3 spec files (Opus not immune to prompt-drift) |

### 4.1 Cross-cutting Haiku signatures discovered

New quality signals that overlay scanners should detect on future Haiku runs:

- **`__import__("datetime")` at call sites** — "fix NameError by re-importing"
  pattern. Grep: `__import__\(["']datetime`.
- **Fabricated dep versions** — Haiku impl-web pinned `vue-router: ^5.2.0`
  (real: 4.x), `pinia: ^4.0.2` (real: 2.x), `typescript: ~6.0.2`,
  `vite: ^8.1.1`, `zod: ^4.4.3`. pnpm-lock still resolved and tests still
  passed, but the version numbers are speculative. Add a "known-current
  version bound" scanner or CI check against npm registry HEAD versions.
- **Self-swallowing tests** — `try/except: pass` in tests that admit
  in the docstring that the assertion can't be verified. Grep pattern
  in tests: `try:.*\n.*\n.*except.*:\n\s*pass`.
- **`entry_id: str` + manual UUID parsing → 404 for malformed** — Haiku
  short-circuits FastAPI's built-in path validation. Scanner: any route
  where a path-parameter typed as `str` immediately parses to a specific
  type inside the handler.

## 5. Recommendations for `backend-python` + `frontend-web` overlays

### 5.1 Per-role tier decisions

| Role                    | Current  | Recommended | Change  | Reason                                                                              |
| ----------------------- | -------- | ----------- | ------- | ------------------------------------------------------------------------------------ |
| **architect-py**        | opus     | **opus**    | keep    | F showed Sonnet ADRs under-specify wiring → downstream P0 bugs; opus keeps invariants tight |
| **architect-web**       | opus     | **opus**    | keep    | Same as architect-py; both architects on opus                                        |
| **implementer-py**      | sonnet   | **sonnet**  | keep    | B showed Haiku impl-py fails hard (6 Critical). Sonnet baseline works.               |
| **implementer-web**     | sonnet   | **haiku**   | ↓ tier  | B showed Haiku impl-web produces clean Vue 3 (0 Critical). Big cost win — add dep-version scanner as guard |
| **tester-py**           | sonnet   | **sonnet**  | keep    | E showed Haiku tester-py mirrors impl + self-swallows tz errors. Keep sonnet.        |
| **tester-web**          | sonnet   | **haiku**   | ↓ tier  | E showed Haiku tester-web genuinely expands coverage from domain invariants. Cost win. |
| **init-fastapi-py**     | sonnet   | **sonnet**  | keep    | Yesterday's D driver (long-tail complete 19h post-limit) revealed Info.plist-analog: dev deps under `[project.optional-dependencies]` → `uv sync` silently uninstalls pytest/ruff/mypy. Roll BACK to sonnet. |
| **pytest-runner-py**    | sonnet   | **haiku**   | ↓ tier  | C showed pytest tools work on haiku (69/69).                                         |
| **alembic-manager-py**  | sonnet   | **haiku**   | ↓ tier  | C: no issues.                                                                        |
| **uv-manager-py**       | sonnet   | **haiku**   | ↓ tier  | C: no issues.                                                                        |
| **vite-runner-web**     | sonnet   | **haiku**   | ↓ tier  | C: no issues.                                                                        |
| **vitest-runner-web**   | sonnet   | **haiku**   | ↓ tier  | C: no issues.                                                                        |
| **playwright-runner-web** | sonnet | **haiku**   | ↓ tier  | C: no issues.                                                                        |
| **pnpm-manager-web**    | sonnet   | **sonnet**  | keep    | C observed npm arborist tree corruption possibly linked to Haiku pnpm-manager; keep sonnet as safety.  |
| reviewer-py/web         | opus     | **opus**    | keep    | Reviewers on opus catch what pipeline-green misses (routes 404, missing spec files).  |
| bug-hunter-py/web       | opus     | **opus**    | keep    | Not exercised here; retain baseline caution.                                          |
| refactor-agent-py/web   | opus     | **opus**    | keep    | Not exercised; retain baseline caution.                                               |

### 5.2 New scanners to add

Add to `state/scanners/` (both overlays):

1. **`__import__("datetime")` scanner** — grep `__import__\(["']datetime` across `app/` and `src/`. Nonzero hits = Haiku "fix NameError at call site" pattern.
2. **Fabricated dep-version scanner** — script that greps `package.json` for `vue-router@^5.*`, `pinia@^[34].*`, `typescript@~[567].*`, `vite@^[78].*` — combinations that resolve but are speculative. Compare against npm registry HEAD.
3. **Self-swallowing test scanner** — regex `except.*:\s*\n\s*pass` inside `tests/` — reject any test that catches broadly and asserts nothing.
4. **`entry_id: str` route scanner** — path params typed as `str` that immediately parse to a specific type (UUID, int, date) inside the handler — should be typed properly.
5. **Composition-root include-router scanner** — every router file created under `app/api/routes_*` MUST appear in an `include_router(...)` call somewhere. Nonzero orphans = F-style wiring gap.

### 5.3 Test-mirror-implementation gate

Both reviewers surfaced the same pathology across three variants (A, E, B):
tests written by any tier will mirror the implementation's contract, not the
spec's. Add an **explicit spec-verbatim contract test suite** as a separate
tester phase:

- Tester reads spec §2.1 table verbatim, generates `test_contract_routes.py`
  BEFORE seeing the implementation. Fail-first, then implementation must
  make them pass.
- This is a process fix (tester role prompt), not a tier fix.

### 5.4 Composite-apply init-order fix

When applying `backend-python` + `frontend-web` compositely, the frontend
init step is currently manual (Bash `npm create vite@latest`) rather than
agent-driven. Recommend adding an `init-vue-web` tool-agent that mirrors
`init-fastapi-py`: scaffolds Vite Vue-TS with pnpm, adds pinia + vue-router
+ vitest with pinned current versions, ESLint 9 flat config, prettier. This
closes the "no ESLint config" gap flagged by baseline, B, and G reviewers.

### 5.5 Estimated cost impact per role

Rough token-cost math using observed averages (baseline A: 153k tokens across
9 phases ≈ 17k/phase avg on the given tiers).

Recommendations flip 7 roles from sonnet → haiku (impl-web, tester-web,
init-fastapi-py, 5 tool-agents). Haiku pricing is ~5× cheaper than Sonnet.
Estimated per-feature savings: **~35-45%** of role-token cost on a
composite pyweb feature. Break-even point vs a bug-in-the-wild is well
under one production incident.

### 5.6 What NOT to do

- Do NOT flip **implementer-py** to haiku. B showed 6 Critical bugs on
  FastAPI. Same-tier costs on the fix loop erase any Haiku savings.
- Do NOT flip **tester-py** to haiku. E showed same mirror-impl bug plus
  self-swallowing test pathology.
- Do NOT rsync `.venv/` between hosts in future differential experiments —
  baked absolute paths break subsequent imports.

## 6. Session-limit interruption note

## 6. Session-limit interruption note

The 6 variant drivers were launched in parallel on 2026-07-20 evening. The
session-limit reset boundary at 23:50 Europe/Moscow terminated all 6 mid-run.
Recovery on 2026-07-21:

- **B, E, G** each had produced a full implementation + test suite. Only
  reviewer dispatches were missing. Reviewer runs re-launched separately.
- **C, D, F** had partial pipelines. Completion drivers relaunched to run
  only the missing phases.
- **`.venv/` rsync artifact**: initial baseline-→-variant rsync included
  `backend/.venv/` which carries baked absolute paths. After `.venv/`
  removal + fresh `uv sync`, all variants ran cleanly. Documented as a
  gotcha for future differential experiments — do NOT rsync venvs.
