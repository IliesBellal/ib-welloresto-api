-- Identifiants utilisateurs numériques (docs/ids-entiers-users-discounts-chantier.md, étape 1).
--
-- users.user_id reste un varchar (décision du 2026-10-20) mais n'est plus
-- rempli par "user-<uuid>" (helpers.GeneratePrefixedID, depuis 2026-04) :
-- UsersRepository.NextUserID tire la valeur de cette séquence, écrite en texte
-- ("263"), comme les identifiants historiques du PHP.
--
-- Point de départ calculé à l'application, pas en dur : la plus grande valeur
-- numérique déjà en circulation, côté users ET côté colonnes qui la
-- référencent (orphelines comprises — un utilisateur supprimé peut avoir
-- laissé des paiements ou des mouvements de stock à son id). Un nouvel
-- utilisateur ne doit jamais hériter de l'historique d'un ancien. Même règle
-- que discounts_discount_id_new_seq (migration 118).
--
-- La renumérotation des "user-<uuid>" existants (migration 188) tire aussi
-- ses valeurs de cette séquence.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. CreateUser et le signup
-- appellent nextval('users_user_id_seq') ; sans la séquence, toute création
-- d'utilisateur échoue.
--
-- Rejouable : CREATE ... IF NOT EXISTS, et setval ne redescend jamais sous
-- une valeur déjà attribuée (le maximum est recalculé à chaque passage).

CREATE SEQUENCE IF NOT EXISTS users_user_id_seq AS bigint;

-- ^[0-9]{1,18}$ : exclut les sentinelles ("-1", "KIOSK", ...) et toute valeur
-- qui dépasserait bigint.
SELECT setval('users_user_id_seq', GREATEST(
    COALESCE((SELECT MAX(user_id::bigint)    FROM users              WHERE user_id    ~ '^[0-9]{1,18}$'), 0),
    COALESCE((SELECT MAX(user_id::bigint)    FROM users_rights       WHERE user_id    ~ '^[0-9]{1,18}$'), 0),
    COALESCE((SELECT MAX(user_id::bigint)    FROM payments           WHERE user_id    ~ '^[0-9]{1,18}$'), 0),
    COALESCE((SELECT MAX(created_by::bigint) FROM orders             WHERE created_by ~ '^[0-9]{1,18}$'), 0),
    COALESCE((SELECT MAX(user_id::bigint)    FROM cash_registers     WHERE user_id    ~ '^[0-9]{1,18}$'), 0),
    COALESCE((SELECT MAX(user_id::bigint)    FROM stock_movements    WHERE user_id    ~ '^[0-9]{1,18}$'), 0),
    COALESCE((SELECT MAX(user_id::bigint)    FROM audit_logs         WHERE user_id    ~ '^[0-9]{1,18}$'), 0),
    COALESCE((SELECT MAX(user_id::bigint)    FROM services_performed WHERE user_id    ~ '^[0-9]{1,18}$'), 0),
    COALESCE((SELECT MAX(user_id::bigint)    FROM delivery_session   WHERE user_id    ~ '^[0-9]{1,18}$'), 0)
) + 1, false);
