-- LOT B B1b (docs/decisions.md) : subscription_items — ce qui est FACTURÉ,
-- distinct de subscriptions.*_enabled qui reste ce à quoi le marchand a
-- ACCÈS (§7.1 du document de référence). Les deux peuvent diverger — c'est
-- le mécanisme de dérogation commerciale (chantier B1d), pas un bug.
--
-- Préfixe d'id demandé par le brief : sbit_. Généré ici via
-- helpers.GeneratePrefixedID("sbit"), qui produit "sbit-<uuid>" (tiret, pas
-- underscore) — même écart déjà observé sur onboarding_tasks
-- (onbt_ documenté vs onb-<uuid> réel, voir docs/decisions.md) : ce dépôt
-- génère systématiquement ses ids "prefix-<uuid>" via ce même helper, quel
-- que soit le séparateur employé dans la documentation de référence. Aligné
-- sur la convention réelle plutôt que sur la notation du brief — l'id est
-- une clé opaque, jamais un contrat exposé comme peut l'être un nom de
-- colonne JSON.
--
-- kind ('plan' | 'module' | 'metered') et code (essentiel | pro | complet |
-- reservation | haccp | planning | planning_employee | marketplaces |
-- delivery | extra_pos | kiosk | sms) : pas de CHECK constraint, même
-- convention que pricing_catalog.kind — validé côté applicatif
-- (subscriptions.ValidKinds/ValidCodes), documenté ici en commentaire.
-- planning_employee et extra_pos sont les deux codes à quantité variable
-- (chantier B1c : recalculée à l'appel, pas lue depuis cette table pour eux).
CREATE TABLE subscription_items (
    id               text PRIMARY KEY,
    merchant_id      text NOT NULL,
    code             text NOT NULL,
    kind             text NOT NULL,
    quantity         integer NOT NULL DEFAULT 1,
    unit_price_cents integer NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    removed_at       timestamptz
);
COMMENT ON COLUMN subscription_items.kind IS 'plan | module | metered — non contraint en base, voir subscriptions.ValidKinds côté Go.';
COMMENT ON COLUMN subscription_items.code IS 'essentiel | pro | complet | reservation | haccp | planning | planning_employee | marketplaces | delivery | extra_pos | kiosk | sms — non contraint en base, voir subscriptions.ValidCodes côté Go.';
COMMENT ON COLUMN subscription_items.unit_price_cents IS 'Prix unitaire figé au moment de l''ajout de la ligne (traçabilité/audit) — le calcul du montant facturé (chantier B1c) relit la table de référence tarifaire à l''appel plutôt que cette colonne, pour rester robuste à une évolution ultérieure de la grille.';
COMMENT ON COLUMN subscription_items.removed_at IS 'NULL = ligne active et facturée. Non-NULL = retirée (module désactivé, changement de pack) — jamais supprimée physiquement, pour l''historique de facturation.';

CREATE INDEX idx_subscription_items_merchant_id ON subscription_items (merchant_id);

-- Une seule ligne active par (merchant_id, code) à la fois — deux lignes
-- actives pour le même code serait une double facturation. Index unique
-- partiel (removed_at IS NULL) plutôt qu'une contrainte UNIQUE classique,
-- pour permettre l'historique (plusieurs lignes retirées) sans le bloquer.
CREATE UNIQUE INDEX idx_subscription_items_merchant_code_active ON subscription_items (merchant_id, code) WHERE removed_at IS NULL;

-- ============================================================================
-- Colonnes de facturation sur subscriptions (chantier B1b)
-- ============================================================================
ALTER TABLE subscriptions
    ADD COLUMN IF NOT EXISTS override_price_cents integer,
    ADD COLUMN IF NOT EXISTS billing_cycle text NOT NULL DEFAULT 'monthly',
    ADD COLUMN IF NOT EXISTS current_period_end timestamptz,
    ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'setup';

COMMENT ON COLUMN subscriptions.override_price_cents IS 'NULL = tarif de grille (calculé depuis subscription_items). 0 = gratuit. Toute autre valeur = montant dérogatoire fixe, prioritaire sur le calcul (chantier B1c) — le détail par ligne reste calculé et retourné pour affichage comparatif.';
COMMENT ON COLUMN subscriptions.billing_cycle IS 'monthly | annual.';
COMMENT ON COLUMN subscriptions.current_period_end IS 'Fin de la période de facturation en cours. NULL tant qu''aucun cycle de facturation réel n''a démarré (LOT B2, mandat SEPA).';
COMMENT ON COLUMN subscriptions.status IS 'setup | active | past_due | suspended | canceled.';

-- ============================================================================
-- ATTENTION — rattrapage obligatoire (même vigilance que activation_state,
-- LOT A Semaine 2, migration 125) : status a un défaut 'setup' — sans cette
-- instruction, tout le parc de marchands déjà en exploitation se
-- retrouverait marqué "en cours de configuration" dès l'ALTER TABLE ci-dessus
-- (Postgres applique le DEFAULT à toutes les lignes existantes lors d'un ADD
-- COLUMN NOT NULL DEFAULT).
-- ============================================================================
UPDATE subscriptions
SET status = 'active'
WHERE status = 'setup';
