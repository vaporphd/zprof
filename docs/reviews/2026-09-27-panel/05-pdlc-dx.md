# 05 — PDLC и DX (эксперт панели AI4PDLC)

Объект: zprof @ `ed75196`; кейсы — `apple-health-sync` (ios-swift), `jarvis-in-hermes` (systems-rust). Всё прочитано read-only.

## Вердикт

- **Delivery-половина PDLC зрелая, discovery и feedback почти отсутствуют.** Путь issue → PR → merge закрыт контрактами (`profiles/base/agents/pr-shepherd.md:45-46`), а шагов «почему эта фича» и «что показало использование» в base нет: в `profiles/base/agents/*.md` нет ни одной product/UX-роли.
- **State-файлы не держат собственных инвариантов.** `followup.md` ≤20 строк (`profiles/base/claude-block-base.md:7`), а в jarvis 83 строки / 11 KB (`wc -l followup.md`). `todo.md` отмечает #6 и #7 как `[x]` (`jarvis-in-hermes/todo.md:18,20`), хотя в GitHub они OPEN (`gh issue list --state open`).
- **`zprof doctor` проверяет механику установки, но не смысл.** Placeholder `<SchemeName>` (`apple-health-sync/CLAUDE.md:39-40`) и раздутый followup проходят без замечаний: doctor вернул только `[info]` про `.agentlog/`.
- **Граница «мутация — раннеру» держится на тексте промпта, а не на механизме.** Фаза 1 scorecard в самом zprof сделана через superpowers-планы (`docs/superpowers/plans/2026-09-26-task-scorecard-phase1.md:3`), а `.zprof/runs/` пуст (`ls .zprof/runs`).
- **Для 1–2 проектов система работает, на 10 проектов не масштабируется** (мнение). Знание размазано по 6–8 файлам на проект, агрегатора между проектами нет, lessons ведутся в двух несовместимых форматах.

## PDLC-карта

| Этап продукта | Артефакт | Кто ведёт | Зрелость (0–3) | Пробел |
|---|---|---|---|---|
| Idea / discovery | — (чат, `thoughts/`) | человек | 0 | Нет роли и шаблона. Гипотеза нигде не фиксируется |
| Direction | `NORTH_STAR.md` | человек + `north-star-auditor` (gate) | 1 | Gate опционален (`with_gates: false` в `jarvis-in-hermes/.zprof.yaml:4`). Агент читает `docs/NORTH_STAR.md` (`gates/north-star-auditor.md:17`), а файл лежит в корне jarvis |
| PRD | `PRD.md` | skill `milestone-spec` (вне zprof) | 1 | В base нет шаблона PRD. Связь с zprof только ручная |
| SPEC | `SPEC.md` + `docs/PROJECT_SPEC.md` | skill / architect | 2 | Два источника; «при расхождении прав `SPEC.md`» (`apple-health-sync/CLAUDE.md:117`) |
| Plan / issues | `plan-N.md`, GitHub issues, `todo.md` | `groomer`, `planner` | 2 | Три копии backlog. `planner` пишет в `tasks/plan-<N>.md` (`planner.md:19`), groomer кладёт `plan-N.md` в корень (кейсы) |
| Приоритизация | порядок в `todo.md`, `depends_on` | человек | 1 | Нет value/cost, нет appetite. `estimated_complexity` только S/M/L (`groomer.md:79`) |
| Delivery | PR, `.zprof/runs/*.md` | `task-runner` → роли → `pr-shepherd` | 3 | — |
| Docs back-sync | `docs/wiki/`, ADR | `wiki-keeper`, `architect` | 2 | Синхронизацию PROJECT_SPEC требует `spec-maintainer` (`pr-shepherd.md:133`), но такого агента в base нет |
| Acceptance | `docs/acceptance.md`, ручные issues | человек | 1 | Ручная приёмка висит открытыми issues (#46, #47 в jarvis) |
| Feedback / метрики | `.agentlog/`, `zprof score` | коллектор | 1 (процесс) / 0 (продукт) | Метрики только процесса агентов. Продуктовых метрик и обратной связи от пользователя нет |
| Kill / sunset | — | — | 0 | Нет механизма отказа от фичи и статуса `dropped` |

## Сильные стороны

- **Чёткий handoff планирования в исполнение.** Groomer пишет AC в проверяемой форме («cargo test зелёные…», `groomer.md:125-126`), task-runner берёт issue по одному (`AGENT_LOOP.md:18-19`). В apple-health-sync за два дня закрыт этап 1 целиком: issues #1–#8, 273/273 тестов (`apple-health-sync/followup.md:16`).
- **Трассируемость issue ↔ PR ↔ todo обязательна.** `Closes #N` и тик в `todo.md` являются preflight-условием (`pr-shepherd.md:45-46`).
- **Wiki как контекст для планирования.** Groomer сначала читает `docs/wiki/INDEX.md` (`groomer.md:52`). В jarvis wiki реально живёт: 14 файлов, `INDEX.md` 29 KB (`ls jarvis-in-hermes/docs/wiki`).
- **ADR действительно пишутся.** В apple-health-sync ADR-0001…0006 появились за два дня (`ls docs/adr`), architect стоит в цепочке фичи (`task-runner.md:100`).
- **Managed-блоки честно отделяют doctrine от своих правил.** `<!-- zprof:begin … -->` (`apple-health-sync/CLAUDE.md:1,29`) плюс «Пиши ниже managed-блока» (`:26`). Пользователь сам переопределил стек ниже блока (`:123`).
- **NORTH_STAR как продуктовый guardrail.** Сама идея сильная: «SUPPORT никогда не цель» (`jarvis-in-hermes/NORTH_STAR.md:9-11`). Это зачаток product-гейта.

## Риски и слабости

1. **Backlog живёт в трёх местах и уже разошёлся.** В `todo.md` #6/#7 закрыты (`jarvis-in-hermes/todo.md:18,20`), в GitHub открыты (`gh issue list`). Следствие: groomer и planner читают `todo.md` (`groomer.md:55`, `planner.md:18`) и планируют по устаревшей картине, а человек не знает, чему верить.
2. **Followup стал журналом.** Норма ≤20 строк (`claude-block-base.md:7`), шаблон задаёт Status ≤10 строк (`state-templates/followup.md:4`). Реально: jarvis 83 строки, в apple-health-sync одна строка статуса занимает около 700 символов (`apple-health-sync/followup.md:14`). В самом zprof followup устарел: «PR #10 → main… merge делает Алекс» (`followup.md:4,9`), хотя `ed75196` уже в main. Следствие: каждая сессия платит токенами за историю, а snapshot перестаёт быть snapshot'ом.
3. **Обратная связь не доходит до продуктовых артефактов.** PROJECT_SPEC по договорённости синхронизирует несуществующий `spec-maintainer` (`pr-shepherd.md:34,133`). Wiki-keeper'у запрещено трогать PROJECT_SPEC и ADR (`wiki-keeper.md:34`). Следствие: SPEC и PRD замерзают на моменте груминга. В jarvis решения всё же вписываются в SPEC/PRD, но руками внутри задачи (`jarvis-in-hermes/followup.md:13-15`).
4. **Doctor не ловит «документация врёт».** Среди 17 проверок (`cli/internal/doctor/diagnostics.go:93-108`) нет проверок placeholder'ов, размера state-файлов и расхождения todo/issues. `grep -n "placeholder\|followup" diagnostics.go` ничего не находит. Следствие: `<SchemeName>` и `<App>.entitlements` (`apple-health-sync/CLAUDE.md:39-44`) остаются в managed-блоке, агенты читают неверную команду сборки, а пользователь закрывает дыру ручным override.
5. **Граница system vs skills никак не enforced.** Зависимость от superpowers прошита прямо в артефакте zprof (`docs/superpowers/plans/2026-09-26-task-scorecard-phase1.md:3`, «REQUIRED SUB-SKILL: superpowers:subagent-driven-development»). При этом `AGENT_LOOP.md:50-51` запрещает main диспатчить роли напрямую. Следствие: у самого сильного use-case (разработка zprof) нет run-логов и scorecard. Это прямо признано в backlog: «zprof сам не ест свой корм» (`todo.md:12`).
6. **Когнитивная нагрузка на старте.** В apple-health-sync 24 managed-агента (`.zprof.yaml:7-30`), 5 managed-блоков в CLAUDE.md, плюс hooks в `.claude/settings.local.json`, `.agentlog/`, `.zprof/runs/`, `*.zprof.bak-*` (`jarvis-in-hermes/.gitignore:3`). README обещает «6 workflow roles» (`README.md:35`), а таблица Consilium перечисляет 16 ролей (`CLAUDE.md`, раздел Consilium). Следствие: новый пользователь не понимает, что трогать, а README расходится с поставкой.
7. **Lessons не работают как система знаний.** Два формата: `L-00N` и датированный (`tasks/lessons.md:8,121`). В jarvis 4 длинные записи на 7.8 KB (`grep -n "^### " lessons.md`), так что правило «~15 записей» ничего не ограничивает. Кросс-проектного переноса нет: урок про MERGE_GATE из jarvis (`jarvis-in-hermes/lessons.md:35`) в base не попал (мнение, по отсутствию в `profiles/base`).
8. **Gate-конфигурация расходится с текстом.** NORTH_STAR jarvis утверждает, что task-runner вызывает gate «первым шагом любой цепочки» (`NORTH_STAR.md:5`), но gates выключены (`.zprof.yaml:4`), а doctor помечает `north-star-auditor` как orphan (`zprof doctor` в jarvis).

## Сравнение с практикой

- **Kiro / spec-driven (requirements → design → tasks).** Там спецификация живая, и задачи регенерируются из неё. У zprof трассировка FR→issue есть только в milestone-spec (`~/.claude/skills/milestone-spec/SKILL.md:43`). Стоит взять обязательный `FR-N` в теле issue и проверку в reviewer.
- **GitHub Projects / Linear как единственный источник правды.** Практика такая: один backlog, а локальные файлы строятся из него. Стоит взять: `todo.md` генерирует `zprof todo sync` из `gh issue list`, руками его не редактируют.
- **PRD-as-code.** PRD версионируется рядом с кодом и меняется через PR с diff. В zprof для этого нужен `docs/PRD.md` в state-templates и правило «изменение scope = PR в PRD».
- **Shape Up.** Appetite вместо оценки, betting table, cool-down, явный отказ от питча. У zprof есть S/M/L, но нет appetite и circuit breaker. Стоит добавить поле `appetite` в plan-N и статус `dropped`.
- **Dual-track agile.** Discovery-трек идёт параллельно delivery. В zprof discovery делают skills вне системы. Стоит оформить его как отдельный вход groomer'а: `discovery-note.md` → groomer.
- **«AI PM»-инструменты** (синтез фидбэка, авто-приоритизация). Главная ценность в них — связь фидбэка с backlog. У zprof есть `.agentlog/` для процесса; аналогичный канал для продукта (заметки об использовании → issues с label `feedback`) стоит дёшево.
- **ADR (Nygard).** zprof следует практике хорошо; не хватает статуса `superseded` и ссылки ADR→FR.

## Рекомендации

**Сейчас**
- Добавить в doctor проверки: `<…>`-placeholder'ы в managed-блоках, followup >20 строк, todo `[x]` при OPEN issue. Эффект: три из найденных дефектов ловятся автоматически.
- Убрать ссылки на `spec-maintainer` из pr-shepherd или создать эту роль. Эффект: у контракта не будет висящей зависимости.
- Сделать `followup.md` rolling-файлом: история уходит в `followup-log.md` через pr-shepherd stamp. Эффект: snapshot снова помещается в ≤20 строк.
- Исправить README («6 roles») и путь NORTH_STAR (`docs/` или корень, одно из двух). Эффект: документация перестаёт противоречить поставке.

**Квартал**
- `zprof todo sync`: GitHub issues как SoT, `todo.md` генерируется. Эффект: backlog больше не расходится с issues.
- Шаблон `PRD.md` и `NORTH_STAR.md` в state-templates, groomer требует `FR-N`. Эффект: трассировка PRD→issue→PR.
- Роль `product-steward` (sonnet): после milestone сверяет PRD/SPEC с merged-изменениями и ведёт kill-list. Эффект: цикл feedback→spec замыкается.
- Явный контракт «skills vs zprof». Например, milestone-spec становится официальным входом groomer'а, а SDD-путь пишет run-лог. Эффект: любой путь оставляет телеметрию.

**Позже**
- Кросс-проектный `zprof lessons promote` в base. Эффект: уроки масштабируются на 10 проектов.
- Портфельный `zprof status --all`: followup/Next по всем проектам. Эффект: выбор «что дальше» делается на уровне портфеля.
- Продуктовые метрики в scorecard (FR покрыты, acceptance закрыт). Эффект: оценивается результат, а не только процесс.

## Вопросы владельцу

1. Что считается источником правды для backlog: GitHub issues или `todo.md`? Сейчас по факту ни то, ни другое.
2. Отсутствие product-ролей в base — это осознанное «продукт делает Алекс» или пробел?
3. Разработку zprof через superpowers SDD нужно считать нарушением doctrine или легитимным вторым режимом?
4. Нужен ли followup как журнал, или история должна жить в git log / run-логах?
5. Какой объём портфеля целевой (2 или 10+ проектов)? От этого зависит, нужен ли кросс-проектный слой.
