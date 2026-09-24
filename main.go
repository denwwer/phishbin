package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/im-kulikov/gonfig"
	"github.com/phishbin/src/api"
	"github.com/phishbin/src/config"
	"github.com/phishbin/src/db"
	_ "github.com/phishbin/src/log"
	"github.com/phishbin/src/worker"
)

const runLayout = "15:04" // hh:mm

func main() {
	// parse env's
	conf := &config.Config{}
	if err := gonfig.Load(conf); err != nil {
		fmt.Println(gonfig.UsageOfEnvs(conf))
		return
	}

	runDur, err := time.ParseDuration(conf.Time)
	if err == nil {
		if err := validateRunDuration(runDur); err != nil {
			slog.Error(err.Error())
			return
		}
	}

	dbClient, err := db.Connect(conf)
	if err != nil {
		slog.Error("Failed to connect to database", "err", err)
		return
	}
	defer dbClient.Close() //nolint:errcheck

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := api.Serve(ctx, conf, dbClient)

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	w := worker.New(conf, dbClient, true)
	runSchedule(ctx, conf.Time, w)
}

func validateRunDuration(runDur time.Duration) error {
	if runDur < 0 {
		return errors.New("duration must be greater than zero")
	}
	if runDur > 0 && runDur < 30*time.Minute {
		return errors.New("duration must be greater than or equal to 30 minutes")
	}
	return nil
}

func runSchedule(ctx context.Context, value string, w worker.Service) {
	runDur, err := time.ParseDuration(value)
	if err == nil && value == "0" {
		slog.Warn("One-time execution")
		w.Run(ctx)
		return
	}

	if err == nil {
		slog.Info(fmt.Sprintf("Scheduled every %s", runDur))

		ticker := time.NewTicker(runDur)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.Run(ctx)
			}
		}
	}

	parsedTime, timeErr := time.Parse(runLayout, value)
	if timeErr != nil {
		slog.Error("Invalid run time", "value", value, "error", timeErr)
		return
	}

	now := time.Now()
	runTime := nextRunTime(now, parsedTime.Hour(), parsedTime.Minute())

	slog.Info("Scheduled daily at " + value)
	for {
		timer := time.NewTimer(time.Until(runTime))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		w.Run(ctx)
		runTime = runTime.AddDate(0, 0, 1)
	}
}

func nextRunTime(now time.Time, hour, minute int) time.Time {
	runTime := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if !runTime.After(now) {
		runTime = runTime.AddDate(0, 0, 1)
	}
	return runTime
}
