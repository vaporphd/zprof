# zprof eval — session 445b990e…2189

_Tier 1 — deterministic. No LLM ran. See `zprof eval --deep` for panel judging._

## Session
- Session ID: `445b990e-2263-4383-8415-a8addba72189`
- Log path:   `/Users/alv/.claude/projects/-Volumes-mydata-z0mi-harness/445b990e-2263-4383-8415-a8addba72189.jsonl`
- Span:       28h35m (2026-07-17T14:11:56Z → 2026-07-18T18:47:00Z)
- Main model: `claude-opus-4-7`
- Main-loop tokens: in 3,443 / out 2,010,386 (cache read 552,046,351, create 11,064,000)
- Subagent tokens (output only): 904,861 across 7 dispatches

## Per-role scorecard

| Role | Model | N | Pass@1 | Median tok | ApT | Compliance | Notes |
|---|---|--:|--:|--:|--:|---|---|
| architect | claude-opus-4-7[1m] | 2 | 1.00 | 110,047 | 1.0 | clean | avg conf 0.88 (2/2) |
| implementer | claude-opus-4-7[1m] | 1 | 1.00 | 150,677 | 0.7 | clean | avg conf 0.85 (1/1) |
| other | claude-opus-4-7[1m] | 1 | 1.00 | 141,943 | 0.7 | clean | avg conf 0.85 (1/1) |
| reviewer | claude-opus-4-7[1m] | 1 | 1.00 | 144,899 | 0.7 | preamble×1 | avg conf 0.85 (1/1) |
| tester | claude-opus-4-7[1m] | 2 | 0.00 | 163,492 | 0.0 | artifact-missing×2 preamble×1 | avg conf 0.93 (2/2) |

## Contract violations

| # | Role | Kind | Detail |
|--:|---|---|---|
| 1 | tester | `artifact-missing` | claimed artifact not found on disk: no commit; no test files created |
| 2 | tester | `return-preamble` | first line was not `verdict:` — got: The mokkery plugin is commented out in `shared/build.gradle.kts` (line 14: `// a… |
| 3 | tester | `artifact-missing` | claimed artifact not found on disk: 7a8561f — shared/src/commonTest/kotlin/com/example/moodjournal/feature/moodjournal/domain/usecase/CurrentStreakUseCaseTest.kt (7 tests), shared/src/commonTest/kotlin/com/example/moodjournal/feature/moodjournal/data/mapper/MoodJournalMapperTest.kt (7 tests) |
| 4 | reviewer | `return-preamble` | first line was not `verdict:` — got: Report file written on disk. Returning the required verdict block. |

## What Tier 2 would add
- Panel-judge quality score (1-5) per dispatch across correctness / adherence / efficiency framings.
- Model-tier recommendation per role (advisory).
- Reasoning quality assessment on ADR / review artifacts.

Run `zprof eval --deep <sessionId>` to dispatch the `evaluator` subagent.
