# ADR 0005: guard — context-evaluator'ы `head_on_remote`, `linked_worktree`, `write_outside_repo`, `branch_pr_merged`

**Date:** 2026-09-28
**Status:** accepted
**Issue:** #24
**Parent:** ADR-0004 (`docs/adr/0004-zprof-guard-pre-tool-frame.md`), разделы D4, D5, D7, D9
**Plan:** `tasks/plan-1.md` (этот ADR — шаг 1; уточняет и в трёх местах перекрывает шаги 2, 5, 6 — см. «Отклонения от plan-1»)
**Spec:** `docs/superpowers/specs/2026-09-27-guard-hooks-design.md` §4, §5.2, §5.3, §7, §13

## Context

ADR-0004 зафиксировал рамку: реестр `CONTEXTS`, сигнатуру evaluator'а
`(call, rule, config) -> bool | str | None`, политику «незнакомый context не
срабатывает» и данные всех пяти контекстных правил в `guard.yaml`. В #23
`CONTEXTS` пуст. Issue #24 регистрирует четыре evaluator'а. Спека и issue
задают их одной строкой каждый; этого мало для детерминированной реализации:

1. **Где пишется `context_error`.** `pre_tool()` пишет только `deny` и
   `role_unresolved`; у evaluator'а есть `call["root"]`, но нет
   `session_id`/`input_hash`/`target` — полей, обязательных по D9 («все ключи
   присутствуют всегда»). Сигнатуру `CONTEXTS` менять нельзя (D4, от неё
   зависит #25).
2. **Что считать ошибкой.** AC1 требует молчания, когда «нет upstream», а
   `git rev-parse @{u}` без upstream даёт тот же exit 128, что и «не git-репо».
   Буквальное «любой ненулевой exit → `context_error`» превращает каждую
   rebase/amend на неопубликованной ветке в запись об ошибке — шум, который
   сделает `context_error` бесполезным для doctor/reader (#29).
3. **Относительные пути git.** `rev-parse --git-dir`/`--git-common-dir`
   печатают путь относительно своего cwd (`.git`). `os.path.realpath(".git")`
   в процессе guard резолвит его от cwd **guard'а**, а не git — ловушка,
   ведущая к ложному `linked_worktree` и к ложному deny в allowance.
4. **macOS и `$TMPDIR`.** `TMPDIR=/var/folders/…/T/`, а `realpath` файла даёт
   `/private/var/folders/…`. План (шаг 5) предписывает не `realpath`'ить
   префиксы — тогда `$TMPDIR/claude-*` на macOS не совпадает никогда.
5. **Разбор имени ветки** для `branch_pr_merged`: `git push --delete origin x`
   (флаг до remote) регулярным выражением `--delete (\S+)` даёт `origin`;
   `:refs/tags/v1` под паттерн `:<name>` превращается в «ветку» `refs/tags/v1`;
   `--delete a b` удаляет две ветки.
6. **Fail-open движка против fail-closed контекста.** Любое исключение,
   вылетевшее из evaluator'а, уходит во внешний `try/except` `main()` → exit 0
   без вывода → **allow**. Для `branch_pr_merged` это прямо противоречит
   спеке (ошибка → deny).

## Decision

### E1. Общий subprocess-helper `_run` (единственная точка вызова внешних команд)

```text
_run(argv: list[str], cwd: str, timeout: float) -> tuple[int | None, str]
```

- `subprocess.run(argv, cwd=cwd, timeout=timeout, capture_output=True,
  text=True, stdin=subprocess.DEVNULL, check=False, env=<os.environ +
  GIT_TERMINAL_PROMPT=0, GH_PROMPT_DISABLED=1, GIT_OPTIONAL_LOCKS=0>)`.
  Без `shell=True`. `stdin=DEVNULL` обязателен: stdin процесса guard — это
  payload хука, дочерний процесс не должен его читать или ждать ввода.
- Возврат:
  - exit 0 → `(0, stdout)`;
  - ненулевой exit → `(returncode, "")` — stdout не отдаём, stderr не
    разбираем никогда (локаль-зависим);
  - `subprocess.TimeoutExpired` или `OSError` (нет бинаря `git`/`gh`, cwd не
    существует / не каталог) → `(None, type(e).__name__)`.
- Сам не бросает (кроме программных ошибок вроде нестрокового argv) и **сам
  ничего не пишет в журнал** — решение «ошибка это или штатный ответ» и
  запись `context_error` остаются за evaluator'ом (E2), потому что для разных
  вызовов один и тот же exit 128 значит разное (Context п.2).
- Evaluator'ы вызывают `_run` через глобальное имя модуля в момент вызова
  (не связывают в default-аргумент) — чтобы тесты подменяли его
  `monkeypatch.setattr(guard, "_run", fake)`. Это единственный способ
  тестировать `branch_pr_merged` без сети и `gh`.
- Константы: `_GIT_TIMEOUT = 3`, `_GH_TIMEOUT = 10`.

Тонкая обёртка `_git(args, cwd) = _run(["git", *args], cwd, _GIT_TIMEOUT)` —
по желанию implementer'а; обязательна только единая точка `_run`.

Каталог для Bash-контекстов (`head_on_remote`, `linked_worktree`,
`branch_pr_merged`): `wd = working_dir(call["command"], call["cwd"] or
call["root"])` — первое использование `working_dir()` из #23. Если
`call["cwd"]` не строка/пуст — берётся `call["root"]`.

### E2. `context_error`: evaluator копит, `pre_tool()` пишет

Сигнатура `CONTEXTS` (D4) **не меняется**. `context_error` — побочное
диагностическое событие, на allow/deny не влияет.

1. Приватный `_note_context_error(call: dict, rule: dict, detail: str) ->
   None`: `call.setdefault("context_errors", []).append({"rule":
   rule.get("id"), "detail": detail})`. Никакого I/O. `setdefault` — чтобы
   evaluator можно было вызывать в юнит-тестах с «голым» `call`.
2. `pre_tool()` — единственная правка движка в #24:
   - в словарь `call` добавляется `"context_errors": []`;
   - `hit = evaluate_rules(call, config)` оборачивается в `try/finally`; в
     `finally` для каждого накопленного элемента — `_safe_write_event(...)`
     события ниже. `finally` — чтобы ошибки ранних evaluator'ов не терялись,
     если более позднее правило бросило `re.error`/`ValueError` (тогда
     `main()` допишет ещё и `error`).
   - порядок в журнале: `role_unresolved` (если есть) → `context_error`×N →
     `deny` (если есть).
3. Формат события (ключи D9 + `detail`, как у `error`):

   ```json
   {"ts":"…","session_id":"…","event":"context_error","role":"implementer",
    "dispatch_id":"toolu_…","tool":"Bash","rule":"rebase_published","decision":null,
    "detail":"git branch: exit 128","target":"git rebase","input_hash":"…","run_id":null}
   ```

   `detail` = `"<argv[0]> <argv[1]>: exit <rc>"` или `"<argv[0]> <argv[1]>:
   <ExcName>"` (`"git rev-parse: TimeoutExpired"`). Пути, stdout, stderr в
   `detail` не пишутся (§7: секреты/приватные пути). `decision: null` —
   как у `role_unresolved`: это не решение, в P7 не считается (reader #29
   фильтрует `deny|block`).

Почему не пишет сам evaluator: у него нет `session_id`, `input_hash`,
`target`; протаскивать их в `call` ради одного побочного эффекта значит
раздвоить логику построения события (она уже живёт в `pre_tool()` для двух
типов). Накопитель оставляет evaluator'ы без I/O журнала — они тестируются
по возвращаемому значению и `call["context_errors"]`.

**Критерий «ошибка vs штатный ответ»** (единый для всех evaluator'ов):
`context_error` пишется, когда evaluator **не смог получить ответ на свой
вопрос**: `rc is None` (таймаут/OSError) всегда; ненулевой exit — только если
для этого вызова он не может быть нормальным «нет». Таблица — в E3–E5.

### E3. `head_on_remote` (правила `rebase_published`, `amend_published`)

Спецификационная конъюнкция сохраняется, **порядок вызовов меняется** —
дешёвый и однозначный первым:

1. `rc, out = _run(["git", "branch", "-r", "--contains", "HEAD"], wd, 3)`.
   - `rc is None` → `_note_context_error`, `return False`.
   - `rc != 0` → `_note_context_error`, `return False`. В любом git-репо с
     валидным HEAD эта команда даёт exit 0 (пусть с пустым выводом), поэтому
     ненулевой exit — это «не git-репо» или unborn HEAD, т.е. настоящая
     ошибка контекста (AC3, AC6 «`context_error` вне git-репо»).
   - `out.strip() == ""` → `return False` (HEAD не опубликован: amend/rebase
     до push — allow, AC1). **Второй вызов не делается.**
2. `rc, _ = _run(["git", "rev-parse", "--abbrev-ref", "@{u}"], wd, 3)`.
   - `rc == 0` → `return True` (deny).
   - `rc is None` → `_note_context_error`, `return False`.
   - `rc != 0` → `return False` **без** `context_error`: репо уже доказано
     шагом 1, значит ненулевой exit — штатное «нет upstream» или detached HEAD.

Итоги по состояниям: не git-репо → False + `context_error`; нет upstream →
False, тихо; detached HEAD → False, тихо (rebase/amend в detached разрешён);
upstream есть, HEAD не на remote → False, тихо (1 вызов); upstream есть и HEAD
на remote → True. Строки `origin/HEAD -> origin/main` в выводе шага 1 —
обычный непустой вывод, отдельно не обрабатываются.

Выигрыш порядка: в частом случае (локальные коммиты) — один subprocess вместо
двух; «нет upstream» не засоряет журнал.

### E4. `linked_worktree` (правило `stash_in_worktree`)

Один вызов вместо двух: `rc, out = _run(["git", "rev-parse", "--git-dir",
"--git-common-dir"], wd, 3)` — git печатает два значения построчно в порядке
аргументов.

- `rc is None` или `rc != 0` → `_note_context_error`, `return False`. Здесь
  ненулевой exit может значить только «не git-репо» — это ошибка по AC3.
- Разбор: `lines = out.splitlines()`; меньше двух непустых строк →
  `_note_context_error(…, "git rev-parse: unexpected output")`, `return False`.
- **Каждый путь резолвится от `wd`, не от cwd процесса:**
  `git_dir = realpath(join(wd, lines[0]))`, `common = realpath(join(wd,
  lines[1]))` (`join` с абсолютным вторым аргументом возвращает его как есть —
  для linked worktree git печатает абсолютные пути, для основного — `.git`).
- `return git_dir != common`.

Итоги: основной worktree (`.git` == `.git`) → False; linked worktree
(`<main>/.git/worktrees/<name>` ≠ `<main>/.git`) → True; вне репо → False +
`context_error`.

### E5. `write_outside_repo` (правило `write_outside_repo`, Edit/Write/MultiEdit/NotebookEdit)

Субъект — `call["subject"]` (движок уже положил туда `file_path` или
`notebook_path`, D2). `call["command"]` здесь `None`, `working_dir()` **не
используется**.

**Шаг 0. Путь.** `subject` не строка или пуст → `return False` (вызов
инструмента всё равно упадёт на валидации; guard не угадывает). Не абсолютный
→ `join(call["cwd"] or call["root"], subject)`. `~` в `subject` не
раскрывается (инструменты Claude Code его не раскрывают — это был бы
относительный путь). `real = os.path.realpath(path)` — **нестрогий** режим:
существующий префикс резолвится с симлинками, несуществующий хвост
дописывается лексически. Отдельный «подъём к предку» для самого сравнения
префиксов не нужен.

**Шаг 1. Раскрытие одного префикса** (`config["allow_write_prefixes"]`; не
list или элемент не str → `ValueError` → fail-open всего guard'а по D2;
`_validated_str_list` не переиспользуется — он отвергает строки на `$`, а здесь
`$` легитимен):

1. Голова:
   - начинается с `$CLAUDE_PROJECT_DIR` (граница — конец строки или `/`) →
     голова = `call["root"]` (это и есть `$CLAUDE_PROJECT_DIR` либо fallback
     `project_root()` — одна точка правды, env здесь не читается повторно);
   - начинается с `$NAME` (`[A-Za-z_][A-Za-z0-9_]*`) → `os.environ.get(NAME)`;
     не задан или пуст → **префикс пропускается** (не матчит ничего);
   - начинается с `~` → `os.path.expanduser` первого сегмента; не раскрылся
     (нет HOME) → пропуск;
   - иначе — строка как есть.
   `$VAR` поддерживается только в начале префикса.
2. Результат не абсолютный → пропуск. Снять хвостовой `/`, разбить по `/`.
3. **Литеральная голова / glob-хвост**: `k` = индекс первого сегмента,
   содержащего любой из `*?[`; нет такого — `k = len(segs)`.
   `head = realpath("/".join(segs[:k]))` (нестрогий), затем снова разбить на
   сегменты; `tail = segs[k:]`.

   Это **перекрывает** указание плана «сами префиксы не `realpath`'ить»
   (Context п.4): `realpath` берётся только от литеральной части, glob-сегменты
   не трогаются. На macOS `$TMPDIR/claude-*` → `/private/var/folders/…/T` +
   `claude-*`; `/tmp/claude-*` → `/private/tmp` + `claude-*` (совпадает с
   явной записью `/private/tmp/claude-*` — дубликат безвреден);
   `~/.claude/projects/*/memory/` → `realpath(~/.claude/projects)` +
   `["*", "memory"]`.

**Шаг 2. Посегментное сравнение** (алгоритм плана, шаг 5, уточнён):
`rsegs = real.split("/")`; префикс матчит, если `len(head_segs) + len(tail) <=
len(rsegs)` и:
- `rsegs[i] == head_segs[i]` для всех `i < len(head_segs)` — **строгое
  равенство**, не fnmatch: сегменты из `$CLAUDE_PROJECT_DIR`/`HOME`/`TMPDIR`
  могут содержать `[`/`*` в имени каталога и не должны трактоваться как glob;
- `fnmatch.fnmatchcase(rsegs[len(head_segs) + j], tail[j])` для всех `j` —
  `fnmatchcase`, не `fnmatch` (не зависит от `os.path.normcase`); сегмент-`*`
  матчит ровно один сегмент, `/` внутри совпадения невозможен.

Префиксная семантика: хвост пути сверх длины префикса не смотрится.
Посегментное сравнение попутно закрывает дыру наивного `str.startswith`:
корень `/a/proj` не матчит `/a/proj2/x` (`startswith` по строке без
хвостового `/` пропустил бы соседний каталог).

Любой префикс совпал → `return False` (allow). Путь в `.worktrees/` проекта
покрыт префиксом `$CLAUDE_PROJECT_DIR` — отдельной ветки нет.

**Шаг 3. Linked-worktree allowance** (только если ни один префикс не совпал):

1. **Ближайший существующий предок (решение по AC5):** `d =
   os.path.dirname(real)`; пока `not os.path.isdir(d)` и `d != "/"` → `d =
   os.path.dirname(d)`. Безопасность подъёма: `d` — предок целевого пути;
   если `d` лежит внутри linked worktree этого репо, то и целевой путь внутри
   него (allow корректен); если весь worktree не существует, подъём выходит
   за его пределы, git отвечает «не репо» или «чужой репо» → deny.
2. `rc, out = _run(["git", "rev-parse", "--git-common-dir"], d, 3)` —
   эквивалент `git -C <d>` из спеки через `cwd`.
3. `rc == 0` и `realpath(join(d, out.strip())) == join(realpath(call["root"]),
   ".git")` → `return False` (allow). `join(d, …)` обязателен по той же
   причине, что в E4 (вывод может быть относительным).
4. Иначе → `return True` (deny). Журнал:
   - `rc != 0` → **без** `context_error`: запись в каталог вне любого git-репо
     (`/etc/x`) — ожидаемый штатный ответ «нет», а не ошибка контекста;
     событие на каждый такой deny было бы шумом рядом с уже записанным `deny`;
   - `rc is None` (таймаут, нет `git`) → `_note_context_error`: это ровно тот
     случай, когда легитимная запись в linked worktree могла получить ложный
     deny, и журнал должен это объяснить;
   - `rc == 0`, но другой common-dir (чужой репо) → без события.

   Ошибка git здесь **не** ведёт к allow и **не** является fail-closed
   исключением: deny — это уже решение по префиксам, allowance его только
   смягчает.

Ограничения (осознанно не решаются): сравнение путей регистрозависимо, а
APFS по умолчанию нет — запись по пути с другим регистром получит ложный
deny; если сама сессия запущена из linked worktree, `root/.git` — файл, и
allowance для соседних worktree не сработает (спека говорит именно
`$CLAUDE_PROJECT_DIR/.git`). Симлинк внутри репо, указывающий наружу,
резолвится `realpath` и получает deny — это корректно.

### E6. `branch_pr_merged` (правило `remote_ref_delete_unmerged`, `roles: [pr-shepherd]`) — единственный fail-closed

**Явное отклонение от D4.** ADR-0004 D4 и AC3 задают для контекстов «ошибка →
правило не срабатывает + `context_error`». Для `branch_pr_merged` — наоборот:
**любая** неудача (не разобрано имя, ненулевой exit `gh`, таймаут, нет `gh`,
не-JSON, не массив, пустой массив) → `True` → deny. `context_error` не
пишется: ошибка здесь не «молчание», а решение, и оно уже записано событием
`deny`. Цена ложного allow — потерянная несмерженная ветка на remote; цена
ложного deny — pr-shepherd возвращает `verdict: blocked` и человек удаляет
ветку руками.

**Инвариант исполнения:** тело evaluator'а целиком в `try/except Exception:
return True`. Вылетевшее исключение ушло бы во внешний fail-open `main()` и
дало бы **allow** (Context п.6). Это обязательно, не «по вкусу».

**1. Разбор цели удаления** — по токенам, не одним regex (Context п.5):

- `call["command"]` (нормализован, пробелы схлопнуты) делится на сегменты по
  `&&`, `||`, `;`, `|`. В каждом сегменте `tokens = seg.split()`; ищется
  позиция `i`, где `tokens[i] == "git"` и `tokens[i+1] == "push"`; сегменты
  без `git push` пропускаются. `args = tokens[i+2:]`.
- Каждый токен сначала освобождается от **одной** пары одинаковых обрамляющих
  кавычек (`"x"`/`'x'`) — нормализатор кавычки не снимает.
- Флаги (токен начинается с `-`): `--delete` и `-d` включают режим удаления для
  этого сегмента; прочие флаги пропускаются (их значения, если флаг
  принимает отдельный аргумент — `-o ci.skip`, — будут восприняты как
  позиционные; это даёт лишнее «имя» → неоднозначность → deny, т.е.
  безопасно).
- Позиционные: первый — remote, остальные — refspec'и.
  - режим удаления: каждый refspec — цель; refspec с `:` внутри →
    неразбираемо;
  - без режима: refspec вида `:<dst>` — цель `<dst>`; refspec `src:dst` или
    без `:` — обычный push, не цель.
- Нормализация цели: срезать ведущий `refs/heads/`; после этого цель,
  начинающаяся с `refs/` (`refs/tags/…`, `refs/pull/…`), — неразбираемо.
- Валидность имени: `^[A-Za-z0-9._][A-Za-z0-9._/-]*$` (в частности, не
  начинается с `-` — защита от инъекции опции в argv `gh`), без `..`.

Итог разбора — множество целей по всей команде. **Ровно одна валидная цель**
→ шаг 2. Ноль, больше одной, или любой токен «неразбираемо» → `return True`
без вызова `gh`. Покрытые формы: `git push origin --delete X`, `git push
--delete origin X`, `git push origin -d X`, `git push origin :X`, `git push
origin :refs/heads/X` (три формы спеки + алиас `-d` и флаг до remote —
семантически те же). `git push origin --delete a b` → две цели → deny
(множественное удаление pr-shepherd'у не нужно; N сетевых вызовов по 10 с —
тоже).

**2. Проверка PR:** `rc, out = _run(["gh", "pr", "list", "--head", name,
"--state", "merged", "--json", "number"], wd, 10)`, `wd` из E1 (при
`cd <repo> && git push …` gh спросит нужный репозиторий).

- `rc == 0` и `json.loads(out)` — `list` с `len > 0` → `return False` (allow).
- Всё остальное → `return True`.

`gh` резолвит репозиторий по default remote каталога `wd`; при удалении ветки
на другом remote (`git push upstream --delete x` в форке) проверка идёт по
default-репо — принятая неточность, в стороне безопасности не хуже deny.

### E7. Регистрация

`CONTEXTS` остаётся объявленным в начале модуля (как в #23). Evaluator'ы — в
новой секции `# Context evaluators (ADR-0005)` между rule engine и журналом;
в её конце `CONTEXTS.update({"head_on_remote": …, "linked_worktree": …,
"write_outside_repo": …, "branch_pr_merged": …})`. Имена `_head_on_remote` и
т.п. — приватные. Движок (`_check_rule`, `evaluate_rules`) не меняется.

### E8. `guard.yaml`: D5, точная точка правки

В `profiles/base/guard.yaml`, блок `- id: remote_ref_delete` (сейчас строки
52–56):

```text
  - id: remote_ref_delete
-   tools: [Bash]      # БЕЗ context, БЕЗ roles — ADR D5: безусловно для всех ролей в #23
+   tools: [Bash]
+   not_roles: [pr-shepherd]   # ADR-0004 D5 / ADR-0005: для pr-shepherd решает remote_ref_delete_unmerged
    match:
```

Литерал `[pr-shepherd]`, не `$merge_roles`: исключение про удаление веток, а
не про merge. Зеркально — в `build_guard_config()` (`test_guard.py:115`)
`"not_roles": ["pr-shepherd"]`. Инвариант для теста (рекомендуется добавить в
дрейф-проверки `test_guard_context.py`): `remote_ref_delete.not_roles ==
remote_ref_delete_unmerged.roles` — иначе либо дыра (роль вне обоих), либо
двойная проверка. Комментарий секции `# --- §5.2 … (данные; evaluator'ы — #24)`
обновить на «активно с #24». Больше в `guard.yaml`/`guard.json` ничего не
меняется.

Существующий `test_pr_shepherd_remote_ref_delete_deny` (`test_guard.py:353`)
ожидает теперь `rule_id == "remote_ref_delete_unmerged"`; чтобы результат не
зависел от установленного и залогиненного `gh` на машине разработчика,
e2e-вызов должен идти из `cwd`, который не git-репо (тогда `gh` падает →
deny), либо с `PATH` без `gh`.

### E9. Отклонения от plan-1 (implementer/tester следуют ADR)

| План | ADR | Почему |
|---|---|---|
| шаг 2: `head_on_remote` — сначала `@{u}`, любая ошибка → `context_error` | E3: сначала `branch -r --contains HEAD`; ненулевой `@{u}` — тихий False | «нет upstream» штатно (AC1), не ошибка; минус один subprocess в частом случае |
| шаг 2: две команды для `linked_worktree` | E4: одна `rev-parse --git-dir --git-common-dir`, пути `join(wd, …)` | латентность; относительные пути |
| шаг 5: префиксы не `realpath`'ить | E5 шаг 1.3: `realpath` литеральной головы | `$TMPDIR` на macOS иначе не матчит никогда |
| шаг 5: allowance через `working_dir()` | E5 шаг 3: cwd = ближайший существующий предок файла | у Edit/Write нет команды |
| шаг 5/6: `context_error` на любой сбой git в allowance, тест на `/etc/x` | E5 шаг 3.4: только при `rc is None`; `/etc/x` → deny **без** `context_error` | «не репо» — ожидаемый ответ; тест `context_error` для allowance — через monkeypatch `_run` → `(None, "TimeoutExpired")` |
| шаг 3: три regex-паттерна | E6: токенный разбор, `-d`, флаг до remote, отказ на `refs/tags/`, ровно одна цель | Context п.5 |

## Consequences

- Движок меняется в одном месте (`pre_tool()`: накопитель `context_errors` +
  `finally`-сброс в журнал). Сигнатура `CONTEXTS` и форма `guard.json`
  неизменны — #25 регистрирует `merge_preflight`/`pr_create_gate` тем же
  путём и может использовать `_run` (timeout 10 для `gh pr view`) и
  `_note_context_error`.
- `guard-events.jsonl` получает новый `event: "context_error"` с
  `decision: null` и ключом `detail`. Reader #29 и doctor (#28) его видят;
  P7 его не считает. Схема D9 расширяется только значением `event`.
- `context_error` означает «контекст не смог ответить» (нет git, таймаут, не
  репо там, где репо обязан быть), а не «ответ был нет». Это делает событие
  пригодным для diagnostics без фильтрации шума.
- Латентность: git-вызовы только при совпадении `match` (rebase/amend/stash)
  или промахе всех префиксов (write). Худший случай Bash — `rebase` + `amend`
  в одной составной команде: 4 вызова × 3 с. Мемоизации между правилами нет —
  не окупается.
- `branch_pr_merged` — сетевой вызов (≤10 с) только для pr-shepherd и только
  на удалении remote-ветки. Любое отклонение команды от канонических форм →
  deny; pr-shepherd в ответ возвращает `verdict: blocked`, не обходит.
- Тестируемость: все внешние вызовы проходят через `_run`, подменяемый
  monkeypatch'ем; e2e (§5.2, AC6) — на реальных `git init` + bare remote.

## Alternatives considered

- **Evaluator сам пишет `context_error` через `_safe_write_event(…,
  call["root"])`** — у него нет `session_id`/`input_hash`/`target` (D9
  требует все ключи); пришлось бы дублировать построение события или
  протаскивать поля в `call`. Отвергнуто в пользу накопителя.
- **Расширить возврат evaluator'а (кортеж `(result, error)`)** — ломает D4 и
  контракт #25. Отвергнуто.
- **Писать `context_error` внутри `_run`** — helper не знает, штатный ли
  ненулевой exit для конкретного вопроса (E3 шаг 2 vs E4). Отвергнуто.
- **Отличать «нет upstream» от «не репо» разбором stderr** (`LC_ALL=C`,
  `fatal: not a git repository`) — хрупко к версиям git; перестановка вызовов
  (E3) даёт то же без парсинга текста. Отвергнуто.
- **`--path-format=absolute` у `rev-parse`** вместо `join(wd, …)` — требует
  git ≥ 2.31; `join` работает везде. Отвергнуто.
- **Срез glob по первой `*` + `startswith`** — дыра на
  `~/.claude/projects/*/memory/` (план, шаг 5). Отвергнуто.
- **`fnmatch` по всей строке пути** (`fnmatch(real, prefix + "*")`) — `*` в
  `fnmatch` матчит `/`, та же дыра. Отвергнуто.
- **Для несуществующего каталога — сразу deny без git** — ломает AC5 для
  нового файла в новом подкаталоге linked worktree (частый случай у
  implementer'а). Отвергнуто в пользу подъёма к предку.
- **`branch_pr_merged` fail-open, как остальные** — спека §5.2 явно требует
  fail-closed; удалённая несмерженная ветка необратима. Отвергнуто.
- **Проверять каждую из нескольких удаляемых веток** — N × 10 с сети и
  частичные решения; сценарий не нужен pr-shepherd'у. Отвергнуто: >1 цели →
  deny.
