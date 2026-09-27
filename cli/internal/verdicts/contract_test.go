package verdicts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseContract_SkipsCommentsAndBlankLines(t *testing.T) {
	rf := "\n# CRITICAL: no preamble\n\nverdict: done|blocked|failed\nartifact: <path>\n"
	c, err := ParseContract(rf)
	require.NoError(t, err)
	require.Equal(t, "verdict", c.Key)
	require.Equal(t, []string{"done", "blocked", "failed"}, c.Tokens)
	require.Equal(t, 4, c.Line)
}

func TestParseContract_TrimsSpacesAroundPipes(t *testing.T) {
	c, err := ParseContract("verdict: valid | insufficient | invalid\n")
	require.NoError(t, err)
	require.Equal(t, []string{"valid", "insufficient", "invalid"}, c.Tokens)
}

func TestParseContract_PlaceholderSuffixBecomesPattern(t *testing.T) {
	c, err := ParseContract("verdict: blocked-external|blocked-<reason>\n")
	require.NoError(t, err)
	require.Equal(t, []string{"blocked-external", "blocked-*"}, c.Tokens)
}

func TestParseContract_WholeValueWildcard(t *testing.T) {
	c, err := ParseContract("question: <only when blocked>\n")
	require.NoError(t, err)
	require.Equal(t, "question", c.Key)
	require.Equal(t, []string{"*"}, c.Tokens)
}

func TestParseContract_LegacyCompletionKey(t *testing.T) {
	c, err := ParseContract("completion: complete|incomplete|blocked\n")
	require.NoError(t, err)
	require.Equal(t, "completion", c.Key)
}

func TestParseContract_EmptyInputErrors(t *testing.T) {
	_, err := ParseContract("# only a comment\n\n")
	require.Error(t, err)
}

func TestBodyTokens_FindsBacktickedAndPlainOccurrences(t *testing.T) {
	body := []byte(
		"Refuse and return `verdict: blocked` with a reason.\n" +
			"Some prose mentioning verdict: done in passing.\n" +
			"Not a match: verdictfoo: bar\n")
	toks := BodyTokens(body)
	require.Len(t, toks, 2)
	require.Equal(t, BodyToken{Token: "blocked", Line: 1}, toks[0])
	require.Equal(t, BodyToken{Token: "done", Line: 2}, toks[1])
}

func TestBodyTokens_SplitsPipeQuotesAndTrimsTrailingDash(t *testing.T) {
	body := []byte("cites `verdict: complete|incomplete|blocked` as the enum\n" +
		"and `verdict: blocked-<reason>` as a wildcard example\n" +
		"and `verdict: blocked-*` literally\n")
	toks := BodyTokens(body)
	var got []string
	for _, tk := range toks {
		got = append(got, tk.Token)
	}
	require.Equal(t, []string{"complete", "incomplete", "blocked", "blocked", "blocked-*"}, got)
}

func TestBodyTokens_DoesNotSpanLineBreaks(t *testing.T) {
	body := []byte("`verdict:\nblocked` split across lines\n")
	require.Empty(t, BodyTokens(body))
}

func TestContractContains(t *testing.T) {
	tokens := []string{"done", "blocked-external", "blocked-*"}
	require.True(t, contractContains(tokens, "done"))
	require.True(t, contractContains(tokens, "blocked-external"))
	require.True(t, contractContains(tokens, "blocked-ci-pending"), "matches via blocked-* pattern")
	require.False(t, contractContains(tokens, "failed"))

	require.True(t, contractContains([]string{"*"}, "anything"))
}

func agentFrontmatter(returnFormat string) map[string]any {
	return map[string]any{"name": "x", "return_format": returnFormat}
}

func TestCheckAgent_CleanAgentHasNoFindings(t *testing.T) {
	r := mustParse(t, minimalRegistry)
	fm := agentFrontmatter("verdict: done|blocked|failed\n")
	body := []byte("Never do X. Return `verdict: done` when finished.\n")
	require.Empty(t, CheckAgent(r, "implementer", fm, body, 10))
}

func TestCheckAgent_UnknownTokenInFrontmatter(t *testing.T) {
	r := mustParse(t, minimalRegistry)
	fm := agentFrontmatter("verdict: done|zzz\n")
	findings := CheckAgent(r, "implementer", fm, nil, 10)
	require.Len(t, findings, 1)
	require.Contains(t, findings[0].Msg, `token "zzz" of role "implementer" is not in verdicts.yaml`)
}

func TestCheckAgent_BodyTokenOutsideOwnEnum(t *testing.T) {
	r := mustParse(t, minimalRegistry)
	fm := agentFrontmatter("verdict: approve|block\n")
	body := []byte("Refuse self-review and return `verdict: blocked` with a reason.\n")
	findings := CheckAgent(r, "reviewer", fm, body, 20)
	require.Len(t, findings, 1)
	require.Equal(t, 20, findings[0].Line)
	require.Contains(t, findings[0].Msg, `body line 20: "verdict: blocked" is outside this agent's return_format enum`)
}

func TestCheckAgent_QuotedTokenIsAllowed(t *testing.T) {
	r := mustParse(t, minimalRegistry)
	fm := agentFrontmatter("verdict: complete|blocked\n")
	body := []byte("Its `verdict: done` is a claim, not a fact.\n")
	require.Empty(t, CheckAgent(r, "auditor", fm, body, 5))
}

func TestCheckAgent_LegacyCompletionKeyIsA0Finding(t *testing.T) {
	r := mustParse(t, minimalRegistry)
	fm := agentFrontmatter("completion: complete|incomplete|blocked\n")
	findings := CheckAgent(r, "auditor", fm, nil, 5)
	require.Len(t, findings, 1)
	require.Contains(t, findings[0].Msg, `return_format must start with "verdict:", got "completion:"`)
}

func TestCheckAgent_UnresolvedRoleYieldsNoFindings(t *testing.T) {
	r := mustParse(t, minimalRegistry)
	fm := agentFrontmatter("verdict: done|blocked|failed\n")
	body := []byte("cites `verdict: whatever` freely\n")
	require.Empty(t, CheckAgent(r, "some-custom-tool-agent", fm, body, 1))
}
