# 41 — Cartographie du blocage `enum` sur `planning_shifts` et balayage proactif des 13 colonnes ENUM

Date: 2026-07-21
Branche: migration/postgres

## Objectif

Ce document ne corrige rien et n'arbitre aucune conversion — il cartographie le nouveau blocage
rencontré au rapport [40](40-parent-order-id-and-full-integer-scan.md) §4
(`101_planning_shifts.sql`, `invalid input value for enum planning_shifts_status_enum: ""`) avant toute
décision, et étend le même balayage proactif que celui fait pour les colonnes entières (rapport 40) à
l'ensemble des colonnes `ENUM` du schéma cible. **Aucun fichier n'a été modifié. Aucune donnée réelle
n'est citée** — uniquement noms de tables/colonnes, comptages, et les libellés ENUM eux-mêmes (déjà
publics dans le schéma versionné, pas des données applicatives).

## 1. La colonne concernée

[04-schema-postgres-target.sql](04-schema-postgres-target.sql), table `planning_shifts` :

```sql
CREATE TYPE planning_shifts_status_enum AS ENUM ('planned', 'confirmed', 'done', 'cancelled');
...
CREATE TABLE planning_shifts (
    ...
    status planning_shifts_status_enum NOT NULL DEFAULT 'planned',
    ...
);
```

- **Colonne** : `planning_shifts.status`
- **Type ENUM déclaré** : `planning_shifts_status_enum`
- **Valeurs autorisées (4)** : `'planned'`, `'confirmed'`, `'done'`, `'cancelled'`
- **Nullabilité** : `NOT NULL` (avec `DEFAULT 'planned'`, qui ne s'applique qu'à un `INSERT` sans
  valeur explicite pour la colonne — sans effet ici puisque le générateur SQL insère toujours une
  valeur explicite par colonne, jamais un `DEFAULT` implicite)

## 2. Comparaison au type MySQL source

[wello-resto-mysql-ddl.md](wello-resto-mysql-ddl.md), ligne 2360 :

```sql
`status` enum('planned','confirmed','done','cancelled') NOT NULL DEFAULT 'planned',
```

**C'est un vrai `ENUM` MySQL**, avec exactement les 4 mêmes libellés que la cible Postgres (traduction
1:1, aucun écart de nom). La chaîne vide `''` **n'est pas** l'une des 4 valeurs déclarées — ce n'est
donc pas un cas où MySQL autoriserait explicitement `''` comme option légale de cet `ENUM` précis.

Ceci dit, la question posée (« MySQL permet une chaîne vide `''` comme valeur `ENUM` distincte ») pointe
vers le bon mécanisme, à une nuance près : MySQL ne déclare jamais `''` comme membre nommé d'un `ENUM`
— mais son moteur de stockage réserve en interne l'**index 0** de tout type `ENUM` à une valeur
« erreur », rendue comme la chaîne vide `''`, distincte des libellés déclarés (qui commencent à
l'index 1). Cette pseudo-valeur `''` est produite quand une insertion tente d'écrire une valeur qui ne
correspond à **aucun** des libellés déclarés (chaîne vide envoyée par l'appelant, valeur mal
orthographiée, tronquée, etc.) :

- **En mode SQL strict** (`STRICT_TRANS_TABLES`/`STRICT_ALL_TABLES`), MySQL **rejette** l'insertion
  avec une erreur.
- **En mode non strict**, MySQL **accepte silencieusement** l'insertion et stocke `''` (index 0) à la
  place — sans avertissement bloquant, sans que la valeur d'origine soit conservée.

Deux éléments de preuve indirecte, cohérents avec un mode non strict au moment de l'écriture de ces
lignes (déjà établi pour une classe de bug voisine — le sentinel de date `0000-00-00` — au rapport
[37](37-zero-date-sentinel-audit.md) §2) :

- L'en-tête du dump réel (`data-migration/migration_welloresto_data.sql`, ligne 10) ne positionne que
  `SET SQL_MODE = "NO_AUTO_VALUE_ON_ZERO";` — sans `STRICT_TRANS_TABLES` ni `STRICT_ALL_TABLES`.
- Le code Go applicatif (§4) ne laisse **aucun chemin** connu qui écrirait explicitly une chaîne vide
  sur cette colonne — la valeur `''` observée n'est donc vraisemblablement pas issue de l'API actuelle,
  mais d'une écriture antérieure (import, script, ancienne version du code) tombée sur ce comportement
  non strict de MySQL.

**Conclusion §2** : ce n'est pas un cas où `''` serait un libellé `ENUM` légitime des deux côtés — c'est
la même classe structurelle que le sentinel de date du rapport 37 (artefact du mode non strict MySQL à
l'écriture), appliquée cette fois à un type `ENUM` plutôt qu'à une colonne date.

## 3. Volume sur le dump réel

Scan direct de `planning_shifts.status` sur le dump réel (`iter_dump_rows`, même tokenizer que le
générateur) :

| Lignes totales | `NULL` | Correspond à un libellé déclaré | Chaîne vide `''` | Autre valeur non conforme |
|---:|---:|---:|---:|---:|
| 54 | 0 | 47 | **7** | 0 |

**7 lignes sur 54** (13 %) portent la chaîne vide sur `status`. Aucune ligne ne porte une valeur qui ne
soit ni un des 4 libellés déclarés ni la chaîne vide — le blocage rencontré au rapport 40 est donc bien
le seul cas de figure présent dans cette colonne, pas la partie visible d'un problème plus large sur la
même colonne.

## 4. Valeur par défaut applicative côté Go

[internal/modules/planning/schedule/service.go:299](../../internal/modules/planning/schedule/service.go#L299),
dans le chemin de création d'un shift :

```go
status := "planned"
if req.Status != nil && strings.TrimSpace(*req.Status) != "" {
    status = strings.ToLower(strings.TrimSpace(*req.Status))
}
if !sharedpkg.IsValidPlanningShiftStatus(status) {
    return nil, models.ErrValidationError
}
```

Et au chemin de mise à jour
([service.go:401-406](../../internal/modules/planning/schedule/service.go#L401-L406)), une valeur
`status` vide ou absente dans la requête est **ignorée** (ne touche pas `current.Status`) plutôt que
d'écraser la valeur existante par une chaîne vide.

**`"planned"` est donc un candidat de conversion naturel et sans ambiguïté** : c'est exactement la
valeur que l'application elle-même pose par défaut quand aucun statut n'est fourni à la création — la
sémantique métier d'un shift « sans statut renseigné » est déjà, côté Go, équivalente à « planifié ».
Le code applicatif actuel ne peut produire `''` sur cette colonne dans aucun des deux chemins observés
(création, mise à jour), ce qui renforce l'hypothèse du §2 : les 7 lignes concernées sont un résidu
antérieur au code actuel, pas un cas que l'application continue de produire.

**Remarque annexe (hors périmètre, non bloquante, à noter pour une prochaine session)** :
`sharedpkg.IsValidPlanningShiftStatus`
([internal/modules/planning/shared/helpers.go:127-134](../../internal/modules/planning/shared/helpers.go#L127-L134))
accepte aussi la valeur `"draft"` en plus des 4 libellés déclarés dans le schéma (MySQL et Postgres) —
un écart entre la validation Go et l'`ENUM` déclaré des deux côtés. Le balayage du §5 confirme
qu'aucune ligne réelle de `planning_shifts.status` ne porte actuellement `"draft"` (0 valeur non
conforme en dehors des 7 vides) : l'écart est latent, pas encore manifesté dans les données. Signalé
ici pour mémoire, sans arbitrage ni correction dans ce document.

## 5. Balayage proactif des 13 colonnes ENUM du schéma cible

Même méthode que le rapport 40 pour les colonnes entières : les 13 types `CREATE TYPE ... AS ENUM`
déclarés en tête de [04-schema-postgres-target.sql](04-schema-postgres-target.sql) (lignes 37-49),
chacun rattaché à sa colonne unique d'utilisation, scannés sur le dump réel pour repérer toute valeur
non-NULL qui ne correspond à aucun des libellés déclarés (chaîne vide incluse).

| Table.Colonne | Libellés déclarés | Nullable | Lignes (dump) | `NULL` | Vides / non conformes |
|---|---|---|---:|---:|---:|
| `booking_waitlist.status` | waiting, notified, seated, expired, cancelled | NOT NULL | 0 | 0 | 0 |
| `cleaning_surfaces.frequency_unit` | day, week, month | NOT NULL | 5 | 0 | 0 |
| `employees.role` | employee, manager, admin | NOT NULL | 3 | 0 | 0 |
| `floor_obstacles.type` | wall, bar, stairs, door | NOT NULL | 0 | 0 | 0 |
| `hours_amendments.type` | permanent, temporary | NOT NULL | 0 | 0 | 0 |
| `kiosks.status` | pending, active, inactive, revoked | NOT NULL | 18 | 0 | 0 |
| `planning_leave_requests.leave_type` | paid, unpaid, sick, other | NOT NULL | 9 | 0 | 0 |
| `planning_leave_requests.status` | pending, approved, rejected, cancelled | NOT NULL | 9 | 0 | 0 |
| **`planning_shifts.status`** | planned, confirmed, done, cancelled | NOT NULL | 54 | 0 | **7 (vides)** |
| `planning_shift_swap_requests.status` | pending, approved, rejected, cancelled | NOT NULL | 6 | 0 | 0 |
| `planning_weeks.status` | draft, published, locked | NOT NULL | 25 | 0 | 0 |
| `temperature_readings.status` | ok, alert, critical | NOT NULL | 21 | 0 | 0 |
| `upsell_suggestions.channel` | POS, SNO, KIOSK | NOT NULL | 207 | 0 | 0 |

**Les 13 colonnes sont bien `NOT NULL`** (aucune ne tolère `NULL` en base, cohérent avec le mode
`ENUM` MySQL source sur ces 13 colonnes, aucune d'entre elles n'étant `DEFAULT NULL` côté source).

**Résultat du balayage : une seule colonne concernée sur les 13, `planning_shifts.status`, avec les 7
lignes déjà identifiées au §3.** Les 12 autres colonnes `ENUM` sont **100 % conformes** à leurs
libellés déclarés sur l'intégralité des lignes présentes dans le dump réel — aucune chaîne vide,
aucune valeur mal casée, aucune valeur hors liste. `booking_waitlist`, `floor_obstacles` et
`hours_amendments` n'ont actuellement aucune ligne dans le dump (0 lignes chacune, cohérent avec les
comptages du rapport [40](40-parent-order-id-and-full-integer-scan.md) — colonnes vérifiées par
construction dès leur premier chargement de données, pas de risque résiduel caché par une absence de
données à ce jour).

## 6. Synthèse

| Question | Réponse |
|---|---|
| Colonne bloquante | `planning_shifts.status`, type `planning_shifts_status_enum`, `NOT NULL DEFAULT 'planned'` |
| Nature du blocage | Chaîne vide `''` — pseudo-valeur d'erreur MySQL (index 0 implicite d'un `ENUM`), pas un libellé déclaré ni côté MySQL ni côté Postgres |
| Volume | 7 lignes sur 54 (13 %) — 0 autre valeur non conforme sur cette colonne |
| Origine probable | Écriture antérieure en mode SQL non strict (cohérent avec le rapport 37) — le code Go actuel ne peut pas produire `''` sur cette colonne (ni à la création, ni à la mise à jour) |
| Candidat de conversion naturel | `'planned'` — valeur posée par défaut par le code Go lui-même quand le statut n'est pas renseigné à la création ([service.go:299](../../internal/modules/planning/schedule/service.go#L299)) |
| Écart annexe relevé (non bloquant) | `IsValidPlanningShiftStatus` (Go) accepte aussi `"draft"`, absent des 2 `ENUM` déclarés — latent, 0 ligne réelle concernée |
| Balayage des 13 colonnes ENUM | 1 seule à risque (`planning_shifts.status`, 7 lignes) ; les 12 autres 100 % conformes sur l'ensemble du dump réel |

**Aucune décision de conversion n'a été prise, aucun fichier n'a été modifié** (ni le schéma, ni le
générateur, ni le code Go) — ce document est une cartographie destinée à préparer l'arbitrage d'une
prochaine session, sur le modèle des rapports 37 (sentinel de date) et
[40](40-parent-order-id-and-full-integer-scan.md) (`parent_order_id`).
