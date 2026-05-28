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
