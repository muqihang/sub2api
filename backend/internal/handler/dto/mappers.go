// Package dto provides data transfer objects for HTTP handlers.
package dto

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func UserFromServiceShallow(u *service.User) *User {
	if u == nil {
		return nil
	}
	return &User{
		ID:                         u.ID,
		Email:                      u.Email,
		Username:                   u.Username,
		Role:                       u.Role,
		Balance:                    u.Balance,
		Concurrency:                u.Concurrency,
		Status:                     u.Status,
		AllowedGroups:              u.AllowedGroups,
		LastActiveAt:               u.LastActiveAt,
		CreatedAt:                  u.CreatedAt,
		UpdatedAt:                  u.UpdatedAt,
		BalanceNotifyEnabled:       u.BalanceNotifyEnabled,
		BalanceNotifyThresholdType: u.BalanceNotifyThresholdType,
		BalanceNotifyThreshold:     u.BalanceNotifyThreshold,
		BalanceNotifyExtraEmails:   NotifyEmailEntriesFromService(u.BalanceNotifyExtraEmails),
		TotalRecharged:             u.TotalRecharged,
		RPMLimit:                   u.RPMLimit,
		DeletedAt:                  u.DeletedAt,
	}
}

func UserFromService(u *service.User) *User {
	if u == nil {
		return nil
	}
	out := UserFromServiceShallow(u)
	if len(u.APIKeys) > 0 {
		out.APIKeys = make([]APIKey, 0, len(u.APIKeys))
		for i := range u.APIKeys {
			k := u.APIKeys[i]
			out.APIKeys = append(out.APIKeys, *APIKeyFromService(&k))
		}
	}
	if len(u.Subscriptions) > 0 {
		out.Subscriptions = make([]UserSubscription, 0, len(u.Subscriptions))
		for i := range u.Subscriptions {
			s := u.Subscriptions[i]
			out.Subscriptions = append(out.Subscriptions, *UserSubscriptionFromService(&s))
		}
	}
	return out
}

// UserFromServiceAdmin converts a service User to DTO for admin users.
// It includes notes - user-facing endpoints must not use this.
func UserFromServiceAdmin(u *service.User) *AdminUser {
	if u == nil {
		return nil
	}
	base := UserFromService(u)
	if base == nil {
		return nil
	}
	return &AdminUser{
		User:       *base,
		Notes:      u.Notes,
		LastUsedAt: u.LastUsedAt,
		GroupRates: u.GroupRates,
	}
}

func APIKeyFromService(k *service.APIKey) *APIKey {
	if k == nil {
		return nil
	}
	out := &APIKey{
		ID:            k.ID,
		UserID:        k.UserID,
		Key:           k.Key,
		Name:          k.Name,
		GroupID:       k.GroupID,
		AugmentOnly:   k.IsAugmentOnly(),
		CodexOnly:     k.IsCodexOnly(),
		Status:        k.Status,
		IPWhitelist:   k.IPWhitelist,
		IPBlacklist:   k.IPBlacklist,
		LastUsedAt:    k.LastUsedAt,
		Quota:         k.Quota,
		QuotaUsed:     k.QuotaUsed,
		ExpiresAt:     k.ExpiresAt,
		CreatedAt:     k.CreatedAt,
		UpdatedAt:     k.UpdatedAt,
		RateLimit5h:   k.RateLimit5h,
		RateLimit1d:   k.RateLimit1d,
		RateLimit7d:   k.RateLimit7d,
		Usage5h:       k.EffectiveUsage5h(),
		Usage1d:       k.EffectiveUsage1d(),
		Usage7d:       k.EffectiveUsage7d(),
		Window5hStart: k.Window5hStart,
		Window1dStart: k.Window1dStart,
		Window7dStart: k.Window7dStart,
		User:          UserFromServiceShallow(k.User),
		Group:         GroupFromServiceShallow(k.Group),
	}
	if k.Window5hStart != nil && !service.IsWindowExpired(k.Window5hStart, service.RateLimitWindow5h) {
		t := k.Window5hStart.Add(service.RateLimitWindow5h)
		out.Reset5hAt = &t
	}
	if k.Window1dStart != nil && !service.IsWindowExpired(k.Window1dStart, service.RateLimitWindow1d) {
		t := k.Window1dStart.Add(service.RateLimitWindow1d)
		out.Reset1dAt = &t
	}
	if k.Window7dStart != nil && !service.IsWindowExpired(k.Window7dStart, service.RateLimitWindow7d) {
		t := k.Window7dStart.Add(service.RateLimitWindow7d)
		out.Reset7dAt = &t
	}
	return out
}

func GroupFromServiceShallow(g *service.Group) *Group {
	if g == nil {
		return nil
	}
	out := groupFromServiceBase(g)
	return &out
}

func GroupFromService(g *service.Group) *Group {
	if g == nil {
		return nil
	}
	return GroupFromServiceShallow(g)
}

// GroupFromServiceAdmin converts a service Group to DTO for admin users.
// It includes internal fields like model_routing and account_count.
func GroupFromServiceAdmin(g *service.Group) *AdminGroup {
	if g == nil {
		return nil
	}
	out := &AdminGroup{
		Group:                       groupFromServiceBase(g),
		ModelRouting:                g.ModelRouting,
		ModelRoutingEnabled:         g.ModelRoutingEnabled,
		MCPXMLInject:                g.MCPXMLInject,
		DefaultMappedModel:          g.DefaultMappedModel,
		MessagesDispatchModelConfig: g.MessagesDispatchModelConfig,
		ModelsListConfig:            g.ModelsListConfig,
		SupportedModelScopes:        g.SupportedModelScopes,
		AccountCount:                g.AccountCount,
		ActiveAccountCount:          g.ActiveAccountCount,
		RateLimitedAccountCount:     g.RateLimitedAccountCount,
		SortOrder:                   g.SortOrder,
	}
	if len(g.AccountGroups) > 0 {
		out.AccountGroups = make([]AccountGroup, 0, len(g.AccountGroups))
		for i := range g.AccountGroups {
			ag := g.AccountGroups[i]
			out.AccountGroups = append(out.AccountGroups, *AccountGroupFromService(&ag))
		}
	}
	return out
}

func groupFromServiceBase(g *service.Group) Group {
	return Group{
		ID:                              g.ID,
		Name:                            g.Name,
		Description:                     g.Description,
		Platform:                        g.Platform,
		RateMultiplier:                  g.RateMultiplier,
		IsExclusive:                     g.IsExclusive,
		Status:                          g.Status,
		SubscriptionType:                g.SubscriptionType,
		DailyLimitUSD:                   g.DailyLimitUSD,
		WeeklyLimitUSD:                  g.WeeklyLimitUSD,
		MonthlyLimitUSD:                 g.MonthlyLimitUSD,
		AugmentGatewayEntitled:          g.AugmentGatewayEntitled,
		CodexGatewayEntitled:            g.CodexGatewayEntitled,
		AllowImageGeneration:            g.AllowImageGeneration,
		ImageRateIndependent:            g.ImageRateIndependent,
		ImageRateMultiplier:             g.ImageRateMultiplier,
		ImagePrice1K:                    g.ImagePrice1K,
		ImagePrice2K:                    g.ImagePrice2K,
		ImagePrice4K:                    g.ImagePrice4K,
		ClaudeCodeOnly:                  g.ClaudeCodeOnly,
		FallbackGroupID:                 g.FallbackGroupID,
		FallbackGroupIDOnInvalidRequest: g.FallbackGroupIDOnInvalidRequest,
		AllowMessagesDispatch:           g.AllowMessagesDispatch,
		RequireOAuthOnly:                g.RequireOAuthOnly,
		RequirePrivacySet:               g.RequirePrivacySet,
		RPMLimit:                        g.RPMLimit,
		CreatedAt:                       g.CreatedAt,
		UpdatedAt:                       g.UpdatedAt,
	}
}

func AccountFromServiceShallow(a *service.Account) *Account {
	if a == nil {
		return nil
	}
	redactedCreds, credsStatus := accountCredentialsForDTO(a)
	out := &Account{
		ID:                      a.ID,
		Name:                    a.Name,
		Notes:                   a.Notes,
		Platform:                a.Platform,
		Type:                    a.Type,
		Credentials:             redactedCreds,
		CredentialsStatus:       credsStatus,
		Extra:                   accountExtraForDTO(a),
		ProxyID:                 a.ProxyID,
		ProxyFallbackOriginID:   a.ProxyFallbackOriginID,
		ProxyFallbackOriginName: a.ProxyFallbackOriginName,
		Concurrency:             a.Concurrency,
		LoadFactor:              a.LoadFactor,
		Priority:                a.Priority,
		RateMultiplier:          a.BillingRateMultiplier(),
		Status:                  a.Status,
		ErrorMessage:            a.ErrorMessage,
		LastUsedAt:              a.LastUsedAt,
		ExpiresAt:               timeToUnixSeconds(a.ExpiresAt),
		AutoPauseOnExpired:      a.AutoPauseOnExpired,
		CreatedAt:               a.CreatedAt,
		UpdatedAt:               a.UpdatedAt,
		Schedulable:             a.Schedulable,
		EffectiveSchedulable:    a.IsSchedulable(),
		IsFormalPool:            service.IsFormalPoolAccount(a),
		RateLimitedAt:           a.RateLimitedAt,
		RateLimitResetAt:        a.RateLimitResetAt,
		OverloadUntil:           a.OverloadUntil,
		TempUnschedulableUntil:  a.TempUnschedulableUntil,
		TempUnschedulableReason: a.TempUnschedulableReason,
		SessionWindowStart:      a.SessionWindowStart,
		SessionWindowEnd:        a.SessionWindowEnd,
		SessionWindowStatus:     a.SessionWindowStatus,
		GroupIDs:                a.GroupIDs,
	}

	// 提取 5h 窗口费用控制和会话数量控制配置（仅 Anthropic OAuth/SetupToken 账号有效）
	if a.IsAnthropicOAuthOrSetupToken() {
		applyFormalPoolAccountFields(out, a)
		if limit := a.GetWindowCostLimit(); limit > 0 {
			out.WindowCostLimit = &limit
		}
		if reserve := a.GetWindowCostStickyReserve(); reserve > 0 {
			out.WindowCostStickyReserve = &reserve
		}
		if maxSessions := a.GetMaxSessions(); maxSessions > 0 {
			out.MaxSessions = &maxSessions
		}
		if idleTimeout := a.GetSessionIdleTimeoutMinutes(); idleTimeout > 0 {
			out.SessionIdleTimeoutMin = &idleTimeout
		}
		if rpm := a.GetBaseRPM(); rpm > 0 {
			out.BaseRPM = &rpm
			strategy := a.GetRPMStrategy()
			out.RPMStrategy = &strategy
			buffer := a.GetRPMStickyBuffer()
			out.RPMStickyBuffer = &buffer
		}
		// 用户消息队列模式
		if mode := a.GetUserMsgQueueMode(); mode != "" {
			out.UserMsgQueueMode = &mode
		}
		// TLS指纹伪装开关
		if a.IsTLSFingerprintEnabled() {
			enabled := true
			out.EnableTLSFingerprint = &enabled
		}
		// TLS指纹模板ID
		if profileID := a.GetTLSFingerprintProfileID(); profileID > 0 {
			out.TLSFingerprintProfileID = &profileID
		}
		// 会话ID伪装开关
		if a.IsSessionIDMaskingEnabled() {
			enabled := true
			out.EnableSessionIDMasking = &enabled
		}
		// 缓存 TTL 强制替换
		if a.IsCacheTTLOverrideEnabled() {
			enabled := true
			out.CacheTTLOverrideEnabled = &enabled
			target := a.GetCacheTTLOverrideTarget()
			out.CacheTTLOverrideTarget = &target
		}
		// 自定义 Base URL 中继转发
		if a.IsCustomBaseURLEnabled() {
			enabled := true
			out.CustomBaseURLEnabled = &enabled
			if customURL := a.GetCustomBaseURL(); customURL != "" {
				out.CustomBaseURL = &customURL
			}
		}
	}

	if a.Platform == service.PlatformOpenAI && a.Extra != nil {
		if _, ok := a.Extra["openai_gateway_tls"]; ok {
			policy := a.GetOpenAIGatewayTLSOverride()
			out.OpenAIGatewayTLS = &service.OpenAIGatewayAccountTLSPolicy{
				Enabled:   policy.Enabled,
				ProfileID: policy.ProfileID,
			}
		}
	}

	// 提取账号配额限制（apikey / bedrock 类型有效）
	if a.IsAPIKeyOrBedrock() {
		if limit := a.GetQuotaLimit(); limit > 0 {
			out.QuotaLimit = &limit
			used := a.GetQuotaUsed()
			out.QuotaUsed = &used
		}
		if limit := a.GetQuotaDailyLimit(); limit > 0 {
			out.QuotaDailyLimit = &limit
			used := a.GetQuotaDailyUsed()
			if a.IsDailyQuotaPeriodExpired() {
				used = 0
			}
			out.QuotaDailyUsed = &used
		}
		if limit := a.GetQuotaWeeklyLimit(); limit > 0 {
			out.QuotaWeeklyLimit = &limit
			used := a.GetQuotaWeeklyUsed()
			if a.IsWeeklyQuotaPeriodExpired() {
				used = 0
			}
			out.QuotaWeeklyUsed = &used
		}
		// 固定时间重置配置
		if mode := a.GetQuotaDailyResetMode(); mode == "fixed" {
			out.QuotaDailyResetMode = &mode
			hour := a.GetQuotaDailyResetHour()
			out.QuotaDailyResetHour = &hour
		}
		if mode := a.GetQuotaWeeklyResetMode(); mode == "fixed" {
			out.QuotaWeeklyResetMode = &mode
			day := a.GetQuotaWeeklyResetDay()
			out.QuotaWeeklyResetDay = &day
			hour := a.GetQuotaWeeklyResetHour()
			out.QuotaWeeklyResetHour = &hour
		}
		if a.GetQuotaDailyResetMode() == "fixed" || a.GetQuotaWeeklyResetMode() == "fixed" {
			tz := a.GetQuotaResetTimezone()
			out.QuotaResetTimezone = &tz
		}
		if a.Extra != nil {
			if v, ok := a.Extra["quota_daily_reset_at"].(string); ok && v != "" {
				out.QuotaDailyResetAt = &v
			}
			if v, ok := a.Extra["quota_weekly_reset_at"].(string); ok && v != "" {
				out.QuotaWeeklyResetAt = &v
			}
		}

		// 配额通知配置
		if enabled := a.GetQuotaNotifyDailyEnabled(); enabled {
			out.QuotaNotifyDailyEnabled = &enabled
		}
		if threshold := a.GetQuotaNotifyDailyThreshold(); threshold > 0 {
			out.QuotaNotifyDailyThreshold = &threshold
		}
		if enabled := a.GetQuotaNotifyWeeklyEnabled(); enabled {
			out.QuotaNotifyWeeklyEnabled = &enabled
		}
		if threshold := a.GetQuotaNotifyWeeklyThreshold(); threshold > 0 {
			out.QuotaNotifyWeeklyThreshold = &threshold
		}
		if enabled := a.GetQuotaNotifyTotalEnabled(); enabled {
			out.QuotaNotifyTotalEnabled = &enabled
		}
		if threshold := a.GetQuotaNotifyTotalThreshold(); threshold > 0 {
			out.QuotaNotifyTotalThreshold = &threshold
		}
	}

	return out
}

func accountCredentialsForDTO(a *service.Account) (map[string]any, map[string]bool) {
	if a == nil {
		return nil, nil
	}
	if a.IsAnthropicOAuthOrSetupToken() && service.IsFormalPoolAccount(a) {
		out := map[string]any{}
		for _, key := range []string{"plan_type", "subscription_expires_at"} {
			if v, ok := a.Credentials[key]; ok {
				out[key] = v
			}
		}
		_, status := RedactCredentials(a.Credentials)
		return out, status
	}
	return RedactCredentials(a.Credentials)
}

func accountExtraForDTO(a *service.Account) map[string]any {
	if a == nil {
		return nil
	}
	if !a.IsAnthropicOAuthOrSetupToken() || !service.IsFormalPoolAccount(a) {
		return a.Extra
	}
	allowed := []string{
		service.FormalPoolExtraOnboardingStage,
		service.FormalPoolExtraOnboardingStageUpdatedAt,
		service.FormalPoolExtraOnboardingLastCheck,
		service.FormalPoolExtraOnboardingLastCheckAt,
		service.FormalPoolExtraOnboardingLastErrorCode,
		service.FormalPoolExtraOnboardingLastErrorBucket,
		service.FormalPoolExtraHealthcheckStatus,
		service.FormalPoolExtraHealthcheckStatusCodeBucket,
		service.FormalPoolExtraHealthcheckRawRef,
		service.FormalPoolExtraLastFailureOrigin,
		service.FormalPoolExtraLastFailureCode,
		service.FormalPoolExtraLastFailureSource,
		service.FormalPoolExtraLastCCGatewayErrorCode,
		service.FormalPoolExtraLastHealthcheckAt,
		service.FormalPoolExtraLastHealthcheckResult,
		service.FormalPoolExtraHealthcheckCCGatewaySeen,
		service.FormalPoolExtraHealthcheckFallbackDetected,
		service.FormalPoolExtraHealthcheckProxyMismatch,
		service.FormalPoolExtraHealthcheckRiskTextDetected,
		service.FormalPoolExtraHealthcheckSafeErrorCode,
		service.FormalPoolExtraHealthcheckSafeErrorBucket,
		service.FormalPoolExtraRateLimitErrorClass,
		service.FormalPoolExtraRateLimitWindow,
		service.FormalPoolExtraRateLimitAction,
		service.FormalPoolExtraRateLimitResetBucket,
		service.FormalPoolExtraRateLimitLastAt,
		service.FormalPoolExtraCredentialGeneration,
		service.FormalPoolExtraRepairedAt,
		service.FormalPoolExtraRepairedBy,
		service.FormalPoolExtraRuntimeRegistered,
		service.FormalPoolExtraRuntimeRegisteredAt,
		service.FormalPoolExtraWarmingStartedAt,
		service.FormalPoolExtraWarmingUntil,
		service.FormalPoolExtraPoolProfileRequested,
		service.FormalPoolExtraPoolProfileEffective,
		service.FormalPoolExtraPoolWeightMode,
		service.FormalPoolExtraRiskEventRef,
		service.FormalPoolExtraQuarantineReason,
		service.FormalPoolExtraQuarantineAt,
		"cc_gateway_enabled",
		"cc_gateway_canary_only",
		"cc_gateway_policy_version",
		"cc_gateway_routes",
		"cc_gateway_routes_deny",
		"cc_gateway_egress_bucket_enabled",
		"cc_gateway_egress_bucket",
		"cc_gateway_account_ref",
		"pool_profile",
		"oauth_refresh_fail_closed",
		"onboarding_state",
	}
	out := map[string]any{}
	for _, key := range allowed {
		if v, ok := a.Extra[key]; ok {
			switch key {
			case service.FormalPoolExtraHealthcheckRawRef, "cc_gateway_account_ref":
				if safe, ok := safeFormalPoolDTORef(v); ok {
					out[key] = safe
				}
			case "cc_gateway_egress_bucket":
				if safe, ok := safeFormalPoolDTOBucket(v); ok {
					out[key] = safe
				}
			case service.FormalPoolExtraLastFailureOrigin,
				service.FormalPoolExtraLastFailureCode,
				service.FormalPoolExtraLastFailureSource,
				service.FormalPoolExtraLastCCGatewayErrorCode,
				service.FormalPoolExtraLastHealthcheckAt,
				service.FormalPoolExtraLastHealthcheckResult,
				service.FormalPoolExtraRepairedAt,
				service.FormalPoolExtraRepairedBy,
				service.FormalPoolExtraQuarantineReason,
				service.FormalPoolExtraWarmingUntil,
				service.FormalPoolExtraHealthcheckSafeErrorCode,
				service.FormalPoolExtraHealthcheckSafeErrorBucket,
				service.FormalPoolExtraRateLimitErrorClass,
				service.FormalPoolExtraRateLimitWindow,
				service.FormalPoolExtraRateLimitAction,
				service.FormalPoolExtraRateLimitResetBucket,
				service.FormalPoolExtraRateLimitLastAt:
				if safe, ok := safeFormalPoolDTOText(v); ok {
					out[key] = safe
				}
			case service.FormalPoolExtraRiskEventRef:
				if safe, ok := safeFormalPoolDTORef(v); ok {
					out[key] = safe
				}
			case service.FormalPoolExtraHealthcheckCCGatewaySeen,
				service.FormalPoolExtraHealthcheckFallbackDetected,
				service.FormalPoolExtraHealthcheckProxyMismatch,
				service.FormalPoolExtraHealthcheckRiskTextDetected:
				if safe, ok := safeFormalPoolDTOBool(v); ok {
					out[key] = safe
				}
			case service.FormalPoolExtraCredentialGeneration:
				if safe, ok := safeFormalPoolDTOInt(v); ok {
					out[key] = safe
				}
			default:
				out[key] = v
			}
		}
	}
	return out
}

var (
	formalPoolDTOHMACRefRe      = regexp.MustCompile(`^hmac-sha256:[0-9a-f]{64}$`)
	formalPoolDTOUUIDLikeRe     = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	formalPoolDTOEmailLikeRe    = regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`)
	formalPoolDTOURLLikeRe      = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://`)
	formalPoolDTOClaudeBucketRe = regexp.MustCompile(`^claude-[0-9a-f]{16}$`)
	formalPoolDTOLocalBucketRe  = regexp.MustCompile(`^bucket-[A-Za-z0-9_-]{1,56}$`)
	formalPoolDTOSensitiveRe    = regexp.MustCompile(`(?i)(authorization|access[_ -]?token|refresh[_ -]?token|id[_ -]?token|raw[_ -]?token|token\s*[:=]|x-api-key|cookie|cch|credential|password|passwd|secret|client[_ -]?secret|proxy[_ -]?url|proxy[_ -]?credential|bearer)`)
)

func safeFormalPoolDTORef(v any) (string, bool) {
	ref, ok := formalPoolDTOString(v)
	if !ok || formalPoolDTOUnsafeText(ref) {
		return "", false
	}
	if formalPoolDTOHMACRefRe.MatchString(ref) {
		return ref, true
	}
	if strings.HasPrefix(ref, "opaque:") || strings.HasPrefix(ref, "scoped:") || strings.HasPrefix(ref, "scoped_hmac_ref:") {
		return ref, true
	}
	return "", false
}

func safeFormalPoolDTOBucket(v any) (string, bool) {
	bucket, ok := formalPoolDTOString(v)
	if !ok || formalPoolDTOUnsafeText(bucket) {
		return "", false
	}
	if formalPoolDTOClaudeBucketRe.MatchString(bucket) {
		return bucket, true
	}
	if _, ok := safeFormalPoolDTORef(bucket); ok {
		return bucket, true
	}
	// Keep compatibility with local/test bucket IDs such as "bucket-a", but do
	// not expose hosts, URLs, credentials, UUIDs, emails, or token-like values.
	if formalPoolDTOLocalBucketRe.MatchString(bucket) {
		return bucket, true
	}
	return "", false
}

func safeFormalPoolDTOText(v any) (string, bool) {
	s, ok := formalPoolDTOString(v)
	if !ok || formalPoolDTOUnsafeText(s) {
		return "", false
	}
	return s, true
}

func safeFormalPoolDTOBool(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		x = strings.ToLower(strings.TrimSpace(x))
		switch x {
		case "true", "1", "yes":
			return true, true
		case "false", "0", "no":
			return false, true
		default:
			return false, false
		}
	case int:
		if x == 0 {
			return false, true
		}
		if x == 1 {
			return true, true
		}
	case int64:
		if x == 0 {
			return false, true
		}
		if x == 1 {
			return true, true
		}
	case float64:
		if x == 0 {
			return false, true
		}
		if x == 1 {
			return true, true
		}
	}
	return false, false
}

func safeFormalPoolDTOInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case int32:
		return int(x), true
	case float64:
		return int(x), true
	case float32:
		return int(x), true
	case string:
		s := strings.TrimSpace(x)
		if formalPoolDTOUnsafeText(s) {
			return 0, false
		}
		n, err := strconv.Atoi(s)
		if err == nil {
			return n, true
		}
	}
	return 0, false
}

func formalPoolDTOString(v any) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	s = strings.TrimSpace(s)
	return s, s != ""
}

func formalPoolDTOUnsafeText(s string) bool {
	if strings.ContainsAny(s, "\r\n\t") {
		return true
	}
	lower := strings.ToLower(s)
	if formalPoolDTOURLLikeRe.MatchString(s) || strings.Contains(s, "://") || strings.Contains(s, "@") {
		return true
	}
	for _, marker := range []string{"raw_body", "raw body", "raw-body", "raw_prompt", "raw prompt", "raw-prompt", "raw_telemetry", "raw telemetry", "raw-telemetry", "raw_cch", "raw cch", "raw-cch", "raw_token", "raw token", "raw-token", "sk-ant-sid"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return formalPoolDTOUUIDLikeRe.MatchString(s) || formalPoolDTOEmailLikeRe.MatchString(s) || formalPoolDTOSensitiveRe.MatchString(s)
}

func AccountFromService(a *service.Account) *Account {
	if a == nil {
		return nil
	}
	out := AccountFromServiceShallow(a)
	if !service.IsFormalPoolAccount(a) {
		out.Proxy = ProxyFromService(a.Proxy)
	}
	if len(a.AccountGroups) > 0 {
		out.AccountGroups = make([]AccountGroup, 0, len(a.AccountGroups))
		for i := range a.AccountGroups {
			ag := a.AccountGroups[i]
			out.AccountGroups = append(out.AccountGroups, *AccountGroupFromService(&ag))
		}
	}
	if len(a.Groups) > 0 {
		out.Groups = make([]*Group, 0, len(a.Groups))
		for _, g := range a.Groups {
			out.Groups = append(out.Groups, GroupFromServiceShallow(g))
		}
	}
	return out
}

func timeToUnixSeconds(value *time.Time) *int64 {
	if value == nil {
		return nil
	}
	ts := value.Unix()
	return &ts
}

func AccountGroupFromService(ag *service.AccountGroup) *AccountGroup {
	if ag == nil {
		return nil
	}
	return &AccountGroup{
		AccountID: ag.AccountID,
		GroupID:   ag.GroupID,
		Priority:  ag.Priority,
		CreatedAt: ag.CreatedAt,
		Account:   AccountFromServiceShallow(ag.Account),
		Group:     GroupFromServiceShallow(ag.Group),
	}
}

func ProxyFromService(p *service.Proxy) *Proxy {
	if p == nil {
		return nil
	}
	return &Proxy{
		ID:             p.ID,
		Name:           p.Name,
		Protocol:       p.Protocol,
		Host:           p.Host,
		Port:           p.Port,
		Username:       p.Username,
		Status:         p.Status,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
		ExpiresAt:      p.ExpiresAt,
		FallbackMode:   p.FallbackMode,
		BackupProxyID:  p.BackupProxyID,
		ExpiryWarnDays: p.ExpiryWarnDays,
	}
}

func ProxyWithAccountCountFromService(p *service.ProxyWithAccountCount) *ProxyWithAccountCount {
	if p == nil {
		return nil
	}
	return &ProxyWithAccountCount{
		Proxy:          *ProxyFromService(&p.Proxy),
		AccountCount:   p.AccountCount,
		LatencyMs:      p.LatencyMs,
		LatencyStatus:  p.LatencyStatus,
		LatencyMessage: p.LatencyMessage,
		IPAddress:      p.IPAddress,
		Country:        p.Country,
		CountryCode:    p.CountryCode,
		Region:         p.Region,
		City:           p.City,
		QualityStatus:  p.QualityStatus,
		QualityScore:   p.QualityScore,
		QualityGrade:   p.QualityGrade,
		QualitySummary: p.QualitySummary,
		QualityChecked: p.QualityChecked,
	}
}

// ProxyFromServiceAdmin converts a service Proxy to AdminProxy DTO for admin users.
// It includes the password field - user-facing endpoints must not use this.
func ProxyFromServiceAdmin(p *service.Proxy) *AdminProxy {
	if p == nil {
		return nil
	}
	base := ProxyFromService(p)
	if base == nil {
		return nil
	}
	return &AdminProxy{
		Proxy:    *base,
		Password: p.Password,
	}
}

// ProxyWithAccountCountFromServiceAdmin converts a service ProxyWithAccountCount to AdminProxyWithAccountCount DTO.
// It includes the password field - user-facing endpoints must not use this.
func ProxyWithAccountCountFromServiceAdmin(p *service.ProxyWithAccountCount) *AdminProxyWithAccountCount {
	if p == nil {
		return nil
	}
	admin := ProxyFromServiceAdmin(&p.Proxy)
	if admin == nil {
		return nil
	}
	return &AdminProxyWithAccountCount{
		AdminProxy:     *admin,
		AccountCount:   p.AccountCount,
		LatencyMs:      p.LatencyMs,
		LatencyStatus:  p.LatencyStatus,
		LatencyMessage: p.LatencyMessage,
		IPAddress:      p.IPAddress,
		Country:        p.Country,
		CountryCode:    p.CountryCode,
		Region:         p.Region,
		City:           p.City,
		QualityStatus:  p.QualityStatus,
		QualityScore:   p.QualityScore,
		QualityGrade:   p.QualityGrade,
		QualitySummary: p.QualitySummary,
		QualityChecked: p.QualityChecked,
	}
}

func ProxyAccountSummaryFromService(a *service.ProxyAccountSummary) *ProxyAccountSummary {
	if a == nil {
		return nil
	}
	return &ProxyAccountSummary{
		ID:       a.ID,
		Name:     a.Name,
		Platform: a.Platform,
		Type:     a.Type,
		Notes:    a.Notes,
	}
}

func RedeemCodeFromService(rc *service.RedeemCode) *RedeemCode {
	if rc == nil {
		return nil
	}
	out := redeemCodeFromServiceBase(rc)
	return &out
}

// RedeemCodeFromServiceAdmin converts a service RedeemCode to DTO for admin users.
// It includes notes - user-facing endpoints must not use this.
func RedeemCodeFromServiceAdmin(rc *service.RedeemCode) *AdminRedeemCode {
	if rc == nil {
		return nil
	}
	return &AdminRedeemCode{
		RedeemCode: redeemCodeFromServiceBase(rc),
		Notes:      rc.Notes,
	}
}

func redeemCodeFromServiceBase(rc *service.RedeemCode) RedeemCode {
	out := RedeemCode{
		ID:           rc.ID,
		Code:         rc.Code,
		Type:         rc.Type,
		Value:        rc.Value,
		Status:       rc.Status,
		UsedBy:       rc.UsedBy,
		UsedAt:       rc.UsedAt,
		CreatedAt:    rc.CreatedAt,
		ExpiresAt:    rc.ExpiresAt,
		GroupID:      rc.GroupID,
		ValidityDays: rc.ValidityDays,
		User:         UserFromServiceShallow(rc.User),
		Group:        GroupFromServiceShallow(rc.Group),
	}
	if rc.IsExpired() {
		out.Status = service.StatusExpired
	}

	// For admin_balance/admin_concurrency types, include notes so users can see
	// why they were charged or credited by admin
	if (rc.Type == "admin_balance" || rc.Type == "admin_concurrency") && rc.Notes != "" {
		out.Notes = &rc.Notes
	}

	return out
}

// AccountSummaryFromService returns a minimal AccountSummary for usage log display.
// Only includes ID and Name - no sensitive fields like Credentials, Proxy, etc.
func AccountSummaryFromService(a *service.Account) *AccountSummary {
	if a == nil {
		return nil
	}
	return &AccountSummary{
		ID:   a.ID,
		Name: a.Name,
	}
}

func usageLogFromServiceUser(l *service.UsageLog) UsageLog {
	// 普通用户 DTO：严禁包含管理员字段（例如 account_rate_multiplier、ip_address、account）。
	requestType := l.EffectiveRequestType()
	stream, openAIWSMode := service.ApplyLegacyRequestFields(requestType, l.Stream, l.OpenAIWSMode)
	requestedModel := l.RequestedModel
	if requestedModel == "" {
		requestedModel = l.Model
	}
	return UsageLog{
		ID:                    l.ID,
		UserID:                l.UserID,
		APIKeyID:              l.APIKeyID,
		AccountID:             l.AccountID,
		RequestID:             l.RequestID,
		Model:                 requestedModel,
		EntityID:              l.EntityID,
		EntityType:            l.EntityType,
		ClaimedEntityID:       l.ClaimedEntityID,
		ServiceTier:           l.ServiceTier,
		ReasoningEffort:       l.ReasoningEffort,
		InboundEndpoint:       l.InboundEndpoint,
		UpstreamEndpoint:      l.UpstreamEndpoint,
		GroupID:               l.GroupID,
		SubscriptionID:        l.SubscriptionID,
		InputTokens:           l.InputTokens,
		OutputTokens:          l.OutputTokens,
		CacheCreationTokens:   l.CacheCreationTokens,
		CacheReadTokens:       l.CacheReadTokens,
		CacheCreation5mTokens: l.CacheCreation5mTokens,
		CacheCreation1hTokens: l.CacheCreation1hTokens,
		InputCost:             l.InputCost,
		OutputCost:            l.OutputCost,
		CacheCreationCost:     l.CacheCreationCost,
		CacheReadCost:         l.CacheReadCost,
		TotalCost:             l.TotalCost,
		ActualCost:            l.ActualCost,
		RateMultiplier:        l.RateMultiplier,
		BillingType:           l.BillingType,
		RequestType:           requestType.String(),
		Stream:                stream,
		OpenAIWSMode:          openAIWSMode,
		DurationMs:            l.DurationMs,
		FirstTokenMs:          l.FirstTokenMs,
		ImageCount:            l.ImageCount,
		ImageSize:             l.ImageSize,
		ImageInputSize:        l.ImageInputSize,
		ImageOutputSize:       l.ImageOutputSize,
		ImageOutputTokens:     l.ImageOutputTokens,
		ImageOutputCost:       l.ImageOutputCost,
		ImageSizeSource:       l.ImageSizeSource,
		ImageSizeBreakdown:    l.ImageSizeBreakdown,
		MediaType:             l.MediaType,
		UserAgent:             l.UserAgent,
		CacheTTLOverridden:    l.CacheTTLOverridden,
		BillingMode:           l.BillingMode,
		CreatedAt:             l.CreatedAt,
		User:                  UserFromServiceShallow(l.User),
		APIKey:                APIKeyFromService(l.APIKey),
		Group:                 GroupFromServiceShallow(l.Group),
		Subscription:          UserSubscriptionFromService(l.Subscription),
	}
}

// UsageLogFromService converts a service UsageLog to DTO for regular users.
// It excludes Account details and IP address - users should not see these.
func UsageLogFromService(l *service.UsageLog) *UsageLog {
	if l == nil {
		return nil
	}
	u := usageLogFromServiceUser(l)
	return &u
}

// UsageLogFromServiceAdmin converts a service UsageLog to DTO for admin users.
// It includes minimal Account info (ID, Name only) and IP address.
func UsageLogFromServiceAdmin(l *service.UsageLog) *AdminUsageLog {
	if l == nil {
		return nil
	}
	providerPromptCacheStatus, providerPromptCacheDetail := providerPromptCacheDiagnosticsFromUsageLog(l)
	return &AdminUsageLog{
		UsageLog:                  usageLogFromServiceUser(l),
		UpstreamModel:             l.UpstreamModel,
		ChannelID:                 l.ChannelID,
		ModelMappingChain:         l.ModelMappingChain,
		BillingTier:               l.BillingTier,
		ProviderPromptCacheStatus: providerPromptCacheStatus,
		ProviderPromptCacheDetail: providerPromptCacheDetail,
		AccountRateMultiplier:     l.AccountRateMultiplier,
		AccountStatsCost:          l.AccountStatsCost,
		IPAddress:                 l.IPAddress,
		Account:                   AccountSummaryFromService(l.Account),
	}
}

func providerPromptCacheDiagnosticsFromUsageLog(l *service.UsageLog) (*string, *string) {
	if l == nil || l.ClientProduct == nil || *l.ClientProduct != service.CodexUsageClientProduct {
		return nil, nil
	}
	if l.FeatureScope == nil || *l.FeatureScope != string(service.CodexGatewayProviderAgnes) {
		return nil, nil
	}
	status := "unsupported"
	detail := "AGNES upstream usage does not expose provider prompt cache hit fields; zero cache tokens mean unsupported/unknown, not a confirmed cold miss."
	return &status, &detail
}

func UsageCleanupTaskFromService(task *service.UsageCleanupTask) *UsageCleanupTask {
	if task == nil {
		return nil
	}
	return &UsageCleanupTask{
		ID:     task.ID,
		Status: task.Status,
		Filters: UsageCleanupFilters{
			StartTime:   task.Filters.StartTime,
			EndTime:     task.Filters.EndTime,
			UserID:      task.Filters.UserID,
			APIKeyID:    task.Filters.APIKeyID,
			AccountID:   task.Filters.AccountID,
			GroupID:     task.Filters.GroupID,
			Model:       task.Filters.Model,
			RequestType: requestTypeStringPtr(task.Filters.RequestType),
			Stream:      task.Filters.Stream,
			BillingType: task.Filters.BillingType,
		},
		CreatedBy:    task.CreatedBy,
		DeletedRows:  task.DeletedRows,
		ErrorMessage: task.ErrorMsg,
		CanceledBy:   task.CanceledBy,
		CanceledAt:   task.CanceledAt,
		StartedAt:    task.StartedAt,
		FinishedAt:   task.FinishedAt,
		CreatedAt:    task.CreatedAt,
		UpdatedAt:    task.UpdatedAt,
	}
}

func requestTypeStringPtr(requestType *int16) *string {
	if requestType == nil {
		return nil
	}
	value := service.RequestTypeFromInt16(*requestType).String()
	return &value
}

func SettingFromService(s *service.Setting) *Setting {
	if s == nil {
		return nil
	}
	return &Setting{
		ID:        s.ID,
		Key:       s.Key,
		Value:     s.Value,
		UpdatedAt: s.UpdatedAt,
	}
}

func UserSubscriptionFromService(sub *service.UserSubscription) *UserSubscription {
	if sub == nil {
		return nil
	}
	out := userSubscriptionFromServiceBase(sub)
	return &out
}

// UserSubscriptionFromServiceAdmin converts a service UserSubscription to DTO for admin users.
// It includes assignment metadata and notes.
func UserSubscriptionFromServiceAdmin(sub *service.UserSubscription) *AdminUserSubscription {
	if sub == nil {
		return nil
	}
	return &AdminUserSubscription{
		UserSubscription: userSubscriptionFromServiceBase(sub),
		AssignedBy:       sub.AssignedBy,
		AssignedAt:       sub.AssignedAt,
		Notes:            sub.Notes,
		AssignedByUser:   UserFromServiceShallow(sub.AssignedByUser),
	}
}

func userSubscriptionFromServiceBase(sub *service.UserSubscription) UserSubscription {
	return UserSubscription{
		ID:                 sub.ID,
		UserID:             sub.UserID,
		GroupID:            sub.GroupID,
		StartsAt:           sub.StartsAt,
		ExpiresAt:          sub.ExpiresAt,
		Status:             sub.Status,
		DailyWindowStart:   sub.DailyWindowStart,
		WeeklyWindowStart:  sub.WeeklyWindowStart,
		MonthlyWindowStart: sub.MonthlyWindowStart,
		DailyUsageUSD:      sub.DailyUsageUSD,
		WeeklyUsageUSD:     sub.WeeklyUsageUSD,
		MonthlyUsageUSD:    sub.MonthlyUsageUSD,
		CreatedAt:          sub.CreatedAt,
		UpdatedAt:          sub.UpdatedAt,
		User:               UserFromServiceShallow(sub.User),
		Group:              GroupFromServiceShallow(sub.Group),
	}
}

func BulkAssignResultFromService(r *service.BulkAssignResult) *BulkAssignResult {
	if r == nil {
		return nil
	}
	subs := make([]AdminUserSubscription, 0, len(r.Subscriptions))
	for i := range r.Subscriptions {
		subs = append(subs, *UserSubscriptionFromServiceAdmin(&r.Subscriptions[i]))
	}
	statuses := make(map[string]string, len(r.Statuses))
	for userID, status := range r.Statuses {
		statuses[strconv.FormatInt(userID, 10)] = status
	}
	return &BulkAssignResult{
		SuccessCount:  r.SuccessCount,
		CreatedCount:  r.CreatedCount,
		ReusedCount:   r.ReusedCount,
		FailedCount:   r.FailedCount,
		Subscriptions: subs,
		Errors:        r.Errors,
		Statuses:      statuses,
	}
}

func PromoCodeFromService(pc *service.PromoCode) *PromoCode {
	if pc == nil {
		return nil
	}
	return &PromoCode{
		ID:          pc.ID,
		Code:        pc.Code,
		BonusAmount: pc.BonusAmount,
		MaxUses:     pc.MaxUses,
		UsedCount:   pc.UsedCount,
		Status:      pc.Status,
		ExpiresAt:   pc.ExpiresAt,
		Notes:       pc.Notes,
		CreatedAt:   pc.CreatedAt,
		UpdatedAt:   pc.UpdatedAt,
	}
}

func PromoCodeUsageFromService(u *service.PromoCodeUsage) *PromoCodeUsage {
	if u == nil {
		return nil
	}
	return &PromoCodeUsage{
		ID:          u.ID,
		PromoCodeID: u.PromoCodeID,
		UserID:      u.UserID,
		BonusAmount: u.BonusAmount,
		UsedAt:      u.UsedAt,
		User:        UserFromServiceShallow(u.User),
	}
}

func applyFormalPoolAccountFields(out *Account, a *service.Account) {
	if out == nil || a == nil || !a.IsAnthropicOAuthOrSetupToken() {
		return
	}
	stage := a.GetExtraString(service.FormalPoolExtraOnboardingStage)
	if stage == "" && a.Extra != nil {
		stage = service.FormalPoolStageLegacyUnknown
	}
	out.OnboardingStage = stage
	out.PoolProfileRequested = a.GetExtraString(service.FormalPoolExtraPoolProfileRequested)
	out.PoolProfileEffective = a.GetExtraString(service.FormalPoolExtraPoolProfileEffective)
	out.PoolWeightMode = a.GetExtraString(service.FormalPoolExtraPoolWeightMode)
	out.HealthcheckStatus = a.GetExtraString(service.FormalPoolExtraHealthcheckStatus)
	out.HealthcheckLastStatusCodeBucket = a.GetExtraString(service.FormalPoolExtraHealthcheckStatusCodeBucket)
	out.FormalPoolLastFailureOrigin = safeFormalPoolAccountText(a, service.FormalPoolExtraLastFailureOrigin)
	out.FormalPoolLastFailureCode = safeFormalPoolAccountText(a, service.FormalPoolExtraLastFailureCode)
	out.FormalPoolLastFailureSource = safeFormalPoolAccountText(a, service.FormalPoolExtraLastFailureSource)
	out.FormalPoolLastCCGatewayErrorCode = safeFormalPoolAccountText(a, service.FormalPoolExtraLastCCGatewayErrorCode)
	out.FormalPoolLastHealthcheckAt = safeFormalPoolAccountText(a, service.FormalPoolExtraLastHealthcheckAt)
	out.FormalPoolLastHealthcheckResult = safeFormalPoolAccountText(a, service.FormalPoolExtraLastHealthcheckResult)
	out.HealthcheckCCGatewaySeen, _ = safeFormalPoolDTOBool(a.Extra[service.FormalPoolExtraHealthcheckCCGatewaySeen])
	out.HealthcheckFallbackDetected, _ = safeFormalPoolDTOBool(a.Extra[service.FormalPoolExtraHealthcheckFallbackDetected])
	out.HealthcheckProxyMismatch, _ = safeFormalPoolDTOBool(a.Extra[service.FormalPoolExtraHealthcheckProxyMismatch])
	out.HealthcheckRiskTextDetected, _ = safeFormalPoolDTOBool(a.Extra[service.FormalPoolExtraHealthcheckRiskTextDetected])
	out.HealthcheckSafeErrorCode = safeFormalPoolAccountText(a, service.FormalPoolExtraHealthcheckSafeErrorCode)
	out.HealthcheckSafeErrorBucket = safeFormalPoolAccountText(a, service.FormalPoolExtraHealthcheckSafeErrorBucket)
	out.FormalPoolRateLimitErrorClass = safeFormalPoolAccountText(a, service.FormalPoolExtraRateLimitErrorClass)
	out.FormalPoolRateLimitWindow = safeFormalPoolAccountText(a, service.FormalPoolExtraRateLimitWindow)
	out.FormalPoolRateLimitAction = safeFormalPoolAccountText(a, service.FormalPoolExtraRateLimitAction)
	out.FormalPoolRateLimitResetBucket = safeFormalPoolAccountText(a, service.FormalPoolExtraRateLimitResetBucket)
	out.FormalPoolRateLimitLastAt = safeFormalPoolAccountText(a, service.FormalPoolExtraRateLimitLastAt)
	if gen, ok := safeFormalPoolDTOInt(a.Extra[service.FormalPoolExtraCredentialGeneration]); ok {
		out.FormalPoolCredentialGeneration = gen
	}
	out.FormalPoolRepairedAt = safeFormalPoolAccountText(a, service.FormalPoolExtraRepairedAt)
	out.FormalPoolRepairedBy = safeFormalPoolAccountText(a, service.FormalPoolExtraRepairedBy)
	out.CCGatewayRuntimeRegistered = formalPoolDTOBool(a.Extra[service.FormalPoolExtraRuntimeRegistered])
	out.QuarantineReason = safeFormalPoolAccountText(a, service.FormalPoolExtraQuarantineReason)
	out.RiskEventRef = safeFormalPoolAccountRef(a, service.FormalPoolExtraRiskEventRef)
	out.WarmingUntil = safeFormalPoolAccountText(a, service.FormalPoolExtraWarmingUntil)
	out.ProductionReady = stage == service.FormalPoolStageProduction
}

func safeFormalPoolAccountRef(a *service.Account, key string) string {
	if a == nil {
		return ""
	}
	safe, ok := safeFormalPoolDTORef(a.Extra[key])
	if !ok {
		return ""
	}
	return safe
}

func safeFormalPoolAccountText(a *service.Account, key string) string {
	if a == nil {
		return ""
	}
	safe, ok := safeFormalPoolDTOText(a.Extra[key])
	if !ok {
		return ""
	}
	return safe
}

func intFromAny(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case int32:
		return int(x)
	case float64:
		return int(x)
	case float32:
		return int(x)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err == nil {
			return n
		}
	}
	return 0
}

func formalPoolDTOBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true" || x == "1" || x == "yes"
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	default:
		return false
	}
}
