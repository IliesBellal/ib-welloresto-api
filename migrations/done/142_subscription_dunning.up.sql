-- LOT B B2b-1 (docs/decisions.md) : subscription_dunning — l'état de la
-- cascade d'impayé (§7.5), distinct de subscriptions.status (le résumé
-- "active/past_due/suspended" déjà posé en B1b) : cette table porte les
-- dates qui pilotent QUAND agir, réévaluées à chaque exécution du cron de
-- relance (internal/tasks/dunning.go) — "jamais un envoi planifié à
-- l'avance" : aucune ligne ici ne représente un envoi futur déjà décidé,
-- seulement l'état déjà survenu (échecs, dernier envoi).
CREATE TABLE subscription_dunning (
    merchant_id           text PRIMARY KEY,
    -- Posé au 1er invoice.payment_failed. Efface (avec toute la ligne) au
    -- prochain invoice.paid.
    first_failed_at       timestamptz NOT NULL,
    -- Posé au 2e invoice.payment_failed — ouvre la fenêtre de 14 jours.
    -- NULL tant qu'un seul échec a eu lieu.
    second_failed_at      timestamptz,
    -- first_failed_at + 14 jours, calculé et figé au moment du 2e échec
    -- (pas recalculé à la volée) — une valeur stable à annoncer aux
    -- relances, cf. §7.5 "annonce la date de suspension".
    suspension_deadline   timestamptz,
    reminder_count        integer NOT NULL DEFAULT 0,
    last_reminder_sent_at timestamptz,
    -- Le dernier SMS+courriel à 48h de l'échéance — un seul envoi, jamais
    -- rejoué (guard applicatif sur cette colonne, pas une contrainte SQL).
    final_notice_sent_at  timestamptz,
    updated_at            timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE subscription_dunning IS 'Une ligne = un merchant actuellement en cascade (status past_due). Supprimée (pas juste réinitialisée) au retour à active, pour que "aucune ligne" = "aucune cascade en cours" reste la seule source de vérité côté cron.';
