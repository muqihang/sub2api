package service

import (
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

func TestExtractOpenAIReasoningEffortFromBodyModelCandidates(t *testing.T) {
	bodyWithoutEffort := []byte(`{"model":"whatever","input":"hello"}`)
	bodyWithMax := []byte(`{"model":"sol","reasoning":{"effort":"max"},"input":"hello"}`)

	tests := []struct {
		name       string
		body       []byte
		candidates []string
		want       string
	}{
		{
			name:       "falls back to original model suffix",
			body:       bodyWithoutEffort,
			candidates: []string{"gpt-5.4", "gpt-5.4", "gpt-5.4-xhigh"},
			want:       "xhigh",
		},
		{
			name:       "preserves GPT 5.6 max suffix",
			body:       bodyWithoutEffort,
			candidates: []string{"gpt-5.6-sol", "gpt-5.6-sol", "gpt-5.6-sol-max"},
			want:       "max",
		},
		{
			name:       "uses mapped model for explicit max",
			body:       bodyWithMax,
			candidates: []string{"gpt-5.6-sol", "sol"},
			want:       "max",
		},
		{
			name:       "normalizes max for non GPT 5.6 model",
			body:       bodyWithMax,
			candidates: []string{"gpt-5.4", "sol"},
			want:       "xhigh",
		},
		{
			name:       "returns nil without an effort suffix",
			body:       bodyWithoutEffort,
			candidates: []string{"gpt-5.4", "gpt-5.4", "gpt-5.4"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := extractOpenAIReasoningEffortFromBody(testCase.body, testCase.candidates...)
			if testCase.want == "" {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, testCase.want, *got)
		})
	}
}

func TestExtractOpenAIReasoningEffortModelCandidates(t *testing.T) {
	reqBody := map[string]any{"model": "gpt-5.3-codex-high", "input": "hello"}

	got := extractOpenAIReasoningEffort(reqBody, "gpt-5.3-codex", "gpt-5.3-codex-high")

	require.NotNil(t, got)
	require.Equal(t, "high", *got)
}

func TestOpenAIGatewayServiceForwardOAuthDerivesEffortFromSuffixModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"usage":{"input_tokens":1,"output_tokens":2}}`)),
		},
	}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := &Account{
		ID:          11,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
		Status:      StatusActive,
		Schedulable: true,
	}
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)
	SetOpenAIClientTransport(ginContext, OpenAIClientTransportHTTP)

	body := []byte(`{"model":"gpt-5.3-codex-xhigh","instructions":"suffix-test","input":"hello","stream":false}`)
	result, err := svc.Forward(context.Background(), ginContext, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "gpt-5.3-codex", gjson.GetBytes(upstream.lastBody, "model").String())
	require.NotNil(t, result.ReasoningEffort)
	require.Equal(t, "xhigh", *result.ReasoningEffort)
}
