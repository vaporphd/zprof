stack:
  kotlin-android:
    lang: kotlin
    kotlin_version: "2.0.20+"          # adjust to gradle/libs.versions.toml
    jvm_target: "17"                   # AGP 8.x requires JDK 17
    agp_version: "8.5.x+"              # Android Gradle Plugin
    compose_bom: "2024.09.x+"          # Jetpack Compose BOM
    min_sdk: 24                        # adapt from app/build.gradle.kts
    target_sdk: 35
    build_cmd: "./gradlew :app:assembleDebug"
    test_cmd: "./gradlew :app:testDebugUnitTest :app:connectedDebugAndroidTest"
    lint_cmd: "./gradlew ktlintCheck detekt"
    format_cmd: "./gradlew ktlintFormat"
    install_cmd: "./gradlew :app:installDebug && adb shell am start -n <package>/.MainActivity"
    dependency_locking: enabled        # gradle.lockfile per module
    r8_shrinker: full                  # release builds
```
