package pg

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sailboxhq/sailbox/apps/api/internal/auth"
	"github.com/sailboxhq/sailbox/apps/api/internal/model"
	"github.com/sailboxhq/sailbox/apps/api/migrations"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
)

// Run only against an explicitly supplied disposable LOCAL database. Every
// object is isolated in a fresh schema; no public-schema objects are touched.
func TestAPIKeyStorePostgres(t *testing.T) {
	dsn := os.Getenv("SAILBOX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set SAILBOX_TEST_DATABASE_URL to a disposable local PostgreSQL database")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" && parsed.Hostname() != "::1") || !strings.HasPrefix(strings.TrimPrefix(parsed.Path, "/"), "sailbox_api_keys_test") {
		t.Fatal("integration test requires localhost and a database named sailbox_api_keys_test*")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
	defer root.Close()
	if err := root.PingContext(ctx); err != nil {
		t.Fatal("local PostgreSQL unavailable")
	}
	schema := "api_keys_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := root.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := root.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	db := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn), pgdriver.WithConnParams(map[string]any{"search_path": schema}))), pgdialect.New())
	defer db.Close()
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	var feature *migrate.Migration
	for _, migration := range migrations.Migrations.Sorted() {
		if strings.Contains(migration.Comment, "add_api_keys") {
			copy := migration
			feature = &copy
			continue
		}
		if err := migration.Up(ctx, migrator, &migration); err != nil {
			t.Fatal(err)
		}
	}
	if feature == nil {
		t.Fatal("API key migration not registered")
	}
	for range 2 {
		if err := feature.Up(ctx, migrator, feature); err != nil {
			t.Fatal(err)
		}
	}
	s := &Store{db: db, idb: db}
	orgID, userID, otherID := uuid.New(), uuid.New(), uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO organizations (id,name) VALUES (?,?)`, orgID, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id,org_id,email,password_hash) VALUES (?,?,?,?)`, userID, orgID, "test@example.invalid", "not-a-real-password-hash"); err != nil {
		t.Fatal(err)
	}
	plaintext, hash, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	key := &model.APIKey{UserID: userID, Name: "test", KeyHash: hash, IsActive: true}
	if err := s.APIKeys().Create(ctx, key); err != nil {
		t.Fatal(err)
	}
	got, err := s.APIKeys().GetByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if got.KeyHash != hash || got.KeyHash == plaintext || got.ID == uuid.Nil || got.CreatedAt.IsZero() {
		t.Fatal("incorrect persisted key")
	}
	keys, err := s.APIKeys().ListByUser(ctx, userID)
	if err != nil || len(keys) != 1 {
		t.Fatal("key listing failed")
	}
	if err := s.APIKeys().UpdateMetadata(ctx, key.ID, otherID, "stolen", nil); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross-owner update allowed")
	}
	if err := s.APIKeys().Revoke(ctx, key.ID, otherID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross-owner revoke allowed")
	}
	expiry := time.Now().UTC().Add(time.Hour)
	if err := s.APIKeys().UpdateMetadata(ctx, key.ID, userID, "renamed", &expiry); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := s.APIKeys().Touch(ctx, key.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := s.APIKeys().Touch(ctx, key.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err = s.APIKeys().GetByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "renamed" || got.ExpiresAt == nil || got.LastUsedAt == nil || got.LastUsedAt.Sub(now).Abs() > time.Microsecond {
		t.Fatal("metadata or activity write throttling failed")
	}
	if err := s.APIKeys().Revoke(ctx, key.ID, userID); err != nil {
		t.Fatal(err)
	}
	got, err = s.APIKeys().GetByHash(ctx, hash)
	if err != nil || got.IsActive {
		t.Fatal("revocation not persisted")
	}
	if err := s.APIKeys().UpdateMetadata(ctx, key.ID, userID, "revive", nil); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("revoked key updated")
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", userID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.APIKeys().GetByHash(ctx, hash); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("user deletion did not cascade")
	}
	if err := feature.Down(ctx, migrator, feature); err != nil {
		t.Fatal(err)
	}
	var removed bool
	if err := db.NewRaw("SELECT to_regclass('api_keys') IS NULL").Scan(ctx, &removed); err != nil || !removed {
		t.Fatal("down migration did not remove api_keys")
	}
	if err := feature.Up(ctx, migrator, feature); err != nil {
		t.Fatal(err)
	}
}
