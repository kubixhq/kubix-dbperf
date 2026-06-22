//go:build integration

package perf_test

import (
	"context"
	"testing"

	"github.com/kubixhq/kubix-dbperf/internal/perf"
)

func TestSlowQueries_Integration_ExtensionMissing_Or_Results(t *testing.T) {
	db := integrationDB(t)
	// Result depends on whether pg_stat_statements is installed on the test DB.
	// Either it returns ErrExtensionRequired or a valid (possibly empty) slice.
	result, err := perf.SlowQueries(context.Background(), db, 100)
	if err != nil {
		if err == perf.ErrExtensionRequired {
			t.Skip("pg_stat_statements not installed — skipping content checks")
		}
		t.Fatalf("unexpected error: %v", err)
	}
	// nil slice is not acceptable regardless of count
	if result == nil {
		t.Error("expected slice (possibly empty), got nil")
	}
}

func TestSlowQueries_Integration_WithExtension_FieldsValid(t *testing.T) {
	db := integrationDB(t)
	result, err := perf.SlowQueries(context.Background(), db, 0)
	if err == perf.ErrExtensionRequired {
		t.Skip("pg_stat_statements not installed")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, sq := range result {
		if sq.Query == "" {
			t.Errorf("result[%d].Query is empty", i)
		}
		if sq.Calls < 0 {
			t.Errorf("result[%d].Calls = %d, must be >= 0", i, sq.Calls)
		}
		if sq.CacheHitRatio < 0 || sq.CacheHitRatio > 1 {
			t.Errorf("result[%d].CacheHitRatio = %v, must be in [0,1]", i, sq.CacheHitRatio)
		}
	}
}

func TestSlowQueries_Integration_SortedDescending(t *testing.T) {
	db := integrationDB(t)
	result, err := perf.SlowQueries(context.Background(), db, 0)
	if err == perf.ErrExtensionRequired {
		t.Skip("pg_stat_statements not installed")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 1; i < len(result); i++ {
		if result[i].MeanExecTime > result[i-1].MeanExecTime {
			t.Errorf("not sorted DESC: result[%d].MeanExecTime(%v) > result[%d].MeanExecTime(%v)",
				i, result[i].MeanExecTime, i-1, result[i-1].MeanExecTime)
		}
	}
}

func TestSlowQueries_Integration_HighThreshold_Empty(t *testing.T) {
	db := integrationDB(t)
	result, err := perf.SlowQueries(context.Background(), db, 999_999_999)
	if err == perf.ErrExtensionRequired {
		t.Skip("pg_stat_statements not installed")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 results for extreme threshold, got %d", len(result))
	}
}
