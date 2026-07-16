package manifest

import (
	"errors"
	"fmt"
	"os"

	"github.com/alcherk/zprof/internal/models"
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
	return os.WriteFile(path, data, 0o644)
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
