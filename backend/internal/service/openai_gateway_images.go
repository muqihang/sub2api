package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const openaiPlatformAPIImagesGenerationsURL = "https://api.openai.com/v1/images/generations"

func buildOpenAIImageGenerationsURL(base string) string {
	normalized := strings.TrimRight(strings.TrimSpace(base), "/")
	switch {
	case strings.HasSuffix(normalized, "/images/generations"):
		return normalized
	case strings.HasSuffix(normalized, "/images"):
		return normalized + "/generations"
	case strings.HasSuffix(normalized, "/v1"):
		return normalized + "/images/generations"
	default:
		return normalized + "/v1/images/generations"
	}
}

func normalizeOpenAIImageSizeFromRequest(body []byte) string {
	size := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "size").String()))
	switch size {
	case "1024x1024", "1024":
		return "1K"
	case "1536x1024", "1024x1536", "1792x1024", "1024x1792", "2048x2048", "2048":
		return "2K"
	case "4096x4096", "4096":
		return "4K"
	default:
		return "1K"
	}
}

func countOpenAIImagesInResponse(body []byte) int {
	if !gjson.ValidBytes(body) {
		return 0
	}
	data := gjson.GetBytes(body, "data")
	if !data.Exists() {
		return 0
	}
	if data.IsArray() {
		return len(data.Array())
	}
	return 1
}

func writeOpenAICompatError(c *gin.Context, statusCode int, errType, message string) {
	if c == nil {
		return
	}
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

func (s *OpenAIGatewayService) handleImageGenerationErrorResponse(resp *http.Response, c *gin.Context, account *Account) error {
	if resp == nil {
		return fmt.Errorf("upstream image request failed: empty response")
	}
	_, err := s.handleCompatErrorResponse(resp, c, account, writeOpenAICompatError)
	return err
}

func (s *OpenAIGatewayService) SelectAPIKeyAccountForModel(ctx context.Context, groupID *int64, requestedModel string, excludedIDs map[int64]struct{}) (*Account, error) {
	accounts, err := s.listSchedulableAccounts(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("query accounts failed: %w", err)
	}
	filtered := make([]Account, 0, len(accounts))
	for i := range accounts {
		if accounts[i].Type != AccountTypeAPIKey {
			continue
		}
		filtered = append(filtered, accounts[i])
	}
	selected, _ := s.selectBestAccount(ctx, groupID, filtered, requestedModel, excludedIDs, false)
	if selected == nil {
		if requestedModel != "" {
			return nil, fmt.Errorf("no available OpenAI api_key accounts supporting model: %s", requestedModel)
		}
		return nil, fmt.Errorf("no available OpenAI api_key accounts")
	}
	return s.hydrateSelectedAccount(ctx, selected)
}

func (s *OpenAIGatewayService) ForwardImageGeneration(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	requestedModel string,
	defaultMappedModel string,
) (*OpenAIForwardResult, []byte, http.Header, error) {
	startTime := time.Now()

	billingModel := resolveOpenAIForwardModel(account, requestedModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)

	rewrittenBody, err := sjson.SetBytes(body, "model", upstreamModel)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("rewrite image generation model: %w", err)
	}

	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("get access token: %w", err)
	}

	targetURL := openaiPlatformAPIImagesGenerationsURL
	switch account.Type {
	case AccountTypeAPIKey:
		baseURL := account.GetOpenAIBaseURL()
		if baseURL != "" {
			validatedURL, validateErr := s.validateUpstreamBaseURL(baseURL)
			if validateErr != nil {
				return nil, nil, nil, validateErr
			}
			targetURL = buildOpenAIImageGenerationsURL(validatedURL)
		}
	default:
		return nil, nil, nil, fmt.Errorf("openai image generation currently requires api_key account")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(rewrittenBody))
	if err != nil {
		return nil, nil, nil, err
	}

	if c != nil && c.Request != nil {
		for key, values := range c.Request.Header {
			lower := strings.ToLower(strings.TrimSpace(key))
			if lower != "content-type" && lower != "accept" {
				continue
			}
			for _, v := range values {
				req.Header.Add(key, v)
			}
		}
	}
	req.Header.Del("authorization")
	req.Header.Set("authorization", "Bearer "+token)
	if req.Header.Get("content-type") == "" {
		req.Header.Set("content-type", "application/json")
	}
	if req.Header.Get("accept") == "" {
		req.Header.Set("accept", "application/json")
	}

	egress, err := s.resolveOpenAIEgress(ctx, account)
	if err != nil {
		return nil, nil, nil, err
	}
	resp, err := s.httpUpstream.Do(req, egress.ProxyURL, account.ID, account.Concurrency)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("upstream image request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if readErr != nil {
			return nil, nil, nil, fmt.Errorf("read upstream image error response: %w", readErr)
		}
		if c != nil && account != nil {
			upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
			upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
			if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
				if s.rateLimitService != nil {
					s.rateLimitService.HandleUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
				}
				return nil, nil, resp.Header.Clone(), &UpstreamFailoverError{
					StatusCode:             resp.StatusCode,
					ResponseBody:           respBody,
					RetryableOnSameAccount: account.IsPoolMode() && isPoolModeRetryableStatus(resp.StatusCode),
				}
			}
			resp.Body = io.NopCloser(bytes.NewReader(respBody))
			return nil, nil, resp.Header.Clone(), s.handleImageGenerationErrorResponse(resp, c, account)
		}
		return nil, respBody, resp.Header.Clone(), fmt.Errorf("upstream image request failed: status %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read upstream image response: %w", err)
	}

	return &OpenAIForwardResult{
		RequestID:       resp.Header.Get("x-request-id"),
		Model:           requestedModel,
		BillingModel:    billingModel,
		UpstreamModel:   upstreamModel,
		Stream:          false,
		OpenAIWSMode:    false,
		ImageCount:      countOpenAIImagesInResponse(respBody),
		ImageSize:       normalizeOpenAIImageSizeFromRequest(rewrittenBody),
		ResponseHeaders: resp.Header.Clone(),
		Duration:        time.Since(startTime),
	}, respBody, resp.Header.Clone(), nil
}
