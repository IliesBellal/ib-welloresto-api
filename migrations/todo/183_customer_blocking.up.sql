-- Blocage de clients pour la commande en ligne ScanNOrder (docs/CUSTOMER_BLOCKING.md).
--
-- Un restaurateur peut bloquer un client de son établissement : ses commandes
-- TAKE_AWAY / DELIVERY en ligne sont refusées sans explication (statut
-- générique côté API). Le blocage est porté par la fiche client, par
-- établissement (customer.merchant_id).
--
--   blocked_at         date du blocage ; NULL = client non bloqué. C'est la seule
--                      colonne lue par le contrôle de commande.
--   blocked_by         user_id (texte : les identifiants utilisateur ne sont pas
--                      tous numériques) de celui qui a bloqué.
--   blocked_reason_id  deletion_reasons.deletion_reason_id (object 'customer'),
--                      stocké en texte comme delivery_session_order.deletion_reason_id.
--                      Pas de FK (convention du repo).
--   blocked_comment    précision libre, obligatoire quand la raison l'exige.
--
-- Les motifs vivent dans deletion_reasons (object 'customer') avec leur libellé
-- FR dans labels (label_type 'deletion_reason'), comme les motifs d'annulation
-- de commande : GET /pos/deletion_reasons/customer les expose sans code. Les
-- IDs sont GENERATED ALWAYS : les libellés sont donc rattachés aux IDs générés,
-- et tout est idempotent (rejouable sans doublon).
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Le contrôle de commande lit
-- customer.blocked_at ; sans la colonne, chaque commande ScanNOrder TAKE_AWAY /
-- DELIVERY échouerait.

ALTER TABLE customer
    ADD COLUMN IF NOT EXISTS blocked_at        timestamptz NULL,
    ADD COLUMN IF NOT EXISTS blocked_by        varchar(64) NULL,
    ADD COLUMN IF NOT EXISTS blocked_reason_id varchar(20) NULL,
    ADD COLUMN IF NOT EXISTS blocked_comment   varchar(255) NULL;

-- Index partiel : la table customer est grosse, les clients bloqués sont une
-- poignée. Le contrôle de commande cherche par (établissement, téléphone)
-- uniquement parmi les bloqués. Pas d'index sur l'email : la commande ScanNOrder
-- ne transporte pas d'email client (models.CustomerRequest), l'email ne sert
-- qu'à retrouver les doublons au moment de bloquer.
CREATE INDEX IF NOT EXISTS idx_customer_blocked_tel
    ON customer (merchant_id, customer_tel)
    WHERE blocked_at IS NOT NULL;

-- Motifs de blocage. deletion_reason_desc porte le libellé français (il est
-- aussi recopié dans labels) et sert de clé d'idempotence avec l'object.
INSERT INTO deletion_reasons (deletion_reason_type, deletion_reason_object, deletion_reason_desc, requires_comment)
SELECT 'manual', 'customer', v.reason_desc, v.requires_comment
FROM (VALUES
    ('Commande non retirée',                   false),
    ('Fausse commande / canular',              false),
    ('Comportement abusif envers le personnel', false),
    ('Impayé / litige de paiement',            false),
    ('Fraude suspectée',                       false),
    ('Autre',                                  true)
) AS v(reason_desc, requires_comment)
WHERE NOT EXISTS (
    SELECT 1 FROM deletion_reasons dr
    WHERE dr.deletion_reason_object = 'customer'
      AND dr.deletion_reason_desc = v.reason_desc
);

-- Libellés FR, rattachés aux IDs générés ci-dessus.
INSERT INTO labels (label_value, label_type, lang, label)
SELECT CAST(dr.deletion_reason_id AS varchar(20)), 'deletion_reason', 'FR', dr.deletion_reason_desc
FROM deletion_reasons dr
WHERE dr.deletion_reason_object = 'customer'
  AND NOT EXISTS (
      SELECT 1 FROM labels l
      WHERE l.label_type = 'deletion_reason'
        AND l.lang = 'FR'
        AND l.label_value = CAST(dr.deletion_reason_id AS varchar(20))
  );
