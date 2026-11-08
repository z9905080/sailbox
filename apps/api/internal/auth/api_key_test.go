package auth

import (
	"encoding/json"
	"github.com/sailboxhq/sailbox/apps/api/internal/model"
	"strings"
	"testing"
)

func TestAPIKeyGeneration(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		key, hash, err := GenerateAPIKey()
		if err != nil {
			t.Fatal(err)
		}
		if !IsAPIKey(key) || len(hash) != 64 || HashAPIKey(key) != hash || key == hash || seen[key] {
			t.Fatal("invalid or duplicate generated key")
		}
		seen[key] = true
		data, err := json.Marshal(model.APIKey{KeyHash: hash, TokenVersion: 7})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), hash) || strings.Contains(string(data), "key_hash") || strings.Contains(string(data), "token_version") {
			t.Fatal("private fields leaked in JSON")
		}
	}
	for _, key := range []string{"", "jwt.token.here", APIKeyPrefix, APIKeyPrefix + strings.Repeat("!", 43), " " + APIKeyPrefix + strings.Repeat("a", 43)} {
		if IsAPIKey(key) {
			t.Fatal("accepted malformed key")
		}
	}
}
