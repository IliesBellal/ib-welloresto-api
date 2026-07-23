# Audit — Résilience de l'enrôlement Kiosk (device_id)

**Phase 0 : audit uniquement, aucune implémentation.** Rapport factuel, lecture seule — aucun fichier n'a été modifié pour produire ce document.

Repos audités :
- `ib-welloresto-api` (racine `d:\Desktop\DTL\Project\git-projects\ib-welloresto-api`) — module `kiosk`, module `cash_registers`, middleware `kiosk_auth`.
- `wello-kiosk` (`d:\Desktop\DTL\Project\git-projects\wello-kiosk`) — app Flutter borne.
- `wello_resto_flutter` (`d:\Desktop\DTL\Project\git-projects\wello_resto_flutter`) — app Flutter POS/caisse (client du pattern `device_link`).
- `wello-back-office` (`d:\Desktop\DTL\Project\git-projects\wello-back-office`) — admin web (React/TS/Vite).

---

## 1. Schéma actuel complet (tables, endpoints, stockage local)

### 1.1 Tables MySQL (module Kiosk)

Migrations : `037_kiosk_module`, `038_kiosk_existing_tables`, `039_kiosk_settings_idle_video`, `040_kiosk_simplify_ids`, `042_kiosk_admin_pin`, `061_kiosk_settings_fees` (`migrations/done/`).

**`kiosks`** ([migrations/done/037_kiosk_module.up.sql](../migrations/done/037_kiosk_module.up.sql), id collapsé en VARCHAR par [040_kiosk_simplify_ids.up.sql](../migrations/done/040_kiosk_simplify_ids.up.sql)) :

| Colonne | Notes |
|---|---|
| `id` | VARCHAR(64), `kiosk-<uuid>` (`helpers.KioskIDPrefix`) — devenu clé primaire directe depuis la migration 040 (plus de distinction id technique / public_id) |
| `merchant_id` | pas de FK (convention du projet) |
| `status` | ENUM `pending,active,inactive,revoked` — **`pending` n'est jamais utilisé en pratique** : [`Repository.CreateKiosk`](../internal/modules/kiosk/repository.go#L49-L70) insère `status='active'` en dur, malgré le design en 2 étapes décrit dans `docs/KIOSK_DECISIONS.md` §A.2 |
| `hardware_model`, `os_version`, `app_version` | chaînes **auto-déclarées par le client** à l'enrôlement/heartbeat (`Platform.operatingSystem`/`Platform.operatingSystemVersion` côté Flutter) — jamais vérifiées, non uniques (deux tablettes identiques envoient la même valeur) |
| `last_heartbeat_at`, `last_ip`, `last_error`, `last_error_at` | télémétrie, mise à jour par `RecordHeartbeat`/`ReportUnavailable` |
| `admin_pin_encrypted` | PIN admin 4 chiffres, chiffrement réversible (`helpers.Encrypt`), déchiffré à la demande pour consultation back-office |
| `enabled` | soft-disable, distinct de `status` |

Pas de colonne `device_id` / fingerprint. C'est le point central de cet audit — détaillé en §1.4.

**`kiosk_enrollment_codes`** : `id`, `merchant_id`, `code_hash` (HMAC-SHA256 + pepper, même mécanisme que `security.HashPIN`), `kiosk_id` (nullable, rempli une fois le code consommé), `expires_at`, `used_at`, `created_by_user_id`. TTL par défaut 15 min (`KIOSK_ENROLLMENT_CODE_TTL_MINUTES`), enforced côté applicatif, pas en DB.

**`kiosk_device_tokens`** (refresh tokens) : `id`, `kiosk_id` (FK), `token_hash`, `expires_at`, `revoked_at`, `last_used_at`, `created_at`. **Écart avec le document de conception** : `docs/KIOSK_DECISIONS.md` §A.4 proposait une colonne `device_id VARCHAR(128)` sur cette table — absente de [037_kiosk_module.up.sql](../migrations/done/037_kiosk_module.up.sql) et absente de [`KioskDeviceTokenRow`](../internal/modules/kiosk/models.go#L41-L49). Elle n'a jamais été implémentée.

**`kiosk_settings`** : 1 ligne par merchant, paramètres d'affichage/fulfillment/paiement — sans rapport direct avec l'auth device.

### 1.2 Endpoints `/kiosk/*` ([cmd/api/routes.go:1185-1262](../cmd/api/routes.go#L1185-L1262))

```
POST /kiosk/auth/enroll                     device, public (pas de token)
POST /kiosk/auth/token/refresh              device, public (pas de token)
  --- à partir d'ici : middleware.KioskAuth ---
POST /kiosk/auth/heartbeat
POST /kiosk/auth/verify-admin-pin
GET  /kiosk/menu | /kiosk/products/{id} | /kiosk/settings | /kiosk/discounts
POST /kiosk/upsell | /kiosk/pricing | /kiosk/orders
GET/DELETE /kiosk/orders/{order_id}
POST /kiosk/orders/{order_id}/counter-payment | /switch-to-counter-payment
POST /kiosk/status/unavailable
POST /kiosk/terminal/connection-token | /payment-intent | /payment-intent/{id}/cancel
/ws-kiosk                                   WebSocket, middleware.KioskAuth

--- POS (staff, authMiddleware humain) ---
POST /pos/kiosk/{kiosk_id}/status

--- Back-office (authMiddleware humain) ---
POST/GET   /pos/settings/kiosk/enrollment-codes
DELETE     /pos/settings/kiosk/enrollment-codes/{code_id}
GET        /pos/settings/kiosk/devices
GET/PUT    /pos/settings/kiosk/devices/{device_id}
POST       /pos/settings/kiosk/devices/{device_id}/enable | /disable | /revoke
GET/POST   /pos/settings/kiosk/devices/{device_id}/admin-pin | /regenerate-admin-pin  (RequirePermission HasSettingsAccess)
GET/PUT    /pos/settings/kiosk/settings
POST       /pos/settings/kiosk/settings/logo | /idle-image | /idle-video
```

Remarque : `{device_id}` dans les routes back-office désigne l'**id du kiosk lui-même** (`kiosks.id`), pas un fingerprint matériel — nommage qui prête à confusion avec le sujet de cet audit.

### 1.3 `middleware.KioskAuth` — validation du token

[internal/middleware/kiosk_auth.go:36-73](../internal/middleware/kiosk_auth.go#L36-L73) : extrait le `Bearer <token>`, appelle `service.ValidateAccessToken`, injecte `AuthenticatedKiosk{KioskID, MerchantID}` dans le contexte. Toute erreur → `models.ErrKioskDeviceTokenInvalid` (HTTP 401, `kiosk_device_token_invalid`).

[`Service.ValidateAccessToken`](../internal/modules/kiosk/service.go#L255-L265) : **aucun accès DB**. L'access token est auto-porteur, signé HMAC-SHA256 (pepper `KIOSK_TOKEN_PEPPER`, fallback `PIN_PEPPER`) — seule la signature et l'expiration embarquée sont vérifiées (`parseAccessToken`/`signPayload`, service.go:1859-1897).

**Conséquence vérifiée sur le code** : seuls [`RefreshDeviceToken`](../internal/modules/kiosk/service.go#L196-L249) (ligne 220) et [`RecordHeartbeat`](../internal/modules/kiosk/service.go#L268-L285) (ligne 276) vérifient `kiosk.Status == "revoked"`. **Aucun** des autres endpoints protégés par `KioskAuth` (menu, commandes, paiement Terminal, PIN admin...) ne revérifie le statut de la borne — une borne révoquée ou désactivée continue de fonctionner normalement (y compris créer des commandes et encaisser par carte) jusqu'à l'expiration naturelle de son access token déjà émis (**≤ 15 min**, `KIOSK_ACCESS_TOKEN_TTL_MINUTES`). Seul le prochain `refresh` ou `heartbeat` est bloqué immédiatement.

### 1.4 Durée de vie des tokens et comportement sur refresh token invalide/expiré

- Access token : 15 min (`KIOSK_ACCESS_TOKEN_TTL_MINUTES`, défaut 15 ; côté Flutter, `AppConfig.accessTokenTTLSeconds = 900`).
- Refresh token : 30 jours (`KIOSK_DEVICE_TOKEN_TTL_DAYS`, défaut 30 ; côté Flutter, `AppConfig.refreshTokenTTLDays = 30`), **rotation sans fenêtre de grâce** — [`RotateDeviceToken`](../internal/modules/kiosk/repository.go#L119) invalide l'ancien refresh token dans la même opération qui émet le nouveau.

[`Service.RefreshDeviceToken`](../internal/modules/kiosk/service.go#L196-L249) :
```go
deviceToken, err := s.repo.GetDeviceTokenByHash(ctx, tokenHash)
...
if deviceToken == nil { return nil, models.ErrKioskDeviceTokenInvalid }   // 401 kiosk_device_token_invalid
if deviceToken.RevokedAt != nil { return nil, models.ErrKioskDeviceTokenInvalid }
if time.Now().UTC().After(deviceToken.ExpiresAt) { return nil, models.ErrKioskDeviceTokenInvalid }
...
if kiosk.Status == "revoked" { return nil, models.ErrKioskRevoked }       // 403 kiosk_revoked
```
Mapping HTTP : [internal/models/responses_models.go:1053-1066](../internal/models/responses_models.go#L1053-L1066) — `kiosk_device_token_invalid` → 401, `kiosk_revoked` → 403, `kiosk_not_found` → 404.

**Il n'existe aucune notion de device_id dans cette vérification** — c'est uniquement une preuve de possession du secret refresh token. Perdre ce secret = aucun moyen de prouver "je suis la même borne qu'avant".

### 1.5 Stockage côté Flutter (wello-kiosk)

`lib/data/services/auth_service.dart` (`AuthService`) :

- **`flutter_secure_storage`** pour les 4 clés sensibles :
  ```dart
  static const String keyDeviceId = 'kiosk_device_id';       // = kiosk_id serveur, PAS un fingerprint
  static const String keyAccessToken = 'kiosk_access_token';
  static const String keyRefreshToken = 'kiosk_refresh_token';
  static const String keyTokenExpiry = 'kiosk_token_expiry';
  ```
- **`shared_preferences`** (non chiffré) pour un flag miroir `kiosk_enrolled` — lu directement par le code natif Android (`BootReceiver.kt`) au démarrage du device pour décider de relancer `MainActivity`, sans avoir à déchiffrer le secure storage. Il ne contient jamais le token lui-même.
- `saveTokens()` écrit les 4 clés + le flag après enrôlement et après chaque refresh. `revokeLocal()` supprime les 4 clés secure-storage et repasse le flag à `false`.

**Aucun identifiant device n'est généré côté client.** Vérifié : pas de dépendance `device_info_plus` dans `pubspec.yaml`, aucun import `uuid`/`device_info`, aucune lecture d'Android ID / `identifierForVendor` dans tout `lib/`. La seule "identité" que l'app possède est **`kiosk_id`, généré côté serveur** à l'enrôlement et simplement recopié dans la clé `kiosk_device_id` — ce n'est pas un identifiant que l'app peut reconstituer seule si le stockage est perdu.

### 1.6 Flow d'enrôlement actuel (wello-kiosk)

`lib/presentation/screens/enrollment_screen.dart` → `KioskAuthController.enroll()` → `AuthRepository.enroll()` :

```dart
final response = await _apiService.enrollDevice(EnrollRequest(
  enrollmentCode: code.replaceAll('-', ''),
  name: deviceName,
  hardwareModel: Platform.operatingSystem,        // ex. "android" — pas un identifiant stable
  osVersion: Platform.operatingSystemVersion,
  appVersion: _kAppVersion,
));
await _authService.saveTokens(
  deviceId: response.kioskId,                      // ré-utilise l'id serveur, pas de valeur locale
  accessToken: response.accessToken,
  refreshToken: response.refreshToken,
  expiresAtIso: response.expiresAt,
);
```
`POST /kiosk/auth/enroll` (`skipAuth: true`, pas de Bearer). Après succès : tentative `AndroidKiosk.startLockTask()` (best-effort), connexion WebSocket, démarrage du heartbeat périodique, navigation vers l'écran idle.

### 1.7 Comportement au démarrage de l'app

`main.dart` → `_bootstrap()` appelle `await kioskAuthController.checkEnrollment()` **avant** `runApp()` :

```dart
Future<void> checkEnrollment() async {
  final enrolled = await _authRepository.isEnrolled();   // lit juste "refresh token présent ?"
  state = enrolled ? KioskAuthState.enrolled : KioskAuthState.unenrolled;
  ...
}
```
`isEnrolled()` (`auth_service.dart:35-38`) **ne fait aucun appel API et ne vérifie aucune expiration** — un refresh token non-vide en storage suffit à considérer l'app "enrôlée", de façon optimiste. Le routeur choisit `initialLocation` en conséquence (`/idle` vs `/enrollment`).

- **Storage vide/absent** → écran d'enrôlement direct, aucun appel réseau, aucune erreur affichée.
- **Token présent mais rejeté par l'API** → non détecté au cold start ; détecté seulement de façon réactive au premier 401 (`ApiService._onError`, tentative de refresh unique, puis si échec : `revokeLocal()` + `onSessionExpired` → `router.go('/enrollment')`, défini dans `main.dart:328-335`). Aucun écran d'erreur dédié, aucune distinction visible entre "jamais enrôlé" et "ex-borne dont le token est mort" — l'utilisateur atterrit sur le même écran de saisie de code que pour un device neuf.

---

## 2. Pattern existant : device-linking du module `cash_registers`

### 2.1 Le point exact où le `device_id` est généré et vérifié

**Génération, côté client** — `wello_resto_flutter/lib/data/services/secure_storage_service.dart:32-57` :

```dart
Future<String> getOrGenerateDeviceId() async {
  final existing = await _storage.read(key: _deviceIdKey);
  if (existing != null && existing.isNotEmpty) return existing;

  final platformDeviceId = await _readPlatformDeviceId();
  if (platformDeviceId != null && platformDeviceId.isNotEmpty) {
    await _storage.write(key: _deviceIdKey, value: platformDeviceId);
    return platformDeviceId;
  }
  throw StateError('Impossible de récupérer un device_id natif');
}
```
`_readPlatformDeviceId()` appelle `PlatformDeviceId.getDeviceId` du package `platform_device_id_plus` (Android ID / `identifierForVendor` iOS — un identifiant **dérivé de l'OS**, pas généré par l'app), mis en cache dans `flutter_secure_storage`, avec repli sur la valeur déjà en cache si l'appel natif échoue.

**Différence structurelle clé** : cet id est **reconstituible** — un cache app vidé (data clear, réinstallation) redonne en général la même valeur puisqu'elle vient de l'OS (sur Android, l'ANDROID_ID ne change pas à la réinstallation d'une app, seulement à un factory reset). C'est l'inverse exact du `kiosk_id` de Kiosk, purement serveur-miné et **irrécupérable** si le cache local qui le contient disparaît.

**Vérification/usage, côté serveur — ce n'est PAS un mécanisme d'authentification** :

- `POST /cash_register/open` ([internal/modules/cash_registers/repository.go:31-85](../internal/modules/cash_registers/repository.go#L31-L85)) : `device_id` sert seulement à (a) vérifier qu'il n'y a pas déjà un registre ouvert pour ce couple `device_id + merchant_id`, et (b) être stocké en simple colonne de traçabilité sur la ligne `cash_registers`. L'authentification de cette route est le token du **membre du staff humain** (`authMiddleware`) — `device_id` n'a aucun poids d'authentification.
- `POST /cash_register/link` / `DELETE /cash_register/link` ([internal/modules/cash_registers/service.go:123-157](../internal/modules/cash_registers/service.go#L123-L157), table `device_link(device_id PK, user_id, on_behalf_of, creation_date)`) : fonctionnalité distincte — un appareil physique peut agir "pour le compte de" (`on_behalf_of`) un autre utilisateur, sans que celui-ci se ré-authentifie sur cet appareil à chaque fois. `IsCircularDeviceLink` ne protège qu'une paire directe A↔B (`WHERE device_id = onBehalfOf AND on_behalf_of = deviceID`), pas les chaînes plus longues.
- Cette fonctionnalité `device_link` **n'a aucune UI côté back-office** (confirmé §4) — pilotée uniquement depuis l'app POS elle-même.

### 2.2 Ce qui est réutilisable tel quel vs à adapter

| | Registre de caisse | Besoin Kiosk |
|---|---|---|
| Identité de session | token humain (staff) | pas d'humain — le device EST l'identité |
| Rôle du `device_id` | tag de traçabilité/délégation, zéro poids d'auth | doit devenir la seule ancre de ré-identification possible |
| Stabilité | dérivé OS, survit à un reinstall/data-clear | `kiosk_id` actuel ne survit à rien (100% local) |
| Révocation/expiration | aucune notion — le device_id est réutilisable indéfiniment | tokens avec rotation + révocation explicite déjà en place |

**Réutilisable tel quel** : la technique client (`platform_device_id_plus` + cache secure-storage) pour obtenir un identifiant stable indépendant de tout secret émis par le serveur.

**À concevoir entièrement pour Kiosk** : tout le reste. Le registre de caisse n'a jamais eu à résoudre "prouver que je suis le même appareil physique qu'avant, sans disposer d'aucun secret préalable" — c'est précisément le problème que Kiosk doit résoudre et qui n'a pas d'équivalent à copier.

---

## 3. Back-office (wello-back-office) — ce que voit le restaurateur aujourd'hui

*(Source : audit délégué sur ce repo, vérifié fichier par fichier — aucune modification apportée.)*

- **Vue "Mes bornes"** (`/kiosk/devices`, `src/pages/kiosks/KiosksPage.tsx`) : colonnes Nom, Statut (badge), Matériel (`hardware_model`), Version app, Dernière connexion (`last_heartbeat_at`, relatif), IP. Données via `GET /pos/settings/kiosk/devices`.
- **Actions manuelles disponibles** : Renommer (`PUT .../devices/{id}`), Activer/Désactiver (`POST .../enable`/`/disable`), **Révoquer** (`POST .../revoke`, destructif, confirmation "déconnectée immédiatement" — cf. §1.3, cette immédiateté est en réalité limitée par l'access token déjà émis). **Pas de suppression définitive** d'une borne (seulement les codes d'enrôlement en attente sont supprimables).
- **Indicateur "hors ligne / token expiré" : absent.** Le badge de statut n'affiche que `status` tel quel renvoyé par l'API (`pending|active|inactive|revoked`) ; "Dernière connexion" est affiché brut, sans seuil ni recoloration "en ligne/hors ligne" calculée côté front. Aucune notion de "token expiré" n'existe côté back-office — cette information n'est même pas exposée par l'API aujourd'hui (le back-office ne voit jamais l'état des tokens, seulement `status`/`last_heartbeat_at`).
- **Comparaison caisse** : contrairement à l'hypothèse de départ, **il n'existe aucune UI de pairing/liaison d'appareil pour les caisses** dans ce repo — `/accounting/registers` (`CashRegisterHistory.tsx`) n'affiche que l'historique des sessions de caisse (ouverture/fermeture Z), sans device_id, sans indicateur de connectivité, sans étape de code de pairing. Le mécanisme `device_link` audité en §2 est entièrement piloté depuis l'app POS, invisible du restaurateur dans ce back-office.
- **Trouvaille annexe** : `src/pages/CashRegisters.tsx` (+ composants associés) forme une UI de caisse parallèle, non routée dans `App.tsx` — code mort, à ne pas utiliser comme référence.

### Bug annexe découvert pendant l'audit (hors périmètre direct, mais pertinent — voir §4)

Le commit [`72b31bd` "fix: ignore revoked kiosks more than 24h"](../internal/modules/kiosk/repository.go) a changé le `WHERE` de [`Repository.ListKiosksByMerchant`](../internal/modules/kiosk/repository.go#L231) en :
```sql
WHERE merchant_id = ?
AND (
  status = 'active'
  OR (status = 'revoked' AND last_heartbeat_at >= DATE_SUB(UTC_TIMESTAMP(), INTERVAL 24 HOUR))
)
```
Ce filtre exclut aussi, sans doute involontairement, les bornes `status='inactive'` (désactivées manuellement) de la liste — une borne désactivée depuis le back-office **disparaît de `GET /pos/settings/kiosk/devices`** et devient introuvable depuis la vue "Mes bornes" pour être réactivée (seul un appel direct `GET /devices/{id}` avec l'id déjà connu la retrouve). À corriger indépendamment de ce projet, mais particulièrement gênant si la V2 s'appuie sur cette même liste pour qu'un restaurateur retrouve une borne à confirmer/réactiver.

---

## 4. Risques et edge cases à anticiper pour la V2

- **Changement de tablette physique** : un mécanisme de ré-identification par `device_id` ne doit pas permettre à un nouvel appareil de réclamer silencieusement l'identité d'un ancien `kiosk_id` sans action délibérée — sinon deux tablettes physiques différentes pourraient se partager à l'insu de tous un même historique/PIN admin/paramètres.
- **Collision de `device_id`** : l'identifiant `platform_device_id_plus` n'est pas garanti unique dans l'absolu (devices rootés/clonés/émulés, variations selon fabricants Android). Il ne doit jamais être la seule condition suffisante pour une réactivation automatique, et sa portée d'unicité (globale vs par merchant) doit être un choix explicite, pas un accident d'implémentation.
- **Fenêtre de révocation actuelle non instantanée** : `ValidateAccessToken` ne fait aucune vérification de statut (§1.3) — une borne révoquée reste pleinement fonctionnelle (menu, commandes, paiement carte) jusqu'à expiration de son access token déjà émis (≤ 15 min). Un flow de ré-identification par device_id ne doit pas rouvrir/élargir cette même fenêtre pour un appareil volé qui redémarrerait après une révocation back-office.
- **`pending` est mort dans le code actuel** : `CreateKiosk` force `status='active'` dès l'enrôlement (§1.1) — si la V2 veut une étape "en attente de confirmation back-office" avant réactivation, ce statut (ou un équivalent) doit être réellement câblé ; aujourd'hui il ne l'est pas malgré sa présence dans l'ENUM et dans le document de conception initial.
- **`kiosk_id` est déjà référencé ailleurs** : clé d'idempotence des commandes (`kiosk:idempotency:{kiosk_id}:...`), colonne `orders.kiosk_id`. Une ré-identification devrait très probablement **réutiliser la ligne `kiosks` existante** (même `kiosk_id`) plutôt qu'en créer une nouvelle, pour ne pas casser la continuité des commandes/rapports déjà associés à cette borne.
- **Rotation de refresh token sans fenêtre de grâce** : si la réponse d'un `refresh` réussi côté serveur se perd en transit (coupure réseau), l'ancien refresh token est déjà révoqué et le nouveau jamais reçu — la borne est aujourd'hui bloquée exactement comme en cas de perte de stockage. Un flow device_id devrait couvrir ce cas aussi, pas seulement le "storage effacé".
- **PIN admin** : actuellement montré en clair uniquement à l'enrôlement et à la régénération explicite. À trancher : une ré-identification doit-elle re-exposer le PIN existant à l'écran, ou forcer une régénération ?
- **Quota `max_kiosks`** : `GetActiveKioskCount` compte `status IN ('pending','active') AND enabled=true`. Une ré-identification qui réutilise la ligne existante (recommandé ci-dessus) ne doit pas la faire compter deux fois — à garantir explicitement dans la conception, pas laissé à l'implémentation.
- **Bug de visibilité back-office** (§3) : si la V2 s'appuie sur une confirmation manuelle via la liste "Mes bornes", le filtre introduit par `72b31bd` doit être corrigé en amont, sous peine de bornes en attente de confirmation invisibles.
- **Absence de traçabilité** : aujourd'hui rien ne permettrait d'afficher côté back-office/support "cette borne s'est ré-identifiée via device_id le [date]" — à prévoir si la confirmation humaine ou l'audit après coup fait partie du besoin.

---

## 5. Proposition de flow (pseudo-étapes) — points de décision à valider avant implémentation

Squelette de flow uniquement, plusieurs branches volontairement laissées ouvertes :

1. Dès l'enrôlement (ou au premier lancement suivant la mise en prod de cette fonctionnalité), la borne résout et persiste un identifiant stable au niveau OS (même technique que `wello_resto_flutter` : `platform_device_id_plus` + cache secure-storage), **en plus de** son `kiosk_id`/ses tokens actuels, et le transmet à l'API dès l'enrôlement pour qu'il soit rattaché à la ligne `kiosks` dès le départ. *(Nécessite une migration : colonne `device_id` sur `kiosks` — **question ouverte : unicité par merchant, ou globale avec vérification secondaire ?**)*
2. Au démarrage, si le refresh token est absent/rejeté (cas actuellement sans issue), l'app tente en plus d'envoyer son `device_id` persisté à un nouvel endpoint, ex. `POST /kiosk/auth/reclaim`. **Question ouverte : que se passe-t-il pour les bornes déjà enrôlées avant le déploiement de cette fonctionnalité, qui n'ont donc aucun `device_id` en base ?** Repli pur et simple sur le flow actuel (ré-enrôlement complet), ou campagne de "rattachement" a posteriori à prévoir ?
3. Le serveur cherche une borne par `device_id`. **Question ouverte : comment le serveur sait-il sur quel merchant scoper la recherche**, si le token/`kiosk_id` local a disparu — l'app garde-t-elle `merchant_id`/`kiosk_id` en cache séparément des 4 clés effacées par `revokeLocal()` (donc plus résilientes), ou le `device_id` seul est-il supposé assez unique pour une recherche globale ?
4. **Point de décision central (celui explicitement soulevé dans le brief)** : sur un `device_id` trouvé, le serveur (a) réémet silencieusement une nouvelle paire de tokens et la borne reprend immédiatement (automatique), ou (b) place la borne en attente (`pending`/statut dédié) et exige une confirmation explicite côté back-office (notification / badge "Borne X demande à se reconnecter") avant d'émettre des tokens ? Au vu des risques de changement de tablette et de vol (§4), une réactivation 100% silencieuse est la option la plus risquée par défaut ; un compromis (réactivation auto seulement si la borne n'a jamais été `revoked` explicitement, confirmation obligatoire sinon) est envisageable mais reste un choix à valider, pas une évidence.
5. Si confirmé (automatiquement ou par un humain), le serveur émet une nouvelle paire access/refresh pour le `kiosk_id` **existant** (pas de nouvelle ligne), met à jour `last_heartbeat_at`/`last_ip`, et l'app persiste comme pour un refresh normal — pas d'étape supplémentaire ("nouveau PIN", "reconfigurer les paramètres") puisque rien d'autre sur la ligne n'a changé.
6. Si aucune correspondance, ou confirmation refusée/non obtenue : repli exact sur le comportement actuel (écran de saisie de code d'enrôlement), éventuellement avec un message différent si un `device_id` a été trouvé mais est en attente de confirmation ("Cette borne est peut-être déjà connue — demandez à votre responsable de confirmer depuis le back-office").

**Questions à trancher avec Ilies avant toute implémentation** :
- Portée d'unicité du `device_id` (par merchant vs globale) et comportement en cas de collision.
- Sort des bornes déjà enrôlées sans `device_id` en base au moment du déploiement.
- Réactivation automatique vs confirmation back-office obligatoire — et si un compromis conditionné au statut (`active`/`inactive` vs `revoked`) est retenu.
- Faut-il re-exposer le PIN admin existant à la ré-identification, ou forcer sa régénération ?
- Faut-il corriger le filtre de `72b31bd` (bornes `inactive` invisibles) en prérequis, si la confirmation back-office s'appuie sur la liste "Mes bornes" ?
