stack:
  re-macho:
    target: "mach-o"                    # Mach-O binaries (macOS + iOS)
    binary_kinds:
      - executable                      # PIE .app main binary
      - dylib                           # dynamic library (.dylib)
      - framework                       # .framework bundle
      - kext                            # kernel extension (rare, macOS)
      - bundle                          # loadable bundle (.bundle, .plugin)
    architectures: ["arm64", "arm64e", "x86_64"]  # fat binaries common
    workflow: "exploratory"             # not dev-pipeline
    output_dir: "reports/"              # markdown reports, NOT PRs
    tools:
      static:
        - otool                         # Mach-O headers, load commands, symbols
        - nm                            # symbols
        - lipo                          # thin/fat binary manipulation
        - class-dump                    # Obj-C class definitions from binary
        - dsdump                        # Swift + Obj-C dump (better than class-dump)
        - jtool2                        # community Mach-O tool with entitlements + code signatures
        - strings                       # ASCII+UTF-16 strings extraction
        - MachOView                     # GUI Mach-O viewer (macOS app)
        - Hopper                        # commercial disassembler (recommended)
        - IDA Pro                       # commercial disassembler (alt)
        - Ghidra                        # free NSA disassembler
      dynamic:
        - lldb                          # Apple's debugger (attach live process)
        - Frida                         # JavaScript-based dynamic instrumentation
        - dtrace                        # kernel-level tracing (macOS; requires SIP-disabled or special entitlement)
        - Instruments                   # profiling (built-in Xcode)
        - proc info                     # ps, lsof, sample (built-in)
      unpacking:
        - PackageManager                # .pkg extraction: `pkgutil --expand-full`
        - hdiutil                       # .dmg mount
        - xar                           # legacy .pkg format
        - unzip                         # .ipa extraction (IPA is a zip)
      encryption:
        - fouldecrypt                   # jailbreak-only iOS decrypt
        - clutch                        # legacy iOS decrypt
        - bagbak                        # modern iOS decrypt (needs jailbroken device)
    codesign:
      - codesign_verify: "codesign -dv --verbose=4 <binary>"
      - entitlements: "codesign -d --entitlements :- <binary>"
      - team_identifier: "codesign -dv <binary> 2>&1 | grep TeamIdentifier"
      - hardened_runtime: "codesign -dv <binary> 2>&1 | grep 'flags='"
    swift_specific:
      - swift_demangle: "xcrun swift-demangle <mangled-name>"
      - swift_metadata: "dsdump extracts Swift reflection metadata (~ Swift 5+)"
    boundaries:
      - "READ-ONLY on target binary — never modify, never re-sign"
      - "OUTPUT is markdown report, never PR / code commit"
      - "NEVER attach lldb / Frida to prod system daemons"
      - "NEVER analyze binaries not authorized (respect scope + legal)"
```
