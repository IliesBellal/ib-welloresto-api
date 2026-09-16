-- Idempotence des webhooks Stripe (audit wello-kiosk/docs/AUDIT_STRIPE_TERMINAL.md
-- §8 points 4/8/9, voir docs/KIOSK_DECISIONS.md) : le webhook Stripe devient
-- progressivement la seule source de vérité pour le paiement carte Kiosk
-- (migration server-driven à venir) et doit donc être sûr contre un event
-- rejoué par Stripe (retry automatique sur non-200, ou double delivery réelle).
--
-- event_id est la clé Stripe (evt_...), globalement unique quel que soit le
-- type d'event ou le compte (plateforme/connecté) qui l'a émis. Une ligne est
-- insérée avant traitement (StripeWebhookService.ProcessEvent) et retirée si
-- le traitement échoue, pour laisser un retry Stripe légitime reprocesser
-- l'event plus tard plutôt que de figer un échec en faux "déjà traité".
CREATE TABLE IF NOT EXISTS stripe_webhook_events (
    event_id text NOT NULL,
    event_type varchar(100) NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id)
);
