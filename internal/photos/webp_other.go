//go:build !linux && !darwin

package photos

import "image"

// WebP reports whether pictures are saved as WebP. Only Linux and macOS can
// load libwebp.
func WebP() bool { return false }

func encodeWebP(*image.RGBA) ([]byte, error) { return nil, errNoWebP }
