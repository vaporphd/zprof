package score

import (
	"math"
	"sort"
	"strings"
)

const ScoreSchema = 1

type Tokens struct {
	Input         int `json:"input"`
	Output        int `json:"output"`
	CacheRead     int `json:"cache_read"`
	CacheCreation int `json:"cache_creation"`
}

func (t Tokens) Total() int { return t.Input + t.Output + t.CacheRead + t.CacheCreation }

type ToolCount struct {
	Tool  string `json:"tool"`
	Count int    `json:"count"`
}

type RoleRow struct {
	Role    string  `json:"role"`
	Tokens  int     `json:"tokens"`
	Calls   int     `json:"calls"`
	Errors  int     `json:"errors"`
	Penalty float64 `json:"penalty"`
	Model   string  `json:"model"`
}

type Facts struct {
	Tokens      Tokens            `json:"tokens"`
	Dispatches  int               `json:"dispatches"`
	ToolCalls   int               `json:"tool_calls"`
	ToolsTop    []ToolCount       `json:"tools_top"`
	DurationMs  int64             `json:"duration_ms"`
	Route       []string          `json:"route"`
	Models      map[string]string `json:"models"`
	ModelCounts map[string]int    `json:"model_counts"`
}

type Inputs struct {
	Dispatches         int      `json:"dispatches"`
	ToolEvents         int      `json:"tool_events"`
	TranscriptsMissing []string `json:"transcripts_missing"`
	Confidence         string   `json:"confidence"`
	// Legacy marks runs collected before schema v2: no root verdict, or
	// several dispatches without a single tool event. P1–P5 read as 0 on
	// such data, so the card is partial and --all-missing skips it.
	Legacy bool `json:"legacy"`
}

// Card is one row of .agentlog/scores.jsonl.
type Card struct {
	ScoreSchema  int       `json:"score_schema"`
	ZprofVersion string    `json:"zprof_version"`
	RunID        string    `json:"run_id"`
	RunLog       string    `json:"run_log,omitempty"`
	SessionID    string    `json:"session_id"`
	ProjectID    string    `json:"project_id"`
	TsUTC        string    `json:"ts_utc"`
	Verdict      string    `json:"verdict"`
	Tier         string    `json:"tier"`
	Score        int       `json:"score"`
	Penalties    []Penalty `json:"penalties"`
	Roles        []RoleRow `json:"roles"`
	Facts        Facts     `json:"facts"`
	Inputs       Inputs    `json:"inputs"`
	WeightsHash  string    `json:"weights_hash"`
}

// TierFor maps verdict + score onto the spec §6 tiers.
func TierFor(verdict string, score int, th Thresholds) string {
	switch {
	case verdict == "blocked":
		return "Blocked"
	case verdict == "failed":
		return "Failed"
	case verdict == "done" || verdict == "ok" || strings.HasPrefix(verdict, "approve"):
		if score >= th.Ideal {
			return "Ideal"
		}
		if score >= th.Solid {
			return "Solid"
		}
		return "Lucky"
	}
	return "Unknown"
}

// ShortModel: "claude-sonnet-5" → "sonnet", "claude-opus-5-5" → "opus"; unknown vendors pass through.
func ShortModel(model string) string {
	if model == "" {
		return "?"
	}
	parts := strings.Split(model, "-")
	if parts[0] == "claude" && len(parts) > 1 {
		return parts[1]
	}
	return model
}

// Compute scores one run under cfg.
func Compute(run Run, cfg Config, zprofVersion string) Card {
	checks := []func(Run, Config) metric{computeP1, computeP2, computeP3, computeP4, computeP5, computeP6, computeP7}
	penalties := make([]Penalty, 0, len(checks))
	sum := 0.0
	rolePenalty := map[string]float64{}
	for i, fn := range checks {
		p := penaltyFrom(PenaltyIDs[i], fn(run, cfg), cfg)
		penalties = append(penalties, p)
		sum += p.Points
		for r, v := range p.ByRole {
			rolePenalty[r] += v
		}
	}
	score := int(math.Round(100 - sum))
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	card := Card{
		ScoreSchema:  ScoreSchema,
		ZprofVersion: zprofVersion,
		RunID:        run.ID,
		RunLog:       run.RunLog,
		SessionID:    run.Root.SessionID,
		ProjectID:    run.Root.ProjectID,
		TsUTC:        run.Root.TsUTC,
		Verdict:      run.Root.Verdict,
		Score:        score,
		Tier:         TierFor(run.Root.Verdict, score, cfg.Thresholds),
		Penalties:    penalties,
		WeightsHash:  cfg.WeightsHash(),
	}
	card.Facts, card.Roles = facts(run, rolePenalty)
	card.Inputs = inputs(run)
	return card
}

func facts(run Run, rolePenalty map[string]float64) (Facts, []RoleRow) {
	f := Facts{Models: map[string]string{}, ModelCounts: map[string]int{}}
	rows := map[string]*RoleRow{}
	toolCounts := map[string]int{}
	var childDuration int64

	for _, d := range run.Dispatches {
		f.Tokens.Input += d.TokensInput
		f.Tokens.Output += d.TokensOutput
		f.Tokens.CacheRead += d.TokensCacheRead
		f.Tokens.CacheCreation += d.TokensCacheCreation
		if d.ModelResolved != "" {
			f.ModelCounts[ShortModel(d.ModelResolved)]++
		}
		if d.DispatchID != run.ID {
			f.Dispatches++
			childDuration += d.DurationMs
			if d.ModelResolved != "" {
				f.Models[d.Role] = d.ModelResolved
			}
		}
		row := rows[d.Role]
		if row == nil {
			row = &RoleRow{Role: d.Role}
			rows[d.Role] = row
		}
		row.Tokens += tokens(d)
		if d.ModelResolved != "" {
			row.Model = ShortModel(d.ModelResolved)
		}
		for _, e := range run.Events[d.DispatchID] {
			f.ToolCalls++
			toolCounts[e.Tool]++
			row.Calls++
			if isErr(e) {
				row.Errors++
			}
		}
	}
	for _, s := range run.Steps {
		f.Route = append(f.Route, s.Role)
	}
	f.DurationMs = run.Root.DurationMs
	if f.DurationMs == 0 {
		f.DurationMs = childDuration
	}
	for tool, n := range toolCounts {
		f.ToolsTop = append(f.ToolsTop, ToolCount{tool, n})
	}
	sort.Slice(f.ToolsTop, func(i, j int) bool {
		if f.ToolsTop[i].Count != f.ToolsTop[j].Count {
			return f.ToolsTop[i].Count > f.ToolsTop[j].Count
		}
		return f.ToolsTop[i].Tool < f.ToolsTop[j].Tool
	})
	if len(f.ToolsTop) > 5 {
		f.ToolsTop = f.ToolsTop[:5]
	}

	out := make([]RoleRow, 0, len(rows))
	for role, row := range rows {
		row.Penalty = rolePenalty[role]
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Penalty != out[j].Penalty {
			return out[i].Penalty > out[j].Penalty
		}
		if out[i].Tokens != out[j].Tokens {
			return out[i].Tokens > out[j].Tokens
		}
		return out[i].Role < out[j].Role
	})
	return f, out
}

func inputs(run Run) Inputs {
	in := Inputs{Dispatches: len(run.Dispatches), Confidence: "full", TranscriptsMissing: []string{}}
	for _, evs := range run.Events {
		in.ToolEvents += len(evs)
	}
	seen := map[string]bool{}
	for _, d := range run.Dispatches {
		truncated := d.TranscriptTruncated != nil && *d.TranscriptTruncated
		if (!d.TranscriptCaptured || truncated) && !seen[d.Role] {
			seen[d.Role] = true
			in.TranscriptsMissing = append(in.TranscriptsMissing, d.Role)
		}
	}
	sort.Strings(in.TranscriptsMissing)
	in.Legacy = (in.ToolEvents == 0 && len(run.Dispatches) > 1) || run.Root.Verdict == ""
	if len(in.TranscriptsMissing) > 0 || in.Legacy {
		in.Confidence = "partial"
	}
	return in
}
