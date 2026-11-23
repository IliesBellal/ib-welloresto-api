# Référencement des pages ScanNOrder

Objectif : que la page ScanNOrder de chaque établissement (`https://scannorder.welloresto.fr/restaurant/{slug}`) ressorte dans Google, Bing et Yahoo sur les recherches qui correspondent à ce qu'il vend : son type de cuisine, ses produits, sa ville.

Trois dépôts sont concernés :

| Dépôt | Rôle |
|---|---|
| `ib-welloresto-api` | stocke les réglages, les expose à la page publique, suggère une description par IA, signale les pages modifiées à IndexNow |
| `wello-back-office` | onglet **ScanNOrder › Référencement** |
| `wello-resto-scannorder` | `server/seo.mjs` : titre, meta, JSON-LD, HTML rendu côté serveur, fichier de clé IndexNow |

## Données

Colonnes de `scannorder_settings` :

| Colonne | Contenu |
|---|---|
| `seo_title` | titre saisi (70 caractères au plus). Vide = titre composé par le front : `Da Mario — Pizzeria à Metz \| Livraison et à emporter` |
| `seo_description` | description (500 caractères au plus), affichée sur la page ; les 160 premiers forment la meta description. Vide = présentation générée par le front à partir de la carte |
| `seo_cuisine_type` | codes de cuisine séparés par des virgules, type principal en premier, 3 au plus (`pizza,italien`). Liste fermée : `internal/helpers/scannorder_cuisines.go`. Un texte libre antérieur à la liste reste publié tel quel |
| `seo_links` (migration 222) | tableau JSON d'URL : fiche Google, Instagram, Facebook… 8 au plus, normalisées (`https://` ajouté, domaine en minuscules) |
| `seo_updated_at` (migration 222) | dernière modification depuis le back-office |
| `seo_keywords` | inutilisée : Google ignore la balise meta keywords |

Un code de cuisine a deux libellés : `label`, la cuisine (« Pizza », « Libanaise ») publiée en `servesCuisine`, et `place`, le type d'établissement (« Pizzeria », « Restaurant libanais ») repris dans le titre et la présentation. Ne jamais renommer un code (il est stocké) ; un libellé peut changer.

## Page publique — `GET /scannorder/{slug}`

En plus des champs existants (`description`, `cuisine_type`, `seo`) :

- `cuisines` : `[{code, label, place}]`, dans l'ordre ;
- `cuisine_type` : les libellés joints par « , » (gardé pour les fronts déjà déployés) ;
- `same_as` : `seo_links`, puis `merchant.web_site` s'il est valide, sans doublon ;
- `reservation_url` : page de réservation, même règle que le hub de franchise (réservations actives et incluses dans l'abonnement).

Le front en tire le JSON-LD `Restaurant` (`servesCuisine`, `priceRange`, `acceptsReservations`, `sameAs`, `hasMap`) et la section « Infos pratiques » de la page. Les liens `sameAs`, surtout celui de la fiche Google, permettent à Google de relier la page à l'établissement qu'il connaît déjà.

## Back-office — `/integrations/scannorder/seo`

| Route | Droits | Rôle |
|---|---|---|
| `GET` | authentifié | réglages, liste fermée des cuisines, bornes de saisie, `suggest_available` |
| `PATCH` | `platforms.manage` | mise à jour partielle (champ absent = inchangé, vide = effacé). Saisie invalide → 400 `{error: "invalid_field", field, reason, value}` ; rien n'est tronqué en silence |
| `POST /suggest` | `platforms.manage`, 10 par heure et par établissement | propose `description` et `cuisine_types` à partir de la carte ; **rien n'est enregistré**, le restaurateur relit puis enregistre |

La page publique est mise à jour en quelques minutes (cache merchant de 2 min côté API, cache de page de 5 min côté front).

## Suggestion par IA — tâche `seo_profile`

- Entrée : nom, ville et carte visible sur ScanNOrder (même règle que la carte publique, 150 produits au plus, descriptions abrégées). Aucun identifiant Wello n'est envoyé.
- Sortie contrainte par schéma JSON : la description et les codes de cuisine (énumérés). Bornes vérifiées en Go (`sanitizeSuggestion`).
- Consigne (`prompt.go`) : rester factuel, ne rien affirmer que la carte ne montre pas (« fait maison », « depuis 1990 »…), première phrase autonome de 150 caractères au plus.
- Pas de crédit consommé : un appel texte court. La limite de 10 par heure et par établissement borne le coût.
- Erreurs : 503 `seo_suggest_disabled` (tâche fermée), 422 `menu_too_small` (moins de 3 produits visibles), 502 `seo_suggest_failed`.

Variables : `AI_TASK_SEO_PROFILE_PROVIDER/MODEL/EFFORT/MAX_TOKENS/TIMEOUT_MS/ENABLED` (défauts : anthropic / `claude-opus-5-5` / `low` / 8000 / 60000 / **false**). Tant que la tâche est fermée, le bouton « Suggérer » est masqué.

## IndexNow (Bing, Yahoo…)

Google lit le sitemap. Bing, et donc Yahoo, DuckDuckGo et Ecosia qui reprennent son index, ainsi que Yandex, Seznam et Naver acceptent en plus le protocole IndexNow : on leur signale les pages modifiées, qu'ils revisitent en minutes.

- Tâche `RunIndexNow`, toutes les 15 minutes : pages du sitemap (établissements et hubs) dont le `lastmod` a bougé depuis le passage précédent → `POST https://api.indexnow.org/indexnow`. La date du dernier passage est gardée dans Redis (`scannorder:indexnow:watermark`) ; au premier passage, seules les pages modifiées la veille partent. Un échec n'avance pas la date : renvoi au passage suivant.
- `lastmod` du sitemap = la plus récente de `merchant_parameters.last_menu_update` et `scannorder_settings.seo_updated_at`.
- Inactif sans `INDEXNOW_KEY` (API) : ne la définir qu'en production. La même clé dans `INDEXNOW_KEY` du front ScanNOrder, qui sert le fichier `https://scannorder.welloresto.fr/{clé}.txt`. Format : 8 à 128 caractères `a-z A-Z 0-9 -`. `SCANNORDER_BASE_URL` doit être en `https`.

## Mise en production

1. Migration `migrations/todo/222_scannorder_seo_links.up.sql` **avant** l'API : `GetMerchantSEOInfo` et le sitemap lisent les nouvelles colonnes. Sans elle, la page publique perd ses données de référencement (l'échec est journalisé, la commande n'est pas bloquée) et le sitemap échoue.
2. API, puis front ScanNOrder, puis back-office. Le front tolère une API antérieure : sans `cuisines` ni `same_as`, il retombe sur l'ancien comportement.
3. Générer une clé (`openssl rand -hex 16`), la poser dans `INDEXNOW_KEY` de l'API **et** du front de production.
4. Ouvrir la suggestion : `AI_TASK_SEO_PROFILE_ENABLED=true` (d'abord sur staging).
5. Déclarer `https://scannorder.welloresto.fr/sitemap.xml` dans Google Search Console (propriété déjà vérifiée) et dans Bing Webmaster Tools (import possible depuis la Search Console).

## Pas encore fait

- `areaServed` (communes livrées) : la zone de livraison est une grille de cellules, il faudrait la convertir en communes (géocodage inverse).
- `suitableForDiet` sur les plats : les produits n'ont pas d'indication de régime (végétarien, halal, sans gluten) exploitable ; les allergènes ne suffisent pas.
- Pages par plat et pages ville × cuisine : voir le lot 2.
