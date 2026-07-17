package eval

import (
	"os"
	"regexp"
	"sort"
	"strings"
)

// roleGuessRe teaches the scorer how to derive a "role" bucket from an
// Agent tool call's Description (which is the label the orchestrator
// chose at dispatch time — "Architect run 2", "Dispatch architect on
// mood feature", "Reviewer — MoodJournalInterface"). We accept a known
// role token anywhere in the description matched as a word — the first
// match wins. Non-matches bucket to "other" — useful signal in itself.
var roleGuessRe = regexp.MustCompile(`(?i)\b(architect|implementer|tester|reviewer|bug[- ]?hunter|refactor(?:-agent)?|explorer|planner|dev[- ]?orchestrator|exploratory[- ]?orchestrator|docs[- ]?writer|xcodegen[- ]?driver|xcode[- ]?runner|spm[- ]?manager|swiftlint[- ]?checker|simulator[- ]?driver|testflight[- ]?shipper|evaluator)\b`)

// GuessRole extracts the role bucket for a dispatch. Descriptions are
// author-chosen — we look for the first known role token anywhere in the
// description, matched as a word. Non-matches bucket to "other".
func GuessRole(description string) string {
	m := roleGuessRe.FindStringSubmatch(description)
	if len(m) < 2 {
		return "other"
	}
	role := strings.ToLower(m[1])
	role = strings.ReplaceAll(role, " ", "-")
	switch role {
	case "refactor":
		return "refactor-agent"
	case "bughunter":
		return "bug-hunter"
	}
	return role
}

// RoleStats is the deterministic scorecard for one role, aggregated over
// every dispatch attributed to that role in a session.
type RoleStats struct {
	Role             string
	Model            string // most recent model seen for this role
	Dispatches       int
	Completed        int
	PassAt1          float64 // ratio, [0.0, 1.0]
	MedianTokens     int
	TotalTokens      int
	ApT              float64 // per OckBench: passed * 1e5 / total_output_tokens
	ArtifactExists   int     // rows where the artifact claim was writing-verified
	ArtifactMissing  int     // artifact claimed but not found on disk
	HadPreamble      int
	NextReachable    int
	NextUnreachable  int
	AvgConfidence    float64 // averaged over dispatches that reported it
	ConfidenceCount  int     // how many dispatches self-reported confidence
	AvgDurationMs    int64
}

// SessionScore is the top-level Tier-1 scorecard.
type SessionScore struct {
	Meta       SessionMeta
	Roles      []RoleStats
	Violations []Violation
}

// Violation is a discrete contract-adherence issue Tier 1 surfaces without
// LLM help. Kind is one of: "artifact-missing", "return-preamble",
// "next-unreachable", "dispatch-never-returned".
type Violation struct {
	DispatchID string
	Role       string
	AgentName  string
	Kind       string
	Detail     string
}

// knownRoles are the role tokens we accept as `next:` targets. Anything else
// is flagged as "next-unreachable" — including free-form typos.
var knownRoles = map[string]bool{
	"architect": true, "implementer": true, "tester": true, "reviewer": true,
	"bug-hunter": true, "refactor-agent": true, "explorer": true, "planner": true,
	"dev-orchestrator": true, "exploratory-orchestrator": true, "docs-writer": true,
	"xcodegen-driver": true, "xcode-runner": true, "spm-manager": true,
	"swiftlint-checker": true, "simulator-driver": true, "testflight-shipper": true,
	"evaluator": true,
	// Explicit sentinel: null means the loop stops here — valid.
	"null": true, "none": true, "": true,
}

// Score runs every deterministic check and produces the Tier-1 scorecard.
// checkArtifactExists is exposed as a parameter so tests can swap in a
// fake — real code passes fsArtifactExists. The scorer never opens files
// it wasn't asked to.
func Score(t *Trace, checkArtifactExists func(string) bool) SessionScore {
	if checkArtifactExists == nil {
		checkArtifactExists = fsArtifactExists
	}
	perRole := map[string]*RoleStats{}
	var violations []Violation

	for _, d := range t.Dispatches {
		role := GuessRole(d.AgentName)
		stats, ok := perRole[role]
		if !ok {
			stats = &RoleStats{Role: role}
			perRole[role] = stats
		}
		stats.Dispatches++
		stats.TotalTokens += d.SubagentTokens
		stats.AvgDurationMs += d.DurationMs
		if d.Model != "" {
			stats.Model = d.Model
		}
		if d.Returned.Confidence > 0 {
			stats.AvgConfidence += d.Returned.Confidence
			stats.ConfidenceCount++
		}
		completed := d.Status == "completed"
		if completed {
			stats.Completed++
		}
		if !completed {
			violations = append(violations, Violation{
				DispatchID: d.ID, Role: role, AgentName: d.AgentName,
				Kind:   "dispatch-never-returned",
				Detail: "no task-notification recorded in this session",
			})
			continue
		}
		if isPass(d.Returned.Verdict) {
			stats.PassAt1++
		}

		if d.Returned.Artifact != "" && d.Returned.Artifact != "none" {
			exists := checkArtifactExists(d.Returned.Artifact)
			if exists {
				stats.ArtifactExists++
			} else {
				stats.ArtifactMissing++
				violations = append(violations, Violation{
					DispatchID: d.ID, Role: role, AgentName: d.AgentName,
					Kind:   "artifact-missing",
					Detail: "claimed artifact not found on disk: " + d.Returned.Artifact,
				})
			}
		}

		if d.Returned.RawFirstLine != "" && !strings.HasPrefix(d.Returned.RawFirstLine, "verdict:") {
			stats.HadPreamble++
			violations = append(violations, Violation{
				DispatchID: d.ID, Role: role, AgentName: d.AgentName,
				Kind:   "return-preamble",
				Detail: "first line was not `verdict:` — got: " + truncate(d.Returned.RawFirstLine, 80),
			})
		}

		nx := strings.ToLower(strings.TrimSpace(d.Returned.Next))
		// Trim trailing comments / conditions like "implementer | planner | null"
		if idx := strings.IndexAny(nx, " |,"); idx > 0 {
			nx = strings.TrimSpace(nx[:idx])
		}
		if knownRoles[nx] {
			stats.NextReachable++
		} else {
			stats.NextUnreachable++
			violations = append(violations, Violation{
				DispatchID: d.ID, Role: role, AgentName: d.AgentName,
				Kind:   "next-unreachable",
				Detail: "next field names an unknown role: " + d.Returned.Next,
			})
		}
	}

	// Finalize per-role averages after the pass.
	var roles []string
	for r := range perRole {
		roles = append(roles, r)
	}
	sort.Strings(roles)
	out := make([]RoleStats, 0, len(roles))
	for _, r := range roles {
		s := perRole[r]
		if s.Completed > 0 {
			s.PassAt1 = s.PassAt1 / float64(s.Completed)
		}
		if s.TotalTokens > 0 && s.PassAt1 > 0 {
			// ApT — Accuracy per Token, scaled to a readable range.
			// pass_count × 1e5 / total_tokens is the OckBench form.
			s.ApT = float64(int(s.PassAt1*float64(s.Completed))) * 1e5 / float64(s.TotalTokens)
		}
		if s.Dispatches > 0 {
			s.AvgDurationMs = s.AvgDurationMs / int64(s.Dispatches)
		}
		if s.ConfidenceCount > 0 {
			s.AvgConfidence = s.AvgConfidence / float64(s.ConfidenceCount)
		}
		s.MedianTokens = medianTokens(t.Dispatches, r)
		out = append(out, *s)
	}
	return SessionScore{Meta: t.Session, Roles: out, Violations: violations}
}

func isPass(verdict string) bool {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case "done", "approve", "approve-with-fixes", "ok":
		return true
	default:
		return false
	}
}

func fsArtifactExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func medianTokens(all []Dispatch, role string) int {
	var vals []int
	for _, d := range all {
		if GuessRole(d.AgentName) != role || d.SubagentTokens == 0 {
			continue
		}
		vals = append(vals, d.SubagentTokens)
	}
	if len(vals) == 0 {
		return 0
	}
	sort.Ints(vals)
	return vals[len(vals)/2]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
