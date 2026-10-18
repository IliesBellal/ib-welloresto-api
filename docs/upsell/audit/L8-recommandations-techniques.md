# Livrable 8 — Recommandations techniques concrètes

**Révisé le 2026-10-16** ([L9](L9-resultats-baseline-prod.md)) : objectif CA,
le moins de friction possible, ~280 sessions par semaine sur SNO et borne,
coûts non saisis.

Les trois chantiers techniques les plus prioritaires, dans l'ordre de
livraison. Aucun ne réécrit le moteur : ils ajoutent un journal, une couche de
politique et un scoreur à côté du chemin actuel, qui reste le bras
« contrôle ». En annexe, l'exclusion d'un produit par le restaurateur (U-26),
demandée et simple.

| | Chantier | Opportunités | Pourquoi d'abord |
|---|---|---|---|
| R1 | Journal d'exposition et indicateurs fiables | U-01, U-04, U-05, U-06, U-27 | Les chiffres actuels trompent (acceptations sur commandes non payées, appels POS sans affichage) |
| R2 | Couche de politique et groupe témoin | U-03, U-23 (partiel) | Filet contre une forte dégradation ; support de tous les tests |
| R3 | Score commun, CA espéré et diversité | U-09, U-10, U-12, U-22a | Premier levier de CA, appuyé par les données (associations en tête, presque jamais prises) |

Migrations à appliquer par Ilies ; aucune n'est destructive.

---

## R1 — Journal d'exposition et indicateurs fiables

### Composants

| Composant | Modification |
|---|---|
| Migration (nouvelle, `migrations/todo/`) | colonnes additives sur `upsell_suggestions` (ci-dessous) |
| [upsell/service.go](../../../internal/modules/upsell/service.go) | latence totale ; filtre de disponibilité par mode et prix du mode (U-05) ; plafond d'affichage par canal (U-06) ; écriture du contexte |
| [menu/repository.go](../../../internal/modules/menu/repository.go) `ListAvailableProductsForUpsell` | lecture de `available_in`, `available_take_away`, `available_delivery` et des prix par mode |
| [upsell/types.go](../../../internal/modules/upsell/types.go), [repository.go](../../../internal/modules/upsell/repository.go) | nouveaux champs de `CreateSuggestionParams` |
| [upsell/tracker.go](../../../internal/modules/upsell/tracker.go) | acceptation = lignes `IsUpsell` de la commande ; `order_id` écrit pour toute session liée |
| [order_life_cycle/service.go](../../../internal/modules/order_life_cycle/service.go) | suivi déclenché aussi lors d'une mise à jour de commande portant `upsell_suggestion_id` (U-27) |
| [analytics/upsell.go](../../../internal/modules/analytics/upsell.go) | acceptée = articles non vides **et** commande liée clôturée, non annulée (C-22) ; proposée = `returned_count > 0` (repli sur l'ancienne définition pour les lignes antérieures) |
| Enveloppes SNO et borne | gardent leurs filtres comme filet (cache) et journalisent tout retrait |
| SNO (`useUpsell.ts`, `payload.ts`, store) | clé de session par panier ; identifiant de l'étape transmis à la commande **même sans ajout** ; drapeau `exposure_tracking` |
| Borne (`upsell_controller.dart`, `order_controller.dart`) | idem ; ne plus effacer l'identifiant quand la liste est vide |
| POS (`order_controller.dart`, `upsell_controller.dart`, barre) | n'appeler que si la barre est affichée (U-27) ; effacer l'identifiant accepté quand plus aucune ligne `isUpsell` ne reste ; libellé « Suggestion » |

```sql
-- Additive, nullable : sans effet sur le code actuel.
ALTER TABLE upsell_suggestions
    ADD COLUMN IF NOT EXISTS order_type             varchar(20),
    ADD COLUMN IF NOT EXISTS cart_product_ids       jsonb,
    ADD COLUMN IF NOT EXISTS session_key            varchar(64),
    ADD COLUMN IF NOT EXISTS policy                 varchar(32),
    ADD COLUMN IF NOT EXISTS experiment_key         varchar(32),
    ADD COLUMN IF NOT EXISTS engine_version         varchar(16),
    ADD COLUMN IF NOT EXISTS returned_count         smallint,
    ADD COLUMN IF NOT EXISTS latency_total_ms       integer,
    ADD COLUMN IF NOT EXISTS client_tracks_exposure boolean NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_upsell_suggestions_merchant_created
    ON upsell_suggestions (merchant_id, created_at);
```

### Règles

- Ce que le moteur enregistre doit être ce que le client reçoit : filtres de
  disponibilité (canal, horaires, **mode**) et plafond d'affichage appliqués
  avant la persistance.
- Une session liée à une commande sans ajout donne `accepted_items = []` :
  **exposition refusée**, plus acceptation.
- Une acceptation ne compte que si la commande liée est clôturée et non
  annulée : sur la borne et SNO, le suivi a lieu avant le paiement.
- `suggested_items` garde pour chaque article `origin`, `score` et, pour une
  association, `reason_product_id`.

### Pseudo-code

```go
const engineVersion = "2026.10-r1"

func (s *Service) generateUpsellSafe(ctx context.Context, in UpsellInput) (*UpsellResult, error) {
    started := time.Now()
    // ... réglages, cache, candidats inchangés, sauf :
    //   availableForOrderType(ap, in.OrderType) ajouté au filtre des candidats (U-05)
    //   maxItems = min(maxItems, displayCap(in.Channel))       // 6 pour SNO et borne (U-06)
    // ... associations, petits prix, LLM inchangés ...
    return s.persistAndCache(ctx, persistInput{
        // champs existants ...
        OrderType:      normalizeOrderType(in.OrderType),
        CartProductIDs: distinctIDs(in.Cart),
        SessionKey:     in.SessionKey,
        Policy:         policy, // "control" tant que R2 n'est pas activé
        EngineVersion:  engineVersion,
        ReturnedCount:  len(suggestions),
        LatencyTotalMs: int(time.Since(started).Milliseconds()),
        ClientTracks:   in.ExposureTracking,
    })
}

// Tracker : l'acceptation vient des lignes, plus d'une intersection par product_id.
func acceptedFrom(final []models.ProductEntry, suggested map[string]struct{}) []AcceptedItem {
    acc := map[string]*AcceptedItem{}
    for _, p := range final {
        if !p.IsUpsell {
            continue
        }
        if _, ok := suggested[p.ProductID]; !ok {
            continue // ligne upsell venue d'une autre liste : ignorée ici
        }
        a := acc[p.ProductID]
        if a == nil {
            a = &AcceptedItem{ProductID: p.ProductID, UnitPrice: p.Price}
            acc[p.ProductID] = a
        }
        a.Quantity += quantityOf(p)
    }
    return values(acc) // tableau vide = exposition refusée
}
```

```sql
-- Analytics : acceptations réelles (C-03, C-22).
COUNT(*) FILTER (
  WHERE jsonb_array_length(s.accepted_items) > 0
    AND o.state IN ('CLOSED', 'DONE')
    AND COALESCE(upper(o.brand_status), '') NOT IN ('DELETED', 'CANCELED')
)
-- avec LEFT JOIN orders o ON o.order_id = s.order_id
```

Pour les anciens clients (`client_tracks_exposure = false`), l'intersection
actuelle reste le repli : ils n'envoient l'identifiant qu'en cas d'ajout et
posent déjà `is_upsell`.

Côté POS, le déclenchement devient conditionnel :

```dart
// order_controller.dart : l'appel ne part que si la barre est réellement visible.
onCartCompositionChanged: () {
  if (upsellBarVisible) { // tablette && posUpsellEnabled, fourni par la vue
    upsellController.onCartChanged(_cart, selectedOrderType.serverValue);
  }
},
```

### Tests

| Niveau | Test |
|---|---|
| Unitaire | `acceptedFrom` : aucune ligne `IsUpsell` → tableau vide ; ligne `IsUpsell` d'un produit suggéré → acceptée avec sa quantité ; produit suggéré pris au menu → non accepté |
| Unitaire | filtre par mode : un candidat indisponible à emporter n'est pas proposé en `TAKE_AWAY`, sa place est reprise |
| Unitaire | plafond : `upsell_max_items = 10` sur SNO → 6 articles générés |
| Intégration (`postgres_integration`) | nouvelles colonnes écrites ; l'Analytics exclut `accepted_items = []` et les commandes annulées ; suivi déclenché sur mise à jour de commande |
| Bout en bout | harnais Redis en mémoire (par exemple miniredis, si le client du cache le permet ; sinon une interface sur le cache) pour couvrir `generateUpsellSafe` en entier |
| SNO (vitest) | popup affiché puis « Non merci » → la commande porte `upsell_suggestion_id` et `exposure_tracking`, aucune ligne `is_upsell` |
| Borne | même scénario ; liste vide → identifiant conservé |
| POS | téléphone → aucun appel ; tablette → appel ; article accepté puis retiré → identifiant effacé ; suggestion tapée sur une commande existante → acceptation suivie |

### Critères d'acceptation

- 100 % des nouvelles lignes SNO et borne portent `order_type`,
  `cart_product_ids`, `returned_count`, `latency_total_ms`, `engine_version`.
- Sur 7 jours, sessions liées à une commande à ±5 % des commandes SNO et borne
  passées par l'étape (contrôle croisé avec B6).
- B8 relancée : plus aucune acceptation comptée sur une commande annulée.
- À 212, le nombre d'appels POS par commande baisse fortement (téléphones) et
  des acceptations POS apparaissent dès qu'une ligne `is_upsell` POS existe.

### Déploiement et retour arrière

1. Migration (Ilies).
2. API : colonnes, Tracker, suivi en mise à jour, **correctif Analytics**.
   Compatible avec les clients actuels.
3. SNO (déploiement web immédiat), puis borne, puis POS.

L'ordre 2 avant 3 est impératif : un client qui envoie l'identifiant sans
ajout, face à l'ancienne API, ferait compter chaque refus comme une
acceptation. **Retour arrière** : revenir sur l'API laisse les colonnes
inutilisées ; si l'API doit revenir en arrière alors que les nouveaux clients
sont en place, couper d'abord l'envoi systématique côté SNO et borne
(drapeau de configuration).

---

## R2 — Groupe témoin et couche de politique

### Rôle, révisé avec les données

Au volume actuel, le témoin ne pourra pas prouver que le popup rapporte plus
qu'il ne coûte (il faudrait détecter 0,6 point d'abandon, soit environ 44 000
sessions). Il sert à deux choses : **détecter une forte dégradation** (abandon
+3 points visible en ~10 semaines avec 20 % de témoin) et **porter les tests**
de classement et de friction (R3, U-13, U-28). La couche de politique est le
vrai livrable ; le témoin en est un bras.

### Composants

| Composant | Modification |
|---|---|
| Migration | `merchant_parameters.upsell_holdout_pct smallint NOT NULL DEFAULT 0`, `upsell_variant_pct smallint NOT NULL DEFAULT 0`, `upsell_experiment_key varchar(32)` |
| [upsell/repository.go](../../../internal/modules/upsell/repository.go) `GetMerchantUpsellSettings` | lit ces colonnes dans la même requête |
| [upsell/service.go](../../../internal/modules/upsell/service.go) | `assignPolicy` avant Redis et le cache ; sortie anticipée pour le témoin ; politique dans la clé de cache |
| [upsell/types.go](../../../internal/modules/upsell/types.go) | `SourceHoldout = "holdout"`, constantes de politique |
| Requêtes SNO et borne | champ `upsell_session_key` (R1) |
| Clients | **aucun changement pour le témoin** : SNO et la borne sautent déjà l'étape sur une liste vide ([useUpsell.ts:90-95](../../../../wello-resto-scannorder/src/hooks/useUpsell.ts#L90-L95), [cart_screen.dart:74](../../../../wello-kiosk/lib/presentation/screens/cart_screen.dart#L74)) |

### Algorithme

```go
const (
    PolicyHoldout   = "holdout"
    PolicyControl   = "control"
    PolicyUnifiedV1 = "unified_v1" // R3
)

// assignPolicy renvoie le bras d'une session. Même session, même bras.
func assignPolicy(merchantID, sessionKey, experimentKey, channel string, holdoutPct, variantPct int) string {
    if channel != ChannelSNO && channel != ChannelKiosk {
        return PolicyControl // le POS reste hors expérience
    }
    if sessionKey == "" {
        sessionKey = newRandomKey() // ancien client : tirage par appel, bras enregistré sur la ligne
    }
    bucket := fnv64a(merchantID+"|"+experimentKey+"|"+sessionKey) % 10000
    switch {
    case int(bucket) < holdoutPct*100:
        return PolicyHoldout
    case int(bucket) < (holdoutPct+variantPct)*100:
        return PolicyUnifiedV1
    default:
        return PolicyControl
    }
}

// Dans generateUpsellSafe, juste après la lecture des réglages :
policy := assignPolicy(merchantID, in.SessionKey, settings.ExperimentKey, channel,
    settings.HoldoutPct, settings.VariantPct)
if policy == PolicyHoldout {
    return s.persistAndCache(ctx, persistInput{
        Suggestions: []SuggestedItem{}, Source: SourceHoldout, CacheKey: "",
        Policy: policy, /* contexte R1 */
    })
}
cacheKey := fmt.Sprintf(cacheKeyResultFmt, merchantID, cartSignature, orderType, channel) + ":" + policy
```

`variantPct` est une part de **toutes** les sessions : avec 10 % de témoin et
45 % de variante, le contrôle reçoit 45 %, soit 50/50 entre bras exposés.

### Analyse (squelette)

```sql
-- CA par session éligible et par bras, pour une expérience (CA HT des commandes clôturées).
WITH s AS (
  SELECT id, merchant_id, policy, order_id
  FROM upsell_suggestions
  WHERE channel::text IN ('SNO', 'KIOSK')
    AND client_tracks_exposure
    AND experiment_key = :experiment
    AND created_at >= :start AND created_at < :end
),
o AS (
  SELECT order_id, ht
  FROM orders
  WHERE order_id IN (SELECT order_id FROM s WHERE order_id IS NOT NULL)
    AND state IN ('CLOSED', 'DONE')
    AND COALESCE(upper(brand_status), '') NOT IN ('DELETED', 'CANCELED')
)
SELECT s.merchant_id, s.policy,
       COUNT(*)                     AS sessions,
       COUNT(o.order_id)            AS sessions_with_paid_order,
       AVG(COALESCE(o.ht, 0))       AS revenue_ht_per_session_cents
FROM s
LEFT JOIN o ON o.order_id = s.order_id
GROUP BY 1, 2;
```

Le CA total par session est trop bruité pour départager deux bras à ce volume
(l'upsell pèse 0,10 € sur un panier qui varie de plusieurs euros). On lit donc
séparément : le taux de commande payée (abandon), le CA des lignes `is_upsell`
par session, et le délai appel → commande. Intervalles calculés hors base
(bootstrap par session, stratifié par établissement).

### Contrôle d'équilibre des bras

Chaque jour : effectifs observés contre attendus, test du χ² ; p < 0,001 →
alerte et suspension.

### Tests

| Niveau | Test |
|---|---|
| Unitaire | 100 000 clés aléatoires : 10 % de témoin à ±0,3 point ; même clé, même bras ; sel différent, affectation indépendante |
| Unitaire | POS toujours `control` ; `holdoutPct = 0` ne produit jamais de témoin |
| Unitaire (cache simulé) | une session témoin ne lit ni n'écrit le cache ; ligne persistée avec `policy = holdout`, liste vide |
| Intégration | réglages lus depuis `merchant_parameters` ; analyse SQL sur des lignes de test |
| Recette (staging) | SNO et borne : session témoin → passage direct au paiement, commande liée |

### Critères d'acceptation

- Pas d'alerte d'équilibre la première semaine.
- Aucune étape affichée dans le bras témoin (recette SNO et borne).
- Indicateurs par bras calculables dès la première semaine.

### Déploiement et retour arrière

1. Migration ; code déployé avec tous les pourcentages à 0.
2. Témoin à 20 % sur 212, 226 et 231 avec une `experiment_key` commune
   (environ 280 sessions par semaine au total) pendant 10 à 12 semaines, puis
   10 % en permanence.
3. **Retour arrière** : `upsell_holdout_pct = 0`, effet immédiat (réglages
   relus à chaque appel), sans déploiement.

---

## R3 — Score commun, CA espéré et diversité

### Principe

1. **Échelle commune** : en l'absence d'association, la meilleure estimation
   de « probabilité d'ajout de B » est P(B), la part des commandes contenant
   B, précisément la valeur vers laquelle le lissage tire déjà P(B|A)
   (β = 10). Associations et petits prix deviennent comparables.
2. **Calibration par origine** : P(B|A) et P(B) mesurent l'achat sur toutes
   les commandes, pas l'ajout au moment du popup. Les données montrent un
   écart d'un ordre de grandeur, différent selon l'origine (ajout observé :
   0,75 % des présentations pour une association, 1,32 % pour un petit prix,
   alors que les scores bruts valent 10 % et plus). Chaque nuit, un facteur
   par origine k = ajouts ÷ Σ des scores bruts des articles présentés est
   estimé sur les expositions des 90 derniers jours, commun à tous les
   établissements, et lissé vers le facteur global (toutes origines) avec un
   poids équivalent à 100 présentations ; p_cal = k × score brut. Deux
   paramètres seulement, estimables dès aujourd'hui (48 ajouts sur ~3 750
   présentations).
3. **Deux variantes, livrées l'une après l'autre** :
   - `unified_v1` : classement par **p_cal** seul + diversité. Dans la bande
     des petits prix (1,5 à 3 €), un ajout rapporte à peu près le même CA
     (1,85 € par session avec ajout) : maximiser les ajouts revient à
     maximiser le CA, sans risque de pousser un article cher peu acheté ;
   - `unified_v2` : **CA espéré** v = p_cal × prix HT, seulement quand la
     calibration distingue aussi la tranche de prix (sous et au-dessus du seuil
     « petit prix ») avec au moins 300 présentations et 5 ajouts par case ;
     sinon un article cher à score brut gonflé passerait devant à tort.
   Les produits à 0 € tombent en dernier dans `unified_v2`. Quand les coûts
   seront saisis (U-11), v = p_cal × marge HT pourra devenir une variante.
4. **Diversité** : au plus 2 articles par catégorie au premier passage,
   catégories déjà au panier en dernier.
5. **Variante d'un groupe** : la plus vendue plutôt que la moins chère, en
   variante séparée (`unified_v1_bestseller`, sonde U-22a).

La règle D12 (pas d'association d'une catégorie déjà au panier) est conservée.

**Appui des données** (L9 §3) : le produit d'une association, toujours en
tête, n'a été ajouté que 2 fois sur 266 présentations ; dans ces mêmes listes,
14 ajouts sur 15 portent sur un petit prix situé plus bas.

### Composants

| Composant | Modification |
|---|---|
| [tasks/upsell.go](../../../internal/tasks/upsell.go) | `LowPriceEntry.Share = orders / totalOrders` (le total est déjà calculé) ; calcul nocturne des facteurs de calibration par origine (puis par origine × tranche de prix) depuis `upsell_suggestions`, stockés dans Redis (`upsell:calibration`) |
| [upsell/types.go](../../../internal/modules/upsell/types.go) | champ `Share` ; politiques `unified_v1`, `unified_v1_bestseller`, `unified_v2` |
| [upsell/service.go](../../../internal/modules/upsell/service.go) | `rankUnified` utilisé pour ces politiques ; chemin actuel inchangé pour `control` ; le score écrit dans `suggested_items` devient le score brut (association : P(B\|A) ; petit prix : `Share`), base de la calibration suivante |

### Pseudo-code

```go
type candidate struct {
    ap     menu.AvailableProduct
    p      float64 // probabilité d'ajout estimée
    origin string  // pattern | low_price
    reason string  // produit du panier à l'origine d'une association
}

func rankUnified(policy string, patterns map[string]patternScore, lowPrice []LowPriceEntry,
    cands map[string]menu.AvailableProduct, cartCats map[string]struct{},
    cal Calibration, limit int) []SuggestedItem {

    pool := map[string]candidate{}
    for pid, ps := range patterns { // déjà filtrées par catégorie du panier (D12)
        ap := cands[pid]
        pool[pid] = candidate{ap: ap, p: cal.Apply(OriginPattern, ap, ps.confidence),
            origin: OriginPattern, reason: ps.sourceProductID}
    }
    for _, e := range lowPrice {
        ap, ok := cands[e.ProductID]
        if !ok || e.Share <= 0 {
            continue
        }
        p := cal.Apply(OriginLowPrice, ap, e.Share)
        if cur, seen := pool[e.ProductID]; !seen || p > cur.p {
            pool[e.ProductID] = candidate{ap: ap, p: p, origin: OriginLowPrice}
        }
    }

    list := sortedValues(pool)
    value := func(c candidate) float64 {
        if policy == PolicyUnifiedV2 && cal.ByPriceBand() {
            return c.p * float64(priceHT(c.ap)) // CA espéré, seulement si calibré par tranche de prix
        }
        return c.p // unified_v1 : probabilité calibrée
    }
    sort.SliceStable(list, func(i, j int) bool {
        vi, vj := value(list[i]), value(list[j])
        if vi != vj {
            return vi > vj
        }
        if list[i].p != list[j].p {
            return list[i].p > list[j].p
        }
        return list[i].ap.ProductID < list[j].ap.ProductID
    })
    return toSuggestedItems(diversify(list, cartCats, 2, limit))
}

// diversify : au plus maxPerCat par catégorie, catégories du panier en dernier,
// puis complétion sans plafond si les candidats manquent. Ordre stable.
func diversify(sorted []candidate, cartCats map[string]struct{}, maxPerCat, limit int) []candidate {
    var first, later []candidate
    perCat := map[string]int{}
    for _, c := range sorted {
        cat := c.ap.CategoryID
        _, inCart := cartCats[cat]
        if cat != "" && (inCart || perCat[cat] >= maxPerCat) {
            later = append(later, c)
            continue
        }
        perCat[cat]++
        first = append(first, c)
    }
    out := append(first, later...)
    if len(out) > limit {
        out = out[:limit]
    }
    return out
}
```

`priceHT` utilise le prix du mode de commande (U-05) et le taux de TVA du
produit pour ce mode. Liste de petits prix calculée avant ce changement (sans
`Share`) : la politique retombe sur `control` pour la requête, et le journalise.

### Tests

| Niveau | Test |
|---|---|
| Unitaire | association à 0,11 contre petit prix à part 0,30, facteurs égaux → le petit prix passe devant (`unified_v1`), l'association reste devant (`control`) |
| Unitaire | calibration (valeurs d'illustration) : facteur d'association 0,03, de petit prix 0,13 → une association à 0,40 (p_cal 0,012) passe derrière un petit prix à 0,15 (p_cal 0,0195) |
| Unitaire | estimation des facteurs : effectifs nuls → facteur a priori ; 48 ajouts sur ~3 750 présentations → facteurs reproduits à partir de B2 |
| Unitaire | produit présent dans les deux sources : la plus grande probabilité calibrée et son origine sont gardées |
| Unitaire | `unified_v2` sans calibration par tranche de prix → se comporte comme `unified_v1` ; avec : à probabilité égale le plus cher passe devant, produit à 0 € dernier |
| Unitaire | `diversify` : trois boissons en tête → la troisième passe après le premier dessert ; catégorie du panier en dernier ; complétion ; ordre stable |
| Unitaire | `unified_v1_bestseller` : association vers un groupe → variante la plus vendue |
| Unitaire | liste sans `Share` → repli sur `control` |
| Unitaire (cron) | `selectUpsellLowPrice` renseigne `Share = orders / total` |
| Intégration | `TestComputeUpsellLowPriceList_Postgres` vérifie `Share` |
| Rejeu hors ligne | paniers des 90 derniers jours (`cart_product_ids`, ou corps des appels dans `api_request_logs`) passés dans les politiques : part de boissons, prix moyen, catégories distinctes, produits à 0 € ; contrôle de vraisemblance, pas critère de succès |

### Critères d'acceptation

- Tests verts ; rejeu sans liste aberrante.
- Adoption décidée par le test ([L7 §6](L7-plan-mesure-profit.md#6-plans-de-test)) :
  à ~280 sessions par semaine et 50/50 entre bras exposés, seul un gain d'au
  moins +50 % des sessions avec ajout payé (5,4 % → 8,1 %) se lit en 10 à 12 semaines ;
  on adopte si la borne basse de l'écart est positive et que l'abandon ne se
  dégrade pas de plus de 3 points.

### Déploiement et retour arrière

1. Code déployé avec `upsell_variant_pct = 0`.
2. `POST /admin/upsell/recompute-patterns` pour remplir `Share` et les
   facteurs de calibration.
3. 45 % de `unified_v1` (50/50 entre bras exposés) sur 212, 226 et 231, en même
   temps que le témoin de R2. `unified_v2` et `unified_v1_bestseller` ensuite,
   un test à la fois.

**Retour arrière** : `upsell_variant_pct = 0`, effet immédiat. `Share` est
ignoré par `control`.

---

## Annexe — Exclusion d'un produit par le restaurateur (U-26)

Demandée (question 5), indépendante des trois chantiers.

| Composant | Modification |
|---|---|
| Migration | `ALTER TABLE products ADD COLUMN IF NOT EXISTS upsell_excluded boolean NOT NULL DEFAULT FALSE;` |
| [menu/repository.go](../../../internal/modules/menu/repository.go) `ListAvailableProductsForUpsell` | `AND COALESCE(p.upsell_excluded, FALSE) = FALSE AND COALESCE(g.upsell_excluded, FALSE) = FALSE` (un groupe exclu exclut ses variantes) |
| [tasks/upsell.go](../../../internal/tasks/upsell.go) `computeUpsellLowPriceList` | même filtre dans le catalogue (les associations vers un produit exclu tombent d'elles-mêmes : il n'est plus candidat) |
| [upsell/repository.go](../../../internal/modules/upsell/repository.go) `ListFeaturedProducts` | même filtre |
| Mise à jour produit (module menu) | champ optionnel `upsell_excluded` dans la mise à jour de produit |
| Back-office | case « Ne jamais proposer en suggestion » sur la fiche produit |

**Tests** : un produit exclu n'est renvoyé ni comme association, ni comme
petit prix, ni par le secours `is_popular` ; une variante d'un groupe exclu non
plus ; le cache (30 min) peut encore servir l'ancien résultat jusqu'à
expiration (à indiquer dans le back-office ou à purger).

**Déploiement** : migration, API, back-office. **Retour arrière** : décocher.
