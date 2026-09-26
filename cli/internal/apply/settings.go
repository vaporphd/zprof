package apply

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vaporphd/zprof/internal/fsutil"
)

// hookGuardTemplate is the command every telemetry hook runs. The guard
// (`test -x … &&`) matters because "any error -> exit 0" (design §6.1)
// covers the collector script itself, but a missing script fails in the
// shell *before* the script ever runs — without the guard, a project that
// hasn't run `zprof apply` yet (no zprof-collect.py) would fail the hook.
const hookGuardTemplate = `test -x "$CLAUDE_PROJECT_DIR/.claude/zprof-collect.py" && "$CLAUDE_PROJECT_DIR/.claude/zprof-collect.py" %s || true`

// scoreHookCommand runs the per-task scorecard after the collector. It is
// chained into the same Stop command because Claude Code runs an event's
// hooks in parallel, and `zprof score --no-collect` must see the collector's
// output. `command -v zprof` keeps projects without the binary silent.
const scoreHookCommand = `command -v zprof >/dev/null 2>&1 && cd "$CLAUDE_PROJECT_DIR" && zprof score --latest --quiet --no-collect || true`

type hookSpec struct {
	event   string
	command string
}

// telemetryHooks lists, in install order, the command each hook event runs.
var telemetryHooks = []hookSpec{
	{"SubagentStop", fmt.Sprintf(hookGuardTemplate, "subagent-stop")},
	{"Stop", fmt.Sprintf(hookGuardTemplate, "stop") + "; " + scoreHookCommand},
	{"SessionStart", fmt.Sprintf(hookGuardTemplate, "session-start")},
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
	claudeDir := filepath.Join(projectDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}
	settingsPath := filepath.Join(claudeDir, "settings.local.json")

	settings := map[string]any{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		if len(data) > 0 {
			if err := json.Unmarshal(data, &settings); err != nil {
				return fmt.Errorf("parse %s: %w", settingsPath, err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", settingsPath, err)
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	for _, spec := range telemetryHooks {
		entry := map[string]any{
			"hooks": []any{
				map[string]any{"type": "command", "command": spec.command},
			},
		}
		existing, _ := hooks[spec.event].([]any)
		if idx := zprofHookIndex(existing); idx >= 0 {
			if hookCommand(existing[idx]) != spec.command {
				existing[idx] = entry // stale zprof hook (older command shape) → upgrade in place
				hooks[spec.event] = existing
			}
			continue
		}
		hooks[spec.event] = append(existing, entry)
	}

	settings["hooks"] = hooks

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", settingsPath, err)
	}
	data = append(data, '\n')
	if err := fsutil.WriteFileAtomic(settingsPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", settingsPath, err)
	}
	return nil
}

// zprofHookIndex returns the position of the entry that invokes
// zprof-collect.py, or -1. Used both to skip duplicates and to upgrade a
// stale command in place.
func zprofHookIndex(entries []any) int {
	for i, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "zprof-collect.py") {
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
