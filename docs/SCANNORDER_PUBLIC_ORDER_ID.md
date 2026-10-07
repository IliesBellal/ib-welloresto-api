# ScanNOrder — id public des commandes dans les liens clients

Tous les liens de suivi ScanNOrder envoyés ou affichés au client final
(redirection Stripe, mail et SMS de confirmation, SMS de suivi livraison, QR
code de la borne) portent désormais `orders.public_id` au lieu de l'`order_id`
interne séquentiel. Objectif : supprimer l'énumération des commandes via
`GET /scannorder/{slug}/orders/{id}` (motivation d'origine de la migration
`033_orders_public_id`).

## 1. Décisions

| # | Décision | Pourquoi |
|---|---|---|
| D1 | **Bascule en deux phases.** Phase 1 (ce lot) : l'API accepte l'id public **et** l'id interne sur les routes publiques ScanNOrder, et émet partout l'id public. Phase 2 (lot suivant) : l'id interne est refusé sur ces routes. | Couvre le déploiement : sessions Stripe en cours (expiration 30 min), fronts pas encore redéployés, liens déjà envoyés. Les liens de suivi n'ont d'utilité que quelques heures, la phase 2 peut suivre rapidement. |
| D2 | **La borne entre dans le chantier** (QR code renvoyé par l'API et QR imprimé par l'app wello-kiosk). | Lien client au même titre que le mail. |
| D3 | **Les liens déjà cassés sont corrigés au passage** : SMS de suivi livraison sans segment `/order/`, QR borne en `/restaurants/` (pluriel), redirection Stripe `partial_order` sans `/`. | Validé avec le PO. |
| D4 | **Plus aucune adresse en dur** (`*.onrender.com` ou autre) : tous les liens partent de `SCANNORDER_BASE_URL`, avec repli sur `https://scannorder.welloresto.fr` si la variable est vide. | Le mail et le SMS de confirmation pointaient en dur vers `wello-resto-scannorder-prod.onrender.com`, y compris depuis staging. **À vérifier sur Render** : `SCANNORDER_BASE_URL` ne doit pas valoir une adresse `.onrender.com`. |
| D5 | Le format existant `order--<uuid>` (double tiret, `GeneratePrefixedID("order-")` ajoute déjà un `-`) est **conservé**. | Cosmétique ; le changer créerait deux formats en base. |
| D6 | L'id public est **résolu à l'entrée** des routes publiques (`public_id` + `merchant_id` → `order_id`), le reste de la chaîne (cache Redis, `ComputeGetOrder`, annulation, remboursement) continue de travailler avec l'id interne. | Changement minimal et localisé ; filtrer aussi sur le marchand empêche d'atteindre la commande d'un autre établissement avec le bon `public_id`. |
| D7 | Les metadata Stripe (`order_id`) restent l'id interne. | Le webhook Stripe s'en sert ; jamais exposées au client. |
| D8 | Le SMS de confirmation affiche `order_num` (numéro de retrait) au lieu de l'id interne. | L'id interne n'a aucun sens pour le client. |
| D9 | ~~L'app borne utilise le `qr_payload` renvoyé par l'API.~~ **Révisée en cours de lot** : l'API ajoute `tracking_url` à `GET /kiosk/orders/{id}` et l'app l'utilise pour le QR d'écran et le QR du ticket. | `qr_payload` n'existe que dans la réponse counter-payment : le parcours carte (Stripe Terminal) n'y passe jamais et n'aurait eu aucun lien. L'app relit toujours `GET /kiosk/orders/{id}` après création, c'est donc le seul point commun aux deux parcours. Une seule source de vérité pour la forme du lien est conservée. |
| D10 | Un id public inconnu, ou appartenant à un autre marchand que le QR de l'URL, répond **404 `not_found`** sur `GET /scannorder/{slug}/orders/{id}` (au lieu de l'ancien 500 / panic sur une liste vide). | Comportement attendu pour un lien invalide. Les autres erreurs gardent leur forme 500 historique. |
| D11 | Pas de lien plutôt qu'un lien cassé : sans id public ou sans slug, le mail part avec un bouton sans lien (warning journalisé), le SMS de suivi n'est pas envoyé (erreur journalisée), la borne n'affiche/n'imprime pas de QR, la session Stripe n'est pas créée (`error_004`). | Ne concerne en pratique que des commandes antérieures au 2026-06-18, donc aucun flux réel. |

## 2. Implémentation

### 2.1 Base de données

| Fichier | Changement |
|---|---|
| `migrations/done/163_orders_public_id_unique_index.{up,down}.sql` | `CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_public_id ON orders (public_id)`, hors transaction. |

État constaté sur staging le 2026-10-03 : colonne `public_id varchar(64)`, **ni index ni trigger**
(le trigger `trg_orders_public_id` décrit dans `docs/DELIVERY_DESIGN.md` n'existe pas en Postgres :
l'id est généré côté Go dans `insertOrderBase`, unique chemin d'insertion en production). 33 867
commandes, 2 975 avec `public_id`, toutes distinctes ; toutes les commandes créées depuis le
2026-06-18 en ont un.

### 2.2 API (ib-welloresto-api)

| Couche | Fichier | Changement |
|---|---|---|
| Helper | `internal/helpers/scannorder_links.go` (+ test) | `ScanNOrderOrderURL(base, slug, publicID)`, `ScanNOrderBaseURL(base)` (repli `DefaultScanNOrderBaseURL`), `IsOrderPublicID(ref)`. |
| Helper | `internal/helpers/ids.go` | Constante `OrderPublicIDPrefix = "order-"`. |
| Création | `order_life_cycle/repository.go` | `insertOrderBase` pose `req.Order.PublicID` ; `CreateOrderResult.PublicID` renseigné. |
| Modèles | `models/create_order_models.go` | `OrderRequest.PublicID` (`json:"-"`, jamais lu du client) ; `CreateOrderResult.PublicID` (`public_id`). |
| Modèles | `models/orders_model.go`, `orders/orders_fetcher_builder.go` | `Order.PublicID` (`public_id`, nullable) lu dans la requête d'en-tête. Le cache Redis (TTL 10 min) peut servir quelques minutes des commandes sans le champ après déploiement : sans impact, la résolution ne passe pas par lui. |
| ScanNOrder | `scannorder/repository.go` | `GetOrderIDByPublicID(qr, publicID)` : jointure `qrcodes` → filtre marchand. |
| ScanNOrder | `scannorder/service.go` | `resolveOrderRef` (phase 1 : id interne laissé passer, `TODO phase 2`) en tête de `GetOrderSNO` et `CancelOrderSNO` ; garde `len(Orders) == 0` → `ErrNotFound` ; `order.PublicID` transmis au checkout. |
| ScanNOrder | `scannorder/handler.go` | `ErrNotFound` → 404. |
| Stripe | `infrastructure/stripe/checkout.go` | `success_url`/`cancel_url` via `ScanNOrderOrderURL` (id public) ; `partial_order` : `/` manquant corrigé. Metadata `order_id` inchangées. |
| Webhook Stripe | `webhook/stripe/{service,repository,models}.go` | `GetOrder` lit `public_id`, `order_num` ; lien du mail et du SMS via `ScanNOrderOrderURL` (fini l'adresse `.onrender.com` en dur) ; SMS : `order_num` au lieu de l'id interne ; `SetScanNOrderBaseURL`. |
| SMS suivi | `merchantsms/{service,marketing_repository,models}.go` (+ test) | `GetOrderTrackingRef` (public_id + order_num, filtré marchand) ; lien `/order/{id public}` (segment `/order/` manquant corrigé) ; `{order_id}` du modèle remplacé par le numéro de retrait ; `SetScanNOrderBaseURL`. Contrat `SMSService` inchangé (delivery_sessions n'a pas bougé). |
| Borne | `kiosk/{service,models,config}.go` | `orderTrackingURL` ; `qr_payload` : id public, `/restaurant/` (pluriel corrigé), base configurable ; `KioskOrderResponse.TrackingURL` (`tracking_url`) ; `Config.ScanNOrderBaseURL`. |
| Câblage | `cmd/api/routes.go` | `SNORedirectBaseURL` passé au webhook Stripe, à merchantsms et à la config kiosk. |

### 2.3 Front ScanNOrder (wello-resto-scannorder)

| Fichier | Changement |
|---|---|
| `src/lib/api/types.ts` | `CreateOrderResponse.public_id?`, `Order.public_id`. |
| `src/components/cart/CheckoutFlow.tsx` | Navigation et `saveActiveOrder` avec `public_id` (repli `order_id` en phase 1) ; affichage « Commande n° » sur `order_num` seul. |
| `src/components/catalogue/ActiveOrderBanner.tsx` | Affiche `#order_num` au lieu de l'id (devenu un uuid). |
| `src/store/orderContext.ts` | Commentaire : `orderId` stocké = id public. |
| Tests `OrderInfoPanel.test.tsx`, `orderStatusSteps.test.ts` | Fixtures complétées (`public_id`). |

Non touchés : `useOrder`/`useCancelOrder` (relaient l'id de l'URL), `PaymentReturnPage` (relaie
`order_id` de la query ou la commande active), `fetchOrderStatus` de `src/lib/api.ts` (mort, non
importé).

### 2.4 App borne (wello-kiosk)

| Fichier | Changement |
|---|---|
| `lib/data/models/order.dart`, `order.g.dart` | `OrderResponse.trackingUrl` (`tracking_url`). `.g.dart` édité à la main au format du générateur : `build_runner` aurait régénéré d'autres fichiers en cours de modification. |
| `lib/presentation/controllers/order_controller.dart` | QR du ticket = `order.trackingUrl` ; constante d'URL en dur supprimée. Le paramètre `slug` reste dans la chaîne d'appel (plus utilisé pour le QR) — nettoyage laissé pour ne pas toucher aux écrans. |
| `lib/presentation/screens/confirmation_screen.dart` | QR d'écran = `order.trackingUrl`, masqué s'il est vide (l'ancien repli `restaurant/order/{id}` était un lien cassé). |
| `lib/data/models/kiosk_settings.dart` | Commentaire du `slug` mis à jour. |

## 3. Déploiement

1. **Vérifier `SCANNORDER_BASE_URL` sur Render** (staging et prod) : doit valoir l'adresse du front
   ScanNOrder de l'environnement, jamais une adresse `.onrender.com` (D4). Vide = repli
   `https://scannorder.welloresto.fr`.
2. **Migration 163** sur chaque base, hors transaction.
3. **API** (rétrocompatible : accepte les deux ids, ajoute des champs).
4. **Front ScanNOrder** (fonctionne aussi contre l'ancienne API grâce au repli `order_id`).
5. **App borne** (sans `tracking_url`, l'ancienne API ne donne simplement pas de QR).
6. **Phase 2** (lot séparé), une fois 4 et 5 en production et après expiration des sessions Stripe
   en cours :
   - `resolveOrderRef` : refuser l'id interne (`models.ErrNotFound`) — `TODO phase 2` dans
     `scannorder/service.go` ;
   - front : retirer le repli `res.order_id` dans `CheckoutFlow.tsx` ;
   - envisager de retirer `order_id` de la réponse publique `GET /scannorder/{slug}/orders/{id}`.

## 4. Statut d'exécution

- **API** : `go build ./...` OK ; `go vet` OK sur les paquets touchés ; `go test` OK sur helpers,
  scannorder, kiosk, merchantsms, infrastructure/stripe, orders, order_life_cycle, integrations,
  cmd/api. `migrations` : sous-test `done` OK ; sous-test `todo` en échec **préexistant** (le
  dossier `migrations/todo/` n'existe plus).
- **Requêtes SQL nouvelles** exécutées en lecture seule sur **staging** : résolution par id public
  (bonne commande ; aucune ligne avec le QR d'un autre marchand), `GetOrder` du webhook,
  `GetOrderTrackingRef`. Tests d'intégration Postgres (`postgres_integration`) **non lancés**.
- **Migration 163 non appliquée** (ni staging ni prod).
- **Front ScanNOrder** : `vitest` 74/74 OK ; `tsc` OK hors erreurs `google` préexistantes
  (`TrackingMap.tsx`, `useMapBounds.ts`) ; `eslint` OK sur les fichiers touchés.
- **App borne** : `flutter analyze` OK sur les fichiers touchés ; `flutter test` 135/135 OK.
- **Non testé de bout en bout** : paiement Stripe réel, réception du mail/SMS, scan du QR borne.
- Rien n'est commité.
