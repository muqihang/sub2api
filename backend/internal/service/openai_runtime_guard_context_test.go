package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIRuntimeGuardContextObviousOverLimitBlocksNativeBeforeUpstream(t *testing.T) {
	upstream := newOpenAIRuntimeGuardShapeUpstream()
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := newOpenAIRuntimeGuardShapeOAuthAccount()
	body := []byte(`{"model":"gpt-5.4","input":"` + strings.Repeat("x", openAIRuntimeGuardContextDefaultLimitTokens*5) + `"}`)

	resp, err := svc.DoNativeResponsesRequest(context.Background(), account, nil, body, false)

	require.Nil(t, resp)
	require.Error(t, err)
	var blocked *OpenAIRuntimeGuardBlockedError
	require.ErrorAs(t, err, &blocked)
	require.Equal(t, http.StatusRequestEntityTooLarge, blocked.StatusCode)
	require.Equal(t, "context.obviously_over_limit", blocked.Decision.Category)
	require.Equal(t, "local_policy_block", gjson.GetBytes(blocked.Payload, "error.code").String())
	require.NotContains(t, string(blocked.Payload), strings.Repeat("x", 32))
	require.Len(t, upstream.bodies, 0, "local context block must not call upstream")
}

func TestOpenAIRuntimeGuardContextNearBoundaryPassesNativeUpstream(t *testing.T) {
	upstream := newOpenAIRuntimeGuardShapeUpstream()
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := newOpenAIRuntimeGuardShapeOAuthAccount()
	body := []byte(`{"model":"gpt-5.4","input":"` + strings.Repeat("x", (openAIRuntimeGuardContextDefaultLimitTokens-1000)*4) + `"}`)

	resp, err := svc.DoNativeResponsesRequest(context.Background(), account, nil, body, false)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, upstream.bodies, 1, "near-boundary estimates must be left to upstream")
}

func TestOpenAIRuntimeGuardContextExplicitCompactPassesEvenWhenHuge(t *testing.T) {
	upstream, rec, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	c.Request.URL.Path = "/v1/responses/compact"
	body := []byte(`{"model":"gpt-5.4","input":"` + strings.Repeat("x", openAIRuntimeGuardContextDefaultLimitTokens*5) + `","prompt_cache_key":"compact-seed"}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, upstream.bodies, 1, "explicit compact path must stay upstream-routed")
}

func TestOpenAIRuntimeGuardContextMetadataDoesNotContainRawBody(t *testing.T) {
	upstream, rec, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	secret := "sk-local-raw-body-secret"
	body := []byte(`{"model":"gpt-5.4","instructions":"do not log ` + secret + `","input":"` + strings.Repeat("x", openAIRuntimeGuardContextDefaultLimitTokens*5) + `"}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.NotContains(t, rec.Body.String(), secret)
	meta, ok := c.Get(OpenAIRuntimeGuardMetadataKey)
	require.True(t, ok)
	encoded, marshalErr := json.Marshal(meta)
	require.NoError(t, marshalErr)
	require.NotContains(t, string(encoded), secret)
	require.NotContains(t, string(encoded), strings.Repeat("x", 32))
	require.Contains(t, string(encoded), "estimated_tokens")
	require.Len(t, upstream.bodies, 0)
}

func TestOpenAIRuntimeGuardContextLocalBlockDoesNotSetUpstreamErrorOrUsage(t *testing.T) {
	upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	body := []byte(`{"model":"gpt-5.4","input":"` + strings.Repeat("x", openAIRuntimeGuardContextDefaultLimitTokens*5) + `"}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.bodies, 0)
	_, hasStatus := c.Get(OpsUpstreamStatusCodeKey)
	_, hasMessage := c.Get(OpsUpstreamErrorMessageKey)
	_, hasErrors := c.Get(OpsUpstreamErrorsKey)
	require.False(t, hasStatus)
	require.False(t, hasMessage)
	require.False(t, hasErrors)
	require.True(t, HasOpsClientBusinessLimited(c))
}

func TestOpenAIRuntimeGuardContextShadowFlagDoesNotBlockObviousOverLimit(t *testing.T) {
	upstream := newOpenAIRuntimeGuardShapeUpstream()
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := newOpenAIRuntimeGuardShapeOAuthAccount()
	account.Extra = map[string]any{"openai_context_guard_mode": "shadow"}
	body := []byte(`{"model":"gpt-5.4","input":"` + strings.Repeat("x", openAIRuntimeGuardContextDefaultLimitTokens*5) + `"}`)

	resp, err := svc.DoNativeResponsesRequest(context.Background(), account, nil, body, false)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, upstream.bodies, 1, "shadow mode should only annotate, not locally block")
}

func TestOpenAIRuntimeGuardContextWSResponseCreateObviousOverLimitBlocks(t *testing.T) {
	account := newOpenAIRuntimeGuardWSOAuthAccount(OpenAIWSIngressModeCtxPool)
	payload := []byte(`{"type":"response.create","model":"gpt-5.4","input":"` + strings.Repeat("x", openAIRuntimeGuardContextDefaultLimitTokens*5) + `"}`)

	repaired, blocked, err := applyOpenAIReasoningEffortGuardToWSResponseCreatePayload(account, payload)

	require.NoError(t, err)
	require.NotNil(t, blocked)
	require.Equal(t, "context.obviously_over_limit", blocked.Decision.Category)
	require.Equal(t, http.StatusRequestEntityTooLarge, blocked.StatusCode)
	event := buildOpenAIRuntimeGuardBlockedWSEvent(blocked)
	require.Contains(t, gjson.GetBytes(event, "error.message").String(), "context")
	require.Equal(t, string(payload), string(repaired))
}

func TestOpenAIRuntimeGuardContextDisabledFlagBypassesObviousOverLimit(t *testing.T) {
	upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	account.Extra["openai_context_guard_mode"] = "disabled"
	body := []byte(`{"model":"gpt-5.4","input":"` + strings.Repeat("x", openAIRuntimeGuardContextDefaultLimitTokens*5) + `"}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	_, hasMeta := c.Get(OpenAIRuntimeGuardMetadataKey)
	require.False(t, hasMeta)
}

func TestOpenAIRuntimeGuardContextHTTPShadowModeRecordsStructuredMetadata(t *testing.T) {
	upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	account.Extra["openai_context_guard_mode"] = "shadow"
	secret := "raw-body-shadow-secret"
	body := []byte(`{"model":"gpt-5.4","instructions":"` + secret + `","input":"` + strings.Repeat("x", openAIRuntimeGuardContextDefaultLimitTokens*5) + `"}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	meta, ok := c.Get(OpenAIRuntimeGuardMetadataKey)
	require.True(t, ok)
	encoded, marshalErr := json.Marshal(meta)
	require.NoError(t, marshalErr)
	require.Contains(t, string(encoded), "openai_runtime_guard.context.shadow_blocked")
	require.Contains(t, string(encoded), "estimated_tokens")
	require.NotContains(t, string(encoded), secret)
	require.NotContains(t, string(encoded), strings.Repeat("x", 32))
}

func TestOpenAIRuntimeGuardContextAccountExtraLimitOverrideBlocksKnownSmallLimit(t *testing.T) {
	upstream := newOpenAIRuntimeGuardShapeUpstream()
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := newOpenAIRuntimeGuardShapeOAuthAccount()
	account.Extra = map[string]any{"openai_context_limit_tokens": 1000}
	body := []byte(`{"model":"gpt-5.4","input":"` + strings.Repeat("x", 200000) + `"}`)

	resp, err := svc.DoNativeResponsesRequest(context.Background(), account, nil, body, false)

	require.Nil(t, resp)
	require.Error(t, err)
	var blocked *OpenAIRuntimeGuardBlockedError
	require.ErrorAs(t, err, &blocked)
	require.Equal(t, "context.obviously_over_limit", blocked.Decision.Category)
	require.Equal(t, 1000, blocked.Decision.LimitTokens)
	require.Len(t, upstream.bodies, 0)
}

func TestOpenAIRuntimeGuardContextWSResponseCreateWithoutModelUsesResolvedModelForBlock(t *testing.T) {
	account := newOpenAIRuntimeGuardWSOAuthAccount(OpenAIWSIngressModeCtxPool)
	payload := []byte(`{"type":"response.create","previous_response_id":"resp_prev_1","input":"` + strings.Repeat("x", openAIRuntimeGuardContextDefaultLimitTokens*5) + `"}`)

	repaired, blocked, err := applyOpenAIReasoningEffortGuardToWSResponseCreatePayloadWithModel(account, payload, "gpt-5.5")

	require.NoError(t, err)
	require.NotNil(t, blocked)
	require.Equal(t, http.StatusRequestEntityTooLarge, blocked.StatusCode)
	require.Equal(t, "context.obviously_over_limit", blocked.Decision.Category)
	require.Equal(t, string(payload), string(repaired))
}

func TestOpenAIRuntimeGuardContextWSResponseCreateWithoutModelResolvedModelNearBoundaryPasses(t *testing.T) {
	account := newOpenAIRuntimeGuardWSOAuthAccount(OpenAIWSIngressModeCtxPool)
	payload := []byte(`{"type":"response.create","previous_response_id":"resp_prev_1","input":"` + strings.Repeat("x", (openAIRuntimeGuardContextDefaultLimitTokens-1000)*4) + `"}`)

	repaired, blocked, err := applyOpenAIReasoningEffortGuardToWSResponseCreatePayloadWithModel(account, payload, "gpt-5.5")

	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, string(payload), string(repaired))
}

func TestOpenAIRuntimeGuardContextWSResponseCreateWithoutModelUnknownResolvedModelPasses(t *testing.T) {
	account := newOpenAIRuntimeGuardWSOAuthAccount(OpenAIWSIngressModeCtxPool)
	payload := []byte(`{"type":"response.create","previous_response_id":"resp_prev_1","input":"` + strings.Repeat("x", openAIRuntimeGuardContextDefaultLimitTokens*5) + `"}`)

	repaired, blocked, err := applyOpenAIReasoningEffortGuardToWSResponseCreatePayloadWithModel(account, payload, "future-model")

	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, string(payload), string(repaired))
}

func TestOpenAIRuntimeGuardContextWSResponseCreateWithoutModelUsesMappedSessionModelForBlock(t *testing.T) {
	account := newOpenAIRuntimeGuardWSOAuthAccount(OpenAIWSIngressModeCtxPool)
	account.Credentials["model_mapping"] = map[string]any{"client-model": "gpt-5.5"}
	payload := []byte(`{"type":"response.create","previous_response_id":"resp_prev_1","input":"` + strings.Repeat("x", openAIRuntimeGuardContextDefaultLimitTokens*5) + `"}`)

	_, blocked, err := applyOpenAIReasoningEffortGuardToWSResponseCreatePayloadWithModel(account, payload, "client-model")

	require.NoError(t, err)
	require.NotNil(t, blocked)
	require.Equal(t, "context.obviously_over_limit", blocked.Decision.Category)
}

func TestOpenAIRuntimeGuardContextWSPassthroughFrameWithoutModelBlocksBeforeRelayPayload(t *testing.T) {
	account := newOpenAIRuntimeGuardWSOAuthAccount(OpenAIWSIngressModeCtxPool)
	followupFrame := []byte(`{"type":"response.create","previous_response_id":"resp_prev_1","input":"` + strings.Repeat("x", openAIRuntimeGuardContextDefaultLimitTokens*5) + `"}`)
	inner := &fakePassthroughFrameConn{reads: [][]byte{followupFrame}}
	wrapper := &openAIWSPolicyEnforcingFrameConn{
		inner: inner,
		filter: func(msgType coderws.MessageType, payload []byte) ([]byte, *OpenAIFastBlockedError, error) {
			if msgType != coderws.MessageText {
				return payload, nil, nil
			}
			guarded, runtimeBlocked, runtimeErr := applyOpenAIReasoningEffortGuardToWSResponseCreatePayloadWithModel(account, payload, "gpt-5.5")
			if runtimeErr != nil {
				return payload, nil, runtimeErr
			}
			if runtimeBlocked != nil {
				return payload, nil, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, openAIRuntimeGuardBlockedWSReason(runtimeBlocked), runtimeBlocked)
			}
			return guarded, nil, nil
		},
	}

	_, payload, err := wrapper.ReadFrame(context.Background())

	require.Error(t, err)
	require.Equal(t, string(followupFrame), string(payload), "FrameConn returns the read payload with the close error; relay must stop on err")
	var closeErr *OpenAIWSClientCloseError
	require.True(t, errors.As(err, &closeErr))
	require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())
	var blocked *OpenAIRuntimeGuardBlockedError
	require.True(t, errors.As(err, &blocked))
	require.Equal(t, "context.obviously_over_limit", blocked.Decision.Category)
	require.Empty(t, inner.writes)
}
