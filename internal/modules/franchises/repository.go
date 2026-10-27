package franchises

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/utils/dbutils"
)

// merchantAdminCondition : l'utilisateur (ur) est administrateur de
// l'établissement, même règle que auth.UserLoginRow.HasAdminRole : rôle
// système admin, ou drapeau historique admin quand aucun rôle n'est attribué.
const merchantAdminCondition = `ur.enabled = TRUE AND ur.login_enabled = TRUE
	AND ((ur.role_id IS NOT NULL AND rl.system_key = 'admin') OR (ur.role_id IS NULL AND ur.admin = TRUE))`

// merchantSNOActive : ScanNOrder activé, non coupé par l'abonnement et QR
// principal existant — les conditions pour figurer sur le hub (mêmes règles que
// scannorder.snoSubscriptionBlocked / snoMainQRCondition et le sitemap).
const merchantSNOActive = `(COALESCE(snos.activated, FALSE)
	AND NOT EXISTS (SELECT 1 FROM subscriptions sub WHERE sub.merchant_id = CAST(m.id AS TEXT)
		AND (sub.status IN ('suspended', 'canceled') OR sub.scannorder_enabled = FALSE))
	AND EXISTS (SELECT 1 FROM qrcodes qr WHERE qr.merchant_id = CAST(m.id AS TEXT)
		AND qr.location_id IS NULL AND qr.user_id IS NULL AND qr.deleted = FALSE AND qr.enabled = TRUE))`

// merchantReservationEnabled : réservation en ligne ouverte (réglages activés,
// module compris dans l'abonnement, abonnement ni suspendu ni résilié).
const merchantReservationEnabled = `EXISTS (SELECT 1 FROM bookings_settings bs
	LEFT JOIN subscriptions sub ON sub.merchant_id = bs.merchant_id
	LEFT JOIN packages p ON p.id = sub.package_id
	WHERE bs.merchant_id = CAST(m.id AS TEXT) AND bs.enabled = TRUE
		AND COALESCE(sub.bookings_enabled, p.bookings_enabled, TRUE)
		AND (sub.status IS NULL OR sub.status NOT IN ('suspended', 'canceled')))`

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) RunInTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	return dbutils.RunInTx(ctx, r.db, fn)
}

// --- Franchises ---

func (r *Repository) GetFranchise(ctx context.Context, franchiseID string) (*Franchise, error) {
	db := dbx.GetDB(ctx, r.db)
	f := &Franchise{}
	err := db.QueryRowContext(ctx, `
		SELECT franchise_id, name, slug, description, logo_url, banner_url, brand_color, creation_date,
		       announcement_enabled, announcement_text, announcement_url, to_char(announcement_ends_on, 'YYYY-MM-DD')
		FROM franchises
		WHERE franchise_id = ?`, franchiseID).Scan(
		&f.FranchiseID, &f.Name, &f.Slug, &f.Description, &f.LogoURL, &f.BannerURL, &f.BrandColor, &f.CreatedAt,
		&f.Announcement.Enabled, &f.Announcement.Text, &f.Announcement.URL, &f.Announcement.EndsOn,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (r *Repository) SlugTaken(ctx context.Context, slug, excludeFranchiseID string) (bool, error) {
	db := dbx.GetDB(ctx, r.db)
	var taken bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM franchises WHERE slug = ? AND franchise_id <> ?)`,
		slug, excludeFranchiseID).Scan(&taken)
	return taken, err
}

func (r *Repository) InsertFranchise(ctx context.Context, f *Franchise) error {
	db := dbx.GetDB(ctx, r.db)
	_, err := db.ExecContext(ctx, `
		INSERT INTO franchises (franchise_id, name, slug, description, logo_url, banner_url, brand_color, creation_date)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		f.FranchiseID, f.Name, f.Slug, f.Description, f.LogoURL, f.BannerURL, f.BrandColor, f.CreatedAt)
	return err
}

func (r *Repository) UpdateFranchise(ctx context.Context, f *Franchise) error {
	db := dbx.GetDB(ctx, r.db)
	_, err := db.ExecContext(ctx, `
		UPDATE franchises
		SET name = ?, slug = ?, description = ?, brand_color = ?,
		    announcement_enabled = ?, announcement_text = ?, announcement_url = ?, announcement_ends_on = CAST(? AS date),
		    updated_at = ?
		WHERE franchise_id = ?`,
		f.Name, f.Slug, f.Description, f.BrandColor,
		f.Announcement.Enabled, f.Announcement.Text, f.Announcement.URL, f.Announcement.EndsOn,
		time.Now().UTC(), f.FranchiseID)
	return err
}

// SetImageURL écrit logo_url ou banner_url.
func (r *Repository) SetImageURL(ctx context.Context, franchiseID, column, url string) error {
	if column != "logo_url" && column != "banner_url" {
		return fmt.Errorf("franchises: invalid image column %q", column)
	}
	db := dbx.GetDB(ctx, r.db)
	_, err := db.ExecContext(ctx,
		`UPDATE franchises SET `+column+` = ?, updated_at = ? WHERE franchise_id = ?`,
		url, time.Now().UTC(), franchiseID)
	return err
}

// DeleteFranchise détache les établissements, retire les gestionnaires et
// supprime la franchise. À appeler dans une transaction.
func (r *Repository) DeleteFranchise(ctx context.Context, franchiseID string) error {
	db := dbx.GetDB(ctx, r.db)
	if _, err := db.ExecContext(ctx, `
		UPDATE merchant SET franchise_id = NULL, franchise_hub_visible = TRUE
		WHERE franchise_id = ?`, franchiseID); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM franchise_members WHERE franchise_id = ?`, franchiseID); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `DELETE FROM franchises WHERE franchise_id = ?`, franchiseID)
	return err
}

// ListUserFranchises : franchises dont l'utilisateur est gestionnaire. Avec
// allFranchises (staff WelloResto), toutes les franchises, rôle owner quand il
// n'en est pas membre.
func (r *Repository) ListUserFranchises(ctx context.Context, userID string, allFranchises bool) ([]FranchiseSummary, error) {
	db := dbx.GetDB(ctx, r.db)
	join := "INNER JOIN"
	if allFranchises {
		join = "LEFT JOIN"
	}
	rows, err := db.QueryContext(ctx, `
		SELECT f.franchise_id, f.name, f.slug, f.logo_url, COALESCE(fm.role, '`+RoleOwner+`'),
			(SELECT COUNT(*) FROM merchant m WHERE m.franchise_id = f.franchise_id)
		FROM franchises f
		`+join+` franchise_members fm ON fm.franchise_id = f.franchise_id AND fm.user_id = ?
		ORDER BY f.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []FranchiseSummary{}
	for rows.Next() {
		var s FranchiseSummary
		if err := rows.Scan(&s.FranchiseID, &s.Name, &s.Slug, &s.LogoURL, &s.Role, &s.MerchantCount); err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

// --- Gestionnaires ---

// GetMemberRole : rôle de l'utilisateur dans la franchise, "" s'il n'en est
// pas gestionnaire.
func (r *Repository) GetMemberRole(ctx context.Context, franchiseID, userID string) (string, error) {
	db := dbx.GetDB(ctx, r.db)
	var role string
	err := db.QueryRowContext(ctx, `
		SELECT role FROM franchise_members WHERE franchise_id = ? AND user_id = ?`,
		franchiseID, userID).Scan(&role)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return role, err
}

func (r *Repository) ListMembers(ctx context.Context, franchiseID string) ([]Member, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT fm.user_id, COALESCE(u.first_name, ''), COALESCE(u.last_name, ''), COALESCE(u.email, ''), fm.role, fm.created_at
		FROM franchise_members fm
		LEFT JOIN users u ON u.user_id = fm.user_id
		WHERE fm.franchise_id = ?
		ORDER BY CASE fm.role WHEN '`+RoleOwner+`' THEN 0 ELSE 1 END, u.last_name, u.first_name`, franchiseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.FirstName, &m.LastName, &m.Email, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (r *Repository) CountOwners(ctx context.Context, franchiseID string) (int, error) {
	db := dbx.GetDB(ctx, r.db)
	var n int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM franchise_members WHERE franchise_id = ? AND role = ?`,
		franchiseID, RoleOwner).Scan(&n)
	return n, err
}

func (r *Repository) InsertMember(ctx context.Context, franchiseID, userID, role string) error {
	db := dbx.GetDB(ctx, r.db)
	_, err := db.ExecContext(ctx, `
		INSERT INTO franchise_members (franchise_id, user_id, role, created_at)
		VALUES (?, ?, ?, ?)`, franchiseID, userID, role, time.Now().UTC())
	return err
}

func (r *Repository) UpdateMemberRole(ctx context.Context, franchiseID, userID, role string) error {
	db := dbx.GetDB(ctx, r.db)
	_, err := db.ExecContext(ctx, `
		UPDATE franchise_members SET role = ?, updated_at = ?
		WHERE franchise_id = ? AND user_id = ?`, role, time.Now().UTC(), franchiseID, userID)
	return err
}

func (r *Repository) DeleteMember(ctx context.Context, franchiseID, userID string) error {
	db := dbx.GetDB(ctx, r.db)
	_, err := db.ExecContext(ctx, `
		DELETE FROM franchise_members WHERE franchise_id = ? AND user_id = ?`, franchiseID, userID)
	return err
}

// FindUserIDByEmail : compte actif portant cet email, "" s'il n'y en a pas.
func (r *Repository) FindUserIDByEmail(ctx context.Context, email string) (string, error) {
	db := dbx.GetDB(ctx, r.db)
	var userID string
	err := db.QueryRowContext(ctx, `
		SELECT user_id FROM users
		WHERE LOWER(email) = LOWER(?) AND enabled = TRUE
		ORDER BY creationdate
		LIMIT 1`, strings.TrimSpace(email)).Scan(&userID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return userID, err
}

// --- Établissements ---

func (r *Repository) IsAdminOfAnyMerchant(ctx context.Context, userID string) (bool, error) {
	db := dbx.GetDB(ctx, r.db)
	var ok bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users_rights ur
			LEFT JOIN roles rl ON rl.id = ur.role_id
			WHERE ur.user_id = ? AND `+merchantAdminCondition+`
		)`, userID).Scan(&ok)
	return ok, err
}

func (r *Repository) IsMerchantAdmin(ctx context.Context, userID, merchantID string) (bool, error) {
	db := dbx.GetDB(ctx, r.db)
	var ok bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users_rights ur
			LEFT JOIN roles rl ON rl.id = ur.role_id
			WHERE ur.user_id = ? AND ur.merchant_id = ? AND `+merchantAdminCondition+`
		)`, userID, merchantID).Scan(&ok)
	return ok, err
}

// ListAdminMerchants : établissements dont l'utilisateur est administrateur,
// avec leur franchise actuelle.
func (r *Repository) ListAdminMerchants(ctx context.Context, userID string) ([]CandidateMerchant, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT CAST(m.id AS TEXT), m.fullName, m.address, m.city, m.franchise_id, f.name
		FROM users_rights ur
		LEFT JOIN roles rl ON rl.id = ur.role_id
		INNER JOIN merchant m ON CAST(m.id AS TEXT) = ur.merchant_id
		LEFT JOIN franchises f ON f.franchise_id = m.franchise_id
		WHERE ur.user_id = ? AND `+merchantAdminCondition+`
		ORDER BY m.fullName`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []CandidateMerchant{}
	for rows.Next() {
		var c CandidateMerchant
		if err := rows.Scan(&c.MerchantID, &c.BusinessName, &c.Address, &c.City, &c.FranchiseID, &c.FranchiseName); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

func (r *Repository) ListFranchiseMerchants(ctx context.Context, franchiseID string) ([]FranchiseMerchant, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT CAST(m.id AS TEXT), m.fullName, m.address, m.zip_code, m.city,
			COALESCE(snos.logo_url, m.logo_url), m.franchise_hub_visible,
			`+merchantSNOActive+`,
			COALESCE(snos.take_away_enabled, FALSE), COALESCE(snos.delivery_enabled, FALSE), COALESCE(snos.in_enabled, FALSE),
			`+merchantReservationEnabled+`
		FROM merchant m
		LEFT JOIN scannorder_settings snos ON snos.merchant_id = CAST(m.id AS TEXT)
		WHERE m.franchise_id = ?
		ORDER BY m.fullName`, franchiseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []FranchiseMerchant{}
	for rows.Next() {
		var m FranchiseMerchant
		if err := rows.Scan(
			&m.MerchantID, &m.BusinessName, &m.Address, &m.ZipCode, &m.City,
			&m.LogoURL, &m.HubVisible, &m.ScanNOrderActive,
			&m.OrderTypes.TakeAway, &m.OrderTypes.Delivery, &m.OrderTypes.In,
			&m.ReservationEnabled,
		); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

// MerchantIDsOf : identifiants des établissements de la franchise (purge des
// caches merchant à la suppression).
func (r *Repository) MerchantIDsOf(ctx context.Context, franchiseID string) ([]string, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT CAST(id AS TEXT) FROM merchant WHERE franchise_id = ?`, franchiseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AttachMerchant rattache l'établissement s'il n'est dans aucune franchise (ou
// déjà dans celle-ci). attached = false : il appartient à une autre franchise,
// ou n'existe pas.
func (r *Repository) AttachMerchant(ctx context.Context, franchiseID, merchantID string) (attached bool, err error) {
	db := dbx.GetDB(ctx, r.db)
	res, err := db.ExecContext(ctx, `
		UPDATE merchant
		SET franchise_id = ?, franchise_hub_visible = CASE WHEN franchise_id = ? THEN franchise_hub_visible ELSE TRUE END
		WHERE CAST(id AS TEXT) = ? AND (franchise_id IS NULL OR franchise_id = ?)`,
		franchiseID, franchiseID, merchantID, franchiseID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DetachMerchant retire l'établissement de la franchise. sql.ErrNoRows s'il
// n'en fait pas partie.
func (r *Repository) DetachMerchant(ctx context.Context, franchiseID, merchantID string) error {
	db := dbx.GetDB(ctx, r.db)
	res, err := db.ExecContext(ctx, `
		UPDATE merchant SET franchise_id = NULL, franchise_hub_visible = TRUE
		WHERE CAST(id AS TEXT) = ? AND franchise_id = ?`, merchantID, franchiseID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetMerchantHubVisible : sql.ErrNoRows si l'établissement n'est pas dans la
// franchise.
func (r *Repository) SetMerchantHubVisible(ctx context.Context, franchiseID, merchantID string, visible bool) error {
	db := dbx.GetDB(ctx, r.db)
	res, err := db.ExecContext(ctx, `
		UPDATE merchant SET franchise_hub_visible = ?
		WHERE CAST(id AS TEXT) = ? AND franchise_id = ?`, visible, merchantID, franchiseID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// MerchantFranchiseID : franchise actuelle de l'établissement (nil si aucune),
// found = false si l'établissement n'existe pas.
func (r *Repository) MerchantFranchiseID(ctx context.Context, merchantID string) (franchiseID *string, found bool, err error) {
	db := dbx.GetDB(ctx, r.db)
	err = db.QueryRowContext(ctx, `
		SELECT franchise_id FROM merchant WHERE CAST(id AS TEXT) = ?`, merchantID).Scan(&franchiseID)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return franchiseID, true, nil
}
