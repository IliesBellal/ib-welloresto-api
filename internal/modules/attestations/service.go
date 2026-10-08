package attestations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"welloresto-api/internal/config"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/fiscalverify"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
	"welloresto-api/internal/version"
)

// integrityDays : période du contrôle d'intégrité préalable à chaque
// attestation (les 31 derniers jours, même borne que le contrôle lancé au
// back-office ; le contrôle complet de l'éditeur, cmd/verify_fiscal --all, est
// passé à la mise en production).
const integrityDays = 31

// editorSignature : signature du représentant légal de l'éditeur, apposée
// sur le volet 1 (remplaçable par une image du bucket privé,
// ATTESTATION_EDITOR_SIGNATURE_KEY).
//
//go:embed signature_editeur.png
var editorSignature []byte

// linkTTL : validité d'un lien de téléchargement.
const linkTTL = time.Hour

// Storage : bucket R2 privé (r2.Client).
type Storage interface {
	UploadPrivateFile(ctx context.Context, key string, file io.Reader, contentType string) (string, error)
	GetFile(ctx context.Context, key string) ([]byte, error)
	GenerateSignedURL(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// Mailer : envoi asynchrone avec pièce jointe (mailer.Service).
type Mailer interface {
	SendAsyncWithAttachment(fromName, fromEmail, to, subject, templateName string, data interface{}, attachmentBytes []byte, attachmentName string)
}

// Service génère, liste, délivre et envoie les attestations.
type Service struct {
	db      *sql.DB
	cfg     config.AttestationConfig
	storage Storage
	mail    Mailer
	now     func() time.Time
}

// NewService : storage et mail peuvent être nuls (interface nulle) ; la
// génération est alors indisponible.
func NewService(db *sql.DB, cfg config.AttestationConfig, storage Storage, mail Mailer) *Service {
	return &Service{db: db, cfg: cfg, storage: storage, mail: mail, now: time.Now}
}

// AttestationDTO : une attestation telle que listée.
type AttestationDTO struct {
	ID          int64     `json:"id"`
	Reference   string    `json:"reference"`
	Software    string    `json:"software"`
	Version     string    `json:"version"`
	MajorRoot   string    `json:"major_root"`
	Obsolete    bool      `json:"obsolete"` // racine majeure différente de la version en service
	CompanyName string    `json:"company_name"`
	SignerName  string    `json:"signer_name"`
	SignedAt    time.Time `json:"signed_at"`
	Filename    string    `json:"filename"`
	SHA256      string    `json:"sha256"`
	SizeBytes   int64     `json:"size_bytes"`
	GeneratedAt time.Time `json:"generated_at"`
}

func dto(a Attestation) AttestationDTO {
	return AttestationDTO{ID: a.ID, Reference: a.Content.Reference, Software: a.Software, Version: a.Version,
		MajorRoot: a.MajorRoot, Obsolete: a.MajorRoot != version.MajorRoot(), CompanyName: a.Content.CompanyName,
		SignerName: a.SignerName, SignedAt: a.SignedAt, Filename: a.Filename, SHA256: a.SHA256, SizeBytes: a.SizeBytes,
		GeneratedAt: a.GeneratedAt}
}

// Prefill : valeurs proposées pour le volet 2, modifiables par le signataire
// (sauf le SIRET).
type Prefill struct {
	CompanyName     string `json:"company_name"`
	Siret           string `json:"siret"`
	Address         string `json:"address"`
	City            string `json:"city"`
	AcquisitionDate string `json:"acquisition_date"` // AAAA-MM-JJ : création du compte
	UsageStartDate  string `json:"usage_start_date"` // AAAA-MM-JJ : premier ticket
}

// Overview répond à GET /accounting/attestations.
type Overview struct {
	Status            string           `json:"status"`
	Available         bool             `json:"available"`
	UnavailableReason string           `json:"unavailable_reason,omitempty"`
	Software          string           `json:"software"`
	Version           string           `json:"version"`
	MajorRoot         string           `json:"major_root"`
	Prefill           Prefill          `json:"prefill"`
	Attestations      []AttestationDTO `json:"attestations"`
}

// GenerateRequest : volet 2, complété et signé par le représentant légal.
type GenerateRequest struct {
	SignerName      string `json:"signer_name"`
	CompanyName     string `json:"company_name"`
	City            string `json:"city"`
	AcquisitionDate string `json:"acquisition_date"` // AAAA-MM-JJ
	UsageStartDate  string `json:"usage_start_date"` // AAAA-MM-JJ
	Certify         bool   `json:"certify"`
}

// GenerateResponse répond à POST /accounting/attestations.
type GenerateResponse struct {
	Status      string         `json:"status"`
	Attestation AttestationDTO `json:"attestation"`
	DownloadURL string         `json:"download_url"` // lien signé, une heure
}

// LinkResponse répond à GET /accounting/attestations/{id}/download.
type LinkResponse struct {
	Status      string `json:"status"`
	Filename    string `json:"filename"`
	SHA256      string `json:"sha256"`
	DownloadURL string `json:"download_url"`
}

// unavailable : raison pour laquelle la génération est fermée, nil sinon.
func (s *Service) unavailable(m merchantInfo) error {
	switch {
	case !s.cfg.Enabled:
		return models.ErrAttestationDisabled
	case !s.cfg.Complete() || s.storage == nil:
		return models.ErrAttestationNotConfigured
	case m.siret == "":
		return models.ErrAttestationSiretMissing
	}
	return nil
}

// Overview : disponibilité, valeurs proposées et attestations de
// l'établissement de l'utilisateur.
func (s *Service) Overview(ctx context.Context) (*Overview, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	db := dbx.GetDB(ctx, s.db)
	m, err := loadMerchant(ctx, db, user.MerchantID)
	if err != nil {
		return nil, err
	}
	loc, _ := time.LoadLocation(m.timezone)
	out := &Overview{Status: "1", Software: version.Product, Version: version.Version, MajorRoot: version.MajorRoot(),
		Prefill: prefill(m, loc), Attestations: []AttestationDTO{}}
	if reason := s.unavailable(m); reason != nil {
		out.UnavailableReason = models.UserMessage(reason)
	} else {
		out.Available = true
	}
	rows, err := list(ctx, db, user.MerchantID)
	if err != nil {
		return nil, err
	}
	for _, a := range rows {
		out.Attestations = append(out.Attestations, dto(a))
	}
	return out, nil
}

func prefill(m merchantInfo, loc *time.Location) Prefill {
	if loc == nil {
		loc = time.UTC
	}
	usage := m.created
	if m.firstReceipt.Valid {
		usage = m.firstReceipt.Time
	}
	return Prefill{CompanyName: m.name, Siret: m.siret, Address: m.address, City: m.city,
		AcquisitionDate: m.created.In(loc).Format("2006-01-02"), UsageStartDate: usage.In(loc).Format("2006-01-02")}
}

var spaces = regexp.MustCompile(`\s+`)

func clean(v string) string { return strings.TrimSpace(spaces.ReplaceAllString(v, " ")) }

func frDate(t time.Time) string { return t.Format("02/01/2006") }

// Generate établit l'attestation de l'établissement de l'utilisateur :
// garde-fous (ouverture, identité de l'éditeur, SIRET, volet 2 complet et
// certifié), contrôle d'intégrité des 31 derniers jours sans erreur, PDF,
// dépôt dans le bucket privé, ligne en base et entrée ATTESTATION_GENERATED
// au journal d'audit chaîné (dans la même transaction). Une génération à la
// fois par établissement.
func (s *Service) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	m, err := loadMerchant(ctx, dbx.GetDB(ctx, s.db), user.MerchantID)
	if err != nil {
		return nil, err
	}
	if err := s.unavailable(m); err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(m.timezone)
	if err != nil {
		loc = time.UTC
	}
	now := s.now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	signer, company, city := clean(req.SignerName), clean(req.CompanyName), clean(req.City)
	acquired, errA := time.Parse("2006-01-02", req.AcquisitionDate)
	usage, errU := time.Parse("2006-01-02", req.UsageStartDate)
	release, errR := time.Parse("2006-01-02", s.cfg.ReleaseDate)
	if errR != nil {
		return nil, models.ErrAttestationNotConfigured
	}
	if !req.Certify || signer == "" || company == "" || city == "" || len(signer) > 120 || len(company) > 200 ||
		len(city) > 100 || errA != nil || errU != nil || acquired.After(today) || usage.After(today) || usage.Before(acquired) {
		return nil, models.ErrAttestationInvalid
	}

	release2, ok, err := fiscal.TryLock(ctx, s.db, "attestation:"+user.MerchantID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, models.ErrAttestationBusy
	}
	defer release2()

	// Contrôle d'intégrité préalable (plan, garde-fou F2).
	from := today.AddDate(0, 0, -(integrityDays - 1))
	report, err := fiscalverify.Run(ctx, s.db, fiscalverify.Options{MerchantID: user.MerchantID, From: from, To: today, Archives: s.storage})
	if err != nil {
		return nil, err
	}
	if !report.OK() {
		return nil, models.ErrAttestationIntegrity
	}
	signature := editorSignature
	if s.cfg.EditorSignatureKey != "" {
		if signature, err = s.storage.GetFile(ctx, s.cfg.EditorSignatureKey); err != nil {
			return nil, fmt.Errorf("attestations: editor signature: %w", err)
		}
	}

	generatedAt := fiscal.Now()
	doc := Document{
		Reference:            fmt.Sprintf("ATT-%s-%s", user.MerchantID, generatedAt.In(loc).Format("20060102150405")),
		EditorRepresentative: s.cfg.EditorRepresentative,
		EditorCompany:        s.cfg.EditorCompany,
		EditorCity:           s.cfg.EditorCity,
		EditorSignedOn:       frDate(now),
		Software:             version.Product,
		SoftwareDescription:  SoftwareDescription,
		Version:              version.Version,
		MajorRoot:            version.MajorRoot(),
		MinorPattern:         version.MinorPattern(),
		ReleaseDate:          frDate(release),
		Licence:              "WR-" + user.MerchantID,
		Covered:              Covered,
		NotCovered:           NotCovered,
		MerchantID:           user.MerchantID,
		CompanyName:          company,
		Siret:                m.siret,
		Address:              m.address,
		City:                 city,
		SignerName:           signer,
		AcquisitionDate:      frDate(acquired),
		UsageStartDate:       frDate(usage),
		SignedOn:             frDate(now),
		SignedAt:             now.Format("02/01/2006 à 15:04") + " (" + m.timezone + ")",
		SignerAccount:        firstNonEmpty(user.Email, user.UserID),
		IntegrityCheck: fmt.Sprintf("Contrôle d'intégrité préalable des données fiscales du %s au %s : conforme (%d avertissement(s)).",
			frDate(from), frDate(today), report.Warnings),
	}
	pdf, err := RenderPDF(doc, signature)
	if err != nil {
		return nil, err
	}
	a := Attestation{
		MerchantID: user.MerchantID, Software: version.Product, Version: version.Version, MajorRoot: version.MajorRoot(),
		Content: doc, SignerName: signer, SignedByUserID: user.UserID, SignedAt: generatedAt,
		Filename:  fmt.Sprintf("%s_attestation_%s_%s_%s.pdf", version.Product, safePart(m.siret), version.Version, now.Format("20060102")),
		SHA256:    fmt.Sprintf("%x", sha256.Sum256(pdf)),
		SizeBytes: int64(len(pdf)), GeneratedAt: generatedAt,
	}
	a.R2Key = fmt.Sprintf("attestations/%s/%d_%s", user.MerchantID, generatedAt.UnixMicro(), a.Filename)
	if _, err := s.storage.UploadPrivateFile(ctx, a.R2Key, bytes.NewReader(pdf), "application/pdf"); err != nil {
		return nil, fmt.Errorf("attestations: upload: %w", err)
	}
	if err := dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, s.db)
		if err := insert(txCtx, db, &a); err != nil {
			return fmt.Errorf("attestations: insert: %w", err)
		}
		return s.audit(txCtx, db, user.MerchantID, user.UserID, models.ActionAttestationGenerated, a, map[string]any{
			"reference": doc.Reference, "version": a.Version, "sha256": a.SHA256, "signer_name": signer,
			"company_name": company, "integrity_warnings": report.Warnings,
		})
	}); err != nil {
		return nil, err
	}
	url, err := s.storage.GenerateSignedURL(ctx, a.R2Key, linkTTL)
	if err != nil {
		return nil, err
	}
	return &GenerateResponse{Status: "1", Attestation: dto(a), DownloadURL: url}, nil
}

func (s *Service) audit(ctx context.Context, db *dbx.DB, merchantID, userID, action string, a Attestation, values map[string]any) error {
	newValues, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return fiscal.AppendAuditLog(ctx, db, fiscal.AuditEntry{
		ID: helpers.GeneratePrefixedID(helpers.AuditLogIDPrefix), MerchantID: merchantID, UserID: userID,
		Action: action, ResourceType: models.ResourceAttestation, ResourceID: strconv.FormatInt(a.ID, 10), NewValues: newValues,
	})
}

// load : une attestation de l'établissement de l'utilisateur.
func (s *Service) load(ctx context.Context, id int64) (*Attestation, string, string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, "", "", models.ErrUnauthorized
	}
	if s.storage == nil {
		return nil, "", "", models.ErrAttestationNotConfigured
	}
	a, err := get(ctx, dbx.GetDB(ctx, s.db), user.MerchantID, id)
	if err != nil {
		return nil, "", "", err
	}
	if a == nil {
		return nil, "", "", models.ErrNotFound
	}
	return a, user.MerchantID, user.UserID, nil
}

// Link inscrit le téléchargement au journal d'audit puis renvoie un lien
// signé d'une heure.
func (s *Service) Link(ctx context.Context, id int64) (*LinkResponse, error) {
	a, merchantID, userID, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		return s.audit(txCtx, dbx.GetDB(txCtx, s.db), merchantID, userID, models.ActionAttestationDownload, *a,
			map[string]any{"reference": a.Content.Reference, "sha256": a.SHA256})
	}); err != nil {
		return nil, err
	}
	url, err := s.storage.GenerateSignedURL(ctx, a.R2Key, linkTTL)
	if err != nil {
		return nil, err
	}
	return &LinkResponse{Status: "1", Filename: a.Filename, SHA256: a.SHA256, DownloadURL: url}, nil
}

// Email envoie l'attestation en pièce jointe (au comptable, par exemple) et
// l'inscrit au journal d'audit.
func (s *Service) Email(ctx context.Context, id int64, to string) error {
	addr, err := mail.ParseAddress(strings.TrimSpace(to))
	if err != nil || s.mail == nil {
		return models.ErrAttestationEmailInvalid
	}
	a, merchantID, userID, err := s.load(ctx, id)
	if err != nil {
		return err
	}
	pdf, err := s.storage.GetFile(ctx, a.R2Key)
	if err != nil {
		return fmt.Errorf("attestations: read %s: %w", a.R2Key, err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(pdf)) != a.SHA256 {
		return fmt.Errorf("attestations: stored file %s does not match its sha256", a.R2Key)
	}
	if err := dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		return s.audit(txCtx, dbx.GetDB(txCtx, s.db), merchantID, userID, models.ActionAttestationSent, *a,
			map[string]any{"reference": a.Content.Reference, "sha256": a.SHA256, "to": addr.Address})
	}); err != nil {
		return err
	}
	s.mail.SendAsyncWithAttachment("Wello Resto", mailer.SupportEmail, addr.Address,
		"Attestation de conformité du logiciel de caisse — "+a.Content.CompanyName, "attestation_email.html",
		map[string]string{"MerchantName": a.Content.CompanyName, "Software": a.Software, "Version": a.Version,
			"SupportEmail": mailer.SupportEmail}, pdf, a.Filename)
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func safePart(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '-' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "inconnu"
	}
	return b.String()
}
