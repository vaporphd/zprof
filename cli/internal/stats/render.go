package stats

import (
	"fmt"
	"html"
	"strings"
)

// RenderHTML produces a self-contained styled HTML report from aggregated
// telemetry data. No external assets, no remote CSS, no JS. Renders in any
// browser and is safe to commit or share via email attachment. Theme respects
// prefers-color-scheme so a dark-mode user gets a dark page automatically.
func RenderHTML(r *Report) string {
	var b strings.Builder
	writeHead(&b, r)
	fmt.Fprintln(&b, `<main class="report">`)
	writeReportHeader(&b, r)
	writeNav(&b)
	writeTrustBlock(&b, r)
	writeActionQueue(&b, r)
	writeVolumeStrip(&b, r)
	writeEconomicsTable(&b, r)
	writeContractSection(&b, r)
	writeReportCatalog(&b, r)
	writeDefinitions(&b)
	fmt.Fprintln(&b, `</main></body></html>`)
	return b.String()
}

// -----------------------------------------------------------------------------
// action items generated from report data
// -----------------------------------------------------------------------------

type action struct {
	severity string // "P0", "P1", "P2"
	title    string
	detail   string
	next     string
}

func generateActions(r *Report) []action {
	var out []action

	// P0: consolidate all roles with compliance < 50% into ONE action
	var failRoles []string
	for _, h := range r.Health {
		if h.PreambleChecked > 0 && h.ComplianceRate < 50 {
			failRoles = append(failRoles, h.Role)
		}
	}
	if len(failRoles) > 0 {
		roleList := strings.Join(failRoles, ", ")
		out = append(out, action{
			severity: "P0",
			title:    "Контракт возврата системно нарушается",
			detail:   fmt.Sprintf("Роли с compliance < 50%%: %s. Разбор по схеме при этом 100%%, проблема в форме ответа.", roleList),
			next:     "Открыть 10 последних нарушений, сгруппировать по config_hash, проверить контракт и scaffold возврата.",
		})
	}

	// P1: missing role > 10%
	if r.TotalDispatches > 0 {
		missingPct := float64(r.Losses.MissingRole) * 100 / float64(r.TotalDispatches)
		if missingPct > 10 {
			out = append(out, action{
				severity: "P1",
				title:    fmt.Sprintf("%.1f%% диспатчей без роли", missingPct),
				detail:   fmt.Sprintf("%d из %d диспатчей не имеют распознанной роли.", r.Losses.MissingRole, r.TotalDispatches),
				next:     "Разложить пропуски по harness version, session и причине.",
			})
		}
	}

	// P1: top role by tokens if > 40% of total
	totalTok := r.Economics.TotalTokens.Total()
	if totalTok > 0 && len(r.Economics.ByRole) > 0 {
		top := r.Economics.ByRole[0]
		share := float64(top.Tokens.Total()) * 100 / float64(totalTok)
		if share > 40 {
			out = append(out, action{
				severity: "P1",
				title:    fmt.Sprintf("%s концентрирует %.1f%% токенов", top.Role, share),
				detail:   fmt.Sprintf("%s из %s токенов при %d диспатчах.", fmtTokens(top.Tokens.Total()), fmtTokens(totalTok), top.Dispatches),
				next:     "Добавить стоимость на принятый run, число повторов и разбивку до/после config_hash.",
			})
		}
	}

	// P2: top 3 roles with P95/P50 ratio > 3
	tailCount := 0
	for _, re := range r.Economics.ByRole {
		if tailCount >= 3 {
			break
		}
		if re.P50Duration > 0 {
			ratio := float64(re.P95Duration) / float64(re.P50Duration)
			if ratio > 3 {
				out = append(out, action{
					severity: "P2",
					title:    fmt.Sprintf("Длинный хвост %s: %.1fx", re.Role, ratio),
					detail:   fmt.Sprintf("p50 = %s, p95 = %s.", fmtDuration(re.P50Duration), fmtDuration(re.P95Duration)),
					next:     "Показать top-10 runs по critical-path duration.",
				})
				tailCount++
			}
		}
	}

	return out
}

// -----------------------------------------------------------------------------
// trust computation
// -----------------------------------------------------------------------------

type trustLevel struct {
	verdict string
	note    string
	color   string // "green", "amber", "red"
}

func computeTrust(r *Report) (transcriptPct float64, unknownRolePct float64, tl trustLevel) {
	if r.TotalDispatches == 0 {
		return 0, 0, trustLevel{"Нет данных", "Нет диспатчей для анализа.", "red"}
	}

	captured := r.TotalDispatches - r.Losses.MissingRole
	transcriptPct = float64(captured) * 100 / float64(r.TotalDispatches)
	unknownRolePct = float64(r.Losses.MissingRole) * 100 / float64(r.TotalDispatches)

	if transcriptPct > 95 && unknownRolePct < 5 {
		tl = trustLevel{"Полное", "Покрытие достаточно для принятия решений.", "green"}
	} else if transcriptPct > 70 {
		tl = trustLevel{"Ограниченное", "Часть данных отсутствует; рейтинги могут быть неполны.", "amber"}
	} else {
		tl = trustLevel{"Недостаточное", "Слишком много пропусков для надёжных выводов.", "red"}
	}
	return
}

// -----------------------------------------------------------------------------
// HTML sections
// -----------------------------------------------------------------------------

func writeHead(b *strings.Builder, r *Report) {
	title := "zprof stats"
	if r.ProjectName != "" {
		title = r.ProjectName + " — Agent Telemetry"
	} else if r.ProjectID != "" {
		title = r.ProjectID + " — Agent Telemetry"
	}
	fmt.Fprintf(b, `<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s</title>
<style>
:root {
  color-scheme: light dark;
  --ground: #e9edf0;
  --paper: #f9faf9;
  --paper-2: #f0f2f2;
  --ink: #10171d;
  --soft: #53616b;
  --faint: #7e8991;
  --rule: #cbd2d6;
  --blue: #176f91;
  --blue-soft: #d7e8ee;
  --amber: #9b6909;
  --amber-soft: #f1e5c9;
  --red: #a43f35;
  --red-soft: #eedbd8;
  --green: #39754a;
  --green-soft: #dce9df;
  --violet: #754779;
  --violet-soft: #e8dfe9;
  --display: "Avenir Next Condensed", "Helvetica Neue Condensed", "Arial Narrow", sans-serif;
  --body: Charter, "Iowan Old Style", Georgia, serif;
  --mono: ui-monospace, "SF Mono", Menlo, Consolas, monospace;
}

@media (prefers-color-scheme: dark) {
  :root {
    --ground: #0d1317;
    --paper: #151c21;
    --paper-2: #1c252b;
    --ink: #e8edef;
    --soft: #a7b2b9;
    --faint: #74818a;
    --rule: #2b3840;
    --blue: #68c1de;
    --blue-soft: #12323e;
    --amber: #e0b557;
    --amber-soft: #352b18;
    --red: #ea8274;
    --red-soft: #3b211f;
    --green: #79c28b;
    --green-soft: #1b3422;
    --violet: #d28bd0;
    --violet-soft: #382139;
  }
}

* { box-sizing: border-box; }
html { scroll-behavior: smooth; -webkit-text-size-adjust: 100%%; }
body {
  margin: 0;
  background: var(--ground);
  color: var(--ink);
  font-family: var(--body);
  font-size: 16px;
  line-height: 1.45;
}
a { color: inherit; }
code { font-family: var(--mono); font-size: .88em; }

.report {
  width: min(1260px, 100%%);
  margin: 0 auto;
  padding: 0 clamp(18px, 4vw, 52px) 80px;
}

.report-head {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 28px;
  align-items: end;
  padding: 44px 0 26px;
  border-bottom: 1px solid var(--rule);
}
.eyebrow {
  margin: 0 0 8px;
  color: var(--faint);
  font-family: var(--mono);
  font-size: 11px;
  letter-spacing: .09em;
  text-transform: uppercase;
}
h1 {
  margin: 0;
  font-family: var(--display);
  font-size: clamp(42px, 6vw, 76px);
  font-weight: 700;
  line-height: .9;
  letter-spacing: -.035em;
}
.head-meta {
  display: grid;
  gap: 5px;
  color: var(--soft);
  font-family: var(--mono);
  font-size: 11px;
  text-align: right;
}

.report-nav {
  position: sticky;
  top: 0;
  z-index: 20;
  display: flex;
  flex-wrap: wrap;
  gap: 4px 16px;
  padding: 11px 0;
  border-bottom: 1px solid var(--rule);
  background: color-mix(in srgb, var(--ground) 91%%, transparent);
  backdrop-filter: blur(9px);
}
.report-nav a {
  color: var(--soft);
  font-family: var(--display);
  font-size: 11px;
  font-weight: 700;
  letter-spacing: .08em;
  text-decoration: none;
  text-transform: uppercase;
}
.report-nav a:hover { color: var(--ink); }

.trust {
  display: grid;
  grid-template-columns: minmax(190px, .72fr) minmax(0, 1.7fr);
  margin-top: 28px;
  border: 1px solid var(--amber);
  background: var(--paper);
}
.trust.trust-green { border-color: var(--green); }
.trust.trust-red { border-color: var(--red); }
.trust-verdict {
  padding: 24px;
  background: var(--amber-soft);
}
.trust.trust-green .trust-verdict { background: var(--green-soft); }
.trust.trust-red .trust-verdict { background: var(--red-soft); }
.trust-label,
.section-label {
  margin: 0 0 8px;
  font-family: var(--mono);
  font-size: 10px;
  letter-spacing: .1em;
  text-transform: uppercase;
}
.trust-label { color: var(--amber); }
.trust.trust-green .trust-label { color: var(--green); }
.trust.trust-red .trust-label { color: var(--red); }
.trust-verdict strong {
  display: block;
  color: var(--amber);
  font-family: var(--display);
  font-size: clamp(32px, 4vw, 50px);
  line-height: .9;
}
.trust.trust-green .trust-verdict strong { color: var(--green); }
.trust.trust-red .trust-verdict strong { color: var(--red); }
.trust-verdict span {
  display: block;
  margin-top: 9px;
  color: var(--soft);
  font-size: 14px;
}
.trust-evidence {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
}
.trust-stat {
  padding: 23px 20px;
  border-left: 1px solid var(--rule);
}
.trust-stat strong {
  display: block;
  font-family: var(--display);
  font-size: clamp(28px, 3.4vw, 42px);
  line-height: 1;
}
.trust-stat span {
  display: block;
  margin-top: 7px;
  color: var(--soft);
  font-size: 13px;
  line-height: 1.25;
}
.trust-bar {
  height: 5px;
  margin-top: 12px;
  background: var(--paper-2);
}
.trust-bar i { display: block; height: 100%%; }

.section {
  margin-top: 42px;
  scroll-margin-top: 60px;
}
.section-head {
  display: flex;
  justify-content: space-between;
  gap: 20px;
  align-items: end;
  margin-bottom: 16px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--rule);
}
.section-label { color: var(--faint); }
h2 {
  margin: 0;
  font-family: var(--display);
  font-size: clamp(28px, 3.8vw, 44px);
  font-weight: 700;
  line-height: .95;
  letter-spacing: -.015em;
}
.section-head p {
  max-width: 46ch;
  margin: 0;
  color: var(--soft);
  font-size: 14px;
  text-align: right;
}

.action-list {
  list-style: none;
  margin: 0;
  padding: 0;
  border-top: 1px solid var(--rule);
}
.action {
  display: grid;
  grid-template-columns: 54px minmax(180px, .9fr) minmax(240px, 1.5fr) minmax(190px, 1fr);
  gap: 16px;
  align-items: start;
  padding: 16px 0;
  border-bottom: 1px solid var(--rule);
}
.sev {
  display: inline-flex;
  justify-content: center;
  padding: 4px 6px;
  font-family: var(--mono);
  font-size: 10px;
  font-weight: 700;
}
.sev-p0 { background: var(--red-soft); color: var(--red); }
.sev-p1 { background: var(--amber-soft); color: var(--amber); }
.sev-p2 { background: var(--blue-soft); color: var(--blue); }
.action strong {
  display: block;
  font-family: var(--display);
  font-size: 19px;
  line-height: 1.05;
}
.action p { margin: 0; color: var(--soft); font-size: 14px; }
.action-next {
  padding-left: 13px;
  border-left: 2px solid var(--rule);
}
.action-next b {
  display: block;
  margin-bottom: 3px;
  color: var(--faint);
  font-family: var(--mono);
  font-size: 9px;
  letter-spacing: .08em;
  text-transform: uppercase;
}

.metric-strip {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  border: 1px solid var(--rule);
  background: var(--paper);
}
.metric {
  padding: 18px;
  border-right: 1px solid var(--rule);
}
.metric:last-child { border-right: 0; }
.metric strong {
  display: block;
  font-family: var(--display);
  font-size: clamp(25px, 3vw, 38px);
  line-height: 1;
}
.metric span { display: block; margin-top: 6px; color: var(--soft); font-size: 12px; }

.table-wrap { overflow-x: auto; }
table { width: 100%%; min-width: 760px; border-collapse: collapse; }
th, td { padding: 10px 12px 10px 0; border-bottom: 1px solid var(--rule); text-align: left; }
th {
  color: var(--faint);
  font-family: var(--display);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: .09em;
  text-transform: uppercase;
}
td { font-size: 14px; }
td.num { font-family: var(--mono); font-size: 12px; text-align: right; white-space: nowrap; }
td.signal { max-width: 260px; color: var(--soft); font-size: 13px; }

.share {
  display: grid;
  grid-template-columns: minmax(80px, 1fr) 42px;
  gap: 8px;
  align-items: center;
}
.share-track { height: 9px; background: var(--paper-2); }
.share-track i { display: block; height: 100%%; background: var(--blue); }
.share span { font-family: var(--mono); font-size: 10px; text-align: right; }

.contract-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.4fr) minmax(260px, .6fr);
  gap: 28px;
}
.contract-note {
  padding: 20px;
  border-left: 4px solid var(--red);
  background: var(--red-soft);
}
.contract-note strong {
  display: block;
  color: var(--red);
  font-family: var(--display);
  font-size: 25px;
  line-height: 1;
}
.contract-note p { margin: 10px 0 0; color: var(--soft); font-size: 14px; }
.contract-note code { color: var(--ink); }
.status {
  display: inline-block;
  padding: 3px 7px;
  font-family: var(--mono);
  font-size: 9px;
  font-weight: 700;
  letter-spacing: .05em;
}
.status-fail { background: var(--red-soft); color: var(--red); }
.status-warn { background: var(--amber-soft); color: var(--amber); }
.status-ok   { background: var(--green-soft); color: var(--green); }
.status-gap  { background: var(--amber-soft); color: var(--amber); }
.status-ready { background: var(--green-soft); color: var(--green); }

.report-catalog {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1px;
  border: 1px solid var(--rule);
  background: var(--rule);
}
.report-card {
  min-height: 170px;
  padding: 20px;
  background: var(--paper);
}
.report-card-top {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: start;
}
.report-card h3 {
  margin: 0;
  font-family: var(--display);
  font-size: 23px;
  line-height: 1;
}
.report-card p { margin: 11px 0 0; color: var(--soft); font-size: 14px; }
.report-card dl {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 3px 9px;
  margin: 14px 0 0;
  font-size: 12px;
}
.report-card dt { color: var(--faint); font-family: var(--mono); }
.report-card dd { margin: 0; color: var(--soft); }

.definitions {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 20px;
  color: var(--soft);
  font-size: 12px;
}
.definitions strong { color: var(--ink); font-family: var(--display); font-size: 15px; }

@media (max-width: 820px) {
  .report-head { grid-template-columns: 1fr; align-items: start; }
  .head-meta { text-align: left; }
  .trust { grid-template-columns: 1fr; }
  .trust-evidence { border-top: 1px solid var(--rule); }
  .trust-stat:first-child { border-left: 0; }
  .action { grid-template-columns: 48px 1fr; }
  .action p, .action-next { grid-column: 2; }
  .metric-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .metric:nth-child(2) { border-right: 0; }
  .metric:nth-child(-n+2) { border-bottom: 1px solid var(--rule); }
  .contract-grid { grid-template-columns: 1fr; }
  .report-catalog { grid-template-columns: 1fr; }
  .definitions { grid-template-columns: 1fr; }
}
@media (max-width: 520px) {
  .trust-evidence { grid-template-columns: 1fr; }
  .trust-stat { border-left: 0; border-bottom: 1px solid var(--rule); }
  .trust-stat:last-child { border-bottom: 0; }
  .section-head { align-items: start; flex-direction: column; }
  .section-head p { text-align: left; }
  .metric-strip { grid-template-columns: 1fr; }
  .metric { border-right: 0; border-bottom: 1px solid var(--rule); }
  .metric:last-child { border-bottom: 0; }
}
@media print {
  :root {
    color-scheme: light;
    --ground: #ffffff; --paper: #ffffff; --paper-2: #f1f3f3;
    --ink: #10171d; --soft: #53616b; --faint: #7e8991; --rule: #cbd2d6;
    --blue: #176f91; --blue-soft: #d7e8ee; --amber: #9b6909; --amber-soft: #f1e5c9;
    --red: #a43f35; --red-soft: #eedbd8; --green: #39754a; --green-soft: #dce9df;
    --violet: #754779; --violet-soft: #e8dfe9;
  }
  .report-nav { display: none; }
  .section { break-inside: avoid; }
}
</style>
</head>
<body>
`, hesc(title))
}

func writeReportHeader(b *strings.Builder, r *Report) {
	heading := r.ProjectName
	if heading == "" {
		heading = r.ProjectID
	}
	if heading == "" {
		heading = "unknown"
	}

	days := 0
	if !r.TimeRange[0].IsZero() && !r.TimeRange[1].IsZero() {
		days = int(r.TimeRange[1].Sub(r.TimeRange[0]).Hours()/24) + 1
	}

	fmt.Fprintln(b, `<header class="report-head">`)
	fmt.Fprintln(b, `<div>`)
	fmt.Fprintf(b, `<p class="eyebrow">Agent telemetry · decision report</p>`)
	fmt.Fprintln(b)
	fmt.Fprintf(b, `<h1>%s</h1>`, hesc(heading))
	fmt.Fprintln(b)
	fmt.Fprintln(b, `</div>`)
	fmt.Fprintln(b, `<div class="head-meta">`)
	fmt.Fprintf(b, `<span>%d сессий · %d дней</span>`, r.Sessions, days)
	fmt.Fprintln(b)
	if !r.TimeRange[0].IsZero() {
		fmt.Fprintf(b, `<span>%s → %s</span>`,
			hesc(r.TimeRange[0].UTC().Format("2006-01-02")),
			hesc(r.TimeRange[1].UTC().Format("2006-01-02")))
		fmt.Fprintln(b)
	}
	if r.ProjectID != "" {
		fmt.Fprintf(b, `<span>project_id: %s</span>`, hesc(r.ProjectID))
	}
	fmt.Fprintln(b)
	if r.Harness != "" {
		fmt.Fprintf(b, `<span>harness: %s</span>`, hesc(r.Harness))
		fmt.Fprintln(b)
	}
	fmt.Fprintln(b, `</div>`)
	fmt.Fprintln(b, `</header>`)
}

func writeNav(b *strings.Builder) {
	fmt.Fprintln(b, `<nav class="report-nav" aria-label="Sections">`)
	fmt.Fprintln(b, `<a href="#actions">Действия</a>`)
	fmt.Fprintln(b, `<a href="#economics">Экономика</a>`)
	fmt.Fprintln(b, `<a href="#contract">Контракт</a>`)
	fmt.Fprintln(b, `<a href="#reports">Отчёты</a>`)
	fmt.Fprintln(b, `<a href="#definitions">Ограничения</a>`)
	fmt.Fprintln(b, `</nav>`)
}

func writeTrustBlock(b *strings.Builder, r *Report) {
	transcriptPct, unknownPct, tl := computeTrust(r)

	trustClass := "trust"
	switch tl.color {
	case "green":
		trustClass += " trust-green"
	case "red":
		trustClass += " trust-red"
	}

	barColor := "var(--amber)"
	switch tl.color {
	case "green":
		barColor = "var(--green)"
	case "red":
		barColor = "var(--red)"
	}

	fmt.Fprintf(b, `<section class="%s" aria-labelledby="trust-title">`, trustClass)
	fmt.Fprintln(b)
	fmt.Fprintln(b, `<div class="trust-verdict">`)
	fmt.Fprintln(b, `<p class="trust-label" id="trust-title">Доверие к отчёту</p>`)
	fmt.Fprintf(b, `<strong>%s</strong>`, hesc(tl.verdict))
	fmt.Fprintln(b)
	fmt.Fprintf(b, `<span>%s</span>`, hesc(tl.note))
	fmt.Fprintln(b)
	fmt.Fprintln(b, `</div>`)

	fmt.Fprintln(b, `<div class="trust-evidence">`)
	// transcript coverage
	fmt.Fprintln(b, `<div class="trust-stat">`)
	fmt.Fprintf(b, `<strong>%s</strong><span>покрытие транскриптов</span>`, hesc(fmtPct(int(transcriptPct*10), 1000)))
	fmt.Fprintln(b)
	fmt.Fprintf(b, `<div class="trust-bar"><i style="width:%.1f%%;background:%s"></i></div>`, transcriptPct, barColor)
	fmt.Fprintln(b)
	fmt.Fprintln(b, `</div>`)

	// unknown role
	fmt.Fprintln(b, `<div class="trust-stat">`)
	fmt.Fprintf(b, `<strong>%d</strong><span>диспатчей без роли · %s</span>`,
		r.Losses.MissingRole, hesc(fmtPct(int(unknownPct*10), 1000)))
	fmt.Fprintln(b)
	covPct := 100 - unknownPct
	if covPct < 0 {
		covPct = 0
	}
	fmt.Fprintf(b, `<div class="trust-bar"><i style="width:%.1f%%;background:%s"></i></div>`, covPct, barColor)
	fmt.Fprintln(b)
	fmt.Fprintln(b, `</div>`)
	fmt.Fprintln(b, `</div>`)

	fmt.Fprintln(b, `</section>`)
}

func writeActionQueue(b *strings.Builder, r *Report) {
	actions := generateActions(r)

	fmt.Fprintln(b, `<section class="section" id="actions">`)
	fmt.Fprintln(b, `<header class="section-head">`)
	fmt.Fprintln(b, `<div><p class="section-label">Сначала смотреть сюда</p><h2>Очередь действий</h2></div>`)
	fmt.Fprintln(b, `<p>Сигналы отсортированы по риску ошибочного решения.</p>`)
	fmt.Fprintln(b, `</header>`)

	if len(actions) == 0 {
		fmt.Fprintln(b, `<p style="color:var(--green)">Нет критических сигналов.</p>`)
		fmt.Fprintln(b, `</section>`)
		return
	}

	fmt.Fprintln(b, `<ol class="action-list">`)
	for _, a := range actions {
		sevClass := "sev-p2"
		switch a.severity {
		case "P0":
			sevClass = "sev-p0"
		case "P1":
			sevClass = "sev-p1"
		}
		fmt.Fprintln(b, `<li class="action">`)
		fmt.Fprintf(b, `<span class="sev %s">%s</span>`, sevClass, hesc(a.severity))
		fmt.Fprintln(b)
		fmt.Fprintf(b, `<strong>%s</strong>`, hesc(a.title))
		fmt.Fprintln(b)
		fmt.Fprintf(b, `<p>%s</p>`, hesc(a.detail))
		fmt.Fprintln(b)
		fmt.Fprintf(b, `<p class="action-next"><b>Следующее действие</b>%s</p>`, hesc(a.next))
		fmt.Fprintln(b)
		fmt.Fprintln(b, `</li>`)
	}
	fmt.Fprintln(b, `</ol>`)
	fmt.Fprintln(b, `</section>`)
}

func writeVolumeStrip(b *strings.Builder, r *Report) {
	totalTok := r.Economics.TotalTokens.Total()
	cachePct := 0.0
	if totalTok > 0 {
		cachePct = float64(r.Economics.TotalTokens.CacheRead) * 100 / float64(totalTok)
	}

	// Compute overall p50/p95 from per-role data (use top role as proxy).
	p50Str, p95Str := "--", "--"
	if len(r.Economics.ByRole) > 0 {
		top := r.Economics.ByRole[0]
		if top.P50Duration > 0 {
			p50Str = fmtDuration(top.P50Duration)
		}
		if top.P95Duration > 0 {
			p95Str = fmtDuration(top.P95Duration)
		}
	}

	fmt.Fprintln(b, `<section class="section">`)
	fmt.Fprintln(b, `<header class="section-head">`)
	fmt.Fprintln(b, `<div><p class="section-label">Контекст окна</p><h2>Объём наблюдения</h2></div>`)
	fmt.Fprintln(b, `<p>Raw tokens полезны для объёма, но не равны стоимости.</p>`)
	fmt.Fprintln(b, `</header>`)
	fmt.Fprintln(b, `<div class="metric-strip">`)
	fmt.Fprintf(b, `<div class="metric"><strong>%d</strong><span>диспатчей</span></div>`, r.TotalDispatches)
	fmt.Fprintln(b)
	fmt.Fprintf(b, `<div class="metric"><strong>%s</strong><span>raw tokens</span></div>`, hesc(fmtTokens(totalTok)))
	fmt.Fprintln(b)
	fmt.Fprintf(b, `<div class="metric"><strong>%.1f%%</strong><span>cache read</span></div>`, cachePct)
	fmt.Fprintln(b)
	fmt.Fprintf(b, `<div class="metric"><strong>%s / %s</strong><span>p50 / p95 (top role)</span></div>`,
		hesc(p50Str), hesc(p95Str))
	fmt.Fprintln(b)
	fmt.Fprintln(b, `</div>`)
	fmt.Fprintln(b, `</section>`)
}

func writeEconomicsTable(b *strings.Builder, r *Report) {
	totalTok := r.Economics.TotalTokens.Total()

	fmt.Fprintln(b, `<section class="section" id="economics">`)
	fmt.Fprintln(b, `<header class="section-head">`)
	fmt.Fprintln(b, `<div><p class="section-label">Где сосредоточен расход</p><h2>Экономика ролей</h2></div>`)
	fmt.Fprintln(b, `<p>Доля считается от всех raw tokens.</p>`)
	fmt.Fprintln(b, `</header>`)
	fmt.Fprintln(b, `<div class="table-wrap">`)
	fmt.Fprintln(b, `<table>`)
	fmt.Fprintln(b, `<thead><tr><th>Роль</th><th class="num">Диспатчи</th><th class="num">Токены</th><th>Доля</th><th class="num">Среднее</th><th class="num">p50 / p95</th><th>Сигнал</th></tr></thead>`)
	fmt.Fprintln(b, `<tbody>`)

	for _, re := range r.Economics.ByRole {
		tok := re.Tokens.Total()
		share := 0.0
		if totalTok > 0 {
			share = float64(tok) * 100 / float64(totalTok)
		}

		p50p95 := "--"
		if re.P50Duration > 0 || re.P95Duration > 0 {
			p50p95 = fmt.Sprintf("%s / %s", fmtDuration(re.P50Duration), fmtDuration(re.P95Duration))
		}

		signal := roleSignal(re, share, r)

		fmt.Fprintf(b, `<tr><td><code>%s</code></td>`, hesc(re.Role))
		fmt.Fprintf(b, `<td class="num">%d</td>`, re.Dispatches)
		fmt.Fprintf(b, `<td class="num">%s</td>`, hesc(fmtTokens(tok)))
		fmt.Fprintf(b, `<td><div class="share"><div class="share-track"><i style="width:%.1f%%"></i></div><span>%.1f%%</span></div></td>`, share, share)
		fmt.Fprintf(b, `<td class="num">%s</td>`, hesc(fmtTokens(re.AvgPerDispatch)))
		fmt.Fprintf(b, `<td class="num">%s</td>`, hesc(p50p95))
		fmt.Fprintf(b, `<td class="signal">%s</td>`, hesc(signal))
		fmt.Fprintln(b, `</tr>`)
	}

	fmt.Fprintln(b, `</tbody></table>`)
	fmt.Fprintln(b, `</div>`)
	fmt.Fprintln(b, `</section>`)
}

func roleSignal(re RoleEconomics, share float64, r *Report) string {
	if share > 40 {
		return "Главный кандидат на run-level разбор."
	}
	// check if this role has compliance issues
	for _, h := range r.Health {
		if h.Role == re.Role && h.PreambleChecked > 0 && h.ComplianceRate < 90 {
			return fmt.Sprintf("Compliance %.0f%% — нужен пересмотр контракта.", h.ComplianceRate)
		}
	}
	if re.P50Duration > 0 && re.P95Duration > 0 {
		ratio := float64(re.P95Duration) / float64(re.P50Duration)
		if ratio > 3 {
			return fmt.Sprintf("Длинный хвост: p95/p50 = %.1fx.", ratio)
		}
	}
	return ""
}

func writeContractSection(b *strings.Builder, r *Report) {
	// Filter health entries that have preamble checked
	var checked []RoleHealth
	for _, h := range r.Health {
		if h.PreambleChecked > 0 {
			checked = append(checked, h)
		}
	}

	fmt.Fprintln(b, `<section class="section" id="contract">`)
	fmt.Fprintln(b, `<header class="section-head">`)
	fmt.Fprintln(b, `<div><p class="section-label">Class A · порог задан контрактом</p><h2>Здоровье возвратов</h2></div>`)
	fmt.Fprintln(b, `<p>Зелёный статус требует 100% соответствия.</p>`)
	fmt.Fprintln(b, `</header>`)

	if len(checked) == 0 {
		fmt.Fprintln(b, `<p style="color:var(--soft)">Нет данных о проверке контрактов.</p>`)
		fmt.Fprintln(b, `</section>`)
		return
	}

	// Check if there are failures to show the contract note
	hasFail := false
	for _, h := range checked {
		if h.ComplianceRate < 100 {
			hasFail = true
			break
		}
	}

	fmt.Fprintln(b, `<div class="contract-grid">`)
	fmt.Fprintln(b, `<div class="table-wrap">`)
	fmt.Fprintln(b, `<table>`)
	fmt.Fprintln(b, `<thead><tr><th>Роль</th><th class="num">N</th><th class="num">Compliance</th><th class="num">Разобран</th><th>Статус</th></tr></thead>`)
	fmt.Fprintln(b, `<tbody>`)

	for _, h := range checked {
		statusCls := "status-ok"
		statusText := "OK"
		if h.ComplianceRate < 90 {
			statusCls = "status-fail"
			statusText = "FAIL"
		} else if h.ComplianceRate < 100 {
			statusCls = "status-warn"
			statusText = "WARN"
		}

		parsedPct := 0.0
		if h.ParsedChecked > 0 {
			parsedPct = float64(h.ParsedCount) * 100 / float64(h.ParsedChecked)
		}

		fmt.Fprintf(b, `<tr><td><code>%s</code></td>`, hesc(h.Role))
		fmt.Fprintf(b, `<td class="num">%d</td>`, h.PreambleChecked)
		fmt.Fprintf(b, `<td class="num">%.0f%%</td>`, h.ComplianceRate)
		fmt.Fprintf(b, `<td class="num">%.0f%%</td>`, parsedPct)
		fmt.Fprintf(b, `<td><span class="status %s">%s</span></td>`, statusCls, statusText)
		fmt.Fprintln(b, `</tr>`)
	}

	fmt.Fprintln(b, `</tbody></table>`)
	fmt.Fprintln(b, `</div>`)

	if hasFail {
		fmt.Fprintln(b, `<aside class="contract-note">`)
		fmt.Fprintln(b, `<strong>Контракт нарушен</strong>`)
		fmt.Fprintln(b, `<p>Одна или несколько ролей не достигают 100% compliance. Локализуйте проблему: поведение роли или формулировка return contract.</p>`)
		fmt.Fprintln(b, `</aside>`)
	}

	fmt.Fprintln(b, `</div>`)
	fmt.Fprintln(b, `</section>`)
}

func writeReportCatalog(b *strings.Builder, r *Report) {
	fmt.Fprintln(b, `<section class="section" id="reports">`)
	fmt.Fprintln(b, `<header class="section-head">`)
	fmt.Fprintln(b, `<div><p class="section-label">Что добавить следующим</p><h2>Недостающие decision reports</h2></div>`)
	fmt.Fprintln(b, `<p>Отчёты, которые закрывают вопросы за пределами агрегатов по диспатчам.</p>`)
	fmt.Fprintln(b, `</header>`)

	// Determine status badges based on available data
	driftStatus := `<span class="status status-gap">нет данных</span>`
	if len(r.Drift) > 1 {
		driftStatus = `<span class="status status-ready">можно строить</span>`
	}

	modelStatus := `<span class="status status-gap">нет данных</span>`
	if len(r.Economics.ByModel) > 0 {
		for _, m := range r.Economics.ByModel {
			if m.Model == "unknown" && m.Dispatches > 0 {
				pct := 0.0
				total := 0
				for _, mm := range r.Economics.ByModel {
					total += mm.Dispatches
				}
				if total > 0 {
					pct = float64(m.Dispatches) * 100 / float64(total)
				}
				modelStatus = fmt.Sprintf(`<span class="status status-gap">%.0f%% unknown</span>`, pct)
				break
			}
		}
		hasUnknown := false
		for _, m := range r.Economics.ByModel {
			if m.Model == "unknown" {
				hasUnknown = true
				break
			}
		}
		if !hasUnknown {
			modelStatus = `<span class="status status-ready">можно строить</span>`
		}
	}

	type card struct {
		title  string
		status string
		desc   string
		answQ  string
		needQ  string
	}

	cards := []card{
		{
			title:  "Runs и стоимость результата",
			status: `<span class="status status-gap">нет outcome</span>`,
			desc:   "Стоимость, wall time и число повторов на задачу.",
			answQ:  "Где дорогая работа приносит результат?",
			needQ:  "runs.jsonl + независимый task_outcome",
		},
		{
			title:  "Маршруты и критический путь",
			status: `<span class="status status-gap">не показан</span>`,
			desc:   "Переходы между ролями, циклы и вклад в общее время.",
			answQ:  "Где цепочка застревает?",
			needQ:  "parent_dispatch_id, seq, run boundary",
		},
		{
			title:  "До / после config_hash",
			status: driftStatus,
			desc:   "Class A, расход и latency по окнам конфигурации.",
			answQ:  "Стало ли лучше после правки контракта?",
			needQ:  "config_hash на каждом диспатче",
		},
		{
			title:  "Model routing и A/B",
			status: modelStatus,
			desc:   "Роль x requested model x resolved model x arm.",
			answQ:  "Какой tier достаточен роли?",
			needQ:  "закрыть unknown + experiment registry",
		},
		{
			title:  "Качество результата",
			status: `<span class="status status-gap">нет ground truth</span>`,
			desc:   "CI, human acceptance, reopen и rollback.",
			answQ:  "Выполнена ли задача по существу?",
			needQ:  "версионированный task_outcome",
		},
		{
			title:  "Здоровье телеметрии",
			status: `<span class="status status-ready">приоритет 0</span>`,
			desc:   "Потери по причинам: missing, truncated, parse drift.",
			answQ:  "Можно ли верить остальным отчётам?",
			needQ:  "первая строка каждого отчёта",
		},
	}

	fmt.Fprintln(b, `<div class="report-catalog">`)
	for _, c := range cards {
		fmt.Fprintln(b, `<article class="report-card">`)
		fmt.Fprintf(b, `<div class="report-card-top"><h3>%s</h3>%s</div>`, hesc(c.title), c.status)
		fmt.Fprintln(b)
		fmt.Fprintf(b, `<p>%s</p>`, hesc(c.desc))
		fmt.Fprintln(b)
		fmt.Fprintf(b, `<dl><dt>Отвечает</dt><dd>%s</dd><dt>Нужно</dt><dd>%s</dd></dl>`,
			hesc(c.answQ), hesc(c.needQ))
		fmt.Fprintln(b)
		fmt.Fprintln(b, `</article>`)
	}
	fmt.Fprintln(b, `</div>`)
	fmt.Fprintln(b, `</section>`)
}

func writeDefinitions(b *strings.Builder) {
	fmt.Fprintln(b, `<section class="section" id="definitions">`)
	fmt.Fprintln(b, `<header class="section-head">`)
	fmt.Fprintln(b, `<div><p class="section-label">Как читать отчёт</p><h2>Границы данных</h2></div>`)
	fmt.Fprintln(b, `</header>`)
	fmt.Fprintln(b, `<div class="definitions">`)
	fmt.Fprintln(b, `<p><strong>Tier 1 — детерминированный.</strong><br>Все числа вычислены из dispatch JSONL без вызова LLM. Ноль токенов потрачено на генерацию этой страницы.</p>`)
	fmt.Fprintln(b, `<p><strong>Не выдумано.</strong><br>Где данных нет (routes, outcomes, config windows), отчёт показывает отсутствие вместо синтетического графика.</p>`)
	fmt.Fprintln(b, `<p><strong>Следующий уровень.</strong><br>Decision report связывает каждый сигнал с evidence, владельцем и проверяемым следующим действием.</p>`)
	fmt.Fprintln(b, `</div>`)
	fmt.Fprintln(b, `</section>`)
}

// -----------------------------------------------------------------------------
// formatting helpers
// -----------------------------------------------------------------------------

func fmtTokens(n int) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.2fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func fmtDuration(ms int64) string {
	if ms <= 0 {
		return "0s"
	}
	sec := ms / 1000
	if sec >= 60 {
		m := sec / 60
		s := sec % 60
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", sec)
}

func fmtPct(n, total int) string {
	if total == 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", float64(n)*100/float64(total))
}

func hesc(s string) string { return html.EscapeString(s) }
