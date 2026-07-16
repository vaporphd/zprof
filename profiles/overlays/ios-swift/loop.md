## iOS Swift loop

Расширение dev-pipeline для iOS проекта.

### Trigger-фразы
- EN: `next ios task`, `build`, `run on simulator`
- RU: `следующая задача`, `собери`, `запусти в симуляторе`, `протести на iPhone`

### Pipeline
Стандартный dev-pipeline, где architect/implementer/tester знают Xcode targets,
Package.swift, SwiftUI vs UIKit vs AppKit layers.

### Специальные диспатчи
| Задача | Агент |
|---|---|
| Обновить SPM зависимости | `spm-manager` |
| Собрать через xcodebuild | `xcode-runner` |
| Запустить симулятор + скриншот | `simulator-driver` |
| Ship в TestFlight | `testflight-shipper` |
| Пересобрать .xcodeproj из yml | `xcodegen-driver` |
| Lint | `swiftlint-checker` |

### Изоляция — специфичные правила
- Никогда не открывать полностью `project.pbxproj` в контекст. Использовать
  `xcodegen-driver` для правок.
- xcodebuild output может быть 10k+ строк — parser tool-агент возвращает
  только first error + last N lines.
