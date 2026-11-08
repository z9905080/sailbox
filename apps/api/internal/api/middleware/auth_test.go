package middleware

import (
	"context"
	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sailboxhq/sailbox/apps/api/internal/auth"
	"github.com/sailboxhq/sailbox/apps/api/internal/model"
	"net/http/httptest"
	"testing"
	"time"
)

type testSessions bool

func (s testSessions) ValidateSession(context.Context, uuid.UUID, int) bool { return bool(s) }

type testKeys struct {
	user  *model.User
	calls int
}

func (s *testKeys) AuthenticateAPIKey(context.Context, string) (*model.User, error) {
	s.calls++
	if s.user == nil {
		return nil, auth.ErrInvalidToken
	}
	return s.user, nil
}
func TestAuthCredentialsAndJWTRegression(t *testing.T) {
	gin.SetMode(gin.TestMode)
	user := &model.User{BaseModel: model.BaseModel{ID: uuid.New()}, OrgID: uuid.New(), Role: model.RoleMember}
	manager := auth.NewJWTManager("test-signing-secret", time.Hour, 2*time.Hour)
	tokens, err := manager.GenerateTokenPair(user.ID, user.OrgID, string(user.Role))
	if err != nil {
		t.Fatal(err)
	}
	expiredManager := auth.NewJWTManager("test-signing-secret", -time.Hour, time.Hour)
	expired, err := expiredManager.GenerateTokenPair(user.ID, user.OrgID, string(user.Role))
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name         string
		headers      map[string][]string
		validSession bool
		validKey     bool
		status       int
		jwtOnly      bool
		adminOnly    bool
	}{
		{name: "JWT", headers: map[string][]string{"Authorization": {"Bearer " + tokens.AccessToken}}, validSession: true, status: 200},
		{name: "revoked JWT session", headers: map[string][]string{"Authorization": {"Bearer " + tokens.AccessToken}}, status: 401},
		{name: "expired JWT", headers: map[string][]string{"Authorization": {"Bearer " + expired.AccessToken}}, validSession: true, status: 401},
		{name: "bearer key", headers: map[string][]string{"Authorization": {"Bearer " + key}}, validKey: true, status: 200},
		{name: "key header", headers: map[string][]string{"X-Api-Key": {key}}, validKey: true, status: 200},
		{name: "invalid key", headers: map[string][]string{"X-Api-Key": {key}}, status: 401},
		{name: "bad key format", headers: map[string][]string{"X-Api-Key": {"sbk_bad"}}, validKey: true, status: 401},
		{name: "JWT not accepted as X-API-Key", headers: map[string][]string{"X-Api-Key": {tokens.AccessToken}}, status: 401},
		{name: "competing headers", headers: map[string][]string{"Authorization": {"Bearer " + tokens.AccessToken}, "X-Api-Key": {key}}, validKey: true, validSession: true, status: 401},
		{name: "duplicate authorization", headers: map[string][]string{"Authorization": {"Bearer " + key, "Bearer " + key}}, validKey: true, status: 401},
		{name: "duplicate API key", headers: map[string][]string{"X-Api-Key": {key, key}}, validKey: true, status: 401},
		{name: "no credentials", status: 401},
		{name: "bad scheme", headers: map[string][]string{"Authorization": {"Basic " + key}}, validKey: true, status: 401},
		{name: "key cannot manage keys", headers: map[string][]string{"Authorization": {"Bearer " + key}}, validKey: true, jwtOnly: true, status: 403},
		{name: "JWT can use JWT-only", headers: map[string][]string{"Authorization": {"Bearer " + tokens.AccessToken}}, validSession: true, jwtOnly: true, status: 200},
		{name: "key member not admin", headers: map[string][]string{"X-Api-Key": {key}}, validKey: true, adminOnly: true, status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keys := &testKeys{}
			if tc.validKey {
				keys.user = user
			}
			r := gin.New()
			handlers := []gin.HandlerFunc{Auth(manager, testSessions(tc.validSession), keys)}
			if tc.jwtOnly {
				handlers = append(handlers, RequireJWT())
			}
			if tc.adminOnly {
				handlers = append(handlers, RequireRole("owner", "admin"))
			}
			handlers = append(handlers, func(c *gin.Context) {
				if GetUserID(c) != user.ID || GetOrgID(c) != user.OrgID || GetUserRole(c) != string(user.Role) {
					t.Error("identity context changed")
				}
				c.Status(200)
			})
			r.GET("/test", handlers...)
			req := httptest.NewRequest("GET", "/test", nil)
			for name, values := range tc.headers {
				for _, value := range values {
					req.Header.Add(name, value)
				}
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d", w.Code, tc.status)
			}
			if tc.status == 401 && tc.validKey && keys.calls != 0 {
				t.Error("invalid credentials fell back to key authentication")
			}
		})
	}
}
func TestTelemetryRedaction(t *testing.T) {
	event := &sentry.Event{Request: &sentry.Request{Headers: map[string]string{"x-api-key": "secret", "AUTHORIZATION": "secret", "Cookie": "secret", "Accept": "application/json"}}}
	RedactSentryCredentials(event, nil)
	if len(event.Request.Headers) != 1 || event.Request.Headers["Accept"] != "application/json" {
		t.Fatal("sensitive telemetry headers not removed")
	}
}
