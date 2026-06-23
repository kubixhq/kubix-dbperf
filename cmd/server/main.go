package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/kubixhq/kubix-dbperf/internal/config"
	"github.com/kubixhq/kubix-dbperf/internal/db"
	"github.com/kubixhq/kubix-dbperf/internal/handler"
)

func main() {
	cfg := config.Load()

	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	database, err := db.Connect(cfg)
	if err != nil {
		log.Fatalf("cannot connect to database: %v", err)
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		log.Printf("warning: alert table migration failed: %v", err)
	}

	h := handler.New(database, cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /api/perf/slow-queries", h.SlowQueries)
	mux.HandleFunc("GET /api/perf/indexes", h.Indexes)
	mux.HandleFunc("POST /api/perf/explain", h.Explain)
	mux.HandleFunc("GET /api/perf/tables", h.Tables)
	mux.HandleFunc("GET /api/perf/alerts", h.ListAlerts)
	mux.HandleFunc("POST /api/perf/alerts", h.CreateAlert)
	mux.HandleFunc("DELETE /api/perf/alerts/{id}", h.DeleteAlert)
	mux.HandleFunc("GET /api/perf/alerts/check", h.CheckAlerts)

	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	log.Printf("kubix-dbperf listening on %s", addr)
	if err := http.ListenAndServe(addr, cors(mux)); err != nil {
		log.Fatal(err)
	}
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
