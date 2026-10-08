package accounting

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/fiscalarchive"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"

	"github.com/go-chi/chi/v5"
)

// Archives fiscales (conformité caisse lot D, docs/attestation-conformite-05-lot-D-brief.md,
// phase 4) : liste, lien de téléchargement tracé au journal d'audit et
// génération à la demande d'une période close. Les archives mensuelles sont
// produites par la tâche horaire RunFiscalArchives.

// fiscalArchiveMaxDays borne une archive à la demande : la génération est
// synchrone (dans la requête) et son coût suit celui d'un mois automatique.
// Les périodes plus longues sont couvertes par les archives mensuelles.
const fiscalArchiveMaxDays = 31

// FiscalArchive est une archive telle que listée au back-office (sans la clé
// R2 interne).
type FiscalArchive struct {
	ID              int64     `json:"id"`
	Kind            string    `json:"kind"`         // MONTH (automatique) | PERIOD (à la demande)
	PeriodStart     string    `json:"period_start"` // YYYY-MM-DD, jour local, inclus
	PeriodEnd       string    `json:"period_end"`   // YYYY-MM-DD, jour local, inclus
	Filename        string    `json:"filename"`
	SHA256          string    `json:"sha256"`          // empreinte du ZIP
	ManifestSHA256  string    `json:"manifest_sha256"` // empreinte de MANIFEST.json
	SizeBytes       int64     `json:"size_bytes"`
	SoftwareVersion string    `json:"software_version"`
	GeneratedBy     string    `json:"generated_by"` // SYSTEM ou identifiant de l'utilisateur
	GeneratedAt     time.Time `json:"generated_at"`
	Hash            string    `json:"hash"` // maillon de la chaîne fiscal_archives
}

func fiscalArchiveDTO(a fiscalarchive.Archive) FiscalArchive {
	return FiscalArchive{
		ID: a.ID, Kind: a.Kind, PeriodStart: a.PeriodStart, PeriodEnd: a.PeriodEnd, Filename: a.Filename,
		SHA256: a.SHA256, ManifestSHA256: a.ManifestSHA256, SizeBytes: a.SizeBytes,
		SoftwareVersion: a.SoftwareVersion, GeneratedBy: a.GeneratedBy, GeneratedAt: a.GeneratedAt, Hash: a.Hash,
	}
}

// ListFiscalArchivesResponse répond à GET /accounting/fiscal-archives.
type ListFiscalArchivesResponse struct {
	Status   string          `json:"status"`
	Archives []FiscalArchive `json:"archives"`
}

// FiscalArchiveResponse répond à POST /accounting/fiscal-archives.
type FiscalArchiveResponse struct {
	Status  string        `json:"status"`
	Archive FiscalArchive `json:"archive"`
}

// FiscalArchiveLinkResponse répond à GET /accounting/fiscal-archives/{id}/download.
type FiscalArchiveLinkResponse struct {
	Status      string `json:"status"`
	ArchiveID   int64  `json:"archive_id"`
	Filename    string `json:"filename"`
	SHA256      string `json:"sha256"`
	DownloadURL string `json:"download_url"` // lien signé, valable une heure
}

// GenerateFiscalArchiveRequest : période close, bornes incluses.
type GenerateFiscalArchiveRequest struct {
	DateFrom string `json:"date_from"` // YYYY-MM-DD
	DateTo   string `json:"date_to"`   // YYYY-MM-DD
}

// fiscalArchiveStorage est la partie du client R2 privé utilisée par les
// archives : dépôt et lien signé.
type fiscalArchiveStorage interface {
	fiscalarchive.Store
	accountingExportSigner
}

// ListFiscalArchives liste les archives de l'établissement de l'utilisateur,
// la plus récente d'abord.
func (s *AccountingService) ListFiscalArchives(ctx context.Context) (*ListFiscalArchivesResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	archives, err := fiscalarchive.List(ctx, dbx.GetDB(ctx, s.repo.database), user.MerchantID)
	if err != nil {
		return nil, err
	}
	out := make([]FiscalArchive, 0, len(archives))
	for _, a := range archives {
		out = append(out, fiscalArchiveDTO(a))
	}
	return &ListFiscalArchivesResponse{Status: "1", Archives: out}, nil
}

// FiscalArchiveLink inscrit la demande au journal d'audit (chaîné et signé)
// puis renvoie un lien signé d'une heure. Sans écriture au journal, pas de
// lien : chaque accès à une archive est tracé.
func (s *AccountingService) FiscalArchiveLink(ctx context.Context, archiveID int64, storage fiscalArchiveStorage) (*FiscalArchiveLinkResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if storage == nil {
		return nil, models.ErrFiscalArchiveStorage
	}
	a, err := fiscalarchive.Get(ctx, dbx.GetDB(ctx, s.repo.database), user.MerchantID, archiveID)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, models.ErrNotFound
	}
	newValues, err := json.Marshal(map[string]string{
		"filename": a.Filename, "sha256": a.SHA256, "period_start": a.PeriodStart, "period_end": a.PeriodEnd,
	})
	if err != nil {
		return nil, err
	}
	if err := dbutils.RunInTx(ctx, s.repo.database, func(txCtx context.Context) error {
		return fiscal.AppendAuditLog(txCtx, dbx.GetDB(txCtx, s.repo.database), fiscal.AuditEntry{
			ID:           helpers.GeneratePrefixedID(helpers.AuditLogIDPrefix),
			MerchantID:   user.MerchantID,
			UserID:       user.UserID,
			Action:       models.ActionFiscalArchiveDownload,
			ResourceType: models.ResourceFiscalArchive,
			ResourceID:   strconv.FormatInt(a.ID, 10),
			NewValues:    newValues,
		})
	}); err != nil {
		return nil, err
	}
	url, err := storage.GenerateSignedURL(ctx, a.R2Key, accountingExportLinkTTL)
	if err != nil {
		return nil, err
	}
	return &FiscalArchiveLinkResponse{Status: "1", ArchiveID: a.ID, Filename: a.Filename, SHA256: a.SHA256, DownloadURL: url}, nil
}

// GenerateFiscalArchive produit une archive à la demande sur une période
// close d'au plus fiscalArchiveMaxDays jours. Une seule génération à la fois
// par établissement : une seconde demande simultanée est refusée.
func (s *AccountingService) GenerateFiscalArchive(ctx context.Context, req GenerateFiscalArchiveRequest, storage fiscalArchiveStorage) (*FiscalArchiveResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if storage == nil {
		return nil, models.ErrFiscalArchiveStorage
	}
	start, errStart := time.Parse("2006-01-02", req.DateFrom)
	end, errEnd := time.Parse("2006-01-02", req.DateTo)
	if errStart != nil || errEnd != nil || end.Before(start) || end.Sub(start) >= fiscalArchiveMaxDays*24*time.Hour {
		return nil, models.ErrFiscalPeriodInvalid
	}
	release, ok, err := fiscal.TryLock(ctx, s.repo.database, "fiscal:archives-on-demand:"+user.MerchantID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, models.ErrFiscalArchiveBusy
	}
	defer release()
	a, err := fiscalarchive.Generate(ctx, s.repo.database, storage, fiscalarchive.Request{
		MerchantID: user.MerchantID, Kind: fiscalarchive.KindPeriod, Start: start, End: end, GeneratedBy: user.UserID,
	})
	if errors.Is(err, fiscalarchive.ErrPeriodNotClosed) {
		return nil, models.ErrFiscalPeriodNotClosed
	}
	if err != nil {
		return nil, err
	}
	return &FiscalArchiveResponse{Status: "1", Archive: fiscalArchiveDTO(*a)}, nil
}

// storage renvoie le client R2 privé comme interface, nulle (et non pointeur
// nul typé) s'il n'a pas pu être créé.
func (h *AccountingHandler) storage() fiscalArchiveStorage {
	if h.r2Client == nil {
		return nil
	}
	return h.r2Client
}

// ListFiscalArchives GET /accounting/fiscal-archives
func (h *AccountingHandler) ListFiscalArchives(w http.ResponseWriter, r *http.Request) {
	resp, err := h.service.ListFiscalArchives(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "accounting", "fiscal_archives_list", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "accounting", "fiscal_archives_list", resp)
}

// FiscalArchiveDownload GET /accounting/fiscal-archives/{archive_id}/download
// — nouveau lien signé (une heure), tracé au journal d'audit.
func (h *AccountingHandler) FiscalArchiveDownload(w http.ResponseWriter, r *http.Request) {
	archiveID, err := strconv.ParseInt(chi.URLParam(r, "archive_id"), 10, 64)
	if err != nil {
		models.SendErrorJSON(w, "accounting", "fiscal_archive_download", models.ErrMissingResourceID)
		return
	}
	resp, err := h.service.FiscalArchiveLink(r.Context(), archiveID, h.storage())
	if err != nil {
		models.SendErrorJSON(w, "accounting", "fiscal_archive_download", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "accounting", "fiscal_archive_download", resp)
}

// GenerateFiscalArchive POST /accounting/fiscal-archives — archive à la
// demande d'une période close ({"date_from","date_to"}, 31 jours au plus).
func (h *AccountingHandler) GenerateFiscalArchive(w http.ResponseWriter, r *http.Request) {
	var req GenerateFiscalArchiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "accounting", "fiscal_archive_generate", models.ErrInvalidRequestBody)
		return
	}
	resp, err := h.service.GenerateFiscalArchive(r.Context(), req, h.storage())
	if err != nil {
		models.SendErrorJSON(w, "accounting", "fiscal_archive_generate", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "accounting", "fiscal_archive_generate", resp)
}
