# Audit de traçabilité upsell — `is_upsell` end-to-end (4 codebases)

Date : 2026-07-02. Mode LECTURE SEULE — aucune modification de code, seul ce
rapport a été créé.

## Contexte

Migration 044 ajoute `orderitems.is_upsell` (TINYINT, DEFAULT 0) et
`upsell_suggestions.staff_member_id` (VARCHAR, non peuplé). Migration 045
ajoute `pos_upsell_enabled`. Le canal (`POS`/`SNO`/`KIOSK`) est déjà injecté
dans `upsell.Service.GenerateUpsell` (`ChannelPOS`, `ChannelSNO`,
`ChannelKiosk` — [internal/modules/upsell/types.go:23-25](../../internal/modules/upsell/types.go#L23)).

Un audit antérieur ([docs/audits/2026-07-01-upsell-v2.md](2026-07-01-upsell-v2.md))
avait constaté que le SNO n'avait ni panier contextuel ni tracking. Cet état a
changé depuis : `PostUpsellSNO` (cart-aware, même moteur que POS/Kiosk) et le
transport de `upsell_suggestion_id` côté SNO existent désormais. Le présent
audit se concentre spécifiquement sur `is_upsell` (ligne de commande), pas sur
`upsell_suggestions` en général.

---

## A. API Go — chemins d'écriture orderitems

**Un seul point d'INSERT dans toute la codebase** — vérifié par grep exhaustif
`INSERT INTO orderitems` sur tout le repo :

- [internal/modules/order_life_cycle/repository.go:1298](../../internal/modules/order_life_cycle/repository.go#L1298) —
  `INSERT INTO orderitems (order_item_id, order_id, product_id, merchant_id, quantity, discount_id, base_price, price, delay_id, is_upsell, ordered_on) ... ON DUPLICATE KEY UPDATE ... is_upsell = VALUES(is_upsell) ...` (ligne 1308). Colonne présente en INSERT **et** en clause `ON DUPLICATE KEY UPDATE`.
- [internal/modules/order_life_cycle/repository.go:1974](../../internal/modules/order_life_cycle/repository.go#L1974) —
  second point d'insertion (probablement `UpdateOrder`/ajout de ligne a
  posteriori), colonne `is_upsell` également présente, valeur lue depuis
  `item.IsUpsell`.

✅ **CÂBLÉ** — `is_upsell` est présent dans les deux requêtes SQL qui écrivent
`orderitems`, alimenté par `p.IsUpsell` / `item.IsUpsell`.

**Struct portant le flag côté API** :
- `models.OrderProductPayload` (DTO entrant, JSON) —
  [internal/models/create_order_models.go:97](../../internal/models/create_order_models.go#L97) :
  `IsUpsell bool `json:"is_upsell"``. Champ bien lu par le désérialiseur JSON
  et transporté (pas seulement décodé puis abandonné) jusqu'à `orderitems`
  (confirmé par la présence de `p.IsUpsell` dans la requête SQL ci-dessus).
- `models.ProductEntry` (DTO interne pour le calcul upsell/menu) —
  [internal/models/orders_model.go:33](../../internal/models/orders_model.go#L33) :
  `IsUpsell bool // true when this line was added from an upsell suggestion`.

**Canal-spécifique — un seul point d'entrée `CreateOrder`** : tous les
handlers (POS `OrdersHandler`, Kiosk `kiosk.Service.CreateOrder`
([internal/modules/kiosk/service.go:1374-1446](../../internal/modules/kiosk/service.go#L1374)),
ScanNOrder `scannorder.Service.CreateOrderSNO`, et les deux webhooks Uber Eats
/ Deliveroo) délèguent in fine à
`OrdersLifeCycleService.CreateOrder`, qui appelle le repository ci-dessus.
Il n'existe donc **pas** de chemin d'écriture alternatif contournant
`is_upsell` — la colonne est disponible pour tous les canaux au niveau API,
**à condition que le payload entrant porte réellement `is_upsell: true` par
item** (voir sections C/D/E : ce n'est vrai que côté POS).

✅ **CÂBLÉ** (mécanisme générique, unique point d'écriture, colonne présente
partout).

---

## B. upsell_suggestions / Tracker

Lecture complète de
[internal/modules/upsell/tracker.go](../../internal/modules/upsell/tracker.go).

- **`TrackAsync(parentCtx, suggestionID, merchantID, orderID, finalProducts)`**
  (lignes 36-170) : fire-and-forget, goroutine isolée avec son propre contexte
  10s, `recover()` déferré. No-op si `suggestionID == ""` (ligne 43-45).
- **order_id** : ✅ peuplé — `capturedOrderID` transmis à
  `RecordAcceptance(ctx, ..., RecordAcceptanceParams{OrderID: capturedOrderID, ...})`
  (ligne 147-151).
- **accepted_items** : ✅ peuplé — reconstruit en croisant `finalProducts`
  (produits réellement commandés) avec `suggestion.SuggestedItems` (lignes
  98-137) : seuls les produits présents dans les deux ensembles sont comptés,
  avec quantité réelle si `ProductEntry.Quantity` est renseigné, sinon
  comptage par occurrence.
- **staff_member_id** : ❌ **NON CÂBLÉ** — grep exhaustif de `StaffMemberID`
  dans `internal/modules/upsell/` : **aucun résultat**. La colonne existe en
  base (migration 044) mais n'est jamais lue ni écrite par
  `Tracker`/`Repository`/`Service`. Confirmé par l'audit antérieur
  ([2026-07-01-upsell-v2.md:135](2026-07-01-upsell-v2.md)) : *"staff_member_id
  ... préparé pour un futur funnel serveur, non peuplé actuellement"*.
- **revenue_impact** : ✅ **calculé**, pas toujours 0 — somme
  `quantity × unitPrice` sur les `acceptedItems`, convertie en euros
  (lignes 139-144). Note : `unitPrice` vient de `pe.Price` (prix du panier
  final), pas du prix de la suggestion d'origine (`suggestedPrices` est
  construit ligne 100 mais **jamais utilisé** dans le calcul de
  `revenueImpact` — variable morte, incohérence mineure si le prix a changé
  entre suggestion et commande, ex. remise appliquée).

**Lien `suggestion_id` → commande** : `RequestObject.UpsellSuggestionID`
([internal/models/request_objects.go:237](../../internal/models/request_objects.go#L237),
alias [create_order_models.go:9](../../internal/models/create_order_models.go#L9))
est le champ unique de transport, réutilisé identiquement par les trois
canaux :
- POS : (à confirmer côté handler `orders`, non lu explicitement dans cet
  audit mais `OrdersLifeCycleService.CreateOrder` le consomme génériquement)
- Kiosk : [internal/modules/kiosk/service.go:1426](../../internal/modules/kiosk/service.go#L1426) — `UpsellSuggestionID: req.UpsellSuggestionID`
- ScanNOrder : [internal/modules/scannorder/service.go:832](../../internal/modules/scannorder/service.go#L832) — `UpsellSuggestionID: req.UpsellSuggestionID`

`OrdersLifeCycleService.CreateOrder`
([internal/modules/order_life_cycle/service.go:963-987](../../internal/modules/order_life_cycle/service.go#L963)) :
`if s.upsellTracker != nil && req.UpsellSuggestionID != nil && *req.UpsellSuggestionID != "" { ... s.upsellTracker.TrackAsync(...) }` — générique, tous canaux confondus, câblé une seule fois.

✅ **CÂBLÉ** pour `order_id`/`accepted_items`/`revenue_impact`/lien
`suggestion_id`→commande. ❌ **NON CÂBLÉ** pour `staff_member_id`.

**Appels de `Tracker`** — grep `Tracker`/`TrackAsync` sur tout le repo : un
seul site d'instanciation ([cmd/api/routes.go:241](../../cmd/api/routes.go#L241))
et un seul site d'appel ([order_life_cycle/service.go:980](../../internal/modules/order_life_cycle/service.go#L980)),
partagé par tous les canaux via `CreateOrder`. Pas de duplication, pas
d'oubli d'un canal au niveau de l'appel du tracker lui-même.

---

## C. POS Flutter (wello_resto_flutter)

- **`UpsellSuggestionsBar`**
  ([lib/ui/widgets/home/upsell/upsell_suggestions_bar.dart:244,282](../../../wello_resto_flutter/lib/ui/widgets/home/upsell/upsell_suggestions_bar.dart)) :
  `return product.copyWith(isUpsell: true);` et un second site
  `isUpsell: true` — le produit ajouté au panier depuis la barre de
  suggestion porte explicitement le flag.
- **`ProductDto`**
  ([lib/models/menu/products/product_dto.dart:55,102,203,252,307](../../../wello_resto_flutter/lib/models/menu/products/product_dto.dart)) :
  champ `final bool isUpsell` par défaut `false`, propagé par `copyWith`.
- **`ProductPayload.toJson()`**
  ([lib/data/api/payload/order/product_payload.dart:19,33,65,81](../../../wello_resto_flutter/lib/data/api/payload/order/product_payload.dart)) :
  ```dart
  final bool isUpsell; // true when this line was added from an upsell suggestion
  ...
  this.isUpsell = false,
  ...
  isUpsell: dto.isUpsell,
  ...
  'is_upsell': isUpsell,
  ```
  → sérialisé sous la clé `is_upsell`, exactement le tag JSON attendu côté API
  (`models.OrderProductPayload.IsUpsell`).

✅ **CÂBLÉ** — chemin tap upsell → `isUpsell: true` → `is_upsell` JSON →
`orderitems.is_upsell`. Le chemin normal (tap menu classique) instancie le
produit avec `isUpsell = false` par défaut ; la seule différence entre les
deux chemins est effectivement cette valeur (rien d'autre ne diverge dans
`ProductPayload`).

**Edge case — même produit ajouté via upsell puis quantité incrémentée via le
menu classique** : non tranché avec certitude dans cet audit (lecture des
seuls fichiers listés dans le périmètre, pas du contrôleur de panier complet
gérant la fusion de lignes). Deux hypothèses possibles selon l'implémentation
du merge de lignes identiques dans le panier POS :
1. Si le merge se fait par `product_id` + configuration identique sans
   comparer `isUpsell`, la ligne existante marquée `isUpsell = true`
   conserverait probablement son flag (l'incrément touche `quantity`, pas
   `isUpsell`) — comportement correct.
2. Si un nouvel item est recréé à chaque tap (`copyWith` sans report explicite
   de `isUpsell`), le flag pourrait être perdu ou, à l'inverse, un item
   normal pourrait hériter à tort du flag d'un item upsell fusionné.

⚠️ **NON VÉRIFIÉ AVEC CERTITUDE** — nécessiterait la lecture du contrôleur de
panier (`cart_controller`/équivalent POS, hors des fichiers explicitement
listés dans le brief pour ce repo) pour trancher formellement le comportement
de merge. Recommandé comme point de vérification manuelle avant mise en
prod à grande échelle.

---

## D. Kiosk Flutter (wello-kiosk)

Le Kiosk **a bien un flux upsell propre**, distinct du POS :

- `lib/presentation/screens/upsell_screen.dart`,
  `lib/presentation/controllers/upsell_controller.dart`,
  `lib/data/repositories/upsell_repository.dart`,
  `lib/data/models/upsell_suggestion.dart`.
- `UpsellController.loadSuggestions()`
  ([lib/presentation/controllers/upsell_controller.dart:40-62](../../../wello-kiosk/lib/presentation/controllers/upsell_controller.dart#L40))
  appelle `_upsellRepository.getUpsell(cartProductIds)`, capture
  `response.suggestionId` dans `_currentBatchSuggestionId`.
- `recordSuggestionTap()` (lignes 71-74) : capture le **batch** de suggestion
  au moment du tap (`_lastAcceptedSuggestionId = _currentBatchSuggestionId`),
  pas au moment de l'ajout effectif au panier — limitation documentée dans le
  code lui-même : "dernière acceptée gagne" si plusieurs batches sont
  acceptés dans la même commande.
- `addSuggestionToCart()` (lignes 82-84) : `cart.addItem(suggestion.product ?? suggestion.toProduct(), const [])` — **aucun paramètre `isUpsell` n'est transmis à `addItem`**.
- `order_controller.dart` (ligne 92) : `upsellSuggestionId: upsell.lastAcceptedSuggestionId` — le **suggestion_id de batch** est bien transmis à la création de commande.

**Vérification directe** : grep de `isUpsell`/`is_upsell` sur
`lib/data/models/order.dart` et `lib/presentation/controllers/cart_controller.dart`
→ **aucun résultat**. Il n'existe **aucun champ `is_upsell`** dans le modèle
d'item de commande Kiosk, et `CartController` ne le sérialise donc jamais
dans le payload envoyé à `POST /kiosk/orders` (`models.RequestObject` côté
API, décodé par `kiosk.Handler.CreateKioskOrder`
([internal/modules/kiosk/handler.go:321-355](../../internal/modules/kiosk/handler.go#L321))).

❌ **NON CÂBLÉ** — le Kiosk a un flux upsell fonctionnel côté UI et un lien
`upsell_suggestions` → commande (`suggestion_id` transmis, donc `Tracker`
fonctionne pour `accepted_items`/`revenue_impact`), **mais aucune ligne de
commande Kiosk n'aura jamais `orderitems.is_upsell = 1`**, car
`OrderProductPayload.IsUpsell` (côté API, attend `is_upsell` en JSON) n'est
jamais rempli côté client Kiosk. Écart direct avec le POS, alors que le
canal API (`OrderProductPayload`) le supporte déjà nativement.

**Handler API recevant les commandes Kiosk** :
`kiosk.Handler.CreateKioskOrder` → `kiosk.Service.CreateOrder`
([internal/modules/kiosk/service.go:1374](../../internal/modules/kiosk/service.go#L1374)) → `ordersService.ComputePricing` puis `ordersLifeCycleSvc.CreateOrder` (même point d'entrée générique que la section A) — donc l'écriture SQL fonctionnerait immédiatement si le client Flutter envoyait le flag.

---

## E. ScanNOrder React (wello-resto-scannorder)

Le flux upsell existe et est plus abouti que ne le suggérait l'audit
antérieur (2026-07-01) — `PostUpsellSNO` (contextuel au panier, même moteur
que POS/Kiosk) est désormais actif côté API
([internal/modules/scannorder/handler.go:261-279](../../internal/modules/scannorder/handler.go#L261)),
et le lien `suggestion_id` → commande **est câblé** :

- `UpsellPopup.tsx` (lignes 83-110) : appelle `useUpsell(slug, upsellBody)`
  (POST cart-aware), et si `apiUpsell?.suggestion_id` existe, appelle
  `setUpsellSuggestionId(apiUpsell.suggestion_id)` (ligne 110).
- `store/cart.ts` (lignes 19, 40) : `setUpsellSuggestionId` stocke l'ID dans
  le store Zustand (`upsellSuggestionId`).
- `lib/api/payload.ts` (ligne 141) : `upsell_suggestion_id: upsellSuggestionId ?? undefined` — bien inclus dans le payload de création de commande envoyé à l'API.
- Côté API : `scannorder.Service` — ligne 832 : `UpsellSuggestionID: req.UpsellSuggestionID` transmis à `OrdersLifeCycleService.CreateOrder`, donc `Tracker.TrackAsync` fonctionne pour SNO exactement comme pour Kiosk/POS.

⚠️ **PARTIEL** pour le lien `upsell_suggestions` (câblé au niveau batch/commande).

**`is_upsell` par item** : grep exhaustif de `isUpsell`/`is_upsell` sur
`src/lib/api/payload.ts` et `src/store/cart.ts` → **aucun résultat**. La
fonction de conversion `CartItem → PricingProductItem`
([src/lib/api/payload.ts:28-55](../../../wello-resto-scannorder/src/lib/api/payload.ts#L28))
ne construit que `product_id`, `quantity`, `comment`, `configuration` — pas
de champ `is_upsell`. Aucune trace dans le modèle `CartItem` d'un flag
équivalent à `isUpsell` du POS/du modèle backend.

❌ **NON CÂBLÉ** — même diagnostic que Kiosk : le lien `upsell_suggestions` ↔
commande fonctionne (au niveau batch, via `suggestion_id`), mais aucune ligne
`orderitems` créée depuis ScanNOrder n'aura jamais `is_upsell = 1`.

**Handler API recevant les commandes ScanNOrder** :
`scannorder.Handler.CreateOrderSNO` → `scannorder.Service.CreateOrderSNO` →
`orderLifeCycleSvc.CreateOrder` (même point d'entrée générique, section A).

---

## F. Webhooks (Uber Eats, Deliveroo)

Grep exhaustif de `INSERT INTO orderitems` sur tout le repo confirme qu'il
n'existe **qu'un seul point d'insertion SQL** (section A), partagé par tous
les canaux. Les fichiers propres à Uber Eats/Deliveroo
(`internal/webhook/ubereats/repository/orders_repo.go`,
`internal/modules/deliveroo/repository.go`) ne contiennent que des `UPDATE`
(distribution, `isDistributed`), jamais d'`INSERT` dédié.

Confirmé par grep : `internal/webhook/deliveroo_orders/service.go` et
`internal/webhook/ubereats/service/order_notification.go` appellent la même
`CreateOrder` de `OrdersLifeCycleService` (ou `OrdersLifeCycleService`
équivalent injecté) que les autres canaux.

➖ **NON APPLICABLE (attendu)** — `models.OrderProductPayload.IsUpsell` n'est
jamais renseigné dans les payloads construits depuis les webhooks Uber
Eats/Deliveroo (structures reconstruites depuis les DTO externes, sans champ
upsell). La colonne `is_upsell` a `DEFAULT 0` (migration 044) et la requête
INSERT liste explicitement la colonne avec une valeur Go `bool` zero-value
(`false`) — aucune erreur SQL, comportement correct : ces commandes
apparaissent normalement avec `is_upsell = 0` sur toutes leurs lignes, ce qui
est le résultat attendu (pas d'upsell in-app sur ces canaux).

---

## G. Analytics

**`GET /stats/upsell` (ou équivalent)** —
[internal/modules/stats/repository.go:356-363](../../internal/modules/stats/repository.go#L356) :
```sql
WHERE oi.is_upsell = 1
  AND o.merchant_id = ?
  AND o.creation_date >= ?
  AND o.creation_date < ?
  AND o.state IN ('CLOSED', 'DONE')
  AND o.brand_status NOT IN ('DELETED', 'CANCELED')
```
✅ **CÂBLÉ correctement** :
- Filtre `is_upsell = 1` présent.
- Filtre `state IN ('CLOSED', 'DONE')` — exclut bien les commandes non
  finalisées.
- Filtre `brand_status NOT IN ('DELETED', 'CANCELED')` — exclut bien les
  commandes annulées/supprimées.
- Scoping `merchant_id` correct (pas de fuite cross-tenant).

Étant donné les gaps constatés en C/D/E, cette requête d'analytics ne
retournera en pratique **que des lignes issues du canal POS** (seul canal où
`is_upsell` est effectivement écrit à `1`), même si Kiosk et ScanNOrder ont
des flux upsell actifs et générateurs de `upsell_suggestions` — sous-estimation
silencieuse du volume d'upsell réel sur ces deux canaux, sans erreur visible.

**Autres lecteurs de `is_upsell`** : recherche limitée à
`internal/modules/stats/` (seul module identifié consommant la colonne pour
de l'analytics) — pas d'autre point de lecture identifié dans le périmètre de
cet audit.

---

## Vérification externe — Uber Eats / Deliveroo metadata custom

Impossible de vérifier — pas d'accès internet dans cet environnement.

---

## Synthèse

| Canal       | Upsell affiché ? | is_upsell émis ? | is_upsell persisté ? | upsell_suggestions lié ? | Statut |
|-------------|-------------------|-------------------|------------------------|-----------------------------|--------|
| POS         | ✅ Oui (`UpsellSuggestionsBar`) | ✅ Oui (`ProductPayload.toJson()` → `is_upsell`) | ✅ Oui (colonne alimentée) | ✅ Oui (`UpsellSuggestionID` → `Tracker`) | ✅ **CÂBLÉ** (edge case merge quantité non vérifié avec certitude) |
| Kiosk       | ✅ Oui (`upsell_screen.dart`, `UpsellController`) | ❌ Non (aucun champ `isUpsell` dans `order.dart`/`cart_controller.dart`) | ❌ Non (jamais `1`) | ✅ Oui (`upsellSuggestionId` transmis à `order_controller`) | ⚠️ **PARTIEL** — suggestion trackée au niveau batch, mais aucune ligne ne porte `is_upsell` |
| ScanNOrder  | ✅ Oui (`UpsellPopup.tsx`, `PostUpsellSNO` cart-aware) | ❌ Non (aucun champ `is_upsell` dans `payload.ts`/`cart.ts`) | ❌ Non (jamais `1`) | ✅ Oui (`upsell_suggestion_id` dans le payload de commande) | ⚠️ **PARTIEL** — même diagnostic que Kiosk |
| Uber Eats   | ➖ N/A (pas de flux upsell in-app) | ➖ N/A | ➖ N/A (`DEFAULT 0`, pas d'erreur) | ➖ N/A | ➖ **NON APPLICABLE** (attendu) |
| Deliveroo   | ➖ N/A (pas de flux upsell in-app) | ➖ N/A | ➖ N/A (`DEFAULT 0`, pas d'erreur) | ➖ N/A | ➖ **NON APPLICABLE** (attendu) |

## Gaps priorisés (impact = perte de traçabilité pondérée par volume de commandes)

1. **[Impact élevé] Kiosk : `is_upsell` jamais transmis côté item.**
   `CartController.addItem` (Kiosk) ne reçoit ni ne sérialise de flag
   `isUpsell` — contrairement au POS qui a exactement le même besoin déjà
   résolu (`ProductDto.isUpsell` → `ProductPayload.toJson()`). Le canal
   `OrderProductPayload.IsUpsell` côté API attend déjà ce champ : c'est un
   correctif **frontend uniquement** (ajouter le champ au modèle d'item Kiosk
   + le répercuter dans `addSuggestionToCart` et dans le payload JSON envoyé
   à `POST /kiosk/orders`). Le Kiosk étant un canal à fort volume potentiel
   (self-service, plusieurs bornes par établissement), c'est le gap le plus
   impactant en perte de données analytiques.

2. **[Impact élevé] ScanNOrder : `is_upsell` jamais transmis côté item.**
   Même diagnostic exact que Kiosk : `UpsellPopup.tsx` ajoute le produit
   suggéré au panier via `addItem` du store Zustand sans marqueur, et
   `payload.ts` ne sérialise jamais de champ `is_upsell`. Canal
   potentiellement très volumétrique (self-service client, sans staff),
   donc perte de traçabilité proportionnellement importante.

3. **[Impact moyen] `staff_member_id` jamais peuplé sur `upsell_suggestions`.**
   Colonne créée par la migration 044 mais entièrement inerte (aucune
   référence dans `internal/modules/upsell/`). Documenté comme "dette
   assumée" dans les commentaires de migration, mais bloque tout funnel
   d'attribution par serveur (POS) tant que non câblé — n'affecte pas
   `is_upsell`/`orderitems` directement mais limite l'analytics upsell côté
   staff.

4. **[Impact faible] `revenueImpact` dans `Tracker.TrackAsync` utilise le
   prix du panier final (`pe.Price`), pas le prix au moment de la
   suggestion (`suggestedPrices`, calculé mais jamais utilisé,
   [tracker.go:100](../../internal/modules/upsell/tracker.go#L100)).**
   Écart mineur si une remise ou un changement de prix intervient entre la
   suggestion et la validation de la commande — variable morte à nettoyer ou
   logique à corriger selon l'intention métier voulue (revenu réellement
   perçu vs revenu attribuable à la suggestion).

5. **[Impact faible / à vérifier] POS — comportement du flag `isUpsell` en
   cas de fusion d'une ligne upsell avec un ajout ultérieur via le menu
   classique (même produit, même configuration).** Non tranché avec
   certitude dans le périmètre de fichiers audité (contrôleur de panier
   POS complet non lu) — à vérifier manuellement avant de considérer le
   POS comme 100% fiable en `is_upsell`, bien que le mécanisme de base soit
   correctement câblé.

6. **[Impact nul, cohérence à surveiller] Analytics `stats/upsell`
   correctement filtré (`is_upsell=1`, statuts, merchant_id) mais sous-estime
   silencieusement le volume réel dès lors que Kiosk/ScanNOrder génèrent de
   l'upsell accepté sans que la ligne `orderitems` correspondante porte le
   flag** — conséquence directe des gaps 1 et 2, pas un bug de la requête
   elle-même.
