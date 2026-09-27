# Plan 1: panel-2026-09-27
created: 2026-09-27
source: docs/reviews/2026-09-27-ai4sdlc-panel.md, «План действий», строки 1–10 (кроме 4 и 11+; 1 и 4 слиты решением владельца)
milestone: panel-2026-09-27

Решения владельца (Alex, 2026-09-27): auto-merge везде, `merge_policy` нет; `--delete-branch` штатно,
стоп-лист уточняется до «несмерженных веток»; защита контрактов — только required CI.

| # | Issue | Title | Type | Complexity | Depends | Status |
|---|-------|-------|------|------------|---------|--------|
| 1 | #14 | ci: pytest, gofmt -l и ruff в CI; отформатировать 20 файлов cli/ (строка 2) | ci | M | — | open |
| 2 | #17 | ci: ruleset на main — required check test (CI), без bypass (строки 1+4, **owner**, не task-runner) | ci | S | #14 | open |
| 3 | #15 | fix(base): стоп-лист — «удаление несмерженных веток и тегов» (строка 3) | fix | S | — | open |
| 4 | #16 | feat(base): pr-shepherd в local-green сам гоняет тесты из ## Executing + детекция rulesets (строка 5) | feat | M | #15 (тот же файл) | open |
| 5 | #18 | feat(cli): zprof apply пишет permissions.deny (строка 6) | feat | M | — | open |
| 6 | #19 | feat(base): глобальный max_dispatches task-runner'а (строка 7) | feat | M | — | open |
| 7 | #20 | feat(base): реестр verdicts.yaml, маппинг, doctor, аудиторы на verdict: (строка 8) | feat | L | #19 | open |
| 8 | #21 | feat(base): один источник маршрутов, implementer в багфиксе, условные ссылки (строка 9) | feat | M | #20, #19 | open |
| 9 | #22 | feat(base): коллектор пишет config_hash и verdict; переразвернуть в zprof (строка 10) | feat | M | — | open |

Параллельно после #14: {#15→#16}, #18, {#19→#20→#21}, #22. #17 — Alex руками после merge #14.
Файловые пересечения: pr-shepherd.md (#15, #16, #21), task-runner.md (#19, #20, #21), zprof-collect.py (#20, #22).
