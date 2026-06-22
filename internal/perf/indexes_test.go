package perf_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/kubixhq/kubix-dbperf/internal/perf"
)

func newIndexDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, mock
}

// expectIndexQueries sets up the three sequential DB queries that Indexes() runs.
func expectIndexQueries(mock sqlmock.Sqlmock, unusedRows, missingRows, dupRows *sqlmock.Rows) {
	mock.ExpectQuery(`pg_stat_user_indexes`).WillReturnRows(unusedRows)
	mock.ExpectQuery(`pg_stat_user_tables`).WillReturnRows(missingRows)
	mock.ExpectQuery(`pg_index`).WillReturnRows(dupRows)
}

func idxCols() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"schemaname", "relname", "indexrelname", "idx_scan"})
}

func missingCols() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"schemaname", "relname", "seq_scan", "idx_scan"})
}

func dupCols() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"nspname", "table_name", "index_name", "cols"})
}

// ── All empty ─────────────────────────────────────────────────────────────────

func TestIndexes_AllEmpty_ReturnsEmptySlices(t *testing.T) {
	db, mock := newIndexDB(t)
	expectIndexQueries(mock, idxCols(), missingCols(), dupCols())

	report, err := perf.Indexes(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Unused == nil || len(report.Unused) != 0 {
		t.Error("Unused should be empty slice, not nil")
	}
	if report.RarelyUsed == nil || len(report.RarelyUsed) != 0 {
		t.Error("RarelyUsed should be empty slice, not nil")
	}
	if report.Missing == nil || len(report.Missing) != 0 {
		t.Error("Missing should be empty slice, not nil")
	}
	if report.Duplicate == nil || len(report.Duplicate) != 0 {
		t.Error("Duplicate should be empty slice, not nil")
	}
}

// ── Unused indexes ────────────────────────────────────────────────────────────

func TestIndexes_Unused_IdxScanZero(t *testing.T) {
	db, mock := newIndexDB(t)
	unusedRows := idxCols().
		AddRow("public", "users", "idx_users_email", int64(0)).
		AddRow("public", "orders", "idx_orders_status", int64(0))
	expectIndexQueries(mock, unusedRows, missingCols(), dupCols())

	report, err := perf.Indexes(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Unused) != 2 {
		t.Errorf("Unused len = %d, want 2", len(report.Unused))
	}
	if len(report.RarelyUsed) != 0 {
		t.Errorf("RarelyUsed len = %d, want 0", len(report.RarelyUsed))
	}
}

// ── Rarely used indexes ───────────────────────────────────────────────────────

func TestIndexes_RarelyUsed_IdxScanUnder10(t *testing.T) {
	db, mock := newIndexDB(t)
	unusedRows := idxCols().
		AddRow("public", "users", "idx_users_phone", int64(1)).
		AddRow("public", "users", "idx_users_addr", int64(9))
	expectIndexQueries(mock, unusedRows, missingCols(), dupCols())

	report, err := perf.Indexes(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Unused) != 0 {
		t.Errorf("Unused len = %d, want 0", len(report.Unused))
	}
	if len(report.RarelyUsed) != 2 {
		t.Errorf("RarelyUsed len = %d, want 2", len(report.RarelyUsed))
	}
}

func TestIndexes_Mixed_UnusedAndRarelyUsed(t *testing.T) {
	db, mock := newIndexDB(t)
	unusedRows := idxCols().
		AddRow("public", "users", "idx_never", int64(0)).
		AddRow("public", "users", "idx_rarely", int64(5)).
		AddRow("public", "users", "idx_active", int64(100)) // not unused or rarely used
	expectIndexQueries(mock, unusedRows, missingCols(), dupCols())

	report, err := perf.Indexes(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Unused) != 1 {
		t.Errorf("Unused len = %d, want 1", len(report.Unused))
	}
	if report.Unused[0].IndexName != "idx_never" {
		t.Errorf("Unused[0].IndexName = %q, want idx_never", report.Unused[0].IndexName)
	}
	if len(report.RarelyUsed) != 1 {
		t.Errorf("RarelyUsed len = %d, want 1", len(report.RarelyUsed))
	}
	if report.RarelyUsed[0].IndexName != "idx_rarely" {
		t.Errorf("RarelyUsed[0].IndexName = %q, want idx_rarely", report.RarelyUsed[0].IndexName)
	}
}

// ── Missing indexes ───────────────────────────────────────────────────────────

func TestIndexes_Missing_SeqScanDominates(t *testing.T) {
	db, mock := newIndexDB(t)
	missingRows := missingCols().
		AddRow("public", "orders", int64(10000), int64(100)).
		AddRow("public", "products", int64(5000), int64(0))
	expectIndexQueries(mock, idxCols(), missingRows, dupCols())

	report, err := perf.Indexes(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Missing) != 2 {
		t.Errorf("Missing len = %d, want 2", len(report.Missing))
	}
	if report.Missing[0].TableName != "orders" {
		t.Errorf("Missing[0].TableName = %q, want orders", report.Missing[0].TableName)
	}
	if report.Missing[0].SeqScan != 10000 {
		t.Errorf("Missing[0].SeqScan = %v, want 10000", report.Missing[0].SeqScan)
	}
}

func TestIndexes_NoMissing_WhenSeqScanLow(t *testing.T) {
	db, mock := newIndexDB(t)
	// seq_scan <= idx_scan → not flagged as missing
	missingRows := missingCols() // empty — DB WHERE clause filters these out
	expectIndexQueries(mock, idxCols(), missingRows, dupCols())

	report, err := perf.Indexes(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Missing) != 0 {
		t.Errorf("Missing len = %d, want 0", len(report.Missing))
	}
}

// ── Duplicate indexes ─────────────────────────────────────────────────────────

func TestIndexes_Duplicate_SameColumnsTwoIndexes(t *testing.T) {
	db, mock := newIndexDB(t)
	dupRows := dupCols().
		AddRow("public", "users", "idx_users_email_1", pq.StringArray{"email"}).
		AddRow("public", "users", "idx_users_email_2", pq.StringArray{"email"})
	expectIndexQueries(mock, idxCols(), missingCols(), dupRows)

	report, err := perf.Indexes(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Duplicate) != 1 {
		t.Fatalf("Duplicate len = %d, want 1", len(report.Duplicate))
	}
	if len(report.Duplicate[0].Indexes) != 2 {
		t.Errorf("Indexes in group = %d, want 2", len(report.Duplicate[0].Indexes))
	}
	if len(report.Duplicate[0].Columns) != 1 || report.Duplicate[0].Columns[0] != "email" {
		t.Errorf("Columns = %v, want [email]", report.Duplicate[0].Columns)
	}
}

func TestIndexes_Duplicate_MultiColumnMatch(t *testing.T) {
	db, mock := newIndexDB(t)
	dupRows := dupCols().
		AddRow("public", "users", "idx_a", pq.StringArray{"first_name", "last_name"}).
		AddRow("public", "users", "idx_b", pq.StringArray{"first_name", "last_name"})
	expectIndexQueries(mock, idxCols(), missingCols(), dupRows)

	report, err := perf.Indexes(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Duplicate) != 1 {
		t.Fatalf("Duplicate len = %d, want 1", len(report.Duplicate))
	}
	if len(report.Duplicate[0].Columns) != 2 {
		t.Errorf("Columns = %v, want [first_name last_name]", report.Duplicate[0].Columns)
	}
}

func TestIndexes_NoDuplicate_DifferentColumns(t *testing.T) {
	db, mock := newIndexDB(t)
	dupRows := dupCols().
		AddRow("public", "users", "idx_email", pq.StringArray{"email"}).
		AddRow("public", "users", "idx_name", pq.StringArray{"name"})
	expectIndexQueries(mock, idxCols(), missingCols(), dupRows)

	report, err := perf.Indexes(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Duplicate) != 0 {
		t.Errorf("Duplicate len = %d, want 0 (different columns)", len(report.Duplicate))
	}
}

func TestIndexes_NoDuplicate_SingleIndexPerTable(t *testing.T) {
	db, mock := newIndexDB(t)
	dupRows := dupCols().
		AddRow("public", "users", "idx_users_email", pq.StringArray{"email"})
	expectIndexQueries(mock, idxCols(), missingCols(), dupRows)

	report, err := perf.Indexes(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Duplicate) != 0 {
		t.Errorf("Duplicate len = %d, want 0 (only one index)", len(report.Duplicate))
	}
}

// ── Error cases ───────────────────────────────────────────────────────────────

func TestIndexes_DBError_OnFirstQuery(t *testing.T) {
	db, mock := newIndexDB(t)
	mock.ExpectQuery(`pg_stat_user_indexes`).WillReturnError(errors.New("connection refused"))

	_, err := perf.Indexes(context.Background(), db)
	if err == nil {
		t.Error("expected error on DB failure")
	}
}

func TestIndexes_DBError_OnDuplicateQuery(t *testing.T) {
	db, mock := newIndexDB(t)
	mock.ExpectQuery(`pg_stat_user_indexes`).WillReturnRows(idxCols())
	mock.ExpectQuery(`pg_stat_user_tables`).WillReturnRows(missingCols())
	mock.ExpectQuery(`pg_index`).WillReturnError(errors.New("connection reset"))

	_, err := perf.Indexes(context.Background(), db)
	if err == nil {
		t.Error("expected error on duplicate query failure")
	}
}
