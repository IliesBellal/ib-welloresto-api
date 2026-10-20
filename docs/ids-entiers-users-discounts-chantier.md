# Chantier — identifiants entiers pour `users` et `discounts`

Statut : **cadré, décisions prises le 2026-10-20**. Aucune ligne de code ni migration écrite.

## 1. Objectif et choix retenus

- **Users** : `users.user_id` **reste un `varchar`**, mais tous les ids deviennent des entiers en texte (`"263"`).
  - Les nouveaux utilisateurs reçoivent leur id d'une séquence Postgres.
  - Les utilisateurs existants en `user-<uuid>` sont renumérotés.
  - Ensuite, un `CHECK` numérique et de vraies FK (`varchar` → `varchar`) sont posés.
  - On écarte un passage du type en `integer` : voir §3, C4.
- **Discounts** : on termine la bascule vers la clé entière entamée par la migration 118. Plus aucune remise n'est créée en `discount-<uuid>`, et le pricing, le Kiosk et ScanNOrder lisent `discount_id_new`.
- **Contrat API inchangé** : les ids restent des **chaînes** en JSON. Aucun client n'est à modifier pour le chantier.

## 2. Constat (staging, 2026-10-20)

Les volumes de staging ne sont pas représentatifs. Les **formats**, eux, sont parlants. Les chiffres de prod sont à relever avec la requête du §7.

### users

- `users.user_id` est un `varchar(64)`, sans séquence. Les utilisateurs créés par le PHP ont des ids numériques en texte (max `262`).
- Depuis le 2026-04-20 (`0159efa`), le Go génère `user-<uuid>` (`helpers.GeneratePrefixedID(helpers.UserIDPrefix)`), à 3 endroits : [users/create_service.go:46](../internal/modules/users/create_service.go#L46) et [signup/service.go:201](../internal/modules/signup/service.go#L201), [:252](../internal/modules/signup/service.go#L252).
- On compte 48 users : 43 numériques et 5 `user-<uuid>`.
- Bugs causés par le format uuid (41 caractères) :
  - `device_link.user_id` et `order_comments.user_id` sont en `varchar(20)` : l'INSERT échoue ;
  - `api_request_logs.user_id` est un `bigint` : la valeur reste NULL ([request_logger/middleware.go:70](../internal/middleware/request_logger/middleware.go#L70)) ;
  - Flutter fait `int.tryParse(userId)` : le livreur est ignoré dans le suivi de position ([delivery_controller.dart:271](../../wello_resto_flutter/lib/controllers/delivery/delivery_controller.dart#L271)).

Classement des colonnes qui contiennent un identifiant d'utilisateur. On en compte environ 45, dans environ 35 tables.

| Famille | Colonnes | Contenu observé |
|---|---|---|
| **Référence pure** | `users_rights`, `users_devices`, `users_nfc_tags`, `user_vacations`, `employees`, `employment_contract`, `services_performed`, `delivery_session`, `delivery_position`, `device_link`, `qrcodes`, `cash_registers.user_id/closed_by`, `cash_funds`, `cash_reports`, `cash_registers_custom_items.created_by`, `stock_movements`, `password_resets`, `planned_shifts`, `planning_*_requests.*_by_user_id`, `kiosk/cds_enrollment_codes.created_by_user_id`, `attestations.signed_by_user_id`, HACCP `created_by`, `menu_import_*`, `welloresto_stripe_customers.creator_user_id`, `access_observation`, `api_calls`, `notifications.notification_user_id`, `order_changes_log.changed_by_user_id` | Uniquement des ids d'utilisateur |
| **Acteur** (contient des sentinelles) | `orders.created_by`, `payments.user_id`, `order_comments.user_id`, `audit_logs.user_id`, `bookings.created_by` | En plus des ids : `UBER_EATS_WH`, `UBER_EATS_WB`, `DELIVEROO_WH`, `DELIVEROO_WB`, `WEBHOOK_UBER_EATS`, `WEBHOOK_DELIVEROO`, `WEBHOOK_STRIPE`, `SCANNORDER`, `KIOSK`, `WR_ONLINE_BOOKING`, `-1`. Environ 20 % des lignes de `orders` et `payments` |
| **Hors périmètre** | `planning_*.employee_id`, `employee_documents.employee_id`, `hours_amendments.employee_id` | Des ids `plan-emp-<uuid>` (employé de planning) |
| **Pas un id** | `merchant_closing_modes.created_by` (`migration-165`), `subscription_overrides.created_by` | Libellé libre |

Orphelins : 40 lignes de `users_rights` pointent vers un `user-<uuid>` absent de `users` (staging, à vérifier en prod).

### discounts

La migration 118 a fait l'expansion :
- `discounts.discount_id_new` est un integer avec séquence et `DEFAULT nextval`, `NOT NULL` et unique ;
- `legacy_discount_id` conserve l'ancien identifiant ;
- les tables filles ont leur `discount_id_new` avec une FK ;
- `discount_redemptions.discount_id` est un integer avec FK.

Ce qu'il reste à faire est listé au hotfix du 2026-10-19 (`docs/decisions.md`). Sur staging : 19 remises, dont 9 numériques et 10 `discount-<uuid>`. `orders.cart_discount_id` est encore un `varchar(64)`.

## 3. Contraintes dures

**C1 — Chaîne fiscale, active en prod depuis le 2026-10-11.** `payments.user_id` et `audit_logs.user_id` sont dans les charges utiles signées ([fiscal/payloads.go:35](../internal/fiscal/payloads.go#L35), [:88](../internal/fiscal/payloads.go#L88)). On ne réécrit **jamais** une ligne déjà scellée. La même règle vaut pour `orderitems.discount_id`.

**C2 — Colonnes acteur.** Leurs sentinelles ne tiennent pas dans un entier. Ces colonnes restent en `varchar` et ne reçoivent pas de FK. Un éventuel modèle d'acteur (`actor_type` + `user_id`) serait un chantier séparé.

**C3 — Contrat JSON des clients en circulation.** Côté Flutter, on trouve 14 déclarations `String userId` et 5 `int userId`. Côté back-office et ScanNOrder, une quarantaine de déclarations TS sont en `string`. Les ids restent donc des chaînes.

**C4 — Typage pgx**, testé le 2026-10-20 sur une table temporaire. pgx convertit dans les deux sens une `string` Go vers une colonne `integer`. Un passage en `integer` resterait donc possible plus tard, sans toucher au Go, une fois tous les ids numériques (`ALTER TYPE ... USING user_id::integer`). Ce passage n'est pas retenu aujourd'hui pour trois raisons :
- les jointures vers les colonnes acteur (environ 18) demanderaient un cast pour toujours, et un oubli ne casse qu'au runtime (`42883`) ;
- tout id non numérique résiduel produirait une erreur `22P02` ;
- `ALTER TYPE` réécrit une trentaine de tables sous verrou exclusif.

En échange, le type `integer` n'apporterait rien que le `CHECK` et les FK `varchar` n'apportent déjà.

## 4. Étapes

Règle générale : une migration **additive** (nouvelle séquence, table, colonne) est appliquée **avant** le déploiement du code qui l'utilise. Une migration qui **réécrit ou contraint** des données est appliquée **après** le déploiement du code qui la tolère. Chaque étape passe par staging, puis par la prod. Ilies applique les migrations et fait les push.

| # | Contenu | Migration | Ordre |
|---|---|---|---|
| **1** | Users : la séquence `users_user_id_seq` remplace `user-<uuid>` aux 3 points de création (`UsersRepository.NextUserID`) | `186_users_user_id_seq` (séquence + `setval` sur le maximum numérique, calculé à l'application) | 186 staging → push `staging` → test → 186 prod → push `main` |
| **2** | Discounts : `CreateDiscount` et l'import carte écrivent `discount_id = discount_id_new::text` ; le pricing (`GetDiscounts`, `GetDiscountProducts`, `GetDiscountProductOptions`), le Kiosk et ScanNOrder renvoient `discount_id_new` en chaîne ; traitement de `orders.cart_discount_id` et du cas `RewardID` | Aucune (la séquence de 118 existe) | push `staging` → test → push `main` |
| **3a** | Users : la table `user_id_remap(legacy_user_id, user_id)` est créée et remplie | `187_user_id_remap` (additive) | Après la reconnaissance prod (§7). 187 staging/prod |
| **3b** | Users : un filet à l'entrée traduit un `user-<uuid>` encore envoyé par un client via `user_id_remap` (sur le modèle de `resolveLegacyDiscountIDs`) ; l'affichage « encaissé par » des lignes scellées passe aussi par la table | Aucune | push `staging` → test → push `main` |
| **3c** | Users : renumérotation de `users` et des colonnes « référence pure », et des colonnes acteur **non scellées** | `188_users_renumber` | Après 3b. Créneau 3h-5h |
| **4** | Users : `CHECK (user_id ~ '^[0-9]+$')` sur `users`, nettoyage des orphelins, FK sur les colonnes « référence pure » (`NOT VALID` puis `VALIDATE`) | `189_users_fk` | Après 3c |
| **5** | Nettoyage : suppression de `helpers.UserIDPrefix` et `DiscountIDPrefix` ; le filet 3b et `resolveLegacyDiscountIDs` sont retirés après N semaines sans déclenchement ; la PRIMARY KEY de `discounts` bascule si on le souhaite | Selon le cas | En dernier |

Le moteur de promotions ([promotions-moteur-chantier.md](promotions-moteur-chantier.md)) démarre **après l'étape 2**, pour partir directement sur la clé entière.

## 5. Questions encore ouvertes

- **Orphelins** (`users_rights` vers un user inexistant, etc.) : supprimer ou mettre de côté ? À trancher sur les chiffres de prod, avant l'étape 4.
- **Contenu Redis** : une session met-elle en cache un `user_id` ? À vérifier avant l'étape 3c.

## 6. Migrations `todo/` → `done/` (2026-10-20)

Ilies a demandé de déplacer toutes les migrations 164 à 185 dans `done/`. Vérification sur staging le même jour, sans table de suivi (`schema_migrations` est absente sur staging) :
- **appliquées** : 164 à 177, 179, 181 ;
- **absentes** : `178_payout_documents`, `180_monthly_commission_invoices`, `182_diagnostic_events`, `183_customer_blocking`, `184_users_devices_app_version`, `185_delivery_hours`.

La branche `origin/staging` a 13 commits de retard sur `main`, ce qui explique ces absences. Ces 6 migrations sont à appliquer sur staging **avant** de pousser `main` sur `staging`. La prod n'a pas pu être vérifiée d'ici.

## 7. Reconnaissance prod (lecture seule)

```sql
-- Formats d'ids users
SELECT CASE WHEN user_id ~ '^[0-9]+$' THEN 'numeric'
            WHEN user_id LIKE 'user-%' THEN 'user-uuid' ELSE 'other' END AS fmt,
       count(*), max(CASE WHEN user_id ~ '^[0-9]+$' THEN user_id::bigint END) AS max_num
FROM users GROUP BY 1;

-- Formats d'ids discounts
SELECT CASE WHEN discount_id ~ '^[0-9]+$' THEN 'numeric'
            WHEN discount_id LIKE 'discount-%' THEN 'discount-uuid' ELSE 'other' END AS fmt,
       count(*)
FROM discounts GROUP BY 1;

-- Lignes scellées portant un user-<uuid> (C1)
SELECT 'payments' AS t, count(*) FROM payments WHERE user_id LIKE 'user-%' AND signature IS NOT NULL
UNION ALL
SELECT 'audit_logs', count(*) FROM audit_logs WHERE user_id LIKE 'user-%' AND signature IS NOT NULL;

-- Orphelins users_rights
SELECT count(*) FROM users_rights ur WHERE NOT EXISTS (SELECT 1 FROM users u WHERE u.user_id = ur.user_id);

-- Taille des tables à renuméroter
SELECT relname, n_live_tup FROM pg_stat_user_tables
WHERE relname IN ('stock_movements','delivery_position','users_rights','cash_registers','services_performed','users_devices','orders','order_comments')
ORDER BY n_live_tup DESC;
```

Le nom exact de la colonne de signature de `payments` est à confirmer avant de lancer la requête.
