// Package overlay loads a base profile and its overlays from disk.
//
// Naming note: manifest.LoadOverlay(path) loads a single manifest.yaml file
// (from the internal/manifest package), while overlay.LoadOverlay(dir) in
// this package loads an entire overlay directory (manifest + detect rules +
// agents + loop/claude-block content). Both names exist by design — always
// use the fully-qualified manifest.LoadOverlay(...) when calling the
// manifest-YAML loader from within this package.
package overlay

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vaporphd/zprof/internal/manifest"
)

// Overlay is a fully loaded overlay directory: its manifest, detect rules,
// namespaced agent prompts, and loop/claude-block templates.
type Overlay struct {
	Manifest    *manifest.OverlayManifest
	Detect      *manifest.DetectRules
	Agents      map[string]string // agent name (filename without .md) -> content
	LoopMD      string
	ClaudeBlock string
	Dir         string
	// GuardSchema is the raw contents of this overlay's guard.yaml, if any
	// (ADR 0009). Optional: most overlays ship no guard.yaml today, and
	// its absence is not an error — deployGuard simply has one less layer
	// to merge.
	GuardSchema []byte
}

// Base is the fully loaded base profile: its manifest, base agent prompts,
// and shared workflow/state templates.
type Base struct {
	Manifest        *manifest.OverlayManifest
	Agents          map[string]string
	ClaudeBlockBase string
	Workflows       map[string]string
	StateTemplates  map[string]string
	// Router is the thin AGENT_LOOP.md router content, loaded from the file
	// named by the manifest's `router:` key.
	Router string
	// CollectorScript is the raw contents of zprof-collect.py, deployed
	// verbatim to <project>/.claude/zprof-collect.py on apply.
	CollectorScript []byte
	// TelemetrySchema is the raw contents of telemetry.yaml (the
	// .agentlog/dispatches.jsonl schema), deployed as <project>/.agentlog/schema.json.
	TelemetrySchema []byte
	// Verdicts is the raw contents of verdicts.yaml (ADR 0003's verdict
	// registry). Its normalized form is merged into schema.json under the
	// `verdicts` key — see apply.renderSchema.
	Verdicts []byte
	// GuardScript is the raw contents of zprof-guard.py, deployed verbatim
	// to <project>/.claude/zprof-guard.py on apply (ADR 0009).
	GuardScript []byte
	// GuardSchema is the raw contents of guard.yaml, the base layer of the
	// three-layer guard config merge (base -> overlay -> .zprof.yaml)
	// rendered to <project>/.claude/guard.json — see apply.deployGuard.
	GuardSchema []byte
}

// readAgents walks dir and returns a map of agent name (relative path, without
// the .md extension) to file content. Missing dir is not an error - it just
// yields an empty map.
func readAgents(dir string) (map[string]string, error) {
	out := map[string]string{}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return out, nil
	}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		// Reject non-regular files (symlinks, sockets, devices). A malicious
		// overlay could plant `agents/x.md` as a symlink to /etc/passwd (or
		// any file the running user can read) and its contents would be
		// silently copied into the project's `.claude/agents/x.md`. Since
		// filepath.Walk already calls Lstat we can just check Mode here.
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(rel, ".md")
		name = filepath.ToSlash(name) // preserve subfolders like gates/foo
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[name] = string(data)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// readTemplateDir reads all *.md files directly under dir into a map keyed
// by filename without the .md extension. A missing dir yields an empty map.
func readTemplateDir(dir string) (map[string]string, error) {
	out := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		out[strings.TrimSuffix(e.Name(), ".md")] = string(data)
	}
	return out, nil
}

// LoadOverlay loads an overlay directory (overlays/<name>/) into an Overlay:
// its manifest.yaml, detect.yaml, agents/*.md, loop.md, and claude-block.md.
func LoadOverlay(dir string) (*Overlay, error) {
	m, err := manifest.LoadOverlay(filepath.Join(dir, "manifest.yaml"))
	if err != nil {
		return nil, err
	}
	// Enforce that the directory name matches manifest.name — otherwise
	// namespacing (which keys off Manifest.Name) and doctor validation
	// (which looks up by manifest name as a dir) silently disagree, and
	// a rename ends up producing agents like architect-<dir> against a
	// non-existent overlays/<manifest.name>/ path.
	if base := filepath.Base(dir); base != m.Name {
		return nil, fmt.Errorf("overlay %s: directory basename %q does not match manifest name %q", dir, base, m.Name)
	}
	det, err := manifest.LoadDetect(filepath.Join(dir, "detect.yaml"))
	if err != nil {
		return nil, err
	}
	agents, err := readAgents(filepath.Join(dir, "agents"))
	if err != nil {
		return nil, fmt.Errorf("read overlay agents: %w", err)
	}
	loop, err := os.ReadFile(filepath.Join(dir, "loop.md"))
	if err != nil {
		return nil, fmt.Errorf("read loop.md: %w", err)
	}
	claude, err := os.ReadFile(filepath.Join(dir, "claude-block.md"))
	if err != nil {
		return nil, fmt.Errorf("read claude-block.md: %w", err)
	}
	guardSchema, err := os.ReadFile(filepath.Join(dir, "guard.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read guard.yaml: %w", err)
	}
	return &Overlay{
		Manifest:    m,
		Detect:      det,
		Agents:      agents,
		LoopMD:      string(loop),
		ClaudeBlock: string(claude),
		Dir:         dir,
		GuardSchema: guardSchema,
	}, nil
}

// LoadBase loads the base profile directory (base/) into a Base: its
// manifest.yaml, agents/*.md, workflows/*.md, state-templates/*.md, and the
// router file named by the manifest's `router:` key.
// claude-block-base.md, zprof-collect.py, telemetry.yaml, zprof-guard.py,
// and guard.yaml are all optional: callers that only need the
// manifest/agents/workflows (doctor's expectedAgentNames, minimal test
// fixtures) must not fail to load a base profile that predates telemetry or
// guard support. deployCollector/deployGuard (apply package) are what make
// them effectively required for `zprof apply`, by skipping their writes
// when the content is absent.
func LoadBase(dir string) (*Base, error) {
	m, err := manifest.LoadOverlay(filepath.Join(dir, "manifest.yaml"))
	if err != nil {
		return nil, err
	}
	agents, err := readAgents(filepath.Join(dir, "agents"))
	if err != nil {
		return nil, fmt.Errorf("read base agents: %w", err)
	}
	workflows, err := readTemplateDir(filepath.Join(dir, "workflows"))
	if err != nil {
		return nil, err
	}
	stateTemplates, err := readTemplateDir(filepath.Join(dir, "state-templates"))
	if err != nil {
		return nil, err
	}
	claudeBase, err := os.ReadFile(filepath.Join(dir, "claude-block-base.md"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read claude-block-base.md: %w", err)
	}
	var router string
	if m.Router != "" {
		data, err := os.ReadFile(filepath.Join(dir, m.Router))
		if err != nil {
			return nil, fmt.Errorf("read router %s: %w", m.Router, err)
		}
		router = string(data)
	}
	collectorScript, err := os.ReadFile(filepath.Join(dir, "zprof-collect.py"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read zprof-collect.py: %w", err)
	}
	telemetrySchema, err := os.ReadFile(filepath.Join(dir, "telemetry.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read telemetry.yaml: %w", err)
	}
	verdictsYAML, err := os.ReadFile(filepath.Join(dir, "verdicts.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read verdicts.yaml: %w", err)
	}
	guardScript, err := os.ReadFile(filepath.Join(dir, "zprof-guard.py"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read zprof-guard.py: %w", err)
	}
	guardSchema, err := os.ReadFile(filepath.Join(dir, "guard.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read guard.yaml: %w", err)
	}
	return &Base{
		Manifest:        m,
		Agents:          agents,
		ClaudeBlockBase: string(claudeBase),
		Workflows:       workflows,
		StateTemplates:  stateTemplates,
		Router:          router,
		CollectorScript: collectorScript,
		TelemetrySchema: telemetrySchema,
		Verdicts:        verdictsYAML,
		GuardScript:     guardScript,
		GuardSchema:     guardSchema,
	}, nil
}

// overlayNickname maps an overlay's canonical name to a short suffix used
// when namespacing agents, so prompts read "architect-ios" rather than the
// more unwieldy "architect-ios-swift".
var overlayNickname = map[string]string{
	"ios-swift":      "ios",
	"android-kotlin": "android",
	"backend-python": "py",
	"frontend-web":   "web",
	"re-macho":       "macho",
	"re-android":     "reandroid",
	"systems-cpp":    "cpp",
	"systems-rust":   "rust",
	"backend-go":     "go",
}

// NamespaceAgent returns "<agentName>-<suffix>", where suffix is overlayName's
// short nickname if one is known, or overlayName itself otherwise.
func NamespaceAgent(agentName, overlayName string) string {
	nick, ok := overlayNickname[overlayName]
	if !ok {
		nick = overlayName
	}
	return fmt.Sprintf("%s-%s", agentName, nick)
}
