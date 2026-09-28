package score

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func fixtureDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "testdata", "run1")
}

func TestDefaults_WeightsSumTo100(t *testing.T) {
	c := Defaults()
	sum := 0.0
	for _, w := range c.Weights {
		sum += w
	}
	require.Equal(t, 100.0, sum)
	require.Len(t, c.Saturation, 7)
	require.Equal(t, Thresholds{Ideal: 85, Solid: 60}, c.Thresholds)
	require.True(t, c.Enabled)
	require.True(t, c.MutatingTools["Edit"])
	require.Empty(t, c.ExemptRoles, "empty since #20: doctor guarantees verdict: on every role")
}

func TestDefaults_MutatingBashExcludesBuildAndTest(t *testing.T) {
	c := Defaults()
	for _, cmd := range []string{"swift test --package-path Packages/Core", "cargo build --release", "go test ./...", "pytest -q", "make test", "git status", "cat foo.txt"} {
		require.False(t, c.IsMutatingBash(cmd), cmd)
	}
	for _, cmd := range []string{"cat > f.txt <<'EOF'", "sed -i 's/a/b/' f", "git commit -m x", "rm -rf build", "xcodegen generate", "echo hi | tee out.log"} {
		require.True(t, c.IsMutatingBash(cmd), cmd)
	}
}

func TestLoadConfig_SchemaJsonThenZprofYaml(t *testing.T) {
	proj := t.TempDir()
	agentlog := filepath.Join(proj, ".agentlog")
	require.NoError(t, os.MkdirAll(agentlog, 0o755))
	schema, err := os.ReadFile(filepath.Join(fixtureDir(), "schema.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(agentlog, "schema.json"), schema, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte(
		"overlays: [ios-swift]\nscore:\n  weights: {P1: 30, P2: 5}\n  thresholds: {ideal: 90}\n"), 0o644))

	c, err := LoadConfig(proj, agentlog)
	require.NoError(t, err)
	require.Equal(t, 30.0, c.Weights["P1"], ".zprof.yaml overrides")
	require.Equal(t, 5.0, c.Weights["P2"])
	require.Equal(t, 10.0, c.Weights["P3"], "untouched keys keep defaults")
	require.Equal(t, 90, c.Thresholds.Ideal)
	require.Equal(t, 60, c.Thresholds.Solid, "partial thresholds override only what is set")
	require.True(t, c.Enabled)
}

func TestLoadConfig_NoFilesUsesDefaults(t *testing.T) {
	c, err := LoadConfig(t.TempDir(), filepath.Join(t.TempDir(), ".agentlog"))
	require.NoError(t, err)
	require.Equal(t, Defaults().Weights, c.Weights)
}

func TestLoadConfig_EnabledFalse(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte("overlays: []\nscore:\n  enabled: false\n"), 0o644))
	c, err := LoadConfig(proj, filepath.Join(proj, ".agentlog"))
	require.NoError(t, err)
	require.False(t, c.Enabled)
}

func TestWeightsHash_StableAndSensitive(t *testing.T) {
	a := Defaults()
	b := Defaults()
	require.Equal(t, a.WeightsHash(), b.WeightsHash())
	require.Len(t, a.WeightsHash(), 12)
	b.Weights["P1"] = 21
	require.NotEqual(t, a.WeightsHash(), b.WeightsHash())
	c := Defaults()
	c.Thresholds.Solid = 61
	require.NotEqual(t, a.WeightsHash(), c.WeightsHash())
}

func TestDefaults_P2ExemptMatchesSleepAndWaitOnly(t *testing.T) {
	c := Defaults()
	for _, cmd := range []string{"sleep 180", "sleep 180 && echo waited", "  sleep 90", "wait", "wait $pid"} {
		require.True(t, c.IsP2Exempt("Bash", cmd), cmd)
	}
	for _, cmd := range []string{"swift test", "git status", "echo waiting", "sleepy-time.sh"} {
		require.False(t, c.IsP2Exempt("Bash", cmd), cmd)
	}
	// Only Bash targets are shell commands; a non-Bash tool never matches
	// even if its target happens to start with "sleep".
	require.False(t, c.IsP2Exempt("Read", "sleep 180"))
}

func TestIsP2Exempt_RtkPrefixStripped(t *testing.T) {
	c := Defaults()
	require.True(t, c.IsP2Exempt("Bash", "rtk proxy sleep 180"))
	require.True(t, c.IsP2Exempt("Bash", "rtk sleep 120"))
	require.False(t, c.IsP2Exempt("Bash", "rtk proxy git status"))
}

func TestLoadConfig_P2ExemptPatternsFromSchema(t *testing.T) {
	agentlog := filepath.Join(t.TempDir(), ".agentlog")
	require.NoError(t, os.MkdirAll(agentlog, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentlog, "schema.json"),
		[]byte(`{"p2_exempt_patterns": ["^\\s*poll\\b"]}`), 0o644))
	c, err := LoadConfig(t.TempDir(), agentlog)
	require.NoError(t, err)
	require.True(t, c.IsP2Exempt("Bash", "poll status"))
	require.False(t, c.IsP2Exempt("Bash", "sleep 180"), "schema.json list replaces, not extends, the default")

	require.NoError(t, os.WriteFile(filepath.Join(agentlog, "schema.json"), []byte(`{}`), 0o644))
	c, err = LoadConfig(t.TempDir(), agentlog)
	require.NoError(t, err)
	require.Equal(t, Defaults().P2ExemptPatterns, c.P2ExemptPatterns, "empty list keeps defaults")
}

func TestWeightsHash_SensitiveToP2ExemptPatterns(t *testing.T) {
	a := Defaults()
	b := Defaults()
	b.P2ExemptPatterns = append([]string{}, b.P2ExemptPatterns...)
	b.P2ExemptPatterns = append(b.P2ExemptPatterns, "^\\s*poll\\b")
	require.NotEqual(t, a.WeightsHash(), b.WeightsHash())
}

func TestLoadConfig_ReviewBlockVerdictsFromSchema(t *testing.T) {
	agentlog := filepath.Join(t.TempDir(), ".agentlog")
	require.NoError(t, os.MkdirAll(agentlog, 0o755))
	require.True(t, Defaults().ReviewBlockVerdicts["changes-requested"])

	require.NoError(t, os.WriteFile(filepath.Join(agentlog, "schema.json"),
		[]byte(`{"review_block_verdicts": ["rejected"]}`), 0o644))
	c, err := LoadConfig(t.TempDir(), agentlog)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"rejected": true}, c.ReviewBlockVerdicts)

	require.NoError(t, os.WriteFile(filepath.Join(agentlog, "schema.json"), []byte(`{}`), 0o644))
	c, err = LoadConfig(t.TempDir(), agentlog)
	require.NoError(t, err)
	require.Equal(t, Defaults().ReviewBlockVerdicts, c.ReviewBlockVerdicts, "empty list keeps defaults")
}
