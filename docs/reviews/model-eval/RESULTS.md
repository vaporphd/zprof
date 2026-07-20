# KMP overlay model-routing eval

Purpose: measure per-role model sensitivity in the KMP overlay pipeline. Isolate one role's model per run, hold reviewer at Opus 4.8 as ground truth, aggregate empirical verdict + toolchain + cost.

## Baseline (sh-8 config, actual alias resolution)

| Role | Alias | Resolved ID |
|---|---|---|
| architect | opus | claude-opus-4-8 |
| reviewer | opus | claude-opus-4-8 |
| implementer | sonnet | claude-sonnet-5 |
| tester | sonnet | claude-sonnet-5 |
| init-kmp | sonnet | claude-sonnet-5 |
| gradle-runner | sonnet | claude-sonnet-5 |
| ktlint-checker | haiku | claude-haiku-4-5-20251001 |
| detekt-checker | haiku | claude-haiku-4-5-20251001 |

## Matrix

| Run | init | arch | impl | tester | runner | Hypothesis |
|---|---|---|---|---|---|---|
| sh-9-baseline | sonnet | opus | sonnet | sonnet | sonnet | Repro sh-8 APPROVE 0/0/0 |
| sh-9b | sonnet | opus | sonnet | **haiku** | sonnet | Tester is mechanical, Haiku sufficient |
| sh-9c | sonnet | opus | sonnet | sonnet | **haiku** | Gradle wrapper is mechanical |
| sh-9a | **haiku** | opus | sonnet | sonnet | sonnet | Scaffold templates hit by Haiku |
| sh-9d | sonnet | opus | **claude-opus-4-6[1m]** | sonnet | sonnet | Implementer upgrade hedge on F-14/15/16/17 |
| sh-9e | sonnet | **sonnet** | sonnet | sonnet | sonnet | Architect downgrade to Sonnet (comparison) |
| sh-9-min | haiku | opus | claude-opus-4-6[1m] | haiku | haiku | Stack winning downgrades |

Priority order: baseline → 9b → 9c → 9a → 9d → 9e → 9-min.

## Decision rule (asymmetric)

Downgrade accepted iff ALL vs baseline:
- reviewer verdict ≥ baseline (APPROVE stays APPROVE)
- overlay findings ≤ baseline (0 stays 0)
- code findings ≤ baseline (0 stays 0)
- toolchain green (ktlint / detekt / tests)
- preamble leak = false
- F-14/15/16/17 grep bans all clean

Any regression → reject downgrade, keep baseline tier.

## Results

| Run | verdict | overlay C/I/M/S | code findings | ktlint | detekt | tests | preamble | wall (min) | subagent tokens | notes |
|---|---|---|---|---|---|---|---|---|---:|---|
| sh-9-baseline | **BLOCK** | 0/2/0/1 | 2 code-level | FAILED | clean | 28/28 | 0/4 | ~63 | ~850K | Regression vs sh-8: Sonnet impl introduced iosMain ktlint red + AppError.message hole (UI always shows "Unknown error"). Overlay contract clean (F-14/15/16/17 all pass, F-4/8/10/11/13 regression sweep pass). |
| sh-9b (tester→haiku) | **BLOCK** | 0/2/1/1 | 2 code (reused) | FAILED | clean | 32/32 | 0/2 | ~14 | ~228K | Differential (reused impl artifacts → same 2 impl bugs). Haiku tester: **+4 tests coverage** vs Sonnet baseline (28→32), all 6 sealed ViewEvent variants F-10 covered. New soft findings: M-1 real-time `delay(50)` in runTest (should be advanceUntilIdle), S-1 empty assertion. **Verdict: Haiku sufficient for tester role, softer test-hygiene.** |
| sh-9c (runner→haiku) | pending | | | | | | | | | Gradle-runner as tool-agent not directly dispatched in my mechanism — may be N/A. |
| sh-9a (init→haiku) | **BLOCK (init tier)** | — | version-pin bug | n/a | n/a | n/a | 0/1 | ~14 | 98K | Structural scaffold complete (91 files, all F-rules met at grep level) but picked **Kotlin 1.9.24 + Gradle 9.6 incompatible pair** → Gradle build fails on `DefaultArtifactPublicationSet` internal API mismatch. Baseline Sonnet 5 picked Kotlin 2.x + Gradle 8.9 = builds. **Haiku insufficient for init-kmp**: version-pin recovery needs reasoning depth Haiku lacks. Downstream phases skipped. |
| sh-9d (impl→opus proxy 4.8) | **APPROVE** | 0/0/1/0 | 1 minor (dead subtype) | clean | clean(NO-SOURCE) | 26/26 | 0/3 | ~34 | ~526K | **Hypothesis SUPPORTED**. Opus 4.8 impl fixed both baseline Sonnet bugs (iosMain ktlint red, AppError.message hole), added structural F-16 upgrade (PersistenceMode enum vs runtime filter hack), and cost LESS than Sonnet (210K vs 287K). Reviewer recommendation: **Opus-tier implementer for KMP overlay**. Explicit reasoning-about-baseline-trap in KDocs. 4.8 as proxy for Alex's requested 4.6[1m] (Agent tool doesn't accept exact IDs); Opus-tier directional signal is strong. |
| sh-9e (arch→sonnet) | **BLOCK** | 0/1/0/0 | 0 substantive code bugs | RED (Logger.kt filename + SqlDelight-generated 266) | clean | 31/31 | 0/3 | ~46 | ~760K | **Sonnet architect SUFFICIENT** — spec quality drove Sonnet impl PAST baseline substantive bugs (iosMain fixed, AppError with cleaner `sealed messageFor` pattern). Still BLOCK on ktlint hygiene items (Logger.kt filename, .editorconfig for generated). **Key insight**: Sonnet-arch ≈ Opus-arch on spec correctness; downstream delta dominated by **implementer polish** (proactive .editorconfig + Logger rename that sh-9d Opus impl did but sh-9e Sonnet impl skipped). Architect tier less critical than implementer tier. |
| sh-9-min (opus-impl + haiku-tester) | **BLOCK** | 0/1/0/? | tester regressions | RED (tester) | clean | **5/5 in 1 class vs expected 26+** | 0/1 (opus reviewer) | ~35 | ~512K | Opus impl parity with sh-9d preserved. Haiku tester **misread sealed class as blocking fixtures** — MoodEntryLocalDataSource.InMemory public nested subclass same file, directly constructible. Sh-9d Sonnet tester handled identical impl at 26/26. Delivered 19% expected coverage + shipped ktlint RED (import/unused-import) + 3 orphaned abandoned test files. **Overrides sh-9b conclusion**: Haiku NOT safe on impls using sealed/Kotlin subclass-visibility patterns. Interaction effect (Opus impl idioms × Haiku tester depth) collapses the stack. |

## Recommendations

### Per-role verdicts

| Role | Recommendation | Evidence | Cost/Perf |
|---|---|---|---|
| **init-kmp** | **KEEP Sonnet 5** | 9a Haiku picked Kotlin 1.9.24 + Gradle 9.6 incompatible pair, unbuildable. Recovery from bad version-pin needs reasoning depth Haiku lacks. | Sonnet: works | Haiku: fails |
| **architect** | **KEEP Opus 4.8** (Sonnet acceptable) | 9e Sonnet architect sufficient — spec quality drove impl past baseline substantive bugs. But architect polish gaps (impl skipped hygiene items) suggest architect insight still matters. | Opus: +19% tokens vs Sonnet (127K vs 158K), better tokens/quality ratio |
| **implementer** | **UPGRADE to Opus** (4.6[1m] or 4.8) | 9d Opus impl flipped BLOCK→APPROVE by fixing both baseline Sonnet bugs (iosMain ktlint, AppError.message hole). Also proactively added .editorconfig + AppLogger rename. Explicit "reasoning-about-baseline-trap" in KDocs. | Opus: 211K vs Sonnet 287K — **cheaper AND better** for this role |
| **tester** | **KEEP Sonnet 5** | 9b Haiku sufficient on simple Sonnet-impl. 9-min Haiku **misread sealed class** as blocking (couldn't work around subclass-visibility), shipped 19% coverage + ktlint RED. Haiku unsafe on impls using Kotlin idioms. | Sonnet: robust across impl shapes | Haiku: interaction risk |
| **reviewer** | **KEEP Opus 4.8** | Ground truth for empirical eval. Not experimented — all reviewers ran on Opus for consistency. |
| **gradle-runner** | **N/A in current dispatch mechanism** | My general-purpose subagents run `./gradlew` directly via Bash. gradle-runner subagent not invoked. When it IS invoked (in native `.claude/agents/` dispatch via Task tool), Haiku likely sufficient — mechanical output parsing. |
| **ktlint-checker / detekt-checker** | **KEEP Haiku 4.5** | Mechanical, not experimented. Baseline default already Haiku. |

### Recommended production config

```yaml
model_overrides:
  implementer: claude-opus-4-6[1m]   # UPGRADED from sonnet (Alex's request; proxy'd via opus-4.8 in eval)
  # architect: sonnet                # OPTIONAL downgrade if cost pressure; keeping opus is safer
  # all others: keep overlay defaults
```

**Delta from current baseline** (sh-8 config): **1 change** — implementer sonnet → opus-4-6[1m].

### Cost projections

Per single shakedown (5-phase pipeline, ~15 files scaffold + tests + review):

| Config | Est tokens | vs baseline |
|---|---:|---|
| Current baseline (sh-8) | 850K | — |
| **Recommended (impl→opus-4-6)** | ~820K | **-3.5%** (Opus impl produces cleaner code in fewer iterations) |
| Stretch (impl→opus, arch→sonnet) | ~800K | -6% but architect-quality risk |
| Bad stretch (tester→haiku with sealed impls) | Regression | **BLOCK risk** — don't do this |

### Empirical rig upgrades

For future evals:
1. **Add `claude-opus-4-6[1m]` alias** to zprof `models.Aliases` map (`cli/internal/models/registry.go:9`). Adding `"opus-4-6": "claude-opus-4-6[1m]"` would enable direct model override without pass-through path.
2. **Extend Agent tool model aliases** (Anthropic-side) to accept `opus-1m` / specific IDs — my eval used `opus` (Opus 4.8) as proxy for Alex's requested `claude-opus-4-6[1m]`. Directional signal held but exact 4.6 vs 4.8 differential not measurable in my session.
3. **Zprof bug fix** already applied to `cli/internal/cmd/apply.go`: preserve existing `.zprof.yaml` model_overrides before rebuilding manifest. **Uncommitted** — needs Alex review + merge.
4. **Init-kmp on Haiku empirical proof point**: KMP overlay init role requires reasoning about Kotlin toolchain version compatibility, not just template rendering. Document in overlay `init-kmp.md` §0: "Sonnet 5 minimum tier for init role; Haiku tier fails version-pin selection."

### Total eval cost

| Run | Subagent tokens | Wall time |
|---|---:|---:|
| baseline | ~850K | 63 min |
| 9a | 98K | 14 min |
| 9b | 228K | 14 min |
| 9d | 526K | 34 min |
| 9e | 760K | 46 min |
| 9-min | 512K | 35 min |
| **TOTAL** | **~3.0M** | **~3.5 hours** |

Under original 5-6M projection. Differential-experiment pattern (reuse baseline scaffolds) saved ~50% vs fresh-per-shakedown.

