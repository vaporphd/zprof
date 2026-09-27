package verdicts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const minimalRegistry = `
version: 1
base_enum: [done, blocked, failed]
actions: [next, loop, insert, triage, escalate, abort]
universal:
  blocked: {base: blocked, action: triage}
quotes:
  task-runner: "*"
  auditor: [done]
templates:
  std: &std
    done:   {base: done,   action: next}
    failed: {base: failed, action: abort}
roles:
  implementer: *std
  reviewer:
    approve: {base: done,   action: next}
    block:   {base: failed, action: "loop:implementer"}
  pr-shepherd:
    blocked-external: {base: blocked, action: escalate}
    blocked-*:        {base: blocked, action: triage}
  auditor:
    complete: {base: done,    action: next}
    blocked:  {base: blocked, action: escalate}
`

func mustParse(t *testing.T, data string) *Registry {
	t.Helper()
	r, err := Parse([]byte(data))
	require.NoError(t, err)
	return r
}

func TestParse_ResolvesAnchorsAndQuoteScalar(t *testing.T) {
	r := mustParse(t, minimalRegistry)
	require.Equal(t, 1, r.Version)
	require.Equal(t, Spec{Base: "done", Action: "next"}, r.Roles["implementer"]["done"])
	require.Equal(t, Spec{Base: "failed", Action: "abort"}, r.Roles["implementer"]["failed"])
	require.Equal(t, []string{"*"}, r.Quotes["task-runner"])
	require.Equal(t, []string{"done"}, r.Quotes["auditor"])
}

func TestParse_UnknownTopLevelKeyErrors(t *testing.T) {
	_, err := Parse([]byte("version: 1\nrole:\n  implementer: {}\n"))
	require.Error(t, err)
}

func TestValidate_RejectsUnknownBase(t *testing.T) {
	bad := `
version: 1
base_enum: [done, blocked, failed]
actions: [next]
roles:
  implementer:
    done: {base: bogus, action: next}
`
	r := mustParse(t, bad)
	require.Error(t, r.Validate())
}

func TestValidate_RejectsUnknownAction(t *testing.T) {
	bad := `
version: 1
base_enum: [done]
actions: [next]
roles:
  implementer:
    done: {base: done, action: bogus}
`
	r := mustParse(t, bad)
	require.Error(t, r.Validate())
}

func TestValidate_RejectsLoopTargetOutsideRoles(t *testing.T) {
	bad := `
version: 1
base_enum: [done, failed]
actions: [next, loop]
roles:
  tester:
    failed: {base: failed, action: "loop:nonexistent-role"}
`
	r := mustParse(t, bad)
	require.Error(t, r.Validate())
}

func TestValidate_AcceptsAtNextAndAtAudited(t *testing.T) {
	ok := `
version: 1
base_enum: [done, failed]
actions: [next, loop]
roles:
  auditor:
    incomplete: {base: failed, action: "loop:@audited"}
  pr-shepherd:
    preflight-failed: {base: failed, action: "loop:@next"}
`
	r := mustParse(t, ok)
	require.NoError(t, r.Validate())
}

func TestValidate_RejectsUnknownQuoteToken(t *testing.T) {
	bad := `
version: 1
base_enum: [done]
actions: [next]
quotes:
  auditor: [nonexistent-token]
roles:
  implementer:
    done: {base: done, action: next}
`
	r := mustParse(t, bad)
	require.Error(t, r.Validate())
}

func TestValidate_AcceptsRealRegistry(t *testing.T) {
	r := mustParse(t, minimalRegistry)
	require.NoError(t, r.Validate())
}

func TestLookup_ExactAndLongestPrefixAndGatesPrefix(t *testing.T) {
	r := &Registry{Roles: map[string]map[string]Spec{
		"implementer":  {},
		"auditor":      {},
		"auditor-deep": {},
	}}
	role, ok := r.Lookup("implementer")
	require.True(t, ok)
	require.Equal(t, "implementer", role)

	role, ok = r.Lookup("implementer-ios")
	require.True(t, ok)
	require.Equal(t, "implementer", role)

	// Longest role prefix wins: auditor-deep, not auditor.
	role, ok = r.Lookup("auditor-deep-ios")
	require.True(t, ok)
	require.Equal(t, "auditor-deep", role)

	role, ok = r.Lookup("gates/plan-reviewer")
	require.False(t, ok) // plan-reviewer isn't in this minimal Roles map

	r.Roles["plan-reviewer"] = map[string]Spec{}
	role, ok = r.Lookup("gates/plan-reviewer")
	require.True(t, ok)
	require.Equal(t, "plan-reviewer", role)

	_, ok = r.Lookup("some-custom-user-agent")
	require.False(t, ok)
}

func TestAllows_ExactPatternAndUniversal(t *testing.T) {
	r := mustParse(t, minimalRegistry)

	require.True(t, r.Allows("implementer", "done"))
	require.True(t, r.Allows("implementer", "blocked"), "universal token allowed on any role")
	require.False(t, r.Allows("implementer", "nonsense"))

	// Exact pattern match: registry literally has "blocked-*" for pr-shepherd.
	require.True(t, r.Allows("pr-shepherd", "blocked-external"))
	require.True(t, r.Allows("pr-shepherd", "blocked-*"), "pattern-shaped token must match the registry's pattern key verbatim")

	// Runtime concrete token matches the longest x-* pattern.
	require.True(t, r.Allows("pr-shepherd", "blocked-ci-pending"))

	// auditor explicitly overrides universal's blocked.
	require.True(t, r.Allows("auditor", "blocked"))

	require.True(t, r.Allows("anything", "*"), "whole-value wildcard always matches")
}

func TestNormalized_MergesUniversalRoleWins(t *testing.T) {
	r := mustParse(t, minimalRegistry)
	n := r.Normalized()
	require.Equal(t, 1, n.Version)
	require.Equal(t, []string{"done", "blocked", "failed"}, n.BaseEnum)

	// implementer has no explicit "blocked" — merged in from universal.
	require.Equal(t, Spec{Base: "blocked", Action: "triage"}, n.Roles["implementer"]["blocked"])
	// auditor overrides universal's blocked with its own escalate action.
	require.Equal(t, Spec{Base: "blocked", Action: "escalate"}, n.Roles["auditor"]["blocked"])
}
