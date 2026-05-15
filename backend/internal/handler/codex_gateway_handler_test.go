package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexGatewayHandlerServiceStub struct {
	modelsReq     *service.CodexGatewayModelsRequest
	modelsResp    *service.CodexGatewayServiceResponse
	modelsErr     error
	responsesReq  *service.CodexGatewayResponsesRequest
	responsesResp *service.CodexGatewayServiceResponse
	responsesErr  error
	responsesHook func(req service.CodexGatewayResponsesRequest)
}

func (s *codexGatewayHandlerServiceStub) Models(_ context.Context, req service.CodexGatewayModelsRequest) (*service.CodexGatewayServiceResponse, error) {
	s.modelsReq = &req
	return s.modelsResp, s.modelsErr
}

func (s *codexGatewayHandlerServiceStub) Responses(_ context.Context, req service.CodexGatewayResponsesRequest) (*service.CodexGatewayServiceResponse, error) {
	s.responsesReq = &req
	if s.responsesHook != nil {
		s.responsesHook(req)
	}
	return s.responsesResp, s.responsesErr
}

func TestCodexGatewayHandler_ResponsesDelegatesBodyAndAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(11)
	product := service.CodexUsageClientProduct
	apiKey := &service.APIKey{
		ID:                      42,
		Key:                     "sk-test",
		Status:                  service.StatusActive,
		GroupID:                 &groupID,
		RestrictedClientProduct: &product,
		Group: &service.Group{
			ID:                   groupID,
			Platform:             service.PlatformOpenAI,
			Status:               service.StatusActive,
			Hydrated:             true,
			CodexGatewayEntitled: true,
		},
	}
	stub := &codexGatewayHandlerServiceStub{
		responsesResp: &service.CodexGatewayServiceResponse{
			StatusCode: http.StatusCreated,
			Headers:    http.Header{"Content-Type": []string{"application/json"}},
			Body:       []byte(`{"id":"resp_123"}`),
		},
	}
	h := NewCodexGatewayHandler(stub)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/codex/v1/responses", http.NoBody)
	c.Request.Body = ioNopCloserString(`{"model":"gpt-5.5","input":"hello"}`)
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)

	h.Responses(c)

	require.Equal(t, http.StatusCreated, w.Code)
	require.JSONEq(t, `{"id":"resp_123"}`, w.Body.String())
	require.NotNil(t, stub.responsesReq)
	require.Equal(t, apiKey, stub.responsesReq.APIKey)
	require.JSONEq(t, `{"model":"gpt-5.5","input":"hello"}`, string(stub.responsesReq.Body))
}

func TestCodexGatewayHandler_ResponsesBodyTooLargeReturns413(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewCodexGatewayHandler(&codexGatewayHandlerServiceStub{})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/codex/v1/responses", ioNopCloserString("abcdef"))
	req.Body = http.MaxBytesReader(w, req.Body, 3)
	c.Request = req
	groupID := int64(12)
	product := service.CodexUsageClientProduct
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		ID:                      42,
		Key:                     "sk-test",
		Status:                  service.StatusActive,
		GroupID:                 &groupID,
		RestrictedClientProduct: &product,
		Group: &service.Group{
			ID:                   groupID,
			Platform:             service.PlatformOpenAI,
			Status:               service.StatusActive,
			Hydrated:             true,
			CodexGatewayEntitled: true,
		},
	})

	h.Responses(c)

	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	require.Contains(t, w.Body.String(), `"type":"invalid_request_error"`)
	require.Contains(t, w.Body.String(), `"code":"invalid_request"`)
}

func TestCodexGatewayHandlerModels_DelegatesClientVersionAndAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(13)
	product := service.CodexUsageClientProduct
	apiKey := &service.APIKey{
		ID:                      42,
		Key:                     "sk-test",
		Status:                  service.StatusActive,
		GroupID:                 &groupID,
		RestrictedClientProduct: &product,
		Group: &service.Group{
			ID:                   groupID,
			Platform:             service.PlatformOpenAI,
			Status:               service.StatusActive,
			Hydrated:             true,
			CodexGatewayEntitled: true,
		},
	}
	stub := &codexGatewayHandlerServiceStub{
		modelsResp: &service.CodexGatewayServiceResponse{
			StatusCode: http.StatusOK,
			Headers:    http.Header{"Content-Type": []string{"application/json"}},
			Body:       []byte(`{"models":[{"slug":"gpt-5.5"}]}`),
		},
	}
	h := NewCodexGatewayHandler(stub)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/codex/v1/models?client_version=1.2.3", nil)
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)

	h.Models(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"models":[{"slug":"gpt-5.5"}]}`, w.Body.String())
	require.NotNil(t, stub.modelsReq)
	require.Equal(t, apiKey, stub.modelsReq.APIKey)
	require.Equal(t, "1.2.3", stub.modelsReq.ClientVersion)
}

func TestCodexGatewayHandlerModels_ServiceErrorReturnsAPIError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(14)
	product := service.CodexUsageClientProduct
	apiKey := &service.APIKey{
		ID:                      42,
		Key:                     "sk-test",
		Status:                  service.StatusActive,
		GroupID:                 &groupID,
		RestrictedClientProduct: &product,
		Group: &service.Group{
			ID:                   groupID,
			Platform:             service.PlatformOpenAI,
			Status:               service.StatusActive,
			Hydrated:             true,
			CodexGatewayEntitled: true,
		},
	}
	h := NewCodexGatewayHandler(&codexGatewayHandlerServiceStub{
		modelsErr: errors.New("boom"),
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/codex/v1/models", nil)
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)

	h.Models(c)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), `"type":"api_error"`)
	require.Contains(t, w.Body.String(), `"code":"internal_error"`)
}

func TestCodexGatewayHandler_ResponsesNilServiceResponseReturnsAPIError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(15)
	product := service.CodexUsageClientProduct
	apiKey := &service.APIKey{
		ID:                      42,
		Key:                     "sk-test",
		Status:                  service.StatusActive,
		GroupID:                 &groupID,
		RestrictedClientProduct: &product,
		Group: &service.Group{
			ID:                   groupID,
			Platform:             service.PlatformOpenAI,
			Status:               service.StatusActive,
			Hydrated:             true,
			CodexGatewayEntitled: true,
		},
	}
	h := NewCodexGatewayHandler(&codexGatewayHandlerServiceStub{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/codex/v1/responses", ioNopCloserString(`{"model":"gpt-5.5","input":"hello"}`))
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)

	h.Responses(c)

	require.Equal(t, http.StatusBadGateway, w.Code)
	require.Contains(t, w.Body.String(), `"type":"api_error"`)
	require.Contains(t, w.Body.String(), `"code":"upstream_error"`)
}

func TestCodexGatewayHandlerResponses_ServiceManagedStreamingResponseIsPreserved(t *testing.T) {
	gin.SetMode(gin.TestMode)

	apiKey := validCodexGatewayHandlerAPIKeyForTest(16)
	h := NewCodexGatewayHandler(&codexGatewayHandlerServiceStub{
		responsesHook: func(req service.CodexGatewayResponsesRequest) {
			req.ResponseHeader.Set("Content-Type", "text/event-stream")
			req.WriteStatus(http.StatusOK)
			_, _ = req.StreamWriter.Write([]byte("data: stream-output\n\n"))
			if req.Flush != nil {
				req.Flush()
			}
		},
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/codex/v1/responses", ioNopCloserString(`{"model":"gpt-5.5","stream":true}`))
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)

	h.Responses(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	require.Equal(t, "data: stream-output\n\n", w.Body.String())
}

func validCodexGatewayHandlerAPIKeyForTest(groupID int64) *service.APIKey {
	product := service.CodexUsageClientProduct
	return &service.APIKey{
		ID:                      42,
		Key:                     "sk-test",
		Status:                  service.StatusActive,
		GroupID:                 &groupID,
		RestrictedClientProduct: &product,
		Group: &service.Group{
			ID:                   groupID,
			Platform:             service.PlatformOpenAI,
			Status:               service.StatusActive,
			Hydrated:             true,
			CodexGatewayEntitled: true,
		},
	}
}

type nopStringReadCloser struct {
	reader *strings.Reader
}

func ioNopCloserString(value string) *nopStringReadCloser {
	return &nopStringReadCloser{reader: strings.NewReader(value)}
}

func (r *nopStringReadCloser) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *nopStringReadCloser) Close() error {
	return nil
}
