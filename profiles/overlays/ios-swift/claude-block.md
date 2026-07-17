stack:
  ios-swift:
    lang: swift
    swift_version: "5.9+"    # адаптируется по Package.swift
    project_manifest: "project.yml"  # XcodeGen mandatory when .xcodeproj is present; project.pbxproj is generated
    regen_cmd: "xcodegen generate"
    build_cmd: "xcodebuild -scheme <SchemeName> -configuration Debug build"
    test_cmd: "xcodebuild test -scheme <SchemeName> -destination 'platform=iOS Simulator,name=iPhone 15'"
    lint_cmd: "swiftlint --strict"
    format_cmd: "swiftformat ."
    # entitlements, provisioning profiles — smoke check
    entitlements_path: "<App>.entitlements"
