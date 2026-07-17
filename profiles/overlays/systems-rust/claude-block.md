stack:
  systems-rust:
    lang: rust
    rust_toolchain: "stable"              # or nightly if project uses unstable features
    rust_version: "1.83+"                 # from rust-toolchain.toml
    edition: "2021"                       # or 2024 (upcoming stable)
    build_tool: "cargo"                   # bundled with rustup
    async_runtime: "tokio-1.41+"          # or async-std (rare), smol
    error_lib: "anyhow + thiserror"       # or plain std::error::Error impls
    log: "tracing"                        # or log + env_logger (legacy)
    serde: "serde-1.0.210+"               # with derive
    http_client: "reqwest"                # or hyper for low-level
    http_server: "axum"                   # or actix-web / rocket / warp
    test_runner: "cargo-nextest"          # 3x faster than cargo test; fallback cargo test
    lint: "clippy (bundled)"
    formatter: "rustfmt (bundled)"
    ub_checker: "miri (via cargo +nightly miri)"
    coverage: "cargo-llvm-cov"            # or cargo-tarpaulin
    bench: "criterion-0.5+"
    fuzz: "cargo-fuzz + libFuzzer"
    build_cmd: "cargo build --release"
    test_cmd: "cargo nextest run --all-features"
    lint_cmd: "cargo clippy --all-targets --all-features -- -D warnings"
    format_cmd: "cargo fmt --all"
    check_cmd: "cargo check --all-targets"
    doc_cmd: "cargo doc --no-deps --document-private-items --open"
```
