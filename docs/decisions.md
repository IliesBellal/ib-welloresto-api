# Decisions

### Bookings — heure décalée (-2h) dans la liste des résas tablette POS (2026-08-13)

- **Symptôme rapporté** : une résa créée pour 18h heure française (stockée
  correctement en base — `booking_date_from` = l'instant UTC équivalent,
  vérifié via inspection directe) s'affichait à 16h dans le carnet de
  réservations de la tablette POS (Flutter).
- **Root cause identifiée** : régression de la migration `056_bookings_dates_utc`
  (bascule du stockage `booking_date_from`/`booking_date_to` en UTC).
  `ListBookingsBackOffice` (`internal/modules/bookings/repository.go`,
  utilisé par `GET /bookings`, l'endpoint carnet de résas de la tablette)
  formatait encore la date via `bkgDateTimeFmt` (`to_char`/`DATE_FORMAT`)
  **sans conversion vers le fuseau du marchand** — correct avant la 056
  quand la colonne stockait déjà l'heure locale marchand en naïf, faux
  depuis puisque `to_char` restitue l'heure "wall-clock" du fuseau de
  session Postgres (UTC), renvoyée en chaîne brute sans offset. Le client
  Flutter (`booking_date_utils.dart`, fonction `bookingDateTimeFromUtcString`
  — mal nommée, elle ne faisait aucune conversion UTC→local) parsait cette
  chaîne en supposant qu'elle était déjà en heure locale marchand (c'était
  vrai avant la 056, plus depuis). D'où le décalage exact de l'offset du
  marchand (+2h l'été).
- **Endpoint détail (`GetBooking`) non affecté** : celui-ci renvoyait déjà un
  timestamp Unix UTC (`bookings_fetcher.go`), correctement reconverti côté
  client par `bookingDateTimeFromUnixSeconds`
  (`DateTime.fromMillisecondsSinceEpoch` → heure locale de l'appareil). Ce
  pattern déjà existant a servi de référence pour le correctif.
- **Autres usages de `bkgDateTimeFmt("b.booking_date_from")` audités et
  écartés** : `ListPendingBookingsToExpire` / `ListBookingsForReminder`
  (SMS/email de rappel) utilisent aussi ce format brut UTC, mais
  `BookingContact.StartDate` est explicitement documenté `// UTC` et
  reconverti côté Go dans `reminders.go` via `time.LoadLocation(b.Timezone)`
  avant formatage du message — pattern déjà correct, non touché.
- **Correctif appliqué (serveur + client, décision : aligner sur le pattern
  Unix déjà en place plutôt que patcher le format string)** :
  - Serveur : `ListBookingsBackOffice` sélectionne désormais la colonne
    brute `b.booking_date_from` (au lieu de `bkgDateTimeFmt(...)`), scannée
    en `sql.NullTime` puis convertie en Unix UTC (`helpers.NullTimePtr(...).UTC().Unix()`,
    même pattern que `bookings_fetcher.go`). `BookingListItem.BookingDateFrom`
    passe de `string` à `int64` (`internal/modules/bookings/models.go`).
  - Client : `BookingListItemDto.dateFrom` bascule sur
    `bookingDateTimeFromUnixSeconds` (comme `BookingDto`) ; l'ancienne
    fonction `bookingDateTimeFromUtcString`, plus utilisée nulle part,
    supprimée (`booking_date_utils.dart`).
- **Non traité ici, signalé mais hors périmètre demandé** : les filtres
  `date_from`/`date_to` de `GET /bookings` (jour affiché sur la tablette)
  sont envoyés par le client comme bornes de journée en heure locale
  naïve (`bookings_api.dart`, `_formatDateTime` sur un `DateTime` local)
  et comparés tels quels à la colonne UTC côté SQL
  (`ListBookingsBackOffice`, `repository.go`) — même classe de bug que
  celui corrigé ici, mais côté filtrage plutôt qu'affichage. Impact limité
  aux résas proches de minuit (le reste de la journée retombe dans la même
  fenêtre malgré le décalage). À traiter séparément si confirmé.
- **Statut d'exécution** : `go build ./...`, `go vet ./internal/modules/bookings/...`
  et `go test ./internal/modules/bookings/...` passent (tests unitaires ;
  la suite `postgres_integration_test.go` n'a pas été relancée, nécessite
  une base Postgres vivante). Côté Flutter, `flutter analyze` passe sur les
  deux fichiers modifiés ; pas de test automatisé existant sur ce chemin,
  vérification manuelle sur device non faite dans cette session.

### Rapport comptable "réel" — deux correctifs d'audit + dette de test notée (2026-08-11)

- **Asymétrie `COALESCE`/`NULLIF` corrigée** : la branche
  `cash_registers_items` de `GetRealPaymentsData`
  (`internal/modules/pos/accounting/repository.go`) ne gérait qu'un
  `labels.label` `NULL` (`COALESCE(l.label, cri.mop)`), pas une ligne
  `labels` existante mais au libellé vide (`''`) — contrairement à la
  branche `cash_registers_custom_items`, déjà en
  `COALESCE(NULLIF(l.label, ''), ...)`. Un libellé `''` produisait une
  ligne à blanc dans le rapport (ou pire, une ligne invisible : `''` fait
  disparaître le montant du regroupement par libellé attendu) au lieu du
  repli sur le code brut. Alignée sur le même pattern des deux côtés.
  Confirmé par un cas de test dédié (`labels.label=''` pour un MOP connu) :
  échoue sans le correctif, passe avec.
- **Détail du `log.Warn` de dérive enrichi** : `GetTrustedEnclosedRegisterIDs`
  loggait seulement `cash_register_id`/`merchant_id` en cas d'écart. Ajoute
  le(s) MOP en dérive avec montant figé (`cash_registers_items`) et montant
  live recalculé, pour que le support puisse diagnostiquer un événement de
  dérive sans avoir à re-dériver l'écart manuellement en base.
- **Dette de test notée, non traitée ici** : ce `log.Warn` n'est vérifié
  que par lecture de code, pas par une assertion automatisée — les
  binaires de test `postgres_integration` n'appellent jamais
  `zap.ReplaceGlobals` (contrairement à `cmd/api/main.go`), donc
  `logger.FromContext` retombe sur le logger zap global no-op et rien
  n'est capturable depuis un test tel quel. Pour fermer ce trou
  proprement : injecter un logger observer via `logger.WithLogger(ctx,
  ...)` dans le contexte de test (pattern à créer, n'existe pas encore
  dans ce dépôt) plutôt que de compter sur `context.Background()` nu.

### `TestPOSAccountingReports_Postgres` — échec pré-existant, sans rapport avec ce dépôt (2026-08-11)

- Le sous-test `GetTVAData = (..., want 1 ligne)` de
  `TestPOSAccountingReports_Postgres`
  (`internal/modules/pos/accounting/postgres_integration_test.go`) échoue
  contre le Postgres de dev actuel : une vraie catégorie `tva_id=-1`
  ("TVA Delivery fees 20%", `tva_desc` ≠ `'itest'`) existe déjà dans la base
  partagée, ce que l'assertion (écrite avant l'existence de cette ligne)
  n'anticipait pas — `GetTVAData` retourne une ligne "frais de livraison" à
  TTC=0 pour toute commande sans frais de livraison dès que cette catégorie
  sentinelle existe globalement (`repository.go`, jointure
  `tva_fees.tva_id = '-1'` sans filtre `delivery_fees > 0`).
- **Confirmé indépendant du chantier "réel des caisses closes"** (entrée
  suivante) : reproduit à l'identique en stashant les 3 commits de ce
  chantier et en relançant le test sur le code d'origine, contre la même
  base. Aucun fichier de ce chantier ne touche `GetTVAData`,
  `tva_categories`, ni les fixtures de ce test.
- **Non corrigé ici** (hors périmètre) : soit scoper l'assertion aux lignes
  du test (même pattern que `TestGetCashRegisterReport_Postgres`, qui filtre
  déjà ses assertions par clé plutôt que par nombre total de lignes, pour
  cette même raison de table de référence globale partagée), soit filtrer
  `delivery_fees > 0` dans la branche frais de livraison de `GetTVAData` —
  à trancher et traiter dans un ticket dédié.

### Rapport comptable — section Encaissements sur le réel des caisses closes (2026-08-11)

- **Contexte** : un bug (`orders.price=0` alors que `orderitems` avait les
  bons prix) a fait diverger le total TVA et le total Encaissements du
  rapport comptable mensuel d'un merchant, révélant que la section
  Encaissements (`AccountingRepository.GetPaymentsData`,
  `internal/modules/pos/accounting/repository.go`) sommait le théorique en
  direct (table `payments`), sans lien avec ce que le restaurateur déclare
  réellement à la clôture de caisse. Décision produit (sur le modèle
  Zelty) : la section Encaissements du PDF affiche désormais le **réel
  uniquement** — `cash_registers_items` (figé à la clôture) +
  `cash_registers_custom_items` (ajustements manuels du caissier) — pour
  les registres `enclosed=true` dans la période, **sans repli théorique
  mélangé dans ce rapport**. Un merchant/mois sans registre correctement
  clôturé affiche une table vide avec un message explicite plutôt qu'un
  tableau théorique.
- **`GetPaymentsData` (théorique) non modifié** : reste utilisé tel quel
  par `internal/modules/pos/reports` (page back-office séparée,
  `/pos/reports/tva` et `/pos/reports/payments`, déjà l'extraction
  théorique jour par jour) — aucune régression possible puisqu'aucun
  fichier de ce module n'est touché.
- **Nouvelles méthodes** `GetTrustedEnclosedRegisterIDs` et
  `GetRealPaymentsData` (`accounting/repository.go`) :
  - Garde-fou anti-dérive : `cash_registers_items` est un instantané figé
    à la clôture, jamais recalculé. Avant de faire confiance à un
    registre `enclosed`, son instantané est comparé à un recalcul live
    des mêmes paiements (même filtre que `cashRegisterReportMOPSQL`,
    `internal/modules/cash_registers/repository.go`). Le moindre écart
    par `(cash_register_id, mop)` — ex. un paiement corrigé sur une
    commande après que son registre a été enclosed, exactement le cas qui
    a motivé ce chantier — écarte le **registre entier** du réel pour ce
    rapport (pas de repli partiel mélangé).
  - Exclusion de canal (`accountingExcludedChannelMOPs` :
    `STRIPE`/`UBER_EATS`/`DELIVEROO`) : `cash_registers_items` est déjà
    agrégé par `(cash_register_id, mop)` à la clôture et ne porte donc pas
    nativement le filtre `brand`/`created_by` du théorique — Uber Eats et
    Deliveroo ont leur propre gestion TVA à venir, `STRIPE` est
    exclusivement ScanNOrder (confirmé par recherche exhaustive dans le
    repo).
  - `LEFT JOIN` volontaire vers `labels` (au lieu de `INNER JOIN` comme le
    théorique) : un code MOP non libellé ne doit jamais faire disparaître
    silencieusement un montant du total censé être le plus fiable —
    affiché sous son code brut à défaut de libellé. Un custom item à
    texte libre sans correspondance MOP apparaît de la même façon comme
    sa propre ligne.
- **Immuabilité post-enclose durcie** (prérequis) :
  `AddCustomItem`/`DeleteCustomItem`
  (`internal/modules/cash_registers/repository.go`) ne vérifiaient que
  `closed`, jamais `enclosed` — rien n'empêchait de modifier les custom
  items d'un registre déjà verrouillé définitivement. Nouvelle méthode
  `isCashRegisterEditable` (`closed AND NOT enclosed`), utilisée
  uniquement par ces deux méthodes. `isCashRegisterClosed` reste inchangée
  pour `GetCashRegisterSummary` et `EncloseCashRegister`, qui doivent
  continuer de fonctionner sur un registre déjà enclosed (trouvé en
  écrivant les tests : réutiliser `isCashRegisterClosed` pour tout aurait
  cassé l'affichage du résumé d'un registre déjà clôturé).
- **Non traité (limite assumée)** : un registre ouvert en toute fin de
  mois et encore ouvert après minuit local pourrait contenir des
  commandes du mois suivant, comptées dans le réel du mauvais mois
  (ancrage sur `start_date`, cohérent avec l'ancrage `creation_date` des
  commandes ailleurs dans le module). Cas marginal, non traité par du
  code cette itération — un `log.Warn` signale les registres en dérive
  pour investigation manuelle.

### Traçabilité HACCP (photos + commentaire) — trou schéma Postgres cible (2026-07-23)

- Migration `067_haccp_traceability` (`haccp_traceability_records`/`haccp_traceability_photos`) ajoutée après la préparation Postgres, comme `planning_day_comments` en son temps (voir `docs/migration-postgres/26-planning-day-comments-integration.md`) : il manque encore sa traduction dans `04-schema-postgres-target.sql` (+ mise à jour `03-table-usage-audit.md`/`07-module-inventory.md`) — à rattraper avant le cutover Postgres (Phase 8), sinon la section traçabilité de `internal/modules/haccp/postgres_integration_test.go` échouera contre le Postgres de dev.

### Phase 1 — Fondation device_id enrôlement Kiosk (2026-07-22)

- **Contexte** : suite à l'audit `docs/KIOSK_ENROLLMENT_RESILIENCE_AUDIT.md`
  (aucun identifiant device stable n'existe côté API ni côté Flutter Kiosk —
  seul le `kiosk_id` serveur, perdu si le stockage local est effacé). Cette
  phase pose uniquement la fondation de capture, **sans** endpoint de
  ré-identification (`/kiosk/auth/reclaim` reste à faire).
- **`kiosks.device_id VARCHAR(191) NULL`** (migration `062_kiosks_device_id`) :
  pas de contrainte UNIQUE — l'unicité applicative (bloquer un reclaim sur
  device_id dupliqué plutôt que faire échouer une insertion) sera gérée par
  le futur endpoint. Colonne nullable, aucun backfill : les bornes déjà
  enrôlées restent à `NULL` et continuent sur le flow d'enrôlement classique.
  Ce n'est pas un secret — aucune valeur d'authentification n'est attachée à
  ce champ.
- **`EnrollRequest.DeviceID` optionnel** (compat ascendante) : chaîne vide ou
  absente → `NULL` stocké (pas une chaîne vide), pour ne jamais faire
  coïncider deux bornes sur un device_id "vide" au lieu d'un `NULL` (qui ne
  matche jamais rien dans une future recherche exacte) — voir
  `Service.EnrollDevice`, conversion en `*string` avant `Repository.CreateKiosk`.
  Aucun changement à `ValidateAccessToken`, `RefreshDeviceToken`, ni à aucun
  comportement d'auth existant ; aucune nouvelle route.
- **Côté Flutter (wello-kiosk) : reporté.** Le plan initial (réutiliser
  `platform_device_id_plus` comme `wello_resto_flutter`, cache dans une
  nouvelle clé secure-storage `kiosk_os_device_id` distincte de
  `AuthService.keyDeviceId`) est bloqué par un conflit de dépendances réel :
  `wakelock_plus ^1.6.1` (déjà utilisé par wello-kiosk, requis pour empêcher
  la mise en veille de la borne) exige `win32 ^6.0.1`, incompatible avec
  `device_info_plus ^11.3.0` (dépendance transitive de
  `platform_device_id_plus ^1.0.7`), qui exige `win32 ^5.5.3`.
  `wello_resto_flutter` n'a pas `wakelock_plus`, d'où l'absence de conflit
  là-bas. Un downgrade `wakelock_plus` → `^1.5.2` résout la contrainte
  (confirmé par `flutter pub get`, testé puis annulé) mais downgrade aussi
  `win32`, `package_info_plus` et `flutter_secure_storage_windows` en
  cascade — décision reportée à Ilies plutôt que tranchée unilatéralement.
  **Aucun fichier Flutter modifié** (tous les edits ont été passés puis
  annulés pour ne pas laisser le projet dans un état qui ne compile pas).
- **Prochaine étape (hors scope ici)** : trancher le conflit de dépendance
  côté wello-kiosk, puis reprendre la capture `device_id` client (même
  design que ci-dessus), avant d'attaquer `/kiosk/auth/reclaim`.

### Phase 2 — Endpoint `/kiosk/auth/reclaim` par device_id (2026-07-22)

- **Contexte** : suite de la Phase 1 (fondation `device_id`). Objectif :
  permettre à une borne dont le refresh token est perdu (stockage effacé,
  réinstallation) de retrouver son profil via `device_id`, sans réenrôlement
  manuel dans le cas courant — voir
  `docs/KIOSK_ENROLLMENT_RESILIENCE_AUDIT.md`.
- **Écart constaté avec le texte de la Phase 1 ci-dessus** (audit-first,
  signalé ici plutôt que corrigé silencieusement) : l'entrée Phase 1 déclare
  la capture `device_id` côté Flutter "reportée" à cause d'un conflit
  `wakelock_plus`/`platform_device_id_plus`. En pratique, `wello-kiosk` a
  depuis résolu ce conflit avec le package `android_id` (Android uniquement,
  aucune dépendance `win32`) — `AuthService.getOrGenerateOsDeviceId()` existe
  et fonctionne, documenté dans `wello-kiosk/docs/KIOSK_DECISIONS.md`
  ("Identifiant device OS (android_id)"). Cette phase s'appuie donc sur cette
  fondation déjà en place ; l'entrée Phase 1 de ce fichier n'a simplement
  jamais été mise à jour après cette résolution côté `wello-kiosk`.
- **Recherche des candidats par `device_id`** : `status IN ('active',
  'inactive')` uniquement — une borne `revoked` n'est jamais éligible et
  n'apparaît même pas dans la requête SQL (`Repository.
  FindKioskCandidatesByDeviceID`), pour ne pas laisser fuiter son existence.
  0 ligne ou >1 ligne (collision) → réponse HTTP identique
  (`kiosk_not_found`, 404) : le client ne distingue jamais les deux cas et
  retombe sur l'enrôlement classique dans les deux cas.
- **Réactivation silencieuse conditionnée au dernier heartbeat connu** :
  `last_heartbeat_at` < 30 jours → réémission de tokens sans aucune
  vérification de PIN (même si un PIN est configuré) ; `last_heartbeat_at`
  ≥ 30 jours ou `NULL` (borne jamais vue depuis son enrôlement) → PIN admin
  obligatoire. Constante `kioskReclaimSilentWindow` (30 jours), non
  configurable par env var pour cette phase.
- **PIN admin réutilisé tel quel** : `Service.verifyAdminPinCore` extrait la
  logique déjà existante de `VerifyAdminPin` (déchiffrement,
  comparaison à temps constant, lockout Redis 5 tentatives/30s par
  `kioskID`) — aucun lockout serveur dédié à `/reclaim`, le lockout propre à
  cet écran est géré côté app Flutter (voir plus bas).
- **Réutilisation de la ligne `kiosks` existante** : `ReclaimDevice` ne crée
  jamais de nouvelle borne — révoque tous les refresh tokens existants
  (`RevokeAllDeviceTokens`, même mécanisme d'hygiène qu'un `refresh` normal)
  puis en émet un nouveau, met à jour `last_heartbeat_at`/`last_ip`
  (`UpdateKioskLastSeenOnReclaim`, dédiée pour ne jamais écraser
  `app_version` avec une valeur vide — le client de reclaim ne la transmet
  pas), PIN admin inchangé (ni régénéré ni ré-exposé dans la réponse).
- **Endpoint public** (`POST /kiosk/auth/reclaim`, pas de Bearer, même
  famille que `/auth/enroll` et `/auth/token/refresh`), sans rate-limit par
  IP pour cette phase (décision explicite, à revisiter plus tard si besoin).
- **Nouveau code d'erreur** `kiosk_reclaim_pin_required` (401) ; 0/>1
  candidat réutilise `kiosk_not_found` (404) existant plutôt que d'en créer
  un distinct pour la collision.

### Statuts produits — fiabilisation backend + affichage/blocage SNO (2026-07-05)

- **Contexte** (audit du 2026-07-04) : `products.status` est une colonne texte
  libre. Valeurs effectives : `available`/`1` (commandable), `not_available`
  (toggle POS)/`0`, `out_of_stock`, `removed_from_menu` (soft-delete, filtré).
  Règle de vérité : commandable ⇔ statut ∈ {available, 1} (POS + pricing).
- **`UnavailableProductInfo.Status` int → string** : la requête
  `GetUnavailableProducts` retourne des statuts textuels — le `rows.Scan` en
  int échouait dès qu'un produit non-numérique était indisponible (pricing en
  erreur au lieu de la liste `unavailable_products`).
- **`ComponentUsage.Status` int → string** (models + module menu) : même bug,
  plus grave — le scan du menu (`GetMenuFromMerchantId`) échouait dès qu'un
  composant portait un statut textuel : **un ingrédient désactivé depuis le
  POS cassait le GetMenu entier** (menu 500 POS/SNO/Kiosk). Scans corrigés
  (menu/repository.go, orders_fetcher_builder.go). Les parseurs POS font déjà
  `json['status']?.toString()` — changement de type wire sans impact client.
- **`not_available` ajouté** : (a) aux checks composants de
  `GetUnavailableProducts` (orders) et `validateProductAvailability`
  (order_life_cycle) — un composant désactivé au POS ne bloquait pas les
  produits qui en dépendent ; (b) à `mapWelloStatusToAvailability` (menu) —
  la sync Uber Eats/Deliveroo était silencieusement sautée quand le POS
  désactivait un produit.
- **Garde CreateOrder SNO** (`scannorder/service.go`) : le pricing répond
  "success" même avec `unavailable_products` non vide, et le gate de création
  (`validateProductAvailability`) ne bloque que `out_of_stock` — un produit
  `not_available` pouvait être **commandé et payé** via SNO. La création SNO
  retourne désormais `{status: "unavailable_products", message: <noms>}`
  (même statut que le gate order_life_cycle).
- **Non traité (dette notée)** : le gate de création (partagé POS/Kiosk) ne
  bloque toujours que `out_of_stock` au niveau produit (asymétrie voulue :
  un staff POS peut encaisser un produit désactivé à la vente en ligne) ;
  le Kiosk n'inspecte pas `unavailable_products` (cf.
  KIOSK_VS_SCANNORDER_STRUCTS.md §propositions) ; `products.available`
  (PATCH /availability) reste sans effet sur le menu — à filtrer ou déprécier.
- Côté SNO : affichage Épuisé/Indisponible + blocage panier/checkout — voir
  `wello-resto-scannorder/docs/decisions.md` (entrée du 2026-07-05).

### Refonte page de suivi de commande SNO — carte temps réel + layouts (2026-07-02)

- `OrderTrackingPage` (repo `wello-resto-scannorder`) refondue : side sheet
  400px + carte interactive sur desktop (≥1024px), carte plein écran + bottom
  sheet `vaul` (3 snap points, safe-area iOS) sur mobile **et tablette**
  (justification : sous 1024px, un side sheet de 400px ne laisserait qu'une
  carte étroite en portrait — le layout tactile reste supérieur jusqu'à `lg`).
  Mode IN sans carte, panneau centré avec `pager_number` en bloc dominant.
- Suivi livreur branché sur `PublicDeliverySession` (inline dans `GET order`,
  polling 10s inchangé) : marqueur interpolé 30s le long de la polyline OSRM
  (port fidèle de `driver_marker_animation.dart`, constantes d'origine
  conservées dans `src/lib/geo.ts`), reroute sur déviation (75m / 2 points /
  90s min), ETA fourchette calculée côté client (segments non parcourus),
  rafraîchie en continu. `stops_before_you > 0` → rang affiché, pas d'ETA
  minute ni de polyline livreur→client. Staleness approximée côté client
  (position inchangée > 60s → marqueur grisé), en attendant l'exposition de
  `last_position_at` (amélioration listée au contrat).
- Types SNO étendus : `PublicDeliverySession`/`PublicDeliveryMan`,
  `Order.delivery_session`/`pager_number`, `OrderCustomer.customer_lat/lng`.
- Détail des choix créatifs : [audits/2026-07-02-order-tracking-page-design-decisions.md](audits/2026-07-02-order-tracking-page-design-decisions.md).

### Finalisation suivi livreur SNO — fix GetDeliverySessionByOrderID + contrat PublicDeliverySession (2026-07-02)

- **Fix `GetDeliverySessionByOrderID`** (`internal/modules/scannorder/repository.go`) :
  la requête n'avait ni `ORDER BY` ni filtre de statut. Après un re-dispatch
  d'une commande (session initiale `failed`/`canceled` côté livreur, nouvelle
  session créée), elle pouvait retourner n'importe quelle session historique
  arbitraire au lieu de la session active courante. Ajout d'un
  `ORDER BY ds.start_date DESC` (pas de colonne `created_at` sur
  `delivery_session`) + `WHERE ds.status != 'canceled'` (garde `active` et
  `done` — une session tout juste terminée doit rester consultable quelques
  minutes pour l'affichage post-livraison côté SNO) + `LIMIT 1`. Valeurs de
  `delivery_session.status` confirmées via la migration
  `035_delivery_session_status_normalization` : uniquement `active`/`done`/
  `canceled` depuis cette normalisation. Le seul appelant
  (`Service.GetOrderSNO`) gérait déjà le cas `nil`, aucun changement
  nécessaire côté appelant.
- **Contrat `PublicDeliverySession` documenté** dans
  [docs/api-contracts/public-delivery-session.md](api-contracts/public-delivery-session.md) :
  champs exposés/non-exposés, sources de données ETA côté SNO (restaurant/
  client/livreur), fréquences de polling recommandées (30s position livreur,
  10s reste du payload), et pattern d'interpolation du marqueur à reproduire
  côté SNO (référence `driver_marker_animation.dart` du POS Flutter). Prêt
  pour consommation par la refonte visuelle SNO. Pas de nouvel endpoint —
  `PublicDeliverySession` reste inline dans
  `GET /scannorder/{slug}/orders/{id}` (Option B, déjà validée par le
  hotfix RGPD ci-dessous).

### 🔴 HOTFIX RGPD — Fuite de données sur GET /scannorder/{slug}/orders/{order_id} (2026-07-02)

- **Problème** : l'endpoint public (client final non authentifié suivant sa
  commande) retournait la `delivery_session` interne complète inline. Son champ
  `orders[]` contenait **toutes les commandes de la tournée du livreur**, chacune
  avec le `Customer` complet des autres clients : noms, adresses, téléphones,
  emails, GPS et `customer_delivery_notes`. Non-conformité RGPD.
- **Correctif** : la `delivery_session` est désormais filtrée via un DTO dédié
  `scannorder.PublicDeliverySession` (+ `PublicDeliveryMan`). Il n'expose que la
  **position du livreur** (`lat`/`lng`/`status`), son **prénom** uniquement, le
  **statut** de la session, et un rang **non-identifiant** du stop du client
  (`stops_before_you` / `total_stops`, des compteurs — jamais les commandes).
  Réponse encapsulée dans `PublicSNOOrder` (embarque l'`Order` du client, dont
  les données propres restent légitimes, et écrase le champ `delivery_session`).
- **Séparation modèle interne / public** : le DTO public est distinct de
  `models.DeliverySession` (utilisé par les endpoints merchant authentifiés, qui
  continuent de voir la tournée complète). Un futur ajout de champ sur le modèle
  interne ne peut donc plus recréer la fuite automatiquement.
- Fin de la fuite de données personnelles des autres clients de la tournée.

### Upsell — Configuration produit complète (2026-07-01)

- Backend : GetUpsellProducts filtre désormais is_product_group = 1 (produits
  groupe non commandables seuls exclus de l'upsell).
- Backend : GetUpsell enrichit chaque produit via GetProductFromMerchantId
  (même fonction que l'endpoint fiche produit) — configuration complète
  (attributs, options, prix) retournée pour chaque suggestion.
- Backend : résultat mis en cache Redis, même TTL que GetMenu.
- Frontend : aucun changement — UpsellPopup.tsx était déjà câblé pour détecter
  les produits configurables (isConfigurable()) et ouvrir ProductModal si besoin.

### Phase B — Unification logique upsell (2026-07-01)

- SuggestedItem enrichi : configuration produit complète (attributs, options,
  prix par canal) retournée par GenerateUpsell pour toutes les plateformes.
- Nouveau handler SNO POST /scannorder/{slug}/upsell utilisant GenerateUpsell
  (Apriori/LLM/featured, contextuel au panier), payload PricingRequest réutilisé.
- Ancienne route GET /scannorder/{slug}/upsell marquée dépréciée, conservée
  temporairement (suppression prévue en Phase A résiduelle).
- Frontend SNO : useUpsell refactor sur le pattern usePricing (POST + debounce
  + queryKey dynamique sur le panier).
- Tracking non branché sur SNO — prévu en Phase C.

### Phase C — Tracking upsell SNO et Kiosk (2026-07-01)

- Migration : colonne channel ENUM('POS','SNO','KIOSK') ajoutée à
  upsell_suggestions (DEFAULT 'POS' pour rétro-compatibilité).
- GenerateUpsell / CreateSuggestion propagent désormais le canal
  jusqu'à la persistence (les 3 handlers passent leur canal explicitement).
- SNO et Kiosk : suggestion_id désormais retourné dans la réponse upsell,
  transporté par le front jusqu'à la création de commande, et peuplé dans
  UpsellSuggestionID du RequestObject.
- Frontend SNO : suggestion_id stocké dans useCart au moment de l'acceptation
  d'une suggestion (option A, cohérence avec POS Flutter).
- Kiosk : correctif symétrique appliqué (les lignes upsell_suggestions
  n'étaient jusqu'ici jamais rattachées à un order_id, l'ID étant
  silencieusement jeté).
- TrackAsync : non modifié — se déclenche automatiquement dès que
  UpsellSuggestionID est non-vide, tous canaux confondus.

### POS Flutter — Branchement product + suggestion_id (2026-07-02)

- UpsellItemDto (POS) enrichi d'un champ `product` optionnel (ProductDto
  complet, parsé via ProductResponse.fromJson) ; UpsellResultDto enrichi
  d'un `suggestionId`.
- Tap sur une suggestion : si `product` est présent, popup de configuration
  (options/groupe) ouverte comme pour un ajout depuis le menu, au lieu du
  ProductDto synthétique précédent (configuration vide hardcodée).
- `suggestion_id` stocké dans UpsellController uniquement à l'acceptation
  d'une suggestion (tap), pas à la simple réception de la liste ; transmis
  jusqu'à OrderDto.upsellSuggestionId puis OrderPayload.upsell_suggestion_id
  à la création de commande. Réinitialisé quand le panier est vidé ou la
  commande finalisée.
- **Limitation connue** : si l'utilisateur accepte deux suggestions
  provenant de batches upsell différents (un nouveau fetch a eu lieu entre
  les deux, donc un nouveau `suggestion_id` côté backend) avant de valider
  la commande, seule la dernière suggestion acceptée est trackée — le
  POS ne transmet qu'un seul `upsell_suggestion_id` par commande, reflet
  direct du modèle `RequestObject.UpsellSuggestionID` (un seul champ,
  pas une liste). Aucun contournement possible côté frontend sans
  évolution du modèle backend (ex. accepter une liste de suggestion_id).

### Homogénéisation des DTO upsell Kiosk (2026-07-01)

- `Service.GetUpsellSuggestions` (kiosk) retourne désormais directement
  `*upsell.UpsellResult` (même structure que `/orders/upsell` POS : `suggestions`,
  `source`, `suggestion_id`) au lieu des DTO dédiés `KioskUpsellSuggestion` /
  `KioskUpsellResponse`, supprimés (`internal/modules/kiosk/models.go`). Plus
  de mapping spécifique Kiosk à maintenir.
- Correction du bug de performance identifié en Phase B (audit
  `docs/audits/2026-07-01-upsell-v2.md`) : la boucle de construction des
  suggestions refaisait un appel `GetProductFromMerchantId` par suggestion,
  alors que `sugg.Product` est déjà chargé par `enrichWithProductConfig` en
  amont dans `GenerateUpsell`. Cet appel redondant est supprimé — `sugg.Product`
  est réutilisé tel quel.
- Nettoyage produit par canal appliqué à la volée avant sérialisation, pas en
  amont dans le cache : POS reste en mode brut (4 prix, `ProductEntry` non
  nettoyé, comportement inchangé). SNO applique `cleanProductForSNO`
  (inchangé). Kiosk applique une nouvelle fonction dédiée `cleanProductForKiosk`
  (`internal/modules/kiosk/service.go`) — distincte de `cleanProductPricesForKiosk`
  (conservée telle quelle, encore utilisée par `mapProductEntryToKioskProduct`
  pour `GetMenu`/`GetProduct`). `cleanProductForKiosk` a été écrite spécifiquement
  pour ce chantier : exposer un `ProductEntry` brut sans nettoyage aurait fuité
  des champs internes/sensibles (`cost_price`, `foodcost_percent`,
  `margin_percent`, `merchant_id`, indicateurs de sync Uber Eats/Deliveroo,
  etc.) au client Kiosk — `cleanProductPricesForKiosk` seule ne fait que
  collapser le prix, elle ne les retire pas. `cleanProductForKiosk` reprend les
  principes de `cleanProductForSNO` (retrait des mêmes catégories de champs),
  adaptés à la convention Kiosk IN/TAKE_AWAY (pas de DELIVERY).
- Effet de bord sur `source` : la réponse Kiosk renvoyait auparavant une valeur
  simplifiée (`"apriori"` / `"featured_fallback"`) ; elle expose désormais les
  valeurs brutes d'`upsell.Service` (`pattern`, `llm`, `cached_pattern`,
  `cached_llm`, `featured_fallback`, `disabled`), identiques à celles du POS.
  Le Flutter Kiosk ne fait aujourd'hui qu'afficher/logger `source`, sans
  brancher de logique dessus — changement sans impact fonctionnel connu, à
  vérifier si un futur usage du champ apparaît côté client.
- Effet de bord sur le filtrage : une suggestion dont l'enrichissement produit
  a échoué en amont (`sugg.Product == nil`, best-effort dans
  `enrichWithProductConfig`) est désormais exclue de la réponse Kiosk plutôt
  que retombée sur un second fetch individuel — même comportement que
  `scannorder.PostUpsell` pour la même situation.
- Dette technique : `KioskUpsellRequest` (`POST /kiosk/upsell`) ne transporte
  toujours pas de `fulfillment_type` — `GetUpsellSuggestions` reçoit `""`,
  qui tombe sans erreur sur le prix de base (IN) dans `cleanProductForKiosk`
  (pas de crash, mais un client TAKE_AWAY verra le prix sur place dans la
  suggestion tant que ce n'est pas câblé). À threader depuis le payload de
  la borne si le prix à emporter doit être exact dans le popup d'upsell.

### Kiosk Flutter — Branchement product + suggestion_id (2026-07-02)

- `UpsellSuggestion` (Kiosk) enrichi d'un champ `product` optionnel (`Product`
  complet, même modèle que `/kiosk/menu`) ; `UpsellResponse` enrichi d'un
  `suggestionId` racine (identifie le batch, pas une suggestion individuelle).
- `_selectSuggestion` utilise `suggestion.product` directement au lieu d'un
  appel systématique à `MenuController.findOrFetchProduct` ; fallback
  transitoire conservé si `product` est absent.
- `suggestion_id` stocké dans `UpsellController` au moment du **tap** sur une
  suggestion (pas à l'ajout effectif au panier, contrairement à SNO — le
  chemin "suggestion configurable" ouvre une route produit indépendante de
  l'écran upsell côté Kiosk, rendant la détection post-ajout fragile).
  Transmis jusqu'à `RequestObject.upsellSuggestionId`. Réinitialisé au
  prochain tap, à la commande finalisée avec succès, ou au panier vidé.
- **Même limitation connue que POS/SNO** : "dernière acceptée gagne", un seul
  `upsell_suggestion_id` par commande.
