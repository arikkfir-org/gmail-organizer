package main

import (
	"errors"
	"flag"
	"fmt"
	"gmail-organizer/internal"
	"gmail-organizer/internal/util"
	"log/slog"
	"os"
	"strings"

	"github.com/emersion/go-imap"
)

type targetConfig struct {
	Username string
	Password string
}

func (c *targetConfig) Validate() error {
	if c.Username == "" {
		return fmt.Errorf("username is required")
	} else if c.Password == "" {
		return fmt.Errorf("password is required")
	}
	return nil
}

type config struct {
	source      targetConfig
	target      targetConfig
	batchSize   uint
	dryRun      bool
	jsonLogging bool
}

func (c *config) Validate() error {
	if err := c.source.Validate(); err != nil {
		return err
	} else if err := c.target.Validate(); err != nil {
		return err
	} else if c.batchSize == 0 {
		return errors.New("batch size must be greater than 0")
	}
	return nil
}

func run() int {
	var cfg config
	flag.StringVar(&cfg.source.Username, "source-username", os.Getenv("SOURCE_USERNAME"), "Source Gmail username (email address)")
	flag.StringVar(&cfg.source.Password, "source-password", os.Getenv("SOURCE_PASSWORD"), "Source Gmail app password")
	flag.StringVar(&cfg.target.Username, "target-username", os.Getenv("TARGET_USERNAME"), "Target Gmail username (email address)")
	flag.StringVar(&cfg.target.Password, "target-password", os.Getenv("TARGET_PASSWORD"), "Target Gmail app password")
	flag.BoolVar(&cfg.jsonLogging, "json-logging", false, "Use JSON logging")
	flag.BoolVar(&cfg.dryRun, "dry-run", false, "Dry run, do not actually sync messages")
	flag.UintVar(&cfg.batchSize, "batch-size", 5000, "Batch size for moving messages")
	flag.Parse()

	// Configure logging
	util.ConfigureLogging(cfg.jsonLogging)

	// Validate required arguments
	if err := cfg.Validate(); err != nil {
		slog.Error("Configuration invalid", "error", err)
		flag.Usage()
		return 1
	}

	// Connect to source
	slog.Info("Connecting to source Gmail IMAP server...", "email", cfg.source.Username)
	src, srcCleanup, err := internal.Dial(cfg.source.Username, cfg.source.Password)
	if err != nil {
		slog.Error("Failed to connect to source IMAP server", "error", err)
		return 1
	}
	defer srcCleanup()

	// Connect to target
	slog.Info("Connecting to target Gmail IMAP server...", "email", cfg.target.Username)
	dst, dstCleanup, err := internal.Dial(cfg.target.Username, cfg.target.Password)
	if err != nil {
		slog.Error("Failed to connect to target IMAP server", "error", err)
		return 1
	}
	defer dstCleanup()

	// Collect message counts for each mailbox and ensure they exist in target
	sourceMailBoxInfos, err := internal.FetchMailboxes(src)
	if err != nil {
		slog.Error("Failed to fetch source mailboxes", "error", err)
		return 1
	}

	// Ensure all mailboxes exist in target
	for _, mboxInfo := range sourceMailBoxInfos {
		// Create mailbox in target if it doesn't exist
		if !util.IsGmailSystemLabel(mboxInfo.Name) {
			if err := dst.Create(mboxInfo.Name); err != nil {
				if !strings.Contains(err.Error(), `Duplicate folder name`) {
					slog.Error("Failed to create mailbox in target", "mailbox", mboxInfo.Name, "error", err)
					return 1
				}
			} else {
				slog.Info("Created mailbox in target", "mailbox", mboxInfo.Name)
			}
		}
	}

	// Ensure all mailboxes exist in target
	for _, srcMailBoxInfo := range sourceMailBoxInfos {
		if srcMailBoxInfo.MessageCount == 0 {
			slog.Info("Skipping empty mailbox", "mailbox", srcMailBoxInfo.Name)
			continue
		}

		slog.Info("Fetching message IDs for target mailbox", "mailbox", srcMailBoxInfo.Name)
		messageIDsInTargetMailBox, err := internal.FetchAllMessageIDs(dst, srcMailBoxInfo.Name, cfg.batchSize)
		if err != nil {
			slog.Error("Failed to fetch message IDs from target", "mailbox", srcMailBoxInfo.Name, "error", err)
			return 1
		}

		// Select source mailbox
		srcMailBox, err := src.Select(srcMailBoxInfo.Name, true)
		if err != nil {
			slog.Error("Failed to select source mailbox", "mailbox", srcMailBoxInfo.Name, "error", err)
			return 1
		}

		slog.Info("Syncing mailbox", "mailbox", srcMailBoxInfo.Name, "messages", srcMailBox.Messages-uint32(len(messageIDsInTargetMailBox)))

		// Process messages in batches
		for i := uint32(1); i <= srcMailBox.Messages; i += uint32(cfg.batchSize) {
			end := i + uint32(cfg.batchSize) - 1
			if end > srcMailBox.Messages {
				end = srcMailBox.Messages
			}
			slog.Info("Syncing source messages to target mailbox", "mailbox", srcMailBoxInfo.Name, "batchStart", i, "batchEnd", end)

			seqSet := new(imap.SeqSet)
			seqSet.AddRange(i, end)

			messages := make(chan *imap.Message, cfg.batchSize)
			done := make(chan error, 1)
			go func() {
				done <- src.Fetch(seqSet, []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchFlags}, messages)
			}()

			for msg := range messages {
				if _, exists := messageIDsInTargetMailBox[msg.Envelope.MessageId]; exists {
					continue
				}

				if cfg.dryRun {
					slog.Info("Would copy message",
						"mailbox", srcMailBoxInfo.Name,
						"uid", msg.Uid,
						"subject", msg.Envelope.Subject)
					continue
				}

				literal, err := internal.FetchMessageLiteral(src, msg.Uid)
				if err != nil {
					slog.Error("Failed to fetch message literal from source",
						"mailbox", srcMailBoxInfo.Name,
						"uid", msg.Uid,
						"subject", msg.Envelope.Subject,
						"error", err)
					continue
				}

				if err := dst.Append(srcMailBoxInfo.Name, msg.Flags, msg.Envelope.Date, literal); err != nil {
					slog.Error("Failed to copy message",
						"mailbox", srcMailBoxInfo.Name,
						"uid", msg.Uid,
						"subject", msg.Envelope.Subject,
						"error", err)
					continue
				}

				slog.Info("Copied message",
					"mailbox", srcMailBoxInfo.Name,
					"uid", msg.Uid,
					"subject", msg.Envelope.Subject)
			}

			if err := <-done; err != nil {
				slog.Error("Failed fetching messages from source mailbox", "mailbox", srcMailBoxInfo.Name, "error", err)
				return 1
			}
		}
	}

	return 0
}

func main() {
	os.Exit(run())
}
