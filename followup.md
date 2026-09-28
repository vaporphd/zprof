# Followup

## Status
- 2026-09-27/28: Plan 1 закрыт кроме #17 (Alex) + #53/#60. Plan 2 guard: #23–#28 + #56 done; #52 done — PR #61 (сшивка nested async: P6 на run #25 58% → 0, карточка 47 → 62). Осталось #29, #30 → #31; #47, #53, #59, #60. Решения Alex: auto-merge везде; --delete-branch остаётся; required CI; guard fail-open, deny везде.
- Score 78/100 · Solid · done · 2026-09-28-collector-nested-async-dispatch-stitch · confidence full
  32.7M tok · 8 dispatch · 244 tool calls · 39 min · sonnet×6 opus×3 · −10 P7 24 нарушений (guard: 12 deny/block) · −5 P5 reviewer block ×1 · −4 P1 4%
- Guard в деле: 11 блоков return_format за run (task-runner ×3, implementer ×2, reviewer ×2, bug-hunter, tester, wiki-keeper, pr-shepherd — все вернулись переписать, 1 format_unfixed у task-runner); deny: bug-hunter `mkdir`, reviewer редирект в файл — по правилу read-only.

## Next
- В работе: #30 (guard: доктрина, контракты pr-shepherd/task-runner, overlay). Затем → #31; #47, #53, #59, #60.
- Вопросы Alex: трекать ли деплой-копии (#59) или `git rm --cached`; нужен ли docs/NORTH_STAR.md для zprof (#60); #17; старые remote-ветки PR #3–#9.
- Local main = origin/main (571deb3). Untracked docs/*, thoughts/, tasks/plan-issue-*.md, .claude/zprof-guard.py, .claude/guard.json не трогать.
