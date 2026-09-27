# Review: #16 — pr-shepherd в local-green гоняет тесты, видит rulesets (раунд 2)

**Date:** 2026-09-27
**Branch:** feat/issue-16-pr-shepherd-local-green-tests
**Head:** 75563e4ef0c20a5d89b651f75a6c215679765311
**Run:** 2026-09-27-pr-shepherd-local-green-tests
**Verdict:** approve

## Scope
`git diff origin/main...HEAD` целиком: `profiles/base/agents/pr-shepherd.md` (+ зеркало
`.claude/agents/pr-shepherd.md`), `docs/adr/0002-pr-shepherd-test-command-source.md`,
`docs/superpowers/specs/2026-09-27-guard-hooks-design.md` (коммит `bc72660`), `followup.md`, `todo.md`.

## Проверка P1 раунда 1
- §3 шаг 2 (стр. 102): формулировка «reserved managed heading — NEVER a source» удалена.
  Теперь явно: T1 сканирует весь `CLAUDE.md` независимо от секции, включая ручной текст
  под `## Executing`. Утверждение, что таблица Agent|Scope не содержит меток, дано как факт
  о содержимом таблицы, а не как исключение секции из поиска. Противоречия с ADR 0002 §Decision 2 нет.
- §8 (стр. 180): «never `## Executing`» удалено; сказано «T1 label anywhere, section-independent».
- Стр. 111 (`no-test-command` → docs-writer «НЕ в ## Executing»): это совет, куда
  вписывать новую строку (managed-блок будет перезаписан), а не правило поиска. Корректно.
- Self-hosting кейс zprof: `CLAUDE.md:88` — `` Build: `…`. Test: `cd cli && go test ./...` ``
  по правилу T1(a) даёт `cd cli && go test ./...`; `CLAUDE.md:90` даёт
  `python3 -m pytest profiles/base/tests/ -v`. T1 непуст, T2 не используется. Merge этого PR
  в local-green пройдёт.

## Остальное
- AC1 rulesets: проверяются оба источника (classic protection и `rules/branches/<default>` с
  `type: required_status_checks`), любой из них → CI-green. OK.
- Verdict enum: `local-tests-failed` добавлен в return_format и в шаблон §7; маршруты
  `next:` для failed / `no-test-command` заданы. OK.
- T2: кавычки снимаются, `<…>` пропускается. OK.
- Зеркало: `diff profiles/base/agents/pr-shepherd.md .claude/agents/pr-shepherd.md` пуст.
- ADR 0002 закоммичен, стухшего SHA в шапке нет. `followup.md` и `todo.md` актуальны.
- `.gitignore` покрывает `.agentlog/` и `.zprof/runs/`. Секретов нет.
- Gate: `cd cli && go test ./...` 285/285 passed; `python3 -m pytest profiles/base/tests/ -v` 224/224 passed.

## Findings
- **P0:** нет. **P1:** нет.
- **P2 (принято):** коммит `bc72660` (guard-спека, `branch_pr_merged`) попадает в PR. Main-сессия
  подтвердила это заранее.
- **P2 (follow-up, не для #16):** PR может ослабить свой собственный `Test:`-гейт, поменяв
  `CLAUDE.md` в head-ветке, потому что команды читаются из head. Стоит завести отдельный
  issue (например, брать команды из base-ветки или требовать их совпадения).
- **P3:** заголовок ADR 0002 «`## Executing` — не источник» при беглом чтении можно понять
  как исключение секции. Decision 1/2/4 это разводят, но заголовок стоит уточнить до
  «таблица Executing — не источник».
- **P3:** русские шаблоны в англоязычном промпте (§3 шаг 2.8/2.9); файл 188 строк, ниже
  конвенции 200–400. Регрессом не является.
