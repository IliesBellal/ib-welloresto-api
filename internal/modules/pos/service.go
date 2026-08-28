package pos

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/notification"
	settingspkg "welloresto-api/internal/modules/planning/settings"
	rolesModule "welloresto-api/internal/modules/roles"
)

// realtimeBroadcaster diffuse un message WebSocket à tous les devices d'un
// merchant. Satisfait par *notification.NotificationService ; déclaré ici en
// interface pour que le service reste testable sans hub réel.
type realtimeBroadcaster interface {
	BroadcastToMerchant(merchantID string, payload map[string]interface{}) bool
}

type POSService struct {
	posRepo        *POSRepository
	holidayService *settingspkg.Service
	rolesRepo      *rolesModule.Repository
	broadcaster    realtimeBroadcaster
}

// NewPOSService construit le service. broadcaster peut être nil (la diffusion
// temps réel est alors simplement inactive) : elle est best-effort et ne doit
// jamais faire échouer une mutation métier qui vient d'aboutir.
func NewPOSService(p *POSRepository, broadcaster realtimeBroadcaster) *POSService {
	return &POSService{
		posRepo:        p,
		holidayService: settingspkg.NewService(settingspkg.NewRepository(p.database)),
		rolesRepo:      rolesModule.NewRepository(p.database),
		broadcaster:    broadcaster,
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

	// RBAC lot 1 removed the users_rights.access_wrreception gate that used to
	// sit here (it was keyed on a flag meant for the now-decommissioned
	// WR_RECEPTION mobile app, not this specific back-office action) without a
	// replacement, opening PATCH /pos/status to any authenticated user for the
	// time it took to reach lot 2. RBAC lot 2 closed it: the route is now
	// gated by permission.POSStatusManage (see cmd/api/routes.go), enforced by
	// middleware.RequirePermission before this method is ever called.
	err = s.posRepo.UpdatePOSStatus(ctx, user.MerchantID, status)
	if err != nil {
		return nil, err
	}

	// La bascule est écrite : on prévient tous les devices du merchant, y
	// compris celui qui vient de l'émettre (l'appliquer deux fois est
	// idempotent). Diffusé après l'écriture et non après le GET ci-dessous
	// pour que l'événement parte même si la relecture du statut composé
	// échoue — le changement, lui, a bien eu lieu.
	s.broadcastPOSStatus(user.MerchantID, status)

	return s.posRepo.GetPOSStatus(ctx, user.MerchantID)
}

// broadcastPOSStatus diffuse l'ouverture/fermeture manuelle du point de vente.
// Best-effort et nil-safe : un merchant sans device connecté (ou une API
// démarrée sans hub) n'est pas une erreur.
//
// is_open est le flag brut de merchant_parameters, pas le statut composé
// renvoyé par GET /pos/status — voir notification.WSEventPOSStatusChanged.
func (s *POSService) broadcastPOSStatus(merchantID string, isOpen bool) {
	if s.broadcaster == nil {
		return
	}
	s.broadcaster.BroadcastToMerchant(merchantID, map[string]interface{}{
		"type":        notification.WSEventPOSStatusChanged,
		"merchant_id": merchantID,
		"is_open":     isOpen,
	})
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

func (s *POSService) ListPlanningVacationPeriods(ctx context.Context, token string) ([]PlanningVacationPeriod, error) {
	return s.holidayService.ListPlanningVacationPeriods(ctx)
}

func (s *POSService) CreatePlanningVacationPeriod(ctx context.Context, token string, req PlanningVacationPeriodCreateRequest) (*PlanningVacationPeriod, error) {
	return s.holidayService.CreatePlanningVacationPeriod(ctx, req)
}

func (s *POSService) UpdatePlanningVacationPeriod(ctx context.Context, token, id string, req PlanningVacationPeriodUpdateRequest) (*PlanningVacationPeriod, error) {
	return s.holidayService.UpdatePlanningVacationPeriod(ctx, id, req)
}

func (s *POSService) DeletePlanningVacationPeriod(ctx context.Context, token, id string) error {
	return s.holidayService.DeletePlanningVacationPeriod(ctx, id)
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
			if req.Ordering.UpsellEnabled != nil {
				req.Parameters.POSUpsellEnabled = req.Ordering.UpsellEnabled
			}
			if req.Ordering.CoversCountRequired != nil {
				req.Parameters.POSCoversCountRequired = req.Ordering.CoversCountRequired
			}
			// mobile_payment_enabled est porte par waiter_app_can_cash_in :
			// voir le commentaire sur models.POSSettingsOrdering.
			if req.Ordering.MobilePaymentEnabled != nil {
				req.Parameters.WaiterAppCanCashIn = req.Ordering.MobilePaymentEnabled
			}
			if req.Ordering.ProductionDisplayMode != nil {
				req.Parameters.ProductionDisplayMode = req.Ordering.ProductionDisplayMode
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

		if req.DeliveryZone != nil {
			if req.Parameters == nil {
				req.Parameters = &models.MerchantParametersSettings{}
			}
			if req.DeliveryZone.ZoningType != nil {
				req.Parameters.ZoningType = req.DeliveryZone.ZoningType
			}
			if req.DeliveryZone.CardinalConeCount != nil {
				req.Parameters.CardinalConeCount = req.DeliveryZone.CardinalConeCount
			}
			if req.DeliveryZone.CardinalZoneRanges != nil {
				req.Parameters.CardinalZoneRanges = req.DeliveryZone.CardinalZoneRanges
			}
			if req.DeliveryZone.RadialConeCount != nil {
				req.Parameters.RadialConeCount = req.DeliveryZone.RadialConeCount
			}
			if req.DeliveryZone.RadialZoneRanges != nil {
				req.Parameters.RadialZoneRanges = req.DeliveryZone.RadialZoneRanges
			}
			if req.DeliveryZone.GridCellSizeKm != nil {
				req.Parameters.GridCellSizeKm = req.DeliveryZone.GridCellSizeKm
			}
			if req.DeliveryZone.GridOriginLat != nil {
				req.Parameters.GridOriginLat = req.DeliveryZone.GridOriginLat
			}
			if req.DeliveryZone.GridOriginLng != nil {
				req.Parameters.GridOriginLng = req.DeliveryZone.GridOriginLng
			}
		}

		if req.CustomerFormRequirements != nil {
			if req.Parameters == nil {
				req.Parameters = &models.MerchantParametersSettings{}
			}
			req.Parameters.CustomerFormRequirements = req.CustomerFormRequirements
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
			LogoURL:      stringVal(m.LogoURL),
		},
		Timings: models.POSSettingsTimings{
			WaitTimeMin:      secondsToMinutes(intVal(params.MinimumPreparationTime)),
			WaitTimeMax:      secondsToMinutes(intVal(params.MaximumPreparationTime)),
			AutoCloseEnabled: boolVal(params.AutoCompleteOrders),
			AutoCloseDelay:   intVal(params.AutoCompleteOrdersDelay),
		},
		Ordering: models.POSSettingsOrdering{
			PaidOrdersOnly:           boolVal(params.KitchenShowOnlyPaid),
			ConcurrentCapacity:       intVal(params.ConcurrentPreparationCapacity),
			ServiceRequired:          serviceRequired,
			DisableLowStock:          boolVal(params.DisableComponentsUnderSafetyStock),
			RegisterRequired:         boolVal(params.CashRegisterRequiredForOrdering),
			ActiveOnSite:             boolVal(params.ManageOnSite),
			ActiveTakeaway:           boolVal(params.ManageTakeAway),
			ActiveDelivery:           boolVal(params.ManageDelivery),
			UpsellEnabled:            boolVal(params.POSUpsellEnabled),
			CoversCountRequired:      boolVal(params.POSCoversCountRequired),
			MobilePaymentEnabled:     boolVal(params.WaiterAppCanCashIn),
			CustomerFormRequirements: params.CustomerFormRequirements,
			ProductionDisplayMode:    stringVal(params.ProductionDisplayMode),
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
		DeliveryZone: models.POSSettingsDeliveryZone{
			ZoningType:         stringVal(params.ZoningType),
			CardinalConeCount:  intVal(params.CardinalConeCount),
			CardinalZoneRanges: stringVal(params.CardinalZoneRanges),
			RadialConeCount:    intVal(params.RadialConeCount),
			RadialZoneRanges:   stringVal(params.RadialZoneRanges),
			GridCellSizeKm:     intVal(params.GridCellSizeKm),
			GridOriginLat:      params.GridOriginLat,
			GridOriginLng:      params.GridOriginLng,
		},
		HoursOfOperations: hoursOfOperations,
	}

	return resp, nil
}

// SetLogoURL persiste l'URL du logo de l'établissement après upload R2 (voir
// POSHandler.UploadMerchantLogo) — même pattern que kiosk.Service.SetLogoURL.
func (s *POSService) SetLogoURL(ctx context.Context, merchantID, url string) (*models.POSSettingsResponse, error) {
	if err := s.posRepo.UpdateMerchant(ctx, merchantID, &models.MerchantSettings{LogoURL: &url}); err != nil {
		return nil, err
	}
	return s.GetMerchantSettings(ctx, "")
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
