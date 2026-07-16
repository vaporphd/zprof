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

// OverlayManifest describes an overlay's manifest.yaml (overlays/<name>/manifest.yaml).
type OverlayManifest struct {
	Name         string   `yaml:"name"`
	DisplayName  string   `yaml:"display_name"`
	Version      string   `yaml:"version"`
	LoopTemplate string   `yaml:"loop_template"`
	RequiresBase string   `yaml:"requires_base"`
	Roles        []string `yaml:"roles"`
	ToolAgents   []string `yaml:"tool_agents"`
}

// LoadOverlay reads and validates an overlay manifest.yaml file at path.
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
