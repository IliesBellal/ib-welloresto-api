-- Import de carte par photo (porte IA de l'import produits) : brouillons
-- durables et crédits. Voir docs/import-carte-ia-03-porte-ia.md.
--
-- menu_import_drafts : un brouillon par extraction lancée. Les photos sont
-- dans R2 (bucket privé), la lecture de chaque photo par l'IA dans pages
-- (jsonb) : le brouillon survit aux 30 min du snapshot Redis de la preview,
-- qui est simplement redéposé à chaque ouverture. Les lignes ne sont jamais
-- supprimées : elles servent au décompte des crédits (consumes_credit).
--
-- menu_import_ai_credits : surcharge du nombre de crédits d'un marchand,
-- posée par le staff Wello. Sans ligne, le défaut AI_MENU_OCR_DEFAULT_CREDITS
-- s'applique.
--
-- ORDRE DE DÉPLOIEMENT : appliquer cette migration AVANT d'activer
-- AI_TASK_MENU_OCR_ENABLED. Le code peut être déployé avant : les routes de
-- la porte IA ne touchent ces tables que lorsque la tâche est active, et le
-- nettoyage périodique ignore l'erreur d'une table absente (journalisée).
CREATE TABLE IF NOT EXISTS menu_import_drafts (
    id              uuid         PRIMARY KEY,
    merchant_id     varchar(64)  NOT NULL,
    created_by      varchar(64)  NOT NULL,
    source          varchar(32)  NOT NULL DEFAULT 'ai_photo',
    status          varchar(16)  NOT NULL,
    consumes_credit boolean      NOT NULL DEFAULT true,
    pages           jsonb        NOT NULL DEFAULT '[]'::jsonb,
    error           text         NULL,
    created_at      timestamptz  NOT NULL DEFAULT now(),
    updated_at      timestamptz  NOT NULL DEFAULT now(),
    expires_at      timestamptz  NOT NULL,
    committed_at    timestamptz  NULL,
    files_purged_at timestamptz  NULL,
    CONSTRAINT chk_menu_import_drafts_status
        CHECK (status IN ('pending', 'processing', 'ready', 'failed', 'committed', 'expired'))
);

CREATE INDEX IF NOT EXISTS idx_menu_import_drafts_merchant
    ON menu_import_drafts (merchant_id, created_at DESC);

-- Une seule extraction en cours par marchand : la garde tient même sous deux
-- requêtes simultanées (la seconde échoue à l'insertion → 409).
CREATE UNIQUE INDEX IF NOT EXISTS uq_menu_import_drafts_one_running
    ON menu_import_drafts (merchant_id)
    WHERE status IN ('pending', 'processing');

COMMENT ON TABLE menu_import_drafts IS 'Brouillons de l''import de carte par photo (porte IA) : photos R2 et lecture IA par photo.';
COMMENT ON COLUMN menu_import_drafts.pages IS 'Par photo : clé R2, statut, sortie IA validée, erreur, usage (modèle, jetons, latence).';
COMMENT ON COLUMN menu_import_drafts.consumes_credit IS 'Faux si échec technique ou refus de l''IA : l''extraction n''est pas décomptée.';

CREATE TABLE IF NOT EXISTS menu_import_ai_credits (
    merchant_id varchar(64) PRIMARY KEY,
    credits     integer     NOT NULL CHECK (credits >= 0),
    updated_by  varchar(64) NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE menu_import_ai_credits IS 'Nombre de crédits d''import photo d''un marchand, posé par le staff Wello (défaut : AI_MENU_OCR_DEFAULT_CREDITS).';
