package eval

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSessionFixture(t *testing.T) {
	trace, err := ParseSession("testdata/fixture-session.jsonl")
	require.NoError(t, err)

	require.Equal(t, "test-01", trace.Session.SessionID)
	require.Equal(t, "claude-opus-4-7", trace.Session.MainLoopModel)
	require.Equal(t, 250, trace.Session.MainLoopIn)
	require.Equal(t, 125, trace.Session.MainLoopOut)
	require.Equal(t, 1000, trace.Session.CacheRead)

	require.Len(t, trace.Dispatches, 3)

	arch := trace.Dispatches[0]
	require.Equal(t, "Architect run 1 for MoodJournal", arch.AgentName)
	require.Equal(t, "completed", arch.Status)
	require.Equal(t, 93000, arch.SubagentTokens)
	require.Equal(t, 8, arch.ToolUses)
	require.Equal(t, int64(180000), arch.DurationMs)
	require.Equal(t, "done", arch.Returned.Verdict)
	require.Equal(t, "architect", arch.Returned.Next)
	require.Equal(t, "PROJECT_SPEC.md awaiting acceptance", arch.Returned.Blocker)
	require.InDelta(t, 0.85, arch.Returned.Confidence, 0.001)
	require.Equal(t, "verdict: done", arch.Returned.RawFirstLine)

	impl := trace.Dispatches[1]
	require.Equal(t, "Implementer — MoodJournalInterface", impl.AgentName)
	require.Equal(t, "done", impl.Returned.Verdict)
	// Preamble present: first line was "Notes for parent orchestrator: ..."
	require.NotEqual(t, "verdict: done", impl.Returned.RawFirstLine)
	require.Contains(t, impl.Returned.RawFirstLine, "Notes for parent")

	orphan := trace.Dispatches[2]
	require.Equal(t, "Reviewer — never returns", orphan.AgentName)
	require.Equal(t, "", orphan.Status) // no task-notification recorded
}

func TestGuessRole(t *testing.T) {
	cases := map[string]string{
		"Architect run 1 for MoodJournal":         "architect",
		"Implementer — MoodJournalInterface":      "implementer",
		"Tester — StreakCalculatorImpl":           "tester",
		"Reviewer — MoodJournalInterface":         "reviewer",
		"Dispatch architect on mood feature":      "architect", // token not at start
		"refactor-agent on something":             "refactor-agent",
		"Refactor MoodEntry":                      "refactor-agent",
		"Some totally different task":             "other",
		"xcodegen-driver: add scheme":             "xcodegen-driver",
		// "Explore" ≠ "explorer" — role tokens must match a full known
		// word; "Explore" bucket-outs to "other" honestly. Callers who
		// want the Explore subagent labeled should use "explorer" in the
		// description.
		"Explore mood feature architecture": "other",
		"explorer over the codebase":        "explorer",
	}
	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			require.Equal(t, want, GuessRole(input))
		})
	}
}
