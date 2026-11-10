package utils

import (
	"bytes"
	"errors"
	"image"
	"io"
	"mime/multipart"

	_ "golang.org/x/image/webp"
)

const MaxUploadedImagePixels = 25_000_000

// NormalizeUploadedImage trusts decoded pixels, not the client MIME or suffix.
// Re-encoding removes active content and metadata/polyglot tails.
func NormalizeUploadedImage(content []byte, width, height int) ([]byte, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || (format != "jpeg" && format != "png" && format != "gif" && format != "webp") || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxUploadedImagePixels {
		return nil, errors.New("รองรับรูป JPG, PNG, GIF หรือ WebP ที่ถูกต้องและไม่เกิน 25 ล้านพิกเซล")
	}
	processed, ok, err := ProcessUploadedImage(content, width, height, 85)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("ไม่สามารถอ่านไฟล์รูปภาพได้")
	}
	return processed, nil
}

func ReadUploadedImage(file *multipart.FileHeader, maxBytes int64, width, height int) ([]byte, error) {
	if file == nil || file.Size <= 0 || file.Size > maxBytes {
		return nil, errors.New("ขนาดไฟล์รูปภาพไม่ถูกต้อง")
	}
	f, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	content, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maxBytes {
		return nil, errors.New("ไฟล์รูปภาพใหญ่เกินกำหนด")
	}
	return NormalizeUploadedImage(content, width, height)
}
