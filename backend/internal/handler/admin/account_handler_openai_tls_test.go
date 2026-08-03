package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupOpenAIAccountTLSUpdateRouter(adminSvc *stubAdminService, core *service.OpenAIGatewayCoreService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	openAISvc := service.NewOpenAIOAuthService(nil, nil)
	openAISvc.SetGatewayCoreService(core)
	handler := NewAccountHandler(adminSvc, nil, openAISvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.POST("/api/v1/admin/accounts", handler.Create)
	router.PUT("/api/v1/admin/accounts/:id", handler.Update)
	return router
}

func TestAccountHandler_UpdateAcceptsOpenAIGatewayTLSPolicy(t *testing.T) {
	adminSvc := newStubAdminService()
	adminSvc.accounts = []service.Account{
		{
			ID:       3,
			Name:     "openai-acc",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Extra: map[string]any{
				"openai_gateway_egress_bucket": "default",
			},
		},
	}
	core := testOpenAIAccountTLSGatewayCore(true, 7)
	router := setupOpenAIAccountTLSUpdateRouter(adminSvc, core)

	body := map[string]any{
		"name": "openai-acc",
		"openai_gateway_tls": map[string]any{
			"enabled":    true,
			"profile_id": 7,
		},
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/accounts/3", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, adminSvc.updatedAccounts, 1)
	require.NotNil(t, adminSvc.updatedAccounts[0].input.OpenAIGatewayTLS)
	require.Equal(t, int64(7), adminSvc.updatedAccounts[0].input.OpenAIGatewayTLS.ProfileID)
	require.Equal(t, map[string]any{
		"enabled":    true,
		"profile_id": int64(7),
	}, adminSvc.updatedAccounts[0].input.Extra["openai_gateway_tls"])
}

func TestAccountHandler_UpdateRejectsInvalidOpenAIGatewayTLSPolicy(t *testing.T) {
	adminSvc := newStubAdminService()
	adminSvc.accounts = []service.Account{
		{
			ID:       3,
			Name:     "openai-acc",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Extra: map[string]any{
				"openai_gateway_egress_bucket": "default",
			},
		},
	}
	core := testOpenAIAccountTLSGatewayCore(true, 7)
	router := setupOpenAIAccountTLSUpdateRouter(adminSvc, core)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/accounts/3", bytes.NewReader([]byte(`{"openai_gateway_tls":{"enabled":true}}`)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, adminSvc.updatedAccounts)
}

func TestAccountHandler_UpdateRejectsRawOpenAIGatewayTLSInExtra(t *testing.T) {
	adminSvc := newStubAdminService()
	adminSvc.accounts = []service.Account{
		{
			ID:       3,
			Name:     "openai-acc",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Extra: map[string]any{
				"openai_gateway_egress_bucket": "default",
			},
		},
	}
	core := testOpenAIAccountTLSGatewayCore(true, 7)
	router := setupOpenAIAccountTLSUpdateRouter(adminSvc, core)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/accounts/3", bytes.NewReader([]byte(`{"extra":{"openai_gateway_egress_bucket":"default","openai_gateway_tls":{"enabled":true,"profile_id":404}}}`)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, adminSvc.updatedAccounts)
}

func TestAccountHandler_CreateRejectsRawOpenAIGatewayTLSInExtra(t *testing.T) {
	adminSvc := newStubAdminService()
	router := setupOpenAIAccountTLSUpdateRouter(adminSvc, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts", bytes.NewReader([]byte(`{
		"name":"openai-acc",
		"platform":"openai",
		"type":"oauth",
		"credentials":{"access_token":"tok"},
		"extra":{"openai_gateway_tls":{"enabled":true,"profile_id":404}}
	}`)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, adminSvc.createdAccounts)
}

func TestAccountHandler_UpdateRejectsTypedOpenAIGatewayTLSWhenValidationContextMissing(t *testing.T) {
	adminSvc := newStubAdminService()
	adminSvc.accounts = []service.Account{
		{
			ID:       3,
			Name:     "openai-acc",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Extra: map[string]any{
				"openai_gateway_egress_bucket": "default",
			},
		},
	}
	router := setupOpenAIAccountTLSUpdateRouter(adminSvc, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/accounts/3", bytes.NewReader([]byte(`{"openai_gateway_tls":{"enabled":true,"profile_id":7}}`)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, adminSvc.updatedAccounts)
}

func testOpenAIAccountTLSGatewayCore(allowOverride bool, profileIDs ...int64) *service.OpenAIGatewayCoreService {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = false
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{
			Name:    "default",
			Enabled: true,
			TLS: config.OpenAIGatewayBucketTLSConfig{
				Enabled:              true,
				ProfileID:            1,
				AllowAccountOverride: allowOverride,
			},
		},
	}
	profiles := make([]*model.TLSFingerprintProfile, 0, len(profileIDs))
	for _, id := range profileIDs {
		profiles = append(profiles, &model.TLSFingerprintProfile{ID: id, Name: "profile"})
	}
	return service.NewOpenAIGatewayCoreService(&openAIGatewayCoreAdminRepo{}, cfg, nil, service.NewTLSFingerprintProfileService(&accountHandlerTLSProfileRepo{profiles: profiles}, nil))
}

type accountHandlerTLSProfileRepo struct {
	profiles []*model.TLSFingerprintProfile
}

func (r *accountHandlerTLSProfileRepo) List(ctx context.Context) ([]*model.TLSFingerprintProfile, error) {
	return r.profiles, nil
}

func (r *accountHandlerTLSProfileRepo) GetByID(ctx context.Context, id int64) (*model.TLSFingerprintProfile, error) {
	for _, profile := range r.profiles {
		if profile != nil && profile.ID == id {
			return profile, nil
		}
	}
	return nil, nil
}

func (r *accountHandlerTLSProfileRepo) Create(ctx context.Context, profile *model.TLSFingerprintProfile) (*model.TLSFingerprintProfile, error) {
	return profile, nil
}

func (r *accountHandlerTLSProfileRepo) Update(ctx context.Context, profile *model.TLSFingerprintProfile) (*model.TLSFingerprintProfile, error) {
	return profile, nil
}

func (r *accountHandlerTLSProfileRepo) Delete(ctx context.Context, id int64) error {
	return nil
}
