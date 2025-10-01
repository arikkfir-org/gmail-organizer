package internal

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

type MailboxInfo struct {
	Name         string
	MessageCount uint32
}

func FetchMailboxes(ctx context.Context, c *client.Client) ([]MailboxInfo, error) {
	// First collect all mailbox names
	imapMailBoxes := make(chan *imap.MailboxInfo, 100)
	done := make(chan error, 1)
	go func() {
		done <- c.List("", "*", imapMailBoxes)
	}()
	var mailBoxNames []string
	for m := range imapMailBoxes {
		mailBoxNames = append(mailBoxNames, m.Name)
	}
	if err := <-done; err != nil {
		return nil, fmt.Errorf("failed to fetch mailboxes: %w", err)
	}

	// For each mailbox, fetch message count
	mailBoxCh := make(chan MailboxInfo, 5)
	done = make(chan error, 1)
	go func() {
		for _, m := range mailBoxNames {
			// "[Gmail]" is a special container that can't be selected, but also can't contain messages so its safe to skip
			if m == "[Gmail]" {
				slog.Info("Skipping special mailbox", "name", m)
			} else {
				// Check source mailbox
				mbox, err := c.Select(m, true)
				if err != nil {
					done <- fmt.Errorf("failed to select mailbox '%s': %w", m, err)
					return
				}
				mailBoxCh <- MailboxInfo{
					Name:         m,
					MessageCount: mbox.Messages,
				}
			}
		}
		done <- nil
	}()

	var mailboxInfos []MailboxInfo
	for {
		select {
		case <-ctx.Done():
			return mailboxInfos, ctx.Err()
		case m := <-mailBoxCh:
			mailboxInfos = append(mailboxInfos, m)
		case err := <-done:
			return mailboxInfos, err
		}
	}
}
