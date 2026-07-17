package apply

import (
	"sort"
	"strings"

	"github.com/vaporphd/zprof/internal/overlay"
)

// IsKnownRole returns true if role is a valid role or gate name — used by
// commands like `zprof agents set` to reject typos before they persist in
// .zprof.yaml where they'd silently misconfigure the next apply.
func IsKnownRole(role string) bool {
	return roleAgents[role] || gateRoles[role]
}

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
// present in this apply. A companion "### Tool Agents" section lists
// each overlay's tool-agents (from manifest.ToolAgents) so the user can
// see the full dispatchable inventory without diffing .claude/agents/.
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

	toolRows := ""
	// Base ships tool-agents too (evaluator lives in base since it doesn't
	// need overlay-specific knowledge). Render those first so the base
	// tools appear at the top of the companion table.
	if opts.Base != nil && opts.Base.Manifest != nil {
		for _, name := range opts.Base.Manifest.ToolAgents {
			if _, ok := opts.Base.Agents[name]; !ok {
				continue
			}
			toolRows += "| " + name + " | " + name + " | base |\n"
		}
	}
	for _, o := range opts.Overlays {
		if o == nil || o.Manifest == nil {
			continue
		}
		for _, name := range o.Manifest.ToolAgents {
			if _, ok := o.Agents[name]; !ok {
				continue
			}
			agent := name
			if multi {
				agent = overlay.NamespaceAgent(name, o.Manifest.Name)
			}
			toolRows += "| " + name + " | " + agent + " | " + o.Manifest.Name + " |\n"
		}
	}
	if toolRows != "" {
		b.WriteString("\n### Tool Agents\n\n")
		b.WriteString("Dispatched from within a workflow rather than the top-level router. Present in `.claude/agents/` and callable by name.\n\n")
		b.WriteString("| Tool | Agent | Source |\n")
		b.WriteString("|---|---|---|\n")
		b.WriteString(toolRows)
	}

	return strings.TrimRight(b.String(), "\n")
}

// buildExecutingTable auto-generates the "## Executing" markdown table
// (agent -> file scope). Preferred source: overlay manifest's `executing:`
// map, which lets each overlay declare exactly which agents own which
// paths. Fallback (for overlays that don't declare `executing:`): map the
// overlay's implementer to its detect.yaml file globs — imprecise, since
// detect globs are for detection, not ownership, but preserved for
// backward compatibility with older manifests.
func buildExecutingTable(opts ApplyOpts) string {
	multi := len(opts.Overlays) > 1

	var b strings.Builder
	b.WriteString("## Executing\n\n")
	b.WriteString("| Agent | Scope |\n")
	b.WriteString("|---|---|\n")

	for _, o := range opts.Overlays {
		if o == nil || o.Manifest == nil {
			continue
		}
		if len(o.Manifest.Executing) > 0 {
			for _, agentName := range sortedMapKeys(o.Manifest.Executing) {
				scope := o.Manifest.Executing[agentName]
				agent := agentName
				if multi {
					agent = overlay.NamespaceAgent(agentName, o.Manifest.Name)
				}
				b.WriteString("| " + agent + " | " + scope + " |\n")
			}
			continue
		}
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

func sortedMapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
