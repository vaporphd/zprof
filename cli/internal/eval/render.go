package eval

import (
	"fmt"
	"strings"
	"time"
)

// RenderSummary produces the human-readable summary.md for a session. The
// output is stable across runs — same trace, same bytes — so it can be
// diffed session-over-session for regression detection.
func RenderSummary(s SessionScore) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# zprof eval — session %s\n\n", short(s.Meta.SessionID))
	fmt.Fprintln(&b, "_Tier 1 — deterministic. No LLM ran. See `zprof eval --deep` for panel judging._")
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Session")
	fmt.Fprintf(&b, "- Session ID: `%s`\n", s.Meta.SessionID)
	fmt.Fprintf(&b, "- Log path:   `%s`\n", s.Meta.Path)
	if !s.Meta.FirstTimestamp.IsZero() {
		span := s.Meta.LastTimestamp.Sub(s.Meta.FirstTimestamp)
		fmt.Fprintf(&b, "- Span:       %s (%s → %s)\n",
			shortDuration(span),
			s.Meta.FirstTimestamp.UTC().Format(time.RFC3339),
			s.Meta.LastTimestamp.UTC().Format(time.RFC3339),
		)
	}
	fmt.Fprintf(&b, "- Main model: `%s`\n", nz(s.Meta.MainLoopModel, "unknown"))
	fmt.Fprintf(&b, "- Main-loop tokens: in %s / out %s (cache read %s, create %s)\n",
		fmtInt(s.Meta.MainLoopIn), fmtInt(s.Meta.MainLoopOut),
		fmtInt(s.Meta.CacheRead), fmtInt(s.Meta.CacheCreate))

	var subagentTotal int
	for _, r := range s.Roles {
		subagentTotal += r.TotalTokens
	}
	fmt.Fprintf(&b, "- Subagent tokens (output only): %s across %d dispatches\n\n",
		fmtInt(subagentTotal), countDispatches(s.Roles))

	fmt.Fprintln(&b, "## Per-role scorecard")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Role | Model | N | Pass@1 | Median tok | ApT | Compliance | Notes |")
	fmt.Fprintln(&b, "|---|---|--:|--:|--:|--:|---|---|")
	for _, r := range s.Roles {
		compliance := ""
		if r.ArtifactMissing > 0 {
			compliance += fmt.Sprintf("artifact-missing×%d ", r.ArtifactMissing)
		}
		if r.HadPreamble > 0 {
			compliance += fmt.Sprintf("preamble×%d ", r.HadPreamble)
		}
		if r.NextUnreachable > 0 {
			compliance += fmt.Sprintf("next-broken×%d ", r.NextUnreachable)
		}
		if compliance == "" {
			compliance = "clean"
		}
		notes := ""
		if r.ConfidenceCount > 0 {
			notes = fmt.Sprintf("avg conf %.2f (%d/%d)", r.AvgConfidence, r.ConfidenceCount, r.Dispatches)
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %.2f | %s | %.1f | %s | %s |\n",
			r.Role,
			nz(r.Model, "(inherited)"),
			r.Dispatches,
			r.PassAt1,
			fmtInt(r.MedianTokens),
			r.ApT,
			strings.TrimSpace(compliance),
			notes,
		)
	}
	fmt.Fprintln(&b)

	if len(s.Violations) > 0 {
		fmt.Fprintln(&b, "## Contract violations")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "| # | Role | Kind | Detail |")
		fmt.Fprintln(&b, "|--:|---|---|---|")
		for i, v := range s.Violations {
			fmt.Fprintf(&b, "| %d | %s | `%s` | %s |\n", i+1, v.Role, v.Kind, escapePipes(v.Detail))
		}
		fmt.Fprintln(&b)
	} else {
		fmt.Fprintln(&b, "## Contract violations")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "None. Every dispatch returned, wrote its claimed artifact, and routed to a reachable next agent.")
		fmt.Fprintln(&b)
	}

	fmt.Fprintln(&b, "## What Tier 2 would add")
	fmt.Fprintln(&b, "- Panel-judge quality score (1-5) per dispatch across correctness / adherence / efficiency framings.")
	fmt.Fprintln(&b, "- Model-tier recommendation per role (advisory).")
	fmt.Fprintln(&b, "- Reasoning quality assessment on ADR / review artifacts.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Run `zprof eval --deep <sessionId>` to dispatch the `evaluator` subagent.")

	return b.String()
}

// -----------------------------------------------------------------------------

func fmtInt(n int) string {
	if n < 0 {
		return "-" + fmtInt(-n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	out := []byte(fmt.Sprintf("%d", n))
	// Insert commas from the right.
	// A dispatch never crosses billions of tokens; a simple loop is fine.
	var withCommas []byte
	for i, c := range out {
		if i > 0 && (len(out)-i)%3 == 0 {
			withCommas = append(withCommas, ',')
		}
		withCommas = append(withCommas, byte(c))
	}
	return string(withCommas)
}

func shortDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

func short(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:8] + "…" + id[len(id)-4:]
}

func nz(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func countDispatches(rs []RoleStats) int {
	n := 0
	for _, r := range rs {
		n += r.Dispatches
	}
	return n
}

func escapePipes(s string) string {
	// Markdown table cells cannot contain unescaped '|'.
	return strings.ReplaceAll(s, "|", `\|`)
}
