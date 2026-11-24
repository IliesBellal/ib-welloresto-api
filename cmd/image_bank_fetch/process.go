package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"unicode"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"golang.org/x/text/unicode/norm"
)

// Cadrage des images téléchargées, avant l'import (qui les allège ensuite) :
//   - square, photo : recadrage carré centré ;
//   - square, produit (Open Food Facts) : produit centré sur fond blanc
//     carré, avec une marge — un recadrage couperait la canette ;
//   - banner : recadrage centré au plus haut en 16:9, au plus large en 3:1.
const (
	maxSide        = 2000
	packshotMargin = 0.06
	minSourceSide  = 500
	// minPackshotSide : plus grand côté minimal d'une photo de produit.
	minPackshotSide = 600
)

var errTooSmall = errors.New("image trop petite")

// frame décode data et renvoie l'image cadrée encodée (PNG si transparence,
// sinon JPEG qualité 92 : l'import fera la compression finale), avec son
// extension.
func frame(data []byte, format string, packshot bool) ([]byte, string, int, int, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", 0, 0, err
	}
	b := img.Bounds()
	// Un produit (canette, bouteille) est haut et étroit : on juge son plus
	// grand côté ; une photo est recadrée, son plus petit côté compte.
	if packshot {
		if max(b.Dx(), b.Dy()) < minPackshotSide {
			return nil, "", 0, 0, errTooSmall
		}
	} else if min(b.Dx(), b.Dy()) < minSourceSide {
		return nil, "", 0, 0, errTooSmall
	}
	switch {
	case format == "banner":
		img = cropRatio(img, 16.0/9.0, 3.0)
	case packshot:
		img = padSquare(img)
	default:
		img = cropRatio(img, 1, 1)
	}
	img = fitMax(img, maxSide)

	var buf bytes.Buffer
	ext := ".jpg"
	if opaque(img) {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 92})
	} else {
		ext = ".png"
		err = png.Encode(&buf, img)
	}
	if err != nil {
		return nil, "", 0, 0, err
	}
	nb := img.Bounds()
	return buf.Bytes(), ext, nb.Dx(), nb.Dy(), nil
}

// cropRatio recadre au centre pour que largeur/hauteur soit entre minR et maxR.
func cropRatio(img image.Image, minR, maxR float64) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	r := float64(w) / float64(h)
	var rect image.Rectangle
	switch {
	case r < minR: // trop haute : on coupe en haut et en bas
		nh := int(float64(w) / minR)
		y := b.Min.Y + (h-nh)/2
		rect = image.Rect(b.Min.X, y, b.Max.X, y+nh)
	case r > maxR: // trop large : on coupe à gauche et à droite
		nw := int(float64(h) * maxR)
		x := b.Min.X + (w-nw)/2
		rect = image.Rect(x, b.Min.Y, x+nw, b.Max.Y)
	default:
		return img
	}
	dst := image.NewNRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(dst, dst.Bounds(), img, rect.Min, draw.Src)
	return dst
}

// padSquare centre l'image sur un carré blanc, avec une marge.
func padSquare(img image.Image) image.Image {
	b := img.Bounds()
	side := int(float64(max(b.Dx(), b.Dy())) * (1 + 2*packshotMargin))
	dst := image.NewNRGBA(image.Rect(0, 0, side, side))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	off := image.Pt((side-b.Dx())/2, (side-b.Dy())/2)
	draw.Draw(dst, image.Rectangle{Min: off, Max: off.Add(b.Size())}, img, b.Min, draw.Over)
	return dst
}

func fitMax(img image.Image, maxSide int) image.Image {
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

func opaque(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	return false
}

// fold : minuscules sans accents.
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

// backgroundScore : part (0 à 1) des pixels du pourtour (bande de 4 %) qui
// sont blancs, gris très clair ou transparents. Une photo de produit
// détourée ou prise sur fond neutre approche 1 ; une photo « sur le vif »
// (main, table, rue) reste basse.
func backgroundScore(img image.Image) float64 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	band := max(1, min(w, h)*4/100)
	step := max(1, min(w, h)/200)
	light, total := 0, 0
	check := func(x, y int) {
		r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
		total++
		if a < 0x4000 {
			light++
			return
		}
		r8, g8, b8 := r>>8, g>>8, bl>>8
		lo, hi := min(r8, g8, b8), max(r8, g8, b8)
		if lo >= 215 && hi-lo <= 25 {
			light++
		}
	}
	for y := 0; y < h; y += step {
		for x := 0; x < w; x += step {
			if x < band || x >= w-band || y < band || y >= h-band {
				check(x, y)
			}
		}
	}
	if total == 0 {
		return 0
	}
	return float64(light) / float64(total)
}
