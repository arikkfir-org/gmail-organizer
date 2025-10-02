package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"slices"

	"cloud.google.com/go/pubsub"
	"github.com/arikkfir-org/gmail-organizer/internal/gcp"
	"github.com/emersion/go-imap"
	"golang.org/x/oauth2/google"
)

type Message struct {
	Uid      uint32 `json:"uid"`
	Envelope struct {
		MessageId string `json:"messageId"`
	} `json:"envelope"`
}

func newWorkerApp(ctx context.Context) (*WorkerApp, error) {

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

	// Determine GCP project ID
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("failed inferring current GCP project: %w", err)
	} else if creds.ProjectID == "" {
		return nil, fmt.Errorf("failed inferring current GCP project: project ID in ADC is empty")
	}

	// Create a Pub/Sub client
	pubSubClient, err := pubsub.NewClient(ctx, creds.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create Pub/Sub client: %w", err)
	}

	return &WorkerApp{
		sourceAccountUsername: sourceAccountUsername,
		sourceAccountPassword: sourceAccountPassword,
		targetAccountUsername: targetAccountUsername,
		targetAccountPassword: targetAccountPassword,
		jsonLogging:           slices.Contains([]string{"t", "true", "y", "yes", "1", "ok", "on"}, os.Getenv("JSON_LOGGING")),
		dryRun:                os.Getenv("DRY_RUN") == "" || slices.Contains([]string{"t", "true", "y", "yes", "1", "ok", "on"}, os.Getenv("DRY_RUN")),
		pubSubClient:          pubSubClient,
	}, nil
}

type WorkerApp struct {
	runExecutionID        string
	sourceAccountUsername string
	sourceAccountPassword string
	targetAccountUsername string
	targetAccountPassword string
	jsonLogging           bool
	dryRun                bool
	pubSubClient          *pubsub.Client
}

func (a *WorkerApp) Close() {
	if err := a.pubSubClient.Close(); err != nil {
		slog.Warn("Failed to close Pub/Sub client", "err", err)
	}
}

func (a *WorkerApp) Run(ctx context.Context) error {
	slog.Info("Starting worker...")
	defer slog.Info("Worker stopped")

	mux := http.NewServeMux()
	mux.HandleFunc("POST /", a.HandleRequest)

	server := http.Server{
		Addr:     ":8080",
		Handler:  mux,
		ErrorLog: slog.NewLogLogger(slog.Default().Handler(), slog.LevelInfo),
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("worker server failed: %w", err)
	}

	return nil
}

func (a *WorkerApp) HandleRequest(w http.ResponseWriter, r *http.Request) {
	pubSubMsg, err := gcp.ReadPubSubMessage[Message](r.Body)
	if err != nil {
		slog.Error("Failed to read Pub/Sub message", "err", err)
		http.Error(w, "Failed to read Pub/Sub message", http.StatusBadRequest)
		return
	}
	slog.Info("Received Pub/Sub message", "message", pubSubMsg)

	// Connect to target Gmail server
	targetGmail := gcp.NewGmail(a.targetAccountUsername)
	defer targetGmail.Close()
	if err := targetGmail.Connect(a.targetAccountPassword); err != nil {
		slog.Error("Failed to connect to target Gmail IMAP server", "email", a.targetAccountUsername, "err", err)
		http.Error(w, "Failed to connect to target Gmail IMAP server", http.StatusInternalServerError)
		return
	}

	// Select the "All Mail" label in the target account
	if err := targetGmail.Select(gcp.GmailAllMailLabel, true); err != nil {
		slog.Error("Failed to select 'All Mail' label in target account", "err", err)
		http.Error(w, "Failed to select 'All Mail' label in target account", http.StatusInternalServerError)
		return
	}

	// Check if the given `Message-Id` already exists in the target account
	sourceGmailUID := pubSubMsg.Message.Data.Uid
	messageID := pubSubMsg.Message.Data.Envelope.MessageId
	if uid, err := targetGmail.FindUIDByMessageID(messageID); err != nil {
		slog.Error("Failed to find UID in target account for given message",
			"err", err, "messageId", messageID, "sourceAccountUID", sourceGmailUID)
		http.Error(w, "Failed to find UID for message in target account", http.StatusInternalServerError)
		return
	} else if uid == nil {
		if err := a.appendNewMessageToTargetAccount(sourceGmailUID); err != nil {
			slog.Error("Failed to append new message to target account",
				"err", err, "messageId", messageID, "sourceAccountUID", sourceGmailUID)
			http.Error(w, "Failed to append new message to target account", http.StatusInternalServerError)
		}
	} else {
		if err := a.updateExistingMessageInTargetAccount(*uid, messageID); err != nil {
			slog.Error("Failed to update existing message in target account",
				"err", err, "messageId", messageID, "sourceAccountUID", sourceGmailUID)
			http.Error(w, "Failed to update existing message in target account", http.StatusInternalServerError)
		}
	}
}

func (a *WorkerApp) appendNewMessageToTargetAccount(sourceGmailUID uint32) error {

	// Connect to source Gmail server
	sourceGmail := gcp.NewGmail(a.sourceAccountUsername)
	defer sourceGmail.Close()
	if err := sourceGmail.Connect(a.sourceAccountPassword); err != nil {
		return fmt.Errorf("failed to connect to source Gmail: %w", err)
	}

	// Select the "All Mail" label in the target account
	if err := sourceGmail.Select(gcp.GmailAllMailLabel, true); err != nil {
		return fmt.Errorf("failed to select 'All Mail' label in source account: %w", err)
	}

	// Fetch message
	msg, err := sourceGmail.FetchMessageByUID(sourceGmailUID, imap.FetchEnvelope, imap.FetchFlags, imap.FetchInternalDate, imap.FetchRFC822, gcp.GmailLabelsExt)
	if err != nil {
		return fmt.Errorf("failed to fetch message '%d' from source account '%s': %w", sourceGmailUID, a.sourceAccountUsername, err)
	}

	// Connect to target Gmail server
	targetGmail := gcp.NewGmail(a.targetAccountUsername)
	defer targetGmail.Close()
	if err := targetGmail.Connect(a.targetAccountPassword); err != nil {
		return fmt.Errorf("failed to connect to target Gmail IMAP server for '%s': %w", a.targetAccountUsername, err)
	}

	// Select the "All Mail" label in the target account
	if err := targetGmail.Select(gcp.GmailAllMailLabel, true); err != nil {
		return fmt.Errorf("failed to select 'All Mail' label in target account: %w", err)
	}

	// Append the message to the target's "[Gmail]/All Mail" folder.
	// This preserves the flags and the original received date.
	if a.dryRun {
		slog.Info("Appending new message",
			"dryRun", true,
			"messageID", msg.Envelope.MessageId,
			"flags", msg.Flags,
			"internalDate", msg.InternalDate,
			"envelope", msg.Envelope,
			"body", msg.Body,
			"items", msg.Items)
	} else if err := targetGmail.AppendMessage(msg); err != nil {
		return fmt.Errorf("failed to append message %d to target: %w", sourceGmailUID, err)
	}

	return nil
}

func (a *WorkerApp) updateExistingMessageInTargetAccount(sourceGmailUID uint32, messageID string) error {

	// Connect to source Gmail server
	sourceGmail := gcp.NewGmail(a.sourceAccountUsername)
	defer sourceGmail.Close()
	if err := sourceGmail.Connect(a.sourceAccountPassword); err != nil {
		return fmt.Errorf("failed to connect to source Gmail: %w", err)
	}

	// Select the "All Mail" label in the target account
	if err := sourceGmail.Select(gcp.GmailAllMailLabel, true); err != nil {
		return fmt.Errorf("failed to select 'All Mail' label in source account: %w", err)
	}

	// Fetch message
	msg, err := sourceGmail.FetchMessageByUID(sourceGmailUID, imap.FetchFlags, gcp.GmailLabelsExt)
	if err != nil {
		return fmt.Errorf("failed to fetch message '%d' from source account '%s': %w", sourceGmailUID, a.sourceAccountUsername, err)
	}

	// Connect to target Gmail server
	targetGmail := gcp.NewGmail(a.targetAccountUsername)
	defer targetGmail.Close()
	if err := targetGmail.Connect(a.targetAccountPassword); err != nil {
		return fmt.Errorf("failed to connect to target Gmail IMAP server for '%s': %w", a.targetAccountUsername, err)
	}

	// Select the "All Mail" label in the target account
	if err := targetGmail.Select(gcp.GmailAllMailLabel, true); err != nil {
		return fmt.Errorf("failed to select 'All Mail' label in target account: %w", err)
	}

	// Update message
	if a.dryRun {
		slog.Info("Updating existing message",
			"dryRun", true,
			"messageID", msg.Envelope.MessageId,
			"flags", msg.Flags,
			"internalDate", msg.InternalDate,
			"envelope", msg.Envelope,
			"body", msg.Body,
			"items", msg.Items)
	} else if err := targetGmail.UpdateMessage(msg); err != nil {
		return fmt.Errorf("failed to update message '%s' in target account: %w", messageID, err)
	}

	return nil
}
