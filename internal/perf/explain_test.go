package perf

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func newExplainDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, mock
}

// ── isSelectQuery ─────────────────────────────────────────────────────────────

func TestIsSelectQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{"SELECT uppercase", "SELECT 1", true},
		{"SELECT lowercase", "select * from users", true},
		{"SELECT leading spaces", "   SELECT id FROM t", true},
		{"SELECT leading tab", "\tSELECT id FROM t", true},
		{"WITH cte uppercase", "WITH cte AS (SELECT 1) SELECT * FROM cte", true},
		{"WITH cte lowercase", "with cte as (select 1) select * from cte", true},
		{"INSERT", "INSERT INTO t VALUES (1)", false},
		{"INSERT lowercase", "insert into t values (1)", false},
		{"UPDATE", "UPDATE t SET col = 1", false},
		{"DELETE", "DELETE FROM t", false},
		{"DROP TABLE", "DROP TABLE t", false},
		{"DROP INDEX", "DROP INDEX idx", false},
		{"ALTER TABLE", "ALTER TABLE t ADD COLUMN c int", false},
		{"CREATE TABLE", "CREATE TABLE t (id int)", false},
		{"TRUNCATE", "TRUNCATE t", false},
		{"TRUNCATE TABLE", "TRUNCATE TABLE t", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isSelectQuery(tc.query)
			if got != tc.want {
				t.Errorf("isSelectQuery(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

// ── parsePlan ─────────────────────────────────────────────────────────────────

func TestParsePlan_SimpleSeqScan(t *testing.T) {
	raw := `[{"Plan":{"Node Type":"Seq Scan","Total Cost":1.5,"Actual Total Time":0.02,"Actual Rows":10}}]`
	node, err := parsePlan(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.Type != "Seq Scan" {
		t.Errorf("Type = %q, want Seq Scan", node.Type)
	}
	if node.Cost != 1.5 {
		t.Errorf("Cost = %v, want 1.5", node.Cost)
	}
	if node.ActualTime != 0.02 {
		t.Errorf("ActualTime = %v, want 0.02", node.ActualTime)
	}
	if node.Rows != 10 {
		t.Errorf("Rows = %v, want 10", node.Rows)
	}
	if len(node.Children) != 0 {
		t.Errorf("Children len = %v, want 0", len(node.Children))
	}
}

func TestParsePlan_NestedJoin(t *testing.T) {
	raw := `[{"Plan":{"Node Type":"Hash Join","Total Cost":10.0,"Actual Total Time":1.0,"Actual Rows":5,` +
		`"Plans":[` +
		`{"Node Type":"Seq Scan","Total Cost":2.0,"Actual Total Time":0.5,"Actual Rows":5},` +
		`{"Node Type":"Hash","Total Cost":3.0,"Actual Total Time":0.3,"Actual Rows":3}` +
		`]}}]`
	node, err := parsePlan(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.Type != "Hash Join" {
		t.Errorf("Type = %q, want Hash Join", node.Type)
	}
	if len(node.Children) != 2 {
		t.Fatalf("Children len = %v, want 2", len(node.Children))
	}
	if node.Children[0].Type != "Seq Scan" {
		t.Errorf("Children[0] = %q, want Seq Scan", node.Children[0].Type)
	}
	if node.Children[1].Type != "Hash" {
		t.Errorf("Children[1] = %q, want Hash", node.Children[1].Type)
	}
}

func TestParsePlan_ZeroValues(t *testing.T) {
	raw := `[{"Plan":{"Node Type":"Result","Total Cost":0,"Actual Total Time":0,"Actual Rows":0}}]`
	node, err := parsePlan(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.Cost != 0 || node.ActualTime != 0 || node.Rows != 0 {
		t.Errorf("expected zero values, got cost=%v time=%v rows=%v", node.Cost, node.ActualTime, node.Rows)
	}
}

func TestParsePlan_AllNodeTypes(t *testing.T) {
	for _, nt := range []string{
		"Seq Scan", "Index Scan", "Index Only Scan", "Bitmap Heap Scan",
		"Hash Join", "Merge Join", "Nested Loop", "Sort", "Aggregate", "Hash",
	} {
		t.Run(nt, func(t *testing.T) {
			raw := `[{"Plan":{"Node Type":"` + nt + `","Total Cost":1.0,"Actual Total Time":0.01,"Actual Rows":1}}]`
			node, err := parsePlan(raw)
			if err != nil {
				t.Fatalf("parsePlan error: %v", err)
			}
			if node.Type != nt {
				t.Errorf("Type = %q, want %q", node.Type, nt)
			}
		})
	}
}

func TestParsePlan_InvalidJSON(t *testing.T) {
	if _, err := parsePlan("not valid json"); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParsePlan_EmptyArray(t *testing.T) {
	if _, err := parsePlan("[]"); err == nil {
		t.Error("expected error for empty plan array")
	}
}

func TestParsePlan_DeepTree_NoStackOverflow(t *testing.T) {
	plan := `{"Node Type":"Seq Scan","Total Cost":1.0,"Actual Total Time":0.01,"Actual Rows":1}`
	for i := 0; i < 50; i++ {
		plan = `{"Node Type":"Nested Loop","Total Cost":2.0,"Actual Total Time":0.02,"Actual Rows":1,"Plans":[` + plan + `]}`
	}
	node, err := parsePlan(`[{"Plan":` + plan + `}]`)
	if err != nil {
		t.Fatalf("unexpected error at depth 50: %v", err)
	}
	depth := 0
	cur := node
	for {
		depth++
		if len(cur.Children) == 0 {
			break
		}
		child := cur.Children[0]
		cur = &child
	}
	if depth != 51 { // 50 Nested Loop + 1 Seq Scan leaf
		t.Errorf("depth = %v, want 51", depth)
	}
}

// ── Explain function ──────────────────────────────────────────────────────────

func TestExplain_EmptyQuery(t *testing.T) {
	if _, err := Explain(context.Background(), nil, ""); !errors.Is(err, ErrEmptyQuery) {
		t.Errorf("got %v, want ErrEmptyQuery", err)
	}
}

func TestExplain_WhitespaceOnly(t *testing.T) {
	if _, err := Explain(context.Background(), nil, "   \t\n  "); !errors.Is(err, ErrEmptyQuery) {
		t.Errorf("got %v, want ErrEmptyQuery", err)
	}
}

func TestExplain_DMLRejected(t *testing.T) {
	for _, q := range []string{
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET col = 1",
		"DELETE FROM t",
		"DROP TABLE t",
		"ALTER TABLE t ADD COLUMN c int",
		"CREATE TABLE t (id int)",
		"TRUNCATE t",
	} {
		t.Run(q, func(t *testing.T) {
			if _, err := Explain(context.Background(), nil, q); !errors.Is(err, ErrNotSelect) {
				t.Errorf("got %v, want ErrNotSelect", err)
			}
		})
	}
}

func TestExplain_ValidSelect(t *testing.T) {
	db, mock := newExplainDB(t)
	planJSON := `[{"Plan":{"Node Type":"Seq Scan","Total Cost":1.5,"Actual Total Time":0.02,"Actual Rows":1}}]`
	mock.ExpectQuery(`EXPLAIN`).WillReturnRows(sqlmock.NewRows([]string{"QUERY PLAN"}).AddRow(planJSON))

	node, err := Explain(context.Background(), db, "SELECT 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.Type != "Seq Scan" {
		t.Errorf("Type = %q, want Seq Scan", node.Type)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestExplain_JoinQuery_ChildNodes(t *testing.T) {
	db, mock := newExplainDB(t)
	planJSON := `[{"Plan":{"Node Type":"Hash Join","Total Cost":20.0,"Actual Total Time":1.0,"Actual Rows":5,` +
		`"Plans":[{"Node Type":"Seq Scan","Total Cost":5.0,"Actual Total Time":0.3,"Actual Rows":5},` +
		`{"Node Type":"Hash","Total Cost":8.0,"Actual Total Time":0.2,"Actual Rows":3}]}}]`
	mock.ExpectQuery(`EXPLAIN`).WillReturnRows(sqlmock.NewRows([]string{"QUERY PLAN"}).AddRow(planJSON))

	node, err := Explain(context.Background(), db, "SELECT a.id FROM a JOIN b ON a.id = b.id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.Type != "Hash Join" {
		t.Errorf("root Type = %q, want Hash Join", node.Type)
	}
	if len(node.Children) != 2 {
		t.Errorf("Children len = %v, want 2", len(node.Children))
	}
}

func TestExplain_3TableJoin_DeeplyNested(t *testing.T) {
	db, mock := newExplainDB(t)
	planJSON := `[{"Plan":{"Node Type":"Hash Join","Total Cost":30.0,"Actual Total Time":2.0,"Actual Rows":3,` +
		`"Plans":[` +
		`{"Node Type":"Hash Join","Total Cost":15.0,"Actual Total Time":1.0,"Actual Rows":3,` +
		`"Plans":[{"Node Type":"Seq Scan","Total Cost":5.0,"Actual Total Time":0.3,"Actual Rows":5},` +
		`{"Node Type":"Hash","Total Cost":3.0,"Actual Total Time":0.1,"Actual Rows":3,` +
		`"Plans":[{"Node Type":"Seq Scan","Total Cost":2.0,"Actual Total Time":0.1,"Actual Rows":3}]}]},` +
		`{"Node Type":"Hash","Total Cost":5.0,"Actual Total Time":0.5,"Actual Rows":5,` +
		`"Plans":[{"Node Type":"Seq Scan","Total Cost":3.0,"Actual Total Time":0.2,"Actual Rows":5}]}` +
		`]}}]`
	mock.ExpectQuery(`EXPLAIN`).WillReturnRows(sqlmock.NewRows([]string{"QUERY PLAN"}).AddRow(planJSON))

	node, err := Explain(context.Background(), db, "SELECT 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(node.Children) != 2 {
		t.Fatalf("root children = %d, want 2", len(node.Children))
	}
	if len(node.Children[0].Children) != 2 {
		t.Errorf("child[0] children = %d, want 2", len(node.Children[0].Children))
	}
}

func TestExplain_CTEQuery(t *testing.T) {
	db, mock := newExplainDB(t)
	planJSON := `[{"Plan":{"Node Type":"CTE Scan","Total Cost":2.5,"Actual Total Time":0.05,"Actual Rows":1}}]`
	mock.ExpectQuery(`EXPLAIN`).WillReturnRows(sqlmock.NewRows([]string{"QUERY PLAN"}).AddRow(planJSON))

	node, err := Explain(context.Background(), db, "WITH cte AS (SELECT 1) SELECT * FROM cte")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.Type != "CTE Scan" {
		t.Errorf("Type = %q, want CTE Scan", node.Type)
	}
}

func TestExplain_InvalidSQL_ReturnsDBError(t *testing.T) {
	db, mock := newExplainDB(t)
	mock.ExpectQuery(`EXPLAIN`).WillReturnError(errors.New("ERROR: syntax error at or near"))

	if _, err := Explain(context.Background(), db, "SELECT FROM WHERE"); err == nil {
		t.Error("expected error for invalid SQL")
	}
}

func TestExplain_NonexistentTable_ReturnsDBError(t *testing.T) {
	db, mock := newExplainDB(t)
	mock.ExpectQuery(`EXPLAIN`).WillReturnError(
		errors.New(`ERROR: relation "nonexistent_table" does not exist`),
	)

	if _, err := Explain(context.Background(), db, "SELECT * FROM nonexistent_table"); err == nil {
		t.Error("expected error for nonexistent table")
	}
}

func TestExplain_Timeout_ReturnsDeadlineError(t *testing.T) {
	db, mock := newExplainDB(t)
	mock.ExpectQuery(`EXPLAIN`).WillReturnError(context.DeadlineExceeded)

	if _, err := Explain(context.Background(), db, "SELECT 1"); err == nil {
		t.Error("expected timeout error")
	}
}

func TestExplain_JSONOutput_ChildrenOmittedWhenEmpty(t *testing.T) {
	db, mock := newExplainDB(t)
	planJSON := `[{"Plan":{"Node Type":"Seq Scan","Total Cost":1.5,"Actual Total Time":0.02,"Actual Rows":7}}]`
	mock.ExpectQuery(`EXPLAIN`).WillReturnRows(sqlmock.NewRows([]string{"QUERY PLAN"}).AddRow(planJSON))

	node, err := Explain(context.Background(), db, "SELECT 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if out["type"] != "Seq Scan" {
		t.Errorf("JSON type = %v, want Seq Scan", out["type"])
	}
	if _, ok := out["children"]; ok {
		t.Error("children should be omitted when empty (omitempty)")
	}
}
