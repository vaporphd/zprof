# ADR 0007: guard — валидатор `return_format` на `SubagentStop`

**Date:** 2026-09-28
**Status:** accepted
**Issue:** #26
**Parent:** ADR-0004 (D2 fail-open, D7 роль/диспатч, D9 журнал), ADR-0006 (F1 форма событий)
**Plan:** `tasks/plan-issue-26.md` (этот ADR — шаг 1; в четырёх местах перекрывает план и в двух — буквальный текст AC, см. G10)
**Spec:** `docs/superpowers/specs/2026-09-27-guard-hooks-design.md` §4, §6, §7, §11, §12 п.3;
`docs/superpowers/specs/2026-08-02-agent-telemetry-design.md` §4.1, §17 (реальный payload `SubagentStop`)

## Context

`zprof-guard.py` (#23–#25) реализует только режим `pre-tool`; `main()` на
любом другом `mode` молчит (заглушка `# Any other mode (incl. "subagent-stop",
implemented in #26) is a no-op.`). Issue #26 добавляет второй режим — проверку
первой строки финального ответа субагента против `return_format` из его
frontmatter. Спека §6 и AC1–AC8 задают поведение, но для детерминированной
реализации этого мало:

1. **Нет формы события.** D9 описывает событие `pre-tool` (`tool`, `target`,
   `input_hash` — от вызова инструмента). У `SubagentStop` нет ни `tool_name`,
   ни `tool_input`; AC5/AC6 называют только `decision: block` и `format_unfixed`.
2. **`<cwd>` против `project_root()`.** AC1 ищет контракт в `<cwd>`; `pre_tool()`
   читает конфиг и пишет журнал в `project_root(payload)`
   (`$CLAUDE_PROJECT_DIR` → `payload["cwd"]` → `os.getcwd()`).
3. **Разбор frontmatter без YAML.** AC8 требует построчный разбор; AC2 не
   говорит, что такое «ровно `---`», какие блочные индикаторы допустимы и что
   делать с незакрытым frontmatter.
4. **`blocked-<reason>` в живом контракте.** Опрос всех 152 файлов с
   `return_format` в `profiles/` и `.claude/agents/`: первая значимая строка
   везде `verdict:` (ни одного `completion:` после миграции #20), индикатор
   везде ровно `return_format: |`. Единственная аномалия — `pr-shepherd.md:10`
   (base, overlay `issue-loop-github-strict`, задеплоенный `.claude/agents/`):
   последний элемент `blocked-<reason>` — литеральный префикс + плейсхолдер.
   Точное сравнение даст ложный `block` на штатный ответ
   `verdict: blocked-worktree-locked`. Ещё 10 файлов (`gates/*`, `re-macho`)
   пишут список с пробелами вокруг `|`.
5. **`transcript_path` на `SubagentStop` — транскрипт main, а не субагента.**
   Реальный payload (telemetry-spec §4.1, проверено на `claude 2.1.220`, §17):
   `transcript_path` = `<slug>/<sess>.jsonl` (сессия main),
   `agent_transcript_path` = `<sess>/subagents/agent-<id>.jsonl`, плюс
   `last_assistant_message` — готовый текст финального ответа. Там же (§17, С1):
   **транскрипт субагента в момент хука не дописан** (3452 байта против 5075
   после). Буквальное AC3 («последняя `assistant`-запись в `transcript_path`»)
   читает последний ход main (обычно `tool_use` вызова `Agent`) — ложный
   `block` или ложный pass на каждом субагенте. Это не гипотеза: коллектор уже
   берёт `agent_transcript_path` (`zprof-collect.py:243`).
6. **Две разные «ошибки» транскрипта.** AC3 «нет assistant-записи → pass» и
   AC7 «битый транскрипт → exit 0 + `error`» — разные исходы, и один
   `try/except` на всё чтение их смешает.
7. **Пустой финальный текст** — не описан.

## Decision

Вся логика — функции внутри `profiles/base/zprof-guard.py`, новый раздел
`# --- subagent-stop validator (ADR-0007, #26) ---` между концом `pre_tool()` и
разделителем `# main`. Ни одного нового модуля, ни одного нового импорта
(`json`, `re`, `Path` уже импортированы). `guard.yaml`/`guard.json` не
меняются: режим не вызывает `load_config()`, не смотрит в `CONTEXTS` и не
зависит от наличия `.claude/guard.json` — это отдельный режим `main()`, не
правило движка. Новых top-level `try/except` нет (G5).

### G1. События журнала: `subagent-stop`/`block` и `format_unfixed`

Ключи — ровно схема D9 (как `pre-tool`/`allow_unverified` в ADR-0006 F1), новых
ключей нет. Значения:

| ключ | провал, `stop_hook_active` не `true` (AC5) | провал, `stop_hook_active is True` (AC6) |
|---|---|---|
| `ts` | `_now_ts()` | `_now_ts()` |
| `session_id` | `payload.get("session_id")` | то же |
| `event` | `"subagent-stop"` | `"format_unfixed"` |
| `role` | `resolve_role(payload)` | то же |
| `dispatch_id` | см. ниже | то же |
| `tool` | `null` | `null` |
| `rule` | `"return_format"` | `"return_format"` |
| `decision` | `"block"` | `null` |
| `detail` | `"key"` \| `"value"` | `"key"` \| `"value"` |
| `target` | `null` | `null` |
| `input_hash` | `null` | `null` |
| `run_id` | `null` | `null` |

- `event: "subagent-stop"` совпадает с тем, что внешний `except` в `main()` уже
  пишет как `event: mode` для `error` этого режима — один `event` на режим.
- `format_unfixed` — диагностика, не решение (D9, как `context_error`/
  `role_unresolved`): вызов пропущен, P7 его не считает (§7 считает только
  `deny`/`block`). Это событие не `decision: "block"` — второй раз не блокируем.
- `rule: "return_format"` — литерал, совпадает с тегом `[return_format]` в
  reason (G1-reason ниже) и с инвариантом «`rule` = id правила» для `stats`
  «top rules».
- `detail` — закрытое множество из двух кодов: `"key"` — провалена проверка 1
  (преамбула, code fence, другой ключ), `"value"` — проверка 2 (значение не из
  списка или пусто). Поле `detail` уже существует (`error`, `context_error`,
  `allow_unverified`); схема не расширяется. Первая строка ответа в журнал
  **не** пишется (может содержать секреты — тот же довод, что §7 о команде).
- `target: null` — **отклонение от плана** (план предлагал роль): §7 определяет
  `target` как объект вызова инструмента (токены команды / basename файла),
  у `SubagentStop` его нет; роль уже лежит в `role`, дубль провоцирует
  reader считать её именем файла.
- `dispatch_id` — существующий `dispatch_id()` без изменений, но на синтетическом
  payload: `dispatch_id({"transcript_path": payload.get("agent_transcript_path")
  or payload.get("transcript_path")})`. На `SubagentStop` `transcript_path` —
  транскрипт main (Context п.5), и `dispatch_id(payload)` вернул бы `null`;
  `agent_transcript_path` указывает на `subagents/agent-<id>.jsonl`, рядом с
  которым лежит `meta.json` с `toolUseId`.
- Успех → ни одной строки в журнале. Режим не вызывает `_note_role_unresolved()`,
  не трогает `guard-state.json` и не читает ничего в `.agentlog/` (AC6).
- Запись — только `_safe_write_event()` (journal best-effort, решение уже принято).

**Вывод `block`** (stdout, одна строка, `json.dumps(..., ensure_ascii=False)` —
уже делает `main()`):

```
{"decision": "block", "reason": "zprof guard [return_format]: ответ <role> должен начинаться строкой `<key>: <raw>`. Сейчас первая строка: «<first>». Перепиши ответ по return_format без преамбулы."}
```

- `<role>` — `resolve_role(payload)`; `<key>` — `verdict`/`completion` из
  контракта; `<raw>` — группа 2 AC2-regex **как в файле** (без нормализации:
  `approved | changes-required`, `merged-stamped|…|blocked-<reason>`, `<…>`).
- `<first>` — первая непустая строка ответа после `strip()`; длиннее 120
  символов → первые 120 + `…`. Кавычки-ёлочки и обратные кавычки — литералы
  шаблона, экранирования не нужно.

### G2. Корень поиска контракта: `project_root(payload)`

`root = project_root(payload)` — тот же helper, что у `pre_tool()`, без
изменений. Отличие от буквального AC1 `<cwd>`: если `$CLAUDE_PROJECT_DIR`
выставлен и является каталогом, он побеждает `payload["cwd"]`. В
задеплоенном хуке Claude Code выставляет `CLAUDE_PROJECT_DIR` = корень
проекта, где и лежит `.claude/agents/`; `cwd` сессии может быть подкаталогом.
Контракт и журнал читаются/пишутся в один и тот же корень — как в `pre-tool`.
Без `CLAUDE_PROJECT_DIR` поведение совпадает с AC1 буквально (тесты через
`_run_guard` снимают эту переменную).

### G3. Разбор frontmatter и блока `return_format` (AC2)

Чистая функция `_return_format_contract(text: str) -> tuple[str, str, list[str]
| None] | None` — `(key, raw, values)` или `None` (= pass). Алгоритм по
`text.splitlines()`:

1. **Открывающая граница:** строка 0 после `rstrip()` равна `"---"`. Иначе
   frontmatter нет → `None`. (`rstrip` — чтобы CRLF и хвостовые пробелы не
   ломали разбор; ведущие пробелы не допускаются.)
2. **Закрывающая граница:** первая следующая строка, у которой `rstrip() ==
   "---"`. Нет такой → frontmatter незакрыт → `None`. Всё дальше — тело
   агента, не просматривается никогда (там встречается `return_format` в прозе).
3. **Начало блока:** строка frontmatter, у которой `rstrip() == "return_format:
   |"`. Только индикатор `|`: `|-`, `|+`, `>`, инлайн-значение
   (`return_format: verdict: …`) → блок считается отсутствующим → `None`. Во
   всех 152 файлах — ровно `|`; расширение — отдельным решением, не молча.
4. **Конец блока:** первая строка, которая матчит
   `^[A-Za-z_][A-Za-z0-9_-]*:` (top-level ключ, без отступа), либо
   закрывающая `---` — что раньше.
5. **Внутри блока:** строки с `s = line.strip()`, где `s == ""` или
   `s.startswith("#")`, пропускаются, поиск продолжается (`# CRITICAL: …` в
   `pr-shepherd.md:8`, `auditor.md:11`, `architect.md`).
6. **Первая оставшаяся строка** матчит `^\s*(verdict|completion):\s*(.+?)\s*$`
   (AC2 дословно, `re.match`). Не матчит → `None` (pass, не ошибка). Строки
   после первой оставшейся не просматриваются: `verdict:` на второй значимой
   строке блока не ищется.
7. Два разных пути к `None` — «нет `return_format: |`» и «блок есть, первая
   значимая строка не `verdict:`/`completion:`» — оба pass, тестируются
   раздельно.

### G4. Список значений и элементы с плейсхолдером

Из группы 2 (`raw`):

1. Если `raw.strip()` целиком `^<.*>$` → `values = None` («любое», проверка 2
   пропускается). Проверяется **до** разбиения — иначе `<a | b>` развалился бы
   на `<a` и `b>`.
2. Иначе `items = [s.strip().lower() for s in raw.split("|")]`, пустые
   элементы отбрасываются. `strip()` каждого элемента — требование: покрывает
   `approved | changes-required` (`plan-reviewer.md:7`) и
   `done|blocked|failed` одинаково.
3. Если хоть один элемент целиком `^<.*>$` → `values = None` (весь список
   «любое», не поэлементно).
4. Иначе `values = items`.

**Элементы с `<` внутри (`blocked-<reason>`) — поддерживаются как префикс.**
Функция `_value_allowed(word: str, values: list[str]) -> bool`: для элемента,
содержащего `<`, `prefix = item.split("<", 1)[0]`; совпадение, если
`word.startswith(prefix) and len(word) > len(prefix)`. Для прочих — `word ==
item`. Итог — `any(...)` по элементам. Следствие:
`verdict: blocked-worktree-locked` от pr-shepherd проходит, `verdict: blocked-`
и `verdict: blockedfoo` — нет. Обоснование: это единственный живой контракт с
таким элементом, и он в продакшн-роли, которая мержит; ложный `block` здесь —
лишний круг на каждом `blocked-*`. Цена — пять строк; принятым риском это
оставлять незачем.

**Слово ответа** (проверка 2) — ровно как `_class_a_checks`
(`zprof-collect.py:1653`, `verdict_value`): `rest = first.split(":", 1)[1].strip()`;
`word = rest.split()[0].lower() if rest else ""`. Пунктуация не срезается
(`done.` ≠ `done`) — единая семантика с `verdict_value` коллектора. Пустое
слово → проверка 2 провалена (`detail: "value"`).

### G5. Финальный текст, источник и граница fail-open (AC3, AC7)

**Источник текста — отклонение от буквального AC3** (Context п.5).
`_final_text(payload) -> str | None`:

1. `payload.get("last_assistant_message")` — если `str`, это и есть текст
   (без чтения файлов). Основной путь в проде.
2. Иначе путь к транскрипту: `agent_transcript_path`, если непустая строка;
   иначе `transcript_path`, **только если** это транскрипт субагента
   (`_AGENT_TRANSCRIPT_RE.match(p.name) and p.parent.name == "subagents"` —
   тот же критерий, что в `resolve_role()`). Транскрипт main не читается никогда.
3. Нет ни того, ни другого → `None` (pass).

Чтение транскрипта — `_last_assistant_text(path: str) -> str | None`:

- `open(path, encoding="utf-8")`, построчно; строки с `strip() == ""`
  пропускаются; `json.loads(line)` **без** локального `try/except`; запись не
  `dict` → пропуск; запоминается последняя с `rec.get("type") == "assistant"`.
- Нет ни одной такой записи (пустой файл, только `user`/`system`/`attachment`)
  → `None` → pass (AC3).
- `message = rec.get("message")`; не `dict` → текст `""`. `content` — `str` →
  как есть; `list` → `"".join(b["text"] for b in content if isinstance(b, dict)
  and b.get("type") == "text" and isinstance(b.get("text"), str))`. Блоки
  `thinking`, `tool_use` и прочие игнорируются молча. Иное → `""`.

**Асимметрия ошибок — зафиксирована, не на усмотрение:**

| ситуация | природа | обработка | итог |
|---|---|---|---|
| файл контракта отсутствует/не читается (`OSError`) | штатно: роль без контракта | локальный `except OSError` в `_load_role_contract` (G6) | pass, журнал пуст |
| транскрипт: нет файла, нет прав, битый JSON в строке, невалидный UTF-8 | неожиданно: порча | **нет** локального `try/except`; `OSError`/`json.JSONDecodeError`/`UnicodeDecodeError` летят из `subagent_stop()` | внешний `except Exception` в `main()` пишет `decision: "error"`, `event: "subagent-stop"`, `detail` = класс; exit 0, stdout пуст |
| контракт найден, но невалидный UTF-8 | порча | не ловится (`UnicodeDecodeError` не `OSError`) | как строка выше |

Монтаж: в `main()` заглушка заменяется на
`elif mode == "subagent-stop": out = subagent_stop(payload)` внутри того же
единственного `try`. Прямой вызов `subagent_stop()` на битом транскрипте
**бросает** — тестируется `pytest.raises`; `exit 0` + `error` — через
`_run_guard(..., mode="subagent-stop")`.

**Принятый риск (fallback-путь).** Если `last_assistant_message` нет (старый
Claude Code, тесты), читается транскрипт, который в момент хука может быть не
дописан (§17): последняя `assistant`-запись — промежуточная реплика, ложный
`block`. Ограничено G1: максимум один лишний круг (`stop_hook_active` →
`format_unfixed`). Проверку `stop_reason` не вводим — поле ненадёжно в
частично записанном файле, а в проде путь 1 это покрывает.

### G6. Поиск файла контракта (AC1)

`_load_role_contract(root: str, role: str) -> tuple[str, str, list[str] | None]
| None`:

1. `role in ("main", "unknown")`, пустая, содержит `/` или `\`, или
   начинается с `.` → `None` без обращения к диску (защита от `agent_type`
   вида `../x`; `main`/`unknown` — не субагенты).
2. Кандидаты по порядку: `<root>/.claude/agents/<role>.md`,
   `<root>/.claude/agents/gates/<role>.md`.
3. `Path(c).read_text(encoding="utf-8")`; `OSError` (в т.ч.
   `FileNotFoundError`, `IsADirectoryError`, `PermissionError`) → следующий
   кандидат. Это штатный путь, не ошибка: событий не пишем.
4. **Первый прочитанный файл — окончательный.** Результат
   `_return_format_contract(text)` возвращается как есть, даже `None`:
   `agents/<role>.md` без `return_format` **не** проваливается в `gates/`.
5. Ни один не прочитан → `None` (pass).

Имена plugin-агентов (`plugin:name`) просто не найдутся → pass.

### G7. Пустой финальный текст

Текст `""` или только из пробельных символов → pass (как «нет записи»), без события.

### G8. `subagent_stop(payload) -> dict | None` — порядок

1. `role = resolve_role(payload)`; `root = project_root(payload)`.
2. `contract = _load_role_contract(root, role)`; `None` → `return None`.
3. `text = _final_text(payload)`; `None` или `not text.strip()` → `return None`.
4. `first` = первая строка `text.splitlines()` с непустым `strip()`, после `strip()`.
5. Проверка 1: `first.lower().startswith(key + ":")`. Провал → `detail="key"`.
6. Проверка 2 (только если `values is not None`): слово по G4,
   `_value_allowed(word, values)`. Провал → `detail="value"`.
7. Обе прошли → `return None`, журнал не трогается.
8. Провал и `payload.get("stop_hook_active") is True` → `format_unfixed`
   (G1), `return None`.
9. Иначе → событие `subagent-stop`/`block` (G1), вернуть `{"decision":
   "block", "reason": …}`.

Контракт загружается **до** чтения транскрипта: роль без контракта не
открывает файлы вообще (битый транскрипт у такой роли — pass, не `error`).

### G9. Прочие правки

- Docstring модуля: `Modes: pre-tool | subagent-stop`, убрать «no-op in this
  issue, see #26»; описание вывода — «deny (pre-tool) или block
  (subagent-stop)»; добавить `ADR: … 0007-guard-subagent-stop-validator.md` и
  `§6` в строку Spec.
- `resolve_role`, `dispatch_id`, `project_root`, `_now_ts`, `write_event`,
  `_safe_write_event` — без изменений.
- Тесты — новый `profiles/base/tests/test_guard_subagent_stop.py`; хелперы из
  `test_guard.py` (`zprof_guard`, `_read_events`, `_run_guard`). Payload
  `subagent-stop` собирается в самом тестовом файле (`_payload` из
  `test_guard.py` заточен под `tool_name`). Транскрипт-фикстуры для fallback
  кладутся в `<tmp>/<sess>/subagents/agent-<id>.jsonl` (иначе G5 п.2 их не
  прочитает); кейсы AC3/AC7 про транскрипт — **без** `last_assistant_message`.
  Кейсы ADR сверх AC7: `blocked-worktree-locked` на живом `pr-shepherd.md` →
  молчание; `blocked-` → block; `plan-reviewer.md` c пробелами → оба значения
  проходят; `last_assistant_message` приоритетнее транскрипта; `transcript_path`
  main-уровня не читается; `agents/<role>.md` без `return_format` не
  проваливается в `gates/`; успех не создаёт `guard-events.jsonl` и
  `guard-state.json`.

### G10. Отклонения от плана и от буквального текста AC

| источник | было | решение | почему |
|---|---|---|---|
| AC1 | `<cwd>` | `project_root(payload)` | G2: один корень с `pre-tool`; без `CLAUDE_PROJECT_DIR` совпадает |
| AC3 | `transcript_path` | `last_assistant_message` → `agent_transcript_path` → subagent-`transcript_path` | Context п.5: `transcript_path` — транскрипт main |
| план шаг 1 п.1 | `target` = роль | `target: null` | G1: `target` — объект tool-вызова |
| план шаг 1 п.1 | `detail` не упомянут | `detail: "key"\|"value"` | различать преамбулу и неверное значение без текста ответа |
| план шаг 3 | `_final_assistant_text(transcript_path)` | `_final_text(payload)` + `_last_assistant_text(path)` | выбор источника требует payload |
| план шаг 3 | `dispatch_id(payload)` | `dispatch_id` на `agent_transcript_path` | G1: иначе всегда `null` |

## Consequences

- `main()` получает одну ветку `elif`; `pre_tool()`, движок правил, `CONTEXTS`,
  `guard.yaml`/`guard.json` — без изменений. Выключить валидатор отдельно от
  `pre-tool` через `guard.yaml` нельзя — только снятием хука `SubagentStop`
  (`guard.enabled: false`, деплой — #28).
- В журнале появляются `event: "subagent-stop"`/`decision: "block"` (P7
  считает, §7) и `event: "format_unfixed"`/`decision: null` (не считает).
  Схема D9 не расширяется: новые только значения `event`, `rule`, `detail`.
- Каждый `block` стоит субагенту одного дополнительного хода; бесконечного
  цикла нет (G8 п.8).
- Живые роли, которые сегодня отвечают с преамбулой или в code fence, начнут
  получать `block` сразу после деплоя хука (#28) — это цель, но shakedown
  (§11) должен пройти до включения по умолчанию.
- Фаза 2 (§12 п.3): источник контракта — `verdicts.yaml`; заменяется только
  `_load_role_contract`, `subagent_stop()` и G1 остаются.
- **Открытый риск, вне фазы 1.** `SubagentStop` стреляет и на ходе субагента,
  который ждёт собственного фонового диспатча (реальный транскрипт
  task-runner этого issue: «Architect dispatched… Waiting for completion»).
  Такой ответ получит `block`. Наблюдать по `detail: "key"` у `task-runner`
  после деплоя; решение (например, pass при `running` в `background_tasks`) —
  отдельным issue.

## Alternatives considered

- **Буквальный AC3 (`transcript_path`)** — на `SubagentStop` это транскрипт
  main; ложные исходы на каждом субагенте. Отвергнуто (G5).
- **Только `agent_transcript_path`, без `last_assistant_message`** —
  транскрипт в момент хука не дописан (§17 С1), финальный ход может
  отсутствовать → ложный `block`. Оставлено fallback'ом, не основным путём.
- **Проверка `stop_reason == "end_turn"` в fallback** — поле ненадёжно в
  частично записанном файле; прод покрыт `last_assistant_message`. Отвергнуто.
- **Один `try/except` вокруг всего `subagent_stop()`** с возвратом `None` —
  битый транскрипт стал бы молчаливым pass, AC7 (`error` в журнале) не
  выполнен, внешний fail-open продублирован. Отвергнуто (G5).
- **`blocked-<reason>` не поддерживать, записать принятым риском** — ложный
  `block` у роли, которая мержит, на каждом `blocked-*`. Отвергнуто (G4).
- **Элемент с `<` внутри = «любое»** — `verdict: whatever` прошёл бы у
  pr-shepherd. Отвергнуто в пользу префикса.
- **`target` = роль** (план) — дублирует `role`, ломает смысл `target` из §7.
  Отвергнуто (G1).
- **`format_unfixed` как `decision: "block"`** — P7 посчитал бы нарушение,
  которого не было как решения (вызов пропущен). Отвергнуто.
- **Писать первую строку ответа в `detail`** — возможные секреты в журнале.
  Отвергнуто; первая строка есть только в `reason` (уходит субагенту).
- **Поддержать `|-`/`>`/инлайн `return_format`** — нет ни одного такого
  файла; каждое расширение без фикстуры — непроверенный код. Отвергнуто до
  первого реального случая.
- **Пустой ответ → `block`** — пустой текст в основном возникает из неполных
  данных (fallback, запись без text-блоков); ложный `block` дороже пропуска,
  коллектор всё равно фиксирует `return_parsed: false`. Отвергнуто (G7).
- **Буквальный `payload["cwd"]` (AC1)** — расходится с `pre-tool` по корню
  журнала при `CLAUDE_PROJECT_DIR` ≠ `cwd`. Отвергнуто (G2).
