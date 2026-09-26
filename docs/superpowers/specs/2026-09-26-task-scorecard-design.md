# Per-task scorecard: детерминированная оценка каждого run task-runner

**Статус:** approved design (брейншторм 2026-09-26), ждёт implementation plan
**Дата:** 2026-09-26
**Автор:** Alex + brainstorming session
**Предшественники:** `2026-08-02-agent-telemetry-design.md` (коллектор, схема `.agentlog/`), `2026-08-02-agent-sre-second-stage.md` (SLI «tokens per accepted result», «retry rounds» — отложены), `2026-07-28-task-runner-design.md` (журнал прогона), `docs/specs/2026-07-17-agent-eval-proposal.md` (Tier 1/2), `docs/milestones/mea-auditor/SPEC.md` (ext.audited — не реализовано)

---

## 1. Цель и рамки

**Цель.** После каждого run task-runner получать балл 0–100 и карточку: сколько сожгли, какими инструментами, сколько ошибочных вызовов и повторов, сколько раундов test→fix, и какая роль утопила балл. Балл считается детерминированно из логов, без LLM-судьи.

**Зачем (по приоритету).**
1. Видеть расточительность сразу после задачи.
2. Сравнивать роли и модели во времени (основа для `model_overrides`).
3. Обратная связь в loop: повторяющиеся находки → сигнал evaluator-telemetry / lessons.md.
4. Гейт качества — **позже**, после калибровки порогов на реальном распределении.

**Не цели (этой спеки).**
- LLM-судья на каждую задачу. Качество кода оценивает reviewer внутри loop; Tier-2 evaluator остаётся отдельным инструментом по запросу.
- Блокировка чего-либо по баллу. Фаза 3, отдельное решение.
- Оценка main-сессии и dispatch'ей без task-runner.
- Оценка в долларах. Цен в реестре моделей нет; токены — первичная валюта, `$` — фаза 2.

## 2. Решения брейншторма

| Вопрос | Решение |
|---|---|
| Единица оценки | Один run task-runner + разбивка по ролям |
| Нормализация | Штрафы за доли брака, не за объём. Абсолютные токены/вызовы/время — справочно |
| Судья | Только детерминированно |
| Не штрафовать | Повторные тесты/сборки (только повтор без правки между); чтение контекста (только перечитывание без правки); `blocked` с вопросом; время стенки |
| Гейт | Пока нет. Собираем и показываем, порог подберём через месяц |
| Где считать | Коллектор (Python) извлекает факты в `.agentlog/`, Go считает. Один источник правды |

## 3. Что есть сегодня (факты, см. карту 2026-09-26)

- Коллектор `profiles/base/zprof-collect.py` пишет `dispatches.jsonl` с токенами, `tool_uses` (только счёт), `duration_ms`, `status`, Class-A флагами. **Нет** имён инструментов, `is_error`, run_id; `verdict` находится для `return_parsed`, но значение не сохраняется (`:1203`).
- Дети task-runner (вложенные dispatch'и) идут по meta-only ветке (`:846-862`): токены есть, `tool_uses`/`duration_ms`/`ts_utc`/точный `status` — нет.
- Транскрипты subagent'ов копируются в `.agentlog/transcripts/*.jsonl.gz`; в них есть `tool_use` (name, input) и `tool_result` (`is_error`); `meta.json` даёт `agentType`, `toolUseId`, `parentAgentId`, `spawnDepth`.
- Правила ретраев task-runner (3 раунда tester→implementer, 1 re-dispatch на не-схему) — только проза; в run log человекочитаемая таблица шагов.
- `zprof stats` считает по ролям/сессиям; карточка «Runs и стоимость результата» (`render.go:1135-1140`) — заглушка, ждёт runs-данные.
- `zprof eval` — Tier-1 по сессии/роли, читает сырой JSONL напрямую; per-run балла нет.
- Коллектор во всех режимах читает payload хука со stdin; `session-start` пропускает текущую сессию и уже собранные.

## 4. Архитектура

```
Claude Code: <session>.jsonl + <session>/subagents/agent-*.{jsonl,meta.json}
        │  Stop / SessionStart hook  (или synthetic payload от `zprof score`)
        ▼
zprof-collect.py ──► .agentlog/dispatches.jsonl   (+verdict, ext.run_id, ext.run_log, полные вложенные строки)
                 ──► .agentlog/tool-events.jsonl  (новый: упорядоченные tool-вызовы, is_error)
        │
        ▼
zprof score (Go, cli/internal/score/) ──► карточка (stdout)
                                      ──► .agentlog/scores.jsonl        (1 строка на run × weights_hash)
                                      ──► .zprof/runs/<id>.md  ## Score (между маркерами, идемпотентно)
        │
        ▼  фаза 2
zprof stats ──► карточка «Runs» в report.html (замена заглушки)
```

Граница ответственности: **Python извлекает факты, Go интерпретирует.** Никакой метрики в коллекторе, никакого парсинга транскриптов в Go.

## 5. Коллектор: изменения

Все правки — под существующим правилом «любая ошибка → лог, `exit(0)`». Stdlib only.

**C1. Вложенные dispatch'и — полные строки.** Для каждого захваченного транскрипта subagent'а коллектор ищет `Agent`/`Task` `tool_use` + `toolUseResult` тем же кодом, что и для главной сессии (`:381-399`, `:535-565`), и извлекает дочерние строки со `status`, `tool_uses`, `duration_ms`, `ts_utc`, `model_resolved`. Дедуп по `dispatch_id`: полная строка вытесняет meta-only. Глубина — любая (task-runner → auditor → …).

**C2. `verdict` заполняется.** Значение первой строки `verdict:` из возврата пишется в поле `verdict` (сейчас всегда null). Дополнительно `ext.next`, `ext.artifact`, `ext.run_log` (из `run_log:` task-runner).

**C3. `tool-events.jsonl`.** Одна строка на tool-вызов из транскрипта subagent'а, кроме `Agent`/`Task` (это dispatch'и, они уже в `dispatches.jsonl`):

```json
{"schema_version": 1, "dispatch_id": "claude-code:<session>:<toolu>", "seq": 17,
 "ts": "2026-09-26T14:03:11Z", "tool": "Bash", "input_hash": "3f9a1c0b2e77",
 "target": "swift test --package-path Packages/HealthSyncCore", "is_error": true,
 "result_chars": 4120}
```
- `input_hash` — sha1 канонического JSON `input` (`sort_keys`, без пробелов), первые 12 hex.
- `target` — `input.file_path` | `input.path` | первые 60 символов `input.command` | `input.pattern`; после `redaction_patterns`.
- `is_error` — из парного `tool_result`; если пары нет (обрыв транскрипта) — `null`.
- `seq` — порядковый номер внутри dispatch'а по порядку в транскрипте.
- Пишется append-only, дедуп по `(dispatch_id, seq)` через watermark в `state.json`.

**C4. Идентичность run.** `ext.run_id` = `dispatch_id` строки с `role == task-runner`. Дети получают `ext.run_id` по цепочке `parent_dispatch_id` (через `meta.json parentAgentId → toolUseId`). Dispatch без task-runner в предках — `ext.run_id` отсутствует.

**Схема.** `profiles/base/telemetry.yaml`: `version: 2`; в `core_fields` — `verdict` больше не «never populated»; новые разделы `tool_events` (поля выше), `mutating_bash_patterns`, `verdict_exempt_roles: [auditor, auditor-deep]`, `score_defaults` (§ 6). `telemetry_test.py` проверяет новые разделы. Читатель `stats` версию не проверяет (`types.go:6` — поле есть, ветвления нет), новые поля игнорирует.

## 6. Модель балла

`score = max(0, 100 − Σ P_i)`. Каждый штраф — доля или счёт брака с насыщением; объём (токены, число вызовов, время) в баллы не входит. Веса в сумме 100, переопределяются в `.zprof.yaml`.

**Run** = dispatch task-runner D₀ + все dispatch'и, чья цепочка `parent_dispatch_id` доходит до D₀. **Шаги** — прямые дети D₀, упорядоченные по `ts_utc`. **Tool-события run** — все строки `tool-events.jsonl` для dispatch'ей run, включая сам D₀.

| # | Метрика | Определение | Штраф | Вменяется |
|---|---|---|---|---|
| P1 | Ошибки инструментов | `rate = Σ is_error=true / Σ событий` по run | `20 × min(1, rate/0.20)` | ролям пропорционально их ошибкам |
| P2 | Слепые повторы | внутри одного dispatch'а: событие e_j с тем же `(tool, input_hash)`, что раннее e_i с `is_error=true`, и без **мутирующего** события между ними. Счёт r по run | `15 × min(1, r/3)` | ролям пропорционально r |
| P3 | Перечитывание | внутри одного dispatch'а: `Read` того же `target`, что раньше, без мутирующего события между. `ratio = rereads / все Read`; считается только если Read ≥ 4 в dispatch'е | `10 × min(1, ratio/0.5)` | ролям пропорционально rereads |
| P4 | Раунды test→fix | число шагов `tester` с `verdict: failed`, за которыми следует шаг `implementer` — extra | `20 × min(1, extra/2)` | implementer |
| P5 | Блоки ревью | число шагов `reviewer` с `verdict: block` — b | `10 × min(1, b/2)` | implementer |
| P6 | Токены впустую | `share` = токены (все 4 типа) dispatch'ей со `status ∈ {failed, killed}` или `return_parsed=false` / все токены run. Роли из `verdict_exempt_roles` по `return_parsed` не считаются | `15 × min(1, share/0.30)` | ролям этих dispatch'ей пропорционально токенам |
| P7 | Нарушения контракта | v = Σ по dispatch'ам run: `has_preamble=true` + (`artifact_exists=false` при verdict ≠ blocked) + `next_is_reachable=false` + `return_parsed=false`; exempt-роли не считаются | `10 × min(1, v/4)` | ролям по числу нарушений |

**Мутирующее событие** (для P2, P3): `tool ∈ {Edit, Write, MultiEdit, NotebookEdit}` или `Bash`, чья команда матчит `mutating_bash_patterns` (`>`, `>>`, `sed -i`, `tee`, `mv`, `cp`, `rm`, `touch`, `git commit`, `git checkout`, `xcodegen`, `cargo|swift|go` с `build|test` — сборка меняет артефакты, но не код; включена, чтобы «пересобрал и перезапустил» не считалось слепым повтором). Список — в `telemetry.yaml`, расширяется без релиза Go.

**Что явно не штрафуется.** Повтор `swift test` после `Edit` — не P2. Пять `Read` разных файлов — не P3. `verdict: blocked` с `question:` — ярус `Blocked`, не `Failed`, без дополнительного штрафа. `duration_ms` — только на карточке.

**Ярусы** по `verdict` D₀ и баллу:

| verdict D₀ | балл | ярус |
|---|---|---|
| done | ≥ 85 | **Ideal** |
| done | 60–84 | **Solid** |
| done | < 60 | **Lucky** — сделано, но процесс плохой |
| blocked | любой | **Blocked** |
| failed | любой | **Failed** |

Пороги 85/60 — стартовые, `.zprof.yaml score.thresholds`. Через ~месяц пересматриваются по распределению.

**Атрибуция ролям.** Баллы каждого P_i делятся между ролями пропорционально их доле в числителе метрики (ошибки, повторы, токены, нарушения); P4 и P5 — implementer целиком. `penalty(role) = Σ_i share_i(role) × P_i`. Карточка сортирует роли по `penalty`.

**Насыщения** (`0.20, 3, 0.5, 2, 2, 0.30, 4`) и веса — в `telemetry.yaml score_defaults`; `.zprof.yaml score.weights` и `score.saturation` переопределяют. `weights_hash` = sha1 канонического JSON `{weights, saturation, thresholds}` (12 hex) пишется в каждую строку `scores.jsonl`, чтобы история оставалась сравнимой после смены весов.

**Confidence.** `full`, если у всех dispatch'ей run `transcript_captured=true` и `transcript_truncated=false`; иначе `partial` со списком ролей, чьи события отсутствуют. Метрики по отсутствующим транскриптам не выдумываются: P1–P3 считаются по имеющимся событиям, P6–P7 — по строкам `dispatches.jsonl`.

## 7. Выходы

**Карточка** (stdout; первые 3–4 строки main копирует в `followup.md`):

```
Score 72/100 · Solid · done · 2026-09-26-skeleton-packages · confidence full
412k tok (in 318k · out 41k · cache 53k) · 6 dispatch · 38 tool calls · 31 min · sonnet×5 opus×1
−15 P2 implementer: 3× `swift test --package-path …` без правок между
−8  P1 4/38 tool errors (11%)
−5  P4 tester→implementer: 1 лишний раунд

role         tokens  calls  err  penalty  model
implementer   260k     22    4    −23     sonnet
tester         74k      8    0     −5     sonnet
reviewer       47k      3    0      0     opus
planner        31k      5    0      0     sonnet
tools: Bash 19 · Read 9 · Edit 6 · Grep 3 · Write 1
```
Находки — топ-3 по баллам, шаблон текста на каждый P_i (RU). `--json` печатает строку `scores.jsonl`.

**`scores.jsonl`** (append-only; читатели берут последнюю строку на `(run_id, weights_hash)`):

```json
{"score_schema": 1, "zprof_version": "0.1.0-dev",
 "run_id": "claude-code:<session>:<toolu>", "run_log": ".zprof/runs/2026-09-26-skeleton-packages.md",
 "session_id": "...", "project_id": "...", "ts_utc": "...",
 "verdict": "done", "tier": "Solid", "score": 72,
 "penalties": {"P1": {"value": 0.105, "points": 8, "by_role": {"implementer": 8}}, "P2": {...}, ...},
 "facts": {"tokens": {"input": 318000, "output": 41000, "cache_read": 53000, "cache_creation": 0},
           "dispatches": 6, "tool_calls": 38, "tools_top": [["Bash", 19], ["Read", 9], ...],
           "duration_ms": 1860000, "route": ["planner", "implementer", "tester", "implementer", "tester", "reviewer"],
           "models": {"implementer": "sonnet", "reviewer": "opus"}},
 "inputs": {"dispatches": 6, "tool_events": 38, "transcripts_missing": [], "confidence": "full"},
 "weights_hash": "a1b2c3d4e5f6"}
```

**Run log.** В `.zprof/runs/<id>.md` после `## Итог` секция между `<!-- zprof:score:begin -->` / `<!-- zprof:score:end -->` с карточкой; повторный запуск заменяет содержимое между маркерами. Путь берётся из `ext.run_log`; если файла нет — секция не пишется, на карточке пометка.

**followup.md.** Правило в `AGENT_LOOP.md` (base `agent-loop-router.md`, пункт «после каждого dispatch — ≤3 строки»): после возврата task-runner main выполняет `zprof score` и вписывает первые 3 строки карточки вместо строк предыдущего run. Лимит ≤20 строк followup сохраняется.

**`zprof stats`** (фаза 2): карточка «Runs» из `scores.jsonl` — последние N run'ов с ярусом, линия балла, медиана `penalty` по ролям, распределение ярусов, топ повторяющихся находок (роль × P_i за окно) как сигнал для `zprof eval-telemetry`.

## 8. Команды, триггеры, конфиг

**`zprof score [--latest | --run <id|run_log|slug> | --all-missing] [--json] [--no-collect] [--quiet]`**

1. Находит сессию для cwd через `eval/locate.go` (самый свежий JSONL slug-каталога).
2. Если не `--no-collect`: собирает synthetic payload `{"session_id", "transcript_path", "cwd"}` и подаёт на stdin `.claude/zprof-collect.py stop`. Инкрементальный watermark коллектора делает повторные вызовы дешёвыми; running-set пустой — незавершённые async dispatch'и придут на следующем проходе.
3. Читает `.agentlog/dispatches.jsonl` и `tool-events.jsonl`, группирует по `ext.run_id`.
4. `--latest`: run с максимальным `ts_utc` среди `role=task-runner, dispatch_complete=true`. `--all-missing`: все завершённые run без строки под текущий `weights_hash`.
5. Считает, пишет `scores.jsonl`, секцию в run log, печатает карточку (или ничего при `--quiet`).

**Stop hook.** `apply/settings.go`: для события `Stop` вторая команда после коллектора:
`command -v zprof >/dev/null 2>&1 && zprof score --latest --quiet --no-collect || true`.
`EnsureHooks` остаётся идемпотентным (upsert по команде). Хук пишет только в `.agentlog/` и run log, stdout не нужен.

**`.zprof.yaml`:**
```yaml
score:
  enabled: true              # false — хук и main ничего не считают
  weights: {P1: 20, P2: 15, P3: 10, P4: 20, P5: 10, P6: 15, P7: 10}
  saturation: {P1: 0.20, P2: 3, P3: 0.5, P4: 2, P5: 2, P6: 0.30, P7: 4}
  thresholds: {ideal: 85, solid: 60}
```
`manifest/project.go`: `Score *ScoreConfig` рядом с `Audit`. Отсутствие раздела = defaults из `telemetry.yaml`, `enabled: true`.

**Пакеты Go.** `cli/internal/score/`: `reader.go` (dispatches + tool-events → runs), `metrics.go` (P1–P7, атрибуция), `scoring.go` (веса, ярус, weights_hash), `render.go` (карточка, JSON), `persist.go` (scores.jsonl, run log маркеры). `cli/internal/cmd/score.go`. Парсинг сырого JSONL в `score` **не** появляется — только `.agentlog/`.

## 9. Ошибки и граничные случаи

- **Нет run в сессии** (main диспатчил напрямую) → `zprof score` печатает `unscored: no task-runner dispatch in session`, код 0.
- **Run не завершён** (`dispatch_complete=false`) → пропуск, сообщение; хук молчит.
- **Транскрипт обрезан/не захвачен** → `confidence: partial`, роли перечислены; штрафы по отсутствующим событиям не начисляются.
- **Нет `ext.run_log` или файла** → `scores.jsonl` пишется, секция в run log — нет, на карточке `run log: missing`.
- **Повторный запуск** с теми же весами — та же строка перезаписывается по смыслу (append, читатели берут последнюю); с другими — новая строка, секция в run log перезаписывается.
- **Auditor'ы** возвращают `completion:` — по контракту; `verdict_exempt_roles` исключает их из P6-по-`return_parsed` и P7.
- **`is_error` у Bash** = ненулевой exit. Намеренные проверки (`test -f`, `grep -q`) дадут ложные P1. Принято: насыщение 20% терпимо; фаза 2 — allowlist паттернов в `telemetry.yaml`.
- **Bash-чтение** (`cat`, `sed -n`) не попадает в P3 — слепое пятно, честно указано; расширение `target` на эти команды — фаза 2.
- **RTK-хук** переписывает команды (`git status` → `rtk git status`) — `input_hash` считается по итоговой команде, внутри сессии стабилен.
- Коллектор при любой ошибке в C1–C4 логирует в `collect.log` и завершает `exit(0)`; частично записанные `tool-events` допустимы (дедуп по `(dispatch_id, seq)`).

## 10. Тестирование

- **Python** (`profiles/base/tests/`): фикстура «главная сессия + `subagents/` с task-runner, у которого 4 ребёнка, один — auditor глубины 2, один транскрипт обрезан». Проверки: C1 полные строки детей и вытеснение meta-only; C2 `verdict`/`ext.run_log`; C3 события с `input_hash`, `target`, `is_error=null` при обрыве, дедуп при повторном прогоне; C4 `ext.run_id` по цепочке; редакция секретов в `target`; `exit(0)` на битом транскрипте.
- **Go** (`cli/internal/score/`): table-driven на каждый P1–P7 с синтетическими `dispatches.jsonl` + `tool-events.jsonl`, включая: повтор после `Edit` не считается P2; сборка между повторами не считается P2; Read < 4 → P3 не считается; blocked → ярус Blocked; exempt-роли вне P6/P7; атрибуция сумм равна P_i. Golden `scores.jsonl` на фикстуру. Идемпотентность маркеров в run log. `weights_hash` меняется при смене любого веса.
- **Shakedown**: фикстурный проект прогоняет task-runner, `zprof score` выдаёт карточку с `confidence full`.
- **Ручная проверка на живом проекте**: jarvis-in-hermes (есть `.agentlog` с историей) — `zprof score --all-missing` должен оценить прошлые run'ы; смотрим распределение баллов глазами до выбора порогов.

## 11. Фазы

1. **Фаза 1 — считать и показывать.** C1–C4 + `telemetry.yaml v2`; пакет `score`; `zprof score`; Stop-хук; правило в `AGENT_LOOP.md`; `.zprof.yaml score`; тесты; прогон на jarvis-in-hermes.
2. **Фаза 2 — тренды.** Карточка «Runs» в `zprof stats`; сигнал повторяющихся находок в `eval-telemetry`; цены в реестре моделей → `$` на карточке; allowlist ложных `is_error`; `cat`/`sed -n` в P3.
3. **Фаза 3 — гейт.** После калибровки порогов: `pr-shepherd` pre-flight читает последнюю строку `scores.jsonl` для PR; `Lucky` → `blocked` с карточкой. Отдельное решение Alex.

## 12. Риски и открытые вопросы

- **Goodhart.** Штрафуем только брак, но агенты могут уйти от `Read` в `cat` (слепое пятно P3) или дробить команды, меняя `input_hash`. Мониторим по трендам; веса и паттерны меняются без релиза.
- **Шум одного run.** Разброс токенов на одной задаче до 30× (исследование 2026-09-26). Балл одного run — сигнал для взгляда, не для вывода; тренды — от ≥10 run'ов на роль.
- **Мутирующий Bash** — эвристика. Ложный P2 возможен при правке нераспознанной командой. Список паттернов расширяем по находкам.
- **Ретраи стоят мультипликативно** (контекст пересылается) — P6 ловит только dispatch'и без результата; внутри одного dispatch'а ретраи видны через P1/P2, не через токены. Осознанно.
- **Дублирование парсинга** сессий остаётся между Python-коллектором и `zprof eval`. Эта спека его не увеличивает: `score` читает только `.agentlog/`.

## 13. Источники исследования

- Anthropic, Demystifying evals for AI agents (2026-01) — детерминированные grader'ы первыми.
- Cost-of-Pass (arXiv 2504.13359), SWE-Bench+ (2410.06992) — `$ / resolved`.
- HAL (arXiv 2510.11977) — Pareto accuracy × cost, отказ от одного числа.
- AgentLens (arXiv 2605.12925) — 0–100, blind-retry penalty `1 − r/|T|`, ярусы Ideal/Solid/Lucky, пять классов waste.
- OpenHands StuckDetector — 4 одинаковых action→observation, 3 action→error.
- ToolMisuseBench SCORING — InvalidCallRate, BudgetExceeded.
- «Tokens after first failure» (dev.to, CI-порог 0.30) — прототип P6.
- Claude Code OTel (`tool_result.success`, `query_source=subagent`) — подтверждение доступности тех же полей в транскриптах.
- Langfuse Score data model — версионированная схема балла (`weights_hash`).
