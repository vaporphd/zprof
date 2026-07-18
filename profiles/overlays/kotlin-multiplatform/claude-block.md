stack:
  kotlin-multiplatform:
    lang: kotlin
    kotlin_version: "2.0.20+"           # adjust to gradle/libs.versions.toml
    jvm_target: "17"                    # KMP JVM + Android target
    agp_version: "8.5.x+"               # Android target
    compose_multiplatform: "1.7.0+"     # commonMain + androidMain + desktopMain UI
    compose_bom_android: "2024.09.x+"   # Jetpack Compose BOM for androidMain interop
    decompose: "3.1.0+"                 # Component-based navigation for commonMain
    koin: "4.0.0+"                      # DI framework across all source sets
    ktor: "3.0.0+"                      # HTTP client with per-platform engines
    sqldelight: "2.0.2+"                # KMP-native SQL DB with typed queries
    kotlinx_serialization: "1.7.3+"     # single Json instance via DI (see implementer §0.10)
    kotlinx_coroutines: "1.9.0+"        # Dispatchers.Main.immediate, Flow, actor
    kotlinx_datetime: "0.6.1+"          # replaces java.time in commonMain
    kotlinx_collections_immutable: "0.3.7+"
    min_sdk_android: 24                 # androidMain target
    target_sdk_android: 35
    ios_deployment_target: "16.0"       # iosMain target
    macos_deployment_target: "13.0"     # macosMain target (if enabled)
    default_platforms: ["android", "ios", "desktop", "web"]
    build_cmd: "./gradlew build"
    build_android_cmd: "./gradlew :composeApp:assembleDebug"
    build_ios_cmd: "./gradlew :shared:linkPodDebugFrameworkIosSimulatorArm64"
    build_desktop_cmd: "./gradlew :composeApp:packageDistributionForCurrentOS"
    build_web_cmd: "./gradlew :composeApp:jsBrowserDevelopmentWebpack"
    test_common_cmd: "./gradlew :shared:allTests"
    test_android_cmd: "./gradlew :shared:testDebugUnitTest"
    test_ios_cmd: "./gradlew :shared:iosSimulatorArm64Test"
    lint_cmd: "./gradlew ktlintCheck detekt"
    format_cmd: "./gradlew ktlintFormat"
    dependency_locking: enabled         # gradle.lockfile per module
