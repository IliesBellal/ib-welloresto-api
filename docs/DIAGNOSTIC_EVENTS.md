# Journal de diagnostic (`diagnostic_events`) et contrôle de cohérence des commandes

2026-10-13. Rédigé au fil des décisions. Non commité, non déployé.

## Pourquoi

Le registre 934 du 5 octobre 2026 avait 12,00 € d'encaissements sans base de TVA.
Cause : la commande 38912 (une pizza à 8,70 €) a été enregistrée avec un total de
20,70 €, payée en deux fois (8,70 puis 12,00) et fermée. La requête de création
(`api_request_logs` #403842) montre que la tablette a envoyé `TTC: 2070` avec
`HT: 791` et `TVA: 79`, soit 8,70 €. Le tarif serveur (`POST /orders/pricing`)
répondait 870 à 21:03 : le serveur calcule juste, mais enregistre le total reçu
sans le comparer aux lignes (`req.Order.TTC`), et une commande est « payée » dès
que `price <= somme des paiements`. L'origine côté tablette n'a pas pu être
établie depuis les logs (aucune requête entre 21:03:32 et 21:14:26).

Deux correctifs : garder une trace exploitable du prochain cas (ce document), et
empêcher la tablette d'envoyer un total incohérent (section Application Flutter).

## La table

`migrations/todo/182_diagnostic_events.up.sql` (à appliquer par vos soins, avant
ou avec le code ; sans elle le code fonctionne et ne journalise que dans les logs).

| Colonne | Rôle |
|---|---|
| `kind` | type stable, clé de filtre et de purge (`order_price_mismatch`, `order_overpaid`) |
| `severity` | `info`, `warn`, `error` |
| `merchant_id`, `user_id` | contexte, texte |
| `request_id` | identifiant de requête des logs applicatifs |
| `subject_type`, `subject_id` | objet concerné (`order`, `38912`) |
| `data` | jsonb libre propre au type |

Écriture : `diagnostics.Record(ctx, diagnostics.Event{...})`
([diagnostics.go](../internal/diagnostics/diagnostics.go)). Toujours un `Warn` de
log `diagnostic_event`, puis une file asynchrone de 500 événements écrite par une
goroutine. Ne bloque jamais et n'échoue jamais pour l'appelant : file pleine, base
indisponible ou table absente = l'événement reste dans les logs. Initialisé dans
`SetupRoutes` (`cmd/api/routes.go`). Purge quotidienne à 5 h 30 au-delà de 180 jours
(`CleanupOldDiagnosticEvents`, [diagnostic_events.go](../internal/tasks/diagnostic_events.go)).

Ajouter un usage : choisir un `kind`, appeler `Record` avec un `Data` sérialisable.
Aucun changement de schéma.

## Premier usage : cohérence des commandes

[order_consistency.go](../internal/modules/order_life_cycle/order_consistency.go),
appelé **après succès** de la création, de la modification et de chaque paiement
(`CreateOrder`, `UpdateOrder`, `CreatePayment*` du service `order_life_cycle`).

- `order_price_mismatch` : `orders.price` différent de la somme des lignes plus les
  frais de livraison. Lignes = (prix de la ligne + suppléments `extra` + options
  payantes) × quantité, même formule que le ticket de caisse et la TVA des registres.
- `order_overpaid` (paiement seulement) : paiements actifs hors remises supérieurs à
  `orders.price`.
- `data` : étape, total stocké, somme des lignes, écart, payé, remises, canal, état,
  statut, type de commande, créateur.

### Décisions

1. **Journaliser, ne rien bloquer côté serveur.** Un refus en plein service fait
   perdre une vente, pire qu'une incohérence consignée. Le blocage viendra après
   lecture des cas réels.
2. **Après commit, hors transaction de l'appelant.** La lecture utilise sa propre
   connexion (`context.Background()` et 3 s) : `ExecuteOrderMutation` peut être
   appelée dans une transaction, où une requête en échec rendrait les suivantes
   invalides. Conséquence assumée : dans ce cas la lecture ne voit pas encore le
   paiement, donc au pire un excédent n'est pas détecté, jamais un faux positif.
3. **Uber Eats et Deliveroo exclus** : leur prix vient de la plateforme et on ne
   sait pas s'il inclut déjà les modificateurs (même règle que `GetOrderSaleLines`).
4. **Pas de corps de requête dans l'événement** : il contient des données clients.
   Le corps complet est dans `api_request_logs` (30 jours) : relier par heure et URL,
   et par `user_id` / `merchant_id`, désormais remplis (voir plus bas).
5. **Pas de dédoublonnage** : une commande incohérente journalise à chaque étape,
   ce qui montre aussi quand l'écart apparaît.

### Interroger

```sql
-- Dernières anomalies de prix
SELECT created_at, merchant_id, subject_id, data->>'stage' AS etape,
       (data->>'stored_price')::int AS total, (data->>'lines_sum')::int AS lignes,
       (data->>'delta')::int AS ecart, data->>'order_created_by' AS createur
FROM diagnostic_events WHERE kind = 'order_price_mismatch' ORDER BY created_at DESC LIMIT 100;

-- À quelle étape l'écart apparaît-il en premier pour une commande ?
SELECT created_at, kind, data FROM diagnostic_events
WHERE subject_type = 'order' AND subject_id = '38912' ORDER BY created_at;

-- Écarts par établissement et par jour
SELECT date_trunc('day', created_at) AS jour, merchant_id, count(*)
FROM diagnostic_events WHERE kind = 'order_price_mismatch' GROUP BY 1, 2 ORDER BY 1 DESC;
```

### Faux positifs connus

Sur staging, les commandes 34243 et 33911 (anciennes) sortent en écart : leurs lignes
portent des options payantes dont le prix a été lu au catalogue (`extra_price` figé
absent avant la migration 173), alors que le total ne les contenait pas. Les
commandes créées depuis portent un prix figé. À regarder sur la production avant de
décider d'un blocage.

## Identité dans `api_request_logs`

Les colonnes `user_id` et `merchant_id` n'ont jamais été remplies : le journal lisait
des clés de contexte que personne ne posait, et le contexte dérivé par
l'authentification est invisible une fois la requête traitée. Désormais le journal
dépose un `models.RequestIdentity` dans la requête, rempli par l'authentification
([auth.go](../internal/middleware/auth.go),
[request_logger/middleware.go](../internal/middleware/request_logger/middleware.go)).
`user_id` est un `bigint` : un identifiant non numérique reste `NULL`. Les lignes
déjà écrites restent à `NULL`.

## Application Flutter : garde de cohérence

`wello_resto_flutter/lib/controllers/order/order_total_guard.dart`, appelée dans
`OrderController._saveOrder` juste avant l'envoi. Si le TTC de la commande à
enregistrer diffère de ses lignes (formule de `computeTotals` du tarif serveur :
(prix effectif + suppléments + options sélectionnées) × quantité + livraison) :

1. un nouveau tarif serveur est demandé une fois ;
2. s'il reste incohérent, ou que le tarif est périmé, la commande n'est pas
   enregistrée (« Total incohérent », même traitement que « Total non vérifié »).

Choix : bloquer côté tablette est sans risque pour les données (le caissier
re-tarife en retirant puis rajoutant un article), contrairement à un blocage
serveur qui toucherait aussi les canaux de plateforme. Le garde-fou ne remplace pas
la cause, toujours inconnue : le diagnostic serveur dira à quelle étape l'écart naît.

## Statut d'exécution (2026-10-13)

- Serveur : compilation, `go vet`, tests unitaires (`diagnostics`, `tasks`,
  `order_life_cycle`, `middleware`) : OK. La requête de cohérence a été exécutée sur
  **staging, en lecture seule** (valide en Postgres ; elle y signale les deux
  commandes anciennes ci-dessus).
- Migration 182 : **non appliquée**. L'écriture réelle dans `diagnostic_events`,
  la purge et les hooks de bout en bout n'ont **pas été testés** : pas de Postgres
  Docker local, et je n'écris pas en base partagée.
- Flutter : `flutter analyze` sans erreur sur les fichiers touchés, tests du garde
  (5) et des contrôleurs existants (14) : OK. Le branchement dans `_saveOrder` n'est
  pas couvert par un test automatique ; à essayer sur une tablette : commande simple,
  commande avec option payante, commande en livraison, offre « 2 pizzas pour 12 € ».
- Les 12 € de la commande 38912 restent à régulariser par avoir (registre validé).

## Import des commandes Uber Eats / Deliveroo : `marketplace_import_degraded`

Écrit quand une commande de plateforme a dû remplacer un article, une option ou son
client par du texte au lieu d'échouer (docs/marketplace-fiabilite-import.md, lots 3 et 5).
La commande est créée ; l'événement dit ce qui a été remplacé.

- `subject_type` : `uber_eats_product`, `uber_eats_modifier_group`, `uber_eats_modifier_option`,
  `deliveroo_product`, `deliveroo_option`, `uber_eats_customer` ou `deliveroo_customer`.
- `subject_id` : identifiant de l'article ou de l'option chez la plateforme (de la commande
  pour un client).
- `data` : `brand`, `what` (`product`, `modifier_group`, `modifier_option`, `option`,
  `customer`), `title` (sauf client) et `error` (cause SQL).

```sql
-- Dégradations des dernières 24 h, par cause
SELECT subject_type, data->>'what' AS quoi, left(data->>'error', 90) AS cause, count(*)
FROM diagnostic_events
WHERE kind = 'marketplace_import_degraded' AND created_at > now() - interval '1 day'
GROUP BY 1, 2, 3 ORDER BY 4 DESC;
```

Une cause fréquente avant la migration 197 : `invalid input syntax for type integer`
(mapping d'un groupe d'options Uber Eats avec identifiant texte).
