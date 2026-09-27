# Guard: детерминированный enforcement-слой zprof на хуках Claude Code

**Дата:** 2026-09-27
**Статус:** утверждён (брейншторм с Alex, четыре решения + дизайн)
**Фаза:** 1 — стоп-лист, merge-гейт, валидатор ответа субагента
**Связанные:** экспертная панель `docs/reviews/2026-09-27-ai4sdlc-panel.md` (строки 6, 8 плана), issues #18, #20, #15; инвентарь правил `thoughts/2026-09-27-prompt-rules-inventory.md`; scorecard `docs/superpowers/specs/2026-09-26-task-scorecard-design.md`

---

## 1. Цель и рамки

Политика zprof — стоп-лист, «pr-shepherd мержит сам», read-only роли, формат
ответа субагента — сегодня исполняется только текстом промпта. Панель
(2026-09-27, «Где эксперты сходятся», п. 1): «у политики нет enforcement-слоя
вне LLM». Цена: каждое противоречие в тексте модель разрешает по-своему; одно и
то же правило владелец повторяет в каждом проекте.

Guard делает нарушение **невозможным**, а не штрафуемым постфактум: хук
Claude Code проверяет вызов инструмента до исполнения и ответ субагента до
возврата, детерминированным stdlib-скриптом, из конфигурации, которой
управляет `zprof apply`.

**Успех фазы 1.** После `zprof sync` в любом проекте: (а) ни одна команда
стоп-листа не исполняется ни из main, ни из субагента; (б) `gh pr merge`
исполняется только из роли `pr-shepherd` и только при PR с `Closes #N` и
`## Gate`; (в) субагент с контрактом не возвращает ответ без `verdict:` первой
строкой; (г) ноль ложных блокировок штатного пути loop'а (implementer → tester
→ reviewer → pr-shepherd) на shakedown-прогоне.

**Вне фазы 1** (решения записаны, см. §12): граница «мутация — раннеру» для
main, гейт на `Agent` со счётчиками диспатчей, доставка контекста хуками,
frontmatter-хуки агентов, проверка run-log у task-runner, обязательные ключи
ответа по реестру #20, парсинг `rm -rf` вне репо.

## 2. Решения брейншторма

| # | Вопрос | Решение | Следствие для дизайна |
|---|---|---|---|
| 1 | Скрипт guard упал (баг, нет python3, битый конфиг) | **Fail-open**: exit 0, вызов проходит; факт ошибки → `.agentlog/guard-events.jsonl`, `zprof doctor`, score | Deny только при точном совпадении с правилом; `try/except` вокруг всего `main()` |
| 2 | Что main правит напрямую (граница) | State + docs + thoughts: `followup.md`, `todo.md`, `lessons.md`, `plan-*.md`, `thoughts/**`, `docs/**` | **Фаза 2.** Записано в §12, в фазе 1 не реализуется |
| 3 | Стоп-лист в интерактивной main-сессии | **Deny везде**, одно поведение для main и субагентов; нужен force-push — руками в терминале | Никакого `ask`, никакого режима «interactive» |
| 4 | Объём фазы 1 | Стоп-лист + merge-гейт + валидатор ответа | Разделы §5–§7; остальное §12 |
| 5 | Подход | **A + C**: глобальный guard с разрешением роли из `transcript_path` + `permissions.deny` как страховка | §4. Frontmatter-хуки (подход B) отложены |

Ранее принятые решения владельца, которые guard обязан уважать: auto-merge
везде и всегда, параметра `merge_policy` нет; `--delete-branch` у pr-shepherd
штатен, стоп-лист после #15 читается как «удаление **несмерженных** веток и
тегов»; защита контрактов — только required CI.

## 3. Что есть сегодня (факты)

- Хуки zprof: `SubagentStop`, `Stop`, `SessionStart` → `.claude/zprof-collect.py`;
  `Stop` дополнительно `zprof score --latest --quiet --no-collect`
  (`cli/internal/apply/settings.go:31-35`). `PreToolUse` нет, `permissions.deny`
  нет ни в одном проекте.
- Хуки одного события Claude Code запускает **параллельно**; upsert в
  `settings.local.json` ищет запись по подстроке `zprof-collect.py`
  (`settings.go:100-113`); поля `matcher` в записях нет.
- Коллектор читает паттерны из `telemetry.yaml` рядом с собой, в деплое — из
  `<cwd>/.agentlog/schema.json`, который `zprof apply` рендерит через `yamlToJSON`
  (`zprof-collect.py:1248-1262`, `apply/collector.go:27-47`).
- `telemetry.yaml` уже содержит `mutating_bash_patterns` (8 regex) и
  `verdict_exempt_roles: [auditor, auditor-deep]` — те возвращают `completion:`.
- Класс-A проверки коллектора (`_class_a_checks`, `zprof-collect.py:1365`):
  `has_preamble`, `return_parsed`, `verdict_value` — первая строка с `verdict:`;
  score P7 суммирует их по dispatch'ам (`score/metrics.go:228-252`).
- Транскрипт субагента лежит в
  `~/.claude/projects/<slug>/<session>/subagents/agent-<id>.jsonl`; рядом
  `agent-<id>.meta.json` с ключами `agentType`, `model`, `spawnDepth`,
  `description`, `toolUseId` (проверено на 88 субагентах этой сессии).
  Дизайн task-runner 2026-07-28 §11 отверг PreToolUse-гейт, потому что payload
  не различает main и субагента — с `meta.json` это ограничение снято.
- Документация Claude Code: `PreToolUse` получает `session_id`, `transcript_path`,
  `cwd`, `tool_name`, `tool_input`, `permission_mode`; решение —
  `hookSpecificOutput.permissionDecision: allow|deny|ask` + `permissionDecisionReason`
  (причина попадает модели); `updatedInput` поддерживается. `SubagentStop`
  получает `agent_type`, `transcript_path`, `stop_hook_active`; ответ
  `{"decision":"block","reason":…}` возвращает агента доделывать. Инструмент
  диспатча называется `Agent`. `hooks:` во frontmatter агента поддерживаются.
- У Alex глобальный `PreToolUse` на `Bash` переписывает команды в `rtk …`
  (`~/.claude/settings.json`); guard видит исходный `tool_input` параллельно с ним.
- Стоп-лист в `profiles/base/manifest.yaml:11-18` и managed-блоке `CLAUDE.md`.
- Overlay `issue-loop-github-strict/agents/pr-shepherd.md` несёт старый контракт
  «never merges» — противоречие с решением владельца, закрывается в #15; guard
  на текст промпта не смотрит.

## 4. Архитектура

```
profiles/base/zprof-guard.py ──apply──▶ .claude/zprof-guard.py        (0755)
profiles/base/guard.yaml     ─┐
profiles/overlays/*/guard.yaml┼─merge─▶ .claude/guard.json             (0644)
.zprof.yaml: guard:          ─┘         + mutating_bash_patterns из telemetry.yaml
                                        + permissions.deny → settings.local.json

Claude Code ─PreToolUse(Bash|Edit|Write|MultiEdit|NotebookEdit)─▶ guard pre-tool ─▶ allow | deny+reason
            ─SubagentStop───────────────────────────────────────▶ guard subagent-stop ─▶ pass | block+reason
                                                                        │
                                                                        ▼
                                                     .agentlog/guard-events.jsonl ─▶ collector (run_id) ─▶ zprof score P7
```

**Компоненты и границы.**

| Компонент | Отвечает за | Не отвечает за |
|---|---|---|
| `zprof-guard.py` | разбор payload, разрешение роли, применение правил, вывод решения, запись события | чтение YAML (только JSON), сеть кроме `gh pr view` в merge-гейте |
| `guard.yaml` (base, overlay, проект) | каталог правил: regex, контекст, роли, тексты причин | что-либо про score |
| `zprof apply` | деплой скрипта, рендер `guard.json` с merge трёх слоёв, upsert хуков с `matcher`, upsert `permissions.deny`, снятие всего при `guard.enabled: false` | валидацию правил на лету |
| `zprof doctor` | наличие хуков и конфига, парсинг, python3, проверка разрешения роли | исправление |
| коллектор | присвоение `run_id` guard-событиям на Stop (как tool-events, C4) | принятие решений |
| `zprof score` | учёт deny/block в P7 | новые метрики |

**Разрешение роли** (общая функция для обоих событий):

1. `agent_type` из payload, если есть (SubagentStop).
2. Иначе `transcript_path`: если путь содержит сегмент `subagents/` и имя
   `agent-<id>.jsonl` — читать `agent-<id>.meta.json` рядом, взять `agentType`.
3. Иначе, если `transcript_path` — файл верхнего уровня `<session>.jsonl`, роль
   `main`.
4. Нет файла / нет ключа / ошибка чтения → роль `unknown`. К `unknown`
   применяются правила без роли (стоп-лист, запись вне репо, `pr create`) и
   единственное role-правило `merge_role` (§5.5: неизвестный вызывающий merge
   не получает); `readonly_mutation` пропускается. Событие `role_unresolved`
   пишется в журнал один раз на сессию (дедуп по `session_id` в
   `.agentlog/guard-state.json`).

**Порядок проверки в `pre-tool`** фиксирован: стоп-лист без контекста →
стоп-лист с контекстом → запись вне репо → read-only роли → merge-гейт →
PR-гейт. Первое совпавшее правило даёт deny; дальше не идём. Ни одно правило
не даёт `allow` явно — отсутствие совпадений = молчание (exit 0 без вывода).

**Нормализация команды Bash** перед матчингом: `tool_input.command` →
убрать ведущие `rtk proxy ` / `rtk ` → схлопнуть пробелы → для контекстных
правил определить рабочий каталог: если команда начинается с `cd <path> &&`
или `cd <path>;`, каталог = `<path>` (относительно `cwd`), иначе `cwd` из
payload. Составные команды (`a && b`, `a; b`, `a | b`) матчатся целиком —
regex ищет по всей строке, поэтому `git status && git push --force` ловится.

## 5. Правила `pre-tool`

Каждое правило в `guard.yaml` имеет `id`, `tools` (список инструментов),
`match` (regex или список regex, Python `re`, флаг `IGNORECASE` не ставится),
опционально `context` (имя встроенной проверки), `roles` / `not_roles` /
`exempt_roles`, `reason` (текст для модели, русский, ≤200 символов, всегда
заканчивается инструкцией «верни `verdict: blocked`, reason: <id>»).

### 5.1 Стоп-лист без контекста (все роли)

| id | tools | match (Python regex) | Источник |
|---|---|---|---|
| `force_push` | Bash | `\bgit\s+push\b.*(\s-f\b\|\s--force\b\|\s--force-with-lease\b\|\s\+\S+)` | manifest.yaml:12 |
| `admin_merge` | Bash | `\bgh\s+pr\s+merge\b.*\s--admin\b` | pr-shepherd.md:32 |
| `no_verify_commit` | Bash | `\bgit\s+commit\b.*\s(-n\|--no-verify)\b` | pr-shepherd.md:32 |
| `no_verify_other` | Bash | `\bgit\s+(push\|merge\|rebase\|cherry-pick)\b.*\s--no-verify\b` | pr-shepherd.md:32 |
| `branch_force_delete` | Bash | `\bgit\s+branch\b.*\s(-D\|-[a-zA-Z]*D[a-zA-Z]*\|--delete\s+--force\|--force\s+--delete)\b` | manifest.yaml:13 (после #15: несмерженных) |
| `remote_ref_delete` | Bash | `\bgit\s+push\b.*(\s--delete\b\|\s-d\b\|\s:refs/\|\s\S+\s+:\S+)` | manifest.yaml:13 |
| `tag_delete` | Bash | `\bgit\s+tag\b.*\s(-d\|--delete)\b` | manifest.yaml:13 |
| `publish` | Bash | `\b(npm\|pnpm\|yarn)\s+publish\b`, `\bcargo\s+publish\b`, `\bgh\s+release\s+(create\|upload\|edit\|delete)\b`, `\bgoreleaser\s+release\b`, `\btwine\s+upload\b`, `\bpoetry\s+publish\b`, `\bxcrun\s+altool\b.*--upload-app`, `\bfastlane\b.*\b(pilot\|deliver\|upload_to_testflight\|upload_to_app_store)\b` | manifest.yaml:15 |
| `curl_pipe_sh` | Bash | `\b(curl\|wget)\b[^\|]*\|\s*(sudo\s+)?(ba\|z\|da)?sh\b` | manifest.yaml:17 (внешний код) |

`git branch -d` (только смерженные) и `gh pr merge --delete-branch` **не
запрещены** — решение владельца 2026-09-27. `--force-with-lease` запрещён:
стоп-лист говорит «force-push» без оговорок.

Overlay `ios-swift` добавляет `exempt_roles: {publish: [testflight-shipper]}` —
это единственная роль, которой публикация поручена явно. Overlay
`backend-python` добавляет правило `pip_install`:
`\b(pip\|pip3)\s+install\b`, `\bpoetry\s+add\b` с причиной «только `uv add`»
(implementer.md:50).

### 5.2 Стоп-лист с контекстом

| id | tools | match | context | deny когда |
|---|---|---|---|---|
| `rebase_published` | Bash | `\bgit\s+rebase\b(?!.*\s--abort\b)` | `head_on_remote` | `git rev-parse --abbrev-ref @{u}` успешен **и** `git branch -r --contains HEAD` непуст |
| `amend_published` | Bash | `\bgit\s+commit\b.*\s--amend\b` | `head_on_remote` | то же |
| `stash_in_worktree` | Bash | `\bgit\s+stash\b(?!\s+(list\|show))` | `linked_worktree` | `realpath(git rev-parse --git-dir) ≠ realpath(git rev-parse --git-common-dir)` |

Контекстные команды `git` выполняются с `timeout 3` в рабочем каталоге из §4;
любая ошибка (не git-репо, таймаут) → правило не срабатывает, событие
`context_error`. `head_on_remote` намеренно узкий: amend/rebase поверх
непушенных коммитов разрешён; остаток (rebase, задевающий пушенные предки)
упирается в `force_push` при попытке отправить.

### 5.3 Запись вне репозитория

| id | tools | условие |
|---|---|---|
| `write_outside_repo` | Edit, Write, MultiEdit, NotebookEdit | `realpath(file_path \| notebook_path)` не начинается ни с одного префикса из `allow_write_prefixes` |

`allow_write_prefixes` по умолчанию: `$CLAUDE_PROJECT_DIR` (realpath, покрывает
`.worktrees/`), `~/.claude/projects/*/memory/`, `~/.claude/plans/`,
`/private/tmp/claude-*`, `/tmp/claude-*`, `$TMPDIR/claude-*`. Дополнительно
разрешён любой путь внутри **linked worktree этого репозитория**: если
`git -C <dir(file_path)> rev-parse --git-common-dir` (timeout 3) резолвится в
`$CLAUDE_PROJECT_DIR/.git` — allow. Это покрывает worktree, созданные нативным
инструментом Claude Code вне каталога проекта. Проект расширяет списком
`guard.allow_write_outside` в `.zprof.yaml`. Bash-запись вне репо
(`cat > /etc/…`, `rm -rf ~/…`) в фазе 1 не разбирается — §12.

### 5.4 Read-only роли

| id | tools | roles | match |
|---|---|---|---|
| `readonly_mutation` | Bash | `readonly_roles` | любой из `mutating_bash_patterns` (копия из `telemetry.yaml`, рендерится в `guard.json` при apply) |

`readonly_roles` по умолчанию: `auditor`, `auditor-deep`, `explorer`,
`architect`, `reviewer`, `bug-hunter`, `expert-panel`, `evaluator`,
`evaluator-telemetry`, `evidence-auditor`, `north-star-auditor`,
`plan-reviewer`, `pr-shepherd`. У pr-shepherd `tools: Read, Grep, Glob, Bash`
и контракт «never writes code»; его штатные `gh …`, `git fetch`, `git log`,
`git rev-parse` в `mutating_bash_patterns` не входят. Overlay-роли с теми же
именами (reviewer из `backend-python`) наследуют правило по имени.

### 5.5 Merge-гейт

| id | tools | match | условие deny |
|---|---|---|---|
| `merge_role` | Bash | `\bgh\s+pr\s+merge\b`, `\bgh\s+api\b.*\/pulls\/\d+\/merge\b` | роль ∉ `merge_roles` (default `[pr-shepherd]`) |
| `merge_preflight` | Bash | те же, роль ∈ `merge_roles` | PR body не содержит `(?i)\bcloses\s+#\d+` **и** `closingIssuesReferences` пуст; или body не содержит `(?m)^##\s+Gate\b` |

Номер PR: первый аргумент `\d+` после `merge`, или `\d+` в конце URL, или
`/pulls/(\d+)/merge`; не найден (например `gh pr merge` без аргумента в
ветке PR) → `gh pr view --json number` в рабочем каталоге. Данные:
`gh pr view <N> --json body,closingIssuesReferences,state` с `timeout 10`.
Ошибка `gh` (сеть, auth) → **allow** + событие `preflight_unverified` —
fail-open по решению 1. Причина deny перечисляет недостающее:
«PR #N без `Closes #`» / «PR #N без раздела `## Gate`».

Роль `main` в `merge_roles` не входит: main-сессия не мержит (`CLAUDE.md`,
«Интеграция ветки»). Роль `unknown` — тоже deny: неизвестный вызывающий не
получает право merge (единственное role-правило, которое к `unknown`
применяется, потому что default безопаснее).

### 5.6 PR-гейт

| id | tools | match | условие deny |
|---|---|---|---|
| `pr_create_gate` | Bash | `\bgh\s+pr\s+create\b` | нет тела (ни `--body/-b`, ни `--body-file/-F`), или `--fill*`, или тело не содержит `(?i)\bcloses\s+#\d+`, или не содержит `(?m)^##\s+Gate\b` |

Разбор через `shlex.split(command)`; `--body-file` читается относительно
рабочего каталога; `-` (stdin) → allow + `preflight_unverified`. Ошибка
`shlex` (незакрытая кавычка, heredoc) → allow + `parse_error`. Правило без
ролей: любой, кто создаёт PR в zprof-проекте, даёт `Closes #N` и `## Gate` —
это pre-flight pr-shepherd (`pr-shepherd.md:45-49`), перенесённый на момент
создания.

### 5.7 Формат ответа deny

```json
{"hookSpecificOutput": {"hookEventName": "PreToolUse",
  "permissionDecision": "deny",
  "permissionDecisionReason": "zprof guard [force_push]: force-push запрещён стоп-листом. Не обходи: верни `verdict: blocked`, reason: force_push."}}
```

Exit 0. Никаких `updatedInput` в фазе 1. Никаких `ask`.

## 6. Валидатор `subagent-stop`

**Вход:** payload `agent_type`, `transcript_path`, `stop_hook_active`, `cwd`.

**Контракт роли:** `<cwd>/.claude/agents/<agent_type>.md`, затем
`<cwd>/.claude/agents/gates/<agent_type>.md`. Нет файла → pass. Frontmatter
между первыми двумя `---`; блок `return_format: |` до следующего ключа
верхнего уровня. В блоке пропускаются строки, начинающиеся с `#` (комментарии
`# CRITICAL: …`). Первая содержательная строка: `^\s*(verdict|completion):\s*(.+?)\s*$`
→ ключ и список значений по `|`; значение вида `<…>` означает «любое» (список
не проверяется). Нет такой строки → pass.

**Финальный текст:** `transcript_path` JSONL, последняя запись
`type == "assistant"`, конкатенация блоков `message.content[*].text`. Нет
записи → pass (агент прерван, коллектор это учтёт сам).

**Проверки (фаза 1, только две):**

1. Первая непустая строка текста начинается с `<ключ>:` (без учёта регистра,
   как `_class_a_checks`).
2. Если список значений задан — первое слово после двоеточия, в нижнем
   регистре, входит в список.

Провал и `stop_hook_active == false` → block:

```json
{"decision": "block",
 "reason": "zprof guard [return_format]: ответ pr-shepherd должен начинаться строкой `verdict: <merged-stamped|verified-stamped|…>`. Сейчас первая строка: «Готово, PR смержен…». Перепиши ответ по return_format без преамбулы."}
```

Провал и `stop_hook_active == true` → pass + событие `format_unfixed` (второй
раз не отправляем, чтобы не зациклить). Успех → молчание.

**Взаимодействие с коллектором.** На одно и то же `SubagentStop` коллектор
пишет pointer по `agent_id` (`add_pointer`, перезапись) — повторное событие
после block безвредно. Валидатор не читает `.agentlog/` и не пишет туда ничего,
кроме `guard-events.jsonl`. Роли из `verdict_exempt_roles` проверяются по
своему ключу `completion:` — им валидатор помогает, а не мешает; после #20
ключ станет `verdict:` и парсер frontmatter подхватит это без изменений кода.

## 7. Телеметрия и score

`.agentlog/guard-events.jsonl`, одна строка на событие, `fcntl.flock` +
`os.fsync`, как у коллектора:

```json
{"ts":"2026-09-27T14:02:11Z","session_id":"…","event":"pre-tool","role":"implementer",
 "tool":"Bash","rule":"force_push","decision":"deny","target":"git push","input_hash":"a1b2c3d4e5f6","run_id":null}
```

`decision` ∈ `deny | block | allow_unverified | error`. `target` — первые два
токена команды или basename файла; полная команда не сохраняется (может
содержать секреты). `input_hash` — sha1 канонического `tool_input`[:12], как в
`tool-events.jsonl`. Ошибки скрипта (`decision: error`, поле `detail` — класс
исключения) пишутся из внешнего `try/except`; если и запись упала — тишина.

**Коллектор.** На `stop` присваивает `run_id` строкам guard-events тем же
оконным правилом, что tool-events (C4, `_assign_run_ids`), и переписывает файл
атомарно. **Score.** Reader загружает `guard-events.jsonl`; `computeP7`
прибавляет по одному нарушению за каждую строку `deny`/`block` в окне run, по
роли; `verdict_exempt_roles` от этого слагаемого не освобождены (deny на
мутацию auditor'а — нарушение). Веса и saturation P7 не меняются (10, sat 4);
`weights_hash` не меняется, потому что веса те же. Карточка в `## Score`
показывает `P7 … (guard: N deny)` когда N > 0. `zprof stats` получает раздел
`guard: top rules` — счётчик по `rule` за период.

## 8. Конфигурация

### 8.1 `profiles/base/guard.yaml` (источник правды)

```yaml
version: 1
readonly_roles: [auditor, auditor-deep, explorer, architect, reviewer, bug-hunter,
                 expert-panel, evaluator, evaluator-telemetry, evidence-auditor,
                 north-star-auditor, plan-reviewer, pr-shepherd]
merge_roles: [pr-shepherd]
allow_write_prefixes: ["$CLAUDE_PROJECT_DIR", "~/.claude/projects/*/memory/", "~/.claude/plans/",
                       "/private/tmp/claude-*", "/tmp/claude-*", "$TMPDIR/claude-*"]
permissions_deny:                      # страховка на случай отсутствия скрипта
  - "Bash(git push --force*)"
  - "Bash(git push -f*)"
  - "Bash(gh pr merge --admin*)"
  - "Bash(git commit --no-verify*)"
  - "Bash(git branch -D*)"
  - "Bash(git tag -d*)"
rules:
  - id: force_push
    tools: [Bash]
    match: ['\bgit\s+push\b.*(\s-f\b|\s--force\b|\s--force-with-lease\b|\s\+\S+)']
    reason: "force-push запрещён стоп-листом"
  - id: rebase_published
    tools: [Bash]
    match: ['\bgit\s+rebase\b(?!.*\s--abort\b)']
    context: head_on_remote
    reason: "rebase опубликованной ветки запрещён стоп-листом"
  - id: readonly_mutation
    tools: [Bash]
    roles: $readonly_roles
    match: $mutating_bash_patterns          # подставляется при apply из telemetry.yaml
    reason: "роль read-only: мутирующая команда запрещена контрактом"
  - id: merge_role
    tools: [Bash]
    match: ['\bgh\s+pr\s+merge\b', '\bgh\s+api\b.*/pulls/\d+/merge\b']
    not_roles: $merge_roles
    reason: "merge выполняет только pr-shepherd (CLAUDE.md «Интеграция ветки»)"
  # … остальные правила §5 в том же формате
```

Ссылки `$name` разрешаются при рендере `guard.json`; в JSON остаются
только литералы. Каждая причина автоматически дополняется хвостом
«Не обходи: верни `verdict: blocked`, reason: <id>.»

### 8.2 Слои и merge

`base/guard.yaml` → каждый активный overlay `guard.yaml` (если есть) →
`.zprof.yaml: guard:`. Правила: списки (`rules`, `readonly_roles`,
`merge_roles`, `allow_write_prefixes`, `permissions_deny`) — конкатенация с
дедупликацией по значению (для `rules` — по `id`, поздний слой заменяет правило
целиком); `exempt_roles` — map `rule_id → [roles]`, объединение списков;
скаляры — поздний слой перекрывает.

### 8.3 `.zprof.yaml`

```yaml
guard:
  enabled: true                 # false → apply снимает хуки guard и permissions_deny
  extra_deny_bash: []           # доп. regex, правило extra_deny с общей причиной
  merge_roles: [pr-shepherd]    # переопределение
  readonly_roles: []            # дополнение
  allow_write_outside: []       # дополнительные префиксы для write_outside_repo
  exempt_roles: {}              # rule_id → [roles]
```

Go: `manifest.GuardConfig{Enabled *bool; ExtraDenyBash, MergeRoles,
ReadonlyRoles, AllowWriteOutside []string; ExemptRoles map[string][]string}`
рядом с `ScoreConfig` (`manifest/project.go:44`).

### 8.4 Хуки в `settings.local.json`

`hookSpec` получает поле `matcher`; запись с непустым `matcher` рендерится как
`{"matcher": "...", "hooks": [...]}`. Upsert по имени скрипта: `zprofHookIndex`
принимает подстроку (`zprof-collect.py` | `zprof-guard.py`), поэтому две
zprof-записи на одном событии (`SubagentStop`) живут рядом и апгрейдятся
независимо.

| Событие | matcher | команда |
|---|---|---|
| `PreToolUse` | `Bash\|Edit\|Write\|MultiEdit\|NotebookEdit` | `test -x "$CLAUDE_PROJECT_DIR/.claude/zprof-guard.py" && "$CLAUDE_PROJECT_DIR/.claude/zprof-guard.py" pre-tool \|\| true` |
| `SubagentStop` | — | `test -x "$CLAUDE_PROJECT_DIR/.claude/zprof-guard.py" && "$CLAUDE_PROJECT_DIR/.claude/zprof-guard.py" subagent-stop \|\| true` |

`|| true` сохраняет fail-open: ненулевой выход без JSON на stdout = allow.
`permissions.deny` дополняется значениями `permissions_deny` из `guard.json`
(без дублей, чужие записи не трогаются). `guard.enabled: false` → обе записи
guard и все `permissions_deny` из `guard.json` удаляются, чужие остаются.

## 9. Изменения по компонентам

| Файл | Изменение |
|---|---|
| `profiles/base/zprof-guard.py` | новый; stdlib; `main()` → `pre_tool()` / `subagent_stop()`; `resolve_role`, `normalize_command`, `working_dir`, контексты `head_on_remote`, `linked_worktree`; `load_config` (`.claude/guard.json`); `write_event`; внешний `try/except` → exit 0 |
| `profiles/base/guard.yaml` | новый; §8.1 полностью |
| `profiles/overlays/ios-swift/guard.yaml` | `exempt_roles: {publish: [testflight-shipper]}` |
| `profiles/overlays/backend-python/guard.yaml` | правило `pip_install` |
| `cli/internal/overlay/loader.go` | `Base.GuardScript`, `Base.GuardSchema`, `Overlay.GuardSchema []byte` |
| `cli/internal/apply/guard.go` | новый; `deployGuard`: скрипт, merge слоёв, подстановка `$mutating_bash_patterns` из `Base.TelemetrySchema`, рендер `guard.json`; вызов из `engine.go` после `deployCollector` |
| `cli/internal/apply/settings.go` | `hookSpec.matcher`; `zprofHookIndex(entries, script)`; спеки guard; `ensurePermissionsDeny`; снятие при `enabled=false` |
| `cli/internal/manifest/project.go` | `GuardConfig` |
| `cli/internal/doctor/diagnostics.go` | `checkGuardHooks`, `checkGuardConfig`, `checkPermissionsDeny`, `checkRoleResolution` |
| `profiles/base/zprof-collect.py` | `_assign_run_ids` также для `guard-events.jsonl` |
| `cli/internal/score/{reader,metrics,render}.go` | загрузка guard-events, слагаемое в P7, строка `guard: N deny` |
| `cli/internal/cmd/stats.go` | раздел `guard: top rules` |
| `profiles/base/manifest.yaml`, managed-блок `CLAUDE.md` | одна строка в доктрину: «Guard: стоп-лист, merge и формат ответа проверяет хук; на deny не ищи обход — верни `verdict: blocked` с reason» |
| `profiles/base/agents/pr-shepherd.md`, `task-runner.md` | одна строка: «deny от zprof guard = стоп-лист, дальше `blocked`» |

`zprof sync` деплоит guard так же, как коллектор (тот же путь `apply`).

## 10. Doctor

| Проверка | Уровень | Сообщение |
|---|---|---|
| `.claude/zprof-guard.py` есть, а в `settings.local.json` нет `PreToolUse`/`SubagentStop` с `zprof-guard.py` | warn | «guard hooks missing for …; run `zprof apply`» |
| `.claude/guard.json` отсутствует или не парсится, или `rules` пуст | warn | «guard.json …» |
| `permissions.deny` не содержит хотя бы одного из `permissions_deny` | warn | перечень недостающих |
| `guard.enabled: false` в `.zprof.yaml` | info | «guard disabled by project config» |
| Разрешение роли: в `~/.claude/projects/<slug>/` (slug = путь проекта с `/`→`-`) есть хотя бы один `*/subagents/*.meta.json` с ключом `agentType` | info при отсутствии | «role resolution unverified: no subagent meta found yet» |
| `python3 -c pass` (уже есть) | без изменений | |

## 11. Тестирование

- **pytest `profiles/base/tests/test_guard.py`**, табличные: для каждого
  правила §5 — минимум один deny и два allow (похожая, но легитимная команда:
  `git push`, `git branch -d`, `gh pr merge` из pr-shepherd с валидным PR через
  подменённый `gh`); `rtk`-префикс; `cd path &&`; роль из `meta.json`; роль
  `unknown` → merge deny, readonly пропуск; fail-open: битый `guard.json`,
  отсутствующий `meta.json`, исключение внутри правила → exit 0, пустой stdout,
  строка `error` в журнале. Для `subagent-stop`: контракт с `verdict:` и с
  `completion:`, свободное значение `<…>`, преамбула, неверное значение,
  `stop_hook_active`, агент без контракта, транскрипт без assistant-записи.
  Фикстуры: временный `HOME` с `projects/<slug>/<session>/subagents/`.
- **Go**: `apply` — рендер `guard.json` с merge трёх слоёв и подстановкой
  паттернов; хуки с `matcher`, upsert и апгрейд стале-записи рядом с
  коллекторной; `permissions.deny` без дублей; снятие при `enabled=false`.
  `doctor` — каждая проверка §10. `score` — P7 с guard-events, golden
  `testdata/run1` расширяется двумя deny (ожидаемый P7 5 → 10 при sat 4:
  было 2 нарушения, стало 4 → saturation, вес 10 полностью).
  `manifest` — парсинг `guard:`.
- **E2E** (`cli/internal/apply` integration test): `zprof apply` во временный
  проект → `settings.local.json` содержит обе guard-записи и deny → запуск
  `.claude/zprof-guard.py pre-tool` с payload force-push → JSON deny; с payload
  `git status` → пустой stdout.
- **Shakedown**: `zprof shakedown` на базовом профиле после внедрения — ноль
  deny на штатном маршруте (критерий успеха §1 г).

## 12. Фаза 2 — зафиксированные решения, не реализуются сейчас

1. **Граница main** (решение 2): PreToolUse Edit/Write/mutating Bash при роли
   `main` → deny, если путь не в allowlist `followup.md`, `todo.md`,
   `lessons.md`, `lessons-archive.md`, `plan-*.md`, `thoughts/**`, `docs/**`,
   `.zprof/**`. Причина: «мутация — раннеру: dispatch task-runner».
2. **Гейт на `Agent`**: main диспатчит только роли из `main_dispatch_roles`;
   `auditor*` только из `task-runner`; `task-runner` не диспатчит `task-runner`
   (`task-runner.md:157-161`); `max_dispatches` (#19) и «≤3 круга
   tester→implementer» (`task-runner.md:150-152`) как счётчики в
   `.zprof/runs/<id>.state.json`; бриф task-runner без `#\d+` → deny.
3. **Обязательные ключи ответа** по реестру `verdicts.yaml` (#20) вместо
   парсинга frontmatter; проверка `artifact` существует, `run_log` в
   `.zprof/runs/`.
4. **Stop-валидатор main**: `followup.md` ≤20 строк, прирост ≤3 на dispatch,
   блок `verdict:` в ответе main.
5. **Доставка контекста**: SessionStart/PostCompact — хвост followup + lessons;
   UserPromptSubmit на «следующая задача» — первый незакрытый пункт todo.
6. **Frontmatter-хуки** для tester (blacklist API, >500 строк) — подход B.
7. **Bash-запись вне репо** (`>` в путь вне репо, `rm -rf` вне репо).

## 13. Риски и границы

- **`meta.json` не документирован.** Смена раскладки → роль `unknown` →
  role-правила молчат (fail-open), merge-гейт при этом закрыт для всех кроме…
  никого — pr-shepherd тоже станет `unknown` и получит deny на merge. Это
  единственное место, где fail-open ломает штатный путь. Смягчение: doctor
  `checkRoleResolution`; событие `role_unresolved` в журнале; в причине deny для
  `merge_role` при роли `unknown` явно сказано «роль не разрешена, проверь
  `zprof doctor`». Владелец принимает этот риск осознанно: закрытый merge
  безопаснее открытого.
- **Параллельные хуки** на `SubagentStop`: guard и коллектор независимы;
  порядок не важен.
- **Ложные срабатывания regex**: `git push origin +main` — намеренно deny;
  `git commit -n` — deny (это `--no-verify`); `git push -n` (dry-run) — allow,
  паттерн `no_verify_other` без `-n`. Regex ищет по всей строке без разбора
  кавычек: `git commit -m "drop -n flag"` даст ложный deny. Принято: модель
  переформулирует сообщение; правило простое и предсказуемое. Тесты §11
  фиксируют каждую пару.
- **`gh pr view` в merge-гейте** — единственный сетевой вызов, ~1 с, только
  при merge, fail-open при ошибке.
- **Латентность**: python3 старт + чтение `guard.json` ≈ 40–60 мс на каждый
  Bash/Edit/Write. Read/Grep/Glob/Agent не матчатся.
- **Глобальные хуки пользователя** (`rtk`, orca) — работают параллельно, guard
  читает исходный `tool_input`; префикс `rtk` снимается на всякий случай.
- **Секреты**: полная команда в журнал не пишется — `target` и хэш.

## 14. Источники

- Экспертная панель 2026-09-27: `docs/reviews/2026-09-27-ai4sdlc-panel.md`
  («Контроль держится на прозе», строки плана 6, 8), линзы 02 и 06.
- Инвентарь 96 правил: `thoughts/2026-09-27-prompt-rules-inventory.md`.
- Дизайн task-runner §11 (2026-07-28) — отвергнутый PreToolUse и открытый
  эксперимент, который здесь закрыт.
- Документация Claude Code: hooks guide (input fields, PreToolUse decision,
  SubagentStop `decision: block`, matcher patterns), sub-agents (frontmatter
  `hooks:`, `tools:`/`disallowedTools:`).
