package outbound

import "strings"

const (
	ChannelEmail = "email"
	ChannelSMS   = "sms"
)

const (
	StatusSent         = "sent"
	StatusDelivered    = "delivered"
	StatusOpened       = "opened"
	StatusClicked      = "clicked"
	StatusBounced      = "bounced"
	StatusFailed       = "failed"
	StatusUnsubscribed = "unsubscribed"
)

var statusPriority = map[string]int{
	StatusSent:         10,
	StatusDelivered:    20,
	StatusOpened:       30,
	StatusClicked:      40,
	StatusBounced:      50,
	StatusFailed:       50,
	StatusUnsubscribed: 60,
}

func NormalizeStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

func IsStatusKnown(status string) bool {
	_, ok := statusPriority[NormalizeStatus(status)]
	return ok
}

func ShouldAdvanceStatus(currentStatus, nextStatus string) bool {
	current := NormalizeStatus(currentStatus)
	next := NormalizeStatus(nextStatus)
	currentRank, currentOK := statusPriority[current]
	nextRank, nextOK := statusPriority[next]

	if !nextOK {
		return false
	}
	if !currentOK {
		return true
	}
	return nextRank > currentRank
}

// CreateParams stores the fields required to persist a tracked outbound message.
type CreateParams struct {
	Channel           string
	Provider          string
	ProviderMessageID string
	Domain            string
	DomainRefID       string
	Recipient         string
	Status            string
}
