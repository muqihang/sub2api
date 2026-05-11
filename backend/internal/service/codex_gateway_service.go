package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

type codexGatewayStreamingHandledError struct{}

func (e *codexGatewayStreamingHandledError) Error() string {
	return "codex gateway streaming error already written"
}

type CodexGatewayService struct {
	registry *CodexGatewayModelRegistry
	executor codexGatewayExecutor
}

type codexGatewayExecutor interface {
	Complete(ctx context.Context, req CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error)
	Stream(ctx context.Context, req CodexGatewayProviderRequest) error
}

func NewCodexGatewayService(registry *CodexGatewayModelRegistry, executor codexGatewayExecutor) *CodexGatewayService {
	return &CodexGatewayService{
		registry: registry,
		executor: executor,
	}
}

func (s *CodexGatewayService) Models(_ context.Context, req CodexGatewayModelsRequest) (*CodexGatewayServiceResponse, error) {
	if err := ValidateCodexScopedAPIKeyAccess(req.APIKey, "/codex/v1/models"); err != nil {
		return codexGatewayHTTPErrorResponse(http.StatusForbidden, CodexGatewayErrorTypeAuthentication, "invalid_api_key", err.Error()), nil
	}
	if s == nil || s.registry == nil {
		return codexGatewayHTTPErrorResponse(http.StatusServiceUnavailable, CodexGatewayErrorTypeAPI, "service_unavailable", "codex gateway model registry is not configured"), nil
	}
	body, err := json.Marshal(s.registry.ModelsResponse())
	if err != nil {
		return nil, err
	}
	return &CodexGatewayServiceResponse{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       body,
	}, nil
}

func (s *CodexGatewayService) Responses(ctx context.Context, req CodexGatewayResponsesRequest) (*CodexGatewayServiceResponse, error) {
	if err := ValidateCodexScopedAPIKeyAccess(req.APIKey, "/codex/v1/responses"); err != nil {
		return codexGatewayHTTPErrorResponse(http.StatusForbidden, CodexGatewayErrorTypeAuthentication, "invalid_api_key", err.Error()), nil
	}
	if s == nil || s.registry == nil || s.executor == nil {
		return codexGatewayHTTPErrorResponse(http.StatusServiceUnavailable, CodexGatewayErrorTypeAPI, "service_unavailable", "codex gateway service is not configured"), nil
	}

	parsed, err := DecodeCodexGatewayResponsesCreateRequest(req.Body)
	if err != nil {
		return codexGatewayHTTPErrorResponse(http.StatusBadRequest, CodexGatewayErrorTypeInvalidRequest, CodexGatewayErrorCodeInvalidRequest, "failed to parse request body"), nil
	}
	if strings.TrimSpace(parsed.Model) == "" {
		return codexGatewayHTTPErrorResponse(http.StatusBadRequest, CodexGatewayErrorTypeInvalidRequest, CodexGatewayErrorCodeInvalidRequest, "model is required"), nil
	}

	model, ok := s.registry.Resolve(parsed.Model)
	if !ok {
		return codexGatewayHTTPErrorResponse(http.StatusBadRequest, CodexGatewayErrorTypeInvalidRequest, CodexGatewayErrorCodeInvalidRequest, fmt.Sprintf("model %q is not supported", parsed.Model)), nil
	}
	if !model.SupportedInAPI || strings.TrimSpace(model.Visibility) != "visible" {
		return codexGatewayHTTPErrorResponse(http.StatusBadRequest, CodexGatewayErrorTypeInvalidRequest, CodexGatewayErrorCodeInvalidRequest, fmt.Sprintf("model %q is not supported", parsed.Model)), nil
	}
	if model.Provider == "deepseek" && parsed.PreviousResponseID != nil && strings.TrimSpace(*parsed.PreviousResponseID) != "" {
		return codexGatewayHTTPErrorResponse(http.StatusBadRequest, CodexGatewayErrorTypeInvalidRequest, CodexGatewayErrorCodeInvalidRequest, "previous_response_id is not supported on the HTTP gateway path for DeepSeek models"), nil
	}

	providerReq := CodexGatewayProviderRequest{
		Request:      req,
		Model:        model,
		Parsed:       parsed,
		SessionKey:   codexGatewaySessionKey(ctx, req.Headers, req.Body),
		IsolationKey: codexGatewayIsolationKey(ctx, req.APIKey),
	}

	isStream := parsed.Stream != nil && *parsed.Stream
	if isStream {
		if req.ResponseHeader != nil {
			req.ResponseHeader.Set("Content-Type", "text/event-stream")
			req.ResponseHeader.Set("Cache-Control", "no-cache")
			req.ResponseHeader.Set("Connection", "keep-alive")
		}
		if req.WriteStatus != nil {
			req.WriteStatus(http.StatusOK)
		}
		if err := s.executor.Stream(ctx, providerReq); err != nil {
			codexGatewayWriteStreamingError(req, err)
		}
		return nil, nil
	}

	resp, err := s.executor.Complete(ctx, providerReq)
	if err != nil {
		return codexGatewayMapProviderError(err), nil
	}
	return resp, nil
}

func codexGatewayWriteStreamingError(req CodexGatewayResponsesRequest, err error) {
	if req.StreamWriter == nil {
		return
	}
	var handled *codexGatewayStreamingHandledError
	if errors.As(err, &handled) {
		return
	}
	status, errType, errCode, message := codexGatewayErrorEnvelopeForError(err)
	if req.ResponseHeader != nil {
		for key := range req.ResponseHeader {
			if codexGatewayAllowedOpenAIResponseHeader(key) {
				req.ResponseHeader.Del(key)
			}
		}
		req.ResponseHeader.Set("Content-Type", "text/event-stream")
		req.ResponseHeader.Set("Cache-Control", "no-cache")
		req.ResponseHeader.Set("Connection", "keep-alive")
	}
	if req.WriteStatus != nil {
		req.WriteStatus(http.StatusOK)
	}
	_ = status
	_ = writeCodexGatewayStreamFailure(req.StreamWriter, "", errType, errCode, message)
	if req.Flush != nil {
		req.Flush()
	}
}

func codexGatewayMapProviderError(err error) *CodexGatewayServiceResponse {
	status, errType, errCode, message := codexGatewayErrorEnvelopeForError(err)
	return codexGatewayHTTPErrorResponse(status, errType, errCode, message)
}

func codexGatewayErrorEnvelopeForError(err error) (int, string, string, string) {
	if err == nil {
		return http.StatusBadGateway, CodexGatewayErrorTypeAPI, "upstream_error", "upstream request failed"
	}
	var unavailable *CodexGatewayProviderUnavailableError
	if errors.As(err, &unavailable) {
		return http.StatusServiceUnavailable, CodexGatewayErrorTypeAPI, "service_unavailable", "No available accounts"
	}
	var failoverErr *UpstreamFailoverError
	if errors.As(err, &failoverErr) {
		return http.StatusBadGateway, CodexGatewayErrorTypeAPI, "upstream_error", "upstream request failed"
	}
	return http.StatusBadGateway, CodexGatewayErrorTypeAPI, "upstream_error", strings.TrimSpace(err.Error())
}

func codexGatewayHTTPErrorResponse(status int, errType, code, message string) *CodexGatewayServiceResponse {
	body, _ := MarshalCodexGatewayErrorJSON(errType, code, message)
	return &CodexGatewayServiceResponse{
		StatusCode: status,
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       body,
	}
}

func codexGatewaySessionKey(ctx context.Context, headers http.Header, body []byte) string {
	sessionID := strings.TrimSpace(headers.Get("session_id"))
	if sessionID == "" {
		sessionID = strings.TrimSpace(headers.Get("conversation_id"))
	}
	if sessionID == "" {
		sessionID = strings.TrimSpace(gjsonStringBytes(body, "prompt_cache_key"))
	}
	if sessionID == "" {
		sessionID = strings.TrimSpace(deriveOpenAIContentSessionSeed(body))
	}
	if sessionID == "" {
		return ""
	}
	currentHash, _ := deriveOpenAISessionHashes(EntityScopedSeedFromContext(ctx, sessionID))
	return currentHash
}

func codexGatewayIsolationKey(ctx context.Context, apiKey *APIKey) string {
	seed := fmt.Sprintf("codex:%d", apiKeyIDValue(apiKey))
	scoped := EntityScopedSeedFromContext(ctx, seed)
	sum := sha256.Sum256([]byte(scoped))
	return hex.EncodeToString(sum[:8])
}

func apiKeyIDValue(apiKey *APIKey) int64 {
	if apiKey == nil {
		return 0
	}
	return apiKey.ID
}

func gjsonStringBytes(body []byte, path string) string {
	return gjson.GetBytes(body, path).String()
}

func cloneCodexGatewayStreamBody(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
