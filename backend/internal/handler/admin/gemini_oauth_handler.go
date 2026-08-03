package admin

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type GeminiOAuthHandler struct {
	geminiOAuthService *service.GeminiOAuthService
	adminService       service.AdminService
}

func NewGeminiOAuthHandler(geminiOAuthService *service.GeminiOAuthService, adminService service.AdminService) *GeminiOAuthHandler {
	return &GeminiOAuthHandler{
		geminiOAuthService: geminiOAuthService,
		adminService:       adminService,
	}
}

// GetCapabilities returns the Gemini OAuth configuration capabilities.
// GET /api/v1/admin/gemini/oauth/capabilities
func (h *GeminiOAuthHandler) GetCapabilities(c *gin.Context) {
	cfg := h.geminiOAuthService.GetOAuthConfig()
	response.Success(c, cfg)
}

type GeminiGenerateAuthURLRequest struct {
	ProxyID   *int64 `json:"proxy_id"`
	ProjectID string `json:"project_id"`
	// OAuth 类型: "code_assist" (需要 project_id) 或 "ai_studio" (不需要 project_id)
	// 默认为 "code_assist" 以保持向后兼容
	OAuthType string `json:"oauth_type"`
	// TierID is a user-selected tier to be used when auto detection is unavailable or fails.
	TierID string `json:"tier_id"`
}

// GenerateAuthURL generates Google OAuth authorization URL for Gemini.
// POST /api/v1/admin/gemini/oauth/auth-url
func (h *GeminiOAuthHandler) GenerateAuthURL(c *gin.Context) {
	var req GeminiGenerateAuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// 默认使用 code_assist 以保持向后兼容
	oauthType := strings.TrimSpace(req.OAuthType)
	if oauthType == "" {
		oauthType = "code_assist"
	}
	if oauthType != "code_assist" && oauthType != "google_one" && oauthType != "ai_studio" {
		response.BadRequest(c, "Invalid oauth_type: must be 'code_assist', 'google_one', or 'ai_studio'")
		return
	}

	// Always pass the "hosted" callback URI; the OAuth service may override it depending on
	// oauth_type and whether the built-in Gemini CLI OAuth client is used.
	redirectURI := deriveGeminiRedirectURI(c)
	result, err := h.geminiOAuthService.GenerateAuthURL(c.Request.Context(), req.ProxyID, redirectURI, req.ProjectID, oauthType, req.TierID)
	if err != nil {
		msg := err.Error()
		// Treat missing/invalid OAuth client configuration as a user/config error.
		if strings.Contains(msg, "OAuth client not configured") ||
			strings.Contains(msg, "requires your own OAuth Client") ||
			strings.Contains(msg, "requires a custom OAuth Client") ||
			strings.Contains(msg, "GEMINI_CLI_OAUTH_CLIENT_SECRET_MISSING") ||
			strings.Contains(msg, "built-in Gemini CLI OAuth client_secret is not configured") {
			response.BadRequest(c, "Failed to generate auth URL: "+msg)
			return
		}
		response.InternalError(c, "Failed to generate auth URL: "+msg)
		return
	}

	response.Success(c, result)
}

type GeminiExchangeCodeRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	State     string `json:"state" binding:"required"`
	Code      string `json:"code" binding:"required"`
	ProxyID   *int64 `json:"proxy_id"`
	// OAuth 类型: "code_assist" 或 "ai_studio"，需要与 GenerateAuthURL 时的类型一致
	OAuthType string `json:"oauth_type"`
	// TierID is a user-selected tier to be used when auto detection is unavailable or fails.
	// This field is optional; when omitted, the server uses the tier stored in the OAuth session.
	TierID string `json:"tier_id"`
}

type GeminiExchangeCodeResponse struct {
	ExpiresIn int64          `json:"expires_in"`
	ExpiresAt int64          `json:"expires_at"`
	TokenType string         `json:"token_type"`
	Scope     string         `json:"scope,omitempty"`
	ProjectID string         `json:"project_id,omitempty"`
	OAuthType string         `json:"oauth_type,omitempty"`
	TierID    string         `json:"tier_id,omitempty"`
	Extra     map[string]any `json:"extra,omitempty"`
}

// ExchangeCode exchanges authorization code for tokens.
// POST /api/v1/admin/gemini/oauth/exchange-code
func (h *GeminiOAuthHandler) ExchangeCode(c *gin.Context) {
	var req GeminiExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// 默认使用 code_assist 以保持向后兼容
	oauthType := strings.TrimSpace(req.OAuthType)
	if oauthType == "" {
		oauthType = "code_assist"
	}
	if oauthType != "code_assist" && oauthType != "google_one" && oauthType != "ai_studio" {
		response.BadRequest(c, "Invalid oauth_type: must be 'code_assist', 'google_one', or 'ai_studio'")
		return
	}

	tokenInfo, err := h.geminiOAuthService.ExchangeCode(c.Request.Context(), &service.GeminiExchangeCodeInput{
		SessionID: req.SessionID,
		State:     req.State,
		Code:      req.Code,
		ProxyID:   req.ProxyID,
		OAuthType: oauthType,
		TierID:    req.TierID,
	})
	if err != nil {
		response.BadRequest(c, "Failed to exchange code: "+err.Error())
		return
	}

	response.Success(c, sanitizeGeminiTokenInfoForResponse(h.geminiOAuthService, tokenInfo))
}

type GeminiCreateAccountFromOAuthRequest struct {
	SessionID                string         `json:"session_id" binding:"required"`
	State                    string         `json:"state" binding:"required"`
	Code                     string         `json:"code" binding:"required"`
	ProxyID                  *int64         `json:"proxy_id"`
	OAuthType                string         `json:"oauth_type"`
	TierID                   string         `json:"tier_id"`
	Name                     string         `json:"name"`
	Notes                    *string        `json:"notes"`
	Extra                    map[string]any `json:"extra"`
	Concurrency              int            `json:"concurrency"`
	Priority                 int            `json:"priority"`
	RateMultiplier           *float64       `json:"rate_multiplier"`
	LoadFactor               *int           `json:"load_factor"`
	GroupIDs                 []int64        `json:"group_ids"`
	ExpiresAt                *int64         `json:"expires_at"`
	AutoPauseOnExpired       *bool          `json:"auto_pause_on_expired"`
	ConfirmMixedChannelRisk  *bool          `json:"confirm_mixed_channel_risk"`
}

// CreateAccountFromOAuth exchanges a Gemini OAuth code and persists the account server-side.
// POST /api/v1/admin/gemini/create-from-oauth
func (h *GeminiOAuthHandler) CreateAccountFromOAuth(c *gin.Context) {
	if h.geminiOAuthService == nil || h.adminService == nil {
		response.Error(c, http.StatusServiceUnavailable, "gemini oauth account creation unavailable")
		return
	}

	var req GeminiCreateAccountFromOAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.RateMultiplier != nil && *req.RateMultiplier < 0 {
		response.BadRequest(c, "rate_multiplier must be >= 0")
		return
	}
	sanitizeExtraBaseRPM(req.Extra)

	oauthType := strings.TrimSpace(req.OAuthType)
	if oauthType == "" {
		oauthType = "code_assist"
	}

	tokenInfo, err := h.geminiOAuthService.ExchangeCode(c.Request.Context(), &service.GeminiExchangeCodeInput{
		SessionID: req.SessionID,
		State:     req.State,
		Code:      req.Code,
		ProxyID:   req.ProxyID,
		OAuthType: oauthType,
		TierID:    req.TierID,
	})
	if err != nil {
		response.BadRequest(c, "Failed to exchange code: "+err.Error())
		return
	}

	credentials, err := h.geminiOAuthService.BuildProtectedAccountCredentials(tokenInfo)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	extra := cloneGeminiOAuthExtra(tokenInfo.Extra)
	if extra == nil {
		extra = map[string]any{}
	}
	for k, v := range req.Extra {
		extra[k] = v
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = defaultGeminiOAuthAccountName(tokenInfo)
	}

	skipCheck := req.ConfirmMixedChannelRisk != nil && *req.ConfirmMixedChannelRisk
	account, err := h.adminService.CreateAccount(c.Request.Context(), &service.CreateAccountInput{
		Name:                  name,
		Notes:                 req.Notes,
		Platform:              service.PlatformGemini,
		Type:                  service.AccountTypeOAuth,
		Credentials:           credentials,
		Extra:                 extra,
		ProxyID:               req.ProxyID,
		Concurrency:           req.Concurrency,
		Priority:              req.Priority,
		RateMultiplier:        req.RateMultiplier,
		LoadFactor:            req.LoadFactor,
		GroupIDs:              req.GroupIDs,
		ExpiresAt:             req.ExpiresAt,
		AutoPauseOnExpired:    req.AutoPauseOnExpired,
		SkipMixedChannelCheck: skipCheck,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.AccountFromService(account))
}

type GeminiReauthorizeAccountFromOAuthRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	State     string `json:"state" binding:"required"`
	Code      string `json:"code" binding:"required"`
	ProxyID   *int64 `json:"proxy_id"`
	OAuthType string `json:"oauth_type"`
	TierID    string `json:"tier_id"`
}

// ReauthorizeAccountFromOAuth exchanges a Gemini OAuth code and updates an existing account.
// POST /api/v1/admin/gemini/accounts/:id/reauthorize-from-oauth
func (h *GeminiOAuthHandler) ReauthorizeAccountFromOAuth(c *gin.Context) {
	if h.geminiOAuthService == nil || h.adminService == nil {
		response.Error(c, http.StatusServiceUnavailable, "gemini oauth account reauthorization unavailable")
		return
	}

	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if account.Platform != service.PlatformGemini || account.Type != service.AccountTypeOAuth {
		response.BadRequest(c, "Account must be a Gemini OAuth account")
		return
	}

	var req GeminiReauthorizeAccountFromOAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	oauthType := strings.TrimSpace(req.OAuthType)
	if oauthType == "" {
		oauthType = "code_assist"
	}

	tokenInfo, err := h.geminiOAuthService.ExchangeCode(c.Request.Context(), &service.GeminiExchangeCodeInput{
		SessionID: req.SessionID,
		State:     req.State,
		Code:      req.Code,
		ProxyID:   req.ProxyID,
		OAuthType: oauthType,
		TierID:    req.TierID,
	})
	if err != nil {
		response.BadRequest(c, "Failed to exchange code: "+err.Error())
		return
	}

	newCredentials, err := h.geminiOAuthService.BuildProtectedAccountCredentials(tokenInfo)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	newCredentials, err = service.MergeProtectedGeminiCredentials(account.Credentials, newCredentials, h.geminiOAuthService.CredentialAccessor())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	updatedAccount, err := h.adminService.UpdateAccount(c.Request.Context(), accountID, &service.UpdateAccountInput{
		Credentials: newCredentials,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.AccountFromService(updatedAccount))
}

func deriveGeminiRedirectURI(c *gin.Context) string {
	origin := strings.TrimSpace(c.GetHeader("Origin"))
	if origin != "" {
		return strings.TrimRight(origin, "/") + "/auth/callback"
	}

	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if xfProto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); xfProto != "" {
		scheme = strings.TrimSpace(strings.Split(xfProto, ",")[0])
	}

	host := strings.TrimSpace(c.Request.Host)
	if xfHost := strings.TrimSpace(c.GetHeader("X-Forwarded-Host")); xfHost != "" {
		host = strings.TrimSpace(strings.Split(xfHost, ",")[0])
	}

	return fmt.Sprintf("%s://%s/auth/callback", scheme, host)
}

func sanitizeGeminiTokenInfoForResponse(geminiOAuthService *service.GeminiOAuthService, tokenInfo *service.GeminiTokenInfo) *GeminiExchangeCodeResponse {
	if tokenInfo == nil {
		return nil
	}
	extra := map[string]any{}
	for key, value := range tokenInfo.Extra {
		extra[key] = value
	}
	switch strings.TrimSpace(tokenInfo.OAuthType) {
	case "code_assist":
		if strings.TrimSpace(tokenInfo.ProjectID) != "" {
			extra["project_id_status"] = "present"
		} else {
			extra["project_id_status"] = "required_missing"
		}
	default:
		if strings.TrimSpace(tokenInfo.ProjectID) != "" {
			extra["project_id_status"] = "present"
		} else {
			extra["project_id_status"] = "optional_empty"
		}
	}
	if extra["gemini_oauth_reason"] == "google_one_default_tier_fallback" {
		extra["tier_status"] = "default_fallback"
	} else if strings.TrimSpace(tokenInfo.TierID) != "" {
		extra["tier_status"] = "present"
	} else {
		extra["tier_status"] = "missing"
	}
	if geminiOAuthService != nil {
		extra["session_store"] = geminiOAuthService.SessionStoreMode()
	}
	return &GeminiExchangeCodeResponse{
		ExpiresIn: tokenInfo.ExpiresIn,
		ExpiresAt: tokenInfo.ExpiresAt,
		TokenType: tokenInfo.TokenType,
		Scope:     tokenInfo.Scope,
		ProjectID: tokenInfo.ProjectID,
		OAuthType: tokenInfo.OAuthType,
		TierID:    tokenInfo.TierID,
		Extra:     extra,
	}
}

func cloneGeminiOAuthExtra(extra map[string]any) map[string]any {
	if len(extra) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(extra))
	for k, v := range extra {
		cloned[k] = v
	}
	return cloned
}

func defaultGeminiOAuthAccountName(tokenInfo *service.GeminiTokenInfo) string {
	if tokenInfo == nil {
		return "Gemini OAuth Account"
	}
	switch strings.TrimSpace(tokenInfo.OAuthType) {
	case "google_one":
		return "Gemini Google One OAuth Account"
	case "ai_studio":
		return "Gemini AI Studio OAuth Account"
	case "code_assist":
		if projectID := strings.TrimSpace(tokenInfo.ProjectID); projectID != "" {
			return "Gemini Code Assist OAuth Account (" + projectID + ")"
		}
	}
	return "Gemini OAuth Account"
}
