# Review — target MoodJournalInterface — 2026-07-18

## Context
- Scope: target `MoodJournalInterface` (single-target audit against ADR-0002)
- Base ref: HEAD (no git in this workspace — filesystem snapshot review)
- Review type: arch + concurrency + build (per ADR-0002 gates)
- Time budget: quick
- Toolchain deltas from §2: none observable (no toolchain runs available — no `swift`, `swiftlint`, `swiftformat`, `xcodebuild` invocations performed; workspace is a bare source tree without a git repo). Recorded as tool-run gaps below.
- Ignored paths: none

## Summary
- Critical: 1
- Important: 2
- Minor: 2
- Style: 0
- Static analysis: swiftlint NOT RUN (tool gate skipped — no toolchain), swiftformat NOT RUN, xcodebuild build NOT RUN
- Tests: `xcodebuild test` NOT RUN
- **Verdict: BLOCK**

## Critical

### [C-1] Strict-concurrency is not actually enabled `=complete` on `MoodJournalInterface`
- File: `Packages/Feature/MoodJournal/Package.swift:14`
- Dimension: build
- Why it matters: ADR-0002 §1 requires the manifest to apply `.enableExperimentalFeature("StrictConcurrency=complete")` (or an equivalent that sets the checking level to `complete`) on **both** targets, and §6 restates `-strict-concurrency=complete` as a compliance requirement. The current manifest passes `.enableExperimentalFeature("StrictConcurrency")` with no value. In Swift 5.9's SwiftPM, the experimental-feature flag does not accept a level via the `Name=Value` form the way the frontend `-strict-concurrency=<minimal|targeted|complete>` flag does, so this incantation either silently no-ops or resolves to a level below `complete` depending on the compiler build. Result: the safety net that the Interface's `Sendable` guarantees are meant to be checked under is not actually engaged, and any future `Impl` target added to the same manifest inherits the same gap. This is the exact class of "you thought the checker was on, it wasn't" trap that strict-concurrency is supposed to prevent. Under Swift 5.9 the reliable form is `.unsafeFlags(["-strict-concurrency=complete"])`; under 5.10 it is `.enableUpcomingFeature("StrictConcurrency")`. ADR-0002 §1 called this out explicitly ("or equivalent `.swiftSettings` for Swift 5.9") — the current line satisfies neither.
- Proposed fix:
  ```diff
  --- a/Packages/Feature/MoodJournal/Package.swift
  +++ b/Packages/Feature/MoodJournal/Package.swift
  @@
           .target(
               name: "MoodJournalInterface",
               swiftSettings: [
  -                .enableExperimentalFeature("StrictConcurrency"),
  +                .unsafeFlags(["-strict-concurrency=complete"]),
               ]
           ),
           .testTarget(
               name: "MoodJournalInterfaceTests",
               dependencies: ["MoodJournalInterface"],
               swiftSettings: [
  -                .enableExperimentalFeature("StrictConcurrency"),
  +                .unsafeFlags(["-strict-concurrency=complete"]),
               ]
           ),
  ```

## Important

### [I-1] `StreakCalculator` protocol is placed in Interface but ADR-0002 §1 assigns it to `MoodJournalImpl/Domain/`
- File: `Packages/Feature/MoodJournal/Sources/MoodJournalInterface/StreakCalculator.swift:1`
- Dimension: arch
- Why it matters: ADR-0002 §1 enumerates the Interface surface exhaustively — value types (`MoodScore`, `MoodEntry`, `DateKey`), protocols (`MoodEntryRepository`, `NudgeScheduling`, `Clock`), route (`MoodJournalRoute`), errors (`MoodEntryRepositoryError`). `StreakCalculator` is explicitly listed in the `MoodJournalImpl/Domain/` bullet ("pure functions/value types") as concrete Impl code, not as an Interface protocol. Introducing a public `protocol StreakCalculator` in the Interface expands the seam beyond what the ADR sanctioned: it now forces every future streak-computation change to bump the Interface's public API, and it invites test-only fakes to leak across the seam. If the team wants a protocol seam here, that is a superseding-ADR conversation, not a silent expansion.
- Proposed fix:
  ```diff
  --- a/Packages/Feature/MoodJournal/Sources/MoodJournalInterface/StreakCalculator.swift
  +++ /dev/null
  @@
  -import Foundation
  -
  -public protocol StreakCalculator: Sendable {
  -    func streak(endingOn day: DateKey, entries: [MoodEntry]) -> Streak
  -}
  ```
  Move the concrete calculator (and, if a protocol is genuinely needed, the protocol itself) to `Packages/Feature/MoodJournal/Sources/MoodJournalImpl/Domain/StreakCalculator.swift` when the Impl target is scaffolded. `Streak` (the value type) may also move to Impl unless the Interface's `MoodEntryRepository` API grows a `streak(...)` method that returns it (ADR §4 shows `await repository.streak(endingOn: yesterday)` — so if that method lands on the protocol, `Streak` stays in Interface and `StreakCalculator` still moves).

### [I-2] `Package.swift` declares only the Interface product; ADR-0002 §1 mandates two library products
- File: `Packages/Feature/MoodJournal/Package.swift:7`
- Dimension: build
- Why it matters: ADR-0002 §1 is unambiguous — "a *single* SPM package with a single `Package.swift`, two library products, two targets, one test target." The manifest here ships only `MoodJournalInterface`. If the intent is that Impl lands in a later PR (matches §Consequences → Estimated PRs), the reviewer flags this as a manifest that is not yet ADR-complete: any consumer resolving the package today cannot build against `MoodJournalImpl` because the product does not exist. This is Important rather than Critical because the Interface half is internally consistent and Impl arrival is expected — but the manifest must gain the second product before the feature is buildable end-to-end. Recording it here so the next Impl-scaffold PR doesn't forget the manifest edit.
- Proposed fix: add the second `.library` product and `.target` (and its test target) when `Sources/MoodJournalImpl/` is populated. Left un-diffed because it depends on Impl file layout, which is out of scope for this postfix review.

## Minor

### [M-1] `DateKey` accepts any `String` — no ISO `yyyy-MM-dd` validation at the boundary
- File: `Packages/Feature/MoodJournal/Sources/MoodJournalInterface/DateKey.swift:7`
- Dimension: null-safety
- Why it matters: `DateKey.init(rawValue:)` accepts any `String`, including `""`, `"not-a-date"`, or `"2026-13-40"`. ADR-0002 §1 calls the type "day-precision `Date`" and §2 says the value doubles as a SwiftData `@Attribute(.unique)` key derived from `Calendar.current` at write time. With no validation, a bug upstream that constructs a `DateKey(rawValue: "")` propagates unchallenged all the way to the unique index. A `throws` init that requires a `Calendar.Component`-driven parse (or a `static func fromDate(_:in:)` factory that is the only way to construct one) closes the gap. Rated Minor because the actual write path in Impl will always go through the `Clock`; still worth pinning down the invariant at the type boundary.

### [M-2] `Streak` static factory named `.none` shadows `Optional.none` at call sites
- File: `Packages/Feature/MoodJournal/Sources/MoodJournalInterface/Streak.swift:20`
- Dimension: style
- Why it matters: `Streak.none` will be ambiguous at any call site typed as `Streak?` (where `.none` also names the optional's `nil` case). A rename to `.empty` (or `.zero`) is cheap and removes the ambiguity permanently.

## Style
- 0 findings — no swiftlint/swiftformat run available in this workspace. Style bucket left empty rather than fabricated.

## Waivers
- None.

## Compliance evidence gathered

Verifications executed against ADR-0002:

1. **§1 Sub-decision A — Interface types present:**
   - `MoodScore` — present (`MoodScore.swift`, 5-case `enum` `awful/bad/ok/good/great`, `Int`-raw, `Sendable`, `Comparable`).
   - `MoodEntry` — present (`MoodEntry.swift`, `Sendable`/`Codable`/`Hashable`/`Identifiable`).
   - `DateKey` — present (`DateKey.swift`, `Sendable`/`Codable`/`Hashable`/`Comparable`). Note deviation from "day-precision `Date`" — see [M-1].
   - `MoodEntryRepository` — present (`MoodEntryRepository.swift`, `Sendable` protocol with `async throws` methods).
   - `NudgeScheduling` — present (`NudgeScheduling.swift`, `Sendable` protocol, `async` entry points).
   - `Clock` — present (`Clock.swift`, `Sendable` protocol).
   - `MoodJournalRoute` — present (`MoodJournalRoute.swift`, `Hashable`/`Codable`/`Sendable` enum with `.week` and `.dayDetail(DateKey)`).
   - `MoodEntryRepositoryError` — present (`MoodEntryRepositoryError.swift`, `Error`/`Equatable`/`Sendable` enum).
   - **Verdict: all eight ADR-mandated types are present.**

2. **§1 — Interface UI-toolkit-free / persistence-free / networking-free:**
   - Ran `grep -RnE '^import '` across `MoodJournalInterface/`. Every source file imports **only `Foundation`**. Zero occurrences of `SwiftUI`, `UIKit`, `Combine`, `SwiftData`, `CoreData`, `Charts`, `UserNotifications`, `URLSession`, or any DI container.
   - **Verdict: PASS.**

3. **§1 — Manifest strict-concurrency:**
   - Manifest applies `.enableExperimentalFeature("StrictConcurrency")` on the Interface target.
   - **Verdict: FAIL — see [C-1].**

Additional types present in Interface not enumerated by ADR §1:
- `MoodNote` + `MoodNoteError` — validated wrapper enforcing the ≤500-char rule from the ADR's product context. Sensible extension; noted, not flagged.
- `Streak` — value type, permissible because `MoodEntryRepository`/`Clock` semantics reference "streak" concepts (ADR §4 shows `await repository.streak(...)`).
- `StreakCalculator` — protocol, ADR-mandated to live in Impl/Domain. See [I-1].

## Tool-run gaps

The following gates from §5 of the reviewer contract were not executed because this workspace has no git repo and no toolchain access from the review shell:

- `git diff <base>..HEAD` — not runnable (no git; the environment reports "Is directory a git repo: No"). Reviewed against the ADR text rather than against a diff.
- `swiftlint --strict` — not runnable in this shell.
- `swiftformat . --lint` — not runnable in this shell.
- `xcodebuild ... clean build` — not runnable in this shell.
- `xcodebuild test` — not runnable in this shell.

None of these gaps affect the correctness of the ADR-compliance findings above. They do mean the verdict cannot claim "static analysis green"; the verdict rests on the ADR audit alone.

## Next
Reply with the finding numbers you want fixed. Examples:
- `C1, I1, I2` — specific items
- `all critical` — every `[C-*]`
- `all critical, all important` — bail on Minor/Style
- `skip all` — approve as-is (blocked if any Critical remains)
- `approve` — same as `skip all`
- `block` — reject the diff outright, no fixes applied
