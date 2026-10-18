// Package photos prepares uploaded pictures for the site. Each one is turned
// the way the camera's orientation note says, shrunk to a size that suits the
// web, and saved fresh, so none of the original metadata (like where a phone
// photo was taken) carries over. A small version is made for grids and
// thumbnails.
//
// Pictures are saved as WebP when libwebp is installed, and otherwise as JPEG,
// or PNG when they have transparency. Animated GIFs keep only their first frame.
package photos

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	// FullMax and SmallMax are the longest side, in pixels, of the two versions.
	FullMax  = 2000
	SmallMax = 600
	// maxPixels keeps one upload from taking more memory than a small server has.
	maxPixels = 50_000_000

	jpegQuality = 82
	webpQuality = 80
)

var ErrTooLarge = fmt.Errorf("image has too many pixels, use one under %d megapixels", maxPixels/1_000_000)

// Result holds the encoded versions and the file extension they share.
type Result struct {
	Ext   string
	Full  []byte
	Small []byte
}

// busy lets one picture be processed at a time, which bounds memory use.
var busy = make(chan struct{}, 1)

// Process decodes data and returns the prepared picture. Small is only made
// when withSmall is set.
func Process(data []byte, withSmall bool) (Result, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Result{}, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return Result{}, ErrTooLarge
	}
	busy <- struct{}{}
	defer func() { <-busy }()

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Result{}, err
	}
	// Shrinking first keeps the rotation work small. The longest side is the
	// same either way round.
	full := orient(shrink(src, FullMax), orientation(data, format))
	enc := chooseEncoder(full)
	var res Result
	res.Ext = enc.ext
	if res.Full, err = enc.encode(full); err != nil {
		return Result{}, err
	}
	if withSmall {
		if res.Small, err = enc.encode(shrink(full, SmallMax)); err != nil {
			return Result{}, err
		}
	}
	return res, nil
}

// SmallName is the file name of the small version of name.
func SmallName(name string) string {
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext) + "-sm" + ext
}

// FullName undoes SmallName. ok is false when name is not a small version.
func FullName(name string) (string, bool) {
	ext := filepath.Ext(name)
	base, ok := strings.CutSuffix(strings.TrimSuffix(name, ext), "-sm")
	return base + ext, ok
}

// shrink scales img down so its longest side is at most limit, and always
// returns a copy as *image.RGBA.
func shrink(img image.Image, limit int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= limit && h <= limit {
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
		return dst
	}
	if w >= h {
		h = max(1, (h*limit+w/2)/w)
		w = limit
	} else {
		w = max(1, (w*limit+h/2)/h)
		h = limit
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	return dst
}

// orient turns src as EXIF orientation o (1 to 8) asks, so the pixels show
// the way the camera meant without the note.
func orient(src *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var sx, sy int
			switch o {
			case 2: // mirrored
				sx, sy = w-1-x, y
			case 3: // upside down
				sx, sy = w-1-x, h-1-y
			case 4: // mirrored upside down
				sx, sy = x, h-1-y
			case 5: // mirrored, turned left
				sx, sy = y, x
			case 6: // needs a quarter turn clockwise
				sx, sy = y, h-1-x
			case 7: // mirrored, turned right
				sx, sy = w-1-y, h-1-x
			case 8: // needs a quarter turn counterclockwise
				sx, sy = w-1-y, x
			}
			si := src.PixOffset(sx, sy)
			di := dst.PixOffset(x, y)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}

type encoder struct {
	ext    string
	encode func(*image.RGBA) ([]byte, error)
}

func chooseEncoder(img *image.RGBA) encoder {
	if WebP() {
		return encoder{".webp", encodeWebP}
	}
	if hasAlpha(img) {
		return encoder{".png", func(img *image.RGBA) ([]byte, error) {
			var buf bytes.Buffer
			err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img)
			return buf.Bytes(), err
		}}
	}
	return encoder{".jpg", func(img *image.RGBA) ([]byte, error) {
		var buf bytes.Buffer
		err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality})
		return buf.Bytes(), err
	}}
}

func hasAlpha(img *image.RGBA) bool {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xFF {
			return true
		}
	}
	return false
}

var (
	errNoWebP = errors.New("libwebp is not installed")
	errEncode = errors.New("libwebp could not encode the picture")
)
