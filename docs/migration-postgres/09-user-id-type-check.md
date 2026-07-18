# 09 — Vérification `user_id` : `qrcodes`, `services_performed`, `users_devices`

Objectif : vérifier que les colonnes `user_id` de ces 3 tables référencent bien `users.user_id`
(`varchar(64)`), et qu'aucun code Go ne les manipule comme des entiers.

Méthode : lecture seule. Inventaire exhaustif des requêtes SQL Go touchant chaque table, puis
remontée du type Go du paramètre/variable jusqu'à son origine.

Référence : `users.user_id varchar(64) NOT NULL` — [04-schema-postgres-target.sql:3676](04-schema-postgres-target.sql#L3676).

## Résumé

| Table | Type cible | Verdict |
|---|---|---|
| `services_performed.user_id` | `varchar(64) NOT NULL` | **CONFIRMÉ** |
| `users_devices.user_id` | `varchar(64) NOT NULL` | **CONFIRMÉ** |
| `qrcodes.user_id` | `varchar(64)` NULL | **CONFIRMÉ (type)** — mais lien FK non démontrable depuis Go |

Aucun usage `int`/`int64` trouvé sur ces 3 colonnes. Le typage `varchar` est cohérent partout.

## `services_performed.user_id` — CONFIRMÉ

Un seul usage Go, en lecture ; aucun `INSERT`/`UPDATE` côté Go :

- [internal/modules/user_services/repository.go:27](../../internal/modules/user_services/repository.go#L27) — `FROM services_performed sp WHERE sp.user_id = ?`

**Type Go** : `GetCurrentService(ctx, userID, merchantID, deviceID string)` — `userID` est un `string`
([repository.go:18](../../internal/modules/user_services/repository.go#L18)).

**Preuve directe du lien avec `users`** : dans la *même fonction*, la variable `userID` est liée aux
deux colonnes à la fois — `sp.user_id = ?` (ligne 28) puis `u.user_id = ?` sur la table `users`
(ligne 66, requête « cash register »). La même valeur sert donc de clé sur `services_performed.user_id`
et sur `users.user_id` : c'est le même référentiel d'identifiants.

**Origine de la valeur** : `middleware.UserFromContext(ctx)` →
`*auth.UserLoginRow` → champ `UserID string`
([auth/models.go:122](../../internal/modules/auth/models.go#L122)), lui-même peuplé par
`SELECT u.user_id … FROM users u INNER JOIN users_rights ur ON ur.user_id = u.user_id WHERE ur.token = ?`
([auth/repository.go:30 et 116-127](../../internal/modules/auth/repository.go#L116)).
La chaîne remonte donc littéralement à un `users.user_id` lu en base.

**Point d'attention (hors périmètre)** : aucune écriture (`INSERT`/`UPDATE`) de `services_performed`
n'existe côté Go — le clock-in/clock-out est écrit ailleurs (autre service, ou SQL manuel).
La migration ne peut pas s'appuyer sur le code Go pour valider les valeurs *insérées*.

## `users_devices.user_id` — CONFIRMÉ

Trois usages Go :

- [auth/repository.go:630](../../internal/modules/auth/repository.go#L630) — `INSERT INTO users_devices (user_id, …)` (écriture)
- [notification/notification_repository.go:26](../../internal/modules/notification/notification_repository.go#L26) — `SELECT fcm_token FROM users_devices ud` — filtre sur `merchant_id`/`last_used`, **ne touche pas `user_id`**
- [notification/notification_repository.go:53](../../internal/modules/notification/notification_repository.go#L53) — `DELETE … WHERE fcm_token = ?` — **ne touche pas `user_id`**

**Type Go** : `SaveDevice(ctx, userID, merchantID, app, deviceID, fcmToken string)` — `userID` est un
`string` ([auth/repository.go:626](../../internal/modules/auth/repository.go#L626)).

**Origine de la valeur** : appelé en [auth/service.go:676](../../internal/modules/auth/service.go#L676)
avec `user.UserID`, où `user` provient de `s.repo.GetUserByToken(ctx, token)` — c'est-à-dire le même
`UserLoginRow.UserID string` issu de `SELECT u.user_id … FROM users u`. Valeur garantie existante dans
`users` (le token est joint via `users_rights`).

**Note adjacente (hors périmètre)** : `users_devices.merchant_id` est `varchar(25)` alors que
`users.merchant_id` est `integer` ([04-schema-postgres-target.sql:3742](04-schema-postgres-target.sql#L3742)
vs [:3677](04-schema-postgres-target.sql#L3677)). Incohérence réelle mais sur une autre colonne — à
traiter séparément.

## `qrcodes.user_id` — CONFIRMÉ (type) / lien FK non démontrable depuis Go

Inventaire complet des requêtes Go sur `qrcodes` — 4 seulement touchent `user_id`, et **aucune n'y écrit
ni ne la compare à `users`** :

| Emplacement | Usage de `user_id` |
|---|---|
| [pos/create_repository.go:61](../../internal/modules/pos/create_repository.go#L61) | `INSERT INTO qrcodes (merchant_id, code, menu_only, mywelloresto_flag)` — `user_id` **absent → NULL** |
| [kiosk/repository.go:594](../../internal/modules/kiosk/repository.go#L594) | `AND user_id IS NULL` (sentinelle) |
| [scannorder/repository.go:767](../../internal/modules/scannorder/repository.go#L767) | `and qr.user_id IS NULL` (sentinelle) |
| [scannorder/repository.go:798](../../internal/modules/scannorder/repository.go#L798) | `and qr.user_id IS NULL` (sentinelle) |
| [scannorder/repository.go:32](../../internal/modules/scannorder/repository.go#L32) | `SELECT qr.user_id` — **seule lecture de la valeur** |

**Type Go** : `*string` de bout en bout, jamais `int` —
`models.MerchantRow.UserID *string` ([internal/models/request_objects.go:253](../../internal/models/request_objects.go#L253)),
recopié tel quel en [scannorder/service.go:171](../../internal/modules/scannorder/service.go#L171)
vers `QRCode.UserID *string \`json:"user_id"\`` ([scannorder/models.go:62](../../internal/modules/scannorder/models.go#L62)),
puis exposé à l'API. Aucune conversion, aucun `strconv`, aucune arithmétique.

**Pourquoi le type est CONFIRMÉ mais pas le lien FK** : le code Go n'utilise `user_id` que comme
**discriminant NULL / non-NULL**, jamais comme clé de jointure. Le sens métier est documenté en
[kiosk/repository.go:584-585](../../internal/modules/kiosk/repository.go#L584) :

> « récupère le code QR principal du merchant (sans `location_id` ni `user_id`) — c'est le
> `{merchant_slug}` utilisé dans les routes scannorder »

Autrement dit : `user_id IS NULL` = QR générique du restaurant ; `user_id` renseigné = QR rattaché à un
utilisateur (vraisemblablement un serveur, par symétrie avec `location_id` = QR de table). Mais **aucun
code Go ne crée ce cas** ni ne joint `qr.user_id` à `users.user_id` — les lignes concernées, si elles
existent, viennent d'ailleurs (back-office, SQL manuel, ou legacy).

**Conclusion** : rien ne contredit `varchar(64)` — le type Go `*string` nullable est exactement
cohérent avec la cible, et la migration est sûre côté typage. Le `FK candidate : user_id -> users.user_id`
annoté dans [04-schema-postgres-target.sql:3016](04-schema-postgres-target.sql#L3016) reste une hypothèse
raisonnable mais **non vérifiée par le code**. Si une FK réelle doit être créée, valider d'abord en base :

```sql
-- doit retourner 0 ligne avant de poser la contrainte
SELECT q.QR_id, q.user_id
FROM qrcodes q
LEFT JOIN users u ON u.user_id = q.user_id
WHERE q.user_id IS NOT NULL AND u.user_id IS NULL;
```
