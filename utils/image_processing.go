package utils

import (
	"bytes"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"

	"github.com/disintegration/imaging"
)

// ProcessUploadedImage downscales an uploaded image to fit within
// maxWidth x maxHeight (aspect-ratio preserved, never upscaled) and
// re-encodes it as JPEG so stored files — and every response that embeds
// their URL — stay small. If the bytes can't be decoded as a raster image
// (animated GIF, WebP, corrupt upload), it returns ok=false and the caller
// should fall back to storing the original bytes untouched.
func ProcessUploadedImage(content []byte, maxWidth, maxHeight, quality int) (processed []byte, ok bool) {
	img, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, false
	}

	bounds := img.Bounds()
	if bounds.Dx() > maxWidth || bounds.Dy() > maxHeight {
		img = imaging.Fit(img, maxWidth, maxHeight, imaging.Lanczos)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, false
	}

	return buf.Bytes(), true
}
