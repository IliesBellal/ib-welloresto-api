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

## Incrément 10 — `GET /kiosk/discounts`

Port de `scannorder.GetDiscounts` (`/scannorder/{merchant_slug}/discounts`)
côté Kiosk. Différence structurelle de départ : ScanNOrder résout le
merchant depuis un QR code (`GetMerchantIDAndTZFromQR`), le Kiosk connaît
déjà son `merchant_id` via `middleware.KioskAuth` — pas de résolution de
slug nécessaire, mais le fuseau horaire du merchant (nécessaire pour calculer
le jour de la semaine courant et filtrer `discounts_schedules`) n'était
disponible nulle part dans le module : ajout de
`Repository.GetMerchantTimezone(ctx, merchantID)` (`SELECT timezone FROM
merchant WHERE id = ?`), dédiée au module Kiosk plutôt que d'importer
`scannorder` juste pour ce champ (cohérent avec le reste du module qui
évite les imports croisés entre modules métier, voir Incrément 2).

### Écart volontaire : pas de filtre `order_type`

`scannorder.GetDiscounts` reçoit `?order_type=` en query et filtre
`discounts.discount_order_type` dessus (valeurs `IN`/`TAKE_AWAY`/`DELIVERY`,
voir `internal/models/orders_model.go`). `GET /kiosk/discounts` n'a **pas**
de query param équivalent : la borne affiche typiquement ses promotions sur
l'écran d'accueil, avant que le client ait choisi un `fulfillment_type`
(`DINE_IN`/`TAKE_AWAY`, vocabulaire différent de `IN`/`TAKE_AWAY` côté
ScanNOrder — une conversion aurait été nécessaire de toute façon).
`Repository.GetDiscounts(ctx, merchantID, orderType, dow)` garde le
paramètre `orderType` (réutilisable plus tard si un filtrage par mode de
retrait est demandé), mais `Service.GetDiscounts` l'appelle avec `""`
(`LIKE '%%'` → aucun filtre de type, seulement validité temporelle + jour de
la semaine). Les autres filtres de `scannorder.GetDiscounts` sont repris à
l'identique : `valid_from`/`valid_to`, `discounts_schedules` (si
`is_time_limited`), `available = true`, `enabled = true`.

### Modèle de réponse : mêmes champs JSON que ScanNOrder

`kiosk.KioskDiscount` reprend exactement les champs de
`scannorder.Discount` (même nommage JSON), pour qu'un client qui consomme
déjà le contrat ScanNOrder n'ait rien à réapprendre. Tableau vide (`[]`),
jamais `null`, si aucune promotion active — même garantie que
`scannorder.GetDiscounts`.

### Exemple réel — `GET /kiosk/discounts`

```json
{
  "id": "kiosk.get_discounts",
  "data": {
    "discounts": [
      {
        "discount_id": "d1",
        "discount_order_type": "IN,TAKE_AWAY",
        "discount_code": null,
        "discount_desc": "10% de réduction sur place et à emporter",
        "discount_name": "Happy Hour",
        "discount_value": 10,
        "discount_unit": "PERCENT",
        "min_order_value": 0,
        "min_order_unit": "EUR",
        "max_discount_value": null,
        "max_discount_unit": null,
        "discounted_quantity": 0,
        "is_cumulative": false,
        "available": true
      }
    ]
  }
}
```

Réponse vide (aucune promotion active) :
```json
{ "id": "kiosk.get_discounts", "data": { "discounts": [] } }
```

### Tests manuels incrément 10

```bash
BASE_URL="http://localhost:8080"
ACCESS_TOKEN="<access_token via /kiosk/auth/enroll ou /kiosk/auth/token/refresh>"

curl -s "$BASE_URL/kiosk/discounts" -H "Authorization: Bearer $ACCESS_TOKEN"
# -> { "data": { "discounts": [...] } }, [] si aucune promotion active pour ce merchant
```

Non exécuté dans ce sandbox (pas de `MYSQL_URL`/`REDIS_URL`) — seul `go
build ./...` a été vérifié (clean).
```

## Incrément 11 — alignement structurel avec ScanNOrder

Suite à `docs/KIOSK_VS_SCANNORDER_STRUCTS.md` (audit comparatif des structs
entre `scannorder` et `kiosk` sur les flux menu/pricing/commande), les écarts
suivants ont été corrigés **côté kiosk uniquement** — `scannorder` n'a pas
été modifié.

### Correction 1 (requalifiée non-bloquante après revue) — `MerchantApproval` forcé à `ACCEPTED`

`Service.CreateKioskOrder` forçait `merchant_approval = "ACCEPTED"` dès la
création, sans paiement réel encaissé. **Mise à jour (Ilies)** : la
qualification initiale "bloquante" était une erreur d'appréciation de
l'audit — ce n'est pas un bug, les commandes Kiosk aboutissent de toute
façon à `ACCEPTED` (immédiatement ou après encaissement comptoir), c'est
l'un des seuls comportements **volontairement différents** de Kiosk par
rapport à ScanNOrder. Voir `docs/KIOSK_VS_SCANNORDER_STRUCTS.md` (note en
tête de document) pour le détail de cette requalification.

Le correctif suivant a néanmoins été appliqué (et reste en place, sans
besoin de revert) : **toute commande kiosk part en `PENDING_APPROVAL`
à la création, qu'elle soit `DINE_IN` ou `TAKE_AWAY`** — contrairement à
`scannorder.CreateOrderSNO` où `IN` part directement en `ACCEPTED` (le client
scanne à table, le staff voit la commande live, pas de paiement à confirmer).
Le kiosk n'a, à cet incrément, que le paiement comptoir (`pay_at_counter`) :
DINE_IN comme TAKE_AWAY doivent attendre que le staff encaisse réellement.

`Service.ConfirmCounterPayment` fait maintenant la transition
`PENDING_APPROVAL → ACCEPTED` via
`OrdersLifeCycleService.SetOrderAccepted(ctx, kioskCreatedBy, merchantID,
orderID)` — le même mécanisme que `AcceptOrder`/POS, appelé directement
(sans passer par `middleware.UserFromContext`, qui n'existe pas pour un
appelant authentifié par device).

### Correction 2 (majeure) — regroupement des options par vrai `configurable_attribute_id`

`Service.buildOrderProducts` regroupait toutes les options sélectionnées
sous un id fictif unique `"kiosk-options"`, perdant l'information de groupe
de modificateur (ticket cuisine, audit). `Repository.
GetConfigurationOptionAttributeIDs` (remplace `GetExistingConfigurationOptionIDs`)
retourne désormais `configurable_attribute_id` par option, et
`buildOrderProducts` reconstruit un `models.ConfigurationAttribute` par
groupe réel — même structure que ce qu'envoie le client ScanNOrder.

### Correction 3 (majeure) — champs manquants sur `KioskProduct`

Ajout de `price_take_away_cents`, `is_popular`, `tva_rate`, `display_order`,
`status` (mappés depuis `models.ProductEntry`). `max_quantity` reste `nil` :
il n'existe pas de limite de quantité par produit en base (seule
`ConfigurableOption.MaxQuantity`, par option de modificateur, existe — déjà
porté par `KioskModifierOption.MaxQuantity`).

### Correction 4 (majeure) — `KioskCategory.available`

Ajout du champ `available`, alimenté depuis `ProductCategory.Available`.
`GetMenu` filtre désormais les catégories désactivées par le restaurateur
(`available = false`) avant de les inclure dans la réponse — auparavant
elles restaient visibles sur la borne.

### Correction 5 (majeure, **breaking change client**) — nommage `KioskModifierGroup`/`KioskModifierOption`

Champs JSON renommés pour s'aligner sur `ConfigurableAttribute`/
`ConfigurableOption` (`internal/models/menu_models.go`, référence ScanNOrder) :

| Avant (kiosk) | Après (kiosk = scannorder) |
|---|---|
| `KioskModifierGroup.name` | `title` |
| `KioskModifierGroup.min` | `min_options` |
| `KioskModifierGroup.max` | `max_options` |
| `KioskModifierGroup.required` (déduit de `min>0`) | supprimé, remplacé par `attribute_type` |
| `KioskModifierOption.name` | `title` |
| `KioskModifierOption.price_delta_cents` | `extra_price` |
| — | `max_quantity` (nouveau) |
| — | `configurable_attribute_id` (nouveau) |
| — | `selected` (nouveau) |

**⚠️ Le client Flutter kiosk doit être mis à jour en conséquence** (modèles
Dart consommant `GET /kiosk/menu` / `GET /kiosk/products/{id}`) — prévu dans
la session Flutter suivante.

### Correction 6 (majeure) — champs manquants sur `KioskPricingResponse`

Ajout de `ht_cents`, `is_orderable`, `not_orderable_reason`,
`applied_discounts`, `unavailable_products` — déjà calculés par
`OrdersService.ComputePricing` mais jusqu'ici non mappés. Point notable :
`OrdersService.ComputePricing` ne renseigne **pas** `PricingResponse.
IsOrderable`/`NotOrderableReason` (champs de réponse) — seulement
`PricingRequest.IsOrderable`/`NotOrderableReason` (champs internes à la
requête, json `"-"`, donc invisibles sur le wire). `scannorder.GetPricingSNO`
les recopie explicitement après l'appel ; `pricingResponseToKiosk` fait
désormais la même chose (`pricing.OrderRequest.IsOrderable` →
`KioskPricingResponse.IsOrderable`).

### Correction 7 — `validateAndCleanPricingPayload` : **non applicable**, vérifié plutôt qu'implémenté

L'audit suggérait de porter `scannorder.validateAndCleanPricingPayload`
(anti-fraude prix) côté kiosk avant `ComputePricing`. Vérification du code
réel de `OrdersService.ComputePricing` (`internal/modules/orders/
service.go`) : `buildSelectedProducts` recalcule **toujours** `Price`/`TvaRate`
depuis `dbProducts` (jamais depuis le payload client), et
`applyConfigurationOptionPrices` réécrit toujours `ExtraPrice` depuis
`GetConfigurationOptionPrices` (DB). `validateAndCleanPricingPayload`
côté scannorder ne fait donc, pour le prix, qu'un travail déjà refait
(de façon redondante) par le moteur partagé — sa seule valeur ajoutée réelle
est de rejeter explicitement un `product_id`/`option_id` inconnu avant
l'appel. Le kiosk fait déjà cette validation d'existence, mais via un
mécanisme différent : `buildOrderProducts` vérifie `GetAvailableKioskProductIDs`
(produits) et `GetConfigurationOptionAttributeIDs` (options, Correction 2)
**avant** d'appeler `ComputePricing`, et retourne `models.ErrInvalidInput`/
`ErrKioskProductUnavailable` si un id n'existe pas. Dupliquer
`validateAndCleanPricingPayload` aurait donc ajouté du code mort. Aucun
changement appliqué pour cette correction.

### M2 — `order_id`/`IsSNO` sur `CreateKioskOrder`

Ajout de `orderReq.IsSNO = false` (explicite, alignement avec
`scannorder.CreateOrderSNO` qui pose `IsSNO = true`). Le mapping
`fulfillment_type → order_type` (M1) et le report des `TTC`/`TVA`/`HT`
calculés par `ComputePricing` avant l'appel à `CreateOrder` (M3) étaient déjà
en place avant cet incrément — vérifiés, non modifiés.

### Tests

`go build ./...` clean. `go test ./internal/modules/kiosk/...` : OK (suite
existante, non étendue dans cet incrément — la Correction 5 étant un
breaking change JSON, des tests dédiés au format de `GET /kiosk/menu`
seraient à ajouter avant la mise à jour du client Flutter).

---

## Incrément 6 — alignement complet des contrats pricing/commande sur scannorder

### Décision [FAIT]

Suppression des quatre types fantômes propres au module Kiosk qui
dupliquaient les structs partagées sans y ajouter de logique métier réelle :
`KioskPricingRequest`, `KioskPricingResponse`, `CreateKioskOrderRequest`,
`CreateKioskOrderResponse` (ainsi que leurs types satellites `KioskPricingItem`
et `KioskOrderItem`, devenus inutiles pour la même raison). Les endpoints
`POST /kiosk/pricing` et `POST /kiosk/orders` consomment désormais
directement `models.PricingRequest` / `models.PricingResponse` /
`models.RequestObject` / `models.CreateOrderResult` (`internal/models/`) —
**exactement le même contrat wire que** `scannorder.GetPricingSNO` /
`scannorder.CreateOrderSNO`.

**Pourquoi** : cohérence multi-canal (un seul format de payload pricing/
commande à maintenir pour POS, ScanNOrder et Kiosk, au lieu de trois) et
simplicité de maintenance (un changement de `models.OrderProductPayload`,
ex. un nouveau champ de configuration produit, se propage désormais
automatiquement au Kiosk sans mapping intermédiaire à mettre à jour).

### Changement de contrat wire côté client Kiosk (breaking change assumé)

Avant cet incrément, le client Kiosk envoyait un format simplifié pour les
items (`product_id`, `quantity`, `selected_option_ids`, `notes`), traduit
côté serveur (`buildOrderProducts`) vers le `models.OrderProductPayload`
complet (`Configuration`/`Attributes`/`Options`) via des lookups DB
(`GetConfigurationOptionAttributeIDs`). Ce mapping intermédiaire est
supprimé : le client Kiosk doit désormais envoyer les items directement au
format `models.OrderProductPayload` (même structure que le client
ScanNOrder), `Configuration`/`Attributes`/`Options` inclus. **Le client
Flutter `wello-kiosk` devra être mis à jour en conséquence** (pas fait dans
cet incrément, hors scope backend).

Seule traduction kiosk-spécifique conservée, faite dans le handler avant
d'appeler le service (pas dans une struct dédiée) : `fulfillment_type`
(`DINE_IN`/`TAKE_AWAY`, vocabulaire écran borne) → `order_type` (`IN`/
`TAKE_AWAY`, convention partagée POS/ScanNOrder/Kiosk), via
`kioskFulfillmentToOrderType` (inchangée, déjà testée par
`menu_pricing_test.go`). `fulfillment_type` lui-même est lu depuis
`models.OrderRequest.FulfillmentType` (champ déjà présent dans la struct
partagée, json `"fulfillment_type"`) — aucune struct nouvelle introduite
pour le porter.

La clé d'idempotence (`idempotency_key`, sans équivalent dans les structs
partagées) n'est plus un champ JSON du body : elle est désormais lue depuis
le header HTTP `Idempotency-Key`, pattern REST standard pour ce type de
donnée orthogonale au payload métier.

### Ce qui reste spécifique au Kiosk côté service (pas de la duplication de logique pricing/commande)

- **`validateKioskProductAvailability`** (remplace `buildOrderProducts`) :
  vérifie `is_available_on_kiosk` pour chaque `product_id` du panier, avant
  d'appeler `ordersService.ComputePricing`. Ce n'est pas du calcul de prix
  (entièrement délégué à `orders`), c'est une règle d'accès au canal —
  un produit peut être vendable en salle/POS mais désactivé sur la borne.
- **`checkFulfillmentEnabled`** : vérifie que le mode (`IN`/`TAKE_AWAY`) est
  activé dans `kiosk_settings` pour ce merchant — gate métier Kiosk, pas du
  pricing.
- **Idempotence** (`Service.CreateOrder`) et **forçage des champs paiement**
  (`OnlinePayment = false`, `Payments = []`, `MerchantApproval = "ACCEPTED"`,
  `CreatedBy`/`CashRegisterId` kiosk) : logique de session/device Kiosk, pas
  de recalcul de prix ou de montant.

### Changement de réponse `POST /kiosk/orders`

Avant : `{order_id, display_number, status: "pending_counter_payment",
total_cents}` (type `CreateKioskOrderResponse`, supprimé). Après :
`models.CreateOrderResult` brut — `{status, order_id, order_num, message,
action, checkout_session}`, identique à la réponse de
`scannorder.CreateOrderSNO`. Le statut `"pending_counter_payment"`
n'apparaît plus dans la réponse de création — il reste accessible via
`GET /kiosk/orders/{order_id}` (`KioskOrderResponse`, non touché par cet
incrément, toujours dérivé de `orders.merchant_approval` via
`mapMerchantApprovalToKioskStatus`).

### Fichiers modifiés

- `internal/modules/kiosk/models.go` : suppression de `KioskPricingItem`,
  `KioskPricingRequest`, `KioskPricingResponse`, `KioskOrderItem`,
  `CreateKioskOrderRequest`, `CreateKioskOrderResponse`.
- `internal/modules/kiosk/service.go` : suppression de `buildOrderProducts`,
  `computeOrderPricing`, `pricingResponseToKiosk` ; `ComputePricing` et
  `CreateKioskOrder` (renommée `CreateOrder`) réécrites pour prendre/
  retourner les structs partagées ; `checkFulfillmentEnabled` adaptée pour
  recevoir `order_type` (`IN`/`TAKE_AWAY`) au lieu de `fulfillment_type`
  brut ; ajout de `validateKioskProductAvailability`.
- `internal/modules/kiosk/handler.go` : `GetKioskPricing`/`CreateKioskOrder`
  décodent désormais `models.PricingRequest`/`models.RequestObject`
  directement, font le mapping `fulfillment_type` → `order_type`, lisent
  l'idempotence depuis le header `Idempotency-Key`.

### Tests

`go build ./...` clean (aucune référence restante aux types supprimés en
dehors de la documentation). `go vet ./...` : aucun nouvel avertissement
introduit par ce changement (les avertissements existants — `ubereats`,
`auth`, `tasks`, `webhook/ubereats/client` — sont préexistants, sans rapport
avec ce module). `go test ./internal/modules/kiosk/...` : OK.

---

## Incrément 7 — bug `POST /kiosk/pricing` retourne toujours `is_orderable: false`

### Cause exacte

`OrdersService.ComputePricing` (`internal/modules/orders/service.go`) calcule
correctement `req.IsOrderable` / `req.NotOrderableReason` sur le
`*models.PricingRequest` (`applyDeliveryRules`, ligne ~1167) — mais ces
champs sont marqués `json:"-"` sur `PricingRequest` (internes, jamais sur le
wire). Le retour final de `ComputePricing` ne les recopiait **jamais** sur
les champs équivalents de `PricingResponse` (`IsOrderable`,
`NotOrderableReason`, `MinimumCartForDeliveryOrder`, eux bien sérialisés en
JSON) : la réponse partait donc toujours avec la valeur zéro de Go,
`is_orderable: false`, quel que soit le résultat réel du calcul.

`scannorder.GetPricingSNO` (ligne 449) fait ce recopiage explicitement après
avoir appelé `ComputePricing` :
```go
pricing.IsOrderable = pricing.OrderRequest.IsOrderable
```
C'est pour ça que ScanNOrder a toujours renvoyé la bonne valeur, alors que
tout autre appelant de `ComputePricing` sans ce recopiage était cassé par
construction.

Le kiosk avait initialement le même recopiage, dans `pricingResponseToKiosk`
(voir Correction 6, Incrément 5) : *"`OrdersService.ComputePricing` ne
renseigne pas `PricingResponse.IsOrderable` (...) `pricingResponseToKiosk`
fait désormais la même chose"*. Ce recopiage a été perdu **sans intention**
lors de l'Incrément 6, quand `pricingResponseToKiosk` (et tout le mapping
vers les structs kiosk dédiées) a été supprimé pour faire consommer
`models.PricingResponse` directement par le handler kiosk — le bug latent de
`ComputePricing` est alors redevenu visible côté Kiosk, **sans aucune
modification volontaire de la logique d'éligibilité**.

`POST /pos/pricing` (`OrdersHandler.GetPricing` → `OrdersService.GetPricing`
→ `ComputePricing`, sans recopiage non plus) a exactement le même bug — pas
spécifique au Kiosk, juste jamais remarqué côté POS.

### Pourquoi `estimated_distribution_time: 1080` n'est pas la cause

Ce champ (`GetEstimatedDistributionTime`) est calculé indépendamment de
`is_orderable` et ne le bloque jamais dans `ComputePricing` — aucun lien
entre les deux. La valeur élevée n'est qu'une coïncidence de données de
test, pas un seuil de blocage.

### Correction appliquée (Option A — fix dans `ComputePricing`, root cause)

`internal/modules/orders/service.go`, retour final de `ComputePricing` :
recopie désormais `req.IsOrderable` → `PricingResponse.IsOrderable`,
`req.NotOrderableReason` → `PricingResponse.NotOrderableReason`,
`req.MinimumCartForDeliveryOrder` → `PricingResponse.MinimumCartForDeliveryOrder`.

Corrigé à la source plutôt que côté kiosk uniquement, parce que le bug
n'est pas propre au Kiosk : le contrat de `ComputePricing` (une fonction
partagée par POS, ScanNOrder et Kiosk) doit produire une réponse correcte
par lui-même, sans dépendre de chaque appelant pour "réparer" le résultat
après coup. Aucune modification de `scannorder.GetPricingSNO` :
son recopiage (ligne 449) devient redondant (même valeur déjà posée par
`ComputePricing`) mais reste inoffensif, y compris pour son cas
spécifique `IsInDeliveryZone` (lignes 450-454, toujours appliqué après,
inchangé). `OrdersService.GetPricing` (route POS `/pricing`) profite du même
correctif sans changement de code supplémentaire.

La logique d'éligibilité elle-même (`applyDeliveryRules`) n'a pas été
touchée : `IsOrderable` ne devient `false` que pour `OrderType == "DELIVERY"
&& IsSNO && TTC < MinimumCartForDeliveryOrder` — ce check ne s'applique déjà
pas à `TAKE_AWAY`/`DINE_IN`, donc pas au cas Kiosk pizza Margarita (qui est
`order_type` Kiosk, jamais `"DELIVERY"`).

### Vérification

- `go build ./...` et `go vet ./...` clean.
- `go test ./internal/modules/orders/... ./internal/modules/kiosk/...
  ./internal/modules/scannorder/...` : OK (aucun test cassé).
- Pour le cas concret du brief (pizza Margarita Jambon, quantity 3, remise
  appliquée, `order_type` Kiosk DINE_IN/TAKE_AWAY) : `applyDeliveryRules`
  pose `req.IsOrderable = true` (pas de branche `DELIVERY`, donc jamais mis
  à `false`) ; ce flag est désormais recopié sur la réponse →
  `is_orderable: true`, `not_orderable_reason` absent. À reconfirmer en
  conditions réelles après déploiement (pas de `MYSQL_URL` dans ce sandbox,
  mêmes limites que les incréments précédents).
- Cas bloquants toujours corrects : panier vide → `unavailable_products`
  non vide, réponse retournée avant `applyDeliveryRules` (la valeur zéro
  Go de `PricingResponse.IsOrderable`, `false`, reste correcte ici, jamais
  écrasée) ; minimum panier livraison non atteint → toujours `IsOrderable =
  false` + `NotOrderableReason = "minimum_cart_for_delivery_not_reached"`,
  désormais effectivement visible sur le wire (ce qui ne l'était pas avant
  ce correctif, autre bug latent corrigé au passage).

---

## Incrément 8 — `/ws-kiosk` : relais `kiosk_unavailable` entrant + fermeture ciblée à la révocation

### Constat de départ

`/ws-kiosk` (Incrément 7) couvrait déjà l'enregistrement de la borne dans le
Hub sous son `merchant_id`, la réception des broadcasts merchant (ping/pong,
désenregistrement propre à la déconnexion). Deux écarts subsistaient par
rapport au besoin (auth device sur un canal WS persistant, pas seulement
broadcast sortant) :
1. `kiosk_unavailable` n'existait que côté REST (`POST
   /kiosk/status/unavailable`) — aucun message entrant lu sur `/ws-kiosk`
   n'était traité, `readPump` ne reconnaissait que `{"type":"PING"}`.
2. La révocation d'une borne (`RevokeKiosk`) ne fermait pas sa connexion
   `/ws-kiosk` active — le `Hub` n'avait aucune notion de `kioskID` (seulement
   `merchantID -> connID -> *Client`), donc aucun moyen de cibler une
   connexion précise.

**Décision : étendre `/ws-kiosk` existant plutôt que créer un second
endpoint `/ws/device` dupliquant `Client`/`Register`/`Unregister`/`serveWS`**
— cohérent avec le rejet explicite de cette duplication en Incrément 7.

### Hub : `kioskID` sur `Client`, fermeture ciblée, broadcast avec exclusion

`internal/infrastructure/websocket/hub.go` :
- `Client.kioskID string` — vide pour une connexion humaine (POS/back-office
  sur `/ws`), renseigné pour une connexion device (`/ws-kiosk`).
- `Hub.CloseKioskConnections(merchantID, kioskID string, code int, reason
  string) bool` — itère les clients du merchant, ferme (via
  `conn.WriteControl(CloseMessage, ...)` puis `conn.Close()`) celles dont le
  `kioskID` correspond. `conn.Close()` déclenche `ReadMessage` en erreur côté
  `readPump`, qui fait son nettoyage habituel (`hub.Unregister` +
  `conn.Close()` à nouveau, sans effet car déjà fermée).
- `Hub.BroadcastToMerchantExcept(merchantID, excludeConnID string, message
  []byte) bool` — même logique que `BroadcastToMerchant`, sauf qu'elle
  saute la connexion émettrice (utilisé pour ne pas renvoyer à la borne le
  message qu'elle vient d'envoyer).

### Relais de `kiosk_unavailable` (`internal/infrastructure/websocket/handler.go`)

`readPump` reconnaît désormais, en plus de `{"type":"PING"}`, tout message
JSON dont `type == "kiosk_unavailable"` — **uniquement si la connexion est
une connexion device** (`c.kioskID != ""` : une connexion `/ws` humaine ne
peut pas émettre ce type de message, il est silencieusement ignoré). Le
`kiosk_id` du message est **toujours réécrit** avec `c.kioskID` (valeur
authentifiée par `KioskAuth` à la connexion) avant relais via
`BroadcastToMerchantExcept` — empêche une borne compromise d'usurper l'id
d'une autre borne du même merchant.

Format attendu de la borne vers le hub (inchangé par rapport à la doc
Incrément 6, désormais réellement consommé) :
```json
{ "type": "kiosk_unavailable", "reason": "connection_lost" }
```
(`kiosk_id` peut être omis ou contenir n'importe quelle valeur — il est
ignoré et remplacé côté serveur.)

### Endpoint REST `POST /kiosk/status/unavailable` : conservé, pas déprécié

Les deux canaux coexistent désormais pour `kiosk_unavailable` (REST et WS) —
décision : **garder le REST en fallback**, ne pas le déprécier. Une borne
peut signaler un problème de connectivité WS via REST précisément dans le
cas où sa connexion `/ws-kiosk` est elle-même la chose qui a un problème
(WS coupé mais réseau HTTP encore fonctionnel) ; le déprécier aurait
supprimé le seul canal fiable dans ce scénario précis.

### Révocation (`Service.RevokeKiosk`, `internal/modules/kiosk/service.go`)

Après la transaction DB (révocation des `kiosk_device_tokens` +
`kiosks.status = 'revoked'`), appel best-effort à
`notificationSvc.CloseKioskConnection(merchantID, kiosk.ID)` (nouvelle
méthode sur `NotificationService`, même pattern que `BroadcastToMerchant` —
pas de dépendance directe de `kiosk` vers `internal/infrastructure/websocket`,
cohérent avec l'injection déjà en place). Code de fermeture WebSocket **1008
(Policy Violation)**, raison `"kiosk_revoked"`. Si la borne n'a pas de
connexion `/ws-kiosk` active, `CloseKioskConnection` retourne simplement
`false`, sans erreur.

Le heartbeat (`POST /kiosk/auth/heartbeat`) reste également bloqué
immédiatement après révocation (`status == "revoked"`, déjà en place) — la
fermeture WS est un mécanisme de notification immédiate complémentaire, pas
un remplacement de cette vérification serveur.

### Tests manuels incrément 8

`go build ./...`, `go vet ./...` (packages touchés) et `go test
./internal/modules/kiosk/...` clean. Pas de `MYSQL_URL`/`REDIS_URL` dans ce
sandbox — non exécuté en conditions réelles.

```bash
BASE_URL="http://localhost:8080"
USER_TOKEN="<token back-office>"
ACCESS_TOKEN="<access_token via /kiosk/auth/enroll>"
KIOSK_ID="kiosk-..."

# 1. Connexion device WS
wscat -c "$BASE_URL/ws-kiosk/" -H "Authorization: Bearer $ACCESS_TOKEN"

# 2. Depuis cette même connexion, envoyer :
{"type":"kiosk_unavailable","reason":"connection_lost"}
# -> un client POS connecté sur /ws du même merchant doit recevoir :
# {"type":"kiosk_unavailable","reason":"connection_lost","kiosk_id":"kiosk-..."}
# (kiosk_id = identité authentifiée, quelle que soit la valeur envoyée par la borne)

# 3. Révocation pendant que la connexion /ws-kiosk est ouverte
curl -s -X POST "$BASE_URL/pos/settings/kiosk/devices/$KIOSK_ID/revoke" \
  -H "Authorization: Bearer $USER_TOKEN"
# -> la connexion wscat de l'étape 1 doit se fermer immédiatement avec le
#    code 1008 ("kiosk_revoked"), sans attendre l'expiration de l'access token
```

---

## Incrément — images sur les options de configuration produit

Basé sur `docs/CONFIG_OPTIONS_IMAGES_AUDIT.md` (audit préalable, lecture
seule). Décisions actées : taille max **2 Mo**, formats JPEG/PNG/WebP
(`r2.ValidateImageType`, inchangé), pas de fallback serveur si
`image_url` est `NULL`, portée d'affichage **Kiosk uniquement** (mais la
struct partagée `models.ConfigurableOption` porte quand même le champ,
pour ne pas créer de dette si ScanNOrder/POS l'exploitent plus tard).

### Migration

`migrations/todo/046_configurable_attribute_options_image_url.{up,down}.sql` —
`ALTER TABLE configurable_attribute_options ADD COLUMN image_url
VARCHAR(500) NULL DEFAULT NULL AFTER extra_price`. Numéro suivant le
dernier réellement utilisé (045, déjà présent non commité dans
`migrations/todo/` au moment de cet incrément). Comme `upsell_suggestions`
documenté ailleurs, c'est la première migration à référencer cette table
legacy (jamais créée par migration).

### Écart corrigé par rapport au plan initial : la vraie source de
`models.ConfigurableOption` côté Kiosk

Le plan initial listait uniquement les 4 requêtes CRUD back-office
(`GetAttributes`, `GetAttribute`, `CreateAttribute`, `UpdateAttribute` —
struct `menu.AttributeOption`, page `Attributes.tsx`). **Ce n'est pas ce
qui alimente le Kiosk.** `models.ConfigurableOption` (la struct partagée,
celle qui porte le nouveau champ `ImageURL`) est en réalité construite par
trois requêtes séparées dans `internal/modules/menu/repository.go` :
`GetMenu`, `GetAllProducts`, `GetProduct` — c'est leur résultat
(`ProductEntry.Configuration`) qui devient `KioskModifierOption` côté
`kiosk.Service.toKioskProduct` (`internal/modules/kiosk/service.go`).
Sans étendre ces trois requêtes, `image_url` serait toujours vide dans
`GET /kiosk/menu`/`GET /kiosk/products/{id}` malgré une implémentation
"complète" sur le papier. **Étendues dans cet incrément**, en plus des 4
requêtes CRUD listées initialement.

`internal/modules/kiosk/repository.go:615`
(`GetConfigurationOptionAttributeIDs`) n'a, à l'inverse, **pas** été
étendue malgré la demande initiale : cette requête ne retourne qu'une
`map[string]string` (id → attribute_id) pour la validation de panier
côté commande, jamais consommée pour l'affichage — y ajouter `image_url`
aurait été du code mort sans aucun consommateur.

### Risque de perte silencieuse corrigé : `UpdateAttribute` (PATCH back-office)

`UpdateAttribute` désactive puis recrée/met à jour toutes les options à
chaque sauvegarde de l'attribut (comportement existant, documenté dans
l'audit). Le formulaire back-office actuel (`Attributes.tsx`) ne connaît
pas encore `image_url` et ne l'envoie donc jamais dans son payload — un
`UPDATE ... SET image_url = ?` inconditionnel aurait effacé à chaque
sauvegarde l'image uploadée séparément via le nouvel endpoint dédié.
Corrigé avec `image_url = COALESCE(?, image_url)` : seule une valeur
explicitement fournie écrase l'existant, sinon l'image déjà uploadée est
préservée.

### Endpoint d'upload

`PUT /menu/attribute_options/{option_id}/image`, calqué à l'identique sur
`UploadProductImage` (form field `photo`, JOIN sur
`configurable_attributes` pour le scoping merchant — les options n'ont pas
de `merchant_id` direct). Plafond dédié `maxAttributeOptionImageBytes = 2
<< 20` (2 Mo, distinct des 5 Mo produit). Clé R2 :
`r2.GenerateConfigOptionKey(merchantID, optionID, ext)` →
`wello_resto_images_storage/merchants/{merchant_id}/config_options/{option_id}{ext}`.
Pas de middleware de permission dédié sur le groupe `/menu` (seul
`authMiddleware`) — cohérent avec toutes les autres routes du groupe.

### Tests manuels

Non exécutés dans cet incrément (pas de `MYSQL_URL`/`REDIS_URL` dans ce
sandbox) — seuls `go build ./...` et `go vet ./...` ont été vérifiés
(propres sur les fichiers touchés ; mêmes avertissements préexistants et
sans rapport ailleurs). Avant exécution réelle : appliquer
`migrations/todo/046_configurable_attribute_options_image_url.up.sql`.

```bash
BASE_URL="http://localhost:8080"
USER_TOKEN="<token back-office>"
OPTION_ID="<id d'une option existante via GET /menu/attributes>"

curl -s -X PUT "$BASE_URL/menu/attribute_options/$OPTION_ID/image" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -F "photo=@./option.png;type=image/png"
# -> { "data": { "image_url": "https://.../wello_resto_images_storage/merchants/.../config_options/....png" } }

# Vérifier la propagation : GET /menu/attributes (image_url sur l'option),
# GET /kiosk/menu et GET /kiosk/products/{id} (image_url sur le modifier
# correspondant, via un access_token kiosk valide)
```

---

## Incrément — paiement carte borne (Stripe Terminal)

Implémentation des endpoints Stripe Terminal côté API pour le canal Kiosk :
lecteur de carte physique intégré à la borne (décision actée G.3 — Stripe
Terminal retenu). Le document `docs/STRIPE_TERMINAL_AUDIT.md` mentionné dans le
brief **n'existe pas dans le repo** — la règle de découplage appliquée est
celle restituée dans le brief lui-même : la logique Terminal vit dans un
service Go interne paramétré par `merchantID`, jamais couplé à `KioskAuth` ;
les handlers `/kiosk/terminal/*` restent des adaptateurs minces qui extraient
`merchantID` du contexte `KioskAuth` puis appellent ce service (objectif :
pouvoir ajouter `/pos/terminal/*` plus tard sans dupliquer la logique).

### 1. Nouveau statut de commande : `pending_card_payment`

**Décision : nouvelle valeur `merchant_approval = "PENDING_CARD_PAYMENT"`**
(`models.MerchantApprovalPendingCardPayment`, `internal/models/orders_model.go`),
et non une réutilisation de `PENDING_APPROVAL`. Justification :

- `pending_counter_payment` est le nom Kiosk de `PENDING_APPROVAL` (incrément 2).
  Pour que `pending_card_payment` soit **réellement distinct** (le brief l'exige :
  `POST /kiosk/terminal/payment-intent` et `switch-to-counter-payment` doivent
  vérifier « la commande est en `pending_card_payment` »), il faut une valeur
  stockée distincte — sinon les deux états seraient indiscernables en base.
- Une commande carte **ne doit pas partir en cuisine** avant confirmation du
  paiement. `PENDING_CARD_PAYMENT` ≠ `ACCEPTED` la tient hors du flux KDS (qui
  ne traite que les commandes `ACCEPTED`) jusqu'au webhook Stripe.

**Blast radius vérifié** (le brief demandait que ce statut ne casse aucun
filtre/affichage) :
- `internal/modules/orders/orders_fetcher_builder.go` scanne `merchant_approval`
  en `string` sans `switch`/enum — une nouvelle valeur passe sans erreur.
- `internal/tasks/orders.go` (`DenyOrders`, cron par ailleurs désactivé) filtre
  `merchant_approval = 'PENDING_APPROVAL'` : une commande carte n'y est **pas**
  capturée — voulu (elle a son propre cycle : retry, cancel PI, ou bascule
  caisse).
- Aucun autre `WHERE merchant_approval = ...` ne cible cette valeur.
- `mapMerchantApprovalToKioskStatus` mappe `PENDING_CARD_PAYMENT` →
  `"pending_card_payment"` (visible sur `GET /kiosk/orders/{id}`).

**Détermination du statut initial** — `Service.CreateOrder` prend désormais un
paramètre `paymentMethod`, lu depuis le champ **racine** `payment_method` du body
de création (via un wrapper de handler qui étend `models.RequestObject`, sans
polluer la struct partagée) :
- `"card"` → gate `card_payment_enabled`, `MerchantApproval = PENDING_CARD_PAYMENT`.
- `"pay_at_counter"` **ou vide** (rétrocompatible avec le client existant) →
  comportement inchangé : gate `pay_at_counter_enabled`, `MerchantApproval = "ACCEPTED"`.
- toute autre valeur → `400 kiosk_payment_method_invalid`.

Dans les deux cas : `OnlinePayment=false`, `Payments=[]` — le Terminal est
encaissé hors bande (pas de Checkout web).

### 2. Service Stripe Terminal — `internal/infrastructure/stripe/terminal.go`

`TerminalService` (package `stripeclient`), construit dans `routes.go` à partir
du `StripeManager` existant, d'un `TerminalAccountStore` (SQL) et de Redis :

- **`CreateConnectionToken(ctx, merchantID)`** → `TerminalConnectionTokens.New`
  scopé au compte connecté (`SetStripeAccount`). Retourne le `secret`.
- **`CreateTerminalPaymentIntent(ctx, merchantID, orderID, amountCents)`** →
  PaymentIntent `card_present`, `CaptureMethod=automatic`, `Currency=eur`, sur le
  **compte connecté** (`SetStripeAccount`) avec `ApplicationFeeAmount` — **même
  modèle de charge directe + commission que `CreateCheckoutSession`** (checkout.go :
  `floor(ttc*variable_fees + fixed_fees + 0.5)`), plutôt que le modèle destination
  charge (`OnBehalfOf`/`TransferData`) : cohérence avec l'existant. Stocke deux
  mappings Redis (TTL 1h) : direct `terminal_pi:{piID}` →
  `{order_id, merchant_id}` (lu par le webhook) et inverse
  `terminal_order_pi:{merchant}:{order}` → `piID` (pour retrouver le PI actif au
  basculement caisse). Le brief ne demandait que `order_id` dans le mapping ;
  `merchant_id` est ajouté car le webhook en a besoin pour `SetOrderAccepted` +
  notification, et le mapping inverse pour le basculement caisse.
- **`CancelTerminalPaymentIntent(ctx, merchantID, paymentIntentID)`** → annule le
  PI sur le compte connecté et supprime les mappings. `merchantID` ajouté à la
  signature du brief : nécessaire pour résoudre le compte connecté (l'annulation
  exige `SetStripeAccount`) et pour refuser qu'une borne annule le PI d'un autre
  merchant.
- **`CancelActivePaymentIntentForOrder(ctx, merchantID, orderID)`** (helper,
  hors liste du brief) → retrouve le PI actif via le mapping inverse et l'annule ;
  no-op si aucun. Utilisé par le basculement caisse.

Clés Redis + struct `TerminalPaymentMapping` **exportées** et réutilisées par le
webhook (jamais dupliquées).

### 3. Handlers et routes Kiosk (groupe `/kiosk`, `KioskAuth`)

- `POST /kiosk/terminal/connection-token` → `{ "secret": "..." }` (gate
  `card_payment_enabled`).
- `POST /kiosk/terminal/payment-intent` — body `{order_id, amount_cents}` :
  vérifie que la commande appartient au merchant **et** est en
  `PENDING_CARD_PAYMENT` ; le montant est re-lu depuis `orders.TTC` (jamais
  depuis le client), `amount_cents` n'est accepté que s'il **correspond**
  (`400 kiosk_amount_mismatch` sinon). Retourne `{client_secret, payment_intent_id}`.
- `POST /kiosk/terminal/payment-intent/{payment_intent_id}/cancel` → annule le PI
  (abandon/timeout). La commande **reste** en `PENDING_CARD_PAYMENT` (retry ou
  bascule caisse possibles).
- `POST /kiosk/orders/{order_id}/switch-to-counter-payment` → vérifie
  `PENDING_CARD_PAYMENT`, annule le PI actif (best-effort), repasse la commande en
  `PENDING_APPROVAL` (+ invalidation du cache Redis order), puis réutilise
  `ConfirmCounterPayment` tel quel (transition `ACCEPTED` + code de retrait + QR +
  notification). Réponse = `CounterPaymentResponse`.

Le service kiosk dépend d'une interface locale `TerminalGateway` (pas du type
concret) — découplage/testabilité.

### 4. Webhook — events Terminal (`internal/webhook/stripe/service.go`)

Le switch `event.Type` est étendu sans casser le flux Checkout en ligne :

- **`payment_intent.succeeded`** → `HandlePaymentIntentSucceeded`. Discriminant
  card_present : **présence du mapping Redis `terminal_pi:{id}`** (plus fiable que
  parser `payment_method_details.type`, non expansé sur l'objet PaymentIntent
  reçu — il faudrait un appel API en plus — et le mapping donne directement la
  commande). Si mapping trouvé → `SetOrderAccepted(ctx, "KIOSK", merchantID,
  orderID)` (`PENDING_CARD_PAYMENT` → `ACCEPTED`, **même mécanisme que
  ConfirmCounterPayment** ; déclenche KDS/impression + broadcast `order_updated`
  en interne) puis suppression des mappings. Sinon → comportement existant
  inchangé (`UpdatePaymentIntentStatus(..., "CAPTURED")`). La suppression du
  mapping rend la confirmation idempotente (redélivrance Stripe → mapping absent →
  no-op).
- **`payment_intent.payment_failed`** (nouveau case) → si mapping trouvé, la
  commande **reste** en `PENDING_CARD_PAYMENT` (aucune annulation serveur, le
  client réessaie ou bascule caisse), on diffuse seulement `order_updated`. Sinon
  ignoré (aucun case n'existait avant).

**Hors périmètre (assumé)** : aucun enregistrement `payments` n'est inséré pour la
charge Terminal (le brief liste explicitement les actions webhook et n'inclut pas
l'insertion d'un paiement) — à ajouter si le reporting `payments.mop` doit couvrir
les encaissements carte borne.

### Variables d'environnement

Aucune nouvelle : le Terminal réutilise `STRIPE_API_KEY` (déjà dans
`StripeManager`), la base et Redis existants. Prérequis : le merchant doit avoir
un compte Stripe connecté (`stripe_accounts`) et `kiosk_settings.card_payment_enabled = TRUE`.

### Tests manuels

Non exécutés dans ce sandbox (pas de `MYSQL_URL`/`REDIS_URL`/clé Stripe test) —
seuls `go build ./...`, `go vet` (paquets touchés) et
`go test ./internal/modules/kiosk/...` ont été vérifiés (clean ; les 2 warnings
`go vet` restants — `cmd/api` copie de lock `NewAuthHandler`, `tasks.go`
unreachable — sont préexistants et sans rapport). Le webhook doit être configuré
pour envoyer `payment_intent.succeeded` **et** `payment_intent.payment_failed`.

```bash
BASE_URL="http://localhost:8080"
ACCESS_TOKEN="<access_token via /kiosk/auth/enroll ou /kiosk/auth/token/refresh>"
AUTH="Authorization: Bearer $ACCESS_TOKEN"
PRODUCT_ID="<product_id is_available_on_kiosk=TRUE>"
# Prérequis : kiosk_settings.card_payment_enabled = TRUE, stripe_accounts renseigné.

# 0. Connection token (appairage du lecteur)
curl -s -X POST "$BASE_URL/kiosk/terminal/connection-token" -H "$AUTH"
# -> { "data": { "secret": "pst_test_..." } }

# 1. Créer une commande carte -> statut pending_card_payment
curl -s -X POST "$BASE_URL/kiosk/orders" -H "$AUTH" -H "Content-Type: application/json" \
  -H "Idempotency-Key: card-$(date +%s)" \
  -d "{\"payment_method\":\"card\",\"order\":{\"fulfillment_type\":\"DINE_IN\",\"products\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":1}]}}" \
  | tee card_order.json
ORDER_ID=$(jq -r .data.order_id card_order.json)

curl -s "$BASE_URL/kiosk/orders/$ORDER_ID" -H "$AUTH"
# -> { "data": { "status": "pending_card_payment", ... } }

# 2. PaymentIntent Terminal (amount_cents DOIT = orders.TTC)
TTC=$(curl -s "$BASE_URL/kiosk/orders/$ORDER_ID" -H "$AUTH" | jq -r .data.total_cents)
curl -s -X POST "$BASE_URL/kiosk/terminal/payment-intent" -H "$AUTH" -H "Content-Type: application/json" \
  -d "{\"order_id\":\"$ORDER_ID\",\"amount_cents\":$TTC}" | tee pi.json
# -> { "data": { "client_secret": "pi_..._secret_...", "payment_intent_id": "pi_..." } }
PI_ID=$(jq -r .data.payment_intent_id pi.json)

# 2bis. Montant incohérent -> 400 kiosk_amount_mismatch
curl -s -X POST "$BASE_URL/kiosk/terminal/payment-intent" -H "$AUTH" -H "Content-Type: application/json" \
  -d "{\"order_id\":\"$ORDER_ID\",\"amount_cents\":1}"
# -> 400 kiosk_amount_mismatch

# --- CAS SUCCÈS ---
# Après paiement réussi sur le lecteur, Stripe envoie payment_intent.succeeded
# (card_present). Le webhook confirme la commande :
curl -s "$BASE_URL/kiosk/orders/$ORDER_ID" -H "$AUTH"
# -> { "data": { "status": "accepted", ... } }  (partie en cuisine)

# --- CAS ÉCHEC ---
# Stripe envoie payment_intent.payment_failed -> la commande reste
# pending_card_payment, un order_updated est diffusé. Le client peut relancer
# un PaymentIntent (retry) ou basculer vers la caisse (ci-dessous).

# 3. Annulation d'un PaymentIntent (abandon/timeout) — commande inchangée
curl -s -X POST "$BASE_URL/kiosk/terminal/payment-intent/$PI_ID/cancel" -H "$AUTH"
# -> { "data": { "status": "cancelled" } } ; GET /kiosk/orders/$ORDER_ID reste pending_card_payment

# --- CAS BASCULE CAISSE ---
# 4. Basculer carte -> caisse (après échec/abandon) sans recréer de commande
curl -s -X POST "$BASE_URL/kiosk/orders/$ORDER_ID/switch-to-counter-payment" -H "$AUTH"
# -> { "data": { "order_id": "...", "pickup_code": "...", "qr_payload": "...", "status": "accepted" } }
#    (PI actif annulé, commande passée en caisse puis encaissée via le flux ConfirmCounterPayment)

# 4bis. Rejouer sur une commande qui n'est plus pending_card_payment -> 409
curl -s -X POST "$BASE_URL/kiosk/orders/$ORDER_ID/switch-to-counter-payment" -H "$AUTH"
# -> 409 kiosk_order_not_card_pending
```

---

## Incrément Terminal 2 — enregistrement `payments`, `net_amount`, `terminal_location_id`

Complète l'increment Terminal précédent (qui acceptait la commande via
`SetOrderAccepted` mais **n'insérait aucune ligne `payments`** — trou signalé
« Hors périmètre (assumé) » ci-dessus, désormais comblé).

### Audit 0.A — capture des frais Stripe existante [CONSTAT]

- **Event déclencheur** : `charge.captured`. Le switch `ProcessEvent`
  (`internal/webhook/stripe/service.go`) route ce type vers
  `HandleRetrieveFees` (commentaire du code : « En PHP c'était retrieveFees »).
- **Lecture des frais** : `HandleRetrieveFees` récupère le `balance_transaction`
  **sur le compte connecté** (`params.SetStripeAccount(event.Account)` puis
  `balancetransaction.Get`), somme `FeeDetails` par type (`application_fee` →
  `wrFees`, `stripe_fee` → `stripeFees`), et lit le total `bt.Fee`.
- **Écriture du champ `fee`** : `repo.UpdateFees(ctx, piID, wrFees, stripeFees,
  bt.Fee)` fait **deux UPDATE** : `stripe_payments` (`wello_resto_total_fees`,
  `stripe_total_fees`) puis `payments.fee = bt.Fee` via la jointure
  `payments p INNER JOIN stripe_payments sp ON sp.payment_id = p.payment_id
  WHERE sp.payment_intent_id = ?`. C'est **le seul endroit** où `payments.fee`
  est renseigné.
- **Synchrone ou asynchrone** : **asynchrone** — `payments.fee` est mis à jour à
  la réception ultérieure du webhook `charge.captured`, pas à la création du
  paiement (où `fee` vaut sa valeur par défaut). La jointure passe par
  `stripe_payments.payment_intent_id` : tout paiement dont
  `AddPaymentAndReturnID` a inséré la ligne `stripe_payments` (cas `MOP=STRIPE`)
  est éligible à cette mise à jour — y compris désormais les encaissements
  Terminal.

**Conséquence pour `net_amount`** : le point 4 du brief (« mettre à jour
`net_amount` quand les frais réels arrivent ») est branché exactement ici —
`UpdateFees` écrit désormais `payments.fee = bt.Fee` **et**
`payments.net_amount = payments.amount - bt.Fee` dans le même UPDATE. Le
mécanisme existe donc bien pour les paiements en ligne (pas de limitation à
documenter) et couvre Terminal par la même jointure.

**Réserve** : la mise à jour de `net_amount` dépend entièrement de l'émission de
`charge.captured` par Stripe. Pour un `card_present` en capture automatique, si
Stripe n'émettait pas cet event, `net_amount` resterait à sa valeur provisoire
(`= amount`, `fee = 0`) — même comportement que le Checkout en ligne, qui repose
sur la même hypothèse. Aucune régression : c'est le mécanisme existant, réutilisé
tel quel.

### Audit 0.B — création de paiements (point d'insertion unique) [CONSTAT]

- **Fonction unique** : `OrdersLifeCycleRepository.AddPaymentAndReturnID`
  (`internal/modules/order_life_cycle/repository.go`). C'est le seul INSERT INTO
  `payments` réellement utilisé : il porte le chaînage fiscal NF525
  (`previous_hash`/`hash`/`signature`), le garde de montant
  (`OrderNotFullyPaidError`), les effets de bord (`restaurant_ticket` pour
  `MOP=TR`, `stripe_payments` pour `MOP=STRIPE`) et le recalcul de `orders.isPaid`.
- **Wrappers** : `AddPayment` (ignore l'ID) ; côté service
  `CreatePayment` / `CreatePaymentNoNotification` /
  `CreatePaymentAndReturnID` (enveloppent dans `ExecuteOrderMutation` : tx +
  audit). Le Checkout en ligne utilise `CreatePaymentNoNotification`.
- **Second INSERT ignoré** : `internal/webhook/stripe/repository.go` porte un
  `InsertPayment` (INSERT simplifié, sans hash), mais il est **explicitement
  marqué `// Decom`** (décommissionné) et n'est appelé nulle part dans le flux
  actif — le Checkout est passé à `orderlifecycle.CreatePaymentNoNotification`.
  Il n'a **pas** été étendu : le paiement Terminal passe par la même fonction
  canonique que le reste du projet.
- **Paramètres acceptés** : `models.Payment{ OrderID, MerchantID, MOP, Amount,
  CashRegisterID, UserID, OperationType, Comment, StatusCheck, Code,
  PaymentIntentID, CheckoutSessionID, CustomerEmail }`.

### Ce qui a été implémenté

1. **Migrations** (`053`, `054`, dans `migrations/` — non appliquées) :
   `payments.net_amount INT NOT NULL DEFAULT 0 AFTER fee` et
   `stripe_accounts.terminal_location_id VARCHAR(255) NULL`. Aucun renommage de
   colonne existante.
2. **`net_amount` initialisé à la création** : `AddPaymentAndReturnID` insère
   `net_amount = amount` **pour tous les paiements**, en injectant `payment.Amount`
   dans la colonne `net_amount` de l'INSERT — **aucun appelant modifié** (pas de
   nouveau champ obligatoire à renseigner partout, `net_amount` ne peut donc pas
   rester à 0 par omission d'un appelant).
3. **`net_amount` mis à jour aux frais réels** : `UpdateFees` (webhook) — voir
   audit 0.A ci-dessus.
4. **`cash_register_id` vide → NULL** : `AddPaymentAndReturnID` convertit une
   `CashRegisterID` vide en `NULL` (`sql.NullString`), pour qu'un paiement borne
   (sans caisse) respecte le point 5 du brief. Les appelants existants passent
   toujours une valeur non vide → comportement inchangé.
5. **Enregistrement du paiement Terminal** : dans
   `handleTerminalPaymentSucceeded` (webhook `payment_intent.succeeded`,
   card_present), après `SetOrderAccepted`, appel de
   `recordTerminalPayment` → `CreatePaymentNoNotification` avec `amount =
   pi.Amount`, `mop = models.StripeMOP` (**même valeur que le Checkout carte en
   ligne**, cohérence multi-canal), `fee = 0` (défaut) / `net_amount = amount`
   (via l'INSERT), `user_id = "KIOSK"`, `cash_register_id = NULL`, `order_id` /
   `merchant_id` depuis le mapping Redis. **Best-effort** : la commande est déjà
   acceptée ; un échec d'insertion est loggé sans faire échouer le webhook (éviter
   un rejeu Stripe qui re-déclencherait l'accept et se heurterait au garde de
   montant à la ré-insertion). L'insertion de `stripe_payments` (faite en interne
   pour `MOP=STRIPE`) relie `payment_intent_id`, ce qui rend le paiement Terminal
   éligible à la mise à jour `fee`/`net_amount` par `charge.captured`.
6. **`terminal_location_id` dans `GET /kiosk/settings`** : nouveau champ
   `terminal_location_id` (nullable) dans `KioskSettingsResponse`, alimenté par
   `Repository.GetTerminalLocationID` (lecture `stripe_accounts` par
   `merchant_id`). `null` si pas de ligne `stripe_accounts` ou colonne NULL —
   jamais d'erreur. Exposé aussi dans le `GET /pos/settings/kiosk` back-office
   (même méthode `GetSettings`), sans effet indésirable.
7. **Annulation `pending_card_payment`** : `CancelKioskOrder` accepte désormais
   `PENDING_APPROVAL` **et** `PENDING_CARD_PAYMENT`. Pour une commande carte, le
   PaymentIntent actif est annulé (`CancelActivePaymentIntentForOrder`,
   best-effort avec warning — cohérent avec `SwitchToCounterPayment`) **avant**
   `DeleteOrder`, pour éviter un PaymentIntent orphelin capturé plus tard.

### Vérifications

`go build ./...` et `go vet` (paquets touchés : webhook/stripe, kiosk,
order_life_cycle, infrastructure/stripe) clean.
`go test ./internal/modules/kiosk/... ./internal/modules/order_life_cycle/...`
passent. Tests manuels DB/Stripe non exécutés (pas de `MYSQL_URL`/clé Stripe test
dans ce sandbox) — appliquer les migrations `053`/`054`, renseigner
`stripe_accounts.terminal_location_id` manuellement pour le merchant de test,
puis rejouer le scénario Terminal ci-dessus en vérifiant en base : une ligne
`payments` (`mop='STRIPE'`, `net_amount=amount`, `fee=0`, `cash_register_id`
NULL) après `payment_intent.succeeded`, puis `fee`/`net_amount` mis à jour après
`charge.captured`.

---

## Incrément Terminal 3 — frais d'application configurables par merchant (kiosk_settings)

### Étape 0 — Audit obligatoire [CONSTAT]

**1. Formule exacte (scannorder / Checkout web)** — `stripeclient.CreateCheckoutSession`
(`internal/infrastructure/stripe/checkout.go:18-30`, le chemin réellement actif ;
`CreateCheckoutSessionOld` est une variante legacy non appelée, avec le même calcul) :

```go
variableFees := *merchant.VariableFees
fixedFees := *merchant.FixedFees
ttc := order.TTC
fees := int64(math.Floor(float64(ttc)*variableFees + float64(fixedFees) + 0.5))
```

Soit `application_fee_amount = floor(TTC_centimes * variable_fees + fixed_fees + 0.5)`
(round-half-up manuel, `ttc` déjà en centimes).

**2. Lecture de `scannorder_settings`** — `scannorder.Repository.GetMerchantByQR`
(`internal/modules/scannorder/repository.go:33`) sélectionne
`snos.variable_fees, snos.fixed_fees` dans la même requête qui résout le merchant
depuis le QR code (`INNER JOIN scannorder_settings snos ON snos.merchant_id = m.id`),
au moment de `GetMerchant`/`computeGetMerchant` (mis en cache Redis, voir
`ARCHITECTURE_API.md` §2.4). Le `models.MerchantRow` résultant porte
`VariableFees *float64` / `FixedFees *int`, propagés jusqu'à
`CheckoutSessionRequestObject.Merchant` au moment de créer la Checkout Session
(paiement TAKE_AWAY/DELIVERY).

**3. Valeur d'`ApplicationFeeAmount` dans `CreateTerminalPaymentIntent` AVANT cette
tâche** — **déjà calculée et transmise** (ni `0`, ni absente) : implémentée à
l'incrément "paiement carte borne (Stripe Terminal)" (ligne 2158 de ce document),
avec exactement la même formule que le Checkout web
(`internal/infrastructure/stripe/terminal.go`, ancien
`fees := int64(math.Floor(float64(amountCents)*variableFees + float64(fixedFees) + 0.5))`).
**Mais la source des frais était `scannorder_settings`**, pas une configuration
Kiosk dédiée : l'ancien `terminalAccountStore.GetTerminalAccount` faisait
`SELECT sa.account_id, snos.variable_fees, snos.fixed_fees FROM stripe_accounts sa
INNER JOIN scannorder_settings snos ON snos.merchant_id = sa.merchant_id`. Deux
conséquences concrètes de ce couplage, corrigées par cette tâche :
- un merchant Kiosk actif sans ligne `scannorder_settings` (ScanNOrder jamais
  activé) aurait fait échouer tout paiement Terminal (`INNER JOIN` vide →
  `ErrNoStripeAccount`, alors que le compte Stripe existe bel et bien) ;
- un merchant avec les deux canaux actifs ne pouvait pas avoir une commission
  Kiosk différente de sa commission ScanNOrder — pas de configuration par canal.

### Ce qui a été implémenté

1. **Migration `061_kiosk_settings_fees`** (`migrations/`, non appliquée) :
   `kiosk_settings.variable_fees DECIMAL(10,4) NOT NULL DEFAULT 0.0070` et
   `kiosk_settings.fixed_fees INT NOT NULL DEFAULT 15`, placées après
   `pay_at_counter_enabled`. Défauts alignés sur ceux de `scannorder_settings`
   pour que les merchants existants gardent la même commission effective au
   déploiement — aucun backfill nécessaire.
   - **Écart de convention constaté et documenté** : le brief demandait
     `migrations/todo/`. Ce dossier n'existe plus dans le repo (vérifié : seuls
     `migrations/` racine et `migrations/done/` existent aujourd'hui ; les
     migrations les plus récentes non appliquées, ex. `050`/`051`, vivent
     directement à la racine). La migration `061` suit donc l'état réel actuel
     du repo plutôt que la doc/le brief : posée à la racine de `migrations/`,
     à déplacer vers `migrations/done/` une fois appliquée en base.
2. **`kiosk.Repository.GetKioskFees(ctx, merchantID) (variableFees float64,
   fixedFees int64, err error)`** (`internal/modules/kiosk/repository.go`) :
   `SELECT variable_fees, fixed_fees FROM kiosk_settings WHERE merchant_id = ?`,
   retombe sur les valeurs par défaut du module (`0.0070`, `15`) si aucune ligne
   n'existe — jamais `sql.ErrNoRows` remonté à l'appelant, même garantie que
   `GetKioskSettings`.
3. **Non-exposition côté API** : `GetKioskFees` est une requête dédiée, distincte
   de `GetSettingsByMerchant`/`KioskSettingsRow` (qui listent leurs colonnes
   explicitement, pas de `SELECT *`) — `variable_fees`/`fixed_fees` n'entrent
   donc dans aucune réponse `GET /kiosk/settings` (device) ni
   `GET /pos/settings/kiosk/settings` (back-office). Vérifié : ces deux routes
   passent par `Service.GetSettings`/`KioskSettingsResponse`, jamais par
   `GetKioskFees`.
4. **Calcul déplacé côté appelant, pas dans l'infra Stripe** :
   `kiosk.Service.CreateTerminalPaymentIntent` appelle désormais
   `s.repo.GetKioskFees(ctx, kiosk.MerchantID)` puis transmet
   `variableFees, fixedFees` à `s.terminal.CreateTerminalPaymentIntent(...)`.
   `TerminalGateway.CreateTerminalPaymentIntent` (interface, `models.go`) et
   `stripeclient.TerminalService.CreateTerminalPaymentIntent` (implémentation,
   `terminal.go`) prennent désormais ces deux valeurs en paramètres explicites
   plutôt que de les résoudre eux-mêmes — la formule (`floor(amount*variable +
   fixed + 0.5)`, identique à `CreateCheckoutSession`) reste dans
   `stripeclient`, seule la **source** des frais change.
5. **`TerminalAccountStore` simplifié** : ne résout plus que l'`account_id`
   Stripe connecté (`SELECT account_id FROM stripe_accounts WHERE merchant_id = ?`,
   sans jointure `scannorder_settings`) — `CreateConnectionToken` et
   `cancelOnStripe` n'avaient d'ailleurs jamais utilisé les frais retournés par
   l'ancienne signature à 4 valeurs, seul `CreateTerminalPaymentIntent` s'en
   servait.

### Vérifications

`go build ./...` et `go vet ./...` clean (aucune erreur nouvelle — les warnings
`go vet` préexistants sur `auth`/`ubereats`/`pos/accounting`/`tasks` sont sans
rapport avec ce changement, non touchés). `go test ./internal/modules/kiosk/...
./internal/infrastructure/stripe/...` passent. Tests manuels DB non exécutés
(pas de `MYSQL_URL` dans ce sandbox) — avant mise en prod : appliquer la
migration `061`, puis vérifier qu'un `POST /kiosk/terminal/payment-intent` sur un
merchant **sans** ligne `kiosk_settings` calcule bien la commission avec les
valeurs par défaut (`0.0070`/`15`), et qu'un `UPDATE kiosk_settings SET
variable_fees = ..., fixed_fees = ...` change effectivement
`application_fee_amount` sur le PaymentIntent créé (vérifiable côté dashboard
Stripe ou en inspectant la réponse de l'API Stripe), sans toucher au comportement
du Checkout ScanNOrder existant (toujours sur `scannorder_settings`, inchangé).

---

## Incrément — composants produit dans `KioskProduct` [DÉCIDÉ]

La donnée existait déjà côté source (`models.ProductEntry.Components
[]models.ComponentUsage`, peuplée par le pipeline menu existant) mais n'était
jamais recopiée vers `KioskProduct` — seul blocage pour l'UI de retrait de
composants côté borne.

**Champs exposés** : nouvelle struct `KioskProductComponent{ID, Name}`
(`internal/modules/kiosk/models.go`), champ `KioskProduct.Components
[]KioskProductComponent` avec `omitempty` (absent du JSON pour les produits
sans composant). Peuplé dans `mapProductEntryToKioskProduct`
(`internal/modules/kiosk/service.go`) par simple recopiage de
`p.Components` (id + name uniquement).

**Décision — pas de `price`/`status`** : `ComponentUsage` porte aussi
`Price`/`Status`/`Quantity`/`UnitOfMeasure`/`Cost`, mais le payload `without`
envoyé par le client à la commande n'utilise que `component_id` (voir
`OrderProductWithout`, `internal/models/menu_models.go`) — les autres champs
sont ignorés côté serveur pour ce flux. Les exposer aurait ajouté du poids au
payload menu sans usage réel côté kiosk ; à revoir uniquement si l'UI veut un
jour afficher un supplément de prix par composant retiré.

**Hors périmètre (inchangé)** : logique de pricing/commande, validation
min/max côté serveur, bug `computeTotals` — aucun n'a été touché.

**Vérifications** : `go build ./internal/modules/kiosk/...` et
`go vet ./internal/modules/kiosk/...` clean. Les échecs de `go build ./...`
sur `internal/tasks`/`auth`/`ubereats`/`pos/accounting` préexistent (vérifié
en stashant ce changement) et sont sans rapport.

---

## Bloquant identifié — homogénéisation du statut paiement carte Kiosk (`merchant_approval=ACCEPTED` + `brand_status=PENDING_CARD_PAYMENT`)

> Audit du 2026-07-15, préalable à la décision produit demandant :
> `merchant_approval = "ACCEPTED"` immédiat pour toutes les commandes Kiosk
> (carte et comptoir), et `brand_status = "PENDING_CARD_PAYMENT"` →
> `"PENDING"` pour porter l'attente de confirmation carte. **Aucune
> implémentation n'a été faite** — ce changement est bloqué par un problème
> de sécurité fonctionnelle identifié ci-dessous, à trancher avant tout code.

### Constat — le KDS-équivalent (écran cuisine POS Flutter) filtre uniquement sur `merchant_approval`

Il n'existe pas de module `internal/modules/kds/` dans ce backend — l'écran
de préparation cuisine est un composant du POS Flutter
(`wello_resto_flutter/lib/controllers/production_controller.dart`), alimenté
par la liste de commandes ouvertes que le backend expose déjà (filtre
`internal/modules/orders/repository.go:32` : `state IN ('OPEN') AND
brand_status NOT IN ('ONLINE_PAYMENT_PENDING')` — ce filtre backend ne
connaît pas `PENDING_CARD_PAYMENT` et laisse **déjà** passer ces commandes
vers le client, comportement inchangé par la présente demande).

Le filtre qui décide réellement qu'une commande doit s'afficher en
préparation est **côté client**, dans `production_controller.dart` :

```dart
// wello_resto_flutter/lib/controllers/production_controller.dart:121-132
List<OrderDto> get orders =>
    orderController.orders
        .where(
          (order) => order.merchantApproval == MerchantApprovalEnum.accepted,
        )
        .where(
          (order) =>
              !ProductionSettingsNotifier.displayOnlyPaidOrders.value ||
              order.isPaid == true,
        )
        .where((order) => getDisplayableProducts(order).isNotEmpty)
        .where((order) => order.isDistributed != true)
        ...
```

Trois gardes s'appliquent en cascade, mais **une seule est fiable dans tous
les cas** :

1. `merchantApproval == accepted` — **seul filtre garanti actif**, aucune
   dépendance à une configuration merchant.
2. `!displayOnlyPaidOrders.value || isPaid == true` — **filtre optionnel,
   désactivé par défaut**. `ProductionSettingsNotifier.displayOnlyPaidOrders`
   est initialisé à `false`
   (`wello_resto_flutter/lib/helpers/production_settings_notifier.dart:9-11`,
   confirmé rechargé depuis les préférences avec fallback `false` si absent
   — `:24-30`). Tant qu'un restaurateur n'a pas explicitement activé ce
   toggle dans les réglages production, cette clause est un no-op
   (`!false || ...` = toujours `true`) : elle ne filtre **aucune** commande
   non payée.
3. `isDistributed != true` / produits affichables non vides — sans lien avec
   le paiement.

**Aucune des trois clauses ne teste `brand_status`.**

### Pourquoi c'est bloquant avec le changement demandé

Aujourd'hui, une commande Kiosk carte en attente porte
`merchant_approval = "PENDING_CARD_PAYMENT"` (voir
`internal/modules/kiosk/service.go:1481`) — elle échoue donc **déjà** la
clause 1 (`== accepted`) et n'apparaît jamais en préparation avant
confirmation du paiement. C'est un filtrage *accidentel* (absence de `case`
plutôt que traitement explicite — déjà signalé dans
`docs/BRAND_STATUS_MERCHANT_APPROVAL_AUDIT.md` §2), mais il fonctionne.

Avec le changement demandé, `merchant_approval` devient `"ACCEPTED"`
**immédiatement à la création**, y compris pour `payment_method == "card"`.
La seule différence entre une commande carte en attente et une commande
carte confirmée serait alors `brand_status`
(`PENDING_CARD_PAYMENT` vs `PENDING`) — **un champ que
`production_controller.dart` ne lit jamais**.

**Conséquence concrète** : par défaut (`displayOnlyPaidOrders = false`, qui
est le réglage initial pour tout merchant tant qu'il ne l'a pas changé), une
commande Kiosk payée par carte apparaîtrait **en préparation cuisine avant
même que le client ait présenté sa carte au Terminal** — entre l'appel
`CreateOrder` et la réception du webhook `payment_intent.succeeded`
(`card_present`). Un client qui annule ou dont la carte est refusée
laisserait une commande déjà partie en cuisine, potentiellement déjà
préparée, sans paiement confirmé. C'est une régression fonctionnelle et un
risque financier direct (perte de marchandise préparée sans paiement), pas
un simple problème d'affichage.

### Correction nécessaire avant d'implémenter le changement demandé

Le filtre `production_controller.dart:121-125` doit être étendu pour exclure
explicitement `brand_status == "PENDING_CARD_PAYMENT"`, indépendamment de
`merchant_approval` :

```dart
.where(
  (order) =>
      order.merchantApproval == MerchantApprovalEnum.accepted &&
      order.brandStatus != 'PENDING_CARD_PAYMENT',
)
```

Cela suppose que `OrderDto` expose déjà `brandStatus` en clair (à vérifier —
`lib/models/orders/order_dto.dart`, voir aussi
`lib/models/orders/brand_status_enum.dart` cité dans
`docs/BRAND_STATUS_MERCHANT_APPROVAL_AUDIT.md` §1 : `BrandStatusEnum` est un
enum **fermé** à 12 valeurs avec `fromServerValue` retournant `null` sur
valeur inconnue — `"PENDING_CARD_PAYMENT"` n'y figure pas aujourd'hui et
devra y être ajoutée pour que la comparaison fonctionne de façon fiable,
plutôt que de comparer une chaîne brute côté Dart).

Cette correction touche le **client POS Flutter**
(`wello_resto_flutter`), pas ce backend — elle sort du périmètre de ce repo
et doit être livrée **avant ou en même temps** que le changement backend
demandé, jamais après (sinon fenêtre de régression en prod entre le déploi
backend et le déploi Flutter).

### Décision requise avant de poursuivre

Deux options, à trancher avec Ilies avant tout code :

- **Option A (recommandée)** : livrer la correction `production_controller.dart`
  (+ ajout de la valeur à `BrandStatusEnum`) **dans le même incrément** que
  le changement backend, coordonné avec un déploi simultané des deux
  applications. Nécessite d'ouvrir ce travail dans le repo
  `wello_resto_flutter` (hors scope de ce repo backend).
- **Option B** : conserver `merchant_approval = "PENDING_CARD_PAYMENT"` côté
  Kiosk (annuler la demande d'homogénéisation), qui a l'avantage de rester
  protégé par le filtre Flutter existant sans aucune modification côté
  client. C'est le statu quo documenté dans
  `docs/SCANNORDER_ONLINE_PAYMENT_LIFECYCLE_AUDIT.md` §7 comme
  "plus simple et plus robuste" que le pattern ScanNOrder.

**Aucune modification de code backend n'a été effectuée dans cette session**
tant que ce point n'est pas tranché — voir demande initiale, étape 0,
condition d'arrêt.

---

## Déblocage — homogénéisation appliquée (Option A backend, filtre étendu côté serveur)

> Session du 2026-07-15 (suite). Le blocage ci-dessus est levé en étendant
> **côté backend** le même filtre qui protège déjà `ONLINE_PAYMENT_PENDING` —
> approche différente des options A/B envisagées plus haut : au lieu de
> corriger le client Flutter, on empêche la commande carte en attente
> d'atteindre le POS en premier lieu, à la source (liste de commandes
> ouvertes). Détails de l'audit d'exhaustivité, de l'implémentation, et **un
> résidu de risque non couvert par le backend seul** (voir dernière section)
> ci-dessous.

### Étape 0 — audit d'exhaustivité de `ONLINE_PAYMENT_PENDING`

Grep exhaustif du repo (`ONLINE_PAYMENT_PENDING`, code uniquement, hors
docs/commentaires d'exemple) :

| Fichier:ligne | Rôle | Doit exclure `PENDING_CARD_PAYMENT` ? |
|---|---|---|
| `internal/modules/orders/repository.go:32` (`GetPendingOrderIDs`) | Alimente `GetPendingOrders` (`orders/service.go:132`) — **c'est la liste que le POS Flutter récupère au chargement/refresh** (`order_network_manager.dart: fetchPendingOrders/getPendingOrders`), source de `orderController.orders` filtré ensuite par `production_controller.dart`. | **Oui — étendu.** Seul point qui, à lui seul, empêchait déjà une commande `ONLINE_PAYMENT_PENDING` d'atteindre le POS. Sans extension, une commande Kiosk carte en attente (nouveau `brand_status='PENDING_CARD_PAYMENT'`, `merchant_approval` déjà `'ACCEPTED'`) l'aurait traversé sans être filtrée. |
| `internal/modules/integrations/repository.go:13` (`kpiExcludedStatuses`) | Utilisé uniquement par 3 requêtes de KPI revenus/nombre de commandes : `GetUberEatsIntegration` (`brand='UBER_EATS'`), `GetDeliverooIntegration` (`brand='DELIVEROO'`), `GetScanNOrderIntegration` (`brand='WELLO_RESTO' AND created_by='SCANNORDER'`). | **Non — vérifié, aucun changement.** Une commande Kiosk a `brand='WELLO_RESTO'` et `created_by='KIOSK'` (`kiosk/service.go`, constante `kioskCreatedBy`) : elle ne peut matcher **aucune** des trois clauses `WHERE` (ni `brand='UBER_EATS'`/`'DELIVEROO'`, ni `created_by='SCANNORDER'`). Il n'existe pas de `GetKioskIntegration` dans ce fichier. Structurellement impossible pour une commande Kiosk d'apparaître dans ces KPI, indépendamment de `brand_status`. De plus ces requêtes filtrent déjà `isPaid = 1`, ce qui exclurait `PENDING_CARD_PAYMENT` (toujours `isPaid=0`) même si le filtre `brand`/`created_by` ne le faisait pas. |
| `internal/webhook/stripe/service.go:121` | Commentaire (exemple illustratif dans un commentaire sur la fraîcheur du cache Redis), pas une clause SQL. | Non concerné — aucun code exécutable. |

**Autres clauses `brand_status` du repo, hors périmètre `ONLINE_PAYMENT_PENDING`** (vérifiées mais non modifiées, car elles ne citent jamais `ONLINE_PAYMENT_PENDING` et ne sont pas dans le périmètre demandé) : `internal/tasks/payments.go:22,39` (crons capture/annulation Stripe, filtrent `DENIED`/`CANCELED`), `internal/modules/orders/repository.go:234` (recherche admin par liste explicite de statuts, pas une exclusion), `internal/modules/pos/reports/*`, `internal/modules/pos/accounting/*`, `internal/modules/stats/repository.go` (tous filtrent `'DELETED','CANCELED'`, sans rapport avec le paiement en attente).

**Décision — pas de constante SQL partagée** : une seule occurrence de code a nécessité une modification (`orders/repository.go:32`). Conformément à la consigne ("ne pas factoriser sauf si plusieurs endroits identiques"), la chaîne reste écrite en clair, cohérente avec le style déjà en place pour `ONLINE_PAYMENT_PENDING` (aucune constante Go pour les valeurs de `brand_status`, voir `docs/order-lifecycle.md` §P8).

### Implémentation appliquée

1. **`internal/modules/orders/repository.go:32`** — `o.brand_status NOT IN('ONLINE_PAYMENT_PENDING', 'PENDING_CARD_PAYMENT')`.
2. **`internal/modules/kiosk/service.go` — `CreateOrder`** : `merchant_approval = "ACCEPTED"` dans tous les cas (carte et comptoir). Pour `payment_method == "card"`, `brand_status = "PENDING_CARD_PAYMENT"` est désormais écrit explicitement (sinon `setOrderDefaults` aurait posé `'PENDING'` par défaut, identique au comptoir — la commande carte serait indiscernable d'une commande déjà encaissée). Pour `pay_at_counter`, rien n'est forcé sur `brand_status` : le défaut existant (`'PENDING'`, car `OnlinePayment=false`) s'applique tel quel, comportement inchangé.
3. **`internal/webhook/stripe/service.go` — `handleTerminalPaymentSucceeded`** : remplace l'appel à `orderlifecycle.SetOrderAccepted` (qui touchait `merchant_approval`) par un appel à la nouvelle méthode `stripe.Repository.ConfirmKioskCardPayment(merchantID, orderID)` (`internal/webhook/stripe/repository.go`), qui exécute :
   ```sql
   UPDATE orders SET brand_status = 'PENDING', last_update = UTC_TIMESTAMP()
   WHERE order_id = ? AND merchant_id = ? AND brand_status = 'PENDING_CARD_PAYMENT'
   ```
   Guard `WHERE brand_status = 'PENDING_CARD_PAYMENT'` : idempotent si le webhook est rejoué. `merchant_approval` n'est plus touché par cette transition (déjà `'ACCEPTED'`). Invalidation du cache Redis de la commande et notification `order_update` reproduites manuellement (elles faisaient partie de `SetOrderAccepted`, désormais plus appelé pour ce chemin).
4. **`internal/modules/kiosk/service.go` — `CancelKioskOrder`** : la garde d'autorisation d'annulation vérifie désormais `brand_status == "PENDING_CARD_PAYMENT"` (via le nouveau helper `isKioskCardPending`) au lieu de `merchant_approval == "PENDING_CARD_PAYMENT"` — ce dernier ne peut plus jamais être vrai pour une commande créée après ce déploiement, puisque `merchant_approval` est toujours `"ACCEPTED"`.

### Écarts au-delà de la liste explicite du prompt (correctifs nécessaires trouvés en cours d'implémentation)

Le prompt listait `CreateKioskOrder`, le webhook, et `CancelKioskOrder`. En traçant tous les appelants de `models.MerchantApprovalPendingCardPayment` (`grep` exhaustif sur `PENDING_CARD_PAYMENT`), deux fonctions supplémentaires gataient exclusivement sur `merchant_approval == PENDING_CARD_PAYMENT` et **auraient cessé de fonctionner à 100%** sans correction (plus aucune commande n'aurait jamais matché cette condition, puisque `merchant_approval` est désormais toujours `"ACCEPTED"`) :

- **`CreateTerminalPaymentIntent`** (`kiosk/service.go`) — gate `order.MerchantApproval != PendingCardPayment` → remplacé par `!isKioskCardPending(order)`. Sans ce correctif, plus aucun PaymentIntent Terminal n'aurait pu être créé pour aucune commande carte.
- **`SwitchToCounterPayment`** (`kiosk/service.go`) — même gate, **et** la transition elle-même : elle appelait `UpdateOrderMerchantApproval(..., PENDING_APPROVAL)`, ce qui aurait fait régresser l'invariant "merchant_approval toujours ACCEPTED" à chaque bascule carte→caisse. Remplacé par une nouvelle méthode `kiosk.Repository.ConfirmKioskCardToCounterBrandStatus` (transition `brand_status: PENDING_CARD_PAYMENT → PENDING`, symétrique à `stripe.Repository.ConfirmKioskCardPayment`), `merchant_approval` n'est plus touché. Le reste de la fonction (réutilisation de `ConfirmCounterPayment` pour le code de retrait/QR/notification) est inchangé — son branchement interne sur `merchant_approval=="PENDING_APPROVAL"` ne s'active simplement plus (il est déjà `"ACCEPTED"`), ce qui est le comportement correct.
- **`mapMerchantApprovalToKioskStatus`** (`kiosk/service.go`) — sans mise à jour, cette fonction aurait renvoyé `"accepted"` à la borne dès la création d'une commande carte (puisque `merchant_approval` est `"ACCEPTED"` immédiatement), masquant l'attente de paiement à l'écran kiosk. Vérifie désormais `order.BrandStatus == "PENDING_CARD_PAYMENT"` **avant** le switch sur `merchant_approval`.

**Nouveau helper `isKioskCardPending(order)`** (`kiosk/service.go`) : `true` si `brand_status == "PENDING_CARD_PAYMENT"` **ou** (fallback) `merchant_approval == models.MerchantApprovalPendingCardPayment`. Le fallback n'est pas demandé explicitement par le prompt — ajouté pour la sécurité du déploiement : une commande créée par l'**ancien** code juste avant le déploiement (donc `merchant_approval="PENDING_CARD_PAYMENT"`, `brand_status="PENDING"` par défaut de l'ancien comportement) resterait sinon totalement bloquée après déploiement du nouveau code (ni annulable, ni capable de recevoir un PaymentIntent Terminal, ni basculable vers la caisse) jusqu'à intervention manuelle. Ce fallback — comme le fallback équivalent dans `mapMerchantApprovalToKioskStatus` — **est à retirer dans une session de nettoyage dédiée** une fois qu'aucune commande créée par l'ancien code ne peut plus être en vol (quelques heures/jours après le déploiement).

### Vérifications

`go build ./...` clean. `go vet ./...` clean sur les fichiers modifiés (warnings pré-existants et sans rapport sur `auth`/`pos/accounting`/`ubereats`/`cmd/api/routes.go`, déjà signalés dans les incréments précédents). `go test ./internal/modules/kiosk/... ./internal/webhook/stripe/... ./internal/modules/orders/...` passent. Tests manuels DB non exécutés (pas de `MYSQL_URL` dans ce sandbox) — avant mise en prod, valider en particulier : création carte → vérifier `brand_status='PENDING_CARD_PAYMENT'` et absence de la commande dans `/orders/pending` ; webhook `payment_intent.succeeded` (`card_present`) → vérifier `brand_status='PENDING'`, `merchant_approval` inchangé (`'ACCEPTED'`) ; annulation d'une commande carte en attente ; bascule carte→caisse.

### Nettoyage différé — `models.MerchantApprovalPendingCardPayment`

**Non supprimée dans cette session** (risque de casser un usage non détecté).
État réel après ce changement : la constante n'est plus jamais **écrite** en
base par le code actuel (`CreateOrder` pose désormais `merchant_approval =
"ACCEPTED"` dans tous les cas). Elle est encore **lue** à trois endroits, tous
en fallback rétrocompatibilité pour des commandes créées par l'ancien code :
`mapMerchantApprovalToKioskStatus` (switch), `isKioskCardPending` (fallback),
et implicitement partout où `isKioskCardPending` est appelé
(`CancelKioskOrder`, `CreateTerminalPaymentIntent`, `SwitchToCounterPayment`).
À supprimer, avec ses trois usages de fallback, dans une session dédiée une
fois confirmé qu'aucune commande en base ne porte plus
`merchant_approval='PENDING_CARD_PAYMENT'` (`SELECT COUNT(*) FROM orders WHERE
merchant_approval='PENDING_CARD_PAYMENT'` doit retourner 0 avant ce nettoyage).
`internal/modules/kiosk/repository.go` : `UpdateOrderMerchantApproval` est
également signalée comme potentiellement morte (plus aucun appelant après le
remplacement de `SwitchToCounterPayment`) — même remarque, à vérifier/retirer
dans la même session.

### ⚠️ Note critique pour la session POS Flutter — le backend seul NE ferme PAS complètement la boucle

Le déblocage ci-dessus étend le filtre **backend** (`GetPendingOrderIDs`), ce
qui protège le chemin de rafraîchissement standard du POS (polling au
chargement / après notification, `fetchPendingOrders()`). **Mais
`ONLINE_PAYMENT_PENDING` bénéficie aujourd'hui d'une deuxième protection, côté
client, que `PENDING_CARD_PAYMENT` n'a pas encore** — et cette deuxième
protection couvre un chemin que le filtre backend ne couvre pas :

**`wello_resto_flutter/lib/controllers/order/order_network_manager.dart` — `updateOrderFromPushNotificationData` (lignes ~117-160)**, le handler appelé à la réception d'une notification push/WebSocket `order_update` :

```dart
// order_network_manager.dart:141-143
if (orderFromServer.state == 'CLOSED' ||
    orderFromServer.brandStatus ==
        BrandStatusEnum.onlinePaymentPending.value) {
  // commande retirée/non ajoutée à la liste locale `orders`
  ...
  return null;
}
```

Ce handler **ne passe jamais par `GetPendingOrderIDs`** : à la réception d'une
notification, il appelle `_getOrder(orderId)` pour récupérer **cette commande
précise par son ID**, directement — le filtre SQL backend étendu dans cette
session ne s'applique pas à cet appel (il ne s'applique qu'à la liste
paginée). Seule cette vérification côté client (`brandStatus ==
onlinePaymentPending`) empêche aujourd'hui une commande `ONLINE_PAYMENT_PENDING`
d'être ajoutée à `orders` (et donc de potentiellement apparaître en
production, selon `production_controller.dart`) via ce chemin.

**Conséquence concrète** : `internal/modules/order_life_cycle/service.go:981`
(`OrdersLifeCycleService.CreateOrder`) envoie un `SendNotificationAsync(...,
NotificationTypeOrderUpdate)` à **chaque** création de commande, y compris
Kiosk carte. Un POS connecté en WebSocket au moment de la création recevrait
donc cette notification, appellerait `_getOrder`, obtiendrait la commande
avec `brand_status='PENDING_CARD_PAYMENT'` — non exclu par la condition
ci-dessus — et l'ajouterait à `orders`, où elle passerait le filtre
`production_controller.dart:124` (`merchantApproval == accepted`, vrai
immédiatement avec ce changement). **La faille identifiée dans la section
"Bloquant identifié" plus haut n'est donc que partiellement corrigée par le
changement backend seul** : le chemin de polling est fermé, le chemin
notification temps réel reste ouvert.

Fenêtre de risque réelle : la commande resterait visible jusqu'au prochain
rafraîchissement complet de la liste via `fetchPendingOrders()` (qui, lui,
bénéficiera correctement du filtre backend étendu une fois ce changement
déployé) — donc une fenêtre courte plutôt qu'indéfinie, mais bien réelle et
non nulle, potentiellement suffisante pour qu'un plat parte en préparation
sur une commande non payée.

**Correctif nécessaire, session POS (ne pas implémenter ici)** :

1. `order_network_manager.dart:141-143` — étendre la condition existante,
   même modèle qu'`onlinePaymentPending` :
   ```dart
   if (orderFromServer.state == 'CLOSED' ||
       orderFromServer.brandStatus == BrandStatusEnum.onlinePaymentPending.value ||
       orderFromServer.brandStatus == BrandStatusEnum.pendingCardPayment.value) {
   ```
2. **Pré-requis** : `BrandStatusEnum` (`lib/models/orders/brand_status_enum.dart:4-37`)
   est un enum **fermé** — `"PENDING_CARD_PAYMENT"` n'y figure pas
   aujourd'hui (seul `onlinePaymentPending("ONLINE_PAYMENT_PENDING")` existe
   pour ce genre de statut). Il faut l'y ajouter (`pendingCardPayment
   ("PENDING_CARD_PAYMENT")`) avant que la comparaison ci-dessus fonctionne —
   sinon comparer contre une valeur inexistante dans l'enum, ou comparer une
   chaîne brute `'PENDING_CARD_PAYMENT'` sans passer par l'enum (fonctionnellement
   correct mais incohérent avec le reste du fichier qui utilise
   `BrandStatusEnum.x.value` partout).
3. **`production_controller.dart:121-125`** (déjà documenté dans la section
   "Bloquant identifié" plus haut) reste également à corriger pour une défense
   en profondeur — même si `order_network_manager.dart` est corrigé, une
   commande pourrait théoriquement atteindre `orders` par un autre chemin futur
   non audité ; un filtre `brand_status` au niveau de l'écran production
   lui-même est plus robuste qu'un filtre unique en amont.
4. **Coordination de déploi** : tant que ces trois points Flutter ne sont pas
   livrés, le risque résiduel décrit ci-dessus subsiste. Recommandation :
   traiter la session POS comme un prérequis au déploiement production du
   changement backend de cette session, pas comme un simple suivi optionnel.

---

## Diagnostic transition brand_status bloquée (paiement Terminal réel confirmé, `pi_3TvjE1ISGuDm6FEV2OM6JgTE`)

Audit du 2026-07-21 : un paiement carte Stripe Terminal réel (mode test),
confirmé côté Stripe (`payment_intent.succeeded` reçu), n'a pas fait
transiter `orders.brand_status` de `PENDING_CARD_PAYMENT` vers `PENDING`.
Investigation menée dans l'ordre webhook reçu → mapping Redis → requête SQL
→ erreur silencieuse, sur la version **actuelle** (post-conversion Postgres)
du code, pas la version documentée plus haut dans ce fichier.

### Cause confirmée : non certaine — deux points de défaillance silencieuse identifiés, aucun accès aux logs Render / dashboard Stripe pour trancher lequel s'est produit pour ce `pi_id` précis

Le code contient **deux angles morts d'observabilité distincts**, chacun
suffisant à lui seul pour reproduire exactement le symptôme rapporté (webhook
reçu, aucune erreur visible, `brand_status` jamais modifié) :

1. **Écriture du mapping Redis jamais vérifiée** — `CreateTerminalPaymentIntent`
   (`internal/infrastructure/stripe/terminal.go`) appelle `storeMapping` après
   création du PaymentIntent sur l'API Stripe. `storeMapping` appelait
   `t.mapping.Set(...)` (x2 : mapping direct + inverse) **sans jamais lire la
   valeur de retour** (`bool` de succès) ni logger un échec. Si Redis était
   indisponible/lent au moment précis de la création du PaymentIntent (avant le
   paiement), l'appelant (`CreateOrder` Kiosk) recevait quand même un
   `client_secret` valide et un succès complet — le paiement carte pouvait donc
   parfaitement aboutir côté Stripe (ce qui correspond exactement à ce qui a été
   observé) alors que le mapping `terminal_pi:{id}` n'existait jamais.
2. **Lecture du mapping Redis (webhook) sans aucun log** — `lookupTerminalMapping`
   (`internal/webhook/stripe/service.go`) retournait silencieusement
   `found=false` que la clé soit absente, expirée (TTL 1h), ou le JSON illisible
   — aucune trace, dans aucun des deux cas. Conséquence en cascade : dans
   `HandlePaymentIntentSucceeded`, `handled=false` fait tomber l'event sur
   `s.repo.UpdatePaymentIntentStatus(ctx, pi.ID, "CAPTURED")`
   (`internal/webhook/stripe/repository.go:332-337`), qui met à jour
   **`stripe_payments.payment_intent_status`**, une table/colonne **différente**
   de `orders.brand_status` — cette requête ne matche aucune ligne pour un
   paiement Terminal (aucune ligne `stripe_payments` n'existe pour ce `pi_id`,
   `InsertStripePayment` n'étant appelé que par le flux Checkout web), n'échoue
   pas, et le handler HTTP (`http_handler.go`) répond **200** à Stripe. Stripe
   considère donc la livraison comme réussie et **ne retente jamais l'event** —
   ce qui est cohérent avec le fait que le paiement soit resté bloqué durablement
   plutôt que de se corriger tout seul après quelques minutes.

Dans les deux cas, le comportement observable serait **identique** : webhook
reçu, HTTP 200 renvoyé à Stripe, aucune erreur, `brand_status` jamais modifié.
Le code seul ne permet pas de distinguer lequel des deux s'est produit pour
`pi_3TvjE1ISGuDm6FEV2OM6JgTE` — voir "Ce qui manque pour confirmer" ci-dessous.

**Écarté avec un niveau de confiance élevé** :
- **Requête SQL Postgres cassée sur la transition elle-même** — `ConfirmKioskCardPayment`
  (`internal/webhook/stripe/repository.go:181-192`) et sa fonction sœur
  `ConfirmKioskCardToCounterBrandStatus` (`internal/modules/kiosk/repository.go:787-796`)
  utilisent bien `dbx.GetDB(ctx, r.database)` (pas un accès direct à `*sql.DB`),
  ce qui fait passer chaque requête par `dbx.Rebind()` : les placeholders `?`
  sont réécrits en `$1, $2, ...` automatiquement quand `DB_DIALECT=postgres`
  (`internal/database/dbx/dialect.go`), et laissés inchangés sinon. Le guard
  idempotent (`WHERE brand_status = 'PENDING_CARD_PAYMENT'`) est une comparaison
  de chaîne exacte, écrite avec la même casse des deux côtés (`kiosk/service.go:1550`
  écrit littéralement `"PENDING_CARD_PAYMENT"`) — pas de piège de casse
  MySQL/Postgres identifié.
- **`rowsAffected` non vérifié** — il l'est (`res.RowsAffected()`, variable
  `confirmed`), mais son résultat `false` n'était (avant ce correctif) jamais
  logué : un no-op silencieux était possible mais indiscernable d'un succès.
- **Erreur avalée avec 200 renvoyé quand même** — vérifié : `http_handler.go`
  renvoie bien **500** dès que `ProcessEvent` retourne une erreur non nil
  (`if err := h.service.ProcessEvent(...); err != nil { http.Error(w, ..., 500) }`).
  Une erreur SQL réelle (contrainte, syntaxe) serait donc visible côté Stripe
  (delivery en échec, "Recent deliveries" rouge, retries automatiques sur
  plusieurs jours) — ce qui rend l'hypothèse "requête SQL en erreur" peu
  probable si le dashboard Stripe montre cette delivery comme réussie (à
  vérifier, voir ci-dessous).

### Preuve

- `internal/infrastructure/stripe/terminal.go` (avant correctif) : `storeMapping`
  n'inspectait pas la valeur de retour de `t.mapping.Set(...)`.
- `internal/webhook/stripe/service.go` (avant correctif) : `lookupTerminalMapping`
  ne loguait aucune de ses quatre branches de sortie (`redis nil`, `not found`,
  `unmarshal error`, `champs vides`) ; `handleTerminalPaymentSucceeded` ne loguait
  pas le cas `confirmed == false`.
- `internal/webhook/stripe/repository.go:332-337` (`UpdatePaymentIntentStatus`) :
  confirmé comme le chemin de repli silencieux emprunté quand
  `handleTerminalPaymentSucceeded` retourne `handled=false` — écrit dans
  `stripe_payments`, jamais dans `orders`.
- `internal/webhook/stripe/http_handler.go:31-34` : confirmé que les erreurs
  remontent bien en HTTP 500 (pas de 200 masquant un échec interne).
- `internal/modules/kiosk/repository.go:787-796` : confirmé que la requête
  sœur de bascule carte→caisse utilise le même mécanisme `dbx.GetDB`/`Rebind`
  correctement, cohérence avec `ConfirmKioskCardPayment`.

### Bug adjacent trouvé et corrigé (non responsable du symptôme actuel, mais réel)

`internal/infrastructure/stripe/terminal.go`, `terminalAccountStore.GetTerminalAccount`
exécutait `SELECT account_id FROM stripe_accounts WHERE merchant_id = ? LIMIT 1`
via `s.db.QueryRowContext` **directement**, sans passer par `dbx.GetDB`/`dbx.Rebind`
— seul endroit du flux Terminal à contourner ce mécanisme. Sous
`DB_DIALECT=postgres`, cette requête aurait échoué systématiquement (placeholder
`?` non supporté par le driver Postgres), bloquant `CreateConnectionToken` et
`CreateTerminalPaymentIntent` **avant même** la création du PaymentIntent —
incompatible avec le fait que le paiement de cet incident ait réellement
abouti côté Stripe. Cela signifie soit que l'environnement testé tournait
encore avec `DB_DIALECT=mysql` (défaut documenté en prod, voir
`docs/migration-postgres/25-tier2-conversion-log.md`/`27-tier3-conversion-log.md`),
soit qu'un test antérieur a réussi avant qu'une bascule de dialecte ne casse ce
point précis. Corrigé par cohérence (`dbx.Rebind(q)` avant exécution) — c'est
un correctif SQL Postgres non ambigu au sens de la règle 2, mais **il ne doit
pas être présenté comme la cause de cet incident**.

### Correction appliquée dans cette session

Ajout de logs explicites (aucun changement de comportement métier) aux deux
points de silence identifiés, pour que la **prochaine** occurrence soit
diagnosticable en quelques secondes de grep sur les logs Render :

1. `internal/infrastructure/stripe/terminal.go` — `storeMapping` logue
   désormais un warning si l'un des deux `Set` Redis échoue, avec `pi_id`/
   `order_id`/`merchant_id`, et un info sur succès.
2. `internal/webhook/stripe/service.go` — `lookupTerminalMapping` logue
   chacune de ses quatre branches de sortie (redis nil, clé absente,
   JSON illisible, champs vides) et le cas trouvé, toujours avec le `pi_id`.
3. `internal/webhook/stripe/service.go` — `handleTerminalPaymentSucceeded`
   logue désormais une erreur explicite sur échec de `ConfirmKioskCardPayment`,
   et un warning si `confirmed == false` (guard no-op).
4. `internal/infrastructure/stripe/terminal.go` — `GetTerminalAccount` route
   maintenant sa requête par `dbx.Rebind`.

`go build ./...` et `go vet ./internal/webhook/stripe/... ./internal/infrastructure/stripe/...`
passent sans erreur. Les tests d'intégration Postgres de ces deux paquets
(`postgres_integration_test.go`, tag `postgres_integration`) n'ont pas pu être
exécutés dans cette session (pas de `POSTGRES_URL`/instance Postgres
disponible dans ce sandbox, même contrainte que les incréments précédents).

### Ce qui manque pour confirmer la cause exacte de cet incident précis

Aucun des éléments ci-dessous n'est disponible dans cet environnement de
travail — à vérifier directement par Ilies :

1. **Dashboard Stripe → Developers → Webhooks → cet endpoint → Recent
   deliveries**, filtré sur `pi_3TvjE1ISGuDm6FEV2OM6JgTE` : confirme si
   l'event `payment_intent.succeeded` a bien été livré, avec quel code de
   réponse HTTP. Un **200** pointe vers l'hypothèse 2 (mapping non trouvé,
   repli silencieux vers `UpdatePaymentIntentStatus`). Un **500** ou une
   absence totale de delivery pointerait vers autre chose (webhook jamais
   appelé, ou erreur réellement remontée — à ré-examiner dans ce cas).
2. **Logs applicatifs Render** au moment de l'event (avant ce correctif, donc
   probablement rien d'exploitable pour *cet* incident précis — mais à
   vérifier si un log générique existe déjà en amont, ex. logging HTTP de la
   route `/webhooks/stripe`).
3. **État actuel de la clé Redis** `terminal_pi:pi_3TvjE1ISGuDm6FEV2OM6JgTE`
   (probablement expirée depuis, TTL 1h) et, si possible, si elle a existé à
   un moment — non vérifiable après coup sans historique Redis.
4. **Valeur de `DB_DIALECT` dans l'environnement où ce test a été effectué** —
   détermine si le bug adjacent `GetTerminalAccount` (ci-dessus) était même
   pertinent pour cet essai, et confirme que le mécanisme `dbx.Rebind` était
   actif (ou non) pour `ConfirmKioskCardPayment` au moment du test.

Ne pas deviner au-delà de ce qui précède : si un nouveau paiement Terminal
reproduit le blocage après déploiement de ce correctif, les nouveaux logs
`[stripe terminal]` permettront de trancher entre les deux hypothèses en un
seul grep sur le `pi_id`.

---

## Diagnostic complémentaire — events Stripe Connect (comptes connectés) non reçus

Suite à la piste apportée par Ilies : les paiements Terminal utilisent des
**direct charges** (`Stripe-Account` header / `SetStripeAccount`), donc leurs
`payment_intent.succeeded`/`charge.captured` sont émis **côté compte
connecté**, pas côté plateforme. Seul `application_fee.created` (toujours
émis côté plateforme, quel que soit le modèle de charge) est arrivé pour ce
paiement. Vérification du code (`http_handler.go`, `service.go`, `models.go`)
+ recherche de la doc Stripe officielle (`docs.stripe.com/connect/webhooks`).

### 1. Le handler lit-il `event.Account` ?

**Le champ existe déjà et est déjà utilisé — mais partiellement.**
`StripeEvent.Account` (`models.go:12`, commenté `// IMPORTANT : L'ID du compte
connecté (acct_...)`) est déjà déclaré et déjà threadé pour deux event types :
`charge.captured` → `HandleRetrieveFees(ctx, event.Data.Object, event.Account)`
et `payout.paid` → `HandlePayoutPaid(ctx, event.Data.Object, event.Account)`
(`service.go`, dispatch de `ProcessEvent`). Les deux en ont un besoin réel :
`HandleRetrieveFees` appelle `balancetransaction.Get` avec
`params.SetStripeAccount(connectedAccountID)` — sans le bon compte, l'appel
API Stripe échouerait ou lirait le mauvais solde.

**Ce qui ne le lisait pas (avant ce correctif)** : `payment_intent.succeeded`/
`.canceled`/`.payment_failed` étaient dispatchés sans `event.Account`
(`HandlePaymentIntentSucceeded(ctx, event.Data.Object)`, signature à 2
arguments). Le code supposait implicitement un event plateforme pour ces
trois types précis — pas par une logique métier qui casserait (voir point 3),
mais par simple absence de plomberie, ce qui rendait impossible de confirmer
par les logs si un event Terminal reçu était bien scopé Connect ou non.

### 2. Secret de vérification de signature webhook : même secret, ou distinct ?

**Distinct, et obligatoirement sur un endpoint séparé** — vérifié sur
`docs.stripe.com/connect/webhooks` :

> Each event from a connected account contains a top-level `account` property
> [...] assign the scope by setting **Events from** to **Your account** or
> **Connected accounts** [...] via the API, assign the scope by setting the
> `connect` parameter to `false` (Your account) ou `true` (Connected accounts).

Point important découvert (et qui corrige une hypothèse initiale) : **un seul
endpoint Stripe ne peut pas être scopé sur les deux à la fois** — "Your
account" et "Connected accounts" sont deux configurations d'endpoint
mutuellement exclusives dans le modèle actuel de Stripe (Dashboard Workbench
ou API `connect` param), chacune avec **son propre secret de signature**
(`whsec_...`), et chacune avec **sa propre liste d'event types sélectionnés**
(pas de parité obligatoire entre les deux).

**Conséquence pour ce projet** : `VerifySignature` (`service.go`, fin du
fichier) **n'est actuellement pas implémentée** (corps vide, commentaire "À
implémenter") **et n'est appelée nulle part** (`http_handler.go` ne fait
aucune vérification de signature — confirmé par grep, contrairement au
webhook UberEats qui appelle bien la sienne). Donc la question "même secret ou
distinct" est aujourd'hui sans objet en pratique (aucun secret n'est vérifié
du tout, quelle que soit la scope) — mais **si cette vérification est
implémentée plus tard**, il faudra :
- soit connaître à l'avance, pour chaque requête entrante, quel secret utiliser
  (ce qui suppose deux routes HTTP distinctes — une par endpoint Stripe créé
  côté Dashboard — puisque le corps de la requête ne s'auto-identifie pas
  avant vérification) ;
- soit essayer les deux secrets connus (`STRIPE_WEBHOOK_SECRET` et un nouveau
  `STRIPE_CONNECT_WEBHOOK_SECRET`) l'un après l'autre sur la même route
  `/webhooks/stripe`, si les deux endpoints Stripe pointent vers la même URL
  (ce que le code actuel laisse supposer, voir point 3 ci-dessous).

Cette absence de vérification de signature est un vrai gap de sécurité
(n'importe qui connaissant l'URL peut poster un faux event), mais **hors
scope de cette session** — non implémentée ici pour ne pas mélanger un
changement de sécurité non demandé avec ce diagnostic ; à traiter séparément
si Ilies le souhaite.

### 3. Le reste du traitement (lookupTerminalMapping, ConfirmKioskCardPayment) dépend-il d'hypothèses compte plateforme/connecté qui casseraient ?

**Non — vérifié fonction par fonction, aucune dépendance cassante.**

- `lookupTerminalMapping` : simple lecture Redis par `paymentIntentID`. Les
  IDs Stripe (`pi_...`) sont **globalement uniques sur toute la plateforme
  Stripe**, jamais réutilisés entre comptes connectés — aucun risque de
  collision entre un `pi_xxx` connect et un `pi_xxx` plateforme.
- `ConfirmKioskCardPayment`/`ConfirmKioskCardToCounterBrandStatus` : UPDATE SQL
  scopé `merchant_id`/`order_id` (résolus depuis le mapping Redis, pas depuis
  l'event Stripe) — aucune dépendance au compte Stripe.
- `recordTerminalPayment` : INSERT SQL uniquement, aucun appel API Stripe.
- Aucune de ces trois fonctions n'appelle l'API Stripe avec ou sans
  `SetStripeAccount` — contrairement à `HandleRetrieveFees`/`HandlePayoutPaid`,
  qui en ont réellement besoin (point 1). **Conclusion : le traitement métier
  du paiement Terminal fonctionnera correctement dès que l'event arrivera**,
  qu'il soit scopé plateforme ou connecté — le blocage n'est pas un problème
  de logique de traitement, uniquement un problème de **réception** de
  l'event (configuration Stripe Dashboard, hors du code).

### Point de vigilance découvert en creusant : le flux Checkout web (ScanNOrder) utilise aussi les direct charges

`internal/infrastructure/stripe/checkout.go:145` (`CreateCheckoutSession`) et
`:276` (version legacy) appellent également
`params.SetStripeAccount(*merchant.AccountID)` — le Checkout Session
ScanNOrder est donc, exactement comme le Terminal Kiosk, un **direct charge**
sur le compte connecté. Par le même raisonnement que ci-dessus, ses
`checkout.session.completed`/`charge.captured` seraient eux aussi des events
**Connect**, pas plateforme.

Or `charge.captured` fonctionne déjà en production pour ScanNOrder (le code
threadé `event.Account` pour `HandleRetrieveFees` existe précisément parce que
quelqu'un a déjà rencontré et résolu ce besoin). **Cela implique qu'un endpoint
Stripe scopé "Connected accounts" existe déjà** dans le Dashboard, pointant
vraisemblablement vers cette même URL `/webhooks/stripe` (le code ne fait
aucune distinction de route selon la scope). Deux lectures possibles, non
tranchables sans accès au Dashboard Stripe :

1. **Cet endpoint Connect existe et fonctionne pour `charge.captured`/
   `payout.paid`, mais sa liste d'"events to send" n'inclut simplement pas
   `payment_intent.succeeded`/`payment_intent.payment_failed`** — ces deux
   types n'ont probablement jamais été nécessaires avant l'ajout du Kiosk
   Terminal (ScanNOrder ne s'appuie que sur `checkout.session.completed`, pas
   sur `payment_intent.succeeded`, pour son propre flux). C'est l'explication
   la plus probable et la plus simple à corriger : **ajouter les deux types
   d'event manquants à l'endpoint Connect existant**, sans rien recréer.
2. **Il n'existe pas d'endpoint Connect du tout**, et `charge.captured`/
   `payout.paid` pour ScanNOrder n'ont en réalité jamais été vérifiés en
   production avec un vrai paiement (code écrit et jamais exercé en usage
   réel) — moins probable si le business tourne déjà sur ce flux, mais pas
   à exclure sans vérification.

**Ce qui manque pour trancher entre 1 et 2** : ouvrir Stripe Dashboard →
Developers → Webhooks, vérifier s'il existe deux endpoints (Your account /
Connected accounts) pointant vers `/webhooks/stripe`, et si l'endpoint
Connected accounts (s'il existe) a bien `payment_intent.succeeded` et
`payment_intent.payment_failed` dans sa liste d'event types sélectionnés. À
faire par Ilies — inaccessible depuis cet environnement de travail.

### Correction appliquée dans cette session

Le code n'avait besoin d'aucune adaptation fonctionnelle (point 3 confirme
qu'aucune logique métier ne dépend de la scope). Amélioration apportée par
cohérence et observabilité, dans la continuité des logs ajoutés dans le
diagnostic précédent :

- `internal/webhook/stripe/service.go` — `HandlePaymentIntentUpdated`,
  `HandlePaymentIntentSucceeded`, `HandlePaymentIntentFailed` reçoivent
  désormais `accountID` (= `event.Account`) en paramètre et le loguent
  systématiquement (`connect_account=<vide ou acct_...>`) aux côtés du
  `payment_intent_id`. Cela permettra, dès le prochain paiement Terminal
  après correction de la configuration Stripe Dashboard, de confirmer en un
  grep sur les logs Render que l'event arrive bien avec `connect_account`
  rempli — sans dépendre de l'accès au Dashboard Stripe pour le vérifier.
- Pas de changement sur `HandleRetrieveFees`/`HandlePayoutPaid` (déjà
  corrects) ni sur `lookupTerminalMapping`/`ConfirmKioskCardPayment` (déjà
  indépendants de la scope, voir point 3).

`go build ./...` et `go vet ./internal/webhook/stripe/... ./internal/infrastructure/stripe/...`
passent sans erreur après ce changement.

### Résumé actionnable pour Ilies (hors code, côté Stripe Dashboard)

1. Vérifier dans Stripe Dashboard → Developers → Webhooks s'il existe un
   endpoint scopé **"Connected accounts"** pointant vers l'URL de production
   de `/webhooks/stripe`.
2. S'il existe : ajouter `payment_intent.succeeded` et
   `payment_intent.payment_failed` à sa liste d'event types (probablement la
   correction complète, la plus simple).
3. S'il n'existe pas : le créer, scope "Connected accounts", en sélectionnant
   au minimum `payment_intent.succeeded`, `payment_intent.payment_failed`,
   `charge.captured`, `payout.paid` (les quatre déjà consommés par du code qui
   a besoin de la scope connect) — et noter le nouveau secret de signature
   généré pour une future implémentation de `VerifySignature` (point 2).
4. Une fois corrigé, rejouer un paiement Terminal et grep les logs Render sur
   `connect_account=` pour confirmer.

---

## Trace complète paiement Terminal — order 33348 (`pi_3Tvl6HISGuDm6FEV2xelLX1w`, merchant 2)

> Audit du 2026-07-22. Contexte : `payment_intent.succeeded` reçu et confirmé
> côté Stripe, HTTP 200 renvoyé par le webhook — donc, à la différence du
> diagnostic du 2026-07-21 (`pi_3TvjE1ISGuDm6FEV2OM6JgTE`), l'hypothèse
> "event Connect jamais reçu" ne s'applique **pas** ici : l'event est bien
> arrivé et traité sans erreur. Reste à expliquer pourquoi la transition
> `brand_status` attendue n'a, semble-t-il, pas produit l'effet escompté pour
> ce paiement précis. Toujours MySQL (`DB_DIALECT` non Postgres pour cette
> partie) — aucune hypothèse de cast Postgres reprise ici.

### Étape 0 : commande de vérification proposée

Aucun accès DB direct dans cette session. Il existe un endpoint back-office
existant, **non Kiosk**, qui expose `brand_status` sans rien modifier :
`GET /orders/{order_id}`, protégé par `authMiddleware` (token utilisateur
back-office classique, pas un token Kiosk) — voir
[cmd/api/routes.go:955-965](cmd/api/routes.go#L955-L965) et
`OrdersHandler.GetOrder` ([internal/modules/orders/handler.go:47](internal/modules/orders/handler.go#L47)),
qui délègue à `OrdersService.GetOrder` ([internal/modules/orders/service.go:265](internal/modules/orders/service.go#L265)) —
celui-ci lit `merchant_id` **depuis le token** (`middleware.UserFromContext`),
donc aucun risque de fuite cross-tenant : il suffit d'un utilisateur
back-office du merchant 2.

Commande à exécuter en premier, avant toute autre investigation :

```bash
BASE_URL="https://<host-staging-ou-prod>"
USER_TOKEN="<token d'un utilisateur back-office du merchant 2>"

curl -s "$BASE_URL/orders/33348" -H "Authorization: Bearer $USER_TOKEN" | jq .
```

Champs à lire dans la réponse (`models.Order`,
[internal/models/orders_model.go:204](internal/models/orders_model.go#L204)) :
`brand_status` (valeur attendue si le paiement a bien été confirmé :
`"PENDING"` ; si toujours `"PENDING_CARD_PAYMENT"`, la transition n'a
effectivement pas eu lieu), `merchant_approval` (doit être `"ACCEPTED"` dans
tous les cas depuis l'homogénéisation), `isPaid`. Si un doute de double
encaissement existe (voir cause identifiée ci-dessous), croiser avec
`GET /orders/33348/payments` ([routes.go:967](cmd/api/routes.go#L967)) pour
compter les lignes `payments` de cette commande (`mop='CB'`,
`payment_intent_id`).

### Étape 1 : conformité de la création — RAS, rien d'anormal trouvé

1. **order_id** : `string` de bout en bout, jamais un entier Go — cohérent
   avec le schéma (`orders.order_id` est un entier auto-incrémenté côté SQL,
   mais tout le code applicatif le manipule comme une chaîne, ex.
   `models.Order.OrderID string`,
   [internal/models/orders_model.go:204](internal/models/orders_model.go#L204)).
   `CreateOrder` reçoit `newOrder.OrderID` (string) et le passe tel quel à
   `CreateTerminalPaymentIntent(ctx, kiosk, orderID string, amountCents)`
   ([internal/modules/kiosk/service.go:1762](internal/modules/kiosk/service.go#L1762)).
   Aucune conversion `strconv`/`fmt.Sprintf` intermédiaire susceptible
   d'introduire un écart (ex. `"33348"` vs `"033348"` ou un float) : c'est la
   même valeur `string` du début à la fin.
2. **Metadata Stripe** : `CreateTerminalPaymentIntent`
   ([internal/infrastructure/stripe/terminal.go:146-150](internal/infrastructure/stripe/terminal.go#L146-L150))
   écrit `Metadata: map[string]string{"order_id": orderID, "merchant_id":
   merchantID, "channel": "kiosk"}` — `orderID`/`merchantID` sont les mêmes
   variables `string` reçues en paramètre, aucune transformation.
3. **Clé Redis + valeur stockée** : `storeMapping`
   ([internal/infrastructure/stripe/terminal.go:224-241](internal/infrastructure/stripe/terminal.go#L224-L241))
   écrit `terminal_pi:{paymentIntentID}` →
   `{"order_id":"33348","merchant_id":"2"}` (JSON de
   `TerminalPaymentMapping{OrderID: orderID, MerchantID: merchantID}`), et le
   mapping inverse `terminal_order_pi:2:33348` → `pi_...`. La fonction qui
   construit la clé (`TerminalPaymentIntentKey`,
   [terminal.go:52-54](internal/infrastructure/stripe/terminal.go#L52-L54))
   est **exportée et réutilisée telle quelle par le webhook** (jamais
   redéfinie/dupliquée côté `internal/webhook/stripe`) : aucun risque de
   divergence de format de clé entre écriture et lecture, par construction.
4. **Retour de `Set()` vérifié** : oui, déjà corrigé lors de la session du
   2026-07-21 — `storeMapping` logue désormais un warning explicite
   (`pi=...order=...merchant=...`) si l'un des deux `Set` Redis échoue, et un
   info sur succès ([terminal.go:236-240](internal/infrastructure/stripe/terminal.go#L236-L240)).
   Ce n'est donc plus un angle mort : si l'écriture a échoué pour ce
   paiement, ce sera visible dans les logs Render au moment de la création du
   PaymentIntent (avant même la présentation de la carte).

**Conclusion étape 1** : aucune non-conformité de code trouvée. Si le mapping
n'a pas été écrit correctement pour ce paiement, ce sera visible dans les
logs (voir grep recommandé plus bas), pas dans le code.

### Étape 2 : conformité du traitement webhook

1. **Placement du log `connect_account`** : tout en haut de
   `HandlePaymentIntentSucceeded`, juste après l'`unmarshal`, **avant** tout
   appel/condition qui pourrait retourner plus tôt
   ([internal/webhook/stripe/service.go:355-368](internal/webhook/stripe/service.go#L355-L368)) :
   ```go
   func (s *StripeWebhookService) HandlePaymentIntentSucceeded(ctx context.Context, data json.RawMessage, accountID string) error {
       var pi stripe.PaymentIntent
       if err := json.Unmarshal(data, &pi); err != nil { return fmt.Errorf(...) }
       logger.FromContext(ctx).Info("[stripe webhook] payment_intent.succeeded pi=" + pi.ID + " connect_account=" + accountID)
       if handled, err := s.handleTerminalPaymentSucceeded(ctx, &pi); handled || err != nil { return err }
       return s.repo.UpdatePaymentIntentStatus(ctx, pi.ID, "CAPTURED")
   }
   ```
   Ce log s'exécute inconditionnellement pour **tout** `payment_intent.succeeded`
   reçu (Kiosk ou Checkout web) — pas de `return` silencieux avant.
2. **Source de l'order_id** : exclusivement le **mapping Redis**
   (`lookupTerminalMapping`), jamais `event.Data.Object`/metadata directement
   — choix documenté (plus fiable que reparser
   `payment_method_details.type`, non expansé sur l'objet reçu). Un seul cas
   où les deux sources pourraient diverger : **le mapping Redis absent ou
   expiré alors que la metadata Stripe, elle, existe toujours** (metadata
   Stripe n'a pas de TTL, le mapping Redis en a un — 1h). Dans ce cas précis,
   le code **ne retombe pas** sur la metadata Stripe en secours — c'est un
   choix de conception (pas un bug de faute de frappe), mais c'est le seul
   point de la chaîne où une divergence order_id metadata vs mapping aurait
   un effet (silencieux, voir plus bas).
3. **Exactitude de la clé Redis interrogée** : `lookupTerminalMapping`
   ([internal/webhook/stripe/service.go:492-515](internal/webhook/stripe/service.go#L492-L515))
   appelle `stripeclient.TerminalPaymentIntentKey(paymentIntentID)` — **la
   même fonction exportée** que celle utilisée à l'écriture (étape 1, point
   3). Aucune reconstruction manuelle de la chaîne `"terminal_pi:" + id` des
   deux côtés : structurellement impossible d'avoir un piège de préfixe/casse
   entre écriture et lecture.
4. **Fonction et clause WHERE de la transition** : `ConfirmKioskCardPayment`
   ([internal/webhook/stripe/repository.go:181-192](internal/webhook/stripe/repository.go#L181-L192)) :
   ```sql
   UPDATE orders SET brand_status = 'PENDING', last_update = UTC_TIMESTAMP()
   WHERE order_id = ? AND merchant_id = ? AND brand_status = 'PENDING_CARD_PAYMENT'
   ```
   avec `orderID="33348"` (string) lié à `order_id` (colonne entière) : MySQL
   coerce implicitement une chaîne numérique dans une comparaison avec une
   colonne `INT`, comportement standard, aucun risque de non-match pour cette
   raison. `merchantID="2"` contre `merchant_id VARCHAR(64)` : direct.
5. **Rows affected vérifié et logué** : oui —
   ```go
   confirmed, err := s.repo.ConfirmKioskCardPayment(ctx, mapping.MerchantID, mapping.OrderID)
   if !confirmed {
       log.Warn("[stripe terminal] ConfirmKioskCardPayment: no row matched (order not in PENDING_CARD_PAYMENT) for pi=" + pi.ID + " order=" + mapping.OrderID + " merchant=" + mapping.MerchantID)
   }
   ```
   ([internal/webhook/stripe/service.go:411-422](internal/webhook/stripe/service.go#L411-L422)).
   **Point important** : ce cas n'est qu'un `Warn`, jamais une erreur — le
   webhook retourne `handled=true, err=nil`, donc HTTP 200 dans tous les cas
   où le mapping est trouvé, que la transition ait réellement eu lieu ou non.
   C'est exactement le comportement rapporté (200 renvoyé) et c'est
   **volontaire** (rejouer un paiement déjà confirmé ne doit pas faire
   échouer la delivery Stripe) — mais cela signifie que **"HTTP 200" ne
   prouve pas que la transition a eu lieu**, seulement que le webhook n'a
   rencontré aucune erreur technique.
6. **defer/recover avalant une panique en 200** : vérifié, absent. Aucun
   `middleware.Recoverer` (chi) n'est monté nulle part dans
   [cmd/api/routes.go](cmd/api/routes.go) (grep exhaustif sur `r.Use(` :
   uniquement CORS, logging, `authMiddleware`, rate-limit, `KioskAuth` —
   jamais de recoverer), et `middleware.LoggingMiddleware` ne contient pas de
   `recover()`. `HandleWebhook`
   ([internal/webhook/stripe/http_handler.go:17-37](internal/webhook/stripe/http_handler.go#L17-L37))
   renvoie 500 dès que `ProcessEvent` retourne une erreur non nil ; une
   panique non recouverte planterait la goroutine de la requête (le
   `net/http` standard log "http: panic serving" et ferme la connexion sans
   réponse) — Stripe verrait un échec de delivery (timeout/connexion
   réinitialisée), pas un 200. **Cette piste est donc écartée avec un niveau
   de confiance élevé** : un 200 confirmé signifie qu'aucune panique n'a eu
   lieu sur ce chemin.

**Conclusion étape 2** : le code du webhook lui-même est correct et cohérent
avec la description du symptôme (200 + aucune erreur ne prouve pas la
transition). Le point 5 ouvre la seule porte réelle : un `handled=true` avec
`confirmed=false` produit exactement "webhook reçu, 200 renvoyé, brand_status
inchangé, aucune erreur visible" — voir cause identifiée ci-dessous pour le
scénario concret qui déclenche ce cas.

### Étape 3 : conformité côté Flutter (repo `wello-kiosk`)

1. **Type/forme de `order_id` envoyé** : cohérent de bout en bout. `OrderController.currentOrder`
   (assigné une seule fois après création,
   [lib/presentation/controllers/order_controller.dart:121](../../wello-kiosk/lib/presentation/controllers/order_controller.dart#L121))
   porte `order.orderId` (`String` Dart) : c'est cette même valeur qui est
   envoyée par `ApiService.createTerminalPaymentIntent`
   (`data: {'order_id': orderId, 'amount_cents': amountCents}`,
   [lib/data/services/api_service.dart:474](../../wello-kiosk/lib/data/services/api_service.dart#L474))
   — jamais recalculée, jamais un `int`. Pas de désynchronisation possible à
   ce niveau : `TerminalScreen` ne connaît qu'un seul `_order.currentOrder`,
   jamais une autre commande.
2. **Double création de commande (double-tap)** : `OrderSummaryScreen._submit`
   est protégé par un flag local `_isSubmitting`
   (`if (_isSubmitting) return;`,
   [lib/presentation/screens/order_summary_screen.dart:41-46](../../wello-kiosk/lib/presentation/screens/order_summary_screen.dart#L41-L46)),
   et la soumission est déclenchée automatiquement par un countdown (pas par
   un tap direct répétable) — pas de chemin de double-tap évident sur la
   création de commande elle-même. **Point signalé néanmoins** :
   `CartController.generateIdempotencyKey`
   ([lib/presentation/controllers/cart_controller.dart:310-313](../../wello-kiosk/lib/presentation/controllers/cart_controller.dart#L310-L313))
   génère `"{deviceId}:{timestamp_ms}"` **à chaque appel** — donc une
   **nouvelle** clé à chaque invocation de `createOrder`. La clé
   d'idempotence backend ne protège donc que contre un retry réseau bas
   niveau (même clé déjà calculée rejouée), pas contre deux appels distincts
   de la méthode Dart `createOrder` (qui recalculerait une clé différente à
   chaque fois). Dans ce repo précis, `_isSubmitting` rend ce deuxième
   scénario peu probable pour `OrderSummaryScreen`, mais ce n'est une
   protection que côté widget, pas une garantie serveur.
3. **`order_id` affiché cohérent avec celui utilisé pour le PaymentIntent** :
   oui, confirmé (point 1) — un seul `currentOrder`, jamais recréé pendant le
   flux `TerminalScreen`.

**Constat additionnel, hors des 3 questions posées mais découvert en traçant
le flux d'annulation/retry** : voir section suivante — c'est la piste la plus
concrète trouvée dans cette session.

### Cause identifiée — asymétrie annulation manuelle / timeout, PaymentIntent jamais annulé côté serveur en cas de timeout

**Pas une certitude absolue pour *ce* pi_id précis (aucun accès aux logs
Render/Stripe Dashboard dans cette session pour confirmer que ce scénario
s'est produit), mais c'est un bug réel, prouvé par citation de code, qui
produit exactement le symptôme rapporté (paiement confirmé côté Stripe, 200
renvoyé, transition non visible pour ce pi_id).**

`TerminalScreen` gère deux façons de sortir de l'attente de carte
(`waitingForCard`), avec un traitement **différent et incohérent** du
PaymentIntent actif :

- **Annulation manuelle** (`_onCancel`,
  [lib/presentation/screens/terminal_screen.dart:195-216](../../wello-kiosk/lib/presentation/screens/terminal_screen.dart#L195-L216)) :
  ```dart
  Future<void> _onCancel() async {
    _cancelTimeout();
    await _terminal.cancelCollectPaymentMethod();
    final piId = _paymentIntentId;
    if (piId != null) {
      await _order.cancelTerminalPaymentIntent(piId);   // <-- annule côté SERVEUR
      _paymentIntentId = null;
    }
    ...
  }
  ```
  Annule bien le PaymentIntent côté serveur (`CancelTerminalPaymentIntent` →
  Stripe `PaymentIntents.Cancel` + suppression des deux mappings Redis) et
  vide `_paymentIntentId`.

- **Timeout automatique** (`_armTimeout`,
  [lib/presentation/screens/terminal_screen.dart:180-186](../../wello-kiosk/lib/presentation/screens/terminal_screen.dart#L180-L186)) :
  ```dart
  void _armTimeout() {
    _timeoutTimer = Timer(Duration(seconds: _payment.timeoutSeconds), () async {
      await _terminal.cancelCollectPaymentMethod();   // <-- SDK local uniquement
      if (!mounted) return;
      _payment.markTimeout();
    });
  }
  ```
  **N'appelle jamais `_order.cancelTerminalPaymentIntent(...)`** — le
  PaymentIntent Stripe **reste actif côté serveur**, le mapping Redis
  `terminal_pi:{id}` reste valide (TTL 1h), et `_paymentIntentId` n'est
  **jamais remis à `null`** dans ce chemin (contrairement à `_onCancel`).
  `_terminal.cancelCollectPaymentMethod()` n'agit que sur le SDK local
  (le lecteur physique arrête d'attendre une carte côté app) — cela n'annule
  rien côté API Stripe.

Ensuite, `_onRetry`
([lib/presentation/screens/terminal_screen.dart:248-253](../../wello-kiosk/lib/presentation/screens/terminal_screen.dart#L248-L253)) :
```dart
void _onRetry() {
  _payment.retry();
  if (_payment.state == PaymentState.waitingForCard) {
    _collectAndConfirm();   // <-- crée un NOUVEAU PaymentIntent, sans avoir annulé l'ancien
  }
}
```
`_collectAndConfirm` ([terminal_screen.dart:99-118](../../wello-kiosk/lib/presentation/screens/terminal_screen.dart#L99-L118))
appelle `_order.createTerminalPaymentIntent()` qui crée un **second**
PaymentIntent pour la **même commande** (toujours `PENDING_CARD_PAYMENT` à ce
stade, `isKioskCardPending` le permet,
[internal/modules/kiosk/service.go:1771](internal/modules/kiosk/service.go#L1771)) —
rien n'empêche le serveur de le faire, une commande carte peut recevoir
plusieurs tentatives de PaymentIntent tant qu'elle est en attente. Le premier
PaymentIntent (celui du timeout) n'a jamais été annulé : **deux PaymentIntents
Stripe distincts, tous deux avec un mapping Redis actif pointant vers order
33348, coexistent désormais.**

**Scénario concret reproduisant exactement le symptôme** : le client présente
sa carte une première fois juste au moment où le timeout se déclenche (délai
serré, lecteur lent, hésitation) — le SDK local annule sa propre attente,
l'écran passe en "Délai dépassé", mais **le PaymentIntent avait déjà commencé
à être traité côté Stripe** (la présentation de carte a eu lieu avant/pendant
l'annulation locale). Le client ou le staff clique "Réessayer" : un second
PaymentIntent est créé et présenté, qui réussit également (ou échoue, peu
importe). Si c'est le **premier** PaymentIntent (celui du timeout,
potentiellement `pi_3Tvl6HISGuDm6FEV2xelLX1w`) qui finit par recevoir son
`payment_intent.succeeded` **après** que le second ait déjà fait transiter
`brand_status` de `PENDING_CARD_PAYMENT` à `PENDING` (guard de
`ConfirmKioskCardPayment` déjà consommé) — ou l'inverse, selon l'ordre
d'arrivée des deux webhooks — celui qui arrive **en second** ne trouvera plus
`brand_status = 'PENDING_CARD_PAYMENT'` : `confirmed=false`, seul un `Warn`
est logué, HTTP 200 renvoyé. **C'est exactement le symptôme rapporté**, sans
qu'aucune requête SQL, aucun mapping Redis, ni aucune ligne de code du
webhook ne soit en tort — le garde est un garde **par commande**
(`brand_status`), pas un garde **par `payment_intent_id`** : il ne peut pas
distinguer "ce paiement précis a-t-il déjà été traité" de "un autre paiement
pour cette même commande a déjà résolu son statut".

Le même mécanisme s'applique, avec la même conséquence silencieuse, si
l'utilisateur a choisi **"Payer en caisse"** après le timeout au lieu de
"Réessayer" : `SwitchToCounterPayment`
([internal/modules/kiosk/service.go:1813-1840](internal/modules/kiosk/service.go#L1813-L1840))
tente d'annuler le PaymentIntent actif via le mapping inverse
(`CancelActivePaymentIntentForOrder`, best-effort, erreur seulement loguée en
`Warn`), **puis fait transiter `brand_status` vers `PENDING` immédiatement,
que cette annulation Stripe ait réussi ou non** :
```go
if s.terminal != nil {
    if err := s.terminal.CancelActivePaymentIntentForOrder(ctx, kiosk.MerchantID, orderID); err != nil {
        logger.FromContext(ctx).Warn("kiosk switch to counter: cancel active payment intent failed: " + err.Error())
    }
}
if _, err := s.repo.ConfirmKioskCardToCounterBrandStatus(ctx, kiosk.MerchantID, orderID); err != nil {
    return nil, err
}
```
Si l'annulation Stripe échoue parce que la carte a déjà été traitée côté
Stripe au moment de l'appel (le PaymentIntent n'est alors plus dans un état
annulable), la commande est malgré tout basculée en paiement caisse — et le
paiement carte, une fois confirmé plus tard par Stripe, ne trouvera plus le
`brand_status` attendu. **Risque financier associé, à vérifier en priorité** :
si ce chemin s'est produit pour la commande 33348, le client a pu être
**débité deux fois** (une fois électroniquement via le Terminal, une fois en
caisse) sans qu'aucune alerte ne soit levée côté serveur — seul un `Warn`
générique indiscernable d'un simple replay Stripe.

**Ce que confirme (et ne confirme pas) cette hypothèse** :
- Explique intégralement webhook reçu + 200 + absence de transition visible,
  sans supposer aucune anomalie de code ailleurs dans la chaîne (étapes 1-2
  entièrement propres).
- N'explique PAS, à elle seule, que `brand_status` soit **resté bloqué** sur
  `PENDING_CARD_PAYMENT` indéfiniment si un premier PaymentIntent a bien
  réussi entre-temps (dans ce cas la commande serait déjà `PENDING`, juste
  avec un risque de double-paiement, pas un blocage). Si `GET /orders/33348`
  (étape 0) montre `brand_status` toujours `PENDING_CARD_PAYMENT`, alors soit
  aucun des deux PaymentIntents (timeout + retry) n'a de guard consommé
  (aucun n'a encore réussi selon Stripe — à vérifier côté Dashboard si
  `pi_3Tvl6HISGuDm6FEV2xelLX1w` est bien `succeeded` et pas seulement
  `processing`), soit le mapping Redis pour CE `pi_id` spécifique n'a jamais
  été écrit/trouvé (retour aux hypothèses du diagnostic du 2026-07-21, déjà
  instrumentées).

### Ce qui reste à vérifier manuellement (aucun accès DB/logs/Stripe Dashboard dans cette session)

1. **Étape 0 en premier** : `GET /orders/33348` (commande ci-dessus) —
   `brand_status` actuel. C'est le fait qui départage la plupart des
   hypothèses ci-dessus.
2. **Stripe Dashboard → Payments → rechercher `pi_3Tvl6HISGuDm6FEV2xelLX1w`** :
   confirmer le statut exact (`succeeded` vs autre), la metadata
   (`order_id`/`merchant_id`/`channel` doivent lire `33348`/`2`/`kiosk`), et
   surtout **rechercher s'il existe un ou plusieurs AUTRES PaymentIntents
   avec la même metadata `order_id=33348`** — c'est la vérification directe
   de l'hypothèse "deux PaymentIntents pour la même commande" ci-dessus.
3. **Logs Render, grep sur `order=33348` ET séparément sur
   `pi=pi_3Tvl6HISGuDm6FEV2xelLX1w`** (tous les logs `[stripe terminal]` et
   `[stripe webhook]` portent l'un ou l'autre, ou les deux) :
   - `storeMapping: stored pi=... -> order=33348` — combien
     d'occurrences ? Une seule = un seul PaymentIntent créé, l'hypothèse
     retry est écartée. Plusieurs = confirme le double-PaymentIntent.
   - `payment_intent.succeeded pi=pi_3Tvl6HISGuDm6FEV2xelLX1w connect_account=...` —
     confirme la réception et la scope Connect.
   - `lookupTerminalMapping: found pi=... -> order=33348` ou au contraire
     `no redis key`/`unreadable JSON`/`empty order_id` — détermine si le
     mapping était bien présent à ce moment précis.
   - `ConfirmKioskCardPayment: no row matched` — présence de ce warning pour
     ce `pi_id` confirme directement la cause identifiée ci-dessus (guard déjà
     consommé par un autre événement).
4. **`GET /orders/33348/payments`** : compter les lignes `payments`
   (`mop='CB'`) — plus d'une ligne carte confirme un double encaissement réel
   à rembourser.
5. **Historique du client sur cette commande** : un retry / un
   switch-to-counter a-t-il été déclenché avant que ce paiement n'aboutisse ?
   (visible côté logs via plusieurs occurrences `storeMapping` avec le même
   `order=33348`, ou une entrée `switch to counter` dans les logs applicatifs
   autour de cet horodatage).

### Instrumentation

Aucune instrumentation backend supplémentaire n'a été nécessaire : les logs
ajoutés lors de la session du 2026-07-21 (`storeMapping`,
`lookupTerminalMapping`, `ConfirmKioskCardPayment`) couvrent déjà exactement
les points de vérification listés ci-dessus (point 3) — un grep sur
`order=33348` suffit à reconstituer toute la chronologie (créations de
PaymentIntent + tentatives de confirmation webhook) sans changement de code.
Un gap d'instrumentation existe côté **Flutter** (aucun log au déclenchement
du timeout dans `_armTimeout`, ni sur `_onRetry`) — non corrigé dans cette
session : ce repo (`wello-kiosk`) est hors du périmètre `go build`/`go vet` de
ce dépôt, et la règle "ne pas corriger avant certitude" s'applique d'autant
plus à une modification cross-repo ; à ajouter dans une session dédiée
Flutter si la piste ci-dessus se confirme via les logs Render.

Aucun fichier Go n'a été modifié dans cette session (audit de code seul) —
`go build ./...` / `go vet ./...` non ré-exécutés pour cette raison (rien à
valider qui n'ait pas déjà été validé aux sessions précédentes).

---

## Retrait de Redis du mapping order_id/payment_intent_id (Terminal Kiosk)

> Session du 2026-07-22 (suite du diagnostic ci-dessus). Objectif : remplacer
> le mapping Redis `terminal_pi:{id}` / `terminal_order_pi:{merchant}:{order}`
> par la table `stripe_payments` déjà utilisée par ScanNOrder, pour éliminer
> la classe de bug diagnostiquée dans toute la section précédente (écriture
> Redis silencieusement perdue, mapping absent/expiré au moment du webhook).

### Étape 0 — audit de l'insertion existante [CONSTAT — corrige une hypothèse fausse de la demande initiale]

La demande partait de l'hypothèse que ScanNOrder insère une ligne
`stripe_payments` **à la création de la Checkout Session**
(`stripeclient.CreateCheckoutSession`, `checkout.go`), par analogie avec ce
que devrait faire le Terminal. **Cette hypothèse est fausse, vérifiée par
lecture directe** :

- `stripeclient.CreateCheckoutSession` (`internal/infrastructure/stripe/checkout.go:18`)
  ne fait qu'un appel API Stripe (`session.New`) et ne touche à aucune table.
  Aucune écriture DB n'a lieu à la création de la session.
- Son unique appelant, `scannorder.Service.CreateOrderSNO`
  (`internal/modules/scannorder/service.go:941`), enchaîne directement sur
  `return *newOrder, nil` après avoir reçu l'URL Stripe — pas d'insertion là
  non plus.
- **L'unique endroit de tout le repo qui insère réellement dans
  `stripe_payments` (hors code de test et hors fonction décommissionnée) est
  `OrdersLifeCycleRepository.AddPaymentAndReturnID`**
  (`internal/modules/order_life_cycle/repository.go:133`), déjà documenté
  dans "Incrément Terminal 2" ci-dessus (Audit 0.B). Cette fonction insère
  **une ligne `payments`** (chaînage fiscal NF525 : `previous_hash`/`hash`/
  `signature`) **et**, dans la foulée, une ligne `stripe_payments` liée par
  `payment_id` — les deux insertions sont couplées dans la même fonction,
  déclenchée uniquement quand un paiement est **réellement encaissé** :
  - ScanNOrder (Checkout web) : au webhook `checkout.session.completed`,
    via `HandleCheckoutSessionCompleted` → `CreatePaymentNoNotification`.
  - Kiosk Terminal (déjà implémenté, incrément précédent) : au webhook
    `payment_intent.succeeded`, via `recordTerminalPayment` →
    `CreatePaymentNoNotification`.
- Un second insert, `mysqlRepo.InsertStripePayment`
  (`internal/webhook/stripe/repository.go:117`), existe mais n'est **jamais
  appelé en production** — seulement exercé par
  `postgres_integration_test.go`, qui documente lui-même
  (`webhook/stripe/postgres_integration_test.go:131`) que cet insert est du
  code mort et échoue de toute façon sur `success_key NOT NULL` sans le
  correctif appliqué manuellement dans le test.

**Conséquence directe** : il n'existe, nulle part dans ce projet avant cette
session, de précédent "insérer une ligne `stripe_payments` avant que le
paiement soit confirmé". Réutiliser *littéralement* `AddPaymentAndReturnID`
pour écrire le mapping à la création du PaymentIntent Terminal (comme
demandé) est exclu : cette fonction insère aussi une ligne `payments` avec
chaînage de hash fiscal — l'appeler avant que Stripe ait confirmé quoi que ce
soit fabriquerait un enregistrement de paiement fiscal pour de l'argent non
reçu. Le design ci-dessous adapte l'intention de la demande (« un seul
endroit qui gère ce mapping pour tous les canaux », `stripe_payments` plutôt
qu'une colonne dédiée) sans dupliquer cette fonction précise.

**Champs de `stripe_payments`** (`docs/migration-postgres/04-schema-postgres-target.sql:3393`,
identique à la DDL MySQL source) : `order_id integer NOT NULL`, `payment_id
integer` **nullable**, `payment_intent_id varchar(200)`,
`payment_intent_status varchar(30) NOT NULL DEFAULT 'REQUIRES_CONFIRMATION'`,
`success_key varchar(100) NOT NULL` (sans défaut — toujours `''` explicite
dans ce projet, jamais le vrai "success key" applicatif), `checkout_session_id`/
`customer_email` nullable. La nullabilité de `payment_id` est ce qui rend
possible une ligne "mapping seul" avant tout encaissement.

**`UpdatePaymentIntentStatus`** (déjà existant,
`internal/webhook/stripe/repository.go:332`) fait un simple
`UPDATE stripe_payments SET payment_intent_status = ? WHERE payment_intent_id = ?`
— totalement indépendant de `payment_id` — réutilisable tel quel pour marquer
`'CANCELED'`/`'CAPTURED'`/`'FAILED'` sur une ligne pré-créée, sans
modification. Cette fonction vit dans `internal/webhook/stripe` (module
webhook), qui importe déjà `internal/infrastructure/stripe` (`stripeclient`,
pour `TerminalPaymentIntentKey`/etc.) — l'inverse (infra important webhook)
créerait un cycle d'import. Le nouveau store SQL décrit ci-dessous vit donc
directement dans `internal/infrastructure/stripe`, avec sa propre requête
`UPDATE stripe_payments SET payment_intent_status = ...` (une ligne dupliquée
avec `UpdatePaymentIntentStatus`, assumé plutôt que de risquer un cycle
d'import pour économiser une requête d'une ligne).

### Design retenu

1. **Écriture à la création** (`CreateTerminalPaymentIntent`, `terminal.go`) :
   nouvelle méthode `TerminalPaymentStore.CreateMapping(ctx, orderID,
   paymentIntentID)` — `INSERT INTO stripe_payments(order_id,
   payment_intent_id, success_key) VALUES (?, ?, '')`, `payment_id` omis
   (NULL), `payment_intent_status` omis (défaut DB `'REQUIRES_CONFIRMATION'`,
   cohérent avec toutes les autres insertions du projet qui ne renseignent
   jamais ce champ explicitement à la création).
   - **Changement de sévérité assumé** : contrairement à l'ancien
     `storeMapping` Redis (best-effort, erreur seulement loguée — c'est
     exactement la cause racine diagnostiquée plus haut dans ce document), un
     échec de cet INSERT fait maintenant échouer `CreateTerminalPaymentIntent`
     dans son ensemble (le PaymentIntent Stripe déjà créé est annulé en
     best-effort avant de remonter l'erreur). Le mapping vit désormais dans la
     même base transactionnelle que le reste de l'application (déjà une
     dépendance dure) : un échec d'écriture ici signale un problème sérieux
     (DB indisponible), pas une dégradation acceptable comme pouvait l'être un
     Redis flaky — le laisser passer silencieusement reproduirait exactement
     l'incident diagnostiqué.
2. **Lecture pour l'annulation** (`CancelActivePaymentIntentForOrder`) :
   `TerminalPaymentStore.GetActivePaymentIntentForOrder(ctx, merchantID,
   orderID)` — `SELECT sp.payment_intent_id FROM stripe_payments sp INNER
   JOIN orders o ON o.order_id = sp.order_id WHERE sp.order_id = ? AND
   o.merchant_id = ? AND sp.payment_intent_status NOT IN ('CANCELED',
   'FAILED', 'CAPTURED') ORDER BY sp.id DESC LIMIT 1`. La jointure `orders`
   remplace la vérification d'appartenance merchant que portait le mapping
   Redis (`stripe_payments` n'a pas de colonne `merchant_id` propre tant que
   `payment_id` est NULL). `ORDER BY id DESC LIMIT 1` : le PaymentIntent le
   plus récent, au cas où plusieurs lignes existeraient pour la même commande
   (retry après timeout, cas documenté dans le diagnostic de la commande
   33348 ci-dessus).
3. **Vérification d'appartenance pour l'annulation directe**
   (`CancelTerminalPaymentIntent`, appelée aussi par l'endpoint
   `POST /kiosk/terminal/payment-intent/{id}/cancel`) :
   `TerminalPaymentStore.GetMerchantIDForPaymentIntent(ctx, paymentIntentID)`
   — même jointure, remplace le test `mapping.MerchantID != merchantID` fait
   auparavant contre le mapping Redis direct.
4. **Statut `'FAILED'` introduit** : `payment_intent.payment_failed`
   n'appelait auparavant aucune mise à jour de `stripe_payments` (seulement
   une notification best-effort). Ajout d'un appel à
   `UpdatePaymentIntentStatus(ctx, pi.ID, "FAILED")` pour que la ligne
   pré-créée sorte de l'ensemble "actif" (point 2) — cohérent avec les
   valeurs déjà existantes `'CAPTURED'`/`'CANCELED'`.
5. **Suppression de l'upsert de duplication côté `AddPaymentAndReturnID`** :
   quand le webhook `payment_intent.succeeded` (Terminal) appelle
   `recordTerminalPayment` → `AddPaymentAndReturnID`, cette dernière
   trouverait, pour un paiement Terminal, une ligne `stripe_payments`
   **déjà présente** (celle créée au point 1, `payment_id` encore NULL) — un
   second `INSERT` créerait un doublon pour le même `payment_intent_id`.
   `AddPaymentAndReturnID` complète maintenant cette ligne par `UPDATE
   stripe_payments SET payment_id = ? WHERE payment_intent_id = ? AND
   payment_id IS NULL`, et ne retombe sur l'`INSERT` existant (comportement
   strictement inchangé) que si cet `UPDATE` n'affecte aucune ligne — ce qui
   est toujours le cas pour le Checkout web (qui ne pré-crée jamais de ligne).
   Couverture : `internal/modules/order_life_cycle/postgres_integration_test.go`
   exerçait déjà ce chemin sans ligne pré-existante (paiement `MOP=CB` avec
   `PaymentIntentID`, `nStripe==1` attendu) — cas non affecté, complété par un
   nouveau cas avec ligne pré-existante.
   - **Bug adjacent corrigé au passage** : l'erreur de cet `UPDATE`/`INSERT`
     était auparavant silencieusement écrasée par le `UPDATE ... isPaid` qui
     suit (même variable `err` réutilisée, jamais vérifiée entre les deux) —
     exactement la classe de bug reprochée à Redis. Cette branche retourne
     désormais l'erreur immédiatement en cas d'échec.
6. **Sens PaymentIntent → order (webhook)** : inchangé dans son principe déjà
   décidé (lire `pi.Metadata`), mais **jamais réellement implémenté** avant
   cette session — le code lisait le mapping Redis (`lookupTerminalMapping`),
   pas la metadata, malgré ce que suggérait la demande initiale. `pi.Metadata`
   porte déjà `order_id`/`merchant_id`/`channel` depuis la création
   (`CreateTerminalPaymentIntent`, inchangé) : `handleTerminalPaymentSucceeded`
   et `HandlePaymentIntentFailed` lisent désormais directement
   `pi.Metadata["channel"] == "kiosk"` pour se reconnaître comme un paiement
   Terminal, `pi.Metadata["order_id"]`/`["merchant_id"]` pour résoudre la
   commande — **aucune requête DB/Redis nécessaire dans ce sens**, la
   metadata Stripe n'a pas de TTL contrairement au mapping Redis qu'elle
   remplace.

### Suppression du code Redis

Supprimés de `internal/infrastructure/stripe/terminal.go` : `TerminalMappingStore`
(interface), `TerminalPaymentMapping` (struct), `TerminalPaymentIntentKey`/
`TerminalOrderKey` (constructeurs de clé, exportés et jusqu'ici réutilisés par
le webhook), `terminalPIKeyPrefix`/`terminalOrderKeyPrefix`/`terminalMappingTTL`,
`storeMapping`/`getMapping`/`deleteMapping`. Supprimés de
`internal/webhook/stripe/service.go` : `lookupTerminalMapping`, les deux
`s.redis.Delete(ctx, stripeclient.Terminal...Key(...))`, l'import `stripeclient`
(devenu inutilisé après ce retrait — vérifié, aucune autre référence dans ce
fichier). **Conservé** : `s.redis` (champ, toujours utilisé par
`HandleCheckoutSessionCompleted` pour l'invalidation du cache de commande,
sans rapport avec le mapping Terminal) et le paramètre `redis` du
constructeur `NewStripeWebhookService` (inchangé).

### Vérifications

`go build ./...` et `go vet ./...` clean (seul warning restant :
`cmd/api/routes.go:431` copie de lock dans `authModule.NewAuthHandler`,
pré-existant, sans rapport). `go test ./internal/modules/kiosk/...
./internal/webhook/stripe/... ./internal/modules/order_life_cycle/...
./internal/infrastructure/stripe/...` passent.

**Différence par rapport aux sessions précédentes : un Postgres de dev était
disponible dans ce sandbox** (conteneur Docker `welloresto-postgres-dev`,
port 5433, déjà démarré) — les tests `postgres_integration` ont donc pu être
**réellement exécutés**, pas seulement compilés :

```bash
POSTGRES_URL="postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev" \
  go test -tags postgres_integration ./internal/modules/kiosk/... \
  ./internal/modules/order_life_cycle/... ./internal/infrastructure/stripe/... \
  ./internal/webhook/stripe/... ./internal/modules/scannorder/...
```

Tout passe, y compris les deux nouveaux/étendus :
- `TestTerminalPaymentStore_Postgres` (nouveau,
  `internal/infrastructure/stripe/postgres_integration_test.go`) : `CreateMapping`
  (ligne créée avec `payment_id` NULL et statut par défaut
  `REQUIRES_CONFIRMATION`), `GetActivePaymentIntentForOrder` (trouvé pour le bon
  merchant, absent pour un autre merchant — vérifie la jointure `orders`),
  `GetMerchantIDForPaymentIntent`, `MarkPaymentIntentStatus` (sort bien de
  l'ensemble "actif" après passage à `CANCELED`).
- `TestOrderLifeCycleRepository_Postgres` (étendu) : nouveau cas — une ligne
  `stripe_payments` pré-créée (simulant `CreateMapping`) est complétée par
  `AddPaymentAndReturnID` (même `payment_intent_id`) sans être dupliquée
  (`COUNT(*) = 1`, `payment_id` de la ligne unique == celui retourné).

**Note de méthode** : ne pas exporter `DB_DIALECT=postgres` manuellement dans
le shell avant de lancer ces tests — `pgtest.Open` le fait déjà via
`t.Setenv` (scope correct, restauré après chaque test). L'exporter
globalement fait basculer aussi les tests unitaires `sqlmock` d'autres
paquets (qui attendent des requêtes `?` MySQL) vers le rebind Postgres,
produisant des échecs qui n'ont rien à voir avec le code testé — piège
rencontré et vérifié dans cette session (`go test -tags postgres_integration
./...` avec `DB_DIALECT` exporté à la main fait échouer des paquets sans
rapport ; sans cet export, seuls les mêmes paquets déjà en échec avant cette
session le restent : `bookingcomm`, `cash_registers`, `planning/employees`,
`planning/leave`, `planning/swaps` — confirmé identique via `git stash`, rien
à voir avec ce changement).

### Fichiers modifiés

- `internal/infrastructure/stripe/terminal.go` — retrait complet du mapping
  Redis (`TerminalMappingStore`, `TerminalPaymentMapping`,
  `TerminalPaymentIntentKey`/`TerminalOrderKey`, `storeMapping`/`getMapping`/
  `deleteMapping`), nouveau `TerminalPaymentStore` (interface + implémentation
  SQL `terminalPaymentStore`), `CreateTerminalPaymentIntent`/
  `CancelTerminalPaymentIntent`/`CancelActivePaymentIntentForOrder` réécrites
  pour l'utiliser.
- `internal/modules/order_life_cycle/repository.go` —
  `AddPaymentAndReturnID` : upsert (`UPDATE` puis `INSERT` de repli) sur
  `stripe_payments` au lieu d'un `INSERT` systématique ; bug adjacent corrigé
  (erreur de cette étape désormais retournée immédiatement, plus jamais
  écrasée par le refresh `isPaid` qui suit).
- `internal/webhook/stripe/service.go` — `HandlePaymentIntentSucceeded`/
  `HandlePaymentIntentFailed`/`handleTerminalPaymentSucceeded`/
  `recordTerminalPayment` : lecture de `pi.Metadata` au lieu du mapping Redis ;
  nouveau helper `kioskTerminalMetadata` ; `lookupTerminalMapping` supprimée ;
  import `stripeclient` retiré (devenu inutilisé) ; nouveau statut `'FAILED'`
  écrit sur `payment_intent.payment_failed` ; `'CAPTURED'` désormais
  effectivement écrit sur succès Terminal (ne l'était jamais avant, faute
  d'atteindre ce code par le chemin `handled=true`).
- `cmd/api/routes.go` — `NewTerminalService` reçoit
  `stripeInternalClient.NewTerminalPaymentStore(mysqlDB)` au lieu de
  `redisClient`.
- `internal/modules/kiosk/service.go` — commentaire de
  `CancelTerminalPaymentIntent` mis à jour (ne mentionne plus Redis).
- `internal/infrastructure/stripe/postgres_integration_test.go` (nouveau) —
  couverture de `terminalPaymentStore`.
- `internal/modules/order_life_cycle/postgres_integration_test.go` (étendu) —
  couverture du scénario upsert (ligne pré-créée, pas de doublon).

### Ce qui n'a PAS été touché (hors périmètre de cette tâche)

- Le format/la valeur de `pi.Metadata` à l'écriture (`CreateTerminalPaymentIntent`)
  était déjà correct depuis l'incrément précédent — non modifié.
- `ConfirmKioskCardPayment`/`ConfirmKioskCardToCounterBrandStatus` (transition
  `brand_status`) — inchangées, déjà correctes (diagnostiquées en détail dans
  la section précédente).
- Le diagnostic ouvert sur les webhooks Connect (endpoint "Connected accounts"
  à vérifier côté Dashboard Stripe) reste entièrement d'actualité — cette
  session ne change rien à la réception des events, seulement au traitement
  une fois reçus.

---

## Audit croisé Terminal Kiosk / ScanNOrder — filtre `pi.Metadata["channel"]`

> Session du 2026-07-22. Objectif : depuis que Terminal Kiosk et ScanNOrder
> (Checkout web) partagent le même mécanisme de lecture `pi.Metadata` sur
> `payment_intent.succeeded`/`payment_intent.payment_failed` (section
> "Retrait de Redis" ci-dessus), vérifier qu'aucun des deux flux ne produit
> d'effet de bord sur l'autre. Audit de code seul, aucune exécution nécessaire
> (le comportement en cause est déterministe et vérifiable par lecture).

### 1. Un PaymentIntent ScanNOrder porte-t-il une metadata `channel` ?

Non — vérifié par lecture directe de
`stripeclient.CreateCheckoutSession` (`internal/infrastructure/stripe/checkout.go:128-143`) :
`PaymentIntentData` ne renseigne que `ApplicationFeeAmount`/`CaptureMethod`,
aucun champ `Metadata`. Stripe ne copie **jamais** automatiquement
`checkout.session.metadata` (qui porte `order_id`/`merchant_id`/
`checkout_session_type`, lignes 134-138) vers `payment_intent.metadata` — ce
sont deux objets distincts côté API Stripe ; seul un `payment_intent_data.metadata`
explicite (absent ici) le ferait. Un PaymentIntent créé pour une commande
ScanNOrder a donc une metadata Stripe **sans la clé `channel` du tout**
(clé absente, pas une valeur vide).

Seul point du repo qui écrit `Metadata["channel"] = "kiosk"` :
`internal/infrastructure/stripe/terminal.go:130`
(`CreateTerminalPaymentIntent`) — confirmé par grep sur `"channel"` dans tout
`internal/`, aucune autre écriture de cette clé.

### 2. Comportement Go d'un accès à une clé absente d'une map

`pi.Metadata` est un `map[string]string`. En Go, lire une clé absente d'une
map (même nil) ne panique jamais et retourne la valeur zéro du type valeur —
`""` pour `string`. Donc pour un PaymentIntent ScanNOrder :
`pi.Metadata["channel"]` vaut `""`, et `kioskTerminalMetadata`
(`internal/webhook/stripe/service.go:408`) :
```go
if pi.Metadata["channel"] != "kiosk" {
    return "", "", false
}
```
`"" != "kiosk"` → `true` → `return "", "", false`. Aucun faux positif possible,
aucun risque de panique — comportement natif du langage, pas une garde
applicative qui pourrait avoir un trou.

### 3. Chemin inverse : un event Terminal peut-il être traité par la logique ScanNOrder ?

Non. `ProcessEvent` (`internal/webhook/stripe/service.go:54-94`) est un
`switch event.Type` classique : chaque `case` fait un `return` immédiat,
aucun chemin n'appelle deux handlers pour le même event. `HandleCheckoutSessionCompleted`
n'est déclenché que par l'event `checkout.session.completed` (ligne 57-58).
Un PaymentIntent Terminal Kiosk est créé directement via
`t.sm.client.PaymentIntents.New(params)` (`terminal.go:136`, `PaymentMethodTypes:
["card_present"]`) — jamais via `checkout/session.New` — donc Stripe ne crée
aucun objet Checkout Session associé et n'émet donc **jamais**
`checkout.session.completed` pour un paiement Terminal. Aucun chemin où le
même event serait traité par les deux logiques.

### 4. Le flux ScanNOrder émet-il aussi `payment_intent.succeeded` aujourd'hui, et si oui, que devient-il ?

Oui — fait Stripe standard : un Checkout Session qui aboutit à un paiement
réussi émet **à la fois** `checkout.session.completed` **et**
`payment_intent.succeeded` pour le PaymentIntent sous-jacent (indépendamment
du produit Stripe utilisé pour créer ce PaymentIntent). `HandlePaymentIntentSucceeded`
est donc bien invoquée aussi pour un paiement ScanNOrder. Trace du chemin
emprunté (`service.go:356-369`) :

1. `handleTerminalPaymentSucceeded(ctx, &pi)` → `kioskTerminalMetadata(pi)` →
   `ok=false` (point 2 ci-dessus) → retourne immédiatement `(false, nil)`
   (`service.go:419-425`), sans toucher à `ConfirmKioskCardPayment` ni à
   `recordTerminalPayment`.
2. `handled=false, err=nil` → le code continue et exécute
   `s.repo.UpdatePaymentIntentStatus(ctx, pi.ID, "CAPTURED")` (ligne 368) — un
   simple `UPDATE stripe_payments SET payment_intent_status = 'CAPTURED' WHERE
   payment_intent_id = ?`. Ce comportement est **strictement antérieur** à
   l'introduction du Kiosk (déjà documenté dans le commentaire de
   `HandlePaymentIntentSucceeded`, "on retombe sur le comportement existant du
   flux Checkout en ligne... strictement inchangé") : ni erreur, ni double
   traitement, ni écriture incorrecte — que la ligne `stripe_payments`
   existe déjà (insérée entre-temps par `HandleCheckoutSessionCompleted` via
   `AddPaymentAndReturnID`) ou pas encore (event arrivé avant, `UPDATE` matche
   0 ligne, no-op silencieux, pas d'erreur SQL) : dans les deux cas le
   paiement est réellement capturé, donc `'CAPTURED'` est la valeur correcte
   à écrire, jamais une valeur erronée.

**Conclusion étape 0** : aucun risque réel de collision aujourd'hui entre les
deux canaux. La garde `pi.Metadata["channel"] != "kiosk"` est déjà stricte de
fait grâce à la sémantique Go des maps (point 2) — pas besoin de la
réécrire en `if channel, ok := pi.Metadata["channel"]; !ok || channel !=
"kiosk"`, ce qui serait équivalent en comportement mais plus verbeux sans
gain de sécurité réel ici. **Aucun changement fonctionnel appliqué.** Seul
ajout : un commentaire d'anticipation au-dessus de `kioskTerminalMetadata`
(`internal/webhook/stripe/service.go`) documentant que `channel` est le point
d'extension prévu pour de futurs canaux carte présente (`pos_till`,
`reservation_deposit`), chacun devant utiliser sa propre valeur distincte.

`go build ./...` et `go vet ./...` clean après l'ajout du commentaire (aucune
modification de logique).

---

## Phase 2 — `POST /kiosk/auth/reclaim` (ré-identification par device_id)

Contexte : suite de la Phase 1 (`kiosks.device_id`, migration
`062_kiosks_device_id`, `EnrollRequest.DeviceID` optionnel — voir
`docs/decisions.md`). Objectif : qu'une borne ayant perdu son refresh token
(storage effacé, réinstallation, rotation sans fenêtre de grâce — voir
`docs/KIOSK_ENROLLMENT_RESILIENCE_AUDIT.md` §4) retrouve son profil via
`device_id`, sans réenrôlement manuel dans le cas courant.

### Portée de la recherche par device_id

`Repository.FindKioskCandidatesByDeviceID` filtre `status IN ('active',
'inactive')` **dans la requête SQL elle-même** — une borne `revoked` n'est
jamais candidate, quel que soit le `device_id`/PIN fourni. Choix délibéré :
même une réponse "PIN required" révélerait l'existence d'une borne connue
pour ce `device_id` ; en excluant les `revoked` de la requête, une borne
volée puis révoquée depuis le back-office ne peut plus jamais se
ré-identifier par ce canal, point final — cohérent avec le risque documenté
dans l'audit (§4, "Fenêtre de révocation actuelle non instantanée").

**0 ligne ou >1 ligne (collision de device_id) → même réponse HTTP**
(`kiosk_not_found`, 404). Une collision n'est pas exposée comme un cas
distinct : le client n'a de toute façon qu'un seul comportement de repli
possible (l'enrôlement classique), donc aucune information supplémentaire
n'a de valeur actionnable pour lui — et ne pas distinguer les deux cas évite
de révéler qu'une collision existe (fuite d'information sur l'état interne
d'autres bornes).

### Silencieux vs PIN admin — seuil sur `last_heartbeat_at`

`kioskReclaimSilentWindow = 30 * 24h` (constante Go, pas de variable d'env
pour cette phase — pas de besoin exprimé de la rendre configurable
merchant-par-merchant). `last_heartbeat_at` récent (<30j) → réémission de
tokens strictement sans toucher au PIN, y compris si un `admin_pin` est
fourni dans la requête (le champ est simplement ignoré) : un appareil qui
donnait encore signe de vie récemment n'a pas de raison de justifier son
identité par un facteur supplémentaire. `last_heartbeat_at` absent (`NULL`,
borne jamais vue depuis l'enrôlement — ex. tablette échangée avant sa
première utilisation réelle) ou ancien (≥30j) → PIN admin obligatoire,
même logique de risque que "changement de tablette physique" discutée dans
l'audit (§4).

### PIN admin : réutilisation stricte de l'existant, pas de nouveau lockout serveur

`Service.verifyAdminPinCore` a été extrait de `VerifyAdminPin` (déchiffrement
`admin_pin_encrypted`, comparaison `subtle.ConstantTimeCompare`, lockout
Redis 5 tentatives/30s **par `kioskID`**, clé `kiosk:admin_pin:lockout:` déjà
existante) — `ReclaimDevice` appelle exactement la même fonction, avec le
`kioskID`/`admin_pin_encrypted` du candidat trouvé (pas besoin d'une requête
supplémentaire scoping merchant, la ligne est déjà en main). Résultat :
tenter un reclaim avec PIN et tenter `POST .../verify-admin-pin` sur la même
borne partagent le même compteur de lockout Redis — décision explicite du
prompt, pas un oubli. Aucun lockout serveur additionnel spécifique à
`/reclaim` ; le lockout propre à cet écran (5 tentatives → 30s, avec bascule
automatique vers l'enrôlement classique après épuisement) est géré côté
Flutter uniquement (voir `wello-kiosk/docs/KIOSK_DECISIONS.md`).

### Réutilisation de la ligne `kiosks` existante — pas de nouvelle borne

`ReclaimDevice` ne passe jamais par `CreateKiosk` : dans la même transaction,
`RevokeAllDeviceTokens` (même fonction que `RevokeKiosk`/hygiène habituelle)
puis `CreateDeviceToken` réémettent un nouveau refresh token sur le
`kiosk_id` du candidat trouvé — aucun double comptage de quota
(`GetActiveKioskCount` ne recompte rien puisqu'aucune ligne n'est insérée).
`UpdateKioskLastSeenOnReclaim` est une méthode dédiée (pas
`UpdateKioskHeartbeat` réutilisée telle quelle) : elle ne met à jour que
`last_heartbeat_at`/`last_ip`, jamais `app_version` — le client de reclaim ne
transmet pas cette information (contrairement à `POST .../auth/heartbeat`),
et réutiliser `UpdateKioskHeartbeat` aurait écrasé la dernière valeur connue
avec une chaîne vide. PIN admin inchangé : ni régénéré, ni ré-exposé dans
`ReclaimDeviceResponse` (qui a la même forme qu'`EnrollResponse` moins
`admin_pin`) — un reclaim silencieux ou par PIN n'a aucune raison de révéler
à nouveau le PIN existant.

### Endpoint et codes d'erreur

`POST /kiosk/auth/reclaim`, public (même groupe de routes que `/auth/enroll`
et `/auth/token/refresh`, avant le middleware `KioskAuth`) :
- `{device_id}` → succès silencieux + tokens, ou `401
  kiosk_reclaim_pin_required`, ou `404 kiosk_not_found`.
- `{device_id, admin_pin}` → succès + tokens, ou `401
  kiosk_admin_pin_invalid`, ou `429 kiosk_admin_pin_locked` (même format que
  `verify-admin-pin` : `{"error": "kiosk_admin_pin_locked", "delay_seconds":
  N}`).

Aucun rate-limit par IP sur cet endpoint pour cette phase — décision
explicite (comme documenté dans le prompt de cette session), à revisiter si
besoin s'en fait sentir en usage réel.

### Tests

Couverture par `sqlmock` (`internal/modules/kiosk/reclaim_test.go`, pas
d'accès DB réel nécessaire) : device_id vide, 0 candidat, collision (2
candidats), heartbeat récent (silencieux, PIN ignoré même absent), heartbeat
`NULL`/ancien sans PIN (`pin_required`), PIN invalide, PIN valide (réémission
complète avec assertions sur la séquence transactionnelle
Begin/Exec×3/Commit). `go build ./...` et `go test
./internal/modules/kiosk/...` verts.
