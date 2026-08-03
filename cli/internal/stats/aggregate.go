package stats

import (
	"sort"
)

func Aggregate(dispatches []Dispatch, losses Losses) *Report {
	r := &Report{Losses: losses, TotalDispatches: len(dispatches)}

	sessions := map[string]bool{}
	var completed []Dispatch
	for _, d := range dispatches {
		sessions[d.SessionID] = true
		if r.ProjectID == "" && d.ProjectID != "" {
			r.ProjectID = d.ProjectID
		}
		if r.MachineID == "" && d.MachineID != "" {
			r.MachineID = d.MachineID
		}
		if r.Harness == "" && d.Harness != "" {
			r.Harness = d.Harness
		}
		if !d.Timestamp.IsZero() {
			if r.TimeRange[0].IsZero() || d.Timestamp.Before(r.TimeRange[0]) {
				r.TimeRange[0] = d.Timestamp
			}
			if d.Timestamp.After(r.TimeRange[1]) {
				r.TimeRange[1] = d.Timestamp
			}
		}
		if d.DispatchComplete {
			completed = append(completed, d)
		}
	}
	r.Sessions = len(sessions)
	r.CompletedCount = len(completed)

	r.Health = aggregateHealth(completed)
	r.Economics = aggregateEconomics(completed)
	r.Routes = aggregateRoutes(completed, dispatches)
	r.Drift = aggregateDrift(completed)
	return r
}

func aggregateHealth(completed []Dispatch) []RoleHealth {
	m := map[string]*RoleHealth{}
	for _, d := range completed {
		role := d.Role
		if role == "" {
			role = "unknown"
		}
		h, ok := m[role]
		if !ok {
			h = &RoleHealth{Role: role}
			m[role] = h
		}
		h.Dispatches++
		if d.Status == "completed" {
			h.Completed++
		}
		if d.Status == "failed" || d.Status == "killed" {
			h.Failed++
		}
		if d.HasPreamble != nil {
			h.PreambleChecked++
			if *d.HasPreamble {
				h.PreambleCount++
			}
		}
		if d.ReturnParsed != nil {
			h.ParsedChecked++
			if *d.ReturnParsed {
				h.ParsedCount++
			}
		}
	}

	var out []RoleHealth
	for _, h := range m {
		if h.PreambleChecked > 0 {
			clean := h.PreambleChecked - h.PreambleCount
			parsed := h.ParsedCount
			total := h.PreambleChecked
			h.ComplianceRate = float64(min(clean, parsed)) / float64(total) * 100
		}
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dispatches > out[j].Dispatches })
	return out
}

func aggregateEconomics(completed []Dispatch) EconomicsReport {
	var e EconomicsReport

	roleM := map[string]*RoleEconomics{}
	roleDur := map[string][]int64{}
	modelM := map[string]*ModelEconomics{}

	for _, d := range completed {
		tb := TokenBreakdown{
			Input: d.TokensInput, Output: d.TokensOutput,
			CacheRead: d.TokensCacheRead, CacheCreation: d.TokensCacheCreation,
		}
		e.TotalTokens.Input += tb.Input
		e.TotalTokens.Output += tb.Output
		e.TotalTokens.CacheRead += tb.CacheRead
		e.TotalTokens.CacheCreation += tb.CacheCreation

		role := d.Role
		if role == "" {
			role = "unknown"
		}
		re, ok := roleM[role]
		if !ok {
			re = &RoleEconomics{Role: role}
			roleM[role] = re
		}
		re.Dispatches++
		re.Tokens.Input += tb.Input
		re.Tokens.Output += tb.Output
		re.Tokens.CacheRead += tb.CacheRead
		re.Tokens.CacheCreation += tb.CacheCreation
		if d.DurationMs > 0 {
			roleDur[role] = append(roleDur[role], d.DurationMs)
		}

		model := d.ModelResolved
		if model == "" {
			model = "unknown"
		}
		me, ok := modelM[model]
		if !ok {
			me = &ModelEconomics{Model: model}
			modelM[model] = me
		}
		me.Dispatches++
		me.Tokens.Input += tb.Input
		me.Tokens.Output += tb.Output
		me.Tokens.CacheRead += tb.CacheRead
		me.Tokens.CacheCreation += tb.CacheCreation
	}

	for role, re := range roleM {
		if re.Dispatches > 0 {
			re.AvgPerDispatch = re.Tokens.Total() / re.Dispatches
		}
		if durations, ok := roleDur[role]; ok && len(durations) > 0 {
			sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
			re.P50Duration = percentile(durations, 0.50)
			re.P95Duration = percentile(durations, 0.95)
		}
		e.ByRole = append(e.ByRole, *re)
	}
	sort.Slice(e.ByRole, func(i, j int) bool { return e.ByRole[i].Tokens.Total() > e.ByRole[j].Tokens.Total() })

	for _, me := range modelM {
		e.ByModel = append(e.ByModel, *me)
	}
	sort.Slice(e.ByModel, func(i, j int) bool { return e.ByModel[i].Dispatches > e.ByModel[j].Dispatches })

	return e
}

func aggregateRoutes(completed, all []Dispatch) RoutesReport {
	rr := RoutesReport{ByStatus: map[string]int{}}
	for _, d := range all {
		if d.Status != "" {
			rr.ByStatus[d.Status]++
		}
	}

	sessionTester := map[string]int{}
	for _, d := range completed {
		if d.Role == "integration-tester" || d.Role == "tester" {
			sessionTester[d.SessionID]++
		}
	}
	for sid, n := range sessionTester {
		if n >= 3 {
			rr.TesterLoops = append(rr.TesterLoops, TesterLoop{SessionID: sid, Rounds: n})
		}
	}
	sort.Slice(rr.TesterLoops, func(i, j int) bool { return rr.TesterLoops[i].Rounds > rr.TesterLoops[j].Rounds })
	return rr
}

func aggregateDrift(completed []Dispatch) []DriftEntry {
	type acc struct {
		count     int
		totalTok  int
		compliant int
		checked   int
	}
	m := map[string]*acc{}
	for _, d := range completed {
		h := d.ConfigHash
		if h == "" {
			continue
		}
		a, ok := m[h]
		if !ok {
			a = &acc{}
			m[h] = a
		}
		a.count++
		a.totalTok += d.TokensInput + d.TokensOutput + d.TokensCacheRead + d.TokensCacheCreation
		if d.HasPreamble != nil {
			a.checked++
			if !*d.HasPreamble {
				a.compliant++
			}
		}
	}
	if len(m) <= 1 {
		return nil
	}
	var out []DriftEntry
	for h, a := range m {
		de := DriftEntry{ConfigHash: h, Dispatches: a.count}
		if a.count > 0 {
			de.AvgTokens = a.totalTok / a.count
		}
		if a.checked > 0 {
			de.ComplianceRate = float64(a.compliant) / float64(a.checked) * 100
		}
		out = append(out, de)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dispatches > out[j].Dispatches })
	return out
}

func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}
