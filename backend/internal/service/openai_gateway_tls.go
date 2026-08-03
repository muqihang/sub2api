package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/model"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

const (
	OpenAIGatewayTLSExtraKey = "openai_gateway_tls"

	openAIGatewayTLSSourceDisabled        = "disabled"
	openAIGatewayTLSSourceBucket          = "bucket"
	openAIGatewayTLSSourceAccountOverride = "account_override"
	openAIGatewayTLSSourceCanaryOverride  = "canary_override"
	openAIGatewayTLSSourceDefaultFallback = "default_fallback"
	openAIGatewayTLSSourcePlainFallback   = "plain_fallback"

	openAIGatewayDefaultTLSProfileName = "Built-in Default (Node.js 24.x)"
)

type openAIGatewayTLSCanaryProfileIDContextKey struct{}

func WithOpenAIGatewayTLSCanaryProfileID(ctx context.Context, profileID int64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if profileID <= 0 {
		return ctx
	}
	return context.WithValue(ctx, openAIGatewayTLSCanaryProfileIDContextKey{}, profileID)
}

func OpenAIGatewayTLSCanaryProfileID(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	profileID, _ := ctx.Value(openAIGatewayTLSCanaryProfileIDContextKey{}).(int64)
	if profileID < 0 {
		return 0
	}
	return profileID
}

type OpenAIGatewayTLSProfileValidationError struct {
	Code       string
	BucketName string
	ProfileID  int64
	Message    string
	Err        error
}

func (e *OpenAIGatewayTLSProfileValidationError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *OpenAIGatewayTLSProfileValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type OpenAIGatewayEffectiveTLS struct {
	Enabled        bool                    `json:"enabled"`
	ProfileID      int64                   `json:"profile_id,omitempty"`
	ProfileName    string                  `json:"profile_name,omitempty"`
	ProfileHash    string                  `json:"profile_hash,omitempty"`
	Source         string                  `json:"source"`
	FallbackReason string                  `json:"fallback_reason,omitempty"`
	CacheIdentity  string                  `json:"cache_identity"`
	HTTPApplicable bool                    `json:"http_applicable"`
	WSApplicable   bool                    `json:"ws_applicable"`
	Profile        *tlsfingerprint.Profile `json:"-"`
}

func ValidateOpenAIGatewayAccountTLSPolicyShape(policy *OpenAIGatewayAccountTLSPolicy) error {
	if policy == nil {
		return nil
	}
	if policy.ProfileID < 0 {
		return infraerrors.BadRequest("INVALID_OPENAI_GATEWAY_TLS", "openai_gateway_tls.profile_id must be >= 0")
	}
	if policy.Enabled && policy.ProfileID <= 0 {
		return infraerrors.BadRequest("INVALID_OPENAI_GATEWAY_TLS", "openai_gateway_tls.profile_id is required when enabled is true")
	}
	return nil
}

func (s *OpenAIGatewayCoreService) ValidateAccountTLSPolicyUpdate(ctx context.Context, account *Account, extra map[string]any, policy *OpenAIGatewayAccountTLSPolicy) error {
	_ = ctx
	if err := ValidateOpenAIGatewayAccountTLSPolicyShape(policy); err != nil {
		return err
	}
	if policy == nil || s == nil || s.cfg == nil {
		return nil
	}
	if policy.ProfileID > 0 && s.tlsProfileService != nil {
		profile := s.tlsProfileService.GetProfileModelByID(policy.ProfileID)
		if profile == nil {
			return infraerrors.BadRequest("INVALID_OPENAI_GATEWAY_TLS", fmt.Sprintf("openai_gateway_tls.profile_id %d not found", policy.ProfileID))
		}
		if err := profile.Validate(); err != nil {
			return infraerrors.BadRequest("INVALID_OPENAI_GATEWAY_TLS", fmt.Sprintf("openai_gateway_tls.profile_id %d invalid", policy.ProfileID)).WithCause(err)
		}
	}
	if !policy.Enabled {
		return nil
	}
	if account == nil {
		account = &Account{}
	}
	target := *account
	target.Extra = extra
	bucketName := s.ResolveEgressBucket(&target)
	bucket, ok := s.findEgressBucket(bucketName)
	if !ok {
		return infraerrors.BadRequest("INVALID_OPENAI_GATEWAY_TLS", fmt.Sprintf("openai_gateway_tls target egress bucket %q not found", bucketName))
	}
	if !bucket.TLS.AllowAccountOverride {
		return infraerrors.BadRequest("INVALID_OPENAI_GATEWAY_TLS", fmt.Sprintf("openai_gateway_tls account override is not allowed for egress bucket %q", bucketName))
	}
	return nil
}

func (s *OpenAIGatewayCoreService) ValidateConfiguredTLSProfiles(ctx context.Context) error {
	_ = ctx
	if s == nil || s.cfg == nil || !s.cfg.Gateway.OpenAICore.TLSBinding.Enabled {
		return nil
	}
	for _, bucket := range s.cfg.Gateway.OpenAICore.EgressBuckets {
		name := strings.TrimSpace(bucket.Name)
		if !bucket.Enabled || !bucket.TLS.Enabled || bucket.TLS.ProfileID <= 0 {
			continue
		}
		if s.tlsProfileService == nil {
			return &OpenAIGatewayTLSProfileValidationError{
				Code:       "tls_profile_service_not_configured",
				BucketName: name,
				ProfileID:  bucket.TLS.ProfileID,
				Message:    fmt.Sprintf("gateway.openai_core.egress_buckets[%s].tls.profile_id %d cannot be validated: tls profile service not configured", name, bucket.TLS.ProfileID),
			}
		}
		profile := s.tlsProfileService.GetProfileModelByID(bucket.TLS.ProfileID)
		if profile == nil {
			return &OpenAIGatewayTLSProfileValidationError{
				Code:       "tls_profile_not_found",
				BucketName: name,
				ProfileID:  bucket.TLS.ProfileID,
				Message:    fmt.Sprintf("gateway.openai_core.egress_buckets[%s].tls.profile_id %d not found", name, bucket.TLS.ProfileID),
			}
		}
		if err := profile.Validate(); err != nil {
			return &OpenAIGatewayTLSProfileValidationError{
				Code:       "tls_profile_invalid",
				BucketName: name,
				ProfileID:  bucket.TLS.ProfileID,
				Message:    fmt.Sprintf("gateway.openai_core.egress_buckets[%s].tls.profile_id %d invalid", name, bucket.TLS.ProfileID),
				Err:        err,
			}
		}
	}
	return nil
}

func (s *OpenAIGatewayCoreService) ResolveEffectiveTLS(ctx context.Context, account *Account, egress *OpenAIEgressResolution, transport OpenAIClientTransport) (*OpenAIGatewayEffectiveTLS, error) {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.OpenAICore.TLSBinding.Enabled {
		return s.buildPlainTLS(account, egress, transport, openAIGatewayTLSSourceDisabled, ""), nil
	}
	if egress == nil {
		return nil, &OpenAIEgressPolicyError{Code: "tls_egress_missing", BucketName: ""}
	}
	bucket, ok := s.findEgressBucket(egress.BucketName)
	if !ok {
		return nil, &OpenAIEgressPolicyError{Code: "tls_bucket_missing", BucketName: egress.BucketName}
	}
	tlsCfg := bucket.TLS
	if !tlsCfg.Enabled {
		if s.cfg.Gateway.OpenAICore.ProductionMode {
			return nil, &OpenAIEgressPolicyError{Code: "tls_policy_missing", BucketName: egress.BucketName}
		}
		return s.buildPlainTLS(account, egress, transport, openAIGatewayTLSSourcePlainFallback, "bucket_tls_disabled"), nil
	}

	if canaryProfileID := OpenAIGatewayTLSCanaryProfileID(ctx); canaryProfileID > 0 {
		effectiveTLS, err := s.buildProfileTLS(canaryProfileID, openAIGatewayTLSSourceCanaryOverride, "", egress, transport)
		return s.applyOpenAIWSStrategyToEffectiveTLS(account, egress, effectiveTLS, transport), err
	}

	override := account.GetOpenAIGatewayTLSOverride()
	var effectiveTLS *OpenAIGatewayEffectiveTLS
	var err error
	if tlsCfg.AllowAccountOverride && override.Enabled && override.ProfileID > 0 {
		effectiveTLS, err = s.buildProfileTLS(override.ProfileID, openAIGatewayTLSSourceAccountOverride, "", egress, transport)
		return s.applyOpenAIWSStrategyToEffectiveTLS(account, egress, effectiveTLS, transport), err
	}
	if tlsCfg.ProfileID > 0 {
		effectiveTLS, err = s.buildProfileTLS(tlsCfg.ProfileID, openAIGatewayTLSSourceBucket, "", egress, transport)
		return s.applyOpenAIWSStrategyToEffectiveTLS(account, egress, effectiveTLS, transport), err
	}
	if tlsCfg.AllowDefaultFallback {
		effectiveTLS = s.buildDefaultTLS(egress, transport, "bucket_profile_unset")
		return s.applyOpenAIWSStrategyToEffectiveTLS(account, egress, effectiveTLS, transport), nil
	}
	if tlsCfg.AllowPlainFallback {
		effectiveTLS = s.buildPlainTLS(account, egress, transport, openAIGatewayTLSSourcePlainFallback, "bucket_profile_unset")
		return s.applyOpenAIWSStrategyToEffectiveTLS(account, egress, effectiveTLS, transport), nil
	}
	return nil, &OpenAIEgressPolicyError{Code: "tls_policy_no_effective_profile", BucketName: egress.BucketName}
}

func (s *OpenAIGatewayCoreService) BuildTLSCanarySnapshot(ctx context.Context, accountID int64, bucketName string, route string, headers http.Header, transport OpenAIClientTransport) (*OpenAIGatewayTLSCanarySnapshot, error) {
	return s.BuildTLSCanarySnapshotWithProfileOverride(ctx, accountID, bucketName, route, headers, transport, 0)
}

func (s *OpenAIGatewayCoreService) BuildTLSCanarySnapshotWithProfileOverride(ctx context.Context, accountID int64, bucketName string, route string, headers http.Header, transport OpenAIClientTransport, tlsProfileID int64) (*OpenAIGatewayTLSCanarySnapshot, error) {
	if s == nil || s.accountRepo == nil {
		return nil, ErrAccountNotFound
	}
	if tlsProfileID > 0 {
		ctx = WithOpenAIGatewayTLSCanaryProfileID(ctx, tlsProfileID)
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	target := *account
	if !target.IsOpenAI() {
		return nil, ErrAccountNotFound
	}
	if trimmedBucket := strings.TrimSpace(bucketName); trimmedBucket != "" {
		target.Extra = mergeMap(target.Extra, map[string]any{"openai_gateway_egress_bucket": trimmedBucket})
	}

	client, err := s.AuthenticateClientHeaders(headers)
	if err != nil {
		return nil, err
	}
	profile, _ := s.resolveCanonicalProfile(&target, headers)
	egress, err := s.ResolveEgress(ctx, &target, resolveOpenAIAccountProxyURL(&target))
	if err != nil {
		return nil, err
	}
	effectiveTLS, err := s.ResolveEffectiveTLS(ctx, &target, egress, transport)
	if err != nil {
		return nil, err
	}
	canaryHeaders := cloneHeader(headers)
	if transport == OpenAIClientTransportWS && target.GetChatGPTAccountID() != "" {
		if canaryHeaders == nil {
			canaryHeaders = http.Header{}
		}
		canaryHeaders.Set("chatgpt-account-id", target.GetChatGPTAccountID())
	}
	sendMethod := openAIGatewayTLSCanarySendMethod(transport, canaryHeaders, egress, effectiveTLS)
	diagnostics := map[string]string{
		"tls_source": strings.TrimSpace(effectiveTLS.Source),
	}
	if effectiveTLS.CacheIdentity != "" {
		diagnostics["cache_identity"] = effectiveTLS.CacheIdentity
	}
	if effectiveTLS.ProfileHash != "" {
		diagnostics["profile_hash"] = effectiveTLS.ProfileHash
	}
	if effectiveTLS.FallbackReason != "" {
		diagnostics["fallback_reason"] = effectiveTLS.FallbackReason
	}
	if transport == OpenAIClientTransportWS {
		for key, value := range openAIWSDialerStrategyDiagnostics(canaryHeaders, egress.ProxyURL, effectiveTLS) {
			diagnostics[key] = value
		}
	}
	debugProxyURL := ""
	if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAICore.ExposeRawProxyInDebug {
		debugProxyURL = egress.ProxyURL
	}
	return &OpenAIGatewayTLSCanarySnapshot{
		AccountID:           target.ID,
		AccountName:         target.Name,
		Bucket:              strings.TrimSpace(bucketName),
		EgressBucket:        egress.BucketName,
		Route:               strings.TrimSpace(route),
		ProxySelected:       egress.ProxySelected,
		ProxyLabel:          egress.ProxyLabel,
		ProxyHash:           egress.ProxyHash,
		DebugProxyURL:       debugProxyURL,
		Transport:           string(transport),
		TLS:                 effectiveTLS,
		EffectiveSendMethod: sendMethod,
		Success:             false,
		FailureReason:       "static_decision_only",
		Probe: &OpenAIGatewayTLSCanaryProbe{
			Mode:          "static_decision",
			Transport:     string(transport),
			Route:         strings.TrimSpace(route),
			FailureReason: "live_probe_runtime_unavailable",
		},
		Diagnostics: diagnostics,
		Client:      client,
		Profile:     profile,
	}, nil
}

func (s *OpenAIGatewayCoreService) applyOpenAIWSStrategyToEffectiveTLS(account *Account, egress *OpenAIEgressResolution, effectiveTLS *OpenAIGatewayEffectiveTLS, transport OpenAIClientTransport) *OpenAIGatewayEffectiveTLS {
	if transport != OpenAIClientTransportWS || effectiveTLS == nil {
		return effectiveTLS
	}
	headers := http.Header{}
	if account != nil {
		if chatgptAccountID := account.GetChatGPTAccountID(); chatgptAccountID != "" {
			headers.Set("chatgpt-account-id", chatgptAccountID)
		}
	}
	proxyURL := ""
	if egress != nil {
		proxyURL = egress.ProxyURL
	}
	diagnostics := openAIWSDialerStrategyDiagnostics(headers, proxyURL, effectiveTLS)
	if diagnostics["ws_transport_supported"] == "true" {
		return effectiveTLS
	}
	copied := *effectiveTLS
	copied.WSApplicable = false
	if reason := strings.TrimSpace(diagnostics["ws_transport_unsupported_reason"]); reason != "" {
		copied.FallbackReason = reason
	}
	return &copied
}

func openAIGatewayTLSCanarySendMethod(transport OpenAIClientTransport, headers http.Header, egress *OpenAIEgressResolution, effectiveTLS *OpenAIGatewayEffectiveTLS) string {
	if transport == OpenAIClientTransportWS {
		diagnostics := openAIWSDialerStrategyDiagnostics(headers, egressProxyURL(egress), effectiveTLS)
		switch diagnostics["ws_dialer_strategy"] {
		case "coder_custom_http_client":
			return "WSCoderCustomHTTPClient"
		case "gorilla_fallback":
			return "WSGorillaFallback"
		default:
			return "WSCoderDefault"
		}
	}
	if effectiveTLS != nil && effectiveTLS.Enabled && effectiveTLS.HTTPApplicable && effectiveTLS.Profile != nil {
		return "DoWithTLS"
	}
	return "Do"
}

func egressProxyURL(egress *OpenAIEgressResolution) string {
	if egress == nil {
		return ""
	}
	return egress.ProxyURL
}

func (s *OpenAIGatewayCoreService) BuildTLSBindingSnapshot() *OpenAIGatewayTLSBindingSnapshot {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.OpenAICore.TLSBinding.Enabled {
		return nil
	}
	snapshot := &OpenAIGatewayTLSBindingSnapshot{
		Enabled: true,
		Buckets: map[string]*OpenAIGatewayEffectiveTLS{},
		Summary: &OpenAIGatewayTLSBindingSummarySnapshot{
			ProfileUsage: map[string]int64{},
		},
	}
	for _, bucket := range s.cfg.Gateway.OpenAICore.EgressBuckets {
		name := strings.TrimSpace(bucket.Name)
		if name == "" {
			continue
		}
		egress := buildOpenAIEgressResolutionWithTLS(name, bucket.ProxyURL, openAIEgressSourceBucket, buildOpenAIEgressTLSView(bucket.TLS))
		tlsCfg := bucket.TLS
		switch {
		case !tlsCfg.Enabled:
			snapshot.Buckets[name] = s.buildPlainTLS(nil, egress, OpenAIClientTransportUnknown, openAIGatewayTLSSourcePlainFallback, "bucket_tls_disabled")
		case tlsCfg.ProfileID > 0:
			tls, err := s.buildProfileTLS(tlsCfg.ProfileID, openAIGatewayTLSSourceBucket, "", egress, OpenAIClientTransportUnknown)
			if err != nil {
				snapshot.Buckets[name] = s.buildPlainTLS(nil, egress, OpenAIClientTransportUnknown, "error", err.Error())
			} else {
				snapshot.Buckets[name] = tls
			}
		case tlsCfg.AllowDefaultFallback:
			snapshot.Buckets[name] = s.buildDefaultTLS(egress, OpenAIClientTransportUnknown, "bucket_profile_unset")
		case tlsCfg.AllowPlainFallback:
			snapshot.Buckets[name] = s.buildPlainTLS(nil, egress, OpenAIClientTransportUnknown, openAIGatewayTLSSourcePlainFallback, "bucket_profile_unset")
		default:
			snapshot.Buckets[name] = s.buildPlainTLS(nil, egress, OpenAIClientTransportUnknown, "error", "tls_policy_no_effective_profile")
		}
	}
	return snapshot
}

func (s *OpenAIGatewayTLSBindingSnapshot) recordAccountStart() {
	if s == nil {
		return
	}
	if s.Summary == nil {
		s.Summary = &OpenAIGatewayTLSBindingSummarySnapshot{}
	}
	if s.Summary.ProfileUsage == nil {
		s.Summary.ProfileUsage = map[string]int64{}
	}
	s.Summary.AccountsTotal++
}

func (s *OpenAIGatewayTLSBindingSnapshot) recordFailClosed() {
	if s == nil {
		return
	}
	if s.Summary == nil {
		s.Summary = &OpenAIGatewayTLSBindingSummarySnapshot{}
	}
	s.Summary.FailClosedAccounts++
}

func (s *OpenAIGatewayTLSBindingSnapshot) recordEffectiveTLS(tls *OpenAIGatewayEffectiveTLS) {
	if s == nil || tls == nil {
		return
	}
	if s.Summary == nil {
		s.Summary = &OpenAIGatewayTLSBindingSummarySnapshot{}
	}
	if s.Summary.ProfileUsage == nil {
		s.Summary.ProfileUsage = map[string]int64{}
	}
	switch strings.TrimSpace(tls.Source) {
	case openAIGatewayTLSSourceBucket, openAIGatewayTLSSourceAccountOverride:
		s.Summary.BoundAccounts++
	case openAIGatewayTLSSourceDefaultFallback:
		s.Summary.DefaultFallbackAccounts++
	case openAIGatewayTLSSourcePlainFallback:
		s.Summary.PlainFallbackAccounts++
	case openAIGatewayTLSSourceDisabled:
		s.Summary.DisabledAccounts++
	default:
		if tls.Enabled {
			s.Summary.BoundAccounts++
		}
	}
	if !tls.Enabled {
		return
	}
	switch {
	case tls.ProfileID > 0:
		s.Summary.ProfileUsage[fmt.Sprintf("profile_id:%d", tls.ProfileID)]++
	case strings.TrimSpace(tls.Source) == openAIGatewayTLSSourceDefaultFallback:
		s.Summary.ProfileUsage["builtin_default"]++
	case strings.TrimSpace(tls.ProfileHash) != "":
		s.Summary.ProfileUsage["profile_hash:"+strings.TrimSpace(tls.ProfileHash)]++
	}
}

func (s *OpenAIGatewayCoreService) buildProfileTLS(profileID int64, source, fallbackReason string, egress *OpenAIEgressResolution, transport OpenAIClientTransport) (*OpenAIGatewayEffectiveTLS, error) {
	_ = transport
	if s == nil || s.tlsProfileService == nil {
		return nil, &OpenAIEgressPolicyError{Code: "tls_profile_service_not_configured", BucketName: egressBucketName(egress)}
	}
	profile := s.tlsProfileService.GetProfileModelByID(profileID)
	if profile == nil {
		return nil, &OpenAIEgressPolicyError{Code: "tls_profile_not_found", BucketName: egressBucketName(egress)}
	}
	if err := profile.Validate(); err != nil {
		return nil, &OpenAIEgressPolicyError{Code: "tls_profile_invalid", BucketName: egressBucketName(egress)}
	}
	profileHash := hashOpenAITLSProfile(profile)
	return &OpenAIGatewayEffectiveTLS{
		Enabled:        true,
		ProfileID:      profile.ID,
		ProfileName:    strings.TrimSpace(profile.Name),
		ProfileHash:    profileHash,
		Source:         source,
		FallbackReason: fallbackReason,
		CacheIdentity:  buildOpenAITLSCacheIdentity(egress, profileHash, source),
		HTTPApplicable: true,
		WSApplicable:   true,
		Profile:        profile.ToTLSProfile(),
	}, nil
}

func (s *OpenAIGatewayCoreService) buildDefaultTLS(egress *OpenAIEgressResolution, transport OpenAIClientTransport, fallbackReason string) *OpenAIGatewayEffectiveTLS {
	_ = transport
	profile := &tlsfingerprint.Profile{Name: openAIGatewayDefaultTLSProfileName}
	profileHash := hashOpenAITLSProfileName(profile.Name)
	return &OpenAIGatewayEffectiveTLS{
		Enabled:        true,
		ProfileName:    profile.Name,
		ProfileHash:    profileHash,
		Source:         openAIGatewayTLSSourceDefaultFallback,
		FallbackReason: fallbackReason,
		CacheIdentity:  buildOpenAITLSCacheIdentity(egress, profileHash, openAIGatewayTLSSourceDefaultFallback),
		HTTPApplicable: true,
		WSApplicable:   true,
		Profile:        profile,
	}
}

func (s *OpenAIGatewayCoreService) buildPlainTLS(account *Account, egress *OpenAIEgressResolution, transport OpenAIClientTransport, source, fallbackReason string) *OpenAIGatewayEffectiveTLS {
	_ = account
	_ = transport
	return &OpenAIGatewayEffectiveTLS{
		Enabled:        false,
		Source:         source,
		FallbackReason: fallbackReason,
		CacheIdentity:  buildOpenAITLSCacheIdentity(egress, "plain", source),
		HTTPApplicable: true,
		WSApplicable:   true,
	}
}

func hashOpenAITLSProfile(profile *model.TLSFingerprintProfile) string {
	if profile == nil {
		return ""
	}
	raw, _ := json.Marshal(tlsfingerprint.EffectiveFingerprint(profile.ToTLSProfile()))
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func hashOpenAITLSProfileName(name string) string {
	_ = name
	raw, _ := json.Marshal(tlsfingerprint.EffectiveFingerprint(&tlsfingerprint.Profile{Name: openAIGatewayDefaultTLSProfileName}))
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func buildOpenAITLSCacheIdentity(egress *OpenAIEgressResolution, profileHash string, source string) string {
	bucket := egressBucketName(egress)
	proxy := "direct"
	if egress != nil && strings.TrimSpace(egress.ProxyHash) != "" {
		proxy = strings.TrimSpace(egress.ProxyHash)
	}
	return fmt.Sprintf("bucket=%s|proxy=%s|profile_hash=%s|source=%s", bucket, proxy, strings.TrimSpace(profileHash), strings.TrimSpace(source))
}

func egressBucketName(egress *OpenAIEgressResolution) string {
	if egress == nil {
		return ""
	}
	return strings.TrimSpace(egress.BucketName)
}
