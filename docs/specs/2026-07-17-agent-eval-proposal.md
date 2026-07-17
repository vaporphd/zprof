# Agent eval — design proposal

**Author:** Claude (with Alex)
**Date:** 2026-07-17
**Status:** proposal, decisions locked

## Goal

Post-task evaluation that reads a Claude Code session JSONL, produces per-agent quality + cost scores, and suggests model-tier changes. Advisory only.

## Decisions (Alex locked in this session)

- **Advisory only** — flag suggestions in the eval report; never auto-edit `.zprof.yaml`.
- **Judge panel** — 3 × Sonnet judges with three rubric framings (PoLL-style, per Verga 2404.18796).
- **Trigger** — manual `zprof eval [<sessionId>]` only. No auto-run on session end. No hooks.
- **Enforcement** — report-only. No blocking. Doctor stays separate.

## Two-tier architecture

```
     ┌──────────────────────────────────────────────────────────────┐
     │  Tier 1: `zprof eval` CLI  (Go, deterministic, zero LLM cost)│
     │  parses ~/.claude/projects/<slug>/<sessionId>.jsonl          │
     │  → .zprof/eval/<sessionId>/trace.jsonl + summary.md          │
     └───────────────────────────┬──────────────────────────────────┘
                                 │
                                 ▼  (on demand: `zprof eval --deep`)
     ┌──────────────────────────────────────────────────────────────┐
     │  Tier 2: `evaluator` subagent   (dispatched by main loop)    │
     │  reads trace.jsonl + agent contracts + panel-judges          │
     │  → docs/reviews/eval-<sessionId>.md                          │
     └──────────────────────────────────────────────────────────────┘
```

## Tier 1 — deterministic parse (no LLM)

Read the JSONL, walk the message tree, output a flat list of dispatch records.

**Per-dispatch schema:**
```
{
  dispatchId, agentName, subagentType, model, promptDescription,
  parentSessionId, timestamp, durationMs,
  usage: { tokensIn, tokensOut, cacheRead, cacheCreate },
  toolUses,
  returned: { verdict, next, artifact, oneLine, confidence?, notes? },
  contractCompliance: {
    artifactExists: bool,   // did the promised file actually get written?
    hasPreamble:    bool,   // did the return begin with narrative before the schema?
    nextIsReachable: bool   // does `next:` name a known role?
  }
}
```

**Deterministic scores (no LLM):**
- **ApT** = successful_dispatches × 1e5 / total_output_tokens per role — per OckBench (2511.05722).
- **Pass@1 rate** per role — verdict ∈ {done, approve, approve-with-fixes} counts as success.
- **Contract-compliance rate** per role — % of dispatches with `artifactExists=true` AND `hasPreamble=false`.
- **Median cost** per dispatch, per role.

**Output:** `.zprof/eval/<sessionId>/summary.md` — a small table Alex can eyeball. This is the fast pass; catches "reviewer lied about writing a report" (H0-class regressions) with zero token spend.

**Extracting from JSONL:**
- Main-loop `assistant.message.usage` — tokens per turn.
- `Agent` tool_use content — subagent_type + description + prompt.
- Task-notification `user` messages contain `<subagent_tokens>N</subagent_tokens>...<result>verdict: ...</result>` — regex-parseable.
- Ordering: dispatches attributed by walking parent→child uuid links.

## Tier 2 — LLM-judge panel (on demand)

Runs only when Alex invokes `zprof eval --deep`. Reads Tier 1's trace + the actual files the subagent produced (or attempted to produce) + the subagent's contract .md.

**Rubric** — 5 dimensions, borrowed from NeoLabHQ `context-engineering-kit/skills/agent-evaluation` with our adjustments:

| Dimension | Weight | What it measures |
|---|---|---|
| Instruction Following | 0.30 | Did the agent follow the ADR/task/contract? Critical: did it violate any HARD RULE? |
| Output Completeness | 0.25 | Did it produce every named artifact / type / section? Critical if ADR-listed types were dropped (C2 finding). |
| Tool Efficiency | 0.20 | Redundant reads, tool churn, wasteful re-searches. |
| Reasoning Quality | 0.15 | For architect: alternatives shown per §0? For reviewer: are findings grounded in the diff? |
| Response Coherence | 0.10 | Return-schema hygiene (H1). |

Scale 1-5 per dimension, weighted sum → 1.0-5.0. Threshold: ≥4.0 = keep tier; 3.0–3.9 = review; <3.0 = flag for upgrade.

**Panel** — three Sonnet judges, three framings:
1. **Correctness lens** — "compare output to the artifact spec; count deviations."
2. **Adherence lens** — "reread the agent's contract; find every clause violation."
3. **Efficiency lens** — "was the token spend proportional to the deliverable?"

Median score wins per dimension. Divergence > 1.0 on any dimension flagged for human review.

**CoT before score** — every judge must justify before scoring (research shows +15-25% reliability per NeoLabHQ, matching Zheng 2306.05685).

**Skip Opus as judge:**
- Self-preference bias (Panickssery 2404.13076) — Opus over-scores Opus outputs.
- Cost: 3 × Sonnet ≈ 1 × Opus but with independence + panel error correction.
- Exception: for architect (Opus tier) reviews, a fourth Haiku judge cross-checks the Sonnet panel to widen family diversity within Anthropic-only options.

## Model-tier recommendation logic

Aggregated across ≥ 5 dispatches per role.

```
IF pass@1_rate >= 0.90 AND panel_quality >= 4.0
   AND (tier_below.expected_ApT / current_ApT) >= 1.30
  → suggest: DOWNGRADE
ELIF pass@1_rate < 0.60 OR panel_quality < 3.0
   AND role has Opus-typical features (long-context reasoning, cross-doc synthesis)
  → suggest: UPGRADE
ELSE
  → suggest: KEEP
```

Report format (per role):
```
architect:
  model: opus           samples: 8
  pass@1: 0.88          ApT: 62
  panel_quality: 4.2    (correctness 4.5 / adherence 4.0 / efficiency 4.0)
  recommendation: KEEP  (edge case: pass@1 just below 0.90 threshold)
```

## Trap avoidance

- **Verbosity bias** — normalize judge scores against output length before comparing across models (Chatbot Arena style-control regression).
- **Position bias** — for pairwise comparisons, always swap orderings and average.
- **Small samples** — require ≥5 dispatches per role before emitting a recommendation. Below that: "insufficient data".
- **Self-preference** — never let Opus judge Opus dispatches.
- **pass@1 vs pass^k** — a role that succeeds one-shot at Sonnet may still fail replays. `zprof eval --replay=3` optional flag re-dispatches N times with same prompt (Alex opt-in — real cost) to measure reliability before downgrading.

## Instrumentation Alex should add now (small change)

Extend every role's `return_format` with three optional fields (no schema break):
- `confidence: 0.0-1.0` — self-reported confidence.
- `self_check: [<✓ items>]` — machine-readable checklist snapshot.
- `notes:` — free-form (implementer already uses this).

Rationale: Tier 2's judges get grounded input beyond the artifact alone. Also enables cascade heuristics ("escalate if confidence < 0.6") in a future run.

## What ships

Assuming Alex signs off, the implementation phases:

1. **`cli/internal/eval/`** — parser + Tier 1 scoring. `cmd/eval.go` invokes.
2. **`profiles/base/agents/evaluator.md`** — subagent contract (Tier 2 orchestrator).
3. **`profiles/base/manifest.yaml`** — add `evaluator` to `tool_agents:`.
4. **Return-format extension** — bulk edit across the 6 role contracts (architect, implementer, tester, reviewer, refactor-agent, bug-hunter) in ios-swift + kotlin-android + backend-python + frontend-web overlays. Optional fields so backward-compat.
5. **`docs/specs/2026-07-17-agent-eval-proposal.md`** — this doc.
6. **Doctor hook** — add a check that flags contract-violation patterns (reviewer artifact-missing, agent-return-preamble) after N dispatches — report-only per Alex decision.

## References

**Papers**
- MT-Bench / LLM-as-Judge — Zheng 2306.05685
- G-Eval — Liu 2303.16634
- Panel of LLM juries (PoLL) — Verga 2404.18796
- Self-preference bias — Panickssery 2404.13076
- τ-bench / pass^k — Yao 2406.12045
- Shapley credit assignment for multi-agent — Wang 2511.10687
- RouteLLM (cost-quality routing) — Ong 2406.18665
- FrugalGPT (cascade) — Chen 2305.05176
- ApT / OckBench — 2511.05722
- s1 budget forcing — 2501.19393
- Position bias — Wang 2305.17926

**Skills / frameworks**
- NeoLabHQ context-engineering-kit `agent-evaluation` — the 5-dim rubric baseline. github.com/NeoLabHQ/context-engineering-kit
- DeepEval trajectory taxonomy — deepeval.com/guides/guides-ai-agent-evaluation-metrics
- LangSmith `TRAJECTORY_ACCURACY_PROMPT` — docs.langchain.com/langsmith/trajectory-evals
- Arize Phoenix `openinference.span.kind=AGENT` — arize.com/docs/phoenix/tracing
