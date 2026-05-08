package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAugmentOfficialSessionBindRejectsMissingBindToken(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	handler := NewAuthHandler(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		service.NewAugmentOfficialSessionService(&handlerOfficialSessionStoreStub{}, newHandlerTestAugmentSessionVaultCipher(t), "bind-secret"),
	)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 42})
		c.Next()
	})
	router.POST("/api/v1/plugin/augment/official-session/bind", handler.AugmentOfficialSessionBind)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugin/augment/official-session/bind", bytes.NewReader([]byte(`{
		"bind_intent_id":"bind-intent-1",
		"state":"state-1",
		"mode":"official_passthrough",
		"source":"official_quick_login",
		"payload":{
			"tenant_url":"https://official.augment.local",
			"access_token":"access-secret",
			"refresh_token":"refresh-secret",
			"expires_at":"2026-05-08T15:00:00Z",
			"scopes":["augment:session"]
		}
	}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Body.String(), "AUGMENT_OFFICIAL_BIND_TOKEN_MISSING")
}

func TestAugmentOfficialSessionBindIntentRejectsInvalidOrigin(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	handler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "https://zhumeng.local"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		service.NewAugmentOfficialSessionService(&handlerOfficialSessionStoreStub{}, newHandlerTestAugmentSessionVaultCipher(t), "bind-secret"),
	)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 42})
		c.Next()
	})
	router.POST("/api/v1/plugin/augment/official-session/bind-intents", handler.AugmentOfficialSessionBindIntent)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugin/augment/official-session/bind-intents", bytes.NewReader([]byte(`{
		"mode":"official_passthrough",
		"source":"official_quick_login",
		"tenant_allowlist":["https://official.augment.local"]
	}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.local")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "AUGMENT_OFFICIAL_BIND_ORIGIN_INVALID")
}

func TestAugmentOfficialSessionStatusDoesNotExposeSecrets(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	now := time.Date(2026, 5, 8, 15, 0, 0, 0, time.UTC)
	store := &handlerOfficialSessionStoreStub{
		publicView: &service.AugmentOfficialSessionStoredPublicView{
			UserID:       42,
			Mode:         "official_passthrough",
			Source:       "official_quick_login",
			TenantOrigin: "https://official.augment.local",
			Scopes:       []string{"augment:session"},
			ExpiresAt:    handlerTimePtr(now.Add(30 * time.Minute)),
			Status:       "active",
			Fingerprint:  "abcdef0123456789",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}
	handler := NewAuthHandler(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		service.NewAugmentOfficialSessionService(store, newHandlerTestAugmentSessionVaultCipher(t), "bind-secret"),
	)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 42})
		c.Next()
	})
	router.GET("/api/v1/plugin/augment/official-session", handler.AugmentOfficialSessionStatus)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugin/augment/official-session", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "access_token")
	require.NotContains(t, rec.Body.String(), "refresh_token")
	require.NotContains(t, rec.Body.String(), "encrypted_credential_payload")
	require.Contains(t, rec.Body.String(), "fingerprint_prefix")
}

func TestAugmentQuickLoginGrantOfficialPassthroughUsesBoundOfficialSession(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	now := time.Date(2026, 5, 8, 15, 10, 0, 0, time.UTC)
	user := &service.User{
		ID:     42,
		Email:  "official@sub2api.local",
		Role:   service.RoleAdmin,
		Status: service.StatusActive,
	}
	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		nil,
		nil,
		nil,
	)

	cipher := newHandlerTestAugmentSessionVaultCipher(t)
	encryptedPayload := mustEncryptHandlerOfficialPayload(t, cipher, map[string]string{
		"access_token":        "official-access",
		"refresh_token":       "official-refresh",
		"official_session_id": "sess_123",
	})
	store := &handlerOfficialSessionStoreStub{
		credentialRow: &service.AugmentOfficialSessionStoredCredentialRow{
			UserID:                     42,
			Mode:                       "official_passthrough",
			Source:                     "official_quick_login",
			TenantOrigin:               "https://official.augment.local",
			PortalOrigin:               handlerStringPtr("https://portal.augment.local"),
			Scopes:                     []string{"augment:session"},
			ExpiresAt:                  handlerTimePtr(now.Add(45 * time.Minute)),
			Status:                     "active",
			EncryptedCredentialPayload: encryptedPayload,
			CredentialSchemaVersion:    1,
			KeyVersion:                 "key-active",
			Fingerprint:                "abcdef0123456789",
			CreatedAt:                  now,
			UpdatedAt:                  now,
		},
	}
	officialService := service.NewAugmentOfficialSessionService(store, cipher, "bind-secret")

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
		officialService,
	)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 42})
		c.Next()
	})
	router.POST("/api/v1/plugin/augment/quick-login/grant", authHandler.AugmentQuickLoginGrant)
	router.POST("/api/v1/plugin/augment/callback/exchange", authHandler.AugmentCallbackExchange)

	grantReq := httptest.NewRequest(http.MethodPost, "/api/v1/plugin/augment/quick-login/grant", bytes.NewReader([]byte(`{
		"mode":"official_passthrough"
	}`)))
	grantReq.Header.Set("Content-Type", "application/json")
	grantReq.Header.Set("Origin", "http://127.0.0.1:18082")
	grantRec := httptest.NewRecorder()
	router.ServeHTTP(grantRec, grantReq)
	require.Equal(t, http.StatusOK, grantRec.Code)

	var grantBody struct {
		Data struct {
			Grant string `json:"grant"`
			State string `json:"state"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(grantRec.Body.Bytes(), &grantBody))

	exchangePayload, err := json.Marshal(map[string]string{
		"grant": grantBody.Data.Grant,
		"state": grantBody.Data.State,
	})
	require.NoError(t, err)

	exchangeReq := httptest.NewRequest(http.MethodPost, "/api/v1/plugin/augment/callback/exchange", bytes.NewReader(exchangePayload))
	exchangeReq.Header.Set("Content-Type", "application/json")
	exchangeReq.Header.Set("Origin", "http://127.0.0.1:18082")
	exchangeRec := httptest.NewRecorder()
	router.ServeHTTP(exchangeRec, exchangeReq)
	require.Equal(t, http.StatusOK, exchangeRec.Code)
	require.Contains(t, exchangeRec.Body.String(), `"access_token":"official-access"`)
	require.Contains(t, exchangeRec.Body.String(), `"refresh_token":"official-refresh"`)
	require.Contains(t, exchangeRec.Body.String(), `"tenant_url":"https://official.augment.local"`)
	require.Contains(t, exchangeRec.Body.String(), `"session_source":"official"`)
}

type handlerOfficialSessionStoreStub struct {
	publicView    *service.AugmentOfficialSessionStoredPublicView
	credentialRow *service.AugmentOfficialSessionStoredCredentialRow
}

func (s *handlerOfficialSessionStoreStub) CreateBindIntent(ctx context.Context, input service.AugmentOfficialSessionBindIntentStoreCreateInput) (*service.AugmentOfficialSessionBindIntentStoreRecord, error) {
	return nil, nil
}

func (s *handlerOfficialSessionStoreStub) ConsumeBindIntent(ctx context.Context, bindIntentID string, userID int64) (*service.AugmentOfficialSessionBindIntentStoreRecord, error) {
	return nil, nil
}

func (s *handlerOfficialSessionStoreStub) UpsertActiveSession(ctx context.Context, input service.AugmentOfficialSessionStoredSessionInput) (*service.AugmentOfficialSessionStoredPublicView, error) {
	return s.publicView, nil
}

func (s *handlerOfficialSessionStoreStub) GetActiveSessionPublicView(ctx context.Context, userID int64) (*service.AugmentOfficialSessionStoredPublicView, error) {
	return s.publicView, nil
}

func (s *handlerOfficialSessionStoreStub) GetActiveSessionAdminView(ctx context.Context, userID int64) (*service.AugmentOfficialSessionStoredAdminView, error) {
	if s.publicView == nil {
		return nil, nil
	}
	return &service.AugmentOfficialSessionStoredAdminView{
		UserID:               s.publicView.UserID,
		Mode:                 s.publicView.Mode,
		Source:               s.publicView.Source,
		TenantOrigin:         s.publicView.TenantOrigin,
		PortalOrigin:         s.publicView.PortalOrigin,
		Scopes:               append([]string(nil), s.publicView.Scopes...),
		ExpiresAt:            s.publicView.ExpiresAt,
		LastRefreshAt:        s.publicView.LastRefreshAt,
		LastSuccessAt:        s.publicView.LastSuccessAt,
		LastErrorAt:          s.publicView.LastErrorAt,
		LastErrorCode:        s.publicView.LastErrorCode,
		Status:               s.publicView.Status,
		CredentialSchemaVersion: s.publicView.CredentialSchemaVersion,
		KeyVersion:           s.publicView.KeyVersion,
		Fingerprint:          s.publicView.Fingerprint,
		CreatedAt:            s.publicView.CreatedAt,
		UpdatedAt:            s.publicView.UpdatedAt,
		RevokedAt:            s.publicView.RevokedAt,
	}, nil
}

func (s *handlerOfficialSessionStoreStub) ListAdminSessions(ctx context.Context) ([]service.AugmentOfficialSessionStoredAdminView, error) {
	view, err := s.GetActiveSessionAdminView(ctx, 0)
	if err != nil || view == nil {
		return nil, err
	}
	return []service.AugmentOfficialSessionStoredAdminView{*view}, nil
}

func (s *handlerOfficialSessionStoreStub) GetActiveSessionCredentialRow(ctx context.Context, userID int64) (*service.AugmentOfficialSessionStoredCredentialRow, error) {
	return s.credentialRow, nil
}

func (s *handlerOfficialSessionStoreStub) RevokeActiveSession(ctx context.Context, userID int64) (*service.AugmentOfficialSessionStoredPublicView, error) {
	return s.publicView, nil
}

func newHandlerTestAugmentSessionVaultCipher(t *testing.T) *service.AugmentSessionVaultCipher {
	t.Helper()
	cipher, err := service.NewAugmentSessionVaultCipher(service.AugmentSessionVaultKeyset{
		ActiveKeyID: "key-active",
		Keys: map[string][]byte{
			"key-active": []byte("0123456789abcdef0123456789abcdef"),
		},
	})
	require.NoError(t, err)
	return cipher
}

func mustEncryptHandlerOfficialPayload(t *testing.T, cipher *service.AugmentSessionVaultCipher, payload map[string]string) []byte {
	t.Helper()
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	out, err := cipher.Encrypt(data)
	require.NoError(t, err)
	return out
}

func handlerTimePtr(value time.Time) *time.Time {
	return &value
}

func handlerStringPtr(value string) *string {
	return &value
}
