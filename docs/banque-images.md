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
| D7 | La banque est alimentée uniquement par le script `cmd/image_bank_import`. Il n'y a pas de rôle « admin Wello » dans l'API pour un écran d'administration. |
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

1. Déposer les fichiers (jpg, png, webp) dans un dossier, par exemple `images/boissons-soft/canette-cola-33-cl.png`.
2. Compléter le manifeste CSV. Le point de départ est [banque-images/liste-images.csv](banque-images/liste-images.csv), au bon format. Colonnes :
   - `slug` : identifiant stable, clé d'upsert ;
   - `file` : chemin relatif à `--dir` ;
   - `category` ;
   - `name` : nom affiché ;
   - `tags` : séparés par `|` ;
   - `format` : `square` ou `banner` ;
   - `source`, `license` : obligatoires ;
   - `author`, `source_url` ;
   - `sort_order`, `enabled` : facultatifs.

   Les colonnes supplémentaires (`priority`, `sourcing`, `search_en`) sont ignorées.
3. Simulation, qui n'écrit rien et affiche le gain de compression :
   ```
   go run ./cmd/image_bank_import --manifest images/manifest.csv
   ```
4. Import, avec `POSTGRES_URL`, `R2_*` et éventuellement `REDIS_URL` :
   ```
   go run ./cmd/image_bank_import --manifest images/manifest.csv --apply
   ```

Le script est idempotent : un fichier inchangé (même sha256) n'est pas renvoyé, et seules ses métadonnées sont mises à jour. Changer la catégorie ou l'extension d'une image crée une nouvelle clé ; l'ancien fichier reste en place (D4).

Format conseillé des fichiers source :
- vignettes : carrées, au moins 1200×1200 ;
- produits du commerce (canettes, bouteilles) : PNG détouré sur fond transparent ;
- bannières : au moins 2000 px de large, sujet centré (recadrées en 4:1 sur ScanNOrder et en 16:9 sur la borne).

## Licences des images (à valider juridiquement)

| Source | Utilisable pour la banque ? |
|---|---|
| **CC0 / domaine public** : [Openverse](https://openverse.org) filtré `license=cc0,pdm`, [WordPress Photo Directory](https://wordpress.org/photos/), Wikimedia Commons CC0 | Oui. C'est la source recommandée pour les plats génériques (colonne `sourcing = libre-cc0`). |
| **Fournisseurs** (packshots de canettes, bouteilles, glaces) | Oui, avec un **accord écrit** du fournisseur ou du distributeur (kit média) pour une diffusion à tous les restaurateurs. Les marques et logos restent la propriété du fabricant. À noter dans `license` (ex. « Accord écrit Fournisseur X 2026-11 »). |
| **Photos commandées ou générées par IA** | Oui si le contrat ou les conditions de l'outil cèdent l'usage commercial. Utile pour les produits très français (tacos français, sauce algérienne…) introuvables en CC0 (`sourcing = ia-ou-photographe`). |
| **Unsplash** | Déconseillé : la licence exclut la compilation de photos pour reproduire un service similaire ou concurrent ([aide Unsplash](https://help.unsplash.com/en/articles/2612331-why-can-t-i-compile-photos-from-unsplash-to-replicate-a-similar-or-competing-service)). Une banque proposée à tous les clients s'en approche. |
| **Pexels** | Déconseillé : interdit de redistribuer les photos sur une autre plateforme de photos ([règles Pexels](https://help.pexels.com/hc/en-us/articles/360042332714-What-are-the-rules-for-using-Pexels-photos-or-videos)). |
| **Pixabay** | Déconseillé : interdit de distribuer le contenu « tel quel » (*standalone*), même redimensionné ou recadré ([licence Pixabay](https://pixabay.com/service/license/)). |

## Coût estimé

Les estimations reposent sur la liste fournie (375 images : 343 vignettes, 32 bannières) après allègement :
- environ 350 Ko par JPEG ;
- environ 900 Ko par PNG détouré (34 images de fournisseurs) ;
- **≈ 150 Mo** au total.

Pour 1 000 images, compter environ 0,4 Go.

| Poste | Tarif Cloudflare (nov. 2026) | Banque de 375 images | Banque de 1 000 images |
|---|---|---|---|
| Stockage R2 | 10 Go-mois gratuits, puis 0,015 $/Go-mois | 0 $ (≈ 0,002 $/mois hors gratuité) | 0 $ (≈ 0,006 $/mois) |
| Écritures (classe A) | 1 M/mois gratuites, puis 4,50 $/M | 375 à l'import : 0 $ | 0 $ |
| Lectures (classe B) | 10 M/mois gratuites, puis 0,36 $/M | quasi nulles (cache CDN) | 0 $ |
| Sortie réseau | gratuite | 0 $ | 0 $ |
| Transformations (miniatures) | 5 000 uniques/mois gratuites, puis 0,50 $ / 1 000 | 375 × 3 largeurs ≈ 1 125 : 0 $ | 3 000 : 0 $ |

Une image de la banque n'est stockée et transformée **qu'une fois**, quel que soit le nombre d'établissements qui l'utilisent. Une photo envoyée par chaque restaurateur, elle, est stockée et transformée pour chacun. La banque coûte donc moins cher que les uploads qu'elle remplace.

L'allègement des uploads (D8) réduit aussi le stockage existant d'environ 5 à 8 fois pour les nouvelles images. Exemples d'avant : WebP 2000×2000 ou PNG jusqu'à 2,4 Mo.

Sources : [tarifs R2](https://developers.cloudflare.com/r2/pricing/), [tarifs Images](https://developers.cloudflare.com/images/pricing/).
