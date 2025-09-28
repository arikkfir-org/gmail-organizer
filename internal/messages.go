package internal

import (
	"errors"
	"fmt"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

func FetchAllMessageIDs(c *client.Client, mailBoxName string, batchSize uint) (map[string]bool, error) {
	if _, err := c.Select(mailBoxName, true); err != nil {
		return nil, fmt.Errorf("failed to select mailbox '%s': %w", mailBoxName, err)
	}

	status, err := c.Status(mailBoxName, []imap.StatusItem{imap.StatusMessages})
	if err != nil {
		return nil, fmt.Errorf("failed to get status for mailbox '%s': %w", mailBoxName, err)
	}
	if status.Messages == 0 {
		return make(map[string]bool), nil
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddRange(1, status.Messages)

	messages := make(chan *imap.Message, batchSize)
	done := make(chan error, 1)

	go func() {
		done <- c.Fetch(seqSet, []imap.FetchItem{imap.FetchEnvelope}, messages)
	}()

	ids := make(map[string]bool)
	for msg := range messages {
		if msg.Envelope != nil {
			ids[msg.Envelope.MessageId] = true
		}
	}

	if err := <-done; err != nil {
		return nil, fmt.Errorf("failed fetching message IDs for mailbox '%s': %w", mailBoxName, err)
	}

	return ids, nil
}

func FetchMessageLiteral(c *client.Client, uid uint32) (imap.Literal, error) {
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uid)

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{section.FetchItem()}

	messages := make(chan *imap.Message, 1)
	done := make(chan error, 1)
	go func() {
		done <- c.UidFetch(seqSet, items, messages)
	}()

	msg := <-messages
	if msg == nil {
		return nil, fmt.Errorf("server did not provide message '%d'", uid)
	}

	r := msg.GetBody(section)
	if r == nil {
		return nil, errors.New("server didn't return message body")
	}

	if err := <-done; err != nil {
		return nil, err
	}

	return r, nil
}
