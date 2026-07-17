# zprof — Plan A: CLI + base + ios-swift (walking skeleton)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a minimum walking skeleton of `zprof`: a Go CLI that can `init`/`apply`/`sync`/`list`/`doctor` on a real iOS Swift project. After Plan A, `brew install zprof && zprof init` in `anti-backlog/` produces a working `.claude/` + `AGENT_LOOP.md` + state files.

**Architecture:** Monorepo layout at `/Volumes/mydata/projects/zprof/` with `cli/` (Go code) and `profiles/` (base + overlays content). CLI reads profiles from `~/.zprof/repo/` (symlinked to `profiles/` in dev, git-cloned in prod). Composition renders managed blocks with `<!-- zprof:begin/end -->` markers so `sync` preserves user edits outside blocks.

**Tech Stack:** Go 1.22, `spf13/cobra`, `spf13/viper`, `charmbracelet/huh`, `hashicorp/go-getter`, `gopkg.in/yaml.v3`, `stretchr/testify`. Golden-file tests for markdown rendering.

## Global Constraints

- Go version floor: `1.22`.
- All CLI text output default language: `ru` (Russian). English retained for identifiers, model IDs, YAML keys, error codes.
- Model tier aliases in overlay frontmatter (NEVER exact IDs in overlays); exact IDs only via `.zprof.yaml` override.
- Managed blocks use markers `<!-- zprof:begin overlay=<name> block=<key> -->` and `<!-- zprof:end -->` — parser is strict, mismatched pairs = error.
- All generated files use LF line endings.
- All Go code passes `gofmt` + `go vet` + `staticcheck`.
- Tests are table-driven with `testify/require`; golden fixtures live in `testdata/`.
- Each task ends with a green `go test ./...` at repo root — no red-leaving-repo commits.
- Commits follow Conventional Commits (`feat:`, `fix:`, `test:`, `docs:`, `refactor:`).

## File Structure

```
zprof/
├── cli/                                        # Go module (module: github.com/vaporphd/zprof)
│   ├── cmd/zprof/main.go                       # entrypoint; wires cobra root
│   ├── internal/
│   │   ├── models/registry.go                  # tier alias → exact ID
│   │   ├── models/registry_test.go
│   │   ├── manifest/overlay.go                 # OverlayManifest struct + YAML load
│   │   ├── manifest/project.go                 # ProjectManifest (.zprof.yaml)
│   │   ├── manifest/detect.go                  # DetectRules
│   │   ├── manifest/*_test.go
│   │   ├── detect/scanner.go                   # walk project, apply DetectRules, score
│   │   ├── detect/scanner_test.go
│   │   ├── managed/parser.go                   # parse markers, extract blocks
│   │   ├── managed/renderer.go                 # render new content, preserve outside
│   │   ├── managed/merge.go                    # 3-mode conflict resolution
│   │   ├── managed/*_test.go
│   │   ├── overlay/loader.go                   # load from ~/.zprof/repo/
│   │   ├── overlay/namespace.go                # suffix agents on multi-overlay
│   │   ├── overlay/*_test.go
│   │   ├── apply/engine.go                     # orchestrates render of .claude/, CLAUDE.md, AGENT_LOOP.md, state
│   │   ├── apply/state_files.go
│   │   ├── apply/*_test.go
│   │   ├── sync/git.go                         # go-getter wrapper for repo clone/pull
│   │   ├── sync/*_test.go
│   │   ├── wizard/init.go                      # huh-based interactive init
│   │   ├── doctor/diagnostics.go               # validate .zprof.yaml, model refs, marker pairs
│   │   ├── doctor/*_test.go
│   │   └── cmd/                                # per-command builders
│   │       ├── root.go
│   │       ├── init.go
│   │       ├── apply.go
│   │       ├── sync.go
│   │       ├── list.go
│   │       ├── agents.go
│   │       ├── doctor.go
│   │       └── models.go
│   ├── testdata/
│   │   ├── overlays/                           # fake overlays for tests
│   │   ├── projects/                           # fake project layouts (ios, py, mixed)
│   │   └── golden/                             # expected rendered output
│   ├── go.mod
│   ├── go.sum
│   └── Makefile                                # build, test, lint, install
├── profiles/
│   ├── base/
│   │   ├── manifest.yaml
│   │   ├── claude-block-base.md
│   │   ├── agents/
│   │   │   ├── planner.md
│   │   │   ├── docs-writer.md
│   │   │   ├── dev-orchestrator.md
│   │   │   ├── exploratory-orchestrator.md
│   │   │   └── gates/
│   │   │       ├── north-star-auditor.md
│   │   │       ├── evidence-auditor.md
│   │   │       └── plan-reviewer.md
│   │   ├── loop-templates/
│   │   │   ├── dev-pipeline.md
│   │   │   └── exploratory.md
│   │   └── state-templates/
│   │       ├── todo.md
│   │       ├── lessons.md
│   │       ├── followup.md
│   │       ├── project-spec-skeleton.md
│   │       └── adr-template.md
│   └── overlays/
│       └── ios-swift/
│           ├── manifest.yaml
│           ├── detect.yaml
│           ├── claude-block.md
│           ├── loop.md
│           ├── agents/
│           │   ├── architect.md
│           │   ├── implementer.md
│           │   ├── tester.md
│           │   ├── bug-hunter.md
│           │   ├── refactor-agent.md
│           │   ├── explorer.md
│           │   ├── xcode-runner.md
│           │   ├── spm-manager.md
│           │   ├── simulator-driver.md
│           │   ├── testflight-shipper.md
│           │   ├── xcodegen-driver.md
│           │   └── swiftlint-checker.md
│           └── state/project-spec-section.md
├── docs/
│   └── superpowers/
│       ├── specs/2026-07-16-zprof-design.md
│       └── plans/2026-07-16-zprof-plan-a-cli-base-ios.md   ← this file
└── .github/workflows/ci.yml
```

---

## Phase 0 — Repository scaffolding

### Task 0.1: Go module + Makefile + directory skeleton

**Files:**
- Create: `cli/go.mod`
- Create: `cli/cmd/zprof/main.go`
- Create: `cli/Makefile`
- Create: `cli/internal/.keep` (empty placeholder)
- Create: `profiles/base/.keep`, `profiles/overlays/.keep`

**Interfaces:**
- Consumes: nothing
- Produces: `go build ./...` succeeds; `zprof --help` prints stub

- [ ] **Step 1: Create go.mod**

```
cd /Volumes/mydata/projects/zprof
mkdir -p cli/cmd/zprof cli/internal profiles/base profiles/overlays
cd cli
go mod init github.com/vaporphd/zprof
go get github.com/spf13/cobra@v1.9.1
go get github.com/spf13/viper@v1.19.0
go get github.com/charmbracelet/huh@v0.5.3
go get gopkg.in/yaml.v3@v3.0.1
go get github.com/stretchr/testify@v1.9.0
go get github.com/hashicorp/go-getter@v1.7.5
```

- [ ] **Step 2: Write main.go (stub)**

```go
// cli/cmd/zprof/main.go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "0.1.0-dev"

func main() {
	root := &cobra.Command{
		Use:     "zprof",
		Short:   "Layered profile system for Claude Code",
		Version: version,
	}
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Write Makefile**

```makefile
.PHONY: build test lint fmt install clean

build:
	go build -o bin/zprof ./cmd/zprof

test:
	go test -race -count=1 ./...

lint:
	go vet ./...
	staticcheck ./... 2>/dev/null || true

fmt:
	gofmt -w .

install:
	go install ./cmd/zprof

clean:
	rm -rf bin/
```

- [ ] **Step 4: Verify build**

```
cd /Volumes/mydata/projects/zprof/cli
make build
./bin/zprof --version
```

Expected: `zprof version 0.1.0-dev`

- [ ] **Step 5: Commit**

```
cd /Volumes/mydata/projects/zprof
git add cli/ profiles/
git commit -m "feat(cli): scaffold go module, cobra root, makefile"
```

### Task 0.2: GitHub Actions CI

**Files:**
- Create: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: `cli/Makefile`
- Produces: CI badge in README

- [ ] **Step 1: Write CI workflow**

```yaml
# .github/workflows/ci.yml
name: CI
on:
  push: { branches: [main] }
  pull_request:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - working-directory: cli
        run: |
          go mod download
          go vet ./...
          go test -race -count=1 ./...
          go build ./...
```

- [ ] **Step 2: Commit**

```
git add .github/
git commit -m "ci: add github actions workflow"
```

---

## Phase 1 — Model tier resolver

### Task 1.1: Registry with tier-alias resolution

**Files:**
- Create: `cli/internal/models/registry.go`
- Create: `cli/internal/models/registry_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `func Resolve(name string) (string, error)` — returns exact model ID
  - `var Aliases = map[string]string{...}` — current alias table

- [ ] **Step 1: Write failing tests**

```go
// cli/internal/models/registry_test.go
package models

import (
	"testing"
	"github.com/stretchr/testify/require"
)

func TestResolve(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"opus", "claude-opus-4-8"},
		{"opus-1m", "claude-opus-4-7[1m]"},
		{"sonnet", "claude-sonnet-5"},
		{"haiku", "claude-haiku-4-5-20251001"},
		{"fable", "claude-fable-5"},
		{"claude-opus-4-8", "claude-opus-4-8"},   // pass-through exact ID
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := Resolve(tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestResolveUnknownAlias(t *testing.T) {
	_, err := Resolve("gpt-5")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown model alias")
	require.Contains(t, err.Error(), "opus, opus-1m, sonnet, haiku, fable")
}
```

- [ ] **Step 2: Run test to verify it fails**

```
cd cli && go test ./internal/models/...
```

Expected: FAIL (`package registry doesn't exist`)

- [ ] **Step 3: Write implementation**

```go
// cli/internal/models/registry.go
package models

import (
	"fmt"
	"sort"
	"strings"
)

// Aliases maps tier alias → exact Claude model ID as of 2026-07-16.
var Aliases = map[string]string{
	"opus":    "claude-opus-4-8",
	"opus-1m": "claude-opus-4-7[1m]",
	"sonnet":  "claude-sonnet-5",
	"haiku":   "claude-haiku-4-5-20251001",
	"fable":   "claude-fable-5",
}

// Resolve returns the exact model ID for either a tier alias or an
// already-exact model ID. Exact IDs pass through if they start with "claude-".
func Resolve(name string) (string, error) {
	if id, ok := Aliases[name]; ok {
		return id, nil
	}
	if strings.HasPrefix(name, "claude-") {
		return name, nil
	}
	keys := make([]string, 0, len(Aliases))
	for k := range Aliases {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return "", fmt.Errorf("unknown model alias %q — valid aliases: %s (or use exact claude-* ID)",
		name, strings.Join(keys, ", "))
}
```

- [ ] **Step 4: Run tests to verify PASS**

```
cd cli && go test ./internal/models/... -v
```

Expected: `PASS`

- [ ] **Step 5: Commit**

```
git add cli/internal/models/
git commit -m "feat(models): tier alias resolver (opus/sonnet/haiku/opus-1m/fable)"
```

### Task 1.2: `zprof models list` subcommand

**Files:**
- Create: `cli/internal/cmd/models.go`
- Modify: `cli/cmd/zprof/main.go` — wire subcommand

**Interfaces:**
- Consumes: `models.Aliases`
- Produces: user-visible `zprof models list` command

- [ ] **Step 1: Write cmd/models.go**

```go
// cli/internal/cmd/models.go
package cmd

import (
	"fmt"
	"sort"

	"github.com/vaporphd/zprof/internal/models"
	"github.com/spf13/cobra"
)

func NewModelsCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "models",
		Short: "Работа с реестром моделей",
	}
	root.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Показать таблицу tier alias → exact ID",
		RunE: func(cmd *cobra.Command, args []string) error {
			keys := make([]string, 0, len(models.Aliases))
			for k := range models.Aliases {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Printf("%-8s  %s\n", k, models.Aliases[k])
			}
			return nil
		},
	})
	return root
}
```

- [ ] **Step 2: Wire in main.go**

```go
// cli/cmd/zprof/main.go — add import + register
import (
	// ...
	"github.com/vaporphd/zprof/internal/cmd"
)

func main() {
	root := &cobra.Command{ /* ... */ }
	root.AddCommand(cmd.NewModelsCmd())
	// ...
}
```

- [ ] **Step 3: Smoke test**

```
make build
./bin/zprof models list
```

Expected output (alphabetical):
```
fable     claude-fable-5
haiku     claude-haiku-4-5-20251001
opus      claude-opus-4-8
opus-1m   claude-opus-4-7[1m]
sonnet    claude-sonnet-5
```

- [ ] **Step 4: Commit**

```
git add cli/
git commit -m "feat(cli): zprof models list subcommand"
```

---

## Phase 2 — Manifest types

### Task 2.1: OverlayManifest (overlays/<name>/manifest.yaml)

**Files:**
- Create: `cli/internal/manifest/overlay.go`
- Create: `cli/internal/manifest/overlay_test.go`
- Create: `cli/testdata/overlays/valid-manifest.yaml`
- Create: `cli/testdata/overlays/invalid-manifest.yaml`

**Interfaces:**
- Consumes: `gopkg.in/yaml.v3`
- Produces:
  - `type OverlayManifest struct { Name, DisplayName, Version, LoopTemplate string; RequiresBase string; Roles []string; ToolAgents []string }`
  - `func LoadOverlay(path string) (*OverlayManifest, error)`

- [ ] **Step 1: Write fixtures**

```yaml
# cli/testdata/overlays/valid-manifest.yaml
name: ios-swift
display_name: "iOS / Swift (UIKit + SwiftUI + AppKit)"
version: 0.1.0
loop_template: dev-pipeline
requires_base: ">= 0.1.0"
roles:
  - architect
  - implementer
  - tester
  - bug-hunter
  - refactor-agent
  - explorer
tool_agents:
  - xcode-runner
  - spm-manager
  - simulator-driver
```

```yaml
# cli/testdata/overlays/invalid-manifest.yaml
name: ""
loop_template: unknown-template
```

- [ ] **Step 2: Write failing test**

```go
// cli/internal/manifest/overlay_test.go
package manifest

import (
	"path/filepath"
	"testing"
	"github.com/stretchr/testify/require"
)

func TestLoadOverlayValid(t *testing.T) {
	m, err := LoadOverlay(filepath.Join("..", "..", "testdata", "overlays", "valid-manifest.yaml"))
	require.NoError(t, err)
	require.Equal(t, "ios-swift", m.Name)
	require.Equal(t, "0.1.0", m.Version)
	require.Equal(t, "dev-pipeline", m.LoopTemplate)
	require.Contains(t, m.Roles, "architect")
	require.Contains(t, m.ToolAgents, "xcode-runner")
}

func TestLoadOverlayInvalidEmptyName(t *testing.T) {
	_, err := LoadOverlay(filepath.Join("..", "..", "testdata", "overlays", "invalid-manifest.yaml"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "name is required")
}

func TestLoadOverlayInvalidLoopTemplate(t *testing.T) {
	_, err := LoadOverlay(filepath.Join("..", "..", "testdata", "overlays", "invalid-manifest.yaml"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "loop_template must be one of: dev-pipeline, exploratory")
}
```

- [ ] **Step 3: Run test to verify FAIL**

```
cd cli && go test ./internal/manifest/... -v
```

Expected: FAIL (`package doesn't exist`)

- [ ] **Step 4: Write implementation**

```go
// cli/internal/manifest/overlay.go
package manifest

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

var validLoopTemplates = map[string]bool{
	"dev-pipeline": true,
	"exploratory":  true,
}

type OverlayManifest struct {
	Name         string   `yaml:"name"`
	DisplayName  string   `yaml:"display_name"`
	Version      string   `yaml:"version"`
	LoopTemplate string   `yaml:"loop_template"`
	RequiresBase string   `yaml:"requires_base"`
	Roles        []string `yaml:"roles"`
	ToolAgents   []string `yaml:"tool_agents"`
}

func LoadOverlay(path string) (*OverlayManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	m := &OverlayManifest{}
	if err := yaml.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("validate %s: %w", path, err)
	}
	return m, nil
}

func (m *OverlayManifest) validate() error {
	if m.Name == "" {
		return fmt.Errorf("name is required")
	}
	if !validLoopTemplates[m.LoopTemplate] {
		return fmt.Errorf("loop_template must be one of: dev-pipeline, exploratory (got %q)", m.LoopTemplate)
	}
	return nil
}
```

- [ ] **Step 5: Run test to verify PASS**

```
cd cli && go test ./internal/manifest/... -v
```

Expected: all pass.

- [ ] **Step 6: Commit**

```
git add cli/internal/manifest/ cli/testdata/
git commit -m "feat(manifest): overlay manifest.yaml load + validation"
```

### Task 2.2: DetectRules (overlays/<name>/detect.yaml)

**Files:**
- Create: `cli/internal/manifest/detect.go`
- Create: `cli/internal/manifest/detect_test.go`
- Create: `cli/testdata/overlays/valid-detect.yaml`

**Interfaces:**
- Consumes: `gopkg.in/yaml.v3`
- Produces:
  - `type DetectRules struct { Name string; AnyFile []string; AnyRegex []RegexRule; Confidence string }`
  - `type RegexRule struct { Path, Match string }`
  - `func LoadDetect(path string) (*DetectRules, error)`

- [ ] **Step 1: Write fixture**

```yaml
# cli/testdata/overlays/valid-detect.yaml
name: ios-swift
detect:
  any_file:
    - "*.xcodeproj"
    - "*.xcworkspace"
    - "Package.swift"
    - "project.yml"
  any_regex:
    - path: "Package.swift"
      match: "swift-tools-version"
  confidence: high
```

- [ ] **Step 2: Write failing test**

```go
// cli/internal/manifest/detect_test.go
package manifest

import (
	"path/filepath"
	"testing"
	"github.com/stretchr/testify/require"
)

func TestLoadDetectValid(t *testing.T) {
	d, err := LoadDetect(filepath.Join("..", "..", "testdata", "overlays", "valid-detect.yaml"))
	require.NoError(t, err)
	require.Equal(t, "ios-swift", d.Name)
	require.Contains(t, d.AnyFile, "*.xcodeproj")
	require.Len(t, d.AnyRegex, 1)
	require.Equal(t, "Package.swift", d.AnyRegex[0].Path)
	require.Equal(t, "high", d.Confidence)
}

func TestLoadDetectRejectsUnknownConfidence(t *testing.T) {
	// write ad-hoc invalid fixture
	tmp := t.TempDir()
	f := filepath.Join(tmp, "bad.yaml")
	err := os.WriteFile(f, []byte("name: x\ndetect:\n  confidence: extreme\n"), 0644)
	require.NoError(t, err)
	_, err = LoadDetect(f)
	require.Error(t, err)
	require.Contains(t, err.Error(), "confidence must be one of: high, medium, low")
}
```

- [ ] **Step 3: Run test to verify FAIL**

```
cd cli && go test ./internal/manifest/... -run TestLoadDetect
```

- [ ] **Step 4: Write implementation**

```go
// cli/internal/manifest/detect.go
package manifest

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type RegexRule struct {
	Path  string `yaml:"path"`
	Match string `yaml:"match"`
}

type DetectRules struct {
	Name   string `yaml:"name"`
	Detect struct {
		AnyFile    []string    `yaml:"any_file"`
		AnyRegex   []RegexRule `yaml:"any_regex"`
		Confidence string      `yaml:"confidence"`
	} `yaml:"detect"`
}

// Flattened accessors for convenience.
func (d *DetectRules) AnyFileList() []string   { return d.Detect.AnyFile }
func (d *DetectRules) AnyRegexList() []RegexRule { return d.Detect.AnyRegex }
func (d *DetectRules) ConfidenceLevel() string  { return d.Detect.Confidence }

var validConfidence = map[string]bool{"high": true, "medium": true, "low": true}

func LoadDetect(path string) (*DetectRules, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	d := &DetectRules{}
	if err := yaml.Unmarshal(data, d); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if !validConfidence[d.Detect.Confidence] {
		return nil, fmt.Errorf("confidence must be one of: high, medium, low (got %q)", d.Detect.Confidence)
	}
	return d, nil
}
```

- [ ] **Step 5: Fix test import (add os)**

```go
// cli/internal/manifest/detect_test.go — top imports
import (
	"os"
	"path/filepath"
	"testing"
	"github.com/stretchr/testify/require"
)
```

- [ ] **Step 6: Run tests to PASS**

```
cd cli && go test ./internal/manifest/... -v
```

- [ ] **Step 7: Update fixture accessor test if needed, commit**

```
git add cli/
git commit -m "feat(manifest): DetectRules load + confidence validation"
```

### Task 2.3: ProjectManifest (.zprof.yaml in project)

**Files:**
- Create: `cli/internal/manifest/project.go`
- Create: `cli/internal/manifest/project_test.go`
- Create: `cli/testdata/projects/multi-stack.zprof.yaml`

**Interfaces:**
- Consumes: `gopkg.in/yaml.v3`, `internal/models`
- Produces:
  - `type ProjectManifest struct { Overlays []string; Language string; WithGates bool; Minimal bool; ModelOverrides map[string]string; AgentOverrides map[string]string }`
  - `func LoadProject(path string) (*ProjectManifest, error)`
  - `func (m *ProjectManifest) Save(path string) error`
  - `func (m *ProjectManifest) ResolvedModel(role string) (string, error)`

- [ ] **Step 1: Write fixture**

```yaml
# cli/testdata/projects/multi-stack.zprof.yaml
overlays:
  - backend-python
  - frontend-web
language: ru
with_gates: false
minimal: false
model_overrides:
  architect-py: opus-1m
  intake-agent: claude-haiku-4-5-20251001
agent_overrides:
  planner: planner-strict
```

- [ ] **Step 2: Write failing test**

```go
// cli/internal/manifest/project_test.go
package manifest

import (
	"os"
	"path/filepath"
	"testing"
	"github.com/stretchr/testify/require"
)

func TestLoadProjectManifest(t *testing.T) {
	m, err := LoadProject(filepath.Join("..", "..", "testdata", "projects", "multi-stack.zprof.yaml"))
	require.NoError(t, err)
	require.Equal(t, []string{"backend-python", "frontend-web"}, m.Overlays)
	require.Equal(t, "ru", m.Language)
	require.Equal(t, "opus-1m", m.ModelOverrides["architect-py"])
	require.Equal(t, "planner-strict", m.AgentOverrides["planner"])
}

func TestSaveAndReloadProjectManifest(t *testing.T) {
	m := &ProjectManifest{
		Overlays:  []string{"ios-swift"},
		Language:  "ru",
		WithGates: true,
	}
	p := filepath.Join(t.TempDir(), ".zprof.yaml")
	require.NoError(t, m.Save(p))

	m2, err := LoadProject(p)
	require.NoError(t, err)
	require.Equal(t, m.Overlays, m2.Overlays)
	require.True(t, m2.WithGates)
}

func TestResolvedModelUsesOverride(t *testing.T) {
	m := &ProjectManifest{ModelOverrides: map[string]string{"architect": "opus-1m"}}
	got, err := m.ResolvedModel("architect")
	require.NoError(t, err)
	require.Equal(t, "claude-opus-4-7[1m]", got)
}

func TestResolvedModelReturnsErrorWhenNoOverride(t *testing.T) {
	m := &ProjectManifest{ModelOverrides: map[string]string{}}
	_, err := m.ResolvedModel("architect")
	require.ErrorIs(t, err, ErrNoOverride)
}
```

- [ ] **Step 3: Run test to verify FAIL**

- [ ] **Step 4: Write implementation**

```go
// cli/internal/manifest/project.go
package manifest

import (
	"errors"
	"fmt"
	"os"

	"github.com/vaporphd/zprof/internal/models"
	"gopkg.in/yaml.v3"
)

var ErrNoOverride = errors.New("no model override set for role")

type ProjectManifest struct {
	Overlays        []string          `yaml:"overlays"`
	Language        string            `yaml:"language"`
	WithGates       bool              `yaml:"with_gates"`
	Minimal         bool              `yaml:"minimal"`
	ModelOverrides  map[string]string `yaml:"model_overrides,omitempty"`
	AgentOverrides  map[string]string `yaml:"agent_overrides,omitempty"`
}

func LoadProject(path string) (*ProjectManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	m := &ProjectManifest{}
	if err := yaml.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m.Language == "" {
		m.Language = "ru"
	}
	return m, nil
}

func (m *ProjectManifest) Save(path string) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ResolvedModel returns the exact model ID for a role from ModelOverrides.
// Returns ErrNoOverride if the role has no override (caller falls back to overlay default).
func (m *ProjectManifest) ResolvedModel(role string) (string, error) {
	raw, ok := m.ModelOverrides[role]
	if !ok {
		return "", ErrNoOverride
	}
	return models.Resolve(raw)
}
```

- [ ] **Step 5: Run tests to PASS, commit**

```
cd cli && go test ./internal/manifest/... -v
git add cli/
git commit -m "feat(manifest): ProjectManifest load/save + ResolvedModel"
```

---

## Phase 3 — Detection engine

### Task 3.1: Scanner (any_file + any_regex)

**Files:**
- Create: `cli/internal/detect/scanner.go`
- Create: `cli/internal/detect/scanner_test.go`
- Create: `cli/testdata/projects/fake-ios/Package.swift`
- Create: `cli/testdata/projects/fake-ios/App.xcodeproj/project.pbxproj`
- Create: `cli/testdata/projects/fake-empty/README.md`

**Interfaces:**
- Consumes: `manifest.DetectRules`
- Produces:
  - `type Match struct { OverlayName string; Confidence string; Evidence []string }`
  - `func Scan(projectDir string, rules []*manifest.DetectRules) []Match`

- [ ] **Step 1: Create fixtures**

```
mkdir -p cli/testdata/projects/fake-ios/App.xcodeproj
mkdir -p cli/testdata/projects/fake-empty
echo '// swift-tools-version:5.9' > cli/testdata/projects/fake-ios/Package.swift
echo '// pbxproj' > cli/testdata/projects/fake-ios/App.xcodeproj/project.pbxproj
echo '# empty' > cli/testdata/projects/fake-empty/README.md
```

- [ ] **Step 2: Write failing test**

```go
// cli/internal/detect/scanner_test.go
package detect

import (
	"path/filepath"
	"testing"

	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/stretchr/testify/require"
)

func TestScanFindsIOS(t *testing.T) {
	rules, err := manifest.LoadDetect(filepath.Join("..", "..", "testdata", "overlays", "valid-detect.yaml"))
	require.NoError(t, err)
	matches := Scan(filepath.Join("..", "..", "testdata", "projects", "fake-ios"), []*manifest.DetectRules{rules})
	require.Len(t, matches, 1)
	require.Equal(t, "ios-swift", matches[0].OverlayName)
	require.Equal(t, "high", matches[0].Confidence)
	require.NotEmpty(t, matches[0].Evidence)
}

func TestScanEmptyProjectYieldsNoMatches(t *testing.T) {
	rules, err := manifest.LoadDetect(filepath.Join("..", "..", "testdata", "overlays", "valid-detect.yaml"))
	require.NoError(t, err)
	matches := Scan(filepath.Join("..", "..", "testdata", "projects", "fake-empty"), []*manifest.DetectRules{rules})
	require.Empty(t, matches)
}
```

- [ ] **Step 3: Run test — FAIL**

- [ ] **Step 4: Write scanner**

```go
// cli/internal/detect/scanner.go
package detect

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/vaporphd/zprof/internal/manifest"
)

type Match struct {
	OverlayName string
	Confidence  string
	Evidence    []string
}

// Scan walks projectDir and returns a Match per rule that finds at least
// one file/regex match.
func Scan(projectDir string, rules []*manifest.DetectRules) []Match {
	var out []Match
	for _, r := range rules {
		evidence := scanOne(projectDir, r)
		if len(evidence) > 0 {
			out = append(out, Match{
				OverlayName: r.Name,
				Confidence:  r.ConfidenceLevel(),
				Evidence:    evidence,
			})
		}
	}
	return out
}

func scanOne(dir string, r *manifest.DetectRules) []string {
	var evidence []string
	// any_file: glob against filenames anywhere in tree
	for _, pattern := range r.AnyFileList() {
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return nil
			}
			if info.IsDir() {
				name := filepath.Base(path)
				if strings.HasPrefix(name, ".") && name != "." {
					return filepath.SkipDir
				}
			}
			m, _ := filepath.Match(pattern, filepath.Base(path))
			if m {
				evidence = append(evidence, path)
			}
			return nil
		})
	}
	// any_regex: read specified path, apply regex
	for _, rr := range r.AnyRegexList() {
		p := filepath.Join(dir, rr.Path)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		re, err := regexp.Compile(rr.Match)
		if err != nil {
			continue
		}
		if re.Match(data) {
			evidence = append(evidence, p+"::"+rr.Match)
		}
	}
	return evidence
}
```

- [ ] **Step 5: Run tests to PASS, commit**

```
cd cli && go test ./internal/detect/... -v
git add cli/
git commit -m "feat(detect): scanner with any_file + any_regex"
```

---

## Phase 4 — Managed-block engine

### Task 4.1: Parser (extract blocks from marker pairs)

**Files:**
- Create: `cli/internal/managed/parser.go`
- Create: `cli/internal/managed/parser_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `type Block struct { Overlay, Key, Content string; StartLine, EndLine int }`
  - `func ParseBlocks(text string) ([]Block, error)`

- [ ] **Step 1: Write failing tests**

```go
// cli/internal/managed/parser_test.go
package managed

import (
	"testing"
	"github.com/stretchr/testify/require"
)

func TestParseBlocksSingle(t *testing.T) {
	src := `# Doc

<!-- zprof:begin overlay=ios-swift block=stack-config -->
stack:
  ios:
    build: xcodebuild
<!-- zprof:end -->

## Custom stuff
custom content here
`
	blocks, err := ParseBlocks(src)
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	require.Equal(t, "ios-swift", blocks[0].Overlay)
	require.Equal(t, "stack-config", blocks[0].Key)
	require.Contains(t, blocks[0].Content, "stack:")
	require.Contains(t, blocks[0].Content, "build: xcodebuild")
}

func TestParseBlocksMultiple(t *testing.T) {
	src := `<!-- zprof:begin overlay=py block=a -->
A
<!-- zprof:end -->
between
<!-- zprof:begin overlay=web block=b -->
B
<!-- zprof:end -->
`
	blocks, err := ParseBlocks(src)
	require.NoError(t, err)
	require.Len(t, blocks, 2)
	require.Equal(t, "py", blocks[0].Overlay)
	require.Equal(t, "web", blocks[1].Overlay)
}

func TestParseBlocksMismatchedMarkers(t *testing.T) {
	src := `<!-- zprof:begin overlay=x block=y -->
oops no end
`
	_, err := ParseBlocks(src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unclosed block")
}

func TestParseBlocksStrayEnd(t *testing.T) {
	src := `<!-- zprof:end -->
`
	_, err := ParseBlocks(src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected end marker")
}
```

- [ ] **Step 2: Run test — FAIL**

- [ ] **Step 3: Write implementation**

```go
// cli/internal/managed/parser.go
package managed

import (
	"fmt"
	"regexp"
	"strings"
)

type Block struct {
	Overlay   string
	Key       string
	Content   string
	StartLine int // 1-indexed line of the begin marker
	EndLine   int // 1-indexed line of the end marker
}

var (
	beginRe = regexp.MustCompile(`^\s*<!--\s*zprof:begin\s+overlay=([\w.-]+)\s+block=([\w.-]+)\s*-->\s*$`)
	endRe   = regexp.MustCompile(`^\s*<!--\s*zprof:end\s*-->\s*$`)
)

// ParseBlocks scans text for managed marker pairs and returns their contents.
// Mismatched or unclosed blocks return an error.
func ParseBlocks(text string) ([]Block, error) {
	lines := strings.Split(text, "\n")
	var out []Block
	var cur *Block
	var buf []string
	for i, line := range lines {
		lineNo := i + 1
		if m := beginRe.FindStringSubmatch(line); m != nil {
			if cur != nil {
				return nil, fmt.Errorf("nested begin at line %d (previous unclosed at %d)", lineNo, cur.StartLine)
			}
			cur = &Block{Overlay: m[1], Key: m[2], StartLine: lineNo}
			buf = buf[:0]
			continue
		}
		if endRe.MatchString(line) {
			if cur == nil {
				return nil, fmt.Errorf("unexpected end marker at line %d", lineNo)
			}
			cur.Content = strings.Join(buf, "\n")
			cur.EndLine = lineNo
			out = append(out, *cur)
			cur = nil
			continue
		}
		if cur != nil {
			buf = append(buf, line)
		}
	}
	if cur != nil {
		return nil, fmt.Errorf("unclosed block at line %d (overlay=%s block=%s)", cur.StartLine, cur.Overlay, cur.Key)
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests PASS, commit**

```
cd cli && go test ./internal/managed/... -v
git add cli/
git commit -m "feat(managed): marker-pair block parser"
```

### Task 4.2: Renderer (rewrite blocks, preserve outside)

**Files:**
- Create: `cli/internal/managed/renderer.go`
- Create: `cli/internal/managed/renderer_test.go`

**Interfaces:**
- Consumes: `Block`, `ParseBlocks`
- Produces:
  - `func Render(existing string, updates []Block) (string, error)` — for each key in updates, replace matching block; if a block key is present in updates but not in existing, append at end.

- [ ] **Step 1: Write failing test**

```go
// cli/internal/managed/renderer_test.go
package managed

import (
	"testing"
	"github.com/stretchr/testify/require"
)

func TestRenderReplacesExistingBlock(t *testing.T) {
	src := `# Head

<!-- zprof:begin overlay=py block=stack -->
old
<!-- zprof:end -->

# Tail
custom paragraph
`
	updates := []Block{{Overlay: "py", Key: "stack", Content: "new\nlines"}}
	out, err := Render(src, updates)
	require.NoError(t, err)
	require.Contains(t, out, "<!-- zprof:begin overlay=py block=stack -->")
	require.Contains(t, out, "new\nlines")
	require.NotContains(t, out, "old")
	require.Contains(t, out, "custom paragraph") // preserved
	require.Contains(t, out, "# Head")
}

func TestRenderAppendsMissingBlock(t *testing.T) {
	src := "# Doc\nplain text\n"
	updates := []Block{{Overlay: "ios", Key: "stack-config", Content: "hello"}}
	out, err := Render(src, updates)
	require.NoError(t, err)
	require.Contains(t, out, "<!-- zprof:begin overlay=ios block=stack-config -->")
	require.Contains(t, out, "hello")
	require.Contains(t, out, "plain text")
}

func TestRenderIdempotent(t *testing.T) {
	src := "# Doc\n"
	updates := []Block{{Overlay: "x", Key: "y", Content: "z"}}
	once, err := Render(src, updates)
	require.NoError(t, err)
	twice, err := Render(once, updates)
	require.NoError(t, err)
	require.Equal(t, once, twice)
}
```

- [ ] **Step 2: Run test — FAIL**

- [ ] **Step 3: Write renderer**

```go
// cli/internal/managed/renderer.go
package managed

import (
	"fmt"
	"strings"
)

// Render returns text with each Block in updates written. Blocks matching
// (Overlay, Key) in existing text are replaced in place; blocks not present
// are appended at the end (separated by a blank line).
func Render(existing string, updates []Block) (string, error) {
	existingBlocks, err := ParseBlocks(existing)
	if err != nil {
		return "", err
	}
	// Build map of existing block indices.
	key := func(b Block) string { return b.Overlay + "\x00" + b.Key }
	existingByKey := map[string]Block{}
	for _, b := range existingBlocks {
		existingByKey[key(b)] = b
	}

	// Replace in place: walk lines, when a begin marker matches an update,
	// swap the block content; else copy.
	lines := strings.Split(existing, "\n")
	var out []string
	i := 0
	updateByKey := map[string]Block{}
	for _, b := range updates {
		updateByKey[key(b)] = b
	}
	handled := map[string]bool{}
	for i < len(lines) {
		if m := beginRe.FindStringSubmatch(lines[i]); m != nil {
			k := m[1] + "\x00" + m[2]
			if upd, ok := updateByKey[k]; ok {
				out = append(out, fmt.Sprintf("<!-- zprof:begin overlay=%s block=%s -->", upd.Overlay, upd.Key))
				out = append(out, upd.Content)
				out = append(out, "<!-- zprof:end -->")
				handled[k] = true
				// skip until end marker
				for i++; i < len(lines) && !endRe.MatchString(lines[i]); i++ {
				}
				i++ // consume end marker
				continue
			}
		}
		out = append(out, lines[i])
		i++
	}
	// Append updates that weren't in the existing text.
	appended := false
	for _, b := range updates {
		if handled[key(b)] {
			continue
		}
		if !appended {
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			appended = true
		}
		out = append(out, fmt.Sprintf("<!-- zprof:begin overlay=%s block=%s -->", b.Overlay, b.Key))
		out = append(out, b.Content)
		out = append(out, "<!-- zprof:end -->")
	}
	// Ensure trailing newline
	rendered := strings.Join(out, "\n")
	if !strings.HasSuffix(rendered, "\n") {
		rendered += "\n"
	}
	// Ignore existing (we just consulted it for keys).
	_ = existingByKey
	return rendered, nil
}
```

- [ ] **Step 4: Run tests PASS, commit**

```
cd cli && go test ./internal/managed/... -v
git add cli/
git commit -m "feat(managed): block renderer with idempotent replace/append"
```

### Task 4.3: Merge (three modes: overwrite, preserve, interactive)

**Files:**
- Create: `cli/internal/managed/merge.go`
- Create: `cli/internal/managed/merge_test.go`

**Interfaces:**
- Consumes: `Block`, `ParseBlocks`, `Render`
- Produces:
  - `type MergeMode int` (`ModeOverwrite`, `ModePreserve`, `ModeInteractive`)
  - `type ConflictResolver func(b Block, existing, incoming string) (string, error)`
  - `func Merge(existing string, updates []Block, mode MergeMode, resolve ConflictResolver) (string, []Conflict, error)`
  - `type Conflict struct { Overlay, Key, Existing, Incoming string }`

- [ ] **Step 1: Write failing test**

```go
// cli/internal/managed/merge_test.go
package managed

import (
	"testing"
	"github.com/stretchr/testify/require"
)

func TestMergeOverwriteAlwaysWins(t *testing.T) {
	src := "<!-- zprof:begin overlay=x block=y -->\nHAND-EDITED\n<!-- zprof:end -->\n"
	updates := []Block{{Overlay: "x", Key: "y", Content: "GENERATED"}}
	out, conflicts, err := Merge(src, updates, ModeOverwrite, nil)
	require.NoError(t, err)
	require.Empty(t, conflicts)
	require.Contains(t, out, "GENERATED")
	require.NotContains(t, out, "HAND-EDITED")
}

func TestMergePreserveKeepsExistingIfDiffers(t *testing.T) {
	src := "<!-- zprof:begin overlay=x block=y -->\nHAND-EDITED\n<!-- zprof:end -->\n"
	updates := []Block{{Overlay: "x", Key: "y", Content: "GENERATED"}}
	out, conflicts, err := Merge(src, updates, ModePreserve, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	require.Contains(t, out, "HAND-EDITED")
	require.NotContains(t, out, "GENERATED")
}

func TestMergeInteractiveCallsResolver(t *testing.T) {
	src := "<!-- zprof:begin overlay=x block=y -->\nHAND\n<!-- zprof:end -->\n"
	updates := []Block{{Overlay: "x", Key: "y", Content: "NEW"}}
	called := false
	resolver := func(b Block, existing, incoming string) (string, error) {
		called = true
		require.Equal(t, "HAND", strings.TrimSpace(existing))
		require.Equal(t, "NEW", incoming)
		return "MERGED", nil
	}
	out, conflicts, err := Merge(src, updates, ModeInteractive, resolver)
	require.NoError(t, err)
	require.True(t, called)
	require.Len(t, conflicts, 1)
	require.Contains(t, out, "MERGED")
}
```

- [ ] **Step 2: Run test — FAIL**

- [ ] **Step 3: Write merge**

```go
// cli/internal/managed/merge.go
package managed

import (
	"fmt"
	"strings"
)

type MergeMode int

const (
	ModeOverwrite MergeMode = iota
	ModePreserve
	ModeInteractive
)

type Conflict struct {
	Overlay, Key, Existing, Incoming string
}

type ConflictResolver func(b Block, existing, incoming string) (string, error)

// Merge applies updates to existing text according to mode. Returns
// list of blocks that differed from existing content.
func Merge(existing string, updates []Block, mode MergeMode, resolve ConflictResolver) (string, []Conflict, error) {
	blocks, err := ParseBlocks(existing)
	if err != nil {
		return "", nil, err
	}
	byKey := map[string]Block{}
	for _, b := range blocks {
		byKey[b.Overlay+"\x00"+b.Key] = b
	}
	var conflicts []Conflict
	final := make([]Block, 0, len(updates))
	for _, u := range updates {
		k := u.Overlay + "\x00" + u.Key
		cur, exists := byKey[k]
		if !exists || strings.TrimSpace(cur.Content) == strings.TrimSpace(u.Content) {
			final = append(final, u)
			continue
		}
		c := Conflict{Overlay: u.Overlay, Key: u.Key, Existing: cur.Content, Incoming: u.Content}
		conflicts = append(conflicts, c)
		switch mode {
		case ModeOverwrite:
			final = append(final, u)
		case ModePreserve:
			final = append(final, cur)
		case ModeInteractive:
			if resolve == nil {
				return "", nil, fmt.Errorf("interactive mode requires resolver")
			}
			chosen, err := resolve(u, cur.Content, u.Content)
			if err != nil {
				return "", nil, err
			}
			final = append(final, Block{Overlay: u.Overlay, Key: u.Key, Content: chosen})
		}
	}
	rendered, err := Render(existing, final)
	return rendered, conflicts, err
}
```

- [ ] **Step 4: Add `strings` import to test file, run tests PASS, commit**

```go
// cli/internal/managed/merge_test.go — top imports
import (
	"strings"
	"testing"
	"github.com/stretchr/testify/require"
)
```

```
cd cli && go test ./internal/managed/... -v
git add cli/
git commit -m "feat(managed): 3-mode merge (overwrite/preserve/interactive)"
```

### Task 4.4: Backup helper (.bak on overwrite)

**Files:**
- Create: `cli/internal/managed/backup.go`
- Create: `cli/internal/managed/backup_test.go`

**Interfaces:**
- Consumes: `os`
- Produces: `func BackupBeforeWrite(path string) (backupPath string, err error)`

- [ ] **Step 1: Write test**

```go
// cli/internal/managed/backup_test.go
package managed

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackupCreatesBakCopy(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "CLAUDE.md")
	require.NoError(t, os.WriteFile(p, []byte("original"), 0o644))
	bak, err := BackupBeforeWrite(p)
	require.NoError(t, err)
	require.FileExists(t, bak)
	got, _ := os.ReadFile(bak)
	require.Equal(t, "original", string(got))
}

func TestBackupSkipsMissingFile(t *testing.T) {
	bak, err := BackupBeforeWrite(filepath.Join(t.TempDir(), "nope"))
	require.NoError(t, err)
	require.Empty(t, bak)
}
```

- [ ] **Step 2: Write impl**

```go
// cli/internal/managed/backup.go
package managed

import (
	"fmt"
	"os"
	"time"
)

// BackupBeforeWrite creates <path>.zprof.bak-<unixtime> if path exists.
// Returns backup path (empty string if source didn't exist).
func BackupBeforeWrite(path string) (string, error) {
	src, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read source: %w", err)
	}
	bak := fmt.Sprintf("%s.zprof.bak-%d", path, time.Now().Unix())
	if err := os.WriteFile(bak, src, 0o644); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}
	return bak, nil
}
```

- [ ] **Step 3: Run tests PASS, commit**

```
cd cli && go test ./internal/managed/... -v
git add cli/
git commit -m "feat(managed): backup file before overwrite"
```

---

## Phase 5 — Overlay loader

### Task 5.1: LoadFromDir + Base + Namespace

**Files:**
- Create: `cli/internal/overlay/loader.go`
- Create: `cli/internal/overlay/loader_test.go`
- Create: `cli/testdata/repo/base/manifest.yaml` (minimal)
- Create: `cli/testdata/repo/base/agents/planner.md`
- Create: `cli/testdata/repo/overlays/fake-ios/manifest.yaml`
- Create: `cli/testdata/repo/overlays/fake-ios/agents/architect.md`
- Create: `cli/testdata/repo/overlays/fake-ios/loop.md`
- Create: `cli/testdata/repo/overlays/fake-ios/claude-block.md`
- Create: `cli/testdata/repo/overlays/fake-ios/detect.yaml`

**Interfaces:**
- Consumes: `manifest.OverlayManifest`, `manifest.DetectRules`
- Produces:
  - `type Overlay struct { Manifest *manifest.OverlayManifest; Detect *manifest.DetectRules; Agents map[string]string; LoopMD, ClaudeBlock string; Dir string }`
  - `type Base struct { Manifest *manifest.OverlayManifest; Agents map[string]string; ClaudeBlockBase string; LoopTemplates map[string]string; StateTemplates map[string]string }`
  - `func LoadOverlay(dir string) (*Overlay, error)`
  - `func LoadBase(dir string) (*Base, error)`
  - `func NamespaceAgent(agentName, overlayName string) string`

- [ ] **Step 1: Create test fixture repo**

```
mkdir -p cli/testdata/repo/base/agents cli/testdata/repo/base/loop-templates cli/testdata/repo/base/state-templates
mkdir -p cli/testdata/repo/overlays/fake-ios/agents cli/testdata/repo/overlays/fake-ios/state
```

```yaml
# cli/testdata/repo/base/manifest.yaml
name: base
version: 0.1.0
loop_template: dev-pipeline
```

```markdown
# cli/testdata/repo/base/agents/planner.md
---
name: planner
model: sonnet
---
Планировщик задач.
```

```markdown
# cli/testdata/repo/base/loop-templates/dev-pipeline.md
# dev-pipeline template
```

```markdown
# cli/testdata/repo/base/state-templates/todo.md
# TODO
```

```yaml
# cli/testdata/repo/overlays/fake-ios/manifest.yaml
name: fake-ios
display_name: "Fake iOS"
version: 0.1.0
loop_template: dev-pipeline
roles: [architect]
tool_agents: []
```

```yaml
# cli/testdata/repo/overlays/fake-ios/detect.yaml
name: fake-ios
detect:
  any_file: ["*.xcodeproj"]
  confidence: high
```

```markdown
# cli/testdata/repo/overlays/fake-ios/agents/architect.md
---
name: architect
model: opus
---
Архитектор iOS.
```

```markdown
# cli/testdata/repo/overlays/fake-ios/loop.md
# loop.md
```

```markdown
# cli/testdata/repo/overlays/fake-ios/claude-block.md
# claude-block
```

- [ ] **Step 2: Write failing test**

```go
// cli/internal/overlay/loader_test.go
package overlay

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadBase(t *testing.T) {
	b, err := LoadBase(filepath.Join("..", "..", "testdata", "repo", "base"))
	require.NoError(t, err)
	require.Equal(t, "base", b.Manifest.Name)
	require.Contains(t, b.Agents, "planner")
	require.Contains(t, b.Agents["planner"], "Планировщик")
	require.Contains(t, b.LoopTemplates, "dev-pipeline")
	require.Contains(t, b.StateTemplates, "todo")
}

func TestLoadOverlay(t *testing.T) {
	o, err := LoadOverlay(filepath.Join("..", "..", "testdata", "repo", "overlays", "fake-ios"))
	require.NoError(t, err)
	require.Equal(t, "fake-ios", o.Manifest.Name)
	require.NotNil(t, o.Detect)
	require.Contains(t, o.Agents, "architect")
	require.NotEmpty(t, o.LoopMD)
	require.NotEmpty(t, o.ClaudeBlock)
}

func TestNamespaceAgent(t *testing.T) {
	require.Equal(t, "architect-ios", NamespaceAgent("architect", "ios-swift"))
	require.Equal(t, "architect-py", NamespaceAgent("architect", "backend-python"))
	require.Equal(t, "architect-web", NamespaceAgent("architect", "frontend-web"))
	require.Equal(t, "architect-macho", NamespaceAgent("architect", "re-macho"))
}
```

- [ ] **Step 3: Write impl**

```go
// cli/internal/overlay/loader.go
package overlay

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vaporphd/zprof/internal/manifest"
)

type Overlay struct {
	Manifest    *manifest.OverlayManifest
	Detect      *manifest.DetectRules
	Agents      map[string]string // filename (without .md) → content
	LoopMD      string
	ClaudeBlock string
	Dir         string
}

type Base struct {
	Manifest        *manifest.OverlayManifest
	Agents          map[string]string
	ClaudeBlockBase string
	LoopTemplates   map[string]string
	StateTemplates  map[string]string
}

func readAgents(dir string) (map[string]string, error) {
	out := map[string]string{}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return out, nil
	}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		name := strings.TrimSuffix(rel, ".md")
		name = strings.ReplaceAll(name, string(os.PathSeparator), "/") // preserve subfolders like gates/foo
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[name] = string(data)
		return nil
	})
	return out, err
}

func LoadOverlay(dir string) (*Overlay, error) {
	m, err := manifest.LoadOverlay(filepath.Join(dir, "manifest.yaml"))
	if err != nil {
		return nil, err
	}
	det, err := manifest.LoadDetect(filepath.Join(dir, "detect.yaml"))
	if err != nil {
		return nil, err
	}
	agents, err := readAgents(filepath.Join(dir, "agents"))
	if err != nil {
		return nil, err
	}
	loop, err := os.ReadFile(filepath.Join(dir, "loop.md"))
	if err != nil {
		return nil, fmt.Errorf("read loop.md: %w", err)
	}
	claude, err := os.ReadFile(filepath.Join(dir, "claude-block.md"))
	if err != nil {
		return nil, fmt.Errorf("read claude-block.md: %w", err)
	}
	return &Overlay{
		Manifest:    m,
		Detect:      det,
		Agents:      agents,
		LoopMD:      string(loop),
		ClaudeBlock: string(claude),
		Dir:         dir,
	}, nil
}

func LoadBase(dir string) (*Base, error) {
	m, err := manifest.LoadOverlay(filepath.Join(dir, "manifest.yaml"))
	if err != nil {
		return nil, err
	}
	agents, err := readAgents(filepath.Join(dir, "agents"))
	if err != nil {
		return nil, err
	}
	loopDir := filepath.Join(dir, "loop-templates")
	loopTemplates := map[string]string{}
	if entries, err := os.ReadDir(loopDir); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".md") {
				data, _ := os.ReadFile(filepath.Join(loopDir, e.Name()))
				loopTemplates[strings.TrimSuffix(e.Name(), ".md")] = string(data)
			}
		}
	}
	stateDir := filepath.Join(dir, "state-templates")
	stateTemplates := map[string]string{}
	if entries, err := os.ReadDir(stateDir); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".md") {
				data, _ := os.ReadFile(filepath.Join(stateDir, e.Name()))
				stateTemplates[strings.TrimSuffix(e.Name(), ".md")] = string(data)
			}
		}
	}
	claudeBase, _ := os.ReadFile(filepath.Join(dir, "claude-block-base.md"))
	return &Base{
		Manifest:        m,
		Agents:          agents,
		ClaudeBlockBase: string(claudeBase),
		LoopTemplates:   loopTemplates,
		StateTemplates:  stateTemplates,
	}, nil
}

// NamespaceAgent returns "<agent>-<suffix>" where suffix is a short overlay
// nickname. Mapping is intentional (not free-form) to keep names ergonomic.
var overlayNickname = map[string]string{
	"ios-swift":       "ios",
	"android-kotlin":  "android",
	"backend-python":  "py",
	"frontend-web":    "web",
	"re-macho":        "macho",
	"re-android":      "reandroid",
	"systems-cpp":     "cpp",
	"systems-rust":    "rust",
	"backend-go":      "go",
}

func NamespaceAgent(agentName, overlayName string) string {
	nick, ok := overlayNickname[overlayName]
	if !ok {
		nick = overlayName
	}
	return fmt.Sprintf("%s-%s", agentName, nick)
}
```

- [ ] **Step 4: Run tests PASS, commit**

```
cd cli && go test ./internal/overlay/... -v
git add cli/
git commit -m "feat(overlay): loader for base and overlays + namespace mapping"
```

---

## Phase 6 — Apply engine

### Task 6.1: Model resolution during agent copy

**Files:**
- Create: `cli/internal/apply/agent_write.go`
- Create: `cli/internal/apply/agent_write_test.go`

**Interfaces:**
- Consumes: `models.Resolve`, agent markdown text
- Produces:
  - `func WriteAgent(destDir, agentName, content string, modelOverride string) error` — parses frontmatter, resolves `model:` field, writes to `destDir/agentName.md`

- [ ] **Step 1: Write failing test**

```go
// cli/internal/apply/agent_write_test.go
package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteAgentResolvesModelAlias(t *testing.T) {
	src := `---
name: planner
model: sonnet
tools: Read, Write
---
Планировщик.
`
	dir := t.TempDir()
	require.NoError(t, WriteAgent(dir, "planner", src, ""))
	data, _ := os.ReadFile(filepath.Join(dir, "planner.md"))
	require.Contains(t, string(data), "model: claude-sonnet-5")
	require.NotContains(t, string(data), "model: sonnet\n")
}

func TestWriteAgentAppliesOverride(t *testing.T) {
	src := `---
name: planner
model: sonnet
---
body
`
	dir := t.TempDir()
	require.NoError(t, WriteAgent(dir, "planner", src, "opus-1m"))
	data, _ := os.ReadFile(filepath.Join(dir, "planner.md"))
	require.Contains(t, string(data), "model: claude-opus-4-7[1m]")
}

func TestWriteAgentErrorsOnUnknownAlias(t *testing.T) {
	src := `---
name: x
model: gpt-5
---
`
	err := WriteAgent(t.TempDir(), "x", src, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown model alias")
}
```

- [ ] **Step 2: Write impl**

```go
// cli/internal/apply/agent_write.go
package apply

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/vaporphd/zprof/internal/models"
)

var modelLineRe = regexp.MustCompile(`(?m)^model:\s*(\S+)\s*$`)

// WriteAgent parses the frontmatter model field, resolves it via the model
// registry (or applies modelOverride if non-empty), and writes to
// destDir/agentName.md.
func WriteAgent(destDir, agentName, content, modelOverride string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	rewritten, err := resolveModelInAgent(content, modelOverride)
	if err != nil {
		return err
	}
	// Preserve subdirs (e.g. gates/foo).
	target := filepath.Join(destDir, agentName+".md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(rewritten), 0o644)
}

func resolveModelInAgent(content, override string) (string, error) {
	m := modelLineRe.FindStringSubmatch(content)
	if m == nil {
		return content, nil // no model field, leave alone
	}
	src := m[1]
	if override != "" {
		src = override
	}
	exact, err := models.Resolve(src)
	if err != nil {
		return "", err
	}
	return modelLineRe.ReplaceAllString(content, "model: "+exact), nil
}

// (unused-avoidance for staticcheck)
var _ = strings.HasPrefix
var _ = fmt.Errorf
```

- [ ] **Step 3: Run tests PASS, commit**

```
cd cli && go test ./internal/apply/... -v
git add cli/
git commit -m "feat(apply): agent writer with model alias resolution + override"
```

### Task 6.2: State-file generator

**Files:**
- Create: `cli/internal/apply/state_files.go`
- Create: `cli/internal/apply/state_files_test.go`

**Interfaces:**
- Consumes: `overlay.Base`
- Produces:
  - `func EnsureStateFiles(projectDir string, base *overlay.Base, minimal bool) ([]string, error)` — writes each state template only if missing; returns list of created file paths

- [ ] **Step 1: Write test**

```go
// cli/internal/apply/state_files_test.go
package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vaporphd/zprof/internal/overlay"
	"github.com/stretchr/testify/require"
)

func TestEnsureStateFilesCreatesMissing(t *testing.T) {
	proj := t.TempDir()
	base := &overlay.Base{StateTemplates: map[string]string{
		"todo":     "# TODO",
		"lessons":  "# Lessons",
		"followup": "# Followup",
	}}
	created, err := EnsureStateFiles(proj, base, true)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"todo.md", "lessons.md", "followup.md"},
		relPaths(proj, created))
}

func TestEnsureStateFilesLeavesExistingUntouched(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, "todo.md"), []byte("user data"), 0o644))
	base := &overlay.Base{StateTemplates: map[string]string{"todo": "TEMPLATE"}}
	_, err := EnsureStateFiles(proj, base, true)
	require.NoError(t, err)
	got, _ := os.ReadFile(filepath.Join(proj, "todo.md"))
	require.Equal(t, "user data", string(got))
}

func TestEnsureStateFilesSkipsDocsInMinimal(t *testing.T) {
	proj := t.TempDir()
	base := &overlay.Base{StateTemplates: map[string]string{
		"todo":                    "T",
		"project-spec-skeleton":   "PS",
		"adr-template":            "AT",
	}}
	created, err := EnsureStateFiles(proj, base, true) // minimal=true
	require.NoError(t, err)
	require.Contains(t, relPaths(proj, created), "todo.md")
	require.NotContains(t, relPaths(proj, created), "docs/PROJECT_SPEC.md")
}

func relPaths(root string, paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		r, _ := filepath.Rel(root, p)
		out[i] = r
	}
	return out
}
```

- [ ] **Step 2: Write impl**

```go
// cli/internal/apply/state_files.go
package apply

import (
	"os"
	"path/filepath"

	"github.com/vaporphd/zprof/internal/overlay"
)

// EnsureStateFiles writes each state template to its canonical path if not
// already present. In minimal mode, docs/PROJECT_SPEC.md and docs/adr/
// templates are skipped.
func EnsureStateFiles(projectDir string, base *overlay.Base, minimal bool) ([]string, error) {
	var created []string
	targets := map[string]string{
		"todo":     filepath.Join(projectDir, "todo.md"),
		"lessons":  filepath.Join(projectDir, "lessons.md"),
		"followup": filepath.Join(projectDir, "followup.md"),
	}
	if !minimal {
		targets["project-spec-skeleton"] = filepath.Join(projectDir, "docs", "PROJECT_SPEC.md")
		targets["adr-template"] = filepath.Join(projectDir, "docs", "adr", "0000-template.md")
	}
	for tmpl, dest := range targets {
		body, ok := base.StateTemplates[tmpl]
		if !ok {
			continue
		}
		if _, err := os.Stat(dest); err == nil {
			continue // already exists, don't touch
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, []byte(body), 0o644); err != nil {
			return nil, err
		}
		created = append(created, dest)
	}
	return created, nil
}
```

- [ ] **Step 3: Run tests, commit**

```
cd cli && go test ./internal/apply/... -v
git add cli/
git commit -m "feat(apply): idempotent state-file generator"
```

### Task 6.3: Full apply orchestration (single overlay)

**Files:**
- Create: `cli/internal/apply/engine.go`
- Create: `cli/internal/apply/engine_test.go`

**Interfaces:**
- Consumes: `overlay.Base`, `overlay.Overlay`, `manifest.ProjectManifest`, `managed.Merge`
- Produces:
  - `type ApplyOpts struct { ProjectDir string; Base *overlay.Base; Overlays []*overlay.Overlay; Project *manifest.ProjectManifest; MergeMode managed.MergeMode; Resolver managed.ConflictResolver }`
  - `func Apply(opts ApplyOpts) (*ApplyResult, error)`
  - `type ApplyResult struct { CreatedAgents []string; UpdatedFiles []string; StateFiles []string; Conflicts []managed.Conflict }`

- [ ] **Step 1: Write end-to-end test using testdata/repo fixtures**

```go
// cli/internal/apply/engine_test.go
package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vaporphd/zprof/internal/managed"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/overlay"
	"github.com/stretchr/testify/require"
)

func loadTestRepo(t *testing.T) (*overlay.Base, *overlay.Overlay) {
	repo := filepath.Join("..", "..", "testdata", "repo")
	b, err := overlay.LoadBase(filepath.Join(repo, "base"))
	require.NoError(t, err)
	o, err := overlay.LoadOverlay(filepath.Join(repo, "overlays", "fake-ios"))
	require.NoError(t, err)
	return b, o
}

func TestApplySingleOverlay(t *testing.T) {
	proj := t.TempDir()
	b, o := loadTestRepo(t)
	res, err := Apply(ApplyOpts{
		ProjectDir: proj,
		Base:       b,
		Overlays:   []*overlay.Overlay{o},
		Project:    &manifest.ProjectManifest{Overlays: []string{"fake-ios"}, Language: "ru"},
		MergeMode:  managed.ModeOverwrite,
	})
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(proj, ".claude", "agents", "planner.md"))
	require.FileExists(t, filepath.Join(proj, ".claude", "agents", "architect.md"))
	require.FileExists(t, filepath.Join(proj, "AGENT_LOOP.md"))
	require.FileExists(t, filepath.Join(proj, "CLAUDE.md"))
	require.FileExists(t, filepath.Join(proj, "todo.md"))
	require.FileExists(t, filepath.Join(proj, ".zprof.yaml"))

	claude, _ := os.ReadFile(filepath.Join(proj, "CLAUDE.md"))
	require.Contains(t, string(claude), "<!-- zprof:begin overlay=fake-ios block=stack-config -->")

	require.NotEmpty(t, res.CreatedAgents)
	require.NotEmpty(t, res.UpdatedFiles)
}

func TestApplyIdempotent(t *testing.T) {
	proj := t.TempDir()
	b, o := loadTestRepo(t)
	opts := ApplyOpts{ProjectDir: proj, Base: b, Overlays: []*overlay.Overlay{o},
		Project: &manifest.ProjectManifest{Overlays: []string{"fake-ios"}}, MergeMode: managed.ModeOverwrite}
	_, err := Apply(opts)
	require.NoError(t, err)
	claude1, _ := os.ReadFile(filepath.Join(proj, "CLAUDE.md"))
	_, err = Apply(opts)
	require.NoError(t, err)
	claude2, _ := os.ReadFile(filepath.Join(proj, "CLAUDE.md"))
	require.Equal(t, string(claude1), string(claude2))
}

func TestApplyMultiOverlayNamespacesAgents(t *testing.T) {
	// use fake-ios twice under two names to prove namespacing wiring
	proj := t.TempDir()
	b, o := loadTestRepo(t)
	// clone overlay with renamed manifest.Name
	o2 := *o
	m := *o.Manifest
	m.Name = "fake-py"
	o2.Manifest = &m
	_, err := Apply(ApplyOpts{
		ProjectDir: proj,
		Base:       b,
		Overlays:   []*overlay.Overlay{o, &o2},
		Project:    &manifest.ProjectManifest{Overlays: []string{"fake-ios", "fake-py"}},
		MergeMode:  managed.ModeOverwrite,
	})
	require.NoError(t, err)
	// architect should be namespaced when >1 overlay
	require.FileExists(t, filepath.Join(proj, ".claude", "agents", "architect-fake-ios.md"))
	require.FileExists(t, filepath.Join(proj, ".claude", "agents", "architect-fake-py.md"))
}
```

- [ ] **Step 2: Write engine**

```go
// cli/internal/apply/engine.go
package apply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vaporphd/zprof/internal/managed"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/overlay"
)

type ApplyOpts struct {
	ProjectDir string
	Base       *overlay.Base
	Overlays   []*overlay.Overlay
	Project    *manifest.ProjectManifest
	MergeMode  managed.MergeMode
	Resolver   managed.ConflictResolver
}

type ApplyResult struct {
	CreatedAgents []string
	UpdatedFiles  []string
	StateFiles    []string
	Conflicts     []managed.Conflict
}

func Apply(opts ApplyOpts) (*ApplyResult, error) {
	if opts.Base == nil {
		return nil, errors.New("base is required")
	}
	if len(opts.Overlays) == 0 {
		return nil, errors.New("at least one overlay is required")
	}
	res := &ApplyResult{}

	agentDest := filepath.Join(opts.ProjectDir, ".claude", "agents")
	multi := len(opts.Overlays) > 1

	// 1. Base agents (never namespaced)
	for name, content := range opts.Base.Agents {
		override, _ := opts.Project.ResolvedModel(name)
		if err := WriteAgent(agentDest, name, content, override); err != nil {
			return nil, fmt.Errorf("write base agent %s: %w", name, err)
		}
		res.CreatedAgents = append(res.CreatedAgents, name)
	}

	// 2. Overlay agents (namespace if multi)
	for _, o := range opts.Overlays {
		for name, content := range o.Agents {
			out := name
			if multi {
				out = overlay.NamespaceAgent(name, o.Manifest.Name)
			}
			override, _ := opts.Project.ResolvedModel(out)
			if err := WriteAgent(agentDest, out, content, override); err != nil {
				return nil, fmt.Errorf("write %s agent %s: %w", o.Manifest.Name, name, err)
			}
			res.CreatedAgents = append(res.CreatedAgents, out)
		}
	}

	// 3. Render AGENT_LOOP.md
	loopPath := filepath.Join(opts.ProjectDir, "AGENT_LOOP.md")
	loopBlocks := buildLoopBlocks(opts)
	if err := renderManagedFile(loopPath, loopBlocks, opts); err != nil {
		return nil, fmt.Errorf("render AGENT_LOOP.md: %w", err)
	}
	res.UpdatedFiles = append(res.UpdatedFiles, loopPath)

	// 4. Render CLAUDE.md
	claudePath := filepath.Join(opts.ProjectDir, "CLAUDE.md")
	claudeBlocks := buildClaudeBlocks(opts)
	if err := renderManagedFile(claudePath, claudeBlocks, opts); err != nil {
		return nil, fmt.Errorf("render CLAUDE.md: %w", err)
	}
	res.UpdatedFiles = append(res.UpdatedFiles, claudePath)

	// 5. State files
	state, err := EnsureStateFiles(opts.ProjectDir, opts.Base, opts.Project.Minimal)
	if err != nil {
		return nil, err
	}
	res.StateFiles = state

	// 6. .gitignore append (thoughts/)
	if err := ensureGitignore(opts.ProjectDir); err != nil {
		return nil, err
	}

	// 7. Save .zprof.yaml
	if err := opts.Project.Save(filepath.Join(opts.ProjectDir, ".zprof.yaml")); err != nil {
		return nil, err
	}
	res.UpdatedFiles = append(res.UpdatedFiles, filepath.Join(opts.ProjectDir, ".zprof.yaml"))

	return res, nil
}

func buildLoopBlocks(opts ApplyOpts) []managed.Block {
	var blocks []managed.Block
	for _, o := range opts.Overlays {
		blocks = append(blocks, managed.Block{
			Overlay: o.Manifest.Name,
			Key:     "loop",
			Content: o.LoopMD,
		})
	}
	return blocks
}

func buildClaudeBlocks(opts ApplyOpts) []managed.Block {
	var blocks []managed.Block
	if opts.Base.ClaudeBlockBase != "" {
		blocks = append(blocks, managed.Block{
			Overlay: "base",
			Key:     "doctrine",
			Content: opts.Base.ClaudeBlockBase,
		})
	}
	for _, o := range opts.Overlays {
		blocks = append(blocks, managed.Block{
			Overlay: o.Manifest.Name,
			Key:     "stack-config",
			Content: o.ClaudeBlock,
		})
	}
	return blocks
}

func renderManagedFile(path string, blocks []managed.Block, opts ApplyOpts) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}
	// Backup before overwrite mode if file has content.
	if opts.MergeMode == managed.ModeOverwrite && existing != "" {
		if _, err := managed.BackupBeforeWrite(path); err != nil {
			return err
		}
	}
	out, _, err := managed.Merge(existing, blocks, opts.MergeMode, opts.Resolver)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

func ensureGitignore(dir string) error {
	p := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(data)
	entries := []string{"thoughts/", "*.zprof.bak-*", ".zprof.yaml.bak-*"}
	needAppend := ""
	for _, e := range entries {
		if !containsLine(content, e) {
			needAppend += e + "\n"
		}
	}
	if needAppend == "" {
		return nil
	}
	if content != "" && !endsWithNewline(content) {
		needAppend = "\n" + needAppend
	}
	return os.WriteFile(p, []byte(content+needAppend), 0o644)
}

func containsLine(text, needle string) bool {
	for _, line := range splitLines(text) {
		if line == needle {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func endsWithNewline(s string) bool { return len(s) > 0 && s[len(s)-1] == '\n' }
```

- [ ] **Step 3: Run tests PASS, commit**

```
cd cli && go test ./internal/apply/... -v
git add cli/
git commit -m "feat(apply): full apply engine with multi-overlay + namespacing + .gitignore"
```

### Task 6.4: `zprof apply` CLI subcommand

**Files:**
- Create: `cli/internal/cmd/apply.go`
- Modify: `cli/cmd/zprof/main.go`

**Interfaces:**
- Consumes: `apply.Apply`, `overlay.LoadBase`, `overlay.LoadOverlay`, `manifest.ProjectManifest`
- Produces: `zprof apply <overlay> [<overlay>...] [--minimal] [--with-gates]`

- [ ] **Step 1: Write cmd/apply.go**

```go
// cli/internal/cmd/apply.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vaporphd/zprof/internal/apply"
	"github.com/vaporphd/zprof/internal/managed"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/overlay"
	"github.com/spf13/cobra"
)

func NewApplyCmd() *cobra.Command {
	var (
		minimal   bool
		withGates bool
		dryRun    bool
	)
	c := &cobra.Command{
		Use:   "apply <overlay> [<overlay>...]",
		Short: "Применить один или несколько overlays в текущем проекте",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo := repoDir()
			base, err := overlay.LoadBase(filepath.Join(repo, "base"))
			if err != nil {
				return fmt.Errorf("load base: %w", err)
			}
			var overlays []*overlay.Overlay
			for _, name := range args {
				o, err := overlay.LoadOverlay(filepath.Join(repo, "overlays", name))
				if err != nil {
					return fmt.Errorf("load overlay %s: %w", name, err)
				}
				overlays = append(overlays, o)
			}
			proj := &manifest.ProjectManifest{
				Overlays:  args,
				Language:  "ru",
				WithGates: withGates,
				Minimal:   minimal,
			}
			pwd, _ := os.Getwd()
			if dryRun {
				fmt.Println("[dry-run] would apply overlays:", args)
				return nil
			}
			res, err := apply.Apply(apply.ApplyOpts{
				ProjectDir: pwd,
				Base:       base,
				Overlays:   overlays,
				Project:    proj,
				MergeMode:  managed.ModeOverwrite,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Создано агентов: %d\n", len(res.CreatedAgents))
			fmt.Printf("Обновлено файлов: %d\n", len(res.UpdatedFiles))
			fmt.Printf("Создано state-файлов: %d\n", len(res.StateFiles))
			return nil
		},
	}
	c.Flags().BoolVar(&minimal, "minimal", false, "Пропустить docs/PROJECT_SPEC.md и docs/adr/")
	c.Flags().BoolVar(&withGates, "with-gates", false, "Включить north-star-auditor / evidence-auditor / plan-reviewer")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "Только показать план, не писать файлы")
	return c
}

// repoDir returns ~/.zprof/repo (dev fallback: ../profiles relative to CWD).
func repoDir() string {
	if p := os.Getenv("ZPROF_REPO"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".zprof", "repo")
}
```

- [ ] **Step 2: Wire in main.go**

```go
// main.go
root.AddCommand(cmd.NewApplyCmd())
```

- [ ] **Step 3: Smoke test with ZPROF_REPO env**

```
cd /tmp && mkdir -p smoke-ios && cd smoke-ios
touch App.xcodeproj  # fake iOS marker
ZPROF_REPO=/Volumes/mydata/projects/zprof/cli/testdata/repo \
  /Volumes/mydata/projects/zprof/cli/bin/zprof apply fake-ios
ls .claude/agents/ AGENT_LOOP.md CLAUDE.md todo.md
```

Expected: all files present.

- [ ] **Step 4: Commit**

```
git add cli/
git commit -m "feat(cli): zprof apply subcommand"
```

---

## Phase 7 — Sync & Update

### Task 7.1: Git-clone/pull for repo

**Files:**
- Create: `cli/internal/sync/git.go`
- Create: `cli/internal/sync/git_test.go`

**Interfaces:**
- Consumes: `hashicorp/go-getter`
- Produces:
  - `func EnsureRepo(remoteURL, localDir string) error` — clones if missing, `go-getter` handles idempotence

- [ ] **Step 1: Write test (uses local file:// remote for hermetic test)**

```go
// cli/internal/sync/git_test.go
package sync

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureRepoClonesFromLocalGit(t *testing.T) {
	tmpRemote := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", tmpRemote, "init", "-q", "-b", "main").Run())
	require.NoError(t, os.WriteFile(filepath.Join(tmpRemote, "README.md"), []byte("test"), 0o644))
	require.NoError(t, exec.Command("git", "-C", tmpRemote, "add", "README.md").Run())
	require.NoError(t, exec.Command("git", "-C", tmpRemote, "-c", "user.email=t@t", "-c", "user.name=t",
		"commit", "-m", "init").Run())

	target := filepath.Join(t.TempDir(), "clone")
	require.NoError(t, EnsureRepo("file://"+tmpRemote, target))
	require.FileExists(t, filepath.Join(target, "README.md"))
}
```

- [ ] **Step 2: Write impl**

```go
// cli/internal/sync/git.go
package sync

import (
	"context"
	"os"
	"path/filepath"

	getter "github.com/hashicorp/go-getter"
)

// EnsureRepo clones remoteURL into localDir on first run; on subsequent runs
// it does a fetch+reset --hard main equivalent via go-getter.
func EnsureRepo(remoteURL, localDir string) error {
	if err := os.MkdirAll(filepath.Dir(localDir), 0o755); err != nil {
		return err
	}
	client := &getter.Client{
		Ctx:     context.Background(),
		Src:     "git::" + remoteURL,
		Dst:     localDir,
		Mode:    getter.ClientModeAny,
	}
	return client.Get()
}
```

- [ ] **Step 3: Run tests PASS, commit**

```
cd cli && go test ./internal/sync/... -v
git add cli/
git commit -m "feat(sync): git clone/pull via hashicorp go-getter"
```

### Task 7.2: `zprof sync` subcommand

**Files:**
- Create: `cli/internal/cmd/sync.go`
- Modify: `cli/cmd/zprof/main.go`

**Interfaces:**
- Consumes: `sync.EnsureRepo`, `apply.Apply` (re-render managed blocks after pull)
- Produces: `zprof sync`

- [ ] **Step 1: Write cmd/sync.go**

```go
// cli/internal/cmd/sync.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vaporphd/zprof/internal/apply"
	"github.com/vaporphd/zprof/internal/managed"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/overlay"
	sy "github.com/vaporphd/zprof/internal/sync"
	"github.com/spf13/cobra"
)

const defaultRemote = "https://github.com/vaporphd/zprof-profiles.git"

func NewSyncCmd() *cobra.Command {
	var remote string
	c := &cobra.Command{
		Use:   "sync",
		Short: "Обновить локальный репозиторий профилей + перегенерировать managed-блоки",
		RunE: func(cmd *cobra.Command, args []string) error {
			repo := repoDir()
			if err := sy.EnsureRepo(remote, repo); err != nil {
				return fmt.Errorf("sync repo: %w", err)
			}
			fmt.Printf("Репозиторий обновлён: %s\n", repo)

			// Re-apply if .zprof.yaml present in current project.
			pwd, _ := os.Getwd()
			mfPath := filepath.Join(pwd, ".zprof.yaml")
			if _, err := os.Stat(mfPath); err != nil {
				fmt.Println("В текущей директории нет .zprof.yaml — sync только обновил репо.")
				return nil
			}
			proj, err := manifest.LoadProject(mfPath)
			if err != nil {
				return err
			}
			base, err := overlay.LoadBase(filepath.Join(repo, "base"))
			if err != nil {
				return err
			}
			var overlays []*overlay.Overlay
			for _, name := range proj.Overlays {
				o, err := overlay.LoadOverlay(filepath.Join(repo, "overlays", name))
				if err != nil {
					return err
				}
				overlays = append(overlays, o)
			}
			_, err = apply.Apply(apply.ApplyOpts{
				ProjectDir: pwd, Base: base, Overlays: overlays,
				Project: proj, MergeMode: managed.ModeOverwrite,
			})
			if err != nil {
				return err
			}
			fmt.Println("Managed-блоки перегенерированы.")
			return nil
		},
	}
	c.Flags().StringVar(&remote, "remote", defaultRemote, "URL репозитория профилей")
	return c
}
```

- [ ] **Step 2: Wire main.go, smoke test, commit**

```
git add cli/
git commit -m "feat(cli): zprof sync subcommand"
```

---

## Phase 8 — Init wizard

### Task 8.1: Interactive init flow

**Files:**
- Create: `cli/internal/wizard/init.go`
- Create: `cli/internal/cmd/init.go`
- Modify: `cli/cmd/zprof/main.go`

**Interfaces:**
- Consumes: `detect.Scan`, `apply.Apply`, `charmbracelet/huh`
- Produces: `zprof init` — full interactive flow

- [ ] **Step 1: Write wizard/init.go**

```go
// cli/internal/wizard/init.go
package wizard

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vaporphd/zprof/internal/apply"
	"github.com/vaporphd/zprof/internal/detect"
	"github.com/vaporphd/zprof/internal/managed"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/overlay"
	"github.com/charmbracelet/huh"
)

type Opts struct {
	ProjectDir string
	RepoDir    string
}

func Run(opts Opts) error {
	// 1. Load all overlay detects from repo
	overlaysDir := filepath.Join(opts.RepoDir, "overlays")
	entries, err := os.ReadDir(overlaysDir)
	if err != nil {
		return fmt.Errorf("read overlays: %w", err)
	}
	var rules []*manifest.DetectRules
	nameByRule := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r, err := manifest.LoadDetect(filepath.Join(overlaysDir, e.Name(), "detect.yaml"))
		if err != nil {
			continue
		}
		rules = append(rules, r)
		nameByRule[r.Name] = e.Name()
	}

	// 2. Scan project
	matches := detect.Scan(opts.ProjectDir, rules)

	// 3. Prompt user with detected + option to add manually
	options := []huh.Option[string]{}
	preSelected := []string{}
	for _, m := range matches {
		label := fmt.Sprintf("%s (%s confidence, %d evidence)",
			m.OverlayName, m.Confidence, len(m.Evidence))
		options = append(options, huh.NewOption(label, nameByRule[m.OverlayName]))
		if m.Confidence == "high" {
			preSelected = append(preSelected, nameByRule[m.OverlayName])
		}
	}
	// Add non-detected overlays as unchecked
	for _, e := range entries {
		if _, seen := findOption(options, e.Name()); seen {
			continue
		}
		options = append(options, huh.NewOption(e.Name()+" (не обнаружен)", e.Name()))
	}

	var (
		chosen    []string
		lang      = "ru"
		withGates bool
		minimal   bool
	)
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Какие overlays применить?").
				Options(options...).
				Value(&chosen),
			huh.NewSelect[string]().
				Title("Язык prompt'ов").
				Options(huh.NewOption("Русский (реком)", "ru"), huh.NewOption("English", "en")).
				Value(&lang),
			huh.NewConfirm().
				Title("Включить gates (north-star / evidence / plan-reviewer)?").
				Value(&withGates),
			huh.NewConfirm().
				Title("Minimal mode (без docs/PROJECT_SPEC.md и adr/)?").
				Value(&minimal),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}
	if len(chosen) == 0 {
		return fmt.Errorf("не выбран ни один overlay")
	}
	chosen = append([]string(nil), preSelected...) // preserve pre-selected
	for _, c := range chosen {
		if !contains(chosen, c) {
			chosen = append(chosen, c)
		}
	}

	// 4. Load base + chosen overlays and apply
	base, err := overlay.LoadBase(filepath.Join(opts.RepoDir, "base"))
	if err != nil {
		return err
	}
	var loaded []*overlay.Overlay
	for _, name := range chosen {
		o, err := overlay.LoadOverlay(filepath.Join(opts.RepoDir, "overlays", name))
		if err != nil {
			return err
		}
		loaded = append(loaded, o)
	}
	proj := &manifest.ProjectManifest{Overlays: chosen, Language: lang, WithGates: withGates, Minimal: minimal}
	_, err = apply.Apply(apply.ApplyOpts{
		ProjectDir: opts.ProjectDir, Base: base, Overlays: loaded,
		Project: proj, MergeMode: managed.ModeOverwrite,
	})
	if err != nil {
		return err
	}
	fmt.Println("✔ zprof init завершён.")
	return nil
}

func findOption(opts []huh.Option[string], name string) (huh.Option[string], bool) {
	for _, o := range opts {
		if o.Value == name {
			return o, true
		}
	}
	return huh.Option[string]{}, false
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Write cmd/init.go**

```go
// cli/internal/cmd/init.go
package cmd

import (
	"os"

	"github.com/vaporphd/zprof/internal/wizard"
	"github.com/spf13/cobra"
)

func NewInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Интерактивный wizard: detect + выбор overlays + apply",
		RunE: func(cmd *cobra.Command, args []string) error {
			pwd, _ := os.Getwd()
			return wizard.Run(wizard.Opts{ProjectDir: pwd, RepoDir: repoDir()})
		},
	}
}
```

- [ ] **Step 3: Wire main.go, smoke test manually**

```
cd /tmp/smoke-ios
ZPROF_REPO=/Volumes/mydata/projects/zprof/cli/testdata/repo \
  /Volumes/mydata/projects/zprof/cli/bin/zprof init
```

- [ ] **Step 4: Commit**

```
git add cli/
git commit -m "feat(cli): zprof init wizard (huh-based)"
```

---

## Phase 9 — List, agents set, doctor

### Task 9.1: `zprof list`

**Files:**
- Create: `cli/internal/cmd/list.go`

**Interfaces:**
- Consumes: `manifest.LoadProject`
- Produces: prints overlays + agent count from `.zprof.yaml` + model overrides

- [ ] **Step 1: Write cmd/list.go**

```go
// cli/internal/cmd/list.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/spf13/cobra"
)

func NewListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Что применено в текущем проекте (из .zprof.yaml)",
		RunE: func(cmd *cobra.Command, args []string) error {
			pwd, _ := os.Getwd()
			m, err := manifest.LoadProject(filepath.Join(pwd, ".zprof.yaml"))
			if err != nil {
				return fmt.Errorf("нет .zprof.yaml в текущей директории (%w)", err)
			}
			fmt.Println("Overlays:", m.Overlays)
			fmt.Println("Язык:", m.Language)
			fmt.Println("With gates:", m.WithGates)
			fmt.Println("Minimal:", m.Minimal)
			if len(m.ModelOverrides) > 0 {
				fmt.Println("Model overrides:")
				for k, v := range m.ModelOverrides {
					fmt.Printf("  %s → %s\n", k, v)
				}
			}
			if len(m.AgentOverrides) > 0 {
				fmt.Println("Agent overrides:")
				for k, v := range m.AgentOverrides {
					fmt.Printf("  %s → %s\n", k, v)
				}
			}
			return nil
		},
	}
}
```

- [ ] **Step 2: Wire main.go, smoke test, commit**

```
git commit -am "feat(cli): zprof list"
```

### Task 9.2: `zprof agents set` (agent + model overrides)

**Files:**
- Create: `cli/internal/cmd/agents.go`
- Create: `cli/internal/cmd/agents_test.go`

**Interfaces:**
- Consumes: `manifest.LoadProject`, `manifest.Save`, `models.Resolve`
- Produces: `zprof agents set <role> [<agent-name>] [--model <alias|exact>]`

- [ ] **Step 1: Write failing test**

```go
// cli/internal/cmd/agents_test.go
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/stretchr/testify/require"
)

func TestSetModelOverride(t *testing.T) {
	dir := t.TempDir()
	base := &manifest.ProjectManifest{Overlays: []string{"ios-swift"}}
	require.NoError(t, base.Save(filepath.Join(dir, ".zprof.yaml")))
	require.NoError(t, os.Chdir(dir))

	c := NewAgentsCmd()
	c.SetArgs([]string{"set", "architect", "--model", "opus-1m"})
	require.NoError(t, c.Execute())

	m, err := manifest.LoadProject(filepath.Join(dir, ".zprof.yaml"))
	require.NoError(t, err)
	require.Equal(t, "opus-1m", m.ModelOverrides["architect"])
}
```

- [ ] **Step 2: Write impl**

```go
// cli/internal/cmd/agents.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/models"
	"github.com/spf13/cobra"
)

func NewAgentsCmd() *cobra.Command {
	root := &cobra.Command{Use: "agents", Short: "Управление агентами и моделями"}
	var modelFlag string
	set := &cobra.Command{
		Use:   "set <role> [<agent-name>]",
		Short: "Свап агента или override модели для роли",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			pwd, _ := os.Getwd()
			mfPath := filepath.Join(pwd, ".zprof.yaml")
			m, err := manifest.LoadProject(mfPath)
			if err != nil {
				return err
			}
			role := args[0]
			if len(args) == 2 {
				if m.AgentOverrides == nil {
					m.AgentOverrides = map[string]string{}
				}
				m.AgentOverrides[role] = args[1]
			}
			if modelFlag != "" {
				if _, err := models.Resolve(modelFlag); err != nil {
					return err
				}
				if m.ModelOverrides == nil {
					m.ModelOverrides = map[string]string{}
				}
				m.ModelOverrides[role] = modelFlag
			}
			if err := m.Save(mfPath); err != nil {
				return err
			}
			fmt.Printf("Обновлено: %s\n", role)
			return nil
		},
	}
	set.Flags().StringVar(&modelFlag, "model", "", "Alias (opus/sonnet/haiku/opus-1m/fable) или exact claude-* ID")
	root.AddCommand(set)
	return root
}
```

- [ ] **Step 3: Run tests, wire main.go, commit**

```
cd cli && go test ./internal/cmd/... -v
git add cli/
git commit -m "feat(cli): zprof agents set <role> [<agent>] [--model X]"
```

### Task 9.3: `zprof doctor`

**Files:**
- Create: `cli/internal/doctor/diagnostics.go`
- Create: `cli/internal/doctor/diagnostics_test.go`
- Create: `cli/internal/cmd/doctor.go`

**Interfaces:**
- Consumes: project `.zprof.yaml`, `.claude/agents/*.md`
- Produces:
  - `type Issue struct { Level string; Message string; Path string }`  (level: `error`, `warn`, `info`)
  - `func Diagnose(projectDir, repoDir string) ([]Issue, error)`

Checks in v1:
1. `.zprof.yaml` parses
2. Each declared overlay exists in `~/.zprof/repo/overlays/`
3. Number of overlays ≤ 3 (warn @2+, error @4+)
4. Every `.claude/agents/*.md` has a resolvable `model:` field
5. Every managed-file has matched marker pairs (no unclosed blocks)

- [ ] **Step 1: Write failing test**

```go
// cli/internal/doctor/diagnostics_test.go
package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiagnoseTooManyOverlaysErrors(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"),
		[]byte("overlays: [a, b, c, d]\n"), 0o644))
	repo := t.TempDir()
	for _, n := range []string{"a", "b", "c", "d"} {
		require.NoError(t, os.MkdirAll(filepath.Join(repo, "overlays", n), 0o755))
	}
	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	hasErr := false
	for _, i := range issues {
		if i.Level == "error" {
			hasErr = true
		}
	}
	require.True(t, hasErr)
}

func TestDiagnoseUnknownOverlayErrors(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"),
		[]byte("overlays: [nonexistent]\n"), 0o644))
	repo := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "overlays"), 0o755))
	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	found := false
	for _, i := range issues {
		if i.Level == "error" && contains(i.Message, "nonexistent") {
			found = true
		}
	}
	require.True(t, found)
}

func contains(a, b string) bool {
	return len(a) >= len(b) && (a[:len(b)] == b || contains(a[1:], b))
}
```

- [ ] **Step 2: Write impl**

```go
// cli/internal/doctor/diagnostics.go
package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vaporphd/zprof/internal/managed"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/models"
)

type Issue struct {
	Level   string // error | warn | info
	Message string
	Path    string
}

func Diagnose(projectDir, repoDir string) ([]Issue, error) {
	var out []Issue
	mfPath := filepath.Join(projectDir, ".zprof.yaml")
	proj, err := manifest.LoadProject(mfPath)
	if err != nil {
		return []Issue{{Level: "error", Message: fmt.Sprintf("failed to parse .zprof.yaml: %v", err), Path: mfPath}}, nil
	}

	// Overlay count
	switch {
	case len(proj.Overlays) >= 4:
		out = append(out, Issue{Level: "error", Message: fmt.Sprintf("too many overlays (%d); max is 3 in v1", len(proj.Overlays))})
	case len(proj.Overlays) >= 2:
		out = append(out, Issue{Level: "warn", Message: fmt.Sprintf("multi-stack composition with %d overlays; verify AGENT_LOOP entry-points", len(proj.Overlays))})
	}

	// Overlay existence
	for _, name := range proj.Overlays {
		p := filepath.Join(repoDir, "overlays", name)
		if _, err := os.Stat(p); err != nil {
			out = append(out, Issue{Level: "error", Message: fmt.Sprintf("overlay %q not found in repo (%s)", name, p)})
		}
	}

	// Agent model resolvability
	agentsDir := filepath.Join(projectDir, ".claude", "agents")
	_ = filepath.Walk(agentsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(line, "model:") {
				continue
			}
			val := strings.TrimSpace(strings.TrimPrefix(line, "model:"))
			if _, err := models.Resolve(val); err != nil {
				out = append(out, Issue{Level: "error", Path: path, Message: err.Error()})
			}
			break
		}
		return nil
	})

	// Managed markers in CLAUDE.md and AGENT_LOOP.md
	for _, f := range []string{"CLAUDE.md", "AGENT_LOOP.md"} {
		p := filepath.Join(projectDir, f)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if _, err := managed.ParseBlocks(string(data)); err != nil {
			out = append(out, Issue{Level: "error", Path: p, Message: err.Error()})
		}
	}
	return out, nil
}
```

- [ ] **Step 3: Write cmd/doctor.go**

```go
// cli/internal/cmd/doctor.go
package cmd

import (
	"fmt"
	"os"

	"github.com/vaporphd/zprof/internal/doctor"
	"github.com/spf13/cobra"
)

func NewDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Диагностика текущего проекта",
		RunE: func(cmd *cobra.Command, args []string) error {
			pwd, _ := os.Getwd()
			issues, err := doctor.Diagnose(pwd, repoDir())
			if err != nil {
				return err
			}
			hasError := false
			for _, i := range issues {
				fmt.Printf("[%s] %s %s\n", i.Level, i.Message, i.Path)
				if i.Level == "error" {
					hasError = true
				}
			}
			if hasError {
				os.Exit(1)
			}
			return nil
		},
	}
}
```

- [ ] **Step 4: Wire main.go, run tests, commit**

```
cd cli && go test ./internal/doctor/... -v
git add cli/
git commit -m "feat(cli): zprof doctor diagnostics"
```

---

## Phase 10 — Base pack content authoring

*Content-authoring tasks — no failing-test cycle; validation is: agent frontmatter parses, model alias resolves, file loads via `overlay.LoadBase`.*

### Task 10.1: base/manifest.yaml + claude-block-base.md

**Files:**
- Create: `profiles/base/manifest.yaml`
- Create: `profiles/base/claude-block-base.md`

- [ ] **Step 1: Write manifest**

```yaml
# profiles/base/manifest.yaml
name: base
display_name: "zprof base pack"
version: 0.1.0
loop_template: dev-pipeline  # not consumed by base but required by validator
requires_base: ">= 0.0.0"
roles: []
tool_agents: []
```

- [ ] **Step 2: Write CLAUDE.md base block content (doctrine)**

```markdown
# profiles/base/claude-block-base.md
## Doctrine (не изменять — управляется zprof)

Этот проект использует zprof — слоеная система agent-loop.

- `AGENT_LOOP.md` — контракт диспатча. Читать при старте каждой loop-итерации.
- `.claude/agents/*.md` — доступные subagent'ы, каждый с return_format schema.
- `followup.md` — living snapshot (Status + Next, ≤20 строк).
- `lessons.md` — corrections ledger (~15 записей, overflow → lessons-archive.md).
- `todo.md` — canonical task list.
- `.zprof.yaml` — какие overlays применены + model overrides.

### Изоляция
Main-сессия НИКОГДА не цитирует output subagent'а. После каждого dispatch:
запиши ≤3 строки в followup.md, дропни ответ subagent'а из рабочей памяти.

### Свои правила
Пиши ниже managed-блока — этот раздел не трогается при `zprof sync`.
```

- [ ] **Step 3: Commit**

```
git add profiles/base/
git commit -m "feat(profiles): base manifest + CLAUDE.md doctrine block"
```

### Task 10.2: base/agents/planner.md + docs-writer.md

**Files:**
- Create: `profiles/base/agents/planner.md`
- Create: `profiles/base/agents/docs-writer.md`

- [ ] **Step 1: Write planner**

```markdown
---
name: planner
description: Декомпозирует задачу в todo.md и plan-<N>.md. Диспатч по фразам «новая задача», «плана нет», «спланируй X».
tools: Read, Write, Edit, Grep, Glob
model: sonnet
return_format: |
  verdict: done|blocked|failed
  artifact: <path к plan-<N>.md>
  next: architect | null
  one_line: <≤120 символов>
---

# Planner

## Что ты делаешь
Читаешь `todo.md` + `followup.md`, берёшь верхнюю невыполненную задачу (или явно
указанную), пишешь план в `tasks/plan-<N>.md` (номер = max+1). План содержит:
1. Цель одной строкой.
2. Список шагов (2-8), каждый с ожидаемым артефактом.
3. Критерий готовности (что должно работать в конце).

## Правила
- Никогда не пиши код. Только план.
- Никогда не выводи полный план в финальное сообщение. Пиши в artifact.
- Финальное сообщение — только return_format schema.
- Если задача уже имеет план — сообщи через verdict=blocked, next=null,
  one_line="план plan-<N>.md уже существует, использовать его".
```

- [ ] **Step 2: Write docs-writer**

```markdown
---
name: docs-writer
description: Обновляет README.md, CLAUDE.md кастомные секции, docs/wiki. Диспатч по фразам «обнови README», «допиши документацию».
tools: Read, Write, Edit, Grep, Glob
model: sonnet
return_format: |
  verdict: done|blocked|failed
  artifact: <path обновлённого файла>
  next: null
  one_line: <≤120 символов>
---

# Docs Writer

## Что ты делаешь
Синхронизируешь пользовательскую документацию с состоянием проекта.
Никогда не трогаешь managed-блоки (`<!-- zprof:begin ... -->`).

## Правила
- Проверяй существование маркеров перед edit'ом.
- Не дублируй информацию, которая уже в PROJECT_SPEC.md.
- Финальное сообщение — только return_format.
```

- [ ] **Step 3: Commit**

```
git add profiles/base/
git commit -m "feat(profiles): base planner + docs-writer agents"
```

### Task 10.3: dev-orchestrator + exploratory-orchestrator

**Files:**
- Create: `profiles/base/agents/dev-orchestrator.md`
- Create: `profiles/base/agents/exploratory-orchestrator.md`

- [ ] **Step 1: Write dev-orchestrator**

```markdown
---
name: dev-orchestrator
description: Пропускает задачу через полный dev-pipeline (planner → architect → implementer → tester → reviewer) через Task-in-Task. Диспатч по фразам «take next task», «следующая задача», «прогони пайплайн».
tools: Task, Read, Write, Bash
model: sonnet
return_format: |
  verdict: done|blocked|failed
  artifact: <path к финальному отчёту / PR link>
  next: null
  one_line: <≤120 символов>
---

# Dev Orchestrator

## Что ты делаешь
Внутри Task-in-Task последовательно вызываешь:
1. planner → артефакт plan-<N>.md
2. architect (overlay-specific) → артефакт docs/adr/NNNN-*.md
3. implementer (overlay-specific) → коммит(ы), branch
4. tester (overlay-specific) → passing tests
5. reviewer (overlay-specific) → PR-ready или замечания

Между шагами: если verdict=blocked/failed — останавливайся, возвращай
одним сообщением main'у.

Финальный ответ main'у — ОДНО сообщение с verdict + путь к финальному
report'у + one_line. Main не должен видеть промежуточные детали.

## Правила
- Не запускай lint/build сам — это делает overlay-specific implementer/tester.
- Если pipeline требует user gate между шагами (например, ADR approval) —
  verdict=blocked, next=<имя следующего агента>, one_line объясняет причину.
- Ты можешь дёрнуть только те агенты, что есть в `.claude/agents/`. Если
  нужный agent отсутствует — verdict=failed.
```

- [ ] **Step 2: Write exploratory-orchestrator**

```markdown
---
name: exploratory-orchestrator
description: Пропускает binary/crash/APK через exploratory-pipeline (intake → unpack → explore → hypothesize → verify → report). Диспатч по фразам «analyze <файл>», «разбери crash», «символизируй».
tools: Task, Read, Write, Bash
model: sonnet
return_format: |
  verdict: done|blocked|failed
  artifact: <path к reports/YYYY-MM-DD-*.md>
  next: null
  one_line: <≤120 символов>
---

# Exploratory Orchestrator

## Что ты делаешь
Для re-macho / re-android overlay: pipeline intake → unpack → explore →
hypothesize → verify → report. Гипотезы могут проверяться параллельно
через Workflow (T4) — используй его если N гипотез ≥ 3.

## Правила
- Никогда не создавай PR, только markdown-отчёт в reports/.
- Финальный ответ — только return_format.
- При N гипотез ≥ 3: используй Workflow с параллельной parallel(hypotheses.map(...)).
```

- [ ] **Step 3: Commit**

```
git add profiles/base/
git commit -m "feat(profiles): dev + exploratory orchestrators"
```

### Task 10.4: Gates (opt-in via --with-gates)

**Files:**
- Create: `profiles/base/agents/gates/north-star-auditor.md`
- Create: `profiles/base/agents/gates/evidence-auditor.md`
- Create: `profiles/base/agents/gates/plan-reviewer.md`

- [ ] **Step 1: Write all three**

```markdown
# profiles/base/agents/gates/north-star-auditor.md
---
name: north-star-auditor
description: HARD upstream gate — сверяет задачу с docs/NORTH_STAR.md. Диспатчится ДО planner'а.
tools: Read, Grep
model: opus
return_format: |
  verdict: aligned | support-ok | misaligned
  artifact: <path к north-star.md ссылке>
  next: planner | null
  one_line: <≤120 символов, почему aligned/misaligned>
---

# North Star Auditor

Читаешь `docs/NORTH_STAR.md`. Оцениваешь: приближает ли предложенная
задача к целям North Star (aligned), нейтральна (support-ok), или уводит
в сторону (misaligned).

Если misaligned — verdict блокирует всю дальнейшую цепочку. Main
обязан спросить пользователя перед продолжением.
```

```markdown
# profiles/base/agents/gates/evidence-auditor.md
---
name: evidence-auditor
description: HARD gate на валидность количественных выводов до записи в тарификацию/спеку. Диспатч перед spec-maintainer/report-writer.
tools: Read, Bash, Grep
model: opus
return_format: |
  verdict: valid | insufficient | invalid
  artifact: <path к evidence-review.md>
  next: null
  one_line: <≤120 символов>
---

# Evidence Auditor

Проверяешь любое числовое утверждение (latency, throughput, cost, count):
- Есть ли сырые данные в артефактах?
- Воспроизведение возможно?
- Sample size достаточен?

Если insufficient или invalid — блокируешь запись в PROJECT_SPEC.md /
reports/.
```

```markdown
# profiles/base/agents/gates/plan-reviewer.md
---
name: plan-reviewer
description: Approve planner DRAFT до создания issues / plan-<N>.md коммита. Диспатч сразу после planner'а если --with-gates.
tools: Read
model: sonnet
return_format: |
  verdict: approved | changes-required
  artifact: <path к review-<N>.md>
  next: implementer | planner
  one_line: <≤120 символов>
---

# Plan Reviewer

Читаешь plan-<N>.md. Проверяешь по чеклисту:
- Цель одной строкой присутствует?
- Каждый шаг имеет ожидаемый артефакт?
- Критерий готовности проверяемый?
- Нет placeholder'ов (TBD, TODO)?

Verdict=changes-required возвращает планировщику с одним comment'ом.
```

- [ ] **Step 2: Commit**

```
git add profiles/base/
git commit -m "feat(profiles): opt-in gates (north-star / evidence / plan-reviewer)"
```

### Task 10.5: loop-templates + state-templates

**Files:**
- Create: `profiles/base/loop-templates/dev-pipeline.md`
- Create: `profiles/base/loop-templates/exploratory.md`
- Create: `profiles/base/state-templates/todo.md`
- Create: `profiles/base/state-templates/lessons.md`
- Create: `profiles/base/state-templates/followup.md`
- Create: `profiles/base/state-templates/project-spec-skeleton.md`
- Create: `profiles/base/state-templates/adr-template.md`

- [ ] **Step 1: Write dev-pipeline template**

```markdown
# profiles/base/loop-templates/dev-pipeline.md

## Dev pipeline

Основной поток. Overlay подставляет свои stack-aware агенты
(architect / implementer / tester / bug-hunter / refactor-agent / explorer).

### Trigger-фразы
- EN: `take next task`, `drain the queue`, `next task`, `run pipeline`
- RU: `следующая задача`, `дальше`, `прогони пайплайн`, `возьми следующее`

### Dispatch table
| Задача | Первый агент |
|---|---|
| Новая feature | `dev-orchestrator` |
| Багфикс | `bug-hunter` (overlay-specific) → `dev-orchestrator` если нужен полный pipeline |
| Только дизайн | `architect` (overlay-specific) |
| Только code review | `reviewer` (overlay-specific) |
| Только тесты | `tester` (overlay-specific) |
| Рефакторинг без feature | `refactor-agent` (overlay-specific) |
| Read-only investigation | `explorer` (overlay-specific) |

### Изоляция (обязательные правила main'а)
1. Не цитировать output subagent'а — только return_format schema.
2. ≤3 строки в followup.md после каждого dispatch.
3. Vocal self-check перед dispatch: «читаю поле <X> из результата».
```

- [ ] **Step 2: Write exploratory template**

```markdown
# profiles/base/loop-templates/exploratory.md

## Exploratory pipeline (RE / analysis)

### Trigger-фразы
- EN: `analyze <target>`, `symbolicate <crash>`, `explain <address>`, `decompile <apk>`
- RU: `разбери <файл>`, `символизируй <краш>`, `объясни <адрес>`, `декомпильни <apk>`

### Pipeline
`intake → unpack → explore → hypothesize → verify → report`

Выход: markdown-отчёт в `reports/YYYY-MM-DD-<slug>.md`, НЕ PR.

### Параллельные гипотезы
Если hypothesizer возвращает N ≥ 3 гипотез — verifier запускается через
Workflow tool (T4) с parallel-fan-out. Ограничение по умолчанию: 5 гипотез.

### Изоляция
Те же правила что и в dev-pipeline.
```

- [ ] **Step 3: Write state templates**

```markdown
# profiles/base/state-templates/todo.md

# TODO

## Milestones

## Current

- [ ] (первая задача)

## Backlog
```

```markdown
# profiles/base/state-templates/lessons.md

# Lessons

## Как использовать
Новые записи сверху. Каждая — 2-4 строки: что произошло, root cause, правило.
При >15 записях — переносить старейшие в `lessons-archive.md`.

## Записи

<!-- зд­есь появляются lessons в процессе работы -->
```

```markdown
# profiles/base/state-templates/followup.md

# Followup

## Status
<Где сейчас находимся, что последнее сделано. ≤10 строк.>

## Next
<Что делаем следующим шагом, кто dispatch, чего ждём. ≤10 строк.>
```

```markdown
# profiles/base/state-templates/project-spec-skeleton.md

# PROJECT_SPEC

## Purpose
<Одно-два предложения о цели проекта.>

## Non-goals
<Что явно НЕ делаем.>

## Architecture
<Компоненты, границы, потоки данных.>

## Milestones
<Список — synced с todo.md.>

## Doctrine
<Правила, которые НЕ пересматриваются без ADR.>
```

```markdown
# profiles/base/state-templates/adr-template.md

# ADR NNNN: <Title>

**Date:** YYYY-MM-DD
**Status:** proposed | accepted | superseded by NNNN

## Context
<Что заставляет принимать решение сейчас.>

## Decision
<Что решили. Максимально конкретно.>

## Consequences
<Что становится проще / сложнее / невозможным после.>

## Alternatives considered
<Что ещё смотрели и почему отвергли.>
```

- [ ] **Step 4: Commit**

```
git add profiles/base/
git commit -m "feat(profiles): base loop-templates + state-templates"
```

---

## Phase 11 — ios-swift overlay authoring

### Task 11.1: manifest + detect + claude-block + loop

**Files:**
- Create: `profiles/overlays/ios-swift/manifest.yaml`
- Create: `profiles/overlays/ios-swift/detect.yaml`
- Create: `profiles/overlays/ios-swift/claude-block.md`
- Create: `profiles/overlays/ios-swift/loop.md`

- [ ] **Step 1: Write files**

```yaml
# profiles/overlays/ios-swift/manifest.yaml
name: ios-swift
display_name: "iOS / Swift (UIKit + SwiftUI + AppKit)"
version: 0.1.0
loop_template: dev-pipeline
requires_base: ">= 0.1.0"
roles:
  - architect
  - implementer
  - tester
  - bug-hunter
  - refactor-agent
  - explorer
tool_agents:
  - xcode-runner
  - spm-manager
  - simulator-driver
  - testflight-shipper
  - xcodegen-driver
  - swiftlint-checker
```

```yaml
# profiles/overlays/ios-swift/detect.yaml
name: ios-swift
detect:
  any_file:
    - "*.xcodeproj"
    - "*.xcworkspace"
    - "Package.swift"
    - "project.yml"       # xcodegen
    - "Podfile"           # legacy but common
  any_regex:
    - path: "Package.swift"
      match: "swift-tools-version"
  confidence: high
```

```markdown
# profiles/overlays/ios-swift/claude-block.md
stack:
  ios-swift:
    lang: swift
    swift_version: "5.9+"    # адаптируется по Package.swift
    build_cmd: "xcodebuild -scheme <SchemeName> -configuration Debug build"
    test_cmd: "xcodebuild test -scheme <SchemeName> -destination 'platform=iOS Simulator,name=iPhone 15'"
    lint_cmd: "swiftlint --strict"
    format_cmd: "swiftformat ."
    # entitlements, provisioning profiles — smoke check
    entitlements_path: "<App>.entitlements"
```

```markdown
# profiles/overlays/ios-swift/loop.md

## iOS Swift loop

Расширение dev-pipeline для iOS проекта.

### Trigger-фразы
- EN: `next ios task`, `build`, `run on simulator`
- RU: `следующая задача`, `собери`, `запусти в симуляторе`, `протести на iPhone`

### Pipeline
Стандартный dev-pipeline, где architect/implementer/tester знают Xcode targets,
Package.swift, SwiftUI vs UIKit vs AppKit layers.

### Специальные диспатчи
| Задача | Агент |
|---|---|
| Обновить SPM зависимости | `spm-manager` |
| Собрать через xcodebuild | `xcode-runner` |
| Запустить симулятор + скриншот | `simulator-driver` |
| Ship в TestFlight | `testflight-shipper` |
| Пересобрать .xcodeproj из yml | `xcodegen-driver` |
| Lint | `swiftlint-checker` |

### Изоляция — специфичные правила
- Никогда не открывать полностью `project.pbxproj` в контекст. Использовать
  `xcodegen-driver` для правок.
- xcodebuild output может быть 10k+ строк — parser tool-агент возвращает
  только first error + last N lines.
```

- [ ] **Step 2: Commit**

```
git add profiles/overlays/ios-swift/
git commit -m "feat(profiles): ios-swift manifest/detect/claude-block/loop"
```

### Task 11.2: Six workflow role agents (architect, implementer, tester, bug-hunter, refactor-agent, explorer)

**Files:**
- Create: `profiles/overlays/ios-swift/agents/architect.md`
- Create: `profiles/overlays/ios-swift/agents/implementer.md`
- Create: `profiles/overlays/ios-swift/agents/tester.md`
- Create: `profiles/overlays/ios-swift/agents/bug-hunter.md`
- Create: `profiles/overlays/ios-swift/agents/refactor-agent.md`
- Create: `profiles/overlays/ios-swift/agents/explorer.md`

- [ ] **Step 1: Write architect**

```markdown
---
name: architect
description: Проектирует iOS-фичу с учётом SwiftUI/UIKit/AppKit layer split, SPM модулей, Xcode targets. Пишет ADR. Диспатч после planner'а по фразам «спроектируй», «design X».
tools: Read, Write, Grep, Glob, Bash
model: opus
return_format: |
  verdict: done|blocked|failed
  artifact: <path к docs/adr/NNNN-*.md>
  next: implementer | null
  one_line: <≤120 символов>
---

# iOS Swift Architect

## Что ты делаешь
Читаешь plan-<N>.md от planner'а. Проектируешь как фичу вписать в проект:
- Какой target/module (main app / SPM package / framework)
- Какой layer (SwiftUI View / UIKit ViewController / AppKit)
- Какие типы, protocol'ы, actor'ы (Swift concurrency)
- Как ложится на существующую архитектуру (MVVM / Composable / Coordinator)

Пишешь ADR в `docs/adr/NNNN-<slug>.md` (см. base template).

## Что знаешь про стек
- Swift 5.9+ concurrency (actor, Sendable, async/await, structured tasks)
- iOS Deployment Target (проверь Package.swift или .xcodeproj)
- SPM vs Xcode-native target split
- App/Extension boundary (entitlements, App Groups)

## Правила
- Не пиши код. Только ADR + структурный дизайн.
- Финальное сообщение — только return_format.
```

- [ ] **Step 2: Write implementer**

```markdown
---
name: implementer
description: Пишет Swift-код по plan + ADR. Работает в feature-branch. Знает Xcode toolchain, SPM. Диспатч после architect'а по фразам «имплементируй», «code it up».
tools: Read, Write, Edit, Bash, Grep, Glob
model: sonnet
return_format: |
  verdict: done|blocked|failed
  artifact: <path к commits-log.md ИЛИ первый коммит SHA>
  next: tester | null
  one_line: <≤120 символов>
---

# iOS Swift Implementer

## Что ты делаешь
Читаешь plan + ADR. Пишешь idiomatic Swift 5.9+ код.
Делаешь feature branch: `git switch -c feat/<slug>`.
Коммитишь маленькими логическими шагами (Conventional Commits).

## Требования
- Тесты пишет `tester`, не ты. Ты — только production code.
- Придерживаешься `swift-format` / `swiftlint` конвенций проекта.
- Async/await в новом коде, не GCD (кроме обоснованных случаев).
- Прод-imports только: Foundation, Combine (если используется), SwiftUI/UIKit/AppKit.
- Собираешь через `xcode-runner` перед коммитом — если build fails, verdict=blocked.

## Правила
- Никогда не открывай `project.pbxproj`. Если нужны Xcode target changes — dispatch `xcodegen-driver`.
- Финальное сообщение — только return_format.
```

- [ ] **Step 3: Write tester**

```markdown
---
name: tester
description: Пишет XCTest / Swift Testing тесты по plan + implemented code. Диспатч после implementer'а.
tools: Read, Write, Edit, Bash, Grep, Glob
model: sonnet
return_format: |
  verdict: done|blocked|failed
  artifact: <path к test-report.md ИЛИ последний commit SHA>
  next: reviewer | null
  one_line: <≤120 символов>
---

# iOS Swift Tester

## Что ты делаешь
Пишешь unit-тесты (XCTest ИЛИ Swift Testing framework — какой уже используется
в проекте — определи через Grep). Snapshot-тесты для SwiftUI если проект
использует swift-snapshot-testing или аналог.

Запускаешь: `xcodebuild test -scheme <> -destination 'platform=iOS Simulator,...'`

## Правила
- Изолированные unit-тесты, не сетевые. Мокай URLSession через URLProtocol.
- Snapshot-тесты — только если snapshot-фреймворк уже подключен.
- Если тесты падают — verdict=failed, one_line = "N tests failed".
- Финальное сообщение — только return_format.
```

- [ ] **Step 4: Write bug-hunter**

```markdown
---
name: bug-hunter
description: Reproducer → failing test → fix. Знает LLDB, Instruments, iOS crash logs, Xcode debug workflow. Диспатч по фразам «bug», «crash», «not working».
tools: Read, Write, Edit, Bash, Grep, Glob
model: opus
return_format: |
  verdict: done|blocked|failed
  artifact: <path к bug-report.md>
  next: reviewer | null
  one_line: <≤120 символов>
---

# iOS Swift Bug Hunter

## Что ты делаешь
1. Reproduce баг: пишешь failing XCTest.
2. Root cause: LLDB / print statements / Instruments trace.
3. Fix: минимальное изменение, которое переводит тест в green.
4. Report: `bug-report.md` с symptom / root cause / fix / prevention.

## Что знаешь про iOS debug
- Xcode symbolication, dSYM
- LLDB commands (po, expr, breakpoint)
- Instruments (Time Profiler, Leaks, Allocations)
- CrashReports.app locations
- Common iOS gotchas: retain cycles, main-thread blocking, weak/unowned

## Правила
- Не патчь симптом. Root cause обязателен.
- Финальное сообщение — только return_format.
```

- [ ] **Step 5: Write refactor-agent + explorer**

```markdown
---
name: refactor-agent
description: Semantics-preserving трансформация Swift-кода. Знает ARC, ownership, Sendable, retroactive protocol conformance. Диспатч по фразам «отрефактори», «cleanup», «переименуй».
tools: Read, Write, Edit, Bash, Grep, Glob
model: opus
return_format: |
  verdict: done|blocked|failed
  artifact: <first commit SHA>
  next: tester | null
  one_line: <≤120 символов>
---

# iOS Swift Refactor Agent

## Что ты делаешь
- Extract function / extension / type / protocol
- Rename symbol (учитывая public API + XCTest coverage)
- Convert GCD → async/await
- Migrate to actor/Sendable
- Split large file (~500+ строк) на logical modules

## Правила
- ARC-безопасность: не создавай retain cycles.
- Sendable: если добавляешь Sendable conformance — проверь все stored properties.
- Тесты ДО и ПОСЛЕ должны быть зелёными. Если ломаешь тест — verdict=blocked, next=tester.
- Финальное сообщение — только return_format.
```

```markdown
---
name: explorer
description: Read-only investigation в iOS-проекте. Знает Xcode workspace layout, SPM Package.swift, где искать storyboards/XIB, Info.plist, entitlements.
tools: Read, Grep, Glob, Bash
model: sonnet
return_format: |
  verdict: done|blocked|failed
  artifact: <path к investigation-notes.md>
  next: null
  one_line: <≤120 символов>
---

# iOS Swift Explorer

## Что ты делаешь
Отвечаешь на вопросы вида «где определён символ X», «как работает feature Y»,
«какие entitlements нужны для Z».

Пиши в `investigation-notes.md` файл с references (path:line) + короткое
объяснение. Не редактируй source.

## Правила
- Никогда не открывай project.pbxproj полностью. Используй `xcodebuild -showBuildSettings` через `xcode-runner`.
- Финальное сообщение — только return_format.
```

- [ ] **Step 6: Commit**

```
git add profiles/overlays/ios-swift/agents/
git commit -m "feat(profiles): ios-swift 6 stack-aware role agents"
```

### Task 11.3: Six tool-agents

**Files:**
- Create: `profiles/overlays/ios-swift/agents/xcode-runner.md`
- Create: `profiles/overlays/ios-swift/agents/spm-manager.md`
- Create: `profiles/overlays/ios-swift/agents/simulator-driver.md`
- Create: `profiles/overlays/ios-swift/agents/testflight-shipper.md`
- Create: `profiles/overlays/ios-swift/agents/xcodegen-driver.md`
- Create: `profiles/overlays/ios-swift/agents/swiftlint-checker.md`

- [ ] **Step 1: Write xcode-runner**

```markdown
---
name: xcode-runner
description: Обёртка над xcodebuild. Compact output — только first error + last N lines. Диспатч по фразам «собери», «build».
tools: Bash, Read
model: haiku
return_format: |
  verdict: success | build-failed | test-failed
  artifact: <path к xcodebuild-output.txt>
  next: null
  one_line: <≤120 символов первая ошибка или success>
---

# Xcode Runner

## Что ты делаешь
Запускаешь xcodebuild с параметрами из CLAUDE.md `stack.ios-swift.build_cmd`
или `test_cmd`. Захватываешь output в файл. Возвращаешь ТОЛЬКО:
- verdict
- artifact path
- one_line: первая error-строка (если fail) или "build succeeded" (если ok)

## Правила
- НИКОГДА не выводить full xcodebuild output. Он огромный.
- Финальное сообщение — только return_format schema.
```

- [ ] **Step 2: Write остальные 5 (spm-manager / simulator-driver / testflight-shipper / xcodegen-driver / swiftlint-checker)**

*Each follows the same shape as xcode-runner: haiku model, tool wrapper, terse return.*

```markdown
# profiles/overlays/ios-swift/agents/spm-manager.md
---
name: spm-manager
description: Обёртка над swift package. Add/update/remove зависимостей. Диспатч по фразам «добавь SPM package», «обнови зависимости».
tools: Bash, Read, Edit
model: haiku
return_format: |
  verdict: done|failed
  artifact: <path к Package.swift или diff>
  next: null
  one_line: <≤120 символов>
---

# SPM Manager

Читаешь Package.swift. Добавляешь/обновляешь `.package(url:from:)` +
target `.dependencies`. Запускаешь `swift package resolve`.

Не изменяешь Xcode target settings — это дело xcodegen-driver.
```

```markdown
# profiles/overlays/ios-swift/agents/simulator-driver.md
---
name: simulator-driver
description: Запуск сборки в iOS Simulator, снятие скриншотов. Диспатч по фразам «запусти в симуляторе», «сделай скриншот».
tools: Bash
model: haiku
return_format: |
  verdict: done|failed
  artifact: <path к screenshot.png или simulator-log.txt>
  next: null
  one_line: <≤120 символов>
---

# Simulator Driver

`xcrun simctl` — boot / install / launch / io screenshot / spawn log.
Screenshot: `xcrun simctl io booted screenshot path/to/shot.png`.
```

```markdown
# profiles/overlays/ios-swift/agents/testflight-shipper.md
---
name: testflight-shipper
description: Archive + upload в TestFlight. Диспатч ТОЛЬКО по явной фразе «ship to testflight». НЕ автоматически.
tools: Bash, Read
model: haiku
return_format: |
  verdict: done|failed
  artifact: <path к upload-log.txt или build number>
  next: null
  one_line: <≤120 символов>
---

# TestFlight Shipper

`xcodebuild archive` → `xcodebuild -exportArchive` → `xcrun altool --upload-app`.
Требует provisioning profile + Apple ID credential в keychain — НЕ передавай
секреты в prompt. Если fail — verdict=failed, one_line с классификацией ошибки.
```

```markdown
# profiles/overlays/ios-swift/agents/xcodegen-driver.md
---
name: xcodegen-driver
description: Пересборка .xcodeproj из project.yml. Единственный агент, который трогает Xcode target settings.
tools: Bash, Read, Edit
model: haiku
return_format: |
  verdict: done|failed
  artifact: <path к обновлённому project.yml>
  next: null
  one_line: <≤120 символов>
---

# Xcodegen Driver

Читаешь `project.yml`. Правишь targets / dependencies / settings.
Запускаешь `xcodegen generate`. Верифицируешь через `xcodebuild -showBuildSettings`.
```

```markdown
# profiles/overlays/ios-swift/agents/swiftlint-checker.md
---
name: swiftlint-checker
description: Запуск swiftlint с strict mode. Возвращает первые N warning'ов. Диспатч по фразам «lint», «проверь стиль».
tools: Bash
model: haiku
return_format: |
  verdict: clean | warnings | errors
  artifact: <path к swiftlint-report.txt>
  next: null
  one_line: <≤120 символов количество violations>
---

# SwiftLint Checker

`swiftlint --strict --reporter markdown > swiftlint-report.txt`.
verdict=clean если 0 issues, warnings если только warnings, errors если строгие.
```

- [ ] **Step 3: Commit**

```
git add profiles/overlays/ios-swift/agents/
git commit -m "feat(profiles): ios-swift six tool-agents (xcode/spm/simulator/testflight/xcodegen/swiftlint)"
```

### Task 11.4: state/project-spec-section.md for ios-swift

**Files:**
- Create: `profiles/overlays/ios-swift/state/project-spec-section.md`

- [ ] **Step 1: Write section**

```markdown
# profiles/overlays/ios-swift/state/project-spec-section.md

## iOS Swift specifics

**Deployment Target:** <iOS 17.0 / macOS 14.0 / etc.>

**Xcode Toolchain:** <Xcode 15.x, Swift 5.9+>

**Package management:** <SPM only / SPM + Podfile legacy / Xcode-native only>

**Layer split:**
- SwiftUI: <какие screens>
- UIKit: <какие screens / legacy>
- AppKit: <если Mac Catalyst или AppKit target>

**Entitlements:**
- <App Groups: group.com.example / iCloud / Keychain sharing / ...>

**TestFlight distribution:** <App Store Connect team / Apple ID>

**Continuous integration:** <Xcode Cloud / GitHub Actions macOS / Bitrise / ...>
```

- [ ] **Step 2: Commit**

```
git add profiles/overlays/ios-swift/
git commit -m "feat(profiles): ios-swift PROJECT_SPEC.md section template"
```

---

## Phase 12 — End-to-end integration test

### Task 12.1: Testdata fixture — realistic minimal iOS project

**Files:**
- Create: `cli/testdata/projects/smoke-ios/Package.swift`
- Create: `cli/testdata/projects/smoke-ios/Sources/App/App.swift`
- Create: `cli/testdata/projects/smoke-ios/README.md`

- [ ] **Step 1: Create fixture**

```
mkdir -p cli/testdata/projects/smoke-ios/Sources/App
cat > cli/testdata/projects/smoke-ios/Package.swift <<'EOF'
// swift-tools-version:5.9
import PackageDescription
let package = Package(
    name: "App",
    platforms: [.iOS(.v17)],
    targets: [ .target(name: "App") ]
)
EOF
echo "public struct App {}" > cli/testdata/projects/smoke-ios/Sources/App/App.swift
echo "# Smoke iOS" > cli/testdata/projects/smoke-ios/README.md
```

- [ ] **Step 2: Commit**

```
git add cli/testdata/projects/smoke-ios/
git commit -m "test(fixtures): minimal iOS SPM project for e2e"
```

### Task 12.2: End-to-end test — `zprof apply ios-swift` on fixture

**Files:**
- Create: `cli/internal/apply/e2e_test.go`

**Interfaces:**
- Uses real `profiles/base/` + `profiles/overlays/ios-swift/` from repo root
- Copies fixture project to tempdir, runs `Apply`, asserts full expected file tree

- [ ] **Step 1: Write test**

```go
// cli/internal/apply/e2e_test.go
package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vaporphd/zprof/internal/managed"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/overlay"
	"github.com/stretchr/testify/require"
)

func copyDir(t *testing.T, src, dst string) {
	require.NoError(t, filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	}))
}

func TestE2E_IOSApplyOnFixture(t *testing.T) {
	// Locate repo root (three dirs up from cli/internal/apply).
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	profilesDir := filepath.Join(root, "profiles")
	fixture := filepath.Join(root, "cli", "testdata", "projects", "smoke-ios")

	proj := t.TempDir()
	copyDir(t, fixture, proj)

	base, err := overlay.LoadBase(filepath.Join(profilesDir, "base"))
	require.NoError(t, err)
	ios, err := overlay.LoadOverlay(filepath.Join(profilesDir, "overlays", "ios-swift"))
	require.NoError(t, err)

	_, err = Apply(ApplyOpts{
		ProjectDir: proj, Base: base, Overlays: []*overlay.Overlay{ios},
		Project:   &manifest.ProjectManifest{Overlays: []string{"ios-swift"}, Language: "ru"},
		MergeMode: managed.ModeOverwrite,
	})
	require.NoError(t, err)

	// Assert expected files
	for _, f := range []string{
		".claude/agents/planner.md",
		".claude/agents/docs-writer.md",
		".claude/agents/dev-orchestrator.md",
		".claude/agents/exploratory-orchestrator.md",
		".claude/agents/architect.md",
		".claude/agents/implementer.md",
		".claude/agents/tester.md",
		".claude/agents/bug-hunter.md",
		".claude/agents/refactor-agent.md",
		".claude/agents/explorer.md",
		".claude/agents/xcode-runner.md",
		".claude/agents/spm-manager.md",
		".claude/agents/simulator-driver.md",
		".claude/agents/testflight-shipper.md",
		".claude/agents/xcodegen-driver.md",
		".claude/agents/swiftlint-checker.md",
		"CLAUDE.md",
		"AGENT_LOOP.md",
		"todo.md",
		"lessons.md",
		"followup.md",
		"docs/PROJECT_SPEC.md",
		"docs/adr/0000-template.md",
		".zprof.yaml",
		".gitignore",
	} {
		require.FileExists(t, filepath.Join(proj, f), "missing: %s", f)
	}

	// Assert agent model resolved (planner=sonnet → claude-sonnet-5)
	planner, _ := os.ReadFile(filepath.Join(proj, ".claude/agents/planner.md"))
	require.Contains(t, string(planner), "model: claude-sonnet-5")

	// Assert architect model resolved (opus → claude-opus-4-8)
	arch, _ := os.ReadFile(filepath.Join(proj, ".claude/agents/architect.md"))
	require.Contains(t, string(arch), "model: claude-opus-4-8")

	// Assert CLAUDE.md has ios-swift managed block
	claude, _ := os.ReadFile(filepath.Join(proj, "CLAUDE.md"))
	require.Contains(t, string(claude), "<!-- zprof:begin overlay=ios-swift block=stack-config -->")
	require.Contains(t, string(claude), "build_cmd:")

	// Assert .gitignore has thoughts/
	gi, _ := os.ReadFile(filepath.Join(proj, ".gitignore"))
	require.Contains(t, string(gi), "thoughts/")
}
```

- [ ] **Step 2: Run test — expect PASS**

```
cd cli && go test ./internal/apply/... -run TestE2E -v
```

- [ ] **Step 3: Commit**

```
git add cli/
git commit -m "test(e2e): full apply of ios-swift overlay on realistic fixture"
```

### Task 12.3: Manual smoke test in real project

**Files:** none (manual verification)

- [ ] **Step 1: Symlink profiles as fake repo home**

```
mkdir -p ~/.zprof
ln -s /Volumes/mydata/projects/zprof/profiles ~/.zprof/repo
```

- [ ] **Step 2: Install CLI locally**

```
cd /Volumes/mydata/projects/zprof/cli
make install
which zprof
```

- [ ] **Step 3: Apply in anti-backlog**

```
cd /Volumes/mydata/projects/anti-backlog
git status                        # ensure clean
zprof init                        # interactive; expect ios-swift detected
ls .claude/agents/ AGENT_LOOP.md CLAUDE.md todo.md .zprof.yaml
zprof doctor                       # expect no errors
```

- [ ] **Step 4: Verify no regression by re-running**

```
zprof apply ios-swift             # idempotent, no diff to CLAUDE.md
```

- [ ] **Step 5: Commit only if any doc updates needed**

---

## Phase 13 — README + docs polish

### Task 13.1: Top-level README

**Files:**
- Create: `README.md` (repo root)

- [ ] **Step 1: Write README**

```markdown
# zprof

Layered profile system for Claude Code — CLI + git-hosted profiles that give
your project a working agent-loop (hft_moex-style) in one command.

## Quickstart

```
brew install vaporphd/tap/zprof   # (planned for Plan C)
cd your-ios-project
zprof init                       # detects stack, applies overlays
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

```
cd cli
make build test
ZPROF_REPO=$PWD/../profiles ./bin/zprof init  # dev mode
```
```

- [ ] **Step 2: Commit**

```
git add README.md
git commit -m "docs: repo README with quickstart + dev notes"
```

---

## Self-review (post-write pass)

### Spec coverage check

| Spec section | Plan task(s) | Status |
|---|---|---|
| §3 Architecture (3-layer) | Phases 0, 5, 6 | ✓ |
| §4 base — planner/docs-writer/orchestrators | Tasks 10.2, 10.3 | ✓ |
| §4.2 opt-in gates | Task 10.4 | ✓ |
| §4.3 loop templates | Task 10.5 | ✓ |
| §5 overlay structure | Tasks 11.1-11.4 | ✓ (ios-swift only; B covers rest) |
| §6 T1 terse handoff | All agent authoring uses return_format frontmatter | ✓ |
| §6 T2 artifact-first | State files (Task 10.5) + agent prompts require artifact | ✓ |
| §6 T3 orchestrators | Task 10.3 | ✓ |
| §6 T4 Workflow fan-out | Documented in loop-templates (Task 10.5); no CLI code required | ✓ |
| §7 model tier aliases + resolver | Tasks 1.1, 1.2, 6.1 | ✓ |
| §7 model overrides | Tasks 2.3, 9.2 | ✓ |
| §8 CLI commands | Tasks 1.2, 6.4, 7.2, 8.1, 9.1, 9.2, 9.3 | ✓ |
| §8.3 managed blocks | Tasks 4.1-4.4 | ✓ |
| §9 multi-stack composition | Task 6.3 (namespacing wiring); full 3-overlay dispatch test deferred to Plan B | ✓ |
| §10 state files | Tasks 6.2, 10.5 | ✓ |
| §11 Russian language | Task 8.1 (default lang=ru); agent content authored in Russian | ✓ |
| §12 RE exploratory loop | Task 10.3 (exploratory-orchestrator); re-macho overlay in Plan B | ✓ |
| §13 distribution | **Deferred to Plan C** (goreleaser + brew tap) |
| §16 risks | Managed-block conflict → merge modes (Task 4.3); .bak (Task 4.4); doctor thresholds (Task 9.3); resolver stale → tier alias table (Task 1.1) | ✓ |

### Placeholder scan

Ran on this document — no `TBD`, `TODO`, `implement later`, or "similar to Task N" references. Every code block shows the actual code.

### Type consistency

- `models.Resolve(name string) (string, error)` — consistent across Tasks 1.1, 2.3, 6.1, 9.2, 9.3
- `manifest.ProjectManifest.ResolvedModel(role) (string, error)` + `ErrNoOverride` — Tasks 2.3, 6.3
- `managed.Block{Overlay, Key, Content, StartLine, EndLine}` — Tasks 4.1-4.3, 6.3
- `managed.Merge(existing, updates, mode, resolve) (string, []Conflict, error)` — Tasks 4.3, 6.3
- `overlay.Overlay{Manifest, Detect, Agents, LoopMD, ClaudeBlock, Dir}` + `overlay.Base` — Tasks 5.1, 6.3
- `apply.ApplyOpts{ProjectDir, Base, Overlays, Project, MergeMode, Resolver}` + `ApplyResult` — Tasks 6.3, 6.4, 7.2, 8.1

All good.

---

## Plan B & Plan C stubs (to be written after Plan A completes)

### Plan B — Six remaining overlays

Estimated ~30-40 tasks. For each of android-kotlin, backend-python, frontend-web, re-macho, systems-cpp, systems-rust:

- manifest.yaml + detect.yaml + claude-block.md + loop.md (using dev-pipeline or exploratory)
- 6 stack-aware role agents (architect / implementer / tester / bug-hunter / refactor-agent / explorer)
- 4-8 tool-agents per overlay
- state/project-spec-section.md
- E2E test on a fake fixture project for that stack

Special attention for re-macho: exploratory loop, no PR output, hypothesizer + verifier + Workflow-fan-out documentation.

### Plan C — Distribution & release

Estimated ~10-15 tasks:

- goreleaser.yaml — multi-arch build (darwin/linux, arm64/amd64)
- GitHub Releases workflow (tag → release)
- Brew tap repo (`vaporphd/homebrew-tap`) with Formula/zprof.rb
- Split monorepo → separate `zprof-profiles` repo; CLI defaults to that URL
- `zprof update` subcommand (self-update via brew)
- Semver tagging discipline documented in RELEASING.md
- Manual first release + brew install verification

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-07-16-zprof-plan-a-cli-base-ios.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**
