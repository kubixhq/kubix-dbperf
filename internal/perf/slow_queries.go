package perf

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

var ErrExtensionRequired = fmt.Errorf("pg_stat_statements extension required")

// SlowQueries returns queries whose mean execution time exceeds thresholdMs,
// ordered from slowest to fastest.
func SlowQueries(ctx context.Context, db *sql.DB, thresholdMs float64) ([]SlowQuery, error) {
	if err := checkStatStatements(ctx, db); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	const q = `
		SELECT
			query,
			mean_exec_time,
			calls,
			total_exec_time,
			CASE
				WHEN (shared_blks_hit + shared_blks_read) = 0 THEN 0
				ELSE shared_blks_hit::float / (shared_blks_hit + shared_blks_read)
			END AS cache_hit_ratio
		FROM pg_stat_statements
		WHERE mean_exec_time > $1
		ORDER BY mean_exec_time DESC`

	rows, err := db.QueryContext(ctx, q, thresholdMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SlowQuery
	for rows.Next() {
		var sq SlowQuery
		if err := rows.Scan(&sq.Query, &sq.MeanExecTime, &sq.Calls, &sq.TotalExecTime, &sq.CacheHitRatio); err != nil {
			return nil, err
		}
		result = append(result, sq)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []SlowQuery{}
	}
	return result, nil
}

func checkStatStatements(ctx context.Context, db *sql.DB) error {
	var exists bool
	err := db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')`).
		Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrExtensionRequired
	}
	return nil
}
