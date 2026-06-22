<div align="center">

<img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License" />
<img src="https://img.shields.io/badge/status-early%20development-orange.svg" alt="Status" />
<img src="https://img.shields.io/badge/go-1.24+-00ADD8.svg" alt="Go Version" />
<img src="https://img.shields.io/badge/PRs-welcome-brightgreen.svg" alt="PRs Welcome" />

# kubix-dbperf

**Database performance insights for your PostgreSQL database.**

Slow query analysis, index health, query plan visualization, and table bloat detection — served as a REST API.

[Getting Started](#getting-started) · [Endpoints](#endpoints) · [Configuration](#configuration) · [Contributing](#contributing)

</div>

---

## What it does

`kubix-dbperf` connects to your PostgreSQL database and exposes four endpoints:

| Endpoint | What it returns |
|----------|----------------|
| `GET /api/perf/slow-queries` | Queries exceeding a mean execution time threshold |
| `GET /api/perf/indexes` | Unused, rarely used, missing, and duplicate indexes |
| `POST /api/perf/explain` | Parsed execution plan for any SELECT query |
| `GET /api/perf/tables` | Table statistics with bloat ratio and vacuum recommendations |

Part of the [Kubix](https://github.com/kubixhq/kubix) observability platform.

---

## Getting started

**Requirements:** Go 1.24+ or Docker, and a PostgreSQL database.

### With Docker

```bash
docker run \
  -e DB_HOST=localhost \
  -e DB_PORT=5432 \
  -e DB_NAME=your_database \
  -e DB_USER=your_user \
  -e DB_PASSWORD=your_password \
  -p 8081:8081 \
  ghcr.io/kubixhq/kubix-dbperf:latest
```

### With Docker Compose

```yaml
services:
  kubix-dbperf:
    image: ghcr.io/kubixhq/kubix-dbperf:latest
    ports:
      - "8081:8081"
    environment:
      DB_HOST: your_db_host
      DB_PORT: 5432
      DB_NAME: your_database
      DB_USER: your_user
      DB_PASSWORD: your_password
```

### From source

```bash
git clone https://github.com/kubixhq/kubix-dbperf.git
cd kubix-dbperf
cp .env.example .env
go run ./cmd/server
```

---

## Endpoints

### `GET /api/perf/slow-queries`

Returns queries whose mean execution time exceeds the threshold, ordered slowest first.

> **Requires** the `pg_stat_statements` extension. See [Enabling pg\_stat\_statements](#enabling-pg_stat_statements).

```bash
# Use the default threshold from config
curl http://localhost:8081/api/perf/slow-queries

# Override threshold per request (ms)
curl "http://localhost:8081/api/perf/slow-queries?threshold=500"
```

```json
[
  {
    "query": "SELECT * FROM orders WHERE status = $1",
    "mean_exec_time": 843.2,
    "calls": 1204,
    "total_exec_time": 1015212.8,
    "cache_hit_ratio": 0.72
  }
]
```

`cache_hit_ratio` is `shared_blks_hit / (shared_blks_hit + shared_blks_read)` — values close to 1.0 indicate effective use of shared buffers.

---

### `GET /api/perf/indexes`

Analyses `pg_stat_user_indexes` and `pg_stat_user_tables` and returns a report with four categories.

```bash
curl http://localhost:8081/api/perf/indexes
```

```json
{
  "unused": [
    {
      "schema_name": "public",
      "table_name": "events",
      "index_name": "idx_events_created_at",
      "idx_scan": 0
    }
  ],
  "rarely_used": [
    {
      "schema_name": "public",
      "table_name": "sessions",
      "index_name": "idx_sessions_token",
      "idx_scan": 3
    }
  ],
  "missing": [
    {
      "schema_name": "public",
      "table_name": "audit_logs",
      "seq_scan": 58420,
      "idx_scan": 12
    }
  ],
  "duplicate": [
    {
      "schema_name": "public",
      "table_name": "users",
      "columns": ["email"],
      "indexes": ["idx_users_email", "uq_users_email"]
    }
  ]
}
```

| Category | Criteria |
|----------|---------|
| `unused` | `idx_scan = 0` since last statistics reset |
| `rarely_used` | `idx_scan < 10` |
| `missing` | `seq_scan > idx_scan` and `seq_scan > 0` |
| `duplicate` | Two or more indexes covering the same column set |

---

### `POST /api/perf/explain`

Runs `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` on a SELECT query and returns the execution plan as a structured tree. Only SELECT and WITH queries are accepted — INSERT, UPDATE, DELETE and DDL are rejected.

```bash
curl -X POST http://localhost:8081/api/perf/explain \
  -H "Content-Type: application/json" \
  -d '{"query": "SELECT u.id, o.total FROM users u JOIN orders o ON o.user_id = u.id WHERE u.active = true"}'
```

```json
{
  "type": "Hash Join",
  "cost": 284.5,
  "actual_time": 12.3,
  "rows": 1840,
  "children": [
    {
      "type": "Seq Scan",
      "cost": 18.5,
      "actual_time": 0.8,
      "rows": 920
    },
    {
      "type": "Hash",
      "cost": 241.0,
      "actual_time": 9.1,
      "rows": 1840,
      "children": [
        {
          "type": "Index Scan",
          "cost": 241.0,
          "actual_time": 7.4,
          "rows": 1840
        }
      ]
    }
  ]
}
```

| Field | Description |
|-------|-------------|
| `type` | Plan node type (Seq Scan, Index Scan, Hash Join, …) |
| `cost` | Planner's estimated total cost |
| `actual_time` | Actual total execution time in ms |
| `rows` | Actual rows returned |
| `children` | Child nodes (present only when non-empty) |

---

### `GET /api/perf/tables`

Returns statistics for all user tables, including bloat ratio and whether a vacuum is recommended.

```bash
curl http://localhost:8081/api/perf/tables
```

```json
[
  {
    "table_name": "events",
    "seq_scan": 120,
    "idx_scan": 8940,
    "live_rows": 1200000,
    "dead_rows": 340000,
    "bloat_ratio": 0.22,
    "last_vacuum": "2025-06-20T03:00:00Z",
    "needs_vacuum": true
  },
  {
    "table_name": "users",
    "seq_scan": 5,
    "idx_scan": 280000,
    "live_rows": 45000,
    "dead_rows": 200,
    "bloat_ratio": 0.004,
    "last_vacuum": "2025-06-22T01:00:00Z",
    "needs_vacuum": false
  }
]
```

Tables are returned ordered by `dead_rows` descending — the worst bloat appears first.

`needs_vacuum` is `true` when `dead_rows / (live_rows + dead_rows) > 10%`. `last_vacuum` is the more recent of `last_vacuum` and `last_autovacuum`.

---

## Enabling pg\_stat\_statements

The `/api/perf/slow-queries` endpoint requires the `pg_stat_statements` extension. Without it, the endpoint returns `503 Service Unavailable`.

**Step 1** — add to `postgresql.conf`:

```
shared_preload_libraries = 'pg_stat_statements'
```

**Step 2** — restart PostgreSQL, then enable the extension:

```sql
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
```

**Step 3** — verify:

```sql
SELECT * FROM pg_stat_statements LIMIT 5;
```

All other endpoints (`/indexes`, `/explain`, `/tables`) work without this extension.

---

## Error responses

All errors return a JSON object with an `error` field:

```json
{ "error": "pg_stat_statements extension required" }
```

| Status | Cause |
|--------|-------|
| `400` | Empty query, non-SELECT query, invalid threshold, invalid JSON |
| `408` | Database query timed out (5 second limit) |
| `503` | Database unavailable or `pg_stat_statements` extension missing |

---

## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DB_HOST` | ✅ | — | PostgreSQL host |
| `DB_PORT` | — | `5432` | PostgreSQL port |
| `DB_NAME` | ✅ | — | Database name |
| `DB_USER` | ✅ | — | Database user |
| `DB_PASSWORD` | ✅ | — | Database password |
| `DB_SSL_MODE` | — | `disable` | `disable` / `require` |
| `SLOW_QUERY_THRESHOLD_MS` | — | `100` | Default threshold for slow query detection |
| `SERVER_PORT` | — | `8081` | HTTP server port |

The `threshold` query parameter on `/api/perf/slow-queries` overrides `SLOW_QUERY_THRESHOLD_MS` per request.

---

## Development

```bash
# Run unit tests (no database required)
go test ./...

# Run with race detector
go test -race ./...

# Run integration tests (requires PostgreSQL)
TEST_DSN="host=localhost port=5432 dbname=postgres user=postgres password=postgres sslmode=disable" \
  go test -tags integration -race ./...

# Build binary
go build -o kubix-dbperf ./cmd/server

# Build Docker image
docker build -t kubix-dbperf .
```

Integration tests skip gracefully when the database is unreachable or when `pg_stat_statements` is not installed.

---

## Contributing

See the org-wide [CONTRIBUTING.md](https://github.com/kubixhq/kubix/blob/main/CONTRIBUTING.md) for guidelines.

Good first issues are tagged [`good first issue`](https://github.com/kubixhq/kubix-catalog/issues?q=is%3Aissue+label%3A%22good+first+issue%22).

---

## License

Apache 2.0 — see [LICENSE](./LICENSE) for details.

---

<div align="center">
Part of <a href="https://github.com/kubixhq/kubix">Kubix</a> — built in public by <a href="https://github.com/kubixhq">kubixhq</a>
</div>
