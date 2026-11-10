package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
)

// Preserve the existing ciphertext format and legacy key normalization so a
// deployment can read existing TOTP secrets before the explicit rotation step.
func twoFactorKey(raw string) []byte {
	key := make([]byte, 32)
	copy(key, []byte(raw))
	return key
}

func primaryTwoFactorKey() []byte {
	raw := os.Getenv("TWO_FACTOR_ENCRYPTION_KEY")
	if raw == "" {
		raw = "00000000000000000000000000000000"
	}
	return twoFactorKey(raw)
}

func ValidateTwoFactorRotationKey() error {
	raw := os.Getenv("TWO_FACTOR_ENCRYPTION_KEY")
	if len([]byte(raw)) != 32 || raw == "00000000000000000000000000000000" {
		return errors.New("rotation requires an explicit non-default 32-byte TWO_FACTOR_ENCRYPTION_KEY")
	}
	previous := os.Getenv("TWO_FACTOR_PREVIOUS_ENCRYPTION_KEY")
	if previous != "" && len([]byte(previous)) != 32 {
		return errors.New("previous encryption key must be exactly 32 bytes")
	}
	return nil
}

func EncryptTwoFactorSecret(plaintext string) (string, error) {
	if err := ValidateTwoFactorRotationKey(); err != nil {
		return "", err
	}
	return encryptTwoFactorWithKey(plaintext, primaryTwoFactorKey())
}

func encryptTwoFactorWithKey(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plaintext), nil)), nil
}

func decryptTwoFactorWithKey(ciphertext string, key []byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func DecryptTwoFactorSecret(ciphertext string) (string, error) {
	plain, err := decryptTwoFactorWithKey(ciphertext, primaryTwoFactorKey())
	if err == nil {
		return plain, nil
	}
	if previous := os.Getenv("TWO_FACTOR_PREVIOUS_ENCRYPTION_KEY"); previous != "" {
		return decryptTwoFactorWithKey(ciphertext, twoFactorKey(previous))
	}
	return "", err
}

// Rewrap returns unchanged ciphertext when it already uses the primary key.
// A previous-key secret is re-encrypted, never returned as plaintext to a tool.
func RewrapTwoFactorSecret(ciphertext string) (string, bool, error) {
	if _, err := decryptTwoFactorWithKey(ciphertext, primaryTwoFactorKey()); err == nil {
		return ciphertext, false, nil
	}
	plain, err := DecryptTwoFactorSecret(ciphertext)
	if err != nil {
		return "", false, err
	}
	wrapped, err := EncryptTwoFactorSecret(plain)
	return wrapped, true, err
}
