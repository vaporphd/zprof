# Followup

## Status
- 2026-09-27/28: Plan 1 закрыт кроме #17 (Alex) + #52/#53. Plan 2 guard: #23–#27 done (PR #48–#55), #56 done (PR #57: RUF059 + ruff 0.16.9 запинен в CI/CLAUDE.md/ruff.toml, CI на main снова зелёный). Решения Alex: auto-merge везде; --delete-branch остаётся; required CI; guard fail-open, deny везде (кроме branch_pr_merged).
- Score 78/100 · Solid · done · 2026-09-28-ruff-ruf059-ci-pin · confidence full
  19M tok · 9 dispatch · 196 tool calls · 30 min · sonnet×7 opus×3 · −10 P7 18 нарушений (6 ролей) · −6 P6 12% · −5 P1 5%
- Урок: local-green pr-shepherd брал команды из `## Executing`, где не было ruff — два merge прошли мимо CI-гейта. Теперь ruff в `## Executing` через `uvx ruff@0.16.9`. Структурно закрывает #17 (CI-green).

## Next
- #28 закрыт (zprof apply деплоит guard: guard.json, хуки с matcher, permissions.deny, GuardConfig), review round 2 approve, PR открыт → pr-shepherd. Далее по backlog: #52 (приоритет) → #29, #30 → #31; #47, #53.
- После #28 guard-хуки живут в settings.local.json zprof: SubagentStop-валидатор начнёт возвращать агентов с ответами не по схеме — ждём падения P7 и retry-диспатчей.
- #17 и старые remote-ветки PR #3–#9 — Alex. Local main = origin/main (95ad98f). Untracked docs/*, thoughts/, tasks/plan-issue-*.md не трогать.
