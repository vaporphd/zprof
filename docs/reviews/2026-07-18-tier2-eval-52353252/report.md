# Tier-2 Evaluator Report — session `52353252…3824`

_First real-world dispatch of the evaluator subagent. Panel is a single-model fallback (three internal framings) — see §Assumptions._

## 1. Summary

Session `52353252-b350-486d-9ad5-678f47053824` (2026-07-16 → 2026-07-17). Scoped this Tier-2 evaluation to the **iOS shakedown segment**: dispatches after **2026-07-17T00:00Z**, roles `architect | implementer | tester | reviewer`. Twenty-one dispatches matched. Four systems-rust round-1 attempts failed simultaneously (infra-shaped cluster failure, not quality); the reviewer role was re-run for systems-rust and completed. Net completed samples: architect 4, implementer 4, tester 4, reviewer 5.

Panel judging ran only on **reviewer** (5 completed ≥ threshold). The other three roles are marked `insufficient-data` per contract §0. Reviewer panel quality = **3.52 / 5.0** (borderline REVIEW band), pass@1 = 0.833. Every completed dispatch violated the `return_format`-first HARD RULE (preamble across 5/5). Artifacts on disk are otherwise strong. **Headline: reviewer KEEP@opus. Fix the preamble bleed in the shakedown prompt or in the general-purpose contract — that's a wrapper problem, not a model-tier problem.** The four roles ran on Opus during this shakedown.

## 2. Per-role scorecard

| Role | Model | N (comp / total) | Pass@1 | Median tok | Panel quality | Framings (C / A / E) | Divergence | Recommendation |
|---|---|---:|---:|---:|---:|---|---|---|
| architect | opus | 4 / 5 | 0.80 | 108,569 | — | — | — | insufficient-data |
| implementer | opus | 4 / 5 | 0.80 | 99,645 | — | — | — | insufficient-data |
| tester | opus | 4 / 5 | 0.80 | 104,498 | — | — | — | insufficient-data |
| reviewer | opus | 5 / 6 | 0.833 | 102,520 | 3.52 🟡 | 4.31 / 2.75 / 3.56 | flagged×5 (see §4) | **KEEP** |

Framings key: **C** correctness lens · **A** adherence (HARD RULES) lens · **E** efficiency lens. Weighted per rubric §2.

### 2.1 Reviewer per-dispatch panel scores (median-wins)

| # | Overlay | tuid | tok | tools | dur | Instr | Compl | ToolEff | Reas | Coher | **Weighted** |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 1 | ios-swift | `…6JeD6c5JCU1x82u` | 130,698 | 8 | 584s | 4 | 4 | 3 | 3 | 2 | **3.25** 🟡 |
| 2 | backend-python | `…b42yw77KmhkQ2j` | 96,936 | 3 | 205s | 4 | 4 | 3 | 4 | 3 | **3.70** 🟡 |
| 3 | frontend-web | `…egjyMAVvQwrmNyu` | 102,520 | 5 | 262s | 4 | 4 | 3 | 3 | 3 | **3.55** 🟡 |
| 4 | systems-cpp | `…4bTdYyMxHUjdoy` | 101,568 | 4 | 239s | 4 | 4 | 3 | 3 | 3 | **3.55** 🟡 |
| 5 | systems-rust (re-run) | `…kdXD8VBv6qa3n5` | 104,607 | 5 | 279s | 4 | 4 | 3 | 3 | 3 | **3.55** 🟡 |

## 3. Per-role recommendations

### architect — `insufficient-data`

Four completed dispatches (ios-swift, backend-python, frontend-web, systems-cpp) — below the §0 five-sample floor. Systems-rust round-1 failed (cluster failure with implementer/tester/reviewer at the same wall-clock — not a per-role signal). Do not recommend a tier change from four data points. Collect one more sample (rerun the systems-rust architect) and re-evaluate.

Rough narrative signal only, not scored: all four artifacts landed within the 400-620 line band, matched the Kotlin-peer depth, and included pinned versions + per-layer allow/deny tables. Preamble×4 (all completed had the same first-line-not-`verdict:` violation).

### implementer — `insufficient-data`

Four completed. Same story as architect. Artifacts 475-573 lines, all included the six-file endpoint slice and forbidden-imports table per prompt. Preamble×4.

### tester — `insufficient-data`

Four completed. Artifacts 430-448 lines, all included pinned test-framework versions and 25-27-item self-validation checklists. Preamble×4.

### reviewer — `KEEP @ opus`

| Field | Value |
|---|---|
| samples | 5 (of 6 dispatched; 1 failed at cluster) |
| pass@1 | 0.833 |
| ApT | 9.32 art / M tok (5 arts / 536,329 out tokens) |
| panel_quality | 3.52 (correctness 4.31 / adherence 2.75 / efficiency 3.56) |
| divergence_flags | 5 (see §4 — every dispatch triggered) |
| current model | opus |
| **recommendation** | **KEEP** |

Reasoning: pass@1 = 0.833 is below the 0.90 downgrade gate; panel_quality 3.52 is in the REVIEW band (3.0-3.9). No signal to upgrade either (both floors clear). Critically, the quality drag is a **schema-compliance failure** (every response starts with prose instead of `verdict:` — 5/5), which is a wrapper/prompt problem, not a model-capacity problem. Sonnet at ~20% of Opus output cost would very likely produce identical preamble behavior; the underlying artifact quality is high (avg 464 lines, all sections present per §Sub-decisions in the prompts). Fix the return-format enforcement first, then rerun; only after the preamble stops should a downgrade experiment be considered.

Suggested next probe (advisory, not to auto-apply):
- Update `profiles/base/agents/reviewer.md` (or the general-purpose wrapper) with an explicit "first character of your response MUST be `v` from `verdict:`" line and a return-schema example inline.
- Re-run the same 5 overlays after the fix. If pass@1 → 1.0 and panel_quality ≥ 4.0, then commission a Sonnet A/B on the next batch.

## 4. Divergence flags

Every reviewer dispatch triggered ≥1 divergence >1.0 across the three framings. Root cause is structural: Framing A (correctness) rewards the on-disk artifact, Framing B (adherence) penalises the preamble hard-rule violation, so **instruction (0.30 weight)** and **coherence (0.10 weight)** are guaranteed to split by 2-3 points on every sample. This is not judgment noise — it's the panel doing exactly what it should when a run is "great artifact, broken schema."

| # | Overlay | Dim | A | B | C | Span | Note |
|---|---|---|---:|---:|---:|---:|---|
| 1 | ios-swift | instruction | 5 | 2 | 3 | 3 | A: artifact meets spec (429 lines, 12 dims, mirrors Kotlin). B: preamble = HARD RULE §0 line 34 violated. |
| 1 | ios-swift | reasoning | 5 | 3 | 2 | 3 | A: peer-mirror is justified. C: 130k tok / 8 tools / 584s = most expensive of five for the same shape. |
| 1 | ios-swift | coherence | 3 | 1 | 3 | 2 | Schema-first violated per B; A/C treat as narrative-level fine. |
| 2 | backend-python | instruction | 5 | 2 | 4 | 3 | Same structural split; C rewards efficient run (96k tok, 3 tools, 205s). |
| 2 | backend-python | tool_efficiency | 3 | 3 | 5 | 2 | C-lens: most efficient dispatch; A/B don't reward efficiency. |
| 2 | backend-python | coherence | 3 | 1 | 3 | 2 | Same schema-first split. |
| 3 | frontend-web | instruction | 4 | 2 | 4 | 2 | 485 lines slightly over 400-500 target; still A=4. |
| 3 | frontend-web | reasoning | 5 | 3 | 3 | 2 | A rewards stack-gated Vue/React/Next split; adherence/efficiency lenses don't score it. |
| 3 | frontend-web | coherence | 3 | 1 | 3 | 2 | Same schema-first split. |
| 4 | systems-cpp | instruction | 5 | 2 | 4 | 3 | 14 C++-specific dimensions, ABI + sanitizers covered. |
| 4 | systems-cpp | reasoning | 5 | 3 | 3 | 2 | A rewards ABI/UB/sanitizer content; B/C flat. |
| 4 | systems-cpp | coherence | 3 | 1 | 3 | 2 | Same. |
| 5 | systems-rust (re-run) | instruction | 5 | 2 | 4 | 3 | 469 lines, 13 dims, extensive pinned versions incl. miri/semver-checks. |
| 5 | systems-rust (re-run) | reasoning | 5 | 3 | 3 | 2 | A: unsafe/ownership dimensions well-covered. |
| 5 | systems-rust (re-run) | coherence | 3 | 1 | 3 | 2 | Same. |

Synthesis: divergence is systematic, not noisy. When Alex fixes the preamble issue, Framing B's instruction score will jump 2 → 4-5, and divergence will collapse. Until then, medians correctly reflect the "good work, broken wrapper" reality.

## 5. Traps observed

- **Universal preamble violation.** All 21 target-role dispatches (17 completed + 4 failed) returned prose ("Rewrote …", "Wrote …", "File: …", "429 lines — …") instead of the `verdict:` schema. This matches the full-session Tier-1 finding of preamble×63 across 78 dispatches. It is the single largest quality drag in this session and is model-independent.
- **Cluster failure at systems-rust round-1.** Four dispatches (`toolu_01Ju9…`, `toolu_01F1P…`, `toolu_01CGU…`, `toolu_01E5G…`) all failed within a 2-minute window with `status: failed`. The reviewer was re-dispatched later and completed cleanly — same prompt shape, same overlay. This looks like a transient infra/timeout issue, not a per-role quality signal, but it does drop three of the four roles below the 5-sample panel-judging threshold. Per contract §6, re-runs (`toolu_019bKGeB…` reviewer re-run) count as **independent samples**, not deduplicated — the prompt was identical, so this counts as replay data, useful for a rough pass^2 read (2 attempts, 1 success on rust reviewer = 50% replay pass rate on the one role that got a retry).
- **First-of-series token inflation.** ios-swift reviewer used 130,698 tok / 8 tools / 584s vs the four-overlay median of 102k / 5 / 262s. Reading the prompt, ios-swift was the first overlay in the shakedown; the agent invested in peer-study of the Kotlin sibling. Subsequent overlays inherited the pattern and were cheaper. Not a bug — but a good argument for pinning peer-study to the first shakedown of a new-overlay series and skipping it thereafter.
- **Line-count overrun on architects.** systems-cpp architect at 617 lines vs the 400-500 target, frontend-web architect at 594 vs 400-500. The prompts themselves said "MATCH depth of backend-python (557 lines)", which overrode the target — this is a spec conflict authored into the prompt, not an agent failure. Worth flagging in the shakedown protocol.
- **Frontend-web tester used 33 tools** (dispatch tuid `…qndJxRufL5WM3Uf`) — 6× the median. Not a completed dispatch under scrutiny (tester role is `insufficient-data`), but it warrants a look before the next shakedown: too much iteration for a 447-line write.

## 6. Assumptions & caveats

- **Panel is single-model fallback, not 3× parallel Sonnet.** Contract §3 mandates three parallel Sonnet judges per dispatch. This dispatch environment does not allow nested subagents, so this evaluator ran three internal framings (Correctness / Adherence / Efficiency) sequentially and took per-dimension medians. This is a documented triangulation, but it is NOT true PoLL — it inherits any bias of the executor model. Callers should discount the panel_quality by roughly one confidence band until a real 3× panel run is possible. Verdicts (KEEP / insufficient-data) are conservative in that light.
- **ApT metric = artifacts_produced × 1e6 / total_output_tokens.** Reviewer = 5 arts / 536,329 tok ≈ 9.32 art/Mtok. Reported for magnitude; not comparable across roles when the "artifact" is a fundamentally different deliverable (an ADR ≠ a review report ≠ a test suite).
- **Expected ApT at Sonnet tier ≈ same tokens, ~5× cost efficiency** (Sonnet output ≈ $15/M vs Opus ≈ $75/M). If pass@1 held at 0.833 on Sonnet, cost-per-artifact would drop ~80%. But there's no evidence pass@1 holds — no Sonnet A/B was run for this role.
- **Failed dispatches excluded from panel scoring.** The four systems-rust round-1 failures had no artifact and no return payload; they count in the pass@1 denominator only.
- **Length normalization was NOT applied.** All five reviewer artifacts fell in a narrow 429-489 line band, well within one log unit. Length bias per §6 is negligible here; would matter more across roles.
- **Position bias irrelevant** — every dispatch scored independently, not pairwise. Recorded per §6 for future readers.
- **Tier-1 attribution is by description parsing.** Every dispatch used `subagent_type: general-purpose`; the Tier-1 CLI routed them into `architect | implementer | tester | reviewer` buckets by matching the description. This attribution is what "the four roles ran on Opus" means — the wrapper is general-purpose@opus with a role-shaped prompt, not four different first-class subagents. Model-tier recommendations here refer to that wrapper, not to sibling subagents that might share a role name.
- **`.zprof.yaml` was not modified**; no subject agent was re-dispatched; no `next:` payload for downstream action.

## 7. What Tier-3 (human) should decide

1. **Fix the return-format-first violation at the wrapper level.** Add a prefixed "MUST first character = `v`" line to the general-purpose agent's prompt, OR add a post-response strip-preamble hook, OR require every subagent's `return_format` example to appear inline in the frontmatter. This one fix would flip 63/78 dispatches from `preamble` to `clean` and probably move reviewer panel_quality from 3.52 to ~4.2.
2. **Rerun the four failed systems-rust dispatches** to lift architect / implementer / tester to the 5-sample floor. Same prompts, same models; if they pass, we can commission a real recommendation for those three roles.
3. **Commission a real 3× Sonnet PoLL** — the current single-model panel is a documented compromise, and the reviewer verdict deserves triangulation before any downgrade experiment.
4. **A/B reviewer on Sonnet** — but only AFTER the preamble fix. Otherwise Sonnet will look worse for a reason unrelated to model capacity.
5. **Amend the shakedown protocol to allow line-count overrides only when the target and the "match-depth-of-peer" constraint conflict explicitly.** Systems-cpp architect (617 vs 400-500) and frontend-web architect (594 vs 400-500) are honest responses to conflicting instructions; the fix belongs in the prompt author, not the agent.

---

_Report author: `evaluator` (see `profiles/base/agents/evaluator.md`). Tier-1 compass: `docs/reviews/2026-07-17-ios-shakedown-eval/summary.html`. This report is advisory — no config was mutated by producing it._
