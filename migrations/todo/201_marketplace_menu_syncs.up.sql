-- Journal des publications de menu vers les plateformes de livraison
-- (Deliveroo aujourd'hui, Uber Eats ensuite : colonne platform).
--
-- Une ligne par tentative de publication d'un compte (merchant_id +
-- location_id = site Deliveroo / store Uber). Elle sert à :
--   - montrer au restaurateur l'état de sa synchro (en cours, réussie, échouée
--     et pourquoi, images refusées) ;
--   - retrouver, à l'arrivée du webhook de résultat, la publication concernée ;
--   - ne pas republier un menu identique au dernier publié (fingerprint) ;
--   - n'aligner les disponibilités que sur les articles réellement publiés
--     (item_ids : un identifiant inconnu fait échouer tout l'envoi Deliveroo).
--
-- origin : manual (bouton du back-office), scenario_13 / scenario_15
-- (certification Deliveroo), auto (synchro automatique, à venir).
--
-- status :
--   sending     envoi en cours
--   processing  accepté par la plateforme, résultat attendu (webhook / job)
--   success     menu en ligne
--   failed      refusé ou en erreur (error, upload_errors)
--   unchanged   identique au dernier menu publié, rien envoyé
--
-- availability_status : alignement des disponibilités après publication
--   (NULL = pas encore fait, 'success', 'failed' + availability_error).
--
-- Documentation : docs/deliveroo-menu-sync.md.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Rejouable.

CREATE TABLE IF NOT EXISTS marketplace_menu_syncs (
    id                      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    platform                varchar(20)  NOT NULL,
    merchant_id             varchar(20)  NOT NULL,
    location_id             varchar(50)  NOT NULL,
    brand_id                varchar(100) NULL,
    menu_id                 varchar(100) NOT NULL,
    origin                  varchar(30)  NOT NULL,
    api_version             varchar(5)   NOT NULL,
    status                  varchar(20)  NOT NULL,
    fingerprint             varchar(64)  NULL,
    item_count              integer      NOT NULL DEFAULT 0,
    item_ids                jsonb        NULL,
    job_id                  varchar(100) NULL,
    http_status             integer      NULL,
    error                   text         NULL,
    warnings                jsonb        NULL,
    upload_errors           jsonb        NULL,
    availability_status     varchar(20)  NULL,
    availability_error      text         NULL,
    created_at              timestamptz  NOT NULL DEFAULT now(),
    sent_at                 timestamptz  NULL,
    finished_at             timestamptz  NULL,
    availability_applied_at timestamptz  NULL
);

CREATE INDEX IF NOT EXISTS idx_marketplace_menu_syncs_account
    ON marketplace_menu_syncs (platform, merchant_id, location_id, id DESC);

CREATE INDEX IF NOT EXISTS idx_marketplace_menu_syncs_pending
    ON marketplace_menu_syncs (platform, location_id, menu_id, id DESC)
    WHERE status IN ('sending', 'processing');

COMMENT ON TABLE marketplace_menu_syncs IS
    'Journal des publications de menu vers Deliveroo / Uber Eats (docs/deliveroo-menu-sync.md).';
