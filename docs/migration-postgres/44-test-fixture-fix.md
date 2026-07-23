# 44 — `TestGetCashRegisterReport_Postgres` : suppression de l'hypothèse implicite sur `tva_categories`

Suite au point ouvert du rapport [43](43-qrcodes-sequence-fix-and-full-rehearsal-v5.md#61--getcashregisterreport--nouvelle-découverte-distincte-dun-blocage-de-chargement) :
« arbitrer le scope de `GetCashRegisterReport`/`TestGetCashRegisterReport_Postgres` vis-à-vis de
`tva_categories` (table de référence globale) ». Aucune donnée réelle n'est citée dans ce rapport.

## 1. Hypothèse implicite cassée

[`TestGetCashRegisterReport_Postgres`](../../internal/modules/cash_registers/postgres_integration_test.go)
vérifie la traduction SQL Postgres de [`cashRegisterReportSQL`](../../internal/modules/cash_registers/repository.go#L105).
Cette requête part de :

```sql
FROM tva_categories all_tva
LEFT JOIN (... agrégats scopés cash_register_id ...) cash_report ON cash_report.tva_id = all_tva.tva_id
LEFT JOIN labels l ON l.label_value = all_tva.delivery_type AND l.lang = 'FR' AND l.label_type = 'delivery_type'
WHERE all_tva.show_in_report IS TRUE
```

`tva_categories` et `labels` sont des **tables de référence globales**, non scopées par marchand ni
par caisse (confirmé sur le schéma cible, [04-schema-postgres-target.sql](04-schema-postgres-target.sql) —
`tva_categories` ne porte aucune colonne `merchant_id`). La requête renvoie donc, par construction,
une ligne pour **chaque** catégorie TVA globalement marquée `show_in_report = TRUE` dans toute la base
— pas seulement celles liées à la caisse demandée (les catégories sans commande rattachée obtiennent
simplement `HT`/`TTC`/`TVA` à 0 via `COALESCE`).

Le test, écrit et validé (rapports 14, 42) à une époque où `tva_categories` ne contenait **que les 3
catégories qu'il insère lui-même**, faisait deux hypothèses implicites, toutes deux invalidées par un
chargement de données réel complet (rapport 43, §3.1 et §6.1) :

1. **Comptage exact** — `len(got) != len(want)` avec `want` limité aux 3 catégories du test : dès que
   `tva_categories` porte son contenu réel (plusieurs catégories globales avec `show_in_report = TRUE`
   en plus des 3 du test), le rapport contient davantage de lignes et l'assertion échoue
   (`ventilation TVA: got 9 lignes (...), want 3` dans le rapport 43).
2. **Propriété exclusive de `tva_id = -1`** — le nettoyage du test (`DELETE FROM tva_categories WHERE
   tva_id IN (9101, 9102, -1)`) supprime sans conditions une ligne dont l'identifiant est un
   **sentinelle réel et significatif du domaine métier** (`tva_id = -1`, utilisé tel quel par la
   branche "frais de livraison" de `cashRegisterReportSQL`, cf. rapport 43 §3.1 —
   `SENTINEL_IDENTITY_RULES[("tva_categories","tva_id")] = "-1"`). Si cette ligne préexiste (données
   réelles chargées), le test l'efface au démarrage, la remplace par sa propre valeur de test, puis
   la supprime à nouveau en fin de run — sans jamais la restaurer. Le même mécanisme touche les
   libellés `labels` (`IN`/`TAKE_AWAY`/`DELIVERY`, `delivery_type`/`FR`), eux aussi des données de
   référence globales couramment déjà présentes.

Le second point est plus grave que le premier : ce n'est pas seulement une assertion trop stricte,
c'est un test qui **efface une ligne de référence réelle** dès qu'il tourne contre une base peuplée.

## 2. Correctif appliqué

Seul [`postgres_integration_test.go`](../../internal/modules/cash_registers/postgres_integration_test.go)
(fonction `TestGetCashRegisterReport_Postgres`) a été modifié — aucun autre test, aucune requête de
production, aucun schéma.

### a) `tva_id` dédiés (9101, 9102) — inchangé dans l'esprit

Ces deux identifiants sont hors de toute plage d'auto-incrément réaliste (`GENERATED ALWAYS AS
IDENTITY`, valeurs de départ basses) : ils restent la propriété exclusive du test, insérés/nettoyés
sans condition, comme avant.

### b) `tva_id = -1` et les labels `delivery_type` — préexistence vérifiée avant toute écriture

Avant d'insérer quoi que ce soit, le test vérifie si la ligne `tva_id = -1` et les 3 libellés existent
déjà :

- `tva_id = -1` : lu via `tva_desc`. S'il existe avec un `tva_desc` différent du marqueur `itest`, il
  est considéré comme une donnée réelle (ou d'un autre propriétaire) — **jamais supprimé ni modifié** ;
  son taux (`tva_rate`), son `delivery_type` et son `tva_title` réels sont lus et utilisés tels quels
  pour calculer les valeurs attendues du test (HT/TVA des frais de livraison, calculés avec le même
  arrondi indépendant que `cashRegisterReportSQL` plutôt que dérivés de HT = TTC − TVA, pour matcher
  exactement le comportement SQL quel que soit le taux réel). S'il n'existe pas, le test insère sa
  propre ligne (taux 20 %, `tva_desc = 'itest'`) et la nettoie en fin de run.
- Labels `IN`/`TAKE_AWAY`/`DELIVERY` (`delivery_type`/`FR`) : même logique, sur simple existence (pas
  de marqueur de contenu disponible sur cette table) — insérés puis nettoyés uniquement s'ils étaient
  absents au démarrage.

### c) Assertions de ventilation TVA scopées aux données du test

L'égalité stricte de cardinalité (`len(got) != len(want)`) est remplacée par :

- une vérification que chacune des 3 clés `(delivery_type, tva_title)` attendues par le test est
  présente avec les bonnes valeurs (HT/TTC/TVA) ;
- une vérification complémentaire que **toute autre ligne** du rapport (catégories globales
  étrangères au test) porte des montants nuls — invariant garanti par construction (caisse et
  produits dédiés à ce test), qui détecterait une fuite de montants vers une mauvaise catégorie sans
  dépendre du nombre total de catégories existantes.

Les totaux du rapport (`report.HT/TTC/TVA`, `GetCashRegisterTVADetails`) sont recalculés dynamiquement
à partir du taux effectif de `tva_id = -1` (`TTC` reste constant à 3800, indépendant du taux ; `HT`/`TVA`
en dépendent).

## 3. Vérification — base vide et base peuplée

Exécuté contre le Postgres Docker de dev (`docker-compose.postgres.yml`, conteneur
`welloresto-postgres-dev`, `POSTGRES_URL=postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev`,
tag `postgres_integration`).

### Base déjà peuplée (état courant du conteneur de dev)

État constaté avant toute exécution (résidu des sessions précédentes documentées au rapport 43 —
`tva_categories` et `labels` déjà amputés de leurs lignes `delivery_type`/sentinelle par un run
antérieur du test non corrigé) : `tva_categories` = 9 lignes, aucune `tva_id = -1` ; `labels` = 70
lignes, aucune des 3 lignes `delivery_type`/`FR` recherchées.

- `go test -tags postgres_integration ./internal/modules/cash_registers/... -run
  TestGetCashRegisterReport_Postgres -count=1` → **PASS**.
- Comptages avant/après identiques (`tva_categories` = 9, `labels` = 70) : le test insère ses propres
  lignes (dont sa propre ligne `tva_id = -1`, absente au départ) et les nettoie intégralement.

### Base avec une ligne `tva_id = -1` et des labels déjà présents (simulation)

Aucune donnée réelle n'étant disponible dans cet environnement d'exécution, la situation d'une base
« déjà peuplée avec le sentinelle réel » a été simulée avec des lignes synthétiques clairement
identifiables (`tva_desc = 'synthetic-not-real-simulated-prod-row'`, taux 7.5 %, libellés suffixés
`(synthetic)`), insérées manuellement avant le run puis supprimées manuellement après vérification —
aucune valeur réelle n'a été utilisée ni citée.

- `go test -tags postgres_integration ./internal/modules/cash_registers/... -run
  TestGetCashRegisterReport_Postgres -count=1` → **PASS**, avec le taux 7.5 % utilisé dynamiquement
  pour les valeurs attendues.
- Après le run : la ligne `tva_id = -1` synthétique et les 3 labels synthétiques sont **intacts**
  (mêmes `tva_desc`/`label`, aucune suppression ni écrasement) — confirme que le test ne touche plus
  une ligne de référence qu'il ne possède pas.
- Nettoyage manuel des lignes synthétiques après vérification : comptages restaurés à l'identique de
  l'état de départ (`tva_categories` = 9, `labels` = 70).

### `go build`

```
go build ./...
go build -tags postgres_integration ./...
go vet ./internal/modules/cash_registers/...
```

Les trois commandes passent sans erreur.

### Suite du module

`TestCashRegisterLifecycle_Postgres` (même fichier) échoue indépendamment de ce correctif :
`CloseCashRegister failed against postgres: sql: Scan error on column index 1, name "label":
converting NULL to string is unsupported`. Reproduit à l'identique en isolant le fichier de test
d'origine (avant application de ce correctif, via `git stash`) — c'est le même résidu `labels`
manquantes documenté au rapport 43 (§3.1, §7), qui affecte cette fois un autre test que celui visé par
cette tâche. Conformément à la consigne (« ne touche à aucun autre test »), **aucune modification n'a
été apportée à `TestCashRegisterLifecycle_Postgres`** ; ce point reste ouvert pour une prochaine
session.

## 4. Synthèse

| Étape | Résultat |
|---|---|
| Hypothèse implicite identifiée | Double : (1) `tva_categories` supposée ne contenir que les 3 lignes du test — comptage exact invalidé par un chargement réel ; (2) `tva_id = -1` et les labels `delivery_type` supposés propriété exclusive du test — suppression destructrice d'une ligne de référence réelle |
| Correctif | Préexistence vérifiée avant toute écriture sur `tva_categories`/`labels` (jamais de suppression/écrasement d'une ligne étrangère) ; assertions de ventilation TVA scopées aux clés propres du test + invariant "lignes étrangères à 0" ; totaux recalculés dynamiquement à partir du taux effectif de `tva_id = -1` |
| Fichiers modifiés | `internal/modules/cash_registers/postgres_integration_test.go` uniquement |
| Base vide | PASS, comptages avant/après identiques |
| Base peuplée (simulation sentinelle + labels préexistants) | PASS, ligne/labels préexistants intacts après le run |
| `go build ./...` / `-tags postgres_integration` / `go vet` | OK |
| Point résiduel hors scope | `TestCashRegisterLifecycle_Postgres` échoue pour une raison distincte et préexistante (labels `delivery_type` manquantes, résidu du rapport 43) — non corrigé, non modifié |
| Données réelles | Aucune citée ni manipulée ; simulation via lignes synthétiques explicitement marquées, supprimées après vérification |
