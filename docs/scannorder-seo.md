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

## Fiche Google et demandes d'avis

Migration 223. Réglages dans le back-office, carte « Votre fiche Google » et « Demande d'avis par e-mail » de l'onglet Référencement.

### Fiche Google

| Colonne de `scannorder_settings` | Contenu |
|---|---|
| `google_place_id` | Place ID, renseigné quand le restaurateur choisit sa fiche dans la recherche |
| `google_maps_url` | lien de la fiche, publié **en premier** dans `same_as` (donc en `sameAs` et en `hasMap` sur la page) |
| `google_review_url` | lien « laisser un avis » envoyé aux clients |
| `review_request_enabled` | demande d'avis par e-mail activée (faux par défaut) |

- **Recherche** : `POST /integrations/scannorder/seo/google-search` (20 par heure et par établissement) interroge l'API Places de Google (« Find Place from Text », clé `GOOGLE_API_KEY`, la même API que `googlemaps.GetAddressFromPlaceID`) avec le nom et l'adresse de l'établissement, puis le nom et la ville si rien n'est trouvé. Chaque fiche proposée porte ses liens : `https://www.google.com/maps/place/?q=place_id:{id}` et `https://search.google.com/local/writereview?placeid={id}` (format documenté par Google). Si Google refuse (API non activée sur la clé, quota), 503 `google_search_unavailable` : le back-office le dit et le restaurateur colle les liens lui-même.
- **Saisie** : les deux liens doivent être sur un domaine Google (`google.<tld>` et sous-domaines, `g.page`, `goo.gl`, `maps.app.goo.gl`), passés en https. Le lien d'avis part dans des e-mails envoyés au nom de l'établissement : il ne peut pas mener ailleurs. Le lien d'avis se trouve aussi dans la fiche Google : « Demander des avis ».
- **Activation** : `review_request_enabled = true` sans lien d'avis est refusé (`missing_review_url`) ; effacer le lien d'une demande active la coupe.

### Demande d'avis par e-mail

Module `internal/modules/reviewrequests`, tâche `RunReviewRequests` toutes les 15 minutes (une instance à la fois, `fiscal.TryLock`).

| Règle | Valeur |
|---|---|
| Commandes | marque `WELLO_RESTO`, **tous canaux** (ScanNOrder, caisse, borne ; Uber Eats et Deliveroo exclus par la marque), `state = 'CLOSED'`, client avec e-mail. Élargi le 2026-11-25 : avant, seulement ScanNOrder (`order_source = 'SCANNORDER'` ou ancien marqueur `created_by` `SCANNORDER` / `-1`) |
| Fiches écartées | e-mail identique à celui d'un utilisateur de l'établissement (`users` via `users_rights`) ; fiche client générique : plus de 5 commandes du même client dans une fenêtre de 24 h (`MaxOrdersPerCustomerPerDay`) |
| Délai | 1 h 30 après la clôture (`delivered_on`), au plus 48 h : pas d'envoi de l'historique au déploiement |
| Heures | de 9 h à 21 h, heure de l'établissement ; en dehors, la commande attend le passage suivant |
| Plafond | une demande par client et par établissement tous les 90 jours |
| Établissement | demande activée, lien d'avis renseigné, abonnement ni suspendu ni résilié, module ScanNOrder actif |
| Désinscription | lien dans chaque e-mail, propre à l'établissement |

- **Suivi** (`review_requests`) : une ligne par commande considérée, écrite **avant** l'envoi (`pending`), puis `sent` (avec le messageId Brevo) ou `failed`. Aucune commande n'est sollicitée deux fois ; une demande en échec n'est pas retentée. Les commandes écartées sont aussi inscrites : `skipped_opt_out`, `skipped_recent`, `skipped_invalid`.
- **Données personnelles** : aucune adresse e-mail n'est conservée par ce module, seulement son empreinte (`email_hash`, sha256 de l'adresse en minuscules) pour le plafond et la désinscription.
- **E-mail** : `review_request.html` (cadre client aux couleurs de l'établissement), objet « Votre avis sur {établissement} ? », expéditeur au nom de l'établissement (`BrevoMailer.SendReviewRequest`, synchrone). Un seul bouton : « Laisser un avis sur Google ».
- **Décision 2026-11-25 (élargissement à tous les canaux)** : avis « simple » — aucune récompense (Google interdit les avis incités par une réduction ou un cadeau, même pour tout avis, et supprime ces avis ; en cas de récidive il restreint la fiche), aucune modification du POS. Conséquences à connaître : (1) le réglage `review_request_enabled` n'est pas par canal, un établissement qui l'avait activé pour ScanNOrder reçoit désormais aussi les clients de la caisse ; (2) le client de caisse n'est pas informé de la demande au moment où l'e-mail est saisi (condition CNIL du « soft opt-in » si la demande est requalifiée en prospection) — le lien de désinscription est le seul garde-fou, d'où l'intérêt de mentionner la demande d'avis dans la politique de confidentialité de l'établissement ; (3) le client rattaché à une commande de caisse n'est pas forcément celui qui a mangé (homonyme, carte fidélité d'une table). Piste si besoin : réglage par canal (migration) et mention sous le champ e-mail du POS.
- **Règles Google sur les avis** : aucune contrepartie, aucun tri des clients selon leur satisfaction (tous les clients éligibles reçoivent la même demande). Ne pas ajouter de question « Êtes-vous satisfait ? » avant le lien.
- **Désinscription** : page ScanNOrder `/desinscription/avis/{jeton}` (hors slug, comme prévu au P10 de `docs/campagnes-marketing.md`). Le jeton est aléatoire (32 octets), stocké dans `review_requests.token` : aucune donnée personnelle dans l'URL, pas d'expiration. API publique `GET /scannorder/review-requests/{jeton}`, `POST …/opt-out` et `POST …/opt-in` (annulation d'un clic par erreur), 30 par minute et par visiteur. La désinscription vaut pour cet établissement seulement (`review_request_optouts`).
- **Test** : `POST /integrations/scannorder/seo/review-request/test` (5 par heure) envoie la demande à l'utilisateur connecté, marquée « test », avec le lien d'avis enregistré. Rien n'est inscrit.
- **Statistique** : le back-office affiche le nombre de demandes envoyées sur 30 jours (`review_requests_sent_30d`).
- **Pas encore fait** : en-têtes `List-Unsubscribe` / `List-Unsubscribe-Post` (désinscription en un clic des messageries). Non testé avec Brevo, et non exigé à ce volume (Gmail et Yahoo l'imposent au-delà de 5 000 envois par jour). À ajouter avec le cadre des campagnes (`docs/campagnes-marketing.md` §10).
- **À valider juridiquement** (Q7 de `docs/campagnes-marketing.md`) : la demande d'avis est envoyée au titre de l'intérêt légitime, comme suite de la commande, sans contenu promotionnel et avec un droit d'opposition dans chaque e-mail. Le mentionner dans la politique de confidentialité des établissements.

## Mise en production

1. Migrations `migrations/todo/222_scannorder_seo_links.up.sql` et `223_google_review_requests.up.sql` **avant** l'API : `GetMerchantSEOInfo`, le sitemap et les demandes d'avis lisent les nouvelles colonnes et tables. Sans elles, la page publique perd ses données de référencement (l'échec est journalisé, la commande n'est pas bloquée) et le sitemap échoue.
2. API, puis front ScanNOrder, puis back-office. Le front tolère une API antérieure : sans `cuisines` ni `same_as`, il retombe sur l'ancien comportement.
3. Générer une clé (`openssl rand -hex 16`), la poser dans `INDEXNOW_KEY` de l'API **et** du front de production.
4. Ouvrir la suggestion : `AI_TASK_SEO_PROFILE_ENABLED=true` (d'abord sur staging).
5. Déclarer `https://scannorder.welloresto.fr/sitemap.xml` dans Google Search Console (propriété déjà vérifiée) et dans Bing Webmaster Tools (import possible depuis la Search Console).
6. Vérifier que la clé `GOOGLE_API_KEY` a l'API Places activée : sinon la recherche de fiche répond `google_search_unavailable` et seule la saisie reste possible.
7. Les demandes d'avis partent du domaine de l'API vers la page de désinscription de `SCANNORDER_BASE_URL` : vérifier cette variable sur chaque environnement.

## Pas encore fait

- `areaServed` (communes livrées) : la zone de livraison est une grille de cellules, il faudrait la convertir en communes (géocodage inverse).
- `suitableForDiet` sur les plats : les produits n'ont pas d'indication de régime (végétarien, halal, sans gluten) exploitable ; les allergènes ne suffisent pas.
- Pages par plat et pages ville × cuisine : voir le lot 2.
