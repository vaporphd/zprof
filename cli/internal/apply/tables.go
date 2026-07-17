package apply

import (
	"sort"
	"strings"

	"github.com/vaporphd/zprof/internal/overlay"
)

// roleAgents is the whitelist of agent basenames considered "roles" for the
// Consilium (role -> agent) table. Everything else — tool-agents dispatched
// from within a workflow rather than from the top-level router — is skipped.
var roleAgents = map[string]bool{
	"planner":                  true,
	"docs-writer":              true,
	"dev-orchestrator":         true,
	"exploratory-orchestrator": true,
	"architect":                true,
	"implementer":              true,
	"tester":                   true,
	"bug-hunter":               true,
	"refactor-agent":           true,
	"explorer":                 true,
	"reviewer":                 true,
}

// gateRoles are the gates/*.md base agents that count as roles when
// --with-gates is set.
var gateRoles = map[string]bool{
	"north-star-auditor": true,
	"evidence-auditor":   true,
	"plan-reviewer":      true,
}

// buildConsiliumTable auto-generates the "## Consilium" markdown table
// (role -> agent -> source) from the base and overlay agents actually
// present in this apply.
func buildConsiliumTable(opts ApplyOpts) string {
	multi := len(opts.Overlays) > 1

	var b strings.Builder
	b.WriteString("## Consilium\n\n")
	b.WriteString("| Role | Agent | Source |\n")
	b.WriteString("|---|---|---|\n")

	baseNames := sortedKeys(opts.Base.Agents)
	for _, name := range baseNames {
		isGate := strings.HasPrefix(name, "gates/")
		role := strings.TrimPrefix(name, "gates/")
		if isGate {
			if !opts.Project.WithGates || !gateRoles[role] {
				continue
			}
		} else if !roleAgents[role] {
			continue
		}
		b.WriteString("| " + role + " | " + name + " | base |\n")
	}

	for _, o := range opts.Overlays {
		for _, name := range sortedKeys(o.Agents) {
			if !roleAgents[name] {
				continue
			}
			agent := name
			if multi {
				agent = overlay.NamespaceAgent(name, o.Manifest.Name)
			}
			b.WriteString("| " + name + " | " + agent + " | " + o.Manifest.Name + " |\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// buildExecutingTable auto-generates the "## Executing" markdown table
// (agent -> file scope) from each overlay's detect.yaml globs, mapping to
// the overlay's implementer agent (falling back to dev-orchestrator if the
// overlay ships no implementer).
func buildExecutingTable(opts ApplyOpts) string {
	multi := len(opts.Overlays) > 1

	var b strings.Builder
	b.WriteString("## Executing\n\n")
	b.WriteString("| Agent | Scope |\n")
	b.WriteString("|---|---|\n")

	for _, o := range opts.Overlays {
		agent := "dev-orchestrator"
		if _, ok := o.Agents["implementer"]; ok {
			agent = "implementer"
			if multi {
				agent = overlay.NamespaceAgent("implementer", o.Manifest.Name)
			}
		}
		globs := "-"
		if o.Detect != nil {
			if files := o.Detect.AnyFileList(); len(files) > 0 {
				globs = strings.Join(files, ", ")
			}
		}
		b.WriteString("| " + agent + " | " + globs + " |\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
