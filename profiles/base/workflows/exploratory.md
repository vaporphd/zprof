# Exploratory pipeline (RE / analysis)

## Pipeline
`intake → unpack → explore → hypothesize → verify → report`

Выход: markdown-отчёт в `reports/YYYY-MM-DD-<slug>.md`, НЕ PR.

## Параллельные гипотезы
Если hypothesizer возвращает N ≥ 3 гипотез — verifier запускается через
Workflow tool (T4) с parallel-fan-out. Ограничение по умолчанию: 5 гипотез.

## Изоляция
Те же правила что и в dev-pipeline.
