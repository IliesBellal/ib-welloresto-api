# PROMPT — Conformité caisse, lot D : archive fiscale mensuelle

**Date :** 2026-10-08
**Références :**
- [plan anticipé D, E, F](attestation-conformite-04-plan-D-E-F.md) ;
- [feuille de route](attestation-conformite-feuille-de-route.md), décision S4 ;
- [audit](attestation-conformite-00-audit.md), constat C7 et son arbitrage ;
- BOI-TVA-DECLA-30-10-30 §50 (données à conserver) et §220 à §250 (archivage).

**Prérequis :** lots A, B, C (clôtures scellées, chaînes fiscales, journal des annulations).

**Objectif :** pour chaque établissement et chaque mois clos, produire automatiquement une archive des données fiscales ligne par ligne, en format ouvert avec une notice en français. Elle est figée, scellée et tracée, et téléchargeable depuis le back-office pendant au moins 6 ans.

## Règle d'or

Reprise des lots précédents :
- aucune régression de performance : l'archive se génère hors des écritures de caisse ;
- aucune donnée existante modifiée ;
- migrations écrites, jamais appliquées ;
- une phase, un point d'arrêt ;
- commits atomiques, sur accord d'Ilies.

## Exigences (BOI)

- **§50**, ligne par ligne :
  - numéro du justificatif, date à la minute, numéro de caisse, TTC ;
  - détail des articles (libellé, quantité, prix unitaire, **total HT de la ligne**, taux de TVA) ;
  - règlements ;
  - traces de modifications et de corrections ;
  - données de traçabilité et d'intégrité.
- **§220** : périodicité au plus annuelle ; l'archive fige les données et leur donne date certaine ; dispositif d'intégrité dans le temps.
- **§230** : format ouvert, notice explicative en français, lisible sans contrainte par l'administration.
- **§240** : traçabilité de la génération, conservée.
- **§250** : l'éditeur fournit la fonction de génération ; l'utilisateur conserve 6 ans.

## Phases

| Phase | Contenu |
|---|---|
| 0 | Recensement (ci-dessous), format proposé, décisions |
| 1 | Ticket figé complet (constat 2), options payantes comprises (migration 173) |
| 2 | Génération de l'archive, chaîne `fiscal_archives` (migration 174) |
| 3 | Tâche horaire, rattrapage des mois passés compris (sans commande dédiée, voir le journal) |
| 4 | Routes et back-office (liste, téléchargement, empreinte) |
| 5 | Mesures et contrôle croisé archive / clôtures |

## Journal

### Phase 0 — Recensement (2026-10-08, revu le même jour)

Aucun code écrit. **Analyse faite sur le code seul** : les données de staging ne sont pas propres et ne servent ni de mesure ni d'indication (correction d'Ilies ; une première version de ce recensement s'appuyait sur des comptages de staging, retirés).

#### 1. Sources des données du §50

| Donnée §50 | Source | Remarque |
|---|---|---|
| numéro du justificatif, date, TTC, HT | `receipts` (`receipt_number`, `created_at`, `total_ttc`, `total_ht`) | numérotation continue par établissement (lot A) |
| TVA par taux | `receipts.tax_details` | renseigné depuis le lot B ; `{}` avant |
| détail des articles | `receipts.items_snapshot` | **incomplet, voir le constat 2** |
| numéro de caisse | aucun sur le ticket | voir le constat 3 |
| règlements | `payments` (moyen, montant, type vente / remboursement, date, utilisateur, registre, état) | annulations tracées depuis le lot C |
| traces de modifications | `audit_logs` (`ORDER_UPDATE`, `ORDER_CLOSE`, `ORDER_DELETE`, `ORDER_REOPEN`, `ORDER_REFUND`, `PAYMENT_ADDED`, `PAYMENT_CANCELLED`) | état avant et après, chaîné et signé |
| intégrité | empreintes et signatures de chaque ligne ; `fiscal_closures` (jours, mois, année, grand total, total perpétuel, empreintes des commandes) | |
| clôtures de registre (Z) | `cash_registers`, `cash_registers_items` | |

#### 2. Le détail figé des tickets est incomplet

**Ce que fait le code :**
- les lignes figées d'un ticket (`receipts.items_snapshot`) sont construites par `utils/receipt.BuildItemsSnapshot` à partir des articles de la commande. Pour chaque article : libellé, quantité, `price_ttc` = `orderitems.price`, taux, TVA unitaire ;
- **les suppléments** sont enregistrés à part (table `extra`, avec leur prix, rattachés à la ligne) et ne sont pas lus par le builder ;
- **les frais de livraison** (`orders.delivery_fees`) n'y figurent pas ;
- **les remises de caisse** (paiements à moyen « remise », `models.DiscountMOPsSQL`) n'y figurent pas ;
- or la TVA ventilée du même ticket (`GetOrderTaxLines`, lot B) compte « (prix + suppléments) × quantité », les frais de livraison non nuls et les remises de caisse.

**Conséquence, par construction :** dès qu'une commande a un supplément, des frais de livraison ou une remise de caisse, la somme des lignes figées de son ticket ne fait pas son total. Le total et la TVA par taux restent justes. Ce sont des cas courants (pizza avec suppléments, livraison, remise).

- **Archive :** le §50 exige le détail des articles avec le **total HT de chaque ligne**, ce que ces lignes ne permettent pas.
- **Ticket imprimé (C8, lot E) :** imprimer « à partir du ticket figé » reproduirait ce détail incomplet.

**Second point, également lu dans le code :** le TTC du ticket est celui de la commande (`orders.price`), transmis par la caisse. Le recalcul serveur des totaux est désactivé depuis le 2026-09-19 (`PrepareCreateOrder`). Rien ne garantit donc que ce TTC égale la somme des lignes et de la TVA ventilée.

**Proposition (phase 1) : ticket figé complet pour les nouveaux tickets.** Format `items_snapshot` v2, construit à la clôture à partir des mêmes données que la TVA ventilée :
- une ligne par article : libellé, quantité, prix unitaire TTC, taux, TVA et HT de la ligne ;
- ses suppléments en lignes rattachées (libellé, prix, quantité) ;
- une ligne « Frais de livraison » si elle n'est pas nulle ;
- une ligne par remise de caisse (montant négatif) ;
- garantie : somme des lignes = somme de la TVA ventilée ; somme des HT = HT ventilé (le test le vérifie) ;
- **écart avec le TTC de la commande** : le ticket ne peut pas être refusé (la vente est faite). L'écart est consigné dans le ticket et journalisé, pour être visible au contrôle (lot E). Le traitement exact se décide en phase 1.

Les tickets existants ne sont jamais modifiés.
- L'archive les exporte tels quels, en indiquant leur format.
- Elle ajoute, pour toutes les commandes du mois, les **lignes de commande** (`orderitems` et suppléments). Pour une commande close et scellée, ce sont les lignes réellement vendues.
- **Effet sur la reclôture (lot C)** : un ticket ancien format comparé au nouveau diffère toujours, donc une commande rouverte sur cette période reçoit un avoir puis un nouveau ticket. C'est correct, juste prudent.

#### 3. Numéro de caisse

Les tickets ne portent pas de registre.
- Arbitrage C11 : « le canal d'encaissement fait office de numéro de caisse ».
- L'archive donne pour chaque ticket le **canal** : `orders.order_source`, écrit à la création par `resolveOrderSource` (caisse, borne, ScanNOrder, Uber Eats, Deliveroo). Il vaut NULL quand l'auteur n'est pas reconnu ; l'archive retombe alors sur la marque (`orders.brand`).
- Elle donne aussi le **registre de la commande** quand il existe : seules les commandes de la caisse en ont un à la création.
- Chaque paiement garde son propre registre.

#### 4. Volumes

**Pas de chiffre tiré de staging.** Ce que le code dit de la taille :
- **tickets, lignes, paiements, clôtures :** proportionnels au nombre de commandes du mois ;
- **journal d'audit :** chaque mutation de commande passée par `ExecuteOrderMutation` (clôture, mise à jour, ajout de paiement, réouverture, remboursement, annulation) écrit l'**état complet de la commande avant et après** en JSON. C'est, de loin, la plus grosse partie de l'archive : plusieurs entrées par commande, chacune avec deux instantanés complets.

**Mesure prévue en phase 5**, sur un mois **généré** par un test (plusieurs milliers de commandes avec suppléments, paiements et corrections) : durée de génération, taille brute et compressée. On pourra aussi lancer en production une requête agrégée, en lecture seule, que je fournirai (nombre de commandes et d'entrées d'audit par établissement et par mois, sans donnée personnelle). La génération se fait hors des écritures de caisse, une fois par établissement et par mois.

**Journal d'audit dans l'archive :** seulement les entrées des ressources fiscales (`orders`, `payments`). Le planning, l'HACCP et les rôles n'en font pas partie.

#### 5. Infrastructure réutilisable

- **R2 privé** : `r2.Client.UploadPrivateFile` et `GenerateSignedURL` (liens d'une heure), déjà utilisés par les exports comptables (migration 166 : table, SHA-256, lien signé).
- **Tâche horaire** `RunFiscalClosures` (lot B) : l'archive du mois M se génère au premier passage qui suit l'écriture de la clôture mensuelle de M. `TasksManager` n'a pas encore de client R2, il faudra l'injecter.
- **Chaîne fiscale** : nouvelle chaîne `fiscal_archives`, même scellement (v2 signé) et même verrou par établissement que les autres. Écrite seule dans sa transaction, sans autre chaîne.
- **Back-office** : les exports comptables ont déjà une liste et un téléchargement (`AccountingExportDialog`, `CashRegisterHistory`), modèles d'une page « Archives fiscales ».

#### 6. Format proposé de l'archive

`WelloResto_archive_<SIRET>_<AAAA-MM>.zip` :

| Fichier | Une ligne par | Colonnes principales |
|---|---|---|
| `tickets.csv` | ticket ou avoir | numéro, date et heure (locale et UTC), type (vente / avoir), commande, canal, registre, TTC, HT, TVA, format des lignes, empreinte, empreinte précédente, signature |
| `tickets_lignes.csv` | ligne de ticket figée | numéro du ticket, rang, libellé, quantité, prix unitaire TTC, taux, TVA, HT (calculés si l'ancien format ne les porte pas) |
| `tickets_tva.csv` | taux de TVA d'un ticket | numéro, taux, TTC, HT, TVA, remise |
| `commandes.csv` | commande close dans le mois | commande, numéro, type, canal, statut, dates de création et de clôture, prix, HT, TVA, registre |
| `commandes_lignes.csv` | ligne de commande et supplément | commande, ligne, libellé, quantité, prix, taux figé, remise |
| `paiements.csv` | paiement | id, commande, date, moyen, montant, type (vente / remboursement), utilisateur, registre, actif, empreinte, signature |
| `journal.csv` | entrée d'audit `orders` / `payments` | id, date, action, ressource, utilisateur, état avant (JSON), état après (JSON), empreinte, signature |
| `clotures.csv` | clôture journalière, mensuelle (et annuelle en décembre) | période, totaux, TVA par taux, règlements, canaux, grand total, total perpétuel, empreinte, signature |
| `registres.csv` | registre fermé dans le mois | id, ouverture, fermeture, fonds, lignes du Z, empreinte |
| `NOTICE.txt` | — | notice en français : contenu, colonnes, unités, vérification des empreintes |
| `MANIFEST.json` | — | établissement, période, version du logiciel, date de génération, SHA-256 de chaque fichier |

**Conventions** :
- UTF-8 avec BOM (lisible directement dans un tableur), séparateur `;` ;
- montants en centimes et en euros ;
- dates ISO 8601, en heure locale et en UTC ;
- JSON du journal dans une colonne.

**Rattachement au mois** : jour local de l'établissement, comme les clôtures (S5, S7) :
- tickets et avoirs selon leur date ;
- paiements selon leur date ;
- commandes selon leur clôture ;
- journal selon sa date.

#### 7. Décisions à confirmer

| # | Question | Recommandation |
|---|---|---|
| D1 | Format | CSV + notice, comme ci-dessus — **validé** |
| D2 | Horodatage par un tiers (RFC 3161) | Non pour l'instant : la chaîne signée suffit — **validé** |
| D3 | Génération à la demande d'une période | Oui, en plus de l'automatique, inscrite dans la même chaîne — **validé** |
| D4 | Second support | **à trancher**, voir ci-dessous |
| D5 | Rattrapage | Depuis le premier mois qui a des tickets (mars 2026) — **validé** |
| D6 | Ticket figé complet (constat 2) | Oui, en phase 1, nouveaux tickets seulement — **validé** |
| D7 | Journal dans l'archive | Ressources fiscales seulement (`orders`, `payments`) — **validé** |

**D4, second support.**
- Le BOI (§250) dit que « plusieurs supports de stockage différents pour une même archive peuvent être proposés », pour plus de sécurité. C'est **facultatif**.
- C'est le restaurateur qui doit conserver ses archives 6 ans. Si elles ne vivent que dans le stockage de WelloResto, il dépend de WelloResto pour les produire.
- Un « second support » serait une copie hors de WelloResto. Exemples :
  - e-mail mensuel au gérant, qui l'invite à télécharger l'archive du mois (lien vers la page du back-office) ;
  - ou l'archive jointe à l'e-mail, si sa taille le permet.
- Options :
  - **(a)** rien de plus que le téléchargement dans le back-office ;
  - **(b)** e-mail mensuel avec lien ;
  - **(c)** e-mail mensuel avec l'archive jointe.
- **Recommandation : (b)**. Peu coûteux, il rappelle chaque mois au restaurateur de garder une copie, sans les limites de taille des pièces jointes.

**Point d'arrêt.**

### Décisions d'Ilies après la phase 0 (2026-10-08)

D1, D2, D3, D5, D6, D7 validés. **D4 : (a)**, rien de plus que le téléchargement dans le back-office.

### Phase 1 — Ticket figé complet (2026-10-08)

**Constat supplémentaire, lu dans le code de la caisse et de l'API : le surcoût des options payantes n'était enregistré nulle part.**
- La caisse facture chaque ligne `(prix unitaire + options choisies + suppléments) × quantité` (`cart_manager._getLocalLinePrice`). Elle envoie le surcoût de chaque option choisie (`extra_price`) avec la commande.
- L'API n'en gardait que l'option et sa quantité (`order_item_configuration`). Le prix restait au catalogue, modifiable.
- Le ticket et sa TVA ventilée (lot B), comme l'export comptable et les rapports, comptent `(prix + suppléments) × quantité` : **le surcoût des options en était absent**, alors que le client l'a payé et qu'il est dans `orders.price`.
- **Uber Eats** porte aussi le prix de ses modificateurs (`modifier_mapper` : `ExtraPrice` = prix Uber), lui aussi ignoré.

**Ce qui est fait :**
- **Migration 173** ([173_order_item_configuration_extra_price.up.sql](../migrations/todo/173_order_item_configuration_extra_price.up.sql)) : `order_item_configuration.extra_price`, surcoût unitaire figé à l'écriture. NULL avant : les lectures retombent sur le catalogue. Aucune ligne existante modifiée. **À appliquer avant le code.**
- **Prix figé à l'écriture** (`freezeOptionPrice`, création et mise à jour de commande) :
  - **Uber Eats, Deliveroo :** le prix de la plateforme porté par la commande, jamais notre catalogue (même nul) ;
  - **caisse, borne, ScanNOrder :** le prix porté par la commande (la caisse envoie celui qu'elle a facturé ; la borne et ScanNOrder sont tarifés par le serveur), à défaut le catalogue du moment ;
  - le prix du catalogue est lu par la requête qui lisait déjà le coût de revient des options : aucun aller-retour de plus.
- **Ticket complet** (`fiscal.BuildReceiptItems`, `receipt.GetOrderSaleLines`) : à la clôture, le détail de la vente est relu en **une requête**, qui remplace celle de la TVA ventilée (même nombre d'allers-retours). Il comprend articles, options payantes, suppléments, frais de livraison et total des remises de caisse. Il donne :
  - les lignes figées : une par article (avec ses options et suppléments rattachés, `parent`), une pour la livraison, une remise par taux ; chacune avec `kind`, `total_ttc`, `total_ht`, `total_tva` ;
  - la TVA ventilée, calculée comme avant, **options comprises** désormais.
  - **Au centime**, à chaque taux : somme des TTC des lignes = TTC ventilé ; somme des HT des lignes = HT ventilé (HT réparti au plus grand reste).
  - Les champs historiques (`name`, `quantity`, `price_ttc`, `tax_rate`, `tax_amount`) gardent leur sens : la facture PDF les lit sans changement, et liste désormais aussi options, suppléments, livraison et remises.
- **Écart entre le TTC de la commande et les lignes :** journalisé (`Warn`). Le ticket est émis avec le TTC de la commande : la vente est faite, il ne peut pas être refusé. L'écart reste visible au ticket, son total différant de ses lignes, et sera signalé par le contrôle d'intégrité (lot E).
- `receipt.BuildItemsSnapshot` (ancien builder, prix de l'article seul) est supprimé. `GenerateFiscalReceipt` ne reçoit plus les lignes de l'appelant.

**Tests :**
- unitaires, verts :
  - `fiscal.TestBuildReceiptItems_*` : vente complète (deux taux, option, supplément, livraison, remise), sans remise, remise absorbant tout, ligne négative ; sommes exactes par taux et `TTC = HT + TVA` sur chaque ligne ;
  - `order_life_cycle.TestFreezeOptionPrice` : règle de prix par canal ;
- intégration (résultats plus bas, migration 173 appliquée) :
  - `receipt.TestReceiptCompleteLines_Postgres` : option figée, option sans prix figé (catalogue), option gratuite absente, supplément, livraison, remise sur deux taux, ordre et rattachement des lignes, écart de TTC journalisé ;
  - à relancer aussi : `TestReceiptTaxDetails`, `TestReceiptReclose`, les suites `order_life_cycle`, puis les mesures (la clôture lit désormais une requête plus large).

**Effet sur les autres lecteurs de la TVA :** l'export comptable (mode manuel), les rapports TVA, la TVA du registre et les analytics ignorent toujours le surcoût des options : ils lisent les lignes, pas les tickets. Les clôtures fiscales (lot B) lisent les tickets, donc les options y entrent à partir de ce lot. **À aligner en dehors du lot D** (voir la question suivante) : sinon, l'export comptable restera inférieur au ticket pour toute vente avec option payante.

#### Prix calculé par la caisse : certification et recalcul serveur

**Qui calcule le prix, aujourd'hui (lu dans le code) :**

| Canal | Prix des lignes et TTC |
|---|---|
| Caisse | **calculés par l'application** (catalogue local), enregistrés tels quels (`insertOrderBase`, `insertOrderItems`) |
| Borne | calculés par le serveur (`OrdersService.ComputePricing`) |
| ScanNOrder | vérifiés contre le catalogue serveur |
| Uber Eats, Deliveroo | prix de la plateforme, qui a encaissé |

**Faut-il certifier la caisse à part ? Non.**
- L'attestation porte sur le **système de caisse WelloResto** : application de caisse et API, un seul éditeur.
- Les quatre conditions (inaltérabilité, sécurisation, conservation, archivage) portent sur les données **une fois enregistrées**. Or elles sont scellées côté serveur, quel que soit le calcul du prix.
- Une caisse qui calcule ses prix n'appelle ni certification séparée ni attestation séparée. Elle fait partie du périmètre attesté : la version 2.x.x couvre l'application et l'API, et une version de caisse incompatible ne devrait pas pouvoir encaisser.

**Le vrai risque n'est pas la certification mais la cohérence du justificatif.** Un ticket dont le total ne correspond pas à ses lignes est incohérent au regard du §50. La TVA déclarée suit les lignes, l'argent encaissé suit le TTC. À un contrôle, un écart systématique se lit comme une minoration. C'était le cas, à l'insu de tous, pour les options payantes.

**Faut-il recalculer le prix côté serveur ? Oui, mais comme contrôle, pas en écrasant le prix.**
- **Écraser** le TTC de la caisse (`computeOrderTotals`, désactivé le 2026-09-19) est dangereux : les paiements suivent le TTC affiché et encaissé par la caisse. Une commande recalculée ne serait plus « entièrement payée » et ne pourrait plus être close. Le ticket contredirait ce que le client a vu et payé.
- **Refuser** une commande dont le total diverge casserait les versions actuelles de la caisse : contraire à la règle de compatibilité.
- **Recommandation :**
  1. **Lancer d'abord en production la requête** [diagnostic-prix-options-lot-D.sql](diagnostic-prix-options-lot-D.sql) (lecture seule, sans donnée personnelle). Elle mesure, par mois et par canal, combien d'écarts « prix ≠ lignes » disparaissent quand on compte les options. **Hypothèse tirée du code :** les « petits écarts » du diagnostic Croq'Ô'Pizzas d'août (`decisions.md`, 2026-09-19), attribués à la caisse, viennent au moins en partie des options payantes, que le serveur ne comptait pas. Les commandes à 0 € relèvent d'une autre cause.
  2. **Recalcul serveur en contrôle**, avec la **formule complète** (prix, options, suppléments, livraison), la même que celle de la caisse et du moteur de la borne (`ComputePricing`). Tout écart est journalisé, et le contrôle d'intégrité (lot E) le signale, commande par commande. Le TTC n'est jamais réécrit.
  3. **Corriger la source dans la caisse** pour les écarts qui restent (prix à 0, arrondis).
  4. **Aligner l'export comptable, les rapports et la TVA du registre** sur la même formule, options comprises, pour qu'ils égalent les tickets. C'est un chantier du périmètre comptable, pas du lot D ; il est à planifier avec la revue de fin d'année prévue pour `computeOrderTotals`.

#### Diagnostic en production (lancé par Ilies, 2026-10-08)

Requête [diagnostic-prix-options-lot-D.sql](diagnostic-prix-options-lot-D.sql), 12 derniers mois, ventes closes. Colonnes clés : écarts « prix ≠ lignes » avec la formule d'avant le lot D, écarts une fois les options comptées.

| Canal | Lecture |
|---|---|
| **ScanNOrder** | **100 % des écarts expliqués par les options**, tous les mois (ex. septembre : 26 sur 26 ; avec options : 0). |
| **Borne** | idem, à quelques commandes près (septembre : 84 écarts, 4 restants). |
| **Caisse** | depuis l'été, les options expliquent presque tout (août : 174 sur 186, il en reste 13 pour 151 € ; septembre : 110 sur 118, il en reste 17 pour 193,30 €). De décembre 2025 à février 2026, les écarts restants étaient nombreux (205 à 268 par mois), puis faibles depuis mars 2026 : une autre cause, corrigée depuis. |
| **Uber Eats** | 60 à 80 % des commandes en écart **sans** les options ; les compter **aggrave** l'écart et n'en explique aucun. |
| **Deliveroo** | écarts jusqu'en mars 2026 (aucune option), aucun depuis avril 2026 : corrigé entre-temps. |

**Conclusions :**
- **L'hypothèse est confirmée.** Pour la caisse, la borne et ScanNOrder, le TTC comprenait bien le surcoût des options, mais les lignes enregistrées non. Les « petits écarts » d'août (diagnostic Croq'Ô'Pizzas) relèvent de là. La migration 173 et le ticket complet ferment ce trou pour les nouvelles ventes.
- **Écarts résiduels de la caisse** : une dizaine à une vingtaine de commandes par mois, dont les commandes à 0 €. Ce sont eux que le contrôle serveur (formule complète, journalisé) et le contrôle d'intégrité du lot E doivent faire ressortir, commande par commande.
- **Uber Eats, lu dans le code** (`webhook/ubereats/service/order_mapper.go`) :
  - le TTC de la commande est la somme `prix unitaire Uber × quantité`, ou, s'il y a une promotion, le `sub_total_promo_applied` d'Uber ;
  - les modificateurs ne sont jamais ajoutés ;
  - la plupart des écarts sont donc des **promotions Uber**, qui baissent le TTC sous la somme des lignes ;
  - **on ne peut pas dire** si le prix unitaire Uber inclut déjà les modificateurs. La documentation Uber ne le précise pas ([référence de l'API v2](https://developer.uber.com/docs/eats/references/api/v2/get-eats-order-orderid)), et le paiement Uber enregistré reprend notre propre total.

**Décision prise dans cette phase, par prudence :** pas de ligne d'option sur les tickets **Uber Eats et Deliveroo**. Leur prix reste figé à l'enregistrement, donc rien n'est perdu. Compter deux fois les modificateurs serait pire que les omettre ; ce comportement est identique à celui d'avant. Les tickets de la caisse, de la borne et de ScanNOrder comptent leurs options.

**À trancher pour les plateformes (hors lot D, à proposer à Ilies) :**
- lire le `sub_total` d'Uber, absent de notre modèle (`UberPayment`), et le comparer à `Σ prix unitaire × quantité` sur de vraies commandes avec modificateurs : cela tranche la question du prix unitaire ;
- prendre le total Uber comme TTC plutôt que de le recalculer ;
- faire figurer la **promotion plateforme** sur le ticket, comme une remise ventilée par taux. Aujourd'hui, la TVA du ticket d'une commande Uber en promotion porte sur le montant avant promotion.

#### Résultats de la phase 1 (migration 173 appliquée sur staging par Ilies)

- **Tests d'intégration, staging :**
  - `receipt.TestReceiptCompleteLines_Postgres` : vente complète ; commande Uber Eats avec option, sans ligne d'option ;
  - `order_life_cycle.TestOrderLifeCycleRepository_OptionsExtrasCostFreeze_Postgres`, étendu : surcoût figé à l'écriture (catalogue faute de prix envoyé, prix envoyé conservé, inchangé quand le catalogue change) ;
  - suites `receipt`, `fiscal`, `order_life_cycle`, `cash_registers`, `pos/accounting`, `webhook/...`, `tasks` : vertes ;
  - échecs connus sans lien : `orders` (schéma staging), tests unitaires `planning`.
- **Mesures** (depuis le poste, aller-retour 20 ms) :
  - clôture séquentielle p50 307 à 318 ms (avant : 304 à 318) ;
  - 10 clôtures simultanées 4,19 à 5,11 s (avant : 4,02 à 4,22 ; le réseau était instable, un `SELECT 1` isolé a pris 462 ms) ;
  - la lecture des lignes de vente prend environ 25 ms, soit un aller-retour plus quelques millisecondes en base, comme la lecture de TVA qu'elle remplace ;
  - les index utiles existent (`orderitems.order_id`, `extra.order_item_id`, `order_item_configuration.order_item_id`).

### Décisions d'Ilies après la phase 1 (2026-10-08)

- **Uber Eats hors du lot D.** Les données Uber de production (réponse `get order` ou autre) seront analysées à part pour décider du traitement des modificateurs et des promotions.
- **Écarts de prix journalisés :** ils ne doivent rien bloquer. C'est le cas : `GenerateFiscalReceipt` écrit un `Warn` et émet le ticket. Ce journal part dans la sortie du processus (zap, configuration de production : JSON sur la sortie d'erreur), c'est-à-dire **dans les logs Render**, pas en base. L'écart reste aussi lisible dans le ticket lui-même (son total diffère de ses lignes) : le contrôle d'intégrité du lot E le retrouvera sans dépendre des logs.

### Phase 2 — Génération et scellement des archives (2026-10-08)

**Migration 174** ([174_fiscal_archives.up.sql](../migrations/todo/174_fiscal_archives.up.sql)) : table `fiscal_archives`, une ligne par archive générée, jamais modifiée.
- Colonnes : établissement, nature (`MONTH` automatique ou `PERIOD` à la demande), période, fuseau, nom de fichier, clé R2, SHA-256 du ZIP et du manifeste, taille, version du logiciel, auteur, date, chaînage (`previous_hash`, `hash`, `signature`).
- Une seule archive `MONTH` par établissement et par mois (index unique partiel).
- **À appliquer avant le code.**

**Chaîne `fiscal_archives`** (`internal/fiscal/seal.go`), écrite seule dans sa transaction. L'empreinte couvre établissement, nature, période, fuseau, nom, SHA-256 du ZIP et du manifeste, taille, version, auteur et date de génération (`fiscalarchive.Payload`). C'est ce chaînage signé qui fige l'archive et lui donne date certaine (§220) ; la table est le journal de génération (§240).

**Paquet `internal/fiscalarchive`** :
- `Generate(ctx, db, store, Request)` :
  1. contrôle que la période est close : chaque jour a sa clôture journalière, et un mois sa clôture mensuelle (`ErrPeriodNotClosed` sinon) ;
  2. pour un mois, renvoie l'archive existante (idempotent) ;
  3. lit toutes les données dans **une transaction en lecture seule, `REPEATABLE READ`** : un seul instantané pour tous les fichiers ;
  4. construit le ZIP en mémoire ;
  5. l'envoie au stockage privé sous une clé unique, jamais écrasée (`fiscal-archives/<établissement>/<AAAA-MM>/<horodatage>_<nom>`) ;
  6. dans une courte transaction : verrou de la chaîne, nouveau contrôle d'existence pour un mois (deux instances), insertion de la ligne scellée.
- `Build` : les 9 fichiers CSV du format validé en phase 0, `NOTICE.txt` et `MANIFEST.json` (SHA-256, lignes et taille de chaque fichier). CSV en UTF-8 avec BOM, séparateur `;`, CRLF, montants en centimes et en euros (virgule), dates locales et UTC.
- `List` : archives d'un établissement, la plus récente d'abord, pour le back-office (phase 4).
- **Nom du fichier :** `WelloResto_archive_<SIRET>_<AAAA-MM>.zip` ; à la demande : `…_<début>_<fin>_<horodatage>.zip`.
- **Rattachement :** les bornes de la période sont les jours locaux de l'établissement. Tickets, paiements et journal sont rattachés par leur date ; commandes et lignes de commande par leur date de clôture ; clôtures : jours de la période, plus le mois ou l'année qui s'y terminent ; registres par leur fermeture.
- Le surcoût d'une option est écrit tel que figé (vide s'il ne l'était pas encore, avant la migration 173), avec le prix du catalogue à côté.

**Paquet `internal/version`** (provisoire, fixé au lot F) : `Product = "WelloResto"`, `Version = "2.0.0-dev"`, injectable au build. Écrit dans chaque manifeste et chaque ligne d'archive.

**Tests :** `fiscalarchive.TestFiscalArchive_Postgres` (résultats ci-dessous). Il couvre :
- un mois réel : ventes, avoir, paiements, registre fermé, journal, clôtures écrites par `CloseDueDays` ;
- le contenu du ZIP (11 fichiers), le recalcul de chaque SHA-256 du manifeste, et le fichier stocké égal au SHA-256 et à la taille scellés ;
- le contenu des CSV : un libellé contenant `;` et des guillemets, l'avoir et son montant en euros, 30 jours plus le mois sans le 1er octobre ;
- le re-scellement à l'identique de la ligne depuis la base ;
- l'idempotence du mois (aucun second envoi), le refus d'un mois non clos ;
- une archive à la demande chaînée sur la précédente.

#### Tests et revue des risques (migration 174 appliquée sur staging par Ilies)

**Test d'intégration** `TestFiscalArchive_Postgres` : vert. Il couvre tout ce qui est décrit plus haut, ainsi que :
- un libellé commençant par `=`, protégé par une apostrophe dans le CSV ;
- **deux générations simultanées du même mois** : une seule archive, rendue aux deux appels ;
- trois archives chaînées sans fourche.

Corrigé en route : l'ordre des clôtures dans `clotures.csv` est désormais jours, puis mois, puis année (auparavant le mois s'intercalait après le 1er).

**Risques examinés et traités :**

| Risque | Traitement |
|---|---|
| **Mémoire sur un gros mois.** Le journal contient l'état complet des commandes avant et après chaque modification. | Construction **en flux** : chaque CSV s'écrit directement dans son entrée du ZIP compressé, son empreinte calculée au passage. Seuls les petits fichiers produits avec les tickets (lignes, TVA) passent par un tampon, le temps de l'étape. **Mesuré** (`TestFiscalArchiveBuildPerf_Postgres`, mois généré : 3 000 commandes et tickets, 6 000 paiements, 12 000 entrées de journal de ~7 Ko, soit **88,5 Mo de journal en clair**) : **pic du tas 7,5 Mo** (2,2 Mo avant), construction 7,2 s depuis le poste, dominée par le transfert de la base. Le ZIP final reste en mémoire pour l'envoi ; il pèsera quelques Mo avec de vraies données (le jeu généré se compresse bien plus). |
| **Deux instances génèrent le même mois.** | Index unique partiel ; nouveau contrôle sous le verrou de la chaîne avant l'insertion. Le second appel renvoie l'archive du premier ; son fichier envoyé reste orphelin, sans effet. Testé. |
| **Données incohérentes entre fichiers.** Une écriture survient pendant la génération. | Toute la lecture se fait dans une transaction en lecture seule, `REPEATABLE READ` : un seul instantané. |
| **Échec de l'envoi ou de l'insertion.** | Envoi raté : rien n'est écrit, nouvel essai au passage suivant. Insertion ratée après l'envoi : fichier orphelin sous une clé unique, jamais écrasé ; nouvel essai au passage suivant. |
| **Période non close** (un jour sans clôture). | Refus (`ErrPeriodNotClosed`) : une archive ne fige que des jours clos. Testé. |
| **Formules dans un tableur** (« injection CSV »). Les archives seront ouvertes par des comptables. | Les textes libres (libellés, commentaires) qui commencent par `=`, `+`, `-`, `@`, tabulation ou retour chariot sont précédés d'une apostrophe. La règle est rappelée dans la notice. Testé (`TestTextProtectsFormulas`). |
| **Lectures sans index adapté en production.** | Voir ci-dessous : migration 175 proposée. |

**Migration 175** ([175_fiscal_period_indexes.up.sql](../migrations/todo/175_fiscal_period_indexes.up.sql)), en `CONCURRENTLY` : `payments (merchant_id, payment_date)` et `orders (merchant_id, delivered_on) WHERE state = 'CLOSED'`.
- **Constat** (plans `EXPLAIN` sur staging, structure seulement) : les paiements d'un établissement sur une période sont lus par un **parcours complet de la table**. Le seul index établissement + date, `idx_payments_fiscal_chain_head`, est partiel (`WHERE hash IS NOT NULL`), donc inutilisable. Les commandes closes sont lues à partir de toutes les commandes de l'établissement.
- **Qui fait ces lectures :** la clôture journalière du lot B, une fois par jour et par établissement, et l'archive. Ce sont des tâches de fond, sans effet sur la caisse, mais leur coût grandit avec les données.
- **Coût des index :** quelques microsecondes de plus à l'insertion d'un paiement et à la clôture d'une commande.
- **Le code fonctionne avec ou sans.** À appliquer avant les rattrapages, qui en profitent le plus.

### Phase 3 — Tâche horaire et rattrapage (2026-10-08)

**`RunFiscalArchives`** ([internal/tasks/fiscal_archives.go](../internal/tasks/fiscal_archives.go)), toutes les heures, à côté de `RunFiscalClosures` :
1. Lit les **mois clos sans archive** : clôture mensuelle écrite, aucune archive `MONTH` pour ce mois (`fiscalarchive.PendingMonths`). Du plus ancien au plus récent, tous établissements, **actifs ou non** : un établissement désactivé garde ses obligations de conservation.
2. Génère chaque mois avec `fiscalarchive.Generate` (phase 2), un à la fois, auteur `SYSTEM` (`fiscalarchive.GenerateDue`).
3. S'arrête après **10 minutes** ; les mois restants attendent le passage suivant.
4. Un mois en échec est journalisé (niveau erreur, établissement et mois), puis retenté au passage suivant. Il ne bloque pas les autres.

**Une seule instance à la fois.** Chaque instance lance la tâche. La première prend un verrou consultatif sans attente (`pg_try_advisory_xact_lock`), tenu par une transaction ouverte le temps du passage ; les autres passent leur tour. Si la connexion tombe, Postgres relâche le verrou. `Generate` reste idempotent : le verrou évite seulement de construire et d'envoyer deux fois le même mois.

**Délai.** La clôture mensuelle s'écrit au premier passage horaire après minuit (heure locale) le 1er du mois. L'archive suit au même passage ou au suivant : **une heure au plus**.

**Stockage absent.** Si le client R2 privé n'a pas pu être créé au démarrage, la tâche le signale en erreur à chaque passage et ne fait rien d'autre. Le stockage est passé à la tâche comme interface nulle, pas comme pointeur nul typé, pour ne jamais paniquer.

**Pas de `cmd/backfill_fiscal_archives`** (prévu au plan, abandonné). La tâche rattrape d'elle-même tous les mois clos depuis mars 2026, une fois les clôtures rattrapées (`cmd/backfill_fiscal_closures`, lot B). Une commande aurait demandé la configuration R2 sur le poste qui la lance, pour le même résultat. L'ordre de mise en production change donc : plus d'étape « rattrapage des archives » ; il se fait au déploiement du code.
- **Durée du rattrapage**, estimée : quelques secondes par mois et par établissement (phase 2 : 7,2 s depuis le poste pour un mois très chargé, temps dominé par le transfert de la base). Une heure de passage absorbe des dizaines d'archives : le rattrapage depuis mars tient en quelques passages. Mesure réelle à la phase 5 et au déploiement (log `RunFiscalArchives: terminé`, champs `archives`, `reportes`, `duree`).

**Coût d'un passage sans travail :** une requête. Plan vérifié sur staging (structure seulement, aucune clôture n'y est écrite) : parcours des clôtures mensuelles, anti-jointure par l'index unique des archives (`uq_fiscal_archives_month`, parcours d'index seul), 0,1 ms. En production, les clôtures `MONTH` sont filtrées dans une table qui gagne environ 380 lignes par établissement et par an. Le parcours reste de l'ordre de la dizaine de millisecondes, une fois par heure et par instance. Aucun index ajouté.

**Tests** (verts sur staging, migration 174 appliquée) :
- `TestFiscalArchive_Postgres`, complété :
  - mois en attente dans l'ordre (août puis septembre ; octobre, non clos, absent) ;
  - échéance atteinte : rien n'est tenté ;
  - envoi impossible : deux échecs signalés, rien d'écrit, les deux mois restent en attente ;
  - après septembre, seul août reste ; à la fin, un passage ne fait rien.
- `TestTryLock_Postgres` (phase 4, `fiscal.TryLock`) : une seconde instance passe son tour tant que la première tient le verrou, puis l'obtient.
- Le point d'entrée réel n'est pas appelé en test : il parcourt tous les établissements de la base (même règle que les autres tâches, voir l'en-tête de `internal/tasks/postgres_integration_test.go`).

**Fichiers :**
- [internal/fiscalarchive/due.go](../internal/fiscalarchive/due.go) ;
- [internal/tasks/fiscal_archives.go](../internal/tasks/fiscal_archives.go) ;
- `TasksManager.FiscalArchiveStore` ([internal/tasks/manager.go](../internal/tasks/manager.go)) ;
- câblage dans [cmd/api/routes.go](../cmd/api/routes.go) et [cmd/api/tasks.go](../cmd/api/tasks.go).

### Phase 4 — Routes et back-office (2026-10-08)

**Routes** ([internal/modules/pos/accounting/fiscal_archives.go](../internal/modules/pos/accounting/fiscal_archives.go)). Elles sont placées dans le groupe `/accounting`, déjà gardé en entier par `reports.financial.read` (RBAC lot 8). Le motif du test de couverture RBAC reste valable sans modification.

| Méthode | Route | Effet |
|---|---|---|
| GET | `/accounting/fiscal-archives` | Archives de l'établissement, la plus récente d'abord : période, type, nom, empreintes du ZIP et du manifeste, taille, version, auteur, date, maillon de chaîne. Pas la clé R2. |
| POST | `/accounting/fiscal-archives` | Archive à la demande (`PERIOD`), `{"date_from","date_to"}`, bornes incluses. Auteur : l'utilisateur. Réponse 201. |
| GET | `/accounting/fiscal-archives/{archive_id}/download` | Inscrit la demande au journal d'audit, **puis** renvoie un lien signé d'une heure. |

**Choix :**
- **Journal des téléchargements.** Entrée `FISCAL_ARCHIVE_DOWNLOAD` (ressource `fiscal_archives`, identifiant de l'archive ; nom, empreinte et période dans `new_values`), chaînée et signée comme les autres. Si l'écriture au journal échoue, aucun lien n'est délivré : aucun accès sans trace. L'entrée n'entre pas dans `journal.csv` des archives, limité aux commandes et paiements (décision D7).
- **31 jours au plus** pour une archive à la demande. La génération est synchrone, dans la requête : son coût reste celui d'un mois automatique, déjà mesuré. Les périodes plus longues sont couvertes par les archives mensuelles. La borne se relève facilement (`fiscalArchiveMaxDays`) si un besoin réel apparaît.
- **Une génération à la fois par établissement.** Le verrou est pris sans attente (`fiscal.TryLock`, clé `fiscal:archives-on-demand:<établissement>`). Un double clic ou deux onglets reçoivent un refus explicite au lieu de construire deux fois. `fiscal.TryLock` remplace la fonction locale de la phase 3 ; la tâche horaire l'utilise aussi.
- **Erreurs** (ajoutées à `SendErrorJSON`, avec un message en français au premier niveau, comme au lot C) :

| Erreur | Statut HTTP | Cas |
|---|---|---|
| `fiscal_period_invalid` | 400 | dates illisibles, fin avant début, plus de 31 jours |
| `fiscal_period_not_closed` | 409 | un jour de la période sans clôture |
| `fiscal_archive_busy` | 409 | génération déjà en cours pour l'établissement |
| `fiscal_archive_storage_unavailable` | 503 | client R2 privé absent |

- **Archive d'un autre établissement** : 404, jamais visible dans la liste. Toutes les lectures filtrent sur l'établissement du jeton (`fiscalarchive.Get` / `List`).
- **Caisse Flutter** : rien. Les archives se consultent au back-office ; les versions actuelles de la caisse ne sont pas touchées.

**Back-office** (wello-back-office, [docs/archives-fiscales.md](../../wello-back-office/docs/archives-fiscales.md) de ce dépôt) :
- page **Comptabilité → Archives fiscales**, gardée par le même droit ;
- explication, génération d'une période (bornée à 31 jours côté page aussi), liste avec empreinte SHA-256 copiable, téléchargement.
- Fichiers neufs, plus deux retouches (`App.tsx`, `navConfig.ts`). Les fichiers qui portent d'autres travaux en cours ne sont pas touchés. Type-check et lint propres sur ces fichiers ; le dépôt a par ailleurs des erreurs TypeScript antérieures, sans rapport.

**Tests** (verts sur staging) :
- `TestFiscalArchivesRoutes_Postgres` :
  - trois périodes invalides, une période non close, stockage absent, génération déjà en cours : refusés, aucun envoi ;
  - génération (auteur, type, borne) ; liste ;
  - lien, avec une entrée de journal scellée au nom de l'utilisateur ;
  - un autre établissement ne voit pas l'archive, ni par la liste ni par le lien.
- `TestTryLock_Postgres` (remplace le test de la phase 3).
- `TestFiscalArchive_Postgres`, et les tests unitaires de `cmd/api` (couverture RBAC), `models`, `accounting`, `tasks`, `fiscal`, `fiscalarchive`.

**Documentation des droits** : [RBAC_ROUTES.md](RBAC_ROUTES.md), section `/accounting`.

### Phase 5 — Mesures et contrôle croisé (2026-10-08)

**Contrôle croisé** : `fiscalarchive.Verify` ([internal/fiscalarchive/verify.go](../internal/fiscalarchive/verify.go)). Il relit un ZIP sans accès à la base et contrôle :
1. **Le manifeste.** Chaque fichier listé est présent, avec son empreinte, sa taille et son nombre de lignes. Aucun fichier n'est hors manifeste. Le calcul se fait en flux : le journal n'est jamais chargé en entier.
2. **Chaque ticket au format complet.** La somme de ses lignes (TTC et HT) égale sa ventilation de TVA.
3. **Chaque clôture journalière.** Ventes, avoirs (TTC et HT), nombre de tickets, premier et dernier numéro sont recalculés depuis les tickets du jour, de la même façon que la clôture : ventilation de TVA du ticket si elle existe, son total sinon. Un ticket daté d'un jour sans clôture est signalé.
4. **Chaque clôture mensuelle** dont tous les jours sont dans l'archive : elle égale la somme de ses jours. Un mois partiel, dans une archive à la demande, n'est pas recoupé.

Les écarts vont dans un rapport (50 premiers en clair, nombre total compté) ; seule une archive illisible renvoie une erreur.

**Branché à la génération.** `Generate` vérifie chaque ZIP juste après sa construction ; le rapport est dans `Archive.Check`.
- Un écart **n'empêche pas** l'archive : elle fige les données telles qu'elles sont.
- La tâche horaire journalise l'écart en erreur (`RunFiscalArchives: contrôle croisé de l'archive en écart`, avec l'établissement, le mois et les anomalies), à examiner par la vérification d'intégrité du lot E.
- En fonctionnement normal, aucun écart n'est attendu. Un ticket ne s'écrit jamais dans un jour déjà clos : il est daté de son écriture, et la journée n'est close que le lendemain.

**Tests** (verts sur staging) :
- `TestFiscalArchive_Postgres` :
  - septembre sans écart (10 fichiers, 3 tickets, 30 jours, 1 mois), contrôle de génération compris ;
  - archive à la demande sans écart (3 jours ; mois partiel non recoupé) ;
  - **un montant de ticket retouché dans `tickets.csv`** : empreinte différente du manifeste ;
  - **une TVA de ticket retouchée dans `tickets_tva.csv`** : écart avec les lignes du ticket et avec la clôture du jour.
- Suites d'intégration `fiscal`, `fiscalarchive`, `accounting`, `receipt`, `order_life_cycle` : vertes.
- Tests unitaires de `internal/...` sans base : seuls échouent les tests déjà en échec avant le lot (planning, Uber Eats BYOC). Avec `DB_DIALECT=postgres` exporté, un test sqlmock d'`order_life_cycle` échoue aussi ; il passe sans la variable (défaut connu).

**Mesures** (depuis le poste, base de staging distante ; les temps de lecture sont dominés par le réseau) :

| Mesure | Résultat |
|---|---|
| Construction d'un mois très chargé : 3 000 tickets, 6 000 paiements, 12 000 entrées de journal de ~7 Ko (88,5 Mo de journal en clair) | 7,2 à 9,3 s selon le passage ; **pic du tas 7,4 Mo** (2,2 Mo avant) ; ZIP de 0,4 Mo (jeu généré très compressible) |
| Contrôle croisé du même ZIP | **155 ms** |
| Passage horaire sans travail (mois en attente) | une requête, **0,1 ms** sur staging (aucune clôture) ; de l'ordre de la dizaine de millisecondes à terme, une fois par heure et par instance |
| Requête des lignes de vente d'un ticket (phase 1) | une requête, ~25 ms (un aller-retour), à la place de deux lectures |
| Clôture de commande, ticket compris (phase 1) | p50 307 à 318 ms, **inchangé** |
| Création de commande (prix d'option figé) | **aucun aller-retour de plus** : `extra_price` est écrit par l'insertion existante, depuis le catalogue déjà lu pour le coût |

**Ce qui reste à mesurer en conditions réelles** (au déploiement, puis dans la vérification avant production) :
- la durée de construction sur Render, au plus près de la base ;
- la durée du rattrapage depuis mars 2026 (log `RunFiscalArchives: terminé`, champs `archives`, `reportes`, `duree`) ;
- la taille des archives réelles.

## Bilan du lot D (2026-10-08)

**Constat C7 traité** (archivage fiscal, BOI §220 à §250) :
- une archive par mois clos et par établissement, automatique, rattrapage compris ;
- des archives à la demande d'une période close (31 jours au plus) ;
- CSV ouverts ligne par ligne, notice en français, manifeste des empreintes ;
- ligne scellée dans la chaîne signée `fiscal_archives` ;
- stockage R2 privé, liste et téléchargement au back-office, chaque lien tracé au journal d'audit ;
- contrôle croisé à chaque génération.

**Ticket figé complet** (constat 2 de la phase 0) : options, suppléments, frais et remises, totaux par ligne. Le surcoût des options est figé à la commande (migration 173).

**Migrations** :
- **173** (`order_item_configuration.extra_price`) et **174** (`fiscal_archives`) : appliquées sur staging ;
- **175** (index par période, `CONCURRENTLY`) : **pas encore appliquée**. Le code fonctionne sans elle ; elle est à jouer avant le rattrapage en production.

**Ordre de mise en production** : voir la [feuille de route](attestation-conformite-feuille-de-route.md). Plus d'étape de rattrapage des archives : la tâche horaire s'en charge après le déploiement du code.

**Hors lot, à reprendre :**
- Uber Eats : faut-il des lignes d'options sur ses tickets ? Décision après analyse des données de production (réponse « get order »).
- Recalcul serveur du prix comme contrôle seulement : l'écart TTC / lignes est déjà journalisé (`Warn`), sans blocage.
- Alignement de l'export comptable, des rapports et de la TVA des registres sur les options payantes.
- Purge des fichiers orphelins de R2 : un envoi suivi d'une insertion ratée, ou d'un mois écrit par une autre instance. Ils sont sans effet, mais occupent de la place.
