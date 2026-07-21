## GitHub issue-driven PR pipeline (strict)

Процесс-overlay поверх любого стека. Полный контур issue-driven разработки: от планирования бэклога до merged + synced spec.

### Trigger phrases
- EN: `plan the milestone`, `groom backlog`, `pick next issue`, `open PR for issue N`, `merge approved PRs`, `sync spec after PR`
- RU: `спланируй милестоун`, `разложи бэклог`, `возьми следующий issue`, `открой PR для issue N`, `слей одобренные`, `синкани спек после PR`

### Pipeline (canonical sequence)
```
backlog empty   → planner (DRAFT) → plan-reviewer (base gate) → planner (AUTHOR: gh issue create)
issue picked    → main-session dispatches architect (if ADR-trigger=yes) → implementer
                  (implementer: branch issue-<N>-<slug> → gh pr create Closes #N)
PR opened       → integration-gate (если diff в INTEGRATION_SCOPE) → wiki-keeper → reviewer
approved        → pr-shepherd (если AUTO_MERGE=on) → squash-merge + delete branch + stamp
post-merge      → spec-maintainer (docs/PROJECT_SPEC.md) + docs-writer (README/CLAUDE/followup/lessons)
```

### Ownership map — who edits what
| File / path | Owner agent | Nobody else touches |
|---|---|---|
| `docs/adr/*` | `architect` (stack overlay) | ✓ |
| `docs/PROJECT_SPEC.md` | `spec-maintainer` | ✓ |
| `docs/wiki/**` | `wiki-keeper` | ✓ |
| `README.md`, `CLAUDE.md`, `followup.md`, `lessons.md`, `tasks/todo.md` | `docs-writer` | ✓ |
| `.githooks/**`, `.github/workflows/**`, root build tooling | `ci-devops` | ✓ |
| `src/**` (code) | `implementer` / `bug-hunter` / `refactor-agent` (stack overlay) | ✓ |
| `**/test/**`, `**/integrationTest/**` | `tester` (stack overlay) | ✓ |
| GitHub issue create / edit | `planner` (AUTHOR mode only) | ✓ |
| `gh pr merge` | `pr-shepherd` (when AUTO_MERGE=on) | ✓ |

### Base gates (already in `base/agents/gates/` — this overlay references, does not redefine)
- `north-star-auditor` — pre-dispatch: does this task actually move the project forward?
- `plan-reviewer` — after `planner` DRAFT: is the decomposition sane, ordered, complete?
- `evidence-auditor` — before any quantitative conclusion: is the number real?

### Композит со стеком
```
zprof apply <stack-overlay> issue-loop-github-strict
```
Примеры валидированных стеков:
- `zprof apply backend-kotlin-jvm issue-loop-github-strict` — Kotlin JVM с полным GitHub-контуром
- `zprof apply backend-python issue-loop-github-strict` — Python с полным GitHub-контуром
- `zprof apply systems-rust issue-loop-github-strict` — Rust с полным GitHub-контуром

Overlay стеко-агностичен: `integration-gate` читает `<INTEGRATION_GATE>` из проектного `CLAUDE.md`, `ci-devops` — `<LINT_CMD>`/`<TEST_CMD>` оттуда же.

### Специальные диспатчи
| Задача | Агент |
|---|---|
| Планирование милестоуна / груминг бэклога | `planner` |
| Merge approved PR (only if AUTO_MERGE=on) | `pr-shepherd` |
| Integration gate против реальной внешней системы | `integration-gate` |
| Sync docs/PROJECT_SPEC.md after merge | `spec-maintainer` |
| Sync docs/wiki/ atomically with code PR | `wiki-keeper` |
| Sync README/CLAUDE.md/followup.md/lessons.md after merge | `docs-writer` |
| Изменения build config / hooks / CI | `ci-devops` |
