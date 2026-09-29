# Followup

## Status
- 2026-09-27/29: Plan 1 закрыт кроме #17 (Alex). Plan 2 закрыт + хвост: #75 done — PR #77 (regex `\bgit\s+(commit|…)\b` матчил `merge-base`/`commit-tree`/`checkout-index`; → `(?![\w-])`, `stash list/show` вне мутаций, `/dev/null` безопасный sink); #73 done — PR #78 (`ln` в мутациях + операнды под scratch). #79 done — PR #80 (RE2 в Go не знает lookahead → оба git-паттерна из #75 молча выпадали из compilePatterns, P2/P3 слепли к git-мутациям; фикс — lookahead-free regex + compilePatterns/LoadConfig fail loud вместо silent drop). Решения Alex: auto-merge везде; --delete-branch остаётся; required CI; guard fail-open, deny везде.
- Smoke после #75 (роль reviewer): `git merge-base … 2>/dev/null`, `cmp … && echo`, `git log`, `git stash list` → allow. Score 70/100 · 41.2M · 8 dispatch · 39 min · −10 P7 20 (guard: 7) · −8 P3 implementer 11/29 · −6 P1 6%.
- Итого 2026-09-27…29: 31 issue закрыт через PR #32–#77, все merge — pr-shepherd сам; 8 run'ов прерваны лимитами, подняты resume_from.

## Next
- #79 (RE2 lookahead silent drop): PR #80 gate-green, reviewer done (без P0/P1) — готов к мержу pr-shepherd'ом. После него очередь пуста.
- Решения Alex: #17 ruleset на main; политика деплой-копий; NORTH_STAR.md для zprof; старые remote-ветки PR #3–#9. Далее: cost review (P3 implementer стабильно 5–30 перечитываний; run'ы guard 30–99 мин), `zprof sync` в jarvis-in-hermes / apple-health-sync, повторная панель (`focus: [01, 02, 06]`) для «Динамики».
- Local main = origin/main (1160b00). Guard активен и для main: стоп-лист-литералы в тексте Bash → deny; такие тексты — через Write.
