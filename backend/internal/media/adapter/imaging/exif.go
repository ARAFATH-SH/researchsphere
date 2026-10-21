package imaging

import (
	"bytes"
	"encoding/binary"
)

// exifOrientation returns the Orientation tag (0x0112) of the first IFD of a JPEG's EXIF
// segment, or 1 (upright) when there is none or the data cannot be read. It reads only
// the segment headers before the image data and checks every offset, so a malformed or
// hostile file gives 1, never a panic.
func exifOrientation(jpg []byte) int {
	if len(jpg) < 4 || jpg[0] != 0xFF || jpg[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(jpg); {
		if jpg[i] != 0xFF {
			return 1
		}
		marker := jpg[i+1]
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2 // markers without a length
			continue
		}
		if marker == 0xDA || marker == 0xD9 { // image data starts, or the image ends
			return 1
		}
		n := int(binary.BigEndian.Uint16(jpg[i+2:])) // includes the two length bytes
		if n < 2 || i+2+n > len(jpg) {
			return 1
		}
		seg := jpg[i+4 : i+2+n]
		if marker == 0xE1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return tiffOrientation(seg[6:])
		}
		i += 2 + n
	}
	return 1
}

// tiffOrientation reads the Orientation from the first IFD of a TIFF header.
func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(t[:4]) {
	case "II*\x00":
		order = binary.LittleEndian
	case "MM\x00*":
		order = binary.BigEndian
	default:
		return 1
	}
	ifd := int(order.Uint32(t[4:]))
	if ifd < 8 || ifd+2 > len(t) {
		return 1
	}
	count := int(order.Uint16(t[ifd:]))
	for e := range count {
		at := ifd + 2 + 12*e
		if at+12 > len(t) {
			return 1
		}
		// Tag 0x0112, type SHORT (3), count 1: the value is in the first two value bytes.
		if order.Uint16(t[at:]) == 0x0112 && order.Uint16(t[at+2:]) == 3 && order.Uint32(t[at+4:]) == 1 {
			v := int(order.Uint16(t[at+8:]))
			if v < 1 || v > 8 {
				return 1
			}
			return v
		}
	}
	return 1
}
