-- LOT A Semaine 3, Chantier 13 (docs/decisions.md) : POST
-- /v1/merchants/{id}/onboarding/{code}/skip nécessite de tracer le motif
-- obligatoire et la date du skip, distincts de completed_at (qui reste
-- réservé à la complétion réelle déduite d'un événement métier).
ALTER TABLE onboarding_tasks
    ADD COLUMN IF NOT EXISTS skip_reason text,
    ADD COLUMN IF NOT EXISTS skipped_at timestamptz;
