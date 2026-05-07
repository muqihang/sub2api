package service

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// OpenAIOAuthService handles OpenAI OAuth authentication flows
type OpenAIOAuthService struct {
	sessionStore         *openai.SessionStore
	proxyRepo            ProxyRepository
	oauthClient          OpenAIOAuthClient
	privacyClientFactory PrivacyClientFactory // 用于调用 chatgpt.com/backend-api（ImpersonateChrome）
	gatewayCoreService   *OpenAIGatewayCoreService
}

// NewOpenAIOAuthService creates a new OpenAI OAuth service
func NewOpenAIOAuthService(proxyRepo ProxyRepository, oauthClient OpenAIOAuthClient) *OpenAIOAuthService {
	return &OpenAIOAuthService{
		sessionStore: openai.NewSessionStore(),
		proxyRepo:    proxyRepo,
		oauthClient:  oauthClient,
	}
}

// SetPrivacyClientFactory 注入 ImpersonateChrome 客户端工厂，
// 用于调用 chatgpt.com/backend-api 获取账号信息（plan_type 等）。
func (s *OpenAIOAuthService) SetPrivacyClientFactory(factory PrivacyClientFactory) {
	s.privacyClientFactory = factory
}

func (s *OpenAIOAuthService) SetGatewayCoreService(core *OpenAIGatewayCoreService) {
	s.gatewayCoreService = core
}

func (s *OpenAIOAuthService) gatewayConfig() *config.Config {
	if s == nil || s.gatewayCoreService == nil {
		return nil
	}
	return s.gatewayCoreService.cfg
}

func (s *OpenAIOAuthService) credentialAccessor() *OpenAIGatewayCredentials {
	return NewOpenAIGatewayCredentials(s.gatewayConfig(), nil)
}

func (s *OpenAIOAuthService) CredentialAccessor() *OpenAIGatewayCredentials {
	return s.credentialAccessor()
}

// OpenAIAuthURLResult contains the authorization URL and session info
type OpenAIAuthURLResult struct {
	AuthURL       string `json:"auth_url"`
	SessionID     string `json:"session_id"`
	EgressBucket  string `json:"egress_bucket,omitempty"`
	ProxySelected bool   `json:"proxy_selected"`
	ProxyLabel    string `json:"proxy_label,omitempty"`
	ProxyHash     string `json:"proxy_hash,omitempty"`
}

// GenerateAuthURL generates an OpenAI OAuth authorization URL
func (s *OpenAIOAuthService) GenerateAuthURL(ctx context.Context, proxyID *int64, redirectURI, platform string) (*OpenAIAuthURLResult, error) {
	return s.GenerateAuthURLWithEgress(ctx, proxyID, redirectURI, platform, "")
}

func (s *OpenAIOAuthService) GenerateAuthURLWithEgress(ctx context.Context, proxyID *int64, redirectURI, platform string, egressBucket string) (*OpenAIAuthURLResult, error) {
	// Generate PKCE values
	state, err := openai.GenerateState()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_OAUTH_STATE_FAILED", "failed to generate state: %v", err)
	}

	codeVerifier, err := openai.GenerateCodeVerifier()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_OAUTH_VERIFIER_FAILED", "failed to generate code verifier: %v", err)
	}

	codeChallenge := openai.GenerateCodeChallenge(codeVerifier)

	// Generate session ID
	sessionID, err := openai.GenerateSessionID()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_OAUTH_SESSION_FAILED", "failed to generate session ID: %v", err)
	}

	proxyURL, err := s.resolveOpenAIOAuthProxyURL(ctx, proxyID)
	if err != nil {
		return nil, err
	}
	egress, err := s.resolveOAuthSessionEgress(ctx, egressBucket, proxyURL)
	if err != nil {
		return nil, err
	}
	if egress != nil {
		proxyURL = egress.ProxyURL
	}

	// Use default redirect URI if not specified
	if redirectURI == "" {
		redirectURI = openai.DefaultRedirectURI
	}
	normalizedPlatform := normalizeOpenAIOAuthPlatform(platform)
	clientID, _ := openai.OAuthClientConfigByPlatform(normalizedPlatform)

	// Store session
	session := &openai.OAuthSession{
		State:         state,
		CodeVerifier:  codeVerifier,
		ClientID:      clientID,
		RedirectURI:   redirectURI,
		ProxyURL:      proxyURL,
		CreatedAt:     time.Now(),
		EgressBucket:  openAIEgressBucketName(egress),
		ProxySelected: openAIEgressProxySelected(egress),
		ProxyLabel:    openAIEgressProxyLabel(egress),
		ProxyHash:     openAIEgressProxyHash(egress),
	}
	s.sessionStore.Set(sessionID, session)

	// Build authorization URL
	authURL := openai.BuildAuthorizationURLForPlatform(state, codeChallenge, redirectURI, normalizedPlatform)

	return &OpenAIAuthURLResult{
		AuthURL:       authURL,
		SessionID:     sessionID,
		EgressBucket:  session.EgressBucket,
		ProxySelected: session.ProxySelected,
		ProxyLabel:    session.ProxyLabel,
		ProxyHash:     session.ProxyHash,
	}, nil
}

// OpenAIExchangeCodeInput represents the input for code exchange
type OpenAIExchangeCodeInput struct {
	SessionID    string
	Code         string
	State        string
	RedirectURI  string
	ProxyID      *int64
	EgressBucket string
}

// OpenAITokenInfo represents the token information for OpenAI
type OpenAITokenInfo struct {
	AccessToken           string   `json:"access_token"`
	RefreshToken          string   `json:"refresh_token"`
	IDToken               string   `json:"id_token,omitempty"`
	ExpiresIn             int64    `json:"expires_in"`
	ExpiresAt             int64    `json:"expires_at"`
	Scope                 string   `json:"scope,omitempty"`
	Scopes                []string `json:"scopes,omitempty"`
	ClientID              string   `json:"client_id,omitempty"`
	Email                 string   `json:"email,omitempty"`
	ChatGPTAccountID      string   `json:"chatgpt_account_id,omitempty"`
	ChatGPTUserID         string   `json:"chatgpt_user_id,omitempty"`
	OrganizationID        string   `json:"organization_id,omitempty"`
	PlanType              string   `json:"plan_type,omitempty"`
	SubscriptionExpiresAt string   `json:"subscription_expires_at,omitempty"`
	PrivacyMode           string   `json:"privacy_mode,omitempty"`
	EgressBucket          string   `json:"egress_bucket,omitempty"`
	ProxySelected         bool     `json:"proxy_selected"`
	ProxyLabel            string   `json:"proxy_label,omitempty"`
	ProxyHash             string   `json:"proxy_hash,omitempty"`
}

// ExchangeCode exchanges authorization code for tokens
func (s *OpenAIOAuthService) ExchangeCode(ctx context.Context, input *OpenAIExchangeCodeInput) (*OpenAITokenInfo, error) {
	// Get session
	session, ok := s.sessionStore.Get(input.SessionID)
	if !ok {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_OAUTH_SESSION_NOT_FOUND", "session not found or expired")
	}
	if input.State == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_OAUTH_STATE_REQUIRED", "oauth state is required")
	}
	if subtle.ConstantTimeCompare([]byte(input.State), []byte(session.State)) != 1 {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_OAUTH_INVALID_STATE", "invalid oauth state")
	}

	proxyURL := session.ProxyURL
	if input.ProxyID != nil {
		var err error
		proxyURL, err = s.resolveOpenAIOAuthProxyURL(ctx, input.ProxyID)
		if err != nil {
			return nil, err
		}
	}
	requestedBucket := strings.TrimSpace(input.EgressBucket)
	if requestedBucket == "" {
		requestedBucket = strings.TrimSpace(session.EgressBucket)
	}
	egress, err := s.resolveOAuthSessionEgress(ctx, requestedBucket, proxyURL)
	if err != nil {
		return nil, err
	}
	if egress != nil {
		proxyURL = egress.ProxyURL
	}

	// Use redirect URI from session or input
	redirectURI := session.RedirectURI
	if input.RedirectURI != "" {
		redirectURI = input.RedirectURI
	}
	clientID := strings.TrimSpace(session.ClientID)
	if clientID == "" {
		clientID = openai.ClientID
	}

	// Exchange code for token
	tokenResp, err := s.oauthClient.ExchangeCode(ctx, input.Code, session.CodeVerifier, redirectURI, proxyURL, clientID)
	if err != nil {
		return nil, err
	}

	// Parse ID token to get user info
	var userInfo *openai.UserInfo
	if tokenResp.IDToken != "" {
		claims, parseErr := openai.ParseIDToken(tokenResp.IDToken)
		if parseErr != nil {
			slog.Warn("openai_oauth_id_token_parse_failed", "error", parseErr)
		} else {
			userInfo = claims.GetUserInfo()
		}
	}

	// Delete session after successful exchange
	s.sessionStore.Delete(input.SessionID)

	tokenInfo := &OpenAITokenInfo{
		AccessToken:   tokenResp.AccessToken,
		RefreshToken:  tokenResp.RefreshToken,
		IDToken:       tokenResp.IDToken,
		ExpiresIn:     int64(tokenResp.ExpiresIn),
		ExpiresAt:     time.Now().Unix() + int64(tokenResp.ExpiresIn),
		ClientID:      clientID,
		EgressBucket:  openAIEgressBucketName(egress),
		ProxySelected: openAIEgressProxySelected(egress),
		ProxyLabel:    openAIEgressProxyLabel(egress),
		ProxyHash:     openAIEgressProxyHash(egress),
	}

	if userInfo != nil {
		tokenInfo.Email = userInfo.Email
		tokenInfo.ChatGPTAccountID = userInfo.ChatGPTAccountID
		tokenInfo.ChatGPTUserID = userInfo.ChatGPTUserID
		tokenInfo.OrganizationID = userInfo.OrganizationID
		tokenInfo.PlanType = userInfo.PlanType
	}

	s.enrichTokenInfo(ctx, tokenInfo, proxyURL)

	return tokenInfo, nil
}

// RefreshToken refreshes an OpenAI OAuth token
func (s *OpenAIOAuthService) RefreshToken(ctx context.Context, refreshToken string, proxyURL string) (*OpenAITokenInfo, error) {
	return s.RefreshTokenWithClientID(ctx, refreshToken, proxyURL, "")
}

func (s *OpenAIOAuthService) RefreshTokenWithClientIDAndEgress(ctx context.Context, refreshToken string, fallbackProxyURL string, clientID string, egressBucket string) (*OpenAITokenInfo, error) {
	egress, err := s.resolveOAuthSessionEgress(ctx, egressBucket, fallbackProxyURL)
	if err != nil {
		return nil, err
	}
	proxyURL := fallbackProxyURL
	if egress != nil {
		proxyURL = egress.ProxyURL
	}
	tokenInfo, err := s.RefreshTokenWithClientID(ctx, refreshToken, proxyURL, clientID)
	if err != nil {
		return nil, err
	}
	tokenInfo.EgressBucket = openAIEgressBucketName(egress)
	tokenInfo.ProxySelected = openAIEgressProxySelected(egress)
	tokenInfo.ProxyLabel = openAIEgressProxyLabel(egress)
	tokenInfo.ProxyHash = openAIEgressProxyHash(egress)
	return tokenInfo, nil
}

// RefreshTokenWithClientID refreshes an OpenAI OAuth token with optional client_id.
func (s *OpenAIOAuthService) RefreshTokenWithClientID(ctx context.Context, refreshToken string, proxyURL string, clientID string) (*OpenAITokenInfo, error) {
	tokenResp, err := s.oauthClient.RefreshTokenWithClientID(ctx, refreshToken, proxyURL, clientID)
	if err != nil {
		return nil, err
	}

	// Parse ID token to get user info
	var userInfo *openai.UserInfo
	if tokenResp.IDToken != "" {
		claims, parseErr := openai.ParseIDToken(tokenResp.IDToken)
		if parseErr != nil {
			slog.Warn("openai_oauth_id_token_parse_failed", "error", parseErr)
		} else {
			userInfo = claims.GetUserInfo()
		}
	}

	tokenInfo := &OpenAITokenInfo{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		ExpiresIn:    int64(tokenResp.ExpiresIn),
		ExpiresAt:    time.Now().Unix() + int64(tokenResp.ExpiresIn),
		Scope:        strings.TrimSpace(tokenResp.Scope),
	}
	if trimmed := strings.TrimSpace(clientID); trimmed != "" {
		tokenInfo.ClientID = trimmed
	}
	tokenInfo.Scopes = extractOpenAIScopesFromAccessToken(tokenResp.AccessToken)

	if userInfo != nil {
		tokenInfo.Email = userInfo.Email
		tokenInfo.ChatGPTAccountID = userInfo.ChatGPTAccountID
		tokenInfo.ChatGPTUserID = userInfo.ChatGPTUserID
		tokenInfo.OrganizationID = userInfo.OrganizationID
		tokenInfo.PlanType = userInfo.PlanType
	}

	s.enrichTokenInfo(ctx, tokenInfo, proxyURL)

	return tokenInfo, nil
}

// enrichTokenInfo 通过 ChatGPT backend-api 补全 tokenInfo 并设置隐私（best-effort）。
// 从 accounts/check 获取最新 plan_type、subscription_expires_at、email，
// 然后尝试关闭训练数据共享。适用于所有获取/刷新 token 的路径。
func (s *OpenAIOAuthService) enrichTokenInfo(ctx context.Context, tokenInfo *OpenAITokenInfo, proxyURL string) {
	if tokenInfo.AccessToken == "" || s.privacyClientFactory == nil {
		return
	}

	// 从 access_token JWT 中提取 orgID（poid），用于匹配正确的账号
	orgID := tokenInfo.OrganizationID
	if orgID == "" {
		if atClaims, err := openai.DecodeIDToken(tokenInfo.AccessToken); err == nil && atClaims.OpenAIAuth != nil {
			orgID = atClaims.OpenAIAuth.POID
		}
	}
	if info := fetchChatGPTAccountInfo(ctx, s.privacyClientFactory, tokenInfo.AccessToken, proxyURL, orgID); info != nil {
		if info.PlanType != "" {
			tokenInfo.PlanType = info.PlanType
		}
		if info.SubscriptionExpiresAt != "" {
			tokenInfo.SubscriptionExpiresAt = info.SubscriptionExpiresAt
		}
		if tokenInfo.Email == "" && info.Email != "" {
			tokenInfo.Email = info.Email
		}
	}

	// 尝试设置隐私（关闭训练数据共享），best-effort
	tokenInfo.PrivacyMode = disableOpenAITraining(ctx, s.privacyClientFactory, tokenInfo.AccessToken, proxyURL)
}

// RefreshAccountToken refreshes token for an OpenAI OAuth account
func (s *OpenAIOAuthService) RefreshAccountToken(ctx context.Context, account *Account) (*OpenAITokenInfo, error) {
	if account.Platform != PlatformOpenAI {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_OAUTH_INVALID_ACCOUNT", "account is not an OpenAI account")
	}
	if account.Type != AccountTypeOAuth {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_OAUTH_INVALID_ACCOUNT_TYPE", "account is not an OAuth account")
	}

	refreshToken, refreshErr := s.credentialAccessor().OpenAIRefreshToken(account)
	if refreshErr != nil {
		refreshToken = ""
	}
	if refreshToken == "" {
		accessToken, accessErr := s.credentialAccessor().OpenAIAccessToken(account)
		if accessErr != nil {
			accessToken = ""
		}
		if accessToken != "" {
			idToken := account.GetCredential("id_token")
			if decryptedIDToken, idTokenErr := s.credentialAccessor().resolveValue(idToken, "id_token"); idTokenErr == nil {
				idToken = decryptedIDToken
			}
			clientID := account.GetCredential("client_id")
			if decryptedClientID, clientIDErr := s.credentialAccessor().OpenAIClientID(account); clientIDErr == nil {
				clientID = decryptedClientID
			}
			tokenInfo := &OpenAITokenInfo{
				AccessToken:      accessToken,
				RefreshToken:     "",
				IDToken:          idToken,
				ClientID:         clientID,
				Email:            account.GetCredential("email"),
				ChatGPTAccountID: account.GetCredential("chatgpt_account_id"),
				ChatGPTUserID:    account.GetCredential("chatgpt_user_id"),
				OrganizationID:   account.GetCredential("organization_id"),
				PlanType:         account.GetCredential("plan_type"),
			}
			if expiresAt := account.GetCredentialAsTime("expires_at"); expiresAt != nil {
				tokenInfo.ExpiresAt = expiresAt.Unix()
				tokenInfo.ExpiresIn = int64(time.Until(*expiresAt).Seconds())
			}
			return tokenInfo, nil
		}
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_OAUTH_NO_REFRESH_TOKEN", "no refresh token available")
	}

	var proxyURL string
	if account.ProxyID != nil {
		proxy, err := s.proxyRepo.GetByID(ctx, *account.ProxyID)
		if err == nil && proxy != nil {
			proxyURL = proxy.URL()
		}
	}
	if s.gatewayCoreService != nil {
		egress, err := s.gatewayCoreService.ResolveEgress(ctx, account, proxyURL)
		if err != nil {
			return nil, err
		}
		if egress != nil {
			proxyURL = egress.ProxyURL
		}
	}

	clientID := account.GetCredential("client_id")
	if decryptedClientID, clientIDErr := s.credentialAccessor().OpenAIClientID(account); clientIDErr == nil {
		clientID = decryptedClientID
	}
	return s.RefreshTokenWithClientID(ctx, refreshToken, proxyURL, clientID)
}

// BuildAccountCredentials builds credentials map from token info
func (s *OpenAIOAuthService) BuildAccountCredentials(tokenInfo *OpenAITokenInfo) map[string]any {
	expiresAt := time.Unix(tokenInfo.ExpiresAt, 0).Format(time.RFC3339)

	creds := map[string]any{
		"access_token": tokenInfo.AccessToken,
		"expires_at":   expiresAt,
	}
	// 仅在刷新响应返回了新的 refresh_token 时才更新，防止用空值覆盖已有令牌
	if strings.TrimSpace(tokenInfo.RefreshToken) != "" {
		creds["refresh_token"] = tokenInfo.RefreshToken
	}

	if tokenInfo.IDToken != "" {
		creds["id_token"] = tokenInfo.IDToken
	}
	if tokenInfo.Email != "" {
		creds["email"] = tokenInfo.Email
	}
	if tokenInfo.ChatGPTAccountID != "" {
		creds["chatgpt_account_id"] = tokenInfo.ChatGPTAccountID
	}
	if tokenInfo.ChatGPTUserID != "" {
		creds["chatgpt_user_id"] = tokenInfo.ChatGPTUserID
	}
	if tokenInfo.OrganizationID != "" {
		creds["organization_id"] = tokenInfo.OrganizationID
	}
	if tokenInfo.PlanType != "" {
		creds["plan_type"] = tokenInfo.PlanType
	}
	if tokenInfo.SubscriptionExpiresAt != "" {
		creds["subscription_expires_at"] = tokenInfo.SubscriptionExpiresAt
	}
	if strings.TrimSpace(tokenInfo.ClientID) != "" {
		creds["client_id"] = strings.TrimSpace(tokenInfo.ClientID)
	}
	if strings.TrimSpace(tokenInfo.Scope) != "" {
		creds["scope"] = strings.TrimSpace(tokenInfo.Scope)
	}
	if len(tokenInfo.Scopes) > 0 {
		creds["scopes"] = append([]string(nil), tokenInfo.Scopes...)
	}
	protected, err := s.credentialAccessor().ProtectCredentials(creds)
	if err != nil {
		return creds
	}
	return protected
}

func (s *OpenAIOAuthService) resolveOpenAIOAuthProxyURL(ctx context.Context, proxyID *int64) (string, error) {
	if proxyID == nil {
		return "", nil
	}
	if s == nil || s.proxyRepo == nil {
		return "", infraerrors.New(http.StatusBadRequest, "OPENAI_OAUTH_PROXY_NOT_FOUND", "proxy repository unavailable")
	}
	proxy, err := s.proxyRepo.GetByID(ctx, *proxyID)
	if err != nil {
		return "", infraerrors.Newf(http.StatusBadRequest, "OPENAI_OAUTH_PROXY_NOT_FOUND", "proxy not found: %v", err)
	}
	if proxy == nil {
		return "", nil
	}
	return proxy.URL(), nil
}

func (s *OpenAIOAuthService) resolveOAuthSessionEgress(ctx context.Context, egressBucket string, fallbackProxyURL string) (*OpenAIEgressResolution, error) {
	if s != nil && s.gatewayCoreService != nil && s.gatewayCoreService.IsEnabled() {
		return s.gatewayCoreService.ResolveOAuthSessionEgress(ctx, egressBucket, fallbackProxyURL)
	}
	return buildOpenAIEgressResolution(strings.TrimSpace(egressBucket), fallbackProxyURL, openAIEgressSourceAccountFallback), nil
}

func openAIEgressBucketName(egress *OpenAIEgressResolution) string {
	if egress == nil {
		return ""
	}
	return strings.TrimSpace(egress.BucketName)
}

func openAIEgressProxySelected(egress *OpenAIEgressResolution) bool {
	return egress != nil && egress.ProxySelected
}

func openAIEgressProxyLabel(egress *OpenAIEgressResolution) string {
	if egress == nil {
		return ""
	}
	return egress.ProxyLabel
}

func openAIEgressProxyHash(egress *OpenAIEgressResolution) string {
	if egress == nil {
		return ""
	}
	return egress.ProxyHash
}

// Stop stops the session store cleanup goroutine
func (s *OpenAIOAuthService) Stop() {
	s.sessionStore.Stop()
}

func normalizeOpenAIOAuthPlatform(platform string) string {
	return openai.OAuthPlatformOpenAI
}
