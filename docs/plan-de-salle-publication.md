# Plan de salle — publication par lot (brouillon → publier)

L'éditeur du back-office ne modifie plus le plan route par route. Il travaille
sur un **brouillon local** (créations, modifications, suppressions, avec des
identifiants temporaires), puis **publie** le différentiel en une requête.
L'API l'applique dans **une seule transaction** : tout est enregistré, ou rien.

Les routes unitaires (`POST /locations/floors/{id}/tables`,
`PATCH /locations/tables/{id}`, `/floors/{id}/obstacles`, …) existent toujours
et gardent leur comportement ; l'éditeur ne s'en sert plus.

## Lecture : `GET /locations`

Inchangée, plus un champ `revision` (entier, `0` si l'établissement n'a jamais
publié par lot). L'éditeur le mémorise au chargement et le renvoie à la
publication. Les clients qui l'ignorent (POS) ne sont pas affectés. La lecture
reste fonctionnelle si la migration 202 n'est pas encore appliquée (`revision`
vaut alors `0`).

## Publication : `POST /locations/plan/publish`

Permission `seating_plan.manage`. Corps :

```json
{
  "base_revision": 3,
  "floors":    { "create": [{ "temp_id": "tmp_f1", "name": "Terrasse" }],
                 "update": [{ "id": "12", "name": "Salle" }],
                 "delete": ["13"] },
  "tables":    { "create": [{ "temp_id": "tmp_t1", "floor_id": "tmp_f1",
                              "location_name": "Table 1", "seats": 2, "shape": "circle",
                              "x": 100, "y": 100, "width": 80, "height": 80, "angle": 0,
                              "attributes": { "pmr": false, "terrace": false, "vip": false, "window": false } }],
                 "update": [{ "id": "40", "location_name": "Table 2", "seats": 4, "x": 200 }],
                 "delete": ["41"] },
  "obstacles": { "create": [{ "temp_id": "tmp_o1", "floor_id": "12", "type": "plant",
                              "x": 50, "y": 50, "width": 40, "height": 40, "angle": 0 }],
                 "update": [{ "id": "obs-1", "type": "wall", "width": 900 }],
                 "delete": ["obs-2"] },
  "areas":     { "create": [], "update": [], "delete": [] }
}
```

Réponse (`data`) :

```json
{ "status": "1", "revision": 4, "id_map": { "tmp_f1": "57", "tmp_t1": "301", "tmp_o1": "obs-ab12…" } }
```

### Règles

- **Identifiants temporaires** : préfixe `tmp_`, uniques sur tout le lot. Ils
  peuvent être utilisés comme `floor_id` par une table, un obstacle ou une zone
  du même lot (y compris pour **déplacer** une table, `floor_id` d'une mise à
  jour). `id_map` associe chacun à l'identifiant réel généré.
- **Cibles de mise à jour / suppression** : identifiants réels uniquement. Une
  entité ne peut pas être à la fois modifiée et supprimée, ni supprimée deux
  fois. Une table ciblée inconnue (ou d'un autre établissement) fait échouer le
  lot entier (`404`).
- **Mises à jour = entité complète** pour les obstacles (au minimum `type`, dont
  dépend la validation de `direction`).
- **Ordre d'exécution** : verrou et contrôle de révision → étages (créés, puis
  renommés) → tables, obstacles, zones (créés, puis modifiés) → suppressions
  (tables, obstacles, zones, **étages en dernier**) → incrément de la révision.
- **Supprimer un étage** : ses obstacles et zones restants sont désactivés avec
  lui. S'il contient encore des tables actives après le lot : `409
  floor_not_empty` (garde-fou conservé, rien n'est appliqué).
- **Bornes** : celles des routes unitaires (table `x/y 0-1000`, `width/height
  40-300`, `angle 0-359`, `seats ≥ 1` ; obstacle `width/height 10-1000`), plus
  `floors.name` ≤ 50 caractères et `locations.location_name` ≤ 20 (tailles des
  colonnes). 2 000 opérations maximum par lot.

### Concurrence

`base_revision` doit être égale à la révision courante, sinon `409
plan_revision_conflict` et rien n'est appliqué. Le verrou posé sur la ligne de
révision sérialise deux publications simultanées : la seconde voit la révision
incrémentée par la première et reçoit le conflit. Seules les publications par
lot incrémentent la révision (pas les routes unitaires).

### Erreurs

| HTTP | `data.status`             | Sens                                                       |
|------|---------------------------|------------------------------------------------------------|
| 409  | `plan_revision_conflict`  | Plan publié par quelqu'un d'autre : recharger              |
| 409  | `floor_not_empty`         | Étage supprimé alors qu'il garde des tables                |
| 404  | `floor_not_found` / `not_found` | Étage ou entité ciblé inconnu                        |
| 422  | `invalid_plan_payload`    | Temp id mal formé / dupliqué / non résolu, nom vide ou trop long, cible à la fois modifiée et supprimée |
| 422  | `invalid_table_geometry` / `invalid_obstacle_geometry` / `invalid_area_geometry` | Valeurs hors bornes |

## Obstacles : catalogue

`wall`, `door`, `window`, `stairs`, `plant`, `bar`, `pillar`. `direction` n'est
accepté que pour `door`. Les coordonnées d'un obstacle désignent son **centre**
(celles d'une table, son coin haut-gauche) ; l'angle est en degrés.

## Déploiement

Migrations **avant** le code (appliquées à la main, rejouables) :

1. `202_floor_plan_revisions` — table `floor_plan_revisions`.
2. `203_floor_obstacle_types` — `floor_obstacles.type` passe d'un enum natif
   (`floor_obstacles_type_enum`) à `varchar(32)` + contrainte `CHECK` à sept
   types. Sans elle, l'insertion d'une fenêtre, d'une plante ou d'un pilier
   échoue.

Tests : `go test ./internal/modules/locations/` (validation, contrat HTTP) et
`go test -tags postgres_integration ./internal/modules/locations/` avec
`POSTGRES_URL` (transaction, rollback, conflit, concurrence) — migrations 202 et
203 appliquées.
