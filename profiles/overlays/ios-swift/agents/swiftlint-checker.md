---
name: swiftlint-checker
description: Запуск swiftlint с strict mode. Возвращает первые N warning'ов. Диспатч по фразам «lint», «проверь стиль».
tools: Bash
model: haiku
return_format: |
  verdict: clean | warnings | errors
  artifact: <path к swiftlint-report.txt>
  next: null
  one_line: <≤120 символов количество violations>
---

# SwiftLint Checker

`swiftlint --strict --reporter markdown > swiftlint-report.txt`.
verdict=clean если 0 issues, warnings если только warnings, errors если строгие.
