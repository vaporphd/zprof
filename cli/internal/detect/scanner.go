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

func scanOne(dir string, r *manifest.DetectRules) []string {
	var evidence []string
	// any_file: glob against filenames anywhere in tree
	for _, pattern := range r.AnyFileList() {
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return nil
			}
			if info.IsDir() {
				name := filepath.Base(path)
				if strings.HasPrefix(name, ".") && name != "." {
					return filepath.SkipDir
				}
			}
			m, _ := filepath.Match(pattern, filepath.Base(path))
			if m {
				evidence = append(evidence, path)
			}
			return nil
		})
	}
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
