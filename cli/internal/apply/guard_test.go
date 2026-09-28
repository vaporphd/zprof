package apply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/overlay"
)

// baseGuardYAML is a small but representative base guard.yaml: two plain
// rules, one $readonly_roles reference, one $mutating_bash_patterns
// reference, one $merge_roles reference — enough to exercise merge and
// $-substitution without the full profiles/base/guard.yaml catalog.
const baseGuardYAML = `
version: 1
readonly_roles: [auditor]
merge_roles: [pr-shepherd]
allow_write_prefixes: ["$CLAUDE_PROJECT_DIR"]
permissions_deny:
  - "Bash(git push --force*)"
exempt_roles:
  force_push: [architect]
rules:
  - id: force_push
    tools: [Bash]
    match:
      - '\bgit\s+push\b.*--force\b'
    reason: "force-push запрещён стоп-листом"
  - id: readonly_mutation
    tools: [Bash]
    roles: $readonly_roles
    match: $mutating_bash_patterns
    reason: "роль read-only: мутирующая команда запрещена контрактом"
  - id: merge_role
    tools: [Bash]
    not_roles: $merge_roles
    match:
      - '\bgh\s+pr\s+merge\b'
    reason: "merge выполняет только pr-shepherd"
`

const baseTelemetryYAML = `
mutating_bash_patterns:
  - "\\btouch\\b"
  - "\\brm\\b"
`

func testBase() *overlay.Base {
	return &overlay.Base{
		GuardScript:     []byte("#!/usr/bin/env python3\nprint('guard')\n"),
		GuardSchema:     []byte(baseGuardYAML),
		TelemetrySchema: []byte(baseTelemetryYAML),
	}
}

// --- parseGuardLayer --------------------------------------------------

func TestParseGuardLayer_BaseRequiresVersion(t *testing.T) {
	_, err := parseGuardLayer([]byte("readonly_roles: [x]\n"), "base", true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing required key")
}

func TestParseGuardLayer_OverlayVersionOptional(t *testing.T) {
	doc, err := parseGuardLayer([]byte("readonly_roles: [architect]\n"), "overlay fake", false)
	require.NoError(t, err)
	require.Equal(t, []string{"architect"}, doc.ReadonlyRoles)
}

func TestParseGuardLayer_UnsupportedVersion(t *testing.T) {
	_, err := parseGuardLayer([]byte("version: 2\n"), "base", true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported guard.yaml version")
}

func TestParseGuardLayer_WrongTypeErrors(t *testing.T) {
	_, err := parseGuardLayer([]byte("version: 1\nreadonly_roles: \"not-a-list\"\n"), "base", true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "readonly_roles: expected list of strings")
}

func TestParseGuardLayer_RuleMissingIDErrors(t *testing.T) {
	_, err := parseGuardLayer([]byte("version: 1\nrules:\n  - tools: [Bash]\n"), "base", true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "rule missing a non-empty string id")
}

func TestParseGuardLayer_UnknownTopLevelKeyGoesToExtra(t *testing.T) {
	doc, err := parseGuardLayer([]byte("version: 1\nfuture_field: [a, b]\n"), "base", true)
	require.NoError(t, err)
	require.Equal(t, []any{"a", "b"}, doc.Extra["future_field"])
}

func TestParseGuardLayer_FullBaseFixture(t *testing.T) {
	doc, err := parseGuardLayer([]byte(baseGuardYAML), "base", true)
	require.NoError(t, err)
	require.Equal(t, []string{"auditor"}, doc.ReadonlyRoles)
	require.Equal(t, []string{"pr-shepherd"}, doc.MergeRoles)
	require.Equal(t, []string{"architect"}, doc.ExemptRoles["force_push"])
	require.Len(t, doc.Rules, 3)
}

// --- mergeGuard ---------------------------------------------------------

func TestMergeGuard_OverlayConcatenatesAndDedups(t *testing.T) {
	base := &guardDoc{ReadonlyRoles: []string{"auditor"}, PermissionsDeny: []string{"a"}}
	overlay1 := &guardDoc{ReadonlyRoles: []string{"auditor", "architect"}, PermissionsDeny: []string{"a", "b"}}
	merged := mergeGuard(base, []*guardDoc{overlay1}, nil)
	require.Equal(t, []string{"auditor", "architect"}, merged.ReadonlyRoles, "dedup preserves first-occurrence order")
	require.Equal(t, []string{"a", "b"}, merged.PermissionsDeny)
}

func TestMergeGuard_RulesByIDLateReplacesInPlace(t *testing.T) {
	base := &guardDoc{Rules: []map[string]any{
		{"id": "force_push", "reason": "base reason"},
		{"id": "other", "reason": "kept"},
	}}
	overlay1 := &guardDoc{Rules: []map[string]any{
		{"id": "force_push", "reason": "overlay reason"},
	}}
	merged := mergeGuard(base, []*guardDoc{overlay1}, nil)
	require.Len(t, merged.Rules, 2, "replaced in place, not appended")
	require.Equal(t, "overlay reason", merged.Rules[0]["reason"], "position preserved")
	require.Equal(t, "kept", merged.Rules[1]["reason"])
}

func TestMergeGuard_ExtraLateLayerOverrides(t *testing.T) {
	base := &guardDoc{Extra: map[string]any{"x": "base"}}
	overlay1 := &guardDoc{Extra: map[string]any{"x": "overlay", "y": "overlay-only"}}
	merged := mergeGuard(base, []*guardDoc{overlay1}, nil)
	require.Equal(t, "overlay", merged.Extra["x"])
	require.Equal(t, "overlay-only", merged.Extra["y"])
}

// TestMergeGuard_MergeRolesDirection fixes AC5 in both directions: overlay
// concatenates, project replaces only when non-empty.
func TestMergeGuard_MergeRolesDirection(t *testing.T) {
	base := &guardDoc{MergeRoles: []string{"pr-shepherd"}}
	overlay1 := &guardDoc{MergeRoles: []string{"pr-shepherd", "architect"}}

	t.Run("overlay concatenates", func(t *testing.T) {
		merged := mergeGuard(base, []*guardDoc{overlay1}, nil)
		require.Equal(t, []string{"pr-shepherd", "architect"}, merged.MergeRoles)
	})

	t.Run("project replaces wholesale when non-empty", func(t *testing.T) {
		proj := &manifest.GuardConfig{MergeRoles: []string{"solo-role"}}
		merged := mergeGuard(base, []*guardDoc{overlay1}, proj)
		require.Equal(t, []string{"solo-role"}, merged.MergeRoles)
	})

	t.Run("empty project list does not clear merge_roles", func(t *testing.T) {
		proj := &manifest.GuardConfig{MergeRoles: []string{}}
		merged := mergeGuard(base, []*guardDoc{overlay1}, proj)
		require.Equal(t, []string{"pr-shepherd", "architect"}, merged.MergeRoles, "empty list must not strip the base+overlay result")
	})

	t.Run("nil project GuardConfig does not clear merge_roles", func(t *testing.T) {
		merged := mergeGuard(base, []*guardDoc{overlay1}, nil)
		require.Equal(t, []string{"pr-shepherd", "architect"}, merged.MergeRoles)
	})
}

func TestMergeGuard_ProjectReadonlyAndAllowWriteOutsideAppend(t *testing.T) {
	base := &guardDoc{ReadonlyRoles: []string{"auditor"}, AllowWritePrefixes: []string{"$CLAUDE_PROJECT_DIR"}}
	proj := &manifest.GuardConfig{
		ReadonlyRoles:     []string{"explorer"},
		AllowWriteOutside: []string{"/tmp/scratch"},
	}
	merged := mergeGuard(base, nil, proj)
	require.Equal(t, []string{"auditor", "explorer"}, merged.ReadonlyRoles)
	require.Equal(t, []string{"$CLAUDE_PROJECT_DIR", "/tmp/scratch"}, merged.AllowWritePrefixes)
}

func TestMergeGuard_ProjectExemptRolesUnion(t *testing.T) {
	base := &guardDoc{ExemptRoles: map[string][]string{"force_push": {"architect"}}}
	proj := &manifest.GuardConfig{ExemptRoles: map[string][]string{"force_push": {"pr-shepherd"}, "tag_delete": {"architect"}}}
	merged := mergeGuard(base, nil, proj)
	require.ElementsMatch(t, []string{"architect", "pr-shepherd"}, merged.ExemptRoles["force_push"])
	require.Equal(t, []string{"architect"}, merged.ExemptRoles["tag_delete"])
}

func TestMergeGuard_ExtraDenyBashProducesSyntheticRule(t *testing.T) {
	base := &guardDoc{}
	proj := &manifest.GuardConfig{ExtraDenyBash: []string{"rm -rf /", "rm -rf /", "curl evil.sh"}}
	merged := mergeGuard(base, nil, proj)
	require.Len(t, merged.Rules, 1)
	rule := merged.Rules[0]
	require.Equal(t, "extra_deny", rule["id"])
	require.Equal(t, []any{"Bash"}, rule["tools"])
	require.Equal(t, []any{"rm -rf /", "curl evil.sh"}, rule["match"], "deduped, order preserved")
	require.Equal(t, extraDenyReason, rule["reason"])
}

func TestExtraDenyReason_ExactText(t *testing.T) {
	// ADR 0009 I5: reason has no "Не обходи…" tail — deny_output() adds it
	// exactly once. Pinned so a future edit can't silently reintroduce it.
	require.Equal(t, "команда запрещена проектным стоп-листом (guard.extra_deny_bash в .zprof.yaml)", extraDenyReason)
	require.NotContains(t, extraDenyReason, "Не обходи")
}

// --- resolveGuardRefs -----------------------------------------------------

func TestResolveGuardRefs_SubstitutesKnownRefs(t *testing.T) {
	doc := &guardDoc{
		ReadonlyRoles: []string{"auditor", "explorer"},
		MergeRoles:    []string{"pr-shepherd"},
		Rules: []map[string]any{
			{"id": "readonly_mutation", "roles": "$readonly_roles", "match": "$mutating_bash_patterns"},
			{"id": "merge_role", "not_roles": "$merge_roles"},
		},
	}
	err := resolveGuardRefs(doc, []string{"\\btouch\\b"})
	require.NoError(t, err)
	require.Equal(t, []any{"auditor", "explorer"}, doc.Rules[0]["roles"])
	require.Equal(t, []any{"\\btouch\\b"}, doc.Rules[0]["match"])
	require.Equal(t, []any{"pr-shepherd"}, doc.Rules[1]["not_roles"])
}

func TestResolveGuardRefs_UnknownReferenceErrors(t *testing.T) {
	doc := &guardDoc{Rules: []map[string]any{{"id": "x", "match": "$bogus"}}}
	err := resolveGuardRefs(doc, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), `guard rule "x": match`)
	require.Contains(t, err.Error(), "unknown reference")
}

func TestResolveGuardRefs_LeadingDollarInsideListErrors(t *testing.T) {
	doc := &guardDoc{Rules: []map[string]any{{"id": "x", "match": []any{"ok", "$readonly_roles"}}}}
	err := resolveGuardRefs(doc, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unresolved reference")
}

func TestResolveGuardRefs_EmptyRolesReferenceErrors(t *testing.T) {
	doc := &guardDoc{
		ReadonlyRoles: nil,
		Rules:         []map[string]any{{"id": "readonly_mutation", "roles": "$readonly_roles"}},
	}
	err := resolveGuardRefs(doc, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "resolved to an empty list")
}

func TestResolveGuardRefs_EmptyMatchReferenceIsNotAnError(t *testing.T) {
	// Invariant 3 (ADR 0009 I3) scopes the empty-list guard to roles/
	// not_roles only — match has no such documented invariant.
	doc := &guardDoc{Rules: []map[string]any{{"id": "x", "match": "$mutating_bash_patterns"}}}
	err := resolveGuardRefs(doc, nil)
	require.NoError(t, err)
	require.Equal(t, []any{}, doc.Rules[0]["match"])
}

func TestGuardNeedsMutatingPatterns(t *testing.T) {
	require.True(t, guardNeedsMutatingPatterns(&guardDoc{Rules: []map[string]any{{"match": "$mutating_bash_patterns"}}}))
	require.False(t, guardNeedsMutatingPatterns(&guardDoc{Rules: []map[string]any{{"match": []any{"literal"}}}}))
	require.False(t, guardNeedsMutatingPatterns(&guardDoc{}))
}

func TestExtractMutatingBashPatterns_MissingSourceErrors(t *testing.T) {
	_, err := extractMutatingBashPatterns(nil)
	require.Error(t, err)
}

func TestExtractMutatingBashPatterns_MissingKeyErrors(t *testing.T) {
	_, err := extractMutatingBashPatterns([]byte("other_key: []\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "mutating_bash_patterns")
}

func TestExtractMutatingBashPatterns_ReadsList(t *testing.T) {
	list, err := extractMutatingBashPatterns([]byte(baseTelemetryYAML))
	require.NoError(t, err)
	require.Equal(t, []string{`\btouch\b`, `\brm\b`}, list)
}

// --- guardDoc.render ------------------------------------------------------

func TestGuardDocRender_NilContainersAreEmptyNotNull(t *testing.T) {
	doc := &guardDoc{}
	data, err := doc.render()
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, float64(1), out["version"])
	require.Equal(t, []any{}, out["readonly_roles"])
	require.Equal(t, []any{}, out["merge_roles"])
	require.Equal(t, []any{}, out["allow_write_prefixes"])
	require.Equal(t, []any{}, out["permissions_deny"])
	require.Equal(t, map[string]any{}, out["exempt_roles"])
	require.Equal(t, []any{}, out["rules"])

	require.NotContains(t, string(data), "null")
}

// --- deployGuard ------------------------------------------------------

func TestDeployGuard_NoOpWithoutBaseGuardContent(t *testing.T) {
	dir := t.TempDir()
	written, err := deployGuard(dir, &overlay.Base{}, GuardLayers{})
	require.NoError(t, err)
	require.Empty(t, written)
	_, err = os.Stat(filepath.Join(dir, ".claude", "guard.json"))
	require.True(t, os.IsNotExist(err))
}

func TestDeployGuard_WritesScriptAndJSONWithPerms(t *testing.T) {
	dir := t.TempDir()
	written, err := deployGuard(dir, testBase(), GuardLayers{})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		filepath.Join(dir, ".claude", "zprof-guard.py"),
		filepath.Join(dir, ".claude", "guard.json"),
	}, written)

	scriptInfo, err := os.Stat(filepath.Join(dir, ".claude", "zprof-guard.py"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), scriptInfo.Mode().Perm())

	jsonInfo, err := os.Stat(filepath.Join(dir, ".claude", "guard.json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o644), jsonInfo.Mode().Perm())

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "guard.json"))
	require.NoError(t, err)
	require.NotContains(t, string(data), "$readonly_roles")
	require.NotContains(t, string(data), "$mutating_bash_patterns")
	require.NotContains(t, string(data), "$merge_roles")

	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	require.Equal(t, float64(1), doc["version"])
}

func TestDeployGuard_MergesOverlayAndProjectLayers(t *testing.T) {
	dir := t.TempDir()
	o := &overlay.Overlay{
		Manifest:    &manifest.OverlayManifest{Name: "fake-ios"},
		GuardSchema: []byte("readonly_roles: [architect]\n"),
	}
	proj := &manifest.GuardConfig{ExtraDenyBash: []string{"nuke everything"}}

	_, err := deployGuard(dir, testBase(), GuardLayers{Overlays: []*overlay.Overlay{o}, Project: proj})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "guard.json"))
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	require.ElementsMatch(t, []any{"auditor", "architect"}, doc["readonly_roles"])

	rules := doc["rules"].([]any)
	var foundExtraDeny bool
	for _, r := range rules {
		rule := r.(map[string]any)
		if rule["id"] == "extra_deny" {
			foundExtraDeny = true
			require.Equal(t, []any{"nuke everything"}, rule["match"])
		}
	}
	require.True(t, foundExtraDeny, "extra_deny rule must be present")
}

func TestDeployGuard_BadOverlayGuardYAMLErrors(t *testing.T) {
	dir := t.TempDir()
	o := &overlay.Overlay{
		Manifest:    &manifest.OverlayManifest{Name: "fake-ios"},
		GuardSchema: []byte("readonly_roles: \"not-a-list\"\n"),
	}
	_, err := deployGuard(dir, testBase(), GuardLayers{Overlays: []*overlay.Overlay{o}})
	require.Error(t, err)
}

func TestDeployGuard_DisabledSkipsFilesAndSetsUpDeny(t *testing.T) {
	dir := t.TempDir()
	proj := &manifest.GuardConfig{Enabled: boolPtr(false)}
	written, err := deployGuard(dir, testBase(), GuardLayers{Project: proj})
	require.NoError(t, err)
	require.Empty(t, written, "disabled deploy reports no written guard files")

	_, err = os.Stat(filepath.Join(dir, ".claude", "guard.json"))
	require.True(t, os.IsNotExist(err), "disabled deploy must not create guard.json")
	_, err = os.Stat(filepath.Join(dir, ".claude", "zprof-guard.py"))
	require.True(t, os.IsNotExist(err))

	settingsData, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	require.NoError(t, err)
	var settings map[string]any
	require.NoError(t, json.Unmarshal(settingsData, &settings))
	hooks, _ := settings["hooks"].(map[string]any)
	require.NotContains(t, hooks, "PreToolUse", "no guard hooks were ever added, none to remove — but none should exist")
}

func TestDeployGuard_DisabledAfterEnabledLeavesExistingFilesUntouched(t *testing.T) {
	dir := t.TempDir()
	base := testBase()

	// First apply: enabled.
	_, err := deployGuard(dir, base, GuardLayers{})
	require.NoError(t, err)
	before, err := os.ReadFile(filepath.Join(dir, ".claude", "guard.json"))
	require.NoError(t, err)
	scriptBefore, err := os.ReadFile(filepath.Join(dir, ".claude", "zprof-guard.py"))
	require.NoError(t, err)

	// Second apply: disabled. Files must remain byte-for-byte identical,
	// but the hooks + deny it previously added must be removed.
	proj := &manifest.GuardConfig{Enabled: boolPtr(false)}
	written, err := deployGuard(dir, base, GuardLayers{Project: proj})
	require.NoError(t, err)
	require.Empty(t, written)

	after, err := os.ReadFile(filepath.Join(dir, ".claude", "guard.json"))
	require.NoError(t, err)
	require.Equal(t, before, after, "guard.json must be left exactly as it was (ADR 0009 I6)")
	scriptAfter, err := os.ReadFile(filepath.Join(dir, ".claude", "zprof-guard.py"))
	require.NoError(t, err)
	require.Equal(t, scriptBefore, scriptAfter)

	settingsData, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	require.NoError(t, err)
	var settings map[string]any
	require.NoError(t, json.Unmarshal(settingsData, &settings))
	hooks, _ := settings["hooks"].(map[string]any)
	require.NotContains(t, hooks, "PreToolUse", "guard PreToolUse hook removed on disable")
	perms, _ := settings["permissions"].(map[string]any)
	if perms != nil {
		require.NotContains(t, perms, "deny", "guard-owned deny entries removed on disable")
	}
}

// TestDeployGuard_RepeatedApplyIsIdempotent fixes the AC10 idempotency
// requirement at the deployGuard (not just ensureGuardSettings) level:
// calling deployGuard repeatedly with unchanged inputs must not grow
// settings.local.json's guard hook entries or permissions.deny, and must
// re-render byte-identical guard.json/zprof-guard.py each time.
func TestDeployGuard_RepeatedApplyIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	base := testBase()
	proj := &manifest.GuardConfig{ExtraDenyBash: []string{"rm -rf /"}}
	layers := GuardLayers{Project: proj}

	var lastJSON, lastScript []byte
	for i := 0; i < 3; i++ {
		written, err := deployGuard(dir, base, layers)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{
			filepath.Join(dir, ".claude", "zprof-guard.py"),
			filepath.Join(dir, ".claude", "guard.json"),
		}, written, "call %d", i)

		jsonData, err := os.ReadFile(filepath.Join(dir, ".claude", "guard.json"))
		require.NoError(t, err)
		scriptData, err := os.ReadFile(filepath.Join(dir, ".claude", "zprof-guard.py"))
		require.NoError(t, err)
		if i > 0 {
			require.Equal(t, lastJSON, jsonData, "guard.json must be byte-stable across repeated identical apply (call %d)", i)
			require.Equal(t, lastScript, scriptData, "zprof-guard.py must be byte-stable across repeated identical apply (call %d)", i)
		}
		lastJSON, lastScript = jsonData, scriptData

		settingsData, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
		require.NoError(t, err)
		var settings map[string]any
		require.NoError(t, json.Unmarshal(settingsData, &settings))
		hooks := settings["hooks"].(map[string]any)
		require.Len(t, hooks["PreToolUse"].([]any), 1, "call %d: PreToolUse must not grow", i)
		require.Len(t, hooks["SubagentStop"].([]any), 1, "call %d: SubagentStop must not grow", i)

		perms, _ := settings["permissions"].(map[string]any)
		require.NotNil(t, perms, "call %d", i)
		deny := toStringSlice(perms["deny"])
		require.Equal(t, []string{"Bash(git push --force*)"}, deny, "call %d: permissions.deny must not grow", i)
	}
}

// TestDeployGuard_MergeRolesSubstitutionUsesProjectReplacedValue is the
// deployGuard-level integration check for ADR 0009 I3's "$merge_roles table
// is built from the already-project-replaced doc.MergeRoles" decision: a
// rule referencing $merge_roles must resolve to the project's wholesale
// replacement, never to the base/overlay concatenation that mergeGuard
// computed before the project layer ran.
func TestDeployGuard_MergeRolesSubstitutionUsesProjectReplacedValue(t *testing.T) {
	dir := t.TempDir()
	proj := &manifest.GuardConfig{MergeRoles: []string{"solo-role"}}

	_, err := deployGuard(dir, testBase(), GuardLayers{Project: proj})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "guard.json"))
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))

	require.Equal(t, []any{"solo-role"}, doc["merge_roles"])

	rules := doc["rules"].([]any)
	var found bool
	for _, r := range rules {
		rule := r.(map[string]any)
		if rule["id"] == "merge_role" {
			found = true
			require.Equal(t, []any{"solo-role"}, rule["not_roles"],
				"merge_role's $merge_roles substitution must use the project-replaced value, not base's [pr-shepherd]")
		}
	}
	require.True(t, found, "merge_role rule must be present")
}

// TestDeployGuard_ReadonlyRolesSubstitutionUsesOverlayMergedValue is the
// mirror check on the overlay-concatenation side: $readonly_roles must
// resolve to the base+overlay merged list (not just base's), exercised
// through the full deployGuard pipeline rather than resolveGuardRefs in
// isolation.
func TestDeployGuard_ReadonlyRolesSubstitutionUsesOverlayMergedValue(t *testing.T) {
	dir := t.TempDir()
	o := &overlay.Overlay{
		Manifest:    &manifest.OverlayManifest{Name: "fake-ios"},
		GuardSchema: []byte("readonly_roles: [architect]\n"),
	}

	_, err := deployGuard(dir, testBase(), GuardLayers{Overlays: []*overlay.Overlay{o}})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "guard.json"))
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	require.ElementsMatch(t, []any{"auditor", "architect"}, doc["readonly_roles"])

	rules := doc["rules"].([]any)
	var found bool
	for _, r := range rules {
		rule := r.(map[string]any)
		if rule["id"] == "readonly_mutation" {
			found = true
			require.ElementsMatch(t, []any{"auditor", "architect"}, rule["roles"],
				"readonly_mutation's $readonly_roles substitution must reflect the base+overlay merge")
		}
	}
	require.True(t, found, "readonly_mutation rule must be present")
}

func boolPtr(b bool) *bool { return &b }
