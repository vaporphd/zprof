# Followup

## Status
- 2026-09-27/29: Plan 1 закрыт кроме #17 (Alex). Plan 2 закрыт (#23–#31, #52, #56, #64, #47, #59, #67); хвост: #75 (ложный deny readonly_mutation у reviewer), #73 (`ln` bypass). Решения Alex: auto-merge везде; --delete-branch остаётся; required CI; guard fail-open, deny везде.
- #59 done — PR #76: .claude/zprof-guard.py в git, guard.json в .gitignore. Score 87/100 Ideal · 9.2M · 2 dispatch · 14 min · busy-poll pr-shepherd 18.
- Итого 2026-09-27…29: 30 issues закрыто через PR #32–#76, все merge — pr-shepherd сам; 7 run'ов прерваны лимитами, подняты resume_from; 3 ложных deny guard на read-only ролях (mkdir → #67 done; git log, cmp → #75).

## Next
- В работе: #75 (readonly_mutation: ложный deny на read-only команде reviewer). Затем #73.
- Решения Alex: #17 ruleset на main; политика деплой-копий (сейчас трекаются по прецеденту); NORTH_STAR.md для zprof; старые remote-ветки PR #3–#9. Далее — cost review (P3/P4 implementer, длина guard-run'ов до 99 мин) и `zprof sync` в jarvis-in-hermes / apple-health-sync (получат guard, expert-panel, новые контракты).
- Local main = origin/main (78996cf). Guard активен и для main: стоп-лист-литералы в тексте Bash → deny; такие тексты — через Write.
- #75 готов: fix/test/docs на ветке `fix/75-guard-readonly-mutation-reviewer-deny` (HEAD a7f3290), reviewer approved, PR открыт — передано pr-shepherd.
