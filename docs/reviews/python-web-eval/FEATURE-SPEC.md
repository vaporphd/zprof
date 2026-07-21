# MoodJournal-Web — Feature Spec

Mirror of the iOS `MoodJournal` feature used for `ios-swift` model-routing
eval, ported to a **FastAPI 0.115 + Vue 3 `<script setup>`** full-stack app.
The spec is deliberately identical in domain shape so cross-stack results
(iOS Swift ↔ Kotlin Multiplatform ↔ Python/Vue) are calibrated on the
same invariant surface — Streak biconditional, DayKey validity, Codable /
Pydantic / TS round-trip preservation.

Consumers of this doc: `python-web-eval` shakedown hosts, both
`backend-python` and `frontend-web` overlays are exercised in one composite
run.

## 1. Domain (single source of truth)

The domain lives on the backend as Pydantic v2 models; the frontend derives
matching TypeScript types from `/openapi.json` at build time.

### 1.1 `MoodValue`

`int enum`, values `1 ... 5`. Semantic labels:

| Value | Label     |
| ----- | --------- |
| 1     | `low`     |
| 2     | `meh`     |
| 3     | `neutral` |
| 4     | `good`    |
| 5     | `great`   |

- Rejected on decode: any `int` outside `1 ... 5`.
- Serialised as the raw integer (schema stability across renames).

### 1.2 `MoodNote`

Newtype wrapper around `str`. Invariant: `1 ≤ len(text.strip()) ≤ 280`.
Round-trip preserves the pre-strip text exactly (strip is only for the
non-empty check).

### 1.3 `DayKey`

Calendar-day identifier bound to a calendar identifier.

- Fields: `year: int`, `month: int (1..12)`, `day: int (1..31)`,
  `calendar: str` (a stable storage token, default `"gregorian"`).
- Validation on construction and on decode: the `(year, month, day)` triple
  must name a real civil day in the specified calendar. Feb 30, Apr 31,
  Feb 29 in non-leap years all rejected.
- `previous()` / `next()` step by one civil day; roll-over across months,
  years, leap-years must be correct.
- Comparable, hashable, JSON-serialisable as
  `{"year": ..., "month": ..., "day": ..., "calendar": "gregorian"}`.

### 1.4 `MoodEntry`

- `id: UUID` (default new UUID4).
- `recorded_at: datetime` (tz-aware, UTC on the wire).
- `mood: MoodValue`.
- `note: MoodNote | None`.

### 1.5 `WeeklyMood`

Aggregate over the last 7 civil days ending at `asOf` (inclusive), in the
user's calendar.

- `buckets: list[DayBucket]` length exactly 7, ordered ascending by day.
- `DayBucket`: `day: DayKey`, `average_mood: float | None`, `entry_count: int`.
  `average_mood` is `None` iff `entry_count == 0`; otherwise it is the
  arithmetic mean of `mood.value` for that day's entries.
- `generated_at: datetime`.

### 1.6 `Streak` (biconditional invariant)

- `current: int ≥ 0` — count of consecutive days ending at *today* that have
  at least one entry.
- `longest: int ≥ max(current, 0)` — longest such consecutive-day run over
  the entire history.
- `last_logged_day: DayKey | None`.

**Biconditional invariant** — validated on every construction path AND on
decode:

> `current == 0 and longest == 0 ⇔ last_logged_day is None`.

Reject any decode payload that violates it.

## 2. Backend contract (`backend-python`)

Layout:

```
app/
  main.py               FastAPI() + lifespan
  api/
    routes_entries.py   /entries CRUD
    routes_stats.py     /streak, /weekly-mood
    schemas.py          Pydantic v2 mirrors of §1
  domain/
    day_key.py
    mood.py
    streak_calculator.py    pure, deterministic
  db/
    models.py           SQLAlchemy 2 declarative
    session.py          async engine + AsyncSession dep
  alembic/
    versions/0001_init.py
tests/
  test_day_key.py
  test_mood.py
  test_streak_calculator.py
  test_routes_entries.py
  test_routes_stats.py
  conftest.py           in-memory SQLite for tests
pyproject.toml          uv-managed, ruff+mypy strict
alembic.ini
```

### 2.1 REST endpoints

| Method | Path                | Body / Query                       | 200 shape                        | Errors                     |
| ------ | ------------------- | ---------------------------------- | -------------------------------- | -------------------------- |
| POST   | `/entries`          | `MoodEntryCreate`                  | `MoodEntry`                      | 422 on invariant violation |
| GET    | `/entries`          | `?from=<DayKey>&to=<DayKey>`       | `list[MoodEntry]` (asc recorded) | 400 on `from > to`         |
| GET    | `/entries/{id}`     | —                                  | `MoodEntry`                      | 404                        |
| PATCH  | `/entries/{id}`     | `MoodEntryPatch` (mood?, note?)    | `MoodEntry`                      | 404 / 422                  |
| DELETE | `/entries/{id}`    | —                                  | `204`                            | 404                        |
| GET    | `/streak`           | `?as_of=<iso datetime>&tz=<IANA>`  | `Streak`                         | —                          |
| GET    | `/weekly-mood`      | `?as_of=<iso datetime>&tz=<IANA>`  | `WeeklyMood`                     | —                          |

- All datetimes UTC on the wire. Server converts to the requested IANA tz
  for bucket computation.
- `MoodEntryCreate.recorded_at` is optional; server defaults to
  `datetime.now(UTC)` on omission.
- All routes typed via `response_model=...` so `/openapi.json` is
  authoritative.

### 2.2 Persistence

- SQLAlchemy 2 async, SQLite for dev/CI, driver `aiosqlite`.
- Table `mood_entries(id UUID PRIMARY KEY, recorded_at TIMESTAMP NOT NULL,
  mood INTEGER NOT NULL CHECK(mood BETWEEN 1 AND 5),
  note TEXT NULL CHECK(note IS NULL OR (length(trim(note)) BETWEEN 1 AND 280)))`.
- One Alembic revision (`0001_init`) creates the table + index on
  `recorded_at`.

### 2.3 Test plan (backend)

Minimum coverage — the tester must produce a proper subset ≥ this list:

- `DayKey` — Feb 30 rejected, Feb 29 leap vs non-leap, month/day bounds,
  round-trip JSON, `previous()`/`next()` across month + year rollover.
- `Streak` biconditional — construction rejects violations; decode rejects
  violations; empty streak equals `Streak(0, 0, None)`.
- `StreakCalculator` — empty → empty; single entry today → `(1, 1, today)`;
  2-day run yesterday+today → `(2, 2, today)`; gap of one day → `current`
  resets, `longest` preserved; DST spring-forward day still counts as one
  civil day.
- Routes — full CRUD happy path; `from > to` → 400; unknown id → 404;
  create with `mood=6` → 422; PATCH preserves untouched fields.

## 3. Frontend contract (`frontend-web`)

Layout:

```
src/
  main.ts
  router.ts
  api/
    client.ts             typed fetch wrapper
    schemas.ts            TS mirrors of §1 (Zod-parseable)
  stores/
    entries.ts            Pinia — optimistic add/update + rollback
    stats.ts              Pinia — streak + weekly mood
  views/
    HomeView.vue          Streak card + WeeklyMood chart
    NewEntryView.vue      mood picker + optional note
    EntryDetailView.vue   view / edit / delete
  components/
    MoodPicker.vue
    StreakCard.vue
    WeeklyMoodChart.vue   plain SVG, no chart lib
tests/
  unit/
    entries.store.spec.ts
    stats.store.spec.ts
    MoodPicker.spec.ts
    StreakCard.spec.ts
  contract/
    schemas.spec.ts       DayKey/MoodEntry/Streak round-trip
package.json              pnpm, TS strict, ESLint (flat), Vitest
vite.config.ts
tsconfig.json             "strict": true, no "any"
```

### 3.1 Router

| Path             | View               |
| ---------------- | ------------------ |
| `/`              | `HomeView`         |
| `/entries/new`   | `NewEntryView`     |
| `/entries/:id`   | `EntryDetailView`  |

### 3.2 Store behaviour (invariants)

- `entriesStore.add(entry)` — optimistic insert into local state, POST,
  reconcile with server response on 2xx, roll back on non-2xx.
- `entriesStore.update(id, patch)` — same shape.
- `statsStore.refreshStreak(asOf)` — pure read; no optimistic path.
- Stores must never leave the UI in a state that violates the Streak
  biconditional (i.e. render only server-confirmed streak values).

### 3.3 Test plan (frontend)

- `schemas.spec.ts` — encode-then-decode round-trip for `DayKey`,
  `MoodEntry` (with + without note), `Streak` (all four biconditional
  corners), `WeeklyMood`.
- `entries.store.spec.ts` — happy add; rollback on 500; PATCH preserves
  untouched fields; DELETE removes local entry.
- `MoodPicker.spec.ts` — emits `update:modelValue` with the picked
  `MoodValue`; disables submit when nothing picked.
- `StreakCard.spec.ts` — renders `current` and `longest`; shows empty
  state when `last_logged_day` is `null`.

## 4. What the shakedown measures

Per role, over 7 runs (baseline + B/C/D/E/F/G — see `RESULTS.md` once
matrix runs):

- **Pass@1** — does `uv run pytest -q` return 0 AND `pnpm test:unit`
  return 0 AND `pnpm build` succeed on the first end-to-end pass?
- **Preamble hits** — count of non-schema-conformant assistant preamble
  tokens leaked before `verdict:`.
- **Boundary scanner hits** — see `SCANNERS.md` (per-run count of
  forbidden-pattern occurrences: SQLAlchemy 1.x style, Pydantic v1
  `class Config`, Options API, `<script>` without `setup`, `any` in TS,
  `.value` misuse in Composition API, `async_sessionmaker` missing, …).
- **Test coverage stability** — number of tests written per role prompt,
  standard deviation across runs.
- **Cost** — tokens per run, extrapolated to a $/feature at ~10 features.

The composite nature (bĕ+fronts share the domain via `/openapi.json`) is
itself a scanner: if backend Pydantic and frontend TS disagree on any of
the six §1 types, the frontend contract tests fail.
