package apply

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vaporphd/zprof/internal/overlay"
	"gopkg.in/yaml.v3"
)

// deployCollector writes the telemetry collector script and its schema into
// the project, returning the paths written. Unlike EnsureStateFiles' targets
// (user-editable docs, written once and then left alone), these are
// generated artifacts owned by the base profile: every apply overwrites them
// so bug fixes and schema changes in profiles/base propagate to
// already-applied projects.
//
// Both inputs are optional on Base (older or stripped-down base profiles
// may not ship telemetry at all — see LoadBase), so each is skipped
// independently when its source content is absent, rather than deploying an
// empty (and, for the script, executable) stub. Narrow signature
// (projectDir, base) rather than the full ApplyOpts: this is also the
// building block for DeployTelemetry, which has no overlay/manifest to give it.
func deployCollector(projectDir string, base *overlay.Base) ([]string, error) {
	var written []string

	if len(base.CollectorScript) > 0 {
		scriptDest := filepath.Join(projectDir, ".claude", "zprof-collect.py")
		if err := os.MkdirAll(filepath.Dir(scriptDest), 0o755); err != nil {
			return nil, err
		}
		if err := writeFileAtomic(scriptDest, base.CollectorScript, 0o755); err != nil {
			return nil, fmt.Errorf("write zprof-collect.py: %w", err)
		}
		written = append(written, scriptDest)
	}

	if len(base.TelemetrySchema) > 0 {
		schema, err := yamlToJSON(base.TelemetrySchema)
		if err != nil {
			return nil, fmt.Errorf("convert telemetry.yaml to schema.json: %w", err)
		}
		schemaDest := filepath.Join(projectDir, ".agentlog", "schema.json")
		if err := os.MkdirAll(filepath.Dir(schemaDest), 0o755); err != nil {
			return nil, err
		}
		if err := writeFileAtomic(schemaDest, schema, 0o644); err != nil {
			return nil, fmt.Errorf("write schema.json: %w", err)
		}
		written = append(written, schemaDest)
	}

	return written, nil
}

// DeployTelemetry writes .claude/zprof-collect.py and .agentlog/schema.json
// and upserts the telemetry hooks (SubagentStop/Stop/SessionStart) into
// .claude/settings.local.json. It touches nothing else: no agents, no
// managed CLAUDE.md/AGENT_LOOP.md blocks, no .zprof.yaml. This is the
// narrow redeploy path behind `zprof apply --telemetry-only`, for projects
// where applying a full overlay would be destructive or isn't wanted
// (see ADR 0001).
func DeployTelemetry(projectDir string, base *overlay.Base) ([]string, error) {
	if base == nil {
		return nil, errors.New("base is required")
	}
	written, err := deployCollector(projectDir, base)
	if err != nil {
		return nil, fmt.Errorf("deploy collector: %w", err)
	}
	if err := EnsureHooks(projectDir); err != nil {
		return nil, fmt.Errorf("ensure telemetry hooks: %w", err)
	}
	written = append(written, filepath.Join(projectDir, ".claude", "settings.local.json"))
	return written, nil
}

// yamlToJSON converts telemetry.yaml into indented JSON for schema.json.
// yaml.v3 decodes mappings into map[string]interface{} (unlike yaml.v2's
// map[interface{}]interface{}), so the decoded value round-trips through
// encoding/json without any key normalization.
func yamlToJSON(data []byte) ([]byte, error) {
	var v interface{}
	if err := yaml.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("unmarshal yaml: %w", err)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal json: %w", err)
	}
	return append(out, '\n'), nil
}
