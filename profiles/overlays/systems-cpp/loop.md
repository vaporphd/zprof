## Systems / C++20-23 loop extension

Расширение dev-pipeline для системного C++ проекта.

### Trigger phrases
- EN: `next cpp task`, `build cpp`, `run asan`, `profile`, `run under valgrind`, `check ub`
- RU: `следующая задача`, `собери cpp`, `запусти asan`, `профилируй`, `проверь UB`, `под санитайзер`

### Pipeline
Стандартный dev-pipeline. architect/implementer/tester/refactor-agent/bug-hunter/reviewer знают:
- Modern C++ (C++20 concepts, ranges, coroutines; C++23 `std::expected`, `std::print`, deducing `this`)
- RAII + smart pointers (`std::unique_ptr`, `std::shared_ptr`, `std::weak_ptr`); NEVER raw `new`/`delete` в новом коде
- Move semantics (rvalue refs, `std::move`, `std::forward`)
- Templates + concepts (constrained generics; SFINAE — legacy)
- `constexpr` / `consteval` / `constinit` correctness
- CMake 3.28+ (targets, generator expressions, `FetchContent`, `find_package`, modules `CMAKE_CXX_MODULE_STD=1`)
- Conan 2.x (recipes, `conanfile.py`, profiles, `deploy` generator)
- Sanitizers: ASan (default builds), UBSan (default), TSan (for concurrency code), MSan (Linux only, needs instrumented libc++)
- Testing: GoogleTest + GoogleMock, EXPECT/ASSERT hierarchy, parameterized (`INSTANTIATE_TEST_SUITE_P`), typed (`TYPED_TEST_SUITE`)
- Static analysis: `clang-tidy` with `--config-file=.clang-tidy`

### Специальные диспатчи
| Задача | Агент |
|---|---|
| Configure / build / test | `cmake-runner` |
| Управление зависимостями | `conan-manager` |
| Static analysis + fixes | `clang-tidy-checker` |
| Запуск под sanitizer (asan/ubsan/tsan/msan) | `sanitizer-runner` |
| Debug сессия / crash dump | `lldb-driver` |
| Первичный scaffold CMake проекта | `init-cpp` |

### Изоляция — специфичные правила
- **`build/CMakeCache.txt` может быть 5k+ строк** — grep только нужные переменные (`CMAKE_CXX_STANDARD`, `CMAKE_BUILD_TYPE`, `CMAKE_TOOLCHAIN_FILE`).
- **`compile_commands.json`** — тоже большой; парсить jq'ом.
- **Compiler output может быть 20k+ строк** при template ошибках — `cmake-runner` возвращает только first error (обычно самая полезная — остальные каскад) + suggestions from clang.
- **`build/` и `.cache/` НИКОГДА не читаем** — только исходники.
- **Не запускать `rm -rf build`** без явной команды пользователя — теряет кеш ccache/ninja (пересборка 5-30 минут).
- **Не запускать `conan install --force`** — рекурсивная переустановка всех deps.
- **Sanitizer output** тоже может быть большим; парсер отдаёт только first ASan/UBSan/TSan diagnostic + stack trace.
- **Никогда не запускать debug-сессию (`lldb <binary>`) в фоне** — интерактивная.
- **Никогда не коммитить `build/`, `install/`, `.vscode/settings.json`** (последнее — user-local).
