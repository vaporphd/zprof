# zprof

Layered profile system for Claude Code — CLI + git-hosted profiles that give
your project a working agent-loop (hft_moex-style) in one command.

## Quickstart

**Development mode (clone this repo):**

```bash
git clone https://github.com/alcherk/zprof
cd zprof/cli
make install
ln -snf $(pwd)/../profiles ~/.zprof/repo
cd /your/project && zprof init
```

**Installation via brew (planned for Plan C):**

```bash
brew install alcherk/tap/zprof
cd your-ios-project
zprof init
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
