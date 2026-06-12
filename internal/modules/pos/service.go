package pos

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	settingspkg "welloresto-api/internal/modules/planning/settings"
)

type POSService struct {
	posRepo        *POSRepository
	holidayService *settingspkg.Service
}

func NewPOSService(p *POSRepository) *POSService {
	return &POSService{
		posRepo:        p,
		holidayService: settingspkg.NewService(settingspkg.NewRepository(p.database)),
	}
}

func (s *POSService) GetPOSStatus(ctx context.Context, token string) (*models.POSStatus, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	return s.posRepo.GetPOSStatus(ctx, user.MerchantID)
}

func (s *POSService) UpdatePOSStatus(ctx context.Context, token string, status bool) (*models.POSStatus, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if !user.Rights.AccessReception {
		return nil, models.ErrForbidden
	}

	err = s.posRepo.UpdatePOSStatus(ctx, user.UserID, status)
	if err != nil {
		return nil, err
	}

	return s.posRepo.GetPOSStatus(ctx, user.MerchantID)
}

func (s *POSService) GetDeletionReasons(ctx context.Context, object string) ([]models.DeletionReason, error) {
	return s.posRepo.GetDeletionReasons(ctx, object)
}

func (s *POSService) ToggleScanNOrder(ctx context.Context, token, status string) (int64, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return 0, models.ErrUnauthorized
	}

	return s.posRepo.ToggleScanNOrder(ctx, user.MerchantID, status)
}

func (s *POSService) ToggleProductionPaidOnly(ctx context.Context, token, status string) (int64, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return 0, models.ErrUnauthorized
	}

	return s.posRepo.ToggleProductionPaidOnly(ctx, user.MerchantID, status)
}

func (s *POSService) GetTVARates(ctx context.Context, token string) ([]ConsumptionType, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	return s.posRepo.GetTVARates(ctx, user.MerchantID)
}

func (s *POSService) ListPlanningHolidays(ctx context.Context, token string, filters PlanningHolidayListFilters) ([]PlanningHoliday, error) {
	return s.holidayService.ListPlanningHolidays(ctx, filters)
}

func (s *POSService) PatchPlanningHolidayOverride(ctx context.Context, token, holidayDate string, req PlanningHolidayOverridePatchRequest) (*PlanningHoliday, error) {
	return s.holidayService.PatchPlanningHolidayOverride(ctx, holidayDate, req)
}

func (s *POSService) DeletePlanningHolidayOverride(ctx context.Context, token, holidayDate string) error {
	return s.holidayService.DeletePlanningHolidayOverride(ctx, holidayDate)
}

func (s *POSService) ToggleSafetyStock(ctx context.Context, token, status string) (int64, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return 0, models.ErrUnauthorized
	}

	return s.posRepo.ToggleSafetyStock(ctx, user.MerchantID, status)
}

func (s *POSService) GetDeliveryMen(ctx context.Context, token string) ([]models.DeliveryMan, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	return s.posRepo.GetDeliveryMen(ctx, user.MerchantID)
}

func (s *POSService) CheckTR(ctx context.Context, token, code string) (*models.TRCheckResponse, error) {
	_, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	// --- Parse code ---
	if len(code) != 20 {
		return &models.TRCheckResponse{
			Status:  "invalid_format",
			Message: "TR code must be 20 digits long",
			Code:    code,
		}, nil
	}

	// extract parts
	id := code[:11]

	valueInt, err := strconv.Atoi(code[11:16])
	if err != nil {
		return nil, errors.New("invalid TR value")
	}
	value := float64(valueInt) / 100.0

	vintage, err := strconv.Atoi(code[16:])
	if err != nil {
		return nil, errors.New("invalid TR vintage")
	}

	if value == 0 {
		return &models.TRCheckResponse{
			Status:  "no_value",
			Message: "Value cannot be 0",
			Code:    code,
			ID:      id,
			Value:   value,
			Vintage: vintage,
		}, nil
	}

	// --- Check if TR already used ---
	used, err := s.posRepo.IsTicketUsed(ctx, code)
	if err != nil {
		return nil, err
	}
	if used {
		return &models.TRCheckResponse{
			Status:  "used",
			Message: "TR already used",
			Code:    code,
			ID:      id,
			Value:   value,
			Vintage: vintage,
		}, nil
	}

	// --- Expiration logic ---
	now := time.Now().UTC()

	expiry := time.Date(vintage+1, 1, 31, 0, 0, 0, 0, time.UTC)
	validFrom := time.Date(vintage-1, 12, 1, 0, 0, 0, 0, time.UTC)

	if now.After(expiry) || now.Before(validFrom) {
		return &models.TRCheckResponse{
			Status:  "expired",
			Message: "TR is expired",
			Code:    code,
			ID:      id,
			Value:   value,
			Vintage: vintage,
		}, nil
	}

	// --- VALID ---
	return &models.TRCheckResponse{
		Status:  "valid",
		Message: "TR can be used",
		Code:    code,
		ID:      id,
		Value:   value,
		Vintage: vintage,
	}, nil
}

func (s *POSService) UpdateMerchantSettings(ctx context.Context, token string, req *models.UpdateMerchantSettingsRequest) (*models.POSSettingsResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	// Support PATCH payload shaped like GET /pos/settings response:
	// { info, timings, ordering, scan_order, hours_of_operations }
	if req != nil {
		if req.Info != nil {
			if req.Merchant == nil {
				req.Merchant = &models.MerchantSettings{}
			}
			if req.Parameters == nil {
				req.Parameters = &models.MerchantParametersSettings{}
			}

			if req.Info.Name != nil {
				req.Merchant.BusinessName = req.Info.Name
			}
			if req.Info.Phone != nil {
				req.Merchant.MerchantTel = req.Info.Phone
			}
			if req.Info.Address != nil {
				req.Merchant.Address = req.Info.Address
			}
			if req.Info.Street != nil {
				req.Merchant.Street = req.Info.Street
			}
			if req.Info.City != nil {
				req.Merchant.City = req.Info.City
			}
			if req.Info.PostalCode != nil {
				req.Merchant.ZipCode = req.Info.PostalCode
			}
			if req.Info.Country != nil {
				req.Merchant.Country = req.Info.Country
			}
			if req.Info.Lat != nil {
				req.Merchant.Lat = req.Info.Lat
			}
			if req.Info.Lng != nil {
				req.Merchant.Lng = req.Info.Lng
			}

			// Intentionally ignore req.Info.SIRET: SIRET must not be updated from this endpoint.

			if req.Info.Currency != nil {
				req.Parameters.Currency = req.Info.Currency
			}
			if req.Info.PrimaryColor != nil {
				req.Parameters.PrimaryColor = req.Info.PrimaryColor
			}
			if req.Info.TextColor != nil {
				req.Parameters.TextColorOnPrimaryColor = req.Info.TextColor
			}
			if req.Info.IsOpen != nil {
				req.Parameters.IsOpen = req.Info.IsOpen
			}
		}

		if req.Timings != nil {
			if req.Parameters == nil {
				req.Parameters = &models.MerchantParametersSettings{}
			}
			if req.Timings.WaitTimeMin != nil {
				req.Parameters.MinimumPreparationTime = req.Timings.WaitTimeMin
			}
			if req.Timings.WaitTimeMax != nil {
				req.Parameters.MaximumPreparationTime = req.Timings.WaitTimeMax
			}
			if req.Timings.AutoCloseEnabled != nil {
				req.Parameters.AutoCompleteOrders = req.Timings.AutoCloseEnabled
			}
			if req.Timings.AutoCloseDelay != nil {
				req.Parameters.AutoCompleteOrdersDelay = req.Timings.AutoCloseDelay
			}
		}

		if req.Ordering != nil {
			if req.Parameters == nil {
				req.Parameters = &models.MerchantParametersSettings{}
			}
			if req.Ordering.PaidOrdersOnly != nil {
				req.Parameters.KitchenShowOnlyPaid = req.Ordering.PaidOrdersOnly
			}
			if req.Ordering.ConcurrentCapacity != nil {
				req.Parameters.ConcurrentPreparationCapacity = req.Ordering.ConcurrentCapacity
			}
			if req.Ordering.ServiceRequired != nil {
				v := strings.ToLower(strings.TrimSpace(*req.Ordering.ServiceRequired))
				required := v == "table"
				req.Parameters.ServiceRequiredForOrdering = &required
			}
			if req.Ordering.DisableLowStock != nil {
				req.Parameters.DisableComponentsUnderSafetyStock = req.Ordering.DisableLowStock
			}
			if req.Ordering.RegisterRequired != nil {
				req.Parameters.CashRegisterRequiredForOrdering = req.Ordering.RegisterRequired
			}
			if req.Ordering.ActiveOnSite != nil {
				req.Parameters.ManageOnSite = req.Ordering.ActiveOnSite
			}
			if req.Ordering.ActiveTakeaway != nil {
				req.Parameters.ManageTakeAway = req.Ordering.ActiveTakeaway
			}
			if req.Ordering.ActiveDelivery != nil {
				req.Parameters.ManageDelivery = req.Ordering.ActiveDelivery
			}
		}

		if req.ScanOrder != nil {
			if req.Parameters == nil {
				req.Parameters = &models.MerchantParametersSettings{}
			}
			if req.ScanOrder.AutoAcceptDelivery != nil {
				req.Parameters.AutoAcceptSnoDeliveryOrders = req.ScanOrder.AutoAcceptDelivery
			}
			if req.ScanOrder.AutoAcceptTakeaway != nil {
				req.Parameters.AutoAcceptSnoTakeAwayOrders = req.ScanOrder.AutoAcceptTakeaway
			}
			if req.ScanOrder.AllowScheduled != nil {
				req.Parameters.EnableAdvanceOrders = req.ScanOrder.AllowScheduled
			}
			if req.ScanOrder.MaxScheduleDays != nil {
				req.Parameters.AdvanceOrderDays = req.ScanOrder.MaxScheduleDays
			}
			if req.ScanOrder.EnableRating != nil {
				req.Parameters.EnabledRating = req.ScanOrder.EnableRating
			}
		}

		if req.Security != nil {
			if req.Parameters == nil {
				req.Parameters = &models.MerchantParametersSettings{}
			}
			if req.Security.POSAutoLockEnabled != nil {
				req.Parameters.POSAutoLockEnabled = req.Security.POSAutoLockEnabled
			}
			if req.Security.POSAutoLockDelayMinutes != nil {
				req.Parameters.POSAutoLockDelayMinutes = req.Security.POSAutoLockDelayMinutes
			}
		}
	}

	if req != nil && req.Parameters != nil && req.Parameters.POSAutoLockDelayMinutes != nil && *req.Parameters.POSAutoLockDelayMinutes < 5 {
		return nil, models.ErrInvalidInput
	}

	// API contract: wait times are expressed in minutes.
	// Persistence contract: merchant_parameters stores wait times in seconds.
	if req != nil && req.Parameters != nil {
		req.Parameters.MinimumPreparationTime = minutesToSecondsPtr(req.Parameters.MinimumPreparationTime)
		req.Parameters.MaximumPreparationTime = minutesToSecondsPtr(req.Parameters.MaximumPreparationTime)
	}

	if err := s.posRepo.UpdateMerchantSettings(ctx, user.MerchantID, req); err != nil {
		return nil, err
	}

	return s.GetMerchantSettings(ctx, token)
}

func (s *POSService) GetMerchantSettings(ctx context.Context, token string) (*models.POSSettingsResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	m, params, _, _, err := s.posRepo.GetMerchantSettings(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}

	hoursOfOperations, err := s.posRepo.GetHoursOfOperations(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}

	serviceRequired := "none"
	if boolVal(params.ServiceRequiredForOrdering) {
		serviceRequired = "table"
	}

	resp := &models.POSSettingsResponse{
		Info: models.POSSettingsInfo{
			Name:         stringVal(m.BusinessName),
			Phone:        stringVal(m.MerchantTel),
			SIRET:        stringVal(m.SIRET),
			Address:      stringVal(m.Address),
			Street:       stringVal(m.Street),
			City:         stringVal(m.City),
			PostalCode:   stringVal(m.ZipCode),
			Country:      stringVal(m.Country),
			Lat:          floatVal(m.Lat),
			Lng:          floatVal(m.Lng),
			Currency:     stringVal(params.Currency),
			PrimaryColor: stringVal(params.PrimaryColor),
			TextColor:    stringVal(params.TextColorOnPrimaryColor),
			IsOpen:       boolVal(params.IsOpen),
		},
		Timings: models.POSSettingsTimings{
			WaitTimeMin:      secondsToMinutes(intVal(params.MinimumPreparationTime)),
			WaitTimeMax:      secondsToMinutes(intVal(params.MaximumPreparationTime)),
			AutoCloseEnabled: boolVal(params.AutoCompleteOrders),
			AutoCloseDelay:   intVal(params.AutoCompleteOrdersDelay),
		},
		Ordering: models.POSSettingsOrdering{
			PaidOrdersOnly:     boolVal(params.KitchenShowOnlyPaid),
			ConcurrentCapacity: intVal(params.ConcurrentPreparationCapacity),
			ServiceRequired:    serviceRequired,
			DisableLowStock:    boolVal(params.DisableComponentsUnderSafetyStock),
			RegisterRequired:   boolVal(params.CashRegisterRequiredForOrdering),
			ActiveOnSite:       boolVal(params.ManageOnSite),
			ActiveTakeaway:     boolVal(params.ManageTakeAway),
			ActiveDelivery:     boolVal(params.ManageDelivery),
		},
		ScanOrder: models.POSSettingsScanOrder{
			ActiveDelivery:     boolVal(params.ManageDelivery),
			ActiveTakeaway:     boolVal(params.ManageTakeAway),
			ActiveOnSite:       boolVal(params.ManageOnSite),
			AutoAcceptDelivery: boolVal(params.AutoAcceptSnoDeliveryOrders),
			AutoAcceptTakeaway: boolVal(params.AutoAcceptSnoTakeAwayOrders),
			AllowScheduled:     boolVal(params.EnableAdvanceOrders),
			MaxScheduleDays:    intVal(params.AdvanceOrderDays),
			EnableRating:       boolVal(params.EnabledRating),
		},
		Security: models.POSSettingsSecurity{
			POSAutoLockEnabled:      boolVal(params.POSAutoLockEnabled),
			POSAutoLockDelayMinutes: intVal(params.POSAutoLockDelayMinutes),
		},
		HoursOfOperations: hoursOfOperations,
	}

	return resp, nil
}

func boolVal(v *bool) bool {
	if v == nil {
		return false
	}
	return *v
}

func intVal(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func stringVal(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func floatVal(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func secondsToMinutes(seconds int) int {
	if seconds <= 0 {
		return 0
	}
	return seconds / 60
}

func minutesToSecondsPtr(v *int) *int {
	if v == nil {
		return nil
	}
	seconds := (*v) * 60
	return &seconds
}

func (s *POSService) CreateHourOfOperation(ctx context.Context, token string, req *models.POSHoursOfOperationPatch) (*models.POSHoursOfOperation, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if req == nil {
		return nil, models.ErrInvalidInput
	}

	return s.posRepo.CreateHourOfOperation(ctx, user.MerchantID, req)
}

func (s *POSService) UpdateHourOfOperation(ctx context.Context, token string, hourID string, req *models.POSHoursOfOperationPatch) (*models.POSHoursOfOperation, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if strings.TrimSpace(hourID) == "" || req == nil {
		return nil, models.ErrInvalidInput
	}

	return s.posRepo.UpdateHourOfOperation(ctx, user.MerchantID, hourID, req)
}

func (s *POSService) DeleteHourOfOperation(ctx context.Context, token string, hourID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}

	if strings.TrimSpace(hourID) == "" {
		return models.ErrInvalidInput
	}

	return s.posRepo.DeleteHourOfOperation(ctx, user.MerchantID, hourID)
}
