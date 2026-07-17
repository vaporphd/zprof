// cli/internal/doctor/diagnostics.go
package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/vaporphd/zprof/internal/managed"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/models"
)

// Issue severity levels.
const (
	LevelError = "error"
	LevelWarn  = "warn"
	LevelInfo  = "info"
)

// Overlay-count thresholds: v1 supports up to 3 overlays comfortably;
// 2-3 draws a warning to double-check AGENT_LOOP entry-points, 4+ is an
// error since managed-block composition isn't validated past that.
const (
	overlayWarnThreshold  = 2
	overlayErrorThreshold = 4
)

// Issue is a single diagnostic finding produced by Diagnose.
type Issue struct {
	Level   string // error | warn | info
	Message string
	Path    string
}

// modelLineRe matches the `model:` frontmatter field in an agent .md file.
// Kept in sync with internal/apply.modelLineRe.
var modelLineRe = regexp.MustCompile(`(?m)^model:\s*(\S+)\s*$`)

// frontmatterRe extracts the YAML block between the leading `---` fences of
// an agent .md file. Group 1 is the frontmatter body without the fences.
// Empty match means no frontmatter — reported separately from a parse error.
var frontmatterRe = regexp.MustCompile(`\A---\r?\n((?s:.*?))\r?\n---\r?\n`)

// Diagnose inspects the project at projectDir against the zprof repo
// checkout at repoDir and returns a list of Issues covering:
//
//  1. .zprof.yaml parses
//  2. every declared overlay exists under repoDir/overlays/
//  3. overlay count is within v1 support (warn at 2+, error at 4+)
//  4. every .claude/agents/*.md has YAML-parseable frontmatter
//  5. every .claude/agents/*.md has a resolvable model: field
//  6. CLAUDE.md / AGENT_LOOP.md managed-block markers are matched
//
// Diagnose only returns a non-nil error for unexpected I/O failures; a
// broken .zprof.yaml is reported as an error Issue, not a Go error, so
// callers get a full report even when the manifest itself is invalid.
func Diagnose(projectDir, repoDir string) ([]Issue, error) {
	mfPath := filepath.Join(projectDir, ".zprof.yaml")
	proj, err := manifest.LoadProject(mfPath)
	if err != nil {
		return []Issue{{
			Level:   LevelError,
			Message: fmt.Sprintf("failed to parse .zprof.yaml: %v", err),
			Path:    mfPath,
		}}, nil
	}

	var out []Issue
	out = append(out, checkOverlayCount(proj.Overlays)...)
	out = append(out, checkOverlaysExist(proj.Overlays, repoDir)...)
	out = append(out, checkAgentFrontmatter(projectDir)...)
	out = append(out, checkAgentModels(projectDir)...)
	out = append(out, checkManagedMarkers(projectDir)...)
	return out, nil
}

// checkAgentFrontmatter parses the YAML frontmatter of every applied
// agent file and errors on any that fails. This guards against the H0
// class of bugs where an overlay ships descriptions containing `: `
// (colon+space) inside a plain scalar — Claude Code drops the agent
// silently at load time. Requires the `name` field to be present as a
// minimal contract; other fields are validated elsewhere (model tier,
// tool whitelist per §T1) or by the human authoring the overlay.
func checkAgentFrontmatter(projectDir string) []Issue {
	agentsDir := filepath.Join(projectDir, ".claude", "agents")
	if info, err := os.Stat(agentsDir); err != nil || !info.IsDir() {
		return nil
	}

	var out []Issue
	_ = filepath.Walk(agentsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			// checkAgentModels will report read failures too; avoid
			// duplicating the issue here.
			return nil
		}
		m := frontmatterRe.FindSubmatch(data)
		if m == nil {
			out = append(out, Issue{
				Level:   LevelError,
				Path:    path,
				Message: "no YAML frontmatter (must begin with `---` fence)",
			})
			return nil
		}
		var fm map[string]any
		if err := yaml.Unmarshal(m[1], &fm); err != nil {
			out = append(out, Issue{
				Level:   LevelError,
				Path:    path,
				Message: fmt.Sprintf("YAML frontmatter parse error: %v", err),
			})
			return nil
		}
		if name, ok := fm["name"].(string); !ok || name == "" {
			out = append(out, Issue{
				Level:   LevelError,
				Path:    path,
				Message: "frontmatter missing `name` field",
			})
		}
		return nil
	})
	return out
}

// checkOverlayCount warns/errors when the project composes more overlays
// than v1 validates well.
func checkOverlayCount(overlays []string) []Issue {
	n := len(overlays)
	switch {
	case n >= overlayErrorThreshold:
		return []Issue{{
			Level:   LevelError,
			Message: fmt.Sprintf("too many overlays (%d); v1 supports at most %d", n, overlayErrorThreshold-1),
		}}
	case n >= overlayWarnThreshold:
		return []Issue{{
			Level:   LevelWarn,
			Message: fmt.Sprintf("%d overlays composed; double-check AGENT_LOOP entry-points don't conflict", n),
		}}
	default:
		return nil
	}
}

// checkOverlaysExist errors for each overlay declared in .zprof.yaml that
// has no matching directory under repoDir/overlays/.
func checkOverlaysExist(overlays []string, repoDir string) []Issue {
	var out []Issue
	for _, name := range overlays {
		p := filepath.Join(repoDir, "overlays", name)
		info, err := os.Stat(p)
		if err != nil || !info.IsDir() {
			out = append(out, Issue{
				Level:   LevelError,
				Path:    p,
				Message: fmt.Sprintf("overlay %q not found in repo", name),
			})
		}
	}
	return out
}

// checkAgentModels errors for any .claude/agents/*.md (recursively, since
// applied agents can live in subdirectories such as gates/) that is
// missing a model: field or whose model doesn't resolve via the model
// registry. A missing agents directory is not itself an issue — a project
// may not have applied any overlay yet.
func checkAgentModels(projectDir string) []Issue {
	agentsDir := filepath.Join(projectDir, ".claude", "agents")
	if info, err := os.Stat(agentsDir); err != nil || !info.IsDir() {
		return nil
	}

	var out []Issue
	_ = filepath.Walk(agentsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			out = append(out, Issue{Level: LevelError, Path: path, Message: fmt.Sprintf("failed to read agent file: %v", readErr)})
			return nil
		}
		m := modelLineRe.FindStringSubmatch(string(data))
		if m == nil {
			out = append(out, Issue{Level: LevelError, Path: path, Message: "no model: field found in agent frontmatter"})
			return nil
		}
		if _, resolveErr := models.Resolve(m[1]); resolveErr != nil {
			out = append(out, Issue{Level: LevelError, Path: path, Message: resolveErr.Error()})
		}
		return nil
	})
	return out
}

// checkManagedMarkers errors when CLAUDE.md or AGENT_LOOP.md contain
// unmatched zprof:begin/zprof:end marker pairs. Missing files are not an
// issue — they're only managed once an overlay has been applied.
func checkManagedMarkers(projectDir string) []Issue {
	var out []Issue
	for _, name := range []string{"CLAUDE.md", "AGENT_LOOP.md"} {
		p := filepath.Join(projectDir, name)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if _, err := managed.ParseBlocks(string(data)); err != nil {
			out = append(out, Issue{Level: LevelError, Path: p, Message: fmt.Sprintf("managed marker error: %v", err)})
		}
	}
	return out
}
