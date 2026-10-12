# Écart TVA / encaissements des registres : les ventes borne

Constat du 2026-10-12, correctif et rattrapage le même jour (non commités, non
déployés). Rédigé au fil des décisions.

## Le constat

Un export multiple de 10 registres de production (PDF, onglet registres du
back-office) montrait, registre par registre, un écart entre le total de la TVA et
le total des encaissements, presque toujours égal aux encaissements de borne —
alors que chaque registre affichait 0 € d'écart à sa clôture.

## La cause

Deux critères de rattachement différents pour deux moitiés du même registre :

| Moitié | Lue sur | Contient la vente borne ? |
|---|---|---|
| Encaissements (théorique figé, `cash_registers_items`, relevé) | `payments.cash_register_id` | oui — le paiement `KIOSK` est requalifié vers le registre qui se ferme (`CloseCashRegister`, étape 3bis) |
| TVA (ticket Z, PDF du registre) | `orders.cash_register_id` | **non** — une commande de borne naît à `'KIOSK'` (`kiosk/service.go`) et n'en change jamais |

L'écart de 0 € de la clôture compare le relevé au théorique, deux lectures des
paiements : il ne voit pas l'asymétrie. Le rapport comptable mensuel n'était pas
touché (rattachement par date de commande, pas par registre).

Même mécanisme pour les commandes de borne « payer en caisse » : le caissier
encaisse sur son registre, la commande reste à `'KIOSK'`. Les canaux ScanNOrder,
Uber Eats et Deliveroo ont le même désalignement, masqué parce que leurs
paiements sont exclus des encaissements du rapport
(`accountingExcludedChannelMOPs`).

## Décisions

1. **Corriger à la lecture (option 1 de l'analyse), pas à l'écriture.** La
   sentinelle `'KIOSK'` reste dans `orders` ; ce sont les lecteurs qui suivent le
   paiement (`cash_registers.RegisterOrdersSQL`). Raison : corrige aussi l'historique
   sans toucher aux données, et tient pour les commandes futures sans changer la
   clôture de caisse (chemin fiscal).
2. **Règle de rattachement d'une commande borne** : le registre de ses paiements
   actifs ; si plusieurs registres, le plus petit. Sans cela une commande à
   paiements sur deux registres serait comptée deux fois dans la TVA. Une commande
   sans paiement actif rattaché (attente, paiement annulé) n'est dans aucun registre,
   comme avant.
3. **Prédicat piloté par les paiements du registre** (`o.order_id IN (SELECT ...
   FROM payments WHERE cash_register_id = ?)`) plutôt que par un `CASE` sur
   `orders` : l'export lit déjà les paiements du registre, un `CASE` sur la
   colonne de la commande interdirait l'index et balaierait toutes les commandes de
   l'enseigne à chaque registre (la fenêtre du PDF est ouverte).
4. **Rattrapage des commandes déjà clôturées par un outil dédié**
   (`cmd/backfill_kiosk_order_registers`) plutôt que par une migration : la
   correction de lecture suffit à rendre les PDF justes, le rattrapage aligne les
   autres lecteurs de `orders.cash_register_id` et reste optionnel, relançable,
   par enseigne, avec un CSV de retour arrière. Une migration SQL aurait imposé de
   l'appliquer en même temps que le déploiement.
5. **`orders.last_update` n'est pas touché** par le rattrapage : les tablettes ne
   doivent pas resynchroniser des milliers de commandes pour un champ qu'elles
   n'affichent pas.
6. **Sans risque fiscal, vérifié dans le code** : `orders.cash_register_id` n'entre
   dans aucune empreinte (`fiscal.OrderClosurePayload` : montants, lignes, date de
   clôture), aucun trigger sur `orders`, et `fiscalarchive.Verify` relit le ZIP sans
   le reconstruire. Les archives déjà émises gardent `KIOSK` dans leur colonne
   « registre » ; seules celles des mois suivants verront le registre rattaché.

## Ce qui change

- `internal/modules/cash_registers/register_orders.go` : `RegisterOrdersSQL` /
  `RegisterOrdersArgs`, le prédicat « commande du registre ».
- Utilisé par `register_vat.go` (ticket Z : lignes, frais de livraison, remises)
  et par `orderScope.predicate` (`pos/accounting/repository.go`, PDF du registre,
  modes AUTO et MANUAL).
- `models.KioskCashRegisterID` : la sentinelle, partagée avec `kiosk/service.go`.

Effet attendu sur un PDF : TVA TTC = encaissements, ventes borne comprises. En
mode AUTO, où la vente borne disparaissait des deux tableaux, elle y apparaît.

## Rattraper les commandes déjà clôturées

```bash
# 1. Simulation (aucune écriture) : combien, sur quels registres, pour quel montant.
POSTGRES_URL="postgres://..." go run ./cmd/backfill_kiosk_order_registers

# 2. Une enseigne seulement, avec la liste des commandes modifiées.
POSTGRES_URL="postgres://..." go run ./cmd/backfill_kiosk_order_registers --merchant=212 --apply --out=kiosk_212.csv

# 3. Tout, par lots (reprise possible avec --start-after=<order_id>).
POSTGRES_URL="postgres://..." go run ./cmd/backfill_kiosk_order_registers --apply --out=kiosk_all.csv
```

Idempotent (le `UPDATE` ne cible que `cash_register_id = 'KIOSK'`) : une seconde
exécution rapporte 0. Le rapport donne, par registre, le nombre de commandes et le
TTC rattachés — c'est le chiffre à comparer à l'écart des PDF — et liste les
commandes à paiements sur plusieurs registres, à vérifier à la main.

**Retour arrière** : `UPDATE orders SET cash_register_id = 'KIOSK' WHERE order_id
IN (<colonne order_id du CSV>)`. La lecture retrouve le même résultat par les
paiements.

Ordre conseillé en production : déployer l'API (PDF et ticket Z justes), exporter
de nouveau les 10 registres pour confirmer l'égalité, puis lancer le rattrapage.

## Statut d'exécution (2026-10-12)

- Compilation, `go vet` (avec et sans `postgres_integration`) et tests unitaires
  des paquets `cash_registers`, `pos/accounting`, `kiosk` : OK.
- Simulation de l'outil de rattrapage sur **staging**, en lecture seule : 221
  commandes borne lues, 185 rattachables (registres 219, 532, 539, 632, 658, 677,
  680, 683), 0 multi-registres. Le prédicat de lecture, exécuté sur ces registres,
  en retrouve exactement les mêmes nombres (683 : 50, 219 : 101, 532 : 3).
- `--apply` **non exécuté**, nulle part.
- Test d'intégration `TestRegisterPDFExport_Postgres` étendu (registre AUTO avec
  vente borne de 10 € : TTC 30,00 / HT 27,27 / TVA 2,73, ticket Z, commande à
  paiements multi-registres comptée une fois) : il compile, mais **n'a pas été
  exécuté** — pas de Postgres Docker de dev disponible sur le poste. À lancer :
  `POSTGRES_URL=... go test -tags postgres_integration ./internal/modules/pos/accounting/ -run TestRegisterPDFExport_Postgres`.
- Les chiffres de production (écarts des 10 PDF) n'ont pas été rapprochés : à
  faire après déploiement.

## Limites connues

- La liste des commandes du résumé d'un registre (`GetCashRegisterSummary`) lit
  encore `orders.cash_register_id` en direct, avec un `INNER JOIN users` sur
  `created_by` : une commande borne n'y figure pas, et si aucun utilisateur
  `'KIOSK'` n'existe dans `users` (non vérifié), elle n'y figurerait pas non plus
  une fois rattachée. Hors du périmètre de l'écart, laissé tel quel.
- Les commandes ScanNOrder / Uber Eats / Deliveroo gardent leur sentinelle : leurs
  paiements sont exclus des encaissements, il n'y a donc pas d'écart visible. Si
  ces canaux entraient un jour dans les encaissements d'un registre, le même
  prédicat serait à étendre à leurs sentinelles.
- Un paiement borne n'est rattaché qu'à la **prochaine** clôture du merchant : une
  vente de la veille tombe dans le registre du lendemain, avec les deux moitiés
  désormais d'accord sur ce registre. Inchangé.
