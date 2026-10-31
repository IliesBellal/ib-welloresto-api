# Fiabilité de l'import des commandes Uber Eats / Deliveroo

> Chantier du 2026-10-29. Objectif : une commande payée sur une plateforme ne doit
> jamais disparaître sans que personne le sache. Lots 1 à 5 faits (§2 à §6), restes au
> §7. Complète `marketplace-commentaires.md` et `marketplace-promotions-tva.md`.
> **Une migration à appliquer** : 197 (§5), sans ordre imposé.

## 1. Constat (audit du 2026-10-29)

Trois façons de perdre une commande, sans erreur visible :

| # | cause | effet |
|---|---|---|
| 1 | L'événement était marqué « traité » dans Redis **avant** le traitement et jamais effacé en cas d'échec | le webhook répondait 500, la plateforme réessayait avec le même identifiant, la nouvelle tentative était prise pour un doublon et recevait 200 : commande perdue |
| 2 | `CreateOrder` renvoyait `unavailable_products` (sans erreur) dès qu'un produit était `out_of_stock` | les webhooks affichaient « commande importée » et tentaient d'accepter un identifiant vide : le client avait payé, le restaurant ne voyait rien |
| 3 | Les webhooks ne regardaient pas le statut de la création | un résultat autre que `success` passait pour un succès |

## 2. Lot 1 — nouvelle tentative réelle (`internal/webhook/idempotency`)

`idempotency.Run` remplace le « marquer puis traiter » d'Uber Eats
(`ubereats/service.ProcessEvent`) et de Deliveroo (`deliveroo_orders.ProcessEvent`) :

- l'événement est **réservé** (`SetNX`, 5 minutes) pour la durée du traitement : un
  doublon simultané est ignoré ;
- la marque « traité » (3 h Uber Eats, 1 h Deliveroo, comme avant) n'est posée qu'en
  **cas de succès** ;
- sur **erreur ou panique**, la réservation est levée et le webhook répond 500 : la
  tentative suivante de la plateforme est vraiment traitée. Une panique devient une
  erreur (500) au lieu de couper la connexion ;
- **Redis indisponible** : `SetNX` renvoie `false` dans ce cas comme pour une clé
  existante ; `Run` relit la clé pour trancher et traite l'événement sans réservation
  plutôt que de le perdre. Le contrôle de doublon en base (`ExistsByBrandOrderID`)
  reste le filet.

Les webhooks traitent aussi un résultat de création autre que `success` comme une
erreur.

## 3. Lot 2 — rupture de stock : créer, signaler, laisser décider

Pour Uber Eats et Deliveroo (`models.IsMarketplaceBrand`), `CreateOrder` ne renvoie
plus `unavailable_products` :

1. la commande est **créée** avec toutes ses lignes ;
2. la rupture est écrite **en tête du commentaire** : « Rupture de stock : X, Y (à
   vérifier avant d'accepter) », suivie du texte du client ; le commentaire apparaît
   en commande ouverte et en production (voir `marketplace-commentaires.md`) ;
3. `CreateOrderResult.UnavailableProducts` liste les produits ; les webhooks
   **n'acceptent pas automatiquement** la commande, et `createOrder` la **notifie**
   même quand l'acceptation automatique avait demandé de ne pas le faire ;
4. le personnel accepte ou refuse comme d'habitude (le refus passe par le flux de
   refus existant de chaque plateforme, inchangé).

Les commandes de la caisse, de la borne et du Scan&Order gardent leur comportement :
`unavailable_products` et aucune commande créée.

**Deliveroo, échec de création** : `ProcessNewOrder` envoie le statut de synchro
`failed` / `webhook_failed`. Ce statut n'est pas final (documentation Deliveroo) : la
tablette Deliveroo prévient l'équipe tout de suite de vérifier le système en caisse
(sans statut, elle le fait après 3 minutes), et la nouvelle tentative du webhook peut
encore réussir.

**Uber Eats, échec de création** : pas de refus automatique. Un refus n'est pas
rattrapable et la nouvelle tentative d'Uber peut réussir ; l'erreur est journalisée
et le webhook répond 500.

## 4. Lot 4 — création atomique (fait)

**Constat.** Aucun appelant n'ouvrait de transaction pour créer une commande de
plateforme : chaque requête était validée seule. Une panne après l'en-tête (paiement
refusé, connexion coupée, annulation du contexte) laissait une commande sans lignes
ou sans paiement, que le contrôle de doublon (`ExistsByBrandOrderID`) empêchait
ensuite de recréer — les nouvelles tentatives du lot 1 ne pouvaient pas la réparer.
Mesuré sur staging : un paiement refusé laissait un client, un en-tête, deux lignes et
deux commentaires.

**Correction** (`OrdersLifeCycleRepository.CreateOrder`). Pour Uber Eats et Deliveroo
seulement (`models.IsMarketplaceBrand`), la création tourne dans `dbutils.RunInTx` :
tout ou rien. Les notifications, l'acceptation automatique et le contrôle de
cohérence ont lieu **après** la validation (ils sont dans `createOrder` du service,
au-delà de l'appel au dépôt). La caisse, la borne et le Scan&Order passent par
`r.createOrder` exactement comme avant.

**Étapes tolérantes préservées.** Sous Postgres, une requête en échec invalide toute
la transaction. Sept étapes avalent aujourd'hui leurs erreurs (le commentaire de la
commande, celui d'une ligne, l'attribution de remise d'une ligne, les coûts de revient
des recettes, des options et des suppléments, le numéro de commande) ; elles sont
isolées par des points de sauvegarde (`dbutils.Isolated`, `dbutils.Shield`) qui
reviennent à l'état d'avant l'échec. Les points de sauvegarde ne sont posés que si le
contexte porte `dbutils.WithSavepoints`, ce que seul `CreateOrder` fait pour une
commande de plateforme : la modification d'une commande et les autres flux déjà en
transaction ne changent ni de comportement ni de nombre de requêtes.

**Vérifié.**
- Test d'intégration : un paiement refusé après l'en-tête ne laisse **aucune trace**
  (commandes, lignes, commentaires, paiements, clients). Le même test **échoue sans la
  transaction** (mutation vérifiée).
- Une commande réussie écrit exactement la même chose que le chemin sans transaction
  (lignes, prix, TVA figée, commentaires, paiement, `isPaid`).
- Un commentaire impossible à écrire (`user_id` trop long) n'empêche toujours pas la
  création, et la suite (lignes, paiement) se déroule normalement.
- Les points de sauvegarde sont validés sur Postgres, avec et sans le marqueur.
- Suites d'intégration complètes de `order_life_cycle`, `orders`, `scannorder`,
  `kiosk`, `receipt` et des webhooks : vertes.

**Risques connus et acceptés.**
- *Numéro de commande.* `orders` n'a pas d'index unique sur `(merchant_id, order_num)` :
  aucune erreur possible. Mais l'en-tête d'une commande de plateforme n'est visible
  des autres commandes qu'à la validation (quelques dizaines de millisecondes) : deux
  commandes du même établissement créées dans cette fenêtre peuvent recevoir le même
  numéro d'affichage. Cosmétique ; la fenêtre existait déjà, en bien plus courte.
- *Latence.* Environ 3 requêtes de plus par ligne (point de sauvegarde) et 9 par
  commande, soit quelques dizaines de millisecondes sur un traitement asynchrone.
- *Connexions.* La transaction garde une connexion du pool (15) pendant qu'un calcul
  d'estimation en emprunte une seconde (hors transaction). Un blocage demanderait 15
  créations de plateforme simultanées sur une même instance.
- *Verrou de la chaîne des paiements.* Il est tenu jusqu'à la validation au lieu de
  quelques millisecondes ; le paiement est l'avant-dernière étape, la durée ajoutée est
  négligeable et aucun autre flux ne touche une commande avant sa validation.

## 5. Lot 3 — dégrader au lieu d'échouer sur un article ou une option (fait)

**Constat.** Toute erreur sur un article, un groupe d'options ou une option faisait
échouer la commande entière. Quatre causes, trouvées dans le code et sur le schéma :

| cause | effet |
|---|---|
| Titres sans limite de taille, colonnes bornées : `configurable_attributes.name` (50), `.title` (80), `configurable_attribute_options.title` (80), `products.name` (255) | un groupe d'options Uber de plus de 47 caractères, une option de plus de 80 : insertion refusée, commande perdue |
| **`integration_uber_eats_attributes_mapping.configurable_attribute_id` est un `integer`** alors que les groupes créés depuis portent un identifiant texte (`attribute-<uuid>`, `attr-<uuid>`) | `CreateAttributeMapping` échoue en 22P02 pour **tout** groupe d'options Uber jamais vu : la commande est perdue. Les groupes déjà mappés fonctionnent, d'où un défaut resté invisible |
| Deux commandes simultanées avec le même article ou option inconnus | doublons de produit (rien n'est unique), ou échec sur l'index unique du mapping d'option |
| Produit et mapping créés par deux requêtes séparées | un produit sans mapping si la seconde échoue, recréé en double à la commande suivante |

**Correction.**
- **Chemin courant inchangé** : un article, un groupe ou une option déjà mappés se lisent,
  sans transaction ni verrou.
- **Création atomique et sérialisée** (`dbutils.RunInTxLocked`, verrou consultatif par
  article, groupe ou option) : relecture sous verrou, puis produit + mapping créés
  ensemble. Deux commandes simultanées ne créent qu'un produit ; un échec n'en laisse
  aucun morceau.
- **Troncature** à la taille des colonnes (`helpers.TruncateRunes`, par caractères, emojis
  compris) pour les titres de groupe, d'option, de produit, les noms de client, les
  numéros et les adresses.
- **Repli visible** quand la création échoue malgré tout :
  - une option ou un groupe non créable passe en texte dans le commentaire de la ligne
    (« Options Base : Tomate, Crème ×2 ») ; le prix des options est déjà dans le prix
    unitaire d'Uber Eats ;
  - un article non créable prend l'**article de repli** de l'établissement (« Article
    Uber Eats non reconnu » / « Article Deliveroo non reconnu », créé une fois, TVA 10 %),
    au prix de la plateforme, avec le vrai titre et les options en texte dans le
    commentaire (« Article Uber Eats : Pizza Reine | … ») ;
  - chaque dégradation écrit un événement `marketplace_import_degraded`
    (`diagnostic_events`, voir `DIAGNOSTIC_EVENTS.md`).
- **Une erreur de lecture** (base en difficulté) n'est pas une dégradation : elle remonte,
  la nouvelle tentative de la plateforme traite la commande avec ses options structurées.
  Si même l'article de repli est impossible, la commande échoue (nouvelle tentative).

**Migration 197 — à appliquer par Ilies** (`migrations/todo/197_uber_attributes_mapping_text_id`) :
`configurable_attribute_id` en `varchar(64)`. Essayée sur staging dans une transaction
annulée : aucune clé étrangère ni index à reconstruire, les 22 mappings existants restent
joignables, et la séquence complète (groupe texte, mapping, option, mapping d'option)
passe ensuite. **Ordre indifférent** : avant la migration un groupe inconnu passe en
texte ; après, il est créé et mappé normalement. Aucun index unique à ajouter : le verrou
remplace celui que j'avais annoncé.

## 6. Lot 5 — le client ne bloque plus la commande (fait)

- **Client facultatif.** Pour Uber Eats et Deliveroo, si l'enregistrement du client
  échoue (nom de plus de 50 caractères, par exemple), la commande est créée sans client
  et « Client : <nom> » est ajouté à la fin de son commentaire (événement de diagnostic
  en plus). La plateforme masque l'identité du client : il n'est qu'un libellé. Le
  comportement des autres canaux est inchangé (l'échec reste une erreur).
- **Garde sur les convives.** Uber Eats : une commande sans convive (`eaters` vide) ne
  plante plus ; le premier convive identifie le client quand il existe.
- **Numéro temporaire enregistré.** Le numéro anonymisé d'Uber Eats et son code
  (`eater.phone`, `phone_code`) allaient dans la requête mais jamais en base
  (`customer_temporary_phone`/`_code` absents de la liste des colonnes écrites) ;
  Deliveroo : numéro intermédiaire et `contact_access_code`. Seules les plateformes
  envoient ces champs (la caisse n'en envoie jamais, vérifié dans l'application).
  Le numéro anonymisé n'est volontairement pas écrit dans `customer_tel`.
- **Adresse Uber Eats** (livraison par l'établissement) : `street_address`, sinon la
  première ligne saisie (`title`) pour une adresse `GOOGLE_PLACE`, puis appartement et
  immeuble. **Adresse Deliveroo** : `line2` ajoutée ; sans elle le format est identique
  à avant.
- **Un client reste créé par commande** (l'identifiant de plateforme n'est pas stocké :
  il faudrait une colonne, donc une migration). À traiter à part si besoin.

## 7. Ce qui reste

| sujet | note |
|---|---|
| Refus vers Uber | le client appelle l'ancien format (`/v1/delivery/order/{id}/deny`, `DenyReason`) ; la documentation actuelle décrit `deny_pos_order` avec `reason.code` (`ITEM_AVAILABILITY`, `out_of_stock_items`…). À confirmer avec Uber avant tout changement |
| Adresse Uber `GOOGLE_PLACE` | seule la première ligne saisie est connue ; l'adresse complète demanderait un appel à Google Places pendant la création, non fait (latence et nouvelle cause d'échec) |
| Doublons de clients | un client par commande de plateforme, sans identifiant de plateforme stocké |
| Options payantes Deliveroo | absentes des lignes (voir `marketplace-commentaires.md`) |

## 8. Tests

- Lots 3 et 5 : repli Uber Eats et Deliveroo avec de faux dépôts (option, groupe, article
  non créables, lecture en échec, article de repli en échec, chemin courant sans
  création) ; client Uber Eats (commande sans convive, numéro temporaire, troncature,
  adresses) et Deliveroo (Line2, code d'accès, format d'adresse inchangé) ; note
  « Client : » du commentaire.
- Lots 3 et 5, intégration Postgres (staging) : 8 commandes simultanées avec le même
  article, groupe et option inconnus créent un seul produit et un seul mapping (mutation
  vérifiée : sans verrou, `duplicate key … uq_integration_deliveroo_options_mapping`) ;
  verrou déterministe (même clé attend, autre clé non, erreur libère) ; article de
  repli unique ; option dont le mapping échoue : aucune option orpheline et commande
  créée ; client non enregistrable : commande créée sans client, nom dans le
  commentaire, et erreur inchangée hors plateforme ; numéro temporaire enregistré.
- `idempotency` : succès puis doublon ignoré ; **échec puis nouvelle tentative traitée** ;
  panique ; doublon simultané ; Redis indisponible ; sans Redis.
- `order_life_cycle` : texte de l'avertissement de rupture ; `IsMarketplaceBrand`.
- Intégration Postgres (exécutée sur staging) : une commande Uber Eats et une
  commande Deliveroo avec un produit en rupture sont créées avec leurs deux lignes, la
  rupture en tête du commentaire et le texte du client à la suite ; sans rupture rien
  ne change ; le test existant de la caisse (produit bloqué → `unavailable_products`)
  passe toujours.

## 9. Journal des décisions

| date | décision | raison |
|---|---|---|
| 2026-10-29 | Marque « traité » seulement après succès, réservation levée sur échec | la plateforme réessaie avec le même identifiant ; l'ignorer perdait la commande |
| 2026-10-29 | Redis indisponible = traiter sans réservation | ne jamais perdre une commande pour une panne de cache |
| 2026-10-29 | Marketplace en rupture : créer, signaler, pas d'acceptation automatique | le client a déjà payé ; le personnel décide, avec les flux de refus existants |
| 2026-10-29 | Pas de refus automatique côté Uber Eats | irréversible alors que la nouvelle tentative peut réussir |
| 2026-10-29 | `webhook_failed` envoyé à Deliveroo sur échec de création | statut non final prévu par Deliveroo pour ce cas |
| 2026-10-30 | Transaction pour Uber Eats et Deliveroo seulement | zéro régression : la caisse, la borne et le Scan&Order gardent leur chemin actuel |
| 2026-10-30 | Points de sauvegarde pilotés par `WithSavepoints`, posés dans les fonctions partagées | les étapes tolérantes gardent leur comportement ; les flux déjà en transaction ne changent pas |
| 2026-10-30 | Tests d'intégration contre l'état d'avant (mutation vérifiée) | prouver que le test détecte la commande à moitié créée |
| 2026-10-30 | Création sous verrou consultatif plutôt qu'un index unique sur le mapping de produit | pas de migration pour la partie produit, pas de doublons existants à purger avant de poser l'index, et produit + mapping atomiques |
| 2026-10-30 | Troncature plutôt que repli pour les textes trop longs | l'information est conservée et la commande garde ses options structurées |
| 2026-10-30 | Article de repli par établissement et par plateforme, vrai titre dans le commentaire | une commande payée ne doit pas être perdue pour un article ; la cuisine voit le plat réel |
| 2026-10-30 | Une erreur de lecture remonte, une erreur de création dégrade | la lecture en échec signale une base en difficulté : la nouvelle tentative donne les options structurées |
| 2026-10-30 | Client facultatif pour Uber Eats et Deliveroo, nom dans le commentaire | le client d'une plateforme n'est qu'un libellé ; le personnel reconnaît la commande à son nom |
| 2026-10-30 | Numéro anonymisé en colonnes temporaires, pas dans `customer_tel` | `customer_tel` sert aux SMS et à l'identité du client ; un numéro intermédiaire à code n'y a pas sa place |
| 2026-10-30 | Migration 197 sans ordre imposé | avant : le groupe inconnu passe en texte ; après : créé et mappé |
