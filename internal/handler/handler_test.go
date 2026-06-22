package handler_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/kubixhq/kubix-dbperf/internal/config"
	"github.com/kubixhq/kubix-dbperf/internal/handler"
)

// ── Test helpers ──────────────────────────────────────────────────────────────

func newMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, mock
}

func newTestServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	cfg := config.Config{SlowQueryThresholdMs: 100}
	h := handler.New(db, cfg)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/perf/slow-queries", h.SlowQueries)
	mux.HandleFunc("GET /api/perf/indexes", h.Indexes)
	mux.HandleFunc("POST /api/perf/explain", h.Explain)
	mux.HandleFunc("GET /api/perf/tables", h.Tables)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func post(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func readBody(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body := readBody(t, resp.Body)
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, want, body)
	}
}

func assertContentType(t *testing.T, resp *http.Response) {
	t.Helper()
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func assertErrorField(t *testing.T, resp *http.Response, contains string) {
	t.Helper()
	var out map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if !strings.Contains(out["error"], contains) {
		t.Errorf("error field = %q, want to contain %q", out["error"], contains)
	}
}

func expectExtension(mock sqlmock.Sqlmock, present bool) {
	mock.ExpectQuery(`SELECT EXISTS`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(present))
}

func slowQueryCols() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"query", "mean_exec_time", "calls", "total_exec_time", "cache_hit_ratio"})
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

func tableCols() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"relname", "seq_scan", "idx_scan",
		"n_live_tup", "n_dead_tup", "last_vacuum", "last_autovacuum",
	})
}

// ── GET /api/perf/slow-queries ────────────────────────────────────────────────

func TestSlowQueries_OK(t *testing.T) {
	db, mock := newMockDB(t)
	expectExtension(mock, true)
	mock.ExpectQuery(`pg_stat_statements`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(slowQueryCols().
			AddRow("SELECT 1", 250.0, 5, 1250.0, 0.9))

	srv := newTestServer(t, db)
	resp := get(t, srv.URL+"/api/perf/slow-queries")
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusOK)
	assertContentType(t, resp)

	var result []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("len = %d, want 1", len(result))
	}
}

func TestSlowQueries_CustomThreshold(t *testing.T) {
	db, mock := newMockDB(t)
	expectExtension(mock, true)
	mock.ExpectQuery(`pg_stat_statements`).
		WithArgs(500.0).
		WillReturnRows(slowQueryCols())

	srv := newTestServer(t, db)
	resp := get(t, srv.URL+"/api/perf/slow-queries?threshold=500")
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusOK)
}

func TestSlowQueries_InvalidThreshold_Negative(t *testing.T) {
	db, _ := newMockDB(t)
	srv := newTestServer(t, db)

	resp := get(t, srv.URL+"/api/perf/slow-queries?threshold=-1")
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusBadRequest)
	assertErrorField(t, resp, "positive")
}

func TestSlowQueries_InvalidThreshold_NotANumber(t *testing.T) {
	db, _ := newMockDB(t)
	srv := newTestServer(t, db)

	resp := get(t, srv.URL+"/api/perf/slow-queries?threshold=abc")
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusBadRequest)
}

func TestSlowQueries_InvalidThreshold_Zero(t *testing.T) {
	db, _ := newMockDB(t)
	srv := newTestServer(t, db)

	resp := get(t, srv.URL+"/api/perf/slow-queries?threshold=0")
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusBadRequest)
}

func TestSlowQueries_ExtensionMissing_Returns503(t *testing.T) {
	db, mock := newMockDB(t)
	expectExtension(mock, false)

	srv := newTestServer(t, db)
	resp := get(t, srv.URL+"/api/perf/slow-queries")
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusServiceUnavailable)
	assertErrorField(t, resp, "pg_stat_statements")
}

func TestSlowQueries_Timeout_Returns408(t *testing.T) {
	db, mock := newMockDB(t)
	expectExtension(mock, true)
	mock.ExpectQuery(`pg_stat_statements`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnError(context.DeadlineExceeded)

	srv := newTestServer(t, db)
	resp := get(t, srv.URL+"/api/perf/slow-queries")
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusRequestTimeout)
}

func TestSlowQueries_DBError_Returns503(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT EXISTS`).WillReturnError(errors.New("connection refused"))

	srv := newTestServer(t, db)
	resp := get(t, srv.URL+"/api/perf/slow-queries")
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusServiceUnavailable)
}

// ── GET /api/perf/indexes ─────────────────────────────────────────────────────

func TestIndexes_OK(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`pg_stat_user_indexes`).WillReturnRows(idxCols())
	mock.ExpectQuery(`pg_stat_user_tables`).WillReturnRows(missingCols())
	mock.ExpectQuery(`pg_index`).WillReturnRows(dupCols())

	srv := newTestServer(t, db)
	resp := get(t, srv.URL+"/api/perf/indexes")
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusOK)
	assertContentType(t, resp)

	var report map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, key := range []string{"unused", "rarely_used", "missing", "duplicate"} {
		if _, ok := report[key]; !ok {
			t.Errorf("missing field %q in response", key)
		}
	}
}

func TestIndexes_DBError_Returns503(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`pg_stat_user_indexes`).WillReturnError(errors.New("connection refused"))

	srv := newTestServer(t, db)
	resp := get(t, srv.URL+"/api/perf/indexes")
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusServiceUnavailable)
}

// ── POST /api/perf/explain ────────────────────────────────────────────────────

func TestExplain_OK(t *testing.T) {
	db, mock := newMockDB(t)
	planJSON := `[{"Plan":{"Node Type":"Seq Scan","Total Cost":1.0,"Actual Total Time":0.01,"Actual Rows":1}}]`
	mock.ExpectQuery(`EXPLAIN`).WillReturnRows(sqlmock.NewRows([]string{"QUERY PLAN"}).AddRow(planJSON))

	srv := newTestServer(t, db)
	resp := post(t, srv.URL+"/api/perf/explain", `{"query":"SELECT 1"}`)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusOK)
	assertContentType(t, resp)

	var node map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&node); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if node["type"] != "Seq Scan" {
		t.Errorf("type = %v, want Seq Scan", node["type"])
	}
}

func TestExplain_EmptyQuery_Returns400(t *testing.T) {
	db, _ := newMockDB(t)
	srv := newTestServer(t, db)

	resp := post(t, srv.URL+"/api/perf/explain", `{"query":""}`)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusBadRequest)
}

func TestExplain_WhitespaceQuery_Returns400(t *testing.T) {
	db, _ := newMockDB(t)
	srv := newTestServer(t, db)

	resp := post(t, srv.URL+"/api/perf/explain", `{"query":"   "}`)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusBadRequest)
}

func TestExplain_INSERT_Returns400(t *testing.T) {
	db, _ := newMockDB(t)
	srv := newTestServer(t, db)

	resp := post(t, srv.URL+"/api/perf/explain", `{"query":"INSERT INTO t VALUES (1)"}`)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusBadRequest)
	assertErrorField(t, resp, "only SELECT")
}

func TestExplain_UPDATE_Returns400(t *testing.T) {
	db, _ := newMockDB(t)
	srv := newTestServer(t, db)

	resp := post(t, srv.URL+"/api/perf/explain", `{"query":"UPDATE t SET col=1"}`)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusBadRequest)
}

func TestExplain_DELETE_Returns400(t *testing.T) {
	db, _ := newMockDB(t)
	srv := newTestServer(t, db)

	resp := post(t, srv.URL+"/api/perf/explain", `{"query":"DELETE FROM t"}`)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusBadRequest)
}

func TestExplain_InvalidJSON_Returns400(t *testing.T) {
	db, _ := newMockDB(t)
	srv := newTestServer(t, db)

	resp := post(t, srv.URL+"/api/perf/explain", `not json at all`)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusBadRequest)
}

func TestExplain_EmptyBody_Returns400(t *testing.T) {
	db, _ := newMockDB(t)
	srv := newTestServer(t, db)

	req, _ := http.NewRequest("POST", srv.URL+"/api/perf/explain", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusBadRequest)
}

func TestExplain_InvalidSQL_Returns400(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`EXPLAIN`).WillReturnError(errors.New("ERROR: syntax error at or near"))

	srv := newTestServer(t, db)
	resp := post(t, srv.URL+"/api/perf/explain", `{"query":"SELECT FROM WHERE"}`)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusBadRequest)
}

func TestExplain_Timeout_Returns408(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`EXPLAIN`).WillReturnError(context.DeadlineExceeded)

	srv := newTestServer(t, db)
	resp := post(t, srv.URL+"/api/perf/explain", `{"query":"SELECT 1"}`)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusRequestTimeout)
}

// ── GET /api/perf/tables ──────────────────────────────────────────────────────

func TestTables_OK(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`pg_stat_user_tables`).
		WillReturnRows(tableCols().
			AddRow("users", int64(500), int64(1000), int64(9000), int64(200), nil, nil))

	srv := newTestServer(t, db)
	resp := get(t, srv.URL+"/api/perf/tables")
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusOK)
	assertContentType(t, resp)

	var result []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	if result[0]["table_name"] != "users" {
		t.Errorf("table_name = %v, want users", result[0]["table_name"])
	}
	if _, ok := result[0]["needs_vacuum"]; !ok {
		t.Error("missing needs_vacuum field")
	}
	if _, ok := result[0]["bloat_ratio"]; !ok {
		t.Error("missing bloat_ratio field")
	}
}

func TestTables_DBError_Returns503(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`pg_stat_user_tables`).WillReturnError(errors.New("connection refused"))

	srv := newTestServer(t, db)
	resp := get(t, srv.URL+"/api/perf/tables")
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusServiceUnavailable)
}

func TestTables_Timeout_Returns408(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`pg_stat_user_tables`).WillReturnError(context.DeadlineExceeded)

	srv := newTestServer(t, db)
	resp := get(t, srv.URL+"/api/perf/tables")
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusRequestTimeout)
}

// ── Method Not Allowed (405) ──────────────────────────────────────────────────

func TestMethodNotAllowed_ExplainAsGET(t *testing.T) {
	db, _ := newMockDB(t)
	srv := newTestServer(t, db)

	resp := get(t, srv.URL+"/api/perf/explain")
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusMethodNotAllowed)
}

func TestMethodNotAllowed_SlowQueriesAsPOST(t *testing.T) {
	db, _ := newMockDB(t)
	srv := newTestServer(t, db)

	resp := post(t, srv.URL+"/api/perf/slow-queries", `{}`)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusMethodNotAllowed)
}

func TestMethodNotAllowed_IndexesAsPOST(t *testing.T) {
	db, _ := newMockDB(t)
	srv := newTestServer(t, db)

	resp := post(t, srv.URL+"/api/perf/indexes", `{}`)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusMethodNotAllowed)
}

func TestMethodNotAllowed_TablesAsPOST(t *testing.T) {
	db, _ := newMockDB(t)
	srv := newTestServer(t, db)

	resp := post(t, srv.URL+"/api/perf/tables", `{}`)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusMethodNotAllowed)
}

// ── Not Found (404) ───────────────────────────────────────────────────────────

func TestNotFound_UnknownEndpoint(t *testing.T) {
	db, _ := newMockDB(t)
	srv := newTestServer(t, db)

	resp := get(t, srv.URL+"/api/perf/unknown")
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusNotFound)
}

func TestNotFound_WrongPrefix(t *testing.T) {
	db, _ := newMockDB(t)
	srv := newTestServer(t, db)

	resp := get(t, srv.URL+"/slow-queries")
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusNotFound)
}

// ── Response structure ────────────────────────────────────────────────────────

func TestSlowQueries_ResponseFields(t *testing.T) {
	db, mock := newMockDB(t)
	expectExtension(mock, true)
	mock.ExpectQuery(`pg_stat_statements`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(slowQueryCols().
			AddRow("SELECT * FROM t", 300.0, 20, 6000.0, 0.85))

	srv := newTestServer(t, db)
	resp := get(t, srv.URL+"/api/perf/slow-queries")
	defer resp.Body.Close()

	var result []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	row := result[0]
	for _, field := range []string{"query", "mean_exec_time", "calls", "total_exec_time", "cache_hit_ratio"} {
		if _, ok := row[field]; !ok {
			t.Errorf("missing field %q in slow query response", field)
		}
	}
}
