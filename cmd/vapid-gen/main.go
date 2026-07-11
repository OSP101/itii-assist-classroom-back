// Generates a fresh VAPID key pair for self-hosted Web Push.
// Usage: go run ./cmd/vapid-gen
//
// Copy the printed lines into your .env file, then restart the API server.
// Regenerating invalidates every existing browser push subscription — do not
// re-run in production unless you are ready for all workers/students to
// re-subscribe on their next visit.
package main

import (
	"fmt"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func main() {
	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		fmt.Println("❌ Failed to generate VAPID keys:", err)
		return
	}

	fmt.Println("# Paste the following into itii-assist-classroom-back/.env")
	fmt.Println("VAPID_PUBLIC_KEY=" + publicKey)
	fmt.Println("VAPID_PRIVATE_KEY=" + privateKey)
	fmt.Println("VAPID_SUBJECT=mailto:admin@kku.ac.th")
}
