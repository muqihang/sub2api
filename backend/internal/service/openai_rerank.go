package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type openAIRerankRequestMeta struct {
	documents       []string
	topN            int
	returnDocuments bool
}

type openAIRerankText struct {
	Text string `json:"text"`
}

type openAIRerankUpstreamRequest struct {
	Model    string             `json:"model"`
	Query    openAIRerankText   `json:"query"`
	Passages []openAIRerankText `json:"passages"`
	Truncate string             `json:"truncate"`
}

func (s *OpenAIGatewayService) ForwardRerank(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()

	originalModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if originalModel == "" {
		writeOpenAIRerankError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}

	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	upstreamBody, requestMeta, err := normalizeOpenAIRerankRequest(body, upstreamModel)
	if err != nil {
		writeOpenAIRerankError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}

	logger.L().Debug("openai rerank: forwarding",
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
	targetURL := buildOpenAIRerankURL(validatedURL, upstreamModel)

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
		if openaiCCRawAllowedHeaders[strings.ToLower(key)] {
			for _, value := range values {
				upstreamReq.Header.Add(key, value)
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

	if resp.StatusCode >= http.StatusBadRequest {
		respBody := s.readUpstreamErrorBody(resp)
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(respBody))

		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
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
			s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody, upstreamModel, "rerank")
			return nil, &UpstreamFailoverError{
				StatusCode:             resp.StatusCode,
				RetryableOnSameAccount: account.IsPoolMode() && !isOpenAIInsufficientBalanceError(respBody, upstreamMsg) && account.IsPoolModeRetryableStatus(resp.StatusCode),
			}
		}
		writeOpenAIRerankError(c, resp.StatusCode, "upstream_error", "Upstream request failed")
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeOpenAIRerankError(c, http.StatusBadGateway, "api_error", "Failed to read upstream response")
		}
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	normalizedBody, err := normalizeOpenAIRerankResponse(respBody, originalModel, requestMeta)
	if err != nil {
		writeOpenAIRerankError(c, http.StatusBadGateway, "api_error", "Invalid rerank response from upstream")
		return nil, fmt.Errorf("normalize upstream rerank response: %w", err)
	}

	writeOpenAIRerankUpstreamResponse(c, resp, normalizedBody, s.responseHeaderFilter)

	return &OpenAIForwardResult{
		RequestID:     firstNonEmptyString(resp.Header.Get("x-request-id"), resp.Header.Get("request-id")),
		Usage:         OpenAIUsage{},
		Model:         originalModel,
		BillingModel:  billingModel,
		UpstreamModel: upstreamModel,
		Stream:        false,
		Duration:      time.Since(startTime),
	}, nil
}

func normalizeOpenAIRerankRequest(body []byte, upstreamModel string) ([]byte, openAIRerankRequestMeta, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, openAIRerankRequestMeta{}, fmt.Errorf("failed to parse request body")
	}

	query, err := openAIRerankTextValue(raw["query"])
	if err != nil || query == "" {
		return nil, openAIRerankRequestMeta{}, fmt.Errorf("query must be a non-empty string or object with a text field")
	}

	documentRaw := raw["documents"]
	if len(documentRaw) == 0 {
		documentRaw = raw["passages"]
	}
	documents, err := openAIRerankTextList(documentRaw)
	if err != nil || len(documents) == 0 {
		return nil, openAIRerankRequestMeta{}, fmt.Errorf("documents must contain at least one string or object with a text field")
	}

	meta := openAIRerankRequestMeta{documents: documents}
	_ = json.Unmarshal(raw["top_n"], &meta.topN)
	_ = json.Unmarshal(raw["return_documents"], &meta.returnDocuments)
	truncate := "NONE"
	if value := raw["truncate"]; len(value) > 0 {
		var configured string
		if json.Unmarshal(value, &configured) == nil && strings.TrimSpace(configured) != "" {
			truncate = strings.ToUpper(strings.TrimSpace(configured))
		}
	}

	passages := make([]openAIRerankText, 0, len(documents))
	for _, document := range documents {
		passages = append(passages, openAIRerankText{Text: document})
	}
	upstreamBody, err := json.Marshal(openAIRerankUpstreamRequest{
		Model:    upstreamModel,
		Query:    openAIRerankText{Text: query},
		Passages: passages,
		Truncate: truncate,
	})
	if err != nil {
		return nil, openAIRerankRequestMeta{}, fmt.Errorf("failed to build upstream request: %w", err)
	}
	return upstreamBody, meta, nil
}

func openAIRerankTextValue(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("missing text")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return strings.TrimSpace(value), nil
	}
	var object openAIRerankText
	if err := json.Unmarshal(raw, &object); err != nil {
		return "", err
	}
	return strings.TrimSpace(object.Text), nil
}

func openAIRerankTextList(raw json.RawMessage) ([]string, error) {
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	documents := make([]string, 0, len(values))
	for _, value := range values {
		text, err := openAIRerankTextValue(value)
		if err != nil || text == "" {
			return nil, fmt.Errorf("invalid document")
		}
		documents = append(documents, text)
	}
	return documents, nil
}

func normalizeOpenAIRerankResponse(body []byte, requestedModel string, meta openAIRerankRequestMeta) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	rankingsRaw := raw["rankings"]
	if len(rankingsRaw) == 0 {
		rankingsRaw = raw["results"]
	}
	var rankings []map[string]json.RawMessage
	if err := json.Unmarshal(rankingsRaw, &rankings); err != nil {
		return nil, err
	}

	limit := len(rankings)
	if meta.topN > 0 && meta.topN < limit {
		limit = meta.topN
	}
	results := make([]map[string]any, 0, limit)
	for _, ranking := range rankings[:limit] {
		var index int
		if err := json.Unmarshal(ranking["index"], &index); err != nil || index < 0 || index >= len(meta.documents) {
			return nil, fmt.Errorf("invalid ranking index")
		}
		score, ok := openAIRerankScore(ranking)
		if !ok {
			return nil, fmt.Errorf("missing ranking score")
		}
		result := map[string]any{
			"index":           index,
			"relevance_score": score,
		}
		if meta.returnDocuments {
			result["document"] = openAIRerankText{Text: meta.documents[index]}
		}
		results = append(results, result)
	}

	return json.Marshal(map[string]any{
		"object":  "list",
		"model":   requestedModel,
		"results": results,
	})
}

func openAIRerankScore(ranking map[string]json.RawMessage) (float64, bool) {
	for _, key := range []string{"relevance_score", "score", "logit"} {
		var score float64
		if raw := ranking[key]; len(raw) > 0 && json.Unmarshal(raw, &score) == nil {
			return score, true
		}
	}
	return 0, false
}

func buildOpenAIRerankURL(base, upstreamModel string) string {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err == nil && strings.EqualFold(parsed.Hostname(), "integrate.api.nvidia.com") {
		model := strings.Trim(strings.TrimSpace(upstreamModel), "/")
		if strings.HasPrefix(strings.ToLower(model), "nvidia/") && strings.Contains(strings.ToLower(model), "rerank") {
			return "https://ai.api.nvidia.com/v1/retrieval/" + model + "/reranking"
		}
	}
	return buildOpenAIEndpointURL(base, "/v1/rerank")
}

func writeOpenAIRerankUpstreamResponse(c *gin.Context, resp *http.Response, body []byte, filter *responseheaders.CompiledHeaderFilter) {
	if c == nil || resp == nil || c.Writer.Written() {
		return
	}
	if resp.Header != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, filter)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(body)
}

func writeOpenAIRerankError(c *gin.Context, statusCode int, errType, message string) {
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}
