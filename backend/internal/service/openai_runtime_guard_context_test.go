package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

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
