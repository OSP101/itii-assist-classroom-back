package utils

import "testing"

func TestTwoFactorKeyTransitionPreservesSecretAndCiphertextFormat(t *testing.T) {
	t.Setenv("TWO_FACTOR_ENCRYPTION_KEY", "")
	t.Setenv("TWO_FACTOR_PREVIOUS_ENCRYPTION_KEY", "")
	old, err := encryptTwoFactorWithKey("JBSWY3DPEHPK3PXP", primaryTwoFactorKey())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TWO_FACTOR_ENCRYPTION_KEY", "12345678901234567890123456789012")
	t.Setenv("TWO_FACTOR_PREVIOUS_ENCRYPTION_KEY", "00000000000000000000000000000000")
	plain, err := DecryptTwoFactorSecret(old)
	if err != nil || plain != "JBSWY3DPEHPK3PXP" {
		t.Fatal("legacy TOTP secret was lost")
	}
	wrapped, changed, err := RewrapTwoFactorSecret(old)
	if err != nil || !changed || wrapped == old {
		t.Fatal("legacy ciphertext did not rotate")
	}
	t.Setenv("TWO_FACTOR_PREVIOUS_ENCRYPTION_KEY", "")
	plain, err = DecryptTwoFactorSecret(wrapped)
	if err != nil || plain != "JBSWY3DPEHPK3PXP" {
		t.Fatal("rotated TOTP requires the previous key")
	}
	// The previous implementation used the same raw AES-GCM format, so an
	// old image configured with the new primary key can still decrypt it.
	if plain, err := decryptTwoFactorWithKey(wrapped, primaryTwoFactorKey()); err != nil || plain != "JBSWY3DPEHPK3PXP" {
		t.Fatal("ciphertext format changed")
	}
	if same, changed, err := RewrapTwoFactorSecret(wrapped); err != nil || changed || same != wrapped {
		t.Fatal("rotation is not idempotent")
	}
	if _, err := DecryptTwoFactorSecret(old); err == nil {
		t.Fatal("old key was accepted after fallback removal")
	}
}

func TestTwoFactorRotationRejectsInvalidConfigurationAndCiphertext(t *testing.T) {
	for _, key := range []string{"", "short", "00000000000000000000000000000000"} {
		t.Setenv("TWO_FACTOR_ENCRYPTION_KEY", key)
		if ValidateTwoFactorRotationKey() == nil {
			t.Fatal("unsafe rotation key accepted")
		}
		if _, err := EncryptTwoFactorSecret("test-secret"); err == nil {
			t.Fatal("new TOTP encrypted without a configured strong key")
		}
	}
	t.Setenv("TWO_FACTOR_ENCRYPTION_KEY", "12345678901234567890123456789012")
	t.Setenv("TWO_FACTOR_PREVIOUS_ENCRYPTION_KEY", "bad-key")
	if ValidateTwoFactorRotationKey() == nil {
		t.Fatal("invalid previous key accepted")
	}
	t.Setenv("TWO_FACTOR_PREVIOUS_ENCRYPTION_KEY", "")
	if err := ValidateTwoFactorRotationKey(); err != nil {
		t.Fatal(err)
	}
	for _, ciphertext := range []string{"not-base64", "AA=="} {
		if _, _, err := RewrapTwoFactorSecret(ciphertext); err == nil {
			t.Fatal("invalid ciphertext accepted")
		}
	}
}
