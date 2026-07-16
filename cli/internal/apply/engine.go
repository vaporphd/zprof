// Package apply implements the full apply engine: writing base+overlay
// agents, rendering managed CLAUDE.md/AGENT_LOOP.md files, ensuring state
// files exist, and persisting the project manifest.
package apply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alcherk/zprof/internal/managed"
	"github.com/alcherk/zprof/internal/manifest"
	"github.com/alcherk/zprof/internal/overlay"
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

	// 1. Base agents (never namespaced)
	for name, content := range opts.Base.Agents {
		override, _ := opts.Project.ResolvedModel(name)
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
			override, _ := opts.Project.ResolvedModel(out)
			if err := WriteAgent(agentDest, out, content, override); err != nil {
				return nil, fmt.Errorf("write %s agent %s: %w", o.Manifest.Name, name, err)
			}
			res.CreatedAgents = append(res.CreatedAgents, out)
		}
	}

	// 3. Render AGENT_LOOP.md
	loopPath := filepath.Join(opts.ProjectDir, "AGENT_LOOP.md")
	loopBlocks := buildLoopBlocks(opts)
	conflicts, err := renderManagedFile(loopPath, loopBlocks, opts)
	if err != nil {
		return nil, fmt.Errorf("render AGENT_LOOP.md: %w", err)
	}
	res.Conflicts = append(res.Conflicts, conflicts...)
	res.UpdatedFiles = append(res.UpdatedFiles, loopPath)

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

	// 6. .gitignore append (thoughts/)
	if err := ensureGitignore(opts.ProjectDir); err != nil {
		return nil, err
	}

	// 7. Save .zprof.yaml
	if err := opts.Project.Save(filepath.Join(opts.ProjectDir, ".zprof.yaml")); err != nil {
		return nil, err
	}
	res.UpdatedFiles = append(res.UpdatedFiles, filepath.Join(opts.ProjectDir, ".zprof.yaml"))

	return res, nil
}

func buildLoopBlocks(opts ApplyOpts) []managed.Block {
	var blocks []managed.Block
	if len(opts.Overlays) > 0 {
		loopTemplate := opts.Overlays[0].Manifest.LoopTemplate
		if base, ok := opts.Base.LoopTemplates[loopTemplate]; ok {
			blocks = append(blocks, managed.Block{
				Overlay: "base",
				Key:     "loop-template-" + loopTemplate,
				Content: base,
			})
		}
	}
	for _, o := range opts.Overlays {
		blocks = append(blocks, managed.Block{
			Overlay: o.Manifest.Name,
			Key:     "loop",
			Content: o.LoopMD,
		})
	}
	return blocks
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
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return nil, err
	}
	return conflicts, nil
}

func ensureGitignore(dir string) error {
	p := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(data)
	entries := []string{"thoughts/", "*.zprof.bak-*", ".zprof.yaml.bak-*"}
	needAppend := ""
	for _, e := range entries {
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
	return os.WriteFile(p, []byte(content+needAppend), 0o644)
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
