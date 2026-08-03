package stats

import (
	"path/filepath"
	"runtime"
	"testing"

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
