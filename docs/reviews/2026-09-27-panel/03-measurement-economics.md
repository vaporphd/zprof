# 03 — Измеримость и экономика (AI observability & evals)

Эксперт панели AI4SDLC · 2026-09-27 · main @ ed75196 · линза: телеметрия, оценка агентов, экономика моделей.

## Вердикт

- **Метрики меряют процесс, а не результат.** Всё, что считается детерминированно (Class-A, P1–P7, pass@1), опирается на self-reported `verdict` агента и форму ответа. Ground truth нет, и дэшборд честно пишет это сам (`cli/internal/stats/render.go:1164-1165` «нет ground truth», `:1135-1139` «нет outcome»).
- **Экономика не в той валюте.** «Токены» суммируются без весов (`cli/internal/stats/report.go:46`). В реальных данных hft_moex 95,4% из 21,5 млрд токенов — `cache_read`, output — 0,3% (подсчёт по `hft_moex/.agentlog/dispatches.jsonl`). Цен в реестре нет (`cli/internal/models/registry.go:8-16`). Ответ на вопрос «окупается ли loop» сегодня не выводится из данных.
- **Решения о тирах приняты на n=1 на вариант.** Методология это признаёт (`docs/reviews/kotlin-jvm-eval/RESULTS.md:463,623`). Как минимум одно решение пришлось откатить через сутки (`d50c10f`, откат xcodegen-driver Haiku→Sonnet).
- **Цикл улучшения работает вручную через shakedown, но не через телеметрию.** Серия KMP shakedown-4…8 → правки контрактов → подтверждение видна в git (`b0da038`, `8f9bbbb`, `e80f5c4`). А в реальных логах `verdict` и `config_hash` не заполнены ни в одной из 3 721 строки трёх проектов, `tool-events.jsonl` отсутствует. Поэтому `zprof score` и drift по `config_hash` пока нечем кормить.
- **Фундамент сильный** (схема v2 с 4 типами токенов, dispatch tree, provenance, false-done от auditor'а, честные «gap»-карточки). Недостаёт одного слоя — **outcome + $**. Без него P1–P7 превратятся в Goodhart-цель.

## Что измеряется, что нет

| Вопрос владельца | Метрика сегодня | Валидна? | Пробел |
|---|---|---|---|
| Стало ли лучше после правки контракта? | drift по `config_hash` (`render.go:1150`) | Нет: `config_hash` пуст в 100% строк (zprof 70, jarvis 468, hft 3183) | Коллектор не пишет hash, нет окон до/после |
| Сколько стоит фича? | сумма 4 типов токенов на роль/сессию (`report.go:46`) | Нет: cache_read весит как output | Нет цен, нет `run_id`→issue/PR, нет `$` |
| Задача реально сделана? | `verdict: done` D₀ → ярусы Ideal/Solid/Lucky (spec `2026-09-26…:118-120`) | Частично: self-report | Нет CI/merge/revert как outcome. Единственный частичный outcome — auditor false-done (`aggregate.go:381-396`) |
| Хорош ли код? | reviewer verdict; Tier-2 судьи 1–5 | Слабо: pass@1 засчитывает `awaiting-approval` (`eval/scoring.go:248-250`) | Нет дефектов после merge, reverts, follow-up fix-коммитов |
| Какой тир нужен роли? | ApT = pass×1e5/tokens (`eval/scoring.go:225-227`) + эвристика evaluator (`profiles/base/agents/evaluator.md:98-104`) | Нет: `expected_ApT_at_lower_tier` «estimated, not measured» (`evaluator.md:106`) | A/B не подключён к диспатчу, нет arm в схеме |
| Где процесс расточителен? | P1–P3 (tool errors, blind retry, re-read) | Да, как диагностика | `tool-events.jsonl` пока нигде нет |
| Сколько ждать результата? | `duration_ms` на dispatch | Частично: заполнен в 53–75% строк | Нет lead time issue→merge |

## Сильные стороны

1. **Схема телеметрии спроектирована на уровне индустрии.** Разделение `model_requested`/`model_resolved`, 4 типа токенов «без totalTokens shortcut», `parent_dispatch_id`/`spawn_depth`, `transcript_captured/truncated`, redaction (`profiles/base/telemetry.yaml:18-56`). По сути это собственная версия OTel GenAI span-атрибутов.
2. **Scorecard нормирует брак, а не объём.** `score = 100 − ΣP_i`, у каждого штрафа есть насыщение, а токены и время в балл не входят (spec `2026-09-26-task-scorecard-design.md:95-107`). `weights_hash` в каждой строке сохраняет сравнимость истории (`:128`). Это прямой ответ на разброс в 30×.
3. **P2/P3 определены аккуратно.** Мутирующее событие сбрасывает память, повтор теста после Edit не штрафуется (`:109-112`). Такой штраф трудно «закрутить» без реальной правки поведения.
4. **Есть зачаток outcome-метрики.** Auditor проверяет среду против acceptance criteria, `FalseDoneRate` агрегируется (`cli/internal/stats/aggregate.go:381-396`, коммит `2e6fd0c`). Это единственная метрика, которая не верит self-report.
5. **Интеллектуальная честность в документах.** Methodology прямо называет стохастичность tester'а (`docs/reviews/ios-model-eval/METHODOLOGY.md:211,219`), Kotlin-отчёт пишет «n=1 is very small statistical basis» (`kotlin-jvm-eval/RESULTS.md:463`), первый Tier-2 отчёт признаёт fallback на одну модель (`docs/reviews/2026-07-18-tier2-eval-52353252/report.md:3`). Второй этап SRE правильно требует pre-registered experiment registry (`docs/superpowers/specs/2026-08-02-agent-sre-second-stage.md:155-181`).
6. **Реальный объём данных есть.** hft_moex: 3 183 dispatch'а за 17 сессий (2026-07-05…09-26). Для калибровки порогов на распределении этого уже достаточно.

## Риски и слабости

1. **Нет outcome, поэтому оптимизируется процесс (Goodhart).**
   *Доказательство:* ярус Ideal = `verdict: done` + балл ≥85 (spec `:118`), `done` пишет сам task-runner. pass@1 = доля self-reported pass-вердиктов (`eval/scoring.go:154-155`).
   *Последствие:* агенту и автору контракта выгодно реже запускать тесты (меньше P1/P4), не перечитывать файлы (P3) и не возвращаться после review (P5). Мнение: P4/P5 штрафуют как раз ту работу по отлову дефектов, которую loop должен делать. Высокий балл при пропущенном дефекте хуже честного Lucky.
2. **Экономика в ложной валюте.**
   *Доказательство:* `Total()` = input+output+cache_read+cache_creation (`stats/report.go:46`). P6 считает «токены (все 4 типа)» (spec `:106`). В hft_moex cache_read = 95,4%. Спека откладывает `$` на фазу 2 (`:24`).
   *Последствие:* при цене cache read порядка 0,1× от input (публичная структура цен Anthropic, мнение о порядке величины) сравнения ролей и моделей «по токенам» искажены в разы. Роль с длинным контекстом и хорошим кэшем выглядит «дорогой», хотя она дешёвая.
3. **Тировые решения статистически не обоснованы.**
   *Доказательство:* матрица из 7 прогонов, по одному на вариант (`ios-model-eval/METHODOLOGY.md:57-67`). Решение `implementer → opus-4-6[1m]` принято по 6–7 прогонам (`:224-225`) и применено в `6a1c899`. Порог evaluator'а «≥5 samples» (`evaluator.md:110`) и правило DOWNGRADE при pass@1 ≥ 0.90 (`:100`): при 5/5 нижняя граница 95% Wilson CI ≈ 0.57, то есть порог 0.90 на n=5 не различим.
   *Последствие:* внутри одной роли p90/p10 токенов 23× (implementer hft_moex), 99× (implementer jarvis), 27× (wiki-keeper jarvis). Эффект смены тира тонет в дисперсии задач. Откат `d50c10f` через сутки после `6a1c899` это подтверждает.
4. **Панель судей — не PoLL.**
   *Доказательство:* «three parallel Sonnet judges» с разными framing'ами (`evaluator.md:3,71`), тогда как PoLL (Verga et al., цитируется в `:203`) опирается на разные семейства моделей. Framing C судит эффективность по токенам (`:77`), и это дублирует Tier-1.
   *Последствие:* корреляция ошибок трёх судей, self-preference, если судят Sonnet-вывод. Калибровки против человеческой разметки нет.
5. **Контур данных фактически не наполнен.**
   *Доказательство:* во всех трёх `.agentlog` `verdict`=0, `config_hash`=0, `ext` пуст, `tool-events.jsonl`/`scores.jsonl`/`runs.jsonl` отсутствуют. `has_preamble` заполнен в 13% строк hft_moex (419/3183).
   *Последствие:* drift-карточка, `zprof score` и P4/P5/P7 на исторических данных не считаются. Первая неделя после установки коллектора ≥0.2 — это и есть весь датасет.
6. **A/B спроектирован, но не подключён.**
   *Доказательство:* `pick-arm` реализован детерминированным хешем (`profiles/base/zprof-collect.py:48-91`), но ни один agent-контракт его не вызывает (grep по `profiles/**/agents` пуст). Поля `arm` в `telemetry.yaml` нет. `ab_experiments` нет ни в одном `.zprof.yaml` проектов.
   *Последствие:* «Model routing и A/B» (`render.go:1157-1161`) остаётся пустой карточкой. Тиры меняются по shakedown-анекдотам.
7. **Shakedown проверяет формат на чужой модели.**
   *Доказательство:* `zprof shakedown --general` по умолчанию идёт на `haiku`, один прогон, проверяет verdict/preamble/artifact (`zprof shakedown --help`).
   *Последствие:* регресс-тест контракта не репрезентативен для sonnet/opus в проде и не даёт pass^k.

## Сравнение с практикой

1. **OpenTelemetry GenAI semconv** (`gen_ai.usage.input_tokens`, `gen_ai.request.model`/`response.model`, agent spans). *Взять:* маппинг `dispatches.jsonl` → OTel-экспорт одной командой, чтобы данные читались любым backend'ом. *Не нужно:* тащить collector/OTLP в рантайм, JSONL + flock достаточно.
2. **Claude Code OTel-метрики** (`claude_code.cost.usage`, `token.usage` по type, `lines_of_code.count`, `commit.count`, `pull_request.count`). *Взять:* `cost.usage` как готовый `$` без собственного прайс-листа и счётчики commit/PR как дешёвый outcome. Мнение: это самый короткий путь к `$` на карточке.
3. **Langfuse / Braintrust scoring.** Scores прикрепляются к trace и бывают трёх видов: human, LLM-judge, code. Датасеты — версионированные. *Взять:* единый `scores`-поток с полем `source ∈ {code, judge, human, ci}` и ручную разметку Алекса («принял / переделал») как золотой стандарт для калибровки судей. *Не нужно:* SaaS.
4. **HAL / Cost-of-Pass.** Метрика — ожидаемая стоимость успешного решения = cost / success rate, Pareto-фронт accuracy×$. *Взять:* ровно это вместо ApT. ApT делит на токены без цены и на self-reported pass.
5. **τ-bench pass^k.** Надёжность = все k повторов успешны. Evaluator упоминает его, но не реализует (`evaluator.md:179`). *Взять:* `shakedown --repeat k` с pass^k на prod-модели, k=3–5 для контрактных регрессий.
6. **AgentLens / trace-level failure attribution.** *Взять:* каузальную атрибуцию «какой шаг утопил run». P1–P7 by_role уже близки к этому, остаётся добавить outcome-метку на run.
7. **DORA / change failure rate.** *Взять:* revert/fix-коммит в течение N дней после merge PR loop'а как дефект после merge. Это post-merge outcome, который считается из git без LLM.

## Рекомендации

**Сейчас (1–2 недели)**
- Добавить таблицу цен и `$` = Σ tokens_type × price_type на карточку score/stats. *Эффект:* сравнения ролей перестают искажаться 95%-ным cache_read.
- Коллектор пишет `config_hash` и `verdict`, проверить на hft_moex за неделю. *Эффект:* оживают drift-карточка и P4/P5/P7.
- В `zprof score` показывать `$` и `n`, а ярус Ideal давать только при подтверждении auditor'ом. *Эффект:* ярус перестаёт зависеть от self-report.

**Квартал**
- `runs.jsonl` с `run_id → issue/PR/merge_sha` и outcome: CI green, merged, reverted/fix-within-7d, auditor false-done. *Эффект:* появляются `cost per accepted change` = Σ$ runs / число merged без revert и Cost-of-Pass.
- Подключить `pick-arm` к task-runner, писать `arm`/`experiment_id`, primary metric = $/accepted change, guardrail = false-done. Стратифицировать по размеру задачи. *Эффект:* тиры меняются по данным, а не по анекдоту.
- Бутстрэп-CI и минимальный n в evaluator и stats: рекомендация только при непересекающихся интервалах. *Эффект:* меньше откатов вроде `d50c10f`.
- `shakedown --repeat k --model <prod>` с pass^k. *Эффект:* регресс контракта ловится на реальной модели.

**Позже**
- Экспорт в OTel GenAI и Tier-2 судьи из разных семейств, откалиброванные на ручной разметке 30–50 runs. *Эффект:* интероперабельность и измеренная точность судей.
- Гейт по баллу — только после корреляции score↔outcome на ≥100 runs. *Эффект:* гейт не станет Goodhart-целью.

## Вопросы владельцу

1. Что для тебя «успех» задачи: merge, merge без revert за 7 дней, твоя ручная приёмка? Без этого `$/resolved` не определён.
2. Какой бюджет в $/месяц на loop приемлем и с чем сравниваешь: твои часы, подрядчик?
3. Готов ли ты размечать 1–2 run в день («принял / переделал / выбросил») как ground truth для калибровки?
4. Держать ли решение `implementer → opus` до честного A/B, или откатить к дефолту, пока нет данных?
5. Для каких проектов (hft_moex, jarvis) loop — продакшен, а для каких — полигон? От этого зависит, где запускать A/B.
