package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	"github.com/arikkfir-org/gmail-organizer/internal/util"
)

func runJob() int {
	// Create context that cancels on SIGINT and SIGTERM
	ctx, cancelCtx := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancelCtx()

	// Create job
	job, err := newDispatcherJob(ctx)
	if err != nil {
		slog.Error("Failed to initialize job", "err", err)
		return 1
	}
	defer job.Close()

	// Configure logging
	util.ConfigureLogging(job.jsonLogging)

	// Run job
	if err := job.Run(ctx); err != nil {
		slog.Error("Job failed", "err", err)
		return 1
	}

	slog.Info("Job completed successfully")
	return 0
}

func main() {
	os.Exit(runJob())
}
