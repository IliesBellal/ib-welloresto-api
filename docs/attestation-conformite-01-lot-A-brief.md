# PROMPT — Conformité caisse, lot A : empreintes complètes, chaînes sérialisées, plateformes dans la chaîne

**Date :** 2026-10-07
**Référence :** [attestation-conformite-00-audit.md](attestation-conformite-00-audit.md) : constats C3, C5 et C10, et arbitrages d'Ilies du 2026-10-06.
**Objectif du chantier :** pouvoir signer l'attestation éditeur (BOI-LETTRE-000242). Ce lot rend les chaînes fiscales complètes, linéaires et vérifiables. Les lots suivants traiteront B (C1, C2), C (C6, C7) et D (C4, C8).

## Règle d'or

1. **Aucune régression de performance.** Les écritures fiscales (encaissement, clôture de commande, ticket, avoir, fermeture de registre, journal d'audit) ne doivent pas être plus lentes qu'aujourd'hui. Le temps de réponse est mesuré avant (phase 0) et après (phase 5). Chaque recherche du « dernier maillon » doit passer par un index.
2. **Aucune donnée existante n'est modifiée.** Les lignes déjà écrites gardent leur empreinte v1, même fausse ou fourchue. On ne répare rien et on ne recalcule rien.
3. **Aucun changement fonctionnel visible**, en dehors de ce qui est listé en phase 4.
4. **Les migrations sont écrites, jamais appliquées**, ni sur staging ni ailleurs. Ilies les applique. Si un test a besoin d'une migration, on s'arrête et on le signale.
5. **Points d'arrêt :** à la fin de chaque phase, on présente le livrable et on attend la validation avant de continuer. La phase 0 se rend **avant toute modification de code**.

## Périmètre

- **Inclus :**
  - **C3 :** empreinte complète et signée sur les cinq chaînes ;
  - **C5 :** sérialisation des chaînes et unicité du numéro de ticket ;
  - **C10 :** les clôtures Uber Eats et Deliveroo passent par la chaîne.
- **Exclus :**
  - **C1** (réouverture) et **C2** (annulations de paiement, garde-fous des webhooks) : lot B. En particulier, la façon dont les webhooks désactivent les paiements **ne change pas** dans ce lot.
  - **C4, C6, C7, C8 et C9.**

## Faits vérifiés (staging, lecture seule, 2026-10-07)

- **Postgres 18.4.** Volumes : `payments` 33 608, `orders` 33 867, `receipts` 5 870, `audit_logs` 10 590, `cash_registers` 710.
- **Index manquants.** Les recherches du dernier maillon font un parcours complet de la table, à chaque écriture et sous verrou :
  - `payments` : seuls `payment_id` et `order_id` sont indexés, rien sur `(merchant_id, payment_date)` ;
  - `receipts` : clé primaire seulement ;
  - `audit_logs` : clé primaire seulement, alors que la table est lue à **chaque** mutation de commande ;
  - `cash_registers` : clé primaire seulement ;
  - `orders` : `merchant_id` et `state` sont indexés séparément, mais pas `(merchant_id, delivered_on)`.
- **Fourches existantes**, c'est-à-dire un même `previous_hash` utilisé deux fois chez un même établissement : **255 dans `payments`**, 4 dans `orders`, 0 dans `receipts`. La course décrite en C5 est donc réelle.
- **Aucun numéro de ticket en double** sur staging : la contrainte d'unicité peut être posée là-bas. Pour la production, voir la phase 1.
- `receipts.tax_details`, `items_snapshot`, `payments_snapshot` et `audit_logs.old_values`, `new_values` sont des colonnes **jsonb**. Postgres réordonne les clés et normalise les espaces : le texte relu n'est pas celui qui a été écrit.

## Phase 0 — Recensement (rendu avant toute modification)

1. **Liste exhaustive des points d'écriture fiscale.** Pour chacun : fichier et ligne, chaîne touchée, appel ou non dans une transaction, chaînes touchées dans la même transaction et dans quel ordre. Points attendus au minimum :
   - commandes : `SetDeliveredLocal`, `DeleteOrderLocal`, `DenyOrderLocal` ;
   - paiements : `AddPaymentAndReturnID` ;
   - tickets : `GenerateFiscalReceipt`, `GenerateRefundReceipt` ;
   - registres : `CloseCashRegister` (le service appelle le dépôt **hors transaction**) ;
   - journal d'audit : `InsertLogWithChain` ;
   - plateformes (C10) : `ubereats.SyncOrderState` (état CLOSED), `ubereats.HandleOrderNotFound`, webhook Uber `CancelOrder` et `MarkFailed`, Deliveroo `UpdateOrderRejected`.
2. **Commandes Uber Eats et Deliveroo sur staging :** proportion de commandes dont le total des paiements actifs est égal au prix au moment de la clôture. `SetDeliveredLocal` refuse de clôturer une commande qui n'est pas entièrement payée. Si une part significative ne l'est pas, on s'arrête et on le signale avant d'écrire la phase 4.
3. **Base de comparaison des performances :** temps de réponse p50 et p95 de `POST /orders/{id}/payments` et de la clôture de commande, en local contre une copie de staging ou par test d'intégration chronométré. Mesurer aussi 10 encaissements simultanés sur un même établissement. Joindre le `EXPLAIN` des recherches du dernier maillon actuelles.

**Livrable :** le recensement, les chiffres, et les écarts éventuels avec ce brief. **Point d'arrêt.**

## Phase 1 — Migrations (fichiers seulement)

Suivre le format de `migrations/todo/` (prochain numéro : 168), avec fichiers `up` et `down`, et les garde-fous `IF NOT EXISTS` du dépôt.

- **168 — colonnes :**
  - `hash_version smallint NOT NULL DEFAULT 1` sur `receipts`, `orders`, `payments`, `cash_registers`, `audit_logs`. Un `DEFAULT` constant est instantané en Postgres 11 et plus : aucune réécriture de table ;
  - `audit_logs.signature text NULL`.
- **169 — index, en `CONCURRENTLY`.** Suivre le modèle de `migrations/done/163_orders_public_id_unique_index` (hors transaction) :
  - `payments (merchant_id, payment_date DESC, payment_id DESC)` ;
  - `receipts (merchant_id, created_at DESC)` ;
  - `UNIQUE receipts (merchant_id, receipt_number)` ;
  - `orders (merchant_id, delivered_on DESC, order_id DESC) WHERE state = 'CLOSED'` ;
  - `audit_logs (merchant_id, created_at DESC, id DESC)` ;
  - `cash_registers (merchant_id, end_date DESC)`.
- **Requête de contrôle à fournir à Ilies**, à lancer en production avant la 169 : doublons de `(merchant_id, receipt_number)`. S'il y en a, la création de l'index unique échoue : on s'arrête et on en discute, sans corriger les données.

**Livrable :** les fichiers et la requête de contrôle. **Point d'arrêt : Ilies applique sur staging.**

## Phase 2 — C5 : un seul verrou par établissement

- **Une fonction utilitaire unique**, par exemple `fiscal.LockMerchant(ctx, merchantID)`, qui exécute `SELECT pg_advisory_xact_lock(hashtextextended('fiscal:' || $1, 0))`.
  - **Un verrou par établissement pour toutes les chaînes**, pas un par chaîne. Aucun interblocage n'est possible, quel que soit l'ordre des chaînes dans une transaction.
  - Il **remplace** les `FOR UPDATE` actuels des recherches du dernier maillon, sans s'y ajouter.
  - Il est pris **juste avant** la recherche du dernier maillon, pas en début de transaction.
  - Il est réentrant : le prendre deux fois dans la même transaction ne pose pas de problème.
- **Garde :** la fonction renvoie une erreur si le `ctx` ne porte pas de transaction. Un verrou de transaction pris hors transaction est relâché immédiatement et ne protège rien.
- **Où vit la transaction (solution C, validée le 2026-10-07, voir le journal de la phase 0) :** les fonctions ouvrent elles-mêmes la transaction avec `dbutils.RunInTx`, qui réutilise celle de l'appelant s'il y en a une. Aucun appelant n'est modifié.
  - **Les 4 opérations métier**, entières : `DeliverOrder`, `DeleteOrder`, `SetOrderDenied` et la fermeture de registre (`CloseCashRegister`). Pour la fermeture, le verrou est pris juste avant le scellement, pas avant le calcul du Z.
  - **Les 2 écritures appelées de partout :** `AddPaymentAndReturnID` et `InsertLogWithChain`.
  - **Les clôtures plateformes de la phase 4** passent par ces fonctions, leurs mises à jour propres comprises.
- **Pourquoi c'est neutre en performance :**
  - aujourd'hui, chaque mutation de commande verrouille déjà la dernière ligne `audit_logs` de l'établissement jusqu'à la validation de la transaction : les écritures d'un même établissement sont déjà sérialisées ;
  - le verrou consultatif ne coûte qu'un appel, sans lecture de table ;
  - les index de la 169 remplacent les parcours complets actuels.
- **Pas de singleflight**, pour les raisons données dans l'audit.

## Phase 3 — C3 : empreinte v2

**Un paquet unique** (par exemple `internal/fiscal`) porte, pour chaque chaîne, une structure de charge utile dédiée et une fonction `HashV2(prev, payload) (hash, signature)`. La signature réutilise `security.SignHash`. Toutes les chaînes v2, `audit_logs` compris, sont signées.

**Règles de sérialisation.** Ce sont elles qui rendent l'empreinte vérifiable :
- **JSON canonique d'une structure Go à champs ordonnés.** Montants en centimes entiers. Lignes triées par identifiant stable.
- **Horodatages :** UTC, tronqués à la microseconde (la précision de Postgres), au format RFC 3339 nano. La **même valeur** est stockée et hachée : l'horodatage est fixé en Go et passé en paramètre, plus de `now()` ou `UTCNow()` côté SQL dans les écritures chaînées.
- **Données jsonb :** l'empreinte porte sur les structures décodées puis réencodées de façon canonique (`json.Decoder.UseNumber`, clés triées), jamais sur les octets bruts. Un test doit prouver qu'une empreinte recalculée à partir de la ligne **relue en base** est identique. Si c'est impossible pour `audit_logs` (contenu libre), on hache le `::text` renvoyé par l'insertion (`RETURNING old_values::text, new_values::text`), puis on met à jour l'empreinte dans la même transaction.
- **Premier maillon v2 :** il chaîne sur le dernier maillon existant, quelle que soit sa version. Une chaîne vide démarre sur `GENESIS_HASH` (aujourd'hui, `DeleteOrderLocal` et `payments` démarrent sur une chaîne vide).
- **Nouvelles lignes :** `hash_version = 2`.

**Contenu de la charge utile par chaîne :**

| Chaîne | Inclus | Exclu, avec la raison |
|---|---|---|
| `receipts` | établissement, numéro, `order_id`, `created_at`, `total_ttc`, `total_ht`, `tax_details`, `items_snapshot`, `payments_snapshot` | — |
| `orders` (clôture) | établissement, `order_id`, `state`, `brand_status`, `order_type`, `price`, `ht`, `tva`, `delivered_on`, et les lignes (`order_item_id`, `product_id`, quantité, `base_price`, `price`, `tva_rate`, `discount_id`, et toute composante de prix des options, à vérifier dans `order_item_configuration`) | — |
| `payments` | établissement, `order_id`, `amount`, `mop`, `operation_type`, `payment_date`, `user_id`, `comment` | `payment_id` (connu seulement après l'insertion) ; `cash_register_id` (rattaché à la clôture du registre, accepté en C11) ; `enabled` (état dérivé, lot B) ; `fee`, `net_amount` et `status_check` (écrits ensuite par Stripe) |
| `cash_registers` | `cash_register_id`, établissement, `start_date`, `end_date`, `cash_fund`, `final_cash_fund`, lignes `cash_registers_items` (moyen de paiement, montant) triées | relevés `cash_registers_custom_items` (déclaratifs, saisis après la fermeture) |
| `audit_logs` | établissement, `user_id`, `action`, `resource_type`, `resource_id`, `created_at`, `old_values`, `new_values` | — |

**Performance :** la clôture de commande ajoute une seule requête de lecture des lignes, indexée par `order_id` (à vérifier avec `EXPLAIN`). Le calcul SHA-256 et HMAC prend quelques microsecondes.

## Phase 4 — C10 : les plateformes dans la chaîne

- **Toute fermeture de commande passe par la clôture commune**, et ne s'applique qu'à une commande encore `OPEN` :
  - **vente terminée** (Uber `COMPLETED`, `READY_FOR_HANDOFF` → CLOSED dans `HandleOrderNotFound`) : même chemin que `DeliverOrder`, c'est-à-dire clôture chaînée et ticket fiscal ;
  - **annulation, refus ou échec** (Uber `CancelOrder`, `MarkFailed`, `DENIED` ou `CANCELED` dans `SyncOrderState`, Deliveroo `UpdateOrderRejected`) : même chemin que `DeleteOrderLocal` et `DenyOrderLocal`, c'est-à-dire clôture chaînée sans ticket.
- **Les colonnes métier** propres à chaque chemin (`brand_status`, `cancelled_by_type` avec sa garde `IS NULL`, `deletion_reason_id`, `merchant_approval`) gardent exactement les mêmes valeurs qu'aujourd'hui.
- **Clôture idempotente :** une commande déjà `CLOSED` n'est jamais rechaînée et ne reçoit jamais un second ticket. Cas réel à couvrir : `SetDeliveredExternal` (livraison confirmée par Uber), qui ne vérifie pas aujourd'hui si la commande est ouverte ([order_life_cycle/service.go:400](../internal/modules/order_life_cycle/service.go#L400)). Si le restaurateur a déjà clôturé, Uber réécrit l'empreinte et émet un second ticket.
- **Inchangé dans ce lot :** la désactivation des paiements par les webhooks (lot B).

## Tests attendus

- **Unitaires :** stabilité de la charge utile (ordre des lignes, horodatage, jsonb) ; une empreinte v2 recalculée à partir d'une ligne relue est identique, pour chacune des cinq chaînes.
- **Intégration** (`postgres_integration`), pour chaque chaîne :
  - 10 écritures simultanées sur un même établissement donnent une chaîne linéaire (aucun parent partagé), et des numéros de ticket distincts et continus ;
  - deux établissements en parallèle ne se bloquent pas ;
  - le premier maillon v2 chaîne sur le dernier v1 ;
  - une fonction de verrou appelée hors transaction renvoie une erreur.
- **Plateformes :** une vente Uber terminée donne une clôture chaînée et un ticket ; une annulation Uber ou un refus Deliveroo donne une clôture chaînée sans ticket ; une livraison confirmée par Uber sur une commande déjà clôturée ne fait rien (un seul ticket, empreinte inchangée).
- **Non-régression :** les tests existants de `order_life_cycle`, `cash_registers`, `receipt`, `audit`, `delivery_sessions`, `kiosk` et des webhooks sont verts. Les tests qui vérifient la formule v1 (par exemple `deny_order_fiscal_chain`) sont adaptés à la v2, sans perdre leur intention.

## Phase 5 — Mesures et livrables

- **Performance :** mêmes mesures qu'en phase 0, après le changement. Critère d'acceptation : le p95 n'est pas pire qu'avant. `EXPLAIN` montre un `Index Scan` pour chaque recherche du dernier maillon.
- **Livrables :**
  - le code ;
  - les migrations 168 et 169 (non appliquées) et la requête de contrôle ;
  - les résultats de tests et les mesures ;
  - une entrée en tête de `docs/decisions.md` ;
  - un journal du lot ajouté à `attestation-conformite-00-audit.md` : constats C3, C5 et C10 marqués comme traités, écarts éventuels.
- **Pas de commit sans accord explicite.**

## Journal

### Phase 0 — Recensement (2026-10-07)

#### 1. Points d'écriture fiscale

| # | Chaîne | Point d'écriture | Appelants | Transaction |
|---|---|---|---|---|
| 1 | `orders` | `SetDeliveredLocal` ([order_life_cycle/repository.go:929](../internal/modules/order_life_cycle/repository.go#L929)), via `DeliverOrder` | `SetDelivered` (caisse, tournées de livraison), `SetDeliveredExternal` (webhook Uber) | oui (`ExecuteOrderMutation`) |
| | | | tâche planifiée `CloseOrders` ([tasks/orders.go:66](../internal/tasks/orders.go#L66)) appelle `DeliverOrder` directement | **non** : la clôture et le ticket sont écrits dans deux transactions implicites distinctes |
| 2 | `orders` | `DeleteOrderLocal` ([:844](../internal/modules/order_life_cycle/repository.go#L844)), via `DeleteOrder` | `SetOrderDeleted` (caisse) | oui |
| | | | webhook Stripe ([stripe/service.go:382](../internal/webhook/stripe/service.go#L382)), webhook Deliveroo ([deliveroo_orders/service.go:105](../internal/webhook/deliveroo_orders/service.go#L105), [:178](../internal/webhook/deliveroo_orders/service.go#L178), [:442](../internal/webhook/deliveroo_orders/service.go#L442)), borne ([kiosk/service.go:2076](../internal/modules/kiosk/service.go#L2076)), Scan'n'Order ([scannorder/service.go:1027](../internal/modules/scannorder/service.go#L1027)) | **non** |
| 3 | `orders` | `DenyOrderLocal` ([:704](../internal/modules/order_life_cycle/repository.go#L704)), via `SetOrderDenied` | caisse (`DenyOrder`), tâche `DenyOrders` ([tasks/orders.go:128](../internal/tasks/orders.go#L128)), webhook Stripe ([stripe/service.go:369](../internal/webhook/stripe/service.go#L369)) | **jamais** |
| 4 | `payments` | `AddPaymentAndReturnID` ([:149](../internal/modules/order_life_cycle/repository.go#L149)) | encaissement en caisse, remboursement, paiement Scan'n'Order (webhook Stripe), paiement borne (Stripe Terminal) | oui |
| | | | paiements initiaux à la création de commande (`insertPayments`, [:2795](../internal/modules/order_life_cycle/repository.go#L2795)) : caisse, borne, Scan'n'Order, Uber Eats, Deliveroo | **non** (`CreateOrder` n'ouvre pas de transaction) |
| 5 | `receipts` | `GenerateFiscalReceipt`, via `DeliverOrder` | comme le point 1 | comme le point 1 |
| 6 | `receipts` | `GenerateRefundReceipt` | `ProcessRefund` | oui |
| 7 | `cash_registers` | `CloseCashRegister` ([cash_registers/repository.go:350](../internal/modules/cash_registers/repository.go#L350)) | service de fermeture du registre | **non** |
| 8 | `audit_logs` | `InsertLogWithChain`, via `LogChange` | `order_life_cycle` (mutations de commande, `UpdateOrder`, lien client-facture) | oui |
| | | | clients, utilisateurs, rôles, HACCP, planning (`_ = LogChange(...)`, erreur ignorée) | **non** |
| 9 | aucune (C10) | `ubereats.SyncOrderState` ([ubereats/repository.go:418](../internal/modules/ubereats/repository.go#L418)), `HandleOrderNotFound` ([:496](../internal/modules/ubereats/repository.go#L496)), webhook Uber `CancelOrder` et `MarkFailed` ([orders_repo.go:39](../internal/webhook/ubereats/repository/orders_repo.go#L39), [:138](../internal/webhook/ubereats/repository/orders_repo.go#L138)), Deliveroo `UpdateOrderRejected` ([deliveroo_orders/repository.go:248](../internal/webhook/deliveroo_orders/repository.go#L248)) | réconciliation Uber, webhooks | **non**, et sans empreinte |

- **Code mort :** `webhook/stripe/repository.go` `InsertPayment` (insertion de paiement sans empreinte, commentée « Decom ») n'a aucun appelant.
- **Ordre des chaînes dans une même transaction :** clôture = `orders` → `receipts` → `audit_logs` ; remboursement = `payments` → `receipts` → `audit_logs` ; encaissement = `payments` → `audit_logs`. Avec un verrou unique par établissement, cet ordre n'a pas d'importance.
- **Précédent dans le code :** `pg_advisory_xact_lock` est déjà utilisé pour les paiements Stripe Terminal ([infrastructure/stripe/terminal.go:1091](../internal/infrastructure/stripe/terminal.go#L1091)).

#### 2. Commandes Uber Eats et Deliveroo sur staging

| Plateforme | `brand_status` (état CLOSED) | Commandes | Paiements = prix | Avec empreinte | Avec ticket |
|---|---|---|---|---|---|
| Uber Eats | CLOSED (clôturée en caisse) | 3 196 | 3 194 | 1 053 | 1 053 |
| Uber Eats | **COMPLETED** (`SyncOrderState`) | **1 150** | 1 150 | **0** | **0** |
| Uber Eats | READY_FOR_HANDOFF (`HandleOrderNotFound`) | 7 | 7 | 0 | 0 |
| Uber Eats | CANCELED / DENIED / FAILED / DELIVERY_FAILED | 85 | — | 1 | 0 |
| Deliveroo | CLOSED | 303 | 302 | 139 | 139 |
| Deliveroo | COLLECTED / DELIVERED / READY_FOR_HANDOFF | 364 | 363 | 0 | 0 |
| Deliveroo | CANCELED / REJECTED / DENIED / DELETED | 243 | — | 6 | 0 |

- **Ventes :** les paiements couvrent le prix dans 99,9 % des cas. Le contrôle « entièrement payée » de `SetDeliveredLocal` passe : 3 exceptions sur environ 5 000 (2 Uber, 1 Deliveroo).
- **Uber `COMPLETED` est le premier chemin de vente qui échappe à la chaîne :** 1 150 commandes sans empreinte ni ticket.
- **Anciennes lignes sans empreinte :** les commandes caisse non chaînées (19 000 sur 23 500 clôturées) datent d'avant la chaîne. On n'y touche pas (règle d'or 2).
- **Tickets de vente multiples, déjà présents :** 36 commandes caisse, 2 Uber et 1 Deliveroo ont plus d'un ticket de vente (reclôtures, et livraison Uber confirmée après la clôture en caisse). 23 commandes caisse annulées ont un ticket de vente sans avoir (clôturée, rouverte, puis annulée : relève de C1, lot B).

#### 3. Performances de référence

**Recherche du dernier maillon** (`EXPLAIN ANALYZE`, établissement le plus actif de staging : 18 592 paiements) :

| Chaîne | Plan actuel | Durée côté base |
|---|---|---|
| `orders` | index `merchant_id` puis tri de 18 969 lignes | **106 ms** |
| `payments` | parcours complet de la table (33 608 lignes) | **80 ms** |
| `audit_logs` | parcours complet | 14 ms |
| `receipts` | parcours complet | 3 ms |
| `cash_registers` | parcours complet | 0,3 ms |

Chaque encaissement et chaque clôture paient donc 80 à 120 ms de base de données sous verrou, rien que pour trouver le maillon précédent, et ce coût croît avec la table.

**Mesure de bout en bout** (`TestFiscalChainPerf_Postgres`, `FISCAL_PERF=1`, contre staging depuis le poste local, latence réseau incluse : seul l'écart avant/après compte) :

| Opération | p50 | p95 | max |
|---|---|---|---|
| Encaissement, séquentiel (30) | 137 ms | 221 ms | 308 ms |
| Encaissement, 10 simultanés | 2,05 s | 2,37 s | 2,48 s (total 2,48 s) |
| Clôture + ticket + audit, séquentielle (20) | 298 ms | 332 ms | 720 ms |
| Clôture + ticket + audit, 10 simultanées | 2,64 s | 3,66 s | 4,04 s (total 4,04 s) |

**Intégrité observée pendant la mesure, code actuel :**
- 6 fourches `payments` sur 10 encaissements simultanés ;
- 6 fourches `orders`, 2 fourches `receipts` et **2 numéros de ticket en double** sur 10 clôtures simultanées ;
- 1 fourche `audit_logs`.

#### Écarts avec le brief, à valider

1. **Transactions.** Beaucoup plus de chemins hors transaction que prévu (points 1 à 4, 7, 8 et 9 ci-dessus). **Validé (Ilies, 2026-10-07) : solution C, mixte.** Les 4 opérations métier (`DeliverOrder`, `DeleteOrder`, `SetOrderDenied`, `CloseCashRegister`) ouvrent leur propre transaction et deviennent atomiques. Les 2 écritures appelées de partout (`AddPaymentAndReturnID`, `InsertLogWithChain`) se protègent elles-mêmes. `LockMerchant` garde son contrôle « erreur hors transaction » comme filet. Changement de comportement assumé : une annulation ou un refus qui échoue en cours de route est annulé en entier au lieu de rester à moitié fait.
2. **Plateforme, vente non entièrement payée** (3 cas sur environ 5 000). `SetDeliveredLocal` refuse la clôture. **Validé (Ilies, 2026-10-07) :** la commande reste ouverte et l'erreur est journalisée, comme pour une commande caisse.
3. **Fichier ajouté avant la phase 1 :** le test de mesure `fiscal_chain_perf_postgres_integration_test.go`. C'est un test seulement ; aucun code de production n'a été modifié.

### Phase 1 — Migrations (2026-10-07)

**Constat supplémentaire, qui a changé les index prévus.**
- La recherche actuelle du dernier maillon trie `delivered_on DESC` (et `payment_date DESC`). En Postgres, les valeurs NULL passent **en premier** dans ce tri.
- Staging compte 1 438 commandes clôturées sans `delivered_on`, toutes sans empreinte. Quand l'une d'elles ressort en tête, le maillon suivant chaîne sur une valeur vide : la chaîne redémarre.
- Résultat : **196 redémarrages de la chaîne `orders`** (et 86 pour `payments`), alors que seuls 9 établissements sont chaînés.
- **Conséquence :** la recherche v2 ne retient que les lignes chaînées (`hash IS NOT NULL`), et les index de `orders`, `payments` et `cash_registers` sont partiels sur cette condition. Ils sont aussi plus petits.
- **Le filtre `state = 'CLOSED'` est retiré de la recherche `orders`.** Une commande rouverte reste la tête de chaîne qu'elle était : l'exclure créerait une fourche au maillon suivant.

**Fichiers écrits** (`go test ./migrations/` vert) :
- [168_fiscal_hash_version](../migrations/todo/168_fiscal_hash_version.up.sql) :
  - `hash_version smallint NOT NULL DEFAULT 1` sur les cinq tables, et `audit_logs.signature` ;
  - `lock_timeout` de 5 s pour qu'un `ALTER` en attente ne bloque pas le trafic.
- [169_fiscal_chain_indexes](../migrations/todo/169_fiscal_chain_indexes.up.sql), en `CONCURRENTLY`, une instruction à la fois :
  - `payments (merchant_id, payment_date DESC, payment_id DESC) WHERE hash IS NOT NULL` ;
  - `orders (merchant_id, delivered_on DESC, order_id DESC) WHERE hash IS NOT NULL` ;
  - `receipts (merchant_id, created_at DESC, receipt_number DESC)` ;
  - `UNIQUE receipts (merchant_id, receipt_number)` ;
  - `audit_logs (merchant_id, created_at DESC, id DESC)` ;
  - `cash_registers (merchant_id, end_date DESC) WHERE hash IS NOT NULL`.
- **Requête de contrôle des doublons**, à lancer en production avant la 169 : elle figure en tête de la 169.
- **Appliquées sur staging par Ilies** le 2026-10-07. Vérifié : 5 colonnes `hash_version`, `audit_logs.signature`, 6 index `indisvalid = true`.

### Phases 2 et 3 — Verrou et empreinte v2 (2026-10-07)

**Nouveau paquet [`internal/fiscal`](../internal/fiscal/) :**
- `LockChain` : verrou consultatif de transaction ; renvoie `ErrNoTransaction` s'il est appelé hors transaction ;
- `Seal` : SHA-256 de `{chain, v, prev, data}`, puis HMAC (`security.SignHash`) ;
- `Canonical` : JSON canonique (clés triées, nombres en décimal exact, indépendant de jsonb) ;
- `Now` et `FormatTime` : UTC à la microseconde ;
- une charge utile par chaîne, et `LoadOrderClosure` / `LoadCashRegisterClosure`, qui lisent en base ce qu'ils scellent. Ces fonctions serviront aussi à la commande de vérification (C4).

**Écart 1 avec le brief : un verrou par chaîne et par établissement, au lieu d'un verrou unique par établissement.**
- **Problème découvert pendant l'écriture :** l'entrée d'audit est écrite en fin de mutation, après le verrou de ligne de la commande. Avec un verrou unique, deux transactions pouvaient s'attendre mutuellement :
  - une modification de la commande X tient la ligne X et attend le verrou de l'établissement pour son audit ;
  - un encaissement sur X tient le verrou de l'établissement et attend la ligne X pour la marquer payée.
- **L'ancien code n'avait pas ce cycle :** il verrouillait chaque chaîne séparément, via le `FOR UPDATE` de sa dernière ligne.
- **Correction :** `LockChain(ctx, chain, merchantID)`, avec la clé `fiscal:<chaîne>:<établissement>`. Elle reproduit exactement la topologie de verrous d'avant, sans la course. Les chaînes sont toujours prises dans le même ordre (payments → orders → receipts → cash_registers → audit_logs, l'audit étant toujours en dernier) : aucun cycle entre chaînes.
- **Effet secondaire :** moins d'attente qu'avec le verrou unique, puisqu'un encaissement ne bloque plus une clôture.

**Écart 2 : les trois fonctions de clôture de commande** (`SetDeliveredLocal`, `DeleteOrderLocal`, `DenyOrderLocal`) ouvrent aussi leur propre transaction, réentrante. C'est une défense en profondeur : plusieurs tests et chemins les appellent directement. Le verrou, la lecture du dernier maillon et l'écriture de la clôture sont tous à l'intérieur de la fonction, donc c'est sans effet sur l'atomicité des opérations métier.

**Transactions (solution C) :**
- `DeliverOrder` : clôture et ticket dans une même transaction. Les effets de bord (stocks, réservation, plateformes) restent après, comme avant.
- `DeleteOrder` et `SetOrderDenied` : partie base de données atomique ; les appels aux plateformes restent après la validation.
- `CloseCashRegister` : une seule transaction. Une fermeture concurrente du même registre est détectée au scellement (`UPDATE … AND closed = false` qui ne touche aucune ligne), annulée en entier, puis rendue comme « déjà fermé ». Les lignes du Z ne peuvent plus être dupliquées.
- `AddPaymentAndReturnID` et `InsertLogWithChain` : leur propre transaction, ou celle de l'appelant.
- `GetLastReceiptData` : exige la transaction de l'appelant (l'insertion du ticket suivant doit s'y faire). Son test a été adapté.

**Dernier maillon :** `hash IS NOT NULL` sur `orders`, `payments` et `cash_registers` (voir la phase 1), plus de `FOR UPDATE`, parent `GENESIS_HASH` pour une chaîne vide.

**Hors empreinte, avec la raison :**
- `orders.state` et `brand_status` : réécrits par les plateformes après la clôture ;
- options des lignes (`order_item_configuration`) : aucun prix de vente propre (le prix est dans `orderitems.price`) ;
- colonnes listées au tableau de la phase 3.

**Tests :**
- **unitaires** `internal/fiscal` : forme canonique stable après passage par jsonb, nombres, déterminisme du scellement et liaison chaîne / parent / données, troncature horaire, garde hors transaction ;
- **intégration** `TestFiscalChainV2_Postgres` : vente avec lignes et 2 paiements, ticket, avoir, refus, annulation, puis 10 encaissements et 10 clôtures simultanés. Chaque ligne des 4 chaînes est re-scellée à partir de sa relecture en base (empreinte et signature identiques), chaque chaîne est linéaire (aucune fourche, aucun redémarrage), et les numéros de ticket sont continus ;
- **intégration** `TestFiscalLock_DoesNotBlockOtherMerchants_Postgres` ;
- **intégration** `cash_registers` : deux fermetures simultanées (une seule ferme, aucune ligne de Z dupliquée), et empreinte de fermeture re-scellée à l'identique.
- **Suite unitaire complète verte**, sauf 4 paquets (`planning/leave`, `planning/swaps`, `planning/employees`, et le test BYOC de `ubereats`) dont les échecs sont **identiques sur `HEAD` propre**, vérifiés dans un worktree temporaire : ils préexistent et sont sans lien avec ce lot.
- **Intégration, tous les paquets** (`./internal/... ./cmd/...` contre staging) : tous les paquets qui écrivent dans une chaîne sont verts (`order_life_cycle`, `receipt`, `audit`, `cash_registers`, `delivery_sessions`, `pos/accounting`, `tasks`, `customers`, `haccp`, webhooks Stripe, Uber Eats et Deliveroo). Ils ont été relancés après le passage au verrou par chaîne, ce qui ajoute `TestFiscalLock_NoDeadlockBetweenOrderRowAndChains_Postgres`.
- **Les 15 paquets en échec sont sans lien avec les chaînes :**
  - données d'amorçage ou schéma de staging : migration 167 `card_payment_pos_toggle` non appliquée, colonnes CDS absentes, contraintes NOT NULL ou de longueur dans les amorçages `roles` et `users`, échecs avant toute écriture d'audit ;
  - tests qui ne compilent plus dans `integrations`, `menu`, `planning/employees` et `planning/performance` ;
  - les 4 échecs unitaires préexistants.

**Mesures** (`TestFiscalChainPerf_Postgres`, 3 passages, contre staging depuis le poste local) :

| | Avant (phase 0) | Après |
|---|---|---|
| Aller-retour réseau seul (`SELECT 1`) | — | p50 21 ms |
| Encaissement séquentiel | p50 137 ms, p95 221 ms | p50 143–150 ms, p95 154–211 ms |
| Clôture + ticket + audit séquentielle | p50 298 ms, p95 332 ms | p50 356–376 ms, p95 423–584 ms |
| 10 encaissements simultanés (total) | 2,48 s | 2,8–3,1 s |
| 10 clôtures simultanées (total) | 4,04 s | 5,5–6,4 s |
| Fourches (4 chaînes) / tickets en double | 13 / **2** | **0 / 0** |

**Lecture des mesures :**
- **Depuis le poste local, le temps mesuré égale le nombre de requêtes × l'aller-retour réseau** (21 ms) :
  - encaissement : 7 allers-retours, soit 147 ms, pour une mesure de 150 ms ;
  - clôture : 17 allers-retours, soit 357 ms, pour une mesure d'environ 360 ms.
- **Le lot ajoute 1 requête par chaîne écrite**, la prise de verrou. Elle ne peut pas être fusionnée avec la lecture du dernier maillon : l'instantané d'une requête est pris avant l'attente du verrou, et la lecture ne verrait pas le maillon écrit par la transaction attendue. Le lot ajoute donc +1 requête à un encaissement et +3 à une clôture (orders, receipts, audit_logs), après deux fusions de lectures qui économisent une requête chacune.
- **Côté base, le travail baisse** (`EXPLAIN ANALYZE`, établissement le plus actif de staging) ; toutes les recherches passent par leur index :

| Chaîne | Avant | Après |
|---|---|---|
| `orders` | 106 ms | 0,73 ms |
| `payments` | 80 ms | 0,75 ms |
| `audit_logs` | 14 ms | 3,5 ms |
| `receipts` | 3 ms | 0,09 ms |
| `cash_registers` | 0,3 ms | 0,05 ms |

  Le gain croît avec la taille des tables, et donc davantage en production qu'à staging.
- **Le critère de la phase 5 (« p95 pas pire qu'avant ») n'est pas atteint tel quel sur ce banc**, qui grossit chaque requête de 21 ms. Entre l'API et la base hébergées dans la même région (hypothèse à confirmer par Ilies), un aller-retour coûte de l'ordre de la milliseconde : +1 à +3 ms d'allers-retours contre −80 à −120 ms de recherches.
- **Option si l'on veut zéro requête de plus, même à forte latence :** une table de têtes de chaîne, où une seule instruction `UPDATE … RETURNING` verrouille et lit le dernier maillon. Elle demande une migration et une reprise des têtes existantes. Elle n'est pas recommandée tant que l'API et la base sont proches.
- **Décision (Ilies, 2026-10-07) :** l'API et la base de production sont dans la même région Render. Le critère de performance est considéré comme rempli, et la table de têtes de chaîne n'est pas retenue.

### Phase 4 — Plateformes dans la chaîne (2026-10-07)

**Périmètre réel :** 4 chemins Uber Eats et la livraison externe.
- **Deliveroo n'a rien à corriger :** `UpdateOrderRejected` et `DisablePayments` du webhook Deliveroo n'ont **aucun appelant** (code mort laissé en place). Les refus et annulations Deliveroo passent par `DeleteOrder`, déjà chaîné.

**Mise en œuvre :**
- **`fiscal.SealOrderClosure` est partagée** par les clôtures caisse et plateformes. `order_life_cycle` l'appelle aussi, ce qui supprime sa copie locale.
- **Nouvelles fonctions dans `internal/fiscal` :**
  - `LockOpenOrdersByBrandOrderID` : verrouille les commandes encore ouvertes d'une commande plateforme ;
  - `OrderFullyPaid` ;
  - `SealColumns` et `Args` : fragment SQL commun des clôtures scellées.
- **Chaque chemin, dans une seule transaction :**
  - les commandes déjà closes reçoivent la mise à jour de statut d'avant, sans changer d'état ni de date de clôture, et sans rechaînage ;
  - les commandes ouvertes reçoivent les mêmes colonnes métier qu'avant, plus la clôture scellée.
- **Uber, réconciliation (`ubereats.SyncOrderState`, `HandleOrderNotFound`) :**
  - vente = `COMPLETED`, `EN_ROUTE_TO_DROPOFF` (`HANDED_OFF`), ou `READY_FOR_HANDOFF` pour une commande introuvable ;
  - la vente doit être entièrement payée : sinon la commande reste ouverte et l'erreur est journalisée (décision d'Ilies) ;
  - elle reçoit son ticket par `SaleReceiptIssuer` (`OrdersLifeCycleService.HandlerFiscalReceiptGeneration`), injecté dans `routes.go` (le module `ubereats` ne peut pas importer `order_life_cycle`) ;
  - sans émetteur branché, la clôture d'une vente échoue et rien n'est écrit, plutôt que de sortir une vente sans ticket ;
  - `DENIED`, `CANCELED` et `DELIVERY_FAILED` sont des annulations : clôture chaînée sans ticket.
- **Uber, webhooks (`CancelOrder`, `MarkFailed`) :**
  - clôture chaînée sans ticket ;
  - `CancelOrder` désactive toujours les paiements (lot B), mais dans la même transaction que la clôture.
- **`SetDeliveredExternal` :** une commande déjà close ne fait plus rien. Avant, elle était reclôturée (empreinte et date réécrites) et recevait un second ticket : 2 commandes Uber à staging en portaient deux.

**Écarts de comportement assumés :**
- **Annulations plateformes :** elles reçoivent désormais un `delivered_on` (date de clôture), comme les annulations caisse. Une ligne chaînée en a besoin, pour l'empreinte et pour l'ordre de la chaîne.
- **`MarkFailed` et réconciliation** ne réécrivent plus `state` ni `delivered_on` d'une commande déjà close (le statut, oui).
- **Non traité :** `SyncOrderState` vers un état ouvert (`ACCEPTED`) peut toujours rouvrir une commande close. Cela relève de C1 (lot B).

**Tests (contre staging, verts) :**
- `TestUberReconciliation_FiscalChain_Postgres` couvre six cas :
  - vente payée : clôture scellée, empreinte reproduite depuis la base, un ticket ;
  - vente non payée : reste ouverte ;
  - pas d'émetteur : échec et rien d'écrit ;
  - annulation chaînée sur la vente : `PLATFORM`, `delivered_on` posé ;
  - commande déjà close : statut seul, scellement et date inchangés ;
  - aucun ticket en dehors de la vente.
- `TestOrdersRepository_Postgres` (webhook Uber) : `CancelOrder` et `MarkFailed` scellés et chaînés ; une commande déjà close n'est ni modifiée ni scellée.
- `TestSetDeliveredExternal_AlreadyClosed_NoOp_Postgres` : un seul ticket, empreinte et date inchangées.
- **Tests existants `ubereats` adaptés** à la nouvelle signature : la vente du grand test porte désormais son paiement `UBER_EATS`, comme une vraie commande Uber. `TestUberEatsRepository_Postgres` dépasse ces sections et échoue plus loin, au contrôle `DisableIntegration` déjà en échec avant le lot.
- **Régression :** unitaires (hors les 4 préexistants) et intégration de tous les paquets touchés (`order_life_cycle`, `receipt`, `audit`, `cash_registers`, `delivery_sessions`, `pos/accounting`, `tasks`, `customers`, `haccp`, `fiscal`, tous les webhooks) : verts.

### Bilan du lot A

- **C3, C5 et C10 sont traités.** Constats annexes corrigés en route :
  - redémarrages de chaîne ;
  - lignes de Z dupliquées ;
  - second ticket des livraisons Uber ;
  - clôture sans ticket depuis la tâche planifiée.
- **Migrations 168 et 169 :** appliquées sur staging. En production, lancer d'abord la requête de contrôle des doublons de tickets (en tête de la 169), puis les appliquer **avant** de déployer le code.
- **Rien n'est commité.**
