package utils

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"

	"github.com/disintegration/imaging"
)

// Allow common high-resolution phone photos, while bounding decoded pixel memory.
const maxUploadedImagePixels int64 = 32_000_000

var ErrUploadedImageTooLarge = errors.New("รูปภาพมีความละเอียดสูงเกินไป รองรับสูงสุด 32 ล้านพิกเซล")

// Bound simultaneous decoded buffers inside the 1.5GB backend container.
var uploadedImageDecodeSlots = make(chan struct{}, 2)

func validateUploadedImageDimensions(width, height int) error {
	// Division avoids multiplication overflow, including on 32-bit builds.
	if width <= 0 || height <= 0 || int64(width) > maxUploadedImagePixels/int64(height) {
		return ErrUploadedImageTooLarge
	}
	return nil
}

// ProcessUploadedImage downscales an uploaded image to fit within
// maxWidth x maxHeight (aspect-ratio preserved, never upscaled) and
// re-encodes it as JPEG so stored files — and every response that embeds
// their URL — stay small. If the bytes can't be decoded as a raster image
// (unsupported format or corrupt upload), it returns ok=false and the caller
// can preserve the existing original-file fallback. A size error must instead
// reject the upload: falling back would expose the oversized original again.
func ProcessUploadedImage(content []byte, maxWidth, maxHeight, quality int) (processed []byte, ok bool, err error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return nil, false, nil
	}
	if err := validateUploadedImageDimensions(cfg.Width, cfg.Height); err != nil {
		return nil, false, err
	}
	uploadedImageDecodeSlots <- struct{}{}
	defer func() { <-uploadedImageDecodeSlots }()
	img, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, false, nil
	}

	bounds := img.Bounds()
	if bounds.Dx() > maxWidth || bounds.Dy() > maxHeight {
		img = imaging.Fit(img, maxWidth, maxHeight, imaging.Lanczos)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, false, nil
	}

	return buf.Bytes(), true, nil
}
