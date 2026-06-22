package perf

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

var ErrNotSelect = fmt.Errorf("only SELECT queries are allowed")
var ErrEmptyQuery = fmt.Errorf("query must not be empty")

// Explain runs EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) on the given SELECT
// query and returns the plan as an ExplainNode tree.
func Explain(ctx context.Context, db *sql.DB, query string) (*ExplainNode, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrEmptyQuery
	}
	if !isSelectQuery(query) {
		return nil, ErrNotSelect
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var raw string
	err := db.QueryRowContext(ctx,
		fmt.Sprintf("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) %s", query)).
		Scan(&raw)
	if err != nil {
		return nil, err
	}

	return parsePlan(raw)
}

func isSelectQuery(q string) bool {
	upper := strings.ToUpper(strings.TrimSpace(q))
	for _, dml := range []string{"INSERT", "UPDATE", "DELETE", "TRUNCATE", "DROP", "ALTER", "CREATE"} {
		if strings.HasPrefix(upper, dml) {
			return false
		}
	}
	return strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH")
}

// pgPlan mirrors the JSON shape that PostgreSQL returns for FORMAT JSON plans.
type pgPlan struct {
	NodeType          string   `json:"Node Type"`
	TotalCost         float64  `json:"Total Cost"`
	ActualTotalTime   float64  `json:"Actual Total Time"`
	ActualRows        int64    `json:"Actual Rows"`
	Plans             []pgPlan `json:"Plans"`
}

type pgPlanWrapper struct {
	Plan pgPlan `json:"Plan"`
}

func parsePlan(raw string) (*ExplainNode, error) {
	var wrappers []pgPlanWrapper
	if err := json.Unmarshal([]byte(raw), &wrappers); err != nil {
		return nil, fmt.Errorf("parse explain output: %w", err)
	}
	if len(wrappers) == 0 {
		return nil, fmt.Errorf("empty explain output")
	}
	node := convertNode(wrappers[0].Plan)
	return &node, nil
}

func convertNode(p pgPlan) ExplainNode {
	node := ExplainNode{
		Type:       p.NodeType,
		Cost:       p.TotalCost,
		ActualTime: p.ActualTotalTime,
		Rows:       p.ActualRows,
	}
	for _, child := range p.Plans {
		node.Children = append(node.Children, convertNode(child))
	}
	return node
}
