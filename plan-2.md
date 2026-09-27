# Plan 2: guard
created: 2026-09-27
source: docs/superpowers/specs/2026-09-27-guard-hooks-design.md (фаза 1, §4–§11)
milestone: guard

Решения владельца (спека §2): fail-open на ошибке скрипта; deny везде (main и субагенты), никакого `ask`;
глобальный guard с ролью из `transcript_path`/`meta.json` + `permissions.deny` как страховка. Фаза 2 (§12) вне плана.

| # | Issue | Title | Type | Complexity | Depends | Status |
|---|-------|-------|------|------------|---------|--------|
| 1 | #23 | feat(base): zprof-guard.py — каркас, стоп-лист без контекста, read-only роли (§4, §5.1, §5.4, §5.7, §7, §8.1) | feat | L | — | open |
| 2 | #24 | feat(base): guard — контекстные правила head_on_remote, linked_worktree, запись вне репо (§5.2, §5.3) | feat | M | #23 | open |
| 3 | #25 | feat(base): guard — merge-гейт и PR-гейт (§5.5, §5.6) | feat | M | #23 | open |
| 4 | #26 | feat(base): guard — валидатор return_format на SubagentStop (§6; связан с #20) | feat | M | #23 | open |
| 5 | #27 | feat: guard-события в zprof score (P7) и zprof stats (§7) | feat | M | #23 | open |
| 6 | #28 | feat(cli): zprof apply деплоит guard — guard.json, хуки с matcher, permissions.deny (§8; supersedes #18) | feat | L | #23 | open |
| 7 | #29 | feat(cli): zprof doctor — проверки guard (§10) | feat | S | #28 | open |
| 8 | #30 | feat(base): guard — строка доктрины, контракты pr-shepherd/task-runner, overlay guard.yaml (§9) | feat | S | #28 | open |
| 9 | #31 | test(base): shakedown — ноль ложных deny guard на штатном маршруте (§1 г, §11) | test | S | #24–#30 | open |

Порядок: #23 → {#24, #25, #26, #27, #28} параллельно → {#29, #30} → #31.
#28 зависит только от формы guard.yaml из #23, поэтому идёт в первой параллельной волне, а не после #24–#27.
Файловые пересечения: `profiles/base/zprof-guard.py` (#23–#27; тесты #24–#26 — отдельными файлами test_guard_*.py);
`pr-shepherd.md`/`task-runner.md` (#30 и #15, #16, #19–#21 из plan-1); `zprof-collect.py` (#27 и #20, #22).
Открытый вопрос владельцу до #31: pr-shepherd в `readonly_roles` vs `git checkout`/`git commit` в его §4–§5 (см. #23).
