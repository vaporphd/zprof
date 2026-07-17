# Agent loop router

Определи профиль по ключевым словам и Read соответствующий workflow-файл до диспатча первого агента.

| Ключевые слова | Профиль | Файл workflow |
|---|---|---|
| новая задача, следующая задача, next task, take next, план, plan, run pipeline, багфикс, bugfix, feature | dev-pipeline | workflows/dev-pipeline.md |
| исследуй, разбери, reverse-engineer, распакуй, explore, investigate | exploratory | workflows/exploratory.md |

Если сомневаешься — спроси через AskUserQuestion.

## Роли → агенты
Смотри `.claude/agents/*.md` — каждый агент имеет frontmatter `description` +
`return_format` + `tools:` whitelist. Диспетч по имени. Overlay-specific роли
(`architect`, `implementer`, `tester`, ...) namespace-ятся когда применено ≥ 2
overlay'а — см. `.zprof.yaml` за списком активных.

## Изоляция (обязательные правила main'а)
1. Не цитировать output subagent'а — только return_format schema.
2. ≤3 строки в followup.md после каждого dispatch.
3. Vocal self-check перед dispatch: «читаю поле <X> из результата».
