package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexModelsMixedGroupAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r codexModelsMixedGroupAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for index := range r.accounts {
		if r.accounts[index].ID == id {
			return &r.accounts[index], nil
		}
	}
	return nil, service.ErrAccountNotFound
}

func (r codexModelsMixedGroupAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]service.Account, error) {
	accounts := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

type codexModelsMixedGroupUpstream struct {
	calls         int
	authorization string
}

func (u *codexModelsMixedGroupUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	u.authorization = request.Header.Get("Authorization")
	return u.response(), nil
}

func (u *codexModelsMixedGroupUpstream) DoWithTLS(request *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(request, "", 0, 0)
}

func (u *codexModelsMixedGroupUpstream) response() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(`{"models":[]}`)),
	}
}

func TestOpenAIGatewayHandlerCodexModelsSkipsPreferredAPIKeyForOAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(24)
	upstream := &codexModelsMixedGroupUpstream{}
	gatewayService := service.NewOpenAIGatewayService(
		codexModelsMixedGroupAccountRepo{accounts: []service.Account{
			{
				ID:          1,
				Platform:    service.PlatformOpenAI,
				Type:        service.AccountTypeAPIKey,
				Status:      service.StatusActive,
				Schedulable: true,
				Priority:    0,
				Credentials: map[string]any{
					"api_key": "test-api-key",
				},
			},
			{
				ID:          2,
				Platform:    service.PlatformOpenAI,
				Type:        service.AccountTypeOAuth,
				Status:      service.StatusActive,
				Schedulable: true,
				Priority:    1,
				Credentials: map[string]any{
					"access_token": "test-oauth-token",
				},
			},
		}},
		nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, upstream, nil, nil,
	)
	handler := NewOpenAIGatewayHandler(gatewayService, nil)

	response := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(response)
	requestContext.Request = httptest.NewRequest(http.MethodGet, "/v1/models?client_version=0.150.0", nil)
	requestContext.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		GroupID: &groupID,
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformOpenAI,
		},
	})

	handler.CodexModels(requestContext)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, `{"models":[]}`, response.Body.String())
	require.Equal(t, 1, upstream.calls)
	require.Equal(t, "Bearer test-oauth-token", upstream.authorization)
}
