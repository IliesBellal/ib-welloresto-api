# Decisions

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
