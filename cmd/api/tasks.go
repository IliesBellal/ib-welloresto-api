package main

import (
	"welloresto-api/internal/tasks"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

func SetupTasks(
	log *zap.Logger,
	taskManager *tasks.TasksManager,
) {
	// Logger cron branché sur zap (verbose pour tracer les skips).
	cronLog := cron.VerbosePrintfLogger(zap.NewStdLog(log.Named("cron")))

	// Chaîne appliquée à chaque job :
	//  - SkipIfStillRunning : jamais deux exécutions parallèles d'un même job
	//    (critique avec le pool MySQL limité à 1 connexion) ;
	//  - Recover : un panic dans une tâche ne tue pas le process.
	// Recover est volontairement À L'INTÉRIEUR de SkipIfStillRunning : un panic
	// non récupéré laisserait le job marqué "en cours" et il ne serait plus
	// jamais exécuté.
	c := cron.New(cron.WithChain(
		cron.SkipIfStillRunning(cronLog),
		cron.Recover(cronLog),
	))

	add := func(spec string, job func()) {
		if _, err := c.AddFunc(spec, job); err != nil {
			log.Error("CRON: enregistrement de tâche échoué",
				zap.String("spec", spec), zap.Error(err))
		}
	}

	// ── Réservation ──────────────────────────────────────────────────────────
	add("@hourly", taskManager.ExpirePendingBookings)
	add("@every 5m", taskManager.ExpireWaitlistNotifications)
	add("@every 30m", taskManager.SendBookingReminders)

	// ── Commandes / paiements ────────────────────────────────────────────────
	// Un job par tâche : SkipIfStillRunning protège chaque tâche
	// indépendamment et une tâche lente n'en retarde pas une autre.
	add("@hourly", taskManager.CloseOrders)
	add("@every 1m", taskManager.DenyOrders)
	add("@hourly", taskManager.SendLoyaltyProgrammReminder) // stub non implémenté (no-op)
	add("@hourly", taskManager.CapturePayments)
	add("@hourly", taskManager.CancelPayments)

	// ── Temps de préparation moyen (simulation capacité parallèle) ──────────
	add("@every 15m", taskManager.UpdateAverageDistributionTime)

	// ── Temps de trajet livraison moyen (fallback + estimation pré-checkout) ──
	add("@every 15m", taskManager.UpdateAverageDeliveryTime)

	// ── Produits populaires : fenêtre glissante 30 jours, recalcul quotidien
	// à 2h du matin (un recalcul mensuel laissait les flags périmés). ────────
	add("0 2 * * *", taskManager.UpdatePopularProducts)

	// Chaque nuit à 3h : recalcul des patterns market basket
	add("0 3 * * *", taskManager.RecomputeUpsellPatterns)

	// 1er du mois à 4h : purge des anciennes suggestions
	add("0 4 1 * *", taskManager.CleanupOldUpsellSuggestions)

	// ── Sécurité ─────────────────────────────────────────────────────────────
	// Chaque nuit à 4h30 : purge des demandes de réinitialisation de mot de passe.
	// 4h30 et non 4h pour ne pas tomber en même temps que la purge upsell le 1er
	// du mois — deux DELETE simultanés se disputeraient l'unique connexion DB.
	add("30 4 * * *", taskManager.CleanupExpiredPasswordResets)

	// ── Logs de requêtes API ─────────────────────────────────────────────────
	// 1er du mois à 5h : purge des lignes api_request_logs de plus de 30 jours.
	// Coïncide avec CleanupExpiredPasswordResets ce jour-là, mais la connexion
	// active est désormais Postgres sur Render (pool de 15, pas 1 comme sur
	// l'ancien MySQL Hostinger) : les deux DELETE peuvent tourner en parallèle
	// sans se disputer une connexion unique.
	add("0 5 1 * *", taskManager.CleanupOldRequestLogs)

	// Démarrage du Cron en arrière-plan
	c.Start()
	log.Info("✅ Système CRON démarré (toutes tâches actives, protégées par SkipIfStillRunning + Recover)")
}
