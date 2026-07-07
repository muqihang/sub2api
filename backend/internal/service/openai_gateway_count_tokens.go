package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type openAIInputTokensCountRequest struct {
	Model        string                    `json:"model"`
	Instructions string                    `json:"instructions,omitempty"`
	Input        json.RawMessage           `json:"input,omitempty"`
	Tools        []apicompat.ResponsesTool `json:"tools,omitempty"`
	ToolChoice   json.RawMessage           `json:"tool_choice,omitempty"`
}

// ForwardCountTokensAsAnthropic bridges Anthropic-compatible count_tokens to
// OpenAI Responses input_tokens. It intentionally does not take concurrency
// slots, record usage, or emit raw upstream body/account runtime logs.
func (s *OpenAIGatewayService) ForwardCountTokensAsAnthropic(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) error {
	if s == nil || s.httpUpstream == nil {
		writeAnthropicCountTokensError(c, http.StatusServiceUnavailable, "api_error", "OpenAI gateway upstream is not configured")
		return fmt.Errorf("count_tokens upstream is not configured")
	}
	if account == nil {
		writeAnthropicCountTokensError(c, http.StatusServiceUnavailable, "api_error", "No available OpenAI accounts")
		return fmt.Errorf("count_tokens missing account")
	}

	var anthropicReq apicompat.AnthropicRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		writeAnthropicCountTokensError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return fmt.Errorf("parse count_tokens request: %w", err)
	}
	applyOpenAICompatModelNormalization(&anthropicReq)
	billingModel := resolveOpenAIForwardModel(account, anthropicReq.Model, strings.TrimSpace(defaultMappedModel))
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)

	responsesReq, err := apicompat.AnthropicToResponses(&anthropicReq)
	if err != nil {
		writeAnthropicCountTokensError(c, http.StatusBadRequest, "invalid_request_error", "Failed to convert request body")
		return fmt.Errorf("convert count_tokens request: %w", err)
	}
	countReq := openAIInputTokensCountRequest{
		Model:        upstreamModel,
		Instructions: responsesReq.Instructions,
		Input:        responsesReq.Input,
		Tools:        responsesReq.Tools,
		ToolChoice:   responsesReq.ToolChoice,
	}
	upstreamBody, err := marshalOpenAIUpstreamJSON(countReq)
	if err != nil {
		writeAnthropicCountTokensError(c, http.StatusInternalServerError, "api_error", "Failed to build request")
		return fmt.Errorf("marshal input_tokens request: %w", err)
	}

	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		writeAnthropicCountTokensError(c, http.StatusBadGateway, "upstream_error", "Failed to get access token")
		return fmt.Errorf("get OpenAI access token: %w", err)
	}
	req, err := s.buildInputTokensUpstreamRequest(ctx, c, account, upstreamBody, token)
	if err != nil {
		writeAnthropicCountTokensError(c, http.StatusInternalServerError, "api_error", "Failed to build request")
		return fmt.Errorf("build input_tokens request: %w", err)
	}

	proxyURL := resolveOpenAIAccountProxyURL(account)
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setOpsUpstreamError(c, 0, safeErr, "")
		writeAnthropicCountTokensError(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
		return fmt.Errorf("input_tokens upstream transport failed: %s", safeErr)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody := s.readUpstreamErrorBody(resp)
		if account.Type == AccountTypeOAuth && isOpenAIOAuthInputTokensUnsupported(resp.StatusCode, respBody) {
			setOpsUpstreamError(c, resp.StatusCode, "input_tokens_oauth_scope_fallback", "")
			c.JSON(http.StatusOK, gin.H{"input_tokens": estimateOpenAIInputTokensCountRequest(countReq)})
			return nil
		}
		if s.rateLimitService != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
		}
		safeSummary := safeOpenAIInputTokensUpstreamErrorSummary(resp.StatusCode, respBody)
		setOpsUpstreamError(c, resp.StatusCode, safeSummary, "")
		if isOpenAIInputTokensUnsupported(resp.StatusCode, respBody) {
			writeAnthropicCountTokensError(c, http.StatusNotFound, "not_found_error", "Token counting is not supported by upstream")
			return nil
		}
		message := "Upstream request failed"
		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			message = "Rate limit exceeded"
		case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			message = "Upstream service temporarily unavailable"
		}
		writeAnthropicCountTokensError(c, resp.StatusCode, "upstream_error", message)
		if safeSummary == "" {
			return fmt.Errorf("input_tokens upstream error: status=%d", resp.StatusCode)
		}
		return fmt.Errorf("input_tokens upstream error: status=%d class=%s", resp.StatusCode, safeSummary)
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, openAIUpstreamErrorBodyReadLimitForConfig(s.cfg)))
	if err != nil {
		writeAnthropicCountTokensError(c, http.StatusBadGateway, "upstream_error", "Failed to read response")
		return fmt.Errorf("read input_tokens response: %w", err)
	}
	inputTokens := gjson.GetBytes(respBody, "input_tokens")
	if !inputTokens.Exists() {
		writeAnthropicCountTokensError(c, http.StatusBadGateway, "upstream_error", "Upstream response missing input_tokens")
		return fmt.Errorf("input_tokens response missing input_tokens")
	}
	c.JSON(http.StatusOK, gin.H{"input_tokens": int(inputTokens.Int())})
	return nil
}

func (s *OpenAIGatewayService) buildInputTokensUpstreamRequest(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	token string,
) (*http.Request, error) {
	targetURL := openaiPlatformAPIInputTokensURL
	if account.Type == AccountTypeAPIKey || account.Type == AccountTypeUpstream {
		if baseURL := account.GetOpenAIBaseURL(); strings.TrimSpace(baseURL) != "" {
			validatedURL, err := s.validateUpstreamBaseURL(baseURL)
			if err != nil {
				return nil, err
			}
			targetURL = buildOpenAIResponsesInputTokensURL(validatedURL)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c != nil && c.Request != nil {
		for key, values := range c.Request.Header {
			lower := strings.ToLower(strings.TrimSpace(key))
			if lower != "user-agent" && lower != "accept-language" {
				continue
			}
			for _, v := range values {
				req.Header.Add(key, v)
			}
		}
	}
	customUA := account.GetOpenAIUserAgent()
	if customUA != "" {
		req.Header.Set("User-Agent", customUA)
	}
	return req, nil
}

func writeAnthropicCountTokensError(c *gin.Context, status int, errType, message string) {
	if c == nil {
		return
	}
	c.JSON(status, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

func isOpenAIInputTokensUnsupported(statusCode int, body []byte) bool {
	if statusCode != http.StatusNotFound {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	return strings.Contains(msg, "input_tokens") && strings.Contains(msg, "not found")
}

func safeOpenAIInputTokensUpstreamErrorSummary(statusCode int, body []byte) string {
	if isOpenAIInputTokensUnsupported(statusCode, body) {
		return "input_tokens_unsupported"
	}
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "input_tokens_auth_error"
	case http.StatusNotFound:
		return "input_tokens_not_found"
	case http.StatusTooManyRequests:
		return "input_tokens_rate_limited"
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return "input_tokens_server_error"
	default:
		if statusCode >= 400 {
			return "input_tokens_upstream_error"
		}
		return ""
	}
}

func isOpenAIOAuthInputTokensUnsupported(statusCode int, body []byte) bool {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
	default:
		return false
	}

	bodyLower := strings.ToLower(string(body))
	msg := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	code := strings.ToLower(strings.TrimSpace(extractUpstreamErrorCode(body)))

	if code == "missing_scope" ||
		strings.Contains(bodyLower, "api.responses.write") ||
		strings.Contains(bodyLower, "missing scopes") ||
		strings.Contains(bodyLower, "insufficient_scope") {
		return true
	}
	if statusCode == http.StatusNotFound && isOpenAIInputTokensUnsupported(statusCode, body) {
		return true
	}
	return strings.Contains(msg, "input_tokens") &&
		(strings.Contains(msg, "not found") ||
			strings.Contains(msg, "not supported") ||
			strings.Contains(msg, "unsupported"))
}

func estimateOpenAIInputTokensCountRequest(req openAIInputTokensCountRequest) int {
	estimated := estimateOpenAIInputTokens(req)
	if estimated < 1 {
		return 1
	}
	return estimated
}
