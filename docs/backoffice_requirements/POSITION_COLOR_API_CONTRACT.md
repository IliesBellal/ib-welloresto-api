# Position Color — API Contract

**Status**: Front-end shipped (mock end-to-end). Backend must catch up.
**Scope**: `EmployeePosition` (table `positions`).

## TL;DR

Add a **required** `color` column (hex `#RRGGBB`, 7 chars) to the `positions` table and surface it through the existing endpoints.

## Schema change

```sql
ALTER TABLE positions
  ADD COLUMN color CHAR(7) NOT NULL DEFAULT '#94a3b8';
-- After backfilling existing rows with sensible defaults, drop the DEFAULT:
ALTER TABLE positions
  ALTER COLUMN color DROP DEFAULT;
```

Validation rule (server-side): `color` MUST match `^#[0-9a-fA-F]{6}$`. Reject otherwise with `error_invalid_data`.

## Endpoints affected

### `GET /planning/positions` → `data.positions[]`

Each item now includes `color: string`:

```jsonc
{
  "id": "pos-1",
  "merchant_id": "...",
  "label": "Manager",
  "color": "#3b82f6",
  "sort_order": 0,
  "active": true,
  "employee_count": 2,
  "created_at": "...",
  "updated_at": "..."
}
```

### `POST /planning/positions`

`color` is **REQUIRED** on creation — no silent default. Reject with `error_invalid_data` (`"La couleur du poste est requise (hex #RRGGBB)."`) if missing or malformed.

```jsonc
// Request body
{
  "label": "Barman",
  "color": "#ec4899",
  "sort_order": 5,   // optional
  "active": true     // optional
}
```

### `PATCH /planning/positions/{id}`

`color` is optional. Same hex validation when provided.

```jsonc
{ "color": "#f59e0b" }
```

### `GET /planning/positions/{id}` / `DELETE /planning/positions/{id}`

No shape change beyond the new `color` field in the GET response.

## Front-end usage

- The hex drives the **position badge** color in the planning grid and the **shift cards** (`ShiftCard`):
  - background: `color + alpha 0x1f` (~12%)
  - border: `color + alpha 0x66` (~40%)
  - dot (pill): the raw hex.
- Unassigned shifts (`employee_id === null`) keep the color of their own position — no override for "unassigned".
- A small preset palette is offered in the position creation/edit dialog (`src/lib/planningShiftColor.ts → POSITION_COLOR_PRESETS`), plus a native `<input type="color">` for custom values.

## Resolution helpers (front-end, for reference)

```ts
// src/lib/planningShiftColor.ts
export function resolveShiftColor(
  shift: { position_id?: string | null; position?: string | null },
  positions: { id: string; label: string; color: string }[],
): string;
```

Lookup order:
1. `shift.position_id` → `positions[].id`.
2. `shift.position` (label, case-insensitive) → `positions[].label` (legacy fallback).
3. `DEFAULT_SHIFT_COLOR` = `#94a3b8` (slate-400).

## Migration notes

- Existing rows: pick a default per `label` (e.g., Manager → `#3b82f6`, Serveur → `#10b981`, Cuisinier → `#f59e0b`…) before flipping `NOT NULL` without default.
- Front-end tolerates an absent `color` by displaying `DEFAULT_SHIFT_COLOR`, but the contract is **NOT NULL** going forward.
