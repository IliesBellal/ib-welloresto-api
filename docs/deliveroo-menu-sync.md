# Synchronisation du menu WelloResto vers Deliveroo

Objectif : le restaurateur gère son menu Deliveroo uniquement depuis
WelloResto. Les produits cochés « Deliveroo » sont publiés, et leur
disponibilité suit celle de WelloResto. Le code est pensé pour accueillir
Uber Eats ensuite, avec le même catalogue commun.

État au 2026-11-04 : étapes 1 à 4 codées (webhook, journal, constructeur de
menu, scénarios 13 et 15). Rien n'est commité. Les migrations 200 et 201 sont
à appliquer. L'étape 5 est à faire (voir « Reste à faire »).

## Vue d'ensemble

```
menu WelloResto (GetMenuWithMarketingCategories)
  + extras produits (price_deliveroo, contains_alcohol)
        │  marketplacecatalog.Build          ← commun à toutes les plateformes
        ▼
catalogue commun (produits, catégories, prix, TVA, options, disponibilité)
        │  deliveroo.BuildMenuDocument       ← propre à Deliveroo
        ▼
document Deliveroo ── empreinte identique au dernier succès ? → "unchanged", rien envoyé
        │
        ├─ correspondances import commandes (AVANT l'envoi)
        ├─ V1 : PUT /menu/v1/brands/{brand}/menus/main
        └─ V3 : URL présignée → dépôt S3 → job publish_menu_to_live
        ▼
webhook menu.upload_result (/webhooks/deliveroo/menu)
        │  CompleteMenuUpload
        ▼
journal = success  →  alignement des disponibilités de TOUS les articles publiés
```

Fichiers :

| Fichier | Rôle |
|---|---|
| `internal/modules/marketplacecatalog/catalog.go` | Catalogue commun (Deliveroo, Uber Eats). Fonctions pures. |
| `internal/modules/deliveroo/menu_document.go` | Catalogue → document Deliveroo, empreinte |
| `internal/modules/deliveroo/cover.go` | Image de couverture 1920×1080 |
| `internal/modules/deliveroo/publish.go` | Publication, fin de publication, alignement des disponibilités |
| `internal/modules/deliveroo/publish_repository.go` | Journal, correspondances, extras produits |
| `internal/webhook/deliveroo_menu/` | Webhook `menu.upload_result` |
| `migrations/todo/200_products_contains_alcohol.*` | Colonne `products.contains_alcohol` |
| `migrations/todo/201_marketplace_menu_syncs.*` | Journal des publications |

## Règles de publication (décisions)

- **Produits publiés** : cochés `sync_deliveroo`. Le chargement du menu exclut
  déjà `removed_from_menu` et les produits supprimés (`enabled = false`). Un
  produit groupe n'est jamais publié, ses sous-produits le remplacent.
  `available_delivery` n'est pas lu : Deliveroo a sa propre case.
- **Prix** : `price_deliveroo` s'il est renseigné (> 0), sinon `price_delivery`
  (> 0), sinon `price`.
- **TVA** : le vrai taux **livraison** du produit (`tva_delivery_id`), en
  pourcentage (`"10"`, `"5.5"`, `"20"`). Il n'est jamais déduit de l'alcool.
- **Alcool** : `products.contains_alcohol`, saisi par le restaurateur, sans
  détection automatique. Le bouton du back-office reste à faire.
- **Identifiants** :
  - article = identifiant produit, PLU identique ;
  - option = `opt-<id>`, PLU identique ;
  - groupe d'options = `mod-<id attribut>` ;
  - catégorie = `cat-<id>` ;
  - menu = `main`, mealtime = `main`.

  Les produits restent sans préfixe : la mise en rupture existante
  (`ToggleItemAvailability`) utilise déjà l'identifiant produit et le menu `main`.
- **Options** : une option partagée entre produits est publiée une fois, avec
  la TVA du premier produit qui l'utilise. `max_options` à 0 signifie sans
  limite : le nombre d'options, ou 99 si une option est répétable.
- **Mealtime** : un seul, ouvert 7j/7 de 00:00:00 à 23:59:59. Les horaires
  réels se gèrent sur le site Deliveroo. Nom et description = nom de
  l'établissement (`merchant.fullname`).
- **Textes** : clé de langue `en` seulement (`menuLocales`). C'est la seule
  clé déjà validée par le sandbox. **À vérifier** : faut-il ajouter `fr` pour
  les sites français ?
- **Nom d'article** : 2 à 120 caractères. Un nom plus court est ignoré et
  signalé dans `warnings` ; un nom plus long est tronqué. Description tronquée
  à 500 caractères.
- **Disponibilité de la catégorie de caisse** (`productcateg.available`) :
  non lue. Le chargement commun du menu ne la renseigne pas (le champ vaut
  toujours `false`).

## Image de couverture

Deliveroo impose une image par mealtime : JPEG ou PNG, au moins 1920×1080,
en 16:9.

- Source : la bannière ScanNOrder (`scannorder_settings.banner_url`). Elle est
  recadrée au centre en 16:9, mise à l'échelle 1920×1080 et convertie en JPEG
  (la bannière est souvent en WebP, refusé par Deliveroo).
- Sans bannière : visuel par défaut, logo WelloResto centré sur fond blanc. Le
  logo est embarqué (`internal/modules/deliveroo/assets/wello-resto-logo.png`,
  copie de `wello-resto-vitrine/public/logos/wello-resto-header-logo-desktop.png`).
  Clé R2 : `wello_resto_images_storage/deliveroo/default-cover-v1.jpg`. Changer
  le visuel = changer `defaultCoverVersion`.
- Clé R2 d'une couverture : `merchants/{id}/deliveroo/cover-<hash de l'URL>.jpg`.
  Une nouvelle bannière (autre `?v=`) donne une nouvelle couverture. Une
  bannière inchangée réutilise l'ancienne, sans regénération.
- Dépendance ajoutée : `golang.org/x/image v0.38.0` (lecture WebP,
  redimensionnement). C'est la version qui ne fait monter aucune autre dépendance.

## Correspondances pour l'import des commandes

L'import des commandes Deliveroo retrouve le produit par `pos_item_id` via
`integration_deliveroo_products_mapping`. **Un article inconnu y crée un
nouveau produit.** Avant chaque envoi, la publication écrit donc :

- `integration_deliveroo_products_mapping` (item_id = id produit → produit).
  Une autre correspondance active du même item_id vers un autre produit est
  désactivée : le menu publié fait foi.
- `integration_deliveroo_options_mapping` (item_id = `opt-<id>` → option),
  par upsert sur la clé unique `(merchant_id, item_id)`.

Les deux valeurs (id d'article et PLU) étant identiques, la correspondance
fonctionne que Deliveroo renvoie l'un ou l'autre dans `pos_item_id`.

## Journal `marketplace_menu_syncs`

Une ligne par tentative. `status` vaut `sending`, `processing`, `success`,
`failed` ou `unchanged`. On y trouve aussi :
- `upload_errors` : images refusées, codes-barres, erreur de traitement ;
- `item_ids` : articles produits publiés ;
- `availability_status` / `availability_error` : alignement des disponibilités.

`integration_deliveroo.last_sync` et `synced_items` sont mis à jour à chaque
succès.

Garde-fous :
- un envoi par minute et par site au maximum (limite Deliveroo). Sinon
  `ErrPublishTooSoon`, et HTTP 429 sur les routes ;
- un webhook arrivé avant l'enregistrement de la réponse à l'envoi n'est pas
  écrasé (`MarkSyncSent` ne touche qu'une ligne encore en `sending`).

## Disponibilités

| WelloResto `products.status` | Deliveroo |
|---|---|
| `available`, `1`, `true` | `available` |
| `out_of_stock` | `unavailable` (épuisé, remis disponible par Deliveroo à l'ouverture du site) |
| `not_available`, `0`, autre | `hidden` (masqué, persistant) |
| `removed_from_menu`, décoché, supprimé | absent du menu |

Après une publication réussie (webhook 200, ou réponse `MATCH_EXISTING_MENU`
sans webhook), l'état de **tous** les articles produits publiés est envoyé en
une seule requête (`POST item_unavailabilities`). WelloResto fait foi. Seuls
les articles du menu publié sont cités, car un identifiant inconnu fait
échouer toute la requête.

Deliveroo n'envoie **aucun** webhook de réactivation ni d'ouverture de site
(vérifié dans tout l'index de la doc le 2026-11-04). La remise à zéro
matinale (« quand le site ouvre ») n'est pas définie précisément par la doc.

## Scénarios de certification

Routes sans authentification, marchand de test `2` (`/test/deliveroo/...`) :

- **13** — `POST /test/deliveroo/13` publie le vrai menu du marchand 2 en V1.
  Deliveroo envoie ensuite `menu.upload_result` (environ une minute). Le
  webhook passe la ligne en `success` et pousse les disponibilités de tous les
  articles. C'est l'étape « update unavailabilities after the webhook ».
- **15** — `POST /test/deliveroo/15` publie le même menu en V3 : URL
  présignée, dépôt S3 immédiat (l'URL expire en quelques secondes), puis job
  `publish_menu_to_live` (paramètres `menu_id` + `version`). La réponse donne
  le `job_id`.
- **16** — `POST /test/deliveroo/16/{job_id}` donne l'état du job
  (`new` / `in_progress` / `success` / `failed`, `details`, `outcome`).

Le marchand 2 a 236 produits publiables, au-delà des 100 exigés par le
scénario 13. Il a au moins un produit épuisé et un masqué.

**Révision forcée** : les deux routes publient avec `Force`, qui ajoute
« rév. AAAAMMJJ-HHMMSS » à la description du mealtime. Le document est donc
toujours nouveau et Deliveroo envoie toujours le webhook, alors qu'un menu
identique répondrait `MATCH_EXISTING_MENU` sans webhook. Les scénarios restent
rejouables sans toucher au code, en respectant une minute entre deux envois.

Ordre : lancer le 13, attendre le webhook, puis le 15. Jamais les deux en même
temps : ils publient le même menu `main` sur le même site.

Contrôle, avant de valider dans le portail :

```sql
SELECT id, origin, api_version, status, http_status, item_count, job_id,
       availability_status, availability_error, error, upload_errors, created_at
FROM marketplace_menu_syncs
WHERE platform = 'deliveroo' AND merchant_id = '2'
ORDER BY id DESC LIMIT 5;
```

Les scénarios 4 et 9 à 12 gardent leur propre mini-menu de test (menu `3`).

## Bouton du back-office

`PATCH /menu/deliveroo/sync` (avec `location_id` en option) publie en V1. La
réponse conserve `status` et `message`, et ajoute `sync_status`
(`processing`, `success` ou `unchanged`), `sync_id`, `item_count` et
`warnings`. Un envoi trop rapproché répond 429 `publish_too_soon`. Le résultat
définitif est dans le journal.

## Ce qui a été retiré

- L'ancien mapper `menu/mapper_deliveroo.go` (format `menus[]` / `price_money`
  inconnu de Deliveroo) et `SyncMenu`, qui envoyaient un PUT sur `/menus` sans
  identifiant.
- Les menus jetables des scénarios 13 et 15.
- Le bricolage du webhook, qui mettait `item_1` en rupture. Il était inactif
  par accident, car l'ancien format de webhook ne correspondait pas au payload
  réel.
- `params.site_ids` du job V3, non documenté : les sites sont dans le document S3.

## Reste à faire (étape 5)

1. Application POS : le bouton « Disponibilité » envoie `out_of_stock` au lieu
   de `not_available` (`menu_api.dart`, `welloresto_menu_controller.dart`).
   Mise à jour forcée des applications avant la mise en ligne.
2. Mise en rupture immédiate : `ToggleItemAvailability` envoie encore
   `unavailable` pour `not_available`. Il faut passer au trio
   available/unavailable/hidden, et calculer l'envoi depuis l'**état courant**
   plutôt que depuis l'événement. Aujourd'hui, deux changements rapprochés
   partent dans deux goroutines sans ordre garanti.
3. File de réveils par compte, avec la migration suivante :
   - après un changement ;
   - après l'ouverture du site (horaires et fuseau lus via l'API Site,
     confirmation par `GET .../status` = `OPEN`) ;
   - un filet de sécurité toutes les 2 à 3 heures, étalé dans le temps.

   Chaque réveil lit l'état chez Deliveroo, le compare à WelloResto et envoie
   la différence. Pour Uber Eats, le webhook `store.status.changed` servira de
   déclencheur.
4. Republication automatique quand l'empreinte du menu change (produit retiré,
   prix, nom…), après quelques minutes de calme.
5. ScanNOrder : masquer `not_available`, seulement après avoir vérifié qu'il
   n'y a pas de régression et qu'Uber Eats se comporte de la même façon (sinon
   on garde le comportement actuel).
6. Choisir la V1 ou la V3 pour la production une fois le scénario 15 validé
   (la doc ne dit pas explicitement qu'un job V3 déclenche le webhook).
7. Back-office : bouton « contient de l'alcool » et affichage du journal.
