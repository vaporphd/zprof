# 04 — Промпт-контракты как код (панель AI4SDLC, 2026-09-27)

Линза: prompt engineering at scale. Объект: `profiles/` @ ed75196. Всё проверено только чтением файлов.

## Вердикт

- **Схема есть, её исполнения нет.** `return_format` в frontmatter обязателен (`cli/internal/doctor/diagnostics.go:120-125`), но словари `verdict` разъехались по ролям. Кроме того, overlay-контракты противоречат собственной схеме (`ios-swift/agents/implementer.md:8-9` против `:448-470`).
- **Объём не соответствует собственной конвенции.** Из 132 agent-файлов 44 длиннее 400 строк, а `kotlin-multiplatform/agents/implementer.md` занимает 1068 строк / 72 KB. Всего в overlays 37 563 строки / 2,74 MB промптов. Над ними нет ни линтера длины, ни бюджета токенов.
- **Версионирование декоративное.** У 9 из 10 overlays `version: 0.1.0`, CHANGELOG нет. `config_hash` есть в схеме, но в 70 из 70 локальных диспатчей он пустой. Нельзя сказать, какая версия контракта дала какой результат.
- **Наследование «роль целиком» приводит к drift'у.** Reviewer'ы ios и rust совпадают на 121 из 359 непустых строк: это copy-paste без общего источника. Base не содержит исполнителей вообще (`profiles/base/manifest.yaml: roles: []`).
- **Тесты контрактов — только smoke.** Shakedown проверяет 10 структурных инвариантов на одной задаче (`profiles/base/shakedown/checks.json`). Семантику ролей, overlay'и и регрессию между версиями контрактов он не проверяет.

## Инвентарь и метрики контрактов

| Файл / группа | Строки | Байты | Дубли / противоречия | Оценка (0–3) |
|---|---|---|---|---|
| base/agents (11 файлов) + router + claude-block | 1661 | 94 407 | auditor ≡ auditor-deep кроме 4 строк (diff); auditor отвечает `completion:`, а не `verdict:` (`auditor.md:11-12`) | 2 |
| base/task-runner.md (orchestrator) | 330 | 20 837 | нет маппинга словаря reviewer'а (`block/approve-with-fixes`) — обрабатываются только `done/blocked/failed` (`task-runner.md:146-147`) | 2 |
| base/pr-shepherd.md vs strict/pr-shepherd.md | 174 / 172 | 13 123 / 13 112 | «Merges when ready» (`base:26`) против «Never merges» (`strict:26`): осознанный override, но в base-схеме нет `blocked` без суффикса | 2 |
| ios-swift (13) | 4633 | 352 265 | implementer 599 строк; §8 OUTPUT FORMAT требует full code в fenced-блоках, а схема запрещает code fence; «ask the user» (`:64`) у subagent'а | 1 |
| systems-rust (13) | 5131 | 387 965 | architect 717, tester 610; reviewer `:25` возвращает `verdict: blocked`, которого нет в его enum (`:10`) | 1 |
| kotlin-multiplatform (14) | 5553 | 407 374 | implementer 1068 строк, единственный файл >800 | 1 |
| backend-python / frontend-web / systems-cpp | 4896 / 4588 / 5000 | 331 K / 344 K / 371 K | тот же reviewer-скелет, та же ошибка `blocked`, что и в ios/rust | 1 |
| re-macho (12) | 3337 | 286 425 | отдельный домен, дублей с dev-overlays мало | 2 |
| issue-loop-github-strict (7) | 1168 | 75 257 | process-overlay корректно переопределяет merge-политику | 2 |
| zcode-harness (10) | 632 | 25 330 | все файлы ≤101 строки, контракт лаконичный | 3 |

Итого: 132 agent-файла, 44 >400 строк, 51 <200 строк (`find profiles -path '*/agents/*.md' | xargs wc -l`).

## Сильные стороны

1. **Схема ответа — первоклассный артефакт.** `doctor` падает с error, если у роли нет `return_format` (`diagnostics.go:120-125`), а collector вычисляет `return_parsed` и `has_preamble` по первой строке `verdict:` (`zprof-collect.py:1388-1398`). Это настоящий контракт с машинной проверкой, не пожелание.
2. **Граница ответственности выражена в промпте явно.** Task-runner перечисляет, чего он не делает, и пишет, какой `verdict: failed` вернуть при соблазне (`task-runner.md:22-34`). Рекурсию раннеров запрещает отдельное правило (`:156-160`). Shakedown это проверяет (`no_recursive_runner`).
3. **Stop list задан данными, а не прозой.** Он лежит в `manifest.yaml` (base + overlay, например ios-swift) и рендерится в CLAUDE.md. `doctor` проверяет stop lists (`diagnostics.go:99`).
4. **Deploy обратим на уровне файлов.** Перед overwrite создаётся backup (`apply/engine.go:280-282`), `*.zprof.bak-*` и `.zprof.yaml.bak-*` уходят в gitignore (`:303`), orphans удаляются с `.bak` (`:50`). `sync --merge overwrite|preserve|interactive` (`cmd/sync.go:89`) даёт стратегию для конфликтов managed-блоков.
5. **Auditor не доверяет исполнителю.** Правило «`verdict: done` — claim, не факт» (`auditor.md:37`) и проверка среды против AC — зрелый паттерн verification-by-environment.
6. **Есть authoring guide с числами.** 200–400 / 500–800 строк, обязательные секции для >150 строк (`2026-07-16-agent-authoring-guide.md:15,43`).

## Риски и слабости

1. **Схема противоречит телу контракта.**
   - Доказательство: `ios-swift/implementer.md:8-9` требует «begins with `verdict:` — no code fence», а `:448-470` требует секции Summary, Folder tree и Full code в fenced-блоках.
   - Последствие: модель получает два взаимоисключающих формата. Отсюда преамбулы (4 из 70 диспатчей `has_preamble: True`) и `return_parsed: False` у pr-shepherd в 3 из 6 случаев (локальный `.agentlog/dispatches.jsonl`). Task-runner тратит retry (`task-runner.md:152-153`).
2. **Словари `verdict` несогласованы.**
   - Доказательство: у reviewer'а enum `block|approve-with-fixes|approve|awaiting-approval` (`systems-rust/reviewer.md:10`), но внутри он же предписывает `verdict: blocked` (`:25`). Auditor отвечает `completion:` (`auditor.md:12`), а collector ищет только `verdict:` (`zprof-collect.py:1391`). У wiki-keeper'а свой `done-noop`, у pr-shepherd'а 7 значений.
   - Последствие: телеметрия записывает аудиторов как not-parsed, а task-runner не знает, что делать с `approve-with-fixes`. Class-A метрика смешивает шум и настоящие нарушения.
3. **Нет версии контракта в рантайме.**
   - Доказательство: `config_hash` есть в `telemetry.yaml:26` и `zprof-collect.py:1492`, но его никто не вычисляет (70/70 None). `stats/render.go:909-914` рисует до/после по `config_hash`, то есть отображает пустоту. CHANGELOG нет, `version` в manifest ни разу не поднимался, кроме kmp (0.2.0).
   - Последствие: нельзя провести A/B правки контракта. `evaluator-telemetry` предлагает diff'ы, эффект которых нечем измерить.
4. **Когнитивная нагрузка не бюджетирована.**
   - Доказательство: ios-swift implementer весит 44,7 KB и на 99,7% написан на английском (118 кириллических символов). При ≈4 B/token это ≈11k tokens одного контракта, без CLAUDE.md и AGENT_LOOP.md. Из 31 заголовка поведенческие инварианты содержат примерно §0, §4, §7 и схема. §1 «MANDATORY INITIAL DIALOGUE» мёртв: subagent не может «ask the user» (`:64`), вопрос никуда не уходит.
   - Последствие (мнение): инструкции, критичные для loop'а (схема, stop list), тонут в стековом справочнике. Сюда же относится `model: opus-4-6` в frontmatter (`:5`) — pin версии модели в тексте контракта в обход `zprof agents`.
5. **Наследование целиком вызывает drift.**
   - Доказательство: 8 reviewer'ов; 124 непустые строки встречаются в ≥5 из них дословно. Ошибка `blocked` из п.2 скопирована во все стеки. Gap-audit ссылается на `overlays/kotlin-android/`, которого уже нет (`2026-07-18-kotlin-overlay-gap-audit.md:5`): документ пережил свой объект.
   - Последствие: фикс общего правила требует 8–10 ручных правок. Пропуск одной из них — тихий drift, который ни один тест не ловит.
6. **Язык контрактов не соответствует конвенции.**
   - Доказательство: CLAUDE.md требует «Промпты: русский текст, английские ключи», base написан по-русски, а dev-overlays почти целиком по-английски.
   - Последствие (мнение): для модели это не критично, Claude одинаково следует обоим. Хуже то, что правило в CLAUDE.md не исполняется, а значит конвенции в целом не enforced.
7. **Shakedown узкий.**
   - Доказательство: 10 проверок, одна задача про `divide` (`shakedown/task.md`). Проверяются факт вызова ролей и отсутствие преамбулы. Нет проверок `next`-маршрутизации, словарей verdict, overlay-специфики, негативных кейсов (stop list, blocked).
   - Последствие: регрессии в 2,7 MB overlay-промптов не имеют ни одного теста.
8. **Rollback не является операцией.**
   - Доказательство: в `cli/internal/cmd/` нет rollback/pin-команды (`ls`: apply, sync, doctor, …). Откат — ручное копирование `.bak-*` или checkout `~/.zprof/repo`.
   - Последствие: плохую правку контракта, доехавшую через `sync`, быстро откатить нельзя.

## Сравнение с практикой

1. **Anthropic, «Writing tools for agents» / prompt engineering.** Там рекомендуют: один формат вывода, примеры вместо длинных правил, eval-driven итерации. zprof силён в формате (schema-first), но нарушает его сам (риск 1), и eval-петля не замкнута (нет `config_hash`).
2. **DSPy / structured prompting.** Сигнатура (input → output fields) отделена от инструкций и компилируется. Аналог для zprof — вынести `return_format` в typed-схему (JSON Schema) в одном месте и генерировать из неё секцию промпта и парсер collector'а.
3. **promptfoo-style tests.** Там фикстура «вход → assertions на вывод» гоняется на каждый PR к промпту. Shakedown — это один такой кейс; нужна матрица роль × сценарий × overlay с дешёвыми assert'ами (regex на первую строку, enum verdict).
4. **Spec-as-code (OpenAPI, protobuf).** Там есть единый реестр enum'ов и semver с breaking-change детектором. Словарь `verdict` — это API между агентами, и ему нужен реестр и diff-проверка.
5. **Cursor rules / `.github/copilot-instructions.md`.** Правила там короткие, scoped по glob'ам и подключаются при совпадении. Аналог — стековые вставки, подключаемые к базовому контракту по `executing:`-glob'ам из manifest, вместо 45 KB монолита.
6. **Superpowers skills (в этой среде).** Frontmatter `description` отвечает за триггер, тело короткое, детали вынесены в reference-файлы и грузятся лениво. Это прямой ответ на риск 4: справочник по Swift concurrency (§3.8–3.10) не нужен в каждом диспатче, его можно читать по требованию.
7. **Claude Code subagents.** Модель задаётся в frontmatter как alias (`sonnet`/`opus`). Pin `opus-4-6` в overlay противоречит `zprof agents` как единому источнику моделей.

## Рекомендации

**Сейчас**
- Убрать §8 OUTPUT FORMAT и §1 INITIAL DIALOGUE из всех overlay-исполнителей. Эффект: исчезает прямой конфликт со схемой, минус ~60–80 строк на файл.
- Зафиксировать реестр verdict-словарей (`profiles/base/verdicts.yaml`) и добавить в `doctor` проверку, что каждый `verdict:` в теле входит в enum своей роли. Эффект: ловится баг `blocked` в 8 reviewer'ах.
- Научить collector парсить `completion:` у аудиторов, либо перевести аудиторов на `verdict:`. Эффект: честный `return_parsed`.
- Вычислять `config_hash` при apply (sha256 контента agent-файла), писать его в frontmatter и через hook класть в raw. Эффект: before/after в `zprof stats` заработает.

**Квартал**
- Перейти на композицию: `base/roles/<role>.md` (инварианты, схема, loop-правила) + `overlays/<stack>/inserts/<role>.md` (стековые правила), сборка в apply. Эффект: общий фикс делается в одном месте, drift reviewer'ов исчезает.
- Добавить lint длины и бюджета токенов в `doctor` (warn >400 / error >800 для executor). Эффект: конвенция CLAUDE.md начнёт исполняться.
- Сделать promptfoo-подобную матрицу shakedown: роль × {happy, blocked, stop list} × overlay. Assert'ы на первую строку, enum и `next`. Эффект: регрессионные тесты промптов в CI.
- Добавить `zprof rollback [--to <config_hash>]` поверх существующих `.bak`. Эффект: откат одной командой.

**Позже**
- Вынести стековые справочники в skills/reference-файлы с ленивой загрузкой. Эффект: −50–70% токенов system prompt исполнителя (мнение).
- Ввести semver overlay + CHANGELOG, генерируемый из diff контрактов, и breaking-флаг при смене enum. Эффект: проекты видят, что именно доехало через `sync`.

## Вопросы владельцу

1. Отличия pr-shepherd в strict-overlay — это целевая модель «overlay переопределяет политику», или merge-политику надо сделать параметром (`MERGE_GATE`) одного контракта?
2. Английские overlays — осознанный отказ от правила «русский текст» или долг? Какую конвенцию считать истинной?
3. Кто владеет словарём `verdict`: task-runner (потребитель) или каждая роль?
4. Pin `model: opus-4-6` в ios implementer — намеренный эксперимент или артефакт? Должен ли `doctor` запрещать не-alias модели в frontmatter?
5. Есть ли телеметрия с реальных проектов (не с самого zprof, где 58 из 70 диспатчей — `general-purpose`), чтобы проверить гипотезу «конфликт схемы вызывает преамбулы» до переписывания?
