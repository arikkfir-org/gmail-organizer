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
	"strconv"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/arikkfir-org/gmail-organizer/internal/gcp"
	"github.com/emersion/go-imap"
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

	// HTTP port
	var port uint16 = 8080
	if s, found := os.LookupEnv("PORT"); found {
		if v, err := strconv.ParseUint(s, 10, 16); err != nil {
			return nil, fmt.Errorf("failed to parse PORT environment variable: %w", err)
		} else {
			port = uint16(v)
		}
	}

	// Determine GCP project ID
	projectID, err := gcp.GetProjectId(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to determine current GCP project: %w", err)
	}

	// Create a Pub/Sub client
	pubSubClient, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create Pub/Sub client: %w", err)
	}

	return &WorkerApp{
		sourceGmail:  gcp.NewGmail(sourceAccountUsername, sourceAccountPassword, 10*time.Second),
		targetGmail:  gcp.NewGmail(targetAccountUsername, targetAccountPassword, 10*time.Second),
		jsonLogging:  slices.Contains([]string{"t", "true", "y", "yes", "1", "ok", "on"}, os.Getenv("JSON_LOGGING")),
		dryRun:       os.Getenv("DRY_RUN") != "" || slices.Contains([]string{"t", "true", "y", "yes", "1", "ok", "on"}, os.Getenv("DRY_RUN")),
		pubSubClient: pubSubClient,
		port:         port,
	}, nil
}

type WorkerApp struct {
	runExecutionID string
	sourceGmail    *gcp.Gmail
	targetGmail    *gcp.Gmail
	jsonLogging    bool
	dryRun         bool
	pubSubClient   *pubsub.Client
	port           uint16
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
		Addr:     fmt.Sprintf(":%d", a.port),
		Handler:  mux,
		ErrorLog: slog.NewLogLogger(slog.Default().Handler(), slog.LevelInfo),
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	httpServerErrCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpServerErrCh <- fmt.Errorf("worker server failed: %w", err)
		} else {
			httpServerErrCh <- nil
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Warn("Failed to shutdown HTTP server", "err", err)
		}
		return ctx.Err()
	case err := <-httpServerErrCh:
		if err != nil {
			return fmt.Errorf("HTTP server failed: %w", err)
		} else {
			return nil
		}
	}
}

func (a *WorkerApp) HandleRequest(w http.ResponseWriter, r *http.Request) {
	pubSubMsg, err := gcp.ReadPubSubMessage[Message](r.Body)
	if err != nil {
		slog.Error("Failed to read Pub/Sub message", "err", err)
		http.Error(w, "Failed to read Pub/Sub message", http.StatusBadRequest)
		return
	}
	slog.Info("Received Pub/Sub message", "message", pubSubMsg)

	// Check if the given `Message-Id` already exists in the target account
	sourceGmailUID := pubSubMsg.Message.Data.Uid
	messageID := pubSubMsg.Message.Data.Envelope.MessageId
	if uid, err := a.targetGmail.FindUIDByMessageID(gcp.GmailAllMailLabel, messageID); err != nil {
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
		if err := a.updateExistingMessageInTargetAccount(sourceGmailUID, messageID); err != nil {
			slog.Error("Failed to update existing message in target account",
				"err", err, "messageId", messageID, "sourceAccountUID", sourceGmailUID)
			http.Error(w, "Failed to update existing message in target account", http.StatusInternalServerError)
		}
	}
}

func (a *WorkerApp) appendNewMessageToTargetAccount(sourceGmailUID uint32) error {

	// Fetch message
	msg, err := a.sourceGmail.FetchMessageByUID(gcp.GmailAllMailLabel, sourceGmailUID, imap.FetchEnvelope, imap.FetchFlags, imap.FetchInternalDate, imap.FetchRFC822, gcp.GmailLabelsExt)
	if err != nil {
		return fmt.Errorf("failed to fetch message '%d' from source account: %w", sourceGmailUID, err)
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
	} else if _, err := a.targetGmail.AppendMessage(gcp.GmailAllMailLabel, msg); err != nil {
		return fmt.Errorf("failed to append message %d to target: %w", sourceGmailUID, err)
	}

	return nil
}

func (a *WorkerApp) updateExistingMessageInTargetAccount(sourceGmailUID uint32, messageID string) error {

	// Fetch message
	sourceMsg, err := a.sourceGmail.FetchMessageByUID(gcp.GmailAllMailLabel, sourceGmailUID, imap.FetchFlags, imap.FetchInternalDate, imap.FetchEnvelope, gcp.GmailLabelsExt)
	if err != nil {
		return fmt.Errorf("failed to fetch message '%d' from source account: %w", sourceGmailUID, err)
	}

	// Update message
	if a.dryRun {
		slog.Info("Updating existing message",
			"dryRun", true,
			"messageID", sourceMsg.Envelope.MessageId,
			"flags", sourceMsg.Flags,
			"internalDate", sourceMsg.InternalDate,
			"envelope", sourceMsg.Envelope,
			"body", sourceMsg.Body,
			"items", sourceMsg.Items)
	} else if err := a.targetGmail.UpdateMessage(gcp.GmailAllMailLabel, sourceMsg); err != nil {
		return fmt.Errorf("failed to update message '%s' in target account: %w", messageID, err)
	}

	return nil
}
