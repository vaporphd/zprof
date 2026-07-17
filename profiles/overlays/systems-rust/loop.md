## Systems / Rust loop extension

Расширение dev-pipeline для Rust проекта.

### Trigger phrases
- EN: `next rust task`, `build rust`, `run clippy`, `run miri`, `bench`, `nextest`
- RU: `следующая задача`, `собери rust`, `запусти clippy`, `под miri`, `бенчмарки`

### Pipeline
Стандартный dev-pipeline. architect/implementer/tester/refactor-agent/bug-hunter/reviewer знают:
- Rust 2021 edition + edition 2024 features
- Ownership + borrowing + lifetimes
- Trait system, generics, `impl Trait`, GATs (generic associated types)
- async/await + tokio (or async-std)
- `Result<T, E>` + `?` operator; `anyhow` for apps, `thiserror` for libs
- Serde для serialization; derive macros
- `tracing` для structured logging
- No `unsafe` в новом коде без ADR + Safety comment
- Modules: `mod`, `pub`, `pub(crate)`, `pub(super)`
- Cargo workspaces для multi-crate проектов
- Proc macros: `#[derive(...)]`, custom derive, attribute macros
- Const generics + `const fn`

### Специальные диспатчи
| Задача | Агент |
|---|---|
| build/check/run/test | `cargo-runner` |
| Управление зависимостями | `cargo-manager` |
| Lints | `clippy-checker` |
| Форматирование | `rustfmt-checker` |
| UB / undefined behavior (nightly) | `miri-checker` |
| Первичный scaffold Rust проекта | `init-rust` |

### Изоляция — специфичные правила
- **`Cargo.lock` может быть 5000+ строк** — grep по имени крейта или `cargo tree`.
- **`target/` НИКОГДА не читаем** — только `src/`, `tests/`, `benches/`, `examples/`.
- **`target/doc/` тоже большой** — не читать.
- **Compiler output** может быть большим при type mismatch cascade — `cargo-runner` возвращает only first error + `cargo --explain E<code>` для лучшего понимания.
- **Не запускать `cargo clean`** без явной команды пользователя — теряет весь кеш (пересборка 3-15 минут).
- **Не запускать `cargo update`** без ask — обновит все deps в пределах semver и может сломать build.
- **Miri работает только под nightly** и намного медленнее (~50-200x slowdown); использовать точечно на unsafe-код.
- **`cargo fuzz` требует nightly + отдельная crate `fuzz/`**.
- **Never bare `panic!()` в library-коде** — return `Result::Err`.
- **Никогда не коммитить `target/`, `.cargo/config.toml` с секретами**.
