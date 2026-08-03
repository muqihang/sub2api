package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountHandlerCreate_GeminiProtectsCredentialsBeforePersist(t *testing.T) {
	gin.SetMode(gin.TestMode)

	adminSvc := newStubAdminService()
	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("67", 32)
	handler := NewAccountHandler(adminSvc, nil, nil, service.NewGeminiOAuthService(nil, nil, nil, nil, cfg), nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/accounts", strings.NewReader(`{"name":"g","platform":"gemini","type":"oauth","credentials":{"refresh_token":"rt","oauth_type":"google_one"},"concurrency":1,"priority":1}`))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Create(c)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, adminSvc.createdAccounts, 1)
	require.True(t, strings.HasPrefix(adminSvc.createdAccounts[0].Credentials["refresh_token"].(string), "genc:v1:"))
}

func TestAccountHandlerUpdate_GeminiProtectsCredentialsBeforePersist(t *testing.T) {
	gin.SetMode(gin.TestMode)

	adminSvc := newStubAdminService()
	adminSvc.accounts = []service.Account{{
		ID:       12,
		Name:     "g",
		Platform: service.PlatformGemini,
		Type:     service.AccountTypeOAuth,
	}}
	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("68", 32)
	handler := NewAccountHandler(adminSvc, nil, nil, service.NewGeminiOAuthService(nil, nil, nil, nil, cfg), nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: "12"}}
	c.Request = httptest.NewRequest(http.MethodPut, "/admin/accounts/12", strings.NewReader(`{"credentials":{"refresh_token":"rt","oauth_type":"google_one"}}`))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Update(c)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, adminSvc.updatedAccounts, 1)
	require.True(t, strings.HasPrefix(adminSvc.updatedAccounts[0].input.Credentials["refresh_token"].(string), "genc:v1:"))
}
