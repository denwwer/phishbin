package worker

import (
	"context"
	"database/sql"
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
)

// import diff-*.sql to cloudflare D1 database.
func (s Service) d1Sync(ctx context.Context) error {
	if !s.sync {
		slog.WarnContext(ctx, "Sync is disabled")
		return nil
	}

	chunks, err := filepath.Glob(filepath.Join(s.cfg.DataDir, "diff", "chunk-*.sql"))
	if err != nil {
		return err
	}

	if len(chunks) == 0 {
		return nil // No chunks
	}

	sort.Strings(chunks)

	startedAt := time.Now()
	opt := []option.RequestOption{option.WithAPIToken(s.cfg.CFD1Token), option.WithMaxRetries(2), option.WithRequestTimeout(30 * time.Second)}
	opt = append(opt, cloudflare.DefaultClientOptions()...)
	client := d1.NewD1Service(opt...)

	// write feeds
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

	// write status
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
