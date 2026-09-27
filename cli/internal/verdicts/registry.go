// Package verdicts loads and validates profiles/base/verdicts.yaml — the
// registry that gives every agent's `verdict:` token a base-enum meaning
// (done/blocked/failed) and a task-runner action (next/loop/insert/
// triage/escalate/abort). See docs/adr/0003-verdicts-registry.md.
//
// doctor, apply, and the repo-level consistency test all call the same
// pure functions here, so `zprof doctor` on a project and CI on the
// profiles repo never disagree.
package verdicts

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Spec is the meaning of one verdict token: the base-enum value it
// collapses to, and the action the runner takes when it sees that token.
// Action is a bare verb ("next", "abort", "triage", "escalate") or a verb
// with a target ("loop:implementer", "insert:@next").
type Spec struct {
	Base   string `yaml:"base" json:"base"`
	Action string `yaml:"action" json:"action"`
}

// Registry is the parsed, alias-resolved contents of verdicts.yaml.
// Templates and YAML anchors are gone by this point — yaml.v3 resolves
// `*std`-style aliases while decoding, so Roles already holds each role's
// concrete token map.
type Registry struct {
	Version   int
	BaseEnum  []string
	Actions   []string
	Universal map[string]Spec
	// Quotes lists, per role, the tokens that role's body may legitimately
	// cite from another agent's contract. "*" means any token known
	// anywhere in the registry (see knownToken).
	Quotes map[string][]string
	Roles  map[string]map[string]Spec
}

// rawRegistry is decoded strictly (unknown top-level keys are a parse
// error) before being reshaped into Registry.
type rawRegistry struct {
	Version   int                        `yaml:"version"`
	BaseEnum  []string                   `yaml:"base_enum"`
	Actions   []string                   `yaml:"actions"`
	Universal map[string]Spec            `yaml:"universal"`
	Quotes    map[string]quoteValue      `yaml:"quotes"`
	Templates map[string]map[string]Spec `yaml:"templates"`
	Roles     map[string]map[string]Spec `yaml:"roles"`
}

// quoteValue decodes a `quotes.<role>` value that is either the bare
// scalar "*" or a YAML list of specific tokens.
type quoteValue []string

// UnmarshalYAML implements yaml.Unmarshaler for quoteValue.
func (q *quoteValue) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		var s string
		if err := node.Decode(&s); err != nil {
			return err
		}
		*q = quoteValue{s}
		return nil
	}
	var list []string
	if err := node.Decode(&list); err != nil {
		return err
	}
	*q = quoteValue(list)
	return nil
}

// Parse decodes verdicts.yaml. Unknown top-level keys are a hard error —
// a typo'd key (e.g. `role:` instead of `roles:`) would otherwise silently
// produce an empty registry that rejects every token.
func Parse(data []byte) (*Registry, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var raw rawRegistry
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse verdicts.yaml: %w", err)
	}
	quotes := make(map[string][]string, len(raw.Quotes))
	for role, tokens := range raw.Quotes {
		quotes[role] = []string(tokens)
	}
	return &Registry{
		Version:   raw.Version,
		BaseEnum:  raw.BaseEnum,
		Actions:   raw.Actions,
		Universal: raw.Universal,
		Quotes:    quotes,
		Roles:     raw.Roles,
	}, nil
}

// Load reads and parses the registry file at path.
func Load(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read verdicts.yaml: %w", err)
	}
	return Parse(data)
}

// Validate checks internal consistency: every Base is in BaseEnum, every
// Action's verb is a known action, and every loop:/insert: target is a
// known role or the reserved `@next`/`@audited` placeholders. It also
// checks that every literal (non-"*") quoted token actually exists
// somewhere in the registry.
func (r *Registry) Validate() error {
	baseSet := make(map[string]bool, len(r.BaseEnum))
	for _, b := range r.BaseEnum {
		baseSet[b] = true
	}
	actionSet := make(map[string]bool, len(r.Actions))
	for _, a := range r.Actions {
		actionSet[a] = true
	}
	validTarget := map[string]bool{"@next": true, "@audited": true}
	for role := range r.Roles {
		validTarget[role] = true
	}

	checkSpec := func(where string, spec Spec) error {
		if !baseSet[spec.Base] {
			return fmt.Errorf("%s: base %q is not in base_enum", where, spec.Base)
		}
		verb, target, hasTarget := strings.Cut(spec.Action, ":")
		if !actionSet[verb] {
			return fmt.Errorf("%s: action %q is not a known action", where, spec.Action)
		}
		if verb == "loop" || verb == "insert" {
			if !hasTarget || target == "" {
				return fmt.Errorf("%s: action %q needs a :target", where, spec.Action)
			}
			if !validTarget[target] {
				return fmt.Errorf("%s: action %q targets unknown role %q", where, spec.Action, target)
			}
		}
		return nil
	}

	for token, spec := range r.Universal {
		if err := checkSpec(fmt.Sprintf("universal.%s", token), spec); err != nil {
			return err
		}
	}
	for role, tokens := range r.Roles {
		for token, spec := range tokens {
			if err := checkSpec(fmt.Sprintf("roles.%s.%s", role, token), spec); err != nil {
				return err
			}
		}
	}

	for role, allowed := range r.Quotes {
		for _, tok := range allowed {
			if tok == "*" {
				continue
			}
			if !r.knownToken(tok) {
				return fmt.Errorf("quotes.%s: token %q is not a known registry token", role, tok)
			}
		}
	}
	return nil
}

// Lookup maps an applied agent file's name to the role it implements in
// the registry, mirroring internal/agents.RoleOf: the `gates/` prefix is
// stripped, an exact match wins, otherwise the longest `<role>-` prefix
// wins (so `auditor-deep-ios` resolves to `auditor-deep`, not `auditor`).
func (r *Registry) Lookup(agentName string) (role string, ok bool) {
	name := strings.TrimPrefix(agentName, "gates/")
	if _, exact := r.Roles[name]; exact {
		return name, true
	}
	best := ""
	for candidate := range r.Roles {
		if strings.HasPrefix(name, candidate+"-") && len(candidate) > len(best) {
			best = candidate
		}
	}
	if best == "" {
		return "", false
	}
	return best, true
}

// Allows reports whether token is a valid verdict for role: an exact match
// in the role's map, a role token pattern (`blocked-*`) matched by exact
// key (frontmatter patterns must appear in the registry verbatim — no
// prefix-expansion of a pattern against a pattern), a runtime token
// matched by the longest `x-*` pattern in the role's map, or a universal
// token. The whole-value wildcard `*` (from a frontmatter field written
// entirely as `<...>`) always matches.
func (r *Registry) Allows(role, token string) bool {
	if token == "*" {
		return true
	}
	if tokens, ok := r.Roles[role]; ok {
		if _, exact := tokens[token]; exact {
			return true
		}
		if !strings.HasSuffix(token, "-*") {
			best := -1
			for pattern := range tokens {
				if !strings.HasSuffix(pattern, "-*") {
					continue
				}
				prefix := strings.TrimSuffix(pattern, "*")
				if strings.HasPrefix(token, prefix) && len(prefix) > best {
					best = len(prefix)
				}
			}
			if best >= 0 {
				return true
			}
		}
	}
	_, universal := r.Universal[token]
	return universal
}

// knownToken reports whether token is declared anywhere in the registry —
// as a universal token or as a token of any role. Used to resolve the
// `quotes.<role>: "*"` wildcard.
func (r *Registry) knownToken(token string) bool {
	if _, ok := r.Universal[token]; ok {
		return true
	}
	for _, tokens := range r.Roles {
		if _, ok := tokens[token]; ok {
			return true
		}
	}
	return false
}

// NormalizedRole is one role's token map in the deployed form: universal
// tokens merged in, role-specific entries winning on conflict.
type NormalizedRole map[string]Spec

// Normalized is the schema.json-ready shape of the registry: anchors,
// `templates`, and `quotes` are gone, and every role's map already
// includes the universal tokens it didn't override.
//
// Lookup rules for a consumer reading this from `.agentlog/schema.json`
// (e.g. the Python collector or a future guard renderer) mirror
// Registry.Lookup/Allows: resolve the role by exact name, else the
// longest `<role>-` prefix (`gates/` stripped first); resolve the token by
// exact key, else the longest `x-*` pattern key.
type Normalized struct {
	Version  int                       `json:"version"`
	BaseEnum []string                  `json:"base_enum"`
	Roles    map[string]NormalizedRole `json:"roles"`
}

// Normalized flattens the registry for deployment: see Normalized (type).
func (r *Registry) Normalized() Normalized {
	roles := make(map[string]NormalizedRole, len(r.Roles))
	for role, tokens := range r.Roles {
		merged := make(NormalizedRole, len(tokens)+len(r.Universal))
		for tok, spec := range r.Universal {
			merged[tok] = spec
		}
		for tok, spec := range tokens {
			merged[tok] = spec
		}
		roles[role] = merged
	}
	return Normalized{
		Version:  r.Version,
		BaseEnum: r.BaseEnum,
		Roles:    roles,
	}
}
