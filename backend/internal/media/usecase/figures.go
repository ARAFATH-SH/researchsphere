package usecase

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

// StripFigure checks a project figure by its content and copies it from r to w without its
// metadata (FR-12.2, docs/decisions.md 2026-11-18): a PNG or JPEG losslessly, never decoded
// or encoded again, so its pixels stay exactly as uploaded; a PDF as it is. WebP is refused,
// as pdfLaTeX cannot include it. The name and content type the client sent are never used.
//
// JPEG keeps its image segments and entropy-coded data byte for byte, JFIF (APP0), the ICC
// colour profile (APP2 ICC_PROFILE) and Adobe's colour transform marker (APP14, needed to
// read CMYK); it drops EXIF and XMP (APP1), IPTC (APP13), every other application segment,
// comments and anything after the image's end. EXIF's orientation goes too: pdfLaTeX
// ignores it, so the preview and the compiled paper agree. PNG keeps its critical chunks and
// the colour and layout chunks, checking every chunk's CRC; it drops eXIf, the text and time
// chunks, every other ancillary chunk and anything after IEND.
//
// Errors: domain.ErrUnsupportedType, domain.ErrInvalidImage (a structure that cannot be
// read, a bad CRC, a truncated file), or the error of r or w unchanged.
func StripFigure(r io.Reader, w io.Writer) (domain.FigureInfo, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	head, err := br.Peek(8)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return domain.FigureInfo{}, err
	}
	out := &errWriter{w: w}
	var info domain.FigureInfo
	switch {
	case bytes.HasPrefix(head, pngSignature):
		info, err = stripPNG(br, out)
		info.ContentType = TypePNG
	case bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF}):
		info, err = stripJPEG(br, out)
		info.ContentType = TypeJPEG
	case bytes.HasPrefix(head, []byte("%PDF-")):
		_, err = io.Copy(out, br)
		info.ContentType = TypePDF
	default:
		return domain.FigureInfo{}, domain.ErrUnsupportedType
	}
	if out.err != nil {
		return domain.FigureInfo{}, out.err
	}
	if err != nil {
		return domain.FigureInfo{}, err
	}
	return info, nil
}

// errWriter keeps the first write error, so a failing destination is never mistaken for a
// broken image.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	n, err := e.w.Write(p)
	e.err = err
	return n, err
}

// invalid wraps a read error: the file ended early or broke a rule. A write error is kept
// by errWriter and wins in StripFigure.
func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", domain.ErrInvalidImage, fmt.Sprintf(format, args...))
}

// ---------------------------------------------------------------------------
// JPEG
// ---------------------------------------------------------------------------

// JPEG markers used here.
const (
	markerSOI  = 0xD8
	markerEOI  = 0xD9
	markerSOS  = 0xDA
	markerTEM  = 0x01
	markerAPP0 = 0xE0
	markerAPP2 = 0xE2
	markerAPPE = 0xEE
	markerCOM  = 0xFE
)

func isRST(m byte) bool { return m >= 0xD0 && m <= 0xD7 }

// isSOF is a start of frame: C0 to CF, except DHT (C4), JPG (C8) and DAC (CC).
func isSOF(m byte) bool { return m >= 0xC0 && m <= 0xCF && m != 0xC4 && m != 0xC8 && m != 0xCC }

// keepJPEGSegment says whether a segment stays: every segment of the image itself, and of
// the application segments only JFIF, the ICC profile and Adobe's marker.
func keepJPEGSegment(m byte, payload []byte) bool {
	switch {
	case m == markerCOM:
		return false
	case m == markerAPP0:
		return bytes.HasPrefix(payload, []byte("JFIF\x00")) // not JFXX, which holds a thumbnail
	case m == markerAPP2:
		return bytes.HasPrefix(payload, []byte("ICC_PROFILE\x00"))
	case m == markerAPPE:
		return bytes.HasPrefix(payload, []byte("Adobe"))
	case m >= 0xE0 && m <= 0xEF:
		return false
	}
	return true
}

func stripJPEG(br *bufio.Reader, w io.Writer) (domain.FigureInfo, error) {
	var soi [2]byte
	if _, err := io.ReadFull(br, soi[:]); err != nil {
		return domain.FigureInfo{}, invalid("jpeg: %v", err)
	}
	_, _ = w.Write(soi[:])
	var (
		info            domain.FigureInfo
		sawSOF, sawScan bool
		next            byte // a marker the scan copy already read
	)
	for {
		m := next
		next = 0
		if m == 0 {
			var err error
			if m, err = readMarker(br); err != nil {
				return domain.FigureInfo{}, err
			}
		}
		switch {
		case m == markerEOI:
			if !sawScan {
				return domain.FigureInfo{}, invalid("jpeg: no image data")
			}
			_, _ = w.Write([]byte{0xFF, markerEOI})
			return info, nil // anything after the image's end is dropped
		case m == markerSOI:
			return domain.FigureInfo{}, invalid("jpeg: a second start of image")
		case isRST(m) || m == markerTEM:
			_, _ = w.Write([]byte{0xFF, m})
			continue
		}
		var lb [2]byte
		if _, err := io.ReadFull(br, lb[:]); err != nil {
			return domain.FigureInfo{}, invalid("jpeg: %v", err)
		}
		n := int(binary.BigEndian.Uint16(lb[:]))
		if n < 2 {
			return domain.FigureInfo{}, invalid("jpeg: segment length %d", n)
		}
		payload := make([]byte, n-2)
		if _, err := io.ReadFull(br, payload); err != nil {
			return domain.FigureInfo{}, invalid("jpeg: %v", err)
		}
		if isSOF(m) {
			if len(payload) < 5 {
				return domain.FigureInfo{}, invalid("jpeg: short frame header")
			}
			info.Height = int(binary.BigEndian.Uint16(payload[1:3]))
			info.Width = int(binary.BigEndian.Uint16(payload[3:5]))
			if info.Width == 0 || info.Height == 0 {
				return domain.FigureInfo{}, invalid("jpeg: no size in the frame header")
			}
			sawSOF = true
		}
		if keepJPEGSegment(m, payload) {
			_, _ = w.Write([]byte{0xFF, m})
			_, _ = w.Write(lb[:])
			_, _ = w.Write(payload)
		}
		if m == markerSOS {
			if !sawSOF {
				return domain.FigureInfo{}, invalid("jpeg: image data before the frame header")
			}
			sawScan = true
			var err error
			if next, err = copyScan(br, w); err != nil {
				return domain.FigureInfo{}, err
			}
		}
	}
}

// readMarker reads 0xFF, any fill bytes, and the marker after them.
func readMarker(br *bufio.Reader) (byte, error) {
	b, err := br.ReadByte()
	if err != nil {
		return 0, invalid("jpeg: %v", err)
	}
	if b != 0xFF {
		return 0, invalid("jpeg: expected a marker, found %#x", b)
	}
	for b == 0xFF {
		if b, err = br.ReadByte(); err != nil {
			return 0, invalid("jpeg: %v", err)
		}
	}
	if b == 0 {
		return 0, invalid("jpeg: a stuffed byte outside image data")
	}
	return b, nil
}

// copyScan copies entropy-coded data as it is, with its stuffed 0xFF 0x00 bytes and restart
// markers, up to the next other marker, which it returns.
func copyScan(br *bufio.Reader, w io.Writer) (byte, error) {
	for {
		chunk, err := br.ReadSlice(0xFF)
		if errors.Is(err, bufio.ErrBufferFull) {
			_, _ = w.Write(chunk)
			continue
		}
		if err != nil {
			return 0, invalid("jpeg: image data ends early: %v", err)
		}
		_, _ = w.Write(chunk[:len(chunk)-1])
		b, err := br.ReadByte()
		for err == nil && b == 0xFF { // fill bytes before a marker
			b, err = br.ReadByte()
		}
		if err != nil {
			return 0, invalid("jpeg: image data ends early: %v", err)
		}
		if b == 0x00 || isRST(b) {
			_, _ = w.Write([]byte{0xFF, b})
			continue
		}
		return b, nil
	}
}

// ---------------------------------------------------------------------------
// PNG
// ---------------------------------------------------------------------------

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}

// keptPNGChunks are the ancillary chunks that change how the image looks or is laid out
// (colour, transparency, gamma, physical size, animation); every other ancillary chunk,
// eXIf and the text and time chunks included, is dropped.
var keptPNGChunks = map[string]bool{
	"tRNS": true, "gAMA": true, "cHRM": true, "sRGB": true, "iCCP": true, "sBIT": true,
	"pHYs": true, "bKGD": true, "cICP": true, "mDCV": true, "cLLI": true,
	"acTL": true, "fcTL": true, "fdAT": true,
}

// criticalPNGChunks are the chunks a PNG decoder must understand.
var criticalPNGChunks = map[string]bool{"IHDR": true, "PLTE": true, "IDAT": true, "IEND": true}

func stripPNG(br *bufio.Reader, w io.Writer) (domain.FigureInfo, error) {
	sig := make([]byte, len(pngSignature))
	if _, err := io.ReadFull(br, sig); err != nil {
		return domain.FigureInfo{}, invalid("png: %v", err)
	}
	_, _ = w.Write(sig)
	var (
		info    domain.FigureInfo
		first   = true
		sawIDAT bool
	)
	for {
		var hdr [8]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			return domain.FigureInfo{}, invalid("png: no IEND: %v", err)
		}
		n := binary.BigEndian.Uint32(hdr[:4])
		typ := string(hdr[4:8])
		if n > 1<<31-1 {
			return domain.FigureInfo{}, invalid("png: chunk length %d", n)
		}
		for _, c := range hdr[4:8] {
			if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
				return domain.FigureInfo{}, invalid("png: chunk type %q", typ)
			}
		}
		if first != (typ == "IHDR") {
			return domain.FigureInfo{}, invalid("png: IHDR is not the first chunk")
		}
		first = false
		critical := hdr[4]&0x20 == 0
		if critical && !criticalPNGChunks[typ] {
			return domain.FigureInfo{}, invalid("png: unknown critical chunk %q", typ)
		}
		keep := critical || keptPNGChunks[typ]

		crc := crc32.NewIEEE()
		_, _ = crc.Write(hdr[4:8])
		dst := io.Writer(crc)
		if keep {
			_, _ = w.Write(hdr[:])
			dst = io.MultiWriter(w, crc)
		}
		if typ == "IHDR" {
			if n != 13 {
				return domain.FigureInfo{}, invalid("png: IHDR length %d", n)
			}
			var d [13]byte
			if _, err := io.ReadFull(br, d[:]); err != nil {
				return domain.FigureInfo{}, invalid("png: %v", err)
			}
			info.Width = int(binary.BigEndian.Uint32(d[0:4]))
			info.Height = int(binary.BigEndian.Uint32(d[4:8]))
			if info.Width == 0 || info.Height == 0 || info.Width > 1<<31-1 || info.Height > 1<<31-1 {
				return domain.FigureInfo{}, invalid("png: size %dx%d", info.Width, info.Height)
			}
			_, _ = dst.Write(d[:])
		} else if _, err := io.CopyN(dst, br, int64(n)); err != nil {
			return domain.FigureInfo{}, invalid("png: chunk %q ends early: %v", typ, err)
		}
		var sum [4]byte
		if _, err := io.ReadFull(br, sum[:]); err != nil {
			return domain.FigureInfo{}, invalid("png: %v", err)
		}
		if binary.BigEndian.Uint32(sum[:]) != crc.Sum32() {
			return domain.FigureInfo{}, invalid("png: chunk %q has a bad CRC", typ)
		}
		if keep {
			_, _ = w.Write(sum[:])
		}
		switch typ {
		case "IDAT":
			sawIDAT = true
		case "IEND":
			if !sawIDAT {
				return domain.FigureInfo{}, invalid("png: no image data")
			}
			return info, nil // anything after IEND is dropped
		}
	}
}
