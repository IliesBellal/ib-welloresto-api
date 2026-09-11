-- LOT A Semaine 2, Chantier 6c (docs/decisions.md) : onboarding_tasks.
--
-- Cinq lignes créées à l'inscription (menu, payment, device, team, logo),
-- statut 'pending' au départ — la déduction automatique depuis des
-- événements métier vient en semaine 3 (non construite ici : ce chantier ne
-- fait que créer les lignes et l'endpoint de lecture, comme demandé).
CREATE TABLE onboarding_tasks (
    id           text PRIMARY KEY,
    merchant_id  text NOT NULL,
    task_key     text NOT NULL,
    status       text NOT NULL DEFAULT 'pending',
    completed_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (merchant_id, task_key)
);
COMMENT ON COLUMN onboarding_tasks.task_key IS 'menu | payment | device | team | logo — fixé à la création (chantier 6), jamais étendu par l''utilisateur.';
COMMENT ON COLUMN onboarding_tasks.status IS 'pending | done — jamais coché manuellement : déduit d''événements métier (semaine 3, non construit par ce chantier).';

CREATE INDEX idx_onboarding_tasks_merchant_id ON onboarding_tasks (merchant_id);
