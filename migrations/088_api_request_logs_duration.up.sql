-- Latence des requetes HTTP dans api_request_logs.
--
-- Aujourd'hui aucune duree n'est enregistree nulle part : ni en base, ni dans
-- les logs zap (le seul zap.Duration existant mesure le flush du logger, pas la
-- requete). Toute discussion de performance repose donc sur des mesures faites a
-- la main sur staging, dont la representativite est douteuse : l'instance
-- PostgreSQL de staging est fortement bridee (10 M d'iterations generate_series
-- en 16,3 s, soit environ 15x plus lent qu'un coeur non bride) et sa variance
-- depasse 40 % d'une execution a l'autre. Sans mesure cote production, aucun
-- arbitrage d'architecture n'est defendable.
--
-- Colonne nullable et sans valeur par defaut : les lignes deja presentes
-- (207 218 sur staging) n'ont pas de duree connue et il serait faux de leur en
-- inventer une. Le middleware alimente la colonne pour les nouvelles lignes.
-- Nullable evite aussi la reecriture complete de la table a l'ajout.
--
-- integer et non bigint : une requete HTTP depassant 24 jours n'existe pas.

ALTER TABLE api_request_logs
    ADD COLUMN duration_ms integer;

COMMENT ON COLUMN api_request_logs.duration_ms IS
    'Duree de traitement de la requete HTTP en millisecondes, mesuree par requestlogger. NULL pour les lignes anterieures a la migration 088.';


-- ---------------------------------------------------------------------------
-- pg_stat_statements : NON ACTIVE ICI, ACTION MANUELLE REQUISE
-- ---------------------------------------------------------------------------
-- L'extension est DISPONIBLE sur l'instance (pg_available_extensions : version
-- 1.12) mais NON INSTALLEE. Elle ne peut pas etre activee par cette migration :
--
--   1. le role applicatif welloresto_api n'est pas superuser
--      (is_superuser = off, rolsuper = false) ;
--   2. pg_stat_statements exige d'etre charge via shared_preload_libraries,
--      qui est un parametre au demarrage du serveur. Ce parametre n'est meme
--      pas lisible avec nos droits ("permission denied to examine
--      shared_preload_libraries") ;
--   3. sur Render, shared_preload_libraries se regle depuis le tableau de bord
--      de l'instance, pas en SQL, et demande un redemarrage.
--
-- Marche a suivre, dans cet ordre :
--   a. tableau de bord Render > instance PostgreSQL > ajouter
--      pg_stat_statements a shared_preload_libraries, puis redemarrer ;
--   b. une fois l'instance redemarree, executer avec un role habilite :
--        CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
--   c. verifier : SELECT count(*) FROM pg_stat_statements;
--
-- Tant que (a) n'est pas fait, (b) echoue avec "pg_stat_statements must be
-- loaded via shared_preload_libraries". Ne pas contourner en instrumentant a la
-- main : la mesure cote base est le seul moyen d'attribuer un temps a une
-- requete plutot qu'a un endpoint.


-- ---------------------------------------------------------------------------
-- PROPOSITION DE RETENTION - VOLONTAIREMENT NON APPLIQUEE
-- ---------------------------------------------------------------------------
-- api_request_logs (207 218 lignes, 51 MB) et audit_logs (10 500 lignes, 56 MB)
-- sont les DEUX PLUS GROSSES TABLES DE LA BASE, devant toutes les tables de
-- faits reunies (orders 10 MB, orderitems 11 MB). Aucune purge n'a jamais eu
-- lieu : aucun DELETE sur ces tables n'existe dans le code.
--
-- Rien n'est applique ici parce que la duree de conservation d'audit_logs est
-- une question juridique, pas technique : ces lignes tracent des actions
-- utilisateur et peuvent servir de preuve. Trancher avant d'ecrire quoi que ce
-- soit.
--
-- Proposition a arbitrer :
--
--   * api_request_logs : purement technique (debug d'integration), aucune
--     valeur probante. 30 jours suffisent. Gain estime : environ 90 % des
--     lignes, soit environ 45 MB.
--
--   * audit_logs : NE PAS PURGER SANS EXPERTISE PREALABLE. La table porte un
--     CHAINAGE DE HASH (colonnes previous_hash et hash, hash NOT NULL, meme
--     dispositif que orders, payments, receipts et cash_registers). Supprimer
--     une ligne au milieu de la chaine casse la continuite et rend TOUTE la
--     chaine restante invalide a la verification. Une purge n'a de sens que par
--     troncature du plus ancien avec re-ancrage documente de la chaine, ou pas
--     du tout. A 10 500 lignes la table ne presse pas : ses 56 MB viennent du
--     volume des colonnes jsonb old_values / new_values, pas du nombre de
--     lignes. Compresser ou externaliser ces deux colonnes serait plus efficace
--     et sans risque juridique.
--
-- Implementation suggeree pour api_request_logs UNIQUEMENT, en tache cron
-- nocturne (cf. cmd/api/tasks.go, ou le pattern "fenetre glissante" de
-- UpdatePopularProducts est deja en place), par lots bornes pour ne pas tenir
-- un verrou long :
--
--   DELETE FROM api_request_logs
--   WHERE id IN (SELECT id FROM api_request_logs
--                WHERE created_at < now() - interval '30 days'
--                LIMIT 5000);
--
-- repete jusqu'a 0 ligne supprimee. Colonne de date confirmee :
-- api_request_logs.created_at (timestamptz). Prevoir d'abord un index sur
-- created_at : la table n'en a aucun aujourd'hui hors cle primaire, chaque
-- passe ferait sinon un parcours complet de 207 218 lignes.
