package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/stretchr/testify/require"
)

func TestScanFindsIOS(t *testing.T) {
	rules, err := manifest.LoadDetect(filepath.Join("..", "..", "testdata", "overlays", "valid-detect.yaml"))
	require.NoError(t, err)
	matches := Scan(filepath.Join("..", "..", "testdata", "projects", "fake-ios"), []*manifest.DetectRules{rules})
	require.Len(t, matches, 1)
	require.Equal(t, "ios-swift", matches[0].OverlayName)
	require.Equal(t, "high", matches[0].Confidence)
	require.NotEmpty(t, matches[0].Evidence)
}

func TestScanEmptyProjectYieldsNoMatches(t *testing.T) {
	rules, err := manifest.LoadDetect(filepath.Join("..", "..", "testdata", "overlays", "valid-detect.yaml"))
	require.NoError(t, err)
	matches := Scan(filepath.Join("..", "..", "testdata", "projects", "fake-empty"), []*manifest.DetectRules{rules})
	require.Empty(t, matches)
}

// issueLoopRules loads the real shipped detect.yaml rather than a fixture:
// the bug this guards against is a rule aimed at the wrong file, which a
// hand-written fixture would simply reproduce.
func issueLoopRules(t *testing.T) *manifest.DetectRules {
	t.Helper()
	rules, err := manifest.LoadDetect(filepath.Join("..", "..", "..", "profiles",
		"overlays", "issue-loop-github-strict", "detect.yaml"))
	require.NoError(t, err)
	return rules
}

func TestScanMatchesConventionVarsInProjectSpec(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(proj, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, "docs", "PROJECT_SPEC.md"),
		[]byte("# Spec\n\n- `MERGE_GATE`: local-green\n"), 0o644))

	matches := Scan(proj, []*manifest.DetectRules{issueLoopRules(t)})
	require.Len(t, matches, 1)

	var viaRegex bool
	for _, e := range matches[0].Evidence {
		if strings.Contains(e, "MERGE_GATE") {
			viaRegex = true
		}
	}
	require.True(t, viaRegex,
		"MERGE_GATE lives in PROJECT_SPEC.md; the regex rule must target that file, got %v",
		matches[0].Evidence)
}

func TestScanDoesNotMatchRetiredAutoMergeToken(t *testing.T) {
	proj := t.TempDir()
	// CLAUDE.md is not in any_file, so a match here could only come from the
	// regex rule — which must no longer know AUTO_MERGE at all.
	require.NoError(t, os.WriteFile(filepath.Join(proj, "CLAUDE.md"),
		[]byte("AUTO_MERGE: true\n"), 0o644))

	require.Empty(t, Scan(proj, []*manifest.DetectRules{issueLoopRules(t)}),
		"AUTO_MERGE was removed from the shipped templates; matching it would be a dead alternative")
}

func TestScanMatchesAnyFilePatternWithPathComponent(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(proj, "gradle"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, "gradle", "libs.versions.toml"),
		[]byte("[versions]\n"), 0o644))
	// Same base name at the root must not satisfy a pattern that asks for
	// the file inside gradle/ — the path component carries meaning.
	require.NoError(t, os.WriteFile(filepath.Join(proj, "elsewhere.toml"),
		[]byte("[versions]\n"), 0o644))

	rules := &manifest.DetectRules{
		Name:       "kotlin-ish",
		AnyFile:    []string{"gradle/libs.versions.toml"},
		Confidence: "high",
	}
	matches := Scan(proj, []*manifest.DetectRules{rules})

	require.Len(t, matches, 1)
	require.Equal(t, []string{filepath.Join(proj, "gradle", "libs.versions.toml")},
		matches[0].Evidence)
}

func TestScanAnyFilePathPatternIsRootRelative(t *testing.T) {
	proj := t.TempDir()
	// The pattern names a root-relative location; a same-named file nested
	// under an unrelated directory must not satisfy it. `submodule/` is
	// deliberately not one of skipDirs — using a skipped directory here
	// would make the test pass for the wrong reason.
	require.NoError(t, os.MkdirAll(filepath.Join(proj, "submodule", "gradle"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, "submodule", "gradle", "libs.versions.toml"),
		[]byte("[versions]\n"), 0o644))

	rules := &manifest.DetectRules{
		Name:       "kotlin-ish",
		AnyFile:    []string{"gradle/libs.versions.toml"},
		Confidence: "high",
	}
	require.Empty(t, Scan(proj, []*manifest.DetectRules{rules}))
}

func TestScanSkipsHeavyDirs(t *testing.T) {
	proj := t.TempDir()
	// Top-level Xcode project: should be matched.
	require.NoError(t, os.MkdirAll(filepath.Join(proj, "App.xcodeproj"), 0o755))
	// A nested Pods/ dependency that itself vendors an .xcodeproj: the
	// scanner must skip descending into Pods/ entirely, so this one must
	// NOT show up in evidence (and, before the fix, a large real Pods/
	// tree would also thrash the disk with a walk per pattern).
	require.NoError(t, os.MkdirAll(filepath.Join(proj, "Pods", "Dependency.xcodeproj"), 0o755))

	rules, err := manifest.LoadDetect(filepath.Join("..", "..", "testdata", "overlays", "valid-detect.yaml"))
	require.NoError(t, err)

	matches := Scan(proj, []*manifest.DetectRules{rules})
	require.Len(t, matches, 1)
	require.Equal(t, []string{filepath.Join(proj, "App.xcodeproj")}, matches[0].Evidence)
}
