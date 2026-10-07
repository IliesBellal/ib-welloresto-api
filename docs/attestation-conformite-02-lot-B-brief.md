# PROMPT — Conformité caisse, lot B : clôtures fiscales et scellement des commandes

**Date :** 2026-10-07
**Références :**
- [feuille de route](attestation-conformite-feuille-de-route.md), décisions S1, S3, S5, S6 et S7 ;
- [audit](attestation-conformite-00-audit.md) : constats C6 et C9 ;
- [lot A](attestation-conformite-01-lot-A-brief.md) : socle `internal/fiscal` (`LockChain`, `Seal`, `Canonical`, charges utiles), commité ;
- BOI-TVA-DECLA-30-10-30 :
  - §170 : clôtures journalière, mensuelle et annuelle « cumulatives et impératives », avec des données cumulatives et récapitulatives intègres et inaltérables, le cumul du grand total de la période et le total perpétuel, jamais purgés ;
  - §100 : preuve que la donnée n'a pas été modifiée depuis son enregistrement.

**Objectif :**
1. Le logiciel produit lui-même, sans action du restaurateur, des clôtures journalières, mensuelles et annuelles scellées.
2. La clôture journalière scelle aussi chaque commande de la journée, ce qui remplace la chaîne par commande du lot A.
3. Tickets et avoirs portent leur TVA ventilée, pour que les totaux soient justes.

## Règle d'or

Elle est reprise du lot A :
1. **Aucune régression de performance sur les écritures** (encaissement, clôture de commande, ticket). Elles doivent même s'alléger, puisque le scellement par commande disparaît. Mesure avec `TestFiscalChainPerf_Postgres`.
2. **Aucune donnée existante modifiée ni recalculée.** Les lignes `orders` scellées en v1 ou v2 restent telles quelles.
3. **Aucun changement visible pour le restaurateur** : registres, Z, export comptable et rapports inchangés.
4. **Migrations écrites, jamais appliquées.**
5. **Une phase, un point d'arrêt** ; phase 0 rendue avant tout code.
6. **Commits atomiques**, hunks du lot uniquement, sur accord d'Ilies.

## Décisions (Ilies, 2026-10-07)

- **S1 : une ligne scellée par établissement et par jour.** Elle contient les totaux de la journée **et l'empreinte de chaque commande clôturée ce jour-là** (en-tête et lignes, comme `fiscal.LoadOrderClosure`).
  - Les clôtures de commande (caisse, annulation, refus, plateformes) n'écrivent plus d'empreinte sur `orders`.
  - Tickets, avoirs, paiements, registres et journal d'audit restent scellés au moment où ils sont écrits.
- **S3 :**
  - clôtures mensuelle et annuelle **calculées à partir des journalières**, sans relire les ventes ;
  - grand total de la période (depuis le 1er janvier) et total perpétuel tenus en cumul.
- **S5 : jour calendaire dans le fuseau de l'établissement** (`merchant.timezone`). Un passage horaire clôt la veille dès 3 h, heure locale, et rattrape les jours manqués dans l'ordre.
- **S6 :**
  - une commande rouverte au moment du passage n'est pas scellée : elle le sera le jour de sa reclôture ;
  - une commande scellée n'est plus rouvrable. Le refus lui-même est codé au lot C ; ce lot fournit le test « commande scellée ».
- **S7 : le rattachement des paiements aux registres ne change pas.** Les clôtures fiscales sont indépendantes des registres.

## Phase 0 — Recensement (rendu avant tout code)

**1. Contenu d'une journée.**
- Rattacher une commande à son jour : sa date de clôture (`delivered_on`) dans le fuseau de l'établissement, et `state = 'CLOSED'`. Le confirmer pour tous les chemins de clôture, y compris les commandes sans `delivered_on` (1 438 à staging, toutes antérieures au lot A).
- Origine des totaux, à proposer :
  - **chiffre d'affaires :** tickets et avoirs de la journée (`receipts`), qui sont les justificatifs ;
  - **paiements par moyen de paiement :** paiements de la journée ;
  - **ventilation par canal :** `orders.order_source`.
- Écarts attendus avec l'export comptable et les rapports, déjà documentés : périmètre de canaux, date de référence, mode manuel. Ils sont à rappeler, pas à corriger.

**2. Grand total et total perpétuel.**
- Le total perpétuel compte le chiffre d'affaires depuis le début de l'utilisation du logiciel. Proposition : la première clôture d'un établissement prend comme **valeur d'ouverture la somme de tous ses tickets antérieurs**, une seule fois, et le documente.
- Le grand total de la période repart à zéro le 1er janvier : année civile, faute d'exercice connu par établissement.

**3. TVA des tickets (C9).**
- Aujourd'hui :
  - `tax_details` vaut `{}` sur tous les tickets ;
  - le détail par article (`items_snapshot`) porte le taux, sans HT par ligne ;
  - l'avoir porte une seule ligne à TVA 0, avec HT = TTC ;
  - les anciens tickets portent le montant de TVA dans `TaxRate` (voir `receipt/builder.go`).
- À proposer : remplir `tax_details` par taux pour tout nouveau ticket, et ventiler l'avoir au prorata des taux du ticket d'origine (plus grand reste au centime).

**4. Retrait du scellement par commande.**
- Recenser tout ce que le lot A a ajouté et qui disparaît :
  - `fiscal.SealOrderClosure` et `SealColumns` ;
  - les appels dans `SetDeliveredLocal`, `DenyOrderLocal`, `DeleteOrderLocal` et les clôtures Uber Eats ;
  - le verrou de la chaîne `orders` ;
  - l'index `idx_orders_fiscal_chain_head`.
- Garder ce qui sert encore :
  - `LoadOrderClosure` (empreinte de chaque commande) ;
  - `LockOpenOrdersByBrandOrderID` ;
  - les transactions de la solution C ;
  - le ticket des ventes plateformes.

**5. Le passage de nuit.**
- La tâche tourne sur **chaque instance de l'API** (`cmd/api/tasks.go`, robfig/cron, `SkipIfStillRunning` par instance seulement). Elle doit donc être idempotente : contrainte d'unicité, verrou de la chaîne des clôtures, contrôle « déjà clos ».
- Volumes à prévoir : commandes par jour pour l'établissement le plus actif ; durée d'une clôture journalière.

**6. Test « commande scellée »** pour le lot C : une commande est scellée si une clôture journalière existe pour sa date de clôture. Proposer la requête et l'index.

**Livrable :** recensement, chiffres, propositions sur les points 1 à 3. **Point d'arrêt.**

## Phase 1 — Migrations (fichiers seulement)

- **`fiscal_closures`, en ajout seul**, une seule table pour les trois périodes :
  - `merchant_id`, `period_type` (`DAY`, `MONTH`, `YEAR`), `period_start`, `period_end`, `timezone`, `closed_at` ;
  - totaux TTC / HT / TVA (ventes, avoirs, net), ventilations en jsonb (taux de TVA, moyens de paiement, canaux), nombre et bornes des numéros de ticket, nombre de commandes ;
  - pour `DAY`, la **liste des empreintes de commandes** ;
  - `grand_total_period`, `perpetual_total` ;
  - `previous_hash`, `hash`, `signature`, `hash_version`.
- **Contraintes et index :**
  - unicité `(merchant_id, period_type, period_start)` ;
  - index de tête de chaîne `(merchant_id, closed_at DESC, id DESC)` ;
  - le cas échéant, l'index du test « commande scellée ».
- **Selon la phase 0 :** suppression de `idx_orders_fiscal_chain_head`, devenu inutile.

**Point d'arrêt : Ilies applique sur staging.**

## Phase 2 — TVA des tickets (C9)

- `tax_details` rempli par taux sur chaque nouveau ticket.
- Avoir ventilé au prorata des taux du ticket d'origine, au centime près (plus grand reste).
- Les tickets existants ne sont pas touchés. Le calcul des totaux lit le détail des anciens tickets selon leur forme historique.

## Phase 3 — Clôtures fiscales

- **Nouvelle chaîne `fiscal_closures`** dans `internal/fiscal` : charge utile, `LockChain`, scellement. **Son ordre dans la liste des chaînes est à documenter dans `lock.go`.**
- **Calcul d'une journée en lots** : une requête pour les commandes et leurs lignes, une pour les tickets, une pour les paiements ; empreintes calculées en Go.
- **Mois et année :** clôturés au passage qui clôt leur dernier jour, à partir des journées scellées.
- **Tâche horaire (`@hourly`)** : pour chaque établissement dont il est au moins 3 h, heure locale, clôture des jours non clos jusqu'à la veille incluse, dans l'ordre, une transaction par clôture.
- **Rattrapage :** un établissement arrêté plusieurs jours est rattrapé jour par jour.

## Phase 4 — Retrait du scellement par commande

- Clôtures caisse et plateformes allégées : plus de verrou ni d'empreinte `orders`.
- Le ticket fiscal des ventes plateformes et le contrôle « entièrement payée » restent.
- Tests du lot A adaptés : les clôtures de commande n'écrivent plus `hash` ; les chaînes payments, receipts, audit et registres sont inchangées.

## Tests attendus

- **Clôture journalière re-scellée à l'identique depuis la base**, empreintes de commandes comprises ; une commande modifiée après la clôture est détectée et identifiée.
- **Idempotence :** deux passages simultanés (deux instances) → une seule clôture par jour.
- **Calendrier :**
  - rattrapage de plusieurs jours ;
  - passage de mois et d'année ;
  - fuseau horaire ;
  - aucune clôture avant 3 h locale.
- **Cumuls :**
  - total perpétuel jamais remis à zéro, valeur d'ouverture comptée une seule fois ;
  - grand total remis à zéro au 1er janvier ;
  - mois et année égaux à la somme de leurs journées.
- **Commande rouverte au passage :** absente de la journée, présente le jour de sa reclôture.
- **C9 :** ticket et avoir ventilés par taux, somme exacte au centime.
- **Écritures :** mesure avant / après ; les clôtures de commande sont plus rapides qu'au lot A.
- **Non-régression :** suites d'intégration des paquets touchés et tests du lot A adaptés.

## Livrables

- Code et migrations (non appliquées).
- Tests et mesures, y compris la durée d'une clôture journalière sur l'établissement le plus actif de staging.
- Une entrée dans `decisions.md`.
- Le journal du lot.
- Commits sur accord.
- **Mise en production des lots A et B ensemble** (voir la feuille de route).

## Journal

### Phase 0 — Recensement (2026-10-07)

**Décisions d'Ilies prises avant la phase 0 :**
- valeur d'ouverture du total perpétuel (point 2) : OK, sous réserve de la source, voir plus bas ;
- origine des totaux (point 1) : OK ;
- suppression de `idx_orders_fiscal_chain_head` : OK ;
- **mise en production unique** après le lot F ;
- **versions actuelles des caisses compatibles** (voir la feuille de route).

#### 1. Contenu d'une journée

- **Rattachement :** une commande appartient au jour local de son `delivered_on`, si `state = 'CLOSED'`.
  - Tous les chemins de clôture écrivent `delivered_on` depuis le lot A : caisse, annulation, refus, tâche planifiée, réconciliation et webhooks Uber. Les 1 438 commandes closes sans `delivered_on` sont toutes anciennes : aucune sur les 30 derniers jours.
  - `state = 'DONE'` : 1 commande ancienne, aucun code ne l'écrit.
  - **Point d'attention pour la phase 4 :** en retirant `fiscal.SealColumns`, les clôtures Uber doivent **garder** `state = 'CLOSED', delivered_on = <date de clôture>`.
- **Totaux proposés (validés) :**
  - **chiffre d'affaires :** tickets de la journée (`receipts.created_at` dans le jour local). Ventes (TTC ≥ 0), avoirs (TTC < 0) et net, en TTC, HT et TVA par taux. La TVA par taux est lue dans `tax_details`, rempli à partir de ce lot (C9) ;
  - **paiements :** paiements de la journée par moyen de paiement et type (vente / remboursement). Les remises de caisse (`models.IsDiscountMOP`) sont isolées, puisque ce n'est pas de l'argent. Les paiements désactivés sont comptés à part, pour la trace ;
  - **canaux :** `orders.order_source`, renseigné partout sauf sur 3 commandes : `WELLO_RESTO_POS`, `UBER_EATS`, `SCANNORDER`, `DELIVEROO`, `KIOSK` ;
  - **continuité :** nombre de tickets, premier et dernier numéro de la journée ;
  - **commandes :** pour chaque commande close ce jour-là, `order_id`, nature à la clôture (vente / annulation / refus / échec, copiée de `brand_status` au moment du scellement) et empreinte de `fiscal.LoadOrderClosure`. Le statut reste hors de l'empreinte de la commande : les plateformes le réécrivent après la clôture.
- **Volumes (staging) :**
  - 44 établissements, dont 31 actifs ;
  - journée la plus chargée : 181 commandes et 265 lignes. La clôture journalière, faite en trois requêtes groupées, se compte en fractions de seconde.

#### 2. Total perpétuel : la source change le résultat

- **Les tickets n'existent que depuis le 2026-03-21** (5 870 au total). Les commandes remontent à 2023.
- Le BOI demande un total perpétuel « depuis le début de l'utilisation du logiciel », qui continue à travers les changements de version.
- **Deux sources possibles pour la valeur d'ouverture**, comptée une seule fois à la première clôture :
  - **(a) somme des tickets antérieurs :** simple et adossée aux justificatifs, mais ignore environ 2,5 ans de ventes ;
  - **(b) somme des ventes closes depuis la première commande** (`orders.price`, hors annulées, refusées, supprimées) : couvre toute l'utilisation, mais les commandes antérieures aux tickets n'ont pas de justificatif chaîné.
- **Recommandation : (b)**, avec la méthode et la date de reprise écrites dans la clôture d'ouverture. Elle correspond mieux à « depuis le début de l'utilisation ». Le grand total de l'année en cours suit la même source.

#### 3. TVA des tickets (C9)

- `tax_details = '{}'` sur les 5 870 tickets.
- **Proposition :** au moment de générer le ticket, ventiler par taux avec la logique de l'export comptable (`pos/accounting/vat_breakdown.go`) :
  - taux figés des lignes ;
  - frais de livraison à leur taux ;
  - remises de caisse déduites au prorata, au plus grand reste.
  
  Cette logique est extraite dans une fonction commune plutôt que dupliquée. L'avoir est ventilé au prorata du ticket d'origine.
- **Dépendance :** cette logique, la migration 164 (TVA figée, appliquée sur staging) et `register_vat.go` font partie du **travail d'Ilies non commité**, comme les migrations 165 à 167 et leur code.

#### 4. Retrait du scellement par commande

À retirer :
- `fiscal.SealOrderClosure`, `fiscal.SealColumns` et `OrderClosureSeal` ;
- `sealOrderClosure` et ses 3 appels dans `order_life_cycle/repository.go` ;
- `closePlatformOrder` dans `ubereats/repository.go` (scellement seulement) ;
- `CancelOrder` et `MarkFailed` dans `webhook/ubereats/repository/orders_repo.go` ;
- l'index `idx_orders_fiscal_chain_head`.

À garder :
- `LoadOrderClosure` (et sa variante verrouillée), `LockOpenOrdersByBrandOrderID`, `OrderFullyPaid` ;
- les transactions de la solution C ;
- le ticket des ventes plateformes ;
- la clôture idempotente de `SetDeliveredExternal`.

**Compatibilité des caisses :** `orders.hash` n'est exposé par aucune réponse d'API (aucun champ JSON dans `internal/models`), et les tickets ne le sont pas non plus. Aucun impact.

#### 5. Passage de nuit

- La tâche est ajoutée dans `cmd/api/tasks.go` (`@hourly`, robfig/cron, `SkipIfStillRunning` limité à une instance). Elle doit être idempotente : contrainte d'unicité, `LockChain(fiscal_closures)` et contrôle « déjà clos » sous le verrou.
- **Proposition :**
  - clôturer **chaque jour, y compris sans activité**, pour tout établissement actif. La clôture vaut alors zéro et garde la continuité ;
  - commencer au **jour de la mise en production** ;
  - un établissement réactivé est rattrapé jour par jour.

#### 6. Test « commande scellée » (pour le lot C)

- Une commande est scellée s'il existe une clôture `DAY` de l'établissement pour la date locale de son `delivered_on`.
- C'est une lecture sur l'index d'unicité `(merchant_id, period_type, period_start)` : pas d'index supplémentaire.

#### Points à trancher

1. **Source du total perpétuel :** (a) tickets depuis mars 2026 ou (b) ventes depuis 2023 ? Recommandation : (b).
2. **Travail non commité dont dépend le lot B** (TVA figée, et par prudence 165 à 167) : il devra être commité avant les commits du lot B. Le commites-tu toi-même, ou veux-tu que je prépare des commits pour lui aussi ?
3. **Jours sans activité :** clôture à zéro pour tout établissement actif, à partir du jour de mise en production ? Recommandation : oui.

**Décisions d'Ilies (2026-10-07), en réponse :**
1. **Total perpétuel :** valeur d'ouverture = **somme des tickets antérieurs** (depuis le 2026-03-21), comptée une seule fois à la première clôture, méthode et date écrites dans la clôture. Le grand total de l'année en cours suit la même source.
2. **Travail non commité d'Ilies :** je prépare aussi ses commits atomiques (TVA figée, modes de clôture, exports comptables, borne, etc.), proposés pour accord **avant** les commits du lot B.
3. **Jours sans activité :** une clôture journalière chaque jour, à zéro si besoin, pour tout établissement actif, à partir du jour de mise en production.

### Phase 1 — Migrations (2026-10-07)

Fichiers écrits (`go test ./migrations/` vert), **non appliqués** :

- **[170_fiscal_closures](../migrations/todo/170_fiscal_closures.up.sql)** : table `fiscal_closures`, en ajout seul, une seule table pour les trois périodes (`DAY`, `MONTH`, `YEAR`).
  - Totaux de ventes, d'avoirs et nets (TTC / HT), ventilations jsonb (taux de TVA, moyens de paiement, canaux), nombre et bornes des numéros de ticket.
  - Empreintes des commandes (`orders`, pour `DAY`) et valeur d'ouverture (`opening`, première clôture).
  - `grand_total_period`, `perpetual_total`, chaînage et signature.
  - Contrainte d'unicité `(merchant_id, period_type, period_start)` : idempotence entre instances, et test « commande scellée ». Index de tête de chaîne.
  - **À appliquer sur staging maintenant.**
- **[171_drop_orders_fiscal_chain_index](../migrations/todo/171_drop_orders_fiscal_chain_index.up.sql)** : suppression `CONCURRENTLY` de `idx_orders_fiscal_chain_head`.
  - **À appliquer seulement après le code du lot B** : le code du lot A s'en sert encore.
  - En production, elle passera dans la mise en production unique, après le déploiement du code.

**Applications sur staging (Ilies, 2026-10-07) :** 170 et 171. L'index de la chaîne `orders` n'existe donc plus sur staging. Le code du lot A s'en passe : seule la performance de sa recherche du dernier maillon est en jeu, et le lot B retire cette recherche.

### Phase 2 — TVA des tickets et des avoirs, C9 (2026-10-07)

**Logique pure, dans [`internal/fiscal/tax.go`](../internal/fiscal/tax.go) :**
- `BuildTaxDetails` regroupe les parts par taux et déduit la remise de caisse, plafonnée au total et répartie au plus grand reste (`helpers.AllocateLargestRemainder`) ;
- `ProrateTaxDetails` ventile un avoir au prorata ;
- `ParseTaxDetails` lit la colonne ;
- HT arrondi au centime ; la TVA est le complément (HT + TVA = TTC).
- **Format de `receipts.tax_details` :** `{"lines":[{"rate","ttc","ht","tva"}],"discount"}`, lignes triées par taux, montants en centimes, nets des remises de caisse.

**Ticket de vente :**
- `receipt.Repository.GetOrderTaxLines` lit les parts de la commande, avec les règles de l'export comptable : (prix + suppléments) × quantité au taux figé sur la ligne (migration 164, à défaut celui de la catégorie), frais de livraison non nuls à leur taux, remises de caisse actives.
- `GenerateFiscalReceipt` remplit `tax_details`.
- `total_ttc` et `total_ht` ne changent pas de sens : TTC brut de la commande, utilisé tel quel par la facture PDF.

**Avoir :**
- Ventilation au prorata du ticket d'origine. Pour un ticket antérieur sans ventilation (`'{}'`), on prend les taux actuels de la commande.
- Une ligne d'article par taux, au lieu d'une ligne unique à TVA 0.
- `total_ht` = somme des HT, au lieu de `total_ht = total_ttc`.

**Dépendance :** le code utilise `models.OrderItemTVARateSQL`, `OrderItemTVAIDSQL`, `DeliveryFeesTVARateSQL`, `DiscountMOPsSQL` et `helpers.AllocateLargestRemainder`, qui appartiennent au travail non commité d'Ilies (TVA figée, remises de caisse). Ses commits devront précéder ceux du lot B.

**Tests :**
- **Unitaires :** taux et frais, remise au prorata, plafond, centime impair, ventilation vide, avoir de somme exacte, lecture de `'{}'`.
- **Intégration** `TestReceiptTaxDetails_Postgres` : commande à 10 % et 20 %, frais de livraison, remise de caisse ; ticket ventilé (1 800 / 720) ; avoir de 10 € ventilé au centime, une ligne par taux ; avoir d'un ticket antérieur à partir des taux de la commande.
- **Régression, contre staging :** `order_life_cycle`, `delivery_sessions`, `tasks`, webhooks Stripe et Uber, `pos/accounting`, `cash_registers` verts. Côté `ubereats`, seuls échouent les deux tests déjà en échec avant le lot.

### Phase 3 — Clôtures fiscales (2026-10-07)

**Moteur, dans [`internal/fiscal/closures.go`](../internal/fiscal/closures.go) :**
- **Chaîne `fiscal_closures`**, prise en dernier dans l'ordre des chaînes (`lock.go`).
- **`ComputeDayClosure`** calcule une journée (jour local de l'établissement) en trois requêtes :
  - tickets et avoirs du jour, nets via `tax_details` (un ticket antérieur au lot B compte pour son total, sans ventilation), ventilés par taux et par canal (`order_source`), avec le nombre et les bornes des numéros ;
  - paiements du jour par moyen, type et état ;
  - commandes closes (`delivered_on`) du jour, avec leur statut et leur empreinte `Fingerprint("order_closure", …)`. Le chargement groupé produit **la même empreinte** que `LoadOrderClosure` (vérifié par test).
- **`CloseDueDays`** clôt dans l'ordre les jours échus d'un établissement :
  - depuis le plus tardif entre la date de départ, le jour de création de l'établissement et le lendemain de la dernière clôture ;
  - jusqu'à la veille, ou l'avant-veille s'il est moins de 3 h locale ;
  - une transaction par jour, avec `LockChain` et un contrôle « déjà clos » sous le verrou : idempotent entre instances ;
  - le dernier jour du mois produit aussi la clôture du mois, le 31 décembre celle de l'année (sommes des journées).
- **Cumuls :**
  - la première clôture porte la valeur d'ouverture (`opening` : tickets antérieurs, total et part de l'année) ;
  - le grand total repart de zéro au 1er janvier ; le total perpétuel n'est jamais remis à zéro.
- **Outils :**
  - `LoadClosure` relit une clôture sous sa forme scellée (pour la vérification, lot E) ;
  - `IsOrderSealed` fournit le test « commande scellée » du lot C ;
  - `time/tzdata` est embarqué, pour lire les fuseaux sur tout hôte.

**Tâche de nuit :**
- `TasksManager.RunFiscalClosures`, en `@hourly` dans `cmd/api/tasks.go`, pour tout établissement actif.
- **Inactive tant que `FISCAL_CLOSURES_START_DATE` n'est pas défini.** C'est à la fois la date de départ (jour de mise en production) et l'interrupteur. Documenté dans `CLAUDE.md`.

**Tests (contre staging, verts) :**
- **`TestFiscalClosures_Postgres` :**
  - valeur d'ouverture (tickets 2025 et 2026) ;
  - vente à deux taux avec remise de caisse, paiement désactivé, commande rouverte exclue ;
  - avoir et annulation ;
  - clôture de février, 1er mars à zéro, aucune clôture avant 3 h ;
  - deux instances simultanées : une seule clôture du 2 mars ;
  - chaîne re-scellée à l'identique et linéaire ;
  - commande modifiée après clôture détectée ;
  - `IsOrderSealed`.
- **`TestFiscalClosures_YearBoundary_Postgres` :** clôture de l'année 2025, remise à zéro du grand total, total perpétuel continu.

**Mesure :**
- Calcul d'une journée pour l'établissement le plus actif de staging (151 commandes) : environ 150 ms depuis le poste, latence réseau comprise.
- Côté base : 6 ms pour les paiements (parcours complet de `payments`, l'index partiel ne s'applique pas), 30 ms pour les commandes (index `merchant_id`).
- C'est une fois par jour et par établissement : aucun index supplémentaire. À revoir en production seulement si la durée du passage le justifie (index `(merchant_id, payment_date)` et `(merchant_id, delivered_on)`).

### Phase 4 — Retrait du scellement par commande (2026-10-07)

**Retiré :**
- `fiscal.SealOrderClosure`, `OrderClosureSeal`, `SealColumns`, `LoadOrderClosureForUpdate` ;
- `sealOrderClosure` et le verrou de la chaîne `orders`.

**Les clôtures de commande n'écrivent plus que leur état et leur date de clôture** (`delivered_on`), qui rattache la commande à sa clôture journalière :
- clôture en caisse, refus et annulation ;
- clôtures Uber Eats (réconciliation et webhooks), avec le fragment commun `fiscal.ClosureColumns`.

**Conservé :**
- les transactions de la solution C ;
- `LockOpenOrdersByBrandOrderID`, qui renvoie désormais aussi l'établissement pour l'émission du ticket ;
- le ticket des ventes plateformes ;
- la clôture idempotente de `SetDeliveredExternal` ;
- `LoadOrderClosure` (empreinte de chaque commande dans sa clôture journalière) ;
- la constante `ChainOrders`, pour vérifier les lignes v2 déjà écrites.

**Ticket :** les parts de TVA et la remise de caisse sont lues en **une seule** requête (`GetOrderTaxLines`), au lieu de deux.

**Tests adaptés :**
- les clôtures de commande sont datées et n'écrivent plus d'empreinte sur leur ligne ;
- le refus figure, avec son statut, dans la clôture de son jour (`TestOrderLifeCycleRepository_DenyOrderLocal_SealedByDayClosure_Postgres`) ;
- les tests Uber Eats (réconciliation, webhooks) et `TestOrderLifeCycleRepository_Postgres` sont adaptés.

**Régression :**
- **unitaires :** seuls échouent les 4 paquets déjà en échec avant le lot ;
- **intégration contre staging :** `fiscal`, `order_life_cycle`, `receipt`, `audit`, `cash_registers`, `delivery_sessions`, `pos/accounting`, `tasks`, tous les webhooks et `customers` sont verts. Côté `ubereats`, seuls échouent les deux tests déjà en échec avant le lot.

**Mesures** (`TestFiscalChainPerf_Postgres`, depuis le poste, aller-retour réseau d'environ 20 ms) :

| | Avant le chantier | Lot A | Lot B |
|---|---|---|---|
| Encaissement séquentiel, p50 | 137 ms | 143–150 ms | 139–146 ms |
| Clôture + ticket + audit, p50 | 298 ms | 356–376 ms | **310 ms** |
| 10 clôtures simultanées (total) | 4,04 s | 5,5–6,4 s | **4,11 s** |
| Fourches / tickets en double | 13 / 2 | 0 / 0 | 0 / 0 |

- **Une clôture compte 16 allers-retours, contre 14 avant le chantier.** S'y ajoutent les verrous des chaînes `receipts` et `audit_logs` et la lecture de la TVA du ticket, moins les deux requêtes fusionnées. Le surcoût correspond à ces deux allers-retours, soit 1 à 2 ms entre l'API et la base de production.
- **Côté base, la clôture est plus légère qu'avant le chantier :** plus aucun parcours complet de table pour trouver le dernier maillon.

### Phase 5 — Démarrage sans configuration, rattrapage, clôture du lot (2026-10-07)

**Décision d'Ilies :** plus de `FISCAL_CLOSURES_START_DATE`. La tâche horaire démarre sans condition dès le déploiement, et un script rattrape les jours antérieurs avant de lui laisser la main.

**Démarrage de la tâche :**
- **`fiscal.DueRange`** calcule les jours à clôturer :
  - le lendemain de la dernière clôture ;
  - pour un établissement sans clôture : le **dernier jour échu** (sa valeur d'ouverture couvre tout ce qui précède), ou la date `from` d'un rattrapage ;
  - jamais avant la création de l'établissement.
- **`CloseDueDays`** prend `from *time.Time`, qui vaut `nil` pour la tâche.
- Une chaîne ne revient jamais en arrière : un rattrapage demandé après la première clôture reprend au lendemain de la dernière.

**[`cmd/backfill_fiscal_closures`](../cmd/backfill_fiscal_closures/main.go) :**
- `--from=AAAA-MM-JJ` (obligatoire), `--merchant` (facultatif), `--apply` (sinon simulation) ;
- autonome : il n'exige que `POSTGRES_URL` et `FISCAL_SIGNING_KEY`, et refuse d'écrire sans la clé ;
- même calcul que la tâche, idempotent ;
- **simulation sur staging** (`--from=2026-10-01`) : 31 établissements, 6 jours chacun, 186 jours, aucune erreur.

**Tests :** `TestFiscalClosures_AutoStart_Postgres` vérifie trois choses :
- sans rattrapage, seul le dernier jour échu est clos, et les tickets antérieurs vont dans l'ouverture ;
- le lendemain, la tâche continue au jour suivant ;
- un rattrapage tardif ne remonte pas avant la première clôture.

Les deux tests de clôture existants passent par le rattrapage (`from`).

**Mise en production (ajoutée à la feuille de route) :**
1. Migrations 168, 169 et 170.
2. `backfill_fiscal_closures --from=<premier jour voulu> --apply`, avec la clé de production.
3. Déploiement du code.
4. Migration 171.

Si le code part avant le rattrapage, la tâche commence au dernier jour échu et les jours antérieurs restent seulement dans la valeur d'ouverture.

**Bilan du lot B :**
- C6 (clôtures journalières, mensuelles et annuelles scellées) et C9 (TVA ventilée sur les tickets et les avoirs) sont traités ;
- les commandes sont scellées par la clôture journalière (S1) ;
- les clôtures de commande retrouvent leur coût d'avant le chantier.

