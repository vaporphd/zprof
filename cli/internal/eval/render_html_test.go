package eval

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderHTMLProducesSelfContainedPage(t *testing.T) {
	trace, err := ParseSession("testdata/fixture-session.jsonl")
	require.NoError(t, err)
	score := Score(trace, func(string) bool { return false })
	html := RenderHTML(score)

	// Structural anchors — every report must have these.
	for _, must := range []string{
		"<!DOCTYPE html>",
		`<meta charset="utf-8">`,
		"<title>zprof eval",
		"Per-role scorecard",
		"Token spend by role",
		"Contract violations",
	} {
		require.True(t, strings.Contains(html, must), "missing anchor %q", must)
	}

	// Self-contained: no external URL fetches — no <script src=…>, no
	// <link rel="stylesheet" href=…>, no <img src="http…>. The report must
	// render offline from a file:// URL.
	for _, forbidden := range []string{
		`<script src=`,
		`<link rel="stylesheet"`,
		`src="http`,
		`href="http`, // any http:// or https:// url as an asset
	} {
		require.False(t, strings.Contains(html, forbidden), "unexpected external asset: %q", forbidden)
	}

	// Role rows exist for architect + implementer + reviewer (from fixture).
	require.Contains(t, html, `<code>architect</code>`)
	require.Contains(t, html, `<code>implementer</code>`)
	require.Contains(t, html, `<code>reviewer</code>`)
}

func TestRenderHTMLEscapesUnsafeContent(t *testing.T) {
	// Guard against XSS-shape content sneaking through from a subagent's
	// return payload — e.g. a Description that contains "<script>".
	score := SessionScore{
		Meta: SessionMeta{SessionID: "id", Path: "/tmp"},
		Roles: []RoleStats{{
			Role: "<script>alert(1)</script>", Model: "opus", Dispatches: 1,
		}},
		Violations: []Violation{{
			Role: "x", Kind: "return-preamble",
			Detail: `<img src=x onerror="alert(1)">`,
		}},
	}
	html := RenderHTML(score)
	require.NotContains(t, html, "<script>alert(1)</script>")
	require.NotContains(t, html, `<img src=x onerror`)
	require.Contains(t, html, `&lt;script&gt;alert(1)&lt;/script&gt;`)
}

func TestRenderHTMLTokenBarSortsDescending(t *testing.T) {
	score := SessionScore{
		Meta: SessionMeta{SessionID: "id"},
		Roles: []RoleStats{
			{Role: "small", TotalTokens: 100},
			{Role: "huge", TotalTokens: 10000},
			{Role: "medium", TotalTokens: 1000},
		},
	}
	html := RenderHTML(score)
	// The scoreboard renders roles alphabetically; the bars section renders
	// them by token count. Scan only the bars section (starts after
	// "Token spend by role" heading).
	i := strings.Index(html, "Token spend by role")
	require.GreaterOrEqual(t, i, 0, "bars section missing")
	bars := html[i:]
	iHuge := strings.Index(bars, `<code>huge</code>`)
	iMedium := strings.Index(bars, `<code>medium</code>`)
	iSmall := strings.Index(bars, `<code>small</code>`)
	require.True(t, iHuge < iMedium, "huge should render before medium")
	require.True(t, iMedium < iSmall, "medium should render before small")
}
