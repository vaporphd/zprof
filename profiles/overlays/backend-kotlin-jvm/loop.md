## Backend Kotlin JVM loop extension

Расширение dev-pipeline для серверного/библиотечного/CLI Kotlin JVM проекта (Gradle Kotlin DSL, JDK 21+, multi-module).

### Trigger phrases
- EN: `next task`, `build the project`, `run tests`, `run integration tests`, `check style`, `add feature`, `fix bug`
- RU: `следующая задача`, `собери проект`, `прогони тесты`, `прогони интеграционные`, `проверь стиль`, `добавь фичу`, `почини баг`

### Pipeline
Стандартный dev-pipeline. architect/implementer/tester/refactor-agent/bug-hunter/reviewer/explorer знают:
- Kotlin JVM — single-target, no source-set graph, no `expect`/`actual`
- Multi-module Gradle с `settings.gradle.kts include(...)` — модули layer-typed (`core-model`, feature modules, `runner`/composition root)
- Coroutines + Flow (`Dispatchers.IO` доступен на JVM — используется для blocking I/O; `Dispatchers.Default` для CPU-bound)
- JUnit5 + Kotest-assertions + MockK (MockK безопасен на JVM, в отличие от KMP)
- kotlinx.serialization с ОДНИМ Json instance через DI
- ktlint 1.3.x via jlleitschuh/ktlint-gradle plugin
- Опциональный `integrationTest` source set — гейт против реальных внешних систем, НЕ в default lifecycle
- Layer taxonomy per module: `domain/` (model/error/usecase|service/repository), `data/` (dto/datasource/mapper), `api/` (только для server), `di/` composition root
- Ban list в production `**/domain/**` + `**/data/**`: `runCatching { }` (F-15 shakedown — swallows CancellationException), `!!`, `GlobalScope`, `runBlocking` (кроме `fun main()`), `println`/`System.out.println`, второй `Json { }` block

### Специальные диспатчи
| Задача | Агент |
|---|---|
| Сборка/тесты/dep-tree через Gradle | `gradle-runner` |
| Проверка стиля Kotlin | `ktlint-checker` |
| Скаффолдинг проекта / нового модуля | `init-kotlin-jvm` |

### Композит с issue-loop-github-strict
Overlay стеко-агностичен — процесс-агенты (`planner`, `pr-shepherd`, `integration-gate`, `spec-maintainer`, `wiki-keeper`, `docs-writer`, `ci-devops`) применяются поверх любого стека. Композит:
```
zprof apply backend-kotlin-jvm issue-loop-github-strict
```
Дает GitHub-issue-driven pipeline с полным контуром: planner (DRAFT→plan-reviewer→AUTHOR через `gh issue create`) → main-session dispatches architect (если ADR-trigger=yes) → implementer (branch `issue-<N>-<slug>` → PR через `gh pr create`) → integration-gate (если diff в `<INTEGRATION_SCOPE>`) → wiki-keeper → reviewer → pr-shepherd (squash-merge + stamp) → spec-maintainer + docs-writer post-merge.
