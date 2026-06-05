package performance

import (
	"context"
	"math"
	"sort"
	"time"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetPerformanceByDay(ctx context.Context, fromLocalDay, toLocalDay time.Time) (*PerformanceResponse, error) {
	raw, err := s.GetRawPerformanceByDay(ctx, fromLocalDay, toLocalDay)
	if err != nil {
		return nil, err
	}

	periods := make([]PerformancePeriod, 0, len(raw.Days))
	membersWithoutRate := map[string]struct{}{}

	totalRevenueCents := int64(0)
	totalPlannedHours := 0.0
	totalWorkedHours := 0.0
	totalHeadcount := 0
	totalPayrollCents := int64(0)

	for _, day := range raw.Days {
		plannedHours := 0.0
		workedHours := 0.0
		payrollRaw := 0.0

		for _, employee := range day.Employees {
			employeePlannedHours := float64(employee.PlannedMinutes) / 60.0
			employeeWorkedRawHours := float64(employee.WorkedSeconds) / 3600.0

			workedDisplayHours := employeeWorkedRawHours
			if employee.WorkedSeconds <= 0 {
				workedDisplayHours = employeePlannedHours
			}

			plannedHours += employeePlannedHours
			workedHours += workedDisplayHours

			if employee.HourlyRateCents <= 0 {
				if workedDisplayHours > 0 {
					membersWithoutRate[employee.EmployeeID] = struct{}{}
				}
				continue
			}

			payrollRaw += workedDisplayHours * float64(employee.HourlyRateCents) * (1.0 + employee.EmployerChargesPct/100.0)
		}

		payrollCents := int64(math.Round(payrollRaw))
		hoursDelta := workedHours - plannedHours

		var payrollRatio *float64
		var revenuePerHourCents *float64
		if day.RevenueHTCents > 0 {
			ratio := float64(payrollCents) / float64(day.RevenueHTCents)
			payrollRatio = &ratio

			if workedHours > 0 {
				revPerHour := float64(day.RevenueHTCents) / workedHours
				revenuePerHourCents = &revPerHour
			}
		}

		period := PerformancePeriod{
			PeriodStart:            day.LocalDay,
			PeriodEnd:              day.LocalDay,
			Label:                  day.LocalDay,
			RevenueActualCents:     day.RevenueHTCents,
			RevenueForecastCents:   nil,
			PlannedHours:           plannedHours,
			WorkedHours:            workedHours,
			Headcount:              day.Headcount,
			PayrollCostLoadedCents: payrollCents,
			PayrollRatio:           payrollRatio,
			RevenuePerHourCents:    revenuePerHourCents,
			HoursDelta:             hoursDelta,
		}
		periods = append(periods, period)

		totalRevenueCents += day.RevenueHTCents
		totalPlannedHours += plannedHours
		totalWorkedHours += workedHours
		totalHeadcount += day.Headcount
		totalPayrollCents += payrollCents
	}

	var totalPayrollRatio *float64
	if totalRevenueCents > 0 {
		ratio := float64(totalPayrollCents) / float64(totalRevenueCents)
		totalPayrollRatio = &ratio
	}

	var totalRevenuePerHour *float64
	if totalRevenueCents > 0 && totalWorkedHours > 0 {
		value := float64(totalRevenueCents) / totalWorkedHours
		totalRevenuePerHour = &value
	}

	totals := PerformancePeriod{
		PeriodStart:            raw.FromLocalDay,
		PeriodEnd:              raw.ToLocalDay,
		Label:                  "Total",
		RevenueActualCents:     totalRevenueCents,
		RevenueForecastCents:   nil,
		PlannedHours:           totalPlannedHours,
		WorkedHours:            totalWorkedHours,
		Headcount:              totalHeadcount,
		PayrollCostLoadedCents: totalPayrollCents,
		PayrollRatio:           totalPayrollRatio,
		RevenuePerHourCents:    totalRevenuePerHour,
		HoursDelta:             totalWorkedHours - totalPlannedHours,
	}

	return &PerformanceResponse{
		From:           raw.FromLocalDay,
		To:             raw.ToLocalDay,
		Granularity:    "day",
		Periods:        periods,
		Totals:         totals,
		PreviousPeriod: nil,
		Warnings: PerformanceWarnings{
			MembersWithoutRate: len(membersWithoutRate),
		},
	}, nil
}

func (s *Service) GetRawPerformanceByDay(ctx context.Context, fromLocalDay, toLocalDay time.Time) (*RawPerformanceResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if toLocalDay.Before(fromLocalDay) {
		return nil, models.ErrPlanningInvalidDate
	}

	plannedRows, err := s.repo.ListPlannedByDayEmployee(ctx, user.MerchantID, fromLocalDay, toLocalDay)
	if err != nil {
		return nil, err
	}
	workedRows, err := s.repo.ListWorkedRawByDayEmployee(ctx, user.MerchantID, fromLocalDay, toLocalDay)
	if err != nil {
		return nil, err
	}
	revenueRows, err := s.repo.ListRevenueByDay(ctx, user.MerchantID, fromLocalDay, toLocalDay)
	if err != nil {
		return nil, err
	}
	rateRows, err := s.repo.ListRatesByEmployee(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}

	days := buildRawDaySkeleton(fromLocalDay, toLocalDay)
	rateByEmployee := buildRateIndex(rateRows)
	headcountSets := map[string]map[string]struct{}{}

	for _, row := range plannedRows {
		day := ensureDay(days, row.LocalDay)
		employee := ensureEmployee(day, row.EmployeeID)
		employee.PlannedMinutes += row.PlannedMinutes
		employee.PlannedHours = float64(employee.PlannedMinutes) / 60.0

		if _, ok := headcountSets[row.LocalDay]; !ok {
			headcountSets[row.LocalDay] = map[string]struct{}{}
		}
		headcountSets[row.LocalDay][row.EmployeeID] = struct{}{}
	}

	for _, row := range workedRows {
		day := ensureDay(days, row.LocalDay)
		employee := ensureEmployee(day, row.EmployeeID)
		employee.WorkedSeconds += row.WorkedSeconds
		employee.WorkedHours = float64(employee.WorkedSeconds) / 3600.0
	}

	for _, row := range revenueRows {
		day := ensureDay(days, row.LocalDay)
		day.RevenueHTCents = row.RevenueHTCents
	}

	for localDay, day := range days {
		for employeeID, employee := range day.employeeIndex {
			if rate, ok := rateByEmployee[employeeID]; ok {
				employee.HourlyRateCents = rate.HourlyRateCents
				employee.EmployerChargesPct = rate.EmployerChargesPct
			}
		}
		if set, ok := headcountSets[localDay]; ok {
			day.Headcount = len(set)
		}
		sort.Slice(day.Employees, func(i, j int) bool {
			return day.Employees[i].EmployeeID < day.Employees[j].EmployeeID
		})
	}

	orderedDays := make([]RawDayMetrics, 0, len(days))
	for d := normalizeDateOnlyUTC(fromLocalDay); !d.After(normalizeDateOnlyUTC(toLocalDay)); d = d.Add(24 * time.Hour) {
		key := d.Format("2006-01-02")
		orderedDays = append(orderedDays, days[key].RawDayMetrics)
	}

	response := &RawPerformanceResponse{
		FromLocalDay: normalizeDateOnlyUTC(fromLocalDay).Format("2006-01-02"),
		ToLocalDay:   normalizeDateOnlyUTC(toLocalDay).Format("2006-01-02"),
		GeneratedAt:  time.Now().UTC(),
		Days:         orderedDays,
	}
	return response, nil
}

type rawDayContainer struct {
	RawDayMetrics
	employeeIndex map[string]*RawDayEmployeeMetrics
}

func buildRawDaySkeleton(fromLocalDay, toLocalDay time.Time) map[string]*rawDayContainer {
	result := map[string]*rawDayContainer{}
	for d := normalizeDateOnlyUTC(fromLocalDay); !d.After(normalizeDateOnlyUTC(toLocalDay)); d = d.Add(24 * time.Hour) {
		key := d.Format("2006-01-02")
		result[key] = &rawDayContainer{
			RawDayMetrics: RawDayMetrics{
				LocalDay:       key,
				RevenueHTCents: 0,
				Headcount:      0,
				Employees:      make([]RawDayEmployeeMetrics, 0),
			},
			employeeIndex: map[string]*RawDayEmployeeMetrics{},
		}
	}
	return result
}

func ensureDay(days map[string]*rawDayContainer, localDay string) *rawDayContainer {
	if day, ok := days[localDay]; ok {
		return day
	}
	day := &rawDayContainer{
		RawDayMetrics: RawDayMetrics{
			LocalDay:       localDay,
			RevenueHTCents: 0,
			Headcount:      0,
			Employees:      make([]RawDayEmployeeMetrics, 0),
		},
		employeeIndex: map[string]*RawDayEmployeeMetrics{},
	}
	days[localDay] = day
	return day
}

func ensureEmployee(day *rawDayContainer, employeeID string) *RawDayEmployeeMetrics {
	if item, ok := day.employeeIndex[employeeID]; ok {
		return item
	}
	day.Employees = append(day.Employees, RawDayEmployeeMetrics{EmployeeID: employeeID})
	ptr := &day.Employees[len(day.Employees)-1]
	day.employeeIndex[employeeID] = ptr
	return ptr
}

func buildRateIndex(rows []EmployeeRateRow) map[string]EmployeeRateRow {
	index := make(map[string]EmployeeRateRow, len(rows))
	for _, row := range rows {
		index[row.EmployeeID] = row
	}
	return index
}
