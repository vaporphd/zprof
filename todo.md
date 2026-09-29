# TODO

## Milestones
- [x] Per-task scorecard — фаза 1 (#11, PR 10): коллектор v2, пакет score, zprof score, Stop-хук, AGENT_LOOP
- [ ] Per-task scorecard — фаза 2: карточка «Runs» в zprof stats, сигнал в eval-telemetry, $ на карточке, --include-legacy
- [ ] Per-task scorecard — фаза 3: гейт в pr-shepherd после калибровки порогов

## Current
- [x] expert-panel: base tool agent для повторного экспертного разбора (#12, PR: см. ниже)
- [ ] После merge PR 10: zprof sync в jarvis-in-hermes и apple-health-sync, первый живой прогон zprof score

## Backlog
- [ ] zprof сам не ест свой корм: нет .zprof.yaml и корневого lessons.md (уроки в tasks/lessons.md) — выровнять раскладку

## Plan 1: panel-2026-09-27
- [x] fix(base): коллектор не сшивает async Agent-dispatch с task-notification без <tool-use-id> (#34) — вне очереди, блокирует карточки
- [x] ci: pytest, gofmt -l и ruff в CI; отформатировать 20 файлов cli/ (#14, PR 32)
- [ ] ci: ruleset на main — required check test (CI), без bypass (#17) — owner, depends: #14
- [x] fix(base): стоп-лист — «удаление несмерженных веток и тегов», merged-ветка штатно (#15)
- [x] feat(base): pr-shepherd в local-green сам гоняет тесты из ## Executing (#16) — после #15
- [x] feat(cli): zprof apply пишет permissions.deny (force, --admin, --no-verify, curl|sh) (#18) — superseded by #28 (guard)
- [x] feat(base): глобальный max_dispatches task-runner'а вне секции аудита (#19)
- [x] feat(base): реестр verdicts.yaml, маппинг в task-runner, проверка в doctor (#20) — depends: #19 — PR #41
- [x] feat(base): один источник маршрутов, implementer в багфиксе, условные spec-maintainer/integration-gate (#21) — depends: #20
- [x] feat(base): коллектор пишет config_hash и verdict; переразвернуть collector и score-hook в zprof (#22)

- [x] fix(base): loss-счётчик #34 дублируется до дедупа; диагностика нерезолвленных task-notification (#36) — follow-up к #34
- [x] chore: переразвернуть .claude/zprof-collect.py после #36 (#45)
- [x] fix(overlays): issue-loop-github-strict/pr-shepherd — старый human-gated контракт противоречит auto-merge (#37) — после #15
- [x] fix(base): коллектор не сшивает вложенные async-диспатчи task-runner с task-notification — P6/P7 завышены (#52) — после #28, приоритет — PR #61
- [x] fix: task-runner ждёт async-детей через sleep 180; P2 не считает sleep слепым повтором (#53) — после #52
- [x] fix(base): north-star-auditor при отсутствии NORTH_STAR.md — детерминированный skip, гейт условный (#60) — PR #71
- [x] fix(base): раннер/pr-shepherd оставляют worktree main в scratchpad; checkout не на main после run (#62)
- [x] fix(cli): zprof score — RE2 молча выбрасывает lookahead-паттерны из mutating_bash_patterns (после #75) (#79) — приоритет, измерения — PR #80

## Plan 2: guard
- [x] fix(ci): ruff RUF059 ломает CI на main; запинить версию ruff (#56) — вне очереди, блокирует #28
- [x] feat(base): zprof-guard.py — каркас, стоп-лист без контекста, read-only роли (#23)
- [x] feat(base): guard — контекстные правила (head_on_remote, linked_worktree, запись вне репо) (#24) — depends: #23
- [x] feat(base): guard — merge-гейт и PR-гейт (Closes #N + ## Gate) (#25) — depends: #23
- [x] feat(base): guard — валидатор return_format на SubagentStop (#26) — depends: #23
- [x] feat: guard-события в zprof score (P7) и zprof stats (#27) — depends: #23 — PR #55
- [x] feat(cli): zprof apply деплоит guard — guard.json, хуки с matcher, permissions.deny (#28) — depends: #23; supersedes #18
- [x] feat(cli): zprof doctor — проверки guard (хуки, guard.json, deny, роли) (#29) — depends: #28 — PR #63
- [x] feat(base): guard — строка доктрины, контракты pr-shepherd/task-runner, overlay guard.yaml (#30) — depends: #28 — PR #65
- [x] fix(cli): zprof doctor без .zprof.yaml — режим telemetry-only (#64) — перед #31
- [x] test(base): shakedown — ноль ложных deny guard на штатном маршруте (#31) — depends: #24, #25, #26, #27, #28, #29, #30
- [x] fix(base): guard — target ещё может утечь секрет через env/export/quoted value (#47) — follow-up к #23 — PR #72
- [x] fix(base): guard — readonly_mutation не учитывает allow_write_prefixes для мутирующих Bash (#67) — follow-up к #31 — PR #74
- [x] chore(base): деплой-копия guard в zprof — трекать .claude/zprof-guard.py, игнорировать guard.json (#59) — follow-up к #28
- [x] fix(base): guard — readonly_mutation не видит standalone `ln` (symlink/hardlink bypass) (#73) — follow-up к #67 — PR #78
- [x] fix(base): guard — readonly_mutation ложный deny на read-only команде reviewer (run #59) (#75) — follow-up к #67 — PR N
