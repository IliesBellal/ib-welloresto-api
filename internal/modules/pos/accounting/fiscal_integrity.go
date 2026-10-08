package accounting

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/fiscalverify"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

// Contrôle d'intégrité lancé par le restaurateur depuis le back-office
// (conformité caisse lot E, phase 4 ; BOI §100 : « détecter et démontrer »).
// Même contrôle que cmd/verify_fiscal (internal/fiscalverify), sur une
// période d'au plus fiscalIntegrityMaxDays jours, en lecture seule.

// fiscalIntegrityMaxDays borne un contrôle lancé depuis le back-office : il
// s'exécute dans la requête. Les contrôles plus longs passent par
// cmd/verify_fiscal.
const fiscalIntegrityMaxDays = 31

// FiscalIntegrityRequest : période, jours locaux inclus.
type FiscalIntegrityRequest struct {
	DateFrom string `json:"date_from"` // YYYY-MM-DD
	DateTo   string `json:"date_to"`   // YYYY-MM-DD
}

// FiscalIntegrityResponse répond à POST /accounting/fiscal-integrity : le
// rapport structuré et sa version texte, à télécharger.
type FiscalIntegrityResponse struct {
	Status string               `json:"status"`
	Report *fiscalverify.Report `json:"report"`
	Text   string               `json:"text"`
}

// fiscalArchiveFetcher : relecture des fichiers d'archive (client R2 privé).
type fiscalArchiveFetcher interface {
	GetFile(ctx context.Context, key string) ([]byte, error)
}

// VerifyFiscalIntegrity contrôle l'établissement de l'utilisateur sur la
// période. Un seul contrôle à la fois par établissement.
func (s *AccountingService) VerifyFiscalIntegrity(ctx context.Context, req FiscalIntegrityRequest, files fiscalArchiveFetcher) (*FiscalIntegrityResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	start, errStart := time.Parse("2006-01-02", req.DateFrom)
	end, errEnd := time.Parse("2006-01-02", req.DateTo)
	if errStart != nil || errEnd != nil || end.Before(start) || end.Sub(start) >= fiscalIntegrityMaxDays*24*time.Hour {
		return nil, models.ErrFiscalPeriodInvalid
	}
	release, ok, err := fiscal.TryLock(ctx, s.repo.database, "fiscal:integrity:"+user.MerchantID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, models.ErrFiscalIntegrityBusy
	}
	defer release()
	opts := fiscalverify.Options{MerchantID: user.MerchantID, From: start, To: end}
	if files != nil {
		opts.Archives = files
	}
	report, err := fiscalverify.Run(ctx, s.repo.database, opts)
	if err != nil {
		return nil, err
	}
	var text strings.Builder
	report.WriteText(&text)
	return &FiscalIntegrityResponse{Status: "1", Report: report, Text: text.String()}, nil
}

// VerifyFiscalIntegrity POST /accounting/fiscal-integrity
func (h *AccountingHandler) VerifyFiscalIntegrity(w http.ResponseWriter, r *http.Request) {
	var req FiscalIntegrityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "accounting", "fiscal_integrity", models.ErrInvalidRequestBody)
		return
	}
	var files fiscalArchiveFetcher
	if h.r2Client != nil {
		files = h.r2Client
	}
	resp, err := h.service.VerifyFiscalIntegrity(r.Context(), req, files)
	if err != nil {
		models.SendErrorJSON(w, "accounting", "fiscal_integrity", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "accounting", "fiscal_integrity", resp)
}
