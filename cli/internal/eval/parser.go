package eval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ParseSession reads a Claude Code session JSONL from path and returns the
// dispatches + session meta it contains. Missing task-notifications are
// tolerated — the Dispatch is emitted with Status="" so the eval report can
// flag it as "dispatched but never returned".
func ParseSession(path string) (*Trace, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open session log: %w", err)
	}
	defer f.Close()
	return parseSessionReader(f, path)
}

// parseSessionReader is the io.Reader-taking half of ParseSession — split
// out for unit tests that want to feed fixture bytes without touching disk.
func parseSessionReader(r io.Reader, path string) (*Trace, error) {
	sc := bufio.NewScanner(r)
	// Session JSONL lines can be very long (multi-KB prompts, embedded
	// diffs). Grow the buffer past bufio's 64KB default.
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)

	dispatches := map[string]*Dispatch{}
	// Preserve first-observed order so the output is chronological even if
	// the JSONL isn't strictly time-sorted.
	var order []string

	meta := SessionMeta{Path: path}

	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var head struct {
			Type      string `json:"type"`
			SessionID string `json:"sessionId"`
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal(line, &head); err != nil {
			continue // tolerate malformed lines (empty, partial)
		}
		if meta.SessionID == "" {
			meta.SessionID = head.SessionID
		}
		ts, _ := time.Parse(time.RFC3339Nano, head.Timestamp)
		if !ts.IsZero() {
			if meta.FirstTimestamp.IsZero() || ts.Before(meta.FirstTimestamp) {
				meta.FirstTimestamp = ts
			}
			if ts.After(meta.LastTimestamp) {
				meta.LastTimestamp = ts
			}
		}

		switch head.Type {
		case "assistant":
			processAssistant(line, ts, &meta, dispatches, &order)
		case "queue-operation":
			processQueueOp(line, dispatches)
		case "user":
			// Newer session format: Agent returns arrive as `user`
			// messages carrying a `tool_result` content block plus a
			// top-level `toolUseResult` with typed usage — no
			// task-notification wrapper on the queue path.
			processUserToolResult(line, dispatches)
		case "system":
			processSystem(line, &meta)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan session log: %w", err)
	}

	// Order dispatches by their tool_use timestamp, falling back to insertion
	// order for those without a parsed timestamp.
	sort.SliceStable(order, func(i, j int) bool {
		return dispatches[order[i]].Timestamp.Before(dispatches[order[j]].Timestamp)
	})
	out := make([]Dispatch, 0, len(order))
	for _, id := range order {
		out = append(out, *dispatches[id])
	}
	return &Trace{Session: meta, Dispatches: out}, nil
}

// -----------------------------------------------------------------------------

func processAssistant(line []byte, ts time.Time, meta *SessionMeta, dispatches map[string]*Dispatch, order *[]string) {
	var env struct {
		Message struct {
			Model   string          `json:"model"`
			Usage   json.RawMessage `json:"usage"`
			Content []struct {
				Type  string          `json:"type"`
				Name  string          `json:"name"`
				ID    string          `json:"id"`
				Input json.RawMessage `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &env); err != nil {
		return
	}
	// Attribute main-loop tokens once per assistant turn.
	if meta.MainLoopModel == "" {
		meta.MainLoopModel = env.Message.Model
	}
	if len(env.Message.Usage) > 0 {
		var u struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		}
		if err := json.Unmarshal(env.Message.Usage, &u); err == nil {
			meta.MainLoopIn += u.InputTokens
			meta.MainLoopOut += u.OutputTokens
			meta.CacheRead += u.CacheReadInputTokens
			meta.CacheCreate += u.CacheCreationInputTokens
		}
	}
	for _, c := range env.Message.Content {
		if c.Type != "tool_use" || c.Name != "Agent" {
			continue
		}
		var in struct {
			Description  string `json:"description"`
			SubagentType string `json:"subagent_type"`
			Model        string `json:"model"`
			Prompt       string `json:"prompt"`
		}
		_ = json.Unmarshal(c.Input, &in)
		if _, ok := dispatches[c.ID]; ok {
			continue
		}
		dispatches[c.ID] = &Dispatch{
			ID:           c.ID,
			AgentName:    in.Description,
			SubagentType: in.SubagentType,
			Model:        in.Model,
			Prompt:       in.Prompt,
			Timestamp:    ts,
		}
		*order = append(*order, c.ID)
	}
}

// notifRe splits a queue-operation payload's <task-notification> block.
// The fields we consume live inside three predictable tags — everything
// else in the notification is ignored.
var (
	notifRe          = regexp.MustCompile(`(?s)<task-notification>(.*?)</task-notification>`)
	toolUseRe        = regexp.MustCompile(`<tool-use-id>(.*?)</tool-use-id>`)
	statusRe         = regexp.MustCompile(`<status>(.*?)</status>`)
	subagentTokensRe = regexp.MustCompile(`<subagent_tokens>(\d+)</subagent_tokens>`)
	toolUsesRe       = regexp.MustCompile(`<tool_uses>(\d+)</tool_uses>`)
	durationRe       = regexp.MustCompile(`<duration_ms>(\d+)</duration_ms>`)
	resultRe         = regexp.MustCompile(`(?s)<result>(.*?)</result>`)
)

func processQueueOp(line []byte, dispatches map[string]*Dispatch) {
	var env struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(line, &env); err != nil {
		return
	}
	m := notifRe.FindStringSubmatch(env.Content)
	if len(m) < 2 {
		return
	}
	body := m[1]
	tu := toolUseRe.FindStringSubmatch(body)
	if len(tu) < 2 {
		return
	}
	d, ok := dispatches[tu[1]]
	if !ok {
		return
	}
	if s := statusRe.FindStringSubmatch(body); len(s) > 1 {
		d.Status = s[1]
	}
	if s := subagentTokensRe.FindStringSubmatch(body); len(s) > 1 {
		n, _ := strconv.Atoi(s[1])
		d.SubagentTokens = n
	}
	if s := toolUsesRe.FindStringSubmatch(body); len(s) > 1 {
		n, _ := strconv.Atoi(s[1])
		d.ToolUses = n
	}
	if s := durationRe.FindStringSubmatch(body); len(s) > 1 {
		n, _ := strconv.ParseInt(s[1], 10, 64)
		d.DurationMs = n
	}
	if s := resultRe.FindStringSubmatch(body); len(s) > 1 {
		d.Returned = parseReturnFormat(s[1])
	}
}

// processUserToolResult handles the newer Agent return shape: a `user`
// message with a `tool_result` content block that carries the subagent's
// verdict text, plus a top-level `toolUseResult` object with typed usage
// (status, totalTokens, totalToolUseCount, totalDurationMs, agentType,
// resolvedModel). This replaces the older queue-operation notification
// path — both formats coexist across sessions, so we keep both readers.
func processUserToolResult(line []byte, dispatches map[string]*Dispatch) {
	var env struct {
		Message struct {
			Content []struct {
				Type       string `json:"type"`
				ToolUseID  string `json:"tool_use_id"`
				IsError    bool   `json:"is_error"`
				Content    []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"content"`
		} `json:"message"`
		ToolUseResult struct {
			Status             string `json:"status"`
			AgentType          string `json:"agentType"`
			ResolvedModel      string `json:"resolvedModel"`
			TotalDurationMs    int64  `json:"totalDurationMs"`
			TotalTokens        int    `json:"totalTokens"`
			TotalToolUseCount  int    `json:"totalToolUseCount"`
		} `json:"toolUseResult"`
	}
	if err := json.Unmarshal(line, &env); err != nil {
		return
	}
	for _, c := range env.Message.Content {
		if c.Type != "tool_result" || c.ToolUseID == "" {
			continue
		}
		d, ok := dispatches[c.ToolUseID]
		if !ok {
			continue
		}
		if env.ToolUseResult.Status != "" {
			d.Status = env.ToolUseResult.Status
		}
		if env.ToolUseResult.TotalTokens > 0 {
			d.SubagentTokens = env.ToolUseResult.TotalTokens
		}
		if env.ToolUseResult.TotalToolUseCount > 0 {
			d.ToolUses = env.ToolUseResult.TotalToolUseCount
		}
		if env.ToolUseResult.TotalDurationMs > 0 {
			d.DurationMs = env.ToolUseResult.TotalDurationMs
		}
		if d.Model == "" && env.ToolUseResult.ResolvedModel != "" {
			d.Model = env.ToolUseResult.ResolvedModel
		}
		// The subagent's return text is the FIRST text block of the
		// tool_result. Trailing blocks carry harness bookkeeping (agentId
		// hint, `<usage>` plaintext) — parseReturnFormat wants only the
		// verdict-and-schema payload.
		for _, x := range c.Content {
			if x.Type == "text" && strings.TrimSpace(x.Text) != "" {
				d.Returned = parseReturnFormat(x.Text)
				break
			}
		}
	}
}

func processSystem(line []byte, meta *SessionMeta) {
	var env struct {
		Subtype    string `json:"subtype"`
		DurationMs int64  `json:"durationMs"`
	}
	if err := json.Unmarshal(line, &env); err != nil {
		return
	}
	if env.Subtype == "turn_duration" {
		meta.TotalDurationMs += env.DurationMs
	}
}

// parseReturnFormat extracts the well-known fields from a subagent's
// return_format text. It is deliberately permissive — the format is
// YAML-flavored key/value pairs but subagents sometimes wrap the block
// in a fenced code block, add trailing commentary, or use fold indicators
// on multi-line values. We take the first line matching each known key.
func parseReturnFormat(raw string) Return {
	r := Return{}
	// Strip a leading ```yaml or ``` fence if the subagent wrapped its return.
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "```yaml")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)

	// Capture the very first non-empty line — HasPreamble uses this to
	// distinguish narrative prose from a schema field.
	for _, ln := range strings.Split(trimmed, "\n") {
		if s := strings.TrimSpace(ln); s != "" {
			r.RawFirstLine = s
			break
		}
	}

	// Field extractor: reads the leading `key: value` on any line whose key
	// matches one of the known fields. Multi-line values (like `notes: |`
	// blocks or continuation lines) are captured by peeking forward until
	// the next known key.
	lines := strings.Split(trimmed, "\n")
	known := map[string]*string{
		"verdict":  &r.Verdict,
		"artifact": &r.Artifact,
		"next":     &r.Next,
		"one_line": &r.OneLine,
		"blocker":  &r.Blocker,
		"notes":    &r.Notes,
	}
	for i, ln := range lines {
		key, val, ok := splitYAMLPair(ln)
		if !ok {
			continue
		}
		if key == "confidence" {
			// Confidence is numeric; tolerate "0.7" and "0.7 " forms.
			c, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
			if err == nil {
				r.Confidence = c
			}
			continue
		}
		if key == "self_check" {
			// self_check may be a YAML flow list `[a, b, c]` or a block list.
			r.SelfCheck = parseSelfCheck(lines[i:])
			continue
		}
		p, ok := known[key]
		if !ok {
			continue
		}
		*p = strings.TrimSpace(val)
	}
	return r
}

// splitYAMLPair returns key, value, true when line looks like `key: value`
// with an unquoted top-level key. It rejects indented lines (which are
// continuations of a preceding value) and lines whose colon lives inside
// a URL/timestamp/prose fragment.
func splitYAMLPair(ln string) (string, string, bool) {
	if len(ln) == 0 || ln[0] == ' ' || ln[0] == '\t' || ln[0] == '-' {
		return "", "", false
	}
	i := strings.Index(ln, ":")
	if i < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(ln[:i])
	if key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	val := ""
	if i+1 < len(ln) {
		val = ln[i+1:]
	}
	return key, val, true
}

// parseSelfCheck accepts YAML flow syntax `self_check: [foo, bar]` and
// block syntax `self_check:\n  - foo\n  - bar` and returns the items.
func parseSelfCheck(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	first := lines[0]
	i := strings.Index(first, ":")
	tail := strings.TrimSpace(first[i+1:])
	if strings.HasPrefix(tail, "[") && strings.HasSuffix(tail, "]") {
		inner := strings.TrimSuffix(strings.TrimPrefix(tail, "["), "]")
		parts := strings.Split(inner, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			s := strings.TrimSpace(p)
			s = strings.Trim(s, `"'`)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	var out []string
	for _, ln := range lines[1:] {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "- ") {
			out = append(out, strings.TrimSpace(t[2:]))
			continue
		}
		if _, _, ok := splitYAMLPair(ln); ok {
			break
		}
	}
	return out
}
