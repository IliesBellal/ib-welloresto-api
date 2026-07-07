# Mini-audit — Consommateurs de `PATCH /locations/{id}/coordinates`

> **Date :** 2026-07-04 · **Complément de :** `audit-tables-plan-de-salle.md` (§3, endpoint signalé orphelin)
> **Méthode :** recherche dans les 4 repos clients de `coordinates`, `/locations`, et des appels PATCH vers des chemins contenant `location` (y compris via les couches API centralisées : `apiClient` React, `_middleware.request` Flutter). Lecture seule.

## 1. Résultats par repo

| Repo | Résultat | Détail |
|---|---|---|
| **wello-back-office** (React) | **Non consommé** | Les seules occurrences de « coordinates » sont le helper local `clampCoordinates` ([useFloorPlan.ts:50](../wello-back-office/src/hooks/useFloorPlan.ts#L50)) et des commentaires. L'éditeur de plan de salle sauvegarde les positions via `PATCH /locations/tables/{id}` ([locationsService.ts:184-196](../wello-back-office/src/services/locationsService.ts#L184-L196)), qui couvre déjà x/y parmi les autres champs. Aucun `apiClient.patch` ne vise `/coordinates` |
| **Flutter POS** (`wello_resto_flutter`) | **Non consommé** | Seul appel au module : `GET /locations` ([customer_api.dart:50-52](../wello_resto_flutter/lib/data/api/customer_api.dart#L50-L52)), en lecture pour le dialog de choix de table. Aucun PATCH vers `/locations` ; les occurrences de « coordinates » relèvent toutes du module livraison (GPS) |
| **Flutter Kiosk** (`wello-kiosk`) | **Non consommé** | Zéro occurrence de `coordinates` ou `/locations` dans `lib/` |
| **ScannOrder** (`wello-resto-scannorder`) | **Non consommé** | Zéro appel à `/locations` ; les occurrences de « coordinates » concernent le routage OSRM (livraison) |

Côté API, le handler [locations/handler.go:40-63](internal/modules/locations/handler.go#L40-L63) et la route [routes.go:897](cmd/api/routes.go#L897) existent bien, mais l'endpoint fait strictement moins que `PATCH /locations/tables/{location_id}` (x/y seulement, vs x/y + nom/étage/dimensions/angle via payload partiel à `COALESCE`).

## 2. Verdict

**A. Supprimer l'endpoint** — aucun consommateur dans les quatre clients de l'écosystème, et son unique cas d'usage (déplacer une table) est déjà couvert par le payload partiel de `PATCH /locations/tables/{id}` ; il s'agit d'un reliquat d'une première itération de l'éditeur de plan de salle.

(Si un doute subsiste sur un client hors workspace — cf. question 4 de l'audit tables — une vérification des logs d'accès production sur ce path avant suppression lève le risque à coût nul.)

## 3. Recommandation pour la phase 0

À inscrire au ticket d'assainissement :

1. Vérifier dans les logs de prod (middleware d'audit des requêtes) qu'aucun appel `PATCH /locations/*/coordinates` n'a eu lieu sur les 30 derniers jours.
2. Supprimer la route ([routes.go:897](cmd/api/routes.go#L897)), le handler `UpdateLocationCoordinates`, la méthode service/repository associée et le DTO `models.UpdateLocationCoordinatesRequest` ([request_objects.go:200-203](internal/models/request_objects.go#L200-L203)).
3. Non-régression : l'éditeur back-office (seul écrivain de la géométrie) passe par `PATCH /locations/tables/{id}` — vérifier qu'un déplacement de table s'enregistre toujours après suppression.
