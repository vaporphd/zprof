# Bug #52 — коллектор не сшивает вложенные async-диспатчи с task-notification

Дата: 2026-09-28. Источник: bug-hunter (диагноз инлайн, без Write) + подтверждение на реальном
`.agentlog/` этого репозитория. Baseline до фикса: `python3 -m pytest profiles/base/tests/ -v` —
**501 passed**.

## Симптом

В issue #52 гипотеза была «вложенные диспатчи вообще не проходят стежку» — это оказалось
неточным. Pass 1 в `_collect_subagent_transcripts` вызывает `_extract_dispatches_from_text` для
транскрипта раннера точно так же, как для main-лога. Но все 14/14 детей run #25
(`claude-code:63994636-…:toolu_01WTwCL2GnE8qofgV1ibMTyB`) оставались `status: async_launched`,
`verdict: null` — `zprof score --run <id>` считал P6 = 0.579 (15 штрафных баллов, «58% токенов в
dispatch'ах без результата»), итоговый score 47 (Lucky) вместо честного.

## Root cause (два независимых дефекта)

1. **Основной.** Код стежки (`_extract_dispatches_from_text`, блок Path 2 & 3) искал
   `<task-notification>` только в записях `type=="queue-operation"` и `type=="user"`. В
   транскриптах раннера (`subagents/agent-<id>.jsonl`) Claude Code пишет уведомления иначе:
   `type=="attachment"`, `attachment.commandMode=="task-notification"`, XML — в
   `attachment.prompt` (копии в `attachment.rendered`/`renderedInHumanTurn`, не разбираются —
   дубли для контекста модели). Этой формы код не знал вообще.
2. **Вторичный.** Pass 1 (`_collect_subagent_transcripts:1285`, вызов nested-извлечения) не
   передавал `agent_index`, поэтому уведомления без `<tool-use-id>` для вложенных детей не могли
   резолвиться через `subagents/agent-<task_id>.meta.json` — в отличие от main-лога, где
   `agent_index` строится из meta.json и передаётся в `_extract_main_log`.

## Репродукция (на реальных данных)

`agent-a06e9e4962f60f9f6.jsonl` (task-runner run #25, 559 записей `type=="attachment"`, 15 из
них — `commandMode=="task-notification"`) — пример реальной записи:

```json
{"type": "attachment", "attachment": {
    "type": "queued_command",
    "prompt": "<task-notification>\n<task-id>a31ecf267bca405d5</task-id>\n<tool-use-id>toolu_01X8kxCM8ZUsD9N7BxfrTUJz</tool-use-id>\n...\n<result>verdict: misaligned\n...</result>\n<usage><subagent_tokens>12888</subagent_tokens>...</usage>\n</task-notification>",
    "commandMode": "task-notification",
    "usage": {"totalTokens": 12888, "toolUses": 4, "durationMs": 14844}
}}
```

53 таких записей в четырёх транскриптах run #25; 3 без `<usage>` в XML (background-command
уведомления без `<result>`), 0 без `<tool-use-id>` — но во втором наблюдаемом сценарии
(main-лог, 63994636…) 3 из 3 top-level `attachment`-уведомлений дублируются
`queue-operation`/`user`-парой того же tool_use_id/status — дедуп должен схлопывать все три
источника в одну запись.

## Фикс

- `_extract_dispatches_from_text`: третья ветка детекции уведомления —
  `rec_type == "attachment"` + `attachment.commandMode == "task-notification"` →
  `notification_text = attachment["prompt"]`. Если в XML нет `<usage>`, поля `subagent_tokens` /
  `tool_uses` / `duration_ms` добираются из `attachment["usage"]` (camelCase:
  `totalTokens`/`toolUses`/`durationMs`), но только когда XML их не дал (XML побеждает).
  Дедуп (`seen_notifications`, ключ `(tool_use_id, status)` / `("task:"+task_id, status)`)
  общий для всех трёх форм — без изменений в логике, просто ещё один источник `notification_text`.
- `_collect_subagent_transcripts`: перед Pass 1 строится общий `agent_index` из всех
  `metas` (плоский `subagents/`, внуки лежат рядом с детьми своего родителя), передаётся в
  каждый nested-вызов `_extract_dispatches_from_text` — тем самым уведомления без
  `<tool-use-id>` резолвятся так же, как в main-логе.

## Проверка на реальном `.agentlog/` (AC4)

Скопирован `.agentlog/` в scratch, в копии `state.json` убран `a06e9e4962f60f9f6` (task-runner
run #25) из `agents_done` (иначе Pass 1 пропускает уже финализированного родителя и не
пересканирует его транскрипт), прогнан `zprof-collect.py stop` с `ZPROF_AGENTLOG=<scratch>` и
реальными `transcript_path`/`session_id` (только чтение реальных транскриптов, запись — в
scratch-копию; настоящий `.agentlog/` не тронут).

| | до | после |
|---|---|---|
| nested `async_launched` (14 детей run #25) | 14 | 0 |
| P6 value / points | 0.579 / 15 | 0 / 0 |
| score / tier | 47 / Lucky | 62 / Solid |

## Тесты

`profiles/base/tests/test_nested_async_attachment_stitch.py` — фикстура «main → task-runner → 3
ребёнка»: один с `<tool-use-id>` в attachment-уведомлении, один без (резолв через meta.json /
`agent_index`), третий без уведомления вовсе → первые два `completed` с `verdict`/
`parent_dispatch_id`, третий остаётся `async_launched`. Отдельно: дедуп attachment vs
queue-operation/user дублей (seq не растёт), backfill `attachment.usage` только когда в XML нет
`<usage>`. Существующие `test_nested_dispatches.py`, `test_async_notification_no_tool_use_id.py`,
`test_main_log_extraction.py` — без изменений, зелёные (501 → 506 passed).
