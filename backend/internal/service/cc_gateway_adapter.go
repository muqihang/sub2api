package service

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
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
	ccGatewayErrorKindHeader        = "x-cc-gateway-error-kind"
	ccGatewayErrorCodeHeader        = "x-cc-gateway-error-code"

	ccGatewayExtraEgressBucket       = "cc_gateway_egress_bucket"
	openAIGatewayExtraEgressFallback = "openai_gateway_egress_bucket"

	// First-wave shared-pool policy is pinned to the canonical Claude Code
	// 2.1.146 persona/version lock enforced by CC Gateway.
	ccGatewayAnthropicPolicyVersion = "2.1.146"
)

type ccGatewayAnthropicRoute string

const (
	ccGatewayRouteNativeMessages   ccGatewayAnthropicRoute = "native_messages"
	ccGatewayRouteNativeCountTokens ccGatewayAnthropicRoute = "native_count_tokens"
	ccGatewayRouteChatCompletions  ccGatewayAnthropicRoute = "chat_completions"
	ccGatewayRouteResponses        ccGatewayAnthropicRoute = "responses"
)

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
	return account != nil &&
		account.Platform == PlatformAnthropic &&
		(account.IsAnthropicOAuthOrSetupToken() || account.IsAnthropicAPIKeyPassthroughEnabled()) &&
		ccGatewayAnthropicEnabled(s.cfg)
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

func (s *GatewayService) selectCCGatewayAnthropicRoute(account *Account, route ccGatewayAnthropicRoute) (bool, error) {
	if !s.shouldUseCCGatewayAnthropic(account) {
		return false, nil
	}

	enabled, ok := parseCCGatewayBool(account.GetExtraString("cc_gateway_enabled"))
	if !ok || !enabled {
		return false, fmt.Errorf("cc gateway disabled or missing for anthropic account %d", account.ID)
	}
	canaryOnly, ok := parseCCGatewayBool(account.GetExtraString("cc_gateway_canary_only"))
	if !ok {
		return false, fmt.Errorf("cc gateway canary gate missing for account %d", account.ID)
	}
	if canaryOnly {
		return false, fmt.Errorf("cc gateway canary-only account %d is not eligible for broad routing", account.ID)
	}
	if account.Status != StatusActive || !account.Schedulable {
		return false, fmt.Errorf("cc gateway lifecycle ineligible for account %d", account.ID)
	}
	if version := strings.TrimSpace(account.GetExtraString("cc_gateway_policy_version")); version == "" || version != ccGatewayAnthropicPolicyVersion {
		return false, fmt.Errorf("cc gateway policy version mismatch for account %d", account.ID)
	}
	bucketEnabled, ok := parseCCGatewayBool(account.GetExtraString("cc_gateway_egress_bucket_enabled"))
	if !ok || !bucketEnabled {
		return false, fmt.Errorf("cc gateway egress bucket disabled or missing for account %d", account.ID)
	}
	if bucket := strings.TrimSpace(resolveCCGatewayEgressBucket(account, ccGatewayConfig(s.cfg).DefaultEgressBucket)); bucket == "" {
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
	ccg := ccGatewayConfig(s.cfg)
	return resolveCCGatewayEgressBucket(account, ccg.DefaultEgressBucket)
}

func resolveCCGatewayEgressBucket(account *Account, fallback string) string {
	if account != nil {
		if bucket := strings.TrimSpace(account.GetExtraString(ccGatewayExtraEgressBucket)); bucket != "" {
			return bucket
		}
		if bucket := strings.TrimSpace(account.GetExtraString(openAIGatewayExtraEgressFallback)); bucket != "" {
			return bucket
		}
	}
	if strings.TrimSpace(fallback) != "" {
		return strings.TrimSpace(fallback)
	}
	return "default"
}

func applyCCGatewayAnthropicHeaders(req *http.Request, cfg *config.Config, account *Account, tokenType string) {
	if req == nil || account == nil || cfg == nil {
		return
	}
	ccg := cfg.Gateway.CCGateway
	setHeaderRaw(req.Header, ccGatewayTokenHeader, strings.TrimSpace(ccg.Token))
	setHeaderRaw(req.Header, ccGatewayAccountIDHeader, strconv.FormatInt(account.ID, 10))
	setHeaderRaw(req.Header, ccGatewayProviderHeader, PlatformAnthropic)
	setHeaderRaw(req.Header, ccGatewayTokenTypeHeader, tokenType)
	setHeaderRaw(req.Header, "x-cc-policy-version", strings.TrimSpace(account.GetExtraString("cc_gateway_policy_version")))
	if email := ccGatewayAccountEmail(account); email != "" {
		setHeaderRaw(req.Header, ccGatewayAccountEmailHeader, email)
	}
	if accountUUID := strings.TrimSpace(account.GetExtraString("account_uuid")); accountUUID != "" {
		setHeaderRaw(req.Header, ccGatewayAccountUUIDHeader, accountUUID)
	}
	if orgUUID := strings.TrimSpace(account.GetExtraString("organization_uuid")); orgUUID != "" {
		setHeaderRaw(req.Header, ccGatewayOrganizationUUIDHeader, orgUUID)
	}
	setHeaderRaw(req.Header, ccGatewayEgressBucketHeader, resolveCCGatewayEgressBucket(account, ccg.DefaultEgressBucket))
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
	if p.ccGatewayAccountEmail != "" {
		req.Header.Set(ccGatewayAccountEmailHeader, strings.TrimSpace(p.ccGatewayAccountEmail))
	}
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
		resolveCCGatewayEgressBucket(account, ccg.DefaultEgressBucket),
		ccGatewayAccountEmail(account)
}
