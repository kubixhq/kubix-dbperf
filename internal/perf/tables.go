package perf

import (
	"context"
	"database/sql"
	"time"
)

const bloatVacuumThreshold = 0.1 // 10% dead tuples → needs vacuum

// Tables returns statistics for all user tables including bloat ratio and
// whether a vacuum is recommended.
func Tables(ctx context.Context, db *sql.DB) ([]TableStat, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	const q = `
		SELECT
			relname,
			seq_scan,
			COALESCE(idx_scan, 0),
			n_live_tup,
			n_dead_tup,
			to_char(last_vacuum,    'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			to_char(last_autovacuum,'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM pg_stat_user_tables
		ORDER BY n_dead_tup DESC`

	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TableStat
	for rows.Next() {
		var (
			ts          TableStat
			lastVac     sql.NullString
			lastAutoVac sql.NullString
		)
		if err := rows.Scan(
			&ts.TableName,
			&ts.SeqScan,
			&ts.IdxScan,
			&ts.LiveRows,
			&ts.DeadRows,
			&lastVac,
			&lastAutoVac,
		); err != nil {
			return nil, err
		}

		// Pick the most recent vacuum (manual or auto).
		switch {
		case lastVac.Valid && lastAutoVac.Valid:
			if lastVac.String >= lastAutoVac.String {
				ts.LastVacuum = &lastVac.String
			} else {
				ts.LastVacuum = &lastAutoVac.String
			}
		case lastVac.Valid:
			ts.LastVacuum = &lastVac.String
		case lastAutoVac.Valid:
			ts.LastVacuum = &lastAutoVac.String
		}

		total := ts.LiveRows + ts.DeadRows
		if total > 0 {
			ts.BloatRatio = float64(ts.DeadRows) / float64(total)
		}
		ts.NeedsVacuum = ts.BloatRatio > bloatVacuumThreshold

		result = append(result, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []TableStat{}
	}
	return result, nil
}
