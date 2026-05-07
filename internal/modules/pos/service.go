package pos

import (
	"context"
	"errors"
	"strconv"
	"time"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

type POSService struct {
	posRepo *POSRepository
}

func NewPOSService(p *POSRepository) *POSService {
	return &POSService{posRepo: p}
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
			Currency:     stringVal(params.Currency),
			PrimaryColor: stringVal(params.PrimaryColor),
			TextColor:    stringVal(params.TextColorOnPrimaryColor),
			IsOpen:       boolVal(params.IsOpen),
		},
		Timings: models.POSSettingsTimings{
			WaitTimeMin:      intVal(params.MinimumPreparationTime),
			WaitTimeMax:      intVal(params.MaximumPreparationTime),
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
