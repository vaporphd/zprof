# Exploratory pipeline (RE / analysis)

Инструкция для `task-runner`, не для main.

## Маршрут

`intake → unpacker → explorer → hypothesizer → verifier → report-writer`

Выход — markdown-отчёт в `reports/YYYY-MM-DD-<slug>.md`, **не** PR.
Артефакты сюда, не в `docs/adr/`: RE-отчёт не архитектурное решение.

## Параллельные гипотезы

`hypothesizer` вернул N ≥ 3 гипотез — запускай `verifier` через Workflow
tool с parallel fan-out. По умолчанию не более **5** гипотез; лимит
переопределяется в `.zprof.yaml`.

## Legal scope

`intake` фиксирует границы разрешённого анализа. Выход за них — стоп-лист:
`verdict: blocked` с вопросом, а не самостоятельное решение.

## Изоляция

Те же правила, что в dev-pipeline: только поля схемы, артефакты точечно.
