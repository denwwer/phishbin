package worker

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/phishbin/src/config"
	"github.com/phishbin/src/httpc"
	"github.com/phishbin/src/provider"
	"github.com/phishbin/src/provider/urlhaus"
)

type Service struct {
	cfg  *config.Config
	db   *sql.DB
	sync bool // sync diff data with D1
}

func New(cfg *config.Config, db *sql.DB, sync bool) *Service {
	return &Service{cfg: cfg, db: db, sync: sync}
}

func (s *Service) Run(ctx context.Context) {
	slog.InfoContext(ctx, "Worker started")

	startAt := time.Now()
	defer func() {
		slog.InfoContext(ctx, fmt.Sprintf("Worker took %s", time.Since(startAt)))
	}()

	// TODO: use gorutine
	for _, p := range []provider.Provider{urlhaus.New(s.cfg.UrlhausKey)} {

		fetchAt := time.Now()
		err := p.Fetch(ctx, httpc.New(), func(urlData string) {
			s.insert(p.Name(), urlData)
		})

		if err != nil {
			slog.ErrorContext(ctx, "Failed to fetch provider %s: %s", p.Name(), err)
		}

		slog.InfoContext(ctx, fmt.Sprintf("%s took %s", p.Name(), time.Since(fetchAt)))
	}

	if err := s.writeDiff(); err != nil {
		slog.ErrorContext(ctx, "Failed to write diff.sql: %s", err)
		return
	}

	s.d1Sync()
}

// insert url to DB
func (s Service) insert(provider string, urlData string) {
	panic("implement me")
}

// write difference of current list and new to Config.DataDir/diff.sql
func (s Service) writeDiff() error {
	panic("implement me")
}

// import diff.sql to cloudflare D1 database and white status to db
func (s Service) d1Sync() {
	if !s.sync {
		return
	}

	panic("implement me")
}
