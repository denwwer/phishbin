package worker

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/d1"
	"github.com/cloudflare/cloudflare-go/v7/option"
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
	for _, pd := range []provider.Provider{urlhaus.New(s.cfg.UrlhausKey)} {
		fetchAt := time.Now()
		fetchErr := pd.Fetch(ctx, httpc.New(), func(urlData string) {
			s.insert(pd, urlData)
		})

		s.writeFeedMeta(ctx, pd, fetchErr)

		slog.InfoContext(ctx, fmt.Sprintf("%s took %s", pd.Name(), time.Since(fetchAt)))
	}

	if err := s.writeDiff(); err != nil {
		slog.ErrorContext(ctx, "Failed to write diff file: %s", err)
		return
	}

	if err := s.d1Sync(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed to sync diff file with D1: %s", err)
	}
}

// insert url to DB
func (s Service) insert(pd provider.Provider, urlData string) {
	u, err := url.Parse(urlData)
	if err != nil || u.Hostname() == "" || u.Scheme != "https" {
		slog.Error("Invalid URL", "provider", pd.Name(), "url", urlData, "error", err)
		return
	}

	hash := sha256.Sum256([]byte(urlData))
	_, err = s.db.Exec(`
		INSERT INTO curr (h, host, src)
		VALUES (?, ?, ?)
		ON CONFLICT(h) DO UPDATE SET src = curr.src | excluded.src`,
		hash[:], strings.ToLower(u.Hostname()), pd.Bit())

	if err != nil {
		slog.Error("Failed to insert URL", "provider", pd.Name(), "url", urlData, "error", err)
	}
}

func (s Service) writeFeedMeta(ctx context.Context, pd provider.Provider, fetchErr error) {
	var count int

	err := s.db.QueryRow(`SELECT COUNT(*) FROM curr WHERE src & ? != 0`, pd.Bit()).Scan(&count)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to count provider URLs", "provider", pd.Name(), "error", err)
		return
	}

	if fetchErr != nil {
		slog.ErrorContext(ctx, "Failed to fetch provider", "provider", pd.Name(), "error", fetchErr)
		_, err := s.db.Exec(`
				INSERT INTO feed_meta (source, last_modified, last_error) VALUES (?, ?, ?)
				ON CONFLICT(source) DO UPDATE SET last_error = excluded.last_error`, pd.Name(), time.Now().String(), fetchErr.Error())
		if err != nil {
			slog.ErrorContext(ctx, "Failed to update provider metadata", "provider", pd.Name(), "error", err)
		}
		return
	}

	_, err = s.db.Exec(`
				INSERT INTO feed_meta (source, last_modified, last_count, last_error) VALUES (?, ?, ?, NULL)
				ON CONFLICT(source) DO UPDATE SET last_count = excluded.last_count, last_error = NULL`, pd.Name(), time.Now().String(), count)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update provider metadata", "provider", pd.Name(), "error", err)
	}
}

// write difference of current list and new to Config.DataDir/diff.sql
func (s Service) writeDiff() error {
	file, err := os.Create(filepath.Join(s.cfg.DataDir, "diff-0001.sql"))
	if err != nil {
		return err
	}
	defer file.Close()

	currRows, err := s.db.Query(`
		SELECT h, host, src FROM curr
		EXCEPT
		SELECT h, host, src FROM prev`)

	if err != nil {
		return err
	}
	defer currRows.Close()

	for currRows.Next() {
		var hash []byte
		var host string
		var src uint32

		if err := currRows.Scan(&hash, &host, &src); err != nil {
			currRows.Close()
			return err
		}

		if _, err := fmt.Fprintf(file, "INSERT OR REPLACE INTO bad_url(h, host, src) VALUES (X'%s', '%s', %d);\n", hex.EncodeToString(hash), strings.ReplaceAll(host, "'", "''"), src); err != nil {
			currRows.Close()
			return err
		}
	}

	if err := currRows.Err(); err != nil {
		return err
	}

	delRows, err := s.db.Query(`
		SELECT h FROM prev
		EXCEPT
		SELECT h FROM curr`)

	if err != nil {
		return err
	}
	defer delRows.Close()

	for delRows.Next() {
		var hash []byte
		if err := delRows.Scan(&hash); err != nil {
			return err
		}

		if _, err := fmt.Fprintf(file, "DELETE FROM bad_url WHERE h = X'%s';\n", hex.EncodeToString(hash)); err != nil {
			return err
		}
	}

	return delRows.Err()
}

// import diff.sql to cloudflare D1 database and white status to db
func (s Service) d1Sync(ctx context.Context) error {
	if !s.sync {
		slog.WarnContext(ctx, "Sync is disabled")
		return nil
	}

	diff, err := os.ReadFile(filepath.Join(s.cfg.DataDir, "diff-0001.sql"))
	if err != nil {
		return err
	}
	if len(diff) == 0 {
		return errors.New("diff is empty")
	}

	startedAt := time.Now()
	client := d1.NewD1Service(option.WithAPIToken(s.cfg.CFD1Token), option.WithMaxRetries(2), option.WithRequestTimeout(60*time.Second))

	_, err = client.Database.Query(ctx, s.cfg.CFDatabase, d1.DatabaseQueryParams{
		AccountID: cloudflare.F(s.cfg.CFAccountID),
		Body: d1.DatabaseQueryParamsBodyD1SingleQuery{
			Sql: cloudflare.F(string(diff)),
		},
	})
	if err != nil {
		return err
	}

	var rows, added, deleted int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM curr`).Scan(&rows); err != nil {
		return err
	}
	if err := s.db.QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT h, host, src FROM curr
			EXCEPT
			SELECT h, host, src FROM prev
		)`).Scan(&added); err != nil {
		return err
	}
	if err := s.db.QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT h FROM prev
			EXCEPT
			SELECT h FROM curr
		)`).Scan(&deleted); err != nil {
		return err
	}

	var sourcesOK, sourcesFailed sql.NullString
	if err := s.db.QueryRow(`
		SELECT group_concat(CASE WHEN last_error IS NULL THEN source END),
		       group_concat(CASE WHEN last_error IS NOT NULL THEN source END)
		FROM feed_meta`).Scan(&sourcesOK, &sourcesFailed); err != nil {
		return err
	}

	statusSQL := fmt.Sprintf(`
		INSERT OR REPLACE INTO sync_status
		(id, finished_at, rows, added, deleted, sources_ok, sources_failed, duration_ms)
		VALUES (1, %d, %d, %d, %d, '%s', '%s', %d);`,
		time.Now().Unix(), rows, added, deleted,
		strings.ReplaceAll(sourcesOK.String, "'", "''"),
		strings.ReplaceAll(sourcesFailed.String, "'", "''"), time.Since(startedAt).Milliseconds())

	_, err = client.Database.Query(ctx, s.cfg.CFDatabase, d1.DatabaseQueryParams{
		AccountID: cloudflare.F(s.cfg.CFAccountID),
		Body: d1.DatabaseQueryParamsBodyD1SingleQuery{
			Sql: cloudflare.F(statusSQL),
		},
	})

	return err
}
