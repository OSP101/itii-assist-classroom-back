package utils

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestNormalizeUploadedImageRejectsActiveContentAndCorruptImages(t *testing.T) {
	for _, content := range [][]byte{[]byte("<!DOCTYPE html><html>active</html>"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), []byte("GIF89a"), nil} {
		if _, err := NormalizeUploadedImage(content, 512, 512); err == nil {
			t.Fatalf("accepted invalid/active upload %q", content)
		}
	}
}

func TestNormalizeUploadedImagePreservesPixelsAndStripsTrailingContent(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var source bytes.Buffer
	if err := png.Encode(&source, img); err != nil {
		t.Fatal(err)
	}
	content := append(source.Bytes(), []byte("<html>trailing active content</html>")...)
	output, err := NormalizeUploadedImage(content, 32, 32)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output, []byte("<html>")) {
		t.Fatal("retained polyglot tail")
	}
	decoded, err := jpeg.Decode(bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 32 || decoded.Bounds().Dy() != 16 {
		t.Fatalf("bad resized dimensions: %v", decoded.Bounds())
	}
}

func TestNormalizeUploadedImageRejectsPixelBombBeforeDecode(t *testing.T) {
	var source bytes.Buffer
	if err := png.Encode(&source, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	content := source.Bytes()
	binary.BigEndian.PutUint32(content[16:20], 100000)
	binary.BigEndian.PutUint32(content[20:24], 100000)
	binary.BigEndian.PutUint32(content[29:33], crc32.ChecksumIEEE(content[12:29]))
	if _, err := NormalizeUploadedImage(content, 512, 512); err == nil {
		t.Fatal("accepted decompression bomb")
	}
}
