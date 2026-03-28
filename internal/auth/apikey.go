package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	id "github.com/hermes-notifications/hermes/internal/id/v2"
)

var apiKeyIDGen = id.NewGenerator(id.Config{Prefix: "key", RandBits: 36})

// apiKeyIDLen is the length of the encoded random part of an API key ID.
// 36 bits → 5 bytes → 7 base64url chars. The full key ID is "key_" + 7 chars.
const apiKeyIDLen = 7

// HMACHashAPIKey computes an HMAC-SHA256 hash of the secret using hmacKey.
// Returns the hex-encoded HMAC.
func HMACHashAPIKey(secret, hmacKey string) string {
	mac := hmac.New(sha256.New, []byte(hmacKey))
	mac.Write([]byte(secret))
	return hex.EncodeToString(mac.Sum(nil))
}

// HMACVerifyAPIKey verifies a secret against an HMAC hash using constant-time comparison.
func HMACVerifyAPIKey(secret, hash, hmacKey string) bool {
	expected, err := hex.DecodeString(hash)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(hmacKey))
	mac.Write([]byte(secret))
	return hmac.Equal(mac.Sum(nil), expected)
}

// GenerateAPIKey creates a new API key with the given environment prefix.
// envPrefix is "" for production, "stg" for staging, "dev" for development.
// Returns the full raw key string and the key ID (e.g., "key_a8f3B2").
func GenerateAPIKey(envPrefix string) (raw string, keyID string, err error) {
	keyID = apiKeyIDGen.New()

	secretBytes := make([]byte, 16) // 128 bits
	if _, err := rand.Read(secretBytes); err != nil {
		return "", "", fmt.Errorf("generate secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)

	if envPrefix != "" {
		raw = fmt.Sprintf("hms_%s_%s_%s", envPrefix, keyID, secret)
	} else {
		raw = fmt.Sprintf("hms_%s_%s", keyID, secret)
	}
	return raw, keyID, nil
}

// ParseAPIKey extracts the key ID and secret from a raw API key string.
// Handles formats:
//   - hms_key_<id>_<secret>       (production)
//   - hms_stg_key_<id>_<secret>   (staging)
//   - hms_dev_key_<id>_<secret>   (dev)
func ParseAPIKey(raw string) (keyID string, secret string, err error) {
	if !strings.HasPrefix(raw, "hms_") {
		return "", "", fmt.Errorf("invalid api key format: missing hms_ prefix")
	}

	trimmed := strings.TrimPrefix(raw, "hms_")

	// Remove optional environment prefix (stg_, dev_)
	for _, env := range []string{"stg_", "dev_"} {
		trimmed = strings.TrimPrefix(trimmed, env)
	}

	// Now trimmed should be "key_<id>_<secret>"
	if !strings.HasPrefix(trimmed, "key_") {
		return "", "", fmt.Errorf("invalid api key format: missing key_ prefix")
	}
	trimmed = strings.TrimPrefix(trimmed, "key_")

	// The ID part is a fixed length (apiKeyIDLen chars of base64url).
	// We can't split on "_" because base64url encoding can contain underscores.
	if len(trimmed) < apiKeyIDLen+2 { // +1 for separator, +1 for min secret length
		return "", "", fmt.Errorf("invalid api key format: too short")
	}
	if trimmed[apiKeyIDLen] != '_' {
		return "", "", fmt.Errorf("invalid api key format: expected separator at position %d", apiKeyIDLen)
	}

	idPart := trimmed[:apiKeyIDLen]
	secret = trimmed[apiKeyIDLen+1:]

	keyID = "key_" + idPart
	return keyID, secret, nil
}
