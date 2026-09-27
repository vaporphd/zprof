package score

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vaporphd/zprof/internal/fsutil"
)

const (
	MarkerBegin = "<!-- zprof:score:begin -->"
	MarkerEnd   = "<!-- zprof:score:end -->"
)

// ScoreKey identifies a scored (run, parameters) pair.
func ScoreKey(c Card) string { return c.RunID + "|" + c.WeightsHash }

// AppendScore appends one JSON line to scores.jsonl (append-only; readers
// take the latest row per ScoreKey).
func AppendScore(path string, c Card) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal score: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return f.Sync()
}

// ReadScoredKeys returns the set of ScoreKey values already present.
func ReadScoredKeys(path string) (map[string]bool, error) {
	keys := map[string]bool{}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return keys, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)
	for sc.Scan() {
		var row struct {
			RunID       string `json:"run_id"`
			WeightsHash string `json:"weights_hash"`
		}
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil || row.RunID == "" {
			continue
		}
		keys[row.RunID+"|"+row.WeightsHash] = true
	}
	return keys, sc.Err()
}

// WriteRunLogSection puts the card between markers in the run log: replaces
// an existing well-formed section (matching begin/end pair) in place,
// otherwise appends a new one at the end. A dangling marker (one without its
// pair) is never treated as the section boundary — it is left untouched as
// inert text so no surrounding content is ever discarded.
func WriteRunLogSection(runLogPath, card string) error {
	data, err := os.ReadFile(runLogPath)
	if err != nil {
		return fmt.Errorf("read run log: %w", err)
	}
	s := string(data)
	section := MarkerBegin + "\n## Score\n```\n" + strings.TrimRight(card, "\n") + "\n```\n" + MarkerEnd

	end := strings.LastIndex(s, MarkerEnd)
	begin := -1
	if end >= 0 {
		begin = strings.LastIndex(s[:end], MarkerBegin)
	}
	if begin >= 0 {
		s = s[:begin] + section + s[end+len(MarkerEnd):]
	} else {
		if !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		s += "\n" + section + "\n"
	}
	return fsutil.WriteFileAtomic(runLogPath, []byte(s), 0o644)
}
