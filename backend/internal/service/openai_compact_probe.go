package service

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// AccountTestModeDefault drives the standard /responses connection test.
	AccountTestModeDefault = "default"
	// AccountTestModeCompact drives the /responses/compact compact-probe test.
	AccountTestModeCompact = "compact"
	// AccountTestModeWebSearch verifies a real hosted web_search_call.
	AccountTestModeWebSearch = "web_search"

	openAICompactProbeProfileCodexV2 = "codex_compact_v2"
	openAICompactProbeProfilePathV1  = "responses_compact_path_v1"
)

func normalizeAccountTestMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case AccountTestModeCompact:
		return AccountTestModeCompact
	case AccountTestModeWebSearch:
		return AccountTestModeWebSearch
	default:
		return AccountTestModeDefault
	}
}

func createOpenAICompactProbePayload(model string) map[string]any {
	return map[string]any{
		"model":        strings.TrimSpace(model),
		"instructions": "You are a helpful coding assistant.",
		"input": []any{
			map[string]any{
				"type":    "message",
				"role":    "user",
				"content": "Respond with OK.",
			},
		},
	}
}

func createOpenAICompactBodySignalProbePayload(model string) map[string]any {
	payload := createOpenAICompactProbePayload(model)
	payload["reasoning"] = map[string]any{"effort": "max"}
	payload["input"] = []any{
		map[string]any{
			"type": "message",
			"id":   "item_compact_probe",
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "Respond with OK."},
			},
		},
		map[string]any{"type": "compaction_trigger"},
	}
	return payload
}

func openAICompactProbeProfileForMode(mode string) string {
	if normalizeOpenAICompactEndpointMode(mode) == OpenAICompactEndpointModeBodySignal {
		return openAICompactProbeProfileCodexV2
	}
	return openAICompactProbeProfilePathV1
}

func isOpenAICompactProbeSuccess(mode string, status int, body []byte) bool {
	if status != http.StatusOK {
		return false
	}
	if normalizeOpenAICompactEndpointMode(mode) != OpenAICompactEndpointModeBodySignal {
		return true
	}

	var payload struct {
		Object string `json:"object"`
		Output []struct {
			Type string `json:"type"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(payload.Object), "response.compaction") {
		return true
	}
	for _, item := range payload.Output {
		if strings.EqualFold(strings.TrimSpace(item.Type), "compaction_summary") {
			return true
		}
	}
	return false
}

func shouldMarkOpenAICompactUnsupported(status int, body []byte) bool {
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		lower := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(body) + " " + string(body)))
		if isOpenAICompactProbeIndeterminateClientFailure(lower) {
			return false
		}
		// The probe is a valid Codex compact-v2 request. A remaining 400/422 means
		// this upstream cannot satisfy that request contract, regardless of which
		// individual field it rejected.
		return true
	case http.StatusForbidden:
		lower := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(body) + " " + string(body)))
		if strings.Contains(lower, "compact") {
			for _, keyword := range []string{
				"unsupported",
				"not support",
				"does not support",
				"not available",
				"disabled",
			} {
				if strings.Contains(lower, keyword) {
					return true
				}
			}
		}
	}
	return false
}

func isOpenAICompactProbeIndeterminateClientFailure(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}
	for _, marker := range []string{
		"unknown model",
		"model_not_found",
		"no available channel",
		"no available account",
		"rate limit",
		"too many requests",
		"insufficient_quota",
		"insufficient user quota",
		"insufficient balance",
		"billing",
		"authentication",
		"unauthorized",
		"invalid api key",
		"invalid token",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func buildOpenAICompactProbeExtraUpdates(resp *http.Response, body []byte, probeErr error, now time.Time) map[string]any {
	updates := map[string]any{
		"openai_compact_checked_at":  now.Format(time.RFC3339),
		"openai_compact_last_status": nil,
	}

	if resp != nil {
		updates["openai_compact_last_status"] = resp.StatusCode
	}

	switch {
	case probeErr != nil:
		updates["openai_compact_last_error"] = truncateString(sanitizeUpstreamErrorMessage(probeErr.Error()), 2048)
	case resp == nil:
		updates["openai_compact_last_error"] = "compact probe failed"
	default:
		errMsg := strings.TrimSpace(extractUpstreamErrorMessage(body))
		if errMsg == "" && len(body) > 0 {
			errMsg = strings.TrimSpace(string(body))
		}
		if errMsg == "" && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
			errMsg = "HTTP " + strconv.Itoa(resp.StatusCode)
		}
		errMsg = truncateString(sanitizeUpstreamErrorMessage(errMsg), 2048)
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			updates["openai_compact_supported"] = true
			updates["openai_compact_last_error"] = ""
		} else {
			if shouldMarkOpenAICompactUnsupported(resp.StatusCode, body) {
				updates["openai_compact_supported"] = false
			}
			updates["openai_compact_last_error"] = errMsg
		}
	}

	return updates
}

func buildOpenAICompactProbeExtraUpdatesForModel(existingExtra map[string]any, requestedModel, upstreamModel string, resp *http.Response, body []byte, probeErr error, now time.Time) map[string]any {
	return buildOpenAICompactProbeExtraUpdatesForModelWithProfile(existingExtra, requestedModel, upstreamModel, "", resp, body, probeErr, now)
}

func buildOpenAICompactProbeExtraUpdatesForModelWithProfile(existingExtra map[string]any, requestedModel, upstreamModel, probeProfile string, resp *http.Response, body []byte, probeErr error, now time.Time) map[string]any {
	updates := buildOpenAICompactProbeExtraUpdates(resp, body, probeErr, now)
	requestedModel = strings.TrimSpace(requestedModel)
	upstreamModel = strings.TrimSpace(upstreamModel)
	probeProfile = strings.TrimSpace(probeProfile)
	updates["openai_compact_last_requested_model"] = requestedModel
	updates["openai_compact_last_upstream_model"] = upstreamModel
	if probeProfile != "" {
		updates["openai_compact_probe_profile"] = probeProfile
	}

	scoped := make(map[string]any)
	if current, ok := existingExtra["openai_compact_model_support"].(map[string]any); ok {
		for key, value := range current {
			scoped[key] = value
		}
	}
	key := normalizeOpenAICompactSupportModel(upstreamModel)
	if key == "" {
		return updates
	}

	entry := map[string]any{
		"requested_model": requestedModel,
		"upstream_model":  upstreamModel,
		"checked_at":      now.Format(time.RFC3339),
		"status":          updates["openai_compact_last_status"],
		"error":           updates["openai_compact_last_error"],
	}
	if probeProfile != "" {
		entry["probe_profile"] = probeProfile
	}
	if supported, known := updates["openai_compact_supported"].(bool); known {
		entry["supported"] = supported
	}
	scoped[key] = entry
	updates["openai_compact_model_support"] = scoped
	return updates
}

func mergeExtraUpdates(base map[string]any, more map[string]any) map[string]any {
	if len(base) == 0 && len(more) == 0 {
		return nil
	}
	out := make(map[string]any, len(base)+len(more))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range more {
		out[key] = value
	}
	return out
}

func compactProbeSessionID(accountID int64) string {
	if accountID <= 0 {
		return "probe_compact"
	}
	return "probe_compact_" + strconv.FormatInt(accountID, 10)
}
