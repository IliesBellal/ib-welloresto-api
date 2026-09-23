package stripeclient

import "github.com/stripe/stripe-go/v84"

// PaymentStatusInput porte tous les éléments nécessaires à NormalizePaymentStatus,
// déjà résolus par l'appelant (aucun appel Stripe/DB ici — fonction pure).
type PaymentStatusInput struct {
	// LocalStatus est stripe_payments.payment_intent_status
	// (REQUIRES_CONFIRMATION/CAPTURED/CANCELED/FAILED/TO_REFUND).
	LocalStatus string
	// PIStatus est le statut Stripe (live ou déjà connu) du PaymentIntent.
	PIStatus stripe.PaymentIntentStatus
	// HasLastPaymentError / LastPaymentErrorCode reflètent pi.LastPaymentError.
	HasLastPaymentError  bool
	LastPaymentErrorCode string
	// ReaderActionStatus est nil si aucune action reader ne correspond à ce
	// PaymentIntent précis (reader non appairé, action sur un autre PI, ou
	// aucune action en cours).
	ReaderActionStatus      *stripe.TerminalReaderActionStatus
	ReaderActionFailureCode string
}

// CardPresentDetails — détails carte affichables, extraits d'un Charge
// card_present (voir ExtractCardPresentDetails).
type CardPresentDetails struct {
	Brand                    string
	Last4                    string
	ApplicationPreferredName string
	DedicatedFileName        string
	AuthorizationCode        string
}

// PaymentStatus est la réponse consolidée du contrat
// (docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md) — assemblée par
// TerminalService.GetPaymentStatus, reprise telle quelle par les handlers
// kiosk et par le push WebSocket.
type PaymentStatus struct {
	OrderID         string
	PaymentIntentID string
	Status          string // waiting_for_card|processing|succeeded|failed|canceled
	FailureCode     *string
	FailureMessage  *string
	CardPresent     *CardPresentDetails
}

// NormalizePaymentStatus implémente, dans cet ordre de priorité (la première
// règle qui matche gagne — reflète exactement
// docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md, sections Normalisation + Dispatch),
// la table de normalisation server-driven du paiement carte Kiosk.
//
// Principe directeur : dans le doute, "processing", jamais "failed" — un
// faux "failed" invite le client à relancer un paiement peut-être déjà en
// cours ; un faux "processing" ne coûte qu'une itération de polling de plus.
//
//  1. LocalStatus ∈ {CAPTURED, TO_REFUND} OU PIStatus ∈ {succeeded,
//     requires_capture}                                                  → "succeeded"
//     (requires_capture = paiement autorisé, capture différée par le cron
//     CapturePayments — voir docs/KIOSK_DECISIONS.md, "Capture différée
//     Terminal" — c'est déjà un succès du point de vue borne/cuisine,
//     seule la capture bancaire réelle est reportée)
//  2. PIStatus == processing                                             → "processing"
//  3. PIStatus == canceled                                               → "canceled"
//  4. ReaderActionStatus == in_progress                                  → "waiting_for_card"
//  5. ReaderActionStatus == succeeded sur ce PI, PIStatus pas encore
//     "succeeded" (fenêtre transitoire fin d'action / MAJ PI)            → "processing"
//  6. ReaderActionStatus == failed OU HasLastPaymentError                → "failed"
//  7. PIStatus == requires_payment_method (rien au-dessus — aucune action
//     jamais dispatchée sur ce PI, diagnostic net)                       → "failed", code "reader_action_missing"
//  8. fallback (état Stripe inattendu, ex. requires_confirmation/
//     requires_action sans action en cours)                              → "processing", pas de code
func NormalizePaymentStatus(in PaymentStatusInput) (status string, failureCode *string) {
	if in.LocalStatus == "CAPTURED" || in.LocalStatus == "TO_REFUND" ||
		in.PIStatus == stripe.PaymentIntentStatusSucceeded || in.PIStatus == stripe.PaymentIntentStatusRequiresCapture {
		return "succeeded", nil
	}
	if in.PIStatus == stripe.PaymentIntentStatusProcessing {
		return "processing", nil
	}
	if in.PIStatus == stripe.PaymentIntentStatusCanceled {
		return "canceled", nil
	}
	if in.ReaderActionStatus != nil && *in.ReaderActionStatus == stripe.TerminalReaderActionStatusInProgress {
		return "waiting_for_card", nil
	}
	if in.ReaderActionStatus != nil && *in.ReaderActionStatus == stripe.TerminalReaderActionStatusSucceeded {
		return "processing", nil
	}
	if in.ReaderActionStatus != nil && *in.ReaderActionStatus == stripe.TerminalReaderActionStatusFailed {
		code := in.ReaderActionFailureCode
		return "failed", &code
	}
	if in.HasLastPaymentError {
		code := in.LastPaymentErrorCode
		return "failed", &code
	}
	if in.PIStatus == stripe.PaymentIntentStatusRequiresPaymentMethod {
		code := "reader_action_missing"
		return "failed", &code
	}
	return "processing", nil
}

// FailureMessageFR mappe un failure_code vers un message FR prêt à
// afficher. Minimal et extensible délibérément :
// TerminalReaderAction.FailureCode est un string libre côté SDK (pas un
// enum), donc seuls les codes confirmés dans le SDK vendored
// (stripe.ErrorCodeTerminalReaderBusy/Offline/Timeout/HardwareFault/
// InvalidLocationForPayment) et nos codes synthétiques
// ("reader_action_missing") sont mappés explicitement ; tout code Stripe non
// listé (ex. un decline_code carte) retombe sur un message générique.
func FailureMessageFR(code string) string {
	switch code {
	case string(stripe.ErrorCodeTerminalReaderBusy):
		return "Le lecteur est occupé par une autre opération. Réessayez dans quelques secondes."
	case string(stripe.ErrorCodeTerminalReaderOffline):
		return "Le lecteur de carte est hors ligne. Vérifiez sa connexion et réessayez."
	case string(stripe.ErrorCodeTerminalReaderTimeout):
		return "Le lecteur n'a pas répondu à temps. Réessayez."
	case string(stripe.ErrorCodeTerminalReaderHardwareFault):
		return "Le lecteur rencontre un problème matériel. Réessayez ou passez en caisse."
	case string(stripe.ErrorCodeTerminalReaderInvalidLocationForPayment):
		return "Le lecteur n'est pas configuré pour cet établissement. Contactez le support."
	case "reader_action_missing":
		return "Le paiement n'a pas encore été envoyé au lecteur. Réessayez."
	default:
		return "Le paiement par carte a échoué. Réessayez ou passez en caisse."
	}
}

// ExtractCardPresentDetails lit les champs carte affichables depuis un
// Charge card_present (pi.LatestCharge après relecture avec
// Expand: []*string{"latest_charge"}). Retourne nil si le charge ou les
// détails card_present sont absents (ex. relecture échouée, PI non
// card_present).
func ExtractCardPresentDetails(charge *stripe.Charge) *CardPresentDetails {
	if charge == nil || charge.PaymentMethodDetails == nil || charge.PaymentMethodDetails.CardPresent == nil {
		return nil
	}
	cp := charge.PaymentMethodDetails.CardPresent
	details := &CardPresentDetails{
		Brand: string(cp.Brand),
		Last4: cp.Last4,
	}
	if cp.Receipt != nil {
		details.ApplicationPreferredName = cp.Receipt.ApplicationPreferredName
		details.DedicatedFileName = cp.Receipt.DedicatedFileName
		details.AuthorizationCode = cp.Receipt.AuthorizationCode
	}
	return details
}
