package main

import (
	"context"
	"fmt"
	"log/slog"
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

	w := worker.New(conf, dbClient, false)

	if conf.Time == "0" {
		slog.Warn("One-time execution")
		w.Run(context.Background())
		return
	}

	// periodic interval
	if err == nil {
		slog.Info(fmt.Sprintf("Scheduled every %s", runDur))

		ticker := time.NewTicker(runDur)
		defer ticker.Stop()

		for range ticker.C {
			w.Run(context.Background())
		}

		return
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
		<-timer.C

		w.Run(context.Background())
		runTime = runTime.AddDate(0, 0, 1)
	}
}
