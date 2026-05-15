package routes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexGatewayRoutesServiceStub struct {
	modelsResp    *service.CodexGatewayServiceResponse
	responsesResp *service.CodexGatewayServiceResponse
}

func (s *codexGatewayRoutesServiceStub) Models(_ context.Context, _ service.CodexGatewayModelsRequest) (*service.CodexGatewayServiceResponse, error) {
	return s.modelsResp, nil
}

func (s *codexGatewayRoutesServiceStub) Responses(_ context.Context, _ service.CodexGatewayResponsesRequest) (*service.CodexGatewayServiceResponse, error) {
	return s.responsesResp, nil
}

func TestCodexGatewayRoutes_SurfaceRegistration(t *testing.T) {
	router := newCodexGatewayRoutesTestRouter(
		&config.Config{Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, Codex: config.GatewayCodexConfig{Enabled: true}}},
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			groupID := int64(1)
			product := service.CodexUsageClientProduct
			c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
				ID:                      99,
				Key:                     "sk-test",
				Status:                  service.StatusActive,
				GroupID:                 &groupID,
				RestrictedClientProduct: &product,
				Group:                   &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, CodexGatewayEntitled: true},
				User:                    &service.User{ID: 7, Status: service.StatusActive, Role: service.RoleUser, Balance: 1, Concurrency: 1},
			})
			c.Next()
		}),
		&codexGatewayRoutesServiceStub{
			modelsResp: &service.CodexGatewayServiceResponse{
				StatusCode: http.StatusOK,
				Headers:    http.Header{"Content-Type": []string{"application/json"}},
				Body:       []byte(`{"models":[{"slug":"gpt-5.5"}]}`),
			},
			responsesResp: &service.CodexGatewayServiceResponse{
				StatusCode: http.StatusCreated,
				Headers:    http.Header{"Content-Type": []string{"application/json"}},
				Body:       []byte(`{"id":"resp_123"}`),
			},
		},
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/codex/v1/models", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/codex/v1/responses", strings.NewReader(`{"model":"gpt-5.5"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/codex/v1/responses", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/codex/v1/responses/compact", strings.NewReader(`{"model":"gpt-5.5"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestCodexGatewayRoutes_ModelsRegisterOnlyWhenEnabled(t *testing.T) {
	router := newCodexGatewayRoutesTestRouter(
		&config.Config{Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, Codex: config.GatewayCodexConfig{Enabled: false}}},
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) { c.Next() }),
		&codexGatewayRoutesServiceStub{},
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/codex/v1/models", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestCodexGatewayRoutes_AuthErrorsUseResponsesEnvelope(t *testing.T) {
	now := time.Now()
	expiredAt := now.Add(-time.Hour)

	cases := []struct {
		name       string
		apiKey     *service.APIKey
		wantStatus int
		wantCode   string
	}{
		{name: "missing", apiKey: nil, wantStatus: http.StatusUnauthorized, wantCode: "api_key_required"},
		{name: "invalid", apiKey: nil, wantStatus: http.StatusUnauthorized, wantCode: "invalid_api_key"},
		{name: "disabled", apiKey: &service.APIKey{ID: 1, Key: "disabled", Status: service.StatusAPIKeyDisabled, User: &service.User{ID: 7, Status: service.StatusActive, Role: service.RoleUser, Balance: 1}}, wantStatus: http.StatusUnauthorized, wantCode: "api_key_disabled"},
		{name: "expired", apiKey: &service.APIKey{ID: 1, Key: "expired", Status: service.StatusAPIKeyExpired, ExpiresAt: &expiredAt, User: &service.User{ID: 7, Status: service.StatusActive, Role: service.RoleUser, Balance: 1}}, wantStatus: http.StatusForbidden, wantCode: "api_key_expired"},
		{name: "quota_exhausted", apiKey: &service.APIKey{ID: 1, Key: "quota", Status: service.StatusAPIKeyQuotaExhausted, User: &service.User{ID: 7, Status: service.StatusActive, Role: service.RoleUser, Balance: 1}}, wantStatus: http.StatusTooManyRequests, wantCode: "api_key_quota_exhausted"},
		{name: "generic_active_key", apiKey: &service.APIKey{ID: 1, Key: "generic", Status: service.StatusActive, GroupID: testInt64Ptr(9), Group: &service.Group{ID: 9, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, CodexGatewayEntitled: true}, User: &service.User{ID: 7, Status: service.StatusActive, Role: service.RoleUser, Balance: 1, Concurrency: 1}}, wantStatus: http.StatusForbidden, wantCode: "invalid_api_key"},
	}

	repo := &codexGatewayRoutesAPIKeyRepo{
		keys: map[string]*service.APIKey{
			"disabled": cases[2].apiKey,
			"expired":  cases[3].apiKey,
			"quota":    cases[4].apiKey,
			"generic":  cases[5].apiKey,
			"valid": {
				ID:     2,
				Key:    "valid",
				Status: service.StatusActive,
				User:   &service.User{ID: 7, Status: service.StatusActive, Role: service.RoleUser, Balance: 1, Concurrency: 1},
			},
		},
	}
	cfg := &config.Config{RunMode: config.RunModeStandard, Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, Codex: config.GatewayCodexConfig{Enabled: true}}}
	apiKeyService := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
	auth := servermiddleware.NewCodexGatewayAPIKeyAuthMiddleware(apiKeyService, nil, cfg)

	router := newCodexGatewayRoutesTestRouter(
		cfg,
		auth,
		&codexGatewayRoutesServiceStub{
			modelsResp: &service.CodexGatewayServiceResponse{StatusCode: http.StatusOK, Body: []byte(`{"models":[]}`)},
			responsesResp: &service.CodexGatewayServiceResponse{StatusCode: http.StatusOK, Body: []byte(`{"id":"resp_123"}`)},
		},
	)

	paths := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/codex/v1/models"},
		{method: http.MethodPost, path: "/codex/v1/responses"},
	}

	for _, tc := range cases {
		for _, pathCase := range paths {
			t.Run(tc.name+" "+pathCase.path, func(t *testing.T) {
				var body io.Reader
				if pathCase.method == http.MethodPost {
					body = strings.NewReader(`{"model":"gpt-5.5"}`)
				}
				req := httptest.NewRequest(pathCase.method, pathCase.path, body)
				if pathCase.method == http.MethodPost {
					req.Header.Set("Content-Type", "application/json")
				}
				switch tc.name {
				case "missing":
				case "invalid":
					req.Header.Set("Authorization", "Bearer missing")
				default:
					req.Header.Set("Authorization", "Bearer "+tc.apiKey.Key)
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				require.Equal(t, tc.wantStatus, w.Code)
				require.NotContains(t, w.Body.String(), `"code":"INVALID_API_KEY"`)
				require.Contains(t, w.Body.String(), `"type":"`)
				require.Contains(t, w.Body.String(), `"code":"`+tc.wantCode+`"`)
				require.Contains(t, w.Body.String(), `"message":`)
			})
		}
	}
}

func TestCodexGatewayRoutes_QueryAPIKeyErrorUsesInvalidRequestError(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeStandard, Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, Codex: config.GatewayCodexConfig{Enabled: true}}}
	apiKeyService := service.NewAPIKeyService(&codexGatewayRoutesAPIKeyRepo{keys: map[string]*service.APIKey{}}, nil, nil, nil, nil, nil, cfg)

	router := newCodexGatewayRoutesTestRouter(
		cfg,
		servermiddleware.NewCodexGatewayAPIKeyAuthMiddleware(apiKeyService, nil, cfg),
		&codexGatewayRoutesServiceStub{
			modelsResp: &service.CodexGatewayServiceResponse{StatusCode: http.StatusOK, Body: []byte(`{"models":[]}`)},
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/codex/v1/models?api_key=legacy", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), `"type":"invalid_request_error"`)
	require.Contains(t, w.Body.String(), `"code":"api_key_in_query_deprecated"`)
}

func newCodexGatewayRoutesTestRouter(cfg *config.Config, apiKeyAuth servermiddleware.APIKeyAuthMiddleware, svc *codexGatewayRoutesServiceStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	RegisterCodexGatewayRoutes(
		router,
		&handler.Handlers{
			CodexGateway: handler.NewCodexGatewayHandler(svc),
		},
		apiKeyAuth,
		nil,
		nil,
		cfg,
	)

	return router
}

type codexGatewayRoutesAPIKeyRepo struct {
	keys map[string]*service.APIKey
}

func (r *codexGatewayRoutesAPIKeyRepo) Create(context.Context, *service.APIKey) error { return nil }
func (r *codexGatewayRoutesAPIKeyRepo) GetByID(context.Context, int64) (*service.APIKey, error) {
	return nil, service.ErrAPIKeyNotFound
}
func (r *codexGatewayRoutesAPIKeyRepo) GetKeyAndOwnerID(context.Context, int64) (string, int64, error) {
	return "", 0, service.ErrAPIKeyNotFound
}
func (r *codexGatewayRoutesAPIKeyRepo) GetByKey(_ context.Context, key string) (*service.APIKey, error) {
	apiKey, ok := r.keys[key]
	if !ok {
		return nil, service.ErrAPIKeyNotFound
	}
	clone := *apiKey
	return &clone, nil
}
func (r *codexGatewayRoutesAPIKeyRepo) GetByKeyForAuth(ctx context.Context, key string) (*service.APIKey, error) {
	return r.GetByKey(ctx, key)
}
func (r *codexGatewayRoutesAPIKeyRepo) Update(context.Context, *service.APIKey) error { return nil }
func (r *codexGatewayRoutesAPIKeyRepo) Delete(context.Context, int64) error          { return nil }
func (r *codexGatewayRoutesAPIKeyRepo) ListByUserID(context.Context, int64, pagination.PaginationParams, service.APIKeyListFilters) ([]service.APIKey, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}
func (r *codexGatewayRoutesAPIKeyRepo) VerifyOwnership(context.Context, int64, []int64) ([]int64, error) {
	return nil, nil
}
func (r *codexGatewayRoutesAPIKeyRepo) CountByUserID(context.Context, int64) (int64, error) {
	return 0, nil
}
func (r *codexGatewayRoutesAPIKeyRepo) ExistsByKey(context.Context, string) (bool, error) {
	return false, nil
}
func (r *codexGatewayRoutesAPIKeyRepo) ListByGroupID(context.Context, int64, pagination.PaginationParams) ([]service.APIKey, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}
func (r *codexGatewayRoutesAPIKeyRepo) SearchAPIKeys(context.Context, int64, string, int) ([]service.APIKey, error) {
	return nil, nil
}
func (r *codexGatewayRoutesAPIKeyRepo) ClearGroupIDByGroupID(context.Context, int64) (int64, error) {
	return 0, nil
}
func (r *codexGatewayRoutesAPIKeyRepo) UpdateGroupIDByUserAndGroup(context.Context, int64, int64, int64) (int64, error) {
	return 0, nil
}
func (r *codexGatewayRoutesAPIKeyRepo) CountByGroupID(context.Context, int64) (int64, error) {
	return 0, nil
}
func (r *codexGatewayRoutesAPIKeyRepo) ListKeysByUserID(context.Context, int64) ([]string, error) {
	return nil, nil
}
func (r *codexGatewayRoutesAPIKeyRepo) ListKeysByGroupID(context.Context, int64) ([]string, error) {
	return nil, nil
}
func (r *codexGatewayRoutesAPIKeyRepo) IncrementQuotaUsed(context.Context, int64, float64) (float64, error) {
	return 0, nil
}
func (r *codexGatewayRoutesAPIKeyRepo) UpdateLastUsed(context.Context, int64, time.Time) error { return nil }
func (r *codexGatewayRoutesAPIKeyRepo) IncrementRateLimitUsage(context.Context, int64, float64) error {
	return nil
}
func (r *codexGatewayRoutesAPIKeyRepo) ResetRateLimitWindows(context.Context, int64) error { return nil }
func (r *codexGatewayRoutesAPIKeyRepo) GetRateLimitData(context.Context, int64) (*service.APIKeyRateLimitData, error) {
	return &service.APIKeyRateLimitData{}, nil
}
