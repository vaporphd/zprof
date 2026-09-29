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
	"strings"

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
	// ReviewBlockVerdicts: reviewer verdicts that send work back (P5).
	ReviewBlockVerdicts map[string]bool
	// P2Exempt: compiled patterns matched against a Bash target (after
	// stripping one leading rtk wrapper prefix) that must not count toward
	// P2 blind retries — sleep/wait loops used to poll an async child
	// (issue #53). A timed-out `sleep` is Bash-tool timeout hygiene, not a
	// blind retry of a failed command.
	P2Exempt []*regexp.Regexp
	// P2ExemptPatterns: the raw (pre-compile) source for P2Exempt, kept
	// around only so WeightsHash can fold it in — see WeightsHash.
	P2ExemptPatterns []string
}

var defaultMutatingBash = []string{
	`\s>>?\s`,
	`\bsed\s+-i\b`,
	`\btee\b`,
	`\b(mv|cp|rm|touch|mkdir)\b`,
	// `ln` (symlink/hardlink) kept as its own pattern, not folded into the
	// mv|cp|rm|touch|mkdir family above: `\bln\b` false-positives on `ls
	// -ln`/`sed -n 1,5p ln.go`/`python3 x.py --ln`. A negative-lookahead
	// form was also rejected: RE2 (no lookaround support) can't parse it.
	// This char-class form needs no lookaround and mirrors telemetry.yaml
	// `mutating_bash_patterns` exactly (issue #73). Note: `compilePatterns`
	// used to silently drop any pattern it failed to compile instead of
	// erroring — that gap is what let issue #79 (two RE2-incompatible
	// lookahead patterns in telemetry.yaml) vanish from `zprof score`
	// unnoticed; `compilePatterns`/`LoadConfig` now fail loud instead.
	`(?:^|[^\w.-])ln(?:$|[^\w.-])`,
	`\bgit\s+(commit|checkout|stash|reset|apply|cherry-pick|merge|rebase)\b`,
	`\bxcodegen\b`,
	`\b(cargo|go|swift)\s+fmt\b`,
	`\b(gofmt\s+-w|swiftformat|rustfmt)\b`,
}

// defaultP2Exempt mirrors telemetry.yaml `p2_exempt_patterns`; keep the two
// in sync (issue #53).
var defaultP2Exempt = []string{
	`^\s*(sleep|wait)\b`,
}

// rtkPrefixes mirrors zprof-guard.py `_RTK_PREFIXES` (ADR D7 §4) so a
// `rtk`-wrapped sleep/wait still matches P2Exempt.
var rtkPrefixes = []string{"rtk proxy ", "rtk "}

// stripRtkPrefix removes one leading rtk wrapper prefix, if present.
func stripRtkPrefix(cmd string) string {
	trimmed := strings.TrimLeft(cmd, " \t")
	for _, p := range rtkPrefixes {
		if strings.HasPrefix(trimmed, p) {
			return trimmed[len(p):]
		}
	}
	return trimmed
}

// Defaults mirrors telemetry.yaml `score_defaults`; keep the two in sync.
func Defaults() Config {
	c := Config{
		Enabled:       true,
		Weights:       map[string]float64{"P1": 20, "P2": 15, "P3": 10, "P4": 20, "P5": 10, "P6": 15, "P7": 10},
		Saturation:    map[string]float64{"P1": 0.20, "P2": 3, "P3": 0.5, "P4": 2, "P5": 2, "P6": 0.30, "P7": 4},
		Thresholds:    Thresholds{Ideal: 85, Solid: 60},
		MutatingTools: map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true},
		// Empty since #20 (ADR 0003): doctor now guarantees every role's
		// return_format starts with `verdict:` (auditor/auditor-deep
		// migrated off `completion:`), so P6/P7 no longer need to look away
		// from these roles. The mechanism stays for schema.json overrides on
		// projects with an older, unmigrated auditor.
		ExemptRoles: map[string]bool{},
		// mirrors telemetry.yaml `review_block_verdicts`
		ReviewBlockVerdicts: map[string]bool{
			"block": true, "changes-requested": true, "awaiting-approval": true,
			"failed": true, "blocked": true,
		},
	}
	c.MutatingBash = mustCompilePatterns(defaultMutatingBash)
	c.P2ExemptPatterns = defaultP2Exempt
	c.P2Exempt = mustCompilePatterns(defaultP2Exempt)
	return c
}

// compilePatterns compiles each pattern and returns an error naming the
// first one regexp.Compile rejects, instead of silently dropping it from
// the result. Silently dropping is what let issue #79 through: two
// telemetry.yaml `mutating_bash_patterns` entries used a negative
// lookahead, which Go's RE2 engine cannot parse (`ErrInvalidPerlOp`), and
// the old version of this function just skipped them — `zprof score` kept
// running, but stopped counting any git-mutation command at all, with no
// error and no log line anywhere.
func compilePatterns(pats []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(pats))
	for i, p := range pats {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("pattern[%d] %q: %w", i, p, err)
		}
		out = append(out, re)
	}
	return out, nil
}

// mustCompilePatterns compiles a hardcoded, compile-time-constant pattern
// list (defaultMutatingBash, defaultP2Exempt) and panics if one fails to
// compile — the same contract as regexp.MustCompile used elsewhere in this
// codebase. Defaults() has no error return (it is the zero-config fallback
// every caller relies on never failing) and these lists are under our own
// control, covered by TestRe2Compat-style tests, so a panic here can only
// mean a programming mistake, never a runtime/user-data problem.
func mustCompilePatterns(pats []string) []*regexp.Regexp {
	out, err := compilePatterns(pats)
	if err != nil {
		panic(fmt.Sprintf("score: default pattern list: %v", err))
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

// IsP2Exempt reports whether a tool event must be excluded from the P2
// blind-retry counter — currently sleep/wait Bash commands used to poll an
// async child (issue #53). The target is matched after stripping one
// leading rtk wrapper prefix, so `rtk proxy sleep 180` exempts the same as
// `sleep 180`.
func (c Config) IsP2Exempt(tool, target string) bool {
	if tool != "Bash" {
		return false
	}
	cmd := stripRtkPrefix(target)
	for _, re := range c.P2Exempt {
		if re.MatchString(cmd) {
			return true
		}
	}
	return false
}

// schemaFile is the subset of .agentlog/schema.json (telemetry.yaml as JSON) we read.
type schemaFile struct {
	MutatingBashPatterns []string `json:"mutating_bash_patterns"`
	P2ExemptPatterns     []string `json:"p2_exempt_patterns"`
	VerdictExemptRoles   []string `json:"verdict_exempt_roles"`
	ReviewBlockVerdicts  []string `json:"review_block_verdicts"`
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
			re, err := compilePatterns(s.MutatingBashPatterns)
			if err != nil {
				return c, fmt.Errorf("compile mutating_bash_patterns: %w", err)
			}
			c.MutatingBash = re
		}
		if len(s.P2ExemptPatterns) > 0 {
			re, err := compilePatterns(s.P2ExemptPatterns)
			if err != nil {
				return c, fmt.Errorf("compile p2_exempt_patterns: %w", err)
			}
			c.P2ExemptPatterns = s.P2ExemptPatterns
			c.P2Exempt = re
		}
		if len(s.VerdictExemptRoles) > 0 {
			c.ExemptRoles = map[string]bool{}
			for _, r := range s.VerdictExemptRoles {
				c.ExemptRoles[r] = true
			}
		}
		if len(s.ReviewBlockVerdicts) > 0 {
			c.ReviewBlockVerdicts = map[string]bool{}
			for _, v := range s.ReviewBlockVerdicts {
				c.ReviewBlockVerdicts[v] = true
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
//
// P2ExemptPatterns is folded in (issue #53): sleep/wait exemption changes
// what P2 actually counts, not just its weight/saturation, so a
// scores.jsonl row scored under a different exempt set must not be treated
// as comparable to one scored under this one. This also means every row
// written after this change carries a hash disjoint from every
// pre-#53 row (the field is empty/absent there), which is the desired
// "don't compare old P2 semantics to new" behavior without any special
// pre/post migration logic.
func (c Config) WeightsHash() string {
	payload := struct {
		Weights          map[string]float64 `json:"weights"`
		Saturation       map[string]float64 `json:"saturation"`
		Thresholds       Thresholds         `json:"thresholds"`
		P2ExemptPatterns []string           `json:"p2_exempt_patterns"`
	}{c.Weights, c.Saturation, c.Thresholds, c.P2ExemptPatterns}
	data, _ := json.Marshal(payload)
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])[:12]
}
