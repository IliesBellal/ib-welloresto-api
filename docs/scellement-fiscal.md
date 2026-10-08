# Comment fonctionne le scellement fiscal (hash) de WelloResto

Document de référence technique, pour comprendre et expliquer le mécanisme sans avoir à relire le code. Il décrit **ce qui est en production dans le code actuel** (`internal/fiscal`), pas l'historique du chantier. Pour le contexte réglementaire et les décisions prises lot par lot, voir `docs/attestation-conformite-*.md` ; ce document-ci ne parle que du mécanisme lui-même.

## En une phrase

Chaque ligne fiscale importante (un paiement, un ticket, une fermeture de caisse, une entrée du journal, une clôture, une archive) porte, en plus de ses propres colonnes, l'empreinte de la ligne qui la précède dans sa chaîne. Toucher à une seule donnée déjà écrite — ou intercaler, supprimer, rejouer une ligne — change son empreinte recalculée, ce qui casse visiblement la chaîne pour tout ce qui suit. Le contrôle d'intégrité ne fait rien d'autre que relire les données et refaire ce calcul pour vérifier qu'il retombe sur les valeurs stockées.

## Ce que ça prouve, ce que ça ne prouve pas

- **Ça prouve** qu'une ligne scellée n'a pas changé depuis son écriture, et que l'ordre d'écriture des lignes d'une même chaîne, pour un même établissement, n'a pas été modifié après coup (pas de ligne retirée, insérée au milieu, ou rejouée dans un autre ordre).
- **Ça prouve aussi** que c'est bien le serveur WelloResto, détenteur de la clé secrète, qui a produit cette empreinte — pas n'importe qui ayant un accès en écriture à la base (voir « Signature » plus bas).
- **Ça ne prouve pas** qu'une donnée était correcte au moment où elle a été scellée. Si une commande a été mal saisie puis close, son empreinte sera parfaitement valide et pourtant la vente sera fausse. Le scellement garantit l'**inaltérabilité après coup**, pas l'exactitude de la saisie initiale.
- **Ça ne protège pas** un accès direct et malveillant à la base qui disposerait aussi de la clé secrète (`FISCAL_SIGNING_KEY`) : cette clé doit rester hors de portée de quiconque n'a pas besoin d'écrire dans ces tables.

## 1. Les deux opérations : empreinte et signature

Chaque ligne scellée porte trois valeurs : `previous_hash` (ou `prev_hash` selon la table, voir plus bas), `hash`, `signature`.

### L'empreinte (`hash`)

```go
// internal/fiscal/seal.go
func Seal(chain Chain, prev string, data any) (hash, signature string, err error) {
    b, _ := json.Marshal(envelope{Chain: chain, V: HashVersion, Prev: prev, Data: data})
    sum := sha256.Sum256(b)
    hash = fmt.Sprintf("%x", sum)          // 64 caractères hexadécimaux
    return hash, security.SignHash(hash), nil
}
```

`hash` est le **SHA-256** (en hexadécimal) du JSON de l'enveloppe `{chain, v, prev, data}` — voir la section suivante pour le détail de cette enveloppe. N'importe qui qui a les mêmes données peut recalculer ce SHA-256 et vérifier qu'il correspond, sans avoir besoin d'aucune clé secrète : c'est un **engagement sur le contenu**, pas une preuve d'origine.

### La signature (`signature`)

```go
// internal/utils/security/hash_signing.go
func SignHash(dataHash string) string {
    key := []byte(os.Getenv("FISCAL_SIGNING_KEY"))
    h := hmac.New(sha256.New, key)
    h.Write([]byte(dataHash))
    return fmt.Sprintf("%x", h.Sum(nil))   // 64 caractères hexadécimaux
}
```

`signature` est un **HMAC-SHA256 de l'empreinte**, avec `FISCAL_SIGNING_KEY` comme clé. Deux niveaux distincts :

1. `hash` = engagement sur le contenu (public, vérifiable par tout le monde) ;
2. `signature` = preuve que c'est le serveur, détenteur de la clé, qui a produit cet engagement.

Sans la clé, on peut vérifier qu'une donnée correspond à son empreinte, mais on ne peut pas fabriquer une nouvelle ligne valide (empreinte *et* signature cohérentes) de toutes pièces, même en ayant un accès direct en écriture à la base.

## 2. L'enveloppe scellée et le JSON canonique

### L'enveloppe

```go
type envelope struct {
    Chain Chain  `json:"chain"`   // nom de la chaîne, ex. "payments"
    V     int    `json:"v"`       // fiscal.HashVersion, actuellement 2
    Prev  string `json:"prev"`    // empreinte du maillon précédent
    Data  any    `json:"data"`    // la charge utile propre à la chaîne (voir section 4)
}
```

Le nom de la chaîne (`chain`) entre dans le calcul : la même charge utile scellée dans deux chaînes différentes donnerait deux empreintes différentes. C'est voulu — une ligne d'une chaîne ne peut jamais être confondue avec une ligne d'une autre.

### Pourquoi une forme « canonique » du JSON

Les colonnes `jsonb` de Postgres (`tax_details`, `items_snapshot`, `payments_snapshot`, `old_values`, `new_values`) ne conservent pas le texte original : Postgres réordonne les clés, retire les espaces, peut réécrire un nombre (`1e-06` devient `0.000001`). Si l'empreinte était calculée depuis le texte JSON brut envoyé à l'écriture, puis recalculée depuis le texte relu en base, elles pourraient légitimement différer sans qu'aucune donnée n'ait changé — un faux positif.

`fiscal.Canonical()` règle ça : il décode le JSON (`UseNumber`, pour garder la précision exacte des nombres), puis le réécrit avec :

- les clés d'objet **triées alphabétiquement** ;
- **aucun espace** superflu ;
- les nombres en **décimal exact minimal** (pas de notation scientifique, pas de zéro inutile) ;
- un contenu vide (`nil` ou chaîne vide) devient le littéral `null`.

Le même contenu, stocké puis relu, retombe donc toujours sur le même texte canonique, quelle que soit la façon dont Postgres l'a reformaté entre-temps.

### Les horodatages

```go
func Now() time.Time        { return time.Now().UTC().Truncate(time.Microsecond) }
func FormatTime(t time.Time) string {
    return t.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
}
```

Tout horodatage entrant dans une empreinte est tronqué à la microseconde — la précision native d'un `timestamptz` Postgres — avant d'être scellé. Sans cette troncature, une valeur Go à la nanoseconde près pourrait perdre en précision à l'écriture en base puis ne plus correspondre à la relecture.

## 3. Le chaînage

Chaque table scellée porte ces colonnes (le nom de la colonne « parent » varie d'une table à l'autre, voir le tableau de la section 4) :

| Colonne | Rôle |
|---|---|
| `previous_hash` (ou `prev_hash` sur `receipts`) | empreinte du maillon précédent de la **même chaîne, du même établissement** |
| `hash` | empreinte de cette ligne |
| `signature` | HMAC de `hash` |
| `hash_version` | formule qui a produit `hash`/`signature` — `2` aujourd'hui, `1` = historique (voir section 6) |

**Premier maillon.** Le tout premier maillon d'une chaîne, pour un établissement donné, n'a pas de parent réel : son `previous_hash` vaut le sentinelle `GENESIS_HASH` (`fiscal.PrevOrGenesis`, appelé si la lecture du dernier maillon ne renvoie rien).

**Un verrou par chaîne et par établissement.** Avant de lire le dernier maillon et d'en écrire un nouveau, le code prend un verrou consultatif Postgres :

```go
// fiscal.LockChain
SELECT pg_advisory_xact_lock(hashtextextended('fiscal:<chaîne>:<établissement>', 0))
```

Ce verrou est **tenu jusqu'au commit** de la transaction. Il sert à éviter qu'en lecture non verrouillée, deux écritures concurrentes relisent la même « dernière » ligne et chaînent toutes les deux dessus — ce qui créerait une fourche (deux lignes différentes avec le même parent). Avec le verrou, la deuxième écriture attend que la première ait commité, puis relit un parent à jour.

**Ordre d'acquisition des verrous.** Pour éviter tout interblocage entre chaînes (une mutation de commande peut toucher plusieurs chaînes dans la même transaction), l'ordre est toujours : `payments`, `orders`, `receipts`, `cash_registers`, `audit_logs`, `fiscal_closures`, `fiscal_archives`. La clôture fiscale et l'archive ne prennent jamais d'autre verrou de chaîne que le leur.

## 4. Les six chaînes

Pour chaque chaîne : où elle s'écrit, quelles colonnes de la table portent le chaînage, quels champs entrent dans l'empreinte (la charge utile `Data` de l'enveloppe), et — point important — **quelles colonnes en sont délibérément exclues** parce qu'elles changent légitimement après coup.

### `payments`

| | |
|---|---|
| Écrite par | `OrdersLifeCycleRepository.addPaymentAndReturnID` (`internal/modules/order_life_cycle/repository.go`) |
| Colonnes de chaînage | `previous_hash`, `hash`, `signature`, `hash_version` |
| Ordre des maillons | `payment_date`, puis `payment_id` |

Charge utile (`fiscal.PaymentPayload`) :

| Champ JSON | Colonne source |
|---|---|
| `merchant_id` | `merchant_id` |
| `order_id` | `order_id` |
| `amount` | `amount` |
| `mop` | `mop` |
| `operation_type` | `operation_type` |
| `payment_date` | `payment_date` (via `FormatTime`) |
| `user_id` | `user_id` |
| `comment` | `comment` (peut être absent) |

**Exclu de l'empreinte** (changent légitimement après le scellement) : `enabled` (annulation), `cash_register_id` (rattaché au registre à sa fermeture), `fee`, `net_amount`, `status_check` (écrits par le webhook Stripe).

### `receipts` (tickets et avoirs)

| | |
|---|---|
| Écrite par | `receiptService.sealReceipt` (`internal/modules/receipt/service.go`) |
| Colonnes de chaînage | `prev_hash` **(nom différent des autres tables)**, `hash`, `signature`, `hash_version` |
| Ordre des maillons | `created_at`, puis `receipt_number` |

Charge utile (`fiscal.ReceiptPayload`) :

| Champ JSON | Colonne source |
|---|---|
| `merchant_id` | `merchant_id` |
| `receipt_number` | `receipt_number` |
| `order_id` | `order_id` |
| `created_at` | `created_at` (via `FormatTime`) |
| `total_ttc` | `total_ttc` |
| `total_ht` | `total_ht` |
| `tax_details` | `tax_details` (jsonb, via `Canonical`) |
| `items` | `items_snapshot` (jsonb, via `Canonical`) |
| `payments` | `payments_snapshot` (jsonb, via `Canonical`) |

Un ticket est **entièrement auto-porté** : tout ce qui entre dans son empreinte est stocké sur sa propre ligne. Aucune autre table n'est relue au moment de vérifier un ticket. Un avoir est un ticket comme un autre, avec `total_ttc` négatif.

### `cash_registers` (fermeture de caisse)

| | |
|---|---|
| Écrite par | `CloseCashRegister` (`internal/modules/cash_registers/repository.go`), charge construite par `fiscal.LoadCashRegisterClosure` |
| Colonnes de chaînage | `previous_hash`, `hash`, `signature`, `hash_version` |
| Ordre des maillons | `end_date` |

Charge utile (`fiscal.CashRegisterClosurePayload`) :

| Champ JSON | Colonne source |
|---|---|
| `cash_register_id` | `cash_registers.cash_register_id` |
| `merchant_id` | `cash_registers.merchant_id` |
| `start_date` | `cash_registers.start_date` |
| `end_date` | valeur sur le point d'être écrite (passée en paramètre) |
| `cash_fund` | `cash_registers.cash_fund` |
| `final_cash_fund` | valeur sur le point d'être écrite (passée en paramètre) |
| `items` | **lignes de `cash_registers_items`** (`mop`, `amount`), relues au moment du scellement |

⚠️ **C'est la seule des six chaînes qui n'est pas auto-portée.** Les quatre autres lisent uniquement les colonnes de leur propre ligne ; celle-ci relit en direct la table `cash_registers_items`. Conséquence pratique :

- si une ligne de `cash_registers_items` est modifiée **après** la fermeture du registre, l'empreinte recalculée ne correspondra plus — c'est voulu, c'est exactement ce que ce mécanisme doit détecter ;
- si un jour la fonction `LoadCashRegisterClosure` elle-même change de logique (nouvelles colonnes lues, calcul différent), **tous les registres déjà fermés** recalculeraient une empreinte différente de celle stockée, même si aucune donnée n'a bougé. C'est le point le plus fragile du système : tout changement de cette fonction doit être traité comme un changement de formule (voir `hash_version`, section 6), jamais comme une simple correction de bug silencieuse.

**Exclu de l'empreinte** : `enclosed`, `closed_by`, `closure_comment`, et les relevés `cash_registers_custom_items` saisis après la fermeture.

### `audit_logs` (journal d'audit)

| | |
|---|---|
| Écrite par | `fiscal.AppendAuditLog` / `AppendAuditLogs` (`internal/fiscal/audit.go`) |
| Colonnes de chaînage | `previous_hash`, `hash`, `signature`, `hash_version` |
| Ordre des maillons | `created_at`, puis `id` |

Charge utile (`fiscal.AuditLogPayload`) :

| Champ JSON | Colonne source |
|---|---|
| `id` | `id` |
| `merchant_id` | `merchant_id` |
| `user_id` | `user_id` |
| `action` | `action` |
| `resource_type` | `resource_type` |
| `resource_id` | `resource_id` |
| `created_at` | `created_at` (via `FormatTime`) |
| `old_values` | `old_values` (jsonb, via `Canonical`) |
| `new_values` | `new_values` (jsonb, via `Canonical`) |

Action notables écrites ici : `ORDER_UPDATE`, `ORDER_REOPEN`, `ORDER_CLOSE`, `ORDER_REFUND`, `PAYMENT_CANCELLED`, `FISCAL_ARCHIVE_DOWNLOAD`, `ATTESTATION_GENERATED`, `ATTESTATION_DOWNLOAD`, `ATTESTATION_SENT`… C'est le journal générique : chaque fois qu'une autre chaîne a besoin de laisser une trace qui n'a pas sa place dans ses propres colonnes (un téléchargement, une génération de document), c'est ici.

Un lot de plusieurs entrées écrites dans un même appel (`AppendAuditLogs`) est daté à la microseconde près d'écart entre chaque entrée, pour que l'ordre `(created_at, id)` du dernier maillon suive exactement l'ordre du chaînage.

### `fiscal_closures` (clôtures journalières, mensuelles, annuelles)

| | |
|---|---|
| Écrite par | `fiscal.insertClosure` (`internal/fiscal/closures.go`), appelée par `closeDay` / `closeAggregate` |
| Colonnes de chaînage | `previous_hash`, `hash`, `signature`, `hash_version` |
| Ordre des maillons | `closed_at`, puis `id` |

Charge utile (`fiscal.ClosurePayload`) — entièrement auto-portée, tout est stocké sur la ligne de clôture elle-même :

| Champ JSON | Contenu |
|---|---|
| `merchant_id`, `period_type` (`DAY`/`MONTH`/`YEAR`), `period_start`, `period_end`, `timezone`, `closed_at` | identité de la période |
| `sales_ttc`, `sales_ht`, `refunds_ttc`, `refunds_ht`, `net_ttc`, `net_ht` | chiffre d'affaires de la période |
| `vat_by_rate` | ventilation de TVA par taux |
| `payments_by_mop` | totaux des paiements par moyen, type, état |
| `by_channel` | chiffre d'affaires net par canal (caisse, borne, ScanNOrder, plateformes) |
| `receipts_count`, `first_receipt_number`, `last_receipt_number` | numérotation des tickets de la période |
| `orders_count`, `orders` | **uniquement pour une clôture `DAY`**, voir ci-dessous |
| `opening` | **uniquement la toute première clôture** de l'établissement : valeur d'ouverture (tickets antérieurs) |
| `grand_total_period`, `perpetual_total` | cumuls (année civile, depuis la première clôture) |

**Le cas particulier des commandes.** La chaîne par commande du lot A a été retirée (lot B) : les commandes ne sont plus chaînées individuellement. À la place, la clôture journalière scelle, pour chaque commande close ce jour-là, une **empreinte imbriquée sans chaînage propre** :

```go
// Dans la clôture DAY :
OrderFingerprint{ OrderID, Status, Hash }
// Hash = fiscal.Fingerprint("order_closure", OrderClosurePayload{...})
```

`Fingerprint()` est une empreinte SHA-256 simple (pas de HMAC, pas de parent) de `{kind: "order_closure", v: 2, data: <contenu de la commande au moment de la clôture>}`. Ce tableau d'empreintes (`orders`) fait partie des colonnes de la ligne `fiscal_closures` elle-même : le chaînage de `fiscal_closures` (section 7, point 1) garantit juste que **ce tableau stocké n'a pas changé** après coup — pas qu'il correspond encore à l'état actuel des commandes.

⚠️ **La distinction compte.** Si une commande est modifiée directement en base *après* la clôture de son jour, la ligne `fiscal_closures` n'en sait rien : son `hash` reste valide, puisqu'il porte sur le tableau `orders` tel que stocké, pas sur l'état courant des commandes. Ce qui détecte ce cas précis n'est **pas** le chaînage, mais un **contrôle croisé séparé** (section 7, point 4) : le vérificateur recalcule en direct, depuis les données actuelles, ce que la clôture du jour *devrait* être (`fiscal.ComputeDayClosure`), et compare chaque empreinte de commande fraîchement recalculée à celle figée dans la clôture stockée. Un écart est rapporté comme « commande modifiée après scellement » — une anomalie métier, pas un échec de signature.

**Exclu de l'empreinte des clôtures** : rien n'est exclu au sens « colonne qui change après coup », puisque `fiscal_closures` n'est jamais modifiée après écriture — une clôture n'est écrite qu'une fois.

### `fiscal_archives`

| | |
|---|---|
| Écrite par | `fiscalarchive.Generate` (`internal/fiscalarchive/archive.go`) |
| Colonnes de chaînage | `previous_hash`, `hash`, `signature`, `hash_version` |
| Ordre des maillons | `generated_at`, puis `id` |

Charge utile (`fiscalarchive.Payload`) :

| Champ JSON | Contenu |
|---|---|
| `merchant_id`, `kind` (`MONTH`/`PERIOD`), `period_start`, `period_end`, `timezone` | identité de l'archive |
| `filename` | nom du fichier |
| `sha256` | empreinte **du fichier ZIP** tel que déposé dans le stockage privé |
| `manifest_sha256` | empreinte du `MANIFEST.json` contenu dans le ZIP (qui liste l'empreinte de chaque CSV) |
| `size_bytes`, `software_version`, `generated_by`, `generated_at` | métadonnées de génération |

Note : l'empreinte de la chaîne ne couvre pas le contenu du ZIP octet par octet, elle couvre son `sha256`. Si le fichier stocké dans le bucket privé était remplacé par un autre, son `sha256` recalculé ne correspondrait plus à celui de cette ligne — c'est ce que vérifie le contrôle d'intégrité en relisant le fichier (voir section 7).

## 5. Récapitulatif en un tableau

| Chaîne | Table | Colonne « parent » | Auto-portée ? | Imbrique |
|---|---|---|---|---|
| `payments` | `payments` | `previous_hash` | oui | — |
| `receipts` | `receipts` | `prev_hash` | oui | — |
| `cash_registers` | `cash_registers` | `previous_hash` | **non** (relit `cash_registers_items`) | — |
| `audit_logs` | `audit_logs` | `previous_hash` | oui | — |
| `fiscal_closures` | `fiscal_closures` | `previous_hash` | oui | l'empreinte de chaque commande close du jour (`Fingerprint`, sans chaînage propre) |
| `fiscal_archives` | `fiscal_archives` | `previous_hash` | oui (le `sha256` du fichier, pas le fichier lui-même) | — |

## 6. `hash_version` : la formule n'est pas figée

`fiscal.HashVersion` vaut **2** aujourd'hui, et ce chiffre est écrit tel quel dans l'enveloppe à chaque scellement (`V: HashVersion`). La colonne `hash_version` de chaque ligne enregistre quelle formule a produit son `hash`/`signature`.

- **`hash_version = 1`** : lignes antérieures à ce chantier de conformité. Leur formule ne couvrait pas tout ce qu'exige le §50 du BOI, et n'était pas forcément signée par clé secrète. **Ces lignes ne sont jamais recalculées** : le contrôle d'intégrité vérifie seulement leur chaînage (pas de trou, pas de fourche), jamais leur empreinte, et toute anomalie y reste un **avertissement**, jamais une erreur bloquante.
- **`hash_version = 2`** : formule actuelle, celle décrite dans ce document. Une anomalie sur une ligne v2 est une **erreur**.

**Ce mécanisme est conçu pour encaisser un futur changement de formule.** Si `fiscal.HashVersion` passait un jour à `3` (nouvelle formule, par exemple pour couvrir un nouveau besoin), toutes les lignes `hash_version = 2` actuelles basculeraient automatiquement dans le même traitement que les `hash_version = 1` aujourd'hui : chaînage vérifié, empreinte non recalculée, avertissement seulement. C'est pour ça qu'il ne faut **jamais** modifier en place la logique de calcul d'une charge utile déjà en production (par exemple `LoadCashRegisterClosure`) sans se poser la question : est-ce que ça change la formule ? Si oui, ça doit s'accompagner d'une incrémentation de `hash_version`, pas d'une correction silencieuse.

Il n'y a aujourd'hui **aucun lien automatique dans le code** entre `fiscal.HashVersion` et `internal/version.Version` (la version commerciale du logiciel, actuellement 2.1.6) : ce sont deux constantes indépendantes. Dans l'esprit du chantier, elles devraient évoluer ensemble — un changement des conditions d'inaltérabilité/sécurisation/conservation/archivage (donc potentiellement de `hash_version`) correspond à une nouvelle version majeure du logiciel et à une nouvelle attestation — mais rien ne le garantit mécaniquement aujourd'hui.

## 7. Comment la vérification s'en sert

Le contrôle d'intégrité (`internal/fiscalverify`, lancé par `cmd/verify_fiscal` ou par `POST /accounting/fiscal-integrity`) ne fait que rejouer ce qui précède, en lecture seule :

1. **Pour chaque ligne `hash_version ≥ 2`** : reconstruire exactement la même charge utile que celle utilisée à l'écriture, avec les **mêmes fonctions** (`NewPaymentPayload`, `NewReceiptPayload`, `LoadCashRegisterClosure`, `NewAuditLogPayload`, la relecture de `fiscal_closures` via `LoadClosure`, `fiscalarchive.PayloadOf`), rappeler `Seal(chain, previous_hash_stocké, payload)`, et comparer le résultat à `hash` et `signature` stockés. Un écart = **« empreinte recalculée différente »** : une donnée a changé après son scellement.
2. **Vérification du chaînage**, pour toutes les lignes quelle que soit leur version : pas deux lignes avec la même empreinte (fourche), le parent de chaque ligne existe bien dans la table (pas de maillon manquant), aucune ligne ne redémarre sur `GENESIS_HASH` ailleurs qu'au tout premier maillon (une chaîne tronquée puis redémarrée serait visible).
3. **Pour les archives**, si l'accès au stockage privé est fourni : relecture du fichier ZIP réel, comparaison de son `sha256` à celui scellé dans la ligne, puis contrôle croisé du contenu du ZIP lui-même (voir `docs/attestation-conformite-05-lot-D-brief.md`).
4. **Au-delà du hash** : le contrôle recoupe aussi des règles métier qui ne sont pas du scellement à proprement parler — numérotation des tickets sans trou ni doublon, clôtures recalculées depuis les tickets/paiements/commandes du jour et comparées aux clôtures stockées, mois et années égaux à la somme de leurs jours, chaque commande close recoupée avec ses tickets. Ces contrôles-là détectent des incohérences même quand toutes les empreintes sont par ailleurs valides.

## FAQ rapide

**Une correction de donnée faite directement en base casse-t-elle toujours le hash ?**
Oui, pour les cinq chaînes auto-portées (tout ce qui a été scellé est sur la ligne elle-même) dès que la correction touche une colonne qui entre dans l'empreinte. Pour `cash_registers`, une correction sur `cash_registers_items` après fermeture casse aussi l'empreinte, même si `cash_registers` elle-même n'est pas touchée — c'est le mécanisme qui fait exactement ce qu'il doit faire.

**Un changement de code (sans toucher à la base) peut-il casser des hash déjà écrits ?**
Pour les cinq chaînes auto-portées, non : la vérification relit uniquement les colonnes de la ligne, le code applicatif qui a servi à les *générer* au départ n'intervient plus. Pour `cash_registers`, oui potentiellement, si `LoadCashRegisterClosure` change de logique — voir section 4.

**Pourquoi staging montre des erreurs alors que rien n'est en production ?**
Les lignes `hash_version = 2` de staging ont été scellées pendant que ce chantier était encore en développement, donc avant que son code ne soit stabilisé. D'éventuelles corrections faites sur des données déjà scellées à cette période-là sont détectées comme prévu. Cela ne dit rien du fonctionnement du code actuel sur des données saisies avec lui.

**Qu'est-ce qui empêche de signer de fausses données avec un accès direct à la base ?**
Il faudrait connaître `FISCAL_SIGNING_KEY` pour produire une `signature` valide. Sans elle, on peut recalculer un `hash` (c'est du SHA-256 public), mais pas la `signature` HMAC qui l'accompagne — la vérification des deux ensemble échouerait.

## Glossaire express

| Terme | Sens |
|---|---|
| **Chaîne** | Suite ordonnée de lignes scellées d'une même table, pour un même établissement (`payments`, `receipts`, `cash_registers`, `audit_logs`, `fiscal_closures`, `fiscal_archives`) |
| **Maillon** | Une ligne de cette suite |
| **Empreinte (`hash`)** | SHA-256 du contenu scellé, vérifiable par tout le monde |
| **Signature** | HMAC-SHA256 de l'empreinte, clé secrète du serveur — prouve l'origine |
| **`GENESIS_HASH`** | Valeur conventionnelle du parent du tout premier maillon d'une chaîne |
| **Charge utile (payload)** | Les données propres à la chaîne qui entrent dans le calcul de l'empreinte |
| **JSON canonique** | Forme normalisée (clés triées, sans espace, nombres en décimal exact) qui rend l'empreinte indépendante du stockage |
| **`hash_version`** | Formule qui a produit `hash`/`signature` sur cette ligne — `1` = historique (jamais recalculé), `2` = actuelle |
| **Fingerprint** | Empreinte simple, sans chaînage ni signature, utilisée pour figer une donnée à l'intérieur d'une autre (la commande dans sa clôture journalière) |
