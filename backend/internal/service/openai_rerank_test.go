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

func TestBuildOpenAIRerankURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		base          string
		upstreamModel string
		want          string
	}{
		{
			name:          "nvidia integrate API uses retrieval endpoint",
			base:          "https://integrate.api.nvidia.com/v1",
			upstreamModel: "nvidia/llama-nemotron-rerank-1b-v2",
			want:          "https://ai.api.nvidia.com/v1/retrieval/nvidia/llama-nemotron-rerank-1b-v2/reranking",
		},
		{
			name:          "nvidia integrate API supports alternate rerank model",
			base:          "https://integrate.api.nvidia.com/v1",
			upstreamModel: "nvidia/llama-nemotron-rerank-vl-1b-v2",
			want:          "https://ai.api.nvidia.com/v1/retrieval/nvidia/llama-nemotron-rerank-vl-1b-v2/reranking",
		},
		{
			name:          "generic OpenAI compatible upstream",
			base:          "https://rerank.example/v1",
			upstreamModel: "rerank-v1",
			want:          "https://rerank.example/v1/rerank",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, buildOpenAIRerankURL(tt.base, tt.upstreamModel))
		})
	}
}

func TestForwardRerank_CohereRequestConvertsToNVIDIAAndNormalizesResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reqBody := []byte(`{
		"model":"nvidia-rerank",
		"query":"What is GPU computing?",
		"documents":[
			"GPUs accelerate parallel workloads.",
			{"text":"Bananas are fruit.","id":"banana"},
			"CUDA is a parallel computing platform for NVIDIA GPUs."
		],
		"top_n":2,
		"return_documents":true
	}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/rerank", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"X-Request-Id": []string{"rerank-rid"},
		},
		Body: io.NopCloser(strings.NewReader(`{
			"rankings":[
				{"index":2,"logit":6.25},
				{"index":0,"logit":5.12},
				{"index":1,"logit":-18.2}
			]
		}`)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:       234,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "nvapi-test",
			"base_url": "https://integrate.api.nvidia.com/v1",
			"model_mapping": map[string]any{
				"nvidia-rerank": "nvidia/llama-nemotron-rerank-1b-v2",
			},
		},
	}

	result, err := svc.ForwardRerank(context.Background(), c, account, reqBody, "")

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, result)
	require.Equal(t, "rerank-rid", result.RequestID)
	require.Equal(t, "nvidia-rerank", result.Model)
	require.Equal(t, "nvidia/llama-nemotron-rerank-1b-v2", result.BillingModel)
	require.Equal(t, "nvidia/llama-nemotron-rerank-1b-v2", result.UpstreamModel)
	require.Equal(t, "https://ai.api.nvidia.com/v1/retrieval/nvidia/llama-nemotron-rerank-1b-v2/reranking", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer nvapi-test", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "nvidia/llama-nemotron-rerank-1b-v2", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "What is GPU computing?", gjson.GetBytes(upstream.lastBody, "query.text").String())
	require.Equal(t, int64(3), gjson.GetBytes(upstream.lastBody, "passages.#").Int())
	require.Equal(t, "CUDA is a parallel computing platform for NVIDIA GPUs.", gjson.GetBytes(upstream.lastBody, "passages.2.text").String())
	require.Equal(t, "NONE", gjson.GetBytes(upstream.lastBody, "truncate").String())

	responseBody := rec.Body.Bytes()
	require.Equal(t, "list", gjson.GetBytes(responseBody, "object").String())
	require.Equal(t, "nvidia-rerank", gjson.GetBytes(responseBody, "model").String())
	require.Equal(t, int64(2), gjson.GetBytes(responseBody, "results.#").Int())
	require.Equal(t, int64(2), gjson.GetBytes(responseBody, "results.0.index").Int())
	require.Equal(t, 6.25, gjson.GetBytes(responseBody, "results.0.relevance_score").Float())
	require.Equal(t, "CUDA is a parallel computing platform for NVIDIA GPUs.", gjson.GetBytes(responseBody, "results.0.document.text").String())
	require.Equal(t, int64(0), gjson.GetBytes(responseBody, "results.1.index").Int())
}
