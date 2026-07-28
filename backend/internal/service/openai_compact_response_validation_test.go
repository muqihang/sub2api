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
)

func TestOpenAICompactResponseSemantics(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantValid bool
		wantCount int
		wantTotal int
	}{
		{
			name:      "one compaction item",
			body:      `{"output":[{"id":"cmp_1","type":"compaction","encrypted_content":"opaque"}]}`,
			wantValid: true,
			wantCount: 1,
			wantTotal: 1,
		},
		{
			name:      "ordinary reasoning and message response",
			body:      `{"output":[{"id":"rs_1","type":"reasoning"},{"id":"msg_1","type":"message"}]}`,
			wantValid: false,
			wantCount: 0,
			wantTotal: 2,
		},
		{
			name:      "response compaction object without output item",
			body:      `{"object":"response.compaction","output":[]}`,
			wantValid: false,
			wantCount: 0,
			wantTotal: 0,
		},
		{
			name: "stream deduplicates done and completed item",
			body: "event: response.output_item.done\n" +
				`data: {"type":"response.output_item.done","item":{"id":"cmp_1","type":"compaction","encrypted_content":"opaque"}}` + "\n\n" +
				"event: response.completed\n" +
				`data: {"type":"response.completed","response":{"output":[{"id":"cmp_1","type":"compaction","encrypted_content":"opaque"}]}}` + "\n\n",
			wantValid: true,
			wantCount: 1,
			wantTotal: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, compactCount, outputCount := openAICompactResponseSemantics([]byte(tt.body))
			require.Equal(t, tt.wantValid, valid)
			require.Equal(t, tt.wantCount, compactCount)
			require.Equal(t, tt.wantTotal, outputCount)
		})
	}
}

func TestOpenAIGatewayService_Forward_AutoCompactRetriesSemanticMismatchOnAlternateEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gpt-5.6-sol","stream":false,"instructions":"compact-test","input":[{"type":"message","role":"user","content":"hello"},{"type":"compaction_trigger"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	MarkOpenAICompactClientStream(c)

	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"id":"resp_wrong","status":"completed","output":[{"id":"rs_1","type":"reasoning"},{"id":"msg_1","type":"message"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
			)),
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"id":"cmp_ok","object":"response.compaction","status":"completed","output":[{"id":"cmp_1","type":"compaction","encrypted_content":"opaque"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
			)),
		},
	}}
	svc := &OpenAIGatewayService{
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		}},
	}
	account := &Account{
		ID: 178, Name: "semantic-fallback", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Concurrency: 1, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://example.test/v1"},
		Extra: map[string]any{
			"openai_compact_endpoint_mode_detected": OpenAICompactEndpointModeBodySignal,
		},
	}

	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "/v1/responses", upstream.requests[0].URL.Path)
	require.Equal(t, "/v1/responses/compact", upstream.requests[1].URL.Path)
	validCompact, compactCount, _ := openAICompactResponseSemantics(rec.Body.Bytes())
	require.True(t, validCompact)
	require.Equal(t, 1, compactCount)
}
