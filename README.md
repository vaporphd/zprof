# zprof

Layered profile system for Claude Code — CLI + git-hosted profiles that give
your project a working agent-loop (hft_moex-style) in one command.

## Quickstart

**Development mode (clone this repo):**

```bash
git clone https://github.com/vaporphd/zprof
cd zprof/cli
make install
ln -snf $(pwd)/../profiles ~/.zprof/repo
cd /your/project && zprof init
```

**Installation via brew:**

```bash
brew install vaporphd/tap/zprof
cd your-ios-project
zprof init
```

Requires a one-time bootstrap of the profiles repo (Formula does this into `$(brew --prefix)/share/zprof/profiles`):

```bash
mkdir -p ~/.zprof
ln -s $(brew --prefix)/share/zprof/profiles ~/.zprof/repo
```

## What it does

- Ships stack-aware `.claude/agents/` — 6 workflow roles + tool-agents per stack
- Generates `AGENT_LOOP.md` — the dispatch contract main session reads
- Renders managed blocks in `CLAUDE.md` — your edits outside blocks survive `zprof sync`
- Isolates subagents from main context via 4-tier design (terse handoff → artifact-first → orchestrators → Workflow)
- Resolves model tier aliases (`opus`/`sonnet`/`haiku`) to current exact IDs

## v1 overlays

- ios-swift, android-kotlin, backend-python, frontend-web
- re-macho, systems-cpp, systems-rust

See `docs/superpowers/specs/2026-07-16-zprof-design.md` for the full design.

## Development

```bash
cd cli
make build test
ZPROF_REPO=$PWD/../profiles ./bin/zprof init  # dev mode
```

## Release (maintainer)

Tag a version and push — GitHub Actions runs goreleaser end-to-end (`.github/workflows/release.yml`):

```bash
git tag v0.1.0
git push origin v0.1.0
```

The workflow builds macOS + Linux binaries (arm64 + amd64), publishes a GitHub Release with tarballs + checksums, and pushes an updated Formula to `vaporphd/homebrew-tap`.

**One-time repo secrets required:**

- `HOMEBREW_TAP_TOKEN` — Personal Access Token with `contents:write` on `vaporphd/homebrew-tap` (fine-grained token scoped to that single repo). GITHUB_TOKEN is not enough because the Formula is pushed to a different repository.

**Prerequisite repos:**

- `vaporphd/homebrew-tap` must exist with a `Formula/` directory; empty repo is fine — goreleaser creates `Formula/zprof.rb` on first release.
