# iOS Swift Platform Specifics

## Deployment Target

**Minimum:** iOS 17.0  
**macOS (if Catalyst):** macOS 14.0  
**watchOS (if supported):** watchOS 10.0

## Xcode Toolchain

**Xcode Version:** 15.3 or later  
**Swift Version:** 5.9+  
**Minimum Deployment:** Enforced at build time

## Package Management

**Primary:** Swift Package Manager (SPM) only  
**Status:** Full migration to SPM; no legacy CocoaPods dependencies  
**Build System:** Xcode native (no build scripts required)

## Architecture Layer Split

**SwiftUI:**
- New features (post-iOS 17)
- Modern MVVM architecture
- Composition-based views
- Real-time preview support

**UIKit:**
- Legacy features requiring UIView subclassing
- Complex animations requiring CABasicAnimation
- Custom drawing via UIGraphicsView
- Backward compatibility layer

**AppKit (if Mac Catalyst or macOS target):**
- Menu bar integrations (macOS only)
- Advanced window management
- Desktop-specific keyboard shortcuts

## Entitlements

- **App Groups:** `group.com.zprof.shared` (for shared data containers)
- **iCloud Keychain:** Enabled for credential sync
- **Keychain Sharing:** Certificate identities across target variants
- **Background Modes:** Fetch (if needed) + Processing
- **Push Notifications:** Remote notifications certificate configured

## TestFlight Distribution

**Team:** Anthropic App Store Connect account  
**Build Process:** Automated via GitHub Actions  
**Signing:** Automatic code signing with development provisioning profiles  
**Testers:** Internal QA + beta users (TestFlight external testing)

## Continuous Integration

**Primary CI/CD:** GitHub Actions (macOS runners)  
**Build Pipeline:**
- Lint: SwiftLint (strict mode)
- Unit Tests: XCTest (100%+ coverage goal)
- UI Tests: XCUITest
- Archive: xcodebuild archive
- Export: xcodebuild -exportArchive with App Store export options

**Build Configurations:**
- Debug: Full symbol tables, assertions enabled
- Release: Optimization level -Osize, strip symbols
- TestFlight: Use Release configuration with embedded dSYM

**Code Signing:**
- Distribution Certificate: App Store
- Provisioning Profiles: App Store (for TestFlight)
- Key Management: GitHub Secrets (APPLE_DEVELOPER_CERTIFICATE_DATA, etc.)

## Performance & Monitoring

**Crash Reporting:** Sentry + Apple's on-device crash reports  
**Analytics:** Custom event tracking (privacy-first)  
**Profiling:** Xcode Instruments (Time Profiler, Allocations, Core Data)  
**Memory Budget:** < 100 MB baseline (per Apple guidelines)

## Code Quality Standards

**SwiftLint Rules:** Strict (ban implicit unwrapping, force casts, etc.)  
**Type Safety:** No `Any`, prefer protocols  
**Concurrency:** async/await throughout; no DispatchQueue callback chains  
**Testing:** Unit tests for business logic; XCTest + protocol-based mocking (или Cuckoo / Mockable если нужна библиотека)
