## Doctrine (не изменять — управляется zprof)

Этот проект использует zprof — слоеная система agent-loop.

- `AGENT_LOOP.md` — контракт диспатча. Читать при старте каждой loop-итерации.
- `.claude/agents/*.md` — доступные subagent'ы, каждый с return_format schema.
- `followup.md` — living snapshot (Status + Next, ≤20 строк).
- `lessons.md` — corrections ledger (~15 записей, overflow → lessons-archive.md).
- `todo.md` — canonical task list.
- `.zprof.yaml` — какие overlays применены + model overrides.

### Изоляция
Main-сессия НИКОГДА не цитирует output subagent'а. После каждого dispatch:
запиши ≤3 строки в followup.md, дропни ответ subagent'а из рабочей памяти.

### Свои правила
Пиши ниже managed-блока — этот раздел не трогается при `zprof sync`.
