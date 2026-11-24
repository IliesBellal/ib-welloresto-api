# Banque d'images Wello Resto

Objectif : simplifier la création du menu en proposant des photos prêtes à l'emploi. Partout où le back-office demande une image (sauf les logos), un menu déroulant propose **« Depuis la banque d'images »** ou **« Depuis cet appareil »**. Le popup de la banque a deux onglets :
- **Banque d'images** : images communes à tous les établissements ;
- **Mes images** : images déjà utilisées par l'établissement.

## Décisions

| # | Décision |
|---|---|
| D1 | Images stockées dans le bucket R2 public, sous `wello_resto_images_storage/image_bank/{catégorie}/{slug}.{ext}`, servies par `https://storage.welloresto.fr`. Les miniatures passent par Cloudflare Image Transformations ([images-redimensionnement.md](images-redimensionnement.md)). |
| D2 | Métadonnées en base : table `image_bank` (migration `224_image_bank`). Liste fermée de catégories dans `internal/modules/imagebank/categories.go`. Deux formats : `square` (produit, option, catégorie) et `banner` (bannières). |
| D3 | Choisir une image de la banque **reprend l'URL telle quelle**. Le fichier est partagé par tous les établissements qui la choisissent. |
| D4 | L'API ne supprime **jamais** un fichier de la banque : la garde est dans `r2.Client.DeleteFile`, par où passent toutes les suppressions de « l'ancienne image » (remplacement, suppression d'image, retrait d'un média de borne ou d'écran CDS). Retirer une image de la banque = `enabled = false`, le fichier reste en place. |
| D5 | « Mes images » **copie** le fichier sous la clé de la cible (`CopyFile`, côté R2). Partager l'objet ne tiendrait pas : remplacer l'image du produit A effacerait celle du produit B. |
| D6 | Pas de nouvel endpoint d'écriture. Les endpoints d'upload existants acceptent aussi un corps JSON `{"image_url": "…"}` (package `internal/imagepick`). Seules les URLs de la banque (image active) ou des dossiers R2 de l'établissement (hors HACCP) sont acceptées. Toute autre URL est refusée (`400 invalid_image_url`). Une image retirée de la banque répond `404 image_not_found`. |
| D7 | La banque est alimentée uniquement par les scripts : `cmd/image_bank_fetch` (collecte automatique) puis `cmd/image_bank_import` (publication). Il n'y a pas de rôle « admin Wello » dans l'API pour un écran d'administration. |
| D8 | Toute image envoyée est **allégée** avant R2 (`internal/infrastructure/imageopt`, Go pur, aucun service externe) : plus grand côté limité à 2000 px, orientation EXIF appliquée, puis JPEG qualité 82, ou PNG si l'image a de la transparence. La version allégée n'est retenue que si elle est plus légère ; une image illisible est envoyée telle quelle. Le traitement s'applique au script d'import **et** à tous les uploads des restaurateurs : produits, options, catégories, catégories marketing, bannière ScanNOrder, hub franchise, veille borne, ruban borne, CDS. L'extension peut changer (un PNG opaque devient un JPEG). Les fichiers existants ne sont pas retraités. |
| D9 | Dupliquer un produit dont l'image vient de la banque garde la même URL (pas de copie). |
| D10 | Provenance obligatoire : chaque ligne du manifeste porte `source` et `license` (l'import refuse sinon), conservés en base (`source`, `license`, `author`, `source_url`). |

## API

| Route | Rôle |
|---|---|
| `GET /image-bank?format=square\|banner&category=…&q=…` | Images actives. La recherche ignore la casse et les accents, et porte sur le nom, les tags et la catégorie. Liste complète en cache Redis (`image_bank:v1:all`, 10 min, vidée par le script). |
| `GET /image-bank/categories` | Catégories, avec le nombre d'images par format. |
| `GET /image-bank/mine` | Images de l'établissement : produits, options, catégories, catégories marketing, bannière ScanNOrder, veille borne, rubans borne et CDS. Dédoublonnées ; banque et URLs externes exclues. |

Endpoints d'upload qui acceptent `{"image_url"}` en plus du multipart :
- `PUT /menu/products/{id}/image`
- `PUT /menu/products/categories/{id}/image`
- `PUT /menu/marketing-categories/{id}/image`
- `PUT /menu/attribute_options/{id}/image`
- `PUT /integrations/scannorder/banner` (et le logo, non proposé par le front)
- `PUT /franchises/{id}/banner` (et le logo) : la copie depuis « Mes images » accepte les dossiers de la franchise et de l'établissement courant
- `POST /pos/settings/kiosk/settings/idle-image` (et le logo)
- `POST /pos/settings/kiosk/marketing/media` (le JSON est toujours une image)
- `POST /cds/displays/{id}/media` : JSON `{"kind":"image","url":…}` existant ; une URL de notre stockage est désormais placée comme ci-dessus, une URL externe reste acceptée telle quelle.

Ordre commun : la nouvelle image est placée, puis enregistrée, et seulement ensuite l'ancienne est supprimée, si sa clé diffère. Un échec ne laisse jamais une cible pointer vers un fichier effacé. Les endpoints de catégorie et de ScanNOrder supprimaient l'ancienne image *avant* l'envoi : c'est corrigé, et ils ajoutent désormais le paramètre de version `?v=`.

## Alimenter la banque

Deux commandes : `cmd/image_bank_fetch` constitue les images automatiquement, puis `cmd/image_bank_import` les allège et les publie.

### 1. Collecte automatique (`cmd/image_bank_fetch`)

Elle part du catalogue [banque-images/catalogue.csv](banque-images/catalogue.csv) : 476 entrées, dont 410 photos libres et 66 produits de marque, avec une priorité P1, P2 ou P3. Chaque ligne indique où chercher :

| `provider` | Source | Ce qui est retenu |
|---|---|---|
| `openverse` | [Openverse](https://openverse.org), licences **CC0** et **Public Domain Mark** uniquement : plats, ingrédients, boissons génériques, bannières | La recherche (`query`, en anglais) vise d'abord les banques de photos professionnelles (StockSnap, rawpixel, WordPress Photo Directory), puis toutes les photos. Sont écartés les dessins, logos, montages et petites images. Le classement tient compte de la source, des mots trouvés et de l'orientation. |
| `openfoodfacts` | [Open Food Facts](https://world.openfoodfacts.org), photo de face des produits de marque vendus en France (Coca-Cola, Dada, Evian, Heinz…), licence **CC BY-SA 3.0** | Le produit vient du code-barres imposé (`barcode`), sinon de la marque (`brand`). Son nom doit contenir les mots de `query` et aucun mot de `exclude` ; les lots sont écartés. Jusqu'à 10 produits sont comparés et la photo au fond le plus neutre est retenue, pour préférer un packshot détouré à une photo en rayon. Sous `--off-min-background` (60 % de fond neutre par défaut), l'élément n'est pas rempli : à demander au fournisseur. |

Cadrage :
- vignettes : recadrées en carré ; les produits de marque sont centrés sur un carré blanc, pour ne pas couper la canette ;
- bannières : recadrées entre 16:9 et 3:1.

```
go run ./cmd/image_bank_fetch --out ./banque-images [--priority P1] [--only slug1,slug2] [--limit 50]
```

- Sortie : les images dans `--out/{catégorie}/`, et `--out/manifest.csv` au format de l'import, provenance et licence remplies. Les éléments sans image convenable sont listés dans `--out/non-trouves.csv`.
- **Relire les images avant l'import.** Pour en remplacer une, supprimer son fichier et relancer : la commande reprend là où elle s'est arrêtée et cherche une autre photo.
- Openverse limite les appels anonymes à 200 recherches par jour. Pour tout le catalogue en une fois, créer un compte API gratuit et renseigner `OPENVERSE_CLIENT_ID` / `OPENVERSE_CLIENT_SECRET`. Sinon, procéder par priorité (`--priority P1`, puis P2…) sur plusieurs jours.
- Open Food Facts : 10 recherches par minute. La commande respecte ces limites et réessaie en cas d'erreur ou de surcharge (une erreur 503 passagère laisse l'élément dans `non-trouves.csv` : relancer).
- Une image supprimée à la relecture est notée dans `--out/rejetees.txt` et n'est plus proposée.
- Résolution : les aperçus publics de StockSnap et rawpixel font environ 1 000 px, ce qui donne des vignettes de 640 à 1 000 px. C'est suffisant pour la carte, le kiosque et les miniatures. Les photos WordPress et Open Food Facts montent jusqu'à 2 000 px.

Essai réel (24 éléments) : les 17 photos libres ont été trouvées, mais 3 ne correspondaient pas au plat (des « truffes » pour le tiramisu, par exemple). Côté produits de marque, 5 sur 12 ont un packshot détouré exploitable (Coca-Cola, Fanta, Perrier, 1664, Heinz). Pour les autres, Open Food Facts n'a que des photos en rayon ou en main : à demander au fournisseur, ou `--off-min-background 0.2` pour les accepter. **La relecture avant import reste indispensable.**

### 2. Import (`cmd/image_bank_import`)

Simulation, qui n'écrit rien et affiche le gain de compression :
```
go run ./cmd/image_bank_import --manifest ./banque-images/manifest.csv
```

Import, avec `POSTGRES_URL`, `R2_*` et éventuellement `REDIS_URL` :
```
go run ./cmd/image_bank_import --manifest ./banque-images/manifest.csv --apply
```

Colonnes du manifeste, également utilisables pour des images ajoutées à la main (photos fournies par un fournisseur, par exemple) :
- `slug` : identifiant stable, clé d'upsert ;
- `file` : chemin relatif au dossier du manifeste ;
- `category` ;
- `name` : nom affiché ;
- `tags` : séparés par `|` ;
- `format` : `square` ou `banner` ;
- `source`, `license` : obligatoires ;
- `author`, `source_url` ;
- `sort_order`, `enabled` : facultatifs.

Le script est idempotent : un fichier inchangé (même sha256) n'est pas renvoyé, et seules ses métadonnées sont mises à jour. Changer la catégorie ou l'extension d'une image crée une nouvelle clé ; l'ancien fichier reste en place (D4).

Format conseillé des fichiers source :
- vignettes : carrées, au moins 1200×1200 ;
- produits du commerce (canettes, bouteilles) : PNG détouré sur fond transparent ;
- bannières : au moins 2000 px de large, sujet centré (recadrées en 4:1 sur ScanNOrder et en 16:9 sur la borne).

## Licences des images (à valider juridiquement)

| Source | Utilisable pour la banque ? |
|---|---|
| **CC0 / domaine public** : [Openverse](https://openverse.org) filtré `license=cc0,pdm`, [WordPress Photo Directory](https://wordpress.org/photos/), Wikimedia Commons CC0 | Oui, sans attribution. C'est la source de `image_bank_fetch` pour les plats, ingrédients et bannières. |
| **Open Food Facts** (photos de produits de marque) | Avec réserves. Licence CC BY-SA 3.0 : l'attribution « Contributeurs Open Food Facts » est conservée en base, mais elle n'est pas affichée sur les cartes des restaurants. Les marques et logos restent la propriété des fabricants : la photo sert à présenter le produit réellement vendu, rien de plus. À remplacer par les packshots officiels dès qu'un fournisseur les fournit. |
| **Fournisseurs** (packshots de canettes, bouteilles, glaces) | Oui, avec un **accord écrit** du fournisseur ou du distributeur (kit média) pour une diffusion à tous les restaurateurs. Les marques et logos restent la propriété du fabricant. À noter dans `license` (ex. « Accord écrit Fournisseur X 2026-11 »). |
| **Photos commandées ou générées par IA** | Oui si le contrat ou les conditions de l'outil cèdent l'usage commercial. Utile pour les produits très français introuvables en CC0 (tacos français, sauce algérienne… : voir `non-trouves.csv`). |
| **Unsplash** | Déconseillé : la licence exclut la compilation de photos pour reproduire un service similaire ou concurrent ([aide Unsplash](https://help.unsplash.com/en/articles/2612331-why-can-t-i-compile-photos-from-unsplash-to-replicate-a-similar-or-competing-service)). Une banque proposée à tous les clients s'en approche. |
| **Pexels** | Déconseillé : interdit de redistribuer les photos sur une autre plateforme de photos ([règles Pexels](https://help.pexels.com/hc/en-us/articles/360042332714-What-are-the-rules-for-using-Pexels-photos-or-videos)). |
| **Pixabay** | Déconseillé : interdit de distribuer le contenu « tel quel » (*standalone*), même redimensionné ou recadré ([licence Pixabay](https://pixabay.com/service/license/)). |

## Coût estimé

Les estimations reposent sur le catalogue (476 images, dont 32 bannières). Mesuré sur un essai réel de 17 images après allègement : **90 Ko par image en moyenne**, de 32 à 350 Ko. Soit environ 45 Mo au total, et moins de 0,1 Go pour 1 000 images.

| Poste | Tarif Cloudflare (nov. 2026) | Banque de 476 images | Banque de 1 000 images |
|---|---|---|---|
| Stockage R2 | 10 Go-mois gratuits, puis 0,015 $/Go-mois | 0 $ (≈ 0,001 $/mois hors gratuité) | 0 $ (≈ 0,002 $/mois) |
| Écritures (classe A) | 1 M/mois gratuites, puis 4,50 $/M | 476 à l'import : 0 $ | 0 $ |
| Lectures (classe B) | 10 M/mois gratuites, puis 0,36 $/M | quasi nulles (cache CDN) | 0 $ |
| Sortie réseau | gratuite | 0 $ | 0 $ |
| Transformations (miniatures) | 5 000 uniques/mois gratuites, puis 0,50 $ / 1 000 | 476 × 3 largeurs ≈ 1 430 : 0 $ | 3 000 : 0 $ |

Une image de la banque n'est stockée et transformée **qu'une fois**, quel que soit le nombre d'établissements qui l'utilisent. Une photo envoyée par chaque restaurateur, elle, est stockée et transformée pour chacun. La banque coûte donc moins cher que les uploads qu'elle remplace.

L'allègement des uploads (D8) réduit aussi le stockage existant d'environ 5 à 8 fois pour les nouvelles images. Exemples d'avant : WebP 2000×2000 ou PNG jusqu'à 2,4 Mo.

Sources : [tarifs R2](https://developers.cloudflare.com/r2/pricing/), [tarifs Images](https://developers.cloudflare.com/images/pricing/).
