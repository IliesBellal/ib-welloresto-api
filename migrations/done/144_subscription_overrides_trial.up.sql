-- LOT B B2c-1 (docs/decisions.md) : "prix de remplacement à échéance" —
-- une dérogation (n'importe quel kind, pas seulement 'price' malgré le nom
-- du chantier) peut désormais porter une échéance. NULL = comportement
-- actuel inchangé (sans limite) — n'affecte aucune dérogation existante.
--
-- Les deux colonnes *_sent_at suivent le même principe que
-- subscription_dunning (B2b-1) : rien n'est jamais planifié à l'avance, le
-- cron réévalue à chaque exécution si un rappel est dû et note ici qu'il
-- l'a envoyé, pour ne jamais le renvoyer.
ALTER TABLE subscription_overrides
    ADD COLUMN IF NOT EXISTS trial_ends_at timestamptz,
    ADD COLUMN IF NOT EXISTS trial_reminder_7d_sent_at timestamptz,
    ADD COLUMN IF NOT EXISTS trial_reminder_1d_sent_at timestamptz;
COMMENT ON COLUMN subscription_overrides.trial_ends_at IS 'NULL = sans limite (comportement historique). Une date : à l''échéance, voir subscriptions.Service.RunTrialExpiryCheck — revocation automatique, et pour kind=''price'' spécifiquement, retour de merchant.activation_state à ''SETUP'' si aucun mandat SEPA réel n''a pris le relais.';
