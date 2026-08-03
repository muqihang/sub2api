package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
)

func createOpenAIWebSearchProbePayload(model string) map[string]any {
	return map[string]any{
		"model": strings.TrimSpace(model),
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "input_text",
						"text": "Use web search to find the current UTC date, then answer with only that date.",
					},
				},
			},
		},
		"tools":       []any{map[string]any{"type": "web_search"}},
		"tool_choice": "required",
		"store":       false,
		"stream":      false,
	}
}

func isOpenAIWebSearchProbeSuccess(status int, body []byte) bool {
	supported, known := decideOpenAIWebSearchProbeSupport(status, body)
	return known && supported
}

func decideOpenAIWebSearchProbeSupport(status int, body []byte) (supported bool, known bool) {
	if status < 200 || status >= 300 {
		// Authentication, quota, rate-limit, and upstream failures do not prove
		// that the endpoint lacks hosted web_search.
		return false, false
	}
	var payload struct {
		Output []struct {
			Type string `json:"type"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false, false
	}
	for _, item := range payload.Output {
		if strings.EqualFold(strings.TrimSpace(item.Type), "web_search_call") {
			return true, true
		}
	}
	if len(payload.Output) == 0 {
		return false, false
	}
	return false, true
}

func buildOpenAIWebSearchProbeExtraUpdates(resp *http.Response, body []byte, probeErr error, now time.Time) map[string]any {
	updates := map[string]any{
		"openai_web_search_checked_at":  now.Format(time.RFC3339),
		"openai_web_search_last_status": nil,
	}
	if resp != nil {
		updates["openai_web_search_last_status"] = resp.StatusCode
	}

	supported, known := false, false
	if resp != nil && probeErr == nil {
		supported, known = decideOpenAIWebSearchProbeSupport(resp.StatusCode, body)
	}
	switch {
	case probeErr != nil:
		updates["openai_web_search_last_error"] = truncateString(sanitizeUpstreamErrorMessage(probeErr.Error()), 2048)
	case resp == nil:
		updates["openai_web_search_last_error"] = "web_search probe failed"
	case known:
		updates["openai_web_search_supported"] = supported
		if supported {
			updates["openai_web_search_last_error"] = ""
		} else {
			updates["openai_web_search_last_error"] = "HTTP 2xx without web_search_call"
		}
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		updates["openai_web_search_last_error"] = "HTTP 2xx without semantic probe output"
	default:
		message := strings.TrimSpace(extractUpstreamErrorMessage(body))
		if message == "" {
			message = "HTTP " + strconv.Itoa(resp.StatusCode)
		}
		updates["openai_web_search_last_error"] = truncateString(sanitizeUpstreamErrorMessage(message), 2048)
	}
	return updates
}

func (s *AccountTestService) ProbeOpenAIWebSearchSupportForModel(ctx context.Context, accountID int64, requestedModel string) {
	if s == nil || s.accountRepo == nil || accountID <= 0 {
		return
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		logger.LegacyPrintf("service.openai_probe", "web_search_probe_load_account_failed: account_id=%d err=%v", accountID, err)
		return
	}
	if account == nil || account.Platform != PlatformOpenAI ||
		(account.Type != AccountTypeAPIKey && account.Type != AccountTypeUpstream && account.Type != AccountTypeOAuth) {
		return
	}

	credentialAccount, authToken, apiURL, isOAuth, probeModel, err := s.prepareOpenAIWebSearchProbe(ctx, account, requestedModel)
	if err != nil {
		logger.LegacyPrintf("service.openai_probe", "web_search_probe_prepare_failed: account_id=%d err=%v", accountID, err)
		return
	}
	resp, body, requestErr := s.executeOpenAIWebSearchProbeRequest(ctx, account, credentialAccount, apiURL, authToken, isOAuth, probeModel)
	updates := buildOpenAIWebSearchProbeExtraUpdates(resp, body, requestErr, time.Now())
	if requestErr == nil && resp != nil {
		if supported, known := decideOpenAIWebSearchProbeSupport(resp.StatusCode, body); known {
			updates[openai_compat.ExtraKeyWebSearchSupported] = supported
			updates[openai_compat.ExtraKeyWebSearchProbeModel] = probeModel
			updates[openai_compat.ExtraKeyWebSearchProbeTarget] = account.OpenAIWebSearchTargetFingerprint()
		}
	}

	latest, latestErr := s.accountRepo.GetByID(ctx, accountID)
	if latestErr != nil || latest == nil || latest.OpenAIWebSearchTargetFingerprint() != account.OpenAIWebSearchTargetFingerprint() {
		logger.LegacyPrintf("service.openai_probe", "web_search_probe_stale_result_skipped: account_id=%d", accountID)
		return
	}
	if persistErr := s.accountRepo.UpdateExtra(ctx, accountID, updates); persistErr != nil {
		logger.LegacyPrintf("service.openai_probe", "web_search_probe_persist_failed: account_id=%d err=%v", accountID, persistErr)
		return
	}
	logger.LegacyPrintf("service.openai_probe", "web_search_probe_done: account_id=%d model=%s status=%d known=%t supported=%t", accountID, probeModel, responseStatus(resp), hasWebSearchProbeEvidence(updates), webSearchProbeSupported(updates))
}

func (s *AccountTestService) prepareOpenAIWebSearchProbe(ctx context.Context, account *Account, requestedModel string) (*Account, string, string, bool, string, error) {
	credentialAccount := account
	if account.IsCredentialShadow() {
		resolved, err := resolveCredentialAccount(ctx, s.accountRepo, account)
		if err != nil {
			return nil, "", "", false, "", err
		}
		credentialAccount = resolved
	}
	openAICredentials := NewOpenAIGatewayCredentials(s.cfg, nil)
	var authToken, apiURL string
	var isOAuth bool
	switch {
	case credentialAccount.IsOAuth():
		isOAuth = true
		token, err := openAICredentials.OpenAIAccessToken(credentialAccount)
		if err != nil {
			return nil, "", "", false, "", errors.New("no access token available")
		}
		authToken = token
		apiURL = chatgptCodexAPIURL
	case credentialAccount.Type == AccountTypeAPIKey || credentialAccount.Type == AccountTypeUpstream:
		key, err := openAICredentials.OpenAIAPIKey(credentialAccount)
		if err != nil {
			return nil, "", "", false, "", errors.New("no API key available")
		}
		authToken = key
		baseURL := credentialAccount.GetOpenAIBaseURL()
		if baseURL == "" {
			baseURL = "https://api.openai.com"
		}
		normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
		if err != nil {
			return nil, "", "", false, "", fmt.Errorf("invalid base URL: %w", err)
		}
		apiURL = buildOpenAIResponsesURL(normalizedBaseURL)
	default:
		return nil, "", "", false, "", fmt.Errorf("unsupported account type: %s", credentialAccount.Type)
	}
	return credentialAccount, authToken, apiURL, isOAuth, selectResponsesProbeModel(account, requestedModel), nil
}

func (s *AccountTestService) executeOpenAIWebSearchProbeRequest(ctx context.Context, account, credentialAccount *Account, apiURL, authToken string, isOAuth bool, probeModel string) (*http.Response, []byte, error) {
	payloadBytes, _ := json.Marshal(createOpenAIWebSearchProbePayload(probeModel))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, nil, err
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+authToken)
	if isOAuth {
		req.Host = "chatgpt.com"
		req.Header.Set("OpenAI-Beta", "responses=experimental")
		req.Header.Set("Originator", "codex_cli_rs")
		req.Header.Set("User-Agent", codexCLIUserAgent)
		setOpenAIChatGPTAccountHeaders(req.Header, credentialAccount)
	}
	credentialAccount.ApplyHeaderOverrides(req.Header)
	if isOAuth {
		enforceCodexIdentityHeaders(req.Header)
	}
	resp, requestErr := s.sendOpenAIAccountTestHTTPRequest(ctx, nil, req, account)
	if requestErr != nil {
		return nil, nil, requestErr
	}
	defer func() { _ = resp.Body.Close() }()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if readErr != nil {
		return resp, nil, readErr
	}
	return resp, body, nil
}

func responseStatus(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

func hasWebSearchProbeEvidence(updates map[string]any) bool {
	_, ok := updates[openai_compat.ExtraKeyWebSearchSupported]
	return ok
}

func webSearchProbeSupported(updates map[string]any) bool {
	supported, _ := updates[openai_compat.ExtraKeyWebSearchSupported].(bool)
	return supported
}

func (s *AccountTestService) testOpenAIWebSearchConnection(c *gin.Context, account *Account, modelID string) error {
	ctx := c.Request.Context()
	credentialAccount := account
	if account.IsCredentialShadow() {
		resolved, err := resolveCredentialAccount(ctx, s.accountRepo, account)
		if err != nil {
			return s.persistOpenAIWebSearchProbeFailure(c, account, err)
		}
		credentialAccount = resolved
	}

	openAICredentials := NewOpenAIGatewayCredentials(s.cfg, nil)
	var authToken, apiURL string
	var isOAuth bool
	switch {
	case credentialAccount.IsOAuth():
		isOAuth = true
		var err error
		authToken, err = openAICredentials.OpenAIAccessToken(credentialAccount)
		if err != nil {
			return s.persistOpenAIWebSearchProbeFailure(c, account, errors.New("no access token available"))
		}
		apiURL = chatgptCodexAPIURL
	case credentialAccount.Type == AccountTypeAPIKey || credentialAccount.Type == AccountTypeUpstream:
		var err error
		authToken, err = openAICredentials.OpenAIAPIKey(credentialAccount)
		if err != nil {
			return s.persistOpenAIWebSearchProbeFailure(c, account, errors.New("no API key available"))
		}
		baseURL := credentialAccount.GetOpenAIBaseURL()
		if baseURL == "" {
			baseURL = "https://api.openai.com"
		}
		normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
		if err != nil {
			return s.persistOpenAIWebSearchProbeFailure(c, account, fmt.Errorf("invalid base URL: %w", err))
		}
		apiURL = buildOpenAIResponsesURL(normalizedBaseURL)
	default:
		return s.persistOpenAIWebSearchProbeFailure(c, account, fmt.Errorf("unsupported account type: %s", credentialAccount.Type))
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()
	s.sendEvent(c, TestEvent{Type: "test_start", Model: modelID})
	s.sendEvent(c, TestEvent{Type: "status", Text: "正在验证 hosted web_search 能力"})

	payloadBytes, _ := json.Marshal(createOpenAIWebSearchProbePayload(modelID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return s.persistOpenAIWebSearchProbeFailure(c, account, err)
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+authToken)
	if isOAuth {
		req.Host = "chatgpt.com"
		req.Header.Set("OpenAI-Beta", "responses=experimental")
		req.Header.Set("Originator", "codex_cli_rs")
		req.Header.Set("User-Agent", codexCLIUserAgent)
		setOpenAIChatGPTAccountHeaders(req.Header, credentialAccount)
	}
	account.ApplyHeaderOverrides(req.Header)
	if isOAuth {
		enforceCodexIdentityHeaders(req.Header)
	}

	resp, requestErr := s.sendOpenAIAccountTestHTTPRequest(ctx, c, req, account)
	if requestErr != nil {
		return s.persistOpenAIWebSearchProbeFailure(c, account, requestErr)
	}
	defer func() { _ = resp.Body.Close() }()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if readErr != nil {
		return s.persistOpenAIWebSearchProbeFailure(c, account, readErr)
	}
	updates := buildOpenAIWebSearchProbeExtraUpdates(resp, body, nil, time.Now())
	if _, known := updates[openai_compat.ExtraKeyWebSearchSupported]; known {
		updates[openai_compat.ExtraKeyWebSearchProbeModel] = strings.TrimSpace(modelID)
		updates[openai_compat.ExtraKeyWebSearchProbeTarget] = account.OpenAIWebSearchTargetFingerprint()
	}
	if s.accountRepo != nil {
		_ = s.accountRepo.UpdateExtra(ctx, account.ID, updates)
		mergeAccountExtra(account, updates)
		if resp.StatusCode == http.StatusTooManyRequests {
			s.reconcileOpenAI429State(ctx, account, resp.Header, body)
		}
		if resp.StatusCode == http.StatusUnauthorized {
			_ = s.accountRepo.SetError(ctx, account.ID, fmt.Sprintf("Web search authentication failed (401): %s", string(body)))
		}
	}
	if !isOpenAIWebSearchProbeSuccess(resp.StatusCode, body) {
		return s.sendErrorAndEnd(c, fmt.Sprint(updates["openai_web_search_last_error"]))
	}
	s.sendEvent(c, TestEvent{Type: "content", Text: "Hosted web_search probe succeeded"})
	s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
	return nil
}

func (s *AccountTestService) persistOpenAIWebSearchProbeFailure(c *gin.Context, account *Account, probeErr error) error {
	if s.accountRepo != nil && account != nil {
		updates := buildOpenAIWebSearchProbeExtraUpdates(nil, nil, probeErr, time.Now())
		_ = s.accountRepo.UpdateExtra(c.Request.Context(), account.ID, updates)
		mergeAccountExtra(account, updates)
	}
	return s.sendErrorAndEnd(c, probeErr.Error())
}
