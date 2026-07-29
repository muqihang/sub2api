package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAccountTestServiceWebSearchProbePersistsSemanticSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := Account{
		ID: 210, Name: "web-search", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://example.com/v1"},
	}
	updates := make(chan map[string]any, 1)
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updates,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"output":[{"type":"web_search_call"},{"type":"message"}]}`)),
	}}
	svc := &AccountTestService{
		accountRepo: repo, httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/210/test", bytes.NewReader(nil))

	err := svc.TestAccountConnection(c, account.ID, "gpt-5.6-sol", "", AccountTestModeWebSearch)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/v1/responses", upstream.lastReq.URL.String())
	require.Equal(t, "web_search", gjson.GetBytes(upstream.lastBody, "tools.0.type").String())
	require.Equal(t, "required", gjson.GetBytes(upstream.lastBody, "tool_choice").String())
	require.Equal(t, true, (<-updates)["openai_web_search_supported"])
	require.Contains(t, rec.Body.String(), `"type":"test_complete"`)
}

func TestAccountTestServiceWebSearchProbeRejectsIgnoredTool(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := Account{
		ID: 37, Name: "ignores-search", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://example.com/v1"},
	}
	updates := make(chan map[string]any, 1)
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updates,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"output":[{"type":"message"}]}`)),
	}}
	svc := &AccountTestService{
		accountRepo: repo, httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/37/test", bytes.NewReader(nil))

	err := svc.TestAccountConnection(c, account.ID, "gpt-5.6-sol", "", AccountTestModeWebSearch)
	require.Error(t, err)
	probeUpdates := <-updates
	require.Equal(t, false, probeUpdates["openai_web_search_supported"])
	require.Contains(t, probeUpdates["openai_web_search_last_error"], "web_search_call")
	require.Contains(t, rec.Body.String(), `"type":"error"`)
}

func TestAccountTestServiceBackgroundWebSearchProbePersistsScopedEvidence(t *testing.T) {
	account := Account{
		ID: 251, Name: "auto-web-search", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://example.com/v1",
			"model_mapping": map[string]any{
				"gpt-5.5": "upstream-model",
			},
		},
	}
	updates := make(chan map[string]any, 1)
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updates,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"output":[{"type":"web_search_call"},{"type":"message"}]}`)),
	}}
	svc := &AccountTestService{
		accountRepo: repo, httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}

	svc.ProbeOpenAIWebSearchSupportForModel(context.Background(), account.ID, "gpt-5.5")

	probeUpdates := <-updates
	require.Equal(t, true, probeUpdates["openai_web_search_supported"])
	require.Equal(t, "upstream-model", probeUpdates["openai_web_search_probe_model"])
	require.Equal(t, account.OpenAIWebSearchTargetFingerprint(), probeUpdates["openai_web_search_probe_target"])
}
