package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func setupStatsAgentlog(t *testing.T) (agentlog string) {
	t.Helper()
	proj := t.TempDir()
	agentlog = filepath.Join(proj, ".agentlog")
	require.NoError(t, os.MkdirAll(agentlog, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentlog, "dispatches.jsonl"), []byte(""), 0o644))
	return agentlog
}

func runStats(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := NewStatsCmd()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), err
}

func TestStatsCmd_NoGuardEventsFilePrintsNoGuardSection(t *testing.T) {
	agentlog := setupStatsAgentlog(t)
	out, err := runStats(t, agentlog, "--format", "json")
	require.NoError(t, err)
	require.Contains(t, out, "saved:")
	require.NotContains(t, out, "guard: top rules")
}

const statsGuardEventsFixture = `{"ts":"2026-09-26T09:45:00Z","session_id":"s1","event":"pre-tool","role":"implementer","dispatch_id":"t1","tool":"Bash","rule":"force_push","decision":"deny","target":"git push","input_hash":"h1"}
{"ts":"2026-09-26T09:46:00Z","session_id":"s1","event":"pre-tool","role":"implementer","dispatch_id":"t1","tool":"Bash","rule":"force_push","decision":"deny","target":"git push","input_hash":"h2"}
{"ts":"2026-09-26T09:47:00Z","session_id":"s1","event":"pre-tool","role":"main","dispatch_id":null,"tool":"Bash","rule":"force_push","decision":"deny","target":"git push","input_hash":"h3"}
{"ts":"2026-09-26T10:20:00Z","session_id":"s1","event":"subagent-stop","role":"implementer","dispatch_id":"t2","tool":null,"rule":"return_format","decision":"block","target":null,"input_hash":null}
{"ts":"2026-09-26T10:21:00Z","session_id":"s1","event":"pre-tool","role":"implementer","dispatch_id":"t2","tool":"Bash","rule":"branch_pr_merged","decision":"allow_unverified","target":null,"input_hash":null}
{"ts":"2026-09-26T10:22:00Z","session_id":"s2","event":"pre-tool","role":"implementer","dispatch_id":"u1","tool":"Bash","rule":"head_on_remote","decision":"deny","target":null,"input_hash":null}
`

func setupStatsAgentlogWithGuardEvents(t *testing.T) (agentlog string) {
	t.Helper()
	agentlog = setupStatsAgentlog(t)
	require.NoError(t, os.WriteFile(filepath.Join(agentlog, "guard-events.jsonl"), []byte(statsGuardEventsFixture), 0o644))
	return agentlog
}

func TestStatsCmd_GuardTopRulesFormatAndOrder(t *testing.T) {
	agentlog := setupStatsAgentlogWithGuardEvents(t)
	out, err := runStats(t, agentlog, "--format", "json")
	require.NoError(t, err)
	// force_push×3 (2 implementer + 1 main), then a tie at ×1 broken by rule
	// name ascending (head_on_remote < return_format); allow_unverified is
	// not a violation and does not appear.
	require.Contains(t, out, "guard: top rules: force_push×3 head_on_remote×1 return_format×1")
}

func TestStatsCmd_GuardTopRulesSessionFilter(t *testing.T) {
	agentlog := setupStatsAgentlogWithGuardEvents(t)
	out, err := runStats(t, agentlog, "--format", "json", "--session", "s1")
	require.NoError(t, err)
	require.Contains(t, out, "guard: top rules: force_push×3 return_format×1")
	require.NotContains(t, out, "head_on_remote", "session s2's event is filtered out")
}

func TestStatsCmd_GuardTopRulesRoleFilter(t *testing.T) {
	agentlog := setupStatsAgentlogWithGuardEvents(t)
	out, err := runStats(t, agentlog, "--format", "json", "--role", "main")
	require.NoError(t, err)
	require.Contains(t, out, "guard: top rules: force_push×1")
}
