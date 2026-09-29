# Import de carte par photo — Étape 1 : sous-produits sur Uber Eats et Deliveroo

Première étape du découpage de [cadrage-import-carte-photo-ia.md](cadrage-import-carte-photo-ia.md) (§ 5.8 et § 9). C'est un correctif indépendant : il règle un défaut déjà présent en production, et doit être livré **avant** que l'import photo crée des groupes de produits en masse.

**Statut :** implémenté et commité le 2026-09-29, non déployé (§ 5, § 6).

---

## 1. Le défaut

Les sous-produits d'un groupe (`products.by_product_of`) ne partent jamais sur Uber Eats ni Deliveroo, et le parent du groupe part à leur place comme un article vendable.

Chaîne de cause, vérifiée dans le code :

1. `SyncUberEatsMenu` et `SyncDeliverooMenu` ([service.go](../internal/modules/menu/service.go)) construisent la carte avec `GetMenuWithMarketingCategories`, qui appelle `GetMenu`.
2. `GetMenu` retire les enfants du premier niveau (`LEFT JOIN products subp … WHERE subp.product_id IS NULL OR …`) et les range dans `SubProducts` de leur parent.
3. [mapper_ubereats.go](../internal/modules/menu/mapper_ubereats.go) et [mapper_deliveroo.go](../internal/modules/menu/mapper_deliveroo.go) ne parcourent que `cat.Products`, jamais `SubProducts`.
4. **Deuxième cause, trouvée en préparant le correctif :** la requête des sous-produits de `GetMenu` (étape `sub_products`) ne lit pas `sync_uber_eats` ni `sync_deliveroo`, alors que celle des racines les lit. Même aplatis, les enfants auraient donc été ignorés par le mapper Uber Eats, qui exige `SyncUberEats == true`.

**Mesure sur staging** (2026-09-29, lecture seule, non représentatif de la production) :
- 95 groupes et 363 sous-produits, sur 8 marchands ;
- les 363 sous-produits sont marqués `sync_uber_eats` et `sync_deliveroo`, et aucun n'est envoyé ;
- les 95 parents sont envoyés comme articles, à des prix de 0 à 12 €.

---

## 2. Décisions

### D1 — Aligner les mappers sur l'aplatissement du kiosk
Même règle que `flattenKioskProducts` ([kiosk/service.go](../internal/modules/kiosk/service.go)) et `scannorder.ComputeGetMenu` :
- un produit **groupe** (`is_product_group = true`) n'est jamais envoyé lui-même ;
- ses sous-produits sont envoyés comme des articles à part entière, chacun avec ses propres filtres (drapeau de synchro, prix, disponibilité) ;
- un produit non groupe qui porterait quand même des sous-produits (donnée incohérente) est envoyé, ainsi que ses sous-produits, comme le fait le kiosk.

*Pourquoi :* c'est déjà le comportement de la borne et du Scan&Order ; les plateformes n'ont pas de notion de groupe d'affichage.

### D2 — Les sous-produits prennent la place du parent dans la catégorie
Le kiosk ajoute les sous-produits **en fin de liste**. Pour les plateformes, ils sont insérés **à la position du parent**, dans l'ordre déjà trié par `sortSubProducts`.

*Pourquoi :* sur Uber Eats et Deliveroo, l'ordre des articles dans une catégorie est l'ordre affiché au client. Envoyer « Coca-Cola Zero, Cherry… » à la fin de la catégorie « Boissons » déplacerait ces articles sans raison. L'écart avec le kiosk est volontaire et limité aux mappers.

### D3 — Lire les drapeaux de synchro pour les sous-produits dans `GetMenu`
Ajout de `p.sync_uber_eats, p.sync_deliveroo` à l'étape `sub_products`, lus exactement comme à l'étape `products_roots`.

*Effet de bord :* `GetMenu` alimente aussi la caisse, la borne et le Scan&Order. Les sous-produits gagnent deux champs JSON (`sync_ubereats`, `sync_deliveroo`), déjà présents sur les racines et marqués `omitempty`. L'ajout est additif, aucun client ne s'appuie sur leur absence.

### D4 — Le mapper Deliveroo applique désormais `sync_deliveroo` (ajout du 2026-09-29)
Constat : le mapper Deliveroo envoyait tous les produits, marqués ou non, alors que le mapper Uber Eats filtre sur `sync_uber_eats`. La case « Deliveroo » du back-office (fiche produit, modification en masse) n'avait donc aucun effet.

- *Premier arbitrage :* laissé hors périmètre, parce que le corriger retirerait des articles de la carte Deliveroo de certains marchands.
- *Décision finale d'Ilies :* le filtre est ajouté, car il est important. Même règle qu'Uber Eats : un produit ou sous-produit sans `sync_deliveroo = true` n'est pas envoyé.
- *Mesure sur staging* (lecture seule) : la colonne vaut `true` par défaut, et les 2 590 produits actifs sont marqués. Le filtre ne retire que les produits qu'un marchand a explicitement décochés.
- Si plus aucun produit n'est marqué, l'envoi échoue avec un message explicite (« none marked for Deliveroo sync? ») plutôt que d'envoyer une carte vide.

### D5 — Hors périmètre, constaté mais non corrigé
- **Un sous-produit dont le parent est désactivé disparaît partout.** La jointure des racines l'exclut, et le rattachement échoue faute de parent. C'est le comportement actuel de tous les canaux, inchangé ici.

---

## 3. Effet pour les marchands concernés

L'envoi vers les plateformes est **manuel** (`PATCH /menu/uber-eats/sync` et `/menu/deliveroo/sync`, depuis le back-office). Au prochain envoi après déploiement, pour un marchand qui a des groupes :
- les articles « groupe » disparaissent de sa carte Uber Eats ou Deliveroo ;
- les sous-produits apparaissent, à la place du groupe.

Et pour un marchand qui a décoché « Deliveroo » sur certains produits : ces produits disparaissent de sa carte Deliveroo au prochain envoi (D4).

Il faut prévenir ces marchands, ou vérifier avec eux que les sous-produits sont prêts à la vente (prix livraison, photos). Pour identifier les produits décochés qui partaient quand même sur Deliveroo :

```sql
SELECT merchant_id, COUNT(*) AS produits_retires_de_deliveroo
FROM products
WHERE enabled AND status <> 'removed_from_menu' AND sync_deliveroo IS NOT TRUE
GROUP BY merchant_id ORDER BY 2 DESC;
```

Pour identifier les marchands qui ont des sous-produits à envoyer :

```sql
SELECT c.merchant_id, COUNT(*) AS sous_produits_a_envoyer
FROM products c JOIN products p ON p.product_id = c.by_product_of
WHERE c.enabled AND c.status <> 'removed_from_menu' AND (c.sync_uber_eats OR c.sync_deliveroo)
GROUP BY c.merchant_id ORDER BY 2 DESC;
```

---

## 4. Implémentation

| Fichier | Changement |
|---|---|
| [repository.go](../internal/modules/menu/repository.go), `GetMenu`, étape `sub_products` | Ajout de `p.sync_uber_eats, p.sync_deliveroo` au `SELECT`, lus en `sql.NullBool` et posés sur `SyncUberEats` / `SyncDeliveroo`, comme à l'étape `products_roots` (D3) |
| [mapper_platform_products.go](../internal/modules/menu/mapper_platform_products.go) (nouveau) | `platformProducts` : aplatissement d'une catégorie, groupes remplacés par leurs sous-produits à leur position (D1, D2) |
| [mapper_ubereats.go](../internal/modules/menu/mapper_ubereats.go), [mapper_deliveroo.go](../internal/modules/menu/mapper_deliveroo.go) | La boucle produits parcourt `platformProducts(cat.Products)` au lieu de `cat.Products`. Les filtres existants s'appliquent inchangés à chaque sous-produit |
| [mapper_deliveroo.go](../internal/modules/menu/mapper_deliveroo.go) | Filtre `SyncDeliveroo` (D4) ; message d'erreur explicite si plus rien n'est marqué |
| [mapper_platform_products_test.go](../internal/modules/menu/mapper_platform_products_test.go) (nouveau) | Tests unitaires, voir § 5 |
| [postgres_integration_test.go](../internal/modules/menu/postgres_integration_test.go) | `GetMenu` : le sous-produit existant (`prodC`) reçoit des drapeaux distincts (Uber vrai, Deliveroo faux) et le test vérifie qu'ils ressortent tels quels |

Le sous-produit garde son propre prix, sa photo, ses options et sa disponibilité livraison : `GetMenu` les enrichit déjà comme les racines.

**Note :** `repository.go` contenait déjà des modifications non commitées sans rapport (catégories marketplace, `CreateExternalProductTx`). Le correctif touche un bloc distinct du même fichier.

## 5. Vérifications

| Vérification | Résultat |
|---|---|
| `go build ./...` | OK |
| `go vet ./internal/modules/menu/` et `go vet -tags postgres_integration ./internal/modules/menu/` | OK |
| `go test ./internal/modules/menu/...` (tests unitaires) | OK |
| `TestPlatformProducts_GroupReplacedByItsSubProductsInPlace` | Le groupe est remplacé par ses 3 sous-produits, à sa position |
| `TestPlatformProducts_NonGroupWithSubProductsKeepsBoth` | Produit non groupe + sous-produits : les deux sont envoyés, comme au kiosk |
| `TestToUberEatsFormat_SendsSubProductsNotGroups` | Groupe absent ; sous-produits présents avec leur prix ; le sous-produit non marqué `sync_uber_eats` reste filtré |
| `TestToDeliverooFormat_SendsSubProductsNotGroups` | Groupe absent ; sous-produits présents avec leur prix ; le sous-produit non marqué `sync_deliveroo` est filtré (D4) |
| `TestToDeliverooFormat_NothingMarkedForSync` | Aucun produit marqué : erreur, pas de carte vide |
| Effet du filtre Deliveroo sur staging (lecture seule) | 2 590 produits actifs, tous marqués : aucun retiré |
| Requête `sub_products` modifiée, exécutée **en lecture seule sur staging** pour les 8 marchands qui ont des groupes | SQL valide ; 363 sous-produits renvoyés, drapeaux lus sur les 363 (aucun NULL) |
| `TestMenuRepository_Postgres` (tag `postgres_integration`) | **Non exécuté.** Il crée puis supprime ses propres données sur la base ciblée ; à lancer avec `POSTGRES_URL` pointé sur staging une fois cette portée validée |

| Chaque commit de code, isolément, dans un worktree propre (sans les modifications locales non commitées) | `go build ./...`, `go vet` (avec et sans tag d'intégration) et tests unitaires du module menu : OK sur les 3 commits |

## 6. Commits

Commits atomiques sur `staging`, autorisés pour ce chantier le 2026-09-29 :

| Commit | Contenu |
|---|---|
| `15ec7ee` | Note de cadrage |
| `144f27b` | `GetMenu` lit les drapeaux de synchro des sous-produits (D3) + test d'intégration |
| `21f5273` | Filtre `sync_deliveroo` dans le mapper Deliveroo (D4) + tests |
| `cda3f52` | Aplatissement des groupes dans les deux mappers (D1, D2) + tests |
| *(ce document)* | Doc de l'étape 1 |

`repository.go` contenait des modifications locales sans rapport (catégories marketplace). Seuls les blocs de ce correctif ont été commités, par patch partiel sur l'index ; les autres restent dans l'arbre de travail.

**Statut :** implémenté, vérifié et commité ; non poussé, non déployé. Après le déploiement : prévenir les marchands concernés (§ 3) avant leur prochain envoi vers les plateformes.
