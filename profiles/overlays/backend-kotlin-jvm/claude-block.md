stack:
  backend-kotlin-jvm:
    lang: kotlin
    kotlin_version: "2.0.20+"           # adjust to gradle/libs.versions.toml
    jdk_toolchain: "21"                 # `jvmToolchain(21)` in subprojects
    gradle: "8.9+"
    junit_jupiter: "5.11.3"             # test framework
    kotest_assertions_core: "5.9.1"     # `shouldBe` / `shouldThrow` assertions
    mockk: "1.13.13"                    # JVM-only mock library (safe here, unlike KMP)
    turbine: "1.1.0"                    # Flow assertions
    kotlinx_coroutines: "1.9.0+"
    kotlinx_serialization: "1.7.3+"     # single Json instance via DI (see implementer §0.12)
    kotlinx_datetime: "0.6.1+"
    ktlint_plugin: "12.1.1"             # jlleitschuh/ktlint-gradle
    build_cmd: "./gradlew build"
    test_cmd: "./gradlew test"
    lint_cmd: "./gradlew ktlintCheck"
    format_cmd: "./gradlew ktlintFormat"
    # Optional integration-gate — populated per project. Registered as a `Test`
    # task NOT wired into check/build (real-external-system gate must not run
    # in default lifecycle). Callers invoke explicitly via <INTEGRATION_GATE>.
    integration_gate: "./gradlew integrationTest"   # override per project
    # Optional dependency injection framework — the overlay default is
    # constructor injection via a composition root (no framework). Common
    # additions are Koin 4.0.0+ or Spring Boot 3.x — those are project-owned
    # ADRs, not overlay defaults.
