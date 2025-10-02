package gcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"cloud.google.com/go/pubsub"
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
	messageDecoder := json.NewDecoder(r)
	if err := messageDecoder.Decode(&message.Message.Data); err != nil {
		return nil, fmt.Errorf("failed to decode JSON from data inside the Pub/Sub message: %w", err)
	}

	return message, nil
}

func CreateTopicIfMissing(ctx context.Context, c *pubsub.Client, topicID string) (*pubsub.Topic, error) {
	topic := c.Topic(topicID)
	if exists, err := topic.Exists(ctx); err != nil {
		return nil, fmt.Errorf("failed to check if Pub/Sub topic '%s' exists: %w", topicID, err)
	} else if !exists {
		if t, err := c.CreateTopic(ctx, topicID); err != nil {
			return nil, fmt.Errorf("failed to create Pub/Sub topic '%s': %w", topicID, err)
		} else {
			return t, nil
		}
	} else {
		return topic, nil
	}
}

func CreateSubscriptionIfMissing(ctx context.Context, c *pubsub.Client, id string, cfg pubsub.SubscriptionConfig) (*pubsub.Subscription, error) {
	sub := c.Subscription(id)
	if exists, err := sub.Exists(ctx); err != nil {
		return nil, fmt.Errorf("failed to check if Pub/Sub subscription exists: %w", err)
	} else if !exists {
		if s, err := c.CreateSubscription(ctx, id, cfg); err != nil {
			return nil, fmt.Errorf("failed to create Pub/Sub subscription: %w", err)
		} else {
			return s, nil
		}
	} else {
		return sub, nil
	}
}
