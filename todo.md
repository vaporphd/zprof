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
- [ ] ci: pytest, gofmt -l и ruff в CI; отформатировать 20 файлов cli/ (#14)
- [ ] ci: ruleset на main — required check test (CI), без bypass (#17) — owner, depends: #14
- [ ] fix(base): стоп-лист — «удаление несмерженных веток и тегов», merged-ветка штатно (#15)
- [ ] feat(base): pr-shepherd в local-green сам гоняет тесты из ## Executing (#16) — после #15
- [ ] feat(cli): zprof apply пишет permissions.deny (force, --admin, --no-verify, curl|sh) (#18)
- [ ] feat(base): глобальный max_dispatches task-runner'а вне секции аудита (#19)
- [ ] feat(base): реестр verdicts.yaml, маппинг в task-runner, проверка в doctor (#20) — depends: #19
- [ ] feat(base): один источник маршрутов, implementer в багфиксе, условные spec-maintainer/integration-gate (#21) — depends: #20
- [ ] feat(base): коллектор пишет config_hash и verdict; переразвернуть collector и score-hook в zprof (#22)
