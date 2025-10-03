package gcp

import (
	"fmt"
	"log/slog"
	"slices"
	"sync"

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

type Gmail struct {
	username string
	password string
	mu       sync.Mutex
	conns    chan *client.Client
	factory  func() (*client.Client, error)
}

func NewGmail(username, password string) *Gmail {
	return &Gmail{
		username: username,
		password: password,
		conns:    make(chan *client.Client, 10),
		factory: func() (*client.Client, error) {
			if c, err := client.DialTLS(gmailImapURL, nil); err != nil {
				return nil, fmt.Errorf("failed to dial: %w", err)
			} else if err := c.Login(username, password); err != nil {
				return nil, fmt.Errorf("failed to login: %w", err)
			} else {
				return c, nil
			}
		},
	}
}

func (g *Gmail) releaseIMAPConnection(c *client.Client) {
	select {
	case g.conns <- c:
	default:
		if err := c.Logout(); err != nil {
			slog.Warn("Failed logging out of a IMAP connection due to full pool", "err", err, "username", g.username)
		}
	}
}

func (g *Gmail) getIMAPConnection() (*client.Client, func(), error) {
	select {
	case c := <-g.conns:
		if err := c.Noop(); err != nil {
			if err := c.Logout(); err != nil {
				slog.Warn("Failed logging out of a bad IMAP connection", "err", err, "username", g.username)
			}
			return g.getIMAPConnection()
		}
		return c, func() { g.releaseIMAPConnection(c) }, nil
	default:
		slog.Info("Connecting to Gmail IMAP server", "username", g.username)
		c, err := g.factory()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create new connection: %w", err)
		}
		return c, func() { g.releaseIMAPConnection(c) }, nil
	}
}

func (g *Gmail) Close() {
	close(g.conns)
	for c := range g.conns {
		if err := c.Logout(); err != nil {
			slog.Warn("Failed to logout from Gmail IMAP server (closing pool)", "err", err, "username", g.username)
		}
	}
}

func (g *Gmail) FindAllUIDs(mailbox string) ([]uint32, error) {
	c, release, err := g.getIMAPConnection()
	if err != nil {
		return nil, fmt.Errorf("failed to get Gmail connection: %w", err)
	}
	defer release()

	if _, err := c.Select(mailbox, true); err != nil {
		return nil, fmt.Errorf("failed to select '%s' in account %s: %w", mailbox, g.username, err)
	}

	criteria := imap.NewSearchCriteria()
	criteria.SeqNum = new(imap.SeqSet)
	criteria.SeqNum.AddRange(1, 0)
	uids, err := c.UidSearch(criteria)
	if err != nil {
		return nil, fmt.Errorf("failed performing criteria search for all UIDs: %w", err)
	}
	return uids, nil
}

func (g *Gmail) FetchByUIDs(mailbox string, uids []uint32, items ...imap.FetchItem) ([]*imap.Message, error) {
	c, release, err := g.getIMAPConnection()
	if err != nil {
		return nil, fmt.Errorf("failed to get Gmail connection: %w", err)
	}
	defer release()

	if _, err := c.Select(mailbox, true); err != nil {
		return nil, fmt.Errorf("failed to select '%s' in account %s: %w", mailbox, g.username, err)
	}

	if !slices.Contains(items, imap.FetchUid) {
		items = append(items, imap.FetchUid)
	}
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uids...)
	messagesCh := make(chan *imap.Message, len(uids))
	if err := c.UidFetch(seqSet, items, messagesCh); err != nil {
		return nil, fmt.Errorf("failed to fetch message IDs: %w", err)
	}
	messages := make([]*imap.Message, 0, len(uids))
	for msg := range messagesCh {
		messages = append(messages, msg)
	}
	return messages, nil
}

func (g *Gmail) FindUIDByMessageID(mailbox string, messageID string) (*uint32, error) {
	c, release, err := g.getIMAPConnection()
	if err != nil {
		return nil, fmt.Errorf("failed to get Gmail connection: %w", err)
	}
	defer release()

	if _, err := c.Select(mailbox, true); err != nil {
		return nil, fmt.Errorf("failed to select '%s' in account %s: %w", mailbox, g.username, err)
	}

	criteria := imap.NewSearchCriteria()
	criteria.Header.Add("Message-Id", messageID)
	uids, err := c.UidSearch(criteria)
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

func (g *Gmail) FetchMessageByUID(mailbox string, uid uint32, items ...imap.FetchItem) (*imap.Message, error) {
	c, release, err := g.getIMAPConnection()
	if err != nil {
		return nil, fmt.Errorf("failed to get Gmail connection: %w", err)
	}
	defer release()

	if _, err := c.Select(mailbox, true); err != nil {
		return nil, fmt.Errorf("failed to select '%s' in account %s: %w", mailbox, g.username, err)
	}

	if !slices.Contains(items, imap.FetchUid) {
		items = append(items, imap.FetchUid)
	}
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uid)
	messages := make(chan *imap.Message, 1)
	if err := c.UidFetch(seqSet, items, messages); err != nil {
		return nil, fmt.Errorf("failed to fetch message '%d' from account '%s': %w", uid, g.username, err)
	}
	msg := <-messages
	if msg == nil {
		return nil, fmt.Errorf("server did not provide message '%d' from account '%s'", uid, g.username)
	}

	return msg, nil
}

func (g *Gmail) AppendMessage(mailbox string, msg *imap.Message) (uint32, error) {
	c, release, err := g.getIMAPConnection()
	if err != nil {
		return 0, fmt.Errorf("failed to get Gmail connection: %w", err)
	}
	defer release()

	if _, err := c.Select(mailbox, false); err != nil {
		return 0, fmt.Errorf("failed to select '%s' in account %s: %w", mailbox, g.username, err)
	}

	if msg.Uid == 0 {
		return 0, fmt.Errorf("cannot append message %d - it has no UID", msg.Uid)
	}

	r := msg.GetBody(&imap.BodySectionName{})
	if r == nil {
		return 0, fmt.Errorf("cannot append message %d - it is missing body", msg.Uid)
	}

	if err := c.Append(GmailAllMailLabel, msg.Flags, msg.InternalDate, r); err != nil {
		return 0, fmt.Errorf("failed to append message %d to target: %w", msg.Uid, err)
	}

	messageID := msg.Envelope.MessageId
	uid, err := g.FindUIDByMessageID(mailbox, messageID)
	if err != nil {
		return 0, fmt.Errorf("failed to find UID for newly-appended message '%s' in target account: %w", messageID, err)
	} else if uid == nil {
		return 0, fmt.Errorf("could not find UID for newly appended message '%s' in target account", messageID)
	}

	var labels []string
	if rawLabels, ok := msg.Items[GmailLabelsExt]; ok {
		if labelInterfaces, ok := rawLabels.([]any); ok {
			for _, l := range labelInterfaces {
				if label, ok := l.(string); ok {
					labels = append(labels, label)
				} else {
					return 0, fmt.Errorf("invalid label type '%T'", l)
				}
			}
			slices.Sort(labels)
		} else {
			return 0, fmt.Errorf("invalid labels type '%T'", rawLabels)
		}
	}
	labelsAsAnyArray := make([]any, len(labels))
	for i, label := range labels {
		labelsAsAnyArray[i] = label
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddNum(*uid)
	if err := c.UidStore(seqSet, GmailLabelsExt+".SILENT", labelsAsAnyArray, nil); err != nil {
		return 0, fmt.Errorf("failed to store labels on target message '%d': %w", *uid, err)
	}

	return *uid, nil
}

func (g *Gmail) UpdateMessage(mailbox string, msg *imap.Message) error {
	c, release, err := g.getIMAPConnection()
	if err != nil {
		return fmt.Errorf("failed to get Gmail connection: %w", err)
	}
	defer release()

	if _, err := c.Select(mailbox, false); err != nil {
		return fmt.Errorf("failed to select '%s' in account %s: %w", mailbox, g.username, err)
	}

	// We use the `Message-Id` value to find the message in this account
	messageID := msg.Envelope.MessageId
	if messageID == "" {
		return fmt.Errorf("cannot append message %d - it has no Message-ID (missing envelope?)", msg.Uid)
	}

	// Fetch message
	uid, err := g.FindUIDByMessageID(mailbox, messageID)
	if err != nil {
		return fmt.Errorf("failed to fetch message '%d' from account '%s': %w", uid, g.username, err)
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
	if err := c.UidStore(seqSet, GmailLabelsExt+".SILENT", labelsAsAnyArray, nil); err != nil {
		return fmt.Errorf("failed to update labels of target message '%d': %w", *uid, err)
	}

	// Get flags
	flagsAsAnyArray := make([]any, len(msg.Flags))
	for i, flag := range msg.Flags {
		flagsAsAnyArray[i] = flag
	}
	if err := c.UidStore(seqSet, imap.FormatFlagsOp(imap.SetFlags, true), flagsAsAnyArray, nil); err != nil {
		return fmt.Errorf("failed to update flags of target message '%d': %w", *uid, err)
	}

	return nil
}
