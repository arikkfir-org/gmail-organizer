package internal

import (
	"fmt"
	"log/slog"

	"github.com/emersion/go-imap/client"
)

const (
	gmailImapHost = "imap.gmail.com"
	gmailImapPort = 993
)

var (
	gmailImapURL = fmt.Sprintf("%s:%d", gmailImapHost, gmailImapPort)
)

func Dial(username, password string) (*client.Client, func(), error) {
	c, err := client.DialTLS(gmailImapURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to IMAP server: %w", err)
	}

	cleanup := func() {
		err := c.Logout()
		if err != nil {
			slog.Warn("Failed to logout from IMAP server", "err", err)
		}
	}

	// Login
	if err := c.Login(username, password); err != nil {
		return nil, cleanup, fmt.Errorf("failed to login: %w", err)
	}

	return c, cleanup, nil
}
