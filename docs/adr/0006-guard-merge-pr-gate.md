# ADR 0006: guard — merge-гейт (`merge_preflight`) и PR-гейт (`pr_create_gate`), событие `allow_unverified`

**Date:** 2026-09-28
**Status:** accepted
**Issue:** #25
**Parent:** ADR-0004 (D2, D3, D4, D9), ADR-0005 (E1, E2, E7, E8)
**Plan:** `tasks/plan-issue-25.md` (этот ADR — шаг 1; в шести местах перекрывает план и в четырёх — буквальный текст AC, см. E10)
**Spec:** `docs/superpowers/specs/2026-09-27-guard-hooks-design.md` §5.5, §5.6, §7, §11, §13

## Context

`guard.yaml` уже содержит `merge_role`, `merge_preflight`, `pr_create_gate`
(ADR-0004 D3); два последних ссылаются на незарегистрированные контексты и
молчат (D4). Issue #25 регистрирует их. Спека и AC задают поведение одной
таблицей; для детерминированной реализации этого мало:

1. **Нет исхода «allow с записью».** `pre_tool()` знает только `deny` (через
   `hit`) и побочные `context_error` (`decision: null`). AC4/AC5 требуют allow
   **и** событие `decision: allow_unverified`. Сигнатура `CONTEXTS`
   `(call, rule, config) -> bool | str | None` неизменна (D4); у evaluator'а нет
   `session_id`/`input_hash`/`target` (тот же довод, что ADR-0005 E2).
2. **Три источника расходятся в имени события.** Спека §5.5: «событие
   `preflight_unverified`»; AC4: `decision: allow_unverified, rule:
   preflight_unverified`; спека §7: `decision ∈ deny|block|allow_unverified|error`
   (т.е. `parse_error` — не `decision`).
3. **Нормализованная команда уничтожает тело PR.** `call["command"]` прошёл
   `normalize_command`, который схлопывает `\s+` в пробел — включая переводы
   строк **внутри кавычек**. Тело `-b "Closes #12\n\n## Gate\n…"` после
   нормализации — `Closes #12 ## Gate …`, и `(?m)^##\s+Gate\b` не совпадает
   никогда. Разбирать тело по `call["command"]` нельзя.
4. **Кавычки ломают поиск номера PR.** Regex «первый `\d+` после `merge`» на
   `gh pr merge --squash --subject "fix 3 bugs" 7` даёт PR #3. У `gh pr merge`
   есть флаги со значением (`-t`, `-b`, `-F`, `-A`, `-R`, `--match-head-commit`).
5. **Два сетевых вызова там, где хватает одного.** AC2 предписывает
   `gh pr view --json number` (fallback) и затем `gh pr view <N> --json body,…` —
   до 2×10 с. `gh pr view` без селектора сам резолвит PR текущей ветки.
6. **Не тот репозиторий.** `gh api repos/o/r/pulls/7/merge` и `gh pr merge 7
   -R o/r` мержат PR в `o/r`; `gh pr view 7` в `wd` смотрит default-репо
   каталога — проверяется чужой PR (ложный allow или ложный deny). То же для
   URL: номер из хвоста URL теряет owner/repo.
7. **`shlex` не bash.** По умолчанию `shlex` считает `#` началом комментария и
   внутри слова: `-t a#b -b x` → `['-t', 'a']`, тело потеряно (ложный deny
   «нет тела»). `2>&1` даёт позиционный токен `2`: `gh pr merge 2>&1` → «PR #2».
8. **AC1 не выполнен текущим кодом.** Для роли `unknown` причина `merge_role`
   должна говорить «роль не разрешена, проверь `zprof doctor`» (§13). Сейчас
   причина статическая; план считает, что по AC1 правок нет, — это не так.

## Decision

### F1. Событие `allow_unverified`: отдельный накопитель `call["unverified"]`

Сигнатура `CONTEXTS` не меняется. `context_errors` **не** переиспользуется:
`context_error` — «контекст не смог ответить, правило молчит, `decision:
null`» (ADR-0005 E2); `allow_unverified` — осознанный fail-open *решения*
гейта, у него ненулевой `decision`, и reader/doctor (#28/#29) должны отличать
их по `event`/`decision` без разбора `detail`.

1. Константы: `_PREFLIGHT_UNVERIFIED = "preflight_unverified"`,
   `_PARSE_ERROR = "parse_error"` — закрытое множество кодов.
2. Приватный `_note_unverified(call: dict, rule: dict, code: str, detail: str)
   -> None`: `call.setdefault("unverified", []).append({"rule":
   rule.get("id"), "detail": f"{code}: {detail}"})`. Никакого I/O;
   `setdefault` — для юнит-вызова с «голым» `call`.
3. `pre_tool()`:
   - в `call` добавляется `"unverified": []` (после `"context_errors": []`);
   - в существующем `finally` **после** цикла по `context_errors` (он не
     меняется) — второй цикл по `call.get("unverified") or []`, каждый элемент —
     `_safe_write_event(...)` события ниже.
4. Формат (ключи D9 + `detail`, порядок ключей как у `context_error`):

   ```json
   {"ts":"…","session_id":"…","event":"pre-tool","role":"pr-shepherd",
    "dispatch_id":"toolu_…","tool":"Bash","rule":"merge_preflight",
    "decision":"allow_unverified","detail":"preflight_unverified: gh pr view: exit 1",
    "target":"gh pr","input_hash":"…","run_id":null}
   ```

   - `event: "pre-tool"` — как у `deny`: это решение pre-tool режима;
     `decision` различает `deny`/`allow_unverified` (§7 перечисляет именно
     `decision`).
   - `rule` — **id правила guard.yaml** (`merge_preflight` / `pr_create_gate`),
     не код причины. Инвариант журнала: `rule` — всегда id правила или `null`
     (так у `deny` и `context_error`; `zprof stats` «top rules» считает по
     `rule`). Код причины нельзя класть в `rule`: `preflight_unverified`
     порождают оба правила (merge — сбой `gh`; create — `-F -`), а `target`
     у обоих `gh pr`, — источник был бы потерян.
   - `detail` = `"<code>: <specifics>"`, `<code>` ∈ {`preflight_unverified`,
     `parse_error`} — первый токен до `": "`; reader извлекает его
     `detail.split(":", 1)[0]`. `<specifics>` — только фиксированные строки и
     имена классов исключений: без путей, тела, stdout/stderr (§7).
   - `parse_error` — **не** `decision`, а код в `detail`; `decision` всегда
     `allow_unverified`.
5. Порядок в журнале: `role_unresolved` → `context_error`×N →
   `allow_unverified`×N → `deny`. Событие пишется и когда более позднее
   правило всё же дало `deny` (составная команда): оно фиксирует, что *это
   правило* пропустило вызов непроверенным, а не итог вызова; итог — строка
   `deny` с тем же `input_hash`. P7 его не считает (фильтр `deny|block`).
6. Evaluator после `_note_unverified` возвращает `False` (правило не
   срабатывает).

### F2. Общий лексер и поиск вызова `gh`

Оба evaluator'а разбирают **сырую** команду `call["tool_input"].get("command")`
(не `call["command"]`, Context п.3). Не строка → `return False`.

```text
_shell_tokens(raw: str) -> list[str]         # raises ValueError
_invocations(tokens, words: tuple[str, ...]) -> list[list[str]]
```

`_shell_tokens`: `lex = shlex.shlex(raw, posix=True, punctuation_chars=True)`;
`lex.whitespace_split = True`; `lex.commenters = ""` (Context п.7);
`return list(lex)`. `ValueError` (незакрытая кавычка, висящий `\`) не
перехватывается здесь — это решает вызывающий. Проверено на Python ≥3.8:
операторы `&&`, `||`, `;`, `|`, `>&`, `<<`, `(`, `)` выходят отдельными
токенами, содержимое кавычек остаётся одним токеном с сохранёнными `\n`.

`_invocations(tokens, words)`: для каждого `i`, где
`os.path.basename(tokens[i]) == words[0]` и `tokens[i+1:i+len(words)] ==
list(words[1:])` (так `rtk gh pr create`, `/opt/homebrew/bin/gh pr create`
находятся без спец-кода), `args` = токены после `words` до первого
**операторного** токена (непустой, все символы из `();<>|&`) или конца. Если
операторный токен начинается с `<` или `>`, а последний элемент `args` —
`^\d+$`, этот элемент отбрасывается (fd-редирект `2>&1`, Context п.7).
Возвращает список `args` по всем вхождениям в порядке появления.

Ноль вхождений (regex правила совпал с текстом внутри кавычек: `grep -r "gh
pr create" docs/`, `echo "…gh pr merge 7…"`) → evaluator `return False`
**без события**. Принятое ограничение: обёртки `bash -c "gh pr create
--fill"`/`eval` не проверяются (как и остальной regex-стоп-лист §13, guard —
ограждение, не песочница); событие на каждый `grep` по докам этого же репо
было бы шумом.

Регулярки тела (модульные константы, одни на оба правила):
`_CLOSES_RE = re.compile(r"(?i)\bcloses\s+#\d+")`,
`_GATE_RE = re.compile(r"(?m)^##\s+Gate\b")`.
`_missing_markers(body: str) -> list[str]` возвращает подмножество
`["`Closes #`", "раздела `## Gate`"]` в этом порядке.

### F3. `merge_preflight`

Правило уже ограничено `roles: $merge_roles` и match; evaluator видит только
merge-вызовы роли из `merge_roles`. Общего `try/except Exception` **нет**
(в отличие от ADR-0005 E6): ожидаемые сбои перехватываются явно ниже,
непредвиденное исключение — баг, уходит в `main()` → allow + `error` (ADR-0004
D2: сломанное правило видно в журнале/doctor). Итог по направлению тот же —
allow.

**1. Разбор.** `tokens = _shell_tokens(raw)`; `ValueError` →
`_note_unverified(…, _PARSE_ERROR, "shlex")`, `return False`.
`wd = working_dir(call.get("command") or "", call.get("cwd") or call["root"])`.
Цели проверки — список пар `(selector: str | None, repo: str | None)`:

- **`gh pr merge`** — для каждого `args` из `_invocations(tokens, ("gh",
  "pr", "merge"))` разбор флагов по семантике pflag:
  - флаги со значением: long `--subject --body --body-file --author-email
    --match-head-commit --repo`, short `t b F A R`;
  - long: `--name=value` — значение слитно; `--name` из списка со значением —
    значение = следующий токен (он потребляется); прочие `--x` — булевы;
  - short-кластер `-xyz` (токен `^-[A-Za-z]+.*`): символы слева направо; первый
    символ из множества со значением забирает остаток токена как значение
    (`-Ro/r`), а при пустом остатке — следующий токен; остальные символы —
    булевы (`-d`, `-s`, `-m`, `-r`);
  - `--` — дальше только позиционные;
  - значение `-R/--repo` запоминается (последнее побеждает, как pflag);
  - первый позиционный токен — `selector` (номер, URL или ветка — `gh pr view`
    принимает все три), остальные позиционные игнорируются.
  - `selector` начинается с `-` (только после `--`) → `_note_unverified(…,
    _PARSE_ERROR, "selector")`, эта цель пропускается. То же для `repo`,
    начинающегося с `-`. Защита от инъекции опции в argv `gh`.
- **`gh api`** — для каждого `args` из `_invocations(tokens, ("gh", "api"))`:
  первый токен, где `re.search(r"/?pulls/(\d+)/merge\b", tok)` находит
  совпадение (`/pulls/` может быть в начале токена без `/`). Затем
  `m = re.match(r"^/?repos/([^/]+)/([^/]+)/pulls/(\d+)/merge/?$", tok)`:
  - `m` и owner/repo оба `^[A-Za-z0-9_.-]+$` → `(m[3], f"{m[1]}/{m[2]}")`;
  - `m`, но owner/repo — плейсхолдеры `{owner}`/`{repo}`/`:owner`/`:repo` →
    `(m[3], None)` (gh api сам подставляет текущий репо — совпадает с `wd`);
  - иначе → `(номер из re.search, None)`.
  Ни одного такого токена в этом вызове → вызов пропускается.

Пустой список целей → `return False` без события (F2). Несколько целей —
проверяются по порядку, первая `deny` возвращается.

**2. Данные — один вызов на цель:**

```text
argv = ["gh", "pr", "view", *([selector] if selector else []),
        *(["-R", repo] if repo else []),
        "--json", "number,body,closingIssuesReferences,state"]
rc, out = _run(argv, wd, _GH_TIMEOUT)
```

Без `selector` это и есть fallback AC2 («`gh pr view --json number` в
рабочем каталоге»), слитый с запросом данных (Context п.5). URL передаётся
селектором целиком — gh резолвит owner/repo из URL (Context п.6), отдельный
regex «`\d+` в конце URL» не нужен.

**3. Отказы — единая точка, один код.** Любой сбой получения данных →
`_note_unverified(call, rule, _PREFLIGHT_UNVERIFIED, <specifics>)`, цель
пропускается (следующая цель ещё проверяется), в итоге `False`:

| Сбой | `<specifics>` |
|---|---|
| `rc is None` | `_context_detail(argv, rc, out)` → `gh pr: TimeoutExpired` / `gh pr: FileNotFoundError` |
| `rc != 0` | `_context_detail(argv, rc, out)` → `gh pr: exit 1` |
| `json.loads` → `ValueError` | `gh pr view: invalid json` |
| не `dict`; `number` не `int`; `body` не `str`; `closingIssuesReferences` не `list`; `state` не `str` | `gh pr view: unexpected shape` |

Одна точка, а не отдельные коды: для гейта все эти случаи значат одно — «данных
PR нет, решение fail-open» (§5.5, решение 1); различие нужно только
диагностике, и оно в `<specifics>`. `bool` не считается `int` для `number`
(`isinstance(x, bool)` → shape).

**4. Решение по данным** (`n = data["number"]`):

- `state != "OPEN"` → цель пропускается, **без события**: закрытый/смерженный
  PR смержить нельзя, защищать нечего; попутно не блокируется
  `gh api …/pulls/N/merge` (GET-проверка «смержен ли») после merge.
- `closes_ok = bool(_CLOSES_RE.search(body)) or len(closingIssuesReferences) > 0`;
  `gate_ok = bool(_GATE_RE.search(body))`.
- оба ok → следующая цель;
- иначе `return` строка (перекрывает `rule["reason"]`, `_check_rule`):
  - только нет closes → `` f"PR #{n} без `Closes #`" ``;
  - только нет gate → `` f"PR #{n} без раздела `## Gate`" ``;
  - оба → `` f"PR #{n} без `Closes #` и без раздела `## Gate`" ``.

Итоговая причина: ``zprof guard [merge_preflight]: PR #7 без раздела `## Gate`. Не обходи: …`` (`deny_output`, без точки в конце строки evaluator'а).

### F4. `pr_create_gate`

Правило **без** `roles`/`not_roles` — осознанно, не ошибка конфигурации:
§5.6 «любой, кто создаёт PR в zprof-проекте, даёт `Closes #N` и `## Gate`»
(pre-flight `pr-shepherd.md:45-49`, перенесённый на момент создания).
Применяется к `main`, `unknown`, pr-shepherd, implementer. Точечное снятие —
только `exempt_roles.pr_create_gate` в `.zprof.yaml`. `_run` не вызывается
никогда — сети нет.

**1. Разбор.** `tokens = _shell_tokens(raw)`; `ValueError` →
`_note_unverified(…, _PARSE_ERROR, "shlex")`, `return False` (AC5: allow,
не deny, не `error`). `wd` — как в F3. Для каждого `args` из
`_invocations(tokens, ("gh", "pr", "create"))` — флаги по pflag, как в F3:

- со значением: long `--body --body-file --title --base --head --assignee
  --label --milestone --project --reviewer --repo --template --recover`,
  short `b F t B H a l m p r R T`;
- fill: long `--fill`, `--fill-first`, `--fill-verbose` (точное равенство
  имени, в т.ч. в форме `--fill=…`), short `f` в любом месте кластера до
  символа со значением (`-f`, `-df`, `-fd`);
- прочее (`-d`, `-w`, `-e`, `--dry-run`, …) — булевы, игнорируются;
  позиционные игнорируются.

**Слитные формы (AC5) — поддерживаются, решение ADR, не implementer'а:**
`--body=X`, `--body-file=X`, `-bX`, `-FX`, `-dbX` разбираются как у gh
(pflag). Отказ от них дал бы ложный deny «нет тела» на вызов, который gh
принимает. Значение, потреблённое флагом, никогда не трактуется как флаг:
`-t "x -b y"` — заголовок, тела нет → deny.

Источники тела — список в порядке появления: `("inline", value)` для
`-b/--body`, `("file", value)` для `-F/--body-file`.

**2. Решение по одному вызову** (строго в этом порядке):

1. любой fill-флаг → deny: `` "`--fill*` запрещён: тело PR должно содержать `Closes #N` и раздел `## Gate` — передай --body или --body-file" ``.
   Приоритет над телом: `--fill -b "<валидное>"` → deny (AC5 — дизъюнкция).
2. источников нет → deny: `` "нет тела PR: передай --body/-b или --body-file/-F с `Closes #N` и разделом `## Gate`" ``.
   Сюда же `-w/--web`, `-e/--editor`, `-T/--template` без `-b/-F`.
3. каждый источник по порядку (**все** должны пройти; `-b` вместе с `-F` gh
   сам отвергает, повторный `-b` — последний побеждает у pflag; проверка всех
   строже и монотонна):
   - `inline` → `body = value`;
   - `file`:
     - `value in ("-", "/dev/stdin", "/dev/fd/0")` →
       `_note_unverified(…, _PREFLIGHT_UNVERIFIED, "body-file: stdin")`,
       вызов считается непроверенным, к следующему вызову (stdin guard'а — это
       payload хука, читать нельзя);
     - `path = os.path.expanduser(value)` (bash раскрывает `~` в незакавыченном
       слове; в закавыченном `~` не бывает на практике); не абсолютный →
       `os.path.join(wd, path)`;
     - `not os.path.isfile(path)` →
       `_note_unverified(…, _PREFLIGHT_UNVERIFIED, "body-file: not a regular file")`,
       к следующему вызову. `isfile` до `open` обязателен: FIFO/устройство
       заблокировали бы guard;
     - `open(path, encoding="utf-8", errors="replace")` + `.read(1_048_576)`;
       `OSError` → `_note_unverified(…, _PREFLIGHT_UNVERIFIED, f"body-file: {type(e).__name__}")`.
   - `missing = _missing_markers(body)`; непусто → deny:
     `` "тело PR без `Closes #`" `` / `` "тело PR без раздела `## Gate`" `` /
     `` "тело PR без `Closes #` и без раздела `## Gate`" ``.
4. Первый deny среди вызовов возвращается; иначе `False`.

**Ошибка чтения `--body-file` → `preflight_unverified`, не `parse_error` и не
deny.** `parse_error` зарезервирован за «текст команды не разобран»; здесь
команда разобрана, недоступны *данные* — тот же класс, что `-F -` и сбой `gh`.
Не deny: guard вычисляет каталог эвристикой `working_dir()` (не видит
`pushd`, `git -C`, `cd` в середине цепочки) — при расхождении с gh файл есть
для gh и отсутствует для guard, deny был бы ложным на валидном PR; если файла
действительно нет, gh упадёт сам и PR не создаст — allow ничего не стоит.

**Принятые ограничения** (фиксируются тестами там, где указано):

- тело в `"$(cat <<'EOF' … EOF)"` — один токен с реальными `\n`, маркеры
  проверяются по содержимому heredoc → работает (тест);
- `-b "$(cat body.md)"` → тело — литерал `$(cat body.md)` → deny; модель
  переходит на `--body-file` (тест не обязателен);
- ANSI-C `$'…\n…'` → `shlex` оставляет `\n` литералом → `## Gate` не в начале
  строки → deny; то же для `-b "a\n## Gate"` в bash — там это и правда не
  перевод строки, deny корректен;
- `gh pr create -F - <<'EOF'` → `preflight_unverified` (heredoc как stdin не
  читается — фаза 2);
- аргументы после редиректа (`gh pr create > log -b x`) не видны → deny «нет
  тела».

### F5. AC1: причина `merge_role` для роли `unknown`

Форма `merge_role` не меняется (ADR-0004 D3: без `context`), движок
(`_check_rule`/`evaluate_rules`) не меняется — `test_evaluate_rules_*`
сравнивают `hit` на равенство `{"id", "reason"}`.

В `pre_tool()`, после `if hit is None: return None`, до вычисления
`clean_reason`:

```text
if role == "unknown" and _is_role_gated(config, hit["id"]):
    reason = f"{str(hit['reason']).rstrip().rstrip('.')}; роль не разрешена (роль вызывающего не определена — unknown), проверь `zprof doctor`"
```

`_is_role_gated(config, rule_id) -> bool`: первый `dict` в `config["rules"]`
с `id == rule_id`; `True`, если непусты `roles` или `not_roles`. Критерий
общий, без id в коде: подсказка добавляется ровно тогда, когда deny зависит от
роли, а роль не определена (§13) — `merge_role` и `remote_ref_delete`
(`not_roles: [pr-shepherd]`; неразрешённый pr-shepherd получает тот же deny).
Для `force_push` и прочих ролевонезависимых правил подсказки нет — там она
вводила бы в заблуждение. Событие `deny` не меняется (у него нет поля причины).

### F6. Регистрация и правки вне evaluator'ов

- Новая секция `# Merge/PR gates (ADR-0006, #25)` после `_branch_pr_merged`,
  до `CONTEXTS.update`; в `CONTEXTS.update({...})` добавляются
  `"merge_preflight": _merge_preflight, "pr_create_gate": _pr_create_gate`.
- `import shlex` — stdlib.
- Комментарий над `CONTEXTS` (строки 30–32) — убрать «§5.5/§5.6, #25 simply
  never fire»; docstring `working_dir()` — «used by #24/#25 evaluators».
- `guard.yaml`: форма и данные **не меняются**; только комментарии секций
  `# --- §5.5 merge-гейт …` и `# --- §5.6 PR-гейт (данные; evaluator — #25) ---`
  → «активно с #25». `merge_role`/`not_roles` не трогаются: `unknown ∉
  merge_roles` уже даёт deny (`test_merge_role_denies_unknown`).

### F7. Дрейф `test_guard.py::UNKNOWN_CONTEXT_CASES` (аналог ADR-0005 E8)

После регистрации два кейса перестают быть «незнакомым контекстом»:

- `("Bash", {"command": "gh pr create -t x -b y"}, "implementer")` — теперь
  детерминированный `deny` `pr_create_gate` (тело `y`);
- `("Bash", {"command": "gh pr merge 7"}, "pr-shepherd")` — теперь вызывает
  `gh` на машине разработчика; результат зависит от установленного/залогиненного
  `gh` и cwd — недетерминирован.

Implementer: удалить оба кортежа из `UNKNOWN_CONTEXT_CASES`; в комментарии над
списком заменить фразу про «genuinely unregistered context
(`pr_create_gate`/`merge_preflight`, #25)» на «#25 registered
`merge_preflight`/`pr_create_gate` (ADR-0006) — those cases moved to
`test_guard_merge.py` with a monkeypatched `_run`». Имя теста не менять (кейсы
`rebase`/`stash` остаются). Больше в `test_guard.py` меняется только
`test_merge_role_denies_unknown`: добавить `assert "zprof doctor" in reason`
(F5), и в `test_merge_role_denies_implementer_and_main` — `assert "zprof
doctor" not in reason`.

### F8. Тесты: `monkeypatch _run` — основной путь; PATH-заглушка — два e2e

Новый файл `profiles/base/tests/test_guard_merge.py`, загрузка и хелперы —
импорт из `test_guard.py` (`zprof_guard`, `build_guard_config`,
`_write_config`, `_payload`, `_bash`, `_read_events`, `_run_guard`), как в
`test_guard_context.py`.

**Табличные кейсы — через `monkeypatch.setattr(zprof_guard, "_run", fake)`**
(паттерн ADR-0005 E1; PATH-заглушек в репо нет). `fake` пишет `(argv, cwd,
timeout)` в список и отдаёт ответ из таблицы — тест проверяет argv, `wd` и
`timeout == zprof_guard._GH_TIMEOUT`. Для всех кейсов `pr_create_gate` и для
ролей вне `merge_roles` `fake` бросает `AssertionError` (сети нет — часть AC7).
Таймаут — `(None, "TimeoutExpired")`, не заглушка с задержкой (иначе 10 с на
тест).

**AC6 буквально требует PATH-заглушку** — выполняется двумя e2e-тестами
поверх `_run_guard` (реальный subprocess guard'а): в `tmp_path/bin/gh` —
`#!/bin/sh` скрипт (`chmod 0o755`), `env["PATH"] = f"{bin}:{os.environ['PATH']}"`.
`_run_guard` для этого получает необязательный параметр `env_extra: dict |
None = None` (единственная правка сигнатуры хелпера; существующие вызовы не
меняются). Кейсы: (а) скрипт печатает валидный JSON (оба маркера,
`state: OPEN`) → пустой stdout; (б) `exit 1` → пустой stdout + событие
`allow_unverified`. Они проверяют то, что monkeypatch не видит: реальный
`_run` (env, `stdin=DEVNULL`, cwd) и argv, дошедший до бинаря (скрипт пишет
`"$@"` в файл, тест его сверяет).

Обязательные кейсы (минимум; роль `pr-shepherd`, если не указано иное):

- merge: валидный PR → `None`, argv `["gh","pr","view","7","--json","number,body,closingIssuesReferences,state"]`, событий нет;
  нет `Closes`, refs пусты → причина содержит ``PR #7 без `Closes #` ``; есть
  `Closes`, нет Gate → ``без раздела `## Gate` ``; оба нет → комбинированная;
  refs непусты без `Closes` в body + Gate → allow;
  `gh pr merge` без селектора → argv без селектора, номер в причине — из JSON;
  URL → селектор — URL целиком; `gh pr merge 7 -R o/r` и `-Ro/r` → `-R o/r` в argv;
  `gh pr merge --squash --subject "fix 3 bugs" 7` → селектор `7`;
  `gh pr merge 2>&1` → без селектора;
  `gh api -X PUT repos/o/r/pulls/12/merge` → `12` + `-R o/r`;
  `gh api repos/{owner}/{repo}/pulls/12/merge` → `12` без `-R`;
  `state: MERGED` без Gate → `None`, событий нет;
  сбои `(1, "")`, `(None, "TimeoutExpired")`, `(0, "not json")`, `(0, "[]")` →
  `None` + ровно одно событие: `event == "pre-tool"`, `decision ==
  "allow_unverified"`, `rule == "merge_preflight"`,
  `detail.startswith("preflight_unverified: ")`, множество ключей == ключам
  `context_error` (D9 + `detail`);
  незакрытая кавычка в merge → `detail.startswith("parse_error: ")`;
  роли `implementer`, `main`, `unknown` на `gh pr merge 7` и `gh api
  …/pulls/12/merge` → deny `merge_role`, `fake` не вызван; `unknown` —
  с `zprof doctor`.
- create (параметризовать ролями `implementer`, `main`, `pr-shepherd`):
  allow — `-b` с реальным `\n` (Python-строка), `--body=…`, `-b…` слитно,
  `-F body.md` (файл в `tmp_path`), `cd sub && gh pr create -F body.md`
  (файл в `tmp_path/sub`), `rtk gh pr create -b …`, тело в `"$(cat <<'EOF' … EOF)"`;
  deny — без тела; `-t "x -b y"`; `--fill`, `--fill-first`, `--fill-verbose`,
  `-f`, `-df` — каждый с валидным `-b`; без Gate; без Closes; `-b` с
  литеральным `\\n` перед `## Gate`;
  allow + `preflight_unverified` — `-F -`, `--body-file missing.md`;
  allow + `parse_error` — `gh pr create -b "unclosed`;
  `echo "gh pr create --fill"` → `None`, событий нет.
- регресс: оба кейса, удалённые из `UNKNOWN_CONTEXT_CASES` (F7).

### F9. AC7 и сеть

`gh` из guard вызывают ровно два evaluator'а: `branch_pr_merged` (#24,
ADR-0005 E6) и `merge_preflight`. `pr_create_gate` сети не трогает. AC7
«`gh` вызывается только в этих двух правилах» писался до #24 — читается как
«#25 добавляет `gh` только в `merge_preflight`». Проверка — тесты с
`fake`, бросающим при вызове.

### F10. Отклонения от плана и от буквального текста AC

| Источник | Было | ADR | Почему |
|---|---|---|---|
| AC4 | `rule: preflight_unverified` | `rule` = id правила; код в `detail` (`"preflight_unverified: …"`) | инвариант «`rule` — id правила» (deny, context_error, top rules); код порождают оба правила — источник иначе теряется (F1) |
| AC2 | `gh pr view --json number`, затем `gh pr view <N> --json body,…` | один вызов `gh pr view [sel] [-R r] --json number,body,closingIssuesReferences,state` | 1 сетевой вызов вместо 2 (≤10 с вместо ≤20 с); fallback — тот же вызов без селектора (F3.2) |
| AC2 / §5.5 | номер = `\d+` в конце URL | URL передаётся селектором; `-R` из `repos/o/r/…` и `-R/--repo` | проверяется тот PR и тот репо, что мержится (Context п.6) |
| AC2 | regex «первый `\d+` после `merge`» | токенный разбор по pflag на `shlex` | `--subject "fix 3 bugs"`, `2>&1` (Context п.4, п.7) |
| AC6 | `gh` подменяется через PATH во всех тестах | таблица — monkeypatch `_run`; PATH-заглушка — 2 e2e; таймаут — monkeypatch | скорость и проверка argv; реальный путь `_run` покрыт e2e (F8) |
| AC7 | `gh` только в двух правилах #25 | `gh` в `merge_preflight` и `branch_pr_merged` (#24) | AC писался до #24 (F9) |
| план, «Предварительная проверка» | по AC1 правок нет | подсказка `zprof doctor` для `unknown` в `pre_tool()` | AC1 требует её явно; сейчас не реализована (F5) |
| план шаг 3/4 | разбор `call["command"]` / резка по `&&\|;` | сырой `tool_input.command`, `shlex` с `punctuation_chars`, `commenters=""` | нормализация стирает `\n` тела (Context п.3); `#` в слове (п.7) |
| план шаг 1 п.4 | чтение `--body-file` — решить | ошибка → `preflight_unverified`, `isfile` до `open` | F4, обоснование там же |
| план шаг 1 п.5 | слитные формы — на усмотрение implementer'а | поддерживаются (pflag) | ложный deny на вызов, который gh принимает |

## Consequences

- Движок меняется в одном месте — `pre_tool()`: второй накопитель + цикл в
  `finally` (F1) и подсказка для `unknown` (F5). `_check_rule`,
  `evaluate_rules`, сигнатура `CONTEXTS`, форма `guard.yaml`/`guard.json` —
  без изменений.
- В журнале появляется `decision: "allow_unverified"` (`event: "pre-tool"`,
  `detail` с кодом). Reader #29 и doctor #28 различают его по `decision`; P7 не
  считает. Схема D9 расширяется только значением `decision`.
- **Main-сессия теперь обязана давать `Closes #N` и `## Gate` в теле уже при
  `gh pr create`** (CLAUDE.md «Интеграция ветки»: PR создаёт main). Промпты
  (`pr-shepherd.md`, раздел про PR в CLAUDE.md-шаблонах) этим ADR не меняются;
  рекомендуется отдельной правкой добавить пример `gh pr create --body-file`.
  Сам PR #25 — первый живой e2e: без обоих маркеров его создание получит deny.
- Латентность: `merge_preflight` — один `gh pr view` (≤10 с) на merge-вызов
  роли из `merge_roles`; `pr_create_gate` — только локальный `shlex` и чтение
  файла.
- Непредвиденное исключение в новых evaluator'ах → allow + `error` (не
  `allow_unverified`): баги видны отдельно от сетевых сбоев.

## Alternatives considered

- **Переиспользовать `context_errors` с доп. полем `decision`** — смешивает
  «правило молчит, диагностика» (`decision: null`) и «решение fail-open»;
  `pre_tool()` ветвился бы по полю внутри одного цикла. Отвергнуто.
- **Отдельный `event: "allow_unverified"`, `decision: null`** (по образцу
  `context_error`) — §7 перечисляет `allow_unverified` как значение
  `decision`; reader фильтрует решения по `decision`. Отвергнуто.
- **Код причины в `rule`** (буквально AC4) — теряет правило-источник,
  ломает инвариант `rule`. Отвергнуто (F1).
- **Отдельный ключ `code`** вместо префикса `detail` — ещё одно поле схемы D9
  ради двух значений; `detail` уже существует для `error`/`context_error`.
  Отвергнуто; формат префикса зафиксирован и тестируется.
- **Разбирать `call["command"]`** — нормализация уничтожает `\n` тела;
  `## Gate` не находится никогда. Отвергнуто.
- **`shlex.split` всей строки + поиск флагов по всем токенам** — `ls -b x &&
  gh pr create --fill` подмешивает чужой `-b`. Отвергнуто в пользу
  `_invocations` с границей по операторам.
- **Резка по `re.split(r"&&|\|\||;|\|")` до `shlex`** (как E6) — режет внутри
  кавычек (`-b "a && b"`). Отвергнуто: `punctuation_chars` делит операторы с
  учётом кавычек.
- **Два вызова `gh` (номер, затем данные)** — буквально AC2; удваивает сетевую
  латентность без выигрыша. Отвергнуто.
- **Fail-closed `merge_preflight`** (как `branch_pr_merged`) — §5.5 явно
  fail-open (решение 1): сбой сети не должен блокировать штатный merge, а
  merge обратим (revert), в отличие от удаления ветки. Отвергнуто.
- **Deny при нечитаемом `--body-file`** — ложный deny при промахе
  `working_dir()`; при реально отсутствующем файле gh падает сам. Отвергнуто.
- **Событие `parse_error` на каждое совпадение regex без вызова `gh`** (ловит
  `bash -c "…"`) — шум на каждом `grep "gh pr create"` в этом репо.
  Отвергнуто; ограничение задокументировано (F2).
- **Причина `unknown` через `context` у `merge_role`** — меняет форму правила
  вопреки ADR-0004 D3. **Статический текст про `zprof doctor` в `reason`** —
  вводит в заблуждение `main`/`implementer`. **`if hit["id"] == "merge_role"`** —
  id в коде движка. Отвергнуты в пользу критерия «правило ролевое» (F5).
- **PATH-заглушка для всех тестов** — медленно (subprocess на кейс), таймаут
  стоит 10 с, argv проверяется через файл. Оставлена для двух e2e (F8).
