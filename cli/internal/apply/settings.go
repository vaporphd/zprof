package apply

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vaporphd/zprof/internal/fsutil"
)

// collectorHookTemplate is the command every telemetry hook runs. The
// guard (`test -x … &&`) matters because "any error -> exit 0" (design
// §6.1) covers the collector script itself, but a missing script fails in
// the shell *before* the script ever runs — without the guard, a project
// that hasn't run `zprof apply` yet (no zprof-collect.py) would fail the
// hook. Named for what it invokes (zprof-collect.py), not for the
// unrelated zprof-guard.py feature (ADR 0009) — kept distinct from
// guardHookTemplate below.
const collectorHookTemplate = `test -x "$CLAUDE_PROJECT_DIR/.claude/zprof-collect.py" && "$CLAUDE_PROJECT_DIR/.claude/zprof-collect.py" %s || true`

// guardHookTemplate is the command the two guard hooks run (ADR 0009,
// design §8.4) — same fail-open shape as collectorHookTemplate, guarding
// against a project that hasn't deployed zprof-guard.py yet.
const guardHookTemplate = `test -x "$CLAUDE_PROJECT_DIR/.claude/zprof-guard.py" && "$CLAUDE_PROJECT_DIR/.claude/zprof-guard.py" %s || true`

// scoreHookCommand runs the per-task scorecard after the collector. It is
// chained into the same Stop command because Claude Code runs an event's
// hooks in parallel, and `zprof score --no-collect` must see the collector's
// output. `command -v zprof` keeps projects without the binary silent.
const scoreHookCommand = `command -v zprof >/dev/null 2>&1 && cd "$CLAUDE_PROJECT_DIR" && zprof score --latest --quiet --no-collect || true`

type hookSpec struct {
	event   string
	matcher string
	command string
}

// telemetryHooks lists, in install order, the command each hook event runs.
var telemetryHooks = []hookSpec{
	{event: "SubagentStop", command: fmt.Sprintf(collectorHookTemplate, "subagent-stop")},
	{event: "Stop", command: fmt.Sprintf(collectorHookTemplate, "stop") + "; " + scoreHookCommand},
	{event: "SessionStart", command: fmt.Sprintf(collectorHookTemplate, "session-start")},
}

// guardHooks are the two guard hooks (ADR 0009, design §8.4). PreToolUse
// carries a matcher so the hook only fires for tool calls the guard cares
// about (TOOLS_GUARDED in zprof-guard.py); SubagentStop has none, matching
// the collector's SubagentStop hook.
var guardHooks = []hookSpec{
	{event: "PreToolUse", matcher: "Bash|Edit|Write|MultiEdit|NotebookEdit", command: fmt.Sprintf(guardHookTemplate, "pre-tool")},
	{event: "SubagentStop", command: fmt.Sprintf(guardHookTemplate, "subagent-stop")},
}

// settingsPath returns <projectDir>/.claude/settings.local.json.
func settingsPath(projectDir string) string {
	return filepath.Join(projectDir, ".claude", "settings.local.json")
}

// readSettings reads and parses settings.local.json, returning an empty
// map if the file doesn't exist yet or is empty.
func readSettings(path string) (map[string]any, error) {
	settings := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &settings); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	return settings, nil
}

// writeSettings marshals and atomically writes settings back to path,
// creating its parent directory if needed.
func writeSettings(path string, settings map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := fsutil.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// EnsureHooks idempotently upserts the three telemetry hooks (SubagentStop,
// Stop, SessionStart) into <projectDir>/.claude/settings.local.json,
// preserving any existing hooks and non-hook keys. Settings.local.json
// rather than settings.json: the latter is typically committed, so writing
// there would fire the hook on a teammate's machine that never ran
// `zprof apply` and has no collector script (design §4.1).
//
// The Stop command chains `zprof score --latest --quiet --no-collect` after
// the collector guard (one hook entry, not two — see scoreHookCommand); a
// stale zprof entry (older command shape, e.g. missing the score chain) is
// upgraded in place rather than duplicated.
func EnsureHooks(projectDir string) error {
	path := settingsPath(projectDir)
	settings, err := readSettings(path)
	if err != nil {
		return err
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for _, spec := range telemetryHooks {
		upsertHook(hooks, spec, "zprof-collect.py")
	}
	settings["hooks"] = hooks

	return writeSettings(path, settings)
}

// ensureGuardSettings performs one read/modify/write of
// settings.local.json: when enabled, upserts the two guard hooks and adds
// doc.PermissionsDeny (deduped) to permissions.deny; when disabled, removes
// both guard hooks and subtracts deny from permissions.deny — all against
// the currently-computed merge, not whatever is on disk (ADR 0009 I4/I8).
// Foreign hook entries and foreign deny values are never touched either
// way.
func ensureGuardSettings(projectDir string, enabled bool, deny []string) error {
	path := settingsPath(projectDir)
	settings, err := readSettings(path)
	if err != nil {
		return err
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	if enabled {
		for _, spec := range guardHooks {
			upsertHook(hooks, spec, "zprof-guard.py")
		}
	} else {
		for _, spec := range guardHooks {
			removeHook(hooks, spec.event, "zprof-guard.py")
		}
	}
	settings["hooks"] = hooks

	if enabled {
		if err := addPermissionsDeny(settings, deny); err != nil {
			return err
		}
	} else {
		if err := removePermissionsDeny(settings, deny); err != nil {
			return err
		}
	}

	return writeSettings(path, settings)
}

// upsertHook inserts spec's entry into hooks[spec.event], or replaces the
// existing entry that invokes script in place if its command or matcher
// changed — never appending a duplicate.
func upsertHook(hooks map[string]any, spec hookSpec, script string) {
	existing, _ := hooks[spec.event].([]any)
	if idx := zprofHookIndex(existing, script); idx >= 0 {
		if hookCommand(existing[idx]) != spec.command || hookMatcher(existing[idx]) != spec.matcher {
			existing[idx] = hookEntry(spec)
			hooks[spec.event] = existing
		}
		return
	}
	hooks[spec.event] = append(existing, hookEntry(spec))
}

// removeHook deletes the entry in hooks[event] that invokes script, if
// any. An event left with no entries is removed from hooks entirely (ADR
// 0009 I4: "пустые контейнеры при снятии... удаляется из hooks").
func removeHook(hooks map[string]any, event, script string) {
	existing, _ := hooks[event].([]any)
	idx := zprofHookIndex(existing, script)
	if idx < 0 {
		return
	}
	existing = append(existing[:idx], existing[idx+1:]...)
	if len(existing) == 0 {
		delete(hooks, event)
		return
	}
	hooks[event] = existing
}

// hookEntry renders spec as a settings.local.json hook entry: `"matcher"`
// is included only when non-empty, so collector entries (empty matcher)
// keep their existing JSON shape byte-for-byte.
func hookEntry(spec hookSpec) map[string]any {
	entry := map[string]any{
		"hooks": []any{
			map[string]any{"type": "command", "command": spec.command},
		},
	}
	if spec.matcher != "" {
		entry["matcher"] = spec.matcher
	}
	return entry
}

// zprofHookIndex returns the position of the entry that invokes script
// (matched as a substring of the entry's JSON, e.g. "zprof-collect.py" or
// "zprof-guard.py"), or -1. Used both to skip duplicates and to upgrade a
// stale command/matcher in place; the substring match on script (rather
// than a hardcoded "zprof-collect.py") is what lets the collector and
// guard entries on the same event (SubagentStop) upgrade independently
// without mistaking one for the other.
func zprofHookIndex(entries []any, script string) int {
	for i, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), script) {
			return i
		}
	}
	return -1
}

// hookCommand extracts the first command string of a hook entry ("" if malformed).
func hookCommand(entry any) string {
	m, _ := entry.(map[string]any)
	hs, _ := m["hooks"].([]any)
	if len(hs) == 0 {
		return ""
	}
	h, _ := hs[0].(map[string]any)
	s, _ := h["command"].(string)
	return s
}

// hookMatcher extracts a hook entry's matcher ("" if absent or malformed —
// indistinguishable from "no matcher set", which is the correct comparison
// value for upsertHook).
func hookMatcher(entry any) string {
	m, _ := entry.(map[string]any)
	s, _ := m["matcher"].(string)
	return s
}

// addPermissionsDeny upserts deny (deduped against what's already there)
// into settings["permissions"]["deny"], leaving every other permissions.*
// key untouched. A no-op when deny is empty — no permissions/deny key is
// created just to add nothing (ADR 0009 I4).
func addPermissionsDeny(settings map[string]any, deny []string) error {
	if len(deny) == 0 {
		return nil
	}
	perms, _ := settings["permissions"].(map[string]any)
	if perms == nil {
		perms = map[string]any{}
	}
	current, err := permissionsDenyList(perms)
	if err != nil {
		return err
	}
	current = dedupAppend(current, deny)
	perms["deny"] = stringsToAny(current)
	settings["permissions"] = perms
	return nil
}

// removePermissionsDeny subtracts deny from settings["permissions"]["deny"],
// leaving every other value (and every other permissions.* key) untouched.
// The `permissions` map itself is never deleted, even if `deny` ends up
// empty (ADR 0009 I4); a `deny` key that becomes empty after subtraction is
// removed rather than kept as `[]`.
func removePermissionsDeny(settings map[string]any, deny []string) error {
	if len(deny) == 0 {
		return nil
	}
	permsRaw, ok := settings["permissions"]
	if !ok {
		return nil
	}
	perms, ok := permsRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("settings.local.json: permissions: expected an object")
	}
	current, err := permissionsDenyList(perms)
	if err != nil {
		return err
	}
	if len(current) == 0 {
		return nil
	}
	remove := make(map[string]bool, len(deny))
	for _, d := range deny {
		remove[d] = true
	}
	kept := current[:0:0]
	for _, d := range current {
		if !remove[d] {
			kept = append(kept, d)
		}
	}
	if len(kept) == 0 {
		delete(perms, "deny")
	} else {
		perms["deny"] = stringsToAny(kept)
	}
	settings["permissions"] = perms
	return nil
}

// permissionsDenyList reads perms["deny"] as a []string. Missing/null is
// (nil, nil); anything present but not an array of strings is an error —
// apply must not silently overwrite a deny list in an unexpected shape.
func permissionsDenyList(perms map[string]any) ([]string, error) {
	v, ok := perms["deny"]
	if !ok || v == nil {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("settings.local.json: permissions.deny: expected an array")
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("settings.local.json: permissions.deny: expected an array of strings")
		}
		out = append(out, s)
	}
	return out, nil
}
