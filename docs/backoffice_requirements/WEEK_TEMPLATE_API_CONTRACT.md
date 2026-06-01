# Week Templates — API Contract

**Status**: Front-end shipped (mock end-to-end). Backend must catch up.
**Scope**: New resource `week_templates` + nested `week_template_shifts`.

⚠️ **Nommage** : la collection imbriquée s'appelle **`week_template_shifts`**.
NE PAS réutiliser `shift_templates` qui désigne déjà les modèles de SHIFT
unitaires (autre feature — cf. [SHIFT_TEMPLATES_API_CONTRACT.md](SHIFT_TEMPLATES_API_CONTRACT.md)).
Pas de collision : ce sont deux ressources distinctes.

## Resource shapes

### `WeekTemplate` — modèle de semaine (méta seule)

```jsonc
{
  "id": "wtmpl_abc123",
  "merchant_id": "...",
  "label": "Semaine type été",
  "notes": "Forte affluence",        // string | null
  "active": true,                    // suppression logique = false
  "shift_count": 12,                 // DÉRIVÉ — lecture seule, jamais en input
  "created_at": "2026-06-01T10:00:00Z",
  "updated_at": "2026-06-01T10:00:00Z"
}
```

### `WeekTemplateShift` — shift d'un modèle (gabarit, sans date)

```jsonc
{
  "id": "wts_xyz789",
  "day_of_week": 1,                  // 0..6 — 0 = dimanche (STANDARD DU REPO, JS .getDay())
  "employee_id": "emp-1",            // string | null
                                     //   string : assignation nominative (préservée par from-week)
                                     //   null   : besoin à pourvoir
  "position_id": "pos-2",            // string | null  (cohérent avec PlanningShift)
  "title": "Ouverture",              // string | null
  "start_time": "09:00",             // "HH:MM" 24h, locale établissement
  "end_time": "17:00",
  "break_minutes": 30,
  "location": "Salle",               // string | null
  "notes": "Briefing à 8h45"         // string | null
}
```

## Endpoints

Enveloppe Wello habituelle : `{ id, data: { status: "success", ... } }`.
Gating : **`manage_plannings`** (admin OR permission).

### 1. `GET /planning/week-templates`

Liste **sans** les shifts (juste `shift_count` dérivé).

```jsonc
// 200
{
  "id": "...",
  "data": {
    "status": "success",
    "week_templates": [ /* WeekTemplate[] */ ]
  }
}
```

### 2. `GET /planning/week-templates/{id}`

Détail **avec** les shifts.

```jsonc
// 200
{
  "id": "...",
  "data": {
    "status": "success",
    "week_template": { /* WeekTemplate */ },
    "week_template_shifts": [ /* WeekTemplateShift[] */ ]
  }
}
```

### 3. `POST /planning/week-templates`

Création explicite (formulaire utilisateur).

```jsonc
// Request
{
  "label": "Semaine type été",
  "notes": "Forte affluence",       // optional
  "active": true,                    // optional, default true
  "shifts": [                        // required (peut être [])
    {
      "day_of_week": 1,
      "employee_id": "emp-1",        // null = besoin
      "position_id": "pos-2",        // null OK
      "title": null,
      "start_time": "09:00",
      "end_time": "17:00",
      "break_minutes": 30,
      "location": null,
      "notes": null
    }
  ]
}

// 201
{
  "data": {
    "status": "success",
    "week_template": { /* WeekTemplate */ },
    "week_template_shifts": [ /* WeekTemplateShift[] avec id */ ]
  }
}
```

### 4. `PATCH /planning/week-templates/{id}`

Patch partiel. Si `shifts` est présent, **remplace intégralement** la
collection — pas de PATCH ligne à ligne (intentionnel, plus simple).

```jsonc
// Request — tous les champs optionnels
{
  "label": "Renommé",
  "notes": "…",
  "active": false,
  "shifts": [ /* WeekTemplateShiftInput[] complet — remplace tout */ ]
}

// 200
{
  "data": {
    "status": "success",
    "week_template": { /* WeekTemplate */ },
    "week_template_shifts": [ /* WeekTemplateShift[] */ ]
  }
}
```

### 5. `DELETE /planning/week-templates/{id}`

Suppression **LOGIQUE** : bascule `active=false`. Le record reste lisible
via `GET /{id}`. La liste retourne tous les templates (actifs et inactifs)
— le filtrage se fait côté front.

```jsonc
// 200
{ "data": { "status": "success" } }
```

### 6. `POST /planning/week-templates/from-week` ⭐

Crée un template **à partir d'une `PlanningWeek` existante**. C'est le
chemin "Enregistrer la semaine courante comme modèle".

```jsonc
// Request
{
  "week_id": "w-current",
  "label": "Modèle — semaine du 1er juin",
  "notes": null                     // optional
}

// 201
{
  "data": {
    "status": "success",
    "week_template": { /* WeekTemplate */ },
    "week_template_shifts": [ /* WeekTemplateShift[] */ ]
  }
}
```

**Règles de mapping (CONTRACT-CRITICAL)** :

| Source `PlanningShift` | → | Cible `WeekTemplateShift` |
|---|---|---|
| `shift_date: "2026-06-01"` | → | `day_of_week: 1` (lundi, via `new Date(iso).getDay()`) |
| `employee_id: "emp-1"` | → | `employee_id: "emp-1"` **(PRÉSERVÉ — pas de strip)** |
| `employee_id: null` | → | `employee_id: null` (reste un besoin) |
| `position` (label) ou `position_id` | → | `position_id` (résolution via catalogue `positions`) |
| `title, start_time, end_time, break_minutes, location, notes` | → | idem |
| `id, merchant_id, week_id, shift_date, status, created_at, updated_at` | → | (ignorés / régénérés) |

⚠️ **`employee_id` est CONSERVÉ tel quel** — c'est l'intérêt principal de
cette opération vs une création manuelle (rejouer la semaine avec les
mêmes employés assignés). Le backend ne doit PAS le mettre à `null`.

## Day-of-week convention

`0..6` avec `0 = dimanche` (JavaScript `Date.getDay()`).
Cf. [`/memories/repo/DAY_OF_WEEK_CONVENTION.md`](../../) et le service mock
[`src/services/weekTemplateService.ts → dayOfWeekFromIsoDate`](../../src/services/weekTemplateService.ts).

| dow | jour |
|---|---|
| 0 | Dimanche |
| 1 | Lundi |
| 2 | Mardi |
| 3 | Mercredi |
| 4 | Jeudi |
| 5 | Vendredi |
| 6 | Samedi |

## Schema (SQL ébauche)

```sql
CREATE TABLE week_templates (
  id            VARCHAR(64) PRIMARY KEY,
  merchant_id   VARCHAR(64) NOT NULL,
  label         VARCHAR(120) NOT NULL,
  notes         TEXT,
  active        BOOLEAN NOT NULL DEFAULT TRUE,
  created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX (merchant_id, active)
);

CREATE TABLE week_template_shifts (
  id                VARCHAR(64) PRIMARY KEY,
  week_template_id  VARCHAR(64) NOT NULL REFERENCES week_templates(id) ON DELETE CASCADE,
  day_of_week       TINYINT NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
  employee_id       VARCHAR(64) NULL,          -- nullable (besoin)
  position_id       VARCHAR(64) NULL,
  title             VARCHAR(120),
  start_time        TIME NOT NULL,
  end_time          TIME NOT NULL,
  break_minutes     INT NOT NULL DEFAULT 0,
  location          VARCHAR(120),
  notes             TEXT,
  INDEX (week_template_id, day_of_week)
);
```

`shift_count` est calculé via `COUNT(*) FROM week_template_shifts WHERE week_template_id = ?`,
ou matérialisé en compteur si la perf le demande.

## Validation server-side

- `label` : requis, non vide, ≤ 120 caractères.
- `day_of_week` : entier `0..6`.
- `start_time` / `end_time` : format `HH:MM`. Pas de contrainte d'ordre côté backend
  (le front bloque déjà ; certains gabarits "overnight" pourraient l'enfreindre
  à terme — à confirmer).
- `break_minutes` : entier ≥ 0.
- `employee_id`, `position_id` : si non null, doivent référencer un record du même `merchant_id`
  (sinon `error_invalid_data` `"Employé/poste introuvable pour ce marchand."`).

## Future endpoint — INSTANTIATE (next sprint)

Pas dans ce contrat (sera l'objet du prochain prompt) :
`POST /planning/week-templates/{id}/instantiate` avec `{ week_id }` pour
créer les `PlanningShift` à partir du modèle, sur la semaine cible — avec
les arbitrages de conflits (overlap, employés absents, etc.).

## Référence front-end

| Concern | File |
|---|---|
| Types (source de vérité) | [src/types/planning.ts](../../src/types/planning.ts) §"Week templates" |
| Service mock = futur appel API | [src/services/weekTemplateService.ts](../../src/services/weekTemplateService.ts) |
| Query keys | [src/lib/queryKeys.ts](../../src/lib/queryKeys.ts) `qk.planningWeekTemplates` |
| UI — modale CRUD | [src/components/team/planning/WeekTemplatesDialog.tsx](../../src/components/team/planning/WeekTemplatesDialog.tsx) |
| UI — éditeur 7 jours | [src/components/team/planning/WeekTemplateEditor.tsx](../../src/components/team/planning/WeekTemplateEditor.tsx) |
| Tests (spec exécutable) | [src/services/\_\_tests\_\_/weekTemplateService.test.ts](../../src/services/__tests__/weekTemplateService.test.ts) |
