# Followup

## Status
- 2026-09-27: panel-2026-09-27 (#14–#22 + #34/#36/#37, plan-1.md); guard (#23–#31, plan-2.md). Решения Alex: auto-merge везде; --delete-branch остаётся; защита контрактов = required CI; guard fail-open, deny везде.
- Done: #14 PR #32, #22 PR #33, #34 PR #35, #15 PR #38, #16 PR #39 (pr-shepherd: rulesets + self-run tests, ADR 0002), #19 PR #40 (глобальный max_dispatches: runner.max_dispatches, deprecated audit.max_dispatches alias). #18 закрыт как superseded #28.
- Score 76/100 · Solid · done · 2026-09-27-pr-shepherd-local-green-tests · confidence full
  30.3M tok · 12 dispatch · 298 tool calls · 43 min · sonnet×10 opus×3 · −10 P7 17 нарушений (7 ролей) · −5 P5 reviewer block ×1 · −4 P1 4% tool errors
- Тренд P7: 15 → 5 → 17 нарушений формата за run; implementer −10. Аргумент за #26 (валидатор) и #20 (реестр).

## Next
- В работе: #20 (реестр verdicts.yaml). Затем #21, #36, #37; Plan 2 с #23. #17 — руками Alex.
- 5 старых remote-веток PR #3–#9 висят — удалять решает Alex.
- Local main = origin/main (337c3cf). Untracked docs/*, thoughts/ не трогать.
