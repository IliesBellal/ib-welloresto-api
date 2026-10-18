# Livrable 2 — Cartographie du moteur existant

Décrit uniquement ce qui a été lu dans le code au 2026-10-16 (branche `main`,
fichiers upsell sans modification locale). Les liens pointent vers les lignes
citées.

## 1. Vue d'ensemble

Le moteur est une **cascade de règles déterministes**, exécutée à la demande,
alimentée par un **calcul statistique nocturne**. Il n'apprend pas des
suggestions refusées ou acceptées : il apprend des ventes.

```
                      Nuit, 3h (cron)                                   Requête (POS / SNO / borne)
┌──────────────────────────────────────────────┐      ┌───────────────────────────────────────────────────────┐
│ RecomputeUpsellPatterns                      │      │ GenerateUpsell(merchant, panier, order_type, canal)   │
│  commandes CLOSED, 90 j, tous canaux         │      │  1. réglages marchand (enable_upsell, max_items)      │
│  • associations A→B par groupe de produits   │─────▶│  2. Redis absent → produits is_popular (secours)      │
│    (≥8 commandes ensemble, lift ≥1,2,        │Redis │  3. cache résultat 30 min (panier, type, canal)       │
│     P(B|A) lissée ≥10 %, top 10 par produit) │ 36 h │  4. candidats : catalogue dispo − panier − variantes  │
│  • « petits prix à succès » : prix ≤         │      │     sœurs − hors canal − hors horaires (SNO/borne)    │
│    médiane/3, vendus ≥1 fois, 30 au plus     │      │  5. associations : max P(B|A) sur le panier,          │
└──────────────────────────────────────────────┘      │     hors catégories déjà au panier                    │
                                                      │  6. complément « petits prix », ordre des ventes      │
                                                      │  7. complément LLM (désactivé en prod)                │
                                                      │  8. arrêt (liste éventuellement incomplète)           │
                                                      │  9. enrichissement fiche produit, 1 ligne en base     │
                                                      └───────────────────────────────────────────────────────┘
                                                                         │ suggestion_id + liste
                                                                         ▼
                              Filtres propres au canal (SNO, borne) → affichage client (6 au plus sur SNO/borne)
                                                                         │ ajout au panier : ligne is_upsell = true
                                                                         ▼
                 Création de commande avec upsell_suggestion_id (seulement si une suggestion a été ajoutée)
                                                                         │
                                                                         ▼
                    Tracker asynchrone : accepted_items = produits suggérés ∩ produits de la commande
```

## 2. Points d'entrée par canal

| | POS (caisse Flutter) | ScanNOrder (web) | Borne (Flutter) |
|---|---|---|---|
| Route | `POST /orders/upsell` ([handler.go:128](../../../internal/modules/orders/handler.go#L128)) | `POST /scannorder/{slug}/upsell` ([service.go:1609](../../../internal/modules/scannorder/service.go#L1609)) | `POST /kiosk/upsell` ([service.go:1631](../../../internal/modules/kiosk/service.go#L1631)) |
| Activation | `enable_upsell` (serveur) **et** `pos_upsell_enabled` (client) | `enable_upsell` | `enable_upsell` et `settings.upsellEnabled` (défaut `true` côté borne, [cart_screen.dart:64](../../../../wello-kiosk/lib/presentation/screens/cart_screen.dart#L64)) |
| Moment de l'appel | À **chaque changement de composition** du panier, après 800 ms ([upsell_controller.dart:49-71](../../../../wello_resto_flutter/lib/controllers/order/upsell_controller.dart#L49-L71)), **y compris sur téléphone**, où la barre n'est pas rendue ([menu_list_view.dart:275-276](../../../../wello_resto_flutter/lib/ui/views/home/menu_list_view.dart#L275-L276)) ; prod : 3,1 appels par commande chez 212 | **Une fois**, au clic sur « Passer la commande » ([useUpsell.ts:52-96](../../../../wello-resto-scannorder/src/hooks/useUpsell.ts#L52-L96)) | **Une fois**, avant le paiement ([cart_screen.dart:66-77](../../../../wello-kiosk/lib/presentation/screens/cart_screen.dart#L66-L77)) |
| Délai client | aucun | 2 s, puis checkout direct ([useUpsell.ts:11](../../../../wello-resto-scannorder/src/hooks/useUpsell.ts#L11)) | 2 s, puis paiement direct ([upsell_controller.dart:13](../../../../wello-kiosk/lib/presentation/controllers/upsell_controller.dart#L13)) |
| Filtres après moteur | aucun | horaires, disponibilité **par mode de commande**, fiche produit absente ([service.go:1649-1665](../../../internal/modules/scannorder/service.go#L1649-L1665)) | disponibilité borne **par mode**, horaires, panier ([service.go:1657-1695](../../../internal/modules/kiosk/service.go#L1657-L1695)) ; côté client : produit commandable, hors panier |
| Plafond affiché | `upsell_max_items` | **6 en dur** ([upsell.ts:6](../../../../wello-resto-scannorder/src/lib/upsell.ts#L6)) | **6 en dur** ([app_config.dart:180](../../../../wello-kiosk/lib/core/config/app_config.dart#L180)) |
| Affichage | Barre horizontale sous le menu (tablette), libellé « Proposé par IA », surlignage si score ≥ 0,8 ([upsell_suggestions_bar.dart:31,139](../../../../wello_resto_flutter/lib/ui/widgets/home/upsell/upsell_suggestions_bar.dart#L31)) | Popup « Un petit extra ? » : image, nom, prix, `+`/`-`, « Non merci » / « Continuer » ([UpsellPopup.tsx](../../../../wello-resto-scannorder/src/components/cart/UpsellPopup.tsx)) | Écran plein « Un petit extra ? », carte produit avec éventuel titre promo ([upsell_screen.dart:162-216](../../../../wello-kiosk/lib/presentation/screens/upsell_screen.dart#L162-L216)) |
| Accroche (`title`) | non affichée | non affichée | non affichée |
| Liste vide | barre vide | étape sautée | étape sautée |
| `upsell_suggestion_id` transmis | batch de la **dernière** suggestion tapée ; jamais effacé si l'article est retiré ([upsell_controller.dart:108-120](../../../../wello_resto_flutter/lib/controllers/order/upsell_controller.dart#L108-L120)) | si au moins une ligne upsell reste au panier ([UpsellPopup.tsx:50-88](../../../../wello-resto-scannorder/src/components/cart/UpsellPopup.tsx#L50-L88)) | idem, effacé quand plus aucune ligne upsell ([upsell_controller.dart:125-171](../../../../wello-kiosk/lib/presentation/controllers/upsell_controller.dart#L125-L171)) |
| Ligne `is_upsell` | oui | oui | oui |

## 3. Pipeline de décision, étape par étape

Fonction : `generateUpsellSafe` ([service.go:107-281](../../../internal/modules/upsell/service.go#L107-L281)).

| Étape | Règle | Code | Responsabilité |
|---|---|---|---|
| Garde | `recover` : toute panique donne une liste vide (`error_fallback`) | [L108-117](../../../internal/modules/upsell/service.go#L108-L117) | robustesse |
| Réglages | `enable_upsell` faux ou illisible → `disabled` ; `upsell_max_items` borné à 1-10, défaut 3 | [L124-134](../../../internal/modules/upsell/service.go#L124-L134), [repository.go:329-369](../../../internal/modules/upsell/repository.go#L329-L369) | politique marchand |
| Redis absent | produits `is_popular` (calculés la nuit), sans associations | [L136-143](../../../internal/modules/upsell/service.go#L136-L143) | repli |
| Cache | clé `merchant : SHA-256(ids distincts triés) : type de commande : canal`, 30 min ; **chaque lecture en cache crée une ligne** | [L148-175](../../../internal/modules/upsell/service.go#L148-L175), [L639-651](../../../internal/modules/upsell/service.go#L639-L651) | performance |
| Candidats | catalogue `available`/`enabled`/statut, jamais de groupe, variantes d'un groupe disponible ; moins panier, groupes et variantes sœurs du panier, hors horaires (SNO/borne), hors drapeau canal | [L181-219](../../../internal/modules/upsell/service.go#L181-L219), [menu/repository.go:139-216](../../../internal/modules/menu/repository.go#L139-L216) | génération + filtres |
| Associations | pour chaque produit du panier (groupe pour une variante), lecture Redis ; score candidat = **maximum** des P(B\|A) lissées ; candidat écarté si sa catégorie est déjà au panier ; association vers un groupe → **variante la moins chère** (à prix égal, la plus vendue) | [L226-243](../../../internal/modules/upsell/service.go#L226-L243), [L738-794](../../../internal/modules/upsell/service.go#L738-L794) | compatibilité + score |
| Classement | tri par score décroissant, puis id ; au plus `maxItems` | [L248](../../../internal/modules/upsell/service.go#L248), [L799-834](../../../internal/modules/upsell/service.go#L799-L834) | classement |
| Petits prix | places restantes remplies dans l'ordre de la liste nocturne ; score **constant 0,5** | [L251-254](../../../internal/modules/upsell/service.go#L251-L254), [L885-906](../../../internal/modules/upsell/service.go#L885-L906) | complément |
| LLM | places restantes, 50 candidats au plus **triés par nom**, délai 1,5 s retry compris, réponse validée (id existant) | [L259-266](../../../internal/modules/upsell/service.go#L259-L266), [L286-390](../../../internal/modules/upsell/service.go#L286-L390), [L928-938](../../../internal/modules/upsell/service.go#L928-L938) | complément (désactivé en prod) |
| Arrêt | pas d'élargissement aux produits chers (arbitrage *(c)*) ; liste vide non mise en cache | [L268-276](../../../internal/modules/upsell/service.go#L268-L276) | politique |
| Enrichissement | une lecture de fiche produit complète **par suggestion** | [L620-633](../../../internal/modules/upsell/service.go#L620-L633) | exposition |
| Persistance | une ligne `upsell_suggestions` (liste, source, canal, coût LLM éventuel) | [L557-613](../../../internal/modules/upsell/service.go#L557-L613), [repository.go:27-63](../../../internal/modules/upsell/repository.go#L27-L63) | traçabilité |

**Gestion des conflits entre règles** : il n'y a pas d'arbitrage par score entre
étapes. Une association, même faible, passe toujours devant un « petit prix »,
lui-même devant le LLM. La règle « pas de catégorie déjà au panier » ne vaut que
pour les associations ; les petits prix peuvent proposer une deuxième boisson.

## 4. Calcul nocturne

[internal/tasks/upsell.go](../../../internal/tasks/upsell.go), planifié à 3h
([cmd/api/tasks.go:61](../../../cmd/api/tasks.go#L61)), relançable par
`POST /admin/upsell/recompute-patterns`.

| Paramètre | Valeur | Code |
|---|---|---|
| Fenêtre | 90 jours, commandes `state = 'CLOSED'` | [L21](../../../internal/tasks/upsell.go#L21), [L88-98](../../../internal/tasks/upsell.go#L88-L98) |
| Périmètre des commandes | **tous canaux**, y compris Uber Eats et Deliveroo ; pas de filtre `brand_status` ; lignes `is_upsell` incluses | [L88-98](../../../internal/tasks/upsell.go#L88-L98) |
| Unité | produit groupe pour une variante (D1) | [L91](../../../internal/tasks/upsell.go#L91) |
| Co-occurrences minimales | 8 | [L22](../../../internal/tasks/upsell.go#L22) |
| Lift minimal | 1,2 | [L23](../../../internal/tasks/upsell.go#L23) |
| P(B\|A) lissée minimale | 10 %, avec β = 10 commandes : (n_AB + 10·n_B/N) / (n_A + 10) | [L24-25](../../../internal/tasks/upsell.go#L24-L25), [L228-231](../../../internal/tasks/upsell.go#L228-L231) |
| Associations gardées | 10 par produit, par P(B\|A) lissée | [L26](../../../internal/tasks/upsell.go#L26), [L257-268](../../../internal/tasks/upsell.go#L257-L268) |
| Petits prix | 0 < prix ≤ médiane des prix / 3, au moins une vente, tri ventes ↓ puis prix ↑, 30 au plus | [L31-32](../../../internal/tasks/upsell.go#L31-L32), [L360-399](../../../internal/tasks/upsell.go#L360-L399) |
| Durée de vie Redis | 36 h (deux nuits ratées = plus aucune association ni petit prix) | [L27](../../../internal/tasks/upsell.go#L27) |
| Purge des suggestions | 8 mois, le 1er du mois à 4h | [L28](../../../internal/tasks/upsell.go#L28), [cmd/api/tasks.go:72](../../../cmd/api/tasks.go#L72) |

## 5. Persistance, suivi et attribution

- **`upsell_suggestions`** : une ligne par appel au moteur (cache compris). Colonnes :
  liste suggérée (avec `origin` et `score` par article), `source`, `channel`,
  `cart_signature` (empreinte, pas le contenu du panier), métadonnées LLM,
  `order_id`, `accepted_items`, `revenue_impact`. **Ni le type de commande, ni le
  contenu du panier, ni la version du moteur, ni la latence totale** ne sont
  stockés. `staff_member_id` existe mais n'est jamais écrit.
- **Suivi** : à la création de commande seulement
  ([order_life_cycle/service.go:1164-1188](../../../internal/modules/order_life_cycle/service.go#L1164-L1188)),
  si le client envoie `upsell_suggestion_id`. Le Tracker
  ([tracker.go:36-170](../../../internal/modules/upsell/tracker.go#L36-L170))
  croise les produits suggérés avec ceux de la commande, par `product_id`,
  quantités cumulées, et écrit `revenue_impact` en euros **TTC**. Le suivi a
  lieu avant le paiement sur la borne et SNO, et n'est jamais révisé. Les
  ajouts à une commande existante (`UpdateOrder`) ne déclenchent aucun suivi,
  alors que le POS y envoie l'identifiant : prod, aucune acceptation POS suivie
  en 90 jours.
- **`orderitems.is_upsell`** : posé par les trois clients sur la ligne ajoutée
  depuis une suggestion. C'est la seule attribution au niveau de la ligne.
- **`orderitems.cost_price_unit`** : coût de revient unitaire figé à la vente,
  **uniquement pour les produits ayant une recette** chiffrée ; `NO_RECIPE` pour
  un produit revendu tel quel (migration 114, [cost.go:55-84](../../../internal/modules/order_life_cycle/cost.go#L55-L84)).
- **`api_request_logs`** : chaque requête est journalisée avec son corps, la
  réponse, le statut et la durée serveur, conservés 90 jours
  ([middleware.go:77-87](../../../internal/middleware/request_logger/middleware.go#L77-L87),
  [request_logs.go:16](../../../internal/tasks/request_logs.go#L16)). Les appels
  upsell y figurent : c'est aujourd'hui la seule trace du contenu des paniers
  exposés et de la latence réelle.

## 6. Restitution (Analytics)

[analytics/upsell.go](../../../internal/modules/analytics/upsell.go) :

- lignes `is_upsell` : nombre, CA HT et TTC, commandes avec au moins une ligne,
  rapporté au total des commandes ; top 10 produits ; classement par vendeur ;
- `upsell_suggestions` : « proposées » = nombre de lignes, « acceptées » = lignes
  dont `accepted_items IS NOT NULL` ([L427-448](../../../internal/modules/analytics/upsell.go#L427-L448)).

Aucune marge, aucun contrefactuel.

## 7. Configuration

| Réglage | Portée | Effet |
|---|---|---|
| `merchant_parameters.enable_upsell` | établissement | active le moteur (tous canaux). Prod 2026-10-16 : activé sur les 8 établissements ayant au moins 50 commandes sur 90 jours ; exposition réelle seulement chez 212, 226, 231 (SNO, borne) et 212, 241 (barre POS) |
| `merchant_parameters.upsell_max_items` | établissement | 1 à 10 (212 : 6). SNO et borne n'affichent jamais plus de 6 |
| `merchant_parameters.pos_upsell_enabled` | établissement | affiche la barre POS |
| `AI_TASK_UPSELL_ENABLED` / `_PROVIDER` / `_MODEL` / `_TEMPERATURE` / `_MAX_TOKENS` | global | LLM : coupé en prod ; défauts anthropic, `claude-haiku-4-5`, 0,5, 1024 ([ai.go:39-49](../../../internal/config/ai.go#L39-L49)) |
| Seuils des associations et des petits prix | constantes de code | globaux, non réglables par établissement |

## 8. Dépendances

PostgreSQL (catalogue, horaires, groupes, réglages, persistance), Redis
(associations, petits prix, cache résultat ; repli si absent), module menu
(`ListAvailableProductsForUpsell`, `GetProduct`), module disponibilités
(horaires), registre IA (LLM), cron (`TasksManager`), trois applications
clientes qui décident du moment, du nombre affiché et de la transmission de
l'identifiant de suggestion.

## 9. Zones insuffisamment documentées ou non vérifiées

| Zone | Pourquoi c'est important | Comment vérifier |
|---|---|---|
| Date de mise en prod de D1 à D13 et versions clientes en salle | fixe la fenêtre de référence | question 4 ; requête B1 (apparition de `source = low_price`) |
| Usage réel de la barre POS (tablette seulement) | le POS génère beaucoup d'appels mais peut-être peu d'expositions réelles | question 4 ; B1 par canal |
| Fusion de lignes `is_upsell` dans le panier POS | fiabilité de l'attribution par ligne | lecture du contrôleur de panier POS (déjà signalé le 2026-07-02, toujours non tranché) |
| Commandes `CLOSED` dont le `brand_status` est annulé | le cron les compte, l'Analytics les exclut | requête ad hoc sur la prod |
| Coût réel de `GetProduct` par suggestion | latence | requête B5 (latence par canal) |
| Historique d'exécution du cron | aucune table d'exécution ; seule `_meta` dans Redis | lecture de `upsell:patterns:<merchant>:_meta` |
| Comportement des clients anciens (sans `isUpsell`) encore en salle | sous-estimation des lignes `is_upsell` | question 4 |
