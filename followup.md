# Followup

## Status
- 2026-09-27/29: Plan 1 закрыт кроме #17 (Alex) + #53/#60/#62. Plan 2 guard: #23–#30 + #52 + #56 + #64 done (PR #48–#66); #31 закрыт аудитом shakedown (PR #68) — один подтверждённый ложный deny (readonly_mutation vs mkdir в allow_write_prefixes у read-only ролей) вынесен в #67; осталось #47, #59. Решения Alex: auto-merge везде; --delete-branch остаётся; required CI; guard fail-open, deny везде (кроме branch_pr_merged).
- `zprof doctor` теперь работает на самом zprof (telemetry-only): guard-проверки OK, 2 info.
- Score 81/100 · Solid · done · 2026-09-29-doctor-telemetry-only · **247M tok · 9 dispatch · 1066 tool calls · 29 min** · −10 P7 19 (guard: 10) · −9 P3 wiki-keeper 9/20 Read
- Стоимость вышла из-под контроля: #29 106M, #64 247M (1066 вызовов за 29 мин = ~37 вызовов/мин — цикл). Разбор по tool-events — в thoughts; отдельный issue по итогам.

## Next
- Открыто: #47, #59, #67; Plan 1 хвост: #53, #60, #62.
- Cost review: кто и что крутит в цикле (pr-shepherd polling? wiki-keeper re-reads?) → issue после анализа. Вопросы Alex: #59, #60, #17, старые remote-ветки.
- Local main = origin/main (50a96f3). Untracked docs/*, thoughts/, tasks/plan-issue-*.md, .claude/zprof-guard.py, guard.json не трогать.
