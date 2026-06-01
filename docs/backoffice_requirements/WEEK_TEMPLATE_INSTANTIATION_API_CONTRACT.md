# Week Template Instantiation — API Contract (FIGÉ)

> **Statut** : Contrat figé côté front. Le **mock fait office de spécification** :
> `src/services/weekTemplateService.ts` (`previewWeekTemplate` + `instantiateWeekTemplate`)
> et `src/lib/weekTemplateProjection.ts` (logique pure) implémentent à l'identique
> ce que le backend doit reproduire.
>
> **Phase** : 3/3 du module Planning (1 = position color, 2 = week templates CRUD,
> 3 = instanciation).

---

## 1. Vue d'ensemble

L'instanciation **applique un `WeekTemplate` à une ou plusieurs semaines réelles**
(`PlanningWeek`). Deux endpoints :

| Méthode | URL | Rôle |
|---|---|---|
| `POST` | `/planning/week-templates/{id}/preview` | DRY-RUN — détecte conflits sans rien écrire. |
| `POST` | `/planning/week-templates/{id}/instantiate` | Exécute. Renvoie les compteurs. |

**Workflow UI inspiré de Skello** :

1. L'utilisateur ouvre `ApplyWeekTemplateDialog` depuis la liste des modèles
   (bouton `Send` sur chaque ligne).
2. Sélection des semaines cibles (par défaut : semaine courante du planning).
3. La preview tourne automatiquement à chaque changement (mode ou sélection).
4. Si `conflicts.length === 0 && auto_unassigned_count === 0` → confirmation simple.
5. Sinon → écran de résolution : résumé + liste détaillée groupée par employé +
   `RadioGroup` des 3 modes.
6. À la confirmation, l'instanciation crée d'abord les semaines manquantes
   (`POST /planning/weeks`) puis écrit les shifts.

---

## 2. Types — Source de vérité

Tous définis dans [`src/types/planning.ts`](../../src/types/planning.ts) §"Week
template — instantiation". Reprise verbatim :

```ts
export type ConflictMode = "keep_existing" | "replace" | "template_to_unassigned";
export type ConflictReason = "overlap" | "on_leave" | "contract_ended";

export interface InstantiationShiftRef {
  day_of_week: number;          // 0..6 (0 = dimanche, JS getDay)
  start_time: string;           // "HH:MM" 24h
  end_time: string;             // "HH:MM" 24h
  position_id: string | null;
}

export interface InstantiationConflict {
  target_week_start: string;            // ISO "YYYY-MM-DD" (lundi)
  day: string;                          // ISO "YYYY-MM-DD" (date réelle)
  template_shift: InstantiationShiftRef;
  existing_shift_id: string | null;     // null pour on_leave / contract_ended
  employee_id: string;                  // jamais null (les besoins n'ont pas de conflit)
  employee_name: string;                // pré-calculé (réduit jointures UI)
  reason: ConflictReason;
}

export interface InstantiationPreview {
  target_week_starts: string[];         // normalisé : unique + trié croissant
  to_create_count: number;
  conflicts: InstantiationConflict[];
  impacted_employee_count: number;      // DISTINCT — 30 conflits / 1 emp = 1
  auto_unassigned_count: number;        // on_leave ∪ contract_ended
  idempotent_skipped_count: number;
}

export interface InstantiationPerWeekResult {
  target_week_start: string;
  week_id: string;
  created_count: number;
  assigned_count: number;
  unassigned_count: number;
  replaced_count: number;
  skipped_count: number;
}

export interface InstantiationResult {
  created_count: number;
  assigned_count: number;
  unassigned_count: number;
  replaced_count: number;
  skipped_count: number;
  per_week: InstantiationPerWeekResult[];
}

export interface InstantiationPreviewRequest { target_week_starts: string[]; }
export interface InstantiationRequest {
  target_week_starts: string[];
  conflict_mode: ConflictMode;
}
```

---

## 3. Endpoints

### 3.1 `POST /planning/week-templates/{id}/preview`

**Request body**

```json
{ "target_week_starts": ["2026-06-01", "2026-06-08"] }
```

- `target_week_starts` : liste de lundis ISO `YYYY-MM-DD`. Le backend doit
  **dédoublonner + trier croissant** avant traitement et echo dans la réponse.
- Pas de `conflict_mode` ici : la preview classifie tout en mode `keep_existing`
  (le plus sûr). La UI rejoue la preview localement si l'utilisateur change de
  mode (cf. mock). Le backend PEUT accepter un paramètre `conflict_mode`
  optionnel pour économiser un round-trip si la UI évolue ; **non requis pour le
  v1**.

**Response 200**

```json
{
  "id": "evt-...",
  "status": "ok",
  "data": {
    "preview": {
      "target_week_starts": ["2026-06-01", "2026-06-08"],
      "to_create_count": 14,
      "impacted_employee_count": 2,
      "auto_unassigned_count": 1,
      "idempotent_skipped_count": 0,
      "conflicts": [
        {
          "target_week_start": "2026-06-01",
          "day": "2026-06-03",
          "template_shift": { "day_of_week": 3, "start_time": "09:00", "end_time": "17:00", "position_id": "pos-2" },
          "existing_shift_id": "sh-1234",
          "employee_id": "emp-7",
          "employee_name": "Alice Martin",
          "reason": "overlap"
        }
      ]
    }
  }
}
```

**Side effects** : AUCUN.

### 3.2 `POST /planning/week-templates/{id}/instantiate`

**Request body**

```json
{
  "target_week_starts": ["2026-06-01", "2026-06-08"],
  "conflict_mode": "keep_existing"
}
```

**Response 200**

```json
{
  "id": "evt-...",
  "status": "ok",
  "data": {
    "result": {
      "created_count": 13,
      "assigned_count": 10,
      "unassigned_count": 3,
      "replaced_count": 1,
      "skipped_count": 1,
      "per_week": [
        {
          "target_week_start": "2026-06-01",
          "week_id": "wk-uuid-A",
          "created_count": 7,
          "assigned_count": 5,
          "unassigned_count": 2,
          "replaced_count": 1,
          "skipped_count": 0
        },
        {
          "target_week_start": "2026-06-08",
          "week_id": "wk-uuid-B",
          "created_count": 6,
          "assigned_count": 5,
          "unassigned_count": 1,
          "replaced_count": 0,
          "skipped_count": 1
        }
      ]
    }
  }
}
```

**Side effects autorisés** :

- Création automatique des `PlanningWeek` manquantes pour chaque
  `target_week_start` (label par défaut : « Semaine du <jour> <mois> <année> »,
  `end_date = start_date + 6j`).
- Création de `PlanningShift` (POST internes équivalents à
  `/planning/weeks/{week_id}/shifts`).
- Suppression de `PlanningShift` **uniquement en mode `replace`** et
  **uniquement** sur les shifts identifiés comme overlap explicite (présents
  dans la preview avec `reason: "overlap"` et `existing_shift_id` non null).

**Side effects interdits** :

- ❌ Ne JAMAIS supprimer un shift existant hors du cas `replace`/`overlap` ci-dessus.
- ❌ Ne JAMAIS modifier un shift existant (pas d'`UPDATE`). Seules `INSERT` et
  `DELETE` (en mode replace) sont autorisées.

---

## 4. Sémantique EXACTE de la classification

Pour chaque `(template_shift, target_week_start)`, le backend doit appliquer
ce pipeline **dans cet ordre exact** :

```
date_du_shift = projection(target_week_start /* lundi */, template_shift.day_of_week)

1. SI template_shift.employee_id IS NULL
   → create_assigned (mais sans employé) = "create_unassigned(reason=need)"
   → pas de conflit

2. SI un PlanningShift existe avec:
       employee_id = template_shift.employee_id
       AND shift_date = date_du_shift
       AND start_time = template_shift.start_time
       AND end_time = template_shift.end_time
       AND position_id = template_shift.position_id
   → IDEMPOTENT : skipped_count++ (pas de doublon, pas de conflit)

3. SI l'employé a un PlanningLeaveRequest APPROUVÉ couvrant date_du_shift
   (status = "approved" AND start_date <= date <= end_date)
   → create_unassigned(reason=on_leave)
   → conflit signalé (reason: "on_leave", existing_shift_id: null)
   ⚠️ TOUJOURS, quel que soit conflict_mode (filet de sécurité).

4. SI l'employé.contract_end_date IS NOT NULL ET contract_end_date < date_du_shift
   → create_unassigned(reason=contract_ended)
   → conflit signalé (reason: "contract_ended", existing_shift_id: null)
   ⚠️ TOUJOURS, quel que soit conflict_mode.

5. SI overlap détecté avec un shift existant de cet employé sur ce jour
   (règle d'overlap : !(endA <= startB || startA >= endB))
   → application de conflict_mode :
       keep_existing          → skip (skipped_count++, existing intact)
       replace                → delete existing + create assigned (replaced_count++)
       template_to_unassigned → create_unassigned(reason=overlap_to_unassigned)
                                (existing intact)
   → conflit signalé (reason: "overlap", existing_shift_id: <id>)

6. SINON
   → create_assigned (avec template_shift.employee_id)
```

### 4.1 Règle de projection `day_of_week` → date

```
target_week_start = lundi ISO ("YYYY-MM-DD")
day_of_week ∈ {0..6} avec 0 = dimanche (JavaScript Date.getDay())

offset = (day_of_week === 0) ? 6 : day_of_week - 1
date_du_shift = target_week_start + offset jours
```

Tests d'ancrage (lundi `2026-06-01`) :

| `day_of_week` | jour | date |
|---:|---|---|
| 1 | lundi | `2026-06-01` |
| 2 | mardi | `2026-06-02` |
| 6 | samedi | `2026-06-06` |
| 0 | dimanche | `2026-06-07` |

### 4.2 Règle d'overlap

Identique au reste du module (cf. `src/lib/planningOverlap.ts:timesOverlap`) :

```ts
overlap(sa, ea, sb, eb) === !(ea <= sb || sa >= eb)
```

- Bornes STRICTES : un shift `09:00–12:00` ne chevauche pas `12:00–14:00`.
- Les besoins (`employee_id = null`) **ne conflictent JAMAIS** (cf. règle 1).

### 4.3 Comptage `impacted_employee_count`

**Cardinalité DISTINCT** des `employee_id` apparaissant dans `conflicts`.
Important : 30 conflits sur 1 seul employé → `impacted_employee_count = 1`
(signal d'absence probable, utile à la UI pour adapter le message).

### 4.4 Comptage `auto_unassigned_count`

Nombre de `template_shifts` qui SERONT créés en non-assigné à cause de
`on_leave` ou `contract_ended` (filets de sécurité). **Indépendant de
`conflict_mode`** — ces deux raisons l'emportent toujours.

Les non-assigné dus à `overlap_to_unassigned` (mode 3) ne sont **pas** comptés
ici (ils dépendent du mode choisi par l'utilisateur).

### 4.5 Comptage `idempotent_skipped_count`

Nombre de shifts du template strictement identiques à un shift existant (cf.
règle 2). Ils sont silencieusement ignorés (pas de conflit signalé).

---

## 5. Compteurs de la réponse `instantiate`

| Champ | Définition |
|---|---|
| `created_count` | Total des `INSERT INTO planning_shifts`. |
| `assigned_count` | Sous-ensemble de `created_count` avec `employee_id != null`. |
| `unassigned_count` | Sous-ensemble de `created_count` avec `employee_id = null` (sommé de : need, on_leave, contract_ended, overlap_to_unassigned). |
| `replaced_count` | Nombre de `DELETE` effectués (mode `replace` uniquement). Notez : `assigned_count` inclut déjà les recréations. |
| `skipped_count` | `idempotent_skipped_count` + (en mode `keep_existing`) overlaps ignorés. |

**Invariant** : `created_count == assigned_count + unassigned_count`.

---

## 6. Pourquoi ces choix (rationale)

- **`on_leave` / `contract_ended` toujours non-assigné, jamais "remplacer le
  congé"** : un congé approuvé est une décision RH, pas un simple conflit de
  shift. La UI le signale clairement (« → ira en non assigné ») et c'est non
  négociable.
- **Pas d'undo natif** mais opération traçable : les seuls `DELETE` autorisés
  sont annoncés dans la `preview` (overlap explicite, mode replace). Aucune
  surprise.
- **Idempotence raisonnable** : éviter les doublons quand l'utilisateur
  réapplique le même modèle.
- **Création automatique des semaines** : aligne le flux sur `ensureWeekFor`
  côté UI (cf. `src/pages/equipe/PlanningPage.tsx:65`).
- **Logique pure isolée** (`src/lib/weekTemplateProjection.ts`) : testable sans
  I/O ; le backend peut transposer 1:1 ce qui se passe dans `classifyTemplateShift`.

---

## 7. Gating

`manage_plannings`. Géré côté front au niveau de la page Planning. Le backend
doit également vérifier la permission sur les deux endpoints.

---

## 8. Tests d'acceptation backend (à reproduire)

Cf. `src/services/__tests__/weekTemplateInstantiation.test.ts` pour les
cas de référence :

1. `projectDayOfWeekToDate` — les 4 cas (lundi=1, mardi=2, samedi=6, dimanche=0).
2. Préséance : `idempotent` > `on_leave` > `contract_ended` > `overlap` >
   `create_assigned`.
3. Les 3 modes sur overlap (`keep_existing` / `replace` / `template_to_unassigned`).
4. `on_leave` et `contract_ended` → non-assigné **quel que soit** le mode.
5. Multi-semaines : `target_week_starts` normalisé (unique + trié), compteurs
   agrégés correctement.
6. `impacted_employee_count` DISTINCT.
7. Mode `replace` : `deleteShift` appelé **avant** `createShift`.

---

## 9. Endpoints adjacents (déjà existants, ne pas dupliquer)

- `POST /planning/weeks` — création de semaine (utilisé pour les semaines
  manquantes).
- `POST /planning/weeks/{id}/shifts` — création de shift (utilisé pour chaque
  shift instancié).
- `DELETE /planning/shifts/{id}` — suppression (utilisé en mode `replace`).
- `GET /planning/leave-requests?status=approved` — congés (utilisé par la
  preview côté front pour valider les filets de sécurité quand le mock est actif ;
  côté backend, à charger en interne).

---

## 10. Références

- Logique pure : [`src/lib/weekTemplateProjection.ts`](../../src/lib/weekTemplateProjection.ts)
- Service mock (= spec) : [`src/services/weekTemplateService.ts`](../../src/services/weekTemplateService.ts)
  fonctions `previewWeekTemplate` et `instantiateWeekTemplate`.
- UI : [`src/components/team/planning/ApplyWeekTemplateDialog.tsx`](../../src/components/team/planning/ApplyWeekTemplateDialog.tsx)
- Tests : [`src/services/__tests__/weekTemplateInstantiation.test.ts`](../../src/services/__tests__/weekTemplateInstantiation.test.ts)
- Contrat parent : [`WEEK_TEMPLATE_API_CONTRACT.md`](./WEEK_TEMPLATE_API_CONTRACT.md)
- Convention day-of-week : [`PLANNING_AND_USERS_INTEGRATION_GUIDE.md`](../../PLANNING_AND_USERS_INTEGRATION_GUIDE.md)
