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
	Router       string   `yaml:"router"`
	Roles        []string `yaml:"roles"`
	ToolAgents   []string `yaml:"tool_agents"`
	// Gitignore is the list of extra `.gitignore` entries this overlay
	// contributes. The apply engine unions them with the base entries and
	// appends any that are not already present. Order-preserving.
	Gitignore []string `yaml:"gitignore"`
	// Executing maps agent name -> file-scope description (a comma-separated
	// glob list rendered verbatim in CLAUDE.md's "## Executing" table). When
	// present, the apply engine renders one row per entry; when absent the
	// legacy detect-globs fallback is used, which is imprecise for overlays
	// with multiple executing agents.
	Executing map[string]string `yaml:"executing"`
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
	// loop_template is optional (base manifest doesn't consume it) but if set
	// it must resolve to a known template — the apply engine reads it to pick
	// which workflows/<name>.md file to compose overlay extensions into.
	if m.LoopTemplate != "" && !validLoopTemplates[m.LoopTemplate] {
		return fmt.Errorf("loop_template must be one of: dev-pipeline, exploratory (got %q)", m.LoopTemplate)
	}
	return nil
}
