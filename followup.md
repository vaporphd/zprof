# Followup

## Status
- 2026-09-27: панель → milestone panel-2026-09-27 (#14–#22, plan-1.md); guard → спека + milestone guard (#23–#31, plan-2.md). Решения Alex: auto-merge везде; --delete-branch остаётся; защита контрактов = required CI; guard fail-open, deny везде.
- #14 done — PR #32 (3d9f914): gofmt/pytest/ruff гейтят merge. #22 done — PR #33 (0a14357): config_hash/verdict, `zprof apply --telemetry-only`, коллектор и Stop-хук в zprof актуальны.
- Карточки всё ещё нет: issue #34 заведён, фикс в работе на ветке fix/34-async-dispatch-notification-stitch (loop).

## Next
- Bug: коллектор ↔ async Agent (task-notification) → task-runner, вне очереди (без него ни одной карточки).
- Затем по todo.md: #15 → #16, #18, #19 → #20 → #21; Plan 2 с #23. #17 — руками Alex.
- Local main = origin/main (0a14357). Untracked docs/*, thoughts/ не трогать.
