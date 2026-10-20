-- Horaires de livraison ScanNOrder (docs/DELIVERY_HOURS.md).
--
-- Restreint les commandes ScanNOrder DELIVERY à des créneaux plus courts que
-- les horaires d'ouverture (ex. ouvert 18h-minuit, livraison 19h-22h30 ; pas
-- de livraison le midi sauf le mardi). Aucune ligne pour un établissement =
-- aucune restriction : la livraison suit les horaires d'ouverture, comme avant.
--
-- Lue uniquement par le module scannorder. Le POS, la borne, Uber Eats et
-- Deliveroo l'ignorent volontairement : ne pas la joindre à hours_of_operation.
--
--   day_of_week  1 = lundi ... 7 = dimanche (ISO, comme hours_of_operation).
--   hour_from    début du créneau (inclus).
--   hour_to      fin du créneau (exclue). hour_to <= hour_from = le créneau
--                passe minuit et se termine le lendemain (ex. 19:00 -> 00:00,
--                22:00 -> 01:00).
--
-- Le back-office remplace toute la semaine d'un coup (PUT
-- /pos/settings/delivery_hours) : pas d'id fonctionnel, pas de soft delete.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. CreateOrderSNO lit la table
-- pour chaque commande DELIVERY ; sans elle, ces commandes échoueraient.

CREATE TABLE IF NOT EXISTS delivery_hours (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    merchant_id  varchar(64) NOT NULL,
    day_of_week  smallint    NOT NULL CHECK (day_of_week BETWEEN 1 AND 7),
    hour_from    time        NOT NULL,
    hour_to      time        NOT NULL CHECK (hour_to <> hour_from),
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_delivery_hours_merchant
    ON delivery_hours (merchant_id);
