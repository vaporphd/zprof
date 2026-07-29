// Package agents holds facts about zprof's agent roster that more than one
// subsystem needs to agree on.
package agents

// Retired lists agent names zprof shipped in earlier versions and has since
// removed. They were never user-authored, so `apply` prunes them even in
// projects applied before `managed_agents` existed — there the tracking
// list is empty and the general orphan rule can never fire, which would
// leave a working dev-orchestrator behind for main to dispatch, preserving
// the exact leak this change closes. `doctor` reports any that survive.
var Retired = []string{"dev-orchestrator", "exploratory-orchestrator"}
