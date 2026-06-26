package service

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

var claudeCodeSessionMapperUUIDLikeRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func useClaudeCodeSessionBoundaryLedgerFileForTest(t *testing.T) string {
	t.Helper()
	resetClaudeCodeSessionBoundaryLedgerForTest()
	path := filepath.Join(t.TempDir(), "formal-pool-session-ledger.json")
	t.Setenv("SUB2API_CLAUDE_CODE_SESSION_BOUNDARY_LEDGER_FILE", path)
	return path
}

func applyCP2FormalPoolProfileTupleForTest(input *ClaudeCodeSessionMapInput) {
	input.EgressProfileRef = "strip_attribution"
	input.ProfilePolicyVersion = "claude_code_2_1_179_cp1_degraded_v1"
	input.BillingShapePolicy = "strip"
	input.RequestShapeProfileRef = "claude_code_2_1_179_messages_streaming_tooldefs_degraded_v1"
	input.CacheParityProfileRef = "claude_code_2_1_179_cache_parity_degraded_v1"
}

func TestClaudeCodeSessionMapperReturnsUUIDLikeOpaqueSessionAndSafeRef(t *testing.T) {
	t.Setenv("SUB2API_SESSION_BUDGET_HMAC_KEY", "sub2api-session-budget-test-key")

	mapper := NewClaudeCodeSessionMapperFromEnv()
	mapping, err := mapper.Map(ClaudeCodeSessionMapInput{
		UserScope:    "user:42",
		AccountRef:   "account-42",
		DeviceID:     "device-a",
		AccountUUID:  "acct-uuid-a",
		RawSessionID: "11111111-2222-4333-8444-555555555555",
	})
	require.NoError(t, err)
	require.NotNil(t, mapping)
	require.Regexp(t, claudeCodeSessionMapperUUIDLikeRe, mapping.SessionID)
	require.NotEqual(t, "11111111-2222-4333-8444-555555555555", mapping.SessionID)
	require.NotNil(t, mapping.SessionRef)
	require.Equal(t, "session_budget_session", mapping.SessionRef.Scope)
	require.True(t, regexp.MustCompile(`^hmac-sha256:[0-9a-f]{64}$`).MatchString(mapping.SessionRef.Value))

	dumped, err := json.Marshal(mapping)
	require.NoError(t, err)
	require.NotContains(t, string(dumped), "11111111-2222-4333-8444-555555555555")
}

func TestClaudeCodeSessionMapperScopesSessionsByUserAndRawSession(t *testing.T) {
	t.Setenv("SUB2API_SESSION_BUDGET_HMAC_KEY", "sub2api-session-budget-test-key")

	mapper := NewClaudeCodeSessionMapperFromEnv()
	base := ClaudeCodeSessionMapInput{
		UserScope:    "user:alpha",
		AccountRef:   "account-42",
		DeviceID:     "device-a",
		AccountUUID:  "acct-uuid-a",
		RawSessionID: "11111111-2222-4333-8444-555555555555",
	}

	first, err := mapper.Map(base)
	require.NoError(t, err)

	second, err := mapper.Map(base)
	require.NoError(t, err)
	require.Equal(t, first.SessionID, second.SessionID)
	require.Equal(t, first.SessionRef.Value, second.SessionRef.Value)

	otherUser, err := mapper.Map(ClaudeCodeSessionMapInput{
		UserScope:    "user:beta",
		AccountRef:   base.AccountRef,
		DeviceID:     base.DeviceID,
		AccountUUID:  base.AccountUUID,
		RawSessionID: base.RawSessionID,
	})
	require.NoError(t, err)
	require.NotEqual(t, first.SessionID, otherUser.SessionID)

	otherSession, err := mapper.Map(ClaudeCodeSessionMapInput{
		UserScope:    base.UserScope,
		AccountRef:   base.AccountRef,
		DeviceID:     base.DeviceID,
		AccountUUID:  base.AccountUUID,
		RawSessionID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
	})
	require.NoError(t, err)
	require.NotEqual(t, first.SessionID, otherSession.SessionID)
}

func TestClaudeCodeSessionUserScopeRoundTrip(t *testing.T) {
	ctx := WithClaudeCodeSessionUserScope(context.Background(), "user:99")

	scope, ok := ClaudeCodeSessionUserScopeFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "user:99", scope)
}

func TestClaudeCodeSessionMapperRejectsBoundarySwapWithSafeAudit(t *testing.T) {
	t.Setenv("SUB2API_SESSION_BUDGET_HMAC_KEY", "sub2api-session-budget-test-key")
	resetClaudeCodeSessionBoundaryLedgerForTest()

	mapper := NewClaudeCodeSessionMapperFromEnv()
	base := ClaudeCodeSessionMapInput{
		UserScope:        "user:cp39",
		BoundaryScope:    "user:cp39",
		EnforceBoundary:  true,
		AccountRef:       "opaque:acct:formal-a",
		CredentialRef:    "opaque:credential-ref:v1:cred-a",
		DeviceID:         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AccountUUID:      "opaque:acct:formal-a",
		EgressBucket:     "bucket-a",
		ProxyIdentityRef: "opaque:proxy-ref:v1:bucket-a",
		PolicyVersion:    "2.1.175",
		PersonaProfile:   "claude_code_2_1_175_subscription_1m",
		ProviderFamily:   "anthropic_formal_pool",
		RawSessionID:     "11111111-2222-4333-8444-555555555555",
	}
	applyCP2FormalPoolProfileTupleForTest(&base)
	first, err := mapper.Map(base)
	require.NoError(t, err)
	require.NotEmpty(t, first.SessionID)

	attempt := base
	attempt.AccountRef = "opaque:acct:formal-b"
	attempt.DeviceID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	attempt.AccountUUID = "opaque:acct:formal-b"
	attempt.EgressBucket = "bucket-b"
	attempt.ProxyIdentityRef = "opaque:proxy-ref:v1:bucket-b"
	attempt.ProviderFamily = "openai_bridge"
	_, err = mapper.Map(attempt)
	require.Error(t, err)
	var boundaryErr *ClaudeCodeSessionBoundaryError
	require.ErrorAs(t, err, &boundaryErr)
	require.Equal(t, "claude_native_session_boundary_failed", boundaryErr.Code)
	require.Equal(t, "opaque:acct:formal-a", boundaryErr.PreviousAccountRef)
	require.Equal(t, "opaque:acct:formal-b", boundaryErr.AttemptedAccountRef)
	require.Equal(t, "bucket-a", boundaryErr.PreviousEgress)
	require.Equal(t, "bucket-b", boundaryErr.AttemptedEgress)
	require.Equal(t, "anthropic_formal_pool", boundaryErr.PreviousProviderFamily)
	require.Equal(t, "openai_bridge", boundaryErr.AttemptedProviderFamily)

	dumped, marshalErr := json.Marshal(boundaryErr)
	require.NoError(t, marshalErr)
	require.NotContains(t, string(dumped), base.RawSessionID)
	require.NotContains(t, string(dumped), "raw prompt")
	require.NotContains(t, string(dumped), "raw body")
}

func TestClaudeCodeSessionMapperRejectsFormalPoolAuthorityFieldSwitches(t *testing.T) {
	t.Setenv("SUB2API_SESSION_BUDGET_HMAC_KEY", "sub2api-session-budget-test-key")
	resetClaudeCodeSessionBoundaryLedgerForTest()

	mapper := NewClaudeCodeSessionMapperFromEnv()
	base := ClaudeCodeSessionMapInput{
		UserScope:              "user:cp39-authority",
		BoundaryScope:          "user:cp39-authority",
		EnforceBoundary:        true,
		AccountRef:             "opaque:acct:formal-a",
		CredentialRef:          "opaque:credential-ref:v1:cred-a",
		DeviceID:               "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AccountUUID:            "opaque:acct:formal-a",
		EgressBucket:           "bucket-a",
		ProxyIdentityRef:       "opaque:proxy-ref:v1:bucket-a",
		PolicyVersion:          "2.1.175",
		PersonaProfile:         "claude_code_2_1_175_subscription_1m",
		EgressProfileRef:       "strip_attribution",
		ProfilePolicyVersion:   "claude_code_2_1_179_cp1_degraded_v1",
		BillingShapePolicy:     "strip",
		RequestShapeProfileRef: "claude_code_2_1_179_messages_streaming_tooldefs_degraded_v1",
		CacheParityProfileRef:  "claude_code_2_1_179_cache_parity_degraded_v1",
		ProviderFamily:         "anthropic_formal_pool",
		RawSessionID:           "11111111-2222-4333-8444-555555555555",
	}
	applyCP2FormalPoolProfileTupleForTest(&base)
	_, err := mapper.Map(base)
	require.NoError(t, err)

	for name, mutate := range map[string]func(*ClaudeCodeSessionMapInput){
		"credential": func(in *ClaudeCodeSessionMapInput) { in.CredentialRef = "opaque:credential-ref:v1:cred-b" },
		"policy":     func(in *ClaudeCodeSessionMapInput) { in.PolicyVersion = "2.1.176" },
		"persona":    func(in *ClaudeCodeSessionMapInput) { in.PersonaProfile = "claude_code_2_1_175_api_key_non_1m" },
		"egress_profile": func(in *ClaudeCodeSessionMapInput) {
			in.EgressProfileRef = "claude_code_2_1_179_first_party_signed_cch"
		},
		"profile_policy":        func(in *ClaudeCodeSessionMapInput) { in.ProfilePolicyVersion = "client_policy" },
		"billing_shape_policy":  func(in *ClaudeCodeSessionMapInput) { in.BillingShapePolicy = "signed_cch" },
		"request_shape_profile": func(in *ClaudeCodeSessionMapInput) { in.RequestShapeProfileRef = "client_shape" },
		"cache_parity_profile":  func(in *ClaudeCodeSessionMapInput) { in.CacheParityProfileRef = "client_cache" },
	} {
		t.Run(name, func(t *testing.T) {
			attempt := base
			mutate(&attempt)
			_, err := mapper.Map(attempt)
			require.Error(t, err)
			var boundaryErr *ClaudeCodeSessionBoundaryError
			require.ErrorAs(t, err, &boundaryErr)
			require.Equal(t, "claude_native_session_boundary_failed", boundaryErr.Code)
			dumped, marshalErr := json.Marshal(boundaryErr)
			require.NoError(t, marshalErr)
			require.NotContains(t, string(dumped), base.RawSessionID)
			require.NotContains(t, string(dumped), base.DeviceID)
		})
	}
}

func TestClaudeCodeSessionMapperFormalPoolProductionRequiresPersistentLedger(t *testing.T) {
	t.Setenv("SUB2API_SESSION_BUDGET_HMAC_KEY", "sub2api-session-budget-test-key")
	t.Setenv("SUB2API_CLAUDE_CODE_SESSION_BOUNDARY_LEDGER_FILE", "")
	resetClaudeCodeSessionBoundaryLedgerForTest()

	mapper := NewClaudeCodeSessionMapperFromEnv()
	input := ClaudeCodeSessionMapInput{
		UserScope:            "user:cp39-production",
		BoundaryScope:        "user:cp39-production",
		EnforceBoundary:      true,
		FormalPoolProduction: true,
		AccountRef:           "opaque:acct:formal-a",
		CredentialRef:        "opaque:credential-ref:v1:cred-a",
		DeviceID:             "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AccountUUID:          "opaque:acct:formal-a",
		EgressBucket:         "bucket-a",
		ProxyIdentityRef:     "opaque:proxy-ref:v1:bucket-a",
		PolicyVersion:        "2.1.175",
		PersonaProfile:       "claude_code_2_1_175_subscription_1m",
		ProviderFamily:       "anthropic_formal_pool",
		RawSessionID:         "11111111-2222-4333-8444-555555555555",
	}
	applyCP2FormalPoolProfileTupleForTest(&input)
	_, err := mapper.Map(input)
	require.Error(t, err)
	var boundaryErr *ClaudeCodeSessionBoundaryError
	require.ErrorAs(t, err, &boundaryErr)
	require.Equal(t, "claude_native_session_boundary_ledger_unavailable", boundaryErr.Code)
}

func TestClaudeCodeSessionMapperRejectsIncompleteFormalPoolBoundaryContext(t *testing.T) {
	t.Setenv("SUB2API_SESSION_BUDGET_HMAC_KEY", "sub2api-session-budget-test-key")
	resetClaudeCodeSessionBoundaryLedgerForTest()

	mapper := NewClaudeCodeSessionMapperFromEnv()
	_, err := mapper.Map(ClaudeCodeSessionMapInput{
		UserScope:       "user:cp39-incomplete",
		BoundaryScope:   "user:cp39-incomplete",
		EnforceBoundary: true,
		AccountRef:      "opaque:acct:formal-a",
		DeviceID:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AccountUUID:     "opaque:acct:formal-a",
		EgressBucket:    "bucket-a",
		ProviderFamily:  "anthropic_formal_pool",
		RawSessionID:    "11111111-2222-4333-8444-555555555555",
	})
	require.Error(t, err)
	var boundaryErr *ClaudeCodeSessionBoundaryError
	require.ErrorAs(t, err, &boundaryErr)
	require.Equal(t, "claude_native_session_boundary_incomplete", boundaryErr.Code)
}

func TestClaudeCodeSessionMapperPersistentLedgerWriteFailureDoesNotPoisonMemory(t *testing.T) {
	t.Setenv("SUB2API_SESSION_BUDGET_HMAC_KEY", "sub2api-session-budget-test-key")
	badLedgerPath := filepath.Join(t.TempDir(), "formal-pool-session-ledger.json")
	require.NoError(t, os.WriteFile(badLedgerPath, []byte(`{"version":1,"entries":{}}`), 0o600))
	require.NoError(t, os.Chmod(badLedgerPath, 0o400))
	t.Cleanup(func() { _ = os.Chmod(badLedgerPath, 0o600) })
	t.Setenv("SUB2API_CLAUDE_CODE_SESSION_BOUNDARY_LEDGER_FILE", badLedgerPath)
	resetClaudeCodeSessionBoundaryLedgerForTest()

	mapper := NewClaudeCodeSessionMapperFromEnv()
	base := ClaudeCodeSessionMapInput{
		UserScope:            "user:cp39-write-fail",
		BoundaryScope:        "user:cp39-write-fail",
		EnforceBoundary:      true,
		FormalPoolProduction: true,
		AccountRef:           "opaque:acct:formal-a",
		CredentialRef:        "opaque:credential-ref:v1:cred-a",
		DeviceID:             "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AccountUUID:          "opaque:acct:formal-a",
		EgressBucket:         "bucket-a",
		ProxyIdentityRef:     "opaque:proxy-ref:v1:bucket-a",
		PolicyVersion:        "2.1.175",
		PersonaProfile:       "claude_code_2_1_175_subscription_1m",
		ProviderFamily:       "anthropic_formal_pool",
		RawSessionID:         "11111111-2222-4333-8444-555555555555",
	}
	applyCP2FormalPoolProfileTupleForTest(&base)
	_, err := mapper.Map(base)
	require.Error(t, err)
	var firstBoundaryErr *ClaudeCodeSessionBoundaryError
	require.ErrorAs(t, err, &firstBoundaryErr)
	require.Equal(t, "claude_native_session_boundary_ledger_unavailable", firstBoundaryErr.Code)

	switched := base
	switched.AccountRef = "opaque:acct:formal-b"
	switched.CredentialRef = "opaque:credential-ref:v1:cred-b"
	switched.EgressBucket = "bucket-b"
	switched.ProxyIdentityRef = "opaque:proxy-ref:v1:bucket-b"
	switched.DeviceID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	switched.AccountUUID = "opaque:acct:formal-b"
	_, err = mapper.Map(switched)
	require.Error(t, err)
	var secondBoundaryErr *ClaudeCodeSessionBoundaryError
	require.ErrorAs(t, err, &secondBoundaryErr)
	require.Equal(t, "claude_native_session_boundary_ledger_unavailable", secondBoundaryErr.Code)
}

func TestClaudeCodeSessionMapperFormalPoolProductionLedgerPersistsSafeRefs(t *testing.T) {
	t.Setenv("SUB2API_SESSION_BUDGET_HMAC_KEY", "sub2api-session-budget-test-key")
	ledgerPath := filepath.Join(t.TempDir(), "formal-pool-session-ledger.json")
	t.Setenv("SUB2API_CLAUDE_CODE_SESSION_BOUNDARY_LEDGER_FILE", ledgerPath)
	resetClaudeCodeSessionBoundaryLedgerForTest()

	mapper := NewClaudeCodeSessionMapperFromEnv()
	base := ClaudeCodeSessionMapInput{
		UserScope:            "user:cp39-persistent",
		BoundaryScope:        "user:cp39-persistent",
		EnforceBoundary:      true,
		FormalPoolProduction: true,
		AccountRef:           "opaque:acct:formal-a",
		CredentialRef:        "opaque:credential-ref:v1:cred-a",
		DeviceID:             "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AccountUUID:          "opaque:acct:formal-a",
		EgressBucket:         "bucket-a",
		ProxyIdentityRef:     "opaque:proxy-ref:v1:bucket-a",
		PolicyVersion:        "2.1.175",
		PersonaProfile:       "claude_code_2_1_175_subscription_1m",
		ProviderFamily:       "anthropic_formal_pool",
		RawSessionID:         "11111111-2222-4333-8444-555555555555",
	}
	applyCP2FormalPoolProfileTupleForTest(&base)
	_, err := mapper.Map(base)
	require.NoError(t, err)

	resetClaudeCodeSessionBoundaryLedgerForTest()
	_, err = mapper.Map(base)
	require.NoError(t, err)

	attempt := base
	attempt.CredentialRef = "opaque:credential-ref:v1:cred-b"
	_, err = mapper.Map(attempt)
	require.Error(t, err)
	var boundaryErr *ClaudeCodeSessionBoundaryError
	require.ErrorAs(t, err, &boundaryErr)
	require.Equal(t, "claude_native_session_boundary_failed", boundaryErr.Code)

	raw, err := os.ReadFile(ledgerPath)
	require.NoError(t, err)
	text := string(raw)
	require.Contains(t, text, "hmac-sha256:")
	require.NotContains(t, text, base.RawSessionID)
	require.NotContains(t, text, base.DeviceID)
	require.NotContains(t, text, "selected-token")
	require.NotContains(t, text, "authorization")
	require.NotContains(t, text, "x-api-key")
}

func TestCCGatewayClaudeCodeSessionMappingUsesAccountOwnedDeviceAndSafeAccountRef(t *testing.T) {
	t.Setenv("SUB2API_SESSION_BUDGET_HMAC_KEY", "sub2api-session-budget-test-key")
	t.Setenv("SUB2API_CLAUDE_CODE_SESSION_BOUNDARY_LEDGER_FILE", filepath.Join(t.TempDir(), "formal-pool-session-ledger.json"))
	resetClaudeCodeSessionBoundaryLedgerForTest()

	account := newCCGatewayBoundaryAccount()
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	account.Extra[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal
	account.Extra["claude_code_device_id"] = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	account.Extra[ccGatewayExtraAccountRef] = "opaque:acct:server-selected"
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"metadata":{"user_id":"{\"device_id\":\"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff\",\"account_uuid\":\"123e4567-e89b-42d3-a456-426614174999\",\"session_id\":\"11111111-2222-4333-8444-555555555555\"}"},"messages":[{"role":"user","content":"hello"}]}`))

	err := applyCCGatewayClaudeCodeSessionMapping(req, account)
	require.NoError(t, err)
	body := claudeCodeReadRequestBody(req)
	parsed := ParseMetadataUserID(gjson.GetBytes(body, "metadata.user_id").String())
	require.NotNil(t, parsed)
	require.Equal(t, "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", parsed.DeviceID)
	require.Equal(t, "opaque:acct:server-selected", parsed.AccountUUID)
	require.NotEqual(t, "11111111-2222-4333-8444-555555555555", parsed.SessionID)
	require.Equal(t, parsed.SessionID, getHeaderRaw(req.Header, "X-Claude-Code-Session-Id"))
	require.NotContains(t, string(body), "123e4567-e89b-42d3-a456-426614174999")
}

func TestCCGatewayClaudeCodeSessionMappingRejectsFormalPoolWithoutAccountOwnedDevice(t *testing.T) {
	t.Setenv("SUB2API_SESSION_BUDGET_HMAC_KEY", "sub2api-session-budget-test-key")
	resetClaudeCodeSessionBoundaryLedgerForTest()

	account := newCCGatewayBoundaryAccount()
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	account.Extra[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal
	delete(account.Extra, "claude_code_device_id")
	delete(account.Extra, "device_id")
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"metadata":{"user_id":"{\"device_id\":\"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff\",\"session_id\":\"11111111-2222-4333-8444-555555555555\"}"},"messages":[{"role":"user","content":"hello"}]}`))

	err := applyCCGatewayClaudeCodeSessionMapping(req, account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "account-owned device identity")
}
