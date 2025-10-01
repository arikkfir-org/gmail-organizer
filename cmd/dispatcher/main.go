package main

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"gmail-organizer/internal"
	"gmail-organizer/internal/util"
	"log/slog"
	"maps"
	"os"
	"os/signal"
	"slices"
	"sync"

	"cloud.google.com/go/firestore"
	run "cloud.google.com/go/run/apiv2"
	"cloud.google.com/go/run/apiv2/runpb"
)

type config struct {
	syncJobName     string
	source          internal.TargetConfig
	target          internal.TargetConfig
	jsonLogging     bool
	projectID       string
	workerJobName   string
	workerJobRegion string
}

func (c *config) Validate() error {
	if c.syncJobName == "" {
		return fmt.Errorf("sync job name is required")
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
	if c.workerJobName == "" {
		return fmt.Errorf("worker job name is required")
	}
	if c.workerJobRegion == "" {
		return fmt.Errorf("worker job region is required")
	}
	return nil
}

func runJob() int {
	cfg := config{
		syncJobName: os.Getenv("SYNC_JOB_NAME"),
		source: internal.TargetConfig{
			Username: os.Getenv("SOURCE_USERNAME"),
			Password: os.Getenv("SOURCE_PASSWORD"),
		},
		target: internal.TargetConfig{
			Username: os.Getenv("TARGET_USERNAME"),
			Password: os.Getenv("TARGET_PASSWORD"),
		},
		jsonLogging:     slices.Contains([]string{"t", "true", "y", "yes", "1", "ok", "on"}, os.Getenv("JSON_LOGGING")),
		projectID:       os.Getenv("GCP_PROJECT_ID"),
		workerJobName:   os.Getenv("WORKER_JOB_NAME"),
		workerJobRegion: cmp.Or(os.Getenv("WORKER_JOB_REGION"), "me-west1"),
	}
	flag.StringVar(&cfg.syncJobName, "sync-job-name", cfg.syncJobName, "Unique identifier for the job instance.")
	flag.StringVar(&cfg.source.Username, "source-username", cfg.source.Username, "Source Gmail username (email address)")
	flag.StringVar(&cfg.source.Password, "source-password", cfg.source.Password, "Source Gmail app password")
	flag.StringVar(&cfg.target.Username, "target-username", cfg.target.Username, "Target Gmail username (email address)")
	flag.StringVar(&cfg.target.Password, "target-password", cfg.target.Password, "Target Gmail app password")
	flag.BoolVar(&cfg.jsonLogging, "json-logging", cfg.jsonLogging, "Use JSON logging")
	flag.StringVar(&cfg.projectID, "project-id", cfg.projectID, "GCP project ID")
	flag.StringVar(&cfg.workerJobName, "worker-job-name", cfg.workerJobName, "Name of Cloud Run worker job to execute")
	flag.StringVar(&cfg.workerJobRegion, "worker-job-region", cfg.workerJobRegion, "Region of Cloud Run worker job to execute")
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

	// Create Cloud Run client
	runClient, err := run.NewJobsClient(ctx)
	if err != nil {
		slog.Error("Failed to create Google Cloud Run client", "err", err)
		return 1
	}
	defer func() {
		if err := runClient.Close(); err != nil {
			slog.Error("Failed to close Google Cloud Run client", "err", err)
		}
	}()

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

	// Collect message counts for each mailbox in source & target (in parallel)
	var sourceMailBoxInfos, targetMailBoxInfos []internal.MailboxInfo
	mailboxesWG := sync.WaitGroup{}
	mailboxesWG.Go(func() {
		slog.Info("Listing source mailboxes...")
		mailboxes, err := internal.FetchMailboxes(ctx, src)
		if err != nil {
			slog.Error("Failed to fetch source mailboxes", "err", err)
		} else {
			sourceMailBoxInfos = mailboxes
		}
	})
	mailboxesWG.Go(func() {
		slog.Info("Listing target mailboxes...")
		mailboxes, err := internal.FetchMailboxes(ctx, dst)
		if err != nil {
			slog.Error("Failed to fetch target mailboxes", "err", err)
		} else {
			targetMailBoxInfos = mailboxes
		}
	})
	mailboxesWG.Wait()
	if sourceMailBoxInfos == nil || targetMailBoxInfos == nil {
		return 1
	}

	// Determine which mailboxes exist in both source and target
	mailboxes := make(map[string]bool, len(sourceMailBoxInfos))
	for _, sourceMailBoxInfo := range sourceMailBoxInfos {
		mailboxes[sourceMailBoxInfo.Name] = false
		for _, targetMailBoxInfo := range targetMailBoxInfos {
			if sourceMailBoxInfo.Name == targetMailBoxInfo.Name {
				mailboxes[sourceMailBoxInfo.Name] = true
				break
			}
		}
	}

	// Collect message chunks from all mailboxes (flattened) and store them in Firestore
	var chunkIndex int
	for mailBoxName, existsInTarget := range mailboxes {
		slog.Info("Identifying messages that need to be synced to target", "mailbox", mailBoxName)

		messageIDsInSourceMailBox, err := internal.FetchAllMessageUids(src, mailBoxName)
		if err != nil {
			slog.Error("Failed listing messages in source mailbox", "mailbox", mailBoxName, "err", err)
			return 1
		}

		// Obtain list of messages in target MISSING from source mailbox
		var messageIDsToSync []uint32 = nil
		if existsInTarget {
			messageIDsInTargetMailBox, err := internal.FetchAllMessageUids(dst, mailBoxName)
			if err != nil {
				slog.Error("Failed listing messages in target mailbox", "mailbox", mailBoxName, "err", err)
				return 1
			}
			for k := range messageIDsInSourceMailBox {
				delete(messageIDsInTargetMailBox, k)
			}
			messageIDsToSync = slices.Collect(maps.Keys(messageIDsInTargetMailBox))
		} else {
			messageIDsToSync = slices.Collect(maps.Keys(messageIDsInSourceMailBox))
		}

		// Split message IDs into chunks
		chunksOfMessageIDsToSync := slices.Collect(slices.Chunk(messageIDsToSync, 50_000))
		for _, messageIDs := range chunksOfMessageIDsToSync {
			_, err := firestoreClient.Doc(fmt.Sprintf("syncJobs/%s/chunks/%d", cfg.syncJobName, chunkIndex)).
				Set(ctx, map[string]interface{}{
					"mailboxName": mailBoxName,
					"messageIDs":  messageIDs,
				})
			if err != nil {
				slog.Error("Failed adding chunk", "chunkIndex", chunkIndex, "mailBoxName", mailBoxName, "err", err)
				return 1
			}
			chunkIndex++
		}
	}

	if chunkIndex == 0 {
		slog.Info("No messages to sync")
		return 0
	}

	slog.Info("Executing Cloud Run worker job", "job", cfg.workerJobName, "tasks", chunkIndex)
	req := &runpb.RunJobRequest{
		Name: fmt.Sprintf("projects/%s/locations/%s/jobs/%s", cfg.projectID, cfg.workerJobRegion, cfg.workerJobName),
		Overrides: &runpb.RunJobRequest_Overrides{
			TaskCount: int32(chunkIndex),
			ContainerOverrides: []*runpb.RunJobRequest_Overrides_ContainerOverride{
				{
					Env: []*runpb.EnvVar{{
						Name:   "SYNC_JOB_NAME",
						Values: &runpb.EnvVar_Value{Value: cfg.syncJobName},
					}},
				},
			},
		},
	}
	op, err := runClient.RunJob(ctx, req)
	if err != nil {
		slog.Error("Failed to schedule Cloud Run worker job execution", "err", err)
		return 1
	}
	slog.Info("Scheduled Cloud Run worker job execution", "syncJobName", cfg.syncJobName, "operation", op.Name())

	return 0
}

func main() {
	os.Exit(runJob())
}
