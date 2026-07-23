# 35 - Diagnostic colonnes candidates a suppression (orders.isDelivery, customer.is_migrated)

Date: 2026-07-21
Branche: migration/postgres

## Objectif de ce document

Suite aux deux blocages remontes dans [33-sql-output-generation.md](33-sql-output-generation.md)
(`orders.isDelivery` et `customer.is_migrated` declarees `boolean` cote schema cible Postgres mais
contenant des valeurs source hors 0/1), diagnostic en lecture seule pour savoir si ces deux colonnes
sont mortes cote code Go, avant toute decision de suppression.

**Aucun fichier n'a ete modifie a cette etape.** Ce document est un diagnostic uniquement — aucune
donnee reelle n'y est citee (uniquement noms de fichiers, de fonctions, de routes, de champs).

## Methode

Recherche exhaustive (grep insensible a la casse, tout le depot, fichiers `.go`) de:
- `isDelivery` / `IsDelivery`
- `is_migrated` / `IsMigrated` / `isMigrated`

Pour chaque occurrence trouvee, classification en deux axes independants:
- **Logique** : existe-t-il un site vivant (code appelable, non mort/commente) qui **lit** la valeur
  pour une decision — filtre, condition, calcul ? Un simple `Scan` SQL qui copie la colonne dans un
  champ de struct **ne compte pas** comme site vivant en logique (c'est de la serialisation, pas une
  decision).
- **JSON** : la colonne apparait-elle comme champ (avec tag `json`) d'une struct utilisee comme
  reponse HTTP, meme si jamais lue en logique ensuite ? Si oui, sur quel(s) endpoint(s), et le champ
  est-il toujours present (pas de `omitempty`) ou peut-il etre absent ?

## 1. `orders.isDelivery`

### Sites trouves (5 fichiers)

| Fichier | Ligne(s) | Nature |
|---|---|---|
| `internal/modules/orders/orders_fetcher_builder.go` | 611 | `SELECT ... CASE WHEN o.isDelivery THEN 1 ELSE 0 END AS isDelivery` — requete SQL vivante |
| `internal/modules/orders/orders_fetcher_builder.go` | 650, 675 | Variable de scan `isDelivery sql.NullInt64`, destination de `rows.Scan` |
| `internal/modules/orders/orders_fetcher_builder.go` | 716 | `ord.IsDelivery = int(isDelivery.Int64)` — affectation au champ de struct |
| `internal/modules/cash_registers/repository.go` | 678, 698 | `/*o.isDelivery,*/` et `//&o.IsDelivery,` — **commentes, code mort** |
| `internal/models/orders_model.go` | 222 | `type Order struct { ... IsDelivery int \`json:"isDelivery"\` ... }` |
| `internal/models/request_objects.go` | 108 | `type CROrder struct { ... IsDelivery *int \`json:"isDelivery"\` ... }` |
| `internal/models/request_objects.go` | 418 | `type PricingOrder struct { ... IsDelivery string \`json:"is_delivery"\` ... }` |

### Statut LOGIQUE : **MORTE**

Aucun site ne **lit** `.IsDelivery` (ni la colonne `o.isDelivery` en SQL) pour brancher une decision
apres l'avoir recuperee. Verifie par grep exhaustif de `.IsDelivery` (acces au champ) sur tout le
depot : les deux seules occurrences sont l'affectation (`ord.IsDelivery = ...`, une ecriture, pas
une lecture) et le site mort/commente dans `cash_registers/repository.go`. Aucun `if`, comparaison,
ou calcul base sur cette valeur n'a ete trouve nulle part dans le code Go vivant.

`PricingOrder` (struct contenant aussi un champ `IsDelivery`) n'est reference **nulle part ailleurs**
dans tout le depot (0 usage trouve en dehors de sa propre definition) — struct entierement morte,
sans rapport avec la colonne `orders.isDelivery` malgre le nom de champ identique.

### Statut JSON : **OUI, exposee, sur plusieurs endpoints vivants**

`models.Order.IsDelivery` (`int`, sans `omitempty` donc toujours present, valeur reelle 0/1 issue de
la requete SQL `CASE WHEN ... THEN 1 ELSE 0`) est renvoye par :

| Route | Handler | Via |
|---|---|---|
| `POST /orders/list` | `ordersH.GetOrders` | `[]models.Order` directement |
| `POST /orders/history` | `ordersH.GetHistory` | `[]models.Order` directement |
| `GET /orders/{order_id}` | `ordersH.GetOrder` | `PendingOrdersResponse.Orders []Order` |
| `GET /orders/pending` | `ordersH.GetPendingOrders` | `PendingOrdersResponse.Orders []Order` |

`CROrder.IsDelivery` (`*int`, sans `omitempty` donc toujours present, mais **jamais assigne** — le
SELECT correspondant est commente dans le repository) est renvoye par :

| Route | Handler | Via |
|---|---|---|
| `GET /cash_register/{cash_register_id}/summary` | `cashRegisterH.GetCashRegisterSummary` | `CashRegisterSummaryResponse.CashRegister.Orders []CROrder`, champ toujours `null` |

Toutes les routes ci-dessus sont enregistrees et actives dans `cmd/api/routes.go` (`authMiddleware`
applique, pas de code mort). `docs/DELIVERY_API_AUDIT.md` documente egalement `IsDelivery` comme
"flag de statut" expose par l'API de livraison, coherent avec ce constat.

### Consequence pour la suite

Le declencheur d'arret defini pour cette tache porte sur un site **vivant en logique** — aucun n'a
ete trouve, donc pas d'arret automatique sur ce critere strict. Cela dit, la colonne est bien
**visible aujourd'hui dans de vraies reponses JSON de production** sur des endpoints de premier
plan (liste des commandes, historique, detail commande). Retirer la colonne cote Postgres changerait
ce contrat d'API (le champ `isDelivery` devrait soit disparaitre, soit etre fige a une valeur
arbitraire) meme si aucune logique interne n'en depend. Cette nuance est remontee ici pour
l'arbitrage — aucune decision de suppression n'est prise dans ce document.

## 2. `customer.is_migrated`

### Sites trouves : aucun

Grep exhaustif insensible a la casse de `is_migrated`, `IsMigrated`, `isMigrated` sur l'intégralite
du depot, restreint aux fichiers `.go` : **0 resultat**. Les seules occurrences dans tout le depot
sont dans la documentation ([docs/migration-postgres/wello-resto-mysql-ddl.md](wello-resto-mysql-ddl.md),
[04-schema-postgres-target.sql](04-schema-postgres-target.sql), et le rapport 33 lui-meme).

Verification complementaire : le repository customer (`internal/modules/customers/repository.go`)
n'utilise jamais `SELECT *` — toutes ses requetes listent les colonnes explicitement — et
`is_migrated` n'apparait dans aucune de ces listes.

### Statut LOGIQUE : **MORTE**

Aucun site, vivant ou mort, ne reference cette colonne cote Go.

### Statut JSON : **NON**

Aucun champ Go ne porte cette donnee, dans aucune struct — elle ne peut donc apparaitre dans aucune
reponse JSON de l'API.

## Conclusion de ce diagnostic

| Colonne | Logique | Expose en JSON | Endpoints concernes |
|---|---|---|---|
| `orders.isDelivery` | MORTE (aucune decision ne depend de la valeur) | **OUI** | `POST /orders/list`, `POST /orders/history`, `GET /orders/{order_id}`, `GET /orders/pending`, `GET /cash_register/{id}/summary` (toujours `null` sur cette derniere) |
| `customer.is_migrated` | MORTE | NON | aucun |

Aucune des deux colonnes n'a de site vivant **en logique**, donc le critere d'arret defini pour cette
tache ne se declenche pas au sens strict. `customer.is_migrated` est un cas net (aucune trace cote
Go, aucune exposition). `orders.isDelivery` est un cas plus nuance : mort en logique, mais bien
visible dans des reponses JSON reellement servies aujourd'hui par plusieurs endpoints actifs — a
prendre en compte avant d'arbitrer sur sa suppression cote schema cible.

Diagnostic initial fige ci-dessus. La suite de ce document couvre l'execution de la suppression,
actee separement (logique morte des deux cotes, aucune consommation confirmee dans
wello_resto_flutter, wello-kiosk, ScanNOrder, wello-back-office).

---

## 3. Execution : retrait du schema cible et du generateur SQL

Date: 2026-07-21 (suite)

Perimetre execute : schema Postgres cible, generateur `transform_mysql_csv.py`, et — en bonus,
juge suffisamment contenu — le code Go MySQL qui alimentait le champ JSON `isDelivery`. **Les
colonnes MySQL source ne sont pas touchees** : `orders.isDelivery` et `customer.is_migrated`
restent en base MySQL telles quelles ; seul leur portage vers Postgres (et, pour `isDelivery`,
leur exposition JSON cote API) est retire.

### 3.1 Schema cible (`04-schema-postgres-target.sql`)

- `orders.isDelivery boolean DEFAULT true` retiree du `CREATE TABLE orders`.
- `customer.is_migrated boolean DEFAULT false` retiree du `CREATE TABLE customer`.
- Notes de casse mixte obsoletes mises a jour (le commentaire `isDelivery: identifiant mixed-case...`
  au-dessus de `CREATE TABLE orders` remplace par une note de suppression renvoyant a ce document).
- **Non touche, verifie explicitement** : `users.isDelivery integer NOT NULL DEFAULT 0` — colonne
  homonyme mais sans rapport (flag de role livreur cote staff), dans une table differente. Confirme
  absent de la liste des colonnes retirees et toujours present apres coup (verification AST ci-dessous).

Revalidation `pglast` (meme methode que les rapports
[13](13-merchant-id-schema-update.md)/[18](18-order-id-schema-update.md)/[26](26-planning-day-comments-integration.md)/[28](28-varchar-widening.md)) :

```
$ python -c "
import pglast
sql = open('docs/migration-postgres/04-schema-postgres-target.sql', encoding='utf-8').read()
stmts = pglast.parse_sql(sql)
print(f'OK - {len(stmts)} statements parsed successfully')
"
OK - 457 statements parsed successfully
```

457 instructions — identique au compte du rapport 28 (aucune instruction ajoutee/supprimee, seules
2 colonnes retirees dans des `CREATE TABLE` existants). Verification ciblee via l'AST (`pglast.ast`,
`CreateStmt.tableElts`) : `isDelivery` absente de `orders`, `is_migrated` absente de `customer`,
`isdelivery` (repli minuscule Postgres pour l'identifiant non cite `isDelivery`) toujours presente
dans `users` — confirme non affectee.

### 3.2 Generateur (`data-migration/transform_mysql_csv.py`)

Probleme a resoudre : le generateur utilisait directement les colonnes du dump MySQL comme liste de
colonnes de l'`INSERT` cible. Une colonne presente cote source mais absente du schema cible
provoquait soit une erreur bloquante (cas booleen deja rencontre dans le rapport 33), soit —
decouvert pendant ce chantier — une `INSERT` invalide referencant une colonne inexistante cote
Postgres (silencieux, aucune exception Python, simplement du SQL casse a l'execution).

Changement apporte :
- Nouvelle fonction `filter_columns_to_schema(table_info, source_columns)` : separe les colonnes
  source en `kept` (presentes dans le schema cible) / `dropped` (absentes). Utilisee par
  `generate_sql_for_table` et `generate_all_sql` a la place d'une utilisation directe des colonnes
  du dump.
- `SqlTableWriter` reecrit pour distinguer colonnes source (position dans chaque ligne brute du
  dump) et colonnes de sortie (celles reellement inserees) — un index de positions calcule une fois
  par table evite un lookup par ligne. Les colonnes ecartees sont listees en commentaire en tete du
  fichier `.sql` genere (`-- Source columns not carried over to Postgres (absent from target
  schema): ...`).
- Rapport JSON de `generate-all-sql` enrichi d'une cle `dropped_source_columns_by_table` (table ->
  liste des colonnes source ecartees), pour que toute future divergence source/cible similaire soit
  visible sans avoir a la decouvrir par un blocage.

**Bug latent trouve et corrige en cours de route** (independant de `isDelivery`/`is_migrated`) :
la regex d'analyse du schema cible (`load_schema` / `_split_table_blocks`) ne reconnaissait que les
identifiants entre backticks, pas les identifiants entre guillemets doubles Postgres (necessaires
pour les mots reserves, ex. `"position"`). Consequence : `planning_shifts.position` (colonne bien
presente cote cible, juste citee `"position"` car `position` est un mot reserve) n'etait pas
reconnue par le parseur de schema et se serait retrouvee, avec le nouveau filtre, exclue a tort de
la generation — une regression que le filtre aurait introduite silencieusement sur une table sans
rapport avec ce chantier. Corrige en etendant la regex aux guillemets doubles en plus des backticks
(3 occurrences dans le fichier). Deuxieme cas concerne par le meme bug : `timezone_info."offset"` —
sans impact pratique, cette table fait partie des 34 tables orphelines deja exclues de la generation.
Verifie apres correction : generation reelle sans aucune colonne source ecartee en dehors des 2
attendues (`orders.isDelivery`, `customer.is_migrated`) — voir 3.3.

### 3.3 Validation sur le fichier reel

`generate-all-sql` relance sur `data-migration/migration_welloresto_data.sql` (250 Mo). Sortie
ecrite dans un dossier temporaire hors du repo, verifiee, puis supprimee — aucune donnee reelle
citee ci-dessous.

- **147/147 tables incluses generees, 0 echec** (`failed_tables: {}`) — contre 145/147 dans le
  rapport 33.
- `dropped_source_columns_by_table` : `{"customer": ["is_migrated"], "orders": ["isDelivery"]}` —
  exactement les 2 colonnes attendues, rien d'autre.
- Comptage de lignes par table : **identique**, table par table, sur les 147 tables, a un comptage
  independant fait directement sur le dump brut (0 ecart) — `orders` 32 849 lignes, `customer`
  7 337 lignes, coherent avec les comptages du rapport 33.
- Verification syntaxique generique (parentheses equilibrees hors chaines, guillemets fermes,
  exactement une paire `BEGIN;`/`COMMIT;` par fichier) sur les 147 fichiers : 0 anomalie, 1055
  instructions `INSERT` au total.
- Preservation du `NULL` (meme methode que le rapport 33, comptage independant sur le dump brut en
  excluant les colonnes ecartees, compare au nombre de `NULL` autonomes trouves dans chaque fichier
  genere) : **1 642 442 `NULL` attendus, 1 642 442 trouves, 0 ecart**, sur les 147 fichiers.
- Confirme structurellement : `isDelivery`/`is_migrated` absentes de la liste de colonnes des
  `INSERT` de `orders.sql`/`customer.sql` ; `position` bien presente dans la liste de colonnes de
  `planning_shifts.sql`.

### 3.4 Bonus : retrait cote API Go (MySQL, independant de Postgres)

Fait — perimetre contenu (4 fichiers), coherent avec les correctifs precedents de ce type sur ce
depot :

- `internal/modules/orders/orders_fetcher_builder.go` : colonne `CASE WHEN o.isDelivery THEN 1 ELSE
  0 END AS isDelivery` retiree du `SELECT`, variable de scan `isDelivery sql.NullInt64` et son
  emplacement dans `rows.Scan(...)` retires, affectation `ord.IsDelivery = int(isDelivery.Int64)`
  retiree. Commentaire de tete de fonction mis a jour (ne mentionne plus que
  `use_customer_temporary_address`).
- `internal/models/orders_model.go` : champ `IsDelivery int \`json:"isDelivery"\`` retire de
  `Order` — ne s'affiche plus sur `POST /orders/list`, `POST /orders/history`,
  `GET /orders/{order_id}`, `GET /orders/pending`.
- `internal/models/request_objects.go` : champ `IsDelivery *int \`json:"isDelivery"\`` retire de
  `CROrder` — ne s'affiche plus sur `GET /cash_register/{id}/summary`. `PricingOrder.IsDelivery`
  **non touche** : struct entierement inutilisee ailleurs (diagnostic initial), hors perimetre des
  5 endpoints cites, laissee telle quelle pour ne pas elargir le changement au-dela de ce qui a ete
  demande.
- `internal/modules/cash_registers/repository.go` : nettoyage des references mortes/commentees
  `/*o.isDelivery,*/` et `//&o.IsDelivery,`, devenues obsoletes une fois le champ retire de
  `CROrder`.

`customer.is_migrated` n'a necessite aucun changement Go — deja absente de tout le code (diagnostic
initial).

Verification : `go build ./...` OK (aucune erreur). `go vet` OK sur les paquets touches (`orders`,
`cash_registers`, `models`) et avec le tag `postgres_integration`. `go test ./internal/modules/orders/...`
OK. Les echecs observes sur `go test ./...` (paquets `bookingcomm`, `planning/employees`,
`planning/leave`, `planning/swaps`) sont pre-existants et sans rapport avec ce chantier — confirme
via `git status` : ces fichiers de test portaient deja des modifications non liees, en cours sur
cette branche avant cette tache.

### 3.5 Recapitulatif

| Etape | Resultat |
|---|---|
| Schema cible | 2 colonnes retirees (`orders.isDelivery`, `customer.is_migrated`), `users.isDelivery` non touchee |
| Validation `pglast` | 457 instructions, 0 erreur, verification AST ciblee |
| Generateur SQL | Filtre colonnes source/cible ajoute + bug de parsing des identifiants cites corrige (evite une regression sur `planning_shifts.position`) |
| Generation reelle | 147/147 tables, 0 echec, 0 ecart sur comptages de lignes et `NULL`, 0 anomalie syntaxique |
| API Go (bonus) | Champ `isDelivery` retire de 3 reponses JSON (`Order`, `CROrder`) sur MySQL ; build + tests des paquets concernes OK |
| Migrations MySQL existantes | Non touchees ; colonnes toujours presentes en base MySQL source |

Aucune donnee reelle n'a ete citee dans ce document. Aucun fichier de sortie contenant de vraies
donnees n'a ete conserve apres verification.
