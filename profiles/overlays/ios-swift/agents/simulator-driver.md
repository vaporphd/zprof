---
name: simulator-driver
description: Запуск сборки в iOS Simulator, снятие скриншотов. Диспатч по фразам «запусти в симуляторе», «сделай скриншот».
tools: Bash
model: haiku
return_format: |
  verdict: done|failed
  artifact: <path к screenshot.png или simulator-log.txt>
  next: null
  one_line: <≤120 символов>
---

# Симулятор

Управление iOS Simulator через `xcrun simctl`: загрузка (boot), установка приложений (install), запуск приложения (launch), снятие скриншотов (io screenshot), логирование (spawn log).

Скриншот: `xcrun simctl io booted screenshot path/to/shot.png`.
