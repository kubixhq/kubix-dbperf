//go:build integration

package perf_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/kubixhq/kubix-dbperf/internal/perf"
	_ "github.com/lib/pq"
)

func integrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DSN")
	if dsn == "" {
		dsn = "host=localhost port=5432 dbname=postgres user=postgres password=postgres sslmode=disable"
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Ping(); err != nil {
		t.Skipf("postgres not reachable: %v", err)
	}
	return db
}

func TestExplain_Integration_ValidSelect(t *testing.T) {
	db := integrationDB(t)
	node, err := perf.Explain(context.Background(), db, "SELECT 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node == nil {
		t.Fatal("expected non-nil node")
	}
	if node.Type == "" {
		t.Error("root node type should not be empty")
	}
}

func TestExplain_Integration_JoinQuery(t *testing.T) {
	db := integrationDB(t)
	q := `SELECT a.oid, b.nspname
	      FROM pg_class a
	      JOIN pg_namespace b ON b.oid = a.relnamespace
	      LIMIT 10`
	node, err := perf.Explain(context.Background(), db, q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(node.Children) == 0 {
		t.Error("join query should produce child nodes")
	}
}

func TestExplain_Integration_EmptyQuery_Returns400Error(t *testing.T) {
	db := integrationDB(t)
	if _, err := perf.Explain(context.Background(), db, ""); err == nil {
		t.Error("expected ErrEmptyQuery")
	}
}

func TestExplain_Integration_InsertRejected(t *testing.T) {
	db := integrationDB(t)
	if _, err := perf.Explain(context.Background(), db, "INSERT INTO t VALUES (1)"); err == nil {
		t.Error("expected ErrNotSelect")
	}
}

func TestExplain_Integration_InvalidSQL(t *testing.T) {
	db := integrationDB(t)
	if _, err := perf.Explain(context.Background(), db, "SELECT FROM WHERE"); err == nil {
		t.Error("expected error for invalid SQL")
	}
}

func TestExplain_Integration_CTE(t *testing.T) {
	db := integrationDB(t)
	q := "WITH cte AS (SELECT 1 AS n) SELECT * FROM cte"
	node, err := perf.Explain(context.Background(), db, q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.Type == "" {
		t.Error("root node type should not be empty")
	}
}

func TestExplain_Integration_NodeFieldsPopulated(t *testing.T) {
	db := integrationDB(t)
	node, err := perf.Explain(context.Background(), db, "SELECT * FROM pg_class LIMIT 100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.Cost == 0 && node.ActualTime == 0 {
		t.Error("expected non-zero cost or actual_time")
	}
}
