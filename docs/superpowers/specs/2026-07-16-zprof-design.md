# zprof — слоеная система профилей Claude Code

**Статус:** draft v1
**Дата:** 2026-07-16
**Автор:** Alex + brainstorming session

## 1. Цель

Портируемая, git-хранимая система «профилей» для Claude Code, повторяющая agent-loop паттерн `hft_moex` (прозаический AGENT_LOOP.md + узкие `.claude/agents/*.md` с tool-whitelist), но:

1. Композируется под конкретный стек — семь доменных overlays в v1 (ios-swift, android-kotlin, backend-python, frontend-web, re-macho, systems-cpp, systems-rust)
2. Не спамит main-сессию токенами subagent'ов (четырёхуровневая изоляция)
3. Осмысленно распределяет модели по агентам (opus/sonnet/haiku), overlay умеет override'ить
4. Активируется одной командой CLI-визарда (`zprof init`), обновляется без потери кастомного контента
5. Multi-stack — можно активировать несколько overlays одновременно

## 2. Не-цели (out of v1)

- Экспорт в Cursor/Windsurf/Codex/OpenCode (harnest это делает; в v2)
- Кастомные slash-команды per-overlay
- Git-hooks / build-recipes / MCP-configs генерация
- `re-android`, `backend-go` overlays (v2 backlog)
- Плюрализм источников профилей (`zprof repo add <url>`) — v2

## 3. Архитектура

Три слоя:

```
┌─────────────────────────────────────────────────────────┐
│  zprof (Go CLI, brew tap alcherk/tap/zprof)             │
│  — wizard, apply, sync, model resolver                  │
└────────────────────────┬────────────────────────────────┘
                         ↓ читает / клонирует
┌─────────────────────────────────────────────────────────┐
│  ~/.zprof/repo/  (клон alcherk/zprof-profiles, семвер)  │
│    base/         универсальные workflow-роли,           │
│                  loop templates, gates                  │
│    overlays/                                            │
│      ios-swift/                                         │
│      android-kotlin/                                    │
│      backend-python/                                    │
│      frontend-web/                                      │
│      re-macho/                                          │
│      systems-cpp/                                       │
│      systems-rust/                                      │
└────────────────────────┬────────────────────────────────┘
                         ↓ zprof apply
┌─────────────────────────────────────────────────────────┐
│  <project>/                                             │
│    .claude/                                             │
│      agents/          (namespace-ed при composition)    │
│      commands/        (v2, пусто в v1)                  │
│      settings.local.json                                │
│    CLAUDE.md          (managed blocks + custom regions) │
│    AGENT_LOOP.md      (dispatch contract)               │
│    todo.md, lessons.md, followup.md                     │
│    docs/PROJECT_SPEC.md, docs/adr/                      │
│    .zprof.yaml        (per-project override manifest)   │
└─────────────────────────────────────────────────────────┘
```

Основной принцип: **overlay полностью описывает свой pipeline через один AGENT_LOOP-контракт + узкие subagent'ы**. Никаких хуков Claude Code, никакой магии — прозрачный markdown, как в hft_moex.

## 4. `base/` — тонкая база

Базовый паkage содержит только те роли, которые **не зависят от стека**, и переиспользуемые скелеты loop'ов.

### 4.1 Роли

| Имя агента | Назначение | Модель |
|---|---|---|
| `planner` | Декомпозиция задачи → `todo.md` + `plan-<N>.md` | sonnet |
| `docs-writer` | README, CLAUDE.md sections, wiki curation | sonnet |
| `dev-orchestrator` | Внутренний диспатч full dev-pipeline (Task-in-Task) | sonnet |
| `exploratory-orchestrator` | Внутренний диспатч RE/analysis pipeline | sonnet |

### 4.2 Опциональные gates (флаг `--with-gates`)

| Имя | Назначение | Модель |
|---|---|---|
| `north-star-auditor` | Проверка задачи против `docs/NORTH_STAR.md` doctrine | opus |
| `evidence-auditor` | Валидация количественных выводов до записи | opus |
| `plan-reviewer` | Approve planner drafts до создания issues/plan-*.md | sonnet |

### 4.3 Loop templates (`base/loop-templates/`)

- `dev-pipeline.md` — planner → architect → implementer → tester → bug-hunter/reviewer → docs
- `exploratory.md` — intake → unpack → explore → hypothesize → verify → report

Overlay ссылается на один из них в своём `loop.md`.

## 5. `overlays/<domain>/` — жирный overlay

Каждый overlay — самостоятельная папка со всей stack-специфичной начинкой.

### 5.1 Обязательная структура

```
overlays/<domain>/
  manifest.yaml           # версия, deps, human name
  detect.yaml             # правила автодетекта стека
  claude-block.md         # YAML-фрагмент для CLAUDE.md
  loop.md                 # AGENT_LOOP-контракт для этого overlay
  agents/                 # domain-aware агенты + tool-агенты
    architect.md
    implementer.md
    tester.md
    bug-hunter.md
    refactor-agent.md
    explorer.md
    <tool-agents>.md      # xcode-runner, cargo-driver, ...
  state/                  # доменные добавки к state-файлам
    project-spec-section.md    # секция для docs/PROJECT_SPEC.md
    adr-template.md            # опционально overlay-специфичный
```

### 5.2 Формат `detect.yaml`

```yaml
name: ios-swift
detect:
  any_file:
    - "*.xcodeproj"
    - "*.xcworkspace"
    - "Package.swift"
    - "project.yml"           # xcodegen
  any_regex:
    - path: "Package.swift"
      match: "swift-tools-version"
  confidence: high | medium | low
```

CLI `zprof init` сканирует и предлагает overlays по confidence.

### 5.3 Формат `manifest.yaml`

```yaml
name: ios-swift
display_name: "iOS / Swift (UIKit + SwiftUI + AppKit)"
version: 0.1.0
loop_template: dev-pipeline   # или exploratory
requires_base: ">= 0.1.0"
roles:
  - architect
  - implementer
  - tester
  - bug-hunter
  - refactor-agent
  - explorer
tool_agents:
  - xcode-runner
  - spm-manager
  - simulator-driver
  - testflight-shipper
```

### 5.4 Стандартный набор ролей overlay

Все overlays поставляют одни и те же **6 stack-aware ролей**:

- `architect` — знает Xcode targets / gradle modules / cargo workspace / FastAPI routes
- `implementer` — пишет код по спеке в идиоматике стека
- `tester` — пишет тесты (XCTest / JUnit / pytest / Vitest / cargo test)
- `bug-hunter` — reproducer → failing test → fix, знает отладчик стека
- `refactor-agent` — semantics-preserving трансформации со знанием ownership/типов
- `explorer` — read-only investigation с корректным пониманием layout стека

Плюс **tool-агенты** (по overlay):

- **ios-swift:** `xcode-runner`, `spm-manager`, `simulator-driver`, `testflight-shipper`, `xcodegen-driver`, `swiftlint-checker`
- **android-kotlin:** `gradle-driver`, `adb-runner`, `compose-designer`, `apk-signer`, `ktlint-checker`
- **backend-python:** `fastapi-designer`, `pydantic-modeler`, `alembic-migrator`, `pytest-runner`, `docker-composer`, `pyright-checker`, `ruff-linter`
- **frontend-web:** `component-designer`, `api-client-generator`, `vite-optimizer`, `playwright-runner`, `storybook-driver`, `lighthouse-checker`
- **re-macho:** `intake-agent`, `macho-unpacker`, `dedupfind-driver`, `atos-runner`, `swift-demangler`, `hypothesizer`, `verifier`, `report-writer`
- **systems-cpp:** `cmake-driver`, `sanitizer-runner`, `valgrind-runner`, `gdb-debugger`, `clang-format-checker`
- **systems-rust:** `cargo-driver`, `clippy-checker`, `miri-runner`, `criterion-benchmarker`, `unsafe-auditor`, `rustsec-checker`

## 6. Изоляция agent-loop от main-сессии (4 уровня)

Проблема: каждый `Task()` возвращает финальное сообщение subagent'а в main context. Наивный pipeline с 5-7 агентами × 3-5k токенов = **15-35k токенов мусора** на итерацию. После 3-4 итераций main перегружен, компакция ломает continuity.

Четыре уровня применяются вместе:

### T1 — Terse handoff protocol

Каждый агент в base+overlay имеет в frontmatter обязательный `return_format`:

```yaml
---
name: implementer-ios
description: ...
tools: Read, Write, Edit, Bash, Glob, Grep
model: sonnet
return_format: |
  verdict: done | blocked | failed
  artifact: <path к файлу с полным output>
  next: <имя следующего агента или null>
  one_line: <≤120 символов, executive summary>
---
```

Тело промпта агента заканчивается жёстким правилом:

> Никогда не выводи analysis/rationale в финальном сообщении. Полный вывод — только в `artifact`. Финальное сообщение = ТОЛЬКО схема выше.

Main получает ~150 токенов на dispatch вместо 3000+.

### T2 — Artifact-first storage

Всё содержательное живёт в файлах:

- `plan-<N>.md` — planner drafts
- `docs/adr/NNNN-<slug>.md` — architect decisions
- `reports/YYYY-MM-DD-<slug>.md` — RE отчёты
- `todo.md` — canonical task list
- `followup.md` — snapshot Status+Next (≤20 строк живёт всегда)
- `lessons.md` — corrections ledger (~15 записей cap, overflow → `lessons-archive.md`)

Main перечитывает **только** `followup.md` + top-3 записи `lessons.md` в начале каждой loop-итерации. Планы/ADR/report'ы читаются точечно, когда main решает что делать дальше.

### T3 — Orchestrator agents

Base поставляет двух orchestrator'ов:

- **`dev-orchestrator`** — внутри Task-in-Task последовательно вызывает `planner → architect → implementer → tester → reviewer`, аккумулирует их schema-outputs в один финальный отчёт (verdict + PR link + list of artifacts). Main видит **один** dispatch на весь pipeline вместо пяти.
- **`exploratory-orchestrator`** — аналогично для RE: `intake → unpack → explore → hypothesize → verify → report`, возвращает финальный путь к `reports/*.md`.

Trade-off: orchestrator сам жрёт токены (внутри своего context'а). Плюс: main-context спасается. Минус: гейтинг посреди pipeline становится сложнее. Для критичных gate-точек overlay может явно предписать «main dispatch'ит напрямую, минуя orchestrator» — записано в `loop.md`.

### T4 — Workflow tool для fan-out

Для параллельных сценариев с ≥5 агентами (RE multi-hypothesis, multi-file review, migration sweep) overlay `loop.md` предписывает main дёрнуть `Workflow(...)` вместо parallel Task-fan-out.

Workflow исполняется в фоне, main получает одно финальное агрегированное сообщение. Дополнительно: workflow может использовать `isolation: "worktree"` для параллельных implementer'ов без file-конфликтов.

### Guardrails в AGENT_LOOP.md

Три обязательных правила для main:

1. **Не цитировать output subagent'а.** Main пересказывает только return schema (verdict + one_line + artifact path). Никогда не вставляет фрагменты из `artifact` в conversation.
2. **≤3 строки в followup.md после каждого dispatch.** Main записывает Status update, дропает ответ subagent'а из рабочей памяти.
3. **Vocal self-check перед dispatch.** Main произносит `буду читать поле <X> из результата`, чтобы не расширить область на весь output.

## 7. Политика моделей (tier alias)

### 7.1 Хранение в frontmatter

Overlay пишет tier alias, не exact ID:

```yaml
model: opus     # → CLI резолвит в актуальный ID
model: sonnet
model: haiku
```

Резолвер живёт в CLI (`internal/models/registry.go`), обновляется с каждым релизом `zprof`. Overlays не стареют при выходе новых моделей.

### 7.2 Актуальная таблица резолвера (обновляется в CLI)

| Alias | Exact ID (на 2026-07-16) |
|---|---|
| `opus` | `claude-opus-4-8` |
| `opus-1m` | `claude-opus-4-7[1m]` |
| `sonnet` | `claude-sonnet-5` |
| `haiku` | `claude-haiku-4-5-20251001` |
| `fable` | `claude-fable-5` |

### 7.3 Дефолтное распределение по ролям

**Opus** — тяжёлое рассуждение, cross-file, ownership:
- `architect`, `reviewer`, `bug-hunter`, `refactor-agent`, `north-star-auditor`, `evidence-auditor`, `hypothesizer` (RE), `verifier` (RE)

**Sonnet** — рабочая лошадь для well-scoped задач:
- `planner`, `implementer`, `tester`, `explorer`, `docs-writer`, `plan-reviewer`, `dev-orchestrator`, `exploratory-orchestrator`

**Haiku** — mechanical, tool-heavy:
- `intake-agent`, `macho-unpacker`, `atos-runner`, `swift-demangler`, `report-writer`, `cold-look-auditor` (когда добавим)

### 7.4 Override

**Per-project через `.zprof.yaml`:**

```yaml
# .zprof.yaml
model_overrides:
  architect: opus-1m         # 1M context вариант для больших кодовых баз
  implementer: opus          # ambitious project
  intake-agent: claude-haiku-4-5-20251001  # exact ID тоже принимается
```

**Императивно через CLI:**

```
zprof agents set architect --model opus-1m
zprof agents set implementer-py --model sonnet
```

## 8. `zprof` CLI (Go)

Стек: Go 1.22+, `spf13/cobra` + `spf13/viper`, `charmbracelet/huh` (TUI wizard), `hashicorp/go-getter` (git ops).

### 8.1 Команды v1

```
zprof init                            # интерактивный wizard: detect + предложить overlays + confirm
zprof apply <overlay> [<overlay>...]  # композиция; создаёт/обновляет .claude/ + state-файлы
zprof list                            # что установлено в проекте (overlays, роли, модели)
zprof agents set <role> <agent>       # свап (harnest DNA); поддерживает namespace
zprof agents set <role> --model <m>   # override модели для роли
zprof sync                            # git pull ~/.zprof/repo + regen managed blocks (custom текст сохраняется)
zprof update                          # обновить сам CLI (self-update через brew)
zprof doctor                          # диагностика: правильно ли .claude/ настроен, все ли агенты валидны
```

### 8.2 Флаги

- `--with-gates` — включить `north-star-auditor` / `evidence-auditor` / `plan-reviewer`
- `--minimal` — не создавать `docs/PROJECT_SPEC.md` / `docs/adr/` (для маленьких проектов)
- `--lang=ru|en` — язык генерируемых prompts/docs (default: `ru`)
- `--dry-run` — показать план изменений, не писать файлы

### 8.3 Managed blocks в файлах

Каждый файл, который CLI перегенерирует (`CLAUDE.md`, `AGENT_LOOP.md`, `.gitignore`, `.zprof.yaml`) содержит помеченные регионы:

```markdown
<!-- zprof:begin overlay=ios-swift block=stack-config -->
...сгенерированный контент...
<!-- zprof:end overlay=ios-swift block=stack-config -->
```

`zprof sync` перегенерирует **только** содержимое между маркерами. Всё вне маркеров считается пользовательским и не трогается. При конфликте (пользователь редактировал managed block вручную) — CLI сохраняет `.bak` и предлагает три режима: `overwrite`, `preserve`, `merge` (интерактивно).

## 9. Multi-stack композиция

`zprof apply backend-python frontend-web` (пример `sm_monitor`):

### 9.1 Namespace агентов

Overlay-специфичные роли namespace-ятся суффиксом:
- `architect-py`, `architect-web`
- `implementer-py`, `implementer-web`
- `tester-py`, `tester-web`
- ...

Base роли (`planner`, `docs-writer`) — общие, без суффиксов.

### 9.2 AGENT_LOOP с несколькими entry-points

```markdown
## Entry-points

### `«backend task»` / `«fix py»` / trigger-phrase python
→ dispatch `dev-orchestrator` (overlay=py)
   pipeline: planner → architect-py → implementer-py → tester-py → reviewer-py

### `«frontend task»` / `«fix ui»` / trigger-phrase web
→ dispatch `dev-orchestrator` (overlay=web)
   pipeline: planner → architect-web → implementer-web → tester-web → reviewer-web

### `«fullstack task»`
→ dispatch `planner` (multi-target mode) → planner декомпозирует на py-subtasks + web-subtasks,
  далее два независимых pipeline'а через orchestrator'ов
```

### 9.3 CLAUDE.md блоки

Overlay-specific YAML-блоки объединяются под namespace-ключами:

```yaml
<!-- zprof:begin overlay=backend-python block=stack-config -->
stack:
  backend:
    lang: python
    version: "3.12"
    build_cmd: "make build-backend"
    test_cmd: "pytest -q"
    lint_cmd: "ruff check ."
<!-- zprof:end -->

<!-- zprof:begin overlay=frontend-web block=stack-config -->
stack:
  frontend:
    lang: typescript
    framework: nextjs        # автодетект
    build_cmd: "pnpm build"
    test_cmd: "pnpm test"
    lint_cmd: "pnpm lint"
<!-- zprof:end -->
```

### 9.4 Ограничения

- В v1 поддерживаем до **3 overlays** одновременно. `zprof doctor` warn'ит при 2+, error при 4+.
- Порядок аргументов в `zprof apply <a> <b> <c>` = priority: первый выигрывает при конфликтах имён (tool-агенты, CLAUDE.md ключи, trigger-фразы). Записывается в `.zprof.yaml` как список `overlays: [...]` в исходном порядке.
- Все overlays равны по dispatch-семантике (нет «primary» с особыми правами); priority работает только как tie-breaker.

## 10. State-файлы

`zprof apply` создаёт (если не существуют):

| Файл | Назначение | `--minimal`? |
|---|---|---|
| `todo.md` (корень) | Canonical task list | всегда |
| `lessons.md` (корень) | Corrections ledger, ~15 записей cap | всегда |
| `followup.md` (корень) | Living snapshot: Status + Next (≤20 строк) | всегда |
| `docs/PROJECT_SPEC.md` | Скелет + overlay-секции | skip если `--minimal` |
| `docs/adr/0000-template.md` | Шаблон ADR | skip если `--minimal` |
| `.gitignore` (добавить `thoughts/`) | Согласовать с глобальной конвенцией Alex | всегда |
| `thoughts/.gitkeep` | Убедиться что папка существует | всегда |
| `.zprof.yaml` | Manifest applied overlays + overrides | всегда |

Формат `followup.md`:

```markdown
# Followup

## Status
<≤10 строк — где сейчас находимся, что последнее сделано, актуальные PR/issues>

## Next
<≤10 строк — что делаем следующим шагом, кто dispatch, чего ждём>
```

## 11. Язык

Все agent prompts, AGENT_LOOP.md, CLAUDE.md шаблоны, README overlays — **русский**.

Остаются в английском:
- Имена агентов и ролей (`planner`, `architect-py`, `hypothesizer`)
- YAML-ключи в frontmatter и manifests
- Trigger-фразы (`take next task`, `drain the queue`) — плюс русские алиасы (`следующая задача`, `дальше`)
- Идентификаторы моделей и exact IDs

Обоснование: LLM устойчиво следует русским prompts на моделях Opus 4.x / Sonnet 5 (проверено на openclaw и agent-team проектах Alex'а); удобство чтения для основного пользователя перевешивает marginal quality diff.

## 12. RE overlay — exploratory loop

`re-macho/loop.md` использует `exploratory-pipeline` template:

```
intake (crash / dSYM / binary) 
  → macho-unpacker
  → explorer (Mach-O layout, load commands, sections)
  → hypothesizer (multiple parallel hypotheses via T4 Workflow)
  → verifier (per-hypothesis via dedupfind + atos)
  → report-writer
  → OUTPUT: reports/YYYY-MM-DD-<slug>.md
```

Особенности:
- **Нет PR-выхода** — результат работы pipeline'а это markdown-отчёт, не merged branch
- **Параллельные гипотезы через Workflow** — hypothesizer возвращает N кандидатов, verifier проверяет каждого параллельно через T4
- **Артефакты в `reports/`, не в `docs/adr/`** — RE отчёты не архитектурные решения
- **Trigger-фразы:** `analyze <binary>`, `symbolicate <crash>`, `explain <address>`, русские алиасы `разбери <файл>`, `символизируй <краш>`

`re-android` (v2 backlog) использует ту же exploratory форму, но с jadx/Fernflower вместо atos/dedupfind.

## 13. GitHub / distribution

Два репо:

**`alcherk/zprof`** — Go CLI
- Cобирается через `goreleaser`
- Brew tap: `alcherk/tap/zprof` → `brew install alcherk/tap/zprof`
- GitHub Releases с multi-arch бинарями (darwin-arm64/amd64, linux-arm64/amd64)
- Self-update: `zprof update` → `brew upgrade`

**`alcherk/zprof-profiles`** — контент (base/ + overlays/)
- Семантические теги: `v0.1.0`, `v0.2.0`, ...
- CLI по умолчанию тянет `main` (rolling), можно закрепить: `zprof sync --pin v0.1.0`
- Локальный клон в `~/.zprof/repo/`
- Каждый overlay имеет свой `manifest.yaml` с version — позволяет частичный rollback

**v2:** `zprof repo add <url>` — форкать/добавлять сторонние источники профилей. Слияние с приоритетом (base priority > user overrides).

## 14. Директорная раскладка репо `zprof-profiles`

```
zprof-profiles/
├── README.md
├── VERSION
├── base/
│   ├── manifest.yaml
│   ├── agents/
│   │   ├── planner.md
│   │   ├── docs-writer.md
│   │   ├── dev-orchestrator.md
│   │   ├── exploratory-orchestrator.md
│   │   └── gates/
│   │       ├── north-star-auditor.md
│   │       ├── evidence-auditor.md
│   │       └── plan-reviewer.md
│   ├── loop-templates/
│   │   ├── dev-pipeline.md
│   │   └── exploratory.md
│   ├── claude-block-base.md
│   └── state-templates/
│       ├── todo.md
│       ├── lessons.md
│       ├── followup.md
│       ├── project-spec-skeleton.md
│       └── adr-template.md
├── overlays/
│   ├── ios-swift/
│   │   ├── manifest.yaml
│   │   ├── detect.yaml
│   │   ├── claude-block.md
│   │   ├── loop.md
│   │   ├── agents/
│   │   │   ├── architect.md
│   │   │   ├── implementer.md
│   │   │   ├── tester.md
│   │   │   ├── bug-hunter.md
│   │   │   ├── refactor-agent.md
│   │   │   ├── explorer.md
│   │   │   ├── xcode-runner.md
│   │   │   ├── spm-manager.md
│   │   │   ├── simulator-driver.md
│   │   │   ├── testflight-shipper.md
│   │   │   ├── xcodegen-driver.md
│   │   │   └── swiftlint-checker.md
│   │   └── state/
│   │       └── project-spec-section.md
│   ├── android-kotlin/…
│   ├── backend-python/…
│   ├── frontend-web/…
│   ├── re-macho/…
│   ├── systems-cpp/…
│   └── systems-rust/…
└── docs/
    ├── ARCHITECTURE.md
    ├── AUTHORING-AN-OVERLAY.md
    └── ISOLATION.md
```

## 15. Директорная раскладка CLI `zprof`

```
zprof/
├── cmd/
│   └── zprof/main.go
├── internal/
│   ├── wizard/           # интерактивный init
│   ├── detect/           # чтение detect.yaml, скан проекта
│   ├── apply/            # composition, managed-block rendering
│   ├── sync/             # git ops + merge modes
│   ├── models/           # tier alias resolver
│   ├── manifest/         # чтение/запись .zprof.yaml
│   └── doctor/           # диагностика
├── go.mod
├── goreleaser.yaml
├── Formula/              # brew tap
└── README.md
```

## 16. Открытые риски и mitigations

| Риск | Mitigation |
|---|---|
| Композиция 3 overlays перегрузит main | Ограничить до 3 в v1; `zprof doctor` warn'ит при 2+, error при 4+; overlays namespace-ятся так, что main видит только entry-points через orchestrator |
| Пользователь редактирует managed block вручную → потеря при sync | Три режима merge (overwrite/preserve/merge) с интерактивным prompt'ом; `.bak` перед перезаписью |
| Модель-alias резолвер устаревает | Резолвер зашит в CLI, обновляется с релизом; `zprof update` = обновление и таблицы; при неизвестном alias → error с подсказкой |
| Orchestrator сам жрёт токены (T3) | Orchestrator получает только schema-outputs от inner Task'ов, не body; sonnet вместо opus для дефолтного orchestrator'а |
| Overlay-специфичный `explorer` избыточен | В v1 всё же держим в overlay — Rust cargo layout ≠ Python monorepo ≠ Xcode workspace; ревью в v2 |
| Trigger-phrase collision между overlays | Namespace через overlay префикс + primary/secondary hint в trigger'е; в `zprof doctor` — детектор коллизий |
| RE-overlay hypothesis fan-out через Workflow слишком тяжёл | Ограничить N гипотез в `re-macho/loop.md` (default 5, override через `.zprof.yaml`) |
| Русскоязычные trigger-фразы — LLM пропускает | Дублировать EN + RU alias в AGENT_LOOP; логирование пропусков в `lessons.md` |

## 17. Success criteria для v1

Минимальный acceptance:

1. `zprof init` в свежем iOS-проекте (`anti-backlog`) обнаруживает `ios-swift`, предлагает apply, применяет — `.claude/` содержит 6 stack-aware ролей + tool-agents, `AGENT_LOOP.md` с dev-pipeline, `CLAUDE.md` с YAML-конфигом STACK/BUILD_CMD/TEST_CMD
2. В multi-stack проекте (`sm_monitor` — Py+Next) `zprof init` обнаруживает оба, apply создаёт namespace-ed агентов + AGENT_LOOP с двумя entry-points
3. Trigger-фраза «take next task» / «следующая задача» приводит к dispatch `dev-orchestrator`, который производит один final artifact (PR link + report), main получает ≤500 токенов на весь pipeline
4. `zprof sync` после ручного редактирования не-managed региона CLAUDE.md сохраняет пользовательский текст, обновляет managed blocks
5. `zprof agents set architect --model opus-1m` меняет модель в agent frontmatter, `zprof doctor` подтверждает корректность
6. `re-macho` overlay в `crash_for_test/` — команда «символизируй crash-123.ips» дёргает `exploratory-orchestrator`, создаётся `reports/2026-07-16-crash-123.md`
7. Обновление модели в резолвере (например, релиз `opus-5`) → `zprof sync` подхватывает без изменений в overlays

## 18. v2 backlog

- `re-android`, `backend-go` overlays
- Экспорт в Cursor/Windsurf/Codex/OpenCode (harnest DNA)
- Кастомные slash commands per-overlay
- MCP-config генерация (Zeal MCP для re-macho, context7 для backend-python)
- Git-hooks генерация (swiftlint/ruff/clippy pre-commit, test pre-push)
- Makefile/justfile generation с BUILD_CMD/TEST_CMD
- Плюрализм источников (`zprof repo add <url>`)
- Templating engine для custom overlays (`zprof scaffold overlay <name>`)
- Analytics: сколько токенов main-сессия тратит с/без T3/T4, autotuning
