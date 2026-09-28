# Bug #36 — нерезолвленное task-notification: loss x2 и нет следа в collect.log

Дата: 2026-09-28. Источник: review PR #35 (issue #34), P2-1 и P2-2. Диагноз ревьюера подтверждён на фикстуре.
Baseline до фикса: `python3 -m pytest profiles/base/tests/ -q` — **225 passed**.

## Симптом

- **P2-1.** Одна реальная потеря (уведомление с неизвестным `<task-id>` без `<tool-use-id>`) даёт `state.json.losses == 2`, а не 1.
  Claude Code пишет каждое уведомление дважды (`queue-operation` + `user`, ~15 мс), и обе копии считаются.
- **P2-2.** В `.agentlog/collect.log` ничего не появляется (файл даже не создаётся). `task_id` потерянного уведомления нигде не сохраняется, поэтому на вопрос «почему нет карточки» ответить нечем.

## Репродукция

Хелперы берутся из `profiles/base/tests/test_async_notification_no_tool_use_id.py`:
`_notification_lines(task_id)` возвращает пару-дубль (queue-operation + user с одинаковым XML),
а `_split_stops(tmp_path, with_meta=False, notif_task_id=...)` запускает два subprocess-Stop'а: в первом только launch, во втором только пара уведомлений.

```python
import json, sys
sys.path.insert(0, "profiles/base/tests")
import test_async_notification_no_tool_use_id as t
UNKNOWN = "a0unknown00000000"

def test_p2_1_in_process_one_logical_notification_counts_once():
    raw = "\n".join(t._notification_lines(UNKNOWN))
    out = t.zprof_collect._extract_dispatches_from_text(t.SESSION, raw, {}, set(), agent_index={})
    assert out["unresolved_notifications"] == 1          # факт: 2

def test_p2_1_end_to_end_losses_equal_one(tmp_path):
    agentlog = t._split_stops(tmp_path, with_meta=False, notif_task_id=UNKNOWN)
    assert json.loads((agentlog / "state.json").read_text())["losses"] == 1   # факт: 2

def test_p2_2_collect_log_mentions_unresolved_task_id(tmp_path):
    agentlog = t._split_stops(tmp_path, with_meta=False, notif_task_id=UNKNOWN)
    log = agentlog / "collect.log"
    assert UNKNOWN in (log.read_text() if log.exists() else "")   # факт: файла нет
```

Результат на `6089d31`: все 3 падают. `assert 2 == 1`, `losses: 2`, `collect.log exists = False`.
Ключи `out` сейчас такие: `['dispatches', 'harness_version', 'truncated', 'unparsed_lines', 'unresolved_notifications']`, списка task_id среди них нет.

## Root cause

`profiles/base/zprof-collect.py`:

- **P2-1.** Строки 628–638 в `_extract_dispatches_from_text`: ветка `else:` выполняет `unresolved_notifications += 1; continue` (стр. 637–638).
  Это происходит **до** дедупа на стр. 641–647 (`dedup_key = (tool_use_id, notif_status)` / `seen_notifications`), поэтому вторая копия пары до дедупа не доходит.
  Существующий тест `test_unresolvable_notification_records_loss` (тестовый файл, стр. 171–176) проверяет только `losses >= 1` и удвоение не ловит.
- **P2-2.** В `_extract_dispatches_from_text` (return на стр. 683–685) и `_extract_main_log` (стр. 740) пробрасывается только count.
  В `_collect_session` (стр. 310–313) есть только `self.state.increment_losses(...)`, без `_log_error`. Для сравнения, у `unparsed_lines` такой лог есть (стр. 314–316).

## Предложенный fix (для implementer)

1. **P2-1.** В ветке нерезолвленного уведомления (стр. 633–638) нужно определить `notif_status` раньше и собрать ключ `("task:" + task_id, notif_status)`.
   Если ключ уже есть в `seen_notifications`, делать `continue` без инкремента. Иначе добавить ключ, сделать `unresolved_notifications += 1` и `continue`.
   Префикс `task:` исключает коллизию с ключами `(tool_use_id, status)`. Если `task_id` пуст, при текущем парсере это невозможно (стр. 421 требует хотя бы один id), но на всякий случай можно считать без дедупа.
2. **P2-2.** В `_extract_dispatches_from_text` собирать `unresolved_task_ids: list[str]` (без дублей, в порядке появления) и вернуть его в dict. Докстринг (стр. 452) тоже обновить.
   В `_extract_main_log` сделать `meta["unresolved_task_ids"] = out[...]`. В `_collect_session` рядом с `increment_losses` вызвать
   `_log_error(self.agentlog, f"session {session_id}: {n} unresolved task-notifications (task_ids={...})")`.
3. **Тесты (для tester).** В `test_unresolvable_notification_records_loss` заменить `>= 1` на `== 1` и добавить проверку, что `"a0unknown00000000"` есть в `agentlog / "collect.log"`.
   Добавить in-process тест на дедуп пары (первый тест из репро) и тест на две **разные** неизвестные task_id, для которого ожидается `== 2`: это страхует от пересушивания.
   Целевое количество: 225 + новые, все зелёные.

## Остаточный риск (вне скоупа #36, к сведению)

`seen_notifications` создаётся заново на каждый вызов `_extract_main_log` (стр. 722) и не сохраняется в `sess`.
Если Stop сработает между `queue-operation` и `user` записями одной пары, дубль будет посчитан дважды, как и для резолвленных уведомлений.
На фикстуре это не воспроизводилось, окно ~15 мс.
