-- Corps de la réponse HTTP dans api_request_logs, en complément du corps de
-- la requête déjà stocké (colonne payload) : permet de déboguer un échange
-- complet sans avoir à le reproduire.
--
-- Pas de troncature : le corps de la réponse est stocké intégralement quand
-- il s'agit de JSON. L'exclusion binaire suit le même mécanisme déjà en place
-- pour payload (voir requestlogger.jsonOrSizeMarker) : un corps non-JSON
-- (export de fichier, image, PDF...) n'est jamais inséré tel quel, seule sa
-- taille est enregistrée sous la forme {"non_json_body_bytes":N} — la colonne
-- cible est jsonb, un corps non-JSON ferait échouer tout le batch d'insertion.
--
-- Nullable et sans valeur par défaut, même raisonnement que duration_ms
-- (migration 088) : les lignes déjà présentes n'ont pas de réponse connue,
-- nullable évite la réécriture complète de la table à l'ajout.
--
-- Absence de retenue de taille : cette colonne va accroître le volume de
-- api_request_logs (déjà la plus grosse table de la base, cf. migration 088).
-- Voir migration 109 pour l'index sur created_at et internal/tasks/request_logs.go
-- pour la purge mensuelle qui compense cette croissance.

ALTER TABLE api_request_logs
    ADD COLUMN response_payload jsonb;

COMMENT ON COLUMN api_request_logs.response_payload IS
    'Corps de la réponse HTTP (JSON) associée à la requête, ou {"non_json_body_bytes":N} si le corps n''est pas du JSON valide (export, fichier...). NULL pour les lignes antérieures à cette migration.';
