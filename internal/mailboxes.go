package internal

import (
	"fmt"
	"log/slog"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

type MailboxInfo struct {
	Name         string
	MessageCount uint32
}

func FetchMailboxNames(c *client.Client) ([]string, error) {
	slog.Info("Listing mailboxes...")
	mailboxes := make(chan *imap.MailboxInfo, 10)
	done := make(chan error, 1)
	var names []string
	go func() {
		done <- c.List("", "*", mailboxes)
	}()
	for m := range mailboxes {
		names = append(names, m.Name)
	}
	if err := <-done; err != nil {
		return nil, fmt.Errorf("failed to list mailboxes: %w", err)
	}
	return names, nil
}

func FetchMailboxes(c *client.Client) ([]MailboxInfo, error) {
	names, err := FetchMailboxNames(c)
	if err != nil {
		return nil, err
	}

	slog.Info("Fetching mailbox sizes...")
	var mailboxInfos []MailboxInfo
	for _, mailboxName := range names {
		// "[Gmail]" is a special container that can't be selected, but also can't contain messages so its safe to skip
		if mailboxName != "[Gmail]" {
			// Check source mailbox
			mbox, err := c.Select(mailboxName, true)
			if err != nil {
				return nil, fmt.Errorf("failed to select mailbox '%s': %w", mailboxName, err)
			}
			mailboxInfos = append(mailboxInfos, MailboxInfo{
				Name:         mailboxName,
				MessageCount: mbox.Messages,
			})
		}
	}

	return mailboxInfos, nil
}
