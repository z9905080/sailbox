package model

import (
	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"time"
)

// APIKey contains metadata only in JSON. Plaintext keys are never persisted.
type APIKey struct {
	bun.BaseModel `bun:"table:api_keys,alias:ak"`
	ID            uuid.UUID  `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	UserID        uuid.UUID  `bun:"user_id,notnull,type:uuid" json:"user_id"`
	Name          string     `bun:"name,notnull" json:"name"`
	KeyHash       string     `bun:"key_hash,notnull,unique" json:"-"`
	TokenVersion  int        `bun:"token_version,notnull" json:"-"`
	CreatedAt     time.Time  `bun:"created_at,notnull,default:current_timestamp" json:"created_at"`
	ExpiresAt     *time.Time `bun:"expires_at" json:"expires_at"`
	LastUsedAt    *time.Time `bun:"last_used_at" json:"last_used_at"`
	IsActive      bool       `bun:"is_active,notnull" json:"is_active"`
}
