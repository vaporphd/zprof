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

// telemetryHooks maps each Claude Code hook event to the collector mode it
// invokes.
var telemetryHooks = map[string]string{
	"SubagentStop": "subagent-stop",
	"Stop":         "stop",
	"SessionStart": "session-start",
}

// EnsureHooks idempotently upserts the three telemetry hooks (SubagentStop,
// Stop, SessionStart) into <projectDir>/.claude/settings.local.json,
// preserving any existing hooks and non-hook keys. Settings.local.json
// rather than settings.json: the latter is typically committed, so writing
// there would fire the hook on a teammate's machine that never ran
// `zprof apply` and has no collector script (design §4.1).
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

	for event, mode := range telemetryHooks {
		command := fmt.Sprintf(hookGuardTemplate, mode)
		entry := map[string]any{
			"hooks": []any{
				map[string]any{"type": "command", "command": command},
			},
		}

		existing, _ := hooks[event].([]any)
		if hasZprofHook(existing) {
			continue // already installed, don't duplicate
		}
		hooks[event] = append(existing, entry)
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

// hasZprofHook reports whether entries already contains a zprof-collect.py
// invocation, so EnsureHooks can skip re-adding it on a repeat apply.
func hasZprofHook(entries []any) bool {
	for _, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "zprof-collect.py") {
			return true
		}
	}
	return false
}
