package worker

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phishbin/src/config"
	"github.com/phishbin/src/db"
	"github.com/phishbin/src/httpc"
	"github.com/phishbin/src/provider"
	providertestutil "github.com/phishbin/src/provider/testutil"
	"github.com/phishbin/src/worker/normalize"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	conf := &config.Config{DataDir: t.TempDir()}
	dbClient, err := db.Connect(conf)
	require.NoError(t, err)
	t.Cleanup(func() { dbClient.Close() })

	providers := []*providertestutil.MockProvider{
		providertestutil.NewMockProvider(t),
		providertestutil.NewMockProvider(t),
	}
	urls := []string{"https://example.com/a", "https://example.org/b"}

	for i, p := range providers {
		p.EXPECT().Name().Return(fmt.Sprintf("mock-provider-%d", i))
		p.EXPECT().Bit().Return(uint32(1 << i))
		p.EXPECT().Fetch(mock.Anything, mock.Anything, mock.Anything).
			Run(func(_ context.Context, _ *httpc.Client, emit func(string)) {
				emit(urls[i])
			}).
			Return(nil)
	}

	service := &service{
		conf:      conf,
		db:        dbClient,
		providers: []provider.Provider{providers[0], providers[1]},
	}
	service.Run(context.Background())

	rows, err := dbClient.Query("SELECT h, pId FROM prev ORDER BY pId")
	require.NoError(t, err)
	defer rows.Close()

	for i, rawURL := range urls {
		require.True(t, rows.Next())
		var gotHash []byte
		var gotPID int64
		require.NoError(t, rows.Scan(&gotHash, &gotPID))

		pattern, err := normalize.Canonical(rawURL)
		require.NoError(t, err)

		hash := normalize.Hash(pattern)
		require.Equal(t, hash[:], gotHash)
		require.Equal(t, int64(1<<i), gotPID)
	}

	require.False(t, rows.Next())
	require.NoError(t, rows.Err())

	diff, err := os.ReadFile(filepath.Join(conf.DataDir, "diff", "chunk-0001.sql"))
	require.NoError(t, err)

	for i, rawURL := range urls {
		pattern, err := normalize.Canonical(rawURL)
		require.NoError(t, err)

		hash := normalize.Hash(pattern)
		statement := fmt.Sprintf("INSERT OR REPLACE INTO abuse_feeds(h, pId) VALUES (X'%s', %d);", hex.EncodeToString(hash[:]), 1<<i)
		require.True(t, strings.Contains(string(diff), statement))
	}
}
