package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	OpenAIAgentIdentityAdmissionStatePending  = "pending"
	OpenAIAgentIdentityAdmissionStateAdmitted = "admitted"
	OpenAIAgentIdentityAdmissionStateRejected = "rejected"

	OpenAIAgentIdentityAdmissionStageRegistration = "registration"
	OpenAIAgentIdentityAdmissionStageResponses    = "responses"
	OpenAIAgentIdentityAdmissionStageCompact      = "compact"
	OpenAIAgentIdentityAdmissionStageNativeSearch = "native_search"
	OpenAIAgentIdentityAdmissionStageComplete     = "complete"

	OpenAIAgentIdentityAdmissionVerdictAdmit  = "admit"
	OpenAIAgentIdentityAdmissionVerdictRetry  = "retry"
	OpenAIAgentIdentityAdmissionVerdictReject = "reject"
)

const (
	defaultOpenAIAgentIdentityAdmissionModel        = "gpt-5.6-sol"
	defaultOpenAIAgentIdentityAdmissionCompactBytes = 1 << 20
	defaultOpenAIAgentIdentityAdmissionInterval     = 10 * time.Second
	defaultOpenAIAgentIdentityAdmissionProbeTimeout = 2 * time.Minute
	defaultOpenAIAgentIdentityAdmissionRetryDelay   = 30 * time.Second
	openAIAgentIdentityAdmissionResponseLimit       = 8 << 20
)

type OpenAIAgentIdentityAdmissionProbeResult struct {
	Verdict    string
	Stage      string
	StatusCode int
	Message    string
}

type OpenAIAgentIdentityAdmissionProber interface {
	Probe(ctx context.Context, account *Account) (OpenAIAgentIdentityAdmissionProbeResult, error)
}

type openAIAgentIdentityAdmissionStore interface {
	FindByExtraField(ctx context.Context, key string, value any) ([]Account, error)
	UpdateExtra(ctx context.Context, id int64, updates map[string]any) error
	SetSchedulable(ctx context.Context, id int64, schedulable bool) error
	ClearError(ctx context.Context, id int64) error
	SetError(ctx context.Context, id int64, errorMsg string) error
}

type OpenAIAgentIdentityAdmissionWorkerOptions struct {
	Interval     time.Duration
	ProbeTimeout time.Duration
	RetryDelay   time.Duration
}

type OpenAIAgentIdentityAdmissionWorker struct {
	store        openAIAgentIdentityAdmissionStore
	prober       OpenAIAgentIdentityAdmissionProber
	interval     time.Duration
	probeTimeout time.Duration
	retryDelay   time.Duration
	now          func() time.Time

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewOpenAIAgentIdentityAdmissionWorker(
	store openAIAgentIdentityAdmissionStore,
	prober OpenAIAgentIdentityAdmissionProber,
	opts OpenAIAgentIdentityAdmissionWorkerOptions,
) *OpenAIAgentIdentityAdmissionWorker {
	if opts.Interval <= 0 {
		opts.Interval = defaultOpenAIAgentIdentityAdmissionInterval
	}
	if opts.ProbeTimeout <= 0 {
		opts.ProbeTimeout = defaultOpenAIAgentIdentityAdmissionProbeTimeout
	}
	if opts.RetryDelay <= 0 {
		opts.RetryDelay = defaultOpenAIAgentIdentityAdmissionRetryDelay
	}
	return &OpenAIAgentIdentityAdmissionWorker{
		store:        store,
		prober:       prober,
		interval:     opts.Interval,
		probeTimeout: opts.ProbeTimeout,
		retryDelay:   opts.RetryDelay,
		now:          time.Now,
	}
}

func (w *OpenAIAgentIdentityAdmissionWorker) Start() {
	if w == nil || w.store == nil || w.prober == nil || w.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			if err := w.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("openai_agent_identity_admission_scan_failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (w *OpenAIAgentIdentityAdmissionWorker) Stop() {
	if w == nil || w.cancel == nil {
		return
	}
	w.cancel()
	w.wg.Wait()
	w.cancel = nil
}

func (w *OpenAIAgentIdentityAdmissionWorker) RunOnce(ctx context.Context) error {
	if w == nil || w.store == nil || w.prober == nil {
		return errors.New("openai agent identity admission worker is not configured")
	}
	accounts, err := w.store.FindByExtraField(ctx, "openai_token_source", OpenAITokenSourceAgentIdentity)
	if err != nil {
		return err
	}
	var firstErr error
	for i := range accounts {
		account := &accounts[i]
		if !account.IsOpenAIAgentIdentity() || !w.shouldProbe(account) {
			continue
		}
		if err := w.probeAccount(ctx, account); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			slog.Warn("openai_agent_identity_admission_failed", "account_id", account.ID, "error", err)
		}
	}
	return firstErr
}

func (w *OpenAIAgentIdentityAdmissionWorker) shouldProbe(account *Account) bool {
	if account == nil || account.GetExtraString("openai_admission_state") != OpenAIAgentIdentityAdmissionStatePending {
		return false
	}
	nextRetry := strings.TrimSpace(account.GetExtraString("openai_admission_next_retry_at"))
	if nextRetry == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339, nextRetry)
	return err != nil || !w.now().Before(parsed)
}

func (w *OpenAIAgentIdentityAdmissionWorker) probeAccount(ctx context.Context, account *Account) error {
	if err := w.store.SetSchedulable(ctx, account.ID, false); err != nil {
		return fmt.Errorf("quarantine account %d: %w", account.ID, err)
	}
	probeCtx, cancel := context.WithTimeout(ctx, w.probeTimeout)
	result, probeErr := w.prober.Probe(probeCtx, account)
	cancel()
	if probeErr != nil {
		result = OpenAIAgentIdentityAdmissionProbeResult{
			Verdict: OpenAIAgentIdentityAdmissionVerdictRetry,
			Stage:   admissionStageOrDefault(result.Stage),
			Message: probeErr.Error(),
		}
	}

	now := w.now().UTC()
	attempts := account.getExtraInt("openai_admission_attempts") + 1
	common := map[string]any{
		"openai_admission_attempts":        attempts,
		"openai_admission_stage":           admissionStageOrDefault(result.Stage),
		"openai_admission_last_checked_at": now.Format(time.RFC3339),
		"openai_admission_last_status":     result.StatusCode,
		"openai_admission_last_error":      strings.TrimSpace(result.Message),
	}

	switch result.Verdict {
	case OpenAIAgentIdentityAdmissionVerdictAdmit:
		common["openai_admission_state"] = OpenAIAgentIdentityAdmissionStateAdmitted
		common["openai_admission_stage"] = OpenAIAgentIdentityAdmissionStageComplete
		common["openai_admission_next_retry_at"] = ""
		common["openai_pool_role"] = OpenAIPoolRoleMain
		common["openai_auth_state"] = OpenAIAuthStateHealthy
		common["openai_validation_outcome"] = OpenAIValidationOutcomeAgentIdentityValidated
		common["openai_last_refresh_error_code"] = ""
		common["openai_last_validated_at"] = now.Format(time.RFC3339)
		if err := w.store.UpdateExtra(ctx, account.ID, common); err != nil {
			return err
		}
		if err := w.store.ClearError(ctx, account.ID); err != nil {
			return err
		}
		if err := w.store.SetSchedulable(ctx, account.ID, true); err != nil {
			return err
		}
		slog.Info("openai_agent_identity_admitted", "account_id", account.ID, "attempts", attempts)
		return nil

	case OpenAIAgentIdentityAdmissionVerdictReject:
		common["openai_admission_state"] = OpenAIAgentIdentityAdmissionStateRejected
		common["openai_admission_next_retry_at"] = ""
		common["openai_pool_role"] = OpenAIPoolRoleQuarantine
		common["openai_auth_state"] = OpenAIAuthStateTerminal
		common["openai_validation_outcome"] = OpenAIValidationOutcomeAgentIdentityQuarantined
		common["openai_last_refresh_error_code"] = admissionErrorCode(result)
		if err := w.store.UpdateExtra(ctx, account.ID, common); err != nil {
			return err
		}
		message := fmt.Sprintf("Agent Identity admission %s failed", admissionStageOrDefault(result.Stage))
		if result.StatusCode > 0 {
			message += fmt.Sprintf(" (%d)", result.StatusCode)
		}
		if strings.TrimSpace(result.Message) != "" {
			message += ": " + strings.TrimSpace(result.Message)
		}
		return w.store.SetError(ctx, account.ID, message)

	default:
		common["openai_admission_state"] = OpenAIAgentIdentityAdmissionStatePending
		common["openai_admission_next_retry_at"] = now.Add(w.retryDelay).Format(time.RFC3339)
		common["openai_pool_role"] = OpenAIPoolRoleQuarantine
		common["openai_auth_state"] = OpenAIAuthStateCooling
		common["openai_validation_outcome"] = OpenAIValidationOutcomeAgentIdentityPending
		return w.store.UpdateExtra(ctx, account.ID, common)
	}
}

func admissionStageOrDefault(stage string) string {
	stage = strings.TrimSpace(stage)
	if stage == "" {
		return OpenAIAgentIdentityAdmissionStageResponses
	}
	return stage
}

func admissionErrorCode(result OpenAIAgentIdentityAdmissionProbeResult) string {
	if result.StatusCode == http.StatusUnauthorized {
		return openAIAuthErrorCodeTokenInvalidated
	}
	if result.StatusCode == http.StatusPaymentRequired {
		return openAIAuthErrorCodeWorkspaceDown
	}
	return "agent_identity_admission_failed"
}

type openAIAgentIdentityAdmissionTransport interface {
	Responses(ctx context.Context, account *Account, headers http.Header, body []byte) (int, http.Header, []byte, error)
	NativeSearch(ctx context.Context, account *Account, headers http.Header, body []byte) (int, http.Header, []byte, error)
}

type openAIAgentIdentityGatewayAdmissionTransport struct {
	gateway *OpenAIGatewayService
}

func (t openAIAgentIdentityGatewayAdmissionTransport) Responses(ctx context.Context, account *Account, headers http.Header, body []byte) (int, http.Header, []byte, error) {
	response, err := t.gateway.DoNativeResponsesRequest(ctx, account, headers, body, true)
	if err != nil {
		return 0, nil, nil, err
	}
	defer response.Body.Close()
	responseBody, readErr := readAdmissionResponseBody(response.Body)
	return response.StatusCode, response.Header, responseBody, readErr
}

func (t openAIAgentIdentityGatewayAdmissionTransport) NativeSearch(ctx context.Context, account *Account, headers http.Header, body []byte) (int, http.Header, []byte, error) {
	response, err := t.gateway.ForwardNativeSearch(ctx, nil, account, headers, body)
	if err != nil {
		return 0, nil, nil, err
	}
	return response.StatusCode, response.Headers, response.Body, nil
}

type OpenAIAgentIdentityAdmissionGatewayProber struct {
	transport    openAIAgentIdentityAdmissionTransport
	model        string
	compactBytes int
}

func NewOpenAIAgentIdentityAdmissionGatewayProber(gateway *OpenAIGatewayService) *OpenAIAgentIdentityAdmissionGatewayProber {
	return &OpenAIAgentIdentityAdmissionGatewayProber{
		transport:    openAIAgentIdentityGatewayAdmissionTransport{gateway: gateway},
		model:        defaultOpenAIAgentIdentityAdmissionModel,
		compactBytes: defaultOpenAIAgentIdentityAdmissionCompactBytes,
	}
}

func (p *OpenAIAgentIdentityAdmissionGatewayProber) Probe(ctx context.Context, account *Account) (OpenAIAgentIdentityAdmissionProbeResult, error) {
	if p == nil || p.transport == nil {
		return OpenAIAgentIdentityAdmissionProbeResult{}, errors.New("agent identity admission transport is not configured")
	}
	headers := http.Header{
		"User-Agent": []string{"codex_cli_rs/agent-identity-admission"},
		"originator": []string{"codex_cli_rs"},
	}

	responsesBody, err := json.Marshal(map[string]any{
		"model":  p.model,
		"input":  "Reply with exactly OK.",
		"stream": true,
		"store":  false,
	})
	if err != nil {
		return OpenAIAgentIdentityAdmissionProbeResult{}, err
	}
	status, _, body, requestErr := p.transport.Responses(ctx, account, headers, responsesBody)
	if result := admissionHTTPResult(OpenAIAgentIdentityAdmissionStageResponses, status, body, requestErr); result != nil {
		return *result, nil
	}
	if err := validateAdmissionResponsesSSE(body); err != nil {
		return retryAdmissionResult(OpenAIAgentIdentityAdmissionStageResponses, status, err), nil
	}

	compactBody, err := buildAdmissionCompactBody(p.model, p.compactBytes)
	if err != nil {
		return OpenAIAgentIdentityAdmissionProbeResult{}, err
	}
	status, _, body, requestErr = p.transport.Responses(ctx, account, headers, compactBody)
	if result := admissionHTTPResult(OpenAIAgentIdentityAdmissionStageCompact, status, body, requestErr); result != nil {
		return *result, nil
	}
	if err := validateAdmissionCompactSSE(body); err != nil {
		return retryAdmissionResult(OpenAIAgentIdentityAdmissionStageCompact, status, err), nil
	}

	searchBody, err := json.Marshal(map[string]any{
		"id":       fmt.Sprintf("agent-identity-admission-%d", account.ID),
		"model":    p.model,
		"commands": map[string]any{},
	})
	if err != nil {
		return OpenAIAgentIdentityAdmissionProbeResult{}, err
	}
	status, _, body, requestErr = p.transport.NativeSearch(ctx, account, headers, searchBody)
	if result := admissionHTTPResult(OpenAIAgentIdentityAdmissionStageNativeSearch, status, body, requestErr); result != nil {
		return *result, nil
	}
	if err := validateOpenAINativeSearchResponse(body); err != nil {
		return retryAdmissionResult(OpenAIAgentIdentityAdmissionStageNativeSearch, status, err), nil
	}

	return OpenAIAgentIdentityAdmissionProbeResult{
		Verdict: OpenAIAgentIdentityAdmissionVerdictAdmit,
		Stage:   OpenAIAgentIdentityAdmissionStageComplete,
	}, nil
}

func admissionHTTPResult(stage string, status int, body []byte, requestErr error) *OpenAIAgentIdentityAdmissionProbeResult {
	if requestErr != nil {
		result := retryAdmissionResult(stage, status, requestErr)
		return &result
	}
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		return nil
	}
	message := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	if message == "" {
		message = http.StatusText(status)
	}
	verdict := OpenAIAgentIdentityAdmissionVerdictRetry
	if isTerminalAgentIdentityAdmissionFailure(status, body) {
		verdict = OpenAIAgentIdentityAdmissionVerdictReject
	}
	return &OpenAIAgentIdentityAdmissionProbeResult{
		Verdict:    verdict,
		Stage:      stage,
		StatusCode: status,
		Message:    message,
	}
}

func isTerminalAgentIdentityAdmissionFailure(status int, body []byte) bool {
	if status == http.StatusUnauthorized || status == http.StatusPaymentRequired {
		return true
	}
	normalized := strings.ToLower(string(body))
	terminalMarkers := []string{
		"token_invalidated",
		"token revoked",
		"token_revoked",
		"deactivated_workspace",
		"workspace has been deactivated",
	}
	for _, marker := range terminalMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func retryAdmissionResult(stage string, status int, err error) OpenAIAgentIdentityAdmissionProbeResult {
	message := "admission probe failed"
	if err != nil {
		message = err.Error()
	}
	return OpenAIAgentIdentityAdmissionProbeResult{
		Verdict:    OpenAIAgentIdentityAdmissionVerdictRetry,
		Stage:      stage,
		StatusCode: status,
		Message:    message,
	}
}

func buildAdmissionCompactBody(model string, targetBytes int) ([]byte, error) {
	if targetBytes <= 0 {
		targetBytes = defaultOpenAIAgentIdentityAdmissionCompactBytes
	}
	marker := "Agent Identity admission compact canary. Preserve ADMISSION_OK. "
	repeat := "0123456789abcdef "
	text := marker + strings.Repeat(repeat, targetBytes/len(repeat)+1)
	if len(text) > targetBytes {
		text = text[:targetBytes]
	}
	return json.Marshal(map[string]any{
		"model":  model,
		"stream": true,
		"store":  true,
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{map[string]any{
					"type": "input_text",
					"text": text,
				}},
			},
			map[string]any{"type": "compaction_trigger"},
		},
	})
}

func readAdmissionResponseBody(body io.Reader) ([]byte, error) {
	if body == nil {
		return nil, errors.New("admission response body is missing")
	}
	data, err := io.ReadAll(io.LimitReader(body, openAIAgentIdentityAdmissionResponseLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > openAIAgentIdentityAdmissionResponseLimit {
		return nil, errors.New("admission response exceeds size limit")
	}
	return data, nil
}

func parseAdmissionSSE(body []byte) ([]map[string]any, error) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 64*1024), openAIAgentIdentityAdmissionResponseLimit)
	var events []map[string]any
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return nil, fmt.Errorf("parse admission SSE: %w", err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, errors.New("admission response returned no SSE data")
	}
	return events, nil
}

func validateAdmissionResponsesSSE(body []byte) error {
	events, err := parseAdmissionSSE(body)
	if err != nil {
		return err
	}
	for _, event := range events {
		if stringValue(event["type"]) != "response.completed" {
			continue
		}
		response, _ := event["response"].(map[string]any)
		if stringValue(response["status"]) != "completed" {
			return errors.New("Responses admission canary was not completed")
		}
		if admissionContainsExactOutputText(response, "OK") {
			return nil
		}
		return errors.New("Responses admission canary did not return OK")
	}
	return errors.New("Responses admission canary returned no completed event")
}

func validateAdmissionCompactSSE(body []byte) error {
	events, err := parseAdmissionSSE(body)
	if err != nil {
		return err
	}
	foundCompaction := false
	foundCompleted := false
	for _, event := range events {
		if admissionContainsType(event, "compaction") || admissionContainsType(event, "compaction_summary") {
			foundCompaction = true
		}
		if stringValue(event["type"]) == "response.completed" {
			response, _ := event["response"].(map[string]any)
			if stringValue(response["status"]) == "completed" {
				foundCompleted = true
			}
		}
	}
	if !foundCompaction {
		return errors.New("Compact admission canary returned no compaction output")
	}
	if !foundCompleted {
		return errors.New("Compact admission canary returned no completed event")
	}
	return nil
}

func admissionContainsExactOutputText(value any, expected string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if stringValue(typed["type"]) == "output_text" && strings.TrimSpace(stringValue(typed["text"])) == expected {
			return true
		}
		for _, child := range typed {
			if admissionContainsExactOutputText(child, expected) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if admissionContainsExactOutputText(child, expected) {
				return true
			}
		}
	}
	return false
}

func admissionContainsType(value any, expected string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if stringValue(typed["type"]) == expected {
			return true
		}
		for _, child := range typed {
			if admissionContainsType(child, expected) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if admissionContainsType(child, expected) {
				return true
			}
		}
	}
	return false
}
