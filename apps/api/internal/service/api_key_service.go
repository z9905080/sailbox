package service

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"github.com/sailboxhq/sailbox/apps/api/internal/apierr"
	"github.com/sailboxhq/sailbox/apps/api/internal/auth"
	"github.com/sailboxhq/sailbox/apps/api/internal/model"
	"github.com/sailboxhq/sailbox/apps/api/internal/store"
	"strings"
	"time"
	"unicode/utf8"
)

type APIKeyService struct{ store store.Store }

func NewAPIKeyService(s store.Store) *APIKeyService { return &APIKeyService{store: s} }

func (s *APIKeyService) AuthenticateAPIKey(ctx context.Context, plaintext string) (*model.User, error) {
	if !auth.IsAPIKey(plaintext) {
		return nil, auth.ErrInvalidToken
	}
	key, err := s.store.APIKeys().GetByHash(ctx, auth.HashAPIKey(plaintext))
	if err != nil || !key.IsActive || (key.ExpiresAt != nil && !time.Now().Before(*key.ExpiresAt)) {
		return nil, auth.ErrInvalidToken
	}
	user, err := s.store.Users().GetByID(ctx, key.UserID)
	// Users are deactivated by soft deletion; Bun excludes deleted rows. Binding
	// to token_version also revokes keys after password, 2FA or role changes.
	if err != nil || user.DeletedAt != nil || user.TokenVersion != key.TokenVersion {
		return nil, auth.ErrInvalidToken
	}
	// Activity metadata is best effort and must never make authentication unavailable.
	_ = s.store.APIKeys().Touch(ctx, key.ID, time.Now().UTC())
	return user, nil
}
func validateKeyMetadata(name string, expiresAt *time.Time) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return "", apierr.ErrValidation.WithDetail("name must contain 1–100 characters")
	}
	if expiresAt != nil && !time.Now().Before(*expiresAt) {
		return "", apierr.ErrValidation.WithDetail("expiry must be in the future")
	}
	return name, nil
}
func (s *APIKeyService) Create(ctx context.Context, userID uuid.UUID, name string, expiresAt *time.Time) (*model.APIKey, string, error) {
	name, err := validateKeyMetadata(name, expiresAt)
	if err != nil {
		return nil, "", err
	}
	user, err := s.store.Users().GetByID(ctx, userID)
	if err != nil {
		return nil, "", apierr.ErrUnauthorized
	}
	if user.Role != model.RoleOwner && user.Role != model.RoleAdmin {
		return nil, "", apierr.ErrForbidden
	}
	plaintext, hash, err := auth.GenerateAPIKey()
	if err != nil {
		return nil, "", err
	}
	key := &model.APIKey{UserID: userID, Name: name, KeyHash: hash, TokenVersion: user.TokenVersion, ExpiresAt: expiresAt, IsActive: true}
	if err := s.store.APIKeys().Create(ctx, key); err != nil {
		return nil, "", err
	}
	return key, plaintext, nil
}
func (s *APIKeyService) List(ctx context.Context, userID uuid.UUID) ([]model.APIKey, error) {
	return s.store.APIKeys().ListByUser(ctx, userID)
}
func (s *APIKeyService) Update(ctx context.Context, id, userID uuid.UUID, name string, expiresAt *time.Time) error {
	name, err := validateKeyMetadata(name, expiresAt)
	if err != nil {
		return err
	}
	return keyNotFound(s.store.APIKeys().UpdateMetadata(ctx, id, userID, name, expiresAt))
}
func (s *APIKeyService) Revoke(ctx context.Context, id, userID uuid.UUID) error {
	return keyNotFound(s.store.APIKeys().Revoke(ctx, id, userID))
}
func keyNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return apierr.ErrNotFound.WithDetail("API key not found")
	}
	return err
}
