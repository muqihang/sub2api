package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	ClaudePlatformAWSAuthProfileXAPIKey      = "x_api_key"
	ClaudePlatformAWSAuthProfileBearerAPIKey = "bearer_api_key"
	ClaudePlatformAWSAuthProfileBlocked      = "BLOCKED_AUTH_PROFILE"

	ClaudePlatformAWSBatchRowCreate    = "create"
	ClaudePlatformAWSBatchRowDuplicate = "duplicate"

	ClaudePlatformAWSExtraWorkspaceRef                     = "anthropic_aws_workspace_ref"
	ClaudePlatformAWSExtraWorkspaceBindingHMAC             = "anthropic_aws_workspace_binding_hmac"
	ClaudePlatformAWSExtraEndpointRef                      = "anthropic_aws_endpoint_ref"
	ClaudePlatformAWSExtraRegion                           = "anthropic_aws_region"
	ClaudePlatformAWSExtraAuthScheme                       = "anthropic_aws_auth_scheme"
	ClaudePlatformAWSExtraRequestShapeProfileRef           = "anthropic_aws_request_shape_profile_ref"
	ClaudePlatformAWSExtraCacheParityProfileRef            = "anthropic_aws_cache_parity_profile_ref"
	ClaudePlatformAWSExtraBetaPolicyRef                    = "anthropic_aws_beta_policy_ref"
	ClaudePlatformAWSExtraCP0AuthProfileEvidenceStatus     = "anthropic_aws_cp0_auth_profile_evidence_status"
	ClaudePlatformAWSExtraCP0RegionWorkspaceEvidenceStatus = "anthropic_aws_cp0_region_workspace_evidence_status"
	ClaudePlatformAWSExtraProductionAdmitted               = "anthropic_aws_production_admitted"
)

const (
	claudePlatformAWSProviderKind = "claude_platform_aws"
	claudePlatformAWSAllowedPath  = "/v1/messages"
)

var (
	claudePlatformAWSWorkspaceIDRe = regexp.MustCompile(`^wrkspc_[A-Za-z0-9]+$`)
	claudePlatformAWSRegionRe      = regexp.MustCompile(`^[a-z]{2}-[a-z]+-[0-9]+$`)
)

type ClaudePlatformAWSAccountValidation struct {
	Region                string
	Endpoint              string
	AccountRef            string
	WorkspaceRef          string
	EndpointRef           string
	CredentialRef         string
	CredentialBindingHMAC string
	ProxyIdentityRef      string
	WorkspaceBindingHMAC  string
	AuthProfileStatus     string
}

type ClaudePlatformAWSAuthEvidence struct {
	XAPIKeyProven                bool
	BearerAPIProven              bool
	BothProfilesExplicitlyChosen bool
	SelectedProfile              string
	Endpoint                     string
	Region                       string
	WorkspaceRef                 string
	RequestShapePath             string
}

type ClaudePlatformAWSBatchImportInput struct {
	Rows []ClaudePlatformAWSBatchImportRow
}

type ClaudePlatformAWSBatchImportRow struct {
	Name        string
	WorkspaceID string
	Region      string
	APIKey      string
	ProxyID     int64
}

type ClaudePlatformAWSBatchImportResult struct {
	Rows []ClaudePlatformAWSBatchImportResultRow `json:"rows"`
}

type ClaudePlatformAWSBatchImportResultRow struct {
	Index                       int    `json:"index"`
	Name                        string `json:"name,omitempty"`
	Status                      string `json:"status"`
	Region                      string `json:"region"`
	WorkspaceRef                string `json:"workspace_ref"`
	WorkspaceBindingHMACPresent bool   `json:"workspace_binding_hmac_present"`
	EndpointRef                 string `json:"endpoint_ref"`
	CredentialRef               string `json:"credential_ref"`
	ProxyIdentityRef            string `json:"proxy_identity_ref"`
	AccountRef                  string `json:"account_ref"`
}

type ClaudePlatformAWSSessionTuple struct {
	ProviderKind           string
	AccountRef             string
	CredentialRef          string
	WorkspaceRef           string
	WorkspaceBindingHMAC   string
	EndpointRef            string
	Region                 string
	AuthScheme             string
	EgressBucket           string
	ProxyIdentityRef       string
	PersonaProfile         string
	RequestShapeProfileRef string
	CacheParityProfileRef  string
	BetaPolicyRef          string
	DeviceRef              string
}

type ClaudePlatformAWSFinalVerifierInput struct {
	FinalURL            string
	Headers             http.Header
	Region              string
	AuthScheme          string
	WorkspaceFromServer bool
	AuthFromServer      bool
	AllowedPath         string
}

func (a *Account) IsClaudePlatformAWS() bool {
	return a != nil && a.Platform == PlatformAnthropic && a.Type == AccountTypeClaudePlatformAWS
}

func ValidateClaudePlatformAWSAccount(account *Account) (ClaudePlatformAWSAccountValidation, error) {
	var out ClaudePlatformAWSAccountValidation
	if account == nil || !account.IsClaudePlatformAWS() {
		return out, fmt.Errorf("claude-platform-aws account is required")
	}
	if account.ProxyID == nil || *account.ProxyID <= 0 {
		return out, fmt.Errorf("proxy_id is required for claude-platform-aws")
	}
	region := strings.TrimSpace(account.GetCredential("aws_region"))
	if !claudePlatformAWSRegionRe.MatchString(region) {
		return out, fmt.Errorf("aws region is invalid")
	}
	workspaceID := strings.TrimSpace(account.GetCredential("anthropic_workspace_id"))
	if !claudePlatformAWSWorkspaceIDRe.MatchString(workspaceID) {
		return out, fmt.Errorf("workspace id is invalid")
	}
	if strings.TrimSpace(account.GetCredential("auth_mode")) != "apikey" {
		return out, fmt.Errorf("auth_mode apikey is required for claude-platform-aws phase 1")
	}
	if strings.TrimSpace(account.GetCredential("api_key")) == "" {
		return out, fmt.Errorf("api key is required for claude-platform-aws")
	}
	endpoint := ClaudePlatformAWSEndpointForRegion(region)
	if rawBase := strings.TrimSpace(account.GetCredential("base_url")); rawBase != "" && strings.TrimRight(rawBase, "/") != endpoint {
		return out, fmt.Errorf("base_url region mismatch for claude-platform-aws")
	}
	credentialRef := formalPoolSafeRef("credential", "claude-platform-aws:"+region+":"+strings.TrimSpace(account.GetCredential("api_key")))
	workspaceRef := ClaudePlatformAWSWorkspaceRef(region, workspaceID)
	endpointRef := formalPoolSafeRef("endpoint", endpoint)
	proxyRef := formalPoolSafeRef("proxy", fmt.Sprintf("%d", *account.ProxyID))
	accountRef := strings.TrimSpace(account.GetExtraString(ccGatewayExtraAccountRef))
	if accountRef == "" || accountRef == "0" {
		accountRef = formalPoolSafeRef("account", workspaceRef+"|"+credentialRef+"|"+proxyRef)
	}
	authScheme := strings.TrimSpace(account.GetExtraString(ClaudePlatformAWSExtraAuthScheme))
	if authScheme == "" {
		authScheme = ClaudePlatformAWSAuthProfileBlocked
	}
	credentialBinding := ccGatewayCredentialBindingHMACForMaterial("sub2api-claude-platform-aws-binding-v1", "apikey", strings.TrimSpace(account.GetCredential("api_key")))
	binding := ClaudePlatformAWSWorkspaceBindingHMAC("sub2api-claude-platform-aws-binding-v1", ClaudePlatformAWSSessionTuple{
		ProviderKind:     claudePlatformAWSProviderKind,
		AccountRef:       accountRef,
		CredentialRef:    credentialRef,
		WorkspaceRef:     workspaceRef,
		EndpointRef:      endpointRef,
		Region:           region,
		AuthScheme:       authScheme,
		EgressBucket:     strings.TrimSpace(account.GetExtraString(ccGatewayExtraEgressBucket)),
		ProxyIdentityRef: proxyRef,
	})
	status := strings.TrimSpace(account.GetCredential("cp0_auth_profile_status"))
	if status == "" || strings.EqualFold(status, "blocked") || status == ClaudePlatformAWSAuthProfileBlocked {
		status = ClaudePlatformAWSAuthProfileBlocked
	}
	out = ClaudePlatformAWSAccountValidation{Region: region, Endpoint: endpoint, AccountRef: accountRef, WorkspaceRef: workspaceRef, EndpointRef: endpointRef, CredentialRef: credentialRef, CredentialBindingHMAC: credentialBinding, ProxyIdentityRef: proxyRef, WorkspaceBindingHMAC: binding, AuthProfileStatus: status}
	return out, nil
}

func ClaudePlatformAWSEndpointForRegion(region string) string {
	return "https://aws-external-anthropic." + strings.TrimSpace(region) + ".api.aws"
}

func ClaudePlatformAWSWorkspaceRef(region, rawWorkspaceID string) string {
	return formalPoolSafeRef("workspace", "claude_platform_aws_workspace_ref_v1\x00"+strings.TrimSpace(region)+"\x00"+strings.TrimSpace(rawWorkspaceID))
}

func ClaudePlatformAWSWorkspaceBindingHMAC(secret string, tuple ClaudePlatformAWSSessionTuple) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		secret = "sub2api-claude-platform-aws-binding-v1"
	}
	parts := []string{
		"claude_platform_aws_workspace_binding_v1",
		tuple.ProviderKind,
		tuple.AccountRef,
		tuple.CredentialRef,
		tuple.WorkspaceRef,
		tuple.EndpointRef,
		tuple.Region,
		tuple.AuthScheme,
		tuple.EgressBucket,
		tuple.ProxyIdentityRef,
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(strings.Join(parts, "\x00")))
	return "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil))
}

func ResolveClaudePlatformAWSAuthProfile(ev ClaudePlatformAWSAuthEvidence) (string, error) {
	if strings.TrimSpace(ev.Endpoint) != "https://aws-external-anthropic.us-east-1.api.aws" || strings.TrimSpace(ev.Region) != "us-east-1" || strings.TrimSpace(ev.WorkspaceRef) == "" || strings.TrimSpace(ev.RequestShapePath) != claudePlatformAWSAllowedPath {
		return "", fmt.Errorf("BLOCKED_AUTH_PROFILE: CP0 endpoint/workspace/shape evidence is incomplete")
	}
	selected := strings.TrimSpace(ev.SelectedProfile)
	if ev.XAPIKeyProven && ev.BearerAPIProven && !ev.BothProfilesExplicitlyChosen {
		return "", fmt.Errorf("explicit operator choice required when both auth profiles are proven")
	}
	switch selected {
	case ClaudePlatformAWSAuthProfileXAPIKey:
		if !ev.XAPIKeyProven {
			return "", fmt.Errorf("silent fallback forbidden: x_api_key evidence is not proven")
		}
		return selected, nil
	case ClaudePlatformAWSAuthProfileBearerAPIKey:
		if !ev.BearerAPIProven {
			return "", fmt.Errorf("silent fallback forbidden: bearer_api_key evidence is not proven")
		}
		return selected, nil
	default:
		return "", fmt.Errorf("BLOCKED_AUTH_PROFILE: no proven auth profile selected")
	}
}

func ClaudePlatformAWSIdempotencyKey(headerKey, bodyKey string) string {
	if key := strings.TrimSpace(headerKey); key != "" {
		return key
	}
	return strings.TrimSpace(bodyKey)
}

func BuildClaudePlatformAWSBatchImport(input ClaudePlatformAWSBatchImportInput) (ClaudePlatformAWSBatchImportResult, error) {
	result := ClaudePlatformAWSBatchImportResult{Rows: make([]ClaudePlatformAWSBatchImportResultRow, 0, len(input.Rows))}
	seen := map[string]int{}
	for i, row := range input.Rows {
		if row.ProxyID <= 0 {
			return result, fmt.Errorf("proxy_id is required for row %d", i)
		}
		proxyID := row.ProxyID
		account := &Account{
			Platform: PlatformAnthropic,
			Type:     AccountTypeClaudePlatformAWS,
			ProxyID:  &proxyID,
			Credentials: map[string]any{
				"auth_mode":              "apikey",
				"api_key":                row.APIKey,
				"aws_region":             row.Region,
				"anthropic_workspace_id": row.WorkspaceID,
				"base_url":               ClaudePlatformAWSEndpointForRegion(row.Region),
			},
			Extra: map[string]any{},
		}
		validation, err := ValidateClaudePlatformAWSAccount(account)
		if err != nil {
			return result, fmt.Errorf("row %d validation failed: %w", i, err)
		}
		proxyRef := formalPoolSafeRef("proxy", fmt.Sprintf("%d", row.ProxyID))
		rowKey := validation.WorkspaceRef + "|" + validation.CredentialRef + "|" + proxyRef
		status := ClaudePlatformAWSBatchRowCreate
		if _, ok := seen[rowKey]; ok {
			status = ClaudePlatformAWSBatchRowDuplicate
		} else {
			seen[rowKey] = i
		}
		result.Rows = append(result.Rows, ClaudePlatformAWSBatchImportResultRow{
			Index:                       i,
			Name:                        safeClaudePlatformAWSLabel(row.Name),
			Status:                      status,
			Region:                      validation.Region,
			WorkspaceRef:                validation.WorkspaceRef,
			WorkspaceBindingHMACPresent: validation.WorkspaceBindingHMAC != "",
			EndpointRef:                 validation.EndpointRef,
			CredentialRef:               validation.CredentialRef,
			ProxyIdentityRef:            proxyRef,
			AccountRef:                  formalPoolSafeRef("account", rowKey),
		})
	}
	return result, nil
}

func safeClaudePlatformAWSLabel(raw string) string {
	label := strings.TrimSpace(raw)
	if label == "" {
		return ""
	}
	if strings.Contains(label, "wrkspc_") || strings.Contains(strings.ToLower(label), "api-key") || strings.Contains(strings.ToLower(label), "secret") {
		return "redacted"
	}
	if len(label) > 80 {
		return label[:80]
	}
	return label
}

func IsClaudePlatformAWSFormalPoolAccount(account *Account) bool {
	if account == nil || !account.IsClaudePlatformAWS() || account.ProxyID == nil {
		return false
	}
	if !formalPoolBool(account.Extra[FormalPoolExtraRuntimeRegistered]) {
		return false
	}
	if strings.TrimSpace(account.GetExtraString(ClaudePlatformAWSExtraCP0AuthProfileEvidenceStatus)) != "pass" || strings.TrimSpace(account.GetExtraString(ClaudePlatformAWSExtraCP0RegionWorkspaceEvidenceStatus)) != "pass" {
		return false
	}
	for _, ref := range []string{
		account.GetExtraString(ccGatewayExtraAccountRef),
		account.GetExtraString(ccGatewayExtraCredentialRef),
		account.GetExtraString(ccGatewayExtraEgressBucket),
		account.GetExtraString(ccGatewayExtraProxyIdentityRef),
		account.GetExtraString(ClaudePlatformAWSExtraWorkspaceRef),
		account.GetExtraString(ClaudePlatformAWSExtraEndpointRef),
	} {
		if !isClaudePlatformAWSSafeRef(ref) {
			return false
		}
	}
	for _, binding := range []string{
		account.GetExtraString(ccGatewayExtraCredentialBindingHMAC),
		account.GetExtraString(ClaudePlatformAWSExtraWorkspaceBindingHMAC),
	} {
		if !ledgerGeneratedHMACRefRe.MatchString(strings.TrimSpace(binding)) {
			return false
		}
	}
	switch strings.TrimSpace(account.GetExtraString(ClaudePlatformAWSExtraAuthScheme)) {
	case ClaudePlatformAWSAuthProfileXAPIKey, ClaudePlatformAWSAuthProfileBearerAPIKey:
	default:
		return false
	}
	region := strings.TrimSpace(account.GetExtraString(ClaudePlatformAWSExtraRegion))
	if !claudePlatformAWSRegionRe.MatchString(region) {
		return false
	}
	if strings.TrimSpace(account.GetExtraString(ClaudePlatformAWSExtraEndpointRef)) != formalPoolSafeRef("endpoint", ClaudePlatformAWSEndpointForRegion(region)) {
		return false
	}
	for _, field := range []string{
		ClaudePlatformAWSExtraRequestShapeProfileRef,
		ClaudePlatformAWSExtraCacheParityProfileRef,
		ClaudePlatformAWSExtraBetaPolicyRef,
	} {
		if strings.TrimSpace(account.GetExtraString(field)) == "" {
			return false
		}
	}
	return true
}

func IsFormalPoolEligibleAccount(account *Account) bool {
	if IsFormalPoolAccount(account) {
		return true
	}
	return IsClaudePlatformAWSFormalPoolAccount(account)
}

func formalPoolBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		enabled, ok := parseCCGatewayBool(v)
		return ok && enabled
	default:
		return false
	}
}

func isClaudePlatformAWSSafeRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if isSafeLedgerRef(ref) {
		return true
	}
	for _, prefix := range []string{"account:", "credential:", "workspace:", "endpoint:", "egress:", "proxy:", "request-shape:", "cache-profile:", "beta-policy:"} {
		if strings.HasPrefix(ref, prefix) && !strings.Contains(strings.ToLower(ref), "secret") && !strings.Contains(ref, "wrkspc_") {
			return true
		}
	}
	return false
}

func SanitizeClaudePlatformAWSInboundHeaders(headers http.Header) http.Header {
	out := http.Header{}
	for key, values := range headers {
		lower := strings.ToLower(strings.TrimSpace(key))
		if shouldStripClaudePlatformAWSInboundHeader(lower) {
			continue
		}
		for _, value := range values {
			out.Add(key, value)
		}
	}
	return out
}

func shouldStripClaudePlatformAWSInboundHeader(lower string) bool {
	if lower == "" {
		return true
	}
	if shouldStripAnthropicCompatInboundHeader(lower) {
		return true
	}
	if lower == "anthropic-workspace-id" || lower == "x-api-key" || lower == "authorization" || strings.HasPrefix(lower, "x-amz-") {
		return true
	}
	for _, marker := range []string{"account", "credential", "egress", "persona", "profile", "session", "billing", "cch", "control-plane", "workspace", "cache", "request-shape", "beta", "policy", "runtime"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func (t ClaudePlatformAWSSessionTuple) ValidateSame(attempt ClaudePlatformAWSSessionTuple) error {
	checks := []struct{ name, a, b string }{
		{"provider", t.ProviderKind, attempt.ProviderKind},
		{"account", t.AccountRef, attempt.AccountRef},
		{"credential", t.CredentialRef, attempt.CredentialRef},
		{"workspace", t.WorkspaceRef, attempt.WorkspaceRef},
		{"workspace_binding", t.WorkspaceBindingHMAC, attempt.WorkspaceBindingHMAC},
		{"endpoint", t.EndpointRef, attempt.EndpointRef},
		{"region", t.Region, attempt.Region},
		{"auth", t.AuthScheme, attempt.AuthScheme},
		{"egress", t.EgressBucket, attempt.EgressBucket},
		{"proxy", t.ProxyIdentityRef, attempt.ProxyIdentityRef},
		{"persona", t.PersonaProfile, attempt.PersonaProfile},
		{"profile", t.RequestShapeProfileRef, attempt.RequestShapeProfileRef},
		{"cache", t.CacheParityProfileRef, attempt.CacheParityProfileRef},
		{"beta", t.BetaPolicyRef, attempt.BetaPolicyRef},
		{"device", t.DeviceRef, attempt.DeviceRef},
	}
	for _, check := range checks {
		if strings.TrimSpace(check.a) != strings.TrimSpace(check.b) {
			return fmt.Errorf("claude-platform-aws session tuple mismatch: %s", check.name)
		}
	}
	return nil
}

func ValidateClaudePlatformAWSNoBypass(account *Account, ccGatewayEnabled bool) error {
	if account == nil || !account.IsClaudePlatformAWS() {
		return nil
	}
	if formalPoolBool(account.Extra[ClaudePlatformAWSExtraProductionAdmitted]) && !ccGatewayEnabled {
		return fmt.Errorf("CC Gateway is required for claude-platform-aws formal-pool production traffic")
	}
	return nil
}

func VerifyClaudePlatformAWSFinalRequest(input ClaudePlatformAWSFinalVerifierInput) error {
	u, err := url.Parse(strings.TrimSpace(input.FinalURL))
	if err != nil {
		return fmt.Errorf("invalid final url")
	}
	region := strings.TrimSpace(input.Region)
	if !claudePlatformAWSRegionRe.MatchString(region) {
		return fmt.Errorf("final region is invalid")
	}
	wantHost := "aws-external-anthropic." + region + ".api.aws"
	if u.Scheme != "https" || u.User != nil || !strings.EqualFold(u.Host, wantHost) {
		return fmt.Errorf("final host/region mismatch")
	}
	allowedPath := strings.TrimSpace(input.AllowedPath)
	if allowedPath == "" {
		allowedPath = claudePlatformAWSAllowedPath
	}
	if u.Path != allowedPath || u.RawQuery != "" {
		return fmt.Errorf("final request must use %s with empty query", allowedPath)
	}
	if !input.WorkspaceFromServer || countNonEmptyClaudePlatformAWSHeaderValues(input.Headers, "anthropic-workspace-id") != 1 {
		return fmt.Errorf("server workspace header is required")
	}
	if !input.AuthFromServer {
		return fmt.Errorf("server auth header is required")
	}
	if strings.TrimSpace(getHeaderRaw(input.Headers, "anthropic-version")) != "2023-06-01" {
		return fmt.Errorf("anthropic-version header is required")
	}
	if !strings.Contains(strings.ToLower(strings.TrimSpace(getHeaderRaw(input.Headers, "content-type"))), "application/json") {
		return fmt.Errorf("content-type application/json is required")
	}
	xKeyCount := countNonEmptyClaudePlatformAWSHeaderValues(input.Headers, "x-api-key")
	authValues := nonEmptyClaudePlatformAWSHeaderValues(input.Headers, "authorization")
	switch input.AuthScheme {
	case ClaudePlatformAWSAuthProfileXAPIKey:
		if xKeyCount != 1 || len(authValues) != 0 {
			return fmt.Errorf("exactly one auth header is required for x_api_key")
		}
	case ClaudePlatformAWSAuthProfileBearerAPIKey:
		if len(authValues) != 1 || xKeyCount != 0 {
			return fmt.Errorf("exactly one auth header is required for bearer_api_key")
		}
		if !strings.HasPrefix(authValues[0], "Bearer ") {
			return fmt.Errorf("Bearer auth header is required for bearer_api_key")
		}
	default:
		return fmt.Errorf("unsupported auth scheme")
	}
	for key := range input.Headers {
		lower := strings.ToLower(strings.TrimSpace(key))
		if shouldStripClaudePlatformAWSFinalHeader(lower) {
			return fmt.Errorf("internal header must be stripped")
		}
	}
	return nil
}

func shouldStripClaudePlatformAWSFinalHeader(lower string) bool {
	if lower == "authorization" || lower == "x-api-key" || lower == "anthropic-workspace-id" || lower == "anthropic-version" || lower == "content-type" || lower == "accept" || lower == "user-agent" {
		return false
	}
	return shouldStripClaudePlatformAWSInboundHeader(lower)
}

func countNonEmptyClaudePlatformAWSHeaderValues(headers http.Header, key string) int {
	return len(nonEmptyClaudePlatformAWSHeaderValues(headers, key))
}

func nonEmptyClaudePlatformAWSHeaderValues(headers http.Header, key string) []string {
	values := []string{}
	for actualKey, actualValues := range headers {
		if !strings.EqualFold(strings.TrimSpace(actualKey), strings.TrimSpace(key)) {
			continue
		}
		for _, value := range actualValues {
			if strings.TrimSpace(value) != "" {
				values = append(values, strings.TrimSpace(value))
			}
		}
	}
	return values
}
