# 24 — Traduction de la procédure stockée GET_POS_STATUS en Go

La procédure MySQL `GET_POS_STATUS` (statut ouvert/fermé d'un marchand +
bornes du dernier/courant/prochain créneau) était le seul point des modules
`pos` et `scannorder` à dépendre d'une procédure stockée — non portable vers
Postgres sans réécriture PL/pgSQL. Elle est remplacée par un calcul Go pur,
la base ne servant plus qu'à lire les créneaux.

## Nouveau package : `internal/modules/openinghours`

| Fichier | Rôle |
|---|---|
| `openinghours.go` | `ComputePOSStatus(currentDatetime time.Time, slots []Slot) POSStatus` — calcul pur, zéro accès base |
| `repository.go` | `FetchActiveSlots(ctx, db, merchantID, currentDatetime)` — lecture SQL simple via `dbx` (rebind cross-dialecte) |
| `openinghours_test.go` | 12 tests unitaires purs (la procédure MySQL n'était couverte par aucun test) |
| `repository_pg_test.go` | Test d'intégration contre le Postgres Docker de dev, ignoré sans `POSTGRES_TEST_URL` |

### Séparation lecture / calcul

```go
slots, err := openinghours.FetchActiveSlots(ctx, r.database, merchantID, now)
// ...
status := openinghours.ComputePOSStatus(now, slots) // pur, testable sans base
```

`FetchActiveSlots` reprend les filtres communs aux trois SELECT de la
procédure : `merchant_id`, `enabled`, fenêtre `valid_from`/`valid_to` comparée
à `currentDatetime` (heure locale marchand, ex-`p_current_datetime`). Toute la
logique de sélection dernier/courant/prochain est dans `ComputePOSStatus`.

### Correspondance MySQL → Go

| Procédure MySQL | Go |
|---|---|
| `WEEKDAY(p_current_datetime) + 1` (1 = lundi … 7 = dimanche) | `isoWeekday()` : `time.Weekday()` avec dimanche 0 → 7 |
| `TIME(p_current_datetime)` et comparaisons `TIME` | secondes depuis minuit (`parseClock`, gère `HH:MM:SS` et `HH:MM`) |
| `DATE_ADD`/`DATE_SUB(DATE(...), INTERVAL n DAY)` + `CONCAT(date, ' ', heure)` | `time.Date(y, m, d+delta, h, mn, s, 0, loc)` — arithmétique calendaire, normalisation des débordements par `time.Date` |
| Dernier créneau : `day > day_to OR (day = day_to AND time > hour_to)`, `ORDER BY day_from DESC, hour_from DESC LIMIT 1` | même prédicat, max par `(DayOfWeekFrom, HourFrom)` |
| Créneau courant : `day BETWEEN day_from AND day_to AND time BETWEEN hour_from AND hour_to` (`LIMIT 1` sans `ORDER BY`) | même prédicat (bornes inclusives) ; choix rendu **déterministe** : min par `(DayOfWeekFrom, HourFrom)` |
| Prochain créneau : `day < day_from OR (day = day_from AND time < hour_from)`, `ORDER BY day_from, hour_from LIMIT 1` | même prédicat, min par `(DayOfWeekFrom, HourFrom)` |
| `p_is_open = 1` si créneau courant trouvé | `POSStatus.IsOpen` |
| OUT `NULL` | borne `*time.Time` nil ; `FormatDateTime` rend `""` (équivalent du `sql.NullString.String` des appelants) |

### Quirks MySQL volontairement conservés

Vérifiés par les tests unitaires — ne pas « corriger » sans décision produit :

- **Semaine bornée** : un lundi matin, le créneau du dimanche précédent n'est
  pas « dernier créneau » (`1 > 7` faux) — il redevient « prochain créneau »
  (dimanche suivant). `LastStart` reste NULL en début de semaine.
- **Créneau à cheval sur minuit** (`hour_from > hour_to`) : ne matche jamais
  comme créneau courant (le `BETWEEN` échoue). Ces horaires doivent être
  saisis en deux lignes (`22:00→23:59:59` + `00:00→02:00`), comme avant.
- **Créneau multi-jours courant** : les bornes `current_start`/`current_end`
  sont datées du jour courant, pas du début/fin réels de la plage.

## Sites d'appel remplacés

Les deux appelants n'exploitaient que `@p_is_open`, `@p_next_start`,
`@p_next_end` (les bornes last/current étaient calculées puis jetées).

1. **`internal/modules/pos/repository.go` — `GetPOSStatus`** : le bloc
   `CALL GET_POS_STATUS` + `SELECT @p_is_open, @p_next_start, @p_next_end`
   devient `FetchActiveSlots` + `ComputePOSStatus`. `isOpen == 1` →
   `hoursStatus.IsOpen` ; `nextStart.String` → `FormatDateTime(hoursStatus.NextStart)`.
2. **`internal/modules/scannorder/repository.go` — `GetMerchantStatus`** :
   étapes 4️⃣/5️⃣ (CALL + lecture des variables de session **sur la même
   connexion**) remplacées à l'identique. Bonus : la contrainte « same conn »
   pour relire les `@vars` disparaît — un point de fragilité en moins avec le
   pool à 1 connexion.

La procédure `GET_POS_STATUS` n'a plus aucun appelant côté API : elle n'est
pas à porter vers Postgres (à retirer du périmètre listé dans
`07-module-inventory.md`).

## Requête SQL cross-dialecte

```sql
SELECT day_of_week_from, day_of_week_to,
       CAST(hour_from AS CHAR(8)) AS hour_from,
       CAST(hour_to AS CHAR(8)) AS hour_to
FROM hours_of_operation
WHERE merchant_id = ?
  AND enabled = TRUE
  AND (valid_from IS NULL OR valid_from <= ?)
  AND (valid_to IS NULL OR valid_to >= ?)
ORDER BY day_of_week_from, hour_from, day_of_week_to, hour_to
```

- `CAST(... AS CHAR(8))` : produit `HH:MM:SS` depuis un `TIME` MySQL comme
  depuis un `time` Postgres (le driver pgx ne scanne pas `time` → `string`
  nativement) ; `08:00:00` fait exactement 8 caractères, pas de padding.
- `enabled = TRUE` : valide en MySQL (`tinyint`) et Postgres (`boolean`).
- `ORDER BY` complet : rend le résultat (et donc le choix « premier créneau
  courant ») déterministe, ce que le `LIMIT 1` sans `ORDER BY` de la
  procédure ne garantissait pas.
- Placeholders `?` rebindés en `$N` par `dbx` quand `DB_DIALECT=postgres`.

**Caveat `valid_from`/`valid_to`** : le paramètre est l'heure locale marchand
formatée en chaîne naïve (comportement historique de `p_current_datetime`).
Côté Postgres, les colonnes cibles sont `timestamptz` : la chaîne naïve est
interprétée dans le fuseau de session (UTC par défaut), soit un décalage
possible de quelques heures **uniquement aux bornes des fenêtres de
validité** — même classe de question que les autres colonnes datetime locales
(cf. `04-schema-mapping-notes.md`). À trancher lors de la migration des
données de cette table.

## Vérifications effectuées (2026-07-18)

- `go build ./...` OK.
- `go test ./internal/modules/openinghours/...` : 12 tests unitaires purs
  verts (ouvert, fermé avant/après, bornes inclusives, minuit, dimanche,
  lundi/semaine bornée, plage multi-jours, choix du plus proche, format
  `HH:MM`, heures malformées, aucun créneau).
- Test d'intégration contre le Postgres Docker de dev
  (`docker compose -f docker-compose.postgres.yml up -d`) :

  ```
  POSTGRES_TEST_URL="postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev" \
    go test ./internal/modules/openinghours/... -run Postgres -v
  ```

  Crée la table (DDL cible), insère 5 créneaux (dont un désactivé et un à
  fenêtre expirée), vérifie le filtrage de `FetchActiveSlots` sous
  `DB_DIALECT=postgres` et le résultat complet de `ComputePOSStatus` — PASS.
- Suite complète `go test ./internal/...` : échecs uniquement dans des
  modules non touchés (auth, bookingcomm, planning/employees, vet
  ubereats/pos-accounting), reproduits à l'identique sans ces changements
  (préexistants sur la branche).
