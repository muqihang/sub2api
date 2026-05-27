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
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/tidwall/gjson"
)

type CodexGatewayProviderUnavailableKind string

const (
	CodexGatewayProviderUnavailableNoProviderGroup CodexGatewayProviderUnavailableKind = "no_provider_group"
	CodexGatewayProviderUnavailableNoAccounts      CodexGatewayProviderUnavailableKind = "no_accounts"
)

type CodexGatewayProviderUnavailableError struct {
	ModelID  string
	Provider string
	Kind     CodexGatewayProviderUnavailableKind
}

func (e *CodexGatewayProviderUnavailableError) Error() string {
	if e == nil {
		return "<nil>"
	}
	switch e.Kind {
	case CodexGatewayProviderUnavailableNoProviderGroup:
		return fmt.Sprintf("codex gateway provider %q for model %q has no provider group configured", e.Provider, e.ModelID)
	default:
		return fmt.Sprintf("codex gateway provider %q for model %q has no available accounts", e.Provider, e.ModelID)
	}
}

type CodexGatewayProviderRequest struct {
	Request              CodexGatewayResponsesRequest
	Model                CodexGatewayModel
	Parsed               CodexGatewayResponsesCreateRequest
	SessionKey           string
	IsolationKey         string
	WorkspaceKey         string
	ManagedSessionBucket string
	CaptureTrace         *CodexGatewayTrace
}

type codexGatewayUsageRecorder interface {
	RecordUsage(ctx context.Context, input *OpenAIRecordUsageInput) error
}

type codexGatewayAccountSelector interface {
	SelectAccountForModelWithExclusions(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*Account, error)
}

type codexGatewayProviderAdapter interface {
	Complete(ctx context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error)
	Stream(ctx context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayProviderResult, error)
}

type CodexGatewayProviderExecutor struct {
	cfg                      *config.Config
	openaiGateway            *OpenAIGatewayService
	stateSource              CodexGatewayRegistryStateSource
	accountSelector          codexGatewayAccountSelector
	anthropicAccountSelector codexGatewayAccountSelector
	stateStore               *CodexGatewayStateStore
	usageRecorder            codexGatewayUsageRecorder
	openaiAdapter            codexGatewayProviderAdapter
	deepseek                 codexGatewayProviderAdapter
	anthropic                codexGatewayProviderAdapter
}

func NewCodexGatewayProviderExecutor(cfg *config.Config, openaiGateway *OpenAIGatewayService, anthropicGateway *GatewayService, stateStore *CodexGatewayStateStore, stateSource CodexGatewayRegistryStateSource) *CodexGatewayProviderExecutor {
	executor := &CodexGatewayProviderExecutor{
		cfg:                      cfg,
		openaiGateway:            openaiGateway,
		stateSource:              stateSource,
		accountSelector:          openaiGateway,
		anthropicAccountSelector: anthropicGateway,
		stateStore:               stateStore,
		usageRecorder:            openaiGateway,
	}
	executor.openaiAdapter = &codexGatewayOpenAIResponsesAdapter{gateway: openaiGateway}
	executor.deepseek = &codexGatewayDeepSeekProviderAdapter{
		stateStore:        stateStore,
		hostedWebSearch:   executor.executeOpenAIHostedWebSearch,
		hostedImageVision: executor.executeOpenAIHostedImageVision,
	}
	executor.anthropic = &codexGatewayAnthropicProviderAdapter{
		stateStore:      stateStore,
		hostedWebSearch: executor.executeOpenAIHostedWebSearch,
	}
	return executor
}

func (e *CodexGatewayProviderExecutor) Complete(ctx context.Context, req CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
	startedAt := time.Now()
	groupID, adapter, err := e.providerGroupAndAdapter(ctx, req.Model)
	if err != nil {
		return nil, err
	}

	var lastFailover *UpstreamFailoverError
	excluded := make(map[int64]struct{})
	attempt := 0
	for {
		attempt++
		account, err := e.selectAccount(ctx, groupID, req, excluded)
		if err != nil {
			var unavailable *CodexGatewayProviderUnavailableError
			if lastFailover != nil && errors.As(err, &unavailable) && unavailable.Kind == CodexGatewayProviderUnavailableNoAccounts {
				return nil, lastFailover
			}
			return nil, err
		}
		codexGatewayCaptureProviderSelection(req, attempt, account)
		result, err := adapter.Complete(ctx, account, req)
		if err == nil {
			codexGatewayCaptureProviderResult(req, result.ProviderResult)
			codexGatewayRecordUsageBestEffort(ctx, e.usageRecorder, req, account, result.ProviderResult, false, startedAt)
			return &result.ServiceResponse, nil
		}
		var failoverErr *UpstreamFailoverError
		if errors.As(err, &failoverErr) {
			codexGatewayCaptureProviderFailover(req, attempt, failoverErr)
			lastFailover = failoverErr
			excluded[account.ID] = struct{}{}
			continue
		}
		return nil, err
	}
}

func (e *CodexGatewayProviderExecutor) Stream(ctx context.Context, req CodexGatewayProviderRequest) error {
	startedAt := time.Now()
	groupID, adapter, err := e.providerGroupAndAdapter(ctx, req.Model)
	if err != nil {
		return err
	}

	var lastFailover *UpstreamFailoverError
	excluded := make(map[int64]struct{})
	attempt := 0
	for {
		attempt++
		account, err := e.selectAccount(ctx, groupID, req, excluded)
		if err != nil {
			var unavailable *CodexGatewayProviderUnavailableError
			if lastFailover != nil && errors.As(err, &unavailable) && unavailable.Kind == CodexGatewayProviderUnavailableNoAccounts {
				return lastFailover
			}
			return err
		}
		codexGatewayCaptureProviderSelection(req, attempt, account)
		providerResult, err := adapter.Stream(ctx, account, req)
		if err == nil {
			codexGatewayCaptureProviderResult(req, providerResult)
			codexGatewayRecordUsageBestEffort(ctx, e.usageRecorder, req, account, providerResult, true, startedAt)
			return nil
		}
		var failoverErr *UpstreamFailoverError
		if errors.As(err, &failoverErr) {
			codexGatewayCaptureProviderFailover(req, attempt, failoverErr)
			lastFailover = failoverErr
			excluded[account.ID] = struct{}{}
			continue
		}
		return err
	}
}

func codexGatewayCaptureProviderResult(req CodexGatewayProviderRequest, result CodexGatewayProviderResult) {
	if req.CaptureTrace == nil || req.CaptureTrace.manager == nil {
		return
	}
	req.CaptureTrace.manager.RecordProviderResult(req.CaptureTrace, result)
}

func codexGatewayCaptureProviderSelection(req CodexGatewayProviderRequest, attempt int, account *Account) {
	if req.CaptureTrace == nil || req.CaptureTrace.manager == nil {
		return
	}
	req.CaptureTrace.manager.RecordProviderSelectionAttempt(
		req.CaptureTrace,
		attempt,
		req.Model.Provider,
		req.Model.UpstreamModel,
		req.CaptureTrace.manager.redact.HashText(fmt.Sprintf("%d", apiKeyAccountIDValue(account))),
	)
}

func codexGatewayCaptureProviderFailover(req CodexGatewayProviderRequest, attempt int, failoverErr *UpstreamFailoverError) {
	if req.CaptureTrace == nil || req.CaptureTrace.manager == nil || failoverErr == nil {
		return
	}
	bodyHash := ""
	if len(failoverErr.ResponseBody) > 0 {
		bodyHash = req.CaptureTrace.manager.redact.HashText(string(failoverErr.ResponseBody))
	}
	req.CaptureTrace.manager.RecordError(req.CaptureTrace, CodexGatewayCaptureError{
		Origin:           "upstream",
		Stage:            "provider_attempt",
		Provider:         req.Model.Provider,
		Model:            req.Model.Slug,
		UpstreamModel:    req.Model.UpstreamModel,
		Attempt:          attempt,
		HTTPStatus:       failoverErr.StatusCode,
		ErrorType:        CodexGatewayErrorTypeAPI,
		ErrorCode:        "upstream_failover",
		Retryable:        true,
		FailoverDecision: "retry_next_account",
		BodyHash:         bodyHash,
		Message:          "upstream attempt failed before visible output",
	})
}

func apiKeyAccountIDValue(account *Account) int64 {
	if account == nil {
		return 0
	}
	return account.ID
}

func (e *CodexGatewayProviderExecutor) providerGroupAndAdapter(ctx context.Context, model CodexGatewayModel) (int64, codexGatewayProviderAdapter, error) {
	if e == nil || e.cfg == nil {
		return 0, nil, fmt.Errorf("codex gateway provider executor is not configured")
	}
	runtime := e.providerRuntime(ctx, model.Provider)
	switch strings.TrimSpace(model.Provider) {
	case "openai":
		if runtime.GroupID <= 0 || !runtime.Healthy {
			return 0, nil, &CodexGatewayProviderUnavailableError{ModelID: model.Slug, Provider: model.Provider, Kind: CodexGatewayProviderUnavailableNoProviderGroup}
		}
		return runtime.GroupID, e.openaiAdapter, nil
	case "deepseek":
		if runtime.GroupID <= 0 || !runtime.Healthy {
			return 0, nil, &CodexGatewayProviderUnavailableError{ModelID: model.Slug, Provider: model.Provider, Kind: CodexGatewayProviderUnavailableNoProviderGroup}
		}
		return runtime.GroupID, e.deepseek, nil
	case "anthropic":
		if runtime.GroupID <= 0 || !runtime.Healthy {
			return 0, nil, &CodexGatewayProviderUnavailableError{ModelID: model.Slug, Provider: model.Provider, Kind: CodexGatewayProviderUnavailableNoProviderGroup}
		}
		return runtime.GroupID, e.anthropic, nil
	default:
		return 0, nil, fmt.Errorf("unsupported codex gateway provider %q", model.Provider)
	}
}

func (e *CodexGatewayProviderExecutor) providerRuntime(ctx context.Context, provider string) CodexGatewayProviderRuntime {
	runtime := CodexGatewayProviderRuntime{
		Provider: normalizeCodexGatewayProvider(CodexGatewayProvider(provider)),
	}
	if e != nil && e.cfg != nil {
		switch runtime.Provider {
		case CodexGatewayProviderOpenAI:
			runtime.GroupID = e.cfg.Gateway.Codex.ProviderGroups.OpenAI
		case CodexGatewayProviderDeepSeek:
			runtime.GroupID = e.cfg.Gateway.Codex.ProviderGroups.DeepSeek
		case CodexGatewayProviderAnthropic:
			runtime.GroupID = e.cfg.Gateway.Codex.ProviderGroups.Anthropic
		}
		runtime.Healthy = runtime.GroupID > 0
	}
	if e == nil || e.stateSource == nil {
		return runtime
	}
	state, err := e.stateSource.LoadCodexGatewayRegistryState(ctx)
	if err != nil || state == nil {
		return runtime
	}
	if loaded, ok := state.ProviderGroups[runtime.Provider]; ok {
		return loaded
	}
	return runtime
}

func (e *CodexGatewayProviderExecutor) selectAccount(ctx context.Context, groupID int64, req CodexGatewayProviderRequest, excluded map[int64]struct{}) (*Account, error) {
	if e == nil || e.accountSelector == nil {
		return nil, fmt.Errorf("codex gateway account selector is not configured")
	}
	selector := e.accountSelector
	if normalizeCodexGatewayProvider(CodexGatewayProvider(req.Model.Provider)) == CodexGatewayProviderAnthropic && e.anthropicAccountSelector != nil {
		selector = e.anthropicAccountSelector
	}
	if selector == nil {
		return nil, fmt.Errorf("codex gateway account selector is not configured")
	}
	account, err := selector.SelectAccountForModelWithExclusions(ctx, &groupID, codexGatewayProviderSelectionSessionKey(req), req.Model.UpstreamModel, excluded)
	if err != nil {
		if !errors.Is(err, ErrNoAvailableAccounts) {
			return nil, err
		}
		return nil, &CodexGatewayProviderUnavailableError{
			ModelID:  req.Model.Slug,
			Provider: req.Model.Provider,
			Kind:     CodexGatewayProviderUnavailableNoAccounts,
		}
	}
	return account, nil
}

const (
	codexGatewayHostedWebSearchOpenAIModel   = "gpt-5.4-mini"
	codexGatewayHostedImageVisionOpenAIModel = "gpt-5.4-mini"
)

func (e *CodexGatewayProviderExecutor) executeOpenAIHostedWebSearch(ctx context.Context, req CodexGatewayProviderRequest, query string) (string, error) {
	if e == nil || e.openaiGateway == nil || e.accountSelector == nil {
		return "", fmt.Errorf("codex gateway hosted web search requires OpenAI provider")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("codex gateway hosted web search requires a query")
	}
	runtime := e.providerRuntime(ctx, string(CodexGatewayProviderOpenAI))
	if runtime.GroupID <= 0 || !runtime.Healthy {
		return "", &CodexGatewayProviderUnavailableError{
			ModelID:  codexGatewayHostedWebSearchOpenAIModel,
			Provider: string(CodexGatewayProviderOpenAI),
			Kind:     CodexGatewayProviderUnavailableNoProviderGroup,
		}
	}
	var lastErr error
	for _, searchModel := range e.openAIHostedSearchModels(ctx) {
		searchReq := req
		searchReq.Model = CodexGatewayModel{
			Slug:          searchModel,
			Provider:      string(CodexGatewayProviderOpenAI),
			UpstreamModel: searchModel,
		}
		searchReq.SessionKey = strings.TrimSpace(req.SessionKey) + ":codex_gateway:hosted_web_search"

		output, handled, err := e.executeOpenAIHostedWebSearchWithModel(ctx, runtime.GroupID, searchReq, searchModel, query)
		if err == nil {
			return output, nil
		}
		lastErr = err
		if handled {
			continue
		}
		return "", err
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", &CodexGatewayProviderUnavailableError{
		ModelID:  codexGatewayHostedWebSearchOpenAIModel,
		Provider: string(CodexGatewayProviderOpenAI),
		Kind:     CodexGatewayProviderUnavailableNoAccounts,
	}
}

func (e *CodexGatewayProviderExecutor) executeOpenAIHostedImageVision(ctx context.Context, req CodexGatewayProviderRequest, imageURL string) (string, error) {
	if e == nil || e.openaiGateway == nil || e.accountSelector == nil {
		return "", fmt.Errorf("codex gateway hosted image vision requires OpenAI provider")
	}
	imageURL = strings.TrimSpace(imageURL)
	if imageURL == "" {
		return "", fmt.Errorf("codex gateway hosted image vision requires an image URL")
	}
	runtime := e.providerRuntime(ctx, string(CodexGatewayProviderOpenAI))
	if runtime.GroupID <= 0 || !runtime.Healthy {
		return "", &CodexGatewayProviderUnavailableError{
			ModelID:  codexGatewayHostedImageVisionOpenAIModel,
			Provider: string(CodexGatewayProviderOpenAI),
			Kind:     CodexGatewayProviderUnavailableNoProviderGroup,
		}
	}
	var lastErr error
	for _, visionModel := range e.openAIHostedSearchModels(ctx) {
		visionReq := req
		visionReq.Model = CodexGatewayModel{
			Slug:          visionModel,
			Provider:      string(CodexGatewayProviderOpenAI),
			UpstreamModel: visionModel,
		}
		visionReq.SessionKey = strings.TrimSpace(req.SessionKey) + ":codex_gateway:hosted_image_vision"
		output, handled, err := e.executeOpenAIHostedImageVisionWithModel(ctx, runtime.GroupID, visionReq, visionModel, imageURL)
		if err == nil {
			return output, nil
		}
		lastErr = err
		if handled {
			continue
		}
		return "", err
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", &CodexGatewayProviderUnavailableError{
		ModelID:  codexGatewayHostedImageVisionOpenAIModel,
		Provider: string(CodexGatewayProviderOpenAI),
		Kind:     CodexGatewayProviderUnavailableNoAccounts,
	}
}

func (e *CodexGatewayProviderExecutor) executeOpenAIHostedWebSearchWithModel(ctx context.Context, groupID int64, req CodexGatewayProviderRequest, searchModel string, query string) (string, bool, error) {
	body, err := json.Marshal(map[string]any{
		"model": searchModel,
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "input_text",
						"text": "Use web search for the query below. Return concise findings with source URLs when available.\n\nQuery: " + query,
					},
				},
			},
		},
		"tools": []any{map[string]any{"type": "web_search"}},
		"store": false,
	})
	if err != nil {
		return "", false, err
	}

	excluded := make(map[int64]struct{})
	for {
		account, err := e.selectHostedWebSearchAccount(ctx, groupID, req, excluded)
		if err != nil {
			return "", true, err
		}
		codexGatewayCaptureUpstreamRequest(req.CaptureTrace, "openai_hosted_web_search", http.Header{}, body)
		resp, err := e.openaiGateway.DoNativeResponsesRequest(ctx, account, http.Header{}, body, false)
		if err != nil {
			excluded[account.ID] = struct{}{}
			continue
		}
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if readErr != nil {
			excluded[account.ID] = struct{}{}
			continue
		}
		codexGatewayCaptureUpstreamResponse(req.CaptureTrace, resp.Header, resp.StatusCode, respBody)
		if resp.StatusCode >= 400 {
			msg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
			if msg == "" {
				msg = http.StatusText(resp.StatusCode)
			}
			failErr := fmt.Errorf("codex gateway hosted web search failed: status=%d message=%s", resp.StatusCode, msg)
			if e.openaiGateway.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, msg, respBody) {
				excluded[account.ID] = struct{}{}
				continue
			}
			return "", false, failErr
		}
		return codexGatewayOpenAIHostedWebSearchOutput(query, respBody), true, nil
	}
}

func (e *CodexGatewayProviderExecutor) executeOpenAIHostedImageVisionWithModel(ctx context.Context, groupID int64, req CodexGatewayProviderRequest, visionModel string, imageURL string) (string, bool, error) {
	body, err := json.Marshal(map[string]any{
		"model": visionModel,
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "input_text",
						"text": "You are a vision preprocessing layer for another model. Describe the image concisely in plain text. Include the main subject, visible text, UI/code clues, and any uncertainty. Do not answer the user's task. Do not mention policies or your own process.",
					},
					map[string]any{
						"type":      "input_image",
						"image_url": imageURL,
					},
				},
			},
		},
		"max_output_tokens": 400,
		"store":             false,
	})
	if err != nil {
		return "", false, err
	}

	excluded := make(map[int64]struct{})
	for {
		account, err := e.selectHostedWebSearchAccount(ctx, groupID, req, excluded)
		if err != nil {
			return "", true, err
		}
		codexGatewayCaptureUpstreamRequest(req.CaptureTrace, "openai_hosted_image_vision", http.Header{}, body)
		resp, err := e.openaiGateway.DoNativeResponsesRequest(ctx, account, http.Header{}, body, false)
		if err != nil {
			excluded[account.ID] = struct{}{}
			continue
		}
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if readErr != nil {
			excluded[account.ID] = struct{}{}
			continue
		}
		codexGatewayCaptureUpstreamResponse(req.CaptureTrace, resp.Header, resp.StatusCode, respBody)
		if resp.StatusCode >= 400 {
			msg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
			if msg == "" {
				msg = http.StatusText(resp.StatusCode)
			}
			failErr := fmt.Errorf("codex gateway hosted image vision failed: status=%d message=%s", resp.StatusCode, msg)
			if e.openaiGateway.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, msg, respBody) {
				excluded[account.ID] = struct{}{}
				continue
			}
			return "", false, failErr
		}
		return strings.TrimSpace(codexGatewayOpenAIResponsesOutputText(respBody)), true, nil
	}
}

func (e *CodexGatewayProviderExecutor) selectHostedWebSearchAccount(ctx context.Context, groupID int64, req CodexGatewayProviderRequest, excluded map[int64]struct{}) (*Account, error) {
	effectiveExcluded := make(map[int64]struct{}, len(excluded))
	for id := range excluded {
		effectiveExcluded[id] = struct{}{}
	}
	for {
		account, err := e.selectAccount(ctx, groupID, req, effectiveExcluded)
		if err != nil {
			return nil, err
		}
		if codexGatewayHostedWebSearchAccountEligible(account, req.Model.UpstreamModel) {
			return account, nil
		}
		if account != nil {
			effectiveExcluded[account.ID] = struct{}{}
			continue
		}
		return nil, &CodexGatewayProviderUnavailableError{
			ModelID:  req.Model.Slug,
			Provider: req.Model.Provider,
			Kind:     CodexGatewayProviderUnavailableNoAccounts,
		}
	}
}

func codexGatewayHostedWebSearchAccountEligible(account *Account, requestedModel string) bool {
	if account == nil || !account.IsOpenAI() || !account.IsSchedulable() {
		return false
	}
	if requestedModel != "" && !account.IsModelSupported(requestedModel) {
		return false
	}
	switch account.Type {
	case AccountTypeOAuth:
		supported, known := account.OpenAICompactSupportKnown()
		if known && !supported {
			return false
		}
		return account.getExtraBool("openai_responses_write_capable") || !known
	case AccountTypeAPIKey, AccountTypeUpstream:
		return true
	default:
		return false
	}
}

func (e *CodexGatewayProviderExecutor) openAIHostedSearchModels(ctx context.Context) []string {
	preferred := []string{codexGatewayHostedWebSearchOpenAIModel, "gpt-5.4", "gpt-5.5", "gpt-5.3-codex"}
	enabled := make(map[string]struct{}, len(preferred))
	if e != nil && e.cfg != nil {
		for _, model := range e.cfg.Gateway.Codex.EnabledModels {
			model = strings.TrimSpace(model)
			if model != "" {
				enabled[model] = struct{}{}
			}
		}
	}
	if e != nil && e.stateSource != nil {
		if state, err := e.stateSource.LoadCodexGatewayRegistryState(ctx); err == nil && state != nil {
			for model, mutation := range state.Models {
				model = strings.TrimSpace(model)
				if model == "" || !mutation.Enabled {
					continue
				}
				enabled[model] = struct{}{}
			}
		}
	}
	if len(enabled) == 0 {
		return append([]string(nil), preferred...)
	}
	out := make([]string, 0, len(preferred))
	for _, model := range preferred {
		if _, ok := enabled[model]; ok {
			out = append(out, model)
		}
	}
	for model := range enabled {
		if strings.HasPrefix(model, "gpt-") {
			out = append(out, model)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), preferred...)
	}
	return uniqueCodexGatewayStringList(out)
}

func uniqueCodexGatewayStringList(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func codexGatewayOpenAIHostedWebSearchOutput(query string, body []byte) string {
	body = codexGatewayNormalizeOpenAIHostedWebSearchBody(body)
	text := strings.TrimSpace(codexGatewayOpenAIResponsesOutputText(body))
	payload := map[string]any{
		"query":    query,
		"provider": "openai_responses",
		"summary":  text,
	}
	if text == "" {
		payload["summary"] = "OpenAI Responses completed the web search, but no output text was returned."
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		if text != "" {
			return text
		}
		return string(body)
	}
	return string(raw)
}

func codexGatewayNormalizeOpenAIHostedWebSearchBody(body []byte) []byte {
	if len(body) == 0 || !looksLikeSSEPayload(body) {
		return body
	}
	normalized, err := codexGatewayOpenAIStreamJSONResponse(body)
	if err != nil || len(normalized) == 0 {
		return body
	}
	return normalized
}

func codexGatewayOpenAIResponsesOutputText(body []byte) string {
	if text := strings.TrimSpace(gjson.GetBytes(body, "output_text").String()); text != "" {
		return text
	}
	output := gjson.GetBytes(body, "output")
	if !output.IsArray() {
		return ""
	}
	var parts []string
	for _, item := range output.Array() {
		if item.Get("type").String() != "message" {
			continue
		}
		content := item.Get("content")
		if !content.IsArray() {
			continue
		}
		for _, block := range content.Array() {
			switch block.Get("type").String() {
			case "output_text", "text":
				if text := strings.TrimSpace(block.Get("text").String()); text != "" {
					parts = append(parts, text)
				}
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func codexGatewayProviderSelectionSessionKey(req CodexGatewayProviderRequest) string {
	sessionKey := strings.TrimSpace(req.SessionKey)
	if sessionKey == "" {
		return ""
	}
	if normalizeCodexGatewayProvider(CodexGatewayProvider(req.Model.Provider)) != CodexGatewayProviderDeepSeek {
		return sessionKey
	}
	upstreamModel := strings.ToLower(strings.TrimSpace(req.Model.UpstreamModel))
	if upstreamModel == "" {
		upstreamModel = strings.ToLower(strings.TrimSpace(req.Model.Slug))
	}
	if upstreamModel == "" {
		return sessionKey
	}
	isolationKey := strings.TrimSpace(req.IsolationKey)
	if isolationKey == "" {
		isolationKey = "shared"
	}
	return sessionKey + ":codex_gateway:deepseek:" + upstreamModel + ":" + isolationKey
}

type codexGatewayDeepSeekProviderAdapter struct {
	stateStore        *CodexGatewayStateStore
	hostedWebSearch   func(ctx context.Context, req CodexGatewayProviderRequest, query string) (string, error)
	hostedImageVision func(ctx context.Context, req CodexGatewayProviderRequest, imageURL string) (string, error)
}

type codexGatewayAnthropicProviderAdapter struct {
	stateStore      *CodexGatewayStateStore
	hostedWebSearch func(ctx context.Context, req CodexGatewayProviderRequest, query string) (string, error)
}

func (a *codexGatewayDeepSeekProviderAdapter) Complete(ctx context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
	if account == nil {
		return CodexGatewayDeepSeekAdapterResult{}, fmt.Errorf("codex gateway deepseek adapter requires selected account")
	}
	cfg := CodexGatewayDeepSeekRequestConfig{}
	cfg.ToolMappingConfig.EnableDeepSeekSchemaFlattening = true
	cfg.ToolMappingConfig.DeepSeekFlattenMinDepth = 3
	cfg.ToolMappingConfig.DeepSeekFlattenMinLeaves = 4
	if a.hostedWebSearch != nil {
		cfg.HostedWebSearch = func(ctx context.Context, query string) (string, error) {
			return a.hostedWebSearch(ctx, req, query)
		}
	}
	if a.hostedImageVision != nil {
		cfg.HostedImageVision = func(ctx context.Context, imageURL string) (string, error) {
			return a.hostedImageVision(ctx, req, imageURL)
		}
	}
	return ExecuteCodexGatewayDeepSeekAdapter(
		ctx,
		http.DefaultClient,
		account.GetOpenAIBaseURL(),
		account.GetOpenAIApiKey(),
		req.Model,
		req.Parsed,
		a.stateStore,
		CodexGatewayDeepSeekRequestContext{
			SessionKey:           req.SessionKey,
			IsolationKey:         req.IsolationKey,
			WorkspaceKey:         req.WorkspaceKey,
			ManagedSessionBucket: req.ManagedSessionBucket,
			CaptureTrace:         req.CaptureTrace,
		},
		cfg,
	)
}

func (a *codexGatewayDeepSeekProviderAdapter) Stream(ctx context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayProviderResult, error) {
	if account == nil {
		return CodexGatewayProviderResult{}, fmt.Errorf("codex gateway deepseek adapter requires selected account")
	}
	cfg := CodexGatewayDeepSeekRequestConfig{}
	cfg.ToolMappingConfig.EnableDeepSeekSchemaFlattening = true
	cfg.ToolMappingConfig.DeepSeekFlattenMinDepth = 3
	cfg.ToolMappingConfig.DeepSeekFlattenMinLeaves = 4
	if a.hostedWebSearch != nil {
		cfg.HostedWebSearch = func(ctx context.Context, query string) (string, error) {
			return a.hostedWebSearch(ctx, req, query)
		}
	}
	if a.hostedImageVision != nil {
		cfg.HostedImageVision = func(ctx context.Context, imageURL string) (string, error) {
			return a.hostedImageVision(ctx, req, imageURL)
		}
	}
	result, err := ExecuteCodexGatewayDeepSeekStream(
		ctx,
		http.DefaultClient,
		account.GetOpenAIBaseURL(),
		account.GetOpenAIApiKey(),
		req.Model,
		req.Parsed,
		a.stateStore,
		CodexGatewayDeepSeekRequestContext{
			SessionKey:           req.SessionKey,
			IsolationKey:         req.IsolationKey,
			WorkspaceKey:         req.WorkspaceKey,
			ManagedSessionBucket: req.ManagedSessionBucket,
			CaptureTrace:         req.CaptureTrace,
		},
		cfg,
		req.Request.StreamWriter,
	)
	if err != nil {
		return CodexGatewayProviderResult{}, err
	}
	copyCodexGatewayHTTPHeaders(req.Request.ResponseHeader, result.ServiceResponse.Headers)
	return result.ProviderResult, nil
}

func (a *codexGatewayAnthropicProviderAdapter) Complete(ctx context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
	if account == nil {
		return CodexGatewayDeepSeekAdapterResult{}, fmt.Errorf("codex gateway anthropic adapter requires selected account")
	}
	return ExecuteCodexGatewayAnthropicAdapter(
		ctx,
		http.DefaultClient,
		codexGatewayAnthropicAccountBaseURL(account),
		account.GetCredential("api_key"),
		req.Model,
		req.Parsed,
		a.stateStore,
		CodexGatewayAnthropicRequestContext{
			SessionKey:   req.SessionKey,
			IsolationKey: req.IsolationKey,
			CaptureTrace: req.CaptureTrace,
		},
		CodexGatewayAnthropicRequestConfig{},
	)
}

func (a *codexGatewayAnthropicProviderAdapter) Stream(ctx context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayProviderResult, error) {
	if account == nil {
		return CodexGatewayProviderResult{}, fmt.Errorf("codex gateway anthropic adapter requires selected account")
	}
	if req.Request.ResponseHeader != nil {
		req.Request.ResponseHeader.Set("Content-Type", "text/event-stream")
		req.Request.ResponseHeader.Set("Cache-Control", "no-cache")
		req.Request.ResponseHeader.Set("Connection", "keep-alive")
	}
	if req.Request.WriteStatus != nil {
		req.Request.WriteStatus(http.StatusOK)
	}
	cfg := CodexGatewayAnthropicRequestConfig{}
	if a.hostedWebSearch != nil {
		cfg.HostedWebSearch = func(ctx context.Context, query string) (string, error) {
			return a.hostedWebSearch(ctx, req, query)
		}
	}
	result, err := ExecuteCodexGatewayAnthropicStream(
		ctx,
		http.DefaultClient,
		codexGatewayAnthropicAccountBaseURL(account),
		account.GetCredential("api_key"),
		req.Model,
		req.Parsed,
		a.stateStore,
		CodexGatewayAnthropicRequestContext{
			SessionKey:   req.SessionKey,
			IsolationKey: req.IsolationKey,
			CaptureTrace: req.CaptureTrace,
		},
		cfg,
		req.Request.StreamWriter,
	)
	if err != nil {
		return CodexGatewayProviderResult{}, err
	}
	copyCodexGatewayHTTPHeaders(req.Request.ResponseHeader, result.ServiceResponse.Headers)
	return result.ProviderResult, nil
}

func codexGatewayAnthropicAccountBaseURL(account *Account) string {
	if account == nil {
		return ""
	}
	if baseURL := strings.TrimSpace(account.GetBaseURL()); baseURL != "" {
		return baseURL
	}
	if strings.EqualFold(strings.TrimSpace(account.Platform), PlatformAnthropic) {
		return strings.TrimSpace(account.GetCredential("base_url"))
	}
	return ""
}

func copyCodexGatewayHTTPHeaders(dst http.Header, src http.Header) {
	if dst == nil || src == nil {
		return
	}
	for key := range dst {
		if codexGatewayAllowedOpenAIResponseHeader(key) {
			dst.Del(key)
		}
	}
	for key, values := range src {
		dst.Del(key)
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

type codexGatewayStreamRecorder struct {
	writer bytes.Buffer
	flush  func()
}

func (s *codexGatewayStreamRecorder) Write(p []byte) (int, error) {
	return s.writer.Write(p)
}

func (s *codexGatewayStreamRecorder) Flush() {
	if s.flush != nil {
		s.flush()
	}
}

func writeCodexGatewayStreamFailure(w io.Writer, responseID string, errType, errCode, message string) error {
	writer := NewCodexGatewayResponseEventWriter(w)
	return writer.WriteResponseFailed(CodexGatewayResponse{
		ID:     strings.TrimSpace(responseID),
		Object: "response",
		Status: "failed",
		Output: []json.RawMessage{},
		Error: &CodexGatewayResponseError{
			Code:    strings.TrimSpace(errCode),
			Message: strings.TrimSpace(message),
			RawFields: map[string]json.RawMessage{
				"type": json.RawMessage(fmt.Sprintf("%q", strings.TrimSpace(errType))),
			},
		},
	})
}
