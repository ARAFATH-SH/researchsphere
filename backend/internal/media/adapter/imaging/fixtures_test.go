package imaging

import (
	"bytes"
	"encoding/binary"
	"flag"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// The fixtures in testdata/ are made by this file, except the WebP ones (testdata/README.md):
//
//	go test ./internal/media/adapter/imaging -run TestWriteFixtures -update
var update = flag.Bool("update", false, "write the image fixtures in testdata/")

// secret is in the EXIF of exif.jpg; no stored image may contain it.
const secret = "SECRET-EXIF-MARKER"

var (
	red   = color.RGBA{R: 255, A: 255}
	blue  = color.RGBA{B: 255, A: 255}
	green = color.RGBA{G: 255, A: 255}
)

// fixtures returns each generated fixture by file name.
func fixtures(t *testing.T) map[string][]byte {
	t.Helper()
	// exif.jpg: 64x48, with an EXIF segment holding a description and a GPS position.
	gradient := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			gradient.SetRGBA(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 5), B: 128, A: 255})
		}
	}
	// rotated.jpg: stored 64x32, left half red, right half blue, with Orientation 6: shown
	// upright it is 32x64 with red on top.
	halves := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := range 32 {
		for x := range 64 {
			halves.SetRGBA(x, y, map[bool]color.RGBA{true: red, false: blue}[x < 32])
		}
	}
	// alpha.png: 30x20, left half fully transparent, right half green.
	alpha := image.NewNRGBA(image.Rect(0, 0, 30, 20))
	for y := range 20 {
		for x := 15; x < 30; x++ {
			alpha.SetNRGBA(x, y, color.NRGBA{G: 255, A: 255})
		}
	}
	return map[string][]byte{
		"exif.jpg":    withExif(t, encodeJPEG(t, gradient), buildExif(binary.LittleEndian, 1, secret, true)),
		"rotated.jpg": withExif(t, encodeJPEG(t, halves), buildExif(binary.BigEndian, 6, "", false)),
		"alpha.png":   encodePNG(t, alpha),
		// huge.png: 9000x1. Its header states a side over the limit; the file is tiny.
		"huge.png":     encodePNG(t, image.NewGray(image.Rect(0, 0, 9000, 1))),
		"notimage.jpg": []byte("This is a text file with a .jpg name. It is not an image.\n"),
	}
}

func TestWriteFixtures(t *testing.T) {
	if !*update {
		t.Skip("run with -update to write the fixtures")
	}
	for name, data := range fixtures(t) {
		if err := os.WriteFile(filepath.Join("testdata", name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // fixed test file names
	if err != nil {
		t.Fatalf("read fixture: %v (go test -run TestWriteFixtures -update writes them)", err)
	}
	return b
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// withExif puts an APP1 segment holding tiff right after the JPEG's start marker.
func withExif(t *testing.T, jpg, tiff []byte) []byte {
	t.Helper()
	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2)) //nolint:gosec // small test data
	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	out = append(out, payload...)
	return append(out, jpg[2:]...)
}

// buildExif returns a TIFF header with one IFD: an ImageDescription when desc is set, the
// Orientation, and a GPS IFD with a latitude reference when gps is set.
func buildExif(order binary.ByteOrder, orientation uint16, desc string, gps bool) []byte {
	type entry struct {
		tag, typ uint16
		count    uint32
		value    []byte // 4 bytes, or the data the entry points to
	}
	var entries []entry
	if desc != "" {
		entries = append(entries, entry{0x010E, 2, uint32(len(desc) + 1), append([]byte(desc), 0)}) //nolint:gosec // small test data
	}
	ov := make([]byte, 4)
	order.PutUint16(ov, orientation)
	entries = append(entries, entry{0x0112, 3, 1, ov})
	if gps {
		entries = append(entries, entry{0x8825, 4, 1, nil}) // offset filled in below
	}

	b := make([]byte, 8)
	if order == binary.LittleEndian {
		copy(b, "II*\x00")
	} else {
		copy(b, "MM\x00*")
	}
	order.PutUint32(b[4:], 8)
	dataAt := 8 + 2 + 12*len(entries) + 4
	ifd := make([]byte, 2+12*len(entries)+4)
	order.PutUint16(ifd, uint16(len(entries))) //nolint:gosec // small test data
	var data []byte
	for i, e := range entries {
		at := 2 + 12*i
		order.PutUint16(ifd[at:], e.tag)
		order.PutUint16(ifd[at+2:], e.typ)
		order.PutUint32(ifd[at+4:], e.count)
		switch {
		case e.tag == 0x8825:
			gpsAt := dataAt + len(data)
			if gpsAt%2 == 1 {
				data = append(data, 0)
				gpsAt++
			}
			order.PutUint32(ifd[at+8:], uint32(gpsAt)) //nolint:gosec // small test data
			g := make([]byte, 2+12+4)
			order.PutUint16(g, 1)
			order.PutUint16(g[2:], 0x0001) // GPSLatitudeRef
			order.PutUint16(g[4:], 2)
			order.PutUint32(g[6:], 2)
			copy(g[10:], "N\x00")
			data = append(data, g...)
		case len(e.value) > 4:
			order.PutUint32(ifd[at+8:], uint32(dataAt+len(data))) //nolint:gosec // small test data
			data = append(data, e.value...)
		default:
			copy(ifd[at+8:], e.value)
		}
	}
	b = append(b, ifd...)
	return append(b, data...)
}
