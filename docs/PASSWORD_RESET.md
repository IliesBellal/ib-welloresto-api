# Réinitialisation de mot de passe (« Mot de passe oublié »)

Document vivant : implémentation, process d'utilisation, et **journal des décisions** tenu au fil de
l'eau (y compris les décisions annulées et pourquoi). Mis à jour à chaque étape livrée.

- **Démarré le** : 2026-08-02
- **Périmètre** : API Go (`ib-welloresto-api`), back-office React (`wello-back-office`), POS Flutter (`wello_resto_flutter`)
- **État** : ✅ **7/7 livrées** — voir [§6 Avancement](#6-avancement)
- **Reste à faire avant production** : appliquer la migration `078` en prod, définir
  `PASSWORD_RESET_BASE_URL`, et un contrôle visuel des écrans back-office et POS

---

## 1. Contexte : ce qui existait avant

Aucun flux de récupération de mot de passe n'existait dans le produit. L'audit initial a relevé :

| Constat | Emplacement |
|---|---|
| Aucune route publique de récupération | Bloc `/auth` de [routes.go](../cmd/api/routes.go) — uniquement `login`, MFA, PIN |
| Aucune table ni token de reset | Aucun `password_reset` / `reset_token` dans `*.go`, `*.sql`, `*.md` |
| Aucun template email de reset | [brevo_mailer/service.go](../internal/infrastructure/brevo_mailer/service.go) |
| Lien « Mot de passe oublié ? » **non branché** | [reinit_password_dialog.dart](../../wello_resto_flutter/lib/ui/widgets/dialogs/reinit_password_dialog.dart) — `onPressed: () {}`, champ sans controller, titre « Nouveau compte » |
| Aucun lien sur la page de login back-office | [Login.tsx](../../wello-back-office/src/pages/Login.tsx) |

Les deux endpoints existants exigent **tous les deux** d'être déjà authentifié, et ne couvrent donc
pas le cas « j'ai perdu mon mot de passe » :

- `PATCH /users/reset-password` — changement avec `old_password` (self-service, connecté)
- `POST /users/{id}/force-reset-password` — un admin impose un mot de passe (RBAC `IsAdmin`)

Le seul chemin de récupération réel aujourd'hui est donc : **passer par un administrateur**.

---

## 2. Journal des décisions

### D1 — Canal : lien par email, pas OTP dans le POS
**Décision** : la demande peut partir du POS ou du back-office, mais la réinitialisation se fait
toujours via un lien email qui ouvre le **back-office**.
**Pourquoi** : l'email est le seul identifiant fiable et déjà vérifié (`users.email_verified_at`) ;
un second écran de saisie sur tablette POS double la surface de code pour un gain faible. Le POS se
contente d'afficher « consultez vos emails ».
**Réversible** : un flux OTP à 6 chiffres dans le POS reste possible en V2, l'infra OTP existe déjà
(`SendVerificationCode`).

### D2 — ~~Stockage du token en Redis~~ → **abandonnée**
**Décision initiale** : stocker le token en Redis avec TTL, comme le fait l'OTP MFA existant
([auth/service.go](../internal/modules/auth/service.go)) — argument avancé : « zéro migration ».
**Annulée le 2026-08-02** après revue de [redis/client.go](../internal/infrastructure/redis/client.go) :

- `Get` (l. 47-62) journalise les erreurs mais renvoie `("", false)` → une panne Redis rendrait
  **tous** les liens « invalides ou expirés », sans distinction avec un vrai token périmé ;
- `Delete` (l. 94-100) fait `_ = c.rdb.Del(...).Err()` et **renvoie `true` inconditionnellement** →
  l'usage unique, qui devait reposer sur ce `Delete`, n'était **pas garanti même Redis en bon état** :
  un échec silencieux laissait le lien rejouable jusqu'à expiration du TTL.

Le second point est un défaut de conception, indépendant de toute panne. C'est lui qui a tranché.

### D3 — Postgres est la source de vérité, Redis n'est qu'un confort
**Décision** : la validité et l'usage unique du token vivent dans la table `password_resets`.

| Besoin | Où | Si Redis tombe |
|---|---|---|
| Validité / usage unique du token | **Postgres** | Aucun impact |
| Rate limit par compte (5/h) | **Postgres** — `COUNT(*)` | Aucun impact |
| Throttle par IP | Redis, best-effort | Dégradé ; la limite par compte tient |

**Pourquoi ce partage** : Redis est un cache best-effort partout dans ce projet, jamais une source de
vérité — `GetUserByToken` retombe d'ailleurs sur la DB en cas de miss. Un throttle qui saute
temporairement est acceptable ; un token rejouable ne l'est pas.

### D4 — Usage unique garanti par un CAS SQL atomique
**Décision** : consommation par
```sql
UPDATE password_resets SET used_at = now()
WHERE token_hash = ? AND used_at IS NULL AND expires_at > now()
```
puis contrôle `RowsAffected == 1`. Pas de `SELECT` préalable suivi d'un `UPDATE`.
**Pourquoi** : un `SELECT` puis `UPDATE` laisse une fenêtre de concurrence (deux clics simultanés sur
le même lien). Le CAS est atomique : un seul gagne. **Vérifié en exécution** — voir [§5](#5-vérifications-effectuées).

### D5 — Token jamais stocké en clair
**Décision** : seul le `sha256` hex (64 caractères) du token est persisté ; le token en clair
n'existe que dans l'email.
**Pourquoi** : une fuite de la table (dump, log, accès lecture) ne permet pas de forger un lien valide.

### D6 — `varchar(64)` et non `char(64)` pour `token_hash`
**Décision** : `varchar`, bien que la longueur soit fixe.
**Pourquoi** : en Postgres `char(n)` est un `bpchar` complété par des espaces, à la sémantique de
comparaison piégeuse sur les espaces significatifs. Et tout le reste du schéma cible utilise `varchar`.
*(Écart assumé par rapport au plan initial, qui annonçait `CHAR(64)`.)*

### D7 — Un index sur `created_at` malgré une table petite
**Décision** : trois index, dont `idx_password_resets_created_at` pour la purge quotidienne.
**Pourquoi** : le pool est plafonné à **1 connexion ouverte**
([postgres.go](../internal/database/postgres.go)) ; un `DELETE` en seq scan bloquerait l'unique
connexion de l'API pendant toute sa durée.

### D8 — DDL Postgres uniquement, pas de jumeau MySQL
**Décision** : la migration `078` est écrite en DDL Postgres natif seulement.
**Pourquoi** : confirmé avec le demandeur — la base réelle tourne désormais sur Postgres. C'est la
première table du dépôt **jamais passée par MySQL**.
**Vigilance** : `DB_DIALECT` vaut encore `mysql` **par défaut**
([dialect.go](../internal/database/dbx/dialect.go)). Le code Go doit rester dialecte-agnostique :
placeholders `?` + `dbx.GetDB` (qui rebinde en `$N`), et `dbx.UTCNow()` plutôt qu'un `now()` en dur.

### D9 — Aucune clé étrangère
**Décision** : pas de FK `user_id → users.user_id`, seulement un commentaire de candidate.
**Pourquoi** : convention du chantier de migration Postgres (« pas de nouvelles FK »).

### D11 — `UPDATE ... RETURNING`, donc Postgres uniquement
**Décision** : la consommation du token est un seul `UPDATE ... RETURNING user_id`, sans `SELECT`
préalable.
**Pourquoi** : un aller-retour de moins sur un pool plafonné à 1 connexion, et surtout aucune fenêtre
de concurrence. `RETURNING` n'existe pas en MySQL, mais la table non plus (D8) — la fonctionnalité est
Postgres-only par construction.

### D12 — Un échec de rotation de session ne fait pas échouer la requête
**Décision** : si `RotateRightsTokensForUser` échoue **après** que le mot de passe a été changé, on
journalise en `Error` et on renvoie quand même un succès.
**Pourquoi** : à ce stade le mot de passe **est** modifié. Renvoyer une erreur ferait croire à un
échec et pousserait l'utilisateur à réessayer avec un token désormais consommé — il serait bloqué
alors que son mot de passe a changé. Le cas est très improbable (une panne DB aurait déjà fait
échouer l'`UPDATE` précédent) et le log le rend visible.
**Compromis assumé** : dans ce cas résiduel, les sessions existantes survivent.

### D13 — Politique de mot de passe centralisée dans `helpers`
**Décision** : `helpers.HashUserPassword` + `helpers.ValidatePassword` deviennent la source unique ;
`users.HashPassword` et `users.validateNewPassword` délèguent, en gardant leur nom (aucun appelant
modifié).
**Pourquoi** : `auth` **ne peut pas** importer `users` — [admin_service_test.go](../internal/modules/users/admin_service_test.go)
importe déjà `auth`, la dépendance inverse créerait un cycle à la compilation des tests.

**⚠️ Anomalie découverte au passage, non corrigée** : il existait déjà **deux** fonctions de hachage
avec des coûts bcrypt différents —

| Fonction | Coût | Utilisée par |
|---|---|---|
| `users.HashPassword` | **12** | création d'utilisateur, changement self-service, force-reset admin |
| `helpers.HashPassword` | **10** (`bcrypt.DefaultCost`) | [auth/repository.go](../internal/modules/auth/repository.go) — ré-encodage des mots de passe *legacy* au login |

Un compte migré depuis l'ancien format se retrouve donc hashé en coût 10, plus faible que les comptes
créés normalement. Laissé tel quel : ce chemin est celui du **login**, hors périmètre de ce ticket.
À traiter séparément.

### D16 — Purge quotidienne à 5h, rétention 7 jours
**Décision** : `add("0 5 * * *", taskManager.CleanupExpiredPasswordResets)` dans
[cmd/api/tasks.go](../cmd/api/tasks.go), suppression des lignes de plus de 7 jours.

**Pourquoi 5h** : les créneaux nocturnes voisins sont déjà pris — 2h (produits populaires), 3h
(patterns upsell), et 4h le 1er du mois (purge des suggestions upsell, seul autre job de purge du
projet). Deux `DELETE` simultanés se disputeraient l'unique connexion DB.

**Pourquoi 7 jours** alors qu'un lien ne vit que 30 minutes : les lignes servent aussi au rate limit
par compte (fenêtre glissante d'1 heure) et au diagnostic support (« je n'ai jamais reçu l'email »,
avec `requested_ip`). Une semaine couvre les deux sans laisser la table grossir.

**Date de coupure calculée en Go, pas en SQL** : `INTERVAL '7 days'` (Postgres) et `DATE_SUB` (MySQL)
n'ont pas de syntaxe commune, un paramètre si. Cohérent avec la neutralité de dialecte imposée par
`dbx` (D8).

### D14 — Le throttle par IP lit `X-Forwarded-For`, et n'est qu'un throttle
**Décision** : `helpers.ClientIP` prend l'entrée la plus à gauche de `X-Forwarded-For`, avec repli sur
`RemoteAddr`.
**Pourquoi** : l'API tourne derrière un reverse proxy, où `RemoteAddr` est l'adresse du proxy — la
même pour tout le monde, donc inutilisable pour compter.
**Limite assumée** : `X-Forwarded-For` est falsifiable par le client. C'est acceptable **parce que**
ce compteur n'est pas la frontière de sécurité : celle-ci est la limite par compte en SQL (D3). Le
compteur Redis est en lecture-modification-écriture (pas d'`INCR` atomique exposé par le wrapper), il
peut donc sous-compter en concurrence — même raisonnement.

### D15 — `PASSWORD_RESET_BASE_URL` absente ⇒ échec silencieux, journalisé
**Décision** : sans URL de base configurée (ou sans mailer), le token est bien créé mais aucun email
ne part ; on journalise en `Error` et on renvoie un succès.
**Pourquoi** : la réponse de `forgot-password` doit être invariante (anti-énumération) ; renvoyer une
erreur ici distinguerait « compte existant mais serveur mal configuré » de « compte inexistant ». Le
log `Error` est le canal de signalement.

### D10 — Invalidation de session : rotation en base, pas seulement purge Redis
**Découverte** : `GetUserByToken` filtre sur `WHERE ur.token = ?` **en base**
([repository.go](../internal/modules/auth/repository.go)). Supprimer la seule clé Redis ne déconnecte
donc personne : la requête suivante relit la DB et re-remplit le cache.
**Décision** : sur reset réussi, **régénérer `users_rights.token`**, *puis* purger les clés Redis.

**⚠️ Correction d'une affirmation antérieure de ce document.** Il y était écrit que
`ForceResetPassword` « n'invalide aucune session ». **C'est faux**, et la vérification du code l'a
montré à l'étape 4 : `UsersRepository.UpdatePassword` rotationne bien `users_rights.token` — mais
seulement `WHERE user_id = ? AND merchant_id = ?`, c'est-à-dire **pour le merchant connecté
uniquement**.

Le défaut réel est donc plus étroit que décrit : pour un utilisateur rattaché à **plusieurs
merchants**, les sessions ouvertes chez les autres merchants survivaient au changement de mot de
passe — alors que `users.password` est global. Pour un utilisateur mono-merchant (le cas courant),
le comportement était déjà correct, et la réponse `"tokens_invalidated": true` était exacte.

**Corrigé à l'étape 4** via `RotateRightsTokensExcept`, appliqué aux deux chemins :
`ForceResetPassword` (rotation de tous les autres liens) et le changement self-service
`UpdatePassword` (idem, en excluant le lien courant pour que l'appelant conserve la session qu'il est
en train d'utiliser). Le flux « mot de passe oublié » de l'étape 2, lui, rotationne **tous** les
liens : personne ne reste connecté.

---

## 3. Architecture cible

```
POS Flutter  ──┐
               ├──> POST /auth/forgot-password  {login}     -> toujours 200
Back-office  ──┘                                   │
                                                   ├─ INSERT password_resets (token_hash, expires_at)
                                                   └─ email Brevo avec le token en clair
                                                            │
Back-office  ─────> POST /auth/reset-password {token, new_password}
                                                   ├─ UPDATE ... WHERE used_at IS NULL  (CAS)
                                                   ├─ UPDATE users.password (bcrypt)
                                                   └─ rotation users_rights.token + purge Redis
```

### Schéma `password_resets`

| Colonne | Type | Rôle |
|---|---|---|
| `id` | `varchar(64)` PK | |
| `user_id` | `varchar(64)` NOT NULL | |
| `token_hash` | `varchar(64)` NOT NULL **UNIQUE** | sha256 hex ; clé de lookup |
| `expires_at` | `timestamptz` NOT NULL | TTL 30 min |
| `used_at` | `timestamptz` NULL | `NULL` = non consommé ; garantit l'usage unique |
| `requested_ip` | `varchar(45)` NULL | IPv6 max ; traçabilité + rate limit de secours |
| `created_at` | `timestamptz` NOT NULL DEFAULT `now()` | rate limit par compte |

Index : `uq_password_resets_token_hash` (UNIQUE), `idx_password_resets_user_created`,
`idx_password_resets_created_at`.

### Surface de code (étape 2, livrée)

| Élément | Emplacement |
|---|---|
| `GetUserForPasswordReset`, `CountPasswordResetsSince`, `InsertPasswordReset`, `ConsumePasswordResetToken`, `RotateRightsTokensForUser` | [auth/repository.go](../internal/modules/auth/repository.go) |
| `RequestPasswordReset`, `ConfirmPasswordReset`, `hashResetToken` | [auth/service.go](../internal/modules/auth/service.go) |
| `PasswordResetTTL` (30 min), `PasswordResetMaxPerHour` (5), `ErrInvalidResetToken`, DTOs | [auth/models.go](../internal/modules/auth/models.go) |
| `HashUserPassword`, `ValidatePassword`, `PasswordBcryptCost` | [helpers/password.go](../internal/helpers/password.go) |

`RequestPasswordReset` renvoie un `*PasswordResetIssue` (utilisateur + token en clair + expiration),
ou `(nil, nil)` quand aucun email ne doit partir. L'envoi de l'email est volontairement laissé à
l'appelant (étape 3) : le service reste ainsi sans dépendance au `mailer`, donc testable seul.
**Le token en clair ne doit jamais être journalisé ni renvoyé dans une réponse HTTP.**

### Surface de code (étape 3, livrée)

| Élément | Emplacement |
|---|---|
| `ForgotPassword`, `ResetPassword` | [auth/handler.go](../internal/modules/auth/handler.go) |
| `SendPasswordResetLink`, `tooManyResetRequestsFromIP` | [auth/service.go](../internal/modules/auth/service.go) |
| Routes publiques `/auth/forgot-password`, `/auth/reset-password` | [routes.go](../cmd/api/routes.go) |
| `SendPasswordReset` (interface + implémentation Brevo) | [mailer/service.go](../internal/infrastructure/mailer/service.go), [brevo_mailer/service.go](../internal/infrastructure/brevo_mailer/service.go) |
| Template email | [password_reset.html](../internal/infrastructure/mailer/templates/password_reset.html) |
| `PASSWORD_RESET_BASE_URL` | [config/auth.go](../internal/config/auth.go) |
| `ClientIP` | [helpers/handler_helpers.go](../internal/helpers/handler_helpers.go) |
| Throttle IP (`PasswordResetIPThrottle*`, 20/h) | [models/redis_models.go](../internal/models/redis_models.go) |

### Surface de code (étape 6, livrée — dépôt `wello-back-office`)

| Élément | Emplacement |
|---|---|
| `forgotPassword`, `resetPassword` (via `apiClient`, `skipAuth: true`) | `src/services/authService.ts` |
| Écran de demande | `src/pages/ForgotPassword.tsx` |
| Écran de saisie du nouveau mot de passe | `src/pages/ResetPassword.tsx` |
| Routes publiques `/forgot-password`, `/reset-password` | `src/App.tsx` (hors `ProtectedRoute`) |
| Lien « Mot de passe oublié ? » | `src/pages/Login.tsx` |
| Correctif de l'appel cassé | `src/components/settings/ChangePasswordDialog.tsx` |

**`PASSWORD_RESET_BASE_URL` doit pointer sur `<back-office>/reset-password`** — c'est la route
attendue par le lien de l'email.

### Surface de code (étape 7, livrée — dépôt `wello_resto_flutter`)

Les trois couches habituelles du POS (`Api` → `Service` → `Controller`), plus le dialogue :

| Élément | Emplacement |
|---|---|
| `ForgotPasswordPayload` | `lib/data/api/payload/authentication/forgot_password_payload.dart` |
| `AuthenticationApi.forgotPassword` | `lib/data/api/authentication_api.dart` |
| `AuthenticationService.forgotPassword` | `lib/data/services/authentication_service.dart` |
| `AuthenticationController.requestPasswordReset` | `lib/controllers/authentication_controller.dart` |
| Dialogue réécrit | `lib/ui/widgets/dialogs/reinit_password_dialog.dart` |

Le POS **déclenche uniquement l'email** ; la saisie du nouveau mot de passe se fait sur le
back-office (D1). L'état de succès affiche « si un compte correspond » et précise que le lien
s'ouvre sur un ordinateur.

### Contrat API

| Route | Auth | Réponse |
|---|---|---|
| `POST /auth/forgot-password` `{login}` | publique | **toujours `200`** |
| `POST /auth/reset-password` `{token, new_password}` | publique | `200` / `400 invalid_or_expired_token` |

`login` accepte le nom d'utilisateur **ou** l'email, en reprenant à l'identique le `WHERE` du login
existant (`UPPER(u.name) = UPPER(?) OR UPPER(u.email) = UPPER(?)`) — pour que « ce qui marche au
login marche ici ».

Le `200` systématique sur `forgot-password` est délibéré : répondre `404` sur un compte inconnu
transformerait l'endpoint en oracle d'énumération de comptes.

---

## 4. Process d'utilisation

### Pour l'utilisateur final
1. Cliquer « Mot de passe oublié ? » sous le bouton de connexion du back-office (ou sur le POS).
2. Saisir son identifiant **ou** son email → message neutre : *« Si un compte correspond, un email de
   réinitialisation vient de partir. »* Ce message s'affiche **même pour un compte inexistant**, par
   conception (anti-énumération) — le support doit le savoir pour ne pas en conclure que le compte existe.
3. Ouvrir le lien reçu (**valable 30 minutes, utilisable une seule fois**) → écran de nouveau mot de passe.
4. Le nouveau mot de passe fait **8 caractères minimum**. Un mot de passe refusé **ne consomme pas** le
   lien : on corrige et on revalide avec la même URL.
5. Toutes les sessions ouvertes sont fermées, POS compris : il faut se reconnecter partout. La page
   redirige ensuite vers la connexion.

Si la messagerie a tronqué le lien (jeton absent de l'URL), l'écran affiche « Lien incomplet » et
propose d'en redemander un, au lieu de laisser saisir un mot de passe pour rien.

### Pour l'administrateur / le support
- Un utilisateur ne reçoit rien → vérifier que `users.email` est renseigné, puis le rate limit
  (5 demandes/heure/compte) ; les demandes sont tracées dans `password_resets` avec `requested_ip`.
- Besoin d'un déblocage immédiat → `POST /users/{id}/force-reset-password` (RBAC `IsAdmin`) reste
  disponible, et ferme désormais les sessions de **tous** les merchants de l'utilisateur (étape 4).
- Diagnostic d'un lien : `SELECT used_at, expires_at FROM password_resets WHERE token_hash = <sha256 du token>`.
  Un `used_at` non nul = lien déjà consommé (comportement normal, pas un bug).

### Pour l'exploitation
- Purge : job cron **quotidien à 5h**, supprimant les lignes de plus de **7 jours**
  ([tasks/password_resets.go](../internal/tasks/password_resets.go)). Journalise `deleted` à chaque
  passage. Les crons tournent sur **tous** les environnements, staging compris.
- Panne Redis : le flux **reste fonctionnel** ; seul le throttle par IP est dégradé.
- **Variable requise : `PASSWORD_RESET_BASE_URL`** — URL de base du back-office, ex.
  `https://backoffice.welloresto.fr/reset-password`. Le flux y ajoute `?token=<token>`.
  **Sans elle, aucun email ne part** : la demande répond quand même `200` (anti-énumération) et un log
  `Error` « PASSWORD_RESET_BASE_URL is not configured » est émis. À surveiller après déploiement.
- Limites en vigueur : **5 demandes/heure/compte** (SQL, fiable) et **20/heure/IP** (Redis, best-effort).

---

## 5. Vérifications effectuées

Étape 1, exécutée contre `postgres:16` (conteneur `welloresto-postgres-dev`) le 2026-08-02 :

| Test | Résultat |
|---|---|
| Application de `078_..._up.sql` | ✅ `CREATE TABLE` + 3 `CREATE INDEX` + 4 `COMMENT` |
| Rejeu de l'`up` (idempotence) | ✅ `NOTICE ... already exists, skipping`, aucune erreur |
| `down` puis vérification | ✅ `DROP TABLE`, table `ABSENTE` |
| Re-`up` après `down` | ✅ |
| CAS de consommation, 1er passage | ✅ `UPDATE 1` |
| CAS rejoué sur le même token | ✅ `UPDATE 0` — **usage unique confirmé** |
| CAS sur un token expiré | ✅ `UPDATE 0` |
| Doublon de `token_hash` | ✅ rejeté par `uq_password_resets_token_hash` |
| Plan du lookup de consommation | ✅ `Index Only Scan using uq_password_resets_token_hash` |

Étape 2 — [`password_reset_integration_test.go`](../internal/modules/auth/password_reset_integration_test.go),
exécuté contre le Postgres de dev :

```bash
DB_DIALECT=postgres POSTGRES_URL="postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev" \
  go test -tags postgres_integration ./internal/modules/auth/... -run TestPasswordReset_Postgres -v
```

| Sous-test | Vérifie |
|---|---|
| `lookup` | Résolution par nom, par email, insensible à la casse ; compte inconnu → `nil` |
| `disabled_account_is_not_eligible` | Un compte désactivé n'obtient aucun lien |
| `weak_password_does_not_burn_the_token` | Un mot de passe refusé laisse `used_at` à `NULL` |
| `nominal_flow` | Token stocké **hashé**, mot de passe changé, ancien refusé, `users_rights.token` **rotationné**, rejeu rejeté |
| `expired_token` | → `ErrInvalidResetToken` |
| `unknown_token` | Token inconnu et token vide → `ErrInvalidResetToken` |
| `per-account_rate_limit` | 5 demandes passent, la 6ᵉ est silencieusement ignorée |

**7/7 verts.**

Étape 3 — deux tests supplémentaires dans le même fichier, plus un test de template :

| Test | Vérifie |
|---|---|
| `TestSendPasswordResetLink_Postgres/sends_a_usable_link` | L'email part avec le bon destinataire, prénom et durée ; **le token extrait de l'URL réinitialise réellement le mot de passe** |
| `.../unknown_login_sends_nothing_but_does_not_error` | Login inconnu → 0 email, 0 erreur |
| `.../missing_base_URL_sends_nothing_but_does_not_error` | Sans `PASSWORD_RESET_BASE_URL` → 0 email, 0 erreur (D15) |
| `.../per-IP_throttle` | Bloque au-delà de 20/h ; une autre IP n'est pas affectée |
| `TestPasswordResetHandlers_Postgres` | **Réponse HTTP octet pour octet identique** entre compte existant et inexistant ; 400 sur corps malformé ; 400 `invalid_or_expired_token` ; mot de passe trop court → 400 **sans brûler le lien** ; le même lien fonctionne ensuite → 200 ; rejeu → 400 |
| [`TestRenderPasswordResetTemplate`](../internal/infrastructure/mailer/password_reset_template_test.go) | Le template rend le lien, le prénom et l'expiration, et ne contient pas `<no value>` |

**11/11 verts** sur les trois tests d'intégration, `go build ./...` OK.

Étape 4 — rotation multi-merchant :

| Test | Vérifie |
|---|---|
| [`TestRotateRightsTokensExcept_Postgres`](../internal/modules/users/postgres_integration_test.go) | 3 merchants : les liens B et C sont rotationnés et retournés, A est intact ; deux liens ne reçoivent jamais le même token ; `exceptMerchantID` vide rotationne tout |
| `TestUsersServiceForceResetPassword` (sqlmock, étendu) | La rotation des autres merchants est bien appelée |
| `TestUsersRepository_Postgres` | **Ne compilait même pas** avant ce chantier — passe désormais (voir bug ci-dessous) |

Étape 5 — purge :

| Test | Vérifie |
|---|---|
| [`TestCleanupExpiredPasswordResets_Postgres`](../internal/tasks/postgres_integration_test.go) | 4 lignes de part et d'autre de la limite : celles de 8 et 30 jours sont supprimées, celles de 6 jours et d'1 minute survivent |

C'est le seul test de ce fichier qui appelle **directement un point d'entrée cron exporté** — les
autres l'évitent car le Postgres de dev contient une copie de données réelles. C'est sans risque ici :
`password_resets` est une table introduite par la migration 078, elle ne contient que des données de
test, et la tâche ne supprime que sur critère d'âge.

Étape 6 — back-office : `npx tsc --noEmit` sans erreur et `npm run build` réussi (4199 modules).

Étape 7 — POS Flutter : `flutter analyze` sur les 6 fichiers touchés → **aucun problème imputable à
ce chantier**. Les 4 warnings `invalid_null_aware_operator` remontés visent
`authentication_controller.dart` l. 240-244, lignes présentes dans `HEAD` et non modifiées ici
(vérifié : mes seuls ajouts à ce fichier sont les 2 lignes de `requestPasswordReset`).

⚠️ **Étapes 6 et 7 vérifiées par compilation/analyse uniquement** — aucun écran n'a été ouvert dans
un navigateur ni sur un appareil. À contrôler visuellement avant mise en production.

⚠️ Le dépôt `wello_resto_flutter` comportait déjà **57 fichiers modifiés non commités** avant ce
chantier. Ne pas confondre ces changements avec ceux de l'étape 7.

### 🐞 Second bug corrigé : `ChangePasswordDialog` ne pouvait pas fonctionner

Le changement de mot de passe self-service du back-office faisait un `fetch` brut avec **trois**
défauts cumulés, chacun suffisant à le faire échouer :

| Défaut | Conséquence |
|---|---|
| URL relative `'/users/reset-password'` | Tapait le front, pas l'API (`API_BASE_URL`) |
| Méthode `POST` | La route est `PATCH` |
| Aucun header `Authorization` | La route est derrière `authMiddleware` |

Corrigé en passant par `apiClient.patch`. Le composant récupérait déjà `data.token` pour rafraîchir
la session — ce qui est nécessaire, puisque le backend rotationne le token à chaque changement de mot
de passe (D10).

### 🐞 Bug de production trouvé à l'étape 4 (hors périmètre, corrigé)

[`create_repository.go`](../internal/modules/users/create_repository.go) — l'`INSERT` de `CreateUser`
déclarait **8 colonnes pour 9 placeholders**, avec 8 arguments :

```sql
INSERT INTO users (user_id, name, first_name, last_name, email, tel, password, token)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)   -- 9 placeholders
```

`POST /users` et `POST /users/create` échouaient donc systématiquement à l'exécution. Le défaut était
invisible car le seul test qui couvrait ce chemin,
[`postgres_integration_test.go`](../internal/modules/users/postgres_integration_test.go), **ne
compilait plus** : ses appels à `CreateUser` passaient un argument `username` supprimé de la signature
depuis. Un fichier de test qui ne compile pas ne signale rien.

Corrigé : placeholder en trop supprimé, appels de test réalignés. Le fichier compile et passe.

### Échecs de tests préexistants (non causés par ce chantier)

Vérifié à chaque étape en rejouant la suite avec les changements remisés (`git stash push -u`) :

- `planning/leave`, `planning/swaps`, `planning/employees` échouent déjà sans ces changements ;
- **les suites `sqlmock` des modules `auth` et `users`** (`pin_test.go`, `last_login_test.go`,
  `admin_service_test.go`) échouent dès que `DB_DIALECT=postgres` est présent dans l'environnement :
  ces tests attendent des placeholders `?` alors que
  [`dbx.Rebind`](../internal/database/dbx/dialect.go) les réécrit en `$N`. Ils passent sans la
  variable. C'est indépendant de ce ticket, mais **cela signifie qu'on ne peut pas lancer
  `go test -tags postgres_integration ./internal/modules/{auth,users}/...` en une fois** — il faut
  cibler les tests d'intégration avec `-run`. À traiter séparément : maintenant que la base est
  Postgres, ces mocks devraient attendre `$N`.

---

## 6. Avancement

| # | Étape | Statut |
|---|---|---|
| **1** | Migration `078_password_resets` | ✅ **Livrée** — appliquée sur dev + staging |
| **2** | Repository + service (`RequestPasswordReset` / `ConfirmPasswordReset`) | ✅ **Livrée** — 7 sous-tests d'intégration verts |
| **3** | Handlers, routes publiques, email Brevo, throttle IP | ✅ **Livrée** — le flux backend est joignable de bout en bout |
| **4** | Correctif `ForceResetPassword` (même rotation de token) | ✅ **Livrée** — + 1 bug de production trouvé et corrigé |
| **5** | Cron de purge quotidien | ✅ **Livrée** — `0 5 * * *`, rétention 7 jours |
| **6** | Back-office : écrans forgot + reset, correctif `ChangePasswordDialog` | ✅ **Livrée** — typecheck + build de prod OK |
| **7** | POS Flutter : `ReinitPasswordDialog` fonctionnel | ✅ **Livrée** — `flutter analyze` sans erreur |

### Statut d'exécution de la migration `078`

| Environnement | Statut |
|---|---|
| Postgres dev (Docker, `localhost:5433`) | ✅ appliquée et vérifiée |
| Postgres staging (Render, `welloresto_staging`) | ✅ appliquée et vérifiée |
| **Production** | 🔴 **NON appliquée** — aucune URL de production dans l'environnement |

Le fichier reste dans `migrations/` tant qu'il n'est pas appliqué en production ; il sera déplacé
vers `migrations/done/` par `git mv` (convention du
[rapport 60](migration-postgres/60-mysql-migrations-status-checklist.md)).

Pour appliquer ailleurs :
```bash
docker exec -i -e PGURL="<url>" welloresto-postgres-dev sh -c 'psql -v ON_ERROR_STOP=1 "$PGURL"' \
  < migrations/078_password_resets.up.sql
```

---

## 7. Références

- Rapport de schéma : [migration-postgres/61-password-resets-schema.md](migration-postgres/61-password-resets-schema.md)
- Migration : [`078_password_resets.up.sql`](../migrations/078_password_resets.up.sql) / [`.down.sql`](../migrations/078_password_resets.down.sql)
- Schéma cible Postgres : [04-schema-postgres-target.sql](migration-postgres/04-schema-postgres-target.sql)
