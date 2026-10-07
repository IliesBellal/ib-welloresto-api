# Export comptable : modes de clôture, canaux, remises

Chantier démarré le 2026-10-04, après le chantier préalable « TVA figée sur
chaque ligne » (docs/TVA_FIGEE_LIGNES_COMMANDE.md) et la correction des
commandes refusées (docs/decisions.md, « Rapport comptable — commandes
refusées (DENIED) exclues de la TVA »).

## Le besoin

L'export comptable filtrait en dur certaines commandes (ScanNOrder, Uber Eats,
Deliveroo) et certains moyens de paiement. Ilies veut à la place un filtre par
canal de commande (caisse, borne, ScanNOrder, Uber Eats, Deliveroo), sans
risque, avec deux exigences :

- **Rien d'anormal ne doit être visible pour le comptable** : aucun
  avertissement sur le PDF ; le total TTC du tableau TVA doit être égal au
  total des encaissements.
- **Suivre la règle fiscale tout en laissant le plus de liberté aux
  restaurateurs.**

Constat de départ : beaucoup de restaurateurs trouvent la clôture de caisse
compliquée, d'autres recopient le théorique dans le réel. Le réel (relevé de
caisse et de TPE saisi à la clôture, `cash_registers_custom_items`) ne peut
par construction jamais égaler exactement la TVA calculée (écart de caisse,
texte libre, aucun lien avec les commandes).

## Décisions (Ilies, 2026-10-04)

### Deux modes de clôture

| | Manuel | Automatique |
|---|---|---|
| Qui | établissements existants | nouveaux établissements (défaut) |
| Fermeture du registre | inchangée : fermeture, relevé de caisse et de TPE (custom items), validation | le restaurateur appuie toujours sur « fermer », sans relevé : fermeture et validation en une fois |
| Export comptable | **inchangé** : TVA calculée sur les lignes, encaissements = réel des registres validés, non configurable | TVA ventilée à partir des paiements, encaissements = paiements, **filtrable par canal** (tous par défaut) |

- Le mode est changé **uniquement par l'équipe WelloResto**, jamais par
  l'établissement, et **planifié pour un début de mois** (entre le dernier
  registre du mois précédent et le premier du nouveau mois).
- **Table d'historique** des modes (établissement, mode, date d'effet), plutôt
  qu'un couple de dates « manuel du / au » : elle permet de régénérer
  correctement un ancien rapport quel que soit le nombre de changements.
  Dates (pas des horodatages) dans le fuseau de l'établissement ; pas de
  modification rétroactive ; un établissement sans historique est en
  automatique ; la migration inscrit « manuel depuis toujours » pour chaque
  établissement existant (à partir de `merchant`, pas de
  `merchant_parameters`, pour n'en oublier aucun).
- **Le mode est inscrit sur chaque registre à son ouverture**, d'après sa date
  d'ouverture : un registre ouvert le 31 au soir et fermé le 1er garde le mode
  du 31.
- Le fond de caisse reste saisi à chaque ouverture, dans les deux modes.

### Export en mode automatique (« B »)

- Chaque paiement est réparti entre les taux de TVA de sa commande, au prorata
  du TTC de ses lignes (frais de livraison compris, à leur propre taux).
  Total TTC de la TVA = total des encaissements, par construction, quels que
  soient la période et les canaux.
- Référence de date : **date de création de la commande** (comme le mode
  manuel) — évite qu'une commande soit comptée deux fois ou perdue au
  changement de mode, et convient aux établissements peu assidus sur leurs
  registres.
- Filtre par canal sur `orders.order_source` (`WELLO_RESTO_POS`, `KIOSK`,
  `SCANNORDER`, `UBER_EATS`, `DELIVEROO`), tous cochés par défaut. Refusé en
  mode manuel.
- Arrondis : méthode du plus grand reste pour que la somme tombe au centime.

### Remises de caisse

Les remises saisies comme moyen de paiement (`CURRENCY` « Réduction
montant », `PERCENTAGE` « Réduction pourcentage », `DISCOUNT`) ne sont pas de
l'argent encaissé, et la TVA est due sur le prix réellement payé (CGI art.
267). Dans les deux modes :

- la remise est déduite de la base TVA, répartie entre les taux de la commande
  au prorata du TTC de chaque taux (plus grand reste pour les centimes,
  plafonnée au total de la commande) ;
- elle n'apparaît pas dans les encaissements (en manuel : lignes de réel
  reconnues par code, libellé FR ou libellé de l'app de caisse
  « Réduction monta. », sans tenir compte de la casse) ;
- une ligne d'information sous le tableau TVA, hors de tout total :
  « CA TTC avant remises · Remises accordées · CA TTC ».

Les remises produit / codes promo (`discount_id`, `discount_redemptions`)
sont déjà dans le prix net de la ligne : rien à faire.

### Autres règles

- Un export dont la période chevauche un changement de mode est refusé.
- Cas limite accepté et documenté : au mois du changement, une commande servie
  après minuit dans une caisse ouverte la veille peut apparaître dans le réel
  d'un mois et dans les paiements de l'autre (encaissements uniquement).
- Archivage : chaque PDF est conservé sous un nom unique horodaté, dans le
  bucket **privé**, référencé dans une table des exports, téléchargé par lien
  signé. Les fichiers déjà publiés ne sont ni supprimés ni déplacés.

## Phases

1. **Modes de clôture** : migration (historique + mode sur le registre),
   résolution du mode, inscription à l'ouverture, validation automatique à la
   fermeture, exposition du mode aux clients, endpoints internes de
   planification.
2. **Export comptable** : mode de la période, refus des périodes à cheval,
   correction des remises (manuel), ventilation par les paiements et filtre
   par canal (automatique).
3. **Archivage des exports** : bucket privé, table des exports, lien signé.
4. **Registre de caisse** : remises hors encaissements et TVA nette.

## Journal

### Phase 1 — Modes de clôture (2026-10-04)

- **Migration 165** (`migrations/todo/165_cash_register_closing_modes`) :
  table `merchant_closing_modes` (établissement, mode, date d'effet, auteur),
  colonne `cash_registers.closing_mode`, ligne « MANUAL depuis 1970-01-01 »
  pour chaque établissement de `merchant`, registres existants marqués MANUAL.
- **`cash_registers/closing_mode.go`** : constantes, résolution du mode à une
  date (ligne de date d'effet la plus récente, sinon AUTO), historique,
  règles de planification.
- **Ouverture** : le mode en vigueur à la date du jour (calendrier de
  l'établissement) est inscrit sur le registre et renvoyé (`closing_mode`).
  Si l'établissement n'existe pas dans `merchant`, repli sur Europe/Paris
  plutôt que de bloquer l'ouverture d'une caisse (cas d'un test existant).
- **Fermeture** : en AUTO, la validation (`enclose`) suit immédiatement, dans
  le service et non dans le dépôt — les tests et chemins qui ferment au niveau
  dépôt gardent le comportement historique. Rejouée si le registre est déjà
  fermé mais pas validé (appel répété après un échec). La réponse porte
  désormais `closing_mode` et `enclosed`. Le résumé et l'historique des
  registres exposent aussi `closing_mode`.
- **Endpoints internes** (`/admin`, `users.is_platform_staff`) :
  `GET|POST /admin/merchants/{id}/cash-register-closing-modes`,
  `DELETE /admin/merchants/{id}/cash-register-closing-modes/{YYYY-MM-DD}`.
  Règles : mode MANUAL ou AUTO ; date au 1er d'un mois, au plus tôt le mois
  suivant (calendrier de l'établissement) ; refus si ce mode est déjà en
  vigueur à cette date, ou si un changement y est déjà planifié ; annulation
  seulement d'un changement pas encore en vigueur. Réponses d'erreur
  explicites (codes `closing_mode_*`), pas les messages génériques.
- La règle « écart proche de 0 » de la validation manuelle est appliquée par
  l'app, pas par l'API (constaté, inchangé).

### Phase 2 — Export comptable (2026-10-04)

- **Requêtes communes** : `GetOrderVATLines` (parts TTC par commande et par
  taux, frais de livraison non nuls inclus, catégories masquées signalées) et
  `GetOrderPayments` (paiements actifs par commande, libellé FR ou code brut).
  Périmètre `orderScope` : historique en manuel (WELLO_RESTO hors ScanNOrder),
  `order_source = ANY(...)` en automatique.
- **Calcul sans base** (`vat_breakdown.go`, testé unitairement) : répartition
  au plus grand reste, `buildManualVAT` (remises déduites au prorata, plafonnées
  au total de la commande, réparties sur toutes les parts y compris les
  catégories masquées pour ne pas reporter leur remise sur les catégories
  affichées), `buildAutoReport` (paiements ventilés, égalité par
  construction).
- **Mode de la période** (`periodClosingMode`) : mode au premier jour ; refus
  si un changement vers un autre mode prend effet dans la période (message :
  générer un export avant et un à partir de la date).
- **Canaux** : valeurs de `orders.order_source` ; vide ou tous cochés = aucun
  filtre (y compris commandes sans canal connu) ; valeur inconnue refusée ;
  filtre refusé en clôture manuelle.
- **Décisions d'implémentation** :
  - en AUTO, **toutes les catégories apparaissent**, y compris celles masquées
    d'ordinaire (« TVA Undefined » des produits sans TVA configurée) : sinon
    l'argent de ces lignes ne tomberait dans aucun taux et l'égalité serait
    rompue. Une telle ligne signale un produit mal paramétré ;
  - en AUTO, une commande payée sans aucune part TTC positive (aucune ligne)
    est exclue des deux tableaux et journalisée (`paid orders without VAT
    lines excluded`) ;
  - les frais de livraison nuls ne produisent plus de ligne à 0 € (y compris
    via `GetTVAData`, qui délègue désormais à `GetOrderVATLines`) — ce qui
    règle au passage l'échec préexistant de `TestPOSAccountingReports_Postgres` ;
  - en manuel, les remises sont aussi retirées du réel (codes, libellés FR,
    libellé de l'app de caisse).
- PDF : ligne d'information des remises sous le tableau TVA (absente sans
  remise) ; message « aucun encaissement » propre à chaque mode.

### Phase 3 — Archivage des exports (2026-10-04)

- **Migration 166** (`migrations/todo/166_accounting_exports`) : table
  `accounting_exports` (période, mode, canaux, nom, clé R2, sha256, taille,
  auteur, date).
- Chaque export est déposé dans le **bucket privé** sous
  `WR_rapport_comptable_<du>_<au>[_<canaux>]_<horodatage UTC>.pdf`, enregistré
  avec son empreinte, et renvoyé avec `export_id` et un **lien signé valable
  une heure** (`download_url` n'est donc plus une URL permanente).
- `GET /pos/accounting/exports` (200 derniers) et
  `GET /pos/accounting/exports/{export_id}/download` (nouveau lien signé,
  limité à l'établissement de l'utilisateur).

### Déclaration de TVA alignée sur l'export (2026-10-05)

`/accounting/vat/calculate` et `/accounting/vat/export-csv` (page « Déclaration
de TVA » du back-office) calculaient la TVA avant remises de caisse, avec leur
propre découpage par canal (la borne rangée dans « restaurant ») et des bornes
de dates en UTC. Ils suivent désormais l'export comptable
(`pos/accounting/vat_declaration.go`) :
- **même méthode, tranche par tranche selon le mode de clôture** : la période
  est découpée par mois (et, par prudence, aux dates de changement de mode) ;
  MANUAL → TVA sur les lignes, remises déduites, catégories affichées ; AUTO →
  ventilation des encaissements, toutes catégories. Chaque mois de
  `monthly_breakdown` porte son `closing_mode` ;
- **mêmes canaux que l'export** (`orders.order_source`), tous déclarables ;
  anciennes valeurs acceptées (`restaurant` = caisse + borne, `scannorder`,
  `ubereats`, `deliveroo`) ; `by_channel` est désormais indexé par
  `order_source` ; tous les canaux (ou tous les types de commande) cochés =
  aucun filtre, commandes sans canal / à type atypique comprises ;
- **dates de calendrier dans le fuseau de l'établissement** (bornes incluses),
  comme l'export — avant, une commande du 1er à 0 h 30 heure de Paris tombait
  dans le mois précédent ;
- **CSV en euros** : `formatCSVAmount` écrivait les centimes comme des euros
  (12,34 € → « 1234.00 ») ;
- HT arrondi par (mois, canal, type, taux), TVA = TTC − HT.

Calculs partagés avec l'export : `vat_breakdown.go` est découpé en parts
nettes par commande (`manualNetShares`, `autoNetShares`) puis agrégation (par
catégorie pour l'export, par mois / canal / type / taux pour la déclaration) ;
comportement de l'export inchangé (tests unitaires inchangés, verts).
`GetVATAggregationRows` et ses filtres SQL sont supprimés.

Tests : `vat_declaration_test.go` (canaux et anciennes valeurs, découpage,
agrégation, CSV en euros) ; `TestAccountingAutoMode_Postgres` (`CalculateVAT`
de bout en bout en AUTO : TVA 3,59 €, caisse 3,14 €, borne 0,45 € ; valeur
`restaurant` ; filtre Uber Eats ; canal inconnu refusé ; mois à cheval sur un
changement de mode) ; `TestPOSAccountingReports_Postgres` (déclaration en
MANUAL, DENIED exclues, TVA figée), verts contre staging. Back-office : page
TVA alignée (cf. `wello-back-office/docs/cloture-caisse-export-comptable.md`).

### Alignement du back-office (2026-10-05)

- **Nouvel endpoint** `GET /pos/accounting/export-options?date_from=YYYY-MM-DD`
  (`AccountingService.ExportOptions`) : mode de clôture de l'établissement à
  cette date (aujourd'hui si absente) et canaux filtrables, avec leurs
  libellés (vides en clôture manuelle). Nécessaire pour qu'un client sache s'il
  doit proposer le filtre par canal. Couvert par
  `TestAccountingAutoMode_Postgres` (AUTO → 5 canaux ; MANUAL → aucun ; mode
  selon la date ; date invalide refusée).
- **Back-office** (`wello-back-office/docs/cloture-caisse-export-comptable.md`) :
  fenêtre d'export comptable (mode, canaux en automatique, refus de l'API
  affichés, liste des exports archivés et retéléchargement) ; fenêtre de
  clôture en mode automatique (encaissements seuls) ; remises hors
  encaissements et en information ; détail TVA brut / remises / net ; mode de
  clôture dans la fiche registre. Au passage : un refus de l'export
  (HTTP 200, `status: "0"`) ouvrait jusqu'ici un onglet vide.

### Phase 4 — Registre de caisse, présentation Square / Lightspeed (2026-10-05)

Ilies a demandé de suivre les grands acteurs (recherche : Lightspeed, Square,
SumUp, guides ticket Z français) : une remise n'est jamais un moyen de
paiement ; rapport en « ventes brutes − remises = ventes nettes », TVA sur le
net ; moyens de paiement listés sans les remises. Application alignée en même
temps (wello_resto_flutter, `docs/decisions.md`, addendum 2026-10-05).

**API.**
- **Ventilation TVA du registre** (`cash_registers/register_vat.go`,
  `computeRegisterVAT`) : remplace la requête SQL héritée de la procédure
  `GET_CASH_REGISTER_REPORT`. Remises déduites au prorata des taux de chaque
  commande (même règle que l'export : plus grand reste, plafond, répartition
  sur toutes les parts). Deux corrections au passage, décidées car le rapport
  Z doit être juste : **les suppléments (`extra`) sont comptés** (l'ancienne
  requête les oubliait, l'export comptable les comptait) et **le HT des frais
  de livraison** vaut `TTC × 100 / (100 + taux)` (300 € à 20 % : 250 € au lieu
  de 240 €). HT arrondi par taux réellement appliqué, TVA = TTC − HT.
- Rapport et détail TVA du registre : nouveaux champs `gross_ttc` et
  `discounts` (`TTC` = net) ; le détail TVA ne liste plus les remises parmi
  les moyens de paiement.
- **Instantané figé inchangé** : `cash_registers_items` garde les remises,
  parce que le contrôle de dérive de l'export comptable
  (`GetTrustedEnclosedRegisterIDs`) le compare à un recalcul en direct qui les
  compte aussi. Les remises sont écartées **à la présentation** :
  - résumé : hors `items` (théorique), hors `custom_items` (une remise recopiée
    dans un relevé avant la phase 4 n'est pas de l'argent compté — l'écart
    reste le même puisqu'elle sort des deux côtés), hors `payments` ; total à
    part dans `discounts` ;
  - historique : hors `payment_methods` et hors `total_revenu` ;
  - PDF du registre : ligne d'information brut / remises / net sous la TVA.
- Briques partagées avec l'export : `models.IsDiscountMOP`,
  `models.IsDiscountPaymentLabel`, `models.DiscountMOPsSQL`,
  `helpers.AllocateLargestRemainder`.

**Application de caisse.**
- Remises hors théorique, relevé, écart et regroupement par utilisateur ;
  filtrées aussi côté app (code ou libellé), pour le cas d'une API pas encore
  à jour. « Remises accordées » affichées pour information sous les totaux de
  clôture ; détail TVA (écran et impression) : ventes brutes, remises, TTC net.
- Clôture automatique : la réponse de fermeture (`closing_mode`, `enclosed`)
  est lue ; en AUTO le résumé n'affiche que les encaissements (ni réel, ni
  écart, ni bouton de validation). Manuel inchangé. Ancienne API = MANUAL.
- Hors périmètre, gardé tel quel : comptage limité aux espèces (Square /
  Lightspeed ne recomptent que le tiroir) — le relevé manuel reste complet,
  comme décidé pour le mode manuel.

**Tests.** API : `TestBuildRegisterVATReport*` (unitaires) ;
`TestGetCashRegisterReport_Postgres` (supplément, remise, frais à la bonne
formule, instantané avec remise, détail TVA sans remise) et
`TestCashRegisterLifecycle_Postgres` (résumé sans remise, total à part,
instantané inchangé), verts contre staging avec tous les tests `cash_registers`,
`pos`, `pos/accounting`. App : `cash_register_closing_test.dart` (7 tests) et
suite complète (31 tests) verts ; `dart analyze` sans erreur sur les fichiers
touchés (6 remarques préexistantes, hors des lignes modifiées).

### Appartenance des registres (2026-10-04, à la demande d'Ilies)

Constat, plus large que signalé au départ : sur 8 endpoints de registre, 3
contrôlaient l'établissement (historique, détail TVA, PDF du registre) ; 5 non :
- **fermeture** : la vérification « déjà fermé pour cet établissement »
  répondait « non » pour un registre d'un autre établissement, et la fermeture
  se poursuivait — registre d'autrui fermé, et paiements ScanNOrder / Uber
  Eats / Deliveroo / borne de l'appelant rattachés à ce registre ;
- **validation**, **résumé** (lecture des commandes et paiements d'autrui),
  **ajout / suppression d'une ligne de relevé** — un ajout entrait dans le réel
  de l'export comptable de l'autre établissement.

Correction, sans effet sur une clôture légitime (le registre est ouvert avec
l'établissement de la session, et c'est la même session qui le ferme) :
- `CashRegisterBelongsToMerchant` : le registre appartient à l'établissement
  si le registre **ou sa caisse** (`cash_desks`, critère déjà utilisé par
  l'historique) lui est rattaché ; un identifiant non numérique n'appartient à
  personne ;
- **fermeture d'un registre étranger ou inexistant** : no-op, réponse « déjà
  fermé, validé » (avec le mode du jour de l'établissement de la session) —
  l'app poursuit normalement, par exemple avec un identifiant de registre
  resté en mémoire d'une session précédente ; rien n'est lu ni modifié, un
  avertissement est journalisé. Garde aussi au niveau dépôt
  (`isCashRegisterClosedForMerchant` répond « déjà fermé »).
- **validation, résumé, relevé** sur un registre étranger : 404 « introuvable »,
  sans rien révéler.
- Pas de changement d'établissement en cours de session dans ce dépôt (le
  multi-comptes concerne les boutiques Uber Eats / Deliveroo) : changer
  d'établissement passe par une nouvelle connexion.

Test : `TestCashRegisterOwnership_Postgres` (B ne ferme pas, ne valide pas, ne
lit pas, ne modifie pas le relevé du registre de A ; le paiement en attente de
B n'est pas rattaché ; parcours manuel de A inchangé ; registre inexistant et
identifiant invalide). En attente de la migration 165 sur staging (l'ouverture
de registre écrit `closing_mode`).

### Constats hors périmètre
- Les exports CSV du rapport journalier (`pos/reports`) sont toujours déposés
  en lecture publique à une adresse prévisible.
- Tests unitaires du module planning (`TestServiceDeleteEmployee*`,
  `TestServiceListCurrentUserLeaveRequests*`) en échec, sans lien.

### Côté applications (hors de ce dépôt)

- **Caisse (Flutter)** : en AUTO (`closing_mode` à l'ouverture, ou `enclosed`
  à la fermeture), ne plus afficher l'écran de relevé de caisse et de TPE.
  **À livrer avant tout nouvel établissement**, puisqu'ils sont en AUTO par
  défaut — sinon le relevé serait proposé puis refusé par l'API (registre
  déjà validé).
- **Export comptable (app / back-office)** : proposer le choix des canaux en
  AUTO uniquement ; ne plus conserver `download_url` (lien d'une heure) mais
  passer par la liste des exports et le téléchargement.

### Statut (2026-10-05)

- Migrations 165 et 166 appliquées par Ilies sur staging (44 établissements,
  44 lignes d'historique MANUAL, aucun registre sans mode). Requête de
  contrôle en prod « registres rattachés à aucun établissement » : 0.
- Tests d'intégration contre staging, **tous verts** : `cash_registers`
  (`TestCashRegisterClosingModes_Postgres`, `TestCashRegisterOwnership_Postgres`,
  cycle de vie, rapport), `pos/accounting` (`TestAccountingAutoMode_Postgres`,
  `TestExportAccountingReport_Postgres` avec archivage,
  `TestGetRealPaymentsData_Postgres`, `TestPOSAccountingReports_Postgres`),
  `pos`, `order_life_cycle`.
- `TestPOSAccountingReports_Postgres`, en échec depuis longtemps sur la ligne
  de frais de livraison à 0 €, passe désormais : l'export l'avait déjà
  supprimée (phase 2), le rapport journalier (`pos/reports.GetTVAReportData`)
  est aligné (`delivery_fees <> 0` dans la branche frais) — sans effet sur les
  montants.
- Phase 4 (registre de caisse) non commencée, en attente d'accord. Non
  commité, non déployé.
