package apply

import (
	"sort"
	"strings"

	"github.com/vaporphd/zprof/internal/agents"
	"github.com/vaporphd/zprof/internal/overlay"
)

// IsKnownRole returns true if role is a valid role or gate name — used by
// commands like `zprof agents set` to reject typos before they persist in
// .zprof.yaml where they'd silently misconfigure the next apply.
//
// The role/tool distinction itself lives in internal/agents so doctor can
// apply the same rule without importing this package.
func IsKnownRole(role string) bool { return agents.IsRole(role) }

// roleAgents / gateRoles alias the shared definitions; the Consilium table
// below reads them on every row.
var (
	roleAgents = agents.Roles
	gateRoles  = agents.Gates
)

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
		// No `executing:` map and no implementer to fall back on: this
		// overlay has no file owner at all (process-only overlays like
		// issue-loop-github-strict, read-only ones like re-macho). Emit
		// nothing. Naming `task-runner` here would contradict its own
		// prompt — it does not write code — and the runner reads this very
		// table to learn who owns what.
		if _, ok := o.Agents["implementer"]; !ok {
			continue
		}
		agent := "implementer"
		if multi {
			agent = overlay.NamespaceAgent("implementer", o.Manifest.Name)
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

// buildStopListBlock renders the "## Stop list" section for CLAUDE.md: the
// base entries followed by each active overlay's, deduplicated, every row
// tagged with the source that declared it. The list binds both actors — the
// runner returns verdict=blocked instead of performing one, and the main
// session (which owns git operations, and the list contains force-push and
// branch/tag deletion) asks the user first. Rendered into CLAUDE.md on
// purpose — a policy the user cannot read is a hidden policy.
func buildStopListBlock(opts ApplyOpts) string {
	var b strings.Builder
	b.WriteString("## Stop list\n\n")
	b.WriteString("Список связывает **обоих** акторов. `task-runner` не выполняет перечисленное и не поручает — возвращает `verdict: blocked` с вопросом. Main-сессия перечисленное не делает своими руками (в списке есть git-операции, которые иначе принадлежат ей) — сначала спрашивает пользователя. Решение в обоих случаях принимает человек.\n\n")
	b.WriteString("| Действие | Источник |\n")
	b.WriteString("|---|---|\n")

	seen := map[string]bool{}
	appendRows := func(entries []string, source string) {
		for _, e := range entries {
			if e == "" || seen[e] {
				continue
			}
			seen[e] = true
			b.WriteString("| " + e + " | " + source + " |\n")
		}
	}

	if opts.Base != nil && opts.Base.Manifest != nil {
		appendRows(opts.Base.Manifest.StopList, "base")
	}
	for _, o := range opts.Overlays {
		if o == nil || o.Manifest == nil {
			continue
		}
		appendRows(o.Manifest.StopList, o.Manifest.Name)
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
