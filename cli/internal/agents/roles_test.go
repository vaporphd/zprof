package agents

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoleOfPlainAndNamespaced(t *testing.T) {
	for name, want := range map[string]string{
		"implementer":         "implementer",
		"implementer-ios":     "implementer",
		"reviewer-py":         "reviewer",
		"task-runner":         "task-runner",
		"gates/plan-reviewer": "plan-reviewer",
		// Longest role wins: `refactor-agent-ios` is the refactor-agent role,
		// not some shorter accidental prefix.
		"refactor-agent-ios": "refactor-agent",
		// Tool-agents and user-authored files are not roles.
		"xcode-runner":  "",
		"pr-shepherd":   "",
		"my-own-helper": "",
		// A gate is only a role under its own name, never namespaced.
		"plan-reviewer": "plan-reviewer",
	} {
		require.Equal(t, want, RoleOf(name), "RoleOf(%q)", name)
	}
}

func TestIsRoleRejectsRetiredOrchestrators(t *testing.T) {
	require.True(t, IsRole("task-runner"))
	for _, n := range Retired {
		require.False(t, IsRole(n), "%s is retired and must not count as a role", n)
	}
}
