// Package dunning implements LOT B B2b-1's cascade d'impayé (§7.5) —
// subscriptions.status transitions past_due -> suspended (or back to
// active), driven by invoice.payment_failed/invoice.paid webhooks and a
// cron re-evaluating what's due each time it runs (never a pre-scheduled
// future send — see subscription_dunning's table comment).
package dunning

import "time"

// State is one row of subscription_dunning — see
// migrations/todo/142_subscription_dunning.up.sql for the field semantics.
type State struct {
	MerchantID         string
	FirstFailedAt      time.Time
	SecondFailedAt     *time.Time
	SuspensionDeadline *time.Time
	ReminderCount      int
	LastReminderSentAt *time.Time
	FinalNoticeSentAt  *time.Time
}

// suspensionWindow — "fenêtre de 14 jours" (§7.5), opened by the 2nd
// invoice.payment_failed.
const suspensionWindow = 14 * 24 * time.Hour

// weeklyReminderInterval — "chaque semaine tant que past_due".
const weeklyReminderInterval = 7 * 24 * time.Hour

// finalNoticeLeadTime — "48h avant la fin des 14 jours".
const finalNoticeLeadTime = 48 * time.Hour
