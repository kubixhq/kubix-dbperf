package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kubixhq/kubix-dbperf/internal/config"
	"github.com/kubixhq/kubix-dbperf/internal/perf"
)

type Handler struct {
	db  *sql.DB
	cfg config.Config
}

func New(db *sql.DB, cfg config.Config) *Handler {
	return &Handler{db: db, cfg: cfg}
}

// GET /api/perf/slow-queries?threshold=100
func (h *Handler) SlowQueries(w http.ResponseWriter, r *http.Request) {
	threshold := h.cfg.SlowQueryThresholdMs
	if v := r.URL.Query().Get("threshold"); v != "" {
		t, err := strconv.ParseFloat(v, 64)
		if err != nil || t <= 0 {
			writeError(w, http.StatusBadRequest, "threshold must be a positive number")
			return
		}
		threshold = t
	}

	queries, err := perf.SlowQueries(r.Context(), h.db, threshold)
	if err != nil {
		h.handlePerfError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, queries)
}

// GET /api/perf/indexes
func (h *Handler) Indexes(w http.ResponseWriter, r *http.Request) {
	report, err := perf.Indexes(r.Context(), h.db)
	if err != nil {
		h.handlePerfError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// POST /api/perf/explain
func (h *Handler) Explain(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	node, err := perf.Explain(r.Context(), h.db, body.Query)
	if err != nil {
		switch {
		case errors.Is(err, perf.ErrEmptyQuery):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, perf.ErrNotSelect):
			writeError(w, http.StatusBadRequest, err.Error())
		case isPostgresParseError(err):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			h.handlePerfError(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, node)
}

// GET /api/perf/tables
func (h *Handler) Tables(w http.ResponseWriter, r *http.Request) {
	stats, err := perf.Tables(r.Context(), h.db)
	if err != nil {
		h.handlePerfError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// ── Alert handlers ────────────────────────────────────────────────────────────

type alertRow struct {
	ID          int64   `json:"id"`
	ThresholdMs float64 `json:"thresholdMs"`
	Label       string  `json:"label"`
	Enabled     bool    `json:"enabled"`
	CreatedAt   string  `json:"createdAt"`
}

// GET /api/perf/alerts
func (h *Handler) ListAlerts(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, threshold_ms, label, enabled, created_at::text
		FROM kubix_perf_alerts ORDER BY created_at DESC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list alerts")
		return
	}
	defer rows.Close()
	var alerts []alertRow
	for rows.Next() {
		var a alertRow
		if err := rows.Scan(&a.ID, &a.ThresholdMs, &a.Label, &a.Enabled, &a.CreatedAt); err == nil {
			alerts = append(alerts, a)
		}
	}
	if alerts == nil {
		alerts = []alertRow{}
	}
	writeJSON(w, http.StatusOK, alerts)
}

// POST /api/perf/alerts  { thresholdMs: 500, label: "Slow queries" }
func (h *Handler) CreateAlert(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ThresholdMs float64 `json:"thresholdMs"`
		Label       string  `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ThresholdMs <= 0 {
		writeError(w, http.StatusBadRequest, "thresholdMs must be a positive number")
		return
	}
	var id int64
	err := h.db.QueryRowContext(r.Context(), `
		INSERT INTO kubix_perf_alerts (threshold_ms, label, created_at)
		VALUES ($1, $2, $3) RETURNING id
	`, req.ThresholdMs, req.Label, time.Now().UTC()).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create alert")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "thresholdMs": req.ThresholdMs, "label": req.Label})
}

// DELETE /api/perf/alerts/{id}
func (h *Handler) DeleteAlert(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	_, _ = h.db.ExecContext(r.Context(), `DELETE FROM kubix_perf_alerts WHERE id = $1`, id)
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/perf/alerts/check
// Returns current slow queries that violate any enabled alert threshold.
func (h *Handler) CheckAlerts(w http.ResponseWriter, r *http.Request) {
	type violation struct {
		AlertID     int64   `json:"alertId"`
		ThresholdMs float64 `json:"thresholdMs"`
		Label       string  `json:"label"`
		Query       string  `json:"query"`
		MeanExecMs  float64 `json:"meanExecMs"`
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, threshold_ms, label FROM kubix_perf_alerts WHERE enabled = true
	`)
	if err != nil {
		writeJSON(w, http.StatusOK, []violation{})
		return
	}
	defer rows.Close()

	var alerts []alertRow
	for rows.Next() {
		var a alertRow
		if err := rows.Scan(&a.ID, &a.ThresholdMs, &a.Label); err == nil {
			alerts = append(alerts, a)
		}
	}

	if len(alerts) == 0 {
		writeJSON(w, http.StatusOK, []violation{})
		return
	}

	// find the minimum threshold to use one query
	minThreshold := alerts[0].ThresholdMs
	for _, a := range alerts[1:] {
		if a.ThresholdMs < minThreshold {
			minThreshold = a.ThresholdMs
		}
	}

	queries, err := perf.SlowQueries(r.Context(), h.db, minThreshold)
	if err != nil {
		writeJSON(w, http.StatusOK, []violation{})
		return
	}

	var violations []violation
	for _, q := range queries {
		for _, a := range alerts {
			if q.MeanExecTime >= a.ThresholdMs {
				violations = append(violations, violation{
					AlertID:     a.ID,
					ThresholdMs: a.ThresholdMs,
					Label:       a.Label,
					Query:       q.Query,
					MeanExecMs:  q.MeanExecTime,
				})
				break
			}
		}
	}
	if violations == nil {
		violations = []violation{}
	}
	writeJSON(w, http.StatusOK, violations)
}

func (h *Handler) handlePerfError(w http.ResponseWriter, err error) {
	if errors.Is(err, perf.ErrExtensionRequired) {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if isTimeout(err) {
		writeError(w, http.StatusRequestTimeout, "database query timed out")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "database unavailable")
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "timeout") ||
		strings.Contains(s, "deadline exceeded") ||
		strings.Contains(s, "context deadline")
}

// isPostgresParseError detects syntax errors returned by Postgres when
// EXPLAIN runs against an invalid SQL query.
func isPostgresParseError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "syntax error") ||
		strings.Contains(s, "parse error") ||
		strings.Contains(s, "ERROR:") && strings.Contains(s, "42601") // SQLSTATE syntax_error
}
