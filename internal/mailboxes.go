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

func FetchMailboxes(c *client.Client) ([]MailboxInfo, error) {
	imapMailBoxes := make(chan *imap.MailboxInfo, 100)
	done := make(chan error, 1)
	go func() {
		done <- c.List("", "*", imapMailBoxes)
	}()

	var mailboxInfos []MailboxInfo
	for m := range imapMailBoxes {
		// "[Gmail]" is a special container that can't be selected, but also can't contain messages so its safe to skip
		if m.Name == "[Gmail]" {
			slog.Info("Skipping special mailbox", "name", m.Name)
		} else {
			// Check source mailbox
			mbox, err := c.Select(m.Name, true)
			if err != nil {
				return nil, fmt.Errorf("failed to select mailbox '%s': %w", m.Name, err)
			}
			mailboxInfos = append(mailboxInfos, MailboxInfo{
				Name:         m.Name,
				MessageCount: mbox.Messages,
			})
			slog.Info("Discovered mailbox", "name", m.Name, "messageCount", mbox.Messages)
		}
	}

	if err := <-done; err != nil {
		return nil, fmt.Errorf("failed to fetch mailboxes: %w", err)
	}

	return mailboxInfos, nil
}
