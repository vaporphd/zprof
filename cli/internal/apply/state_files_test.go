package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vaporphd/zprof/internal/overlay"
	"github.com/stretchr/testify/require"
)

func TestEnsureStateFilesCreatesMissing(t *testing.T) {
	proj := t.TempDir()
	base := &overlay.Base{StateTemplates: map[string]string{
		"todo":     "# TODO",
		"lessons":  "# Lessons",
		"followup": "# Followup",
	}}
	created, err := EnsureStateFiles(proj, base, true)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"todo.md", "lessons.md", "followup.md"},
		relPaths(proj, created))
}

func TestEnsureStateFilesLeavesExistingUntouched(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, "todo.md"), []byte("user data"), 0o644))
	base := &overlay.Base{StateTemplates: map[string]string{"todo": "TEMPLATE"}}
	_, err := EnsureStateFiles(proj, base, true)
	require.NoError(t, err)
	got, _ := os.ReadFile(filepath.Join(proj, "todo.md"))
	require.Equal(t, "user data", string(got))
}

func TestEnsureStateFilesSkipsDocsInMinimal(t *testing.T) {
	proj := t.TempDir()
	base := &overlay.Base{StateTemplates: map[string]string{
		"todo":                  "T",
		"project-spec-skeleton": "PS",
		"adr-template":          "AT",
	}}
	created, err := EnsureStateFiles(proj, base, true) // minimal=true
	require.NoError(t, err)
	require.Contains(t, relPaths(proj, created), "todo.md")
	require.NotContains(t, relPaths(proj, created), "docs/PROJECT_SPEC.md")
}

func TestEnsureStateFilesIncludesDocsWhenNotMinimal(t *testing.T) {
	proj := t.TempDir()
	base := &overlay.Base{StateTemplates: map[string]string{
		"todo":                  "T",
		"project-spec-skeleton": "PS",
		"adr-template":          "AT",
	}}
	created, err := EnsureStateFiles(proj, base, false) // minimal=false
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"todo.md", "docs/PROJECT_SPEC.md", "docs/adr/0000-template.md"},
		relPaths(proj, created))
	body, _ := os.ReadFile(filepath.Join(proj, "docs", "PROJECT_SPEC.md"))
	require.Equal(t, "PS", string(body))
}

func relPaths(root string, paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		r, _ := filepath.Rel(root, p)
		out[i] = r
	}
	return out
}
