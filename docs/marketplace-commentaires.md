# Commentaires des commandes Uber Eats / Deliveroo

> Audit du 2026-10-29 : les consignes écrites par le client sont-elles correctement
> créées, puis visibles sur la caisse (POS) — commandes en cours et production, dans
> les deux modes ? Complète `marketplace-promotions-tva.md`.

## 1. Où vivent les commentaires

Une seule table, `order_comments` (`content` text, `user_id` varchar(20)) :

- commentaire de **commande** : `order_item_id` vide ;
- commentaire d'**article** : `order_item_id` renseigné.

Écriture : `insertOrderComment` (échec = erreur de création) et
`insertOrderItemComment` (non bloquant). L'auteur est `WEBHOOK_UBER_EATS` ou
`WEBHOOK_DELIVEROO`, qui tiennent dans `user_id` et sont laissés intacts par le
chantier des identifiants entiers (`userids.go` ne touche que les valeurs présentes
dans `user_id_remap`).

Lecture : `orders_fetcher_builder.go` renvoie `comment` (texte, dernier commentaire
de commande non vide) sur la commande et `comment` (objet `content`) sur chaque
article. Couvert par les tests d'intégration existants (`orders` et
`order_life_cycle`).

## 2. Ce que les plateformes envoient

| plateforme | commande | article | autre |
|---|---|---|---|
| Uber Eats | `cart.special_instructions` (si l'établissement l'a activé) | `special_instructions` ; `special_requests[].allergy` (allergènes + texte, à faire activer par Uber) | `eater.delivery.notes` (livraison par l'établissement) ; `packaging.disposable_items.should_include` |
| Deliveroo | `order_notes`, `cutlery_notes` | **aucune note par article** | `delivery.delivery_notes` ; les consignes de préparation d'un article arrivent comme **options** (`modifiers[]`) |

Sources : documentation « Get Order Details » d'Uber Eats et « order.new » de
Deliveroo (champs vérifiés le 2026-10-29).

## 3. Ce qui ne marchait pas, et la correction

| # | défaut | correction |
|---|---|---|
| 1 | Uber, commande : on lisait `store_instructions`, que la documentation actuelle ne cite pas, et on ignorait `cart.special_instructions` | `orderComment` (`ubereats/service/comments.go`) lit les deux, plus les consignes de livraison et les couverts demandés |
| 2 | Uber, article : on lisait `customer_request`, absent de la documentation, et on ignorait `special_instructions` et `special_requests[].allergy` ; **notes d'article et allergies n'arrivaient probablement jamais** | `itemComment` : allergies en premier (« Allergie : gluten, arachides — pas de traces »), puis consignes de préparation, puis l'ancien format |
| 3 | Deliveroo, commande : `delivery_notes` ignorées ; `cutlery_notes` remplacé par un marqueur « [Couverts demandés] » deviné, qui pouvait dire l'inverse du client | `orderComment` (`deliveroo_orders/comments.go`) : notes, « Couverts : <texte du client> », « Livraison : <texte> » ; « PAS DE COUVERTS » reste silencieux |
| 4 | Deliveroo, options : l'import d'une option jamais vue **échouait** (`configurable_attributes.product_id` obligatoire non renseigné) et la **commande entière n'était pas créée** | le groupe d'options « Deliveroo Options » est créé par produit, avec son `product_id` ; test d'intégration corrigé (il vérifiait l'échec) |

Les commentaires multi-lignes sont affichés tels quels par la caisse. Une ligne
d'article scindée par une promotion (voir `marketplace-promotions-tva.md`) porte le
commentaire sur chacune de ses parties : c'est la même consigne pour les deux unités.

## 4. Ce que la caisse affiche (Flutter, lecture seule)

| écran | commentaire de commande | commentaire d'article | options de l'article |
|---|---|---|---|
| Commande en cours, une fois ouverte (panier) | oui (bouton « note », pastille active) | oui (`product_item_cell`) | oui |
| Liste des commandes en cours (`order_cell`) | **non** (aucune indication) | – | – |
| Production, mode classique (par commande) | oui (`classic_product_cell`) | oui (`product_row_cell`) | oui |
| Production, mode focus produit — produits notifiés | oui (`production_order_cell`) | oui (`focus_product_cell`) | oui |
| Production, mode focus produit — produits non notifiables | oui | oui | **non** (nom et commentaire seulement) |
| Commandes terminées récemment | oui | – | – |

Pas de modification Flutter faite. Écarts à décider :

1. La liste des commandes en cours n'indique pas qu'une commande porte une consigne
   (allergie, livraison) : il faut l'ouvrir. Une pastille « note » sur la carte
   suffirait.
2. En focus produit, un produit non notifiable n'affiche ni ses options ni ses
   suppléments : les options Deliveroo (consignes de préparation) n'y sont donc pas
   visibles pour ces produits.

## 5. À valider sur de vraies commandes

- **Uber Eats** : une commande avec une consigne de commande, une consigne d'article
  et une allergie. Les champs `cart.special_instructions`, `special_instructions` et
  `special_requests` sont désactivés par défaut pour les établissements connectés
  (l'allergie doit être activée par Uber) : si rien ne remonte, c'est l'activation
  qui manque, pas le code.
- **Deliveroo** : une commande avec `cutlery_notes` et `delivery_notes`, et une
  commande avec une option jamais importée. Les valeurs de `cutlery_notes` ne sont
  pas documentées ; seul « PAS DE COUVERTS » est traité comme « rien à signaler ».
- Pour obtenir un payload réel d'Uber Eats : `GET /v2/eats/order/{brand_order_id}`
  (jeton `eats.order`), `brand_order_id` se lit dans `orders`.

## 6. Journal des décisions

| date | décision | raison |
|---|---|---|
| 2026-10-29 | Lire les champs documentés et garder les anciens en repli | aucun payload réel avec consigne n'a pu être consulté ; ne rien perdre dans les deux cas |
| 2026-10-29 | Allergies en tête du commentaire d'article | sécurité alimentaire : doivent être vues en premier |
| 2026-10-29 | Texte du client reproduit tel quel pour les couverts Deliveroo | valeurs non documentées ; un marqueur deviné pouvait inverser le sens |
| 2026-10-29 | Pas de mention quand le client ne veut pas de couverts (Uber) | la valeur `false` est fréquente, le bruit masquerait les vraies consignes |
| 2026-10-29 | Groupe d'options Deliveroo par produit | `configurable_attributes.product_id` obligatoire ; les autres produits s'y rattachent déjà par `product_configurable_attribute` |
