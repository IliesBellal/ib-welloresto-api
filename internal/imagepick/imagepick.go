// Package imagepick place l'image d'un produit, d'une option, d'une catégorie,
// d'une bannière… quelle que soit sa provenance (docs/banque-images.md) :
//
//   - fichier envoyé depuis l'appareil (multipart) : allégé par imageopt puis
//     envoyé sous la clé R2 de la cible ;
//   - image de la banque (JSON {"image_url"}) : l'URL est reprise telle quelle,
//     le fichier est partagé entre établissements et jamais supprimé ;
//   - image déjà utilisée par l'établissement (« Mes images », même JSON) :
//     le fichier est copié sous la clé de la cible. Partager l'objet ne
//     tiendrait pas : remplacer l'image de l'un effacerait celle de l'autre.
//
// Les endpoints gardent leur logique propre (droits, ancienne image,
// persistance) ; seul le placement du fichier passe par ici.
package imagepick

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"welloresto-api/internal/infrastructure/imageopt"
	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/models"
)

const (
	// maxUploadBytes : plafond de lecture en mémoire d'un fichier envoyé. Les
	// endpoints ont leurs propres limites, en général plus basses.
	maxUploadBytes = 25 << 20
	maxJSONBytes   = 4 << 10
)

// Error : refus explicite, renvoyé au client avec son code.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code }

var (
	ErrInvalidURL = &Error{Status: http.StatusBadRequest, Code: "invalid_image_url",
		Message: "Image URL must point to the image bank or to one of your images"}
	ErrBankImageNotFound = &Error{Status: http.StatusNotFound, Code: "image_not_found",
		Message: "This image is no longer available in the image bank"}
	ErrInvalidBody = &Error{Status: http.StatusBadRequest, Code: "invalid_body"}
	ErrInvalidType = &Error{Status: http.StatusBadRequest, Code: "invalid_image_type",
		Message: "Only JPEG, PNG, and WebP images are allowed"}
	ErrTooLarge = &Error{Status: http.StatusBadRequest, Code: "file_too_large_or_invalid"}
)

// BankChecker : vérifie qu'une clé est une image active de la banque.
type BankChecker interface {
	IsBankImage(ctx context.Context, key string) (bool, error)
}

// Storage : opérations R2 utilisées (r2.Client).
type Storage interface {
	UploadFile(ctx context.Context, key string, file io.Reader, contentType string) (string, error)
	CopyFile(ctx context.Context, srcKey, dstKey string) (string, error)
	GetKeyFromURL(rawURL string) string
	PublicURL(key string) string
}

// Picker place les images. Un Picker nil (stockage non configuré) refuse
// tout placement.
type Picker struct {
	store Storage
	bank  BankChecker
	opt   imageopt.Options
	now   func() time.Time
}

// New renvoie nil si store est nil.
func New(store *r2.Client, bank BankChecker) *Picker {
	if store == nil {
		return nil
	}
	return newPicker(store, bank)
}

func newPicker(store Storage, bank BankChecker) *Picker {
	return &Picker{store: store, bank: bank, now: time.Now}
}

// Placed : fichier désormais référencé par la cible.
type Placed struct {
	// URL à enregistrer. Versionnée (?v=…) pour un fichier envoyé ou copié
	// (la clé de la cible est souvent fixe) ; canonique pour la banque.
	URL string
	// Key : clé R2 du fichier référencé. Sert à décider si l'ancienne image
	// peut être supprimée (oldKey != Key).
	Key string
	// Shared : image de la banque.
	Shared      bool
	ContentType string
}

// KeyFunc donne la clé R2 de la cible pour une extension (".jpg", ".png"…).
type KeyFunc func(ext string) string

// Request : paramètres d'un placement depuis une requête HTTP.
type Request struct {
	// Field : champ multipart du fichier ("photo" ou "file").
	Field string
	// MultipartMemory : mémoire de ParseMultipartForm (comportement
	// historique de chaque endpoint).
	MultipartMemory int64
	// OwnPrefixes : dossiers R2 dont un fichier peut être copié (« Mes
	// images ») — en général r2.MerchantKeyPrefix(merchantID).
	OwnPrefixes []string
	KeyFor      KeyFunc
}

// WantsURL : la requête choisit une image par URL (corps JSON) plutôt que
// d'envoyer un fichier.
func WantsURL(r *http.Request) bool {
	return strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json")
}

// DecodeURL lit {"image_url": "…"}.
func DecodeURL(w http.ResponseWriter, r *http.Request) (string, error) {
	var body struct {
		ImageURL string `json:"image_url"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBytes)).Decode(&body); err != nil {
		return "", ErrInvalidBody
	}
	u := strings.TrimSpace(body.ImageURL)
	if u == "" {
		return "", ErrInvalidURL
	}
	return u, nil
}

// Place lit la requête (JSON {"image_url"} ou multipart) et place l'image.
func (p *Picker) Place(w http.ResponseWriter, r *http.Request, req Request) (*Placed, error) {
	if p == nil {
		return nil, errors.New("image storage not configured")
	}
	if WantsURL(r) {
		rawURL, err := DecodeURL(w, r)
		if err != nil {
			return nil, err
		}
		return p.FromURL(r.Context(), rawURL, req.OwnPrefixes, req.KeyFor)
	}

	if err := r.ParseMultipartForm(req.MultipartMemory); err != nil {
		return nil, ErrTooLarge
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	file, header, err := r.FormFile(req.Field)
	if err != nil {
		return nil, &Error{Status: http.StatusBadRequest, Code: "missing_" + req.Field + "_field"}
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = r2.GetContentTypeFromExtension(header.Filename)
	}
	return p.Upload(r.Context(), file, contentType, req.KeyFor)
}

// Upload allège puis envoie un fichier sous la clé de la cible.
func (p *Picker) Upload(ctx context.Context, file io.Reader, contentType string, keyFor KeyFunc) (*Placed, error) {
	if p == nil {
		return nil, errors.New("image storage not configured")
	}
	if !r2.ValidateImageType(contentType) {
		return nil, ErrInvalidType
	}
	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		return nil, ErrTooLarge
	}
	if len(data) > maxUploadBytes {
		return nil, ErrTooLarge
	}
	res := imageopt.Optimize(data, contentType, p.opt)
	key := keyFor(r2.GetExtensionFromContentType(res.ContentType))
	publicURL, err := p.store.UploadFile(ctx, key, bytes.NewReader(res.Data), res.ContentType)
	if err != nil {
		return nil, fmt.Errorf("upload image: %w", err)
	}
	return &Placed{URL: p.versioned(publicURL), Key: key, ContentType: res.ContentType}, nil
}

// FromURL place une image choisie par son URL : banque (reprise telle
// quelle) ou fichier d'un des dossiers ownPrefixes (copié sous la clé de la
// cible).
func (p *Picker) FromURL(ctx context.Context, rawURL string, ownPrefixes []string, keyFor KeyFunc) (*Placed, error) {
	if p == nil {
		return nil, errors.New("image storage not configured")
	}
	key := p.store.GetKeyFromURL(rawURL)
	if key == "" || !r2.IsImageKey(key) {
		return nil, ErrInvalidURL
	}
	contentType := r2.GetContentTypeFromExtension(key)

	if r2.IsImageBankKey(key) {
		if p.bank == nil {
			return nil, ErrBankImageNotFound
		}
		ok, err := p.bank.IsBankImage(ctx, key)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrBankImageNotFound
		}
		return &Placed{URL: p.store.PublicURL(key), Key: key, Shared: true, ContentType: contentType}, nil
	}

	if !ownedBy(key, ownPrefixes) {
		return nil, ErrInvalidURL
	}
	ext := strings.ToLower(path.Ext(key))
	if ext == ".jpeg" {
		ext = ".jpg"
	}
	dst := keyFor(ext)
	if dst == key {
		// L'image choisie est déjà celle de la cible : rien à copier.
		return &Placed{URL: p.versioned(p.store.PublicURL(key)), Key: key, ContentType: contentType}, nil
	}
	publicURL, err := p.store.CopyFile(ctx, key, dst)
	if err != nil {
		return nil, fmt.Errorf("copy image: %w", err)
	}
	return &Placed{URL: p.versioned(publicURL), Key: dst, ContentType: contentType}, nil
}

// ownedBy : key est dans un des dossiers autorisés, hors traçabilité HACCP
// (photos d'étiquettes, jamais proposées comme visuels).
func ownedBy(key string, prefixes []string) bool {
	if strings.Contains(key, "/haccp/") {
		return false
	}
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

func (p *Picker) versioned(publicURL string) string {
	return publicURL + "?v=" + strconv.FormatInt(p.now().UnixMilli(), 10)
}

// Send répond au client après un échec de Place / Upload / FromURL.
func Send(w http.ResponseWriter, module, fnName string, err error) {
	var e *Error
	if errors.As(err, &e) {
		body := map[string]string{"error": e.Code}
		if e.Message != "" {
			body["message"] = e.Message
		}
		models.SendJSON(w, e.Status, module, fnName, body)
		return
	}
	models.SendErrorJSON(w, module, fnName, fmt.Errorf("failed to upload image"))
}
