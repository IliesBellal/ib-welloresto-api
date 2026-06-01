# API Contract — Planning Performance

> **Status :** specification (le mock côté front est la **seule** source de vérité actuelle).
> **Source mock :** `src/services/performanceService.ts` — `PerformanceService.getForRange(query)`.
> **Types :** `src/types/performance.ts`.
> Le jour où le backend implémente cet endpoint, **seul le corps de `getForRange`** doit changer (passer de calcul local à un `fetch`). La signature et la forme JSON ne changent pas.

---

## Endpoint

```
GET /planning/performance
```

### Query params

| Param         | Type                                  | Requis | Description                                                                 |
| ------------- | ------------------------------------- | ------ | --------------------------------------------------------------------------- |
| `from`        | `string` (ISO `YYYY-MM-DD`)           | ✅     | Début de la fenêtre, inclusive.                                             |
| `to`          | `string` (ISO `YYYY-MM-DD`)           | ✅     | Fin de la fenêtre, inclusive.                                               |
| `granularity` | `"day" \| "week" \| "month"`          | ✅     | Granularité d'agrégation des lignes (`periods[]`).                          |
| `compare`     | `"previous"` (optional)               | ❌     | Si présent, renseigne aussi `previous_period` pour la période juste avant. |

### Réponse — enveloppe Wello

```json
{ "status": "success", "data": { "performance": <PerformanceResponse> } }
```

---

## `PerformanceResponse`

| Champ             | Type                                  | Null ?    | Description                                                 |
| ----------------- | ------------------------------------- | --------- | ----------------------------------------------------------- |
| `from`            | `string` `YYYY-MM-DD`                 | non       | Echo du paramètre.                                          |
| `to`              | `string` `YYYY-MM-DD`                 | non       | Echo du paramètre.                                          |
| `granularity`     | `"day" \| "week" \| "month"`          | non       | Echo du paramètre.                                          |
| `periods`         | `PerformancePeriod[]`                 | non (`[]`) | Une ligne par bucket de granularité ; clampé à `[from,to]`.|
| `totals`          | `PerformancePeriod` (`label="Total"`) | non       | Agrégat sur l'ensemble de la fenêtre.                       |
| `previous_period` | `{from,to,periods,totals}` ou `null`  | oui       | Présent **seulement** si `compare=previous`.                |
| `warnings.members_without_rate` | `number` (≥ 0)             | non       | Membres planifiés sans `hourly_rate` exclus du calcul MS.   |

## `PerformancePeriod`

| Champ                       | Type             | Null ?  | Convention                                                                 |
| --------------------------- | ---------------- | ------- | -------------------------------------------------------------------------- |
| `period_start`              | `string` ISO     | non     | Inclusif.                                                                  |
| `period_end`                | `string` ISO     | non     | Inclusif.                                                                  |
| `label`                     | `string`         | non     | Libellé lisible (`"lun. 25 mai"`, `"sem. 22"`, `"mai 2026"`, `"Total"`).   |
| `revenue_actual_cents`      | `integer` (cts)  | **oui** | CA HT réalisé. `null` tant que la source CA n'est pas branchée.            |
| `revenue_forecast_cents`    | `integer` (cts)  | **oui** | CA HT prévisionnel (réservé, `null` par défaut).                           |
| `planned_hours`             | `number` (h, décimal) | non | Heures planifiées (`shifts` – pauses).                                     |
| `worked_hours`              | `number` (h, décimal) | non | Heures pointées (fallback = `planned_hours` quand pas de pointages).       |
| `headcount`                 | `integer` (≥ 0)  | non     | Effectifs distincts planifiés sur la période.                              |
| `payroll_cost_loaded_cents` | `integer` (cts)  | non     | Masse salariale chargée = `Σ heures × hourly_rate × (1 + charges)`.        |
| `payroll_ratio`             | `float` `[0..1]` | **oui** | `payroll_cost_loaded_cents / revenue_actual_cents` ; `null` si CA `null`/`0`. |
| `revenue_per_hour_cents`    | `integer` (cts)  | **oui** | `revenue_actual_cents / worked_hours` ; `null` si CA `null` ou heures `0`. |
| `hours_delta`               | `number` (h, décimal) | non | `worked_hours − planned_hours` (peut être négatif).                        |

---

## Conventions transverses

- **Monnaie :** **entiers en centimes** (`*_cents`) — jamais de `float` côté € ; pas de division côté front.
- **Ratios :** `float` (ex. `0.32` = 32 %), jamais en pourcentage entier.
- **Heures :** `number` décimal en heures (ex. `7.5`). Pas de `HH:MM`.
- **Dates :** ISO `YYYY-MM-DD`, sans timezone (les bornes sont locales établissement).
- **`null` est une donnée :** signifie « valeur **non disponible** » (source absente, ou division par 0). Le front affiche `—` + tooltip.
- **Clamping :** les buckets `week`/`month` qui débordent de `[from, to]` sont coupés ; `period_start` / `period_end` ne sortent **jamais** de la fenêtre demandée.
- **Comparaison `compare=previous` :**
  - `granularity=day`  → `[from − N, to − N]` avec `N = (to − from + 1)` jours.
  - `granularity=week` → idem (fenêtre décalée de N jours).
  - `granularity=month` → **mois calendaire précédent** (`startOfMonth(from − 1m)` → `endOfMonth(from − 1m)`).

---

## Exemple

```http
GET /planning/performance?from=2026-05-25&to=2026-05-31&granularity=day&compare=previous
```

```json
{
  "status": "success",
  "data": {
    "performance": {
      "from": "2026-05-25",
      "to": "2026-05-31",
      "granularity": "day",
      "periods": [
        {
          "period_start": "2026-05-25",
          "period_end": "2026-05-25",
          "label": "lun. 25 mai",
          "revenue_actual_cents": null,
          "revenue_forecast_cents": null,
          "planned_hours": 32.5,
          "worked_hours": 32.5,
          "headcount": 5,
          "payroll_cost_loaded_cents": 69225,
          "payroll_ratio": null,
          "revenue_per_hour_cents": null,
          "hours_delta": 0
        }
      ],
      "totals": {
        "period_start": "2026-05-25",
        "period_end": "2026-05-31",
        "label": "Total",
        "revenue_actual_cents": null,
        "revenue_forecast_cents": null,
        "planned_hours": 218,
        "worked_hours": 218,
        "headcount": 7,
        "payroll_cost_loaded_cents": 464575,
        "payroll_ratio": null,
        "revenue_per_hour_cents": null,
        "hours_delta": 0
      },
      "previous_period": {
        "from": "2026-05-18",
        "to": "2026-05-24",
        "periods": [],
        "totals": { /* idem PerformancePeriod */ }
      },
      "warnings": { "members_without_rate": 1 }
    }
  }
}
```

---

## TODO côté backend

1. **Brancher la source CA** (`revenue_actual_cents`, `revenue_forecast_cents`) — actuellement `null` côté mock.
2. **Brancher les pointages globaux** sur la fenêtre (l'API actuelle est par-employé) ; sinon garder le fallback `worked_hours = planned_hours`.
3. **Charges patronales par défaut :** si `employer_charges_pct` est `null` sur l'employé, le mock prend `0`. À confirmer (taux d'établissement ?).
4. **Exclure les pointages encore ouverts** (`clock_out_at = null`) du calcul `worked_hours`.

---

## Gating

Lecture/affichage : `manage_plannings`. Le même droit gouverne déjà l'écran Planning : pas de gating supplémentaire à brancher côté front (la feuille n'est accessible que depuis l'écran Planning).
