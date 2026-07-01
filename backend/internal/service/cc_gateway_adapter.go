package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/tidwall/gjson"
)

const (
	ccGatewayTokenHeader               = "x-cc-gateway-token"
	ccGatewayAccountIDHeader           = "x-cc-account-id"
	ccGatewayProviderHeader            = "x-cc-provider"
	ccGatewayTokenTypeHeader           = "x-cc-token-type"
	ccGatewayAccountEmailHeader        = "x-cc-account-email"
	ccGatewayAccountUUIDHeader         = "x-cc-account-uuid"
	ccGatewayOrganizationUUIDHeader    = "x-cc-organization-uuid"
	ccGatewayProjectIDHeader           = "x-cc-project-id"
	ccGatewayEgressBucketHeader        = "x-cc-egress-bucket"
	ccGatewayPolicyVersionHeader       = "x-cc-policy-version"
	ccGatewayCredentialRefHeader       = "x-cc-credential-ref"
	ccGatewayFormalPoolContextHeader   = "x-cc-formal-pool-context"
	ccGatewayFormalPoolSignatureHeader = "x-cc-formal-pool-signature"
	ccGatewayInternalControlHeader     = "x-cc-internal-control-token"
	ccGatewayTrustedPersonaHeader      = "x-sub2api-persona-trusted"
	ccGatewayHealthcheckPersonaHeader  = "x-sub2api-healthcheck-persona"
	ccGatewayContext1MHeader           = "x-sub2api-context-1m"
	ccGatewayDefaultPersonaProfile     = "claude_code_2_1_179_native_degraded"
	ccGatewayHealthcheckNon1MProfile   = "claude_code_2_1_179_native_degraded"
	ccGatewayErrorKindHeader           = "x-cc-gateway-error-kind"
	ccGatewayErrorCodeHeader           = "x-cc-gateway-error-code"

	ccGatewayExtraEgressBucket          = "cc_gateway_egress_bucket"
	ccGatewayExtraEgressBucketEnabled   = "cc_gateway_egress_bucket_enabled"
	ccGatewayExtraPolicyVersion         = "cc_gateway_policy_version"
	ccGatewayExtraAccountRef            = "cc_gateway_account_ref"
	ccGatewayExtraCredentialRef         = "cc_gateway_credential_ref"
	ccGatewayExtraCredentialBindingHMAC = "cc_gateway_credential_binding_hmac"
	ccGatewayExtraProxyIdentityRef      = "cc_gateway_proxy_identity_ref"
	ccGatewayExtraPersonaProfile        = "cc_gateway_persona_profile"
	ccGatewayExtraTrustedEgressProfile  = "cc_gateway_trusted_egress_profile_ref"
	ccGatewayExtraEgressTLSProfileRef   = "cc_gateway_egress_tls_profile_ref"
	ccGatewayExtraProfilePolicyVersion  = "cc_gateway_profile_policy_version"
	ccGatewayExtraBillingShapePolicy    = "cc_gateway_billing_shape_policy"
	ccGatewayExtraRequestShapeProfile   = "cc_gateway_request_shape_profile_ref"
	ccGatewayExtraCacheParityProfile    = "cc_gateway_cache_parity_profile_ref"
	ccGatewayExtraEnvResidueProfileRef  = "cc_gateway_env_residue_profile_ref"
	ccGatewayExtraLocaleProfileRef      = "cc_gateway_locale_profile_ref"
	ccGatewayExtraBaseURLResidueProfile = "cc_gateway_base_url_residue_profile_ref"
	openAIGatewayExtraEgressFallback    = "openai_gateway_egress_bucket"

	ccGatewayExtraEnabled    = "cc_gateway_enabled"
	ccGatewayExtraCanaryOnly = "cc_gateway_canary_only"
	ccGatewayExtraBillingCCH = "billing_cch_mode"

	ccGatewayDefaultTrustedEgressProfileRef  = "strip_attribution"
	ccGatewayDefaultEgressTLSProfileRef      = "tls-profile:claude-code-2.1.179-real-oracle-tcp-v1"
	ccGatewayDefault2179ProfilePolicyVersion = "claude_code_2_1_179_cp1_degraded_v1"
	ccGatewayDefaultBillingShapePolicy       = "strip"
	ccGatewayDefault2179RequestShapeProfile  = "claude_code_2_1_179_messages_streaming_tooldefs_degraded_v1"
	ccGatewayDefault2179CacheParityProfile   = "claude_code_2_1_179_cache_parity_degraded_v1"
	ccGatewayDefaultEnvResidueProfileRef     = "env-residue-profile:claude-code-2.1.179-us-pacific-official-anthropic-v1"
	ccGatewayDefaultLocaleProfileRef         = "locale-profile:us-pacific-v1"
	ccGatewayDefaultBaseURLResidueProfileRef = "base-url-residue-profile:official-anthropic-v1"

	// Final shared-pool policy is anchored to the verified Claude Code 2.1.179
	// profile. Stale compatible account metadata is admission-only and final
	// normal outbound traffic canonicalizes to this version.
	ccGatewayAnthropicPolicyVersion = "2.1.179"
)

var ccGatewayCredentialBindingHMACRe = regexp.MustCompile(`^hmac-sha256:[a-fA-F0-9]{64}$`)

type ccGatewayAnthropicRoute string

const (
	ccGatewayRouteNativeMessages    ccGatewayAnthropicRoute = "native_messages"
	ccGatewayRouteNativeCountTokens ccGatewayAnthropicRoute = "native_count_tokens"
	ccGatewayRouteChatCompletions   ccGatewayAnthropicRoute = "chat_completions"
	ccGatewayRouteResponses         ccGatewayAnthropicRoute = "responses"
)

type CCGatewayAnthropicCanaryRequest struct {
	AccountID      int64
	AccountHash    string
	EgressBucket   string
	BillingCCHMode string
	Method         string
	Route          string
}

type ccGatewayExplicitCanaryRequestContextKey struct{}
type ccGatewayExplicitCanaryLocalOnlyContextKey struct{}
type ccGatewayObservedClientProfileContextKey struct{}

type ccGatewayObservedClientProfileSeed struct {
	CLIVersionBucket   string
	ClientFamilyBucket string
	ObservedProfile    map[string]any
}

func WithCCGatewayExplicitCanaryRequest(ctx context.Context, req CCGatewayAnthropicCanaryRequest) context.Context {
	return context.WithValue(ctx, ccGatewayExplicitCanaryRequestContextKey{}, req)
}

func GetCCGatewayExplicitCanaryRequest(ctx context.Context) (CCGatewayAnthropicCanaryRequest, bool) {
	req, ok := ctx.Value(ccGatewayExplicitCanaryRequestContextKey{}).(CCGatewayAnthropicCanaryRequest)
	return req, ok
}

func WithCCGatewayExplicitCanaryLocalOnly(ctx context.Context) context.Context {
	return context.WithValue(ctx, ccGatewayExplicitCanaryLocalOnlyContextKey{}, true)
}

func IsCCGatewayExplicitCanaryLocalOnly(ctx context.Context) bool {
	v, _ := ctx.Value(ccGatewayExplicitCanaryLocalOnlyContextKey{}).(bool)
	return v
}

func withCCGatewayObservedClientProfileSeed(ctx context.Context, headers http.Header) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	version := ccGatewayObservedCLIVersionBucketFromHeaders(headers)
	if version == "" {
		version = "unknown"
	}
	family := ccGatewayClientFamilyBucketFromValues(
		getHeaderRaw(headers, "User-Agent"),
		getHeaderRaw(headers, "X-Claude-Code-Client-Family"),
		getHeaderRaw(headers, "X-Client-Family"),
	)
	return context.WithValue(ctx, ccGatewayObservedClientProfileContextKey{}, ccGatewayObservedClientProfileSeed{CLIVersionBucket: version, ClientFamilyBucket: family})
}

func attachCCGatewayObservedClientProfileSnapshot(req *http.Request) {
	if req == nil {
		return
	}
	seed, _ := req.Context().Value(ccGatewayObservedClientProfileContextKey{}).(ccGatewayObservedClientProfileSeed)
	seed.ObservedProfile = ccGatewayObservedClientProfileForBody(req, ccGatewayRouteClassFromRequest(req), claudeCodeReadRequestBody(req))
	*req = *req.WithContext(context.WithValue(req.Context(), ccGatewayObservedClientProfileContextKey{}, seed))
}

func ccGatewayConfig(cfg *config.Config) config.GatewayCCGatewayConfig {
	if cfg == nil {
		return config.GatewayCCGatewayConfig{}
	}
	return cfg.Gateway.CCGateway
}

func ccGatewayAnthropicEnabled(cfg *config.Config) bool {
	ccg := ccGatewayConfig(cfg)
	return ccg.Enabled && ccg.Providers.Anthropic
}

func ccGatewayAntigravityEnabled(cfg *config.Config) bool {
	ccg := ccGatewayConfig(cfg)
	return ccg.Enabled && ccg.Providers.Antigravity
}

func ccGatewayURL(baseURL, path string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", fmt.Errorf("cc_gateway base_url is empty")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid cc_gateway base_url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid cc_gateway base_url: %s", baseURL)
	}
	basePath := strings.TrimRight(u.Path, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if idx := strings.Index(path, "?"); idx >= 0 {
		u.RawQuery = path[idx+1:]
		path = path[:idx]
	}
	u.Path = basePath + path
	return u.String(), nil
}

func (s *GatewayService) ccGatewayAnthropicRequestURL(path string) (string, error) {
	ccg := ccGatewayConfig(s.cfg)
	return ccGatewayURL(ccg.BaseURL, path)
}

func (s *GatewayService) shouldUseCCGatewayAnthropic(account *Account) bool {
	if account == nil ||
		account.Platform != PlatformAnthropic ||
		(!account.IsAnthropicOAuthOrSetupToken() && !account.IsAnthropicAPIKeyPassthroughEnabled()) ||
		!ccGatewayAnthropicEnabled(s.cfg) {
		return false
	}
	enabled, ok := parseCCGatewayBool(account.GetExtraString(ccGatewayExtraEnabled))
	if !ok {
		return false
	}
	return enabled
}

func (s *GatewayService) hasExplicitCCGatewayAnthropicConfig(account *Account) bool {
	return account != nil &&
		account.Platform == PlatformAnthropic &&
		(account.IsAnthropicOAuthOrSetupToken() || account.IsAnthropicAPIKeyPassthroughEnabled()) &&
		ccGatewayAnthropicEnabled(s.cfg) &&
		strings.TrimSpace(account.GetExtraString(ccGatewayExtraEnabled)) != ""
}

func requiresCCGatewayAnthropicFailClosed(account *Account) bool {
	return IsFormalPoolAccount(account)
}

// GetExplicitCCGatewayCanaryAccount fetches a canary-only account for a
// request that has already opted into the explicit canary control plane.
func (s *GatewayService) GetExplicitCCGatewayCanaryAccount(ctx context.Context, req CCGatewayAnthropicCanaryRequest) (*Account, error) {
	if req.AccountID <= 0 {
		return nil, fmt.Errorf("cc gateway explicit canary account id is required")
	}
	account, err := s.accountRepo.GetByID(ctx, req.AccountID)
	if err != nil {
		return nil, fmt.Errorf("get cc gateway explicit canary account: %w", err)
	}
	useCCGateway, err := s.selectCCGatewayAnthropicCanaryRoute(account, req)
	if err != nil {
		return nil, err
	}
	if !useCCGateway {
		return nil, fmt.Errorf("cc gateway explicit canary account %d is not eligible", req.AccountID)
	}
	return account, nil
}

func ccGatewayBaseURLIsLocal(cfg *config.Config) bool {
	base := strings.TrimSpace(ccGatewayConfig(cfg).BaseURL)
	if base == "" {
		return false
	}
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func parseCCGatewayBool(raw string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func parseCCGatewayRouteSet(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, part := range strings.FieldsFunc(strings.ToLower(strings.TrimSpace(raw)), func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	}) {
		if part == "" {
			continue
		}
		out[part] = struct{}{}
	}
	return out
}

func (s *GatewayService) selectCCGatewayAnthropicCanaryRoute(account *Account, req CCGatewayAnthropicCanaryRequest) (bool, error) {
	if !s.shouldUseCCGatewayAnthropic(account) {
		return false, nil
	}
	if err := validateCCGatewayAnthropicCanaryAccountWithConfig(s.cfg, account, req); err != nil {
		return false, err
	}
	return s.selectCCGatewayAnthropicRouteForMode(account, ccGatewayRouteNativeMessages, true)
}

func ValidateCCGatewayAnthropicCanaryAccount(account *Account, req CCGatewayAnthropicCanaryRequest) error {
	if account == nil {
		return fmt.Errorf("cc gateway canary account is required")
	}
	if account.Platform != PlatformAnthropic || !account.IsAnthropicOAuthOrSetupToken() {
		return fmt.Errorf("cc gateway canary requires anthropic oauth/setup-token account")
	}
	if strings.ToUpper(strings.TrimSpace(req.Method)) != http.MethodPost || strings.TrimSpace(req.Route) != "/v1/messages" {
		return fmt.Errorf("cc gateway canary route must be POST /v1/messages for account %d", account.ID)
	}
	if strings.TrimSpace(req.BillingCCHMode) != "sign" || strings.TrimSpace(account.GetExtraString(ccGatewayExtraBillingCCH)) != "sign" {
		return fmt.Errorf("cc gateway canary requires billing_cch_mode=sign for account %d", account.ID)
	}
	if account.Status != StatusActive {
		return fmt.Errorf("cc gateway canary lifecycle ineligible for account %d", account.ID)
	}
	if err := validateAnthropicMessagesInferenceScope(account); err != nil {
		return err
	}
	if req.AccountID > 0 && req.AccountID != account.ID {
		return fmt.Errorf("cc gateway canary account id mismatch for account %d", account.ID)
	}
	if requestedHash := strings.TrimSpace(req.AccountHash); requestedHash != "" && requestedHash != ccGatewayAccountRef(account) {
		return fmt.Errorf("cc gateway canary account hash mismatch for account %d", account.ID)
	}
	enabled, ok := parseCCGatewayBool(account.GetExtraString(ccGatewayExtraEnabled))
	if !ok || !enabled {
		return fmt.Errorf("cc gateway canary enabled gate missing for account %d", account.ID)
	}
	canaryOnly, ok := parseCCGatewayBool(account.GetExtraString(ccGatewayExtraCanaryOnly))
	if !ok || !canaryOnly {
		return fmt.Errorf("cc gateway canary-only gate missing for account %d", account.ID)
	}
	if strings.TrimSpace(account.GetExtraString(ccGatewayExtraEgressBucket)) == "" {
		return fmt.Errorf("cc gateway canary requires direct cc gateway egress bucket for account %d", account.ID)
	}
	if strings.TrimSpace(req.EgressBucket) == "" || strings.TrimSpace(req.EgressBucket) != strings.TrimSpace(resolveCCGatewayEgressBucket(account)) {
		return fmt.Errorf("cc gateway canary egress bucket mismatch for account %d", account.ID)
	}
	return nil
}

func validateCCGatewayAnthropicCanaryAccountWithConfig(cfg *config.Config, account *Account, req CCGatewayAnthropicCanaryRequest) error {
	if !ccGatewayBaseURLIsLocal(cfg) {
		accountID := int64(0)
		if account != nil {
			accountID = account.ID
		}
		return fmt.Errorf("cc gateway canary requires local cc gateway base url for account %d", accountID)
	}
	return ValidateCCGatewayAnthropicCanaryAccount(account, req)
}

func (s *GatewayService) selectCCGatewayAnthropicRoute(account *Account, route ccGatewayAnthropicRoute) (bool, error) {
	return s.selectCCGatewayAnthropicRouteForMode(account, route, false)
}

func (s *GatewayService) selectCCGatewayAnthropicRouteForMode(account *Account, route ccGatewayAnthropicRoute, explicitCanary bool) (bool, error) {
	if !s.shouldUseCCGatewayAnthropic(account) {
		if s.hasExplicitCCGatewayAnthropicConfig(account) || requiresCCGatewayAnthropicFailClosed(account) {
			return false, fmt.Errorf("cc gateway disabled or missing for anthropic account %d", account.ID)
		}
		return false, nil
	}

	enabled, ok := parseCCGatewayBool(account.GetExtraString(ccGatewayExtraEnabled))
	if !ok || !enabled {
		return false, fmt.Errorf("cc gateway disabled or missing for anthropic account %d", account.ID)
	}
	canaryOnly, ok := parseCCGatewayBool(account.GetExtraString(ccGatewayExtraCanaryOnly))
	if !ok {
		return false, fmt.Errorf("cc gateway canary gate missing for account %d", account.ID)
	}
	if canaryOnly && !explicitCanary {
		return false, fmt.Errorf("cc gateway canary-only account %d is not eligible for broad routing", account.ID)
	}
	if IsFormalPoolAccount(account) && !isSafeLedgerRef(strings.TrimSpace(account.GetExtraString(ccGatewayExtraAccountRef))) {
		return false, fmt.Errorf("cc gateway account ref missing or unsafe for account %d", account.ID)
	}
	if IsFormalPoolAccount(account) && !explicitCanary {
		if !account.IsSchedulable() {
			return false, fmt.Errorf("cc gateway lifecycle ineligible for account %d", account.ID)
		}
	} else if account.Status != "" && account.Status != StatusActive {
		return false, fmt.Errorf("cc gateway lifecycle ineligible for account %d", account.ID)
	} else if account.Status != "" && !explicitCanary && !account.IsSchedulable() {
		return false, fmt.Errorf("cc gateway lifecycle ineligible for account %d", account.ID)
	}
	if version := strings.TrimSpace(account.GetExtraString(ccGatewayExtraPolicyVersion)); version == "" || !ccGatewayPolicyVersionCompatible(version) {
		return false, fmt.Errorf("cc gateway policy version mismatch for account %d", account.ID)
	}
	bucketEnabled, ok := parseCCGatewayBool(account.GetExtraString(ccGatewayExtraEgressBucketEnabled))
	if !ok || !bucketEnabled {
		return false, fmt.Errorf("cc gateway egress bucket disabled or missing for account %d", account.ID)
	}
	if bucket := strings.TrimSpace(resolveCCGatewayEgressBucket(account)); bucket == "" {
		return false, fmt.Errorf("cc gateway egress bucket missing for account %d", account.ID)
	}

	allowSet := parseCCGatewayRouteSet(account.GetExtraString("cc_gateway_routes"))
	if len(allowSet) == 0 {
		return false, fmt.Errorf("cc gateway route allowlist missing for account %d", account.ID)
	}
	routeName := string(route)
	if _, denied := parseCCGatewayRouteSet(account.GetExtraString("cc_gateway_routes_deny"))[routeName]; denied {
		return false, fmt.Errorf("cc gateway route %s denied for account %d", routeName, account.ID)
	}
	if _, allowed := allowSet[routeName]; !allowed {
		return false, fmt.Errorf("cc gateway route %s not allowed for account %d", routeName, account.ID)
	}

	return true, nil
}

func (s *GatewayService) ccGatewayEgressBucket(account *Account) string {
	return resolveCCGatewayEgressBucket(account)
}

func resolveCCGatewayEgressBucket(account *Account) string {
	if account != nil {
		if bucket := strings.TrimSpace(account.GetExtraString(ccGatewayExtraEgressBucket)); bucket != "" {
			return bucket
		}
	}
	return ""
}

func applyCCGatewayAnthropicHeaders(req *http.Request, cfg *config.Config, account *Account, tokenType string) error {
	if req == nil || account == nil || cfg == nil {
		return nil
	}
	ccg := cfg.Gateway.CCGateway
	setHeaderRaw(req.Header, ccGatewayTokenHeader, strings.TrimSpace(ccg.Token))
	setHeaderRaw(req.Header, ccGatewayAccountIDHeader, ccGatewayAccountRef(account))
	setHeaderRaw(req.Header, ccGatewayProviderHeader, PlatformAnthropic)
	setHeaderRaw(req.Header, ccGatewayTokenTypeHeader, tokenType)
	setHeaderRaw(req.Header, ccGatewayPolicyVersionHeader, ccGatewayAnthropicPolicyVersion)
	setHeaderRaw(req.Header, ccGatewayCredentialRefHeader, ccGatewayCredentialRef(account))
	// Formal shared-pool Anthropic routing must not send raw email/account/org
	// identity headers to CC Gateway. Account identity is selected by the
	// server-owned x-cc-account-id ref and CC Gateway account_identities config.
	setHeaderRaw(req.Header, ccGatewayEgressBucketHeader, resolveCCGatewayEgressBucket(account))
	return applyCCGatewayClaudeCodeSessionMapping(req, account)
}

func applyCCGatewayInternalControlToken(req *http.Request, cfg *config.Config) {
	if req == nil || cfg == nil {
		return
	}
	if token := strings.TrimSpace(cfg.Gateway.CCGateway.InternalControlToken); token != "" {
		setHeaderRaw(req.Header, ccGatewayInternalControlHeader, token)
	}
}

func applyCCGatewayFormalPoolAttestation(req *http.Request, cfg *config.Config, account *Account) error {
	return applyCCGatewayFormalPoolAttestationWithPersona(req, cfg, account, "")
}

func applyCCGatewayFormalPoolAttestationWithPersona(req *http.Request, cfg *config.Config, account *Account, personaOverride string) error {
	if req == nil || cfg == nil || account == nil {
		return nil
	}
	if !requiresCCGatewayFormalPoolAttestation(account) {
		return nil
	}
	applyCCGatewayInternalControlToken(req, cfg)
	secret := strings.TrimSpace(cfg.Gateway.CCGateway.ContextAttestationSecret)
	if secret == "" {
		if cfg.Gateway.CCGateway.Enabled {
			return fmt.Errorf("cc gateway formal-pool attestation secret is required")
		}
		return nil
	}
	credentialRef := strings.TrimSpace(ccGatewayCredentialRef(account))
	credentialBinding := strings.TrimSpace(ccGatewayCredentialBindingHMAC(account))
	proxyIdentityRef := strings.TrimSpace(ccGatewayProxyIdentityRef(account))
	egressTLSProfileRef, tlsProfileOK := ccGatewayEgressTLSProfileRef(account)
	personaProfile := strings.TrimSpace(personaOverride)
	if personaProfile == "" {
		personaProfile = strings.TrimSpace(ccGatewayPersonaProfile(account))
	}
	sessionID := strings.TrimSpace(getHeaderRaw(req.Header, "X-Claude-Code-Session-Id"))
	missing := make([]string, 0, 12)
	if credentialRef == "" {
		missing = append(missing, "credential_ref")
	}
	if proxyIdentityRef == "" {
		missing = append(missing, "proxy_identity_ref")
	}
	if personaProfile == "" {
		missing = append(missing, "persona_profile")
	}
	if egressTLSProfileRef == "" {
		missing = append(missing, "egress_tls_profile_ref")
	}
	if sessionID == "" {
		missing = append(missing, "session_id")
	}
	awsCtx, err := claudePlatformAWSFormalPoolAttestationContext(account)
	if err != nil {
		return err
	}
	if account.IsClaudePlatformAWS() {
		for _, field := range []struct {
			name  string
			value string
		}{
			{"credential_binding_hmac", credentialBinding},
			{"provider_kind", stringFromMap(awsCtx, "provider_kind")},
			{"upstream_auth_scheme", stringFromMap(awsCtx, "upstream_auth_scheme")},
			{"aws_region", stringFromMap(awsCtx, "aws_region")},
			{"upstream_endpoint_ref", stringFromMap(awsCtx, "upstream_endpoint_ref")},
			{"upstream_host", stringFromMap(awsCtx, "upstream_host")},
			{"allowed_upstream_path", stringFromMap(awsCtx, "allowed_upstream_path")},
			{"workspace_ref", stringFromMap(awsCtx, "workspace_ref")},
			{"workspace_binding_hmac", stringFromMap(awsCtx, "workspace_binding_hmac")},
			{"request_shape_profile_ref", stringFromMap(awsCtx, "request_shape_profile_ref")},
			{"cache_parity_profile_ref", stringFromMap(awsCtx, "cache_parity_profile_ref")},
			{"beta_policy_ref", stringFromMap(awsCtx, "beta_policy_ref")},
		} {
			if strings.TrimSpace(field.value) == "" {
				missing = append(missing, field.name)
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("cc gateway formal-pool attestation context is incomplete: missing %s", strings.Join(missing, ","))
	}
	if !isSafeLedgerRef(credentialRef) || !isSafeLedgerRef(proxyIdentityRef) {
		return fmt.Errorf("cc gateway formal-pool attestation refs are unsafe")
	}
	if !tlsProfileOK {
		return fmt.Errorf("cc gateway formal-pool attestation egress_tls_profile_ref is unsafe")
	}
	envResidueProfileRef, envResidueOK := ccGatewayEnvResidueProfileRef(account)
	localeProfileRef, localeOK := ccGatewayLocaleProfileRef(account)
	baseURLResidueProfileRef, baseURLOK := ccGatewayBaseURLResidueProfileRef(account)
	if !envResidueOK {
		return fmt.Errorf("cc gateway formal-pool attestation env_residue_profile_ref is unsafe")
	}
	if !localeOK {
		return fmt.Errorf("cc gateway formal-pool attestation locale_profile_ref is unsafe")
	}
	if !baseURLOK {
		return fmt.Errorf("cc gateway formal-pool attestation base_url_residue_profile_ref is unsafe")
	}
	stripClientTLSProfileHints(req)
	stripClientEnvResidueProfileHints(req)
	routeClass := ccGatewayRouteClassFromRequest(req)
	path := "/v1/messages"
	if req.URL != nil {
		path = strings.TrimSpace(req.URL.Path)
	}
	ctx := map[string]any{
		"account_id":                   strings.TrimSpace(getHeaderRaw(req.Header, ccGatewayAccountIDHeader)),
		"credential_source":            "server_account_credentials",
		"credential_ref":               credentialRef,
		"egress_bucket":                strings.TrimSpace(getHeaderRaw(req.Header, ccGatewayEgressBucketHeader)),
		"method":                       strings.ToUpper(strings.TrimSpace(req.Method)),
		"nonce":                        "nonce-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		"observed_client_profile":      ccGatewayObservedClientProfile(req, routeClass),
		"path":                         path,
		"persona_profile":              personaProfile,
		"policy_version":               strings.TrimSpace(getHeaderRaw(req.Header, ccGatewayPolicyVersionHeader)),
		"proxy_identity_ref":           proxyIdentityRef,
		"route_class":                  routeClass,
		"session_id":                   sessionID,
		"timestamp_ms":                 time.Now().UnixMilli(),
		"token_type":                   strings.TrimSpace(getHeaderRaw(req.Header, ccGatewayTokenTypeHeader)),
		"trusted_egress_profile_ref":   ccGatewayTrustedEgressProfileRef(account),
		"egress_tls_profile_ref":       egressTLSProfileRef,
		"env_residue_profile_ref":      envResidueProfileRef,
		"locale_profile_ref":           localeProfileRef,
		"base_url_residue_profile_ref": baseURLResidueProfileRef,
		"profile_policy_version":       ccGatewayProfilePolicyVersion(account),
		"billing_shape_policy":         ccGatewayBillingShapePolicy(account),
		"request_shape_profile_ref":    ccGatewayAttestationRequestShapeProfileRef(account),
		"cache_parity_profile_ref":     ccGatewayAttestationCacheParityProfileRef(account),
	}
	if credentialBinding != "" {
		ctx["credential_binding_hmac"] = credentialBinding
	}
	for k, v := range awsCtx {
		ctx[k] = v
	}
	raw, err := json.Marshal(ctx)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(raw)
	setHeaderRaw(req.Header, ccGatewayFormalPoolContextHeader, base64.RawURLEncoding.EncodeToString(raw))
	setHeaderRaw(req.Header, ccGatewayFormalPoolSignatureHeader, "hmac-sha256:"+hex.EncodeToString(mac.Sum(nil)))
	return nil
}

func requiresCCGatewayFormalPoolAttestation(account *Account) bool {
	if IsFormalPoolAccount(account) {
		return true
	}
	if account != nil && account.IsClaudePlatformAWS() {
		return true
	}
	if account == nil || account.Platform != PlatformAnthropic || account.Type != AccountTypeAPIKey {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(account.GetExtraString(ccGatewayExtraEnabled)), "true")
}

func applyCCGatewayContext1MSelection(req *http.Request, cfg *config.Config, account *Account) {
	if req == nil {
		return
	}
	if ccGatewayServerSelectedContext1M(account) {
		applyCCGatewayInternalControlToken(req, cfg)
		setHeaderRaw(req.Header, ccGatewayContext1MHeader, "true")
	}
}

func ccGatewayServerSelectedContext1M(account *Account) bool {
	for _, profile := range []string{
		account.GetExtraString(FormalPoolExtraPoolProfileEffective),
		ccGatewayPersonaProfile(account),
	} {
		profile = strings.ToLower(strings.TrimSpace(profile))
		if profile == "" || strings.Contains(profile, "non_1m") || strings.Contains(profile, "non-1m") {
			continue
		}
		if strings.HasSuffix(profile, "_1m") ||
			strings.HasSuffix(profile, "-1m") ||
			strings.Contains(profile, "subscription_1m") ||
			strings.Contains(profile, "context_1m") {
			return true
		}
	}
	return false
}

func applyCCGatewayAnthropicPolicyVersion(ctx context.Context, req *http.Request, account *Account) {
	if req == nil {
		return
	}
	if IsFormalPoolAccount(account) {
		if version := strings.TrimSpace(account.GetExtraString(ccGatewayExtraPolicyVersion)); version != "" && ccGatewayPolicyVersionCompatible(version) {
			setHeaderRaw(req.Header, ccGatewayPolicyVersionHeader, ccGatewayAnthropicPolicyVersion)
		}
		return
	}
	trustedPersona := ccGatewayTrustedPersonaContext(ctx)
	if trustedPersona {
		if version := strings.TrimSpace(GetClaudeCodeVersion(ctx)); version != "" {
			if ccGatewayPolicyVersionCompatible(version) {
				setHeaderRaw(req.Header, ccGatewayTrustedPersonaHeader, "1")
				setHeaderRaw(req.Header, ccGatewayPolicyVersionHeader, version)
				return
			}
		} else {
			setHeaderRaw(req.Header, ccGatewayTrustedPersonaHeader, "1")
		}
	}
	if account != nil {
		// Stale compatible account metadata is admission-only. Do not mutate DB Extra
		// here, but canonicalize final normal outbound persona to the verified
		// final policy version.
		if version := strings.TrimSpace(account.GetExtraString(ccGatewayExtraPolicyVersion)); version != "" && ccGatewayPolicyVersionCompatible(version) {
			setHeaderRaw(req.Header, ccGatewayPolicyVersionHeader, ccGatewayAnthropicPolicyVersion)
		}
	}
}

func ccGatewayTrustedPersonaContext(ctx context.Context) bool {
	_, ok := GetCCGatewayExplicitCanaryRequest(ctx)
	return ok
}

func enforceFormalPoolNativeProtocolBoundary(ctx context.Context, account *Account, protocol string) error {
	if !IsFormalPoolAccount(account) {
		return nil
	}
	protocol = strings.TrimSpace(protocol)
	if _, ok := ClaudeCodeBridgeAuditSummaryFromContext(ctx); ok {
		return fmt.Errorf("formal pool accounts must not use bridge protocol")
	}
	if _, ok := AnthropicCompatAuditSummaryFromContext(ctx); ok {
		switch protocol {
		case "", "native_messages", "native_count_tokens":
			if !IsClaudeCodeClient(ctx) {
				return fmt.Errorf("formal pool accounts must not use compat protocol")
			}
		default:
			return fmt.Errorf("formal pool accounts must not use compat protocol")
		}
	}
	switch protocol {
	case "", "native_messages", "native_count_tokens":
		return nil
	default:
		return fmt.Errorf("formal pool accounts require native messages protocol")
	}
}

func ccGatewayAccountRef(account *Account) string {
	if account == nil {
		return ""
	}
	if ref := strings.TrimSpace(account.GetExtraString(ccGatewayExtraAccountRef)); ref != "" {
		return ref
	}
	return strconv.FormatInt(account.ID, 10)
}

func ccGatewayCredentialRef(account *Account) string {
	if account == nil {
		return ""
	}
	return strings.TrimSpace(account.GetExtraString(ccGatewayExtraCredentialRef))
}

func ccGatewayCredentialBindingHMAC(account *Account) string {
	if account == nil {
		return ""
	}
	value := strings.TrimSpace(account.GetExtraString(ccGatewayExtraCredentialBindingHMAC))
	if ccGatewayCredentialBindingHMACRe.MatchString(value) {
		return strings.ToLower(value)
	}
	return ""
}

func ccGatewayCredentialBindingHMACForMaterial(secret, tokenType, rawCredential string) string {
	secret = strings.TrimSpace(secret)
	tokenType = strings.TrimSpace(tokenType)
	rawCredential = strings.TrimSpace(rawCredential)
	if secret == "" || tokenType == "" || rawCredential == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("formal_pool_credential_binding_v1"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(tokenType))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(rawCredential))
	return "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil))
}

func ccGatewayOAuthCredentialBindingHMAC(secret, accessToken string) string {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return ""
	}
	return ccGatewayCredentialBindingHMACForMaterial(secret, "oauth", "Bearer "+accessToken)
}

func ccGatewaySelectedCredentialBindingMaterial(account *Account) (string, string) {
	if account == nil {
		return "", ""
	}
	return ccGatewayCredentialBindingMaterialFromCredentials(account.Type, account.Credentials)
}

func ccGatewayCredentialBindingMaterialFromCredentials(accountType string, credentials map[string]any) (string, string) {
	switch accountType {
	case AccountTypeOAuth, AccountTypeSetupToken:
		if accessToken := ccGatewayCredentialString(credentials, "access_token"); accessToken != "" {
			return "oauth", "Bearer " + accessToken
		}
	case AccountTypeAPIKey, AccountTypeClaudePlatformAWS:
		if apiKey := ccGatewayCredentialString(credentials, "api_key"); apiKey != "" {
			return "apikey", apiKey
		}
	}
	return "", ""
}

func ccGatewayCredentialString(credentials map[string]any, key string) string {
	if credentials == nil {
		return ""
	}
	v, ok := credentials[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val)
	case json.Number:
		return strings.TrimSpace(val.String())
	case float64:
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)
	case int:
		return strconv.Itoa(val)
	default:
		return ""
	}
}

func ccGatewayCredentialBindingHMACFromAccount(secret string, account *Account) string {
	tokenType, rawCredential := ccGatewaySelectedCredentialBindingMaterial(account)
	return ccGatewayCredentialBindingHMACForMaterial(secret, tokenType, rawCredential)
}

func ccGatewayGeneratedCredentialRef(accountRef, generation string) string {
	accountRef = strings.TrimSpace(accountRef)
	generation = strings.TrimSpace(generation)
	if generation == "" {
		generation = "1"
	}
	if accountRef == "" {
		return ""
	}
	return formalPoolSafeRef("credential", accountRef+":"+generation)
}

func ccGatewayGeneratedDeviceID(accountRef string) string {
	accountRef = strings.TrimSpace(accountRef)
	if accountRef == "" {
		return ""
	}
	return hex.EncodeToString(scopedStickyHMACBytes("formal_pool_claude_code_device", accountRef))
}

func ccGatewayDeviceID(account *Account) string {
	if account == nil {
		return ""
	}
	for _, key := range []string{"claude_code_device_id", "device_id"} {
		if deviceID := strings.TrimSpace(account.GetExtraString(key)); claudeCodeDeviceIDRe.MatchString(deviceID) {
			return strings.ToLower(deviceID)
		}
	}
	return ""
}

func ccGatewayProxyIdentityRef(account *Account) string {
	if account == nil {
		return ""
	}
	if ref := strings.TrimSpace(account.GetExtraString(ccGatewayExtraProxyIdentityRef)); ref != "" {
		return ref
	}
	return strings.TrimSpace(account.GetExtraString("proxy_identity_ref"))
}

func ccGatewayPersonaProfile(account *Account) string {
	if account == nil {
		return ""
	}
	if v := strings.TrimSpace(account.GetExtraString(ccGatewayExtraPersonaProfile)); v != "" {
		return v
	}
	return ""
}

func ccGatewayTrustedEgressProfileRef(account *Account) string {
	if account != nil {
		if v := safeProfileRef(account.GetExtraString(ccGatewayExtraTrustedEgressProfile)); v != "" {
			return v
		}
	}
	return ccGatewayDefaultTrustedEgressProfileRef
}

func ccGatewayEgressTLSProfileRef(account *Account) (string, bool) {
	if account != nil {
		raw := strings.TrimSpace(account.GetExtraString(ccGatewayExtraEgressTLSProfileRef))
		if raw != "" {
			value := safeTLSProfileRef(raw)
			return value, value != ""
		}
	}
	return ccGatewayDefaultEgressTLSProfileRef, true
}

func ccGatewayEnvResidueProfileRef(account *Account) (string, bool) {
	return ccGatewayResidueAuthorityProfileRef(account, ccGatewayExtraEnvResidueProfileRef, ccGatewayDefaultEnvResidueProfileRef, "env-residue-profile:")
}

func ccGatewayLocaleProfileRef(account *Account) (string, bool) {
	return ccGatewayResidueAuthorityProfileRef(account, ccGatewayExtraLocaleProfileRef, ccGatewayDefaultLocaleProfileRef, "locale-profile:")
}

func ccGatewayBaseURLResidueProfileRef(account *Account) (string, bool) {
	return ccGatewayResidueAuthorityProfileRef(account, ccGatewayExtraBaseURLResidueProfile, ccGatewayDefaultBaseURLResidueProfileRef, "base-url-residue-profile:")
}

func ccGatewayResidueAuthorityProfileRef(account *Account, extraKey, defaultValue, prefix string) (string, bool) {
	if account != nil {
		raw := strings.TrimSpace(account.GetExtraString(extraKey))
		if raw != "" {
			value := safeEnvResidueAuthorityProfileRef(raw, prefix)
			return value, value != ""
		}
	}
	return defaultValue, true
}

func ccGatewayProfilePolicyVersion(account *Account) string {
	if account != nil {
		if v := safeProfileRef(account.GetExtraString(ccGatewayExtraProfilePolicyVersion)); v != "" {
			return v
		}
	}
	return ccGatewayDefault2179ProfilePolicyVersion
}

func ccGatewayBillingShapePolicy(account *Account) string {
	if account != nil {
		switch strings.TrimSpace(strings.ToLower(account.GetExtraString(ccGatewayExtraBillingShapePolicy))) {
		case "strip", "no_cch", "signed_cch":
			return strings.TrimSpace(strings.ToLower(account.GetExtraString(ccGatewayExtraBillingShapePolicy)))
		}
	}
	return ccGatewayDefaultBillingShapePolicy
}

func ccGatewayRequestShapeProfileRef(account *Account) string {
	if account != nil {
		if v := safeProfileRef(account.GetExtraString(ccGatewayExtraRequestShapeProfile)); v != "" {
			return v
		}
	}
	return ccGatewayDefault2179RequestShapeProfile
}

func ccGatewayCacheParityProfileRef(account *Account) string {
	if account != nil {
		if v := safeProfileRef(account.GetExtraString(ccGatewayExtraCacheParityProfile)); v != "" {
			return v
		}
	}
	return ccGatewayDefault2179CacheParityProfile
}

func safeTLSProfileRef(raw string) string {
	value := safeProfileRef(raw)
	if value == "" || !strings.HasPrefix(value, "tls-profile:") {
		return ""
	}
	lower := strings.ToLower(value)
	for _, forbidden := range []string{"secret", "token", "api-key", "apikey", "sk-", "bearer", "basic", "sha256:", "md5:", "clienthello", "cipher", "extension", "pcap", "cert", "key"} {
		if strings.Contains(lower, forbidden) {
			return ""
		}
	}
	return value
}

func safeEnvResidueAuthorityProfileRef(raw, prefix string) string {
	value := safeProfileRef(raw)
	if value == "" || !strings.HasPrefix(value, prefix) {
		return ""
	}
	lower := strings.ToLower(value)
	for _, forbidden := range []string{"secret", "token", "api-key", "apikey", "sk-", "bearer", "basic", "http:", "https:", "anthropic_base_url", "http_proxy", "https_proxy", "all_proxy", "no_proxy", "tz=", "clienthello", "pcap", "cert", "key"} {
		if strings.Contains(lower, forbidden) {
			return ""
		}
	}
	return value
}

func isCCGatewayClientTLSProfileHintKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	normalized := strings.ReplaceAll(strings.ReplaceAll(lower, "-", "_"), ":", "_")
	compact := strings.NewReplacer("_", "", "-", "", ":", "", ".", "").Replace(lower)
	switch normalized {
	case "egress_tls_profile_ref", "tls_profile", "tls_profile_ref", "client_tls_profile", "client_tls_profile_ref", "cc_gateway_egress_tls_profile_ref", "x_cc_egress_tls_profile_ref", "x_cc_tls_profile_ref", "x_sub2api_tls_profile", "x_sub2api_egress_tls_profile", "x_tls_profile":
		return true
	default:
		return strings.Contains(normalized, "tls_profile") ||
			strings.Contains(normalized, "egress_tls") ||
			strings.Contains(compact, "tlsprofile") ||
			strings.Contains(compact, "egresstls")
	}
}

func isCCGatewayClientEnvResidueHintKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	normalized := strings.ReplaceAll(strings.ReplaceAll(lower, "-", "_"), ":", "_")
	compact := strings.NewReplacer("_", "", "-", "", ":", "", ".", "").Replace(lower)
	switch normalized {
	case "env_residue_profile_ref", "locale_profile_ref", "base_url_residue_profile_ref",
		"cc_gateway_env_residue_profile_ref", "cc_gateway_locale_profile_ref", "cc_gateway_base_url_residue_profile_ref",
		"x_cc_env_residue_profile_ref", "x_cc_locale_profile_ref", "x_cc_base_url_residue_profile_ref",
		"anthropic_base_url", "base_url", "proxy_url", "http_proxy", "https_proxy", "all_proxy", "no_proxy", "tz", "timezone":
		return true
	default:
		return strings.Contains(normalized, "env_residue_profile") ||
			strings.Contains(normalized, "locale_profile") ||
			strings.Contains(normalized, "base_url_residue_profile") ||
			strings.Contains(normalized, "anthropic_base_url") ||
			strings.Contains(normalized, "base_url") ||
			strings.Contains(normalized, "proxy_url") ||
			strings.Contains(normalized, "http_proxy") ||
			strings.Contains(normalized, "https_proxy") ||
			strings.Contains(normalized, "all_proxy") ||
			strings.Contains(normalized, "no_proxy") ||
			compact == "tz" ||
			compact == "timezone" ||
			strings.Contains(compact, "envresidueprofile") ||
			strings.Contains(compact, "localeprofile") ||
			strings.Contains(compact, "baseurlresidueprofile") ||
			strings.Contains(compact, "anthropicbaseurl") ||
			compact == "baseurl" ||
			compact == "proxyurl" ||
			strings.Contains(compact, "httpproxy") ||
			strings.Contains(compact, "httpsproxy") ||
			strings.Contains(compact, "allproxy") ||
			strings.Contains(compact, "noproxy")
	}
}

func stripClientTLSProfileHints(req *http.Request) {
	if req == nil {
		return
	}
	if req.URL != nil {
		q := req.URL.Query()
		changed := false
		for key := range q {
			if isCCGatewayClientTLSProfileHintKey(key) {
				delete(q, key)
				changed = true
			}
		}
		if changed {
			req.URL.RawQuery = q.Encode()
		}
	}
	for _, key := range []string{
		"x-cc-egress-tls-profile-ref",
		"x-cc-tls-profile-ref",
		"x-sub2api-tls-profile",
		"x-sub2api-egress-tls-profile",
		"x-tls-profile",
	} {
		req.Header.Del(key)
	}
}

func stripClientEnvResidueProfileHints(req *http.Request) {
	if req == nil {
		return
	}
	if req.URL != nil {
		q := req.URL.Query()
		changed := false
		for key := range q {
			if isCCGatewayClientEnvResidueHintKey(key) {
				delete(q, key)
				changed = true
			}
		}
		if changed {
			req.URL.RawQuery = q.Encode()
		}
	}
	for _, key := range []string{
		"x-cc-env-residue-profile-ref",
		"x-cc-locale-profile-ref",
		"x-cc-base-url-residue-profile-ref",
		"x-sub2api-env-residue-profile-ref",
		"x-sub2api-locale-profile-ref",
		"x-sub2api-base-url-residue-profile-ref",
		"anthropic-base-url",
		"base-url",
		"http-proxy",
		"https-proxy",
		"all-proxy",
		"no-proxy",
		"tz",
	} {
		req.Header.Del(key)
	}
	body := claudeCodeReadRequestBody(req)
	if len(body) == 0 {
		return
	}
	rewritten, changed := stripClientEnvResidueProfileHintsFromBody(body)
	if changed {
		claudeCodeReplaceRequestBody(req, rewritten)
	}
}

func stripClientEnvResidueProfileHintsFromBody(body []byte) ([]byte, bool) {
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return body, false
	}
	changed := stripClientEnvResidueHintsFromTopLevel(parsed)
	if !changed {
		return body, false
	}
	rewritten, err := json.Marshal(parsed)
	if err != nil {
		return body, false
	}
	return rewritten, true
}

func stripClientEnvResidueHintsFromTopLevel(body map[string]any) bool {
	changed := false
	for key, child := range body {
		if isCCGatewayClientEnvResidueHintKey(key) {
			delete(body, key)
			changed = true
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "metadata":
			if stripClientEnvResidueHintsFromAllowedSurface(child) {
				changed = true
			}
		case "tools":
			if stripClientEnvResidueHintsFromTools(child) {
				changed = true
			}
		}
	}
	return changed
}

func stripClientEnvResidueHintsFromTools(v any) bool {
	tools, ok := v.([]any)
	if !ok {
		return false
	}
	changed := false
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for key, child := range tool {
			if isCCGatewayClientEnvResidueHintKey(key) {
				delete(tool, key)
				changed = true
				continue
			}
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "metadata", "input_schema", "schema", "parameters", "fields", "properties":
				if stripClientEnvResidueHintsFromAllowedSurface(child) {
					changed = true
				}
			}
		}
	}
	return changed
}

func stripClientEnvResidueHintsFromAllowedSurface(v any) bool {
	switch x := v.(type) {
	case []any:
		changed := false
		for _, child := range x {
			if stripClientEnvResidueHintsFromAllowedSurface(child) {
				changed = true
			}
		}
		return changed
	case map[string]any:
		changed := false
		for key, child := range x {
			if isCCGatewayClientEnvResidueHintKey(key) {
				delete(x, key)
				changed = true
				continue
			}
			if stripClientEnvResidueHintsFromAllowedSurface(child) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func safeProfileRef(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == ':' || r == '.' {
			continue
		}
		return ""
	}
	if len(value) > 128 {
		return ""
	}
	return value
}

func ccGatewayObservedClientProfile(req *http.Request, routeClass string) map[string]any {
	if req != nil {
		if seed, ok := req.Context().Value(ccGatewayObservedClientProfileContextKey{}).(ccGatewayObservedClientProfileSeed); ok && len(seed.ObservedProfile) > 0 {
			return cloneCCGatewayObservedClientProfile(seed.ObservedProfile)
		}
	}
	return ccGatewayObservedClientProfileForBody(req, routeClass, claudeCodeReadRequestBody(req))
}

func ccGatewayObservedClientProfileForBody(req *http.Request, routeClass string, body []byte) map[string]any {
	billingHeaders := collectCCGatewayBillingHeaderTexts(body)
	profile := map[string]any{
		"schema_version":     "observed_client_profile.v1",
		"cli_version_bucket": ccGatewayObservedCLIVersionBucketWithBillingHeaders(req, billingHeaders),
		"route_class":        sanitizeReasonCode(routeClass),
	}
	if req == nil {
		return profile
	}
	if stream := gjson.GetBytes(body, "stream"); stream.Exists() && (stream.Type == gjson.True || stream.Type == gjson.False) {
		profile["stream"] = stream.Bool()
	}
	if len(body) > 0 {
		keys := make([]string, 0)
		unknownKeys := 0
		gjson.ParseBytes(body).ForEach(func(k, _ gjson.Result) bool {
			rawKey := k.String()
			if isCCGatewayClientTLSProfileHintKey(rawKey) || isCCGatewayClientEnvResidueHintKey(rawKey) {
				return true
			}
			key := ccGatewayObservedSafeTopLevelBodyKey(rawKey)
			if key != "" {
				keys = append(keys, key)
			} else {
				unknownKeys++
			}
			return true
		})
		sort.Strings(keys)
		profile["top_level_body_keys"] = keys
		if unknownKeys > 0 {
			profile["unknown_top_level_body_key_count"] = unknownKeys
		}
		profile["tool_count"] = int(gjson.GetBytes(body, "tools.#").Int())
		profile["thinking_present"] = gjson.GetBytes(body, "thinking").Exists()
		profile["output_config_present"] = gjson.GetBytes(body, "output_config").Exists()
		profile["context_management_present"] = gjson.GetBytes(body, "context_management").Exists()
	}
	profile["client_family_bucket"] = ccGatewayClientFamilyBucket(req, billingHeaders)
	applyCCGatewayObservedEnvResidueBuckets(profile, req, body)
	profile["billing_block_count"] = len(billingHeaders)
	profile["billing_shape"] = ccGatewayBillingShapeFromObservedHeaders(billingHeaders)
	profile["cc_entrypoint_bucket"] = ccGatewayEntrypointBucketFromObservedHeaders(billingHeaders)
	return profile
}

func cloneCCGatewayObservedClientProfile(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		switch values := v.(type) {
		case []string:
			out[k] = append([]string(nil), values...)
		case []any:
			out[k] = append([]any(nil), values...)
		default:
			out[k] = v
		}
	}
	return out
}

func ccGatewayRouteClassFromRequest(req *http.Request) string {
	if req != nil && req.URL != nil && strings.TrimSpace(req.URL.Path) == "/v1/messages/count_tokens" {
		return "count_tokens"
	}
	return "messages"
}

func ccGatewayObservedSafeTopLevelBodyKey(raw string) string {
	key := strings.TrimSpace(raw)
	if isCCGatewayClientTLSProfileHintKey(key) || isCCGatewayClientEnvResidueHintKey(key) {
		return ""
	}
	switch key {
	case "anthropic_beta",
		"context_management",
		"max_tokens",
		"messages",
		"metadata",
		"model",
		"output_config",
		"service_tier",
		"stream",
		"system",
		"thinking",
		"tool_choice",
		"tools":
		return key
	default:
		return ""
	}
}

func ccGatewayObservedCLIVersionBucket(req *http.Request) string {
	if req == nil {
		return "unknown"
	}
	if seed, ok := req.Context().Value(ccGatewayObservedClientProfileContextKey{}).(ccGatewayObservedClientProfileSeed); ok {
		if version := strings.TrimSpace(seed.CLIVersionBucket); version != "" {
			return version
		}
	}
	return ccGatewayObservedCLIVersionBucketFromHeaders(req.Header)
}

func ccGatewayObservedCLIVersionBucketFromHeaders(headers http.Header) string {
	return ccGatewayObservedCLIVersionBucketFromValues(
		getHeaderRaw(headers, "User-Agent"),
		getHeaderRaw(headers, ClaudeCodeNativeClaudeCodeVersionHeader),
	)
}

func ccGatewayObservedCLIVersionBucketWithBillingHeaders(req *http.Request, billingHeaders []string) string {
	values := make([]string, 0, 2+len(billingHeaders))
	if req != nil {
		if seed, ok := req.Context().Value(ccGatewayObservedClientProfileContextKey{}).(ccGatewayObservedClientProfileSeed); ok {
			if version := strings.TrimSpace(seed.CLIVersionBucket); version != "" && version != "unknown" {
				return version
			}
		}
		values = append(values, getHeaderRaw(req.Header, "User-Agent"), getHeaderRaw(req.Header, ClaudeCodeNativeClaudeCodeVersionHeader))
	}
	values = append(values, billingHeaders...)
	return ccGatewayObservedCLIVersionBucketFromValues(values...)
}

func ccGatewayObservedCLIVersionBucketFromValues(values ...string) string {
	for _, raw := range values {
		if match := regexp.MustCompile(`\b(\d+\.\d+\.\d+)\b`).FindStringSubmatch(raw); len(match) == 2 {
			return match[1]
		}
	}
	return "unknown"
}

func ccGatewayClientFamilyBucket(req *http.Request, billingHeaders []string) string {
	if req != nil {
		if seed, ok := req.Context().Value(ccGatewayObservedClientProfileContextKey{}).(ccGatewayObservedClientProfileSeed); ok {
			if family := strings.TrimSpace(seed.ClientFamilyBucket); family != "" && family != "unknown" {
				return family
			}
		}
	}
	values := make([]string, 0, 3+len(billingHeaders))
	if req != nil {
		values = append(values, getHeaderRaw(req.Header, "User-Agent"), getHeaderRaw(req.Header, "X-Claude-Code-Client-Family"), getHeaderRaw(req.Header, "X-Client-Family"))
	}
	values = append(values, billingHeaders...)
	return ccGatewayClientFamilyBucketFromValues(values...)
}

func ccGatewayClientFamilyBucketFromValues(values ...string) string {
	for _, raw := range values {
		lower := strings.ToLower(raw)
		switch {
		case strings.Contains(lower, "claude-vscode") || strings.Contains(lower, "vscode") || strings.Contains(lower, "vs code"):
			return "vscode"
		case strings.Contains(lower, "desktop"):
			return "desktop"
		case strings.Contains(lower, "claude-cli") || strings.Contains(lower, "sdk-cli") || regexp.MustCompile(`(?i)\bcc_entrypoint=cli;`).MatchString(raw):
			return "cli"
		}
	}
	return "unknown"
}

func applyCCGatewayObservedEnvResidueBuckets(profile map[string]any, req *http.Request, body []byte) {
	dateFormat, apostrophe, markerObserved := ccGatewayObservedDateMarkerBuckets(body)
	baseURLCategory, baseURLObserved := ccGatewayObservedBaseURLCategoryBucket(req, body)
	proxyBucket, proxyObserved := ccGatewayObservedProxyEnvBucket(req, body)
	envHintObserved := ccGatewayEnvResidueHintObserved(req, body)
	profile["local_env_residue_present"] = markerObserved || baseURLObserved || proxyObserved || envHintObserved
	profile["date_format_bucket"] = dateFormat
	profile["apostrophe_bucket"] = apostrophe
	profile["base_url_category_bucket"] = baseURLCategory
	profile["proxy_env_bucket"] = proxyBucket
}

func ccGatewayObservedDateMarkerBuckets(body []byte) (string, string, bool) {
	texts := ccGatewaySystemTextBlocks(body)
	dateFormat := "not_observed"
	apostrophe := "not_observed"
	observed := false
	re := regexp.MustCompile(`(?i)Today(['\x{2019}\x{2018}\x{02BC}])s date is ([0-9]{4})([-/])([0-9]{2})[-/]([0-9]{2})\.`)
	for _, text := range texts {
		match := re.FindStringSubmatch(text)
		if len(match) == 0 {
			continue
		}
		observed = true
		switch match[1] {
		case "'":
			apostrophe = "ascii"
		case "\u2019":
			apostrophe = "unicode_variant_1"
		case "\u2018":
			apostrophe = "unicode_variant_2"
		case "\u02bc":
			apostrophe = "unicode_variant_3"
		default:
			apostrophe = "other"
		}
		switch match[3] {
		case "-":
			dateFormat = "hyphen"
		case "/":
			dateFormat = "slash"
		default:
			dateFormat = "other"
		}
		break
	}
	return dateFormat, apostrophe, observed
}

func ccGatewaySystemTextBlocks(body []byte) []string {
	if len(body) == 0 {
		return nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	system, ok := parsed["system"]
	if !ok {
		return nil
	}
	var out []string
	switch v := system.(type) {
	case string:
		out = append(out, v)
	case []any:
		for _, item := range v {
			switch block := item.(type) {
			case string:
				out = append(out, block)
			case map[string]any:
				if typ, _ := block["type"].(string); typ != "" && typ != "text" {
					continue
				}
				if text, _ := block["text"].(string); text != "" {
					out = append(out, text)
				}
			}
		}
	}
	return out
}

func ccGatewayObservedBaseURLCategoryBucket(req *http.Request, body []byte) (string, bool) {
	var values []string
	if req != nil {
		for _, key := range []string{"Anthropic-Base-Url", "Base-Url"} {
			if v := strings.TrimSpace(getHeaderRaw(req.Header, key)); v != "" {
				values = append(values, v)
			}
		}
		if req.URL != nil {
			for key, vals := range req.URL.Query() {
				if isCCGatewayBaseURLLiteralKey(key) {
					values = append(values, vals...)
				}
			}
		}
	}
	values = append(values, ccGatewayCollectStringValuesForHintKeys(body, func(key string) bool {
		return isCCGatewayBaseURLLiteralKey(key)
	})...)
	for _, value := range values {
		if bucket := ccGatewayBaseURLCategoryBucket(value); bucket != "" {
			return bucket, true
		}
	}
	return "not_observed", false
}

func isCCGatewayBaseURLResidueKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	normalized := strings.ReplaceAll(strings.ReplaceAll(lower, "-", "_"), ":", "_")
	compact := strings.NewReplacer("_", "", "-", "", ":", "", ".", "").Replace(lower)
	return normalized == "anthropic_base_url" ||
		normalized == "base_url" ||
		normalized == "base_url_residue_profile_ref" ||
		strings.Contains(normalized, "anthropic_base_url") ||
		strings.Contains(normalized, "base_url_residue") ||
		strings.Contains(compact, "anthropicbaseurl") ||
		strings.Contains(compact, "baseurlresidue")
}

func isCCGatewayBaseURLLiteralKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	normalized := strings.ReplaceAll(strings.ReplaceAll(lower, "-", "_"), ":", "_")
	compact := strings.NewReplacer("_", "", "-", "", ":", "", ".", "").Replace(lower)
	return normalized == "anthropic_base_url" ||
		normalized == "base_url" ||
		strings.Contains(normalized, "anthropic_base_url") ||
		strings.Contains(compact, "anthropicbaseurl")
}

func ccGatewayBaseURLCategoryBucket(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return ""
	}
	if strings.Contains(value, "api.anthropic.com") || strings.Contains(value, "anthropic.com") {
		return "official_anthropic"
	}
	if strings.Contains(value, ".cn") {
		return "china_tld"
	}
	if strings.Contains(value, "gateway") || strings.Contains(value, "localhost") || strings.Contains(value, "127.0.0.1") || strings.Contains(value, "test.invalid") {
		return "neutral_gateway"
	}
	if strings.Contains(value, ".org") {
		return "china_org_domain"
	}
	if strings.Contains(value, "cloud") {
		return "china_cloud_domain"
	}
	if strings.Contains(value, "ai") || strings.Contains(value, "lab") {
		return "ai_lab_keyword"
	}
	if strings.Contains(value, "proxy") || strings.Contains(value, "resale") {
		return "claude_proxy_resale_like"
	}
	return "unknown"
}

func ccGatewayObservedProxyEnvBucket(req *http.Request, body []byte) (string, bool) {
	var values []string
	if req != nil {
		for _, key := range []string{"HTTP-Proxy", "HTTPS-Proxy", "All-Proxy", "No-Proxy"} {
			if v := strings.TrimSpace(getHeaderRaw(req.Header, key)); v != "" {
				values = append(values, v)
			}
		}
		if req.URL != nil {
			for key, vals := range req.URL.Query() {
				if isCCGatewayProxyEnvResidueKey(key) {
					values = append(values, vals...)
				}
			}
		}
	}
	values = append(values, ccGatewayCollectStringValuesForHintKeys(body, isCCGatewayProxyEnvResidueKey)...)
	if len(values) == 0 {
		return "no_proxy_env", false
	}
	for _, value := range values {
		lower := strings.ToLower(strings.TrimSpace(value))
		if strings.Contains(lower, "127.0.0.1") || strings.Contains(lower, "localhost") || strings.Contains(lower, "[::1]") {
			continue
		}
		if strings.Contains(lower, "no_proxy") || strings.Contains(lower, "no-proxy") {
			return "no_proxy_bypass_guarded", true
		}
		return "non_loopback_proxy_rejected", true
	}
	return "loopback_proxy_only", true
}

func isCCGatewayProxyEnvResidueKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	normalized := strings.ReplaceAll(strings.ReplaceAll(lower, "-", "_"), ":", "_")
	compact := strings.NewReplacer("_", "", "-", "", ":", "", ".", "").Replace(lower)
	return normalized == "http_proxy" || normalized == "https_proxy" || normalized == "all_proxy" || normalized == "no_proxy" ||
		strings.Contains(normalized, "http_proxy") || strings.Contains(normalized, "https_proxy") ||
		strings.Contains(normalized, "all_proxy") || strings.Contains(normalized, "no_proxy") ||
		strings.Contains(compact, "httpproxy") || strings.Contains(compact, "httpsproxy") ||
		strings.Contains(compact, "allproxy") || strings.Contains(compact, "noproxy")
}

func ccGatewayEnvResidueHintObserved(req *http.Request, body []byte) bool {
	if req != nil {
		if strings.TrimSpace(getHeaderRaw(req.Header, "TZ")) != "" {
			return true
		}
		if req.URL != nil {
			for key := range req.URL.Query() {
				if isCCGatewayClientEnvResidueHintKey(key) {
					return true
				}
			}
		}
	}
	return len(ccGatewayCollectStringValuesForHintKeys(body, isCCGatewayClientEnvResidueHintKey)) > 0
}

func ccGatewayCollectStringValuesForHintKeys(body []byte, keyMatch func(string) bool) []string {
	if len(body) == 0 || keyMatch == nil {
		return nil
	}
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	var out []string
	var walk func(any, string)
	walk = func(v any, parentKey string) {
		switch x := v.(type) {
		case map[string]any:
			for key, child := range x {
				if keyMatch(key) {
					if text, ok := child.(string); ok {
						out = append(out, text)
					}
					continue
				}
				if strings.EqualFold(parentKey, "messages") || strings.EqualFold(key, "messages") {
					continue
				}
				walk(child, key)
			}
		case []any:
			for _, child := range x {
				walk(child, parentKey)
			}
		}
	}
	walk(parsed, "")
	return out
}

func collectCCGatewayBillingHeaderTexts(body []byte) []string {
	if len(body) == 0 {
		return nil
	}
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(x)), "x-anthropic-billing-header:") {
				out = append(out, x)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		case map[string]any:
			for key, child := range x {
				if isCCGatewayClientTLSProfileHintKey(key) {
					continue
				}
				walk(child)
			}
		}
	}
	walk(parsed)
	return out
}

func ccGatewayBillingShapeFromObservedHeaders(headers []string) string {
	if len(headers) == 0 {
		return "absent"
	}
	for _, header := range headers {
		if regexp.MustCompile(`(?i)\bcch=[a-f0-9]{5};`).MatchString(header) {
			return "cch_present"
		}
	}
	return "no_cch"
}

func ccGatewayEntrypointBucketFromObservedHeaders(headers []string) string {
	for _, header := range headers {
		match := regexp.MustCompile(`(?i)\bcc_entrypoint=([^;]+);`).FindStringSubmatch(header)
		if len(match) < 2 {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(match[1])) {
		case "cli":
			return "cli"
		case "sdk-cli":
			return "sdk-cli"
		case "claude-vscode":
			return "claude-vscode"
		default:
			return "other"
		}
	}
	return "absent"
}

func ccGatewayAccountEmail(account *Account) string {
	if account == nil {
		return ""
	}
	for _, key := range []string{"email", "account_email"} {
		if v := strings.TrimSpace(account.GetExtraString(key)); v != "" {
			return v
		}
	}
	for _, key := range []string{"email", "account_email"} {
		if v := strings.TrimSpace(account.GetCredential(key)); v != "" {
			return v
		}
	}
	return ""
}

func ccGatewayPolicyVersionCompatible(version string) bool {
	// Keep this as an explicit verified-corpus gate, not a broad semver range.
	// 2.1.171 was not published; 2.1.172+ are not admitted unless promoted to
	// an explicit verified profile such as the current 2.1.175 final persona.
	switch strings.TrimSpace(version) {
	case "2.1.150", "2.1.153", "2.1.169", "2.1.170", ccGatewayAnthropicPolicyVersion:
		return true
	default:
		return false
	}
}

func applyCCGatewayAntigravityHeaders(req *http.Request, p antigravityRetryLoopParams) {
	if req == nil || p.account == nil {
		return
	}
	req.Header.Set(ccGatewayTokenHeader, strings.TrimSpace(p.ccGatewayToken))
	req.Header.Set(ccGatewayAccountIDHeader, strconv.FormatInt(p.account.ID, 10))
	req.Header.Set(ccGatewayProviderHeader, PlatformAntigravity)
	req.Header.Set(ccGatewayTokenTypeHeader, "oauth")
	req.Header.Set(ccGatewayEgressBucketHeader, strings.TrimSpace(p.ccGatewayEgressBucket))
	if p.ccGatewayProjectID != "" {
		req.Header.Set(ccGatewayProjectIDHeader, strings.TrimSpace(p.ccGatewayProjectID))
	}
	// Do not send raw account email to CC Gateway. Formal shared-pool
	// identity is selected by server-owned account refs, not raw PII headers.
}

func isCCGatewayControlPlaneResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(resp.Header.Get(ccGatewayErrorKindHeader)), "control-plane")
}

func ccGatewayControlPlaneCode(resp *http.Response, body []byte) string {
	if resp != nil {
		if code := safeCCGatewayControlPlaneCode(resp.Header.Get(ccGatewayErrorCodeHeader)); code != "" {
			return code
		}
	}
	if code := safeCCGatewayControlPlaneCode(gjson.GetBytes(body, "error.code").String()); code != "" {
		return code
	}
	return "unknown_control_plane"
}

func safeCCGatewayControlPlaneCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 128 || !formalPoolDiagnosticSafeCodeRe.MatchString(code) || formalPoolUnsafeDiagnosticText(code) {
		return ""
	}
	return code
}

func ccGatewayControlPlaneMessage(body []byte) string {
	if msg := strings.TrimSpace(gjson.GetBytes(body, "error.message").String()); msg != "" {
		if ccGatewayControlPlaneMessageUnsafe(msg) {
			return "CC Gateway control-plane rejected request"
		}
		return sanitizeUpstreamErrorMessage(msg)
	}
	return "CC Gateway control-plane rejected request"
}

func ccGatewayControlPlaneMessageUnsafe(msg string) bool {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return false
	}
	if formalPoolDiagnosticSensitiveKeyValueRe.MatchString(msg) {
		return true
	}
	for _, token := range strings.Fields(msg) {
		if formalPoolUnsafeDiagnosticText(token) {
			return true
		}
	}
	return false
}

func newAntigravityAPIRequestWithCCGateway(
	ctx context.Context,
	baseURL string,
	action string,
	accessToken string,
	body []byte,
	p antigravityRetryLoopParams,
) (*http.Request, error) {
	req, err := antigravity.NewAPIRequestWithURL(ctx, baseURL, action, accessToken, body)
	if err != nil {
		return nil, err
	}
	if p.ccGatewayEnabled {
		applyCCGatewayAntigravityHeaders(req, p)
	}
	return req, nil
}

func (s *AntigravityGatewayService) ccGatewayAntigravityParams(account *Account, projectID string) (enabled bool, baseURL string, token string, bucket string, email string) {
	if s == nil || s.settingService == nil || s.settingService.cfg == nil || !ccGatewayAntigravityEnabled(s.settingService.cfg) {
		return false, "", "", "", ""
	}
	ccg := s.settingService.cfg.Gateway.CCGateway
	return true,
		strings.TrimSpace(ccg.BaseURL),
		strings.TrimSpace(ccg.Token),
		resolveCCGatewayEgressBucket(account),
		ccGatewayAccountEmail(account)
}

func claudePlatformAWSFormalPoolAttestationContext(account *Account) (map[string]any, error) {
	if account == nil || !account.IsClaudePlatformAWS() {
		return nil, nil
	}
	region := strings.TrimSpace(account.GetExtraString(ClaudePlatformAWSExtraRegion))
	if !claudePlatformAWSRegionRe.MatchString(region) {
		return nil, nil
	}
	endpoint := ClaudePlatformAWSEndpointForRegion(region)
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("cc gateway formal-pool attestation endpoint is invalid")
	}
	return map[string]any{
		"provider_kind":             claudePlatformAWSProviderKind,
		"upstream_auth_scheme":      strings.TrimSpace(account.GetExtraString(ClaudePlatformAWSExtraAuthScheme)),
		"aws_region":                region,
		"upstream_endpoint_ref":     strings.TrimSpace(account.GetExtraString(ClaudePlatformAWSExtraEndpointRef)),
		"upstream_host":             u.Host,
		"allowed_upstream_path":     claudePlatformAWSAllowedPath,
		"workspace_ref":             strings.TrimSpace(account.GetExtraString(ClaudePlatformAWSExtraWorkspaceRef)),
		"workspace_binding_hmac":    strings.TrimSpace(account.GetExtraString(ClaudePlatformAWSExtraWorkspaceBindingHMAC)),
		"request_shape_profile_ref": ccGatewayAttestationRequestShapeProfileRef(account),
		"cache_parity_profile_ref":  ccGatewayAttestationCacheParityProfileRef(account),
		"beta_policy_ref":           strings.TrimSpace(account.GetExtraString(ClaudePlatformAWSExtraBetaPolicyRef)),
	}, nil
}

func ccGatewayAttestationRequestShapeProfileRef(account *Account) string {
	if account != nil && account.IsClaudePlatformAWS() {
		return safeProfileRef(account.GetExtraString(ClaudePlatformAWSExtraRequestShapeProfileRef))
	}
	return ccGatewayRequestShapeProfileRef(account)
}

func ccGatewayAttestationCacheParityProfileRef(account *Account) string {
	if account != nil && account.IsClaudePlatformAWS() {
		return safeProfileRef(account.GetExtraString(ClaudePlatformAWSExtraCacheParityProfileRef))
	}
	return ccGatewayCacheParityProfileRef(account)
}
