package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/phishbin/src/config"
	"github.com/phishbin/src/log"
)

type StatusResponse struct {
	FetchAt   string `json:"fetchAt"`
	Errors    int64  `json:"errors"`
	Scheduler string `json:"scheduler"`
}

func Serve(ctx context.Context, conf *config.Config, dbClient *sql.DB) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var jobID, fetchAt string

		err := dbClient.QueryRowContext(r.Context(), `SELECT jobId, fetchAt FROM feed_history ORDER BY id DESC LIMIT 1`).Scan(&jobID, &fetchAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			slog.ErrorContext(r.Context(), err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		resp := StatusResponse{
			FetchAt:   fetchAt,
			Errors:    log.ErrorCount(),
			Scheduler: conf.Time,
		}

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.ErrorContext(r.Context(), "Health response failed", "err", err)
		}
	})

	srv := &http.Server{Addr: conf.HTTPAddr, Handler: mux}
	go func() {
		slog.InfoContext(ctx, "Starting info server", "addr", conf.HTTPAddr)

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server failed", "err", err)
		}
	}()

	return srv
}
