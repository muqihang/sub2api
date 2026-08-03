package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"

	"github.com/imroc/req/v3"
)

func NewClaudeOAuthClient() service.ClaudeOAuthClient {
	return &claudeOAuthService{
		baseURL:       "https://claude.ai",
		tokenURL:      oauth.TokenURL,
		clientFactory: createReqClient,
	}
}

type claudeOAuthService struct {
	baseURL       string
	tokenURL      string
	clientFactory func(proxyURL string) (*req.Client, error)
}

func (s *claudeOAuthService) GetOrganizationUUID(ctx context.Context, sessionKey, proxyURL string) (string, error) {
	client, err := s.clientFactory(proxyURL)
	if err != nil {
		return "", fmt.Errorf("create HTTP client: %w", err)
	}

	var orgs []struct {
		UUID      string  `json:"uuid"`
		Name      string  `json:"name"`
		RavenType *string `json:"raven_type"` // nil for personal, "team" for team organization
	}

	targetURL := s.baseURL + "/api/organizations"
	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 1: Getting organization UUID from %s", targetURL)

	resp, err := client.R().
		SetContext(ctx).
		SetCookies(&http.Cookie{
			Name:  "sessionKey",
			Value: sessionKey,
		}).
		SetHeader("Accept", "application/json").
		SetHeader("Accept-Language", "en-US,en;q=0.9").
		SetHeader("Cache-Control", "no-cache").
		SetHeader("Origin", "https://claude.ai").
		SetHeader("Referer", "https://claude.ai/new").
		SetHeader("User-Agent", claudeOAuthBrowserUserAgent).
		SetSuccessResult(&orgs).
		Get(targetURL)

	if err != nil {
		logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 1 FAILED - Request error: %v", err)
		return "", fmt.Errorf("request failed: %w", err)
	}

	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 1 Response - Status: %d", resp.StatusCode)

	if !resp.IsSuccessState() {
		return "", fmt.Errorf("failed to get organizations: status %d, body: %s", resp.StatusCode, redactedOAuthResponseBody(resp))
	}

	if len(orgs) == 0 {
		return "", fmt.Errorf("no organizations found")
	}

	// 如果只有一个组织，直接使用
	if len(orgs) == 1 {
		logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 1 SUCCESS - Single org found, selected_org_present=%t", orgs[0].UUID != "")
		return orgs[0].UUID, nil
	}

	// 如果有多个组织，优先选择 raven_type 为 "team" 的组织
	for _, org := range orgs {
		if org.RavenType != nil && *org.RavenType == "team" {
			logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 1 SUCCESS - Selected team org, selected_org_present=%t, raven_type_present=%t", org.UUID != "", org.RavenType != nil)
			return org.UUID, nil
		}
	}

	// 如果没有 team 类型的组织，使用第一个
	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 1 SUCCESS - No team org found, using first org, selected_org_present=%t", orgs[0].UUID != "")
	return orgs[0].UUID, nil
}

func (s *claudeOAuthService) GetAuthorizationCode(ctx context.Context, sessionKey, orgUUID, scope, codeChallenge, state, proxyURL string) (string, error) {
	client, err := s.clientFactory(proxyURL)
	if err != nil {
		return "", fmt.Errorf("create HTTP client: %w", err)
	}

	authURL := fmt.Sprintf("%s/v1/oauth/%s/authorize", s.baseURL, orgUUID)

	reqBody := map[string]any{
		"response_type":         "code",
		"client_id":             oauth.ClientID,
		"organization_uuid":     orgUUID,
		"redirect_uri":          oauth.RedirectURI,
		"scope":                 scope,
		"state":                 state,
		"code_challenge":        codeChallenge,
		"code_challenge_method": "S256",
	}

	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 2: Getting authorization code from %s", safeClaudeOAuthAuthorizeURLForLog(s.baseURL, orgUUID))
	reqBodyJSON, _ := json.Marshal(redactClaudeOAuthLogMap(reqBody))
	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 2 Request Body: %s", string(reqBodyJSON))

	var result struct {
		RedirectURI string `json:"redirect_uri"`
	}

	resp, err := client.R().
		SetContext(ctx).
		SetCookies(&http.Cookie{
			Name:  "sessionKey",
			Value: sessionKey,
		}).
		SetHeader("Accept", "application/json").
		SetHeader("Accept-Language", "en-US,en;q=0.9").
		SetHeader("Cache-Control", "no-cache").
		SetHeader("Origin", "https://claude.ai").
		SetHeader("Referer", "https://claude.ai/new").
		SetHeader("Content-Type", "application/json").
		SetBody(reqBody).
		SetSuccessResult(&result).
		Post(authURL)

	if err != nil {
		safeErr := redactClaudeOAuthLogText(err.Error())
		logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 2 FAILED - Request error: %s", safeErr)
		return "", fmt.Errorf("request failed: %s", safeErr)
	}

	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 2 Response - Status: %d, Body: %s", resp.StatusCode, redactClaudeOAuthLogJSON(resp.Bytes()))

	if !resp.IsSuccessState() {
		return "", fmt.Errorf("failed to get authorization code: status %d, body: %s", resp.StatusCode, redactedOAuthResponseBody(resp))
	}

	if result.RedirectURI == "" {
		return "", fmt.Errorf("no redirect_uri in response")
	}

	parsedURL, err := url.Parse(result.RedirectURI)
	if err != nil {
		return "", fmt.Errorf("failed to parse redirect_uri: %w", err)
	}

	queryParams := parsedURL.Query()
	authCode := queryParams.Get("code")
	responseState := queryParams.Get("state")

	if authCode == "" {
		return "", fmt.Errorf("no authorization code in redirect_uri")
	}

	fullCode := authCode
	if responseState != "" {
		fullCode = authCode + "#" + responseState
	}

	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 2 SUCCESS - Got authorization code")
	return fullCode, nil
}

func (s *claudeOAuthService) ExchangeCodeForToken(ctx context.Context, code, codeVerifier, state, proxyURL string, isSetupToken bool) (*oauth.TokenResponse, error) {
	client, err := s.clientFactory(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("create HTTP client: %w", err)
	}

	// Parse code which may contain state in format "authCode#state"
	authCode := code
	codeState := ""
	if idx := strings.Index(code, "#"); idx != -1 {
		authCode = code[:idx]
		codeState = code[idx+1:]
	}

	formData := url.Values{}
	formData.Set("code", authCode)
	formData.Set("grant_type", "authorization_code")
	formData.Set("client_id", oauth.ClientID)
	formData.Set("redirect_uri", oauth.RedirectURI)
	formData.Set("code_verifier", codeVerifier)
	if codeState != "" {
		formData.Set("state", codeState)
	}

	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 3: Exchanging code for token at %s", s.tokenURL)
	logBody := map[string]any{}
	for key, values := range formData {
		if len(values) > 0 {
			logBody[key] = values[0]
		}
	}
	reqBodyJSON, _ := json.Marshal(logredact.RedactMap(logBody))
	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 3 Request Body: %s", string(reqBodyJSON))

	var tokenResp oauth.TokenResponse

	resp, err := client.R().
		SetContext(ctx).
		SetHeader("Accept", "application/json, text/plain, */*").
		SetHeader("User-Agent", "axios/1.13.6").
		SetFormDataFromValues(formData).
		SetSuccessResult(&tokenResp).
		Post(s.tokenURL)

	if err != nil {
		logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 3 FAILED - Request error: %v", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}

	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 3 Response - Status: %d, Body: %s", resp.StatusCode, redactClaudeOAuthLogJSON(resp.Bytes()))

	if !resp.IsSuccessState() {
		return nil, fmt.Errorf("token exchange failed: status %d, body: %s", resp.StatusCode, redactedOAuthResponseBody(resp))
	}

	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 3 SUCCESS - Got access token")
	return &tokenResp, nil
}

func (s *claudeOAuthService) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*oauth.TokenResponse, error) {
	client, err := s.clientFactory(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("create HTTP client: %w", err)
	}

	formData := url.Values{}
	formData.Set("grant_type", "refresh_token")
	formData.Set("refresh_token", refreshToken)
	formData.Set("client_id", oauth.ClientID)

	var tokenResp oauth.TokenResponse

	resp, err := client.R().
		SetContext(ctx).
		SetHeader("Accept", "application/json, text/plain, */*").
		SetHeader("User-Agent", "axios/1.13.6").
		SetFormDataFromValues(formData).
		SetSuccessResult(&tokenResp).
		Post(s.tokenURL)

	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if !resp.IsSuccessState() {
		return nil, fmt.Errorf("token refresh failed: status %d, body: %s", resp.StatusCode, redactedOAuthResponseBody(resp))
	}

	return &tokenResp, nil
}

const claudeOAuthBrowserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

func createReqClient(proxyURL string) (*req.Client, error) {
	// 禁用 CookieJar，确保每次授权都是干净的会话
	client := req.C().
		SetTimeout(60 * time.Second).
		ImpersonateChrome().
		SetCookieJar(nil) // 禁用 CookieJar

	trimmed, _, err := proxyurl.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	if trimmed != "" {
		client.SetProxyURL(trimmed)
	}

	return client, nil
}

func redactedOAuthResponseBody(resp *req.Response) string {
	if resp == nil {
		return ""
	}
	raw := resp.Bytes()
	if len(raw) == 0 {
		return ""
	}
	return redactClaudeOAuthLogText(string(raw))
}

var claudeOAuthExtraSensitiveLogKeys = []string{"organization_uuid", "org_uuid", "account_uuid", "email", "name"}

func redactClaudeOAuthLogMap(input map[string]any) map[string]any {
	return logredact.RedactMap(input, claudeOAuthExtraSensitiveLogKeys...)
}

func redactClaudeOAuthLogJSON(raw []byte) string {
	return logredact.RedactJSON(raw, claudeOAuthExtraSensitiveLogKeys...)
}

func redactClaudeOAuthLogText(input string) string {
	redacted := logredact.RedactText(input, claudeOAuthExtraSensitiveLogKeys...)
	return claudeOAuthAuthorizePathRefRe.ReplaceAllString(redacted, "/v1/oauth/<redacted>/authorize")
}

func safeClaudeOAuthAuthorizeURLForLog(baseURL, orgUUID string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "<claude-oauth-base>"
	}
	return fmt.Sprintf("%s/v1/oauth/<redacted>/authorize", base)
}

var claudeOAuthAuthorizePathRefRe = regexp.MustCompile(`/v1/oauth/[^/"\s]+/authorize`)
