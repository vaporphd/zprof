## Android / Kotlin section (для PROJECT_SPEC.md)

### Build & toolchain
- Kotlin: <version from libs.versions.toml>
- AGP: <version from libs.versions.toml>
- Compose BOM: <version> or Compose Compiler: <version>
- JVM target: 17 (default for AGP 8.x)
- JDK for Gradle: 17 (specified in `~/.gradle/config.properties` or `org.gradle.java.home`)

### App metadata
- Application ID: <e.g. com.example.app>
- Min SDK: <e.g. 24 — Android 7.0>
- Target SDK: <e.g. 35 — Android 15>
- Compile SDK: <e.g. 35>
- Signing config: <path to keystore + how to unlock>
- ProGuard/R8 rules: <path to proguard-rules.pro>

### Module layout
- `:app` — application module
- `:core:*` — shared foundations (network, database, ui-kit, testing)
- `:feature:*` — feature modules per domain slice
- `:build-logic` — convention plugins (Gradle Kotlin DSL)

### DI framework
- Hilt / Koin / manual factories — choose ONE, document

### Architecture
- MVI / MVVM / MVP / Clean — choose ONE, document
- Navigation: Compose Navigation / Jetpack Navigation / Cicerone / Decompose

### Persistence
- Room / SQLDelight / DataStore for preferences / raw SharedPreferences (avoid)

### Networking
- Retrofit + OkHttp / Ktor Client
- Serialization: kotlinx.serialization / Moshi / Gson (avoid Gson in new code)

### Async
- Coroutines: yes (mandatory in new code)
- Flow / StateFlow / SharedFlow for reactive streams
- RxJava: legacy only, not for new code

### Testing stack
- Unit: JUnit5 or JUnit4 + kotlin.test + kotlinx-coroutines-test
- Instrumentation: AndroidX Test + Espresso + UI Automator
- Compose UI tests: `androidx.compose.ui.test.junit4`
- Mocking: MockK (kotlin-native), never Mockito for pure-Kotlin code
- Coroutines assertion: Turbine for Flow
- Coverage: Kover (Gradle plugin) or JaCoCo

### CI
- GitHub Actions / GitLab CI / Bitrise / self-hosted
- Emulator: Google APIs image on `x86_64`, hardware acceleration required

### Distribution
- Play Store internal track / Firebase App Distribution / manual APK

### Known caveats
- <e.g. multi-window layout breaks on Samsung DeX>
- <e.g. proprietary MDM strips notification permissions>
