// cli/internal/wizard/init.go
package wizard

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vaporphd/zprof/internal/apply"
	"github.com/vaporphd/zprof/internal/detect"
	"github.com/vaporphd/zprof/internal/managed"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/overlay"
	"github.com/charmbracelet/huh"
)

// Opts bundles the paths Run needs: the project being initialized and the
// zprof repo (base + overlays) to read from.
type Opts struct {
	ProjectDir string
	RepoDir    string
}

// Run drives the interactive `zprof init` flow: detect overlays in
// ProjectDir, prompt the user to confirm/adjust the selection plus
// language/gates/minimal options, then apply the chosen overlays.
func Run(opts Opts) error {
	// 1. Load all overlay detects from repo
	overlaysDir := filepath.Join(opts.RepoDir, "overlays")
	entries, err := os.ReadDir(overlaysDir)
	if err != nil {
		return fmt.Errorf("read overlays: %w", err)
	}
	var rules []*manifest.DetectRules
	var warnings []string
	nameByRule := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r, err := manifest.LoadDetect(filepath.Join(overlaysDir, e.Name(), "detect.yaml"))
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("overlay %s: skipped (bad detect.yaml: %v)", e.Name(), err))
			continue
		}
		rules = append(rules, r)
		nameByRule[r.Name] = e.Name()
	}

	// 2. Scan project — surface scanner warnings so a malformed rule
	// doesn't silently hide its overlay from the detection UI.
	scan := detect.ScanWithWarnings(opts.ProjectDir, rules)
	matches := scan.Matches
	warnings = append(warnings, scan.Warnings...)
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warn:", w)
	}

	// 3. Prompt user with detected + option to add manually
	options := []huh.Option[string]{}
	preSelected := []string{}
	for _, m := range matches {
		label := fmt.Sprintf("%s (%s confidence, %d evidence)",
			m.OverlayName, m.Confidence, len(m.Evidence))
		options = append(options, huh.NewOption(label, nameByRule[m.OverlayName]))
		if m.Confidence == "high" {
			preSelected = append(preSelected, nameByRule[m.OverlayName])
		}
	}
	// Add non-detected overlays as unchecked
	for _, e := range entries {
		if _, seen := findOption(options, e.Name()); seen {
			continue
		}
		options = append(options, huh.NewOption(e.Name()+" (не обнаружен)", e.Name()))
	}

	// Mark the high-confidence detections as pre-selected in the form.
	for i, o := range options {
		if contains(preSelected, o.Value) {
			options[i] = o.Selected(true)
		}
	}

	var (
		chosen    []string
		lang      = "ru"
		withGates bool
		minimal   bool
	)
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Какие overlays применить?").
				Options(options...).
				Value(&chosen),
			huh.NewSelect[string]().
				Title("Язык prompt'ов").
				Options(huh.NewOption("Русский (реком)", "ru"), huh.NewOption("English", "en")).
				Value(&lang),
			huh.NewConfirm().
				Title("Включить gates (north-star / evidence / plan-reviewer)?").
				Value(&withGates),
			huh.NewConfirm().
				Title("Minimal mode (без docs/PROJECT_SPEC.md и adr/)?").
				Value(&minimal),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}
	if len(chosen) == 0 {
		return fmt.Errorf("не выбран ни один overlay")
	}

	// 4. Load base + chosen overlays and apply
	base, err := overlay.LoadBase(filepath.Join(opts.RepoDir, "base"))
	if err != nil {
		return err
	}
	var loaded []*overlay.Overlay
	for _, name := range chosen {
		o, err := overlay.LoadOverlay(filepath.Join(opts.RepoDir, "overlays", name))
		if err != nil {
			return err
		}
		loaded = append(loaded, o)
	}
	proj := &manifest.ProjectManifest{Overlays: chosen, Language: lang, WithGates: withGates, Minimal: minimal}
	_, err = apply.Apply(apply.ApplyOpts{
		ProjectDir: opts.ProjectDir, Base: base, Overlays: loaded,
		Project: proj, MergeMode: managed.ModeOverwrite,
	})
	if err != nil {
		return err
	}
	fmt.Println("✔ zprof init завершён.")
	return nil
}

func findOption(opts []huh.Option[string], name string) (huh.Option[string], bool) {
	for _, o := range opts {
		if o.Value == name {
			return o, true
		}
	}
	return huh.Option[string]{}, false
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
