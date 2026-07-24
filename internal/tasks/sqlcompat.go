package tasks

import "welloresto-api/internal/database/dbx"

// Fragments SQL portables MySQL/Postgres pour internal/tasks/, sur le même
// principe que bkgAbsSecondsFromNow (internal/modules/bookings/repository.go)
// et workedExpr (internal/modules/planning/performance/repository.go) :
// TIMESTAMPDIFF/UNIX_TIMESTAMP/DATE_SUB(..., INTERVAL ? UNIT) sont MySQL-only,
// aucun de ces fragments ne comporte de `?` propre sauf mention contraire.

// tskMinutesSince : minutes écoulées entre col (timestamp/timestamptz) et
// maintenant (UTC).
func tskMinutesSince(col string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "FLOOR(EXTRACT(EPOCH FROM (" + dbx.UTCNow() + " - " + col + ")) / 60)"
	}
	return "TIMESTAMPDIFF(MINUTE, " + col + ", " + dbx.UTCNow() + ")"
}

// tskSecondsBetween : écart en secondes entre deux colonnes timestamp (from, to).
func tskSecondsBetween(from, to string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "EXTRACT(EPOCH FROM (" + to + " - " + from + "))::bigint"
	}
	return "TIMESTAMPDIFF(SECOND, " + from + ", " + to + ")"
}

// tskUnixTimestamp : équivalent UNIX_TIMESTAMP(col).
func tskUnixTimestamp(col string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "EXTRACT(EPOCH FROM " + col + ")::bigint"
	}
	return "UNIX_TIMESTAMP(" + col + ")"
}

// tskNowMinusMinutes : "maintenant (UTC) - N minutes", N paramétré par un `?`
// supplémentaire porté par le fragment (même position que l'original
// DATE_SUB(UTC_TIMESTAMP(), INTERVAL ? MINUTE)).
func tskNowMinusMinutes() string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "(" + dbx.UTCNow() + " - (? * interval '1 minute'))"
	}
	return "DATE_SUB(" + dbx.UTCNow() + ", INTERVAL ? MINUTE)"
}

// tskNowMinusDays : "maintenant - N jours", N paramétré par un `?`
// supplémentaire porté par le fragment. NOW() est valide dans les deux
// dialectes (aucune traduction requise pour la fonction elle-même, seule la
// syntaxe INTERVAL diverge) — conservé tel quel (pas UTC_TIMESTAMP) pour ne
// pas changer le comportement horloge des tâches qui l'utilisaient déjà.
func tskNowMinusDays() string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "(NOW() - (? * interval '1 day'))"
	}
	return "DATE_SUB(NOW(), INTERVAL ? DAY)"
}

// tskNowMinus30Days : "maintenant - 30 jours" (borne fixe, non paramétrée).
func tskNowMinus30Days() string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "NOW() - INTERVAL '30 days'"
	}
	return "NOW() - INTERVAL 30 DAY"
}

// tskMerchantJoinCast : cast portable de merchant.id (integer identity) pour
// le comparer à une colonne merchant_id (varchar) dans une jointure — même
// pattern déjà établi dans auth/users/ubereats/scannorder/reservation
// (CAST(m.id AS CHAR) MySQL / AS TEXT Postgres). MySQL coerce silencieusement
// integer/varchar dans une jointure, Postgres refuse la comparaison sans cast
// explicite ("operator does not exist: integer = character varying").
// Suppose que l'alias de la table merchant dans la requête est `m`.
func tskMerchantJoinCast() string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "CAST(m.id AS TEXT)"
	}
	return "CAST(m.id AS CHAR)"
}
