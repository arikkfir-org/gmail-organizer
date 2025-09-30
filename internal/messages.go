package internal

import (
	"fmt"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

func FetchAllMessageUids(c *client.Client, mailBoxName string) (map[uint32]bool, error) {
	if _, err := c.Select(mailBoxName, true); err != nil {
		return nil, fmt.Errorf("failed to select mailbox '%s' for fetching message UIDs: %w", mailBoxName, err)
	}

	status, err := c.Status(mailBoxName, []imap.StatusItem{imap.StatusMessages})
	if err != nil {
		return nil, fmt.Errorf("failed to get status for mailbox '%s': %w", mailBoxName, err)
	}
	if status.Messages == 0 {
		return make(map[uint32]bool), nil
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddRange(1, status.Messages)

	messages := make(chan *imap.Message, 5000)
	done := make(chan error, 1)

	go func() {
		done <- c.Fetch(seqSet, []imap.FetchItem{imap.FetchEnvelope}, messages)
	}()

	ids := make(map[uint32]bool)
	for msg := range messages {
		if msg.Envelope != nil {
			ids[msg.Uid] = true
		}
	}

	if err := <-done; err != nil {
		return nil, fmt.Errorf("failed fetching message IDs for mailbox '%s': %w", mailBoxName, err)
	}

	return ids, nil
}
