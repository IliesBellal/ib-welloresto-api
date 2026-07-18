package openinghours

import (
	"context"
	"database/sql"
	"time"

	"welloresto-api/internal/database/dbx"
)

// FetchActiveSlots lit les créneaux actifs d'un marchand avec les mêmes
// filtres que l'ancienne procédure GET_POS_STATUS : enabled + fenêtre
// valid_from/valid_to. currentDatetime doit être dans le fuseau du marchand
// (ex-paramètre p_current_datetime). Le tri rend le calcul déterministe.
func FetchActiveSlots(ctx context.Context, database *sql.DB, merchantID string, currentDatetime time.Time) ([]Slot, error) {
	db := dbx.GetDB(ctx, database)

	now := currentDatetime.Format(DateTimeLayout)

	// CAST en CHAR(8) : rend "HH:MM:SS" aussi bien depuis un TIME MySQL que
	// depuis un time Postgres (le driver pgx ne scanne pas time -> string).
	rows, err := db.QueryContext(ctx, `
		SELECT day_of_week_from,
		       day_of_week_to,
		       CAST(hour_from AS CHAR(8)) AS hour_from,
		       CAST(hour_to AS CHAR(8)) AS hour_to
		FROM hours_of_operation
		WHERE merchant_id = ?
		  AND enabled = TRUE
		  AND (valid_from IS NULL OR valid_from <= ?)
		  AND (valid_to IS NULL OR valid_to >= ?)
		ORDER BY day_of_week_from, hour_from, day_of_week_to, hour_to`,
		merchantID, now, now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var slots []Slot
	for rows.Next() {
		var s Slot
		if err := rows.Scan(&s.DayOfWeekFrom, &s.DayOfWeekTo, &s.HourFrom, &s.HourTo); err != nil {
			return nil, err
		}
		slots = append(slots, s)
	}
	return slots, rows.Err()
}
