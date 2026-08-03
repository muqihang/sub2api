package service

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIAPIKeyRequestBuilderAppliesSafeHeaderOverrides(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(`{"model":"gpt-5"}`)))
	c.Request.Header.Set("User-Agent", "client-agent")

	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":                    "sk-real",
			credKeyHeaderOverrideEnabled: true,
			credKeyHeaderOverrides: map[string]any{
				"User-Agent":  "override-agent/1.0",
				"OpenAI-Beta": "responses=v1",
			},
		},
	}

	req, err := svc.buildUpstreamRequest(c.Request.Context(), c, account, []byte(`{"model":"gpt-5"}`), "sk-real", false, "", false)
	require.NoError(t, err)
	require.Equal(t, "Bearer sk-real", req.Header.Get("Authorization"))
	require.Equal(t, "override-agent/1.0", req.Header.Get("User-Agent"))
	require.Equal(t, "responses=v1", getHeaderRaw(req.Header, "openai-beta"))
}

func TestAnthropicAPIKeyRequestBuilderAppliesHeaderOverridesAfterDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"claude-3-5-sonnet-20241022","messages":[]}`)))
	c.Request.Header.Set("Anthropic-Beta", "client-beta")

	svc := &GatewayService{
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}
	account := &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":                    "anthropic-real",
			credKeyHeaderOverrideEnabled: true,
			credKeyHeaderOverrides: map[string]any{
				"anthropic-beta": "override-beta",
				"user-agent":     "override-claude/1.0",
			},
		},
	}

	req, _, err := svc.buildUpstreamRequest(
		context.Background(), c, account, []byte(`{"model":"claude-3-5-sonnet-20241022","messages":[]}`),
		"anthropic-real", "apikey", "claude-3-5-sonnet-20241022", false, false, false,
	)
	require.NoError(t, err)
	require.Equal(t, "anthropic-real", getHeaderRaw(req.Header, "x-api-key"))
	require.Equal(t, "override-beta", getHeaderRaw(req.Header, "anthropic-beta"))
	require.Equal(t, "override-claude/1.0", req.Header.Get("User-Agent"))
}
