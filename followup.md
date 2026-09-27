# Followup

## Status
- 2026-09-27: panel-2026-09-27 (#14–#22 + #34/#36, plan-1.md); guard (#23–#31, plan-2.md). Решения Alex: auto-merge везде; --delete-branch остаётся; защита контрактов = required CI; guard fail-open, deny везде.
- Done: #14 PR #32 (CI-гейты), #22 PR #33 (config_hash/verdict, --telemetry-only), #34 PR #35 (сшивка async task-notification). Карточки заработали.
- #15 закрыт: stop_list переформулирован на «удаление несмерженных веток и тегов»; CLAUDE.md и manifest.yaml синхронны (рендер проверен через zprof apply в scratch).
- Score 78/100 · Solid · done · 2026-09-27-collector-async-dispatch-stitch · confidence full
  33M tok · 9 dispatch · 297 tool calls · 43 min · sonnet×7 opus×3
  −10 P7 15 нарушений контракта (8 ролей) · −7 P3 bug-hunter 15 перечитываний · −5 P1 14/297 tool errors

## Next
- По todo.md: #15 → #16, #18, #19 → #20 → #21, #36; Plan 2 с #23. #17 — руками Alex.
- Сигнал из карточки: P7 15 нарушений формата ответа у 8 ролей за один run — аргумент за #26 (валидатор) и #20 (реестр).
- Local main = origin/main (5f8fd1a). Untracked docs/*, thoughts/ не трогать.
