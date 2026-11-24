// Package imageopt allège une image avant son envoi dans R2 : redimensionnement
// (plus grand côté borné), orientation EXIF appliquée, réencodage JPEG pour les
// images opaques et PNG pour celles qui ont de la transparence (canettes ou
// bouteilles détourées). Voir docs/banque-images.md.
//
// Tout est en Go pur (bibliothèque standard + golang.org/x/image) : aucun
// service externe ni binaire système. Le résultat n'est retenu que s'il est
// plus léger que l'original ; en cas de doute (image illisible, trop grande
// pour être décodée sans risque), l'original est renvoyé tel quel : alléger
// une image ne doit jamais bloquer son import.
package imageopt

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // décodage WebP (pas d'encodeur en Go pur)
)

const (
	// DefaultMaxSide : plus grand côté conservé. Suffit à un plein écran de
	// borne (1080×1920) et à une bannière desktop ; les miniatures sont
	// produites ensuite par Cloudflare Image Transformations.
	DefaultMaxSide = 2000
	// DefaultJPEGQuality : qualité JPEG visuellement sans perte pour une
	// photo culinaire, environ 3 à 5 fois plus légère qu'un export à 100.
	DefaultJPEGQuality = 82
	// maxPixels : au-delà (≈ 60 Mpx), on ne décode pas, pour borner la
	// mémoire d'un appel (≈ 240 Mo en RGBA).
	maxPixels = 60_000_000
)

// Options règle l'optimisation. Les valeurs nulles prennent les défauts.
type Options struct {
	MaxSide     int
	JPEGQuality int
}

// Result décrit l'image à envoyer.
type Result struct {
	Data        []byte
	ContentType string
	Width       int
	Height      int
	// Optimized est vrai quand Data est la version réencodée (sinon
	// l'original).
	Optimized bool
}

// Optimize renvoie la version la plus légère de data entre l'original et sa
// version réencodée. contentType est celui de l'original (image/jpeg,
// image/png ou image/webp, déjà validé par l'appelant).
func Optimize(data []byte, contentType string, opt Options) Result {
	if opt.MaxSide <= 0 {
		opt.MaxSide = DefaultMaxSide
	}
	if opt.JPEGQuality <= 0 || opt.JPEGQuality > 100 {
		opt.JPEGQuality = DefaultJPEGQuality
	}
	original := Result{Data: data, ContentType: contentType}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return original
	}
	original.Width, original.Height = cfg.Width, cfg.Height
	if cfg.Width*cfg.Height > maxPixels {
		return original
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return original
	}

	// Réduire d'abord (le plus grand côté borné ne dépend pas de l'orientation),
	// puis redresser : la rotation pixel par pixel porte sur moins de pixels.
	img = fit(img, opt.MaxSide)
	if contentType == "image/jpeg" {
		img = applyOrientation(img, jpegOrientation(data))
	}

	var buf bytes.Buffer
	outType := "image/jpeg"
	if isOpaque(img) {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: opt.JPEGQuality})
	} else {
		outType = "image/png"
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img)
	}
	if err != nil {
		return original
	}

	// Un réencodage qui n'allège pas est écarté. L'original garde alors son
	// EXIF, que les navigateurs appliquent eux-mêmes.
	if buf.Len() >= len(data) {
		return original
	}
	b := img.Bounds()
	return Result{Data: buf.Bytes(), ContentType: outType, Width: b.Dx(), Height: b.Dy(), Optimized: true}
}

// fit réduit img pour que son plus grand côté ne dépasse pas maxSide.
func fit(img image.Image, maxSide int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxSide && h <= maxSide {
		return img
	}
	nw, nh := maxSide, maxSide
	if w >= h {
		nh = max(1, h*maxSide/w)
	} else {
		nw = max(1, w*maxSide/h)
	}
	dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

// isOpaque : vrai si aucun pixel n'est transparent. Les types standard
// exposent Opaque() ; à défaut, on considère l'image transparente pour ne
// jamais perdre de transparence.
func isOpaque(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	return false
}
