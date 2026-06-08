package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexGatewaySemanticTraceExtractsFoundationFromGoldenStream(t *testing.T) {
	stream := renderCodexGatewayGoldenStreamFixture(t, "namespace_tool_call_stream")

	trace, err := BuildCodexGatewaySemanticTraceFromSSE(stream)
	require.NoError(t, err)
	require.NotEmpty(t, trace.Events)
	require.Equal(t, "response.completed", trace.TerminalEvent)
	require.Equal(t, "completed", trace.TerminalStatus)
	require.True(t, trace.Usage.Present)
	require.True(t, trace.Usage.HasInputTokens)
	require.True(t, trace.Usage.HasOutputTokens)
	require.True(t, trace.Usage.HasTotalTokens)
	require.True(t, trace.Usage.HasCachedInputTokens)

	var sawToolAdded bool
	for _, event := range trace.Events {
		if event.EventType != "response.output_item.added" {
			continue
		}
		if event.ItemType != CodexGatewayOutputItemTypeFunctionCall {
			continue
		}
		sawToolAdded = true
		require.NotNil(t, event.OutputIndex)
		require.GreaterOrEqual(t, *event.OutputIndex, 0)
		require.True(t, event.HasItemID)
		require.True(t, event.HasCallID)
		require.Equal(t, "in_progress", event.ItemStatus)
		require.Equal(t, "open-page", event.ItemName)
	}
	require.True(t, sawToolAdded)
}

func TestCodexGatewayParityReportFoundationWithoutBaseline(t *testing.T) {
	stream := renderCodexGatewayGoldenStreamFixture(t, "custom_tool_call_stream")

	trace, err := BuildCodexGatewaySemanticTraceFromSSE(stream)
	require.NoError(t, err)

	report := BuildCodexGatewaySemanticParityReport(trace, nil)
	require.True(t, report.FoundationPass)
	require.False(t, report.Pass)
	require.False(t, report.Fail)
	require.False(t, report.BaselineCompared)
	require.Equal(t, "baseline_missing", report.DegradedReason)
	require.NotEmpty(t, report.Invariants)
	require.True(t, report.InvariantPassed("output_item_id_presence"))
	require.True(t, report.InvariantPassed("raw_reasoning_not_visible"))
	require.Equal(t, trace.TerminalEvent, report.Candidate.TerminalEvent)
	require.Equal(t, trace.TerminalStatus, report.Candidate.TerminalStatus)
}

func TestCodexGatewayParityReportPassesAgainstEquivalentBaseline(t *testing.T) {
	stream := renderCodexGatewayGoldenStreamFixture(t, "wait_agent_normalized_stream")

	trace, err := BuildCodexGatewaySemanticTraceFromSSE(stream)
	require.NoError(t, err)

	baseline := trace
	report := BuildCodexGatewaySemanticParityReport(trace, &baseline)
	require.True(t, report.FoundationPass)
	require.True(t, report.Pass)
	require.False(t, report.Fail)
	require.True(t, report.BaselineCompared)
	require.Empty(t, report.Mismatches)
}

func TestCodexGatewaySemanticTraceCapturesMessagePhase(t *testing.T) {
	stream := "" +
		"event: response.output_item.added\n" +
		"data: {\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"in_progress\",\"phase\":\"commentary\"},\"sequence_number\":0}\n\n" +
		"event: response.completed\n" +
		"data: {\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"output\":[]},\"sequence_number\":1}\n\n"

	trace, err := BuildCodexGatewaySemanticTraceFromSSE(stream)
	require.NoError(t, err)
	require.Len(t, trace.Events, 2)
	require.Equal(t, "commentary", trace.Events[0].Phase)

	summary := codexGatewaySemanticTraceSummary(trace)
	require.Equal(t, []string{"commentary"}, summary.MessagePhases)
}

func TestCodexGatewayParityReportFailsMessagePhaseMismatch(t *testing.T) {
	baselineStream := "" +
		"event: response.output_item.added\n" +
		"data: {\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"in_progress\",\"phase\":\"commentary\"},\"sequence_number\":0}\n\n" +
		"event: response.output_item.done\n" +
		"data: {\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"completed\",\"phase\":\"commentary\"},\"sequence_number\":1}\n\n" +
		"event: response.completed\n" +
		"data: {\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"completed\",\"phase\":\"commentary\"}]},\"sequence_number\":2}\n\n"
	candidateStream := strings.ReplaceAll(baselineStream, "\"phase\":\"commentary\"", "\"phase\":\"final_answer\"")

	baseline, err := BuildCodexGatewaySemanticTraceFromSSE(baselineStream)
	require.NoError(t, err)
	candidate, err := BuildCodexGatewaySemanticTraceFromSSE(candidateStream)
	require.NoError(t, err)

	report := BuildCodexGatewaySemanticParityReport(candidate, &baseline)
	require.True(t, report.Fail)
	require.Contains(t, report.Mismatches, "message_phases")
}

func TestCodexGatewayParityReportFailsBrokenToolAndTerminalInvariants(t *testing.T) {
	stream := "" +
		"event: response.created\n" +
		"data: {\"response\":{\"id\":\"resp_bad\",\"object\":\"response\",\"status\":\"in_progress\"},\"sequence_number\":0}\n\n" +
		"event: response.output_item.added\n" +
		"data: {\"response_id\":\"resp_bad\",\"output_index\":0,\"item\":{\"id\":\"fc_missing_call\",\"type\":\"function_call\",\"status\":\"in_progress\",\"name\":\"get_weather\",\"arguments\":\"\"},\"sequence_number\":1}\n\n" +
		"event: response.function_call_arguments.delta\n" +
		"data: {\"response_id\":\"resp_bad\",\"output_index\":0,\"item_id\":\"fc_missing_call\",\"delta\":\"{\\\"city\\\":\",\"sequence_number\":2}\n\n" +
		"event: response.function_call_arguments.done\n" +
		"data: {\"response_id\":\"resp_bad\",\"output_index\":0,\"item\":{\"id\":\"fc_missing_call\",\"type\":\"function_call\",\"status\":\"completed\",\"call_id\":\"\",\"name\":\"get_weather\",\"arguments\":\"{\\\"zip\\\":94107}\"},\"sequence_number\":3}\n\n" +
		"event: response.completed\n" +
		"data: {\"response\":{\"id\":\"resp_bad\",\"object\":\"response\",\"status\":\"completed\",\"output\":[{\"id\":\"fc_missing_call\",\"type\":\"function_call\",\"status\":\"completed\",\"call_id\":\"\",\"name\":\"get_weather\",\"arguments\":\"{\\\"zip\\\":94107}\"}]},\"sequence_number\":4}\n\n" +
		"event: response.failed\n" +
		"data: {\"response\":{\"id\":\"resp_bad\",\"object\":\"response\",\"status\":\"failed\",\"output\":[]},\"sequence_number\":5}\n\n"

	trace, err := BuildCodexGatewaySemanticTraceFromSSE(stream)
	require.NoError(t, err)

	report := BuildCodexGatewaySemanticParityReport(trace, nil)
	require.False(t, report.Pass)
	require.True(t, report.Fail)
	require.Equal(t, "candidate_invariants_failed", report.DegradedReason)
	require.False(t, report.InvariantPassed("single_terminal_event"))
	require.True(t, report.InvariantPassed("output_item_id_presence"))
	require.False(t, report.InvariantPassed("tool_call_id_presence"))
	require.False(t, report.InvariantPassed("tool_delta_reconstructable"))
}

func TestCodexGatewayParityReportFailsWhenRequiredOutputIndexIsMissing(t *testing.T) {
	stream := "" +
		"event: response.created\n" +
		"data: {\"response\":{\"id\":\"resp_missing_index\",\"object\":\"response\",\"status\":\"in_progress\"},\"sequence_number\":0}\n\n" +
		"event: response.output_item.added\n" +
		"data: {\"response_id\":\"resp_missing_index\",\"item\":{\"id\":\"msg_missing_index\",\"type\":\"message\",\"status\":\"in_progress\"},\"sequence_number\":1}\n\n" +
		"event: response.output_item.done\n" +
		"data: {\"response_id\":\"resp_missing_index\",\"item\":{\"id\":\"msg_missing_index\",\"type\":\"message\",\"status\":\"completed\"},\"sequence_number\":2}\n\n" +
		"event: response.completed\n" +
		"data: {\"response\":{\"id\":\"resp_missing_index\",\"object\":\"response\",\"status\":\"completed\",\"output\":[]},\"sequence_number\":3}\n\n"

	trace, err := BuildCodexGatewaySemanticTraceFromSSE(stream)
	require.NoError(t, err)

	report := BuildCodexGatewaySemanticParityReport(trace, nil)
	require.False(t, report.FoundationPass)
	require.False(t, report.InvariantPassed("stable_output_index"))
}

func TestCodexGatewayParityReportFailsWhenToolDeltaHasNoDone(t *testing.T) {
	stream := "" +
		"event: response.created\n" +
		"data: {\"response\":{\"id\":\"resp_missing_done\",\"object\":\"response\",\"status\":\"in_progress\"},\"sequence_number\":0}\n\n" +
		"event: response.output_item.added\n" +
		"data: {\"response_id\":\"resp_missing_done\",\"output_index\":0,\"item\":{\"id\":\"fc_missing_done\",\"type\":\"function_call\",\"status\":\"in_progress\",\"call_id\":\"call_missing_done\",\"name\":\"wait_agent\"},\"sequence_number\":1}\n\n" +
		"event: response.function_call_arguments.delta\n" +
		"data: {\"response_id\":\"resp_missing_done\",\"output_index\":0,\"item_id\":\"fc_missing_done\",\"delta\":\"{\\\"targets\\\":[\\\"agent-1\\\"]}\",\"sequence_number\":2}\n\n" +
		"event: response.completed\n" +
		"data: {\"response\":{\"id\":\"resp_missing_done\",\"object\":\"response\",\"status\":\"completed\",\"output\":[]},\"sequence_number\":3}\n\n"

	trace, err := BuildCodexGatewaySemanticTraceFromSSE(stream)
	require.NoError(t, err)

	report := BuildCodexGatewaySemanticParityReport(trace, nil)
	require.False(t, report.FoundationPass)
	require.False(t, report.InvariantPassed("tool_delta_reconstructable"))
}

func renderCodexGatewayGoldenStreamFixture(t *testing.T, fixtureName string) string {
	t.Helper()

	fixture := loadCodexGatewayGoldenFixture(t, fixtureName)
	require.NotNil(t, fixture.Upstream)

	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	stateStore := newCodexGatewayGoldenStateStore()
	if fixture.SeedState != nil {
		insertCodexGatewayGoldenState(t, stateStore, *fixture.SeedState)
	}
	var buf bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		for key, value := range fixture.Upstream.Headers {
			w.Header().Set(key, value)
		}
		w.WriteHeader(fixture.Upstream.StatusCode)
		_, err := io.WriteString(w, fixture.Upstream.Body)
		require.NoError(t, err)
	}))
	defer server.Close()

	_, err := ExecuteCodexGatewayDeepSeekStream(
		context.Background(),
		server.Client(),
		server.URL,
		"test-key",
		model,
		fixture.Request.toCreateRequest(),
		stateStore,
		CodexGatewayDeepSeekRequestContext{SessionKey: "trace-session", IsolationKey: "trace-isolation"},
		CodexGatewayDeepSeekRequestConfig{},
		&buf,
	)
	require.NoError(t, err)
	return buf.String()
}
