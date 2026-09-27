# Followup

## Status
- 2026-09-27: panel-2026-09-27 (#14–#22 + #34/#36/#37, plan-1.md); guard (#23–#31, plan-2.md). Решения Alex: auto-merge везде; --delete-branch остаётся; защита контрактов = required CI; guard fail-open, deny везде.
- Done: #14 PR #32 (CI-гейты), #22 PR #33 (config_hash/verdict, --telemetry-only), #34 PR #35 (сшивка async task-notification), #15 PR #38 (стоп-лист: несмерженные ветки). Карточки работают.
- Score 90/100 · Ideal · done · 2026-09-27-stop-list-unmerged-branches · confidence full (возобновлённая часть run после 429)
  3.5M tok · 2 dispatch · 42 tool calls · 5 min · sonnet×2 opus×1 · −10 P7 5 нарушений контракта (pr-shepherd, reviewer, task-runner)
- Предыдущая карточка #34: 78/100 Solid, P7 15 нарушений у 8 ролей → аргумент за #26/#20.

## Next
- #16 реализован (branch feat/issue-16-pr-shepherd-local-green-tests, ветка не запушена) — pr-shepherd в local-green ищет тест-команды по всему CLAUDE.md (ADR 0002, ## Executing не источник) + видит rulesets; PR ещё не создан.
- По todo.md: #16 → #18, #19 → #20 → #21, #36, #37; Plan 2 с #23. #17 — руками Alex.
- Guard-спека дополнена: branch_pr_merged для pr-shepherd (комментарии в #23/#24). 5 старых remote-веток PR #3–#9 висят — удалять решает Alex.
- Local main = origin/main (14d89d8). Untracked docs/*, thoughts/ не трогать.
