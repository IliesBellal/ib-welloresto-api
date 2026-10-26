# Mesure d'audience

Mesure anonyme du parcours des clients sur **ScanNOrder** (pages restaurant et hubs de franchise) et sur les **bornes de commande**, affichée au back-office dans la page Analyse (onglets ScanNOrder et Borne) et dans Franchise → Statistiques. Document de référence pour l'API, le back-office, ScanNOrder et l'app borne. Les décisions propres aux franchises (hub, attribution d'une commande au hub, statistiques de la franchise) restent dans [franchises.md](franchises.md).

Objectif d'Ilies : le maximum sans bandeau de consentement, avec une architecture qui permette d'ajouter plus tard ce qui en demande un.

## Principes communs

| # | Décision | Pourquoi |
|---|---|---|
| D17 | **Mesure anonyme** : on compte des événements, jamais des personnes : ni IP, ni cookie, ni identifiant persistant. | Exemption de consentement de la CNIL pour la mesure d'audience : pas de bandeau. |
| D22 | **Conservation 25 mois** : tâche quotidienne `CleanupOldAudienceEvents` (`sno_events`, `sno_order_attributions`, `kiosk_events`). | Durée maximale admise par la CNIL pour une mesure exemptée. |

## ScanNOrder (décisions du 2026-10-25)

| # | Décision | Pourquoi |
|---|---|---|
| D19 | **Entonnoir anonyme** sur les pages restaurant : `menu_view` (avec sa source), `product_view`, `add_to_cart`, `checkout_start`, `payment_redirect`, `order_created` ; et pour le hub : `hub_view`, `hub_order_click`, `hub_reservation_click`. Table unique `sno_events`. | Savoir où les clients décrochent et d'où ils viennent ; avant, on ne voyait que les commandes. |
| D20 | **Visite** = identifiant tiré au hasard par onglet (sessionStorage), oublié à la fermeture ou après 30 min d'inactivité. Jamais persistant : pas de visiteurs uniques ni de clients récurrents. | Rester dans l'exemption de la CNIL. Les visiteurs uniques demanderont un consentement. |
| D21 | **Source d'une visite** : `utm_source` ou `src` dans l'URL, sinon `hub` (venu du hub), `qr_table` (QR de table), `search` / `social` / `referral` (selon le site d'origine), `direct` (dont les scans du QR principal, sans site d'origine). L'origine de chaque commande est posée à sa création (`visit_source`, `hub_franchise_slug`) dans `sno_order_attributions` : CA par source. **L'identifiant de visite n'est jamais conservé avec la commande** : une commande est nominative, l'y rattacher relierait la visite anonyme à une personne et sortirait de l'exemption. | Savoir quels supports (QR, Google, Instagram, hub) rapportent. |
| D23 | **Évolutif vers le consentement** : le front classe chaque outil de mesure par catégorie (`audience` exemptée, toujours active ; `analytics` et `marketing` désactivées tant qu'aucun bandeau n'existe). Ajouter Google Analytics ou un pixel = brancher un « récepteur » dans sa catégorie et afficher le bandeau ; rien ne change côté API ni dans les événements. | Le jour où un pixel est demandé, on ajoute le bandeau sans réécrire la mesure. |

L'onglet ScanNOrder est une vue d'établissement : le hub y est une source parmi d'autres, avec un lien vers les statistiques de la franchise (franchises.md, D26).

## Bornes de commande (décisions du 2026-10-26)

| # | Décision | Pourquoi |
|---|---|---|
| D27 | Onglet **Borne** dans la page Analyse, séparé de l'onglet ScanNOrder, même mise en page : CA, commandes, panier moyen, part du CA, entonnoir, produits consultés, par jour, par établissement, et **par borne**. Même permission et même périmètre que les autres onglets. | Décision Ilies : deux canaux distincts, deux onglets. |
| D28 | **Session** = du toucher de l'écran de veille au retour en veille (commande terminée, abandon, inactivité). Identifiant tiré au hasard par la borne, gardé en mémoire, **jamais relié à une commande**. La borne appartient au restaurant : aucun traceur n'est déposé sur l'appareil d'un client, et rien n'identifie une personne. | Pas de consentement à recueillir ; rester anonyme quand même. |
| D29 | **Événements** : `session_start`, `fulfillment_chosen` (`IN` / `TAKE_AWAY`), `product_view`, `add_to_cart`, `summary_view` (panier affiché), `payment_start` (`card` / `counter`), `payment_failed`, `order_created`, `session_abandoned`. Chacun porte le temps écoulé depuis le début de la session (`elapsed_ms`, mesuré par la borne). Table `kiosk_events` (migration 195). | Entonnoir complet de la borne, et des durées justes même si l'horloge de la borne est fausse. |
| D30 | **Aucun impact sur le client** : l'enregistrement d'un événement ne fait que le poser dans une liste en mémoire (pas de réseau, pas de disque) ; l'envoi part en tâche de fond par lots, avec un délai court, une seule nouvelle tentative, puis abandon ; liste plafonnée. Toute erreur de la mesure est ignorée. **Interrupteur** `KIOSK_AUDIENCE_ENABLED` (API, défaut activé) renvoyé à la borne dans `GET /kiosk/settings` (`audience_enabled`) : absent ou faux, la borne ne mesure rien. Côté API, un événement invalide est ignoré seul, sans refuser le lot. | Décision Ilies : « on préfère ne pas avoir de stats plutôt que de bloquer un client ». L'interrupteur coupe la mesure sans mise à jour des bornes. |
| D31 | **Mesures propres à la borne**, dès la v1 : **abandons** (`session_abandoned`, raison `inactivity` / `cancel`, ou `other` pour toute autre fin de session sans commande, étape `fulfillment` / `menu` / `cart` / `upsell` / `pager` / `payment`, montant du panier), **échecs de paiement carte** (`payment_failed`, raison `declined` / `canceled` / `terminal_unavailable` / `error`), **durée d'une commande** (médiane et 90e centile de `elapsed_ms` à `order_created`). | Le manque à gagner, les terminaux qui posent problème, la fluidité du parcours. |
| D32 | **Répartitions** tirées des commandes, sans mesure : sur place / à emporter (`orders.order_type`), carte à la borne / au comptoir (carte = PaymentIntent Stripe Terminal capturé dans `stripe_payments`), par borne (`orders.kiosk_id`, nom dans `kiosks` ; une borne supprimée n'a plus de nom). | Chiffres exacts, disponibles pour tout l'historique. |
| D33 | Le **ruban marketing** n'est pas mesuré. | Il affiche des images et des vidéos en boucle, sans interaction possible (Ilies). |

## Données

- `sno_events` (migration 194) : `event_type`, `merchant_id`, `franchise_id`, `visit_id`, `source`, `product_id`, `created_at`.
- `sno_order_attributions` (migration 194) : `order_id`, `merchant_id`, `source`, `franchise_id` (`visit_id` jamais renseigné, D21).
- `kiosk_events` (migration 195) : `event_type`, `merchant_id`, `kiosk_id`, `session_id`, `elapsed_ms`, `product_id`, `amount_cents`, `detail`, `step`, `created_at`.

## API

- `POST /scannorder/events` `{visit_id?, events: [{type, merchant_id?, franchise_slug?, product_id?, source?}]}` (25 événements au plus par lot, 120 lots/min par IP) → 204 ; type ou forme invalide → 400 ; établissement ou franchise inconnus ignorés.
- `POST /scannorder/{slug}/orders` accepte `visit_source` et `hub_franchise_slug` → `sno_order_attributions`.
- `POST /kiosk/events` (jeton de la borne) `{events: [{type, session_id, elapsed_ms?, product_id?, amount_cents?, detail?, step?}]}` (100 événements au plus par lot) → 204, y compris mesure coupée ; borne et établissement pris du jeton, jamais du corps ; événement mal formé ignoré seul ; corps illisible ou lot trop gros → 400.
- `POST /analytics/scannorder` `{date_from, date_to, merchant_ids?}` (reports.sales.read, périmètre `pos.analytics`) → `{scope, totals: {sno_orders, sno_revenue_ttc_cents, all_revenue_ttc_cents}, audience_since, funnel: {visits, product_views, add_to_cart, checkout_start, payment_redirect, orders}, sources: [{source, visits, orders, revenue_ttc_cents}], top_products: [{product_id, name, views, add_to_cart}], hub: {in_franchise, franchises}, daily: [{date, visits, sno_orders}], by_merchant: [{merchant_id, visits, sno_orders, sno_revenue_ttc_cents}]}`.
- `POST /analytics/kiosk` `{date_from, date_to, merchant_ids?}` (mêmes droits) → `{scope, totals: {kiosk_orders, kiosk_revenue_ttc_cents, all_revenue_ttc_cents}, audience_since, funnel: {sessions, fulfillment_chosen, product_views, add_to_cart, summary_views, payment_starts, orders}, abandonments: {sessions, amount_cents, by_step: [{step, sessions, amount_cents}], by_reason: [{key, count}]}, payment_failures: {card_starts, failures, by_reason: [{key, count}]}, duration: {orders, median_seconds, p90_seconds}, by_order_type: [{key, orders, revenue_ttc_cents}], by_payment: [{key, orders, revenue_ttc_cents}], top_products: [...], daily: [{date, sessions, kiosk_orders, abandonments}], by_merchant: [{merchant_id, sessions, kiosk_orders, kiosk_revenue_ttc_cents}], by_kiosk: [{kiosk_id, name, merchant_id, sessions, orders, revenue_ttc_cents, abandonments, payment_failures, median_seconds}]}`.

Les commandes et le CA suivent `AnalyticsOrdersScope`, comme les onglets CA et Commandes. Les chiffres d'audience ne commencent qu'à `audience_since` : avant, rien n'était mesuré (une borne non mise à jour n'envoie rien).

## Politique de confidentialité

À mentionner (finalité, absence de cookie et de croisement, durée de 25 mois) : la mesure ScanNOrder et celle des bornes. Paragraphe dans la politique du site vitrine (`wello-resto-vitrine`, `src/pages/confidentialite.astro`, §8).
