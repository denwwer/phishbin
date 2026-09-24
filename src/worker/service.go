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
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/phishbin/src/config"
	"github.com/phishbin/src/db"
	"github.com/phishbin/src/httpc"
	"github.com/phishbin/src/log"
	"github.com/phishbin/src/provider"
	"github.com/phishbin/src/provider/openphish"
	"github.com/phishbin/src/provider/phishingdb"
	"github.com/phishbin/src/provider/phishtank"
	"github.com/phishbin/src/provider/tweetfeed"
	"github.com/phishbin/src/provider/urlhaus"
	"github.com/phishbin/src/worker/normalize"
)

// Max chunk size for splitting SQL diff statements into separate files.
const maxChunkSize = 90 * 1024 // keep 90 KB

type Service interface {
	Run(ctx context.Context)
}

type service struct {
	conf      *config.Config
	db        *sql.DB
	sync      bool // sync diff data with D1
	jobID     string
	providers []provider.Provider
}

func New(conf *config.Config, db *sql.DB, sync bool) Service {
	return &service{
		conf:      conf,
		db:        db,
		sync:      sync,
		providers: []provider.Provider{urlhaus.New(conf.UrlhausKey), openphish.New(), phishtank.New(), tweetfeed.New(), phishingdb.New()},
	}
}

// Run executes worker loop, fetching, inserting, and sync providers data with D1.
func (s *service) Run(ctx context.Context) {
	log.ResetErrorCount()
	slog.InfoContext(ctx, "Worker started")

	startAt := time.Now()
	defer func() {
		slog.InfoContext(ctx, fmt.Sprintf("Worker took %s", time.Since(startAt)))
	}()

	cl := httpc.New(s.conf.CacheDir)
	wg := sync.WaitGroup{}
	s.jobID = uuid.NewString()

	for _, pd := range s.providers {
		wg.Go(func() {
			fetchAt := time.Now()

			var insertErr error
			fetchErr := pd.Fetch(ctx, cl, func(urlData string) {
				if err := s.insert(ctx, pd, urlData); err != nil {
					insertErr = err
				}
			})

			s.writeFeedHistory(ctx, pd, errors.Join(fetchErr, insertErr))

			slog.InfoContext(ctx, fmt.Sprintf("%s took %s", pd.Name(), time.Since(fetchAt)))
		})
	}

	wg.Wait()

	diffCount, err := s.writeDiff()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to write diff file", "error", err)
		return
	}

	if err := s.d1Sync(ctx, diffCount); err != nil {
		slog.ErrorContext(ctx, "Failed to sync with D1", "error", err)
		return
	}

	if err := s.rotate(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed to rotate local state", "error", err)
	}
}

// insert url to DB
func (s service) insert(ctx context.Context, pd provider.Provider, rawURL string) error {
	pattern, err := normalize.Canonical(rawURL)
	if err != nil {
		slog.ErrorContext(ctx, "Invalid URL", "provider", pd.Name(), "url", rawURL, "error", err)
		return fmt.Errorf("%s: %s", err.Error(), rawURL)
	}

	hash := normalize.Hash(pattern)

	db.RetryQuery(ctx, func() error {
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO curr (h, pId)
			VALUES (?, ?)
			ON CONFLICT(h) DO UPDATE SET pId = curr.pId | excluded.pId`,
			hash[:], pd.Bit())
		return err
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to insert URL", "provider", pd.Name(), "url", rawURL, "error", err)
	}
	return err
}

// WriteFeedHistory save the current URL's count and last error for a provider.
func (s service) writeFeedHistory(ctx context.Context, pd provider.Provider, lastError error) {
	var count int

	err := s.db.QueryRow(`SELECT COUNT(*) FROM curr WHERE pId & ? != 0`, pd.Bit()).Scan(&count)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to count provider URLs", "provider", pd.Name(), "error", err)
		return
	}

	var errMsg any
	if lastError != nil {
		errMsg = lastError.Error()
	}

	_, err = s.db.Exec(`
		INSERT INTO feed_history (jobId, provider, pId, fetchAt, records, lastError) VALUES (?, ?, ?, ?, ?, ?)`,
		s.jobID, pd.Name(), pd.Bit(), time.Now().Format(time.RFC3339), count, errMsg)

	if err != nil {
		slog.ErrorContext(ctx, "Failed to update feed history", "provider", pd.Name(), "error", err)
	}
}

// write difference of current list and new to Config.DataDir/diff/chunk-*.sql.
// Returns total diff records.
func (s service) writeDiff() (int, error) {
	diffPath := filepath.Join(s.conf.DataDir, "diff")
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

	writeStatement := func(h []byte, pId uint32) error {
		qInsert := fmt.Sprintf("INSERT OR REPLACE INTO abuse_feeds(h, pId) VALUES (X'%s', %d);\n", hex.EncodeToString(h), pId)

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
		SELECT h, pId FROM curr
		EXCEPT
		SELECT h, pId FROM prev`)

	if err != nil {
		return 0, err
	}
	defer currRows.Close()

	var rowsDiff int

	for currRows.Next() {
		rowsDiff++
		var hash []byte
		var pId uint32

		if err := currRows.Scan(&hash, &pId); err != nil {
			return 0, err
		}

		if err := writeStatement(hash, pId); err != nil {
			return 0, err
		}
	}

	if rowsDiff == 0 {
		slog.Info(fmt.Sprintf("No diff rows, chunks %d", chunk))
	} else if chunk > 0 {
		slog.Info(fmt.Sprintf("Diff rows %d, chunks %d", rowsDiff, chunk))
	}

	return rowsDiff, currRows.Err()
}

// rotate atomically SQLite database schema.
// If sync is enabled, it removes the local diff files.
func (s service) rotate(ctx context.Context) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DROP TABLE prev`); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `ALTER TABLE curr RENAME TO prev`); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE curr (
			h BLOB PRIMARY KEY,
			pId INTEGER NOT NULL,
			reasons TEXT NOT NULL DEFAULT 'malware'
		) WITHOUT ROWID`); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	if s.sync {
		return os.RemoveAll(filepath.Join(s.conf.DataDir, "diff"))
	}

	db.Vacuum(ctx)

	return nil
}
