# zprof eval — session 445b990e…2189

_Tier 1 — deterministic. No LLM ran. See `zprof eval --deep` for panel judging._

## Session
- Session ID: `445b990e-2263-4383-8415-a8addba72189`
- Log path:   `/Users/alv/.claude/projects/-Volumes-mydata-z0mi-harness/445b990e-2263-4383-8415-a8addba72189.jsonl`
- Span:       27h25m (2026-07-17T14:11:56Z → 2026-07-18T17:37:22Z)
- Main model: `claude-opus-4-7`
- Main-loop tokens: in 3,333 / out 1,914,081 (cache read 507,424,048, create 10,961,815)
- Subagent tokens (output only): 982,949 across 9 dispatches

## Per-role scorecard

| Role | Model | N | Pass@1 | Median tok | ApT | Compliance | Notes |
|---|---|--:|--:|--:|--:|---|---|
| architect | claude-opus-4-7[1m] | 2 | 1.00 | 114,705 | 0.9 | clean | avg conf 0.88 (2/2) |
| implementer | claude-opus-4-7[1m] | 2 | 1.00 | 141,684 | 0.7 | clean | avg conf 0.82 (2/2) |
| other | (inherited) | 1 | 0.00 | 0 | 0.0 | clean |  |
| reviewer | claude-opus-4-7[1m] | 2 | 1.00 | 158,876 | 0.7 | clean | avg conf 0.84 (2/2) |
| tester | claude-opus-4-7[1m] | 2 | 1.00 | 130,653 | 0.9 | clean | avg conf 0.82 (2/2) |

## Contract violations

| # | Role | Kind | Detail |
|--:|---|---|---|
| 1 | other | `dispatch-never-returned` | no task-notification recorded in this session |

## What Tier 2 would add
- Panel-judge quality score (1-5) per dispatch across correctness / adherence / efficiency framings.
- Model-tier recommendation per role (advisory).
- Reasoning quality assessment on ADR / review artifacts.

Run `zprof eval --deep <sessionId>` to dispatch the `evaluator` subagent.
