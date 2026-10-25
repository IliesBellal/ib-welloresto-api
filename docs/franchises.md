# Franchises

Regroupement d'établissements sous une **franchise** : une page hub ScanNOrder publique (`/franchise/{slug}`) où le client choisit son établissement, et un menu **Franchise** au back-office pour la créer et la gérer. Document de référence pour les trois dépôts (API, back-office, ScanNOrder). Décisions prises avec Ilies le 2026-10-24.

## Décisions

| # | Décision | Pourquoi |
|---|---|---|
| D1 | Le concept s'appelle **franchise** partout (code, API, URL, libellés). La table `brands` devient `franchises`, `brand_id` devient `franchise_id`. | « brand » désigne déjà le canal d'une commande (`orders.brand`, `brand_status`, `brand_store_id`), la marque d'une fiche client (`customer_brand`), celle d'une carte (`card_brand`) et l'identifiant de marque Deliveroo (`integration_deliveroo.brand_id`). « Franchise » se dit pareil en français et en anglais. |
| D2 | Un établissement appartient **au plus à une** franchise (`merchant.franchise_id`). | Pas de table de liaison, pas d'ambiguïté sur le hub ou le lien retour. |
| D3 | La sélection affichée sur le hub est un drapeau par établissement, `merchant.franchise_hub_visible`, pas une entité « hub » séparée. | Le hub = la franchise ; masquer un établissement sans le détacher. |
| D4 | Droits propres aux franchises (`franchise_members`, rôles `owner` / `manager`), **hors RBAC** par établissement. | Le RBAC donne un rôle par établissement (un token par établissement) ; une franchise est transverse. Les droits suivent la personne : un gestionnaire de deux franchises les gère depuis un seul compte, quel que soit l'établissement sélectionné dans le back-office. |
| D5 | Rattacher un établissement exige d'en être **administrateur** (rôle système `admin`, ou drapeau historique `admin` sans rôle — même règle que `HasAdminRole`). | Empêche d'ajouter un concurrent à son hub, sans flux d'invitation en v1. |
| D6 | Le staff WelloResto (`users.is_platform_staff`) a les droits d'un owner sur toutes les franchises. | Support, et reprise des franchises sans gestionnaire (cas de la ligne `brands` existante). |
| D7 | Hub public indexable, URL `/franchise/{slug}`, toujours affiché même avec un seul établissement. | Page utile pour les recherches sur le nom de la marque. |
| D8 | Distance calculée **dans le navigateur** à partir des coordonnées renvoyées ; l'API ne reçoit jamais la position du client. | Vie privée, et une réponse identique pour tous, donc cachable. Remplace le filtre 50 km côté SQL de l'ancien `GET /scannorder/brands/{slug}`. |
| D9 | Cache Redis de la réponse du hub : **60 s**, purgé à chaque modification au back-office. | Le statut ouvert/fermé et le temps d'attente changent sans purge ; 5-10 min les rendrait faux. Les données statiques tiennent en une requête : un second niveau de cache n'apporterait rien. |
| D10 | Couleur de marque (`brand_color`) en v1. | Demandée pour le hub et les pins de la carte. |
| D11 | Le hub ne montre que les établissements ScanNOrder actifs (activé, abonnement non coupé, QR principal), comme le sitemap. Un établissement masqué ou sans ScanNOrder reste listé au back-office avec un avertissement. | Pas de lien de commande mort sur une page publique. |
| D12 | Bouton « Réserver une table » seulement si la réservation en ligne est ouverte ; un établissement « sur place » seul n'a pas de bouton « Commander » (commander sur place passe par le QR de la table). | Décision Ilies. |

### Hub v1.1 (décisions du 2026-10-24)

| # | Décision | Pourquoi |
|---|---|---|
| D13 | **« Votre établissement habituel »** : le dernier établissement où le client a cliqué « Commander » ou « Réserver » sur ce hub est épinglé en tête, mémorisé sur l'appareil (localStorage, par franchise). | La plupart des clients reviennent au même endroit. Rien côté serveur. |
| D14 | **Précommander** : fermé avec commande programmée possible (`advance_orders_enabled`) → « Fermé · ouvre à … » et bouton « Précommander » ; fermé sans commande programmée → pas de bouton de commande. | Une visite à la fermeture devient une commande, sans proposer une commande impossible. |
| D15 | **Offres sur les cartes** : remises actives maintenant, valables à emporter ou en livraison (`discounts`, même forme que `/discounts`). | Premier levier de clic des marketplaces. |
| D16 | **Statistiques** : onglet **ScanNOrder** de la page Analyse (par établissement, périmètre et période de la page) : CA, commandes, panier moyen et part du ScanNOrder, puis le hub : affichages, clics Commander / Réserver, commandes et CA attribués, conversion. | Prouver la valeur du hub au franchiseur et à chaque établissement. |
| D17 | **Mesure anonyme** : on compte des événements, jamais des personnes : ni IP, ni cookie, ni identifiant persistant (voir D19 à D23). Les affichages du hub sont comptés par franchise (un affichage montre tous ses établissements). | Exemption de consentement de la CNIL pour la mesure d'audience : pas de bandeau. |
| D18 | **Attribution d'une commande au hub** : clic « Commander » sur le hub, puis commande dans les 2 h sur le même établissement (mémorisé dans l'onglet, envoyé en `hub_franchise_slug` à la création). Table `sno_order_attributions`, pas de colonne sur `orders`. | Rien ne change dans l'écriture ni la signature des commandes. |

### Mesure d'audience ScanNOrder (décisions du 2026-10-25)

Objectif d'Ilies : le maximum sans bandeau de consentement, avec une architecture qui permette d'ajouter plus tard ce qui en demande un. Aucun restaurateur n'a demandé de pixel publicitaire à ce jour.

| # | Décision | Pourquoi |
|---|---|---|
| D19 | **Entonnoir anonyme** sur les pages restaurant : `menu_view` (avec sa source), `product_view`, `add_to_cart`, `checkout_start`, `payment_redirect`, `order_created` ; et pour le hub : `hub_view`, `hub_order_click`, `hub_reservation_click`. Table unique `sno_events`. | Savoir où les clients décrochent et d'où ils viennent ; aujourd'hui on ne voyait que les commandes. |
| D20 | **Visite** = identifiant tiré au hasard par onglet (sessionStorage), oublié à la fermeture ou après 30 min d'inactivité. Jamais persistant : pas de visiteurs uniques ni de clients récurrents. | Rester dans l'exemption de la CNIL. Les visiteurs uniques demanderont un consentement. |
| D21 | **Source d'une visite** : `utm_source` ou `src` dans l'URL, sinon `hub` (venu du hub), `qr_table` (QR de table), `search` / `social` / `referral` (selon le site d'origine), `direct` (dont les scans du QR principal, sans site d'origine). L'origine de chaque commande est posée à sa création (`visit_source`, `hub_franchise_slug`) dans `sno_order_attributions` : CA par source. **L'identifiant de visite n'est jamais conservé avec la commande** : une commande est nominative, l'y rattacher relierait la visite anonyme à une personne et sortirait de l'exemption. | Savoir quels supports (QR, Google, Instagram, hub) rapportent. |
| D22 | **Conservation 25 mois** : tâche quotidienne `CleanupOldAudienceEvents`. | Durée maximale admise par la CNIL pour une mesure exemptée. |
| D23 | **Évolutif vers le consentement** : le front classe chaque outil de mesure par catégorie (`audience` exemptée, toujours active ; `analytics` et `marketing` désactivées tant qu'aucun bandeau n'existe). Ajouter Google Analytics ou un pixel = brancher un « récepteur » dans sa catégorie et afficher le bandeau ; rien ne change côté API ni dans les événements. | Le jour où un pixel est demandé, on ajoute le bandeau sans réécrire la mesure. |

**À faire côté légal** : mentionner cette mesure d'audience anonyme (finalité, durée de 25 mois, absence de croisement) dans la politique de confidentialité de ScanNOrder.

## Modèle de données (migration 193)

- `franchises` (ex-`brands`) : `franchise_id` (varchar 64, `fra-<uuid>`), `name` (80), `slug` (unique), `description`, `logo_url`, `banner_url`, `brand_color` (`#RRGGBB`), `creation_date`, `updated_at`. Logo, bannière et description facultatifs.
- `merchant.franchise_id` (ex-`brand_id`, indexé), `merchant.franchise_hub_visible` (défaut `true`).
- `franchise_members` (`franchise_id`, `user_id`, `role` ∈ `owner` | `manager`, `created_at`, `updated_at`), clé primaire (`franchise_id`, `user_id`).

**Déploiement : migration AVANT le code.** Rejouable. La ligne `brands` existante est conservée (renommée) ; elle n'a aucun gestionnaire : le staff WelloResto la voit, ou ajouter un owner à la main.

## Droits

| Action | Qui |
|---|---|
| Voir le menu Franchise | membre d'au moins une franchise, ou administrateur d'au moins un établissement (`can_create`) |
| Créer une franchise | administrateur d'au moins un établissement — devient owner |
| Voir, modifier l'apparence, images, slug | owner, manager |
| Ajouter un établissement | owner, manager **et** administrateur de l'établissement ; l'établissement n'est dans aucune autre franchise |
| Retirer un établissement, le masquer sur le hub | owner, manager |
| Gestionnaires : ajouter (par email d'un compte existant), changer le rôle, retirer | owner (un manager peut se retirer lui-même) ; il reste toujours au moins un owner |
| Supprimer la franchise | owner — les établissements sont détachés, pas supprimés |

Une franchise que l'utilisateur ne gère pas répond `404 franchise_not_found` (son existence n'est pas révélée).

## API back-office — `/franchises` (authentifié)

| Méthode | Route | Corps / réponse |
|---|---|---|
| GET | `/franchises` | `{franchises: [{franchise_id, name, slug, logo_url, role, merchant_count}], can_create}` |
| POST | `/franchises` | `{name, slug?}` → détail (201). Slug dérivé du nom s'il est absent (`-2`, `-3`… s'il est pris). |
| GET | `/franchises/slug-availability?slug=&exclude_id=` | `{slug, valid, available}` |
| GET | `/franchises/{id}` | détail : `{franchise, hub_url, role, merchants: [{merchant_id, business_name, address, zip_code, city, logo_url, hub_visible, scannorder_active, order_types: {take_away, delivery, in}, reservation_enabled}]}` |
| PATCH | `/franchises/{id}` | `{name?, slug?, description?, brand_color?}` (vide = efface) → détail |
| DELETE | `/franchises/{id}` | owner |
| PUT | `/franchises/{id}/logo`, `/banner` | multipart, champ `photo` (JPEG/PNG/WebP, 5 Mo) → `{photo_url}` |
| GET | `/franchises/{id}/candidates` | `{merchants: [{merchant_id, business_name, address, city, franchise_id, franchise_name}]}` — établissements administrés, ceux d'une autre franchise compris (à griser) |
| POST | `/franchises/{id}/merchants` | `{merchant_ids: []}` — tout ou rien → détail |
| PATCH | `/franchises/{id}/merchants/{merchant_id}` | `{hub_visible}` → détail |
| DELETE | `/franchises/{id}/merchants/{merchant_id}` | → détail |
| GET / POST | `/franchises/{id}/members` | `{members: [{user_id, first_name, last_name, email, role, created_at}]}` ; POST `{email, role?}` (manager par défaut) |
| PATCH / DELETE | `/franchises/{id}/members/{user_id}` | `{role}` |

Erreurs (`status` de la réponse) : `franchise_not_found` 404, `franchise_forbidden` 403, `franchise_create_forbidden` 403, `franchise_name_invalid` / `franchise_slug_invalid` / `franchise_brand_color_invalid` / `franchise_description_invalid` / `franchise_role_invalid` 400, `franchise_slug_taken` 409, `franchise_merchant_not_admin` 403, `franchise_merchant_in_other_franchise` 409, `franchise_merchant_not_in_franchise` 404, `franchise_member_not_found` 404, `franchise_member_exists` 409, `franchise_user_not_found` 404, `franchise_last_owner` 409.

## Hub public — `GET /scannorder/franchises/{slug}`

Remplace `GET /scannorder/brands/{slug}` (que rien n'appelait). 404 `franchise_not_found` si le slug est inconnu.

```json
{
  "status": "success",
  "franchise": { "franchise_id": "...", "name": "...", "slug": "...", "description": null,
                 "logo_url": null, "banner_url": null, "brand_color": "#FF5500" },
  "merchants": [{
    "merchant_id": "2", "business_name": "...", "slug": "brasseriedumidi",
    "url": "https://scannorder.welloresto.fr/restaurant/brasseriedumidi",
    "logo_url": "...",
    "address": { "address": "...", "city": "Metz", "zip_code": "57000", "lat": 49.1, "lng": 6.1 },
    "is_open": true, "next_opening": null, "preparation_time": 15,
    "order_types": { "takeaway_enabled": true, "takeaway_available": true, "delivery_enabled": true,
                     "delivery_available": true, "in_enabled": false, "in_available": false },
    "reservation_url": "https://rsv.welloresto.fr/restaurant/brasseriedumidi"
  }]
}
```

- `next_opening` : prochaine ouverture (`YYYY-MM-DD HH:MM:SS`, heure locale), seulement quand fermé.
- Statut ouvert/fermé : même calcul que la page restaurant (`GetMerchantOpenState` : ouverture ScanNOrder, horaires, jours fériés, vacances). Calculé en parallèle (4 à la fois) ; un échec affiche l'établissement fermé sans faire échouer le hub.
- Chaque établissement porte aussi `advance_orders_enabled` (D14) et `discounts` (D15).
- `POST /scannorder/events` `{visit_id?, events: [{type, merchant_id?, franchise_slug?, product_id?, source?}]}` (25 événements au plus par lot, 120 lots/min par IP) → 204 ; type ou forme invalide → 400 ; établissement ou franchise inconnus ignorés. Table `sno_events` (migration 194).
- `POST /scannorder/{slug}/orders` accepte `visit_source` et `hub_franchise_slug` (D18, D21) → `sno_order_attributions`.
- `POST /analytics/scannorder` `{date_from, date_to, merchant_ids?}` (reports.sales.read, périmètre `pos.analytics` comme les autres onglets) → `{scope, totals: {sno_orders, sno_revenue_ttc_cents, all_revenue_ttc_cents}, audience_since, funnel: {visits, product_views, add_to_cart, checkout_start, payment_redirect, orders}, sources: [{source, visits, orders, revenue_ttc_cents}], top_products: [{product_id, name, views, add_to_cart}], hub: {in_franchise, franchises, views, order_clicks, reservation_clicks, orders, revenue_ttc_cents}, daily: [{date, visits, sno_orders, hub_orders, order_clicks}], by_merchant: [...]}`. Commandes au sens des onglets CA / Commandes (`AnalyticsOrdersScope`).
- `GET /scannorder/sitemap` gagne `franchises: [{slug, updated_at}]` : les hubs qui affichent au moins un établissement.
- La page restaurant (`GET /scannorder/{slug}`) gagne `merchant.franchise: {name, slug}` (nil si aucune franchise ou masqué sur le hub), pour le lien « autres établissements ».

## Plus tard (v2)

- Invitations : un franchiseur invite un établissement qu'il n'administre pas, l'admin de l'établissement accepte (remplace D5 pour ce cas).
- Création d'un établissement directement dans la franchise (inscription avec `franchise_id`).
- **Livraison sur le hub** (validé pour la v2, 2026-10-24) : « Livre à votre adresse », avec frais de livraison et minimum de commande, à partir de l'adresse saisie sur le hub ; en même temps que les zones de chalandise définies dans la franchise, pour que les établissements ne se marchent pas dessus (aujourd'hui : distance seule).
- Rôles plus fins (valeurs de `role`, sans changement de structure), statistiques consolidées de la franchise.
- **Menu commun** à tous les établissements de la franchise (demande d'Ilies, 2026-10-24). Aujourd'hui chaque établissement a ses propres lignes de menu (produits, catégories, attributs) et seul existe un import ponctuel depuis un autre établissement (`POST /menu/import/preview-from-merchant`). Un menu commun est une synchronisation continue : piste probable, un menu source (établissement de référence ou catalogue de la franchise) poussé vers chaque établissement, avec une table de correspondance source → copie et des surcharges locales (prix, disponibilité, ruptures), puis purge des caches menu de chaque établissement (`InvalidateMerchantMenuCaches`). Rien à changer dans le modèle actuel : D2 (une seule franchise par établissement) évite deux sources concurrentes, et l'édition du menu commun sera un droit de plus dans `franchise_members.role`. À cadrer avant de coder (sous-produits, menus composés, TVA et prix par mode de commande, établissements qui quittent la franchise).
