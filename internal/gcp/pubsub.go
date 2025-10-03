package gcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"cloud.google.com/go/pubsub"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

type wrapperMessage struct {
	Subscription string `json:"subscription"`
	Message      struct {
		Data        string            `json:"data"`
		MessageID   string            `json:"messageId"`
		PublishTime string            `json:"publishTime"`
		Attributes  map[string]string `json:"attributes"`
	} `json:"message"`
}

type Message[T any] struct {
	Subscription string
	Message      MessageContents[T]
}

type MessageContents[T any] struct {
	MessageID   string
	PublishTime string
	Attributes  map[string]string
	Data        T
}

func ReadPubSubMessage[T any](r io.Reader) (*Message[T], error) {
	var raw bytes.Buffer
	var wrapper wrapperMessage
	wrapperDecoder := json.NewDecoder(io.TeeReader(r, &raw))
	if err := wrapperDecoder.Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("failed to decode Pub/Sub message wrapper: %w", err)
	}

	if b, err := base64.StdEncoding.DecodeString(wrapper.Message.Data); err != nil {
		return nil, fmt.Errorf("failed to decode base64 data inside a Pub/Sub message wrapper: %w", err)
	} else {
		wrapper.Message.Data = string(b)
	}

	message := &Message[T]{
		Subscription: wrapper.Subscription,
		Message: MessageContents[T]{
			MessageID:   wrapper.Message.MessageID,
			PublishTime: wrapper.Message.PublishTime,
			Attributes:  wrapper.Message.Attributes,
		},
	}
	messageDecoder := json.NewDecoder(strings.NewReader(wrapper.Message.Data))
	if err := messageDecoder.Decode(&message.Message.Data); err != nil {
		return nil, fmt.Errorf("failed to decode JSON from data inside the Pub/Sub message: %w", err)
	}

	return message, nil
}

func CreateTopicIfMissing(ctx context.Context, c *pubsub.Client, id string) (*pubsub.Topic, error) {
	slog.Info("Checking if Pub/Sub topic exists", "topic", id)
	topic := c.Topic(id)
	if exists, err := topic.Exists(ctx); err != nil {
		return nil, fmt.Errorf("failed to check if Pub/Sub topic '%s' exists: %w", id, err)
	} else if !exists {
		slog.Info("Creating Pub/Sub topic", "topic", id)
		if t, err := c.CreateTopic(ctx, id); err != nil {
			return nil, fmt.Errorf("failed to create Pub/Sub topic '%s': %w", id, err)
		} else {
			return t, nil
		}
	} else {
		return topic, nil
	}
}

func CreateSubscriptionIfMissing(ctx context.Context, c *pubsub.Client, id string, cfg pubsub.SubscriptionConfig) (*pubsub.Subscription, error) {
	slog.Info("Checking if Pub/Sub subscription exists", "subscription", id)
	sub := c.Subscription(id)
	if exists, err := sub.Exists(ctx); err != nil {
		return nil, fmt.Errorf("failed to check if Pub/Sub subscription exists: %w", err)
	} else if !exists {
		slog.Info("Creating Pub/Sub subscription", "subscription", id)
		if s, err := c.CreateSubscription(ctx, id, cfg); err != nil {
			return nil, fmt.Errorf("failed to create Pub/Sub subscription: %w", err)
		} else {
			return s, nil
		}
	} else {
		slog.Info("Verifying Pub/Sub subscription configuration", "subscription", id)
		config, err := sub.Config(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get Pub/Sub subscription configuration: %w", err)
		}

		ignores := []cmp.Option{
			cmpopts.IgnoreUnexported(pubsub.SubscriptionConfig{}, pubsub.Topic{}),
			cmpopts.IgnoreFields(pubsub.SubscriptionConfig{}, "State", "TopicMessageRetentionDuration"),
		}
		if !cmp.Equal(config, cfg, ignores...) {
			diff := cmp.Diff(config, cfg, ignores...)
			slog.Info("Updating Pub/Sub subscription configuration", "subscription", id, "diff", diff)
			if _, err := sub.Update(ctx, pubsub.SubscriptionConfigToUpdate{
				PushConfig:                &cfg.PushConfig,
				BigQueryConfig:            &cfg.BigQueryConfig,
				CloudStorageConfig:        &cfg.CloudStorageConfig,
				AckDeadline:               cfg.AckDeadline,
				RetainAckedMessages:       cfg.RetainAckedMessages,
				RetentionDuration:         cfg.RetentionDuration,
				ExpirationPolicy:          cfg.ExpirationPolicy,
				DeadLetterPolicy:          cfg.DeadLetterPolicy,
				Labels:                    cfg.Labels,
				RetryPolicy:               cfg.RetryPolicy,
				EnableExactlyOnceDelivery: cfg.EnableExactlyOnceDelivery,
				MessageTransforms:         cfg.MessageTransforms,
			}); err != nil {
				return nil, fmt.Errorf("failed to update Pub/Sub subscription configuration: %w", err)
			}
		}
		return sub, nil
	}
}
