package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sailboxhq/sailbox/apps/api/internal/api/middleware"
	v1 "github.com/sailboxhq/sailbox/apps/api/internal/api/v1"
	"github.com/sailboxhq/sailbox/apps/api/internal/auth"
	"github.com/sailboxhq/sailbox/apps/api/internal/model"
	"github.com/sailboxhq/sailbox/apps/api/internal/service"
	"github.com/sailboxhq/sailbox/apps/api/internal/store"
)

// Embedded interfaces intentionally panic on unimplemented operations: these
// tests may not silently reach unrelated deployment or infrastructure services.
type memoryStore struct {
	store.Store
	users    *memoryUsers
	keys     *memoryKeys
	projects *memoryProjects
	members  *memoryMembers
}

func (s *memoryStore) Users() store.UserStore                   { return s.users }
func (s *memoryStore) APIKeys() store.APIKeyStore               { return s.keys }
func (s *memoryStore) Projects() store.ProjectStore             { return s.projects }
func (s *memoryStore) ProjectMembers() store.ProjectMemberStore { return s.members }
func (s *memoryStore) ValidateSession(ctx context.Context, id uuid.UUID, version int) bool {
	user, err := s.users.GetByID(ctx, id)
	return err == nil && user.TokenVersion == version
}

type memoryUsers struct {
	store.UserStore
	rows map[uuid.UUID]*model.User
}

func (s *memoryUsers) GetByID(_ context.Context, id uuid.UUID) (*model.User, error) {
	user, ok := s.rows[id]
	if !ok || user.DeletedAt != nil {
		return nil, sql.ErrNoRows
	}
	return user, nil
}

type memoryKeys struct {
	rows map[uuid.UUID]*model.APIKey
	fail bool
}

func (s *memoryKeys) Create(_ context.Context, key *model.APIKey) error {
	key.ID = uuid.New()
	key.CreatedAt = time.Now().UTC()
	stored := *key
	s.rows[key.ID] = &stored
	return nil
}
func (s *memoryKeys) ListByUser(_ context.Context, userID uuid.UUID) ([]model.APIKey, error) {
	keys := []model.APIKey{}
	for _, key := range s.rows {
		if key.UserID == userID {
			keys = append(keys, *key)
		}
	}
	return keys, nil
}
func (s *memoryKeys) GetByHash(_ context.Context, hash string) (*model.APIKey, error) {
	if s.fail {
		return nil, errors.New("database unavailable")
	}
	for _, key := range s.rows {
		if key.KeyHash == hash {
			copy := *key
			return &copy, nil
		}
	}
	return nil, sql.ErrNoRows
}
func (s *memoryKeys) UpdateMetadata(_ context.Context, id, userID uuid.UUID, name string, expiry *time.Time) error {
	key, ok := s.rows[id]
	if !ok || key.UserID != userID || !key.IsActive {
		return sql.ErrNoRows
	}
	key.Name = name
	key.ExpiresAt = expiry
	return nil
}
func (s *memoryKeys) Revoke(_ context.Context, id, userID uuid.UUID) error {
	key, ok := s.rows[id]
	if !ok || key.UserID != userID {
		return sql.ErrNoRows
	}
	key.IsActive = false
	return nil
}
func (s *memoryKeys) Touch(_ context.Context, id uuid.UUID, now time.Time) error {
	s.rows[id].LastUsedAt = &now
	return nil
}

type memoryProjects struct {
	store.ProjectStore
	rows map[uuid.UUID]*model.Project
}

func (s *memoryProjects) GetByID(_ context.Context, id uuid.UUID) (*model.Project, error) {
	project, ok := s.rows[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return project, nil
}

type memoryMembers struct {
	store.ProjectMemberStore
	grants []model.ProjectMember
}

func (s *memoryMembers) ListByUserInOrg(_ context.Context, userID, orgID uuid.UUID) ([]model.ProjectMember, error) {
	return s.grants, nil
}

type keyFixture struct {
	store   *memoryStore
	router  *gin.Engine
	manager *auth.JWTManager
	user    *model.User
	jwt     string
}

func newKeyFixture(t *testing.T) *keyFixture {
	t.Helper()
	user := &model.User{BaseModel: model.BaseModel{ID: uuid.New()}, OrgID: uuid.New(), Role: model.RoleOwner}
	s := &memoryStore{users: &memoryUsers{rows: map[uuid.UUID]*model.User{user.ID: user}}, keys: &memoryKeys{rows: map[uuid.UUID]*model.APIKey{}}, projects: &memoryProjects{rows: map[uuid.UUID]*model.Project{}}, members: &memoryMembers{}}
	manager := auth.NewJWTManager("test-only-signing-secret", time.Hour, 24*time.Hour)
	pair, err := manager.GenerateTokenPair(user.ID, user.OrgID, string(user.Role))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(&RouterDeps{Store: s, Services: &service.Container{}, JWTManager: manager, Sessions: s, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	return &keyFixture{s, router, manager, user, pair.AccessToken}
}
func (f *keyFixture) request(method, path, credential, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	result := httptest.NewRecorder()
	f.router.ServeHTTP(result, req)
	return result
}
func requireStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, want %d (body intentionally omitted)", w.Code, status)
	}
}
func (f *keyFixture) create(t *testing.T) (model.APIKey, string) {
	t.Helper()
	w := f.request("POST", "/api/v1/api-keys", f.jwt, `{"name":"automation"}`)
	requireStatus(t, w, 201)
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("secret response is cacheable")
	}
	var result struct {
		APIKey model.APIKey `json:"api_key"`
		Key    string       `json:"key"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !auth.IsAPIKey(result.Key) {
		t.Fatal("missing one-time key")
	}
	if strings.Contains(w.Body.String(), "key_hash") {
		t.Fatal("hash leaked")
	}
	return result.APIKey, result.Key
}

func TestAPIKeyLifecycleThroughProductionRouter(t *testing.T) {
	f := newKeyFixture(t)
	requireStatus(t, f.request("GET", "/api/v1/status", "", ""), 401)
	requireStatus(t, f.request("GET", "/api/v1/status", f.jwt, ""), 200)
	key, plaintext := f.create(t)
	stored := f.store.keys.rows[key.ID]
	if stored.KeyHash == plaintext || stored.KeyHash != auth.HashAPIKey(plaintext) {
		t.Fatal("plaintext persisted or incorrect hash")
	}
	requireStatus(t, f.request("GET", "/api/v1/status", plaintext, ""), 200)
	if stored.LastUsedAt == nil {
		t.Fatal("last used time not recorded")
	}
	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	req.Header.Set("X-API-Key", plaintext)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	requireStatus(t, w, 200)
	list := f.request("GET", "/api/v1/api-keys", f.jwt, "")
	requireStatus(t, list, 200)
	if strings.Contains(list.Body.String(), plaintext) || strings.Contains(list.Body.String(), stored.KeyHash) || strings.Contains(list.Body.String(), "key_hash") {
		t.Fatal("secret/hash leaked in listing")
	}
	requireStatus(t, f.request("PUT", "/api/v1/api-keys/"+key.ID.String(), f.jwt, `{"name":"renamed","expires_at":"2099-01-01T00:00:00Z"}`), 204)
	if stored.Name != "renamed" || stored.ExpiresAt == nil {
		t.Fatal("name/expiry not saved")
	}
	requireStatus(t, f.request("PUT", "/api/v1/api-keys/"+key.ID.String(), f.jwt, `{"name":"renamed","expires_at":null}`), 204)
	if stored.ExpiresAt != nil {
		t.Fatal("expiry not cleared")
	}
	requireStatus(t, f.request("DELETE", "/api/v1/api-keys/"+key.ID.String(), f.jwt, ""), 204)
	requireStatus(t, f.request("GET", "/api/v1/status", plaintext, ""), 401)
	requireStatus(t, f.request("PUT", "/api/v1/api-keys/"+key.ID.String(), f.jwt, `{"name":"cannot revive"}`), 404)
	requireStatus(t, f.request("GET", "/api/v1/status", f.jwt, ""), 200)
}

func TestAPIKeyValidationAndRevocation(t *testing.T) {
	for _, scenario := range []string{"expired", "at expiry", "inactive", "deleted user", "missing user", "changed token version", "database error", "unknown key", "malformed key"} {
		t.Run(scenario, func(t *testing.T) {
			f := newKeyFixture(t)
			key, plaintext := f.create(t)
			stored := f.store.keys.rows[key.ID]
			switch scenario {
			case "expired":
				expiry := time.Now().Add(-time.Minute)
				stored.ExpiresAt = &expiry
			case "at expiry":
				expiry := time.Now()
				stored.ExpiresAt = &expiry
			case "inactive":
				stored.IsActive = false
			case "deleted user":
				now := time.Now()
				f.user.DeletedAt = &now
			case "missing user":
				delete(f.store.users.rows, f.user.ID)
			case "changed token version":
				f.user.TokenVersion++
			case "database error":
				f.store.keys.fail = true
			case "unknown key":
				var err error
				plaintext, _, err = auth.GenerateAPIKey()
				if err != nil {
					t.Fatal(err)
				}
			case "malformed key":
				plaintext = "sbk_invalid"
			}
			requireStatus(t, f.request("GET", "/api/v1/status", plaintext, ""), 401)
		})
	}
}

func TestAPIKeyManagementAuthorization(t *testing.T) {
	f := newKeyFixture(t)
	key, plaintext := f.create(t)
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/v1/api-keys", ""},
		{"POST", "/api/v1/api-keys", `{"name":"persistent"}`},
		{"PUT", "/api/v1/api-keys/" + key.ID.String(), `{"name":"changed"}`},
		{"DELETE", "/api/v1/api-keys/" + key.ID.String(), ""},
		{"PATCH", "/api/v1/auth/profile", `{"display_name":"changed"}`},
		{"POST", "/api/v1/auth/change-password", `{}`},
		{"POST", "/api/v1/auth/2fa/setup", `{}`},
		{"POST", "/api/v1/auth/2fa/verify", `{}`},
		{"POST", "/api/v1/auth/2fa/disable", `{}`},
	} {
		requireStatus(t, f.request(tc.method, tc.path, plaintext, tc.body), 403)
	}
	for _, role := range []model.Role{model.RoleAdmin, model.RoleMember} {
		other := &model.User{BaseModel: model.BaseModel{ID: uuid.New()}, OrgID: f.user.OrgID, Role: role}
		f.store.users.rows[other.ID] = other
		pair, err := f.manager.GenerateTokenPair(other.ID, other.OrgID, string(role))
		if err != nil {
			t.Fatal(err)
		}
		if role == model.RoleMember {
			requireStatus(t, f.request("GET", "/api/v1/api-keys", pair.AccessToken, ""), 403)
			requireStatus(t, f.request("POST", "/api/v1/api-keys", pair.AccessToken, `{"name":"forbidden"}`), 403)
			if _, _, err := service.NewAPIKeyService(f.store).Create(context.Background(), other.ID, "forbidden", nil); err == nil {
				t.Fatal("member created key")
			}
		} else {
			list := f.request("GET", "/api/v1/api-keys", pair.AccessToken, "")
			requireStatus(t, list, 200)
			if strings.Contains(list.Body.String(), key.ID.String()) {
				t.Fatal("other admin can list owner's keys")
			}
			requireStatus(t, f.request("DELETE", "/api/v1/api-keys/"+key.ID.String(), pair.AccessToken, ""), 404)
			requireStatus(t, f.request("PUT", "/api/v1/api-keys/"+key.ID.String(), pair.AccessToken, `{"name":"steal"}`), 404)
			// An injected user_id is ignored; ownership is always taken from JWT context.
			w := f.request("POST", "/api/v1/api-keys", pair.AccessToken, `{"name":"own key","user_id":"`+f.user.ID.String()+`"}`)
			requireStatus(t, w, 201)
			var result struct {
				APIKey model.APIKey `json:"api_key"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.APIKey.UserID != other.ID {
				t.Fatal("caller-controlled ownership")
			}
		}
	}
	for _, body := range []string{`{}`, `{"name":"   "}`, `{"name":"` + strings.Repeat("a", 101) + `"}`, `{"name":"bad date","expires_at":"not-a-date"}`, `{"name":"past","expires_at":"2000-01-01T00:00:00Z"}`} {
		requireStatus(t, f.request("POST", "/api/v1/api-keys", f.jwt, body), 400)
	}
	requireStatus(t, f.request("DELETE", "/api/v1/api-keys/not-a-uuid", f.jwt, ""), 400)
}

func TestAPIKeyUsesCurrentRoleAndProjectGuards(t *testing.T) {
	f := newKeyFixture(t)
	_, plaintext := f.create(t)
	// Deliberately omit a token-version bump to prove no stale admin role is used.
	f.user.Role = model.RoleMember
	projectID, otherProjectID, foreignID := uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{projectID, otherProjectID} {
		f.store.projects.rows[id] = &model.Project{BaseModel: model.BaseModel{ID: id}, OrgID: f.user.OrgID}
	}
	f.store.projects.rows[foreignID] = &model.Project{BaseModel: model.BaseModel{ID: foreignID}, OrgID: uuid.New()}
	f.store.members.grants = []model.ProjectMember{{ProjectID: projectID, UserID: f.user.ID, Role: "viewer"}}
	guard := v1.NewGuard(f.store)
	g := f.router.Group("/test", middleware.Auth(f.manager, f.store, service.NewAPIKeyService(f.store)))
	g.GET("/admin", middleware.RequireRole("owner", "admin"), func(c *gin.Context) { c.Status(200) })
	g.GET("/projects/:id", guard.Project(), func(c *gin.Context) { c.Status(200) })
	g.PUT("/projects/:id", guard.Project(), func(c *gin.Context) { c.Status(200) })
	requireStatus(t, f.request("GET", "/test/admin", plaintext, ""), 403)
	requireStatus(t, f.request("GET", "/test/projects/"+projectID.String(), plaintext, ""), 200)
	requireStatus(t, f.request("PUT", "/test/projects/"+projectID.String(), plaintext, `{}`), 403)
	requireStatus(t, f.request("GET", "/test/projects/"+otherProjectID.String(), plaintext, ""), 403)
	requireStatus(t, f.request("GET", "/test/projects/"+foreignID.String(), plaintext, ""), 403)
	f.store.members.grants[0].Role = "developer"
	requireStatus(t, f.request("PUT", "/test/projects/"+projectID.String(), plaintext, `{}`), 200)
	f.user.TokenVersion++
	requireStatus(t, f.request("GET", "/api/v1/status", plaintext, ""), 401)
	requireStatus(t, f.request("GET", "/api/v1/status", f.jwt, ""), 401)
}
