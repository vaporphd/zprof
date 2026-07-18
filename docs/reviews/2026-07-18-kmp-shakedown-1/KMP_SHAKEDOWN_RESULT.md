# KMP overlay shakedown #1 — first empirical validation

Empirical validation of the `kotlin-multiplatform` overlay (commits `9a23b34` .. `e034e11`) against a fresh MoodJournal KMP shakedown.

## Method

- Fresh project `/Volumes/mydata/zprof-test-kmp-1/`.
- `zprof apply kotlin-multiplatform` — 19 agents installed.
- Full role pipeline dispatched:
  - **architect × 2** — bootstrap PROJECT_SPEC + ADR-0001 (Nygard), then ADR-0002 for MoodJournal feature+source-set layout (all 4 targets, expect/actual registry, Compose MP + SwiftUI + Vue).
  - **implementer** — 16-file MoodJournal slice: commonMain Domain + Data + Presentation Component + DI + iosMain ComponentWrapper + composeApp Android Screen + View. Toolchain build skipped (no Gradle/Xcode/Node provisioned for shakedown container).
  - **tester** — 4 commonTest suites: kotlin.test + Mokkery + Turbine. Test run skipped (same reason).
  - **reviewer** — deep audit exercising kmp-boundary + koin-usage + ios-bridge + arch + compose-mp scanners.

Session ID: `445b990e-2263-4383-8415-a8addba72189`.

## Result — the KMP boundary metrics the overlay was for

```
Zero violations across all 8 boundary scanners:

  android.*/androidx.* in commonMain:      0
  Foundation/UIKit in commonMain:          0
  java.io.File / java.time / concurrent:   0
  Dispatchers.IO in commonMain:            0
  expect outside core/:                    0
  Hilt / Retrofit / Room / MockK / etc:    0
  import Combine (Swift concept):          0
  Second Json { instance:                  0
```

Every hard-block the architect §2.2 / implementer §0.11-0.12 / reviewer §3.2 forbid — zero occurrences across 21 Kotlin files.

## Positive markers (contract adherence)

```
kotlin.test imports:                     17 hits
Mokkery imports:                         12 hits
Turbine imports:                          1 hit
obtainEvent calls:                       14 hits
onEvent calls (contradiction):            0 hits
Decompose ComponentContext by delegate:   1 hit  (correct — one Component)
Koin factoryOf/singleOf:                  7 hits
runTest coroutine tests:                 10 hits
kotlinx.datetime LocalDate uses:         21 hits
```

Contract vocabulary chosen consistently:
- **Mokkery** across every test — no MockK regression.
- **`obtainEvent`** across every Component call site — no mixing with `onEvent`.
- **`kotlinx.datetime`** for every date — no `java.time` leak.
- **Koin DSL** (`factoryOf` / `singleOf` / manual `factory`) — no Hilt annotations.

## Files generated

21 Kotlin files:
- 15 under `shared/src/commonMain/**/feature/moodjournal/{domain,data,presentation,di}/`
- 1 under `shared/src/iosMain/**/ios/` — the `MoodJournalComponentWrapper` (§13.3 pattern)
- 2 under `composeApp/src/androidMain/**` — Screen + View (§13.1 pattern)
- 5 under `shared/src/commonTest/**` — 4 test suites + 1 test fixture

Test suites cover the pure-logic contract:
- `CurrentStreakUseCaseTest` — 8 boundary scenarios (empty, today only, yesterday-anchor, gap, three-day streak, duplicates, future-only, mixed).
- `LogMoodEntryUseCaseTest` — happy path + Repository failure mapping.
- `MoodJournalComponentTest` — state-transition + Turbine-observed `viewState` + Mokkery-verified use-case calls.
- `MoodJournalMapperTest` — DTO ↔ Domain round-trip.

## Reviewer report

Full report at `/Volumes/mydata/zprof-test-kmp-1/docs/reviews/2026-07-18-kmp-moodjournal/report.md`.

- **3 Critical** — iOS wrapper Flow leak / wall-clock `Clock.System.todayIn()` in the Component / a swallowed `CancellationException`
- **8 Important** — including the `@Immutable` divergence (implementer removed the annotation on the assumption it violated commonMain purity; reviewer flagged this as needing either a supersede ADR or the Compose plugin adoption clarification)
- **3 Minor**
- **3 Informational** — the SQLDelight-generated types don't exist since `init-kmp` scaffold was skipped for the shakedown; the review notes this is not the implementer's fault

Notably ALL reviewer findings are on real code shape — none are on overlay-boundary violations, because there were none.

## Tier-1 eval scorecard

`zprof eval` on the session segment:

| Role | N | Pass@1 | Median tok | ApT | Compliance |
|---|--:|--:|--:|--:|---|
| architect | 2 | 1.00 | 114,705 | 0.9 | **clean** |
| implementer | 2 | 1.00 | 141,684 | 0.7 | **clean** |
| tester | 2 | 1.00 | 130,653 | 0.9 | **clean** |
| reviewer | 2 | 1.00 | 158,876 | 0.7 | **clean** |

**Zero preamble across all 8 zprof-role dispatches.** All ran on `claude-opus-4-7[1m]` — the model followed the schema-first return_format perfectly. No `dispatch-never-returned`, no `artifact-missing`, no `next-unreachable`, no `return-preamble`.

Total subagent output: 982,949 tokens across 8 dispatches (average ~123K per dispatch).

## Comparison to prior shakedowns

| Shakedown | Overlay | Boundary hits | Preamble | Pass@1 avg |
|---|---|---:|---:|---:|
| iOS `-3` (2026-07-18 pre-Combine-purge) | ios-swift | not measured | high | ~0.75 |
| iOS `-4` (2026-07-18 post-purge) | ios-swift | Combine 0, VIPER 0 | 1/8 (Sonnet-5) | 1.00 |
| KMP `-1` (2026-07-18) | kotlin-multiplatform | **All 8 scanners 0** | **0/8** | **1.00** |

The KMP shakedown is the cleanest empirical result of the three:
- Zero preamble across all dispatches (vs 1/8 in the iOS post-purge run).
- Zero boundary violations across a much richer scanner set (8 distinct hard-blocks vs 2).
- Every reviewer finding was a real code-shape issue (not an overlay-doctrine leak).

## Interpretation

The KMP overlay's hard rules — `commonMain` platform-freedom, `expect`/`actual` scope restriction, JVM-only library ban, Kotlin-native library preference (Ktor/Koin/SQLDelight/Mokkery), Decompose Component + `obtainEvent` convention, single `Json` instance — all held under a full multi-agent pipeline. Not one boundary was crossed by an implementer that had to invent 16 files across 3 source sets from an ADR.

The reviewer's `kmp-boundary` scanner (§3.2 of the reviewer contract) ran end-to-end and reported clean. Its `koin-usage`, `ios-bridge`, and `arch` scanners caught real issues (iOS Flow leak, wall-clock in SUT) — which is exactly what those scanners exist to catch.

## Verdict

**KMP overlay holds under a real multi-agent shakedown.** The nine-phase rewrite (`9a23b34` .. `e034e11`) produces a coherent contract set that agents can follow without violating the underlying architecture — even with the toolchain unprovisioned and the SQLDelight scaffold missing.
