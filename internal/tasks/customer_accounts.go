package tasks

import (
	"context"
	"time"
)

// CustomerAccountReminderRunner envoie les relances automatiques des ardoises
// (customer_accounts.Service.RunReminders).
type CustomerAccountReminderRunner interface {
	RunReminders(ctx context.Context)
}

// SendCustomerAccountReminders : tâche horaire ; chaque établissement est
// relancé une fois par jour, à 10 h heure locale (docs/customer-accounts.md).
func (tm *TasksManager) SendCustomerAccountReminders() {
	if tm.CustomerAccounts == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	tm.CustomerAccounts.RunReminders(ctx)
}
