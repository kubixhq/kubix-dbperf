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

	h := handler.New(database, cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/perf/slow-queries", h.SlowQueries)
	mux.HandleFunc("GET /api/perf/indexes", h.Indexes)
	mux.HandleFunc("POST /api/perf/explain", h.Explain)
	mux.HandleFunc("GET /api/perf/tables", h.Tables)

	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	log.Printf("kubix-dbperf listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
