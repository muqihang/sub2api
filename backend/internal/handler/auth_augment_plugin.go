package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type augmentQuickLoginGrantResponse struct {
	Grant          string   `json:"grant"`
	State          string   `json:"state"`
	ExpiresAt      string   `json:"expires_at"`
	TenantURL      string   `json:"tenant_url,omitempty"`
	PortalURL      string   `json:"portal_url,omitempty"`
	Scopes         []string `json:"scopes"`
	VSCodeDeeplink string   `json:"vscode_deeplink,omitempty"`
}

type augmentQuickLoginGrantRequest struct {
	Mode                  string `json:"mode"`
	Source                string `json:"source"`
	OfficialTenantURL     string `json:"official_tenant_url"`
	OfficialAccessToken   string `json:"official_access_token"`
	OfficialRefreshToken  string `json:"official_refresh_token"`
	OfficialExpiresAt     string `json:"official_expires_at"`
	OfficialScopes        string `json:"official_scopes"`
	OfficialSessionBundle string `json:"official_session_bundle"`
	TenantURL             string `json:"tenant_url"`
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	ExpiresAt             string `json:"expires_at"`
	Scopes                string `json:"scopes"`
	SessionBundle         string `json:"session_bundle"`
}

type augmentCallbackExchangeRequest struct {
	Grant string `json:"grant"`
	Code  string `json:"code"`
	State string `json:"state" binding:"required"`
}

type augmentSessionRefreshRequest struct {
	RefreshToken          string `json:"refresh_token" binding:"required"`
	Mode                  string `json:"mode"`
	OfficialTenantURL     string `json:"official_tenant_url"`
	OfficialAccessToken   string `json:"official_access_token"`
	OfficialRefreshToken  string `json:"official_refresh_token"`
	OfficialExpiresAt     string `json:"official_expires_at"`
	OfficialScopes        string `json:"official_scopes"`
	OfficialSessionBundle string `json:"official_session_bundle"`
	TenantURL             string `json:"tenant_url"`
	AccessToken           string `json:"access_token"`
	ExpiresAt             string `json:"expires_at"`
	Scopes                string `json:"scopes"`
	SessionBundle         string `json:"session_bundle"`
}

type augmentAPIKeyVerifyRequest struct {
	APIKey string `json:"api_key" binding:"required"`
}

// AugmentQuickLoginGrant issues a short-lived single-use grant for the local Augment quick-login flow.
// POST /api/v1/plugin/augment/quick-login/grant
func (h *AuthHandler) AugmentQuickLoginGrant(c *gin.Context) {
	if h.augmentPluginService == nil {
		response.InternalError(c, "Augment plugin service is unavailable")
		return
	}

	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req augmentQuickLoginGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	options := buildAugmentQuickLoginGrantOptions(req, h.augmentTenantURL(c))
	var poolLease *service.AugmentOfficialPoolSessionLease
	if options.Mode == service.AugmentQuickLoginModeOfficialPassthrough && options.OfficialSessionBundle == nil && h != nil && h.augmentOfficialPoolService != nil {
		var err error
		poolLease, err = h.augmentOfficialPoolService.AcquireSessionBundle(c.Request.Context(), requestedAugmentPoolSources(req.Source))
		if err == nil && poolLease != nil {
			options.OfficialSessionBundle = poolLease.Bundle
		}
	}
	if options.Mode == service.AugmentQuickLoginModeOfficialPassthrough && options.OfficialSessionBundle == nil {
		options = h.buildAugmentQuickLoginGrantOptions(c, subject.UserID, req)
	}
	if options.Mode == service.AugmentQuickLoginModeOfficialPassthrough && options.OfficialSessionBundle == nil {
		if h != nil && h.augmentOfficialPoolService != nil {
			response.ErrorFrom(c, service.ErrAugmentOfficialPoolSessionUnavailable)
		} else {
			response.ErrorFrom(c, service.ErrAugmentPluginOfficialAuth)
		}
		return
	}

	grant, err := h.augmentPluginService.CreateQuickLoginGrant(
		c.Request.Context(),
		subject.UserID,
		options,
	)
	if poolLease != nil {
		_ = poolLease.Release(c.Request.Context(), err == nil, "grant_failed")
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, augmentQuickLoginGrantResponse{
		Grant:          grant.Grant,
		State:          grant.State,
		ExpiresAt:      grant.ExpiresAt,
		TenantURL:      grant.TenantURL,
		PortalURL:      grant.PortalURL,
		Scopes:         grant.Scopes,
		VSCodeDeeplink: buildAugmentVSCodeDeeplink(grant.Grant, grant.State, h.augmentTenantURL(c), grant.PortalURL),
	})
}

// AugmentCallbackExchange exchanges a one-time grant+state for a site session bundle.
// POST /api/v1/plugin/augment/callback/exchange
func (h *AuthHandler) AugmentCallbackExchange(c *gin.Context) {
	if h.augmentPluginService == nil {
		response.InternalError(c, "Augment plugin service is unavailable")
		return
	}

	var req augmentCallbackExchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	grant := strings.TrimSpace(req.Grant)
	if grant == "" {
		grant = strings.TrimSpace(req.Code)
	}
	if grant == "" {
		response.BadRequest(c, "Invalid request: grant or code is required")
		return
	}

	bundle, err := h.augmentPluginService.ExchangeGrant(c.Request.Context(), grant, req.State, h.augmentTenantURL(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, bundle)
}

// AugmentSessionRefresh refreshes an Augment plugin session using the standard refresh-token rotation.
// POST /api/v1/plugin/augment/session/refresh
func (h *AuthHandler) AugmentSessionRefresh(c *gin.Context) {
	if h.augmentPluginService == nil {
		response.InternalError(c, "Augment plugin service is unavailable")
		return
	}

	var req augmentSessionRefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	options := buildAugmentSessionRefreshOptions(req, h.augmentTenantURL(c))
	var poolLease *service.AugmentOfficialPoolSessionLease
	if options.Mode == service.AugmentQuickLoginModeOfficialPassthrough && options.OfficialSessionBundle == nil && h != nil && h.augmentOfficialPoolService != nil {
		if _, ok := h.augmentPrincipalFromBearer(c); !ok {
			return
		}
		var err error
		poolLease, err = h.augmentOfficialPoolService.AcquireSessionBundle(c.Request.Context(), nil)
		if err == nil && poolLease != nil {
			options.OfficialSessionBundle = poolLease.Bundle
		}
	}
	if options.Mode == service.AugmentQuickLoginModeOfficialPassthrough && options.OfficialSessionBundle == nil {
		options = h.buildAugmentSessionRefreshOptions(c, req)
	}
	if options.Mode == service.AugmentQuickLoginModeOfficialPassthrough && options.OfficialSessionBundle == nil {
		if h != nil && h.augmentOfficialPoolService != nil {
			response.ErrorFrom(c, service.ErrAugmentOfficialPoolSessionUnavailable)
		} else {
			response.ErrorFrom(c, service.ErrAugmentPluginOfficialAuth)
		}
		return
	}

	bundle, err := h.augmentPluginService.RefreshSessionWithOptions(
		c.Request.Context(),
		req.RefreshToken,
		options,
	)
	if poolLease != nil {
		_ = poolLease.Release(c.Request.Context(), err == nil, "refresh_failed")
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, bundle)
}

// AugmentAPIKeyVerify verifies a presented site API key for Augment local integration.
// POST /api/v1/plugin/augment/api-key/verify
func (h *AuthHandler) AugmentAPIKeyVerify(c *gin.Context) {
	if h.augmentPluginService == nil {
		response.InternalError(c, "Augment plugin service is unavailable")
		return
	}

	var req augmentAPIKeyVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	result, err := h.augmentPluginService.VerifyPresentedAPIKey(c.Request.Context(), req.APIKey)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, result)
}

// AugmentSummary returns the minimal site/account summary consumed by the Augment extension.
// GET /api/v1/plugin/augment/summary
func (h *AuthHandler) AugmentSummary(c *gin.Context) {
	if h.augmentPluginService == nil {
		response.InternalError(c, "Augment plugin service is unavailable")
		return
	}

	principal, ok := h.augmentPrincipalFromBearer(c)
	if !ok {
		return
	}

	summary, err := h.augmentPluginService.BuildSummary(c.Request.Context(), *principal)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, summary)
}

// AugmentCompatMetadata returns the minimal compatibility metadata consumed by the Augment extension.
// GET /api/v1/plugin/augment/compat/metadata
func (h *AuthHandler) AugmentCompatMetadata(c *gin.Context) {
	if h.augmentPluginService == nil {
		response.InternalError(c, "Augment plugin service is unavailable")
		return
	}

	principal, ok := h.augmentPrincipalFromBearer(c)
	if !ok {
		return
	}

	compat, err := h.augmentPluginService.BuildCompatMetadata(c.Request.Context(), *principal, h.augmentGatewayBaseURL(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, compat)
}

func (h *AuthHandler) augmentPrincipalFromBearer(c *gin.Context) (*service.AugmentPluginPrincipal, bool) {
	token := extractBearerToken(c.GetHeader("Authorization"))
	if token == "" {
		response.Unauthorized(c, "Authorization header is required")
		return nil, false
	}

	principal, err := h.augmentPluginService.ResolvePrincipalFromBearer(c.Request.Context(), token)
	if err != nil {
		response.ErrorFrom(c, err)
		return nil, false
	}
	return principal, true
}

func (h *AuthHandler) AugmentGatewayAPIKeyFromAuthorization(ctx context.Context, authorization string) (string, bool) {
	if h == nil || h.augmentPluginService == nil {
		return "", false
	}
	token := extractBearerToken(authorization)
	if token == "" {
		return "", false
	}
	principal, err := h.augmentPluginService.ResolvePrincipalFromBearer(ctx, token)
	if err != nil {
		return "", false
	}
	gatewayKey, err := h.augmentLegacyGatewayBearer(ctx, principal)
	if err != nil || strings.TrimSpace(gatewayKey) == "" {
		return "", false
	}
	return strings.TrimSpace(gatewayKey), true
}

func (h *AuthHandler) augmentGatewayBaseURL(c *gin.Context) string {
	if h.settingSvc != nil {
		if settings, err := h.settingSvc.GetPublicSettings(c.Request.Context()); err == nil {
			if apiBaseURL := normalizeAbsoluteURL(settings.APIBaseURL, false); apiBaseURL != "" {
				return apiBaseURL
			}
		}
	}

	if origin := normalizeAbsoluteURL(c.GetHeader("Origin"), false); origin != "" {
		return origin
	}

	if origin := requestOrigin(c); origin != "" {
		return origin
	}

	if h.cfg != nil {
		if fallback := normalizeAbsoluteURL(h.cfg.Server.FrontendURL, false); fallback != "" {
			return fallback
		}
	}

	return ""
}

func (h *AuthHandler) augmentTenantURL(c *gin.Context) string {
	if origin := normalizeAbsoluteURL(c.GetHeader("Origin"), true); origin != "" {
		return origin
	}

	if origin := requestOrigin(c); origin != "" {
		return origin
	}

	if h.cfg != nil {
		if fallback := normalizeAbsoluteURL(h.cfg.Server.FrontendURL, true); fallback != "" {
			return fallback
		}
	}

	return h.augmentGatewayBaseURL(c)
}

func extractBearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func requestOrigin(c *gin.Context) string {
	host := strings.TrimSpace(c.Request.Host)
	if host == "" {
		return ""
	}

	scheme := "http"
	if strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")), "https") {
		scheme = "https"
	} else if c.Request.TLS != nil {
		scheme = "https"
	}

	return scheme + "://" + host
}

func normalizeAbsoluteURL(raw string, originOnly bool) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	if originOnly {
		return parsed.Scheme + "://" + parsed.Host
	}
	return strings.TrimRight(parsed.String(), "/")
}

func buildAugmentVSCodeDeeplink(grant, state, issuer, portal string) string {
	values := url.Values{}
	values.Set("grant", strings.TrimSpace(grant))
	values.Set("state", strings.TrimSpace(state))
	values.Set("source", "quick_login")
	if normalizedIssuer := normalizeAbsoluteURL(issuer, true); normalizedIssuer != "" {
		values.Set("issuer", normalizedIssuer)
	}
	if normalizedPortal := normalizeAbsoluteURL(portal, false); normalizedPortal != "" {
		values.Set("portal", normalizedPortal)
	}
	return "vscode://Augment.vscode-augment/autoAuth?" + values.Encode()
}

func buildAugmentQuickLoginGrantOptions(req augmentQuickLoginGrantRequest, tenantURL string) service.AugmentQuickLoginGrantOptions {
	options := service.AugmentQuickLoginGrantOptions{
		TenantURL: tenantURL,
		Mode:      inferAugmentSessionMode(strings.TrimSpace(req.Mode), req),
	}

	if bundle := parseAugmentSessionBundleString(req.OfficialSessionBundle); bundle != nil {
		options.OfficialSessionBundle = bundle
		return options
	}

	officialTenantURL := strings.TrimSpace(req.OfficialTenantURL)
	officialAccessToken := strings.TrimSpace(req.OfficialAccessToken)
	if officialTenantURL == "" && officialAccessToken == "" {
		return options
	}

	options.OfficialSessionBundle = &service.AugmentSessionBundle{
		TenantURL:    officialTenantURL,
		AccessToken:  officialAccessToken,
		RefreshToken: strings.TrimSpace(req.OfficialRefreshToken),
		ExpiresAt:    strings.TrimSpace(req.OfficialExpiresAt),
		Scopes:       splitAugmentScopes(req.OfficialScopes),
	}
	return options
}

func (h *AuthHandler) buildAugmentQuickLoginGrantOptions(c *gin.Context, userID int64, req augmentQuickLoginGrantRequest) service.AugmentQuickLoginGrantOptions {
	options := buildAugmentQuickLoginGrantOptions(req, h.augmentTenantURL(c))
	if options.Mode != service.AugmentQuickLoginModeOfficialPassthrough || options.OfficialSessionBundle != nil || h == nil || h.augmentOfficialSessionService == nil {
		return options
	}
	credential, err := h.augmentOfficialSessionService.GetCredentialForRoute(c.Request.Context(), userID)
	if err != nil || credential == nil {
		return options
	}
	options.OfficialSessionBundle = augmentOfficialSessionBundleFromCredential(credential)
	return options
}

func buildAugmentSessionRefreshOptions(req augmentSessionRefreshRequest, tenantURL string) service.AugmentSessionRefreshOptions {
	options := service.AugmentSessionRefreshOptions{
		TenantURL: tenantURL,
		Mode:      inferAugmentSessionMode(strings.TrimSpace(req.Mode), req),
	}

	if bundle := parseAugmentSessionBundleString(req.OfficialSessionBundle); bundle != nil {
		options.OfficialSessionBundle = bundle
		return options
	}

	officialTenantURL := strings.TrimSpace(req.OfficialTenantURL)
	officialAccessToken := strings.TrimSpace(req.OfficialAccessToken)
	if officialTenantURL == "" && officialAccessToken == "" {
		return options
	}

	options.OfficialSessionBundle = &service.AugmentSessionBundle{
		TenantURL:    officialTenantURL,
		AccessToken:  officialAccessToken,
		RefreshToken: strings.TrimSpace(req.OfficialRefreshToken),
		ExpiresAt:    strings.TrimSpace(req.OfficialExpiresAt),
		Scopes:       splitAugmentScopes(req.OfficialScopes),
	}
	return options
}

func requestedAugmentPoolSources(source string) []string {
	source = strings.TrimSpace(source)
	switch source {
	case "official_quick_login", "wukong_quick_login":
		return []string{source}
	default:
		return nil
	}
}

func (h *AuthHandler) buildAugmentSessionRefreshOptions(c *gin.Context, req augmentSessionRefreshRequest) service.AugmentSessionRefreshOptions {
	options := buildAugmentSessionRefreshOptions(req, h.augmentTenantURL(c))
	if options.Mode != service.AugmentQuickLoginModeOfficialPassthrough || options.OfficialSessionBundle != nil || h == nil || h.augmentOfficialSessionService == nil {
		return options
	}
	userID, ok := h.augmentOfficialSessionUserID(c)
	if !ok {
		return options
	}
	credential, err := h.augmentOfficialSessionService.GetCredentialForRoute(c.Request.Context(), userID)
	if err != nil || credential == nil {
		return options
	}
	options.OfficialSessionBundle = augmentOfficialSessionBundleFromCredential(credential)
	return options
}

func (h *AuthHandler) augmentOfficialSessionUserID(c *gin.Context) (int64, bool) {
	if subject, ok := servermiddleware.GetAuthSubjectFromContext(c); ok && subject.UserID > 0 {
		return subject.UserID, true
	}
	if h == nil || h.augmentPluginService == nil {
		return 0, false
	}
	token := extractBearerToken(c.GetHeader("Authorization"))
	if token == "" {
		return 0, false
	}
	principal, err := h.augmentPluginService.ResolvePrincipalFromBearer(c.Request.Context(), token)
	if err != nil || principal == nil || principal.User == nil || principal.User.ID <= 0 || principal.Kind != "jwt" {
		return 0, false
	}
	return principal.User.ID, true
}

func augmentOfficialSessionBundleFromCredential(credential *service.AugmentOfficialSessionCredential) *service.AugmentSessionBundle {
	if credential == nil {
		return nil
	}
	bundle := &service.AugmentSessionBundle{
		AccessToken:   credential.AccessToken,
		RefreshToken:  credential.RefreshToken,
		TenantURL:     credential.TenantOrigin,
		Scopes:        append([]string(nil), credential.Scopes...),
		SessionSource: service.AugmentSessionSourceOfficial,
	}
	if credential.PortalOrigin != nil {
		bundle.PortalURL = *credential.PortalOrigin
	}
	if credential.ExpiresAt != nil {
		bundle.ExpiresAt = credential.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return bundle
}

func parseAugmentSessionBundleString(raw string) *service.AugmentSessionBundle {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var bundle service.AugmentSessionBundle
	if err := json.Unmarshal([]byte(raw), &bundle); err != nil {
		return nil
	}
	return &bundle
}

func splitAugmentScopes(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	scopes := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			scopes = append(scopes, trimmed)
		}
	}
	return scopes
}

func inferAugmentSessionMode(mode string, req any) string {
	trimmedMode := strings.TrimSpace(mode)
	if trimmedMode != "" {
		return trimmedMode
	}

	switch typed := any(req).(type) {
	case augmentQuickLoginGrantRequest:
		if augmentRequestCarriesOfficialSession(typed.OfficialSessionBundle, typed.OfficialAccessToken) {
			return service.AugmentQuickLoginModeOfficialPassthrough
		}
	case augmentSessionRefreshRequest:
		if augmentRequestCarriesOfficialSession(typed.OfficialSessionBundle, typed.OfficialAccessToken) {
			return service.AugmentQuickLoginModeOfficialPassthrough
		}
	}

	return service.AugmentQuickLoginModeLocalCompat
}

func augmentRequestCarriesOfficialSession(sessionBundle, accessToken string) bool {
	if strings.TrimSpace(sessionBundle) != "" {
		return true
	}
	return strings.TrimSpace(accessToken) != ""
}
