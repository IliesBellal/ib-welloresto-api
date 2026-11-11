-- Suites (envoi des plats en plusieurs temps : entrées, puis plats, puis
-- desserts). Voir docs/order-lifecycle.md, section « Suites ».
--
-- productcateg.course_step : étape de la catégorie (1 = entrées, 2 = plats…).
-- NULL = pas d'étape : les produits de la catégorie partent tout de suite
-- (boissons). Un établissement sans aucune étape ne voit jamais de suite.
--
-- orderitems.course : numéro de la suite de la ligne dans SA commande
-- (1, 2, 3… attribués par la caisse ou par Scan&Order à partir des étapes
-- présentes dans la commande). NULL = hors suite, part tout de suite.
--
-- order_courses : une ligne par suite d'une commande.
--   delay_minutes : NULL = envoi manuel (« Réclamer »), sinon envoi
--                   automatique N minutes après l'envoi de la suite précédente.
--   fire_at       : heure d'envoi programmée (délai, ou les 5 s d'annulation
--                   après « Réclamer ») ; la tâche FireDueOrderCourses l'exécute.
--   fired_at      : heure d'envoi réelle. NULL = suite en attente.
-- Les suites partent toujours dans l'ordre : envoyer la suite N envoie les
-- suites < N encore en attente.
--
-- merchant_parameters.sno_course_delay_minutes : délai d'envoi des suites
-- suivantes pour les commandes Scan&Order sur place. 0 = manuel.
--
-- ORDRE DE DÉPLOIEMENT : appliquer cette migration AVANT de déployer le code
-- (le chargement des commandes lit oi.course et order_courses, le menu lit
-- pc.course_step).
--
-- ADD COLUMN sans défaut ou avec défaut constant : pas de réécriture de table.

ALTER TABLE productcateg
    ADD COLUMN IF NOT EXISTS course_step smallint NULL;

ALTER TABLE productcateg
    DROP CONSTRAINT IF EXISTS chk_productcateg_course_step;
ALTER TABLE productcateg
    ADD CONSTRAINT chk_productcateg_course_step
    CHECK (course_step IS NULL OR course_step BETWEEN 1 AND 20);

COMMENT ON COLUMN productcateg.course_step IS 'Étape de service de la catégorie (1 = entrées, 2 = plats…), sert à répartir les produits en suites. NULL = part tout de suite.';

ALTER TABLE orderitems
    ADD COLUMN IF NOT EXISTS course smallint NULL;

COMMENT ON COLUMN orderitems.course IS 'Numéro de la suite de la ligne dans sa commande (1, 2, 3…). NULL = hors suite, part tout de suite. État de la suite : order_courses.';

CREATE TABLE IF NOT EXISTS order_courses (
    order_id      varchar(64)  NOT NULL,
    merchant_id   varchar(64)  NOT NULL,
    course        smallint     NOT NULL CHECK (course > 0),
    delay_minutes integer      NULL CHECK (delay_minutes IS NULL OR delay_minutes > 0),
    fire_at       timestamptz  NULL,
    fired_at      timestamptz  NULL,
    fired_by      varchar(64)  NULL,
    created_at    timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (order_id, course)
);

-- Tâche FireDueOrderCourses (toutes les 5 s) : seules les suites en attente
-- avec un envoi programmé sont parcourues.
CREATE INDEX IF NOT EXISTS idx_order_courses_due
    ON order_courses (fire_at)
    WHERE fired_at IS NULL AND fire_at IS NOT NULL;

COMMENT ON TABLE order_courses IS 'Suites d''une commande : mode d''envoi (manuel ou délai), envoi programmé et envoi réel.';
COMMENT ON COLUMN order_courses.delay_minutes IS 'NULL = envoi manuel ; sinon envoi automatique N minutes après l''envoi de la suite précédente.';
COMMENT ON COLUMN order_courses.fire_at IS 'Envoi programmé (délai ou fenêtre d''annulation de « Réclamer »), exécuté par la tâche FireDueOrderCourses.';
COMMENT ON COLUMN order_courses.fired_at IS 'Envoi réel en cuisine. NULL = suite en attente.';

ALTER TABLE merchant_parameters
    ADD COLUMN IF NOT EXISTS sno_course_delay_minutes integer NOT NULL DEFAULT 0;

COMMENT ON COLUMN merchant_parameters.sno_course_delay_minutes IS 'Scan&Order sur place : délai d''envoi automatique des suites suivantes, en minutes. 0 = envoi manuel depuis la caisse.';
