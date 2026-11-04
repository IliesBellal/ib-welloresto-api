package deliveroo_menu

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"welloresto-api/internal/modules/deliveroo"
)

type fakeCompleter struct {
	got   []deliveroo.MenuUploadResult
	calls int
}

func (f *fakeCompleter) CompleteMenuUpload(_ context.Context, res deliveroo.MenuUploadResult) error {
	f.calls++
	f.got = append(f.got, res)
	return nil
}

// Exemple de la doc Deliveroo (reference/menu-events-webhook).
const docPayload = `{
  "event": "menu.upload_result",
  "body": {
    "menu_upload_result": {
      "http_status": 200,
      "brand_id": "brand-external-id",
      "menu_id": "main",
      "site_ids": ["site_location_id_1", "site_location_id_2"],
      "errors": {
        "processing": "",
        "images": [{"url": "https://x/img.jpg", "message": "cannot decode image"}],
        "barcodes": [{"barcode": "1234567", "message": "barcode length is invalid"}]
      }
    }
  }
}`

func TestHandleMenuWebhook_ParsesDocumentedPayload(t *testing.T) {
	fake := &fakeCompleter{}
	h := NewMenuWebhookHandler(&MenuWebhookService{publisher: fake}, "")

	rec := httptest.NewRecorder()
	h.HandleMenuWebhook(rec, httptest.NewRequest(http.MethodPost, "/webhooks/deliveroo/menu", strings.NewReader(docPayload)))

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if fake.calls != 1 {
		t.Fatalf("CompleteMenuUpload appelé %d fois, want 1", fake.calls)
	}
	got := fake.got[0]
	if got.HTTPStatus != 200 || got.MenuID != "main" || got.BrandID != "brand-external-id" || len(got.SiteIDs) != 2 {
		t.Errorf("résultat = %+v", got)
	}
	if got.Errors.Empty() || len(got.Errors.Images) != 1 || len(got.Errors.Barcodes) != 1 {
		t.Errorf("erreurs mal lues : %+v", got.Errors)
	}
}

func TestHandleMenuWebhook_IgnoresUnknownEvent(t *testing.T) {
	fake := &fakeCompleter{}
	h := NewMenuWebhookHandler(&MenuWebhookService{publisher: fake}, "")

	rec := httptest.NewRecorder()
	h.HandleMenuWebhook(rec, httptest.NewRequest(http.MethodPost, "/webhooks/deliveroo/menu", strings.NewReader(`{"event":"autre"}`)))

	if rec.Code != http.StatusOK || fake.calls != 0 {
		t.Errorf("code %d, appels %d : un événement inconnu est acquitté sans traitement", rec.Code, fake.calls)
	}
}
