# Upsell — Variantes et complément « boissons / desserts »

Démarré le 2026-09-27. Document tenu au fil du chantier : les décisions sont
consignées au moment où elles sont prises, y compris celles reportées ou
écartées. Résumé dans [decisions.md](decisions.md) (entrée du 2026-09-27).

**Statut (2026-09-28) : D1, D2 et D5 à D13 implémentés et testés. D13 non
commité, non déployé.**

## En bref : comment l'upsell choisit ses produits après ce chantier

Pour un panier donné, sur un canal donné (POS, SNO, borne) :

1. **Candidats** : produits disponibles, hors panier. **Jamais de produit
   groupe** (« Coca Cola », prix 0 en base) : ce sont ses variantes qui sont
   proposées (« Coca Cola (33cl) »), comme dans les catalogues SNO et borne.
   Une variante du panier exclut toutes les variantes de son groupe. Sur SNO
   et la borne, on retire aussi les produits non vendus sur le canal et ceux
   hors horaires (D1, D9, D13).
2. **Associations** (« achetés ensemble », calculées chaque nuit) : au
   moins 8 commandes ensemble, un lien plus fréquent que le hasard (lift
   ≥ 1,2) et au moins 10 % des acheteurs du produit du panier qui ajoutent la
   suggestion (part lissée). Classement par cette part. Une suggestion d'une
   catégorie déjà présente dans le panier est écartée : pas de deuxième pizza
   (D12). Les associations sont calculées par groupe ; une association vers
   un groupe propose sa variante la moins chère (à prix égal, la plus
   vendue, D13). On garde tout ce qui est trouvé, même moins que le maximum
   (D2).
3. **Petits prix à succès** : les produits vendus sur 90 jours et dont le
   prix vaut au plus le tiers du prix médian de la carte, meilleures ventes
   d'abord. Variantes comprises, avec leur propre prix et leurs propres
   ventes ; jamais de groupe. Produits à 0 € exclus. Liste calculée chaque
   nuit (D10, D13).
4. **LLM** pour les places restantes, s'il est activé (désactivé en prod
   actuellement).
5. **Arrêt** : pas de produits plus chers pour compléter. La liste peut donc
   compter moins d'articles que `upsell_max_items` (D11).

`upsell_max_items` s'applique à tous les canaux, y compris la borne, qui
était plafonnée à 3 (D8). Les produits `is_popular` ne servent plus qu'en
secours total, quand Redis ou le catalogue est indisponible.

---

## 1. Contexte

Question de départ : `upsell_max_items` est-il toujours atteint ? Réponse :
non. Le moteur actuel ([internal/modules/upsell/service.go](../internal/modules/upsell/service.go))
enchaîne trois étapes exclusives :

1. **Patterns** (associations calculées chaque nuit par
   [internal/tasks/upsell.go](../internal/tasks/upsell.go), stockées dans Redis) :
   utilisés **seulement si** au moins `maxItems` produits associés sont
   trouvés. Sinon, tout ce qui a été trouvé est jeté.
2. **LLM** : pour tout le reste.
3. **`featured_fallback`** : produits `is_popular = TRUE`, cochés à la main
   par le commerçant, si le LLM échoue ou est coupé.

### Constats (données staging, lecture seule, 2026-09-27)

> Ces chiffres viennent de `RENDER_STAGING_DATABASE_URL`. Ils servent à
> comprendre la mécanique, **pas à calibrer** : voir D4.

| Constat | Chiffre |
|---|---|
| Suggestions `pattern` / total | 1 / 321 |
| Suggestions `llm` | 0 |
| Produits vendus (marchand 212, 90 j) ayant ≥ 4 associations | 2 sur 152 |
| Produits vendus sans aucune association | 121 |
| Paniers ne contenant qu'un seul produit | 592 / 1 238 (48 %) |
| Produits `is_popular` du marchand 212 | 0 (fallback vide) |
| Lignes de commande pointant vers une variante (`by_product_of`) | 328 / 2 292 |

**Variantes.** Une variante est un produit dont `by_product_of` pointe vers
un produit « groupe » (`is_product_group = TRUE`). Exemple : « Coca Cola
(33cl) », « Coca Cola (1.25L) » → groupe « Coca Cola » (id 584). Les lignes
de commande portent l'id de la **variante**, alors que les produits
proposables ([menu/repository.go:137](../internal/modules/menu/repository.go#L137))
excluent les variantes et ne gardent que le groupe. Conséquences :

- une association qui pointe vers une variante n'est jamais proposable et
  disparaît sans message ; ce sont justement les plus fortes du jeu de
  données (Coca 1.25L, Oasis 2L) ;
- les ventes d'un même produit sont éclatées entre ses variantes, ce qui
  affaiblit les statistiques ;
- si le panier contient « Coca Cola (33cl) », seul l'id de la variante est
  exclu, et le groupe « Coca Cola » peut encore être suggéré. Confirmé pour
  SNO, qui envoie l'id de la variante choisie
  (`selectedSubProduct?.id || product.id`, `wello-resto-scannorder/src/lib/api/payload.ts`).
  POS et Kiosk n'ont pas été vérifiés un par un : la correction fonctionne
  que le client envoie la variante ou le groupe.

**Prix des groupes.** Les produits groupe ont `price = 0` (vérifié sur 584,
585, 586, 587, 2339). Tout tri « du moins cher » les ferait remonter en tête,
et `SuggestedItem.Price` vaut aujourd'hui 0 pour eux.

---

## 2. Décisions

### D1 — Rattacher les variantes à leur groupe (2026-09-27, validé)

Dans l'analyse de paniers, chaque ligne de commande est comptée sous
l'id de son groupe : `COALESCE(NULLIF(p.by_product_of, 0), oi.product_id)`.
Une ligne dont le produit n'existe plus garde son id d'origine.

Même rattachement au moment de la suggestion, pour les produits du panier :
- recherche des associations sous l'id du groupe ;
- exclusion du groupe des candidats quand une de ses variantes est déjà
  dans le panier.

*Pourquoi* : c'est une correction, pas un réglage. Elle ne dépend pas des
données de prod.

> **Révisé par D13 (2026-09-28)** : le rattachement reste pour les
> statistiques et pour le panier, mais le groupe n'est plus jamais suggéré.
> Ce sont ses variantes qui le sont, et toutes sont exclues quand l'une
> d'elles est dans le panier.

### D2 — Toujours viser `upsell_max_items` (2026-09-27, validé)

On ne jette plus les associations quand il y en a moins que `maxItems` :
on les garde, puis on complète jusqu'à `maxItems`. On ne descend sous
`maxItems` que si le catalogue n'a plus de candidat admissible.

*Pourquoi* : demande explicite. Les associations restent en tête de liste
(« on garde la logique de déduction d'articles qui vont bien ensemble »).

### D3 — Compléter avec des boissons et desserts moins chers (2026-09-27, validé dans le principe, remplacé par D10)

> Remplacé : aucune règle de reconnaissance des catégories n'est jugée
> assez solide (arbitrage P1). Le complément se fait sur « petits prix à
> succès », sans interpréter les catégories (D10).

Le complément vient des catégories **boissons** et **desserts**, en
privilégiant les produits **moins chers** qui **se vendent**.

*Pourquoi* : ce sont des achats d'impulsion, peu coûteux, qui complètent
naturellement un plat. Le complément générique envisagé au départ
(« produits à succès pas chers », toutes catégories confondues) pouvait
proposer une deuxième pizza. Les modalités restent à fixer : voir §3.

### D4 — Ne pas calibrer sur staging (2026-09-27)

Staging n'est pas jugée représentative. Seuils et pondérations ne sont
donc pas réglés à partir des chiffres ci-dessus. D1 et D2 n'en dépendent
pas et peuvent avancer. Pour D3, il faut les données de prod : voir §3, Q6.

### D5 — Ordre de complément provisoire : associations → LLM → produits mis en avant (2026-09-27, remplacé par D11)

Tant que D3 n'est pas construit, les places restantes sont complétées avec
les étapes existantes, dans leur ordre actuel : le LLM ne reçoit que les
places restantes, puis les produits `is_popular` comblent ce qui manque
encore. D3 viendra remplacer ou précéder l'étape `is_popular` (Q5 reste
ouvert).

*Pourquoi* : D2 ne dépend pas des données de prod et corrige déjà le
« tout ou rien ». Le coût LLM ne change pas : le LLM était déjà appelé dès
que les associations étaient insuffisantes, et il ne l'est toujours pas
quand elles suffisent.

### D6 — Origine par article, `source` = première étape qui a contribué (2026-09-27)

Une même liste peut maintenant mélanger plusieurs origines. Chaque article
porte un champ `origin` (`pattern`, `llm`, `featured`) dans
`suggested_items`. Le champ est additif et n'est pas émis quand il est vide,
donc les clients l'ignorent. La colonne `source` garde ses valeurs actuelles
et nomme la première étape qui a contribué : une liste « 2 associations +
2 produits mis en avant » a pour `source` `pattern`.

*Pourquoi* : réponse partielle à Q8, sans migration ni changement des valeurs
de `source`. Vérifié en cherchant ces valeurs dans le code : aucun code hors
du module upsell ne les lit.

### D7 — `{MAX_ITEMS}` réellement remplacé dans le prompt LLM (2026-09-27)

Le prompt système contenait `{MAX_ITEMS}` en dur : le LLM recevait
littéralement « entre 1 et {MAX_ITEMS} produits ». Le texte est maintenant
remplacé par le nombre de places restantes.

*Pourquoi* : nécessaire pour que le LLM complète, au lieu de reproposer une
liste complète tronquée ensuite.

### Reporté (non retenu pour l'instant)

- **Associations catégorie → catégorie génériques** (par ex. « Pizzas → Boissons :
  30 % des paniers »). D3 couvre le besoin concret. À réexaminer avec les
  données de prod.
- **Remplacer le lift par une probabilité lissée** : le lift favorise les
  produits rares (lift de 35 sur 6 commandes communes). Reporté, car on ne
  peut pas mesurer l'effet d'un changement de score sans suivi des
  acceptations.
- **Exclure les lignes `is_upsell` de l'analyse**, pour éviter qu'une
  suggestion se renforce elle-même. Ce drapeau vaut toujours `false` pour
  l'instant, donc sans effet aujourd'hui.
- **Suivi des acceptations** : 1 acceptation enregistrée sur 320
  suggestions. Sans ce suivi, on ne pourra pas savoir si ce chantier améliore
  les ventes. Chantier séparé, voir
  [audits/audit_upsell_traceability.md](audits/audit_upsell_traceability.md).

---

## 3. Points ouverts

**Q1 — Comment reconnaître une catégorie boisson ou dessert ?**
`productcateg` n'a pas de type, seulement un nom libre. Noms observés :
« Boissons », « Boisson », « 🥤 Boissons », « Softs », « Boissons chaudes »,
« Cocktails », « Desserts », « 🍰 Desserts », « NOS DESSERTS », « Sucré ».
- (a) Mots-clés sur le nom normalisé (sans emoji, sans accents, en
  minuscules) : rien à configurer, mais fragile. « Sucré » est-il un dessert ?
  « Cocktails » est-il une boisson à proposer ?
- (b) Nouvelle colonne sur `productcateg` (par ex. `upsell_role`), réglée
  par le commerçant : fiable, mais demande une migration et un réglage.
- Proposition : (a) par défaut, (b) plus tard pour corriger les cas
  particuliers.

**Q2 — Que veut dire « moins cher » ?** Plusieurs options : un seuil absolu
(par ex. ≤ 4 €), un seuil relatif au catalogue du marchand (≤ médiane
boissons/desserts), ou un tri seul, sans seuil. À décider avec les prix de
prod.

**Q3 — Comment classer « ventes » et « prix » ensemble ?** Par exemple :
ventes sur 90 jours avec un minimum de N ventes, puis tri par prix
croissant ; ou un score ventes / prix.

**Q4 — Répartition boissons / desserts.** Proposition : alterner les deux,
et commencer par la catégorie absente du panier (panier avec une boisson →
dessert d'abord). Ne rien proposer d'une catégorie dont un produit est déjà
dans le panier ?

**Q5 — Place du LLM.** Faut-il garder l'ordre associations → LLM →
complément, ou passer à associations → complément, avec le LLM seulement
pour rédiger les accroches ? Sur staging, le LLM n'a jamais produit de
suggestion (kill-switch ou délai de 1,5 s, à vérifier dans les logs de prod).
En attendant, l'ordre actuel est conservé (D5).

**Q6 — Données de prod.** Pour Q1 à Q4, il faut les noms de catégories, les
prix et les volumes de vente réels. Solution retenue pour l'instant : des
requêtes agrégées en lecture seule à exécuter sur la prod, sans aucune
donnée client. Elles sont dans [upsell-analyse-prod.sql](upsell-analyse-prod.sql).
Un refresh de staging depuis la prod reste possible, mais il copierait aussi
les données clients sur staging.

**Q7 — Prix d'un groupe.** Proposition : le prix d'un groupe est le prix
minimum de ses variantes disponibles, utilisé pour le tri et pour
`SuggestedItem.Price`. Changer la valeur affichée concerne aussi les clients
(POS, Kiosk, SNO) : à vérifier. Les requêtes de prod utilisent déjà ce prix
effectif. Côté SNO, la popup upsell affiche déjà le prix de la première
variante (`UpsellPopup.tsx`).

**Q8 — Traçabilité.** En partie résolu par D6 (champ `origin` par article).
Reste à décider si les analytics doivent l'exploiter.

---

## 4. Plan d'implémentation

| Étape | Contenu | Fichiers | État |
|---|---|---|---|
| 1 | D1 côté cron : variantes rattachées à leur groupe dans l'analyse de paniers | `internal/tasks/upsell.go` | ✅ fait |
| 2 | D1 côté service : variantes du panier rattachées à leur groupe (recherche des associations, exclusion des candidats, catégorie dans le prompt LLM) | `internal/modules/upsell/service.go`, `repository.go` | ✅ fait |
| 3 | D2 + D5 : fin du « tout ou rien », complément LLM puis produits mis en avant | `service.go` | ✅ fait |
| 4 | D6 : origine par article | `types.go`, `repository.go`, `service.go` | ✅ fait |
| 5 | D7 : `{MAX_ITEMS}` remplacé | `service.go`, `prompts.go` | ✅ fait |
| 6 | ~~D3 : complément boissons / desserts~~ | — | remplacé par D10 |
| 7 | D8 : la borne suit `upsell_max_items` | `internal/modules/kiosk/service.go` | ✅ fait |
| 8 | D9 : filtres par canal dans le moteur | `service.go`, `menu/repository.go`, `upsell/repository.go`, `cmd/api/routes.go` | ✅ fait |
| 9 | D10 : liste « petits prix à succès » | `internal/tasks/upsell.go`, `service.go`, `types.go` | ✅ fait |
| 10 | D11 : ordre associations → petits prix → LLM → arrêt | `service.go` | ✅ fait |

---

## 5. Implémentation, première passe (D1, D2, D5 à D7)

> L'étape 5 ci-dessous (complément `is_popular`) a été retirée par la
> seconde passe (§7, D11). Le reste est toujours en place.

### Cron — [internal/tasks/upsell.go](../internal/tasks/upsell.go)

`upsellBasketLinesSQL()` définit une seule fois les lignes (commande,
produit) analysées : `SELECT DISTINCT order_id, COALESCE(NULLIF(p.by_product_of, 0), oi.product_id)`,
avec un `LEFT JOIN products`. Une ligne dont le produit n'existe plus garde
son id. Les étapes 2 (support par produit) et 3 (paires) lisent toutes les
deux cette sous-requête. Deux variantes d'un même groupe dans une commande
ne comptent qu'une fois. Le nombre total de commandes (étape 1), les seuils
et le format Redis ne changent pas.

### Service — [internal/modules/upsell/service.go](../internal/modules/upsell/service.go)

Déroulé de `generateUpsellSafe` :

1. Réglages marchand et cache du résultat : inchangés.
2. `resolveCart` : `Repository.GetProductGroups` renvoie, pour les produits du
   panier, les correspondances variante → groupe. On en tire un `cartView` :
   - `excluded` : les ids du panier plus les groupes de ses variantes ; aucun
     ne peut être suggéré ;
   - `patternKeys` : les clés Redis à lire, c'est-à-dire le groupe pour une
     variante et le produit lui-même sinon, sans doublon ;
   - `groupOf` : sert à donner au LLM la catégorie d'une variante, qui
     n'apparaît que sur son groupe.

   Si la requête échoue, on garde les ids bruts du panier (comportement
   d'avant), avec un warning dans les logs.
3. Associations : si le meilleur score cumulé atteint `minLift` (1,5), on
   garde les `maxItems` meilleures (`rankPatternSuggestions`), **même s'il y
   en a moins de `maxItems`**. En cas d'égalité de score, l'ordre suit l'id
   produit, pour un cache déterministe.
4. LLM (`llmSuggestions`) : appelé uniquement s'il reste des places. Il ne
   reçoit que les candidats non encore choisis, et le prompt système indique
   le nombre de places restantes. Tout échec (kill-switch, provider absent,
   délai dépassé, réponse invalide) est journalisé et ne renvoie aucun
   article.
5. Produits mis en avant (`featuredSuggestions`, `is_popular`) : complètent
   les places restantes en excluant le panier et les articles déjà choisis.
6. `source` = `sourceOf` (première étape qui a contribué, D6). Une liste
   composée uniquement de produits mis en avant garde `featured_fallback`, et
   comme avant elle **n'est pas mise en cache** : l'appel suivant retente les
   associations et le LLM. `persistAndCache` ne met plus en cache quand
   `cacheKey` est vide.

`featuredFallback`, le repli quand Redis est absent ou que le catalogue est
illisible, s'appuie sur `featuredSuggestions` et exclut désormais aussi les
groupes des variantes du panier.

### Changements visibles

- Les listes atteignent `maxItems` plus souvent : les associations trouvées
  ne sont plus jetées, et les produits mis en avant complètent après le LLM.
  Elles ne l'atteignent pas toujours : il faut encore assez de produits
  `is_popular`. C'est l'objet de D3.
- Un groupe (« Coca Cola ») n'est plus proposé quand une de ses variantes
  (« Coca Cola (33cl) ») est déjà dans le panier.
- Un nouveau champ JSON `origin` apparaît sur chaque article, dans la réponse
  et dans `upsell_suggestions.suggested_items`.
- Ce qui reste inchangé : les réglages marchand, les seuils du cron, la
  durée du cache (30 min), le délai LLM (1,5 s), le kill-switch
  `AI_TASK_UPSELL_ENABLED` et le contrat des endpoints hormis `origin`.

### Tests

| Test | Ce qu'il vérifie | Exécuté |
|---|---|---|
| `service_test.go` (unitaires, sans tag) | `buildCartView` (variantes → groupe, clés sans doublon), `rankPatternSuggestions` (garde moins de `maxItems`, ordre, plafond), `appendUnique`, `excludedWith` / `withoutSuggested`, `sourceOf`, catégorie d'une variante dans le prompt, `llmSuggestions` avec un faux provider (places restantes dans le prompt, candidats filtrés, kill-switch) | ✅ `go test ./internal/modules/upsell/...` |
| `TestUpsellRepository_Postgres` (complété) | `GetProductGroups` : variante → groupe, produit simple et groupe absents, pas de fuite entre marchands | ✅ sur staging |
| `TestProcessUpsellPatternsForMerchant_VariantsRolledUp_Postgres` (nouveau) | A + V1 (×3) et A + V2 (×3) donnent 2 patterns A↔G. Contre-épreuve : le test **échoue** sur l'ancien cron (0 pattern) | ✅ sur staging (les deux sens) |
| `TestProcessUpsellPatternsForMerchant_Postgres` (existant) | pas de régression | ✅ sur staging |

Les tests d'intégration ont tourné sur staging (`POSTGRES_URL=$RENDER_STAGING_DATABASE_URL`).
Ils créent leurs propres lignes `itest` puis les suppriment ; aucune ligne
`itest` restante n'a été trouvée après exécution.

`go test ./internal/...` : 4 paquets en échec (`planning/employees`,
`planning/leave`, `planning/swaps`, `ubereats`). Ils échouent à l'identique
sur `main` sans ces changements, donc sans lien avec ce chantier. Le premier
relevé n'en citait que 3 parce que la sortie avait été tronquée.

Non testé : le déroulé complet de `generateUpsellSafe` avec un vrai Redis
(il n'existe pas de harnais Redis dans les tests du dépôt). Chaque étape est
couverte séparément.

### Déploiement

- Aucune migration. Aucune variable d'environnement nouvelle.
- Les patterns Redis sont recalculés chaque nuit à 3h et durent 36 h.
  Jusqu'au premier calcul après déploiement, les anciens patterns, indexés
  par id de variante, ne sont plus lus pour un panier contenant une
  variante : ces paniers n'ont donc pas d'associations pendant au plus une
  nuit. Pour éviter ce trou, on peut lancer le recalcul à la main juste
  après le déploiement : `POST /admin/upsell/recompute-patterns`.
- Les résultats déjà en cache (30 min) sont servis sans `origin` jusqu'à
  expiration.

---

## 6. Données de prod et propositions pour D3 (2026-09-27)

Résultats de [upsell-analyse-prod.sql](upsell-analyse-prod.sql), exécuté sur
la prod par Ilies le 2026-09-27. Périmètre : 8 établissements ayant au moins
50 commandes clôturées sur 90 jours. L'upsell n'est activé que sur 212
(`upsell_max_items` = **6**) et sur 2 (établissement de test, 4).

### Constats

- ~~`maxItems` jamais atteint sur 212~~ **Corrigé** : 212 est passé de 4 à
  6 le 2026-09-27, deux heures avant la requête. Les moyennes (4,06 et 4,00)
  correspondent donc au maximum de l'époque, soit 4. **Aucune suggestion `llm`
  en 90 jours** : le LLM est désactivé en prod. 1 seule acceptation
  enregistrée.
- **La borne n'affichait que 3 produits** (constat terrain). Deux causes dans
  le code, en aval du moteur :
  - le Kiosk plafonne à **3 en dur**, quel que soit `upsell_max_items`
    ([kiosk/service.go](../internal/modules/kiosk/service.go), `GetUpsellSuggestions`) ;
  - le Kiosk et SNO **filtrent après le moteur**, sans remplacer ce qu'ils
    retirent. Le Kiosk retire les produits hors `is_available_on_kiosk` et
    hors horaires ; SNO retire seulement les produits hors horaires et **ne
    vérifie pas `is_available_on_sno`** (la fiche produit SNO, elle, le
    vérifie). Chaque produit retiré est une place perdue.
- **Noms de catégories** : toutes les catégories boissons contiennent
  « boisson » (« Boissons », « 🥤 Boissons », « Boissons 🥤 », « Nos Boissons ») ;
  toutes les catégories desserts contiennent « dessert » (« Desserts »,
  « 🍰 Desserts », « Desserts 🍨 », « Dessert »). Restent hors de ces deux mots :
  « Cocktails » et « MILKSHAKE » (234, 6 €), « Coupes » (234, 18,90 €),
  « Apéritifs » (234, mini-brochettes).
- **Prix médians** :

  | | 212 | 226 | 231 | 234 | 235 | 236 |
  |---|---|---|---|---|---|---|
  | Boissons | 1,90 € | 1,50 € | 2,00 € | 2,00 € | 2,00 € | 2,00 € |
  | Desserts | 2,90 € | 3,00 € | 3,00 € | 6,00 € | 7,00 € | 7,00 € |
  | Plats | 7,50 à 15 € | | | | | |

  Valeurs extrêmes : une théière à 6 € (234), un produit de test à 1 111 €
  et une « Glace composée » à 0 € (établissement 2).
- **Achetés ensemble** (commandes contenant A qui contiennent aussi B) :

  | Établissement | Plat → boisson | Plat → dessert |
  |---|---|---|
  | 212 (pizzas) | 32,5 % | 6,7 % |
  | 236 (assiettes) | 93 % | 5 % |
  | 226 (tacos) | 28 % | 4 % |

  Une boisson est trois à dix fois plus souvent ajoutée qu'un dessert.
  Quand un dessert est commandé, une boisson l'accompagne dans 44 à 75 % des
  cas.
- **Les menus incluent déjà une boisson** : chez 226, un panier « Menu Tacos »
  contient une boisson dans 9,6 % des cas, contre 27,9 % pour un tacos seul.
- **Faux produits vendus** : « Zone 1 » et « Zone 2 » (catégorie « Frais de
  livraison », 0 €, 235) figurent dans 31 % des commandes de 235, et la
  « Sauce Croq'ô » (0 €, 212) dans 42. Le moteur d'associations peut
  aujourd'hui les proposer.
- **Qualité du catalogue de 212** : la catégorie « temp » compte 94 produits,
  dont les deux meilleures pizzas, et 98 produits n'ont aucune catégorie.
  Sans effet sur D3, puisque boissons et desserts sont bien rangés.

### Propositions (à valider)

- **P1 — Détection (Q1)** : une catégorie est une boisson si son nom
  normalisé (sans emoji ni accents, en minuscules) contient « boisson », un
  dessert s'il contient « dessert ». Cela couvre 100 % des cas observés.
  Cocktails, milkshakes, coupes et apéritifs sont exclus volontairement. Une
  colonne de réglage sera ajoutée seulement si un cas réel l'exige.
- **P2 — « Moins cher » (Q2)** : pas de seuil en euros, les prix varient
  trop d'un établissement à l'autre. On exclut les produits à 0 € et ceux
  qui coûtent plus du double de la médiane de leur catégorie dans
  l'établissement (cela écarte la théière et le produit de test). Pour un
  groupe, on prend le prix de sa variante la moins chère.
- **P3 — Classement (Q3)** : ventes sur 90 jours décroissantes, puis prix
  croissant. Les produits jamais vendus passent en dernier. Liste calculée
  par le cron de 3h et stockée dans Redis ; la disponibilité est revérifiée
  au moment de la suggestion.
- **P4 — Ordre (Q4)** : la catégorie absente du panier d'abord, en commençant
  par les boissons. Si le panier contient déjà une boisson ou un menu, les
  desserts d'abord. Ensuite on alterne les deux catégories jusqu'à `maxItems`.
- **P5 — Place du LLM (Q5)** : associations → boissons / desserts → LLM (s'il
  reste des places) → `is_popular`. Le LLM n'a rien produit en 90 jours et
  peut coûter jusqu'à 1,5 s par appel non caché. Avec cet ordre, il ne serait
  presque plus appelé.
- **P6 — Correctif annexe** : exclure de tous les candidats les produits à
  0 € qui ne sont pas des groupes (frais de livraison, sauces offertes).

### Arbitrages d'Ilies (2026-09-27)

- **P1 rejetée** : aucune règle de reconnaissance des catégories n'est assez
  solide. On part sur les produits **les moins chers qui se vendent**, sans
  interpréter les catégories.
- **P6 modifiée** : les produits à 0 € sont exclus **uniquement du
  complément**. Une association (par ex. une sauce maison) peut toujours les
  proposer. Les frais de livraison sont gérés par la disponibilité par canal
  (SNO, Kiosk) : soit le produit est désactivé sur le canal, soit il est
  légitime de le proposer.
- **P5 sans objet pour l'instant** : le LLM est désactivé en prod.

### Arbitrages d'Ilies, suite (2026-09-27)

- **D8 — La borne suit `upsell_max_items`** : suppression du plafond de 3 en
  dur. Côté Flutter, l'écran upsell est une grille qui défile
  (`upsell_screen.dart`, `GridView.builder` sur `suggestions.length`) :
  aucun changement nécessaire.
- **D9 — Filtres par canal dans le moteur (validé)** : disponibilité sur le
  canal (`is_available_on_sno`, `is_available_on_kiosk`) et horaires,
  appliqués avant la sélection pour que le complément remplace ce qui est
  retiré. Le canal entre dans la clé du cache.
- **D10 — Seuil « petit prix » = 1/3 du prix médian de la carte (validé,
  sous réserve du coût)** : calcul expliqué dans la section suivante. Coût
  jugé acceptable ; validé le 2026-09-27.
- **D11 — Ordre final : associations → petits prix → LLM → arrêt (validé le
  2026-09-27)**. `is_popular` sort du complément : c'est un top des ventes
  surtout composé de plats, ce qui contredirait *(c)*. Il ne reste qu'en
  secours total (Redis ou catalogue indisponible). Le point *(d)* (suppléments
  vendus comme produits) est laissé en l'état : un seul cas observé, et la
  vraie réponse est de les modéliser en options.
- **Reporté, à analyser plus tard :**
  - *(b)* varier les catégories. Sans règle, 212 recevrait environ 4
    boissons sur 6. Idée à creuser : utiliser la catégorie comme simple
    étiquette (au plus 2 produits par catégorie au premier passage, et les
    catégories déjà présentes dans le panier en dernier).
  - *(c)* quand il n'y a pas assez de petits prix, **ne pas** élargir aux
    produits plus chers. La liste peut donc rester sous le maximum (par
    exemple 235, avec seulement ~3 boissons sous le seuil).
  - Associations catégorie → catégorie apprises des données, sans
    interpréter les noms (captent par exemple « menu = boisson incluse » :
    chez 226, 9,6 % de boisson avec un menu contre 27,9 % avec un tacos seul).
- *(d)* **Suppléments vendus comme produits** (par ex. « Supplément viande »
  2 €, 231) : à éviter pour le moment. Aucune règle fiable sans interpréter
  les noms ; à préciser (voir la réponse du 2026-09-27).
- **Idée d'Ilies — options populaires** : ajouter un `is_popular` sur les
  options (`configurable_attribute_options`) et l'afficher sur la borne et
  SNO pour booster les ventes. Chantier séparé, voir « Pistes » plus bas.

### Calcul de la liste « petits prix » (validé et implémenté, voir §7)

- **Quand** : chaque nuit, dans le cron de 3h (`RecomputeUpsellPatterns`),
  pour les mêmes établissements. Le résultat est stocké dans Redis
  (`upsell:lowprice:<merchant>`, durée de vie 36 h, comme les patterns).
- **Comment** (révisé par D13, voir §9) :
  1. médiane des prix des produits proposables : produits simples et
     variantes, jamais les groupes ; produits à 0 € exclus ;
  2. ventes sur 90 jours **par produit**, variantes non rattachées (une
     requête dédiée : le comptage des associations, lui, est fait par
     groupe) ;
  3. on garde les produits avec 0 < prix ≤ médiane / 3 et au moins une
     vente, triés par ventes décroissantes puis prix croissant ; au plus
     30 produits.
- **Au moment de la suggestion** : une lecture Redis de plus, puis un filtrage
  en mémoire (disponibilité en direct, canal, horaires, panier, articles
  déjà choisis). Aucune requête SQL supplémentaire.
- **Coût** : deux requêtes de plus par établissement et par nuit (le
  catalogue et les ventes par produit), bien plus légères que la jointure
  des paires que le cron fait déjà.
- **Fraîcheur** : un changement de prix ou un nouveau produit est pris en
  compte la nuit suivante ; une rupture ou un produit hors horaires le sont
  immédiatement.

### Découverte : `is_popular` est calculé, pas saisi (2026-09-27)

Correction de §1 et §3 : `products.is_popular` n'est pas coché par le
commerçant. Le cron `UpdatePopularProducts` (2h,
[internal/tasks/products.go](../internal/tasks/products.go)) le recalcule
chaque nuit : meilleure vente de chaque catégorie (au moins 5 ventes) plus les
10 meilleures ventes globales, sur 30 jours. Défauts relevés, non corrigés :
- il compte les lignes de commande brutes, donc il marque des **variantes**
  et non leur groupe ;
- il ne filtre pas sur `state = 'CLOSED'` ;
- le top 10 global contient surtout des plats (des pizzas chez 212). Pour
  l'étape de complément `is_popular`, cela contredit l'arbitrage *(c)* :
  pas de produits chers pour compléter. **Résolu par D11** : `is_popular`
  n'est plus une étape de complément.

### Pistes

- **Options populaires** (idée d'Ilies) : `is_popular` sur
  `configurable_attribute_options`, calculé chaque nuit comme pour les
  produits, à partir de `order_item_configuration` (sélections sur 30 ou
  90 jours). Badge sur la borne et SNO (le badge « Populaire » des produits
  existe déjà dans les deux). Précautions :
  - ne jamais pré-cocher une option payante : il faut le consentement exprès
    du client pour tout paiement supplémentaire ;
  - un attribut peut être partagé entre plusieurs produits
    (`product_configurable_attribute`).

---

## 7. Implémentation, seconde passe (D8 à D11)

### Borne — [internal/modules/kiosk/service.go](../internal/modules/kiosk/service.go) (D8)

`GetUpsellSuggestions` ne coupe plus à 3 : la borne reçoit toute la liste du
moteur, donc au plus `upsell_max_items`. Ses filtres existants
(`is_available_on_kiosk`, horaires, panier) restent en place comme filet de
sécurité, car un résultat en cache peut dater de 30 minutes. Côté Flutter,
rien à changer : l'écran est une grille qui défile.

### Filtres par canal dans le moteur (D9)

- `menu.AvailableProduct` porte maintenant `IsAvailableOnSNO` et
  `IsAvailableOnKiosk`, lus par `ListAvailableProductsForUpsell`. Les deux
  colonnes sont `NOT NULL DEFAULT TRUE` en base (vérifié sur staging).
- `upsell.Service` reçoit un `ScheduleAvailability` (interface satisfaite par
  `availabilities.AvailabilitiesService`, injecté dans `cmd/api/routes.go`,
  dont le bloc « Availabilities » est remonté avant « Upsell »).
- `generateUpsellSafe` écarte des candidats, avant toute sélection :
  - sur SNO, les produits avec `is_available_on_sno = FALSE` ;
  - sur la borne, ceux avec `is_available_on_kiosk = FALSE` ;
  - sur SNO et la borne, les produits hors horaires
    (`GetUnavailableProductsAt`). Le POS n'applique pas les horaires, comme
    dans son propre catalogue.

  Si la lecture des horaires échoue, on ne filtre pas sur les horaires et on
  l'écrit dans les logs.
- La clé du cache inclut le canal : `upsell:result:<merchant>:<panier>:<type de commande>:<canal>`.
- Le secours `featuredFallback` applique les mêmes filtres :
  `ListFeaturedProducts` prend le canal, et les produits hors horaires sont
  exclus.
- Gain concret : SNO ne vérifiait pas `is_available_on_sno` pour l'upsell.
  Sur staging, 95 des 261 candidats de 212 ne sont pas vendus sur SNO et
  pouvaient donc y être suggérés.

### Liste « petits prix à succès » (D10)

- **Cron** ([internal/tasks/upsell.go](../internal/tasks/upsell.go), étape 2b de
  `processUpsellPatternsForMerchant`) :
  - `computeUpsellLowPriceList` lit le catalogue proposable (mêmes filtres
    que les candidats, prix effectif d'un groupe = sa variante disponible la
    moins chère) ;
  - `selectUpsellLowPrice` applique la règle : 0 < prix ≤ médiane / 3, au
    moins une vente, tri ventes ↓ puis prix ↑ puis id, 30 au plus ;
  - les ventes viennent du comptage déjà fait pour les associations
    (variantes rattachées, commandes clôturées sur 90 jours) ;
  - le résultat va dans `upsell:lowprice:<merchant>` (36 h). Un échec est
    journalisé et n'empêche pas le calcul des associations ;
  - `upsell:patterns:<merchant>:_meta` contient désormais aussi
    `low_price_median`, `low_price_threshold` et `low_price_items`, pour
    contrôler le calcul.
- **Service** : `lowPriceSuggestions` lit la liste et garde, dans l'ordre,
  les produits encore candidats (disponibles, vendus sur le canal, dans leurs
  horaires, hors panier, pas déjà suggérés). Origine `low_price`, accroche
  tirée des mêmes modèles de phrase que les associations.
- Les produits à 0 € sont exclus **uniquement** de cette liste : une
  association peut toujours proposer une sauce maison à 0 €.

### Ordre et `source` (D11)

- Étapes : associations → petits prix → LLM (s'il reste des places et s'il
  est activé) → arrêt.
- Nouvelles valeurs : `source` = `low_price` (en cache : `cached_low_price`)
  quand la liste commence par un petit prix, et `none` quand elle est vide.
  Une liste vide est enregistrée (pour les analytics) mais pas mise en cache.
  `featured_fallback` ne sert plus qu'au secours total.
- Hors du module upsell, aucun code ne lit ces valeurs.

### Tests (seconde passe)

| Test | Ce qu'il vérifie | Exécuté |
|---|---|---|
| `internal/tasks/upsell_test.go` (nouveau, unitaire) | `selectUpsellLowPrice` : médiane (paire, impaire, prix à 0 ignorés), seuil au tiers, produits à 0 € et jamais vendus exclus, ordre ventes puis prix, plafond, catalogue vide | ✅ |
| `service_test.go` (complété) | `lowPriceFromEntries` (ordre, candidats seulement, plafond, origine), `availableOnChannel`, `unavailableNow` (POS sans horaires, SNO/borne filtrés, échec de lecture = aucun filtre), `sourceOf` avec `low_price` et `none`, `mergeSets` | ✅ |
| `TestComputeUpsellLowPriceList_Postgres` (nouveau) | prix effectif d'un groupe = variante **disponible** la moins chère ; produits à 0 € et indisponibles écartés ; médiane et ordre | ✅ sur staging |
| `TestUpsellRepository_Postgres` (complété) | `ListFeaturedProducts` par canal : POS toujours, SNO/borne selon le drapeau | ✅ sur staging |
| Vérification ponctuelle, test temporaire supprimé | `ListAvailableProductsForUpsell` avec les nouveaux drapeaux, en lecture seule sur staging (212 : 261 candidats, 166 vendus sur SNO, 261 sur la borne) | ✅ |

`TestMenuRepository_Postgres` et 3 tests d'import du paquet `menu` échouent,
mais de la même façon sur `main`, sans ces changements. Le premier échoue
avant la ligne qui appelle `ListAvailableProductsForUpsell`, d'où la
vérification ponctuelle ci-dessus. Aucune ligne `itest` restante sur
staging après exécution.

Non testés : le plafond retiré de la borne (le service Kiosk n'a pas de test
autour de `GetUpsellSuggestions`, le changement se limite à supprimer une
condition) et le déroulé complet avec un vrai Redis.

### Déploiement (les deux passes)

- Aucune migration, aucune variable d'environnement nouvelle.
- **Juste après le déploiement**, lancer `POST /admin/upsell/recompute-patterns`.
  Cet appel calcule d'un coup les associations avec variantes rattachées et
  la première liste « petits prix ». Sans lui, jusqu'au cron de 3h, il n'y a
  **aucun complément petits prix**, et plus d'associations pour les paniers
  contenant une variante.
- La clé du cache change (ajout du canal) : les anciens résultats en cache ne
  sont plus lus et expirent d'eux-mêmes en 30 minutes.
- La borne peut afficher jusqu'à `upsell_max_items` suggestions (6 pour 212)
  au lieu de 3.
- À surveiller à l'usage : la part de `source` = `low_price` et `none` dans
  `upsell_suggestions`, le nombre moyen d'articles par canal, et le contenu de
  `upsell:patterns:<merchant>:_meta` (médiane, seuil, taille de la liste).

---

## 8. Piste : durcir les associations « achetés ensemble » (proposition, 2026-09-27)

Demande d'Ilies : obtenir des associations de meilleure qualité. Rien n'est
implémenté ; à valider sur les données de prod avec
[upsell-associations-comparaison-prod.sql](upsell-associations-comparaison-prod.sql).

### Faiblesses des règles actuelles

- **Seuils bas** : 5 commandes ensemble suffisent, avec un lift ≥ 1,0 (donc
  simplement pas moins que le hasard) et P(B | A) ≥ 10 %.
- **Classement par lift** : le lift favorise les produits rares. Exemple
  (staging) : Pizza Jambon → Oasis 2L, lift 5,25, mais seulement **5 %** des
  clients qui prennent cette pizza ajoutent une Oasis. Pour savoir si une
  suggestion sera acceptée, c'est P(B | A) qui compte.
- **Petits échantillons pris au pied de la lettre** : 6 commandes sur 6 donnent
  100 %.
- **Scores additionnés** dans le service : avec 2 produits au panier, deux
  lifts de 1,0 font 2,0 et franchissent le seuil `minLift` = 1,5, qui porte
  sur la somme.
- **Associations entre deux plats** (une pizza → une autre pizza) : fréquentes
  dans les commandes familiales, discutables comme suggestion.

### Proposition

| Paramètre | Actuel | Proposé |
|---|---|---|
| Commandes ensemble minimum | 5 | 8 |
| Lift minimum | 1,0 | 1,2 |
| P(B \| A) minimum | 10 % (brute) | 15 % (lissée) |
| Classement | lift | P(B \| A) lissée |
| Plusieurs produits au panier | somme des lifts, seuil 1,5 sur la somme | meilleure P(B \| A) parmi les produits du panier, sans seuil sur une somme |
| Score renvoyé | lift / 5 | P(B \| A) lissée (déjà entre 0 et 1) |

P(B | A) lissée = (commandes A+B + α × part de B) / (commandes A + α), avec
α = 10 : sur peu de commandes, la valeur est ramenée vers la fréquence
moyenne de B.

Question ouverte : exclure les associations entre deux produits de la
**même catégorie**, en utilisant la catégorie comme simple étiquette. La
requête de comparaison affiche la colonne `meme_categorie` pour en juger.

Conséquence attendue : moins d'associations, mais plus fiables. Les places
libérées sont reprises par les petits prix (D10). La requête A2 mesure cette
perte de couverture.

### Résultats de prod (2026-09-28) et analyse

Ilies a répondu **oui** à l'exclusion des associations dans une même
catégorie.

Couverture (produits vendus ayant au moins 1 / au moins 3 associations) :

| Établissement | ≥ 1 actuel | ≥ 1 proposé | ≥ 3 actuel | ≥ 3 proposé |
|---|---|---|---|---|
| 212 | 66 | 33 | 22 | 4 |
| 236 | 44 | 37 | 38 | 25 |
| 235 | 41 | 27 | 29 | 8 |
| 226 | 24 | 9 | 5 | 0 |
| 234 | 21 | 6 | 10 | 0 |

Constats :

- **Classer par P(B | A) lissée est nettement meilleur.** En classant par
  lift, les petits échantillons passent en tête : Tenders → Bouchée
  Camembert (lift 8,6 sur 5 commandes), Escalope → Salade (6,4 sur 6),
  Soda → Brochette mixte à 18 €. En classant par P(B | A), on obtient des
  liens concrets : Escalope → Soda (59 %), Bricks → Assiette Keftaji (37 %),
  Tenders → Frites (38 %).
- **Le seuil de 15 % est trop strict** : il écarte des associations
  spécifiques et pertinentes, par exemple chez 212 Montagnarde → Coca
  (12 %, lift 2,1), Pizza au Bœuf → Coca (11 %, lift 2,0) et Pizza Poulet →
  Coca (10 %, lift 1,8).
- **Le lift minimum de 1,2 écarte surtout la boisson « universelle »**
  (Pizza fromage → Orangina, lift 1,16). Ce n'est pas un problème :
  l'Orangina est de toute façon en tête de la liste petits prix de 212. Les
  rôles se séparent bien : les associations portent les liens spécifiques,
  les petits prix portent les ajouts génériques.
- **Frais de livraison.** Chez 235, les produits « Zone 1/2/3 » (0 €,
  présents dans 31 % des commandes) passent **en tête** avec le classement
  par P(B | A) : ils sont 1er pour 5 des 10 produits les plus vendus. Sur le
  POS, aucun drapeau de canal ne les retire.
- **Même catégorie**, confirmé par les données : pizza → pizza (212, 235),
  Menu Tacos 1 → Menu Tacos 2 (226). Pertes collatérales : Frites ↔ Tenders
  (226, « Tex Mex »), mini-brochettes entre elles (234), Bricks → Salade
  Méchouia (236). En partie reprises par les petits prix.
- **Boisson → plat** : Orangina → pizza (212), Soda → escalope à 16 €
  (234). Si un plat de cette catégorie est déjà dans le panier, proposer un
  deuxième plat est peu pertinent.

Proposition révisée (à valider) :

- lift ≥ 1,2, au moins 8 commandes ensemble, **P(B | A) lissée ≥ 10 %**, et
  classement par P(B | A) lissée ;
- exclure une suggestion dont la catégorie est **déjà présente dans le
  panier**, pas seulement celle du produit source : cela couvre pizza →
  pizza et aussi Orangina → pizza quand une pizza est déjà prise. Cette
  règle ne s'applique qu'aux associations : les petits prix peuvent toujours
  proposer une deuxième boisson ;
- exclure des associations les produits à 0 € (frais de livraison). Cela
  revient sur l'arbitrage P6 : une sauce offerte ne serait plus proposée non
  plus. À trancher par Ilies.

### D12 — Associations durcies (validé le 2026-09-28)

Arbitrages d'Ilies sur la proposition révisée :

1. Seuils et classement : **validés**. Au moins 8 commandes ensemble,
   lift ≥ 1,2, P(B | A) lissée ≥ 10 %, classement par P(B | A) lissée.
2. Produits à 0 € : **pas d'exclusion**. Ilies demandera aux commerçants
   concernés de retirer les frais de livraison de la borne et de SNO ; le
   filtre par canal (D9) les écartera alors. Sur le **POS**, qui n'a pas de
   drapeau de canal, ils peuvent encore être suggérés (235 : « Zone 1/2 »).
   Risque connu et accepté. ~~Les produits groupe (à 0 € en base) ne sont
   jamais traités comme gratuits : leur prix est celui de leurs variantes et
   les variantes restent rattachées au groupe.~~ **Interprétation erronée,
   corrigée par D13** : la consigne « hors groupes de produit qu'il faut
   exclure mais garder les sous-produits » voulait dire que les groupes ne
   doivent jamais être proposés, et leurs sous-produits si.
3. Catégories déjà dans le panier : **validé**.

### Implémentation (D12)

- **Cron** ([internal/tasks/upsell.go](../internal/tasks/upsell.go)) :
  - nouveaux seuils `upsellMinCoOccur` = 8, `upsellMinLift` = 1,2 et
    `upsellMinConfidence` = 0,10, ce dernier portant désormais sur la valeur
    lissée ;
  - `upsellConfidenceBeta` = 10 : poids du lissage, en commandes ;
  - `upsellSmoothedConfidence` calcule P(B | A) lissée =
    (A+B + 10 × part de B) / (A + 10) ;
  - `upsellPairPatterns` applique les règles à une paire, dans les deux sens ;
  - `sortUpsellPatterns` classe par P(B | A) lissée et garde 10 suggestions
    par produit. Correction au passage : l'ancien code ne triait rien quand
    un produit avait 10 associations ou moins, l'ordre était donc celui de la
    requête ;
  - `PatternEntry.Confidence` contient désormais la valeur **lissée** ;
    `Lift` et `Support` restent stockés pour le diagnostic.
- **Service** ([internal/modules/upsell/service.go](../internal/modules/upsell/service.go)) :
  - `cartCategories` donne les catégories du panier, une variante prenant
    celle de son groupe. Les produits sans catégorie sont ignorés : n'avoir
    aucune catégorie n'est pas un point commun ;
  - `aggregatePatterns` garde pour chaque candidat la **meilleure** P(B | A)
    parmi les produits du panier (un maximum, plus une somme de lifts), et
    écarte les candidats d'une catégorie déjà présente dans le panier ;
  - le score renvoyé est cette valeur, déjà comprise entre 0 et 1 ;
  - suppression de `minLift` (seuil sur la somme) et de `normalizeScore` ;
  - le prompt LLM reçoit `confidence` au lieu de `lift` dans
    `frequent_pairs`.
- **Hors associations, rien ne change** : les petits prix peuvent toujours
  proposer un produit d'une catégorie déjà présente dans le panier (par
  exemple une deuxième boisson).

### Tests (D12)

| Test | Ce qu'il vérifie | Exécuté |
|---|---|---|
| `TestUpsellSmoothedConfidence_…` | un 6 sur 6 tombe à 38 % ; sur un gros échantillon le lissage change peu | ✅ |
| `TestUpsellPairPatterns_Rules` | deux sens, moins de 8 commandes ensemble, lift < 1,2 (boisson présente partout), un seul sens | ✅ |
| `TestSortUpsellPatterns_ByConfidenceThenID` | le plus gros lift ne passe plus devant ; égalités départagées par id ; plafond | ✅ |
| `TestCartCategories_…`, `TestAggregatePatterns_…` | catégorie d'une variante, produits sans catégorie ; maximum et non somme, pas de deuxième pizza | ✅ |
| `TestProcessUpsellPatternsForMerchant_Postgres` (réécrit) | 8 × A+B et 4 × C → 2 associations (lift 1,5) | ✅ sur staging |
| `TestProcessUpsellPatternsForMerchant_VariantsRolledUp_Postgres` (réécrit) | 4 × A+V1, 4 × A+V2, 4 × C → A↔G ; aucune paire A+Vn n'atteint 8 | ✅ sur staging |
| Vérification ponctuelle, test temporaire supprimé | calcul complet sur les données de staging (212 et 2), en lecture seule, sans Redis : pas d'erreur (22 et 2 associations orientées, chiffres non exploités, D4) | ✅ |

Les mêmes 4 paquets en échec qu'avant (`planning/employees`,
`planning/leave`, `planning/swaps`, `ubereats`), sans lien avec ce
chantier. La requête de comparaison utilise maintenant les seuils retenus
(10 %).

### Déploiement (D12)

Rien de plus que pour D1 à D11 : lancer `POST /admin/upsell/recompute-patterns`
après le déploiement. Tant que ce n'est pas fait, les associations de la
nuit précédente (anciennes règles, `Confidence` brute) restent en place. Le
service les lit sans erreur, avec le maximum et le filtre de catégorie.

---

## 9. D13 — Plus jamais de produit groupe, ses variantes à la place (2026-09-28)

### Constat

Réponse SNO transmise par Ilies (212) : le moteur propose « Coca Cola »
(584) et « Cristalline » (2332), deux **produits groupe**. Un groupe n'est
qu'un conteneur : prix 0, aucune configuration, et dans cette réponse aucun
sous-produit à choisir. Le catalogue SNO ne les affiche d'ailleurs jamais :
il les remplace par leurs sous-produits (`scannorder/service.go`, mise à
plat des groupes). C'était aussi le sens de la consigne mal lue en D12.

### Décision (Ilies)

Un produit groupe ne doit **jamais** être proposé, ni par les associations
ni par le complément. Ses sous-produits (variantes) peuvent l'être.

### Choix d'implémentation

- **Candidats** (`menu.ListAvailableProductsForUpsell`) :
  - groupes exclus (`is_product_group`) ;
  - variantes incluses si leur groupe est lui-même disponible, actif et au
    bon statut (s'il est retiré de la carte, ses variantes aussi) ;
  - une variante sans catégorie ou sans image prend celles de son groupe ;
  - nouveau champ `GroupID`.

  Vérifié en lecture seule sur staging : 584 et 2332 ne sont plus candidats ;
  212 compte 271 candidats dont 18 variantes, avec leur catégorie.
- **Panier** : une variante du panier exclut toutes les variantes de son
  groupe (Coca 33cl dans le panier : pas de Coca 1.25L ni de Coca Zero). Des
  horaires posés sur le groupe s'appliquent à ses variantes.
- **Associations** : toujours calculées par groupe (D1, statistiques plus
  solides). Une association vers un groupe propose **sa variante la moins
  chère** parmi les candidats (`cheapestVariants`) : c'est le plus petit
  ajout, dans l'esprit de l'upsell. À prix égal (chez 212, Coca Cola,
  Cherry et Zero sont tous en 33 cl à 1,90 €), la plus vendue l'emporte
  (ventes connues grâce à la liste petits prix), puis l'ordre alphabétique.
  Sans ce départage, un tri par id en texte aurait choisi le Coca Zero
  (« 2340 » < « 556 »).
- **Petits prix** (`computeUpsellLowPriceList`) : le catalogue suit les
  mêmes règles que les candidats. Chaque produit y garde son prix et ses
  ventes, comptées **sans** rattachement aux groupes, par une requête dédiée.
  Les listes calculées avant ce changement peuvent contenir des groupes :
  ils ne sont plus candidats, donc simplement ignorés.
- **Secours `is_popular`** : `ListFeaturedProducts` exclut les groupes.
- Rien ne change côté clients : SNO, la borne et le POS reçoivent déjà des
  variantes comme produits ordinaires (même forme que dans leur catalogue).

### Tests (D13)

| Test | Ce qu'il vérifie | Exécuté |
|---|---|---|
| `TestCheapestVariants` | variante la moins chère ; à prix égal la plus vendue, puis le nom (le Coca classique, pas le Zero) ; produits simples ignorés | ✅ |
| `TestAggregatePatterns_GroupTargetBecomesItsVariant` | une association vers un groupe score sa variante ; catégorie du panier appliquée à la variante | ✅ |
| `TestComputeUpsellLowPriceList_Postgres` (réécrit) | le groupe n'apparaît jamais ; ventes par variante tirées des commandes ; 0 €, indisponible et au-dessus du seuil écartés | ✅ sur staging |
| `TestUpsellRepository_Postgres` (complété) | un groupe marqué `is_popular` n'est pas renvoyé par le secours | ✅ sur staging |
| Vérification ponctuelle, test temporaire supprimé | candidats réels de 212 sur staging : ni 584 ni 2332, variantes présentes avec leur catégorie | ✅ |

Les 4 paquets en échec habituels (`planning/employees`, `planning/leave`,
`planning/swaps`, `ubereats`) ne bougent pas. Aucune ligne `itest` restante
sur staging.

Non testé : le filtre des candidats dans `generateUpsellSafe` (variantes
sœurs, horaires du groupe) est écrit en ligne dans le service, qui n'a pas
de test de bout en bout (pas de Redis de test).

### Déploiement (D13)

- Aucune migration.
- Lancer `POST /admin/upsell/recompute-patterns` après le déploiement : la
  liste petits prix est recalculée par variante. En attendant, l'ancienne
  liste reste lue sans erreur ; ses groupes sont ignorés.
- Les résultats en cache (30 min) peuvent encore contenir un groupe
  jusqu'à expiration.

---

## 10. Journal

- **2026-09-27** : état des lieux du code et analyse des données staging
  en lecture seule (outil `go run` jetable, hors dépôt). Décisions D1 à D4.
  Document créé.
- **2026-09-27** : vérification côté SNO de l'id envoyé dans le panier
  (variante). Implémentation de D1 et D2. Décisions D5 à D7, prises pendant
  l'implémentation. Tests unitaires et d'intégration (staging) au vert.
  Requêtes de prod pour D3 écrites dans [upsell-analyse-prod.sql](upsell-analyse-prod.sql),
  syntaxe validée sur staging dans une transaction en lecture seule
  (résultats non exploités, D4). Rien n'est commité ni déployé.
- **2026-09-27** : résultats de prod reçus et analysés (§6). Propositions P1
  à P6 en attente de validation. Pas de code sur ordre explicite (« ne code
  pas tout de suite »).
- **2026-09-27** : échanges avec Ilies. Arbitrages : P1 rejetée, P6 modifiée,
  décisions D8 à D11 ; (b), (c) et les associations entre catégories sont
  reportés ; (d) laissé en l'état. Constats en cours de route : la borne
  plafonnait à 3, SNO ne vérifiait pas `is_available_on_sno`, et
  `is_popular` est calculé par un cron.
- **2026-09-27** : implémentation de D8 à D11 (§7). Tests unitaires au vert ;
  tests d'intégration upsell et tasks au vert sur staging ; vérification
  ponctuelle en lecture seule des drapeaux de canal. Rien n'est commité ni
  déployé. Le chantier « options populaires » reste à lancer séparément.
- **2026-09-27** : proposition de durcir les associations ; requête de
  comparaison écrite (§8), syntaxe validée sur staging.
- **2026-09-28** : résultats de prod reçus et analysés (§8). Proposition
  révisée : 10 % au lieu de 15 %, catégories du panier, produits à 0 €.
  Arbitrages d'Ilies → D12 (0 € non exclus). Implémentation, tests unitaires
  et d'intégration au vert sur staging. Rien n'est commité ni déployé.
- **2026-09-28** : Ilies signale des produits groupe proposés sur SNO (584
  Coca Cola, 2332 Cristalline). Mon interprétation de sa consigne en D12
  était fausse. Décision D13 : jamais de groupe, ses variantes à la place
  (§9). Implémentation, tests unitaires et d'intégration au vert sur
  staging, vérification en lecture seule des candidats réels de 212. Non
  commité, non déployé.

