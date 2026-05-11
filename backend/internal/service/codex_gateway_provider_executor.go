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
	Request      CodexGatewayResponsesRequest
	Model        CodexGatewayModel
	Parsed       CodexGatewayResponsesCreateRequest
	SessionKey   string
	IsolationKey string
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
	cfg           *config.Config
	openaiGateway *OpenAIGatewayService
	accountSelector codexGatewayAccountSelector
	stateStore    *CodexGatewayStateStore
	usageRecorder codexGatewayUsageRecorder
	openaiAdapter codexGatewayProviderAdapter
	deepseek      codexGatewayProviderAdapter
}

func NewCodexGatewayProviderExecutor(cfg *config.Config, openaiGateway *OpenAIGatewayService, stateStore *CodexGatewayStateStore) *CodexGatewayProviderExecutor {
	executor := &CodexGatewayProviderExecutor{
		cfg:           cfg,
		openaiGateway: openaiGateway,
		accountSelector: openaiGateway,
		stateStore:    stateStore,
		usageRecorder: openaiGateway,
	}
	executor.openaiAdapter = &codexGatewayOpenAIResponsesAdapter{gateway: openaiGateway}
	executor.deepseek = &codexGatewayDeepSeekProviderAdapter{stateStore: stateStore}
	return executor
}

func (e *CodexGatewayProviderExecutor) Complete(ctx context.Context, req CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
	startedAt := time.Now()
	groupID, adapter, err := e.providerGroupAndAdapter(req.Model)
	if err != nil {
		return nil, err
	}

	var lastFailover *UpstreamFailoverError
	excluded := make(map[int64]struct{})
	for {
		account, err := e.selectAccount(ctx, groupID, req, excluded)
		if err != nil {
			var unavailable *CodexGatewayProviderUnavailableError
			if lastFailover != nil && errors.As(err, &unavailable) && unavailable.Kind == CodexGatewayProviderUnavailableNoAccounts {
				return nil, lastFailover
			}
			return nil, err
		}
		result, err := adapter.Complete(ctx, account, req)
		if err == nil {
			codexGatewayRecordUsageBestEffort(ctx, e.usageRecorder, req, account, result.ProviderResult, false, startedAt)
			return &result.ServiceResponse, nil
		}
		var failoverErr *UpstreamFailoverError
		if errors.As(err, &failoverErr) {
			lastFailover = failoverErr
			excluded[account.ID] = struct{}{}
			continue
		}
		return nil, err
	}
}

func (e *CodexGatewayProviderExecutor) Stream(ctx context.Context, req CodexGatewayProviderRequest) error {
	startedAt := time.Now()
	groupID, adapter, err := e.providerGroupAndAdapter(req.Model)
	if err != nil {
		return err
	}

	var lastFailover *UpstreamFailoverError
	excluded := make(map[int64]struct{})
	for {
		account, err := e.selectAccount(ctx, groupID, req, excluded)
		if err != nil {
			var unavailable *CodexGatewayProviderUnavailableError
			if lastFailover != nil && errors.As(err, &unavailable) && unavailable.Kind == CodexGatewayProviderUnavailableNoAccounts {
				return lastFailover
			}
			return err
		}
		providerResult, err := adapter.Stream(ctx, account, req)
		if err == nil {
			codexGatewayRecordUsageBestEffort(ctx, e.usageRecorder, req, account, providerResult, true, startedAt)
			return nil
		}
		var failoverErr *UpstreamFailoverError
		if errors.As(err, &failoverErr) {
			lastFailover = failoverErr
			excluded[account.ID] = struct{}{}
			continue
		}
		return err
	}
}

func (e *CodexGatewayProviderExecutor) providerGroupAndAdapter(model CodexGatewayModel) (int64, codexGatewayProviderAdapter, error) {
	if e == nil || e.cfg == nil {
		return 0, nil, fmt.Errorf("codex gateway provider executor is not configured")
	}
	switch strings.TrimSpace(model.Provider) {
	case "openai":
		if e.cfg.Gateway.Codex.ProviderGroups.OpenAI <= 0 {
			return 0, nil, &CodexGatewayProviderUnavailableError{ModelID: model.Slug, Provider: model.Provider, Kind: CodexGatewayProviderUnavailableNoProviderGroup}
		}
		return e.cfg.Gateway.Codex.ProviderGroups.OpenAI, e.openaiAdapter, nil
	case "deepseek":
		if e.cfg.Gateway.Codex.ProviderGroups.DeepSeek <= 0 {
			return 0, nil, &CodexGatewayProviderUnavailableError{ModelID: model.Slug, Provider: model.Provider, Kind: CodexGatewayProviderUnavailableNoProviderGroup}
		}
		return e.cfg.Gateway.Codex.ProviderGroups.DeepSeek, e.deepseek, nil
	default:
		return 0, nil, fmt.Errorf("unsupported codex gateway provider %q", model.Provider)
	}
}

func (e *CodexGatewayProviderExecutor) selectAccount(ctx context.Context, groupID int64, req CodexGatewayProviderRequest, excluded map[int64]struct{}) (*Account, error) {
	if e == nil || e.accountSelector == nil {
		return nil, fmt.Errorf("codex gateway account selector is not configured")
	}
	account, err := e.accountSelector.SelectAccountForModelWithExclusions(ctx, &groupID, req.SessionKey, req.Model.UpstreamModel, excluded)
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

type codexGatewayDeepSeekProviderAdapter struct {
	stateStore *CodexGatewayStateStore
}

func (a *codexGatewayDeepSeekProviderAdapter) Complete(ctx context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
	if account == nil {
		return CodexGatewayDeepSeekAdapterResult{}, fmt.Errorf("codex gateway deepseek adapter requires selected account")
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
			SessionKey:   req.SessionKey,
			IsolationKey: req.IsolationKey,
			UserID:       fmt.Sprintf("user:%d", req.Request.APIKey.UserID),
		},
		CodexGatewayDeepSeekRequestConfig{},
	)
}

func (a *codexGatewayDeepSeekProviderAdapter) Stream(ctx context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayProviderResult, error) {
	if account == nil {
		return CodexGatewayProviderResult{}, fmt.Errorf("codex gateway deepseek adapter requires selected account")
	}
	if req.Request.ResponseHeader != nil {
		req.Request.ResponseHeader.Set("Content-Type", "text/event-stream")
		req.Request.ResponseHeader.Set("Cache-Control", "no-cache")
		req.Request.ResponseHeader.Set("Connection", "keep-alive")
	}
	if req.Request.WriteStatus != nil {
		req.Request.WriteStatus(http.StatusOK)
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
			SessionKey:   req.SessionKey,
			IsolationKey: req.IsolationKey,
			UserID:       fmt.Sprintf("user:%d", req.Request.APIKey.UserID),
		},
		CodexGatewayDeepSeekRequestConfig{},
		req.Request.StreamWriter,
	)
	if err != nil {
		return CodexGatewayProviderResult{}, err
	}
	copyCodexGatewayHTTPHeaders(req.Request.ResponseHeader, result.ServiceResponse.Headers)
	return result.ProviderResult, nil
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
