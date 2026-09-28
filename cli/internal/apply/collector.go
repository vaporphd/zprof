package apply

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vaporphd/zprof/internal/overlay"
	"github.com/vaporphd/zprof/internal/verdicts"
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
		schema, err := renderSchema(base.TelemetrySchema, base.Verdicts)
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

// DeployTelemetry writes .claude/zprof-collect.py, .agentlog/schema.json,
// and the guard artifacts (.claude/zprof-guard.py, .claude/guard.json —
// ADR 0009), then upserts the telemetry hooks (SubagentStop/Stop/
// SessionStart) and the guard hooks (PreToolUse/SubagentStop) into
// .claude/settings.local.json. It touches nothing else: no agents, no
// managed CLAUDE.md/AGENT_LOOP.md blocks, no .zprof.yaml. This is the
// narrow redeploy path behind `zprof apply --telemetry-only`, for projects
// where applying a full overlay would be destructive or isn't wanted
// (see ADR 0001). deployGuard lives inside this single function — not
// duplicated between Apply() and --telemetry-only — precisely so the two
// paths can never deploy guard differently (ADR 0009 I7).
func DeployTelemetry(projectDir string, base *overlay.Base, layers GuardLayers) ([]string, error) {
	if base == nil {
		return nil, errors.New("base is required")
	}
	written, err := deployCollector(projectDir, base)
	if err != nil {
		return nil, fmt.Errorf("deploy collector: %w", err)
	}
	guardFiles, err := deployGuard(projectDir, base, layers)
	if err != nil {
		return nil, fmt.Errorf("deploy guard: %w", err)
	}
	written = append(written, guardFiles...)
	if err := EnsureHooks(projectDir); err != nil {
		return nil, fmt.Errorf("ensure telemetry hooks: %w", err)
	}
	written = append(written, filepath.Join(projectDir, ".claude", "settings.local.json"))
	return written, nil
}

// renderSchema converts telemetry.yaml into indented JSON for schema.json,
// merging the verdicts registry (ADR 0003, docs/adr/0003-verdicts-registry.md
// §D4) into it under the top-level `verdicts` key when verdictsYAML is
// non-empty. yaml.v3 decodes mappings into map[string]interface{} (unlike
// yaml.v2's map[interface{}]interface{}), so the decoded telemetry value
// round-trips through encoding/json without any key normalization.
//
// The registry is deployed by merging rather than as its own file: every
// runtime reader of the zprof contract (collector, score, the future guard
// renderer) already reads schema.json, and `zprof apply --telemetry-only`
// (ADR 0001) gets the registry for free without touching DeployTelemetry's
// signature. A bad registry fails the apply outright (fail-closed) — that's
// an authoring error in zprof caught by the repo-level consistency test,
// not something a project author can fix.
func renderSchema(telemetry, verdictsYAML []byte) ([]byte, error) {
	var v interface{}
	if err := yaml.Unmarshal(telemetry, &v); err != nil {
		return nil, fmt.Errorf("unmarshal yaml: %w", err)
	}
	if len(verdictsYAML) > 0 {
		m, ok := v.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("render verdicts into schema.json: telemetry.yaml: top level is not a mapping")
		}
		if err := mergeVerdicts(m, verdictsYAML); err != nil {
			return nil, fmt.Errorf("render verdicts into schema.json: %w", err)
		}
		v = m
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal json: %w", err)
	}
	return append(out, '\n'), nil
}

// mergeVerdicts parses and validates verdictsYAML, then sets telemetry's
// `verdicts` key to its normalized form. It errors if telemetry.yaml
// already defines that key — the two sources must not collide silently.
func mergeVerdicts(telemetry map[string]interface{}, verdictsYAML []byte) error {
	if _, exists := telemetry["verdicts"]; exists {
		return fmt.Errorf("telemetry.yaml must not define top-level key %q", "verdicts")
	}
	reg, err := verdicts.Parse(verdictsYAML)
	if err != nil {
		return err
	}
	if err := reg.Validate(); err != nil {
		return err
	}
	telemetry["verdicts"] = reg.Normalized()
	return nil
}
