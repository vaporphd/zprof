# MoodJournal-Kt — Feature Spec

Mirror of the pyweb `MoodJournal-Web` feature used for the `python-web-eval`
shakedown, ported to a **Ktor 3.0 server + JDBC (Exposed 0.55) + Postgres 16**
Kotlin JVM app. Deliberately identical in domain shape so cross-stack results
(iOS Swift ↔ Kotlin Multiplatform ↔ Python/Vue ↔ Kotlin JVM) are calibrated
on the same invariant surface — Streak biconditional, DayKey validity,
kotlinx.serialization / Pydantic / Codable / TS round-trip preservation.

Consumers of this doc: `kotlin-jvm-eval` shakedown hosts, both
`backend-kotlin-jvm` AND `issue-loop-github-strict` overlays are exercised in
one composite run. Additionally, this shakedown includes a **dry-run of the
issue-loop process pipeline** against a real disposable GitHub repository
— validating that planner→implementer→pr-shepherd actually call `gh` end
to end.

## 1. Domain (single source of truth)

The domain lives as pure-Kotlin `data class` + `sealed class` in a
`core-model` module; the API layer serializes via kotlinx.serialization
with a single Koin-provided `Json` instance.

### 1.1 `MoodValue`

`enum class` with values `1 ... 5`. Semantic labels:

| Value | Label     |
| ----- | --------- |
| 1     | `LOW`     |
| 2     | `MEH`     |
| 3     | `NEUTRAL` |
| 4     | `GOOD`    |
| 5     | `GREAT`   |

- `enum class MoodValue(val raw: Int) { LOW(1), MEH(2), NEUTRAL(3), GOOD(4), GREAT(5) }`.
- Custom `KSerializer<MoodValue>` serialises as the raw `Int` (schema stability
  across renames). Rejects any int outside `1..5` with a typed exception,
  translated to HTTP 422 in the API layer.

### 1.2 `MoodNote`

Value class wrapping `String`. Invariant: `1 ≤ text.trim().length ≤ 280`.

- `@JvmInline value class MoodNote private constructor(val raw: String)`.
- `MoodNote.of(String): Result<MoodNote>` factory; `Result.failure(MoodError.InvalidNote)` on violation.
- Round-trip preserves the pre-strip text exactly (strip is only for the non-empty check).

### 1.3 `DayKey`

Calendar-day identifier bound to a calendar identifier.

- `data class DayKey(val year: Int, val month: Int, val day: Int, val calendar: String = "gregorian")`.
- Validation on construction and on decode: the `(year, month, day)` triple
  must name a real civil day in the specified calendar. Feb 30, Apr 31,
  Feb 29 in non-leap years all rejected via `require { ... }` inside `init` block.
- `previous(): DayKey` / `next(): DayKey` step by one civil day; roll-over
  across months, years, leap-years must be correct.
- Implements `Comparable<DayKey>` (by year, then month, then day; calendar
  mismatch throws).
- JSON shape: `{"year": ..., "month": ..., "day": ..., "calendar": "gregorian"}`.
- Backed by `kotlinx.datetime.LocalDate` internally for arithmetic; the
  public surface stays on the `data class` shape for wire stability.

### 1.4 `MoodEntry`

- `id: UUID` (default `UUID.randomUUID()`).
- `recordedAt: Instant` (`kotlinx.datetime.Instant`, UTC on the wire).
- `mood: MoodValue`.
- `note: MoodNote?` (nullable).

### 1.5 `WeeklyMood`

Aggregate over the last 7 civil days ending at `asOf` (inclusive), in the
user's calendar.

- `buckets: List<DayBucket>` — MUST be `kotlinx.collections.immutable.ImmutableList<DayBucket>`
  (list-typed fields on `data class` cross a boundary; immutable is the
  convention). Length exactly 7, ordered ascending by day.
- `DayBucket(day: DayKey, averageMood: Double?, entryCount: Int)`.
  `averageMood` is `null` iff `entryCount == 0`; otherwise it is the
  arithmetic mean of `mood.raw` for that day's entries.
- `generatedAt: Instant`.

### 1.6 `Streak` (biconditional invariant)

- `current: Int` — must be `≥ 0`; count of consecutive days ending at
  *today* that have at least one entry.
- `longest: Int` — must be `≥ max(current, 0)`; longest such consecutive-day
  run over the entire history.
- `lastLoggedDay: DayKey?`.

**Biconditional invariant** — validated on every construction path AND on
decode:

> `current == 0 && longest == 0 ⇔ lastLoggedDay == null`.

Enforced via `init { require(...) { ... } }` on the `data class` and inside
a custom `KSerializer<Streak>` (or `@Serializable` companion validator) that
throws `SerializationException` on decode of a violating payload.

## 2. Backend contract (`backend-kotlin-jvm`)

Layout (multi-module Gradle):

```
settings.gradle.kts                 include("core-model", "api-server", "runner")
build.gradle.kts                    subprojects { } — plugins, JDK 21, ktlint,
                                                    JUnit5+Kotest wiring,
                                                    integrationTest source set
gradle/libs.versions.toml           version catalog
gradle/wrapper/*                    Gradle 8.9

core-model/                         pure-Kotlin domain — no I/O, no framework
  build.gradle.kts                  minimal — kotlinx.coroutines-core, kotlinx.serialization,
                                              kotlinx.datetime, kotlinx.collections.immutable
  src/main/kotlin/com/example/moodjournal/domain/
    model/
      DayKey.kt
      MoodValue.kt
      MoodNote.kt
      MoodEntry.kt
      WeeklyMood.kt                 (contains DayBucket)
      Streak.kt
    error/
      MoodError.kt                  sealed class : Exception (InvalidNote, NotFound,
                                                              Persistence, Unknown)
    service/
      StreakCalculator.kt           pure, deterministic — no I/O
  src/test/kotlin/com/example/moodjournal/domain/
    DayKeyTest.kt
    MoodValueTest.kt
    MoodNoteTest.kt
    StreakTest.kt
    StreakCalculatorTest.kt

api-server/                         Ktor 3.0 server + persistence
  build.gradle.kts                  depends on :core-model,
                                    ktor-server-core / ktor-server-netty / ktor-server-content-negotiation,
                                    ktor-serialization-kotlinx-json,
                                    exposed-core / exposed-jdbc / exposed-java-time,
                                    hikaricp, postgresql (runtimeOnly),
                                    slf4j-simple
  src/main/kotlin/com/example/moodjournal/api/
    Application.kt                  fun main() — Netty engine + module()
    Module.kt                       fun Application.moodJournalModule() — plugins + routing
    routes/
      EntryRoutes.kt                /entries CRUD
      StatsRoutes.kt                /streak, /weekly-mood
    dto/
      MoodEntryDto.kt               @Serializable — POST/GET DTO
      MoodEntryPatchDto.kt          @Serializable — PATCH DTO (nullable fields)
      MoodEntryCreateDto.kt         @Serializable — request-only
    mapper/
      EntryMapper.kt                object with DTO ↔ Domain extension functions
  src/main/kotlin/com/example/moodjournal/data/
    db/
      DatabaseFactory.kt            HikariCP + Exposed init; reads DATABASE_URL env
      MoodEntriesTable.kt           Exposed Table object; UUID PK, TIMESTAMP recorded_at,
                                                            INT mood, TEXT note (nullable)
    repository/
      MoodEntryRepository.kt        open class — persist, findAll, findById, delete
    datasource/
      MoodEntryLocalDataSource.kt   Exposed newSuspendedTransaction { } wrapper
  src/main/kotlin/com/example/moodjournal/di/
    AppModule.kt                    composition-root object — no framework DI (overlay default)
                                    — wires DatabaseFactory → DataSource → Repository → Routes
  src/main/resources/
    application.conf                Ktor HOCON — port, HikariCP pool size
    logback.xml                     — level=INFO, pattern with timestamp
  src/test/kotlin/com/example/moodjournal/api/
    EntryRoutesTest.kt              testApplication { } — happy path CRUD
    StatsRoutesTest.kt              testApplication { } — /streak, /weekly-mood
    EntryMapperTest.kt              DTO ↔ Domain round-trip
  src/integrationTest/kotlin/com/example/moodjournal/
    MoodJournalIT.kt                Testcontainers Postgres 16-alpine + real routes;
                                    the FULL CRUD+streak+weekly journey against real DB

runner/                             composition root (dev/prod entry — thin)
  build.gradle.kts                  depends on :api-server, applies application plugin
                                    application { mainClass.set("com.example.moodjournal.api.ApplicationKt") }
  src/main/kotlin/com/example/moodjournal/runner/
    Runner.kt                       fun main() — delegates to api.Application.main
```

### 2.1 REST endpoints

| Method | Path                | Body / Query                       | 200 shape                        | Errors                       |
| ------ | ------------------- | ---------------------------------- | -------------------------------- | ---------------------------- |
| POST   | `/entries`          | `MoodEntryCreateDto`               | `MoodEntryDto`                   | 422 on invariant violation   |
| GET    | `/entries`          | `?from=<DayKey>&to=<DayKey>`       | `List<MoodEntryDto>` (asc recorded) | 400 on `from > to`; 422 on parse |
| GET    | `/entries/{id}`     | path `id: UUID`                    | `MoodEntryDto`                   | 404 not found; 422 on non-UUID path parse |
| PATCH  | `/entries/{id}`     | `MoodEntryPatchDto` (mood?, note?) | `MoodEntryDto`                   | 404 / 422                    |
| DELETE | `/entries/{id}`     | path `id: UUID`                    | `204 No Content`                 | 404 / 422                    |
| GET    | `/streak`           | `?as_of=<iso Instant>&tz=<IANA>`   | `StreakDto`                      | 400 on bad tz                |
| GET    | `/weekly-mood`      | `?as_of=<iso Instant>&tz=<IANA>`   | `WeeklyMoodDto`                  | 400 on bad tz                |

- All datetimes UTC on the wire. Server converts to the requested IANA tz
  via `kotlinx.datetime.TimeZone.of(tz)` for bucket / streak computation.
- `MoodEntryCreateDto.recordedAt` is optional; server defaults to
  `Clock.System.now()` on omission — Clock is injected via composition
  root (`Clock.System` in prod, `Clock.fixed(...)` in tests).
- Non-UUID path parameter (`/entries/{id}`) MUST yield 422, not 404. The
  overlay's Haiku-shortcut watch list flags `entry_id: str` + manual parse
  as an anti-pattern (§4 boundary scanners). Ktor's `Uuid` path converter
  (or a typed extractor) handles this cleanly.

### 2.2 Persistence

- Postgres 16 in production; **Testcontainers `postgres:16-alpine` in
  integrationTest**; H2 in-memory (or embedded Postgres via `pg_embedded`)
  for pure-unit tests inside `src/test/`.
- HikariCP pool (min 2, max 10). `DATABASE_URL` env var per 12-factor.
- Exposed 0.55.0 (JDBC layer). Table:
  ```kotlin
  object MoodEntriesTable : UUIDTable("mood_entries") {
      val recordedAt = timestamp("recorded_at").index()
      val mood = integer("mood").check { (it greaterEq 1) and (it lessEq 5) }
      val note = text("note").nullable()
      // trim length check enforced at Repository level (Exposed lacks portable
      // CHECK for TRIM+LENGTH across dialects — the invariant is the domain's
      // MoodNote value class + a Repository-level guard)
  }
  ```
- Exposed migrations via `SchemaUtils.create(MoodEntriesTable)` at
  application start for shakedown simplicity (a real project would use
  Flyway or Liquibase — this is `docs/reviews`, not prod).

### 2.3 Test plan (backend)

Minimum coverage — the tester MUST produce a proper superset of this list:

- `DayKeyTest` — Feb 30 rejected, Feb 29 leap vs non-leap, month/day
  bounds, round-trip JSON, `previous()`/`next()` across month + year
  rollover, `Comparable` sort order.
- `StreakTest` — biconditional construction rejects violations
  (all four corners); decode rejects violations; `Streak(0, 0, null)`
  round-trips.
- `StreakCalculatorTest` — empty history → `Streak(0, 0, null)`; single
  entry today → `Streak(1, 1, today)`; 2-day run yesterday+today →
  `Streak(2, 2, today)`; gap of one day → `current` resets, `longest`
  preserved; DST spring-forward day still counts as one civil day (test
  with a US tz around March 2026).
- `MoodValueTest` — decode `0`/`6`/negative → `SerializationException`;
  encode `GREAT` → literal integer `5`.
- `MoodNoteTest` — `""` rejected, `"   "` rejected, `"a"` accepted,
  `"a".repeat(280)` accepted, `"a".repeat(281)` rejected, round-trip
  preserves interior whitespace.
- `EntryMapperTest` — DTO ↔ Domain round-trip preserves all fields.
- `EntryRoutesTest` (testApplication) — full CRUD happy path;
  `from > to` → 400; unknown id → 404; non-UUID id `foo` → **422** (NOT
  404 — this is F-B in pyweb Haiku shortcut list, ported to JVM);
  create with `mood=6` → 422; PATCH preserves untouched fields; DELETE
  returns 204 and subsequent GET returns 404.
- `StatsRoutesTest` (testApplication) — `/streak` empty user → `(0, 0, null)`;
  `/streak` after 3 entries yesterday+today+today → `(2, 2, today)`;
  `/weekly-mood` shape (7 buckets, correct order, null on empty days).
- `MoodJournalIT` (integrationTest — Testcontainers Postgres) — the FULL
  create→list→streak→delete journey against real Postgres, asserting HTTP
  status + JSON shape + database row count.

Every test above uses:
- JUnit5 `@Test` (`org.junit.jupiter.api.Test`).
- Kotest assertions (`shouldBe`, `shouldThrow<T>`).
- MockK where a collaborator (Repository) is mocked; no MockK in
  integrationTest (real Repository against real DB).
- Turbine only if a Flow is returned (Ktor routes don't).

## 3. Process contract (`issue-loop-github-strict`)

The pipeline agents are exercised against a **real disposable GitHub repo**
created for this shakedown: `vaporphd/zprof-shakedown-ktjvm-<date>` (private).

Dry-run flow per variant host:

1. `planner` (DRAFT mode) reads a hand-authored `docs/PROJECT_SPEC.md` +
   this feature spec, produces `tasks/plan-mood-journal.md` decomposing
   the epic into 6 dependency-ordered issues (a-f):
   - a — `core-model` DayKey + MoodValue + MoodNote scaffolding
   - b — `core-model` MoodEntry + WeeklyMood + Streak
   - c — `core-model` StreakCalculator + tests
   - d — `api-server` Exposed DAO + MoodEntryRepository
   - e — `api-server` routes + DTOs + mapper (depends on d)
   - f — `api-server` integrationTest against Testcontainers Postgres (depends on e)
2. `plan-reviewer` (base gate) returns `approve` on the plan.
3. `planner` (AUTHOR mode) calls `gh issue create` for each — captures
   real issue numbers. Also creates an epic issue linking all 6.
4. `architect` dispatched on issue #a (ADR-trigger=yes — "new external
   dependency: Exposed 0.55.0 + HikariCP + Postgres driver") — produces
   `docs/adr/0002-choose-exposed-over-jooq.md`.
5. `implementer` picks issue #a → creates branch `issue-<N>-daykey-scaffold`
   → writes code → runs `./gradlew :core-model:build test ktlintCheck` →
   commits → pushes → `gh pr create` with `Closes #<N>`.
6. `integration-gate` — for PRs touching `api-server/src/main/kotlin/**` or
   `api-server/src/integrationTest/kotlin/**` (that's `<INTEGRATION_SCOPE>`
   for this project), dispatched between implementer and reviewer.
7. `wiki-keeper` — for every PR (§0.4 MAINTAIN mode after integration-gate
   green), updates `docs/wiki/` atomically.
8. `reviewer` — reads diff, posts `gh pr review --approve` or `--request-changes`.
9. `pr-shepherd` (AUTO_MERGE=on for shakedown) — squash-merges via
   `gh pr merge <N> --squash --delete-branch`, verifies post-merge state.
10. `spec-maintainer` post-merge — updates `docs/PROJECT_SPEC.md § Recent
    merges` + `## Milestone progress`.
11. `docs-writer` post-merge — reconciles `tasks/todo.md` + replaces
    `followup.md` snapshot.

Only issue #a needs to complete this full cycle per shakedown variant
(6 issues × 6 variants = 36 full cycles is out of scope). Issue #a
exercises every agent in the loop with the smallest possible diff.

## 4. Boundary scanners (this overlay's forbidden patterns)

Scanners are grepped over the generated code per variant. Zero hits =
tier-appropriate. See §5 of `RESULTS.md` for per-variant counts.

- **`runCatching { }` in `**/domain/**` or `**/data/**`** —
  ```
  grep -rnE '\brunCatching\s*\{' --include='*.kt' core-model/src/main api-server/src/main | \
    grep -E '(/domain/|/data/)'
  ```
  Cause: swallows `CancellationException` (F-15). Zero legitimate uses in
  production paths.

- **Double-bang `!!`** —
  ```
  grep -rnE '(^|[^!])!![^=]' --include='*.kt' core-model/src/main api-server/src/main
  ```
  Zero legitimate uses.

- **Second `Json { }` instance** —
  ```
  grep -rn "Json\s*{" --include='*.kt' core-model/src/main api-server/src/main
  ```
  Must return exactly one hit — the composition-root declaration. Two = review-blocker.

- **`GlobalScope` / `runBlocking` in production** —
  ```
  grep -rnE '(GlobalScope\.|\brunBlocking\s*\{)' --include='*.kt' core-model/src/main api-server/src/main | \
    grep -v 'fun main('
  ```
  Zero hits.

- **`println` / `System.out.println` in production** —
  ```
  grep -rnE '(^|\s)println\s*\(|System\.out\.println' --include='*.kt' core-model/src/main api-server/src/main
  ```
  Zero hits — logging via slf4j / logback.

- **JVM-only bans in `core-model/**`** —
  ```
  grep -rnE '^import (io\.ktor\.|org\.jetbrains\.exposed\.|com\.zaxxer\.hikari)' \
    --include='*.kt' core-model/src/main
  ```
  Zero — `core-model` is I/O-free.

- **Path param typed as `String` + manual UUID parse (Haiku shortcut F-B port)** —
  ```
  grep -rnE 'call\.parameters\["id"\][^\n]*(!!|\?:\s*)' --include='*.kt' api-server/src/main
  ```
  Should be zero if the routes use typed extraction (`call.parameters.getOrFail<Uuid>("id")`
  or equivalent). Any hit → likely mis-routing non-UUID to 404 instead of 422.

- **Self-swallowing `try/except`-analog in tests (Haiku shortcut F-C port)** —
  ```
  grep -rnE 'catch\s*\([^)]*\)\s*\{\s*\}' --include='*.kt' \
    core-model/src/test api-server/src/test api-server/src/integrationTest
  ```
  Zero. Empty catch = swallowed assertion.

- **Fabricated dep versions (Haiku shortcut F-D port)** —
  ```
  # Cross-check every version in libs.versions.toml against Maven Central or
  # a known-good pin list (per overlay claude-block.md). This is a manual
  # scanner: list the pinned versions per variant and verify they resolve.
  grep -E '^(kotlin|kotest|junit|mockk|ktor|exposed|hikaricp|kotlinx-|logback|slf4j)' \
    gradle/libs.versions.toml
  ```

- **`Throwable` catch or bare `Exception` outside `execute`** —
  ```
  grep -rnE '\bcatch\s*\(\s*[a-z]+:\s*(Throwable|Exception)\s*\)' \
    --include='*.kt' core-model/src/main api-server/src/main | \
    grep -v 'execute(' # allowed inside UseCase/Service.execute
  ```
  Zero outside `execute`.

## 5. What the shakedown measures

Per role, over 7 runs (baseline + B/C/D/E/F/G — see `RESULTS.md`):

- **Pass@1** — does `./gradlew :core-model:test :api-server:test build
  ktlintCheck` return 0 AND `./gradlew integrationTest` return 0 on the
  first end-to-end pass? Boolean per variant.
- **Preamble hits** — count of non-schema-conformant assistant preamble
  tokens leaked before `verdict:` per agent per role.
- **Boundary scanner hits** — §4 count per variant.
- **Test count stability** — how many tests written per role prompt;
  standard deviation across variants (a proxy for tier consistency).
- **Reviewer findings** — Critical / Important / Nit per variant.
- **Cost** — tokens per run, extrapolated to a `$/feature` at ~10 features.
- **Process-side (issue-loop) validation** — for each variant: did
  `planner` (AUTHOR) successfully create real GitHub issues? Did
  `implementer` push a real branch? Did `pr-shepherd` successfully
  squash-merge? Any process-agent failure is a **process-BLOCK** independent
  of code-quality metrics.

Cross-stack calibration: results feed into the `RESULTS.md` §4 hypothesis
verdicts, comparable to the pyweb + KMP + iOS runs on the same MoodJournal
domain. The critical hypothesis this shakedown tests:

> **H-JVM-1** — Kotlin JVM's mature training footprint (Ktor 3.0, Exposed,
> JUnit5, MockK all long-tenured on JVM) shifts the Haiku viability line
> UPWARD vs pyweb's FastAPI (where Haiku failed on backend but succeeded
> on Vue). Baseline hypothesis: Haiku impl produces a green Ktor
> service; final tier map ends up MORE haiku-tolerant than pyweb.

Rejection of H-JVM-1 (Haiku impl produces Critical bugs on the JVM stack
too) would generalize the pyweb finding — "backend tier viability is
independent of stack maturity; the FastAPI-specific issues WEREN'T
FastAPI-specific". Confirmation would suggest Python's async/pydantic
combo is uniquely hostile to Haiku vs Kotlin's simpler serialization story.
