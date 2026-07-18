# zprof eval — session 445b990e…2189

_Tier 1 — deterministic. No LLM ran. See `zprof eval --deep` for panel judging._

## Session
- Session ID: `445b990e-2263-4383-8415-a8addba72189`
- Log path:   `/Users/alv/.claude/projects/-Volumes-mydata-z0mi-harness/445b990e-2263-4383-8415-a8addba72189.jsonl`
- Span:       25h10m (2026-07-17T14:11:56Z → 2026-07-18T15:22:04Z)
- Main model: `claude-opus-4-7`
- Main-loop tokens: in 2,789 / out 1,275,194 (cache read 345,990,057, create 10,194,299)
- Subagent tokens (output only): 978,501 across 9 dispatches

## Per-role scorecard

| Role | Model | N | Pass@1 | Median tok | ApT | Compliance | Notes |
|---|---|--:|--:|--:|--:|---|---|
| architect | claude-opus-4-7[1m] | 2 | 1.00 | 97,517 | 1.1 | clean | avg conf 0.89 (2/2) |
| implementer | claude-opus-4-7[1m] | 3 | 1.00 | 118,021 | 0.9 | artifact-missing×2 preamble×1 | avg conf 0.90 (3/3) |
| reviewer | claude-opus-4-7[1m] | 2 | 1.00 | 131,100 | 0.8 | clean | avg conf 0.87 (2/2) |
| tester | claude-opus-4-7[1m] | 2 | 1.00 | 96,751 | 1.1 | artifact-missing×2 | avg conf 0.94 (2/2) |

## Contract violations

| # | Role | Kind | Detail |
|--:|---|---|---|
| 1 | implementer | `return-preamble` | first line was not `verdict:` — got: Build clean. Emit response. |
| 2 | implementer | `artifact-missing` | claimed artifact not found on disk: 80e7bb1 Packages/FeatureMoodJournal/Sources/MoodJournalInterface |
| 3 | tester | `artifact-missing` | claimed artifact not found on disk: 9e01f94 — Packages/FeatureMoodJournal/Package.swift; Packages/FeatureMoodJournal/Tests/MoodJournalInterfaceTests/{MoodEntryTests.swift, MoodJournalErrorTests.swift, MoodJournalRouteTests.swift} |
| 4 | implementer | `artifact-missing` | claimed artifact not found on disk: 101ecc7 Packages/FeatureMoodJournal/Sources/MoodJournalImpl/Domain/StreakCalculator.swift |
| 5 | tester | `artifact-missing` | claimed artifact not found on disk: fbca9c5 — Packages/FeatureMoodJournal/Tests/MoodJournalImplTests/StreakCalculatorTests.swift, Packages/FeatureMoodJournal/Package.swift |

## What Tier 2 would add
- Panel-judge quality score (1-5) per dispatch across correctness / adherence / efficiency framings.
- Model-tier recommendation per role (advisory).
- Reasoning quality assessment on ADR / review artifacts.

Run `zprof eval --deep <sessionId>` to dispatch the `evaluator` subagent.
