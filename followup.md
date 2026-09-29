# Followup

## Status
- 2026-09-27/29: Plan 1 закрыт кроме #17 (Alex) + хвост #60/#62. Plan 2 закрыт (#23–#31, #52, #56, #64) + хвост #47, #59, #67. #53 done — PR #69: контракт ожидания (sleep 120–180 + timeout), P2 не штрафует sleep, busy-poll сигнал. Решения Alex: auto-merge везде; --delete-branch остаётся; required CI; guard fail-open, deny везде.
- Пересчёт после #53: run #31 60 → 75. Run #64 (247M): карточка теперь показывает «busy-poll task-runner: 260 Bash подряд без sleep/правок» — сигнал AC5 работает (строка 6, ниже штрафов).
- Score 79/100 · Solid · done · 2026-09-29-issue-53-async-wait-p2-exempt · 47.1M tok · 7 dispatch · 339 calls · 53 min · −10 P7 17 (guard: 7) · −5 P5 reviewer block · −3 P3

## Next
- В работе: #62 (гигиена checkout/worktree у раннера и pr-shepherd, doctor warn). Затем #60 → #47 → #67 → #59.
- Вопросы Alex: деплой-копии (#59), NORTH_STAR.md (#60), #17 ruleset, старые remote-ветки PR #3–#9. Local main = origin/main (b9fb57c).
