## Reverse Engineering / Mach-O section (для PROJECT_SPEC.md)

### Legal & scope
- Legal basis: <e.g. "own IP / educational / bug bounty scope / customer NDA">
- Authorized targets: <list of binary names / bundle IDs>
- OUT OF scope: <what NOT to touch>
- Reporting: <who receives final markdown reports>
- Retention: <how long raw dumps + reports kept>

### Target inventory
- <name>-macos.app — macOS application, arch: arm64+x86_64 (fat), signed by <team-id>
- <name>-ios.ipa — iOS app, arch: arm64+arm64e, encrypted (needs jailbreak decrypt)
- <name>-daemon.plist — LaunchDaemon (if applicable)
- <name>.framework — private framework bundled inside app

### Reference workspaces
- `unpacked/` — decompressed binaries (git-ignored)
- `reports/` — final markdown reports (committed)
- `scripts/frida/` — Frida instrumentation scripts (committed)
- `scripts/lldb/` — lldb session scripts (committed)
- `dumps/class/` — class-dump / dsdump outputs (git-ignored, big)
- `dumps/strings/` — strings extraction (git-ignored)
- `dumps/symbols/` — nm / otool -tvV outputs (git-ignored)

### Tooling installed (verify presence)
- `otool`, `nm`, `lipo`, `codesign`, `plutil` — bundled with Xcode Command Line Tools
- `class-dump` — `brew install class-dump` OR built from source (recent Swift support may need fork)
- `dsdump` — https://github.com/DerekSelander/dsdump (better Swift than class-dump)
- `jtool2` — `brew install jtool2` (comprehensive Mach-O)
- `Hopper Disassembler` — commercial, `brew install --cask hopper-disassembler`
- `Ghidra` — free NSA, `brew install --cask ghidra` OR from download
- `Frida` — `pipx install frida-tools` OR `pip install frida-tools`
- `frida-server` — for iOS device: install on jailbroken device via Cydia/Sileo
- `lldb` — bundled with Xcode

### Analysis conventions
- Every hypothesis has a `Verify with: <tool + method>` note
- Every hook: Frida script committed under `scripts/frida/<slug>.js` with header comment (purpose, target, expected output)
- Every lldb session recording: `scripts/lldb/<slug>.lldb` + captured `.log`
- Every finding cross-references binary path + offset + symbol name

### Report template
- `## Executive summary` — 3-5 sentences: what was analyzed + top findings
- `## Scope & method` — target, tools used, timebox
- `## Binary anatomy` — arch, load commands, dylibs, entitlements, code signature
- `## Class / function map` — filtered to relevant subsystem
- `## Hypotheses tested` — table: hypothesis | method | evidence | verdict
- `## Findings` — categorized (arch note, potential vuln, obfuscation observation, algorithm reconstruction)
- `## Reproducer` — steps to reproduce dynamic finding (Frida script path + expected output)
- `## Open questions`
- `## Recommended next steps`

### Known caveats
- <e.g. "iOS binary encrypted with FairPlay — needs jailbroken device with `bagbak`">
- <e.g. "Hopper doesn't handle Swift 5.9 generics well — use dsdump + IDA hybrid">
- <e.g. "Frida on iOS 17+ requires jailbreak — no bypass yet">
