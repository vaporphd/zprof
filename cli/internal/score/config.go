// Package score computes the per-task scorecard from .agentlog/ data
// (spec: docs/superpowers/specs/2026-09-26-task-scorecard-design.md).
// It never reads raw Claude Code session logs — only what the Python
// collector already normalized.
package score

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/vaporphd/zprof/internal/manifest"
)

// PenaltyIDs is the fixed order of the seven penalties.
var PenaltyIDs = []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7"}

type Thresholds struct {
	Ideal int `json:"ideal"`
	Solid int `json:"solid"`
}

type Config struct {
	Enabled       bool
	Weights       map[string]float64
	Saturation    map[string]float64
	Thresholds    Thresholds
	MutatingBash  []*regexp.Regexp
	MutatingTools map[string]bool
	ExemptRoles   map[string]bool
}

var defaultMutatingBash = []string{
	`\s>>?\s`,
	`\bsed\s+-i\b`,
	`\btee\b`,
	`\b(mv|cp|rm|touch|mkdir)\b`,
	`\bgit\s+(commit|checkout|stash|reset|apply|cherry-pick|merge|rebase)\b`,
	`\bxcodegen\b`,
	`\b(cargo|go|swift)\s+fmt\b`,
	`\b(gofmt\s+-w|swiftformat|rustfmt)\b`,
}

// Defaults mirrors telemetry.yaml `score_defaults`; keep the two in sync.
func Defaults() Config {
	c := Config{
		Enabled:       true,
		Weights:       map[string]float64{"P1": 20, "P2": 15, "P3": 10, "P4": 20, "P5": 10, "P6": 15, "P7": 10},
		Saturation:    map[string]float64{"P1": 0.20, "P2": 3, "P3": 0.5, "P4": 2, "P5": 2, "P6": 0.30, "P7": 4},
		Thresholds:    Thresholds{Ideal: 85, Solid: 60},
		MutatingTools: map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true},
		ExemptRoles:   map[string]bool{"auditor": true, "auditor-deep": true},
	}
	c.MutatingBash = compilePatterns(defaultMutatingBash)
	return c
}

func compilePatterns(pats []string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, p := range pats {
		if re, err := regexp.Compile(p); err == nil {
			out = append(out, re)
		}
	}
	return out
}

// IsMutatingBash reports whether a Bash command changes files/state.
func (c Config) IsMutatingBash(command string) bool {
	for _, re := range c.MutatingBash {
		if re.MatchString(command) {
			return true
		}
	}
	return false
}

// schemaFile is the subset of .agentlog/schema.json (telemetry.yaml as JSON) we read.
type schemaFile struct {
	MutatingBashPatterns []string `json:"mutating_bash_patterns"`
	VerdictExemptRoles   []string `json:"verdict_exempt_roles"`
	ScoreDefaults        *struct {
		Weights    map[string]float64 `json:"weights"`
		Saturation map[string]float64 `json:"saturation"`
		Thresholds *Thresholds        `json:"thresholds"`
	} `json:"score_defaults"`
}

// LoadConfig layers: compiled defaults ← <agentlogDir>/schema.json ← <projectDir>/.zprof.yaml.
// Missing files are not errors; malformed ones are.
func LoadConfig(projectDir, agentlogDir string) (Config, error) {
	c := Defaults()

	if data, err := os.ReadFile(filepath.Join(agentlogDir, "schema.json")); err == nil {
		var s schemaFile
		if err := json.Unmarshal(data, &s); err != nil {
			return c, fmt.Errorf("parse schema.json: %w", err)
		}
		if len(s.MutatingBashPatterns) > 0 {
			c.MutatingBash = compilePatterns(s.MutatingBashPatterns)
		}
		if len(s.VerdictExemptRoles) > 0 {
			c.ExemptRoles = map[string]bool{}
			for _, r := range s.VerdictExemptRoles {
				c.ExemptRoles[r] = true
			}
		}
		if s.ScoreDefaults != nil {
			mergeFloats(c.Weights, s.ScoreDefaults.Weights)
			mergeFloats(c.Saturation, s.ScoreDefaults.Saturation)
			if s.ScoreDefaults.Thresholds != nil {
				mergeThresholds(&c.Thresholds, *s.ScoreDefaults.Thresholds)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return c, fmt.Errorf("read schema.json: %w", err)
	}

	manifestPath := filepath.Join(projectDir, ".zprof.yaml")
	if _, err := os.Stat(manifestPath); err == nil {
		m, err := manifest.LoadProject(manifestPath)
		if err != nil {
			return c, fmt.Errorf("load .zprof.yaml: %w", err)
		}
		if m.Score != nil {
			if m.Score.Enabled != nil {
				c.Enabled = *m.Score.Enabled
			}
			mergeFloats(c.Weights, m.Score.Weights)
			mergeFloats(c.Saturation, m.Score.Saturation)
			if m.Score.Thresholds != nil {
				mergeThresholds(&c.Thresholds, Thresholds{Ideal: m.Score.Thresholds.Ideal, Solid: m.Score.Thresholds.Solid})
			}
		}
	}
	return c, nil
}

func mergeFloats(dst, src map[string]float64) {
	for k, v := range src {
		if _, known := dst[k]; known {
			dst[k] = v
		}
	}
}

func mergeThresholds(dst *Thresholds, src Thresholds) {
	if src.Ideal > 0 {
		dst.Ideal = src.Ideal
	}
	if src.Solid > 0 {
		dst.Solid = src.Solid
	}
}

// WeightsHash identifies the scoring parameters so historical rows stay comparable.
// encoding/json sorts map keys, so the encoding is canonical.
func (c Config) WeightsHash() string {
	payload := struct {
		Weights    map[string]float64 `json:"weights"`
		Saturation map[string]float64 `json:"saturation"`
		Thresholds Thresholds         `json:"thresholds"`
	}{c.Weights, c.Saturation, c.Thresholds}
	data, _ := json.Marshal(payload)
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])[:12]
}
