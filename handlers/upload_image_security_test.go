package handlers

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"itii-assist/utils"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestSmallImageUploadsSaveCourseCoverScoreAndLeaveEvidence(t *testing.T) {
	t.Chdir(t.TempDir())
	var source bytes.Buffer
	if err := png.Encode(&source, image.NewRGBA(image.Rect(0, 0, 32, 16))); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"image", "images", "evidence"} {
		t.Run(field, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile(field, "small.png")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = part.Write(source.Bytes())
			_ = writer.Close()
			app := fiber.New()
			app.Post("/upload", func(c fiber.Ctx) error {
				switch field {
				case "image":
					return UploadCourseCoverHandler(c)
				case "images":
					paths, err := saveScoreEditRequestImages(c)
					if err != nil {
						return err
					}
					if len(paths) != 1 {
						t.Fatalf("expected saved score evidence, got %v", paths)
					}
				case "evidence":
					form, err := c.MultipartForm()
					if err != nil {
						return err
					}
					paths, err := saveLeaveEvidenceFiles("test-course", form.File[field])
					if err != nil {
						return err
					}
					if len(paths) != 1 {
						t.Fatalf("expected saved leave evidence, got %v", paths)
					}
				}
				return c.SendStatus(200)
			})
			req := httptest.NewRequest("POST", "/upload", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			response, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			result, _ := io.ReadAll(response.Body)
			if response.StatusCode != 200 {
				t.Fatalf("small upload failed: %d %s", response.StatusCode, result)
			}
		})
	}
	files := 0
	err := filepath.WalkDir("uploads", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if _, format, err := image.DecodeConfig(bytes.NewReader(content)); err != nil || format != "jpeg" {
				t.Fatalf("invalid saved image %s: format=%s err=%v", path, format, err)
			}
			files++
		}
		return nil
	})
	if err != nil || files != 3 {
		t.Fatalf("expected 3 saved JPEGs: files=%d err=%v", files, err)
	}
}

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
	if response.StatusCode != 400 || !strings.Contains(string(result), "25") {
		t.Fatalf("must reject pixel limit, got %d %s", response.StatusCode, result)
	}
}

func TestUploadsTenMiBBoundaryAndThreeFileBundles(t *testing.T) {
	t.Chdir(t.TempDir())
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"cover", "announcement", "score", "leave"} {
		for _, over := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/over=%v", kind, over), func(t *testing.T) {
				fileSize := utils.MaxUploadedImageBytes
				if over {
					fileSize++
				}
				payload := make([]byte, fileSize)
				copy(payload, pngBytes.Bytes())
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				field, count := "image", 1
				if kind == "score" {
					field, count = "images", 3
				}
				if kind == "leave" {
					field, count = "evidence", 3
				}
				for i := 0; i < count; i++ {
					part, err := writer.CreateFormFile(field, "boundary.png")
					if err != nil {
						t.Fatal(err)
					}
					if _, err := part.Write(payload); err != nil {
						t.Fatal(err)
					}
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				app := fiber.New(fiber.Config{BodyLimit: utils.MaxUploadRequestBytes})
				app.Post("/upload", func(c fiber.Ctx) error {
					if kind == "cover" {
						return UploadCourseCoverHandler(c)
					}
					if kind == "announcement" {
						return UploadAnnouncementImageHandler(c)
					}
					var paths []string
					var err error
					if kind == "score" {
						paths, err = saveScoreEditRequestImages(c)
					} else {
						var form *multipart.Form
						form, err = c.MultipartForm()
						if err == nil {
							paths, err = saveLeaveEvidenceFiles("boundary-course", form.File[field])
						}
					}
					if err != nil {
						return c.Status(400).SendString(err.Error())
					}
					if len(paths) != count {
						return c.Status(500).SendString("missing saved evidence")
					}
					return c.SendStatus(200)
				})
				req := httptest.NewRequest("POST", "/upload", &body)
				req.Header.Set("Content-Type", writer.FormDataContentType())
				response, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				result, _ := io.ReadAll(response.Body)
				want := 200
				if over {
					want = 400
				}
				if response.StatusCode != want {
					t.Fatalf("want %d got %d: %s", want, response.StatusCode, result)
				}
				if over && !strings.Contains(string(result), "10") {
					t.Fatalf("missing size limit: %s", result)
				}
			})
		}
	}
}
