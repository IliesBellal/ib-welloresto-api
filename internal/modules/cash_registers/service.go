package cash_registers

import (
	"context"
	"time"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

type CashRegisterService struct {
	cashRegisterRepo *CashRegisterRepository
}

func NewCashRegisterService(cashRegisterRepo *CashRegisterRepository) *CashRegisterService {
	return &CashRegisterService{
		cashRegisterRepo: cashRegisterRepo,
	}
}

func (s *CashRegisterService) OpenCashRegister(ctx context.Context, token string, req *models.OpenCashRegisterRequest) (*models.CashRegisterOpenResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	// LOT B B2b-3 (§7.6) : refus d'ouverture de registre tant que
	// activation_state != 'LIVE' ou status = 'suspended' — même message
	// dans les deux cas, aucun montant, aucun détail d'abonnement.
	activated, err := s.cashRegisterRepo.IsActivatedForOrdering(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}
	if !activated {
		return nil, models.ErrCashRegisterNotActivated
	}

	// merchantID via user
	// userID vient de req (comme en PHP)
	req.CashRegister.UserID = user.UserID

	return s.cashRegisterRepo.OpenCashRegister(ctx, req, user.MerchantID)
}

// CloseCashRegisterResult décrit l'issue d'une fermeture de registre.
type CloseCashRegisterResult struct {
	AlreadyClosed bool
	ClosingMode   string // MANUAL / AUTO
	Enclosed      bool   // validé (toujours vrai en AUTO après la fermeture)
}

func (s *CashRegisterService) CloseCashRegister(ctx context.Context, token string, cashRegisterID string, req *models.CloseCashRegisterRequest) (*CloseCashRegisterResult, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	// Registre d'un autre établissement, ou inexistant : rien n'est fait, et la
	// réponse est celle d'un registre déjà fermé et validé, pour que l'app
	// poursuive normalement (ex. identifiant de registre resté en mémoire d'une
	// session précédente). Rien n'est lu ni renvoyé de ce registre.
	owned, err := s.cashRegisterRepo.CashRegisterBelongsToMerchant(ctx, cashRegisterID, user.MerchantID)
	if err != nil {
		return nil, err
	}
	if !owned {
		logger.FromContext(ctx).Warn("close requested on a cash register not owned by the merchant — no-op",
			zap.String("cash_register_id", cashRegisterID), zap.String("merchant_id", user.MerchantID))
		return &CloseCashRegisterResult{AlreadyClosed: true, ClosingMode: s.merchantClosingModeToday(ctx, user.MerchantID), Enclosed: true}, nil
	}

	// userID fourni dans la requête → OK (comme PHP)
	alreadyClosed, err := s.cashRegisterRepo.CloseCashRegister(ctx, cashRegisterID, user.MerchantID, req)
	if err != nil {
		return nil, err
	}

	mode, enclosed, err := s.cashRegisterRepo.GetRegisterClosingState(ctx, cashRegisterID)
	if err != nil {
		return nil, err
	}

	// Clôture automatique : pas de relevé de caisse ni de TPE, la validation
	// suit immédiatement la fermeture (cf. closing_mode.go). Aussi rejouée sur
	// un registre déjà fermé mais pas encore validé, pour qu'un appel répété
	// après une validation échouée ne laisse pas le registre en suspens.
	if mode == ClosingModeAuto && !enclosed {
		if err := s.cashRegisterRepo.EncloseCashRegister(ctx, user.UserID, cashRegisterID, ""); err != nil {
			return nil, err
		}
		enclosed = true
	}

	return &CloseCashRegisterResult{AlreadyClosed: alreadyClosed, ClosingMode: mode, Enclosed: enclosed}, nil
}

// merchantClosingModeToday renvoie le mode de clôture en vigueur aujourd'hui
// pour l'établissement (MANUAL en cas d'erreur de lecture : réponse
// informative seulement).
func (s *CashRegisterService) merchantClosingModeToday(ctx context.Context, merchantID string) string {
	loc, err := s.cashRegisterRepo.MerchantLocation(ctx, merchantID)
	if err != nil {
		return ClosingModeManual
	}
	mode, err := s.cashRegisterRepo.ResolveClosingMode(ctx, merchantID, localDay(time.Now(), loc))
	if err != nil {
		return ClosingModeManual
	}
	return mode
}

// requireOwnRegister renvoie models.ErrNotFound si le registre n'appartient pas
// à l'établissement de l'utilisateur (ou n'existe pas) : on ne révèle rien du
// registre d'un autre établissement, on n'y touche pas.
func (s *CashRegisterService) requireOwnRegister(ctx context.Context, cashRegisterID, merchantID string) error {
	owned, err := s.cashRegisterRepo.CashRegisterBelongsToMerchant(ctx, cashRegisterID, merchantID)
	if err != nil {
		return err
	}
	if !owned {
		return models.ErrNotFound
	}
	return nil
}

// GetClosingModes renvoie le mode de clôture en vigueur aujourd'hui pour un
// établissement et tout son historique. Réservé à l'équipe WelloResto
// (/admin).
func (s *CashRegisterService) GetClosingModes(ctx context.Context, merchantID string) (*ClosingModesOverview, error) {
	loc, err := s.cashRegisterRepo.MerchantLocation(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	today := localDay(time.Now(), loc)
	current, err := s.cashRegisterRepo.ResolveClosingMode(ctx, merchantID, today)
	if err != nil {
		return nil, err
	}
	history, err := s.cashRegisterRepo.ListClosingModes(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	return &ClosingModesOverview{
		MerchantID:  merchantID,
		Timezone:    loc.String(),
		Today:       today.Format(closingModeDateLayout),
		CurrentMode: current,
		History:     history,
	}, nil
}

// ScheduleClosingMode planifie un changement de mode de clôture pour un
// établissement, au 1er d'un mois futur (cf. validateClosingModeSchedule).
// Réservé à l'équipe WelloResto (/admin).
func (s *CashRegisterService) ScheduleClosingMode(ctx context.Context, merchantID string, req ScheduleClosingModeRequest) (*ClosingModesOverview, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	loc, err := s.cashRegisterRepo.MerchantLocation(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	effectiveFrom, err := validateClosingModeSchedule(req, time.Now(), loc)
	if err != nil {
		return nil, err
	}
	inEffect, err := s.cashRegisterRepo.ResolveClosingMode(ctx, merchantID, effectiveFrom)
	if err != nil {
		return nil, err
	}
	if inEffect == req.Mode {
		return nil, ErrClosingModeAlreadyInEffect
	}
	if err := s.cashRegisterRepo.InsertClosingMode(ctx, merchantID, req.Mode, effectiveFrom, user.UserID); err != nil {
		return nil, err
	}
	return s.GetClosingModes(ctx, merchantID)
}

// CancelClosingMode annule un changement de mode planifié, tant qu'il n'est
// pas entré en vigueur (date d'effet postérieure à aujourd'hui, calendrier de
// l'établissement). Un changement déjà en vigueur n'est jamais supprimé : il
// sert à régénérer les anciens exports. Réservé à l'équipe WelloResto (/admin).
func (s *CashRegisterService) CancelClosingMode(ctx context.Context, merchantID, effectiveFromRaw string) (*ClosingModesOverview, error) {
	loc, err := s.cashRegisterRepo.MerchantLocation(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	effectiveFrom, err := time.ParseInLocation(closingModeDateLayout, effectiveFromRaw, loc)
	if err != nil {
		return nil, ErrClosingModeInvalidDate
	}
	if !effectiveFrom.After(localDay(time.Now(), loc)) {
		return nil, ErrClosingModeRetroactive
	}
	if err := s.cashRegisterRepo.DeleteClosingMode(ctx, merchantID, effectiveFrom); err != nil {
		return nil, err
	}
	return s.GetClosingModes(ctx, merchantID)
}

func (s *CashRegisterService) GetCashRegisterSummary(ctx context.Context, token string, cashRegisterID string) (*models.CashRegisterSummaryResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if err := s.requireOwnRegister(ctx, cashRegisterID, user.MerchantID); err != nil {
		return nil, err
	}
	return s.cashRegisterRepo.GetCashRegisterSummary(ctx, cashRegisterID, user.MerchantID)
}

func (s *CashRegisterService) GetCashRegisterTVADetails(ctx context.Context, token string, cashRegisterID string) (*models.CashRegisterDetails, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	return s.cashRegisterRepo.GetCashRegisterTVADetails(ctx, user.MerchantID, cashRegisterID)
}

func (s *CashRegisterService) AddCustomItem(ctx context.Context, token string, id string, req *models.AddCustomItemRequest) (interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if err := s.requireOwnRegister(ctx, id, user.MerchantID); err != nil {
		return nil, err
	}
	itemID, err := s.cashRegisterRepo.AddCustomItem(ctx, id, req, user)
	if err != nil {
		if err.Error() == "cash_register_closed" {
			return map[string]interface{}{"status": "-1", "error": "Cash register " + id + " closed."}, nil
		}
		return nil, err
	}

	return models.HandlerDefaultResponseModelSet{
		Status: "success",
		Data1:  itemID,
	}, nil
}

func (s *CashRegisterService) DeleteCustomItem(ctx context.Context, token string, id string, itemID string) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if err := s.requireOwnRegister(ctx, id, user.MerchantID); err != nil {
		return nil, err
	}
	err = s.cashRegisterRepo.DeleteCustomItem(ctx, id, itemID, user)
	if err != nil {
		if err.Error() == "cash_register_closed" {
			return map[string]interface{}{"status": "-1", "error": "Cash register " + id + " closed."}, nil
		}
		return nil, err
	}
	return map[string]interface{}{"status": "1"}, nil
}

func (s *CashRegisterService) EncloseCashRegister(ctx context.Context, id, token, comment string) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if err := s.requireOwnRegister(ctx, id, user.MerchantID); err != nil {
		return nil, err
	}
	err = s.cashRegisterRepo.EncloseCashRegister(ctx, user.UserID, id, comment)
	if err != nil {
		if err.Error() == "cash_register_closed" {
			return map[string]interface{}{"status": "-1", "error": "Cash register closed."}, nil
		}
		return nil, err
	}

	return map[string]interface{}{"status": "1"}, nil
}

func (s *CashRegisterService) GetCashRegisterHistory(ctx context.Context, req CashRegisterHistoryRequest) (*CashRegisterHistoryResult, error) {
	// Récupérer l'utilisateur depuis le contexte
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.cashRegisterRepo.GetCashRegisterHistory(ctx, user.MerchantID, user.UserID, req)
}

func (s *CashRegisterService) LinkDevice(ctx context.Context, req DeviceLinkRequest) error {
	// Récupérer l'utilisateur depuis le contexte
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	circular, err := s.cashRegisterRepo.IsCircularDeviceLink(ctx, req.DeviceID, req.OnBehalfOf)
	if err != nil {
		return models.ErrInternalServerError
	}
	if circular {
		return models.ErrCircularDeviceLink
	}

	// Logique métier additionnelle si nécessaire
	err = s.cashRegisterRepo.UpsertDeviceLink(ctx, req.DeviceID, user.UserID, req.OnBehalfOf)
	if err != nil {
		return models.ErrInternalServerError
	}
	return nil
}

func (s *CashRegisterService) UnlinkDevice(ctx context.Context, deviceID string) error {
	rowsAffected, err := s.cashRegisterRepo.DeleteDeviceLink(ctx, deviceID)
	if err != nil {
		return models.ErrInternalServerError
	}

	if rowsAffected == 0 {
		return models.ErrNotFound
	}

	return nil
}
