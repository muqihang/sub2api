package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// NewAPIKeyAuthMiddleware 创建 API Key 认证中间件
func NewAPIKeyAuthMiddleware(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, cfg *config.Config) APIKeyAuthMiddleware {
	return APIKeyAuthMiddleware(apiKeyAuthWithSubscription(apiKeyService, subscriptionService, cfg))
}

// NewCodexGatewayAPIKeyAuthMiddleware creates a Codex-specific auth middleware
// that preserves existing semantics but writes Responses-compatible errors.
func NewCodexGatewayAPIKeyAuthMiddleware(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, cfg *config.Config) APIKeyAuthMiddleware {
	return APIKeyAuthMiddleware(apiKeyAuthWithSubscriptionAndErrorWriter(apiKeyService, subscriptionService, cfg, codexGatewayAPIKeyAuthErrorWriter))
}

type apiKeyAuthErrorWriter func(c *gin.Context, status int, code, message string)

// apiKeyAuthWithSubscription API Key认证中间件（支持订阅验证）
//
// 中间件职责分为两层：
//   - 鉴权（Authentication）：验证 Key 有效性、用户状态、IP 限制 —— 始终执行
//   - 计费执行（Billing Enforcement）：过期/配额/订阅/余额检查 —— skipBilling 时整块跳过
//
// /v1/usage 端点只需鉴权，不需要计费执行（允许过期/配额耗尽的 Key 查询自身用量）。
func apiKeyAuthWithSubscription(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, cfg *config.Config) gin.HandlerFunc {
	return apiKeyAuthWithSubscriptionAndErrorWriter(apiKeyService, subscriptionService, cfg, defaultAPIKeyAuthErrorWriter)
}

func defaultAPIKeyAuthErrorWriter(c *gin.Context, status int, code, message string) {
	AbortWithError(c, status, code, message)
}

func codexGatewayAPIKeyAuthErrorWriter(c *gin.Context, status int, code, message string) {
	errorType := service.CodexGatewayErrorTypeAuthentication
	switch status {
	case http.StatusBadRequest:
		errorType = service.CodexGatewayErrorTypeInvalidRequest
	case http.StatusTooManyRequests:
		errorType = service.CodexGatewayErrorTypeRateLimit
	case http.StatusInternalServerError:
		errorType = service.CodexGatewayErrorTypeAPI
	}
	normalizedCode := strings.ToLower(strings.TrimSpace(code))
	if normalizedCode == "" {
		normalizedCode = service.CodexGatewayErrorCodeInvalidRequest
	}
	service.WriteCodexGatewayErrorJSON(c.Writer, status, errorType, normalizedCode, message)
	c.Abort()
}

func apiKeyAuthWithSubscriptionAndErrorWriter(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, cfg *config.Config, writeError apiKeyAuthErrorWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ── 1. 提取 API Key ──────────────────────────────────────────

		queryKey := strings.TrimSpace(c.Query("key"))
		queryApiKey := strings.TrimSpace(c.Query("api_key"))
		if queryKey != "" || queryApiKey != "" {
			writeError(c, 400, "api_key_in_query_deprecated", "API key in query parameter is deprecated. Please use Authorization header instead.")
			return
		}

		// 尝试从Authorization header中提取API key (Bearer scheme)
		authHeader := c.GetHeader("Authorization")
		var apiKeyString string
		apiKeySource := ""

		if authHeader != "" {
			// 验证Bearer scheme
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				apiKeyString = strings.TrimSpace(parts[1])
				apiKeySource = "authorization"
			}
		}

		// 如果Authorization header中没有，尝试从x-api-key header中提取
		if apiKeyString == "" {
			apiKeyString = c.GetHeader("x-api-key")
			if strings.TrimSpace(apiKeyString) != "" {
				apiKeySource = "x-api-key"
			}
		}

		// 如果x-api-key header中没有，尝试从x-goog-api-key header中提取（Gemini CLI兼容）
		if apiKeyString == "" {
			apiKeyString = c.GetHeader("x-goog-api-key")
			if strings.TrimSpace(apiKeyString) != "" {
				apiKeySource = "x-goog-api-key"
			}
		}

		// 如果所有header都没有API key
		if apiKeyString == "" {
			writeError(c, 401, "API_KEY_REQUIRED", "API key is required in Authorization header (Bearer scheme), x-api-key header, or x-goog-api-key header")
			return
		}

		// ── 2. 验证 Key 存在 ─────────────────────────────────────────

		apiKey, err := apiKeyService.GetByKey(c.Request.Context(), apiKeyString)
		if err != nil {
			if errors.Is(err, service.ErrAPIKeyNotFound) {
				writeError(c, 401, "INVALID_API_KEY", "Invalid API key")
				return
			}
			writeError(c, 500, "INTERNAL_ERROR", "Failed to validate API key")
			return
		}

		if !validateAndSetAPIKeyContext(c, apiKey, apiKeyService, subscriptionService, cfg, apiKeySource, apiKeyString, writeError) {
			return
		}
		c.Next()
	}
}

func validateAndSetAPIKeyContext(
	c *gin.Context,
	apiKey *service.APIKey,
	apiKeyService *service.APIKeyService,
	subscriptionService *service.SubscriptionService,
	cfg *config.Config,
	apiKeySource string,
	apiKeyString string,
	writeError apiKeyAuthErrorWriter,
) bool {
	if cfg == nil {
		cfg = &config.Config{}
	}
	if writeError == nil {
		writeError = defaultAPIKeyAuthErrorWriter
	}

	// 记录已加载的 API Key，供 Ops 错误日志在鉴权早退时安全回退使用。
	SetOpsFallbackAPIKey(c, apiKey)

	// ── 3. 基础鉴权（始终执行） ─────────────────────────────────

	if !apiKey.IsActive() &&
		apiKey.Status != service.StatusAPIKeyExpired &&
		apiKey.Status != service.StatusAPIKeyQuotaExhausted {
		logAPIKeyAuthFailure(c, apiKeySource, apiKeyString, "api_key_disabled", apiKey)
		writeError(c, 401, "API_KEY_DISABLED", "API key is disabled")
		return false
	}

	if len(apiKey.IPWhitelist) > 0 || len(apiKey.IPBlacklist) > 0 {
		clientIP := ip.GetTrustedClientIP(c)
		if cfg.TrustForwardedIPForAPIKeyACL() {
			clientIP = ip.GetClientIP(c)
		}
		allowed, _ := ip.CheckIPRestrictionWithCompiledRules(clientIP, apiKey.CompiledIPWhitelist, apiKey.CompiledIPBlacklist)
		if !allowed {
			if clientIP == "" {
				clientIP = "unknown"
			}
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonIPRestriction)
			writeError(c, 403, "ACCESS_DENIED", "Access denied. Your IP is "+clientIP)
			return false
		}
	}

	if apiKey.User == nil {
		logAPIKeyAuthFailure(c, apiKeySource, apiKeyString, "user_not_found", apiKey)
		writeError(c, 401, "USER_NOT_FOUND", "User associated with API key not found")
		return false
	}

	if !apiKey.User.IsActive() {
		logAPIKeyAuthFailure(c, apiKeySource, apiKeyString, "user_inactive", apiKey)
		writeError(c, 401, "USER_INACTIVE", "User account is not active")
		return false
	}

	if code, message, ok := validateAPIKeyGroupAvailable(apiKey); !ok {
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonAPIKeyGroupUnavailable)
		writeError(c, 403, code, message)
		return false
	}
	if !validateAPIKeyGroupAllowed(apiKey) {
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonAPIKeyGroupUnavailable)
		writeError(c, 403, "GROUP_NOT_ALLOWED", "API Key 所属专属分组不再允许当前用户使用")
		return false
	}

	if cfg.RunMode == config.RunModeSimple {
		setAPIKeyContext(c, apiKey, nil)
		if apiKeyService != nil {
			_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)
		}
		return true
	}

	skipBilling := c.Request.URL.Path == "/v1/usage"

	var subscription *service.UserSubscription
	isSubscriptionType := apiKey.Group != nil && apiKey.Group.IsSubscriptionType()
	if isSubscriptionType && subscriptionService != nil {
		sub, subErr := subscriptionService.GetActiveSubscription(
			c.Request.Context(),
			apiKey.User.ID,
			apiKey.Group.ID,
		)
		if subErr != nil {
			if !skipBilling {
				writeError(c, 403, "SUBSCRIPTION_NOT_FOUND", "No active subscription found for this group")
				return false
			}
		} else {
			subscription = sub
		}
	}

	if !skipBilling {
		switch apiKey.Status {
		case service.StatusAPIKeyQuotaExhausted:
			writeError(c, 429, "API_KEY_QUOTA_EXHAUSTED", "API key 额度已用完")
			return false
		case service.StatusAPIKeyExpired:
			writeError(c, 403, "API_KEY_EXPIRED", "API key 已过期")
			return false
		}

		if apiKey.IsExpired() {
			writeError(c, 403, "API_KEY_EXPIRED", "API key 已过期")
			return false
		}
		if apiKey.IsQuotaExhausted() {
			writeError(c, 429, "API_KEY_QUOTA_EXHAUSTED", "API key 额度已用完")
			return false
		}

		if subscription != nil {
			needsMaintenance, validateErr := subscriptionService.ValidateAndCheckLimits(subscription, apiKey.Group)
			if validateErr != nil {
				code := "SUBSCRIPTION_INVALID"
				status := 403
				if errors.Is(validateErr, service.ErrDailyLimitExceeded) ||
					errors.Is(validateErr, service.ErrWeeklyLimitExceeded) ||
					errors.Is(validateErr, service.ErrMonthlyLimitExceeded) {
					code = "USAGE_LIMIT_EXCEEDED"
					status = 429
				}
				writeError(c, status, code, validateErr.Error())
				return false
			}
			if needsMaintenance {
				maintenanceCopy := *subscription
				subscriptionService.DoWindowMaintenance(&maintenanceCopy)
			}
		} else if apiKey.User.Balance <= 0 {
			writeError(c, 403, "INSUFFICIENT_BALANCE", "Insufficient account balance")
			return false
		}
	}

	setAPIKeyContext(c, apiKey, subscription)
	if apiKeyService != nil {
		_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)
	}
	return true
}

func setAPIKeyContext(c *gin.Context, apiKey *service.APIKey, subscription *service.UserSubscription) {
	if subscription != nil {
		c.Set(string(ContextKeySubscription), subscription)
	}
	c.Set(string(ContextKeyAPIKey), apiKey)
	c.Set(string(ContextKeyUser), AuthSubject{
		UserID:      apiKey.User.ID,
		Concurrency: apiKey.User.Concurrency,
	})
	c.Set(string(ContextKeyUserRole), apiKey.User.Role)
	setGroupContext(c, apiKey.Group)
}

func fingerprintAPIKeyForLog(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func logAPIKeyAuthFailure(c *gin.Context, source, apiKey string, reason string, resolvedKey *service.APIKey) {
	fields := []any{
		"auth_reason", strings.TrimSpace(reason),
		"api_key_source", strings.TrimSpace(source),
		"api_key_fingerprint", fingerprintAPIKeyForLog(apiKey),
		"api_key_length", len(strings.TrimSpace(apiKey)),
	}
	if c != nil && c.Request != nil {
		fields = append(fields,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
		)
	}
	if resolvedKey != nil {
		fields = append(fields,
			"api_key_id", resolvedKey.ID,
			"user_id", resolvedKey.UserID,
			"api_key_status", resolvedKey.Status,
		)
	}
	slog.Warn("api_key_auth_rejected", fields...)
}

// GetAPIKeyFromContext 从上下文中获取API key
func GetAPIKeyFromContext(c *gin.Context) (*service.APIKey, bool) {
	value, exists := c.Get(string(ContextKeyAPIKey))
	if !exists {
		return nil, false
	}
	apiKey, ok := value.(*service.APIKey)
	return apiKey, ok
}

// SetOpsFallbackAPIKey 记录已加载的 API Key，供 Ops 错误日志在鉴权早退时回退使用。
// 与 ContextKeyAPIKey 区分：写入它不代表请求已通过鉴权，因此不影响 handler、
// 审计日志等对“已鉴权”的判断。
func SetOpsFallbackAPIKey(c *gin.Context, apiKey *service.APIKey) {
	if c == nil || apiKey == nil {
		return
	}
	c.Set(string(ContextKeyOpsFallbackAPIKey), apiKey)
}

// GetOpsFallbackAPIKey 读取 Ops 错误日志专用的回退 API Key。
func GetOpsFallbackAPIKey(c *gin.Context) (*service.APIKey, bool) {
	value, exists := c.Get(string(ContextKeyOpsFallbackAPIKey))
	if !exists {
		return nil, false
	}
	apiKey, ok := value.(*service.APIKey)
	return apiKey, ok
}

// GetSubscriptionFromContext 从上下文中获取订阅信息
func GetSubscriptionFromContext(c *gin.Context) (*service.UserSubscription, bool) {
	value, exists := c.Get(string(ContextKeySubscription))
	if !exists {
		return nil, false
	}
	subscription, ok := value.(*service.UserSubscription)
	return subscription, ok
}

func setGroupContext(c *gin.Context, group *service.Group) {
	if !service.IsGroupContextValid(group) {
		return
	}
	if existing, ok := c.Request.Context().Value(ctxkey.Group).(*service.Group); ok && existing != nil && existing.ID == group.ID && service.IsGroupContextValid(existing) {
		return
	}
	ctx := context.WithValue(c.Request.Context(), ctxkey.Group, group)
	c.Request = c.Request.WithContext(ctx)
}

func abortIfAPIKeyGroupUnavailable(c *gin.Context, apiKey *service.APIKey) bool {
	code, message, ok := validateAPIKeyGroupAvailable(apiKey)
	if ok {
		return false
	}
	service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonAPIKeyGroupUnavailable)
	AbortWithError(c, 403, code, message)
	return true
}

func abortIfAPIKeyGroupNotAllowed(c *gin.Context, apiKey *service.APIKey) bool {
	if validateAPIKeyGroupAllowed(apiKey) {
		return false
	}
	service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonAPIKeyGroupUnavailable)
	AbortWithError(c, 403, "GROUP_NOT_ALLOWED", "API Key 所属专属分组不再允许当前用户使用")
	return true
}

func validateAPIKeyGroupAllowed(apiKey *service.APIKey) bool {
	if apiKey == nil || apiKey.GroupID == nil || apiKey.User == nil || apiKey.Group == nil {
		return true
	}
	group := apiKey.Group
	if group.IsSubscriptionType() {
		return true
	}
	return apiKey.User.CanBindGroup(group.ID, group.IsExclusive)
}

func validateAPIKeyGroupAvailable(apiKey *service.APIKey) (string, string, bool) {
	if apiKey == nil || apiKey.GroupID == nil {
		return "", "", true
	}
	group := apiKey.Group
	if group == nil || strings.EqualFold(group.Status, "deleted") {
		return "GROUP_DELETED", "API Key 所属分组已删除", false
	}
	if !group.IsActive() {
		return "GROUP_DISABLED", "API Key 所属分组已停用", false
	}
	return "", "", true
}
