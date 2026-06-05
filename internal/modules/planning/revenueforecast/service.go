package revenueforecast

import (
	"context"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	sharedpkg "welloresto-api/internal/modules/planning/shared"
	"welloresto-api/internal/utils/dbutils"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Upsert(ctx context.Context, req UpsertRevenueForecastsRequest) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}

	return dbutils.RunInTx(ctx, s.repo.db, func(txCtx context.Context) error {
		for _, forecast := range req.Forecasts {
			forecastDate, err := sharedpkg.ParsePlanningDate(forecast.Date)
			if err != nil {
				return models.ErrPlanningInvalidDate
			}

			if forecast.AmountCents == nil {
				if err := s.repo.DeleteByDate(txCtx, user.MerchantID, forecastDate); err != nil {
					return err
				}
				continue
			}

			if *forecast.AmountCents < 0 {
				return models.ErrInvalidRequestBody
			}

			if err := s.repo.Upsert(txCtx, user.MerchantID, forecastDate, *forecast.AmountCents); err != nil {
				return err
			}
		}

		return nil
	})
}
