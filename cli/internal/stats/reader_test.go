package stats

import (
	"testing"
	"time"
)

const fixturePath = "testdata/basic.jsonl"

func TestReadDispatches_Count(t *testing.T) {
	dispatches, _, err := ReadDispatches(fixturePath)
	if err != nil {
		t.Fatalf("ReadDispatches returned error: %v", err)
	}
	const want = 13
	if got := len(dispatches); got != want {
		t.Fatalf("len(dispatches) = %d, want %d", got, want)
	}
}

func TestReadDispatches_Losses(t *testing.T) {
	dispatches, losses, err := ReadDispatches(fixturePath)
	if err != nil {
		t.Fatalf("ReadDispatches returned error: %v", err)
	}

	if want := len(dispatches) + losses.ParseErrors; losses.TotalLines != want {
		t.Errorf("TotalLines = %d, want %d (parsed + parse errors)", losses.TotalLines, want)
	}
	if losses.ParseErrors != 1 {
		t.Errorf("ParseErrors = %d, want 1", losses.ParseErrors)
	}
	if losses.MissingRole != 2 {
		t.Errorf("MissingRole = %d, want 2", losses.MissingRole)
	}
	if losses.Incomplete != 2 {
		t.Errorf("Incomplete = %d, want 2", losses.Incomplete)
	}
}

func TestReadDispatches_Timestamp(t *testing.T) {
	dispatches, _, err := ReadDispatches(fixturePath)
	if err != nil {
		t.Fatalf("ReadDispatches returned error: %v", err)
	}
	if len(dispatches) == 0 {
		t.Fatal("no dispatches parsed")
	}

	first := dispatches[0]
	want, err := time.Parse(time.RFC3339Nano, first.TsUTC)
	if err != nil {
		t.Fatalf("failed to parse TsUTC fixture value %q: %v", first.TsUTC, err)
	}
	if !first.Timestamp.Equal(want) {
		t.Errorf("Timestamp = %v, want %v (parsed from TsUTC=%q)", first.Timestamp, want, first.TsUTC)
	}

	// Every successfully parsed record should have a non-zero timestamp,
	// since every fixture line has a valid ts_utc.
	for i, d := range dispatches {
		if d.Timestamp.IsZero() {
			t.Errorf("dispatches[%d] (dispatch_id=%s) has zero Timestamp", i, d.DispatchID)
		}
	}
}

func TestReadDispatches_BoolPointers(t *testing.T) {
	dispatches, _, err := ReadDispatches(fixturePath)
	if err != nil {
		t.Fatalf("ReadDispatches returned error: %v", err)
	}

	var withPreamble, withoutPreamble int
	for _, d := range dispatches {
		if d.HasPreamble != nil {
			withPreamble++
			if !*d.HasPreamble {
				t.Errorf("dispatch_id=%s: HasPreamble = false, want true", d.DispatchID)
			}
			// Other bool-pointer fields are absent from this record in the
			// fixture, so they must decode as nil, not as a false value.
			if d.ArtifactExists != nil {
				t.Errorf("dispatch_id=%s: ArtifactExists = %v, want nil (absent in fixture)", d.DispatchID, *d.ArtifactExists)
			}
		} else {
			withoutPreamble++
		}
	}

	if withPreamble != 1 {
		t.Errorf("dispatches with non-nil HasPreamble = %d, want 1", withPreamble)
	}
	if want := len(dispatches) - 1; withoutPreamble != want {
		t.Errorf("dispatches with nil HasPreamble = %d, want %d", withoutPreamble, want)
	}
}

func TestReadDispatches_MissingFile(t *testing.T) {
	dispatches, losses, err := ReadDispatches("testdata/does-not-exist.jsonl")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if dispatches != nil {
		t.Errorf("expected nil dispatches on error, got %v", dispatches)
	}
	if losses != (Losses{}) {
		t.Errorf("expected zero-value Losses on error, got %+v", losses)
	}
}
