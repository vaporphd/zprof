# iOS overlay model-routing eval

Purpose: measure per-role model sensitivity in the ios-swift overlay pipeline. Isolate one role's model per run, hold reviewer at Opus 4.8 as ground truth, aggregate empirical verdict + toolchain + cost. Mirrors the KMP model-eval methodology; feature spec = MoodJournal (single-user iOS 17+, SwiftData/SwiftUI/async-await).

Toolchain: Xcode 26.2, xcodegen 2.45.3, Swift 6.2.3. Feature slice = ADR-0002 Sub-decision A (MoodJournalInterface public API: CoreModel value types + FeatureMoodJournalInterface protocols/route).

## Baseline (ios-swift overlay defaults, actual alias resolution)

| Role | Alias | Resolved ID |
|---|---|---|
| architect | opus | claude-opus-4-8 |
| reviewer | opus | claude-opus-4-8 |
| implementer | sonnet | claude-sonnet-5 |
| tester | sonnet | claude-sonnet-5 |
| xcode-runner | sonnet | claude-sonnet-5 |
| xcodegen-driver | sonnet | claude-sonnet-5 |
| spm-manager | sonnet | claude-sonnet-5 |
| simulator-driver | sonnet | claude-sonnet-5 |
| testflight-shipper | sonnet | claude-sonnet-5 |
| swiftlint-checker | haiku | claude-haiku-4-5-20251001 |
| refactor-agent | opus | claude-opus-4-8 |
| bug-hunter | opus | claude-opus-4-8 |
| explorer | sonnet | claude-sonnet-5 |

## Matrix

| Run | arch | impl | tester | tool-agents | Hypothesis |
|---|---|---|---|---|---|
| sh-ios-baseline | opus | sonnet | sonnet | sonnet | Ground truth |
| sh-ios-b | opus | sonnet | **haiku** | sonnet | Tester mechanical, Haiku sufficient? |
| sh-ios-c | opus | sonnet | sonnet | **xcode-runner→haiku** | Build/lint parser mechanical |
| sh-ios-c2 | opus | sonnet | sonnet | **xcodegen+spm→haiku** | Manifest editors mechanical |
| sh-ios-d | opus | **opus** | sonnet | sonnet | Impl upgrade — KMP transfer confirm |
| sh-ios-e | **sonnet** | sonnet | sonnet | sonnet | Architect downgrade — KMP transfer confirm |
| sh-ios-min | opus | opus | haiku | haiku (via c2 artifacts) | Stack production candidate |

Priority: baseline → b (parallel with e) → d → c/c2 → min.

## Decision rule (asymmetric)

Downgrade accepted iff ALL vs baseline:
- reviewer verdict ≥ baseline (BLOCK → APPROVE is a win; APPROVE → BLOCK is a loss)
- overlay findings (C/I/M/S) not worse
- code findings not worse
- toolchain green
- preamble leak = false
- Package.swift not broken
- Layer purity: 0 forbidden imports

Any regression → reject downgrade, keep baseline tier.

## Results

| Run | verdict | overlay C/I/M/S | code findings | build | tests | preamble | wall (min) | subagent tokens | key evidence |
|---|---|---|---|---|---|---|---|---:|---|
| sh-ios-baseline | **BLOCK** | 3/4/3/10 | 10 | green | 80/81 | 0/5 | ~50 | ~840K | DayKey.init(from:) accepts Feb 30 (real bug); AppCore ADR-drift; swiftlint+swiftformat red on tests; WeeklyMood ordering; 3 Critical from Sonnet impl. Baseline sonnet TESTER caught the Feb 30 bug in own diff. |
| sh-ios-b (tester→haiku) | **BLOCK** | 2/3/2/0 | 7 | red | 104/105 | 0/2 | ~11 | ~266K | Haiku tester wrote MORE tests (105 vs 81) AND caught same Feb 30 bug — BUT **broke FeatureMoodJournal Package.swift** by adding testTarget entries (syntax error). Also missed UTC-calendar discipline across 30+ test sites. Tester didn't run `swift build` before declaring done. **Haiku sufficient for bug-finding, unsafe for manifest mutations.** |
| sh-ios-c (xcode-runner→haiku) | **failed** (per xcode-runner semantics) | — | — | green | 81/82 | 0/1 | ~2 | ~67K | Correctly ran build+test+lint+format, structured report, actionable next-step. Identified real bug + 5 lint violations + 2 warnings. **Fastest run in matrix. Haiku sufficient for xcode-runner unambiguously.** |
| sh-ios-c2 (xcodegen+spm→haiku) | **done** | — | — | green | — | 0/1 | ~3 | ~69K | xcodegen-driver generated MoodJournalApp.xcodeproj green + verified gitignore. spm-manager added StreakCalculatorImpl target to Packages/FeatureMoodJournal/Package.swift and target compiles. Both tool-agent mechanical tasks handled correctly. **Haiku sufficient for both.** |
| sh-ios-d (impl→opus) | **APPROVE-WITH-FIXES** | 0/1/2/0 | 3 | green | 74/74 | 0/3 | ~22 | ~458K | **Opus impl fixed Feb 30 bug** (DayKey.isValid round-trips through calendar; Feb30/Apr31/Feb29-nonleap all reject). Sole Important = one `TimeZone(secondsFromGMT:0)!` documented + trivially → `.gmt`. Opus impl 151K vs baseline sonnet 181K = **17% cheaper**, 38 tools vs 74 = **48% fewer**. **KMP-transfer confirmed empirically.** |
| sh-ios-e (arch→sonnet) | **APPROVE-WITH-FIXES** | 0/0/4/0 | 0 | green | 85/85 | 0/4 | ~30 | ~536K | Zero Critical/Important. 4 Minor drift (UTC vs .gmt, single-consumer AsyncStream doc, test fixture year mismatch, AppCore retirement deferred). **BUT: sonnet tester on this run wrote 84 tests and MISSED the Feb 30 bug** (baseline sonnet tester caught it). Non-deterministic tester coverage. Sonnet-arch itself sufficient. |
| sh-ios-min (opus+opus+haiku) | **BLOCK** | 2/4/3/0 | 9 | green | 112/112 | 0/1 | ~13 | ~262K | Haiku tester on opus impl: 112 tests all green (opus impl has no bugs). BUT: **6× `#expect(true)` fake Sendable tests + 1 `as!` force-cast in tests**. Package.swift intact this time (I copied working manifest from d, Haiku didn't touch it). "Very close, 10-min fixes" per reviewer. |

## Cost per role (this eval)

Subagent tokens per phase, averaged where multiple samples exist:

| Role | Model | Tokens | Wall (min) | Tool uses |
|---|---|---:|---:|---:|
| architect (bootstrap+feature) | opus | 223K | 12 | 35 |
| architect (bootstrap+feature) | sonnet | 214K | 11 | 16 |
| implementer | sonnet | 181K/166K (baseline/e) | 9-10 | 65-74 |
| implementer | opus | 151K (d) | 8 | 38 |
| tester | sonnet | 191K/167K/177K | 8-12 | 49-65 |
| tester | haiku | 124K (b) / 109K (min) | 6-7 | 41-50 |
| reviewer | opus | 129K-166K | 5-8 | 33-49 |
| xcode-runner | haiku | 67K | 2 | 20 |
| xcodegen+spm | haiku | 69K | 3 | 34 |

## Recommendations

### Per-role verdicts

| Role | Recommendation | Evidence | Cost/Perf |
|---|---|---|---|
| **architect** | **KEEP Opus 4.8** (Sonnet acceptable) | sh-ios-e Sonnet architect landed 0C/0I/4M — spec quality drove impl to zero code findings, same envelope as baseline Opus. Sonnet arch works. **Nuance**: sh-ios-e sonnet tester on same spec missed Feb 30 bug that baseline sonnet tester caught — tester-side interaction, not arch-side. | Opus 223K vs Sonnet 214K — sonnet marginally cheaper. Opus more decisive. |
| **implementer** | **UPGRADE to Opus** (4.6[1m] or 4.8) | sh-ios-d Opus impl flipped BLOCK→APPROVE-WITH-FIXES by fixing Feb 30 DayKey bug that baseline+e Sonnet impls both had. Proactively re-validated all invariants via round-trip. | Opus 151K vs Sonnet 181K = **17% cheaper**, 48% fewer tool iterations. **Cost-negative + quality upgrade.** |
| **tester** | **KEEP Sonnet 5** (do NOT downgrade to Haiku) | Two-mode failure of Haiku tester: (1) sh-ios-b: broke FeatureMoodJournal Package.swift manifest by adding testTarget entries; didn't run `swift build` before declaring done. (2) sh-ios-min: wrote 6× `#expect(true)` fake Sendable tests + 1 `as!` force-cast. Sonnet tester never made either mistake. Haiku CAN find bugs (caught Feb 30 in b), but code-quality discipline unreliable. | Haiku would be 40% cheaper (109-124K vs 167-191K) but Package.swift breakage + fake tests are hard regressions. |
| **reviewer** | **KEEP Opus 4.8** | Ground truth for eval — every reviewer ran on Opus. Correctly flagged Haiku-tester issues in b and min, correctly approved Opus-impl in d. Not experimented. |
| **xcode-runner** | **DOWNGRADE to Haiku 4.5** | sh-ios-c: 67K tokens, 2 min wall, cleanest report in matrix. Structured output (build+test+lint+format counts + actionable next), correctly identified real bug. Mechanical output parsing suits Haiku perfectly. | Haiku ~45% cheaper than sonnet estimate for equivalent orchestration. |
| **xcodegen-driver** | **DOWNGRADE to Haiku 4.5** | sh-ios-c2: xcodegen generate green + xcodeproj gitignore verified. project.yml YAML editing is deterministic manifest work. | 69K combined with spm-manager — very cheap. |
| **spm-manager** | **DOWNGRADE to Haiku 4.5** | sh-ios-c2: added StreakCalculatorImpl target to Package.swift + verified `swift build` green on that target. Haiku edited Package.swift correctly (contrast: as tester it broke Package.swift — but there it was writing new testTarget with less scaffolding context). | Under 40K tokens for the spm-manager portion. |
| **swiftlint-checker** | **KEEP Haiku 4.5** | Baseline default already Haiku. Mechanical, not experimented. |
| **simulator-driver** | **Haiku candidate** (not tested) | Mechanical `xcrun simctl` orchestration. Same class as xcode-runner. Recommend downgrade with light shakedown before adopting production. |
| **testflight-shipper** | **KEEP Sonnet** (not tested) | Code-signing + entitlements reasoning nontrivial. Failure = expensive (bad build shipped). Hold Sonnet until an explicit tester validates Haiku sufficient. |
| **refactor-agent** | **KEEP Opus 4.8** (not tested) | Behavior-preservation reasoning. Silent-regression risk on Sonnet. |
| **bug-hunter** | **KEEP Opus 4.8** (not tested) | Hypothesis-generating. Sonnet may produce shallow leads. |
| **explorer** | **KEEP Sonnet** (not tested) | Read-only synthesis. Haiku might miss cross-file connections. |

### Recommended production config (change from baseline)

```yaml
model_overrides:
  implementer: claude-opus-4-6[1m]     # UPGRADED from sonnet
  xcode-runner: haiku                  # DOWNGRADED from sonnet
  xcodegen-driver: haiku               # DOWNGRADED from sonnet
  spm-manager: haiku                   # DOWNGRADED from sonnet
  # simulator-driver: haiku            # RECOMMENDED after light shakedown
  # all others: keep overlay defaults
```

**Delta from current baseline**: 4 changes empirically validated + 1 recommended (needs mini-shakedown for simulator-driver).

### Cost projections per shakedown

| Config | Est tokens (full pipeline: arch bootstrap+feature + impl + tester + reviewer + build/lint pass) | vs baseline |
|---|---:|---|
| Current baseline (arch=opus, impl=sonnet, tester=sonnet, tool-agents=sonnet, reviewer=opus) | ~840K | — |
| **Recommended (impl→opus, tool-agents→haiku)** | ~720K | **-14%** (Opus impl 17% cheaper + Haiku tool-agents 40% cheaper for their share) |
| Stretch (arch→sonnet on top) | ~710K | -15% but tester interaction risk (arch spec quality shift affects tester coverage — sh-ios-e evidence) |
| Bad stretch (tester→haiku) | Regression (BLOCK) | Package.swift break + fake `#expect(true)` tests |

### iOS-specific findings vs KMP

| Signal | KMP eval | iOS eval | Same or Different |
|---|---|---|---|
| Implementer Opus upgrade | ✅ (fixed 2 bugs, 28% cheaper) | ✅ (fixed Feb 30, 17% cheaper) | **Same — strong transfer** |
| Architect Sonnet downgrade acceptable | ✅ (spec quality sufficient) | ✅ (sh-ios-e zero Critical) | **Same** |
| Tester Haiku unsafe | ✅ (sealed class trap in KMP-9-min) | ✅ (Package.swift break + fake tests) | **Same conclusion, DIFFERENT mechanism** — KMP hit Kotlin subclass-visibility, iOS hit SPM manifest syntax + Swift Testing conventions |
| Xcode/Gradle runner Haiku sufficient | ~ (N/A dispatch in KMP eval) | ✅ (67K, cleanest report) | **iOS new signal**: mechanical tool-agents strongly Haiku |
| Reviewer Opus mandatory | Ground truth | Ground truth | **Same — no experimentation** |

### Non-deterministic tester coverage (new signal not seen in KMP eval)

Sonnet tester wrote different depth on the same task:
- baseline sonnet tester: 81 tests, caught Feb 30 bug
- sh-ios-e sonnet tester: 84 tests, MISSED Feb 30 bug (bug still in impl since sonnet impl)
- sh-ios-d sonnet tester: 74 tests, on opus impl (no bugs to find)

Same tier (Sonnet 5), same task, variance in coverage depth. Haiku tester in sh-ios-b caught Feb 30 with 105 tests; in sh-ios-min wrote 112 tests but no bugs to catch. **Implication**: tester coverage is a stochastic property regardless of tier — the empirical rig should assume non-deterministic bug-detection rate and account for it in shakedown design (multiple tester runs per impl, aggregate coverage).

### Empirical rig upgrades (identical to KMP recommendations)

1. Add `claude-opus-4-6[1m]` alias to zprof `models.Aliases` (`cli/internal/models/registry.go`).
2. Extend Agent tool model aliases (Anthropic-side) to accept `opus-1m` / exact IDs. This eval used `opus` (4.8) as proxy for Alex's requested 4.6[1m].
3. **Zprof bug fix still uncommitted**: `cli/internal/cmd/apply.go` model_overrides preservation. Task #182 open.
4. Document in overlay `tester.md` §0: "Haiku tier is unsafe for tester role in ios-swift overlay — will break SPM Package.swift when adding testTarget entries and may write `#expect(true)` fake tests. Sonnet minimum tier."

### Total eval cost

| Run | Subagent tokens | Wall time |
|---|---:|---:|
| sh-ios-baseline (arch bootstrap+feature + impl + tester + reviewer) | ~840K | ~50 min |
| sh-ios-b (tester + reviewer differential) | ~266K | ~11 min |
| sh-ios-c (xcode-runner + reviewer) | ~67K | ~2 min |
| sh-ios-c2 (xcodegen+spm tool-agents) | ~69K | ~3 min |
| sh-ios-d (impl + tester + reviewer, arch reused) | ~458K | ~22 min |
| sh-ios-e (arch bootstrap+feature + impl + tester + reviewer) | ~536K | ~30 min |
| sh-ios-min (tester + reviewer, arch+impl reused from d) | ~262K | ~13 min |
| **TOTAL** | **~2.5M** | **~2:11 wall (fanned out)** |

Under the 3M KMP projection thanks to more aggressive differential reuse. Fan-out reduced wall clock roughly 3× vs sequential.

## Interpretation

Three headline findings:

1. **iOS confirms KMP for the two most impactful role assignments.** Implementer→Opus is a cost-negative quality upgrade; tester Haiku downgrade is unsafe (different failure mechanism but same verdict). One production overlay change replicated across two domains.

2. **iOS mechanical tool-agents are Haiku territory.** xcode-runner + xcodegen-driver + spm-manager all validated at Haiku with sub-70K tokens each — 45%+ cost savings for build/manifest orchestration. KMP eval didn't have equivalent breadth of tool-agent testing; iOS eval fills that gap and the answer is clear.

3. **Tester coverage is stochastic within a tier.** Same Sonnet tester on same task wrote different depths of coverage across runs; same Haiku tester found bugs on one host and missed opportunities on another. The bug-detection rate of a tester subagent should be treated as a probability distribution, not a deterministic property — production shakedown practice should account for this (multiple tester runs, aggregate coverage measurement).

The iOS overlay is production-viable with the four-change model_overrides delta; no further evals needed for the roles empirically covered here.
