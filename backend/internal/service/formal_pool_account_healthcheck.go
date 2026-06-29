package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const (
	formalPoolHealthcheckSeenHeader   = "X-CC-Gateway-Seen"
	formalPoolHealthcheckRawRefHeader = "X-CC-Gateway-Raw-Capture-Ref"
)

type FormalPoolGatewayHealthcheckRunner struct {
	accountRepo AccountRepository
	upstream    HTTPUpstream
	cfg         *config.Config
	quarantine  *AccountQuarantineService
}

func NewFormalPoolGatewayHealthcheckRunner(repo AccountRepository, upstream HTTPUpstream, cfg *config.Config, quarantine *AccountQuarantineService) *FormalPoolGatewayHealthcheckRunner {
	return &FormalPoolGatewayHealthcheckRunner{accountRepo: repo, upstream: upstream, cfg: cfg, quarantine: quarantine}
}

func (r *FormalPoolGatewayHealthcheckRunner) RunHealthcheck(ctx context.Context, input FormalPoolAcceptanceInput) (*FormalPoolAcceptanceResult, error) {
	result := &FormalPoolAcceptanceResult{
		Status:                         "failed_acceptance",
		AccountID:                      input.AccountID,
		AccountRef:                     input.AccountRef,
		ProxyRef:                       input.ProxyRef,
		EgressBucket:                   input.EgressBucket,
		PoolProfile:                    input.PoolProfile,
		Checks:                         []FormalPoolAcceptanceCheck{},
		NoRealMessagesRequestPerformed: false,
		ActivationRequired:             false,
	}
	add := func(name string, pass bool, msg string) {
		status := "pass"
		if !pass {
			status = "fail"
		}
		result.Checks = append(result.Checks, FormalPoolAcceptanceCheck{Name: name, Status: status, Message: msg})
	}
	if r == nil || r.accountRepo == nil || r.upstream == nil || r.cfg == nil || !ccGatewayAnthropicEnabled(r.cfg) {
		add("directed_healthcheck_runner", false, "cc gateway healthcheck runner unavailable")
		return result, nil
	}
	account, err := r.accountRepo.GetByID(ctx, input.AccountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, ErrAccountNotFound
	}
	result.AccountRef = ccGatewayAccountRef(account)
	result.EgressBucket = resolveCCGatewayEgressBucket(account)
	if result.ProxyRef == "" {
		result.ProxyRef = input.ProxyRef
	}
	if err := validateFormalPoolHealthcheckAccount(account, input); err != nil {
		add("directed_account_scope", false, err.Error())
		return result, nil
	}

	endpoint, err := r.ccGatewayMessagesURL()
	if err != nil {
		return nil, err
	}
	body, err := formalPoolHealthcheckBody(account)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("authorization", "Bearer "+account.GetCredential("access_token"))
	setHeaderRaw(req.Header, ccGatewayHealthcheckPersonaHeader, ccGatewayHealthcheckNon1MProfile)
	if err := applyCCGatewayAnthropicHeaders(req, r.cfg, account, "oauth"); err != nil {
		return nil, err
	}
	applyCCGatewayAnthropicPolicyVersion(ctx, req, account)
	if err := applyCCGatewayFormalPoolAttestationWithPersona(req, r.cfg, account, ccGatewayHealthcheckNon1MProfile); err != nil {
		return nil, err
	}

	resp, err := r.upstream.DoWithTLS(req, "", account.ID, account.Concurrency, nil)
	if err != nil {
		add("directed_healthcheck_request", false, "request failed")
		result.SafeErrorCode = "egress_proxy_failure"
		result.SafeErrorBucket = "proxy"
		_ = r.quarantineHardRisk(ctx, account.ID, RiskEventKindProxyMismatch, "egress_proxy_failure", 502)
		return result, nil
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	result.StatusCodeBucket = statusBucketFromHTTP(resp.StatusCode)
	result.CCGatewaySeen = headerTruthy(resp.Header.Get(formalPoolHealthcheckSeenHeader))
	result.RawCaptureRef = strings.TrimSpace(resp.Header.Get(formalPoolHealthcheckRawRefHeader))
	result.RawCapturePresent = isSafeLedgerRef(result.RawCaptureRef)
	result.FallbackDetected = headerTruthy(resp.Header.Get("X-CC-Gateway-Fallback-Detected")) || strings.Contains(strings.ToLower(string(responseBody)), "fallback")
	result.ProxyMismatch = headerTruthy(resp.Header.Get("X-CC-Gateway-Proxy-Mismatch"))
	result.RiskTextDetected = formalPoolRiskTextDetected(responseBody)
	result.SafeErrorCode, result.SafeErrorBucket = formalPoolHealthcheckSafeClassification(resp.StatusCode, responseBody, result)

	add("directed_healthcheck_status_200", resp.StatusCode == http.StatusOK, result.StatusCodeBucket)
	add("cc_gateway_seen", result.CCGatewaySeen, "cc gateway response evidence required")
	add("raw_capture_present", result.RawCapturePresent, "safe raw capture ref required")
	add("no_fallback", !result.FallbackDetected, "fallback must not occur")
	add("proxy_match", !result.ProxyMismatch, "proxy mismatch must not occur")
	add("no_risk_text", !result.RiskTextDetected, "risk text must not occur")

	shouldQuarantine := FormalPoolShouldQuarantineHTTPStatus(resp.StatusCode, responseBody) || result.FallbackDetected || result.ProxyMismatch || result.RiskTextDetected
	if resp.StatusCode == http.StatusTooManyRequests && !result.FallbackDetected && !result.ProxyMismatch && !result.RiskTextDetected {
		shouldQuarantine = false
	}
	if shouldQuarantine {
		kind, reason := formalPoolHealthcheckRiskKind(resp.StatusCode, responseBody, result)
		_ = r.quarantineHardRisk(ctx, account.ID, kind, reason, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusOK && result.CCGatewaySeen && result.RawCapturePresent {
		result.Status = FormalPoolOnboardingStatusHealthcheckPassed
	}
	if result.FormalPoolHealthcheckPassed() {
		result.Status = FormalPoolOnboardingStatusHealthcheckPassed
		result.ActivationRequired = true
	}
	return result, nil
}

func validateFormalPoolHealthcheckAccount(account *Account, input FormalPoolAcceptanceInput) error {
	if account == nil || !account.IsAnthropicOAuthOrSetupToken() || account.Platform != PlatformAnthropic {
		return fmt.Errorf("healthcheck requires anthropic oauth/setup-token account")
	}
	if input.AccountID > 0 && input.AccountID != account.ID {
		return fmt.Errorf("healthcheck account id mismatch")
	}
	if ref := strings.TrimSpace(input.AccountRef); ref != "" && ref != ccGatewayAccountRef(account) {
		return fmt.Errorf("healthcheck account ref mismatch")
	}
	if bucket := strings.TrimSpace(input.EgressBucket); bucket != "" && bucket != resolveCCGatewayEgressBucket(account) {
		return fmt.Errorf("healthcheck egress bucket mismatch")
	}
	if strings.TrimSpace(account.GetCredential("access_token")) == "" {
		return fmt.Errorf("healthcheck requires access token")
	}
	if err := validateAnthropicMessagesInferenceScope(account); err != nil {
		return err
	}
	enabled, ok := parseCCGatewayBool(account.GetExtraString("cc_gateway_enabled"))
	if !ok || !enabled {
		return fmt.Errorf("cc gateway disabled or missing for account %d", account.ID)
	}
	canaryOnly, ok := parseCCGatewayBool(account.GetExtraString("cc_gateway_canary_only"))
	if !ok || canaryOnly {
		return fmt.Errorf("cc gateway broad route gate missing for account %d", account.ID)
	}
	if version := strings.TrimSpace(account.GetExtraString(ccGatewayExtraPolicyVersion)); version == "" || !ccGatewayPolicyVersionCompatible(version) {
		return fmt.Errorf("cc gateway policy version mismatch for account %d", account.ID)
	}
	bucketEnabled, ok := parseCCGatewayBool(account.GetExtraString(ccGatewayExtraEgressBucketEnabled))
	if !ok || !bucketEnabled || strings.TrimSpace(resolveCCGatewayEgressBucket(account)) == "" {
		return fmt.Errorf("cc gateway egress bucket disabled or missing for account %d", account.ID)
	}
	allowSet := parseCCGatewayRouteSet(account.GetExtraString("cc_gateway_routes"))
	if _, allowed := allowSet[string(ccGatewayRouteNativeMessages)]; !allowed {
		return fmt.Errorf("cc gateway native messages route not allowed for account %d", account.ID)
	}
	return nil
}

func (r *FormalPoolGatewayHealthcheckRunner) ccGatewayMessagesURL() (string, error) {
	base := strings.TrimRight(strings.TrimSpace(r.cfg.Gateway.CCGateway.BaseURL), "/")
	if base == "" {
		return "", fmt.Errorf("cc gateway base url missing")
	}
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("invalid cc gateway base url")
	}
	return base + "/v1/messages?beta=true", nil
}

const formalPoolHealthcheckModel = "claude-haiku-4-5-20251001"

func formalPoolHealthcheckBody(account ...*Account) ([]byte, error) {
	sessionID := formalPoolHealthcheckSessionID(nil)
	if len(account) > 0 {
		sessionID = formalPoolHealthcheckSessionID(account[0])
	}
	return json.Marshal(map[string]any{
		"model":      formalPoolHealthcheckModel,
		"max_tokens": 64,
		"metadata":   map[string]any{"user_id": fmt.Sprintf(`{"device_id":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","session_id":"%s"}`, sessionID)},
		"stream":     false,
		"system": []map[string]any{
			{"type": "text", "text": "<env>\nPlatform: darwin\nShell: zsh\nOS Version: Darwin 24.4.0\nWorking directory: /tmp/formal-pool-healthcheck\n</env>"},
			{"type": "text", "text": "You are Claude Code, Anthropic's official CLI for software engineering tasks. This is a formal-pool directed healthcheck using the lowest-cost Claude Code model. Keep the response minimal and do not inspect files or call tools."},
		},
		"messages": []map[string]any{{"role": "user", "content": []map[string]any{{"type": "text", "text": "Return a compact healthcheck JSON object with ok true."}}}},
		"tools":    []map[string]any{},
	})
}

func formalPoolHealthcheckSessionID(account *Account) string {
	seed := "formal-pool-healthcheck"
	if account != nil {
		parts := []string{
			strings.TrimSpace(ccGatewayAccountRef(account)),
			strings.TrimSpace(resolveCCGatewayEgressBucket(account)),
			strings.TrimSpace(ccGatewayProxyIdentityRef(account)),
		}
		joined := strings.Join(parts, "|")
		if strings.Trim(joined, "|") != "" {
			seed = "formal-pool-healthcheck|" + joined
		}
	}
	return claudeCodeUUIDLikeFromDigest(scopedStickyHMACBytes("formal_pool_healthcheck_session", seed))
}

func headerTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "seen", "present":
		return true
	default:
		return false
	}
}

func formalPoolRiskTextDetected(body []byte) bool {
	msg := strings.ToLower(extractUpstreamErrorMessage(body) + " " + string(body))
	return strings.Contains(msg, "unusual activity") || strings.Contains(msg, "account on hold") || strings.Contains(msg, "account is on hold") || strings.Contains(msg, "kyc") || strings.Contains(msg, "risk")
}

func formalPoolHealthcheckSafeClassification(status int, body []byte, result *FormalPoolAcceptanceResult) (string, string) {
	msg := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(body) + " " + string(body)))
	if result != nil {
		if result.ProxyMismatch || strings.Contains(msg, "proxy_mismatch") || strings.Contains(msg, "proxy mismatch") {
			return "proxy_mismatch", "proxy"
		}
		if result.FallbackDetected || strings.Contains(msg, "fallback") {
			return "fallback", "fallback"
		}
	}
	if strings.Contains(msg, "missing_account_identity") || strings.Contains(msg, "missing_identity") {
		return "missing_account_identity", "cc_gateway"
	}
	if strings.Contains(msg, "egress_proxy_failure") {
		return "egress_proxy_failure", "proxy"
	}
	if strings.Contains(msg, "account is on hold") || strings.Contains(msg, "account on hold") {
		return "account_on_hold", "hold"
	}
	if formalPoolRiskTextDetected(body) {
		return "risk_text", "risk"
	}
	switch status {
	case http.StatusUnauthorized:
		if isInvalidGrantText(msg) || strings.Contains(msg, "invalid_grant") {
			return "invalid_grant", "auth"
		}
		return "auth", "auth"
	case http.StatusForbidden:
		return "forbidden", "auth"
	case http.StatusTooManyRequests:
		if formalPoolLongContextUsageCreditsText(msg) {
			return "long_context_usage_credits", "long_context"
		}
		return "rate_limited", "rate_limited"
	}
	if result != nil {
		if !result.RawCapturePresent {
			return "raw_capture_missing", "raw_capture"
		}
		if !result.CCGatewaySeen {
			return "cc_gateway_not_seen", "cc_gateway"
		}
	}
	return "unknown", "unknown"
}

func formalPoolLongContextUsageCreditsText(msg string) bool {
	msg = strings.ToLower(strings.TrimSpace(msg))
	return strings.Contains(msg, "long context") && (strings.Contains(msg, "usage credit") || strings.Contains(msg, "usage_credits"))
}

func formalPoolHealthcheckRiskKind(status int, body []byte, result *FormalPoolAcceptanceResult) (string, string) {
	if result != nil {
		if result.ProxyMismatch {
			return RiskEventKindProxyMismatch, "proxy_mismatch"
		}
		if result.FallbackDetected {
			return RiskEventKindFallback, "fallback_detected"
		}
		if result.RiskTextDetected {
			return RiskEventKindRiskText, "risk_text"
		}
	}
	if status == http.StatusUnauthorized {
		return RiskEventKindIdentityBoundaryFail, "invalid_auth"
	}
	if status == http.StatusForbidden {
		return RiskEventKindIdentityBoundaryFail, "forbidden"
	}
	msg := strings.ToLower(extractUpstreamErrorMessage(body) + " " + string(body))
	if strings.Contains(msg, "egress_proxy_failure") {
		return RiskEventKindProxyMismatch, "egress_proxy_failure"
	}
	if strings.Contains(msg, "missing_account_identity") || strings.Contains(msg, "missing_identity") {
		return RiskEventKindIdentityBoundaryFail, "missing_account_identity"
	}
	return RiskEventKindIdentityBoundaryFail, "healthcheck_failed"
}

func (r *FormalPoolGatewayHealthcheckRunner) quarantineHardRisk(ctx context.Context, accountID int64, kind, reason string, status int) error {
	quarantine := r.quarantine
	if quarantine == nil {
		quarantine = NewAccountQuarantineService(r.accountRepo, nil)
	}
	_, err := quarantine.Quarantine(ctx, AccountQuarantineInput{AccountID: accountID, Kind: kind, Reason: reason, Source: "formal_pool_healthcheck", StatusCode: status})
	return err
}
