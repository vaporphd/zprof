## Systems / Rust section (для PROJECT_SPEC.md)

### Toolchain
- Rust: stable <e.g. 1.83.0> (from `rust-toolchain.toml`)
- Edition: 2021 (or 2024)
- Cargo: bundled with rustup <version>
- Target(s): <e.g. x86_64-unknown-linux-gnu, aarch64-apple-darwin, wasm32-unknown-unknown>
- MSRV (Minimum Supported Rust Version): <e.g. 1.75>

### Crate structure
- <name>: application binary OR library
- Workspace: yes/no; if yes, member crates: <list>
- Feature flags: <list default features + all optional>
- Optional deps behind features: <mapping>

### Async / concurrency
- Runtime: tokio <version> / async-std / smol
- Channels: tokio::sync (mpsc, oneshot, broadcast, watch) / crossbeam / std::sync::mpsc
- Sync primitives: `std::sync::{Mutex, RwLock, Arc}` / `parking_lot`
- Task cancellation: `tokio_util::sync::CancellationToken`

### Error handling
- App: `anyhow::Result<T>` + `.context(...)` chains
- Library: `thiserror` custom enum errors
- No `unwrap()` / `expect()` in production code (only in main + tests)
- No bare `panic!()` in library code

### Serialization
- `serde` with derive; formats: `serde_json`, `serde_yaml`, `toml`, `bincode`, `postcard`
- Zero-copy: `serde_bytes`, custom `Cow<'_, str>`
- Schema evolution: `#[serde(default)]`, `#[serde(rename = "...")]`, `#[serde(alias = "...")]`

### Logging / tracing
- `tracing` with structured fields; `tracing-subscriber` for output
- Formatters: JSON (prod), pretty (dev)
- Sampling / filtering: `EnvFilter`
- Distributed tracing: `tracing-opentelemetry` → OTLP → Jaeger/Tempo

### HTTP
- Client: `reqwest` (high-level) or `hyper` (low-level)
- Server: `axum` (recommended) / `actix-web` / `rocket` / `warp`
- WebSocket: `tokio-tungstenite` / `axum::extract::ws`

### Persistence
- SQL: `sqlx` (async, compile-time-checked queries) / `diesel` (sync, mature)
- NoSQL: `mongodb` / `redis` / `foundationdb`
- Embedded: `sled` / `rocksdb` (via `rust-rocksdb`) / `heed` / `redb`

### Testing
- Runner: `cargo-nextest` (recommended, 3x faster) / `cargo test`
- Property-based: `proptest` / `quickcheck`
- Snapshot: `insta`
- Async tests: `#[tokio::test]` (with runtime) or `#[tokio::test(flavor = "multi_thread")]`
- HTTP mocks: `wiremock` / `mockito`
- Coverage: `cargo-llvm-cov` (LLVM instrumentation) / `cargo-tarpaulin` (Linux only)

### Benchmarks
- `criterion` (statistical + regression detection) for user-visible perf
- `cargo bench` (nightly) with `#[bench]` for quick micro
- Store baselines in `target/criterion/`; upload artifact in CI

### Quality
- `cargo clippy --all-targets --all-features -- -D warnings` — mandatory in CI
- `cargo fmt --all --check` — CI check
- `cargo audit` — CVE audit (via cargo-audit)
- `cargo deny` — license + duplicate-dep + advisory check
- `miri` (nightly): `cargo +nightly miri test`

### Build
- Release: `cargo build --release --locked` (respect Cargo.lock)
- Cross-compile: `cross build --target <triple>` (via `cross` crate) or add target: `rustup target add <triple>`
- Static binaries (Linux): `RUSTFLAGS='-C target-feature=+crt-static' cargo build --release --target x86_64-unknown-linux-musl`
- WASM: `wasm-pack build --target web --release`

### Deployment
- Docker multi-stage: `rust:1.83-alpine` builder → `alpine:3.20` runner (or `gcr.io/distroless/cc-debian12`); non-root user
- Static musl binary → copy to `scratch` container

### CI
- GitHub Actions: matrix (linux + macos + windows) × (stable + msrv); use `Swatinem/rust-cache@v2` for caching
- Steps: `cargo fmt --check`, `cargo clippy --all-targets --all-features -- -D warnings`, `cargo nextest run --all-features`, `cargo llvm-cov` (Linux only), `cargo build --release --locked`

### Known caveats
- <e.g. sqlx offline mode requires `SQLX_OFFLINE=true` + committed `.sqlx/` cache>
- <e.g. Windows: `openssl-sys` build requires `vcpkg` or `native-tls` with-vendored feature>
