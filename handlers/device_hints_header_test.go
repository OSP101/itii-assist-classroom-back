package handlers

import (
	"io"
	"net/http/httptest"
	"testing"

	"itii-assist/utils"

	"github.com/gofiber/fiber/v3"
)

// Proves the header the frontend sends is the header the guard reads: same
// name, same format, surviving a real request through Fiber's header parsing.
// The two halves are wired by a string constant in two languages, so nothing
// else would catch a rename on one side.
func TestDeviceHintsHeaderRoundTrip(t *testing.T) {
	app := fiber.New()
	app.Get("/probe", func(c fiber.Ctx) error {
		h := utils.ParseDeviceHints(c.Get(utils.DeviceHintsHeader))
		return c.JSON(fiber.Map{"touch": h.Touch, "coarse": h.CoarsePointer, "maxtouch": h.MaxTouchPoints})
	})

	req := httptest.NewRequest("GET", "/probe", nil)
	// Byte-for-byte what buildDeviceHintsHeader() produced in a touch browser.
	req.Header.Set("X-Client-Device-Hints", "touch=1;coarse=1;maxtouch=5")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if got, want := string(body), `{"coarse":true,"maxtouch":5,"touch":true}`; got != want {
		t.Fatalf("guard did not receive the hints the browser sent\n got: %s\nwant: %s", got, want)
	}
}
