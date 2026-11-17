package usecase

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"slices"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

// FR-12.2 (docs/decisions.md 2026-11-18): a figure's metadata is removed losslessly. The
// image is never decoded or encoded again, so what remains is byte for byte the upload.

// testImage is a 64x48 picture with every pixel different, so any change shows.
func testImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			img.Set(x, y, color.NRGBA{R: uint8(x * 4), G: uint8(y * 5), B: uint8(x ^ y), A: 255}) //nolint:gosec // x < 64, y < 48
		}
	}
	return img
}

// pixels lists every pixel's 16-bit RGBA, whatever the image's type.
func pixels(img image.Image) []byte {
	var out []byte
	r := img.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			cr, cg, cb, ca := img.At(x, y).RGBA()
			out = binary.BigEndian.AppendUint64(out, uint64(cr)<<48|uint64(cg)<<32|uint64(cb)<<16|uint64(ca))
		}
	}
	return out
}

func pngChunk(typ string, data []byte) []byte {
	b := binary.BigEndian.AppendUint32(nil, uint32(len(data))) //nolint:gosec // small test chunks
	b = append(b, typ...)
	b = append(b, data...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(append([]byte(typ), data...)))
}

// splitPNG returns the signature with IHDR, and the chunks after IHDR.
func splitPNG(t *testing.T, b []byte) (head, rest []byte) {
	t.Helper()
	n := 8 + 8 + 13 + 4
	if !bytes.Equal(b[12:16], []byte("IHDR")) {
		t.Fatal("IHDR is not first")
	}
	return slices.Clone(b[:n]), slices.Clone(b[n:])
}

func strip(t *testing.T, in []byte) ([]byte, domain.FigureInfo, error) {
	t.Helper()
	var out bytes.Buffer
	info, err := StripFigure(bytes.NewReader(in), &out)
	return out.Bytes(), info, err
}

func TestStripFigurePNG(t *testing.T) {
	var enc bytes.Buffer
	if err := png.Encode(&enc, testImage()); err != nil {
		t.Fatal(err)
	}
	head, rest := splitPNG(t, enc.Bytes())
	gama := pngChunk("gAMA", []byte{0, 0, 0xB1, 0x8F})
	icc := pngChunk("iCCP", []byte("sRGB\x00\x00x\x9c"))
	var in []byte
	in = append(in, head...)
	in = append(in, pngChunk("tEXt", []byte("Comment\x00secret note"))...)
	in = append(in, gama...)
	in = append(in, pngChunk("eXIf", []byte("MM\x00\x2aGPS 51.5N 0.1W"))...)
	in = append(in, icc...)
	in = append(in, pngChunk("tIME", []byte{0x07, 0xEA, 11, 18, 10, 0, 0})...)
	in = append(in, pngChunk("iTXt", []byte("Author\x00\x00\x00\x00\x00Someone"))...)
	in = append(in, pngChunk("zTXt", []byte("Software\x00\x00x\x9c"))...)
	in = append(in, pngChunk("prVt", []byte("private data"))...)
	in = append(in, rest...)
	in = append(in, []byte("appended after IEND")...)

	out, info, err := strip(t, in)
	if err != nil {
		t.Fatal(err)
	}
	if info != (domain.FigureInfo{ContentType: TypePNG, Width: 64, Height: 48}) {
		t.Errorf("info = %+v", info)
	}
	// Exactly the encoder's output with the colour chunks kept: every IDAT byte as uploaded.
	want := slices.Concat(head, gama, icc, rest)
	if !bytes.Equal(out, want) {
		t.Fatalf("stripped PNG differs from the expected bytes (%d vs %d)", len(out), len(want))
	}
	for _, gone := range []string{"secret", "GPS", "tIME", "Someone", "Software", "private", "appended"} {
		if bytes.Contains(out, []byte(gone)) {
			t.Errorf("%q is still in the file", gone)
		}
	}
	// The pixels are identical, decoded from the upload and from what is stored.
	a, err := png.Decode(bytes.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	b, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pixels(a), pixels(b)) || !bytes.Equal(pixels(b), pixels(testImage())) {
		t.Error("pixel data changed")
	}

	// A PNG with nothing to remove is copied unchanged.
	if out, _, err := strip(t, enc.Bytes()); err != nil || !bytes.Equal(out, enc.Bytes()) {
		t.Errorf("a clean PNG changed: %v", err)
	}
}

// jpegSegment is a marker segment: 0xFF, the marker, its length and payload.
func jpegSegment(m byte, payload string) []byte {
	b := []byte{0xFF, m}
	b = binary.BigEndian.AppendUint16(b, uint16(len(payload)+2)) //nolint:gosec // small test segments
	return append(b, payload...)
}

func TestStripFigureJPEG(t *testing.T) {
	var enc bytes.Buffer
	if err := jpeg.Encode(&enc, testImage(), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	base := enc.Bytes() // SOI, then the image segments, the scan and EOI
	jfif := jpegSegment(0xE0, "JFIF\x00\x01\x02\x00\x00\x48\x00\x48\x00\x00")
	icc := jpegSegment(0xE2, "ICC_PROFILE\x00\x01\x01fake profile")
	adobe := jpegSegment(0xEE, "Adobe\x00\x64\x00\x00\x00\x00\x01")
	var in []byte
	in = append(in, base[:2]...)
	in = append(in, jfif...)
	in = append(in, jpegSegment(0xE1, "Exif\x00\x00MM\x00\x2aGPS 51.5N 0.1W Orientation 6")...)
	in = append(in, jpegSegment(0xE1, "http://ns.adobe.com/xap/1.0/\x00<x:xmpmeta>creator</x:xmpmeta>")...)
	in = append(in, icc...)
	in = append(in, jpegSegment(0xE2, "MPF\x00second picture")...)
	in = append(in, jpegSegment(0xED, "Photoshop 3.0\x008BIM iptc caption")...)
	in = append(in, jpegSegment(0xE0, "JFXX\x00\x10thumbnail")...)
	in = append(in, adobe...)
	in = append(in, jpegSegment(0xFE, "a comment")...)
	in = append(in, base[2:]...)
	in = append(in, []byte("appended after EOI")...)

	out, info, err := strip(t, in)
	if err != nil {
		t.Fatal(err)
	}
	if info != (domain.FigureInfo{ContentType: TypeJPEG, Width: 64, Height: 48}) {
		t.Errorf("info = %+v", info)
	}
	want := slices.Concat(base[:2], jfif, icc, adobe, base[2:])
	if !bytes.Equal(out, want) {
		t.Fatalf("stripped JPEG differs from the expected bytes (%d vs %d)", len(out), len(want))
	}
	for _, gone := range []string{"Exif", "GPS", "xmpmeta", "MPF", "iptc", "thumbnail", "comment", "appended"} {
		if bytes.Contains(out, []byte(gone)) {
			t.Errorf("%q is still in the file", gone)
		}
	}
	a, err := jpeg.Decode(bytes.NewReader(base))
	if err != nil {
		t.Fatal(err)
	}
	b, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	ya, yb := a.(*image.YCbCr), b.(*image.YCbCr)
	if !bytes.Equal(ya.Y, yb.Y) || !bytes.Equal(ya.Cb, yb.Cb) || !bytes.Equal(ya.Cr, yb.Cr) {
		t.Error("pixel data changed")
	}
	if out, _, err := strip(t, base); err != nil || !bytes.Equal(out, base) {
		t.Errorf("a clean JPEG changed: %v", err)
	}
}

// A progressive JPEG has several scans with segments between them; the image data keeps its
// stuffed bytes and restart markers exactly.
func TestStripFigureJPEGScans(t *testing.T) {
	sof := jpegSegment(0xC2, "\x08\x00\x10\x00\x20\x01\x01\x11\x00")
	sos := jpegSegment(0xDA, "\x01\x01\x00\x00\x3F\x00")
	dht := jpegSegment(0xC4, "\x00\x00")
	scan1 := []byte{0x12, 0xFF, 0x00, 0x34, 0xFF, 0xD0, 0x56, 0xFF, 0x00}
	scan2 := []byte{0x9A, 0xFF, 0xD1, 0xBC}
	in := slices.Concat([]byte{0xFF, 0xD8}, sof, sos, scan1, jpegSegment(0xFE, "between scans"), dht, sos, scan2, []byte{0xFF, 0xD9})
	out, info, err := strip(t, in)
	if err != nil {
		t.Fatal(err)
	}
	want := slices.Concat([]byte{0xFF, 0xD8}, sof, sos, scan1, dht, sos, scan2, []byte{0xFF, 0xD9})
	if !bytes.Equal(out, want) {
		t.Errorf("out = % x\nwant  % x", out, want)
	}
	if info.Width != 32 || info.Height != 16 {
		t.Errorf("info = %+v", info)
	}
}

func TestStripFigureTypes(t *testing.T) {
	pdf := []byte("%PDF-1.5\n1 0 obj <<>> endobj\n%%EOF\nanything")
	out, info, err := strip(t, pdf)
	if err != nil || !bytes.Equal(out, pdf) || info != (domain.FigureInfo{ContentType: TypePDF}) {
		t.Errorf("a PDF must be stored as uploaded: %+v, %v", info, err)
	}
	for name, data := range map[string][]byte{
		"webp":        append(slices.Clone(webpHead), 1, 2, 3),
		"gif":         []byte("GIF89a\x01\x00\x01\x00"),
		"renamed exe": append([]byte("MZ\x90\x00"), make([]byte, 64)...),
		"svg":         []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"eps":         []byte("%!PS-Adobe-3.0 EPSF-3.0\n"),
		"empty":       nil,
	} {
		if _, _, err := strip(t, data); !errors.Is(err, domain.ErrUnsupportedType) {
			t.Errorf("%s: err = %v, want ErrUnsupportedType", name, err)
		}
	}
}

func TestStripFigureInvalid(t *testing.T) {
	var p bytes.Buffer
	if err := png.Encode(&p, testImage()); err != nil {
		t.Fatal(err)
	}
	head, rest := splitPNG(t, p.Bytes())
	badCRC := slices.Clone(p.Bytes())
	badCRC[len(head)+10] ^= 0xFF // inside the first IDAT
	var j bytes.Buffer
	if err := jpeg.Encode(&j, testImage(), nil); err != nil {
		t.Fatal(err)
	}
	jp := j.Bytes()
	firstSeg := 4 + int(binary.BigEndian.Uint16(jp[4:6])) // SOI and the segment after it
	for name, data := range map[string][]byte{
		"png cut in its data":     p.Bytes()[:len(p.Bytes())/2],
		"png without IEND":        p.Bytes()[:len(p.Bytes())-12],
		"png with a bad CRC":      badCRC,
		"png unknown critical":    slices.Concat(head, pngChunk("ABCD", []byte("x")), rest),
		"png without IDAT":        slices.Concat(head, pngChunk("IEND", nil)),
		"png without IHDR first":  slices.Concat(pngSignature, pngChunk("gAMA", []byte{0, 0, 0, 1}), head[8:], rest),
		"jpeg cut in its data":    jp[:len(jp)-40],
		"jpeg without image data": slices.Concat([]byte{0xFF, 0xD8}, jpegSegment(0xFE, "x"), []byte{0xFF, 0xD9}),
		"jpeg scan before frame":  slices.Concat([]byte{0xFF, 0xD8}, jpegSegment(0xDA, "\x01\x01\x00\x00\x3F\x00"), []byte{0x01, 0xFF, 0xD9}),
		"jpeg junk between":       slices.Concat(jp[:firstSeg], []byte{0x00, 0x01}, jp[firstSeg:]),
		"jpeg short segment":      slices.Concat(jp[:2], []byte{0xFF, 0xE1, 0x00, 0x40, 'E'}),
	} {
		if _, _, err := strip(t, data); !errors.Is(err, domain.ErrInvalidImage) {
			t.Errorf("%s: err = %v, want ErrInvalidImage", name, err)
		}
	}
}

type failingWriter struct{}

var errDisk = errors.New("disk full")

func (failingWriter) Write([]byte) (int, error) { return 0, errDisk }

// A destination that fails is reported as such, never as a broken image.
func TestStripFigureWriteError(t *testing.T) {
	var p bytes.Buffer
	if err := png.Encode(&p, testImage()); err != nil {
		t.Fatal(err)
	}
	for _, in := range [][]byte{p.Bytes(), []byte("%PDF-1.4\n")} {
		if _, err := StripFigure(bytes.NewReader(in), failingWriter{}); !errors.Is(err, errDisk) || errors.Is(err, domain.ErrInvalidImage) {
			t.Errorf("err = %v, want the write error", err)
		}
	}
}
