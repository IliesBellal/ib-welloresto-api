package deliveroo

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"sync"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Image de couverture du mealtime Deliveroo : JPEG ou PNG, au moins
// 1920x1080, en 16:9. On publie la bannière ScanNOrder recadrée au centre et
// mise à l'échelle 1920x1080 (convertie en JPEG : elle est souvent en WebP,
// refusé par Deliveroo). Sans bannière, un visuel WelloResto par défaut :
// logo centré sur fond blanc.
//
// Les images générées sont déposées sur le bucket R2 public, sous une clé
// dérivée de l'URL source : une nouvelle bannière (URL avec un autre ?v=)
// produit une nouvelle couverture, une bannière inchangée réutilise l'ancienne.

const (
	coverWidth   = 1920
	coverHeight  = 1080
	coverQuality = 88
	// defaultCoverVersion change dès que le visuel par défaut change.
	defaultCoverVersion = "v1"
	maxBannerBytes      = 15 << 20
)

//go:embed assets/wello-resto-logo.png
var welloRestoLogoPNG []byte

// ImageStore est le sous-ensemble du client R2 utilisé ici.
type ImageStore interface {
	UploadFile(ctx context.Context, key string, file io.Reader, contentType string) (string, error)
	GetFile(ctx context.Context, key string) ([]byte, error)
	PublicURL(key string) string
}

type coverCache struct {
	mu   sync.Mutex
	urls map[string]string // clé R2 -> URL publique déjà vérifiée
}

var covers = &coverCache{urls: map[string]string{}}

var bannerHTTPClient = &http.Client{Timeout: 20 * time.Second}

// coverImageURL renvoie l'URL publique de la couverture d'un établissement.
func coverImageURL(ctx context.Context, store ImageStore, merchantID, bannerURL string) (string, error) {
	if store == nil {
		return "", errors.New("deliveroo cover: stockage d'images indisponible")
	}
	if bannerURL == "" {
		return ensureCover(ctx, store, "wello_resto_images_storage/deliveroo/default-cover-"+defaultCoverVersion+".jpg", RenderDefaultCover)
	}
	sum := sha256.Sum256([]byte(bannerURL))
	key := fmt.Sprintf("wello_resto_images_storage/merchants/%s/deliveroo/cover-%s.jpg", merchantID, hex.EncodeToString(sum[:6]))
	return ensureCover(ctx, store, key, func() ([]byte, error) {
		src, err := downloadImage(ctx, bannerURL)
		if err != nil {
			return nil, err
		}
		return RenderCoverFromImage(src)
	})
}

// ensureCover publie l'image si elle n'existe pas encore sur R2.
func ensureCover(ctx context.Context, store ImageStore, key string, render func() ([]byte, error)) (string, error) {
	covers.mu.Lock()
	defer covers.mu.Unlock()

	if u, ok := covers.urls[key]; ok {
		return u, nil
	}
	if _, err := store.GetFile(ctx, key); err == nil {
		u := store.PublicURL(key)
		covers.urls[key] = u
		return u, nil
	}
	data, err := render()
	if err != nil {
		return "", err
	}
	u, err := store.UploadFile(ctx, key, bytes.NewReader(data), "image/jpeg")
	if err != nil {
		return "", fmt.Errorf("deliveroo cover: dépôt R2: %w", err)
	}
	covers.urls[key] = u
	return u, nil
}

func downloadImage(ctx context.Context, url string) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := bannerHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deliveroo cover: téléchargement de la bannière: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deliveroo cover: bannière inaccessible (HTTP %d)", resp.StatusCode)
	}
	img, _, err := image.Decode(io.LimitReader(resp.Body, maxBannerBytes))
	if err != nil {
		return nil, fmt.Errorf("deliveroo cover: bannière illisible: %w", err)
	}
	return img, nil
}

// RenderCoverFromImage recadre l'image au centre en 16:9 puis la met à
// l'échelle 1920x1080, en JPEG.
func RenderCoverFromImage(src image.Image) ([]byte, error) {
	crop := centerCrop16x9(src.Bounds())
	if crop.Empty() {
		return nil, errors.New("deliveroo cover: image vide")
	}
	dst := image.NewRGBA(image.Rect(0, 0, coverWidth, coverHeight))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, draw.Over, nil)
	return encodeJPEG(dst)
}

// RenderDefaultCover : logo WelloResto centré sur fond blanc, 1920x1080.
func RenderDefaultCover() ([]byte, error) {
	logo, _, err := image.Decode(bytes.NewReader(welloRestoLogoPNG))
	if err != nil {
		return nil, fmt.Errorf("deliveroo cover: logo embarqué illisible: %w", err)
	}
	dst := image.NewRGBA(image.Rect(0, 0, coverWidth, coverHeight))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	lb := logo.Bounds()
	w := coverWidth * 55 / 100 // le logo occupe 55 % de la largeur
	h := w * lb.Dy() / lb.Dx()
	x := (coverWidth - w) / 2
	y := (coverHeight - h) / 2
	draw.CatmullRom.Scale(dst, image.Rect(x, y, x+w, y+h), logo, lb, draw.Over, nil)
	return encodeJPEG(dst)
}

// centerCrop16x9 : plus grand rectangle 16:9 centré dans b.
func centerCrop16x9(b image.Rectangle) image.Rectangle {
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return image.Rectangle{}
	}
	if w*9 > h*16 { // trop large : on rogne les côtés
		cw := h * 16 / 9
		x0 := b.Min.X + (w-cw)/2
		return image.Rect(x0, b.Min.Y, x0+cw, b.Max.Y)
	}
	ch := w * 9 / 16 // trop haute : on rogne le haut et le bas
	y0 := b.Min.Y + (h-ch)/2
	return image.Rect(b.Min.X, y0, b.Max.X, y0+ch)
}

func encodeJPEG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: coverQuality}); err != nil {
		return nil, fmt.Errorf("deliveroo cover: encodage JPEG: %w", err)
	}
	return buf.Bytes(), nil
}
