# Followup

## Status
- 2026-09-27/29: Plan 1 закрыт кроме #17 (Alex) + хвост #60/#62. Plan 2 guard закрыт: #23–#31 + #52 + #56 + #64 (PR #48–#68); хвост #47, #59, #67. Решения Alex: auto-merge везде; --delete-branch остаётся; required CI; guard fail-open, deny везде (кроме branch_pr_merged).
- Shakedown #31 (docs/reviews/2026-09-29-guard-shakedown.md): за Plan 2 deny 6 / block 40 / allow_unverified 1; ложный deny один — bug-hunter `mkdir -p` в scratchpad (readonly_mutation без allow-префиксов) → #67; reviewer `git log` с редиректом — неопределимо (полная команда не логируется).
- #53 done (ветка fix/async-wait-p2-exempt-53): task-runner.md — контракт ожидания async-ребёнка (sleep 120–180 + явный timeout + запрет busy-poll); p2_exempt_patterns исключает sleep/wait из P2; новый сигнал busy-poll (не P1-P7). Run #25 до/после: score 47→62, P2 15→0; тот же прогон поймал 25× подряд `gh pr view` у pr-shepherd новым сигналом.
- Стоимость: #29 106M, #64 247M (busy-poll: 595× git log, 199× date у task-runner) — теперь виден как сигнал в карточке, не только постфактум.

## Next
- Дальше: #62 → #60 → #47 → #67 → #59.
- Вопросы Alex: деплой-копии (#59), NORTH_STAR.md (#60), #17 ruleset, старые remote-ветки PR #3–#9.
- Local main = origin/main (42ff888). Untracked docs/*, thoughts/, tasks/plan-issue-*.md, .claude/zprof-guard.py, guard.json не трогать.
