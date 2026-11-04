# Synchro du menu Uber Eats

Objectif : WelloResto est l'unique endroit où le restaurateur gère son menu Uber Eats.
Les produits cochés « Uber Eats » sont publiés depuis le menu WelloResto, et les
disponibilités suivent WelloResto (désactiver une pizza la désactive sur WelloResto,
Deliveroo et Uber Eats). Le code commun aux plateformes est `marketplacecatalog` ;
pendant Deliveroo : [deliveroo-menu-sync.md](deliveroo-menu-sync.md).

## Règles

| WelloResto (`products.status`) | Uber Eats |
|---|---|
| `available` | en vente (suspension retirée) |
| `out_of_stock` | article suspendu **sans limite de durée** (`suspend_until` = 8 640 000 000) |
| `not_available` | **retiré du menu publié** ; suspendu en attendant la republication |
| retiré / décoché | idem `not_available` |

- Uber Eats ne remet **jamais** un article en vente seul : c'est WelloResto qui le fait.
  C'était le défaut de l'ancienne version (suspension de 30 minutes par événement, jetons
  d'erreur ignorés, aucun rattrapage) — supprimée.
- **WelloResto fait foi.** Chaque alignement est calculé depuis l'état courant, jamais depuis
  un événement : deux changements rapprochés donnent le même résultat quel que soit l'ordre.
- Prix : prix Uber Eats > prix de livraison > prix de base. TVA publiée = vraie TVA de
  livraison du produit (`tax_info.vat_rate_percentage`), jamais déduite de l'alcool.
- Un produit à plus de 250 € n'est pas publié (Uber refuserait tout le menu) : il est
  signalé dans le journal (`warnings`).
- Alcool : `products.contains_alcohol` (saisi par le restaurateur) → classification
  `dish_info.classifications.alcoholic_items = 1`.

## Publication (`PublishMenu`, `menu_publish.go`)

1. Menu WelloResto → catalogue commun → document Uber (`menu_document.go`, schéma de la doc
   Uber : `menus`, `categories`, `items`, `modifier_groups`).
2. Horaires du menu (`service_availability`) = **horaires d'ouverture** de l'établissement
   (`hours_of_operation`, pas les horaires de livraison, réservés à ScanNOrder), jour par jour,
   créneaux passant minuit coupés à 23:59. Sans horaires : publication refusée
   (« Renseignez d'abord vos horaires d'ouverture ») — publier en 24 h/24 ouvrirait le
   restaurant en permanence.
3. Document identique au dernier publié → rien n'est envoyé (`unchanged`). L'empreinte
   **ignore les ruptures de stock** : elles passent par la mise à jour d'article.
4. Tables de correspondance de l'import des commandes **écrites avant l'envoi** :
   produits (`item_id` = id produit), options (`opt_<id>`), groupes (`mg_<id>`). Une commande
   passée sur le nouveau menu retrouve toujours son produit.
5. `PUT /v2/eats/stores/{id}/menus` : **remplace** le menu chez Uber ; réponse 204
   synchrone, aucun webhook de résultat. Le journal `marketplace_menu_syncs` est donc clos
   tout de suite.
6. L'état envoyé de chaque article est mémorisé (`marketplace_item_states`, migration 203) :
   Uber ne permet pas de relire les suspensions.

### Premier envoi : confirmation

Le premier envoi d'un compte **remplace le menu actuel** d'Uber Eats. L'API répond
`409 {"error":"confirmation_required"}` tant que `confirm_replace=true` n'est pas passé
(`PATCH /menu/uber-eats/sync?confirm_replace=true`, idem Deliveroo
`/menu/deliveroo/sync`). Le back-office affiche une fenêtre d'avertissement. Les routes de
certification Deliveroo (`/test/deliveroo/*`) et la synchro automatique ne sont pas concernées.

## Disponibilités (`menu_availability.go`)

À chaque modification du catalogue (`MenuChangeNotifier`) : alignement dans les 3 secondes
(rafales regroupées) — compare WelloResto à l'état envoyé et n'envoie que la différence,
une requête `POST .../menus/items/{id}` par article (`suspension` à `null` pour remettre en
vente). Un article en erreur n'empêche pas les suivants ; il est retenté au réveil suivant.

`TODO(options)` : les options (articles `opt_<id>`) ne sont pas alignées.

## Réveils (`menu_wakeups.go`, cron chaque minute)

Table `marketplace_sync_state` (`platform = 'uber_eats'`), un compte y entre à sa première
publication. Pas d'heure d'ouverture à surveiller (Uber ne remet rien à zéro le matin).

- menu modifié → republié après **3 minutes de calme**, seulement s'il a vraiment changé
  (produit masqué/décoché, nouveau produit, horaires...) ;
- filet de sécurité toutes les 2 à 3 h (envoi raté, redémarrage, horaires modifiés) ;
- `store.menu_refresh_request` (webhook Uber) → menu renvoyé au prochain tour ;
- `store.status.changed` : journalisé seulement.

## Journal

`GET /integrations/uber-eats/syncs?store_id=&limit=` — mêmes lignes que Deliveroo
(`platform = 'uber_eats'`), statuts `success` / `failed` / `unchanged`.

## Migrations

- **203** `marketplace_item_states` (à jouer avant le code) ; 200, 201, 202 déjà jouées ;
  197 (groupes d'options en texte) nécessaire pour les groupes dont l'id n'est pas numérique.

## Points à confirmer au sandbox

1. **Alcool** : la doc Uber place `classifications` à deux endroits (page Upload vs guide :
   `dish_info.classifications`). Implémenté : `dish_info.classifications.alcoholic_items`
   (constante `alcoholicItemsValue`, `menu_document.go`).
2. **Suspension sans limite** : `suspend_until = 8640000000` (exemple de la doc). À valider.
3. **`price_info`** : seul `price` est envoyé (la doc cite aussi `in_store_price`).
4. **Réenvoi du menu et suspensions** : non documenté ; le document porte les suspensions du
   moment, l'état envoyé est remplacé en conséquence.
5. **Mise à jour d'article sur une option** : non documenté.
6. Uber n'accepte la mise à jour d'article que si le menu a été envoyé **par l'API**.

## Protocole de test (sandbox, staging)

Prérequis : migration 203 jouée, établissement de test avec horaires d'ouverture et au moins
trois produits cochés Uber Eats (un avec options, un avec alcool, un autre), `UBER_EATS_*`
sur le sandbox. Après chaque étape, relire le journal (`/integrations/uber-eats/syncs`) et
`api_request_logs` : aucune étape ne demande de modifier le code.

1. **Premier envoi** : `PATCH /menu/uber-eats/sync` → 409 `confirmation_required` ;
   rejouer avec `confirm_replace=true` → `success`. Vérifier le menu dans l'app sandbox
   (catégories, prix, TVA, horaires, photos).
2. **Idempotence** : relancer → `unchanged`, rien envoyé.
3. **Rupture** : produit en « Épuisé » depuis la caisse → suspendu côté Uber en quelques
   secondes (log `disponibilités alignées`) ; pas d'expiration après 30 minutes.
4. **Remise en vente** : « Disponible » → plus de suspension.
5. **Double bascule** (disponible puis épuisé en rafale) : état final = épuisé.
6. **Retrait** : `not_available` → suspendu tout de suite, **retiré du menu** après 3 minutes
   de calme (nouvelle ligne `auto` dans le journal).
7. **Alcool** : produit avec la case Alcool → vérifier le retour de l'API (l'erreur éventuelle
   figure dans le journal) ; sinon essayer l'autre emplacement du champ.
8. **Commande de test** : passer une commande contenant un produit et une option → importée
   sur le **bon** produit (aucun doublon « UBER IMPORT »).
9. **Sans horaires** : supprimer les horaires → publication refusée avec message clair.
