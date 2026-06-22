package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

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
