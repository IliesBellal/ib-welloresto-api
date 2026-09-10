-- RBAC lot 2, phase d'observation.
--
-- Une ligne par (merchant, user, permission, route) rencontrée par
-- RequirePermission, avec la décision qui a été prise (granted) — pas
-- seulement qui passe par où, mais aussi qui se ferait refuser l'accès sous
-- le nouveau modèle. C'est la table à partir de laquelle la matrice
-- droits/rôles sera construite avant d'assigner le premier role_id réel.
--
-- Écrite en PostgreSQL. Note MySQL : la clause d'upsert utilisée par
-- l'observeur (internal/middleware/rbacobserve) est ON CONFLICT ... DO UPDATE
-- ici ; l'équivalent MySQL est ON DUPLICATE KEY UPDATE sur cette même clé
-- primaire composite (déjà géré côté Go via dbx.ActiveDialect(), comme
-- AuthRepository.SaveDevice).
--
-- Pas de FK vers merchant/users/permissions/roles à dessein : mêmes raisons
-- que le reste du schéma RBAC (types incompatibles avec les tables
-- historiques, voir migration 032) et parce que cette table doit continuer
-- d'encaisser des observations même si permission_key ne correspond
-- (temporairement) à rien de connu — elle observe la réalité du trafic, elle
-- ne la valide pas.
CREATE TABLE access_observation (
    merchant_id    varchar(64) NOT NULL,
    user_id        varchar(64) NOT NULL,
    permission_key varchar(64) NOT NULL,
    route          varchar(255) NOT NULL,
    granted        boolean NOT NULL,
    first_seen     timestamptz NOT NULL DEFAULT now(),
    last_seen      timestamptz NOT NULL DEFAULT now(),
    hits           bigint NOT NULL DEFAULT 1,
    PRIMARY KEY (merchant_id, user_id, permission_key, route)
);
