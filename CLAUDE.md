<!-- zprof:begin doctrine -->
## Doctrine (не изменять — управляется zprof)

Этот проект использует zprof — слоеная система agent-loop.

- `AGENT_LOOP.md` — контракт диспатча. Читать при старте каждой loop-итерации.
- `.claude/agents/*.md` — доступные subagent'ы, каждый с return_format schema.
- `followup.md` — living snapshot (Status + Next, ≤20 строк).
- `lessons.md` — corrections ledger (~15 записей, overflow → lessons-archive.md).
- `todo.md` — canonical task list.
- `.zprof.yaml` — какие overlays применены + model overrides.

### Граница

Всё, что меняет репозиторий, исполняет `task-runner` — main спавнит его на
одну задачу и получает одну схему. Чтение, объяснения, обсуждение и
git-операции main делает сам. Правило: **мутация — раннеру, чтение — сам.**

### Изоляция
Main-сессия НИКОГДА не цитирует output subagent'а. После каждого dispatch:
запиши ≤3 строки в followup.md, дропни ответ subagent'а из рабочей памяти.
Прогресс задачи живёт в `.zprof/runs/<id>.md` — читай хвост по запросу, не
весь файл.

### Свои правила
Пиши ниже managed-блока — этот раздел не трогается при `zprof sync`.
<!-- zprof:end doctrine -->

<!-- zprof:begin consilium -->
## Consilium

| Роль | Агент | Модель | Специализация |
|------|-------|--------|---------------|
| task-runner | task-runner | sonnet | петля agent-loop, роутинг, журнал |
| planner | planner | sonnet | декомпозиция задачи → todo.md + plan-N.md |
| architect | architect | opus | дизайн, ADR, интерфейсы |
| implementer | implementer | sonnet | Go CLI + Python collector код |
| tester | tester | sonnet | go test + pytest, write missing tests |
| reviewer | reviewer | opus | code review, conventions check |
| bug-hunter | bug-hunter | opus | diagnosis, repro, root cause |
| refactor-agent | refactor-agent | sonnet | restructure without behavior change |
| explorer | explorer | sonnet | read-only codebase investigation |
| docs-writer | docs-writer | sonnet | README, CLAUDE.md sections, wiki |
| frontend-developer | frontend-developer | sonnet | UI components, pages; delegates to frontend-design skill |
| pr-shepherd | pr-shepherd | sonnet | pre-flight, delivery verification, post-merge stamp |
| groomer | groomer | opus | spec/prompt → GitHub issues + plan-N.md + todo.md |
| wiki-keeper | wiki-keeper | sonnet | docs/wiki/ per-component docs + INDEX.md |
| auditor | auditor | sonnet | read-only step verification (mechanical) |
| auditor-deep | auditor-deep | opus | read-only step verification (semantic) |

### Gates (--with-gates)

| Роль | Агент | Модель | Точка |
|------|-------|--------|-------|
| north-star-auditor | gates/north-star-auditor | opus | до первого агента маршрута |
| plan-reviewer | gates/plan-reviewer | sonnet | после planner DRAFT |
| evidence-auditor | gates/evidence-auditor | opus | перед report-writer |

### Tool agents

| Агент | Назначение |
|-------|-----------|
| evaluator | shakedown eval scoring |
| evaluator-telemetry | telemetry-driven contract diffs |
| expert-panel | AI4SDLC/AI4PDLC панель: 6 read-only линз + сводный разбор в docs/reviews |
<!-- zprof:end consilium -->

<!-- zprof:begin stop-list -->
## Stop list

Необратимые действия — task-runner не делает и не поручает:

- force-push, rebase или amend опубликованной ветки
- удаление несмерженных веток и тегов (удаление merged-ветки после squash-merge — штатное действие pr-shepherd)
- запись за пределы репозитория: БД, продовые конфиги, секреты
- релиз, деплой, публикация пакета
- слом публичного API, у которого есть внешние потребители
- новая платная зависимость или новый внешний сервис
- любая отправка содержимого репозитория наружу
<!-- zprof:end stop-list -->

<!-- zprof:begin executing -->
## Executing

### Стек

- **Go CLI** (`cli/`): Go 1.22+, cobra, 15 internal packages.
  Build: `cd cli && go build ./...`. Test: `cd cli && go test ./...`
- **Python collector** (`profiles/base/`): Python 3.10+, **stdlib only**.
  Test: `python3 -m pytest profiles/base/tests/ -v`
- **Prompt layer** (`profiles/`): agent .md с YAML frontmatter

### Конвенции

- Коммиты: `feat(cli):`, `feat(base):`, `fix(stats):`, `docs:`, `test(cli):`
- Go: `internal/` packages, table-driven tests, testify/require, error wrapping
- Python: stdlib only, `exit(0)` always, `fcntl.flock`, `os.fsync()`
- Промпты: русский текст, английские ключи/имена
- Agent files: 200–400 lines executors, 500–800 orchestrators
<!-- zprof:end executing -->

### Стек — доп. команды вне managed-блока (ADR 0002)

`## Executing` выше — managed-блок (`buildExecutingTable`); pr-shepherd не
считает его источником тест-команд, T1-сканер ищет метки `` Test: `<cmd>` ``
по всему файлу независимо от секции (ADR 0002). Метки, которых ещё нет в
managed-блоке, — здесь:

- Python collector (`profiles/base/`): Test: `ruff check profiles/base/`
  (lint, дополняет `Test:`-строку с pytest в managed-блоке выше) — версия
  зафиксирована в `ruff.toml` (`required-version`) и
  `.github/workflows/ci.yml` (`pip install ... ruff==0.16.9`), должны
  совпадать.

## Интеграция ветки

Готовая ветка попадает в `main` **только через PR**, который ведёт `pr-shepherd`:
`git push -u origin <branch>` → `gh pr create` → dispatch `pr-shepherd`, который проверяет
pre-flight и доставку и **сам мержит** (человек решил, когда запустил loop — повторно не
спрашивать). Main-сессия не мержит в `main` сама и не предлагает «merge locally»,
даже если внешний skill (finishing-a-development-branch, SDD) показывает такое меню —
инструкции этого файла его перебивают. Это же правило действует во всех проектах
под zprof (урок: `tasks/lessons.md`, 2026-09-27).
