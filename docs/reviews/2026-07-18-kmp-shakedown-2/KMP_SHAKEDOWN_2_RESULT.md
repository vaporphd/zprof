# KMP overlay shakedown #2 — full pipeline with real toolchain enforcement

Second empirical validation of the `kotlin-multiplatform` overlay. Where shakedown-1 skipped `./gradlew` build + tests + lint due to unprovisioned toolchain, shakedown-2 runs the real thing on JDK 21 / Gradle 8.9 / Xcode 26 / Node 24 (host `zprof-test-kmp-2`). Android target is disabled — no Android SDK provisioned.

## Method

- Fresh project `/Volumes/mydata/zprof-test-kmp-2/`.
- `zprof apply kotlin-multiplatform` — 19 agents.
- Full pipeline, no skipped rules:
  1. **init-kmp** — actual Gradle scaffold. iOS + Desktop + Web targets active; Android commented out with `TODO(android)` markers.
  2. **architect × 2** — bootstrap PROJECT_SPEC + ADR-0001; then ADR-0002 for MoodJournal feature. ADR-0002 explicitly authorizes `androidx.compose.runtime.*` in commonMain (Compose MP re-export exemption per shakedown-1 finding).
  3. **implementer** — 15-file MoodJournal slice. **REAL `./gradlew` build**: `compileCommonMainKotlinMetadata`, `compileKotlinIosSimulatorArm64`, `compileKotlinDesktop`, `compileKotlinJs` — all green. **REAL `ktlintCheck` + `detekt`** — clean (detekt vacuously so — no config).
  4. **scaffold fix** — `fix(scaffold): enable Mokkery plugin` (init-kmp bundled it with the Android TODO block; Mokkery is KMP-native).
  5. **tester × 2** — first blocked on Mokkery-plugin-missing; second dispatched after scaffold fix, wrote 14 tests, `./gradlew :shared:desktopTest` — **14/14 pass**. Log+Component tests blocked by architectural conflict (below).
  6. **reviewer** — deep audit + actual `ktlintCheck`/`detekt`/`desktopTest` re-runs. Report at `/Volumes/mydata/zprof-test-kmp-2/docs/reviews/2026-07-18-kmp-moodjournal-shakedown2/report.md`.

Session ID: `445b990e-2263-4383-8415-a8addba72189`.

## Real-toolchain wins

Every check shakedown-1 skipped now ran for real and stayed green (where the overlay was correct):

| Check | shakedown-1 | shakedown-2 |
|---|---|---|
| Gradle project actually builds | not run | **BUILD SUCCESSFUL** |
| `:shared:compileCommonMainKotlinMetadata` | not run | **green** |
| `:shared:compileKotlinIosSimulatorArm64` | not run | **green** |
| `:shared:compileKotlinDesktop` | not run | **green** |
| `:shared:compileKotlinJs` | not run | **green** |
| `./gradlew ktlintCheck` | not run | **clean** |
| `./gradlew detekt` | not run | vacuously clean (no config — flagged `[I]`) |
| `:shared:desktopTest` | not run | **14/14 pass** |
| iOS SwiftUI scaffold | never generated | `iosApp.xcodeproj` present, ContentView.swift generated |
| Vite + Vue 3 scaffold | never generated | `webApp/` present |

## KMP boundary metrics — same as shakedown-1

```
android.*/androidx.* (non-compose) in commonMain:   0
androidx.compose.* in commonMain (ADR-exempt):      1  ← the @Immutable annotation, correctly restored per ADR-0002
Foundation/UIKit in commonMain:                     0
java.io.File / java.time / concurrent:              0
Dispatchers.IO in commonMain (real code):           0  ← the 2 grep hits are documentation comments, not code
expect outside core/:                               0
Hilt / Retrofit / Room / MockK / etc:               0
import Combine:                                     0
Json { instances (should be exactly 1):             1  ← in core/network/HttpClientFactory.kt
```

The eight hard-blocks that shakedown-1 also verified — all still zero. The @Immutable annotation is BACK (implementer used it correctly this time per ADR-0002 exemption).

## Real architectural findings that shakedown-1 could not surface

Shakedown-1 skipped `./gradlew build`. Skipping the toolchain hides an entire class of overlay-contract bugs — the kind that only surface when the code actually tries to compile+link+run. Shakedown-2 surfaced **three**:

### Finding 1 — Mokkery vs final classes (Critical, overlay contract-level)

The tester attempted to write `LogMoodEntryUseCaseTest` and `MoodJournalComponentTest` — both need to mock `MoodJournalRepository` / `LogMoodEntryUseCase` / `CurrentStreakUseCase`. Mokkery 2.4.0 rejected the compile with `FINAL_TYPE_CANNOT_BE_INTERCEPTED`.

**Root cause:** ADR-0002 (following the overlay's implementer §3.4) mandates "Repository is a concrete class (no interface unless ADR justifies)". Concrete classes are `final` by default in Kotlin. Mokkery 2.4.0's compiler-plugin transform requires open classes or interfaces — it will not mock finals.

**Contradiction:** the overlay contract has two rules that oppose each other on the same tool:
- implementer §3.4 — concrete class default
- tester §3.8 — "Mokkery is the KMP-native mock lib. MockK is BANNED — JVM-only."

Result: 2 of 4 planned test suites cannot compile.

**Options for resolution (proposed by reviewer):**
- A. Mark Repository/UseCase classes `open` — least intrusive.
- B. Reintroduce protocol-oriented interfaces (`interface MoodJournalRepository` + `class MoodJournalRepositoryImpl : MoodJournalRepository`) — most intrusive, reverses ADR.
- C. Switch to a Mokkery config that opens final classes — verify it exists.
- D. Emit only integration tests that hit real fakes (hand-rolled `FakeRepository`) — bypasses Mokkery for mockable seams.

Reviewer recommends **A** (least disruption).

**This is a real finding at the OVERLAY level, not the code level.** It affects every future feature slice under kotlin-multiplatform. A supersede ADR is needed.

### Finding 2 — Detekt vacuously clean (Important)

The overlay ships `alias(libs.plugins.detekt)` and every contract promises "detekt clean before commit". But without a `detekt.yml` config in-tree, detekt reports NO-SOURCE and does not scan anything. The green light means nothing.

**Fix:** init-kmp should emit a starter `detekt.yml` on scaffold. Not the implementer's or reviewer's job to add it — it's a scaffold hole.

### Finding 3 — Mokkery plugin bundled with Android TODO (Critical, scaffold-level)

init-kmp emitted `alias(libs.plugins.mokkery)` INSIDE the Android-disabled TODO block, so it was commented out along with `alias(libs.plugins.android.library)`. Mokkery is KMP-native — decoupled from the Android target.

**Root cause:** init-kmp's `shared/build.gradle.kts` template treats Mokkery as an Android dependency. It isn't.

**Fix applied for the shakedown:** commit `078fcb8` uncommented the Mokkery line unconditionally. The init-kmp contract should be updated to always enable Mokkery regardless of target set.

## Tier-1 zprof eval scorecard

| Role | N | Pass@1 | Median tok | ApT | Compliance |
|---|--:|--:|--:|--:|---|
| init-kmp (bucketed as "other") | 1 | 1.00 | 141,943 | 0.7 | clean |
| architect | 2 | 1.00 | 110,047 | 1.0 | clean |
| implementer | 1 | 1.00 | 150,677 | 0.7 | clean |
| tester | 2 | **0.00** | 163,492 | 0.0 | 2× artifact-missing + 1× preamble |
| reviewer | 1 | 1.00 | 144,899 | 0.7 | 1× preamble |

**7 dispatches, 904,861 subagent tokens.** All ran on `claude-opus-4-7[1m]`.

**Tester Pass@1 = 0.00** is the correct signal — both dispatches ended with `verdict: blocked` because of Finding 1 (Mokkery vs final classes). The eval accurately reflects the empirical block. Not a role failure; a real architectural conflict surfacing at test-write time.

**Reviewer + tester preamble** — 1 preamble each. Reviewer's opening line was "Report file written on disk. Returning the required verdict block." — a status-report before the schema. Tester's opening line was "The mokkery plugin is commented out in `shared/build.gradle.kts`..." — a narration before the schema. Both Opus 4.7[1m] dispatches with a residual narration prior. Not the sonnet-5 pattern; a different regression to investigate.

## Comparison: shakedown-1 vs shakedown-2

| Dimension | shakedown-1 | shakedown-2 |
|---|---|---|
| Overlay contracts followed | yes | yes |
| KMP boundary scanners | 8/8 clean | 8/8 clean (once comments are excluded) |
| `@Immutable` handling | implementer wrongly stripped it | correctly kept (ADR-0002 explicit exemption) |
| Gradle build actually run | **skipped** | **all 4 targets green** |
| Static analysis actually run | **skipped** | ktlint green, detekt vacuous |
| Tests actually run | **skipped** | 14/14 green on `desktopTest` |
| Findings on real code | 3 Critical + 8 Important + 3 Minor | 3 Critical + 6 Important + 2 Minor |
| Findings on **overlay contract** | 0 | **3 real Criticals** — Mokkery-vs-final, Mokkery scaffold placement, detekt no-config |
| Preamble | 0/8 | 2/7 (Opus preamble under long-context multi-step task) |
| Total tokens | 982,949 | 904,861 |

**shakedown-2 is the more informative run** — real toolchain enforcement flushed out overlay-contract bugs that shakedown-1 could not see because the bugs live at compile/test time.

## Interpretation

Two orthogonal wins:

1. **The KMP overlay boundary rules HOLD under real compilation.** Every hard-block stays zero when the code actually goes through the compiler. The `@Immutable` false positive from shakedown-1 was fixable via ADR-0002 documenting the Compose MP exemption.

2. **The KMP overlay contract has 3 real bugs discoverable only with actual toolchain enforcement.** Skipping `./gradlew` in shakedown-1 hid all three. If the doctrine is "shakedown validates the overlay", shakedowns MUST run the toolchain end-to-end.

**Action items for the overlay:**
- Follow-up ADR-0003: supersede ADR-0002's "concrete class no interface" for testable classes — either mark them `open` or reintroduce interfaces. Recommend option A (mark `open`).
- Update init-kmp §3.2 shared/build.gradle.kts template — move `alias(libs.plugins.mokkery)` OUT of the Android-conditional block.
- Update init-kmp scaffold to emit a starter `detekt.yml` — the promise of "detekt clean" is vacuous without a config.
- Update reviewer §3.2 to explicitly carve out `androidx.compose.runtime.*` and `androidx.compose.foundation.*` when the shared module applies `org.jetbrains.compose` (was already done in ADR-0002; the reviewer contract should incorporate).
- Investigate Opus preamble regression under long multi-step context (2/7 in this shakedown vs 0/8 in shakedown-1).

## Verdict

**KMP overlay boundary rules confirmed a second time; three real contract-level bugs discovered.** The overlay is safer to use after shakedown-2 than after shakedown-1 because the discovered bugs are now visible and fixable rather than lurking.
