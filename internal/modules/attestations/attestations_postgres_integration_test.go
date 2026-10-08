//go:build postgres_integration

package attestations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/config"
	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/version"
)

type memStorage struct{ files map[string][]byte }

func (m *memStorage) UploadPrivateFile(_ context.Context, key string, file io.Reader, _ string) (string, error) {
	b, err := io.ReadAll(file)
	m.files[key] = b
	return key, err
}

func (m *memStorage) GetFile(_ context.Context, key string) ([]byte, error) {
	b, ok := m.files[key]
	if !ok {
		return nil, errors.New("absent: " + key)
	}
	return b, nil
}

func (m *memStorage) GenerateSignedURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://signed.example/" + key, nil
}

type sentMail struct {
	to, template string
	attachment   []byte
}

type memMailer struct{ sent []sentMail }

func (m *memMailer) SendAsyncWithAttachment(_, _, to, _, templateName string, _ interface{}, attachment []byte, _ string) {
	m.sent = append(m.sent, sentMail{to: to, template: templateName, attachment: attachment})
}

// Attestation (lot F, phase 2) : garde-fous, génération, liste, lien, envoi,
// journal d'audit, cloisonnement par établissement.
func TestAttestations_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const siret = "siret-itest-attestation"

	cleanup := func(mid string) {
		for _, q := range []string{
			`DELETE FROM attestations WHERE merchant_id = $1`,
			`DELETE FROM audit_logs WHERE merchant_id = $1`,
			`DELETE FROM receipts WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id::text = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var old int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = $1 OR token = 'mt-attestation' LIMIT 1`, siret).Scan(&old); err == nil {
		cleanup(strconv.FormatInt(old, 10))
	}
	var mid int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, creation_date)
		VALUES ('Pizzeria ITest', 'a', '1', 'rue de la Paix', '75001', 'Paris', '', 'https://x', '06', 'mt-attestation', 'Europe/Paris', '2026-03-01')
		RETURNING id`).Scan(&mid); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(mid, 10)
	t.Cleanup(func() { cleanup(merchantID) })

	var sig bytes.Buffer
	_ = png.Encode(&sig, image.NewRGBA(image.Rect(0, 0, 60, 20)))
	storage := &memStorage{files: map[string][]byte{"editor/signature.png": sig.Bytes()}}
	mails := &memMailer{}
	cfg := config.AttestationConfig{Enabled: true, EditorRepresentative: "BELLAL Ilies", EditorCompany: "BINYA",
		EditorCity: "Metz", EditorSignatureKey: "editor/signature.png", ReleaseDate: "2026-07-15"}
	userCtx := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: "itest-att-user", MerchantID: merchantID, Email: "gerant@example.fr"})
	valid := GenerateRequest{SignerName: "MARTIN Paul", CompanyName: "Pizzeria ITest SARL", City: "Paris",
		AcquisitionDate: "2026-03-01", UsageStartDate: "2026-03-15", Certify: true}

	// Garde-fous, dans l'ordre : fermée, éditeur incomplet, SIRET absent.
	closed := cfg
	closed.Enabled = false
	if _, err := NewService(db, closed, storage, mails).Generate(userCtx, valid); !errors.Is(err, models.ErrAttestationDisabled) {
		t.Fatalf("disabled: %v", err)
	}
	incomplete := cfg
	incomplete.EditorCompany = ""
	if _, err := NewService(db, incomplete, storage, mails).Generate(userCtx, valid); !errors.Is(err, models.ErrAttestationNotConfigured) {
		t.Fatalf("incomplete config: %v", err)
	}
	if _, err := NewService(db, cfg, nil, mails).Generate(userCtx, valid); !errors.Is(err, models.ErrAttestationNotConfigured) {
		t.Fatalf("no storage: %v", err)
	}
	svc := NewService(db, cfg, storage, mails)
	if ov, err := svc.Overview(userCtx); err != nil || ov.Available || ov.UnavailableReason == "" || ov.Prefill.AcquisitionDate != "2026-03-01" {
		t.Fatalf("overview without SIRET = (%+v, %v)", ov, err)
	}
	if _, err := svc.Generate(userCtx, valid); !errors.Is(err, models.ErrAttestationSiretMissing) {
		t.Fatalf("no siret: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE merchant SET siret = $2 WHERE id::text = $1`, merchantID, siret); err != nil {
		t.Fatal(err)
	}

	// Volet 2 incomplet ou incohérent.
	tomorrow := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	for name, mutate := range map[string]func(*GenerateRequest){
		"not certified":       func(r *GenerateRequest) { r.Certify = false },
		"no signer":           func(r *GenerateRequest) { r.SignerName = "  " },
		"no company":          func(r *GenerateRequest) { r.CompanyName = "" },
		"no city":             func(r *GenerateRequest) { r.City = "" },
		"bad date":            func(r *GenerateRequest) { r.AcquisitionDate = "01/03/2026" },
		"future":              func(r *GenerateRequest) { r.UsageStartDate = tomorrow },
		"usage before bought": func(r *GenerateRequest) { r.UsageStartDate = "2026-02-01" },
	} {
		req := valid
		mutate(&req)
		if _, err := svc.Generate(userCtx, req); !errors.Is(err, models.ErrAttestationInvalid) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}

	// Génération déjà en cours.
	release, _, _ := fiscal.TryLock(ctx, db, "attestation:"+merchantID)
	if _, err := svc.Generate(userCtx, valid); !errors.Is(err, models.ErrAttestationBusy) {
		t.Fatalf("busy: %v", err)
	}
	release()

	// Génération.
	started := time.Now()
	gen, err := svc.Generate(userCtx, valid)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	t.Logf("génération (contrôle d'intégrité, PDF, dépôt, base, journal) : %v", time.Since(started).Round(time.Millisecond))
	a := gen.Attestation
	stored := storage.files[gen.DownloadURL[len("https://signed.example/"):]]
	if a.Version != version.Version || a.MajorRoot != "2" || a.Obsolete || a.SignerName != "MARTIN Paul" ||
		fmt.Sprintf("%x", sha256.Sum256(stored)) != a.SHA256 || !bytes.HasPrefix(stored, []byte("%PDF-")) {
		t.Fatalf("attestation: %+v (stored %d bytes)", a, len(stored))
	}
	var content string
	if err := db.QueryRowContext(ctx, `SELECT content->>'licence' || '|' || (content->>'company_name') || '|' || (content->>'acquisition_date')
		FROM attestations WHERE id = $1`, a.ID).Scan(&content); err != nil || content != "WR-"+merchantID+"|Pizzeria ITest SARL|01/03/2026" {
		t.Fatalf("content = %q (%v)", content, err)
	}
	countAudit := func(action string) int {
		var n int
		_ = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_logs WHERE merchant_id = $1 AND action = $2 AND resource_type = 'attestations'
			AND resource_id = $3 AND hash IS NOT NULL`, merchantID, action, strconv.FormatInt(a.ID, 10)).Scan(&n)
		return n
	}
	if countAudit("ATTESTATION_GENERATED") != 1 {
		t.Fatal("missing ATTESTATION_GENERATED audit entry")
	}

	// Liste, lien, envoi.
	ov, err := svc.Overview(userCtx)
	if err != nil || !ov.Available || len(ov.Attestations) != 1 || ov.Attestations[0].ID != a.ID || ov.Prefill.Siret != siret {
		t.Fatalf("overview = (%+v, %v)", ov, err)
	}
	if link, err := svc.Link(userCtx, a.ID); err != nil || link.SHA256 != a.SHA256 || countAudit("ATTESTATION_DOWNLOAD") != 1 {
		t.Fatalf("Link = (%+v, %v)", link, err)
	}
	if err := svc.Email(userCtx, a.ID, "pas-une-adresse"); !errors.Is(err, models.ErrAttestationEmailInvalid) {
		t.Fatalf("bad email: %v", err)
	}
	if err := svc.Email(userCtx, a.ID, "comptable@example.fr"); err != nil || len(mails.sent) != 1 ||
		mails.sent[0].to != "comptable@example.fr" || mails.sent[0].template != "attestation_email.html" ||
		!bytes.Equal(mails.sent[0].attachment, stored) || countAudit("ATTESTATION_SENT") != 1 {
		t.Fatalf("Email: err = %v, sent = %d", err, len(mails.sent))
	}

	// Autre établissement : rien de visible.
	otherCtx := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: "x", MerchantID: "itest-att-other"})
	if _, err := svc.Link(otherCtx, a.ID); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("other merchant link: %v", err)
	}

	// Contrôle d'intégrité en erreur : un ticket v2 altéré bloque la
	// génération.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO receipts (receipt_id, merchant_id, order_id, receipt_number, total_ttc, total_ht, tax_details, items_snapshot, payments_snapshot, created_at, prev_hash, hash, signature, hash_version)
		VALUES ($1, $2, 1, 'F-2026-900001', 1000, 909, '{"lines":[{"rate":10,"ttc":1000,"ht":909,"tva":91}],"discount":0}', '[]', '[]', now() - interval '1 day', 'GENESIS_HASH', $3, 'bad', 2)`,
		"itest-att-receipt-"+merchantID, merchantID, fmt.Sprintf("%064d", 1)); err != nil {
		t.Fatalf("seed altered receipt: %v", err)
	}
	if _, err := svc.Generate(userCtx, valid); !errors.Is(err, models.ErrAttestationIntegrity) {
		t.Fatalf("integrity errors: %v", err)
	}
}
