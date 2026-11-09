package imaging

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

// FR-16.7: an attached image keeps its size and loses everything but its pixels: EXIF, GPS and
// any comment.
func TestReencodeStripsExifAndKeepsSize(t *testing.T) {
	in := readFixture(t, "exif.jpg")
	if !bytes.Contains(in, []byte(secret)) {
		t.Fatal("the fixture has lost its EXIF")
	}
	out, f, err := Codec{}.Reencode(bytes.NewReader(in), domain.FormatJPEG, 90)
	if err != nil || f != domain.FormatJPEG {
		t.Fatalf("Reencode = %s, %v", f, err)
	}
	for _, s := range []string{secret, "Exif\x00\x00", "\xFF\xE1", "GPS"} {
		if bytes.Contains(out, []byte(s)) {
			t.Errorf("output still holds %q", s)
		}
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(out))
	if err != nil || cfg.Width != 64 || cfg.Height != 48 {
		t.Errorf("size = %dx%d, %v", cfg.Width, cfg.Height, err)
	}
}

// A rotated photo is stored upright, at its own size turned.
func TestReencodeTurnsRotatedPhotosUpright(t *testing.T) {
	out, _, err := Codec{}.Reencode(bytes.NewReader(readFixture(t, "rotated.jpg")), domain.FormatJPEG, 90)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 64 {
		t.Fatalf("size = %v, want 32x64", b)
	}
	if r, _, bl, _ := img.At(16, 8).RGBA(); r < 0xc000 || bl > 0x4000 {
		t.Errorf("top is not red: r=%x b=%x", r, bl)
	}
}

// A PNG stays a PNG with its transparency and size; its text chunks are dropped.
func TestReencodePNG(t *testing.T) {
	in := withTextChunk(t, readFixture(t, "alpha.png"), secret)
	out, f, err := Codec{}.Reencode(bytes.NewReader(in), domain.FormatPNG, 90)
	if err != nil || f != domain.FormatPNG {
		t.Fatalf("Reencode = %s, %v", f, err)
	}
	if bytes.Contains(out, []byte(secret)) {
		t.Error("output still holds the text chunk")
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 30 || b.Dy() != 20 {
		t.Errorf("size = %v", b)
	}
	if _, _, _, a := img.At(2, 2).RGBA(); a != 0 {
		t.Errorf("transparent pixel has alpha %x", a)
	}
	if _, g, _, a := img.At(25, 10).RGBA(); a != 0xffff || g != 0xffff {
		t.Errorf("green pixel = g %x a %x", g, a)
	}
}

// An opaque WebP becomes a JPEG of its size (Go has no WebP encoder).
func TestReencodeWebP(t *testing.T) {
	out, f, err := Codec{}.Reencode(bytes.NewReader(readFixture(t, "still.webp")), domain.FormatWebP, 90)
	if err != nil || f != domain.FormatJPEG {
		t.Fatalf("Reencode = %s, %v", f, err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(out))
	if err != nil || cfg.Width != 48 || cfg.Height != 36 {
		t.Errorf("size = %dx%d, %v", cfg.Width, cfg.Height, err)
	}
}

func TestReencodeUnreadable(t *testing.T) {
	if _, _, err := (Codec{}).Reencode(bytes.NewReader(readFixture(t, "notimage.jpg")), domain.FormatJPEG, 90); err == nil {
		t.Error("a text file was encoded")
	}
}

func TestOpaque(t *testing.T) {
	solid := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			solid.SetNRGBA(x, y, color.NRGBA{R: 1, A: 255})
		}
	}
	if !opaque(solid) {
		t.Error("solid image is not opaque")
	}
	solid.SetNRGBA(1, 1, color.NRGBA{A: 10})
	if opaque(solid) {
		t.Error("image with a transparent pixel is opaque")
	}
}

// withTextChunk puts a tEXt chunk holding text after the PNG's IHDR, with its CRC.
func withTextChunk(t *testing.T, p []byte, text string) []byte {
	t.Helper()
	const ihdrEnd = 8 + 4 + 4 + 13 + 4
	data := append([]byte("Comment\x00"), text...)
	body := append([]byte("tEXt"), data...)
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(data))) //nolint:gosec // a few bytes
	chunk = append(chunk, body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(body))
	out := append([]byte{}, p[:ihdrEnd]...)
	out = append(out, chunk...)
	return append(out, p[ihdrEnd:]...)
}
