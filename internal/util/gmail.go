package util

var gmailSystemLabels = map[string]bool{
	"[Gmail]":           true,
	"[Gmail]/All Mail":  true,
	"[Gmail]/Drafts":    true,
	"[Gmail]/Important": true,
	"[Gmail]/Sent Mail": true,
	"[Gmail]/Spam":      true,
	"[Gmail]/Starred":   true,
	"[Gmail]/Trash":     true,
}

func IsGmailSystemLabel(name string) bool {
	return gmailSystemLabels[name]
}
