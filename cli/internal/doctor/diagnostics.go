// cli/internal/doctor/diagnostics.go
package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/vaporphd/zprof/internal/agents"
	"github.com/vaporphd/zprof/internal/managed"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/models"
	"github.com/vaporphd/zprof/internal/overlay"
	"github.com/vaporphd/zprof/internal/verdicts"
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
//  4. every .claude/agents/*.md has YAML-parseable frontmatter, and every
//     role among them declares a return_format
//  5. every .claude/agents/*.md has a resolvable model: field
//  6. CLAUDE.md / AGENT_LOOP.md managed-block markers are matched
//  7. task-runner.md is present and no retired orchestrator survives
//  8. every active overlay declares a non-empty stop_list
//  9. no agent file is orphaned — absent from both managed_agents and the
//     currently active sources, so nothing will ever prune it
//  10. .zprof/runs/ is covered by .gitignore
//  11. .zprof/runs/ isn't piling up past runLogWarnThreshold files
//  12. .agentlog/ is covered by .gitignore
//  13. .agentlog/ isn't tracked by git (a different failure than #12 — a
//     file added before the .gitignore entry existed stays tracked)
//  14. settings.local.json wires up all three telemetry hooks
//  15. python3 -c 'pass' actually runs (macOS without Xcode CLT hangs it)
//  16. .agentlog/ is reminded to be vulnerable to `git clean -xdf`
//  17. every role's return_format enum and body `verdict:` citations are
//     covered by the verdicts.yaml registry (ADR 0003)
//  18. every chain cell in task-runner.md's `## Роутинг` table names only
//     agents present under .claude/agents/ or whitelisted in
//     `### Условные агенты маршрутов`
//  19. guard.enabled, guard hooks, guard.json and its permissions_deny are
//     deployed consistently (ADR 0009)
//  20. role resolution has at least one subagent meta.json on record
//
// Diagnose only returns a non-nil error for unexpected I/O failures; a
// broken .zprof.yaml is reported as an error Issue, not a Go error, so
// callers get a full report even when the manifest itself is invalid.
//
// A project that has no .zprof.yaml at all (fs.ErrNotExist, as opposed to a
// present-but-broken file) but did deploy telemetry/guard via `zprof apply
// --telemetry-only` — zprof's own repo checkout is exactly this shape, see
// ADR 0001 / issue #22 — falls back to diagnoseTelemetryOnly instead of the
// single LevelError below (issue #64).
func Diagnose(projectDir, repoDir string) ([]Issue, error) {
	mfPath := filepath.Join(projectDir, ".zprof.yaml")
	proj, err := manifest.LoadProject(mfPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && telemetryDeployed(projectDir) {
			return diagnoseTelemetryOnly(projectDir), nil
		}
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
	out = append(out, checkAgentVerdicts(projectDir, repoDir)...)
	out = append(out, checkAgentModels(projectDir)...)
	out = append(out, checkManagedMarkers(projectDir)...)
	out = append(out, checkTaskRunner(projectDir)...)
	out = append(out, checkRouteAgentsExist(projectDir)...)
	out = append(out, checkStopLists(proj.Overlays, repoDir)...)
	out = append(out, checkOrphanAgents(projectDir, repoDir, proj)...)
	out = append(out, checkRunsGitignored(projectDir)...)
	out = append(out, checkRunLogs(projectDir)...)
	out = append(out, checkAgentlogGitignored(projectDir)...)
	out = append(out, checkAgentlogNotTracked(projectDir)...)
	out = append(out, checkTelemetryHooks(projectDir)...)
	out = append(out, checkPython3Available()...)
	out = append(out, checkAgentlogCleanVulnerability(projectDir)...)
	out = append(out, checkAuditConfig(projectDir, proj)...)
	out = append(out, checkRunnerBudget(proj)...)
	out = append(out, checkGuardDeployment(projectDir, proj)...)
	out = append(out, checkRoleResolution(projectDir)...)
	return out, nil
}

// telemetryDeployed reports whether apply's --telemetry-only path has
// written either collector script into .claude/ — the marker Diagnose uses
// to tell "no manifest yet, but there's something on disk to check" apart
// from "no zprof deployment here at all" (issue #64).
func telemetryDeployed(projectDir string) bool {
	for _, name := range []string{"zprof-collect.py", "zprof-guard.py"} {
		if _, err := os.Stat(filepath.Join(projectDir, ".claude", name)); err == nil {
			return true
		}
	}
	return false
}

// diagnoseTelemetryOnly runs the subset of Diagnose's checks that need no
// .zprof.yaml at all, for a project whose telemetry/guard was deployed via
// `zprof apply --telemetry-only` (or predates a manifest entirely) but never
// got one written. Every check gated on proj (overlay/agent-roster/audit/
// runner-budget checks) is skipped outright rather than fed a synthetic
// manifest, since there is no overlay selection or roster to validate
// against. checkGuardDeployment is the one exception: it takes an empty
// *manifest.ProjectManifest{} so guard.enabled defaults to on (ADR 0009
// §8.3) instead of panicking on a nil proj.Guard dereference.
func diagnoseTelemetryOnly(projectDir string) []Issue {
	out := []Issue{{
		Level:   LevelInfo,
		Message: "no .zprof.yaml — manifest checks skipped (telemetry-only project)",
	}}
	out = append(out, checkRunsGitignored(projectDir)...)
	out = append(out, checkRunLogs(projectDir)...)
	out = append(out, checkAgentlogGitignored(projectDir)...)
	out = append(out, checkAgentlogNotTracked(projectDir)...)
	out = append(out, checkTelemetryHooks(projectDir)...)
	out = append(out, checkPython3Available()...)
	out = append(out, checkAgentlogCleanVulnerability(projectDir)...)
	out = append(out, checkGuardDeployment(projectDir, &manifest.ProjectManifest{})...)
	out = append(out, checkRoleResolution(projectDir)...)
	return out
}

// checkAgentFrontmatter parses the YAML frontmatter of every applied
// agent file and errors on any that fails. This guards against the H0
// class of bugs where an overlay ships descriptions containing `: `
// (colon+space) inside a plain scalar — Claude Code drops the agent
// silently at load time. Requires the `name` field to be present as a
// minimal contract; other fields are validated elsewhere (model tier,
// tool whitelist per §T1) or by the human authoring the overlay.
//
// Roles additionally must declare `return_format`: whoever dispatched a
// role parses its answer as a schema (`verdict:` first line, `next:`
// routing), and a role that never states its schema returns prose that
// silently derails the loop. Tool-agents are exempt — their output is
// consumed by the workflow step that called them, and a user's own agent
// in .claude/agents/ is none of doctor's business. Role membership is
// resolved via agents.RoleOf, which understands the namespaced names a
// multi-overlay apply writes (`implementer-ios`).
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
		fm, _, _, parseErr := parseAgentFile(path)
		if parseErr != nil {
			if errors.Is(parseErr, errReadAgentFile) {
				// checkAgentModels will report read failures too; avoid
				// duplicating the issue here.
				return nil
			}
			out = append(out, Issue{Level: LevelError, Path: path, Message: parseErr.Error()})
			return nil
		}
		if name, ok := fm["name"].(string); !ok || name == "" {
			out = append(out, Issue{
				Level:   LevelError,
				Path:    path,
				Message: "frontmatter missing `name` field",
			})
		}
		if role := agents.RoleOf(agentNameFor(agentsDir, path)); role != "" {
			if rf, ok := fm["return_format"].(string); !ok || strings.TrimSpace(rf) == "" {
				out = append(out, Issue{
					Level:   LevelError,
					Path:    path,
					Message: fmt.Sprintf("role %q has no `return_format` in frontmatter — its caller parses the answer as a schema", role),
				})
			}
		}
		return nil
	})
	return out
}

// errReadAgentFile marks a parseAgentFile failure that came from the
// os.ReadFile call itself, as opposed to a frontmatter/YAML problem in the
// file's contents. Callers that already have a dedicated read-failure
// check elsewhere (checkAgentModels) use it to skip reporting the same
// failure twice.
var errReadAgentFile = errors.New("read agent file")

// parseAgentFile splits an agent .md file into its YAML frontmatter (as a
// map), the body that follows the closing `---` fence, and the 1-based
// line number in the full file where that body begins. Shared by
// checkAgentFrontmatter and checkAgentVerdicts so both agree on exactly
// where the frontmatter ends — a prerequisite for checkAgentVerdicts to
// report body findings at the right absolute line number.
func parseAgentFile(path string) (fm map[string]any, body []byte, bodyStartLine int, err error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return nil, nil, 0, fmt.Errorf("%w: %v", errReadAgentFile, readErr)
	}
	m := frontmatterRe.FindSubmatch(data)
	if m == nil {
		return nil, nil, 0, errors.New("no YAML frontmatter (must begin with `---` fence)")
	}
	if err := yaml.Unmarshal(m[1], &fm); err != nil {
		return nil, nil, 0, fmt.Errorf("YAML frontmatter parse error: %v", err)
	}
	bodyStartLine = strings.Count(string(m[0]), "\n") + 1
	body = data[len(m[0]):]
	return fm, body, bodyStartLine, nil
}

// checkAgentVerdicts loads the verdicts registry (profiles/base/verdicts.yaml
// under repoDir) and checks every role among .claude/agents/**/*.md against
// it: its return_format enum must be a subset of the registry's allowance
// for its role (internal/verdicts.Registry.Allows), and every `verdict:`
// token its body cites must be inside its own enum or the registry's
// `quotes` allowance (internal/verdicts.CheckAgent). See ADR
// docs/adr/0003-verdicts-registry.md §D3.
//
// Gated on repoDir/base existing as a directory — same posture as
// checkOrphanAgents' expectedAgentNames: a repoDir that isn't a real
// profiles checkout (most unit-test fixtures pass a bare temp dir) has
// nothing to validate against, and that's not a project misconfiguration
// worth reporting. Once repoDir/base is a real checkout, a missing
// verdicts.yaml specifically means "checkout predates this registry" and
// is worth a warning.
func checkAgentVerdicts(projectDir, repoDir string) []Issue {
	agentsDir := filepath.Join(projectDir, ".claude", "agents")
	if info, err := os.Stat(agentsDir); err != nil || !info.IsDir() {
		return nil
	}
	baseDir := filepath.Join(repoDir, "base")
	if info, err := os.Stat(baseDir); err != nil || !info.IsDir() {
		return nil
	}

	regPath := filepath.Join(baseDir, "verdicts.yaml")
	if _, err := os.Stat(regPath); err != nil {
		return []Issue{{
			Level:   LevelWarn,
			Path:    regPath,
			Message: "verdicts.yaml not found in repo checkout — skipping verdict checks (checkout predates ADR 0003 / #20); run `zprof sync`",
		}}
	}
	reg, err := verdicts.Load(regPath)
	if err != nil {
		return []Issue{{Level: LevelError, Path: regPath, Message: err.Error()}}
	}
	if err := reg.Validate(); err != nil {
		return []Issue{{Level: LevelError, Path: regPath, Message: fmt.Sprintf("verdicts.yaml is inconsistent: %v", err)}}
	}

	var out []Issue
	_ = filepath.Walk(agentsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		fm, body, bodyStartLine, parseErr := parseAgentFile(path)
		if parseErr != nil {
			// checkAgentFrontmatter already reports broken frontmatter.
			return nil
		}
		rf, _ := fm["return_format"].(string)
		if strings.TrimSpace(rf) == "" {
			return nil // no contract; checkAgentFrontmatter flags roles missing it
		}

		name := agentNameFor(agentsDir, path)
		role, ok := reg.Lookup(name)
		if !ok {
			if roleName := agents.RoleOf(name); roleName != "" {
				out = append(out, Issue{
					Level:   LevelError,
					Path:    path,
					Message: fmt.Sprintf("role %q is missing from verdicts.yaml", roleName),
				})
			}
			// Not a role zprof's registry tracks (a user's own agent, or a
			// tool-agent) — none of doctor's business.
			return nil
		}
		_ = role

		for _, f := range verdicts.CheckAgent(reg, name, fm, body, bodyStartLine) {
			out = append(out, Issue{Level: LevelError, Path: path, Message: f.Msg})
		}
		return nil
	})

	schemaPath := filepath.Join(projectDir, ".agentlog", "schema.json")
	if data, err := os.ReadFile(schemaPath); err == nil {
		var schema map[string]any
		if json.Unmarshal(data, &schema) == nil {
			if _, hasVerdicts := schema["verdicts"]; !hasVerdicts {
				out = append(out, Issue{
					Level:   LevelWarn,
					Path:    schemaPath,
					Message: "schema.json has no `verdicts` key — run `zprof apply` (or `--telemetry-only`) to redeploy the verdicts registry",
				})
			}
		}
	}

	return out
}

// disownedInFrontmatter reports whether an agent file carries
// `zprof_managed: false` — the marker by which a user claims a file as their
// own so the orphan check stops nagging about it. A file zprof cannot read or
// parse is not treated as disowned: other checks report those, and silently
// suppressing a warning on a broken file would hide two problems at once.
func disownedInFrontmatter(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	m := frontmatterRe.FindSubmatch(data)
	if m == nil {
		return false
	}
	var fm map[string]any
	if err := yaml.Unmarshal(m[1], &fm); err != nil {
		return false
	}
	managed, ok := fm["zprof_managed"].(bool)
	return ok && !managed
}

// agentNameFor converts an on-disk agent path into the name zprof knows it
// by: the path relative to .claude/agents/ without the .md suffix, slashes
// normalized. `gates/plan-reviewer.md` → `gates/plan-reviewer`.
func agentNameFor(agentsDir, path string) string {
	rel, err := filepath.Rel(agentsDir, path)
	if err != nil {
		rel = filepath.Base(path)
	}
	return filepath.ToSlash(strings.TrimSuffix(rel, ".md"))
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

// runLogWarnThreshold is the number of files under .zprof/runs/ past which
// doctor suggests cleaning up. There is no automatic retention in v1.
const runLogWarnThreshold = 50

// checkTaskRunner errors when the task-runner agent is absent, or when a
// retired orchestrator is still present. Both break the isolation
// contract: without the runner main has nothing to delegate to, and with a
// leftover orchestrator it has a second, unsupervised path.
func checkTaskRunner(projectDir string) []Issue {
	agentsDir := filepath.Join(projectDir, ".claude", "agents")
	if info, err := os.Stat(agentsDir); err != nil || !info.IsDir() {
		return nil // no agents applied yet — not this check's business
	}

	var out []Issue
	if _, err := os.Stat(filepath.Join(agentsDir, "task-runner.md")); err != nil {
		out = append(out, Issue{
			Level:   LevelError,
			Path:    agentsDir,
			Message: "task-runner.md is missing — main has nobody to hand tasks to; run `zprof sync`",
		})
	}
	for _, name := range agents.Retired {
		p := filepath.Join(agentsDir, name+".md")
		if _, err := os.Stat(p); err == nil {
			out = append(out, Issue{
				Level:   LevelError,
				Path:    p,
				Message: fmt.Sprintf("%s is retired but still present in the project — main could dispatch it, bypassing task-runner; run `zprof sync`", name),
			})
		}
	}
	return out
}

// routeChainBacktickRe matches a single backtick-quoted span inside
// task-runner.md's `## Роутинг` table or its `### Условные агенты
// маршрутов` whitelist paragraph — for example a cell reading
// `bug-hunter → implementer → tester` or a bare name like `tester`.
var routeChainBacktickRe = regexp.MustCompile("`([^`]+)`")

// routeAgentNames splits every backtick span found in s on the route-chain
// arrow (`→`) and returns the trimmed, non-empty names across all spans —
// one call handles both a multi-agent chain cell and a single bare name.
func routeAgentNames(s string) []string {
	var out []string
	for _, m := range routeChainBacktickRe.FindAllStringSubmatch(s, -1) {
		for _, part := range strings.Split(m[1], "→") {
			if name := strings.TrimSpace(part); name != "" {
				out = append(out, name)
			}
		}
	}
	return out
}

// sectionUntilNextH2 returns the body of the markdown section starting
// right after the given "## "-level heading line, up to (not including)
// the next "## " heading or end of file. Nested "### " subsections stay
// included. Returns "" if heading isn't found.
func sectionUntilNextH2(content, heading string) string {
	idx := strings.Index(content, heading+"\n")
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(heading):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		return rest[:end]
	}
	return rest
}

// sectionParagraph returns the first paragraph (contiguous non-blank
// lines) immediately following a markdown heading line equal to heading,
// or "" if the heading isn't found. Used to scope a whitelist to exactly
// the paragraph documenting it, not everything up to the next heading.
func sectionParagraph(content, heading string) string {
	idx := strings.Index(content, heading+"\n")
	if idx < 0 {
		return ""
	}
	rest := strings.TrimLeft(content[idx+len(heading):], "\n")
	if end := strings.Index(rest, "\n\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// checkRouteAgentsExist warns when a chain cell in task-runner.md's
// `## Роутинг` table names an agent that is neither present under
// .claude/agents/ nor listed in the `### Условные агенты маршрутов`
// whitelist right below it. Those two states mean different things: a
// name that's simply missing dead-ends the route at dispatch time with a
// `verdict: failed` main has to interpret by hand, while a whitelisted
// name (e.g. the RE / анализ бинаря route's re-macho-only agents) is
// expected to be absent until the owning overlay is applied — not a
// configuration error at all.
//
// "Present" is resolved by role, not by exact filename: a multi-overlay
// apply namespaces on-disk agent files (`implementer-ios.md`), so a route
// table that names the bare role (`implementer`) must match against
// diskRoles rather than stat the literal `implementer.md`, or every route
// cell for a namespaced role false-positives the moment a second overlay
// is applied.
func checkRouteAgentsExist(projectDir string) []Issue {
	trPath := filepath.Join(projectDir, ".claude", "agents", "task-runner.md")
	data, err := os.ReadFile(trPath)
	if err != nil {
		return nil // checkTaskRunner already reports a missing task-runner.md
	}
	content := string(data)

	whitelist := map[string]bool{}
	for _, name := range routeAgentNames(sectionParagraph(content, "### Условные агенты маршрутов")) {
		whitelist[name] = true
	}

	agentsDir := filepath.Join(projectDir, ".claude", "agents")
	diskRoles := rolesOnDisk(agentsDir)
	seen := map[string]bool{}
	var out []Issue
	for _, line := range strings.Split(sectionUntilNextH2(content, "## Роутинг"), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		for _, name := range routeAgentNames(line) {
			if seen[name] || whitelist[name] {
				continue
			}
			seen[name] = true
			if agentPresentOnDisk(agentsDir, diskRoles, name) {
				continue
			}
			out = append(out, Issue{
				Level:   LevelWarn,
				Path:    trPath,
				Message: fmt.Sprintf("route names agent %q, missing from .claude/agents/ and not marked conditional", name),
			})
		}
	}
	return out
}

// rolesOnDisk walks agentsDir and returns the set of roles (per
// agents.RoleOf) that at least one on-disk agent file implements. A
// namespaced file such as `implementer-ios.md` contributes its role
// (`implementer`), same as a bare `implementer.md` would.
func rolesOnDisk(agentsDir string) map[string]bool {
	roles := map[string]bool{}
	_ = filepath.Walk(agentsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		if role := agents.RoleOf(agentNameFor(agentsDir, path)); role != "" {
			roles[role] = true
		}
		return nil
	})
	return roles
}

// agentPresentOnDisk reports whether a route table's agent name is backed by
// a real file: either the exact `<name>.md` exists, or name is a role that a
// namespaced on-disk file (`<role>-<stack>.md`) already implements.
func agentPresentOnDisk(agentsDir string, diskRoles map[string]bool, name string) bool {
	if _, err := os.Stat(filepath.Join(agentsDir, name+".md")); err == nil {
		return true
	}
	if role := agents.RoleOf(name); role != "" && diskRoles[role] {
		return true
	}
	return false
}

// checkStopLists errors for every active overlay whose manifest declares no
// stop_list. An empty list means the runner has no idea what it must not do
// on its own, and irreversible actions pass unreviewed.
//
// It also errors when an overlay's directory exists but its manifest.yaml
// fails to load (missing file, invalid YAML, failed validation) — that
// leaves the same blind spot as an empty stop_list, and unlike a genuinely
// missing overlay (whose directory is absent — checkOverlaysExist's turf),
// nothing else in doctor diagnoses it, even though `apply`/`sync` fail on
// it outright.
func checkStopLists(overlays []string, repoDir string) []Issue {
	var out []Issue
	for _, name := range overlays {
		dir := filepath.Join(repoDir, "overlays", name)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue // checkOverlaysExist already reports a missing overlay
		}
		p := filepath.Join(dir, "manifest.yaml")
		m, err := manifest.LoadOverlay(p)
		if err != nil {
			out = append(out, Issue{
				Level:   LevelError,
				Path:    p,
				Message: fmt.Sprintf("overlay %q manifest failed to load: %v", name, err),
			})
			continue
		}
		if len(m.StopList) == 0 {
			out = append(out, Issue{
				Level:   LevelError,
				Path:    p,
				Message: fmt.Sprintf("overlay %q declares no stop_list — task-runner has no way to know what it must not do on its own", name),
			})
		}
	}
	return out
}

// checkOrphanAgents warns about files in .claude/agents/ that neither the
// manifest's managed_agents roster nor the currently active sources account
// for. Nothing removes those automatically: prune only touches names the
// last apply recorded, so in a project applied by a zprof old enough to
// predate managed_agents the roster is empty and every stale agent survives
// silently. Warn rather than error — a user is entitled to keep their own
// agents next to zprof's.
//
// A user settles the ambiguity by writing `zprof_managed: false` into the
// agent's frontmatter, which silences the warning for that file. The marker
// lives in the file rather than in .zprof.yaml so that deleting the agent
// disposes of its claim too; a roster in the manifest would outlive the file
// it described. Claiming a file an active source also provides earns a
// different warning instead of silence — see below.
//
// Retired names are skipped: checkTaskRunner already reports those as
// errors with a more specific message.
func checkOrphanAgents(projectDir, repoDir string, proj *manifest.ProjectManifest) []Issue {
	agentsDir := filepath.Join(projectDir, ".claude", "agents")
	if info, err := os.Stat(agentsDir); err != nil || !info.IsDir() {
		return nil
	}
	expected, err := expectedAgentNames(proj, repoDir)
	if err != nil {
		// Without a readable repo checkout every file would look orphaned.
		// checkOverlaysExist reports the underlying problem.
		return nil
	}
	known := map[string]bool{}
	for _, n := range proj.ManagedAgents {
		known[n] = true
	}
	for n := range expected {
		known[n] = true
	}
	for _, n := range agents.Retired {
		known[n] = true
	}

	var out []Issue
	_ = filepath.Walk(agentsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		name := agentNameFor(agentsDir, path)
		disowned := disownedInFrontmatter(path)
		if known[name] {
			// A file the active sources will rewrite cannot be disowned:
			// the next apply overwrites it whatever the frontmatter says.
			// Saying so is the point — the marker would otherwise read as
			// protection the user does not actually have.
			if disowned && expected[name] {
				out = append(out, Issue{
					Level:   LevelWarn,
					Path:    path,
					Message: fmt.Sprintf("agent %q claims `zprof_managed: false` but an active source provides it — the next apply overwrites this file; rename it to keep your own version", name),
				})
			}
			return nil
		}
		if disowned {
			return nil
		}
		out = append(out, Issue{
			Level:   LevelWarn,
			Path:    path,
			Message: fmt.Sprintf("agent %q is listed in neither managed_agents nor the active sources — zprof will never remove it; delete it by hand if it is stale, or add `zprof_managed: false` to its frontmatter to claim it as your own", name),
		})
		return nil
	})
	return out
}

// expectedAgentNames reproduces the roster the current .zprof.yaml would
// produce if applied right now: base agents (gates only with --with-gates)
// plus each overlay's agents, namespaced exactly as apply namespaces them
// when more than one overlay is active.
func expectedAgentNames(proj *manifest.ProjectManifest, repoDir string) (map[string]bool, error) {
	base, err := overlay.LoadBase(filepath.Join(repoDir, "base"))
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for name := range base.Agents {
		if !proj.WithGates && strings.HasPrefix(name, "gates/") {
			continue
		}
		out[name] = true
	}
	multi := len(proj.Overlays) > 1
	for _, name := range proj.Overlays {
		o, err := overlay.LoadOverlay(filepath.Join(repoDir, "overlays", name))
		if err != nil {
			return nil, err
		}
		for agentName := range o.Agents {
			if multi {
				agentName = overlay.NamespaceAgent(agentName, o.Manifest.Name)
			}
			out[agentName] = true
		}
	}
	return out, nil
}

// checkRunsGitignored warns when the runner's journal directory can end up
// committed. Run logs are per-machine scratch: useful to tail, worthless in
// history, and they leak task phrasing into the repo.
//
// A project with no .gitignore at all is only flagged once .zprof/runs/
// actually exists — before that there is nothing to leak, and the project
// may not even be a git repo.
func checkRunsGitignored(projectDir string) []Issue {
	warn := func(path string) []Issue {
		return []Issue{{
			Level:   LevelWarn,
			Path:    path,
			Message: "`.zprof/runs/` is not in .gitignore — run logs will be committed; add the entry or run `zprof apply` again",
		}}
	}
	p := filepath.Join(projectDir, ".gitignore")
	data, err := os.ReadFile(p)
	if err != nil {
		if info, statErr := os.Stat(filepath.Join(projectDir, ".zprof", "runs")); statErr == nil && info.IsDir() {
			return warn(projectDir)
		}
		return nil
	}
	if gitignoreCoversRuns(string(data)) {
		return nil
	}
	return warn(p)
}

// gitignoreCoversRuns reports whether any active .gitignore pattern
// excludes .zprof/runs/. Accepts the exact entry apply writes plus the
// coarser `.zprof/` form, with or without leading/trailing slashes.
// Comment lines never count.
func gitignoreCoversRuns(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSuffix(strings.TrimPrefix(line, "/"), "/")
		if line == ".zprof/runs" || line == ".zprof" {
			return true
		}
	}
	return false
}

// checkRunLogs warns when run logs pile up. They are gitignored and
// harmless, but a large pile makes the tail-read habit expensive.
func checkRunLogs(projectDir string) []Issue {
	runs := filepath.Join(projectDir, ".zprof", "runs")
	entries, err := os.ReadDir(runs)
	if err != nil {
		return nil
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	if n > runLogWarnThreshold {
		return []Issue{{
			Level:   LevelWarn,
			Path:    runs,
			Message: fmt.Sprintf("%d run logs (> %d) — consider cleaning up .zprof/runs/", n, runLogWarnThreshold),
		}}
	}
	return nil
}

// checkAgentlogGitignored warns when the telemetry collector's data
// directory can end up committed. Mirrors checkRunsGitignored: .agentlog/
// holds per-machine collector output that may contain transcripts and other
// secrets (design §4.3, §16) — useless in history and a leak risk if
// committed.
//
// A project with no .gitignore at all is only flagged once .agentlog/
// actually exists — before that there is nothing to leak, and the project
// may not even be a git repo.
func checkAgentlogGitignored(projectDir string) []Issue {
	warn := func(path string) []Issue {
		return []Issue{{
			Level:   LevelWarn,
			Path:    path,
			Message: "`.agentlog/` is not in .gitignore — telemetry logs (which may contain transcripts and secrets) will be committed; add the entry or run `zprof apply` again",
		}}
	}
	p := filepath.Join(projectDir, ".gitignore")
	data, err := os.ReadFile(p)
	if err != nil {
		if info, statErr := os.Stat(filepath.Join(projectDir, ".agentlog")); statErr == nil && info.IsDir() {
			return warn(projectDir)
		}
		return nil
	}
	if gitignoreCoversAgentlog(string(data)) {
		return nil
	}
	return warn(p)
}

// gitignoreCoversAgentlog reports whether any active .gitignore pattern
// excludes .agentlog/. Accepts the exact entry apply writes
// (ensureGitignore, cli/internal/apply/engine.go), with or without
// leading/trailing slashes. Comment lines never count.
func gitignoreCoversAgentlog(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSuffix(strings.TrimPrefix(line, "/"), "/")
		if line == ".agentlog" {
			return true
		}
	}
	return false
}

// checkAgentlogNotTracked errors when files under .agentlog/ are tracked by
// git. This is a different failure than checkAgentlogGitignored: a file
// `git add`ed before the .gitignore entry existed stays tracked forever,
// .gitignore entry or not (design §4.3, §16) — those files (which may
// contain transcripts or other secrets) keep shipping in every commit.
//
// Silent whenever git itself can't answer — not a repo, git not installed,
// or any other failure to run `git ls-files`. There's nothing to diagnose
// without a working git, and other checks own reporting a missing git.
func checkAgentlogNotTracked(projectDir string) []Issue {
	out, err := exec.Command("git", "-C", projectDir, "ls-files", ".agentlog/").Output()
	if err != nil {
		return nil
	}
	tracked := strings.TrimSpace(string(out))
	if tracked == "" {
		return nil
	}
	files := strings.Split(tracked, "\n")
	return []Issue{{
		Level: LevelError,
		Path:  filepath.Join(projectDir, ".agentlog"),
		Message: fmt.Sprintf(
			"%d file(s) under .agentlog/ are tracked by git even though the directory should be gitignored — untrack them with `git rm -r --cached .agentlog` (they may contain transcripts or secrets): %s",
			len(files), strings.Join(files, ", ")),
	}}
}

// telemetryHookEvents are the three Claude Code hook events zprof wires up
// to drive zprof-collect.py. Kept as a local literal — mirroring
// internal/apply.telemetryHooks's keys — rather than importing internal/apply
// for three string constants.
var telemetryHookEvents = []string{"SubagentStop", "Stop", "SessionStart"}

// checkTelemetryHooks warns when settings.local.json doesn't wire up all
// three telemetry hooks with a zprof-collect.py command.
//
// Gated on the collector script actually being deployed
// (.claude/zprof-collect.py) — same reasoning as checkTaskRunner's agentsDir
// gate: a project that never applied a telemetry-shipping base profile has
// nothing for the hooks to call, so there's nothing to warn about yet.
func checkTelemetryHooks(projectDir string) []Issue {
	if _, err := os.Stat(filepath.Join(projectDir, ".claude", "zprof-collect.py")); err != nil {
		return nil
	}

	p := filepath.Join(projectDir, ".claude", "settings.local.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return []Issue{{
			Level:   LevelWarn,
			Path:    p,
			Message: "settings.local.json is missing — telemetry hooks (SubagentStop, Stop, SessionStart) are not installed; run `zprof apply`",
		}}
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return []Issue{{
			Level:   LevelWarn,
			Path:    p,
			Message: fmt.Sprintf("settings.local.json failed to parse: %v — cannot verify telemetry hooks are installed", err),
		}}
	}
	hooks, _ := settings["hooks"].(map[string]any)
	var missing []string
	for _, event := range telemetryHookEvents {
		if !hookArrayHasScript(hooks[event], "zprof-collect.py") {
			missing = append(missing, event)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []Issue{{
		Level:   LevelWarn,
		Path:    p,
		Message: fmt.Sprintf("settings.local.json is missing telemetry hooks for %s — run `zprof apply` to (re)install the zprof-collect.py hooks", strings.Join(missing, ", ")),
	}}
}

// hookArrayHasScript reports whether a settings.local.json hooks[event]
// value already contains an invocation of script (e.g. "zprof-collect.py"
// or "zprof-guard.py"). Mirrors internal/apply.hasZprofHook's
// marshal-and-substring-check approach.
func hookArrayHasScript(v any, script string) bool {
	entries, ok := v.([]any)
	if !ok {
		return false
	}
	for _, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), script) {
			return true
		}
	}
	return false
}

// guardHookEvents are the two Claude Code hook events zprof wires up to
// drive zprof-guard.py (ADR 0009, design §8.4). Kept as a local literal —
// mirroring internal/apply.guardHooks's keys — rather than importing
// internal/apply for two string constants (same reasoning as
// telemetryHookEvents above).
var guardHookEvents = []string{"PreToolUse", "SubagentStop"}

// checkGuardHooks warns when settings.local.json doesn't wire up both guard
// hooks with a zprof-guard.py command.
//
// Gated on the guard script actually being deployed
// (.claude/zprof-guard.py) — same reasoning as checkTelemetryHooks's gate: a
// project that never applied a guard-shipping base profile has nothing for
// the hooks to call, so there's nothing to warn about yet.
func checkGuardHooks(projectDir string) []Issue {
	if _, err := os.Stat(filepath.Join(projectDir, ".claude", "zprof-guard.py")); err != nil {
		return nil
	}

	p := filepath.Join(projectDir, ".claude", "settings.local.json")
	data, err := os.ReadFile(p)
	var hooks map[string]any
	if err == nil {
		var settings map[string]any
		if unmarshalErr := json.Unmarshal(data, &settings); unmarshalErr != nil {
			return []Issue{{
				Level:   LevelWarn,
				Path:    p,
				Message: fmt.Sprintf("settings.local.json failed to parse: %v — cannot verify guard hooks are installed", unmarshalErr),
			}}
		}
		hooks, _ = settings["hooks"].(map[string]any)
	}
	// A missing settings.local.json and a missing "hooks" key are treated
	// the same way: both events are absent, hooks stays nil either way.

	var missing []string
	for _, event := range guardHookEvents {
		if !hookArrayHasScript(hooks[event], "zprof-guard.py") {
			missing = append(missing, event)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []Issue{{
		Level:   LevelWarn,
		Path:    p,
		Message: fmt.Sprintf("guard hooks missing for %s; run `zprof apply`", strings.Join(missing, ", ")),
	}}
}

// guardConfigRules is the shape checkGuardConfig needs from guard.json to
// tell whether the merged ruleset is non-empty. Mirrors the `rules` key
// apply/guard.go's guardDoc.render() writes.
type guardConfigRules struct {
	Rules []map[string]any `json:"rules"`
}

// checkGuardConfig warns about .claude/guard.json's presence, parseability
// and content. Unlike checkGuardHooks, this is not gated on
// .claude/zprof-guard.py existing: apply writes both files together
// whenever a base profile ships GuardScript/GuardSchema, but guard.json can
// in principle exist on its own, and doctor reports what's actually on
// disk rather than assume the pairing (see plan-issue-29.md item 4).
func checkGuardConfig(projectDir string) []Issue {
	p := filepath.Join(projectDir, ".claude", "guard.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return []Issue{{
			Level:   LevelWarn,
			Path:    p,
			Message: "guard.json is missing — run `zprof apply`",
		}}
	}
	var cfg guardConfigRules
	if err := json.Unmarshal(data, &cfg); err != nil {
		return []Issue{{
			Level:   LevelWarn,
			Path:    p,
			Message: fmt.Sprintf("guard.json failed to parse: %v", err),
		}}
	}
	if len(cfg.Rules) == 0 {
		return []Issue{{
			Level:   LevelWarn,
			Path:    p,
			Message: "guard.json has no rules — guard will never deny anything; run `zprof apply`",
		}}
	}
	return nil
}

// guardConfigDeny is the shape checkPermissionsDeny needs from guard.json.
type guardConfigDeny struct {
	PermissionsDeny []string `json:"permissions_deny"`
}

// settingsPermissionsDeny is the shape checkPermissionsDeny needs from
// settings.local.json.
type settingsPermissionsDeny struct {
	Permissions struct {
		Deny []string `json:"deny"`
	} `json:"permissions"`
}

// checkPermissionsDeny warns when settings.local.json's permissions.deny
// list is missing entries guard.json's permissions_deny declares. It
// self-gates on guard.json being readable and non-empty: an unreadable or
// unparseable guard.json is already reported by checkGuardConfig, and an
// empty permissions_deny has nothing to compare against — either way this
// check stays silent rather than duplicate or manufacture a warning.
func checkPermissionsDeny(projectDir string) []Issue {
	guardPath := filepath.Join(projectDir, ".claude", "guard.json")
	data, err := os.ReadFile(guardPath)
	if err != nil {
		return nil
	}
	var guardCfg guardConfigDeny
	if err := json.Unmarshal(data, &guardCfg); err != nil {
		return nil
	}
	if len(guardCfg.PermissionsDeny) == 0 {
		return nil
	}

	settingsPath := filepath.Join(projectDir, ".claude", "settings.local.json")
	var current []string
	if data, err := os.ReadFile(settingsPath); err == nil {
		var settingsCfg settingsPermissionsDeny
		if json.Unmarshal(data, &settingsCfg) == nil {
			current = settingsCfg.Permissions.Deny
		}
	}
	// A missing or unparseable settings.local.json is treated as an empty
	// deny list — checkGuardHooks/checkTelemetryHooks already diagnose a
	// malformed settings.local.json; this check would only duplicate that.

	have := make(map[string]bool, len(current))
	for _, d := range current {
		have[d] = true
	}
	var missing []string
	for _, d := range guardCfg.PermissionsDeny {
		if !have[d] {
			missing = append(missing, d)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []Issue{{
		Level: LevelWarn,
		Path:  settingsPath,
		Message: fmt.Sprintf(
			"permissions.deny is missing %d value(s) from guard.json's permissions_deny — run `zprof apply`: %s",
			len(missing), strings.Join(missing, ", ")),
	}}
}

// checkGuardDeployment is the single gate on guard.enabled: false. When a
// project's .zprof.yaml explicitly disables guard, checkGuardHooks/
// checkGuardConfig/checkPermissionsDeny never run — a project that opted
// out shouldn't be nagged about a deployment it declined. proj.Guard == nil
// means guard is on by default (GuardConfig.IsEnabled), so this reports
// nothing unless the project layer is present and explicitly says false.
func checkGuardDeployment(projectDir string, proj *manifest.ProjectManifest) []Issue {
	if proj.Guard != nil && !proj.Guard.IsEnabled() {
		return []Issue{{Level: LevelInfo, Message: "guard disabled by project config"}}
	}
	var out []Issue
	out = append(out, checkGuardHooks(projectDir)...)
	out = append(out, checkGuardConfig(projectDir)...)
	out = append(out, checkPermissionsDeny(projectDir)...)
	return out
}

// python3CheckTimeout bounds checkPython3Available so a broken python3 —
// the exact failure mode it's checking for, see below — can't hang `zprof
// doctor` itself.
const python3CheckTimeout = 5 * time.Second

// checkPython3Available warns when `python3 -c 'pass'` fails or hangs. On
// macOS without Xcode Command Line Tools, /usr/bin/python3 is a shim that
// opens a GUI "install command line developer tools" dialog instead of
// running — which silently hangs any hook that shells out to it (design
// §20). This is exactly the check `apply` runs before installing hooks;
// doctor re-runs it so drift (e.g. CLT uninstalled after apply) is caught
// too.
func checkPython3Available() []Issue {
	ctx, cancel := context.WithTimeout(context.Background(), python3CheckTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, "python3", "-c", "pass").Run(); err != nil {
		return []Issue{{
			Level:   LevelWarn,
			Message: "`python3 -c 'pass'` failed or timed out — telemetry hooks will not run; on macOS without Xcode Command Line Tools, /usr/bin/python3 opens a GUI install dialog instead of executing, which hangs any hook that calls it",
		}}
	}
	return nil
}

// checkAuditConfig warns when audit.enabled is true but the auditor agent
// files are missing from .claude/agents/. Without them task-runner tries to
// dispatch a role that doesn't exist, producing an immediate verdict: failed.
func checkAuditConfig(projectDir string, proj *manifest.ProjectManifest) []Issue {
	if !proj.AuditEnabled() {
		return nil
	}
	agentsDir := filepath.Join(projectDir, ".claude", "agents")
	var out []Issue
	for _, name := range []string{"auditor.md", "auditor-deep.md"} {
		if _, err := os.Stat(filepath.Join(agentsDir, name)); err != nil {
			out = append(out, Issue{
				Level:   LevelWarn,
				Path:    filepath.Join(agentsDir, name),
				Message: fmt.Sprintf("audit.enabled is true but %s is missing — task-runner cannot dispatch the auditor; run `zprof apply`", name),
			})
		}
	}
	return out
}

// checkRunnerBudget warns when task-runner's global dispatch budget
// (manifest.ProjectManifest.RunnerMaxDispatches) is too small to survive a
// single executor+auditor+retry cycle, and flags the deprecated
// audit.max_dispatches alias. Runs unconditionally — the runner's dispatch
// counter is active regardless of audit.enabled, so this check must not be
// gated on it either.
func checkRunnerBudget(proj *manifest.ProjectManifest) []Issue {
	var out []Issue
	if budget := proj.RunnerMaxDispatches(); budget < 3 {
		out = append(out, Issue{
			Level:   LevelWarn,
			Message: fmt.Sprintf("runner.max_dispatches is %d — at least 3 needed for a single executor+auditor+retry cycle", budget),
		})
	}
	if proj.Audit != nil && proj.Audit.MaxDispatches > 0 {
		out = append(out, Issue{
			Level: LevelWarn,
			Message: fmt.Sprintf(
				"audit.max_dispatches is deprecated — use runner.max_dispatches instead (effective budget: %d, via RunnerMaxDispatches())",
				proj.RunnerMaxDispatches()),
		})
	}
	return out
}

// checkAgentlogCleanVulnerability reminds that .agentlog/ — gitignored and
// living in the project's working tree by design, so the whole project can
// be `cp -r`'d as one unit (design §4.3) — is destroyed by `git clean -xdf`
// along with every other untracked file. Info level: this is an accepted,
// documented risk with a cheap mitigation (back it up first), not a defect
// to fix.
//
// Only fires once there's something to lose — an empty or absent directory
// has nothing `git clean` could destroy.
func checkAgentlogCleanVulnerability(projectDir string) []Issue {
	dir := filepath.Join(projectDir, ".agentlog")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return nil
	}
	return []Issue{{
		Level:   LevelInfo,
		Path:    dir,
		Message: "`.agentlog/` is gitignored and lives in the working tree — `git clean -xdf` deletes it along with everything else untracked; back it up first (`cp -r .agentlog /somewhere`)",
	}}
}

// checkRoleResolution reports whether any subagent transcript metadata is on
// record yet for this project under ~/.claude/projects/<slug>/. It is
// informational, not a defect: a brand-new project simply hasn't run any
// subagent yet. HOME is read via os.Getenv rather than os.UserHomeDir so
// tests can override it with t.Setenv; the slug algorithm mirrors
// internal/eval.LocateSession's cwd-based rule (kept as a local literal —
// doctor doesn't otherwise depend on internal/eval for one line).
func checkRoleResolution(projectDir string) []Issue {
	unverified := []Issue{{
		Level:   LevelInfo,
		Message: "role resolution unverified: no subagent meta found yet",
	}}

	home := os.Getenv("HOME")
	if home == "" {
		return nil
	}
	slug := "-" + strings.ReplaceAll(strings.TrimPrefix(projectDir, "/"), "/", "-")
	matches, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", slug, "*", "subagents", "*.meta.json"))
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		var meta map[string]any
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		if _, ok := meta["agentType"]; ok {
			return nil
		}
	}
	return unverified
}
