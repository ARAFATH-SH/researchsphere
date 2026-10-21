// Package imaging implements the media domain's ImageCodec with the standard library's
// JPEG and PNG codecs and golang.org/x/image for WebP and scaling (FR-4.6). Every image is
// decoded and encoded again, so nothing of the uploaded file but its pixels is kept:
// no EXIF, GPS position, comment or data appended after the image.
package imaging

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

// Codec reads and re-encodes JPEG, PNG and WebP images.
type Codec struct{}

var _ domain.ImageCodec = Codec{}

// Size reads only the image header.
func (Codec) Size(r io.Reader, f domain.Format) (domain.ImageSize, error) {
	var (
		cfg image.Config
		err error
	)
	switch f {
	case domain.FormatJPEG:
		cfg, err = jpeg.DecodeConfig(r)
	case domain.FormatPNG:
		cfg, err = png.DecodeConfig(r)
	case domain.FormatWebP:
		cfg, err = webp.DecodeConfig(r)
	default:
		return domain.ImageSize{}, fmt.Errorf("%w: unknown format %q", domain.ErrInvalidImage, f)
	}
	if err != nil {
		return domain.ImageSize{}, fmt.Errorf("%w: %v", domain.ErrInvalidImage, err) //nolint:errorlint // the codec's error is detail; the kind is ErrInvalidImage
	}
	return domain.ImageSize{Width: cfg.Width, Height: cfg.Height}, nil
}

// Render decodes the image, crops the largest centred width:height rectangle, scales it
// with Catmull-Rom onto white (transparent pixels become white) and encodes a JPEG. A JPEG
// whose EXIF Orientation says it is rotated or mirrored is turned upright: the crop and
// scale run in the stored orientation, with width and height swapped for a quarter turn,
// and only the small result is rotated.
func (Codec) Render(r io.Reader, f domain.Format, width, height, quality int) ([]byte, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	orientation := 1
	if f == domain.FormatJPEG {
		orientation = exifOrientation(data)
	}
	src, err := decode(bytes.NewReader(data), f)
	if err != nil {
		return nil, err
	}

	w, h := width, height
	if orientation >= 5 { // a quarter turn: the stored image is the other way round
		w, h = height, width
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, centerCrop(src.Bounds(), w, h), draw.Over, nil)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, orient(dst, orientation), &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return out.Bytes(), nil
}

func decode(r io.Reader, f domain.Format) (image.Image, error) {
	var (
		img image.Image
		err error
	)
	switch f {
	case domain.FormatJPEG:
		img, err = jpeg.Decode(r)
	case domain.FormatPNG:
		img, err = png.Decode(r)
	case domain.FormatWebP:
		img, err = webp.Decode(r)
	default:
		return nil, fmt.Errorf("%w: unknown format %q", domain.ErrInvalidImage, f)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrInvalidImage, err) //nolint:errorlint // as in Size
	}
	return img, nil
}

// centerCrop returns the largest rectangle of ratio w:h centred in b.
func centerCrop(b image.Rectangle, w, h int) image.Rectangle {
	bw, bh := b.Dx(), b.Dy()
	cw, ch := bw, bh
	if bw*h > bh*w { // wider than w:h: keep the height
		cw = max(1, bh*w/h)
	} else {
		ch = max(1, bw*h/w)
	}
	x0 := b.Min.X + (bw-cw)/2
	y0 := b.Min.Y + (bh-ch)/2
	return image.Rect(x0, y0, x0+cw, y0+ch)
}

// orient turns img upright for an EXIF Orientation value (1 to 8). 1, and any value
// outside 2 to 8, returns img unchanged.
func orient(img *image.RGBA, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return img
	}
	sw, sh := img.Bounds().Dx(), img.Bounds().Dy()
	dw, dh := sw, sh
	if orientation >= 5 {
		dw, dh = sh, sw
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		for x := range dw {
			var sx, sy int
			switch orientation {
			case 2: // mirrored left to right
				sx, sy = sw-1-x, y
			case 3: // turned 180°
				sx, sy = sw-1-x, sh-1-y
			case 4: // mirrored top to bottom
				sx, sy = x, sh-1-y
			case 5: // mirrored along the main diagonal
				sx, sy = y, x
			case 6: // needs a quarter turn clockwise
				sx, sy = y, sh-1-x
			case 7: // mirrored along the other diagonal
				sx, sy = sw-1-y, sh-1-x
			case 8: // needs a quarter turn anticlockwise
				sx, sy = sw-1-y, x
			}
			dst.SetRGBA(x, y, img.RGBAAt(img.Bounds().Min.X+sx, img.Bounds().Min.Y+sy))
		}
	}
	return dst
}
