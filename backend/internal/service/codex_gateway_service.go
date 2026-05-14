package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	capture  *CodexGatewayCaptureManager
}

type codexGatewayExecutor interface {
	Complete(ctx context.Context, req CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error)
	Stream(ctx context.Context, req CodexGatewayProviderRequest) error
}

func NewCodexGatewayService(registry *CodexGatewayModelRegistry, executor codexGatewayExecutor, capture ...*CodexGatewayCaptureManager) *CodexGatewayService {
	svc := &CodexGatewayService{
		registry: registry,
		executor: executor,
	}
	if len(capture) > 0 {
		svc.capture = capture[0]
	}
	return svc
}

func (s *CodexGatewayService) Models(ctx context.Context, req CodexGatewayModelsRequest) (*CodexGatewayServiceResponse, error) {
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
	if s.capture != nil {
		trace := s.capture.StartTrace(ctx, CodexGatewayCaptureTraceMeta{
			Method:       "GET",
			Path:         "/codex/v1/models",
			ForceCapture: false,
		})
		if trace != nil {
			s.capture.RecordModelCatalog(trace, body)
			s.capture.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "ok", HTTPStatus: http.StatusOK})
		}
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

	trace := s.startCaptureTrace(ctx, req)
	if trace != nil {
		req.CaptureTrace = trace
		s.capture.RecordClientRequest(trace, req.Headers, req.Body)
	}

	parsed, err := DecodeCodexGatewayResponsesCreateRequest(req.Body)
	if err != nil {
		s.finishCaptureError(trace, CodexGatewayCaptureError{Origin: "client", Stage: "decode", ErrorType: CodexGatewayErrorTypeInvalidRequest, ErrorCode: CodexGatewayErrorCodeInvalidRequest, Message: "failed to parse request body"})
		return codexGatewayHTTPErrorResponse(http.StatusBadRequest, CodexGatewayErrorTypeInvalidRequest, CodexGatewayErrorCodeInvalidRequest, "failed to parse request body"), nil
	}
	if strings.TrimSpace(parsed.Model) == "" {
		s.finishCaptureError(trace, CodexGatewayCaptureError{Origin: "client", Stage: "validate", ErrorType: CodexGatewayErrorTypeInvalidRequest, ErrorCode: CodexGatewayErrorCodeInvalidRequest, Message: "model is required"})
		return codexGatewayHTTPErrorResponse(http.StatusBadRequest, CodexGatewayErrorTypeInvalidRequest, CodexGatewayErrorCodeInvalidRequest, "model is required"), nil
	}

	model, ok := s.registry.Resolve(parsed.Model)
	if !ok {
		s.finishCaptureError(trace, CodexGatewayCaptureError{Origin: "gateway", Stage: "model_resolution", Model: parsed.Model, ErrorType: CodexGatewayErrorTypeInvalidRequest, ErrorCode: CodexGatewayErrorCodeInvalidRequest, Message: fmt.Sprintf("model %q is not supported", parsed.Model)})
		return codexGatewayHTTPErrorResponse(http.StatusBadRequest, CodexGatewayErrorTypeInvalidRequest, CodexGatewayErrorCodeInvalidRequest, fmt.Sprintf("model %q is not supported", parsed.Model)), nil
	}
	if !model.SupportedInAPI || strings.TrimSpace(model.Visibility) != "visible" {
		s.finishCaptureError(trace, CodexGatewayCaptureError{Origin: "gateway", Stage: "model_visibility", Provider: model.Provider, Model: parsed.Model, UpstreamModel: model.UpstreamModel, ErrorType: CodexGatewayErrorTypeInvalidRequest, ErrorCode: CodexGatewayErrorCodeInvalidRequest, Message: fmt.Sprintf("model %q is not supported", parsed.Model)})
		return codexGatewayHTTPErrorResponse(http.StatusBadRequest, CodexGatewayErrorTypeInvalidRequest, CodexGatewayErrorCodeInvalidRequest, fmt.Sprintf("model %q is not supported", parsed.Model)), nil
	}
	if model.Provider == "deepseek" && parsed.PreviousResponseID != nil && strings.TrimSpace(*parsed.PreviousResponseID) != "" {
		s.finishCaptureError(trace, CodexGatewayCaptureError{Origin: "gateway", Stage: "validate", Provider: model.Provider, Model: parsed.Model, UpstreamModel: model.UpstreamModel, ErrorType: CodexGatewayErrorTypeInvalidRequest, ErrorCode: CodexGatewayErrorCodeInvalidRequest, Message: "previous_response_id is not supported on the HTTP gateway path for DeepSeek models"})
		return codexGatewayHTTPErrorResponse(http.StatusBadRequest, CodexGatewayErrorTypeInvalidRequest, CodexGatewayErrorCodeInvalidRequest, "previous_response_id is not supported on the HTTP gateway path for DeepSeek models"), nil
	}
	if trace != nil {
		trace.Meta.Model = parsed.Model
		trace.Meta.Provider = model.Provider
		s.capture.RecordProviderSelection(trace, model.Provider, model.UpstreamModel, "")
	}
	streamWriter := req.StreamWriter
	if trace != nil && streamWriter != nil {
		streamWriter = NewCodexGatewayCaptureStreamWriter(streamWriter, s.capture, trace, "client")
		req.StreamWriter = streamWriter
	}

	providerReq := CodexGatewayProviderRequest{
		Request:      req,
		Model:        model,
		Parsed:       parsed,
		SessionKey:   codexGatewaySessionKey(ctx, req.Headers, req.Body),
		IsolationKey: codexGatewayIsolationKey(ctx, req.APIKey),
		CaptureTrace: trace,
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
			s.captureStreamPending(streamWriter)
			s.finishCaptureError(trace, captureErrorForCodexGatewayError(err, model, "stream"))
			codexGatewayWriteStreamingError(req, err)
		} else {
			s.captureStreamPending(streamWriter)
			s.finishCaptureOK(trace, CodexGatewayCaptureFinishSummary{Status: "ok", UpstreamModel: model.UpstreamModel})
		}
		return nil, nil
	}

	resp, err := s.executor.Complete(ctx, providerReq)
	if err != nil {
		s.finishCaptureError(trace, captureErrorForCodexGatewayError(err, model, "complete"))
		return codexGatewayMapProviderError(err), nil
	}
	if trace != nil && resp != nil {
		s.finishCaptureOK(trace, CodexGatewayCaptureFinishSummary{Status: "ok", HTTPStatus: resp.StatusCode, UpstreamModel: model.UpstreamModel})
	}
	return resp, nil
}

func (s *CodexGatewayService) startCaptureTrace(ctx context.Context, req CodexGatewayResponsesRequest) *CodexGatewayTrace {
	if s == nil || s.capture == nil {
		return nil
	}
	return s.capture.StartTrace(ctx, CodexGatewayCaptureTraceMeta{
		Method:       "POST",
		Path:         "/codex/v1/responses",
		SessionID:    strings.TrimSpace(req.Headers.Get("session_id")),
		ThreadID:     firstCaptureNonEmpty(req.Headers.Get("thread_id"), req.Headers.Get("conversation_id")),
		ForceCapture: false,
	})
}

func (s *CodexGatewayService) finishCaptureOK(trace *CodexGatewayTrace, summary CodexGatewayCaptureFinishSummary) {
	if s == nil || s.capture == nil || trace == nil {
		return
	}
	if summary.HTTPStatus == 0 {
		summary.HTTPStatus = http.StatusOK
	}
	s.capture.FinishTrace(trace, summary)
}

func (s *CodexGatewayService) finishCaptureError(trace *CodexGatewayTrace, errMeta CodexGatewayCaptureError) {
	if s == nil || s.capture == nil || trace == nil {
		return
	}
	s.capture.RecordError(trace, errMeta)
	s.capture.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "failed", HTTPStatus: errMeta.HTTPStatus, UpstreamModel: errMeta.UpstreamModel})
}

func (s *CodexGatewayService) captureStreamPending(w io.Writer) {
	if flusher, ok := w.(*CodexGatewayCaptureStreamWriter); ok {
		flusher.FlushPending()
	}
}

func captureErrorForCodexGatewayError(err error, model CodexGatewayModel, stage string) CodexGatewayCaptureError {
	status, errType, errCode, message := codexGatewayErrorEnvelopeForError(err)
	return CodexGatewayCaptureError{
		Origin:        "upstream",
		Stage:         stage,
		Provider:      model.Provider,
		Model:         model.Slug,
		UpstreamModel: model.UpstreamModel,
		HTTPStatus:    status,
		ErrorType:     errType,
		ErrorCode:     errCode,
		Message:       message,
	}
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
		if len(failoverErr.ResponseBody) > 0 {
			errType := strings.TrimSpace(gjson.GetBytes(failoverErr.ResponseBody, "error.type").String())
			errCode := strings.TrimSpace(gjson.GetBytes(failoverErr.ResponseBody, "error.code").String())
			message := strings.TrimSpace(gjson.GetBytes(failoverErr.ResponseBody, "error.message").String())
			if errType != "" || errCode != "" || message != "" {
				if errType == "" {
					errType = CodexGatewayErrorTypeAPI
				}
				if errCode == "" {
					errCode = "upstream_error"
				}
				if message == "" {
					message = "upstream request failed"
				}
				return http.StatusBadGateway, errType, errCode, message
			}
		}
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
