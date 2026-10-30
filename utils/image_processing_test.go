package utils

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"testing"
)

func TestProcessUploadedImagePreservesNormalDimensions(t *testing.T) {
	for _, size := range []image.Point{{X: 80, Y: 40}, {X: 8, Y: 4}} {
		var input bytes.Buffer
		if err := png.Encode(&input, image.NewRGBA(image.Rect(0, 0, size.X, size.Y))); err != nil {
			t.Fatal(err)
		}
		output, ok, err := ProcessUploadedImage(input.Bytes(), 20, 20, 85)
		if err != nil || !ok {
			t.Fatalf("normal upload failed: ok=%v err=%v", ok, err)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(output))
		if err != nil || format != "jpeg" {
			t.Fatalf("expected valid JPEG, got %s: %v", format, err)
		}
		want := size
		if size.X > 20 {
			want = image.Pt(20, 10)
		}
		if cfg.Width != want.X || cfg.Height != want.Y {
			t.Fatalf("got %dx%d, want %v", cfg.Width, cfg.Height, want)
		}
	}
}

// Valid IHDR with no image body: the pixel cap must reject before Decode tries
// to allocate the advertised pixels or encounters the missing body.
func oversizedPNGHeader(width, height uint32) []byte {
	b := make([]byte, 33)
	copy(b, "\x89PNG\r\n\x1a\n")
	binary.BigEndian.PutUint32(b[8:12], 13)
	copy(b[12:16], "IHDR")
	binary.BigEndian.PutUint32(b[16:20], width)
	binary.BigEndian.PutUint32(b[20:24], height)
	b[24], b[25] = 8, 2
	binary.BigEndian.PutUint32(b[29:33], crc32.ChecksumIEEE(b[12:29]))
	return b
}

func TestProcessUploadedImageRejectsOversizedHeader(t *testing.T) {
	for _, size := range [][2]uint32{{100_000, 100_000}, {8_001, 4_000}} {
		output, ok, err := ProcessUploadedImage(oversizedPNGHeader(size[0], size[1]), 512, 512, 85)
		if !errors.Is(err, ErrUploadedImageTooLarge) || ok || output != nil {
			t.Fatalf("must reject %v without fallback: ok=%v err=%v", size, ok, err)
		}
	}
}

func TestProcessUploadedImageKeepsUnsupportedFallback(t *testing.T) {
	for _, content := range [][]byte{nil, []byte("not an image"), []byte("RIFF\x00\x00\x00\x00WEBP")} {
		output, ok, err := ProcessUploadedImage(content, 512, 512, 85)
		if err != nil || ok || output != nil {
			t.Fatalf("unsupported/corrupt format should keep fallback: ok=%v err=%v", ok, err)
		}
	}
}

func TestUploadedImagePixelBudgetBoundary(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, tc := range []struct {
		width, height int
		reject        bool
	}{
		{8000, 4000, false}, {8001, 4000, true},
		{maxInt, maxInt, true}, {0, 10, true}, {-1, 10, true}, {10, 0, true},
	} {
		err := validateUploadedImageDimensions(tc.width, tc.height)
		if errors.Is(err, ErrUploadedImageTooLarge) != tc.reject {
			t.Fatalf("unexpected decision for %dx%d: %v", tc.width, tc.height, err)
		}
	}
}
