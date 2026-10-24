// Package userids porte la transition des identifiants utilisateurs
// "user-<uuid>" vers des ids numériques (docs/ids-entiers-users-discounts-chantier.md).
//
// users.user_id reste un varchar. Depuis la migration 186, tout nouvel
// utilisateur reçoit un id numérique (users_user_id_seq). Les "user-<uuid>"
// créés entre 2026-04 et cette migration sont renumérotés par Apply
// (cmd/renumber_users), qui remplit user_id_remap (migration 187).
//
// Règle fiscale (fiscal/payloads.go) : payments.user_id et audit_logs.user_id
// font partie des charges utiles signées. Une ligne déjà signée garde son
// ancien id pour toujours ; seule user_id_remap la relie à l'utilisateur.
package userids

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/utils/dbutils"

	"go.uber.org/zap"
)

// Column est une colonne qui contient un users.user_id.
type Column struct {
	Table string
	Name  string
	// Sealed : la colonne entre dans une charge utile fiscale signée. Seules
	// les lignes non signées (signature IS NULL) sont réécrites.
	Sealed bool
}

func (c Column) String() string { return c.Table + "." + c.Name }

// Columns liste toutes les colonnes texte qui contiennent un id utilisateur
// (inventaire du 2026-10-20, par le contenu et non par le nom : voir le doc
// du chantier §2). Les colonnes integer/bigint (cash_funds.closed_by,
// planned_shifts.created_by, shift_templates.created_by,
// api_request_logs.user_id) n'ont jamais pu contenir de "user-<uuid>" et
// n'y figurent pas. Les colonnes absentes de la base visée sont ignorées.
//
// Toute nouvelle colonne qui stocke un id utilisateur doit être ajoutée ici
// tant que la transition n'est pas close (étape 5 du chantier).
var Columns = []Column{
	{"users_rights", "user_id", false},
	{"users_devices", "user_id", false},
	{"users_nfc_tags", "user_id", false},
	{"user_vacations", "user_id", false},
	{"employees", "user_id", false},
	{"employment_contract", "user_id", false},
	{"services_performed", "user_id", false},
	{"delivery_session", "user_id", false},
	{"delivery_position", "user_id", false},
	{"device_link", "user_id", false},
	{"qrcodes", "user_id", false},
	{"cash_registers", "user_id", false},
	{"cash_registers", "closed_by", false},
	{"cash_funds", "user_id", false},
	{"cash_reports", "user_id", false},
	{"cash_registers_custom_items", "created_by", false},
	{"stock_movements", "user_id", false},
	{"password_resets", "user_id", false},
	{"planned_shifts", "user_id", false},
	{"planning_leave_requests", "requested_by_user_id", false},
	{"planning_leave_requests", "processed_by_user_id", false},
	{"planning_shift_swap_requests", "requested_by_user_id", false},
	{"planning_shift_swap_requests", "processed_by_user_id", false},
	{"planning_day_comments", "created_by", false},
	{"planning_day_comments", "updated_by", false},
	{"kiosk_enrollment_codes", "created_by_user_id", false},
	{"cds_enrollment_codes", "created_by_user_id", false},
	{"attestations", "signed_by_user_id", false},
	{"cleaning_executions", "created_by", false},
	{"cleaning_sessions", "created_by", false},
	{"temperature_readings", "created_by", false},
	{"temperature_sessions", "created_by", false},
	{"temperature_reading_corrective_actions", "created_by", false},
	{"goods_receipts", "created_by", false},
	{"haccp_traceability_records", "created_by", false},
	{"hours_amendments", "created_by", false},
	{"menu_import_ai_credits", "updated_by", false},
	{"menu_import_drafts", "created_by", false},
	{"order_item_remakes", "created_by", false},
	{"order_changes_log", "changed_by_user_id", false},
	{"customer", "blocked_by", false},
	{"upsell_suggestions", "staff_member_id", false},
	{"notifications", "notification_user_id", false},
	{"welloresto_stripe_customers", "creator_user_id", false},
	{"access_observation", "user_id", false},
	{"api_calls", "user_id", false},
	{"bookings", "created_by", false},
	{"bookings", "updated_by", false},
	{"merchant_closing_modes", "created_by", false},
	{"subscription_overrides", "created_by", false},
	// Colonnes « acteur » (contiennent aussi SCANNORDER, KIOSK, WEBHOOK_*…) :
	// seules les valeurs présentes dans user_id_remap sont touchées.
	// orders.created_by et cash_registers ne sont pas dans les charges
	// utiles signées (OrderClosurePayload, CashRegisterClosurePayload).
	{"orders", "created_by", false},
	{"order_comments", "user_id", false},
	{"payments", "collected_by", false},
	{"payments", "user_id", true},
	{"audit_logs", "user_id", true},
}

// IsNumeric indique si id est déjà au format numérique (rien à traduire).
func IsNumeric(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// LegacyPrefix est le préfixe des ids générés par helpers.GeneratePrefixedID
// entre 2026-04 et la migration 186 — le seul format non numérique jamais
// produit par l'API.
const LegacyPrefix = "user-"

// Resolve traduit un ancien id ("user-<uuid>") encore envoyé par un client en
// circulation vers son nouvel id, via user_id_remap. Tout autre id (numérique,
// vide, autre format) ne coûte aucune requête et est renvoyé tel quel, comme
// un "user-<uuid>" inconnu de la table : l'appelant garde son comportement
// habituel (404, liste vide…). Une erreur SQL est loguée, jamais remontée —
// ce filet ne doit pas faire échouer une requête qui aurait réussi sans lui.
func Resolve(ctx context.Context, db *sql.DB, id string) string {
	trimmed := strings.TrimSpace(id)
	if !strings.HasPrefix(trimmed, LegacyPrefix) {
		return id
	}
	log := logger.FromContext(ctx)

	var newID string
	err := dbx.GetDB(ctx, db).QueryRowContext(ctx,
		`SELECT user_id FROM user_id_remap WHERE legacy_user_id = ?`, trimmed,
	).Scan(&newID)
	switch {
	case err == nil:
		log.Warn("legacy user_id translated", zap.String("legacy_user_id", trimmed), zap.String("user_id", newID))
		return newID
	case errors.Is(err, sql.ErrNoRows):
		return id
	default:
		log.Warn("legacy user_id lookup failed", zap.String("legacy_user_id", trimmed), zap.Error(err))
		return id
	}
}

// ColumnCount compte, pour une colonne, les lignes qui portent un ancien id.
type ColumnCount struct {
	Column Column
	// Rewrite : lignes que Apply réécrit.
	Rewrite int64
	// KeptSealed : lignes signées qui gardent l'ancien id (Sealed seulement).
	KeptSealed int64
}

// Report décrit ce que Apply fait (Plan) ou a fait (Apply).
type Report struct {
	// Users : anciens ids -> nouveaux ids. Dans Plan, les nouveaux ids ne
	// sont pas encore tirés de la séquence et valent "".
	Users   map[string]string
	Columns []ColumnCount
	// Missing : colonnes de Columns absentes de la base visée.
	Missing []Column
}

// scope restreint Plan/Apply à certains anciens ids. Vide = tous (usage de
// production, cmd/renumber_users). Les tests d'intégration, qui tournent
// contre une base partagée, ne renumérotent ainsi que leurs propres
// utilisateurs.
type scope []string

// users : filtre sur users.user_id (alias de table fourni).
func (s scope) users(alias string) (string, []interface{}) {
	if len(s) == 0 {
		return "", nil
	}
	return " AND " + alias + ".user_id = ANY(?)", []interface{}{[]string(s)}
}

// remap : filtre sur user_id_remap.legacy_user_id (alias de table fourni).
func (s scope) remap(alias string) (string, []interface{}) {
	if len(s) == 0 {
		return "", nil
	}
	return " AND " + alias + ".legacy_user_id = ANY(?)", []interface{}{[]string(s)}
}

// legacyIDs : tout id non numérique encore dans users (à renuméroter) ou déjà
// dans user_id_remap (renuméroté lors d'un passage précédent, mais des lignes
// ont pu être écrites avec l'ancien id entre-temps).
func (s scope) legacyIDs() (string, []interface{}) {
	uf, ua := s.users("lu")
	rf, ra := s.remap("lr")
	return `
		SELECT lu.user_id FROM users lu WHERE lu.user_id !~ '^[0-9]+$'` + uf + `
		UNION
		SELECT lr.legacy_user_id FROM user_id_remap lr WHERE TRUE` + rf, append(ua, ra...)
}

// Plan calcule ce que Apply ferait, sans rien écrire.
func Plan(ctx context.Context, db *sql.DB) (Report, error) {
	return plan(ctx, db, nil)
}

func plan(ctx context.Context, db *sql.DB, only scope) (Report, error) {
	q := dbx.GetDB(ctx, db)
	rep := Report{Users: map[string]string{}}

	uf, ua := only.users("u")
	rows, err := q.QueryContext(ctx, `
		SELECT u.user_id, COALESCE(r.user_id, '')
		FROM users u
		LEFT JOIN user_id_remap r ON r.legacy_user_id = u.user_id
		WHERE u.user_id !~ '^[0-9]+$'`+uf, ua...)
	if err != nil {
		return Report{}, fmt.Errorf("userids: plan users: %w", err)
	}
	for rows.Next() {
		var legacy, next string
		if err := rows.Scan(&legacy, &next); err != nil {
			rows.Close()
			return Report{}, fmt.Errorf("userids: plan users: %w", err)
		}
		rep.Users[legacy] = next
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Report{}, fmt.Errorf("userids: plan users: %w", err)
	}

	existing, err := existingColumns(ctx, q)
	if err != nil {
		return Report{}, err
	}
	legacy, la := only.legacyIDs()
	for _, c := range Columns {
		if !existing[c.String()] {
			rep.Missing = append(rep.Missing, c)
			continue
		}
		cc := ColumnCount{Column: c}
		if c.Sealed {
			err = q.QueryRowContext(ctx, fmt.Sprintf(`
				SELECT count(*) FILTER (WHERE signature IS NULL), count(*) FILTER (WHERE signature IS NOT NULL)
				FROM %s WHERE %s IN (%s)`, c.Table, c.Name, legacy), la...).Scan(&cc.Rewrite, &cc.KeptSealed)
		} else {
			err = q.QueryRowContext(ctx, fmt.Sprintf(`
				SELECT count(*) FROM %s WHERE %s IN (%s)`, c.Table, c.Name, legacy), la...).Scan(&cc.Rewrite)
		}
		if err != nil {
			return Report{}, fmt.Errorf("userids: plan %s: %w", c, err)
		}
		rep.Columns = append(rep.Columns, cc)
	}
	return rep, nil
}

// Apply renumérote, dans une seule transaction, chaque utilisateur au format
// non numérique : nouvel id tiré de users_user_id_seq, correspondance écrite
// dans user_id_remap, puis toutes les colonnes de Columns réécrites (lignes
// signées exceptées), users en dernier. Rejouable : un second passage ne crée
// aucune nouvelle correspondance et ne réécrit que les lignes écrites avec un
// ancien id depuis le premier (sessions encore en cache, clients pas à jour).
//
// Le cache Redis des sessions (models.UserCachePrefix) porte l'ancien
// UserID : l'appelant doit le vider juste après (cmd/renumber_users le fait).
func Apply(ctx context.Context, db *sql.DB) (Report, error) {
	return apply(ctx, db, nil)
}

func apply(ctx context.Context, db *sql.DB, only scope) (Report, error) {
	rep := Report{Users: map[string]string{}}
	err := dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
		q := dbx.GetDB(txCtx, db)

		// Deux Apply concurrents tireraient deux ids pour le même utilisateur.
		if _, err := q.ExecContext(txCtx, `LOCK TABLE user_id_remap IN EXCLUSIVE MODE`); err != nil {
			return fmt.Errorf("userids: lock: %w", err)
		}

		uf, ua := only.users("u")
		if _, err := q.ExecContext(txCtx, `
			INSERT INTO user_id_remap (legacy_user_id, user_id)
			SELECT legacy.user_id, nextval('users_user_id_seq')::text
			FROM (
				SELECT u.user_id
				FROM users u
				WHERE u.user_id !~ '^[0-9]+$'
				  AND NOT EXISTS (SELECT 1 FROM user_id_remap r WHERE r.legacy_user_id = u.user_id)`+uf+`
				ORDER BY u.created_at NULLS LAST, u.user_id
			) legacy`, ua...); err != nil {
			return fmt.Errorf("userids: fill user_id_remap: %w", err)
		}

		existing, err := existingColumns(txCtx, q)
		if err != nil {
			return err
		}
		rf, ra := only.remap("r")
		for _, c := range Columns {
			if !existing[c.String()] {
				rep.Missing = append(rep.Missing, c)
				continue
			}
			guard := ""
			if c.Sealed {
				guard = " AND t.signature IS NULL"
			}
			res, err := q.ExecContext(txCtx, fmt.Sprintf(`
				UPDATE %[1]s AS t SET %[2]s = r.user_id
				FROM user_id_remap r
				WHERE t.%[2]s = r.legacy_user_id%[3]s%[4]s`, c.Table, c.Name, guard, rf), ra...)
			if err != nil {
				return fmt.Errorf("userids: rewrite %s: %w", c, err)
			}
			n, _ := res.RowsAffected()
			cc := ColumnCount{Column: c, Rewrite: n}
			if c.Sealed {
				if err := q.QueryRowContext(txCtx, fmt.Sprintf(`
					SELECT count(*) FROM %[1]s t JOIN user_id_remap r ON r.legacy_user_id = t.%[2]s
					WHERE t.signature IS NOT NULL%[3]s`, c.Table, c.Name, rf), ra...).Scan(&cc.KeptSealed); err != nil {
					return fmt.Errorf("userids: count sealed %s: %w", c, err)
				}
			}
			rep.Columns = append(rep.Columns, cc)
		}

		// users en dernier : jusque-là, l'ancien id reste la clé de référence.
		rows, err := q.QueryContext(txCtx, `
			UPDATE users u SET user_id = r.user_id
			FROM user_id_remap r
			WHERE u.user_id = r.legacy_user_id`+rf+`
			RETURNING r.legacy_user_id, r.user_id`, ra...)
		if err != nil {
			return fmt.Errorf("userids: rewrite users: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var legacy, next string
			if err := rows.Scan(&legacy, &next); err != nil {
				return fmt.Errorf("userids: rewrite users: %w", err)
			}
			rep.Users[legacy] = next
		}
		return rows.Err()
	})
	if err != nil {
		return Report{}, err
	}
	return rep, nil
}

// Leftover est une colonne texte HORS de Columns qui contient encore un ancien
// identifiant ("user-…"), avant ou après la renumérotation.
type Leftover struct {
	Table, Column string
	Rows          int64
}

// auditIgnored : colonnes qui contiennent légitimement un ancien id sans
// pouvoir être réécrites. audit_logs.resource_id fait partie de la charge
// utile signée du journal d'audit (fiscal.AuditLogPayload) ; les contenus JSON
// (old_values, new_values, journaux de requêtes) ne sont de toute façon pas
// parcourus.
var auditIgnored = map[string]bool{
	"audit_logs.resource_id":       true,
	"users.user_id":                true,
	"user_id_remap.legacy_user_id": true,
	"user_id_remap.user_id":        true,
}

// Audit cherche, dans TOUTES les colonnes texte de la base, les anciens
// identifiants (utilisateurs encore à renuméroter, ou déjà renumérotés dans
// user_id_remap) que Columns ne couvre pas. L'inventaire de Columns vient du
// schéma et du contenu d'un environnement de test : une colonne présente
// seulement en production, ou vide ailleurs, lui échapperait, et la
// renumérotation y laisserait un identifiant orphelin.
//
// Lecture seule, mais elle parcourt chaque colonne texte de chaque table :
// à lancer hors des heures de service.
func Audit(ctx context.Context, db *sql.DB) ([]Leftover, error) {
	q := dbx.GetDB(ctx, db)
	listed := map[string]bool{}
	for _, c := range Columns {
		listed[c.String()] = true
	}

	rows, err := q.QueryContext(ctx, `
		SELECT c.table_name, c.column_name
		FROM information_schema.columns c
		JOIN information_schema.tables t
		  ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema = current_schema() AND t.table_type = 'BASE TABLE'
		  AND c.data_type IN ('character varying', 'text', 'character')
		ORDER BY c.table_name, c.column_name`)
	if err != nil {
		return nil, fmt.Errorf("userids: audit schema: %w", err)
	}
	var candidates []Column
	for rows.Next() {
		var col Column
		if err := rows.Scan(&col.Table, &col.Name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("userids: audit schema: %w", err)
		}
		if !listed[col.String()] && !auditIgnored[col.String()] {
			candidates = append(candidates, col)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("userids: audit schema: %w", err)
	}

	legacy, _ := scope(nil).legacyIDs()
	var out []Leftover
	for _, col := range candidates {
		var n int64
		if err := q.QueryRowContext(ctx, fmt.Sprintf(
			`SELECT count(*) FROM %s WHERE %s IN (%s)`, quoteIdent(col.Table), quoteIdent(col.Name), legacy),
		).Scan(&n); err != nil {
			return nil, fmt.Errorf("userids: audit %s: %w", col, err)
		}
		if n > 0 {
			out = append(out, Leftover{Table: col.Table, Column: col.Name, Rows: n})
		}
	}
	return out, nil
}

// quoteIdent protège un nom de table ou de colonne lu dans le catalogue.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// existingColumns renvoie les colonnes "table.colonne" de Columns présentes
// dans le schéma courant (une base en retard d'une migration ne fait pas
// échouer la renumérotation : la colonne absente est signalée, pas réécrite).
func existingColumns(ctx context.Context, q *dbx.DB) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT table_name, column_name FROM information_schema.columns
		WHERE table_schema = current_schema()`)
	if err != nil {
		return nil, fmt.Errorf("userids: schema: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var t, c string
		if err := rows.Scan(&t, &c); err != nil {
			return nil, fmt.Errorf("userids: schema: %w", err)
		}
		out[t+"."+c] = true
	}
	return out, rows.Err()
}
