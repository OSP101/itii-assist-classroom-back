package repositories

import (
	"context"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"itii-assist/config"
	"itii-assist/utils"
	"testing"
)

func rotationFixture(t *testing.T) (string, func()) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	previousDB := config.DB
	config.DB = db
	t.Cleanup(func() { config.DB = previousDB; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	for _, sql := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, two_factor_method TEXT, two_factor_secret TEXT, updated_at datetime, deleted_at datetime)`,
		`CREATE TABLE two_factor_pendings (id INTEGER PRIMARY KEY, method TEXT, secret TEXT)`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TWO_FACTOR_ENCRYPTION_KEY", "")
	t.Setenv("TWO_FACTOR_PREVIOUS_ENCRYPTION_KEY", "")
	// Ciphertext produced by the previous AES-GCM implementation with its
	// default key; this is a public test secret, not production data.
	old := "AQEBAQEBAQEBAQEB6Ru5dwwYqCE6y8TO0yogOmdpOSYrRAQ3EUqXGrU32Co="
	if err := db.Exec(`INSERT INTO users VALUES (1, 'totp', ?, NULL, '2026-01-01')`, old).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO two_factor_pendings VALUES (1, 'totp', ?), (2, 'email', 'login:fixture')`, old).Error; err != nil {
		t.Fatal(err)
	}
	t.Setenv("TWO_FACTOR_ENCRYPTION_KEY", "12345678901234567890123456789012")
	t.Setenv("TWO_FACTOR_PREVIOUS_ENCRYPTION_KEY", "00000000000000000000000000000000")
	return old, func() { t.Setenv("TWO_FACTOR_PREVIOUS_ENCRYPTION_KEY", "") }
}

func TestTwoFactorRotationDryRunApplyAndIdempotence(t *testing.T) {
	old, removePrevious := rotationFixture(t)
	result, err := RotateTwoFactorEncryptionKey(context.Background(), true)
	if err != nil || result.Scanned != 2 || result.NeedsRotation != 2 || result.Rotated != 0 {
		t.Fatalf("dry run failed: %+v %v", result, err)
	}
	var ciphertext string
	config.DB.Raw(`SELECT two_factor_secret FROM users WHERE id=1`).Scan(&ciphertext)
	if ciphertext != old {
		t.Fatal("dry run changed ciphertext")
	}
	result, err = RotateTwoFactorEncryptionKey(context.Background(), false)
	if err != nil || result.Rotated != 2 {
		t.Fatalf("rotation failed: %+v %v", result, err)
	}
	removePrevious()
	config.DB.Raw(`SELECT two_factor_secret FROM users WHERE id=1`).Scan(&ciphertext)
	if plain, err := utils.DecryptTwoFactorSecret(ciphertext); err != nil || plain != "JBSWY3DPEHPK3PXP" {
		t.Fatal("rotation lost the user's TOTP")
	}
	config.DB.Raw(`SELECT secret FROM two_factor_pendings WHERE id=1`).Scan(&ciphertext)
	if plain, err := utils.DecryptTwoFactorSecret(ciphertext); err != nil || plain != "JBSWY3DPEHPK3PXP" {
		t.Fatal("rotation lost the pending setup")
	}
	config.DB.Raw(`SELECT secret FROM two_factor_pendings WHERE id=2`).Scan(&ciphertext)
	if ciphertext != "login:fixture" {
		t.Fatal("email challenge was changed")
	}
	result, err = RotateTwoFactorEncryptionKey(context.Background(), false)
	if err != nil || result.NeedsRotation != 0 || result.Rotated != 0 {
		t.Fatal("repeat rotation changed data")
	}
}

func TestTwoFactorRotationIsAtomicWhenAnySecretIsInvalid(t *testing.T) {
	old, _ := rotationFixture(t)
	config.DB.Exec(`INSERT INTO users VALUES (2, 'totp', 'invalid-ciphertext', NULL, NULL)`)
	result, err := RotateTwoFactorEncryptionKey(context.Background(), false)
	if err == nil || result.Rotated != 0 {
		t.Fatal("invalid data did not abort the transaction")
	}
	var ciphertext string
	config.DB.Raw(`SELECT two_factor_secret FROM users WHERE id=1`).Scan(&ciphertext)
	if ciphertext != old {
		t.Fatal("failed rotation partially changed user secrets")
	}
}
