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
}

// ABExperiment defines the control and candidate models for a role.
type ABExperiment struct {
	Control   string `yaml:"control"`
	Candidate string `yaml:"candidate"`
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
