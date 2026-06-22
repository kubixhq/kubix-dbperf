package perf_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/kubixhq/kubix-dbperf/internal/perf"
)

func newSlowDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, mock
}

func expectExtension(mock sqlmock.Sqlmock, present bool) {
	mock.ExpectQuery(`SELECT EXISTS`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(present))
}

func slowQueryCols() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"query", "mean_exec_time", "calls", "total_exec_time", "cache_hit_ratio"})
}

// ── Extension check ───────────────────────────────────────────────────────────

func TestSlowQueries_ExtensionMissing_Returns503Error(t *testing.T) {
	db, mock := newSlowDB(t)
	expectExtension(mock, false)

	_, err := perf.SlowQueries(context.Background(), db, 100)
	if !errors.Is(err, perf.ErrExtensionRequired) {
		t.Errorf("got %v, want ErrExtensionRequired", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

// ── Happy path ────────────────────────────────────────────────────────────────

func TestSlowQueries_EmptyResult_ReturnsEmptySlice(t *testing.T) {
	db, mock := newSlowDB(t)
	expectExtension(mock, true)
	mock.ExpectQuery(`pg_stat_statements`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(slowQueryCols())

	result, err := perf.SlowQueries(context.Background(), db, 5000)
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

func TestSlowQueries_SingleResult(t *testing.T) {
	db, mock := newSlowDB(t)
	expectExtension(mock, true)
	mock.ExpectQuery(`pg_stat_statements`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(slowQueryCols().AddRow("SELECT 1", 200.5, 10, 2005.0, 0.9))

	result, err := perf.SlowQueries(context.Background(), db, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	q := result[0]
	if q.Query != "SELECT 1" {
		t.Errorf("Query = %q, want SELECT 1", q.Query)
	}
	if q.MeanExecTime != 200.5 {
		t.Errorf("MeanExecTime = %v, want 200.5", q.MeanExecTime)
	}
	if q.Calls != 10 {
		t.Errorf("Calls = %v, want 10", q.Calls)
	}
	if q.TotalExecTime != 2005.0 {
		t.Errorf("TotalExecTime = %v, want 2005.0", q.TotalExecTime)
	}
	if q.CacheHitRatio != 0.9 {
		t.Errorf("CacheHitRatio = %v, want 0.9", q.CacheHitRatio)
	}
}

func TestSlowQueries_Results_SortedSlowToFast(t *testing.T) {
	db, mock := newSlowDB(t)
	expectExtension(mock, true)
	// DB returns rows already sorted DESC (ORDER BY mean_exec_time DESC in SQL).
	mock.ExpectQuery(`pg_stat_statements`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(slowQueryCols().
			AddRow("slow query", 500.0, 5, 2500.0, 0.8).
			AddRow("medium query", 300.0, 8, 2400.0, 0.7).
			AddRow("fast query", 150.0, 20, 3000.0, 0.95))

	result, err := perf.SlowQueries(context.Background(), db, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
	if result[0].MeanExecTime < result[1].MeanExecTime || result[1].MeanExecTime < result[2].MeanExecTime {
		t.Errorf("results not in descending order: %v %v %v",
			result[0].MeanExecTime, result[1].MeanExecTime, result[2].MeanExecTime)
	}
}

func TestSlowQueries_CacheHitRatio_ZeroBlocks(t *testing.T) {
	db, mock := newSlowDB(t)
	expectExtension(mock, true)
	mock.ExpectQuery(`pg_stat_statements`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(slowQueryCols().AddRow("SELECT 1", 200.0, 1, 200.0, 0.0))

	result, err := perf.SlowQueries(context.Background(), db, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].CacheHitRatio != 0.0 {
		t.Errorf("CacheHitRatio = %v, want 0.0", result[0].CacheHitRatio)
	}
}

func TestSlowQueries_CacheHitRatio_AllCached(t *testing.T) {
	db, mock := newSlowDB(t)
	expectExtension(mock, true)
	mock.ExpectQuery(`pg_stat_statements`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(slowQueryCols().AddRow("SELECT 1", 200.0, 1, 200.0, 1.0))

	result, err := perf.SlowQueries(context.Background(), db, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].CacheHitRatio != 1.0 {
		t.Errorf("CacheHitRatio = %v, want 1.0", result[0].CacheHitRatio)
	}
}

func TestSlowQueries_CacheHitRatio_PartialHit(t *testing.T) {
	db, mock := newSlowDB(t)
	expectExtension(mock, true)
	mock.ExpectQuery(`pg_stat_statements`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(slowQueryCols().AddRow("SELECT 1", 200.0, 1, 200.0, 0.75))

	result, err := perf.SlowQueries(context.Background(), db, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].CacheHitRatio != 0.75 {
		t.Errorf("CacheHitRatio = %v, want 0.75", result[0].CacheHitRatio)
	}
}

func TestSlowQueries_ZeroMeanExecTime_Returned(t *testing.T) {
	db, mock := newSlowDB(t)
	expectExtension(mock, true)
	mock.ExpectQuery(`pg_stat_statements`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(slowQueryCols().AddRow("SELECT 1", 0.0, 0, 0.0, 0.0))

	result, err := perf.SlowQueries(context.Background(), db, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 result for zero mean_exec_time, got %d", len(result))
	}
}

func TestSlowQueries_ManyResults(t *testing.T) {
	db, mock := newSlowDB(t)
	expectExtension(mock, true)

	rows := slowQueryCols()
	for i := 0; i < 100; i++ {
		rows.AddRow("SELECT $1", float64(1000-i), int64(i+1), float64((1000-i)*i+1), 0.5)
	}
	mock.ExpectQuery(`pg_stat_statements`).WithArgs(sqlmock.AnyArg()).WillReturnRows(rows)

	result, err := perf.SlowQueries(context.Background(), db, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 100 {
		t.Errorf("expected 100 results, got %d", len(result))
	}
}

// ── Error cases ───────────────────────────────────────────────────────────────

func TestSlowQueries_DBError_OnExtensionCheck(t *testing.T) {
	db, mock := newSlowDB(t)
	mock.ExpectQuery(`SELECT EXISTS`).WillReturnError(errors.New("connection refused"))

	_, err := perf.SlowQueries(context.Background(), db, 100)
	if err == nil {
		t.Error("expected error on DB failure")
	}
}

func TestSlowQueries_DBError_OnMainQuery(t *testing.T) {
	db, mock := newSlowDB(t)
	expectExtension(mock, true)
	mock.ExpectQuery(`pg_stat_statements`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnError(errors.New("connection reset by peer"))

	_, err := perf.SlowQueries(context.Background(), db, 100)
	if err == nil {
		t.Error("expected error on query failure")
	}
}

func TestSlowQueries_Timeout(t *testing.T) {
	db, mock := newSlowDB(t)
	expectExtension(mock, true)
	mock.ExpectQuery(`pg_stat_statements`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnError(context.DeadlineExceeded)

	_, err := perf.SlowQueries(context.Background(), db, 100)
	if err == nil {
		t.Error("expected timeout error")
	}
}
