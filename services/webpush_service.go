package services

import (
	"encoding/json"
	"itii-assist/repositories"
	"log/slog"
	"net/http"
	"os"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// webPushPayload is the JSON body delivered to the service worker's `push`
// event. It intentionally mirrors the shape expected by the Notification API
// so the service worker can pass it almost directly to showNotification().
type webPushPayload struct {
	Title              string            `json:"title"`
	Body               string            `json:"body"`
	Icon               string            `json:"icon,omitempty"`
	Badge              string            `json:"badge,omitempty"`
	Tag                string            `json:"tag,omitempty"`
	RequireInteraction bool              `json:"requireInteraction"`
	Vibrate            []int             `json:"vibrate,omitempty"`
	Data               map[string]string `json:"data,omitempty"`
}

// DefaultWorkerVibrationPattern is used for new-task alerts so a worker feels
// the phone buzz even when the screen is off / another app is in front.
var DefaultWorkerVibrationPattern = []int{200, 100, 200, 100, 400}

func getVAPIDKeys() (publicKey, privateKey, subject string) {
	publicKey = strings.TrimSpace(os.Getenv("VAPID_PUBLIC_KEY"))
	privateKey = strings.TrimSpace(os.Getenv("VAPID_PRIVATE_KEY"))
	subject = strings.TrimSpace(os.Getenv("VAPID_SUBJECT"))
	if subject == "" {
		subject = "admin@example.com"
	}
	// webpush-go's getVAPIDAuthorizationHeader prepends "mailto:" itself
	// whenever the subscriber string doesn't already start with "https:" - it
	// does NOT check for an existing "mailto:" prefix. VAPID_SUBJECT is
	// conventionally written as "mailto:someone@example.com" (that's the
	// literal value the docs/.env examples for this env var use), so passing
	// it straight through produced a VAPID JWT "sub" claim of
	// "mailto:mailto:someone@example.com" - not a syntactically valid mailto
	// URI. Apple's web push relay validates that claim strictly and rejected
	// every single push with 403, silently; other push services (FCM,
	// Mozilla) were far more lenient about it, which is why this only ever
	// showed up as "iOS never gets anything."
	if withoutScheme, ok := strings.CutPrefix(strings.ToLower(subject), "mailto:"); ok {
		subject = subject[len(subject)-len(withoutScheme):]
	}
	return
}

// GetVAPIDPublicKey exposes the public key so the frontend can call
// PushManager.subscribe({ applicationServerKey: publicKey }).
func GetVAPIDPublicKey() string {
	publicKey, _, _ := getVAPIDKeys()
	return publicKey
}

// WebPushDeliveryResult reports how many of a user's subscriptions the push
// service actually accepted. Callers (e.g. the test-push endpoint) use it to
// tell the TA "we tried N devices, N-1 succeeded, one is stale" — otherwise
// TAs have no way to tell that their old subscription is silently 410 Gone.
type WebPushDeliveryResult struct {
	Attempted int
	Delivered int
	Stale     int
}

// SendWebPushToUser delivers a Web Push notification (via webpush-go, VAPID —
// no external push provider account needed) to every active subscription a
// user has registered, e.g. across multiple devices/browsers.
func SendWebPushToUser(userID uint, title, body string, data map[string]string, requireInteraction bool) WebPushDeliveryResult {
	var result WebPushDeliveryResult

	publicKey, privateKey, subject := getVAPIDKeys()
	if publicKey == "" || privateKey == "" {
		slog.Warn("webpush: VAPID keys are not configured; skipping web push", "user_id", userID)
		return result
	}

	subs, err := repositories.GetActivePushSubscriptionsByUserID(userID)
	if err != nil {
		slog.Warn("webpush: failed to load push subscriptions", "user_id", userID, "error", err)
		return result
	}
	if len(subs) == 0 {
		return result
	}
	result.Attempted = len(subs)

	payload := webPushPayload{
		Title:              title,
		Body:               body,
		Icon:               "/icons/icon-192.png",
		Badge:              "/icons/badge-96.png",
		Tag:                data["type"],
		RequireInteraction: requireInteraction,
		Vibrate:            DefaultWorkerVibrationPattern,
		Data:               data,
	}
	message, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("webpush: failed to encode payload", "user_id", userID, "error", err)
		return result
	}

	options := &webpush.Options{
		VAPIDPublicKey:  publicKey,
		VAPIDPrivateKey: privateKey,
		Subscriber:      subject,
		TTL:             60,
		Urgency:         webpush.UrgencyHigh,
	}

	for _, sub := range subs {
		subscription := &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys: webpush.Keys{
				P256dh: sub.P256dhKey,
				Auth:   sub.AuthKey,
			},
		}

		resp, sendErr := webpush.SendNotification(message, subscription, options)
		if sendErr != nil {
			slog.Warn("webpush: send failed", "user_id", userID, "endpoint", sub.Endpoint, "error", sendErr)
			continue
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
				// Subscription expired or was revoked by the browser/push service.
				result.Stale++
				if err := repositories.DeactivatePushSubscriptionByEndpoint(sub.Endpoint); err != nil {
					slog.Warn("webpush: failed to deactivate stale subscription", "endpoint", sub.Endpoint, "error", err)
				}
				return
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				slog.Warn("webpush: push service returned error status", "user_id", userID, "endpoint", sub.Endpoint, "status", resp.StatusCode)
				return
			}
			result.Delivered++
		}()
	}
	return result
}
