# Chantier — identifiants entiers pour `users` et `discounts`

Statut : **étapes 1 à 3 codées le 2026-10-20**, non commitées, migrations 186 et 187 à appliquer. Étape 4 reportée (voir §8). Détail de l'implémentation : §8.

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
| **3a** | Users : la table `user_id_remap(legacy_user_id, user_id)` est créée **vide** | `187_user_id_remap` (additive) | 187 staging/prod, avant le code |
| **3b** | Users : un filet à l'entrée (`userids.Resolve`) traduit un `user-<uuid>` encore envoyé par un client ; le détail de caisse et la liste des paiements d'une commande joignent `user_id_remap` pour les paiements signés | Aucune | push `staging` → test → push `main` |
| **3c** | Users : renumérotation de `users`, des colonnes « référence pure » et des colonnes acteur, **sauf les lignes signées** de `payments` et `audit_logs`, puis vidage du cache Redis des sessions | Aucune : commande `cmd/renumber_users` (aperçu, puis `--apply`) | Après 3b. Créneau 3h-5h |
| **4** | Users : `CHECK (user_id ~ '^[0-9]+$')` sur `users`, nettoyage des orphelins, FK sur les colonnes « référence pure » | `188_users_fk` | **Reportée** (§8) |
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

## 8. Implémentation (2026-10-20)

### Étape 1 : ids utilisateurs numériques

- **Migration 186.** Elle crée `users_user_id_seq` et la cale au-dessus du plus grand id numérique trouvé dans `users` **et** dans les tables qui le référencent (`users_rights`, `payments`, `orders.created_by`, `cash_registers`, `stock_movements`, `audit_logs`, `services_performed`, `delivery_session`). Un nouvel utilisateur ne doit jamais hériter de l'historique d'un utilisateur supprimé. C'est la même règle que la migration 118 pour les remises.
- **`UsersRepository.NextUserID`** fournit l'id aux 3 points de création (création d'un équipier, inscription par mot de passe, inscription Google). Il est appelé hors transaction : une inscription annulée laisse seulement un trou dans la séquence.
- `helpers.UserIDPrefix` est supprimé, puisqu'il n'a plus aucun usage.

### Étape 2 : discounts sur la clé entière

- **Création.** `discounts.Repository.CreateDiscount` tire `nextval('discounts_discount_id_new_seq')`, puis écrit la valeur dans `discount_id_new` et, en texte, dans `discount_id` (toujours PRIMARY KEY), sur la remise et ses tables filles. Le `discount_id` envoyé par un client était déjà écrasé ; il l'est maintenant au niveau du repository. L'import carte ne génère plus d'id. `helpers.DiscountIDPrefix` est supprimé.
- **Lecture.** Six requêtes renvoient désormais `discount_id_new::text` et joignent les tables filles sur `discount_id_new` : les trois du pricing (`GetDiscounts`, `GetDiscountProducts`, `GetDiscountProductOptions`), les deux du Kiosk (`GetDiscounts`, `GetDiscountProductIDs`) et celle de ScanNOrder (`GetDiscounts`). Le contrat JSON reste une chaîne.
  - Côté Kiosk, l'`ORDER BY` reprend l'expression texte, comme l'exige le `DISTINCT`. Le départage reste lexicographique, comme avec l'ancien varchar.
- **Laissé tel quel :**
  - `resolveLegacyDiscountIDs`, nécessaire tant que des POS ont un pricing en cache ;
  - `orders.cart_discount_id`, qu'aucun code Go n'écrit ;
  - le cas `RewardID` mis dans `DiscountID` (`orders/service.go`) : y toucher change le comportement côté client. Ce point reste ouvert ;
  - la bascule de la PRIMARY KEY (D3).

### Étape 3 : renumérotation

**Ce qui a changé par rapport au cadrage, après lecture des données de staging et du code fiscal :**

- **Lignes signées.** Seules `payments.user_id` et `audit_logs.user_id` entrent dans une charge utile signée. `orders` et `cash_registers` sont scellés, mais leurs colonnes d'utilisateur ne font pas partie de la charge utile (`OrderClosurePayload`, `CashRegisterClosurePayload`) : on les réécrit. Les tickets signés utilisent leur propre `payments_snapshot`. Les archives sont vérifiées sur le fichier, pas reconstruites depuis la base.
- **Orphelins.** La plupart des valeurs `user-%` de staging pointent vers des utilisateurs qui n'existent plus : les 812 lignes `orders.created_by`, les 1 143 lignes `audit_logs` et 40 lignes `users_rights`. Seuls les 5 utilisateurs existants sont renumérotés ; les orphelins gardent leur valeur.
- **Colonnes à réécrire.** L'inventaire a été fait **par le contenu** et non par le nom de colonne. Il est dans `userids.Columns`.

**Ce qui a été livré :**

- **Migration 187** : elle crée `user_id_remap` **vide**. Cette table n'est jamais vidée : c'est le seul lien entre une ligne signée et son utilisateur.
- **Package `internal/userids` :**
  - `Columns` : l'inventaire des colonnes à réécrire ;
  - `Resolve` : le filet d'entrée. Il n'interroge la base que pour un id `user-…` ; tout autre id ne coûte aucune requête et ne perturbe pas les tests sqlmock ;
  - `Plan` et `Apply` : l'aperçu et la renumérotation.
- **Commande `cmd/renumber_users`**, qui remplace la migration 188 prévue en SQL :
  - sans option, elle affiche ce qui va bouger, table par table ;
  - `--apply` fait tout en une seule transaction, puis vide le cache Redis des sessions (`models.UserCachePrefix`) ;
  - elle refuse de démarrer sans `REDIS_URL`, pour ne pas laisser une session en cache écrire l'ancien id dans un paiement qui serait ensuite signé ;
  - `--apply --skip-redis` (ajouté le 2026-10-31) renumérote sans toucher à Redis, pour qui ne l'atteint pas depuis son poste (Render n'expose que l'URL interne). Les sessions déjà en cache gardent alors l'ancien `UserID` jusqu'à 60 minutes au plus (`models.UserCacheTTL`, durée fixe) : toute écriture faite avec elles porte l'ancien id. Rattrapage : attendre 60 minutes, puis relancer `--apply --skip-redis` (rejouable). Le plus sûr reste de vider Redis depuis le dashboard Render juste après `--apply` ;
  - elle est rejouable : un second passage rattrape les lignes écrites avec un ancien id entre-temps.
- **Points d'entrée qui passent par le filet :**
  - `/users/{id}/*` (8 routes du back-office) et `/users/{user_id}/location` ;
  - `roles.SetUserRole` ;
  - le filtre `?user_id=` de la liste des employés du planning ;
  - `StartDelivery?user_id=` ;
  - `StartDeliverySession` (`delivery_man.user_id`).
- **Jointures sur les paiements signés** (qui gardent l'ancien id) :
  - le détail d'une caisse (`cash_registers`) : son `INNER JOIN users` aurait fait **disparaître** ces paiements ;
  - l'id de l'encaisseur renvoyé avec les paiements d'une commande (`orders_fetcher_builder`).

### Étape 4 reportée

Trois raisons :
1. **Les orphelins.** Une FK, même `NOT VALID`, exige de décider d'abord ce qu'on fait de ces lignes (§5).
2. **Les tests d'intégration.** Environ 25 fichiers créent des utilisateurs avec des ids `itest-…`, et certains insèrent `users_rights` avant `users`. Un `CHECK` numérique et des FK les casseraient tous. Comme ils tournent contre la base staging partagée, un id numérique en dur pourrait entrer en collision avec un vrai utilisateur : il faudrait que chaque test tire ses ids de la séquence.
3. **Le gain est faible** une fois la génération numérique en place. C'est à Ilies de trancher.

### Tests

- **Unitaires.**
  - Verts, sauf 12 tests planning (`employees` DeleteEmployee, `leave` self, `swaps`) qui échouent **déjà sur HEAD** (vérifié sur une copie propre).
  - Nouveaux : `userids` (`IsNumeric`, `Resolve` sans requête hors `user-`, unicité de `Columns`, colonnes signées).
  - Mis à jour : les tests de fragments SQL du pricing et du Kiosk, qui vérifient aussi la nouvelle clé.
- **Intégration contre staging.**
  - Verts : orders, kiosk, scannorder, discounts, order_life_cycle. Leurs seeds de remises étaient cassés depuis la migration 118 (`discount_id_new` NOT NULL) et ont été corrigés ; leurs assertions comparent désormais à `discount_id_new`.
  - Échecs du module menu qui existaient déjà : `tva` `varchar(30)`, `bg_color`.
- **En attente de l'application de 186 et 187 sur staging :**
  - `userids.TestApply_Postgres`, qui s'arrête avec un message explicite tant qu'elles manquent ;
  - les tests d'intégration qui créent un utilisateur (users, signup) ;
  - ceux qui lisent les paiements d'une commande ou le détail d'une caisse.

### Déroulé

1. **Staging :**
   1. migrations 186 et 187 ;
   2. push du code ;
   3. tests : inscription, création d'un équipier, promo neuve sur POS / Kiosk / ScanNOrder, détail d'une caisse ;
   4. `verify_fiscal --all` ;
   5. `go run ./cmd/renumber_users` (aperçu), puis `--apply` ;
   6. `verify_fiscal --all` à nouveau.
2. **Prod :**
   1. aperçu lancé avant le créneau ;
   2. puis, dans le créneau 3h-5h : sauvegarde, migrations 186 et 187, push de `main`, `--apply` avec le `REDIS_URL` de prod, `verify_fiscal --all`.

### Constaté hors périmètre

- **`delivery_sessions.FinalizeDeliveredStop`** (`UPDATE payments SET user_id = ds.user_id`) réaffecte au livreur un paiement **déjà signé**. Or `user_id` fait partie de la charge utile de ce paiement : sa vérification fiscale échouera. À traiter à part.

## 9. Lot 1, suite : encaisseur du livreur et récompenses (2026-10-21)

Deux sujets liés aux mêmes tables, traités avec le lot des ids parce qu'ils partagent les mêmes migrations et le même déploiement.

### 9.1 Encaisseur réel d'un paiement : `payments.collected_by`

**Problème.** `delivery_sessions.FinalizeDeliveredStop` réécrivait `payments.user_id` avec le livreur, pour **tous** les paiements d'une commande livrée, y compris les paiements en ligne. `user_id` est dans la charge utile signée (`fiscal.PaymentPayload`) : l'empreinte de ces paiements ne se recalculait plus.

**Décision.**
- `payments.user_id` reste « qui a saisi le paiement » et n'est **jamais** réécrit après l'insertion.
- Nouvelle colonne `payments.collected_by` (migration 188), **hors charge utile signée** comme `cash_register_id`. NULL = l'encaisseur est `user_id`.
- `FinalizeDeliveredStop` n'écrit que `collected_by`, et seulement pour les paiements encaissés en main propre : `models.HandCollectedMOPs` = `ES`, `CB`, `TR`, `CHEQUE`, `HOLIDAY_VOUCHER`. Les paiements en ligne, des plateformes, de la borne et les récompenses gardent leur encaisseur.
- Lecture : `COALESCE(collected_by, user_id_remap.user_id, user_id)` dans le détail de caisse (`cash_registers`) et dans les paiements d'une commande (`orders_fetcher_builder`). Le champ JSON `collected_by` du détail de caisse, que le POS et le back-office utilisent pour découper les résumés par utilisateur, garde donc la même forme.
- `payments.collected_by` est dans l'inventaire de `cmd/renumber_users` (`userids.Columns`).

**Effet sur les résumés de caisse par utilisateur.** Les totaux du Z ne bougent pas (rattachement par `cash_register_id`). Le découpage par utilisateur reste identique pour les paiements en main propre. Il change pour les paiements en ligne d'une commande livrée, qui restent sous la personne qui les a saisis au lieu de partir chez le livreur : c'est la correction.

**Réparation de l'existant** : `cmd/repair_payment_collectors` (aperçu, puis `--apply`). Elle retrouve l'ancien `user_id` d'un paiement signé réécrit : l'empreinte est déterministe, le seul candidat qui la redonne est l'id d'origine. Candidats : l'auteur de la commande, les employés de l'établissement, les ids déjà vus sur ses paiements, les anciens ids de `user_id_remap`. Puis elle remet `user_id` et inscrit le livreur dans `collected_by` pour les paiements en main propre. Un paiement dont l'ancien id n'est pas retrouvé n'est pas modifié.
- **Staging** : rien à réparer. Les 28 paiements livrés sont en `hash_version` 1 (juin-juillet, avant le scellement v2) et ne sont pas contrôlés à l'empreinte.
- **Production** : la commande ne traite que les paiements v2, scellés depuis le 11/10. Lancer l'aperçu avant de conclure.

### 9.2 Récompenses « produit offert » : `orderitems.reward_id`

**Problème.** Une récompense de fidélité (`customer_rewards`, `reward_type = 'free_product'`) était enregistrée dans `orderitems.discount_id`, dans le même espace de numérotation que les remises. Constat sur staging : les `reward_id` vont de 80 à 293 et les `discount_id_new` de 3 à 100, donc ils se chevauchent. Trois conséquences : la ligne pouvait afficher le nom d'une remise sans rapport, la clôture fiscale signée enregistrait la valeur comme une remise, et l'écriture dans `discount_redemptions` violait sa clé étrangère.

**Décision.** Champ propre `reward_id`, `discount_id` ne contient plus que des remises.
- **Migration 189** : `orderitems.reward_id` (integer, nullable, sans clé étrangère). Hors charge utile signée.
- **Pricing** : une ligne offerte porte `reward_id`. Tant que `models.RewardLineInDiscountID` vaut `true`, elle porte **aussi** l'id de récompense dans `discount_id`, comme avant : un ancien POS fusionne les lignes d'un même produit dès que leur `discount_id` est identique ; sans lui, la ligne offerte fusionnerait avec une ligne payée et le panier serait faux.
- **Écriture** (`resolveLineDiscounts`, création et modification de commande) : `discount_id` ne reçoit jamais une récompense.
  1. `reward_id` fourni : conservé, `discount_id` vidé.
  2. `discount_id` d'une remise du marchand : conservé.
  3. `discount_id` d'une récompense « produit offert » de cette commande (`used_rewards` à la création, `customer_rewards.used_on_order_id` en modification) : déplacé dans `reward_id`. C'est le cas des POS pas encore à jour.
  4. Tout autre identifiant (inconnu, d'un autre marchand, non numérique, hors `integer`, récompense en montant ou en pourcentage) : attribution abandonnée avec un log, la ligne garde son prix. Avant, un `discount_id` inconnu faisait échouer l'écriture dans la transaction de la commande.
  - Un identifiant à la fois remise du marchand et récompense de la commande reste une remise (loggé). Cette ambiguïté disparaît avec la transition.
- **Lecture** (`orders_fetcher_builder`) : `reward_id` est renvoyé ; `discount_id` le reprend à défaut pendant la transition ; `discounted_price` est renseigné pour une ligne offerte.
- **Fiscal** : une ligne offerte est signée avec `discount_id = null`. `reward_id` n'est pas dans la charge utile signée : aucune version de signature à changer.

**Clients.**

| Client | Adaptation | État |
|---|---|---|
| POS Flutter | `rewardId` dans le modèle, la lecture du pricing, la charge utile de commande, la clé de fusion des lignes du panier (`cart_merge_mixin`) et la clé de contenu (`product_content_key_helper`). Test `test/core/mixins/cart_merge_reward_test.dart` | Codé, à pousser par Ilies |
| ScanNOrder, API | Rien : la création de commande recalcule le pricing côté serveur et reprend `reward_id` | Rien à faire |
| ScanNOrder, front | `normalizePricingResponse` compte une ligne `reward_id` à part (« Récompense fidélité »), sans la fusionner avec une remise de même numéro. Types `reward_id`. Test `normalize.pricing.test.ts` | Codé, non commité |
| Borne (Kiosk) | Rien : elle n'utilise que le nom de remise des lignes et l'API recalcule le pricing | Rien à faire |
| Back-office | Rien : le type d'une ligne de commande déclare `discount_id` et `discounted_price`, mais aucun code ne les lit | Rien à faire |

### 9.3 Déroulé

**Lot 1** (tout ce qui précède, plus les étapes 1 à 3 du §8). Le code dépend des migrations : **les appliquer avant de pousser**.
1. Staging : migrations 186, 187, 188 et 189, puis push `staging`.
2. Tests sur staging : inscription, création d'un équipier, promo sur POS / Kiosk / ScanNOrder, une récompense « produit offert » (une ligne offerte et une ligne payée du même produit sur le POS), une livraison avec paiement espèces et paiement en ligne, détail d'une caisse.
3. Les tests d'intégration bloqués jusque-là : `userids`, signup, users, orders, order_life_cycle, cash_registers, `fiscal` (réparation), `delivery_sessions`, `reward_line`.
4. Aperçu de `cmd/repair_payment_collectors` et de `cmd/renumber_users`, puis `cmd/renumber_users --audit` (colonnes hors inventaire) ; `verify_fiscal --all` avant et après.
5. Production : sauvegarde, migrations 186 à 189, push `main`, puis les deux commandes en `--apply` dans le créneau 3h-5h.

**Lot 2** (après la mise à jour des POS et du front ScanNOrder, vérifiable avec `users_devices.app_version`) :
1. Passer `models.RewardLineInDiscountID` à `false`.
2. Vérifier sur une tablette à jour qu'une ligne offerte (prix remisé, sans `discount_id`) s'affiche correctement.
3. Supprimer la constante et ses branches.
### 9.4 Produit offert conservé quand on modifie la commande (2026-10-21)

**Problème.** Modifier au POS une commande dont la récompense « produit offert » était déjà consommée (créée par ScanNOrder) refaisait payer la ligne offerte au plein tarif. Le recalcul de prix ne retrouvait plus la récompense : le POS n'envoie aucune `available_rewards`, et `GetRewards` écarte de toute façon les récompenses déjà utilisées. Défaut antérieur au lot 1.

**Correction** (`OrdersRepository.GetRewards`, `appliedRewardIDs`). Le POS n'envoie pas l'identifiant de la commande au recalcul, mais l'`order_item_id` de ses lignes déjà enregistrées. Le serveur lit en base la récompense que portent ces lignes (`orderitems.reward_id`) et la réapplique. Rien n'est cru sur parole : la ligne doit appartenir au marchand, la récompense doit avoir été consommée par la commande de cette ligne et appartenir au client de la commande. Seul le produit offert est repris : une réduction en montant ou en pourcentage passe par un paiement `REWARD` déjà enregistré, qu'un recalcul ne doit pas doubler. Aucune modification de l'app. Les recalculs sans ligne enregistrée (nouvelle commande, borne, ScanNOrder) ne changent pas et n'exécutent aucune requête de plus.

**Limite connue.** Si le caissier retire le produit offert, la récompense reste consommée par la commande au lieu de revenir au client. C'est le comportement d'avant ; la libération fait partie du lot 2.

Test : `reward_edit_postgres_integration_test.go` (10 cas sur la vraie base, dont les refus : autre client, autre marchand, récompense d'une autre commande, réduction en montant, identifiants invalides). Les deux cas positifs échouent quand on neutralise la correction.

### 9.5 Lot 2 : récompenses à la caisse (décisions du 2026-10-21, à faire)

Aujourd'hui, une récompense ne s'applique qu'à ScanNOrder : le serveur y retrouve le client par son téléphone. Le POS envoie déjà `customer_id` (chaîne vide pour un nouveau client) mais ne charge ni n'envoie jamais de récompense.

**Décisions d'Ilies.**
1. **Toutes les récompenses** : produit offert, réduction en montant, réduction en pourcentage.
2. **`automatically_add_customer_rewards` s'applique à la caisse**, comme à ScanNOrder.
3. Pas de récompense pour un nouveau client (`customer_id` vide).

**Contraintes relevées, à respecter pour que ce soit sûr.**
- Le POS n'envoie jamais `used_rewards` (toujours `[]`) et `consumeUsedRewards` ne s'exécute qu'à la création, d'après cette liste du client : une récompense appliquée à la caisse ne serait jamais consommée et pourrait servir à l'infini. La consommation doit être décidée par le serveur, à partir de lignes et de paiements `REWARD` contrôlés, jamais d'une liste envoyée par le client.
- `UpdateOrder` ne consomme ni ne libère aucune récompense : une récompense ajoutée à la modification ne serait pas consommée, une récompense dont la ligne est retirée ne reviendrait pas au client.
- Le chargement des récompenses du client côté serveur se fait dans `OrdersService.GetPricing` (chemin POS uniquement), en ignorant toute liste envoyée par le client.
- `free_product` ne s'applique pas avec `reward_value <= 0` et facture la ligne `reward_value` centimes.
### 9.6 Relecture du lot 1 (2026-10-21)

Relecture critique de l'ensemble du lot après les tests sur staging. Trois corrections :

- **Colonnes oubliées de la renumérotation** : `customer.blocked_by` (écrite avec l'id de l'utilisateur qui bloque un client) et `upsell_suggestions.staff_member_id` ne figuraient pas dans `userids.Columns`. L'inventaire avait été établi par le contenu de staging, où ces colonnes sont vides ; en production, un `blocked_by` aurait gardé un ancien id orphelin.
- **Filet pour ce que l'inventaire ne connaît pas** : `cmd/renumber_users --audit` cherche les anciens identifiants dans **toutes** les colonnes texte de la base hors inventaire (`userids.Audit`). Lecture seule, longue (70 secondes sur staging) : à lancer hors service en production, avant puis après `--apply`. Sont ignorées `audit_logs.resource_id` (dans la charge utile signée du journal) et les contenus JSON. Test : `TestAudit_Postgres` (une colonne hors inventaire contenant un ancien id doit être signalée).
- **`reward_id` explicite contrôlé** : un `reward_id` envoyé par un client est désormais vérifié comme l'ancien écho dans `discount_id` — la récompense doit être « produit offert » et consommée par cette commande (`used_rewards` à la création, `customer_rewards.used_on_order_id` à la modification). Sinon il est écarté avec un log. Une ligne porte une récompense ou une remise, jamais les deux.

Vérifié sans problème : les ids du menu ScanNOrder (cache de 10 minutes) et ceux des lignes du panier ne sont jamais rapprochés par le front.