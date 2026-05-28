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

	ccGatewayExtraEgressBucket       = "cc_gateway_egress_bucket"
	ccGatewayExtraPolicyVersion      = "cc_gateway_policy_version"
	openAIGatewayExtraEgressFallback = "openai_gateway_egress_bucket"

	ccGatewayExtraEnabled    = "cc_gateway_enabled"
	ccGatewayExtraCanaryOnly = "cc_gateway_canary_only"
	ccGatewayExtraBillingCCH = "billing_cch_mode"
)

type ccGatewayExplicitCanaryLocalOnlyContextKey struct{}

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
	return account != nil &&
		account.Platform == PlatformAnthropic &&
		(account.IsAnthropicOAuthOrSetupToken() || account.IsAnthropicAPIKeyPassthroughEnabled()) &&
		ccGatewayAnthropicEnabled(s.cfg)
}

// GetExplicitCCGatewayCanaryAccount returns a canary-only Anthropic account for
// tightly-scoped local/real canary requests. This intentionally bypasses normal
// broad scheduling, but only after the account and request control fields match.
func (s *GatewayService) GetExplicitCCGatewayCanaryAccount(ctx context.Context, accountID int64, egressBucket, billingMode string) (*Account, error) {
	if accountID <= 0 {
		return nil, fmt.Errorf("explicit canary account id is required")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("get explicit canary account: %w", err)
	}
	if err := validateExplicitCCGatewayCanaryAccount(s.cfg, account, egressBucket, billingMode); err != nil {
		return nil, err
	}
	return account, nil
}

func validateExplicitCCGatewayCanaryAccount(cfg *config.Config, account *Account, egressBucket, billingMode string) error {
	if account == nil {
		return fmt.Errorf("explicit canary account not found")
	}
	if account.Platform != PlatformAnthropic {
		return fmt.Errorf("explicit canary account must be anthropic")
	}
	if !account.IsAnthropicOAuthOrSetupToken() {
		return fmt.Errorf("explicit canary account must use anthropic oauth or setup-token")
	}
	if !ccGatewayAnthropicEnabled(cfg) {
		return fmt.Errorf("cc gateway anthropic runtime is not enabled")
	}
	if !ccGatewayBaseURLIsLocal(cfg) {
		return fmt.Errorf("explicit canary requires local cc gateway base url")
	}
	if !account.getExtraBool(ccGatewayExtraEnabled) {
		return fmt.Errorf("explicit canary account missing cc_gateway_enabled")
	}
	if !account.getExtraBool(ccGatewayExtraCanaryOnly) {
		return fmt.Errorf("explicit canary account must be canary-only")
	}
	if got, want := strings.TrimSpace(resolveCCGatewayEgressBucket(account, ccGatewayConfig(cfg).DefaultEgressBucket)), strings.TrimSpace(egressBucket); want == "" || got != want {
		return fmt.Errorf("explicit canary egress bucket mismatch")
	}
	if got, want := strings.TrimSpace(account.GetExtraString(ccGatewayExtraBillingCCH)), strings.TrimSpace(billingMode); want == "" || got != want {
		return fmt.Errorf("explicit canary billing mode mismatch")
	}
	if !strings.Contains(" "+account.GetCredential("scope")+" ", " user:inference ") {
		return fmt.Errorf("explicit canary account missing user:inference scope")
	}
	return nil
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

func applyCCGatewayAnthropicPolicyVersion(ctx context.Context, req *http.Request, account *Account) {
	if req == nil {
		return
	}
	if version := strings.TrimSpace(GetClaudeCodeVersion(ctx)); version != "" {
		setHeaderRaw(req.Header, ccGatewayPolicyVersionHeader, version)
		return
	}
	if account != nil {
		if version := strings.TrimSpace(account.GetExtraString(ccGatewayExtraPolicyVersion)); version != "" {
			setHeaderRaw(req.Header, ccGatewayPolicyVersionHeader, version)
		}
	}
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
