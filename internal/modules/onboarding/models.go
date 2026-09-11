package onboarding

import "time"

// TaskKeys are the five fixed onboarding tasks created at signup (LOT A
// Semaine 2, Chantier 6c). Never extended by the user — status is deduced
// from business events (semaine 3, not built by this chantier), never
// checked off manually.
var TaskKeys = []string{"menu", "payment", "device", "team", "logo"}

const (
	StatusPending = "pending"
	StatusDone    = "done"
)

type Task struct {
	ID          string     `json:"id"`
	MerchantID  string     `json:"merchant_id"`
	TaskKey     string     `json:"task_key"`
	Status      string     `json:"status"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
