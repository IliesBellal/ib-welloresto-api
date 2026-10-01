-- Migration server-driven Stripe Terminal (docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md,
-- voir docs/KIOSK_DECISIONS.md) : appairage lecteur par borne (pas par
-- merchant) + colonnes stripe_payments nécessaires au dispatch server-driven
-- (kiosk_id, détails carte affichables côté back-office/reçu).

-- Un nouvel enrollment/reclaim n'a jamais ces colonnes renseignées :
-- appairage manuel obligatoire (décision actée).
ALTER TABLE kiosks
    ADD COLUMN IF NOT EXISTS stripe_reader_id varchar(255),
    ADD COLUMN IF NOT EXISTS stripe_reader_label varchar(255),
    ADD COLUMN IF NOT EXISTS stripe_reader_serial varchar(255);

-- Un reader physique ne doit jamais être appairé à deux kiosks simultanément
-- (la résolution "quel kiosk notifier" côté webhook passe par
-- stripe_payments.kiosk_id, pas par un lookup reader->kiosk, mais
-- l'ambiguïté d'appairage resterait un bug en soi). Index partiel : ne
-- contraint que les lignes réellement appairées.
CREATE UNIQUE INDEX IF NOT EXISTS uq_kiosks_stripe_reader_id
    ON kiosks (stripe_reader_id) WHERE stripe_reader_id IS NOT NULL;

-- kiosk_id : posé au moment du dispatch (POST /kiosk/terminal/payment),
-- même type que orders.kiosk_id (migration 038). Détails carte : posés à
-- payment_intent.succeeded (relecture PI avec expand latest_charge). Pas de
-- FK, cohérent avec le reste de cette table (aucune FK déclarée dessus).
ALTER TABLE stripe_payments
    ADD COLUMN IF NOT EXISTS kiosk_id varchar(64),
    ADD COLUMN IF NOT EXISTS card_brand varchar(30),
    ADD COLUMN IF NOT EXISTS card_last4 varchar(4),
    ADD COLUMN IF NOT EXISTS card_application_preferred_name varchar(100),
    ADD COLUMN IF NOT EXISTS card_dedicated_file_name varchar(100),
    ADD COLUMN IF NOT EXISTS card_authorization_code varchar(50);
