# zprof eval — session 445b990e…2189

_Tier 1 — deterministic. No LLM ran. See `zprof eval --deep` for panel judging._

## Session
- Session ID: `445b990e-2263-4383-8415-a8addba72189`
- Log path:   `/Users/alv/.claude/projects/-Volumes-mydata-z0mi-harness/445b990e-2263-4383-8415-a8addba72189.jsonl`
- Span:       25h05m (2026-07-17T14:11:56Z → 2026-07-18T15:16:59Z)
- Main model: `claude-opus-4-7`
- Main-loop tokens: in 2,735 / out 1,250,651 (cache read 337,484,000, create 10,121,464)
- Subagent tokens (output only): 106,078 across 9 dispatches

## Per-role scorecard

| Role | Model | N | Pass@1 | Median tok | ApT | Compliance | Notes |
|---|---|--:|--:|--:|--:|---|---|
| architect | (inherited) | 2 | 0.00 | 0 | 0.0 | clean |  |
| implementer | (inherited) | 3 | 1.00 | 106,078 | 0.9 | preamble×1 | avg conf 0.90 (1/3) |
| reviewer | (inherited) | 2 | 0.00 | 0 | 0.0 | clean |  |
| tester | (inherited) | 2 | 0.00 | 0 | 0.0 | clean |  |

## Contract violations

| # | Role | Kind | Detail |
|--:|---|---|---|
| 1 | implementer | `return-preamble` | first line was not `verdict:` — got: Build clean. Emit response. |
| 2 | architect | `dispatch-never-returned` | no task-notification recorded in this session |
| 3 | architect | `dispatch-never-returned` | no task-notification recorded in this session |
| 4 | implementer | `dispatch-never-returned` | no task-notification recorded in this session |
| 5 | tester | `dispatch-never-returned` | no task-notification recorded in this session |
| 6 | reviewer | `dispatch-never-returned` | no task-notification recorded in this session |
| 7 | implementer | `dispatch-never-returned` | no task-notification recorded in this session |
| 8 | tester | `dispatch-never-returned` | no task-notification recorded in this session |
| 9 | reviewer | `dispatch-never-returned` | no task-notification recorded in this session |

## What Tier 2 would add
- Panel-judge quality score (1-5) per dispatch across correctness / adherence / efficiency framings.
- Model-tier recommendation per role (advisory).
- Reasoning quality assessment on ADR / review artifacts.

Run `zprof eval --deep <sessionId>` to dispatch the `evaluator` subagent.
