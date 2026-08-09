# 66 — Premier chargement de données réelles sur le Postgres de **production**

Date : 2026-08-09
Branche : `staging`

## Objectif

Charger un extrait frais de données réelles de production (`data-migration/migration_welloresto_data.sql`)
sur la **nouvelle instance Postgres de production** — une base neuve, structure déjà en place, jamais
chargée de données jusqu'ici — avec la même rigueur de vérification que les répétitions précédentes sur
staging Render ([rapport 51](51-render-staging-chunked-load.md),
[rapport 62](62-staging-fresh-data-rehearsal.md)). **Aucune donnée réelle n'est citée dans ce rapport,
et aucune information de connexion (hôte, port, identifiants) n'y figure.** Rien n'a été commité.

## 0. Notes de méthode

### 0.1 Confirmation préalable

Deux écarts ont été relevés avant toute connexion à l'instance de production et soumis à confirmation
explicite plutôt que traités par hypothèse :

- `POSTGRES_PROD_URL` n'était présente dans l'environnement d'aucun des deux shells d'outillage (Bash,
  PowerShell), alors que la consigne l'annonçait déjà exportée.
- Le « rapport 65 » cité par la consigne pour la référence de 192 tables n'existe pas dans
  `docs/migration-postgres/` (la série s'arrête à 63 au moment de cette session).

Confirmation obtenue : (1) lire la chaîne de connexion une seule fois depuis `.vscode/launch.json`
(fichier local couvert par `.gitignore`, jamais commité) et ne plus jamais la référencer que par le
chemin d'un fichier temporaire hors dépôt (`PGURL_FILE`) — même méthode que les
[rapports 51 §0](51-render-staging-chunked-load.md) et [62 §0](62-staging-fresh-data-rehearsal.md) ; (2)
traiter cette opération comme le véritable basculement de production (et non une répétition sur
staging), le chiffre de 192 tables étant alors vérifié en direct plutôt que pris pour acquis. Fichier
temporaire supprimé en fin de session (§7).

### 0.2 Écart de consigne relevé — sans conséquence

L'audit en lecture avant toute modification confirme **192 tables, 3 clés étrangères**, exactement la
valeur citée par la consigne (elle-même non vérifiable depuis le dépôt, cf. §0.1). Aucune dérive
structurelle analogue à celle documentée au rapport 62 (184 → 187) n'a été constatée ici.

## 1. Audit initial — base neuve confirmée

| Élément | Constaté |
|---|---|
| Tables (`information_schema.tables`, `BASE TABLE`, schéma `public`) | **192** |
| Clés étrangères (`pg_constraint.contype = 'f'`) | **3** |
| Lignes totales (192 tables) | **0** |
| Tables non vides | **0** |

Base neuve confirmée — aucune donnée préexistante, pas de condition d'arrêt déclenchée. Chargement
autorisé à se poursuivre.

`elapsed=9,37 s`

## 2. Génération des fichiers SQL depuis l'export frais

```
python data-migration/transform_mysql_csv.py generate-all-sql \
  --dump data-migration/migration_welloresto_data.sql --output-dir <hors dépôt>
```

| Élément | Valeur |
|---|---|
| Durée | **42,3 s** |
| Fichiers produits | **149** (`001_…sql` → `149_…sql`) |
| Lignes attendues au total | **489 574** |
| Tables porteuses de lignes | 137 |
| `failed_tables` | **aucune** |
| Tables ignorées (orphelines, hors schéma cible) | 33 |
| Tables ignorées (non mappées) | 1 — `user_status_view` |
| Tables nécessitant `OVERRIDING SYSTEM VALUE` | 59 |
| Lignes écartées (clé nulle) | 2, sur `orderitems` |
| Colonnes source écartées | `customer.is_migrated`, `orders.isDelivery` |

Décisions identiques à celles déjà actées aux rapports [30](30-final-orphan-tables-list.md) et
[35](35-dead-columns-removal.md) — aucun changement de comportement du générateur.

## 3. Chargement sur production

Chargeur en instructions séparées réimplémenté à l'identique de la spécification des rapports
[51](51-render-staging-chunked-load.md)/[62](62-staging-fresh-data-rehearsal.md) — il n'existait plus
dans l'arbre, supprimé en fin de session à chaque répétition précédente, conformément à la politique de
nettoyage : découpage de chaque fichier en instructions individuelles par une machine à états suivant
les littéraux `'…'` (échappement `''`) et les commentaires `--`, puis re-découpage défensif de toute
instruction `INSERT` dépassant `MAX_STATEMENT_BYTES` (2 Mio) en plusieurs `INSERT` partageant le même
en-tête de colonnes.

**Validation locale avant toute connexion à production** (`DRY_RUN`, aucun accès réseau) :

```
FILES=149 STATEMENTS=1515 OVERSIZE_REMAINING=0 ELAPSED=1.929s
```

**0 instruction restant au-dessus du seuil.**

Chargement effectif, fichier par fichier, sur la même connexion, arrêt immédiat prévu au premier échec
(pas de retry automatique) :

```
OK 1/149 001_allergens.sql …
…
OK 149/149 149_without.sql (10 statements) 237ms
FILES=149 STATEMENTS=1515 OVERSIZE_REMAINING=0 ELAPSED=4m12.367s
ALL_OK
```

**149/149 fichiers chargés, aucune erreur, aucun retry nécessaire. Durée : 4 min 12,4 s.**

## 4. Comptages exacts vs l'extrait généré

```
tables_expected(generated)=149   tables_actual_total=192
total_expected=489574   total_actual_generated_tables=489574   total_actual_ALL_192=489574
mismatches=0
out_of_scope_tables_count=43   out_of_scope_nonzero={}
```

| Contrôle | Résultat |
|---|---|
| Tables comparées ligne à ligne au rapport de génération | **149/149, 0 écart** |
| Total | **489 574 / 489 574** |
| Tables hors périmètre de génération (43) contenant des lignes | **0** — restées vides |
| Total base entière (192 tables) | 489 574 — exactement la somme attendue, aucune ligne parasite |

## 5. Vérifications applicatives réelles (repository layer, `DB_DIALECT=postgres`)

Programme Go autonome (jetable, supprimé en fin de session) appelant directement les mêmes
constructeurs et méthodes que l'application, sur des identifiants réels choisis à l'exécution dans les
données chargées (jamais imprimés), plus une opération d'écriture complète (marchand sentinelle
clairement marqué, nettoyé après coup) pour les vérifications qui nécessitent une insertion identity
réelle :

| # | Vérification | Module | Durée | Résultat |
|---|---|---|---|---|
| 1 | `GetOrder` (commande la plus fournie du marchand principal) | `orders` | 801 ms | ✅ PASS |
| 2 | `GetOrder` sur une commande porteuse d'emplacement (jointure `order_location`) | `orders` | 274 ms | ✅ PASS |
| 3 | `GetCashRegisterReport` | `cash_registers` | 494 ms | ✅ PASS |
| 4 | `GetUserByToken` (jeton volontairement inexistant) | `auth` | 46 ms | ✅ PASS |
| 5 | `FetchActiveSlots` + `ComputePOSStatus` | `openinghours` | 112 ms | ✅ PASS |
| 6 | `ListPlanningShiftsTeamWeekView` | `planning/schedule` | 80 ms | ✅ PASS |
| 7 | `GetAttributes` | `menu` | 164 ms | ✅ PASS |
| 8 | `InsertMerchant` (insertion identity réelle) | `pos` | 40 ms | ✅ PASS |
| 9 | `InitMerchantSatellites` (insertion identity réelle sur `qrcodes`, ×2) | `pos` | 339 ms | ✅ PASS |
| 10 | `CreateOrder` (insertion identity réelle sur `orders`/`orderitems`) | `order_life_cycle` | 405 ms | ✅ PASS |

**10/10 PASS.**

### 5.1 Deux incidents d'outillage, aucun défaut applicatif

Par souci de transparence, deux erreurs sont survenues **dans le programme de vérification jetable**,
pas dans le code applicatif ni dans les données chargées — documentées ici comme le rapport 62 l'avait
fait pour un incident comparable (§7.5) :

1. **Premier essai de `CreateOrder` en échec (`no_cash_register_open`)** : le programme de vérification
   n'avait pas seedé de ligne `cash_registers` ouverte pour le marchand sentinelle avant d'appeler
   `CreateOrder`, qui l'exige via `GetActiveCashRegisterID` dès qu'un `DeviceID` est fourni — comportement
   correct de l'application, oubli du script de test. Corrigé en ajoutant la ligne `cash_registers`
   manquante (même schéma que le test d'intégration `order_life_cycle`) ; le check est repassé PASS.
2. **Bug de nettoyage laissant temporairement 13 lignes sentinelles en production** : la fonction de
   nettoyage du même programme jetable appliquait un paramètre `merchantID` à une requête
   (`DELETE FROM device_link WHERE device_id = '…'`) qui n'en attend aucun, provoquant une erreur
   *« mismatched param and argument count »* qui interrompait le nettoyage avant qu'il ne s'exécute. Un
   recomptage complet des 192 tables a immédiatement révélé l'écart (489 587 au lieu de 489 574 attendu,
   soit exactement les 13 lignes du marchand sentinelle et de ses satellites — aucune autre table
   affectée). Le bug a été corrigé, le nettoyage rejoué isolément, puis vérifié :
   - Recomptage complet après correction : **489 574 lignes, 0 écart** — retour exact à l'état
     post-chargement.
   - `merchant` : 29 lignes (marchands réels uniquement) — le marchand sentinelle n'y figure plus.
   - Une passe complète et propre des 10 vérifications a ensuite été rejouée de bout en bout avec le
     programme corrigé (résultats du tableau ci-dessus), avec nettoyage automatique réussi cette
     fois (`CLEANUP_OK`, résidu re-vérifié nul à l'issue du même passage).

   Fenêtre d'exposition : les 13 lignes sentinelles (marchand + satellites, données de test clairement
   marquées — `SIRET`, e-mails et noms tous préfixés `itest-prod66`/`ZZZ ITEST…`, aucune donnée réelle
   impliquée) sont restées en base le temps de deux commandes de diagnostic en lecture seule avant
   d'être supprimées, sans jamais avoir été exposées à un flux applicatif réel.

## 6. Séquences identity — les 89 rattachées, pas seulement `orders`/`qrcodes`

Première difficulté : la requête initiale de rattachement séquence↔colonne (`pg_depend.deptype = 'a'`)
retournait 0 résultat. Cause : le schéma cible utilise des colonnes `GENERATED … AS IDENTITY` (norme
SQL), pas des `SERIAL` classiques — leur dépendance dans `pg_depend` est typée `'i'` (interne), pas
`'a'` (auto). Requête corrigée pour couvrir les deux types.

| Verdict | Nombre |
|---|---|
| Séquences rattachées à une colonne (`pg_depend`) | **89** |
| `last_value` **exactement égal** à `max(colonne)` | **48** |
| `last_value` strictement supérieur à `max(colonne)` | **9** |
| Table vide → séquence jamais appelée (prochain `nextval` = valeur de départ) | 32 |
| **Séquence en retard sur les données (collision au prochain `nextval`)** | **0** |

Les 9 séquences « en avance » (`merchant`, `qrcodes`, `orders`, `orderitems`, `products`,
`cash_registers`, `cash_desks`, `bookings_settings`, `merchant_marketing_settings`) correspondent très
précisément aux tables touchées par le marchand sentinelle des vérifications applicatives (§5) : leur
`nextval()` a avancé avant que les lignes ne soient supprimées — état normal et sans risque, aucune
collision possible avec les données réelles.

**0 séquence en retard sur les 89 rattachées : aucune ne provoquera de collision au prochain
`nextval()`.**

## 7. Nettoyage

Supprimés en fin de session : les 149 fichiers `.sql` régénérés et leur répertoire, le rapport JSON de
génération, le journal de chargement, les fichiers de comptage et de vérification des séquences, **le
fichier temporaire contenant la chaîne de connexion**, et les trois programmes Go jetables créés sous
`tools/` (`pgops`, `pgload`, `pgverify` — nécessaires sous le module pour importer les packages
`internal/…`, jamais commités). `git status` ne montre aucune trace de ces artefacts après nettoyage —
seules les modifications de travail déjà en cours avant cette session (fichiers listés en préambule de
session, non touchés) restent présentes.

## 8. Chronométrage — synthèse

| Étape | Durée mesurée |
|---|---|
| **1. Audit initial (192 tables, 0 ligne)** | **9,4 s** |
| **2. Génération des 149 fichiers SQL depuis l'export** | **42,3 s** |
| Validation locale du découpage en instructions (`DRY_RUN`) | 1,9 s |
| **3. Chargement des 149 fichiers sur production** | **4 min 12,4 s** |
| **4. Comptages exacts des 192 tables vs attendu** | **11,9 s** |
| **5. 10 vérifications applicatives (passage propre final)** | **3,6 s** |
| **6. Contrôle des 89 séquences identity** | **9,9 s** |
| **Total du cycle (audit → chargement → vérifications)** | **≈ 5 min 31 s** |

Comparable au rapport 62 (≈ 6 min 37 s sur staging Render, pour un extrait légèrement plus petit —
486 898 lignes contre 489 574 ici) : le chargement reste l'étape dominante (76 % du temps total), la
génération 13 %. Le temps consacré à la correction des deux incidents d'outillage (§5.1) n'est pas inclus
dans ce chronométrage — il ne concerne que le programme de vérification jetable, pas le pipeline de
chargement lui-même.

## 9. Synthèse

| Point | Résultat |
|---|---|
| Base neuve avant chargement | ✅ 192 tables, 3 FK, **0 ligne** — confirmé en lecture seule avant toute écriture |
| Génération depuis l'export frais | ✅ 149 fichiers, 489 574 lignes attendues, 0 échec, **42,3 s** |
| Chargement sur production | ✅ **149/149, ALL_OK**, 1 515 instructions, 0 instruction sur-taille, **4 min 12,4 s** |
| Comptages vs extrait | ✅ **489 574 / 489 574, 0 écart**, aucune ligne hors périmètre sur les 43 tables non générées |
| Vérifications applicatives | ✅ **10/10 PASS** au passage final, après correction de deux bugs du script jetable (§5.1) |
| Résidu de test en production | **0** au terme de la session — un résidu temporaire de 13 lignes sentinelles, détecté par recomptage et corrigé dans la même session, est documenté en toute transparence (§5.1) |
| Séquences identity | ✅ **89/89 rattachées vérifiées, 0 en retard** — bug de détection (`deptype`) trouvé et corrigé en cours de route |
| Écart de consigne relevé | Rapport 65 cité comme référence introuvable dans le dépôt ; le chiffre qu'il annonçait (192 tables) s'est néanmoins vérifié exact en direct |
| Fichiers `.sql` / identifiant de connexion / outillage temporaire | Tous supprimés en fin de session |
| Fichiers commités | Aucun |

**Premier chargement de données réelles sur le Postgres de production, vérifié de bout en bout : audit
initial, génération, chargement chunké, comptages exacts, vérifications applicatives réelles et
resynchronisation des 89 séquences identity — en ≈ 5 min 31 s de traitement effectif. Deux incidents
survenus dans l'outillage de vérification jetable (jamais dans le pipeline de chargement ni dans le code
applicatif) sont documentés en détail au §5.1, avec confirmation que l'état final de la base ne porte
aucune trace résiduelle.**
