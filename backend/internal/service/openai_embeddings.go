package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

func (s *OpenAIGatewayService) ForwardEmbeddings(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()

	originalModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if originalModel == "" {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}

	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	if account.Type == AccountTypeOAuth {
		if blocked := s.blockOpenAIRuntimeGuardLearnedRequest(c, account, upstreamModel, "embeddings"); blocked != nil {
			return nil, blocked
		}
	}
	upstreamBody := body
	if upstreamModel != originalModel {
		upstreamBody = ReplaceModelInBody(body, upstreamModel)
	}
	if strings.TrimSpace(gjson.GetBytes(upstreamBody, "input_type").String()) == "" {
		if inputType, ok := configuredOpenAIEmbeddingDefaultInputType(account, originalModel); ok {
			configuredBody, setErr := sjson.SetBytes(upstreamBody, "input_type", inputType)
			if setErr != nil {
				writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", "invalid embeddings request")
				return nil, fmt.Errorf("set configured embedding input type: %w", setErr)
			}
			upstreamBody = configuredBody
		}
	}
	if dimensions, ok := configuredOpenAIEmbeddingDimensions(account, originalModel); ok {
		var configuredBody []byte
		var setErr error
		if configuredOpenAIEmbeddingOmitDimensions(account, originalModel) {
			configuredBody, setErr = sjson.DeleteBytes(upstreamBody, "dimensions")
		} else {
			configuredBody, setErr = sjson.SetBytes(upstreamBody, "dimensions", dimensions)
		}
		if setErr != nil {
			writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", "invalid embeddings request")
			return nil, fmt.Errorf("set configured embedding dimensions: %w", setErr)
		}
		upstreamBody = configuredBody
	}

	logger.L().Debug("openai embeddings: forwarding",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
	)

	apiKey := account.GetOpenAIApiKey()
	if apiKey == "" {
		return nil, fmt.Errorf("account %d missing api_key", account.ID)
	}
	baseURL := account.GetOpenAIBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	validatedURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base_url: %w", err)
	}
	targetURL := buildOpenAIEmbeddingsURL(validatedURL)

	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(upstreamBody))
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	upstreamReq = upstreamReq.WithContext(WithHTTPUpstreamProfile(upstreamReq.Context(), HTTPUpstreamProfileOpenAI))
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)
	upstreamReq.Header.Set("Accept", "application/json")
	for key, values := range c.Request.Header {
		lowerKey := strings.ToLower(key)
		if openaiCCRawAllowedHeaders[lowerKey] {
			for _, v := range values {
				upstreamReq.Header.Add(key, v)
			}
		}
	}
	if customUA := account.GetOpenAIUserAgent(); customUA != "" {
		upstreamReq.Header.Set("user-agent", customUA)
	}
	account.ApplyHeaderOverrides(upstreamReq.Header)

	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody := s.readUpstreamErrorBody(resp)
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(respBody))

		upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
		upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
		if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
			upstreamDetail := ""
			if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
				maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
				if maxBytes <= 0 {
					maxBytes = 2048
				}
				upstreamDetail = truncateString(string(respBody), maxBytes)
			}
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  resp.Header.Get("x-request-id"),
				Kind:               "failover",
				Message:            upstreamMsg,
				Detail:             upstreamDetail,
			})
			s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody, upstreamModel, "embeddings")
			return nil, &UpstreamFailoverError{
				StatusCode:             resp.StatusCode,
				RetryableOnSameAccount: account.IsPoolMode() && !isOpenAIInsufficientBalanceError(respBody, upstreamMsg) && account.IsPoolModeRetryableStatus(resp.StatusCode),
			}
		}
		writeOpenAIEmbeddingsError(c, resp.StatusCode, "upstream_error", "Upstream request failed")
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeOpenAIEmbeddingsError(c, http.StatusBadGateway, "api_error", "Failed to read upstream response")
		}
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	if dimensions, ok := configuredOpenAIEmbeddingDimensions(account, originalModel); ok {
		if actual, valid := openAIEmbeddingResponseDimension(respBody); !valid || actual != dimensions {
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:    account.Platform,
				AccountID:   account.ID,
				AccountName: account.Name,
				Kind:        "failover",
				Message:     "embedding response dimension mismatch",
			})
			return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway}
		}
	}

	clientBody, rewriteErr := sjson.SetBytes(respBody, "model", originalModel)
	if rewriteErr != nil {
		writeOpenAIEmbeddingsError(c, http.StatusBadGateway, "api_error", "Invalid embeddings response from upstream")
		return nil, fmt.Errorf("rewrite embeddings response model: %w", rewriteErr)
	}
	writeOpenAIEmbeddingsUpstreamResponse(c, resp, clientBody, s.responseHeaderFilter)

	return &OpenAIForwardResult{
		RequestID:     firstNonEmptyString(resp.Header.Get("x-request-id"), resp.Header.Get("request-id")),
		Usage:         extractOpenAIEmbeddingsUsage(respBody),
		Model:         originalModel,
		BillingModel:  billingModel,
		UpstreamModel: upstreamModel,
		Stream:        false,
		Duration:      time.Since(startTime),
	}, nil
}

func configuredOpenAIEmbeddingDefaultInputType(account *Account, requestedModel string) (string, bool) {
	if account == nil || account.Credentials == nil {
		return "", false
	}
	raw, ok := account.Credentials["embedding_default_input_type"]
	if !ok || raw == nil {
		return "", false
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return "", false
	}
	for model, candidate := range values {
		if !strings.EqualFold(strings.TrimSpace(model), strings.TrimSpace(requestedModel)) {
			continue
		}
		value, _ := candidate.(string)
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "query" || value == "passage" {
			return value, true
		}
		return "", false
	}
	return "", false
}

func configuredOpenAIEmbeddingOmitDimensions(account *Account, requestedModel string) bool {
	if account == nil || account.Credentials == nil {
		return false
	}
	raw, ok := account.Credentials["embedding_omit_dimensions"]
	if !ok || raw == nil {
		return false
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	for model, candidate := range values {
		if !strings.EqualFold(strings.TrimSpace(model), strings.TrimSpace(requestedModel)) {
			continue
		}
		value, _ := candidate.(bool)
		return value
	}
	return false
}

func openAIEmbeddingResponseDimension(body []byte) (int64, bool) {
	data := gjson.GetBytes(body, "data")
	if !data.Exists() || !data.IsArray() {
		return 0, false
	}
	var dimension int64
	valid := true
	count := 0
	data.ForEach(func(_, item gjson.Result) bool {
		embedding := item.Get("embedding")
		if !embedding.IsArray() {
			valid = false
			return false
		}
		current := int64(len(embedding.Array()))
		if current <= 0 || (count > 0 && current != dimension) {
			valid = false
			return false
		}
		dimension = current
		count++
		return true
	})
	return dimension, valid && count > 0
}

func configuredOpenAIEmbeddingDimensions(account *Account, requestedModel string) (int64, bool) {
	if account == nil || account.Credentials == nil {
		return 0, false
	}
	raw, ok := account.Credentials["embedding_dimensions"]
	if !ok || raw == nil {
		return 0, false
	}
	var value any
	switch configured := raw.(type) {
	case map[string]any:
		value, ok = configured[requestedModel]
		if !ok {
			for model, candidate := range configured {
				if strings.EqualFold(strings.TrimSpace(model), strings.TrimSpace(requestedModel)) {
					value, ok = candidate, true
					break
				}
			}
		}
	case map[string]int:
		value, ok = configured[requestedModel]
	case map[string]int64:
		value, ok = configured[requestedModel]
	default:
		return 0, false
	}
	if !ok {
		return 0, false
	}
	var dimensions int64
	switch number := value.(type) {
	case float64:
		dimensions = int64(number)
	case float32:
		dimensions = int64(number)
	case int:
		dimensions = int64(number)
	case int64:
		dimensions = number
	case int32:
		dimensions = int64(number)
	default:
		return 0, false
	}
	if dimensions <= 0 {
		return 0, false
	}
	return dimensions, true
}

func writeOpenAIEmbeddingsUpstreamResponse(c *gin.Context, resp *http.Response, body []byte, filter *responseheaders.CompiledHeaderFilter) {
	if c == nil || resp == nil {
		return
	}
	if c.Writer.Written() {
		return
	}
	if resp.Header != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, filter)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		c.Writer.Header().Set("Content-Type", ct)
	} else {
		c.Writer.Header().Set("Content-Type", "application/json")
	}
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(body)
}

func writeOpenAIEmbeddingsError(c *gin.Context, statusCode int, errType, message string) {
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

func extractOpenAIEmbeddingsUsage(body []byte) OpenAIUsage {
	usage := gjson.GetBytes(body, "usage")
	if !usage.Exists() || !usage.IsObject() {
		return OpenAIUsage{}
	}
	inputTokens := firstPositiveGJSONInt(
		usage.Get("prompt_tokens"),
		usage.Get("input_tokens"),
		usage.Get("total_tokens"),
	)
	outputTokens := firstPositiveGJSONInt(
		usage.Get("completion_tokens"),
		usage.Get("output_tokens"),
	)
	cacheReadTokens := openAICacheReadTokensFromUsage(usage)
	cacheCreationTokens := openAICacheCreationTokensFromUsage(usage)
	imageInputTokens := firstPositiveGJSONInt(
		usage.Get("prompt_tokens_details.image_tokens"),
		usage.Get("input_tokens_details.image_tokens"),
	)
	return OpenAIUsage{
		InputTokens:              inputTokens,
		ImageInputTokens:         imageInputTokens,
		OutputTokens:             outputTokens,
		CacheReadInputTokens:     cacheReadTokens,
		CacheCreationInputTokens: cacheCreationTokens,
	}
}

func firstPositiveGJSONInt(values ...gjson.Result) int {
	for _, value := range values {
		if !value.Exists() {
			continue
		}
		n := int(value.Int())
		if n > 0 {
			return n
		}
	}
	return 0
}

func buildOpenAIEmbeddingsURL(base string) string {
	return buildOpenAIEndpointURL(base, "/v1/embeddings")
}
