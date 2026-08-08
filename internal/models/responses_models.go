package models

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"welloresto-api/internal/logger"
)

type PendingOrdersData struct {
	Orders []Order `json:"orders"`
}

type PaginationMetadata struct {
	TotalItems  int `json:"total_items"`
	TotalPages  int `json:"total_pages"`
	CurrentPage int `json:"current_page"`
	Limit       int `json:"limit"`
}

type OrderHistoryMetadata struct {
	PaginationMetadata
	TotalRevenue int64   `json:"total_revenue"`
	AvgBasket    float64 `json:"avg_basket"`
	// Summary agrege TOUTE la periode filtree, independamment de la pagination :
	// il permet au POS d'afficher le resume de la journee sans avoir charge les
	// pages suivantes. Attention, son perimetre n'est PAS celui de la pagination
	// (voir OrderHistorySummary.OrdersCount).
	Summary OrderHistorySummary `json:"summary"`
}

// OrderHistorySummary porte les agregats de la periode. Les moyennes (panier
// moyen, couvert moyen) ne sont volontairement pas calculees ici : seules les
// sommes brutes sont exposees, le client derive les moyennes lui-meme.
type OrderHistorySummary struct {
	// OrdersCount ne compte que les commandes retenues par le resume
	// (hors CANCELED/DELETED, hors price <= 0). Il differe donc de
	// PaginationMetadata.TotalItems, qui couvre tout ce qu'affiche la liste.
	OrdersCount  int   `json:"orders_count"`
	CoversCount  int64 `json:"covers_count"`
	TotalRevenue int64 `json:"total_revenue"`
	// RefundsTotal est negatif (les remboursements sont stockes comme des
	// paiements a montant negatif) et vaut 0 en l'absence de remboursement.
	// C'est lui qui explique l'ecart entre TotalRevenue (somme des TTC, qui
	// ignore les remboursements) et la somme des ByPayment (nette).
	RefundsTotal int64               `json:"refunds_total"`
	ByChannel    []ChannelSummaryRow `json:"by_channel"`
	ByPayment    []PaymentSummaryRow `json:"by_payment"`
}

// ChannelSummaryRow : un couple (marque, type de commande). OrderTypeLabel est
// resolu via la table labels ; il retombe sur le code brut si aucun libelle
// n'existe, de sorte qu'une ligne ne soit jamais affichee vide.
type ChannelSummaryRow struct {
	Brand          string `json:"brand"`
	OrderType      string `json:"order_type"`
	OrderTypeLabel string `json:"order_type_label"`
	Total          int64  `json:"total"`
	OrdersCount    int    `json:"orders_count"`
}

// PaymentSummaryRow : un moyen de paiement. Total est NET des remboursements.
// Les commandes marketplace ont de vraies lignes de paiement (mop UBER_EATS /
// DELIVEROO), elles sont donc couvertes ici sans traitement particulier.
type PaymentSummaryRow struct {
	MOP   string `json:"mop"`
	Label string `json:"label"`
	Total int64  `json:"total"`
}

type OrderHistoryData struct {
	Metadata OrderHistoryMetadata `json:"metadata"`
	Orders   []Order              `json:"orders"`
}

type CustomerListData struct {
	Metadata  PaginationMetadata     `json:"metadata"`
	Customers []CustomerSearchResult `json:"customers"`
}

type OpenCashRegisterData struct {
	Status string `json:"status"`
}

type HandlerDefaultResponse struct {
	ID   string      `json:"id"`
	Data interface{} `json:"data"`
}

type HandlerDefaultResponseModelSet struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
	Data1   string `json:"data1,omitempty"`
}

// SendJSON envoie une réponse JSON standardisée avec la structure HandlerDefaultResponse
// Params:
//   - w: http.ResponseWriter
//   - statusCode: Code HTTP (ex: http.StatusOK, http.StatusUnauthorized)
//   - module: nom du module (ex: "auth")
//   - fnName: nom de la fonction handler (ex: "login")
//   - data: données à retourner (peut être nil)
func SendJSON(w http.ResponseWriter, statusCode int, module string, fnName string, data interface{}) {
	result := HandlerDefaultResponse{
		ID:   module + "." + fnName,
		Data: data,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode) // Très important : doit être appelé APRÈS le header mais AVANT l'encode

	if err := json.NewEncoder(w).Encode(result); err != nil {
		// Erreur lors de l'encoding JSON - logger pour debug
		// Note: On ne peut pas changer le statut car WriteHeader est déjà appelé
		// mais on peut au moins logger l'erreur
		println("[JSON_ENCODE_ERROR] " + module + "." + fnName + ": " + err.Error())
	}
}

// Sentinel errors pour une gestion d'erreurs standardisée entre Services et Handlers
var (
	// ErrUnauthorized indique que l'utilisateur n'est pas authentifié (401)
	ErrUnauthorized = errors.New("unauthorized")

	// ErrForbidden indique que l'utilisateur n'a pas les permissions nécessaires (403)
	ErrForbidden = errors.New("forbidden")

	// ErrNotFound indique que la ressource demandée n'existe pas (404)
	ErrNotFound = errors.New("not found")

	// ErrInvalidInput indique que les données fournies sont invalides (400)
	ErrInvalidInput = errors.New("invalid input")

	ErrInvalidData = errors.New("invalid_data")

	// ErrValidationError indique une erreur de validation métier/payload HACCP (400)
	ErrValidationError = errors.New("validation_error")

	ErrInvalidRequestBody = errors.New("invalid_request_body")

	ErrMissingResourceID = errors.New("missing_resource_id")

	ErrInvalidPage = errors.New("invalid_page")

	ErrInvalidPageSize = errors.New("invalid_page_size")

	ErrInvalidHACCPDate = errors.New("invalid_haccp_date")

	ErrInvalidActivityType = errors.New("invalid_activity_type")

	ErrInvalidActivityStatus = errors.New("invalid_activity_status")

	ErrTemperatureZoneNameRequired = errors.New("temperature_zone_name_required")

	ErrTemperatureZoneInvalidRange = errors.New("temperature_zone_invalid_range")

	ErrTemperatureReadingsRequired = errors.New("temperature_readings_required")

	ErrTemperatureZoneReferenceInvalid = errors.New("temperature_zone_reference_invalid")

	ErrTemperatureCorrectiveActionRequired = errors.New("temperature_corrective_action_required")

	ErrCorrectiveActionRequired = errors.New("corrective_action_required")

	ErrInvalidCorrectiveAction = errors.New("invalid_corrective_action")

	ErrDuplicateCorrectiveAction = errors.New("duplicate_corrective_action")

	ErrCorrectiveActionNoteRequired = errors.New("corrective_action_note_required")

	// ErrCleaningPhotoRequired indique qu'une photo est requise par la configuration HACCP (422)
	ErrCleaningPhotoRequired = errors.New("cleaning_photo_required")

	ErrCleaningZoneNameRequired = errors.New("cleaning_zone_name_required")

	ErrCleaningSurfaceZoneRequired = errors.New("cleaning_surface_zone_required")

	ErrCleaningSurfaceNameRequired = errors.New("cleaning_surface_name_required")

	ErrCleaningSurfaceInvalidFrequencyUnit = errors.New("cleaning_surface_invalid_frequency_unit")

	ErrCleaningSurfaceInvalidFrequencyCount = errors.New("cleaning_surface_invalid_frequency_count")

	ErrCleaningExecutionsRequired = errors.New("cleaning_executions_required")

	ErrCleaningSurfaceReferenceInvalid = errors.New("cleaning_surface_reference_invalid")

	ErrDuplicateCleaningSurfaceExecution = errors.New("duplicate_cleaning_surface_execution")

	ErrCleaningSurfaceInactive = errors.New("cleaning_surface_inactive")

	// ErrTemperatureFailurePhotoRequired indique qu'une photo est requise pour un relevé de température en écart (422)
	ErrTemperatureFailurePhotoRequired = errors.New("temperature_failure_photo_required")

	ErrGoodsReceiptSupplierRequired = errors.New("goods_receipt_supplier_required")

	ErrGoodsReceiptProductTypeRequired = errors.New("goods_receipt_product_type_required")

	ErrGoodsReceiptBatchNumberRequired = errors.New("goods_receipt_batch_number_required")

	ErrReceptionControlSampleRequired = errors.New("reception_control_sample_required")

	ErrReceptionNonConformitiesRequired = errors.New("reception_non_conformities_required")

	ErrReceptionInvoiceRequired = errors.New("reception_invoice_required")

	ErrUploadFileTooLargeOrInvalid = errors.New("upload_file_too_large_or_invalid")

	ErrUploadFileMissing = errors.New("upload_file_missing")

	ErrInvalidImageType = errors.New("invalid_image_type")

	// ErrTraceabilityPhotosRequired indique qu'aucune photo n'a été fournie pour
	// une soumission de traçabilité HACCP (400)
	ErrTraceabilityPhotosRequired = errors.New("traceability_photos_required")

	// ErrTraceabilityTooManyPhotos indique que plus de 10 photos ont été
	// fournies pour une soumission de traçabilité HACCP (400)
	ErrTraceabilityTooManyPhotos = errors.New("traceability_too_many_photos")

	// ErrInvalidInput indique que les données fournies sont invalides (400)
	ErrInvalidInputPasswordTooShort = errors.New("le mot de passe doit faire au minimum 8 charactères")

	// Erreurs spécifiques métier
	ErrDeliverySessionAlreadyActive = errors.New("delivery_session_already_active")

	// ErrNoActiveDeliverySession indique que l'utilisateur n'a aucune session de livraison active (404)
	ErrNoActiveDeliverySession = errors.New("no_active_delivery_session")

	// ErrDeliveryStopNotFound indique que l'order_id ciblé n'est pas un arrêt de la session (404)
	ErrDeliveryStopNotFound = errors.New("stop_not_found")

	// ErrDeliveryStopTerminal indique que l'arrêt ciblé est déjà dans un état terminal (409)
	ErrDeliveryStopTerminal = errors.New("stop_terminal")

	// ErrDeliveryStopNotCurrent indique que l'order_id ciblé n'est pas l'arrêt courant de la session (409)
	ErrDeliveryStopNotCurrent = errors.New("not_current_stop")

	// ErrDeliveryStopNotEnRoute indique que l'arrêt courant n'est pas (ou plus) en_route (409)
	ErrDeliveryStopNotEnRoute = errors.New("stop_not_en_route")

	// ErrDeliveryStopNotDeliverable indique que l'arrêt courant n'est ni en_route ni arrived (409)
	ErrDeliveryStopNotDeliverable = errors.New("stop_not_deliverable")

	// ErrOrderNotFullyPaid indique que la commande n'est pas encore intégralement payée (409)
	ErrOrderNotFullyPaid = errors.New("order_not_fully_paid")

	// ErrFailReasonRequired indique que le champ "reason" est manquant ou trop long (400)
	ErrFailReasonRequired = errors.New("reason_required")

	// ErrSessionHasPendingStops indique que la session a encore des arrêts non terminaux (409)
	ErrSessionHasPendingStops = errors.New("session_has_pending_stops")

	ErrInvalidToken = errors.New("invalid_token")

	ErrCannotDisableExternalPayments = errors.New("cannot disable external platforms payments")

	// Erreurs d'authentification
	ErrUserNotFound = errors.New("user_not_found")

	ErrAccountDisabled = errors.New("account_disabled")

	ErrTooLateToDeleteOrder = errors.New("too_late_to_delete_order")

	ErrOrderAlreadyAccepted = errors.New("order_already_accepted")

	ErrUserNotAllowed = errors.New("user_not_allowed")

	ErrCartEmpty = errors.New("cart_is_empty")

	ErrInternalServerError = errors.New("internal_server_error")

	ErrNoCashRegisterOpen = errors.New("no_cash_register_open")

	// ErrLinkedDeviceRegisterClosed indique que le device est lié à un autre device dont le registre n'est pas ouvert
	ErrLinkedDeviceRegisterClosed = errors.New("linked_device_register_closed")

	// ErrCircularDeviceLink indique une tentative de liaison circulaire entre deux devices
	ErrCircularDeviceLink = errors.New("circular_device_link")

	ErrRefoundMustBeGreaterThanZero = errors.New("refund_amount_must_be_greater_than_zero")

	ErrReceiptNotFound = errors.New("receipt_not_found")

	ErrDeviceIDMissing = errors.New("device_id_missing")

	ErrMOPMissing = errors.New("mop_missing")

	ErrRefoundMustBeLowerThanOriginalReceipt = errors.New("refund_amount_must_be_lower_than_original_receipt")

	ErrOrdersStillOpened = errors.New("orders_still_opened")

	ErrCashRegisterStillOpen = errors.New("cash_register_still_open")

	ErrOrderClosed = errors.New("order_closed")

	ErrOrderOpen = errors.New("order_open")

	ErrMFARequired = errors.New("mfa_required")

	ErrMFAExpired = errors.New("mfa_expired")

	ErrOTPMismatch = errors.New("otp_mismatch")

	ErrOTPWaitTime = errors.New("otp_wait_time")

	ErrTranslationLanguagesLimitReached = errors.New("translation_languages_limit_reached")

	ErrPlanningSettingsNotFound                 = errors.New("planning_settings_not_found")
	ErrPlanningAttendanceSourceInvalid          = errors.New("planning_attendance_source_invalid")
	ErrPlanningShiftSwapApprovalModeInvalid     = errors.New("planning_shift_swap_approval_mode_invalid")
	ErrPlanningPremiumCumulationModeInvalid     = errors.New("planning_premium_cumulation_mode_invalid")
	ErrPlanningHolidayOverrideNotFound          = errors.New("planning_holiday_override_not_found")
	ErrPlanningPositionNotFound                 = errors.New("planning_position_not_found")
	ErrPlanningPositionLabelRequired            = errors.New("planning_position_label_required")
	ErrPlanningPositionAlreadyExists            = errors.New("planning_position_already_exists")
	ErrPlanningPositionInUse                    = errors.New("planning_position_in_use")
	ErrPlanningWeekTemplateNotFound             = errors.New("planning_week_template_not_found")
	ErrPlanningWeekTemplateLabelRequired        = errors.New("planning_week_template_label_required")
	ErrPlanningWeekTemplateInvalidDayOfWeek     = errors.New("planning_week_template_invalid_day_of_week")
	ErrPlanningWeekTemplateInvalidConflictMode  = errors.New("planning_week_template_invalid_conflict_mode")
	ErrPlanningWeekTemplatePreviewRangeTooLarge = errors.New("planning_week_template_preview_range_too_large")
	ErrPlanningShiftTemplateNotFound            = errors.New("planning_shift_template_not_found")
	ErrPlanningShiftTemplateLabelRequired       = errors.New("planning_shift_template_label_required")
	ErrPlanningShiftTemplateInvalidRange        = errors.New("planning_shift_template_invalid_range")
	ErrPlanningEmployeeNotFound                 = errors.New("planning_employee_not_found")
	ErrPlanningEmployeeNameRequired             = errors.New("planning_employee_name_required")
	ErrPlanningEmployeeLastNameRequired         = errors.New("planning_employee_last_name_required")
	ErrPlanningEmployeePositionRequired         = errors.New("planning_employee_position_required")
	ErrPlanningEmployeeContractTypeRequired     = errors.New("planning_employee_contract_type_required")
	ErrPlanningEmployeeContractTypeInvalid      = errors.New("planning_employee_contract_type_invalid")
	ErrPlanningEmployeeTimeTrackingModeInvalid  = errors.New("planning_employee_time_tracking_mode_invalid")
	ErrPlanningEmployeeUserLinkInvalid          = errors.New("planning_employee_user_link_invalid")
	ErrPlanningEmployeeDocumentNotFound         = errors.New("planning_employee_document_not_found")
	ErrPlanningEmployeeDocumentTypeInvalid      = errors.New("planning_employee_document_type_invalid")
	ErrPlanningEmployeeDocumentNameRequired     = errors.New("planning_employee_document_name_required")
	ErrPlanningEmployeeDocumentFileRequired     = errors.New("planning_employee_document_file_required")
	ErrPlanningEmployeeDocumentUploadFailed     = errors.New("planning_employee_document_upload_failed")
	ErrPlanningEmployeeDocumentUrlFailed        = errors.New("planning_employee_document_url_failed")
	ErrPlanningEmployeeUserNotLinkedToMerchant  = errors.New("planning_employee_user_not_linked_to_merchant")
	ErrPlanningEmployeeUserAlreadyAssigned      = errors.New("planning_employee_user_already_assigned")
	ErrPlanningWeekAlreadyExists                = errors.New("planning_week_already_exists")
	ErrPlanningWeekNotFound                     = errors.New("planning_week_not_found")
	ErrPlanningShiftNotFound                    = errors.New("planning_shift_not_found")
	ErrPlanningShiftConflict                    = errors.New("planning_shift_conflict")
	ErrPlanningShiftInvalidRange                = errors.New("planning_shift_invalid_range")
	ErrPlanningDayCommentNotFound               = errors.New("planning_day_comment_not_found")
	ErrPlanningDayCommentTooLong                = errors.New("planning_day_comment_too_long")
	ErrPlanningTimeEntryNotFound                = errors.New("planning_time_entry_not_found")
	ErrPlanningTimeEntryAlreadyOpen             = errors.New("planning_time_entry_already_open")
	ErrPlanningTimeEntryNotOpen                 = errors.New("planning_time_entry_not_open")
	ErrPlanningTimeEntryInvalidRange            = errors.New("planning_time_entry_invalid_range")
	ErrPlanningTimeEntrySourceDisabled          = errors.New("planning_time_entry_source_disabled")
	ErrPlanningTimeEntryModeInvalid             = errors.New("planning_time_entry_mode_invalid")
	ErrPlanningTimeEntryShiftInvalid            = errors.New("planning_time_entry_shift_invalid")
	ErrPlanningLeaveRequestNotFound             = errors.New("planning_leave_request_not_found")
	ErrPlanningLeaveTypeInvalid                 = errors.New("planning_leave_type_invalid")
	ErrPlanningLeaveStatusInvalid               = errors.New("planning_leave_status_invalid")
	ErrPlanningLeaveInvalidRange                = errors.New("planning_leave_invalid_range")
	ErrPlanningLeaveShiftConflict               = errors.New("planning_leave_shift_conflict")
	ErrPlanningShiftSwapRequestNotFound         = errors.New("planning_shift_swap_request_not_found")
	ErrPlanningShiftSwapApprovalForbidden       = errors.New("planning_shift_swap_approval_forbidden")
	ErrPlanningShiftSwapStatusInvalid           = errors.New("planning_shift_swap_status_invalid")
	ErrPlanningShiftSwapInvalid                 = errors.New("planning_shift_swap_invalid")
	ErrPlanningShiftSwapConflict                = errors.New("planning_shift_swap_conflict")
	ErrPlanningLaborRuleNotFound                = errors.New("planning_labor_rule_not_found")
	ErrPlanningInvalidCountryCode               = errors.New("planning_invalid_country_code")
	ErrPlanningInvalidDate                      = errors.New("planning_invalid_date")
	ErrPlanningInvalidHours                     = errors.New("planning_invalid_hours")
	ErrMerchantUserNotFound                     = errors.New("merchant_user_not_found")
	ErrMerchantUserAlreadyLinked                = errors.New("merchant_user_already_linked")

	ErrRedisNotAvailable = errors.New("not_available")

	// Erreurs du module Kiosk (internal/modules/kiosk)
	ErrKioskEnrollmentCodeInvalid = errors.New("kiosk_enrollment_code_invalid")
	ErrKioskEnrollmentCodeExpired = errors.New("kiosk_enrollment_code_expired")
	ErrKioskEnrollmentCodeUsed    = errors.New("kiosk_enrollment_code_used")
	ErrKioskMaxKiosksReached      = errors.New("kiosk_max_kiosks_reached")
	ErrKioskDeviceTokenInvalid    = errors.New("kiosk_device_token_invalid")
	ErrKioskRevoked               = errors.New("kiosk_revoked")
	ErrKioskNotFound              = errors.New("kiosk_not_found")

	// Erreur du module Kiosk — reclaim par device_id (voir
	// docs/KIOSK_ENROLLMENT_RESILIENCE_AUDIT.md et docs/KIOSK_DECISIONS.md).
	// 0 candidat ou collision (>1) réutilisent volontairement ErrKioskNotFound
	// ci-dessus (même réponse HTTP dans les deux cas).
	ErrKioskReclaimPinRequired = errors.New("kiosk_reclaim_pin_required")

	// Erreurs du module Kiosk — incrément 2 (menu, commandes)
	ErrKioskProductNotFound         = errors.New("kiosk_product_not_found")
	ErrKioskProductUnavailable      = errors.New("kiosk_product_unavailable")
	ErrKioskFulfillmentTypeInvalid  = errors.New("kiosk_fulfillment_type_invalid")
	ErrKioskFulfillmentTypeDisabled = errors.New("kiosk_fulfillment_type_disabled")
	ErrKioskPayAtCounterDisabled    = errors.New("kiosk_pay_at_counter_disabled")
	ErrKioskOrderNotFound           = errors.New("kiosk_order_not_found")
	ErrKioskOrderNotCancellable     = errors.New("kiosk_order_not_cancellable")

	// Erreurs du module Kiosk — incrément 3 (CRUD bornes, enrollment codes, settings)
	ErrKioskEnrollmentCodeNotFound    = errors.New("kiosk_enrollment_code_not_found")
	ErrKioskEnrollmentCodeAlreadyUsed = errors.New("kiosk_enrollment_code_already_used")
	ErrKioskInvalidColor              = errors.New("kiosk_invalid_color")

	// Erreurs du module Kiosk — PIN admin par borne et nom obligatoire à l'enrôlement
	ErrKioskNameInvalid           = errors.New("kiosk_name_invalid")
	ErrKioskAdminPinInvalid       = errors.New("kiosk_admin_pin_invalid")
	ErrKioskAdminPinNotConfigured = errors.New("kiosk_admin_pin_not_configured")

	// Erreurs du module Kiosk — paiement carte (Stripe Terminal)
	ErrKioskCardPaymentDisabled   = errors.New("kiosk_card_payment_disabled")
	ErrKioskPaymentMethodInvalid  = errors.New("kiosk_payment_method_invalid")
	ErrKioskOrderNotCardPending   = errors.New("kiosk_order_not_card_pending")
	ErrKioskAmountMismatch        = errors.New("kiosk_amount_mismatch")
	ErrKioskTerminalNotConfigured = errors.New("kiosk_terminal_not_configured")

	// Erreurs de l'envoi de facture par email
	ErrInvoiceInvalidEmail       = errors.New("invoice_invalid_email")
	ErrInvoiceCustomerNotFound   = errors.New("invoice_customer_not_found")
	ErrInvoiceAttachmentTooLarge = errors.New("invoice_attachment_too_large")

	// Erreurs du plan de salle (module locations)
	ErrInvalidTableGeometry    = errors.New("invalid_table_geometry")
	ErrFloorNotEmpty           = errors.New("floor_not_empty")
	ErrFloorNotFound           = errors.New("floor_not_found")
	ErrInvalidObstacleGeometry = errors.New("invalid_obstacle_geometry")
	ErrInvalidAreaGeometry     = errors.New("invalid_area_geometry")

	// Erreurs du module réservation
	ErrTableConflict   = errors.New("table_conflict")
	ErrSlotUnavailable = errors.New("slot_unavailable")

	// Erreurs de la liste d'attente (module bookings, migration 059)
	ErrWaitlistDisabled = errors.New("waitlist_disabled")
	ErrWaitlistFull     = errors.New("waitlist_full")

	// Erreurs de doublon de nom (module menu)
	ErrProductNameAlreadyExists   = errors.New("product_name_already_exists")
	ErrComponentNameAlreadyExists = errors.New("component_name_already_exists")
	ErrAttributeNameAlreadyExists = errors.New("attribute_name_already_exists")

	// Variantes "avec confirmation par nouvel essai" (Redis actif) : le 1er
	// appel avec un nom en doublon renvoie cette erreur, un 2e appel identique
	// est accepté malgré le doublon.
	ErrProductNameAlreadyExistsWithRetry   = errors.New("product_name_already_exists_with_retry")
	ErrComponentNameAlreadyExistsWithRetry = errors.New("component_name_already_exists_with_retry")
	ErrAttributeNameAlreadyExistsWithRetry = errors.New("attribute_name_already_exists_with_retry")
)

// SendErrorJSON analyse l'erreur et envoie la réponse structurée appropriée
func SendErrorJSON(w http.ResponseWriter, module string, fnName string, err error) {
	status := http.StatusInternalServerError
	errorStatus := "internal_server_error"
	errorMsg := "internal_server_error"

	// Mapping des erreurs sentinelles vers les codes HTTP
	switch {
	case errors.Is(err, ErrInvalidRequestBody):
		status = http.StatusBadRequest
		errorStatus = "invalid_request_body"
		errorMsg = "The request body is invalid or malformed."

	case errors.Is(err, ErrMissingResourceID):
		status = http.StatusBadRequest
		errorStatus = "missing_resource_id"
		errorMsg = "A required resource id is missing."

	case errors.Is(err, ErrInvalidPage):
		status = http.StatusBadRequest
		errorStatus = "invalid_page"
		errorMsg = "The page parameter must be a valid integer."

	case errors.Is(err, ErrInvalidPageSize):
		status = http.StatusBadRequest
		errorStatus = "invalid_page_size"
		errorMsg = "The page_size parameter must be a valid integer."

	case errors.Is(err, ErrInvalidHACCPDate):
		status = http.StatusBadRequest
		errorStatus = "invalid_haccp_date"
		errorMsg = "The provided HACCP date is invalid."

	case errors.Is(err, ErrInvalidActivityType):
		status = http.StatusBadRequest
		errorStatus = "invalid_activity_type"
		errorMsg = "The activity type is invalid."

	case errors.Is(err, ErrInvalidActivityStatus):
		status = http.StatusBadRequest
		errorStatus = "invalid_activity_status"
		errorMsg = "The activity status is invalid."

	case errors.Is(err, ErrTemperatureZoneNameRequired):
		status = http.StatusBadRequest
		errorStatus = "temperature_zone_name_required"
		errorMsg = "The temperature zone name is required."

	case errors.Is(err, ErrTemperatureZoneInvalidRange):
		status = http.StatusBadRequest
		errorStatus = "temperature_zone_invalid_range"
		errorMsg = "The minimum temperature cannot be greater than the maximum temperature."

	case errors.Is(err, ErrTemperatureReadingsRequired):
		status = http.StatusBadRequest
		errorStatus = "temperature_readings_required"
		errorMsg = "At least one temperature reading is required."

	case errors.Is(err, ErrTemperatureZoneReferenceInvalid):
		status = http.StatusBadRequest
		errorStatus = "temperature_zone_reference_invalid"
		errorMsg = "One or more temperature zone references are invalid."

	case errors.Is(err, ErrTemperatureCorrectiveActionRequired):
		status = http.StatusUnprocessableEntity
		errorStatus = "temperature_corrective_action_required"
		errorMsg = "A corrective action comment is required for an out-of-range temperature."

	case errors.Is(err, ErrCorrectiveActionRequired):
		status = http.StatusUnprocessableEntity
		errorStatus = "corrective_action_required"
		errorMsg = "At least one corrective action is required for an out-of-range temperature."

	case errors.Is(err, ErrInvalidCorrectiveAction):
		status = http.StatusBadRequest
		errorStatus = "invalid_corrective_action"
		errorMsg = "One or more corrective actions are invalid."

	case errors.Is(err, ErrDuplicateCorrectiveAction):
		status = http.StatusBadRequest
		errorStatus = "duplicate_corrective_action"
		errorMsg = "A corrective action cannot be selected more than once for the same reading."

	case errors.Is(err, ErrCorrectiveActionNoteRequired):
		status = http.StatusUnprocessableEntity
		errorStatus = "corrective_action_note_required"
		errorMsg = "A corrective action note is required for the selected action."

	case errors.Is(err, ErrTooLateToDeleteOrder):
		status = http.StatusForbidden
		errorStatus = "too_late_to_delete_order"
		errorMsg = "Too late to delete order, you can only delete an order within 60 seconds after its creation"

	case errors.Is(err, ErrOrderAlreadyAccepted):
		status = http.StatusConflict
		errorStatus = "order_already_accepted"
		errorMsg = "This order has already been accepted by the restaurant and can no longer be cancelled"

	case errors.Is(err, ErrRedisNotAvailable):
		status = http.StatusServiceUnavailable
		errorStatus = "not_available"
		errorMsg = "Redis not available"

	case errors.Is(err, ErrMFAExpired):
		status = http.StatusUnauthorized
		errorStatus = "mfa_expired"
		errorMsg = "The MFA code was provided too recently. Please try again later."

	case errors.Is(err, ErrOTPWaitTime):
		status = http.StatusForbidden
		errorStatus = "otp_wait_time"
		errorMsg = "The OTP code was provided too recently. Please try again later."

	case errors.Is(err, ErrTranslationLanguagesLimitReached):
		status = http.StatusBadRequest
		errorStatus = "translation_languages_limit_reached"
		errorMsg = "Maximum of 4 translation languages per merchant reached"

	case errors.Is(err, ErrPlanningSettingsNotFound):
		status = http.StatusNotFound
		errorStatus = "planning_settings_not_found"
		errorMsg = "Planning settings not found"

	case errors.Is(err, ErrPlanningEmployeeNotFound):
		status = http.StatusNotFound
		errorStatus = "planning_employee_not_found"
		errorMsg = "Planning employee not found"

	case errors.Is(err, ErrPlanningPositionNotFound):
		status = http.StatusNotFound
		errorStatus = "planning_position_not_found"
		errorMsg = "Planning position not found"

	case errors.Is(err, ErrPlanningPositionLabelRequired):
		status = http.StatusBadRequest
		errorStatus = "planning_position_label_required"
		errorMsg = "The planning position label is required."

	case errors.Is(err, ErrPlanningPositionAlreadyExists):
		status = http.StatusConflict
		errorStatus = "planning_position_already_exists"
		errorMsg = "An active planning position already exists with this label."

	case errors.Is(err, ErrPlanningPositionInUse):
		status = http.StatusConflict
		errorStatus = "planning_position_in_use"
		errorMsg = "The planning position is still assigned to employees."

	case errors.Is(err, ErrPlanningWeekTemplateNotFound):
		status = http.StatusNotFound
		errorStatus = "planning_week_template_not_found"
		errorMsg = "Planning week template not found"

	case errors.Is(err, ErrPlanningWeekTemplateLabelRequired):
		status = http.StatusBadRequest
		errorStatus = "planning_week_template_label_required"
		errorMsg = "The week template label is required."

	case errors.Is(err, ErrPlanningWeekTemplateInvalidDayOfWeek):
		status = http.StatusBadRequest
		errorStatus = "planning_week_template_invalid_day_of_week"
		errorMsg = "The week template shift day_of_week must be between 0 and 6."

	case errors.Is(err, ErrPlanningWeekTemplateInvalidConflictMode):
		status = http.StatusBadRequest
		errorStatus = "planning_week_template_invalid_conflict_mode"
		errorMsg = "The conflict_mode must be one of: keep_existing, replace, template_to_unassigned."

	case errors.Is(err, ErrPlanningWeekTemplatePreviewRangeTooLarge):
		status = http.StatusBadRequest
		errorStatus = "planning_week_template_preview_range_too_large"
		errorMsg = "Preview target range is too large. Maximum is 26 weeks."

	case errors.Is(err, ErrPlanningShiftTemplateNotFound):
		status = http.StatusNotFound
		errorStatus = "planning_shift_template_not_found"
		errorMsg = "Planning shift template not found"

	case errors.Is(err, ErrPlanningShiftTemplateLabelRequired):
		status = http.StatusBadRequest
		errorStatus = "planning_shift_template_label_required"
		errorMsg = "The shift template label is required."

	case errors.Is(err, ErrPlanningShiftTemplateInvalidRange):
		status = http.StatusBadRequest
		errorStatus = "planning_shift_template_invalid_range"
		errorMsg = "The shift template time range is invalid."

	case errors.Is(err, ErrPlanningEmployeeNameRequired):
		status = http.StatusBadRequest
		errorStatus = "planning_employee_name_required"
		errorMsg = "The employee first name is required."

	case errors.Is(err, ErrPlanningEmployeeLastNameRequired):
		status = http.StatusBadRequest
		errorStatus = "planning_employee_last_name_required"
		errorMsg = "The employee last name is required."

	case errors.Is(err, ErrPlanningEmployeePositionRequired):
		status = http.StatusBadRequest
		errorStatus = "planning_employee_position_required"
		errorMsg = "The employee position is required."

	case errors.Is(err, ErrPlanningEmployeeContractTypeRequired):
		status = http.StatusBadRequest
		errorStatus = "planning_employee_contract_type_required"
		errorMsg = "The employee contract type is required."

	case errors.Is(err, ErrPlanningEmployeeContractTypeInvalid):
		status = http.StatusBadRequest
		errorStatus = "planning_employee_contract_type_invalid"
		errorMsg = "The employee contract type is invalid."

	case errors.Is(err, ErrPlanningAttendanceSourceInvalid):
		status = http.StatusBadRequest
		errorStatus = "planning_attendance_source_invalid"
		errorMsg = "The planning attendance source is invalid."

	case errors.Is(err, ErrPlanningShiftSwapApprovalModeInvalid):
		status = http.StatusBadRequest
		errorStatus = "planning_shift_swap_approval_mode_invalid"
		errorMsg = "The planning shift swap approval mode is invalid."

	case errors.Is(err, ErrPlanningPremiumCumulationModeInvalid):
		status = http.StatusBadRequest
		errorStatus = "planning_premium_cumulation_mode_invalid"
		errorMsg = "The planning premium cumulation mode is invalid."

	case errors.Is(err, ErrPlanningHolidayOverrideNotFound):
		status = http.StatusNotFound
		errorStatus = "planning_holiday_override_not_found"
		errorMsg = "Planning holiday override not found."

	case errors.Is(err, ErrPlanningEmployeeTimeTrackingModeInvalid):
		status = http.StatusBadRequest
		errorStatus = "planning_employee_time_tracking_mode_invalid"
		errorMsg = "The employee time tracking mode is invalid."

	case errors.Is(err, ErrPlanningEmployeeUserLinkInvalid):
		status = http.StatusBadRequest
		errorStatus = "planning_employee_user_link_invalid"
		errorMsg = "The linked user is invalid."

	case errors.Is(err, ErrPlanningEmployeeDocumentNotFound):
		status = http.StatusNotFound
		errorStatus = "planning_employee_document_not_found"
		errorMsg = "Planning employee document not found"

	case errors.Is(err, ErrPlanningEmployeeDocumentTypeInvalid):
		status = http.StatusBadRequest
		errorStatus = "planning_employee_document_type_invalid"
		errorMsg = "The employee document type is invalid."

	case errors.Is(err, ErrPlanningEmployeeDocumentNameRequired):
		status = http.StatusBadRequest
		errorStatus = "planning_employee_document_name_required"
		errorMsg = "The employee document name is required."

	case errors.Is(err, ErrPlanningEmployeeDocumentFileRequired):
		status = http.StatusBadRequest
		errorStatus = "planning_employee_document_file_required"
		errorMsg = "The employee document file is required."

	case errors.Is(err, ErrPlanningEmployeeDocumentUploadFailed):
		status = http.StatusInternalServerError
		errorStatus = "planning_employee_document_upload_failed"
		errorMsg = "The employee document upload failed."

	case errors.Is(err, ErrPlanningEmployeeDocumentUrlFailed):
		status = http.StatusInternalServerError
		errorStatus = "planning_employee_document_url_failed"
		errorMsg = "The employee document URL could not be generated."

	case errors.Is(err, ErrPlanningEmployeeUserNotLinkedToMerchant):
		status = http.StatusConflict
		errorStatus = "planning_employee_user_not_linked_to_merchant"
		errorMsg = "The selected user is not linked to the current merchant."

	case errors.Is(err, ErrPlanningEmployeeUserAlreadyAssigned):
		status = http.StatusConflict
		errorStatus = "planning_employee_user_already_assigned"
		errorMsg = "The selected user is already linked to another active employee."

	case errors.Is(err, ErrPlanningWeekAlreadyExists):
		status = http.StatusConflict
		errorStatus = "planning_week_already_exists"
		errorMsg = "An active planning week already exists for this start date."

	case errors.Is(err, ErrPlanningWeekNotFound):
		status = http.StatusNotFound
		errorStatus = "planning_week_not_found"
		errorMsg = "Planning week not found"

	case errors.Is(err, ErrPlanningShiftNotFound):
		status = http.StatusNotFound
		errorStatus = "planning_shift_not_found"
		errorMsg = "Planning shift not found"

	case errors.Is(err, ErrPlanningShiftConflict):
		status = http.StatusConflict
		errorStatus = "planning_shift_conflict"
		errorMsg = "The shift conflicts with an existing assignment."

	case errors.Is(err, ErrPlanningShiftInvalidRange):
		status = http.StatusBadRequest
		errorStatus = "planning_shift_invalid_range"
		errorMsg = "The shift time range is invalid."

	case errors.Is(err, ErrPlanningDayCommentNotFound):
		status = http.StatusNotFound
		errorStatus = "planning_day_comment_not_found"
		errorMsg = "Planning day comment not found"

	case errors.Is(err, ErrPlanningDayCommentTooLong):
		status = http.StatusBadRequest
		errorStatus = "planning_day_comment_too_long"
		errorMsg = "The day comment exceeds the maximum allowed length."

	case errors.Is(err, ErrPlanningTimeEntryNotFound):
		status = http.StatusNotFound
		errorStatus = "planning_time_entry_not_found"
		errorMsg = "Planning time entry not found"

	case errors.Is(err, ErrPlanningTimeEntryAlreadyOpen):
		status = http.StatusConflict
		errorStatus = "planning_time_entry_already_open"
		errorMsg = "A time entry is already open for this employee."

	case errors.Is(err, ErrPlanningTimeEntryNotOpen):
		status = http.StatusConflict
		errorStatus = "planning_time_entry_not_open"
		errorMsg = "No open time entry was found for this employee."

	case errors.Is(err, ErrPlanningTimeEntryInvalidRange):
		status = http.StatusBadRequest
		errorStatus = "planning_time_entry_invalid_range"
		errorMsg = "The time entry range is invalid."

	case errors.Is(err, ErrPlanningTimeEntrySourceDisabled):
		status = http.StatusConflict
		errorStatus = "planning_time_entry_source_disabled"
		errorMsg = "Time entries are disabled when the planning attendance source is set to planning."

	case errors.Is(err, ErrPlanningTimeEntryModeInvalid):
		status = http.StatusBadRequest
		errorStatus = "planning_time_entry_mode_invalid"
		errorMsg = "The time entry mode is invalid."

	case errors.Is(err, ErrPlanningTimeEntryShiftInvalid):
		status = http.StatusBadRequest
		errorStatus = "planning_time_entry_shift_invalid"
		errorMsg = "The linked shift is invalid for this time entry."

	case errors.Is(err, ErrPlanningLeaveRequestNotFound):
		status = http.StatusNotFound
		errorStatus = "planning_leave_request_not_found"
		errorMsg = "Planning leave request not found"

	case errors.Is(err, ErrPlanningLeaveTypeInvalid):
		status = http.StatusBadRequest
		errorStatus = "planning_leave_type_invalid"
		errorMsg = "The leave type is invalid."

	case errors.Is(err, ErrPlanningLeaveStatusInvalid):
		status = http.StatusBadRequest
		errorStatus = "planning_leave_status_invalid"
		errorMsg = "The leave request status is invalid."

	case errors.Is(err, ErrPlanningLeaveInvalidRange):
		status = http.StatusBadRequest
		errorStatus = "planning_leave_invalid_range"
		errorMsg = "The leave date range is invalid."

	case errors.Is(err, ErrPlanningLeaveShiftConflict):
		status = http.StatusConflict
		errorStatus = "planning_leave_shift_conflict"
		errorMsg = "The leave overlaps assigned shifts that must be resolved first."

	case errors.Is(err, ErrPlanningShiftSwapRequestNotFound):
		status = http.StatusNotFound
		errorStatus = "planning_shift_swap_request_not_found"
		errorMsg = "Planning shift swap request not found"

	case errors.Is(err, ErrPlanningShiftSwapApprovalForbidden):
		status = http.StatusForbidden
		errorStatus = "planning_shift_swap_approval_forbidden"
		errorMsg = "The current user cannot validate this shift swap request."

	case errors.Is(err, ErrPlanningShiftSwapStatusInvalid):
		status = http.StatusBadRequest
		errorStatus = "planning_shift_swap_status_invalid"
		errorMsg = "The shift swap request status is invalid."

	case errors.Is(err, ErrPlanningShiftSwapInvalid):
		status = http.StatusBadRequest
		errorStatus = "planning_shift_swap_invalid"
		errorMsg = "The shift swap request is invalid."

	case errors.Is(err, ErrPlanningShiftSwapConflict):
		status = http.StatusConflict
		errorStatus = "planning_shift_swap_conflict"
		errorMsg = "The shift swap would create a scheduling conflict."

	case errors.Is(err, ErrPlanningLaborRuleNotFound):
		status = http.StatusNotFound
		errorStatus = "planning_labor_rule_not_found"
		errorMsg = "Planning labor rule not found"

	case errors.Is(err, ErrPlanningInvalidCountryCode):
		status = http.StatusBadRequest
		errorStatus = "planning_invalid_country_code"
		errorMsg = "The country code is invalid."

	case errors.Is(err, ErrPlanningInvalidDate):
		status = http.StatusBadRequest
		errorStatus = "planning_invalid_date"
		errorMsg = "The date is invalid."

	case errors.Is(err, ErrPlanningInvalidHours):
		status = http.StatusBadRequest
		errorStatus = "planning_invalid_hours"
		errorMsg = "The hours value is invalid."

	case errors.Is(err, ErrMerchantUserNotFound):
		status = http.StatusNotFound
		errorStatus = "merchant_user_not_found"
		errorMsg = "The merchant user link was not found."

	case errors.Is(err, ErrMerchantUserAlreadyLinked):
		status = http.StatusConflict
		errorStatus = "merchant_user_already_linked"
		errorMsg = "The user is already linked to the current merchant."

	case errors.Is(err, ErrOTPMismatch):
		status = http.StatusUnauthorized
		errorStatus = "otp_mismatch"
		errorMsg = "The OTP code provided is not valid. Please try again."

	case errors.Is(err, ErrMFARequired):
		status = http.StatusUnauthorized
		errorStatus = "mfa_required"
		errorMsg = "MFA required, please try login"

	case errors.Is(err, ErrOrderOpen):
		status = http.StatusUnauthorized
		errorStatus = "order_open"
		errorMsg = "cannot permorm this action on an opened order"

	case errors.Is(err, ErrOrderClosed):
		status = http.StatusUnauthorized
		errorStatus = "order_closed"
		errorMsg = "cannot permorm this action on a closed order"

	case errors.Is(err, ErrUnauthorized):
		status = http.StatusUnauthorized
		errorStatus = "unauthorized"
		errorMsg = "unauthorized"

	case errors.Is(err, ErrNoCashRegisterOpen):
		status = http.StatusConflict
		errorStatus = "no_cash_register_opened"
		errorMsg = "no cash register opened for this device id"

	case errors.Is(err, ErrLinkedDeviceRegisterClosed):
		status = http.StatusConflict
		errorStatus = "linked_device_register_closed"
		errorMsg = "Le registre de caisse doit être ouvert sur l'appareil principal auquel vous êtes lié."

	case errors.Is(err, ErrCircularDeviceLink):
		status = http.StatusConflict
		errorStatus = "circular_device_link"
		errorMsg = "Impossible de se lier à cet appareil : il est déjà lié au vôtre. Supprimez d'abord la liaison existante sur l'autre appareil."

	case errors.Is(err, ErrCashRegisterStillOpen):
		status = http.StatusConflict
		errorStatus = "cash_register_still_open"
		errorMsg = "cannot perform this action on an opened cash register"

	case errors.Is(err, ErrForbidden):
		status = http.StatusForbidden
		errorStatus = "permission_denied"
		errorMsg = "permission_denied"

	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
		errorStatus = "not_found"
		errorMsg = "not_found"

	case errors.Is(err, ErrInvalidInput):
		status = http.StatusBadRequest
		errorStatus = "invalid_input"
		errorMsg = "Invalid input. Please check payloads and path parameters."

	case errors.Is(err, ErrInvalidData):
		status = http.StatusBadRequest
		errorStatus = "error_invalid_data"
		errorMsg = "The request data is invalid."

	case errors.Is(err, ErrValidationError):
		status = http.StatusBadRequest
		errorStatus = "validation_error"
		errorMsg = "The request data is invalid."

	case errors.Is(err, ErrCleaningZoneNameRequired):
		status = http.StatusBadRequest
		errorStatus = "cleaning_zone_name_required"
		errorMsg = "The cleaning zone name is required."

	case errors.Is(err, ErrCleaningSurfaceZoneRequired):
		status = http.StatusBadRequest
		errorStatus = "cleaning_surface_zone_required"
		errorMsg = "A cleaning surface must reference a zone."

	case errors.Is(err, ErrCleaningSurfaceNameRequired):
		status = http.StatusBadRequest
		errorStatus = "cleaning_surface_name_required"
		errorMsg = "The cleaning surface name is required."

	case errors.Is(err, ErrCleaningSurfaceInvalidFrequencyUnit):
		status = http.StatusBadRequest
		errorStatus = "cleaning_surface_invalid_frequency_unit"
		errorMsg = "The cleaning surface frequency unit must be day, week, or month."

	case errors.Is(err, ErrCleaningSurfaceInvalidFrequencyCount):
		status = http.StatusBadRequest
		errorStatus = "cleaning_surface_invalid_frequency_count"
		errorMsg = "The cleaning surface frequency count must be greater than zero."

	case errors.Is(err, ErrCleaningExecutionsRequired):
		status = http.StatusBadRequest
		errorStatus = "cleaning_executions_required"
		errorMsg = "At least one cleaning execution is required."

	case errors.Is(err, ErrCleaningSurfaceReferenceInvalid):
		status = http.StatusBadRequest
		errorStatus = "cleaning_surface_reference_invalid"
		errorMsg = "One or more cleaning surface references are invalid."

	case errors.Is(err, ErrDuplicateCleaningSurfaceExecution):
		status = http.StatusBadRequest
		errorStatus = "duplicate_cleaning_surface_execution"
		errorMsg = "A cleaning surface cannot be submitted more than once in the same session."

	case errors.Is(err, ErrCleaningSurfaceInactive):
		status = http.StatusUnprocessableEntity
		errorStatus = "cleaning_surface_inactive"
		errorMsg = "Cleaning session cannot be created with an inactive surface."

	case errors.Is(err, ErrCleaningPhotoRequired):
		status = http.StatusUnprocessableEntity
		errorStatus = "cleaning_photo_required"
		errorMsg = "A photo is required when the cleaning_photo setting is enabled."

	case errors.Is(err, ErrTemperatureFailurePhotoRequired):
		status = http.StatusUnprocessableEntity
		errorStatus = "temperature_failure_photo_required"
		errorMsg = "A photo is required for an out-of-range temperature reading."

	case errors.Is(err, ErrGoodsReceiptSupplierRequired):
		status = http.StatusBadRequest
		errorStatus = "goods_receipt_supplier_required"
		errorMsg = "The supplier field is required."

	case errors.Is(err, ErrGoodsReceiptProductTypeRequired):
		status = http.StatusBadRequest
		errorStatus = "goods_receipt_product_type_required"
		errorMsg = "The product type field is required."

	case errors.Is(err, ErrGoodsReceiptBatchNumberRequired):
		status = http.StatusBadRequest
		errorStatus = "goods_receipt_batch_number_required"
		errorMsg = "The batch number field is required."

	case errors.Is(err, ErrReceptionControlSampleRequired):
		status = http.StatusUnprocessableEntity
		errorStatus = "reception_control_sample_required"
		errorMsg = "A control sample is required by the current HACCP settings."

	case errors.Is(err, ErrReceptionNonConformitiesRequired):
		status = http.StatusUnprocessableEntity
		errorStatus = "reception_non_conformities_required"
		errorMsg = "At least one non-conformity is required by the current HACCP settings."

	case errors.Is(err, ErrReceptionInvoiceRequired):
		status = http.StatusUnprocessableEntity
		errorStatus = "reception_invoice_required"
		errorMsg = "An invoice photo is required by the current HACCP settings."

	case errors.Is(err, ErrUploadFileTooLargeOrInvalid):
		status = http.StatusBadRequest
		errorStatus = "upload_file_too_large_or_invalid"
		errorMsg = "The uploaded file is too large or invalid."

	case errors.Is(err, ErrUploadFileMissing):
		status = http.StatusBadRequest
		errorStatus = "upload_file_missing"
		errorMsg = "The file field is required."

	case errors.Is(err, ErrInvalidImageType):
		status = http.StatusBadRequest
		errorStatus = "invalid_image_type"
		errorMsg = "The uploaded image type is not supported."

	case errors.Is(err, ErrTraceabilityPhotosRequired):
		status = http.StatusBadRequest
		errorStatus = "traceability_photos_required"
		errorMsg = "At least one photo is required."

	case errors.Is(err, ErrTraceabilityTooManyPhotos):
		status = http.StatusBadRequest
		errorStatus = "traceability_too_many_photos"
		errorMsg = "A maximum of 10 photos is allowed per submission."

	case errors.Is(err, ErrInvalidInputPasswordTooShort):
		status = http.StatusBadRequest
		errorStatus = "password_too_short"
		errorMsg = "Le mot de passe doit faire au minimum 8 charactères"

	case errors.Is(err, ErrCannotDisableExternalPayments):
		status = http.StatusUnavailableForLegalReasons
		errorStatus = "cannot_disable_external_payments"
		errorMsg = "Cannot disable external payments"

	case errors.Is(err, ErrUserNotFound):
		status = http.StatusNotFound
		errorStatus = "user_not_found"
		errorMsg = "User not found"

	case errors.Is(err, ErrAccountDisabled):
		status = http.StatusForbidden
		errorStatus = "account_disabled"
		errorMsg = "User account disabled"

	case errors.Is(err, ErrUserNotAllowed):
		status = http.StatusForbidden
		errorStatus = "user_not_allowed"
		errorMsg = "User not allowed to perform this action"

	case errors.Is(err, ErrCartEmpty):
		status = http.StatusUnauthorized
		errorStatus = "cart_is_empty"
		errorMsg = "Cart is empty"

	case errors.Is(err, ErrRefoundMustBeGreaterThanZero):
		status = http.StatusBadRequest
		errorStatus = "refund_amount_must_be_greater_than_zero"
		errorMsg = "Refund amount must be greater than zero"

	case errors.Is(err, ErrReceiptNotFound):
		status = http.StatusNotFound
		errorStatus = "receipt_not_found"
		errorMsg = "Receipt not found"

	case errors.Is(err, ErrDeviceIDMissing):
		status = http.StatusBadRequest
		errorStatus = "device_id_missing"
		errorMsg = "Device ID missing"

	case errors.Is(err, ErrMOPMissing):
		status = http.StatusBadRequest
		errorStatus = "mop_missing"
		errorMsg = "Mean of payment missing"

	case errors.Is(err, ErrRefoundMustBeLowerThanOriginalReceipt):
		status = http.StatusBadRequest
		errorStatus = "refund_amount_must_be_lower_than_original_receipt"
		errorMsg = "Refund amount must be lower than original receipt"

	case errors.Is(err, ErrOrdersStillOpened):
		status = http.StatusUnauthorized
		errorStatus = "orders_still_opened"
		errorMsg = "Some orders created with this cash register are still opened"

	case errors.Is(err, ErrKioskEnrollmentCodeInvalid):
		status = http.StatusUnauthorized
		errorStatus = "kiosk_enrollment_code_invalid"
		errorMsg = "The enrollment code is invalid."

	case errors.Is(err, ErrKioskEnrollmentCodeExpired):
		status = http.StatusUnauthorized
		errorStatus = "kiosk_enrollment_code_expired"
		errorMsg = "The enrollment code has expired."

	case errors.Is(err, ErrKioskEnrollmentCodeUsed):
		status = http.StatusUnauthorized
		errorStatus = "kiosk_enrollment_code_used"
		errorMsg = "The enrollment code has already been used."

	case errors.Is(err, ErrKioskMaxKiosksReached):
		status = http.StatusForbidden
		errorStatus = "kiosk_max_kiosks_reached"
		errorMsg = "The maximum number of active kiosks for this merchant has been reached."

	case errors.Is(err, ErrKioskDeviceTokenInvalid):
		status = http.StatusUnauthorized
		errorStatus = "kiosk_device_token_invalid"
		errorMsg = "The kiosk device token is invalid or expired."

	case errors.Is(err, ErrKioskRevoked):
		status = http.StatusForbidden
		errorStatus = "kiosk_revoked"
		errorMsg = "This kiosk has been revoked."

	case errors.Is(err, ErrKioskNotFound):
		status = http.StatusNotFound
		errorStatus = "kiosk_not_found"
		errorMsg = "Kiosk not found."

	case errors.Is(err, ErrKioskReclaimPinRequired):
		status = http.StatusUnauthorized
		errorStatus = "kiosk_reclaim_pin_required"
		errorMsg = "Admin PIN required to reclaim this kiosk."

	case errors.Is(err, ErrKioskProductNotFound):
		status = http.StatusNotFound
		errorStatus = "kiosk_product_not_found"
		errorMsg = "Product not found."

	case errors.Is(err, ErrKioskProductUnavailable):
		status = http.StatusUnprocessableEntity
		errorStatus = "kiosk_product_unavailable"
		errorMsg = "One or more products are not available on this kiosk."

	case errors.Is(err, ErrKioskFulfillmentTypeInvalid):
		status = http.StatusBadRequest
		errorStatus = "kiosk_fulfillment_type_invalid"
		errorMsg = "fulfillment_type must be DINE_IN or TAKE_AWAY."

	case errors.Is(err, ErrKioskFulfillmentTypeDisabled):
		status = http.StatusUnprocessableEntity
		errorStatus = "kiosk_fulfillment_type_disabled"
		errorMsg = "This fulfillment type is disabled for this merchant."

	case errors.Is(err, ErrKioskPayAtCounterDisabled):
		status = http.StatusUnprocessableEntity
		errorStatus = "kiosk_pay_at_counter_disabled"
		errorMsg = "Pay-at-counter is disabled for this merchant."

	case errors.Is(err, ErrKioskOrderNotFound):
		status = http.StatusNotFound
		errorStatus = "kiosk_order_not_found"
		errorMsg = "Order not found."

	case errors.Is(err, ErrKioskOrderNotCancellable):
		status = http.StatusConflict
		errorStatus = "kiosk_order_not_cancellable"
		errorMsg = "This order can no longer be cancelled."

	case errors.Is(err, ErrKioskEnrollmentCodeNotFound):
		status = http.StatusNotFound
		errorStatus = "kiosk_enrollment_code_not_found"
		errorMsg = "Enrollment code not found or expired."

	case errors.Is(err, ErrKioskEnrollmentCodeAlreadyUsed):
		status = http.StatusConflict
		errorStatus = "kiosk_enrollment_code_already_used"
		errorMsg = "This enrollment code has already been used."

	case errors.Is(err, ErrKioskInvalidColor):
		status = http.StatusBadRequest
		errorStatus = "kiosk_invalid_color"
		errorMsg = "primary_color must be null or a valid hex color (#RRGGBB)."

	case errors.Is(err, ErrKioskNameInvalid):
		status = http.StatusBadRequest
		errorStatus = "kiosk_name_invalid"
		errorMsg = "name is required and must be at most 100 characters."

	case errors.Is(err, ErrKioskAdminPinInvalid):
		status = http.StatusUnauthorized
		errorStatus = "kiosk_admin_pin_invalid"
		errorMsg = "The admin PIN is invalid."

	case errors.Is(err, ErrKioskAdminPinNotConfigured):
		status = http.StatusNotFound
		errorStatus = "kiosk_admin_pin_not_configured"
		errorMsg = "This kiosk has no admin PIN configured yet. Regenerate one via POST .../regenerate-admin-pin."

	case errors.Is(err, ErrKioskCardPaymentDisabled):
		status = http.StatusForbidden
		errorStatus = "kiosk_card_payment_disabled"
		errorMsg = "Card payment is disabled for this merchant's kiosks."

	case errors.Is(err, ErrKioskPaymentMethodInvalid):
		status = http.StatusBadRequest
		errorStatus = "kiosk_payment_method_invalid"
		errorMsg = "payment_method must be either \"card\" or \"pay_at_counter\"."

	case errors.Is(err, ErrKioskOrderNotCardPending):
		status = http.StatusConflict
		errorStatus = "kiosk_order_not_card_pending"
		errorMsg = "This order is not awaiting a card payment (pending_card_payment)."

	case errors.Is(err, ErrKioskAmountMismatch):
		status = http.StatusBadRequest
		errorStatus = "kiosk_amount_mismatch"
		errorMsg = "amount_cents does not match the order total."

	case errors.Is(err, ErrKioskTerminalNotConfigured):
		status = http.StatusFailedDependency
		errorStatus = "kiosk_terminal_not_configured"
		errorMsg = "No Stripe connected account is configured for this merchant."

	case errors.Is(err, ErrInvoiceInvalidEmail):
		status = http.StatusBadRequest
		errorStatus = "invoice_invalid_email"
		errorMsg = "The provided email address is missing or invalid."

	case errors.Is(err, ErrInvoiceCustomerNotFound):
		status = http.StatusNotFound
		errorStatus = "invoice_customer_not_found"
		errorMsg = "The provided customer_id does not match any customer for this merchant."

	case errors.Is(err, ErrInvoiceAttachmentTooLarge):
		status = http.StatusBadGateway
		errorStatus = "invoice_attachment_too_large"
		errorMsg = "The generated invoice PDF exceeds Brevo's attachment size limit."

	case errors.Is(err, ErrInvalidTableGeometry):
		status = http.StatusUnprocessableEntity
		errorStatus = "invalid_table_geometry"
		errorMsg = "Table properties are out of bounds (x/y 0-1000, width/height 40-300, angle 0-359, seats >= 1, shape circle|square|rectangle)."

	case errors.Is(err, ErrFloorNotEmpty):
		status = http.StatusConflict
		errorStatus = "floor_not_empty"
		errorMsg = "The floor still has active tables attached. Move or delete them first."

	case errors.Is(err, ErrFloorNotFound):
		status = http.StatusNotFound
		errorStatus = "floor_not_found"
		errorMsg = "The requested floor does not exist for this merchant."

	case errors.Is(err, ErrInvalidObstacleGeometry):
		status = http.StatusUnprocessableEntity
		errorStatus = "invalid_obstacle_geometry"
		errorMsg = "Obstacle properties are out of bounds (x/y 0-1000, width/height 10-500, angle 0-359, direction only for type=door)."

	case errors.Is(err, ErrInvalidAreaGeometry):
		status = http.StatusUnprocessableEntity
		errorStatus = "invalid_area_geometry"
		errorMsg = "Area properties are invalid (name 1-50 chars, stroke_color/color must be hex #RRGGBB[AA], points >= 3, angle 0-359)."

	case errors.Is(err, ErrTableConflict):
		status = http.StatusConflict
		errorStatus = "table_conflict"
		errorMsg = "One or more requested tables are already booked on an overlapping time slot."

	case errors.Is(err, ErrSlotUnavailable):
		status = http.StatusConflict
		errorStatus = "slot_unavailable"
		errorMsg = "The requested slot no longer has enough remaining capacity."

	case errors.Is(err, ErrWaitlistDisabled):
		status = http.StatusUnprocessableEntity
		errorStatus = "waitlist_disabled"
		errorMsg = "The waitlist is not enabled for this merchant."

	case errors.Is(err, ErrWaitlistFull):
		status = http.StatusConflict
		errorStatus = "waitlist_full"
		errorMsg = "The waitlist has reached its maximum size."

	case errors.Is(err, ErrProductNameAlreadyExists):
		status = http.StatusConflict
		errorStatus = "product_name_already_exists"
		errorMsg = "A product with this name already exists."

	case errors.Is(err, ErrComponentNameAlreadyExists):
		status = http.StatusConflict
		errorStatus = "component_name_already_exists"
		errorMsg = "An ingredient with this name already exists."

	case errors.Is(err, ErrAttributeNameAlreadyExists):
		status = http.StatusConflict
		errorStatus = "attribute_name_already_exists"
		errorMsg = "A configuration attribute with this name already exists."

	case errors.Is(err, ErrProductNameAlreadyExistsWithRetry):
		status = http.StatusConflict
		errorStatus = "product_name_already_exists_with_retry"
		errorMsg = "A product with this name already exists. Submit the same request again to confirm and create it anyway."

	case errors.Is(err, ErrComponentNameAlreadyExistsWithRetry):
		status = http.StatusConflict
		errorStatus = "component_name_already_exists_with_retry"
		errorMsg = "An ingredient with this name already exists. Submit the same request again to confirm and create it anyway."

	case errors.Is(err, ErrAttributeNameAlreadyExistsWithRetry):
		status = http.StatusConflict
		errorStatus = "attribute_name_already_exists_with_retry"
		errorMsg = "A configuration attribute with this name already exists. Submit the same request again to confirm and create it anyway."

	default:
		// Pour les erreurs inconnues, on peut logguer l'erreur réelle ici
		errorStatus = err.Error()
	}

	logger.FromContext(context.Background()).Warn("error " + strconv.Itoa(status) + " " + module + "." + fnName + ": " + errorMsg + " - " + errorStatus)

	SendJSON(w, status, module, fnName, map[string]string{"status": errorStatus, "message": errorMsg, "error": errorMsg})
}
