stack:
  systems-cpp:
    lang: cpp
    cxx_standard: "23"                    # C++23; fallback C++20 if toolchain lags
    compiler: "clang++"                   # or g++ (Linux) — detect from CMakeCache
    compiler_min_version: "clang-18 / gcc-13 / MSVC 19.38+"
    build_system: "cmake-3.28+"           # 3.28 needed for C++20 modules
    generator: "Ninja"                    # or "Unix Makefiles" fallback
    package_manager: "conan-2"            # or "vcpkg" — detect from conanfile / vcpkg.json
    test_framework: "GoogleTest 1.15+"    # or Catch2 3.x
    sanitizers: [address, undefined, thread]   # msan requires custom libstdc++
    static_analysis: "clang-tidy-18"
    formatter: "clang-format-18"
    coverage: "llvm-cov (clang -fprofile-instr-generate)"
    debugger: "lldb-18"                   # gdb on Linux if preferred
    profiler: "Instruments (macOS) / perf (Linux) / vtune (Intel)"
    build_cmd: "cmake --build build --parallel"
    configure_cmd: "cmake --preset default"
    test_cmd: "ctest --preset default --output-on-failure"
    lint_cmd: "clang-tidy -p build $(find src -name '*.cpp')"
    format_cmd: "clang-format -i $(find src include -name '*.cpp' -o -name '*.hpp')"
    coverage_cmd: "cmake --build build --target coverage && llvm-cov show ..."
```
