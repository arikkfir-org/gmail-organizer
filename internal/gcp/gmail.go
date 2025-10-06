package gcp

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"
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
	getConnTimeout time.Duration
	newConnMU      sync.Mutex
	username       string
	password       string
	mu             sync.Mutex
	conns          chan *client.Client
	factory        func(context.Context) (*client.Client, error)
}

func NewGmail(username, password string, connLimit uint8, getConnTimeout time.Duration) (*Gmail, error) {
	g := &Gmail{
		getConnTimeout: getConnTimeout,
		username:       username,
		password:       password,
		conns:          make(chan *client.Client, connLimit),
		factory: func(ctx context.Context) (*client.Client, error) {
			return backoff.Retry[*client.Client](
				ctx,
				func() (*client.Client, error) {
					if c, err := client.DialTLS(gmailImapURL, nil); err != nil {
						return nil, fmt.Errorf("failed to dial: %w", err)
					} else if err := c.Login(username, password); err != nil {
						return nil, fmt.Errorf("failed to login: %w", err)
					} else {
						return c, nil
					}
				},
				backoff.WithBackOff(backoff.NewExponentialBackOff()),
			)
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var i uint8
	for i = 0; i < connLimit; i++ {
		time.Sleep(time.Second)
		go func(i uint8) {
			if c, err := g.factory(ctx); err != nil {
				slog.Warn("Failed to create initial IMAP connection", "err", err, "username", g.username)
			} else {
				slog.Debug("Creating initial IMAP connection", "index", i, "username", g.username)
				g.conns <- c
			}
		}(i)
	}

	return g, nil
}

func (g *Gmail) Close() {
	close(g.conns)
	for c := range g.conns {
		if c != nil {
			if err := c.Logout(); err != nil {
				if !strings.Contains(err.Error(), "Already logged out") {
					slog.Warn("Failed to logout from Gmail IMAP server (closing pool)", "err", err, "username", g.username)
				}
			}
		}
	}
}

func (g *Gmail) releaseIMAPConnection(c *client.Client) {
	slog.Debug("Releasing IMAP connection", "username", g.username)
	g.conns <- c
}

func (g *Gmail) getIMAPConnection(ctx context.Context) (*client.Client, func(), error) {
	timer := time.NewTimer(g.getConnTimeout)
	defer timer.Stop()

	select {
	case c := <-g.conns:
		// Should never happen, but just in case
		if c == nil {
			panic("ILLEGAL STATE: got nil IMAP connection from pool")
		}

		if err := c.Noop(); err != nil {

			// Discard the bad connection
			slog.Warn("Discarding bad IMAP connection", "err", err, "username", g.username)
			if err := c.Logout(); err != nil {
				if !strings.Contains(err.Error(), "Already logged out") {
					slog.Warn("Failed logging out of a bad IMAP connection", "err", err, "username", g.username)
				}
			}

			// Create a new one in place of the one we just discarded
			if c, err := g.factory(ctx); err != nil {
				return nil, nil, fmt.Errorf("failed to create initial IMAP connections: %w", err)
			} else {
				return c, func() { g.releaseIMAPConnection(c) }, nil
			}

		} else {
			// Good connection, return it
			return c, func() { g.releaseIMAPConnection(c) }, nil
		}
	case <-timer.C:
		// Timed out :(
		return nil, nil, fmt.Errorf("failed to get IMAP connection within timeout")
	}
}

func (g *Gmail) FindAllUIDs(ctx context.Context, mailbox string) ([]uint32, error) {
	return backoff.Retry[[]uint32](
		ctx,
		func() ([]uint32, error) {
			c, release, err := g.getIMAPConnection(ctx)
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
		},
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
	)
}

func (g *Gmail) FetchByUIDs(ctx context.Context, mailbox string, uids []uint32, items ...imap.FetchItem) ([]*imap.Message, error) {
	return backoff.Retry[[]*imap.Message](
		ctx,
		func() ([]*imap.Message, error) {
			c, release, err := g.getIMAPConnection(ctx)
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
		},
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
	)
}

func (g *Gmail) FindUIDByMessageID(ctx context.Context, mailbox string, messageID string) (*uint32, error) {
	return backoff.Retry[*uint32](
		ctx,
		func() (*uint32, error) {
			c, release, err := g.getIMAPConnection(ctx)
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
			} else {
				if len(uids) > 1 {
					slog.Warn("Found multiple UIDs for Message-ID", "messageID", messageID, "uids", uids)
				}
				return &uids[0], nil
			}
		},
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
	)
}

func (g *Gmail) FetchMessageByUID(ctx context.Context, mailbox string, uid uint32, items ...imap.FetchItem) (*imap.Message, error) {
	return backoff.Retry[*imap.Message](
		ctx,
		func() (*imap.Message, error) {
			c, release, err := g.getIMAPConnection(ctx)
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
		},
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
	)
}

func (g *Gmail) AppendMessage(ctx context.Context, mailbox string, msg *imap.Message) (uint32, error) {
	return backoff.Retry[uint32](
		ctx,
		func() (uint32, error) {
			c, release, err := g.getIMAPConnection(ctx)
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
			uid, err := g.FindUIDByMessageID(ctx, mailbox, messageID)
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
		},
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
	)
}

func (g *Gmail) UpdateMessage(ctx context.Context, mailbox string, msg *imap.Message) error {
	_, err := backoff.Retry(
		ctx,
		func() (any, error) {
			c, release, err := g.getIMAPConnection(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to get Gmail connection: %w", err)
			}
			defer release()

			if _, err := c.Select(mailbox, false); err != nil {
				return nil, fmt.Errorf("failed to select '%s' in account %s: %w", mailbox, g.username, err)
			}

			// We use the `Message-Id` value to find the message in this account
			messageID := msg.Envelope.MessageId
			if messageID == "" {
				return nil, fmt.Errorf("cannot append message %d - it has no Message-ID (missing envelope?)", msg.Uid)
			}

			// Fetch message
			uid, err := g.FindUIDByMessageID(ctx, mailbox, messageID)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch message '%d' from account '%s': %w", uid, g.username, err)
			} else if uid == nil {
				return nil, fmt.Errorf("could not find UID for message '%s' in target account", messageID)
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
							return nil, fmt.Errorf("invalid label type '%T'", l)
						}
					}
					slices.Sort(labels)
				} else {
					return nil, fmt.Errorf("invalid labels type '%T'", rawLabels)
				}
			}
			labelsAsAnyArray := make([]any, len(labels))
			for i, label := range labels {
				labelsAsAnyArray[i] = label
			}
			if err := c.UidStore(seqSet, GmailLabelsExt+".SILENT", labelsAsAnyArray, nil); err != nil {
				return nil, fmt.Errorf("failed to update labels of target message '%d': %w", *uid, err)
			}

			// Get flags
			flagsAsAnyArray := make([]any, len(msg.Flags))
			for i, flag := range msg.Flags {
				flagsAsAnyArray[i] = flag
			}
			if err := c.UidStore(seqSet, imap.FormatFlagsOp(imap.SetFlags, true), flagsAsAnyArray, nil); err != nil {
				return nil, fmt.Errorf("failed to update flags of target message '%d': %w", *uid, err)
			}

			return nil, nil
		},
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
	)
	return err
}

func (g *Gmail) FetchMailboxNames(ctx context.Context, ignoreSystemLabels, ignoreUnselectables bool) ([]string, error) {
	return backoff.Retry[[]string](
		ctx,
		func() ([]string, error) {
			c, release, err := g.getIMAPConnection(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to get Gmail connection: %w", err)
			}
			defer release()

			imapMailBoxes := make(chan *imap.MailboxInfo, 100)
			done := make(chan error, 1)
			go func() {
				done <- c.List("", "*", imapMailBoxes)
			}()
			var names []string
			for m := range imapMailBoxes {
				if ignoreSystemLabels {
					if m.Name == "INBOX" {
						continue
					} else if strings.HasPrefix(m.Name, "[Gmail]") {
						continue
					} else if slices.Contains(m.Attributes, imap.AllAttr) {
						continue
					} else if slices.Contains(m.Attributes, imap.DraftsAttr) {
						continue
					} else if slices.Contains(m.Attributes, imap.JunkAttr) {
						continue
					} else if slices.Contains(m.Attributes, imap.TrashAttr) {
						continue
					} else if slices.Contains(m.Attributes, imap.ArchiveAttr) {
						continue
					} else if slices.Contains(m.Attributes, imap.SentAttr) {
						continue
					} else if slices.Contains(m.Attributes, imap.FlaggedAttr) {
						continue
					}
				}
				if ignoreUnselectables && slices.Contains(m.Attributes, imap.NoSelectAttr) {
					continue
				}
				names = append(names, m.Name)
			}
			if err := <-done; err != nil {
				return nil, fmt.Errorf("failed to fetch mailboxes names: %w", err)
			}
			return names, nil
		},
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
	)
}

func (g *Gmail) CreateMailboxes(ctx context.Context, names ...string) error {
	_, err := backoff.Retry[any](
		ctx,
		func() (any, error) {
			c, release, err := g.getIMAPConnection(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to get Gmail connection: %w", err)
			}
			defer release()

			for _, name := range names {
				if err := c.Create(name); err != nil {
					if !strings.Contains(err.Error(), "Duplicate folder name") {
						return nil, fmt.Errorf("failed to create mailbox '%s': %w", name, err)
					}
				}
			}

			return nil, nil
		},
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
	)
	return err
}
