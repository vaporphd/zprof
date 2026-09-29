# Followup

## Status
- 2026-09-27/29: Plan 1 закрыт кроме #17 (Alex); #60 done — PR #71 (north-star: skip без NORTH_STAR.md, гейт условный, doctor info). Plan 2 закрыт + хвост #59, #67; #47 done — guard `_target()` masks env/export/quoted-value secret leaks (shlex + `_mask_assignment`). Последнее: #53 PR #69 (ожидание через sleep, P2 exempt, busy-poll сигнал), #62 PR #70 (гигиена checkout/worktree, doctor checkGitCheckoutHygiene). Решения Alex: auto-merge везде; --delete-branch остаётся; required CI; guard fail-open, deny везде.
- Score 86/100 · Ideal · done · 2026-09-29-issue-62-worktree-main-checkout (хвост после 429) · 5.1M tok · 2 dispatch · 50 calls · 10 min · −10 P7 7 (guard: 2) · −4 P1 · busy-poll pr-shepherd 14 Bash подряд
- Итого за 2026-09-27…29: 25 issues закрыто через PR #32–#70, все merge — pr-shepherd сам; 6 run'ов прерваны лимитами и подняты resume_from без потерь.

## Next
- #67 done (guard readonly_mutation scratch-path fix), PR pending pr-shepherd. Затем #59.
- Вопросы Alex: деплой-копии (#59) — трекать или `git rm --cached`; NORTH_STAR.md для zprof (#60); #17 ruleset на main; старые remote-ветки PR #3–#9; разбор стоимости (busy-poll теперь виден в карточке).
- Local main = origin/main (4885d73). Guard активен и для main: стоп-лист-литералы в тексте Bash (JSON/heredoc) → хук отклонит вызов; такие тексты — через Write. Untracked docs/*, thoughts/, tasks/plan-issue-*.md, .claude/zprof-guard.py, guard.json не трогать.
