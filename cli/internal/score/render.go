package score

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// RenderCard produces the terminal card (spec §7). The first four lines are
// what main pastes into followup.md.
func RenderCard(c Card) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Score %d/100 · %s · %s · %s · confidence %s\n",
		c.Score, c.Tier, orDash(c.Verdict), slug(c), orDash(c.Inputs.Confidence))
	fmt.Fprintf(&b, "%s tok (in %s · out %s · cache %s) · %d dispatch · %d tool calls · %s · %s\n",
		humanTokens(c.Facts.Tokens.Total()), humanTokens(c.Facts.Tokens.Input), humanTokens(c.Facts.Tokens.Output),
		humanTokens(c.Facts.Tokens.CacheRead+c.Facts.Tokens.CacheCreation),
		c.Facts.Dispatches, c.Facts.ToolCalls, humanDuration(c.Facts.DurationMs), modelSummary(c.Facts.ModelCounts))

	findings := make([]Penalty, 0, len(c.Penalties))
	for _, p := range c.Penalties {
		if p.Points > 0 {
			findings = append(findings, p)
		}
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Points > findings[j].Points })
	if len(findings) > 3 {
		findings = findings[:3]
	}
	if len(findings) == 0 {
		b.WriteString("no penalties\n")
	}
	for _, p := range findings {
		fmt.Fprintf(&b, "−%d %s %s\n", roundInt(p.Points), p.ID, p.Detail)
	}
	b.WriteString("\n")

	if len(c.Roles) > 0 {
		b.WriteString("role         tokens  calls  err  penalty  model\n")
		for _, r := range c.Roles {
			fmt.Fprintf(&b, "%-12s %6s %6d %4d  %7s  %s\n",
				r.Role, humanTokens(r.Tokens), r.Calls, r.Errors, penaltyCell(r.Penalty), orDash(r.Model))
		}
	}
	if len(c.Facts.ToolsTop) > 0 {
		parts := make([]string, 0, len(c.Facts.ToolsTop))
		for _, tc := range c.Facts.ToolsTop {
			parts = append(parts, fmt.Sprintf("%s %d", tc.Tool, tc.Count))
		}
		fmt.Fprintf(&b, "tools: %s\n", strings.Join(parts, " · "))
	}
	if len(c.Inputs.TranscriptsMissing) > 0 {
		fmt.Fprintf(&b, "missing transcripts: %s\n", strings.Join(c.Inputs.TranscriptsMissing, ", "))
	}
	if c.RunLog == "" {
		b.WriteString("run log: missing\n")
	}
	return b.String()
}

func slug(c Card) string {
	if c.RunLog != "" {
		return strings.TrimSuffix(filepath.Base(c.RunLog), ".md")
	}
	if n := len(c.RunID); n > 8 {
		return c.RunID[n-8:]
	}
	return orDash(c.RunID)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func roundInt(f float64) int { return int(f + 0.5) }

func penaltyCell(p float64) string {
	if p <= 0 {
		return "0"
	}
	return fmt.Sprintf("−%d", roundInt(p))
}

func humanTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e6), ".0") + "M"
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

func humanDuration(ms int64) string {
	if ms <= 0 {
		return "—"
	}
	min := ms / 60000
	if min < 1 {
		return fmt.Sprintf("%ds", ms/1000)
	}
	if min >= 120 {
		return fmt.Sprintf("%.1fh", float64(min)/60)
	}
	return fmt.Sprintf("%d min", min)
}

func modelSummary(counts map[string]int) string {
	if len(counts) == 0 {
		return "—"
	}
	type kv struct {
		k string
		v int
	}
	items := make([]kv, 0, len(counts))
	for k, v := range counts {
		items = append(items, kv{k, v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].v != items[j].v {
			return items[i].v > items[j].v
		}
		return items[i].k < items[j].k
	})
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%s×%d", it.k, it.v))
	}
	return strings.Join(parts, " ")
}
