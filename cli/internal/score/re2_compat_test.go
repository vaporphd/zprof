package score

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// telemetryPatterns is the tiny slice of profiles/base/telemetry.yaml this
// test needs — a hand-rolled struct rather than the unexported schemaFile
// (shaped for schema.json's JSON, not the YAML source), mirroring the
// precedent in cli/internal/verdicts/telemetry_invariant_test.go.
type telemetryPatterns struct {
	MutatingBashPatterns []string `yaml:"mutating_bash_patterns"`
	P2ExemptPatterns     []string `yaml:"p2_exempt_patterns"`
}

// repoRoot locates the zprof repository root from this test file's own
// path (cli/internal/score is three directories under the root), mirroring
// cli/internal/verdicts/repo_test.go's convention.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, f, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root, err := filepath.Abs(filepath.Join(filepath.Dir(f), "..", "..", ".."))
	require.NoError(t, err)
	return root
}

// TestTelemetryPatternsCompileUnderRE2 is the Go-side counterpart of
// profiles/base/telemetry_test.py's mutating_bash_patterns/p2_exempt_patterns
// compile check (issue #79). Python's `re` engine supports Perl-style
// lookaround (`(?!...)`, `(?<!...)`); Go's regexp package is RE2, which
// does not — a pattern that compiles fine under Python `re` can fail
// regexp.Compile with ErrInvalidPerlOp. The Python-side test would not
// have caught issue #79 (both lookahead patterns are valid Python regex);
// this test is the one that would.
func TestTelemetryPatternsCompileUnderRE2(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "profiles", "base", "telemetry.yaml"))
	require.NoError(t, err)

	var tel telemetryPatterns
	require.NoError(t, yaml.Unmarshal(data, &tel))

	require.NotEmpty(t, tel.MutatingBashPatterns)
	for i, p := range tel.MutatingBashPatterns {
		_, err := regexp.Compile(p)
		require.NoErrorf(t, err, "mutating_bash_patterns[%d] %q does not compile under Go RE2", i, p)
	}

	require.NotEmpty(t, tel.P2ExemptPatterns)
	for i, p := range tel.P2ExemptPatterns {
		_, err := regexp.Compile(p)
		require.NoErrorf(t, err, "p2_exempt_patterns[%d] %q does not compile under Go RE2", i, p)
	}
}

// TestCompilePatterns_ErrorsOnBadPattern is the AC3 regression test for
// issue #79: compilePatterns must surface a bad pattern as an error,
// never silently shrink the result.
func TestCompilePatterns_ErrorsOnBadPattern(t *testing.T) {
	_, err := compilePatterns([]string{`\bgit\s+commit\b`, `(?!bad)`, `\btee\b`})
	require.Error(t, err)
	require.Contains(t, err.Error(), "pattern[1]")
}

// TestLoadConfig_SchemaJsonBadPatternErrors is the AC3 end-to-end
// regression test for issue #79: a schema.json (as rendered by `zprof
// apply` from telemetry.yaml) that carries an RE2-incompatible pattern
// must make LoadConfig fail loud, not return a Config whose MutatingBash
// list quietly lost an entry.
func TestLoadConfig_SchemaJsonBadPatternErrors(t *testing.T) {
	proj := t.TempDir()
	agentlog := filepath.Join(proj, ".agentlog")
	require.NoError(t, os.MkdirAll(agentlog, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentlog, "schema.json"),
		[]byte(`{"mutating_bash_patterns": ["\\bgit\\s+commit\\b", "(?!bad)"]}`), 0o644))

	_, err := LoadConfig(proj, agentlog)
	require.Error(t, err)
	require.Contains(t, err.Error(), "mutating_bash_patterns")
}

// TestLoadConfig_FixedGitPatterns_IsMutatingBash is the AC8 end-to-end
// sanity check for issue #79: a schema.json carrying the fixed (RE2-safe)
// telemetry.yaml patterns correctly flags a real git-mutation command as
// mutating and correctly spares a read-only-looking plumbing lookalike —
// the exact regression #79 is about (the pre-fix lookahead patterns
// vanished at compile time, so IsMutatingBash("git commit -m x") silently
// went from true to false).
func TestLoadConfig_FixedGitPatterns_IsMutatingBash(t *testing.T) {
	proj := t.TempDir()
	agentlog := filepath.Join(proj, ".agentlog")
	require.NoError(t, os.MkdirAll(agentlog, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentlog, "schema.json"),
		[]byte(`{"mutating_bash_patterns": [`+
			`"\\bgit\\s+(commit|checkout|reset|apply|cherry-pick|merge|rebase)(?:$|[^\\w-])",`+
			`"\\bgit\\s+stash(?:\\s+(?:push|pop|apply|drop|clear|save|branch|create|store)\\b|\\s+-|\\s*[^\\w\\s-]|\\s*$)"`+
			`]}`), 0o644))

	c, err := LoadConfig(proj, agentlog)
	require.NoError(t, err)

	require.True(t, c.IsMutatingBash("git commit -m x"))
	require.False(t, c.IsMutatingBash("git merge-base HEAD~1"))
	require.True(t, c.IsMutatingBash("git stash pop"))
	require.False(t, c.IsMutatingBash("git stash list"))
}
