package apply

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/overlay"
	"gopkg.in/yaml.v3"
)

// extraDenyReason is the exact reason text for the synthetic extra_deny
// rule (ADR 0009 I5). It intentionally has no "Не обходи: …" tail — that
// tail is appended exactly once by zprof-guard.py's deny_output(), and no
// base/overlay rule's reason carries it either; duplicating it here would
// double it in the deny output.
const extraDenyReason = "команда запрещена проектным стоп-листом (guard.extra_deny_bash в .zprof.yaml)"

// guardVersion is the only supported guard.yaml `version` value.
const guardVersion = 1

// guardRefFields lists the rule keys `resolveGuardRefs` substitutes into —
// exactly the keys `_validated_str_list` validates in zprof-guard.py.
var guardRefFields = []string{"match", "roles", "not_roles"}

// GuardLayers bundles the overlay and project-level guard.yaml sources
// deployGuard needs on top of Base.GuardSchema. Overlays is nil for a bare
// base deploy (no overlay applied, e.g. `--telemetry-only` without a
// .zprof.yaml); Project is nil when the project has no `guard:` section
// (ADR 0009 I7).
type GuardLayers struct {
	Overlays []*overlay.Overlay
	Project  *manifest.GuardConfig
}

// guardDoc is one layer of guard.yaml, or the merged result of all layers
// (ADR 0009 I1). Known top-level keys get typed fields so merge (§8.2) is a
// compile-checked operation per field; rules stay raw maps keyed by "id" so
// a new rule field (or a whole new context evaluator) never needs a Go
// change. Extra holds any top-level key this Go version doesn't know about,
// so a newer profile still round-trips through an older binary.
type guardDoc struct {
	ReadonlyRoles      []string
	MergeRoles         []string
	AllowWritePrefixes []string
	PermissionsDeny    []string
	ExemptRoles        map[string][]string
	Rules              []map[string]any
	Extra              map[string]any
}

// parseGuardLayer parses one guard.yaml layer (base or a single overlay)
// into a guardDoc. src identifies the layer in error messages ("base",
// "overlay <name>"). requireVersion is true only for the base layer — an
// overlay may omit `version` entirely.
func parseGuardLayer(data []byte, src string, requireVersion bool) (*guardDoc, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	if raw == nil {
		raw = map[string]any{}
	}

	if v, ok := raw["version"]; ok {
		if !isGuardVersion1(v) {
			return nil, fmt.Errorf("%s: unsupported guard.yaml version %v", src, v)
		}
		delete(raw, "version")
	} else if requireVersion {
		return nil, fmt.Errorf("%s: missing required key %q", src, "version")
	}

	doc := &guardDoc{}
	var err error
	if doc.ReadonlyRoles, err = asStringList(raw, "readonly_roles", src); err != nil {
		return nil, err
	}
	if doc.MergeRoles, err = asStringList(raw, "merge_roles", src); err != nil {
		return nil, err
	}
	if doc.AllowWritePrefixes, err = asStringList(raw, "allow_write_prefixes", src); err != nil {
		return nil, err
	}
	if doc.PermissionsDeny, err = asStringList(raw, "permissions_deny", src); err != nil {
		return nil, err
	}
	if doc.ExemptRoles, err = asStringListMap(raw, "exempt_roles", src); err != nil {
		return nil, err
	}
	if doc.Rules, err = asRuleList(raw, "rules", src); err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		doc.Extra = raw
	}
	return doc, nil
}

func isGuardVersion1(v any) bool {
	switch t := v.(type) {
	case int:
		return t == guardVersion
	case int64:
		return t == guardVersion
	case uint64:
		return t == guardVersion
	case float64:
		return t == guardVersion
	default:
		return false
	}
}

// asStringList extracts and removes key from raw as a []string. A missing
// or null key yields (nil, nil); anything else that isn't a list of plain
// strings is an error.
func asStringList(raw map[string]any, key, src string) ([]string, error) {
	v, ok := raw[key]
	delete(raw, key)
	if !ok || v == nil {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: %s: expected list of strings", src, key)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s: %s: expected list of strings", src, key)
		}
		out = append(out, s)
	}
	return out, nil
}

// asStringListMap extracts and removes key from raw as a
// map[string][]string (exempt_roles' shape).
func asStringListMap(raw map[string]any, key, src string) (map[string][]string, error) {
	v, ok := raw[key]
	delete(raw, key)
	if !ok || v == nil {
		return nil, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: %s: expected map of string lists", src, key)
	}
	out := make(map[string][]string, len(m))
	for k, val := range m {
		list, ok := val.([]any)
		if !ok {
			return nil, fmt.Errorf("%s: %s.%s: expected list of strings", src, key, k)
		}
		strs := make([]string, 0, len(list))
		for _, item := range list {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s: %s.%s: expected list of strings", src, key, k)
			}
			strs = append(strs, s)
		}
		out[k] = strs
	}
	return out, nil
}

// asRuleList extracts and removes key from raw as a []map[string]any
// (rules' shape). Only "id" is validated here (non-empty string) — every
// other field stays a raw, unparsed value, per I1.
func asRuleList(raw map[string]any, key, src string) ([]map[string]any, error) {
	v, ok := raw[key]
	delete(raw, key)
	if !ok || v == nil {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: %s: expected list of rules", src, key)
	}
	out := make([]map[string]any, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: %s[%d]: expected a mapping", src, key, i)
		}
		id, ok := m["id"].(string)
		if !ok || id == "" {
			return nil, fmt.Errorf("%s: %s[%d]: rule missing a non-empty string id", src, key, i)
		}
		out = append(out, m)
	}
	return out, nil
}

// mergeGuard merges base, then overlays in order, then the project layer,
// into one guardDoc (ADR 0009 I2, spec §8.2/§8.3). Base is always merged
// even if nil-ish (empty doc); this is a pure function — none of the input
// docs are mutated except for the rule maps' `match`/`roles`/`not_roles`
// values, which resolveGuardRefs mutates afterwards.
func mergeGuard(base *guardDoc, overlays []*guardDoc, proj *manifest.GuardConfig) *guardDoc {
	merged := &guardDoc{}
	layers := make([]*guardDoc, 0, 1+len(overlays))
	layers = append(layers, base)
	layers = append(layers, overlays...)
	for _, l := range layers {
		if l == nil {
			continue
		}
		merged.ReadonlyRoles = dedupAppend(merged.ReadonlyRoles, l.ReadonlyRoles)
		merged.MergeRoles = dedupAppend(merged.MergeRoles, l.MergeRoles)
		merged.AllowWritePrefixes = dedupAppend(merged.AllowWritePrefixes, l.AllowWritePrefixes)
		merged.PermissionsDeny = dedupAppend(merged.PermissionsDeny, l.PermissionsDeny)
		merged.ExemptRoles = mergeExemptRoles(merged.ExemptRoles, l.ExemptRoles)
		merged.Rules = mergeRules(merged.Rules, l.Rules)
		merged.Extra = mergeExtra(merged.Extra, l.Extra)
	}

	if proj == nil {
		return merged
	}

	merged.ReadonlyRoles = dedupAppend(merged.ReadonlyRoles, proj.ReadonlyRoles)
	merged.AllowWritePrefixes = dedupAppend(merged.AllowWritePrefixes, proj.AllowWriteOutside)
	merged.ExemptRoles = mergeExemptRoles(merged.ExemptRoles, proj.ExemptRoles)

	// MergeRoles is the one documented exception (AC5/I2): a non-empty
	// project list replaces the merged result wholesale rather than
	// extending it. An empty/nil list must NOT mean "nobody merges" — see
	// GuardConfig.MergeRoles's doc comment.
	if len(proj.MergeRoles) > 0 {
		merged.MergeRoles = append([]string(nil), proj.MergeRoles...)
	}

	if len(proj.ExtraDenyBash) > 0 {
		rule := map[string]any{
			"id":     "extra_deny",
			"tools":  []any{"Bash"},
			"match":  stringsToAny(dedupAppend(nil, proj.ExtraDenyBash)),
			"reason": extraDenyReason,
		}
		merged.Rules = mergeRules(merged.Rules, []map[string]any{rule})
	}

	return merged
}

// dedupAppend appends the elements of src to dst that aren't already in
// dst, preserving first-occurrence order (ADR 0009 I1: "первая встреча
// значения сохраняет позицию").
func dedupAppend(dst, src []string) []string {
	if len(src) == 0 {
		return dst
	}
	seen := make(map[string]bool, len(dst)+len(src))
	for _, v := range dst {
		seen[v] = true
	}
	for _, v := range src {
		if seen[v] {
			continue
		}
		seen[v] = true
		dst = append(dst, v)
	}
	return dst
}

// mergeExemptRoles unions src into dst per key, deduping each key's list.
func mergeExemptRoles(dst, src map[string][]string) map[string][]string {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = map[string][]string{}
	}
	for k, v := range src {
		dst[k] = dedupAppend(dst[k], v)
	}
	return dst
}

// mergeRules merges src into dst by rule id: an id already present in dst
// is replaced in place (its position preserved); a new id is appended in
// src's order.
func mergeRules(dst, src []map[string]any) []map[string]any {
	if len(src) == 0 {
		return dst
	}
	index := make(map[string]int, len(dst))
	for i, r := range dst {
		if id, ok := r["id"].(string); ok {
			index[id] = i
		}
	}
	for _, r := range src {
		id, _ := r["id"].(string)
		if i, ok := index[id]; ok {
			dst[i] = r
			continue
		}
		index[id] = len(dst)
		dst = append(dst, r)
	}
	return dst
}

// mergeExtra overlays src onto dst key-by-key — the "поздний слой
// перекрывает" rule applied to unknown top-level keys (ADR 0009 I1).
func mergeExtra(dst, src map[string]any) map[string]any {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func stringsToAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// guardNeedsMutatingPatterns reports whether any rule's match/roles/
// not_roles is the bare reference "$mutating_bash_patterns" — used to
// decide, before resolveGuardRefs runs, whether Base.TelemetrySchema must
// be parsed at all (ADR 0009 I3: read lazily).
func guardNeedsMutatingPatterns(doc *guardDoc) bool {
	for _, rule := range doc.Rules {
		for _, key := range guardRefFields {
			if s, ok := rule[key].(string); ok && s == "$mutating_bash_patterns" {
				return true
			}
		}
	}
	return false
}

// extractMutatingBashPatterns reads the `mutating_bash_patterns` key out of
// telemetry.yaml (Base.TelemetrySchema). Only called when a guard rule
// actually references $mutating_bash_patterns; an absent/empty source is
// then an apply error, not a silently-empty substitution (ADR 0009 I3).
func extractMutatingBashPatterns(telemetrySchema []byte) ([]string, error) {
	const src = "telemetry.yaml"
	if len(telemetrySchema) == 0 {
		return nil, fmt.Errorf("guard rule references $mutating_bash_patterns but base telemetry.yaml is absent")
	}
	var raw map[string]any
	if err := yaml.Unmarshal(telemetrySchema, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", src, err)
	}
	if raw == nil {
		raw = map[string]any{}
	}
	list, err := asStringList(raw, "mutating_bash_patterns", src)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("%s: mutating_bash_patterns: missing or empty, but referenced by a guard rule", src)
	}
	return list, nil
}

// resolveGuardRefs substitutes $readonly_roles, $merge_roles, and
// $mutating_bash_patterns in every rule's match/roles/not_roles with a copy
// of the resolved list, in a single pass over the already-merged doc (ADR
// 0009 I3). It mutates doc.Rules in place.
func resolveGuardRefs(doc *guardDoc, mutating []string) error {
	refs := map[string][]string{
		"$readonly_roles":         doc.ReadonlyRoles,
		"$merge_roles":            doc.MergeRoles,
		"$mutating_bash_patterns": mutating,
	}
	for _, rule := range doc.Rules {
		id, _ := rule["id"].(string)
		for _, key := range guardRefFields {
			v, ok := rule[key]
			if !ok || v == nil {
				continue
			}
			resolved, err := resolveGuardField(key, v, refs)
			if err != nil {
				return fmt.Errorf("guard rule %q: %s: %w", id, key, err)
			}
			rule[key] = resolved
		}
	}
	return nil
}

// resolveGuardField resolves one match/roles/not_roles value: a bare
// "$name" string is replaced by a copy of the referenced list; a list is
// checked for a leading-"$" element (unresolved reference, or a splice the
// spec doesn't support) and returned unchanged otherwise. Any other shape
// passes through untouched — zprof-guard.py's own _validated_str_list is
// the final arbiter of what's a legal match/roles/not_roles value.
func resolveGuardField(key string, v any, refs map[string][]string) (any, error) {
	if s, ok := v.(string); ok {
		if !strings.HasPrefix(s, "$") {
			return v, nil
		}
		list, ok := refs[s]
		if !ok {
			return nil, fmt.Errorf("unknown reference %q", s)
		}
		if (key == "roles" || key == "not_roles") && len(list) == 0 {
			return nil, fmt.Errorf("reference %q resolved to an empty list", s)
		}
		return stringsToAny(append([]string(nil), list...)), nil
	}
	list, ok := v.([]any)
	if !ok {
		return v, nil
	}
	for _, item := range list {
		if s, ok := item.(string); ok && strings.HasPrefix(s, "$") {
			return nil, fmt.Errorf("unresolved reference %q", s)
		}
	}
	return v, nil
}

// render serializes the merged doc to guard.json's exact shape: `version:
// 1`, the five known keys (nil slices/maps normalized to `[]`/`{}`, never
// `null` — ADR 0009 I1, Context п.5), `rules`, and any Extra keys.
// encoding/json sorts map keys, so the byte output is stable across
// repeated applies with unchanged input.
func (d *guardDoc) render() ([]byte, error) {
	out := map[string]any{
		"version":              guardVersion,
		"readonly_roles":       normalizeStrings(d.ReadonlyRoles),
		"merge_roles":          normalizeStrings(d.MergeRoles),
		"allow_write_prefixes": normalizeStrings(d.AllowWritePrefixes),
		"permissions_deny":     normalizeStrings(d.PermissionsDeny),
		"exempt_roles":         normalizeExemptRoles(d.ExemptRoles),
		"rules":                normalizeRules(d.Rules),
	}
	for k, v := range d.Extra {
		out[k] = v
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal guard.json: %w", err)
	}
	return append(data, '\n'), nil
}

func normalizeStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func normalizeExemptRoles(m map[string][]string) map[string][]string {
	out := make(map[string][]string, len(m))
	for k, v := range m {
		out[k] = normalizeStrings(v)
	}
	return out
}

func normalizeRules(r []map[string]any) []map[string]any {
	if r == nil {
		return []map[string]any{}
	}
	return r
}

// deployGuard merges base/overlay/project guard.yaml layers, resolves $
// references, and — when guard is enabled for this project — writes
// .claude/zprof-guard.py and .claude/guard.json, returning their paths.
// The merge itself always runs regardless of enabled (ADR 0009 I4): a
// broken overlay guard.yaml fails apply even for a disabled project.
//
// A no-op ("nil, nil": no files, no hooks, no removal) when the base
// profile ships neither GuardScript nor GuardSchema — an older or
// stripped-down base, exactly like deployCollector's optional-content
// handling.
//
// Narrow signature (projectDir, base, layers) rather than the full
// ApplyOpts, for the same reason deployCollector's is narrow (see its doc
// comment): this is also DeployTelemetry's building block, and
// --telemetry-only has no ApplyOpts to give it.
func deployGuard(projectDir string, base *overlay.Base, layers GuardLayers) ([]string, error) {
	if len(base.GuardScript) == 0 || len(base.GuardSchema) == 0 {
		return nil, nil
	}

	baseDoc, err := parseGuardLayer(base.GuardSchema, "base", true)
	if err != nil {
		return nil, err
	}

	var overlayDocs []*guardDoc
	for _, o := range layers.Overlays {
		if o == nil || len(o.GuardSchema) == 0 {
			continue
		}
		src := "overlay " + o.Manifest.Name
		d, err := parseGuardLayer(o.GuardSchema, src, false)
		if err != nil {
			return nil, err
		}
		overlayDocs = append(overlayDocs, d)
	}

	merged := mergeGuard(baseDoc, overlayDocs, layers.Project)

	var mutating []string
	if guardNeedsMutatingPatterns(merged) {
		mutating, err = extractMutatingBashPatterns(base.TelemetrySchema)
		if err != nil {
			return nil, err
		}
	}
	if err := resolveGuardRefs(merged, mutating); err != nil {
		return nil, err
	}

	enabled := layers.Project.IsEnabled()
	if err := ensureGuardSettings(projectDir, enabled, merged.PermissionsDeny); err != nil {
		return nil, fmt.Errorf("ensure guard settings: %w", err)
	}

	if !enabled {
		// zprof-guard.py/guard.json from a previous enabled apply, if any,
		// are left exactly as they are (ADR 0009 I6) — hooks are already
		// gone, so nothing invokes them.
		return nil, nil
	}

	scriptDest := filepath.Join(projectDir, ".claude", "zprof-guard.py")
	if err := os.MkdirAll(filepath.Dir(scriptDest), 0o755); err != nil {
		return nil, err
	}
	if err := writeFileAtomic(scriptDest, base.GuardScript, 0o755); err != nil {
		return nil, fmt.Errorf("write zprof-guard.py: %w", err)
	}

	rendered, err := merged.render()
	if err != nil {
		return nil, err
	}
	jsonDest := filepath.Join(projectDir, ".claude", "guard.json")
	if err := writeFileAtomic(jsonDest, rendered, 0o644); err != nil {
		return nil, fmt.Errorf("write guard.json: %w", err)
	}

	return []string{scriptDest, jsonDest}, nil
}
