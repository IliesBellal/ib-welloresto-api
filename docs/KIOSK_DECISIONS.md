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

---

## Incrément 1 — ce qui a été implémenté

Réalisé : migrations `037_kiosk_module` / `038_kiosk_existing_tables`, module
`internal/modules/kiosk/` (models, repository, service, handler,
admin_handler), `internal/middleware/kiosk_auth.go`, branchement dans
`cmd/api/routes.go`. `go build ./...` passe sans erreur.

### Écarts par rapport aux décisions G.1 / G.2 ci-dessus

- **G.1 (auth device)** : implémenté avec rotation de refresh token
  (`kiosk_device_tokens`, option 2 recommandée) **+** un access token
  auto-porteur signé HMAC-SHA256 (pepper `KIOSK_TOKEN_PEPPER`, ou
  `PIN_PEPPER` en fallback), non persisté en base. Ce n'est pas un JWT
  (pas de librairie JWT, pas de format de claims standard), mais le
  principe est similaire : ça évite un lookup SQL sur la table à 1 connexion
  partagée à chaque heartbeat. Conséquence : une borne révoquée reste
  utilisable jusqu'à l'expiration de son access token déjà émis (15 min par
  défaut, `KIOSK_ACCESS_TOKEN_TTL_MINUTES`) — le refresh, lui, est bloqué
  immédiatement.
- **G.2 (backfill disponibilité)** : tranché en faveur du backfill —
  `is_available_on_kiosk BOOLEAN NOT NULL DEFAULT TRUE`, donc tous les
  produits existants restent visibles sur Kiosk sans action du
  restaurateur (pas d'héritage de `is_available_on_sno`, juste `TRUE` par
  défaut — plus simple, écarte le risque "tout invisible au lancement").
- **orders.kiosk_id** : `VARCHAR(64)` stockant `kiosks.public_id`
  (`KIOSK-<uuid>`), sans contrainte FK — cohérent avec "pas de FK vers une
  table historique".

### Contrainte d'import à connaître

`internal/middleware/kiosk_auth.go` importe `internal/modules/kiosk` (pour
le type `AuthenticatedKiosk`). Cela interdit au module `kiosk` d'importer
`internal/middleware` ou `internal/config` en retour (cycle d'import) :
- Le contexte "borne authentifiée" est lu via `kiosk.FromContext(ctx)`
  (vit dans le module lui-même), pas via un import de `middleware` côté
  handler.
- L'identité de l'utilisateur back-office (pour les routes admin) est
  injectée par `routes.go` via `kiosk.WithAdminUser(ctx, ...)`, lue côté
  handler via `kiosk.AdminUserFromContext(ctx)` — `routes.go` fait le pont
  entre `middleware.GetUser(r)` et ce contexte dédié.
- La config (`KioskConfig`) est définie comme `type Config struct{...}`
  dans le module kiosk lui-même ; `internal/config/kiosk.go` ne fait que
  l'aliaser (`type KioskConfig = kiosk.Config`), même pattern que
  `config.UberEatsConfig = ubereats.ConfigUberEats`.

### Variables d'environnement ajoutées

- `KIOSK_TOKEN_PEPPER` (optionnel, retombe sur `PIN_PEPPER`)
- `KIOSK_ENROLLMENT_CODE_TTL_MINUTES` (défaut 15)
- `KIOSK_DEVICE_TOKEN_TTL_DAYS` (défaut 30)
- `KIOSK_ACCESS_TOKEN_TTL_MINUTES` (défaut 15)

### Tests manuels incrément 1

**Non exécutés dans cette session** : l'environnement d'implémentation n'a
pas accès à `MYSQL_URL` / `REDIS_URL` / aux identifiants du projet (pas de
`.env` dans le repo, conforme à CLAUDE.md). Seul `go build ./...` a pu être
vérifié. Avant mise en prod, exécuter ce scénario sur un environnement avec
DB/Redis configurés, après avoir :
1. Appliqué `037_kiosk_module.up.sql` puis `038_kiosk_existing_tables.up.sql`.
2. Mis `subscriptions.max_kiosks >= 1` pour le merchant de test.

```bash
BASE_URL="http://localhost:8080"
USER_TOKEN="<token d'un user back-office authentifié>"

# 1. Génération d'un code d'enrôlement (back-office)
curl -s -X POST "$BASE_URL/pos/settings/kiosk/enrollment-codes" \
  -H "Authorization: Bearer $USER_TOKEN" | tee enroll_code.json
# -> { "id": "kiosk.generate_enrollment_code", "data": { "code": "AB3D9F2K", "expires_at": "..." } }

CODE=$(jq -r .data.code enroll_code.json)

# 2. Enrôlement de la borne (device, public)
curl -s -X POST "$BASE_URL/kiosk/auth/enroll" \
  -H "Content-Type: application/json" \
  -d "{\"enrollment_code\":\"$CODE\",\"name\":\"Borne Salle 1\",\"hardware_model\":\"Elo 1502L\",\"os_version\":\"Android 13\",\"app_version\":\"1.0.0\"}" \
  | tee enroll.json
# -> { "data": { "kiosk_id": "KIOSK-...", "access_token": "...", "refresh_token": "...", "expires_at": "..." } }

ACCESS_TOKEN=$(jq -r .data.access_token enroll.json)
REFRESH_TOKEN=$(jq -r .data.refresh_token enroll.json)

# 3. Heartbeat (device, protégé KioskAuth)
curl -s -X POST "$BASE_URL/kiosk/auth/heartbeat" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"app_version":"1.0.1"}'
# -> { "data": { "status": "ok" } }

# 4. Refresh du token
curl -s -X POST "$BASE_URL/kiosk/auth/token/refresh" \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\":\"$REFRESH_TOKEN\"}" | tee refresh.json
# -> nouveau access_token + refresh_token (rotation : l'ancien refresh_token est révoqué)

# 5. Vérifier que l'ancien refresh_token est bien révoqué (doit échouer)
curl -s -X POST "$BASE_URL/kiosk/auth/token/refresh" \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\":\"$REFRESH_TOKEN\"}"
# -> { "data": { "error": "kiosk_device_token_invalid", ... } } attendu

# 6. Liste des bornes (back-office)
curl -s "$BASE_URL/pos/settings/kiosk/devices" -H "Authorization: Bearer $USER_TOKEN"

# 7. Révocation de la borne (back-office)
KIOSK_ID=$(jq -r .data.kiosk_id enroll.json)
curl -s -X POST "$BASE_URL/pos/settings/kiosk/devices/$KIOSK_ID/revoke" \
  -H "Authorization: Bearer $USER_TOKEN"
# -> { "data": { "status": "revoked" } }

# 8. Vérifier que le refresh échoue désormais pour cette borne
NEW_REFRESH=$(jq -r .data.refresh_token refresh.json)
curl -s -X POST "$BASE_URL/kiosk/auth/token/refresh" \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\":\"$NEW_REFRESH\"}"
# -> kiosk_device_token_invalid attendu (RevokeKiosk révoque tous les
# refresh tokens, donc le rejet se fait au niveau du token avant même
# d'atteindre la vérification du statut "revoked" de la borne)
```

---

## Incrément 2 — menu, upsell, pricing, commandes (pay-at-counter)

Réalisé : extension de `internal/modules/kiosk/{models,repository,service,handler}.go`,
routes `/kiosk/{menu,products/{id},settings,upsell,pricing,orders,...}` (toutes
sous `middleware.KioskAuth`), aucune modification de `menuService`,
`ordersService`, `ordersLifeCycleService` ni `upsellService` — uniquement
consommés. `go build ./...` passe. Tests existants non affectés (`go test
./...` montre les mêmes échecs pré-existants dans `planning/employees` et
`planning/leave`, sans rapport avec ce module — vérifié via `git status`,
aucun fichier planning touché).

### Écart majeur découvert : cycle d'import middleware ↔ kiosk

L'incrément 1 avait fait porter `AuthenticatedKiosk` et `KioskContextKey`
par le module `kiosk` (`middleware/kiosk_auth.go` important `kiosk` pour ce
type). Tant que `kiosk.Service` n'avait pas de dépendance transverse, ça
fonctionnait. Dès que ce service consomme `menuService`/`ordersService`/
`ordersLifeCycleService` (qui importent tous `middleware` pour
`middleware.UserFromContext`), la chaîne devient :
`middleware -> kiosk -> menu -> middleware` → cycle d'import, build cassé.

**Fix** : `AuthenticatedKiosk` et la clé de contexte vivent maintenant dans
`internal/middleware/kiosk_auth.go` (pas dans `kiosk`). `kiosk.AuthenticatedKiosk`
est un simple alias (`type AuthenticatedKiosk = middleware.AuthenticatedKiosk`)
— le sens de dépendance est désormais strictement `kiosk -> middleware`,
jamais l'inverse. Même cause that avait nécessité l'alias `KioskConfig =
kiosk.Config` côté `internal/config` en incrément 1 ; **cet alias a dû être
abandonné** pour la même raison (`config -> kiosk -> menu -> deliveroo ->
config`) : `internal/config/kiosk.go` redevient une struct plate
indépendante, convertie explicitement en `kiosk.Config` dans `routes.go`
au moment de construire le service. Conséquence pratique pour la suite :
**le module kiosk peut désormais importer librement `middleware` et tout
module métier**, mais aucun de ces modules (ni `internal/config`) ne doit
jamais importer `kiosk` en retour.

Bénéfice collatéral : comme `kiosk` importe maintenant `middleware`
directement, le mécanisme `AdminUser`/`WithAdminUser`/`AdminUserFromContext`
(bricolage de l'incrément 1 pour contourner ce même problème) a été
supprimé — `admin_handler.go` appelle directement `middleware.GetUser(r)`,
comme tous les autres modules protégés par `authMiddleware`. `routes.go`
n'a donc plus besoin du middleware wrapper ad-hoc sur `/pos/settings/kiosk`.

### product_id : string, pas int64 (écart par rapport au brief)

`products.product_id` est un `VARCHAR` (UUID applicatif), pas un entier —
voir `internal/models/menu_models.go` (`ProductEntry.ProductID string`) et
`docs/ARCHITECTURE_API.md` §6.2. Tous les identifiants produit du module
Kiosk (`KioskProduct.ID`, `KioskOrderItem.ProductID`, `cart_product_ids`,
etc.) sont donc des `string`, jamais des `int64` comme demandé littéralement
dans le brief — c'eût été incompatible avec le schéma réel.

### Filtre `is_available_on_kiosk` : méthode repo dédiée, pas un paramètre menuService

`menuService.GetMenuFromMerchantIdWithMarketing`/`GetProductFromMerchantId`
ne savent rien de la colonne `is_available_on_kiosk` (ajoutée en incrément 1
sur `products`, jamais branchée dans le module `menu`). Plutôt que de
modifier `menu` (interdit), `kiosk.Repository` expose deux méthodes dédiées :
- `GetKioskProductAvailabilityMap(ctx, merchantID)` — carte complète
  `product_id -> bool`, utilisée par `GetMenu` pour filtrer le menu déjà
  récupéré auprès de `menuService` (même logique de flatten groupe/sous-
  produits que `scannorder.ComputeGetMenu`, mais sur cette carte au lieu de
  `IsAvailableOnSNO`).
- `GetAvailableKioskProductIDs(ctx, merchantID, productIDs)` — version
  ciblée (sous-ensemble d'IDs), utilisée par `GetProduct`, l'upsell et la
  validation de panier (`buildOrderProducts`) : un produit absent du
  résultat est traité comme invalide, qu'il n'existe pas ou qu'il soit
  simplement masqué sur la borne (même conséquence côté sécurité dans les
  deux cas).

### Pourquoi `ComputePricing` (orders) suffit déjà à re-valider les prix

`orders.OrdersService.ComputePricing` ignore déjà entièrement les prix
envoyés par le client : `buildSelectedProducts` recalcule `Price`/`TvaRate`
depuis `dbp` (lookup DB par `product_id`), et `applyConfigurationOptionPrices`
réécrase `ExtraPrice` depuis `configurable_attribute_options`. Le seul trou
réel : un `product_id` totalement inconnu (ou masqué côté Kiosk) n'est pas
rejeté, il reçoit silencieusement un prix de `0` (DBProduct zero-value).
`kiosk.buildOrderProducts` comble ce trou en validant existence +
`is_available_on_kiosk` **avant** d'appeler `ComputePricing` — c'est
l'équivalent fonctionnel de `scannorder.validateAndCleanPricingPayload`,
mais en amont plutôt qu'en nettoyage après coup, et sans dupliquer la
logique de calcul de prix elle-même (déjà sécurisée côté `orders`).

### Statut "pending_counter_payment"

**Ce n'est pas une nouvelle valeur stockée en base.** C'est le nom Kiosk
(JSON `status`) de la valeur existante `orders.merchant_approval =
"PENDING_APPROVAL"` — déjà utilisée partout dans le projet pour "commande
créée, pas encore validée" (c'est l'état initial des commandes ScanNOrder
`TAKE_AWAY`/`DELIVERY`, voir `scannorder.CreateOrderSNO`). Introduire une
valeur inédite aurait rendu les commandes Kiosk invisibles à tout code qui
filtre explicitement sur `'PENDING_APPROVAL'` (listes "en attente" du
back-office, etc.) — exactement le risque signalé dans le brief.

Conséquence sur le découpage des deux endpoints :
- `CreateKioskOrder` crée la commande directement avec
  `MerchantApproval = "PENDING_APPROVAL"` (sans ça, `setOrderDefaults` la
  mettrait à `"ACCEPTED"` par défaut — comportement à éviter ici, le
  paiement comptoir n'a pas encore eu lieu).
- `ConfirmCounterPayment` **ne fait pas de transition d'état** : il
  récupère la commande (déjà `PENDING_APPROVAL`), génère `pickup_code`
  (= `orders.order_num`, déjà généré par `OrdersLifeCycleRepository.CreateOrder`)
  et `qr_payload`, puis rebroadcast `notification.NotificationTypeOrderUpdate`
  pour que l'écran comptoir/back-office se rafraîchisse. Le passage réel à
  `"ACCEPTED"` (commande encaissée, part en cuisine) est un geste staff qui
  existe déjà ailleurs dans le projet (`OrdersLifeCycleService.AcceptOrder`,
  routes `/orders/{order_id}/accept`) — non dupliqué ici, hors périmètre de
  ce qui était demandé pour `ConfirmCounterPayment`.

### Annulation : `DeleteOrder` direct, pas `SetOrderDeleted`

`OrdersLifeCycleService.SetOrderDeleted` (et `AcceptOrder`) appellent
`middleware.UserFromContext(ctx)` en interne — incompatible avec un appelant
Kiosk (pas d'utilisateur humain dans le contexte). `DeleteOrder` (la méthode
qu'ils enveloppent) ne dépend que des champs explicites de
`models.DenyOrderInput` ; `CancelKioskOrder` l'appelle directement.
Conséquence : pas d'entrée d'audit `ExecuteOrderMutation` pour les
annulations Kiosk dans cet incrément (le wrapper d'audit est précisément ce
que `SetOrderDeleted` ajoute par-dessus `DeleteOrder`) — à revoir si l'audit
des annulations Kiosk devient un besoin explicite.

`deletion_reason_id` n'est pas une contrainte FK stricte dans ce projet
(`OrdersLifeCycleRepository.DeleteOrderLocal` stocke la valeur telle quelle).
Une borne n'a pas accès à la liste de motifs configurés par le restaurateur
(`deletion_reasons`, table par merchant) ; `CancelKioskOrder` utilise donc la
valeur littérale `"KIOSK_CUSTOMER_CANCELLED"`, jamais validée contre cette
table — acceptable puisqu'aucune contrainte ne l'exige, mais à signaler si
les rapports d'annulation par motif doivent un jour inclure les bornes.

### Upsell : mapping des sources internes vers `apriori` / `featured_fallback`

`upsell.Service.GenerateUpsell` (moteur Apriori réel, partagé avec
`orders.GetUpsell` — pas le `scannorder.GetUpsell` simplifié qui ne fait que
filtrer `is_popular`) retourne l'une de : `pattern`, `cached_pattern`, `llm`,
`cached_llm`, `featured_fallback`, `disabled`, `error_fallback`. Le brief
demandait `"apriori"` ou `"featured_fallback"` côté Kiosk : `pattern` /
`cached_pattern` / `llm` / `cached_llm` sont regroupés sous `"apriori"`
(ce sont les sources qui ne sont *pas* le simple fallback "produits
populaires") ; tout le reste passe tel quel.

### Idempotence des commandes

Clé Redis `kiosk:idempotency:{kiosk_id}:{idempotency_key}` (scope par borne,
pas seulement par clé brute, pour éviter toute collision improbable entre
deux bornes), valeur = `CreateKioskOrderResponse` sérialisé JSON, TTL 10 min.
Si `idempotency_key` est vide, aucune déduplication n'est appliquée (chaque
appel crée une commande) — choix délibéré pour rester permissif plutôt que
de bloquer un client qui omettrait ce champ.

### Tests manuels incrément 2

Mêmes limites qu'à l'incrément 1 : pas de `MYSQL_URL`/`REDIS_URL` dans ce
sandbox, donc non exécutés réellement — seul `go build ./...` /
`go test ./...` ont pu être vérifiés. Pré-requis avant exécution réelle :
un produit avec `is_available_on_kiosk = TRUE`, `kiosk_settings.pay_at_counter_enabled = TRUE`
et `fulfillment_dine_in/take_away = TRUE` pour le merchant de test
(valeurs par défaut si la ligne n'existe pas encore — voir incrément 1).

```bash
BASE_URL="http://localhost:8080"
ACCESS_TOKEN="<access_token obtenu via /kiosk/auth/enroll ou /kiosk/auth/token/refresh>"
AUTH="Authorization: Bearer $ACCESS_TOKEN"

# 1. Menu filtré is_available_on_kiosk, avec ETag
curl -s -D - "$BASE_URL/kiosk/menu" -H "$AUTH" -o menu.json
ETAG=$(grep -i '^etag:' /dev/stdin <<< "$(curl -sI "$BASE_URL/kiosk/menu" -H "$AUTH")" | awk '{print $2}' | tr -d '\r')

# 1bis. Requête conditionnelle -> 304 attendu si rien n'a changé
curl -s -o /dev/null -w "%{http_code}\n" "$BASE_URL/kiosk/menu" -H "$AUTH" -H "If-None-Match: $ETAG"
# -> 304

PRODUCT_ID=$(jq -r '.data.categories[0].products[0].id' menu.json)

# 2. Détail produit
curl -s "$BASE_URL/kiosk/products/$PRODUCT_ID" -H "$AUTH"

# 3. Paramètres borne (timeout, fulfillment, pager, apparence)
curl -s "$BASE_URL/kiosk/settings" -H "$AUTH"

# 4. Upsell pour un panier d'un seul produit
curl -s -X POST "$BASE_URL/kiosk/upsell" -H "$AUTH" -H "Content-Type: application/json" \
  -d "{\"cart_product_ids\":[\"$PRODUCT_ID\"]}"

# 5. Pricing (prévisualisation, aucune commande créée)
curl -s -X POST "$BASE_URL/kiosk/pricing" -H "$AUTH" -H "Content-Type: application/json" \
  -d "{\"fulfillment_type\":\"DINE_IN\",\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":2}]}"

# 6. Création de commande (pay_at_counter), idempotency_key fixe
IDK="test-$(date +%s)"
curl -s -X POST "$BASE_URL/kiosk/orders" -H "$AUTH" -H "Content-Type: application/json" \
  -d "{\"fulfillment_type\":\"DINE_IN\",\"idempotency_key\":\"$IDK\",\"payment_method\":\"pay_at_counter\",\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":2}]}" \
  | tee order.json
# -> { "data": { "order_id": "...", "display_number": "...", "status": "pending_counter_payment", "total_cents": ... } }

# 6bis. Rejouer la même requête (même idempotency_key) -> doit renvoyer EXACTEMENT la même réponse, pas de doublon en base
curl -s -X POST "$BASE_URL/kiosk/orders" -H "$AUTH" -H "Content-Type: application/json" \
  -d "{\"fulfillment_type\":\"DINE_IN\",\"idempotency_key\":\"$IDK\",\"payment_method\":\"pay_at_counter\",\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":2}]}"

ORDER_ID=$(jq -r .data.order_id order.json)

# 7. Suivi de la commande
curl -s "$BASE_URL/kiosk/orders/$ORDER_ID" -H "$AUTH"

# 8. Confirmation comptoir : pickup_code + qr_payload + notification WS
curl -s -X POST "$BASE_URL/kiosk/orders/$ORDER_ID/counter-payment" -H "$AUTH"
# -> { "data": { "order_id": "...", "pickup_code": "...", "qr_payload": "KIOSK:<order_id>:<pickup_code>", "status": "pending_counter_payment" } }

# 9. Annulation (doit échouer une fois la commande déjà acceptée par le staff ailleurs)
curl -s -X DELETE "$BASE_URL/kiosk/orders/$ORDER_ID" -H "$AUTH"
# -> { "data": { "status": "cancelled" } } si toujours PENDING_APPROVAL
```

---

## Incrément 3 — CRUD bornes, enrollment codes, settings, uploads R2

### Inventaire de départ (avant cet incrément)

`admin_handler.go` exposait : `GenerateEnrollmentCode`, `ListKioskDevices`,
`RevokeKioskDevice`, `GetKioskSettings`, `UpdateKioskSettings`. Pas de
détail/édition/enable/disable de borne, pas de liste/révocation de codes
d'enrôlement, pas d'upload logo/idle-image. `handler.go` (device, incrément
1-2) était déjà complet pour `/kiosk/settings` côté Flutter (tous les champs
demandés étaient déjà retournés, y compris `logo_url`/`idle_image_url`/
`primary_color` — aucun changement nécessaire sur cette route).

### Bug critique trouvé et corrigé : insertion d'un ID préfixé dans une colonne BIGINT AUTO_INCREMENT

`CreateDeviceToken`, `RotateDeviceToken` (table `kiosk_device_tokens`) et
`CreateEnrollmentCode` (table `kiosk_enrollment_codes`) inséraient
`helpers.GeneratePrefixedID("ksk-dev-tkn"/"ksk-enrl-cd")` — une chaîne — dans
la colonne `id`, qui est `BIGINT UNSIGNED AUTO_INCREMENT` dans les deux
tables (migration 037). En mode SQL strict, cet INSERT échoue purement et
simplement (valeur non numérique dans une colonne entière) — l'enrôlement
d'une borne et la génération d'un code d'enrôlement étaient donc cassés à
l'exécution malgré un `go build` propre. **Fix** : les deux méthodes
omettent désormais la colonne `id` et laissent MySQL gérer l'auto-incrément
— ces deux tables n'ont pas besoin d'identifiant public exposé au client
(le refresh token et le code d'enrôlement, déjà générés séparément, jouent
ce rôle).

### Axe 1 — préfixes des identifiants publics [DÉCIDÉ]

Convention du projet observée dans `internal/helpers/ids.go` : préfixes
**courts, en minuscules, avec tirets**, déclarés comme constantes nommées
(`PrinterIDPrefix = "printer"`, `TagIDPrefix = "tag"`, etc.), jamais en
dur dans l'appelant. `helpers.GeneratePrefixedID("KIOSK")` (majuscules,
littéral) ne respectait pas cette convention.

- **`kiosks.public_id`** : nouvelle constante `helpers.KioskIDPrefix = "kiosk"`
  (au lieu du littéral `"KIOSK"`), utilisée dans `Service.EnrollDevice`.
  Format : `kiosk-<uuid>` (avant : `KIOSK-<uuid>`). Les migrations déjà
  exécutées (037/038, dossier `migrations/done/`) ne sont pas modifiées —
  seul leur commentaire SQL mentionne encore l'ancienne casse, sans impact
  fonctionnel (la colonne est un simple `VARCHAR`).
- **`kiosk_enrollment_codes`** : pas de `public_id`. L'identifiant
  pertinent exposé au restaurateur est le **code lui-même** (8 caractères
  alphanumériques majuscules, alphabet sans `0/O/1/I/L` pour éviter les
  confusions de lecture à l'écran — déjà implémenté par
  `generateEnrollmentCode`, inchangé). La ligne en base n'a qu'un id
  technique auto-incrémenté, jamais exposé seul (sauf dans la nouvelle
  liste back-office, où il sert de clé d'action pour le `DELETE`, voir
  Axe 3 ci-dessous).
- **`kiosk_device_tokens`** : pas de `public_id`. Le token opaque
  (`helpers.GenerateToken(32)`, hex 64 caractères) est déjà la bonne
  fonction de génération pour un secret — aucun changement.

### Axe 2 — CRUD bornes [DÉCIDÉ]

Endpoints ajoutés : `GET /devices/{device_id}`, `PUT /devices/{device_id}`,
`POST /devices/{device_id}/enable`, `POST /devices/{device_id}/disable`.
`POST /devices/{device_id}/revoke` existait déjà et était déjà correct
(passe `status='revoked'` **et** révoque tous les `kiosk_device_tokens` via
`RevokeAllDeviceTokens` dans la même transaction) — aucune modification
nécessaire sur ce point, juste vérifié.

- **enable** : vérifie le quota `max_kiosks` (même logique que
  l'enrôlement : `GetActiveKioskCount` compte `status IN ('pending','active')
  AND enabled = TRUE`) avant de repasser la borne en `active`/`enabled=true`
  — sauf si elle est déjà `active` (no-op de quota, pour ne pas bloquer un
  appel idempotent).
- **disable** : passe en `inactive`/`enabled=false`, **ne touche pas**
  `kiosk_device_tokens` — une borne désactivée peut se réactiver sans
  ré-enrôlement. `RecordHeartbeat`/`RefreshDeviceToken` ne bloquent que sur
  `status == "revoked"` : une borne `inactive` continue donc de répondre au
  heartbeat/refresh tant que son token n'a pas expiré naturellement — voulu
  (disable ≠ revoke), mais à signaler si le besoin réel est qu'une borne
  désactivée arrête immédiatement de répondre.
- Réponse `GET/PUT/enable/disable` réutilise désormais un mapper unique
  `toKioskDeviceResponse` (factorisation avec `ListKioskDevices`), qui
  inclut maintenant aussi `last_ip` et `enabled` (absents de la version
  précédente de `KioskDeviceResponse`).

### Axe 3 — Enrollment codes [DÉCIDÉ]

`GET /enrollment-codes` : liste les codes `used_at IS NULL AND expires_at >
NOW()`, triés par `created_at DESC`. Champs : `id` (identifiant technique
interne, jamais exposé ailleurs que dans cette liste — sert uniquement à
cibler le `DELETE`), `created_at`, `expires_at`, `used_at` (toujours `null`
ici puisque la requête filtre les codes déjà utilisés — gardé dans la
réponse pour la cohérence du modèle plutôt que de l'omettre).

`DELETE /enrollment-codes/{code_id}` : **suppression définitive** de la
ligne (pas de marquage `used_at = NOW()`) — la table ne porte pas de notion
de "révoqué" distincte de "utilisé", et réutiliser `used_at` pour ça aurait
rendu ce champ ambigu (consommé via enrôlement vs. supprimé sans avoir
servi). Comportement :
- code introuvable **ou expiré** → 404 (`kiosk_enrollment_code_not_found`)
  — un code expiré n'a plus d'existence actionnable côté back-office, donc
  même statut que "introuvable".
- code déjà utilisé → 409 (`kiosk_enrollment_code_already_used`) — sentinelle
  **distincte** de `ErrKioskEnrollmentCodeUsed`/`Expired` déjà utilisées par
  le flux d'enrôlement (`POST /kiosk/auth/enroll`, qui répond 401 sur ces
  mêmes conditions) : le mapping erreur → HTTP est global par sentinelle
  dans `SendErrorJSON`, donc une même condition logique appelant un statut
  différent selon le contexte (401 pour un device qui se trompe de code,
  404/409 pour un staff qui gère ses codes en attente) exige deux
  sentinelles séparées plutôt qu'une seule réutilisée.

### Axe 4 — Settings [DÉCIDÉ]

`GET /settings` (back-office) et `GET /kiosk/settings` (device) étaient
déjà conformes (tous les champs demandés, défauts si pas de ligne, jamais
de 404) — vérifiés, non modifiés.

`PUT /settings` : `logo_url`/`idle_image_url` **retirés** de
`UpdateKioskSettingsRequest` — ce endpoint n'accepte/n'écrase plus ces
champs (avant cet incrément, un PATCH sur `/settings` pouvait pointer ces
champs vers une URL arbitraire fournie par le client, sans passer par R2 —
faille mineure de cohérence, corrigée). Ils ne sont modifiables que par les
deux nouveaux endpoints d'upload. `primary_color` est désormais validé
(`^#[0-9A-Fa-f]{6}$`, ou `null`) — `400 kiosk_invalid_color` sinon.

`POST /settings/logo` et `POST /settings/idle-image` : multipart
(`file`), JPEG/PNG/WebP uniquement, 2 Mo max, clé R2 déterministe
`wello_resto_images_storage/merchants/{merchant_id}/kiosk/{logo|idle}{ext}`
(préfixe `wello_resto_images_storage/...` réutilisé du pattern existant
`r2.GenerateScanNOrderKey`/`GenerateProductKey`, pas du chemin littéral
`merchants/{merchant_id}/kiosk/...` du brief — cohérence avec le reste du
bucket plutôt que littéralité du brief). Même séquence que
`MenuHandler.UploadProductImage` : récupérer l'ancienne URL, supprimer
l'ancien fichier R2 (best-effort, non bloquant), uploader le nouveau,
persister l'URL dans `kiosk_settings` (upsert, créé si la ligne n'existait
pas encore).

### Tests manuels incrément 3

Mêmes limites que les incréments précédents (pas de `MYSQL_URL`/`REDIS_URL`
dans ce sandbox) : seuls `go build ./...` et `go vet ./...` ont pu être
vérifiés (propres sur tout le module kiosk et les fichiers touchés ;
les warnings restants de `go vet ./...` sont préexistants, sans rapport
avec ce module — `ubereats`, `auth`, `tasks`).

```bash
BASE_URL="http://localhost:8080"
USER_TOKEN="<token d'un user back-office authentifié>"
AUTH="Authorization: Bearer $USER_TOKEN"

# 1. Détail d'une borne
KIOSK_ID="kiosk-..."   # public_id obtenu via /pos/settings/kiosk/devices
curl -s "$BASE_URL/pos/settings/kiosk/devices/$KIOSK_ID" -H "$AUTH"

# 2. Renommer la borne
curl -s -X PUT "$BASE_URL/pos/settings/kiosk/devices/$KIOSK_ID" -H "$AUTH" \
  -H "Content-Type: application/json" -d '{"name":"Borne Entrée"}'

# 3. Désactiver puis réactiver
curl -s -X POST "$BASE_URL/pos/settings/kiosk/devices/$KIOSK_ID/disable" -H "$AUTH"
curl -s -X POST "$BASE_URL/pos/settings/kiosk/devices/$KIOSK_ID/enable" -H "$AUTH"
# -> 403 kiosk_max_kiosks_reached si le quota subscriptions.max_kiosks est dépassé

# 4. Générer un code, puis le lister
curl -s -X POST "$BASE_URL/pos/settings/kiosk/enrollment-codes" -H "$AUTH"
curl -s "$BASE_URL/pos/settings/kiosk/enrollment-codes" -H "$AUTH"

# 5. Révoquer un code avant usage
CODE_ID=$(curl -s "$BASE_URL/pos/settings/kiosk/enrollment-codes" -H "$AUTH" | jq -r '.data.codes[0].id')
curl -s -X DELETE "$BASE_URL/pos/settings/kiosk/enrollment-codes/$CODE_ID" -H "$AUTH"
# -> rejouer la même requête -> 404 (la ligne n'existe plus)

# 6. Settings : couleur invalide rejetée
curl -s -X PUT "$BASE_URL/pos/settings/kiosk/settings" -H "$AUTH" \
  -H "Content-Type: application/json" -d '{"primary_color":"not-a-color"}'
# -> 400 kiosk_invalid_color

# 7. Upload logo
curl -s -X POST "$BASE_URL/pos/settings/kiosk/settings/logo" -H "$AUTH" \
  -F "file=@./logo.png;type=image/png"
# -> { "data": { "logo_url": "https://.../wello_resto_images_storage/merchants/.../kiosk/logo.png" } }

# 8. Upload idle-image
curl -s -X POST "$BASE_URL/pos/settings/kiosk/settings/idle-image" -H "$AUTH" \
  -F "file=@./idle.jpg;type=image/jpeg"

# 9. Vérifier que les URLs sont bien répercutées sur /kiosk/settings (device)
curl -s "$BASE_URL/kiosk/settings" -H "Authorization: Bearer $ACCESS_TOKEN"
```

---

## Incrément 4 — order_notes/without_component_ids, idle-video, audit R2

### Audit 1 — helper R2 réutilisé

Package : `internal/infrastructure/r2` (`github.com/aws/aws-sdk-go-v2`, client S3
pointé vers l'endpoint R2 — pas un SDK Cloudflare dédié). Signature reprise
telle quelle, aucune modification du package :

```go
func (c *Client) UploadFile(ctx context.Context, key string, file io.Reader, contentType string) (string, error)
func (c *Client) DeleteFile(ctx context.Context, key string) error
func (c *Client) GetKeyFromURL(url string) string
```

Convention de clé déjà en place depuis l'incrément 3 :
`wello_resto_images_storage/merchants/{merchant_id}/kiosk/{logo|idle}{ext}`
(`r2.GenerateKioskKey(merchantID, imageType, ext)`), réutilisée à l'identique
pour la vidéo de veille (`imageType = "idle_video"`). Variables
d'environnement (déjà configurées, non modifiées) : `R2_ACCESS_KEY_ID`,
`R2_SECRET_ACCESS_KEY`, `R2_ENDPOINT`, `R2_PRIVATE_BUCKET`, `R2_PUBLIC_BASE_URL`
(noms exacts à vérifier dans `internal/config/r2.go` — non modifié ici, voir
`cmd/api/routes.go` pour la construction de `r2Client`).

### Audit 2 — POST /kiosk/orders avant cet incrément

`CreateKioskOrderRequest` et `KioskOrderItem` (équivalent du `OrderItemRequest`
du brief — pas de struct nommée `OrderItemRequest` dans ce module) **n'avaient
ni `order_notes` ni `without_component_ids`** avant cet incrément. Le panier
ne supportait que `notes` par item (commentaire libre, déjà existant,
transmis via `models.OrderItemCommentPayload`) — aucun moyen de retirer un
composant ("sans oignons") ni de laisser une note globale sur la commande.

### Axe 1 — order_notes / without_component_ids [FAIT]

- `CreateKioskOrderRequest.OrderNotes string` (`json:"order_notes,omitempty"`)
  → transmis tel quel à `orderReq.Comment` (`models.OrderRequest.Comment
  *string`, champ commande déjà existant et consommé partout ailleurs dans
  le projet — ScanNOrder, POS), uniquement si non vide.
- `KioskOrderItem.WithoutComponentIDs []string`
  (`json:"without_component_ids,omitempty"`) → mappé vers
  `models.OrderProductPayload.Without []*models.OrderWithoutPayload{ComponentID}`,
  exactement la même interface que celle déjà consommée par
  `orders.OrdersService.buildSelectedProducts` (voir `internal/modules/orders/service.go:529`)
  pour les commandes POS/ScanNOrder — aucune nouvelle interface introduite
  côté `ordersLifeCycleService`/`ordersService`.
- **Pas de validation d'existence des `component_id`** contre une table de
  composants : `Without` est purement informationnel côté `orders`
  (n'affecte jamais le prix recalculé, voir `buildSelectedProducts`), et
  aucune autre intégration du projet (POS, ScanNOrder) ne le valide non plus
  — ajouter une validation ici aurait introduit une contrainte absente du
  reste du projet sans bénéfice de sécurité (contrairement aux
  `selected_option_ids`, qui eux influencent le prix et sont déjà validés
  contre `configurable_attribute_options` dans `buildOrderProducts`).
- `KioskPricingItem` (`POST /kiosk/pricing`, prévisualisation) n'a **pas**
  reçu `without_component_ids` — hors périmètre du brief (qui ne mentionne
  que `POST /kiosk/orders`), et la prévisualisation de prix n'a de toute
  façon aucun intérêt à connaître les composants retirés puisque ça n'affecte
  pas le total.

### Axe 3 — Upload R2 idle-video [FAIT]

- Migration `migrations/todo/039_kiosk_settings_idle_video.{up,down}.sql` :
  `ALTER TABLE kiosk_settings ADD COLUMN idle_video_url VARCHAR(500) NULL
  DEFAULT NULL AFTER idle_image_url` — placée dans `migrations/todo/` (pas
  encore appliquée en base, contrairement à 037/038 qui sont dans
  `migrations/done/`).
- `POST /pos/settings/kiosk/settings/idle-video` : multipart (`file`),
  `video/mp4`/`video/webm` uniquement (validation via
  `r2.ValidateVideoType`, nouvelle fonction ajoutée au package `r2` —
  symétrique à `ValidateImageType`, n'altère pas le comportement existant
  des uploads image), 50 Mo max. Clé R2 :
  `wello_resto_images_storage/merchants/{merchant_id}/kiosk/idle_video{ext}`
  (`r2.GenerateKioskKey(merchantID, "idle_video", ext)`, même fonction que
  logo/idle-image — pas de nouvelle fonction de clé). Même séquence que les
  uploads image : récupération de l'ancienne URL, suppression best-effort de
  l'ancien fichier R2, upload, upsert `kiosk_settings.idle_video_url`.
- `KioskSettingsResponse.IdleVideoURL` ajouté — répercuté automatiquement sur
  `GET /pos/settings/kiosk/settings` **et** `GET /kiosk/settings` (device),
  les deux réutilisant `Service.GetSettings` sans modification de leur
  propre code.

### Axe 5 — Correction de cohérence trouvée : plafond logo/idle-image partagé à tort

L'incrément 3 avait introduit une seule constante
`maxKioskSettingsImageBytes = 2 << 20` partagée par `/settings/logo` **et**
`/settings/idle-image`, alors que le brief (et la cohérence produit — une
image de veille plein écran est plus lourde qu'un logo) demande 2 Mo pour le
logo mais **5 Mo** pour l'image de veille. **Corrigé** dans cet incrément :
trois constantes dédiées `maxKioskLogoBytes` (2 Mo), `maxKioskIdleImageBytes`
(5 Mo), `maxKioskIdleVideoBytes` (50 Mo) ; `uploadSettingsImage` prend
désormais le plafond en paramètre au lieu d'une constante globale.

### Tests manuels incrément 4

Mêmes limites que les incréments précédents (pas de `MYSQL_URL`/`REDIS_URL`
dans ce sandbox) : seuls `go build ./...` et `go vet ./...` ont été vérifiés
(propres sur le module kiosk ; mêmes avertissements préexistants et sans
rapport ailleurs — `ubereats`, `auth`, `tasks`). **Avant exécution réelle,
appliquer `migrations/todo/039_kiosk_settings_idle_video.up.sql`.**

```bash
BASE_URL="http://localhost:8080"
ACCESS_TOKEN="<access_token via /kiosk/auth/enroll ou /kiosk/auth/token/refresh>"
USER_TOKEN="<token d'un user back-office authentifié>"
PRODUCT_ID="<product_id is_available_on_kiosk=TRUE>"
COMPONENT_ID="<component_id existant sur ce produit>"

# 1. Commande avec note globale + composant retiré
curl -s -X POST "$BASE_URL/kiosk/orders" -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"fulfillment_type\":\"DINE_IN\",\"idempotency_key\":\"test-$(date +%s)\",\"payment_method\":\"pay_at_counter\",\"order_notes\":\"Table 4, allergie noix\",\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":1,\"without_component_ids\":[\"$COMPONENT_ID\"]}]}"
# -> commande créée, order_notes visible en back-office sur orders.comment,
#    without_component_ids visible sur le ticket cuisine (même chemin que POS)

# 2. Upload vidéo de veille
curl -s -X POST "$BASE_URL/pos/settings/kiosk/settings/idle-video" \
  -H "Authorization: Bearer $USER_TOKEN" -F "file=@./idle.mp4;type=video/mp4"
# -> { "data": { "idle_video_url": "https://.../wello_resto_images_storage/merchants/.../kiosk/idle_video.mp4" } }

# 3. Type vidéo invalide rejeté
curl -s -X POST "$BASE_URL/pos/settings/kiosk/settings/idle-video" \
  -H "Authorization: Bearer $USER_TOKEN" -F "file=@./idle.mov;type=video/quicktime"
# -> 400 invalid_video_type

# 4. Vérifier que idle_video_url apparaît côté device
curl -s "$BASE_URL/kiosk/settings" -H "Authorization: Bearer $ACCESS_TOKEN"
```

---

## Incrément 5 — simplification id/public_id

### Contexte trouvé en reprenant ce fil

Entre les incréments 3/4 et celui-ci, deux commits manuels ("fix: kiosk IDs
in string") avaient commencé ce refactor côté Go uniquement, sans toucher au
schéma SQL : `CreateDeviceToken`/`RotateDeviceToken` puis (non commité)
`CreateEnrollmentCode` généraient déjà un id préfixé (`helpers.GeneratePrefixedID("ksk-dev-tkn"/"ksk-enrl-cd")`,
littéraux non nommés) et l'inséraient dans la colonne `id` de
`kiosk_device_tokens`/`kiosk_enrollment_codes` — alors que ces colonnes
étaient toujours `BIGINT AUTO_INCREMENT` (migration 037, jamais modifiée).
C'est exactement le bug déjà documenté et corrigé une fois en incrément 3
("insertion d'un ID préfixé dans une colonne BIGINT AUTO_INCREMENT") qui
avait été réintroduit. Cet incrément corrige le problème à la racine en
migrant réellement le schéma plutôt qu'en Go seul.

### Décision : un seul id VARCHAR(64), pas de distinction technique/public

**Justification** : la distinction `id` (BIGINT interne, jamais exposé) /
`public_id` (VARCHAR exposé au client) a du sens quand une ressource est
accessible par un acteur non authentifié ou quand l'id technique fuite par
un canal non maîtrisé (URL publique indexée, export tiers...). Aucune route
Kiosk n'est dans ce cas :
- Les routes device (`/kiosk/...`) sont toutes derrière `middleware.KioskAuth`
  — seule la borne elle-même (qui connaît déjà son propre id, reçu à
  l'enrôlement) peut s'authentifier.
- Les routes back-office (`/pos/settings/kiosk/...`) sont toutes derrière
  `authMiddleware` et scopées au merchant authentifié
  (`GetKioskByIDForMerchant` vérifie `merchant_id = ?`) — un id qui fuite
  ne donne accès à rien sans être déjà staff de ce merchant.
- Aucun id n'apparaît jamais dans une URL publique sans authentification
  (contrairement à, par exemple, un lien de réinitialisation de mot de
  passe où l'opacité de l'id a un rôle de sécurité).

Garder deux identifiants n'apportait donc aucune protection réelle, juste de
la complexité (toujours choisir lequel passer à quelle méthode, deux champs
à synchroniser dans les réponses). **Décision : un seul `id VARCHAR(64)`**,
généré côté backend via `helpers.GeneratePrefixedID(...)` — même pattern que
tous les autres modules du projet (`helpers.UserIDPrefix`, `PrinterIDPrefix`,
etc.), avec deux nouvelles constantes ajoutées à `internal/helpers/ids.go` :
`KioskEnrollmentCodeIDPrefix = "kiosk-enrl-cd"`, `KioskDeviceTokenIDPrefix =
"kiosk-dev-tkn"` (`KioskIDPrefix = "kiosk"` existait déjà depuis
l'incrément 1).

### Génération déplacée dans le service, jamais dans le repository

Avant cet incrément, `CreateEnrollmentCode` générait son id directement dans
le repository (`helpers.GeneratePrefixedID("ksk-enrl-cd")` en dur dans la
requête SQL). Déplacé dans `Service.GenerateEnrollmentCode` /
`Service.EnrollDevice` / `Service.RefreshDeviceToken` — le repository ne
fait plus que persister un id qu'on lui fournit, cohérent avec le reste du
module (`CreateKiosk` prenait déjà `publicID` en paramètre avant cet
incrément, jamais généré lui-même).

### Migration 040 : fusion id + public_id, BIGINT → VARCHAR(64)

`migrations/todo/040_kiosk_simplify_ids.{up,down}.sql`. Portée réelle après
relecture des migrations 037/038 (l'état réel diffère de l'énoncé du
brief) :
- **`kiosks`** : avait à la fois `id BIGINT AUTO_INCREMENT` (PK) et
  `public_id VARCHAR(64)` (UNIQUE) — fusionnés en un seul `id VARCHAR(64)
  PRIMARY KEY` (ex-`public_id`, l'ancien `id` BIGINT est supprimé).
- **`kiosk_device_tokens`** et **`kiosk_enrollment_codes`** : n'avaient
  jamais de `public_id` séparé, seulement `id BIGINT AUTO_INCREMENT` — leur
  `id` devient directement `VARCHAR(64)` (généré côté Go), et leur colonne
  `kiosk_id` (FK vers `kiosks`) passe de `BIGINT UNSIGNED` à `VARCHAR(64)`
  pour suivre le nouveau type de `kiosks.id`.
- **`kiosk_settings`** : **aucun changement**. Cette table n'a jamais eu de
  colonne `id` — sa clé primaire est `merchant_id` (une ligne par merchant,
  même pattern que `scannorder_settings`/`merchant_parameters`). Le brief
  listait cette table par précaution mais il n'y avait rien à fusionner.
- **`orders.kiosk_id`** (migration 038) : déjà `VARCHAR(64)`, déjà rempli
  avec la valeur de `kiosks.public_id` — donc déjà cohérent avec le nouvel
  `kiosks.id` sans aucune migration de données ; `idx_orders_kiosk` reste
  valide tel quel.

La migration backfille les FK existantes avant de changer les types
(`UPDATE ... JOIN kiosks ...` pour mapper l'ancien `kiosks.id` BIGINT vers
`kiosks.public_id`) plutôt que de supposer qu'aucune donnée n'existe encore
— mesure de précaution, le module n'étant pas confirmé vide en
environnement réel à ce stade.

### Repository : méthode renommée, pas de doublon fonctionnel

`GetKioskByPublicID(ctx, merchantID, publicID)` → renommée
`GetKioskByIDForMerchant(ctx, merchantID, kioskID)` : même requête, juste
`public_id` remplacé par `id` dans le SQL et le nom mis à jour. `GetKioskByID(ctx,
kioskID)` (sans scope merchant) est conservée séparément — toujours
nécessaire pour `RefreshDeviceToken`, où l'appartenance au merchant est
déjà garantie par la résolution du refresh token (pas besoin d'un second
contrôle merchant à ce point).

---

### Tests manuels incrément 5

Mêmes limites que les incréments précédents (pas de `MYSQL_URL`/`REDIS_URL`
dans ce sandbox) : seuls `go build ./...` et `go vet ./...` ont été vérifiés
(clean sur le module kiosk ; mêmes avertissements préexistants et sans
rapport ailleurs). **Avant exécution réelle, appliquer dans l'ordre
`migrations/todo/039_kiosk_settings_idle_video.up.sql` puis
`migrations/todo/040_kiosk_simplify_ids.up.sql`** (039 n'a aucune dépendance
sur 040, mais l'ordre numérique doit être respecté par convention du
projet).

```bash
BASE_URL="http://localhost:8080"
USER_TOKEN="<token d'un user back-office authentifié>"

# 1. Enrôlement : vérifier que kiosk_id retourné est bien le nouveau format
#    "kiosk-<uuid>" (inchangé côté client, c'était déjà le format de
#    l'ancien public_id)
CODE=$(curl -s -X POST "$BASE_URL/pos/settings/kiosk/enrollment-codes" -H "Authorization: Bearer $USER_TOKEN" | jq -r .data.code)
curl -s -X POST "$BASE_URL/kiosk/auth/enroll" -H "Content-Type: application/json" \
  -d "{\"enrollment_code\":\"$CODE\",\"name\":\"Borne test\",\"hardware_model\":\"Elo\",\"os_version\":\"Android 13\",\"app_version\":\"1.0.0\"}" \
  | tee enroll.json
KIOSK_ID=$(jq -r .data.kiosk_id enroll.json)
ACCESS_TOKEN=$(jq -r .data.access_token enroll.json)

# 2. Détail back-office par id — doit fonctionner avec le même id reçu à l'enrôlement
curl -s "$BASE_URL/pos/settings/kiosk/devices/$KIOSK_ID" -H "Authorization: Bearer $USER_TOKEN"

# 3. Refresh token : vérifier la rotation fonctionne toujours (nouvel id généré pour le nouveau refresh token)
REFRESH_TOKEN=$(jq -r .data.refresh_token enroll.json)
curl -s -X POST "$BASE_URL/kiosk/auth/token/refresh" -H "Content-Type: application/json" \
  -d "{\"refresh_token\":\"$REFRESH_TOKEN\"}"

# 4. Révocation puis vérification que le device_id reste utilisable pour le lookup back-office
curl -s -X POST "$BASE_URL/pos/settings/kiosk/devices/$KIOSK_ID/revoke" -H "Authorization: Bearer $USER_TOKEN"
curl -s "$BASE_URL/pos/settings/kiosk/devices/$KIOSK_ID" -H "Authorization: Bearer $USER_TOKEN"
```

---

## Incrément 6 — statut kiosk temps réel (WebSocket)

### Événements WebSocket ajoutés

Constantes déclarées dans `internal/modules/notification/notification_models.go`
(même fichier que `NotificationTypeOrderUpdate`, seul endroit où des "types
d'événements WS" étaient déjà typés dans le projet — pas de fichier dédié WS
event créé pour rester cohérent avec l'existant) :

```go
const (
    WSEventKioskStatusChanged = "kiosk_status_changed"
    WSEventKioskUnavailable   = "kiosk_unavailable"
)
```

**`kiosk_status_changed`** — POS ou back-office vers la borne (en pratique :
diffusé à tous les clients WebSocket connectés du merchant, voir limite
ci-dessous) :
```json
{
    "type": "kiosk_status_changed",
    "kiosk_id": "kiosk-...",
    "status": "active" | "inactive",
    "enabled": true | false,
    "triggered_by": "pos" | "backoffice"
}
```

**`kiosk_unavailable`** — la borne vers le hub :
```json
{
    "type": "kiosk_unavailable",
    "kiosk_id": "kiosk-...",
    "reason": "connection_lost" | "app_error" | "manual"
}
```

### Diffusion : `NotificationService.BroadcastToMerchant` (nouvelle méthode)

Le hub WebSocket existant (`internal/infrastructure/websocket`) ne connaît
que `merchantID -> connID -> *Client` — pas de canal par device, voir
`ARCHITECTURE_API.md` §10.2. Aucune modification du Hub n'a été nécessaire
(confirmé conforme à §10.4) : `NotificationService.BroadcastToMerchant(merchantID,
payload map[string]interface{}) bool` sérialise le payload et appelle
`hub.BroadcastToMerchant` — même mécanisme que celui déjà utilisé en interne
par `SendNotificationAsync`, mais exposé publiquement et **sans** le volet
FCM (ces deux events ne déclenchent pas de notification push mobile,
seulement un rafraîchissement temps réel du POS/back-office déjà ouvert).
`kiosk.Service.broadcastKioskStatus`/`ReportUnavailable` appellent cette
méthode — best-effort, ne bloque jamais la mutation DB qui précède (si aucun
client n'est connecté, l'event est simplement perdu, voir fallback heartbeat
ci-dessous).

### Pas de nouvelle authentification WebSocket pour les bornes

Le brief soulevait la question d'un WebSocket dédié aux bornes (auth device
≠ auth humaine, voir `ServeWS` qui exige `middleware.GetUser(r)`). **Décision :
aucun nouveau WebSocket kiosk.** Une borne physique n'a aujourd'hui aucune
raison de recevoir des messages WS — elle n'a pas d'écran de supervision
multi-commandes à rafraîchir en push (son propre statut, elle peut le
redemander via heartbeat). Construire une seconde variante du Hub avec un
schéma d'auth différent (token kiosk auto-porteur vs `*auth.UserLoginRow`)
aurait dupliqué `Client`/`Register`/`Unregister`/`BroadcastToMerchant` pour
un besoin non confirmé. Conséquence pratique :
- `kiosk_status_changed` est reçu par les clients WS déjà connectés du
  merchant (POS, back-office) — **pas par la borne elle-même**, qui n'a pas
  de connexion WS. C'est le **heartbeat** (`POST /kiosk/auth/heartbeat`,
  toutes les 5 min côté Flutter) qui sert de mécanisme de fallback pour que
  la borne apprenne son propre statut — voir ci-dessous. Latence max avant
  qu'une borne désactivée à distance arrête réellement de prendre des
  commandes : la durée d'un cycle de heartbeat (5 min), sauf si une future
  itération ajoute un canal direct.
- `kiosk_unavailable` est **émis** par la borne via un nouvel endpoint REST
  protégé `KioskAuth` (`POST /kiosk/status/unavailable`), pas via une
  connexion WebSocket sortante de la borne — cohérent avec le choix
  ci-dessus (la borne ne parle qu'en REST authentifié par son access token,
  jamais en WS).

### `POST /pos/kiosk/{kiosk_id}/status` (POS Flutter, staff)

`kioskAdminHandler.SetKioskStatusFromPOS`, protégé `authMiddleware` (même
niveau de protection que les routes `/pos/settings/kiosk/*` existantes —
**pas** de `RequirePermission` dédié ajouté : aucune des routes kiosk
back-office actuelles n'en a, et le brief ne demande pas de restreindre
au-delà de "staff authentifié", donc rester cohérent avec l'existant plutôt
que d'introduire une permission Kiosk inédite sans avoir été demandée).
Réutilise `Repository.SetKioskStatusEnabled` (même méthode que
`Enable/DisableKioskDevice`, voir Incrément 1) et la vérification de quota
`max_kiosks` à l'activation (même logique que `EnableKioskDevice`).
`triggered_by = "pos"` dans l'event diffusé.

### Extension de `EnableKioskDevice`/`DisableKioskDevice` (back-office web)

Factorisation demandée par le brief : `broadcastKioskStatus(merchantID,
kioskID, enabled, triggeredBy)` est l'unique point d'appel du broadcast,
utilisé par les trois flux (POS, enable back-office, disable back-office) —
aucune duplication du payload `kiosk_status_changed`. `triggered_by =
"backoffice"` pour ces deux-là.

### Extension du heartbeat

`HeartbeatResponse` gagne `kiosk_status` et `enabled`, lus depuis la ligne
`kiosks` déjà chargée par `RecordHeartbeat` (`GetKioskByIDForMerchant`,
aucune requête SQL supplémentaire) :
```json
{ "commands": [], "kiosk_status": "active" | "inactive", "enabled": true | false }
```
Note : ce module n'a jamais eu de notion de `commands` (pas dans le scope de
cet incrément, pas trouvé ailleurs dans le module Kiosk) — le champ
`commands: []` mentionné dans le brief n'existe pas dans
`HeartbeatResponse` actuel et n'a pas été ajouté ici (aucune commande à
faire transiter par ce canal aujourd'hui) ; seuls `kiosk_status`/`enabled`
ont été ajoutés, conformément au besoin réel exprimé (fallback de statut si
le WebSocket est coupé).

### Tests manuels incrément 6

Mêmes limites que les incréments précédents (pas de `MYSQL_URL`/`REDIS_URL`
dans ce sandbox) : seul `go build ./...` a été vérifié (clean).

```bash
BASE_URL="http://localhost:8080"
USER_TOKEN="<token d'un user back-office/POS authentifié>"
ACCESS_TOKEN="<access_token via /kiosk/auth/enroll ou /kiosk/auth/token/refresh>"
KIOSK_ID="kiosk-..."

# 1. POS désactive une borne -> diffuse kiosk_status_changed (triggered_by=pos)
# (ouvrir une connexion WS sur le merchant avant cet appel pour observer l'event)
curl -s -X POST "$BASE_URL/pos/kiosk/$KIOSK_ID/status" -H "Authorization: Bearer $USER_TOKEN" \
  -H "Content-Type: application/json" -d '{"enabled":false}'

# 2. Heartbeat : la borne voit son statut même sans WebSocket
curl -s -X POST "$BASE_URL/kiosk/auth/heartbeat" -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" -d '{"app_version":"1.0.1"}'
# -> { "data": { "status": "ok", "kiosk_status": "inactive", "enabled": false } }

# 3. Back-office réactive -> triggered_by=backoffice
curl -s -X POST "$BASE_URL/pos/settings/kiosk/devices/$KIOSK_ID/enable" -H "Authorization: Bearer $USER_TOKEN"

# 4. La borne signale un problème -> diffuse kiosk_unavailable
curl -s -X POST "$BASE_URL/kiosk/status/unavailable" -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" -d '{"reason":"connection_lost"}'
# -> { "data": { "status": "ok" } }, last_error/last_error_at mis à jour en base

# 5. Reason invalide rejetée
curl -s -X POST "$BASE_URL/kiosk/status/unavailable" -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" -d '{"reason":"bogus"}'
# -> 400 invalid_input
```

---

## Incrément 7 — endpoint WebSocket dédié à la borne (`/ws-kiosk`)

### Diagnostic ayant invalidé la décision de l'incrément 6

L'incrément 6 concluait : "aucun nouveau WebSocket kiosk" car une borne n'a
"aujourd'hui aucune raison de recevoir des messages WS". Cette hypothèse
était fausse — vérifiée en relisant le code Flutter `wello-kiosk` réel : le
service `WebSocketService` du Kiosk consomme déjà 5 événements (`menu_updated`,
`availability_update`, `device_command`, `order_update`, `kiosk_status_changed`)
et `MenuController` est bien câblé sur les trois premiers depuis le début.
Sauf que `WebSocketService.connect()` s'authentifiait avec l'access token
device sur `/ws`, qui n'accepte que `middleware.Auth` (lookup Redis d'un
`users_rights.token` humain) — l'access token Kiosk (HMAC auto-porteur, jamais
stocké en Redis) n'y correspond jamais : `ServeWS` renvoyait 401 avant même
l'upgrade WebSocket. Donc **les 5 événements n'étaient jamais reçus par
aucune borne en pratique**, malgré un code Flutter syntaxiquement correct —
pas seulement `kiosk_status_changed` comme suspecté initialement.

### Décision révisée : endpoint `/ws-kiosk`, même Hub, middleware différent

Plutôt que documenter ce canal comme mort (option envisagée), un second
point d'entrée WebSocket est ajouté : `/ws-kiosk`, protégé par
`middleware.KioskAuth(kioskService)` au lieu de `authMiddleware`
(`cmd/api/routes.go`). Aucune duplication du `Hub`/`Client`/`Register`/
`Unregister`/`BroadcastToMerchant` — ces types sont déjà indifférents à
l'origine de la connexion (clé `merchantID -> connID -> *Client`).
`internal/infrastructure/websocket/handler.go` factorise désormais la
logique d'upgrade dans `serveWS(hub, w, r, merchantID)`, appelée par :
- `ServeWS` (inchangé pour l'appelant) : extrait `merchantID` via
  `middleware.GetUser(r)`.
- `ServeKioskWS` (nouveau) : extrait `merchantID` via
  `middleware.GetKiosk(r)`.

Conséquence : une borne reçoit désormais réellement tous les events broadcastés
à son merchant, y compris ceux destinés au POS/back-office (pas de filtrage
serveur par type de client) — c'est au client Flutter de filtrer, comme déjà
documenté côté `WebSocketService` (filtrage par `kiosk_id`/`order_id` côté
appelant, pas par le service).

### Pourquoi pas un canal séparé par device

Le `Hub` reste mono-canal par merchant (pas de canal par device) — une borne
reçoit donc aussi les events des autres bornes/POS du même merchant (ex:
`kiosk_status_changed` d'une autre borne, qu'elle ignore déjà côté client en
comparant `kiosk_id`). Introduire un routage par device aurait été une
sur-ingénierie pour un volume de bornes par merchant qui reste faible — à
réévaluer si ce volume devient significatif.

### Heartbeat conservé comme fallback (pas raccourci)

Le heartbeat (`POST /kiosk/auth/heartbeat`, 5 min côté Flutter) reste à son
intervalle actuel : avec `/ws-kiosk` fonctionnel, c'est désormais un
mécanisme de repli (borne qui rate un event WS pendant une reconnexion), pas
le canal primaire — la branche du brief qui demandait un raccourcissement à
30s ne s'applique donc plus (elle ne s'appliquait qu'au scénario "WebSocket
device impossible").

### Tests manuels incrément 7

```bash
BASE_URL="http://localhost:8080"
ACCESS_TOKEN="<access_token via /kiosk/auth/enroll ou /kiosk/auth/token/refresh>"

# Connexion WS device (wscat ou équivalent) :
wscat -c "$BASE_URL/ws-kiosk/" -H "Authorization: Bearer $ACCESS_TOKEN"
# -> connexion acceptée (pas de 401), contrairement à /ws/ avec ce même token

# Pendant que la connexion est ouverte, déclencher un broadcast merchant
# (ex: PUT /pos/settings/kiosk/devices/{id}/disable, ou tout event order_update
# du même merchant) -> l'event correspondant doit apparaître sur la connexion.
```

---

## Incrément 8 — nom obligatoire à l'enrôlement, PIN admin par borne

### Nom de la borne : `name`, pas `device_name`

Le brief demandait un champ `device_name`. Vérification faite : le contrat
réel de `POST /kiosk/auth/enroll` (`EnrollRequest.Name`, JSON `name`) existait
déjà depuis l'incrément 1, et le client Flutter `wello-kiosk`
(`lib/data/models/enroll_request.dart`) envoie déjà `name` (pas
`device_name`). Renommer le champ aurait cassé le client existant sans aucun
bénéfice — **le champ reste `name`**, seule sa validation change :
`validateKioskName` (`internal/modules/kiosk/service.go`) rejette désormais
une valeur vide ou de plus de 100 caractères (limite de `kiosks.name
VARCHAR(100)`, comptée en runes pour rester correcte avec des noms accentués)
avec `400 kiosk_name_invalid`. Réutilisée aussi par
`UpdateKioskDeviceName` (back-office), qui n'avait jusqu'ici qu'une
vérification "non vide".

### PIN admin par borne — chiffrement réversible, pas hash

**Révision** : la v1 de cet incrément stockait `admin_pin_hash` (HMAC-SHA256,
même pattern que `auth.pin_hash`) — un hash unidirectionnel ne permet par
construction aucune relecture du PIN. Besoin produit confirmé entre-temps :
le PIN doit pouvoir être **réaffiché depuis le POS** (technicien sans accès à
la borne physique, ou qui n'a pas noté le PIN affiché une seule fois à
l'enrôlement). Migration `migrations/todo/042_kiosk_admin_pin.up.sql` (encore
non appliquée, modifiée en place plutôt que dupliquée en 043) :
`kiosks.admin_pin_encrypted VARBINARY(255) NULL` à la place de
`admin_pin_hash`.

**Chiffrement réversible recherché dans le projet avant d'en créer un** :
aucun mécanisme réutilisable trouvé. Les seuls secrets actuellement persistés
en base (tokens Uber Eats `access_token`/`refresh_token`/`bearer_token` dans
`integration_uber_eats`) sont stockés **en clair** (`VARCHAR`), pas chiffrés
— rien à réutiliser. Le seul code AES existant du projet
(`helpers.EncryptPHP`, `internal/helpers/services_helpers.go`) est un AES-128
en mode **ECB** utilisé uniquement comme fallback de vérification de mot de
passe (one-way, jamais déchiffré) — ECB n'a pas de nonce/IV et n'est pas
adapté à un chiffrement réversible générique. **Nouveau helper créé** :
`internal/helpers/encryption.go`, `Encrypt(plaintext string) ([]byte, error)`
/ `Decrypt(ciphertext []byte) (string, error)`, AES-**256-GCM** (AEAD :
authentifie l'intégrité du ciphertext en plus de le chiffrer, contrairement à
ECB/CBC nu) — nonce de 12 octets généré aléatoirement à chaque appel et
préfixé au ciphertext retourné (pas de colonne séparée pour le stocker).

**Variable d'environnement** : `KIOSK_PIN_ENCRYPTION_KEY` — clé AES-256, 32
octets encodés en base64. Génération :
```bash
openssl rand -base64 32
```
Chargée une seule fois (`sync.Once`) au premier `Encrypt`/`Decrypt` ; absente
ou mal formée (pas du base64 valide, ou décodée à une longueur ≠ 32 octets)
→ erreur propagée telle quelle (pas de fallback silencieux en clair). Cette
clé est **distincte** du pepper Kiosk existant (`KIOSK_TOKEN_PEPPER`/
`Config.Pepper`, qui sert au hachage HMAC des refresh tokens/codes
d'enrôlement) : un pepper HMAC et une clé de chiffrement symétrique ont des
propriétés différentes (HMAC n'est pas réversible par construction, donc
inutilisable ici) et ne doivent pas être confondus même s'ils sont tous deux
des secrets serveur.

**Pourquoi chiffrement plutôt que hash, ici précisément** : un hash est le
bon choix quand on n'a besoin de vérifier une égalité que côté serveur (PIN
employé, refresh token, code d'enrôlement — c'est encore le cas de
`verify-admin-pin`, qui pourrait rester un hash). Le chiffrement réversible
est **nécessaire** dès qu'il faut réafficher le secret en clair à un humain
après coup (ici : consultation POS) — un hash ne le permettrait jamais, quel
que soit l'algorithme.

- **Génération** : à l'enrôlement (`EnrollDevice`) — 4 chiffres,
  `generateAdminPin()` (`crypto/rand`, alphabet 0-9, même esprit que
  `generateEnrollmentCode`). Chiffré via `helpers.Encrypt` avant stockage.
  Retourné en clair dans `EnrollResponse.AdminPin` — toujours utile pour le
  technicien sur site qui n'a pas forcément le POS sous la main au moment de
  l'installation.
- **Vérification** — `POST /kiosk/auth/verify-admin-pin` (device,
  `KioskAuth`, inchangé dans le principe) : `Service.VerifyAdminPin`
  déchiffre `admin_pin_encrypted` puis compare en **temps constant**
  (`crypto/subtle.ConstantTimeCompare`, plus approprié qu'une comparaison de
  hash `==` puisqu'on compare maintenant le PIN en clair lui-même) au PIN
  fourni. Rate-limiting Redis **par borne** (clé
  `kiosk:admin_pin:lockout:{kiosk_id}`) : 5 tentatives puis 30s de lockout
  fixe (pas de backoff exponentiel — le brief ne demande qu'un lockout fixe).
  Une erreur de déchiffrement (clé absente/invalide, ciphertext corrompu)
  n'incrémente **pas** le compteur de lockout et est propagée comme erreur
  serveur (500) — ce n'est pas une tentative de PIN invalide, c'est une
  panne de configuration qu'il faut voir dans les logs, pas masquer derrière
  un 401 générique.
- **Consultation** (nouveau) — `GET /pos/settings/kiosk/devices/{id}/admin-pin`
  (back-office) : `Service.GetAdminPin` déchiffre et retourne le PIN courant
  sans le modifier. `404 kiosk_admin_pin_not_configured` si
  `admin_pin_encrypted` est NULL (borne enrôlée avant cette fonctionnalité,
  ou jamais régénérée depuis) — message invitant explicitement à régénérer
  plutôt qu'un 404 générique ambigu.
- **Régénération** — `POST /pos/settings/kiosk/devices/{id}/regenerate-admin-pin` :
  génère un nouveau PIN, l'écrase en base (`UpdateKioskAdminPinEncrypted`),
  le retourne en clair une seule fois — utile si la borne est compromise (un
  vrai changement de secret, contrairement à la simple consultation
  ci-dessus). N'invalide pas un éventuel déverrouillage déjà actif côté
  borne (état Flutter local, hors scope API).
- **Permission back-office** : le brief demandait "même permission que la
  gestion des bornes existante" pour la consultation — vérifié qu'**aucune**
  route `/pos/settings/kiosk/*` n'a de `RequirePermission` dédié aujourd'hui
  (seulement `authMiddleware`, voir Incrément 6). Plutôt que de laisser ces
  deux routes sans aucune permission spécifique comme le reste du module —
  ce qui serait cohérent avec l'existant mais insuffisant pour un secret
  réaffichable en clair — `middleware.RequirePermission(middleware.HasSettingsAccess)`
  a été ajouté **uniquement** sur `GET .../admin-pin` et
  `POST .../regenerate-admin-pin` (pas sur le reste du groupe) : ce sont les
  deux seules routes Kiosk qui exposent un secret en clair, elles méritent un
  contrôle que les autres (liste, rename, enable/disable...) n'ont pas besoin
  d'avoir. Écart volontaire par rapport à "même permission que l'existant"
  puisque l'existant n'en a aucune.

### Tests manuels incrément 8

Mêmes limites que les incréments précédents (pas de `MYSQL_URL`/`REDIS_URL`
dans ce sandbox) : seul `go build ./...` a été vérifié (clean). **Avant
exécution réelle :**
1. Appliquer `migrations/todo/042_kiosk_admin_pin.up.sql`.
2. Configurer `KIOSK_PIN_ENCRYPTION_KEY` (`openssl rand -base64 32`) —
   `Encrypt`/`Decrypt` échouent sinon (`encryption key not configured`).
3. S'assurer que l'utilisateur de test a `HasSettingsAccess` (ou `IsAdmin`)
   pour les étapes 5/6 ci-dessous.

```bash
BASE_URL="http://localhost:8080"
USER_TOKEN="<token d'un user back-office authentifié, HasSettingsAccess>"

# 1. Nom vide rejeté à l'enrôlement
CODE=$(curl -s -X POST "$BASE_URL/pos/settings/kiosk/enrollment-codes" -H "Authorization: Bearer $USER_TOKEN" | jq -r .data.code)
curl -s -X POST "$BASE_URL/kiosk/auth/enroll" -H "Content-Type: application/json" \
  -d "{\"enrollment_code\":\"$CODE\",\"name\":\"\",\"hardware_model\":\"Elo\",\"os_version\":\"Android 13\",\"app_version\":\"1.0.0\"}"
# -> 400 kiosk_name_invalid

# 2. Enrôlement valide : admin_pin présent une seule fois
curl -s -X POST "$BASE_URL/kiosk/auth/enroll" -H "Content-Type: application/json" \
  -d "{\"enrollment_code\":\"$CODE\",\"name\":\"Borne test\",\"hardware_model\":\"Elo\",\"os_version\":\"Android 13\",\"app_version\":\"1.0.0\"}" \
  | tee enroll.json
ACCESS_TOKEN=$(jq -r .data.access_token enroll.json)
ADMIN_PIN=$(jq -r .data.admin_pin enroll.json)
KIOSK_ID=$(jq -r .data.kiosk_id enroll.json)

# 3. Vérification du PIN admin (succès)
curl -s -X POST "$BASE_URL/kiosk/auth/verify-admin-pin" -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" -d "{\"pin\":\"$ADMIN_PIN\"}"
# -> { "data": { "valid": true } }

# 4. PIN invalide x5 -> lockout
for i in 1 2 3 4 5; do
  curl -s -X POST "$BASE_URL/kiosk/auth/verify-admin-pin" -H "Authorization: Bearer $ACCESS_TOKEN" \
    -H "Content-Type: application/json" -d '{"pin":"0000"}'
done
# -> 401 kiosk_admin_pin_invalid x5, puis :
curl -s -X POST "$BASE_URL/kiosk/auth/verify-admin-pin" -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" -d '{"pin":"0000"}'
# -> 429 kiosk_admin_pin_locked avec delay_seconds proche de 30

# 5. Consultation back-office (déchiffrement)
curl -s "$BASE_URL/pos/settings/kiosk/devices/$KIOSK_ID/admin-pin" \
  -H "Authorization: Bearer $USER_TOKEN"
# -> { "data": { "admin_pin": "<ADMIN_PIN>" } } — doit être identique au PIN reçu à l'étape 2

# 6. Régénération back-office
curl -s -X POST "$BASE_URL/pos/settings/kiosk/devices/$KIOSK_ID/regenerate-admin-pin" \
  -H "Authorization: Bearer $USER_TOKEN"
# -> { "data": { "admin_pin": "<nouveau PIN>" } }, l'ancien ADMIN_PIN ne fonctionne plus à l'étape 3,
#    et la consultation (étape 5) renvoie désormais ce nouveau PIN

# 7. Utilisateur sans HasSettingsAccess -> 403 sur les deux routes ci-dessus
```

## Incrément 9 — `business_name` dans `GET /kiosk/settings` (bandeau d'accueil Menu côté borne)

Demandé côté `wello-kiosk` pour le bandeau coloré au-dessus de `MenuScreen`
(logo + nom de l'établissement) : le nom à afficher n'était exposé par
aucun endpoint Kiosk. `kiosk_settings` ne contient que des paramètres
d'affichage (logo/couleur/vidéo) — jamais l'identité du merchant, voir
incrément 4 ("Ticket client sans identité merchant"). Le nom existe déjà en
base sous `merchant.fullName`, utilisé par le login humain
(`internal/modules/users/repository.go`, alias `MerchantName`), mais jamais
joint côté Kiosk (auth device, pas auth humaine).

**`Repository.GetKioskSettings`** (`internal/modules/kiosk/repository.go`)
attache désormais `KioskSettingsRow.BusinessName` via une requête séparée
(`getMerchantBusinessName`, `SELECT fullName FROM merchant WHERE id = ?`)
plutôt que d'étendre le `JOIN` dans `GetSettingsByMerchant` : ce dernier
retourne `(nil, nil)` quand le merchant n'a pas encore de ligne
`kiosk_settings` (cas normal pour une borne neuve, voir
`defaultKioskSettingsRow`), un `JOIN` dans cette requête n'aurait donc pas
attaché le nom dans ce cas précis. La requête séparée s'applique après coup,
que la ligne `kiosk_settings` existe ou non.

`KioskSettingsResponse.BusinessName` (`business_name` en JSON, `*string`,
absent → `null`) est exposé en lecture seule : **pas** de champ
correspondant dans `UpdateKioskSettingsRequest` — le nom de l'établissement
se gère via la fiche merchant existante (back-office), pas via les
paramètres Kiosk.
```
