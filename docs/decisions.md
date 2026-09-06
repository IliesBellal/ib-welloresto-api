### PROMPT 24 Phase 5 — Pool analytique : 2→4 connexions, work_mem 16→8 Mo (2026-09-06)

- **`internal/database/postgres.go`** : `AnalyticsMaxOpenConns` 2→4,
  `AnalyticsWorkMemMB` 16→8, dans le même commit — le calcul du prompt tient
  exactement : 4 connexions × ~4 nœuds de tri × 8 Mo = 128 Mo, + 64 Mo de
  `shared_buffers` = 192 Mo, **identique** au budget actuel (2×4×16 = 128 Mo
  + 64 Mo = 192 Mo). Deux fois plus de connexions concurrentes pour le même
  plafond mémoire, pas une hausse de mémoire.
- **Mesure du spill, pas supposée** : un test Go temporaire (supprimé après
  usage, jamais commité) réutilise verbatim les fonctions de construction de
  requête déjà en production (`AnalyticsOrdersScope`, `htLineJoins`/
  `htLineExpr`, `optionsCombinedCTE`, `productsSortColumn`/
  `optionsSortColumn`) — pas de SQL retranscrit à la main, donc le plan
  mesuré est provablement la même requête que la production exécute. Exécuté
  avec les VRAIES constantes post-Phase-5 (`SET LOCAL work_mem = '8MB'`,
  `statement_timeout = 4000`, via `Repository.runTx`), contre le staging
  Render, sur l'établissement le plus lourd **réellement présent sur
  staging** (vérifié par requête, pas supposé depuis PROD) :
  établissement 212, 34 440 lignes `orderitems` — coïncidence avec le
  "merchant 212" cité comme le plus gros de PROD dans les commentaires du
  code, pas une hypothèse reprise sans vérifier. Fenêtre all-time
  (2000-01-01 → aujourd'hui), taille de page élargie à 200 (au-delà du
  défaut 50) pour forcer le pire cas.
  - **Options (`GetOptionsPage`)** : 1089 ms, aucun spill — chaque `Sort
    Method` est `quicksort`, chaque `HashAggregate`/`Hash` a `Batches: 1`,
    pic mémoire mesuré ~1,9 Mo (`Memory Usage`), très en dessous de 8 Mo.
  - **Produits (`GetProductsPage`)** : 1474 ms, aucun spill — même profil,
    pic mémoire ~3,3 Mo.
  - Aucune ligne `external merge` / `temp read` / `temp written` /
    `Disk:` dans les deux plans complets (`EXPLAIN (ANALYZE, BUFFERS)`).
  - **Conclusion : pas de débordement disque à 8 Mo**, sur les deux
    requêtes les plus lourdes du paquet, à la volumétrie réelle la plus
    haute présente sur staging. Aucun arbitrage à faire pour l'instant — si
    une requête future spillait à 8 Mo, ce serait un vrai arbitrage
    connexions/mémoire à trancher, pas un seuil à relever en silence (les
    deux constantes portent maintenant ce rappel dans leur commentaire).
- **Durées à 1, 2, 3 établissements** (même mesure temporaire, requêtes
  contre les 3 établissements staging les plus lourds — 212, 223, 2 —
  fenêtre 12 mois, mesurées au niveau requête/DB, pas bout-en-bout HTTP :
  `cmd/analytics_bench` existe déjà pour ça mais exige des jetons bearer par
  établissement indisponibles dans cet environnement ; le coût dominant est
  la requête DB, pas le HTTP par-dessus) :
  | Établissements | Options | Produits | CA (totaux) |
  |---|---|---|---|
  | 1 | 715 ms | 955 ms | 172 ms |
  | 2 | 601 ms | 1005 ms | 179 ms |
  | 3 | 695 ms | 927 ms | 198 ms |

  Options et Produits restent globalement plats (dominés par le volume de
  l'établissement le plus lourd du trio, pas par leur nombre — la variance
  observée est du bruit de mesure sur du staging partagé, pas une tendance).
  Le CA croît légèrement et régulièrement (172→198 ms), cohérent avec un
  `COUNT`/`SUM` dont le coût suit le volume de lignes scannées. Aucune des
  trois n'approche le fusible de 4000 ms.
- **`cmd/analytics_bench/main.go` corrigé en passant** : `explainSpill`
  utilisait un `work_mem` codé en dur à `16MB` — un vrai bug fonctionnel une
  fois Phase 5 déployée (l'outil aurait mesuré le spill sous le MAUVAIS
  `work_mem`, pas celui réellement en production). Lit maintenant
  `database.AnalyticsWorkMemMB`/`AnalyticsMaxOpenConns` directement, plus
  aucune figure recopiée à la main dans ce fichier.
- **Vérifié** : `go build ./...`, suite `postgres_integration` complète du
  paquet analytics contre staging (aucune régression — le paquet ne dépend
  pas de la valeur exacte des constantes, seulement `Repository.runTx` qui
  les applique déjà par construction), suite unitaire, aucun fichier de
  mesure temporaire laissé dans le dépôt.

### PROMPT 24 Phase 4 — Graphiques à N séries (wello-back-office, front seul) (2026-09-06)

- **`src/utils/merchantColors.ts`** : `merchantColor(merchantId)`, même
  contrat que `channels.ts`'s `channelColor` — fonction pure, dérivée de
  l'identifiant, jamais du rang dans le tableau affiché. Hash de chaîne
  (djb2-like) plutôt qu'un modulo sur l'entier brut : des `merchant_id`
  séquentiels ("212"/"213") tomberaient sinon trop souvent sur des couleurs
  adjacentes de la palette.
- **Palette délibérément disjointe de `CHANNEL_COLORS`** : violet/pink/
  indigo/fuchsia/rose/purple contre le bleu/vert/ambre/cyan/teal des
  canaux — deux échelles catégorielles distinctes, vérifié à l'œil (aucun
  chevauchement de teinte) plutôt que réutiliser/éclaircir la palette
  existante.
- **`EstablishmentComparisonChart.tsx`** (composant partagé, un seul
  endroit pour la logique couleur/légende) : un graphique à barres
  horizontales, une barre par établissement — pas un graphique multi-lignes,
  parce que `by_merchant` sur les 5 onglets comparables est un total par
  établissement sur la période, pas une série temporelle par établissement
  (contrairement au `by_channel_ttc_cents` journalier de CA/Commandes). Une
  ligne aurait supposé une donnée qui n'existe pas côté backend. Légende
  toujours rendue (`Legend payload={...}`, pas la légende par défaut de
  Recharts qui ne sait pas construire une entrée par `Cell` sur un seul
  `dataKey`).
- **Remplace les tableaux simples posés en Phase 3** sur les 5 onglets
  comparables (CA, Commandes, Règlements, TVA, Annulations) — même
  composant partagé partout, seuls la valeur et son formatage changent par
  onglet.
- **Vérifié, précisément sur le point que le prompt signale comme piège
  classique** : test interactif (mêmes conditions que la Phase 3 — session
  factice, hôte API réel bloqué) avec 5 établissements sélectionnés, lecture
  directe des couleurs SVG (légende + barres) avant/après désélection du
  PREMIER établissement de la liste. Résultat : chaque établissement restant
  garde exactement sa couleur (`COLOR STABILITY CHECK: PASS`, comparaison
  automatisée des couleurs par nom d'établissement avant/après) — la
  désélection ne décale aucune couleur, contrairement à ce qu'un
  index-dans-le-tableau-affiché aurait produit. Capture d'écran à 5 séries :
  cinq barres et cinq entrées de légende toutes visuellement distinctes,
  lisibles, sans chevauchement avec la palette canaux visible sur le même
  écran (graphique "Évolution CA" juste en dessous). Fixtures de mock
  repassées à leur état d'origine (1 établissement, `scope` non dynamique)
  après vérification.
- **`tsc --noEmit`, `vite build`, ESLint** : propres (mêmes warnings
  `exhaustive-deps` pré-existants qu'aux phases précédentes, aucune erreur).

### PROMPT 24 Phase 3 — Le sélecteur (wello-back-office, front seul) (2026-09-06)

- **Nouveau composant `EstablishmentFilter.tsx`**, global, à côté du filtre de
  période dans `DashboardAnalysis.tsx` — pas le pattern par-onglet de
  `ChannelFilter` (PROMPT 18) : la portée établissements est commune à toute
  la page, comme la période, donc l'état vit dans `DashboardAnalysis`, pas
  dans chaque onglet. Chips (`SelectableChip`, déjà utilisé pour tags/
  allergènes) + `ToggleGroup` (cumulé/comparé) pour le mode.
- **Rendu conditionnel** : la carte "Établissements" (et le rappel de portée
  dans chaque onglet) ne s'affiche que si `accessibleMerchants.length > 1` —
  un compte à un seul établissement n'a rien à filtrer ni à comparer, la
  carte serait un rappel d'une évidence sur chaque onglet.
- **Sélection par défaut** : l'établissement du token
  (`authData.session.merchant_id`, déjà utilisé par `Header.tsx`), avec repli
  sur le premier de la liste si absent — décision du prompt appliquée
  littéralement.
- **Bascule automatique en comparé** : passer de 1 à 2 établissements
  sélectionnés bascule `comparisonMode` sur `'compare'`
  (`handleMerchantSelectionChange` dans `DashboardAnalysis.tsx`) — la
  bascule ne se redéclenche qu'au FRANCHISSEMENT du seuil (1→2), pas à
  chaque changement, pour que revenir de 3 à 2 établissements ne réinitialise
  pas un choix "cumulé" déjà fait par l'utilisateur. Le sélecteur de mode
  est désactivé (`disabled`, jamais masqué) sous 2 établissements sélectionnés.
- **`ScopeSummary`/`AggregationNotice`** (`ScopeNotice.tsx`, partagé) :
  chaque onglet affiche les établissements réellement renvoyés par
  `scope.merchant_ids` (jamais ceux demandés), et les 5 onglets non
  comparables (Produits, Options, Clients, Remises, Vente additionnelle)
  affichent un bandeau explicite en mode comparé à 2+ établissements — "cet
  onglet agrège", jamais un mode qui semble actif sans effet visible.
- **`merchant_ids` maintenant envoyé partout**, pas seulement sur les 5
  onglets comparables : `analyticsService.ts` ne l'envoyait auparavant sur
  AUCUN appel réel (vérifié au prompt 23 — seul `scope` en lecture existait
  dans les types TS). Les 5 onglets non comparables et les 3 blocs
  nominatifs (`getClientsTop`, `getUpsellByStaff`,
  `getCancellationsByStaff`) le reçoivent aussi désormais — un bloc nominatif
  doit continuer à se masquer sur 403 quand le droit manque sur un seul des
  établissements sélectionnés (déjà géré côté serveur par
  `requireKeyOnAllMerchants`, PROMPT 23 Phase 2 ; ce lot fait juste en sorte
  que la sélection réelle de l'utilisateur atteigne enfin ces 3 endpoints).
- **Phase 4 non anticipée délibérément** : les 5 onglets comparables
  affichent un tableau simple "par établissement" (nom, total, compte) sous
  les graphiques existants quand `group_by=merchant`, pas encore les
  graphiques à N séries avec couleurs stables — c'est explicitement Phase 4
  ("les graphiques à N séries"), une phase séparée dans le prompt.
- **Vérifié en conditions quasi réelles** : `tsc --noEmit` et `vite build`
  propres ; ESLint ne remonte que des warnings `exhaustive-deps` de la même
  nature que ceux déjà présents ailleurs dans ce fichier (aucune erreur).
  Vérification interactive : serveur de dev Vite lancé avec
  `VITE_USE_MOCK=true`, session `localStorage.authData` factice
  (`access.admin=true`) pour passer `ProtectedRoute` sans backend réel, hôte
  API réel (`welloresto-api-staging.onrender.com`) bloqué via interception
  réseau Playwright pour éviter qu'un 401 sur un appel non mocké
  (`/me/permissions`, hors `withMock`) ne vide la session factice
  (`clearAuthAndRedirect`). Avec 2 établissements mockés temporairement :
  carte "Établissements" visible avec ses 2 chips, sélection du second
  bascule le mode sur "Comparé" (confirmé), désélection revenant à 1 désactive
  le bouton "Cumulé" (confirmé), le bandeau d'agrégation apparaît sur l'onglet
  Produits en mode comparé (confirmé, capture d'écran). Fixture mock repassée
  à 1 établissement après vérification — aucun changement de test laissé en
  place.

### PROMPT 24 Phase 2 — `group_by` sur Règlements, TVA, Annulations (2026-09-05)

- **Règlements** : `PaymentsRequest.GroupBy` + `PaymentsResponse.ByMerchant`
  ([]`PaymentsMerchantTotal`), même schéma que CA/Commandes —
  `Repository.GetPaymentsByMerchant` fait un simple `GROUP BY o.merchant_id`
  sur `paymentsScopeJoin` (même filtre `payments.enabled = TRUE`, mêmes
  jointures que `GetPaymentsTotals`). Aucun apportionnement requis (SUM/COUNT
  directs, pas de figure dérivée).
- **Annulations** : `GroupBy` ajouté à `CancellationsRequest`
  uniquement (l'endpoint d'agrégats) — le classement nominatif par serveur
  (`/cancellations/by-staff`) reste fusionné quel que soit le mode, décision
  explicite du prompt, non touché par ce lot. Deux nouvelles requêtes
  groupées par `merchant_id` (`GetOrdersCreatedCountByMerchant` — dénominateur
  du taux, `GetCancellationsTotalsByMerchant`), fusionnées côté service
  (`Service.cancellationsByMerchant`) puisqu'elles portent sur deux scopes SQL
  différents (tout ce qui a été créé vs. seulement ce qui a été annulé) —
  un établissement avec commandes mais 0 annulation doit apparaître avec
  `CancelledCount: 0`, pas disparaître d'une jointure interne.
- **TVA — le point d'attention du prompt.** `apportionVATByRate`/
  `apportionVATByChannel` (le "plus fort reste" du PROMPT 06 §1) tournent
  maintenant **une fois par établissement** en mode comparé
  (`Service.buildVATByMerchant`), chacune contre le `TotalHTCents` **propre à
  cet établissement** (`Repository.GetVATTotalsByMerchant`, une ROUND()
  indépendante par établissement) — jamais contre le total combiné du
  périmètre. Trois nouvelles requêtes groupées par `merchant_id` en plus de
  `rate`/`channel` (`GetVATTotalsByMerchant`, `GetVATByRateByMerchant`,
  `GetVATByChannelByMerchant`), même UNION ALL produit×frais de livraison que
  les versions non groupées.
- **Vérifié, précisément sur ce point** :
  `TestGetVAT_GroupByMerchant_PartsSumToOwnTotal_Postgres` construit 2
  établissements à des totaux HT délibérément différents (1000 exact vs. un
  mélange 20%/10% avec reste réel) et vérifie que le `by_rate`/`by_channel`
  de **chacun** somme exactement à **son propre** `TotalHTCents`/
  `TotalVATCents` — pas au total combiné. `apportionCents` force sa sortie à
  sommer exactement à la valeur qu'on lui passe : ce test aurait donc échoué
  immédiatement si l'implémentation avait apportionné contre le total combiné
  au lieu du total par établissement (l'établissement à une seule part se
  serait vu attribuer 100% du total combiné au lieu de son propre total).
- **Limite connue, non cachée** : `VATMerchantTotal.TotalHTCents` est
  arrondi indépendamment par établissement (une `ROUND()` par groupe), alors
  que le total combiné du scope est arrondi une seule fois sur la somme brute
  — un écart de ±1 centime entre `Σ(TotalHTCents par établissement)` et le
  total cumulé non groupé est mathématiquement possible (le problème classique
  `round(a)+round(b) ≠ round(a+b)`), même si le jeu de test actuel ne
  l'exhibe pas. `TotalTTCCents`, lui, reste une somme entière exacte (jamais
  dérivée), donc la réconciliation TTC "somme des séries comparées = total
  cumulé" tient toujours au centime près — c'est HT/VAT spécifiquement, déjà
  une figure dérivée avant ce lot, qui hérite de cette limite d'arrondi
  indépendant. Pas un choix caché : signalé ici pour arbitrage si un client
  fait un jour ce rapprochement au centime.
- Reconciliation en plus pour les 4 autres tabs comparables :
  `TestGetPaymentsByMerchant_Postgres`/`TestGetCancellationsByMerchant_Postgres`
  (parts = total non groupé, exact), plus les tests bout-en-bout déjà
  existants pour CA/Commandes (`TestGetOrders_GroupByMerchant_EndToEnd`)
  étendus au même schéma pour Règlements et Annulations
  (`TestGetPayments_GroupByMerchant_EndToEnd`,
  `TestGetCancellations_GroupByMerchant_EndToEnd`), y compris le rejet d'un
  `group_by` invalide (`ErrInvalidRequest`).
- **Vérifié** : `go build ./...`, suite unitaire du package, suite complète
  `postgres_integration` du package contre le staging Render (58 tests, tous
  verts).

### PROMPT 24 Phase 1 — Endpoint des établissements accessibles (2026-09-05)

- **Pas de doublon de `ResolveAccessibleMerchants`.** Aucun endpoint
  n'exposait déjà la liste nommée avant ce lot — vérifié dans `handler.go` et
  `cmd/api/routes.go` (aucune route "merchants"/"establishments"). Le seul
  candidat proche, `auth.AuthRepository.GetMerchants` (alimente le sélecteur
  d'établissement au login), a une portée différente et plus large : aucun
  filtre `pos.analytics`, aucun filtre `enabled`/`login_enabled`. Le réutiliser
  aurait fuité des établissements hors du périmètre analytics — écarté.
- **Nouveau : `GET /analytics/merchants`**, gardé par `permission.POSAnalytics`
  (pas `reports.sales.read` comme le reste du groupe `/analytics` — cet
  endpoint EST la donnée du gate pos.analytics, pas un chiffre de vente).
  `Service.GetAccessibleMerchants` appelle `ResolveAccessibleMerchants` (la
  même fonction que chaque onglet, aucune seconde implémentation de la règle
  de portée) puis `Repository.GetMerchantNames` pour les libellés. Une scope
  vide renvoie `{"merchants": []}`, jamais une erreur.
- `Repository.GetMerchantNames` doit caster `merchant.id` (entier) en texte
  pour le comparer au tableau `merchantIDs` (`CAST(id AS TEXT) = ANY(?)`) —
  même contrainte que `auth.authMerchantJoinCast()`, ce package étant déjà
  Postgres-only (le `= ANY(?)` de `scope.go` le suppose partout).
- **Vérifié** : `go build ./...`, `go vet ./internal/modules/analytics/...`,
  suite unitaire du package, suite `postgres_integration` complète du package
  contre le staging Render (`RENDER_STAGING_DATABASE_URL`) — nouveau test
  `TestGetAccessibleMerchants_Postgres` inclus (établissement avec
  `pos.analytics` → visible et nommé ; établissement avec
  `reports.sales.read` seul mais pas `pos.analytics` → exclu ; établissement
  sans lien du tout → exclu ; utilisateur sans aucun accès → liste vide, pas
  d'erreur). `go test ./internal/...` fait apparaître 3 échecs pré-existants
  et sans rapport (`planning/leave`, `planning/swaps`, `ubereats`) — non
  causés par ce lot.

### PROMPT 23 lot 2 — Contrat, cache, performance (Phase 3+4+5) (2026-09-05)

- **Phase 3 — le paramètre de requête `merchant_ids` était déjà sur les 10
  endpoints** avant ce lot (`ResolveAccessibleMerchants`/
  `ValidateRequestedMerchants` déjà branchés partout, 403 strict déjà en
  place) — rien à ajouter ici, seulement à re-vérifier après l'élargissement
  de portée de la Phase 1 (fait : les tests postgres_integration du lot 1
  couvrent déjà cette validation contre le nouveau périmètre).

- **`group_by` : état réel, pas supposé.** Vérifié endpoint par endpoint —
  seuls **2 des 10** endpoints portent ce champ au contrat
  (`RevenueRequest`/`OrdersRequest`, hérité de PROMPT 03) ; les 8 autres
  (Règlements, TVA, Annulations, Produits, Options, Clients, Vente
  additionnelle, Remises) n'en ont jamais eu. **Sur les 2 qui l'ont, un seul
  fonctionnait réellement** : `GetOrders` acceptait `group_by=merchant`,
  l'échait dans `Scope.GroupBy`, l'incluait même dans la clé de cache — sans
  jamais rien calculer (`OrdersResponse` n'avait pas de champ `ByMerchant` du
  tout). Un paramètre mort, pire qu'absent : un appelant ne pouvait pas
  distinguer "ignoré" de "rien à ventiler".

  **Corrigé** : `Repository.GetOrdersByMerchant` (nouveau, même gabarit que
  `GetRevenueByMerchant` — `GROUP BY o.merchant_id` sur des `COUNT`/`SUM`
  entiers, donc les lignes somment exactement au total ungroupé, aucune
  apportion nécessaire, contrairement à un HT dérivé) + `OrdersResponse.
  ByMerchant` + validation stricte de `group_by` dans `GetOrders` (valeur
  invalide → 400, comme `GetRevenue`). **Aucun test n'existait pour
  `GetRevenueByMerchant` ni pour cette validation avant ce lot** — c'est
  précisément comme ce défaut d'`GetOrders` est passé inaperçu si longtemps ;
  3 tests postgres_integration nouveaux couvrent maintenant les deux
  endpoints, avec la vérification de réconciliation exacte
  (`sum(by_merchant) == total ungroupé`).

  **Décision de périmètre, assumée plutôt que silencieuse** : je n'ai PAS
  étendu `group_by` aux 8 autres endpoints dans ce lot. Le brief demande de
  "vérifier" l'implémentation, pas de la généraliser ; Règlements/TVA/
  Annulations partagent la forme "une série de KPI période" de CA/Commandes
  et s'y prêteraient (TVA demanderait de réutiliser `apportionCents` pour
  HT/TVA par établissement, exactement comme `by_rate`/`by_channel` le font
  déjà — travail cadré mais non trivial) ; Produits/Options/Clients/Vente
  additionnelle/Remises sont des tables paginées ou des classements, pas des
  séries — "comparé vs cumulé" n'y a pas le même sens évident et
  demanderait une décision produit propre (pagination par établissement ?
  tableaux séparés ?) hors du périmètre "backend seul, socle" de ce lot.
  Recommandation posée mais non implémentée : étendre en priorité à
  Règlements/TVA/Annulations si l'usage réel le demande.

- **Phase 4 — le cache était déjà correct.** Les 5 fonctions de clé
  (`buildCacheKey`, `buildProductsCacheKey`, `buildOptionsCacheKey`,
  `buildClientsCacheKey`, `buildDiscountsCacheKey`) triaient déjà
  `merchantIDs` avant hachage — aucune n'avait de test le prouvant. 5 tests
  unitaires ajoutés (`cache_test.go`), un par fonction : `[212,228]` et
  `[228,212]` produisent la même clé, `[212]` et `[212,228]` des clés
  différentes.

- **Phase 5 — mesure directe des requêtes (pas HTTP), via le pool analytics
  réel** (`database.NewAnalyticsPostgres`, mêmes réglages fusible que la
  prod : `statement_timeout=4000ms`, `work_mem=16MB` en `SET LOCAL`),
  contre staging, protocole 5 exécutions/1re jetée/médiane, répété deux fois.
  Bundle mesuré = exactement les 8 requêtes que `GetRevenue` exécute pour
  CA/12 mois/`include_ht=true` (3 périodes × TTC+HT, + timeline + by_channel)
  — une première version du script ne mesurait qu'une période et
  sous-comptait le coût réel d'un facteur ~3, corrigée avant de conclure.

  | Établissements | Médiane bundle (ms) | Requête la plus lente (ms) |
  |--:|--:|--:|
  | 1 (212 seul) | ~2 500–3 200 | ~800–900 |
  | 5 (top 5 volume, dont 212) | ~4 300–4 500 | ~1 400 |
  | 10 (top 10 volume, dont 212) | ~6 000–6 400 (pic isolé 8 200) | ~2 200 |
  | 20 (30 établissements staging, 20 pris, dont 212 + 19 quasi vides) | ~5 800–6 300 | ~2 700 |

  **Aucune exécution n'a déclenché le `statement_timeout`**, mais la requête
  la plus lente est passée de 800ms (1 étab.) à 2 700ms (20 étab.) — 68% du
  budget de 4000ms sur une seule requête, en ne comptant qu'**un seul**
  établissement réellement volumineux (212, ~9 600 commandes/an) parmi les
  20 : staging n'a qu'un seul établissement à ce volume, donc ce chiffre est
  un **plancher**, pas un plafond — un groupe réel avec plusieurs
  établissements aussi actifs que 212 sélectionnés ensemble dépasserait
  vraisemblablement le fusible avant 20. La latence bout-en-bout (4,3–6,4s
  dès 5 établissements) est de toute façon déjà dégradée pour un affichage
  interactif, indépendamment du fusible.

  **Plafond recommandé : 5 établissements** pour la requête la plus lourde
  (CA, 12 mois, `include_ht=true`) — marge confortable sur le fusible
  (~1,4s max par requête) tout en restant utile pour un comparatif réel.
  **Non appliqué en code dans ce lot** (le brief demande une recommandation,
  pas une garde technique — décision produit/frontend, hors périmètre
  "backend seul" de PROMPT 23). Si l'usage réel pousse au-delà de 5-10
  établissements réguliers, c'est un argument direct pour la
  pré-aggrégation déjà anticipée par `logInstrumentation`
  (`analytics_query`) plutôt que pour desserrer le fusible ou le pool à 2
  connexions.

- **Vérifié** : `go build ./...` propre, suites unitaires (`analytics`,
  `auth`, `permission`, `cmd/api`) et `postgres_integration` du paquet
  `analytics` (32s, tout vert) contre staging. Script de mesure Phase 5
  jetable (staging uniquement, jamais commité) — supprimé après usage.

### PROMPT 23 lot 1 — Socle multi-établissements : périmètre + vérification croisée (Phase 1+2) (2026-09-05)

- **Étape intermédiaire, validée avec l'utilisateur avant de poursuivre** :
  Phases 1 et 2 seules (résolution du périmètre + `Has(clé, merchant_id)`),
  Phases 3-5 (contrat sur les 10 endpoints, cache, mesures perf/plafond)
  restent à faire dans un lot suivant.

- **`ResolveAccessibleMerchants` change de nature, pas seulement de contenu** :
  passe d'une fonction pure (`internal/modules/analytics/scope.go`,
  `[]string{user.MerchantID}`, gardée par un commentaire interdisant
  explicitement d'en faire une requête) à une méthode de `Repository`
  (`repository.go`) — la portée réelle est maintenant "tout établissement où
  `users_rights` (actif : `enabled AND login_enabled`) donne `pos.analytics`
  à cet utilisateur", en **une seule requête** (`LEFT JOIN role_permissions`,
  pas de boucle par lien). L'ancien garde-fou est explicitement caduc : la
  décision produit qu'il anticipait ("multi-établissement = mécanisme
  séparé, pas encore décidé") est maintenant prise par PROMPT 23 lui-même.

- **Piège user_id vide neutralisé structurellement, pas par convention** :
  `AND ur.user_id <> ''` dans le WHERE rend impossible de fusionner les 4
  lignes `user_id=''` de staging avec le périmètre de quiconque, y compris
  dans le cas dégénéré où `userID` sondé vaudrait lui-même `""` — testé
  explicitement (`TestResolveAccessibleMerchants_Postgres`, avec 4
  établissements orphelins créés pour l'occasion) plutôt que supposé sûr par
  construction de la requête appelante.

- **Les deux mondes RBAC, dans la même requête** : `role_id` renseigné →
  jointure vers `role_permissions` (aucun court-circuit `system_key=admin`,
  cohérent avec le retrait de ce court-circuit dans `Has()` au lot 11) ;
  `role_id` NULL → repli sur `users_rights.admin` seul, puisque
  `pos.analytics` n'a **aucune** entrée dans `legacyPermissionFallback`.
  Testé séparément (`TestResolveAccessibleMerchants_BothRBACWorlds`) contre
  staging.

- **Établissement du token absent du résultat : décision assumée, pas un
  bug** — si le lien courant ne porte pas `pos.analytics`, l'utilisateur ne
  verrait de toute façon pas l'onglet Analyse côté front (même droit qui
  gate le menu). `ValidateRequestedMerchants` (inchangé) rejette alors tout
  `merchant_id` demandé avec un 403, jamais un filtrage silencieux.

- **`Has(clé, merchant_id)` construit sans toucher au repli historique** :
  `Repository.HasForMerchant` (nouvelle méthode) réplique `Has()` pour un
  établissement quelconque — une seule requête (role_id + jointure
  `role_permissions` en une fois), `Rights.Admin` sinon
  `auth.LegacyFallback(clé, rights)`, un nouvel accesseur exporté (purement
  additif) sur `legacyPermissionFallback` : réutilise la même map plutôt que
  d'en dupliquer le contenu, pour qu'il n'y ait rien à garder synchronisé à
  la main. `internal/modules/auth/permissions.go` n'est autrement pas
  modifié.

- **Test de non-divergence, exigé par le brief, vérifié contre staging** :
  `TestHasForMerchant_AgreesWithHasOnTokenMerchant` construit un
  `UserLoginRow` à la main et compare `user.Has(clé)` à
  `repo.HasForMerchant(...)` sur le **même** établissement, dans les deux
  mondes RBAC (rôle porteur/non-porteur, `admin=true`, `admin=false` avec et
  sans entrée de repli) — les deux implémentations concordent aujourd'hui.

- **Appliqué aux 3 blocs nominatifs** (Annulations/staff, Vente
  additionnelle/staff, Top clients) : `Service.requireKeyOnAllMerchants`
  vérifie `reports.staff_performance.read` (ou `customers.manage` pour Top
  clients) sur **chaque** `merchant_id` de la portée validée, avant de
  construire la réponse — sinon `ErrNominativeAccessDenied` → 403
  (`nominative_access_denied`, distinct de `merchant_not_accessible`).
  Aucun classement partiellement amputé.

- **Vérifié contre staging (lecture seule)** : les 4 nouveaux tests
  (`scope_postgres_integration_test.go`) passent, ainsi que la suite
  `postgres_integration` complète du paquet `analytics` (34,5 s, aucune
  régression). `go build ./...` et les suites unitaires (`analytics`,
  `auth`, `permission`, `middleware`, `cmd/api`) sont vertes. Deux paquets
  non touchés par ce lot (`roles`, `planning/employees`) ont des tests déjà
  en échec sur cette copie de travail avant toute modification de ce
  lot — confirmé en relançant ces mêmes suites sans aucun changement
  d'environnement (`go test ./internal/modules/roles/...` seul reste vert ;
  l'échec n'apparaît qu'en combinant `-tags postgres_integration` avec
  `DB_DIALECT=postgres` exporté pour TOUT le paquet, y compris ses tests
  `sqlmock`, qui supposent `?` non traduit — artefact de la commande de test
  choisie pour vérifier ce lot, pas une régression qu'il introduit).
  `TestSystemAdminRolesContainFullCatalog_Postgres` échoue par ailleurs pour
  une raison réelle mais préexistante et documentée (DROITS.md §5.2) :
  `cmd/seed_system_roles` n'a jamais été relancé depuis l'ajout de
  `reports.staff_performance.read` (PROMPT 10) — sans effet sur ce lot
  (aucun rôle admin ne perd `pos.analytics`, déjà backfillé pour lot 10).

### PROMPT 22 — Onglet Remises (2026-09-05)

- **Contexte** : la maquette de cet onglet décrivait un produit qui n'existe
  pas (`discount_type`, section remises panier) — corrigé plutôt que reproduit,
  sur la base du modèle figé par PROMPT 21 (`discount_redemptions`). Un seul
  endpoint (`POST /analytics/discounts`, `reports.sales.read`) : aucune donnée
  nominative dans cet onglet.

- **Groupement par `discount_id` (integer, `discounts.discount_id_new`),
  jamais par libellé** : `internal/modules/analytics/discounts.go` grouoe
  `GetDiscountsPage` sur `dr.discount_id` seul — un renommage ne fragmente ni
  ne fusionne jamais l'historique d'une remise. `discounts` est LEFT JOIN
  (jamais INNER) pour le libellé : une remise supprimée (`enabled=false`)
  garde ses lignes historiques visibles, avec `is_deleted=true`.

- **Aucune section remises panier** : `applyCartDiscount` n'existe nulle part
  (confirmé par PROMPT 21) — pas une section vide (qui laisserait croire à
  zéro), rien du tout.

- **Dénominateur du taux de remise moyen, tranché explicitement** :
  `total_discounted_cents / reference_revenue_ttc_cents`, où le second est le
  CA TTC de **toute** la période (commandes remisées ou non), filtré par
  canal — pas le CA des seules commandes remisées (l'autre choix également
  défendable). Motif : répond à "quelle part de ce qui a été encaissé a été
  remisée", directement comparable à la tuile CA des autres onglets. Le champ
  `reference_revenue_ttc_cents` est toujours renvoyé à côté, jamais un taux nu.

- **Seuil de matérialité réutilisé verbatim** : `discountsMinOrdersForRate =
  staffCancellationMinOrders` (30, cancellations.go) gate `DiscountRatePercent`
  ET `OrdersWithDiscountRatePercent` sur `DiscountedOrdersCount` — en dessous,
  les deux sont absents (jamais 0), mais les volumes bruts et le volume de
  référence restent toujours affichés (`discounted_orders_count`,
  `total_orders_count`, `reference_revenue_ttc_cents`).

- **Impact sur la marge, au contrat, à `null` en pratique** : `DiscountsMarginCoverage`
  reprend le contrat `ProductsCostCoverage` à l'identique (même seuil de
  couverture 20%, `coversCoverageThreshold`), restreint aux redemptions
  `scope=PRODUCT_LINE` jointes à `orderitems.cost_price_unit` — une remise
  `CART` ne peut être rattachée à aucune ligne costée, donc exclue du
  numérateur ET du dénominateur (jamais un faux zéro). `cost_price_unit` étant
  `null` sur la quasi-totalité des lignes (PROMPT 07 lot 1), l'écran affiche
  "non disponible", jamais un montant.

- **Plancher vs mesure complète, porté par `is_reconstructed` dans le
  contrat lui-même, pas en note de page** : chaque période distingue
  `reconstructed_amount_cents`/`measured_amount_cents` (et leurs comptes de
  redemptions). `MeasurementCompleteFrom` (nouveau, `GetDiscountsMeasurementCompleteFrom`)
  expose la date de bascule réelle — `MIN(created_at)` parmi les lignes
  `is_reconstructed=false`, **non bornée à la période demandée** (fait global
  sur l'établissement, pas une fenêtre de rapport) — `nil` tant qu'aucune
  écriture en direct n'existe. Le frontend affiche un bandeau "Montant
  plancher, pas un total" tant qu'une part de la période est reconstituée,
  avec la date de bascule ou l'absence totale de mesure directe selon le cas
  — jamais une mention en pied de page.

- **Vérification en lecture seule, staging, merchant 212, 12 mois
  (2025-09-05 → 2026-09-05, transaction READ ONLY)** : montant total remisé
  110 840 centimes, intégralement reconstitué (367 redemptions, 0 mesuré en
  direct) — la répartition par remise (une seule remise réelle sur ce
  périmètre, "2 pizzas pour 12€") somme exactement au total. 186 commandes
  remisées sur 9 619 (1,93 %), CA de référence 16 016 930 centimes, taux de
  remise moyen 0,69 %. Marge : couverture 0 % (`cost_price_unit` jamais
  renseigné sur ces lignes), donc "non disponible" comme attendu. **Fait
  notable, vérifié à l'échelle globale (tous établissements)** : les 545
  lignes de `discount_redemptions` sont *toutes* `is_reconstructed=true` —
  aucune écriture en direct n'a encore eu lieu nulle part
  (`upsertOrderItemDiscountRedemption`, câblé par PROMPT 21, n'a pas encore
  tourné une seule fois en conditions réelles) : `MeasurementCompleteFrom` est
  `nil` partout aujourd'hui, pas seulement sur le merchant 212. Test
  d'accuracy dédié (`discounts_postgres_integration_test.go`, jeu de données
  synthétique) rejoué avec succès contre staging : somme exacte, répartition
  qui somme au total, `COUNT(DISTINCT order_id)` correct, partition par canal
  qui somme au total, et une remise supprimée d'une commande simulée
  "rouverte" (ligne `discount_redemptions` supprimée manuellement) disparaît
  bien immédiatement de tous les agrégats — cet onglet n'a aucune logique
  d'exclusion propre, il lit `discount_redemptions` tel quel.

- **Frontend** (`wello-back-office`) : `DiscountsAnalyticsTab.tsx` remplace le
  mock `renderDiscountsTab`/`getDiscountsAnalytics` synchrone dans
  `DashboardAnalysis.tsx` (filtre type de remise/`MultiFilter` retiré avec
  lui — n'existe plus). Réutilise `ChannelFilter` (prompt 18) et le gabarit
  pagination/tri serveur de Produits/Options. `analyticsService.ts` porte les
  nouveaux types `Discounts*`/`DiscountRow` et `getDiscountsAnalytics`/
  `exportDiscountsCSV` (reconstruit côté client à partir des données déjà
  chargées, même limite que Annulations/Clients — aucun endpoint CSV backend
  n'existe). Vérifié en navigateur réel (Playwright headless, mode mock local
  `--mode smoketest`, session injectée via `localStorage.authData` pour
  contourner l'auth réelle indisponible en local) : rendu correct du bandeau
  plancher, des 4 tuiles, du tableau trié/paginé et de l'interaction de tri —
  aucune erreur console hors l'échec CORS déjà pré-existant de la
  revalidation `/me/permissions` en environnement local (non lié à ce lot).

- **Hors périmètre, comme demandé** : le modèle de remises (figé par PROMPT
  21) n'a pas été touché ; aucune fonctionnalité de remise panier construite.

### PROMPT 21 — Assainir le modèle de remises, Phases 1-3+5 (2026-09-05)

- **Contexte** : `discounts.discount_id` (varchar) et `orderitems.discount_id`
  (integer) ne peuvent structurellement pas se référencer par jointure directe
  depuis toujours (rapport 57) ; le brief demandait un plan validé avant tout
  code, puis expansion/déploiement/contraction, aucune suppression de colonne
  jouée. Recon + plan complet présentés et validés en chat avant implémentation
  (pas de rapport séparé dans `docs/` — recon + plan tiennent dans cette
  entrée, conformément au format `decisions.md`).

- **La prémisse « aucun front ne manipule discount_id » était fausse, corrigée
  avant d'agir** : vérifié dans le code que `wello-back-office` (CRUD promotions,
  `GET/PATCH/DELETE /menu/discounts/{discount_id}`) et le POS `wello_resto_flutter`
  (`product_payload.dart` renvoie `discount_id` au serveur) font transiter cette
  valeur en lecture-écriture — jamais générée ni interprétée côté client
  (`discounts/service.go:60` écrase toujours l'ID côté serveur), donc sans
  rupture fonctionnelle, mais la décision de garder le **type JSON** `discount_id`
  en `string` (désormais le texte décimal d'un entier, plus un
  `discount-<uuid>`) plutôt que de basculer en `number` a permis d'éviter toute
  coordination de déploiement avec ces deux fronts — aucun n'a été touché.

- **Chiffrage réel (staging, `RENDER_STAGING_DATABASE_URL`)**, tous les
  chiffres cités par le brief confirmés à l'identique : 19 remises (9 au
  format numérique legacy actif, 10 `discount-<uuid>` toutes "Happy Hour"
  merchant 2, **0 ligne orderitems** pour ces 10 — jamais utilisables via la
  colonne integer) ; 4 609 `orderitems.discount_id` non nuls dont **17
  orphelines** (5 valeurs : 81/83/84/89/90, remises supprimées) ; **545**
  lignes `base_price ≠ price`, **100% avec un `discount_id` qui matche encore
  une remise réelle** (aucun recoupement avec les 17 orphelines) ;
  `orders.cart_discount_amount > 0` : **0 ligne**, confirmé jamais alimentée.
  `applyCartDiscount` (citée par un commentaire de `create_order_models.go`)
  **n'existe nulle part** — pas un bug de synchronisation, une fonctionnalité
  jamais construite.

- **Phase 2 (clé entière)** : `discounts.discount_id_new integer` attribué
  sans réécrire `orderitems.discount_id` — les 9 remises legacy gardent leur
  valeur numérique actuelle telle quelle (4 592 lignes orderitems valides
  inchangées bit à bit, vérifié), les 10 `discount-<uuid>` reçoivent des
  valeurs neuves après le plus grand entier déjà en circulation (calculé
  dynamiquement en SQL — la migration tourne aussi en prod, dont les valeurs
  réelles diffèrent). `discounts.legacy_discount_id` conserve l'ancien varchar,
  jamais supprimée. Élargi au passage à `discounts_products`/`discounts_schedules`/
  `discounts_products_options` (pas demandées nommément par le brief, mais
  même situation structurelle — cette dernière a une colonne `varchar(20)`,
  trop courte pour un futur `discount-<uuid>` de 45 caractères) : les trois
  reçoivent enfin de vraies contraintes `FOREIGN KEY` (aucune des trois n'en
  avait jamais eu, le mismatch de type l'en empêchait). Pas de FK sur
  `orderitems.discount_id` : les 17 orphelines la feraient échouer, laissées
  telles quelles comme acté. Bascule de la `PRIMARY KEY` elle-même différée à
  un lot de contraction futur. Migrations
  [`118_discounts_integer_key_expansion`](../migrations/todo/118_discounts_integer_key_expansion.up.sql)
  appliquées et vérifiées sur staging (idempotence testée par double
  application de la partie backfill).

- **Phase 3 (table de liaison)** : `discount_redemptions` (créée à vide par
  `041_cart_discounts`, jamais câblée — rapport 57) étendue plutôt que
  remplacée — `scope` (`PRODUCT_LINE`/`CART`, distinct de
  `discounts.discount_scope` qui décrit la configuration, pas une utilisation
  précise), `order_item_id` nullable, `customer_id` retypé `varchar→integer`
  (corrige le deuxième mismatch noté rapport 57), `is_reconstructed`.
  L'ancienne `UNIQUE(discount_id, order_id)` était fausse pour une remise
  ligne (la même remise peut s'appliquer à 2 lignes de la même commande) :
  remplacée par deux index uniques partiels, un par portée. Montant figé à
  l'application (pas un pourcentage recalculable) — même raisonnement déjà
  posé pour le coût de revient (PROMPT 07) : un prix qui change plus tard ne
  doit pas faire bouger un montant déjà accordé. Écriture en direct câblée
  dans `order_life_cycle.upsertOrderItemDiscountRedemption` (appelée par
  `InsertOrderItem` et par la boucle de `UpdateOrder`) : supprime la ligne de
  liaison si la remise est retirée d'une commande encore ouverte (cas cité par
  le brief comme le plus facile à manquer), upsert sinon — y compris pour
  "graduer" `is_reconstructed=false` si une commande fermée est rouverte puis
  réellement réécrite.

- **Phase 4 (cart_discount_amount) différée, pas juste laissée de côté** :
  vérifié qu'aucun code n'applique jamais une remise panier (`applyCartDiscount`
  n'existe pas) — maintenir la colonne "juste après chaque application"
  suppose une application qui n'existe pas. Décidé avec l'utilisateur :
  ce lot prépare seulement le modèle (`discount_redemptions` scope=CART prêt),
  le calcul/validation d'un code promo panier est un chantier produit à part,
  hors périmètre de cet assainissement.

- **Phase 5 (reprise de l'historique)** : les 545 lignes `base_price ≠ price`
  reconstituées dans `discount_redemptions` (`amount_applied_cents = base_price
  - price`, `scope='PRODUCT_LINE'` sans ambiguïté possible — `ORDER_TOTAL`
  n'existe que depuis Sprint 2 et `cart_discount_amount` n'a jamais été
  alimenté), marquées `is_reconstructed=true`. `customer_id` laissé `NULL` :
  `orderitems` ne porte pas cette information, une jointure via
  `orders.customer_id` serait une déduction de plus n'apportant rien à
  l'objectif. Migration
  [`119_discount_redemptions_historical_backfill`](../migrations/todo/119_discount_redemptions_historical_backfill.up.sql),
  idempotente (`ON CONFLICT DO NOTHING`, testé par double application),
  appliquée sur staging : 545/545 lignes, montant total vérifié égal à la
  somme réelle `base_price - price` du jeu de données source.

- **Ce qui reste à faire** (hors périmètre explicite de ce lot) :
  `cart_discount_id`/`cart_discount_code` — migration de suppression préparée
  ([`120_drop_cart_discount_legacy_columns`](../migrations/todo/120_drop_cart_discount_legacy_columns.up.sql)),
  **non jouée**. La simplification des jointures `orders/orders_fetcher_builder.go`
  et `orders/repository.go` (`CAST(oi.discount_id AS TEXT)` devenu inutile, un
  join direct `int = int` suffit désormais) n'a pas été faite dans ce lot —
  fonctionnellement toujours correcte telle quelle, cleanup sûr et non urgent
  à faire plus tard. `docs/migration-postgres/04-schema-postgres-target.sql`
  pas mis à jour dans ce lot (même pattern que les migrations 108-117 :
  reconciliation périodique en bloc, pas systématique migration par migration
  — voir rapport 63) : à faire lors du prochain passage de reconciliation.
  Bascule de la `PRIMARY KEY` de `discounts` vers `discount_id_new` : lot de
  contraction futur, une fois `legacy_discount_id`/`discount_id` confirmés
  plus lus nulle part.

### PROMPT 20 — Diagnostic vente additionnelle, Phase 1 (2026-09-05)

- **Contexte** : le prompt partait de trois hypothèses (moteur qui ne
  produit rien, suggestions non affichées, `is_upsell` non écrit) et
  demandait un diagnostic chiffré avant tout correctif. Rapport complet :
  [docs/audits/2026-09-05-upsell-diagnostic-prompt20.md](audits/2026-09-05-upsell-diagnostic-prompt20.md).

- **Cause dominante du faible volume, vérifiée en base (staging)** :
  `enable_upsell` n'est activé que sur **2 établissements sur 30**
  (`merchant_parameters`). Des établissements avec un volume de commandes
  bien supérieur à celui des 2 établissements actifs (617, 348, 189
  commandes clôturées/90j) ont **zéro** ligne `upsell_suggestions` —
  `generateUpsellSafe` retourne avant même de tenter une persistance
  quand le flag est faux. Constat produit (rollout pilote non généralisé
  ou oubli d'activation ?), pas un bug.

- **Même activé, le moteur "intelligent" ne se déclenche presque jamais** :
  312 des 313 lignes générées sont `source=featured_fallback` (dernier
  recours statique), 1 seule est `pattern`, aucune n'est `llm`. Le seuil
  de co-occurrence (`upsellMinCoOccur=5`/90j) semble structurellement dur
  à atteindre pour un volume de restaurant indépendant, y compris à 2000+
  commandes/trimestre — remonté comme arbitrage produit, seuil non
  modifié de ma propre initiative (consigne explicite du prompt).

- **`accepted_items` fonctionne mais n'a été exercé qu'une fois** dans
  toute l'histoire de la base (2026-07-02, le jour même de l'audit qui
  avait trouvé ScanNOrder non câblé) — mesure différente de
  `orderitems.is_upsell` (corrélation passive produit-dans-commande vs
  preuve d'interaction UI), les deux ne se corroborent pas mutuellement.

- **Surprise principale** : deux des trois correctifs attendus en Phase 2
  étaient déjà faits, sans trace documentée. `wello-resto-scannorder`
  envoie déjà `is_upsell` (`payload.ts:55`, `UpsellPopup.tsx:133`) dans le
  HEAD committé actuel — vérifié par lecture directe + `git show HEAD`,
  contredisant la prémisse du prompt ("aucune occurrence dans le dépôt").
  Aucun code touché pour ce point. Seul correctif appliqué : commentaire
  périmé de `stats/service.go:106` ("jusqu'à ce que l'app Flutter écrive
  is_upsell") corrigé pour pointer vers ce diagnostic plutôt que
  réaffirmer une cause qui ne tient plus depuis juin.

- **Traçabilité du build client (1.3)** : aucun mécanisme serveur ne
  persiste la version applicative réellement en usage par établissement.
  `app_version`/`app_version_merchant` ne servent qu'au contrôle de mise à
  jour (jamais d'écriture du côté serveur) et semblent à l'abandon (dernière
  version connue : avril 2025, alors que le client envoie déjà la version
  100 aujourd'hui). Piste la plus légère identifiée : `api_request_logs`
  capture déjà le corps de `/app/version/check` (`payload` jsonb), mais
  `merchant_id` y est vide sur cette route — recommandation remontée, pas
  implémentée (changement de logging sur route d'auth partagée, à valider).

- **Aucune donnée de production directe** : diagnostic mené contre
  `RENDER_STAGING_DATABASE_URL` (pas d'accès prod local,
  [[reference_staging_db_access]]). Les chiffres collent presque
  exactement à ceux cités par le prompt pour la production (287/26/1 vs
  284/258+26/1 avec dérive expliquée par le temps écoulé) — à confirmer
  par le lecteur avant de traiter ces chiffres comme définitifs pour la
  vraie prod.

### PROMPT 18 — onglet Clients (2026-09-05)

- **Contexte** : huitième onglet analytics branché en SQL direct. Deux
  routes, deux permissions, même découpage que l'onglet Annulations
  (PROMPT 10) : agrégats (nouveaux clients, taux de récurrence, segments,
  fréquence, panier par segment — `permission.ReportsSalesRead`, même porte
  que les 7 autres onglets) et classement nominatif Top Clients (nom, valeur
  vie, dernière visite, panier moyen — `permission.CustomersManage`, déjà
  existante, `is_sensitive`, aucune clé créée). Backend :
  `internal/modules/analytics/{clients,channels,models,service,handler}.go`,
  routes `POST /analytics/clients` et `POST /analytics/clients/top`
  (`cmd/api/routes.go`). Frontend : `ClientsAnalyticsTab.tsx`
  (wello-back-office), remplace `renderClientsTab`/`getCustomersAnalytics`
  de `DashboardAnalysis.tsx`.

- **Le filtre par canal, vérifié avant d'être copié** : le prompt affirme que
  ce filtre « existe déjà pour Produits » et demande de réutiliser le même
  mécanisme. Vérifié dans le code (`products.go`) : **faux** — Produits
  filtre par catégorie, jamais par canal ; le canal n'a jamais existé
  ailleurs dans ce paquet que comme dimension d'affichage
  (`channelCaseExpr`, utilisé uniquement en `GROUP BY` par les tabs
  CA/Commandes/TVA/Annulations), jamais en `WHERE`. Le prompt anticipait
  cette possibilité (« s'il n'est pas encore factorisé, factorise-le à cette
  occasion ») : `ChannelFilter` (`channels.go`) est donc le premier filtre
  d'entrée par canal de ce paquet, construit sur `channelCaseExpr` existant
  et le même référentiel `Channels`/`channels.ts` — disponible pour tout
  futur onglet qui en aurait besoin. Côté front, même constat :
  `ChannelFilter.tsx` (composant générique coché/décoché) existait déjà mais
  n'était câblé nulle part — c'est ici sa première utilisation réelle.

- **Les trois pièges de calcul (§3 du prompt)**, chacun avec sa propre
  vérification :
  - `customer.customer_nb_orders`/`customer_total_spent`/`last_order_date`
    ne sont jamais lus — `GetCustomersLifetimeStats` recalcule tout depuis
    `orders`. Le test d'intégration seed délibérément ces trois colonnes
    avec des valeurs fausses pour prouver que la requête les ignore.
  - « Nouveau client » = première commande **de tous les temps** tombant
    dans la fenêtre — jamais un `MIN(creation_date)` borné à la période.
    Toute agrégation par client (`first_order_date`, `last_order_date`,
    `lifetime_orders`, `lifetime_value_cents`) porte sur tout l'historique,
    de `customersEpoch` (2000-01-01) jusqu'à `periodEnd` (jamais l'horloge
    système) — un rapport sur une période passée reste reproductible.
  - `customer.creation_date` (date d'import) n'est utilisée nulle part.

- **Définitions retenues** (une ligne de justification chacune, servies
  dans le contrat lui-même — `ClientsResponse.Definitions` — pas seulement
  documentées ici) :
  - **Récurrence** : part des clients actifs sur la période (≥1 commande
    dans la fenêtre) ayant ≥2 commandes au total depuis toujours. Lecture
    "de mes clients servis cette période, combien sont des clients
    fidélisés", plus actionnable que "sur toute mon histoire, quelle part a
    commandé ≥2 fois".
  - **Segments** (nouveau/récurrent/fidèle/inactif/dormant, ordre de
    priorité strict) : nouveau (1er ordre jamais placé dans la fenêtre) >
    fidèle (actif + ≥5 commandes à vie) > récurrent (actif, <5 commandes à
    vie) > inactif (dernière commande à plus de 180 jours de la fin de
    période) > dormant (ni actif, ni assez ancien). Le bucket "dormant"
    n'existe QUE si la fenêtre demandée est plus courte que 180 jours — sur
    12 mois il est structurellement toujours à 0 (vérifié sur staging : 0).
  - **Fréquence d'achat** : commandes de la période ÷ clients actifs sur la
    période — pas l'intervalle moyen entre deux commandes (rejeté : la
    majorité des clients n'ont qu'une seule commande sur la période, un
    intervalle moyen serait indéfini pour eux et biaiserait la métrique).
  - **Inactivité** : dernière commande à plus de 180 jours, calculé à la
    date de FIN de période (pas à la date du jour) — un rapport sur une
    période passée reste reproductible s'il est rejoué plus tard.
  - **Seuil de matérialité** : 30 clients identifiés actifs sur la période,
    réutilisé **littéralement** (`minCustomersForRate =
    staffCancellationMinOrders`) depuis Annulations (PROMPT 10) plutôt
    qu'un nouveau chiffre — sous ce seuil, `RecurringRate` est `nil` et
    `SegmentRatesAvailable` est `false` ; le nombre absolu et le volume de
    référence (`IdentifiedCustomersInPeriod`) restent toujours affichés.

- **Le bug de la maquette corrigé** : `analyticsData.clients.top_clients`
  n'existait dans aucun mock (tableau mort depuis toujours) ; la ligne
  dépliée lisait `client.lifetime_value`/`client.last_visit`, deux clés
  jamais produites ; `analyticsData.clients.loyalty_score` était lu avec un
  fallback `|| 72` qui masquait silencieusement son absence permanente ;
  `analyticsService.exportClientsCSV` était appelé sans jamais être défini.
  Les quatre corrigés : `ClientsAnalyticsResponse`/`ClientsTopResponse`
  produisent réellement chaque champ consommé, `exportClientsCSV` existe
  (même patron que `exportCancellationsCSV`, CSV reconstruit côté client à
  partir des deux réponses déjà chargées).

- **Vérifié en lecture seule contre staging** (merchant 212, 12 mois,
  `POSTGRES_URL`=`RENDER_STAGING_DATABASE_URL`, `DB_DIALECT=postgres`, outil
  jetable supprimé après usage) :
  - couverture globale 45,51% (4 378/9 620) ; **Wello Resto seul : 12,30%**
    (735/5 977, proche du 86% non-identifié cité par le prompt) contre
    **marketplaces : 100,00%** (3 643/3 643) — les deux sommes reconstituent
    exactement le total non filtré (4 378 et 9 620), le filtre canal
    partitionne bien sans recouvrement ni perte ;
  - nouveaux clients recalculés à la main (requête SQL indépendante,
    n'appelant aucune fonction du dépôt) : 3 115, identique au chiffre
    produit par `GetCustomersLifetimeStats` ;
  - valeur vie du client le plus rentable (`customer_id=781`, 198 160
    centimes / 40 commandes) et d'un client médian (`customer_id=7240`,
    1 919 centimes / 1 commande) : les deux concordent au centime avec un
    `SUM(price)`/`COUNT(*)` indépendant sur `orders` ;
  - un `merchant_id` inexistant renvoie des zéros propres (`0` client,
    `0`/`0` couverture), aucune erreur ;
  - segments sur cette fenêtre : nouveau 3 115, fidèle 76, récurrent 150,
    inactif 1 010, dormant 0 (attendu, fenêtre > 180 jours) — taux de
    récurrence 523/3 341 = 15,65%.

- **Test d'intégration** :
  `internal/modules/analytics/clients_postgres_integration_test.go`
  (`-tags postgres_integration`), même patron que les 7 autres onglets —
  seed délibérément les compteurs `customer` avec des valeurs fausses,
  6 profils clients couvrant les 5 segments + une commande annulée (exclue)
  + une commande sans client (comptée en couverture, jamais en profil) +
  un canal marketplace pour vérifier la partition par canal. Exécuté avec
  succès contre staging (`go test -tags postgres_integration
  ./internal/modules/analytics/... -run TestClients_Postgres`), ainsi que
  la suite complète des 7 autres tabs (aucune régression). Tests unitaires
  (`clients_test.go`) : `ChannelFilter`, précédence des 5 segments (dont la
  découverte que "dormant" est inatteignable sur une fenêtre ≥180 jours),
  `AvgBasketTTCCents` jamais une division par zéro.

### PROMPT 16 — onglet Produits (2026-09-05)

- **Contexte** : sixième onglet analytics branché en SQL direct (après CA,
  Commandes, Règlements, TVA, Annulations). Contrat complet dès maintenant —
  coût et marge inclus — mais leur valeur reste `null` tant que les prix
  d'achat des composants ne sont pas saisis (règle posée au lot 1, PROMPT 07 :
  `orderitems.cost_price_unit`/`cost_price_reason`, jamais un `0` silencieux).
  Backend : `internal/modules/analytics/{models,products,service,handler}.go`,
  route `POST /analytics/products` (`permission.ReportsSalesRead`, même porte
  que les 5 autres onglets). Frontend : `ProductsAnalyticsTab.tsx` (wello-back-office),
  remplace le mock `renderProductsTab`/`getProductsAnalytics` de
  `DashboardAnalysis.tsx`.

- **Défauts de la maquette corrigés** (docs/analytics/AUDIT.md, wello-back-office,
  Onglet 3 + I9) : tableau tronqué à 10 lignes en dur avec tri client → vraie
  pagination serveur (`page`/`page_size`, `COUNT(*) OVER()` dans la même passe
  que l'agrégation, jamais une deuxième requête juste pour compter) et tri
  résolu en SQL (`sort_by` ∈ quantity/revenue_ttc/margin, whitelist jamais
  interpolée dans `ORDER BY`) ; liste de catégories codée en dur
  (`entrees/plats/desserts/boissons`) → lue depuis `productcateg`
  (`GetProductCategories`), incluse dans la réponse (`available_categories`),
  jamais une deuxième requête frontend séparée.

- **Le piège de l'agrégation partielle (§ du prompt)** : ne jamais diviser un
  CA complet par un coût partiel. Appliqué à deux niveaux — par produit,
  `MarginCents`/`MarginPercent` sont dérivés UNIQUEMENT de
  `CostKnownRevenueTTCCents` (le sous-ensemble des lignes à coût connu), jamais
  de `RevenueTTCCents` (le total du produit) ; en agrégat,
  `ProductsCostCoverage` réutilise **littéralement**
  `coversCoverageThreshold` (20%, déjà adopté pour
  `OrdersPeriodTotals.CoversDataAvailable`) — sous ce seuil de couverture,
  aucune marge agrégée n'est renvoyée, seulement `revenue_ttc_cents_covered`/
  `revenue_ttc_cents_total`/`coverage_ratio`, pour que le front puisse encore
  dire « marge connue sur X% du CA » plutôt que de n'afficher rien du tout.

- **Performance (§4)** : la requête de l'audit (« M2 — Mix produits + HT +
  suppléments ») mesurait 1 347 ms pour 120 433 lignes lues / 1 239 rendues —
  cinq parcours séquentiels. `GetProductsPage` lit `orderitems` une seule fois
  (un CTE agrège tout, `COUNT(*) OVER()` fournit le total de pagination dans
  la même passe) ; `GetProductsScopeTotals` est une deuxième passe, non
  groupée, pour les tuiles KPI et la couverture agrégée ;
  `GetProductsPreviousRevenue` est borné aux `product_id` de la page courante
  (jamais tout le catalogue). Mesuré en lecture (aucune écriture) contre
  staging, merchant 212, fenêtre 12 mois : `GetProductsPage` ~700-900 ms,
  `GetProductsScopeTotals` ~500 ms-1,3 s — sous les 4 s du fusible
  (`AnalyticsStatementTimeoutMS`), migration 087 (index `idx_orders_merchant_creation`,
  `idx_orderitems_order_id`, `idx_extra_order_item_id`) déjà en place.

- **Découverte en vérifiant le § « les deux onglets doivent raconter la même
  histoire »** : sur staging, merchant 212, 12 mois, la somme du CA TTC par
  produit (`orderitems`) et le total de l'onglet CA (`orders.price`) NE
  reconcilient PAS sur le périmètre complet (187 311,98 € vs 159 875,40 €,
  écart de 27 436,58 €). Tracé précisément :
  - **WELLO_RESTO seul reconcilie quasi exactement** (écart résiduel de
    -1 176,40 €, expliqué à moins de 21 € près par `orders.delivery_fees`
    (1 197,00 €) — un écart structurel déjà documenté ailleurs (VATResponse,
    même fichier) : les frais de livraison n'apparaissent jamais dans
    `orderitems`, uniquement sur `orders.delivery_fees`.
  - **L'anomalie réelle est Uber Eats** : la somme `orderitems` dépasse
    `orders.price` de **59%** sur ce périmètre (92 055,59 € vs 57 959,36 €,
    3 079 commandes) — un écart massif, pas un artefact d'arrondi.
  - **Deliveroo** est sous-évalué d'environ moitié (5 174,10 € vs
    10 657,35 €) ; en partie des lignes à prix négatif trouvées sur quelques
    commandes (9 lignes, -508,60 € au total sur toute la fenêtre) mais cela
    n'explique qu'une fraction de l'écart — la cause complète n'a pas été
    creusée davantage (chemin d'écriture Deliveroo hors périmètre de ce
    prompt).
  - **Décision (validée avec l'utilisateur, 2026-09-05)** : ne pas retarder
    la livraison de l'onglet pour investiguer/corriger le chemin d'écriture
    Uber Eats/Deliveroo — explicitement hors périmètre de ce prompt (« Ne
    touche pas... au chemin d'écriture »). L'onglet est livré tel quel, avec
    la formule du prompt (`oi.price + extra` par ligne, seule façon
    d'obtenir un CA PAR PRODUIT — `orders.price` est un scalaire par
    commande, structurellement non décomposable). Le désaccord CA
    tab/Produits tab pour les commandes marketplace est un **fait sur les
    données découvert par ce travail**, pas une régression introduite par
    lui (vérifié : la somme indépendante recalculée directement en SQL
    concorde au centime avec `GetProductsScopeTotals`). À traiter comme un
    chantier de fiabilisation du chemin d'écriture Uber Eats/Deliveroo
    séparé, pas comme un défaut de l'onglet Produits.

- **Vérifié en lecture seule contre staging** (merchant 212, 12 mois,
  `POSTGRES_URL`=`RENDER_STAGING_DATABASE_URL`, `DB_DIALECT=postgres`) :
  quantité/CA recalculés indépendamment en SQL brut concordent au centime
  avec `GetProductsScopeTotals` ; aucune ligne `orderitems.merchant_id` ne
  diffère de `orders.merchant_id` sur le périmètre (écarte une fuite
  cross-merchant via la jointure sur `order_id`) ; sur cette fenêtre,
  0 ligne `NO_RECIPE`/`INCOMPLETE_RECIPE` — la couverture de coût mesurée
  est 0% (tous les `cost_price_unit` sont NULL, cohérent avec « 85% des
  lignes NULL sur l'historique » cité par le prompt, ici 100% sur ce
  périmètre précis faute de recettes chiffrées pour ce marchand).

- **Test d'intégration** :
  `internal/modules/analytics/products_postgres_integration_test.go`
  (`-tags postgres_integration`), même patron que les 5 autres onglets —
  seed direct de `cost_price_unit`/`cost_price_reason` (pas de recette
  seedée : ce endpoint ne fait que lire ces deux colonnes, leur calcul est
  testé ailleurs, `order_life_cycle/cost_postgres_integration_test.go`).
  Couvre : NULL jamais 0 (produit NO_RECIPE/INCOMPLETE_RECIPE), garde
  d'agrégation partielle par produit, seuil de matérialité agrégé via le
  filtre catégorie, pagination + tri serveur (NULLS LAST sur marge),
  évolution nil quand pas de vente période précédente, période vide sans
  erreur. Exécuté avec succès contre staging (`go test -tags
  postgres_integration ./internal/modules/analytics/... -run
  TestProducts_Postgres`), ainsi que la suite complète des 5 autres tabs
  (aucune régression).

### PROMPT 11 — solder l'instrumentation du chemin d'écriture (2026-09-04)

- **Contexte** : suite du lot 1 (`feature/write-path-instrumentation-lot1`) et
  de l'onglet Annulations (PROMPT 10). Branche dédiée
  `feature/write-path-instrumentation-lot2`. Quatre chantiers : collision de
  numéro de migration, vérification du chemin d'écriture de
  `cancelled_by_type`, coût figé pour les options/suppléments, fiabilisation
  de `deletion_reason_id`. Aucune touche au frontend, à la couche analytique
  ni à la logique fiscale ; aucune suppression de colonne ; aucun
  rétro-remplissage de coût.

#### Découverte préalable : le lot 1 n'était pas perdu, seulement jamais committé

- `git stash list` portait `stash@{1}` : *"On feature/write-path-instrumentation-lot1:
  Lot1 write-path-instrumentation WIP (uncommitted)"*. La branche elle-même
  ne portait aucun commit propre (même HEAD que `staging`). Le stash contenait
  l'implémentation complète du lot 1 (migration, `internal/costing`,
  `order_life_cycle/{cost,cancellation,source}.go`, test d'intégration coût,
  audit TVA, câblage repository/service) — vérifiée contre
  `RENDER_STAGING_DATABASE_URL` : la migration 114 avait bien été appliquée
  pour de vrai (colonnes réelles, chiffres de rétro-remplissage strictement
  identiques à ceux du brief). Verdict pour le §2 du prompt : le chemin
  d'écriture existe et est correct, il n'était simplement pas en ligne —
  construit, validé sur staging, puis la branche abandonnée en cours de route
  au lieu d'être committée et fusionnée (hypothèse 1 du prompt, avec plus de
  nuance que « personne n'a écrit le code »). Restauré sur la nouvelle branche
  via `git stash apply` (stash conservé, pas `pop`, jusqu'à confirmation) —
  résolution manuelle de `docs/decisions.md` et
  `send_invoice_email_test.go`, tous deux modifiés en parallèle côté
  Annulations avec exactement le même correctif (vérifié par diff, pas de
  vrai conflit).

#### §1 — Collision de numéro de migration

- Renommage : `114_permission_reports_staff_performance_read` (jamais
  appliquée, encore non trackée par git) → **115** (`114_write_path_instrumentation`
  du lot 1, déjà appliquée sur staging, garde son numéro). Références mises à
  jour (`keys_gen.go`, `analytics/models.go`, `docs/decisions.md`).
- **Trois autres collisions préexistantes trouvées** en vérifiant
  l'intégralité de `migrations/todo` et `migrations/done` (regroupement par
  numéro + slug distinct, pas juste par numéro — up/down partagent
  légitimement le même numéro) : `103_permission_catalog_lot10` vs
  `103_production_ready_delivery_arrival` (`todo/`, toutes deux déjà
  appliquées — RBAC lot 10 et les colonnes de livraison sont des
  fonctionnalités vivantes) ; `024_add_planning_weeks_published_at` vs
  `024_add_users_last_login_at` et `033`/`062` de même (`done/`, archive
  MySQL gelée). Aucune renommée : toutes déjà appliquées, renommer
  réécrirait une histoire déjà déployée pour aucun bénéfice. Devenues
  l'allowlist documentée du test de garde ci-dessous.
- **Test de garde** : `migrations/migrations_numbering_test.go`
  (`TestNoDuplicateMigrationNumbers`, package `migrations` — même patron que
  `keys_gen_test.go`/`routes_rbac_permission_coverage_test.go`). Scanne
  `migrations/todo` et `migrations/done` séparément, groupe par numéro,
  échoue si plus d'un slug distinct partage un numéro, avec l'allowlist des 4
  collisions historiques ci-dessus. Vérifié qu'il détecte réellement une
  collision (retrait temporaire de `103` de l'allowlist → échec confirmé,
  restauré).

#### §2 — `orders.cancelled_by_type`

- Chemin d'écriture restauré (lot 1, stash) : `classifyCancelledByType`
  (`order_life_cycle/cancellation.go`) câblé sur `DenyOrderLocal`/
  `DeleteOrderLocal` (signature `userID` ajoutée) et le webhook direct Uber
  Eats `orders_repo.go:CancelOrder`.
- **Trois trous supplémentaires trouvés** en grep-ant tout `state = 'CLOSED'`
  du dépôt (le recensement du lot 1 — 16 chemins → 2 chokepoints + 1 bypass —
  n'était pas exhaustif) et corrigés :
  - `deliveroo_orders/repository.go:UpdateOrderRejected` — webhook de rejet
    Deliveroo, jamais touché par cancelled_by_type.
  - `ubereats/repository.go:SyncOrderState` (transition DENIED/CANCELED
    seulement, DELIVERY_FAILED exclu — même frontière que le lot 1) et
    `HandleOrderNotFound` (404 Uber) — les deux chemins de réconciliation
    appelés par `RecoverOrderState` après l'échec d'un appel API sortant
    (Deny/Cancel/SetReady/Accept).
  - Les trois utilisent une garde `cancelled_by_type IS NULL` plutôt qu'une
    écriture inconditionnelle : `SetOrderDenied`/`DeleteOrder`
    (order_life_cycle) écrivent `DenyOrderLocal`/`DeleteOrderLocal` (STAFF) de
    façon **synchrone**, avant de déclencher l'appel API externe de façon
    asynchrone — si celui-ci échoue et déclenche une réconciliation, elle ne
    doit pas écraser l'attribution STAFF déjà posée. Découverte notable en
    creusant ce point : **un chemin d'annulation staff pour Deliveroo existe
    bel et bien** dans ce dépôt (`order_life_cycle/service.go` →
    `deliverooSvc.DenyOrder`), contrairement à ce que le rétro-remplissage de
    la migration 114 supposait (« aucun chemin d'annulation staff n'existe
    pour Deliveroo ») — non corrigé rétroactivement (aucun rétro-remplissage
    dans ce lot), signalé pour référence future.
- **Bug préexistant trouvé en testant, sans rapport avec cancelled_by_type
  mais sur la même fonction** : `SyncOrderState` ne fonctionnait jamais sur
  Postgres dès que `reasonID.Valid` (cas DENIED/CANCELED précisément) — pgx
  ne sait pas encoder un `sql.NullInt64` dans une colonne `varchar`
  (`deletion_reason_id`), erreur avalée silencieusement par l'appelant
  (`RecoverOrderState` ignore l'erreur de `FinishOrderIfDoesNotExist`). Trouvé
  par le nouveau test d'intégration (l'ancien n'exerçait jamais ce chemin
  qu'avec `reasonID` vide). Corrigé au même endroit : conversion explicite en
  chaîne avant l'appel, signature publique inchangée.
- Tests : `cancellation_test.go` (table-driven, tous les sentinels de
  `classifyCancelledByType`) ; `cancellation_postgres_integration_test.go`
  (`DenyOrderLocal`/`DeleteOrderLocal`, staging réel, chaque sentinel →
  valeur attendue) ; assertions ajoutées aux tests d'intégration existants de
  `webhook/ubereats/repository`, `modules/ubereats` (nouveau test dédié
  couvrant aussi le cas « déjà classé, ne pas écraser ») et
  `webhook/deliveroo_orders`.

#### §3 — Coût figé des options et des suppléments

- Migration 116 ajoute `cost_price_unit integer`/`cost_price_reason
  varchar(20)` à `order_item_configuration` et `extra`, même vocabulaire
  `CHECK` que le lot 1 (`NO_RECIPE`/`INCOMPLETE_RECIPE`, constantes
  `costing.Reason*` réutilisées telles quelles). Aucun rétro-remplissage.
- **Découverte staging avant d'écrire le code** :
  `configurable_attribute_options.component_id` est **100 % NULL**
  (3140/3140) — la liaison option→ingrédient (migration 079) n'a jamais été
  utilisée depuis l'admin marchand. Conséquence assumée, pas un bug : tant
  qu'aucune option n'est liée, toute ligne `order_item_configuration` gèle
  `NULL`/`NO_RECIPE` — reflet correct de l'état réel, pas un défaut de
  résolution. `extra.component_id` est `NOT NULL` (toujours un ingrédient
  réel) et n'a pas de colonne `unit_of_measure` propre : `extra.quantity`
  (défaut 1, jamais fixé explicitement par le chemin d'écriture actuel)
  compte directement des unités de `purchase_price_quantity` du composant, pas
  de conversion nécessaire.
- **Options** : `resolveOptionCostsBatch` (lot 1, déjà batché par commande)
  réutilisé tel quel — le seul cas `ok=true, costCents=0` qu'il produit est
  « aucun composant lié » (`costing.PricePerUnit` rejette déjà tout prix
  ≤ 0), donc directement interprétable comme `NO_RECIPE` sans ambiguïté.
- **Suppléments** : nouveau `resolveExtraCostsBatch` (`cost.go`), même patron
  de batching, plus simple (pas de conversion d'unité).
- **Pas de requête dupliquée** : `resolveOrderItemCostsForOrder` (chemin
  `CreateOrder`) retourne désormais aussi la map `optionCosts` déjà calculée,
  réutilisée pour geler `order_item_configuration` sans reformuler la requête
  `configurable_attribute_options`. Une seule requête neuve ajoutée par
  commande (le batch extras). `UpdateOrder` résout par ligne (même asymétrie
  que le lot 1 pour `orderitems` — peu de lignes éditées à la fois).
- Tests : `cost_options_extras_postgres_integration_test.go` — coût figé
  d'une option liée et d'un supplément, non affecté par un changement
  ultérieur de `components.purchase_price` (le test central de ce lot) ;
  composant sans prix → `NULL`/`INCOMPLETE_RECIPE`, jamais `0` ; option non
  liée → `NULL`/`NO_RECIPE`, jamais `0`.
- Mesuré sur staging (méthodologie du lot 1, latence dev→Render dominante) :
  la requête neuve (`resolveExtraCostsBatch`) coûte ~25,6 ms en moyenne sur 10
  essais, contre ~26,5 ms pour un aller-retour trivial (`SELECT 1`) — confirme
  qu'elle ajoute exactement un aller-retour réseau, rien de plus, cohérent
  avec le ~21 ms/aller-retour mesuré par le lot 1. `CreateOrder` passe donc
  d'environ ~44 ms (lot 1, panier de 5 lignes) à environ ~70 ms dans ces
  mêmes conditions dev→Render — sur-estime largement l'impact réel en
  production (service et DB colocalisés), même réserve que le lot 1.

#### §4 — `deletion_reason_id`

- **Guillemets parasites : déjà mort dans le chemin d'écriture actuel**,
  vérifié contre les données et pas seulement en lisant le code. 212 lignes
  historiques (sept. 2025–févr. 2026, concentrées sur un compte staff) portent
  des guillemets littéraux ; **zéro** ligne après le 2026-02-18, y compris des
  annulations récentes du même compte staff, n'en porte. Aucune logique de
  concaténation de guillemets trouvée dans le chemin Go actuel
  (`handler.go` → `service.go` → `DenyOrderLocal`/`DeleteOrderLocal` passe
  `req.DeletionReasonID` tel quel en paramètre lié). Commit exact du correctif
  non retrouvé (recherché dans l'historique `wello_resto_flutter` autour de
  la date, sans résultat concluant) mais la donnée est concluante : rien à
  corriger côté code d'écriture pour ce point précis.
- **Ce qui est encore vivant et cassé** : `varchar(11)` était trop étroit
  pour les constantes de ce dépôt lui-même (`KIOSK_CUSTOMER_CANCELLED` = 24
  caractères, `SNO_CUSTOMER_CANCELLED` = 23). Confirmé par sonde directe
  contre staging (table temporaire) qu'une écriture paramétrée normale
  **échoue** sur `varchar(11)` en cas de dépassement (`value too long`), sans
  troncature silencieuse — pourtant des lignes tronquées récentes
  (`KIOSK_CUSTO`, jusqu'au 2026-07-23) existent en base. Mécanisme exact non
  élucidé (aucun trigger/règle sur `orders`, aucune troncature côté Go
  trouvée) — vraisemblablement un chemin d'écriture hors de ce dépôt.
  Corrigé quand même à la source la plus sûre : migration 116 élargit
  `orders.deletion_reason_id` à `varchar(32)` (marge au-dessus des 24
  caractères actuels).
- Migration 117 (**écrite, non appliquée**, conformément à la consigne) :
  nettoie les 212 lignes à guillemets (`trim(both '''' from ...)`, même
  motif que le rétro-remplissage du lot 1).

#### Validation

- `go build ./...`, `go vet ./...` : mêmes avertissements préexistants, sans
  rapport (ubereats/client code inatteignable, verrou copié dans
  auth/handler.go).
- `go test ./...` sur l'arbre entier : mêmes 4 échecs préexistants que ceux
  déjà catalogués par le lot 1 (`planning/employees`, `planning/leave`,
  `planning/swaps`, `internal/modules/ubereats`) — zéro régression neuve.
- Suite `postgres_integration` complète sur les paquets touchés : verte, à
  une exception préexistante et hors périmètre — `orders.brand_store_id`
  (ajoutée par la migration 111, déjà committée, jamais appliquée sur cette
  instance staging) fait échouer les trois tests qui touchent
  `GetOrderMetadata`/`CreateOrder` côté Uber Eats/order_life_cycle ; non
  appliquée ici (hors périmètre de ce lot), signalé pour qui gère le
  déploiement staging.
- Un défaut de fixture de test préexistant trouvé et corrigé en cours de
  validation (hors périmètre mais nécessaire pour obtenir un run vert) :
  `webhook/deliveroo_orders/postgres_integration_test.go` seedait/attendait
  encore `brand_status` en minuscules pour `UpdateOrderAccepted`/
  `UpdateOrderConfirmed`, jamais mis à jour quand le lot 1 a normalisé
  l'écriture/la comparaison en majuscules (B3) — fixture corrigée, aucune
  logique applicative touchée.
- Migrations 115 et 116 appliquées sur staging pour de vrai (additives,
  idempotentes) — chiffres ci-dessus mesurés après application réelle.
  Migration 117 volontairement non appliquée.

### PROMPT 10 — onglet Annulations : périmètre, deux endpoints, `reports.staff_performance.read` (2026-09-04)

- **Livrable** : `internal/modules/analytics/{scope,cancellations,models,service,handler}.go`,
  migration `115_permission_reports_staff_performance_read` (renumérotée
  depuis 114 par PROMPT 11 — collision avec `114_write_path_instrumentation`
  du lot 1, resté non fusionné/non numéroté au moment où ce lot a pris le
  numéro ; voir l'entrée PROMPT 11 plus bas), constante
  `permission.ReportsStaffPerformanceRead`, câblage `cmd/api/routes.go`,
  tests unitaires + `TestCancellations_Postgres` (lecture seule, staging).
  `POST /analytics/revenue|orders|payments|vat` non touchés.
- **Périmètre d'annulation retenu : `brand_status = 'CANCELED'` seul**, pas
  `CANCELED+DENIED+DELETED` (1 738 lignes, périmètre PROD, tout historique —
  le chiffre cité par le prompt). Sous `CANCELED` seul : 1 461 lignes sur le
  même tout-historique PROD (248 `DENIED` + 29 `DELETED` exclus). Tracé dans
  le code qui
  écrit chaque statut (`internal/modules/order_life_cycle/repository.go`,
  `internal/modules/ubereats/repository.go`, `internal/modules/deliveroo/
  repository.go`) : `DENIED` est un refus **à la prise**, jamais après
  acceptation (`cancelled_by_type` y est exclusivement `SYSTEM`/`PLATFORM`,
  jamais `STAFF`/`CUSTOMER` — vérifié sur staging) ; `DELETED` est du code
  mort côté écriture (`git log --all -S "brand_status='DELETED'"` : aucun
  résultat, sur aucune branche) — plus aucune ligne depuis le 2024-10-26,
  `DeleteOrderLocal` écrit désormais `CANCELED`. Documenté en tête de
  `scope.go` (`AnalyticsCancellationsScope`), sans filtre `state` (les
  chemins d'écriture plateforme ne touchent jamais `orders.state`).
- **Dénominateur du taux retenu : annulées ÷ toutes commandes créées** (pas
  ÷ annulées+valides — `AnalyticsOrdersScope` exclut déjà les annulations par
  construction, ce qui aurait obligé à redéfinir "valide" ad hoc rien que
  pour ce ratio). `AnalyticsAllOrdersCreatedScope` (scope.go) est la seule
  définition. Le backend n'émet jamais de taux pré-divisé — seuls
  `cancelled_count`/`total_orders_created` sont renvoyés, le front calcule
  l'affichage, comme `OrdersPeriodTotals` le fait déjà pour les couverts.
- **Deux endpoints, une seule clé nouvelle** : `POST /analytics/cancellations`
  reste sous `reports.sales.read` (même groupe `r.Use` que les 4 autres
  onglets) ; `POST /analytics/cancellations/by-staff` est déclaré **hors** de
  ce groupe, avec son propre `r.Use(RequirePermission(ReportsStaffPerformanceRead))`
  — `RequirePermission` ne prend qu'une seule clé (RBAC lot 2), empiler les
  deux aurait exigé les deux droits à la fois pour la même route.
- **`reports.staff_performance.read` (migration 115, sort_order 135,
  `is_sensitive=true`)** : recette DROITS.md §5.3 suivie à la lettre —
  constante + `All` dans `keys_gen.go`, câblée sur `/by-staff`,
  `TestAllMatchesMigrationCatalog`/`TestRBACPermissionCoverage`/
  `TestRBACRatchet` verts. **Pas d'entrée `legacyPermissionFallback`**
  (deliberate, comme les 5 clés du lot 10) : en production (`role_id` encore
  NULL partout), cette route retombe de facto à `Rights.Admin` seul tant que
  `cmd/seed_system_roles` + l'attribution de rôles n'ont pas tourné —
  documenté ici pour ne pas être une redécouverte surprise (même remarque
  que RBAC lot 11 Phase 5 pour lot 10).
  **Reste à faire, hors périmètre lecture seule de ce lot** : déployer la
  migration 115 puis lancer `cmd/seed_system_roles` sur staging pour que le
  rôle Administrateur porte la nouvelle clé (non fait ici — aucune écriture
  sur staging n'a été autorisée pour ce prompt).
- **`cancelled_by_type` : chaîne d'écriture introuvable dans ce dépôt.**
  Vérifié : `git log --all -S "cancelled_by_type"` → zéro résultat, sur
  toutes les branches, tout l'historique. Pourtant la colonne est réelle,
  vivante, et croît en continu jusqu'aux commandes les plus récentes
  (couverture ~0% mi-2025 → ~100% depuis fin 2025, aucun trigger ni fonction
  Postgres ne la référence). Le code qui l'alimente n'est dans aucun dépôt
  local accessible. Fiable pour construire dessus (donnée vivante, pas un
  backfill figé) mais à signaler à qui maintient l'écriture réelle.
  **Résolu par PROMPT 11** (voir plus bas) : `git log --all -S` ne pouvait pas
  le trouver car `--all` ne couvre pas `refs/stash` — le code existait déjà,
  complet et validé sur staging par le lot 1, mais était resté dans une stash
  jamais poppée ni committée sur `feature/write-path-instrumentation-lot1`.
- **Deux bugs qualité découverts sur `orders.deletion_reason_id`**, tous deux
  gérés en le traitant comme un **identifiant potentiellement sale**, jamais
  en supposant une jointure propre vers `deletion_reasons` (entier) :
  guillemets parasites stockés dans la valeur elle-même (ex. la chaîne
  littérale `'3'`, apostrophes comprises) et troncature (`varchar(11)`, un
  code observé coupé net à 11 octets, ex. `KIOSK_CUSTO`). `GetCancellationsByReason`
  (cancellations.go) trim les guillemets avant jointure et route tout ce qui
  ne matche aucune ligne du référentiel vers un bucket `uncatalogued:<brut>`
  explicite plutôt que de le faire disparaître silencieusement — même
  principe que le bucket `UNKNOWN` de `cancelled_by_type`.
- **Motifs dérivés des commandes réellement annulées, jamais du référentiel
  `deletion_reasons` énuméré à part** : `deletion_reasons` est global et
  partagé avec les réservations/livraisons (DROITS.md/PERIMETRE.md) ; en
  groupant `GetCancellationsByReason` depuis `orders` puis en joignant VERS
  le référentiel, un motif de réservation ne peut structurellement jamais
  apparaître dans cet onglet — pas besoin d'une liste blanche
  `deletion_reason_object`.
- **Bloc nominatif** : seuil d'effectif = **30 commandes créées sur la
  période** avant d'afficher un taux (comptes bruts sinon, toujours visibles).
  Vérifié sur staging (PROD, 12 mois) : seulement 7 couples
  (établissement, serveur) portent la moindre annulation `STAFF`, la plupart
  des établissements tournant sur un compte POS partagé plutôt qu'un compte
  par salarié (`Users` par établissement très faible, PERIMETRE.md). Les
  annulations `STAFF` non rattachables à un `users.user_id` réel (~11 % sur
  le périmètre PROD complet, 12 mois ; 0 sur le seul merchant 212) forment une ligne
  synthétique `unattributed` plutôt que de disparaître — condition posée par
  le prompt §6 : la somme des `cancelled_count` du endpoint nominatif doit
  égaler exactement le sous-total `STAFF` de l'agrégat, vérifié sur staging.
- **Vérifié en lecture seule contre staging, merchant 212, 12 mois
  (2025-09-04→2026-09-04)** : `cancelled_count=830`, `total_orders_created=
  10 474` (taux 7,92 %), `cancelled_amount_cents=1 567 382`. Motifs/canaux/
  typologies d'auteur somment chacun exactement à 830. Endpoint nominatif :
  `Σcancelled_count=775` = sous-total `STAFF` de l'agrégat (775), et 100 %
  des annulations `STAFF` de ce merchant sont rattachables à un utilisateur
  réel (2 comptes : `226`→774, `2`→1). `unknown_cancelled_count=2/830`
  (0,24 %) et `with_reason=825/830` (99,4 %) — très en-dessous du chiffre
  « 13,4 % de NULL » cité par le prompt : recalculé sous ce périmètre
  (`CANCELED` seul, pas `CANCELED+DENIED+DELETED`), pas repris tel quel.
  Établissement à zéro annulation sur la période (merchant 225, 3 mois) :
  zéros propres, aucune erreur.
- **Verdict coût des options (vérification préalable du prompt)** :
  `orderitems.cost_price_unit`/`cost_price_reason` existent (le snapshot
  produit du lot 1 est bien posé), mais `extra` (table des options/suppléments
  vendus) **n'a aucune colonne de coût** — ni `cost_price_unit` ni
  équivalent, seulement `component_id` pointant vers `components.purchase_price`
  (prix d'achat **courant**, pas un instantané à la vente). Comme
  `cancelled_by_type`, ni l'une ni l'autre de ces deux colonnes de coût
  n'a de migration ni de code d'écriture dans ce dépôt (même vérification
  `git log --all -S`, zéro résultat) — posées hors bande, comme `cancelled_by_type`.
  Le coût des options reste donc un chantier séparé et n'a pas été codé ici,
  conformément à la consigne du prompt.

### PROMPT 08 lot 2 — RBAC : retrait des deux derniers lecteurs directs de `Rights.Admin` (2026-09-04)

- **Contexte** : ferme le chantier ouvert par le prompt 05. Sur les dix
  méthodes sœurs de `UserLoginRow` (`HasMenuAccess`, `HasSettingsAccess`,
  etc.), deux n'avaient jamais été migrées vers `Has()`/le catalogue RBAC et
  lisaient encore `Rights.Admin` en direct : `HasAccessReception()` et
  `CanPrintCashReport()` (`internal/modules/auth/models.go`). Décisions
  reçues telles quelles (pas de nouvelle analyse d'opportunité) : la première
  est supprimée, la seconde décommissionnée. Branche dédiée
  `feature/rbac-lot12-decommission-legacy-checks`, partie de `staging`.
- **Recensement, avant toute suppression** : grep exhaustif des deux noms de
  méthode sur tout le dépôt Go.
  - `HasAccessReception()` : **zéro appelant**, nulle part — ni route, ni
    champ de réponse. Confirmé mort avant même ce lot (un contrôle d'accès
    retiré ailleurs dans une bascule antérieure, sans que la méthode
    elle-même ait jamais été nettoyée). Suppression pure, aucun contrat à
    préserver.
  - `CanPrintCashReport()` : **un seul appelant**,
    `internal/modules/auth/service.go` (avant ce lot, ligne 470),
    alimentant `capabilities.actions.print_merchant_cash_report` dans la
    réponse de login (`LoginCapabilityActionsResponse`) — un usage
    d'**affichage** (capability envoyée au client), jamais une décision
    d'autorisation : aucune route ne l'appelait. Deux autres champs voisins
    portent presque le même nom JSON mais sont indépendants et non
    touchés : `access.permissions.print_merchant_cash_report`
    (`LoginAccessPermissionsResponse`, lit `Rights.PrintMerchantCashReport`
    brut, sans `Admin`, commentaire déjà explicite en place depuis RBAC lot
    6) et `legacy.print_cash_report` (`LoginLegacyFields`, payload déprécié,
    même lecture brute).
- **Vérification client, avant de toucher au contrat de login** : recherche
  dédiée dans les 4 dépôts front (`wello_resto_flutter`, `wello-kiosk`,
  `wello-resto-scannorder`, `wello-back-office`) pour
  `print_merchant_cash_report`. Résultat : **`wello_resto_flutter`**
  (`AuthCapabilityActionsResponse`/`AuthCapabilityActionsDto`, y compris un
  getter deprecated qui pointe explicitement vers
  `capabilities.actions.printMerchantCashReport`) et **`wello-back-office`**
  (`AuthCapabilities.actions.print_merchant_cash_report` dans
  `src/types/auth.ts`) parsent et stockent ce champ ; aucun des deux ne s'en
  sert pour gater une action UI à ce jour (recherché explicitly, rien
  trouvé), mais **le champ est bien consommé** (parsé/stocké) — condition
  suffisante pour ne pas le retirer du contrat. `wello-kiosk` et
  `wello-resto-scannorder` : aucune référence, pas concernés. Un homonyme
  sans rapport existe côté back-office (`MerchantUserPermissions.print_merchant_cash_report`,
  l'endpoint admin `/users/{id}/rights` de gestion des droits employé — un
  contrat différent, non touché par ce lot).
- **Appliqué** :
  - `HasAccessReception()` supprimée (`internal/modules/auth/models.go`).
  - `CanPrintCashReport()` supprimée également (le corps aurait été
    `return true`, aucun intérêt à garder un wrapper) ; son unique site
    d'appel remplacé par la constante `true` directement, avec commentaire
    renvoyant à cette entrée — exactement la remédiation « valeur constante
    en attendant la mise à jour du client » demandée par le brief plutôt que
    de supprimer le champ JSON. `access.permissions.*` et `legacy.*` restent
    des lectures brutes de `Rights.PrintMerchantCashReport`, inchangées.
  - Repli `pos.status.manage` (`legacyPermissionFallback[permission.POSStatusManage]`,
    `internal/modules/auth/permissions.go:23`, backé par la colonne
    `users_rights.access_wrreception` via le champ Go `AccessReception`)
    vérifié **toujours présent et intact** — fichier non touché par ce lot,
    confirmé par `git diff` vide sur `permissions.go` et par les tests
    `TestRequirePermission_POSStatusManage_*`
    (`internal/middleware/require_permission_test.go`), qui passent tels
    quels. Ce repli est un champ Go différent (`AccessReception`, lu par
    `legacyPermissionFallback`) de la méthode supprimée
    (`HasAccessReception()`, qui lisait `Rights.Admin || Rights.AccessReception`
    puis n'était appelée nulle part) — deux choses distinctes qui partagent
    la même colonne source, pas la même fonction.
- **Non touché, comme demandé** : `users_rights.admin` (la colonne) reste en
  base, toujours lue par le repli `Has()` (`RoleID == nil`) et par
  `HasAdminRole()` — ce sont eux qui font tourner la production tant que
  `role_id` y est `NULL` partout (voir `docs/RBAC_REPLI_HISTORIQUE.md`).
  Migration `113_drop_users_rights_admin_column` reste préparée, **non
  appliquée**. Son commentaire d'en-tête mis à jour : elle listait
  `HasAccessReception()`/`CanPrintCashReport()` comme deux des quatre
  lecteurs bloquants — n'en reste que deux (`HasAdminRole()`'s branche
  `RoleID == nil`, `LoginLegacyFields.Admin`), ce lot en a fermé deux sur
  quatre, pas la totalité.
- **Documentation** : `docs/RBAC_REPLI_HISTORIQUE.md` (nouvelle section
  « RBAC lot 12 ») et `docs/RBAC_ROUTES.md` (note dans l'en-tête) mis à jour.
  **Le tableau des 18 clés du catalogue et le compte « 10 avec repli / 8 sans »
  ne changent pas** : les deux méthodes retirées n'étaient ni des clés
  `permission.Key` ni des entrées de `legacyPermissionFallback` — deux
  méthodes autonomes hors du système `Has()`/repli, question posée
  explicitement par le brief et tranchée par la négative.
- **Tests** : nouveau
  `TestBuildLoginResponse_PrintMerchantCashReport_AlwaysTrue`
  (`internal/modules/auth/login_response_test.go`, 4 sous-cas croisant
  `Admin`/`PrintMerchantCashReport`) — confirme la capability toujours à
  `true` et, dans le même test, que `access.permissions.print_merchant_cash_report`
  continue de refléter le droit réel de l'utilisateur (non affecté). Suite
  complète `go build ./...`/`go test ./...` : aucune régression — les 4
  échecs pré-existants sans rapport (`planning/employees`, `planning/leave`,
  `planning/swaps`, `internal/modules/ubereats`) inchangés ; un cinquième,
  déjà rencontré et corrigé sur le lot « instrumentation du chemin
  d'écriture » (signature `customers.NewCustomersService` désynchronisée,
  bloquait la compilation des tests d'`order_life_cycle`), corrigé ici aussi
  à l'identique puisque cette branche part aussi de `staging`.
- **Suite** : rien de planifié par ce lot — la condition de retrait du
  repli historique et de la colonne `users_rights.admin` reste celle décrite
  dans `docs/RBAC_REPLI_HISTORIQUE.md` (0 ligne `role_id IS NULL` active,
  production comprise), non atteinte, non traitée ici.

### PROMPT 07 lot 1 — instrumentation du chemin d'écriture : coût figé, source, annulation, casse (2026-09-04)

- **Contexte** : première vague de `ROADMAP-analytics.md` (doc non présent
  dans le dépôt — même situation que `docs/analytics/DROITS.md` pour le
  chantier RBAC : les figures citées par le brief ont toutes été vérifiées
  contre staging et se sont révélées exactes, donc pas de blocage sur son
  absence). Périmètre : migration additive unique
  (`migrations/todo/114_write_path_instrumentation.up/down.sql`), code
  d'écriture sur branche dédiée `feature/write-path-instrumentation-lot1`,
  audit TVA (`docs/TVA_MARKETPLACE_AUDIT.md`). Aucune touche à la couche
  analytique, à la logique fiscale ni au RBAC.
- **Recherche préalable** : 4 agents Explore en parallèle (chemin d'écriture
  `orderitems`/calcul de coût existant, recensement exhaustif des 16 chemins
  d'annulation, schéma `orders`/`customer` et usages de `brand_status`,
  payloads webhook Uber Eats/Deliveroo pour la TVA) + vérification directe
  contre `RENDER_STAGING_DATABASE_URL` à chaque étape (schéma réel,
  distribution des données, simulation SQL des règles de rétro-remplissage
  avant de les figer dans la migration).

#### B2 — coût de revient figé sur `orderitems`

- **Colonnes** : `cost_price_unit integer` (coût **unitaire**, pas coût de
  ligne — se multiplie trivialement par `quantity` à la lecture, comme
  `price`/`base_price` déjà stockés en unitaire ; un coût de ligne
  pré-multiplié aurait dû être recalculé à chaque changement de quantité) et
  `cost_price_reason varchar(20)` (`NO_RECIPE` | `INCOMPLETE_RECIPE`,
  `CHECK` constraint). Centimes entiers, jamais de flottant — `orders.monnaie`
  documenté comme contre-exemple (colonne `real`, jamais lue côté Go après
  scan, dérive silencieuse potentielle).
- **Distinction NO_RECIPE / INCOMPLETE_RECIPE** obtenue sans colonne
  supplémentaire : `cost_price_unit` NULL + `cost_price_reason` NULL = ligne
  antérieure à ce lot (jamais évaluée) ; NULL + `NO_RECIPE`/`INCOMPLETE_RECIPE`
  = évaluée après déploiement, cas normal vs défaut de paramétrage.
- **Calcul** : `internal/costing` (nouveau package, pur, sans accès DB) —
  `UnitCost`/`ConvertedQuantity`/`PricePerUnit`. La logique existante
  (`menu.GetAllProducts`) était dupliquée 3 fois et divergeait sur la
  convention `unit_of_measure_convert` : `menu.GetAllProducts` interroge
  `(id_from=unité requise, id_to=unité composant)` et divise,
  `stocks.ConsumeOrderStock` interroge la paire inverse et multiplie — les
  deux sont justes (vérifié sur staging : chaque paire existe en double sens,
  ratios bien réciproques), mais ne doivent pas être mélangées. `internal/costing`
  fixe la convention de `menu.GetAllProducts` (division) et corrige un vrai
  bug de cette dernière au passage : `COALESCE(conv.ratio, 1)` suppose
  silencieusement une conversion 1:1 quand aucune ligne de conversion
  n'existe pour des unités différentes — remplacé par un échec explicite
  (`ok=false` → `INCOMPLETE_RECIPE`), conforme à la règle NULL≠0 du lot.
  `requires.consumable_id` (17/2246 lignes actives sur staging) n'est pas
  résolu par ce lot (le brief ne mentionne que `components.purchase_price`) :
  traité comme incomplet plutôt qu'ignoré silencieusement.
- **Factorisation partielle, assumée** : `menu.GetAllComponents` et
  `stocks.GetComponentsList` (duplications triviales à une ligne,
  `purchase_price / purchase_price_quantity`) migrées vers
  `costing.PricePerUnit`. Le calcul complet de `menu.GetAllProducts`
  (agrégation multi-composants, affichage admin uniquement, jamais sur le
  chemin d'écriture) n'a **pas** été réécrit pour réutiliser `internal/costing`
  — risque de régression sur l'éditeur de menu hors scope de ce lot pour un
  gain nul sur l'objectif B2. Signalé comme nettoyage candidat pour un lot
  séparé plutôt que fait à la sauvette ici.
- **Options** : `configurable_attribute_options.component_id/quantity/unit_of_measure`
  existaient déjà (migration 079, jamais branchés sur un calcul de coût).
  Le coût d'une option sélectionnée est résolu avec le même
  `costing.UnitCost` et additionné au coût unitaire de la ligne — pas de
  nouvelle colonne. Convention alignée sur `extra_price` (déjà multiplié par
  `oi.quantity` dans les recalculs de CA existants) : le coût d'une option
  est résolu **par unité de produit**, pas par ligne.
- **Performance, mesurée avant/après correctif** : la résolution initiale
  (une par ligne de commande) coûtait ~2 allers-retours SQL par ligne
  (recherche de recette + jointure requires/components/conversion),
  mesuré à ~46 ms/ligne depuis la machine de dev vers Postgres staging —
  chiffre dominé par la latence réseau dev→Render (confirmé : une simple
  requête déjà sur le chemin critique, `GetNextOrderNum`, coûte ~21 ms dans
  les mêmes conditions), donc surestime largement l'impact réel
  service→DB colocalisés en production. Corrigé quand même, par principe
  (le nombre d'allers-retours, lui, était réel et proportionnel au nombre de
  lignes) : `insertOrderItems` (chemin `CreateOrder`) résout désormais tout
  le panier en un lot (`resolveOrderItemCostsForOrder` — 2 requêtes IN(...)
  au total, quelle que soit la taille du panier) au lieu d'une résolution
  par ligne. Mesuré après correctif : ~44 ms pour un panier de 5 lignes
  (contre ~230 ms estimés sans le lot), soit le coût figé de 2
  allers-retours au lieu de 2×N. `UpdateOrder` (édition d'une commande
  ouverte) garde la résolution ligne par ligne — édite typiquement 1-2
  lignes à la fois, pas tout un panier neuf ; changement jugé disproportionné
  pour ce lot, à reconsidérer si `UpdateOrder` s'avère un jour appelé avec
  beaucoup de lignes modifiées simultanément. Pas de cache Redis posé : le
  vrai problème était le nombre d'allers-retours (résolu par le batch), pas
  le coût de calcul lui-même.
- **Choke points touchés** : `InsertOrderItem`/`insertOrderItems` (repository.go,
  chemin `CreateOrder` — POS, kiosk, ScanNOrder, webhooks Uber Eats/Deliveroo,
  tous convergent ici) et les deux variantes de `UpdateOrder` (insert de
  nouvelle ligne + upsert MySQL/Postgres — les deux tenues synchronisées en
  nombre de placeholders, le chemin MySQL étant mort en production mais
  toujours compilé/lié au même jeu de paramètres).

#### A6 — `orders.order_source`

- **Colonne** : `varchar(30)` + `CHECK IN ('WELLO_RESTO_POS','KIOSK',
  'SCANNORDER','UBER_EATS','DELIVEROO')`. Aucune colonne existante ne porte
  déjà cette info — confirmé par grep et par deux docs internes
  (`docs/ARCHITECTURE_API.md` §7.3, `docs/KIOSK_DECISIONS.md`) qui proposaient
  déjà ce champ sans jamais l'implémenter (dette technique signalée, jamais
  traitée avant ce lot).
- **Dérivation** (`internal/modules/order_life_cycle/source.go`,
  `resolveOrderSource`, mêmes règles dans la migration pour le
  rétro-remplissage) : `brand=UBER_EATS/DELIVEROO` → valeur directe (created_by
  ne dit rien du canal pour ces deux, seulement de la commande d'origine du
  webhook) ; `brand=WELLO_RESTO` + `created_by='KIOSK'` → `KIOSK` ;
  `created_by IN ('SCANNORDER','-1')` → `SCANNORDER` (`-1` = marqueur legacy,
  déjà traité comme équivalent ailleurs dans `pos/accounting`) ; `created_by`
  numérique ou `user-<uuid>` → `WELLO_RESTO_POS`. Piège trouvé et corrigé en
  cours de route : `upsertCustomer` s'exécute **avant** `setOrderDefaults`
  dans `CreateOrder`, donc `brand` peut encore valoir `""` au moment de
  dériver l'`acquisition_source` du client — `resolveOrderSource` traite `""`
  comme `WELLO_RESTO` pour rester correct indépendamment de l'ordre d'appel.
- **Rétro-remplissage** : vérifié sur staging avant d'écrire la migration —
  33858/33862 commandes (99,99 %) couvertes sans ambiguïté ; le reste (3
  lignes) porte un `created_by` contredisant `brand` (anomalie de données
  historique, ex. `brand=WELLO_RESTO` avec `created_by=WEBHOOK_UBER_EATS`) →
  laissé `NULL`.

#### A6b — `customer.acquisition_source`

- **Colonne** : `varchar(30)`, pas de `CHECK` (vocabulaire ouvert, le brief
  ne fixe pas de liste fermée contrairement à A6). Distincte de
  `customer_brand` existant (marque propriétaire de la fiche, pas canal
  d'acquisition — confusion explicitement signalée par le brief).
- **Non rétro-remplie**, par construction. Protection en ceinture et
  bretelles : `CustomersRepository.UpdateOrCreateCustomer` exclut désormais
  `acquisition_source` de sa branche UPDATE (pas seulement laissée `nil` côté
  appelant) — impossible d'écraser la valeur captée à la création d'un client
  existant, même par erreur d'appel future.

#### C2 — `orders.cancelled_by_type`

- **Recensement exhaustif** : 16 chemins identifiés (voir l'agent de
  recherche dédié), convergeant tous vers 2 points de choke
  (`DenyOrderLocal`, `DeleteOrderLocal`) + 1 chemin direct hors
  `order_life_cycle` (webhook Uber Eats `orders_repo.go:CancelOrder`, bypass
  complet). `cancelled_by_type` dérivé de l'acteur déjà porté par
  `DenyOrderRequest.UserID`/`DenyOrderInput.UserID` (jusque-là utilisé
  seulement pour les notifications/clé Redis, jamais persisté) :
  `SYSTEM`/`WEBHOOK_STRIPE` → SYSTEM, `WEBHOOK_DELIVEROO`/`WEBHOOK_UBER_EATS`
  → PLATFORM, `SNO_CUSTOMER`/`KIOSK` → CUSTOMER, tout le reste (par
  construction : un vrai `user_id` authentifié) → STAFF. Uber Eats webhook
  direct : `PLATFORM` codé en dur (aucun acteur du tout dans ce payload).
  **Hors périmètre, assumé** : `terminalizeDeliveryStop` (échecs de livraison
  `DELIVERY_FAILED`/`DELIVERY_CANCELED`) — statuts distincts de
  DENIED/CANCELED, une livraison ratée peut être retentée, pas clairement
  une « annulation » au sens C2 ; laissé de côté plutôt que deviné.
- **Rétro-remplissage**, vérifié sur staging avant migration (2225 commandes
  annulées/refusées au total) : `brand=DELIVEROO` → `PLATFORM` systématique
  (aucun chemin d'annulation staff n'existe pour Deliveroo dans ce dépôt,
  contrairement à Uber Eats — vérifié par grep exhaustif). `brand=UBER_EATS` :
  signal décisif = `deletion_reason_id`, pas `created_by` (qui ne porte que
  l'identité du webhook créateur, jamais celle de qui annule) — reason `39`/`41`
  (catalogue `uber_eats_webhook`/`uber_eats_api`) → PLATFORM, reason dans le
  catalogue `uber_eats_cancel`/`uber_eats_deny` (12-26, 28-34 — raison Uber
  choisie par un staff pour propager l'annulation via l'API) → STAFF.
  `brand=WELLO_RESTO` : `created_by='KIOSK'` ou reason
  `KIOSK_CUSTOMER_CANCELLED`/`SNO_CUSTOMER_CANCELLED` → CUSTOMER (**bug
  préexistant découvert en vérifiant les données** : `orders.deletion_reason_id`
  est `varchar(11)`, ces deux sentinelles Go, plus longues, sont tronquées
  silencieusement à l'écriture — `KIOSK_CUSTO`/`SNO_CUSTOME` sur staging ;
  migration adaptée pour matcher les deux formes, bug lui-même **non
  corrigé** ici, hors périmètre de ce lot, signalé pour un lot séparé) ;
  reason `42`/`43` (expiration automatique d'approbation/paiement) → SYSTEM ;
  `created_by` réel (numérique ou `user-...`) + reason du catalogue générique
  `order` (motifs 1-8) → STAFF. Couverture finale : STAFF 1270, SYSTEM 485,
  PLATFORM 145, CUSTOMER 27, **NULL 298 (13,4 %, laissé tel quel plutôt que
  deviné** — essentiellement des lignes sans `deletion_reason_id` exploitable,
  legacy d'avant l'usage systématique de ce champ).
- **Artefact de données trouvé en vérifiant, corrigé dans la migration
  seulement** : des lignes historiques portent `deletion_reason_id` avec des
  guillemets littéraux dans la valeur (`"'3'"` au lieu de `"3"`, ~212 lignes)
  — `trim(both '''' from ...)` ajouté à toutes les comparaisons du
  rétro-remplissage pour les récupérer plutôt que les laisser tomber en NULL.

#### B3 — normalisation `brand_status`

- **Cause racine trouvée** : Deliveroo est le seul provider à écrire en
  minuscules, et à **deux** endroits, pas un seul — `buildOrderRequestObject`
  (`internal/webhook/deliveroo_orders/service.go`, passthrough
  `brandStatus := ord.Status` à la **création** de commande, jamais vu par
  l'audit initial du brief) et `Repository.UpdateOrderRejected`/
  `UpdateOrderAccepted`/`UpdateOrderConfirmed` (mises à jour de statut,
  littéraux `'scheduled'`/`'accepted'`/`'confirmed'` + la comparaison CASE
  elle-même). Les deux corrigés pour écrire/comparer en majuscules.
  Uber Eats et WELLO_RESTO écrivaient déjà en majuscules partout — aucun
  autre chemin d'écriture touché.
- **Backfill** : `UPDATE orders SET brand_status = upper(brand_status) WHERE
  brand_status <> upper(brand_status)` — 36 lignes sur staging (accepted:14
  canceled:11 rejected:10 placed:1, toutes `brand=DELIVEROO`), exactement le
  chiffre cité par le brief pour le périmètre total.
- **Pas de nouveau code de comparaison à corriger** : tous les filtres
  existants (`pos/accounting`, `pos/reports`, `stats`, `tasks/orders.go`,
  `tasks/payments.go`, `orders/repository.go`) comparent déjà en majuscules
  nu, sans `upper()` — corrects par construction une fois qu'aucun code
  n'écrit plus en minuscules. `analytics/scope.go` avait déjà un `upper()`
  défensif (chantier RBAC/analytics antérieur) — laissé tel quel, inoffensif.

#### B1 — audit TVA marketplace

- Constat détaillé : `docs/TVA_MARKETPLACE_AUDIT.md`. Résumé : aucun champ
  taxe dans les deux payloads webhook, `ht`/`tva` à 0 par construction (codé
  en dur côté Deliveroo, jamais assigné côté Uber Eats). Le recalcul depuis
  les lignes existe déjà et couvre les deux marketplaces, mais uniquement
  côté `internal/modules/analytics` (`pos/reports` les exclut explicitement) —
  et sa fiabilité pour ces deux marques dépend d'un mapping `tva_categories`
  qui diffère à l'auto-création de produit (Uber Eats en affecte une par
  défaut, Deliveroo non — risque de perte silencieuse de lignes côté
  Deliveroo, non quantifié). Aucun correctif posé, conformément au brief.

#### Validation

- **Corrections incidentes, hors périmètre du lot mais nécessaires pour
  vérifier le reste** : `send_invoice_email_test.go` (signature
  `customers.NewCustomersService` désynchronisée d'un chantier antérieur,
  bloquait la compilation de tout le paquet `order_life_cycle`, y compris ses
  tests d'intégration) — un seul argument `nil` ajouté, aucune logique
  touchée.
- **Migration 114 appliquée sur staging** (additive, idempotente) pour
  valider réellement le lot plutôt que sur la seule lecture du code — voir
  chiffres de rétro-remplissage ci-dessus, tous mesurés après application
  réelle, pas simulés.
- Nouveau test d'intégration Postgres
  (`cost_postgres_integration_test.go`, tag `postgres_integration`) contre
  staging : coût figé après modification de `components.purchase_price`
  (le test central du lot), `NO_RECIPE` ≠ `INCOMPLETE_RECIPE`, coût d'option
  inclus, résolution batchée strictement identique à la résolution ligne par
  ligne sur un panier mélangeant les 4 cas + un même produit sur deux lignes.
- `go build ./...`, `go vet ./...` (mêmes avertissements pré-existants,
  sans rapport, vérifiés sur l'arbre propre) et `go test ./...` diffés
  avant/après ce lot sur l'arbre entier : **zéro régression** — les 4
  échecs pré-existants (`planning/employees`, `planning/leave`,
  `planning/swaps`, `internal/modules/ubereats`) sont strictement identiques
  avant et après, et ce lot en corrige un cinquième
  (`order_life_cycle [build failed]`) au passage.
- **Suite** : nettoyage candidat signalé (factorisation complète de
  `menu.GetAllProducts`), bug de troncature `deletion_reason_id` signalé
  (non corrigé), correctif TVA marketplace éventuel — tous explicitement
  hors périmètre de ce lot, non planifiés ici.

### RBAC lot 11, Phase 6 — runbook de déploiement production (2026-09-03)

- **Livrable** : `docs/RBAC_DEPLOIEMENT_PROD.md`. Couvre en une fois les lots
  1 à 11 (production n'a jamais reçu la moindre migration ni ligne de code
  RBAC) — pas seulement le travail de ce chantier. Étend
  `docs/RBAC_BASCULE.md` (qui s'arrêtait à la migration 099) aux migrations
  103 (lot 10) et 110 (nettoyage colonnes mortes), et au code des phases 2-4
  de ce lot.
- **Principe expansion/déploiement/contraction appliqué en 3 vagues** :
  A (schéma additif seul — 094, 095, 096, 097, 098, 099, 100, 103 —
  inoffensif car le code de production actuel ne connaît aucune de ces
  tables), B (déploiement de code + `cmd/seed_system_roles` +
  `cmd/assign_admin_role` + migration 110, dans cet ordre précis), C (retrait
  du repli historique + migration 113 — non planifiés ici, bloqués sur
  l'invariant "0 lien sans role_id en production").
- **Point clé identifié en écrivant ce runbook** : le déploiement de code de
  la vague B, à lui seul, est un no-op comportemental strict pour la
  production — `Has()` ne consulte `Permissions`/le rôle que si
  `role_id != nil`, et personne n'en a un avant que `cmd/assign_admin_role`
  ne tourne. Le vrai point de bascule est cette commande, pas le déploiement.
- **Numérotation de migrations signalée** : collision de numéro entre
  `103_permission_catalog_lot10` (RBAC) et `103_production_ready_delivery_arrival`
  (chantier temps de livraison, sans rapport) — le runbook les distingue
  explicitement par nom de fichier complet pour éviter toute ambiguïté à
  l'exécution.
- **Migration 110 (colonnes legacy déjà vivantes en production)
  explicitement isolée de la vague A** : contrairement aux autres migrations
  additives, elle touche des colonnes que le code de production lit
  aujourd'hui — placée en vague B, après confirmation du nouveau déploiement,
  jamais avant ni en même temps.
- **Suite** : chantier RBAC lot 11 terminé (phases 1 à 6). Restent, hors
  périmètre et non planifiés : le rollout réel en production (dépend d'une
  décision de planification, pas de ce chantier), et la vague C.

### RBAC lot 11, Phase 5 — le repli historique documenté, `reports.sales.read` vérifiée (2026-09-03)

- **Contexte** : phase purement documentaire, aucun changement de code — le
  brief est explicite : cette branche fait tourner la production aujourd'hui
  (migrations RBAC jamais appliquées là-bas, voir `docs/RBAC_BASCULE.md`) et
  ne doit pas être retirée par ce chantier.
- **Livrable** : `docs/RBAC_REPLI_HISTORIQUE.md` — tableau exhaustif des 18
  clés du catalogue avec/sans entrée dans `legacyPermissionFallback` (10
  avec, 8 sans), ce qui se passe pour une clé sans repli (accessible au seul
  `Rights.Admin`, jamais par défaut), et la condition exacte de retrait
  (`SELECT COUNT(*) FROM users_rights WHERE enabled = TRUE AND role_id IS NULL`
  doit renvoyer 0 **en production comprise**, pas seulement en staging).
- **`reports.sales.read` vérifiée** (risque de mise en production signalé par
  le brief — c'est la clé qui garde les nouveaux endpoints analytiques) : a
  bien un repli, `CanViewReports`. Confirmé par lecture directe de
  `internal/modules/auth/permissions.go` — aucun 403 générique à craindre en
  production sur ce point.
- **Point signalé, non tranché ici (hors périmètre)** : les 5 clés du lot 10
  (`bookings.manage`, `platforms.manage`, `kiosk.manage`, `pos.analytics`,
  `seating_plan.manage`) n'ont pas de repli — avant ce lot, leurs routes
  n'avaient aucune garde RBAC (accessibles à tout compte authentifié). En
  production, où `role_id` reste NULL, l'absence de repli les a donc fait
  retomber de facto à « admin historique uniquement » — un resserrement réel,
  pas qu'une formalisation. Documenté dans `docs/RBAC_REPLI_HISTORIQUE.md`
  pour que ce soit un choix explicite si quelqu'un le remarque, pas une
  redécouverte surprise.
- **Suite** : phase 6 (runbook production, `docs/RBAC_DEPLOIEMENT_PROD.md`) —
  non commencée ici, checkpoint volontaire.

### RBAC lot 11, Phase 4 — décommissionnement de `is_admin` (`RequireAdmin` retirée, `users_rights.admin` en sursis) (2026-09-03)

- **Contexte** : suite de la phase 3. Deux routes restaient gardées par
  `middleware.RequireAdmin()` (« détient tous les droits », hors catalogue) au
  lieu d'une `permission.Key` — un chemin d'autorisation parallèle à `Has()`,
  distinct de tout ce que les phases 1-3 ont changé. Le recensement de la
  phase 1 avait déjà noté que le troisième candidat pressenti,
  `POST /users/{id}/merchant-link`, était en réalité déjà sous
  `staff.manage` : seuls deux consommateurs réels restaient.
- **Consommateurs déplacés** (`cmd/api/routes.go:625-626`) :
  `POST /users/{id}/force-reset-password` et
  `DELETE /users/{id}/merchant-link` passent de `RequireAdmin()` à
  `RequirePermission(permission.StaffManage)` — même droit que toutes les
  autres routes de gestion d'équipe juste au-dessus dans le même bloc
  (`GET/POST /users/`, `PUT /{id}/role`, etc.). Pas d'autre candidat trouvé
  (grep exhaustif `RequireAdmin\(\)` sur tout le dépôt : seuls ces deux appels
  et un snippet synthétique dans `routes_rbac_ratchet_test.go`, qui teste la
  logique générique du scanner et ne référence aucune route réelle).
- **`RequireAdmin` et `IsAdmin` retirés entièrement**, plus aucun appelant
  après le déplacement ci-dessus :
  - `middleware.RequireAdmin()` et `adminObservationKey`
    (`internal/middleware/require_permission.go`).
  - `middleware.IsAdmin()` et le fichier qui ne contenait qu'elle,
    `internal/middleware/permissions.go` (supprimé — plus rien à y garder une
    fois la fonction retirée).
  - `UserLoginRow.IsAdmin()` (`internal/modules/auth/models.go`) — l'unique
    appelant était `middleware.IsAdmin()`.
  - `HasAdminRole()` (affichage, `access.admin`/`is_admin`) **non touchée** —
    usage légitime distinct, voir le recensement de la phase 1.
- **Tests** : `internal/middleware/require_permission_test.go`,
  `TestRequirePermission_ForceResetPasswordAndMerchantLinkDelete_MovedOffRequireAdmin`
  — requête HTTP réelle à travers le routeur chi pour les deux routes
  déplacées, refusée (403) sans `staff.manage`, acceptée (200) avec, même
  gabarit que les tests `RequirePermission(permission.POSStatusManage)`
  existants. `cmd/api/routes_rbac_coverage_test.go` mis à jour (les deux
  patterns statiques attendent désormais `RequirePermission(permission.StaffManage)`
  au lieu de `RequireAdmin`).
- **Documentation tenue à jour** : `docs/RBAC_ROUTES.md` (les deux lignes +
  note d'en-tête — plus aucune route ne devrait porter `RequireAdmin`),
  `internal/middleware/rbacobserve/observer.go` (note historique sur
  l'observation `"__admin__"`, qui n'a plus de producteur).
- **`users_rights.admin` (la colonne) — en sursis, pas retirée** : à ne pas
  confondre avec le rôle admin (`roles.system_key = 'admin'`), qui portent le
  même mot pour deux choses sans rapport (c'est précisément la confusion que
  ce chantier corrige). Elle reste lue par le repli historique volontairement
  conservé en phase 5
  (`internal/modules/auth/permissions.go`, `Has()` branche `RoleID == nil` :
  `if u.Rights.Admin { return true }`), par `HasAdminRole()` (même branche,
  affichage), par `HasAccessReception()`/`CanPrintCashReport()` (affichage,
  capacités du login) et par le champ déprécié `LoginLegacyFields.Admin`. La
  migration de suppression est **préparée mais non appliquée** :
  `migrations/todo/113_drop_users_rights_admin_column.{up,down}.sql`. Elle ne
  pourra être jouée qu'une fois qu'aucun de ces lecteurs n'existe plus dans le
  code déployé — condition non remplie par ce chantier (phase 5 la garde
  intacte), voir `docs/RBAC_DEPLOIEMENT_PROD.md` (phase 6) pour l'ordre exact.
  **`docs/migration-postgres/04-schema-postgres-target.sql` volontairement
  PAS mis à jour** (contrairement à la migration 110) : cette colonne reste
  un besoin réel du code aujourd'hui, la retirer du schéma cible maintenant
  suggérerait à tort qu'elle est déjà obsolète.
- **Statut d'exécution** : `go build ./...` et
  `go build -tags postgres_integration ./...` passent. `go test
  ./internal/permission/... ./internal/modules/auth/...
  ./internal/modules/users/... ./internal/modules/roles/...
  ./internal/tasks/... ./internal/middleware/... ./cmd/api/...` passent.
- **Suite** : phase 5 (documenter précisément le repli historique — quelles
  clés en ont un, `reports.sales.read` en particulier) et phase 6 (runbook
  production) — non commencées ici, checkpoint volontaire.

### RBAC lot 11, Phase 3 — retrait du court-circuit `system_key` dans `Has()` (2026-09-03)

- **Contexte** : suite immédiate de la phase 2 ci-dessous, une fois son
  prérequis vérifié (0/30 rôle admin incomplet en staging). Le rôle
  Administrateur devient un rôle comme les autres : `UserLoginRow.Has()`
  n'accorde plus jamais un droit par la seule lecture de
  `RoleSystemKey == "admin"` — uniquement par une ligne réelle dans
  `Permissions` (elle-même chargée depuis `role_permissions`).
- **Changement** : la branche court-circuit
  (`internal/modules/auth/permissions.go`, ex-lignes 49-51) supprimée. `Has()`
  en mode rôle (`RoleID != nil`) ne fait plus qu'une seule chose : chercher la
  clé dans `Permissions`. Le repli historique (`RoleID == nil` →
  `Rights.Admin` puis `legacyPermissionFallback`) est **inchangé** — hors
  périmètre de ce lot (phase 5).
- **Tests** (`internal/modules/auth/permissions_test.go`) :
  - `TestHas_RoleMode_AdminSystemKeyDeniedWithoutExplicitGrant` — la preuve
    directe que le court-circuit est parti : un rôle `system_key=admin`
    auquel il manque une clé se voit refuser exactement cette clé (et
    seulement celle-là — les autres restent accordées).
  - `TestHas_RoleMode_AdminSystemKeyGrantedByExplicitPermissions` remplace
    l'ancien `TestHas_RoleMode_SystemKeyAdminGrantsEverything`, qui testait
    littéralement le court-circuit retiré (`Permissions: nil` suffisait) — un
    rôle admin complet accorde toujours tout, mais par appartenance
    explicite, plus par `system_key` seul.
  - Vérifié qu'aucun autre test du dépôt ne construisait un `UserLoginRow`
    avec `RoleSystemKey` en s'appuyant sur le court-circuit (grep exhaustif
    `RoleSystemKey:\s*&` — seuls `permissions_test.go` et
    `roles/service_test.go` construisent ce champ ; ce dernier ne l'utilise
    que pour des rôles `staff`, jamais `admin`).
- **`permission.SystemKeyAdmin` garde un seul rôle après ce retrait** :
  identifier *lequel* rôle marquer non supprimable / non modifiable
  (`models.ErrRoleImmutable`, garde G4). Vérifié déjà en place et suffisant
  sans changement : `UpdateRole` (rename), `ReplacePermissions` (édition des
  droits) et `ArchiveRole` (suppression) testent chacun
  `role.SystemKey != nil && *role.SystemKey == permission.SystemKeyAdmin` et
  renvoient `ErrRoleImmutable` (`internal/modules/roles/service.go:225,287,396`).
  `HasAdminRole()` (affichage `access.admin`/`is_admin`) reste également
  inchangée — c'est un usage d'affichage légitime de `system_key`, distinct de
  l'autorisation retirée ici (voir le recensement de la phase 1).
- **Statut d'exécution** : `go build ./...` passe. `go test
  ./internal/modules/auth/...` passe (12 tests, dont les 2 nouveaux/modifiés
  ci-dessus). `go test ./...` sur l'ensemble du dépôt : seuls des échecs
  **pré-existants et sans rapport** (constatés avant toute modification de ce
  lot, aucun des fichiers en cause n'étant touché) —
  `internal/modules/order_life_cycle` (échec de compilation, signature
  `customers.NewCustomersService` désynchronisée par le chantier `audit` en
  cours ailleurs dans l'arbre de travail), `internal/modules/planning/{employees,leave,swaps}`
  et `internal/modules/ubereats` (fixtures de mocks désynchronisées, sans
  rapport avec RBAC). Grep exhaustif confirmant qu'aucun autre package ne
  construit un `UserLoginRow{RoleSystemKey: ...}` ayant pu dépendre du
  court-circuit retiré.
- **Suite** : phase 4 (déplacer les deux consommateurs de
  `middleware.RequireAdmin()` — `POST /users/{id}/force-reset-password` et
  `DELETE /users/{id}/merchant-link` — vers une permission du catalogue,
  probablement `staff.manage`) — non commencée ici, checkpoint volontaire.

### RBAC lot 11, Phase 2 — invariant « rôle admin = catalogue complet », réconciliation automatisée (2026-09-03)

- **Contexte** : chantier « le rôle administrateur devient un rôle comme les
  autres » (retrait du court-circuit `RoleSystemKey == SystemKeyAdmin` dans
  `UserLoginRow.Has()`, `internal/modules/auth/permissions.go:49-51`). Ce
  court-circuit masque une dérive réelle : le recensement (phase 1 du
  chantier) a vérifié en base staging que **29 des 30 rôles admin étaient
  incomplets**, chacun manquant exactement les 5 clés du lot 10
  (`pos.analytics`, `bookings.manage`, `platforms.manage`, `kiosk.manage`,
  `seating_plan.manage`) — `go run ./cmd/seed_system_roles` n'avait jamais été
  relancé après la migration 103, alors que l'entrée du lot 10 ci-dessous le
  prescrivait. Retirer le court-circuit avant de combler ce trou aurait
  déconnecté ces 29 établissements de leurs propres droits admin. Cette entrée
  ne touche **pas** encore à `Has()` — uniquement le prérequis (phase 2 du
  chantier) : garantir l'invariant, l'automatiser, le vérifier.
- **Invariant testé** (`internal/modules/roles/postgres_integration_test.go`,
  `TestSystemAdminRolesContainFullCatalog_Postgres`) : scanne tous les rôles
  `system_key='admin'` de la base pointée par `POSTGRES_URL` et échoue si l'un
  d'eux ne porte pas l'intégralité des permissions non dépréciées du
  catalogue — même convention que `TestNoCrossTenantRoleAssignment`
  (exécutable contre dev, CI, ou un environnement réel en surchargeant
  `POSTGRES_URL`). Repose sur une nouvelle méthode,
  `Repository.FindIncompleteAdminRoles`. Un second test,
  `TestReconcileSystemRoles_BackfillsIncompleteAdminRole_Postgres`, reproduit
  la dérive constatée (supprime une permission d'un rôle admin fraîchement
  créé) et vérifie que la réconciliation la rattrape. Aucun pipeline CI
  (GitHub Actions ou autre) n'existe dans ce dépôt pour l'exécuter
  automatiquement — ces tests suivent la convention `-tags
  postgres_integration` déjà en place, seul mécanisme de gate d'intégration
  du projet à ce jour.
- **Réconciliation automatisée** : la logique de `cmd/seed_system_roles`
  (lister les établissements, `EnsureSystemRoles`, pointer
  `default_role_id`) déplacée dans `Repository.ReconcileSystemRoles`
  (`internal/modules/roles/repository.go`) — implémentation unique partagée
  par la CLI et une nouvelle tâche cron,
  `TasksManager.ReconcileSystemRolePermissions`
  (`internal/tasks/rbac.go`), enregistrée `@hourly` dans `cmd/api/tasks.go`.
  Un ajout au catalogue ne dépend plus d'une commande lancée à la main.
  Différence avec la CLI : une erreur sur un établissement n'interrompt plus
  le traitement des autres (`[]MerchantRoleReconciliation`, erreur par
  établissement) — adapté à une tâche de fond non supervisée ; la CLI, elle,
  garde un exit code non-zéro si un établissement échoue, pour qu'un run
  manuel supervisé ne passe pas inaperçu.
- **Création d'établissement déjà correcte** : `POSService.CreateMerchant`
  (`internal/modules/pos/create_service.go:52`) appelle déjà
  `EnsureSystemRoles` de façon inconditionnelle, dans la même transaction —
  vérifié, aucun changement nécessaire.
- **Staging réconcilié** (2026-09-03, `go run ./cmd/seed_system_roles` contre
  `RENDER_STAGING_DATABASE_URL`) : 30/30 établissements traités, 0 échec.
  Vérifié avant/après par requête directe et par
  `TestSystemAdminRolesContainFullCatalog_Postgres` exécuté contre staging :
  29 rôles incomplets → 0.
- **Statut d'exécution** : `go build ./...` et
  `go build -tags postgres_integration ./...` passent. `go vet -tags
  postgres_integration ./internal/modules/roles/... ./internal/tasks/...
  ./cmd/seed_system_roles/... ./cmd/api/...` ne signale que l'avertissement
  préexistant `auth/handler.go` (copie de `sync.Mutex` via
  `singleflight.Group`, sans rapport avec ce lot — voir lot 9). `go test
  ./internal/permission/... ./internal/modules/auth/...
  ./internal/modules/users/... ./internal/modules/roles/...
  ./internal/tasks/... ./cmd/api/...` passent. Tests d'intégration Postgres
  (`-tags postgres_integration`) exécutés contre staging, décrits ci-dessus.
- **Suite** : phase 3 du chantier (retrait du court-circuit dans `Has()`,
  avec test dédié « un rôle admin amputé d'une permission se voit refuser
  cette permission ») — non commencée ici, checkpoint volontaire avant d'y
  toucher.

### Droits legacy morts — nettoyage `access_delivery`/`access_waiter`/`export_reports`/`export_financials`/`export_customers` (2026-09-01)

- **Contexte** : à côté du catalogue RBAC (`permission.Key` / table `permissions`),
  le système historique de droits booléens (`UserRowRights` / colonnes
  `users_rights`) porte 17 champs. Une revue de recensement (agent de
  recherche en lecture seule, croisant l'API Go, le schéma MySQL/Postgres et
  `wello-back-office`) a montré que 5 d'entre eux ne gardent plus rien nulle
  part : ni une route API, ni une décision d'affichage côté back-office.

- **Les 5 droits retirés** :
  - `access_wrdelivery` / `access_wrwaiter` — alimentaient uniquement
    `LoginAccessResponse.Apps.Delivery/Waiter`, un mécanisme distinct du RBAC
    (pas de `permission.Key` associée ; le fallback RBAC de
    `access_wrwaiter` vers `pos.access` avait déjà disparu à la suppression
    de `pos.access` — lot 8). Confirmé mort côté `wello-back-office` (aucune
    décision de gating ne les lit) et quasi mort côté Flutter
    (`wello_resto_flutter` : un seul accesseur, déjà marqué `@Deprecated`,
    lui-même non appelé). `access_wrreception` n'est **pas** touchée : elle
    reste le fallback legacy de `pos.status.manage` et garde
    `PATCH /pos/status` pour un compte sans `role_id`.
  - `export_reports` / `export_financials` / `export_customers` — n'ont
    jamais eu de `permission.Key` dédiée (marquées « deliberately absent »
    dans `internal/modules/auth/permissions.go` depuis le lot 1/2 : lire un
    rapport et l'exporter n'ont jamais été deux droits distincts).
    Round-trippées jusqu'ici dans la réponse de login et l'éditeur de droits
    back-office (`RightsTab`), mais cet éditeur a déjà été remplacé par un
    sélecteur de rôle (`AccessTab.tsx`) — plus aucune UI ne les lit ni ne les
    coche.

- **Retiré de l'API Go** : les 5 champs de `UserRowRights`
  (`internal/modules/auth/models.go`), leurs méthodes `Has*` dérivées
  (`HasAccessDelivery`, `HasAccessWaiter`, `HasReportsExportAccess`,
  `HasFinancialsExportAccess`, `HasCustomerExportAccess`), leur lecture/
  écriture SQL (`internal/modules/auth/repository.go` ×3 requêtes,
  `internal/modules/users/{repository,admin_repository}.go`), leur
  restitution dans la réponse de login
  (`internal/modules/auth/login_response.go` :
  `LoginAccessAppsResponse.Delivery/Waiter`,
  `LoginAccessPermissionsResponse`/`LoginCapabilityActionsResponse`
  `.ExportReports/ExportFinancials/ExportCustomers`), et le DTO d'admin
  (`internal/modules/users/admin_models.go`,
  `MerchantUserPermissions`). `Capabilities.Modules.Reports/Financials/
  Customers` simplifiés en conséquence (ne dépendaient plus que du droit de
  lecture, plus du droit d'export disparu).

- **Migration** (`migrations/todo/110_drop_dead_legacy_rights_columns`) :
  `DROP COLUMN` Postgres sur `users_rights` pour les 5 colonnes, gardée par
  `to_regclass`/`IF EXISTS` (même convention que la migration 104).
  `docs/migration-postgres/04-schema-postgres-target.sql` mis à jour en
  miroir. Pas de traduction MySQL séparée : le schéma cible de ce dépôt est
  Postgres (voir les migrations 094+ sous `migrations/todo/`) — MySQL est
  l'état de départ historique, pas une cible à maintenir en parallèle.

- **Retiré de `wello-back-office`** : `MerchantUserPermissions`
  (`src/types/adminUsers.ts`), `AuthCapabilities.apps`/`.actions` et le
  fallback `allow_delivery_account`/`allow_waiter_account`
  (`src/types/auth.ts`), les fixtures (`src/services/mocks/teamMocks.ts`,
  `src/services/authService.ts`) et le payload figé de liaison d'un
  utilisateur existant (`src/components/team/CreateMemberSheet.tsx`).
  `access_reception` conservée partout (toujours vivante côté API).
  `npx tsc --noEmit` et `npx eslint` passent sans erreur sur ce dépôt après
  coup.

- **Hors périmètre, non tranché ici** : `access_wrreception` /
  `access_wrdelivery` / `access_wrwaiter` alimentent aussi
  `LoginAccessResponse.Apps`, un mécanisme distinct du RBAC potentiellement
  consommé par d'autres apps clientes (Kiosk, ScanNOrder) non auditées dans
  cette passe — seul `wello_resto_flutter` a été vérifié pour `access_wrdelivery`/
  `access_wrwaiter` (mort ou quasi mort, voir ci-dessus).

### RBAC lot 10 — 5 nouvelles clés (bookings/platforms/kiosk/analytics/plan de salle) + backfill des descriptions (2026-08-28)

- **Contexte** : plusieurs surfaces back-office restaient accessibles à tout
  utilisateur authentifié sans droit RBAC dédié — paramètres de réservation,
  onglet « Canaux et Plateformes », onglet « Kiosk », page « Analyse », page
  « Plan de salle ». Migration `migrations/todo/103_permission_catalog_lot10.up.sql`
  ajoute 5 clés : `bookings.manage`, `platforms.manage`, `kiosk.manage`,
  `pos.analytics`, `seating_plan.manage`. `internal/permission/keys_gen.go`
  mis à jour en miroir (13 → 18 clés).
- **Principe de garde appliqué** (repris du lot 8) : CONFIGURATION gardée,
  CONSULTATION/SAISIE courante laissée libre.
  - `bookings.manage` : uniquement `/bookings/settings*` (PUT settings,
    CRUD des `duration-rules`, PUT hours). La liste/gestion courante des
    réservations (`POST /create`, accept/deny/cancel/seat/…) n'est **pas**
    concernée — hors périmètre de la demande, qui ne visait que les
    paramètres.
  - `platforms.manage` : toutes les routes de configuration sous
    `/integrations` (PATCH uber-eats/deliveroo/scannorder, onboarding
    scannorder, logo/banner, toggles globaux, liens Stripe Connect/branding).
    `GET /integrations/stripe/balance` reste gardé par
    `reports.financial.read` seul (lot 8) — pas de double garde.
  - `kiosk.manage` : les mutations de `/pos/settings/kiosk` (codes
    d'enrôlement, devices, réglages/logo/idle-media). Les deux routes
    admin-pin restent gardées par `settings.manage` seul (exception
    documentée de longue date, `docs/KIOSK_DECISIONS.md`, non touchée). Les
    `GET` restent libres.
  - `pos.analytics` : ancrage unique `GET /stats/upsell` — seule route
    "analytics" qui n'était couverte par aucun droit `reports.*` existant.
    La page « Analyse » du front peut afficher d'autres widgets déjà
    couverts par `reports.sales.read`/`reports.financial.read` ; seul
    `pos.analytics` gate la page dans son ensemble côté front.
  - `seating_plan.manage` : tout `/floors*` (CRUD floors/obstacles/areas) et
    les mutations de tables sous `/locations` (`POST .../tables`, PATCH/DELETE
    `/tables/{id}`). `GET /locations` reste libre — utilisé ailleurs (prise
    de commande, `AssignBookingLocations`), pas seulement par la page « Plan
    de salle ».
- **`sort_order`** : `pos.analytics` = 55 (rejoint le groupe `pos.*` existant,
  entre `pos.cash_drawer.open`=50 et `catalog.manage`=60) ; les 4 autres sont
  de nouveaux domaines, ajoutés après `settings.manage`=140 (150/160/170/180).
- **Backfill des `description`** : les 13 clés existantes avaient toutes une
  `description` vide (`095` l'omettait délibérément — "content is a
  product-copy task"). Le back-office affiche maintenant systématiquement ce
  champ sous chaque droit (retravail `PermissionsEditor.tsx` de la même
  session côté front) ; laissé vide, ça rendait une ligne blanche sous
  chaque droit. La migration 103 fait l'`UPDATE` des 13 existantes en même
  temps qu'elle insère les 5 nouvelles avec une description dès le départ.
- **Ratchet RBAC** (`cmd/api/routes_rbac_ratchet_test.go`) : le nombre de
  routes mutatives non gardées passe de 212 à 175 — `unguardedMutativeRouteCeiling`
  abaissé en conséquence.
- **Backfill des rôles existants** : cette migration ajoute des clés au
  catalogue mais ne touche `role_permissions` d'aucun établissement — à
  lancer juste après déploiement : `go run ./cmd/seed_system_roles` (même
  précédent que pour `097_permission_pos_status_manage`), pour que le rôle
  « Administrateur » de chaque établissement récupère les 5 nouvelles clés.
- **Statut d'exécution** : `go build ./...` et
  `go test ./internal/permission/... ./cmd/api/... -run TestAllMatchesMigrationCatalog|TestNoDuplicateKeys|TestRBACPermissionCoverage|TestRBACRatchet`
  passent. Tests d'intégration Postgres non relancés (pas d'accès
  Postgres/Redis locaux dans cette session — même limite que les lots
  précédents).

### RBAC lot 9 — clés à points dans /login, admin dérivé du rôle, rôle exposé sur GET /users/{id} (2026-08-27)

- **Contexte** : lot 9 = écrans d'administration des rôles côté back-office
  (dépôt `wello-back-office`). Deux prémisses du brief front se sont révélées
  fausses à la lecture du code, corrigées ici côté API avant que le front ne
  s'appuie dessus.

- **`permissions: string[]` ajouté à la réponse de login**, en sibling de
  `access` (pas dedans — `access.permissions` garde sa forme et ses noms
  historiques, d'autres clients en dépendent).
  `internal/modules/auth/login_response.go` (nouveau champ) +
  `service.go` (`buildLoginResponse`, `Permissions: user.Permissions`). Donnée
  déjà calculée par `attachRolePermissions`/`loadRolePermissions`
  (`repository.go`, RBAC lot 2) — rien de recalculé. Un seul site de
  construction réel existe (`buildLoginResponse`, appelé uniquement par
  `AuthService.Login`) : le login par mot de passe, le login par PIN
  (`AuthenticatePIN` délègue à `Login`) et la bascule d'établissement
  (`switchMerchant`/`loginWithToken` côté back-office, `POST /auth/login`
  avec un autre token porteur) passent tous par ce même chemin. `LoginOld`
  est du code mort (entièrement commenté). La restauration de session est
  100% côté client (localStorage, aucun appel réseau) — aucun chemin API à
  couvrir de ce côté.
- **Garde-fou `permission.FilterValid`** (`internal/permission/filter.go`) :
  ne garde que les clés présentes dans `permission.All`, appelé dans
  `attachRolePermissions` avant d'écrire `data.Permissions`. La contrainte FK
  de `role_permissions.permission_key` devrait déjà garantir cet invariant
  (cf. entrée du lot 8 ci-dessous) ; ce filtre le rend explicite et testable
  plutôt que de reposer uniquement sur la contrainte DB. Tests :
  `internal/permission/filter_test.go`,
  `internal/modules/auth/login_response_test.go`
  (`TestBuildLoginResponse_PermissionsPassthrough` — le pendant, côté sortie,
  de `TestRBACPermissionCoverage`/`keys_gen_test.go`).

- **Bug trouvé en cours de route, corrigé le même jour** : `access.admin`
  (réponse de login) et `is_admin` (`GET /me/permissions`) lisaient tous les
  deux directement `user.Rights.Admin` — la colonne booléenne historique,
  jamais le rôle. Or `Has()` (l'autorisation réelle, utilisée par
  `RequirePermission`) ignore déjà `Rights.Admin` dès que `role_id` est
  renseigné, même s'il contredit le rôle (`permissions.go:43-64`, testé). Les
  deux champs d'affichage étaient donc en décalage avec l'autorisation
  réelle — sans conséquence pour l'API elle-même (les routes catalogue
  restent correctement gardées), mais un risque concret pour ce lot : un
  compte non-admin avec `Rights.Admin` resté à `true` (signalé comme
  fréquent en production) aurait affiché `access.admin = true`, et
  `usePermissions().has()` côté front court-circuite sur ce drapeau — aucun
  menu n'aurait jamais été masqué, quel que soit le rôle réellement assigné.
  **Correctif** : nouvelle méthode `UserLoginRow.HasAdminRole()`
  (`internal/modules/auth/permissions.go`), qui reprend exactement la
  branche admin de `Has()` (role_id renseigné → `RoleSystemKey == "admin"` ;
  sinon repli sur `Rights.Admin`). Utilisée par `buildLoginResponse` pour
  `access.admin` et par `roles.Service.MyPermissions` pour `is_admin`
  (remplace le `OR` avec `Rights.Admin` qui y subsistait). **Distincte
  d'`IsAdmin()`** (`models.go`), qui reste `Rights.Admin` tel quel et continue
  de servir `middleware.RequireAdmin()` — une décision d'autorisation
  différente (« détient tous les droits », hors catalogue), non touchée ici,
  hors périmètre du lot 9. Tests : 4 cas ajoutés à
  `internal/modules/auth/permissions_test.go`.

- **`GET /users/{id}` expose désormais `role_id`/`role`** (nécessaire pour le
  sélecteur de rôle du nouvel onglet « Accès » côté back-office).
  `internal/modules/users/admin_repository.go` :
  `GetMerchantUserByID` a désormais sa propre requête (LEFT JOIN `roles`) et
  son propre scan (`scanMerchantUserDetail`), plutôt que de faire grossir la
  requête/scan partagés avec `ListMerchantUsers`
  (`scanMerchantUserListItem`) — la liste n'a pas besoin de ces colonnes.
  `MerchantUserDetail` (`admin_models.go`) gagne `RoleID *string` et
  `Role *RoleRef` (nil si l'utilisateur n'a pas encore de `role_id`, monde
  pré-lot-4). Handler inchangé : `models.SendJSON` sérialise la struct
  directement, sans liste blanche de champs — confirmé en lisant
  `admin_handler.go`. Fixture de test partagée
  (`merchantUserDetailRows`, `admin_service_test.go`) mise à jour pour les 3
  colonnes supplémentaires (nulles — aucun test existant n'exerce le chemin
  rôle).

- **Statut d'exécution** : `go build ./cmd/api/...` (et un `go build` scopé
  aux paquets touchés — l'environnement de build a rencontré un disque C:
  plein pendant cette session, contournement en limitant le scope plutôt
  qu'un `./...` complet) ; `go test ./internal/permission/...
  ./internal/modules/auth/... ./internal/modules/users/...
  ./internal/modules/roles/...` passent, y compris après correction de la
  fixture `merchantUserDetailRows`. `go vet` sur les mêmes paquets ne
  signale que deux avertissements préexistants dans `auth/handler.go` (copie
  de `sync.Mutex` via `singleflight.Group`), non liés à ce lot. Tests
  d'intégration Postgres non relancés (pas d'accès Postgres/Redis locaux
  dans cette session — même limite que les lots précédents).

### RBAC lot 8 — catalogue 15 → 13, trois gardes mal posées retirées (2026-08-27)

- **Contexte** : RBAC lot 7 (audit, `docs/RBAC_CLIENTS.md`) avait trouvé trois
  gardes de sur-dosage (un droit `*.manage` posé sur une CONSULTATION ou une
  SAISIE courante plutôt que sur une CONFIGURATION/CORRECTION) et huit droits
  du catalogue qui ne gardaient aucune route. Ce lot traite les deux : retire
  les trois gardes, relie six des huit droits orphelins à une route, et
  supprime les deux restants du catalogue. Un des trois retraits a lui-même
  fait naître un neuvième orphelin en cours de route (`haccp.manage`, voir
  ci-dessous) — également résolu le même jour.

- **`/haccp/traceability` — garde `haccp.manage` retirée (POST, GET, GET
  `/{id}`).** Motif d'origine de la garde (posée le 2026-07-23, commit
  `0b4509f` « ready for staging », message sans contexte) : **aucune trace
  écrite n'en subsistait dans le dépôt** — ni `docs/decisions.md` (qui a une
  entrée à cette date, mais sur un tout autre sujet : un trou de schéma
  Postgres pour la même fonctionnalité), ni aucun autre document, ni le
  message de commit lui-même. Confirmé par l'auteur de la décision dans cette
  session : **le raisonnement était le même que celui retiré ci-dessous pour
  `POST /customers/`** — « ça écrit des données », donc ça mérite une garde.
  C'est exactement le raisonnement que le principe directeur du lot 7 invalide
  (écrire une donnée ne rend pas une action CONFIGURATION — relever une
  température écrit aussi une donnée, et n'est pas gardé). La traçabilité
  HACCP (réception de marchandise tracée, photo + commentaire) est une
  obligation légale quotidienne, saisie par n'importe quel employé de cuisine
  via l'app Flutter — exactement comme le relevé de température ou le log de
  nettoyage juste à côté dans le même module, tous deux libres. Lecture ET
  écriture passent donc libres, alignées sur le reste de `/haccp`.
  **Conséquence, tranchée le jour même** : retirer cette garde a laissé
  `haccp.manage` sans aucune route (orphelin au sens du lot 7/8), attrapé par
  `TestRBACPermissionCoverage`. Décision (même journée, 2026-08-27) : `PUT
  /haccp/settings` (paramétrer les seuils et réglages du module — CONFIGURATION,
  à l'inverse de la traçabilité qui est de la SAISIE) porte maintenant
  `haccp.manage`. Les autres candidats CONFIGURATION identifiés par l'audit
  (créer/éditer les zones et surfaces de nettoyage, les zones de température)
  restent libres, non traités par ce lot. **Suivi explicitement différé à un
  lot futur, non implémenté ici** : masquer la section HACCP du menu
  back-office pour un compte sans `haccp.manage`, symétriquement à la garde
  API — aujourd'hui rien ne cache ce menu, un compte sans le droit verrait
  l'écran de paramétrage puis un 403 à l'enregistrement.

- **`POST /customers/` (création unitaire) — garde `customers.manage`
  retirée.** Créer une fiche client est une SAISIE courante (un serveur
  inscrivant un client au programme de fidélité en salle en a besoin au
  quotidien), pas une CONFIGURATION — `customers.manage` reste sur les 3
  routes d'import en masse, qui écrivent potentiellement tout le fichier
  client d'un coup. `GET /customers/list` et `GET /customers/search` restent
  libres, comme avant ce lot (confirmé, aucun changement).

- **`pos.access` et `pos.discount.apply` supprimés du catalogue** (migration
  `100_deprecate_pos_access_and_discount_apply`). Ni l'un ni l'autre ne
  gardait de route, et aucun remplacement n'est prévu : encaisser et
  appliquer une remise restent, comme ils l'ont toujours été, des gestes
  libres pour tout compte authentifié — ce ne sont pas des actions qu'un
  restaurateur voudrait réellement restreindre à certains employés. La
  migration supprime d'abord les lignes `role_permissions` référençant ces
  deux clés sur tous les rôles de tous les établissements (contrainte FK,
  même raisonnement que le down de la migration 095), puis les deux lignes du
  catalogue `permissions`. `internal/permission/keys_gen.go` régénéré (15 →
  13 constantes) ; `legacyPermissionFallback`
  (`internal/modules/auth/permissions.go`) perd l'entrée `pos.access` ->
  `AccessWaiter`.

- **Rôle système « Employé polyvalent » (`system_key = 'staff'`) : ne porte
  plus aucun droit par défaut.** C'était le seul rôle du catalogue à porter
  `pos.access` et `pos.discount.apply` par défaut
  (`internal/modules/roles/repository.go`, `systemRolePermissions[SystemKeyStaff]`,
  désormais `{}`) — les deux droits supprimés ci-dessus. C'est le résultat
  attendu de cette suppression, pas une régression à corriger après coup :
  tout ce qu'un employé polyvalent fait au quotidien (encaisser, appliquer
  une remise, relever une température, tracer une réception, prendre une
  commande...) reste et est resté intégralement libre côté routes — les 13
  droits qui restent au catalogue gardent tous des gestes d'encadrement
  (correction : rouvrir un ticket, rembourser ; configuration : gérer le
  menu, le planning, les stocks ; rapport : consulter les ventes ou les
  finances) qu'un employé polyvalent n'exerce pas par définition du rôle.
  Aucun droit n'a été réattribué arbitrairement pour « remplir » le rôle —
  un rôle vide qui garde son nom et sa place est l'état correct ici, pas un
  trou à combler. `staff` reste entièrement client-éditable : un
  établissement qui veut lui donner des droits d'encadrement le fait via
  `PUT /roles/{id}/permissions`, comme pour n'importe quel rôle personnalisé.

- **Trace laissée pour la prochaine fois** : le motif de la garde HACCP de
  juillet n'existait nulle part par écrit — reconstitué ici en croisant le
  commit qui l'a posée, `docs/decisions.md`, `docs/migration-postgres/56-*`
  et `docs/PERMISSIONS_MIDDLEWARE_GUIDE.md`, pour un motif qui tient en une
  phrase. Cette entrée existe pour que personne n'ait à refaire ce travail
  pour les décisions d'aujourd'hui.

### Reservation (site public) — double conversion de fuseau à la création, résa décalée de -2h (2026-08-13)

- **Symptôme rapporté** : sur le site de réservation public, sélectionner
  un créneau à 19h (heure marchand, Europe/Paris/CEST) stockait
  `booking_date_from` = 17h+02:00, soit 15h UTC au lieu des 17h UTC
  attendus (19h CEST) — un décalage exact d'un offset marchand (-2h) par
  rapport à la valeur correcte. Le POS affichait donc 17h, correctement,
  vu que le correctif précédent (cf. entrée du jour "heure décalée dans la
  liste des résas") lit fidèlement ce qui est en base — la donnée
  elle-même était fausse à l'écriture, pas seulement mal affichée.
- **Root cause : double conversion locale→UTC dans le flux public
  `POST /rsv/{slug}/booking/create`** (`internal/modules/reservation/`) :
  1. `service.go:193-194` (`CreateReservation`) parse correctement
     `"19:00:00"` avec `time.ParseInLocation(..., merchant.Timezone)` →
     19:00 CEST = 17:00 UTC. Correct.
  2. `service.go:218-219` reconvertit ce temps en chaîne UTC naïve
     (`"17:00:00"`, sans marqueur de fuseau — indiscernable d'une heure
     locale dans le format `"2006-01-02 15:04:05"`) et **réécrit
     `req.Booking.StartDate`/`EndDate` avec cette valeur déjà-UTC**.
  3. `repository.go:434-443` (`CreateBookingTransaction`) recevait cette
     chaîne déjà-UTC mais la reparsait **avec le fuseau marchand**
     (`loadMerchantLocation` + `ParseInLocation(..., loc)`), la traitant à
     tort comme de l'heure locale une deuxième fois → 17:00 CEST = 15:00
     UTC stocké.
  - `bookingcore.CreateBooking` (partagé avec le flux staff) n'est pas en
    cause : il fait fidèlement le seul `.UTC()` nécessaire sur la valeur
    qu'on lui passe — le problème est amont, dans la valeur déjà faussée
    reçue.
- **Écosystème audité pour écarter les autres suspects** :
  - Site public (`luxury-table-booking`) : confirmé innocent — le payload
    `start_date` est une simple concaténation de chaînes
    (`` `${date} ${time}:00` ``, `src/lib/api.ts`), aucun objet `Date`,
    aucune conversion, aucune lib de fuseau (`dayjs`/`luxon`/`date-fns-tz`)
    sur ce chemin, vérifié source + bundle buildé.
  - Flux staff (`internal/modules/bookings`) : non affecté — son
    `service.go` ne pré-convertit jamais en UTC, le repository fait
    l'unique `ParseInLocation` avec le fuseau marchand.
  - `UpdateReservation` (reschedule public, `service.go:311-363`) : même
    pré-conversion UTC en `service.go:350-351`, mais **pas de bug** —
    `repository.go UpdateBooking` (ligne 405-416) ne reparse jamais,
    bind directement la chaîne UTC déjà prête dans la requête SQL. C'est
    ce contraste (`UpdateBooking` correct vs `CreateBookingTransaction`
    buggé) qui a confirmé où était l'unique conversion de trop.
  - Idempotence (`tryIdempotentReplay`/`saveIdempotencyResult`/
    `clearPendingIdempotency`) vérifiée : clé sur `(slug, idempotencyKey)`
    uniquement, aucune dépendance au format de date — sans impact du
    correctif.
  - `FindExistingActiveBookingWarning` (`service.go:225`,
    `repository.go:288-314`) vérifiée : compare `booking_date_from = ?`
    directement contre la colonne (vraie UTC), donc a bien besoin de la
    chaîne déjà-UTC produite par `service.go:219` — confirme que la
    pré-conversion UTC en `service.go` doit rester ; le correctif ne
    devait toucher que le second parse en trop côté repository.
- **Correctif appliqué** (`internal/modules/reservation/repository.go`,
  `CreateBookingTransaction`) : les deux `ParseInLocation` reparsent
  désormais avec `time.UTC` au lieu du fuseau marchand (la chaîne reçue
  est déjà UTC à ce stade — un simple parse, pas une conversion). Le
  helper `loadMerchantLocation` de ce fichier, devenu sans appelant,
  supprimé.
- **Statut d'exécution** : `go build ./...` et
  `go vet ./internal/modules/reservation/...` (y compris sous
  `-tags postgres_integration`, pour couvrir `postgres_integration_test.go`)
  passent. **Dette de test notée** : `internal/modules/reservation` n'a
  aucun test unitaire (`go test` → `[no test files]`), et son seul test
  d'intégration (`postgres_integration_test.go`) seed les données
  directement en SQL sans passer par `CreateReservation`/
  `CreateBookingTransaction` — il n'exerçait donc pas ce chemin avant le
  bug et ne le couvre toujours pas après le correctif. Une connexion
  Postgres locale de dev (`POSTGRES_URL`, cf. doc
  `internal/database/dbx/pgtest/pgtest.go`) serait nécessaire pour ajouter
  et exécuter un test de bout en bout ; non fait cette session (seule
  `RENDER_STAGING_DATABASE_URL`, une base staging partagée, était
  disponible — écarté pour ne pas faire tourner un test mutateur dessus
  sans validation préalable).

### Bookings — lien de gestion dans le SMS, au même titre que l'email (2026-08-13)

- **Demande** : le SMS envoyé au client ne contenait pas de lien vers la
  page de gestion de sa réservation, contrairement à l'email qui a déjà un
  bouton "Gérer ma réservation" (`{{if .ManagementLink}}` dans les
  templates `booking_confirmation.html`, `booking_reminder.html`,
  `booking_modification.html`, `booking_reconfirmation.html`).
- **Où** : tout le montage email/SMS des réservations est centralisé dans
  `internal/modules/bookingcomm/service.go` (`BookingMessage`, un seul
  point d'envoi par type de message, découplé des modules métier `bookings`/
  `reservation` pour éviter les cycles d'import). Le lien existait déjà,
  construit par `BookingMessage.managementLink(baseURL)` (`{baseURL}/restaurant/{slug}/booking/{bookingNumber}`,
  `baseURL` = `PUBLIC_RESERVATION_BASE_URL`) mais n'était consommé que par
  `emailData()` pour l'email — jamais passé aux `fmt.Sprintf` des corps SMS.
  Tous les call sites (`bookings/communication.go`, `bookings/reminders.go`,
  `bookings/service.go`, `reservation/service.go`) peuplent déjà
  `MerchantSlug`+`BookingNumber` sur le `BookingMessage`, donc aucun
  plumbing supplémentaire n'était nécessaire.
- **Correctif** : nouvelle méthode `(*Service) smsWithManagementLink(m, text)`
  qui ajoute le lien (même lien que l'email) en suffixe du corps SMS,
  séparé par un espace, et renvoie le texte inchangé si le lien est
  indisponible (slug/booking_number manquant ou `baseURL` non configuré) —
  évite l'espace final orphelin. Branché sur `SendConfirmation`,
  `SendReminder`, `SendModification`, `SendReconfirmation`. **Pas** branché
  sur `SendCancellation`, à l'image de l'email : `booking_cancellation.html`
  n'a pas de bloc `ManagementLink`, rien à gérer une fois la résa annulée.
  Liste d'attente (`SendWaitlistAvailable`/`WaitlistMessage`) non concernée :
  pas de réservation existante à gérer, pas de `BookingNumber`/`MerchantSlug`
  dans cette struct.
- **Point de vigilance signalé, non traité** : aucune limite de longueur/
  encodage GSM-7 n'existe dans le code SMS (`brevo_sms/service.go`) — les
  corps étaient déjà proches d'un segment de 160 caractères pour certains
  marchands/numéros de résa ; l'ajout de l'URL (~50-70 caractères selon le
  slug) fera probablement basculer une partie des envois en multi-segments
  (facturation Brevo par segment). Aucune action prise sur ce point, à
  arbitrer séparément si le volume/coût SMS devient un sujet.
- **Statut d'exécution** : `go build ./...`, `go vet ./internal/modules/bookingcomm/...`
  et `go test ./internal/modules/bookingcomm/... ./internal/modules/bookings/...`
  passent. Tests ajoutés (`service_test.go`) vérifiant la présence du lien
  dans le SMS pour confirmation/rappel/modification/reconfirmation, son
  absence pour l'annulation, et l'absence de lien + absence d'espace final
  quand `PUBLIC_RESERVATION_BASE_URL` n'est pas configuré. Pas de vérification
  d'envoi réel (Brevo) dans cette session.

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

### Rapport comptable — labels exclus en plus des codes MOP, `cash_registers_items` retiré du réel (2026-08-13)

- **Exclusion par libellé affiché** : `accountingExcludedChannelMOPs`
  (`STRIPE`/`UBER_EATS`/`DELIVEROO`) filtre en SQL le code MOP brut
  (`cri.mop`/`crci.label`) *avant* la jointure vers `labels` — un custom
  item à texte libre saisi littéralement "Uber Eats"/"Deliveroo"/
  "ScanNOrder" (au lieu du code MOP exact) passait donc au travers. Ajout
  d'un filtre applicatif `filterExcludedPaymentLabels`
  (`accounting/service.go`), en aval de `GetRealPaymentsData`, qui compare
  le libellé affiché en majuscule (`UBER EATS`/`DELIVEROO`/`SCANNORDER`)
  — garde-fou en plus du filtre SQL existant, pas un remplacement.
- **`cash_registers_items` totalement retiré de `GetRealPaymentsData`** :
  confirmé par le métier (Ilies, 2026-08-13) — un restaurateur ne peut
  enclose sa caisse qu'après avoir lui-même ressaisi le détail réel en
  `cash_registers_custom_items`, avec un écart proche de 0 exigé avant de
  pouvoir valider. `cash_registers_items` (instantané MOP automatique posé
  à la clôture, cf. entrée précédente) est donc redondant pour ce
  rapport : la requête de `GetRealPaymentsData` ne lit plus que
  `cash_registers_custom_items`, l'UNION vers `cash_registers_items` a été
  supprimée.
  - `GetTrustedEnclosedRegisterIDs` (comparaison frozen `cash_registers_items`
    vs live `payments`, garde-fou anti-dérive) **n'est pas concernée** :
    elle continue de s'appuyer sur `cash_registers_items` pour détecter
    qu'un paiement a été corrigé après l'enclose d'un registre — ce
    garde-fou reste nécessaire même si son contenu ne sert plus à
    afficher les montants du rapport.
  - Tests d'intégration Postgres mis à jour
    (`accounting/postgres_integration_test.go`) : les `mopPayment` seedés
    restent nécessaires pour exercer le frozen/live check, mais les
    montants attendus dans `GetRealPaymentsData` viennent désormais de
    `customItemSeed`/`AddCustomItem` ajoutés en parallèle, pas des
    `mopPayment` seuls.

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
