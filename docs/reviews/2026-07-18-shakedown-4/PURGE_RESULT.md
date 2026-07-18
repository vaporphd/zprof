# iOS shakedown #4 — post-purge Combine + VIPER verification

Empirically validates commit `e201121` (refactor(ios-swift): purge Combine and VIPER from the overlay) against a fresh MoodJournal shakedown.

## Method

- Fresh project `/Volumes/mydata/zprof-test-ios-4/` (same shape as `-3/`).
- `zprof apply ios-swift` after the purge commit.
- Full pipeline dispatched: architect × 2 (bootstrap + ADR-0002) → implementer + tester + reviewer on Interface → implementer + tester + reviewer on StreakCalculator Impl.
- Same MoodJournal spec used in prior shakedowns.

## Result — the metric the purge was for

```
Combine surface in generated code:
  grep -rn -E 'import Combine|AnyPublisher|PassthroughSubject|CurrentValueSubject|@Published|\.sink\b|AnyCancellable' \
    --include='*.swift' Packages/
  → 0 hits

VIPER shape in generated code:
  find Packages -name '*Presenter.swift' -o -name '*Interactor.swift' \
       -o -name '*Router.swift' -o -name '*Contract.swift'
  → 0 files
```

**Zero Combine, zero VIPER across 8 subagent dispatches.** Every implementer wrote `async/await` / `AsyncStream<T>` for the reactive surfaces the Interface required. No agent proposed a Combine bridge, no agent proposed a Presenter/Interactor/Router split.

## Comparison

| Shakedown | Overlay Combine posture | Combine hits in generated code | VIPER hits |
|---|---|---:|---:|
| `-2` (2026-07-17) | Combine allowed for reactive UI streams | not measured (feature was smaller) | 0 |
| `-3` (2026-07-18 pre-purge) | Combine allowed narrowly | not measured (no reactive surface exercised) | 0 |
| `-4` (2026-07-18 post-purge) | Combine banned everywhere | **0** | **0** |

The `-4` slice exercised a genuinely reactive surface — `MoodJournalRepository.stream()` — which is the exact place a Combine-trained implementer would reach for `AnyPublisher<[MoodEntry], Never>` or `PassthroughSubject`. The post-purge implementer wrote `AsyncStream<[MoodEntry]>` cleanly, no hesitation.

## Build + test

```
swift build Packages/FeatureMoodJournal        → green
swift test  Packages/FeatureMoodJournal        → 36/36 passed
swiftformat --lint                             → clean
swiftlint --strict                             → clean on touched targets
```

## Reviewer §3.5 no-combine scanner

Both reviewer dispatches ran the new scanner. Both reported: `Combine surface: 0 hits`. `VIPER: 0 files`. Scanner works end-to-end.

## Preamble metric (secondary — Sonnet-5 residual)

Only 1 of the 4 Sonnet-5-tier dispatches (implementer x2 + tester x2) leaked a preamble: `"Build clean. Emit response."` — matches the residual documented in `sonnet5_narrate_first_prior.md`. Opus roles (architect + reviewer) obeyed the schema perfectly. No new preamble-tier regression from the purge.

## Verdict

Purge held. The overlay's `async/await`-only + no-VIPER doctrine survived a full multi-agent pipeline that exercised the exact reactive surface where the old contract would have permitted Combine.
