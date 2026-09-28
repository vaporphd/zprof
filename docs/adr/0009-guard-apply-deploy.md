# ADR 0009: `zprof apply` деплоит guard — `guard.json`, хуки с `matcher`, `permissions.deny`

**Date:** 2026-09-28
**Status:** accepted
**Issue:** #28 (supersedes #18)
**Parent:** ADR-0001 (`--telemetry-only`, `DeployTelemetry`), ADR-0004 (D2 fail-open, D8 формат deny), ADR-0006 (F3 `$merge_roles` в `merge_preflight`)
**Plan:** `tasks/plan-issue-28.md` (этот ADR — шаг 1; закрывает шесть «Архитектурных вопросов» плана, в четырёх местах перекрывает план и в одном — буквальный текст AC, см. I9)
**Spec:** `docs/superpowers/specs/2026-09-27-guard-hooks-design.md` §4, §5.7, §8.1–§8.4, §9, §11

## Context

`zprof-guard.py`, `guard.yaml` и форма `guard.json` готовы (#23–#27), но Go-сторона
их не деплоит: `LoadBase` не читает ни скрипт, ни `guard.yaml`, `apply` не рендерит
`.claude/guard.json`, в `settings.local.json` нет guard-хуков. Спека §8.2–§8.4 и
AC1–AC12 фиксируют **семантику** (слои, merge, форма хуков), но не Go-структуру и
не несколько граничных случаев. Факты кода, на которые опирается решение:

1. **`Apply()` не зовёт `deployCollector` напрямую.** `engine.go:152` зовёт
   `DeployTelemetry(opts.ProjectDir, opts.Base)`, а та — `deployCollector` +
   `EnsureHooks`. Тот же `DeployTelemetry` — единственное тело `--telemetry-only`
   (`cmd/apply.go:55`). У `DeployTelemetry` нет ни overlay, ни `.zprof.yaml`.
2. **Скрипт сам дописывает хвост причины.** `zprof-guard.py:deny_output()`
   (строки 270–282) строит `"zprof guard [<id>]: <reason без точки>. Не обходи: верни
   `verdict: blocked`, reason: <id>."`. Хвост в `reason` правила — дубль в выводе.
3. **Пустой `roles`/`not_roles` в скрипте = «без ограничения».** `_check_rule`:
   `if roles and call["role"] not in roles` — пустой список пропускает все роли.
   Подстановка `$readonly_roles` → `[]` превратила бы `readonly_mutation` в запрет
   мутаций **для всех ролей**; `$merge_roles` → `[]` — запрет merge всем.
4. **Недоразрешённая `$ref` молча выключает правило.** `_validated_str_list` бросает
   `ValueError` на любой элемент с ведущим `$` в `match`/`roles`/`not_roles`;
   внешний fail-open съедает исключение — **весь** pre-tool вызов проходит как allow.
5. **Скрипт требует типов.** `load_config` — `version == 1`; `allow_write_prefixes`
   не-список → `ValueError` (строки 505–507). `null` вместо `[]` ломает guard.
6. **`CarryOverFrom` не знает про `guard:`.** `cmd/apply.go:95–108` строит свежий
   `ProjectManifest` и переносит из старого `.zprof.yaml` только перечисленные поля;
   `Apply()` затем `Save()`-ит его. Без переноса `Guard` каждый `zprof apply <overlay>`
   стирает секцию `guard:` из `.zprof.yaml` и деплоит guard как `enabled: true`.
   (`zprof sync` грузит манифест целиком — там проблемы нет.)
7. **Профили и бинарь обновляются независимо.** `zprof sync` тянет `profiles/` из
   remote; бинарь — `make install`. Новый ключ в `base/guard.yaml` не должен ронять
   `apply` у старого бинаря.

## Decision

Весь Go-код guard-деплоя — в новом `cli/internal/apply/guard.go` плюс правки
`settings.go`, `collector.go`, `overlay/loader.go`, `manifest/project.go`,
`cmd/apply.go`. Новых Go-зависимостей нет (`yaml.v3`, `encoding/json` уже есть).

### I1. Представление слоя: типизированный верх + сырые правила (вопрос 1)

Гибрид, как рекомендует план.

```go
// guardDoc — один слой guard.yaml и итог merge; сериализуется в guard.json.
type guardDoc struct {
	ReadonlyRoles      []string
	MergeRoles         []string
	AllowWritePrefixes []string
	PermissionsDeny    []string
	ExemptRoles        map[string][]string
	Rules              []map[string]any // сырые, ключ merge — rule["id"].(string)
	Extra              map[string]any   // неизвестные top-level ключи (кроме version)
}
```

- **Парсинг слоя** (`parseGuardLayer(data []byte, src string) (*guardDoc, error)`):
  `yaml.Unmarshal` в `map[string]any` (как `renderSchema`), затем известные ключи
  извлекаются хелперами `asStringList`/`asStringListMap`/`asRuleList` с
  `fmt.Errorf("%s: %s: expected list of strings", src, key)` на неверный тип.
  Не `yaml.Decoder.KnownFields` и не struct-теги: нужен п. «Extra» ниже.
- **`version`:** если ключ есть — обязан быть `1`, иначе ошибка
  (`"%s: unsupported guard.yaml version %v"`). У base ключ обязателен; overlay может
  опустить. В `guard.json` `version: 1` пишет рендер, не merge.
- **Правила — сырые карты.** Каждое правило обязано иметь непустой строковый `id`
  (иначе ошибка с `src` и индексом). Остальные поля не парсятся: схема правила
  расширяется скриптом (`context`, будущие поля) без правки Go.
- **`Extra`** — все прочие top-level ключи as-is; merge — «поздний слой перекрывает»
  (правило скаляров §8.2, применённое к любому неизвестному значению). Даёт
  совместимость «новый профиль + старый бинарь» (Context п.7).
- **Рендер** (`(*guardDoc).render() ([]byte, error)`): собирает `map[string]any` с
  `version: 1`, пятью известными ключами, `rules` и `Extra`, затем
  `json.MarshalIndent(v, "", "  ")` + `\n` (как `renderSchema`). `encoding/json`
  сортирует ключи карт — байтовая стабильность повторного apply бесплатна.
  **Nil-срезы и nil-карта рендерятся как `[]`/`{}`, никогда `null`** (Context п.5):
  нормализация до `render`. `Extra` не может перезаписать известный ключ (они
  исключены при парсинге).

Порядок в итоговых списках детерминирован: первая встреча значения сохраняет позицию.
`rules`: правило позднего слоя с существующим `id` заменяет прежнее **на его месте**;
новые `id` дописываются в конец в порядке слоя.

### I2. Merge и проектный слой

`mergeGuard(base *guardDoc, overlays []*guardDoc, proj *manifest.GuardConfig) *guardDoc`
— чистая функция, порядок слоёв: base → overlays в порядке `opts.Overlays` →
проект.

- base→overlay: всё по §8.2 — пять списков конкатенируются с дедупом (**включая
  `merge_roles`** — overlay дописывает), `exempt_roles` — union списков по ключу с
  дедупом, `rules` — по `id`, `Extra` — перекрытие.
- Проектный слой (`manifest.GuardConfig`) по §8.3:
  - `ReadonlyRoles` → дописываются в `readonly_roles` (дедуп);
  - `AllowWriteOutside` → дописываются в `allow_write_prefixes` (дедуп);
  - `ExemptRoles` → union;
  - `MergeRoles`: **`len(MergeRoles) > 0` → заменяет итог целиком**; пустой/nil —
    не трогает. Пустой список не означает «никто не мержит»: (а) тег `omitempty`
    теряет `[]` при `Save()`, (б) Context п.3 — пустой `$merge_roles` снимал бы
    ограничение у `merge_preflight`. Запретить merge всем — через `exempt_roles`/
    `extra_deny_bash`, не через пустой список;
  - `ExtraDenyBash` непустой → синтетическое правило (I5), добавляется последним
    через тот же merge по `id` (проектный слой поздний — замещает одноимённое
    правило overlay, если оно было).
- В `manifest`: `func (g *GuardConfig) IsEnabled() bool` — `g == nil || g.Enabled == nil
  || *g.Enabled`. `CarryOverFrom` получает `if m.Guard == nil { m.Guard = prev.Guard }`
  (Context п.6) — без этого AC9 недостижим через `zprof apply <overlay>`; тест —
  рядом с существующим `project_test.go:66`.

### I3. `$`-подстановка — один проход после merge (вопрос 2)

Подтверждаю рекомендацию плана: **после merge, один проход**,
`resolveGuardRefs(doc *guardDoc, mutating []string) error`.

- Таблица ссылок строится из **итогового** документа: `$readonly_roles` →
  `doc.ReadonlyRoles`, `$merge_roles` → `doc.MergeRoles` (уже после проектной
  замены), `$mutating_bash_patterns` → `mutating`.
- `mutating` читается из `Base.TelemetrySchema`: `yaml.Unmarshal` в `map[string]any`,
  ключ `mutating_bash_patterns` через тот же `asStringList`. Читается **лениво** —
  только если в правилах есть ссылка на него; отсутствие `TelemetrySchema` или ключа
  при наличии ссылки — ошибка apply.
- Область: в каждом правиле ключи `match`, `roles`, `not_roles` (ровно то, что
  валидирует `_validated_str_list`). Значение-строка, равное `$<name>`, заменяется
  **копией** списка. Элемент списка, начинающийся с `$`, — ошибка (сплайс в список
  спека не предусматривает). Прочие поля правил (`reason`, `context`, …) и
  `allow_write_prefixes` (`$CLAUDE_PROJECT_DIR`, `$TMPDIR` раскрывает скрипт) не
  трогаются.
- Инварианты, нарушение каждого — `error`, apply падает (fail-closed, как
  `renderSchema` на битом реестре — это ошибка автора профиля, не проекта):
  1. неизвестное имя ссылки;
  2. после прохода в `match`/`roles`/`not_roles` любого правила осталась строка с
     ведущим `$` (Context п.4);
  3. ссылка в `roles`/`not_roles` разрешилась в пустой список (Context п.3).
- Функция возвращает ошибку с `rule id` и ключом:
  `fmt.Errorf("guard rule %q: %s: %w", id, key, err)`.

### I4. `enabled` гейтит только деплой; merge считается всегда (вопрос 3, AC9)

**Прочтение плана подтверждаю.** `deployGuard` всегда выполняет parse → merge →
resolve независимо от `enabled`. `enabled` решает только, что делать с результатом:

| | `enabled` (по умолчанию) | `enabled: false` |
|---|---|---|
| `.claude/zprof-guard.py`, `.claude/guard.json` | пишутся (0755 / 0644) | не пишутся и не удаляются (I6) |
| guard-хуки `PreToolUse`, `SubagentStop` | upsert | удаляются **все** записи, содержащие `zprof-guard.py`, на этих двух событиях |
| `permissions.deny` | дополняется `doc.PermissionsDeny` без дублей | из него вычитается **свежепосчитанный** `doc.PermissionsDeny`; чужие остаются |

- Источник вычитания — итог текущего merge, **не** `guard.json` на диске. Файл на
  диске — артефакт вывода, а не реестр владения: он может отсутствовать, быть
  отредактирован руками или устареть. Принятый риск: если base/overlay/проект
  поменяли `permissions_deny` между последним enabled-apply и disable, снимается
  текущее состояние, не историческое (тот же контракт, что у остального
  идемпотентного `apply`).
- Следствие: битый overlay `guard.yaml` роняет apply и при `enabled: false` —
  осознанно (ошибка автора профиля, ловится repo-level тестами).
- Пустые контейнеры при снятии: событие, у которого не осталось записей, удаляется
  из `hooks`; ключ `deny` при пустом списке удаляется; карта `permissions` не
  удаляется никогда. При `enabled` и пустом `permissions_deny` ключи не создаются.

### I5. Правило `extra_deny` — финальный текст (вопрос 5, AC4)

Форма (ключи JSON сортируются рендером):

```json
{"id": "extra_deny", "tools": ["Bash"], "match": [<extra_deny_bash, дедуп, порядок .zprof.yaml>],
 "reason": "команда запрещена проектным стоп-листом (guard.extra_deny_bash в .zprof.yaml)"}
```

**`reason` в `guard.json` — без хвоста** (Context п.2): хвост «Не обходи: …» дописывает
`deny_output()`, как для каждого правила base (ни одно `reason` в `guard.yaml` хвоста
не содержит). Дословно:

- `reason` (77 символов): `команда запрещена проектным стоп-листом (guard.extra_deny_bash в .zprof.yaml)`
- итог `permissionDecisionReason` (161 символ, ≤200), который проверяет E2E/tester:

```
zprof guard [extra_deny]: команда запрещена проектным стоп-листом (guard.extra_deny_bash в .zprof.yaml). Не обходи: верни `verdict: blocked`, reason: extra_deny.
```

Go-константа `extraDenyReason` в `guard.go`; тест сравнивает с ней, а не с копией
литерала. Regex из `extra_deny_bash` Go не компилирует и не валидирует (Go `regexp`
≠ Python `re`: lookahead и пр.) — невалидный regex обрабатывает fail-open скрипта.

### I6. Файлы guard при `enabled: false` — оставить (вопрос 4)

**Оставляем** `.claude/zprof-guard.py` и `.claude/guard.json` как есть — не пишем и
не удаляем. Основания: хуки сняты — скрипт никто не вызывает; симметрия с
`deployCollector`, который своих файлов не удаляет никогда; doctor (#29) может
показать «guard disabled» рядом с последним известным конфигом. `deployGuard` при
`enabled: false` возвращает пустой список путей. Тест AC10 фиксирует: после disable
оба файла существуют и байтово равны состоянию до disable.

### I7. Сигнатуры, порядок и `--telemetry-only` (вопрос 6)

**Зависимости по данным между `deployCollector` и `deployGuard` нет — подтверждаю.**
`deployGuard` берёт `mutating_bash_patterns` из `Base.TelemetrySchema` в памяти, а не
из `.agentlog/schema.json`; оба апсерта в `settings.local.json` адресуют записи по
имени скрипта и не видят друг друга. Порядок — соглашение читаемости.

**`deployGuard` входит в `DeployTelemetry`**, а не дублируется в двух местах: сегодня
`DeployTelemetry` — единственный путь деплоя hook-инфраструктуры и для `Apply()`, и
для `--telemetry-only`; дубль вызова гарантированно разъедется.

```go
// collector.go
type GuardLayers struct {
	Overlays []*overlay.Overlay    // активные overlay; nil — только base
	Project  *manifest.GuardConfig // секция guard: .zprof.yaml; nil — дефолты
}
func DeployTelemetry(projectDir string, base *overlay.Base, layers GuardLayers) ([]string, error)
// тело: deployCollector → deployGuard → EnsureHooks → append settings.local.json

// guard.go
func deployGuard(projectDir string, base *overlay.Base, layers GuardLayers) ([]string, error)
```

- Узкая сигнатура вместо `deployGuard(opts ApplyOpts)` из AC3 — ровно по той причине,
  по которой узкая у `deployCollector` (комментарий `collector.go:25–27`): у
  `--telemetry-only` нет `ApplyOpts`.
- `deployGuard`: если `len(base.GuardScript) == 0 || len(base.GuardSchema) == 0` —
  no-op (`nil, nil`): ни файлов, ни хуков, ни снятия (старый/урезанный base, как
  optional-поля коллектора). Иначе: parse base → parse каждый непустой
  `o.GuardSchema` (src = `"overlay " + o.Manifest.Name`) → `mergeGuard` →
  `resolveGuardRefs` → при `IsEnabled()` запись скрипта (verbatim, 0755) и
  `guard.json` (0644) через `writeFileAtomic` → `ensureGuardSettings`. Возврат — пути
  двух файлов при enabled, иначе пусто. `settings.local.json` в список добавляет
  `DeployTelemetry` один раз (как сейчас).
- `engine.go`: `DeployTelemetry(opts.ProjectDir, opts.Base, GuardLayers{Overlays:
  opts.Overlays, Project: opts.Project.Guard})`. Шаг 5.5/5.6 остаётся на своём месте.
- `cmd/apply.go --telemetry-only`: если `.zprof.yaml` есть — `manifest.LoadProject`,
  `LoadOverlay` каждого `proj.Overlays` (ошибка загрузки — ошибка команды, как в
  `sync.go`), `GuardLayers{Overlays, proj.Guard}`; нет `.zprof.yaml` —
  `GuardLayers{}` (только base, guard включён по умолчанию, §8.3). Dry-run печатает
  ещё две строки: `.claude/zprof-guard.py`, `.claude/guard.json`.
- Существующие вызовы `DeployTelemetry` в `collector_test.go` получают
  `GuardLayers{}`; их фикстуры base без `GuardScript` → guard no-op, ожидаемые
  списки путей не меняются.

### I8. `settings.go`

- `hookGuardTemplate` → `collectorHookTemplate` (единственный файл-потребитель).
- `hookSpec{event, matcher, command string}`; запись рендерит
  `"matcher"` только при непустом значении. Коллекторные спеки — `matcher: ""`,
  их JSON не меняется (существующие тесты `settings_test.go` не трогаются).
- `zprofHookIndex(entries []any, script string) int` — подстрока `script` в
  JSON записи. Запись «устарела», если `hookCommand(e) != spec.command` **или**
  `hookMatcher(e) != spec.matcher` (отсутствие = `""`) — смена matcher тоже апгрейд.
- `guardHooks` — две спеки, команды дословно AC7 / §8.4, через
  `guardHookTemplate = test -x "$CLAUDE_PROJECT_DIR/.claude/zprof-guard.py" &&
  "$CLAUDE_PROJECT_DIR/.claude/zprof-guard.py" %s || true`.
- Read/modify/write `settings.local.json` выносится из `EnsureHooks` в
  `readSettings(path) (map[string]any, error)` / `writeSettings(path, map)` без
  изменения поведения. `EnsureHooks(projectDir)` — сигнатура не меняется.
- `ensureGuardSettings(projectDir string, enabled bool, deny []string) error` — один
  read/modify/write: `upsertGuardHooks`/`removeGuardHooks` + `ensurePermissionsDeny`/
  `removePermissionsDeny`, все четыре — над картой в памяти. `permissions.deny`, если
  есть, но не `[]any` строк — ошибка apply (чужой формат не перезаписываем молча).
  Прочие ключи `permissions` (`allow`, `ask`, …) не трогаются.

### I9. Отклонения от плана и буквального AC

1. **Вопрос 5 / AC4 — хвост в `reason`:** план требует хвост «Не обходи…» в тексте
   правила; фиксируем `reason` **без** хвоста, т.к. его дописывает скрипт (Context
   п.2). Итоговая строка deny содержит хвост ровно один раз — это и есть намерение
   плана.
2. **AC3 `deployGuard(opts)`** → узкая сигнатура (I7).
3. **План шаг 4 `ensurePermissionsDeny(projectDir, …)`** → функция над картой внутри
   `ensureGuardSettings` (один read/modify/write на guard).
4. **План шаг 5 «`collector.go` и/или `apply.go`»** → строго `DeployTelemetry` +
   загрузка слоёв в `cmd/apply.go` (I7).
5. **Не было в плане:** `CarryOverFrom` переносит `Guard` (Context п.6) — правка
   `manifest/project.go` в шаге 2 плана; `MergeRoles` заменяет только при
   `len > 0` (I2); fail-closed на пустую ссылку в `roles`/`not_roles` (I3).

## Consequences

- `zprof apply`, `zprof sync` и `--telemetry-only` деплоят guard одним путём
  (`DeployTelemetry`); расхождение между ними исключено конструктивно.
- `guard.json` гарантированно без неразрешённых `$` в `match`/`roles`/`not_roles`,
  без `null`-контейнеров и без пустых `roles`/`not_roles` из ссылок — три тихих
  способа выключить guard (Context п.3–5) закрыты на стороне apply.
- Новый top-level ключ в `guard.yaml` проходит через старый бинарь (`Extra`);
  новое поле правила — тоже (сырые карты). Цена: Go не валидирует схему правил —
  опечатка в имени поля правила ловится только тестами скрипта.
- Merge считается и при `enabled: false`: сломанный overlay `guard.yaml` блокирует
  apply даже у проекта с выключенным guard.
- Снятие `permissions.deny` работает по текущему merge, не по истории; значение,
  удалённое из профиля между applies, остаётся в `settings.local.json` пользователя
  (при enabled — навсегда, пока не удалят руками). Реестр владения — не в этой задаче.
- При `enabled: false` в проекте остаются `zprof-guard.py`/`guard.json` последнего
  enabled-apply; doctor (#29) обязан не считать это ошибкой.
- `DeployTelemetry` меняет сигнатуру — внутренний пакет, внешних потребителей нет;
  правка вызовов в `engine.go`, `cmd/apply.go`, `collector_test.go`.

## Alternatives considered

- **Полностью типизированные правила** (`type guardRule struct{…}`): ломает
  совместимость «новый профиль + старый бинарь» и требует `interface{}` для
  `match`/`roles` (строка-ссылка или список) — выигрыша в строгости нет. Отклонено.
- **Всё сырое (`map[string]any` на верхнем уровне)**: merge-операции §8.2 стали бы
  type-switch на каждом ключе без компиляторной проверки. Отклонено.
- **Подстановка в каждом слое до merge**: `$merge_roles` в base разрешился бы до
  проектной замены — неверный итог; плюс N проходов вместо одного. Отклонено.
- **Снятие `permissions.deny` по `guard.json` на диске** (или union диск+свежий):
  файл — вывод, не реестр владения; ручная правка файла приводила бы к удалению
  пользовательских записей deny. Отклонено.
- **Удалять `zprof-guard.py`/`guard.json` при disable**: асимметрия с коллектором и
  потеря диагностики для doctor без выигрыша (хуки уже сняты). Отклонено.
- **Отдельный `DeployGuard` в `engine.go` и в `cmd/apply.go`**: два места вызова
  разъедутся (ровно риск, который issue просит проверить для `--telemetry-only`).
  Отклонено.
- **Хвост «Не обходи…» внутри `reason` extra_deny**: дубль хвоста в выводе
  `deny_output()`. Отклонено.
