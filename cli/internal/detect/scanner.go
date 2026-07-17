package detect

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/vaporphd/zprof/internal/manifest"
)

type Match struct {
	OverlayName string
	Confidence  string
	Evidence    []string
}

// ScanResult carries matches plus non-fatal warnings (malformed rules,
// paths that escaped the project root, regexes that failed to compile).
// Callers should surface Warnings — the previous silent-continue behavior
// made overlays invisible when their detect.yaml had a typo.
type ScanResult struct {
	Matches  []Match
	Warnings []string
}

// Scan walks projectDir and returns a Match per rule that finds at least
// one file/regex match. Kept for backwards compatibility with callers that
// don't need warnings; new code should use ScanWithWarnings.
func Scan(projectDir string, rules []*manifest.DetectRules) []Match {
	return ScanWithWarnings(projectDir, rules).Matches
}

// ScanWithWarnings is Scan plus diagnostics for malformed rules.
func ScanWithWarnings(projectDir string, rules []*manifest.DetectRules) ScanResult {
	var res ScanResult
	for _, r := range rules {
		evidence, warns := scanOne(projectDir, r)
		res.Warnings = append(res.Warnings, warns...)
		if len(evidence) > 0 {
			res.Matches = append(res.Matches, Match{
				OverlayName: r.Name,
				Confidence:  r.ConfidenceLevel(),
				Evidence:    evidence,
			})
		}
	}
	return res
}

var skipDirs = map[string]bool{
	"Pods": true, "DerivedData": true, ".build": true,
	"node_modules": true, "build": true, "vendor": true,
	".git": true, ".svn": true,
}

func scanOne(dir string, r *manifest.DetectRules) ([]string, []string) {
	var evidence []string
	var warnings []string
	seen := make(map[string]bool)
	patterns := r.AnyFileList()

	// any_file: single walk of the tree, matched against every pattern,
	// skipping heavy vendor/build directories so large projects (e.g.
	// with a populated Pods/ or node_modules/) don't thrash the disk with
	// one walk per rule. Directory *names* are still matched against the
	// patterns (not just files) — an Xcode project (*.xcodeproj) or
	// workspace (*.xcworkspace) is itself a directory, never a plain
	// file, so skipping pattern checks for dirs would silently break
	// detection of pure Xcode (non-SPM) projects.
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		name := filepath.Base(path)
		if info.IsDir() {
			// Never SkipDir on the walk root itself: filepath.Walk fires
			// the callback for `dir` first, and if the project directory's
			// basename starts with a dot (e.g. /x/.myproj) the dot-prefix
			// rule below would abort the entire walk before touching any
			// children, yielding zero evidence and no overlays detected.
			if path != dir && (skipDirs[name] || (strings.HasPrefix(name, ".") && name != ".")) {
				return filepath.SkipDir
			}
		}
		for _, pattern := range patterns {
			m, err := filepath.Match(pattern, name)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("overlay=%s: bad any_file pattern %q: %v", r.Name, pattern, err))
				continue
			}
			if m && !seen[path] {
				evidence = append(evidence, path)
				seen[path] = true
				break
			}
		}
		return nil
	})

	// any_regex: read specified path, apply regex. Paths in detect.yaml
	// come from overlays that may be pulled from an external git repo — a
	// crafted `path: ../../../etc/passwd` (or an absolute path) would let a
	// malicious overlay read arbitrary files and leak their presence via
	// evidence output. Reject anything that escapes the project root or is
	// absolute before touching the disk.
	for _, rr := range r.AnyRegexList() {
		if filepath.IsAbs(rr.Path) {
			warnings = append(warnings, fmt.Sprintf("overlay=%s: absolute any_regex path %q rejected", r.Name, rr.Path))
			continue
		}
		cleaned := filepath.Clean(rr.Path)
		if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			warnings = append(warnings, fmt.Sprintf("overlay=%s: any_regex path %q escapes project root", r.Name, rr.Path))
			continue
		}
		p := filepath.Join(dir, cleaned)
		// Belt-and-braces: after Join, ensure the resolved path is still
		// under dir. Handles clever inputs like `foo/../../..` that Clean
		// collapses to something outside.
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		absP, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if absP != absDir && !strings.HasPrefix(absP, absDir+string(filepath.Separator)) {
			warnings = append(warnings, fmt.Sprintf("overlay=%s: any_regex path %q resolves outside project root", r.Name, rr.Path))
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		re, err := regexp.Compile(rr.Match)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("overlay=%s: any_regex %q failed to compile: %v", r.Name, rr.Match, err))
			continue
		}
		if re.Match(data) {
			evidence = append(evidence, p+"::"+rr.Match)
		}
	}
	return evidence, warnings
}
