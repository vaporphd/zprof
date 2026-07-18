// Package eval parses Claude Code session JSONL transcripts into per-dispatch
// records, computes deterministic quality/cost metrics (Tier 1), and renders
// a human-readable summary. Tier 2 (LLM-judged panel scores) lives in the
// evaluator subagent — this package only produces the trace it consumes.
package eval

import "time"

// Dispatch is one Agent-tool invocation observed in a session transcript.
// The zero-value is invalid; use ParseSession to construct one.
type Dispatch struct {
	// ID is the tool_use identifier (toolu_...) — stable across the dispatch
	// message and its matching task-notification.
	ID string
	// AgentName is the human-readable label from the Agent tool call's
	// `description` input. It's what the user sees in progress views and the
	// primary grouping key when Description strings encode a role prefix
	// (e.g. "Reviewer — MoodJournalInterface" → role guess "reviewer").
	AgentName string
	// SubagentType is Claude Code's registered agent type ("general-purpose",
	// "Explore", "Plan", or a project agent name). For general-purpose runs
	// the effective role is inferred from AgentName.
	SubagentType string
	// Model is the model override passed to Agent (empty when inherited from
	// the main loop).
	Model string
	// Prompt is the raw prompt handed to the subagent — kept for the LLM
	// judge (Tier 2) to compare intent against outcome. Empty for aborted
	// dispatches where the tool_use had no input recorded.
	Prompt string
	// WorkingDir is the filesystem root the subagent worked from — parsed
	// from a "Working directory: <path>" hint in the dispatch prompt. Used
	// by the scorer to resolve relative artifact paths that would otherwise
	// stat against `zprof eval`'s own cwd (a different project) and miss.
	WorkingDir string
	// Timestamp is when the dispatch was issued (parent tool_use).
	Timestamp time.Time
	// DurationMs is the wall-clock spent in the subagent, from the
	// task-notification's <duration_ms>. Zero if not completed.
	DurationMs int64
	// SubagentTokens is the total output tokens the subagent spent, taken
	// verbatim from <subagent_tokens> in the task-notification. Zero for
	// aborted or interrupted dispatches.
	SubagentTokens int
	// ToolUses is the count of tool calls the subagent made, from
	// <tool_uses> in the task-notification.
	ToolUses int
	// Status is the task-notification <status>: "completed", "aborted", or
	// empty if the dispatch never returned in the transcript.
	Status string
	// Returned is the parsed return_format payload. Fields default to
	// empty strings when the subagent's response did not include them.
	Returned Return
	// Compliance holds deterministic contract-adherence checks.
	Compliance Compliance
}

// Return is the subagent's parsed return_format block. Optional fields
// (Confidence, Notes, Blocker, SelfCheck) are empty when the role's
// contract does not emit them yet — see profiles/*/agents/*.md.
type Return struct {
	Verdict    string
	Next       string
	Artifact   string
	OneLine    string
	Blocker    string
	Confidence float64
	Notes      string
	SelfCheck  []string
	// RawFirstLine is the first non-empty line of the subagent's response;
	// used by Compliance.HasPreamble to check whether the return started
	// with the return_format block or with narrative prose.
	RawFirstLine string
}

// Compliance flags contract-adherence patterns catchable without LLM help.
// Every field defaults to a permissive value — a Dispatch we could not
// check gets no false-positive flag.
type Compliance struct {
	// ArtifactExists is true when Returned.Artifact points to a file we
	// found on disk at eval time. False when the path was reported but the
	// file is missing (H0/C1-class silent failure). Nil pointer semantics
	// via the checked flag — if ArtifactChecked is false, the value is
	// meaningless.
	ArtifactExists  bool
	ArtifactChecked bool
	// HasPreamble is true when the subagent's return begins with narrative
	// prose before the first `verdict:` line — the H1 finding pattern.
	HasPreamble bool
	// NextIsReachable is true when Returned.Next names a role we can
	// dispatch (present in the applied overlays) or "null". False when the
	// route dead-ends the loop.
	NextIsReachable bool
}

// SessionMeta summarizes the whole session (not a per-dispatch view).
type SessionMeta struct {
	SessionID       string
	Path            string
	MainLoopModel   string
	MainLoopIn      int
	MainLoopOut     int
	CacheRead       int
	CacheCreate     int
	FirstTimestamp  time.Time
	LastTimestamp   time.Time
	TotalDurationMs int64
}

// Trace is what ParseSession returns: the session-level meta plus every
// dispatch discovered in chronological order.
type Trace struct {
	Session    SessionMeta
	Dispatches []Dispatch
}
