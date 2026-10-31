# Recherche de clients (POS) — implémentation et exploitation

Chantier ouvert le 2026-10-30. Objectif : que le restaurateur retrouve le bon client
en quelques frappes au moment de la prise de commande (99 % des appels viennent du POS).
Ce document consigne les décisions **au fur et à mesure** ; l'état d'exécution
(ce qui a été lancé, ce qui ne l'a pas été) est en fin de document.

## 1. Constat de départ (avant le chantier)

Le POS appelle `GET /customers/search?term=…` sans `sort_field`, `page_size` ni `page`
(`wello_resto_flutter/lib/data/api/customer_api.dart`). Le serveur appliquait donc :
page 1, 10 résultats, tri `creation_date ASC`.

| # | Défaut | Conséquence |
|---|---|---|
| 1 | Tri par ancienneté, `match_score` calculé en Go **après** la pagination | Le meilleur candidat pouvait être hors des 10 premiers ; le POS n'affiche que la page 1 |
| 2 | Terme de téléphone systématiquement préfixé `+33` | Les derniers chiffres (« 5678 ») ne trouvaient rien |
| 3 | Terme entier comparé à chaque colonne séparément | « Jean Dupont » / « Dupont Jean » ne trouvaient rien si `customer_name` n'avait pas cet ordre |
| 4 | `UPPER(…) LIKE '%x%'` sur des expressions | Aucun index utilisable, parcours de tous les clients du marchand à chaque frappe |
| 5 | `COUNT(*)` séparé | Double parcours, total inutilisé par le POS |
| 6 | `UPPER` ne gère pas les accents | « francois » ne trouvait pas « François » |
| 7 | Email non cherché | |
| 8 | Terme non encodé dans l'URL côté POS | `+` lu comme espace, `&`/`#` coupent le terme |
| 9 | Recherche dès 1 caractère | Requêtes qui renvoient « tout » |

Mesures lues en lecture seule sur **staging** le 2026-10-30 (rappel : staging n'est pas
représentatif, ces chiffres ne servent qu'à valider les formats) :
PostgreSQL 18 ; `pg_trgm`, `unaccent`, `btree_gin` disponibles et non installées ;
téléphones : 144 / 21 397 hors format `+chiffres` (anciens formats) ; `customer_code`
quasi toujours NULL, sinon 4 chiffres ; `customer_name` = prénom + nom dans 24 874 / 26 226 lignes.

## 2. Décisions

**D1 — Une seule requête réécrite, pertinence calculée en SQL.** `SearchCustomers`
([repository.go](../internal/modules/customers/repository.go)) délègue la construction du filtre et
du score à [search.go](../internal/modules/customers/search.go) (fonctions pures, testées sans base).
Le score est calculé *avant* le `LIMIT` : le POS n'affiche que la première page, un calcul après
coup en Go ne voyait que les 10 lignes déjà choisies. L'ancien `computeScore` et ses helpers sont supprimés.

**D2 — La saisie est découpée en mots, chacun doit correspondre (AND), dans n'importe quel ordre.**
« jean dupont », « dupont jean », « jean dup » trouvent le même client. Les blocs de chiffres
adjacents sont refusionnés en un numéro (« 06 12 34 56 78 » arrive en 5 mots). Au plus 5 mots,
40 caractères par mot, 100 au total (borne le coût d'une requête).

**D3 — Barème (somme, par mot, de la *meilleure* correspondance).**

| Correspondance | Points |
|---|---|
| Code client exact (4 caractères max) | 1000 |
| Téléphone identique | 600 |
| Début d'un numéro international / fin d'un numéro saisi en chiffres seuls / début d'un numéro national (« 0612… ») | 450 |
| Fin d'un numéro saisi commençant par 0 | 400 |
| Mot = un prénom/nom exact | 500 |
| Un prénom/nom commence par le mot | 300 |
| Début de numéro (chiffres seuls) | 300 |
| Numéro qui contient les chiffres | 150 |
| Nom qui contient le mot | 100 |
| Email commence par / contient le mot | 80 / 40 |

À score égal : plus de commandes d'abord, puis dernière commande la plus récente, puis `customer_id`.
Raison : le client qui appelle est statistiquement un habitué. Le `match_score` renvoyé est ce score
de pertinence (le badge « forte correspondance » du back-office, seuil 200, garde son sens : mot exact,
début de nom, numéro). Il ne contient plus les bonus « profil complet » (4 × 40) ni le nombre de
commandes de l'ancien calcul : ils faisaient dépasser 200 à des correspondances faibles.

**D4 — Pertinence par défaut seulement si aucun tri n'est demandé.** Le POS n'envoie ni `sort_field`
ni `sort_dir` → tri par pertinence. Le back-office envoie un tri explicite quand l'utilisateur en
choisit un → il est respecté à l'identique. Sans terme, ordre historique (`creation_date`).

**D5 — Téléphone.** Comparaison sur les chiffres seuls (`customer_search_phone`), ce qui absorbe les
144 anciens formats hors `+…` vus sur staging. Quatre cas : national (« 0612… » → préfixe stocké
`33612…` OU forme locale ancienne), international (« +33… » / « 0033… »), chiffres seuls (le plus
souvent la fin du numéro : « 5678 »), et un numéro de 4 chiffres peut aussi être un code client.
Le préfixe `33` (France) est une constante (`searchFRCountryDigits`), comme le `"FR"` de
`NormalizePhoneNumber` partout ailleurs. **Corrige le défaut d'origine** : « 5678 » était transformé
en `+335678` et ne trouvait rien.

**D6 — Seuils.** Mot de 1 caractère ou numéro de moins de 3 chiffres : ignoré. Si plus rien
d'exploitable ne reste, l'API renvoie une liste vide **sans interroger la base**. Conséquence assumée
pour le back-office : une recherche d'une seule lettre ne renvoie plus « les 10 plus anciens clients
contenant cette lettre » mais rien. L'email n'est cherché qu'à partir de 3 caractères (« ma » ne doit pas
remonter tous les « gmail »).

**D7 — Total dans la même requête** (`COUNT(*) OVER ()`) : plus de second parcours. Page hors bornes
(aucune ligne, donc pas de total) : un `COUNT(*)` de secours uniquement dans ce cas. La liste vide est
désormais `[]` et non `null`.

**D8 — Accents et casse : en base, pas en Go.** `customer_search_norm` = minuscules + `unaccent` +
tirets/apostrophes comptés comme espaces (« Jean-Pierre » ↔ « jean pierre »). Le terme saisi passe par
la *même* fonction SQL que les colonnes, donc aucune divergence possible entre deux tables de
correspondance de caractères.

**D9 — Index (migration 198).** Trois index GIN trigramme, un par expression (nom, téléphone, email),
préfixés par `merchant_id` via `btree_gin` (une recherche ne parcourt que les clients du marchand) et
**partiels** : mêmes prédicats que la requête (clients visibles, hors UBER_EATS/DELIVEROO — souvent la
majorité des lignes d'un marchand actif). Pas d'index sur `customer_nb_orders` / `last_order_date` :
ces colonnes sont mises à jour à chaque commande clôturée, un index les empêcherait les mises à jour
« HOT » de PostgreSQL. Les trois expressions de la requête doivent rester **identiques** à celles des
index ; `customerVisibleFilter` porte un commentaire en ce sens et un test garde le prédicat.

**D10 — Clients fréquents (ancien point 12, version réduite).** `GET /customers/frequent?limit=N`
(défaut 50, max 200), même forme de réponse que la recherche. Classement : nombre de commandes, puis
dernière commande, puis dépense. Exclus : sans commande, dernière commande > 12 mois
(`FrequentCustomersRecencyMonths`), bloqués, places de marché. Un client importé sans date de dernière
commande reste éligible, classé après. Le POS le charge **une fois à la connexion** (couvre aussi la
reprise de session par jeton) et l'affiche avant toute saisie. L'« algorithme d'anticipation de commande »
est volontairement reporté : le classement actuel n'utilise que les compteurs à vie de `customer`.

**D11 — POS.** Debounce 500 → 250 ms ; la liste reste affichée pendant le chargement (barre de progression
fine au lieu d'un spinner qui efface tout) ; une réponse en vol pour une ancienne saisie est écartée ;
terme encodé dans l'URL (`+`, `&`, `#`) ; aucune requête sous le seuil de D6 (règle miroir dans
`CustomerSearchQueryHelper`, le serveur reste l'autorité) ; liste « Clients fréquents » quand la saisie
est vide ou trop courte. Échec du chargement des fréquents : silencieux, jamais bloquant pour la connexion.

**Décisions écartées.**
- *Email dans les index du POS / placeholder* : le placeholder du POS reste « Nom, téléphone, code » ;
  l'email est cherché (utile au back-office) mais classé bas.
- *Cache Redis des recherches* : résultats dépendants d'un terme libre et de compteurs qui changent à
  chaque commande clôturée ; invalidation plus coûteuse que le gain.
- *Normalisation des accents en Go* (`x/text`) : risque de divergence avec `unaccent` (ligatures, etc.).
- *Colonne générée `search_text`* : réécrit toute la table `customer` (verrou exclusif) pour un gain nul
  par rapport à des index sur expression.

## 3. Fichiers

API : `internal/modules/customers/{search.go, repository.go, service.go, handler.go}`, route
`GET /customers/frequent` (`cmd/api/routes.go`), `migrations/todo/198_customer_search.{up,down}.sql`.
Tests : `search_test.go`, `repository_test.go` (sqlmock), `search_postgres_integration_test.go`
(tag `postgres_integration`, saute tant que la migration 198 n'est pas appliquée).

POS (`wello_resto_flutter`) : `data/api/customer_api.dart`, `data/services/customer_service.dart`,
`controllers/customers/frequent_customers_controller.dart`, `helpers/customer_search_query_helper.dart`,
`ui/widgets/dialogs/customer_form/customer_list_view/customer_list_view.dart`, branchement dans
`controllers/authentication_controller.dart` et `main.dart`. Tests : `test/helpers/customer_search_query_helper_test.dart`,
`test/controllers/frequent_customers_controller_test.dart`.

## 4. Ordre de déploiement (important)

1. **Appliquer la migration 198** (hors bloc transactionnel pour les `CREATE INDEX CONCURRENTLY`, voir l'en-tête).
2. Déployer l'API. *Avant la migration, toute recherche de client échoue* (`function customer_search_norm does not exist`).
3. Publier le POS. Il fonctionne contre l'ancienne API (pas de `/customers/frequent` → liste fréquents vide,
   silencieux) et contre la nouvelle (recherche améliorée même avec l'ancien POS : le tri par pertinence
   s'applique dès que le serveur est à jour, le POS n'envoyant aucun tri).

Retour arrière : redéployer l'ancien code, puis `198_customer_search.down.sql`.

## 5. Vérifier que les index servent (à faire sur un gros marchand de production)

```sql
EXPLAIN (ANALYZE, BUFFERS)
SELECT c.customer_id FROM customer c
WHERE c.merchant_id = '<id>'
  AND (c.enabled = true OR c.blocked_at IS NOT NULL)
  AND c.customer_brand NOT IN ('UBER_EATS', 'DELIVEROO')
  AND (customer_search_name(c.customer_first_name, c.customer_last_name, c.customer_name) LIKE '%' || customer_search_norm('dupont') || '%'
       OR lower(c.customer_email) LIKE '%dupont%');
```

Attendu : `Bitmap Index Scan` sur `idx_customer_search_name` et `idx_customer_search_email` (BitmapOr).
Un `Seq Scan` sur un petit marchand est normal (le planificateur a raison). Sur un gros : vérifier d'abord
que les index sont `valid` (`SELECT indexrelid::regclass, indisvalid FROM pg_index WHERE indrelid = 'customer'::regclass`).
Les mots de 1–2 caractères (« li ») ne peuvent pas s'appuyer sur un index trigramme : parcours complet de
l'index du marchand, correct mais moins rapide — c'est une des raisons du seuil de D6.

## 6. Limites connues

- Le préfixe pays « 33 » est celui de la France ; un numéro national belge saisi avec un 0 initial
  n'est retrouvé que par ses derniers chiffres ou en international (comme avant).
- Deux clients au même nom : départagés par le nombre de commandes, pas par la proximité géographique.
- Les clients fréquents se basent sur les compteurs à vie ; un client très actif il y a 11 mois mais
  absent depuis reste classé haut jusqu'à 12 mois. L'algorithme d'anticipation prévu plus tard
  remplacera ce critère.

## 7. État d'exécution (2026-10-30) — ce qui a été lancé, et ce qui ne l'a pas été

| Vérification | Résultat |
|---|---|
| `go build ./...`, `go test ./internal/... ./cmd/...` | OK |
| Tests unitaires de la recherche (mots, téléphones, concordance `?`/arguments, tri, seuil, page hors bornes, fréquents) | OK |
| Requête générée exécutée **en lecture seule sur staging** avec les 3 fonctions remplacées par leurs équivalents inline (sans `unaccent`) | OK : syntaxe, typage, classement attendus (nom/prénom dans les deux ordres, fin de numéro, début de numéro national, international, mot exact avant préfixe, fréquents triés) |
| Migration 198 | **Non appliquée, non exécutée** (règle : l'utilisateur applique les migrations). Relue seulement. Extensions `pg_trgm`, `unaccent`, `btree_gin` confirmées *disponibles* sur staging (PostgreSQL 18.4), non installées |
| `search_postgres_integration_test.go` | **Écrit, compilé (`go vet -tags postgres_integration`), non exécuté** : il saute proprement tant que 198 n'est pas appliquée. À lancer après application (voir son en-tête) |
| Insensibilité aux accents, usage réel des index (`EXPLAIN`) | **Non vérifiés** : dépendent de la migration. Requête de contrôle en §5 |
| Flutter : `flutter test` des deux nouveaux fichiers | OK (11 tests). `flutter analyze` sur les fichiers touchés : aucun nouvel avertissement (5 avertissements existants hors périmètre) |
| Recette sur tablette (POS), parcours complet connexion → fenêtre client | **Non faite** |
| Mesures de temps de réponse avant/après | **Non faites** (staging non représentatif ; à relever avec §5 sur un gros marchand) |
