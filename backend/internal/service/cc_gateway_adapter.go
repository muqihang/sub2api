package service

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/tidwall/gjson"
)

const (
	ccGatewayTokenHeader            = "x-cc-gateway-token"
	ccGatewayAccountIDHeader        = "x-cc-account-id"
	ccGatewayProviderHeader         = "x-cc-provider"
	ccGatewayTokenTypeHeader        = "x-cc-token-type"
	ccGatewayAccountEmailHeader     = "x-cc-account-email"
	ccGatewayAccountUUIDHeader      = "x-cc-account-uuid"
	ccGatewayOrganizationUUIDHeader = "x-cc-organization-uuid"
	ccGatewayProjectIDHeader        = "x-cc-project-id"
	ccGatewayEgressBucketHeader     = "x-cc-egress-bucket"
	ccGatewayPolicyVersionHeader    = "x-cc-policy-version"
	ccGatewayTrustedPersonaHeader   = "x-sub2api-persona-trusted"
	ccGatewayErrorKindHeader        = "x-cc-gateway-error-kind"
	ccGatewayErrorCodeHeader        = "x-cc-gateway-error-code"

	ccGatewayExtraEgressBucket        = "cc_gateway_egress_bucket"
	ccGatewayExtraEgressBucketEnabled = "cc_gateway_egress_bucket_enabled"
	ccGatewayExtraPolicyVersion       = "cc_gateway_policy_version"
	ccGatewayExtraAccountRef          = "cc_gateway_account_ref"
	openAIGatewayExtraEgressFallback  = "openai_gateway_egress_bucket"

	ccGatewayExtraEnabled    = "cc_gateway_enabled"
	ccGatewayExtraCanaryOnly = "cc_gateway_canary_only"
	ccGatewayExtraBillingCCH = "billing_cch_mode"

	// First-wave shared-pool policy stays anchored to the verified Claude Code
	// 2.1.150 registry profile. Same-minor CLI-through drift (for example
	// 2.1.151) is still forwarded to CC Gateway for source-of-truth resolver
	// handling; unknown newer minors/majors stay fail-closed at the resolver.
	ccGatewayAnthropicPolicyVersion = "2.1.150"
)

var ccGatewayVersionRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)

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
		if s.hasExplicitCCGatewayAnthropicConfig(account) {
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
	if account.Status != StatusActive || (!explicitCanary && !account.IsSchedulable()) {
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

func applyCCGatewayAnthropicHeaders(req *http.Request, cfg *config.Config, account *Account, tokenType string) {
	if req == nil || account == nil || cfg == nil {
		return
	}
	ccg := cfg.Gateway.CCGateway
	setHeaderRaw(req.Header, ccGatewayTokenHeader, strings.TrimSpace(ccg.Token))
	setHeaderRaw(req.Header, ccGatewayAccountIDHeader, ccGatewayAccountRef(account))
	setHeaderRaw(req.Header, ccGatewayProviderHeader, PlatformAnthropic)
	setHeaderRaw(req.Header, ccGatewayTokenTypeHeader, tokenType)
	setHeaderRaw(req.Header, ccGatewayPolicyVersionHeader, strings.TrimSpace(account.GetExtraString(ccGatewayExtraPolicyVersion)))
	// Formal shared-pool Anthropic routing must not send raw email/account/org
	// identity headers to CC Gateway. Account identity is selected by the
	// server-owned x-cc-account-id ref and CC Gateway account_identities config.
	setHeaderRaw(req.Header, ccGatewayEgressBucketHeader, resolveCCGatewayEgressBucket(account))
	applyCCGatewayClaudeCodeSessionMapping(req, account)
}

func applyCCGatewayAnthropicPolicyVersion(ctx context.Context, req *http.Request, account *Account) {
	if req == nil {
		return
	}
	trustedPersona := ccGatewayTrustedPersonaContext(ctx)
	if trustedPersona {
		setHeaderRaw(req.Header, ccGatewayTrustedPersonaHeader, "1")
		if version := strings.TrimSpace(GetClaudeCodeVersion(ctx)); version != "" {
			setHeaderRaw(req.Header, ccGatewayPolicyVersionHeader, version)
			return
		}
	}
	if account != nil {
		if version := strings.TrimSpace(account.GetExtraString(ccGatewayExtraPolicyVersion)); version != "" {
			setHeaderRaw(req.Header, ccGatewayPolicyVersionHeader, version)
		}
	}
}

func ccGatewayTrustedPersonaContext(ctx context.Context) bool {
	_, ok := GetCCGatewayExplicitCanaryRequest(ctx)
	return ok
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
	normalized := strings.TrimSpace(version)
	if normalized == ccGatewayAnthropicPolicyVersion {
		return true
	}
	base := ccGatewayVersionRe.FindStringSubmatch(ccGatewayAnthropicPolicyVersion)
	candidate := ccGatewayVersionRe.FindStringSubmatch(normalized)
	if len(base) != 4 || len(candidate) != 4 {
		return false
	}
	return base[1] == candidate[1] && base[2] == candidate[2]
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
		if code := strings.TrimSpace(resp.Header.Get(ccGatewayErrorCodeHeader)); code != "" {
			return code
		}
	}
	if code := strings.TrimSpace(gjson.GetBytes(body, "error.code").String()); code != "" {
		return code
	}
	return "unknown_control_plane"
}

func ccGatewayControlPlaneMessage(body []byte) string {
	if msg := strings.TrimSpace(gjson.GetBytes(body, "error.message").String()); msg != "" {
		return sanitizeUpstreamErrorMessage(msg)
	}
	return "CC Gateway control-plane rejected request"
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
