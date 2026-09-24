package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/phishbin/src/config"
	"modernc.org/sqlite"
	_ "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const (
	dbName    = "phishbin"
	dbAttempt = 5
)

//go:embed migrations/*
var fs embed.FS

// client for database connection.
var client *sql.DB

// Error code that indicates a UNIQUE constraint was violated during an INSERT or UPDATE operation.
const SQLiteConstraintUnique = "2067"

func Connect(conf *config.Config) (*sql.DB, error) {
	var err error

	if info, err := os.Stat(conf.DataDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("data directory %s not exists", conf.DataDir)
	}

	client, err = sql.Open("sqlite", dsn(conf.DataDir))
	if err != nil {
		return nil, err
	}

	if err = client.Ping(); err != nil {
		_ = client.Close()
		return nil, err
	}

	if err = migrationsUp(); err != nil {
		_ = client.Close()
		return nil, err
	}

	return client, nil
}

func dsn(dataPath string) string {
	return fmt.Sprintf("file:%s.sqlite?_txlock=immediate&_busy_timeout=5000&_pragma=journal_mode(WAL)", filepath.Join(dataPath, dbName))
}

func migrationsUp() error {
	src, err := iofs.New(fs, "migrations")
	if err != nil {
		return err
	}
	defer src.Close() //nolint:errcheck

	driver, err := migratesqlite.WithInstance(client, &migratesqlite.Config{})
	if err != nil {
		return err
	}

	m, err := migrate.NewWithInstance("iofs", src, "sqlite", driver)
	if err != nil {
		return err
	}

	err = m.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}

	return nil
}

// Vacuum to reclaim unused space after work done
// https://www.sqlite.org/lang_vacuum.html
func Vacuum(ctx context.Context) {
	_, err := client.ExecContext(ctx, "PRAGMA incremental_vacuum(1000)") // Release pages
	if err != nil {
		slog.ErrorContext(ctx, "Vacuum failed", "err", err)
	}
}

// Returns the total database size in bytes.
func Size(ctx context.Context) (int64, error) {
	var size sql.NullInt64
	err := client.QueryRowContext(ctx, `SELECT page_count * page_size AS size_bytes FROM pragma_page_count(), pragma_page_size();`).Scan(&size)
	if err != nil {
		return 0, err
	}

	if size.Valid {
		return size.Int64, nil
	}

	return 0, nil
}

// RetryQuery retries the provided function with exponential backoff on busy errors.
func RetryQuery(ctx context.Context, stmFunc func() error) {
	for attempt := 0; attempt < dbAttempt; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if attempt > 0 {
			time.Sleep(time.Millisecond << attempt)
		}

		err := stmFunc()
		if err == nil || !isBusy(err) {
			break
		}
	}
}

// Returns true if the error represents a database busy lock.
func isBusy(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code()&0xff == sqlite3.SQLITE_BUSY
}
