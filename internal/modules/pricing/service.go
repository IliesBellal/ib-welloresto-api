package pricing

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// GetPackageIDForPlan resolves a plan_code (essentiel/pro/complet) — as
// decoded from a signup-context token's claims — to the real packages.id.
// Used by signup.Service.resolveContext so /v1/signup never needs to
// re-derive a Quote (and re-run ResolveCheapestPlan) just to find the
// package a previously-resolved plan_code points to.
func (s *Service) GetPackageIDForPlan(ctx context.Context, planCode string) (string, error) {
	catalog, err := s.repo.LoadCatalog(ctx)
	if err != nil {
		return "", fmt.Errorf("GetPackageIDForPlan: load catalog: %w", err)
	}
	plan, ok := catalog.Plans[planCode]
	if !ok {
		return "", fmt.Errorf("GetPackageIDForPlan: unknown plan_code %q", planCode)
	}
	return s.repo.GetPackageIDByName(ctx, plan.PackageName)
}

type moduleCost struct {
	code  string
	cents int
}

// ResolveCheapestPlan implements §4.4's exact formula (found in
// docs/WelloResto-Parcours-Client-v2.docx after this chantier had already
// shipped a different, incorrect one based on a verbal simplification —
// see docs/decisions.md for that history):
//
//	coût_à_la_carte   = 79 + Σ(modules cochés) + planning éventuel (29 + 2,50 × salariés) + 25 × postes_supplémentaires
//	coût_pack_pro     = 129 + Σ(modules au-delà des 2 inclus) + max(0 ; 2,50 × (salariés − 10)) si planning + 25 × postes_supplémentaires
//	coût_pack_complet = 189 + max(0 ; 2,50 × (salariés − 10)) si planning + 25 × postes_supplémentaires
//	plan_retenu = argmin ; égalité stricte → pack supérieur
//
// Two things not spelled out by the formula itself, resolved from §1.3's
// plan descriptions: planning is never one of Pro's "2 modules au choix" —
// it is always partially included on Pro/Complet (up to
// planningFreeEmployeesOnPlans employees), a separate line from the 2-free
// slots that only reservation/haccp/marketplaces/delivery compete for. And
// on Pro, the 2 free slots go to the 2 most expensive requested modules —
// not specified either way by §4.4, but the only choice consistent with
// picking the cheapest plan at all.
func (s *Service) ResolveCheapestPlan(ctx context.Context, cart Cart) (Quote, error) {
	catalog, err := s.repo.LoadCatalog(ctx)
	if err != nil {
		return Quote{}, fmt.Errorf("ResolveCheapestPlan: load catalog: %w", err)
	}

	billing := strings.TrimSpace(cart.BillingCycle)
	if billing == "" {
		billing = "monthly"
	}

	hasPlanning := false
	nonPlanningCosts := make([]moduleCost, 0, len(cart.Modules))
	for _, code := range cart.Modules {
		if code == planningModuleCode {
			hasPlanning = true
			continue
		}
		mod, ok := catalog.Modules[code]
		if !ok || !nonPlanningModuleCodes[code] {
			return Quote{}, fmt.Errorf("ResolveCheapestPlan: unknown module code %q", code)
		}
		nonPlanningCosts = append(nonPlanningCosts, moduleCost{code: code, cents: mod.MonthlyPriceCents})
	}
	sort.Slice(nonPlanningCosts, func(i, j int) bool { return nonPlanningCosts[i].cents > nonPlanningCosts[j].cents })

	planningBaseCents := 0
	planningPerEmployeeCents := 0
	if hasPlanning {
		mod, ok := catalog.Modules[planningModuleCode]
		if !ok {
			return Quote{}, fmt.Errorf("ResolveCheapestPlan: planning module missing from catalog")
		}
		planningBaseCents = mod.MonthlyPriceCents
		if mod.PerUnitPriceCents != nil {
			planningPerEmployeeCents = *mod.PerUnitPriceCents
		}
	}

	extraPOSCents := 0
	if addon, ok := catalog.Addons["extra_seat"]; ok {
		extraPOSCents = addon.MonthlyPriceCents * cart.ExtraPOS
	}

	essentielPlan, ok := catalog.Plans[PlanEssentiel]
	if !ok {
		return Quote{}, fmt.Errorf("ResolveCheapestPlan: plan %q missing from catalog", PlanEssentiel)
	}
	proPlan, ok := catalog.Plans[PlanPro]
	if !ok {
		return Quote{}, fmt.Errorf("ResolveCheapestPlan: plan %q missing from catalog", PlanPro)
	}
	completPlan, ok := catalog.Plans[PlanComplet]
	if !ok {
		return Quote{}, fmt.Errorf("ResolveCheapestPlan: plan %q missing from catalog", PlanComplet)
	}

	// --- coût_à_la_carte (essentiel) ---
	essentielTotal := planPrice(essentielPlan, billing)
	essentielBreakdown := []BreakdownLine{{Code: PlanEssentiel, Label: essentielPlan.PackageName, AmountCents: planPrice(essentielPlan, billing)}}
	for _, c := range nonPlanningCosts {
		essentielTotal += c.cents
		essentielBreakdown = append(essentielBreakdown, moduleBreakdownLine(catalog, c.code, c.cents))
	}
	if hasPlanning {
		planningCents := planningBaseCents + planningPerEmployeeCents*cart.Employees
		essentielTotal += planningCents
		essentielBreakdown = append(essentielBreakdown, moduleBreakdownLine(catalog, planningModuleCode, planningCents))
	}
	essentielTotal += extraPOSCents

	// --- coût_pack_pro ---
	const proFreeSlots = 2
	proTotal := planPrice(proPlan, billing)
	proBreakdown := []BreakdownLine{{Code: PlanPro, Label: proPlan.PackageName, AmountCents: planPrice(proPlan, billing)}}
	for i, c := range nonPlanningCosts {
		if i < proFreeSlots {
			continue // one of the 2 included modules — free
		}
		proTotal += c.cents
		proBreakdown = append(proBreakdown, moduleBreakdownLine(catalog, c.code, c.cents))
	}
	if hasPlanning {
		surcharge := planningOverageCents(planningPerEmployeeCents, cart.Employees)
		if surcharge > 0 {
			proTotal += surcharge
			proBreakdown = append(proBreakdown, BreakdownLine{Code: "planning_overage", Label: "Planning au-delà de 10 salariés", AmountCents: surcharge})
		}
	}
	proTotal += extraPOSCents

	// --- coût_pack_complet ---
	completTotal := planPrice(completPlan, billing)
	completBreakdown := []BreakdownLine{{Code: PlanComplet, Label: completPlan.PackageName, AmountCents: planPrice(completPlan, billing)}}
	if hasPlanning {
		surcharge := planningOverageCents(planningPerEmployeeCents, cart.Employees)
		if surcharge > 0 {
			completTotal += surcharge
			completBreakdown = append(completBreakdown, BreakdownLine{Code: "planning_overage", Label: "Planning au-delà de 10 salariés", AmountCents: surcharge})
		}
	}
	completTotal += extraPOSCents

	if extraPOSCents > 0 {
		line := BreakdownLine{Code: "extra_pos", Label: "Postes supplémentaires", AmountCents: extraPOSCents}
		essentielBreakdown = append(essentielBreakdown, line)
		proBreakdown = append(proBreakdown, line)
		completBreakdown = append(completBreakdown, line)
	}

	// argmin ; strict tie -> the more inclusive plan (essentiel < pro < complet).
	candidates := []struct {
		code      string
		total     int
		breakdown []BreakdownLine
		rank      int
	}{
		{PlanEssentiel, essentielTotal, essentielBreakdown, 0},
		{PlanPro, proTotal, proBreakdown, 1},
		{PlanComplet, completTotal, completBreakdown, 2},
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.total < best.total || (c.total == best.total && c.rank > best.rank) {
			best = c
		}
	}
	planCode, total, breakdown := best.code, best.total, best.breakdown

	plan := catalog.Plans[planCode]
	packageID, err := s.repo.GetPackageIDByName(ctx, plan.PackageName)
	if err != nil {
		return Quote{}, fmt.Errorf("ResolveCheapestPlan: resolve package_id for %q: %w", plan.PackageName, err)
	}

	return Quote{
		PlanCode:          planCode,
		PackageID:         packageID,
		MonthlyTotalCents: total,
		Breakdown:         breakdown,
	}, nil
}

// planningOverageCents is the >planningFreeEmployeesOnPlans surcharge pro
// and complet bill for planning — max(0 ; 2,50 × (salariés − 10)).
func planningOverageCents(perEmployeeCents, employees int) int {
	overage := employees - planningFreeEmployeesOnPlans
	if overage <= 0 {
		return 0
	}
	return perEmployeeCents * overage
}

func moduleBreakdownLine(catalog *Catalog, code string, cents int) BreakdownLine {
	if mod, ok := catalog.Modules[code]; ok {
		return BreakdownLine{Code: code, Label: mod.Label, AmountCents: cents}
	}
	return BreakdownLine{Code: code, Label: code, AmountCents: cents}
}
