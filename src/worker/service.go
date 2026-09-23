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
	"time"

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
		INSERT INTO curr (h, pId)
		VALUES (?, ?)
		ON CONFLICT(h) DO UPDATE SET pId = curr.pId | excluded.pId`,
		hash[:], pd.Bit())

	if err != nil {
		slog.Error("Failed to insert URL", "provider", pd.Name(), "url", urlData, "error", err)
	}
	return err
}

// WriteFeedHistory save the current URL's count and last error for a provider.
func (s Service) writeFeedHistory(ctx context.Context, pd provider.Provider, lastError error) {
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
		INSERT INTO feed_history (provider, pId, fetchAt, records, last_error) VALUES (?, ?, ?, ?, ?)`,
		pd.Name(), pd.Bit(), time.Now().Format(time.RFC3339), count, errMsg)

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
		return err
	}
	defer currRows.Close()

	var rowsDiff int

	for currRows.Next() {
		rowsDiff++
		var hash []byte
		var pId uint32

		if err := currRows.Scan(&hash, &pId); err != nil {
			return err
		}

		if err := writeStatement(hash, pId); err != nil {
			return err
		}
	}

	if rowsDiff == 0 {
		slog.Info(fmt.Sprintf("No diff rows, chunks %d", chunk))
	} else if chunk > 0 {
		slog.Info(fmt.Sprintf("Diff rows %d, chunks %d", rowsDiff, chunk))
	}

	return currRows.Err()
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
			pId INTEGER NOT NULL,
			reasons TEXT NOT NULL DEFAULT 'malware'
		) WITHOUT ROWID`); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return os.RemoveAll(filepath.Join(s.cfg.DataDir, "diff"))
}
