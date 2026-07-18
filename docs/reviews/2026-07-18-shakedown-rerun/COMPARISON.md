# iOS shakedown — before/after the return-format-first fix

## The claim we set out to verify

Yesterday's Tier-2 evaluator report said:

> "This one fix would flip 63/78 dispatches from `preamble` to `clean` and probably move reviewer panel_quality from 3.52 to ~4.2."

That is: adding a `# CRITICAL:` YAML comment inside every agent's `return_format` block would fix the schema-first violation that made 85% of yesterday's dispatches leak narrative prose before `verdict:`.

## Experiment

- Fresh test project `/Volumes/mydata/zprof-test-ios-3/` (same shape as yesterday's `/Volumes/mydata/zprof-test-ios-2/`).
- Same feature (MoodJournal), same ADR shape, same role sequence: architect × 2 → implementer + tester + reviewer on Interface → implementer + tester + reviewer on StreakCalculator Impl.
- The only variable that changed: agent contracts now carry the CRITICAL preamble inside their `return_format` block (commit `bbb447f`).

## Results

### Yesterday — full iOS shakedown segment (2026-07-17)

Source: `docs/reviews/2026-07-17-ios-shakedown-eval/summary.html` (Tier 1) + `docs/reviews/2026-07-18-tier2-eval-52353252/report.md` §2 (Tier 2).

| Role | Completed | Preamble | Preamble rate | Model |
|---|---:|---:|---:|---|
| architect | 4 | 4 | **100%** | opus |
| implementer | 4 | 4 | **100%** | opus |
| tester | 4 | 4 | **100%** | opus |
| reviewer | 5 | 5 | **100%** | opus |
| **Total** | **17** | **17** | **100%** | |

### Today — rerun after fix (2026-07-18)

Source: `docs/reviews/2026-07-18-shakedown-rerun/summary.md`.

| Role | Completed | Preamble | Preamble rate | Model |
|---|---:|---:|---:|---|
| architect | 2 | 0 | **0%** | opus |
| implementer | 2 | 2 | **100%** | claude-sonnet-5 |
| tester | 2 | 1 | **50%** | claude-sonnet-5 |
| reviewer | 2 | 0 | **0%** | opus |
| **Total** | **8** | **3** | **37.5%** | |

### The delta

- **Overall preamble rate: 100% → 37.5%** — 63-point drop.
- **Opus roles: 100% → 0%** — perfect compliance after the fix.
- **Sonnet-5 roles: 100% → 75%** — meaningful but incomplete recovery.

## Interpretation — the pattern the evaluator missed

The evaluator prescribed the fix but did NOT predict a model-tier split. What the rerun surfaced:

**All four Opus dispatches (2 architect + 2 reviewer) obeyed the CRITICAL rule perfectly.** First character was `v` from `verdict:`. Every schema field present. Populated `self_check`, `notes:` used as designed.

**All four Sonnet-5 dispatches (2 implementer + 2 tester) had a narrative-first default that partially resisted the rule.** Two of four leaked a full narrative summary, one leaked a two-line preamble ("Build is green. Returning result."), one obeyed cleanly.

Sample sizes:
- Opus: 4/4 clean = 100% compliance.
- Sonnet-5: 1/4 clean = 25% compliance.
- Sonnet-5 preamble specifics: all three preamble cases summarized the concrete artifact ("Build is green", "Test target builds clean under strict concurrency", "All 11 new tests pass") — the model's default "report what I did" pattern.

The single fix carried Opus from 0% → 100% clean but only halved Sonnet-5's rate.

## What this changes about the follow-up plan

The evaluator's original Tier-3 shortlist item #1 called the preamble a "wrapper problem, not a model-capacity problem". That was directionally right but incomplete: it's *both*:

- The wrapper enforcement (CRITICAL rule inside return_format) fixed the wrapper level entirely for stronger models.
- Weaker/newer models with a stronger narrate-what-you-did prior need a second layer of enforcement.

Possible next levers, in order of surgical minimalism:

1. **Prompt-level reinforcement at dispatch site.** I tested this on the second implementer dispatch — added "Your entire response begins with `verdict:`" to the prompt. It didn't help. The narrate-first prior is stronger than a single prompt line.
2. **Prepend a `verdict:` scaffold in the prompt.** Give the model the first line already written; ask it to complete. Untested; would need dispatch-site changes.
3. **Post-response strip-preamble hook.** Requires Claude Code hook support for subagent responses — I don't know of one.
4. **Downgrade Sonnet-5 to Sonnet 4.5 for implementer/tester** — untested hypothesis, could go either way.
5. **Route implementer/tester to Opus** — most conservative recovery, but ~5× the token cost.

## Numbers to feed Tier 2

- 8 dispatches, 894,120 subagent output tokens over ~40 minutes wall-clock.
- Pass@1 by role (Tier 1): architect 1.00 / implementer 0.50 / tester 1.00 / reviewer 0.50.
  - Implementer 0.50 is one artifact-missing (agent wrote "no-git; module …" as the artifact string instead of an absolute path — the module IS on disk, tooling mistook narrative for a path).
  - Reviewer 0.50 is one `awaiting-approval` verdict on the Interface review — not a fail semantically; the Tier-1 pass classifier treats it as neither pass nor block. Report caveat, not a real failure.
- Every architect + reviewer artifact exists on disk. Every test suite compiles and passes.

## Confidence

The 100%→0% collapse on Opus roles across 4 dispatches (4/4 unanimous) is a strong signal. The 100%→75% partial recovery on Sonnet-5 across 4 dispatches (3/4 leak) is a weaker signal — three data points is small — but the *pattern* (all three preamble cases begin with a summary of what was built) is consistent enough to hypothesize the model-tier split is real.

Sample size caveat: 8 dispatches is below the ≥5 samples per role floor Tier-2 recommendations require. This comparison is descriptive, not prescriptive. To decide anything about model tiers we still need a full re-shakedown across ≥5 samples per role with the current fix in place.
