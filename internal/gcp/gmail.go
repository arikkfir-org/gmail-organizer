package gcp

import (
	"fmt"
	"log/slog"
	"slices"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

const (
	GmailAllMailLabel = "[Gmail]/All Mail"
	gmailImapHost     = "imap.gmail.com"
	gmailImapPort     = 993
	GmailLabelsExt    = "X-GM-LABELS"
)

var (
	gmailImapURL = fmt.Sprintf("%s:%d", gmailImapHost, gmailImapPort)
)

func NewGmail(account string) *Gmail {
	return &Gmail{account: account}
}

type Gmail struct {
	account string

	c *client.Client
}

func (g *Gmail) Connect(password string) error {
	// TODO: pool IMAP connections
	slog.Info("Connecting to Gmail IMAP server", "email", g.account)
	if c, err := client.DialTLS(gmailImapURL, nil); err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	} else if err := c.Login(g.account, password); err != nil {
		return fmt.Errorf("failed to login: %w", err)
	} else {
		g.c = c
		return nil
	}
}

func (g *Gmail) Close() {
	if err := g.c.Logout(); err != nil {
		slog.Warn("Failed to logout from Gmail IMAP server", "err", err, "email", g.account)
	}
}

func (g *Gmail) Select(mailbox string, readOnly bool) error {
	if g.c == nil {
		return fmt.Errorf("not connected to Gmail IMAP server for '%s'", g.account)
	} else if _, err := g.c.Select(mailbox, readOnly); err != nil {
		return fmt.Errorf("failed to select '%s' in account %s: %w", mailbox, g.account, err)
	} else {
		return nil
	}
}

func (g *Gmail) FindAllUIDs() ([]uint32, error) {
	criteria := imap.NewSearchCriteria()
	criteria.SeqNum = new(imap.SeqSet)
	criteria.SeqNum.AddRange(1, 0)
	uids, err := g.c.UidSearch(criteria)
	if err != nil {
		return nil, fmt.Errorf("failed performing criteria search for all UIDs: %w", err)
	}
	return uids, nil
}

func (g *Gmail) FetchByUIDs(uids []uint32, items ...imap.FetchItem) ([]*imap.Message, error) {
	if !slices.Contains(items, imap.FetchUid) {
		items = append(items, imap.FetchUid)
	}
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uids...)
	messagesCh := make(chan *imap.Message, len(uids))
	if err := g.c.UidFetch(seqSet, items, messagesCh); err != nil {
		return nil, fmt.Errorf("failed to fetch message IDs: %w", err)
	}
	messages := make([]*imap.Message, len(uids))
	for msg := range messagesCh {
		messages = append(messages, msg)
	}
	return messages, nil
}

func (g *Gmail) FindUIDByMessageID(messageID string) (*uint32, error) {
	criteria := imap.NewSearchCriteria()
	criteria.Header.Add("Message-Id", messageID)
	uids, err := g.c.UidSearch(criteria)
	if err != nil {
		return nil, fmt.Errorf("failed to search for message by Message-ID: %w", err)
	} else if len(uids) == 0 {
		return nil, nil
	} else if len(uids) == 1 {
		return &uids[0], nil
	} else {
		return nil, fmt.Errorf("found multiple UIDs for Message-ID '%s'", messageID)
	}
}

func (g *Gmail) FetchMessageByUID(uid uint32, items ...imap.FetchItem) (*imap.Message, error) {
	if !slices.Contains(items, imap.FetchUid) {
		items = append(items, imap.FetchUid)
	}
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uid)
	messages := make(chan *imap.Message, 1)
	if err := g.c.UidFetch(seqSet, items, messages); err != nil {
		return nil, fmt.Errorf("failed to fetch message '%d' from account '%s': %w", uid, g.account, err)
	}
	msg := <-messages
	if msg == nil {
		return nil, fmt.Errorf("server did not provide message '%d' from account '%s'", uid, g.account)
	}

	return msg, nil
}

func (g *Gmail) AppendMessage(msg *imap.Message) error {
	if msg.Uid == 0 {
		return fmt.Errorf("cannot append message %d - it has no UID", msg.Uid)
	}

	r := msg.GetBody(&imap.BodySectionName{})
	if r == nil {
		return fmt.Errorf("cannot append message %d - it is missing body", msg.Uid)
	}

	if err := g.c.Append(GmailAllMailLabel, msg.Flags, msg.InternalDate, r); err != nil {
		return fmt.Errorf("failed to append message %d to target: %w", msg.Uid, err)
	}

	messageID := msg.Envelope.MessageId
	uid, err := g.FindUIDByMessageID(messageID)
	if err != nil {
		return fmt.Errorf("failed to find UID for newly-appended message '%s' in target account: %w", messageID, err)
	} else if uid == nil {
		return fmt.Errorf("could not find UID for newly appended message '%s' in target account", messageID)
	}

	var labels []string
	if rawLabels, ok := msg.Items[GmailLabelsExt]; ok {
		if labelInterfaces, ok := rawLabels.([]any); ok {
			for _, l := range labelInterfaces {
				if label, ok := l.(string); ok {
					labels = append(labels, label)
				} else {
					return fmt.Errorf("invalid label type '%T'", l)
				}
			}
			slices.Sort(labels)
		} else {
			return fmt.Errorf("invalid labels type '%T'", rawLabels)
		}
	}
	labelsAsAnyArray := make([]any, len(labels))
	for i, label := range labels {
		labelsAsAnyArray[i] = label
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddNum(*uid)
	if err := g.c.UidStore(seqSet, GmailLabelsExt+".SILENT", labelsAsAnyArray, nil); err != nil {
		return fmt.Errorf("failed to store labels on target message '%d': %w", *uid, err)
	}

	return nil
}

func (g *Gmail) UpdateMessage(msg *imap.Message) error {

	// We use the `Message-Id` value to find the message in this account
	messageID := msg.Envelope.MessageId
	if messageID == "" {
		return fmt.Errorf("cannot append message %d - it has no Message-ID (missing envelope?)", msg.Uid)
	}

	// Fetch message
	uid, err := g.FindUIDByMessageID(messageID)
	if err != nil {
		return fmt.Errorf("failed to fetch message '%d' from account '%s': %w", uid, g.account, err)
	} else if uid == nil {
		return fmt.Errorf("could not find UID for message '%s' in target account", messageID)
	}
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(*uid)

	// Get labels
	var labels []string
	if rawLabels, ok := msg.Items[GmailLabelsExt]; ok {
		if labelInterfaces, ok := rawLabels.([]any); ok {
			for _, l := range labelInterfaces {
				if label, ok := l.(string); ok {
					labels = append(labels, label)
				} else {
					return fmt.Errorf("invalid label type '%T'", l)
				}
			}
			slices.Sort(labels)
		} else {
			return fmt.Errorf("invalid labels type '%T'", rawLabels)
		}
	}
	labelsAsAnyArray := make([]any, len(labels))
	for i, label := range labels {
		labelsAsAnyArray[i] = label
	}
	if err := g.c.UidStore(seqSet, GmailLabelsExt+".SILENT", labelsAsAnyArray, nil); err != nil {
		return fmt.Errorf("failed to update labels of target message '%d': %w", *uid, err)
	}

	// Get flags
	flagsAsAnyArray := make([]any, len(msg.Flags))
	for i, flag := range msg.Flags {
		flagsAsAnyArray[i] = flag
	}
	if err := g.c.UidStore(seqSet, imap.FormatFlagsOp(imap.SetFlags, true), flagsAsAnyArray, nil); err != nil {
		return fmt.Errorf("failed to update flags of target message '%d': %w", *uid, err)
	}

	return nil
}
