package daycomments

import (
	"time"

	"welloresto-api/internal/models"
)

// MaxCommentLength caps the free-text day comment so it stays readable in a
// grid-cell popover (back-office) or a tooltip on a POS tablet. This is a
// short operational note, not a document.
const MaxCommentLength = 400

// PlanningDayComment is a single free-text note attached to a calendar day
// for a merchant. It has no relationship to a PlanningWeek: it can exist
// before a week is created for that date, and it survives if the week is
// later deleted. Deletion is a hard delete (no enabled/deleted_at) since
// this note has no audit/legal retention requirement.
type PlanningDayComment struct {
	ID          string          `json:"id"`
	MerchantID  string          `json:"merchant_id"`
	CommentDate models.DateOnly `json:"comment_date"`
	Comment     string          `json:"comment"`
	CreatedBy   *string         `json:"created_by,omitempty"`
	UpdatedBy   *string         `json:"updated_by,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// PlanningDayCommentUpsertRequest is the body of
// `PUT /planning/day-comments/{date}`. An empty/blank comment is rejected —
// callers wanting to clear a day should use DELETE instead, so intent stays
// explicit.
type PlanningDayCommentUpsertRequest struct {
	Comment string `json:"comment"`
}
