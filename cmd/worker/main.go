package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	"github.com/arikkfir-org/gmail-organizer/internal/util"
)

func runApp() int {

	// Create context that cancels on SIGINT and SIGTERM
	ctx, cancelCtx := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancelCtx()

	// Create app
	app, err := newWorkerApp(ctx)
	if err != nil {
		slog.Error("Failed to initialize app", "err", err)
		return 1
	}
	defer app.Close()

	// Configure logging
	util.ConfigureLogging(app.jsonLogging)

	// Run job
	if err := app.Run(ctx); err != nil {
		slog.Error("Job failed", "err", err)
		return 1
	}

	slog.Info("App completed successfully")
	return 0
}

func main() {
	os.Exit(runApp())
}
