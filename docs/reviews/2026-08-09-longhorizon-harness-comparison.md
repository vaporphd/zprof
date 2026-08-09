# LongHorizon-Harness vs zprof — сравнительный разбор

**Дата:** 2026-08-09
**Источник:** [arXiv:2608.01964](https://arxiv.org/abs/2608.01964) — "LongHorizon-Harness: Advancing Long-Horizon Agents for Real-World Tasks"
**Авторы:** Ziyu Ma, Hailang Huang, Shun Zou, Yong Wang, Shidong Yang, Yiming Hu, Fei Wei, XiangXiang Chu
**PDF:** `docs/reviews/2608.01964-longhorizon-harness.pdf`

---

## 1. Что это

Фреймворк для long-horizon задач (GUI и CLI), разделяющий петлю агента на
три роли — Manager, Executor, Auditor (MEA). Главная идея: **состояние задачи
живёт вне контекстного окна любого из участников**, и каждый Executor
получает свежий контекст на свой подэтап. Аудитор верифицирует результат
read-only, не доверяя самоотчёту Executor'а.

Результаты: Qwen 3.7-Plus на WeaveBench 51.8% → 80.7%, на OSWorld 2.0
2.8% → 8.3%, на Terminal-Bench 2.1 69.7% → 77.2%.

---

## 2. Сравнение архитектур

### 2.1 Петля управления

| Аспект | LongHorizon-Harness (MEA) | zprof (task-runner) |
|--------|---------------------------|---------------------|
| Контроллер | Manager — отдельная LLM-роль, ~2–8% токенов | task-runner — subagent в Claude Code, sonnet |
| Исполнитель | Executor — свежий контекст, бюджет 1800с | Роли (implementer, tester, bug-hunter…) через Task/Agent |
| Верификатор | Auditor — read-only, 19–38% токенов | Нет выделенного. Есть gates (north-star-auditor, evidence-auditor), но опциональные, не в петле |
| Формула | (S_{i+1}, q, c) = Φ_mgr(𝒯, S, V) | verdict: done\|blocked\|failed + artifact + run_log |

**Что совпадает:**
- Контроллер не трогает среду, только роутит
- Один спавн = одна задача, свежий контекст
- Стоп-лист / boundary constraints
- Явный протокол `blocked` → вопрос пользователю → resume

**Где MEA дальше:**
- Auditor как обязательная фаза каждого цикла (у нас gates опциональны)
- Формальная integrity-проверка (clean/suspect/violation)
- Executor claims не меняют persistent state — только audit report может

**Где zprof дальше:**
- Телеметрия и observability как первый класс (dispatches.jsonl, Class A checks, drift analysis, economics per role)
- A/B tier experiments с детерминистическим arm selection
- Stack-aware overlays — доменные роли и tool-агенты
- Оркестрация через Claude Code native (Task/Agent), не кастомная обёртка

### 2.2 Состояние задачи

| Аспект | MEA | zprof |
|--------|-----|-------|
| Формат | Structured records: Requirement, Artifact, Fact — с completion status | Run-лог .zprof/runs/<id>.md — таблица шагов |
| Кто обновляет | Только Manager после аудита | task-runner пишет построчно |
| Персистентность | В памяти Manager'а между раундами | На диске, git-ignorable |
| Cross-round | S_i + selective V_i | Артефакты в репозитории (план, ADR, коммиты) |

**Ключевое различие:** MEA хранит формальное состояние задачи (requirements
с completion status). zprof'овский task-runner хранит журнал и полагается
на то, что состояние — это сам репозиторий (§7 task-runner spec: "раннер
восстанавливает состояние из артефактов").

### 2.3 Контекст и изоляция

| Аспект | MEA | zprof |
|--------|-----|-------|
| Executor context | Свежий каждый раунд, raw trajectory отбрасывается | Четыре уровня изоляции (T1–T4), subagents не загрязняют main |
| Manager context | Растёт (S + V), но компактный | task-runner тоже растёт, но свежий для каждой задачи |
| Ceiling | N_max = 25 раундов | Нет формального лимита (implicit через token budget) |

### 2.4 Верификация

| Аспект | MEA Auditor | zprof |
|--------|-------------|-------|
| Когда | После каждого Executor | Class A checks при сборе телеметрии (post-hoc) |
| Что проверяет | Environment vs acceptance criteria | has_preamble, return_parsed, artifact_exists, next_is_reachable |
| Read-only enforcement | Мониторинг workspace, mutation = violation | Нет runtime enforcement |
| Вес | 19–38% всех токенов | 0% runtime, post-hoc из JSONL |

**Это главный gap.** MEA тратит значительные ресурсы на верификацию каждого
шага, и это окупается результатами. zprof проверяет контракт (формат ответа),
но не проверяет среду (действительно ли задача выполнена).

### 2.5 AgentAdapter vs zprof overlays

MEA решает другую проблему: один и тот же MEA-цикл поверх разных бэкендов
(Claude Code, Codex, OpenClaw, Hermes). zprof решает: один и тот же agent-loop
поверх разных стеков (iOS, Python, Kotlin, C++). Это ортогональные оси.

### 2.6 Телеметрия

| Аспект | MEA | zprof |
|--------|-----|-------|
| Token breakdown per role | Упоминается в eval (Figure 5) | Первый класс: dispatches.jsonl, stats, HTML dashboard |
| Daily dispatch timeline | Нет | Есть |
| Config drift analysis | Нет | Есть (before/after config_hash) |
| A/B model experiments | Нет | Есть (pick-arm) |
| Cost per task | Нет (per benchmark, не per task) | Заявлено как следующий report |
| Route analysis | Нет | Transitions, tester loops, status distribution |

---

## 3. Что взять из MEA

### 3.1 Обязательный аудит каждого шага (high value, high cost)

MEA тратит 19–38% токенов на Auditor, но это даёт +29pp на WeaveBench.
task-runner может после каждого subagent dispatch прогонять read-only
верификатор, проверяющий environment state против acceptance criteria.

**Цена вопроса:** при 5 шагах в task, 300с на аудит каждого — это 1500с
overhead, но зато не нужны повторные прогоны.

**Вариант для v2:** auditor как опциональный gate в петле task-runner'а,
включаемый через `.zprof.yaml`. Это уже подготовлено — gates (north-star-auditor,
evidence-auditor) существуют, нужно только встроить в цикл.

### 3.2 Structured task state (medium value, low cost)

Сейчас task-runner ведёт markdown-журнал. MEA формализует state как записи
с completion status. Это ортогонально журналу — можно вести и то, и другое.

**Практический шаг:** Добавить в run-лог секцию requirements с
`[x]`/`[ ]`/`[blocked]` маркерами, которую task-runner обновляет
после каждого шага. Это уже полу-формальное состояние, и его можно
парсить для телеметрии.

### 3.3 Executor claims ≠ verified state (high value, zero cost)

Принцип: self-report Executor'а не меняет persistent state. Только
аудированный результат может закрыть requirement.

**Практический шаг:** Добавить в Class A checks новый check: `audited`.
Dispatch с `verdict: done` без подтверждения аудитора помечается
`audited: false`. Это telemetry-only (не блокирует), но даёт метрику.

### 3.4 Integrity monitoring (medium value, medium cost)

Auditor мониторит workspace на мутации. Если аудитор read-only, но
обнаруживает изменения — это violation.

**Для zprof не нужно**: Claude Code sandbox и tool permissions уже
ограничивают инструменты per-agent через frontmatter. Но git-level
verification (git diff before/after audit) было бы полезно.

---

## 4. Что у нас лучше

### 4.1 Телеметрия и observability

MEA показывает token split в eval-секции paper'а. zprof — production-ready
система сбора, агрегации и визуализации. dispatches.jsonl, Class A checks,
drift analysis, daily timeline, economics per role, evaluator-telemetry agent.
Это не сравнимо.

### 4.2 A/B experiments

MEA не имеет инфраструктуры для экспериментов с моделями. zprof имеет
pick-arm с детерминистическим hash-based arm selection.

### 4.3 Stack awareness

MEA model-agnostic и harness-agnostic, но stack-agnostic тоже — нет
доменных знаний. zprof'овские overlays дают domain-specific роли,
tool-агенты, стоп-листы.

### 4.4 Native integration

MEA требует кастомную обёртку (AgentAdapter). zprof работает через
нативные Claude Code механизмы (Task, Agent, hooks). Нет отдельного
процесса, нет кастомного runtime.

### 4.5 Простота

MEA — 3 LLM-вызова на шаг (manager + executor + auditor).
zprof task-runner — 1 LLM-вызов на шаг (subagent), контроллер — тот же
subagent. Gates — опциональные. Базовый overhead ниже.

---

## 5. Рекомендации

| Приоритет | Что | Откуда | Статус |
|-----------|-----|--------|--------|
| P1 | Auditor в петле task-runner (opt-in) | §3.1 | **done** — `mea-auditor` milestone, S1+S3 |
| P2 | Structured requirements в run-логе | §3.2 | **done** — секция Requirements в task-runner.md |
| P2 | `audited` flag в Class A checks | §3.3 | **done** — ext.audited в dispatches.jsonl, false-done rate в stats |
| P3 | git diff verification в audit phase | §3.4 | **done** — integrity-снимки в петле task-runner |

---

## 6. Цитаты из paper'а

> "Agent capability is a property of the complete model–harness system"
> — подтверждает подход zprof: профиль (harness config) влияет на
> результат наравне с моделью.

> "Executor claims do not directly change the persistent state"
> — принцип, который стоит заложить в task-runner v2.

> "The effectiveness of LongHorizon-Harness is determined less by
> whether a task uses a GUI or CLI than by whether its primary
> bottleneck lies in long-horizon execution reliability or in solving
> an individual step."
> — это объясняет, где MEA помогает, а где нет. Для zprof: auditor
> ценен именно для длинных dev-pipeline (5+ шагов), не для quick bugfix.
