package tasks

import (
	"context"
	"time"
)

// uberEatsWakeupsBudget borne un passage (chaque minute) : un compte lent ne
// doit pas empiéter sur les suivants.
const uberEatsWakeupsBudget = 5 * time.Minute

// RunUberEatsWakeups traite les comptes Uber Eats dont le réveil est échu :
// republication du menu s'il a changé, alignement des ruptures de stock sur
// WelloResto (docs/ubereats-menu-sync.md, « Réveils »). Le service prend lui-même
// un bail par compte : deux instances ne traitent pas le même compte en parallèle.
func (tm *TasksManager) RunUberEatsWakeups() {
	if tm.UberEatsService == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), uberEatsWakeupsBudget)
	defer cancel()
	tm.UberEatsService.RunWakeups(ctx)
}
