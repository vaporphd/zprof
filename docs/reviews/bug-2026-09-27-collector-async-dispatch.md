# Bug: коллектор не сшивает async Agent-dispatch с `<task-notification>` без `<tool-use-id>` (#34)

Дата: 2026-09-27 · bug-hunter · файл: `profiles/base/zprof-collect.py` (совпадает с `.claude/zprof-collect.py`, `diff -q` пуст)

## Симптом

- В `.agentlog/dispatches.jsonl` 49 async-dispatch'ей; у 46 есть строка с `dispatch_complete: true`, у **3 — нет, и все 3 это `task-runner`** из сессии `63994636…` (`toolu_017s1UWE…`, `toolu_01Guyc7p…`, `toolu_01EBesUP…`). У каждого одна строка `seq: 0, status: async_launched, verdict: null`.
- `zprof score` (`cli/internal/score/reader.go:115`) считает корнем run'а только строку с `role == "task-runner" && dispatch_complete`; таких нет → «unscored».
- В `.agentlog/collect.log` ровно два `session 63994636…: 2 unparsed lines (format drift?)` (15:29Z и 16:21Z) — это и есть выброшенные уведомления (queue-operation + user-дубль), по одному логическому уведомлению на каждый из первых двух task-runner'ов. `losses` в state.json = 0: потеря сейчас не учитывается как loss.

Уточнения к фактам issue (на фикс не влияют):
- `ts_utc` у зависших строк **не null** — это ts запуска (`14:57:12.716Z`, `15:30:39.486Z`). Null только `verdict` и `ext.run_id` (у двух из трёх).
- Реальных уведомлений без `<tool-use-id>` в транскрипте — 4 записи = 2 логических (по одному на task-runner, каждое продублировано queue-operation + user). Цифры 11/154 в issue — сырой grep, в который попадают упоминания тега в тексте разговора. Третий task-runner (`toolu_01EBesUP…`) на момент проверки ещё в работе — уведомления нет.
- В транскрипте async `toolUseResult` содержит `agentId, canReadOutputFile, description, isAsync, outputFile, prompt, resolvedModel, status` — **без `agentType`**; роль берётся из `subagent_type` у tool_use. meta.json содержит `agentType, toolUseId, spawnDepth, …`.

## Репродукция

```bash
python3 -m pytest profiles/base/tests/test_async_notification_no_tool_use_id.py -v
# 5 failed; остальные 218 passed
```

Тест-файл `profiles/base/tests/test_async_notification_no_tool_use_id.py` (не закоммичен — это git-операция для main). Сценарии:

| тест | AC | что проверяет | сейчас |
|---|---|---|---|
| `test_parse_keeps_notification_without_tool_use_id` | AC1 | парсер возвращает dict с `task_id`, `result` | `None` |
| `test_meta_json_resolves_dispatch_in_process` | AC1(a), AC2, AC3 | один кусок лога + meta.json, `require_completion_evidence=True` → dispatch `TUID` completed, `agent_id`, `ts_utc` = ts уведомления, `returned`, агент в `agents_done` | только `async_launched` |
| `test_meta_json_split_stops_yields_scoreable_root` | AC1(a), форма AC5 | два Stop через subprocess (запуск → уведомление), последняя строка dispatch'а: `dispatch_complete`, `verdict: done`, `role: task-runner` | `async_launched` |
| `test_fallback_launch_map_split_stops` | AC1(b), AC4 | нет meta.json, резолв по `agentId` из запуска | `async_launched` |
| `test_unresolvable_notification_records_loss` | AC1 (negative) | неизвестный task-id → строка остаётся `async_launched`, `losses >= 1` | `losses == 0` |

Живое подтверждение без приватного транскрипта:

```bash
python3 -c "import json;[print(r['dispatch_id'][-30:],r['seq'],r['status'],r.get('verdict')) for r in map(json.loads,open('.agentlog/dispatches.jsonl')) if r.get('role')=='task-runner']"
grep 'unparsed lines' .agentlog/collect.log
```

## Root cause

`profiles/base/zprof-collect.py`:

- **L387–412 `_parse_task_notification_xml`** — L412 `return result if "tool_use_id" in result else None`. Если `<tool-use-id>` нет, возвращается `None` даже при наличии `<task-id>`.
- **L586–626 Path 2 & 3 в `_extract_dispatches_from_text` (L436)** — L587 вызов парсера; L588 `if notif:` → иначе L625–626 `unparsed += 1` (отсюда «unparsed lines» в collect.log). L589 `tool_use_id = notif["tool_use_id"]` — жёсткая зависимость от ключа; L595 дедуп и L601 `notify_seq` тоже по `tool_use_id`; L611 `agent_id = notif.get("task_id")`; L614–615 `<result>` → `dispatch["returned"]`. Строка уведомления **не несёт `role`**.
- **L347 `_IN_FLIGHT_STATUSES`** — корректен, трогать не нужно.
- **L529–553 async-ветка Path 1&2** — пишет `dispatch["agent_id"] = tur["agentId"]` (L552) при `dispatch_id = tool_use_id`: здесь уже есть всё для карты (b) `agentId → (tool_use_id, role)`, где роль из `pending.subagent_type` (L538).
- **L1045 `_collect_subagent_transcripts`** — L1093 `subagents_dir = tp.with_suffix("") / "subagents"`, L1104–1113 читает все `agent-*.meta.json` в `metas[agent_id]`; L1144 и L1162 `meta.get("toolUseId")`. Pass 2 (L1159–1287): при `require_completion_evidence` (L1163–1168) агент финализируется, только если в `dispatches` есть dict с `dispatch_id == toolUseId` и `dispatch_complete`. Поэтому после резолва Path 2/3 даёт такой dict — и **AC3 выполняется автоматически, отдельной логики не нужно**. Роль строке уведомления сейчас достаётся только из enrichment pass 2 (L1235–1236, L1255–1257).
- **L1539 `_class_a_checks`**, вызов в `_normalize_dispatch` L1699–1717 — работает от `raw["returned"]`; раз L615 уже кладёт `<result>` в `returned`, **для AC2 менять ничего не нужно**, достаточно, чтобы dict не выбрасывался.
- Дедуп строк — `_load_dedup_set` L1728 по `(dispatch_id, seq)`; строка уведомления получает `seq >= 1` из `notify_seq` и не конфликтует с `seq 0` запуска.

Нарушенное допущение: «в каждом `<task-notification>` есть `<tool-use-id>`». Текущий Claude Code его не гарантирует.

## Где резолвить `tool_use_id` (п.4 задачи)

1. **Парсер (L412) должен стать чистым**: возвращать dict, если есть `task_id` **или** `tool_use_id`, и ничего не резолвить. У него на входе только `text`, контекста (путь к транскрипту, карта) нет и быть не должно. Резолв — **после** парсера, в Path 2/3 (между L588 и L589), до вычисления `dedup_key` / `notify_seq`, чтобы дедуп и seq шли по уже резолвнутому id.
2. **`_extract_dispatches_from_text` не знает пути к транскрипту** (сигнатура `(session_id, raw, notify_seq, seen_notifications)`). Нужен новый необязательный параметр, например `agent_index: dict[agent_id -> {"tool_use_id", "role"}] | None`. Вложенные вызовы (L1140, pass 1) и тесты с позиционными аргументами не должны сломаться.
3. **Источник (a), meta.json** — собирается в `_extract_main_log` (L632): у неё есть `path`, subagents-dir = `path.with_suffix("") / "subagents"` (тот же рецепт, что L1093). `toolUseId` + `agentType` (роль!) из `agent-<task_id>.meta.json`. Лучше вынести чтение метаданных в общий хелпер, который используют и L1104–1113, и `_extract_main_log`, чтобы не держать два glob'а.
4. **Источник (b), карта запусков — главная ловушка: инкрементальный offset.** `_extract_main_log` читает только новые байты с `main_log_offset` (L650–661). В реальном потоке Stop после хода запуска уже прочитал `toolUseResult` с `agentId`, а уведомление приходит в следующем куске. Карта, построенная только по текущему `raw`, в проде fallback (b) **никогда не сработает** (тест `test_fallback_launch_map_split_stops` моделирует ровно это). Варианты для implementer'а (выбор за ним): сохранять карту `agentId → {tool_use_id, role}` в `sess` рядом с `notify_seq` (L306–307 / L677) либо восстанавливать её из уже записанных строк `.agentlog/raw/<session>.jsonl` (в raw есть `agent_id` + сырой `dispatch_id`; в `dispatches.jsonl` `agent_id` нет — см. ниже). Порядок: сначала (a), потом (b), в текущем куске и в сохранённой карте.
5. **Loss (AC1)**: если ни (a), ни (b) не нашли id, нужно отдать счётчик наружу через `out`/`meta` (по аналогии с `unparsed_lines`), а в `_collect_session` вызвать `self.state.increment_losses(n)` (L199). Сейчас такие уведомления попадают в `unparsed_lines` → «format drift?», и это вводит в заблуждение.
6. **Роль на строке уведомления — обязательна для AC5.** `BuildRuns` (reader.go L113–116) берёт *последнюю* строку по `dispatch_id` и проверяет на ней `role == "task-runner"`. Роль на строку уведомления сейчас ставит только pass 2 enrichment. Pass 2 пропускает агентов из `agents_done` (L1125). На живых данных `a60d8c64` (`toolu_017s1UWE…`) уже лежит в `agents_done`: старая версия коллектора до #22 записала для него meta-only строку `seq 0, completed` (`raw/…jsonl`, строка 104), и дедуп её выкинул из-за `seq 0` у `async_launched`. Значит, при AC5-прогоне строка уведомления этого dispatch'а **не получит роль от pass 2**. Резолвер должен сам ставить `role` из meta.json `agentType` / из карты (b). Это не расширение scope, а условие AC5. Попутно: 7 уже существующих completed-строк `seq > 0` в `dispatches.jsonl` без `role` — тот же механизм; их лечить в рамках #34 не нужно.

## Расхождения с формулировками AC

- **AC4 «существующие тесты (218) зелёные»** противоречит AC1: `tests/test_main_log_extraction.py` `TestParseTaskNotificationXml.test_missing_tool_use_id_returns_none` (L444–446) закрепляет текущее багованное поведение. Его нужно **перевернуть** (ожидать dict с `task_id`), а не держать зелёным.
- **AC4 «строка dispatch получает `agent_id: X`»**: `_normalize_dispatch` (L1630–1725) не копирует `agent_id` в `dispatches.jsonl` (в живом файле 0 из 119 строк с этим ключом). `agent_id` проверяется на raw-dict (`_extract_main_log` / `.agentlog/raw/<session>.jsonl`), как в repro-тестах. Добавлять `agent_id` в схему строки — вне scope.
- **AC5 на живых данных**: offset сессии `63994636…` в state.json (`main_log_offset = 9567104`) уже за уведомлениями. Простой повторный Stop их не перечитает. Для ручной проверки: stop-прогон с копией `.agentlog` и сброшенными `main_log_offset`/`main_log_head_sha` для этой сессии, либо (вне scope) SessionStart-recovery. Дедуп по `(dispatch_id, seq)` защитит от дублей при перечитывании.

## Предложенный fix (текстом)

1. `_parse_task_notification_xml`: условие возврата — есть `task_id` или `tool_use_id`. Докстринг обновить.
2. `_extract_dispatches_from_text`: необязательный параметр с индексом агентов. В Path 2/3, если `tool_use_id` нет, резолвить по `task_id`: (a) meta-индекс, затем (b) карта запусков (текущий кусок + сохранённая). Если резолва нет — счётчик `unresolved_notifications` в `out`, `continue`. В async-ветке Path 1&2 (L550–553) пополнять карту `agentId → {tool_use_id, role}`. На резолвнутую строку уведомления ставить `role`, если он известен.
3. `_extract_main_log`: собрать meta-индекс из `path.with_suffix("")/subagents/agent-*.meta.json` (общий хелпер с L1104–1113), передать карту запусков из `sess` и вернуть обновлённую в `meta` (как `notify_seq`).
4. `_collect_session` (L286–336): сохранить карту в `sess`, `increment_losses` по `unresolved_notifications`, «unparsed lines» писать только для реально нераспарсенного.
5. Тесты: перевернуть L444–446; `test_async_notification_no_tool_use_id.py` должен позеленеть целиком. Если implementer выберет другую форму хранения карты, тест-хелперы менять не придётся: split-тесты идут через subprocess `stop`.
6. AC5/AC6: ручной прогон (см. выше), затем `zprof apply --telemetry-only` и пустой `diff profiles/base/zprof-collect.py .claude/zprof-collect.py`.

Реализация в implementer'е должна закрыть **AC1–AC6 из issue #34**.
