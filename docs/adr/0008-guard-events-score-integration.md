# ADR 0008: guard-события в `zprof score` (P7) и `zprof stats`

**Date:** 2026-09-28
**Status:** accepted
**Issue:** #27
**Parent:** ADR-0004 (D9 журнал `guard-events.jsonl`), ADR-0006 (F1 форма событий), ADR-0007 (G1 `subagent-stop`/`block`, `dispatch_id` из `agent_transcript_path`)
**Plan:** `tasks/plan-issue-27.md` (этот ADR — шаг 1; в трёх местах перекрывает план по фактам кода, см. H8)
**Spec:** `docs/superpowers/specs/2026-09-27-guard-hooks-design.md` §7 «Телеметрия и score», §9

## Context

`zprof-guard.py` (#23–#26) пишет `.agentlog/guard-events.jsonl`, но Go-сторона
его не читает: P7 (`computeP7`, `cli/internal/score/metrics.go`) считает только
4 контрактных нарушения из `dispatches.jsonl`, `zprof stats` guard не видит.
Спека §7 задаёт цель, но для детерминированной реализации её мало. Кроме пяти
развилок плана, чтение кода вскрыло два факта, которые план не учёл:

1. **Разные формы `dispatch_id`.** `dispatches.jsonl` и `tool-events.jsonl`
   хранят составной id `claude-code:<session_id>:<toolUseId>`
   (`_make_composite_id`, `zprof-collect.py:1733–1741`; видно в живом
   `.agentlog/`). Guard пишет **сырой** `toolUseId` (`dispatch_id()`,
   `zprof-guard.py:96–102`; §7: «`toolUseId` из `agent-<id>.meta.json`»).
   Прямое сравнение строк, как предлагает план («по индексу `runOf`»), на
   реальных данных не совпадёт ни разу; в golden-фикстуре совпало бы только
   потому, что тестер записал бы составной id — тест зелёный, прод мёртвый.
2. **`Root.Timestamp` — время завершения, не старта.** `ts_utc` dispatch'а —
   `record["timestamp"]` строки `tool_result` / нотификации завершения
   (`zprof-collect.py:574`, `:684`); у завершённого асинхронного runner'а
   `BuildRuns` берёт последнюю строку (больший `seq`). План («run с
   наибольшим `Root.Timestamp ≤ ts`») относил бы main-события, случившиеся
   **во время** run N, к run N−1 (или отбрасывал бы их для первого run).
   Golden-фикстура `run1` синтетическая (дети `10:01…` позже корня `10:00`),
   и этот дефект на ней не виден.

Плюс одно следствие арифметики плана (п.6 «Предварительной проверки»): после
guard-слагаемого P7 = 10 очков, наравне с P2/P4; `sort.SliceStable` в
`RenderCard` оставляет порядок P1, P2, P4, P7 → P7 **четвёртый и в карточку не
попадает**. Вариант «только суффикс в `Detail`» не выполняет AC4 на той самой
фикстуре, которую требует AC5.

## Decision

### H1. `GuardEvent` и `ReadGuardEvents` (`internal/score/reader.go`)

```go
// GuardEvent is one row of .agentlog/guard-events.jsonl (guard spec §7).
type GuardEvent struct {
	Ts         string `json:"ts"`
	SessionID  string `json:"session_id"`
	Event      string `json:"event"`
	Role       string `json:"role"`
	DispatchID string `json:"dispatch_id"` // raw toolUseId; "" for main/unknown (JSON null)
	Tool       string `json:"tool"`
	Rule       string `json:"rule"`
	Decision   string `json:"decision"`
	Target     string `json:"target"`
	InputHash  string `json:"input_hash"`
}

func ReadGuardEvents(path string) ([]GuardEvent, error)
```

- Ровно 10 ключей §7, все `string`: JSON `null` → `""` без ошибки. `run_id`
  (всегда `null`) и `detail` (диагностика `error`/`block`) в структуру **не**
  переносятся — ни P7, ни `stats` их не читают.
- Отсутствующий файл → `(nil, nil)`; прочая ошибка open/scan → wrap
  `fmt.Errorf("open %s: %w", ...)`/`"scan %s: %w"` — зеркало `ReadToolEvents`,
  включая `sc.Buffer(…, 4 MiB)`.
- **Инвариант: строка пропускается только при ошибке `json.Unmarshal`.**
  Условия `ev.DispatchID == ""` → `continue` (как в `ReadToolEvents`) быть не
  должно: пустой `dispatch_id` — легитимный main и единственный вход в H2-fallback.
  Регрессия ловится отдельным тестом (H7).
- Без дедупа: у строк нет `seq`, коллектор файл не переписывает (§7). Порядок —
  порядок файла.
- Reader не фильтрует по `decision`/`event` — это делает потребитель (H3, H6).

### H2. Привязка к `Run`: `AttachGuardEvents`, сигнатура `BuildRuns` не меняется

Рекомендация плана принята (отдельная функция, `BuildRuns(ds, evs)` и ~10 её
вызовов не трогаются), алгоритм — с двумя поправками из Context.

```go
// Run получает поле:
GuardEvents []GuardEvent // in file order; flat — GuardEvent carries its own Role

// AttachGuardEvents distributes guard events over runs built by BuildRuns.
// Returns runs (same slice, same order) with GuardEvents filled.
func AttachGuardEvents(runs []Run, events []GuardEvent) []Run
```

Вызов — `cmd/score.go`, сразу после `runs := score.BuildRuns(ds, evs)`. Внутри
`BuildRuns` не вызывается.

1. **Ключ — сырой `toolUseId`.** `rawID(s) = s[strings.LastIndex(s, ":")+1:]`
   (для строки без `:` — сама строка). Индекс `rawID(d.DispatchID) → i` по всем
   `runs[i].Dispatches` (корень в `Dispatches` уже входит — `runIDFor` вернёт
   его собственный id). Событие ищется по `rawID(e.DispatchID)` — так одинаково
   работают и реальный сырой id guard'а, и составной, если он когда-нибудь
   появится. Сессию в ключ не включаем: `toolUseId` глобально уникален, а
   равенство `session_id` subagent-хука и main-сессии коллектора не
   гарантировано ничем, кроме наблюдения.
2. **`DispatchID != ""`**: найден → `runs[i].GuardEvents = append(...)`; не
   найден (dispatch вне complete-run'а, runner ещё идёт, чужой лог) → событие
   молча отбрасывается, симметрично `ToolEvent` в `BuildRuns`. Временной
   fallback для таких событий **не** применяется (§7: «никаких временных окон»).
3. **`DispatchID == ""`** (main/unknown) — единственный временной fallback:
   - `ts` парсится `time.Parse(time.RFC3339Nano, e.Ts)` (тот же формат, что
     `stats` для `ts_utc`); ошибка → отбросить.
   - Окно run'а: `end = Root.Timestamp`, `start = end − Root.DurationMs`;
     событие попадает, если `start ≤ ts ≤ end` (обе границы включительно).
   - Run-кандидат пропускается, если `Root.Timestamp.IsZero()` или
     `Root.DurationMs <= 0` — «активен» не определён, лучше потерять событие,
     чем приписать его чужому run'у.
   - Если `e.SessionID != ""` и `Root.SessionID != ""` — требуется равенство:
     main-сессия и корневой dispatch её runner'а пишут один `session_id`;
     параллельные сессии в одном `.agentlog/` не смешиваются.
   - Несколько подходящих окон (параллельные async-runner'ы одной сессии) →
     **первый** по порядку `runs` (он уже отсортирован `lessDispatch`: время
     завершения, затем `DispatchID`) — детерминированно, событие считается
     ровно один раз.
   - Ни одного окна → отбросить.
4. Чистая функция: не пишет stderr, не возвращает ошибок, `runs` без событий
   сохраняют `GuardEvents == nil`.

### H3. `computeP7`: guard-слагаемое

- Новый **отдельный** цикл по `run.GuardEvents` после существующего цикла по
  `run.Dispatches`. Считается строка с `Decision == "deny" || Decision == "block"`;
  `allow_unverified`, `error`, `""` (`format_unfixed`, `role_unresolved`,
  `context_error`) — не нарушения.
- На каждую такую строку: `m.value++`, `m.byRole[e.Role]++`, `m.guard++`.
- Пустой `Role` на практике недостижим: `role` пуст только у `decision: "error"`
  из внешнего `except` в `main()`, а он отфильтрован выше. Нормализацию в
  `"unknown"` **не** писать.
- Веса и saturation P7 (10 / 4) не меняются → `weights_hash` не меняется (§7).
- Сигнатура `computeP7(run Run, cfg Config) metric` не меняется.

**Инвариант (не решение — прямое требование AC3 и §7):** `cfg.ExemptRoles`
(`verdict_exempt_roles`) **не** освобождает от guard-слагаемого. В guard-цикле
нет обращения к `cfg.ExemptRoles` вообще; `continue` из контрактного цикла
на него не распространяется. Deny на мутацию auditor'а — нарушение.

### H4. Роль `main` в таблице ролей — не синтезировать

Принято известное ограничение фазы 1: `facts()` (`scoring.go`) строит
`RoleRow` только из `run.Dispatches`, строки `main` не будет.
`Penalty.ByRole["main"]` существует (сумма атрибуции по-прежнему равна
`Points`, `TestPenaltyAttributionSumsToPoints` проходит), в общую сумму очков
входит, в `scores.jsonl` видна через `penalties[P7].by_role`. В тексте
карточки `main` виден в `Detail` P7 (`joinRoles` перечисляет роли), а P7-строка
при guard > 0 гарантированно печатается (H5). `scoring.go` не трогается —
его нет в §9, и синтетическая строка без `tokens`/`calls` вводила бы в
заблуждение (у main нет dispatch'а, его токены коллектор не считает).

### H5. `(guard: N deny)` — суффикс `Detail` + закрепление P7-строки

Ни (a) в чистом виде, ни (b) «отдельная строка карточки». Решение:

1. **Текст** — в `computeP7`. При `m.value > 0` detail как сейчас
   `"%d нарушений контракта (%s)"` (value и роли включают guard), и **только
   при `m.guard > 0`** дописывается `fmt.Sprintf(" (guard: %d deny)", m.guard)`.
   Метка — буквально `deny` и для `block` (§7 фиксирует текст).
2. **Счётчик наружу** — через `Penalty`, не через `Card`:
   ```go
   type metric struct { ...; guard int }            // metrics.go
   type Penalty struct { ...
   	GuardDenies int `json:"guard_denies,omitempty"` // metrics.go
   }
   ```
   `penaltyFrom` копирует `m.guard` в `p.GuardDenies` (в том числе на ранних
   `return`). `Card`, `Compute`, `scoring.go` не меняются.
3. **Рендер** (`render.go`, `RenderCard`): после отбора top-3 и **до** проверки
   «no penalties» — каждый `p` из `c.Penalties` с `p.GuardDenies > 0`, которого
   нет среди `findings` (по `ID`), дописывается в конец `findings` тем же
   форматом `−%d %s %s`. Top-3 и его порядок не меняются; P7 при guard > 0
   просто всегда виден (до 4 строк findings).

Почему так:

- AC4 буквально: «строка P7 … показывает `(guard: N deny)`». Чистый (a) на
  golden-фикстуре P7 не печатает (Context), т.е. AC4 не проверяем на AC5.
  Отдельная строка (b) — не «строка P7», и дублирует `Detail`.
- При N = 0: `GuardDenies == 0` → никакого закрепления, суффикса нет,
  `omitempty` → `scores.jsonl` и карточка **байт-в-байт** как раньше (AC4-хвост).
  `ScoreSchema` не повышается: поле аддитивное и опциональное.
- Первые 4 строки карточки (то, что main вставляет в `followup.md`) не
  меняются: закреплённая строка идёт после top-3.
- Дифф `render.go` — ~6 строк внутри одной функции; существующие проверки
  `render_test.go` на `lines[0..4]` остаются верными, сдвигаются только
  индексы пустой строки и таблицы ролей (H7).

### H6. `zprof stats`: `guard: top rules` — узкий скоуп подтверждён

`stats.Report`, `RenderHTML`, JSON `report.json` не меняются (п.8 плана
подтверждён: новый контракт отчёта ради одной диагностической строки не
оправдан; HTML-секция — отдельный issue, если понадобится). В `cmd/stats.go`,
внутри цикла по `args`, после строки `saved: …`:

- `score.ReadGuardEvents(filepath.Join(dir, "guard-events.jsonl"))`; ошибка
  чтения → `return fmt.Errorf("read guard events: %w", err)`; файла нет → ничего.
- Те же фильтры, что для dispatch'ей: `--session` по `e.SessionID`, `--role`
  по `e.Role` (`--role main` работает).
- Счёт по `Rule` только для `Decision ∈ {deny, block}` — та же семантика, что H3.
- Ноль посчитанных строк → ничего не печатать. Иначе одна строка в
  `cmd.ErrOrStderr()`, формат зафиксирован:
  `guard: top rules: force_push×3 return_format×1` — сортировка count desc,
  затем `rule` asc, не более 5 элементов, разделитель — пробел (как
  `modelSummary`). `internal/cmd` уже может импортировать `internal/score`
  (`cmd/score.go`), цикла импортов нет.

### H7. Тесты и фикстура (уточнения к шагу 7 плана)

- `testdata/run1/guard-events.jsonl`, ровно две строки, **реалистичные формы**:
  1. `{"ts":"2026-09-26T10:20:00Z","session_id":"s1","event":"subagent-stop","role":"implementer","dispatch_id":"t2","tool":null,"rule":"return_format","decision":"block","target":null,"input_hash":null,"run_id":null}`
     — сырой id `t2` (≡ `claude-code:s1:t2`), путь H2.1–2.
  2. `{"ts":"2026-09-26T09:45:00Z","session_id":"s1","event":"pre-tool","role":"main","dispatch_id":null,"tool":"Bash","rule":"force_push","decision":"deny","target":"git push","input_hash":"a1b2c3d4e5f6","run_id":null}`
     — main-fallback. Окно корня `t0`: `[09:30:00Z, 10:00:00Z]`
     (`ts_utc 10:00`, `duration_ms 1800000`). **Не** `10:00–10:40`, как в плане.
- Ожидание golden (подтвердить прогоном): P7 raw 4 → 10 очков, `Score 40/100`,
  `Lucky`; top-3 `P1, P2, P4` без изменений; `lines[5]` =
  `−10 P7 4 нарушений контракта (implementer, main) (guard: 2 deny)`; пустая
  строка → `lines[6]`, таблица ролей → `lines[7..]`; штраф `implementer` в
  таблице меняется (P7 делится 3:1 implementer:main) — число берётся из прогона.
  В `scoring_test` — `c.Penalties[6].GuardDenies == 2`.
- `cmd/score_test.go`: `setupScoreProject` копирует и `guard-events.jsonl`;
  три `"Score 45/100"` → `"Score 40/100"`. `persist_test.go` не трогать.
- `reader_test.go`: `ReadGuardEvents` — нет файла → `(nil, nil)`; битая строка
  пропущена, соседние целы; строка с `"dispatch_id": null` **сохранена**.
  `AttachGuardEvents` — table-driven: сырой id → run; составной id → тот же
  run; id вне runs → отброшен; null + ts внутри окна второго из двух run'ов →
  второй (регрессия на «≤ end» из плана: ts между `end₁` и `end₂`, но до
  `start₂`, → отброшен); null + ts вне окон / непарсящийся / пустой → отброшен;
  null + другой `session_id` → отброшен; `DurationMs == 0` → отброшен.
- `metrics_test.go`: `deny` и `block` считаются, `allow_unverified`/`error`/`""`
  нет; `ExemptRoles{"auditor"}` + guard-deny auditor'а → учтён, при этом
  контрактное нарушение того же auditor'а — нет (разница в одной функции);
  N = 0 → `Detail` без суффикса и `GuardDenies == 0`.
- `render_test.go`: карточка с P7 вне top-3 и `GuardDenies > 0` → P7-строка
  дописана после top-3; с `GuardDenies == 0` → вывод идентичен текущему.
- Новый `cmd/stats_test.go`: без файла — строки нет; с файлом — точный формат
  H6; `allow_unverified` не считается; `--session`/`--role` фильтруют.

### H8. Отклонения от плана

| План | ADR | Причина |
|---|---|---|
| Матч по `dispatch_id` через `runOf`-подобный индекс составных id | Матч по сырому `toolUseId` (H2.1) | Guard пишет сырой id, коллектор — составной (Context п.1) |
| Fallback: наибольший `Root.Timestamp ≤ ts` | Окно `[end − DurationMs, end]` + равенство `session_id` (H2.3) | `ts_utc` — время завершения (Context п.2) |
| Выбор (a) суффикс / (b) отдельная строка | Суффикс + закрепление P7-строки через `Penalty.GuardDenies` (H5) | (a) не показывает P7 на golden; (b) — не «строка P7» |
| Фикстура: main-событие в `10:00–10:40`, `dispatch_id` составной | `09:45`, сырой `t2` (H7) | Следствие двух строк выше |

## Consequences

- Меняются только `cli/internal/score/{reader,metrics,render}.go`,
  `cli/internal/cmd/{score,stats}.go` и тесты/фикстура при них. `scoring.go`
  (`Card`, `Compute`, `facts`) не меняется.
- **Не меняются:** `profiles/base/` (`zprof-guard.py`, `zprof-collect.py`,
  `guard.yaml`, pytest), `internal/manifest` (`GuardConfig` — #28),
  `internal/stats` (`Report`, `RenderHTML`), doctor (#29), код #52/#53.
  `go.mod` без новых зависимостей.
- `scores.jsonl`: у P7 появляется `guard_denies` только когда > 0. Старые
  строки читаются как раньше. `--all-missing` не пересчитывает уже оценённые
  runs (`ScoreKey` = run + `weights_hash`, хэш не изменился) — guard-события,
  появившиеся для старых runs, в их карточки не попадут без явного `--run`.
- Main-события вне окна любого завершённого run'а (main работал без loop'а)
  в score не попадают — только в `zprof stats`. Это соответствует §7:
  score — оценка run'а, не сессии.
- Роль `main` в таблице ролей отсутствует (H4) — видна в `Detail` и JSON.
- Если `session_id` subagent-хука когда-нибудь окажется отличным от main-сессии,
  H2.1 это переживёт (ключ без сессии); H2.3 касается только main, где
  `session_id` хука и `Root.SessionID` — одна сессия.

## Alternatives considered

- **Третий параметр `BuildRuns(ds, evs, gevs)`** — ~10 правок вызовов ради
  логики, которая от `BuildRuns` не зависит, кроме готовых `runs`. Отвергнуто.
- **Сравнение `dispatch_id` строкой как есть** (план) — ни одного совпадения на
  реальных данных. Отвергнуто (Context п.1).
- **Композиция `claude-code:<e.SessionID>:<raw>`** вместо `rawID` — зависит от
  равенства `session_id` subagent-хука и main-сессии, которое не гарантировано.
  Отвергнуто в пользу ключа без сессии.
- **Fallback «наибольший `Root.Timestamp ≤ ts`»** (план) — приписывает события
  run'а N к run'у N−1. Отвергнуто (Context п.2).
- **Fallback «до начала следующего run'а»** (без верхней границы
  `DurationMs`) — штрафует run за действия main после его завершения.
  Отвергнуто.
- **Синтетическая `RoleRow{Role: "main"}`** — дифф `scoring.go` вне §9, строка
  с нулевыми `tokens`/`calls`. Отвергнуто (H4).
- **(a) Только суффикс в `Detail`** — P7 не печатается, когда вне top-3, в том
  числе на golden. Отвергнуто (H5).
- **(b) `Card.GuardDenyTotal` + отдельная строка `guard: N deny`** — новое
  поле карточки и прокидка из `computeP7` через `Compute`; строка не является
  «строкой P7». Отвергнуто (H5).
- **Нормализация пустой роли в `"unknown"`** — мёртвый код (H3). Отвергнуто.
- **`guard: top rules` в `stats.Report`/HTML/JSON** — новый контракт отчёта
  ради одной строки. Отложено, отдельным issue при запросе.
