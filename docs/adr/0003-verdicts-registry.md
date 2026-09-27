# ADR 0003: Реестр вердиктов `verdicts.yaml`: маппинг в task-runner, проверка в doctor, деплой в `schema.json`

**Date:** 2026-09-27
**Status:** proposed
**Issue:** #20 (milestone panel-2026-09-27); связан с #19 (бюджет, уже в `main`) и #26 (guard, фаза 2)
**Run:** `.zprof/runs/2026-09-27-verdicts-yaml-registry.md`, audit_step 1

## Context

Факты сняты с `main` @ `4cbb907`. Номера строк указаны по текущим файлам и расходятся с теми, что в тексте issue.

1. **Словарь вердиктов.** В `return_format` всех `profiles/base/agents/**/*.md`,
   `profiles/overlays/*/agents/*.md` и `.claude/agents/**/*.md` 70 разных имён агентов и около
   55 разных токенов. Полный список по агентам приведён в §D1. Отклонения, которые ломают петлю:
   - reviewer: в шести overlay'ях `block|approve-with-fixes|approve|awaiting-approval`,
     в `backend-kotlin-jvm` `approve|changes-requested|blocked`, в `zcode-harness` и
     `.claude/` `done|blocked|failed`;
   - bug-hunter: в `backend-kotlin-jvm` `fixed|blocked|failed`, в шести overlay'ях
     `done|blocked|failed|awaiting-approval`;
   - `passed` возвращают **runner'ы** (`gradle-runner`, `pytest-runner`, `xcode-runner`,
     `playwright-runner`, `vitest-runner`). У tester'а в каждом overlay'е `done|blocked|failed`,
     так что пример из issue «tester → passed» неточен;
   - auditor и auditor-deep отвечают `completion: complete|incomplete|blocked`
     (`auditor.md:11-12, 89-91, 115`, в `.claude/`-копиях то же самое).
2. **task-runner** (`profiles/base/agents/task-runner.md`, байт-в-байт совпадает с
   `.claude/agents/task-runner.md`). Раздел «Правила диспатча» занимает `:140-161`: маршрутизация
   только по `failed` (`:146`), `blocked` (`:147-149`) и tester `failed` (`:150-152`), правило
   non-schema в `:153-154`. Раздел «Бюджет» (`:163-208`) уже вводит `runner.max_dispatches`
   (дефолт 14) и в `:190-192` говорит, что круги reviewer→implementer расходуют общий счётчик.
   «Аудит шагов» `:266-275` читает `completion:`.
3. **Коллектор** (`profiles/base/zprof-collect.py`). Вердикт разбирает функция `_class_a_checks`
   в `:1625-1668`, а не в `:1388-1408`, как написано в issue. Там лежит `_SCHEMA_FIELDS`. Функция
   ищет **первую строку в любом месте ответа**, которая после `strip().lower()` начинается с
   `verdict:`, а не только первую строку. От этого зависят `return_parsed`, `has_preamble`
   (есть текст до этой строки) и `verdict_value` (первое слово, в нижнем регистре). У аудиторов
   строки `verdict:` нет, поэтому `return_parsed=False`, а `has_preamble=None`.
4. **Score** (`cli/internal/score/config.go:59-61`, `metrics.go`).
   - `ExemptRoles={auditor, auditor-deep}` исключает эти роли из P6 (токены на неразобранный
     ответ) и P7 (Class-A нарушения). Обоснование в `telemetry.yaml:95`: «contract does not start
     with `verdict:`».
   - `ReviewBlockVerdicts={block, changes-requested, blocked}` считается только при
     `Role == "reviewer"` (`metrics.go:191`), поэтому `blocked` аудитора в P5 не попадает при
     любом списке.
   - `LoadConfig` перекрывает дефолты из `schema.json` только непустым списком
     (`config.go:112, 118`).
5. **Doctor** (`cli/internal/doctor/diagnostics.go`). `checkAgentFrontmatter` (`:129-183`) парсит
   frontmatter через `frontmatterRe` + `yaml.v3` и для ролей (`agents.RoleOf`) проверяет только,
   что `return_format` не пустой. Парсера **строки вердикта** из `return_format` в Go нет:
   `eval/parser.go:336 parseReturnFormat` разбирает *ответы* субагентов, а не frontmatter.
   Doctor работает по `<project>/.claude/agents` и получает `repoDir` (checkout `profiles/`).
6. **Деплой** (`cli/internal/apply/collector.go:27-57`). `deployCollector(projectDir, base)` пишет
   `telemetry.yaml` через `yamlToJSON` в `.agentlog/schema.json`. На тот же путь опирается
   `DeployTelemetry` (`zprof apply --telemetry-only`, ADR 0001). Про overlay'и функция ничего не
   знает. `overlay.LoadBase` (`loader.go:164-211`) читает `telemetry.yaml` как опциональный файл.
   Python-сторона (коллектор, `telemetry_test.py`) YAML не парсит: stdlib only, есть
   самописный парсер плоских списков. В деплое она читает `schema.json`.
7. **Guard, #26** (`docs/superpowers/specs/2026-09-27-guard-hooks-design.md`).
   - §6: в фазе 1 валидатор `subagent-stop` берёт первую содержательную строку `return_format`
     (`^(verdict|completion):`) и проверяет первое слово ответа по списку из frontmatter.
   - §12 п.3: в фазе 2 «обязательные ключи ответа» берутся из `verdicts.yaml`.
   - §8: guard читает только `.claude/guard.json`, который рендерит `zprof apply` из base и
     overlay-слоёв и подставляет в него значения из `telemetry.yaml` (`$mutating_bash_patterns`).
   - §5.7: любой deny требует от агента «верни `verdict: blocked`».
8. **Тела агентов.** Токены после `verdict:` в теле, которых нет в enum собственного frontmatter
   (скан по всем файлам):
   - шесть overlay-reviewer'ов, `:25`: `verdict: blocked` (отказ от self-review). Это проблема AC4;
   - `auditor*.md:37`: `verdict: done`, цитата claim'а исполнителя;
   - `evaluator.md:81`: `verdict: judged`, схема суб-судей `general-purpose`;
   - `pr-shepherd.md:88`: `verdict: done`, цитата фабрикации Haiku;
   - `task-runner.md:134`: `verdict: insufficient`, цитата гейта.

   Последние четыре случая — законные цитаты чужих контрактов, не ошибки.

## Decision

### D1. `profiles/base/verdicts.yaml`: формат и содержимое

Реестр ключуется по **имени агента** (базовое имя файла без stack-суффикса) и хранит объединение
токенов этого имени по всем scope'ам (base, каждый overlay, `.claude/`). Точный enum конкретного
файла по-прежнему задаёт его frontmatter. Реестр добавляет две вещи: **семантику** каждого токена
(base-enum + действие раннера) и **допустимое множество** на уровне роли. Doctor связывает оба
уровня: frontmatter ⊆ реестр, тело ⊆ frontmatter (§D3).

Схема (version 1):

```yaml
version: 1
base_enum: [done, blocked, failed]
# Действия раннера — закрытый словарь. Для loop/insert после двоеточия пишется цель:
# имя роли, @next (роль из поля `next:` ответа) или @audited (исполнитель шага,
# который проверял аудитор).
actions: [next, loop, insert, triage, escalate, abort]
# Разрешён любой роли, даже если не перечислен в её карте. Роль может его переопределить.
universal:
  blocked: {base: blocked, action: triage}
# Токены, которые тело агента цитирует из чужих контрактов (doctor check b).
# "*" = любой токен, известный реестру.
quotes:
  task-runner: "*"
  auditor: [done]
  auditor-deep: [done]
  evaluator: [judged]
  pr-shepherd: [done]
templates:
  std: &std
    done:   {base: done,   action: next}
    failed: {base: failed, action: abort}
roles:
  <name>: {<token>: {base: <base_enum>, action: <action>[:<target>]}, ...}  # или *std
```

Токен-шаблон пишется ключом с суффиксом `-*`. `blocked-*` совпадает с `blocked-<что угодно>`. При
поиске точный ключ важнее шаблона. Во frontmatter `blocked-<reason>` задаёт шаблон `blocked-*`, а
значение целиком в `<…>` означает «любое» и не проверяется (так же, как в guard §6).

Семантика действий. Её дословно получает раздел «Правила диспатча» в task-runner.md (§D2).

| action | Что делает раннер |
|---|---|
| `next` | следующий шаг маршрута. Если шаг последний, раннер возвращает `done` |
| `loop:<X>` | диспатч `X` с `artifact`/`one_line` как заданием, затем **повтор текущего шага**. Не больше 3 кругов на пару (текущий, X), каждый диспатч списывается из общего бюджета |
| `insert:<X>` | диспатч `X` с `artifact` как заданием, затем следующий шаг маршрута после текущего, **без повтора** текущего |
| `triage` | текущее правило `blocked` (`:147-149`): причина в стоп-листе → `escalate`; нехватку данных закрывает сосед (`next:` или причина) → шаг соседу |
| `escalate` | раннер возвращает `verdict: blocked` + `question` |
| `abort` | раннер возвращает `verdict: failed` |

Общее правило: после любого внепланового `implementer` (от `loop`/`insert`) идёт `tester`, если его
нет в оставшемся маршруте. Инвариант «код → тесты» уже есть во всех маршрутах.

Роли и маппинг. Это **обязательное содержимое** файла. Каждый токен проверен по frontmatter;
`blocked` приходит через `universal`, если не указан отдельно:

```yaml
roles:
  # --- base-роли ---
  task-runner: *std              # отвечает main; action для main не используется
  planner: *std
  architect: *std
  implementer: *std
  refactor-agent: *std
  frontend-developer: *std
  groomer: *std
  evaluator: *std
  evaluator-telemetry: *std
  expert-panel: *std
  tester:
    done:   {base: done,   action: next}
    failed: {base: failed, action: "loop:implementer"}
  reviewer:
    approve:            {base: done,   action: next}
    done:               {base: done,   action: next}              # zcode-harness, .claude
    approve-with-fixes: {base: done,   action: "insert:implementer"}
    block:              {base: failed, action: "loop:implementer"}
    changes-requested:  {base: failed, action: "loop:implementer"} # backend-kotlin-jvm
    awaiting-approval:  {base: failed, action: "loop:implementer"} # раннер = approver, §D2
    failed:             {base: failed, action: "loop:implementer"} # zcode-harness, .claude
  bug-hunter:
    done:              {base: done,   action: next}
    fixed:             {base: done,   action: next}               # backend-kotlin-jvm
    awaiting-approval: {base: done,   action: "insert:implementer"} # диагноз готов, раннер = approver
    failed:            {base: failed, action: abort}
  explorer:
    done:    {base: done,   action: next}
    partial: {base: done,   action: next}                        # re-macho
    failed:  {base: failed, action: abort}
  docs-writer:
    done:   {base: done,   action: next}
    no-op:  {base: done,   action: next}                         # issue-loop-github-strict
    failed: {base: failed, action: abort}
  wiki-keeper:
    done:      {base: done, action: next}
    done-noop: {base: done, action: next}
  pr-shepherd:
    merged-stamped:     {base: done,    action: next}
    verified-stamped:   {base: done,    action: next}
    preflight-failed:   {base: failed,  action: "loop:@next"}    # next: implementer | docs-writer
    delivery-failed:    {base: failed,  action: "loop:@next"}
    local-tests-failed: {base: failed,  action: "loop:@next"}
    squash-incomplete:  {base: failed,  action: abort}           # уже после merge — только отчёт
    blocked-external:   {base: blocked, action: escalate}
    blocked-*:          {base: blocked, action: triage}
  auditor: &audit
    complete:   {base: done,    action: next}
    incomplete: {base: failed,  action: "loop:@audited"}
    blocked:    {base: blocked, action: escalate}                # переопределяет universal
  auditor-deep: *audit
  # --- gates ---
  north-star-auditor:
    aligned:    {base: done,    action: next}
    support-ok: {base: done,    action: next}
    misaligned: {base: blocked, action: escalate}
  plan-reviewer:
    approved:         {base: done,   action: next}
    changes-required: {base: failed, action: "loop:planner"}
  evidence-auditor:
    valid:        {base: done,   action: next}
    insufficient: {base: failed, action: triage}
    invalid:      {base: failed, action: triage}
  # --- встроенный агент Claude Code (судьи evaluator'а) ---
  general-purpose:
    judged: {base: done, action: next}
  # --- overlay: std-исполнители ---
  adb-driver: *std
  cargo-manager: *std
  cargo-runner: *std
  ci-devops: *std
  cmake-runner: *std
  conan-manager: *std
  emulator-driver: *std
  init-cpp: *std
  init-fastapi: *std
  init-kmp: *std
  init-kotlin-jvm: *std
  init-rust: *std
  pnpm-manager: *std
  simulator-driver: *std
  spm-manager: *std
  uv-manager: *std
  vite-runner: *std
  xcodegen-driver: *std
  entitlements-parser: *std
  hypothesizer: *std
  report-writer: *std
  # --- test-runner'ы ---
  gradle-runner: &runner
    passed: {base: done,   action: next}
    failed: {base: failed, action: "loop:implementer"}
  pytest-runner: *runner
  playwright-runner: *runner
  vitest-runner: *runner
  xcode-runner: *runner
  # --- чекеры ---
  ktlint-checker: &lint
    clean:      {base: done,   action: next}
    violations: {base: failed, action: "loop:implementer"}
    error:      {base: failed, action: abort}
  ruff-checker: *lint
  eslint-checker: *lint
  swiftlint-checker: *lint
  mypy-checker: &typecheck
    clean:  {base: done,   action: next}
    errors: {base: failed, action: "loop:implementer"}
    error:  {base: failed, action: abort}
  tsc-checker: *typecheck
  clang-tidy-checker:
    pass:       {base: done,   action: next}
    violations: {base: failed, action: "loop:implementer"}
    error:      {base: failed, action: abort}
  clippy-checker:
    clean:    {base: done,   action: next}
    warnings: {base: failed, action: "loop:implementer"}
    error:    {base: failed, action: abort}
  detekt-checker:
    clean:  {base: done,   action: next}
    smells: {base: failed, action: "loop:implementer"}
    error:  {base: failed, action: abort}
  rustfmt-checker:
    clean: {base: done,   action: next}
    drift: {base: failed, action: "loop:implementer"}
    error: {base: failed, action: abort}
  sanitizer-runner:
    clean:       {base: done,   action: next}
    diagnostics: {base: failed, action: "loop:implementer"}
    error:       {base: failed, action: abort}
  miri-checker:
    clean:         {base: done,    action: next}
    ub:            {base: failed,  action: "loop:implementer"}
    error:         {base: failed,  action: abort}
    not-installed: {base: blocked, action: escalate}
  # --- отладчики ---
  lldb-driver:
    done:    {base: done,   action: next}
    crashed: {base: done,   action: next}                        # краш пойман, это результат
    timeout: {base: failed, action: abort}
    failed:  {base: failed, action: abort}
  # --- stop-list tool-агенты ---
  alembic-manager:
    done:              {base: done,    action: next}
    awaiting-approval: {base: blocked, action: escalate}         # БД вне репо = стоп-лист
    failed:            {base: failed,  action: abort}
  testflight-shipper:
    done:              {base: done,    action: next}
    awaiting-approval: {base: blocked, action: escalate}         # публикация = стоп-лист
    failed:            {base: failed,  action: abort}
  # --- issue-loop-github-strict ---
  integration-gate:
    green:                             {base: done,    action: next}
    transient-recovered:               {base: done,    action: next}
    reproduced-handoff-to-implementer: {base: failed,  action: "loop:implementer"}
    bootstrap-failed:                  {base: blocked, action: escalate}
  spec-maintainer:
    synced: {base: done,   action: next}
    no-op:  {base: done,   action: next}
    failed: {base: failed, action: abort}
  # --- re-macho ---
  intake:
    done:          {base: done,    action: next}
    blocked-legal: {base: blocked, action: escalate}
    blocked-scope: {base: blocked, action: escalate}
    failed:        {base: failed,  action: abort}
  unpacker:
    done:                 {base: done,    action: next}
    blocked-encrypted:    {base: blocked, action: escalate}
    blocked-missing-tool: {base: blocked, action: escalate}
    failed:               {base: failed,  action: abort}
  hopper-launcher: &needs_tool
    done:                 {base: done,    action: next}
    blocked-missing-tool: {base: blocked, action: escalate}
    failed:               {base: failed,  action: abort}
  otool-runner: *needs_tool
  class-dump-runner:
    done:         {base: done,    action: next}
    missing-tool: {base: blocked, action: escalate}
    failed:       {base: failed,  action: abort}
  frida-instrumentor:
    done:                   {base: done,    action: next}
    blocked-frida-detected: {base: blocked, action: escalate}
    failed:                 {base: failed,  action: abort}
  lldb-attach:
    done:                {base: done,    action: next}
    crashed:             {base: done,    action: next}
    blocked-sip:         {base: blocked, action: escalate}
    blocked-entitlement: {base: blocked, action: escalate}
    failed:              {base: failed,  action: abort}
  verifier:
    done:    {base: done, action: next}
    partial: {base: done, action: next}   # REFINED-петля остаётся в `next:` самого verifier
```

У `zcode-harness/{cargo-runner,reference-explorer,spec-reader}.md` нет `return_format`, реестр их не
покрывает. Для doctor это не ошибка: это не роли, §D3.

Семантика base-enum (вписывается комментарием в шапку файла):
- `done` — цель шага достигнута, результат принят, в том числе с некритичными правками;
- `failed` — рабочий продукт дефектен или шаг не выполнен, дальше решает action (петля или обрыв);
- `blocked` — нужно решение вне полномочий агента и раннера.

Инвариант с score, проверяется тестом (§D6):
`telemetry.yaml: review_block_verdicts` == {t ∈ `roles.reviewer` : base ∈ {failed, blocked}} =
`{block, changes-requested, awaiting-approval, failed, blocked}`. Сейчас в списке нет
`awaiting-approval` и `failed`, хотя оба возвращают работу implementer'у. Их добавляем и в
`telemetry.yaml`, и в `score.Defaults()` (`config.go:61`, там стоит комментарий «mirrors»).

### D2. `task-runner.md`: раздел «Правила диспатча» и «Аудит шагов»

Правка идёт в `profiles/base/agents/task-runner.md` и `.claude/agents/task-runner.md` одним
коммитом. `diff` между ними должен остаться пустым.

1. **`:140-161` «Правила диспатча».** Пункты `:146` (failed обрывает) и `:150-152` (tester failed)
   заменяются подразделом `### Вердикты` со следующим содержимым:
   - Ссылка: «Реестр вердиктов — `verdicts` в `.agentlog/schema.json` (источник:
     `profiles/base/verdicts.yaml`). Роль = имя агента без stack-суффикса (`reviewer-rs` →
     `reviewer`)».
   - Словарь действий: таблица из §D1 и правило «после внепланового implementer — tester».
   - Сжатая таблица маппинга по семействам ролей. Строк должно быть не больше 20. Полный реестр
     остаётся в `schema.json`, раннер открывает его только для роли, которой нет в таблице:

     | Роль | Токен → действие |
     |---|---|
     | любая | `blocked` → triage |
     | исполнители (planner, architect, implementer, refactor-agent, explorer, docs-writer, frontend-developer, groomer, wiki-keeper, init-*, *-manager, *-driver) | `done`/`fixed`/`no-op`/`done-noop`/`partial` → next · `failed` → abort |
     | tester, *-runner с `passed` | `passed`/`done` → next · `failed` → loop:implementer |
     | чекеры | `clean`/`pass` → next · `violations`/`errors`/`warnings`/`smells`/`drift`/`diagnostics`/`ub` → loop:implementer · `error` → abort · `not-installed`/`missing-tool`/`blocked-<tool>` → escalate |
     | reviewer | `approve`/`done` → next · `approve-with-fixes` → insert:implementer · `block`/`changes-requested`/`awaiting-approval`/`failed` → loop:implementer |
     | bug-hunter | `awaiting-approval` → insert:implementer |
     | pr-shepherd | `merged-stamped`/`verified-stamped` → next · `preflight-failed`/`delivery-failed`/`local-tests-failed` → loop:@next · `squash-incomplete` → abort · `blocked-external` → escalate · `blocked-*` → triage |
     | alembic-manager, testflight-shipper | `awaiting-approval` → escalate (стоп-лист: БД вне репо / публикация) |
     | auditor, auditor-deep | см. «Аудит шагов» |
     | гейты | см. «Гейты» (`:115-138`, токены там уже совпадают с реестром) |
     | integration-gate, spec-maintainer, re-macho | по реестру |
   - **Без human-gate** (решение владельца: auto-merge везде). `awaiting-approval` не означает
     «спросить человека». Для reviewer раннер сам становится approver'ом: в задание implementer'у
     идут все Critical и Important из `artifact`, после чего следует повторное ревью (loop). Для
     bug-hunter отчёт из `artifact` сразу становится заданием implementer'у. `escalate` для
     alembic-manager и testflight-shipper — не новый гейт, это существующий `## Stop list`
     (БД вне репо, публикация).
   - Правило non-schema заменяет `:153-154`: «Ответ **не схема**, если первая содержательная
     строка — не `verdict: <token>` **или** `<token>` не входит в допустимые токены роли (реестр +
     `blocked`). Сделай один повтор диспатча: потребуй только схему и приведи список допустимых
     токенов. Второй сбой — `verdict: failed`. Повтор списывается из общего бюджета». Если в
     `schema.json` нет `verdicts` (проект применён до #20), допустимым считается enum из
     frontmatter `.claude/agents/<agent>.md`, а действия берутся из таблицы.
   - Счётчик кругов звучит так: «`loop` — не больше 3 кругов на пару (текущий шаг, цель). Это
     обобщение прежних "трёх кругов tester↔implementer" на reviewer↔implementer,
     pr-shepherd↔implementer и чекер↔implementer. Каждый диспатч в круге списывается из **общего**
     лимита `runner.max_dispatches` (`## Бюджет`), отдельного бюджета у кругов нет. Не сошлось за
     3 круга — `verdict: blocked` с историей попыток. Исчерпан бюджет — формат из `## Бюджет`».
2. **`## Бюджет` `:190-192`.** Абзац переписывается ссылкой на action `loop` («все `loop:*`- и
   `insert:*`-диспатчи расходуют этот же счётчик»). Новый ключ не вводится, дефолт 14 остаётся
   прежним. Формулу `7 + 3*2 + 1` не меняем: reviewer-круги конкурируют за тот же лимит, и так и
   задумано.
3. **«Аудит шагов» `:266-275`.** Везде `completion:` → `verdict:`: `verdict: complete` +
   `integrity: clean`, `verdict: incomplete`, `verdict: blocked`. Действия не меняются и
   совпадают с реестром: `complete → next`, `incomplete → loop:@audited`, `blocked → escalate`.
   Шаг 2 (`verdict != done → обрабатывай как обычно`) остаётся как есть и отсылает к подразделу
   «Вердикты».

### D3. Doctor: `checkAgentVerdicts`

Новая функция стоит рядом с `checkAgentFrontmatter` и вызывается в `Diagnose` пунктом 17 после
`checkAgentFrontmatter`. `checkAgentFrontmatter` не расширяется: ей нужен только `projectDir`, её
контракт — «parse + name + наличие return_format», и её тесты остаются нетронутыми. Общий разбор
файла выносится в хелпер `parseAgentFile(path) (fm map[string]any, body []byte, bodyStartLine int, err error)`.
Им пользуются обе функции.

Логика живёт в **новом пакете `cli/internal/verdicts`**. Doctor, apply и repo-тест вызывают одну
и ту же чистую функцию:

```go
package verdicts

type Spec struct{ Base, Action string }            // Action: "next" | "loop:implementer" | ...
type Registry struct {
    Version   int
    BaseEnum  []string
    Universal map[string]Spec
    Quotes    map[string][]string                  // "*" = any registry token
    Roles     map[string]map[string]Spec           // anchors resolved by yaml.v3
}
type Contract struct{ Key string; Tokens []string; Line int } // "<…>" → wildcard, "x-<…>" → "x-*"
type Finding struct{ Line int; Msg string }

func Parse(data []byte) (*Registry, error)         // strict: unknown top-level keys → error
func Load(path string) (*Registry, error)
func (r *Registry) Validate() error                // base∈base_enum, action∈actions, loop/insert target ∈ roles∪{@next,@audited}, quotes ⊆ all tokens
func (r *Registry) Lookup(agentName string) (role string, ok bool) // strip "gates/"; exact, else longest "<role>-" prefix
func (r *Registry) Allows(role, token string) bool // exact, else longest "x-*" pattern, else universal
func (r *Registry) Normalized() Normalized         // for schema.json, §D4
func ParseContract(returnFormat string) (Contract, error)
func BodyTokens(body []byte) []BodyToken           // {Token, Line}
func CheckAgent(r *Registry, agentName string, fm map[string]any, body []byte, bodyStartLine int) []Finding
```

`ParseContract` повторяет алгоритм guard §6. Пустые строки и строки, начинающиеся с `#`,
пропускаются. Первая содержательная строка должна совпадать с
`^\s*([A-Za-z_]+):\s*(.+?)\s*$`. Значение делится по `|`, каждый токен обрезается по пробелам:
в gates встречается запись `valid | insufficient | invalid`.

`BodyTokens`: regex ``\bverdict:\s*`?([a-z][a-z0-9|*-]*)`` по строкам тела. Захват делится по
`|`, завершающие `-` обрезаются, `blocked-*` остаётся шаблоном, `<…>` не захватывается.

`CheckAgent` возвращает error-находки:
- **(a0)** Ключ контракта не `verdict`. Сообщение:
  `return_format must start with "verdict:", got "completion:"`.
- **(a)** Токен из frontmatter не проходит `Allows(role, token)`. Сообщение:
  `token "x" of role "r" is not in verdicts.yaml`. Шаблон `x-*` из frontmatter должен быть в
  реестре дословно.
- **(b)** Токен из тела не входит ни в enum *этого* frontmatter (с учётом шаблонов), ни в
  `quotes[role]`. Сообщение: `body line N: "verdict: x" is outside this agent's return_format enum`.
  Сверка идёт с frontmatter, а не с реестром: так ловится ровно AC4 (`:10` против `:25`), а
  guard фазы 1 согласован с тем, что агент пишет в теле.

Поведение в doctor:
- Реестр грузится из `filepath.Join(repoDir, "base", "verdicts.yaml")`. Нет файла → один `warn`
  (checkout старше #20), проверка пропускается. Ошибка `Parse` или `Validate` → `error` по пути
  реестра, проверка пропускается.
- По каждому `.claude/agents/**/*.md` с непустым `return_format`:
  - `Lookup(name)` не нашёл имя, но `agents.RoleOf(name) != ""` → `error` «role missing in
    verdicts.yaml».
  - Не нашёл и это не роль (tool-агент, пользовательский агент) → молча пропустить. Правило
    doctor'а: «a user's own agent is none of doctor's business».
  - Нашёл → каждая находка `CheckAgent` становится `Issue{Level: LevelError, Path: path}`.
- Отдельный `warn`: в `.agentlog/schema.json` нет ключа `verdicts` → «перезапусти `zprof apply`
  (или `--telemetry-only`)».

Тесты — table-driven в `diagnostics_test.go` на фикстурах: чистый агент, неизвестный токен (a),
`verdict: blocked` в теле вне enum (b), `completion:` (a0), namespaced `reviewer-rs`,
пользовательский агент вне реестра (нет issue). Unit-тесты `internal/verdicts` покрывают
`ParseContract` (комментарии, пробелы, `<…>`, `x-<…>`), `Lookup` (exact, longest prefix,
`gates/`, `auditor-deep-ios` → `auditor-deep`), `Allows` (шаблон, universal) и `Validate`.

### D4. Деплой: вариант (b), слияние в `.agentlog/schema.json` под ключом `verdicts`

**Выбор: (b).** Исходник остаётся отдельным файлом `profiles/base/verdicts.yaml` со своим
`version`, своим loader'ом и своими тестами. В проект он **не** деплоится отдельно: при рендере
`schema.json` его нормализованная форма ложится в тот же JSON под ключом верхнего уровня
`verdicts`.

Обоснование:
1. Все runtime-читатели контракта zprof уже читают `schema.json`: коллектор
   (`_load_pattern_list`, `zprof-collect.py:1507-1531`), score (`LoadConfig`), рендерер guard
   (§8.1 подставляет значения из телеметрии). Один файл — одна единица свежести и одна проверка
   doctor на staleness. Путь `--telemetry-only` (ADR 0001) повезёт реестр без нового кода. С
   отдельным файлом пришлось бы расширять `DeployTelemetry`, doctor и тест e2e.
2. Инвариант `review_block_verdicts` ↔ `verdicts.roles.reviewer` (§D1) и exempt-роли проверяются
   в пределах одного документа. Позже P5 можно будет выводить прямо из `verdicts` одной правкой
   `score/config.go`.
3. Раздельное версионирование и тестирование сохраняется на уровне исходника: у `verdicts.version`
   своя версия, независимая от `telemetry.version`. Смешивание происходит только в рендере.
4. Python YAML всё равно не читает, так что JSON-деплой обязателен в обоих вариантах. Второй
   JSON-файл ничего бы не упростил.

Для guard (#26 фаза 2) это значит: рендерер `guard.json` берёт реестр из `base.Verdicts` в
памяти, как и `$mutating_bash_patterns`. Python-скрипту guard, если ему понадобится runtime-чтение,
хватит `json.load(".agentlog/schema.json")["verdicts"]`. Это решение цитируется в PR-описании для #26.

Точная реализация:
- `overlay.Base` получает поле `Verdicts []byte`. `LoadBase` читает `verdicts.yaml` как
  опциональный файл, по тому же шаблону, что `telemetry.yaml` (`loader.go:197-200`):
  `os.IsNotExist` → nil, иначе ошибка `fmt.Errorf("read verdicts.yaml: %w", err)`.
- В `apply/collector.go` `yamlToJSON(data)` превращается в
  `renderSchema(telemetry, verdictsYAML []byte) ([]byte, error)`:
  1. `yaml.Unmarshal(telemetry, &v)`. Результат должен быть `map[string]any`, иначе
     `fmt.Errorf("telemetry.yaml: top level is not a mapping")`.
  2. Если `len(verdictsYAML) > 0`: `reg, err := verdicts.Parse(verdictsYAML)` →
     `reg.Validate()` → если в `v` уже есть ключ `"verdicts"`, вернуть
     `fmt.Errorf("telemetry.yaml must not define top-level key %q", "verdicts")` → иначе
     `v["verdicts"] = reg.Normalized()`. Ошибки оборачиваются как
     `fmt.Errorf("render verdicts into schema.json: %w", err)`. Apply падает (fail-closed):
     битый реестр — авторская ошибка в zprof, её ловит repo-тест раньше пользователя.
  3. `json.MarshalIndent(v, "", "  ")` + `\n`, как сейчас.
- `deployCollector` вызывает `renderSchema(base.TelemetrySchema, base.Verdicts)` внутри текущей
  ветки `if len(base.TelemetrySchema) > 0`. Реестр без telemetry не деплоится. Сигнатуры
  `deployCollector` и `DeployTelemetry` не меняются: реестр ключуется по имени агента и знание
  overlay'ев ему не нужно.
- `Normalized` — форма для потребителей: универсальные токены влиты в каждую роль, anchors и
  `templates`/`quotes` убраны:

  ```json
  "verdicts": {
    "version": 1,
    "base_enum": ["done", "blocked", "failed"],
    "roles": {
      "reviewer": {"approve": {"base": "done", "action": "next"},
                   "blocked": {"base": "blocked", "action": "triage"}, "...": {}},
      "pr-shepherd": {"blocked-*": {"base": "blocked", "action": "triage"}, "...": {}}
    }
  }
  ```

  Правила поиска для Python-потребителей фиксируются в doc-комментарии `Normalized`. Роль: точное
  имя, иначе самый длинный префикс `<role>-`, `gates/` отрезается. Токен: точный ключ, иначе
  самый длинный `x-*`.
- Тесты `apply/collector_test.go`: в `schema.json` есть `verdicts.roles.reviewer.approve`; без
  `verdicts.yaml` получается прежний вывод байт-в-байт; коллизия ключа даёт ошибку; невалидный
  реестр даёт ошибку.

### D5. AC4: шесть overlay-reviewer'ов. `blocked` **добавляется в enum**

Файлы: `profiles/overlays/{backend-python,frontend-web,ios-swift,kotlin-multiplatform,systems-cpp,systems-rust}/agents/reviewer.md`.
Строка `:10` становится `verdict: block|approve-with-fixes|approve|awaiting-approval|blocked`.
Строку `:25` не трогаем.

Решение одно для всех шести. Почему не замена `:25` на токен из enum:
1. Отказ от self-review — это «не могу выполнить шаг», то есть base `blocked`. `block` означает
   «код дефектен» и по реестру уведёт в `loop:implementer`, то есть отправит implementer'а чинить
   код, в котором дефекта нет. `awaiting-approval` тоже не подходит: это «есть находки».
   Легитимного токена для этой ситуации в текущем enum нет.
2. Guard §5.7 при любом deny велит агенту «верни `verdict: blocked`». Фаза 1 guard проверяет
   ответ по enum из frontmatter (§6). Без `blocked` в enum guard заблокирует собственную
   инструкцию.
3. `blocked` уже есть в `universal` реестра, в `review_block_verdicts` и в enum reviewer'а
   `backend-kotlin-jvm`.

Для #26 из этого следует: валидатору стоит считать `universal`-токены допустимыми для любой роли
(сейчас их нет в enum у чекеров, `re-macho` и гейтов). В рамках #20 эти enum'ы не расширяются.

### D6. AC5: аудиторы переходят на `verdict:`, коллектор и score

- `auditor.md` и `auditor-deep.md` правятся в `profiles/base/agents/` и `.claude/agents/`, пары
  синхронно, `diff` пустой. Во всех местах `completion:` → `verdict:`: `:11` (комментарий
  CRITICAL), `:12` (enum), `:89-91`, `:115`. Токены `complete|incomplete|blocked` остаются как
  есть. `:37` (`verdict: done` исполнителя) законен через `quotes`.
- task-runner: §D2 п.3.
- **Коллектор: код не меняется.** Это подтверждено чтением `_class_a_checks`
  (`zprof-collect.py:1648-1668`): функция ищет первую строку `verdict:` в любом месте ответа и
  берёт первое слово. Ответ аудитора `verdict: complete\nintegrity: clean\n…` даст
  `return_parsed=True`, `has_preamble=False`, `verdict_value="complete"`, и `norm["verdict"]`
  подхватится через `:1793-1794`. Нужен регрессионный тест в
  `profiles/base/tests/test_normalization.py`, в стиле существующих тестов `_class_a_checks`:
  ответ аудитора нового формата даёт `return_parsed is True`, `verdict == "complete"`; ответ в
  старом формате `completion:` даёт `return_parsed is False` (фиксирует, что legacy-алиаса нет
  намеренно).
- **Score и exempt.** Причина exempt («contract does not start with `verdict:`») исчезает, а
  doctor (a0) теперь запрещает не-`verdict` контракты. Поэтому:
  - `telemetry.yaml`: `verdict_exempt_roles: []`, комментарий «пусто с #20: doctor гарантирует
    `verdict:` у всех ролей; механизм оставлен для совместимости». `review_block_verdicts:
    [block, changes-requested, awaiting-approval, failed, blocked]` (§D1).
  - `score/config.go` `Defaults()`: `ExemptRoles: map[string]bool{}`, `ReviewBlockVerdicts` —
    те же пять токенов. Пустой список в `schema.json` не перекрывает дефолты (`config.go:112`),
    поэтому пустыми должны быть **и** дефолты, иначе exempt вернётся тихо.
  - Аудиторы по-прежнему не влияют на P5: у P5 фильтр `Role == "reviewer"`
    (`metrics.go:191`), и `blocked` аудитора туда не попадёт. Зато P6 и P7 теперь честно ловят
    преамбулу и неразобранный ответ у аудиторов, ради этого #20 и затевался.
  - `profiles/base/telemetry_test.py:181, 184` — ассерты обновить. Самописный парсер
    `load_schema` должен принять `verdict_exempt_roles: []`. Если не принимает, оставить секцию
    без элементов (заголовок без значения). Implementer проверяет это тестом.
- Тест-инвариант в `internal/verdicts` (repo-тест, корень находится через
  `filepath.Join("..","..","..")`, как в `apply/e2e_test.go:33`):
  1. `review_block_verdicts` из `profiles/base/telemetry.yaml` совпадает с множеством из §D1;
  2. `score.Defaults().ReviewBlockVerdicts` совпадает с ним же;
  3. каждая роль из `verdict_exempt_roles` отсутствует в реестре или её контракт не `verdict:`
     (после #20 список пустой).

### D7. Repo-уровень: «doctor без error на каждом overlay»

Doctor проверяет *проект*, а AC1 и AC4 — про *репозиторий профилей*. Их закрепляет Go-тест
`TestProfilesVerdictsConsistent` в `internal/verdicts`. Он обходит `profiles/base/agents/**/*.md`,
`profiles/overlays/*/agents/*.md` и `.claude/agents/**/*.md`, прогоняет `CheckAgent` с реестром
`profiles/base/verdicts.yaml` и требует ноль находок. Файлы без `return_format` пропускаются. Имя
файла без `.md` должно найтись через `Lookup` (покрытие AC1). Функция та же, что у doctor, так что
расхождение между CI и `zprof doctor` исключено.

### Definition of Done для implementer

- [ ] `profiles/base/verdicts.yaml` по §D1; `Validate()` и `TestProfilesVerdictsConsistent` зелёные.
- [ ] `cli/internal/verdicts` (+ тесты), `overlay.Base.Verdicts`, `renderSchema` в `apply/collector.go`
      (+ тесты), `checkAgentVerdicts` в doctor (+ фикстуры).
- [ ] `task-runner.md` (две копии), `auditor.md` и `auditor-deep.md` (по две копии): `diff` каждой пары пустой.
- [ ] Шесть overlay `reviewer.md` с `|blocked` в `:10`.
- [ ] `telemetry.yaml`, `score/config.go` `Defaults()`, `telemetry_test.py`, тест коллектора.
- [ ] `cd cli && go test ./...` зелёный.
- [ ] `python3 -m pytest profiles/base/tests/ -v` зелёный (и `profiles/base/telemetry_test.py`).
- [ ] `cd cli && go build ./...` + `make install`, затем `zprof apply --telemetry-only` в этом репо
      (ADR 0001): в `.agentlog/schema.json` есть ключ `verdicts`.

Коммиты: `feat(base): verdicts.yaml registry + task-runner mapping (#20)`,
`feat(cli): verdicts package, doctor check, schema.json render (#20)`,
`fix(base): auditors return verdict:, overlay reviewers allow blocked (#20)`.

## Consequences

**Проще:**
- У раннера появляется детерминированная таблица вместо импровизации. «Молчаливый false-done» на
  `approve-with-fixes`/`awaiting-approval` исчезает.
- У аудиторов работает `return_parsed`, их нарушения контракта видны в P6/P7.
- Guard фазы 2 получает готовый нормализованный реестр в `schema.json` и в памяти рендерера.
- Doctor и CI ловят дрейф enum'ов: новый токен в overlay без записи в реестре не попадёт в `main`.

**Сложнее и риски:**
- Новый токен в любом агенте теперь требует записи в `verdicts.yaml`. Это намеренное трение.
- **P5 растёт**: в список входят `awaiting-approval` и `failed`, и у проектов с overlay-reviewer'ами
  score может упасть. Это исправление недоучёта, а не регрессия. Отметить в PR.
- Проекты, где сделан `--telemetry-only` без полного `apply`, получат `verdict_exempt_roles: []`
  при старых аудиторах с `completion:`. Их P6 и P7 будут штрафоваться, пока не пройдёт полный
  `zprof apply`. Проекты без редеплоя остаются согласованными: в старом `schema.json` exempt-список
  прежний.
- Таблица в task-runner.md — сжатая копия реестра, она может разойтись с ним. Защита: для ролей
  вне таблицы раннер читает `schema.json`, doctor и тесты сверяют сам реестр. Проверку «таблица ==
  реестр» не вводим: табличная прошивка промпта хрупкая.
- Точность «enum на overlay» живёт во frontmatter, а не в реестре. Реестр на уровне роли
  допускает объединение токенов: rust-reviewer, вернувший `changes-requested`, пройдёт проверку
  реестра, и раннер маршрутизирует его правильно. Строгость по конкретному файлу держат guard
  фазы 1 и doctor (b).

**Вне скоупа (follow-up):**
- В overlay-reviewer'ах остался текст «waits for the user to pick findings», а bug-hunter'ы в §0
  говорят «do nothing on silence». Это human-gate формулировки, они противоречат auto-режиму.
  Нужен отдельный issue на правку промптов.
- Нормализация `verdict_base` в коллекторе (`ext.verdict_base` из `schema.json`) — отдельный issue.
- Расширение enum'ов чекеров, гейтов и `re-macho` универсальным `blocked` (см. §D5) — в #26.

## Alternatives considered

1. **Реестр по scope'ам** (`base:` + `overlays.<name>.<agent>:` с override, деплой уже
   разрешённой карты по именам применённых агентов). Отвергнуто:
   - это дублирует enum каждого frontmatter (около 130 файлов), который doctor и так пинит;
   - разрешение требует знать активные overlay'и при деплое, а `deployCollector`/`DeployTelemetry`
     работают только с base (ADR 0001). Пришлось бы менять их сигнатуры и путь `--telemetry-only`;
   - по ролям семантика токенов одинакова во всех overlay'ях. Разошёлся только `blocked` у
     reviewer'а (kotlin-jvm — жёсткий блок, шесть overlay'ев — отказ от self-review), и оба случая
     покрывает `triage`.
2. **Отдельный деплой (a), `.agentlog/verdicts.json`.** Отвергнуто по §D4: вторая единица свежести,
   расширение `DeployTelemetry` и doctor, а выигрыша нет: раздельное версионирование сохраняется в
   исходнике.
3. **Сливать реестр прямо в исходник `telemetry.yaml`.** Отвергнуто: `telemetry_test.py` и
   коллектор парсят `telemetry.yaml` самописным парсером плоских списков, вложенные карты его
   сломают. Кроме того, смешались бы два разных контракта в одном авторском файле.
4. **AC4: заменить `verdict: blocked` в теле на `block`.** Отвергнуто по §D5: неверная маршрутизация
   (implementer чинит несуществующий дефект), и guard §5.7 требует `blocked`.
5. **Оставить аудиторов в `verdict_exempt_roles`.** Отвергнуто: exempt прячет ровно тот класс
   ошибок (преамбула, неразобранный ответ), который #20 делает видимым. От P5 аудиторов и так
   защищает фильтр по роли.
6. **Legacy-алиас `completion:` в коллекторе.** Отвергнуто: он увековечил бы второй ключ. Проекты
   со старыми аудиторами согласованы через старый `schema.json` (см. Consequences).
7. **Проверять тело (b) по реестру, а не по frontmatter.** Отвергнуто: AC4-конфликт (`:10` против
   `:25`) не был бы пойман, потому что `blocked` в реестре есть через `universal`.
