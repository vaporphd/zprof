# zprof shakedown — ios-swift overlay end-to-end

**Date:** 2026-07-17
**Tester:** main-loop Claude (Opus 4.7)
**Scope:** installed base + `ios-swift` overlay into a fresh Swift package (`MoodJournal`) and ran the full dev-pipeline (architect → implementer → tester → reviewer) on two features via `general-purpose` subagents given each overlay agent's `.md` as its contract. Objective: prove the overlay's agent-loop produces coherent, contract-compliant work end-to-end and surface defects.

## TL;DR

Overlay is solid end-to-end. **Architect and implementer produced production-grade artifacts** (536-line ADR-0002, 8 pure-Swift files, `-strict-concurrency=complete` on every target, zero forbidden imports). **71 tests across two features all pass** under real `swift test`. **Reviewer surfaced real defects** the implementer missed (see §Findings-C). Two profile-level bugs found and fixed pre-test (xcodegen mandate wasn't enforced; layout mismatch in CLI error). One profile bug found post-test (reviewer silently omits its report file). Overall: ship-worthy with the fixes noted below.

## What was tested

- **Project scaffold:** `/Volumes/mydata/zprof-test-ios/` — SPM package, iOS 17 / macOS 14, single `AppCore` skeleton, no `.xcodeproj`.
- **Install:** `zprof apply ios-swift` — 17 agents, 4 managed CLAUDE.md blocks, workflows/dev-pipeline.md, docs/PROJECT_SPEC.md + docs/adr/ scaffold.
- **Doctor:** `zprof doctor` — clean.
- **Feature 1 (Sub-decision A of ADR-0002):** `MoodJournalInterface` package — 6 value types + 3 protocols + route. 59 XCTest cases across 7 suites.
- **Feature 2 (Sub-decision D of ADR-0002):** `StreakCalculatorImpl` in new `MoodJournalImpl` target. 12 XCTest cases. Total: **71 tests, all pass**.

## What went well

- **Architect ADR-0001 (Nygard bootstrap):** 4 alternatives (A/B/C/D), superseding chain, no placeholders. `§0 Alternatives-are-non-negotiable` rule was followed literally.
- **Architect ADR-0002 (mood-journal):** 536 lines, concrete package graph (Packages/CoreModel, CorePersistence, CoreNotifications, FeatureMoodJournal/{Interface,Impl}), sub-decisions A-D each with three alternatives. Compliance section maps to every Doctrine rule. **Xcodegen mandate propagated end-to-end** — the ADR calls App target "XcodeGen-driven, project.pbxproj gitignored", ADR-0001 Doctrine #10 pins the rule, PROJECT_SPEC.md Doctrine #10 restates it.
- **Implementer discipline:** MoodJournalInterface is 8 files, zero forbidden imports (no SwiftUI/UIKit/SwiftData/UserNotifications/Combine), pure value types, `Sendable` everywhere. Root `Package.swift` edit was truly one-line (per §0.2). Custom Codable decoders re-validate on decode — DayKey rejects Feb 30 on both `init(rawValue:)` and `init(from decoder:)`. Round-trip safety proven, not assumed.
- **Tester rigor:** No `Thread.sleep`, no `XCTAssertNil`-on-throws, one-behavior-per-test, UTC-pinned calendar fixtures, leap-year Feb 29 + cross-month + cross-year boundary tests. Concrete error-case matching via `XCTAssertThrowsError` + closure inspection.
- **Reviewer signal:** Found 1 real error + 3 warns + 3 info in an implementation that also compiled clean and had 59 passing tests. This is the highest-value agent in the pipeline — it catches ADR-drift the implementer missed (see F-C1 below).
- **Model tier distribution:** architect=opus, implementer=sonnet, tester=sonnet, reviewer=opus — matches spec §7.3. Confirmed by frontmatter of applied .md files.
- **State layer:** followup.md/lessons.md/todo.md templates present at root; base's Doctrine block in CLAUDE.md explains their purpose.
- **PROJECT_SPEC.md Decisions Log:** architect run #2 correctly appended the ADR-0002 bullet without rewriting anything else — bootstrap-vs-follow-up discipline held.

## Findings

### C — Critical (fix before ship)

**C1 — Reviewer reports artifact path it never writes.**
Reviewer's `return_format` requires `artifact: <absolute path to docs/reviews/YYYY-MM-DD-<slug>.md>`. In this run the reviewer returned `artifact: /Volumes/mydata/zprof-test-ios/docs/reviews/2026-07-17-mood-journal-interface.md` but never actually created the file (verified: `docs/reviews/` doesn't exist). This is a *silent* failure — orchestrator trusts the schema, thinks the report is on disk, moves on. Same failure mode as H0-class YAML: the tool reports success, downstream loses evidence. Two fixes:
- Reviewer contract §Output-format must explicitly say "write the report to disk before returning; the return value's `artifact:` must be a path you actually created".
- Add a doctor check: for every `verdict: approve*|block` reviewer artifact referenced in git history / recent runs, confirm the path exists. (Cheap: grep or a dispatch-log manifest.)

**C2 — Implementer skipped Event/ViewState types listed in ADR-0002 §Sub-decision A.**
ADR §A listed `MoodJournalEvent`, `MoodJournalViewState`, `WeeklyMoodViewState` as Interface exports; implementer silently omitted them. Not implementer's fault alone — my task prompt didn't call them out. But implementer §0 says "read the ADR, then follow it" — the ADR *was* the source of truth and the implementer bypassed it. This is a real risk with autonomous runs: implementer treats the user prompt as authoritative and ADR as background. Fix candidates:
- Add to implementer contract §0.x: "if ADR §-under-implementation lists a type and the user prompt omits it, add the type and record 'added per ADR §X, not in prompt' in return_format's `notes:` field. Never silently drop."
- Reviewer already catches it. Loop just needs the reviewer to actually run every time.

### H — High

**H1 — Reviewer + implementer both violated "return only return_format" instruction.**
Both agents prefixed their return with narrative ("Based on my thorough review…", "Build succeeded. Reporting per contract."). Downstream isolation rule (base's AGENT_LOOP §Isolation) says main must not cite subagent output verbatim — that's easier if agents genuinely only return the schema. Instrument this: add a validator that rejects returns not starting with `verdict:` and forces a re-emit.

**H2 — `.gitignore` written by overlay lacks iOS-critical entries.**
Installed `.gitignore` covers `thoughts/` + `*.zprof.bak-*`. For an iOS project it also needs at minimum: `.build/`, `.swiftpm/`, `*.xcodeproj/`, `DerivedData/`, `xcuserdata/`, `*.xcuserstate`. Xcodegen mandate makes `.xcodeproj/` gitignore critical (source of truth is project.yml). Overlay should append these under a managed block.

**H3 — apply CLI error message misleads when user passes `base` as arg.**
Ran `zprof apply base ios-swift` → error `load overlay base: read .../overlays/base/manifest.yaml: no such file or directory`. Cause: base is auto-applied; args are overlays only. Fix in `cli/internal/cmd/apply.go` — either:
- Recognize `base` as a no-op arg with a warning, OR
- Emit a clear error: `"base" is applied implicitly; pass only overlay names (see zprof list --available)`.

### M — Medium

**M1 — Architect §15 bootstrap-first-then-defer forces a two-turn UX.**
Architect on a fresh repo produces PROJECT_SPEC + ADR-0001 and returns `next: null`. User must re-invoke to get the feature ADR. This is contract-compliant (see §15 last paragraph) and defensible ("caller must confirm PROJECT_SPEC first"), but the friction cost is real — in autonomous loops, `next: null` reads as "workflow done", not "call me back". Consider `next: architect` + `blocker: PROJECT_SPEC.md acceptance` so the loop routes back automatically.

**M2 — Consilium block in CLAUDE.md omits tool-agents.**
Consilium table lists 11 role-agents (dev-orchestrator, planner, architect, implementer, tester, reviewer, refactor-agent, bug-hunter, explorer, docs-writer, exploratory-orchestrator). Missing: xcodegen-driver, xcode-runner, spm-manager, swiftlint-checker, simulator-driver, testflight-shipper. Intentional (Consilium = decision-making roles) but the omission is undocumented — a user reading CLAUDE.md wouldn't know the tool-agents exist. Options:
- Add a "Tool agents" companion table under Consilium.
- Or add a comment above Consilium: "Tool agents (xcodegen-driver, xcode-runner, spm-manager, ...) are in `.claude/agents/` but not decision-making — dispatch on demand."

**M3 — Executing block is skeletal.**
`| implementer | *.xcodeproj, *.xcworkspace, Package.swift, project.yml, Podfile |` — that's the entire Executing table. It undersells the model: implementer touches .swift; xcodegen-driver owns project.yml + regen; spm-manager owns Package.swift. Current entry is misleading (implementer does NOT own project.yml — that's an xcodegen-mandate violation). Fix:
```
| Agent            | Scope                                                    |
|------------------|----------------------------------------------------------|
| implementer      | .swift sources under Sources/*, Tests/*                  |
| tester           | XCTest / swift-testing files under Tests/*               |
| xcodegen-driver  | project.yml + xcodegen generate; owns .xcodeproj shape   |
| spm-manager      | Package.swift, Package.resolved                          |
| xcode-runner     | xcodebuild build/test invocations                        |
| swiftlint-checker| .swiftlint.yml + lint runs                               |
```

### L — Low / observations

**L1 — DayKey's Date-based init doesn't share validation path.** Reviewer flagged. Real bug potential if `dateComponents(...)` ever returns nil year/month/day (defensive `?? 0` produces "0000-00-00" which would fail decode). Cosmetic under normal use, but the fix is trivial.

**L2 — MoodNote measures length after trim.** Reviewer flagged. Semantic ambiguity vs PROJECT_SPEC "≤500 chars"; documented in the reviewer's report. Not a bug per se — a decision that needs writing down.

**L3 — Streak lacks Equatable/Hashable.** Reviewer flagged. Blocks future MoodJournalViewState synthesis. Fix is one-line.

**L4 — `verdict` verb inconsistency across roles.** architect/implementer/tester use `done|blocked|failed`. reviewer uses `block|approve-with-fixes|approve|awaiting-approval`. Legit domain difference (review is a gate, not an action) but the split is undocumented — orchestrator has to know which agent uses which vocabulary.

**L5 — `docs/reviews/` isn't created by apply.** Reviewer contract writes there but the dir doesn't exist until the reviewer creates it. If the reviewer never runs successfully (see C1), the dir stays missing forever. Add to state-templates so it exists at install.

**L6 — SourceKit LSP noise dominates the diagnostics stream.** Every `swift build`/`swift test` invocation surfaces "No such module 'PackageDescription'/'XCTest'" from LSP. Real toolchain builds fine. Not a zprof bug — LSP indexing issue with SPM packages under `Packages/*`. Worth noting in overlay docs so users don't chase phantom errors.

## Profile-level fixes applied during this run

Before running the pipeline I made three edits to enforce the xcodegen mandate (previously implicit, per Alex's standing rule saved as memory `ios-xcodegen-mandatory`):

- `profiles/overlays/ios-swift/agents/architect.md` — added HARD RULE in §0 mandating XcodeGen when `.xcodeproj` is present; updated Q7 default to name XcodeGen (not "Tuist/XcodeGen"); §2 BuildPlugins scoped to XcodeGen instead of "Tuist/XcodeGen".
- `profiles/overlays/ios-swift/loop.md` — added explicit "XcodeGen обязателен" rule at top of Изоляция.
- `profiles/overlays/ios-swift/claude-block.md` — added `project_manifest: "project.yml"` + `regen_cmd: "xcodegen generate"` under the stack block.

**Verification:** architect run #2 obeyed the mandate — ADR-0002 §"Package graph introduced" says "App/ (Xcode application target, XcodeGen-driven)"; PROJECT_SPEC.md Doctrine #10 restates the rule. Sub-decision D notes UN-delegate registration in App target routes through xcodegen-driver.

## Recommended follow-ups (priority order)

1. **C1** — reviewer must write the report file, not just claim it in the schema.
2. **C2** — implementer contract clause forcing type-list adherence to the referenced ADR.
3. **H2** — overlay's `.gitignore` block adds iOS entries + `*.xcodeproj/`.
4. **H1** — validate agent returns start with `verdict:`; strip preamble.
5. **H3** — friendlier `zprof apply base` error.
6. **M3** — flesh out the Executing block.
7. **M1, M2** — UX / discoverability polish.
8. **L1-L6** — pick up during normal maintenance.

## Artifacts

- Test project: `/Volumes/mydata/zprof-test-ios/`
- ADR-0001: `docs/adr/0001-record-architecture-decisions.md` (141 lines, Nygard bootstrap)
- ADR-0002: `docs/adr/0002-mood-journal-architecture.md` (536 lines, four sub-decisions)
- Feature 1: `Packages/FeatureMoodJournal/Sources/MoodJournalInterface/` (8 files) + 59 tests
- Feature 2: `Packages/FeatureMoodJournal/Sources/MoodJournalImpl/StreakCalculatorImpl.swift` + 12 tests
- Total: 71 XCTest, 0 failures.

## Fixes applied 2026-07-17 (same-day follow-up)

All C/H/M plus L5 fixed; verified against the same test project via re-apply.

**Contract .md edits** (`profiles/overlays/ios-swift/agents/`):
- `reviewer.md` §0 — new HARD RULE: report file must be written to disk before returning; `artifact: none` when nothing was written. §0 — return only `return_format` block. §12 — same clause reiterated.
- `implementer.md` §0.11 — never silently drop ADR-listed types; add a `notes:` line in `return_format` when adding on-ADR-not-in-prompt work. §0.12 — return only `return_format`.
- `architect.md` §0 — return only `return_format`. §15 — bootstrap run now returns `next: architect` + `blocker: ...` instead of `next: null`, so the loop routes back automatically. `return_format` extended with an optional `blocker:` field.

**Overlay-manifest schema** (`cli/internal/manifest/overlay.go`):
- Added `Gitignore []string` and `Executing map[string]string` fields to `OverlayManifest`. Optional; backward-compat.

**Apply engine** (`cli/internal/apply/`):
- `engine.go` — `ensureGitignore` now unions base entries + per-overlay `Gitignore:` lists, dedup-preserving.
- `tables.go` — `buildConsiliumTable` appends `### Tool Agents` companion section listing overlay `tool_agents:`. `buildExecutingTable` prefers overlay's `executing:` map; falls back to old detect-globs behavior for overlays that don't declare it.
- `state_files.go` — creates `docs/reviews/README.md` on install (fixes L5; new base template `state-templates/reviews-readme.md`).
- `cmd/apply.go` — recognizes literal `"base"` in overlay args, prints friendly note, ignores it. Empty args after strip → clear error.

**ios-swift overlay manifest** (`profiles/overlays/ios-swift/manifest.yaml`):
- Added `gitignore:` with `.build/`, `.swiftpm/`, `*.xcodeproj/`, `*.xcworkspace/`, `DerivedData/`, `xcuserdata/`, `*.xcuserstate`, `*.hmap`, `*.ipa`, `*.dSYM*`.
- Added `executing:` map with per-agent scopes matching the M3 finding — crucially, `xcodegen-driver` owns `project.yml`; `implementer` scope is `*.swift`; `spm-manager` owns `Package.swift`.

**Verification** (same test project, re-applied):
- `zprof apply base ios-swift` → prints "note: base is applied implicitly; ignoring" then applies cleanly.
- `.gitignore` gained 11 iOS entries.
- `docs/reviews/README.md` created.
- `CLAUDE.md` `## Consilium` has the new `### Tool Agents` sub-table listing the 6 tool agents.
- `CLAUDE.md` `## Executing` shows 6 rows with correct scope; implementer no longer claims `project.yml`.
- `zprof doctor` → clean.
- `cli` unit tests: 73 / 73 green.
- Test-project SPM build + all 71 XCTest cases: green.

**Not fixed in this pass** (out of scope for the shakedown):
- L1/L2/L3 are defects in the *test-project code*, not the profile; captured in the review findings — flagged for a future dev-pipeline run.
- L4 verdict-verb inconsistency: documented; considered legit (review is a gate, actions are actions).
- L6 SourceKit LSP noise: not a zprof bug.
- C1 doctor-side enforcement (grep-verify that reviewer artifacts exist) — contract-side fix is the primary lever; doctor check would be a belt-and-suspenders add if the contract rule doesn't stick.

## Not exercised (out of scope for this run)

- SwiftData Impl (would need iOS SDK linkage; SPM-only project can't build SwiftData targets).
- SwiftUI views (same).
- xcodegen-driver, xcode-runner, simulator-driver, testflight-shipper, spm-manager, swiftlint-checker, bug-hunter, refactor-agent, explorer, planner, dev-orchestrator, exploratory-orchestrator — none of these agents were dispatched. Their contracts read reasonable on inspection but were not empirically validated.
- Gates (`--with-gates`) — not applied. north-star-auditor / evidence-auditor / plan-reviewer untested.
- `zprof sync` — not exercised (no remote profile repo config in this test).
- Full pipeline via base's `dev-orchestrator` routing — I dispatched agents directly by name instead of routing through the orchestrator. Untested whether orchestrator makes correct routing choices given a raw feature request.
