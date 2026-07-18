# 13 — Unification `merchant_id` en `varchar(64)` côté schéma Postgres cible

Suite à [12-merchant-id-unification.md](12-merchant-id-unification.md) (conversion Go des sites
`merchant_id int → string`), ce chantier aligne le schéma Postgres cible
([04-schema-postgres-target.sql](04-schema-postgres-target.sql)) : toutes les colonnes `merchant_id`
(et la variante mixed-case `merchant_Id`) passent en `varchar(64)`, quel que soit leur type d'origine
(`integer`, `bigint`, ou `varchar` d'une largeur inférieure). Cible : cohérence avec le reste des
identifiants du schéma, déjà très majoritairement en `varchar(64)`.

Seul le fichier SQL cible Postgres et sa documentation sont concernés. Aucun fichier `.go` touché,
aucune migration MySQL existante modifiée.

## Colonnes modifiées (72)

105 colonnes `merchant_id`/`merchant_Id` existent dans le schéma ; 33 étaient déjà en `varchar(64)`
(dont `haccp_settings.merchant_id` et `kiosk_settings.merchant_id`, citées comme référence dans les
commentaires "FK candidate" du fichier) et n'ont pas été touchées. Les 72 colonnes suivantes ont été
converties :

| Table | Colonne | Ancien type | Nouveau type |
|---|---|---|---|
| `api_request_logs` | `merchant_id` | `bigint` | `varchar(64)` |
| `app_version_merchant` | `merchant_id` | `varchar(25)` | `varchar(64)` |
| `audit_logs` | `merchant_id` | `varchar(36)` | `varchar(64)` |
| `availabilities` | `merchant_id` | `varchar(50)` | `varchar(64)` |
| `average_distribution_time` | `merchant_id` | `integer` | `varchar(64)` |
| `average_distribution_time_by_category` | `merchant_id` | `integer` | `varchar(64)` |
| `average_distribution_time_history` | `merchant_id` | `integer` | `varchar(64)` |
| `barcodes` | `merchant_id` | `integer` | `varchar(64)` |
| `bookings` | `merchant_id` | `integer` | `varchar(64)` |
| `bookings_settings` | `merchant_id` | `varchar(20)` | `varchar(64)` |
| `cash_desks` | `merchant_id` | `integer` | `varchar(64)` |
| `cash_registers` | `merchant_id` | `integer` | `varchar(64)` |
| `cash_registers_custom_items` | `merchant_id` | `varchar(35)` | `varchar(64)` |
| `cash_reports` | `merchant_id` | `integer` | `varchar(64)` |
| `category_discount` | `merchant_id` | `integer` | `varchar(64)` |
| `components` | `merchant_id` | `integer` | `varchar(64)` |
| `component_category` | `merchant_id` | `integer` | `varchar(64)` |
| `configurable_attributes` | `merchant_id` | `varchar(20)` | `varchar(64)` |
| `consumables` | `merchant_id` | `integer` | `varchar(64)` |
| `customer` | `merchant_id` | `integer` | `varchar(64)` |
| `customer_loyalty_programs` | `merchant_id` | `varchar(30)` | `varchar(64)` |
| `delivery_session` | `merchant_id` | `integer` | `varchar(64)` |
| `discounts` | `merchant_id` | `integer` | `varchar(64)` |
| `employment_agreement` | `merchant_id` | `integer` | `varchar(64)` |
| `employment_contract` | `merchant_id` | `integer` | `varchar(64)` |
| `expiration_dates` | `merchant_id` | `integer` | `varchar(64)` |
| `extra` | `merchant_id` | `integer` | `varchar(64)` |
| `floors` | `merchant_id` | `integer` | `varchar(64)` |
| `hours_of_operation` | `merchant_id` | `integer` | `varchar(64)` |
| `integration_deliveroo` | `merchant_id` | `integer` | `varchar(64)` |
| `integration_deliveroo_attributes_mapping` | `merchant_id` | `varchar(20)` | `varchar(64)` |
| `integration_deliveroo_components_mapping` | `merchant_id` | `integer` | `varchar(64)` |
| `integration_deliveroo_options_mapping` | `merchant_id` | `varchar(20)` | `varchar(64)` |
| `integration_deliveroo_products_mapping` | `merchant_id` | `integer` | `varchar(64)` |
| `integration_uber_direct` | `merchant_id` | `integer` | `varchar(64)` |
| `integration_uber_eats` | `merchant_id` | `integer` | `varchar(64)` |
| `integration_uber_eats_attributes_mapping` | `merchant_id` | `varchar(20)` | `varchar(64)` |
| `integration_uber_eats_components_mapping` | `merchant_id` | `integer` | `varchar(64)` |
| `integration_uber_eats_options_mapping` | `merchant_id` | `varchar(20)` | `varchar(64)` |
| `integration_uber_eats_products_mapping` | `merchant_id` | `integer` | `varchar(64)` |
| `invoices` | `merchant_id` | `integer` | `varchar(64)` |
| `locations` | `merchant_id` | `integer` | `varchar(64)` |
| `merchant_code` | `merchant_id` | `integer` | `varchar(64)` |
| `merchant_parameters` | `merchant_id` | `integer` | `varchar(64)` |
| `merchant_sms_monthly` | `merchant_id` | `varchar(50)` | `varchar(64)` |
| `orderitems` | `merchant_id` | `integer` | `varchar(64)` |
| `orders` | `merchant_id` | `integer` | `varchar(64)` |
| `payments` | `merchant_id` | `integer` | `varchar(64)` |
| `pictures` | `merchant_id` | `integer` | `varchar(64)` |
| `planned_shifts` | `merchant_id` | `integer` | `varchar(64)` |
| `planning_roles` | `merchant_id` | `integer` | `varchar(64)` |
| `productcateg` | `merchant_id` | `integer` | `varchar(64)` |
| `products` | `merchant_Id` (mixed-case) | `integer` | `varchar(64)` |
| `purchased_components` | `merchant_id` | `integer` | `varchar(64)` |
| `qrcodes` | `merchant_id` | `integer` | `varchar(64)` |
| `receipts` | `merchant_id` | `integer` | `varchar(64)` |
| `recipes` | `merchant_id` | `integer` | `varchar(64)` |
| `restaurant_ticket` | `merchant_id` | `integer` | `varchar(64)` |
| `scannorder_settings` | `merchant_id` | `integer` | `varchar(64)` |
| `services_performed` | `merchant_id` | `integer` | `varchar(64)` |
| `shift_templates` | `merchant_id` | `integer` | `varchar(64)` |
| `stock_movements` | `merchant_id` | `varchar(50)` | `varchar(64)` |
| `stripe_accounts` | `merchant_id` | `integer` | `varchar(64)` |
| `subscriptions` | `merchant_id` | `integer` | `varchar(64)` |
| `subscription_invoices` | `merchant_id` | `integer` | `varchar(64)` |
| `tags` | `merchant_id` | `varchar(35)` | `varchar(64)` |
| `users` | `merchant_id` | `integer` | `varchar(64)` |
| `users_devices` | `merchant_id` | `varchar(25)` | `varchar(64)` |
| `users_nfc_tags` | `merchant_id` | `integer` | `varchar(64)` |
| `users_rights` | `merchant_id` | `integer` | `varchar(64)` |
| `welloresto_stripe_customers` | `merchant_id` | `integer` | `varchar(64)` |
| `without` | `merchant_id` | `integer` | `varchar(64)` |

Cas particulier : `products.merchant_Id` est l'identifiant mixed-case déjà signalé en commentaire
au-dessus de la table (PG le replie en `merchant_id` en usage non quoté) ; seul son type change, la
casse du nom est conservée à l'identique. Il fait partie de la clé primaire composite
`PRIMARY KEY (product_id, merchant_Id)`, qui référence le nom de colonne (inchangé) et n'a donc pas
eu besoin d'édition séparée.

## CHECK `>= 0` sur colonnes `merchant_id` (UNSIGNED d'origine)

Recherche de toute occurrence `CHECK (merchant_id ...)` dans le fichier : **aucune trouvée**. Les
`CHECK (col >= 0)` générés par la règle "INT/BIGINT UNSIGNED" du fichier (voir en-tête du schéma)
n'ont jamais été appliqués aux colonnes `merchant_id` — seules `id`, `delivery_session_id`,
`current_order_id`, `order_rating_id` et `rating`/`delivery_rating` en portent dans ce fichier. Rien
à supprimer pour cette étape.

## Incohérence de type sur les FK candidates commentées

Chaque table du fichier porte, en commentaire au-dessus de son `CREATE TABLE`, une liste
`FK candidate (non creee) : merchant_id -> ...` qui inclut par transitivité `merchant.id`. Cette
liste elle-même n'a pas été réécrite (elle énumère des noms de colonnes, pas des types), mais un
avertissement a été ajouté au-dessus de `CREATE TABLE merchant` (ligne ~1983) : `merchant.id` reste
`integer GENERATED ALWAYS AS IDENTITY` — il n'est **pas** concerné par cette unification, puisque
c'est la colonne auto-increment de la table `merchant` elle-même, pas une colonne `merchant_id` qui
la référence. Toutes les colonnes `merchant_id` du reste du schéma étant maintenant `varchar(64)`,
une éventuelle FK future entre l'une d'elles et `merchant.id` nécessiterait un cast explicite
(`merchant.id::varchar`) — ou, plus probablement, la conversion de `merchant.id` lui-même dans un
chantier séparé (hors périmètre ici).

## Validation syntaxique

Fichier revalidé avec un vrai parseur Postgres (`pglast` v8.2, binding Python de `libpg_query`) :

```
python3 -c "
import pglast
with open('docs/migration-postgres/04-schema-postgres-target.sql', encoding='utf-8') as f:
    sql = f.read()
stmts = pglast.parse_sql(sql)
print('PARSE OK -', len(stmts), 'statements')
"
→ PARSE OK - 453 statements
```

Baseline (avant modification) déjà validée avec le même parseur avant toute édition — même résultat
"PARSE OK", confirmant que les substitutions n'ont pas cassé la syntaxe.

## Récapitulatif

- **72 colonnes** converties de `integer`/`bigint`/`varchar(20|25|30|35|36|50)` vers `varchar(64)`.
- **33 colonnes** déjà en `varchar(64)` avant ce chantier, non modifiées (dont `haccp_settings` et
  `kiosk_settings`, qui servaient de référence "cible" dans les commentaires FK candidate).
- **0 CHECK** `merchant_id >= 0` à supprimer (aucun n'existait).
- **1 commentaire** ajouté au-dessus de `CREATE TABLE merchant` pour documenter l'incohérence de
  type entre les `merchant_id varchar(64)` du reste du schéma et `merchant.id integer`.
- Aucun fichier `.go` touché, aucune migration MySQL modifiée.
- Validation `pglast` : **OK**, 453 statements, aucune erreur de parsing.
