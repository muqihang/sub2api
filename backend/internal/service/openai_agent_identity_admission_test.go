package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type agentIdentityAdmissionStoreStub struct {
	accounts       []Account
	extraUpdates   map[int64]map[string]any
	schedulable    map[int64]bool
	clearedErrors  []int64
	terminalErrors map[int64]string
}

func (s *agentIdentityAdmissionStoreStub) FindByExtraField(_ context.Context, key string, value any) ([]Account, error) {
	if key != "openai_token_source" || value != OpenAITokenSourceAgentIdentity {
		return nil, errors.New("unexpected admission lookup")
	}
	return append([]Account(nil), s.accounts...), nil
}

func (s *agentIdentityAdmissionStoreStub) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	if s.extraUpdates == nil {
		s.extraUpdates = map[int64]map[string]any{}
	}
	s.extraUpdates[id] = cloneJSONMap(updates)
	return nil
}

func (s *agentIdentityAdmissionStoreStub) SetSchedulable(_ context.Context, id int64, schedulable bool) error {
	if s.schedulable == nil {
		s.schedulable = map[int64]bool{}
	}
	s.schedulable[id] = schedulable
	return nil
}

func (s *agentIdentityAdmissionStoreStub) ClearError(_ context.Context, id int64) error {
	s.clearedErrors = append(s.clearedErrors, id)
	return nil
}

func (s *agentIdentityAdmissionStoreStub) SetError(_ context.Context, id int64, message string) error {
	if s.terminalErrors == nil {
		s.terminalErrors = map[int64]string{}
	}
	s.terminalErrors[id] = message
	return nil
}

type agentIdentityAdmissionProberStub struct {
	result OpenAIAgentIdentityAdmissionProbeResult
	err    error
	calls  []int64
}

func (s *agentIdentityAdmissionProberStub) Probe(_ context.Context, account *Account) (OpenAIAgentIdentityAdmissionProbeResult, error) {
	s.calls = append(s.calls, account.ID)
	return s.result, s.err
}

func pendingAgentIdentityAccount(id int64) Account {
	return Account{
		ID:       id,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"auth_mode": "agentIdentity",
		},
		Extra: map[string]any{
			"openai_token_source":    OpenAITokenSourceAgentIdentity,
			"openai_admission_state": OpenAIAgentIdentityAdmissionStatePending,
		},
		Status:      StatusDisabled,
		Schedulable: false,
	}
}

func TestOpenAIAgentIdentityAdmissionRunOnceAdmitsOnlyAfterAllProbesPass(t *testing.T) {
	store := &agentIdentityAdmissionStoreStub{accounts: []Account{pendingAgentIdentityAccount(215)}}
	prober := &agentIdentityAdmissionProberStub{result: OpenAIAgentIdentityAdmissionProbeResult{
		Verdict: OpenAIAgentIdentityAdmissionVerdictAdmit,
		Stage:   OpenAIAgentIdentityAdmissionStageComplete,
	}}
	worker := NewOpenAIAgentIdentityAdmissionWorker(store, prober, OpenAIAgentIdentityAdmissionWorkerOptions{
		ProbeTimeout: time.Second,
		RetryDelay:   time.Minute,
	})

	require.NoError(t, worker.RunOnce(context.Background()))
	require.Equal(t, []int64{215}, prober.calls)
	require.Equal(t, true, store.schedulable[215])
	require.Equal(t, []int64{215}, store.clearedErrors)
	require.Empty(t, store.terminalErrors)
	require.Equal(t, OpenAIAgentIdentityAdmissionStateAdmitted, store.extraUpdates[215]["openai_admission_state"])
	require.Equal(t, OpenAIPoolRoleMain, store.extraUpdates[215]["openai_pool_role"])
	require.Equal(t, OpenAIValidationOutcomeAgentIdentityValidated, store.extraUpdates[215]["openai_validation_outcome"])
}

func TestOpenAIAgentIdentityAdmissionRunOnceRejectsTerminalProbeFailure(t *testing.T) {
	store := &agentIdentityAdmissionStoreStub{accounts: []Account{pendingAgentIdentityAccount(216)}}
	prober := &agentIdentityAdmissionProberStub{result: OpenAIAgentIdentityAdmissionProbeResult{
		Verdict:    OpenAIAgentIdentityAdmissionVerdictReject,
		Stage:      OpenAIAgentIdentityAdmissionStageResponses,
		StatusCode: 402,
		Message:    "workspace has been deactivated",
	}}
	worker := NewOpenAIAgentIdentityAdmissionWorker(store, prober, OpenAIAgentIdentityAdmissionWorkerOptions{})

	require.NoError(t, worker.RunOnce(context.Background()))
	require.Equal(t, false, store.schedulable[216])
	require.Contains(t, store.terminalErrors[216], "402")
	require.Equal(t, OpenAIAgentIdentityAdmissionStateRejected, store.extraUpdates[216]["openai_admission_state"])
	require.Equal(t, OpenAIAuthStateTerminal, store.extraUpdates[216]["openai_auth_state"])
	require.Equal(t, OpenAIValidationOutcomeAgentIdentityQuarantined, store.extraUpdates[216]["openai_validation_outcome"])
}

func TestOpenAIAgentIdentityAdmissionRunOnceKeepsRetryableFailureQuarantined(t *testing.T) {
	account := pendingAgentIdentityAccount(217)
	account.Extra["openai_admission_attempts"] = float64(2)
	store := &agentIdentityAdmissionStoreStub{accounts: []Account{account}}
	prober := &agentIdentityAdmissionProberStub{result: OpenAIAgentIdentityAdmissionProbeResult{
		Verdict:    OpenAIAgentIdentityAdmissionVerdictRetry,
		Stage:      OpenAIAgentIdentityAdmissionStageCompact,
		StatusCode: 503,
		Message:    "upstream unavailable",
	}}
	worker := NewOpenAIAgentIdentityAdmissionWorker(store, prober, OpenAIAgentIdentityAdmissionWorkerOptions{
		RetryDelay: time.Minute,
	})

	require.NoError(t, worker.RunOnce(context.Background()))
	require.Equal(t, false, store.schedulable[217])
	require.Empty(t, store.clearedErrors)
	require.Empty(t, store.terminalErrors)
	require.Equal(t, OpenAIAgentIdentityAdmissionStatePending, store.extraUpdates[217]["openai_admission_state"])
	require.Equal(t, 3, store.extraUpdates[217]["openai_admission_attempts"])
	require.NotEmpty(t, store.extraUpdates[217]["openai_admission_next_retry_at"])
}

func TestOpenAIAgentIdentityAdmissionRunOnceSkipsAccountsBeforeRetryTime(t *testing.T) {
	account := pendingAgentIdentityAccount(218)
	account.Extra["openai_admission_next_retry_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	store := &agentIdentityAdmissionStoreStub{accounts: []Account{account}}
	prober := &agentIdentityAdmissionProberStub{}
	worker := NewOpenAIAgentIdentityAdmissionWorker(store, prober, OpenAIAgentIdentityAdmissionWorkerOptions{})

	require.NoError(t, worker.RunOnce(context.Background()))
	require.Empty(t, prober.calls)
}

type agentIdentityAdmissionTransportResponse struct {
	status int
	body   string
	err    error
}

type agentIdentityAdmissionTransportStub struct {
	responses []agentIdentityAdmissionTransportResponse
	search    agentIdentityAdmissionTransportResponse
	bodies    [][]byte
}

func (s *agentIdentityAdmissionTransportStub) Responses(_ context.Context, _ *Account, _ http.Header, body []byte) (int, http.Header, []byte, error) {
	s.bodies = append(s.bodies, append([]byte(nil), body...))
	if len(s.responses) == 0 {
		return 0, nil, nil, errors.New("unexpected Responses probe")
	}
	response := s.responses[0]
	s.responses = s.responses[1:]
	return response.status, http.Header{"Content-Type": []string{"text/event-stream"}}, []byte(response.body), response.err
}

func (s *agentIdentityAdmissionTransportStub) NativeSearch(_ context.Context, _ *Account, _ http.Header, body []byte) (int, http.Header, []byte, error) {
	s.bodies = append(s.bodies, append([]byte(nil), body...))
	return s.search.status, http.Header{"Content-Type": []string{"application/json"}}, []byte(s.search.body), s.search.err
}

const admissionResponsesSuccessSSE = "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"OK\"}]}]}}\n\n"

const admissionCompactSuccessSSE = "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"compaction\"}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"compaction\"}]}}\n\n"

func TestOpenAIAgentIdentityAdmissionGatewayProberRequiresAllThreeCapabilities(t *testing.T) {
	transport := &agentIdentityAdmissionTransportStub{
		responses: []agentIdentityAdmissionTransportResponse{
			{status: http.StatusOK, body: admissionResponsesSuccessSSE},
			{status: http.StatusOK, body: admissionCompactSuccessSSE},
		},
		search: agentIdentityAdmissionTransportResponse{status: http.StatusOK, body: `{"output":"search-ok"}`},
	}
	prober := &OpenAIAgentIdentityAdmissionGatewayProber{
		transport:    transport,
		model:        "gpt-test",
		compactBytes: 1024,
	}
	account := pendingAgentIdentityAccount(219)

	result, err := prober.Probe(context.Background(), &account)
	require.NoError(t, err)
	require.Equal(t, OpenAIAgentIdentityAdmissionVerdictAdmit, result.Verdict)
	require.Equal(t, OpenAIAgentIdentityAdmissionStageComplete, result.Stage)
	require.Len(t, transport.bodies, 3)
	require.Contains(t, string(transport.bodies[0]), "Reply with exactly OK")
	require.Contains(t, string(transport.bodies[1]), "compaction_trigger")
	require.GreaterOrEqual(t, len(transport.bodies[1]), 1024)
	require.Contains(t, string(transport.bodies[2]), `"commands":{}`)
}

func TestOpenAIAgentIdentityAdmissionGatewayProberRejectsDeactivatedWorkspace(t *testing.T) {
	transport := &agentIdentityAdmissionTransportStub{
		responses: []agentIdentityAdmissionTransportResponse{{
			status: http.StatusPaymentRequired,
			body:   `{"detail":{"code":"deactivated_workspace","message":"workspace has been deactivated"}}`,
		}},
	}
	prober := &OpenAIAgentIdentityAdmissionGatewayProber{transport: transport, model: "gpt-test", compactBytes: 1024}
	account := pendingAgentIdentityAccount(220)

	result, err := prober.Probe(context.Background(), &account)
	require.NoError(t, err)
	require.Equal(t, OpenAIAgentIdentityAdmissionVerdictReject, result.Verdict)
	require.Equal(t, OpenAIAgentIdentityAdmissionStageResponses, result.Stage)
	require.Equal(t, http.StatusPaymentRequired, result.StatusCode)
	require.Len(t, transport.bodies, 1)
}

func TestOpenAIAgentIdentityAdmissionGatewayProberRetriesCompact503(t *testing.T) {
	transport := &agentIdentityAdmissionTransportStub{
		responses: []agentIdentityAdmissionTransportResponse{
			{status: http.StatusOK, body: admissionResponsesSuccessSSE},
			{status: http.StatusServiceUnavailable, body: `{"error":{"message":"temporarily unavailable"}}`},
		},
	}
	prober := &OpenAIAgentIdentityAdmissionGatewayProber{transport: transport, model: "gpt-test", compactBytes: 1024}
	account := pendingAgentIdentityAccount(221)

	result, err := prober.Probe(context.Background(), &account)
	require.NoError(t, err)
	require.Equal(t, OpenAIAgentIdentityAdmissionVerdictRetry, result.Verdict)
	require.Equal(t, OpenAIAgentIdentityAdmissionStageCompact, result.Stage)
	require.Equal(t, http.StatusServiceUnavailable, result.StatusCode)
	require.Len(t, transport.bodies, 2)
}

func TestOpenAIAgentIdentityAdmissionGatewayProberKeepsUsageLimited403ForRetry(t *testing.T) {
	transport := &agentIdentityAdmissionTransportStub{
		responses: []agentIdentityAdmissionTransportResponse{{
			status: http.StatusForbidden,
			body:   `{"error":{"type":"usage_limit_reached","message":"The usage limit has been reached"}}`,
		}},
	}
	prober := &OpenAIAgentIdentityAdmissionGatewayProber{transport: transport, model: "gpt-test", compactBytes: 1024}
	account := pendingAgentIdentityAccount(223)

	result, err := prober.Probe(context.Background(), &account)
	require.NoError(t, err)
	require.Equal(t, OpenAIAgentIdentityAdmissionVerdictRetry, result.Verdict)
	require.Equal(t, OpenAIAgentIdentityAdmissionStageResponses, result.Stage)
	require.Equal(t, http.StatusForbidden, result.StatusCode)
	require.Len(t, transport.bodies, 1)
}

func TestOpenAIAgentIdentityAdmissionGatewayProberRetriesInvalidNativeSearchContract(t *testing.T) {
	transport := &agentIdentityAdmissionTransportStub{
		responses: []agentIdentityAdmissionTransportResponse{
			{status: http.StatusOK, body: admissionResponsesSuccessSSE},
			{status: http.StatusOK, body: admissionCompactSuccessSSE},
		},
		search: agentIdentityAdmissionTransportResponse{status: http.StatusOK, body: `{}`},
	}
	prober := &OpenAIAgentIdentityAdmissionGatewayProber{transport: transport, model: "gpt-test", compactBytes: 1024}
	account := pendingAgentIdentityAccount(222)

	result, err := prober.Probe(context.Background(), &account)
	require.NoError(t, err)
	require.Equal(t, OpenAIAgentIdentityAdmissionVerdictRetry, result.Verdict)
	require.Equal(t, OpenAIAgentIdentityAdmissionStageNativeSearch, result.Stage)
	require.Len(t, transport.bodies, 3)
}
