package utils

import (
	"crypto/rand"
	"math/big"
)

const nanoAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-"

// GenerateNanoID generates a URL-safe random string of length n (NanoID-compatible).
func GenerateNanoID(n int) (string, error) {
	result := make([]byte, n)
	for i := range result {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(nanoAlphabet))))
		if err != nil {
			return "", err
		}
		result[i] = nanoAlphabet[idx.Int64()]
	}
	return string(result), nil
}
