package agents

import "strings"

// Roles is the whitelist of agent basenames zprof treats as *roles* — the
// units a router hands a whole step to, as opposed to tool-agents, which a
// workflow calls from inside a step. Three subsystems have to agree on the
// distinction, which is why it lives here rather than in any one of them:
//
//   - apply renders the Consilium role→agent table from it,
//   - `zprof agents set` rejects unknown role names against it,
//   - doctor requires a `return_format` from roles specifically, because a
//     role's output is parsed as a schema by whoever dispatched it, while a
//     tool-agent's is consumed by the step that called it.
var Roles = map[string]bool{
	"planner":        true,
	"docs-writer":    true,
	"task-runner":    true,
	"architect":      true,
	"implementer":    true,
	"tester":         true,
	"bug-hunter":     true,
	"refactor-agent": true,
	"explorer":       true,
	"reviewer":       true,
}

// Gates are the base agents/gates/*.md that count as roles when
// --with-gates is set. They are never namespaced — base ships them once.
var Gates = map[string]bool{
	"north-star-auditor": true,
	"evidence-auditor":   true,
	"plan-reviewer":      true,
}

// IsRole reports whether name is a role or gate name, exactly as written.
func IsRole(name string) bool { return Roles[name] || Gates[name] }

// RoleOf maps an applied agent file's name back to the role it implements,
// or "" when the file is not a role. It accepts the plain name written by a
// single-overlay apply (`implementer`), the namespaced form written when
// several overlays are composed (`implementer-ios`, `reviewer-py`), and the
// `gates/` path prefix. Longest role wins, so `refactor-agent-ios` resolves
// to `refactor-agent` rather than to a shorter accidental prefix.
func RoleOf(name string) string {
	name = strings.TrimPrefix(name, "gates/")
	if IsRole(name) {
		return name
	}
	best := ""
	for role := range Roles {
		if strings.HasPrefix(name, role+"-") && len(role) > len(best) {
			best = role
		}
	}
	return best
}
