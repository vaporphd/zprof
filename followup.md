# Followup

## Status
- 2026-09-27: фаза 1 per-task scorecard реализована на ветке feat/task-scorecard (24 коммита), PR #10 → main, issue #11.
- Все 11 задач плана прошли task-ревью; финальное ревью (opus) → 1 Critical + 6 Important закрыты одной волной; scoped re-review чистый.
- Тесты на head: pytest 181, go test ./... зелёный.

## Next
- PR expert-panel (#12): pr-shepherd мержит сам по новому контракту; после merge — `zprof sync` в проектах, панель доступна фразой «собери панель экспертов».
- pr-shepherd: pre-flight + delivery verification PR #10, затем merge делает Алекс.
- После merge: zprof sync в jarvis-in-hermes и apple-health-sync; первые живые карточки; калибровка порогов Ideal/Solid.
