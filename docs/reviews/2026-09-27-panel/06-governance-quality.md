# 06 — Governance, безопасность, аудируемость и инженерное качество

Эксперт панели: риски агентной разработки. Объект: zprof main @ ed75196, 2026-09-27. Всё проверялось только чтением. Содержимое `.agentlog/transcripts` я намеренно не анализировал: выводы о секретах сделаны по коду коллектора.

## Вердикт

- **Контроль держится на прозе, технических гарантий почти нет.** Branch protection на `main` не настроена (`gh api repos/vaporphd/zprof/branches/main/protection` → `404 Branch not protected`), CODEOWNERS нет, `zprof apply` пишет только хуки и никаких `permissions` (`cli/internal/apply/settings.go:31-35`).
- **Автоматический merge без обязательного review уже работает в проде.** PR #10 смержен от имени `vaporphd` при 0 reviews (`gh pr view 10` → `{"m":"vaporphd","r":0,"c":["SUCCESS"]}`). Этот же PR менял контракт pr-shepherd, по которому его и смержили.
- **Редакция секретов покрывает индексы, но не сырые данные.** Транскрипты сабагентов и tool-results копируются в `.agentlog/` без редакции (`profiles/base/zprof-collect.py:875-879`, `:1120-1131`).
- **Аудиторский след частичный.** Данные есть (`dispatches.jsonl`, `tool-events.jsonl`, `schema_version`), но run log хранится прозой ≤120 символов на шаг, лежит в gitignored `.zprof/runs/`, а локально этот каталог пуст. Коммиты не подписаны (`git log --format=%G?` → `N`).
- **Инженерная база крепкая, но инструмент сам не пользуется собственным последним релизом.** Go-пакеты разделены чисто, CI есть, тестов ~180. При этом gofmt drift в 20 файлах, Python-линтера нет, а развёрнутый в репо коллектор отстаёт от исходника (1357 против 1691 строк).

## Модель угроз

| Угроза | Вектор | Чем закрыто | Остаток | Дешёвая мера |
|---|---|---|---|---|
| Prompt injection | тело issue/PR через `gh issue view`, файлы репо, возвраты сабагентов | проза: «external PRs → blocked-external» (`pr-shepherd.md:28`); MEA-аудиторы проверяют среду, а не отчёт (`auditor.md`) | выс | помечать внешний текст как данные в контрактах groomer/planner; brief task-runner'а собирать только из issue владельца (фильтр `author`) |
| Исполнение произвольных команд | Bash у implementer/tester/task-runner без allowlist | ничем; в `settings.local.json` allow только playwright MCP, `deny` нет | выс | `zprof apply` пишет `permissions.deny` (`git push --force*`, `gh pr merge --admin*`, `rm -rf /*`, `curl * \| sh`) плюс PreToolUse-хук, который проверяет стоп-лист |
| Захват хука (code exec) | `.claude/zprof-collect.py` лежит в git (`git ls-files .claude`), хук запускает его на каждом SessionStart/Stop | `test -x … \|\| true` защищает только от падения, не от подмены | сред | хук вызывает версию из `~/.zprof/` или бинарь, а не файл из рабочей копии; CODEOWNERS на `.claude/**` |
| PATH-hijack | `command -v zprof && … zprof score` (`settings.go:24`) | — | низк | писать абсолютный путь к бинарю при apply |
| Утечка секретов на диск | сырые транскрипты `.gz`, tool-results, права 0644 | `.agentlog/` в `.gitignore`; редакция `dispatches`/`tool-events` (`zprof-collect.py:1549`, `:867`) | сред | прогонять `_redact_secrets` по строкам перед `_gzip_copy`; `chmod 600`; TTL |
| Неполные regex редакции | `telemetry.yaml:57-65` | 8 паттернов | сред | добавить `github_pat_`, `gh[ousr]_`, `sk-ant-[\w-]+`, `AKIA[0-9A-Z]{16}`, `xox[bap]-`; тест на каждый |
| Самомодификация governance | агенты правят `profiles/base/agents/**`, `CLAUDE.md`, `lessons.md` и сами мержат | ничем техническим | выс | CODEOWNERS + required review на эти пути |
| Supply chain агентов | `agent-installer` делает `curl` сырого `.md` с `main` чужого репо (`~/.claude/agents/agent-installer.md:24,73`), без pin и хеша | — | сред | pin на commit SHA, показывать diff до записи, lock-файл с sha256 |
| Supply chain бинаря | `make install` вручную, `version = "0.1.0-dev"` (`cli/cmd/zprof/main.go:12`) | — | низк (пока один пользователь) | goreleaser + checksums + SLSA provenance при первом внешнем пользователе |

## Сильные стороны

- **Граница «мутация — раннеру, чтение — сам»** (`CLAUDE.md`, «Граница»). Она сужает поверхность атаки: писать может один исполнитель, его контракт один, его журнал один.
- **Аудиторы проверяют среду, а не отчёт исполнителя** (`.claude/agents/auditor.md` в описании: «не доверяя отчёту исполнителя»). Это прямая защита от сфабрикованного «done». В pr-shepherd есть §2.1 «Fabrication cross-check» (`pr-shepherd.md:64`).
- **Жёсткие запреты для merge-агента:** `--admin`, force, `--no-verify`, внешние PR (`pr-shepherd.md:28,32,99,169-170`). MERGE_GATE выбирается по данным: читается protection API (`pr-shepherd.md:98`).
- **Хуки не ломают сессию и не попадают в чужие окружения.** Хуки пишутся в `settings.local.json`, а не в коммитимый `settings.json` (`settings.go:39-44`), upsert идемпотентный, stale-запись обновляется на месте (`settings.go:77-86`). Запись атомарная (`fsutil.WriteFileAtomic`).
- **Телеметрия версионирована и структурирована:** `schema_version` в строках (`zprof-collect.py:864`, `internal/score/reader.go:17`), `dispatch_id`/`parent_dispatch_id`/`spawn_depth`. Это основа для provenance.
- **Стоп-лист один на всех.** Одинаковый текст в `manifest.yaml:10-17` и в `CLAUDE.md`, task-runner обязан читать его на старте (`task-runner.md:49,262-271`).
- **CI с `-race`, `go vet`, build** (`.github/workflows/ci.yml`). PR #10 прошёл его (`statusCheckRollup: SUCCESS`).

## Риски и слабости

1. **Merge без человека и без технического гейта.** Правило в `tasks/lessons.md:146-148`: «pr-shepherd … сам выполняет `gh pr merge --squash --delete-branch`». Protection отсутствует, поэтому MERGE_GATE = `local-green` (`pr-shepherd.md:98`): merge выполняется сразу, даже CI не обязателен. Последствие: одна успешная prompt injection или галлюцинация reviewer'а доходит до `main` без единой человеческой проверки. Отказ агента на PR #10 (он вернул `blocked`, `lessons.md:137`) говорит о хорошей калибровке, но это не контроль: правило тут же переписали в прозе, и следующий запуск его уже не остановит.
2. **Governance может менять сам управляемый.** PR #10 изменил контракт pr-shepherd и был смержен по новым правилам (0 reviews). Механизма, который отделял бы изменение правил от их применения, нет. Моё мнение: без него любой «стоп-лист прозой» агенты могут в итоге переписать сами.
3. **Стоп-лист противоречит контракту merge.** `manifest.yaml:12` запрещает «удаление веток и тегов», а `pr-shepherd.md:30` предписывает `--delete-branch`. Противоречия в прозе агенты разрешают сами, и каждый раз по-своему.
4. **Нет allowlist/deny для Bash.** `settings.go` не пишет `permissions`. Стоп-лист соблюдается только добровольно, со стороны модели.
5. **Хук исполняет файл из рабочей копии.** `.claude/zprof-collect.py` под git, а хук вызывает его на SessionStart. Если ветка правит этот файл, код выполнится на машине владельца при следующем открытии сессии, ещё до всякого review.
6. **Сырые данные без редакции.** `_gzip_copy` (`zprof-collect.py:875-879`) и копия tool-results (`:1120-1131`) пишут байты как есть, в `.agentlog/transcripts` уже 35 файлов на 6.6M. `.gitignore` защищает от коммита, но не от бэкапов, синка папки и `zprof eval`, который может отправить транскрипт в LLM.
7. **Аудиторский след рвётся.** `.zprof/runs/` в `.gitignore`, и локально run log'ов нет вообще (`ls .zprof/runs/*.md` → no matches). Шаги журнала — проза ≤120 символов (`task-runner.md:303`). Коммиты не подписаны, автор всегда `Alex Vapor`, даже когда действовал агент. По git нельзя отличить решение человека от решения агента.
8. **Качество инструмента.** `gofmt -l` показывает 20 файлов (в брифе было 13, drift растёт). Python не линтуется и не гоняется в CI: в `ci.yml` только Go, 180 pytest-тестов в CI не запускаются. Развёрнутый `.claude/zprof-collect.py` (1357 строк, 2026-08-09) отстаёт от `profiles/base/zprof-collect.py` (1691 строка), а в `settings.local.json` нет score-цепочки. Выходит, zprof не проверяет на себе собственный текущий релиз. Разбор JSONL сессий есть и в `internal/eval/parser.go`, и в коллекторе. Это два источника истины для одного формата, и `schema_version` сверяется только на чтении в `score`. Совместимость профиль↔бинарь↔проект задаётся одним `requires_base: ">= 0.0.0"` (`manifest.yaml:4`), то есть фактически не проверяется.
9. **Коллектор одним файлом на 1691 строку.** Деплой одним файлом — разумный аргумент при stdlib-only. Но `except Exception` вместе с `exit(0) always` (`zprof-collect.py:28,42,61,121,172,991,1056`) скрывает регрессии, а сигнала «коллектор молча сломан» нет. Отдельная деградация есть только в `_log_error`.

## Сравнение с практикой

- **OWASP LLM Top 10 (2025):** LLM01 Prompt Injection и LLM06 Excessive Agency здесь самые острые. Рекомендации OWASP — least privilege и human-in-the-loop на высокорисковых действиях. Merge в `main` как раз такое действие.
- **OWASP Agentic AI Threats (T2 Tool Misuse, T3 Privilege Compromise, T6 Intent Breaking):** правило «ограничивать инструменты технически, а не инструкцией» здесь не выполнено: в `permissions` нет ни одного deny.
- **Anthropic, Claude Code permissions/hooks:** рекомендуется `permissions.deny` для опасных команд и PreToolUse-хуки как enforcement layer. У zprof хуки уже ставятся автоматически, так что добавить PreToolUse-гейт стоит дёшево.
- **GitHub rulesets:** required status checks, required review, CODEOWNERS на пути, restrict bypass. Для соло-владельца достаточно «required checks + CODEOWNERS review только для `profiles/base/agents/**`, `.claude/**`, `CLAUDE.md`».
- **SLSA v1.0 Build L1-L2:** provenance для бинаря через goreleaser + `actions/attest-build-provenance`. Нужно до первого внешнего пользователя.
- **EU AI Act / SOC2 (мнение, не юр. консультация):** zprof вряд ли станет high-risk системой. Но покупателю по SOC2 CC8.1 (change management) понадобятся одобрение изменений, отделение автора от approver'а и неизменяемые логи. Сейчас не выполнено ни одно из трёх.
- **Agent governance (NIST AI RMF, «Govern/Map/Measure/Manage»):** Measure здесь сильнее среднего (scorecard, телеметрия). Слабее всего Govern: кто утверждает изменение политики.

## Рекомендации

**Сейчас (≤1 день)**
- Ruleset на `main`: required check `CI/test` без bypass для агентов → pr-shepherd переходит на `CI-green`, и merge без зелёного CI становится невозможным.
- `.github/CODEOWNERS` + required review на `profiles/base/agents/**`, `.claude/**`, `CLAUDE.md`, `manifest.yaml` → агенты больше не меняют свои правила без человека.
- Снять противоречие `--delete-branch` ↔ «удаление веток» в `manifest.yaml` → стоп-лист снова однозначен.
- `zprof apply` пишет `permissions.deny` для force/`--admin`/`--no-verify` → стоп-лист частично становится техническим.
- В CI добавить `pytest profiles/base/tests`, `gofmt -l` (fail), `ruff` → регрессии коллектора видны в PR.

**Квартал**
- Редакция перед `_gzip_copy` и tool-results, `chmod 600`, TTL для `.agentlog/` → на диске не остаются секреты в открытом виде.
- Хук вызывает коллектор из `~/.zprof/` (версия из бинаря), а не из рабочей копии → ветка не может подменить исполняемый код хука.
- Trailer `Agent-Run: <run_id>` и `Co-authored-by` для агентских коммитов, run log в JSONL рядом с `dispatches.jsonl` → связка «коммит → решение → доказательство».
- PreToolUse-хук со стоп-листом в виде данных (`manifest.yaml`) → единый источник, enforcement вне модели.
- Совместимость версий: `requires_zprof`, проверка `schema_version` в `doctor` → раннее обнаружение drift.

**Позже (перед продажей)**
- goreleaser + checksums + SLSA provenance + подписанные теги → поставка бинаря, которую можно проверить.
- `agent-installer` с pin на SHA и lock-файлом → воспроизводимая и проверяемая установка агентов.
- Экспорт audit trail (PR ↔ issue ↔ run ↔ dispatches ↔ tool-events) одной командой → материал для SOC2 CC8.1.

## Вопросы владельцу

1. Какой риск ты готов принять за скорость auto-merge: хватит ли required CI, или для изменений контрактов агентов нужен review человека?
2. Будут ли у zprof внешние пользователи или команды в ближайшие 2 квартала? От этого зависят SLSA и формализация audit trail.
3. Используют ли `zprof eval` или evaluator сырые транскрипты как вход для LLM? Если да, редакция перед сохранением обязательна.
4. Где должен храниться run log: в git (история решений) или локально (приватность)? Сейчас не хранится нигде.
5. Допустимо ли, чтобы агентские коммиты шли от твоего имени, или нужен отдельный bot-аккаунт/токен с урезанными правами?
