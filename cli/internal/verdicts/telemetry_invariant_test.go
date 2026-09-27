package verdicts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vaporphd/zprof/internal/score"
	"gopkg.in/yaml.v3"
)

// telemetryFixture is the tiny slice of profiles/base/telemetry.yaml this
// test needs — a hand-rolled struct rather than the collector's own parser
// (which is Python, stdlib-only, and lives in a different language) or the
// Go score package's schemaFile (unexported, and shaped for schema.json's
// JSON, not the YAML source).
type telemetryFixture struct {
	ReviewBlockVerdicts []string `yaml:"review_block_verdicts"`
	VerdictExemptRoles  []string `yaml:"verdict_exempt_roles"`
}

func setKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestReviewBlockVerdictsConsistentAcrossSources is the D6 invariant test
// (ADR 0003 §D6): telemetry.yaml's review_block_verdicts must equal the set
// of `roles.reviewer` tokens in verdicts.yaml whose base is failed/blocked
// (universal tokens included, e.g. `blocked`), score.Defaults() must mirror
// telemetry.yaml exactly, and verdict_exempt_roles must be empty now that
// doctor guarantees every role's return_format starts with `verdict:`.
func TestReviewBlockVerdictsConsistentAcrossSources(t *testing.T) {
	root := repoRoot(t)

	reg, err := Load(filepath.Join(root, "profiles", "base", "verdicts.yaml"))
	require.NoError(t, err)
	require.NoError(t, reg.Validate())

	reviewer := reg.Normalized().Roles["reviewer"]
	require.NotEmpty(t, reviewer, "registry must define the reviewer role")
	want := map[string]bool{}
	for token, spec := range reviewer {
		if spec.Base == "failed" || spec.Base == "blocked" {
			want[token] = true
		}
	}

	data, err := os.ReadFile(filepath.Join(root, "profiles", "base", "telemetry.yaml"))
	require.NoError(t, err)
	var tel telemetryFixture
	require.NoError(t, yaml.Unmarshal(data, &tel))

	require.ElementsMatch(t, setKeys(want), tel.ReviewBlockVerdicts,
		"telemetry.yaml review_block_verdicts must equal verdicts.yaml roles.reviewer's failed/blocked tokens")

	defaults := score.Defaults()
	require.Equal(t, want, defaults.ReviewBlockVerdicts,
		"score.Defaults().ReviewBlockVerdicts must mirror telemetry.yaml (config.go comment says so)")

	require.Empty(t, tel.VerdictExemptRoles,
		"verdict_exempt_roles must be empty since #20 — doctor guarantees verdict: on every role")
	require.Empty(t, defaults.ExemptRoles,
		"score.Defaults().ExemptRoles must mirror the now-empty verdict_exempt_roles")
}
