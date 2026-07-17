## Systems / C++ section (для PROJECT_SPEC.md)

### Toolchain
- C++ standard: <e.g. C++23 with C++20 fallback>
- Compiler: clang++ <version> (recommended) OR g++ <version> OR MSVC 19.x
- Build system: CMake <version, min 3.28 for modules>
- Generator: Ninja (recommended) or Unix Makefiles
- Package manager: Conan 2.x OR vcpkg OR system packages OR git submodules + FetchContent
- Linker: ld.lld (with clang) / mold (Linux, fast) / ld64 (macOS system)
- ccache/sccache for compile cache

### Targets
- <name>-lib: static library / shared library / interface (header-only)
- <name>-app: executable
- <name>-tests: test suite
- <name>-bench: benchmarks (optional)

### CMake presets (CMakePresets.json)
- `default` — Debug + Ninja + Conan toolchain
- `release` — Release with LTO + no sanitizers
- `asan` — Debug + AddressSanitizer + UBSan
- `tsan` — Debug + ThreadSanitizer
- `coverage` — Debug + `-fprofile-instr-generate -fcoverage-mapping`

### Testing
- Framework: GoogleTest 1.15+ (recommended) or Catch2 3.x
- Test binaries: linked via `target_link_libraries(<name>-tests PRIVATE GTest::gtest_main)`
- Discovery: `gtest_discover_tests(<name>-tests)` (CMake integration)
- CTest driver: `ctest --preset default`
- Fuzzing: libFuzzer (`-fsanitize=fuzzer`), AFL++, honggfuzz — for security-critical

### Quality
- Static analysis: clang-tidy 18+ with `.clang-tidy` config
- Formatting: clang-format 18+ with `.clang-format` config
- Sanitizers (per preset): ASan, UBSan, TSan, MSan (Linux + custom libc++)
- Coverage: `llvm-cov show` from clang instrumented binaries
- Docs: Doxygen (optional)

### Deployment
- Static binaries where possible (`-static-libstdc++`, `-static-libgcc`)
- Signing (macOS): `codesign -s <cert> --deep --force`; hardened runtime
- Signing (Windows): signtool.exe with EV cert
- Packaging: CPack (deb, rpm, dmg, msi, tarball) OR system package (apt/dnf/brew)

### CI
- GitHub Actions matrix: ubuntu-latest × macOS-latest × windows-latest × [clang, gcc, msvc]
- Steps: install conan → conan install → cmake configure → build → ctest → asan/tsan runs → upload artifacts
- ccache/sccache cache action for compile cache
- Coverage upload: codecov / coveralls

### Known caveats
- <e.g. macOS Instruments not scriptable — use `xctrace record`>
- <e.g. clang libc++ hardening: `_LIBCPP_HARDENING_MODE=_LIBCPP_HARDENING_MODE_FAST`>
- <e.g. Windows: mind `NOMINMAX` and `WIN32_LEAN_AND_MEAN` in every windows.h include>
