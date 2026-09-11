//go:build postgres_integration

package pricing_test

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/modules/pricing"
)

// TestResolveCheapestPlan_Postgres covers LOT A Semaine 3, Chantier 11b's
// real formula (docs/WelloResto-Parcours-Client-v2.docx §4.4, found after
// this chantier had shipped a different, incorrect formula based on a
// verbal simplification — see docs/decisions.md for that history):
//
//	essentiel = 79 + Σ(modules) + planning(29 + 2.50×employees) + 25×extra_pos
//	pro       = 129 + Σ(non-planning modules beyond the 2 priciest) + max(0, 2.50×(employees-10)) si planning + 25×extra_pos
//	complet   = 189 + max(0, 2.50×(employees-10)) si planning + 25×extra_pos
//	argmin ; strict tie -> the higher plan
//
// Planning is never one of pro's "2 modules au choix" (§1.3: pro includes
// planning up to 10 employees as a separate, always-partial inclusion).
func TestResolveCheapestPlan_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	repo := pricing.NewRepository(db)
	svc := pricing.NewService(repo)

	t.Run("no modules -> essentiel", func(t *testing.T) {
		q, err := svc.ResolveCheapestPlan(ctx, pricing.Cart{})
		if err != nil {
			t.Fatalf("ResolveCheapestPlan: %v", err)
		}
		if q.PlanCode != pricing.PlanEssentiel {
			t.Fatalf("plan = %q, want %q", q.PlanCode, pricing.PlanEssentiel)
		}
		if q.MonthlyTotalCents != 7900 {
			t.Fatalf("total = %d, want 7900", q.MonthlyTotalCents)
		}
		wantPkgID, err := repo.GetPackageIDByName(ctx, "Essentiel")
		if err != nil {
			t.Fatalf("GetPackageIDByName(Essentiel): %v", err)
		}
		if q.PackageID != wantPkgID {
			t.Fatalf("package_id = %q, want %q", q.PackageID, wantPkgID)
		}
	})

	t.Run("three modules -> pro cheaper than essentiel and complet", func(t *testing.T) {
		// haccp(35)+marketplaces(39)+delivery(39) = 113.
		// essentiel = 79+113 = 192.
		// pro = 129 + (billed beyond the 2 priciest [39,39] -> 35) = 164.
		// complet = 189.
		// pro (164) wins.
		q, err := svc.ResolveCheapestPlan(ctx, pricing.Cart{Modules: []string{"haccp", "marketplaces", "delivery"}})
		if err != nil {
			t.Fatalf("ResolveCheapestPlan: %v", err)
		}
		if q.PlanCode != pricing.PlanPro {
			t.Fatalf("plan = %q, want %q", q.PlanCode, pricing.PlanPro)
		}
		if q.MonthlyTotalCents != 16400 {
			t.Fatalf("total = %d, want 16400", q.MonthlyTotalCents)
		}
	})

	t.Run("all five modules, no employees -> complet cheaper than pro", func(t *testing.T) {
		// non-planning sum = 59+35+39+39 = 172 ; pro = 129 + (billed beyond
		// top 2 [59,39] -> 39+35=74) = 203 ; complet = 189 (planning
		// overage 0 at zero employees). Complet wins.
		q, err := svc.ResolveCheapestPlan(ctx, pricing.Cart{
			Modules: []string{"reservation", "haccp", "planning", "marketplaces", "delivery"},
		})
		if err != nil {
			t.Fatalf("ResolveCheapestPlan: %v", err)
		}
		if q.PlanCode != pricing.PlanComplet {
			t.Fatalf("plan = %q, want %q", q.PlanCode, pricing.PlanComplet)
		}
		if q.MonthlyTotalCents != 18900 {
			t.Fatalf("total = %d, want 18900", q.MonthlyTotalCents)
		}
	})

	t.Run("planning alone above 10 employees: pro's free threshold beats essentiel's flat per-employee rate", func(t *testing.T) {
		// essentiel = 79 + (29 + 2.50*15 = 66.50) = 145.50 -> 14550.
		// pro = 129 + max(0, 2.50*(15-10)=12.50) = 141.50 -> 14150.
		// complet = 189 + 12.50 = 201.50 -> 20150.
		// pro wins — proves the >10-employee threshold that essentiel lacks.
		q, err := svc.ResolveCheapestPlan(ctx, pricing.Cart{Modules: []string{"planning"}, Employees: 15})
		if err != nil {
			t.Fatalf("ResolveCheapestPlan: %v", err)
		}
		if q.PlanCode != pricing.PlanPro {
			t.Fatalf("plan = %q, want %q", q.PlanCode, pricing.PlanPro)
		}
		if q.MonthlyTotalCents != 14150 {
			t.Fatalf("total = %d, want 14150", q.MonthlyTotalCents)
		}
	})

	t.Run("planning never competes for pro's 2 free non-planning slots", func(t *testing.T) {
		// reservation+haccp both fit pro's 2 free slots ; planning (5
		// employees, under the free-10 threshold) costs nothing extra on
		// pro either. If planning wrongly competed for a slot, one of the
		// three would be billed and pro's total would exceed 12900.
		q, err := svc.ResolveCheapestPlan(ctx, pricing.Cart{
			Modules: []string{"reservation", "haccp", "planning"}, Employees: 5,
		})
		if err != nil {
			t.Fatalf("ResolveCheapestPlan: %v", err)
		}
		if q.PlanCode != pricing.PlanPro {
			t.Fatalf("plan = %q, want %q", q.PlanCode, pricing.PlanPro)
		}
		if q.MonthlyTotalCents != 12900 {
			t.Fatalf("total = %d, want 12900 (base only — planning didn't take a free slot from reservation/haccp)", q.MonthlyTotalCents)
		}
	})

	t.Run("extra_pos adds identically to every plan, never changing the choice", func(t *testing.T) {
		q, err := svc.ResolveCheapestPlan(ctx, pricing.Cart{ExtraPOS: 2})
		if err != nil {
			t.Fatalf("ResolveCheapestPlan: %v", err)
		}
		if q.PlanCode != pricing.PlanEssentiel {
			t.Fatalf("plan = %q, want %q", q.PlanCode, pricing.PlanEssentiel)
		}
		if q.MonthlyTotalCents != 7900+5000 {
			t.Fatalf("total = %d, want %d", q.MonthlyTotalCents, 7900+5000)
		}
	})

	t.Run("annual billing uses the discounted plan rate", func(t *testing.T) {
		q, err := svc.ResolveCheapestPlan(ctx, pricing.Cart{BillingCycle: "annual"})
		if err != nil {
			t.Fatalf("ResolveCheapestPlan: %v", err)
		}
		if q.MonthlyTotalCents != 6600 {
			t.Fatalf("total = %d, want 6600 (essentiel annual rate)", q.MonthlyTotalCents)
		}
	})

	t.Run("unknown module code errors", func(t *testing.T) {
		if _, err := svc.ResolveCheapestPlan(ctx, pricing.Cart{Modules: []string{"does-not-exist"}}); err == nil {
			t.Fatal("ResolveCheapestPlan with an unknown module code: expected an error, got nil")
		}
	})
}
