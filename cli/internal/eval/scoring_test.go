package eval

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScoreFlagsMissingArtifactAndPreamble(t *testing.T) {
	trace, err := ParseSession("testdata/fixture-session.jsonl")
	require.NoError(t, err)

	// Pretend every claimed artifact is missing on disk (fixture points at
	// /nonexistent/*), and confirm the scorer surfaces the violation.
	alwaysMissing := func(string) bool { return false }
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
	score := Score(trace, func(string) bool { return false })
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
