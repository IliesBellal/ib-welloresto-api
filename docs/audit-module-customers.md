# Audit — Domaine « client » (`internal/modules/customers/`), état actuel

Date : 2026-08-14 · Branche : `staging` · Périmètre : `ib-welloresto-api` + `wello-back-office`

Ce document audite le domaine **client tel qu'il existe aujourd'hui**, indépendamment de tout import.
Objectif : préalable à la conception d'un futur import de clients, sur le modèle architectural documenté dans
[`docs/audit-import-produits-implementation.md`](audit-import-produits-implementation.md) (ci-après « l'audit
produit »). Le vocabulaire de ce document reprend celui de l'audit produit (fonction « transaction-agnostique »,
tables `import_*_mapping`, `dbutils.RunInTx` réentrant, scoping merchant) et compare explicitement les deux
domaines quand c'est pertinent.

Convention : chemins relatifs à la racine du dépôt cité. `ib-welloresto-api` sauf mention contraire ;
`wello-back-office` explicité par un préfixe `[BO]`.

---

## 1. Modèle d'unicité des clients

### 1.1 Contrainte SQL

La table `customer` ne porte **aucune contrainte UNIQUE**, ni sur `customer_email`, ni sur `customer_tel`, ni sur
aucune combinaison de colonnes — scopée par marchand ou globale. Seules deux structures existent :

- `PRIMARY KEY (customer_id)` — [`staging_schema_dump.sql:6548`](../staging_schema_dump.sql#L6548) (contrainte
  `customer_pkey`).
- Un **index composite non-unique** de recherche :
  ```sql
  CREATE INDEX idx_customer_idx_customer_lookup
    ON customer (merchant_id, enabled, customer_code, customer_name, customer_tel, customer_address);
  ```
  [`docs/migration-postgres/04-schema-postgres-target.sql:886`](migration-postgres/04-schema-postgres-target.sql#L886),
  identique dans [`staging_schema_dump.sql:7767`](../staging_schema_dump.sql#L7767).

**Contraste avec `products`** : le module menu n'a pas non plus d'UNIQUE SQL sur `products.name` (l'audit produit
§5.1 le note aussi), mais la dédup y est entièrement applicative et centralisée dans `preview.go`/`commit_plan.go`.
Côté `customer`, il n'existe **aucun équivalent** de ce mécanisme centralisé (voir §1.2).

### 1.2 Contrainte applicative : dispersée, pas centralisée

Deux fonctions de lookup existent dans le repository et sont **disponibles**, mais **ne sont pas appelées
automatiquement** par la fonction d'écriture elle-même :

- `FindCustomerByEmail(ctx, email, merchantID)` — [`repository.go:259`](../internal/modules/customers/repository.go#L259),
  comparaison `LOWER(customer_email) = LOWER(?)`, scopée merchant.
- `FindCustomerByPhone(ctx, phone, merchantID)` — [`repository.go:279`](../internal/modules/customers/repository.go#L279),
  normalise le téléphone en entrée (`helpers.NormalizePhoneNumber(phone, "FR")`) puis compare en égalité stricte,
  filtre `enabled = true`, scopée merchant.

`UpdateOrCreateCustomer` — [`repository.go:36`](../internal/modules/customers/repository.go#L36) — **ne consulte
aucun de ces deux lookups**. Sa branche INSERT (`CustomerID` vide) écrit inconditionnellement une nouvelle ligne,
quel que soit l'état existant en base pour cet email/téléphone.

La responsabilité de « chercher avant de créer » est donc **répartie chez chaque appelant**, avec des stratégies
différentes selon le point d'entrée :

| Appelant | Stratégie de dédup avant upsert | Chemin |
|---|---|---|
| `bookingcore.CreateBooking` (réservation, flux public + staff) | Lookup par **téléphone** uniquement, et seulement si `CustomerID` absent | [`bookingcore/create.go:77-89`](../internal/modules/bookingcore/create.go#L77-L89) |
| `bookings.upsertBookingCustomer` (édition réservation) | Même logique téléphone, **dupliquée** (pas de fonction partagée avec `CreateBooking`) | [`bookings/repository.go:1194-1231`](../internal/modules/bookings/repository.go#L1194-L1231) |
| `bookings.FindOrCreateCustomerByPhone` (liste d'attente) | Encore une **troisième** implémentation : SQL brut réécrit à la main plutôt qu'un appel à `FindCustomerByPhone` | [`bookings/waitlist_repository.go:15-38`](../internal/modules/bookings/waitlist_repository.go#L15-L38) |
| `order_life_cycle.upsertCustomer` (création de commande — flux caisse/SNO/kiosk) | **Aucune** — fait confiance à `req.Order.Customer.CustomerID` s'il est fourni, sinon insère à l'aveugle | [`order_life_cycle/repository.go:1254-1313`](../internal/modules/order_life_cycle/repository.go#L1254-L1313) |
| `order_life_cycle` flux facturation (email facture) | Lookup par `CustomerID` si fourni, **sinon par email** (`FindCustomerByEmail`) — troisième clé de dédup différente | [`order_life_cycle/service.go:1178-1214`](../internal/modules/order_life_cycle/service.go#L1178-L1214) |

**Constat clé** : il existe **4 chemins de création distincts, 3 stratégies de dédup différentes (téléphone /
email / aucune), aucune fonction unique appelée par tous.** Le chemin le plus emprunté en volume — la création
de commande standard (`order_life_cycle.upsertCustomer`) — **ne fait aucune vérification de doublon**.

### 1.3 Email / téléphone : optionnalité

- DDL : `customer_tel varchar(20)` et `customer_email varchar(255)` sont tous deux **nullable**, aucun `NOT NULL`
  ([`staging_schema_dump.sql:1401-1404`](../staging_schema_dump.sql#L1401-L1404)).
- Côté Go, `models.Customer` (le type réellement utilisé partout) déclare `CustomerTel *string` et
  `CustomerEmail *string` — [`internal/models/orders_model.go:164-165`](../internal/models/orders_model.go#L164-L165) —
  tous deux pointeurs optionnels.
- La branche INSERT de `UpdateOrCreateCustomer` boucle sur **toutes** les colonnes de la map `allowed`
  ([`repository.go:107-115`](../internal/modules/customers/repository.go#L107-L115)) et écrit `NULL` pour tout champ
  absent (`extractFieldValue` retourne `nil`) — un client peut donc être créé **sans email ni téléphone**, seul
  `merchant_id` est forcé ([`repository.go:117-120`](../internal/modules/customers/repository.go#L117-L120)).
- **[À CLARIFIER]** Il existe un second type `Customer` local au paquet, déclaré dans
  [`customers/models.go:178-208`](../internal/modules/customers/models.go#L178-L208), où `CustomerTel` est une
  `string` **non pointeur** (suggérant un champ obligatoire) — mais ce type n'est référencé nulle part ailleurs
  dans le dépôt (`customers.Customer` n'apparaît dans aucun autre fichier `.go`). C'est un type mort/orphelin,
  potentiellement trompeur pour un futur lecteur qui s'y fierait plutôt qu'à `models.Customer`.

### 1.4 Comportement à la création d'un doublon

**Pas de rejet dur, pas de fusion explicite, pas d'avertissement structuré.** Le comportement dépend entièrement
du chemin d'entrée (§1.2) :

- Chemins avec lookup préalable (réservations) : si le téléphone correspond à un client existant, celui-ci est
  **réutilisé silencieusement** (le `customer_id` trouvé est injecté avant l'appel à `UpdateOrCreateCustomer`,
  qui bascule alors en mode UPDATE). Aucun signal n'est renvoyé à l'appelant indiquant qu'un doublon potentiel a
  été fusionné plutôt que créé.
- Chemin sans lookup (`order_life_cycle.upsertCustomer`, création de commande) : **une nouvelle ligne `customer`
  est insérée à chaque commande sans `customer_id` connu côté client**, même si un client au même téléphone/email
  existe déjà — aucun code d'erreur, aucun warning, la duplication est silencieuse.

**Contraste avec `products`** : le chemin unitaire de création produit a un mécanisme de confirmation à double
appel (`ErrProductNameAlreadyExistsWithRetry`, documenté en audit produit §5.2/§14.b). **Rien d'équivalent
n'existe côté client** — ni blocage, ni confirmation, ni retour d'un code d'erreur dédié.

### 1.5 Normalisation avant comparaison

| Champ | Normalisation à l'écriture | Normalisation à la lecture/comparaison |
|---|---|---|
| Téléphone | `helpers.NormalizePhoneNumber(tel, "FR")` appliqué **à chaque écriture**, dans `extractFieldValue` — [`repository.go:168-172`](../internal/modules/customers/repository.go#L168-L172) | `FindCustomerByPhone` normalise aussi la valeur recherchée avant comparaison stricte — [`repository.go:281`](../internal/modules/customers/repository.go#L281) |
| Email | **Aucune** — la valeur brute fournie est stockée telle quelle (`extractFieldValue`, cas `default`, [`repository.go:199-206`](../internal/modules/customers/repository.go#L199-L206)) | `LOWER(...)` uniquement dans `FindCustomerByEmail` — [`repository.go:266`](../internal/modules/customers/repository.go#L266) ; **aucune autre requête** ne normalise la casse (ex. `SearchCustomers` ne recherche même pas sur `customer_email`, voir §4) |

Conséquence directe pour une dédup email : deux emails ne différant que par la casse (`Jean@Wello.fr` vs
`jean@wello.fr`) sont stockés tels quels et **ne seront reconnus comme identiques que par les appelants qui
passent explicitement par `FindCustomerByEmail`** — ce qui exclut `order_life_cycle.upsertCustomer` (§1.2).
`helpers.NormalizePhoneNumber` — [`internal/helpers/phone_helper.go:23`](../internal/helpers/phone_helper.go#L23) —
gère le préfixe international et le `0` initial pour la France (`countryCode="FR"` codé en dur dans tous les
appels du module `customers`), mais ne valide pas la plausibilité du numéro (contraste avec
`helpers.FormatToE164`, [`phone_helper.go:95`](../internal/helpers/phone_helper.go#L95), qui existe dans le même
fichier mais **n'est jamais utilisé côté module `customers`**).

---

## 2. Fonctions cœur réutilisables pour un futur commit d'import

### 2.1 `UpdateOrCreateCustomer` — transaction-agnostique, mais via le contexte, pas un paramètre `tx`

Signature : `func (r *CustomersRepository) UpdateOrCreateCustomer(ctx context.Context, c *models.Customer) (*string, error)`
— [`repository.go:36`](../internal/modules/customers/repository.go#L36).

Elle résout son exécuteur SQL via `dbx.GetDB(ctx, r.database)` ([`repository.go:37`](../internal/modules/customers/repository.go#L37)),
qui délègue à `dbutils.GetDB(ctx, defaultDB)` — [`internal/utils/dbutils/tx.go:31`](../internal/utils/dbutils/tx.go#L31) —
lequel retourne la transaction injectée dans le `ctx` par `dbutils.RunInTx` si elle existe
([`internal/utils/dbutils/tx.go:24-29`](../internal/utils/dbutils/tx.go#L24-L29)), sinon la connexion par défaut.

C'est le **même mécanisme exact** que celui utilisé par `insertProductTx` côté menu — signature
`func (r *MenuRepository) insertProductTx(ctx context.Context, p *CreateProductPayload) (string, error)`
([`internal/modules/menu/repository.go:2462`](../internal/modules/menu/repository.go#L2462)) : ni l'une ni l'autre
ne prend un paramètre `tx` explicite ; toutes deux résolvent l'exécuteur depuis le `ctx`. Le terme
« transaction-agnostique » de l'audit produit s'applique donc **identiquement** à `UpdateOrCreateCustomer` — la
réutilisabilité dans une transaction englobante multi-clients est déjà acquise, **aucun changement de signature
requis**.

Preuve d'usage réel en imbrication : `bookingcore.CreateBooking` ouvre `dbutils.RunInTx` puis appelle
`customerRepo.UpdateOrCreateCustomer(txCtx, customer)` **à l'intérieur** — [`bookingcore/create.go:61-92`](../internal/modules/bookingcore/create.go#L61-L92) —
et `dbutils.RunInTx` est bien réentrant (si une tx est déjà dans le `ctx`, elle est réutilisée telle quelle,
[`internal/utils/dbutils/run_in_tx.go:11-13`](../internal/utils/dbutils/run_in_tx.go#L11-L13)) — exactement le
même contrat que documenté pour `dbutils.RunInTx` dans l'audit produit §6.

### 2.2 Couche service : passthrough, pas de transaction propre

`CustomersService.UpsertCustomer(ctx, c)` — [`service.go:43-45`](../internal/modules/customers/service.go#L43-L45) —
ne fait qu'appeler `s.customerRepo.UpdateOrCreateCustomer(ctx, c)` sans ouvrir de transaction. Il est donc
lui-même utilisable dans une transaction englobante ouverte par l'appelant — confirmé en usage réel :
`order_life_cycle/service.go` l'appelle **à l'intérieur** d'un `dbutils.RunInTx(ctx, s.db, func(txCtx...))`
([`order_life_cycle/service.go:1178-1214`](../internal/modules/order_life_cycle/service.go#L1178-L1214)).

**Piège de nommage à noter pour un futur import** : il existe une **autre** méthode,
`CustomersService.UpdateOrCreateCustomer(ctx, params map[string]interface{})` —
[`service.go:24-30`](../internal/modules/customers/service.go#L24-L30) — au nom presque identique à la méthode du
repository, mais qui est un **stub mort** : elle ne touche ni le repository ni la base, et retourne un succès
factice codé en dur (`"status": "success"`). Un futur commit d'import qui appellerait par erreur cette méthode de
service plutôt que `UpsertCustomer` n'écrirait **rien** en base sans lever d'erreur.

### 2.3 Entités annexes

**Aucune table de tags/segments/groupes clients n'existe** (recherche `customer_tags`, `customer_segment`,
`customer_group` : zéro résultat dans le code Go ou le schéma). Les seules entités annexes réelles sont le
programme de fidélité :

- `CreateLoyaltyProgram(ctx, merchantID, loyaltyProgramID, req)` — [`repository.go:630`](../internal/modules/customers/repository.go#L630) —
  également transaction-agnostique via `dbx.GetDB(ctx, ...)`, mais **exécute 3 écritures non enveloppées dans une
  transaction locale** : `INSERT INTO customer_loyalty_programs` puis
  `replaceLoyaltyProgramTargetProducts` puis `replaceLoyaltyProgramRewardProducts`
  ([`repository.go:638-681`](../internal/modules/customers/repository.go#L638-L681)). Si l'appelant ne les
  enveloppe pas lui-même dans un `RunInTx`, une panne entre les 3 étapes laisse un état partiel (programme créé
  sans ses produits cibles/récompenses). **Contraste avec `MaterializeImportTx`** côté menu (audit produit §6/§7.4)
  qui garantit tout-ou-rien sur l'ensemble du lot : ici, rien ne garantit l'atomicité même pour une seule entité
  fidélité, en dehors d'un appelant qui prendrait l'initiative d'un `RunInTx` (aucun des appelants actuels du
  repository ne le fait pour `CreateLoyaltyProgram`).
- `customer_advertisement_emails` (table de suivi marketing) : présente au schéma
  ([`staging_schema_dump.sql:1441-1451`](../staging_schema_dump.sql#L1441-L1451)) mais **aucune référence dans le
  code Go** — ni lecture ni écriture. Probable reliquat legacy, hors périmètre d'un import.

**Conclusion §2** : le patron `insertProductTx`-like existe déjà côté client sans réécriture de signature
nécessaire (`UpdateOrCreateCustomer` + `dbx.GetDB(ctx,...)`), mais **rien d'équivalent aux tables
catégories/tags du module menu n'existe côté client** — pas de structure annexe à mapper au-delà du programme de
fidélité, qui n'a pas vocation à être peuplé par un import clients.

---

## 3. Modèle de données

### 3.1 Table `customer`

DDL complet ([`staging_schema_dump.sql:1393-1433`](../staging_schema_dump.sql#L1393-L1433), identique en
substance dans [`docs/migration-postgres/04-schema-postgres-target.sql:843-885`](migration-postgres/04-schema-postgres-target.sql#L843-L885)) :

| Colonne | Type | Nullable | Défaut |
|---|---|---|---|
| `customer_id` | `integer` (IDENTITY) | NOT NULL | auto |
| `customer_brand` | `varchar(20)` | NOT NULL | `'WELLO_RESTO'` |
| `customer_brand_id` | `varchar(50)` | nullable | — |
| `merchant_id` | `varchar(64)` | **nullable** (pas de `NOT NULL` au niveau colonne) | — |
| `customer_name` | `varchar(50)` | nullable | — |
| `customer_first_name` | `varchar(50)` | nullable | — |
| `customer_last_name` | `varchar(50)` | nullable | — |
| `customer_code` | `varchar(4)` | nullable | — |
| `customer_tel` | `varchar(20)` | nullable | — |
| `customer_temporary_phone` / `_code` | `varchar(20)` | nullable | — |
| `customer_email` | `varchar(255)` | nullable | — |
| `customer_address` | `varchar(255)` | nullable | — |
| `customer_floor_number` | `varchar(11)` | nullable | — |
| `customer_door_number` | `varchar(25)` | nullable | — |
| `customer_additional_address` | `varchar(255)` | nullable | — |
| `customer_business_name` | `varchar(50)` | nullable | — |
| `customer_birthdate` | `date` | nullable | — |
| `customer_additional_info` | `varchar(255)` | nullable | — |
| `customer_temporary_*` (adresse/lat/lng/étage/porte) | mixte | nullable | — |
| `customer_total_spent` | `integer` | NOT NULL | `0` |
| `customer_google_place_id` | `varchar(255)` | nullable | — |
| `customer_lat` / `customer_lng` | `double precision` | nullable | — |
| `customer_nb_orders` | `integer` | NOT NULL | `0` |
| `customer_nb_bookings` | `integer` | NOT NULL | `0` |
| `customer_zone_code` | `varchar(4)` | nullable | — |
| `customer_zone_updated_at` | `timestamptz` | nullable | — |
| `last_order_date` | `timestamptz` | nullable | — |
| `last_advertisement_date` | `timestamptz` | nullable | — |
| `loyalty_reminder_count` | `integer` | NOT NULL | `0` |
| `advertising_consent` | `boolean` | NOT NULL | `true` |
| `creation_date` | `timestamptz` | NOT NULL | `now()` |
| `enabled` | `boolean` | NOT NULL | `true` |
| `delivery_notes` | `text` | nullable | — |

Contraintes : `PRIMARY KEY (customer_id)` uniquement (§1.1). Pas de FK déclarée vers `merchant` dans le dump
(convention déjà notée côté menu : absence de FK généralisée dans ce dépôt).

### 3.2 Champs RGPD / consentement marketing

- `advertising_consent boolean NOT NULL DEFAULT true` — seul champ de consentement explicite.
- **Incohérence entre défaut SQL et défaut applicatif** : le défaut SQL est `true` (opt-in par défaut), mais
  `extractFieldValue` pour ce champ retourne **`false`** dès que `c.AdvertisingConsent` est `nil`
  ([`repository.go:144-148`](../internal/modules/customers/repository.go#L144-L148)) :
  ```go
  case "advertising_consent":
      if c.AdvertisingConsent != nil {
          return *c.AdvertisingConsent
      }
      return false
  ```
  Or la branche INSERT écrit **toujours** une valeur explicite pour cette colonne (boucle sur tout `allowed`,
  §1.3) — le défaut SQL `true` n'est donc **jamais réellement appliqué** en pratique via ce chemin : tout client
  créé sans consentement explicitement fourni reçoit `advertising_consent = false`. C'est cohérent avec une
  posture RGPD prudente (pas de consentement par défaut), mais contredit la lecture littérale du DDL — à garder
  en tête pour un import qui devrait décider explicitement de la valeur à écrire plutôt que de compter sur le
  défaut SQL.
- `customer_advertisement_emails` (table séparée, historique d'envois marketing) et
  `last_advertisement_date` (colonne sur `customer`) : présents au schéma, **aucune référence en code Go** — état
  mort/non exploité actuellement (§2.3).
- Pas de champ « date de consentement » (horodatage de l'opt-in/opt-out) — seul l'état booléen courant est
  conservé, sans historique.

### 3.3 Scoping marchand

Un client est rattaché à **un seul `merchant_id`** — confirmé par toutes les requêtes du repository qui filtrent
systématiquement sur `merchant_id = ?` (`GetCustomerByID`, `FindCustomerByEmail`, `FindCustomerByPhone`,
`SearchCustomers`, `ListCustomers`), et par l'insertion qui force `merchant_id` à chaque écriture
([`repository.go:118-120`](../internal/modules/customers/repository.go#L118-L120)). Aucun mécanisme de partage
entre marchands n'existe dans le code actuel — confirmé également par une note externe au module (
[`cadrage-technico-fonctionnel-lot1.md:146`](audit-reservation-existant.md) indique explicitement que la « fiche
client partagée multi-établissement » est un chantier hors périmètre, non commencé).

**Fait notable** : la colonne `merchant_id` elle-même n'est **pas `NOT NULL`** au niveau DDL (§3.1) — le scoping
est garanti uniquement par la discipline applicative (le code force toujours sa valeur à l'écriture), pas par une
contrainte SQL.

### 3.4 Statuts / suppression

- `enabled boolean NOT NULL DEFAULT true` — utilisé en **lecture** comme filtre de soft-delete
  (`FindCustomerByPhone` : `WHERE ... AND enabled = true`, [`repository.go:287`](../internal/modules/customers/repository.go#L287) ;
  `SearchCustomers`/`ListCustomers` : même filtre, [`repository.go:1005`](../internal/modules/customers/repository.go#L1005) et
  [`repository.go:1204`](../internal/modules/customers/repository.go#L1204)).
- **Aucun code Go ne met jamais `enabled = false` sur la table `customer`** (recherche exhaustive sur
  `UPDATE customer ... enabled`, aucune occurrence trouvée) — contrairement à `customer_loyalty_programs`, qui a
  bien un `DeleteLoyaltyProgram` soft-delete ([`repository.go:772-782`](../internal/modules/customers/repository.go#L772-L782)).
  **[À CLARIFIER]** : le mécanisme qui désactiverait un client (le cas échéant) n'est pas dans ce module — ni
  endpoint HTTP, ni tâche de fond identifiée dans `internal/tasks/` référençant `customer.enabled`.
- Pas de suppression physique identifiée non plus (aucun `DELETE FROM customer` dans le code applicatif, en
  dehors des tests d'intégration qui nettoient leurs fixtures).

---

## 4. Points d'entrée existants de création/modification client

### 4.1 Aucun endpoint HTTP de création/modification directe d'un client

**Fait majeur, à contraster avec le module menu** : il n'existe **aucune route `POST`/`PUT`/`PATCH` exposant
directement `UpdateOrCreateCustomer`/`UpsertCustomer`**. Les routes enregistrées pour `/customers` — 
[`cmd/api/routes.go:1129-1141`](../cmd/api/routes.go#L1129-L1141) — sont exclusivement :

| Méthode + URL | Handler | Nature |
|---|---|---|
| `GET /customers/search` | `SearchCustomers` | lecture |
| `GET /customers/list` | `ListCustomers` | lecture |
| `GET /customers/loyalty-programs` | `GetLoyaltyPrograms` | lecture |
| `GET /customers/loyalty-programs/{id}` | `GetLoyaltyProgram` | lecture |
| `POST /customers/loyalty-programs` | `CreateLoyaltyProgram` | écriture (programme fidélité, pas client) |
| `PATCH /customers/loyalty-programs/{id}` | `UpdateLoyaltyProgram` | écriture (idem) |
| `DELETE /customers/loyalty-programs/{id}` | `DeleteLoyaltyProgram` | écriture (idem) |
| `GET /customers/{id}/loyalty` | `GetCustomerLoyalty` | lecture |
| `PATCH /customers/{id}/loyalty/{program_id}` | `UpdateLoyaltyProgress` | écriture (progression fidélité) |
| `PATCH /customers/{id}/rewards/{id}` | `UpdateLoyaltyReward` | écriture (récompense) |

**La création/mise à jour de la fiche client elle-même n'a jamais de route HTTP dédiée.** Elle n'existe que
comme **effet de bord interne** des flux commande (`order_life_cycle`) et réservation (`bookingcore`/`bookings`),
appelée directement depuis le code service/repository de ces modules, jamais depuis un handler `customers`.

Confirmation côté front : `customersService.ts` — [`wello-back-office/src/services/customersService.ts`](../../wello-back-office/src/services/customersService.ts) —
n'expose **aucune fonction `createCustomer`/`updateCustomer`**. Seules des fonctions de lecture (`getCustomersList`,
`searchCustomers`, `getCustomerOrders`, `getCustomerLoyalty`) et d'écriture fidélité existent. `CustomerDetailsSheet.tsx`
([`wello-back-office/src/components/customers/CustomerDetailsSheet.tsx`](../../wello-back-office/src/components/customers/CustomerDetailsSheet.tsx))
est un panneau de consultation en lecture seule (onglets commandes / fidélité) — aucun formulaire d'édition de
fiche client. **Il n'existe donc, contrairement au produit (`ProductCreateSheet`), aucun patron d'écran de
création/édition client à réutiliser ou dont s'inspirer côté front.**

Route dépréciée `/customer` (sans « s ») — [`cmd/api/routes.go:1112-1126`](../cmd/api/routes.go#L1112-L1126) —
strictement identique en périmètre (mêmes lectures, mêmes écritures fidélité), même absence de
création/modification client directe. Dépréciée depuis 2024-06-25 (cf. CLAUDE.md), toujours enregistrée.

### 4.2 Validations : dispersées par appelant, pas centralisées

Il n'existe pas de fonction de validation unique équivalente à `validateProductForCreate` du module menu. Les
seules validations trouvées sont :

- `bookingcore.CreateBooking` : `IsValidStatus(p.Status)` (statut réservation, sans rapport avec les champs
  client) — [`create.go:57-59`](../internal/modules/bookingcore/create.go#L57-L59). Aucune validation de format
  email/téléphone avant écriture.
- `order_life_cycle` (flux facturation uniquement) : regex sur l'email
  (`invoiceEmailRegex.MatchString(email)`) — [`service.go:1161-1164`](../internal/modules/order_life_cycle/service.go#L1161-L1164) —
  mais **ce contrôle n'existe que sur ce chemin précis** (génération de facture), pas sur la création de commande
  standard (`order_life_cycle.upsertCustomer`), ni sur les réservations.
- Aucune validation de format téléphone (au-delà de la normalisation silencieuse, §1.5) : un numéro invalide
  normalisé par `NormalizePhoneNumber` sans passer par `FormatToE164`/`phonenumbers.IsValidNumber` est accepté
  tel quel.

**Conclusion** : contrairement au module menu où `validateProductForCreate` est documenté comme point de
validation unique explicitement mirroré par la preview d'import (audit produit §4.4), **il n'existe côté client
aucune fonction de validation centrale à mirrorer** pour un futur commit d'import — chaque flux applique (ou
n'applique pas) ses propres règles.

### 4.3 RBAC / permissions

Les routes `/customers/*` ne portent **que `authMiddleware`**
([`cmd/api/routes.go:1130`](../cmd/api/routes.go#L1130)) — **aucun `middleware.RequirePermission` n'est appliqué**,
alors que :

- Deux fonctions de permission dédiées existent et sont pleinement implémentées :
  `HasCustomerManagementAccess` et `HasCustomerExportAccess` —
  [`internal/middleware/permissions.go:88-96`](../internal/middleware/permissions.go#L88-L96), s'appuyant sur
  `UserLoginRow.HasCustomerManagementAccess()`/`HasCustomerExportAccess()` —
  [`internal/modules/auth/models.go:334-341`](../internal/modules/auth/models.go#L334-L341) — exposées dans la
  réponse de login (`internal/modules/auth/service.go:493,515-516`).
- La documentation interne du dépôt **prescrit explicitement** leur usage sur ce bloc de routes :
  [`docs/PERMISSIONS_MIDDLEWARE_GUIDE.md:325`](PERMISSIONS_MIDDLEWARE_GUIDE.md#L325) — 
  `| Customers | /customers/* | Gestion clients | HasCustomerManagementAccess |`.

**Écart documentation/code non corrigé** : n'importe quel utilisateur authentifié (quel que soit son rôle) peut
aujourd'hui lister/rechercher tous les clients et gérer les programmes de fidélité d'un marchand, sans que la
permission dédiée prévue pour cela ne soit vérifiée.

**Contraste avec le module menu import** : l'audit produit §9 souligne que les 3 routes d'import sont
« les seules du bloc `/menu` à porter `RequirePermission(HasMenuAccess)` » — un geste RBAC délibéré pour un
endpoint à fort impact. Côté client, la fonction équivalente (`HasCustomerManagementAccess`) existe déjà mais
**n'est appliquée nulle part**, ce qui est un point à corriger *avant* d'exposer un futur endpoint d'import
clients (potentiellement plus sensible qu'un `GET /customers/list` puisqu'il écrirait en masse).

---

## 5. Contraintes techniques pertinentes pour un import en lot

### 5.1 Effets de bord à la création — analysés un par un

- **Email/SMS de bienvenue** : aucun trouvé. Recherche dans `internal/infrastructure/brevo*` : les seuls envois
  identifiés sont liés aux **commandes** (`SendOrderConfirmationToCustomer`,
  [`internal/infrastructure/brevo_mailer/service.go:84`](../internal/infrastructure/brevo_mailer/service.go#L84))
  et aux **factures** (`SendInvoiceEmailToCustomer`,
  [`brevo_mailer/service.go:144`](../internal/infrastructure/brevo_mailer/service.go#L144)) — déclenchés par le
  cycle de vie de la commande, jamais par la création d'une fiche client en tant que telle.
- **Tâche de fond / cron** : aucune tâche dans `internal/tasks/` ne référence la création client ni les colonnes
  RGPD (`advertising_consent`, `last_advertisement_date`) — confirmé par recherche exhaustive, zéro occurrence.
- **Webhook sortant** : aucun identifié — `internal/webhook/` ne contient aucun sous-paquet lié aux clients (il
  couvre Stripe, Uber Eats, Deliveroo, dans le sens entrant uniquement).
- **Effet de bord réel identifié, mais indirect** : la création/mise à jour client déclenche des mises à jour de
  compteurs dénormalisés sur d'autres tables **seulement à la clôture d'une commande** (pas à la création du
  client) via `UpdateLoyaltyFromOrder` — [`repository.go:1353-1519`](../internal/modules/customers/repository.go#L1353-L1519) —
  qui incrémente `customer_nb_orders`/`customer_total_spent` et fait progresser les programmes de fidélité actifs.
  **Sans rapport avec la création elle-même** ; un import de clients n'insérant pas de commandes ne déclenche
  jamais ce chemin.

**Conclusion** : contrairement à ce qu'un import produits doit neutraliser (aucun effet de bord identifié côté
menu non plus, l'audit produit §10.2 le confirme), **un import clients en masse ne devrait déclencher aucun effet
de bord existant** — il n'y a rien à désactiver, car rien n'est câblé sur la création client aujourd'hui. Point
de vigilance inverse : si un futur import veut déclencher un e-mail de bienvenue, **il n'existe aucune
infrastructure existante à réutiliser** pour ce cas précis — elle serait entièrement à construire.

### 5.2 Limite technique déjà documentée (contexte, pas découverte)

Le pool MySQL est plafonné à **1 connexion ouverte + 1 idle**, 3 minutes de durée de vie
(`internal/database/mysql.go`, documenté dans `CLAUDE.md` et déjà cité dans l'audit produit §6). Cette contrainte
s'applique **globalement**, donc également à tout futur import clients : toute preview en lot devra rester
séquentielle (comme les 9 SELECT de la preview produit), et tout commit en lot devra passer par une seule
transaction plutôt que N transactions ouvertes/fermées par client. Rien de spécifique au module `customers` ne
s'ajoute à cette contrainte déjà connue.

### 5.3 Volume et absence de garde-fou

Aucune limite de taille de lot n'existe côté client (pas de constante `maxImportFileSize` équivalente, puisqu'il
n'y a pas d'import aujourd'hui). `SearchCustomers`/`ListCustomers` paginent (`page`, `page_size`), mais rien
n'indique de limite haute sur `page_size` — **[À CLARIFIER]**, non vérifié plus avant (hors périmètre lecture
client pur).

---

## Implications pour la conception de l'import clients

1. Aucune contrainte UNIQUE SQL n'existe sur email/téléphone (scopée ou globale) : toute règle de dédup devra
   être **entièrement applicative**, comme pour les catégories/tags produit — pas d'appui possible sur une
   contrainte DB en cas de course.
2. 3 stratégies de dédup coexistent déjà en prod (téléphone / email / aucune) sur 4 chemins de création
   différents : le modèle canonique import devra choisir **une** clé de dédup explicite, sans compter sur un
   comportement existant unifié à répliquer.
3. `UpdateOrCreateCustomer(ctx, c)` est directement réutilisable dans une transaction englobante (résolution via
   `ctx`, pas de paramètre `tx`) — aucun changement de signature nécessaire pour un commit multi-clients.
4. Email non normalisé à l'écriture (casse) ; téléphone normalisé FR uniquement, sans validation de plausibilité.
5. Les deux champs sont optionnels en base : le modèle canonique devra décider lui-même si import exige l'un des
   deux (aucune règle existante à copier).
6. `advertising_consent` : le défaut SQL (`true`) n'est jamais réellement appliqué par le chemin Go actuel (qui
   écrit `false` par défaut) — l'import devra fixer explicitement cette valeur plutôt que s'appuyer sur un défaut.
7. Aucun endpoint HTTP ni écran front de création/édition client n'existe : pas de patron UI/API à copier
   (contrairement au produit) — tout est à construire, y compris le RBAC (`HasCustomerManagementAccess` existe
   mais n'est appliqué nulle part aujourd'hui sur `/customers/*`).
8. Aucun effet de bord (email, tâche, webhook) n'est câblé sur la création client aujourd'hui : rien à désactiver
   pour un import de masse, mais rien à réutiliser non plus si un besoin de notification apparaît.
