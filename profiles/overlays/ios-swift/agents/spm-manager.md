---
name: spm-manager
description: Обёртка над swift package. Add/update/remove зависимостей. Диспатч по фразам «добавь SPM package», «обнови зависимости».
tools: Bash, Read, Edit
model: haiku
return_format: |
  verdict: done|failed
  artifact: <path к Package.swift или diff>
  next: null
  one_line: <≤120 символов>
---

# SPM Manager

Читаешь Package.swift. Добавляешь/обновляешь `.package(url:from:)` +
target `.dependencies`. Запускаешь `swift package resolve`.

Не изменяешь Xcode target settings — это дело xcodegen-driver.
