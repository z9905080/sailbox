package pg

import (
	"context"
	"database/sql"
	"github.com/google/uuid"
	"github.com/sailboxhq/sailbox/apps/api/internal/model"
	"github.com/uptrace/bun"
	"time"
)

type apiKeyStore struct{ db bun.IDB }

func (s *apiKeyStore) Create(ctx context.Context, key *model.APIKey) error {
	_, err := s.db.NewInsert().Model(key).Returning("*").Exec(ctx)
	return err
}
func (s *apiKeyStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]model.APIKey, error) {
	keys := []model.APIKey{}
	err := s.db.NewSelect().Model(&keys).Where("user_id = ?", userID).OrderExpr("created_at DESC").Scan(ctx)
	return keys, err
}
func (s *apiKeyStore) GetByHash(ctx context.Context, hash string) (*model.APIKey, error) {
	key := new(model.APIKey)
	err := s.db.NewSelect().Model(key).Where("key_hash = ?", hash).Scan(ctx)
	return key, err
}
func (s *apiKeyStore) UpdateMetadata(ctx context.Context, id, userID uuid.UUID, name string, expiresAt *time.Time) error {
	result, err := s.db.NewUpdate().Model((*model.APIKey)(nil)).
		Set("name = ?", name).Set("expires_at = ?", expiresAt).
		Where("id = ? AND user_id = ? AND is_active = TRUE", id, userID).Exec(ctx)
	return keyUpdateResult(result, err)
}
func (s *apiKeyStore) Revoke(ctx context.Context, id, userID uuid.UUID) error {
	result, err := s.db.NewUpdate().Model((*model.APIKey)(nil)).Set("is_active = FALSE").
		Where("id = ? AND user_id = ?", id, userID).Exec(ctx)
	return keyUpdateResult(result, err)
}

// Throttle activity writes to once per minute. Authentication itself is never cached.
func (s *apiKeyStore) Touch(ctx context.Context, id uuid.UUID, now time.Time) error {
	_, err := s.db.NewUpdate().Model((*model.APIKey)(nil)).Set("last_used_at = ?", now).
		Where("id = ?", id).Where("last_used_at IS NULL OR last_used_at < ?", now.Add(-time.Minute)).Exec(ctx)
	return err
}
func keyUpdateResult(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return sql.ErrNoRows
	}
	return err
}
