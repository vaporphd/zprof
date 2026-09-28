# Followup

## Status
- 2026-09-27/29: Plan 1 закрыт кроме #17 (Alex) + #53/#60/#62. Plan 2 guard: #23–#30 + #52 + #56 + #64 done (PR #48–#66); осталось → #31; #47, #59. Решения Alex: auto-merge везде; --delete-branch остаётся; required CI; guard fail-open, deny везде (кроме branch_pr_merged).
- Score 88/100 · Ideal · done · 2026-09-28-issue-30-guard-doctrine-contracts (хвост после 429) · 16.6M tok · 2 dispatch · 127 tool calls · 10 min · −10 P7 6 (guard: 3) · −2 P1 2%
- Guard доктрина в managed-блоке CLAUDE.md, контракты pr-shepherd/task-runner про deny, overlay guard.yaml (ios-swift, backend-python). Копии агентов и коллектора = источник.

## Next
- #64 done (PR #66, doctor поддерживает telemetry-only проекты без .zprof.yaml). Далее #31 shakedown (включить `zprof doctor` на zprof); #47, #59; Plan 1 хвост: #53, #60, #62.
- После Plan 2: разбор стоимости (run #29 106M, #20 92M) и retry по формату; вопросы Alex: деплой-копии (#59), NORTH_STAR.md (#60), #17, старые remote-ветки.
- Local main = origin/main (26b34e1). Untracked docs/*, thoughts/, tasks/plan-issue-*.md, .claude/zprof-guard.py, guard.json не трогать.
