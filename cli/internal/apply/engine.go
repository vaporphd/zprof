// Package apply implements the full apply engine: writing base+overlay
// agents, rendering managed CLAUDE.md/AGENT_LOOP.md files, ensuring state
// files exist, and persisting the project manifest.
package apply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vaporphd/zprof/internal/managed"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/overlay"
)

// ApplyOpts bundles everything Apply needs to write a project's agents,
// managed files, state files, and manifest.
type ApplyOpts struct {
	ProjectDir string
	Base       *overlay.Base
	Overlays   []*overlay.Overlay
	Project    *manifest.ProjectManifest
	MergeMode  managed.MergeMode
	Resolver   managed.ConflictResolver
}

// ApplyResult reports what Apply created or updated.
type ApplyResult struct {
	CreatedAgents []string
	UpdatedFiles  []string
	StateFiles    []string
	Conflicts     []managed.Conflict
}

// Apply orchestrates a full profile application: base agents, namespaced
// overlay agents, managed AGENT_LOOP.md/CLAUDE.md rendering, state file
// bootstrap, .gitignore maintenance, and project manifest persistence.
func Apply(opts ApplyOpts) (*ApplyResult, error) {
	if opts.Base == nil {
		return nil, errors.New("base is required")
	}
	if len(opts.Overlays) == 0 {
		return nil, errors.New("at least one overlay is required")
	}
	res := &ApplyResult{}

	agentDest := filepath.Join(opts.ProjectDir, ".claude", "agents")
	multi := len(opts.Overlays) > 1

	// 1. Base agents (never namespaced; skip gates/* when !WithGates)
	for name, content := range opts.Base.Agents {
		if !opts.Project.WithGates && strings.HasPrefix(name, "gates/") {
			continue
		}
		override, err := opts.Project.ResolvedModel(name)
		if err != nil && !errors.Is(err, manifest.ErrNoOverride) {
			return nil, fmt.Errorf("resolve model for %s: %w", name, err)
		}
		if err := WriteAgent(agentDest, name, content, override); err != nil {
			return nil, fmt.Errorf("write base agent %s: %w", name, err)
		}
		res.CreatedAgents = append(res.CreatedAgents, name)
	}

	// 2. Overlay agents (namespace if multi)
	for _, o := range opts.Overlays {
		for name, content := range o.Agents {
			out := name
			if multi {
				out = overlay.NamespaceAgent(name, o.Manifest.Name)
			}
			override, err := opts.Project.ResolvedModel(out)
			if err != nil && !errors.Is(err, manifest.ErrNoOverride) {
				return nil, fmt.Errorf("resolve model for %s: %w", out, err)
			}
			if err := WriteAgent(agentDest, out, content, override); err != nil {
				return nil, fmt.Errorf("write %s agent %s: %w", o.Manifest.Name, name, err)
			}
			res.CreatedAgents = append(res.CreatedAgents, out)
		}
	}

	// 3. Render AGENT_LOOP.md (thin router only)
	loopPath := filepath.Join(opts.ProjectDir, "AGENT_LOOP.md")
	loopBlocks := buildRouterBlocks(opts)
	conflicts, err := renderManagedFile(loopPath, loopBlocks, opts)
	if err != nil {
		return nil, fmt.Errorf("render AGENT_LOOP.md: %w", err)
	}
	res.Conflicts = append(res.Conflicts, conflicts...)
	res.UpdatedFiles = append(res.UpdatedFiles, loopPath)

	// 3.5. Render workflows/*.md (composed base workflow + overlay extensions)
	wfConflicts, wfFiles, err := writeWorkflowFiles(opts)
	if err != nil {
		return nil, fmt.Errorf("render workflows: %w", err)
	}
	res.Conflicts = append(res.Conflicts, wfConflicts...)
	res.UpdatedFiles = append(res.UpdatedFiles, wfFiles...)

	// 4. Render CLAUDE.md
	claudePath := filepath.Join(opts.ProjectDir, "CLAUDE.md")
	claudeBlocks := buildClaudeBlocks(opts)
	conflicts, err = renderManagedFile(claudePath, claudeBlocks, opts)
	if err != nil {
		return nil, fmt.Errorf("render CLAUDE.md: %w", err)
	}
	res.Conflicts = append(res.Conflicts, conflicts...)
	res.UpdatedFiles = append(res.UpdatedFiles, claudePath)

	// 5. State files
	state, err := EnsureStateFiles(opts.ProjectDir, opts.Base, opts.Project.Minimal)
	if err != nil {
		return nil, err
	}
	res.StateFiles = state

	// 6. .gitignore append (base entries + per-overlay contributions).
	if err := ensureGitignore(opts.ProjectDir, opts.Overlays); err != nil {
		return nil, err
	}

	// 7. Save .zprof.yaml
	if err := opts.Project.Save(filepath.Join(opts.ProjectDir, ".zprof.yaml")); err != nil {
		return nil, err
	}
	res.UpdatedFiles = append(res.UpdatedFiles, filepath.Join(opts.ProjectDir, ".zprof.yaml"))

	return res, nil
}

// buildRouterBlocks returns the managed blocks for AGENT_LOOP.md. It is now
// a thin router: the base router content only, no overlay contributions.
func buildRouterBlocks(opts ApplyOpts) []managed.Block {
	return []managed.Block{
		{
			Overlay: "base",
			Key:     "router",
			Content: opts.Base.Router,
		},
	}
}

// writeWorkflowFiles renders workflows/<name>.md for every unique workflow
// referenced by opts.Overlays, composing the base workflow content with each
// overlay's workflow-extension (LoopMD) content as separate managed blocks.
func writeWorkflowFiles(opts ApplyOpts) ([]managed.Conflict, []string, error) {
	var names []string
	seen := map[string]bool{}
	for _, o := range opts.Overlays {
		name := o.Manifest.LoopTemplate
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}

	var conflicts []managed.Conflict
	var updated []string
	for _, name := range names {
		base, ok := opts.Base.Workflows[name]
		if !ok {
			return nil, nil, fmt.Errorf("base workflow %q not found (referenced by an overlay)", name)
		}
		blocks := []managed.Block{
			{
				Overlay: "base",
				Key:     "workflow-" + name,
				Content: base,
			},
		}
		for _, o := range opts.Overlays {
			if o.Manifest.LoopTemplate != name {
				continue
			}
			blocks = append(blocks, managed.Block{
				Overlay: o.Manifest.Name,
				Key:     "workflow-extension",
				Content: o.LoopMD,
			})
		}
		path := filepath.Join(opts.ProjectDir, "workflows", name+".md")
		c, err := renderManagedFile(path, blocks, opts)
		if err != nil {
			return nil, nil, fmt.Errorf("render workflows/%s.md: %w", name, err)
		}
		conflicts = append(conflicts, c...)
		updated = append(updated, path)
	}
	return conflicts, updated, nil
}

func buildClaudeBlocks(opts ApplyOpts) []managed.Block {
	var blocks []managed.Block
	if opts.Base.ClaudeBlockBase != "" {
		blocks = append(blocks, managed.Block{
			Overlay: "base",
			Key:     "doctrine",
			Content: opts.Base.ClaudeBlockBase,
		})
	}
	for _, o := range opts.Overlays {
		blocks = append(blocks, managed.Block{
			Overlay: o.Manifest.Name,
			Key:     "stack-config",
			Content: o.ClaudeBlock,
		})
	}
	blocks = append(blocks, managed.Block{
		Overlay: "base",
		Key:     "consilium",
		Content: buildConsiliumTable(opts),
	})
	blocks = append(blocks, managed.Block{
		Overlay: "base",
		Key:     "executing",
		Content: buildExecutingTable(opts),
	})
	return blocks
}

func renderManagedFile(path string, blocks []managed.Block, opts ApplyOpts) ([]managed.Conflict, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}
	// Backup before overwrite mode if file has content.
	if opts.MergeMode == managed.ModeOverwrite && existing != "" {
		if _, err := managed.BackupBeforeWrite(path); err != nil {
			return nil, err
		}
	}
	out, conflicts, err := managed.Merge(existing, blocks, opts.MergeMode, opts.Resolver)
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(path, []byte(out), 0o644); err != nil {
		return nil, err
	}
	return conflicts, nil
}

func ensureGitignore(dir string, overlays []*overlay.Overlay) error {
	p := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(data)
	entries := []string{"thoughts/", "*.zprof.bak-*", ".zprof.yaml.bak-*"}
	for _, o := range overlays {
		if o == nil || o.Manifest == nil {
			continue
		}
		entries = append(entries, o.Manifest.Gitignore...)
	}
	needAppend := ""
	seen := map[string]bool{}
	for _, e := range entries {
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		if !containsLine(content, e) {
			needAppend += e + "\n"
		}
	}
	if needAppend == "" {
		return nil
	}
	if content != "" && !endsWithNewline(content) {
		needAppend = "\n" + needAppend
	}
	return writeFileAtomic(p, []byte(content+needAppend), 0o644)
}

func containsLine(text, needle string) bool {
	for _, line := range splitLines(text) {
		if line == needle {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func endsWithNewline(s string) bool { return len(s) > 0 && s[len(s)-1] == '\n' }
