package eval

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScoreFlagsMissingArtifactAndPreamble(t *testing.T) {
	trace, err := ParseSession("testdata/fixture-session.jsonl")
	require.NoError(t, err)

	// Pretend every claimed artifact is missing on disk (fixture points at
	// /nonexistent/*), and confirm the scorer surfaces the violation.
	alwaysMissing := func(string, string) bool { return false }
	score := Score(trace, alwaysMissing)

	// Two completed dispatches (architect + implementer) each claimed an
	// artifact that doesn't exist → 2 artifact-missing violations. Plus
	// the orphan reviewer never returned → 1 dispatch-never-returned.
	// The implementer preamble ("Notes for parent orchestrator:...") →
	// 1 return-preamble.
	kinds := map[string]int{}
	for _, v := range score.Violations {
		kinds[v.Kind]++
	}
	require.Equal(t, 2, kinds["artifact-missing"], "expected 2 artifact-missing violations")
	require.Equal(t, 1, kinds["return-preamble"], "expected 1 return-preamble violation")
	require.Equal(t, 1, kinds["dispatch-never-returned"], "expected 1 dispatch-never-returned")

	byRole := map[string]RoleStats{}
	for _, r := range score.Roles {
		byRole[r.Role] = r
	}
	// Both architect and implementer completed AND passed → pass@1 = 1.0.
	require.InDelta(t, 1.0, byRole["architect"].PassAt1, 0.001)
	require.InDelta(t, 1.0, byRole["implementer"].PassAt1, 0.001)
	// Reviewer never returned → 1 dispatch, 0 completed, pass@1 = 0.
	require.Equal(t, 1, byRole["reviewer"].Dispatches)
	require.Equal(t, 0, byRole["reviewer"].Completed)
	// Confidence recorded on architect only.
	require.Equal(t, 1, byRole["architect"].ConfidenceCount)
	require.InDelta(t, 0.85, byRole["architect"].AvgConfidence, 0.001)
}

func TestRenderSummaryProducesStableMarkdown(t *testing.T) {
	trace, err := ParseSession("testdata/fixture-session.jsonl")
	require.NoError(t, err)
	score := Score(trace, func(string, string) bool { return false })
	md := RenderSummary(score)

	// Anchor points — the report must always name these headings.
	for _, must := range []string{
		"# zprof eval — session ",
		"## Session",
		"## Per-role scorecard",
		"## Contract violations",
		"## What Tier 2 would add",
	} {
		require.True(t, strings.Contains(md, must), "missing anchor %q", must)
	}
	// Role rows visible.
	require.Contains(t, md, "architect")
	require.Contains(t, md, "implementer")
	require.Contains(t, md, "reviewer")
	// Violation table row for the orphan reviewer.
	require.Contains(t, md, "dispatch-never-returned")
}

func TestParseReturnFormatToleratesFencedBlock(t *testing.T) {
	// Some agents wrap their return in a fenced ```yaml block. The parser
	// must strip that fence, not treat "```" as the first non-empty line.
	fenced := "```yaml\nverdict: block\nartifact: none\nnext: null\none_line: BLOCK — 2 Critical\n```"
	r := parseReturnFormat(fenced)
	require.Equal(t, "block", r.Verdict)
	require.Equal(t, "none", r.Artifact)
	require.Equal(t, "verdict: block", r.RawFirstLine)
}

func TestNextFieldAcceptsTaskRunner(t *testing.T) {
	// task-runner replaces dev-orchestrator/exploratory-orchestrator as the
	// loop's entry point. Any agent that hands control back to it via
	// `next: task-runner` must not be flagged as next-unreachable — that's
	// exactly how §12.1 attributes dispatch outcomes to the new role.
	trace := &Trace{
		Dispatches: []Dispatch{
			{
				ID:        "d1",
				AgentName: "task-runner dispatch",
				Status:    "completed",
				Returned:  Return{Verdict: "done", Next: "task-runner", RawFirstLine: "verdict: done"},
			},
			{
				ID:        "d2",
				AgentName: "task-runner dispatch",
				Status:    "completed",
				Returned:  Return{Verdict: "done", Next: "totally-made-up", RawFirstLine: "verdict: done"},
			},
		},
	}
	score := Score(trace, func(string, string) bool { return true })

	var unreachable []string
	for _, v := range score.Violations {
		if v.Kind == "next-unreachable" {
			unreachable = append(unreachable, v.DispatchID)
		}
	}
	require.NotContains(t, unreachable, "d1", "next: task-runner must be a known role")
	require.Contains(t, unreachable, "d2", "sanity check: an unknown role must still be flagged — otherwise the assertion above proves nothing")
}

func TestIsPassAcceptsReviewerVerdicts(t *testing.T) {
	// Reviewer uses a different verdict vocabulary than architect/implementer.
	// The scorer must recognize all approval variants including the routine
	// intermediate `awaiting-approval` state (reviewer §12).
	require.True(t, isPass("done"))
	require.True(t, isPass("approve"))
	require.True(t, isPass("approve-with-fixes"))
	require.True(t, isPass("awaiting-approval"),
		"awaiting-approval is the routine intermediate reviewer verdict; contract-compliant work")
	require.False(t, isPass("block"))
	require.False(t, isPass("blocked"))
	require.False(t, isPass("failed"))
	require.False(t, isPass(""), "empty verdict means the schema was never emitted")
}

// TestNextFieldAcceptsEveryTargetProfilesEmit locks knownRoles to the set of
// `next:` targets the shipped profiles actually emit. Every miss here scored
// a contract-compliant handoff as a `next-unreachable` violation, which made
// the "Compliance without violations" acceptance criterion unreachable on
// any project using issue-loop-github-strict or re-macho.
//
// The list is the deduplicated output of
// `grep -rhoE '^ *next: .*' profiles/ --include='*.md'` split on `|`.
func TestNextFieldAcceptsEveryTargetProfilesEmit(t *testing.T) {
	emitted := []string{
		"architect", "bug-hunter", "explorer", "human", "hypothesizer",
		"implementer", "integration-gate", "main-session", "null",
		"plan-reviewer", "planner", "pr-shepherd", "refactor-agent",
		"report-writer", "reviewer", "task-runner", "tester", "unpacker",
		"verifier",
	}
	var dispatches []Dispatch
	for i, target := range emitted {
		dispatches = append(dispatches, Dispatch{
			ID:        fmt.Sprintf("ok-%d", i),
			AgentName: "reviewer run",
			Status:    "completed",
			Returned:  Return{Verdict: "done", Next: target, RawFirstLine: "verdict: done"},
		})
	}
	// Negative control: an invented target must still be flagged, otherwise
	// the assertions above would pass on a knownRoles map that accepts all.
	dispatches = append(dispatches, Dispatch{
		ID: "bogus", AgentName: "reviewer run", Status: "completed",
		Returned: Return{Verdict: "done", Next: "kubernetes-whisperer", RawFirstLine: "verdict: done"},
	})

	score := Score(&Trace{Dispatches: dispatches}, func(string, string) bool { return true })

	unreachable := map[string]bool{}
	for _, v := range score.Violations {
		if v.Kind == "next-unreachable" {
			unreachable[v.DispatchID] = true
		}
	}
	for i, target := range emitted {
		require.False(t, unreachable[fmt.Sprintf("ok-%d", i)],
			"next: %s is emitted by a shipped profile and must be reachable", target)
	}
	require.True(t, unreachable["bogus"], "an unknown target must still be flagged")
}

// The exploratory chain used to bucket to "other", which made RE sessions
// unreadable in the scorecard.
func TestGuessRoleKnowsExploratoryChain(t *testing.T) {
	for desc, want := range map[string]string{
		"Intake for the macho binary":       "intake",
		"unpacker pass 1":                   "unpacker",
		"Hypothesizer — 4 candidates":       "hypothesizer",
		"Verifier on hypothesis 2":          "verifier",
		"report-writer final":               "report-writer",
		"Report writer final":               "report-writer",
		"Dev-orchestrator (archived run)":   "dev-orchestrator",
		"exploratory orchestrator archived": "exploratory-orchestrator",
		"totally unrelated label":           "other",
	} {
		require.Equal(t, want, GuessRole(desc), "description: %q", desc)
	}
}
