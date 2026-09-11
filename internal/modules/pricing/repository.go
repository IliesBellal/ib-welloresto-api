package pricing

import (
	"context"
	"database/sql"
	"errors"

	"welloresto-api/internal/database/dbx"
)

// ErrPackageNameNotFound is returned when a plan's package_name (pricing_catalog)
// does not match any row in packages — a data-consistency problem between
// the two tables, never expected in normal operation.
var ErrPackageNameNotFound = errors.New("package_name_not_found")

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// LoadCatalog reads the whole pricing_catalog table, bucketed by kind.
func (r *Repository) LoadCatalog(ctx context.Context) (*Catalog, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
		SELECT kind, code, package_name, label, monthly_price_cents, annual_price_cents,
		       included_modules_count, per_unit_price_cents, per_unit_label
		FROM pricing_catalog
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	catalog := &Catalog{
		Plans:   map[string]CatalogPlan{},
		Modules: map[string]CatalogModule{},
		Addons:  map[string]CatalogAddon{},
	}
	for rows.Next() {
		var kind, code, label string
		var packageName sql.NullString
		var monthlyCents int
		var annualCents, includedModulesCount, perUnitPriceCents sql.NullInt64
		var perUnitLabel sql.NullString
		if err := rows.Scan(&kind, &code, &packageName, &label, &monthlyCents, &annualCents,
			&includedModulesCount, &perUnitPriceCents, &perUnitLabel); err != nil {
			return nil, err
		}
		switch kind {
		case "plan":
			plan := CatalogPlan{
				Code:              code,
				PackageName:       packageName.String,
				MonthlyPriceCents: monthlyCents,
				AnnualPriceCents:  int(annualCents.Int64),
			}
			if includedModulesCount.Valid {
				n := int(includedModulesCount.Int64)
				plan.IncludedModulesCount = &n
			}
			catalog.Plans[code] = plan
		case "module":
			mod := CatalogModule{Code: code, Label: label, MonthlyPriceCents: monthlyCents}
			if perUnitPriceCents.Valid {
				n := int(perUnitPriceCents.Int64)
				mod.PerUnitPriceCents = &n
				mod.PerUnitLabel = perUnitLabel.String
			}
			catalog.Modules[code] = mod
		case "addon":
			catalog.Addons[code] = CatalogAddon{Code: code, Label: label, MonthlyPriceCents: monthlyCents}
		}
	}
	return catalog, rows.Err()
}

// GetPackageIDByName resolves a pricing_catalog plan's package_name to the
// real packages.id, dynamically rather than a hardcoded literal — auto-
// generated ids are not guaranteed identical across environments.
func (r *Repository) GetPackageIDByName(ctx context.Context, packageName string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	castExpr := "CAST(id AS CHAR)"
	if dbx.ActiveDialect() == dbx.Postgres {
		castExpr = "CAST(id AS TEXT)"
	}

	var id string
	err := db.QueryRowContext(ctx, `SELECT `+castExpr+` FROM packages WHERE package_name = ?`, packageName).Scan(&id)
	if err == sql.ErrNoRows {
		return "", ErrPackageNameNotFound
	}
	return id, err
}
