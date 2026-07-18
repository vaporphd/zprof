# zprof eval — session 445b990e…2189

_Tier 1 — deterministic. No LLM ran. See `zprof eval --deep` for panel judging._

## Session
- Session ID: `445b990e-2263-4383-8415-a8addba72189`
- Log path:   `/Users/alv/.claude/projects/-Volumes-mydata-z0mi-harness/445b990e-2263-4383-8415-a8addba72189.jsonl`
- Span:       20h37m (2026-07-17T14:11:56Z → 2026-07-18T10:49:19Z)
- Main model: `claude-opus-4-7`
- Main-loop tokens: in 2,318 / out 1,081,624 (cache read 280,867,314, create 8,131,565)
- Subagent tokens (output only): 894,120 across 8 dispatches

## Per-role scorecard

| Role | Model | N | Pass@1 | Median tok | ApT | Compliance | Notes |
|---|---|--:|--:|--:|--:|---|---|
| architect | (inherited) | 2 | 1.00 | 104,112 | 1.0 | clean | avg conf 0.88 (2/2) |
| implementer | (inherited) | 2 | 0.50 | 114,800 | 0.5 | artifact-missing×1 preamble×2 | avg conf 0.90 (1/2) |
| reviewer | (inherited) | 2 | 0.50 | 135,541 | 0.4 | clean | avg conf 0.87 (2/2) |
| tester | (inherited) | 2 | 1.00 | 126,108 | 0.9 | artifact-missing×2 preamble×1 | avg conf 0.95 (2/2) |

## Contract violations

| # | Role | Kind | Detail |
|--:|---|---|---|
| 1 | implementer | `return-preamble` | first line was not `verdict:` — got: Test target builds clean under strict concurrency. Root workspace also green. |
| 2 | tester | `artifact-missing` | claimed artifact not found on disk: no-git (working dir has no git); test files under /Volumes/mydata/zprof-test-ios-3/Packages/FeatureMoodJournal/Tests/MoodJournalInterfaceTests/ — MoodScoreTests.swift, MoodEntryTests.swift, MoodEntryDraftTests.swift, MoodJournalRouteTests.swift, MoodJournalViewStateTests.swift, MoodJournalEventTests.swift, StreakNudgePolicyTests.swift; stub MoodJournalInterfaceTests.swift deleted |
| 3 | implementer | `artifact-missing` | claimed artifact not found on disk: no-git; module `Packages/FeatureMoodJournal/Sources/MoodJournalImpl/` (added `StreakCalculatorImpl.swift` + test skeleton, updated `Package.swift`) |
| 4 | implementer | `return-preamble` | first line was not `verdict:` — got: Build is green. Returning result. |
| 5 | tester | `artifact-missing` | claimed artifact not found on disk: (no git repo — files written) Packages/FeatureMoodJournal/Tests/MoodJournalImplTests/StreakCalculatorImplTests.swift, Packages/FeatureMoodJournal/Tests/MoodJournalImplTests/Fixtures/MoodEntryFixtures.swift, Packages/FeatureMoodJournal/Package.swift (test-target dep addition only) |
| 6 | tester | `return-preamble` | first line was not `verdict:` — got: All 11 new tests pass, plus the pre-existing skeleton test. |

## What Tier 2 would add
- Panel-judge quality score (1-5) per dispatch across correctness / adherence / efficiency framings.
- Model-tier recommendation per role (advisory).
- Reasoning quality assessment on ADR / review artifacts.

Run `zprof eval --deep <sessionId>` to dispatch the `evaluator` subagent.
