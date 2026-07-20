# Model-routing eval — methodology + reproduction guide

How zprof overlay per-role model routing is empirically evaluated. Written after the KMP (`docs/reviews/model-eval/`) and iOS Swift (`docs/reviews/ios-model-eval/`) evals; both used identical mechanics.

## 1. What the eval measures

Each overlay agent (`.claude/agents/*.md`) declares a `model:` in its frontmatter — e.g. `sonnet`, `opus`, `haiku`, or an exact `claude-*` ID. Different roles have different failure modes and different token appetites. The eval answers **per role**:

- Is the default tier necessary, or can we downgrade (cost win)?
- Is an upgrade warranted (quality win — sometimes also cost-negative when a stronger model iterates less)?
- Does a downgrade in one role interact with the tier of another (e.g. weaker impl × weaker tester)?

The output is a `RESULTS.md` with a per-role verdict table and a recommended production `model_overrides:` block for the overlay.

## 2. Components

### 2.1 The overlay under test
A zprof overlay (e.g. `profiles/overlays/ios-swift/`) applied into a project via `zprof apply <overlay>`. Apply creates `.claude/agents/*.md` in the host, plus `PROJECT_SPEC.md` scaffold, ADR skeleton, workflows, and managed CLAUDE.md blocks. The `model:` field in each agent's frontmatter is the switch this eval flips.

### 2.2 Test hosts
Independent directories, one per experimental run. Each is a fresh SPM/Gradle/KMP scaffold + `zprof apply <overlay>`. Hosts must be isolated — separate `.build/`, separate git, no shared caches — so that concurrent shakedowns don't cross-contaminate.

Naming convention: `zprof-test-<overlay>-eval-<variant>` (e.g. `zprof-test-ios-eval-baseline`, `zprof-test-ios-eval-b`, ...).

### 2.3 The feature spec
A single feature spec is fixed across every run so the varying role's output is comparable across hosts. Past evals used **MoodJournal** — a small iOS/KMP mood-tracking feature with a public API surface (value types + protocols + navigation route) plus persistence + streak algorithm. The spec lives in the architect's ADR-0002 and covers enough types to exercise: Codable round-trip validation, calendar-date invariants, protocol contracts, layer purity.

### 2.4 Subagent dispatch mechanism
This eval was run from a Claude Code parent session. Each role dispatch is an `Agent` tool call with:
- `subagent_type: general-purpose`
- `model: opus | sonnet | haiku` (parent Agent tool only accepts these aliases — no exact IDs; `opus-4-6[1m]` is proxied via `opus` (4.8)).
- The full text of the role's `.md` contract as system context via the prompt.

The subagent reads its contract file inside the host directory (which is where the overlay was applied), performs the role, and returns the schema block declared in the contract's `return_format:` frontmatter.

Alternative dispatch mechanism (not what this eval used): `claude -p` headless with fan-out via `dev-orchestrator`. The `CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS=600s` default made this brittle for full pipelines; general-purpose subagents proved more reliable.

### 2.5 Metrics + verdicts
Each run emits:
- **Reviewer report** — markdown under `<host>/docs/reviews/YYYY-MM-DD-*.md` with C/I/M/S findings + verdict + file:line references.
- **Metrics JSON** — under `docs/reviews/<overlay>-model-eval/sh-*.json`, structured fields for aggregation (verdict, C/I/M/S counts, build/test results, forbidden-imports count, preamble-leak flag, subagent tokens, wall time, per-role model IDs).

### 2.6 Decision rule (asymmetric)
A downgrade is accepted iff **all** of the following hold vs baseline:
- reviewer verdict ≥ baseline (BLOCK stays BLOCK is neutral; APPROVE → BLOCK is a loss)
- overlay findings (C/I/M/S) not worse
- code findings not worse
- toolchain green (compile / test / lint / format)
- preamble leak = false
- layer-purity greps clean
- (overlay-specific) no manifest regressions

Any regression rejects the downgrade → keep baseline tier for that role.

## 3. Experimental design

### 3.1 Base matrix (7 runs)
For an overlay with roles {architect, impl, tester, tool-agents, reviewer}, the standard matrix is:

| # | Variant | Purpose |
|---|---|---|
| 1 | baseline | ground truth — current overlay defaults |
| 2 | tester → haiku | is tester tier reducible? |
| 3 | tool-agent-A → haiku | (e.g. xcode-runner / gradle-runner) |
| 4 | tool-agent-B → haiku | (e.g. xcodegen-driver + spm-manager) |
| 5 | impl → opus (or higher tier) | is upgrade cost-neutral / quality-positive? |
| 6 | architect → sonnet | is arch tier reducible? |
| 7 | min — stack winners | production-candidate integration |

Baseline runs first. Downstream runs may reuse baseline artifacts (differential experiments) to reduce cost — e.g. a tester-variant reuses baseline's arch+impl and only re-dispatches tester+reviewer.

### 3.2 Differential experiments
For any run that changes only a downstream role, `rsync` the earlier phases' artifacts into the new host and dispatch only the changed role + reviewer. This cuts cost ~50% vs re-running the full pipeline per shakedown.

Rules:
- Copy `docs/` (arch artifacts) + `Packages/` (impl artifacts) + `Package.swift`/build manifests + lint configs.
- Strip artifacts belonging to the role being re-evaluated (e.g. delete `Tests/` if re-testing).
- Commit the import as its own commit for git-history clarity: `sh-ios-<variant>: import baseline arch+impl`.
- The evaluated role gets a clean starting state for its work.

### 3.3 Fan-out for wall-clock
Runs writing to different hosts are independent. Dispatch several subagents in parallel — the token budget is the same either way, but wall time drops roughly linearly with concurrency (empirical: 7 runs took ~2:11 wall fanned out vs an estimated ~4h serial).

## 4. Alias resolution + zprof registry

Overlay frontmatter uses aliases; zprof resolves them at apply time:

| Alias | Resolves to |
|---|---|
| `opus` | `claude-opus-4-8` |
| `opus-1m` | `claude-opus-4-7[1m]` |
| `opus-4-6` | `claude-opus-4-6[1m]` |
| `sonnet` | `claude-sonnet-5` |
| `haiku` | `claude-haiku-4-5-20251001` |
| `fable` | `claude-fable-5` |

Exact `claude-*` IDs pass through unchanged (allowing pinned overrides).

**Registry source of truth**: `cli/internal/models/registry.go`.

## 5. Reproducing on a fresh Mac

### 5.1 Prerequisites
- **macOS** with a POSIX shell (bash or zsh) and `git`.
- **Claude Code CLI**: `curl -fsSL https://claude.com/install.sh | sh` or via Anthropic install docs. An active Claude subscription (Pro / Max / Team) authorized in the CLI (`claude auth`).
- **Go 1.22+** — to build zprof: `brew install go`.
- **Domain tools** (per overlay):
  - `ios-swift`: Xcode 16+ (`xcode-select --install` for CLTs, full Xcode from the App Store), `brew install xcodegen`.
  - `kotlin-multiplatform`: JDK 21 (`brew install --cask temurin`), Gradle 8.9+ auto-fetched via wrapper.
- **Python 3** (for aggregating JSON metrics — optional): preinstalled on macOS.

### 5.2 Get zprof
```sh
git clone https://github.com/vaporphd/zprof.git ~/src/zprof
cd ~/src/zprof/cli
go build -o "${GOBIN:-$HOME/go/bin}/zprof" ./cmd/zprof
export PATH="${GOBIN:-$HOME/go/bin}:$PATH"
zprof --version   # expect: zprof version 0.1.0-dev
```

Point zprof at the profiles repo:
```sh
export ZPROF_REPO=~/src/zprof/profiles
```

### 5.3 Prepare eval hosts
```sh
OVERLAY=ios-swift
mkdir -p ~/eval-hosts && cd ~/eval-hosts
for v in baseline b c c2 d e min; do
  HOST=~/eval-hosts/zprof-test-${OVERLAY}-eval-${v}
  mkdir -p "$HOST" && cd "$HOST"
  git init -q
  # Seed a minimal package appropriate to the overlay (see §5.3.1 for ios-swift).
  # ... seed steps ...
  git add -A && git commit -qm "seed"
  zprof apply "$OVERLAY"
done
```

#### 5.3.1 iOS Swift seed
```sh
cat > Package.swift <<'EOF'
// swift-tools-version: 5.10
import PackageDescription
let package = Package(
    name: "MoodJournal",
    platforms: [.iOS(.v17), .macOS(.v14)],
    products: [.library(name: "AppCore", targets: ["AppCore"])],
    targets: [
        .target(name: "AppCore", path: "Sources/AppCore"),
        .testTarget(name: "AppCoreTests", dependencies: ["AppCore"], path: "Tests/AppCoreTests"),
    ]
)
EOF
mkdir -p Sources/AppCore Tests/AppCoreTests
echo 'public enum AppCore { public static let version = "0.1.0" }' > Sources/AppCore/AppCore.swift
cat > Tests/AppCoreTests/AppCoreTests.swift <<'EOF'
import XCTest
@testable import AppCore
final class AppCoreTests: XCTestCase {
    func test_version_isPresent() { XCTAssertFalse(AppCore.version.isEmpty) }
}
EOF
```

### 5.4 Dispatch pipelines
From a Claude Code session parented at any convenient working directory:

1. **baseline**: dispatch architect (`model: opus`) — bootstrap ADR-0001 + PROJECT_SPEC.md, then feature ADR-0002. Then implementer (`sonnet`), tester (`sonnet`), reviewer (`opus`). Each is a separate `Agent` tool call with the role's `.md` referenced by path in the prompt.
2. **variants** (b/c/c2/d/e): `rsync` baseline's frozen phases into the variant host, dispatch only the changed role + reviewer on the target tier.
3. **min**: stack winners (impl → opus, tool-agents → haiku, keep tester on sonnet if downgrade was rejected). Full pipeline.

Prompt template for each dispatch:
```
You are running as the <role> agent from the zprof <overlay> overlay.
Your contract lives at <HOST>/.claude/agents/<role>.md — READ IT COMPLETELY then follow it EXACTLY.

Working directory: <HOST>

Repo state: <describe imported artifacts + last commit SHA>

Task: <specific task derived from the ADR sub-decision>

Return only the return_format YAML block per the frontmatter. First line `verdict:`. No preamble.
```

### 5.5 Aggregation
Each reviewer subagent writes:
- Human-readable report under `<host>/docs/reviews/YYYY-MM-DD-*.md`
- Machine-readable metrics under `~/src/zprof/docs/reviews/<overlay>-model-eval/sh-*.json`

Aggregate manually into `RESULTS.md` — matrix table, per-role verdicts, recommended `model_overrides:` block, cost projections.

### 5.6 Apply verdicts back to the overlay
```sh
# Edit the frontmatter of each agent per your findings:
$EDITOR profiles/overlays/<overlay>/agents/<role>.md   # change `model:` line
# Rebuild + reinstall zprof to pick up any registry alias changes:
cd cli && go build -o "$HOME/go/bin/zprof" ./cmd/zprof
# Verify a fresh apply resolves aliases correctly:
TMP=$(mktemp -d) && cd "$TMP" && git init -q && zprof apply <overlay>
grep "^model:" .claude/agents/*.md
```

## 6. Anti-patterns to avoid

- **Skipping baseline**: differential runs need baseline artifacts. Always run baseline first.
- **Sharing `.build/` across hosts**: SwiftPM's `.build` caches per-toolchain PCH's — a stale one from a sibling host will silently break `swift build`. Fresh host = fresh `.build`.
- **Sharing `.git` across hosts**: git state confuses the review of "what did this role produce".
- **Trusting the schema without disk verification**: reviewer contracts require the report file to actually exist. Grep the referenced `artifact:` path and confirm.
- **Running unbounded parallel dispatches**: 3-5 concurrent subagents is fine; more risks flakiness. Sequence heavier stages, parallel the cheap ones.
- **Interpreting a single-run tester coverage as tier-representative**: tester coverage is stochastic within a tier. If a bug-detection claim matters, run the tester twice on identical impl and compare.

## 7. Known limitations of this eval mechanic

- **Parent Agent tool aliases only cover 4 tiers**: no way to hit exact `claude-opus-4-6[1m]` from the parent session — this eval used `opus` (4.8) as proxy for that tier.
- **Applied-overlay `.claude/agents/*.md` model field is not what the dispatched Claude actually runs when dispatched from a parent Claude Code session** — the parent's `Agent` tool takes the model from its own parameter, not the applied file. To measure the overlay in its native dispatch mode (via `dev-orchestrator` inside a `claude -p` invocation), use §2.4's alternative mechanism.
- **Tester coverage variance is a nuisance for A/B routing conclusions** — if the eval design isn't careful about this, tier verdicts get polluted by within-tier stochasticity.
- **This eval doesn't test tool-agent-in-context interaction unless explicitly wired** — `sh-ios-c` and `sh-ios-c2` tested tool-agents standalone; only `sh-ios-prod` exercised them as part of a full pipeline. Standalone tool-agent success is necessary but not sufficient for production adoption.

## 8. Prior evals + artifacts

- **KMP model-eval** (2026-07-20): `docs/reviews/model-eval/RESULTS.md` + `sh-9*.json`. 6 executed runs, ~3M tokens. Outcome: `implementer → opus-4-6[1m]`; hold tester on sonnet.
- **iOS Swift model-eval** (2026-07-20): `docs/reviews/ios-model-eval/RESULTS.md` + `sh-ios-*.json`. 7 executed runs, ~2.5M tokens. Outcome: `implementer → opus-4-6[1m]`; `xcode-runner / xcodegen-driver / spm-manager → haiku`; hold tester on sonnet.
- **Prod-config validation** (2026-07-20): `sh-ios-prod{,-xcode-runner}.json`. Full pipeline on new overlay config. Verdict: config end-to-end works; BLOCK on real code bugs (not model choices).
