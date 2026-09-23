package db

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/phishbin/src/config"
	_ "modernc.org/sqlite"
)

const dbName = "phishbin"

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
		return nil, err
	}

	if err = migrationsUp(); err != nil {
		return nil, err
	}

	return client, nil
}

func dsn(dataPath string) string {
	return fmt.Sprintf("file:%s.sqlite?_txlock=immediate&_pragma=journal_mode(WAL)", filepath.Join(dataPath, dbName))
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
func Vacuum() error {
	var updatedAt int64

	err := client.QueryRow(`SELECT updated_at FROM settings where name = ?`, "vacuum").Scan(&updatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// vacuum every 7 days
	if updatedAt == 0 || time.Unix(0, updatedAt).Before(time.Now().AddDate(0, 0, -7)) {
		_, err = client.Exec("PRAGMA incremental_vacuum(1000)") // Release pages
		if err != nil {
			return err
		}

		now := time.Now().UnixNano()
		_, err = client.Exec(`
		INSERT INTO settings (name, created_at, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET updated_at = ?`, "vacuum", now, now, now, now)
	}

	return err
}

func Size() (int64, error) {
	var size sql.NullInt64
	err := client.QueryRow(`SELECT page_count * page_size AS size_bytes FROM pragma_page_count(), pragma_page_size();`).Scan(&size)
	if err != nil {
		return 0, err
	}

	if size.Valid {
		return size.Int64, nil
	}

	return 0, nil
}
