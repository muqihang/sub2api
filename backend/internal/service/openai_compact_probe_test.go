package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tidwall/gjson"
)

func TestNormalizeAccountTestMode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: AccountTestModeDefault},
		{input: "default", want: AccountTestModeDefault},
		{input: " compact ", want: AccountTestModeCompact},
		{input: "COMPACT", want: AccountTestModeCompact},
		{input: "unknown", want: AccountTestModeDefault},
	}

	for _, tt := range tests {
		if got := normalizeAccountTestMode(tt.input); got != tt.want {
			t.Fatalf("normalizeAccountTestMode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsOpenAICompactProbeSuccessRequiresSemanticProofForBodySignal(t *testing.T) {
	tests := []struct {
		name string
		mode string
		body string
		want bool
	}{
		{name: "dedicated path", mode: OpenAICompactEndpointModePath, body: `{"id":"cmp_1"}`, want: true},
		{name: "body signal object", mode: OpenAICompactEndpointModeBodySignal, body: `{"object":"response.compaction"}`, want: true},
		{name: "body signal output", mode: OpenAICompactEndpointModeBodySignal, body: `{"output":[{"type":"compaction_summary"}]}`, want: true},
		{name: "ordinary response", mode: OpenAICompactEndpointModeBodySignal, body: `{"object":"response","output":[{"type":"message"}]}`, want: false},
		{name: "invalid json", mode: OpenAICompactEndpointModeBodySignal, body: `not-json`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isOpenAICompactProbeSuccess(tt.mode, http.StatusOK, []byte(tt.body)); got != tt.want {
				t.Fatalf("isOpenAICompactProbeSuccess() = %v, want %v", got, tt.want)
			}
		})
	}
	if isOpenAICompactProbeSuccess(OpenAICompactEndpointModeBodySignal, http.StatusBadGateway, []byte(`{"object":"response.compaction"}`)) {
		t.Fatal("non-200 response must not pass compact semantic validation")
	}
}

func TestCreateOpenAICompactBodySignalProbePayloadExercisesCodexCompatibility(t *testing.T) {
	payload, err := json.Marshal(createOpenAICompactBodySignalProbePayload("gpt-5.4"))
	if err != nil {
		t.Fatal(err)
	}

	if got := gjson.GetBytes(payload, "reasoning.effort").String(); got != "max" {
		t.Fatalf("reasoning.effort = %q, want max", got)
	}
	if got := gjson.GetBytes(payload, "input.0.id").String(); !strings.HasPrefix(got, "item_") {
		t.Fatalf("input.0.id = %q, want Codex item_ prefix", got)
	}
	if got := gjson.GetBytes(payload, "input.1.type").String(); got != "compaction_trigger" {
		t.Fatalf("input.1.type = %q, want compaction_trigger", got)
	}
}

func TestBuildOpenAICompactProbeExtraUpdates_SuccessMarksSupported(t *testing.T) {
	now := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
	updates := buildOpenAICompactProbeExtraUpdates(&http.Response{StatusCode: http.StatusOK}, []byte(`{"id":"cmp_1"}`), nil, now)

	if got := updates["openai_compact_supported"]; got != true {
		t.Fatalf("openai_compact_supported = %v, want true", got)
	}
	if got := updates["openai_compact_last_status"]; got != http.StatusOK {
		t.Fatalf("openai_compact_last_status = %v, want %d", got, http.StatusOK)
	}
	if got := updates["openai_compact_last_error"]; got != "" {
		t.Fatalf("openai_compact_last_error = %v, want empty string", got)
	}
	if got := updates["openai_compact_checked_at"]; got != now.Format(time.RFC3339) {
		t.Fatalf("openai_compact_checked_at = %v, want %s", got, now.Format(time.RFC3339))
	}
}

func TestBuildOpenAICompactProbeExtraUpdates_404MarksUnsupported(t *testing.T) {
	now := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
	body := []byte(`404 page not found`)
	updates := buildOpenAICompactProbeExtraUpdates(&http.Response{StatusCode: http.StatusNotFound}, body, nil, now)

	if got := updates["openai_compact_supported"]; got != false {
		t.Fatalf("openai_compact_supported = %v, want false", got)
	}
	if got := updates["openai_compact_last_status"]; got != http.StatusNotFound {
		t.Fatalf("openai_compact_last_status = %v, want %d", got, http.StatusNotFound)
	}
}

func TestBuildOpenAICompactProbeExtraUpdates_ContractRejectionMarksUnsupported(t *testing.T) {
	tests := []string{
		`{"error":{"message":"Invalid 'input[12].id': 'item_abc'. Expected an ID that begins with 'msg'."}}`,
		`{"error":{"message":"level \"max\" not supported, valid levels: low, medium, high, xhigh"}}`,
	}
	for _, body := range tests {
		updates := buildOpenAICompactProbeExtraUpdates(
			&http.Response{StatusCode: http.StatusBadRequest},
			[]byte(body),
			nil,
			time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
		)
		if got := updates["openai_compact_supported"]; got != false {
			t.Fatalf("openai_compact_supported = %v for %s, want false", got, body)
		}
	}
}

func TestBuildOpenAICompactProbeExtraUpdates_TransientClientFailureRemainsUnknown(t *testing.T) {
	tests := []string{
		`{"error":{"code":"model_not_found","message":"unknown model gpt-5.4"}}`,
		`{"error":{"code":"insufficient_quota","message":"insufficient user quota"}}`,
		`{"error":{"message":"rate limit exceeded"}}`,
	}
	for _, body := range tests {
		updates := buildOpenAICompactProbeExtraUpdates(
			&http.Response{StatusCode: http.StatusBadRequest},
			[]byte(body),
			nil,
			time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
		)
		if _, exists := updates["openai_compact_supported"]; exists {
			t.Fatalf("transient failure must remain unknown: %s", body)
		}
	}
}

func TestBuildOpenAICompactProbeExtraUpdates_502DoesNotMarkUnsupported(t *testing.T) {
	now := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
	updates := buildOpenAICompactProbeExtraUpdates(&http.Response{StatusCode: http.StatusBadGateway}, []byte(`Upstream request failed`), nil, now)

	if _, exists := updates["openai_compact_supported"]; exists {
		t.Fatalf("did not expect openai_compact_supported for 502 response")
	}
	if got := updates["openai_compact_last_status"]; got != http.StatusBadGateway {
		t.Fatalf("openai_compact_last_status = %v, want %d", got, http.StatusBadGateway)
	}
}

func TestBuildOpenAICompactProbeExtraUpdates_RequestErrorDoesNotMarkUnsupported(t *testing.T) {
	now := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
	updates := buildOpenAICompactProbeExtraUpdates(nil, nil, errors.New("dial tcp timeout"), now)

	if _, exists := updates["openai_compact_supported"]; exists {
		t.Fatalf("did not expect openai_compact_supported for request error")
	}
	if got, exists := updates["openai_compact_last_status"]; !exists || got != nil {
		t.Fatalf("openai_compact_last_status = %v, want nil key", got)
	}
	if got := updates["openai_compact_last_error"]; got == "" {
		t.Fatalf("expected openai_compact_last_error to be populated")
	}
}

func TestBuildOpenAICompactProbeExtraUpdates_NoResponseClearsLastStatus(t *testing.T) {
	now := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
	updates := buildOpenAICompactProbeExtraUpdates(nil, nil, nil, now)

	if got, exists := updates["openai_compact_last_status"]; !exists || got != nil {
		t.Fatalf("openai_compact_last_status = %v, want nil key", got)
	}
	if got := updates["openai_compact_last_error"]; got != "compact probe failed" {
		t.Fatalf("openai_compact_last_error = %v, want compact probe failed", got)
	}
}

func TestBuildOpenAICompactProbeExtraUpdates_UnknownModelDoesNotMarkUnsupported(t *testing.T) {
	now := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
	body := []byte(`{"error":{"message":"unknown model gpt-5.4-openai-compact"}}`)
	updates := buildOpenAICompactProbeExtraUpdates(&http.Response{StatusCode: http.StatusBadRequest}, body, nil, now)

	if _, exists := updates["openai_compact_supported"]; exists {
		t.Fatalf("did not expect openai_compact_supported for unknown-model diagnostics")
	}
	if got := updates["openai_compact_last_status"]; got != http.StatusBadRequest {
		t.Fatalf("openai_compact_last_status = %v, want %d", got, http.StatusBadRequest)
	}
}

func TestBuildOpenAICompactProbeExtraUpdates_EmptyFailureBodyFallsBackToHTTPStatus(t *testing.T) {
	now := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
	updates := buildOpenAICompactProbeExtraUpdates(&http.Response{StatusCode: http.StatusServiceUnavailable}, nil, nil, now)

	if got := updates["openai_compact_last_status"]; got != http.StatusServiceUnavailable {
		t.Fatalf("openai_compact_last_status = %v, want %d", got, http.StatusServiceUnavailable)
	}
	if got := updates["openai_compact_last_error"]; got != "HTTP 503" {
		t.Fatalf("openai_compact_last_error = %v, want HTTP 503", got)
	}
}

func TestBuildOpenAICompactProbeExtraUpdatesForModel_MergesScopedResults(t *testing.T) {
	now := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	existing := map[string]any{
		"openai_compact_model_support": map[string]any{
			"gpt-5.5": map[string]any{"supported": false, "status": http.StatusNotFound},
		},
	}

	updates := buildOpenAICompactProbeExtraUpdatesForModel(existing, "gpt-5.6-sol", "gpt-5.4", &http.Response{StatusCode: http.StatusOK}, []byte(`{"id":"cmp"}`), nil, now)
	scoped, ok := updates["openai_compact_model_support"].(map[string]any)
	if !ok {
		t.Fatalf("openai_compact_model_support = %#v, want map", updates["openai_compact_model_support"])
	}
	if _, exists := scoped["gpt-5.5"]; !exists {
		t.Fatal("existing gpt-5.5 result was not preserved")
	}
	entry, ok := scoped["gpt-5.4"].(map[string]any)
	if !ok {
		t.Fatalf("gpt-5.4 entry = %#v, want map", scoped["gpt-5.4"])
	}
	if entry["supported"] != true || entry["requested_model"] != "gpt-5.6-sol" || entry["upstream_model"] != "gpt-5.4" {
		t.Fatalf("gpt-5.4 entry = %#v", entry)
	}
	if updates["openai_compact_last_requested_model"] != "gpt-5.6-sol" || updates["openai_compact_last_upstream_model"] != "gpt-5.4" {
		t.Fatalf("last model fields = (%v, %v)", updates["openai_compact_last_requested_model"], updates["openai_compact_last_upstream_model"])
	}
}

func TestBuildOpenAICompactProbeExtraUpdatesForModel_TransientFailureRemainsUnknown(t *testing.T) {
	now := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	updates := buildOpenAICompactProbeExtraUpdatesForModel(nil, "gpt-5.6-sol", "gpt-5.4", &http.Response{StatusCode: http.StatusBadGateway}, []byte(`Upstream request failed`), nil, now)
	scoped, ok := updates["openai_compact_model_support"].(map[string]any)
	if !ok {
		t.Fatalf("openai_compact_model_support = %#v, want map", updates["openai_compact_model_support"])
	}
	entry, ok := scoped["gpt-5.4"].(map[string]any)
	if !ok {
		t.Fatalf("gpt-5.4 entry = %#v, want map", scoped["gpt-5.4"])
	}
	if _, exists := entry["supported"]; exists {
		t.Fatalf("transient failure must not persist supported=false: %#v", entry)
	}
}
