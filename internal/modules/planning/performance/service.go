package performance

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
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
	return s.GetPerformance(ctx, fromLocalDay, toLocalDay, "day", "")
}

func (s *Service) GetPerformance(ctx context.Context, fromLocalDay, toLocalDay time.Time, granularity, compare string) (*PerformanceResponse, error) {
	current, err := s.buildPerformanceForRange(ctx, fromLocalDay, toLocalDay, granularity)
	if err != nil {
		return nil, err
	}

	if strings.EqualFold(strings.TrimSpace(compare), "previous") {
		prevFrom, prevTo := computePreviousRange(fromLocalDay, toLocalDay, granularity)
		previous, err := s.buildPerformanceForRange(ctx, prevFrom, prevTo, granularity)
		if err != nil {
			return nil, err
		}
		current.PreviousPeriod = &PerformancePreviousPeriod{
			From:    previous.From,
			To:      previous.To,
			Periods: previous.Periods,
			Totals:  previous.Totals,
		}
	}

	return current, nil
}

func (s *Service) buildPerformanceForRange(ctx context.Context, fromLocalDay, toLocalDay time.Time, granularity string) (*PerformanceResponse, error) {
	raw, err := s.GetRawPerformanceByDay(ctx, fromLocalDay, toLocalDay)
	if err != nil {
		return nil, err
	}

	computedDays := make([]computedDay, 0, len(raw.Days))
	membersWithoutRate := map[string]struct{}{}
	for _, day := range raw.Days {
		computed := computeDayMetrics(day, raw.Premium, membersWithoutRate)
		computedDays = append(computedDays, computed)
	}

	periods := rollupPeriods(computedDays, granularity, raw.FromLocalDay, raw.ToLocalDay)
	totals := aggregateTotals(computedDays, raw.FromLocalDay, raw.ToLocalDay)

	return &PerformanceResponse{
		From:           raw.FromLocalDay,
		To:             raw.ToLocalDay,
		Granularity:    strings.ToLower(strings.TrimSpace(granularity)),
		Periods:        periods,
		Totals:         totals,
		PreviousPeriod: nil,
		Warnings: PerformanceWarnings{
			MembersWithoutRate: len(membersWithoutRate),
		},
	}, nil
}

func computeDayMetrics(day RawDayMetrics, premium PremiumConfig, membersWithoutRate map[string]struct{}) computedDay {
	plannedHours := 0.0
	workedHours := 0.0
	payrollRaw := 0.0
	dayMSIncomplete := false
	dayHasMSActivity := false
	shiftEmployeeIDs := map[string]struct{}{}

	for _, employee := range day.Employees {
		employeePlannedHours := float64(employee.PlannedMinutes) / 60.0
		employeeWorkedRawHours := float64(employee.WorkedSeconds) / 3600.0

		workedDisplayHours := employeeWorkedRawHours
		displaySegments := employee.WorkedPremium
		if employee.WorkedSeconds <= 0 {
			workedDisplayHours = employeePlannedHours
			displaySegments = employee.PlannedPremium
		}
		if workedDisplayHours > 0 {
			dayHasMSActivity = true
		}

		plannedHours += employeePlannedHours
		workedHours += workedDisplayHours

		if employee.HourlyRateCents <= 0 {
			if workedDisplayHours > 0 {
				membersWithoutRate[employee.EmployeeID] = struct{}{}
				dayMSIncomplete = true
			}
		} else {
			weightedHours := weightedPremiumHours(displaySegments, employee.SundayPremiumEligible, employee.NightPremiumEligible, premium)
			payrollRaw += weightedHours * float64(employee.HourlyRateCents) * (1.0 + employee.EmployerChargesPct/100.0)
		}

		if employee.PlannedMinutes > 0 {
			shiftEmployeeIDs[employee.EmployeeID] = struct{}{}
		}
	}

	payrollCents := int64(math.Round(payrollRaw))
	hoursDelta := workedHours - plannedHours

	// Determine effective revenue: real if >0, else forecast if present, else 0
	var effectiveCents int64
	if day.RevenueHTCents > 0 {
		effectiveCents = day.RevenueHTCents
	} else if day.RevenueForecastCents != nil {
		effectiveCents = *day.RevenueForecastCents
	} else {
		effectiveCents = 0
	}

	payrollRatio, revenuePerHourCents := computeRatios(
		effectiveCents,
		payrollCents,
		workedHours,
		dayMSIncomplete,
		dayHasMSActivity && effectiveCents <= 0,
		workedHours > 0 && effectiveCents <= 0,
	)

	period := PerformancePeriod{
		PeriodStart:            day.LocalDay,
		PeriodEnd:              day.LocalDay,
		Label:                  day.LocalDay,
		RevenueActualCents:     day.RevenueHTCents,
		RevenueForecastCents:   day.RevenueForecastCents,
		PlannedHours:           plannedHours,
		WorkedHours:            workedHours,
		Headcount:              day.Headcount,
		PayrollCostLoadedCents: payrollCents,
		PayrollRatio:           payrollRatio,
		RevenuePerHourCents:    revenuePerHourCents,
		HoursDelta:             hoursDelta,
	}

	return computedDay{
		LocalDay:               day.LocalDay,
		Period:                 period,
		ShiftEmployeeIDs:       shiftEmployeeIDs,
		MSIncomplete:           dayMSIncomplete,
		HasMSActivity:          dayHasMSActivity,
		WorkedHoursPositive:    workedHours > 0,
		RevenueActualCents:     day.RevenueHTCents,
		RevenueForecastCents:   day.RevenueForecastCents,
		RevenueEffectiveCents:  effectiveCents,
		PayrollCostLoadedCents: payrollCents,
		PlannedHours:           plannedHours,
		WorkedHours:            workedHours,
	}
}

func rollupPeriods(days []computedDay, granularity, fromDayRaw, toDayRaw string) []PerformancePeriod {
	granularity = strings.ToLower(strings.TrimSpace(granularity))
	if granularity == "" || granularity == "day" {
		periods := make([]PerformancePeriod, 0, len(days))
		for _, day := range days {
			periods = append(periods, day.Period)
		}
		return periods
	}

	fromDay, err := time.Parse("2006-01-02", fromDayRaw)
	if err != nil {
		return []PerformancePeriod{}
	}
	toDay, err := time.Parse("2006-01-02", toDayRaw)
	if err != nil {
		return []PerformancePeriod{}
	}

	type bucketAggregate struct {
		PeriodStart           time.Time
		PeriodEnd             time.Time
		Label                 string
		RevenueActualCents    int64
		RevenueEffectiveCents int64
		RevenueForecastCents  *int64
		PlannedHours          float64
		WorkedHours           float64
		PayrollCents          int64
		HoursDelta            float64
		MSIncomplete          bool
		AnyMSDayWithoutCA     bool
		AnyWorkedNoCA         bool
		ShiftEmployees        map[string]struct{}
	}

	buckets := map[string]*bucketAggregate{}
	orderedKeys := make([]string, 0)

	for _, day := range days {
		dayDate, parseErr := time.Parse("2006-01-02", day.LocalDay)
		if parseErr != nil {
			continue
		}

		bucketStart, bucketEnd, bucketLabel := computeBucketBounds(dayDate, granularity, fromDay, toDay)
		key := bucketStart.Format("2006-01-02")

		bucket, exists := buckets[key]
		if !exists {
			bucket = &bucketAggregate{
				PeriodStart:    bucketStart,
				PeriodEnd:      bucketEnd,
				Label:          bucketLabel,
				ShiftEmployees: map[string]struct{}{},
			}
			buckets[key] = bucket
			orderedKeys = append(orderedKeys, key)
		}

		bucket.RevenueActualCents += day.RevenueActualCents
		bucket.RevenueEffectiveCents += day.RevenueEffectiveCents
		if day.RevenueForecastCents != nil {
			if bucket.RevenueForecastCents == nil {
				v := *day.RevenueForecastCents
				bucket.RevenueForecastCents = &v
			} else {
				*bucket.RevenueForecastCents += *day.RevenueForecastCents
			}
		}
		bucket.PlannedHours += day.PlannedHours
		bucket.WorkedHours += day.WorkedHours
		bucket.PayrollCents += day.PayrollCostLoadedCents
		bucket.HoursDelta += day.Period.HoursDelta

		if day.MSIncomplete {
			bucket.MSIncomplete = true
		}
		if day.HasMSActivity && day.RevenueEffectiveCents <= 0 {
			bucket.AnyMSDayWithoutCA = true
		}
		if day.WorkedHoursPositive && day.RevenueEffectiveCents <= 0 {
			bucket.AnyWorkedNoCA = true
		}

		for employeeID := range day.ShiftEmployeeIDs {
			bucket.ShiftEmployees[employeeID] = struct{}{}
		}
	}

	sort.Strings(orderedKeys)
	periods := make([]PerformancePeriod, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		bucket := buckets[key]
		payrollRatio, revenuePerHour := computeRatios(
			bucket.RevenueEffectiveCents,
			bucket.PayrollCents,
			bucket.WorkedHours,
			bucket.MSIncomplete,
			bucket.AnyMSDayWithoutCA,
			bucket.AnyWorkedNoCA,
		)

		periods = append(periods, PerformancePeriod{
			PeriodStart:            bucket.PeriodStart.Format("2006-01-02"),
			PeriodEnd:              bucket.PeriodEnd.Format("2006-01-02"),
			Label:                  bucket.Label,
			RevenueActualCents:     bucket.RevenueActualCents,
			RevenueForecastCents:   bucket.RevenueForecastCents,
			PlannedHours:           bucket.PlannedHours,
			WorkedHours:            bucket.WorkedHours,
			Headcount:              len(bucket.ShiftEmployees),
			PayrollCostLoadedCents: bucket.PayrollCents,
			PayrollRatio:           payrollRatio,
			RevenuePerHourCents:    revenuePerHour,
			HoursDelta:             bucket.HoursDelta,
		})
	}

	return periods
}

func aggregateTotals(days []computedDay, fromDay, toDay string) PerformancePeriod {
	totalRevenueCents := int64(0)
	totalRevenueEffectiveCents := int64(0)
	var totalRevenueForecastCents *int64
	totalPlannedHours := 0.0
	totalWorkedHours := 0.0
	totalHeadcount := 0
	totalPayrollCents := int64(0)
	totalMSIncomplete := false
	anyMSDayWithoutCA := false
	anyWorkedHoursDayWithoutCA := false

	for _, day := range days {
		totalRevenueCents += day.RevenueActualCents
		totalRevenueEffectiveCents += day.RevenueEffectiveCents
		if day.RevenueForecastCents != nil {
			if totalRevenueForecastCents == nil {
				v := *day.RevenueForecastCents
				totalRevenueForecastCents = &v
			} else {
				*totalRevenueForecastCents += *day.RevenueForecastCents
			}
		}
		totalPlannedHours += day.PlannedHours
		totalWorkedHours += day.WorkedHours
		totalHeadcount += day.Period.Headcount
		totalPayrollCents += day.PayrollCostLoadedCents

		if day.MSIncomplete {
			totalMSIncomplete = true
		}
		if day.HasMSActivity && day.RevenueEffectiveCents <= 0 {
			anyMSDayWithoutCA = true
		}
		if day.WorkedHoursPositive && day.RevenueEffectiveCents <= 0 {
			anyWorkedHoursDayWithoutCA = true
		}
	}

	totalPayrollRatio, totalRevenuePerHour := computeRatios(
		totalRevenueEffectiveCents,
		totalPayrollCents,
		totalWorkedHours,
		totalMSIncomplete,
		anyMSDayWithoutCA,
		anyWorkedHoursDayWithoutCA,
	)

	return PerformancePeriod{
		PeriodStart:            fromDay,
		PeriodEnd:              toDay,
		Label:                  "Total",
		RevenueActualCents:     totalRevenueCents,
		RevenueForecastCents:   totalRevenueForecastCents,
		PlannedHours:           totalPlannedHours,
		WorkedHours:            totalWorkedHours,
		Headcount:              totalHeadcount,
		PayrollCostLoadedCents: totalPayrollCents,
		PayrollRatio:           totalPayrollRatio,
		RevenuePerHourCents:    totalRevenuePerHour,
		HoursDelta:             totalWorkedHours - totalPlannedHours,
	}
}

func computeRatios(revenueCents, payrollCents int64, workedHours float64, msIncomplete, anyMSWithoutCA, anyWorkedWithoutCA bool) (*float64, *float64) {
	var payrollRatio *float64
	if revenueCents > 0 && !msIncomplete && !anyMSWithoutCA {
		ratio := float64(payrollCents) / float64(revenueCents)
		payrollRatio = &ratio
	}

	var revenuePerHour *float64
	if revenueCents > 0 && workedHours > 0 && !anyWorkedWithoutCA {
		value := float64(revenueCents) / workedHours
		revenuePerHour = &value
	}

	return payrollRatio, revenuePerHour
}

func computeBucketBounds(day time.Time, granularity string, fromDay, toDay time.Time) (time.Time, time.Time, string) {
	switch granularity {
	case "week":
		weekday := int(day.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		weekStart := day.AddDate(0, 0, -(weekday - 1))
		weekEnd := weekStart.AddDate(0, 0, 6)
		start := maxDate(weekStart, fromDay)
		end := minDate(weekEnd, toDay)
		label := weekStart.Format("2006-01-02")
		return start, end, label
	case "month":
		monthStart := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC)
		monthEnd := monthStart.AddDate(0, 1, -1)
		start := maxDate(monthStart, fromDay)
		end := minDate(monthEnd, toDay)
		label := monthStart.Format("2006-01")
		return start, end, label
	default:
		return day, day, day.Format("2006-01-02")
	}
}

func computePreviousRange(fromDay, toDay time.Time, granularity string) (time.Time, time.Time) {
	from := normalizeDateOnlyUTC(fromDay)
	to := normalizeDateOnlyUTC(toDay)

	switch strings.ToLower(strings.TrimSpace(granularity)) {
	case "month":
		firstCurrentMonth := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
		monthCount := countMonthsInRange(from, to)
		prevFrom := firstCurrentMonth.AddDate(0, -monthCount, 0)
		prevTo := firstCurrentMonth.AddDate(0, 0, -1)
		return prevFrom, prevTo
	default:
		lengthDays := int(to.Sub(from).Hours()/24) + 1
		prevTo := from.AddDate(0, 0, -1)
		prevFrom := prevTo.AddDate(0, 0, -(lengthDays - 1))
		return prevFrom, prevTo
	}
}

func countMonthsInRange(from, to time.Time) int {
	count := 0
	cursor := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	last := time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC)
	for !cursor.After(last) {
		count++
		cursor = cursor.AddDate(0, 1, 0)
	}
	if count <= 0 {
		return 1
	}
	return count
}

func maxDate(left, right time.Time) time.Time {
	if left.After(right) {
		return left
	}
	return right
}

func minDate(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

type computedDay struct {
	LocalDay               string
	Period                 PerformancePeriod
	ShiftEmployeeIDs       map[string]struct{}
	MSIncomplete           bool
	HasMSActivity          bool
	WorkedHoursPositive    bool
	RevenueActualCents     int64
	RevenueForecastCents   *int64
	RevenueEffectiveCents  int64
	PayrollCostLoadedCents int64
	PlannedHours           float64
	WorkedHours            float64
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

	merchantSettings, err := s.repo.settings.GetOrCreateSettings(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}
	night, err := parseNightWindow(merchantSettings.NightShiftStart, merchantSettings.NightShiftEnd)
	if err != nil {
		return nil, fmt.Errorf("parse night window: %w", err)
	}
	holidayRows, err := s.repo.settings.ListPlanningHolidays(ctx, user.MerchantID, fromLocalDay, toLocalDay)
	if err != nil {
		return nil, err
	}
	holidayByDate := make(map[string]bool, len(holidayRows))
	for _, h := range holidayRows {
		if h.CountAsHoliday {
			holidayByDate[h.Date.Format("2006-01-02")] = true
		}
	}

	plannedIntervals, err := s.repo.ListPlannedShiftIntervals(ctx, user.MerchantID, fromLocalDay, toLocalDay)
	if err != nil {
		return nil, err
	}
	workedIntervals, err := s.repo.ListWorkedEntryIntervals(ctx, user.MerchantID, fromLocalDay, toLocalDay)
	if err != nil {
		return nil, err
	}

	days := buildRawDaySkeleton(fromLocalDay, toLocalDay)
	rateByEmployee := buildRateIndex(rateRows)
	headcountSets := map[string]map[string]struct{}{}

	for _, row := range plannedRows {
		localDayKey := normalizeLocalDayKey(row.LocalDay)
		day := ensureDay(days, localDayKey)
		employee := ensureEmployee(day, row.EmployeeID)
		employee.PlannedMinutes += row.PlannedMinutes
		employee.PlannedHours = float64(employee.PlannedMinutes) / 60.0

		if _, ok := headcountSets[localDayKey]; !ok {
			headcountSets[localDayKey] = map[string]struct{}{}
		}
		headcountSets[localDayKey][row.EmployeeID] = struct{}{}
	}

	for _, row := range workedRows {
		day := ensureDay(days, normalizeLocalDayKey(row.LocalDay))
		employee := ensureEmployee(day, row.EmployeeID)
		employee.WorkedSeconds += row.WorkedSeconds
		employee.WorkedHours = float64(employee.WorkedSeconds) / 3600.0
	}

	for _, interval := range plannedIntervals {
		// Attributed to the day the shift STARTS on, even if it runs past
		// midnight — same convention as ListPlannedByDayEmployee's
		// GROUP BY local_day (= s.shift_date).
		day := ensureDay(days, interval.StartAt.Format("2006-01-02"))
		employee := ensureEmployee(day, interval.EmployeeID)
		segments := segmentInterval(interval.StartAt, interval.EndAt, night, holidayByDate)
		segments = segments.applyBreakProration(int64(interval.BreakMinutes) * 60)
		employee.PlannedPremium.add(segments)
	}

	for _, interval := range workedIntervals {
		// Attributed to the day of clock-in — same convention as
		// ListWorkedRawByDayEmployee's local_day derivation.
		day := ensureDay(days, interval.StartAt.Format("2006-01-02"))
		employee := ensureEmployee(day, interval.EmployeeID)
		segments := segmentInterval(interval.StartAt, interval.EndAt, night, holidayByDate)
		employee.WorkedPremium.add(segments)
	}

	for _, row := range revenueRows {
		day := ensureDay(days, normalizeLocalDayKey(row.LocalDay))
		day.RevenueHTCents = row.RevenueHTCents
	}

	// Load revenue forecasts from planning_revenue_forecasts table
	forecastRows, err := s.repo.ListRevenueForecastByDay(ctx, user.MerchantID, fromLocalDay, toLocalDay)
	if err != nil {
		return nil, err
	}
	for _, row := range forecastRows {
		day := ensureDay(days, normalizeLocalDayKey(row.LocalDay))
		// avoid taking address of loop variable
		amt := row.AmountHTCents
		day.RevenueForecastCents = &amt
	}

	for localDay, day := range days {
		for employeeID, employee := range day.employeeIndex {
			if rate, ok := rateByEmployee[employeeID]; ok {
				employee.HourlyRateCents = rate.HourlyRateCents
				employee.EmployerChargesPct = rate.EmployerChargesPct
				employee.SundayPremiumEligible = rate.SundayPremiumEligible
				employee.NightPremiumEligible = rate.NightPremiumEligible
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
		Premium: PremiumConfig{
			NightShiftMultiplier:          merchantSettings.NightShiftMultiplier,
			SundayMultiplier:              merchantSettings.SundayMultiplier,
			CumulationMode:                merchantSettings.PremiumCumulationMode,
			NightSundayCombinedMultiplier: merchantSettings.NightSundayCombinedMultiplier,
		},
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

func normalizeLocalDayKey(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if idx := strings.Index(trimmed, "T"); idx > 0 {
		return trimmed[:idx]
	}

	if len(trimmed) >= len("2006-01-02") {
		candidate := trimmed[:10]
		if _, err := time.Parse("2006-01-02", candidate); err == nil {
			return candidate
		}
	}

	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04:05.000000", "2006-01-02"} {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed.UTC().Format("2006-01-02")
		}
	}

	return trimmed
}
