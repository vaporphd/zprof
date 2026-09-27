# Followup

## Status
- 2026-09-27: panel-2026-09-27 (#14–#22 + #34/#36/#37, plan-1.md); guard (#23–#31, plan-2.md). Решения Alex: auto-merge везде; --delete-branch остаётся; защита контрактов = required CI; guard fail-open, deny везде.
- Done: #14 PR #32, #22 PR #33, #34 PR #35, #15 PR #38, #16 PR #39, #19 PR #40 (runner.max_dispatches=14, audit.max_dispatches deprecated). #18 superseded #28.
- Done: #20 PR #41.
- Score 69/100 · Solid · done · 2026-09-27-runner-global-max-dispatches · confidence full
  25.8M tok · 10 dispatch · 227 tool calls · 30 min · sonnet×9 opus×2 · −10 P7 19 нарушений (7 ролей) · −6 P6 12% токенов без результата (general-purpose, implementer) · −5 P1 5%
- Тренд P7 за run: 15 → 5 → 17 → 19; implementer −13. Runner диспатчит general-purpose вместо отсутствующего spec-maintainer → #21.

## Next
- Done: #20. В работе: #21, #36, #37; Plan 2 с #23. #17 — руками Alex.
- Раннер #19 оставил checkout на feature-ветке и не выровнял main — main сделал сам; локальные merged-ветки не удаляю (решение Alex).
- Local main = origin/main (4cbb907). Untracked docs/*, thoughts/ не трогать.
