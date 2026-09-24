package main

import (
	"context"
	"testing"
	"time"

	"github.com/phishbin/src/worker/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestValidateRunDuration(t *testing.T) {
	tests := []struct {
		name string
		dur  time.Duration
		err  bool
	}{
		{name: "negative", dur: -time.Minute, err: true},
		{name: "too short", dur: 29 * time.Minute, err: true},
		{name: "minimum", dur: 30 * time.Minute},
		{name: "zero", dur: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRunDuration(tt.dur)
			if tt.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRunScheduleOneTimeRunsService(t *testing.T) {
	w := testutil.NewMockService(t)
	w.EXPECT().Run(mock.Anything).Once()

	runSchedule(context.Background(), "0", w)
}

func TestNextRunTimeSchedulesTomorrowWhenTimePassed(t *testing.T) {
	now := time.Date(2026, time.September, 24, 15, 0, 0, 0, time.UTC)
	want := time.Date(2026, time.September, 25, 14, 30, 0, 0, time.UTC)

	got := nextRunTime(now, 14, 30)

	require.Equal(t, want, got)
}
