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
	var conf *config.Config
	if err := gonfig.Load(conf); err != nil {
		fmt.Println(gonfig.UsageOfEnvs(conf))
		return
	}

	runDur, err := time.ParseDuration(conf.Time)
	if err == nil && runDur <= 0 {
		slog.Error("Duration must be greater than zero")
		return
	}

	dbClient, err := db.Connect(conf)
	if err != nil {
		slog.Error("Failed to connect to database", "err", err)
		return
	}

	w := worker.New(conf, dbClient, false)

	// periodic interval
	if err == nil {
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
		fmt.Println(gonfig.UsageOfEnvs(&conf))
		return
	}

	now := time.Now()
	runTime := time.Date(now.Year(), now.Month(), now.Day(), parsedTime.Hour(), parsedTime.Minute(), 0, 0, now.Location())
	if !runTime.After(now) {
		runTime = runTime.AddDate(0, 0, 1)
	}

	// daily schedule
	for {
		timer := time.NewTimer(time.Until(runTime))
		<-timer.C

		w.Run(context.Background())
		runTime = runTime.AddDate(0, 0, 1)
	}
}
