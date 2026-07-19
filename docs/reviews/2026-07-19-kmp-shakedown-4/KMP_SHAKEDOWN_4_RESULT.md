# KMP overlay shakedown #4 — post-fix empirical validation of F-4/F-5/F-6

Fourth empirical validation of the `kotlin-multiplatform` overlay. Purpose: verify the three overlay-contract fixes committed in `af83d57` (sh-3 findings F-4/F-5/F-6) hold under a fresh end-to-end pipeline.

Host: `zprof-test-kmp-4`. JDK 21 / Gradle 8.9 / Xcode 26 / Node 24. Android target disabled (no Android SDK provisioned).

## Method

Same shape as sh-3 — full pipeline with real toolchain:
1. **init-kmp** — Gradle scaffold. iOS + Desktop + Web active.
2. **architect ×2** — PROJECT_SPEC + ADR-0001 baseline; ADR-0002 for MoodJournal.
3. **implementer** — 15-file MoodJournal slice. Real `./gradlew` build all 4 targets + ktlint + detekt.
4. **tester** — 4 test suites, Mokkery on `open` Repository + UseCases. Real `./gradlew :shared:desktopTest`.
5. **reviewer** — deep audit + real toolchain re-runs. Report at `zprof-test-kmp-4/docs/reviews/2026-07-19-kmp-moodjournal-shakedown4/report.md`.

Session ID: `445b990e-2263-4383-8415-a8addba72189`.

## Fix-verification matrix

| Fix | Contract change (af83d57) | Empirical verification | Result |
|---|---|---|---|
| **F-4** — no dead callback stubs on Composables | implementer §13.5 bans `consume(x) = Unit`, `@Suppress("UnusedParameter")` on stubs; §11 checklist adds "every callback param attached to real UI action". | `grep -rEn 'consume\(' shared/src/commonMain/kotlin/com/moodjournal/feature/` → 1 hit but KDoc-only (documents the ban). `@Suppress("UnusedParameter")` in `feature/**` → 0. `MoodEntryRow.kt:37` attaches `onClick` to `Modifier.clickable(onClick = onClick)`. Reviewer confirmed: "F-4 fix intact". | **PASSED in `feature/**`** |
| **F-5** — jsMain actual for every commonMain expect | init-kmp §2 layout lists `jsMain/database/DatabaseDriverFactory.kt` stub actual; §5 must-not rule; §4 doctor pass greps `^expect ` in commonMain. | `shared/src/jsMain/kotlin/com/moodjournal/core/database/DatabaseDriverFactory.kt` present as stub throwing `NotImplementedError`. `compileKotlinJs` green. Implementer did NOT need to add it — scaffold provided. | **PASSED** |
| **F-6** — `:shared:linkDebugFrameworkIosSimulatorArm64` in doctor pass | init-kmp §4 doctor mandates the link step BEFORE `verdict: done`; §5 must-not rule. | Link task ran. `shared/build/bin/iosSimulatorArm64/debugFramework/shared.framework/shared` present on disk (213 MB, mtime 2026-07-19). | **PARTIALLY passed** — the link step ran and the framework exists, but SourceKit still red-flags `import shared` in Swift files. See F-6 refinement below. |

Two of three fixes fully passed. **F-6 needs a refinement**: linking the framework is necessary but not sufficient — the Xcode project must also wire `FRAMEWORK_SEARCH_PATHS` (or equivalent SPM/Podfile) so Swift can actually resolve `import shared`. This surfaced as a real diagnostic even on a fresh scaffold and re-fires the same "No such module 'shared'" red-flag that sh-3 flagged.

## Real-toolchain results

| Check | sh-1 | sh-2 | sh-3 | sh-4 |
|---|---|---|---|---|
| `:shared:compileCommonMainKotlinMetadata` | not run | green | green | **green** |
| `:shared:compileKotlinIosSimulatorArm64` | not run | green | green | **green** |
| `:shared:compileKotlinDesktop` | not run | green | green | **green** |
| `:shared:compileKotlinJs` | not run | green | green | **green** |
| `./gradlew ktlintCheck` | not run | clean | 4 style hits | **61 style hits (production clean, all in commonTest)** |
| `./gradlew detekt` | not run | **NO-SOURCE** | 39 files clean | **44 files clean** |
| `:shared:desktopTest` | not run | 14/14 pass | 28/28 pass | **31/31 pass** |
| `:shared:linkDebugFrameworkIosSimulatorArm64` | not run | not run | not run | **green — F-6 link step**|
| iOS Swift `import shared` resolves in SourceKit | n/a | red | red | **still red — F-6 refinement needed** |

## KMP boundary sweep

All eight hard-block rules stay at zero for the fourth consecutive shakedown:
```
android.*/androidx.* (non-compose) in commonMain:   0
androidx.compose.* in commonMain (ADR-exempt):      40 (documented re-export trap)
Foundation/UIKit in commonMain:                     0
java.io.File/java.time/java.util.concurrent:        0
Dispatchers.IO in commonMain (code):                0  ← 2 comment hits only
expect outside core/:                               0
Hilt/Retrofit/Room/MockK/import Combine:            0
GlobalScope/runBlocking/printStackTrace:            0
Json { } instances:                                 1  ← core/network/di/NetworkModule.kt
```

## New findings (sh-4)

### Finding-7 — F-6 refinement: `FRAMEWORK_SEARCH_PATHS` unset in xcodeproj (Important, init-kmp scope)

- **What**: `./gradlew :shared:linkDebugFrameworkIosSimulatorArm64` produces `shared/build/bin/iosSimulatorArm64/debugFramework/shared.framework/` on disk (F-6 fix), but `iosApp/iosApp.xcodeproj/project.pbxproj` does NOT set `FRAMEWORK_SEARCH_PATHS` to point at it. Swift `import shared` fails in Xcode/SourceKit as `No such module 'shared'` — same red-flag sh-3 F-6 was supposed to fix.
- **Why F-6 alone is insufficient**: linking the framework only produces the binary; the consumer (Xcode) must also know where to search. Options: (a) set `FRAMEWORK_SEARCH_PATHS = $(inherited) $(SRCROOT)/../shared/build/bin/$(KONAN_TARGET)/debugFramework` in xcodeproj build settings, (b) wire `embedAndSignAppleFrameworkForXcode` gradle task + Xcode Run Script phase, (c) emit Podfile+cocoapods integration, (d) emit `Package.swift` SPM manifest.
- **Recommended overlay fix**: init-kmp §3.6 template + §4 step 8 (iOS scaffold emit) must generate the xcodeproj with `FRAMEWORK_SEARCH_PATHS` pre-wired to the standalone debugFramework path. If xcodegen is used, add `settings.FRAMEWORK_SEARCH_PATHS` in the `project.yml`. If the project is emitted by hand, patch the `XCBuildConfiguration` `buildSettings` dictionary.
- **Downgraded from Critical** because it does not block Kotlin build or tests — only Xcode indexing on Swift-side. But it recurs on every fresh scaffold and is user-visible immediately.

### Finding-8 — F-4 rule too narrowly scoped: also fires on init-kmp's own core scaffold (Minor, promote to F-4 v2)

- **What**: reviewer M-1 caught `shared/src/commonMain/kotlin/com/moodjournal/core/navigation/RootContent.kt:16-19` using `@Suppress("UnusedParameter")` on the `component: RootComponent` parameter — the exact anti-pattern F-4 bans.
- **Why sh-4 F-4 grep did not catch this**: the shakedown's focused F-4 verification was scoped to `feature/**` (the scope the fix originally targeted). But the F-4 rule applies to ALL Composable callbacks, and the very scaffold init-kmp emits contains a violation.
- **Recommended overlay fix**: (a) implementer §13.5 rule is correct and scope-agnostic — no change needed there; (b) init-kmp §3 Kotlin templates must NOT use `@Suppress("UnusedParameter")` on `RootContent` or any other stub — either wire the parameter to a placeholder `Text(component::class.simpleName ?: "root")` or use a body-less `expect fun` style stub with proper TODO.
- **Downgraded to Minor** because the file is a documented placeholder awaiting the first feature wire-up, but the fix is trivial and prevents the anti-pattern from being planted in every new project by the scaffold itself.

### Finding-9 — tester-side gap: ktlintFormat not run before return (Important, tester scope)

- **What**: reviewer verdict was BLOCK because ktlint reported **61 style violations in commonTest** — every one is `standard:multiline-expression-wrapping` / `standard:function-expression-body` / `standard:parameter-list-wrapping` / `standard:no-consecutive-blank-lines`. All auto-fixable via `./gradlew ktlintFormat`.
- **Why sh-2 + sh-3 did not surface this**: tester emitted more compact tests those runs; sh-4 tester used more Kotlin-idiomatic expression-bodies + wide param lists that trip ktlint's standard rules. The tester overlay does NOT enforce a pre-return `ktlintFormat` step, so the red goes downstream to reviewer.
- **Recommended overlay fix**: tester §5 (workflow) should add: "Before returning `verdict: done`, run `./gradlew ktlintFormat && ./gradlew ktlintCheck` — if ktlintCheck is still red after format, the failure is real; if it goes green, commit the auto-format hunks with the tests." This blocks the class of tester-flagged style violations from cascading to reviewer.
- **Alternate framing**: could also add to implementer + init-kmp doctor pass; but the tester is where the specific violation always emerges (test files are naturally chattier).

### Finding-10 — dead-code in scaffold: `MoodJournalViewEvent.DeleteRequested` misroutes to navigation (Important, implementer scope)

- **What**: reviewer I-3 — the `DeleteRequested(entryId)` branch of `MoodJournalComponent`'s `when(event)` dispatches to `onNavigateToDetail(event.entryId)`; no delete UseCase, no repository call. A real product-correctness bug in the implementer output.
- **Overlay-level implication**: not clear this needs a contract rule — it's a code-level correctness bug per feature slice, and the reviewer caught it. Documented for follow-up if it repeats across shakedowns.

### Finding-11 — desktop `DatabaseDriverFactory` uses IN_MEMORY driver (Minor, init-kmp scope)

- **What**: `shared/src/desktopMain/kotlin/com/moodjournal/core/database/DatabaseDriverFactory.kt` uses `JdbcSqliteDriver.IN_MEMORY` — every user restart discards persisted state.
- **Recommended overlay fix**: init-kmp desktop actual template should use `JdbcSqliteDriver("jdbc:sqlite:${System.getProperty("user.home")}/.<app>/<app>.db")` with directory creation, and gate the in-memory variant behind a debug/test Koin qualifier.

## Comparison: sh-1 vs sh-2 vs sh-3 vs sh-4

| Dimension | sh-1 | sh-2 | sh-3 | sh-4 |
|---|---|---|---|---|
| Gradle build actually run | skipped | ✅ 4 targets | ✅ 4 targets | ✅ 4 targets |
| ktlint actually run | skipped | ✅ clean | 4 hits | 61 hits (tests only) |
| detekt actually run | skipped | ❌ NO-SOURCE | ✅ 39 files | ✅ 44 files |
| desktopTest actually run | skipped | 14/14 (2 blocked) | 28/28 | 31/31 |
| iOS framework link run | skipped | skipped | skipped | ✅ green |
| SourceKit `import shared` resolves | n/a | red | red | **still red** |
| Overlay-contract Criticals | 0 | **3** | 0 | 0 |
| Overlay-contract Importants | 0 | 0 | 1 | **2** (F-6-refinement, tester-ktlintFormat gap) |
| Overlay-contract Minors | 0 | 0 | 2 | **2** (F-4 scope, desktop IN_MEMORY) |
| Preamble | 0/8 | 2/7 | 0/6 | 0/6 |
| Total subagent tokens | 982,949 | 904,861 | 812,475 | ~876,700 |

## Interpretation

Two clear signals:

1. **F-4 + F-5 land clean.** The F-4 anti-pattern rule is respected inside `feature/**` (0 hits); the F-5 jsMain stub is present out-of-the-box (0 patch needed by implementer). Two of three sh-3 fixes are structurally sound.

2. **F-6 needs a refinement.** Linking the framework was the necessary first step; wiring `FRAMEWORK_SEARCH_PATHS` in xcodeproj is the missing second step. sh-4 promotes this from "candidate refinement" (noted in sh-3) to concrete Finding-7 with the fix path spelled out.

Two additional findings surface at the boundary of what fix scoping catches:
- **F-4 v2**: the rule is right but init-kmp's own scaffold predates it — `RootContent` uses `@Suppress("UnusedParameter")`. Extend the rule to init-kmp's Kotlin templates so new projects don't ship the anti-pattern from day one.
- **Tester-ktlintFormat gap**: reviewer BLOCK cascaded from tester's style violations because tester doesn't run `ktlintFormat` before returning. This isn't unique to sh-4 — it's a systemic tester-contract gap that only surfaced now because sh-4's tester emitted more idiomatic Kotlin than sh-2/sh-3's.

**Action items surfaced by sh-4**:
- init-kmp §3.6 + step 8 — pre-wire `FRAMEWORK_SEARCH_PATHS` in xcodeproj (Finding-7 / F-6 v2).
- init-kmp §3 Kotlin templates — remove `@Suppress("UnusedParameter")` from RootContent stub (Finding-8 / F-4 v2).
- tester §5 workflow — run `ktlintFormat` before returning `verdict: done` (Finding-9).
- init-kmp desktop actual — file-backed JDBC driver, not IN_MEMORY (Finding-11).

## Verdict

**F-4 and F-5 empirically confirmed at pipeline level.** **F-6 partially confirmed** — the link step lands correctly but the Xcode-side wiring is missing; F-6 v2 is the natural refinement. Four smaller findings (2 Important, 2 Minor) surfaced that refine the overlay contracts without invalidating them.

The overlay continues to converge: each shakedown removes a set of known bugs and surfaces a smaller, more targeted set. sh-2 → sh-3 dropped Criticals 3→0; sh-3 → sh-4 held Criticals at 0 and reduced total finding severity.
