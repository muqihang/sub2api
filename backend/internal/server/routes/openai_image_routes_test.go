package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayRoutesOpenAIPrefixedRoutesAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/openai/_health"},
		{method: http.MethodGet, path: "/openai/_verify?account_id=1&transport=http"},
		{method: http.MethodGet, path: "/openai/_tls_canary?account_id=1&bucket=default&transport=http"},
		{method: http.MethodPost, path: "/openai/_tls/canary", body: `{"account_id":1,"bucket":"default","transport":"http","route":"/v1/responses"}`},
		{method: http.MethodGet, path: "/openai/v1/responses"},
		{method: http.MethodPost, path: "/openai/v1/responses", body: `{"model":"gpt-5.4"}`},
		{method: http.MethodPost, path: "/openai/v1/responses/compact", body: `{"model":"gpt-5.4"}`},
		{method: http.MethodPost, path: "/openai/v1/chat/completions", body: `{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}]}`},
		{method: http.MethodPost, path: "/openai/v1/images/generations", body: `{"model":"gpt-image-2","prompt":"apple"}`},
	}

	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)
			require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should be registered", tc.path)
		})
	}
}

func TestGatewayRoutesOpenAIImageGenerationRoutesAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/images/generations",
		"/images/generations",
		"/openai/v1/images/generations",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-image-2","prompt":"apple"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should be registered", path)
	}
}
