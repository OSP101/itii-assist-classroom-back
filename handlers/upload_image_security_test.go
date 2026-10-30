package handlers

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestCourseCoverRejectsOversizedImageBeforeSaving(t *testing.T) {
	// Valid PNG dimensions over the pixel budget, without a pixel body.
	header := make([]byte, 33)
	copy(header, "\x89PNG\r\n\x1a\n")
	binary.BigEndian.PutUint32(header[8:12], 13)
	copy(header[12:16], "IHDR")
	binary.BigEndian.PutUint32(header[16:20], 100_000)
	binary.BigEndian.PutUint32(header[20:24], 100_000)
	header[24], header[25] = 8, 2
	binary.BigEndian.PutUint32(header[29:33], crc32.ChecksumIEEE(header[12:29]))
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "oversized.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(header)
	_ = writer.Close()
	// CreateFormFile uses octet-stream; explicitly supply the image header to
	// exercise the real handler's image branch, rather than its MIME rejection.
	content := bytes.Replace(body.Bytes(), []byte("application/octet-stream"), []byte("image/png"), 1)
	t.Chdir(t.TempDir())
	app := fiber.New()
	app.Post("/cover", UploadCourseCoverHandler)
	req := httptest.NewRequest("POST", "/cover", bytes.NewReader(content))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	result, _ := io.ReadAll(response.Body)
	if response.StatusCode != 400 || !strings.Contains(string(result), "32") {
		t.Fatalf("must reject pixel limit, got %d %s", response.StatusCode, result)
	}
}
