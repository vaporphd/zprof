# ADR 0004: `zprof-guard.py pre-tool` — каркас, формат `guard.yaml`/`guard.json`, стоп-лист §5.1 и read-only роли

**Date:** 2026-09-28
**Status:** proposed
**Issue:** #23 (зависимые: #24 контексты, #25 merge/PR-гейт, #26 subagent-stop, #28 apply)
**Run:** `.zprof/runs/2026-09-28-issue-23-zprof-guard-py.md`, audit_step 2
**Spec:** `docs/superpowers/specs/2026-09-27-guard-hooks-design.md` §2, §4, §5, §7, §8.1, §11, §13

## Context

Политика zprof (стоп-лист, read-only роли, «мержит только pr-shepherd») исполняется
только текстом промпта. Спека guard вводит детерминированный хук `PreToolUse`.
Issue #23 — первый кирпич: скрипт-каркас, **полный** каталог правил и две группы
правил без внешнего контекста. От формы `guard.yaml`/`guard.json`, которую
фиксирует этот ADR, зависят #24, #25, #26 и #28 — после #23 форма не меняется.

Три вещи в источниках противоречат друг другу или ловушечны; ADR их разрешает:

1. **`remote_ref_delete` + `branch_pr_merged`.** Спека §5.1 даёт безусловное
   правило, §5.2 — вариант с контекстом «только для pr-shepherd»; комментарий
   владельца к #23 говорит «`remote_ref_delete` получает `context:
   branch_pr_merged` с `roles: [pr-shepherd]`… тест “pr-shepherd без контекста →
   deny”»; AC2 issue: «правило с незнакомым `context` в этом issue не
   срабатывает». Решение — D5.
2. **Футер issue #23** («Реализуй по спеке (pr-shepherd в `readonly_roles`)… тест
   deny на `git checkout main` у pr-shepherd») устарел: спека §5.4/§8.1 и
   последующий комментарий владельца исключили pr-shepherd. Решение — D6.
3. **`curl_pipe_sh` в таблице §5.1.** Механическое правило «`\|` в таблице → `|`
   в YAML» для этого regex **неверно**: в нём один `\|` — это литеральный пайп
   (экранирование regex), а не markdown. Наивная подстановка даёт
   `\b(curl|wget)\b[^|]*|\s*(sudo\s+)?(ba|z|da)?sh\b` — альтернация верхнего
   уровня, `sh\b` совпадает с `push`. Проверено на `re`: наивный вариант
   запрещает `git push`, `gh pr merge …`, `git push -n` — т.е. ломает каждый AC9-allow.
   Точная строка — в D3.

## Decision

### D1. Файлы и границы

| Файл | Кто создаёт | Что |
|---|---|---|
| `profiles/base/zprof-guard.py` | implementer (#23) | `#!/usr/bin/env python3`, 0755, Python 3.10+, stdlib only, режим `pre-tool`; режим `subagent-stop` — no-op (exit 0), логика в #26 |
| `profiles/base/guard.yaml` | implementer (#23) | каталог D3 целиком |
| `profiles/base/tests/test_guard.py` | implementer (#23) | D10 |

Не входит: деплой в `.claude/`, `settings.local.json`, `permissions.deny`,
рендер `guard.json`, merge слоёв (#28); evaluator'ы контекстов (#24, #25);
Go-код. Скрипт **никогда** не читает YAML — только `guard.json`.

### D2. Семантика правила (фиксируется для всех будущих issue)

Поля записи в `rules:`

| Ключ | Тип в `guard.json` | Обяз. | Смысл |
|---|---|---|---|
| `id` | str | да | уникален в списке; ключ дедупа слоёв (§8.2), имя в deny и в журнале |
| `tools` | list[str] | да | `tool_name` должен входить в список |
| `match` | list[str] (или одна str) | нет | regex Python `re`, без флагов, `re.search` по «субъекту»; совпал любой → условие выполнено. **Отсутствует/пуст → условие считается выполненным** (правило целиком решает `context`) |
| `context` | str | нет | имя встроенного evaluator'а (D4) |
| `roles` | list[str] | нет | правило применяется, только если роль ∈ списка |
| `not_roles` | list[str] | нет | правило применяется, только если роль ∉ списка |
| `reason` | str | да | русский, ≤200 символов, **без** хвоста «Не обходи…» и без финальной точки — хвост дописывает скрипт (D8) |

Верхний уровень: `version` (int, =1), `readonly_roles`, `merge_roles`,
`allow_write_prefixes`, `permissions_deny`, **`exempt_roles`** (map
`rule_id → [roles]`, в base пустой `{}` — единственная форма исключений;
поля `exempt_roles` на уровне правила нет), `rules`.

Субъект для `match`: `Bash` → `normalize_command(tool_input.command)`;
`Edit|Write|MultiEdit` → `tool_input.file_path`; `NotebookEdit` →
`tool_input.notebook_path`. Субъект не строка/пуст → правило с непустым
`match` не совпадает.

**Проверка одного правила** (строго в этом порядке, дешёвое раньше дорогого):
`tool_name ∈ tools` → `roles` → `not_roles` → `role ∉ exempt_roles.get(id, [])`
→ `match` → `context`. Правило «срабатывает», когда прошли все шаги.

**Роль `unknown` без спецслучаев**: `unknown ∉ readonly_roles` → `readonly_mutation`
не применяется; `unknown ∉ merge_roles` → `merge_role` применяется. Поведение
§4/§13 выводится из общей семантики, отдельного `if role == "unknown"` в движке нет.

**Порядок = порядок списка `rules:`** (AC6). Отдельного поля приоритета нет.
Первое сработавшее правило → deny, дальше не идём. Ни одно правило не даёт
`allow`; нет срабатываний → exit 0, пустой stdout. `ask` не используется никогда.

**Строгость типов** (защита от нерендеренных ссылок): если `match`, `roles`
или `not_roles` — строка, начинающаяся с `$` (ссылка, которую apply не
подставил), или не str/list[str] → `ValueError` → внешний `try/except` →
fail-open + событие `error`. Причина: строка `"$mutating_bash_patterns"`,
проитерированная посимвольно, дала бы regex `$`, совпадающий с любой командой.
Невалидный regex → `re.error` → то же. Отдельной «пропустить только это
правило» логики нет: одно правило сломано — guard молчит целиком и это видно
в журнале/doctor. Валидацию при рендере делает #28.

### D3. `guard.yaml` — точная структура (AC1)

Стиль файла (обязателен, чтобы тестовый дрейф-чек мог грепать без PyYAML):
отступ 2 пробела; каждое правило начинается строкой `  - id: <id>`; regex —
только в **одинарных кавычках**, каждый отдельной строкой блочного списка
(`      - '<regex>'`) — в одинарных кавычках YAML обратный слэш литерален,
а `|` не требует экранирования; списки ролей — flow-стиль `[a, b]`; ссылки
`$name` — голым скаляром. Для Go `yaml.v3` в #28 блочный и flow-стили
эквивалентны §8.1.

Ниже — содержание, которое implementer переносит в файл. Все regex здесь уже
в YAML-форме (markdown-экранирование снято; литеральный пайп в `curl_pipe_sh`
сохранён как `\|`). Колонка «в #23» — активно ли правило в скрипте #23.

```text
version: 1
readonly_roles: [auditor, auditor-deep, explorer, architect, reviewer, bug-hunter,
                 expert-panel, evaluator, evaluator-telemetry, evidence-auditor,
                 north-star-auditor, plan-reviewer]      # pr-shepherd намеренно нет, §5.4
merge_roles: [pr-shepherd]
allow_write_prefixes: ["$CLAUDE_PROJECT_DIR", "~/.claude/projects/*/memory/", "~/.claude/plans/",
                       "/private/tmp/claude-*", "/tmp/claude-*", "$TMPDIR/claude-*"]
permissions_deny:
  - "Bash(git push --force*)"
  - "Bash(git push -f*)"
  - "Bash(gh pr merge --admin*)"
  - "Bash(git commit --no-verify*)"
  - "Bash(git branch -D*)"
  - "Bash(git tag -d*)"
exempt_roles: {}
rules:
  # --- §5.1 стоп-лист без контекста (все роли) ---
  - id: force_push                 tools [Bash]
      - '\bgit\s+push\b.*(\s-f\b|\s--force\b|\s--force-with-lease\b|\s\+\S+)'
  - id: admin_merge                tools [Bash]
      - '\bgh\s+pr\s+merge\b.*\s--admin\b'
  - id: no_verify_commit           tools [Bash]
      - '\bgit\s+commit\b.*\s(-n|--no-verify)\b'
  - id: no_verify_other            tools [Bash]
      - '\bgit\s+(push|merge|rebase|cherry-pick)\b.*\s--no-verify\b'
  - id: branch_force_delete        tools [Bash]
      - '\bgit\s+branch\b.*\s(-D|-[a-zA-Z]*D[a-zA-Z]*|--delete\s+--force|--force\s+--delete)\b'
  - id: remote_ref_delete          tools [Bash]      # БЕЗ context, БЕЗ roles — D5
      - '\bgit\s+push\b.*(\s--delete\b|\s-d\b|\s:refs/|\s\S+\s+:\S+)'
  - id: tag_delete                 tools [Bash]
      - '\bgit\s+tag\b.*\s(-d|--delete)\b'
  - id: publish                    tools [Bash]
      - '\b(npm|pnpm|yarn)\s+publish\b'
      - '\bcargo\s+publish\b'
      - '\bgh\s+release\s+(create|upload|edit|delete)\b'
      - '\bgoreleaser\s+release\b'
      - '\btwine\s+upload\b'
      - '\bpoetry\s+publish\b'
      - '\bxcrun\s+altool\b.*--upload-app'
      - '\bfastlane\b.*\b(pilot|deliver|upload_to_testflight|upload_to_app_store)\b'
  - id: curl_pipe_sh               tools [Bash]
      - '\b(curl|wget)\b[^|]*\|\s*(sudo\s+)?(ba|z|da)?sh\b'
  # --- §5.2 стоп-лист с контекстом (данные; evaluator'ы — #24) ---
  - id: rebase_published           tools [Bash]   context: head_on_remote
      - '\bgit\s+rebase\b(?!.*\s--abort\b)'
  - id: amend_published            tools [Bash]   context: head_on_remote
      - '\bgit\s+commit\b.*\s--amend\b'
  - id: stash_in_worktree          tools [Bash]   context: linked_worktree
      - '\bgit\s+stash\b(?!\s+(list|show))'
  - id: remote_ref_delete_unmerged tools [Bash]   roles: [pr-shepherd]   context: branch_pr_merged
      - '\bgit\s+push\b.*(\s--delete\b|\s-d\b|\s:refs/|\s\S+\s+:\S+)'
  # --- §5.3 запись вне репо (данные; evaluator — #24) ---
  - id: write_outside_repo         tools [Edit, Write, MultiEdit, NotebookEdit]
                                   context: write_outside_repo      # match отсутствует
  # --- §5.4 read-only роли ---
  - id: readonly_mutation          tools [Bash]   roles: $readonly_roles
                                   match: $mutating_bash_patterns   # подставляет apply из telemetry.yaml
  # --- §5.5 merge-гейт ---
  - id: merge_role                 tools [Bash]   not_roles: $merge_roles
      - '\bgh\s+pr\s+merge\b'
      - '\bgh\s+api\b.*/pulls/\d+/merge\b'
  - id: merge_preflight            tools [Bash]   roles: $merge_roles   context: merge_preflight
      - '\bgh\s+pr\s+merge\b'
      - '\bgh\s+api\b.*/pulls/\d+/merge\b'
  # --- §5.6 PR-гейт ---
  - id: pr_create_gate             tools [Bash]   context: pr_create_gate
      - '\bgh\s+pr\s+create\b'
```

(Сокращённая запись `tools [..]`/`context:` в одной строке — только для
плотности ADR; в файле это обычные ключи `tools:`, `match:`, `context:`,
`roles:`, `not_roles:`, `reason:` каждый своей строкой.)

`reason` (без хвоста, без финальной точки):

| id | reason |
|---|---|
| force_push | force-push запрещён стоп-листом |
| admin_merge | merge в обход защиты ветки (--admin) запрещён стоп-листом |
| no_verify_commit | коммит без хуков (-n/--no-verify) запрещён стоп-листом |
| no_verify_other | push/merge/rebase/cherry-pick с --no-verify запрещён стоп-листом |
| branch_force_delete | удаление несмерженной ветки (-D/--force) запрещено стоп-листом; смерженную удаляй через git branch -d |
| remote_ref_delete | удаление ветки или тега на remote запрещено стоп-листом |
| tag_delete | удаление тега запрещено стоп-листом |
| publish | релиз и публикация пакета запрещены стоп-листом |
| curl_pipe_sh | исполнение скачанного кода (curl/wget в shell) запрещено стоп-листом |
| rebase_published | rebase опубликованной ветки запрещён стоп-листом |
| amend_published | amend опубликованного коммита запрещён стоп-листом |
| stash_in_worktree | git stash в linked worktree запрещён: stash общий для всех worktree |
| remote_ref_delete_unmerged | удалять на remote можно только ветку уже смерженного PR |
| write_outside_repo | запись вне репозитория запрещена стоп-листом |
| readonly_mutation | роль read-only: мутирующая команда запрещена контрактом |
| merge_role | merge выполняет только pr-shepherd (CLAUDE.md «Интеграция ветки») |
| merge_preflight | PR не прошёл pre-flight: нужны Closes #N и раздел ## Gate |
| pr_create_gate | PR создаётся только с телом, где есть Closes #N и раздел ## Gate |

**Активность в #23** (AC1):

| Группа | id | В #23 |
|---|---|---|
| §5.1 | 9 правил | **активно** (regex, без context) |
| §5.4 | `readonly_mutation` | **активно** (roles + regex, без context) |
| §5.5 | `merge_role` | **активно** — см. ниже |
| §5.2 | `rebase_published`, `amend_published`, `stash_in_worktree`, `remote_ref_delete_unmerged` | данные: context не зарегистрирован → не срабатывает (#24) |
| §5.3 | `write_outside_repo` | данные (#24) |
| §5.5 | `merge_preflight` | данные (#25) |
| §5.6 | `pr_create_gate` | данные (#25) |

`merge_role` — осознанное отклонение от брифа («активны только §5.1 +
`readonly_mutation`»): форма правила задана §8.1 без `context`, движок
универсален (D2), поэтому оно срабатывает само. Выключать его искусственным
флагом или фиктивным context значило бы менять форму, которую читает #28.
Поведение совпадает со спекой (main/implementer/unknown → deny, pr-shepherd →
пропуск до `merge_preflight`). #25 добавляет evaluator `merge_preflight` и
уточнение причины для `unknown` (§13); форма `merge_role` не меняется. На
прод это не влияет до #28 (guard не деплоится).

Зарезервированные имена контекстов (единый реестр, D4): `head_on_remote`,
`linked_worktree`, `branch_pr_merged` (#24), `write_outside_repo` (#24),
`merge_preflight`, `pr_create_gate` (#25). Имя контекста у нерегексных
правил совпадает с `id` правила (`write_outside_repo`, `merge_preflight`,
`pr_create_gate`) — одно имя на одну проверку.

### D4. Контексты: политика «незнакомый context не срабатывает» (AC3)

- В скрипте — модульный реестр `CONTEXTS: dict[str, Callable[[dict, dict, dict], bool | str]]`,
  в #23 **пустой**. Сигнатура evaluator'а: `(call, rule, config) -> bool | str`:
  `False`/`None` — не срабатывает; `True` — срабатывает с `rule.reason`;
  `str` — срабатывает с этой причиной (нужно #25: «PR #N без `Closes #`»).
  Возврат строки — часть рамки сейчас, чтобы #25 не менял движок.
- `rule.context` задан, а в `CONTEXTS` такого имени нет → правило **не
  срабатывает**, переход к следующему правилу. Никаких событий в журнал (в #23
  это штатное состояние для 7 правил — шум). Никаких исключений из правила:
  ни одно правило с незнакомым context не денаит «на всякий случай».
- Ошибки внутри evaluator'а (таймаут git и т.п.) обрабатывает сам evaluator
  (#24: `context_error` + False; `branch_pr_merged` — fail-closed, True).
  Движок не ловит их отдельно — непойманное уходит во внешний `try/except`.
- Опечатка в имени контекста в overlay молча выключит правило — закрывается
  проверкой имён в doctor (#28), не в #23.

### D5. `remote_ref_delete` — разрешение противоречия

Согласен с анализом task-runner по сути и расширяю его: **два правила с
разными `id`**.

1. `remote_ref_delete` (§5.1) — безусловное, без `context`, без ролей. В #23
   `pr-shepherd git push origin --delete feat/x` → deny `remote_ref_delete`.
   Это и есть тест владельца «pr-shepherd без контекста → deny».
2. `remote_ref_delete_unmerged` (§5.2) — `roles: [pr-shepherd]`,
   `context: branch_pr_merged`, тот же regex; в #23 не срабатывает (D4).
   Это «правило с полем context», которое просил комментарий владельца.
3. В #24: `remote_ref_delete` получает `not_roles: [pr-shepherd]`, в
   `CONTEXTS` регистрируется `branch_pr_merged` (True = PR не смержен или
   ошибка `gh`/разбора). Итог — ровно таблица §5.2.

Почему не одно правило `remote_ref_delete` с `context` (буквальное чтение
комментария): (а) AC2 без исключений → правило с незнакомым контекстом не
срабатывает → в #23 удаление remote-веток открыто **для всех ролей** — дыра в
стоп-листе; (б) одно правило не выражает «безусловно для всех, условно для
pr-shepherd» без спецлогики в движке; (в) §8.2 дедуплицирует по `id`, поэтому
две записи с одним `id` невозможны. Порядок двух правил между собой не важен:
после #24 их множества ролей не пересекаются.

### D6. `readonly_roles` (AC4)

Ровно 12, дословно §8.1: `auditor, auditor-deep, explorer, architect,
reviewer, bug-hunter, expert-panel, evaluator, evaluator-telemetry,
evidence-auditor, north-star-auditor, plan-reviewer`. **pr-shepherd нет.**
Правило — `readonly_mutation`: `tools: [Bash]`, `roles: $readonly_roles`,
`match: $mutating_bash_patterns`, без context. Футер issue #23 про
pr-shepherd в `readonly_roles` и тест «pr-shepherd `git checkout main` → deny»
**не исполнять** — он написан до правки §5.4. Вместо него (комментарий
владельца): pr-shepherd `git checkout main && git pull --ff-only` → allow,
`git add -u && git commit -m "stamp"` → allow, `gh pr merge 7 --admin` → deny `admin_merge`.

### D7. Публичный интерфейс `zprof-guard.py` (AC2)

```text
TOOLS_GUARDED = {"Bash", "Edit", "Write", "MultiEdit", "NotebookEdit"}
CONTEXTS: dict[str, Callable[[dict, dict, dict], bool | str]] = {}   # пуст в #23

project_root(payload: dict) -> str
    $CLAUDE_PROJECT_DIR, если задан и каталог; иначе payload["cwd"]; иначе os.getcwd().
    Корень для guard.json и .agentlog (в linked worktree cwd ≠ корень, а
    .claude/ там может отсутствовать — иначе guard молча выключится у субагента).

resolve_role(payload: dict) -> str
    §4: (1) payload["agent_type"] непуст → он; (2) transcript_path: родитель
    каталога == "subagents" и имя ^agent-(.+)\.jsonl$ → agentType из
    соседнего agent-<id>.meta.json; нет файла/ключа/битый JSON → "unknown";
    (3) имя *.jsonl и родитель ≠ "subagents" → "main" (существование файла не
    проверяется); (4) иначе "unknown". Не бросает.

dispatch_id(payload: dict) -> str | None
    toolUseId из того же meta.json; для main/unknown — None. Чтение meta —
    общий приватный _read_meta(transcript_path) -> dict | None.

normalize_command(cmd: str) -> str
    lstrip → снять один ведущий "rtk proxy " либо "rtk " → re.sub(r"\s+", " ")
    → strip. Кавычки не разбираются (§13: `git commit -m "drop -n flag"` → deny принят).

working_dir(cmd: str, cwd: str) -> str
    cmd — уже нормализованная. ^cd\s+(<path>)\s*(&&|;) , <path> — "…", '…' или \S+;
    ~ раскрыть; относительный → join(cwd, path); normpath. Иначе cwd.
    В #23 движком не используется — API для evaluator'ов #24/#25.

load_config(root: str) -> dict | None
    <root>/.claude/guard.json. Нет файла → None (guard не развёрнут/выключен — не
    ошибка). Не парсится / не dict / version != 1 → исключение (→ error).
    Только json, никакого YAML.

evaluate_rules(call: dict, config: dict) -> dict | None
    call — payload, обогащённый в pre_tool ключами role, dispatch_id,
    subject (D2), command (нормализованная или None), root.
    Идёт по config["rules"] в порядке списка, D2 + D4.
    Возврат: {"id": <rule id>, "reason": <reason или строка от context>} | None.

deny_output(rule_id: str, reason: str) -> dict
    D8.

write_event(event: dict, root: str) -> None
    Отклонение от брифа: явный root вместо скрытого глобального состояния
    (тестируемость; тот же путь, что у load_config). D9.

pre_tool(payload: dict) -> dict | None
    tool_name ∉ TOOLS_GUARDED → None; config = load_config(root) → None → None;
    role/dispatch_id; role == "unknown" → _note_role_unresolved (D9, один раз
    на session_id); hit = evaluate_rules(call, config); hit → write_event(deny)
    и вернуть deny_output(...); иначе None.

main() -> None
    try: mode = sys.argv[1]; payload = json.load(stdin); mode == "pre-tool" →
    out = pre_tool(payload); out → одна строка json.dumps(out, ensure_ascii=False)
    в stdout. Любой другой mode (в т.ч. "subagent-stop" до #26) → ничего.
    except Exception as e: try write_event({… decision: "error", detail:
    type(e).__name__}, root) except Exception: pass.
    Всегда sys.exit(0). Печать в stdout — последний шаг, после всех вычислений:
    исключение не может оставить частичный JSON. stderr не пишется.
```

### D8. Формат deny (AC5, §5.7 дословно)

```json
{"hookSpecificOutput": {"hookEventName": "PreToolUse",
  "permissionDecision": "deny",
  "permissionDecisionReason": "zprof guard [<id>]: <reason>. Не обходи: верни `verdict: blocked`, reason: <id>."}}
```

`<reason>` = `reason.rstrip().rstrip(".")` — хвост добавляется всегда, двойной
точки не бывает. Exit 0. Нет `updatedInput`, нет `ask`, нет явного `allow`.
`verdict: blocked` совместим с `base_enum` из `verdicts.yaml` (#20).

### D9. Журнал `.agentlog/guard-events.jsonl` (AC5, §7)

Одна JSON-строка на событие, `ensure_ascii=False`, все ключи присутствуют
всегда (null, если нет значения — в отличие от `tool-events.jsonl`, схема
стабильна для reader'а #29):

```json
{"ts":"2026-09-27T14:02:11Z","session_id":"…","event":"pre-tool","role":"implementer",
 "dispatch_id":"toolu_01AbC…","tool":"Bash","rule":"force_push","decision":"deny",
 "target":"git push","input_hash":"a1b2c3d4e5f6","run_id":null}
```

- `ts` — UTC `%Y-%m-%dT%H:%M:%SZ`. `run_id: null` — присваивает коллектор позже (AC7 issue).
- `decision` ∈ `deny | block | allow_unverified | error`; в #23 пишутся только
  `deny` и `error`.
- `target` — первые два whitespace-токена нормализованной команды или basename
  `file_path`/`notebook_path`; полная команда не пишется никогда.
- `input_hash` — sha1 канонического `tool_input` (`json.dumps(sort_keys=True,
  separators=(",",":"), ensure_ascii=False)`)[:12] — копия `_input_hash` из
  `zprof-collect.py:875` (импортировать нельзя: файлы деплоятся раздельно);
  тест сверяет с коллектором.
- `error`: `event` = режим (`"pre-tool"` или `null`, если argv не разобран),
  `decision:"error"`, доп. ключ `detail` = `type(e).__name__`; остальные поля
  best-effort (null, если payload не разобран).
- `role_unresolved`: `event:"role_unresolved"`, `role:"unknown"`, `rule`/`decision`
  null. Один раз на `session_id`: `.agentlog/guard-state.json`
  `{"version":1,"role_unresolved_sessions":[…]}` (хранить последние 200).
- Запись: `mkdir(exist_ok=True)` для `.agentlog`; файл в режиме `a`,
  `fcntl.flock(LOCK_EX)` на его fd, write, flush, `os.fsync`, unlock.
  `guard-state.json` — read-modify-write под `flock` на `.agentlog/.guard.lock`.
  Общий `.agentlog/.lock` коллектора **не** используется: коллектор держит его
  секундами, guard стоит на каждом Bash/Edit.

### D10. Тесты `profiles/base/tests/test_guard.py` (для implementer)

- Загрузка модуля — `importlib.util.spec_from_file_location` (как
  `test_nested_dispatches.py`); end-to-end — `subprocess.run([sys.executable,
  GUARD, "pre-tool"], input=json, cwd=tmp)` с `env` **без `CLAUDE_PROJECT_DIR`**
  (тесты могут идти внутри Claude Code, где он выставлен) и `HOME=tmp/home`.
- Фикстура `guard.json` — питоновский dict-литерал в тесте, `$`-ссылки
  подставлены; `mutating_bash_patterns` — мини-парсер `telemetry.yaml`: строки
  `  - "…"` после ключа, значение через `json.loads` (экранирование YAML
  double-quoted и JSON совпадает для этих строк).
- Дрейф: (1) множество `id` фикстуры == множество `^\s*- id:\s*(\S+)` из
  `guard.yaml` (AC10); (2) рекомендуется: для правил с regex список строк
  `^\s+- '(.*)'$` под каждым `id` в `guard.yaml` == `match` фикстуры.
- Транскрипты: `tmp/home/.claude/projects/<slug>/<session>.jsonl` (main) и
  `…/<session>/subagents/agent-<id>.{jsonl,meta.json}` с `agentType`,
  `toolUseId`. `resolve_role` читает `transcript_path` напрямую, HOME — для
  правдоподобия раскладки.
- Табличные пары (роль implementer, если не сказано иначе): §5.1 — ≥1 deny и
  ≥2 allow на каждое; обязательно `git push` allow, `git push origin +main`
  deny, `git push -n` allow, `git commit -n` deny, `git branch -d x` allow,
  `git commit -m "drop -n flag"` deny (помечен как принятое ложное
  срабатывание §13), `curl … | sh` deny, `curl -o f URL` allow,
  `git push origin --delete x` deny. **`gh pr merge 7 --squash --delete-branch`
  allow — от роли `pr-shepherd`**: от implementer его денаит `merge_role`.
- `readonly_mutation`: reviewer `git checkout -b x` deny, reviewer
  `git diff` / `grep -rn x .` allow, implementer `git commit -m x` allow;
  роль `unknown` + `git commit -m x` → пусто.
- pr-shepherd: D6 (три кейса) + `git push origin --delete feat/x` → deny
  `remote_ref_delete`.
- Незнакомый context не срабатывает: implementer `git rebase main`,
  `git stash`, `gh pr create -t x -b y`; Write в `/etc/hosts`; pr-shepherd
  `gh pr merge 7` → пустой stdout. `merge_role`: main и implementer `gh pr merge 7` → deny.
- Нормализация: `rtk git push --force`, `rtk proxy git push -f`,
  `git status && git push --force`, многострочная команда; `working_dir`:
  `cd sub && …`, `cd "a b"; …`, `cd ~/x && …`, без `cd`.
- Роль: meta.json → agentType; нет meta.json → `unknown` + одна строка
  `role_unresolved` на два вызова с одним `session_id` и вторая — на другой
  `session_id`; `agent_type` в payload имеет приоритет.
- Fail-open (exit 0, пустой stdout, строка `decision:"error"`): битый
  `guard.json`; правило с `match: ['(']`; правило с `match: "$mutating_bash_patterns"`
  (нерендеренная ссылка); stdin не JSON. Нет `guard.json` → пусто и **без** error.
- Deny-вывод: точная строка §5.7 для `force_push`; `reason` с финальной точкой
  не даёт `..`. Событие: ключи D9, `target == "git push"`, полной команды в
  строке нет, `input_hash` == `_input_hash` коллектора.
- Исполняемость: `os.access(GUARD, X_OK)`, первая строка — shebang.

## Consequences

- Форма `guard.yaml`/`guard.json` зафиксирована для #24–#28: новые проверки
  добавляются регистрацией evaluator'а в `CONTEXTS` без правки движка и данных
  (кроме `not_roles: [pr-shepherd]` у `remote_ref_delete` в #24).
- Порядок проверки — только порядок списка; overlay, добавляющий правило, ставит
  его в конец (решение #28 при merge), что совместимо с «первое совпадение = deny»,
  так как правил-allow нет.
- Любая ошибка данных (битый regex, нерендеренная ссылка) выключает guard целиком
  до исправления — цена fail-open; видно по `error` в журнале и в doctor.
- Read-only роли пишут артефакты только через Write/Edit: `cat > file`, `mkdir`,
  `rm` в Bash для них deny (включая scratchpad). `\b(mv|cp|rm|…)\b` ловит и
  `docker run --rm` — ложные deny возможны; проверяется shakedown'ом §1(г).
- `merge_role` активен с первого деплоя (#28), даже если #25 ещё не влит.

## Alternatives considered

- **Одно правило `remote_ref_delete` с `context: branch_pr_merged`** — отвергнуто (D5).
- **Спецветка «незнакомый context → deny»** — нарушает fail-open и AC2; отвергнуто.
- **Пропускать только битое правило, а не весь guard** — больше кода и тихо
  деградирует стоп-лист частично; решено: целиком fail-open + событие.
- **Разрешение `$ref` в скрипте** — дублирует #28 и прячет ошибки рендера; отвергнуто.
- **Поле `exempt_roles` на уровне правила** в дополнение к map — две формы для
  одного; оставлена только map (§8.2/§8.3, AC5 issue).
- **Конфиг по `payload.cwd`** (буква AC2 issue) — в linked worktree `.claude/`
  может отсутствовать, guard бы молчал у субагентов; выбран `$CLAUDE_PROJECT_DIR`
  с fallback на `cwd`.
