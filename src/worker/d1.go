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
func (s service) d1Sync(ctx context.Context, diffCount int) error {
	if !s.sync {
		slog.WarnContext(ctx, "Sync is disabled")
		return nil
	}

	chunks, err := filepath.Glob(filepath.Join(s.conf.DataDir, "diff", "chunk-*.sql"))
	if err != nil {
		return err
	}

	if len(chunks) == 0 {
		return nil // No chunks
	}

	sort.Strings(chunks)

	startedAt := time.Now()
	opt := []option.RequestOption{option.WithAPIToken(s.conf.CFD1Token), option.WithMaxRetries(5), option.WithRequestTimeout(30 * time.Second)}
	opt = append(opt, cloudflare.DefaultClientOptions()...)
	client := d1.NewD1Service(opt...)

	slog.InfoContext(ctx, "Sync started")

	// write feeds
	for _, chunk := range chunks {
		diff, err := os.ReadFile(chunk)
		if err != nil {
			return err
		}
		if len(diff) == 0 {
			return fmt.Errorf("diff is empty: %s", chunk)
		}

		_, err = client.Database.Query(ctx, s.conf.CFDatabase, d1.DatabaseQueryParams{
			AccountID: cloudflare.F(s.conf.CFAccountID),
			Body: d1.DatabaseQueryParamsBodyD1SingleQuery{
				Sql: cloudflare.F(string(diff)),
			},
		})
		if err != nil {
			return err
		}
	}

	// write status
	var lastError sql.NullString
	if err := s.db.QueryRow(`
		SELECT group_concat(lastError)
		FROM feed_history
		WHERE jobId = ? AND lastError IS NOT NULL`, s.jobID).Scan(&lastError); err != nil {
		return err
	}

	statusSQL := fmt.Sprintf(`
		INSERT OR REPLACE INTO abuse_feeds_status
		(id, lastSyncAt, rows, providersFailed, durationMs)
		VALUES (1, %d, %d, '%s', %d);`,
		time.Now().Unix(), diffCount,
		strings.ReplaceAll(lastError.String, "'", "''"), time.Since(startedAt).Milliseconds())

	_, err = client.Database.Query(ctx, s.conf.CFDatabase, d1.DatabaseQueryParams{
		AccountID: cloudflare.F(s.conf.CFAccountID),
		Body: d1.DatabaseQueryParamsBodyD1SingleQuery{
			Sql: cloudflare.F(statusSQL),
		},
	})

	slog.InfoContext(ctx, fmt.Sprintf("Sync took %s", time.Since(startedAt)))

	return err
}
