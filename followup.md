# Followup

## Status
- 2026-09-27: scorecard (PR #10), expert-panel (PR #13) в main; панель → milestone panel-2026-09-27 (#14–#22, plan-1.md).
- Решения Alex: auto-merge везде без параметра; --delete-branch остаётся (#15 уточняет стоп-лист); защита контрактов = только required CI; guard fail-open, deny везде, граница main — фаза 2.
- Guard: спека docs/superpowers/specs/2026-09-27-guard-hooks-design.md утверждена → milestone guard (#23–#31, plan-2.md); pr-shepherd не read-only, guard-события с dispatch_id.
- #14 (CI gofmt/pytest/ruff) в PR #32, review чистый (P2: pin ruff, telemetry_test.py вне CI) — ожидает pr-shepherd.

## Next
- #17 (ruleset на main) — руками Alex; pr-shepherd увидит ruleset только после #16.
- «следующая задача» → task-runner берёт по порядку todo.md: Plan 1 с #14, затем Plan 2 с #23 (или наоборот — решение Alex).
- До #20/#26: закоммитить или отбросить незакоммиченные правки auditor-deep.md. Локальные docs-коммиты на main не запушены — уедут с первым PR.
