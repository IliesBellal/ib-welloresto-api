package imagebank

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"welloresto-api/internal/infrastructure/r2"
)

// CacheKey : liste complète des images actives, en JSON. Le script d'import la
// supprime après écriture ; à défaut, elle expire après cacheTTL.
const (
	CacheKey = "image_bank:v1:all"
	cacheTTL = 10 * time.Minute
)

// Cache : sous-ensemble du client Redis utilisé (nil accepté : sans cache).
type Cache interface {
	Get(ctx context.Context, key string) (string, bool)
	Set(ctx context.Context, key string, value string, ttl time.Duration) bool
}

// KeyResolver : extraction de la clé R2 d'une URL publique (r2.Client).
type KeyResolver interface {
	GetKeyFromURL(rawURL string) string
}

type store interface {
	ListEnabled(ctx context.Context) ([]Image, error)
	IsEnabledKey(ctx context.Context, key string) (bool, error)
	ListMerchantImageURLs(ctx context.Context, merchantID string) ([]MyImage, error)
}

type Service struct {
	repo  store
	cache Cache
	keys  KeyResolver
}

func NewService(repo *Repository, cache Cache, keys KeyResolver) *Service {
	return &Service{repo: repo, cache: cache, keys: keys}
}

func (s *Service) all(ctx context.Context) ([]Image, error) {
	if s.cache != nil {
		if raw, ok := s.cache.Get(ctx, CacheKey); ok {
			var images []Image
			if json.Unmarshal([]byte(raw), &images) == nil {
				return images, nil
			}
		}
	}
	images, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	if s.cache != nil {
		if raw, err := json.Marshal(images); err == nil {
			s.cache.Set(ctx, CacheKey, string(raw), cacheTTL)
		}
	}
	return images, nil
}

// List renvoie les images actives correspondant au filtre. La recherche
// ignore la casse et les accents, et exige chaque mot dans le nom, les tags
// ou le libellé de la catégorie.
func (s *Service) List(ctx context.Context, f ListFilter) (*ListResponse, error) {
	images, err := s.all(ctx)
	if err != nil {
		return nil, err
	}
	words := strings.Fields(fold(f.Query))
	out := []Image{}
	for _, img := range images {
		if f.Format != "" && img.Format != f.Format {
			continue
		}
		if f.Category != "" && img.Category != f.Category {
			continue
		}
		if len(words) > 0 && !matches(img, words) {
			continue
		}
		out = append(out, img)
	}
	return &ListResponse{Images: out, Total: len(out)}, nil
}

// Categories renvoie la liste fermée des catégories avec leur nombre
// d'images actives ; les catégories vides sont incluses (count = 0).
func (s *Service) Categories(ctx context.Context) ([]CategoryCount, error) {
	images, err := s.all(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[string]*CategoryCount{}
	out := make([]CategoryCount, len(Categories))
	for i, c := range Categories {
		out[i] = CategoryCount{Category: c, Formats: map[string]int{FormatSquare: 0, FormatBanner: 0}}
		counts[c.Code] = &out[i]
	}
	for _, img := range images {
		if c, ok := counts[img.Category]; ok {
			c.Count++
			c.Formats[img.Format]++
		}
	}
	return out, nil
}

// IsBankImage indique si key est le fichier d'une image active de la banque.
func (s *Service) IsBankImage(ctx context.Context, key string) (bool, error) {
	if !r2.IsImageBankKey(key) {
		return false, nil
	}
	return s.repo.IsEnabledKey(ctx, key)
}

// Mine renvoie les images de l'établissement réutilisables (« Mes images ») :
// fichiers de son dossier R2, au format image, dédoublonnés. Les images de la
// banque et les URLs externes en sont exclues : la banque a son propre onglet,
// et une URL externe ne peut pas être copiée.
func (s *Service) Mine(ctx context.Context, merchantID string) (*MyImagesResponse, error) {
	if s.keys == nil { // stockage non configuré : rien de copiable
		return &MyImagesResponse{Images: []MyImage{}}, nil
	}
	rows, err := s.repo.ListMerchantImageURLs(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	prefix := r2.MerchantKeyPrefix(merchantID)
	seen := map[string]bool{}
	out := []MyImage{}
	for _, row := range rows {
		key := s.keys.GetKeyFromURL(row.ImageURL)
		if key == "" || !strings.HasPrefix(key, prefix) || !r2.IsImageKey(key) || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, row)
	}
	return &MyImagesResponse{Images: out}, nil
}

func matches(img Image, words []string) bool {
	hay := fold(img.Name + " " + strings.Join(img.Tags, " ") + " " + categoryLabel(img.Category) + " " + img.Category)
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

func categoryLabel(code string) string {
	for _, c := range Categories {
		if c.Code == code {
			return c.Label
		}
	}
	return ""
}

// fold : minuscules sans accents (« Crème brûlée » → « creme brulee »).
func fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
