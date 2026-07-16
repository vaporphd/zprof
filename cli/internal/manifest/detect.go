package manifest

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

var validDetectConfidence = map[string]bool{"high": true, "medium": true, "low": true}

// RegexRule pairs a file path with a regex pattern that must match its contents.
type RegexRule struct {
	Path  string `yaml:"path"`
	Match string `yaml:"match"`
}

// DetectRules describes an overlay's detect rules (overlays/<name>/detect.yaml),
// used to auto-detect whether a project matches this overlay.
type DetectRules struct {
	Name       string
	AnyFile    []string
	AnyRegex   []RegexRule
	Confidence string
}

// detectFile mirrors the on-disk YAML shape, which nests detection rules
// under a `detect:` key.
type detectFile struct {
	Name   string `yaml:"name"`
	Detect struct {
		AnyFile    []string    `yaml:"any_file"`
		AnyRegex   []RegexRule `yaml:"any_regex"`
		Confidence string      `yaml:"confidence"`
	} `yaml:"detect"`
}

// LoadDetect reads and validates a detect.yaml file at path.
func LoadDetect(path string) (*DetectRules, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var f detectFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if !validDetectConfidence[f.Detect.Confidence] {
		return nil, fmt.Errorf("confidence must be one of: high, medium, low (got %q)", f.Detect.Confidence)
	}
	d := &DetectRules{
		Name:       f.Name,
		AnyFile:    f.Detect.AnyFile,
		AnyRegex:   f.Detect.AnyRegex,
		Confidence: f.Detect.Confidence,
	}
	return d, nil
}

// AnyFileList returns the any_file glob patterns.
func (d *DetectRules) AnyFileList() []string { return d.AnyFile }

// AnyRegexList returns the any_regex path/match rules.
func (d *DetectRules) AnyRegexList() []RegexRule { return d.AnyRegex }

// ConfidenceLevel returns the detection confidence level.
func (d *DetectRules) ConfidenceLevel() string { return d.Confidence }
