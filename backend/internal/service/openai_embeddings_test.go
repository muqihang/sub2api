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

func TestBuildOpenAIEmbeddingsURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		base string
		want string
	}{
		{"bare domain", "https://api.openai.com", "https://api.openai.com/v1/embeddings"},
		{"bare /v1", "https://api.openai.com/v1", "https://api.openai.com/v1/embeddings"},
		{"already embeddings", "https://api.openai.com/v1/embeddings", "https://api.openai.com/v1/embeddings"},
		{"third-party versioned path", "https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4/embeddings"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, buildOpenAIEmbeddingsURL(tt.base))
		})
	}
}

func TestForwardEmbeddings_APIKeyPassthroughRecordsUsageAndBatchInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reqBody := []byte(`{
		"model":"nowledge-embedding",
		"input":["hello","world"],
		"encoding_format":"float",
		"dimensions":256
	}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"X-Request-Id": []string{"emb-rid"},
		},
		Body: io.NopCloser(strings.NewReader(`{
			"object":"list",
			"data":[
				{"object":"embedding","index":0,"embedding":[0.1,0.2]},
				{"object":"embedding","index":1,"embedding":[0.3,0.4]}
			],
			"model":"jina-embeddings-v5-text-small",
			"usage":{"prompt_tokens":13,"total_tokens":13}
		}`)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:       42,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://api.jina.ai",
			"model_mapping": map[string]any{
				"nowledge-embedding": "jina-embeddings-v5-text-small",
			},
		},
	}

	result, err := svc.ForwardEmbeddings(context.Background(), c, account, reqBody, "")

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, result)
	require.Equal(t, "emb-rid", result.RequestID)
	require.Equal(t, "nowledge-embedding", result.Model)
	require.Equal(t, "jina-embeddings-v5-text-small", result.BillingModel)
	require.Equal(t, "jina-embeddings-v5-text-small", result.UpstreamModel)
	require.Equal(t, 13, result.Usage.InputTokens)
	require.Equal(t, 0, result.Usage.OutputTokens)
	require.Equal(t, "https://api.jina.ai/v1/embeddings", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer sk-test", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "jina-embeddings-v5-text-small", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, int64(2), gjson.GetBytes(upstream.lastBody, "input.#").Int())
	require.Equal(t, "hello", gjson.GetBytes(upstream.lastBody, "input.0").String())
	require.Equal(t, "world", gjson.GetBytes(upstream.lastBody, "input.1").String())
	require.Equal(t, "float", gjson.GetBytes(upstream.lastBody, "encoding_format").String())
	require.Equal(t, int64(256), gjson.GetBytes(upstream.lastBody, "dimensions").Int())
}

func TestForwardEmbeddings_AccountAliasForcesConfiguredDimensions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reqBody := []byte(`{
		"model":"Zhumeng-embeddings-1536",
		"input":["hello"],
		"dimensions":1024
	}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"object":"list",
			"data":[{"object":"embedding","index":0,"embedding":[` +
			strings.TrimSuffix(strings.Repeat("0.1,", 1536), ",") + `]}],
			"model":"nvidia/llama-nemotron-embed-1b-v2",
			"usage":{"prompt_tokens":1,"total_tokens":1}
		}`)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{
		ID:       42,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://integrate.api.nvidia.com/v1",
			"model_mapping": map[string]any{
				"Zhumeng-embeddings-1536": "nvidia/llama-nemotron-embed-1b-v2",
			},
			"embedding_dimensions": map[string]any{
				"Zhumeng-embeddings-1536": float64(1536),
			},
		},
	}

	_, err := svc.ForwardEmbeddings(context.Background(), c, account, reqBody, "")

	require.NoError(t, err)
	require.Equal(t, int64(1536), gjson.GetBytes(upstream.lastBody, "dimensions").Int())
	require.Equal(t, "Zhumeng-embeddings-1536", gjson.Get(rec.Body.String(), "model").String())
}

func TestForwardEmbeddings_FixedNativeDimensionOmitsUpstreamDimensions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reqBody := []byte(`{
		"model":"Zhumeng-embeddings-1024",
		"input":["hello"],
		"dimensions":1536
	}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"object":"list",
			"data":[{"object":"embedding","index":0,"embedding":[0.1,0.2,0.3]}],
			"model":"native-fixed-model",
			"usage":{"prompt_tokens":1,"total_tokens":1}
		}`)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{
		ID:       42,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://integrate.api.nvidia.com/v1",
			"model_mapping": map[string]any{
				"Zhumeng-embeddings-1024": "native-fixed-model",
			},
			"embedding_dimensions": map[string]any{
				"Zhumeng-embeddings-1024": float64(3),
			},
			"embedding_default_input_type": map[string]any{
				"Zhumeng-embeddings-1024": "query",
			},
			"embedding_omit_dimensions": map[string]any{
				"Zhumeng-embeddings-1024": true,
			},
		},
	}

	_, err := svc.ForwardEmbeddings(context.Background(), c, account, reqBody, "")

	require.NoError(t, err)
	require.False(t, gjson.GetBytes(upstream.lastBody, "dimensions").Exists())
	require.Equal(t, "query", gjson.GetBytes(upstream.lastBody, "input_type").String())
	require.Equal(t, "Zhumeng-embeddings-1024", gjson.Get(rec.Body.String(), "model").String())
}

func TestForwardEmbeddings_ExplicitInputTypeOverridesConfiguredDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reqBody := []byte(`{"model":"Zhumeng-embeddings-1024","input":["hello"],"input_type":"passage"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(reqBody))

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"data":[{"index":0,"embedding":[0.1]}],
			"model":"fixed-model"
		}`)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{
		ID:       42,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-test",
			"embedding_default_input_type": map[string]any{
				"Zhumeng-embeddings-1024": "query",
			},
		},
	}

	_, err := svc.ForwardEmbeddings(context.Background(), c, account, reqBody, "")

	require.NoError(t, err)
	require.Equal(t, "passage", gjson.GetBytes(upstream.lastBody, "input_type").String())
}

func TestForwardEmbeddings_ResponseDimensionMismatchTriggersFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reqBody := []byte(`{"model":"Zhumeng-embeddings-1024","input":["hello"]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(reqBody))

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"data":[{"index":0,"embedding":[0.1,0.2]}],
			"model":"wrong-dimension-model"
		}`)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{
		ID:       42,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-test",
			"embedding_dimensions": map[string]any{
				"Zhumeng-embeddings-1024": float64(3),
			},
		},
	}

	_, err := svc.ForwardEmbeddings(context.Background(), c, account, reqBody, "")

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.False(t, rec.Result().Header.Get("Content-Type") != "" || rec.Body.Len() > 0)
}
