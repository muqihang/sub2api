package service

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	testAWSWorkspaceID  = syntheticAWSWorkspaceID(1)
	testAWSWorkspaceID2 = syntheticAWSWorkspaceID(2)
	testAWSAPIKey       = syntheticAWSAPIKey()
)

func syntheticAWSWorkspaceID(seed int) string {
	letter := string(rune('A' + seed))
	return "wrkspc_" + strings.Repeat(letter, 12)
}

func syntheticAWSAPIKey() string {
	return "synthetic-cpaws-" + strings.Repeat("k", 16)
}

func TestClaudePlatformAWSAccountTypeIsIsolatedFromExistingAnthropicTypes(t *testing.T) {
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeClaudePlatformAWS}

	require.True(t, account.IsClaudePlatformAWS())
	require.False(t, account.IsAnthropicOAuthOrSetupToken(), "AWS Platform must not enter OAuth/setup-token path")
	require.False(t, account.IsBedrock(), "AWS Platform must not enter Bedrock path")
	require.False(t, account.IsAnthropicAPIKeyPassthroughEnabled(), "AWS Platform must not enter first-party API-key passthrough")
	require.False(t, account.Type == AccountTypeServiceAccount, "AWS Platform must not enter Vertex service_account path")
	require.False(t, account.Type == AccountTypeUpstream, "AWS Platform must not enter generic upstream path")
}

func TestValidateClaudePlatformAWSAccountRequiresProxyWorkspaceRegionAndDerivedEndpoint(t *testing.T) {
	proxyID := int64(42)
	account := &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeClaudePlatformAWS,
		ProxyID:  &proxyID,
		Credentials: map[string]any{
			"auth_mode":                 "apikey",
			"api_key":                   testAWSAPIKey,
			"aws_region":                "us-east-1",
			"anthropic_workspace_id":    testAWSWorkspaceID,
			"base_url":                  "https://aws-external-anthropic.us-east-1.api.aws",
			"cp0_auth_profile_status":   "blocked",
			"cp0_region_workspace_pass": false,
		},
		Extra: map[string]any{},
	}

	result, err := ValidateClaudePlatformAWSAccount(account)
	require.NoError(t, err)
	require.Equal(t, "us-east-1", result.Region)
	require.Equal(t, "https://aws-external-anthropic.us-east-1.api.aws", result.Endpoint)
	require.NotEmpty(t, result.WorkspaceRef)
	require.NotContains(t, result.WorkspaceRef, testAWSWorkspaceID)
	require.NotContains(t, result.EndpointRef, "aws-external-anthropic.us-east-1.api.aws")
	require.Equal(t, ClaudePlatformAWSAuthProfileBlocked, result.AuthProfileStatus)

	missingProxy := *account
	missingProxy.ProxyID = nil
	_, err = ValidateClaudePlatformAWSAccount(&missingProxy)
	require.ErrorContains(t, err, "proxy_id")

	invalidWorkspace := *account
	invalidWorkspace.Credentials = cloneCredentials(account.Credentials)
	invalidWorkspace.Credentials["anthropic_workspace_id"] = "not-a-workspace"
	_, err = ValidateClaudePlatformAWSAccount(&invalidWorkspace)
	require.ErrorContains(t, err, "workspace")
	require.NotContains(t, err.Error(), "not-a-workspace")

	mismatch := *account
	mismatch.Credentials = cloneCredentials(account.Credentials)
	mismatch.Credentials["base_url"] = "https://aws-external-anthropic.eu-west-1.api.aws"
	_, err = ValidateClaudePlatformAWSAccount(&mismatch)
	require.ErrorContains(t, err, "region")
	require.NotContains(t, err.Error(), testAWSWorkspaceID)
}

func TestClaudePlatformAWSAuthProfilesAreMutuallyExclusiveAndEvidenceGated(t *testing.T) {
	profile, err := ResolveClaudePlatformAWSAuthProfile(ClaudePlatformAWSAuthEvidence{
		XAPIKeyProven:    true,
		BearerAPIProven:  false,
		SelectedProfile:  ClaudePlatformAWSAuthProfileXAPIKey,
		Endpoint:         "https://aws-external-anthropic.us-east-1.api.aws",
		Region:           "us-east-1",
		WorkspaceRef:     "workspace:abc",
		RequestShapePath: "/v1/messages",
	})
	require.NoError(t, err)
	require.Equal(t, ClaudePlatformAWSAuthProfileXAPIKey, profile)

	profile, err = ResolveClaudePlatformAWSAuthProfile(ClaudePlatformAWSAuthEvidence{
		XAPIKeyProven:    false,
		BearerAPIProven:  true,
		SelectedProfile:  ClaudePlatformAWSAuthProfileBearerAPIKey,
		Endpoint:         "https://aws-external-anthropic.us-east-1.api.aws",
		Region:           "us-east-1",
		WorkspaceRef:     "workspace:abc",
		RequestShapePath: "/v1/messages",
	})
	require.NoError(t, err)
	require.Equal(t, ClaudePlatformAWSAuthProfileBearerAPIKey, profile)

	_, err = ResolveClaudePlatformAWSAuthProfile(ClaudePlatformAWSAuthEvidence{SelectedProfile: ClaudePlatformAWSAuthProfileXAPIKey})
	require.ErrorContains(t, err, "BLOCKED_AUTH_PROFILE")

	_, err = ResolveClaudePlatformAWSAuthProfile(ClaudePlatformAWSAuthEvidence{
		XAPIKeyProven:    true,
		BearerAPIProven:  true,
		SelectedProfile:  ClaudePlatformAWSAuthProfileXAPIKey,
		Endpoint:         "https://aws-external-anthropic.us-east-1.api.aws",
		Region:           "us-east-1",
		WorkspaceRef:     "workspace:abc",
		RequestShapePath: "/v1/messages",
	})
	require.ErrorContains(t, err, "explicit operator choice")

	_, err = ResolveClaudePlatformAWSAuthProfile(ClaudePlatformAWSAuthEvidence{
		XAPIKeyProven:    true,
		SelectedProfile:  ClaudePlatformAWSAuthProfileBearerAPIKey,
		Endpoint:         "https://aws-external-anthropic.us-east-1.api.aws",
		Region:           "us-east-1",
		WorkspaceRef:     "workspace:abc",
		RequestShapePath: "/v1/messages",
	})
	require.ErrorContains(t, err, "silent fallback")
}

func TestClaudePlatformAWSBatchImportCreatesDistinctSafeWorkspaceAccountsAndDuplicatesAreSafe(t *testing.T) {
	proxyID := int64(7)
	input := ClaudePlatformAWSBatchImportInput{
		Rows: []ClaudePlatformAWSBatchImportRow{
			{Name: "aws-a", WorkspaceID: testAWSWorkspaceID, Region: "us-east-1", APIKey: testAWSAPIKey, ProxyID: proxyID},
			{Name: "aws-b", WorkspaceID: testAWSWorkspaceID2, Region: "us-east-1", APIKey: testAWSAPIKey, ProxyID: proxyID},
			{Name: "aws-a-duplicate", WorkspaceID: testAWSWorkspaceID, Region: "us-east-1", APIKey: testAWSAPIKey, ProxyID: proxyID},
		},
	}

	result, err := BuildClaudePlatformAWSBatchImport(input)
	require.NoError(t, err)
	require.Len(t, result.Rows, 3)
	require.Equal(t, ClaudePlatformAWSBatchRowCreate, result.Rows[0].Status)
	require.Equal(t, ClaudePlatformAWSBatchRowCreate, result.Rows[1].Status)
	require.Equal(t, ClaudePlatformAWSBatchRowDuplicate, result.Rows[2].Status)
	require.NotEqual(t, result.Rows[0].WorkspaceRef, result.Rows[1].WorkspaceRef)
	require.Equal(t, result.Rows[0].WorkspaceRef, result.Rows[2].WorkspaceRef)
	require.True(t, result.Rows[0].WorkspaceBindingHMACPresent)
	require.True(t, result.Rows[1].WorkspaceBindingHMACPresent)
	require.NotEqual(t, result.Rows[0].AccountRef, result.Rows[1].AccountRef)
	require.NotEmpty(t, result.Rows[0].CredentialRef)
	require.NotEmpty(t, result.Rows[1].CredentialRef)

	payload, err := json.Marshal(result)
	require.NoError(t, err)
	text := string(payload)
	require.NotContains(t, text, testAWSWorkspaceID)
	require.NotContains(t, text, testAWSWorkspaceID2)
	require.NotContains(t, text, testAWSAPIKey)
	require.NotContains(t, strings.ToLower(text), "x-api-key")
	require.NotContains(t, strings.ToLower(text), "authorization")
}

func TestClaudePlatformAWSFormalPoolEligibilityRequiresAllSafeBindingsAndCP0Evidence(t *testing.T) {
	proxyID := int64(7)
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeClaudePlatformAWS, Status: StatusActive, Schedulable: true, ProxyID: &proxyID, Extra: map[string]any{}}
	require.False(t, IsClaudePlatformAWSFormalPoolAccount(account))
	require.False(t, IsFormalPoolEligibleAccount(account))

	account.Extra = map[string]any{
		"cc_gateway_account_ref":                               "hmac-sha256:" + strings.Repeat("c", 64),
		"cc_gateway_credential_ref":                            "hmac-sha256:" + strings.Repeat("d", 64),
		"cc_gateway_credential_binding_hmac":                   "hmac-sha256:" + strings.Repeat("a", 64),
		"cc_gateway_egress_bucket":                             "egress:bucket-a",
		"cc_gateway_proxy_identity_ref":                        "hmac-sha256:" + strings.Repeat("e", 64),
		"cc_gateway_persona_profile":                           ccGatewayDefaultPersonaProfile,
		"claude_code_device_id":                                strings.Repeat("f", 64),
		ClaudePlatformAWSExtraWorkspaceRef:                     "workspace:ws-a",
		ClaudePlatformAWSExtraWorkspaceBindingHMAC:             "hmac-sha256:" + strings.Repeat("b", 64),
		ClaudePlatformAWSExtraEndpointRef:                      formalPoolSafeRef("endpoint", ClaudePlatformAWSEndpointForRegion("us-east-1")),
		ClaudePlatformAWSExtraRegion:                           "us-east-1",
		ClaudePlatformAWSExtraAuthScheme:                       ClaudePlatformAWSAuthProfileXAPIKey,
		ClaudePlatformAWSExtraRequestShapeProfileRef:           "request-shape:aws-v1",
		ClaudePlatformAWSExtraCacheParityProfileRef:            "cache-profile:aws-v1",
		ClaudePlatformAWSExtraBetaPolicyRef:                    "beta-policy:aws-v1",
		ClaudePlatformAWSExtraCP0AuthProfileEvidenceStatus:     "pass",
		ClaudePlatformAWSExtraCP0RegionWorkspaceEvidenceStatus: "pass",
		FormalPoolExtraRuntimeRegistered:                       "true",
	}
	require.False(t, IsClaudePlatformAWSFormalPoolAccount(account), "runtime registration flag without timestamp and enabled egress bucket must fail closed")
	account.Extra[FormalPoolExtraRuntimeRegisteredAt] = "2026-06-27T00:00:00Z"
	account.Extra[ccGatewayExtraEgressBucketEnabled] = "true"
	require.True(t, IsClaudePlatformAWSFormalPoolAccount(account))
	require.True(t, IsFormalPoolEligibleAccount(account))
	require.True(t, account.IsSchedulable())

	account.Extra[ClaudePlatformAWSExtraCP0AuthProfileEvidenceStatus] = "blocked"
	require.False(t, IsClaudePlatformAWSFormalPoolAccount(account))
	require.False(t, account.IsSchedulable())
}

func TestClaudePlatformAWSSanitizesClientSpoofedAuthorityHeaders(t *testing.T) {
	in := http.Header{}
	for _, key := range []string{
		"anthropic-workspace-id", "x-api-key", "authorization", "x-cc-account-id", "x-cc-credential-ref",
		"x-sub2api-persona-trusted", "x-sub2api-profile", "x-amz-security-token", "x-account-id", "x-credential-ref",
		"x-egress-bucket", "x-persona", "x-profile", "x-session", "x-billing", "x-cch-mode", "x-control-plane",
	} {
		in.Set(key, "client-forged")
	}
	in.Set("content-type", "application/json")
	in.Set("anthropic-version", "2023-06-01")

	out := SanitizeClaudePlatformAWSInboundHeaders(in)
	require.Equal(t, "application/json", out.Get("content-type"))
	require.Equal(t, "2023-06-01", out.Get("anthropic-version"))
	for key := range in {
		lower := strings.ToLower(key)
		if lower == "content-type" || lower == "anthropic-version" {
			continue
		}
		require.Empty(t, out.Get(key), key)
	}
}

func TestClaudePlatformAWSSessionTupleRejectsAuthoritySwitches(t *testing.T) {
	base := ClaudePlatformAWSSessionTuple{
		ProviderKind: "claude_platform_aws", AccountRef: "account:a", CredentialRef: "credential:a", WorkspaceRef: "workspace:a",
		WorkspaceBindingHMAC: "hmac-sha256:" + strings.Repeat("a", 64), EndpointRef: "endpoint:use1", Region: "us-east-1",
		AuthScheme: ClaudePlatformAWSAuthProfileXAPIKey, EgressBucket: "egress:a", ProxyIdentityRef: "proxy:a",
		PersonaProfile: "persona:v1", RequestShapeProfileRef: "request-shape:aws-v1", CacheParityProfileRef: "cache:aws-v1",
		BetaPolicyRef: "beta:aws-v1", DeviceRef: "device:a",
	}
	require.NoError(t, base.ValidateSame(base))

	for name, mutate := range map[string]func(*ClaudePlatformAWSSessionTuple){
		"workspace":  func(tu *ClaudePlatformAWSSessionTuple) { tu.WorkspaceRef = "workspace:b" },
		"credential": func(tu *ClaudePlatformAWSSessionTuple) { tu.CredentialRef = "credential:b" },
		"egress":     func(tu *ClaudePlatformAWSSessionTuple) { tu.EgressBucket = "egress:b" },
		"proxy":      func(tu *ClaudePlatformAWSSessionTuple) { tu.ProxyIdentityRef = "proxy:b" },
		"profile":    func(tu *ClaudePlatformAWSSessionTuple) { tu.RequestShapeProfileRef = "request-shape:b" },
		"beta":       func(tu *ClaudePlatformAWSSessionTuple) { tu.BetaPolicyRef = "beta:b" },
		"endpoint":   func(tu *ClaudePlatformAWSSessionTuple) { tu.EndpointRef = "endpoint:usw2" },
		"auth":       func(tu *ClaudePlatformAWSSessionTuple) { tu.AuthScheme = ClaudePlatformAWSAuthProfileBearerAPIKey },
	} {
		attempt := base
		mutate(&attempt)
		require.ErrorContains(t, base.ValidateSame(attempt), name)
	}
}

func TestClaudePlatformAWSProductionTrafficCannotBypassCCGateway(t *testing.T) {
	proxyID := int64(7)
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeClaudePlatformAWS, ProxyID: &proxyID, Extra: map[string]any{
		ClaudePlatformAWSExtraProductionAdmitted: true,
	}}
	require.ErrorContains(t, ValidateClaudePlatformAWSNoBypass(account, false), "CC Gateway")
	require.NoError(t, ValidateClaudePlatformAWSNoBypass(account, true))
}

func TestClaudePlatformAWSFinalVerifierRequiresServerSelectedAuthWorkspaceAndStripsInternalHeaders(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-api-key", syntheticAWSAPIKey())
	headers.Set("anthropic-workspace-id", syntheticAWSWorkspaceID(6))
	headers.Set("anthropic-version", "2023-06-01")
	headers.Set("content-type", "application/json")

	input := ClaudePlatformAWSFinalVerifierInput{
		FinalURL:            "https://aws-external-anthropic.us-east-1.api.aws/v1/messages",
		Headers:             headers,
		Region:              "us-east-1",
		AuthScheme:          ClaudePlatformAWSAuthProfileXAPIKey,
		WorkspaceFromServer: true,
		AuthFromServer:      true,
		AllowedPath:         "/v1/messages",
	}
	require.NoError(t, VerifyClaudePlatformAWSFinalRequest(input))

	forged := headers.Clone()
	forged.Set("authorization", "Bearer "+strings.Repeat("f", 16))
	err := VerifyClaudePlatformAWSFinalRequest(ClaudePlatformAWSFinalVerifierInput{FinalURL: input.FinalURL, Headers: forged, Region: input.Region, AuthScheme: input.AuthScheme, WorkspaceFromServer: true, AuthFromServer: true, AllowedPath: input.AllowedPath})
	require.ErrorContains(t, err, "exactly one auth")

	internal := headers.Clone()
	internal.Set("x-cc-account-id", "account:a")
	err = VerifyClaudePlatformAWSFinalRequest(ClaudePlatformAWSFinalVerifierInput{FinalURL: input.FinalURL, Headers: internal, Region: input.Region, AuthScheme: input.AuthScheme, WorkspaceFromServer: true, AuthFromServer: true, AllowedPath: input.AllowedPath})
	require.ErrorContains(t, err, "internal")

	legacyAuthority := headers.Clone()
	legacyAuthority.Set("x-account-id", "account:a")
	err = VerifyClaudePlatformAWSFinalRequest(ClaudePlatformAWSFinalVerifierInput{FinalURL: input.FinalURL, Headers: legacyAuthority, Region: input.Region, AuthScheme: input.AuthScheme, WorkspaceFromServer: true, AuthFromServer: true, AllowedPath: input.AllowedPath})
	require.ErrorContains(t, err, "internal")

	betaAuthority := headers.Clone()
	betaAuthority.Set("anthropic-beta", "client-forged")
	err = VerifyClaudePlatformAWSFinalRequest(ClaudePlatformAWSFinalVerifierInput{FinalURL: input.FinalURL, Headers: betaAuthority, Region: input.Region, AuthScheme: input.AuthScheme, WorkspaceFromServer: true, AuthFromServer: true, AllowedPath: input.AllowedPath})
	require.ErrorContains(t, err, "internal")

	wrongPath := headers.Clone()
	err = VerifyClaudePlatformAWSFinalRequest(ClaudePlatformAWSFinalVerifierInput{FinalURL: "https://aws-external-anthropic.us-east-1.api.aws/v1/files", Headers: wrongPath, Region: input.Region, AuthScheme: input.AuthScheme, WorkspaceFromServer: true, AuthFromServer: true, AllowedPath: input.AllowedPath})
	require.ErrorContains(t, err, "/v1/messages")
}

func TestClaudePlatformAWSBatchImportJSONUsesBooleanHMACPresenceOnly(t *testing.T) {
	result, err := BuildClaudePlatformAWSBatchImport(ClaudePlatformAWSBatchImportInput{Rows: []ClaudePlatformAWSBatchImportRow{{
		Name:        "aws-a",
		WorkspaceID: syntheticAWSWorkspaceID(1),
		Region:      "us-east-1",
		APIKey:      syntheticAWSAPIKey(),
		ProxyID:     7,
	}}})
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	require.True(t, result.Rows[0].WorkspaceBindingHMACPresent)

	payload, err := json.Marshal(result)
	require.NoError(t, err)
	text := string(payload)
	require.Contains(t, text, `"workspace_binding_hmac_present":true`)
	require.NotContains(t, text, `"workspace_binding_hmac"`)
	var decoded map[string][]map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))
	require.IsType(t, true, decoded["rows"][0]["workspace_binding_hmac_present"])
}

func TestClaudePlatformAWSFormalPoolEligibilityRejectsNonHMACBindingsAndUnknownAuthScheme(t *testing.T) {
	proxyID := int64(7)
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeClaudePlatformAWS, Status: StatusActive, Schedulable: true, ProxyID: &proxyID, Extra: map[string]any{
		"cc_gateway_account_ref":                               "hmac-sha256:" + strings.Repeat("c", 64),
		"cc_gateway_credential_ref":                            "hmac-sha256:" + strings.Repeat("d", 64),
		"cc_gateway_credential_binding_hmac":                   "hmac-sha256:" + strings.Repeat("a", 64),
		"cc_gateway_egress_bucket":                             "egress:bucket-a",
		"cc_gateway_proxy_identity_ref":                        "hmac-sha256:" + strings.Repeat("e", 64),
		"cc_gateway_persona_profile":                           ccGatewayDefaultPersonaProfile,
		"claude_code_device_id":                                strings.Repeat("f", 64),
		ClaudePlatformAWSExtraWorkspaceRef:                     "workspace:ws-a",
		ClaudePlatformAWSExtraWorkspaceBindingHMAC:             "hmac-sha256:" + strings.Repeat("b", 64),
		ClaudePlatformAWSExtraEndpointRef:                      formalPoolSafeRef("endpoint", ClaudePlatformAWSEndpointForRegion("us-east-1")),
		ClaudePlatformAWSExtraRegion:                           "us-east-1",
		ClaudePlatformAWSExtraAuthScheme:                       ClaudePlatformAWSAuthProfileXAPIKey,
		ClaudePlatformAWSExtraRequestShapeProfileRef:           "request-shape:aws-v1",
		ClaudePlatformAWSExtraCacheParityProfileRef:            "cache-profile:aws-v1",
		ClaudePlatformAWSExtraBetaPolicyRef:                    "beta-policy:aws-v1",
		ClaudePlatformAWSExtraCP0AuthProfileEvidenceStatus:     "pass",
		ClaudePlatformAWSExtraCP0RegionWorkspaceEvidenceStatus: "pass",
		FormalPoolExtraRuntimeRegistered:                       "true",
		FormalPoolExtraRuntimeRegisteredAt:                     "2026-06-27T00:00:00Z",
		ccGatewayExtraEgressBucketEnabled:                      "true",
	}}
	require.True(t, IsClaudePlatformAWSFormalPoolAccount(account))

	badWorkspaceBinding := *account
	badWorkspaceBinding.Extra = cloneCredentials(account.Extra)
	badWorkspaceBinding.Extra[ClaudePlatformAWSExtraWorkspaceBindingHMAC] = "workspace:not-a-binding-hmac"
	require.False(t, IsClaudePlatformAWSFormalPoolAccount(&badWorkspaceBinding))

	badCredentialBinding := *account
	badCredentialBinding.Extra = cloneCredentials(account.Extra)
	badCredentialBinding.Extra["cc_gateway_credential_binding_hmac"] = "credential:not-a-binding-hmac"
	require.False(t, IsClaudePlatformAWSFormalPoolAccount(&badCredentialBinding))

	badAuth := *account
	badAuth.Extra = cloneCredentials(account.Extra)
	badAuth.Extra[ClaudePlatformAWSExtraAuthScheme] = "sigv4"
	require.False(t, IsClaudePlatformAWSFormalPoolAccount(&badAuth))
}

func TestClaudePlatformAWSSanitizerStripsProviderPolicyAuthorityHeaders(t *testing.T) {
	in := http.Header{}
	for _, key := range []string{"anthropic-beta", "x-cache-policy", "x-request-shape-profile", "x-beta-policy-ref"} {
		in.Set(key, "client-forged")
	}
	out := SanitizeClaudePlatformAWSInboundHeaders(in)
	for key := range in {
		require.Empty(t, out.Get(key), key)
	}
}

func TestClaudePlatformAWSFinalVerifierChecksRequiredHeadersAndBearerShape(t *testing.T) {
	base := http.Header{}
	base.Set("authorization", "Bearer "+strings.Repeat("a", 16))
	base.Set("anthropic-workspace-id", syntheticAWSWorkspaceID(4))
	base.Set("anthropic-version", "2023-06-01")
	base.Set("content-type", "application/json")
	input := ClaudePlatformAWSFinalVerifierInput{
		FinalURL:            "https://aws-external-anthropic.us-east-1.api.aws/v1/messages",
		Headers:             base,
		Region:              "us-east-1",
		AuthScheme:          ClaudePlatformAWSAuthProfileBearerAPIKey,
		WorkspaceFromServer: true,
		AuthFromServer:      true,
		AllowedPath:         "/v1/messages",
	}
	require.NoError(t, VerifyClaudePlatformAWSFinalRequest(input))

	badBearer := base.Clone()
	badBearer.Set("authorization", strings.Repeat("a", 16))
	require.ErrorContains(t, VerifyClaudePlatformAWSFinalRequest(ClaudePlatformAWSFinalVerifierInput{FinalURL: input.FinalURL, Headers: badBearer, Region: input.Region, AuthScheme: input.AuthScheme, WorkspaceFromServer: true, AuthFromServer: true, AllowedPath: input.AllowedPath}), "Bearer")

	missingVersion := base.Clone()
	missingVersion.Del("anthropic-version")
	require.ErrorContains(t, VerifyClaudePlatformAWSFinalRequest(ClaudePlatformAWSFinalVerifierInput{FinalURL: input.FinalURL, Headers: missingVersion, Region: input.Region, AuthScheme: input.AuthScheme, WorkspaceFromServer: true, AuthFromServer: true, AllowedPath: input.AllowedPath}), "anthropic-version")

	duplicateWorkspace := base.Clone()
	duplicateWorkspace.Add("anthropic-workspace-id", syntheticAWSWorkspaceID(5))
	require.ErrorContains(t, VerifyClaudePlatformAWSFinalRequest(ClaudePlatformAWSFinalVerifierInput{FinalURL: input.FinalURL, Headers: duplicateWorkspace, Region: input.Region, AuthScheme: input.AuthScheme, WorkspaceFromServer: true, AuthFromServer: true, AllowedPath: input.AllowedPath}), "workspace")
}

func TestClaudePlatformAWSIdempotencyKeyUsesHeaderBeforeBody(t *testing.T) {
	require.Equal(t, "header-key", ClaudePlatformAWSIdempotencyKey(" header-key ", "body-key"))
	require.Equal(t, "body-key", ClaudePlatformAWSIdempotencyKey("", " body-key "))
	require.Empty(t, ClaudePlatformAWSIdempotencyKey("", ""))
}

func TestClaudePlatformAWSFormalPoolEligibilityBindsRegionToEndpointRef(t *testing.T) {
	proxyID := int64(7)
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeClaudePlatformAWS, Status: StatusActive, Schedulable: true, ProxyID: &proxyID, Extra: map[string]any{
		"cc_gateway_account_ref":                               "hmac-sha256:" + strings.Repeat("c", 64),
		"cc_gateway_credential_ref":                            "hmac-sha256:" + strings.Repeat("d", 64),
		"cc_gateway_credential_binding_hmac":                   "hmac-sha256:" + strings.Repeat("a", 64),
		"cc_gateway_egress_bucket":                             "egress:bucket-a",
		"cc_gateway_egress_bucket_enabled":                     "true",
		"cc_gateway_proxy_identity_ref":                        "hmac-sha256:" + strings.Repeat("e", 64),
		"cc_gateway_persona_profile":                           ccGatewayDefaultPersonaProfile,
		"claude_code_device_id":                                strings.Repeat("f", 64),
		ClaudePlatformAWSExtraWorkspaceRef:                     "workspace:ws-a",
		ClaudePlatformAWSExtraWorkspaceBindingHMAC:             "hmac-sha256:" + strings.Repeat("b", 64),
		ClaudePlatformAWSExtraEndpointRef:                      formalPoolSafeRef("endpoint", ClaudePlatformAWSEndpointForRegion("us-east-1")),
		ClaudePlatformAWSExtraRegion:                           "us-east-1",
		ClaudePlatformAWSExtraAuthScheme:                       ClaudePlatformAWSAuthProfileXAPIKey,
		ClaudePlatformAWSExtraRequestShapeProfileRef:           "request-shape:aws-v1",
		ClaudePlatformAWSExtraCacheParityProfileRef:            "cache-profile:aws-v1",
		ClaudePlatformAWSExtraBetaPolicyRef:                    "beta-policy:aws-v1",
		ClaudePlatformAWSExtraCP0AuthProfileEvidenceStatus:     "pass",
		ClaudePlatformAWSExtraCP0RegionWorkspaceEvidenceStatus: "pass",
		FormalPoolExtraRuntimeRegistered:                       "true",
		FormalPoolExtraRuntimeRegisteredAt:                     "2026-06-27T00:00:00Z",
	}}
	require.True(t, IsClaudePlatformAWSFormalPoolAccount(account))

	mismatch := *account
	mismatch.Extra = cloneCredentials(account.Extra)
	mismatch.Extra[ClaudePlatformAWSExtraRegion] = "eu-west-1"
	require.False(t, IsClaudePlatformAWSFormalPoolAccount(&mismatch))

	wrongEndpoint := *account
	wrongEndpoint.Extra = cloneCredentials(account.Extra)
	wrongEndpoint.Extra[ClaudePlatformAWSExtraEndpointRef] = formalPoolSafeRef("endpoint", ClaudePlatformAWSEndpointForRegion("eu-west-1"))
	require.False(t, IsClaudePlatformAWSFormalPoolAccount(&wrongEndpoint))
}

func TestClaudePlatformAWSFinalVerifierRejectsPortUserinfoAndInvalidRegion(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-api-key", syntheticAWSAPIKey())
	headers.Set("anthropic-workspace-id", syntheticAWSWorkspaceID(7))
	headers.Set("anthropic-version", "2023-06-01")
	headers.Set("content-type", "application/json")
	base := ClaudePlatformAWSFinalVerifierInput{Headers: headers, Region: "us-east-1", AuthScheme: ClaudePlatformAWSAuthProfileXAPIKey, WorkspaceFromServer: true, AuthFromServer: true, AllowedPath: "/v1/messages"}

	withPort := base
	withPort.FinalURL = "https://aws-external-anthropic.us-east-1.api.aws:443/v1/messages"
	require.ErrorContains(t, VerifyClaudePlatformAWSFinalRequest(withPort), "host")

	withUser := base
	withUser.FinalURL = "https://user@aws-external-anthropic.us-east-1.api.aws/v1/messages"
	require.ErrorContains(t, VerifyClaudePlatformAWSFinalRequest(withUser), "host")

	badRegion := base
	badRegion.Region = "us-east-1.evil"
	badRegion.FinalURL = "https://aws-external-anthropic.us-east-1.evil.api.aws/v1/messages"
	require.ErrorContains(t, VerifyClaudePlatformAWSFinalRequest(badRegion), "region")
}
