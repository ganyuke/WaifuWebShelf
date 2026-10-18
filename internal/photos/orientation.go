package photos

import (
	"encoding/binary"
)

// orientation reads the EXIF orientation note (1 to 8) from a JPEG, PNG or
// WebP file, or returns 1 when there is none.
func orientation(data []byte, format string) int {
	var tiff []byte
	switch format {
	case "jpeg":
		tiff = jpegExif(data)
	case "png":
		tiff = pngChunk(data, "eXIf")
	case "webp":
		tiff = webpChunk(data, "EXIF")
	}
	// Some writers keep the JPEG-style prefix inside PNG and WebP too.
	if len(tiff) >= 6 && string(tiff[:6]) == "Exif\x00\x00" {
		tiff = tiff[6:]
	}
	if o := tiffOrientation(tiff); o >= 1 && o <= 8 {
		return o
	}
	return 1
}

// jpegExif returns the TIFF part of the first EXIF segment before the image data.
func jpegExif(b []byte) []byte {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return nil
	}
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xFF {
			return nil
		}
		marker := b[i+1]
		if marker == 0xFF {
			i++
			continue
		}
		if marker == 0xDA || marker == 0xD9 {
			return nil
		}
		if marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		end := i + 2 + int(binary.BigEndian.Uint16(b[i+2:]))
		if end > len(b) || end < i+4 {
			return nil
		}
		if body := b[i+4 : end]; marker == 0xE1 && len(body) > 6 && string(body[:6]) == "Exif\x00\x00" {
			return body[6:]
		}
		i = end
	}
	return nil
}

func pngChunk(b []byte, kind string) []byte {
	for i := 8; i+12 <= len(b); {
		n := int(binary.BigEndian.Uint32(b[i:]))
		end := i + 12 + n
		if n < 0 || end > len(b) || end < i {
			return nil
		}
		if string(b[i+4:i+8]) == kind {
			return b[i+8 : i+8+n]
		}
		i = end
	}
	return nil
}

func webpChunk(b []byte, kind string) []byte {
	for i := 12; i+8 <= len(b); {
		n := int(binary.LittleEndian.Uint32(b[i+4:]))
		end := i + 8 + n
		if n < 0 || end > len(b) || end < i {
			return nil
		}
		if string(b[i:i+4]) == kind {
			return b[i+8 : end]
		}
		i = end + n%2
	}
	return nil
}

// tiffOrientation reads tag 0x0112 from the first directory, or returns 0.
func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	ifd := int(bo.Uint32(t[4:]))
	if ifd < 8 || ifd+2 > len(t) {
		return 0
	}
	n := int(bo.Uint16(t[ifd:]))
	for k := 0; k < n; k++ {
		e := ifd + 2 + 12*k
		if e+12 > len(t) {
			return 0
		}
		if bo.Uint16(t[e:]) == 0x0112 && bo.Uint16(t[e+2:]) == 3 {
			return int(bo.Uint16(t[e+8:]))
		}
	}
	return 0
}
