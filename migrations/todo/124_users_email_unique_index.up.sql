-- LOT A Semaine 1, Chantier 3 (docs/decisions.md) : unicité de l'adresse
-- électronique des utilisateurs, prérequis au self-onboarding (POST /v1/signup).
-- Aujourd'hui rien n'empêche deux comptes de partager le même e-mail (aucune
-- contrainte ni en base ni en code) ; CreateUser n'interroge jamais la table
-- avant d'insérer.
--
-- ATTENTION - la dernière instruction de ce fichier (CREATE UNIQUE INDEX
-- CONCURRENTLY) doit être jouée hors bloc transactionnel, comme les autres
-- CREATE INDEX CONCURRENTLY de ce dépôt (087_analytics_indexes,
-- 109_api_request_logs_created_at_index, 118_discounts_integer_key_expansion) :
-- instruction par instruction, jamais dans un BEGIN...COMMIT englobant.
--
-- Détection de doublons exécutée avant d'écrire cette migration : aucun en
-- production (vérifié). Sur staging : 1 groupe de 4 comptes de test
-- désactivés (enabled = false, créés à la même seconde le 2026-05-30,
-- user_id 227/233/242/244) partageant "iliesbellaltemp@gmail.com" — pas de
-- doublon parmi des comptes actifs. La section 2 ci-dessous neutralise ce
-- cas (et tout cas équivalent, y compris futur) plutôt que de nécessiter un
-- nettoyage manuel séparé sur un environnement partagé : elle ne modifie
-- rien quand il n'y a aucun doublon (donc no-op en production).

-- ============================================================================
-- 1. Normalisation : espaces superflus et casse
-- ============================================================================
UPDATE users SET email = lower(trim(email));

-- ============================================================================
-- 2. Désambiguïsation des doublons restants (le cas échéant)
-- ============================================================================
-- Pour un e-mail partagé par plusieurs comptes, le compte le plus ancien
-- (user_id le plus petit) garde l'adresse telle quelle ; les suivants
-- reçoivent un suffixe +1, +2, ... sur la partie locale (ex.
-- iliesbellaltemp@gmail.com, iliesbellaltemp+1@gmail.com,
-- iliesbellaltemp+2@gmail.com, ...), qui reste une adresse syntaxiquement
-- valide. Aucune ligne touchée si tous les e-mails sont déjà distincts.
WITH ranked AS (
    SELECT user_id,
           email,
           ROW_NUMBER() OVER (PARTITION BY email ORDER BY user_id) AS rn
    FROM users
)
UPDATE users u
SET email = regexp_replace(u.email, '@', '+' || (r.rn - 1) || '@')
FROM ranked r
WHERE u.user_id = r.user_id
  AND r.rn > 1;

-- ============================================================================
-- 3. users.name suit désormais l'e-mail (préparation retrait colonne, lot C)
-- ============================================================================
-- À ce stade email est normalisé ET unique (étapes 1-2) : aucun risque que
-- cette étape recrée une collision sur uq_users_name (index déjà existant).
UPDATE users SET name = lower(trim(email)) WHERE name IS DISTINCT FROM lower(trim(email));

-- ============================================================================
-- 4. Contrainte d'unicité
-- ============================================================================
-- Hors bloc transactionnel (voir ATTENTION en tête de fichier).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_users_email_lower ON users (lower(email));
