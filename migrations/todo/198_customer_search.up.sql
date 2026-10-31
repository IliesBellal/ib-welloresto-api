-- Recherche de clients du POS (docs/recherche-clients.md).
--
-- internal/modules/customers/search.go cherche par mots (prénom/nom, email,
-- téléphone) avec des LIKE '%mot%', insensibles à la casse ET aux accents.
-- Cette migration fournit :
--   * les extensions pg_trgm (index de sous-chaînes) et unaccent ;
--   * trois fonctions IMMUTABLE qui normalisent texte, nom et téléphone ;
--   * trois index GIN trigramme PARTIELS (clients visibles hors places de
--     marché), préfixés par merchant_id (btree_gin) : chaque recherche ne
--     touche que les clients du marchand authentifié.
--
-- ATTENTION - ORDRE DE DÉPLOIEMENT : cette migration doit être appliquée AVANT
-- le déploiement du code. Le code appelle customer_search_norm / _name / _phone ;
-- sans elles toute recherche de client échoue (« function ... does not exist »).
--
-- ATTENTION - CREATE INDEX CONCURRENTLY doit être joué hors bloc transactionnel
-- (comme les autres index CONCURRENTLY de ce dépôt), instruction par instruction.
-- Si l'un d'eux échoue il laisse un index INVALIDE : le supprimer (DROP INDEX
-- CONCURRENTLY) puis relancer. CREATE EXTENSION requiert un rôle autorisé ;
-- pg_trgm, unaccent et btree_gin sont des extensions « de confiance » (le
-- propriétaire de la base peut les créer).
--
-- Les prédicats WHERE des index reprennent À L'IDENTIQUE customerVisibleFilter
-- (internal/modules/customers/repository.go) : sinon PostgreSQL ne les utilise pas.

CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS unaccent;
CREATE EXTENSION IF NOT EXISTS btree_gin;

-- unaccent() est STABLE : inutilisable dans un index. Cette enveloppe IMMUTABLE
-- (patron documenté de la communauté) fige le dictionnaire. Tout est qualifié
-- (public.) : lors des maintenances (ANALYZE, autovacuum), PostgreSQL exécute
-- les fonctions d'index avec un search_path vide.
-- Minuscules, sans accents, tirets et apostrophes comptés comme des espaces
-- ("Jean-Pierre" -> "jean pierre", "O'Brien" -> "o brien").
CREATE OR REPLACE FUNCTION customer_search_norm(txt text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT AS
$$ SELECT pg_catalog.lower(public.unaccent('public.unaccent'::pg_catalog.regdictionary,
                                           pg_catalog.translate(txt, E'-\'’', '   '))) $$;

-- Texte cherché pour un client : prénom, nom et nom d'affichage normalisés.
CREATE OR REPLACE FUNCTION customer_search_name(p_first text, p_last text, p_name text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE AS
$$ SELECT public.customer_search_norm(coalesce(p_first, '') || ' ' ||
                                      coalesce(p_last, '') || ' ' ||
                                      coalesce(p_name, '')) $$;

-- Téléphone réduit à ses chiffres : "+33 6 12 34 56 78" et "06.12.34.56.78"
-- sont comparables, quel que soit le format historique.
CREATE OR REPLACE FUNCTION customer_search_phone(tel text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT AS
$$ SELECT pg_catalog.regexp_replace(tel, '[^0-9]', '', 'g') $$;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_customer_search_name
    ON customer USING gin (merchant_id, customer_search_name(customer_first_name, customer_last_name, customer_name) gin_trgm_ops)
    WHERE (enabled = true OR blocked_at IS NOT NULL) AND customer_brand NOT IN ('UBER_EATS', 'DELIVEROO');

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_customer_search_phone
    ON customer USING gin (merchant_id, customer_search_phone(customer_tel) gin_trgm_ops)
    WHERE (enabled = true OR blocked_at IS NOT NULL) AND customer_brand NOT IN ('UBER_EATS', 'DELIVEROO');

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_customer_search_email
    ON customer USING gin (merchant_id, lower(customer_email) gin_trgm_ops)
    WHERE (enabled = true OR blocked_at IS NOT NULL) AND customer_brand NOT IN ('UBER_EATS', 'DELIVEROO');
