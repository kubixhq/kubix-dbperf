//go:build integration

package perf_test

import (
	"context"
	"testing"

	"github.com/kubixhq/kubix-dbperf/internal/perf"
)

func TestTables_Integration_ReturnsResults(t *testing.T) {
	db := integrationDB(t)
	result, err := perf.Tables(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Error("expected slice (possibly empty), got nil")
	}
}

func TestTables_Integration_FieldsValid(t *testing.T) {
	db := integrationDB(t)
	result, err := perf.Tables(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, ts := range result {
		if ts.TableName == "" {
			t.Errorf("result[%d].TableName is empty", i)
		}
		if ts.BloatRatio < 0 || ts.BloatRatio > 1 {
			t.Errorf("result[%d].BloatRatio = %v, must be in [0,1]", i, ts.BloatRatio)
		}
		if ts.LiveRows < 0 {
			t.Errorf("result[%d].LiveRows = %d, must be >= 0", i, ts.LiveRows)
		}
		if ts.DeadRows < 0 {
			t.Errorf("result[%d].DeadRows = %d, must be >= 0", i, ts.DeadRows)
		}
	}
}

func TestTables_Integration_NeedsVacuumConsistent(t *testing.T) {
	db := integrationDB(t)
	result, err := perf.Tables(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, ts := range result {
		// NeedsVacuum must be consistent with BloatRatio (threshold = 0.1)
		expectedNeedsVacuum := ts.BloatRatio > 0.1
		if ts.NeedsVacuum != expectedNeedsVacuum {
			t.Errorf("result[%d] (%s): NeedsVacuum=%v but BloatRatio=%v",
				i, ts.TableName, ts.NeedsVacuum, ts.BloatRatio)
		}
	}
}

func TestTables_Integration_NoSystemTables(t *testing.T) {
	db := integrationDB(t)
	result, err := perf.Tables(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, ts := range result {
		if len(ts.TableName) > 3 && ts.TableName[:3] == "pg_" {
			t.Errorf("system table %q should not appear in results", ts.TableName)
		}
	}
}
