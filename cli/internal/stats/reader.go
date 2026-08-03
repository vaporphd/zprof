package stats

import (
	"bufio"
	"encoding/json"
	"os"
	"time"
)

// ReadDispatches reads a dispatches.jsonl file at path, parsing each line as
// a Dispatch record. Malformed lines are skipped and counted in Losses
// rather than causing the whole read to fail.
func ReadDispatches(path string) ([]Dispatch, Losses, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, Losses{}, err
	}
	defer f.Close()

	var dispatches []Dispatch
	var losses Losses
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)
	for sc.Scan() {
		losses.TotalLines++
		var d Dispatch
		if err := json.Unmarshal(sc.Bytes(), &d); err != nil {
			losses.ParseErrors++
			continue
		}
		d.Timestamp, _ = time.Parse(time.RFC3339Nano, d.TsUTC)
		if d.Role == "" {
			losses.MissingRole++
		}
		if !d.DispatchComplete {
			losses.Incomplete++
		}
		dispatches = append(dispatches, d)
	}
	return dispatches, losses, sc.Err()
}
