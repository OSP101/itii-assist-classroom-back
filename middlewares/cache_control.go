package middlewares

import "github.com/gofiber/fiber/v3"

// NoStore marks a response as uncacheable by any intermediary (browser, the
// KKU edge, Cloudflare, or any proxy in between). Apply it to public,
// per-session, or identity-bearing endpoints — e.g. the attendance check-in
// and session-info routes — whose bodies must never be served to a different
// user or a later request from a shared cache. It also sends Vary so that if a
// cache is ever placed in front, it keys on the auth-bearing request headers
// instead of collapsing distinct users onto one entry.
func NoStore() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(fiber.HeaderCacheControl, "no-store, no-cache, must-revalidate, max-age=0")
		c.Set(fiber.HeaderPragma, "no-cache")
		c.Set(fiber.HeaderVary, "Cookie, Authorization")
		return c.Next()
	}
}
