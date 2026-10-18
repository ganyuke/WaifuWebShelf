//go:build linux || darwin

package photos

import (
	"image"
	"image/draw"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// libwebp is opened at runtime, so the app builds and runs without it and
// only uses it when the system has it.
var (
	webpOnce   sync.Once
	webpOK     bool
	webpEncode func(rgba *byte, width, height, stride int32, quality float32, out **byte) uintptr
	webpFree   func(p unsafe.Pointer)
)

var webpLibs = map[string][]string{
	"linux":  {"libwebp.so.7", "libwebp.so"},
	"darwin": {"libwebp.7.dylib", "libwebp.dylib", "/opt/homebrew/lib/libwebp.dylib", "/usr/local/lib/libwebp.dylib"},
}

// WebP reports whether pictures are saved as WebP.
func WebP() bool {
	webpOnce.Do(func() {
		for _, name := range webpLibs[runtime.GOOS] {
			lib, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_LOCAL)
			if err != nil {
				continue
			}
			enc, err1 := purego.Dlsym(lib, "WebPEncodeRGBA")
			free, err2 := purego.Dlsym(lib, "WebPFree")
			if err1 != nil || err2 != nil {
				continue
			}
			purego.RegisterFunc(&webpEncode, enc)
			purego.RegisterFunc(&webpFree, free)
			webpOK = true
			return
		}
	})
	return webpOK
}

func encodeWebP(img *image.RGBA) ([]byte, error) {
	if !WebP() {
		return nil, errNoWebP
	}
	// libwebp takes straight alpha, Go's RGBA is premultiplied.
	n := image.NewNRGBA(img.Bounds())
	draw.Draw(n, n.Bounds(), img, img.Bounds().Min, draw.Src)
	if len(n.Pix) == 0 {
		return nil, errNoWebP
	}
	var out *byte
	size := webpEncode(&n.Pix[0], int32(n.Rect.Dx()), int32(n.Rect.Dy()), int32(n.Stride), webpQuality, &out)
	runtime.KeepAlive(n)
	if size == 0 || out == nil {
		return nil, errEncode
	}
	defer webpFree(unsafe.Pointer(out))
	return append([]byte(nil), unsafe.Slice(out, size)...), nil
}
