# task-runner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Вынести контроллер agent-loop из main-сессии в отдельного агента `task-runner`, который принимает одну задачу и возвращает одну схему.

**Architecture:** `task-runner` поглощает `dev-orchestrator` и `exploratory-orchestrator` — глубина вложенности не растёт. Роутинг переезжает из `AGENT_LOOP.md` (который читал main) внутрь раннера. Развилки вне стоп-листа раннер решает сам; на стоп-листе возвращает `blocked` с вопросом, и main передаёт вопрос пользователю дословно. Прогресс пишется в `.zprof/runs/<id>.md` — main читает хвост только по запросу.

**Tech Stack:** Go 1.22+, `gopkg.in/yaml.v3`, `stretchr/testify/require`, markdown-профили с YAML frontmatter.

**Спека:** `docs/superpowers/specs/2026-07-28-task-runner-design.md`

**Исполнение:** задачи 1–7 выполняются сабагентами. Задачи 0 и 8 — ручные: требуют живой интерактивной сессии Claude Code в другом проекте с применённым zprof, сабагенту это недоступно. Их делает Alex.

## Global Constraints

- **Все коммиты делаются из корня репозитория, в родительский репо `vaporphd/zprof`.** Каталог `profiles/` содержит `.git` — пустой клон `vaporphd/zprof-profiles` без единого коммита. Содержимое `profiles/` трекается родительским репо (174 файла). Команда `cd profiles && git commit` отправит работу в репозиторий-призрак. Никогда не выполняй git-команды с `-C profiles` или из этого каталога.
- Go-тесты запускаются из `cli/`: `cd cli && go test ./...`.
- Язык тела промптов агентов, workflow-файлов и AGENT_LOOP — **русский**. Имена агентов, YAML-ключи, `description:` во frontmatter — английский (v1-спека §11).
- Модель в frontmatter агента указывается **алиасом тира** (`opus` / `sonnet` / `haiku` / `opus-1m` / `opus-4-6` / `fable`), не точным ID. Точный ID подставляет CLI при apply.
- `return_format` в каждом агенте начинается с комментария `# CRITICAL: ответ начинается с \`verdict:\` — без преамбулы и код-фенса.`
- Тесты используют `github.com/stretchr/testify/require`. Фикстуры лежат в `cli/testdata/`.
- Никаких новых зависимостей.

---

## File Structure

**Новые файлы**

| Путь | Ответственность |
|---|---|
| `cli/internal/agents/retired.go` | Единственный источник правды: имена агентов, которые zprof больше не поставляет |
| `cli/internal/apply/prune.go` | Удаление агентов, которых zprof больше не поставляет |
| `cli/internal/apply/prune_test.go` | Тесты удаления |
| `cli/testdata/overlays/with-stop-list.yaml` | Фикстура манифеста со `stop_list` |
| `profiles/base/agents/task-runner.md` | Промпт раннера |

**Изменяемые файлы**

| Путь | Изменение |
|---|---|
| `cli/internal/manifest/project.go` | `ManagedAgents []string` в `.zprof.yaml` |
| `cli/internal/manifest/overlay.go` | `StopList []string` в манифесте overlay |
| `cli/internal/apply/engine.go` | вызов prune, `.zprof/runs/` в gitignore, блок `stop-list` |
| `cli/internal/apply/tables.go` | `roleAgents` → `task-runner`; `buildStopListBlock` |
| `cli/internal/doctor/diagnostics.go` | четыре новые проверки |
| `profiles/base/agent-loop-router.md` | переписан как контракт main'а |
| `profiles/base/workflows/dev-pipeline.md` | переписан как инструкция раннеру |
| `profiles/base/workflows/exploratory.md` | то же |
| `profiles/base/claude-block-base.md` | правило границы мутация/чтение |
| `profiles/base/manifest.yaml` | базовый `stop_list` |
| `profiles/overlays/*/manifest.yaml` (9 шт.) | `stop_list` |
| `profiles/overlays/*/loop.md` (9 шт.) | чистка от «main дёргает X» |

**Удаляемые файлы**

- `profiles/base/agents/dev-orchestrator.md`
- `profiles/base/agents/exploratory-orchestrator.md`

---

## Task 0: Снять baseline

Критерий приёмки §12.4 сравнивает токены «до» и «после». После миграции замер «до» не воспроизвести — снимаем первым.

**Files:**
- Create: `docs/reviews/2026-07-28-task-runner-baseline.md`

- [ ] **Step 1: Выбрать проект с применённым zprof и прогнать типовую задачу**

Нужен реальный проект, где уже есть `.zprof.yaml` и `.claude/agents/`. Задача — багфикс или маленькая фича, проходящая полный цикл. Прогон делается вручную в интерактивной сессии Claude Code, на текущей схеме (main крутит петлю).

- [ ] **Step 2: Снять отчёт eval**

```bash
cd <проект-с-zprof>
zprof eval > /tmp/baseline-eval.txt
```

Без аргумента `eval` берёт самый свежий `.jsonl` под `~/.claude/projects/<slug соответствует cwd>/`.

- [ ] **Step 3: Записать замер**

Создай `docs/reviews/2026-07-28-task-runner-baseline.md` в репо zprof. Обязательные поля: проект, формулировка задачи, дата, количество диспатчей верхнего уровня, суммарные `SubagentTokens`, нарушения `Compliance`. Вставь вывод `zprof eval` целиком в fenced-блок.

- [ ] **Step 4: Commit**

```bash
git add docs/reviews/2026-07-28-task-runner-baseline.md
git commit -m "docs(reviews): baseline замер main-контекста до task-runner"
```

---

## Task 1: `managed_agents` и удаление осиротевших агентов

Без этого апгрейд оставляет рабочий `dev-orchestrator.md` в `.claude/agents/`, main его дёргает, и утечка сохраняется ровно там, где мы её чиним.

**Files:**
- Create: `cli/internal/agents/retired.go`
- Modify: `cli/internal/manifest/project.go:20-27`
- Create: `cli/internal/apply/prune.go`
- Create: `cli/internal/apply/prune_test.go`
- Modify: `cli/internal/apply/engine.go:29-35` (ApplyResult), `:49-83` (после записи агентов)

**Interfaces:**
- Consumes: `managed.BackupBeforeWrite(path string) (string, error)`, `manifest.ProjectManifest`
- Produces: `agents.Retired []string` (потребляется также Task 7); `apply.PruneOrphanAgents(agentDir string, previous, current []string) ([]string, error)`; поле `ProjectManifest.ManagedAgents []string`; поле `ApplyResult.RemovedAgents []string`

- [ ] **Step 1: Написать падающий тест**

Создай `cli/internal/apply/prune_test.go`:

```go
package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeAgentFile(t *testing.T, dir, name string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	p := filepath.Join(dir, name+".md")
	require.NoError(t, os.WriteFile(p, []byte("---\nname: "+name+"\nmodel: sonnet\n---\nbody\n"), 0o644))
	return p
}

// Агент, который zprof писал раньше и больше не поставляет, удаляется, а
// его содержимое сохраняется в .bak.
func TestPruneRemovesOrphanWrittenByZprof(t *testing.T) {
	dir := t.TempDir()
	p := writeAgentFile(t, dir, "legacy-agent")

	removed, err := PruneOrphanAgents(dir, []string{"legacy-agent", "planner"}, []string{"planner"})
	require.NoError(t, err)
	require.Equal(t, []string{"legacy-agent"}, removed)
	require.NoFileExists(t, p)

	baks, err := filepath.Glob(filepath.Join(dir, "legacy-agent.md.zprof.bak-*"))
	require.NoError(t, err)
	require.Len(t, baks, 1, "удаление должно оставить ровно один бэкап")
}

// Агент, которого zprof никогда не писал, не трогается — это файл пользователя.
func TestPruneKeepsUserAuthoredAgent(t *testing.T) {
	dir := t.TempDir()
	p := writeAgentFile(t, dir, "my-custom-agent")

	removed, err := PruneOrphanAgents(dir, []string{"planner"}, []string{"planner"})
	require.NoError(t, err)
	require.Empty(t, removed)
	require.FileExists(t, p)
}

// Упразднённые имена удаляются даже когда previous пуст — это проекты,
// применённые до появления managed_agents.
func TestPruneRemovesRetiredAgentWithoutTrackingList(t *testing.T) {
	dir := t.TempDir()
	orch := writeAgentFile(t, dir, "dev-orchestrator")
	expl := writeAgentFile(t, dir, "exploratory-orchestrator")

	removed, err := PruneOrphanAgents(dir, nil, []string{"planner", "task-runner"})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"dev-orchestrator", "exploratory-orchestrator"}, removed)
	require.NoFileExists(t, orch)
	require.NoFileExists(t, expl)
}

// Агент, присутствующий в текущем составе, не удаляется, даже если он же
// числится в previous.
func TestPruneKeepsStillLiveAgent(t *testing.T) {
	dir := t.TempDir()
	p := writeAgentFile(t, dir, "planner")

	removed, err := PruneOrphanAgents(dir, []string{"planner"}, []string{"planner"})
	require.NoError(t, err)
	require.Empty(t, removed)
	require.FileExists(t, p)
}

// Отсутствие файла на диске — не ошибка: запись могла быть удалена руками.
func TestPruneSkipsMissingFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(dir, 0o755))

	removed, err := PruneOrphanAgents(dir, []string{"ghost"}, nil)
	require.NoError(t, err)
	require.Empty(t, removed)
}
```

- [ ] **Step 2: Прогнать тест — убедиться, что падает**

Run: `cd cli && go test ./internal/apply/ -run TestPrune -v`
Expected: FAIL — `undefined: PruneOrphanAgents`

- [ ] **Step 3: Создать общий пакет с упразднёнными именами**

Список нужен двум пакетам — `apply` (удаляет файлы) и `doctor` (диагностирует уцелевшие). Две копии разъедутся, поэтому источник один.

Создай `cli/internal/agents/retired.go`:

```go
// Package agents holds facts about zprof's agent roster that more than one
// subsystem needs to agree on.
package agents

// Retired lists agent names zprof shipped in earlier versions and has since
// removed. They were never user-authored, so `apply` prunes them even in
// projects applied before `managed_agents` existed — there the tracking
// list is empty and the general orphan rule can never fire, which would
// leave a working dev-orchestrator behind for main to dispatch, preserving
// the exact leak this change closes. `doctor` reports any that survive.
var Retired = []string{"dev-orchestrator", "exploratory-orchestrator"}
```

- [ ] **Step 3.5: Реализовать `PruneOrphanAgents`**

Создай `cli/internal/apply/prune.go`:

```go
package apply

import (
	"os"
	"path/filepath"

	"github.com/vaporphd/zprof/internal/agents"
	"github.com/vaporphd/zprof/internal/managed"
)

// PruneOrphanAgents removes agent files zprof wrote on a previous apply
// that the current sources no longer provide. `previous` is the
// managed_agents list read from .zprof.yaml; `current` is what this apply
// just wrote. Names absent from `previous` are user-authored and are never
// touched. Every removal is backed up first. Returns the removed names.
func PruneOrphanAgents(agentDir string, previous, current []string) ([]string, error) {
	live := make(map[string]bool, len(current))
	for _, n := range current {
		live[n] = true
	}

	seen := map[string]bool{}
	var doomed []string
	for _, n := range append(append([]string(nil), previous...), agents.Retired...) {
		if n == "" || live[n] || seen[n] {
			continue
		}
		seen[n] = true
		doomed = append(doomed, n)
	}

	var removed []string
	for _, name := range doomed {
		path := filepath.Join(agentDir, name+".md")
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, err
		}
		if _, err := managed.BackupBeforeWrite(path); err != nil {
			return removed, err
		}
		if err := os.Remove(path); err != nil {
			return removed, err
		}
		removed = append(removed, name)
	}
	return removed, nil
}
```

- [ ] **Step 4: Прогнать тест — убедиться, что проходит**

Run: `cd cli && go test ./internal/apply/ -run TestPrune -v`
Expected: PASS (5 тестов)

- [ ] **Step 5: Добавить поле в ProjectManifest**

В `cli/internal/manifest/project.go` внутрь структуры `ProjectManifest`, после `AgentOverrides`:

```go
	// ManagedAgents lists the agent names zprof itself wrote on the last
	// apply. Names present here but absent from the current sources are
	// orphans from an earlier profile version and get pruned on the next
	// apply; anything not listed is user-authored and never touched.
	ManagedAgents []string `yaml:"managed_agents,omitempty"`
```

- [ ] **Step 6: Подключить prune в engine**

В `cli/internal/apply/engine.go` добавь `"sort"` в импорты. В структуру `ApplyResult` добавь поле:

```go
	RemovedAgents []string
```

Сразу после цикла записи overlay-агентов (заканчивается на `res.CreatedAgents = append(res.CreatedAgents, out)` и закрывающих скобках) вставь:

```go
	// 2.5. Prune agents a previous apply wrote that no longer exist, then
	// record the current roster so the next apply can do the same.
	removed, err := PruneOrphanAgents(agentDest, opts.Project.ManagedAgents, res.CreatedAgents)
	if err != nil {
		return nil, fmt.Errorf("prune orphan agents: %w", err)
	}
	res.RemovedAgents = removed
	opts.Project.ManagedAgents = append([]string(nil), res.CreatedAgents...)
	sort.Strings(opts.Project.ManagedAgents)
```

Место важно: до шага 3 (рендер AGENT_LOOP.md) и заведомо до шага 7 (`opts.Project.Save`), иначе список не попадёт в `.zprof.yaml`.

- [ ] **Step 7: Прогнать все тесты пакета**

Run: `cd cli && go test ./... `
Expected: PASS. Если `engine_test.go` или `e2e_test.go` сравнивают `ApplyResult` целиком — обнови ожидания, добавив пустой `RemovedAgents`.

- [ ] **Step 8: Commit**

```bash
git add cli/internal/agents/retired.go cli/internal/apply/prune.go \
        cli/internal/apply/prune_test.go cli/internal/apply/engine.go \
        cli/internal/manifest/project.go
git commit -m "feat(apply): удалять осиротевших агентов через managed_agents"
```

---

## Task 2: `stop_list` в манифесте overlay

**Files:**
- Modify: `cli/internal/manifest/overlay.go:16-35`
- Create: `cli/testdata/overlays/with-stop-list.yaml`
- Modify: `cli/internal/manifest/overlay_test.go` (добавить тесты в конец)

**Interfaces:**
- Produces: поле `OverlayManifest.StopList []string`

- [ ] **Step 1: Создать фикстуру**

Создай `cli/testdata/overlays/with-stop-list.yaml`:

```yaml
name: ios-swift
display_name: "iOS / Swift"
version: 0.1.0
loop_template: dev-pipeline
stop_list:
  - "загрузка билда в TestFlight или App Store Connect"
  - "смена bundle identifier, entitlements, provisioning profile"
```

- [ ] **Step 2: Написать падающий тест**

Добавь в конец `cli/internal/manifest/overlay_test.go`:

```go
func TestLoadOverlayParsesStopList(t *testing.T) {
	m, err := LoadOverlay(filepath.Join("..", "..", "testdata", "overlays", "with-stop-list.yaml"))
	require.NoError(t, err)
	require.Len(t, m.StopList, 2)
	require.Equal(t, "загрузка билда в TestFlight или App Store Connect", m.StopList[0])
}

// Манифест без stop_list грузится без ошибки: пустоту диагностирует
// zprof doctor, а не парсер — иначе старые overlay'и перестанут читаться.
func TestLoadOverlayWithoutStopListIsValid(t *testing.T) {
	m, err := LoadOverlay(filepath.Join("..", "..", "testdata", "overlays", "valid-manifest.yaml"))
	require.NoError(t, err)
	require.Empty(t, m.StopList)
}
```

- [ ] **Step 3: Прогнать тест — убедиться, что падает**

Run: `cd cli && go test ./internal/manifest/ -run TestLoadOverlayParsesStopList -v`
Expected: FAIL — `m.StopList undefined`

- [ ] **Step 4: Добавить поле**

В `cli/internal/manifest/overlay.go`, в структуру `OverlayManifest` после `Executing`:

```go
	// StopList enumerates irreversible or outward-facing actions the
	// task-runner must never take on its own — it returns verdict=blocked
	// and hands the decision back to the user instead. Plain
	// human-readable sentences: the runner reads them, nothing matches
	// them mechanically. Emptiness is diagnosed by `zprof doctor`, not by
	// this parser, so overlays predating the field still load.
	StopList []string `yaml:"stop_list"`
```

- [ ] **Step 5: Прогнать тест — убедиться, что проходит**

Run: `cd cli && go test ./internal/manifest/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cli/internal/manifest/overlay.go cli/internal/manifest/overlay_test.go cli/testdata/overlays/with-stop-list.yaml
git commit -m "feat(manifest): парсинг stop_list в манифесте overlay"
```

---

## Task 3: Блок `stop-list` в CLAUDE.md и `.zprof/runs/` в gitignore

Стоп-лист, которого пользователь не видит, — это скрытая политика. Рендерим его туда же, где остальные правила проекта.

**Files:**
- Modify: `cli/internal/apply/tables.go` (добавить функцию в конец, до хелперов)
- Modify: `cli/internal/apply/engine.go:196-223` (`buildClaudeBlocks`), `:256` (entries в `ensureGitignore`)
- Modify: `cli/internal/apply/engine_test.go` (добавить тест в конец)

**Interfaces:**
- Consumes: `OverlayManifest.StopList` (Task 2)
- Produces: `buildStopListBlock(opts ApplyOpts) string`; managed-блок с `Overlay: "base", Key: "stop-list"`

- [ ] **Step 1: Написать падающий тест**

Добавь в конец `cli/internal/apply/engine_test.go`:

```go
func TestBuildStopListBlockMergesBaseAndOverlays(t *testing.T) {
	opts := ApplyOpts{
		Base: &overlay.Base{
			Manifest: &manifest.OverlayManifest{
				Name:     "base",
				StopList: []string{"force-push в опубликованную ветку", "релиз, деплой, публикация пакета"},
			},
		},
		Overlays: []*overlay.Overlay{
			{Manifest: &manifest.OverlayManifest{
				Name:     "ios-swift",
				StopList: []string{"загрузка билда в TestFlight", "релиз, деплой, публикация пакета"},
			}},
		},
	}

	got := buildStopListBlock(opts)

	require.Contains(t, got, "## Stop list")
	require.Contains(t, got, "| force-push в опубликованную ветку | base |")
	require.Contains(t, got, "| загрузка билда в TestFlight | ios-swift |")
	// Дубль, объявленный и в base, и в overlay, рендерится один раз — с
	// источником base, потому что base идёт первым.
	require.Equal(t, 1, strings.Count(got, "релиз, деплой, публикация пакета"))
}
```

Убедись, что в импортах файла есть `strings`, `github.com/vaporphd/zprof/internal/manifest`, `github.com/vaporphd/zprof/internal/overlay`, `github.com/stretchr/testify/require`.

- [ ] **Step 2: Прогнать тест — убедиться, что падает**

Run: `cd cli && go test ./internal/apply/ -run TestBuildStopListBlock -v`
Expected: FAIL — `undefined: buildStopListBlock`

- [ ] **Step 3: Реализовать рендер**

Добавь в `cli/internal/apply/tables.go` перед `sortedMapKeys`:

```go
// buildStopListBlock renders the "## Stop list" section for CLAUDE.md: the
// base entries followed by each active overlay's, deduplicated, every row
// tagged with the source that declared it. The task-runner refuses to
// perform these on its own and returns verdict=blocked instead. Rendered
// into CLAUDE.md on purpose — a policy the user cannot read is a hidden
// policy.
func buildStopListBlock(opts ApplyOpts) string {
	var b strings.Builder
	b.WriteString("## Stop list\n\n")
	b.WriteString("`task-runner` не выполняет перечисленное самостоятельно: он возвращает `verdict: blocked` с вопросом, решение принимает человек.\n\n")
	b.WriteString("| Действие | Источник |\n")
	b.WriteString("|---|---|\n")

	seen := map[string]bool{}
	appendRows := func(entries []string, source string) {
		for _, e := range entries {
			if e == "" || seen[e] {
				continue
			}
			seen[e] = true
			b.WriteString("| " + e + " | " + source + " |\n")
		}
	}

	if opts.Base != nil && opts.Base.Manifest != nil {
		appendRows(opts.Base.Manifest.StopList, "base")
	}
	for _, o := range opts.Overlays {
		if o == nil || o.Manifest == nil {
			continue
		}
		appendRows(o.Manifest.StopList, o.Manifest.Name)
	}

	return strings.TrimRight(b.String(), "\n")
}
```

- [ ] **Step 4: Прогнать тест — убедиться, что проходит**

Run: `cd cli && go test ./internal/apply/ -run TestBuildStopListBlock -v`
Expected: PASS

- [ ] **Step 5: Подключить блок в CLAUDE.md**

В `cli/internal/apply/engine.go`, в `buildClaudeBlocks`, после блока `executing` и перед `return blocks`:

```go
	blocks = append(blocks, managed.Block{
		Overlay: "base",
		Key:     "stop-list",
		Content: buildStopListBlock(opts),
	})
```

- [ ] **Step 6: Добавить `.zprof/runs/` в gitignore**

В `cli/internal/apply/engine.go`, в `ensureGitignore`, замени строку с `entries := ...` на:

```go
	entries := []string{"thoughts/", ".zprof/runs/", "*.zprof.bak-*", ".zprof.yaml.bak-*"}
```

- [ ] **Step 7: Прогнать все тесты**

Run: `cd cli && go test ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add cli/internal/apply/tables.go cli/internal/apply/engine.go cli/internal/apply/engine_test.go
git commit -m "feat(apply): сводный stop-list в CLAUDE.md, .zprof/runs/ в gitignore"
```

---

## Task 4: Агент `task-runner`, удаление оркестраторов

**Files:**
- Create: `profiles/base/agents/task-runner.md`
- Delete: `profiles/base/agents/dev-orchestrator.md`, `profiles/base/agents/exploratory-orchestrator.md`
- Modify: `cli/internal/apply/tables.go:20-32` (`roleAgents`), `:150` (fallback в `buildExecutingTable`)
- Create: `cli/internal/apply/tables_test.go` (файла нет)
- Modify: `cli/internal/apply/e2e_test.go:62-63` — ассерты на файлы оркестраторов
- Modify: `cli/internal/eval/scoring.go:17` — `roleGuessRe`

**Interfaces:**
- Consumes: `PruneOrphanAgents` (Task 1) удалит старые файлы из уже применённых проектов
- Produces: агент с именем `task-runner`, попадающий в таблицу Consilium как роль

- [ ] **Step 1: Написать падающий тест на Consilium**

Добавь в конец `cli/internal/apply/tables_test.go` (создай файл с `package apply`, если его нет):

```go
package apply

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/overlay"
)

func TestConsiliumListsTaskRunnerNotOrchestrators(t *testing.T) {
	opts := ApplyOpts{
		Base: &overlay.Base{
			Manifest: &manifest.OverlayManifest{Name: "base"},
			Agents: map[string]string{
				"task-runner": "---\nname: task-runner\nmodel: sonnet\n---\n",
				"planner":     "---\nname: planner\nmodel: sonnet\n---\n",
			},
		},
		Overlays: []*overlay.Overlay{
			{Manifest: &manifest.OverlayManifest{Name: "ios-swift"}, Agents: map[string]string{}},
		},
		Project: &manifest.ProjectManifest{},
	}

	got := buildConsiliumTable(opts)

	require.Contains(t, got, "| task-runner | task-runner | base |")
	require.NotContains(t, got, "dev-orchestrator")
	require.NotContains(t, got, "exploratory-orchestrator")
}

func TestIsKnownRoleAcceptsTaskRunner(t *testing.T) {
	require.True(t, IsKnownRole("task-runner"))
	require.False(t, IsKnownRole("dev-orchestrator"))
}
```

- [ ] **Step 2: Прогнать тест — убедиться, что падает**

Run: `cd cli && go test ./internal/apply/ -run "TestConsiliumListsTaskRunner|TestIsKnownRoleAcceptsTaskRunner" -v`
Expected: FAIL — `task-runner` не в `roleAgents`, поэтому строки нет

- [ ] **Step 3: Поправить `roleAgents`**

В `cli/internal/apply/tables.go` замени карту `roleAgents`:

```go
var roleAgents = map[string]bool{
	"planner":        true,
	"docs-writer":    true,
	"task-runner":    true,
	"architect":      true,
	"implementer":    true,
	"tester":         true,
	"bug-hunter":     true,
	"refactor-agent": true,
	"explorer":       true,
	"reviewer":       true,
}
```

Записи `dev-orchestrator` и `exploratory-orchestrator` удаляются.

Там же, в `buildExecutingTable`, замени строку fallback'а `agent := "dev-orchestrator"` на:

```go
	agent := "task-runner"
```

- [ ] **Step 3.5: Починить e2e-ассерты и распознавание роли в eval**

`e2e_test.go` гоняет apply на **настоящем** каталоге `profiles/`, поэтому удаление оркестраторов ломает его ассерты. В `cli/internal/apply/e2e_test.go` замени две строки

```go
		".claude/agents/dev-orchestrator.md",
		".claude/agents/exploratory-orchestrator.md",
```

на одну:

```go
		".claude/agents/task-runner.md",
```

Проверь, нет ли таких же ассертов в `TestE2E_IOSApplyWithGates` ниже по файлу:

Run: `grep -n "orchestrator" cli/internal/apply/e2e_test.go`
Expected после правки: пусто

Затем в `cli/internal/eval/scoring.go` добавь `task-runner` в `roleGuessRe` — без этого замер §12.1 в Task 8 не сможет атрибутировать диспатч к роли:

```go
var roleGuessRe = regexp.MustCompile(`(?i)\b(architect|implementer|tester|reviewer|bug[- ]?hunter|refactor(?:-agent)?|explorer|planner|task[- ]?runner|dev[- ]?orchestrator|exploratory[- ]?orchestrator|docs[- ]?writer|xcodegen[- ]?driver|xcode[- ]?runner|spm[- ]?manager|swiftlint[- ]?checker|simulator[- ]?driver|testflight[- ]?shipper|evaluator)\b`)
```

Старые имена оркестраторов в регулярке **остаются**: `zprof eval` читает и архивные сессии, снятые до этого изменения, включая baseline из Task 0.

- [ ] **Step 4: Прогнать тест — убедиться, что проходит**

Run: `cd cli && go test ./internal/apply/ ./internal/eval/ -v`
Expected: PASS

- [ ] **Step 5: Написать промпт раннера**

Создай `profiles/base/agents/task-runner.md`:

````markdown
---
name: task-runner
description: Owns the whole agent loop for ONE task. Main spawns it with a task and gets a single schema back; the runner routes, dispatches the chain, retries failing tests, and keeps its own run log. Dispatch it for any request that MUTATES the repository. Trigger phrases — EN — "implement", "fix this", "refactor", "take next task", "next task", "ship the slice". RU — "сделай", "реализуй", "почини", "исправь", "отрефактори", "следующая задача", "прогони пайплайн".
tools: Task, Read, Write, Glob, Grep, Bash
model: sonnet
color: yellow
return_format: |
  # CRITICAL: ответ начинается с `verdict:` — без преамбулы и код-фенса.
  verdict: done|blocked|failed
  artifact: <PR link | commit SHA | reports/*.md>
  run_log: .zprof/runs/<id>.md
  one_line: <≤120 символов>
  question: <только при blocked — что решить>
  resume_hint: <только при blocked — где остановились>
---

# Task Runner

Ты владеешь agent-loop целиком. Main отдал тебе **одну задачу** и больше в
цикл не вмешивается. Ты сам роутишь, сам диспатчишь цепочку, сам ведёшь
журнал и возвращаешь **одну** схему.

## Что ты НЕ делаешь

- **Не пишешь код.** Не редактируешь исходники, не создаёшь модули.
- **Не запускаешь билды и тесты.** Этим занимаются tool-агенты.
- **Не берёшь следующую задачу.** Закончил — вернул схему и умер.

`Write` у тебя только ради журнала. `Bash` — только `date` и `git log -1`.
Если тянет отредактировать файл самому — значит, нужного агента не хватает:
верни `verdict: failed` и скажи, какого.

## Вход

```
task: <формулировка пользователя дословно>
context_hint: <файл или модуль; может быть пусто>
resume_from: <путь к run-логу, если это продолжение после blocked>
decision: <ответ пользователя, если resume_from задан>
```

## Старт

1. Read `.zprof.yaml` — активные overlay'и, их порядок (приоритет при
   конфликте имён), `model_overrides`.
2. Read `CLAUDE.md` — секции `## Stop list`, `## Consilium`, `## Executing`.
3. Read нужный `workflows/*.md` — базовую часть и расширения активных
   overlay'ев.
4. Если задан `resume_from` — Read этот журнал и артефакты, на которые он
   ссылается. Продолжай с шага, указанного в `resume_hint`. **Не
   пересоздавай** уже существующие `plan-N.md` и ADR.
5. Заведи журнал (см. «Журнал»), если это не продолжение.

## Роутинг

Классифицируй задачу и выбери маршрут:

| Тип | Цепочка |
|---|---|
| Новая фича | `planner → architect → implementer → tester → reviewer` |
| Багфикс | `bug-hunter → tester → reviewer` |
| Рефактор без новой функциональности | `refactor-agent → tester → reviewer` |
| Только тесты | `tester` |
| Только ревью | `reviewer` |
| RE / анализ бинаря | `intake → unpacker → explorer → hypothesizer → verifier → report-writer` |

Имена агентов бери из таблицы `## Consilium` в `CLAUDE.md` — при
нескольких overlay'ях они namespace-нуты (`implementer-ios`,
`implementer-py`). Если активно несколько overlay'ев, выбирай namespace по
затронутым файлам; при неоднозначности — по порядку `overlays:` в
`.zprof.yaml`. Если задача пересекает два стека — диспатчи `planner` в
мульти-таргет режиме и гоняй две цепочки последовательно.

## Правила диспатча

- Один агент за раз, дожидайся результата.
- Читай **только** поля схемы: `verdict`, `artifact`, `next`, `one_line`.
  Не втягивай содержимое артефактов в свой контекст без необходимости —
  оно нужно следующему агенту, а не тебе.
- `verdict: failed` у любого агента — цепочка обрывается немедленно.
- `verdict: blocked` у агента — оцени: если причина в стоп-листе, эскалируй
  наверх (см. ниже); если это нехватка данных, которую закрывает соседний
  агент, — дай ему следующий шаг.
- `tester` вернул `failed` — это **не** провал цикла: верни задачу
  `implementer`'у с текстом падения. Максимум **три** круга. Не сошлось за
  три — `verdict: blocked` с историей попыток.
- Агент вернул не-схему — повтори диспатч один раз с требованием вернуть
  только схему. Второй сбой — `verdict: failed`.
- Нужного агента нет в `.claude/agents/` — сразу `verdict: failed` с
  указанием имени. Это ошибка конфигурации, чинить её на ходу нельзя.

## Стоп-лист

Секция `## Stop list` в `CLAUDE.md` перечисляет необратимое. Наткнулся на
такое действие — **не делай и не поручай**:

1. Запиши в журнал строку `BLOCKED` с вопросом.
2. Верни `verdict: blocked`, `question` (что именно решить, одним
   предложением), `resume_hint` (файл и шаг, где остановились).

Всё, чего в стоп-листе нет, решай сам: опирайся на `docs/PROJECT_SPEC.md`,
существующие ADR и `lessons.md`, зафиксируй выбор новым ADR через
`architect`, едь дальше. Не блокируйся на вкусовщине — это возвращает
трафик в main, ради чего всё и затевалось.

## Журнал

Путь: `.zprof/runs/<YYYY-MM-DD>-<slug>.md`, `slug` — из формулировки задачи
(латиницей, через дефис, ≤40 символов). Дату бери из `date +%F`.

Формат:

```markdown
# <task дословно>
started: <ISO-время> · overlays: <список> · route: <workflow>/<тип>

| время | агент | verdict | artifact |
|-------|-------|---------|----------|
| 10:02 | bug-hunter-ios | done | reports/crash-repro.md |

## Итог
verdict: done · artifact: PR #128
```

Правила: одна строка на шаг, **≤120 символов**, вывод агентов не
вставляется — иначе журнал станет тем же мусором, просто на диске.
Секцию `## Итог` пиши последним действием перед возвратом схемы.

## Возврат

Финальный ответ — **только** схема из `return_format`. Никакой преамбулы,
никакого пересказа того, что делали агенты. `artifact` — ссылка на PR,
SHA коммита или путь к отчёту. `run_log` — путь к журналу всегда.
````

- [ ] **Step 6: Удалить оркестраторы**

```bash
git rm profiles/base/agents/dev-orchestrator.md profiles/base/agents/exploratory-orchestrator.md
```

- [ ] **Step 7: Проверить, что на них никто не ссылается**

Run: `grep -rn "dev-orchestrator\|exploratory-orchestrator" profiles/ cli/ --include="*.md" --include="*.go" --include="*.yaml"`
Expected: совпадения только в `cli/internal/apply/prune.go` (список `retiredAgents`) и в тестах Task 1/4. Любое совпадение в `profiles/` — незакрытая ссылка, чини в Task 5 или 6.

- [ ] **Step 8: Commit**

```bash
git add profiles/base/agents/task-runner.md cli/internal/apply/tables.go \
        cli/internal/apply/tables_test.go cli/internal/apply/e2e_test.go \
        cli/internal/eval/scoring.go
git commit -m "feat(base): task-runner вместо dev/exploratory оркестраторов"
```

Удаления `profiles/base/agents/dev-orchestrator.md` и
`exploratory-orchestrator.md` уже проиндексированы через `git rm` в шаге 6.

---

## Task 5: Роутер и workflow-файлы

`AGENT_LOOP.md` перестаёт быть скриптом для main и становится его контрактом. Из `dev-pipeline.md` уходят разрешения ходить мимо оркестратора — иначе main снова начнёт диспатчить агентов напрямую.

**Files:**
- Modify: `profiles/base/agent-loop-router.md` (полная замена)
- Modify: `profiles/base/workflows/dev-pipeline.md` (полная замена)
- Modify: `profiles/base/workflows/exploratory.md` (полная замена)
- Modify: `profiles/base/claude-block-base.md`

- [ ] **Step 1: Переписать роутер**

Замени содержимое `profiles/base/agent-loop-router.md` целиком:

```markdown
# Контракт main-сессии

Этот файл — правила для main, а не список агентов. Маршрутизацию внутри
задачи ведёт `task-runner`, main в неё не вмешивается.

## Граница

| Запрос | Кто исполняет |
|---|---|
| Меняет репозиторий: фича, багфикс, рефактор, миграция, «следующая задача» | `task-runner` |
| Не меняет: объяснить, показать дифф, обсудить, закоммитить, «как там раннер?» | main сам, обычными инструментами |

Правило одно: **мутация — раннеру, чтение — сам.**

## Как диспатчить

Спавни `task-runner` с фиксированной формой:

```
task: <формулировка пользователя дословно>
context_hint: <файл или модуль, если пользователь указал; иначе пусто>
resume_from: <путь к run-логу, если продолжение после blocked>
decision: <ответ пользователя, если resume_from задан>
```

`task` передаётся **дословно**. Пересказ вносит твою интерпретацию, и
раннер начнёт решать искажённую задачу.

## Что делать с результатом

| verdict | Действие main |
|---|---|
| `done` | Сообщи пользователю `one_line` и `artifact`. Ничего не цитируй сверх этого. |
| `blocked` | Задай пользователю `question` **дословно**, через `AskUserQuestion`. Получив ответ — спавни **нового** раннера с `resume_from` и `decision`. |
| `failed` | Сообщи `one_line` и путь к `run_log`. Решение о следующем шаге — за пользователем. |

## Правила изоляции

1. Не цитируй вывод сабагента. Только `verdict`, `one_line`, `artifact`.
2. После каждого dispatch — ≤3 строки в `followup.md`, ответ раннера
   выброси из рабочей памяти.
3. На вопрос «как там?» читай **хвост** `run_log` (последние ~10 строк), не
   весь файл и не артефакты.
4. Никогда не диспатчи `implementer`, `tester`, `architect` и прочие роли
   напрямую. Их владелец — раннер.
```

- [ ] **Step 2: Переписать dev-pipeline**

Замени содержимое `profiles/base/workflows/dev-pipeline.md` целиком:

```markdown
# Dev pipeline

Инструкция для `task-runner`, не для main. Overlay подставляет свои
stack-aware агенты (`architect` / `implementer` / `tester` / `bug-hunter` /
`refactor-agent` / `explorer` / `reviewer`), при нескольких overlay'ях —
namespace-нутые.

## Маршруты

| Тип задачи | Цепочка |
|---|---|
| Новая фича | `planner → architect → implementer → tester → reviewer` |
| Багфикс | `bug-hunter → tester → reviewer` |
| Рефактор без новой функциональности | `refactor-agent → tester → reviewer` |
| Только тесты | `tester` |
| Только ревью | `reviewer` |
| Read-only investigation внутри задачи | `explorer` |

## Петля тестов

`tester` вернул `failed` — это не провал цикла. Верни работу
`implementer`'у с текстом падения, максимум **три** круга. Не сошлось —
`verdict: blocked` с историей попыток.

## Fan-out

Если требуется ≥5 независимых параллельных проверок (обзор многих файлов,
sweep по миграции) — используй Workflow tool вместо параллельных Task'ов.
Для параллельных `implementer`'ов задавай `isolation: "worktree"`, иначе
они подерутся за файлы.

## Изоляция

Читай только поля схемы возвращаемых агентов. Содержимое артефактов
втягивай, лишь когда оно нужно тебе для решения, а не «на всякий случай».
```

- [ ] **Step 3: Переписать exploratory**

Замени содержимое `profiles/base/workflows/exploratory.md` целиком:

```markdown
# Exploratory pipeline (RE / analysis)

Инструкция для `task-runner`, не для main.

## Маршрут

`intake → unpacker → explorer → hypothesizer → verifier → report-writer`

Выход — markdown-отчёт в `reports/YYYY-MM-DD-<slug>.md`, **не** PR.
Артефакты сюда, не в `docs/adr/`: RE-отчёт не архитектурное решение.

## Параллельные гипотезы

`hypothesizer` вернул N ≥ 3 гипотез — запускай `verifier` через Workflow
tool с parallel fan-out. По умолчанию не более **5** гипотез; лимит
переопределяется в `.zprof.yaml`.

## Legal scope

`intake` фиксирует границы разрешённого анализа. Выход за них — стоп-лист:
`verdict: blocked` с вопросом, а не самостоятельное решение.

## Изоляция

Те же правила, что в dev-pipeline: только поля схемы, артефакты точечно.
```

- [ ] **Step 4: Добавить правило границы в доктрину**

В `profiles/base/claude-block-base.md` замени раздел `### Изоляция` на:

```markdown
### Граница

Всё, что меняет репозиторий, исполняет `task-runner` — main спавнит его на
одну задачу и получает одну схему. Чтение, объяснения, обсуждение и
git-операции main делает сам. Правило: **мутация — раннеру, чтение — сам.**

### Изоляция
Main-сессия НИКОГДА не цитирует output subagent'а. После каждого dispatch:
запиши ≤3 строки в followup.md, дропни ответ subagent'а из рабочей памяти.
Прогресс задачи живёт в `.zprof/runs/<id>.md` — читай хвост по запросу, не
весь файл.
```

- [ ] **Step 5: Проверить, что ссылок на оркестраторов не осталось**

Run: `grep -rn "orchestrator" profiles/base/`
Expected: пусто

- [ ] **Step 6: Commit**

```bash
git add profiles/base/agent-loop-router.md profiles/base/workflows/ profiles/base/claude-block-base.md
git commit -m "feat(base): роутер как контракт main, workflow как инструкция раннеру"
```

---

## Task 6: Стоп-листы и чистка overlay'ев

**Files:**
- Modify: `profiles/base/manifest.yaml`
- Modify: `profiles/overlays/<name>/manifest.yaml` × 9
- Modify: `profiles/overlays/<name>/loop.md` × 9

- [ ] **Step 1: Базовый стоп-лист**

Добавь в конец `profiles/base/manifest.yaml`:

```yaml
stop_list:
  - "force-push, rebase или amend опубликованной ветки"
  - "удаление веток и тегов"
  - "запись за пределы репозитория: БД, продовые конфиги, секреты"
  - "релиз, деплой, публикация пакета"
  - "слом публичного API, у которого есть внешние потребители"
  - "новая платная зависимость или новый внешний сервис"
  - "любая отправка содержимого репозитория наружу"
```

- [ ] **Step 2: Стоп-листы overlay'ев**

Добавь `stop_list:` в конец каждого `profiles/overlays/<name>/manifest.yaml`:

**ios-swift**
```yaml
stop_list:
  - "загрузка билда в TestFlight или App Store Connect"
  - "смена bundle identifier, entitlements или provisioning profile"
  - "смена team ID и параметров подписи"
  - "правка project.pbxproj руками вместо project.yml"
```

**backend-kotlin-jvm**
```yaml
stop_list:
  - "публикация артефакта в Maven-репозиторий"
  - "смена groupId или artifactId"
  - "ломающая смена публичного API библиотеки"
  - "правка production-конфигов (application-prod.*)"
```

**kotlin-multiplatform**
```yaml
stop_list:
  - "публикация в Play Console или App Store Connect"
  - "смена applicationId или bundle identifier"
  - "смена keystore и параметров подписи"
  - "ломающая смена публичного API общего модуля"
```

**backend-python**
```yaml
stop_list:
  - "alembic downgrade на существующих данных"
  - "миграция с DROP TABLE или DROP COLUMN"
  - "правка production .env и файлов с секретами"
  - "публикация пакета в PyPI"
```

**frontend-web**
```yaml
stop_list:
  - "деплой на продовый хостинг"
  - "публикация npm-пакета"
  - "смена публичного контракта API-клиента, который потребляют другие приложения"
  - "правка продовых переменных окружения"
```

**systems-rust**
```yaml
stop_list:
  - "публикация крейта на crates.io"
  - "ломающая смена публичного API (semver major)"
  - "повышение MSRV"
  - "добавление unsafe в ранее safe публичный API"
```

**systems-cpp**
```yaml
stop_list:
  - "ломающая смена ABI публичной библиотеки"
  - "смена минимального стандарта C++ или тулчейна"
  - "публикация пакета в Conan-репозиторий"
```

**re-macho**
```yaml
stop_list:
  - "распространение распакованных или декриптованных бинарей и их частей"
  - "инструментация процесса вне legal scope, зафиксированного intake"
  - "обход DRM или проверки подписи за пределами статического анализа"
  - "публикация отчёта, содержащего проприетарный код"
```

**issue-loop-github-strict**
```yaml
stop_list:
  - "merge PR в основную ветку"
  - "закрытие issue"
  - "создание релиза или тега"
  - "изменение настроек репозитория и CI-секретов"
```

- [ ] **Step 3: Вычистить loop.md от адресации к main**

Пройди все девять `profiles/overlays/<name>/loop.md`. В каждом:

- Заголовок секции `### Специальные диспатчи` (или аналог) переименуй в
  `### Специальные диспатчи (для task-runner)`.
- Любую формулировку вида «main дёргает X», «main диспатчит X»,
  «main читает X» перепиши на «раннер дёргает X» / «раннер читает X».
- Формулировки про trigger-фразы оставь: они нужны main'у, чтобы понять,
  что запрос вообще относится к этому стеку.

Найти проблемные места:

Run: `grep -rn "main" profiles/overlays/*/loop.md`
Expected после правки: только упоминания в контексте trigger-фраз и веток git (`main branch`), ни одного «main дёргает/диспатчит/читает».

- [ ] **Step 4: Проверить, что все манифесты парсятся**

Run: `cd cli && go build ./... && go test ./internal/manifest/ -v`
Expected: PASS

- [ ] **Step 5: Прогнать apply на временном проекте**

Пути берутся от корня репозитория, а не хардкодятся — иначе проверка легко уедет на чужой чекаут и даст ложный зелёный.

```bash
REPO=$(git rev-parse --show-toplevel)
(cd "$REPO/cli" && make build)
TMP=$(mktemp -d) && cd "$TMP" && git init -q
ZPROF_REPO="$REPO/profiles" "$REPO/cli/bin/zprof" apply ios-swift
grep -A 12 "block=stop-list" CLAUDE.md
ls .claude/agents/task-runner.md
ls .claude/agents/ | grep orchestrator && echo "ОШИБКА: оркестратор остался" || echo "оркестраторов нет — верно"
```

Expected: блок `stop-list` содержит и базовые строки, и iOS-специфичные; оркестраторов в `.claude/agents/` нет; `task-runner.md` есть.

- [ ] **Step 6: Commit**

```bash
cd /Volumes/mydata/projects/zprof
git add profiles/base/manifest.yaml profiles/overlays/
git commit -m "feat(overlays): стоп-листы во всех девяти overlay'ях + чистка loop.md"
```

---

## Task 7: Проверки `zprof doctor`

**Files:**
- Modify: `cli/internal/doctor/diagnostics.go`
- Modify: `cli/internal/doctor/diagnostics_test.go`

**Interfaces:**
- Consumes: `manifest.ProjectManifest.Overlays`, `OverlayManifest.StopList` (Task 2), `agents.Retired` (Task 1)
- Produces: `checkTaskRunner(projectDir string) []Issue`, `checkStopLists(overlays []string, repoDir string) []Issue`, `checkRunLogs(projectDir string) []Issue`

Добавь `"github.com/vaporphd/zprof/internal/agents"` в импорты `diagnostics.go`. Список упразднённых имён живёт там — своей копии в `doctor` быть не должно.

- [ ] **Step 1: Написать падающие тесты**

Добавь в конец `cli/internal/doctor/diagnostics_test.go`:

```go
func TestCheckTaskRunnerMissing(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude", "agents"), 0o755))

	issues := checkTaskRunner(dir)
	require.Len(t, issues, 1)
	require.Equal(t, LevelError, issues[0].Level)
	require.Contains(t, issues[0].Message, "task-runner")
}

func TestCheckTaskRunnerFlagsSurvivingOrchestrator(t *testing.T) {
	dir := t.TempDir()
	agents := filepath.Join(dir, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agents, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agents, "task-runner.md"), []byte("---\nname: task-runner\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agents, "dev-orchestrator.md"), []byte("---\nname: dev-orchestrator\n---\n"), 0o644))

	issues := checkTaskRunner(dir)
	require.Len(t, issues, 1)
	require.Equal(t, LevelError, issues[0].Level)
	require.Contains(t, issues[0].Message, "dev-orchestrator")
}

func TestCheckStopListsEmptyOverlay(t *testing.T) {
	repo := t.TempDir()
	ovDir := filepath.Join(repo, "overlays", "demo")
	require.NoError(t, os.MkdirAll(ovDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ovDir, "manifest.yaml"),
		[]byte("name: demo\nloop_template: dev-pipeline\n"), 0o644))

	issues := checkStopLists([]string{"demo"}, repo)
	require.Len(t, issues, 1)
	require.Equal(t, LevelError, issues[0].Level)
	require.Contains(t, issues[0].Message, "stop_list")
}

func TestCheckRunLogsWarnsAboveFifty(t *testing.T) {
	dir := t.TempDir()
	runs := filepath.Join(dir, ".zprof", "runs")
	require.NoError(t, os.MkdirAll(runs, 0o755))
	for i := 0; i < 51; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(runs, fmt.Sprintf("r%02d.md", i)), []byte("x"), 0o644))
	}

	issues := checkRunLogs(dir)
	require.Len(t, issues, 1)
	require.Equal(t, LevelWarn, issues[0].Level)
}
```

Убедись, что в импортах теста есть `fmt`, `os`, `path/filepath`, `testing`, `github.com/stretchr/testify/require`.

- [ ] **Step 2: Прогнать тесты — убедиться, что падают**

Run: `cd cli && go test ./internal/doctor/ -run "TestCheckTaskRunner|TestCheckStopLists|TestCheckRunLogs" -v`
Expected: FAIL — `undefined: checkTaskRunner`

- [ ] **Step 3: Реализовать проверки**

Добавь в конец `cli/internal/doctor/diagnostics.go`:

```go
// runLogWarnThreshold is the number of files under .zprof/runs/ past which
// doctor suggests cleaning up. There is no automatic retention in v1.
const runLogWarnThreshold = 50

// checkTaskRunner errors when the task-runner agent is absent, or when a
// retired orchestrator is still present. Both break the isolation
// contract: without the runner main has nothing to delegate to, and with a
// leftover orchestrator it has a second, unsupervised path.
func checkTaskRunner(projectDir string) []Issue {
	agentsDir := filepath.Join(projectDir, ".claude", "agents")
	if info, err := os.Stat(agentsDir); err != nil || !info.IsDir() {
		return nil // no agents applied yet — not this check's business
	}

	var out []Issue
	if _, err := os.Stat(filepath.Join(agentsDir, "task-runner.md")); err != nil {
		out = append(out, Issue{
			Level:   LevelError,
			Path:    agentsDir,
			Message: "task-runner.md отсутствует — main'у некому передавать задачи; запусти `zprof sync`",
		})
	}
	for _, name := range agents.Retired {
		p := filepath.Join(agentsDir, name+".md")
		if _, err := os.Stat(p); err == nil {
			out = append(out, Issue{
				Level:   LevelError,
				Path:    p,
				Message: fmt.Sprintf("%s упразднён, но остался в проекте — main может дёрнуть его в обход task-runner; запусти `zprof sync`", name),
			})
		}
	}
	return out
}

// checkStopLists errors for every active overlay whose manifest declares no
// stop_list. An empty list means the runner has no idea what it must not do
// on its own, and irreversible actions pass unreviewed.
func checkStopLists(overlays []string, repoDir string) []Issue {
	var out []Issue
	for _, name := range overlays {
		p := filepath.Join(repoDir, "overlays", name, "manifest.yaml")
		m, err := manifest.LoadOverlay(p)
		if err != nil {
			continue // checkOverlaysExist already reports a missing overlay
		}
		if len(m.StopList) == 0 {
			out = append(out, Issue{
				Level:   LevelError,
				Path:    p,
				Message: fmt.Sprintf("overlay %q не объявляет stop_list — task-runner не узнает, что нельзя делать самостоятельно", name),
			})
		}
	}
	return out
}

// checkRunLogs warns when run logs pile up. They are gitignored and
// harmless, but a large pile makes the tail-read habit expensive.
func checkRunLogs(projectDir string) []Issue {
	runs := filepath.Join(projectDir, ".zprof", "runs")
	entries, err := os.ReadDir(runs)
	if err != nil {
		return nil
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	if n > runLogWarnThreshold {
		return []Issue{{
			Level:   LevelWarn,
			Path:    runs,
			Message: fmt.Sprintf("%d run-логов (> %d) — стоит почистить", n, runLogWarnThreshold),
		}}
	}
	return nil
}
```

- [ ] **Step 4: Подключить проверки в Diagnose**

В `cli/internal/doctor/diagnostics.go`, в функции `Diagnose`, после `out = append(out, checkManagedMarkers(projectDir)...)`:

```go
	out = append(out, checkTaskRunner(projectDir)...)
	out = append(out, checkStopLists(proj.Overlays, repoDir)...)
	out = append(out, checkRunLogs(projectDir)...)
```

Обнови doc-комментарий `Diagnose`, добавив пункты 7–9 к списку проверок.

- [ ] **Step 5: Прогнать тесты — убедиться, что проходят**

Run: `cd cli && go test ./internal/doctor/ -v`
Expected: PASS

- [ ] **Step 6: Прогнать всё**

Run: `cd cli && go test ./... && go vet ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add cli/internal/doctor/
git commit -m "feat(doctor): проверки task-runner, stop_list и run-логов"
```

---

## Task 8: Шейкдаун и замер эффекта

**Files:**
- Create: `docs/reviews/2026-07-28-task-runner-shakedown.md`

- [ ] **Step 1: Применить новый профиль к тому же проекту, что в Task 0**

```bash
cd <проект-из-Task-0>
zprof sync
zprof doctor
```

Expected: `doctor` без ошибок. Если репортит уцелевший `dev-orchestrator` — Task 1 не сработал, возвращайся туда.

- [ ] **Step 2: Прогнать сопоставимую задачу**

Задача должна быть того же класса, что в Task 0 (багфикс — багфиксом, фича — фичей). Формулируй естественно, без подсказок про раннера: проверяем в том числе, распознает ли main границу.

- [ ] **Step 3: Снять eval**

```bash
zprof eval > /tmp/after-eval.txt
```

- [ ] **Step 4: Проверить критерии приёмки**

Сверь с `docs/superpowers/specs/2026-07-28-task-runner-design.md` §12:

1. Диспатчей верхнего уровня — ровно один.
2. В main вернулись только поля схемы, `Compliance` без нарушений.
3. Журнал `.zprof/runs/<id>.md` существует, содержит `## Итог`, строки ≤120 символов.
4. Суммарные токены в main ниже baseline из Task 0.

- [ ] **Step 5: Проверить сценарий blocked**

Дай задачу, упирающуюся в стоп-лист (для iOS — «залей билд в TestFlight»). Ожидается: `verdict: blocked`, вопрос задан дословно, после ответа — новый раннер с `resume_from`, и `plan-N.md` **не пересоздан**.

- [ ] **Step 6: Записать отчёт**

Создай `docs/reviews/2026-07-28-task-runner-shakedown.md` по образцу существующих отчётов в `docs/reviews/`: таблица baseline против after по каждому критерию, вывод `zprof eval` в fenced-блоках, найденные расхождения и что с ними делать.

- [ ] **Step 7: Commit**

```bash
git add docs/reviews/2026-07-28-task-runner-shakedown.md
git commit -m "docs(reviews): шейкдаун task-runner — замер против baseline"
```

---

## Self-Review

**Покрытие спеки**

| Раздел спеки | Задача |
|---|---|
| §4.1 base: task-runner, удаление оркестраторов | Task 4 |
| §4.1 base: роутер, workflows, доктрина | Task 5 |
| §4.1 base: manifest stop_list | Task 6 шаг 1 |
| §4.2 overlays: stop_list + чистка loop.md | Task 6 |
| §4.3 CLI: tables.go roleAgents | Task 4 шаг 3 |
| §4.3 CLI: парсинг stop_list | Task 2 |
| §4.3 CLI: managed_agents | Task 1 шаг 5 |
| §4.3 CLI: блок stop-list, gitignore | Task 3 |
| §4.3 CLI: doctor | Task 7 |
| §5 контракт раннера | Task 4 шаг 5 |
| §6 стоп-лист | Task 6 |
| §7 blocked и resume | Task 4 шаг 5 (промпт), Task 5 шаг 1 (роутер), Task 8 шаг 5 (проверка) |
| §8 журнал | Task 4 шаг 5 |
| §9 миграция | Task 1 |
| §10 проверки doctor | Task 7 |
| §12 критерии приёмки | Task 0 (baseline), Task 8 (замер) |
| §13 порядок работ | порядок задач 0→8 |

Пробелов нет.

**Согласованность имён**

- `PruneOrphanAgents(agentDir, previous, current)` — определена в Task 1 шаг 3, вызывается в Task 1 шаг 6. Совпадает.
- `retiredAgents` объявляется дважды: `apply` (Task 1) и `doctor` (Task 7). Дублирование намеренное — пакеты не зависят друг от друга, и комментарий в `doctor` прямо указывает на зеркало. Если при реализации появится общий пакет — вынести туда.
- `buildStopListBlock(opts ApplyOpts) string` — Task 3 шаг 3, вызов в Task 3 шаг 5. Совпадает.
- `checkTaskRunner` / `checkStopLists` / `checkRunLogs` — Task 7 шаг 3, подключение в шаг 4. Совпадает.
- Ключ managed-блока `stop-list` — Task 3 шаг 5, проверяется в Task 6 шаг 5 через `grep -A 12 "block=stop-list"`. Совпадает.
- Поле `ManagedAgents` — Task 1 шаг 5, используется в Task 1 шаг 6. Совпадает.

**Побочные потребители удаляемых имён — проверено grep'ом по репозиторию**

| Место | Задача |
|---|---|
| `apply/tables.go:23-24` (`roleAgents`) | Task 4 шаг 3 |
| `apply/tables.go:150` (fallback `buildExecutingTable`) | Task 4 шаг 3 |
| `apply/e2e_test.go:62-63` (ассерты на файлы) | Task 4 шаг 3.5 |
| `eval/scoring.go:17` (`roleGuessRe`) | Task 4 шаг 3.5 |
| `agents/retired.go` (`Retired`) | намеренно — это и есть список упразднённых, единственный |
| `eval/parser.go:165`, `eval/scoring.go:12` (комментарии) | косметика, правка не требуется |

**Плейсхолдеры**

Не найдены: каждый шаг с кодом содержит полный текст, каждый шаг с проверкой — точную команду и ожидаемый результат.
