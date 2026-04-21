package reports

import (
	"context"
	"sort"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

type ReportsService struct {
	reportsRepo *ReportsRepository
}

func NewReportsService(repo *ReportsRepository) *ReportsService {
	return &ReportsService{reportsRepo: repo}
}

// GetTVAReport retourne le rapport TVA pour la période donnée
func (s *ReportsService) GetTVAReport(ctx context.Context, token, dateFrom, dateTo string) (*TVAReportResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	merchantID := user.MerchantID
	if merchantID == "" {
		return nil, models.ErrUnauthorized
	}

	// Fetch TVA data from repository
	tvaData, err := s.reportsRepo.GetTVAReportData(ctx, merchantID, dateFrom, dateTo)
	if err != nil {
		return nil, err
	}

	// Sort by date
	sort.Slice(tvaData, func(i, j int) bool {
		return tvaData[i].Date < tvaData[j].Date
	})

	report := &TVAReportResponse{
		Status:   "success",
		Calendar: tvaData,
	}

	return report, nil
}

// GetPaymentsReport retourne le rapport de paiements pour la période donnée
func (s *ReportsService) GetPaymentsReport(ctx context.Context, token, dateFrom, dateTo string) (*PaymentsReportResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	merchantID := user.MerchantID
	if merchantID == "" {
		return nil, models.ErrUnauthorized
	}

	// Fetch payments data from repository
	paymentsData, err := s.reportsRepo.GetPaymentsReportData(ctx, merchantID, dateFrom, dateTo)
	if err != nil {
		return nil, err
	}

	// Sort by date
	sort.Slice(paymentsData, func(i, j int) bool {
		return paymentsData[i].Date < paymentsData[j].Date
	})

	report := &PaymentsReportResponse{
		Status:   "success",
		Calendar: paymentsData,
	}

	return report, nil
}
