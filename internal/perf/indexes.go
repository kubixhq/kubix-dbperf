package perf

import (
	"context"
	"database/sql"
	"time"

	"github.com/lib/pq"
)

// Indexes analyses pg_stat_user_indexes and pg_stat_user_tables and returns
// a full IndexReport (unused, rarely used, missing, duplicate).
func Indexes(ctx context.Context, db *sql.DB) (*IndexReport, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	unused, rarelyUsed, err := queryUnusedIndexes(ctx, db)
	if err != nil {
		return nil, err
	}

	missing, err := queryMissingIndexes(ctx, db)
	if err != nil {
		return nil, err
	}

	duplicate, err := queryDuplicateIndexes(ctx, db)
	if err != nil {
		return nil, err
	}

	return &IndexReport{
		Unused:     unused,
		RarelyUsed: rarelyUsed,
		Missing:    missing,
		Duplicate:  duplicate,
	}, nil
}

func queryUnusedIndexes(ctx context.Context, db *sql.DB) (unused, rarelyUsed []IndexInfo, err error) {
	const q = `
		SELECT
			schemaname,
			relname,
			indexrelname,
			idx_scan
		FROM pg_stat_user_indexes
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
		ORDER BY idx_scan, schemaname, relname, indexrelname`

	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var info IndexInfo
		if err := rows.Scan(&info.SchemaName, &info.TableName, &info.IndexName, &info.IdxScan); err != nil {
			return nil, nil, err
		}
		switch {
		case info.IdxScan == 0:
			unused = append(unused, info)
		case info.IdxScan < 10:
			rarelyUsed = append(rarelyUsed, info)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if unused == nil {
		unused = []IndexInfo{}
	}
	if rarelyUsed == nil {
		rarelyUsed = []IndexInfo{}
	}
	return unused, rarelyUsed, nil
}

func queryMissingIndexes(ctx context.Context, db *sql.DB) ([]MissingIndex, error) {
	// Tables where sequential scans dominate index scans indicate a missing index.
	const q = `
		SELECT
			schemaname,
			relname,
			seq_scan,
			COALESCE(idx_scan, 0) AS idx_scan
		FROM pg_stat_user_tables
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
		  AND seq_scan > COALESCE(idx_scan, 0)
		  AND seq_scan > 0
		ORDER BY seq_scan DESC`

	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []MissingIndex
	for rows.Next() {
		var m MissingIndex
		if err := rows.Scan(&m.SchemaName, &m.TableName, &m.SeqScan, &m.IdxScan); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []MissingIndex{}
	}
	return result, nil
}

func queryDuplicateIndexes(ctx context.Context, db *sql.DB) ([]DuplicateIndexGroup, error) {
	// Fetch every user index with its ordered column list so we can group duplicates in Go.
	const q = `
		SELECT
			n.nspname,
			t.relname,
			i.relname,
			array_agg(a.attname ORDER BY x.ordinality) AS cols
		FROM pg_index ix
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY AS x(attnum, ordinality) ON true
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = x.attnum
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
		GROUP BY n.nspname, t.relname, i.relname`

	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type key struct{ schema, table, cols string }
	groups := map[key][]string{}
	colsMap := map[key][]string{}

	for rows.Next() {
		var schema, table, idxName string
		var cols pq.StringArray
		if err := rows.Scan(&schema, &table, &idxName, &cols); err != nil {
			return nil, err
		}
		k := key{schema, table, joinStrings([]string(cols))}
		groups[k] = append(groups[k], idxName)
		if _, ok := colsMap[k]; !ok {
			colsMap[k] = []string(cols)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var result []DuplicateIndexGroup
	for k, idxNames := range groups {
		if len(idxNames) > 1 {
			result = append(result, DuplicateIndexGroup{
				SchemaName: k.schema,
				TableName:  k.table,
				Columns:    colsMap[k],
				Indexes:    idxNames,
			})
		}
	}
	if result == nil {
		result = []DuplicateIndexGroup{}
	}
	return result, nil
}

// joinStrings produces a stable string key from a string slice.
func joinStrings(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}
