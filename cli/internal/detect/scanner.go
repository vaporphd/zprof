package detect

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alcherk/zprof/internal/manifest"
)

type Match struct {
	OverlayName string
	Confidence  string
	Evidence    []string
}

// Scan walks projectDir and returns a Match per rule that finds at least
// one file/regex match.
func Scan(projectDir string, rules []*manifest.DetectRules) []Match {
	var out []Match
	for _, r := range rules {
		evidence := scanOne(projectDir, r)
		if len(evidence) > 0 {
			out = append(out, Match{
				OverlayName: r.Name,
				Confidence:  r.ConfidenceLevel(),
				Evidence:    evidence,
			})
		}
	}
	return out
}

var skipDirs = map[string]bool{
	"Pods": true, "DerivedData": true, ".build": true,
	"node_modules": true, "build": true, "vendor": true,
	".git": true, ".svn": true,
}

func scanOne(dir string, r *manifest.DetectRules) []string {
	var evidence []string
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
			if skipDirs[name] || (strings.HasPrefix(name, ".") && name != ".") {
				return filepath.SkipDir
			}
		}
		for _, pattern := range patterns {
			m, _ := filepath.Match(pattern, name)
			if m && !seen[path] {
				evidence = append(evidence, path)
				seen[path] = true
				break
			}
		}
		return nil
	})

	// any_regex: read specified path, apply regex
	for _, rr := range r.AnyRegexList() {
		p := filepath.Join(dir, rr.Path)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		re, err := regexp.Compile(rr.Match)
		if err != nil {
			continue
		}
		if re.Match(data) {
			evidence = append(evidence, p+"::"+rr.Match)
		}
	}
	return evidence
}
