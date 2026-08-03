package service

import "strings"

const (
	codexGatewayCaptureErrorClassModelUnavailableBackgroundTask = "model_unavailable_background_task"
	codexGatewayCaptureErrorClassAccountUnavailable             = "account_unavailable"
	codexGatewayCaptureErrorClassProvider4xx                    = "provider_4xx"
	codexGatewayCaptureErrorClassProvider5xx                    = "provider_5xx"
	codexGatewayCaptureErrorClassProviderTimeout                = "provider_timeout"
	codexGatewayCaptureErrorClassCloudflareHTML                 = "cloudflare_html"
	codexGatewayCaptureErrorClassStreamParseError               = "stream_parse_error"
	codexGatewayCaptureErrorClassClientDisconnect               = "client_disconnect"
	codexGatewayCaptureErrorClassGatewayMappingError            = "gateway_mapping_error"
	codexGatewayCaptureErrorClassCanceledAfterVisibleOutput     = "canceled_after_visible_output"
)

func (m *CodexGatewayCaptureManager) enrichError(trace *CodexGatewayTrace, errMeta CodexGatewayCaptureError) CodexGatewayCaptureError {
	if trace != nil {
		trace.mu.Lock()
		if trace.state.VisibleOutputStarted {
			errMeta.VisibleOutputStarted = true
		}
		if terminal := trace.state.ClientTerminalEvent; terminal != "" {
			errMeta.TerminalEventSeen = true
		}
		trace.mu.Unlock()
	}
	errMeta.ErrorClass = codexGatewayCaptureClassifyError(errMeta)
	return errMeta
}

func (m *CodexGatewayCaptureManager) sanitizeErrorForCapture(errMeta CodexGatewayCaptureError) CodexGatewayCaptureError {
	message := strings.TrimSpace(errMeta.Message)
	if message != "" && m != nil && m.redact != nil {
		errMeta.MessageHash = m.redact.HashText(message)
		errMeta.MessageChars = len([]rune(message))
	}
	errMeta.Message = ""
	return errMeta
}

func codexGatewayCaptureClassifyError(errMeta CodexGatewayCaptureError) string {
	message := strings.ToLower(strings.TrimSpace(errMeta.Message))
	code := strings.ToLower(strings.TrimSpace(errMeta.ErrorCode))
	stage := strings.ToLower(strings.TrimSpace(errMeta.Stage))
	provider := strings.ToLower(strings.TrimSpace(errMeta.Provider))
	model := strings.ToLower(strings.TrimSpace(errMeta.Model))
	if strings.Contains(message, "no available openai accounts supporting model") ||
		strings.Contains(message, "no available accounts") ||
		strings.Contains(message, "has no available accounts") {
		if strings.Contains(model, "gpt-5.4-mini") || strings.Contains(message, "gpt-5.4-mini") {
			return codexGatewayCaptureErrorClassModelUnavailableBackgroundTask
		}
		return codexGatewayCaptureErrorClassAccountUnavailable
	}
	if strings.Contains(message, "cloudflare") || strings.Contains(message, "<!doctype html") || strings.Contains(message, "524: a timeout occurred") || errMeta.HTTPStatus == 524 {
		return codexGatewayCaptureErrorClassCloudflareHTML
	}
	if strings.Contains(message, "context canceled") || strings.Contains(message, "client disconnected") || strings.Contains(message, "broken pipe") {
		if errMeta.VisibleOutputStarted {
			return codexGatewayCaptureErrorClassCanceledAfterVisibleOutput
		}
		return codexGatewayCaptureErrorClassClientDisconnect
	}
	if strings.Contains(message, "timeout") || strings.Contains(code, "timeout") {
		return codexGatewayCaptureErrorClassProviderTimeout
	}
	if strings.Contains(stage, "mapping") || strings.Contains(message, "mapping") {
		return codexGatewayCaptureErrorClassGatewayMappingError
	}
	if strings.Contains(stage, "parse") || strings.Contains(message, "parse") {
		return codexGatewayCaptureErrorClassStreamParseError
	}
	if provider != "" && errMeta.HTTPStatus >= 400 && errMeta.HTTPStatus < 500 {
		return codexGatewayCaptureErrorClassProvider4xx
	}
	if provider != "" && errMeta.HTTPStatus >= 500 {
		return codexGatewayCaptureErrorClassProvider5xx
	}
	if strings.Contains(message, "no available") {
		return codexGatewayCaptureErrorClassAccountUnavailable
	}
	return ""
}
