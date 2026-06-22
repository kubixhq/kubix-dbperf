package perf_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/kubixhq/kubix-dbperf/internal/perf"
)

func newTablesDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, mock
}

func tableCols() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"relname", "seq_scan", "idx_scan",
		"n_live_tup", "n_dead_tup", "last_vacuum", "last_autovacuum",
	})
}

// ── Empty result ──────────────────────────────────────────────────────────────

func TestTables_Empty_ReturnsEmptySlice(t *testing.T) {
	db, mock := newTablesDB(t)
	mock.ExpectQuery(`pg_stat_user_tables`).WillReturnRows(tableCols())

	result, err := perf.Tables(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 results, got %d", len(result))
	}
}

// ── BloatRatio calculation ────────────────────────────────────────────────────

func TestTables_BloatRatio_ZeroTotal(t *testing.T) {
	db, mock := newTablesDB(t)
	mock.ExpectQuery(`pg_stat_user_tables`).
		WillReturnRows(tableCols().AddRow("empty_table", 0, 0, 0, 0, nil, nil))

	result, err := perf.Tables(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].BloatRatio != 0 {
		t.Errorf("BloatRatio = %v, want 0 (zero total rows guard)", result[0].BloatRatio)
	}
	if result[0].NeedsVacuum {
		t.Error("NeedsVacuum should be false when no rows")
	}
}

func TestTables_BloatRatio_AllDead(t *testing.T) {
	// live=0, dead=100 → ratio should be 1.0
	db, mock := newTablesDB(t)
	mock.ExpectQuery(`pg_stat_user_tables`).
		WillReturnRows(tableCols().AddRow("bloated", 1000, 0, 0, 100, nil, nil))

	result, err := perf.Tables(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].BloatRatio != 1.0 {
		t.Errorf("BloatRatio = %v, want 1.0", result[0].BloatRatio)
	}
}

func TestTables_BloatRatio_Mixed(t *testing.T) {
	// live=900, dead=100 → ratio = 100/1000 = 0.1
	db, mock := newTablesDB(t)
	mock.ExpectQuery(`pg_stat_user_tables`).
		WillReturnRows(tableCols().AddRow("mixed", 500, 200, 900, 100, nil, nil))

	result, err := perf.Tables(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const want = 0.1
	if result[0].BloatRatio != want {
		t.Errorf("BloatRatio = %v, want %v", result[0].BloatRatio, want)
	}
}

func TestTables_BloatRatio_LowBloat(t *testing.T) {
	// live=9900, dead=100 → ratio = 0.01
	db, mock := newTablesDB(t)
	mock.ExpectQuery(`pg_stat_user_tables`).
		WillReturnRows(tableCols().AddRow("healthy", 5000, 1000, 9900, 100, nil, nil))

	result, err := perf.Tables(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].BloatRatio >= 0.1 {
		t.Errorf("BloatRatio = %v, want < 0.1", result[0].BloatRatio)
	}
}

// ── NeedsVacuum flag ──────────────────────────────────────────────────────────

func TestTables_NeedsVacuum_True_WhenBloatHigh(t *testing.T) {
	// live=800, dead=200 → ratio = 0.2 > threshold → needs vacuum
	db, mock := newTablesDB(t)
	mock.ExpectQuery(`pg_stat_user_tables`).
		WillReturnRows(tableCols().AddRow("dirty", 100, 0, 800, 200, nil, nil))

	result, err := perf.Tables(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result[0].NeedsVacuum {
		t.Errorf("NeedsVacuum = false, want true (bloat=0.2)")
	}
}

func TestTables_NeedsVacuum_False_WhenBloatLow(t *testing.T) {
	// live=990, dead=10 → ratio = 0.01 → no vacuum needed
	db, mock := newTablesDB(t)
	mock.ExpectQuery(`pg_stat_user_tables`).
		WillReturnRows(tableCols().AddRow("clean", 500, 200, 990, 10, nil, nil))

	result, err := perf.Tables(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].NeedsVacuum {
		t.Errorf("NeedsVacuum = true, want false (bloat=0.01)")
	}
}

// ── LastVacuum selection ──────────────────────────────────────────────────────

func TestTables_LastVacuum_BothNull(t *testing.T) {
	db, mock := newTablesDB(t)
	mock.ExpectQuery(`pg_stat_user_tables`).
		WillReturnRows(tableCols().AddRow("t", 0, 0, 100, 0, nil, nil))

	result, err := perf.Tables(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].LastVacuum != nil {
		t.Errorf("LastVacuum = %v, want nil", result[0].LastVacuum)
	}
}

func TestTables_LastVacuum_ManualOnly(t *testing.T) {
	db, mock := newTablesDB(t)
	ts := "2025-01-10T12:00:00Z"
	mock.ExpectQuery(`pg_stat_user_tables`).
		WillReturnRows(tableCols().AddRow("t", 0, 0, 100, 0, ts, nil))

	result, err := perf.Tables(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].LastVacuum == nil || *result[0].LastVacuum != ts {
		t.Errorf("LastVacuum = %v, want %q", result[0].LastVacuum, ts)
	}
}

func TestTables_LastVacuum_AutoOnly(t *testing.T) {
	db, mock := newTablesDB(t)
	ts := "2025-02-15T08:30:00Z"
	mock.ExpectQuery(`pg_stat_user_tables`).
		WillReturnRows(tableCols().AddRow("t", 0, 0, 100, 0, nil, ts))

	result, err := perf.Tables(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].LastVacuum == nil || *result[0].LastVacuum != ts {
		t.Errorf("LastVacuum = %v, want %q", result[0].LastVacuum, ts)
	}
}

func TestTables_LastVacuum_ManualNewerThanAuto(t *testing.T) {
	db, mock := newTablesDB(t)
	manual := "2025-06-20T10:00:00Z"
	auto := "2025-06-15T10:00:00Z"
	mock.ExpectQuery(`pg_stat_user_tables`).
		WillReturnRows(tableCols().AddRow("t", 0, 0, 100, 0, manual, auto))

	result, err := perf.Tables(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].LastVacuum == nil || *result[0].LastVacuum != manual {
		t.Errorf("LastVacuum = %v, want manual (%q)", result[0].LastVacuum, manual)
	}
}

func TestTables_LastVacuum_AutoNewerThanManual(t *testing.T) {
	db, mock := newTablesDB(t)
	manual := "2025-06-01T10:00:00Z"
	auto := "2025-06-20T10:00:00Z"
	mock.ExpectQuery(`pg_stat_user_tables`).
		WillReturnRows(tableCols().AddRow("t", 0, 0, 100, 0, manual, auto))

	result, err := perf.Tables(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].LastVacuum == nil || *result[0].LastVacuum != auto {
		t.Errorf("LastVacuum = %v, want auto (%q)", result[0].LastVacuum, auto)
	}
}

// ── Fields ────────────────────────────────────────────────────────────────────

func TestTables_Fields_AllPopulated(t *testing.T) {
	db, mock := newTablesDB(t)
	ts := "2025-03-01T00:00:00Z"
	mock.ExpectQuery(`pg_stat_user_tables`).
		WillReturnRows(tableCols().AddRow("orders", int64(1200), int64(800), int64(5000), int64(50), ts, nil))

	result, err := perf.Tables(context.Background(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := result[0]
	if r.TableName != "orders" {
		t.Errorf("TableName = %q, want orders", r.TableName)
	}
	if r.SeqScan != 1200 {
		t.Errorf("SeqScan = %v, want 1200", r.SeqScan)
	}
	if r.IdxScan != 800 {
		t.Errorf("IdxScan = %v, want 800", r.IdxScan)
	}
	if r.LiveRows != 5000 {
		t.Errorf("LiveRows = %v, want 5000", r.LiveRows)
	}
	if r.DeadRows != 50 {
		t.Errorf("DeadRows = %v, want 50", r.DeadRows)
	}
}

// ── Error cases ───────────────────────────────────────────────────────────────

func TestTables_DBError(t *testing.T) {
	db, mock := newTablesDB(t)
	mock.ExpectQuery(`pg_stat_user_tables`).WillReturnError(errors.New("connection refused"))

	_, err := perf.Tables(context.Background(), db)
	if err == nil {
		t.Error("expected error on DB failure")
	}
}

func TestTables_Timeout(t *testing.T) {
	db, mock := newTablesDB(t)
	mock.ExpectQuery(`pg_stat_user_tables`).WillReturnError(context.DeadlineExceeded)

	_, err := perf.Tables(context.Background(), db)
	if err == nil {
		t.Error("expected timeout error")
	}
}
