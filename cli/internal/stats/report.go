package stats

import "time"

type Report struct {
	ProjectName     string
	ProjectID       string
	MachineID       string
	Harness         string
	TimeRange       [2]time.Time
	TotalDispatches int
	CompletedCount  int
	Sessions        int
	Losses          Losses
	TelHealth       TelemetryHealth
	Health          []RoleHealth
	Economics       EconomicsReport
	Routes          RoutesReport
	Drift           []DriftEntry
}

type RoleHealth struct {
	Role            string
	Dispatches      int
	Completed       int
	Failed          int
	PreambleCount   int
	PreambleChecked int
	ParsedCount     int
	ParsedChecked   int
	ComplianceRate  float64
}

type EconomicsReport struct {
	TotalTokens TokenBreakdown
	ByRole      []RoleEconomics
	ByModel     []ModelEconomics
}

type TokenBreakdown struct {
	Input, Output, CacheRead, CacheCreation int
}

func (t TokenBreakdown) Total() int {
	return t.Input + t.Output + t.CacheRead + t.CacheCreation
}

type RoleEconomics struct {
	Role           string
	Dispatches     int
	Tokens         TokenBreakdown
	AvgPerDispatch int
	P50Duration    int64
	P95Duration    int64
}

type ModelEconomics struct {
	Model      string
	Dispatches int
	Tokens     TokenBreakdown
}

type RoutesReport struct {
	ByStatus    map[string]int
	TesterLoops []TesterLoop
	Transitions []Transition
}

type TesterLoop struct {
	SessionID string
	Rounds    int
}

type Transition struct {
	From  string
	To    string
	Count int
}

type DriftEntry struct {
	ConfigHash     string
	FirstSeen      time.Time
	LastSeen       time.Time
	Dispatches     int
	Tokens         TokenBreakdown
	AvgTokens      int
	ComplianceRate float64
	P50Duration    int64
	P95Duration    int64
}

type TelemetryHealth struct {
	TranscriptsCaptured int
	TranscriptsTotal    int
	TranscriptPct       float64
	Truncated           int
	UnknownRole         int
	UnknownModel        int
	ParseErrors         int
	AsyncIncomplete     int
	DailyDispatches     []DayCount
}

type DayCount struct {
	Date  string
	Count int
}
