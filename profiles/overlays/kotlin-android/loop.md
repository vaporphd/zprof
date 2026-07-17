## Kotlin / Android loop extension

Расширение dev-pipeline для Android/Kotlin проекта.

### Trigger phrases
- EN: `next android task`, `build apk`, `install on device`, `run compose preview`
- RU: `следующая задача`, `собери apk`, `установи на устройство`, `прогони compose preview`, `запусти на эмуляторе`

### Pipeline
Стандартный dev-pipeline. architect/implementer/tester/refactor-agent/bug-hunter/reviewer знают:
- Jetpack Compose vs XML views
- Coroutines + Flow (structured concurrency, viewModelScope, lifecycleScope)
- Hilt / Koin DI
- Room / SQLDelight persistence
- Retrofit / Ktor Client networking
- Multi-module Gradle с `libs.versions.toml`

### Специальные диспатчи
| Задача | Агент |
|---|---|
| Сборка (assembleDebug / assembleRelease) | `gradle-runner` |
| Установка на устройство / эмулятор | `adb-driver` |
| Запуск эмулятора + скриншот | `emulator-driver` |
| Проверка стиля | `ktlint-checker` |
| Статический анализ | `detekt-checker` |
| Первичный scaffold проекта | `init-android` |

### Изоляция — специфичные правила
- **Никогда не открывать `gradle.lockfile` полностью в контекст** (может быть 5000+ строк). Использовать `grep` по имени зависимости.
- **Никогда не читать сгенерированные Compose-классы** (`ComposableSingletons$*.kt`, `*_HiltComponents.kt`, KSP output под `build/generated/`). Использовать только исходники.
- **Gradle output может быть 20k+ строк** при первой сборке. `gradle-runner` возвращает только first error + last N строк + время сборки.
- **Не запускать `./gradlew clean` без явной команды пользователя** — уничтожает кеш и заставляет пересобирать всё (5-15 минут).
- **`./gradlew --refresh-dependencies`** — тяжёлая операция, только по явной команде.
- **Никогда не коммитить `local.properties`** (содержит `sdk.dir`, часто с абсолютным путём).
