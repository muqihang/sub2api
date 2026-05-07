package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	OpenAIGatewayClientTokenHeader = "X-OpenAI-Gateway-Token"

	OpenAIGatewayProfileModeFixed   = "fixed"
	OpenAIGatewayProfileModeObserve = "observe"
	OpenAIGatewayProfileModeFrozen  = "frozen"

	openAIGatewayClientFamilyCodexOfficial = "codex_official"
	openAIGatewayClientFamilyOpenAISDK     = "openai_sdk"
	openAIGatewayClientFamilyGenericHTTP   = "generic_http"
	openAIGatewayClientFamilyUnknown       = "unknown"
	openAIGatewayClientFamilyLegacyAPIKey  = "legacy_api_key"
)

var errOpenAIGatewayClientUnauthorized = errors.New("openai gateway client token unauthorized")

type OpenAIGatewayClientIdentity struct {
	Name          string `json:"name,omitempty"`
	Authenticated bool   `json:"authenticated"`
	Family        string `json:"family"`
}

type OpenAIGatewayCanonicalProfile struct {
	ProfileID               string `json:"profile_id"`
	Mode                    string `json:"mode"`
	UserAgent               string `json:"user_agent"`
	StainlessLang           string `json:"stainless_lang"`
	StainlessPackageVersion string `json:"stainless_package_version"`
	StainlessOS             string `json:"stainless_os"`
	StainlessArch           string `json:"stainless_arch"`
	StainlessRuntime        string `json:"stainless_runtime"`
	StainlessRuntimeVersion string `json:"stainless_runtime_version"`
}

type OpenAIGatewayAccountRuntime struct {
	Client        *OpenAIGatewayClientIdentity   `json:"client,omitempty"`
	Profile       *OpenAIGatewayCanonicalProfile `json:"profile"`
	EgressBucket  string                         `json:"egress_bucket"`
	ProxySelected bool                           `json:"proxy_selected"`
	ProxyLabel    string                         `json:"proxy_label,omitempty"`
	ProxyHash     string                         `json:"proxy_hash,omitempty"`
	DebugProxyURL string                         `json:"debug_proxy_url,omitempty"`
	Transport     string                         `json:"transport"`
}

type OpenAIGatewayHealthSnapshot struct {
	GatewayStatus            string                             `json:"gateway_status"`
	OAuthStatus              string                             `json:"oauth_status"`
	OpenAIOAuthAccountsTotal int64                              `json:"openai_oauth_accounts_total"`
	RTManagedAccountsTotal   int64                              `json:"rt_managed_accounts_total"`
	TerminalAccountsTotal    int64                              `json:"terminal_accounts_total"`
	CoolingAccountsTotal     int64                              `json:"cooling_accounts_total"`
	EgressBuckets            map[string]int64                   `json:"egress_buckets"`
	DegradedReason           string                             `json:"degraded_reason,omitempty"`
	Refresh                  OpenAITokenRuntimeMetrics          `json:"refresh"`
	WS                       OpenAIWSPerformanceMetricsSnapshot `json:"ws"`
}

type OpenAIGatewayVerifySnapshot struct {
	AccountID           int64                          `json:"account_id"`
	AccountName         string                         `json:"account_name"`
	Client              *OpenAIGatewayClientIdentity   `json:"client,omitempty"`
	Profile             *OpenAIGatewayCanonicalProfile `json:"profile"`
	EgressBucket        string                         `json:"egress_bucket"`
	ProxySelected       bool                           `json:"proxy_selected"`
	ProxyLabel          string                         `json:"proxy_label,omitempty"`
	ProxyHash           string                         `json:"proxy_hash,omitempty"`
	DebugProxyURL       string                         `json:"debug_proxy_url,omitempty"`
	Transport           string                         `json:"transport"`
	RequestedUA         string                         `json:"requested_user_agent,omitempty"`
	RequestedOriginator string                         `json:"requested_originator,omitempty"`
}

type OpenAIGatewayAdminAccountSnapshot struct {
	AccountID             int64    `json:"account_id"`
	AccountName           string   `json:"account_name"`
	Status                string   `json:"status"`
	Schedulable           bool     `json:"schedulable"`
	ProfileID             string   `json:"profile_id,omitempty"`
	ProfileMode           string   `json:"profile_mode,omitempty"`
	EgressBucket          string   `json:"egress_bucket,omitempty"`
	PoolRole              string   `json:"pool_role,omitempty"`
	AuthState             string   `json:"auth_state,omitempty"`
	TokenSource           string   `json:"token_source,omitempty"`
	ClientFamily          string   `json:"client_family,omitempty"`
	LastVerifiedAt        string   `json:"last_verified_at,omitempty"`
	LastValidatedAt       string   `json:"last_validated_at,omitempty"`
	LastRefreshErrorCode  string   `json:"last_refresh_error_code,omitempty"`
	LastGrantedScope      string   `json:"last_granted_scope,omitempty"`
	LastAccessTokenScopes []string `json:"last_access_token_scopes,omitempty"`
	ResponsesWriteCapable bool     `json:"responses_write_capable"`
}

type OpenAIGatewayAdminBucketSnapshot struct {
	Name          string `json:"name"`
	Enabled       bool   `json:"enabled"`
	ProxySelected bool   `json:"proxy_selected"`
	ProxyLabel    string `json:"proxy_label,omitempty"`
	ProxyHash     string `json:"proxy_hash,omitempty"`
	AccountCount  int64  `json:"account_count"`
	Warning       string `json:"warning,omitempty"`
}

type OpenAIGatewayAdminStatusSnapshot struct {
	Health   *OpenAIGatewayHealthSnapshot        `json:"health"`
	Buckets  []OpenAIGatewayAdminBucketSnapshot  `json:"buckets"`
	Accounts []OpenAIGatewayAdminAccountSnapshot `json:"accounts"`
}

type OpenAIGatewayCoreService struct {
	accountRepo          AccountRepository
	cfg                  *config.Config
	openAITokenProvider  *OpenAITokenProvider
	accountWriteThrottle *accountWriteThrottle
}

func NewOpenAIGatewayCoreService(accountRepo AccountRepository, cfg *config.Config, openAITokenProvider *OpenAITokenProvider) *OpenAIGatewayCoreService {
	return &OpenAIGatewayCoreService{
		accountRepo:          accountRepo,
		cfg:                  cfg,
		openAITokenProvider:  openAITokenProvider,
		accountWriteThrottle: newAccountWriteThrottle(15 * time.Minute),
	}
}

func (s *OpenAIGatewayCoreService) IsEnabled() bool {
	return s != nil && s.cfg != nil && s.cfg.Gateway.OpenAICore.Enabled
}

func (s *OpenAIGatewayCoreService) AuthenticateClientHeaders(headers http.Header) (*OpenAIGatewayClientIdentity, error) {
	if headers == nil {
		return &OpenAIGatewayClientIdentity{Family: openAIGatewayClientFamilyUnknown}, nil
	}
	if s == nil || s.cfg == nil {
		return &OpenAIGatewayClientIdentity{Family: detectOpenAIGatewayClientFamily(headers)}, nil
	}
	token := strings.TrimSpace(headers.Get(OpenAIGatewayClientTokenHeader))
	family := detectOpenAIGatewayClientFamily(headers)
	if token == "" {
		return &OpenAIGatewayClientIdentity{Family: family}, nil
	}

	for _, item := range s.cfg.Gateway.OpenAICore.ClientTokens {
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(item.Token)), []byte(token)) == 1 {
			return &OpenAIGatewayClientIdentity{
				Name:          strings.TrimSpace(item.Name),
				Authenticated: true,
				Family:        family,
			}, nil
		}
	}
	return nil, errOpenAIGatewayClientUnauthorized
}

func (s *OpenAIGatewayCoreService) ProbeRequiresClientToken() bool {
	if s == nil || s.cfg == nil {
		return false
	}
	if len(s.cfg.Gateway.OpenAICore.ClientTokens) > 0 {
		return true
	}
	return s.cfg.Gateway.OpenAICore.ProbeRequireClientToken
}

func (s *OpenAIGatewayCoreService) ResolveEgressBucket(account *Account) string {
	if s == nil || s.cfg == nil {
		if account != nil {
			if bucket := strings.TrimSpace(account.GetExtraString("openai_gateway_egress_bucket")); bucket != "" {
				return bucket
			}
		}
		return "default"
	}
	if account == nil {
		return strings.TrimSpace(s.cfg.Gateway.OpenAICore.DefaultEgressBucket)
	}
	if bucket := strings.TrimSpace(account.GetExtraString("openai_gateway_egress_bucket")); bucket != "" {
		return bucket
	}
	if s == nil || s.cfg == nil {
		return "default"
	}
	if bucket := strings.TrimSpace(s.cfg.Gateway.OpenAICore.DefaultEgressBucket); bucket != "" {
		return bucket
	}
	return "default"
}

func (s *OpenAIGatewayCoreService) ResolveEgressProxyURL(account *Account, fallbackProxyURL string) string {
	resolution, err := s.ResolveEgress(context.Background(), account, fallbackProxyURL)
	if err != nil || resolution == nil {
		return ""
	}
	return resolution.ProxyURL
}

func (s *OpenAIGatewayCoreService) HasEgressBucket(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	if s == nil || s.cfg == nil {
		return false
	}
	for _, bucket := range s.cfg.Gateway.OpenAICore.EgressBuckets {
		if strings.TrimSpace(bucket.Name) == name {
			return true
		}
	}
	return false
}

func resolveOpenAIAccountProxyURL(account *Account) string {
	if account == nil {
		return ""
	}
	if account.Proxy != nil {
		return strings.TrimSpace(account.Proxy.URL())
	}
	return ""
}

func (s *OpenAIGatewayCoreService) ResolveAccountRuntime(ctx context.Context, account *Account, headers http.Header, transport OpenAIClientTransport) (*OpenAIGatewayAccountRuntime, error) {
	if account == nil {
		return nil, ErrAccountNilInput
	}

	client, err := s.AuthenticateClientHeaders(headers)
	if err != nil {
		return nil, err
	}

	profile, updates := s.resolveCanonicalProfile(account, headers)
	egress, err := s.ResolveEgress(ctx, account, "")
	if err != nil {
		return nil, err
	}
	runtime := &OpenAIGatewayAccountRuntime{
		Client:        client,
		Profile:       profile,
		EgressBucket:  egress.BucketName,
		ProxySelected: egress.ProxySelected,
		ProxyLabel:    egress.ProxyLabel,
		ProxyHash:     egress.ProxyHash,
		Transport:     string(transport),
	}
	if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAICore.ExposeRawProxyInDebug {
		runtime.DebugProxyURL = egress.ProxyURL
	}

	now := time.Now().UTC().Format(time.RFC3339)
	updates["openai_gateway_egress_bucket"] = runtime.EgressBucket
	updates["openai_gateway_last_verified_at"] = now
	if client != nil && client.Family != "" {
		updates["openai_gateway_client_family"] = client.Family
	}
	if len(updates) > 0 {
		s.applyRuntimeUpdates(ctx, account, updates)
	}

	return runtime, nil
}

func (s *OpenAIGatewayCoreService) ApplyCanonicalHeaders(headers http.Header, profile *OpenAIGatewayCanonicalProfile) {
	if headers == nil || profile == nil {
		return
	}
	setIfNotEmpty := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			headers.Set(key, value)
		}
	}
	setIfNotEmpty("user-agent", profile.UserAgent)
	setIfNotEmpty("X-Stainless-Lang", profile.StainlessLang)
	setIfNotEmpty("X-Stainless-Package-Version", profile.StainlessPackageVersion)
	setIfNotEmpty("X-Stainless-OS", profile.StainlessOS)
	setIfNotEmpty("X-Stainless-Arch", profile.StainlessArch)
	setIfNotEmpty("X-Stainless-Runtime", profile.StainlessRuntime)
	setIfNotEmpty("X-Stainless-Runtime-Version", profile.StainlessRuntimeVersion)
}

func (s *OpenAIGatewayCoreService) RewriteMetadataUserID(body []byte, account *Account, runtime *OpenAIGatewayAccountRuntime) ([]byte, error) {
	if len(body) == 0 || account == nil || runtime == nil || runtime.Profile == nil {
		return body, nil
	}
	userID := strings.TrimSpace(gjson.GetBytes(body, "metadata.user_id").String())
	if userID == "" {
		return body, nil
	}
	parsed := ParseMetadataUserID(userID)
	if parsed == nil {
		return body, nil
	}
	accountUUID := account.GetChatGPTAccountID()
	if accountUUID == "" {
		accountUUID = parsed.AccountUUID
	}
	rewritten := FormatMetadataUserID(runtime.Profile.ProfileID, accountUUID, parsed.SessionID, runtime.Profile.UserAgent)
	if rewritten == userID {
		return body, nil
	}
	return sjson.SetBytes(body, "metadata.user_id", rewritten)
}

func (s *OpenAIGatewayCoreService) RewritePayloadMetadataUserID(payload map[string]any, account *Account, runtime *OpenAIGatewayAccountRuntime) map[string]any {
	if len(payload) == 0 {
		return payload
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return payload
	}
	rewritten, err := s.RewriteMetadataUserID(raw, account, runtime)
	if err != nil || len(rewritten) == 0 {
		return payload
	}
	var out map[string]any
	if err := json.Unmarshal(rewritten, &out); err != nil {
		return payload
	}
	return out
}

func (s *OpenAIGatewayCoreService) BuildHealthSnapshot(ctx context.Context, ws OpenAIWSPerformanceMetricsSnapshot) (*OpenAIGatewayHealthSnapshot, error) {
	if s == nil || s.accountRepo == nil {
		return &OpenAIGatewayHealthSnapshot{
			GatewayStatus:  "degraded",
			OAuthStatus:    "degraded",
			EgressBuckets:  map[string]int64{},
			DegradedReason: "gateway_core_not_configured",
			WS:             ws,
		}, nil
	}
	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		return nil, err
	}

	snapshot := &OpenAIGatewayHealthSnapshot{
		GatewayStatus: "ok",
		OAuthStatus:   "valid",
		EgressBuckets: map[string]int64{},
		WS:            ws,
	}
	if s.openAITokenProvider != nil {
		snapshot.Refresh = s.openAITokenProvider.SnapshotRuntimeMetrics()
	}

	for _, account := range accounts {
		if !account.IsOpenAIOAuth() {
			continue
		}
		snapshot.OpenAIOAuthAccountsTotal++
		if account.IsOpenAIRTManaged() {
			snapshot.RTManagedAccountsTotal++
		}
		if account.GetOpenAIAuthState() == OpenAIAuthStateTerminal || account.Status == StatusError {
			snapshot.TerminalAccountsTotal++
		}
		if account.GetOpenAIAuthState() == OpenAIAuthStateCooling {
			snapshot.CoolingAccountsTotal++
		}
		snapshot.EgressBuckets[s.ResolveEgressBucket(&account)]++
	}

	switch {
	case snapshot.OpenAIOAuthAccountsTotal == 0:
		snapshot.GatewayStatus = "degraded"
		snapshot.OAuthStatus = "degraded"
		snapshot.DegradedReason = "no_openai_oauth_accounts"
	case snapshot.RTManagedAccountsTotal == 0:
		snapshot.GatewayStatus = "degraded"
		snapshot.OAuthStatus = "degraded"
		snapshot.DegradedReason = "no_rt_managed_accounts"
	case snapshot.TerminalAccountsTotal > 0:
		snapshot.GatewayStatus = "degraded"
		snapshot.OAuthStatus = "degraded"
		snapshot.DegradedReason = "terminal_accounts_present"
	case snapshot.Refresh.RefreshFailure > 0:
		snapshot.GatewayStatus = "degraded"
		snapshot.OAuthStatus = "degraded"
		snapshot.DegradedReason = "refresh_failures_present"
	case snapshot.CoolingAccountsTotal > 0:
		snapshot.GatewayStatus = "degraded"
		snapshot.OAuthStatus = "degraded"
		snapshot.DegradedReason = "cooling_accounts_present"
	}

	return snapshot, nil
}

func (s *OpenAIGatewayCoreService) BuildVerifySnapshot(ctx context.Context, accountID int64, headers http.Header, transport OpenAIClientTransport) (*OpenAIGatewayVerifySnapshot, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	runtime, err := s.ResolveAccountRuntime(ctx, account, headers, transport)
	if err != nil {
		return nil, err
	}
	return &OpenAIGatewayVerifySnapshot{
		AccountID:           account.ID,
		AccountName:         account.Name,
		Client:              runtime.Client,
		Profile:             runtime.Profile,
		EgressBucket:        runtime.EgressBucket,
		ProxySelected:       runtime.ProxySelected,
		ProxyLabel:          runtime.ProxyLabel,
		ProxyHash:           runtime.ProxyHash,
		DebugProxyURL:       runtime.DebugProxyURL,
		Transport:           runtime.Transport,
		RequestedUA:         strings.TrimSpace(headers.Get("User-Agent")),
		RequestedOriginator: strings.TrimSpace(headers.Get("originator")),
	}, nil
}

func (s *OpenAIGatewayCoreService) BuildAdminStatusSnapshot(ctx context.Context, ws OpenAIWSPerformanceMetricsSnapshot) (*OpenAIGatewayAdminStatusSnapshot, error) {
	health, err := s.BuildHealthSnapshot(ctx, ws)
	if err != nil {
		return nil, err
	}
	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		return nil, err
	}
	result := &OpenAIGatewayAdminStatusSnapshot{
		Health: health,
	}
	for _, bucket := range s.cfg.Gateway.OpenAICore.EgressBuckets {
		proxyURL := strings.TrimSpace(bucket.ProxyURL)
		snapshot := OpenAIGatewayAdminBucketSnapshot{
			Name:          strings.TrimSpace(bucket.Name),
			Enabled:       bucket.Enabled,
			ProxySelected: proxyURL != "",
			ProxyLabel:    MaskOpenAIProxyURL(proxyURL),
			ProxyHash:     HashOpenAIProxyURL(proxyURL),
			AccountCount:  health.EgressBuckets[strings.TrimSpace(bucket.Name)],
		}
		result.Buckets = append(result.Buckets, snapshot)
	}
	for _, account := range accounts {
		if !account.IsOpenAIOAuth() {
			continue
		}
		result.Accounts = append(result.Accounts, OpenAIGatewayAdminAccountSnapshot{
			AccountID:             account.ID,
			AccountName:           account.Name,
			Status:                account.Status,
			Schedulable:           account.Schedulable,
			ProfileID:             strings.TrimSpace(account.GetExtraString("openai_gateway_profile_id")),
			ProfileMode:           strings.TrimSpace(account.GetExtraString("openai_gateway_profile_mode")),
			EgressBucket:          s.ResolveEgressBucket(&account),
			PoolRole:              account.GetOpenAIPoolRole(),
			AuthState:             account.GetOpenAIAuthState(),
			TokenSource:           account.GetOpenAITokenSource(),
			ClientFamily:          strings.TrimSpace(account.GetExtraString("openai_gateway_client_family")),
			LastVerifiedAt:        strings.TrimSpace(account.GetExtraString("openai_gateway_last_verified_at")),
			LastValidatedAt:       strings.TrimSpace(account.GetExtraString("openai_last_validated_at")),
			LastRefreshErrorCode:  strings.TrimSpace(account.GetExtraString("openai_last_refresh_error_code")),
			LastGrantedScope:      strings.TrimSpace(account.GetExtraString("openai_last_granted_scope")),
			LastAccessTokenScopes: scopesFromAny(account.Extra["openai_last_access_token_scopes"]),
			ResponsesWriteCapable: account.getExtraBool("openai_responses_write_capable"),
		})
	}
	return result, nil
}

func (s *OpenAIGatewayCoreService) resolveCanonicalProfile(account *Account, headers http.Header) (*OpenAIGatewayCanonicalProfile, map[string]any) {
	mode := strings.TrimSpace(account.GetExtraString("openai_gateway_profile_mode"))
	if mode == "" && s.cfg != nil {
		mode = strings.TrimSpace(s.cfg.Gateway.OpenAICore.DefaultProfileMode)
	}
	switch mode {
	case OpenAIGatewayProfileModeObserve, OpenAIGatewayProfileModeFrozen:
	default:
		mode = OpenAIGatewayProfileModeFixed
	}

	profile := &OpenAIGatewayCanonicalProfile{
		ProfileID:               strings.TrimSpace(account.GetExtraString("openai_gateway_profile_id")),
		Mode:                    mode,
		UserAgent:               strings.TrimSpace(account.GetExtraString("openai_gateway_canonical_user_agent")),
		StainlessLang:           strings.TrimSpace(account.GetExtraString("openai_gateway_canonical_stainless_lang")),
		StainlessPackageVersion: strings.TrimSpace(account.GetExtraString("openai_gateway_canonical_stainless_package_version")),
		StainlessOS:             strings.TrimSpace(account.GetExtraString("openai_gateway_canonical_stainless_os")),
		StainlessArch:           strings.TrimSpace(account.GetExtraString("openai_gateway_canonical_stainless_arch")),
		StainlessRuntime:        strings.TrimSpace(account.GetExtraString("openai_gateway_canonical_stainless_runtime")),
		StainlessRuntimeVersion: strings.TrimSpace(account.GetExtraString("openai_gateway_canonical_stainless_runtime_version")),
	}
	if profile.ProfileID == "" {
		profile.ProfileID = buildOpenAIGatewayProfileID(account.ID)
	}

	defaults := s.defaultCanonicalProfile()
	updates := map[string]any{
		"openai_gateway_profile_id":   profile.ProfileID,
		"openai_gateway_profile_mode": mode,
	}

	applyValue := func(current *string, headerKey string, fallback string) {
		if strings.TrimSpace(*current) != "" {
			if mode == OpenAIGatewayProfileModeObserve {
				if observed := strings.TrimSpace(headers.Get(headerKey)); observed != "" && observed != *current {
					*current = observed
				}
			}
			return
		}
		if mode == OpenAIGatewayProfileModeObserve {
			if observed := strings.TrimSpace(headers.Get(headerKey)); observed != "" {
				*current = observed
				return
			}
		}
		*current = fallback
	}

	applyValue(&profile.UserAgent, "User-Agent", defaults.UserAgent)
	applyValue(&profile.StainlessLang, "X-Stainless-Lang", defaults.StainlessLang)
	applyValue(&profile.StainlessPackageVersion, "X-Stainless-Package-Version", defaults.StainlessPackageVersion)
	applyValue(&profile.StainlessOS, "X-Stainless-OS", defaults.StainlessOS)
	applyValue(&profile.StainlessArch, "X-Stainless-Arch", defaults.StainlessArch)
	applyValue(&profile.StainlessRuntime, "X-Stainless-Runtime", defaults.StainlessRuntime)
	applyValue(&profile.StainlessRuntimeVersion, "X-Stainless-Runtime-Version", defaults.StainlessRuntimeVersion)

	updates["openai_gateway_canonical_user_agent"] = profile.UserAgent
	updates["openai_gateway_canonical_stainless_lang"] = profile.StainlessLang
	updates["openai_gateway_canonical_stainless_package_version"] = profile.StainlessPackageVersion
	updates["openai_gateway_canonical_stainless_os"] = profile.StainlessOS
	updates["openai_gateway_canonical_stainless_arch"] = profile.StainlessArch
	updates["openai_gateway_canonical_stainless_runtime"] = profile.StainlessRuntime
	updates["openai_gateway_canonical_stainless_runtime_version"] = profile.StainlessRuntimeVersion

	return profile, updates
}

func (s *OpenAIGatewayCoreService) defaultCanonicalProfile() *OpenAIGatewayCanonicalProfile {
	if s == nil || s.cfg == nil {
		return &OpenAIGatewayCanonicalProfile{
			UserAgent:               codexCLIUserAgent,
			StainlessLang:           "js",
			StainlessPackageVersion: "0.70.0",
			StainlessOS:             "Linux",
			StainlessArch:           "arm64",
			StainlessRuntime:        "node",
			StainlessRuntimeVersion: "v24.13.0",
		}
	}
	cfg := s.cfg.Gateway.OpenAICore
	return &OpenAIGatewayCanonicalProfile{
		UserAgent:               strings.TrimSpace(cfg.CanonicalUserAgent),
		StainlessLang:           strings.TrimSpace(cfg.CanonicalStainlessLang),
		StainlessPackageVersion: strings.TrimSpace(cfg.CanonicalStainlessPackageVersion),
		StainlessOS:             strings.TrimSpace(cfg.CanonicalStainlessOS),
		StainlessArch:           strings.TrimSpace(cfg.CanonicalStainlessArch),
		StainlessRuntime:        strings.TrimSpace(cfg.CanonicalStainlessRuntime),
		StainlessRuntimeVersion: strings.TrimSpace(cfg.CanonicalStainlessRuntimeVersion),
	}
}

func buildOpenAIGatewayProfileID(accountID int64) string {
	sum := sha256.Sum256([]byte("openai-gateway-profile:" + strconv.FormatInt(accountID, 10)))
	return hex.EncodeToString(sum[:])
}

func (s *OpenAIGatewayCoreService) applyRuntimeUpdates(ctx context.Context, account *Account, updates map[string]any) {
	if account == nil || len(updates) == 0 {
		return
	}
	now := time.Now()
	if !s.accountWriteThrottle.Allow(account.ID, now) {
		for k, v := range updates {
			if account.Extra == nil {
				account.Extra = map[string]any{}
			}
			account.Extra[k] = v
		}
		return
	}
	_ = s.accountRepo.UpdateExtra(ctx, account.ID, updates)
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	for k, v := range updates {
		account.Extra[k] = v
	}
}

func detectOpenAIGatewayClientFamily(headers http.Header) string {
	userAgent := strings.TrimSpace(headers.Get("User-Agent"))
	originator := strings.TrimSpace(headers.Get("originator"))
	switch {
	case openai.IsCodexOfficialClientByHeaders(userAgent, originator):
		return openAIGatewayClientFamilyCodexOfficial
	case strings.TrimSpace(headers.Get("X-Stainless-Lang")) != "" ||
		strings.TrimSpace(headers.Get("X-Stainless-Package-Version")) != "":
		return openAIGatewayClientFamilyOpenAISDK
	case userAgent != "":
		return openAIGatewayClientFamilyGenericHTTP
	default:
		return openAIGatewayClientFamilyUnknown
	}
}
