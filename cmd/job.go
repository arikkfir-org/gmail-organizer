package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/arikkfir-org/gmail-organizer/internal/gcp"
	"github.com/emersion/go-imap"
)

const (
	batchSize                   = 500
	messageMigrationConcurrency = 1000
	messageMigrationWorkers     = 50
	sourceGmailConnectionsLimit = 15
	targetGmailConnectionsLimit = 15
)

type migrationRequest struct {
	sourceGmailUID uint32
	messageID      string
}

type DispatcherJob struct {
	sourceGmail        *gcp.Gmail
	targetGmail        *gcp.Gmail
	maxEmailsToProcess uint64
	jsonLogging        bool
	dryRun             bool
	messagesCh         chan *migrationRequest
}

func newDispatcherJob() (*DispatcherJob, error) {

	// Source Gmail account username
	sourceAccountUsername := os.Getenv("SOURCE_ACCOUNT_USERNAME")
	if sourceAccountUsername == "" {
		return nil, fmt.Errorf("SOURCE_ACCOUNT_USERNAME environment variable is required")
	}

	// Source Gmail account password
	sourceAccountPassword := os.Getenv("SOURCE_ACCOUNT_PASSWORD")
	if sourceAccountPassword == "" {
		return nil, fmt.Errorf("SOURCE_ACCOUNT_PASSWORD environment variable is required")
	}

	// Target Gmail account username
	targetAccountUsername := os.Getenv("TARGET_ACCOUNT_USERNAME")
	if targetAccountUsername == "" {
		return nil, fmt.Errorf("TARGET_ACCOUNT_USERNAME environment variable is required")
	}

	// Target Gmail account password
	targetAccountPassword := os.Getenv("TARGET_ACCOUNT_PASSWORD")
	if targetAccountPassword == "" {
		return nil, fmt.Errorf("TARGET_ACCOUNT_PASSWORD environment variable is required")
	}

	// Gmail account password
	var maxEmailsToProcess uint64 = math.MaxUint64
	if s, found := os.LookupEnv("MAX_EMAILS"); found {
		if v, err := strconv.ParseUint(s, 10, 64); err != nil {
			return nil, fmt.Errorf("failed to parse MAX_EMAILS environment variable: %w", err)
		} else {
			maxEmailsToProcess = v
		}
	}

	sourceGmail, err := gcp.NewGmail(sourceAccountUsername, sourceAccountPassword, sourceGmailConnectionsLimit, 1*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("failed to create source Gmail connection: %w", err)
	}

	targetGmail, err := gcp.NewGmail(targetAccountUsername, targetAccountPassword, targetGmailConnectionsLimit, 1*time.Hour)
	if err != nil {
		go sourceGmail.Close()
		return nil, fmt.Errorf("failed to create target Gmail connection: %w", err)
	}

	return &DispatcherJob{
		sourceGmail:        sourceGmail,
		targetGmail:        targetGmail,
		maxEmailsToProcess: maxEmailsToProcess,
		jsonLogging:        slices.Contains([]string{"t", "true", "y", "yes", "1", "ok", "on"}, os.Getenv("JSON_LOGGING")),
		dryRun:             os.Getenv("DRY_RUN") != "" || slices.Contains([]string{"t", "true", "y", "yes", "1", "ok", "on"}, os.Getenv("DRY_RUN")),
		messagesCh:         make(chan *migrationRequest, messageMigrationConcurrency),
	}, nil
}

func (j *DispatcherJob) Close() {
	j.sourceGmail.Close()
	j.targetGmail.Close()
}

func (j *DispatcherJob) Run(ctx context.Context) error {
	if err := j.migrateMailboxes(ctx); err != nil {
		return fmt.Errorf("failed to migrate mailboxes: %w", err)
	}

	collectionErrorCh := make(chan error, 1)
	go func() {
		collectionErrorCh <- j.collectMessagesForMigration(ctx)
	}()

	migrationErrorCh := make(chan error, messageMigrationWorkers)
	for i := 0; i < messageMigrationWorkers; i++ {
		go func() {
			migrationErrorCh <- j.migrateMessages(ctx)
		}()
	}

	done := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-collectionErrorCh:
			if err != nil {
				return fmt.Errorf("failed during message collection for migration: %w", err)
			} else {
				slog.Info("Message collection done")
			}
		case err := <-migrationErrorCh:
			if err != nil {
				return fmt.Errorf("failed during message migration: %w", err)
			} else {
				done++
				slog.Info("Migration worker done", "workersDone", done)
				if done == messageMigrationWorkers {
					return nil
				}
			}
		}
	}
}

func (j *DispatcherJob) migrateMailboxes(ctx context.Context) error {
	slog.Info("Fetching source mailbox names")
	sourceMailboxNames, err := j.sourceGmail.FetchMailboxNames(ctx, true, false)
	if err != nil {
		return fmt.Errorf("failed to fetch source mailbox names: %w", err)
	}

	slog.Info("Fetching target mailbox names")
	targetMailboxNames, err := j.targetGmail.FetchMailboxNames(ctx, true, false)
	if err != nil {
		return fmt.Errorf("failed to fetch target mailbox names: %w", err)
	}
	var missingMailboxNames []string
	for _, targetMailboxName := range targetMailboxNames {
		if !slices.Contains(sourceMailboxNames, targetMailboxName) {
			missingMailboxNames = append(missingMailboxNames, targetMailboxName)
		}
	}

	slog.Info("Creating mailboxes in target account")
	if err := j.targetGmail.CreateMailboxes(ctx, missingMailboxNames...); err != nil {
		return fmt.Errorf("failed to create mailboxes: %w", err)
	}

	return nil
}

func (j *DispatcherJob) collectMessagesForMigration(ctx context.Context) error {
	// Iterate messages one by one and fetch
	slog.Info("Fetching messages for migration")
	allUIDs, err := j.sourceGmail.FindAllUIDs(ctx, gcp.GmailAllMailLabel)
	if err != nil {
		return fmt.Errorf("failed to find all UIDs: %w", err)
	}

	slog.Info("Sorting for consistency", "size", len(allUIDs))
	slices.Sort(allUIDs)

	if uint64(len(allUIDs)) > j.maxEmailsToProcess {
		allUIDs = allUIDs[:int(j.maxEmailsToProcess)]
	}
	slog.Info("Collected message set for migration", "size", len(allUIDs))

	// Process in chunks to avoid fetching all UIDs at once
	chunks := slices.Collect(slices.Chunk(allUIDs, batchSize))
	for chunkNumber, chunkUIDs := range chunks {
		slog.Info("Migrating chunk", "chunkIndex", chunkNumber)
		messages, err := j.sourceGmail.FetchByUIDs(ctx, gcp.GmailAllMailLabel, chunkUIDs, imap.FetchEnvelope)
		if err != nil {
			return fmt.Errorf("failed to fetch messages for chunk %d: %w", chunkNumber, err)
		}
		for _, msg := range messages {
			if msg.Envelope == nil {
				return fmt.Errorf("failed to fetch envelope of UID '%d'", msg.Uid)
			}
			j.messagesCh <- &migrationRequest{
				sourceGmailUID: msg.Uid,
				messageID:      msg.Envelope.MessageId,
			}
		}
	}

	j.messagesCh <- nil
	close(j.messagesCh)
	return nil
}

func (j *DispatcherJob) migrateMessages(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case r, more := <-j.messagesCh:
			if !more {
				return nil
			} else if r == nil {
				return nil
			} else if err := j.migrateMessage(ctx, r.sourceGmailUID, r.messageID); err != nil {
				return fmt.Errorf("failed to migrate message '%s' (%d): %w", r.messageID, r.sourceGmailUID, err)
			}
		}
	}
}

func (j *DispatcherJob) migrateMessage(ctx context.Context, sourceGmailUID uint32, messageID string) error {
	slog.Debug("Migrating message", "sourceGmailUID", sourceGmailUID, "messageID", messageID)
	if uid, err := j.targetGmail.FindUIDByMessageID(ctx, gcp.GmailAllMailLabel, messageID); err != nil {
		return fmt.Errorf("failed to search for message '%s' in target account: %w", messageID, err)
	} else if uid == nil {
		if err := j.appendNewMessageToTargetAccount(ctx, sourceGmailUID); err != nil {
			return fmt.Errorf("failed to append new message '%s' to target account: %w", messageID, err)
		}
	} else if err := j.updateExistingMessageInTargetAccount(ctx, sourceGmailUID, messageID); err != nil {
		return fmt.Errorf("failed to update existing message '%s' in target account: %w", messageID, err)
	}
	return nil
}

func (j *DispatcherJob) appendNewMessageToTargetAccount(ctx context.Context, sourceGmailUID uint32) error {

	// Fetch message
	slog.Debug("Appending new message to target account", "sourceGmailUID", sourceGmailUID)
	msg, err := j.sourceGmail.FetchMessageByUID(ctx, gcp.GmailAllMailLabel, sourceGmailUID, imap.FetchEnvelope, imap.FetchFlags, imap.FetchInternalDate, imap.FetchRFC822, gcp.GmailLabelsExt)
	if err != nil {
		return fmt.Errorf("failed to fetch message '%d' from source account: %w", sourceGmailUID, err)
	}

	// Append the message to the target's "[Gmail]/All Mail" folder.
	// This preserves the flags and the original received date.
	if j.dryRun {
		slog.Info("Appending new message",
			"dryRun", true,
			"messageID", msg.Envelope.MessageId,
			"flags", msg.Flags,
			"internalDate", msg.InternalDate,
			"envelope", msg.Envelope,
			"body", msg.Body,
			"items", msg.Items)
	} else if _, err := j.targetGmail.AppendMessage(ctx, gcp.GmailAllMailLabel, msg); err != nil {
		return fmt.Errorf("failed to append message %d to target: %w", sourceGmailUID, err)
	}

	return nil
}

func (j *DispatcherJob) updateExistingMessageInTargetAccount(ctx context.Context, sourceGmailUID uint32, messageID string) error {

	// Fetch message
	slog.Debug("Updating message in target account", "sourceGmailUID", sourceGmailUID, "messageID", messageID)
	sourceMsg, err := j.sourceGmail.FetchMessageByUID(ctx, gcp.GmailAllMailLabel, sourceGmailUID, imap.FetchFlags, imap.FetchInternalDate, imap.FetchEnvelope, gcp.GmailLabelsExt)
	if err != nil {
		return fmt.Errorf("failed to fetch message '%d' from source account: %w", sourceGmailUID, err)
	}

	// Update message
	if j.dryRun {
		slog.Info("Updating existing message",
			"dryRun", true,
			"messageID", sourceMsg.Envelope.MessageId,
			"flags", sourceMsg.Flags,
			"internalDate", sourceMsg.InternalDate,
			"envelope", sourceMsg.Envelope,
			"body", sourceMsg.Body,
			"items", sourceMsg.Items)
	} else if err := j.targetGmail.UpdateMessage(ctx, gcp.GmailAllMailLabel, sourceMsg); err != nil {
		return fmt.Errorf("failed to update message '%s' in target account: %w", messageID, err)
	}

	return nil
}
