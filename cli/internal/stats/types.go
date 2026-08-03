package stats

import "time"

type Dispatch struct {
	SchemaVersion       int            `json:"schema_version"`
	Harness             string         `json:"harness"`
	HarnessVersion      string         `json:"harness_version"`
	MachineID           string         `json:"machine_id"`
	ProjectID           string         `json:"project_id"`
	TsUTC               string         `json:"ts_utc"`
	SessionID           string         `json:"session_id"`
	DispatchID          string         `json:"dispatch_id"`
	Seq                 int            `json:"seq"`
	ParentDispatchID    string         `json:"parent_dispatch_id,omitempty"`
	SpawnDepth          int            `json:"spawn_depth"`
	Role                string         `json:"role"`
	Overlay             string         `json:"overlay,omitempty"`
	ConfigHash          string         `json:"config_hash,omitempty"`
	ModelRequested      string         `json:"model_requested,omitempty"`
	ModelResolved       string         `json:"model_resolved,omitempty"`
	Verdict             string         `json:"verdict,omitempty"`
	Status              string         `json:"status"`
	DispatchComplete    bool           `json:"dispatch_complete"`
	TokensInput         int            `json:"tokens_input"`
	TokensOutput        int            `json:"tokens_output"`
	TokensCacheRead     int            `json:"tokens_cache_read"`
	TokensCacheCreation int            `json:"tokens_cache_creation"`
	ToolUses            int            `json:"tool_uses"`
	DurationMs          int64          `json:"duration_ms"`
	ArtifactExists      *bool          `json:"artifact_exists,omitempty"`
	HasPreamble         *bool          `json:"has_preamble,omitempty"`
	NextIsReachable     *bool          `json:"next_is_reachable,omitempty"`
	ReturnParsed        *bool          `json:"return_parsed,omitempty"`
	TranscriptRef       string         `json:"transcript_ref,omitempty"`
	TranscriptCaptured  bool           `json:"transcript_captured"`
	TranscriptTruncated *bool          `json:"transcript_truncated,omitempty"`
	Ext                 map[string]any `json:"ext,omitempty"`
	Timestamp           time.Time      `json:"-"`
}

type Losses struct {
	TotalLines  int
	ParseErrors int
	Incomplete  int
	MissingRole int
}
