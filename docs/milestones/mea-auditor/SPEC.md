# SPEC — mea-auditor: техническая спецификация

**PRD:** `PRD.md` рядом. Все номера FR — оттуда.
**Кодовая база:** промпт-слой `profiles/base/`, Go CLI `cli/`, телеметрия по `docs/superpowers/specs/2026-08-02-agent-telemetry-design.md`.

## 1. Карта изменений

| Компонент | Статус | Что меняется |
|---|---|---|
| `profiles/base/agents/auditor.md` | **новый** | read-only аудитор, model: sonnet (FR-2) |
| `profiles/base/agents/auditor-deep.md` | **новый** | тот же промпт, model: opus (FR-6) |
| `profiles/base/agents/task-runner.md` | правка | петля claim→audit→state, контракты шагов, секция Requirements, бюджет, integrity-снимки (FR-1,3,4,5,8) |
| `.zprof.yaml` схема + `cli/` config | правка | блок `audit:` (FR-9) |
| telemetry hook (`ext` в dispatches.jsonl) | правка | `audited`, `audit_verdict` (FR-7) |
| telemetry stats/отчёт | правка | агрегат false-done rate (FR-7) |
| `docs/superpowers/specs/2026-07-28-task-runner-design.md` | правка | §8 журнал — добавить Requirements; отметить v2 |

Роли исполнителей и overlays не меняются — контракт шага приходит им в тексте диспатча, схема возврата прежняя.

## 2. Роль auditor (FR-2)

Frontmatter (по образцу `profiles/base/agents/gates/evidence-auditor.md`):

```yaml
---
name: auditor
description: Read-only аудит одного шага task-runner. Проверяет СРЕДУ против acceptance criteria контракта, не доверяя отчёту исполнителя. Диспатчится только task-runner'ом.
tools: Read, Grep, Glob, Bash
model: sonnet        # auditor-deep.md — model: opus
return_format: |
  # CRITICAL: ответ начинается с `completion:` — без преамбулы и код-фенса.
  completion: complete|incomplete|blocked
  integrity: clean|violation
  evidence: .zprof/runs/<run-id>-audit-<n>.md
  requirements: <id>=<completed|blocked|untrusted>[, ...]
  one_line: <≤120 символов>
---
```

Ключевые правила промпта:

1. **Вход:** контракт шага (цель, acceptance criteria, границы), отчёт исполнителя (только как указатель, где искать), релевантные строки Requirements. **Не** траектория исполнителя
2. **Проверяй среду, не отчёт:** каждое acceptance criterion закрывается свидетельством, полученным собственной проверкой (чтение файлов, grep, непроверяющие команды: `git log/diff/show`, прогон тестов разрешён — см. §6 edge cases)
3. **Evidence-файл обязателен:** по каждому критерию — проверка, команда/файл, результат. Финальное сообщение — только схема
4. **Никаких мутаций:** не создавать/менять/удалять файлы задачи (evidence-файл — единственная разрешённая запись); нарушение поймает integrity-снимок раннера
5. `blocked` — когда проверка невозможна (нет доступа/окружения), с объяснением в evidence

## 3. Петля task-runner (FR-1, FR-3, FR-5)

Изменение §«цикл» в `task-runner.md`; активна только при `audit.enabled: true` (иначе — текущее поведение, буквально).

```
для каждого мутирующего шага маршрута:
  1. составить контракт: goal, acceptance_criteria (проверяемые!),
     boundaries (стоп-лист), refs (id требований + прошлые evidence)
  2. dispatch исполнителя с контрактом      → verdict
     verdict != done → как сейчас (retry/route)
     verdict == done → requirement(ы) шага := claimed
  3. integrity-снимок A (см. §5)
  4. dispatch auditor|auditor-deep (таблица §4) с контрактом + отчётом
  5. integrity-снимок B; A != B → integrity=violation независимо от ответа
  6. completion=complete && integrity=clean → requirements := completed (+evidence)
     completion=incomplete → requirements остаются claimed; шаг ретраится
       с выдержкой из evidence аудита («что не так»)
     integrity=violation → requirements := untrusted; шаг ретраится
     completion=blocked → verdict: blocked + question (эскалация)
  7. счётчик dispatches++ (исполнитель И аудитор считаются);
     счётчик > max_dispatches → verdict: blocked + question
       «бюджет исчерпан, закрыто N из M требований, продолжить?»
```

Не аудируются: read-only шаги (explorer, planner — их выход идёт через существующий gate `plan-reviewer`), диспатчи самих аудиторов.

## 4. Таблица моделей аудита (FR-6)

Дефолт (в промпте task-runner, override через `.zprof.yaml audit.model_by_role`):

| Роль исполнителя шага | Аудитор |
|---|---|
| tester*, билд/линт tool-агенты, docs-writer | `auditor` (sonnet) |
| implementer*, refactor-agent*, bug-hunter*, architect* | `auditor-deep` (opus) |

`*` — включая stack-суффиксы (`implementer-ios` → по базовому имени). Два файла роли вместо динамического model-параметра — решение в пользу детерминизма (см. §8 отвергнутые).

## 5. Integrity-снимки (FR-8)

Снимок = `git status --porcelain=v1` + `git diff HEAD --stat`, захэшированные. Делает **task-runner** (не аудитор — иначе он аудирует сам себя) до и после диспатча аудитора; расхождение снимков = мутация во время аудита.

Требуется расширить Bash-whitelist task-runner'а: сейчас в промпте «только `date` и `git log -1`» → добавить `git status --porcelain`, `git diff HEAD --stat`, `shasum`. Untracked-файл evidence аудитора — ожидаемое расхождение: снимок фильтрует путь `.zprof/runs/*` (единственная разрешённая запись аудитора).

Ограничение (честно): ловит только git-видимые мутации; изменения вне working tree (глобальные конфиги, ~/) не детектируются. Для zprof-профилей это приемлемо — стоп-листы и так запрещают выход за репозиторий.

## 6. Edge cases

- **Аудитор не вернул схему / упал** → 1 повторный диспатч аудитора; снова мимо → шаг считается неаудированным, requirement остаётся claimed, эскалация blocked (не молчаливый пропуск)
- **Тесты как проверка аудитора:** прогон тестов меняет build-артефакты → integrity-снимок по git обычно чист (`build/` в .gitignore), но если тест-раннер пишет в tracked-файлы (снапшот-тесты!) — это violation. Правило в промпте аудитора: снапшот-обновляющие режимы запрещены (`--ci`/read-only флаги)
- **Resume (`resume_from`):** новый раннер читает Requirements; `claimed`-записи при resume деградируют в `pending` (недоаудированное недоказано), `completed` не перепроверяются
- **Два окна Claude Code:** телеметрия уже лочится (telemetry design С6); run-лог пишет только один раннер — без изменений
- **Роль вне таблицы §4** (кастомный агент пользователя) → auditor-deep (дороже, но безопаснее)

## 7. Конфиг и телеметрия (FR-7, FR-9)

```yaml
# .zprof.yaml
audit:
  enabled: true          # дефолт false
  max_dispatches: 7      # deprecated-алиас (issue #19); реальный источник лимита — runner.max_dispatches, дефолт 14
  model_by_role:         # опционально, override §4
    docs-writer: opus
```

**Deprecated (issue #19):** `max_dispatches` внутри `audit:` — устаревший алиас старого дефолта (7). Глобальный лимит диспатчей теперь задаётся `runner.max_dispatches` (дефолт 14), см. `profiles/base/agents/task-runner.md` §«Бюджет».

- Go CLI: парсинг блока в конфиг-структуру; `zprof apply` кладёт auditor/auditor-deep в `.claude/agents/` и вносит в `managed_agents` (миграция бесплатно, task-runner design §9); `zprof doctor` предупреждает, если `audit.enabled` при отсутствующем auditor.md
- dispatches.jsonl `ext`: `audited: true|false|skipped`, `audit_verdict: "complete/clean"|...`, `dispatch_budget_used: n/7`. `skipped` — аудит выключен; `false` — шаг мутирующий, аудита не было при включённом (это баг-сигнал)
- stats: false-done rate per role/overlay; токен-доля аудиторов (сверка с 19–38% из статьи)
- **OPEN:** точное имя поля `ext` и место врезки в hook — свериться с реализацией telemetry stage 1 при исполнении (S3 в PLAN)

## 8. Ключевые решения и отвергнутые альтернативы

| Решение | Отвергнуто | Почему |
|---|---|---|
| Блокирующий аудит | telemetry-only сначала (comparison-док §3.3) | решение Алекса; неблокирующий аудит — театр, ценность статьи именно в «claim ≠ факт» |
| Новая роль в base | расширять evidence-auditor | у него другая специализация (числовые утверждения) и hard-gate семантика; смешение размоет оба |
| Два файла роли (sonnet/opus) | динамический model-параметр Task | недетерминированно, зависит от версии Claude Code; два файла — прозрачно и совместимо с model-eval инфраструктурой |
| Requirements в run-логе | отдельный state.yaml | решение Алекса; один файл на задачу, парсится телеметрией, человекочитаем |
| Критерии от task-runner при диспатче | из плана planner'а | обходные маршруты (bugfix без плана) остаются покрытыми |
| Снимки делает раннер | аудитор сам отчитывается о read-only | самоотчёт о честности — та же self-grading проблема на уровень глубже |

## 9. Решения по OPEN-вопросам

### OPEN (PLAN §Открытые вопросы): декомпозиция задачи в requirements

**Решение:** 1–3 requirements на типовую задачу. task-runner формулирует их
при старте run на основе классификации задачи и маршрута. Примеры:

- Багфикс (3 шага): R1=баг воспроизведён, R2=патч не ломает тесты, R3=PR создан
- Фича (5 шагов): R1=план утверждён, R2=код реализован, R3=тесты зелёные
- Только тесты (1 шаг): R1=тесты написаны и зелёные

Более 3 — только при явно многоэтапной задаче (миграция, мульти-таргет).
Уточнять по shakedown (Stage 4).

### OPEN (SPEC §7): ext-поля телеметрии

**Решение (предварительное, верифицировать в Stage 3):** поля кладутся
в существующий `ext` объект dispatches.jsonl. Точные имена:
`ext.audited` (bool|string), `ext.audit_verdict` (string),
`ext.dispatch_budget_used` (string "n/max"). Врезка — в `_normalize_dispatch()`
зprof-collect.py через raw dict, аналогично `project_id_provisional`.

## 10. Безопасность / совместимость

- `audit.enabled: false` (дефолт): ни одного нового диспатча, run-лог без секции Requirements не ломает парсеры (секция опциональна для читателей)
- Evidence-файлы в `.zprof/runs/` — уже в `.gitignore`
- Промпты — на русском, имена ролей/ключи — английские (конвенция репо)
