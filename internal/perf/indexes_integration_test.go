//go:build integration

package perf_test

import (
	"context"
	"testing"

	"github.com/kubixhq/kubix-dbperf/internal/perf"
)

func TestIndexes_Integration_ReturnsValidReport(t *testing.T) {
	db := integrationDB(t)
	report, err := perf.Indexes(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.Unused == nil {
		t.Error("Unused should be slice (possibly empty), not nil")
	}
	if report.RarelyUsed == nil {
		t.Error("RarelyUsed should be slice (possibly empty), not nil")
	}
	if report.Missing == nil {
		t.Error("Missing should be slice (possibly empty), not nil")
	}
	if report.Duplicate == nil {
		t.Error("Duplicate should be slice (possibly empty), not nil")
	}
}

func TestIndexes_Integration_UnusedFields(t *testing.T) {
	db := integrationDB(t)
	report, err := perf.Indexes(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, idx := range report.Unused {
		if idx.IndexName == "" {
			t.Errorf("Unused[%d].IndexName is empty", i)
		}
		if idx.TableName == "" {
			t.Errorf("Unused[%d].TableName is empty", i)
		}
		if idx.IdxScan != 0 {
			t.Errorf("Unused[%d].IdxScan = %d, want 0", i, idx.IdxScan)
		}
	}
}

func TestIndexes_Integration_RarelyUsedFields(t *testing.T) {
	db := integrationDB(t)
	report, err := perf.Indexes(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, idx := range report.RarelyUsed {
		if idx.IdxScan == 0 || idx.IdxScan >= 10 {
			t.Errorf("RarelyUsed[%d].IdxScan = %d, want 1-9", i, idx.IdxScan)
		}
	}
}

func TestIndexes_Integration_DuplicateGroupsHaveMultipleIndexes(t *testing.T) {
	db := integrationDB(t)
	report, err := perf.Indexes(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, g := range report.Duplicate {
		if len(g.Indexes) < 2 {
			t.Errorf("Duplicate[%d] has %d indexes, want >= 2", i, len(g.Indexes))
		}
		if len(g.Columns) == 0 {
			t.Errorf("Duplicate[%d].Columns is empty", i)
		}
	}
}
