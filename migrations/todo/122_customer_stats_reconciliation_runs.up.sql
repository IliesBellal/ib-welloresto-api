-- PROMPT 26 Phase 4 — Garde-fou anti-redérive des compteurs client.
--
-- L'infrastructure cron de ce dépôt n'a aucun journal d'exécution (CLAUDE.md,
-- "Real-time & background jobs") : impossible de savoir si une tâche a
-- tourné. Cette table est la trace exploitable pour CE contrôle précis —
-- pas une solution générale au problème (hors périmètre de ce lot) —
-- alimentée par tasks.TasksManager.ReconcileCustomerStats
-- (internal/tasks/customer_stats.go), une ligne par exécution quotidienne.
CREATE TABLE customer_stats_reconciliation_runs (
    id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_at timestamptz NOT NULL DEFAULT now(),
    sample_size integer NOT NULL,
    mismatches integer NOT NULL,
    mismatch_ratio double precision NOT NULL,
    max_nb_orders_diff integer NOT NULL,
    max_total_spent_diff_cents bigint NOT NULL,
    alert_threshold_ratio double precision NOT NULL,
    alert_triggered boolean NOT NULL,
    duration_ms integer NOT NULL
);

CREATE INDEX idx_customer_stats_reconciliation_runs_run_at
    ON customer_stats_reconciliation_runs (run_at DESC);

COMMENT ON TABLE customer_stats_reconciliation_runs IS 'Historique du contrôle quotidien anti-redérive des compteurs client (PROMPT 26 Phase 4) — une ligne par exécution, seule trace de passage de cette tâche cron.';
