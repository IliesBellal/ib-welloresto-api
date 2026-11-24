package imagepick

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/infrastructure/r2"
)

const base = "https://storage.welloresto.fr/"

type fakeStore struct {
	uploads map[string]string // key → content type
	copies  [][2]string
}

func newFakeStore() *fakeStore { return &fakeStore{uploads: map[string]string{}} }

func (f *fakeStore) UploadFile(_ context.Context, key string, file io.Reader, ct string) (string, error) {
	_, _ = io.ReadAll(file)
	f.uploads[key] = ct
	return base + key, nil
}
func (f *fakeStore) CopyFile(_ context.Context, src, dst string) (string, error) {
	f.copies = append(f.copies, [2]string{src, dst})
	return base + dst, nil
}
func (f *fakeStore) GetKeyFromURL(u string) string {
	if i := strings.Index(u, "?"); i >= 0 {
		u = u[:i]
	}
	if strings.HasPrefix(u, base) {
		return strings.TrimPrefix(u, base)
	}
	return ""
}
func (f *fakeStore) PublicURL(key string) string { return base + key }

type fakeBank map[string]bool

func (b fakeBank) IsBankImage(_ context.Context, key string) (bool, error) { return b[key], nil }

func picker(store *fakeStore, bank fakeBank) *Picker {
	p := newPicker(store, bank)
	p.now = func() time.Time { return time.UnixMilli(42) }
	return p
}

func productKey(ext string) string { return r2.GenerateProductKey("7", "99", ext) }

var own = []string{r2.MerchantKeyPrefix("7")}

func TestFromURL_BankImageKeptAsIs(t *testing.T) {
	key := r2.ImageBankPrefix + "pizzas/margherita.jpg"
	store := newFakeStore()
	placed, err := picker(store, fakeBank{key: true}).FromURL(context.Background(), base+key, own, productKey)
	if err != nil {
		t.Fatal(err)
	}
	if !placed.Shared || placed.URL != base+key || placed.Key != key {
		t.Fatalf("placement inattendu : %+v", placed)
	}
	if len(store.copies) != 0 || len(store.uploads) != 0 {
		t.Fatal("une image de la banque ne doit être ni copiée ni envoyée")
	}
}

func TestFromURL_DisabledBankImageRefused(t *testing.T) {
	key := r2.ImageBankPrefix + "pizzas/old.jpg"
	_, err := picker(newFakeStore(), fakeBank{}).FromURL(context.Background(), base+key, own, productKey)
	if !errors.Is(err, ErrBankImageNotFound) {
		t.Fatalf("attendu ErrBankImageNotFound, obtenu %v", err)
	}
}

func TestFromURL_OwnImageIsCopied(t *testing.T) {
	src := r2.GenerateProductKey("7", "12", ".png")
	store := newFakeStore()
	placed, err := picker(store, nil).FromURL(context.Background(), base+src+"?v=1", own, productKey)
	if err != nil {
		t.Fatal(err)
	}
	want := productKey(".png")
	if len(store.copies) != 1 || store.copies[0] != [2]string{src, want} {
		t.Fatalf("copie inattendue : %v", store.copies)
	}
	if placed.Shared || placed.Key != want || placed.URL != base+want+"?v=42" {
		t.Fatalf("placement inattendu : %+v", placed)
	}
}

func TestFromURL_SameKeyNotCopied(t *testing.T) {
	src := productKey(".jpg")
	store := newFakeStore()
	placed, err := picker(store, nil).FromURL(context.Background(), base+src, own, productKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.copies) != 0 || placed.Key != src {
		t.Fatalf("aucune copie attendue : %v %+v", store.copies, placed)
	}
}

func TestFromURL_Refusals(t *testing.T) {
	cases := map[string]string{
		"autre établissement": base + r2.GenerateProductKey("8", "1", ".jpg"),
		"url externe":         "https://exemple.fr/photo.jpg",
		"pas une image":       base + "wello_resto_images_storage/merchants/7/reports/x.csv",
		"photo haccp":         base + r2.GenerateHACCPTraceabilityKey("7", "r1", 0, ".jpg"),
	}
	for name, u := range cases {
		_, err := picker(newFakeStore(), nil).FromURL(context.Background(), u, own, productKey)
		if !errors.Is(err, ErrInvalidURL) {
			t.Errorf("%s : attendu ErrInvalidURL, obtenu %v", name, err)
		}
	}
}

func pngBytes(t *testing.T) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(0, 0, color.NRGBA{0, 0, 0, 0})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPlace_MultipartUpload(t *testing.T) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="photo"; filename="a.png"`}
	h["Content-Type"] = []string{"image/png"}
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(pngBytes(t))
	_ = mw.Close()

	r := httptest.NewRequest(http.MethodPut, "/x", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	store := newFakeStore()
	placed, err := picker(store, nil).Place(httptest.NewRecorder(), r, Request{Field: "photo", MultipartMemory: 1 << 20, OwnPrefixes: own, KeyFor: productKey})
	if err != nil {
		t.Fatal(err)
	}
	if placed.Key != productKey(".png") || store.uploads[placed.Key] != "image/png" {
		t.Fatalf("upload inattendu : %+v %v", placed, store.uploads)
	}
}

func TestPlace_JSONBody(t *testing.T) {
	key := r2.ImageBankPrefix + "burgers/cheese.jpg"
	r := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{"image_url":"`+base+key+`"}`))
	r.Header.Set("Content-Type", "application/json")
	placed, err := picker(newFakeStore(), fakeBank{key: true}).Place(httptest.NewRecorder(), r, Request{Field: "photo", OwnPrefixes: own, KeyFor: productKey})
	if err != nil || !placed.Shared {
		t.Fatalf("attendu image de banque, obtenu %+v %v", placed, err)
	}
}

func TestPlace_MissingField(t *testing.T) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("other", "x")
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPut, "/x", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	_, err := picker(newFakeStore(), nil).Place(httptest.NewRecorder(), r, Request{Field: "file", MultipartMemory: 1 << 20, KeyFor: productKey})
	var e *Error
	if !errors.As(err, &e) || e.Code != "missing_file_field" {
		t.Fatalf("attendu missing_file_field, obtenu %v", err)
	}
}
