package score

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vaporphd/zprof/internal/stats"
)

// metric is the raw outcome of one P-check before weights are applied.
type metric struct {
	value  float64            // compared against Saturation[id]
	byRole map[string]float64 // numerator share per role (errors, retries, tokens, …)
	detail string             // human text for the card (RU)
	guard  int                // P7 only: guard-events.jsonl deny/block rows counted (ADR-0008 H3)
}

func newMetric() metric { return metric{byRole: map[string]float64{}} }

// Penalty is one scored P-check.
type Penalty struct {
	ID     string             `json:"id"`
	Value  float64            `json:"value"`
	Points float64            `json:"points"`
	ByRole map[string]float64 `json:"by_role"`
	Detail string             `json:"detail,omitempty"`
	// GuardDenies is the count of guard-events.jsonl deny/block rows folded
	// into this penalty (P7 only, ADR-0008 H5). Zero on every other penalty
	// and omitted from scores.jsonl when zero.
	GuardDenies int `json:"guard_denies,omitempty"`
}

// penaltyFrom applies weight × min(1, value/saturation) and splits the points
// across roles proportionally to their numerator share.
func penaltyFrom(id string, m metric, cfg Config) Penalty {
	p := Penalty{ID: id, Value: m.value, ByRole: map[string]float64{}, Detail: m.detail, GuardDenies: m.guard}
	sat := cfg.Saturation[id]
	if sat <= 0 || m.value <= 0 {
		return p
	}
	frac := m.value / sat
	if frac > 1 {
		frac = 1
	}
	p.Points = cfg.Weights[id] * frac
	total := 0.0
	for _, v := range m.byRole {
		total += v
	}
	if total > 0 {
		for r, v := range m.byRole {
			p.ByRole[r] = p.Points * v / total
		}
	}
	return p
}

func roleIndex(run Run) map[string]string {
	m := make(map[string]string, len(run.Dispatches))
	for _, d := range run.Dispatches {
		m[d.DispatchID] = d.Role
	}
	return m
}

// isMutating: the collector's flag (computed on the full command) wins;
// rows without it fall back to matching the truncated Target.
func isMutating(e ToolEvent, cfg Config) bool {
	if cfg.MutatingTools[e.Tool] {
		return true
	}
	if e.Mutating != nil {
		return *e.Mutating
	}
	return e.Tool == "Bash" && cfg.IsMutatingBash(e.Target)
}

func isErr(e ToolEvent) bool { return e.IsError != nil && *e.IsError }

func tokens(d stats.Dispatch) int {
	return d.TokensInput + d.TokensOutput + d.TokensCacheRead + d.TokensCacheCreation
}

// P1 — tool error rate: errored / events that got a result.
func computeP1(run Run, cfg Config) metric {
	m := newMetric()
	roles := roleIndex(run)
	total, errs := 0, 0
	for id, evs := range run.Events {
		for _, e := range evs {
			if e.IsError == nil {
				continue
			}
			total++
			if *e.IsError {
				errs++
				m.byRole[roles[id]]++
			}
		}
	}
	if total > 0 {
		m.value = float64(errs) / float64(total)
	}
	m.detail = fmt.Sprintf("%d/%d tool errors (%.0f%%)", errs, total, m.value*100)
	return m
}

// P2 — blind retries: same (tool, input_hash) repeated after an error with no
// mutating event in between. Order per event: exempt-skip, then check, then
// reset-if-mutating, then record.
//
// sleep/wait Bash commands (cfg.IsP2Exempt) are skipped entirely — they
// never enter the lastErr/targets bookkeeping at all, in either direction:
// a timed-out `sleep 180` neither counts as a repeat of a prior one nor
// primes lastErr for a later, unrelated command sharing its key. These are
// async-child wait loops (task-runner.md "Ожидание async-ребёнка"), not
// blind retries of a failed action — a `sleep` erroring out on the Bash
// tool's own 120s default timeout is timeout hygiene, not a bug (issue
// #53).
func computeP2(run Run, cfg Config) metric {
	m := newMetric()
	roles := roleIndex(run)
	targets := map[string]int{}
	for id, evs := range run.Events {
		lastErr := map[string]bool{}
		for _, e := range evs {
			if cfg.IsP2Exempt(e.Tool, e.Target) {
				continue
			}
			key := e.Tool + "|" + e.InputHash
			if lastErr[key] {
				m.value++
				m.byRole[roles[id]]++
				targets[e.Target]++
			}
			if isMutating(e, cfg) {
				lastErr = map[string]bool{}
			}
			lastErr[key] = isErr(e)
		}
	}
	if m.value > 0 {
		top := topKey(targets)
		// Count of the *specific* top target, not the run-wide m.value —
		// with several distinct repeated targets the two diverge (e.g. a
		// card previously read "15× `sleep 180`" when only 12 of the 15
		// total repeats were that exact command; the other 3 were `sleep
		// 120`). Fixed alongside the #53 exemption while this function was
		// already open.
		m.detail = fmt.Sprintf("%s: %d× `%s` без правок между", topRole(m.byRole), targets[top], truncate(top, 40))
	}
	return m
}

// P3 — re-reads: Read of a path already read in this dispatch with no mutating
// event since. Only dispatches with ≥ 4 Reads count (noise floor).
func computeP3(run Run, cfg Config) metric {
	m := newMetric()
	roles := roleIndex(run)
	sumReads, sumRereads := 0, 0
	for id, evs := range run.Events {
		reads, rereads := 0, 0
		seen := map[string]bool{}
		for _, e := range evs {
			if isMutating(e, cfg) {
				seen = map[string]bool{}
				continue
			}
			if e.Tool != "Read" {
				continue
			}
			reads++
			if seen[e.Target] {
				rereads++
			}
			seen[e.Target] = true
		}
		if reads >= 4 {
			sumReads += reads
			sumRereads += rereads
			if rereads > 0 {
				m.byRole[roles[id]] += float64(rereads)
			}
		}
	}
	if sumReads > 0 {
		m.value = float64(sumRereads) / float64(sumReads)
	}
	if sumRereads > 0 {
		m.detail = fmt.Sprintf("%s: %d перечитываний из %d Read", topRole(m.byRole), sumRereads, sumReads)
	}
	return m
}

// P4 — tester `failed` immediately followed by an implementer step.
func computeP4(run Run, cfg Config) metric {
	m := newMetric()
	for i := 0; i+1 < len(run.Steps); i++ {
		s := run.Steps[i]
		if s.Role == "tester" && s.Verdict == "failed" && run.Steps[i+1].Role == "implementer" {
			m.value++
		}
	}
	if m.value > 0 {
		m.byRole["implementer"] = m.value
		m.detail = fmt.Sprintf("tester→implementer: %d лишн. раунд(ов)", int(m.value))
	}
	return m
}

// P5 — reviewer verdicts listed in review_block_verdicts (block, changes-requested, …).
func computeP5(run Run, cfg Config) metric {
	m := newMetric()
	for _, s := range run.Steps {
		if s.Role == "reviewer" && cfg.ReviewBlockVerdicts[s.Verdict] {
			m.value++
		}
	}
	if m.value > 0 {
		m.byRole["implementer"] = m.value
		m.detail = fmt.Sprintf("reviewer block ×%d", int(m.value))
	}
	return m
}

// P6 — share of run tokens spent in dispatches that produced nothing:
// status failed/killed, or an unparsable return (exempt roles excluded).
func computeP6(run Run, cfg Config) metric {
	m := newMetric()
	total, wasted := 0, 0
	for _, d := range run.Dispatches {
		tk := tokens(d)
		total += tk
		failed := d.Status == "failed" || d.Status == "killed"
		unparsed := d.ReturnParsed != nil && !*d.ReturnParsed && !cfg.ExemptRoles[d.Role]
		if failed || unparsed {
			wasted += tk
			m.byRole[d.Role] += float64(tk)
		}
	}
	if total > 0 {
		m.value = float64(wasted) / float64(total)
	}
	if wasted > 0 {
		m.detail = fmt.Sprintf("%.0f%% токенов в dispatch'ах без результата (%s)", m.value*100, joinRoles(m.byRole))
	}
	return m
}

// P7 — Class-A contract violations, exempt roles excluded; a missing artifact
// on a `blocked` verdict is not a violation. Guard-events.jsonl deny/block
// rows (guard spec §7) add one violation each in a separate loop, attributed
// by the event's own Role — cfg.ExemptRoles does not apply to this
// slagaemое at all (ADR-0008 H3): a deny on an exempt role's mutation still
// counts.
func computeP7(run Run, cfg Config) metric {
	m := newMetric()
	for _, d := range run.Dispatches {
		if cfg.ExemptRoles[d.Role] {
			continue
		}
		n := 0
		if d.HasPreamble != nil && *d.HasPreamble {
			n++
		}
		if d.ArtifactExists != nil && !*d.ArtifactExists && d.Verdict != "blocked" {
			n++
		}
		if d.NextIsReachable != nil && !*d.NextIsReachable {
			n++
		}
		if d.ReturnParsed != nil && !*d.ReturnParsed {
			n++
		}
		if n > 0 {
			m.value += float64(n)
			m.byRole[d.Role] += float64(n)
		}
	}
	for _, e := range run.GuardEvents {
		if e.Decision != "deny" && e.Decision != "block" {
			continue
		}
		m.value++
		m.byRole[e.Role]++
		m.guard++
	}
	if m.value > 0 {
		m.detail = fmt.Sprintf("%d нарушений контракта (%s)", int(m.value), joinRoles(m.byRole))
		if m.guard > 0 {
			m.detail += fmt.Sprintf(" (guard: %d deny)", m.guard)
		}
	}
	return m
}

// busyPollThreshold: a streak of this many consecutive read-only Bash calls
// (no sleep/wait, no mutation between them — cfg.IsP2Exempt / isMutating)
// is still "one combined status check" territory (task-runner.md "Ожидание
// async-ребёнка" explicitly allows one command like `git log -1 --oneline
// && git status --porcelain`, i.e. one Bash call, but a human occasionally
// splits that into 2-3 quick calls without it being a real busy-loop). The
// 4th call onward in the same unbroken streak is scored.
const busyPollThreshold = 3

// computeBusyPoll — signal (not a P1-P7 penalty, no weight/saturation, does
// not affect Score): consecutive read-only Bash calls issued back-to-back
// with no `sleep`/`wait` (cfg.IsP2Exempt) and no mutating command between
// them. This is the busy-poll pattern task-runner.md now forbids in
// "Ожидание async-ребёнка" — checking an async child's status by spamming
// `date`/`git log`/`git diff` instead of sleeping between checks (issue
// #53 AC5). None of P1-P7 catch it: these calls are not errors (P1), not
// retries of a failed command (P2 — is_error need not be true here at
// all), and not Reads (P3).
//
// Deliberately not gated on is_error like P2 — the whole point of this
// signal is successful, well-formed status checks fired too rapidly, which
// P1/P2 have no way to see at all.
func computeBusyPoll(run Run, cfg Config) metric {
	m := newMetric()
	roles := roleIndex(run)
	maxStreak := 0
	var maxStreakRole, maxStreakTarget string
	for id, evs := range run.Events {
		streak := 0
		for _, e := range evs {
			// isMutating first and unconditionally: an Edit/Write between
			// two Bash checks is real work and must reset the streak even
			// though it isn't itself a Bash event (the opposite ordering
			// let Edit slide past silently and inflated the streak across
			// it — caught by testdata/run1's build-fix-retest loop, which
			// must NOT read as busy-poll).
			if isMutating(e, cfg) {
				streak = 0
				continue
			}
			if e.Tool != "Bash" {
				continue // non-mutating, non-Bash (Read, Grep, ...) neither extends nor breaks the Bash streak
			}
			if cfg.IsP2Exempt(e.Tool, e.Target) {
				streak = 0
				continue
			}
			streak++
			if streak > busyPollThreshold {
				m.value++
				m.byRole[roles[id]]++
			}
			if streak > maxStreak {
				maxStreak = streak
				maxStreakRole = roles[id]
				maxStreakTarget = e.Target
			}
		}
	}
	if m.value > 0 {
		m.detail = fmt.Sprintf("%s: %d Bash подряд без sleep/правок между (посл. `%s`)",
			maxStreakRole, maxStreak, truncate(maxStreakTarget, 40))
	}
	return m
}

func topRole(byRole map[string]float64) string { return topKey(toIntMap(byRole)) }

func toIntMap(m map[string]float64) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = int(v)
	}
	return out
}

func topKey(counts map[string]int) string {
	best, bestN := "", -1
	for k, n := range counts {
		if n > bestN || (n == bestN && k < best) {
			best, bestN = k, n
		}
	}
	return best
}

func joinRoles(byRole map[string]float64) string {
	keys := make([]string, 0, len(byRole))
	for k := range byRole {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// truncate cuts s to at most n runes, marking the cut with "…".
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
