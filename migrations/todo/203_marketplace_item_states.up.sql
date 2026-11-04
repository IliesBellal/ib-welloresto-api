-- État de disponibilité envoyé à une plateforme de livraison, article par article.
--
-- Pourquoi : Uber Eats ne permet pas de relire l'état d'un article (la lecture du
-- menu n'est pas exploitable pour retrouver les suspensions). Pour n'envoyer que
-- la différence entre WelloResto et Uber Eats, on mémorise ce qu'on lui a
-- envoyé : à chaque publication de menu (toutes les suspensions du document) et
-- à chaque mise à jour d'article réussie.
--
-- state : 'available' | 'sold_out'. Un produit masqué (not_available) n'est pas
-- dans le menu publié : il n'a pas de ligne.
--
-- Deliveroo n'utilise pas cette table (il relit l'état chez Deliveroo) ; la
-- colonne platform la garde utilisable par une future plateforme.
--
-- Documentation : docs/ubereats-menu-sync.md.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Rejouable.

CREATE TABLE IF NOT EXISTS marketplace_item_states (
    platform    varchar(20)  NOT NULL,
    merchant_id varchar(20)  NOT NULL,
    location_id varchar(150) NOT NULL,
    item_id     varchar(100) NOT NULL,
    state       varchar(20)  NOT NULL,
    updated_at  timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (platform, merchant_id, location_id, item_id)
);

COMMENT ON TABLE marketplace_item_states IS
    'Dernier état de disponibilité envoyé à la plateforme pour chaque article publié (docs/ubereats-menu-sync.md).';
