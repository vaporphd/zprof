# Panel 01 — SDLC-процесс (AI-native SDLC)

Объект: zprof @ ed75196. Линза: архитектура процесса разработки с агентами. Эксперт читал только те файлы, на которые ниже есть ссылки.

## Вердикт

- **Середина SDLC (issue → код → тест → ревью → merge) проработана сильнее, чем обычно бывает у агентных harness'ов. Края отсутствуют**: релиза, эксплуатации, инцидентов и обратной связи от пользователей в loop нет. Stop list прямо относит «релиз, деплой» к человеку (`CLAUDE.md:75`), но процесса для человека нигде не описано.
- **Auto-merge от pr-shepherd убрал последнюю human-точку между намерением и `main`**. При `local-green` (нет branch protection) merge стоит только на LLM-reviewer'е и attestation, которую pr-shepherd сам не перепроверяет (`pr-shepherd.md:98`, `:166`). По умолчанию MEA-аудит выключен (`project.go:48`). Контроль держится на одном LLM-вердикте.
- **Контракт противоречит сам себе**: `--delete-branch` в merge-команде (`pr-shepherd.md:30`) нарушает stop list «удаление веток и тегов» (`CLAUDE.md:73`). Base pr-shepherd ссылается на `spec-maintainer` и `integration-gate` (`pr-shepherd.md:48,131`), которые есть только в overlay `issue-loop-github-strict`.
- **Маршрутов мало, и они расходятся между собой**. Их два источника (`task-runner.md:98-106` и `workflows/dev-pipeline.md:10-17`), и в них разные цепочки. У багфикса в base нет шага, который пишет fix. Hotfix, migration, spike, revert, dependency bump как маршрутов нет.
- **На 3–5 человек / 10 проектов модель распространения не рассчитана**. `sync` тянет `ref=HEAD` без пина (`sync/git.go:65`), `requires_base` парсится, но нигде не проверяется (`manifest/overlay.go:21`), `version: 0.1.0` у base не менялась при 21 коммите в `profiles/base`. Изменение доктрины одним человеком сразу доходит до всех проектов без ревью и без rollout.

## Что покрыто, что нет

| Стадия | Кто делает | Чем | Зрелость (0–3) | Пробел |
|---|---|---|---|---|
| Идея / North Star | человек | `docs/NORTH_STAR.md` + gate `north-star-auditor` | 1 | gate опционален (`--with-gates`, `task-runner.md:115-119`). В самом zprof `docs/NORTH_STAR.md` нет |
| PRD / spec | человек + skill `milestone-spec` | `docs/superpowers/specs/*`, `docs/milestones/*/PRD.md` | 2 | spec не входит в loop: `PROJECT_SPEC.md`, на который опирается раннер (`task-runner.md:271`), в репо отсутствует |
| План / issues | groomer, planner, plan-reviewer | `gh issue create`, `plan-N.md`, `todo.md` | 2 | нет приоритизации и WIP-лимита, backlog растёт без чистки |
| Реализация | implementer (overlay) | step contract, worktree для параллели (`dev-pipeline.md:29`) | 3 | — |
| Тесты | tester, 3 круга test→fix | `task-runner.md:150-152` | 2 | CI zprof гоняет только Go (`.github/workflows/ci.yml`). pytest collector'а и lint промптов в CI нет |
| Верификация шага | auditor / auditor-deep | claim→audit→state (`task-runner.md:193-260`) | 2 | по умолчанию выключено (`project.go:48`) |
| Ревью | reviewer (LLM, opus) | `approve/block` | 2 | ревью нет у человека и у второй модели/вендора. Reviewer и implementer читают одни правила |
| Интеграция | pr-shepherd | squash-merge, delivery + fabrication check | 2 | см. риски 1–2 |
| Релиз | человек | `release.yml` по тегу, goreleaser | 1 | нет release-агента, changelog, semver-политики, rollout профилей на проекты |
| Эксплуатация / инциденты | — | — | 0 | нет маршрута incident/hotfix, нет postmortem → lessons |
| Observability продукта | — | — | 0 | телеметрия есть только для агентов (`zprof-collect.py`, `telemetry.yaml`), для продукта нет |
| Обратная связь → backlog | человек (lessons вручную) | `tasks/lessons.md` | 1 | нет пути user report / crash → issue. Lessons пишутся руками после корректировки |
| Процесс-метрики | score P1–P7 | `2026-09-26-task-scorecard-design.md:101-107` | 2 | метрики про эффективность агента, про поток поставки их нет (см. DORA) |

## Сильные стороны

1. **Разделены claim и факт.** В контракт заложено «исполнитель заявил ≠ сделано» (`task-runner.md:199-204`), статусы `claimed/completed/untrusted` (`:316-322`), в pr-shepherd есть fabrication cross-check (`pr-shepherd.md:64-91`). Правило выросло из реального инцидента (Haiku отдал `done` и не записал ни одного файла, `:86-89`). Это лучше среднего по индустрии, где вердикт агента обычно принимается как есть.
2. **Жёсткая граница мутаций и изоляция контекста.** «Мутация — раннеру, чтение — сам» (`CLAUDE.md`, Граница). Раннер не пишет код (`task-runner.md:23-34`), рекурсия запрещена (`:157-161`). Итог: предсказуемая глубина и дешёвый main-контекст.
3. **Протокол blocked/resume.** Ровно два диспатча, `DECISION:` записывается в журнал, повторять вопрос запрещено (`task-runner.md:55-61`). Human-in-the-loop сделан асинхронным, без пинг-понга. Это лучшая HITL-механика в репо.
4. **Обучение через lessons.** Корректировка владельца превращается в правило и доходит до контракта. Пример: pr-shepherd за один день (`tasks/lessons.md:121-148`). Петля короткая и работает.
5. **Телеметрия агентов и scorecard** (P1–P7) — по сути SRE для самого loop. В сравнении с MEA это прямо записано как преимущество (`2026-08-09-longhorizon-harness-comparison.md:152`).
6. **Эмпирика вместо веры.** 13+ shakedown/eval прогонов в `docs/reviews/` (`2026-07-18-kmp-shakedown-*`, `kotlin-jvm-eval`) — контракты правятся по данным.

## Риски и слабости

1. **Без человека на merge нет независимой верификации.** Суть: решение «merge» принято в момент запуска loop, дальше `main` меняют LLM-reviewer и LLM-shepherd. Доказательство: `pr-shepherd.md:19`, `:26`. При 404 на protection берётся `local-green` (`:98`), а «never run the gate as authority — trust the reviewer's attestation» (`:166`). Аудит по умолчанию off (`project.go:48`). Последствие: в проектах без CI и branch protection (частый случай в overlay-проектах) единственной проверкой становится текст reviewer'а. Fabrication check (`:64-91`) ловит отсутствие файлов, но не неверную семантику. Change failure rate никто не измеряет, поэтому регресс будет замечен поздно. Мнение: в одиночном режиме это разумный trade-off, но обязательным его делает отсутствие CI, а не решение владельца.
2. **Stop list и pr-shepherd противоречат друг другу.** `gh pr merge --squash --delete-branch` (`pr-shepherd.md:30`) против «удаление веток и тегов» (`CLAUDE.md:73`). Последствие: одна модель читает это как разрешение, другая как запрет. Будут `blocked` или нарушения, и доверие к stop list как к абсолютному правилу размывается.
3. **Маршруты задублированы, у багфикса нет fix-шага.** `task-runner.md:101` пишет `bug-hunter → tester → …`, а `.claude/agents/bug-hunter.md:5` — «Does NOT fix». В overlay-версиях bug-hunter как раз чинит (kotlin-jvm `verdict: fixed`), так что поведение зависит от overlay. `dev-pipeline.md:12-14` не знает про wiki-keeper и pr-shepherd. Последствие: в самом zprof багфикс-маршрут без implementer'а завершится без фикса или обходным путём. Два источника правды будут и дальше расходиться.
4. **Недостающие маршруты.** Нет hotfix (короткий путь с обязательным человеком), data migration (router упоминает «миграция» — `agent-loop-router.md:10`, у раннера маршрута нет), spike/experiment (throwaway-ветка без merge), revert/rollback, dependency bump, release. Последствие: такие задачи классифицируются как «фича» и проходят полный planner→architect, то есть идут дорого и без нужных для них gate'ов. Для миграций данных нет обязательной обратимости и dry-run.
5. **Висячие ссылки base → overlay.** `spec-maintainer` и `integration-gate` (`pr-shepherd.md:48,131-136`) существуют только в `profiles/overlays/issue-loop-github-strict/agents/`. `PROJECT_SPEC.md` и `NORTH_STAR.md` в zprof тоже нет. Последствие: `next: main-session (spec-maintainer …)` отправляет в никуда. Spec-driven цикл в base формально объявлен, но не замкнут.
6. **Распространение профилей без версий.** `ref=HEAD` (`sync/git.go:65`), `RequiresBase` только объявлен (`manifest/overlay.go:21`), base `version: 0.1.0`. Последствие для 3–5 людей: правка lessons у одного человека («pr-shepherd мержит сам на каждом проекте», `CLAUDE.md:108`) без согласования меняет HITL-политику у всех. Нельзя откатить профиль у одного проекта и нельзя провести canary.
7. **Дрейф overlay'ев.** Каждый overlay держит свои копии reviewer'а на 260–490 строк (`profiles/overlays/*/agents/reviewer.md`). Общие правила (fabrication check, merge policy) нужно вручную разносить в 8 мест. Последствие: при 10 проектах поведение одного и того же шага будет разным.

## Сравнение с практикой

1. **DORA (Accelerate / State of DevOps).** zprof меряет эффективность агента (P1–P7), но не поток: lead time for changes, deployment frequency, change failure rate, time to restore. Данные для первых трёх уже есть (issue→PR→merge SHA в журнале и stamp), не хватает только агрегации. Хуже среднего зрелой команды, лучше среднего агентного harness'а.
2. **Trunk-based development.** Короткие ветки, squash, auto-merge — по духу это trunk-based. Но у TBD есть обязательная подстраховка: CI на каждый коммит, feature flags, быстрый revert. В zprof из этого есть только CI на Go, flags и revert-маршрута нет.
3. **Spec-driven development (GitHub Spec Kit, Kiro и т.п.).** Идея spec → plan → tasks присутствует (groomer, planner, plan-reviewer), но spec — это не живой артефакт loop'а. `PROJECT_SPEC.md` нет, spec-maintainer живёт только в одном overlay. В SDD spec — источник истины, который обновляется при merge. Здесь цикл разомкнут.
4. **Planner / executor / verifier (MEA, arXiv 2608.01964).** Разбор уже сделан (`2026-08-09-longhorizon-harness-comparison.md`), рекомендации частично внедрены: auditor, Requirements. Отставание в том, что в MEA аудит обязателен, а у zprof это opt-in, выключенный по умолчанию. Так теряется главное свойство.
5. **Two-person rule / SLSA (source integrity).** SLSA Source L3+ и классический SOX требуют второго человека на изменение `main`. Auto-merge делает zprof непригодным для таких контекстов без overlay'я, который возвращает человека. Для solo это нормально, но должно быть явной политикой, а не хардкодом.
6. **Postmortem culture (Google SRE book).** `lessons.md` — хороший зачаток blameless postmortem для агентов. Для продукта аналога нет.
7. **Policy-as-code (OPA, branch protection).** Stop list — это политика на естественном языке, её исполнение зависит от модели. Индустрия уводит необратимые действия в hooks и permissions. В Claude Code есть PreToolUse hooks, которые позволяют сделать stop list исполняемым.

## Рекомендации

**Сейчас (≤1 недели)**
- Убрать `--delete-branch` или явно вынести удаление merged-веток из stop list. Эффект: исчезает противоречие контракта.
- В `local-green` режиме pr-shepherd сам запускает тест-команду из `## Executing` перед merge. Эффект: у merge появляется независимая проверка, не LLM.
- Свести маршруты в один источник (`dev-pipeline.md`), добавить в багфикс `implementer`. Эффект: багфикс реально что-то чинит.
- Добавить pytest collector'а в `ci.yml`. Эффект: половина кода zprof перестаёт мержиться без тестов.
- Висячие ссылки spec-maintainer / integration-gate в base сделать условными («если есть в `.claude/agents/`»). Эффект: не будет dispatch'ей в никуда.

**Квартал**
- Ввести `merge_policy: auto|human|auto-if-ci` в `.zprof.yaml`, по умолчанию `auto-if-ci`. Эффект: HITL становится решением проекта, а не доктрины.
- Маршруты hotfix, revert, spike, migration (у migration обязательные dry-run и down-миграция, на hotfix `blocked` для человека). Эффект: реальные типы работ перестают идти через «фичу».
- Пинить профиль: `zprof sync --ref vX`, проверка `requires_base`, semver base/overlay, changelog. Эффект: откат и canary на проект.
- DORA-подобные метрики потока в `zprof stats` из журналов и merge SHA. Эффект: видно, помогает ли loop поставке, а не только насколько экономен агент.
- Stop list продублировать PreToolUse-hook'ами для `gh pr merge --admin`, `push --force`, `git tag`. Эффект: политика исполняется, а не просится.

**Позже**
- Маршрут `incident → hotfix → postmortem → lessons/issue` и intake продуктовой обратной связи (crash / user report → issue через groomer). Эффект: замыкается правая половина SDLC.
- Общие фрагменты контрактов (fabrication check, merge policy) через include/partials вместо копий в 8 overlay'ях. Эффект: нет дрейфа между стеками.
- Для команды: CODEOWNERS на `profiles/base`, ревью изменений доктрины человеком, release-канал stable/edge. Эффект: изменение доктрины становится изменением с ревью.

## Вопросы владельцу

1. Auto-merge — это принцип для всех проектов или удобство для solo-режима? Готов ли ты сделать его параметром проекта?
2. Почему audit выключен по умолчанию: цена (бюджет 7 диспатчей) или недоверие к качеству аудитора?
3. Где в твоих продуктовых проектах (Mira, iOS) живут релиз и инциденты, и должен ли zprof их касаться, или это сознательно вне scope?
4. Удаление веток после merge — это необратимое действие в твоём понимании или нет? От ответа зависит stop list.
5. Кто, кроме тебя, будет менять `profiles/base` в ближайшие полгода? От этого зависит, срочны ли версионирование и пин.
