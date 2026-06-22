package perf

// SlowQuery represents one row from pg_stat_statements filtered by threshold.
type SlowQuery struct {
	Query         string  `json:"query"`
	MeanExecTime  float64 `json:"mean_exec_time"`
	Calls         int64   `json:"calls"`
	TotalExecTime float64 `json:"total_exec_time"`
	CacheHitRatio float64 `json:"cache_hit_ratio"`
}

// IndexInfo describes a single index.
type IndexInfo struct {
	SchemaName string `json:"schema_name"`
	TableName  string `json:"table_name"`
	IndexName  string `json:"index_name"`
	IdxScan    int64  `json:"idx_scan"`
}

// DuplicateIndexGroup holds a set of indexes that cover the same columns.
type DuplicateIndexGroup struct {
	SchemaName string   `json:"schema_name"`
	TableName  string   `json:"table_name"`
	Columns    []string `json:"columns"`
	Indexes    []string `json:"indexes"`
}

// MissingIndex describes a table that likely needs an index.
type MissingIndex struct {
	SchemaName string `json:"schema_name"`
	TableName  string `json:"table_name"`
	SeqScan    int64  `json:"seq_scan"`
	IdxScan    int64  `json:"idx_scan"`
}

// IndexReport is the full response for /api/perf/indexes.
type IndexReport struct {
	Unused     []IndexInfo           `json:"unused"`
	RarelyUsed []IndexInfo           `json:"rarely_used"`
	Missing    []MissingIndex        `json:"missing"`
	Duplicate  []DuplicateIndexGroup `json:"duplicate"`
}

// ExplainNode is one node in the EXPLAIN plan tree.
type ExplainNode struct {
	Type       string        `json:"type"`
	Cost       float64       `json:"cost"`
	ActualTime float64       `json:"actual_time"`
	Rows       int64         `json:"rows"`
	Children   []ExplainNode `json:"children,omitempty"`
}

// TableStat is one row for /api/perf/tables.
type TableStat struct {
	TableName   string  `json:"table_name"`
	SeqScan     int64   `json:"seq_scan"`
	IdxScan     int64   `json:"idx_scan"`
	LiveRows    int64   `json:"live_rows"`
	DeadRows    int64   `json:"dead_rows"`
	BloatRatio  float64 `json:"bloat_ratio"`
	LastVacuum  *string `json:"last_vacuum"`
	NeedsVacuum bool    `json:"needs_vacuum"`
}
