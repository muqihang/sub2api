package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	pkgerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type codexGatewayHandlerService interface {
	Models(ctx context.Context, req service.CodexGatewayModelsRequest) (*service.CodexGatewayServiceResponse, error)
	Responses(ctx context.Context, req service.CodexGatewayResponsesRequest) (*service.CodexGatewayServiceResponse, error)
}

type CodexGatewayHandler struct {
	service codexGatewayHandlerService
}

func NewCodexGatewayHandler(svc codexGatewayHandlerService) *CodexGatewayHandler {
	return &CodexGatewayHandler{service: svc}
}

func (h *CodexGatewayHandler) Models(c *gin.Context) {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		service.WriteCodexGatewayErrorJSON(c.Writer, http.StatusInternalServerError, service.CodexGatewayErrorTypeAPI, "internal_error", "authenticated API key missing from context")
		return
	}
	if err := service.ValidateCodexScopedAPIKeyAccess(apiKey, c.Request.URL.Path); err != nil {
		service.WriteCodexGatewayErrorJSON(c.Writer, http.StatusForbidden, service.CodexGatewayErrorTypeAuthentication, "invalid_api_key", pkgerrors.Message(err))
		return
	}
	if h == nil || h.service == nil {
		service.WriteCodexGatewayErrorJSON(c.Writer, http.StatusNotImplemented, service.CodexGatewayErrorTypeInvalidRequest, "not_implemented", "Codex gateway service is not configured")
		return
	}

	resp, err := h.service.Models(c.Request.Context(), service.CodexGatewayModelsRequest{
		APIKey:        apiKey,
		ClientVersion: strings.TrimSpace(c.Query("client_version")),
	})
	if err != nil {
		service.WriteCodexGatewayErrorJSON(c.Writer, http.StatusInternalServerError, service.CodexGatewayErrorTypeAPI, "internal_error", err.Error())
		return
	}
	writeCodexGatewayServiceResponse(c, resp)
}

func (h *CodexGatewayHandler) Responses(c *gin.Context) {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		service.WriteCodexGatewayErrorJSON(c.Writer, http.StatusInternalServerError, service.CodexGatewayErrorTypeAPI, "internal_error", "authenticated API key missing from context")
		return
	}
	if err := service.ValidateCodexScopedAPIKeyAccess(apiKey, c.Request.URL.Path); err != nil {
		service.WriteCodexGatewayErrorJSON(c.Writer, http.StatusForbidden, service.CodexGatewayErrorTypeAuthentication, "invalid_api_key", pkgerrors.Message(err))
		return
	}
	if h == nil || h.service == nil {
		service.WriteCodexGatewayErrorJSON(c.Writer, http.StatusNotImplemented, service.CodexGatewayErrorTypeInvalidRequest, "not_implemented", "Codex gateway service is not configured")
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr):
			service.WriteCodexGatewayErrorJSON(c.Writer, http.StatusRequestEntityTooLarge, service.CodexGatewayErrorTypeInvalidRequest, service.CodexGatewayErrorCodeInvalidRequest, buildBodyTooLargeMessage(maxErr.Limit))
		default:
			if derived, ok := extractMaxBytesError(err); ok {
				service.WriteCodexGatewayErrorJSON(c.Writer, http.StatusRequestEntityTooLarge, service.CodexGatewayErrorTypeInvalidRequest, service.CodexGatewayErrorCodeInvalidRequest, buildBodyTooLargeMessage(derived.Limit))
				return
			}
			service.WriteCodexGatewayErrorJSON(c.Writer, http.StatusBadRequest, service.CodexGatewayErrorTypeInvalidRequest, service.CodexGatewayErrorCodeInvalidRequest, err.Error())
		}
		return
	}

	resp, err := h.service.Responses(c.Request.Context(), service.CodexGatewayResponsesRequest{
		APIKey:         apiKey,
		Headers:        c.Request.Header.Clone(),
		Body:           body,
		StreamWriter:   c.Writer,
		ResponseHeader: c.Writer.Header(),
		WriteStatus:    c.Status,
		Flush:          c.Writer.Flush,
	})
	if err != nil {
		service.WriteCodexGatewayErrorJSON(c.Writer, http.StatusInternalServerError, service.CodexGatewayErrorTypeAPI, "internal_error", err.Error())
		return
	}
	if resp == nil && c.Writer.Written() {
		return
	}
	writeCodexGatewayServiceResponse(c, resp)
}

func writeCodexGatewayServiceResponse(c *gin.Context, resp *service.CodexGatewayServiceResponse) {
	if resp == nil {
		service.WriteCodexGatewayErrorJSON(c.Writer, http.StatusBadGateway, service.CodexGatewayErrorTypeAPI, "upstream_error", "Codex gateway service returned an empty response")
		return
	}
	for key, values := range resp.Headers {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	status := resp.StatusCode
	if status == 0 {
		status = http.StatusOK
	}
	c.Status(status)
	if len(resp.Body) > 0 {
		_, _ = c.Writer.Write(resp.Body)
	}
}
