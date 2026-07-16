package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

var chatGPTNativeSearchURL = "https://chatgpt.com/backend-api/codex/alpha/search"

const openAINativeSearchBodyLimit int64 = 8 << 20

var openAINativeSearchInvalidResponseBody = []byte(`{"error":{"type":"upstream_error","message":"Native Search upstream returned an invalid response"}}`)

type OpenAINativeSearchResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

func (s *OpenAIGatewayService) ForwardNativeSearch(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	clientHeaders http.Header,
	body []byte,
) (*OpenAINativeSearchResponse, error) {
	if s == nil || s.httpUpstream == nil {
		return nil, errors.New("native Search upstream is not configured")
	}
	if account == nil || !account.IsOpenAIOAuth() {
		return nil, errors.New("native Search requires an OpenAI OAuth account")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if blocked := s.blockOpenAIRuntimeGuardLearnedRequest(c, account, model, "search"); blocked != nil {
		return nil, blocked
	}

	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("get native Search access token: %w", err)
	}
	req, err := s.buildNativeSearchRequest(ctx, account, clientHeaders, body, token)
	if err != nil {
		return nil, err
	}

	resp, err := s.sendOpenAIHTTPRequest(ctx, c, req, account)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if isOpenAIEgressPolicyError(err) {
			return nil, err
		}
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody := s.readUpstreamErrorBody(resp)
		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody, model, "search")
		if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
			return nil, &UpstreamFailoverError{
				StatusCode:             resp.StatusCode,
				ResponseBody:           nativeSearchUpstreamErrorBody(resp.StatusCode),
				RetryableOnSameAccount: isOpenAIUpstreamRetryableOnSameAccount(account, resp.StatusCode, upstreamMsg, respBody),
			}
		}
		return &OpenAINativeSearchResponse{
			StatusCode: resp.StatusCode,
			Headers:    nativeSearchResponseHeaders(resp.Header),
			Body:       nativeSearchUpstreamErrorBody(resp.StatusCode),
		}, nil
	}

	respBody, err := readOpenAINativeSearchBody(resp.Body)
	if err != nil {
		return nil, &UpstreamFailoverError{
			StatusCode:   http.StatusBadGateway,
			ResponseBody: openAINativeSearchInvalidResponseBody,
		}
	}
	if err := validateOpenAINativeSearchResponse(respBody); err != nil {
		return nil, &UpstreamFailoverError{
			StatusCode:   http.StatusBadGateway,
			ResponseBody: openAINativeSearchInvalidResponseBody,
		}
	}

	return &OpenAINativeSearchResponse{
		StatusCode: resp.StatusCode,
		Headers:    nativeSearchResponseHeaders(resp.Header),
		Body:       respBody,
	}, nil
}

func (s *OpenAIGatewayService) buildNativeSearchRequest(
	ctx context.Context,
	account *Account,
	clientHeaders http.Header,
	body []byte,
	token string,
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatGPTNativeSearchURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build native Search request: %w", err)
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	req.Host = "chatgpt.com"

	copyNativeSearchIdentityHeaders(req.Header, clientHeaders)
	account.ApplyHeaderOverrides(req.Header)
	profile := buildOpenAIGatewayFallbackProfile(req.Header)
	if s.gatewayCoreService != nil {
		if runtime, runtimeErr := s.gatewayCoreService.ResolveAccountRuntime(ctx, account, clientHeaders, OpenAIClientTransportHTTP); runtimeErr == nil && runtime != nil && runtime.Profile != nil {
			profile = runtime.Profile
		}
	}
	artifact := BuildOpenAIGatewayProfileArtifact(
		profile,
		OpenAIGatewayProfileRouteNativeSearch,
		OpenAIGatewayProfileArtifactOptions{
			RequestedOriginator: req.Header.Get("originator"),
			IsOfficialClient:    openai.IsCodexOfficialClientRequest(req.Header.Get("User-Agent")),
		},
	)
	if customUA := account.GetOpenAIUserAgent(); customUA != "" {
		artifact = artifact.WithUserAgentOverride(customUA)
	}
	if s.cfg != nil && s.cfg.Gateway.ForceCodexCLI {
		artifact = artifact.ForceCodexCLI()
	}
	artifact.ApplyHTTP(req.Header)

	if err := resolveAndSetOpenAIChatGPTAccountHeaders(ctx, s.accountRepo, req.Header, account); err != nil {
		return nil, fmt.Errorf("resolve native Search account headers: %w", err)
	}
	req.Header.Del("x-api-key")
	req.Header.Del("x-goog-api-key")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	enforceCodexIdentityHeaders(req.Header)
	return req, nil
}

func copyNativeSearchIdentityHeaders(dst, src http.Header) {
	if dst == nil || src == nil {
		return
	}
	allowed := map[string]bool{
		"user-agent":                  true,
		"originator":                  true,
		"accept-language":             true,
		"x-stainless-lang":            true,
		"x-stainless-package-version": true,
		"x-stainless-os":              true,
		"x-stainless-arch":            true,
		"x-stainless-runtime":         true,
		"x-stainless-runtime-version": true,
	}
	for key, values := range src {
		if !allowed[strings.ToLower(strings.TrimSpace(key))] {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func readOpenAINativeSearchBody(body io.Reader) ([]byte, error) {
	if body == nil {
		return nil, errors.New("native Search response body is missing")
	}
	data, err := io.ReadAll(io.LimitReader(body, openAINativeSearchBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > openAINativeSearchBodyLimit {
		return nil, errors.New("native Search response exceeds size limit")
	}
	return data, nil
}

func validateOpenAINativeSearchResponse(body []byte) error {
	var object map[string]json.RawMessage
	if len(bytes.TrimSpace(body)) == 0 || json.Unmarshal(body, &object) != nil || object == nil {
		return errors.New("native Search response must be a JSON object")
	}
	outputRaw, ok := object["output"]
	if !ok {
		return errors.New("native Search response output is missing")
	}
	var output string
	if err := json.Unmarshal(outputRaw, &output); err != nil {
		return errors.New("native Search response output must be a string")
	}
	if encryptedRaw, exists := object["encrypted_output"]; exists && !bytes.Equal(bytes.TrimSpace(encryptedRaw), []byte("null")) {
		var encrypted string
		if err := json.Unmarshal(encryptedRaw, &encrypted); err != nil {
			return errors.New("native Search response encrypted_output must be a string or null")
		}
	}
	return nil
}

func nativeSearchResponseHeaders(upstream http.Header) http.Header {
	headers := http.Header{"Content-Type": []string{"application/json"}}
	for _, key := range []string{"X-Request-Id", "Request-Id"} {
		if value := strings.TrimSpace(upstream.Get(key)); value != "" {
			headers.Set(key, value)
		}
	}
	return headers
}

func nativeSearchUpstreamErrorBody(statusCode int) []byte {
	message := "Native Search upstream request failed"
	if statusCode == http.StatusTooManyRequests {
		message = "Native Search upstream rate limit exceeded"
	} else if statusCode == http.StatusBadGateway || statusCode == http.StatusServiceUnavailable || statusCode == http.StatusGatewayTimeout || statusCode >= http.StatusInternalServerError {
		message = "Native Search upstream is temporarily unavailable"
	}
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"type":    "upstream_error",
			"message": message,
		},
	})
	return body
}
