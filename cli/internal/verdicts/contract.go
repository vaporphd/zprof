package verdicts

import (
	"fmt"
	"regexp"
	"strings"
)

// Contract is the parsed first content line of an agent's return_format
// frontmatter value: its key (expected to be "verdict") and the enum of
// tokens the value declares.
type Contract struct {
	Key    string
	Tokens []string
	Line   int // 1-based line number within the return_format text
}

// contractLineRe matches a `key: value` contract line once comments and
// blank lines have been skipped.
var contractLineRe = regexp.MustCompile(`^\s*([A-Za-z_]+):\s*(.+?)\s*$`)

// tokenPlaceholderRe matches a frontmatter token shaped `word-<reason>`,
// which the registry treats as the pattern `word-*`.
var tokenPlaceholderRe = regexp.MustCompile(`^([a-z][a-z0-9-]*)-<[^>]*>$`)

// ParseContract extracts the contract line from a return_format value: it
// repeats the algorithm guard phase 1 uses (docs/superpowers/specs/
// 2026-09-27-guard-hooks-design.md §6). Empty lines and lines starting
// with `#` are skipped; the first remaining line must match `key: value`,
// and the value is split on `|`, each token trimmed of surrounding
// whitespace (frontmatter in profiles/base/agents/gates/*.md writes
// `valid | insufficient | invalid`, with spaces around the pipe).
//
// A token shaped `word-<placeholder>` becomes the pattern `word-*`; a
// token that is entirely `<placeholder>` becomes the whole-value wildcard
// `*` (matches anything, never checked) — same convention as guard.
func ParseContract(returnFormat string) (Contract, error) {
	for i, line := range strings.Split(returnFormat, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		m := contractLineRe.FindStringSubmatch(line)
		if m == nil {
			return Contract{}, fmt.Errorf("return_format line %d is not `key: value`: %q", i+1, line)
		}
		return Contract{Key: m[1], Tokens: splitTokens(m[2]), Line: i + 1}, nil
	}
	return Contract{}, fmt.Errorf("return_format has no content line")
}

// splitTokens splits a contract value on `|` and normalizes each token
// per the placeholder rules documented on ParseContract.
func splitTokens(value string) []string {
	parts := strings.Split(value, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" {
			continue
		}
		switch {
		case tokenPlaceholderRe.MatchString(t):
			t = tokenPlaceholderRe.FindStringSubmatch(t)[1] + "-*"
		case strings.HasPrefix(t, "<") && strings.HasSuffix(t, ">"):
			t = "*"
		}
		out = append(out, t)
	}
	return out
}

// BodyToken is one `verdict: <token>` occurrence found in an agent's body.
type BodyToken struct {
	Token string
	Line  int // 1-based, relative to the body slice passed to BodyTokens
}

// bodyVerdictRe finds `verdict: <token>` inside prose, optionally wrapped
// in a single backtick (as Markdown inline code: `` `verdict: blocked` ``).
var bodyVerdictRe = regexp.MustCompile("\\bverdict:\\s*`?([a-z][a-z0-9|*-]*)")

// BodyTokens scans body line by line for `verdict: <token>` occurrences. A
// token split across a line break is not found — the same limitation as
// guard's phase-1 validator, and acceptable here: it only under-detects,
// never flags a false positive. A captured value made of several
// `|`-separated words (an agent quoting an enum in prose) yields one
// BodyToken per word. A trailing `-` left over from a truncated
// `blocked-<reason>` placeholder is trimmed; a literal `blocked-*` pattern
// (ending in `*`, not `-`) is kept as-is.
func BodyTokens(body []byte) []BodyToken {
	var out []BodyToken
	for i, line := range strings.Split(string(body), "\n") {
		for _, m := range bodyVerdictRe.FindAllStringSubmatch(line, -1) {
			for _, tok := range strings.Split(m[1], "|") {
				tok = strings.TrimRight(tok, "-")
				if tok == "" {
					continue
				}
				out = append(out, BodyToken{Token: tok, Line: i + 1})
			}
		}
	}
	return out
}

// contractContains reports whether token is exactly one of tokens, matches
// a `word-*` pattern in tokens by prefix, or tokens contains the
// whole-value wildcard `*`.
func contractContains(tokens []string, token string) bool {
	matched := false
	for _, t := range tokens {
		if t == token || t == "*" {
			return true
		}
		if strings.HasSuffix(t, "-*") && strings.HasPrefix(token, strings.TrimSuffix(t, "*")) {
			matched = true
		}
	}
	return matched
}

// quotesAllow reports whether role's body may cite token as a quote of
// another agent's contract, per the registry's `quotes` map.
func (r *Registry) quotesAllow(role, token string) bool {
	for _, allowed := range r.Quotes[role] {
		if allowed == "*" {
			return r.knownToken(token)
		}
		if allowed == token {
			return true
		}
	}
	return false
}

// Finding is one contract violation CheckAgent reports.
type Finding struct {
	Line int // 0 when the finding is about the frontmatter, not a body line
	Msg  string
}

// CheckAgent validates one agent file against the registry:
//
//   - (a0) the return_format contract key must be "verdict", not a legacy
//     key like "completion".
//   - (a) every token return_format declares must be allowed for the
//     agent's role (Registry.Allows) — a frontmatter `x-*` pattern must
//     appear in the registry verbatim.
//   - (b) every `verdict: <token>` the body cites must be inside this very
//     agent's own return_format enum (patterns included), or listed in
//     the registry's `quotes[role]` allowance for legitimately citing
//     another agent's contract.
//
// agentName resolves the role via Registry.Lookup. When it doesn't resolve
// to any role, CheckAgent has nothing to validate tokens against and
// returns no (a)/(b) findings — callers such as doctor decide separately
// whether a role name that fails to resolve is itself an error (ADR
// docs/adr/0003-verdicts-registry.md §D3).
func CheckAgent(r *Registry, agentName string, fm map[string]any, body []byte, bodyStartLine int) []Finding {
	rawReturnFormat, _ := fm["return_format"].(string)
	contract, err := ParseContract(rawReturnFormat)
	if err != nil {
		return nil
	}

	var findings []Finding
	if contract.Key != "verdict" {
		findings = append(findings, Finding{
			Msg: fmt.Sprintf(`return_format must start with "verdict:", got %q`, contract.Key+":"),
		})
		return findings
	}

	role, ok := r.Lookup(agentName)
	if !ok {
		return findings
	}

	for _, tok := range contract.Tokens {
		if !r.Allows(role, tok) {
			findings = append(findings, Finding{
				Msg: fmt.Sprintf("token %q of role %q is not in verdicts.yaml", tok, role),
			})
		}
	}

	for _, bt := range BodyTokens(body) {
		if contractContains(contract.Tokens, bt.Token) || r.quotesAllow(role, bt.Token) {
			continue
		}
		line := bodyStartLine + bt.Line - 1
		findings = append(findings, Finding{
			Line: line,
			Msg:  fmt.Sprintf(`body line %d: "verdict: %s" is outside this agent's return_format enum`, line, bt.Token),
		})
	}

	return findings
}
