# 11 — Format réel des valeurs `merchant_id` dans les colonnes déjà en varchar

Objectif : déterminer ce que contiennent réellement les colonnes `merchant_id` déjà en `varchar`
(52 tables : `employees`, `kiosks`, `planning_shifts`, `haccp_*`, `booking_*`…) — l'entier
stringifié, ou un format préfixé distinct.

Analyse en lecture seule. Aucun fichier modifié. Fait suite à
[10-merchant-id-type-scope.md](10-merchant-id-type-scope.md).

## Réponse

> **(a) — l'entier stringifié tel quel.** Les colonnes `merchant_id` en `varchar` stockent la
> représentation décimale de `merchant.id`, l'auto-increment MySQL : `"2"`, `"42"`, `"123"`.
> **Aucun préfixage n'existe pour le merchant**, nulle part. Il n'y a ni `merchant_uuid`, ni
> `merchant_code` exploité, ni fonction générant un `mrc_…`.

C'est une bonne nouvelle pour la migration : le passage `integer → varchar` des 40 tables restantes
produira **exactement les mêmes valeurs** que celles déjà en base dans les 52 colonnes varchar. Les
deux moitiés du schéma convergent naturellement, sans transformation ni backfill de valeurs.

## 1. `merchant.id` est un auto-increment nu

[04-schema-postgres-target.sql:1984](04-schema-postgres-target.sql#L1984) :

```sql
CREATE TABLE merchant (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    brand_id varchar(35),
    fullName varchar(50) NOT NULL,
    ...
```

La table `merchant` ne possède **aucune** colonne `uuid`, `slug`, `code`, `public_id` ou `reference`
qui pourrait servir d'identifiant alternatif. Le seul candidat au nom évocateur, la table
`merchant_code` (`code varchar(6)`), est une **orpheline confirmée** :
`grep merchant_code --include=*.go` → **zéro résultat**, cohérent avec
[03-table-usage-audit.md](03-table-usage-audit.md) §2 qui la classe déjà comme morte.

L'ID du merchant exposé au reste du code est donc **l'entier auto-increment brut**, sans habillage.

## 2. Il n'y a pas de préfixe merchant — alors qu'il en existe 60 autres

Le repo possède un système de génération d'ID préfixés bien établi
([internal/helpers/ids.go:64](../../internal/helpers/ids.go#L64)) :

```go
// GeneratePrefixedID generates a unique ID with the given prefix (e.g., "order-xxxx-xxxx").
func GeneratePrefixedID(prefix string) string {
	return prefix + "-" + uuid.New().String()
}
```

**60 préfixes** y sont déclarés — `audit-log`, `avail`, `discount`, `tag`, `receipt`, `user`,
`haccp-tz`, `plan-shift`, `plan-emp`, `kiosk`, `loc`, `flr`, `obs`… ([ids.go:12-60](../../internal/helpers/ids.go#L12)).
**Aucun ne concerne le merchant.** L'absence est d'autant plus parlante que `UserIDPrefix = "user"`
existe, lui : le voisin immédiat du merchant a son préfixe, pas le merchant.

**Distinction essentielle** : ce système sert à générer la **PK propre** des entités récentes —
`kiosks.id` vaut `kiosk-<uuid>`, `planning_shifts.id` vaut `plan-shift-<uuid>`. Il ne s'applique
jamais à `merchant_id`, qui est une **clé étrangère** vers le `merchant.id` historique et en conserve
la forme entière.

## 3. Le chemin réel de la valeur, de la base au varchar

La chaîne est courte et intégralement tracée :

1. **Lecture** — [auth/repository.go:116-127](../../internal/modules/auth/repository.go#L116) :
   `SELECT … ur.merchant_id … FROM users u INNER JOIN users_rights ur ON ur.user_id = u.user_id WHERE ur.token = ?`.
   `users_rights.merchant_id` est **`INTEGER`** dans le schéma actuel (il fait partie des 56 tables à convertir).
2. **Conversion implicite par le driver** — la colonne est scannée dans
   `&data.MerchantID` où `UserLoginRow.MerchantID` est un **`string`**
   ([auth/models.go:149](../../internal/modules/auth/models.go#L149)). `database/sql` convertit
   l'entier en sa représentation décimale : `42` → `"42"`. **C'est ici que naît le format.**
3. **Propagation** — ce `string` circule via `middleware.UserFromContext(ctx)` → `user.MerchantID`
   jusqu'à toutes les écritures.
4. **Écriture en varchar** — sans aucune transformation :
   - [kiosk/repository.go:53-56](../../internal/modules/kiosk/repository.go#L53) —
     `INSERT INTO kiosks (id, merchant_id, …)` : `id` reçoit un `kiosk-<uuid>` généré, `merchantID`
     est passé tel quel. **Les deux formats cohabitent dans la même requête** — c'est l'illustration
     la plus nette de la distinction PK préfixée / FK entière.
   - [planning/schedule/repository.go:324-328](../../internal/modules/planning/schedule/repository.go#L324) —
     `INSERT INTO planning_shifts (id, merchant_id, …)`, `shift.MerchantID` inséré sans conversion.
   - [planning/employees/repository.go:253-259](../../internal/modules/planning/employees/repository.go#L253) —
     `INSERT INTO employees (id, merchant_id, …)`, idem.

Le `varchar` reçoit donc littéralement ce que le driver a produit à l'étape 2 : `"42"`.

## 4. Exemple de valeur réelle observée

Le code non-test contient la valeur en dur, **six fois** —
[deliveroo/handler.go:27, 62, 82, 98, 119, 137](../../internal/modules/deliveroo/handler.go#L98) :

```go
func (h *DeliverooHandler) HandleScenario9(w http.ResponseWriter, r *http.Request) {
	// Extraction des paramètres (ex: /test/scenario9?merchant_id=123&menu_id=menu-abc)
	merchantID := "2"
	menuID := "3"
```

Ce sont des handlers de scénarios de test tapant sur le **sandbox Deliveroo réel** : `"2"` est un
merchant existant. Deux preuves en une :

- la valeur littérale `"2"` — un entier stringifié, pas un `mrc_…` ;
- le commentaire juxtapose `merchant_id=123` (**entier nu**) et `menu_id=menu-abc` (**préfixé**) —
  l'auteur distingue explicitement les deux conventions, dans la même ligne.

### Ne pas se fier aux fixtures de test

Les tests utilisent `"merchant_1"` (153 occurrences) et `"merch-1"`
([auth/pin_test.go:138](../../internal/modules/auth/pin_test.go#L138)). Ce sont des **valeurs
arbitraires de mock `sqlmock`**, jamais confrontées à une vraie base — elles ne prouvent rien sur le
format de production, et leur séparateur (`_`) ne correspond d'ailleurs à aucune convention réelle
du repo (`GeneratePrefixedID` utilise `-`).

### Trois exemples de documentation sont trompeurs

À corriger un jour, car ils suggèrent un format qui n'existe pas et se contredisent entre eux :

| Fichier | Valeur affichée | Réalité |
|---|---|---|
| [docs/ARCHITECTURE_API.md:474](../ARCHITECTURE_API.md) | `"merchant_id": "abc123"` | jamais produit par le code |
| [docs/DELIVERY_DESIGN.md:255](../DELIVERY_DESIGN.md) | `"merchant_id": "m_1"` | préfixe `m_` inexistant |
| [docs/backoffice_requirements/*_API_CONTRACT.md](../backoffice_requirements/) | `"merchant_id": "..."` | élidé |

Un intégrateur lisant `"abc123"` pourrait raisonnablement conclure à un ID opaque et coder en
conséquence. La valeur réelle sérialisée est `"2"`.

## 5. Deux confirmations structurelles

**Les largeurs de colonnes excluent physiquement un UUID préfixé.** Six tables déclarent
`merchant_id varchar(20)` — `bookings_settings`, `configurable_attributes`,
`integration_deliveroo_options_mapping`, `integration_deliveroo_attributes_mapping`,
`integration_uber_eats_options_mapping`, `integration_uber_eats_attributes_mapping` —
et deux autres `varchar(25)` (`app_version_merchant`, `users_devices`). Un ID au format
`GeneratePrefixedID` fait ~42 caractères (`kiosk-` + 36) : il **ne rentrerait pas**. Ces largeurs
n'ont de sens que pour des entiers stringifiés. Le reste s'échelonne en `varchar(30/35/36/50/64)`,
un dimensionnement hétérogène typique du copier-coller entre migrations, sans rapport avec un format
cible.

**Aucune migration ne transforme la valeur.** Recherche de `CAST(`, `CONVERT(`, `SET merchant_id`
dans `migrations/` : **aucun backfill**. Les colonnes `merchant_id varchar` n'ont pas été *converties*
depuis un entier — elles ont été **créées neuves** avec les modules récents (haccp, planning, kiosk,
bookings, availabilities) et alimentées directement par l'applicatif avec le `string` de l'étape 2.
Exemple typique :
[migrations/done/010_haccp_temperature_tranche1.sql:12](../../migrations/done/010_haccp_temperature_tranche1.sql#L12)
(`CHANGE COLUMN restaurant_id merchant_id VARCHAR(64) NOT NULL` — un simple renommage).

*Curiosité sans gravité* :
[migrations/done/003_create_availabilities_tables.sql:10](../../migrations/done/003_create_availabilities_tables.sql#L10)
déclare `merchant_id CHAR(36)` — largeur d'UUID, manifestement recopiée de la ligne
`availability_id CHAR(36)` juste au-dessus. Elle stocke malgré tout `"42"` ; en `CHAR`, MySQL
complète par des espaces mais les retire à la lecture, donc sans effet observable. À normaliser en
`varchar` au passage à Postgres, où la sémantique `char(n)` diffère.

## Conclusion

**(a), sans ambiguïté.** Les colonnes `merchant_id` en `varchar` contiennent l'entier stringifié.
Cinq faits convergents :

1. `merchant.id` est un `integer GENERATED ALWAYS AS IDENTITY`, sans colonne alternative ;
2. aucun `MerchantIDPrefix` parmi les 60 préfixes déclarés, alors que `user`, `kiosk`, `loc` en ont un ;
3. la valeur naît d'un scan d'`INTEGER` vers un champ Go `string` — le driver produit `"42"` ;
4. six littéraux `merchantID := "2"` en code non-test, contre un sandbox réel ;
5. six colonnes `varchar(20)` trop courtes pour héberger un UUID préfixé.

**Conséquence pour la migration** : convertir les 40 tables restantes en `varchar` ne demande
**aucune transformation de données** — un `ALTER … TYPE varchar USING merchant_id::varchar` produit
la valeur déjà présente partout ailleurs. Les 52 colonnes varchar et les 40 colonnes integer
contiennent, et continueront de contenir, le même format.

### Point ouvert, à trancher séparément

Cet audit dit ce que le format **est**, pas ce qu'il **devrait être**. Si l'objectif à terme est un
vrai ID opaque et préfixé pour le merchant (`mrc-<uuid>`, cohérent avec les 60 autres entités), c'est
un **second chantier** — génération, backfill, unicité, compatibilité des clients — à ne pas
confondre avec le simple alignement de type traité ici. Les deux commentaires relevés en
[10-merchant-id-type-scope.md](10-merchant-id-type-scope.md#1-typage-global--string-quasi-partout)
(`// kept as string to match your desired future ids`) suggèrent que cette cible existe dans l'esprit
de l'équipe ; rien dans le code ne l'implémente aujourd'hui.

### Limites

Analyse statique du seul code Go de ce repo. Non vérifiés :
- le contenu **réel** en base (un `SELECT DISTINCT merchant_id FROM employees LIMIT 20` tranchera
  définitivement, et coûte une requête) ;
- les composants externes écrivant en base (ancien back PHP, BI, cron) ;
- la sérialisation côté clients (Flutter, kiosk, scannorder), qui peuvent traiter `merchant_id`
  comme un nombre JSON.
