package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
)

type openAIResponsesProbeResultRepository interface {
	UpdateOpenAIResponsesProbeResult(ctx context.Context, id int64, updates map[string]any) error
}

// openaiResponsesProbeTimeout 是探测请求的超时时长。
// 探测在后台异步执行；留出余量给推理型模型先推理再产出工具调用。
const openaiResponsesProbeTimeout = 15 * time.Second

const responsesProbeMaxBodyBytes = 256 * 1024

const responsesCustomToolProbeRuns = 3

// openaiResponsesProbePayload 是探测使用的最小 Responses 请求体。
// 探测强制一次函数调用，只有真正产出 function_call 的 2xx 响应才判定支持。
func openaiResponsesProbePayload(modelID string) []byte {
	if strings.TrimSpace(modelID) == "" {
		modelID = openai.DefaultTestModel
	}
	body, _ := json.Marshal(map[string]any{
		"model": modelID,
		"input": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "Call the probe_ping function with ok=true."},
				},
			},
		},
		"tools": []map[string]any{
			{
				"type":        "function",
				"name":        "probe_ping",
				"description": "Capability probe. Call to acknowledge.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"ok": map[string]any{"type": "boolean"},
					},
					"required": []string{"ok"},
				},
			},
		},
		"tool_choice":       "required",
		"max_output_tokens": 512,
		"stream":            false,
	})
	return body
}

func openaiResponsesCustomToolsProbePayload(modelID string) []byte {
	if strings.TrimSpace(modelID) == "" {
		modelID = openai.DefaultTestModel
	}
	body, _ := json.Marshal(map[string]any{
		"model":        modelID,
		"instructions": "You are a coding agent. Use the exec tool for shell commands.",
		"input": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "Run pwd now and report its result."},
				},
			},
		},
		"tools": []map[string]any{
			{
				"type":        "custom",
				"name":        "exec",
				"description": "Run a shell command.",
				"format":      map[string]any{"type": "text"},
			},
		},
		"tool_choice":       "auto",
		"max_output_tokens": 128,
		"stream":            false,
		"store":             false,
	})
	return body
}

func selectResponsesProbeModel(account *Account, requestedModels ...string) string {
	if account == nil {
		return openai.DefaultTestModel
	}
	if len(requestedModels) > 0 {
		requestedModel := strings.TrimSpace(requestedModels[0])
		if requestedModel != "" && account.IsModelSupported(requestedModel) {
			if mapped := strings.TrimSpace(account.GetMappedModel(requestedModel)); mapped != "" && !strings.Contains(mapped, "*") {
				return mapped
			}
		}
	}
	mapping := account.GetModelMapping()
	for _, preferred := range []string{"gpt-5.6-sol", "gpt-5.6", "gpt-5.4", "gpt-5.2"} {
		if upstream := strings.TrimSpace(mapping[preferred]); upstream != "" && !strings.Contains(upstream, "*") {
			return upstream
		}
	}
	candidates := make([]string, 0, len(mapping))
	for _, upstream := range mapping {
		upstream = strings.TrimSpace(upstream)
		if upstream == "" || strings.Contains(upstream, "*") {
			continue
		}
		candidates = append(candidates, upstream)
	}
	if len(candidates) == 0 {
		return openai.DefaultTestModel
	}
	sort.Strings(candidates)
	return candidates[0]
}

// ProbeOpenAIAPIKeyResponsesSupport 探测 OpenAI APIKey 账号上游是否支持
// /v1/responses 端点，并将结果持久化到 accounts.extra.openai_responses_supported。
//
// 调用时机：账号创建/更新后，且仅当 platform=openai && type=apikey 时。
//
// 探测策略（参见包文档 internal/pkg/openai_compat）：
//   - 上游 404 / 405 → 不支持，写 false
//   - 上游 2xx / 其他 4xx（401/422/400 等）/ 5xx → 支持，写 true
//   - 网络层失败（连接错误、超时）→ 不写标记，保持 unknown
//     （后续请求仍按"现状即证据"默认走 Responses）
//
// 该方法是幂等的：重复调用会以最新探测结果覆盖标记。
//
// 关于失败处理：探测本身的失败不应阻塞账号创建——账号能创建/更新成功就够了，
// 探测结果只影响后续路由优化。所有错误都仅记录日志，不向调用方传播。
func (s *AccountTestService) ProbeOpenAIAPIKeyResponsesSupport(ctx context.Context, accountID int64) {
	s.probeOpenAIAPIKeyResponsesSupport(ctx, accountID, "")
}

func (s *AccountTestService) ProbeOpenAIAPIKeyResponsesSupportForModel(ctx context.Context, accountID int64, requestedModel string) {
	s.probeOpenAIAPIKeyResponsesSupport(ctx, accountID, requestedModel)
}

func (s *AccountTestService) probeOpenAIAPIKeyResponsesSupport(ctx context.Context, accountID int64, requestedModel string) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		logger.LegacyPrintf("service.openai_probe", "probe_load_account_failed: account_id=%d err=%v", accountID, err)
		return
	}
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		// 仅 OpenAI APIKey 账号需要探测；其他账号类型无能力差异。
		return
	}

	apiKey, err := NewOpenAIGatewayCredentials(s.cfg, nil).OpenAIAPIKey(account)
	if err != nil {
		logger.LegacyPrintf("service.openai_probe", "probe_skip_no_apikey: account_id=%d err=%v", accountID, err)
		return
	}
	baseURL := account.GetOpenAIBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		logger.LegacyPrintf("service.openai_probe", "probe_invalid_baseurl: account_id=%d base_url=%q err=%v", accountID, baseURL, err)
		return
	}

	probeURL := buildOpenAIResponsesURL(normalizedBaseURL)
	probeModel := selectResponsesProbeModel(account, requestedModel)
	probeTarget := account.OpenAIResponsesCustomToolsTargetFingerprint(probeModel)

	status, bodyBytes, err := s.executeOpenAIResponsesProbe(ctx, account, probeURL, apiKey, openaiResponsesProbePayload(probeModel))
	if err != nil {
		// 网络层失败：不写标记，保持 unknown，下次重试或由网关 fallback 处理
		logger.LegacyPrintf("service.openai_probe", "probe_request_failed: account_id=%d url=%s err=%v", accountID, probeURL, err)
		return
	}

	supported := decideResponsesProbeSupport(status, bodyBytes)
	updates := map[string]any{
		openai_compat.ExtraKeyResponsesSupported:   supported,
		openai_compat.ExtraKeyResponsesProbeModel:  probeModel,
		openai_compat.ExtraKeyResponsesProbeTarget: probeTarget,
	}
	customToolsSupported := false
	customToolsKnown := !supported
	customStatus := 0
	if supported {
		customToolsSupported, customToolsKnown, customStatus = s.probeOpenAIResponsesCustomToolsConsistency(
			ctx,
			account,
			probeURL,
			apiKey,
			probeModel,
		)
	}
	if customToolsKnown {
		updates[openai_compat.ExtraKeyResponsesCustomToolsSupported] = customToolsSupported
		updates[openai_compat.ExtraKeyResponsesCustomToolsProbeModel] = probeModel
		updates[openai_compat.ExtraKeyResponsesCustomToolsProbeTarget] = probeTarget
	}

	latest, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil || latest.OpenAIResponsesCustomToolsTargetFingerprint(selectResponsesProbeModel(latest, requestedModel)) != probeTarget {
		logger.LegacyPrintf("service.openai_probe", "probe_stale_result_skipped: account_id=%d", accountID)
		return
	}
	if customToolsKnown && customToolsSupported && hasOpenAIResponsesCustomToolsNegativeEvidence(latest, probeModel, probeTarget) {
		customToolsSupported = false
		updates[openai_compat.ExtraKeyResponsesCustomToolsSupported] = false
		logger.LegacyPrintf("service.openai_probe", "custom_tools_probe_recovery_suppressed: account_id=%d model=%s reason=prior_negative_same_target", accountID, probeModel)
	}

	var persistErr error
	if repo, ok := s.accountRepo.(openAIResponsesProbeResultRepository); ok {
		persistErr = repo.UpdateOpenAIResponsesProbeResult(ctx, accountID, updates)
	} else {
		persistErr = s.accountRepo.UpdateExtra(ctx, accountID, updates)
	}
	if persistErr != nil {
		logger.LegacyPrintf("service.openai_probe", "probe_persist_failed: account_id=%d supported=%v err=%v", accountID, supported, persistErr)
		return
	}

	logger.LegacyPrintf("service.openai_probe",
		"probe_done: account_id=%d base_url=%s status=%d supported=%v custom_tools_status=%d custom_tools_known=%v custom_tools_supported=%v",
		accountID, normalizedBaseURL, status, supported, customStatus, customToolsKnown, customToolsSupported,
	)
}

func (s *AccountTestService) probeOpenAIResponsesCustomToolsConsistency(
	ctx context.Context,
	account *Account,
	probeURL string,
	apiKey string,
	probeModel string,
) (supported bool, known bool, lastStatus int) {
	payload := openaiResponsesCustomToolsProbePayload(probeModel)
	for attempt := 1; attempt <= responsesCustomToolProbeRuns; attempt++ {
		status, body, err := s.executeOpenAIResponsesProbe(ctx, account, probeURL, apiKey, payload)
		lastStatus = status
		if err != nil {
			logger.LegacyPrintf("service.openai_probe", "custom_tools_probe_request_failed: account_id=%d url=%s attempt=%d/%d err=%v", account.ID, probeURL, attempt, responsesCustomToolProbeRuns, err)
			return false, false, lastStatus
		}
		attemptSupported, attemptKnown := decideResponsesCustomToolsProbeSupport(status, body)
		if !attemptKnown {
			return false, false, lastStatus
		}
		if !attemptSupported {
			return false, true, lastStatus
		}
	}
	return true, true, lastStatus
}

func (s *AccountTestService) executeOpenAIResponsesProbe(ctx context.Context, account *Account, probeURL, apiKey string, payload []byte) (int, []byte, error) {
	probeCtx, cancel := context.WithTimeout(ctx, openaiResponsesProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost, probeURL, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("User-Agent", codexCLIUserAgent)
	account.ApplyHeaderOverrides(req.Header)

	resp, err := s.sendOpenAIProbeHTTPRequest(probeCtx, req, account)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, responsesProbeMaxBodyBytes))
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, responsesProbeMaxBodyBytes))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

// isResponsesEndpointSupportedByStatus 根据探测响应的 HTTP 状态码判定上游
// 是否暴露 /v1/responses 端点。
//
// 关键观察：第三方 OpenAI 兼容上游（DeepSeek/Kimi 等）对未知端点统一返回 404
// 或 405；而 OpenAI 官方/有 Responses 实现的上游会因为请求体最简（缺字段）
// 返回 400/422 等业务错误，但端点本身存在。
//
// 因此：仅 404 和 405 视为"端点不存在"，其他 status 视为"端点存在"。
//
// 5xx 也视为"端点存在"——上游偶发故障不应误判为不支持。
func isResponsesEndpointSupportedByStatus(status int) bool {
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return false
	}
	return true
}

func (s *AccountTestService) sendOpenAIProbeHTTPRequest(ctx context.Context, req *http.Request, account *Account) (*http.Response, error) {
	resp, err := s.sendOpenAIAccountTestHTTPRequest(ctx, nil, req, account)
	if err != nil {
		accountID := int64(0)
		if account != nil {
			accountID = account.ID
		}
		logger.LegacyPrintf("service.openai_probe", "probe_egress_or_tls_policy_rejected: account_id=%d err=%v", accountID, err)
		return nil, err
	}
	return resp, nil
}

func decideResponsesProbeSupport(status int, body []byte) bool {
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed {
		return false
	}
	if status < 200 || status >= 300 {
		return true
	}
	return responsesProbeBodyHasFunctionCall(body)
}

func responsesProbeBodyHasFunctionCall(body []byte) bool {
	return responsesProbeBodyHasOutputType(body, "function_call")
}

func responsesProbeBodyHasCustomToolCall(body []byte) bool {
	return responsesProbeBodyHasOutputType(body, "custom_tool_call")
}

func decideResponsesCustomToolsProbeSupport(status int, body []byte) (supported bool, known bool) {
	if status < 200 || status >= 300 {
		return false, false
	}
	var parsed struct {
		Output []struct {
			Type string `json:"type"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.Output) == 0 {
		return false, false
	}
	explicitFailure := false
	for _, item := range parsed.Output {
		switch strings.ToLower(strings.TrimSpace(item.Type)) {
		case "custom_tool_call":
			return true, true
		case "message", "function_call":
			explicitFailure = true
		}
	}
	return false, explicitFailure
}

func hasOpenAIResponsesCustomToolsNegativeEvidence(account *Account, probeModel, probeTarget string) bool {
	if account == nil || account.Extra == nil {
		return false
	}
	priorSupported, ok := account.Extra[openai_compat.ExtraKeyResponsesCustomToolsSupported].(bool)
	if !ok || priorSupported {
		return false
	}
	return strings.TrimSpace(account.OpenAIResponsesCustomToolsProbeModel()) == strings.TrimSpace(probeModel) &&
		strings.TrimSpace(account.OpenAIResponsesCustomToolsProbeTarget()) == strings.TrimSpace(probeTarget)
}

func responsesProbeBodyHasOutputType(body []byte, outputType string) bool {
	var parsed struct {
		Output []struct {
			Type string `json:"type"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false
	}
	for _, item := range parsed.Output {
		if strings.EqualFold(strings.TrimSpace(item.Type), outputType) {
			return true
		}
	}
	return false
}
