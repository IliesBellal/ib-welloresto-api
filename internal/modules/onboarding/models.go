package onboarding

import "time"

// TaskKeys are the five fixed onboarding tasks created at signup (LOT A
// Semaine 2, Chantier 6c). Never extended by the user — status is deduced
// from business events (LOT A Semaine 3, Chantier 13), never checked off
// manually, except for SkippableTaskKeys via the explicit skip endpoint.
var TaskKeys = []string{"menu", "payment", "device", "team", "logo"}

// SkippableTaskKeys are the only two tasks POST
// /v1/merchants/{id}/onboarding/{code}/skip accepts — "menu", "payment" and
// "device" are load-bearing enough (a merchant literally cannot sell without
// a priced menu, cannot be billed without a payment method, cannot run a
// kiosk/POS without a paired device) that the chantier deliberately leaves
// them un-skippable.
var SkippableTaskKeys = map[string]bool{"team": true, "logo": true}

const (
	StatusPending = "pending"
	StatusDone    = "done"
	StatusSkipped = "skipped"
)

type Task struct {
	ID          string     `json:"id"`
	MerchantID  string     `json:"merchant_id"`
	TaskKey     string     `json:"task_key"`
	Status      string     `json:"status"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	SkipReason  *string    `json:"skip_reason,omitempty"`
	SkippedAt   *time.Time `json:"skipped_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
