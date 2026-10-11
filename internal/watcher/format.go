package watcher

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"slack-mention-relay/internal/barkclient"
	"slack-mention-relay/internal/notifdb"
)

const (
	// Group collapses relayed notifications into one group on the iPhone.
	Group = "slack-relay"

	// maxPlaintext keeps the encrypted payload (base64 adds ~4/3) well
	// inside APNs' 4 KB limit together with Bark's own fields.
	maxPlaintext = 2000
)

// Format maps a Slack notification to the message sent to the iPhone
// (section 6.4).
//
// PROVISIONAL: which of title/subtitle/body carries the channel, the sender
// and the text for channel mentions, thread mentions and DMs has not been
// confirmed yet (REQUIREMENTS 10-5). Until then the macOS fields are passed
// through unchanged; this function is the only place to adjust.
func Format(n notifdb.Notification) barkclient.Message {
	m := barkclient.Message{
		Title:    strings.TrimSpace(n.Title),
		Subtitle: strings.TrimSpace(n.Subtitle),
		Body:     strings.TrimSpace(n.Body),
		Group:    Group,
	}
	if m.Title == "" {
		m.Title = "Slack"
	}
	if m.Body == "" {
		m.Body = "(本文なし)"
	}
	return fit(m)
}

// fit shortens the body until the JSON plaintext is at most maxPlaintext bytes.
func fit(m barkclient.Message) barkclient.Message {
	size := func() int { b, _ := json.Marshal(m); return len(b) }
	for size() > maxPlaintext {
		over := size() - maxPlaintext
		r := []rune(m.Body)
		cut := len(r) - max(over/2, 1) - 1
		if cut <= 0 {
			m.Body = "…"
			if size() > maxPlaintext {
				m.Subtitle = truncateRunes(m.Subtitle, 50)
				m.Title = truncateRunes(m.Title, 50)
			}
			return m
		}
		m.Body = string(r[:cut]) + "…"
	}
	return m
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
