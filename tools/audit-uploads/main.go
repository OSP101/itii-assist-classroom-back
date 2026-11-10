// audit-uploads inspects existing files without moving or deleting evidence.
package main

import (
	"bytes"
	"encoding/json"
	"image"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"itii-assist/utils"
)

func main() {
	root := "uploads"
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	type finding struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}
	findings := []finding{}
	scanned := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		scanned++
		if !info.Mode().IsRegular() {
			findings = append(findings, finding{path, "non-regular file; manual review required"})
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".gif" && ext != ".webp" && ext != ".avif" && ext != ".pdf" {
			findings = append(findings, finding{rel, "unsupported extension"})
			return nil
		}
		if info.Size() > 10*1024*1024 {
			findings = append(findings, finding{rel, "requires size review"})
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if ext == ".pdf" {
			if http.DetectContentType(content) != "application/pdf" {
				findings = append(findings, finding{rel, "invalid PDF signature"})
			}
			return nil
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(content))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > utils.MaxUploadedImagePixels {
			findings = append(findings, finding{rel, "invalid or oversized raster; manual review required"})
		}
		return nil
	})
	if err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"scanned": scanned, "findings": findings, "modified_files": 0})
}
