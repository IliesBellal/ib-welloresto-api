package imageopt

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand"
	"testing"
)

func noisy(w, h int, alpha bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(1))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			a := uint8(255)
			if alpha && x < w/4 {
				a = 0
			}
			img.SetNRGBA(x, y, color.NRGBA{uint8(x/8 + rng.Intn(24)), uint8(y/8 + rng.Intn(24)), uint8(rng.Intn(24)), a})
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image, q int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestOptimize_OpaquePNGBecomesSmallerJPEGAndIsResized(t *testing.T) {
	data := encodePNG(t, noisy(3000, 1500, false))
	res := Optimize(data, "image/png", Options{MaxSide: 1000})
	if !res.Optimized || res.ContentType != "image/jpeg" {
		t.Fatalf("attendu JPEG optimisé, obtenu %+v", res.ContentType)
	}
	if res.Width != 1000 || res.Height != 500 {
		t.Fatalf("dimensions %dx%d, attendu 1000x500", res.Width, res.Height)
	}
	if len(res.Data) >= len(data) {
		t.Fatalf("pas plus léger : %d >= %d", len(res.Data), len(data))
	}
}

func TestOptimize_TransparentStaysPNG(t *testing.T) {
	data := encodePNG(t, noisy(2400, 2400, true))
	res := Optimize(data, "image/png", Options{MaxSide: 800})
	if !res.Optimized || res.ContentType != "image/png" {
		t.Fatalf("attendu PNG optimisé, obtenu optimized=%v type=%s", res.Optimized, res.ContentType)
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Fatalf("transparence perdue (alpha=%d)", a)
	}
}

func TestOptimize_KeepsOriginalWhenNotLighter(t *testing.T) {
	data := encodeJPEG(t, noisy(200, 200, false), 40)
	res := Optimize(data, "image/jpeg", Options{})
	if res.Optimized || !bytes.Equal(res.Data, data) || res.ContentType != "image/jpeg" {
		t.Fatalf("l'original aurait dû être conservé")
	}
	if res.Width != 200 || res.Height != 200 {
		t.Fatalf("dimensions de l'original non renseignées : %dx%d", res.Width, res.Height)
	}
}

func TestOptimize_InvalidDataReturnsOriginal(t *testing.T) {
	data := []byte("pas une image")
	res := Optimize(data, "image/png", Options{})
	if res.Optimized || !bytes.Equal(res.Data, data) {
		t.Fatal("une image illisible doit être renvoyée telle quelle")
	}
}

// withOrientation insère un segment APP1 EXIF (orientation o) après le SOI.
func withOrientation(jpg []byte, o uint16) []byte {
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1}
	entry := make([]byte, 12)
	binary.BigEndian.PutUint16(entry[0:], 0x0112)
	binary.BigEndian.PutUint16(entry[2:], 3) // SHORT
	binary.BigEndian.PutUint32(entry[4:], 1)
	binary.BigEndian.PutUint16(entry[8:], o)
	tiff = append(tiff, entry...)
	tiff = append(tiff, 0, 0, 0, 0)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	seg = append(seg, payload...)
	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

func TestJPEGOrientation(t *testing.T) {
	jpg := encodeJPEG(t, noisy(40, 20, false), 90)
	if got := jpegOrientation(jpg); got != 1 {
		t.Fatalf("sans EXIF : %d, attendu 1", got)
	}
	if got := jpegOrientation(withOrientation(jpg, 6)); got != 6 {
		t.Fatalf("EXIF 6 : %d", got)
	}
}

func TestOptimize_AppliesOrientationWhenResizing(t *testing.T) {
	jpg := withOrientation(encodeJPEG(t, noisy(3000, 1500, false), 95), 6)
	res := Optimize(jpg, "image/jpeg", Options{MaxSide: 1000})
	if !res.Optimized {
		t.Fatal("attendu une version optimisée")
	}
	if res.Width != 500 || res.Height != 1000 {
		t.Fatalf("orientation non appliquée : %dx%d, attendu 500x1000", res.Width, res.Height)
	}
}
