pipeline:
  issue-loop-github-strict:
    issue_tracker: "GitHub (gh CLI)"
    # These values are per-project — this claude-block declares the KEYS the
    # pipeline reads. The project's own CLAUDE.md `## Project Configuration`
    # populates each. Leave placeholders here and edit per project.
    default_branch: "main"                # override per project
    merge_gate: "local-green"             # local-green | CI-green
    auto_merge: "off"                     # on | off — controls pr-shepherd dispatch
    integration_scope: []                 # list of paths/globs whose PRs must exercise <integration_gate>
    integration_gate: "<see stack overlay's claude-block>"
    build_cmd: "<see stack overlay's claude-block>"
    test_cmd: "<see stack overlay's claude-block>"
    lint_cmd: "<see stack overlay's claude-block>"
    format_cmd: "<see stack overlay's claude-block>"
    type_labels: [feat, fix, refactor, docs, chore, test, perf]
    area_labels: []                       # project-specific bounded contexts
    milestones: []                        # project's milestone list
    # ADR-exclusion list — commit types that skip spec-maintainer sync
    adr_exclusion:
      - "pure docs (README/CLAUDE.md/wiki-only diff)"
      - "pure style/formatter (ktlintFormat / black / cargo fmt only)"
      - "pure CI/hook tuning (.githooks or .github/workflows only)"
      - "bug fix < 10 lines"
      - "retro-ADR-only (adds an ADR documenting a shipped decision, no code)"
    # Pipeline shape — the canonical sequence agents run in
    pipeline_shape: |
      backlog empty   → planner (DRAFT) → plan-reviewer (base gate) → planner (AUTHOR, gh issue create)
      issue picked    → main-session dispatches architect (if ADR-trigger=yes) → implementer
      PR opened       → integration-gate (if in INTEGRATION_SCOPE) → wiki-keeper → reviewer
      approved        → pr-shepherd (if AUTO_MERGE=on) → merge + delete branch + stamp
      post-merge      → spec-maintainer (docs/PROJECT_SPEC.md) + docs-writer (README/CLAUDE.md/followup.md/lessons.md)
