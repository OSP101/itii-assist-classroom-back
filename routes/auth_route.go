package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"
	"itii-assist/services"

	"github.com/gofiber/fiber/v3"
)

func SetupAuthRoutes(app *fiber.App, auditLogger *services.AuditLogger) {
	authHandler := handlers.NewAuthHandler(auditLogger)
	api := app.Group("/api/auth")

	// Public
	api.Post("/login", authHandler.Login)
	api.Post("/refresh", handlers.RefreshHandler)
	api.Post("/logout", authHandler.Logout)
	api.Post("/forgot-password", handlers.ForgotPasswordHandler)
	api.Post("/validate-reset-token", handlers.ValidateResetTokenHandler)
	api.Post("/reset-password", handlers.ResetPasswordHandler)

	// Protected
	api.Get("/me", middlewares.Protected(), handlers.GetMeHandler)
	api.Post("/change-password", middlewares.Protected(), handlers.ChangePasswordHandler)
	api.Post("/force-change-password", middlewares.Protected(), handlers.ForceChangePasswordHandler)
	api.Put("/profile", middlewares.Protected(), handlers.UpdateProfileHandler)
	api.Put("/preferences", middlewares.Protected(), handlers.UpdatePreferencesHandler)
	api.Post("/avatar", middlewares.Protected(), handlers.UploadAvatarHandler)
	api.Delete("/avatar", middlewares.Protected(), handlers.RemoveAvatarHandler)

	// Sessions
	api.Get("/sessions", middlewares.Protected(), handlers.GetSessionsHandler)
	api.Delete("/sessions/:sessionId", middlewares.Protected(), authHandler.RevokeSession)
	api.Post("/sessions/revoke-all", middlewares.Protected(), handlers.RevokeAllSessionsHandler)

	// Google OAuth
	api.Get("/google", handlers.GoogleLoginHandler)
	api.Get("/google/callback", handlers.GoogleCallbackHandler)
	api.Get("/github", handlers.GitHubLoginHandler)
	api.Get("/github/callback", handlers.GitHubCallbackHandler)

	// KKU SSO
	api.Get("/kku", handlers.KKULoginHandler)
	api.Get("/kku/callback", handlers.KKUCallbackHandler)

	// 2FA — public endpoints (login flow)
	twofa := app.Group("/api/auth/2fa")
	twofa.Post("/verify-login", handlers.VerifyLoginWith2FAHandler)
	twofa.Post("/send-login-code", handlers.SendLoginCodeHandler)
	twofa.Post("/complete-login", handlers.CompleteTwoFALoginHandler)
	twofa.Post("/step-up/challenge", middlewares.Protected(), handlers.RequestStepUpChallengeHandler)
	twofa.Post("/step-up/verify", middlewares.Protected(), handlers.VerifyStepUpChallengeHandler)

	// 2FA — protected endpoints (setup/manage)
	twofa.Get("/status", middlewares.Protected(), handlers.Get2FAStatusHandler)
	twofa.Post("/setup/totp", middlewares.Protected(), handlers.Setup2FATOTPHandler)
	twofa.Post("/setup/email", middlewares.Protected(), handlers.Setup2FAEmailHandler)
	twofa.Post("/verify", middlewares.Protected(), handlers.Verify2FAHandler)
	twofa.Post("/resend-email", middlewares.Protected(), handlers.Resend2FAEmailHandler)
	twofa.Post("/disable", middlewares.Protected(), handlers.Disable2FAHandler)
	twofa.Post("/backup-codes", middlewares.Protected(), handlers.RegenerateBackupCodesHandler)
}
