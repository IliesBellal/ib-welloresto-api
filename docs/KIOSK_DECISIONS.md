# Décisions de conception — Module Kiosk
### À valider avec Ilies avant implémentation

Généré le : 2026-06-18 — basé sur l'audit de `docs/ARCHITECTURE_API.md`.

Chaque décision porte un statut : **[PROPOSÉ]** par défaut. Aucune n'est validée — ceci est un document de travail, pas une spec figée.

---

## A. Schéma de base de données — nouvelles tables

### A.1 Principes directeurs [PROPOSÉ]

- Style aligné sur les migrations récentes (032-036) : `ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`.
- Clé primaire technique `BIGINT UNSIGNED AUTO_INCREMENT` (`id`) — pas d'UUID en clé primaire, conforme aux tables internes récentes (`delivery_position`).
- Identifiant public exposé au client séparé de la clé primaire technique, préfixé, généré via `helpers.GeneratePrefixedID("KIOSK")` côté Go (pattern déjà utilisé pour `PAY-...`) — cohérent avec la remarque CLAUDE.md sur `helpers.GeneratePrefixedID`.
- Soft delete via `enabled BOOLEAN NOT NULL DEFAULT TRUE` (convention dominante du projet), pas `deleted_at`.
- Pas de FK vers les tables historiques (`merchant`, `orders`, `products`) — cohérent avec le reste du projet. FK autorisée entre tables Kiosk elles-mêmes.
- Timestamps `created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP` + `updated_at DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP` où pertinent (peu de tables existantes ont les deux, mais c'est utile pour le parc de bornes).

### A.2 Table `kiosks` [PROPOSÉ]

```sql
-- Kiosk module: physical terminal registry, fleet management & remote support.
-- public_id is the identifier exposed to clients (KIOSK-<uuid>), never the
-- internal auto-increment id. No FK to `merchant` (no FK to historical
-- tables anywhere in this codebase).

CREATE TABLE kiosks (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id       VARCHAR(64) NOT NULL,
    merchant_id     VARCHAR(64) NOT NULL,
    name            VARCHAR(100) NOT NULL,
    location_id     VARCHAR(64) NULL DEFAULT NULL,
    status          ENUM('pending','active','inactive','revoked') NOT NULL DEFAULT 'pending',
    app_version     VARCHAR(20) NULL DEFAULT NULL,
    hardware_model  VARCHAR(100) NULL DEFAULT NULL,
    os_version      VARCHAR(50) NULL DEFAULT NULL,
    last_heartbeat_at   DATETIME NULL DEFAULT NULL,
    last_ip         VARCHAR(45) NULL DEFAULT NULL,
    last_error      TEXT NULL DEFAULT NULL,
    last_error_at   DATETIME NULL DEFAULT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY idx_kiosks_public_id (public_id),
    KEY idx_kiosks_merchant (merchant_id),
    KEY idx_kiosks_status (merchant_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

**Notes** :
- `status` distingue `pending` (enrôlement en cours, pas encore confirmé) de `active`/`inactive`/`revoked` demandés dans le brief — `pending` ajouté car l'enrôlement est en 2 étapes (code généré → borne qui l'utilise → confirmation).
- `last_error`/`last_error_at` ajoutés pour le **support distant** (visibilité back-office sur "pourquoi cette borne semble down") — à discuter si nécessaire dès le MVP ou en v2.
- `location_id` nullable : si le merchant a plusieurs zones (ex. plusieurs salles), permet de rattacher une borne à un emplacement — réutilise `internal/modules/locations` déjà existant. **Optionnel pour le MVP.**
- Alternative rejetée : stocker `app_version`/`hardware_model` dans une table séparée `kiosk_telemetry` avec historique — jugé prématuré, une seule ligne "dernier état connu" suffit pour le MVP ; un historique pourrait être ajouté plus tard sans casser ce schéma.

### A.3 Table `kiosk_enrollment_codes` [PROPOSÉ]

```sql
-- One-time enrollment codes. code_hash uses the same HMAC-SHA256 pattern as
-- auth.pin (internal/utils/security/pin.go) for deterministic lookup without
-- storing the plaintext code.

CREATE TABLE kiosk_enrollment_codes (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    merchant_id     VARCHAR(64) NOT NULL,
    code_hash       VARCHAR(64) NOT NULL,
    kiosk_id        BIGINT UNSIGNED NULL DEFAULT NULL,
    expires_at      DATETIME NOT NULL,
    used_at         DATETIME NULL DEFAULT NULL,
    created_by_user_id VARCHAR(64) NULL DEFAULT NULL,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY idx_enrollment_code_hash (code_hash),
    KEY idx_enrollment_merchant (merchant_id),
    CONSTRAINT fk_enrollment_kiosk FOREIGN KEY (kiosk_id) REFERENCES kiosks(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

**Notes** :
- `code_hash` plutôt que `code` en clair : même logique que `users_rights.pin_hash` (HMAC-SHA256 + pepper applicatif). Le code affiché au restaurateur (ex. 8 caractères alphanumériques) n'est jamais stocké en clair.
- `kiosk_id` nullable et rempli **après** la première utilisation réussie (le code crée la ligne `kiosks` au moment de l'enrôlement, ou la lie à une borne existante en cas de ré-enrôlement).
- FK vers `kiosks` autorisée (table interne au module Kiosk, pas une table historique).
- TTL recommandé : court (15-30 min) — à trancher en section C.

### A.4 Table `kiosk_device_tokens` [PROPOSÉ]

```sql
-- Kiosk refresh tokens. Mirrors the hashing approach used for PINs
-- (HMAC-SHA256 + pepper), NOT the opaque permanent token pattern used for
-- human users (users_rights.token) — see KIOSK_DECISIONS.md section "Auth".

CREATE TABLE kiosk_device_tokens (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    kiosk_id        BIGINT UNSIGNED NOT NULL,
    device_id       VARCHAR(128) NOT NULL,
    token_hash      VARCHAR(64) NOT NULL,
    expires_at      DATETIME NOT NULL,
    revoked_at      DATETIME NULL DEFAULT NULL,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY idx_device_token_hash (token_hash),
    KEY idx_device_kiosk (kiosk_id),
    CONSTRAINT fk_device_token_kiosk FOREIGN KEY (kiosk_id) REFERENCES kiosks(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

**Notes** — voir section "Authentification Kiosk" (G.1) pour la justification de ce choix vs le token opaque permanent existant.

### A.5 Table `kiosk_sessions` [PROPOSÉ — à évaluer]

```sql
-- Optional: per-customer ordering session on a kiosk, for analytics
-- (time-to-order, abandoned cart, upsell acceptance). Not required for a
-- functional MVP — orders already carry kiosk_id (see section D).

CREATE TABLE kiosk_sessions (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    kiosk_id        BIGINT UNSIGNED NOT NULL,
    merchant_id     VARCHAR(64) NOT NULL,
    started_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at        DATETIME NULL DEFAULT NULL,
    order_id        INT UNSIGNED NULL DEFAULT NULL,
    abandoned       BOOLEAN NOT NULL DEFAULT FALSE,
    upsell_shown    BOOLEAN NOT NULL DEFAULT FALSE,
    upsell_accepted BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_session_kiosk (kiosk_id, started_at),
    KEY idx_session_merchant (merchant_id, started_at),
    CONSTRAINT fk_session_kiosk FOREIGN KEY (kiosk_id) REFERENCES kiosks(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

**Recommandation** : ne pas créer cette table au MVP. `kiosk_id` sur `orders` (section D) suffit à reconstituer le taux de conversion par borne via une jointure simple. `kiosk_sessions` n'apporte de la valeur que pour mesurer l'**abandon de panier avant commande** (un client qui repart sans commander) — métrique utile mais pas bloquante pour lancer le module. À réévaluer une fois le besoin business confirmé.

---

## B. Disponibilité produit par canal [PROPOSÉ]

### Constat de l'audit

`products.is_available_on_sno` est une colonne booléenne dédiée, utilisée par `scannorder.ComputeGetMenu` pour filtrer le menu (voir `ARCHITECTURE_API.md` §7.6). C'est la seule disponibilité par canal qui existe aujourd'hui.

### Options évaluées

| Option | Description | Avantage | Inconvénient |
|---|---|---|---|
| **A — Colonne dédiée `is_available_on_kiosk`** (recommandé) | Même pattern que `is_available_on_sno`, ajoutée par migration `ALTER TABLE products ADD COLUMN is_available_on_kiosk BOOLEAN NULL DEFAULT NULL` | Cohérent avec l'existant, zéro changement dans `menu.MenuService`, simple à comprendre pour le restaurateur (un toggle par produit, par canal) | Une colonne par canal — si demain il y a 5 canaux, `products` devient large ; duplication du filtre dans chaque service (`scannorder.ComputeGetMenu`, futur `kiosk.ComputeGetMenu`) |
| B — Table de disponibilité par canal (`product_channel_availability(product_id, channel, available)`) | Une ligne par produit × canal | Extensible sans ALTER TABLE à chaque nouveau canal, requêtable génériquement | Nouveau concept à introduire alors que le reste du projet ne l'utilise pas ; jointure supplémentaire sur un chemin chaud (affichage menu) ; nécessite de migrer `is_available_on_sno` aussi pour rester cohérent (sinon 2 mécanismes coexistent) |
| C — Flag sur la catégorie (désactiver une catégorie entière sur la borne) | Colonne `is_available_on_kiosk` sur `product_types`/catégorie | Pratique pour désactiver "Desserts" en un clic | Ne remplace pas le besoin produit par produit ; complémentaire, pas alternatif |

### Recommandation [PROPOSÉ]

**Option A** pour rester cohérent avec le pattern existant et ne pas introduire un second mécanisme de disponibilité en parallèle de celui de ScanNOrder — la table de disponibilité générique (option B) est une vraie amélioration mais c'est un changement transverse qui dépasse le scope du Kiosk seul (il faudrait migrer ScanNOrder aussi pour ne pas avoir deux systèmes). À proposer comme refactor séparé si le nombre de canaux augmente encore (Kiosk + SNO + UberEats + Deliveroo ont déjà chacun leurs propres colonnes de sync/dispo : `sync_uber_eats`, `sync_deliveroo`, `is_available_on_sno` — le pattern "colonne par canal" est déjà l'état de fait du projet).

**Complément recommandé** : ajouter aussi un flag catégorie (option C) — `product_types.is_available_on_kiosk` (ou table parallèle) — car désactiver une catégorie entière en un clic est une opération fréquente côté restaurateur (ex. "pas de plats chauds sur la borne le matin") que la colonne produit seule rend fastidieuse.

**Impact migration** :
```sql
ALTER TABLE products ADD COLUMN is_available_on_kiosk BOOLEAN NULL DEFAULT NULL;
ALTER TABLE product_types ADD COLUMN is_available_on_kiosk BOOLEAN NULL DEFAULT NULL;
```
(`NULL` par défaut, pas `FALSE`, pour distinguer "jamais configuré" de "explicitement désactivé" — à confirmer selon le comportement attendu par défaut, voir point ouvert G.2.)

---

## C. Paramètres back-office [PROPOSÉ]

### Section "Bornes" (gestion du parc)

| Paramètre | Type | Notes |
|---|---|---|
| Liste des bornes enrôlées | Lecture | `name`, `status`, `app_version`, `last_heartbeat_at`, `hardware_model` — table `kiosks` |
| Génération de code d'enrôlement | Action | Crée une ligne `kiosk_enrollment_codes`, TTL proposé **15 minutes** (cohérent avec la durée de vie courte des OTP du projet, `models.OTPCacheTTL`) |
| Révocation d'une borne | Action | `kiosks.status = 'revoked'` + révocation de tous ses `kiosk_device_tokens` (`revoked_at = NOW()`) — la borne doit être déconnectée immédiatement (pas d'attente d'expiration token) |
| Nom et description de la borne | Édition | `kiosks.name` (+ colonne `description TEXT NULL` à ajouter si jugé utile) |
| Renommer/réassigner un emplacement | Édition | `kiosks.location_id`, si la notion de localisation par borne est retenue |

### Section "Paramètres de commande Kiosk"

| Paramètre | Type | Indépendant de la caisse ? | Notes |
|---|---|---|---|
| Modes de fulfillment disponibles (sur place / à emporter) | Multi-select | **Oui** | Voir section E |
| Demande de numéro de pager | Bool | **Oui** | Voir section F |
| Affichage des allergènes | Bool | Non forcément lié caisse | Réutilise `internal/modules/allergens` déjà existant — vérifier si l'info est déjà au niveau produit |
| Timeout d'inactivité (retour à l'accueil) | Int (secondes) | Spécifique Kiosk | Pas d'équivalent caisse, paramètre purement UX borne |
| Message d'accueil personnalisable | Texte | Spécifique Kiosk | |
| Upsell activé/désactivé | Bool, par borne **et** par merchant | Le merchant peut avoir l'upsell SNO activé sans l'avoir sur Kiosk | Réutilise `internal/modules/upsell` — ajouter un flag de portée Kiosk |
| Paiement carte activé/désactivé | Bool | Spécifique Kiosk (merchants sans TPE) | Dépend du point ouvert G.3 (intégration Stripe Terminal vs Checkout web sur écran) |
| Option "payer en caisse" | Bool | Spécifique Kiosk | Commande créée en `PENDING_APPROVAL` sans paiement en ligne, comme `order_type = "IN"` chez ScanNOrder |
| Langue(s) disponibles sur la borne | Multi-select | Spécifique Kiosk | Réutilise potentiellement `internal/modules/translation` |
| Délai avant annulation auto. d'une commande non payée | Int | Spécifique Kiosk | Évite les commandes orphelines si le client abandonne à l'étape paiement |

### Section "Apparence Kiosk"

**Proposition : reporter à une v2.** Le logo/bannière/couleurs existent déjà au niveau merchant (`scannorder_settings.logo_url`, `banner_url`, `merchant_parameters.primary_color`, `text_color_on_primary_color`) et peuvent être réutilisés tels quels pour le MVP Kiosk sans nouveau champ. Une image de veille ("écran de veille" entre deux clients) est un besoin réel mais non bloquant — à ajouter (`kiosks.idle_screen_image_url` ou un paramètre merchant `kiosk_settings.idle_screen_url`) une fois le MVP fonctionnel validé.

### Table de paramètres associée [PROPOSÉ]

Plutôt que d'éclater ces paramètres entre plusieurs tables existantes, créer une table dédiée **`kiosk_settings`** (1 ligne par merchant, comme `scannorder_settings` et `merchant_parameters`) :

```sql
CREATE TABLE kiosk_settings (
    merchant_id                VARCHAR(64) NOT NULL,
    activated                  BOOLEAN NOT NULL DEFAULT FALSE,
    fulfillment_dine_in        BOOLEAN NOT NULL DEFAULT TRUE,
    fulfillment_take_away      BOOLEAN NOT NULL DEFAULT TRUE,
    force_fulfillment_type     VARCHAR(20) NULL DEFAULT NULL, -- NULL = demander à l'écran
    pager_number_required      BOOLEAN NOT NULL DEFAULT FALSE,
    show_allergens             BOOLEAN NOT NULL DEFAULT TRUE,
    idle_timeout_seconds       INT NOT NULL DEFAULT 90,
    welcome_message            VARCHAR(255) NULL DEFAULT NULL,
    upsell_enabled              BOOLEAN NOT NULL DEFAULT TRUE,
    card_payment_enabled       BOOLEAN NOT NULL DEFAULT FALSE,
    pay_at_counter_enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    unpaid_order_cancel_minutes INT NOT NULL DEFAULT 10,
    created_at                 DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                 DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (merchant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```
(pas de FK vers `merchant`, conforme à la convention du projet — `merchant_id` reste une simple colonne indexée/clé primaire).

---

## D. Traçabilité des commandes par borne [PROPOSÉ]

### Lien `orders` ↔ `kiosks`

**Recommandation : FK `kiosk_id BIGINT UNSIGNED NULL` sur `orders`**, plutôt qu'un `source_device_id VARCHAR` libre :

```sql
ALTER TABLE orders ADD COLUMN kiosk_id BIGINT UNSIGNED NULL DEFAULT NULL;
CREATE INDEX idx_orders_kiosk ON orders (kiosk_id);
```

Justification : le projet n'a pas de FK vers les tables historiques, mais `kiosks` est une **nouvelle** table — rien n'empêche `orders` (table historique) de référencer `kiosks` (table neuve) sans contrainte stricte (`NULL` par défaut, pas de `CONSTRAINT FOREIGN KEY` pour rester cohérent avec "pas de FK vers/depuis `orders`" observé dans la migration 032 — à confirmer : ajouter l'index suffit, la FK elle-même est optionnelle et peut être omise par cohérence avec le style du projet).

### Distinguer les commandes Kiosk dans les rapports

Pas de colonne `channel`/`source` générique existante (voir `ARCHITECTURE_API.md` §7.3 — le marquage actuel est éclaté entre `is_sno`, `created_by`, `cash_register_id`). Deux options :

- **Option 1 (minimale, cohérente avec l'existant)** : suivre le pattern ScanNOrder — `order.CreatedBy = "KIOSK"` et un `kiosk_id` renseigné suffisent à distinguer une commande Kiosk dans les rapports (`WHERE kiosk_id IS NOT NULL` ou `WHERE created_by = 'KIOSK'`).
- **Option 2 (introduire enfin un champ générique)** : ajouter `orders.channel ENUM('STAFF','SNO','KIOSK','UBER_EATS','DELIVEROO') NULL` — résout proprement le problème pour tous les canaux, pas seulement Kiosk, mais c'est un changement qui dépasse le scope Kiosk (il faudrait backfiller les commandes existantes et mettre à jour ScanNOrder/webhooks en parallèle).

**Recommandation** : Option 1 pour le MVP Kiosk (ne pas bloquer le projet Kiosk sur un refactor transverse), **mais signaler l'Option 2 comme dette technique à traiter séparément** — le besoin de filtrer "toutes les commandes par canal" va se reposer à chaque nouveau canal.

### Analytics à ajouter

- **Temps de passage en borne** : nécessite un timestamp de début (premier produit ajouté au panier) — seulement possible avec `kiosk_sessions` (section A.5) ou en calculant `order.creation_date - kiosk_session.started_at`. Sans `kiosk_sessions`, impossible de mesurer un temps de passage, seulement un temps "de la création de commande au paiement".
- **Taux d'upsell accepté** : déjà trackable via le module `upsell.Tracker` existant (réutilisé tel quel, voir `ARCHITECTURE_API.md` §7.4) — pas de nouvelle table nécessaire si le tracker est appelé avec le bon `suggestion_id` depuis le flux Kiosk.
- **Mode de paiement** : déjà disponible via `payments.mop` (existant) — rien à ajouter.
- **Fulfillment type** : déjà disponible via `orders.order_type` (existant) — rien à ajouter.

---

## E. Modes de fulfillment [PROPOSÉ]

### Modélisation actuelle

`merchant_parameters.manage_on_site` / `manage_take_away` / `manage_delivery` (booléens globaux par merchant, vus dans `auth.UserLoginRow` et la réponse de login). Côté ScanNOrder, c'est `scannorder_settings.in_enabled` / `in_available`, `take_away_enabled` / `take_away_available`, `delivery_enabled` / `delivery_available` — **un couple "activé par le restaurateur" / "actuellement disponible"** (ex. désactivé temporairement sans changer la config) par mode, **par canal** (ScanNOrder a son propre couple, distinct de `merchant_parameters`).

### Recommandation pour le Kiosk

Suivre exactement le même pattern que ScanNOrder, dans `kiosk_settings` (section C) : `fulfillment_dine_in` / `fulfillment_take_away` (booléens "activé"), sans dupliquer la distinction "available" en plus de "enabled" sauf si un besoin de désactivation temporaire dans la journée est confirmé (ex. "à emporter indisponible entre 12h et 14h" — pas un besoin exprimé dans le brief, à clarifier si nécessaire).

### "Demander à l'écran" vs "forcer un mode"

Ajouté dans le schéma `kiosk_settings.force_fulfillment_type VARCHAR(20) NULL` : `NULL` = l'écran demande le choix au client (si les deux modes sont activés), une valeur (`"DINE_IN"`/`"TAKE_AWAY"`) = ce mode est imposé sans demander (utile pour un kiosque dédié à un seul flux, ex. zone food-court sans service sur place).

---

## F. Numéro de pager [PROPOSÉ]

### Existant

`merchant_parameters.pager_number_required` (booléen global merchant, vu dans `auth.UserLoginRow.PagerNumberRequired` et la réponse de login `LoginMerchantSettingsResponse.PagerNumberRequired`) — actuellement un seul paramètre, **pas de distinction par canal**.

### Recommandation

Ajouter `kiosk_settings.pager_number_required` (déjà inclus dans le schéma section C) comme paramètre **indépendant** de `merchant_parameters.pager_number_required` — un restaurateur peut vouloir le pager en caisse (où le staff connaît le contexte) mais pas sur la borne (où le client pourrait se tromper de numéro sans supervision), ou inversement.

**Point ouvert** : faut-il que `kiosk_settings.pager_number_required` ait une valeur par défaut héritée de `merchant_parameters.pager_number_required` à la création (copie), ou toujours démarrer à `FALSE` indépendamment ? Recommandation : démarrer à `FALSE` par défaut — le restaurateur active explicitement ce qu'il veut sur la borne, pas d'héritage implicite qui pourrait surprendre.

---

## G. Autres décisions identifiées pendant l'audit

### G.1 Authentification du device Kiosk [PROPOSÉ — décision structurante]

Le projet n'a **aucun JWT** ; tout repose sur un token opaque permanent stocké en base (`users_rights.token`, voir `ARCHITECTURE_API.md` §8.2-8.3). Un device Kiosk n'est pas un humain : il n'a pas de mot de passe, doit pouvoir se reconnecter seul après un redémarrage, et doit pouvoir être révoqué à distance immédiatement.

**Options évaluées** :
1. **Token opaque permanent, façon `users_rights.token`** — cohérent avec le reste du projet, mais pas de révocation "douce" (un seul état: existe ou pas) et pas de rotation, ce qui est plus risqué pour un device qui peut être volé/exposé physiquement (un kiosque est en accès public, contrairement à un poste de caisse en back-office).
2. **Paire access/refresh avec rotation, façon `kiosk_device_tokens` proposée en A.4** — le device présente son `device_id` + refresh token hashé à `/kiosk/auth/refresh`, reçoit un access token court (en mémoire, pas persisté) + un nouveau refresh token (rotation à chaque usage). Permet une révocation immédiate (`revoked_at`) et limite l'exposition si le device est compromis.
3. **JWT signé avec claims (merchant_id, kiosk_id, exp)** — léger, pas de lookup DB à chaque requête, mais introduit un concept totalement absent du reste du projet (aucune lib JWT actuellement importée), et la révocation immédiate d'un JWT non expiré nécessite une liste de révocation (donc un lookup DB de toute façon — perd l'avantage principal du JWT).

**Recommandation** : **Option 2**. Elle s'aligne sur le fait qu'un device est un risque physique différent d'un utilisateur humain (justifiant un mécanisme de rotation que le projet n'a pas pour les humains), tout en restant dans l'esprit "token haché en base, comparaison déterministe" déjà utilisé pour les PIN (`security.HashPIN`). Le JWT (option 3) est rejeté pour rester cohérent avec l'absence totale de JWT ailleurs dans le projet, sauf si Ilies souhaite l'introduire consciemment comme première brique d'une migration plus large.

**Conséquence sur le middleware** : un nouveau middleware `middleware.KioskAuth(service KioskAuthService)` est nécessaire, distinct de `middleware.Auth` (qui type le contexte avec `*auth.UserLoginRow`) — injecterait un `*kiosk.AuthenticatedKiosk{KioskID, MerchantID}` dans le contexte via une clé dédiée. Les handlers Kiosk liraient ce contexte au lieu de `middleware.GetUser(r)`.

### G.2 Comportement par défaut de la disponibilité produit [POINT OUVERT]

Si `products.is_available_on_kiosk` est `NULL` pour tous les produits existants au moment du lancement (cas par défaut, migration sans backfill), **tous les produits seront invisibles sur le Kiosk** tant que le restaurateur ne les active pas un par un — ce qui peut être perçu comme un bug plutôt qu'une fonctionnalité à l'activation du module. Alternative : backfiller `is_available_on_kiosk = is_available_on_sno` (hériter de la config ScanNOrder existante comme valeur de départ raisonnable) plutôt que tout désactiver. **Décision à prendre avec Ilies avant la migration.**

### G.3 Paiement carte sur Kiosk — flux à clarifier [POINT OUVERT]

Le brief mentionne "Paiement carte activé/désactivé (pour les merchants sans TPE)". Le mécanisme de paiement actuel pour ScanNOrder est une **redirection vers Stripe Checkout** (URL web, `StripeManager.CreateCheckoutSession`) — adapté à un téléphone client, mais probablement **pas adapté à l'écran d'un kiosque** qui n'a pas de navigateur client distinct. Deux pistes possibles, non tranchées par cet audit :
- Stripe Terminal (lecteur de carte physique intégré au kiosque) — nécessite un module d'intégration entièrement nouveau, non présent dans le projet actuellement.
- Affichage du QR code Stripe Checkout à l'écran, scanné par le client avec son propre téléphone — réutilise l'infra existante sans nouveau module, mais expérience moins fluide.

**Ce point nécessite une décision produit avant tout travail d'implémentation paiement Kiosk** — il conditionne fortement l'architecture du flux de commande borne.

### G.4 Webhook Stripe — vérification nécessaire avant d'activer le paiement Kiosk [POINT OUVERT]

Le webhook Stripe (`internal/webhook/stripe/`) n'a pas été audité en détail dans cette session (hors scope de l'audit initial). Avant d'implémenter un flux de paiement Kiosk, il faudra vérifier comment ce webhook distingue aujourd'hui une commande ScanNOrder d'une autre, pour s'assurer qu'une commande Kiosk sera traitée correctement par le même point d'entrée (ou nécessitera une branche dédiée).

---

## Résumé — décisions nécessitant une validation humaine avant implémentation

1. **A.5** — Créer `kiosk_sessions` dès le MVP, ou différer ? (recommandation : différer)
2. **B** — Colonne dédiée `is_available_on_kiosk` (option A, recommandé) vs table de disponibilité générique (option B, refactor plus large)
3. **C** — Périmètre exact des paramètres `kiosk_settings` pour le MVP (lesquels sont v1 vs v2, notamment apparence)
4. **D** — FK stricte `orders.kiosk_id → kiosks.id` ou simple colonne indexée sans contrainte ? Et faut-il introduire `orders.channel` générique (option 2) maintenant ou plus tard ?
5. **G.1** — Authentification device : token opaque permanent (option 1) vs refresh token avec rotation (option 2, recommandé) vs JWT (option 3, écarté sauf décision contraire explicite)
6. **G.2** — Backfill de `is_available_on_kiosk` à l'activation (hériter de `is_available_on_sno` vs tout désactiver par défaut)
7. **G.3** — Flux de paiement carte sur borne (Stripe Terminal vs QR code Checkout affiché à l'écran) — **bloquant pour toute implémentation paiement Kiosk**
8. **G.4** — Audit du webhook Stripe à faire avant d'activer le paiement Kiosk
