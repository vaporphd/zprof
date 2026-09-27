package manifest

import (
	"errors"
	"fmt"
	"os"

	"github.com/vaporphd/zprof/internal/fsutil"
	"github.com/vaporphd/zprof/internal/models"
	"gopkg.in/yaml.v3"
)

// ErrNoOverride is returned by ResolvedModel when the given role has no
// entry in ModelOverrides. Callers should fall back to the overlay default.
var ErrNoOverride = errors.New("no model override set for role")

// ProjectManifest describes a project's .zprof.yaml state file: which
// overlays are active, language/gate preferences, and any per-role
// model/agent overrides.
type ProjectManifest struct {
	Overlays       []string          `yaml:"overlays"`
	Language       string            `yaml:"language"`
	WithGates      bool              `yaml:"with_gates"`
	Minimal        bool              `yaml:"minimal"`
	ModelOverrides map[string]string `yaml:"model_overrides,omitempty"`
	AgentOverrides map[string]string `yaml:"agent_overrides,omitempty"`

	// ManagedAgents lists the agent names zprof itself wrote on the last
	// apply. Names present here but absent from the current sources are
	// orphans from an earlier profile version and get pruned on the next
	// apply; anything not listed is user-authored and never touched.
	ManagedAgents []string `yaml:"managed_agents,omitempty"`

	// ABExperiments configures A/B tier experiments per role.
	// task-runner calls zprof-collect.py pick-arm to get the model.
	ABExperiments map[string]ABExperiment `yaml:"ab_experiments,omitempty"`

	// Audit configures the MEA-style step auditor in task-runner's loop.
	Audit *AuditConfig `yaml:"audit,omitempty"`

	// Runner controls task-runner's global dispatch budget — independent
	// of the audit subsystem, active regardless of audit.enabled.
	Runner *RunnerConfig `yaml:"runner,omitempty"`

	// Score configures `zprof score` (per-task scorecard). Nil = defaults
	// from telemetry.yaml / the compiled-in table; enabled unless
	// `enabled: false` is set explicitly.
	Score *ScoreConfig `yaml:"score,omitempty"`
}

// AuditConfig controls the blocking auditor in task-runner's dispatch loop.
// When Enabled is false (the default), task-runner behaves identically to
// the pre-auditor version — no auditor dispatches, no Requirements section.
type AuditConfig struct {
	Enabled       bool              `yaml:"enabled"`
	MaxDispatches int               `yaml:"max_dispatches,omitempty"`
	ModelByRole   map[string]string `yaml:"model_by_role,omitempty"`
}

// RunnerConfig controls task-runner's global dispatch budget —
// independent of the audit subsystem, active regardless of audit.enabled.
type RunnerConfig struct {
	MaxDispatches int `yaml:"max_dispatches,omitempty"`
}

// ABExperiment defines the control and candidate models for a role.
type ABExperiment struct {
	Control   string `yaml:"control"`
	Candidate string `yaml:"candidate"`
}

// ScoreConfig overrides scorecard weights, saturation points and tier thresholds.
// Only keys present override; the rest fall back to defaults.
type ScoreConfig struct {
	Enabled    *bool              `yaml:"enabled,omitempty"`
	Weights    map[string]float64 `yaml:"weights,omitempty"`
	Saturation map[string]float64 `yaml:"saturation,omitempty"`
	Thresholds *ScoreThresholds   `yaml:"thresholds,omitempty"`
}

// ScoreThresholds are tier cut-offs; zero means "not set".
type ScoreThresholds struct {
	Ideal int `yaml:"ideal,omitempty"`
	Solid int `yaml:"solid,omitempty"`
}

// LoadProject reads and parses a project manifest (.zprof.yaml) at path.
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

// Save writes the project manifest to path as YAML.
func (m *ProjectManifest) Save(path string) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data, 0o644)
}

// CarryOverFrom copies the fields of a previously saved manifest that a
// fresh apply must not lose. Overlays/Language/WithGates/Minimal are
// deliberately NOT carried over — those come from the command line and
// describe the apply being requested right now.
//
// ManagedAgents is the load-bearing one: Apply overwrites it with the
// roster it just wrote, so a caller that builds a fresh manifest without
// carrying the previous value forward leaves PruneOrphanAgents blind —
// every namespaced agent from a dropped overlay stays in .claude/agents/
// as a valid, dispatchable file that nothing will ever remove.
//
// Only unset (nil) fields are filled, so an explicitly built manifest can
// still override any of them.
func (m *ProjectManifest) CarryOverFrom(prev *ProjectManifest) {
	if prev == nil {
		return
	}
	if m.ModelOverrides == nil {
		m.ModelOverrides = prev.ModelOverrides
	}
	if m.AgentOverrides == nil {
		m.AgentOverrides = prev.AgentOverrides
	}
	if m.ManagedAgents == nil {
		m.ManagedAgents = prev.ManagedAgents
	}
	if m.Audit == nil {
		m.Audit = prev.Audit
	}
	if m.Runner == nil {
		m.Runner = prev.Runner
	}
	if m.Score == nil {
		m.Score = prev.Score
	}
}

// AuditEnabled reports whether the blocking auditor is active.
func (m *ProjectManifest) AuditEnabled() bool {
	return m.Audit != nil && m.Audit.Enabled
}

// defaultRunnerMaxDispatches covers the longest base feature route —
// planner, architect, implementer, tester, wiki-keeper, reviewer,
// pr-shepherd = 7 dispatches — plus three tester<->implementer retry
// rounds (2 dispatches per round: implementer retry + tester recheck)
// plus one non-schema-response retry:
//
//	7 + 3*2 + 1 = 14
const defaultRunnerMaxDispatches = 14

// AuditMaxDispatches returns the dispatch budget for a single run.
// Returns 7 when unconfigured.
//
// Deprecated: the global dispatch budget is now sourced via
// RunnerMaxDispatches(); this method stays for backward-compat callers.
func (m *ProjectManifest) AuditMaxDispatches() int {
	if m.Audit != nil && m.Audit.MaxDispatches > 0 {
		return m.Audit.MaxDispatches
	}
	return 7
}

// RunnerMaxDispatches returns the global dispatch budget for a single
// task-runner run: every executor, auditor, gate and non-schema-retry
// dispatch counts against it, regardless of audit.enabled.
//
// runner.max_dispatches is the source of truth. audit.max_dispatches is
// kept as a deprecated alias for backward compat: if both are set, the
// larger of the two wins (an upgrade never silently shrinks an existing
// budget). Falls back to defaultRunnerMaxDispatches if neither is set.
func (m *ProjectManifest) RunnerMaxDispatches() int {
	runnerVal := 0
	if m.Runner != nil && m.Runner.MaxDispatches > 0 {
		runnerVal = m.Runner.MaxDispatches
	}
	auditVal := 0
	if m.Audit != nil && m.Audit.MaxDispatches > 0 {
		auditVal = m.Audit.MaxDispatches
	}
	switch {
	case runnerVal > 0 && auditVal > 0:
		if runnerVal >= auditVal {
			return runnerVal
		}
		return auditVal
	case runnerVal > 0:
		return runnerVal
	case auditVal > 0:
		return auditVal
	default:
		return defaultRunnerMaxDispatches
	}
}

// ResolvedModel returns the exact model ID for a role from ModelOverrides.
// Returns ErrNoOverride if the role has no override (caller falls back to
// the overlay default).
func (m *ProjectManifest) ResolvedModel(role string) (string, error) {
	raw, ok := m.ModelOverrides[role]
	if !ok {
		return "", ErrNoOverride
	}
	return models.Resolve(raw)
}
