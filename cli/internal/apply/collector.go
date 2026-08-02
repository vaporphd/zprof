package apply

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
// empty (and, for the script, executable) stub.
func deployCollector(opts ApplyOpts) ([]string, error) {
	var written []string

	if len(opts.Base.CollectorScript) > 0 {
		scriptDest := filepath.Join(opts.ProjectDir, ".claude", "zprof-collect.py")
		if err := os.MkdirAll(filepath.Dir(scriptDest), 0o755); err != nil {
			return nil, err
		}
		if err := writeFileAtomic(scriptDest, opts.Base.CollectorScript, 0o755); err != nil {
			return nil, fmt.Errorf("write zprof-collect.py: %w", err)
		}
		written = append(written, scriptDest)
	}

	if len(opts.Base.TelemetrySchema) > 0 {
		schema, err := yamlToJSON(opts.Base.TelemetrySchema)
		if err != nil {
			return nil, fmt.Errorf("convert telemetry.yaml to schema.json: %w", err)
		}
		schemaDest := filepath.Join(opts.ProjectDir, ".agentlog", "schema.json")
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
