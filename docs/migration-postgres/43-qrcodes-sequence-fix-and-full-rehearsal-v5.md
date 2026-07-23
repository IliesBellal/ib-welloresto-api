# 43 — Correctif `qrcodes.QR_id`, généralisation, confirmation `labels`, et 5ᵉ répétition générale (premier chargement complet 147/147)

Date: 2026-07-21
Branche: migration/postgres

## Objectif

Corriger le bug de casse identifié au rapport [42](42-full-data-load-rehearsal-v4.md) (`qrcodes.QR_id`),
généraliser le correctif à toute colonne identity à casse mixte, confirmer rigoureusement l'explication
de l'écart `labels` (rapport 42 §4.3), puis rejouer le protocole de répétition générale jusqu'au bout.
**Aucune donnée réelle n'est citée.** Rien n'a été commité.

## 1. Correctif `qrcodes.QR_id`

[data-migration/transform_mysql_csv.py](../../data-migration/transform_mysql_csv.py),
`_setval_columns_for` — avant :

```python
dump_columns_lower = {c.lower(): c for c in dump_columns}
return tuple(
    dump_columns_lower[identity_col.lower()]   # <- casse du dump source (ex. "QR_id")
    for identity_col in table_info.identity_columns
    if identity_col.lower() in dump_columns_lower
)
```

Après :

```python
dump_columns_lower = {c.lower() for c in dump_columns}
return tuple(
    identity_col.lower()                       # <- forme repliée Postgres (ex. "qr_id")
    for identity_col in table_info.identity_columns
    if identity_col.lower() in dump_columns_lower
)
```

Le nom de colonne utilisé pour construire le trailer `setval()` vient maintenant systématiquement de sa
forme repliée en minuscules (comportement Postgres pour tout identifiant non guillemeté à la création,
cf. [04-schema-postgres-target.sql](04-schema-postgres-target.sql)), plus jamais de la casse d'origine
du dump MySQL. La liste des colonnes de l'`INSERT` lui-même (`output_columns`) n'est **pas** touchée —
elle continue d'utiliser la casse du dump, ce qui est sans conséquence : un identifiant non guillemeté
dans une clause de colonnes `INSERT` est replié par Postgres au moment du parsing, contrairement à un
argument texte de `pg_get_serial_sequence()` (rapport 42 §3.2).

Vérifié directement :

```
>>> _setval_columns_for(schema['qrcodes'], ('QR_id', 'merchant_id', ...))
('qr_id',)
```

## 2. Généralisation : balayage de toutes les colonnes identity à casse mixte

Balayage de `table_info.identity_columns` tel que le générateur les reconnaît lui-même (pas une
relecture manuelle du schéma) :

```
Total colonnes identity dans le schéma cible : 83
Colonnes identity à casse mixte : [('qrcodes', 'QR_id')]
```

**`qrcodes.QR_id` est la seule occurrence dans les 181 tables du schéma.** Le correctif du §1
s'applique cependant **uniformément** à toutes les colonnes identity, pas seulement à celle-ci —
`identity_col.lower()` est un no-op pour les 82 colonnes déjà tout-minuscules (aucun changement de
comportement pour elles), et corrige la seule qui en avait besoin. Confirmé après régénération complète
(§4) : balayage des 59 instructions `setval(pg_get_serial_sequence(...))` effectivement générées dans
les 147 fichiers de sortie — **0 occurrence à casse mixte, dans aucun des deux arguments**, sur les 59.
Aucune colonne identity restante ne peut donc reproduire cette classe de bug plus loin dans la séquence
de chargement.

## 3. Confirmation rigoureuse de l'explication `labels`

Le rapport 42 §4.3 attribuait l'écart `labels` (−3) à `TestGetCashRegisterReport_Postgres`
([internal/modules/cash_registers/postgres_integration_test.go](../../internal/modules/cash_registers/postgres_integration_test.go)),
dont le nettoyage (ligne 43) supprime sans le scoper à ses propres données toute ligne
`label_type = 'delivery_type' AND lang = 'FR' AND label_value IN ('IN','TAKE_AWAY','DELIVERY')`.
Reproduit ici en isolation, avant la répétition complète, pour confirmer chaque maillon de
l'explication plutôt que de la revalider par défaut :

1. **Correspondance structurelle (comptage, pas contenu)** : reparsing du fichier généré
   `076_labels.sql` (73 lignes au total) — exactement **3** lignes correspondent structurellement au
   filtre du test (mêmes 3 colonnes-clé, mêmes valeurs de catégorie `label_type`/`lang`/`label_value`
   que celles citées dans le code source du test lui-même — aucune valeur de la colonne `label`
   elle-même n'a été lue ni citée).
2. **Même Postgres Docker de dev** : reproduction isolée — schéma + seul `076_labels.sql` chargés dans
   `welloresto-postgres-dev` (`localhost:5433`, base `welloresto_dev`, le même conteneur que celui
   utilisé pour la répétition complète du §4) — comptage avant test : **73 lignes, dont exactement 3**
   correspondant au filtre du test. Le test a été exécuté avec
   `POSTGRES_URL=postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev` — **le même DSN, le
   même port, le même conteneur** que la répétition complète (le test se connecte via
   `pgtest.Open(t)`, qui lit cette variable).
3. **Résultat après exécution du test réel (`go test -tags postgres_integration
   ./internal/modules/cash_registers/... -run TestGetCashRegisterReport_Postgres`)** : `labels` passe de
   **73 à 70** (delta exact **−3**), et **0** ligne ne correspond plus au filtre du test après coup —
   cohérent au chiffre près avec le nettoyage pré-test (suppression des 3 lignes réelles préexistantes)
   suivi du nettoyage post-test (suppression des 3 lignes que le test venait d'insérer lui-même, de
   structure identique).

**L'explication du rapport 42 est intégralement confirmée**, avec preuve reproduite dans cette session
(pas une simple relecture du rapport précédent) : le test PASS dans les deux cas (isolation ici, et dans
la répétition complète du §4), l'écart est entièrement expliqué, aucune part non expliquée ne subsiste.

### 3.1 — Un second exemple du même phénomène, découvert pendant la répétition complète

La répétition complète du §4 a révélé un écart supplémentaire non observé au rapport 42 (qui ne
chargeait pas encore `tva_categories`, atteinte seulement après le point de blocage résolu ici) :
`tva_categories` passe de 10 à 9 après l'exécution de la même suite de tests. Même mécanisme exact :
le nettoyage du même test ([repository_test.go:42](../../internal/modules/cash_registers/postgres_integration_test.go#L42))
supprime sans le scoper `DELETE FROM tva_categories WHERE tva_id IN (9101, 9102, -1)` — `tva_id = -1`
est un **identifiant sentinelle réel et significatif** du domaine métier (règle documentée dans le
générateur, `SENTINEL_IDENTITY_RULES[("tva_categories","tva_id")] = "-1"` — une ligne réelle,
préexistante dans le dump, portant cet identifiant précis). Le filtre du test la supprime au même titre
qu'une ligne de test, pour la même raison structurelle que `labels`. Confirme, sur une seconde table
indépendante, que le mécanisme identifié au rapport 42 est le bon — pas une coïncidence isolée.

**Aucune correction apportée** à ces fichiers de test (hors périmètre de cette tâche — ce sont des
suites de tests préexistantes, non modifiées) ; ligne restaurée uniquement dans la mesure du possible
(voir §5, `hours_of_operation`) ; `labels` et `tva_categories` restent à −3/−1 dans cette instance de
dev jetable (le dump sur disque n'est pas affecté).

## 4. Régénération des 147 fichiers

`generate-all-sql` relancé avec le générateur corrigé :

- **147/147 tables générées, 0 échec.**
- **Total : 472 774 lignes** — identique à tous les rapports précédents (36→42), dump source inchangé.
- `dropped_null_key_rows_by_table: {"orderitems": 2}`,
  `dropped_source_columns_by_table: {"customer": ["is_migrated"], "orders": ["isDelivery"]}` —
  inchangés.
- `114_qrcodes.sql` : trailer désormais `SELECT setval(pg_get_serial_sequence('qrcodes', 'qr_id'),
  COALESCE(MAX(qr_id), 1), MAX(qr_id) IS NOT NULL) FROM qrcodes;` — deux arguments minuscules.

## 5. Répétition complète, depuis un Postgres dev remis à zéro

### Reset et schéma

```
docker compose -f docker-compose.postgres.yml down -v
docker compose -f docker-compose.postgres.yml up -d
```

Conteneur recréé, prêt en 2 s. Schéma chargé : **0 erreur, 181 tables**.

### Chargement séquentiel

Même protocole (`psql -v ON_ERROR_STOP=1`, un fichier à la fois, arrêt immédiat au premier échec) :

**147/147 fichiers chargés avec succès. `ALL_OK`. Aucun blocage.** C'est la première fois, sur les 5
répétitions de cette série (rapports 36, 38, 39, 40/42, celle-ci), que le chargement complet des 147
fichiers aboutit sans interruption — la correction `qrcodes.QR_id` était le dernier point bloquant
identifié.

### Comptages exacts sur les 147 tables

Comptage `SELECT count(*)` sur les 147 tables, comparé aux `row_counts` du rapport de génération,
**avant toute exécution de test Go** (pour ne pas polluer la mesure de référence — voir §6 pour la
suite) :

**147/147 tables : comptage strictement identique, 0 écart. Total : 472 774 lignes**, identique à la
régénération (§4) et à tous les rapports précédents.

### Resynchronisation des séquences identity

```
qrcodes : MAX(qr_id) = 375  |  qrcodes_qr_id_seq : last_value = 375, is_called = true
orders  : MAX(order_id) = 33255  |  orders_order_id_seq : last_value = 33255, is_called = true
```

Les deux séquences sont exactement resynchronisées sur le maximum réellement chargé — **`qrcodes`
n'échoue plus** (c'était le point de blocage du rapport 42).

## 6. Vérifications applicatives réelles (Go, `DB_DIALECT=postgres`)

Exécutées contre ce Postgres pleinement chargé (147/147), via les suites de tests d'intégration
existantes (tag `postgres_integration`,
`POSTGRES_URL=postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev`) :

| Fonction | Module | Résultat |
|---|---|---|
| `GetOrder` | `internal/modules/orders` | ✅ PASS |
| `GetUserByToken` | `internal/modules/users` | ✅ PASS |
| `ComputePOSStatus` (+ `FetchActiveSlots`) | `internal/modules/openinghours` | ✅ PASS |
| Suite `planning/schedule` | `internal/modules/planning/schedule` | ✅ PASS |
| `InsertMerchant` (insertion identity réelle sur `qrcodes`) | `internal/modules/pos` | ✅ PASS |
| `CreateOrder` (insertion identity réelle sur `orders`) | `internal/modules/order_life_cycle` | ✅ PASS |
| `GetCashRegisterReport` | `internal/modules/cash_registers` | ❌ **FAIL — voir §6.1** |

### 6.1 — `GetCashRegisterReport` : nouvelle découverte, distincte d'un blocage de chargement

**Ce n'est pas un échec de la requête contre Postgres** : `GetCashRegisterReport` s'exécute sans erreur
SQL, aucune incompatibilité de dialecte, aucun problème de traduction MySQL→Postgres. C'est
`TestGetCashRegisterReport_Postgres` qui échoue sur une **assertion de nombre de lignes** :

```
ventilation TVA: got 9 lignes (...), want 3
```

**Cause exacte, structurelle** : la requête [cashRegisterReportSQL](../../internal/modules/cash_registers/repository.go#L105)
part de `FROM tva_categories all_tva ... WHERE all_tva.show_in_report IS TRUE` — **sans filtre par
marchand ni par caisse** : `tva_categories` est une table de référence globale (non scopée), donc cette
requête renvoie par construction **une ligne pour chaque catégorie de TVA globalement marquée
`show_in_report = TRUE` dans toute la base**, avec `HT`/`TTC`/`TVA` à 0 (via `COALESCE`) pour les
catégories sans commande rattachée à la caisse demandée.

Ce test avait été écrit et validé (rapports 14, 42) dans un `tva_categories` ne contenant, à ce
moment-là, **que les 3 catégories que le test insère lui-même** (`tva_categories` n'ayant jamais encore
été atteinte par un chargement complet avant cette session — le blocage `qrcodes` du rapport 42
survenait avant, au fichier 114/147, alors que `tva_categories` est le fichier 137/147). C'est la
**première fois** que ce test s'exécute contre une base où `tva_categories` porte son plein contenu
réel — révélant que l'hypothèse implicite du test (« aucune autre catégorie que les miennes n'existe »)
ne tient plus dès que la table de référence est complètement chargée. Sur les 9 lignes obtenues,
seulement 3 correspondent aux catégories insérées par le test — les autres proviennent des catégories
réelles déjà présentes dans le chargement en masse (aucune valeur réelle citée ici, uniquement le
comptage).

**Ce n'est vraisemblablement pas un bug de migration ni une divergence Postgres/MySQL** : la requête est
neutre vis-à-vis du dialecte (mêmes `?`, même structure `LEFT JOIN`/`UNION`, aucune fonction
spécifique à un moteur), le même comportement serait attendu à l'identique sur MySQL avec un
`tva_categories` tout aussi complet — c'est une hypothèse du fixture de test (base quasi-vide au
moment de son écriture) invalidée par la complétude désormais atteinte du chargement, pas quelque chose
introduit par cette session de travail sur `qrcodes`/`planning_shifts`/`parent_order_id`.

**Aucune correction apportée** — ni à la requête, ni au test, ni au schéma — conformément à la
consigne. Signalé ici pour arbitrage dans une prochaine session (scoper la requête par marchand/caisse,
ou scoper le test pour qu'il ne dépende plus du contenu global de `tva_categories`).

## 7. Nettoyage

Les 147 fichiers `.sql` régénérés, leur copie dans le conteneur (`/tmp/load`, `/tmp/*.out`), les
artefacts d'analyse locaux (requêtes de comptage, rapport JSON de génération) ont été supprimés. Les 5
lignes de test résiduelles sur `hours_of_operation` (même bug d'ordonnancement `defer`/`t.Cleanup` que
le rapport 42 §4.3, non corrigé — hors périmètre) ont été nettoyées manuellement. `labels` (−3) et
`tva_categories` (−1) restent dans l'état atteint par les tests d'intégration dans cette instance de dev
jetable (§3.1) — aucune remise à zéro supplémentaire demandée après la dernière vérification. **Rien
n'a été commité.**

## 8. Synthèse

| Étape | Résultat |
|---|---|
| Correctif `qrcodes.QR_id` | Appliqué — `_setval_columns_for` utilise désormais la forme repliée minuscule, pas la casse du dump |
| Généralisation | 83 colonnes identity balayées, 1 seule à casse mixte (`qrcodes.QR_id`), correctif uniforme, 0 occurrence à casse mixte restante sur les 59 `setval()` générés |
| Confirmation `labels` | **Confirmée intégralement**, reproduite en isolation dans cette session (même conteneur/port), delta exact −3, structure exacte, 0 part inexpliquée. Second exemple similaire découvert (`tva_categories`, −1, même mécanisme) |
| Régénération | 147/147, 0 échec, 472 774 lignes (inchangé) |
| Chargement complet | **147/147 fichiers chargés avec succès — première répétition sans blocage de la série** |
| Comptages | 147/147 tables exactes, 0 écart, 472 774 lignes (avant tout test Go) |
| Resynchronisation séquences | `orders` et `qrcodes` toutes deux exactement resynchronisées (`is_called = true`, `last_value` = max réel) |
| Requêtes applicatives Go | 6/7 PASS (`GetOrder`, `GetUserByToken`, `ComputePOSStatus`, `planning/schedule`, `InsertMerchant`→`qrcodes`, `CreateOrder`→`orders`) |
| Nouvelle découverte | `GetCashRegisterReport` : la requête Postgres fonctionne correctement ; c'est l'hypothèse du test (`tva_categories` quasi-vide) qui ne tient plus une fois la table de référence complètement chargée — vraisemblablement pas un bug de migration, non corrigé, signalé pour arbitrage |
| Fichiers commités | Aucun |

**Le chargement complet (147/147) est désormais atteint et reproductible.** Point ouvert pour la suite :
arbitrer le scope de `GetCashRegisterReport`/`TestGetCashRegisterReport_Postgres` vis-à-vis de
`tva_categories` (table de référence globale) — hors périmètre de ce rapport, qui se limitait à
`qrcodes`/`labels` et à la vérification de complétude du chargement.
