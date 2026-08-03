package stats

import (
	"strings"
	"testing"
	"time"
)

func testReport() *Report {
	t0 := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 8, 2, 18, 0, 0, 0, time.UTC)
	return &Report{
		ProjectID:       "test-project-id",
		MachineID:       "mac-01",
		Harness:         "zprof",
		TimeRange:       [2]time.Time{t0, t1},
		TotalDispatches: 1000,
		CompletedCount:  900,
		Sessions:        5,
		Losses: Losses{
			TotalLines:  1050,
			ParseErrors: 10,
			Incomplete:  40,
			MissingRole: 200,
		},
		Health: []RoleHealth{
			{
				Role:            "implementer",
				Dispatches:      400,
				Completed:       390,
				Failed:          10,
				PreambleCount:   35,
				PreambleChecked: 50,
				ParsedCount:     50,
				ParsedChecked:   50,
				ComplianceRate:  30,
			},
			{
				Role:            "reviewer",
				Dispatches:      200,
				Completed:       195,
				Failed:          5,
				PreambleCount:   10,
				PreambleChecked: 80,
				ParsedCount:     80,
				ParsedChecked:   80,
				ComplianceRate:  87.5,
			},
			{
				Role:       "tester",
				Dispatches: 100,
				Completed:  100,
			},
		},
		Economics: EconomicsReport{
			TotalTokens: TokenBreakdown{
				Input: 500_000_000, Output: 100_000_000,
				CacheRead: 5_000_000_000, CacheCreation: 50_000_000,
			},
			ByRole: []RoleEconomics{
				{
					Role:           "implementer",
					Dispatches:     400,
					Tokens:         TokenBreakdown{Input: 300_000_000, Output: 60_000_000, CacheRead: 3_000_000_000, CacheCreation: 30_000_000},
					AvgPerDispatch: 8_475_000,
					P50Duration:    600_000,
					P95Duration:    2_400_000,
				},
				{
					Role:           "reviewer",
					Dispatches:     200,
					Tokens:         TokenBreakdown{Input: 100_000_000, Output: 20_000_000, CacheRead: 1_000_000_000, CacheCreation: 10_000_000},
					AvgPerDispatch: 5_650_000,
					P50Duration:    300_000,
					P95Duration:    800_000,
				},
				{
					Role:           "tester",
					Dispatches:     100,
					Tokens:         TokenBreakdown{Input: 50_000_000, Output: 10_000_000, CacheRead: 500_000_000, CacheCreation: 5_000_000},
					AvgPerDispatch: 5_650_000,
					P50Duration:    240_000,
					P95Duration:    600_000,
				},
			},
			ByModel: []ModelEconomics{
				{Model: "claude-sonnet-4-20250514", Dispatches: 600, Tokens: TokenBreakdown{Input: 400_000_000, Output: 80_000_000}},
				{Model: "unknown", Dispatches: 100, Tokens: TokenBreakdown{Input: 50_000_000, Output: 10_000_000}},
			},
		},
		Drift: []DriftEntry{
			{ConfigHash: "abc123", Dispatches: 500, AvgTokens: 8_000_000, ComplianceRate: 80},
			{ConfigHash: "def456", Dispatches: 300, AvgTokens: 7_500_000, ComplianceRate: 95},
		},
	}
}

func TestRenderHTML_Doctype(t *testing.T) {
	r := testReport()
	out := RenderHTML(r)
	if !strings.HasPrefix(out, "<!DOCTYPE html>") {
		t.Error("output should start with <!DOCTYPE html>")
	}
}

func TestRenderHTML_ContainsProjectID(t *testing.T) {
	r := testReport()
	out := RenderHTML(r)
	if !strings.Contains(out, "test-project-id") {
		t.Error("output should contain the project ID")
	}
}

func TestRenderHTML_TrustSection(t *testing.T) {
	r := testReport()
	out := RenderHTML(r)
	if !strings.Contains(out, "Доверие к отчёту") {
		t.Error("output should contain trust section header")
	}
	// 20% missing role means 80% coverage; should say "Ограниченное"
	if !strings.Contains(out, "Ограниченное") {
		t.Error("should show limited trust for 80% coverage")
	}
	if !strings.Contains(out, "80.0%") {
		t.Error("should contain 80.0% coverage figure")
	}
}

func TestRenderHTML_TrustFull(t *testing.T) {
	r := testReport()
	r.Losses.MissingRole = 10 // 1% — should be "Полное"
	out := RenderHTML(r)
	if !strings.Contains(out, "Полное") {
		t.Error("should show full trust when coverage > 95%")
	}
}

func TestRenderHTML_TrustInsufficient(t *testing.T) {
	r := testReport()
	r.Losses.MissingRole = 500 // 50% — should be "Недостаточное"
	out := RenderHTML(r)
	if !strings.Contains(out, "Недостаточное") {
		t.Error("should show insufficient trust when coverage < 70%")
	}
}

func TestRenderHTML_ActionQueueP0(t *testing.T) {
	r := testReport()
	out := RenderHTML(r)
	// implementer has compliance 30% < 50% so P0
	if !strings.Contains(out, "P0") {
		t.Error("should contain P0 action for low compliance")
	}
	if !strings.Contains(out, "implementer") {
		t.Error("P0 action should mention the role with bad compliance")
	}
}

func TestRenderHTML_ActionQueueP1MissingRole(t *testing.T) {
	r := testReport()
	out := RenderHTML(r)
	// 200/1000 = 20% missing role > 10%
	if !strings.Contains(out, "P1") {
		t.Error("should contain P1 action for >10% missing role")
	}
	if !strings.Contains(out, "без роли") {
		t.Error("P1 action should mention missing role")
	}
}

func TestRenderHTML_ActionQueueP1TokenConcentration(t *testing.T) {
	r := testReport()
	out := RenderHTML(r)
	// implementer has 3390M / 5650M = ~60% > 40%
	if !strings.Contains(out, "концентрирует") {
		t.Error("should contain P1 action for token concentration > 40%")
	}
}

func TestRenderHTML_ActionQueueP2LongTail(t *testing.T) {
	r := testReport()
	out := RenderHTML(r)
	// implementer has p95/p50 = 2400000/600000 = 4x > 3
	if !strings.Contains(out, "Длинный хвост") {
		t.Error("should contain P2 action for long tail")
	}
}

func TestRenderHTML_NoActionsWhenClean(t *testing.T) {
	r := &Report{
		ProjectID:       "clean-project",
		TotalDispatches: 100,
		Sessions:        1,
		Losses:          Losses{MissingRole: 5},
		Economics: EconomicsReport{
			TotalTokens: TokenBreakdown{Input: 1000, Output: 500, CacheRead: 500},
			ByRole: []RoleEconomics{
				{Role: "alpha", Dispatches: 40, Tokens: TokenBreakdown{Input: 400, Output: 200}, P50Duration: 60000, P95Duration: 90000},
				{Role: "beta", Dispatches: 30, Tokens: TokenBreakdown{Input: 300, Output: 150, CacheRead: 250}, P50Duration: 50000, P95Duration: 80000},
				{Role: "gamma", Dispatches: 30, Tokens: TokenBreakdown{Input: 300, Output: 150, CacheRead: 250}, P50Duration: 50000, P95Duration: 80000},
			},
		},
	}
	out := RenderHTML(r)
	if !strings.Contains(out, "Нет критических сигналов") {
		t.Error("should show no-action message when everything is clean")
	}
}

func TestRenderHTML_DarkModeCSS(t *testing.T) {
	r := testReport()
	out := RenderHTML(r)
	if !strings.Contains(out, "prefers-color-scheme: dark") {
		t.Error("output should contain dark mode media query")
	}
}

func TestRenderHTML_NoExternalURLs(t *testing.T) {
	r := testReport()
	out := RenderHTML(r)
	for _, proto := range []string{"http://", "https://"} {
		if strings.Contains(out, proto) {
			t.Errorf("output should not contain external URLs but found %s", proto)
		}
	}
}

func TestRenderHTML_HTMLEntitiesEscaped(t *testing.T) {
	r := testReport()
	r.ProjectID = `<script>alert("xss")</script>`
	out := RenderHTML(r)
	if strings.Contains(out, `<script>`) {
		t.Error("HTML entities should be escaped; found raw <script> tag")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Error("should contain escaped script tag")
	}
}

func TestRenderHTML_EconomicsTable(t *testing.T) {
	r := testReport()
	out := RenderHTML(r)
	if !strings.Contains(out, "Экономика ролей") {
		t.Error("should contain economics section")
	}
	if !strings.Contains(out, "share-track") {
		t.Error("should contain share bar elements")
	}
}

func TestRenderHTML_ContractSection(t *testing.T) {
	r := testReport()
	out := RenderHTML(r)
	if !strings.Contains(out, "Здоровье возвратов") {
		t.Error("should contain contract health section")
	}
	if !strings.Contains(out, "FAIL") {
		t.Error("should show FAIL status for low compliance")
	}
}

func TestRenderHTML_ReportCatalog(t *testing.T) {
	r := testReport()
	out := RenderHTML(r)
	if !strings.Contains(out, "report-catalog") {
		t.Error("should contain report catalog")
	}
	if !strings.Contains(out, "можно строить") {
		t.Error("drift report should show 'можно строить' when >1 config_hash")
	}
}

func TestRenderHTML_VolumeStrip(t *testing.T) {
	r := testReport()
	out := RenderHTML(r)
	if !strings.Contains(out, "metric-strip") {
		t.Error("should contain metric strip")
	}
	if !strings.Contains(out, "диспатчей") {
		t.Error("should show dispatch count in metric strip")
	}
}

func TestRenderHTML_StickyNav(t *testing.T) {
	r := testReport()
	out := RenderHTML(r)
	if !strings.Contains(out, "report-nav") {
		t.Error("should contain sticky navigation")
	}
	if !strings.Contains(out, `href="#actions"`) {
		t.Error("nav should link to actions section")
	}
}

func TestRenderHTML_EmptyReport(t *testing.T) {
	r := &Report{}
	out := RenderHTML(r)
	if !strings.HasPrefix(out, "<!DOCTYPE html>") {
		t.Error("empty report should still produce valid HTML")
	}
	if !strings.Contains(out, "Нет данных") {
		t.Error("empty report should show no-data verdict")
	}
}

// -----------------------------------------------------------------------------
// formatting helpers
// -----------------------------------------------------------------------------

func TestFmtTokens(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1234, "1.2K"},
		{1_234_567, "1.23M"},
		{1_234_567_890, "1.23B"},
	}
	for _, tt := range tests {
		got := fmtTokens(tt.in)
		if got != tt.want {
			t.Errorf("fmtTokens(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFmtDuration(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0s"},
		{45_000, "45s"},
		{60_000, "1m0s"},
		{265_000, "4m25s"},
		{3_660_000, "61m0s"},
	}
	for _, tt := range tests {
		got := fmtDuration(tt.in)
		if got != tt.want {
			t.Errorf("fmtDuration(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFmtPct(t *testing.T) {
	tests := []struct {
		n, total int
		want     string
	}{
		{455, 1000, "45.5%"},
		{0, 1000, "0.0%"},
		{1000, 1000, "100.0%"},
		{0, 0, "0.0%"},
	}
	for _, tt := range tests {
		got := fmtPct(tt.n, tt.total)
		if got != tt.want {
			t.Errorf("fmtPct(%d, %d) = %q, want %q", tt.n, tt.total, got, tt.want)
		}
	}
}

func TestHesc(t *testing.T) {
	got := hesc(`<b>"hello"&</b>`)
	// html.EscapeString uses &#34; for double quotes
	want := `&lt;b&gt;&#34;hello&#34;&amp;&lt;/b&gt;`
	if got != want {
		t.Errorf("hesc(%q) = %q, want %q", `<b>"hello"&</b>`, got, want)
	}
}
