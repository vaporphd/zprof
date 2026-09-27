package verdicts

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// repoRoot locates the zprof repository root from this test file's own
// path, mirroring apply/e2e_test.go's filepath.Join("..", "..", "..")
// convention (cli/internal/verdicts is three directories under the root).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, f, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root, err := filepath.Abs(filepath.Join(filepath.Dir(f), "..", "..", ".."))
	require.NoError(t, err)
	return root
}

// repoFrontmatterRe mirrors internal/doctor.frontmatterRe — kept as its own
// copy so this package doesn't need to import doctor (which itself imports
// verdicts; a mutual import would cycle).
var repoFrontmatterRe = regexp.MustCompile(`\A---\r?\n((?s:.*?))\r?\n---\r?\n`)

// splitAgentFile is a minimal stand-in for doctor's parseAgentFile, scoped
// to what this test needs: the frontmatter map, the body, and the line at
// which the body starts.
func splitAgentFile(t *testing.T, path string) (fm map[string]any, body []byte, bodyStartLine int) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	m := repoFrontmatterRe.FindSubmatch(data)
	if m == nil {
		return nil, nil, 0
	}
	if err := yaml.Unmarshal(m[1], &fm); err != nil {
		t.Fatalf("%s: frontmatter YAML parse error: %v", path, err)
	}
	bodyStartLine = strings.Count(string(m[0]), "\n") + 1
	body = data[len(m[0]):]
	return fm, body, bodyStartLine
}

// agentNameRelativeTo converts an on-disk agent path into the name zprof
// knows it by, relative to the agents/ directory it lives under —
// mirrors internal/doctor.agentNameFor.
func agentNameRelativeTo(agentsDir, path string) string {
	rel, err := filepath.Rel(agentsDir, path)
	if err != nil {
		rel = filepath.Base(path)
	}
	return filepath.ToSlash(strings.TrimSuffix(rel, ".md"))
}

// walkAgentsDirs collects every agents/ directory under root worth
// checking: profiles/base/agents, every profiles/overlays/*/agents, and
// .claude/agents (the currently-applied instance in this very repo).
func walkAgentsDirs(root string) []string {
	var dirs []string
	dirs = append(dirs, filepath.Join(root, "profiles", "base", "agents"))
	dirs = append(dirs, filepath.Join(root, ".claude", "agents"))
	overlaysDir := filepath.Join(root, "profiles", "overlays")
	entries, err := os.ReadDir(overlaysDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				dirs = append(dirs, filepath.Join(overlaysDir, e.Name(), "agents"))
			}
		}
	}
	return dirs
}

// TestProfilesVerdictsConsistent is the repo-level backstop for AC1/AC4:
// every agent file across base, every overlay, and .claude/agents that
// declares a return_format and resolves to a known role must pass
// CheckAgent against profiles/base/verdicts.yaml with zero findings. This
// is exactly the check `zprof doctor` runs per-project (checkAgentVerdicts
// in internal/doctor), so CI and doctor can never disagree.
func TestProfilesVerdictsConsistent(t *testing.T) {
	root := repoRoot(t)
	reg, err := Load(filepath.Join(root, "profiles", "base", "verdicts.yaml"))
	require.NoError(t, err)
	require.NoError(t, reg.Validate())

	checked := 0
	for _, agentsDir := range walkAgentsDirs(root) {
		info, err := os.Stat(agentsDir)
		if err != nil || !info.IsDir() {
			continue
		}
		err = filepath.Walk(agentsDir, func(path string, info os.FileInfo, err error) error {
			require.NoError(t, err)
			if info.IsDir() || !strings.HasSuffix(path, ".md") {
				return nil
			}
			fm, body, bodyStartLine := splitAgentFile(t, path)
			if fm == nil {
				return nil // no frontmatter — not doctor's business here either
			}
			rf, _ := fm["return_format"].(string)
			if strings.TrimSpace(rf) == "" {
				return nil // tool-agent with no schema contract
			}
			name := agentNameRelativeTo(agentsDir, path)
			role, ok := reg.Lookup(name)
			if !ok {
				return nil // not a role zprof's registry tracks
			}
			checked++
			findings := CheckAgent(reg, name, fm, body, bodyStartLine)
			require.Empty(t, findings, "%s (role %s): %v", path, role, findings)
			return nil
		})
		require.NoError(t, err)
	}
	require.Greater(t, checked, 50, "sanity: expected to have checked a substantial share of the agent roster")
}
