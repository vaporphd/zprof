package stats

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func fixtureDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "testdata")
}

func loadFixture(t *testing.T) ([]Dispatch, Losses) {
	t.Helper()
	ds, losses, err := ReadDispatches(filepath.Join(fixtureDir(), "basic.jsonl"))
	require.NoError(t, err)
	return ds, losses
}

func TestAggregate_FilterCompleted(t *testing.T) {
	ds, losses := loadFixture(t)
	r := Aggregate(ds, losses)
	require.Greater(t, r.CompletedCount, 0)
	require.Less(t, r.CompletedCount, r.TotalDispatches, "async_launched should be excluded from completed")
}

func TestAggregate_Sessions(t *testing.T) {
	ds, losses := loadFixture(t)
	r := Aggregate(ds, losses)
	require.Greater(t, r.Sessions, 0)
}

func TestAggregate_Health(t *testing.T) {
	ds, losses := loadFixture(t)
	r := Aggregate(ds, losses)
	require.NotEmpty(t, r.Health, "health should have entries for roles")
	found := false
	for _, h := range r.Health {
		if h.PreambleChecked > 0 && h.PreambleCount > 0 {
			found = true
		}
	}
	require.True(t, found, "fixture has at least one has_preamble=true dispatch")
}

func TestAggregate_Economics(t *testing.T) {
	ds, losses := loadFixture(t)
	r := Aggregate(ds, losses)
	require.Greater(t, r.Economics.TotalTokens.Total(), 0)
	require.NotEmpty(t, r.Economics.ByRole)
	require.NotEmpty(t, r.Economics.ByModel)
	// sorted descending by total tokens
	if len(r.Economics.ByRole) > 1 {
		require.GreaterOrEqual(t, r.Economics.ByRole[0].Tokens.Total(), r.Economics.ByRole[1].Tokens.Total())
	}
}

func TestAggregate_Routes(t *testing.T) {
	ds, losses := loadFixture(t)
	r := Aggregate(ds, losses)
	require.NotEmpty(t, r.Routes.ByStatus)
	require.Contains(t, r.Routes.ByStatus, "completed")
}

func TestAggregate_Losses(t *testing.T) {
	ds, losses := loadFixture(t)
	r := Aggregate(ds, losses)
	require.Equal(t, losses, r.Losses)
	require.Greater(t, r.Losses.ParseErrors, 0, "fixture has a malformed line")
}

func TestAggregate_Empty(t *testing.T) {
	r := Aggregate(nil, Losses{})
	require.Equal(t, 0, r.TotalDispatches)
	require.Empty(t, r.Health)
	require.Equal(t, 0, r.Economics.TotalTokens.Total())
}

func TestPercentile(t *testing.T) {
	sorted := []int64{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000}
	require.Equal(t, int64(500), percentile(sorted, 0.50))
	require.Equal(t, int64(900), percentile(sorted, 0.95))
	require.Equal(t, int64(0), percentile(nil, 0.50))
}

func TestTokenBreakdown_Total(t *testing.T) {
	tb := TokenBreakdown{Input: 100, Output: 200, CacheRead: 300, CacheCreation: 400}
	require.Equal(t, 1000, tb.Total())
}

func boolPtr(b bool) *bool { return &b }

func TestAggregate_DriftGroupsByConfigHash(t *testing.T) {
	base := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	ds := []Dispatch{
		{
			DispatchComplete: true, ConfigHash: "abc123", TokensInput: 1000, TokensOutput: 500,
			HasPreamble: boolPtr(false), Timestamp: base, DurationMs: 100,
		},
		{
			DispatchComplete: true, ConfigHash: "abc123", TokensInput: 2000, TokensOutput: 1000,
			HasPreamble: boolPtr(true), Timestamp: base.Add(time.Hour), DurationMs: 200,
		},
		{
			DispatchComplete: true, ConfigHash: "def456", TokensInput: 500, TokensOutput: 250,
			HasPreamble: boolPtr(false), Timestamp: base.Add(2 * time.Hour), DurationMs: 300,
		},
		// no config_hash: must not create a spurious group and must not panic.
		{DispatchComplete: true, ConfigHash: "", TokensInput: 10, Timestamp: base},
		// not complete: must be excluded from drift entirely.
		{DispatchComplete: false, ConfigHash: "zzz999", TokensInput: 999},
	}
	r := Aggregate(ds, Losses{})
	require.Len(t, r.Drift, 2, "two distinct non-empty config_hash values among completed dispatches")

	byHash := map[string]DriftEntry{}
	for _, de := range r.Drift {
		byHash[de.ConfigHash] = de
	}
	require.NotContains(t, byHash, "", "dispatches without config_hash must not form a group")
	require.NotContains(t, byHash, "zzz999", "incomplete dispatches must not be counted")

	abc := byHash["abc123"]
	require.Equal(t, 2, abc.Dispatches)
	require.Equal(t, (1000+500+2000+1000)/2, abc.AvgTokens)
	require.Equal(t, 50.0, abc.ComplianceRate, "1 of 2 dispatches has_preamble=false -> compliant")
	require.Equal(t, base, abc.FirstSeen)
	require.Equal(t, base.Add(time.Hour), abc.LastSeen)

	def := byHash["def456"]
	require.Equal(t, 1, def.Dispatches)
	require.Equal(t, 100.0, def.ComplianceRate, "sole dispatch has_preamble=false -> compliant")

	// Sorted by LastSeen descending: def456 (newest) before abc123.
	require.Equal(t, "def456", r.Drift[0].ConfigHash)
	require.Equal(t, "abc123", r.Drift[1].ConfigHash)
}

func TestAggregate_DriftNilWhenSingleConfigHash(t *testing.T) {
	ds := []Dispatch{
		{DispatchComplete: true, ConfigHash: "only1", TokensInput: 100},
		{DispatchComplete: true, ConfigHash: "only1", TokensInput: 200},
	}
	r := Aggregate(ds, Losses{})
	require.Nil(t, r.Drift, "drift comparison needs >1 distinct config_hash to be meaningful")
}
