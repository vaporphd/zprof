# ADR 0001: config_hash в коллекторе и штатный передеплой телеметрии в самом zprof

**Date:** 2026-09-27
**Status:** proposed
**Issue:** #22 (milestone panel-2026-09-27)
**Run:** .zprof/runs/2026-09-27-collector-config-hash-verdict.md

## Context

В схеме телеметрии есть `config_hash` и `verdict`, но в реальных логах они пустые
(zprof 70/70, jarvis 468, hft_moex 3183). Из-за этого не работают drift-карточка,
P4/P5/P7 и `zprof score`.

Что выяснилось при чтении кода (факты, на которые опираются решения ниже):

1. **config_hash никто не вычисляет.** `_normalize_dispatch` копирует
   `raw.get("config_hash")` (`profiles/base/zprof-collect.py:1502`), но ни один путь
   сборки raw-диспатча это поле не выставляет.
2. **verdict в исходнике уже извлекается.** `_class_a_checks` находит первую строку
   `verdict:` (`:1398-1418`) и отдаёт `verdict_value`; `_normalize_dispatch` подставляет
   его в `norm["verdict"]`, если raw-verdict пуст (`:1538-1544`). В задеплоенной копии
   `.claude/zprof-collect.py` (1357 строк) `verdict_value` не встречается ни разу, то есть
   в zprof verdict пуст из-за **устаревшего деплоя**, а не из-за логики исходника.
3. **SubagentStop только ставит pointer.** `_handle_subagent_stop` (`:231-239`) пишет
   `state.pointers[agent_id] = {agent_transcript_path, agent_type, ts}`. Строки
   диспатчей собираются позже, на Stop/SessionStart, в `_collect_subagent_transcripts`
   (pass 2, `:1018-1122`, словарь `enrichment`). Pointer'ы сейчас никто не читает и не
   чистит.
4. **agent_type — это frontmatter `name:`, а не имя файла.**
   - `WriteAgent` (`cli/internal/apply/agent_write.go:26`) пишет `<agentName>.md` с
     подпапками (`gates/foo`), но frontmatter `name:` не трогает.
   - Gate: `.claude/agents/gates/north-star-auditor.md` содержит `name: north-star-auditor`.
   - Namespaced (multi-overlay): `NamespaceAgent` (`cli/internal/overlay/loader.go:230`)
     даёт файл `implementer-ios.md`, но внутри остаётся `name: implementer`
     (см. `profiles/overlays/ios-swift/agents/implementer.md`). В двух overlay'ях будет
     два файла с одинаковым `name: implementer`.
   Поэтому `Path(".claude/agents") / f"{agent_type}.md"` ошибается в двух из трёх случаев.
5. **`zprof apply` требует хотя бы один overlay, а не `.zprof.yaml`.**
   - `cli/internal/cmd/apply.go:26`: `cobra.MinimumNArgs(1)`; `:43-52`: аргумент `base`
     отбрасывается, и если больше ничего нет, возвращается ошибка
     «no overlay names given ... pass at least one overlay name».
   - `cli/internal/apply/engine.go:61-63`: `Apply` возвращает
     `"at least one overlay is required"`.
   - `.zprof.yaml` на вход **не нужен**: `apply.go:64-75` собирает `ProjectManifest` из
     аргументов, а `LoadProject` вызывается только для `CarryOverFrom`, если файл есть.
     `engine.go:168` сам записывает `.zprof.yaml` в конце.
   - `zprof sync` с `.zprof.yaml`, где `overlays: []`, передаёт пустой список в `Apply`
     (`cli/internal/cmd/sync.go:67-78`) и падает на `engine.go:62`.
6. **Любой существующий overlay в корне zprof разрушителен.** Семь executor-агентов
   (`architect`, `implementer`, `tester`, `reviewer`, `bug-hunter`, `explorer`,
   `refactor-agent`) написаны вручную под zprof (коммит `7c611ed`) и есть в каждом
   dev-pipeline overlay'е. Apply перезаписал бы их. В режиме `--merge overwrite`
   (по умолчанию) он также перегенерирует блоки `consilium`/`executing` в `CLAUDE.md` и
   добавит `workflows/dev-pipeline.md`. Остальные агенты в `.claude/agents/` побайтно
   совпадают с `profiles/base/agents/`.
7. **Хуки.** `scoreHookCommand` (`cli/internal/apply/settings.go:24`) склеивается со
   Stop-guard'ом в `telemetryHooks` (`settings.go:34`). `EnsureHooks` (`settings.go:49`)
   находит существующую zprof-запись через `zprofHookIndex` (`settings.go:105`, ищет
   подстроку `zprof-collect.py`) и, если команда отличается, заменяет её на месте
   (`settings.go:79-81`). Остальные ключи (`permissions`) сохраняются.
8. **Коллектор.** `deployCollector` (`cli/internal/apply/collector.go:23`) при каждом
   вызове перезаписывает `.claude/zprof-collect.py` байтами `Base.CollectorScript`
   (`:27-31`) и `.agentlog/schema.json`. `~/.zprof/repo` является симлинком на
   `profiles/`, поэтому источник совпадает с рабочим деревом.
9. `zprof score` работает без `.zprof.yaml`: `score.LoadConfig`
   (`cli/internal/score/config.go:101`) начинает с `Defaults()` и читает
   `.agentlog/schema.json`.

## Decision

### D1. Резолв agent_type → файл агента: индекс по frontmatter `name:`

Коллектор не склеивает путь из `agent_type`. Он строит индекс по всем определениям агентов:

```
_agent_file_index(cwd) -> dict[str, list[Path]]
  root = Path(cwd) / ".claude" / "agents"
  for p in sorted(root.rglob("*.md")):          # рекурсивно: gates/ и любые будущие подпапки
      skip: не файл | resolve() вне root (симлинк наружу) | *.bak-* / *.zprof.bak-*
      key = frontmatter name: (строка между первыми '---'; кавычки и пробелы срезаны)
      index[key].append(p)
  stem_index: то же, но ключ = p.stem       # fallback для файлов без name:
```

`_resolve_agent_file(cwd, agent_type) -> tuple[Path | None, int]` (путь, число кандидатов):

1. Пустой `agent_type` → `(None, 0)`.
2. Кандидаты: `index[agent_type]`. Если их нет, берутся `stem_index[agent_type]`.
3. Ровно один кандидат → этот файл. Ноль → `None` (встроенные `Explore`,
   `general-purpose`, plugin-агенты, пользовательские `~/.claude/agents`).
   Больше одного → `None`, причём число кандидатов возвращается, чтобы записать его в `ext`.
   Коллектор не угадывает, какой из одноимённых файлов выбрал Claude Code: неверный
   хеш хуже пустого, потому что даёт ложный drift.

Покрытие трёх случаев:

| agent_type | Как находится |
|---|---|
| `implementer` (base / одиночный overlay) | `name: implementer` → `.claude/agents/implementer.md` |
| `implementer-ios` | срабатывает, если `name: implementer-ios` (ручной или будущий переименованный namespaced-файл) → `implementer-ios.md`. Если Claude Code сообщил `implementer` и одноимённых файлов два, получаем ambiguous → `null` |
| `north-star-auditor` | `name: north-star-auditor` в `gates/north-star-auditor.md` (rglob) |

Почему индекс, а не «прямое имя, потом известные подпапки»: при прямом имени не
находятся namespaced-файлы (у них другой `name:`), а список подпапок пришлось бы
держать в синхронизации с `profiles/base/agents/`. Индекс ровно повторяет то, по чему
Claude Code сам резолвит агента. Путь из недоверенного ввода не собирается, поэтому
path traversal (`../`) невозможен в принципе. Стоимость: около 25 мелких файлов на один
хук, единицы миллисекунд. Индекс строится один раз на вызов коллектора.

**Хеш:** `hashlib.sha256(path.read_bytes()).hexdigest()[:12]` по байтам файла как есть,
без нормализации. Файл пишет apply вместе с резолвом `model:` (`agent_write.go:75`),
поэтому смена модели тоже меняет хеш. Это намеренно: модель входит в конфиг.
Любой `OSError`/`UnicodeDecodeError` → `None`, исключение не выходит наружу, `exit(0)`
сохраняется. Только stdlib.

**Когда вычислять.** Снимок делается **в момент SubagentStop**: в pointer дописываются
`config_hash` и `config_hash_candidates`. Если считать на Stop, правка контракта между
концом диспатча и концом сессии приписала бы строке новый хеш.
В pass 2 (`_collect_subagent_transcripts`) `enrichment["config_hash"]` берётся из
`state.pointers[agent_id]`. Pointer после использования удаляется (сейчас pointer'ы
копятся бесконечно). Если pointer'а нет (SessionStart-recovery мёртвой сессии,
хук не отработал), хеш вычисляется в момент сборки и ставится
`ext.config_hash_source = "collect"`. При снимке пишется
`ext.config_hash_source = "subagent-stop"`. При неоднозначности пишется
`ext.config_hash_ambiguous = <N>`. Строка `:1502` уже переносит поле из raw в
нормализованную строку, её менять не нужно.

Для `ext.config_hash_source` не требуется менять `telemetry.yaml`, потому что `ext`
расширяемое. Если reviewer потребует, эти ключи надо описать в разделе ext схемы.

### D2. verdict: логику не меняем, чиним деплой и закрываем тестами

Первая строка `verdict:` уже разбирается (`:1398-1418`), а fallback уже работает
(`:1538-1544`). По AC2 делаем следующее:
- добавляем pytest-кейсы (D5), которые фиксируют, что verdict берётся из первой строки
  `verdict:`, в том числе при наличии preamble, и что `return_parsed=False` → нет verdict;
- чиним сам симптом в zprof передеплоем (D3).
Разбор markdown-вариантов (`**verdict:**`, `- verdict:`) в объём задачи не входит.
Он пересекается с #20 (`:1388-1408`) и меняет семантику `return_parsed`. Если после
передеплоя у task-runner по-прежнему `return_parsed=False`, заводится отдельный issue.

### D3. Передеплой в zprof: `zprof apply --telemetry-only`, `.zprof.yaml` НЕ заводим

`.zprof.yaml` не нужен и не помогает. Apply не читает его как вход (факт 5), а режим
«base без overlay» блокируется проверкой количества overlay'ев
(`apply.go:26,50-52`, `engine.go:61-63`), а не отсутствием файла. Ручной `.zprof.yaml` с
`overlays: []` ещё и ломает `zprof sync` в корне zprof (`sync.go:67-78` → `engine.go:62`).
Если применить существующий overlay, он затрёт семь ручных агентов (факт 6).
**Минимальный `.zprof.yaml` не создаётся.** Это отступление от буквы fallback'а в AC4,
его нужно отметить в PR.

Вместо этого добавляется узкий штатный режим apply, который трогает только телеметрию:

- `cli/internal/apply`: новая экспортируемая функция
  ```go
  // DeployTelemetry writes .claude/zprof-collect.py and .agentlog/schema.json
  // and upserts the telemetry hooks. It touches nothing else.
  func DeployTelemetry(projectDir string, base *overlay.Base) ([]string, error)
  ```
  Она вызывает `deployCollector` и `EnsureHooks`. Сигнатура `deployCollector` сужается до
  `(projectDir string, base *overlay.Base)`. `Apply` вызывает `DeployTelemetry` вместо
  текущих шагов 5.5/5.6 (`engine.go:147-160`). Поведение `Apply` не меняется.
- `cli/internal/cmd/apply.go`: флаг `--telemetry-only`. С ним:
  `Args` допускает 0 аргументов. Overlay-аргументы с этим флагом дают ошибку
  (взаимоисключение). `LoadBase(repoDir()/base)` → `DeployTelemetry(pwd, base)`.
  `.zprof.yaml`, агенты, `CLAUDE.md`, `AGENT_LOOP.md`, `workflows/` и `.gitignore`
  не трогаются. `--dry-run` печатает список целей. `--minimal`, `--with-gates` и
  `--merge` в этом режиме не действуют (одна строка в help). Без флага поведение
  остаётся прежним, включая ошибку `apply.go:51`.
- Команда для implementer в корне zprof: `zprof apply --telemetry-only`, после
  `make install` (или `go install`), чтобы бинарь знал флаг.

### D4. Score-цепочка в Stop-хуке встанет сама

Код `settings.go` менять не нужно. `DeployTelemetry` → `EnsureHooks` (`settings.go:49`)
найдёт текущую Stop-запись по `zprof-collect.py` (`zprofHookIndex`, `:105`). Её команда
(только guard) не равна `telemetryHooks[1].command` (`:34` = guard + `"; "` +
`scoreHookCommand` из `:24`), поэтому запись заменится на месте (`:79-81`), без дубля.
SubagentStop и SessionStart уже совпадают и останутся как есть. `permissions`
сохранится. Замечание: `json.MarshalIndent` переформатирует однострочные записи хуков.
Это косметический diff, так и задумано.

### D5. Тесты (AC3, AC6)

pytest в `profiles/base/tests/` (новый `test_config_hash.py`, fixture-каталог
`.claude/agents` в `tmp_path`):
- стабильность: два вызова по одному файлу дают одинаковые 12 hex (`[0-9a-f]{12}`);
- инвалидация: изменили байт, хеш изменился; изменили только `model:`, хеш тоже изменился;
- missing: неизвестный `agent_type` / нет `.claude/agents` / пустой `agent_type` → `None`,
  исключения нет;
- резолв: base (`implementer`), gate (`gates/north-star-auditor.md` по `name:`), файл
  `implementer-ios.md` с `name: implementer-ios`, два файла с одним `name:` → `None`
  плюс ambiguous = 2, файл без frontmatter → fallback по stem, симлинк за пределы root
  игнорируется;
- интеграция: SubagentStop кладёт хеш в pointer; Stop даёт строку в
  `dispatches.jsonl` с тем же `config_hash`, а pointer удалён; без pointer'а
  `ext.config_hash_source == "collect"`;
- verdict: первая строка `verdict:` → `verdict`; preamble перед ней →
  `has_preamble=True`, verdict всё равно заполнен; две строки `verdict:` → берётся первая;
  нет строки → нет `verdict`, `return_parsed=False`.

Go: table-driven тест на `DeployTelemetry` (пишет только 3 файла, не создаёт
`.zprof.yaml`/агентов, апгрейдит stale Stop-хук) и тест cmd на взаимоисключение флага
с аргументами. Существующие `settings_test.go` и `e2e_test.go` должны остаться
зелёными.

## Consequences

- Проще: у каждой строки диспатча, резолвящейся в единственный файл, появляется
  `config_hash`, и drift-карточка и P4/P5/P7 получают данные. В самом zprof есть
  штатный способ обновить телеметрию, не рискуя ручными агентами.
- Сложнее: `zprof sync` в корне zprof по-прежнему не передеплоит коллектор
  (`sync.go:54-56`, нет `.zprof.yaml`). Отставание исходника от деплоя может
  повториться. Рекомендуется отдельным коммитом дописать `zprof apply --telemetry-only`
  в dogfood-шаг `make install` репозитория.
- Для namespaced multi-overlay проектов хеш будет `null` (ambiguous), пока
  `WriteAgent` не переписывает `name:`. Это отдельный дефект: два агента с одним
  `name:` конфликтуют и в самом Claude Code. Нужен отдельный issue:
  «NamespaceAgent должен переписывать frontmatter name».
- Pointer'ы в `state` начинают очищаться. Размер state-файла перестаёт расти без
  ограничений.
- Пересечение с #20 только в `_class_a_checks` (`:1388-1408`). Эта задача там код не
  меняет, только добавляет тесты. Rebase, если понадобится, будет тривиальным.

## Alternatives considered

- **`agent_type + ".md"` в корне, потом подпапки `gates/`.** Отвергнуто: не находит
  namespaced-файлы (у них другой `name:`), держит список подпапок вручную и собирает
  путь из недоверенной строки.
- **Хеш на Stop вместо SubagentStop.** Отвергнуто как основной путь (ложная атрибуция
  при правке контракта внутри сессии). Оставлено как fallback с пометкой в `ext`.
- **При ambiguous хешировать сортированную конкатенацию кандидатов.** Отвергнуто:
  значение не соответствует ни одному реальному контракту и ломает сравнение до/после.
- **Минимальный `.zprof.yaml` (base, без overlay).** Невозможен: `apply` требует
  overlay-аргумент (`apply.go:26,50-52`), а `sync` с `overlays: []` падает на
  `engine.go:62`.
- **Разрешить `Apply` без overlay'ев (base-only apply).** Отвергнуто: перерисует
  `consilium`/`executing` в `CLAUDE.md` и уберёт из таблицы семь ручных ролей, запишет
  `.zprof.yaml` с пустыми overlays (ломает sync) и расширит площадь изменения далеко за
  пределы issue.
- **Overlay `zprof-self` с ручными агентами zprof.** Хорошо в долгую, потому что
  формализует dogfooding, но это отдельный дизайн. Кандидат в follow-up.
- **Ручное копирование `cp profiles/base/zprof-collect.py .claude/`.** Запрещено AC4.
