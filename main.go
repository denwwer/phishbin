package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/im-kulikov/gonfig"
	"github.com/phishbin/src/config"
	"github.com/phishbin/src/db"
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
		if runDur < 0 {
			slog.Error("Duration must be greater than zero")
			return
		}

		// TODO: is 30 min?
		if runDur > 0 && runDur < 30*time.Minute {
			slog.Error("Duration must be greater than or equal to 30 minutes")
			return
		}
	}

	dbClient, err := db.Connect(conf)
	if err != nil {
		slog.Error("Failed to connect to database", "err", err)
		return
	}
	defer dbClient.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	w := worker.New(conf, dbClient, true)

	if conf.Time == "0" {
		slog.Warn("One-time execution")
		w.Run(ctx)
		return
	}

	// periodic interval
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

	parsedTime, timeErr := time.Parse(runLayout, conf.Time)
	if timeErr != nil {
		slog.Error("Invalid run time", "value", conf.Time, "error", timeErr)
		fmt.Println(gonfig.UsageOfEnvs(conf))
		return
	}

	now := time.Now()
	runTime := time.Date(now.Year(), now.Month(), now.Day(), parsedTime.Hour(), parsedTime.Minute(), 0, 0, now.Location())
	if !runTime.After(now) {
		runTime = runTime.AddDate(0, 0, 1)
	}

	// daily schedule
	slog.Info(fmt.Sprintf("Scheduled daily at %s", conf.Time))
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
