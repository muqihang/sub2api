package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestSafeForwardErrorEventClassifiesWithoutLeakingMessage(t *testing.T) {
	event := safeForwardErrorEvent(errors.New("formal-pool claude native admission requires account-owned device identity: raw prompt secret-token"), false, 0)

	if event["event"] != "sub2api_forward_error" {
		t.Fatalf("unexpected event: %#v", event["event"])
	}
	if event["safe_error_class"] != "formal_pool_missing_account_device_identity" {
		t.Fatalf("unexpected class: %#v", event["safe_error_class"])
	}
	if _, ok := event["error"]; ok {
		t.Fatalf("raw error must not be persisted: %#v", event)
	}
	if _, ok := event["message"]; ok {
		t.Fatalf("raw message must not be persisted: %#v", event)
	}
}

func TestSafeForwardErrorEventRecordsWrittenResponseStatusOnlyWhenWritten(t *testing.T) {
	written := safeForwardErrorEvent(errors.New("cc gateway control-plane error: missing_account_identity"), true, 403)
	if written["response_written"] != true || written["response_status"] != 403 {
		t.Fatalf("written response evidence missing: %#v", written)
	}
	if written["safe_error_class"] != "cc_gateway_contract" {
		t.Fatalf("unexpected class: %#v", written["safe_error_class"])
	}

	unwritten := safeForwardErrorEvent(errors.New("access_token not found in credentials"), false, 200)
	if unwritten["response_written"] != false {
		t.Fatalf("unwritten response evidence missing: %#v", unwritten)
	}
	if _, ok := unwritten["response_status"]; ok {
		t.Fatalf("unwritten response must not report gin default status: %#v", unwritten)
	}
	if unwritten["safe_error_class"] != "selected_credential_missing" {
		t.Fatalf("unexpected class: %#v", unwritten["safe_error_class"])
	}
}

func TestLocalHarnessAccountUsesFormalPoolProductionRefs(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{
		"ContextAttestationSecret",
		"FormalPoolExtraOnboardingStage",
		"FormalPoolStageProduction",
		"cc_gateway_credential_ref",
		"cc_gateway_credential_binding_hmac",
		"FormalPoolExtraCredentialGeneration",
		"cc_gateway_proxy_identity_ref",
		"cc_gateway_persona_profile",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("local Sub2API harness missing formal-pool production contract marker %s", required)
		}
	}
}

func TestSafeForwardErrorEventRecordsCCGatewayCodeOnly(t *testing.T) {
	event := safeForwardErrorEvent(errors.New("cc gateway control-plane error: formal_pool_context_mismatch raw-prompt secret-token"), false, 0)
	if event["safe_error_class"] != "cc_gateway_contract" {
		t.Fatalf("unexpected class: %#v", event)
	}
	if event["cc_gateway_error_code"] != "formal_pool_context_mismatch" {
		t.Fatalf("missing safe cc gateway code: %#v", event)
	}
	if _, ok := event["message"]; ok {
		t.Fatalf("raw message must not be persisted: %#v", event)
	}
}

func TestSafeForwardErrorEventClassifiesSessionBoundaryWithoutRawError(t *testing.T) {
	event := safeForwardErrorEvent(errors.New("claude_native_session_boundary_ledger_unavailable diagnostic-detail-must-not-leak"), false, 0)

	if event["safe_error_class"] != "formal_pool_session_boundary" {
		t.Fatalf("unexpected class: %#v", event)
	}
	if _, ok := event["message"]; ok {
		t.Fatalf("raw message must not be persisted: %#v", event)
	}
	if _, ok := event["error"]; ok {
		t.Fatalf("raw error must not be persisted: %#v", event)
	}
}

func TestLocalHarnessUsesProductionRouteNotCanaryOnlyContext(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if strings.Contains(text, "WithCCGatewayExplicitCanaryRequest") || strings.Contains(text, "WithCCGatewayExplicitCanaryLocalOnly") {
		t.Fatalf("local full-chain harness must exercise production formal-pool selection, not canary-only context")
	}
}

func TestLocalHarnessVerifiesNativeAttestationBeforeForward(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{
		"VerifyMessagesRequest",
		"WithClaudeCodeNativeAuditSummary",
		"ClaudeCodeVersion",
		"LocalSessionRef",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("local Sub2API harness must mirror production native attestation context setup: missing %s", required)
		}
	}
}

func TestLocalHarnessFormalPoolAccountHasOwnedDeviceID(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, "claude_code_device_id") {
		t.Fatalf("local formal-pool harness account must include account-owned Claude Code device identity")
	}
}

func TestLocalHarnessUses2179FormalPoolProfileDefaults(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{
		"cc_gateway_trusted_egress_profile_ref",
		"strip_attribution",
		"cc_gateway_billing_shape_policy",
		"strip",
		"claude_code_2_1_179_cp1_degraded_v1",
		"claude_code_2_1_179_messages_streaming_tooldefs_degraded_v1",
		"claude_code_2_1_179_cache_parity_degraded_v1",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("local full-chain harness must use CP4 2.1.179 strip/default profile marker %s", required)
		}
	}
	if strings.Contains(text, "\"billing_cch_mode\":                                 \"sign\"") {
		t.Fatalf("local full-chain harness must not default production path to signed CCH")
	}
}

func TestLocalHarnessUses2179NativePersona(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, "claude_code_2_1_179_native_degraded") {
		t.Fatalf("local full-chain harness must bind 2.1.179 policy to the 2.1.179 native persona")
	}
	if strings.Contains(text, `"cc_gateway_persona_profile":                       "claude_code_2_1_175_subscription_1m"`) {
		t.Fatalf("local full-chain harness must not bind 2.1.179 policy to 2.1.175 subscription persona")
	}
}
