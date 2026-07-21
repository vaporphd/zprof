## Backend Kotlin JVM section (для PROJECT_SPEC.md)

### Build & toolchain
- Kotlin: <version from gradle/libs.versions.toml>
- JDK toolchain: 21 (via `kotlin { jvmToolchain(21) }`)
- Gradle: <version from gradle/wrapper/gradle-wrapper.properties>
- ktlint plugin: 12.1.1 (jlleitschuh/ktlint-gradle)
- detekt: <version if applied, else "not applied">

### Kind
- library (jar) | application (jar + `main`) | server (Ktor / Spring Boot) | backtest / offline harness

### Module layout
- List every `include(...)` from `settings.gradle.kts`, one line per module with a one-line purpose:
  - `:core-model` — pure-Kotlin domain types; no I/O, no framework
  - `:<feature-module>` — <purpose>
  - `:runner` — composition root; wires the graph, no domain logic
  - `:<app>` — application entry (if applicable)

### Layer taxonomy per module
- `domain/` — `model/`, `error/`, `usecase|service/`, `repository/` (pure Kotlin)
- `data/` — `dto/`, `datasource/`, `mapper/`
- `api/` — HTTP routes (server modules only)
- `di/` — composition root fragment or Koin module

### DI framework
- <none — constructor injection via composition root (overlay default)> | Koin 4.0.0+ | Spring Boot 3.x

### Persistence
- <none — pure logic> | JDBC + HikariCP | Exposed <version> | Spring Data JPA

### Networking
- <none — pure logic> | Ktor Client 3.0.0 | OkHttp 4.12.0 | Java 21 HttpClient (no dep)
- Server-side (if applicable): Ktor Server / Spring Boot / Javalin

### Serialization
- kotlinx.serialization 1.7.3 with SINGLE `Json` instance via DI (see backend-kotlin-jvm implementer §0.12)
- Jackson only if the project is Spring-flavored

### Async
- Coroutines + Flow (mandatory in new code)
- `Dispatchers.IO` available and used at Repository/DataSource boundaries for blocking I/O
- `Dispatchers.Default` for CPU-bound work
- Injected `DispatcherProvider` recommended for testability
- Banned: `GlobalScope`, `runBlocking` outside `fun main()` and test source sets

### Test framework
- JUnit5 (Jupiter) 5.11.3 + Kotest-assertions-core 5.9.1 (`shouldBe`, `shouldThrow`)
- MockK 1.13.13 for mocking (JVM-only, safe on this stack — unlike KMP where MockK fails on iOS/JS)
- Turbine 1.1.0 for Flow assertions
- kotlinx-coroutines-test 1.9.0 for `runTest`
- Coverage: Kover 0.8.x (preferred) or JaCoCo 0.8.12

### Integration gate (opt-in per project)
- Source set: `src/integrationTest/kotlin/**` — registered by root `subprojects { }` block
- Task: `./gradlew integrationTest` — NOT wired into `check`/`build`
- Naming: `<Feature>IT.kt` (uppercase-I-T suffix)
- Exercises: <real DB via Testcontainers | real HTTP staging endpoint | real external binary | real fixture files re-derived from reality>
- Oracle: <where the known-correct expected values live>

### CI / merge gate
- MERGE_GATE: <local-green | CI-green>
- If local-green: `.githooks/pre-commit` runs `ktlintCheck` on staged .kt; `.githooks/pre-push` runs `test`. No CI required.
- If CI-green: GitHub Actions / GitLab CI workflow at `.github/workflows/*.yml`; must be green before merge.
- AUTO_MERGE: <on | off> — controls whether `pr-shepherd` auto-merges approved+green PRs.

### Forbidden imports blacklist (per module)
- `core-model/**` — no `io.ktor.*`, `org.springframework.*`, `java.sql.*`, `okhttp3.*`, `retrofit2.*`, or any other feature module's package.
- Any production — no `kotlinx.coroutines.GlobalScope`, `println`, `System.out.println`.
- Any `**/domain/**` and `**/data/**` — no `runCatching { }` (silently swallows CancellationException — F-15 shakedown).

### Known caveats
- <e.g. "integrationTest hits real MOEX ISS — respects rate limits">
- <e.g. "SQLDelight schema changes require Alembic-style migration ADR">
