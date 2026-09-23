package worker

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/d1"
	"github.com/cloudflare/cloudflare-go/v7/option"
	"github.com/phishbin/src/config"
	"github.com/phishbin/src/httpc"
	"github.com/phishbin/src/provider"
	"github.com/phishbin/src/provider/urlhaus"
	"github.com/phishbin/src/worker/normalize"
)

// Max chunk size for splitting SQL diff statements into separate files.
const maxChunkSize = 90 * 1024 // keep 90 KB

type Service struct {
	cfg  *config.Config
	db   *sql.DB
	sync bool // sync diff data with D1
}

func New(cfg *config.Config, db *sql.DB, sync bool) *Service {
	return &Service{cfg: cfg, db: db, sync: sync}
}

// Run executes worker loop, fetching, inserting, and sync providers data with D1.
func (s *Service) Run(ctx context.Context) {
	slog.InfoContext(ctx, "Worker started")

	startAt := time.Now()
	defer func() {
		slog.InfoContext(ctx, fmt.Sprintf("Worker took %s", time.Since(startAt)))
	}()

	// TODO: use gorutine
	for _, pd := range []provider.Provider{urlhaus.New(s.cfg.UrlhausKey)} {
		fetchAt := time.Now()

		var insertErr error
		fetchErr := pd.Fetch(ctx, httpc.New(), func(urlData string) {
			if err := s.insert(pd, urlData); err != nil {
				insertErr = err
			}
		})

		s.writeFeedHistory(ctx, pd, errors.Join(fetchErr, insertErr))

		slog.InfoContext(ctx, fmt.Sprintf("%s took %s", pd.Name(), time.Since(fetchAt)))
	}

	if err := s.writeDiff(); err != nil {
		slog.ErrorContext(ctx, "Failed to write diff file", "error", err)
		return
	}

	if err := s.d1Sync(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed to sync diff file with D1", "error", err)
		return
	}

	if err := s.rotate(); err != nil {
		slog.ErrorContext(ctx, "Failed to rotate local state", "error", err)
	}
}

// insert url to DB
func (s Service) insert(pd provider.Provider, urlData string) error {
	pattern, err := normalize.Canonical(urlData)
	if err != nil {
		slog.Error("Invalid URL", "provider", pd.Name(), "url", urlData, "error", err)
		return err
	}

	hash := normalize.Hash(pattern)
	_, err = s.db.Exec(`
		INSERT INTO curr (h, src)
		VALUES (?, ?)
		ON CONFLICT(h) DO UPDATE SET src = curr.src | excluded.src`,
		hash[:], pd.Bit())

	if err != nil {
		slog.Error("Failed to insert URL", "provider", pd.Name(), "url", urlData, "error", err)
	}
	return err
}

// WriteFeedHistory save the current URL's count and last error for a provider.
func (s Service) writeFeedHistory(ctx context.Context, pd provider.Provider, lastError error) {
	var count int

	err := s.db.QueryRow(`SELECT COUNT(*) FROM curr WHERE src & ? != 0`, pd.Bit()).Scan(&count)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to count provider URLs", "provider", pd.Name(), "error", err)
		return
	}

	var errMsg any
	if lastError != nil {
		errMsg = lastError.Error()
	}

	_, err = s.db.Exec(`
		INSERT INTO feed_history (provider, fetchAt, records, last_error) VALUES (?, ?, ?, ?)`,
		pd.Name(), time.Now().Format(time.RFC3339), count, errMsg)

	if err != nil {
		slog.ErrorContext(ctx, "Failed to update feed history", "provider", pd.Name(), "error", err)
	}
}

// write difference of current list and new to Config.DataDir/diff/chunk-*.sql
func (s Service) writeDiff() error {
	diffPath := filepath.Join(s.cfg.DataDir, "diff")
	os.RemoveAll(diffPath)
	os.MkdirAll(diffPath, 0755)

	var file *os.File
	var chunkSize int
	var chunk int

	closeChunk := func() {
		if file != nil {
			_ = file.Close()
		}
	}
	defer closeChunk()

	writeStatement := func(h []byte, src uint32) error {
		qInsert := fmt.Sprintf("INSERT OR REPLACE INTO abuse_feeds(h, src) VALUES (X'%s', %d);\n", hex.EncodeToString(h), src)

		if file == nil || chunkSize+len(qInsert) > maxChunkSize {
			closeChunk()
			chunk++

			var err error
			file, err = os.Create(filepath.Join(diffPath, fmt.Sprintf("chunk-%04d.sql", chunk)))
			if err != nil {
				return err
			}
			chunkSize = 0
		}
		if _, err := file.WriteString(qInsert); err != nil {
			return err
		}

		chunkSize += len(qInsert)
		return nil
	}

	currRows, err := s.db.Query(`
		SELECT h, src FROM curr
		EXCEPT
		SELECT h, src FROM prev`)

	if err != nil {
		return err
	}
	defer currRows.Close()

	var rowsDiff int

	for currRows.Next() {
		rowsDiff++
		var hash []byte
		var src uint32

		if err := currRows.Scan(&hash, &src); err != nil {
			return err
		}

		if err := writeStatement(hash, src); err != nil {
			return err
		}
	}

	if rowsDiff == 0 {
		slog.Info("No diff rows")
	} else if chunk > 0 {
		slog.Info(fmt.Sprintf("Diff rows %d, chunks %d", rowsDiff, chunk))
	}

	return currRows.Err()
}

// import diff.sql to cloudflare D1 database and white status to db
func (s Service) d1Sync(ctx context.Context) error {
	if !s.sync {
		slog.WarnContext(ctx, "Sync is disabled")
		return nil
	}

	chunks, err := filepath.Glob(filepath.Join(s.cfg.DataDir, "diff", "chunk-*.sql"))
	if err != nil {
		return err
	}

	sort.Strings(chunks)
	if len(chunks) == 0 {
		return errors.New("diff is empty")
	}

	startedAt := time.Now()
	client := d1.NewD1Service(option.WithAPIToken(s.cfg.CFD1Token), option.WithMaxRetries(2), option.WithRequestTimeout(30*time.Second))

	for _, chunk := range chunks {
		diff, err := os.ReadFile(chunk)
		if err != nil {
			return err
		}
		if len(diff) == 0 {
			return fmt.Errorf("diff is empty: %s", chunk)
		}
		_, err = client.Database.Query(ctx, s.cfg.CFDatabase, d1.DatabaseQueryParams{
			AccountID: cloudflare.F(s.cfg.CFAccountID),
			Body: d1.DatabaseQueryParamsBodyD1SingleQuery{
				Sql: cloudflare.F(string(diff)),
			},
		})
		if err != nil {
			return err
		}
	}

	var rows int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM curr`).Scan(&rows); err != nil {
		return err
	}

	var sourcesFailed sql.NullString
	if err := s.db.QueryRow(`
		SELECT group_concat(last_error)
		FROM feed_history
		WHERE last_error IS NOT NULL`).Scan(&sourcesFailed); err != nil {
		return err
	}

	statusSQL := fmt.Sprintf(`
		INSERT OR REPLACE INTO abuse_feeds_status
		(id, lastSyncAt, rows, providersFailed, durationMs)
		VALUES (1, %d, %d, '%s', %d);`,
		time.Now().Unix(), rows,
		strings.ReplaceAll(sourcesFailed.String, "'", "''"), time.Since(startedAt).Milliseconds())

	_, err = client.Database.Query(ctx, s.cfg.CFDatabase, d1.DatabaseQueryParams{
		AccountID: cloudflare.F(s.cfg.CFAccountID),
		Body: d1.DatabaseQueryParamsBodyD1SingleQuery{
			Sql: cloudflare.F(statusSQL),
		},
	})

	return err
}

// Rotates the local database schema by swapping current and previous tables.
func (s Service) rotate() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DROP TABLE prev`); err != nil {
		return err
	}

	if _, err := tx.Exec(`ALTER TABLE curr RENAME TO prev`); err != nil {
		return err
	}

	if _, err := tx.Exec(`
		CREATE TABLE curr (
			h BLOB PRIMARY KEY,
			src INTEGER NOT NULL,
			reasons TEXT NOT NULL DEFAULT 'malware'
		) WITHOUT ROWID`); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return os.RemoveAll(filepath.Join(s.cfg.DataDir, "diff"))
}
