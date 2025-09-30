package main

import (
	"context"
	"flag"
	"fmt"
	"gmail-organizer/internal"
	"gmail-organizer/internal/util"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"

	"cloud.google.com/go/firestore"
	"github.com/emersion/go-imap"
)

type workerMode string

const (
	WorkerModeTask workerMode = "task"
	WorkerModeAll  workerMode = "all"
)

type config struct {
	syncJobName       string
	workerMode        workerMode
	cloudRunTaskIndex uint16
	source            internal.TargetConfig
	target            internal.TargetConfig
	jsonLogging       bool
	projectID         string
	dryRun            bool
}

func (c *config) Validate() error {
	if c.syncJobName == "" {
		return fmt.Errorf("sync job name is required")
	}
	if c.workerMode == "" {
		return fmt.Errorf("worker mode is required")
	}
	if err := c.source.Validate(); err != nil {
		return err
	}
	if err := c.target.Validate(); err != nil {
		return err
	}
	if c.projectID == "" {
		return fmt.Errorf("GCP project ID is required")
	}
	return nil
}

func runJob() int {
	defaultWorkerMode := WorkerModeAll
	if v, found := os.LookupEnv("WORKER_MODE"); found {
		switch v {
		case "task":
			defaultWorkerMode = WorkerModeTask
		case "all":
			defaultWorkerMode = WorkerModeAll
		default:
			slog.Error("Invalid worker mode specified in 'WORKER_MODE' environment variable", "mode", v)
			return 1
		}
	}

	var defaultCloudRunTaskIndex uint16 = 0
	if s, found := os.LookupEnv("CLOUD_RUN_TASK_INDEX"); found {
		if v, err := strconv.ParseUint(s, 10, 16); err != nil {
			slog.Error("Invalid value specified in 'CLOUD_RUN_TASK_INDEX' environment variable", "value", s, "err", err)
			return 1
		} else {
			defaultCloudRunTaskIndex = uint16(v)
		}
	}

	defaultDryRun := true
	if s, found := os.LookupEnv("DRY_RUN"); found {
		if v, err := strconv.ParseBool(s); err != nil {
			slog.Error("Invalid value specified in 'DRY_RUN' environment variable", "value", s, "err", err)
			return 1
		} else {
			defaultDryRun = v
		}
	}

	cfg := config{
		syncJobName:       os.Getenv("SYNC_JOB_NAME"),
		workerMode:        defaultWorkerMode,
		cloudRunTaskIndex: defaultCloudRunTaskIndex,
		source: internal.TargetConfig{
			Username: os.Getenv("SOURCE_USERNAME"),
			Password: os.Getenv("SOURCE_PASSWORD"),
		},
		target: internal.TargetConfig{
			Username: os.Getenv("TARGET_USERNAME"),
			Password: os.Getenv("TARGET_PASSWORD"),
		},
		jsonLogging: slices.Contains([]string{"t", "true", "y", "yes", "1", "ok", "on"}, os.Getenv("JSON_LOGGING")),
		projectID:   os.Getenv("GCP_PROJECT_ID"),
		dryRun:      defaultDryRun,
	}
	flag.StringVar(&cfg.syncJobName, "sync-job-name", cfg.syncJobName, "Unique identifier for the job instance.")
	flag.Func("worker-mode", "Worker mode (one of: \"task\" or \"all\")", func(s string) error {
		if s == "task" {
			cfg.workerMode = WorkerModeTask
			return nil
		} else if s == "all" {
			cfg.workerMode = WorkerModeAll
			return nil
		} else {
			return fmt.Errorf("invalid worker mode: %s", s)
		}
	})
	flag.StringVar(&cfg.source.Username, "source-username", cfg.source.Username, "Source Gmail username (email address)")
	flag.StringVar(&cfg.source.Password, "source-password", cfg.source.Password, "Source Gmail app password")
	flag.StringVar(&cfg.target.Username, "target-username", cfg.target.Username, "Target Gmail username (email address)")
	flag.StringVar(&cfg.target.Password, "target-password", cfg.target.Password, "Target Gmail app password")
	flag.BoolVar(&cfg.jsonLogging, "json-logging", cfg.jsonLogging, "Use JSON logging")
	flag.StringVar(&cfg.projectID, "project-id", cfg.projectID, "GCP project ID")
	flag.BoolVar(&cfg.dryRun, "dry-run", cfg.dryRun, "Dry run, do not actually sync messages")
	flag.Func("cloud-run-task-index", "Index of the task (chunk) for this worker to process (only if mode=worker).", func(s string) error {
		if s == "" {
			return fmt.Errorf("value is required")
		} else if v, err := strconv.ParseUint(s, 10, 16); err != nil {
			return fmt.Errorf("invalid value '%s': %w", s, err)
		} else {
			cfg.cloudRunTaskIndex = uint16(v)
			return nil
		}
	})
	flag.Parse()

	// Configure logging
	util.ConfigureLogging(cfg.jsonLogging)

	// Validate required arguments
	if err := cfg.Validate(); err != nil {
		slog.Error("Configuration invalid", "err", err)
		flag.Usage()
		return 1
	}

	// Create context that cancels on SIGINT and SIGTERM
	ctx, cancelCtx := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancelCtx()

	// Create Firestore client
	firestoreClient, err := firestore.NewClient(ctx, cfg.projectID)
	if err != nil {
		slog.Error("Failed to create Firestore client", "err", err)
		return 1
	}
	defer func(firestoreClient *firestore.Client) {
		err := firestoreClient.Close()
		if err != nil {
			slog.Error("Failed to close Firestore client", "err", err)
		}
	}(firestoreClient)

	// Connect to source
	slog.Info("Connecting to source Gmail IMAP server...", "email", cfg.source.Username)
	src, srcCleanup, err := internal.Dial(cfg.source.Username, cfg.source.Password)
	if err != nil {
		slog.Error("Failed to connect to source IMAP server", "err", err)
		return 1
	}
	defer srcCleanup()

	// Connect to target
	slog.Info("Connecting to target Gmail IMAP server...", "email", cfg.target.Username)
	dst, dstCleanup, err := internal.Dial(cfg.target.Username, cfg.target.Password)
	if err != nil {
		slog.Error("Failed to connect to target IMAP server", "err", err)
		return 1
	}
	defer dstCleanup()

	// Find our chunk document
	res, err := firestoreClient.Doc(fmt.Sprintf("syncJobs/%s/chunks/%d", cfg.syncJobName, cfg.cloudRunTaskIndex)).Get(ctx)
	if err != nil {
		slog.Error("Failed fetching chunk", "chunkIndex", cfg.cloudRunTaskIndex, "err", err)
		return 1
	}

	// Extract chunk data
	data := res.Data()
	var mailBoxName string
	var messageIDs []uint32
	if v, ok := data["mailBoxName"]; ok {
		mailBoxName = v.(string)
	} else {
		slog.Error("Missing mailbox name field in Firestore chunk", "chunkIndex", cfg.cloudRunTaskIndex)
		return 1
	}
	if v, ok := data["messageIDs"]; ok {
		messageIDs = v.([]uint32)
	} else {
		slog.Error("Missing message IDs field in Firestore chunk", "chunkIndex", cfg.cloudRunTaskIndex)
		return 1
	}
	slog.Info("Syncing chunk", "mailbox", mailBoxName, "chunkIndex", cfg.cloudRunTaskIndex, "messageCount", len(messageIDs))

	// Ensure target mailbox exists
	if !util.IsGmailSystemLabel(mailBoxName) {
		if err := dst.Create(mailBoxName); err != nil {
			if !strings.Contains(err.Error(), "Duplicate folder name") {
				slog.Error("Failed to create target mailbox", "mailbox", mailBoxName, "err", err)
				return 1
			}
		} else {
			slog.Info("Created target mailbox", "mailbox", mailBoxName)
		}
	}

	// Start background goroutine to fetch messages
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(messageIDs...)
	messages := make(chan *imap.Message, 5000)
	done := make(chan error, 1)
	go func() {
		section := &imap.BodySectionName{}
		items := []imap.FetchItem{section.FetchItem()}
		done <- src.UidFetch(seqSet, items, messages)
	}()

	// Iterate over messages as they arrive from the background goroutine
	for msg := range messages {
		section := &imap.BodySectionName{}
		literal := msg.GetBody(section)
		if literal == nil {
			slog.Error("Failed to get message body", "uid", msg.Uid, "chunkIndex", cfg.cloudRunTaskIndex, "mailbox", mailBoxName)
			continue
		}

		if cfg.dryRun {
			slog.Info("Skipping message copy (in dry-run mode)", "mailbox", mailBoxName, "uid", msg.Uid, "subject", msg.Envelope.Subject)
			continue
		}

		if err := dst.Append(mailBoxName, msg.Flags, msg.Envelope.Date, literal); err != nil {
			slog.Error("Failed to copy message", "mailbox", mailBoxName, "uid", msg.Uid, "subject", msg.Envelope.Subject, "err", err)
			continue
		}

		slog.Debug("Copied message", "mailbox", mailBoxName, "uid", msg.Uid, "subject", msg.Envelope.Subject)
	}

	if err := <-done; err != nil {
		slog.Error("Failed syncing chunk", "mailbox", mailBoxName, "chunkIndex", cfg.cloudRunTaskIndex, "err", err)
		return 1
	}

	return 0
}

func main() {
	os.Exit(runJob())
}
