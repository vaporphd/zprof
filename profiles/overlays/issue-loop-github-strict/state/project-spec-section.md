## Pipeline / merge policy section (для PROJECT_SPEC.md)

### Issue tracker
- GitHub (via `gh` CLI)
- Repository: `<owner/repo>`
- Default branch: `<main>`

### Merge policy
- `MERGE_GATE`: <local-green | CI-green>
  - `local-green`: pre-commit + pre-push hooks (`.githooks/*`) ARE the merge gate. CI is a courtesy or absent.
  - `CI-green`: GitHub Actions checks must pass; pr-shepherd uses `--auto` mode.
- `AUTO_MERGE`: <on | off>
  - `on`: pr-shepherd dispatched after reviewer approves; auto-squash-merges verified PRs.
  - `off`: main session stops at reviewer approve; a human merges.

### Integration gate
- `INTEGRATION_SCOPE`: <list of file paths/globs whose PRs must exercise the real gate>
  - Example: `datastore/src/main/kotlin/**`, `marketdata/src/main/kotlin/**`, anything touching the wire protocol.
- `INTEGRATION_GATE`: `<the exact command that exercises the real system>`
  - Example: `./gradlew integrationTest` (Kotlin JVM), `pytest tests/integration/` (Python), `cargo test --test integration` (Rust).
- The gate validates against the REAL external system — never mocks. Oracle values re-derived from real system runs; never editable to silence red diff.

### Labels + milestones
- `TYPE_LABELS`: feat, fix, refactor, docs, chore, test, perf
- `AREA_LABELS`: <project-specific bounded contexts — one label per module or logical area>
- `MILESTONES`: <list of active + planned milestones>

### Branch convention
- Feature branches: `issue-<N>-<slug>`
- Direct push to `<default-branch>`: allowed ONLY for docs-only diffs (`docs/**`, `tasks/**`, `followup.md`, `lessons.md`) — enforced by `.githooks/pre-push`.

### Placeholder / stamp convention (optional)
- If the project uses `PR N` / `0000000` placeholders in `tasks/todo.md` + `followup.md` that pr-shepherd replaces at merge:
  - Script: `scripts/stamp-merge.sh <PR> <SHA>`
  - Placeholder shape: `PR N` for issue #, `0000000` for merge SHA (implementer writes these; pr-shepherd stamps).
- If not: skip; pr-shepherd will report "no stamp convention".

### ADR-EXCLUSION list (spec-maintainer skips sync for these commit types)
- Pure docs (README / CLAUDE.md / wiki only)
- Pure style / formatter (ktlintFormat / black / cargo fmt only)
- Pure CI / hook tuning (`.githooks/*` or `.github/workflows/*` only)
- Bug fix `< 10` lines
- Retro-ADR-only (adds ADR documenting shipped decision; no code)

### Agent ownership (from issue-loop-github-strict + base gates + stack overlay)
- Upstream: `north-star-auditor` (base) → `planner` → `plan-reviewer` (base) → `planner` (AUTHOR)
- Execution: `architect` (stack) → `implementer` (stack) → `tester` (stack)
- Pre-merge: `integration-gate` → `wiki-keeper` → `reviewer` (stack)
- Merge: `pr-shepherd`
- Post-merge: `spec-maintainer` → `docs-writer`
- Infrastructure: `ci-devops`
- Support: `bug-hunter` / `refactor-agent` / `explorer` (stack) — dispatched on demand
