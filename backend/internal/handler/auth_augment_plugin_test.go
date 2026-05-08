package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type augmentPluginAuthStub struct{}

func (augmentPluginAuthStub) GenerateTokenPair(ctx context.Context, user *service.User, familyID string) (*service.TokenPair, error) {
	return &service.TokenPair{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ExpiresIn:    3600,
	}, nil
}

func (augmentPluginAuthStub) RefreshTokenPair(ctx context.Context, refreshToken string) (*service.TokenPairWithUser, error) {
	return nil, nil
}

func (augmentPluginAuthStub) ValidateToken(token string) (*service.JWTClaims, error) {
	return nil, service.ErrInvalidToken
}

type augmentPluginJWTAuthStub struct {
	token  string
	userID int64
}

func (s augmentPluginJWTAuthStub) GenerateTokenPair(ctx context.Context, user *service.User, familyID string) (*service.TokenPair, error) {
	return augmentPluginAuthStub{}.GenerateTokenPair(ctx, user, familyID)
}

func (s augmentPluginJWTAuthStub) RefreshTokenPair(ctx context.Context, refreshToken string) (*service.TokenPairWithUser, error) {
	return nil, nil
}

func (s augmentPluginJWTAuthStub) ValidateToken(token string) (*service.JWTClaims, error) {
	if token == s.token {
		return &service.JWTClaims{UserID: s.userID}, nil
	}
	return nil, service.ErrInvalidToken
}

type augmentPluginUserStub struct {
	user  *service.User
	users map[int64]*service.User
}

func (s augmentPluginUserStub) GetByID(ctx context.Context, id int64) (*service.User, error) {
	if s.users != nil {
		if user, ok := s.users[id]; ok {
			return user, nil
		}
	}
	if s.user != nil && s.user.ID == id {
		return s.user, nil
	}
	return nil, service.ErrUserNotFound
}

type augmentPluginAPIKeyStub struct {
	apiKeyByValue   map[string]*service.APIKey
	keysByUser      map[int64][]service.APIKey
	availableByUser map[int64][]service.Group
}

func (s augmentPluginAPIKeyStub) GetByKey(ctx context.Context, key string) (*service.APIKey, error) {
	if apiKey, ok := s.apiKeyByValue[key]; ok {
		return apiKey, nil
	}
	return nil, service.ErrAPIKeyNotFound
}

func (s augmentPluginAPIKeyStub) List(ctx context.Context, userID int64, params pagination.PaginationParams, filters service.APIKeyListFilters) ([]service.APIKey, *pagination.PaginationResult, error) {
	keys := append([]service.APIKey(nil), s.keysByUser[userID]...)
	return keys, &pagination.PaginationResult{
		Total:    int64(len(keys)),
		Page:     params.Page,
		PageSize: params.PageSize,
		Pages:    1,
	}, nil
}

func (s augmentPluginAPIKeyStub) GetAvailableGroups(ctx context.Context, userID int64) ([]service.Group, error) {
	return append([]service.Group(nil), s.availableByUser[userID]...), nil
}

func (s augmentPluginAPIKeyStub) Create(ctx context.Context, userID int64, req service.CreateAPIKeyRequest) (*service.APIKey, error) {
	return nil, service.ErrServiceUnavailable
}

type augmentPluginSubscriptionStub struct {
	activeByUser map[int64][]service.UserSubscription
}

func (s augmentPluginSubscriptionStub) ListActiveUserSubscriptions(ctx context.Context, userID int64) ([]service.UserSubscription, error) {
	return append([]service.UserSubscription(nil), s.activeByUser[userID]...), nil
}

type augmentPluginSettingStub struct {
	public *service.PublicSettings
}

func (s augmentPluginSettingStub) GetPublicSettings(ctx context.Context) (*service.PublicSettings, error) {
	return s.public, nil
}

func (s augmentPluginSettingStub) GetSiteName(ctx context.Context) string {
	if s.public != nil && s.public.SiteName != "" {
		return s.public.SiteName
	}
	return "Zhumeng"
}

func TestAugmentCallbackExchangeAcceptsGrantAndCode(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:     1,
		Email:  "admin@sub2api.local",
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

	handler := NewAuthHandler(nil, nil, nil, nil, nil, nil, nil, pluginService)

	router := gin.New()
	router.POST("/api/v1/plugin/augment/callback/exchange", handler.AugmentCallbackExchange)

	testCases := []struct {
		name string
		body map[string]string
	}{
		{
			name: "grant field",
			body: map[string]string{
				"grant": "",
				"state": "",
			},
		},
		{
			name: "legacy code field",
			body: map[string]string{
				"code":  "",
				"state": "",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			grant, err := pluginService.CreateQuickLoginGrant(context.Background(), user.ID, service.AugmentQuickLoginGrantOptions{
				TenantURL: "http://127.0.0.1:18082",
				Mode:      service.AugmentQuickLoginModeLocalCompat,
			})
			require.NoError(t, err)

			tc.body["state"] = grant.State
			if _, ok := tc.body["grant"]; ok {
				tc.body["grant"] = grant.Grant
			}
			if _, ok := tc.body["code"]; ok {
				tc.body["code"] = grant.Grant
			}

			payload, marshalErr := json.Marshal(tc.body)
			require.NoError(t, marshalErr)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/plugin/augment/callback/exchange", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)
			require.Contains(t, rec.Body.String(), `"access_token":"access-token"`)
			require.Contains(t, rec.Body.String(), `"refresh_token":"refresh-token"`)
			require.Contains(t, rec.Body.String(), `"session_source":"local_compat"`)
		})
	}
}

func TestAugmentQuickLoginGrantAcceptsOfficialPassthroughBundle(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:     1,
		Email:  "admin@sub2api.local",
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

	handler := NewAuthHandler(nil, nil, nil, nil, nil, nil, nil, pluginService)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: user.ID})
		c.Next()
	})
	router.POST("/api/v1/plugin/augment/quick-login/grant", handler.AugmentQuickLoginGrant)
	router.POST("/api/v1/plugin/augment/callback/exchange", handler.AugmentCallbackExchange)

	grantReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/plugin/augment/quick-login/grant",
		bytes.NewReader([]byte(`{
			"official_session_bundle":"{\"tenant_url\":\"https://official.augment.local\",\"access_token\":\"official-access\",\"refresh_token\":\"official-refresh\",\"expires_at\":\"2026-04-21T12:30:00Z\",\"scopes\":[\"augment:session\"]}"
		}`)),
	)
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
	require.NotEmpty(t, grantBody.Data.Grant)
	require.NotEmpty(t, grantBody.Data.State)

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

func TestAugmentCallbackExchangeIncludesPortalURLFromGrantRecord(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:       1,
		Email:    "admin@zhumeng.local",
		Username: "zhumeng",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	apiKey := service.APIKey{
		ID:        1,
		UserID:    user.ID,
		Key:       "sk-plugin-generated",
		Name:      "Augment Plugin",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      user,
	}

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			keysByUser: map[int64][]service.APIKey{
				user.ID: {apiKey},
			},
		},
		nil,
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)

	handler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: user.ID})
		c.Next()
	})
	router.POST("/api/v1/plugin/augment/quick-login/grant", handler.AugmentQuickLoginGrant)
	router.POST("/api/v1/plugin/augment/callback/exchange", handler.AugmentCallbackExchange)

	grantReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/plugin/augment/quick-login/grant",
		bytes.NewReader([]byte(`{
			"official_session_bundle":"{\"tenant_url\":\"https://official.augment.local\",\"access_token\":\"official-access\",\"refresh_token\":\"official-refresh\",\"expires_at\":\"2026-04-21T12:30:00Z\",\"scopes\":[\"augment:session\"]}"
		}`)),
	)
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
	require.Contains(t, exchangeRec.Body.String(), `"portal_url":"http://127.0.0.1:18081?token=sk-plugin-generated"`)
}

func TestAugmentQuickLoginGrantDefaultsToLocalCompatWithoutOfficialInputs(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:     1,
		Email:  "admin@sub2api.local",
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

	handler := NewAuthHandler(nil, nil, nil, nil, nil, nil, nil, pluginService)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: user.ID})
		c.Next()
	})
	router.POST("/api/v1/plugin/augment/quick-login/grant", handler.AugmentQuickLoginGrant)
	router.POST("/api/v1/plugin/augment/callback/exchange", handler.AugmentCallbackExchange)

	grantReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/plugin/augment/quick-login/grant",
		bytes.NewReader([]byte(`{}`)),
	)
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
	require.NotEmpty(t, grantBody.Data.Grant)
	require.NotEmpty(t, grantBody.Data.State)

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
	require.Contains(t, exchangeRec.Body.String(), `"access_token":"access-token"`)
	require.Contains(t, exchangeRec.Body.String(), `"refresh_token":"refresh-token"`)
	require.Contains(t, exchangeRec.Body.String(), `"session_source":"local_compat"`)
}

func TestAugmentQuickLoginGrantIncludesPortalInVSCodeDeeplink(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:       1,
		Email:    "admin@zhumeng.local",
		Username: "zhumeng",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	apiKey := service.APIKey{
		ID:        1,
		UserID:    user.ID,
		Key:       "sk-plugin-generated",
		Name:      "Augment Plugin",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      user,
	}

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			keysByUser: map[int64][]service.APIKey{
				user.ID: {apiKey},
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)

	handler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	router := gin.New()
	router.POST("/api/v1/plugin/augment/quick-login/grant", func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: user.ID})
		c.Set(string(servermiddleware.ContextKeyUserRole), user.Role)
		handler.AugmentQuickLoginGrant(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugin/augment/quick-login/grant", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:18082")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	deeplink, _ := body.Data["vscode_deeplink"].(string)
	require.NotEmpty(t, deeplink)
	parsed, err := url.Parse(deeplink)
	require.NoError(t, err)
	portal := parsed.Query().Get("portal")
	require.NotEmpty(t, portal)
	require.Contains(t, portal, "http://127.0.0.1:18081")
	require.Contains(t, portal, "token=")
}

func TestBuildAugmentQuickLoginGrantOptionsKeepsGenericSessionInputsInLocalCompatMode(t *testing.T) {
	t.Parallel()

	options := buildAugmentQuickLoginGrantOptions(augmentQuickLoginGrantRequest{
		SessionBundle: `{"tenant_url":"https://official.augment.local","access_token":"official-access"}`,
		AccessToken:   "generic-access-token",
		TenantURL:     "https://tenant.from-query.local",
	}, "https://tenant.local")

	require.Equal(t, service.AugmentQuickLoginModeLocalCompat, options.Mode)
	require.Nil(t, options.OfficialSessionBundle)
	require.Equal(t, "https://tenant.local", options.TenantURL)
}

func TestBuildAugmentSessionRefreshOptionsKeepsGenericSessionInputsInLocalCompatMode(t *testing.T) {
	t.Parallel()

	options := buildAugmentSessionRefreshOptions(augmentSessionRefreshRequest{
		RefreshToken:  "refresh-local",
		SessionBundle: `{"tenant_url":"https://official.augment.local","access_token":"official-access"}`,
		AccessToken:   "generic-access-token",
		TenantURL:     "https://tenant.from-query.local",
	}, "https://tenant.local")

	require.Equal(t, service.AugmentQuickLoginModeLocalCompat, options.Mode)
	require.Nil(t, options.OfficialSessionBundle)
	require.Equal(t, "https://tenant.local", options.TenantURL)
}

func TestAugmentSessionRefreshAcceptsOfficialPassthroughBundle(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	handler := NewAuthHandler(nil, nil, nil, nil, nil, nil, nil, service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: &service.User{
			ID:     1,
			Email:  "admin@sub2api.local",
			Role:   service.RoleAdmin,
			Status: service.StatusActive,
		}},
		nil,
		nil,
		nil,
	))

	router := gin.New()
	router.POST("/api/v1/plugin/augment/session/refresh", handler.AugmentSessionRefresh)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/plugin/augment/session/refresh",
		bytes.NewReader([]byte(`{
			"refresh_token":"official-refresh-next",
			"mode":"official_passthrough",
			"official_session_bundle":"{\"tenant_url\":\"https://official.augment.local\",\"access_token\":\"official-access-next\",\"refresh_token\":\"official-refresh-next\",\"expires_at\":\"2026-04-21T13:00:00Z\",\"scopes\":[\"augment:session\"]}"
		}`)),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:18082")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"access_token":"official-access-next"`)
	require.Contains(t, rec.Body.String(), `"tenant_url":"https://official.augment.local"`)
	require.Contains(t, rec.Body.String(), `"session_source":"official"`)
}

func TestAugmentSessionRefreshOfficialPassthroughUsesBoundOfficialSessionFromBearer(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:     1,
		Email:  "admin@sub2api.local",
		Role:   service.RoleAdmin,
		Status: service.StatusActive,
	}
	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginJWTAuthStub{token: "jwt-official", userID: user.ID},
		augmentPluginUserStub{user: user},
		nil,
		nil,
		nil,
	)
	cipher, err := service.NewAugmentSessionVaultCipher(service.AugmentSessionVaultKeyset{
		ActiveKeyID: "key-active",
		Keys: map[string][]byte{
			"key-active": []byte("0123456789abcdef0123456789abcdef"),
		},
	})
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]string{
		"access_token":        "official-access-next",
		"refresh_token":       "official-refresh-next",
		"official_session_id": "sess_123",
	})
	require.NoError(t, err)
	encryptedPayload, err := cipher.Encrypt(payload)
	require.NoError(t, err)

	officialService := service.NewAugmentOfficialSessionService(
		&handlerOfficialSessionStoreStub{
			credentialRow: &service.AugmentOfficialSessionStoredCredentialRow{
				UserID:                     user.ID,
				Mode:                       "official_passthrough",
				Source:                     "official_quick_login",
				TenantOrigin:               "https://official.augment.local",
				PortalOrigin:               handlerStringPtr("https://portal.augment.local"),
				Scopes:                     []string{"augment:session"},
				ExpiresAt:                  handlerTimePtr(time.Date(2026, 5, 8, 13, 0, 0, 0, time.UTC)),
				Status:                     "active",
				EncryptedCredentialPayload: encryptedPayload,
				CredentialSchemaVersion:    1,
				KeyVersion:                 "key-active",
				Fingerprint:                "abcdef0123456789",
				CreatedAt:                  time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
				UpdatedAt:                  time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
			},
		},
		cipher,
		"bind-secret",
	)

	handler := NewAuthHandler(nil, nil, nil, nil, nil, nil, nil, pluginService, officialService)
	router := gin.New()
	router.POST("/api/v1/plugin/augment/session/refresh", handler.AugmentSessionRefresh)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/plugin/augment/session/refresh",
		bytes.NewReader([]byte(`{
			"refresh_token":"official-refresh-next",
			"mode":"official_passthrough"
		}`)),
	)
	req.Header.Set("Authorization", "Bearer jwt-official")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:18082")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"access_token":"official-access-next"`)
	require.Contains(t, rec.Body.String(), `"tenant_url":"https://official.augment.local"`)
	require.Contains(t, rec.Body.String(), `"session_source":"official"`)
}

func TestAugmentSessionRefreshOfficialPassthroughRejectsAPIKeyBearerForBoundSession(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:     1,
		Email:  "admin@sub2api.local",
		Role:   service.RoleAdmin,
		Status: service.StatusActive,
	}
	apiKey := &service.APIKey{
		ID:        11,
		UserID:    user.ID,
		Key:       "sk-bound-refresh",
		Name:      "bound-refresh",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{
				apiKey.Key: apiKey,
			},
		},
		nil,
		nil,
	)
	cipher, err := service.NewAugmentSessionVaultCipher(service.AugmentSessionVaultKeyset{
		ActiveKeyID: "key-active",
		Keys: map[string][]byte{
			"key-active": []byte("0123456789abcdef0123456789abcdef"),
		},
	})
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]string{
		"access_token":        "official-access-next",
		"refresh_token":       "official-refresh-next",
		"official_session_id": "sess_123",
	})
	require.NoError(t, err)
	encryptedPayload, err := cipher.Encrypt(payload)
	require.NoError(t, err)

	officialService := service.NewAugmentOfficialSessionService(
		&handlerOfficialSessionStoreStub{
			credentialRow: &service.AugmentOfficialSessionStoredCredentialRow{
				UserID:                     user.ID,
				Mode:                       "official_passthrough",
				Source:                     "official_quick_login",
				TenantOrigin:               "https://official.augment.local",
				Scopes:                     []string{"augment:session"},
				ExpiresAt:                  handlerTimePtr(time.Date(2026, 5, 8, 13, 0, 0, 0, time.UTC)),
				Status:                     "active",
				EncryptedCredentialPayload: encryptedPayload,
				CredentialSchemaVersion:    1,
				KeyVersion:                 "key-active",
				Fingerprint:                "abcdef0123456789",
				CreatedAt:                  time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
				UpdatedAt:                  time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
			},
		},
		cipher,
		"bind-secret",
	)

	handler := NewAuthHandler(nil, nil, nil, nil, nil, nil, nil, pluginService, officialService)
	router := gin.New()
	router.POST("/api/v1/plugin/augment/session/refresh", handler.AugmentSessionRefresh)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/plugin/augment/session/refresh",
		bytes.NewReader([]byte(`{
			"refresh_token":"official-refresh-next",
			"mode":"official_passthrough"
		}`)),
	)
	req.Header.Set("Authorization", "Bearer "+apiKey.Key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:18082")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "AUGMENT_PLUGIN_OFFICIAL_SESSION_REQUIRED")
}

func TestAugmentSummaryAPIKeyPrincipalDoesNotLeakSiblingActiveKey(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:          8,
		Email:       "summary-isolated@sub2api.local",
		Username:    "summary-isolated",
		Role:        service.RoleUser,
		Status:      service.StatusActive,
		Balance:     9.5,
		Concurrency: 2,
	}
	olderKey := service.APIKey{
		ID:        40,
		UserID:    user.ID,
		Key:       "sk-older-handler",
		Name:      "older",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 9, 0, 0, 0, time.UTC),
	}
	currentKey := service.APIKey{
		ID:        41,
		UserID:    user.ID,
		Key:       "sk-current-handler",
		Name:      "current",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC),
		User:      user,
	}

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{
				currentKey.Key: &currentKey,
			},
			keysByUser: map[int64][]service.APIKey{
				user.ID: {olderKey, currentKey},
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{SiteName: "Augment Local"},
		},
	)

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	router := gin.New()
	router.GET("/api/v1/plugin/augment/summary", authHandler.AugmentSummary)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugin/augment/summary", nil)
	req.Header.Set("Authorization", "Bearer "+currentKey.Key)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			GatewayAPIKey string `json:"gateway_api_key"`
			PrimaryAPIKey string `json:"primary_api_key"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	require.Equal(t, "success", body.Message)
	require.Equal(t, currentKey.Key, body.Data.GatewayAPIKey)
	require.Equal(t, currentKey.Key, body.Data.PrimaryAPIKey)
}

func TestAugmentLegacyBalanceAndModelsCompatibility(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:          1,
		Email:       "admin@zhumeng.local",
		Username:    "zhumeng",
		Role:        service.RoleAdmin,
		Status:      service.StatusActive,
		Balance:     40.73,
		Concurrency: 5,
	}
	apiKey := &service.APIKey{
		ID:        1,
		UserID:    user.ID,
		Key:       "sk-compat-1",
		Name:      "compat",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	group := service.Group{
		ID:                 100,
		Name:               "OpenAI",
		Platform:           service.PlatformOpenAI,
		Status:             service.StatusActive,
		Hydrated:           true,
		DefaultMappedModel: "gpt-5.4",
	}
	expiresAt := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{
				apiKey.Key: apiKey,
			},
			keysByUser: map[int64][]service.APIKey{
				user.ID: {*apiKey},
			},
			availableByUser: map[int64][]service.Group{
				user.ID: {group},
			},
		},
		augmentPluginSubscriptionStub{
			activeByUser: map[int64][]service.UserSubscription{
				user.ID: {
					{
						ID:        1,
						UserID:    user.ID,
						GroupID:   group.ID,
						Status:    service.StatusActive,
						ExpiresAt: expiresAt,
						Group:     &group,
					},
				},
			},
		},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	router := gin.New()
	router.GET("/usage/api/balance", authHandler.AugmentLegacyBalance)
	router.GET("/usage/api/get-models", authHandler.AugmentLegacyModels)
	router.GET("/usage/api/getLoginToken", authHandler.AugmentLegacyLoginToken)
	router.POST("/get-models", authHandler.AugmentLegacyInternalGetModels)
	router.POST("/checkpoint-blobs", authHandler.AugmentLegacyCheckpointBlobs)
	router.POST("/report-error", authHandler.AugmentLegacyNoContent)

	balanceReq := httptest.NewRequest(http.MethodGet, "/usage/api/balance", nil)
	balanceReq.Header.Set("Authorization", "Bearer "+apiKey.Key)
	balanceReq.Header.Set("Origin", "http://127.0.0.1:18082")
	balanceRec := httptest.NewRecorder()
	router.ServeHTTP(balanceRec, balanceReq)
	require.Equal(t, http.StatusOK, balanceRec.Code)

	var balanceBody map[string]any
	require.NoError(t, json.Unmarshal(balanceRec.Body.Bytes(), &balanceBody))
	require.Equal(t, true, balanceBody["success"])
	data, ok := balanceBody["data"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, 40.73, data["remain_amount"])
	require.Equal(t, "admin@zhumeng.local", data["name"])
	loginToken, ok := data["login_token"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "http://127.0.0.1:18082", loginToken["tenantUrl"])
	require.NotEmpty(t, loginToken["accessToken"])
	require.Equal(t, "local_compat", loginToken["sessionSource"])

	modelsReq := httptest.NewRequest(http.MethodGet, "/usage/api/get-models", nil)
	modelsReq.Header.Set("Authorization", "Bearer "+apiKey.Key)
	modelsReq.Header.Set("Origin", "http://127.0.0.1:18082")
	modelsRec := httptest.NewRecorder()
	router.ServeHTTP(modelsRec, modelsReq)
	require.Equal(t, http.StatusOK, modelsRec.Code)

	var modelsBody map[string]map[string]any
	require.NoError(t, json.Unmarshal(modelsRec.Body.Bytes(), &modelsBody))
	require.Contains(t, modelsBody, "gpt-5.4")
	require.Equal(t, true, modelsBody["gpt-5.4"]["isDefault"])

	loginReq := httptest.NewRequest(http.MethodGet, "/usage/api/getLoginToken", nil)
	loginReq.Header.Set("Authorization", "Bearer "+apiKey.Key)
	loginReq.Header.Set("Origin", "http://127.0.0.1:18082")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	require.Equal(t, http.StatusOK, loginRec.Code)

	var loginBody map[string]any
	require.NoError(t, json.Unmarshal(loginRec.Body.Bytes(), &loginBody))
	require.Equal(t, "http://127.0.0.1:18082", loginBody["tenantUrl"])
	require.NotEmpty(t, loginBody["accessToken"])
	require.Equal(t, "local_compat", loginBody["sessionSource"])

	internalReq := httptest.NewRequest(http.MethodPost, "/get-models", bytes.NewReader([]byte(`{}`)))
	internalReq.Header.Set("Authorization", "Bearer "+apiKey.Key)
	internalReq.Header.Set("Origin", "http://127.0.0.1:18082")
	internalRec := httptest.NewRecorder()
	router.ServeHTTP(internalRec, internalReq)
	require.Equal(t, http.StatusOK, internalRec.Code)

	var internalBody map[string]any
	require.NoError(t, json.Unmarshal(internalRec.Body.Bytes(), &internalBody))
	require.Equal(t, "gpt-5.4", internalBody["default_model"])
	models, ok := internalBody["models"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, models)
	require.Contains(t, internalRec.Body.String(), `"name":"gpt-5.4"`)

	checkpointReq := httptest.NewRequest(
		http.MethodPost,
		"/checkpoint-blobs",
		bytes.NewReader([]byte(`{"blobs":{"checkpoint_id":"cp-prev","added_blobs":["blob-b","blob-a"],"deleted_blobs":["blob-z"]}}`)),
	)
	checkpointReq.Header.Set("Authorization", "Bearer "+apiKey.Key)
	checkpointReq.Header.Set("Content-Type", "application/json")
	checkpointRec := httptest.NewRecorder()
	router.ServeHTTP(checkpointRec, checkpointReq)
	require.Equal(t, http.StatusOK, checkpointRec.Code)

	var checkpointBody map[string]any
	require.NoError(t, json.Unmarshal(checkpointRec.Body.Bytes(), &checkpointBody))
	require.NotEmpty(t, checkpointBody["new_checkpoint_id"])

	reportReq := httptest.NewRequest(http.MethodPost, "/report-error", bytes.NewReader([]byte(`{"message":"test"}`)))
	reportReq.Header.Set("Authorization", "Bearer "+apiKey.Key)
	reportRec := httptest.NewRecorder()
	router.ServeHTTP(reportRec, reportReq)
	require.Equal(t, http.StatusNoContent, reportRec.Code)
}

func TestAugmentLegacyModelsPrefersCurrentAPIKeyGroupDefaultModel(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:       1,
		Email:    "admin@zhumeng.local",
		Username: "zhumeng",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	openAIGroupID := int64(3)
	openAIGroup := service.Group{
		ID:                    openAIGroupID,
		Name:                  "openai-default",
		Platform:              service.PlatformOpenAI,
		Status:                service.StatusActive,
		Hydrated:              true,
		AllowMessagesDispatch: false,
		SupportedModelScopes:  []string{"claude", "gemini_text", "gemini_image"},
	}
	apiKey := &service.APIKey{
		ID:        1,
		UserID:    user.ID,
		Key:       "sk-compat-openai",
		Name:      "compat-openai",
		Status:    service.StatusActive,
		GroupID:   &openAIGroupID,
		Group:     &openAIGroup,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	anthropicGroup := service.Group{
		ID:                   2,
		Name:                 "anthropic-default",
		Platform:             service.PlatformAnthropic,
		Status:               service.StatusActive,
		Hydrated:             true,
		SupportedModelScopes: []string{"claude", "gemini_text", "gemini_image"},
	}

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{
				apiKey.Key: apiKey,
			},
			keysByUser: map[int64][]service.APIKey{
				user.ID: {*apiKey},
			},
			availableByUser: map[int64][]service.Group{
				user.ID: {anthropicGroup, openAIGroup},
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	router := gin.New()
	router.GET("/usage/api/get-models", authHandler.AugmentLegacyModels)

	req := httptest.NewRequest(http.MethodGet, "/usage/api/get-models", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey.Key)
	req.Header.Set("Origin", "http://127.0.0.1:18082")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, true, body["gpt-5.4"]["isDefault"])
	require.NotContains(t, body["claude-sonnet-4-5"], "isDefault")
}

func TestAugmentLegacyModelsExposeDefaultAugmentGatewayRegistry(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	authHandler, apiKey := newAugmentModelRegistryTestHandler()
	router := gin.New()
	router.GET("/usage/api/get-models", authHandler.AugmentLegacyModels)

	req := httptest.NewRequest(http.MethodGet, "/usage/api/get-models", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, len(firstBatchAugmentModelIDs()))
	for _, modelID := range firstBatchAugmentModelIDs() {
		entry, ok := body[modelID]
		require.Truef(t, ok, "missing model %s", modelID)
		require.Equal(t, modelID, entry["displayName"])
		require.Contains(t, entry, "priority")
	}
	require.Equal(t, true, body["gpt-5.4"]["isDefault"])
	require.NotContains(t, body, "claude-sonnet-4-5")
	require.NotContains(t, body, "gemini-2.5-pro")
}

func TestAugmentLegacyInternalGetModelsExposeDefaultAugmentGatewayRegistry(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	authHandler, apiKey := newAugmentModelRegistryTestHandler()
	router := gin.New()
	router.POST("/get-models", authHandler.AugmentLegacyInternalGetModels)

	req := httptest.NewRequest(http.MethodPost, "/get-models", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		DefaultModel string                       `json:"default_model"`
		Models       []augmentLegacyInternalModel `json:"models"`
		FeatureFlags map[string]any               `json:"feature_flags"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "gpt-5.4", body.DefaultModel)
	require.Equal(t, firstBatchAugmentModelIDs(), augmentLegacyInternalModelNames(body.Models))
	require.Equal(t, true, body.FeatureFlags["show_thinking_summary"])
	require.Equal(t, true, body.FeatureFlags["showThinkingSummary"])

	modelRegistry := decodeStringMapFeatureFlag(t, body.FeatureFlags, "model_registry")
	require.ElementsMatch(t, firstBatchAugmentModelIDs(), mapValues(modelRegistry))
	require.NotContains(t, mapValues(modelRegistry), "claude-sonnet-4-5")
	require.NotContains(t, mapValues(modelRegistry), "gemini-2.5-pro")

	modelRegistryCamel := decodeStringMapFeatureFlag(t, body.FeatureFlags, "modelRegistry")
	require.Equal(t, modelRegistry, modelRegistryCamel)

	additionalChatModels := decodeStringMapFeatureFlag(t, body.FeatureFlags, "additional_chat_models")
	require.Equal(t, modelRegistry, additionalChatModels)

	modelInfoRegistry := decodeNestedFeatureFlag(t, body.FeatureFlags, "model_info_registry")
	require.Len(t, modelInfoRegistry, len(firstBatchAugmentModelIDs()))
	for index, modelID := range firstBatchAugmentModelIDs() {
		info, ok := modelInfoRegistry[modelID]
		require.Truef(t, ok, "missing model info %s", modelID)
		require.Contains(t, info, "displayName")
		require.Contains(t, info, "shortName")
		require.Equal(t, false, info["disabled"])
		require.Equal(t, float64(index), info["priority"])
		require.NotContains(t, info, "provider")
		require.NotContains(t, info, "protocol")
		require.NotContains(t, info, "upstream_model")
		require.NotContains(t, info, "upstreamModel")
		require.NotContains(t, info, "thinking")
		require.NotContains(t, info, "reasoning_effort")
		require.NotContains(t, info, "reasoningEffort")
		require.NotContains(t, info, "tool_choice")
		require.NotContains(t, info, "toolChoice")
	}
	require.NotContains(t, modelInfoRegistry, "claude-sonnet-4-5")
	require.NotContains(t, modelInfoRegistry, "gemini-2.5-pro")

	modelInfoRegistryCamel := decodeNestedFeatureFlag(t, body.FeatureFlags, "modelInfoRegistry")
	require.Equal(t, modelInfoRegistry, modelInfoRegistryCamel)
}

func TestAugmentLegacyModelsHideClaudeGeminiWhenEnabledWithoutProviderGroups(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	authHandler, apiKey := newAugmentModelRegistryTestHandlerWithAugmentConfig(config.GatewayAugmentConfig{
		Enabled: true,
		EnabledModels: []string{
			"gpt-5.4",
			"gpt-5.5",
			"gpt-5.4-mini",
			"deepseek-v4-pro",
			"deepseek-v4-flash",
			"claude-sonnet-4-5",
			"gemini-2.5-pro",
		},
		ProviderGroups: config.GatewayAugmentProviderGroupsConfig{
			OpenAI:   1001,
			DeepSeek: 1002,
		},
	})

	router := gin.New()
	router.GET("/usage/api/get-models", authHandler.AugmentLegacyModels)
	router.POST("/get-models", authHandler.AugmentLegacyInternalGetModels)

	req := httptest.NewRequest(http.MethodGet, "/usage/api/get-models", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.NotContains(t, body, "claude-sonnet-4-5")
	require.NotContains(t, body, "gemini-2.5-pro")

	postReq := httptest.NewRequest(http.MethodPost, "/get-models", bytes.NewReader([]byte(`{}`)))
	postReq.Header.Set("Authorization", "Bearer "+apiKey)
	postRec := httptest.NewRecorder()
	router.ServeHTTP(postRec, postReq)
	require.Equal(t, http.StatusOK, postRec.Code)

	var postBody struct {
		Models []augmentLegacyInternalModel `json:"models"`
	}
	require.NoError(t, json.Unmarshal(postRec.Body.Bytes(), &postBody))
	require.Equal(t, []string{
		"gpt-5.4",
		"gpt-5.5",
		"gpt-5.4-mini",
		"deepseek-v4-pro",
		"deepseek-v4-flash",
	}, augmentLegacyInternalModelNames(postBody.Models))
}

func TestAugmentLegacyModelsExposeClaudeGeminiWhenEnabledAndConfigured(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	authHandler, apiKey := newAugmentModelRegistryTestHandlerWithAugmentConfig(config.GatewayAugmentConfig{
		Enabled: true,
		EnabledModels: []string{
			"gpt-5.4",
			"gpt-5.5",
			"gpt-5.4-mini",
			"deepseek-v4-pro",
			"deepseek-v4-flash",
			"claude-sonnet-4-5",
			"gemini-2.5-pro",
		},
		ProviderGroups: config.GatewayAugmentProviderGroupsConfig{
			OpenAI:    1001,
			DeepSeek:  1002,
			Anthropic: 2001,
			Gemini:    2002,
		},
	})

	router := gin.New()
	router.GET("/usage/api/get-models", authHandler.AugmentLegacyModels)
	router.POST("/get-models", authHandler.AugmentLegacyInternalGetModels)

	req := httptest.NewRequest(http.MethodGet, "/usage/api/get-models", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Contains(t, body, "claude-sonnet-4-5")
	require.Contains(t, body, "gemini-2.5-pro")

	postReq := httptest.NewRequest(http.MethodPost, "/get-models", bytes.NewReader([]byte(`{}`)))
	postReq.Header.Set("Authorization", "Bearer "+apiKey)
	postRec := httptest.NewRecorder()
	router.ServeHTTP(postRec, postReq)
	require.Equal(t, http.StatusOK, postRec.Code)

	var postBody struct {
		Models []augmentLegacyInternalModel `json:"models"`
	}
	require.NoError(t, json.Unmarshal(postRec.Body.Bytes(), &postBody))
	require.Equal(t, []string{
		"gpt-5.4",
		"gpt-5.5",
		"gpt-5.4-mini",
		"deepseek-v4-pro",
		"deepseek-v4-flash",
		"claude-sonnet-4-5",
		"gemini-2.5-pro",
	}, augmentLegacyInternalModelNames(postBody.Models))
}

func newAugmentModelRegistryTestHandler() (*AuthHandler, string) {
	return newAugmentModelRegistryTestHandlerWithAugmentConfig(config.GatewayAugmentConfig{
		Enabled:       true,
		EnabledModels: firstBatchAugmentModelIDs(),
		ProviderGroups: config.GatewayAugmentProviderGroupsConfig{
			OpenAI:   1001,
			DeepSeek: 1002,
		},
	})
}

func newAugmentModelRegistryTestHandlerWithAugmentConfig(augmentCfg config.GatewayAugmentConfig) (*AuthHandler, string) {
	user := &service.User{
		ID:       42,
		Email:    "models@example.com",
		Username: "models",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	apiKey := &service.APIKey{
		ID:        42,
		UserID:    user.ID,
		Key:       "sk-augment-model-registry",
		Name:      "augment-model-registry",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	cfg := &config.Config{
		Server:  config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"},
		Gateway: config.GatewayConfig{Augment: augmentCfg},
	}
	pluginService := service.NewAugmentPluginService(
		cfg,
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{apiKey.Key: apiKey},
			keysByUser:    map[int64][]service.APIKey{user.ID: {*apiKey}},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)
	return NewAuthHandler(cfg, nil, nil, nil, nil, nil, nil, pluginService), apiKey.Key
}

func firstBatchAugmentModelIDs() []string {
	return []string{
		"gpt-5.4",
		"gpt-5.5",
		"gpt-5.4-mini",
		"deepseek-v4-pro",
		"deepseek-v4-flash",
	}
}

func augmentLegacyInternalModelNames(models []augmentLegacyInternalModel) []string {
	names := make([]string, 0, len(models))
	for _, model := range models {
		names = append(names, model.Name)
	}
	return names
}

func decodeStringMapFeatureFlag(t *testing.T, flags map[string]any, key string) map[string]string {
	t.Helper()
	raw, ok := flags[key].(string)
	require.Truef(t, ok, "feature flag %s must be a JSON string", key)
	var decoded map[string]string
	require.NoError(t, json.Unmarshal([]byte(raw), &decoded))
	return decoded
}

func decodeNestedFeatureFlag(t *testing.T, flags map[string]any, key string) map[string]map[string]any {
	t.Helper()
	raw, ok := flags[key].(string)
	require.Truef(t, ok, "feature flag %s must be a JSON string", key)
	var decoded map[string]map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &decoded))
	return decoded
}

func mapValues(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
