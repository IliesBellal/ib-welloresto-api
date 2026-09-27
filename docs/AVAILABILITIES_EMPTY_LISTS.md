# Disponibilités : vider les produits / les créneaux (2026-09-27)

## Problème constaté

Dans le back-office, retirer **le dernier produit** d'une disponibilité (ou
son dernier créneau) semblait fonctionner — toast « Disponibilité mise à
jour », réponse 200 — mais rien n'était modifié en base : au rechargement,
les produits étaient toujours là et toujours masqués sur la borne et
ScanNOrder hors créneau.

Reproduit sur staging en rejouant les PATCH exacts du back-office via le
handler (merchant de test isolé, nettoyé ensuite) :

| Action back-office | Payload | Réponse | En base (avant correctif) |
|---|---|---|---|
| Retirer 1 produit sur 3 | `product_ids: ["1001","1002"]` | 200 | ✅ remplacé |
| Retirer tous les produits | `product_ids: null` | 200 | ❌ inchangé |
| Retirer tous les créneaux | `schedules` absent | 200 | ❌ inchangé |

Cause, des deux côtés :

- **API** — `AvailabilitiesRepository.Update` ne remplaçait les listes que si
  `len(req.ProductIDs) > 0` / `len(req.Schedules) > 0` : liste vide et `null`
  valaient « ne pas modifier ». La création refusait en plus une disponibilité
  sans produit ou sans créneau.
- **Back-office** — `transformAvailabilityForAPI` envoyait `product_ids: null`
  pour une liste vide et omettait `schedules` ; le formulaire exigeait au moins
  un créneau.

## Règle métier (décision produit, 2026-09-27)

- Une disponibilité **sans produit** est autorisée : elle ne restreint aucun
  produit (sans effet sur la borne et ScanNOrder).
- Une disponibilité active **sans créneau** est autorisée : ses produits sont
  **masqués en permanence** sur la borne et ScanNOrder (règle « liste
  blanche » inchangée : un produit rattaché à une disponibilité active n'est
  visible que pendant un de ses créneaux — sans créneau, jamais).
- Dans les deux cas, le back-office affiche un avertissement qui décrit le
  risque et demande confirmation avant d'enregistrer.

## Contrat API — `PATCH /menu/availabilities/{id}`

Pour `product_ids` et `schedules` :

| Payload | Effet |
|---|---|
| clé absente | inchangé |
| `null` | liste vidée |
| `[]` | liste vidée |
| `[...]` | liste remplacée |

`POST /menu/availabilities` accepte `product_ids` et `schedules` vides ou
absents. Le nom reste obligatoire ; chaque créneau fourni est toujours validé
(`validateSchedules`).

## Décisions

1. **`null` explicite = vider (et pas seulement `[]`).** Le back-office déjà
   déployé envoie `product_ids: null` pour une liste vide : interpréter
   `null` comme « vider » corrige le retrait du dernier produit dès le
   déploiement de l'API, sans attendre celui du back-office. Même approche que
   `discounts.UpdateDiscountRequest` (`valid_to: null` explicite =
   `ClearValidTo`). Une clé absente reste « ne pas modifier » : le toggle
   actif/inactif du back-office n'envoie que `available` et ne doit rien vider.
   Implémenté par `UpdateAvailabilityRequest.UnmarshalJSON` : une clé présente
   à `null` devient une liste vide non nil ; le repository teste `!= nil`.
2. **Le back-office envoie `[]`** plutôt que `null` (intention explicite), et
   `schedules: []` quand tous les créneaux ont été retirés du formulaire.
3. **Pas de refus côté API** des listes vides : la règle est « autorisé avec
   avertissement », l'avertissement est une affaire d'interface. L'API reste
   le garde-fou technique (format des créneaux), pas le garde-fou métier.
4. **Confirmation au moment d'enregistrer** (et pas seulement un bandeau) :
   le cas « sans créneau » masque des produits en permanence, un bandeau seul
   se rate facilement. Le bandeau reste visible dans le formulaire pour
   expliquer l'état avant même de cliquer.
5. **Toggle actif/inactif d'une disponibilité déjà sans créneau** : pas de
   confirmation supplémentaire (hors périmètre) ; la carte de la disponibilité
   affiche un badge d'avertissement pour que l'état reste visible.

## Back-office

- Encart explicatif en tête des onglets Promotions et Disponibilités
  (à quoi sert chaque outil, et la différence : une promotion change le prix,
  elle ne masque jamais un produit ; une disponibilité masque un produit hors
  de ses créneaux sur la borne et ScanNOrder, pas sur la caisse).
- Formulaire disponibilité : bandeau d'avertissement si aucun produit / aucun
  créneau, et confirmation avant enregistrement qui décrit ce qui va se passer.

## Fichiers modifiés

API (`ib-welloresto-api`) :
- `internal/modules/availabilities/models.go` — `UpdateAvailabilityRequest.UnmarshalJSON`
- `internal/modules/availabilities/service.go` — création sans produit/créneau
  autorisée ; mise à jour : tests `!= nil`, créneaux fournis toujours validés
- `internal/modules/availabilities/repository.go` — `Update` remplace dès que
  la liste est non nil
- `internal/modules/availabilities/update_request_test.go` (nouveau),
  `postgres_integration_test.go` (cas « vider » + création vide)
- `internal/modules/availabilities/README.md`

Back-office (`wello-back-office`) — voir
`docs/promotions-disponibilites-listes-vides.md` dans ce dépôt :
- `src/services/promotionsService.ts` — `transformAvailabilityForAPI`
- `src/pages/PromotionsAvailabilities.tsx` — encarts, avertissements,
  confirmation, badges

Aucun changement côté borne / ScanNOrder : la règle d'évaluation
(`UnavailableProductsAt`) est inchangée, une disponibilité sans produit ne
produit simplement aucune ligne dans `GetActiveProductSchedules`.

## Statut d'exécution (2026-09-27)

- `go build ./...`, `go vet` du module : OK.
- `go test ./internal/modules/availabilities/...` : OK (dont les nouveaux
  tests de décodage).
- Tests d'intégration `-tags postgres_integration` du module **contre la base
  staging** (merchants de test `itest-avail-*`, nettoyés en fin de test) : OK,
  y compris le nouveau cas « vider les produits et créneaux ».
- Back-office : `npm run build` OK ; `tsc` sans erreur sur les fichiers
  modifiés (le projet a des erreurs `tsc` préexistantes ailleurs). **Pas testé
  visuellement dans un navigateur.**
- Rien n'est déployé ni commité.

## Ordre de déploiement

Indifférent, mais l'API d'abord est préférable : grâce à la décision 1, l'API
seule corrige déjà le retrait du dernier produit avec le back-office actuel ;
le retrait du dernier créneau et les avertissements nécessitent le nouveau
back-office. Un nouveau back-office face à l'ancienne API : `[]` ignoré au
PATCH comme avant, et une création sans produit ou sans créneau refusée
(400) — donc API d'abord.
