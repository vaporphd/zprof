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
<!-- zprof:end consilium -->

<!-- zprof:begin stop-list -->
## Stop list

Необратимые действия — task-runner не делает и не поручает:

- force-push, rebase или amend опубликованной ветки
- удаление веток и тегов
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
