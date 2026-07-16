# Dev pipeline

Основной поток. Overlay подставляет свои stack-aware агенты
(architect / implementer / tester / bug-hunter / refactor-agent / explorer).

## Dispatch table
| Задача | Первый агент |
|---|---|
| Новая feature | `dev-orchestrator` |
| Багфикс | `bug-hunter` (overlay-specific) → `dev-orchestrator` если нужен полный pipeline |
| Только дизайн | `architect` (overlay-specific) |
| Только code review | `reviewer` (overlay-specific) |
| Только тесты | `tester` (overlay-specific) |
| Рефакторинг без feature | `refactor-agent` (overlay-specific) |
| Read-only investigation | `explorer` (overlay-specific) |

## Изоляция (обязательные правила main'а)
1. Не цитировать output subagent'а — только return_format schema.
2. ≤3 строки в followup.md после каждого dispatch.
3. Vocal self-check перед dispatch: «читаю поле <X> из результата».
