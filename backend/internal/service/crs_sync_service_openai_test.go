//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type crsSyncOpenAIRepoStub struct {
	mockAccountRepoForGemini
	created []*Account
	updated []*Account
	byCRSID map[string]*Account
}

func (r *crsSyncOpenAIRepoStub) Create(ctx context.Context, account *Account) error {
	cloned := *account
	r.created = append(r.created, &cloned)
	return nil
}

func (r *crsSyncOpenAIRepoStub) Update(ctx context.Context, account *Account) error {
	cloned := *account
	r.updated = append(r.updated, &cloned)
	return nil
}

func (r *crsSyncOpenAIRepoStub) GetByCRSAccountID(ctx context.Context, crsAccountID string) (*Account, error) {
	if r.byCRSID == nil {
		return nil, nil
	}
	return r.byCRSID[crsAccountID], nil
}

func (r *crsSyncOpenAIRepoStub) ListByPlatform(ctx context.Context, platform string) ([]Account, error) {
	if platform != PlatformOpenAI {
		return nil, nil
	}
	return append([]Account(nil), r.accounts...), nil
}

type crsSyncProxyRepoStub struct{}

func (r *crsSyncProxyRepoStub) Create(ctx context.Context, proxy *Proxy) error { return nil }
func (r *crsSyncProxyRepoStub) GetByID(ctx context.Context, id int64) (*Proxy, error) {
	return nil, nil
}
func (r *crsSyncProxyRepoStub) ListByIDs(ctx context.Context, ids []int64) ([]Proxy, error) {
	return nil, nil
}
func (r *crsSyncProxyRepoStub) Update(ctx context.Context, proxy *Proxy) error { return nil }
func (r *crsSyncProxyRepoStub) Delete(ctx context.Context, id int64) error     { return nil }
func (r *crsSyncProxyRepoStub) List(ctx context.Context, params pagination.PaginationParams) ([]Proxy, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *crsSyncProxyRepoStub) ListWithFilters(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]Proxy, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *crsSyncProxyRepoStub) ListWithFiltersAndAccountCount(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]ProxyWithAccountCount, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *crsSyncProxyRepoStub) ListActive(ctx context.Context) ([]Proxy, error) { return nil, nil }
func (r *crsSyncProxyRepoStub) ListActiveWithAccountCount(ctx context.Context) ([]ProxyWithAccountCount, error) {
	return nil, nil
}
func (r *crsSyncProxyRepoStub) ExistsByHostPortAuth(ctx context.Context, host string, port int, username, password string) (bool, error) {
	return false, nil
}
func (r *crsSyncProxyRepoStub) CountAccountsByProxyID(ctx context.Context, proxyID int64) (int64, error) {
	return 0, nil
}
func (r *crsSyncProxyRepoStub) ListAccountSummariesByProxyID(ctx context.Context, proxyID int64) ([]ProxyAccountSummary, error) {
	return nil, nil
}

type crsSyncOpenAIClientStub struct {
	refreshErr error
}

func (s *crsSyncOpenAIClientStub) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*openai.TokenResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *crsSyncOpenAIClientStub) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *crsSyncOpenAIClientStub) RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error) {
	return nil, s.refreshErr
}

func TestCRSSyncService_OpenAIStaleRTDoesNotOverrideNewerManagedAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/web/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"token":   "admin-token",
			})
		case "/admin/sync/export-accounts":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"exportedAt":              "2026-04-16T00:00:00Z",
					"claudeAccounts":          []any{},
					"claudeConsoleAccounts":   []any{},
					"openaiResponsesAccounts": []any{},
					"geminiOAuthAccounts":     []any{},
					"geminiApiKeyAccounts":    []any{},
					"openaiOAuthAccounts": []map[string]any{
						{
							"kind":        "openai-oauth",
							"id":          "crs-openai-1",
							"name":        "CRS OpenAI",
							"platform":    PlatformOpenAI,
							"authType":    AccountTypeOAuth,
							"isActive":    true,
							"schedulable": true,
							"priority":    50,
							"status":      StatusActive,
							"credentials": map[string]any{
								"access_token":       "stale-at",
								"refresh_token":      "stale-rt",
								"chatgpt_account_id": "acct-1",
							},
							"extra": map[string]any{
								"crs_email": "user@example.com",
							},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	repo := &crsSyncOpenAIRepoStub{
		mockAccountRepoForGemini: mockAccountRepoForGemini{
			accounts: []Account{
				{
					ID:       99,
					Name:     "Local OpenAI",
					Platform: PlatformOpenAI,
					Type:     AccountTypeOAuth,
					Status:   StatusActive,
					Extra: map[string]any{
						"openai_pool_role":    OpenAIPoolRoleMain,
						"openai_auth_state":   OpenAIAuthStateHealthy,
						"openai_token_source": OpenAITokenSourceRTManaged,
					},
					Credentials: map[string]any{
						"access_token":       "fresh-at",
						"refresh_token":      "fresh-rt",
						"chatgpt_account_id": "acct-1",
					},
				},
			},
		},
	}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true

	svc := NewCRSSyncService(
		repo,
		&crsSyncProxyRepoStub{},
		nil,
		NewOpenAIOAuthService(nil, &crsSyncOpenAIClientStub{refreshErr: errors.New("token refresh failed: status 400, body: {\"error\":\"refresh_token_expired\"}")}),
		nil,
		cfg,
	)

	result, err := svc.SyncFromCRS(context.Background(), SyncFromCRSInput{
		BaseURL:     server.URL,
		Username:    "admin",
		Password:    "pass",
		SyncProxies: false,
	})

	require.NoError(t, err)
	require.Equal(t, 0, result.Created)
	require.Equal(t, 0, result.Updated)
	require.Equal(t, 1, result.Failed)
	require.Len(t, repo.created, 0)
	require.Len(t, repo.updated, 0)
	require.Len(t, result.Items, 1)
	require.Equal(t, OpenAIValidationOutcomeRTValidationTerminalFailure, result.Items[0].ValidationOutcome)
	require.Equal(t, OpenAITokenSourceRTManaged, result.Items[0].TokenSource)
}
