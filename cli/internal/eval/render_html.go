package eval

import (
	"fmt"
	"html"
	"strings"
	"time"
)

// RenderHTML produces a self-contained styled HTML report. No external
// assets, no remote CSS, no JS. Renders in any browser and is safe to
// commit or share via email attachment. Theme respects prefers-color-scheme
// so a dark-mode user gets a dark page automatically.
func RenderHTML(s SessionScore) string {
	var b strings.Builder
	fmt.Fprint(&b, htmlHead(fmt.Sprintf("zprof eval — %s", short(s.Meta.SessionID))))

	fmt.Fprintln(&b, `<main class="wrap">`)
	writeHTMLHeader(&b, s)
	writeHTMLScoreboard(&b, s)
	writeHTMLTokenChart(&b, s)
	writeHTMLViolations(&b, s)
	writeHTMLFooter(&b)
	fmt.Fprintln(&b, `</main></body></html>`)
	return b.String()
}

// -----------------------------------------------------------------------------

func writeHTMLHeader(b *strings.Builder, s SessionScore) {
	fmt.Fprintln(b, `<header>`)
	fmt.Fprintf(b, `<h1>zprof eval <span class="dim">— %s</span></h1>`, hesc(short(s.Meta.SessionID)))
	fmt.Fprintln(b)
	fmt.Fprintf(b, `<p class="tier1-note">Tier 1 — deterministic. No LLM ran. Zero tokens spent producing this page.</p>`)
	fmt.Fprintln(b)

	fmt.Fprintln(b, `<dl class="session-meta">`)
	dl(b, "Session ID", `<code>`+hesc(s.Meta.SessionID)+`</code>`)
	dl(b, "Log path", `<code>`+hesc(s.Meta.Path)+`</code>`)
	if !s.Meta.FirstTimestamp.IsZero() {
		span := s.Meta.LastTimestamp.Sub(s.Meta.FirstTimestamp)
		dl(b, "Span",
			fmt.Sprintf(`<strong>%s</strong> <span class="dim">(%s → %s)</span>`,
				hesc(shortDuration(span)),
				hesc(s.Meta.FirstTimestamp.UTC().Format(time.RFC3339)),
				hesc(s.Meta.LastTimestamp.UTC().Format(time.RFC3339)),
			))
	}
	dl(b, "Main model", `<code>`+hesc(nz(s.Meta.MainLoopModel, "unknown"))+`</code>`)

	subagentTotal := 0
	dispatches := 0
	for _, r := range s.Roles {
		subagentTotal += r.TotalTokens
		dispatches += r.Dispatches
	}

	dl(b, "Main-loop tokens",
		fmt.Sprintf(`in %s / out %s <span class="dim">(cache: %s read, %s create)</span>`,
			fmtInt(s.Meta.MainLoopIn), fmtInt(s.Meta.MainLoopOut),
			fmtInt(s.Meta.CacheRead), fmtInt(s.Meta.CacheCreate)))
	dl(b, "Subagent tokens",
		fmt.Sprintf(`<strong>%s</strong> output across <strong>%d</strong> dispatches`,
			fmtInt(subagentTotal), dispatches))
	fmt.Fprintln(b, `</dl>`)
	fmt.Fprintln(b, `</header>`)
}

func writeHTMLScoreboard(b *strings.Builder, s SessionScore) {
	fmt.Fprintln(b, `<section>`)
	fmt.Fprintln(b, `<h2>Per-role scorecard</h2>`)
	fmt.Fprintln(b, `<div class="scroll">`)
	fmt.Fprintln(b, `<table class="scoreboard">`)
	fmt.Fprintln(b, `<thead><tr>`)
	fmt.Fprintln(b, `<th>Role</th><th>Model</th><th>N</th><th>Pass@1</th><th>Median tok</th><th>ApT</th><th>Compliance</th><th>Notes</th>`)
	fmt.Fprintln(b, `</tr></thead>`)
	fmt.Fprintln(b, `<tbody>`)
	for _, r := range s.Roles {
		passCls := passClass(r.PassAt1)
		compliance := renderComplianceHTML(r)
		notes := ""
		if r.ConfidenceCount > 0 {
			notes = fmt.Sprintf(`avg conf <strong>%.2f</strong> <span class="dim">(%d/%d)</span>`,
				r.AvgConfidence, r.ConfidenceCount, r.Dispatches)
		}
		fmt.Fprintf(b,
			`<tr><td><code>%s</code></td><td><code>%s</code></td><td class="num">%d</td><td class="num %s">%.2f</td><td class="num">%s</td><td class="num">%.1f</td><td>%s</td><td>%s</td></tr>`+"\n",
			hesc(r.Role),
			hesc(nz(r.Model, "(inherited)")),
			r.Dispatches,
			passCls, r.PassAt1,
			hesc(fmtInt(r.MedianTokens)),
			r.ApT,
			compliance,
			notes,
		)
	}
	fmt.Fprintln(b, `</tbody></table>`)
	fmt.Fprintln(b, `</div></section>`)
}

func writeHTMLTokenChart(b *strings.Builder, s SessionScore) {
	if len(s.Roles) == 0 {
		return
	}
	// Bar chart of total output tokens per role. Sort descending for a
	// pareto-style ordering that reads left-to-right.
	type entry struct {
		role   string
		tokens int
	}
	items := make([]entry, 0, len(s.Roles))
	max := 0
	for _, r := range s.Roles {
		items = append(items, entry{r.Role, r.TotalTokens})
		if r.TotalTokens > max {
			max = r.TotalTokens
		}
	}
	// Descending by tokens.
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].tokens > items[j-1].tokens; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
	if max == 0 {
		return
	}
	fmt.Fprintln(b, `<section>`)
	fmt.Fprintln(b, `<h2>Token spend by role</h2>`)
	fmt.Fprintln(b, `<div class="scroll"><table class="bars"><tbody>`)
	for _, e := range items {
		if e.tokens == 0 {
			continue
		}
		pct := int(float64(e.tokens) * 100 / float64(max))
		fmt.Fprintf(b,
			`<tr><td class="bars-role"><code>%s</code></td>`+
				`<td class="bars-cell"><div class="bar" style="width:%d%%"></div></td>`+
				`<td class="num">%s</td></tr>`+"\n",
			hesc(e.role), pct, hesc(fmtInt(e.tokens)),
		)
	}
	fmt.Fprintln(b, `</tbody></table></div>`)
	fmt.Fprintln(b, `</section>`)
}

func writeHTMLViolations(b *strings.Builder, s SessionScore) {
	fmt.Fprintln(b, `<section>`)
	fmt.Fprintln(b, `<h2>Contract violations</h2>`)
	if len(s.Violations) == 0 {
		fmt.Fprintln(b, `<p class="ok">Clean. Every dispatch returned, wrote its claimed artifact, and routed to a reachable next agent.</p>`)
		fmt.Fprintln(b, `</section>`)
		return
	}
	counts := map[string]int{}
	for _, v := range s.Violations {
		counts[v.Kind]++
	}
	fmt.Fprintln(b, `<div class="badges">`)
	for kind, n := range counts {
		fmt.Fprintf(b, `<span class="badge %s">%s × %d</span>`, violationClass(kind), hesc(kind), n)
	}
	fmt.Fprintln(b, `</div>`)

	fmt.Fprintln(b, `<details class="violations-list">`)
	fmt.Fprintf(b, `<summary>%d violations — click to expand</summary>`, len(s.Violations))
	fmt.Fprintln(b)
	fmt.Fprintln(b, `<div class="scroll"><table><thead><tr><th>#</th><th>Role</th><th>Kind</th><th>Detail</th></tr></thead><tbody>`)
	for i, v := range s.Violations {
		fmt.Fprintf(b,
			`<tr><td class="num">%d</td><td><code>%s</code></td><td><span class="badge %s">%s</span></td><td class="detail">%s</td></tr>`+"\n",
			i+1,
			hesc(v.Role),
			violationClass(v.Kind),
			hesc(v.Kind),
			hesc(v.Detail),
		)
	}
	fmt.Fprintln(b, `</tbody></table></div>`)
	fmt.Fprintln(b, `</details>`)
	fmt.Fprintln(b, `</section>`)
}

func writeHTMLFooter(b *strings.Builder) {
	fmt.Fprintln(b, `<section class="footer">`)
	fmt.Fprintln(b, `<h2>What Tier 2 would add</h2>`)
	fmt.Fprintln(b, `<ul>`)
	fmt.Fprintln(b, `<li>Panel-judge quality (1-5) per dispatch across correctness / adherence / efficiency framings.</li>`)
	fmt.Fprintln(b, `<li>Model-tier recommendation per role (advisory).</li>`)
	fmt.Fprintln(b, `<li>Reasoning-quality assessment on ADR / review artifacts.</li>`)
	fmt.Fprintln(b, `</ul>`)
	fmt.Fprintln(b, `<p class="dim">Run <code>zprof eval --deep &lt;sessionId&gt;</code> to dispatch the <code>evaluator</code> subagent.</p>`)
	fmt.Fprintln(b, `</section>`)
}

// -----------------------------------------------------------------------------

func passClass(p float64) string {
	switch {
	case p >= 0.9:
		return "ok"
	case p >= 0.6:
		return "warn"
	default:
		return "bad"
	}
}

func renderComplianceHTML(r RoleStats) string {
	if r.ArtifactMissing == 0 && r.HadPreamble == 0 && r.NextUnreachable == 0 {
		return `<span class="ok">clean</span>`
	}
	var parts []string
	if r.ArtifactMissing > 0 {
		parts = append(parts, fmt.Sprintf(`<span class="badge bad">artifact-missing×%d</span>`, r.ArtifactMissing))
	}
	if r.HadPreamble > 0 {
		parts = append(parts, fmt.Sprintf(`<span class="badge warn">preamble×%d</span>`, r.HadPreamble))
	}
	if r.NextUnreachable > 0 {
		parts = append(parts, fmt.Sprintf(`<span class="badge warn">next-broken×%d</span>`, r.NextUnreachable))
	}
	return strings.Join(parts, " ")
}

func violationClass(kind string) string {
	switch kind {
	case "artifact-missing", "dispatch-never-returned":
		return "bad"
	case "return-preamble", "next-unreachable":
		return "warn"
	default:
		return ""
	}
}

func dl(b *strings.Builder, k, v string) {
	fmt.Fprintf(b, `<dt>%s</dt><dd>%s</dd>`, hesc(k), v)
	fmt.Fprintln(b)
}

func hesc(s string) string { return html.EscapeString(s) }

func htmlHead(title string) string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + hesc(title) + `</title>
<style>
:root {
  --bg: #ffffff;
  --fg: #1a1a1a;
  --dim: #6b7280;
  --muted: #f3f4f6;
  --border: #e5e7eb;
  --ok: #10b981;
  --ok-bg: #d1fae5;
  --warn: #f59e0b;
  --warn-bg: #fef3c7;
  --bad: #ef4444;
  --bad-bg: #fee2e2;
  --accent: #6366f1;
  --code-bg: #f3f4f6;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #0f172a;
    --fg: #e5e7eb;
    --dim: #94a3b8;
    --muted: #1e293b;
    --border: #334155;
    --ok-bg: #064e3b;
    --warn-bg: #78350f;
    --bad-bg: #7f1d1d;
    --code-bg: #1e293b;
  }
}
* { box-sizing: border-box; }
body {
  margin: 0;
  padding: 0;
  font-family: -apple-system, BlinkMacSystemFont, "SF Pro Text", "Segoe UI", system-ui, sans-serif;
  font-size: 14px;
  line-height: 1.5;
  color: var(--fg);
  background: var(--bg);
}
main.wrap {
  max-width: 1100px;
  margin: 0 auto;
  padding: 2rem 1.5rem 4rem;
}
h1 { font-size: 1.6rem; margin: 0 0 0.5rem; }
h1 .dim { color: var(--dim); font-weight: 400; }
h2 { font-size: 1.15rem; margin: 2rem 0 0.75rem; border-bottom: 1px solid var(--border); padding-bottom: 0.35rem; }
header .tier1-note { color: var(--dim); font-style: italic; margin-top: 0; }
code {
  font-family: "SF Mono", ui-monospace, "Menlo", monospace;
  font-size: 0.9em;
  padding: 0.1em 0.35em;
  background: var(--code-bg);
  border-radius: 3px;
}
dl.session-meta {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 0.35rem 1rem;
  margin: 1rem 0;
}
dl.session-meta dt {
  color: var(--dim);
  font-weight: 500;
}
dl.session-meta dd { margin: 0; word-break: break-all; }
.dim { color: var(--dim); }
.scroll { overflow-x: auto; }
table {
  border-collapse: collapse;
  width: 100%;
  font-size: 0.92rem;
}
table thead th {
  text-align: left;
  padding: 0.5rem 0.75rem;
  border-bottom: 1px solid var(--border);
  color: var(--dim);
  font-weight: 600;
  font-size: 0.8rem;
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
table tbody td {
  padding: 0.5rem 0.75rem;
  border-bottom: 1px solid var(--border);
  vertical-align: top;
}
table tbody tr:hover { background: var(--muted); }
td.num { text-align: right; font-variant-numeric: tabular-nums; }
td.detail { max-width: 55ch; word-break: break-word; color: var(--dim); font-size: 0.85rem; }

.badge {
  display: inline-block;
  padding: 0.15em 0.55em;
  border-radius: 3px;
  font-size: 0.75rem;
  font-weight: 600;
  background: var(--muted);
  color: var(--fg);
}
.badge.ok    { background: var(--ok-bg);   color: var(--ok); }
.badge.warn  { background: var(--warn-bg); color: var(--warn); }
.badge.bad   { background: var(--bad-bg);  color: var(--bad); }
.badges { margin: 0.5rem 0; }
.badges .badge { margin-right: 0.35rem; }

.num.ok   { color: var(--ok);   font-weight: 600; }
.num.warn { color: var(--warn); font-weight: 600; }
.num.bad  { color: var(--bad);  font-weight: 600; }

.ok { color: var(--ok); }
p.ok { padding: 0.5rem 0.75rem; background: var(--ok-bg); border-left: 3px solid var(--ok); border-radius: 3px; }

table.bars td.bars-role { width: 12rem; }
table.bars td.bars-cell { width: 100%; }
table.bars .bar {
  height: 0.75rem;
  background: linear-gradient(90deg, var(--accent), #8b5cf6);
  border-radius: 2px;
}

details.violations-list summary {
  cursor: pointer;
  padding: 0.35rem 0;
  color: var(--dim);
  font-size: 0.9rem;
}
details.violations-list summary:hover { color: var(--fg); }
details.violations-list[open] summary { margin-bottom: 0.5rem; }

section.footer { margin-top: 3rem; color: var(--dim); }
section.footer h2 { color: var(--fg); }
section.footer ul { line-height: 1.7; }
</style>
</head>
<body>
`
}
