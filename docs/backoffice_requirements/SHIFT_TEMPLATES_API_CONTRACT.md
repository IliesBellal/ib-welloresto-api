# API Contract — Planning Shift Templates

> **Status :** specification (le mock côté front est la **seule** source de vérité actuelle).
> **Source mock :** `src/services/shiftTemplateService.ts` (`ShiftTemplateService.{list, create, update, remove}`).
> **Types :** `src/types/shiftTemplate.ts`.
> Le jour où le backend implémente ces endpoints, **seul le corps de chaque méthode** côté front change (passe du store en mémoire à `apiClient.*`). La signature, la forme JSON et les composants appelants ne bougent pas.

---

## Endpoints

Tous les payloads sont enveloppés dans le wrapper Wello : `{ id, data: { status: "success", ... } }`.

| Méthode  | URL                                       | Body                          | Réponse `data.*`           |
| -------- | ----------------------------------------- | ----------------------------- | -------------------------- |
| `GET`    | `/planning/shift-templates`               | —                             | `shift_templates: ShiftTemplate[]` |
| `POST`   | `/planning/shift-templates`               | `ShiftTemplateCreateRequest`  | `shift_template: ShiftTemplate`    |
| `PATCH`  | `/planning/shift-templates/{id}`          | `ShiftTemplateUpdateRequest`  | `shift_template: ShiftTemplate`    |
| `DELETE` | `/planning/shift-templates/{id}`          | —                             | suppression **logique** (`active=false`) |

Gating : `manage_plannings` (lecture + écriture).

---

## `ShiftTemplate`

| Champ           | Type                  | Null ? | Convention / notes                                                                                |
| --------------- | --------------------- | ------ | ------------------------------------------------------------------------------------------------- |
| `id`            | `string`              | non    | Id opaque généré par le backend.                                                                  |
| `label`         | `string`              | non    | Libellé affiché ("Service midi", "Coupure"…). Doit être unique côté UX (pas une contrainte DB).   |
| `start_time`    | `string` `"HH:MM"`    | non    | Heure de début, 24h, fuseau **locale établissement** (pas d'ISO, pas de TZ).                      |
| `end_time`      | `string` `"HH:MM"`    | non    | Heure de fin. Doit être `> start_time` (le front rejette sinon).                                  |
| `break_minutes` | `integer` (≥ 0)       | non    | Pause en minutes.                                                                                 |
| `position_id`   | `string \| null`      | **oui**| `id` d'`EmployeePosition`. `null` = "toutes positions" (le template laisse le poste vide).        |
| `color`         | `string` `"#RRGGBB"`  | non    | Hex 6 caractères. Sert uniquement à l'identification visuelle du template ; n'est PAS copié sur le shift créé. |
| `sort_order`    | `integer`             | non    | Croissant ; ordonne l'affichage dans la modale de gestion et dans le sélecteur du sidesheet.       |
| `active`        | `boolean`             | non    | `false` = suppression logique. Les templates inactifs n'apparaissent pas dans le sélecteur de shift mais restent visibles dans la modale de gestion. |
| `created_at`    | `string` ISO-8601 UTC | non    | Renseigné par le backend.                                                                         |
| `updated_at`    | `string` ISO-8601 UTC | non    | Renseigné par le backend.                                                                         |

---

## `ShiftTemplateCreateRequest`

```ts
{
  label: string;
  start_time: string;           // "HH:MM"
  end_time: string;             // "HH:MM"
  break_minutes: number;        // >= 0
  position_id: string | null;
  color: string;                // "#RRGGBB"
  sort_order?: number;          // défaut backend : max(existing.sort_order) + 1
  active?: boolean;             // défaut : true
}
```

## `ShiftTemplateUpdateRequest`

Patch partiel — n'importe quel sous-ensemble des champs ci-dessus (tous optionnels). `id` n'est jamais dans le body. `sort_order` peut être réécrit pour réordonner (le front fait un swap 2-patches, pas de batch endpoint).

---

## Conventions transverses

- **Suppression = `active=false`** (logique). Le backend conserve la ligne pour l'historique des shifts qui auraient pu y faire référence (aucune FK n'est posée aujourd'hui — le template n'est utilisé que comme **pré-remplissage** côté UI).
- **Pas de FK posée sur les shifts.** Un template appliqué pré-remplit le formulaire, mais le shift créé est totalement détaché — modifier ou supprimer un template **n'a aucun effet** sur les shifts existants. C'est volontaire (le template est un raccourci, pas une référence vivante).
- **`position_id` nullable** = "toutes positions" — sémantiquement "ce créneau s'applique à n'importe quel poste".
- **`color`** est de la métadonnée d'UI uniquement (le `PlanningShift` n'a pas de champ couleur).
- **Pas de filtrage serveur** sur `list()` : le front filtre `active === true` quand il alimente le sélecteur du sidesheet shift.
- **`HH:MM` 24h, locale établissement** — cohérent avec `PlanningShift.start_time` / `end_time`.

---

## Exemple — réponse `GET /planning/shift-templates`

```json
{
  "id": "evt_42",
  "data": {
    "status": "success",
    "shift_templates": [
      {
        "id": "tmpl_01",
        "label": "Service midi",
        "start_time": "11:00",
        "end_time": "15:00",
        "break_minutes": 0,
        "position_id": null,
        "color": "#10b981",
        "sort_order": 0,
        "active": true,
        "created_at": "2026-05-25T08:00:00Z",
        "updated_at": "2026-05-25T08:00:00Z"
      },
      {
        "id": "tmpl_02",
        "label": "Coupure bar",
        "start_time": "11:00",
        "end_time": "23:00",
        "break_minutes": 180,
        "position_id": "pos_bar",
        "color": "#f59e0b",
        "sort_order": 1,
        "active": true,
        "created_at": "2026-05-25T08:00:00Z",
        "updated_at": "2026-05-25T08:00:00Z"
      }
    ]
  }
}
```

---

## TODO côté backend

1. Implémenter les 4 endpoints avec la forme JSON ci-dessus.
2. Suppression LOGIQUE (`active=false`), pas hard-delete.
3. Pas besoin de FK depuis `planning_shifts` vers `shift_templates` — le template est un raccourci d'UI.
4. À la bascule côté front : remplacer le corps de chaque méthode dans `shiftTemplateService.ts` par UN appel `apiClient.*` (TODO inlines marquent la ligne exacte à modifier).
