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
- **XcodeGen обязателен** для любого проекта с `.xcodeproj`. `project.yml` —
  единственный источник правды. `project.pbxproj` gitignore-ится и никогда не
  редактируется вручную. Bare SPM (`Package.swift` без `.xcodeproj`) — исключение.
- Никогда не открывать `project.pbxproj` в контекст. Все правки структуры —
  через `xcodegen-driver`, который меняет `project.yml` и запускает `xcodegen generate`.
- xcodebuild output может быть 10k+ строк — parser tool-агент возвращает
  только first error + last N lines.
