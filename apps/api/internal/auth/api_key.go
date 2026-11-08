package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"github.com/sailboxhq/sailbox/apps/api/internal/model"
	"strings"
)

const APIKeyPrefix = "sbk_"

// APIKeyAuthenticator resolves a key to its current user, not stale role claims.
type APIKeyAuthenticator interface {
	AuthenticateAPIKey(context.Context, string) (*model.User, error)
}

func GenerateAPIKey() (string, string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", "", err
	}
	key := APIKeyPrefix + base64.RawURLEncoding.EncodeToString(secret)
	return key, HashAPIKey(key), nil
}
func HashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
func IsAPIKey(key string) bool {
	if !strings.HasPrefix(key, APIKeyPrefix) || len(key) != len(APIKeyPrefix)+43 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(key, APIKeyPrefix))
	return err == nil && len(raw) == 32 && base64.RawURLEncoding.EncodeToString(raw) == strings.TrimPrefix(key, APIKeyPrefix)
}
