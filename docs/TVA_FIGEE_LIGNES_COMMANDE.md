# TVA figée sur chaque ligne de commande

Chantier préalable au chantier « export comptable par canaux / modes de
clôture » (cf. docs/decisions.md). Démarré le 2026-10-04.

## Pourquoi

Jusqu'ici, tout rapport retrouvait le taux de TVA d'une ligne au moment de la
lecture : `orderitems.product_id` → `products.tva_in_id / tva_take_away_id /
tva_delivery_id` (selon `orders.order_type`) → `tva_categories.tva_rate`.
Rien n'était conservé sur la ligne. Conséquence : si un restaurateur change la
catégorie de TVA d'un produit (ou si un taux de catégorie change), **tous ses
anciens rapports régénérés changent** — export comptable, registre de caisse,
rapport journalier, analytics, factures renvoyées. Même chose pour les frais de
livraison (catégorie globale `tva_id = -1`).

Objectif : qu'un rapport ou une facture régénéré(e) plus tard redonne les mêmes
montants (exigence d'inaltérabilité, loi anti-fraude 2018).

## Décisions (prises avec Ilies, 2026-10-04)

- **On fige la catégorie et le taux seulement** (`tva_id`, `tva_rate`), pas le
  titre. Le titre est relu dans `tva_categories` : un renommage de catégorie
  change le libellé d'un ancien rapport, jamais ses montants.
- **Frais de livraison** : la catégorie est toujours `-1`, on ne fige que le
  taux, sur la commande (`orders.delivery_fees_tva_rate`).
- **Rattrapage de l'historique intégré à la migration**, par lots. Il ne peut
  se faire qu'à partir de la configuration *actuelle* des produits : c'est une
  reconstruction, signalée par `tva_reconstructed = true` (même principe que
  `discount_redemptions.is_reconstructed`). Une ligne dont le produit ou la
  catégorie est introuvable reste vide plutôt que devinée.
- **Les suppléments (`extra`)** n'ont pas de TVA propre : ils suivent le taux
  de leur ligne, comme avant.

## Conception

### Écriture

Les lignes de commande ne sont écrites qu'à deux endroits, tous deux dans
`order_life_cycle` : `CreateOrder` (création, appelée par la caisse, la borne,
ScanNOrder et les webhooks Uber Eats / Deliveroo) et `UpdateOrder`
(modification d'une commande ouverte). Les 10 autres `UPDATE orderitems` du
dépôt ne touchent que la production / distribution (ni produit, ni prix, ni
type de commande).

Le type de commande peut changer pendant que la commande est ouverte
(`updateOrderBase`), et le taux en dépend. D'où une seule fonction,
`freezeOrderVAT`, appelée à la fin de `CreateOrder` et de `UpdateOrder`, qui
recalcule en une requête la TVA de **toutes** les lignes de la commande, plus
le taux des frais de livraison.

Règle : on (re)fige tant que la commande n'est pas close, ou si la valeur est
encore vide. Une commande close garde ses valeurs figées pour toujours, même si
un chemin la réécrivait.

### Lecture

Partout, la catégorie devient `COALESCE(oi.tva_id, <dérivation produit>)` et le
taux `COALESCE(oi.tva_rate, tva.tva_rate)` (idem frais :
`COALESCE(o.delivery_fees_tva_rate, tva_fees.tva_rate)`). Le repli sur le
produit couvre les lignes encore vides (fenêtre entre la migration et le
déploiement, lignes au produit introuvable). Expressions centralisées dans
`internal/models` pour ne pas les dupliquer.

## Décisions prises pendant l'implémentation

- **Échec du figeage = échec de l'écriture de la commande** (et non une simple
  erreur loguée, comme envisagé au départ). `UpdateOrder` tourne dans une
  transaction (`RunInTx`), et certains appelants de `CreateOrder` aussi : en
  Postgres, une requête en échec invalide toutes les suivantes de la
  transaction, donc un échec « toléré » ferait de toute façon échouer la suite
  avec une erreur trompeuse. Seul échec plausible : migration 164 non
  appliquée — d'où l'ordre de déploiement (migration d'abord).
- **Regroupement des rapports par (titre, taux)** et non plus par titre seul
  (`GetTVAData`, `pos/reports.GetTVAReportData`) : une catégorie dont le taux
  a changé peut désormais porter deux taux sur une même période.
- **Registre de caisse** : les montants HT/TVA suivent le taux figé de chaque
  ligne ; le taux *affiché* reste celui de la catégorie (la requête groupe par
  catégorie). Identique tant qu'un taux de catégorie ne change pas.
- **Lecture des commandes (`orders_fetcher_builder`)** : la caisse reçoit pour
  chaque ligne les trois taux du produit (sur place / à emporter / livraison)
  et recalcule elle-même les totaux quand on change le type de service. Le
  taux figé ne remplace donc celui du type de la commande **que pour une
  commande close** (réédition de ticket, facture) ; une commande ouverte garde
  les taux du produit, refigés à l'enregistrement suivant.
- **Facture** : `GetDeliveryFeesVATRate` prend désormais l'id de la commande et
  lit le taux figé des frais avant celui de la catégorie -1. Les taux des
  lignes viennent de la lecture des commandes ci-dessus.
- **Migration dans `migrations/todo/`** : le dossier avait été vidé par le
  commit e07b7ff (tout déplacé dans `done/`), mais c'est lui que lit le
  contrôle de démarrage `WarnUnrecordedMigrations` pour signaler une
  migration non jouée — et 164 ne l'est pas encore.

## Fichiers touchés

- `migrations/todo/164_freeze_order_vat.{up,down}.sql`
- `internal/models/orders_model.go` : `OrderItemTVAIDSQL`,
  `OrderItemTVARateSQL`, `DeliveryFeesTVARateSQL`.
- Écriture : `order_life_cycle/repository.go` (`freezeOrderVAT`, appels dans
  `CreateOrder` et `UpdateOrder`, `GetDeliveryFeesVATRate(orderID)`),
  `order_life_cycle/service.go` (facture).
- Lecture : `pos/accounting/repository.go` (`GetTVAData`,
  `GetVATAggregationRows`), `cash_registers/repository.go`
  (`cashRegisterReportSQL`, remplacée depuis par `computeRegisterVAT` dans
  `cash_registers/register_vat.go` — phase 4 de docs/EXPORT_COMPTABLE_MODES_CLOTURE.md), `pos/reports/repository.go`
  (`GetTVAReportData`), `analytics/repository.go` (`htLineExpr`,
  `htLineJoins`, `deliveryFeeHTExpr`, CA HT, `GetVATByRate*`),
  `analytics/upsell.go`, `stats/repository.go`, `orders/orders_fetcher_builder.go`.
- Non touché, volontairement : `orders/repository.go:GetProductsForPricing`
  (tarification d'un panier, pas de ligne existante) et `computeOrderTotals`
  (calcul à l'écriture, déjà sur la configuration du jour).

## Hors périmètre, signalé

- **Registre de caisse, frais de livraison** : le HT est calculé
  `frais × (100 − taux) / 100` au lieu de `frais × 100 / (100 + taux)` —
  300 € à 20 % donnent HT 240 / TVA 60 au lieu de 250 / 50. Préexistant, non
  corrigé ici — **corrigé depuis** par la phase 4 de
  docs/EXPORT_COMPTABLE_MODES_CLOTURE.md.

## Déploiement

1. Jouer `164_freeze_order_vat.up.sql` (hors bloc transactionnel, de
   préférence hors service).
2. Déployer le code.
3. Rejouer le même fichier pour compléter les lignes créées entre 1 et 2
   (il ne touche que les valeurs encore vides).

## Tests

- `order_life_cycle/vat_freeze_postgres_integration_test.go` : figeage selon
  le type, recalcul au changement de type, commande close intouchable,
  remplissage d'une ligne vide, taux des frais et lecture facture.
- `pos/accounting/postgres_integration_test.go` : une ligne à catégorie figée
  différente du produit est comptée avec sa catégorie figée dans l'export
  comptable, l'agrégation TVA et le rapport journalier ; taux figé des frais.
- Les tests existants (registre, analytics, stats) couvrent la non-régression
  quand les colonnes sont vides.

## Statut (2026-10-04)

- **Migration 164 appliquée par Ilies en prod puis sur staging.** Sur staging,
  le rattrapage a figé 77 443 lignes sur 77 464 ; les 21 restantes (produit ou
  catégorie introuvable) restent vides, comme prévu. En prod, le code n'est
  pas encore déployé : les lignes créées d'ici là restent vides — **rejouer le
  fichier 164 après le déploiement** (étape 3 ci-dessus).
- **Tests d'intégration contre staging** (`-p 1`) :
  - verts : `order_life_cycle` / `TestOrderLifeCycleRepository_FreezeOrderVAT_Postgres`
    jusqu'à la lecture des commandes (cf. ci-dessous), `pos/accounting` (les
    trois tests, dont les nouveaux cas TVA figée et DENIED), `cash_registers`,
    `pos` ;
  - lecture des commandes (`FetchAndBuildOrders`) : d'abord invérifiable, la
    migration 162 (`order_item_remakes`) manquant sur staging ; Ilies l'a
    appliquée, après quoi **tout `order_life_cycle` est vert** contre staging,
    `TestOrderLifeCycleRepository_FreezeOrderVAT_Postgres` compris (taux
    produit renvoyé pour une commande ouverte, taux figé pour une commande
    close) et `TestOrderLifeCycleRepository_Postgres` (création / modification
    de commande passant par `freezeOrderVAT`) ;
  - échecs préexistants sans lien avec ce chantier : ligne de frais de
    livraison à 0 € quand `tva_id = -1` existe (`TestPOSAccountingReports_Postgres`,
    deux assertions, neutralisées le temps d'un run pour vérifier le reste) ;
    `analytics/TestGetRevenueTotalsThreePeriods_Postgres` attend un HT de
    833 × 2 pour une période à 1 500 € TTC (1 000 + 500), alors que le bon HT à
    20 % est 1 250 — attente du test erronée ; `TestDiscountTimeRestriction_Postgres`
    et `TestOrdersRepository_Postgres` (schéma des remises) ; tests sqlmock
    (`order_life_cycle`, `orders`, `stats`) qui échouent seulement quand
    `DB_DIALECT=postgres` est exporté dans le même run — verts en tests
    unitaires normaux.
- Non commité, non déployé.
