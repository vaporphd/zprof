## Kotlin Multiplatform loop extension

Расширение dev-pipeline для Kotlin Multiplatform проекта (Android + iOS + Desktop + Web).

### Trigger phrases
- EN: `next kmp task`, `build shared`, `build for iOS`, `assemble xcframework`, `install on Android`, `run compose desktop`, `deploy web`
- RU: `следующая задача`, `собери shared`, `собери под iOS`, `запусти на симуляторе`, `собери desktop`, `собери web`, `запусти на эмуляторе Android`

### Pipeline
Стандартный dev-pipeline. architect/implementer/tester/refactor-agent/bug-hunter/reviewer знают:
- Kotlin Multiplatform — commonMain / androidMain / iosMain / desktopMain / webMain source sets
- expect/actual контракты — только в `core/`, никогда в `feature/`
- Compose Multiplatform для Android+Desktop; SwiftUI/UIKit для iOS; Vue/React/Angular для Web
- Decompose Component как источник состояния (не Android ViewModel) — StackNavigation, childStack
- Coroutines + Flow (structured concurrency, coroutineScope от Decompose, SupervisorJob)
- **Koin** DI (единый DSL для всех source sets)
- **SQLDelight** persistence (типизированные SQL-запросы, per-platform DriverFactory)
- **Ktor Client** networking (HttpClient с per-platform engines: OkHttp/Darwin/CIO/Js)
- kotlinx.serialization с ОДНИМ Json instance через DI
- Multi-module Gradle с `libs.versions.toml`, `kotlin("multiplatform")` плагин

### Специальные диспатчи
| Задача | Агент |
|---|---|
| Сборка Gradle (assembleDebug / linkFramework / packageDistribution) | `gradle-runner` |
| Сборка/запуск iOS (Xcode, симулятор) | `xcode-runner` |
| Установка на Android-устройство/эмулятор | `adb-driver` |
| Запуск Android AVD + скриншот | `emulator-driver` |
| Проверка стиля | `ktlint-checker` |
| Статический анализ | `detekt-checker` |
| Первичный scaffold проекта | `init-kmp` |

### Изоляция — специфичные правила
- **Никогда не открывать `gradle.lockfile` полностью в контекст** (может быть 5000+ строк). Использовать `grep` по имени зависимости.
- **Никогда не читать сгенерированные Compose/KSP-классы** (`ComposableSingletons$*.kt`, SQLDelight-generated `<Database>.kt`, Koin-verifier output под `build/generated/`). Использовать только исходники.
- **Gradle output KMP может быть 30k+ строк** при первой сборке всех платформ. `gradle-runner` возвращает только first error + last N строк + время сборки per platform.
- **`linkPodDebugFrameworkIos*` тяжёлая задача** (5-10 минут первый раз) — не запускать без явной команды. `xcode-runner` пропускает если XCFramework не изменился.
- **Не запускать `./gradlew clean` без явной команды пользователя** — уничтожает кеш и заставляет пересобирать все таргеты (10-30 минут).
- **`./gradlew --refresh-dependencies`** — тяжёлая операция, только по явной команде.
- **Никогда не коммитить `local.properties`** (содержит `sdk.dir`, часто с абсолютным путём) и `Podfile.lock` в `iosApp/` (генерируется на CI).
- **`shared.xcframework` в `iosApp/`** — генерируется, gitignored, никогда не редактировать вручную.
