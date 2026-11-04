package tasks

import (
	"context"
	"time"
)

// deliverooWakeupsBudget borne un passage (chaque minute) : un compte lent ne
// doit pas empiéter sur les suivants.
const deliverooWakeupsBudget = 3 * time.Minute

// RunDeliverooWakeups traite les comptes Deliveroo dont le réveil est échu :
// horaires d'ouverture, republication du menu s'il a changé, alignement des
// disponibilités sur WelloResto (docs/deliveroo-menu-sync.md, « Réveils »).
// Le service prend lui-même un bail par compte : deux instances ne traitent
// pas le même compte en parallèle.
func (tm *TasksManager) RunDeliverooWakeups() {
	if tm.DeliverooService == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), deliverooWakeupsBudget)
	defer cancel()
	tm.DeliverooService.RunWakeups(ctx)
}
