package imaging

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

func TestSizeReadsTheHeader(t *testing.T) {
	tests := []struct {
		file   string
		format domain.Format
		want   domain.ImageSize
	}{
		{"exif.jpg", domain.FormatJPEG, domain.ImageSize{Width: 64, Height: 48}},
		{"alpha.png", domain.FormatPNG, domain.ImageSize{Width: 30, Height: 20}},
		{"still.webp", domain.FormatWebP, domain.ImageSize{Width: 48, Height: 36}},
		{"huge.png", domain.FormatPNG, domain.ImageSize{Width: 9000, Height: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			got, err := Codec{}.Size(bytes.NewReader(readFixture(t, tt.file)), tt.format)
			if err != nil {
				t.Fatalf("Size: %v", err)
			}
			if got != tt.want {
				t.Errorf("Size = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestUnreadableImages(t *testing.T) {
	png := readFixture(t, "alpha.png")
	tests := []struct {
		name   string
		data   []byte
		format domain.Format
	}{
		{"text renamed .jpg", readFixture(t, "notimage.jpg"), domain.FormatJPEG},
		{"png cut after the header", png[:40], domain.FormatPNG},
		{"png read as jpeg", png, domain.FormatJPEG},
		{"empty webp", nil, domain.FormatWebP},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := (Codec{}).Render(bytes.NewReader(tt.data), tt.format, 400, 400, 85); !errors.Is(err, domain.ErrInvalidImage) {
				t.Errorf("Render err = %v, want ErrInvalidImage", err)
			}
		})
	}
	if _, err := (Codec{}).Size(bytes.NewReader(readFixture(t, "notimage.jpg")), domain.FormatJPEG); !errors.Is(err, domain.ErrInvalidImage) {
		t.Errorf("Size of a text file: err = %v, want ErrInvalidImage", err)
	}
}

// render renders a fixture and decodes the result, checking that it is a JPEG of w x h.
func render(t *testing.T, file string, f domain.Format, w, h int) ([]byte, image.Image) {
	t.Helper()
	out, err := Codec{}.Render(bytes.NewReader(readFixture(t, file)), f, w, h, 85)
	if err != nil {
		t.Fatalf("Render %s: %v", file, err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("result is not a JPEG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
		t.Fatalf("result is %dx%d, want %dx%d", b.Dx(), b.Dy(), w, h)
	}
	return out, img
}

func TestRenderStripsExif(t *testing.T) {
	in := readFixture(t, "exif.jpg")
	if !bytes.Contains(in, []byte(secret)) || !bytes.Contains(in, []byte("Exif\x00\x00")) {
		t.Fatal("fixture exif.jpg has no EXIF; regenerate it")
	}
	out, _ := render(t, "exif.jpg", domain.FormatJPEG, 400, 400)
	for _, s := range []string{secret, "Exif\x00\x00", "\xFF\xE1"} {
		if bytes.Contains(out, []byte(s)) {
			t.Errorf("result still contains %q", s)
		}
	}
}

func near(c color.Color, want color.RGBA) bool {
	r, g, b, _ := c.RGBA()
	d := func(a uint32, b uint8) bool { return int(a>>8)-int(b) < 40 && int(b)-int(a>>8) < 40 }
	return d(r, want.R) && d(g, want.G) && d(b, want.B)
}

func TestRenderConvertsPNG(t *testing.T) {
	out, img := render(t, "alpha.png", domain.FormatPNG, 400, 400)
	if !bytes.HasPrefix(out, []byte{0xFF, 0xD8, 0xFF}) {
		t.Error("result does not start like a JPEG")
	}
	// The centred 20x20 square spans x 5..25: transparent on the left, green on the right.
	white := color.RGBA{R: 255, G: 255, B: 255}
	if c := img.At(60, 200); !near(c, white) {
		t.Errorf("transparent part = %v, want white", c)
	}
	if c := img.At(340, 200); !near(c, green) {
		t.Errorf("opaque part = %v, want green", c)
	}
}

func TestRenderCover(t *testing.T) {
	render(t, "alpha.png", domain.FormatPNG, 1584, 396)
	render(t, "exif.jpg", domain.FormatJPEG, 1584, 396)
}

func TestRenderWebP(t *testing.T) {
	_, img := render(t, "still.webp", domain.FormatWebP, 400, 400)
	if c := img.At(200, 200); !near(c, color.RGBA{G: 128, B: 255}) {
		t.Errorf("centre = %v, want the fixture's blue", c)
	}
}

func TestRenderTurnsRotatedPhotosUpright(t *testing.T) {
	_, img := render(t, "rotated.jpg", domain.FormatJPEG, 400, 400)
	if c := img.At(200, 50); !near(c, red) {
		t.Errorf("top = %v, want red (the stored left half)", c)
	}
	if c := img.At(200, 350); !near(c, blue) {
		t.Errorf("bottom = %v, want blue (the stored right half)", c)
	}
	// A cover is 4:1 upright, so the crop of the stored image is 1:4.
	render(t, "rotated.jpg", domain.FormatJPEG, 1584, 396)
}

func TestOrient(t *testing.T) {
	a, b, c, d := color.RGBA{R: 1, A: 255}, color.RGBA{R: 2, A: 255}, color.RGBA{R: 3, A: 255}, color.RGBA{R: 4, A: 255}
	src := image.NewRGBA(image.Rect(0, 0, 2, 2)) // A B / C D
	src.SetRGBA(0, 0, a)
	src.SetRGBA(1, 0, b)
	src.SetRGBA(0, 1, c)
	src.SetRGBA(1, 1, d)
	want := map[int][4]color.RGBA{ // top left, top right, bottom left, bottom right
		1: {a, b, c, d},
		2: {b, a, d, c},
		3: {d, c, b, a},
		4: {c, d, a, b},
		5: {a, c, b, d},
		6: {c, a, d, b},
		7: {d, b, c, a},
		8: {b, d, a, c},
		0: {a, b, c, d},
		9: {a, b, c, d},
	}
	for o, w := range want {
		img := orient(src, o)
		got := [4]color.RGBA{}
		for i, p := range []image.Point{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
			got[i] = color.RGBAModel.Convert(img.At(p.X, p.Y)).(color.RGBA) //nolint:forcetypeassert // RGBAModel returns RGBA
		}
		if got != w {
			t.Errorf("orientation %d: got %v, want %v", o, got, w)
		}
	}
	// A non-square image: a quarter turn swaps width and height.
	if b := orient(image.NewRGBA(image.Rect(0, 0, 4, 2)), 6).Bounds(); b.Dx() != 2 || b.Dy() != 4 {
		t.Errorf("orientation 6 of 4x2 = %v, want 2x4", b)
	}
}

func TestExifOrientation(t *testing.T) {
	jpg := encodeJPEG(t, image.NewGray(image.Rect(0, 0, 8, 8)))
	tests := []struct {
		name string
		data []byte
		want int
	}{
		{"no exif", jpg, 1},
		{"little endian", withExif(t, jpg, buildExif(binary.LittleEndian, 8, "x", true)), 8},
		{"big endian", withExif(t, jpg, buildExif(binary.BigEndian, 3, "", false)), 3},
		{"value out of range", withExif(t, jpg, buildExif(binary.LittleEndian, 9, "", false)), 1},
		{"not a jpeg", []byte("hello"), 1},
		{"empty", nil, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exifOrientation(tt.data); got != tt.want {
				t.Errorf("exifOrientation = %d, want %d", got, tt.want)
			}
		})
	}
}

// Every prefix of a file with EXIF, and every byte of it changed, must give a value from
// 1 to 8 and never panic.
func TestExifOrientationOnDamagedFiles(t *testing.T) {
	full := withExif(t, encodeJPEG(t, image.NewGray(image.Rect(0, 0, 8, 8))), buildExif(binary.BigEndian, 6, "desc", true))
	for n := range len(full) {
		if o := exifOrientation(full[:n]); o < 1 || o > 8 {
			t.Fatalf("prefix %d: %d", n, o)
		}
	}
	for i := range 120 {
		for _, v := range []byte{0x00, 0xFF, 0x7F} {
			b := bytes.Clone(full)
			b[i] = v
			if o := exifOrientation(b); o < 1 || o > 8 {
				t.Fatalf("byte %d = %#x: %d", i, v, o)
			}
		}
	}
}

func FuzzExifOrientation(f *testing.F) {
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x10})
	f.Fuzz(func(t *testing.T, b []byte) {
		if o := exifOrientation(b); o < 1 || o > 8 {
			t.Fatalf("exifOrientation = %d", o)
		}
	})
}
