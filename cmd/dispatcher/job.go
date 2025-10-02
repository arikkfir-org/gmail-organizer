package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/arikkfir-org/gmail-organizer/internal/gcp"
	"github.com/emersion/go-imap"
)

const (
	batchSize = 500
)

func newDispatcherJob(ctx context.Context) (*DispatcherJob, error) {

	// Cloud Run job execution ID
	runExecutionID := os.Getenv("CLOUD_RUN_EXECUTION")
	if runExecutionID == "" {
		return nil, fmt.Errorf("CLOUD_RUN_EXECUTION environment variable is required")
	}

	// Message processor endpoint
	processorEndpoint := os.Getenv("PROCESSOR_ENDPOINT")
	if processorEndpoint == "" {
		return nil, fmt.Errorf("PROCESSOR_ENDPOINT environment variable is required")
	}

	// Gmail account username
	accountUsername := os.Getenv("SOURCE_ACCOUNT_USERNAME")
	if accountUsername == "" {
		return nil, fmt.Errorf("SOURCE_ACCOUNT_USERNAME environment variable is required")
	}

	// Gmail account password
	accountPassword := os.Getenv("SOURCE_ACCOUNT_PASSWORD")
	if accountPassword == "" {
		return nil, fmt.Errorf("SOURCE_ACCOUNT_PASSWORD environment variable is required")
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

	return &DispatcherJob{
		runExecutionID:    runExecutionID,
		processorEndpoint: processorEndpoint,
		accountUsername:   accountUsername,
		accountPassword:   accountPassword,
		jsonLogging:       slices.Contains([]string{"t", "true", "y", "yes", "1", "ok", "on"}, os.Getenv("JSON_LOGGING")),
		pubSubClient:      pubSubClient,
	}, nil
}

type DispatcherJob struct {
	runExecutionID    string
	processorEndpoint string
	accountUsername   string
	accountPassword   string
	jsonLogging       bool
	pubSubClient      *pubsub.Client
}

func (j *DispatcherJob) Close() {
	if err := j.pubSubClient.Close(); err != nil {
		slog.Warn("Failed to close Pub/Sub client", "err", err)
	}
}

func (j *DispatcherJob) Run(ctx context.Context) error {

	// Create our Pub/Sub topics
	topic, err := gcp.CreateTopicIfMissing(ctx, j.pubSubClient, fmt.Sprintf("messages-%s", j.runExecutionID))
	if err != nil {
		return fmt.Errorf("failed to ensure messages topic: %w", err)
	}
	dlTopic, err := gcp.CreateTopicIfMissing(ctx, j.pubSubClient, fmt.Sprintf("%s-dl", topic.ID()))
	if err != nil {
		return fmt.Errorf("failed to ensure messages dead-letter topic: %w", err)
	}

	// Create our Pub/Sub subscriptions
	if _, err := j.createSubscription(ctx, topic, dlTopic); err != nil {
		return fmt.Errorf("failed to ensure messages subscription: %w", err)
	}
	if _, err := j.createDeadLetterSubscription(ctx, dlTopic); err != nil {
		return fmt.Errorf("failed to ensure messages dead-letter topic subscription: %w", err)
	}

	// Connect to Gmail server
	gmail, err := gcp.NewGmail(j.accountUsername, j.accountPassword)
	if err != nil {
		return fmt.Errorf("failed to create Gmail client: %w", err)
	}
	defer gmail.Close()

	// Select the "All Mail" label
	if err := gmail.Select(gcp.GmailAllMailLabel, true); err != nil {
		return fmt.Errorf("failed to select all-mail label: %w", err)
	}

	// Iterate messages one by one and fetch
	allUIDs, err := gmail.FindAllUIDs()
	if err != nil {
		return fmt.Errorf("failed to find all UIDs: %w", err)
	}
	if os.Getenv("TEST") == "true" {
		allUIDs = allUIDs[:10]
	}
	chunks := slices.Collect(slices.Chunk(allUIDs, batchSize))

	// Process each chunk
	for chunkNumber, chunkUIDs := range chunks {
		messages, err := gmail.FetchByUIDs(chunkUIDs, imap.FetchEnvelope)
		if err != nil {
			return fmt.Errorf("failed to fetch messages for chunk %d: %w", chunkNumber, err)
		}
		for _, msg := range messages {
			if msg.Envelope != nil {
				data := map[string]any{
					"uid": msg.Uid,
					"envelope": map[string]any{
						"messageId": msg.Envelope.MessageId,
					},
				}
				jsonBytes, err := json.Marshal(data)
				if err != nil {
					return fmt.Errorf("failed to marshal JSON for message UID '%d': %w", msg.Uid, err)
				}
				result := topic.Publish(ctx, &pubsub.Message{
					Data: jsonBytes,
					Attributes: map[string]string{
						"run-execution-id": j.runExecutionID,
					},
				})
				go func(r *pubsub.PublishResult) {
					if _, err := result.Get(ctx); err != nil {
						slog.Warn("Failed to publish message to messages topic", "uid", msg.Uid, "err", err)
						// TODO: consider retry publishing the message
					}
				}(result)
			} else {
				return fmt.Errorf("failed to fetch envelope of UID '%d'", msg.Uid)
			}
		}
	}

	return nil
}

func (j *DispatcherJob) createSubscription(ctx context.Context, topic, dlTopic *pubsub.Topic) (*pubsub.Subscription, error) {
	return j.createSub(ctx, fmt.Sprintf("messages-worker-%s", j.runExecutionID), pubsub.SubscriptionConfig{
		Topic: topic,
		PushConfig: pubsub.PushConfig{
			Endpoint: j.processorEndpoint,
		},
		AckDeadline:      60 * time.Second,
		Labels:           map[string]string{"run-execution-id": j.runExecutionID},
		ExpirationPolicy: 24 * time.Hour * 7,
		DeadLetterPolicy: &pubsub.DeadLetterPolicy{
			DeadLetterTopic:     dlTopic.String(),
			MaxDeliveryAttempts: 100,
		},
		RetryPolicy: &pubsub.RetryPolicy{
			MinimumBackoff: 10 * time.Second,
			MaximumBackoff: 10 * time.Minute,
		},
	})
}

func (j *DispatcherJob) createDeadLetterSubscription(ctx context.Context, topic *pubsub.Topic) (*pubsub.Subscription, error) {
	return j.createSub(ctx, fmt.Sprintf("messages-worker-%s-dl", j.runExecutionID), pubsub.SubscriptionConfig{
		Topic: topic,
		Labels: map[string]string{
			"run-execution-id": j.runExecutionID,
		},
		ExpirationPolicy: 24 * time.Hour * 31,
	})
}

func (j *DispatcherJob) createSub(ctx context.Context, id string, cfg pubsub.SubscriptionConfig) (*pubsub.Subscription, error) {
	return gcp.CreateSubscriptionIfMissing(ctx, j.pubSubClient, id, cfg)
}
