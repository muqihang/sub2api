package service

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func (s *GatewayService) buildUpstreamRequestClaudePlatformAWS(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	_ string,
	_ string,
) (*http.Request, []byte, error) {
	if err := ValidateClaudePlatformAWSNoBypassForRoute(account, false, claudePlatformAWSDirectBuilderDiagnosticAllowed(c)); err != nil {
		return nil, nil, err
	}
	validation, err := ValidateClaudePlatformAWSAccount(account)
	if err != nil {
		return nil, nil, err
	}

	authScheme, err := resolveClaudePlatformAWSBuilderAuthScheme(account, validation)
	if err != nil {
		return nil, nil, err
	}

	apiKey := strings.TrimSpace(account.GetCredential("api_key"))
	if apiKey == "" {
		return nil, nil, fmt.Errorf("api_key not found in claude-platform-aws credentials")
	}
	workspaceID := strings.TrimSpace(account.GetCredential("anthropic_workspace_id"))
	if workspaceID == "" {
		return nil, nil, fmt.Errorf("workspace id is invalid")
	}

	clientHeaders := http.Header{}
	if c != nil && c.Request != nil {
		clientHeaders = SanitizeClaudePlatformAWSInboundHeaders(c.Request.Header)
	}

	body, err = applyClaudePlatformAWSRequestShapeProfile(body, account.GetExtraString(ClaudePlatformAWSExtraRequestShapeProfileRef))
	if err != nil {
		return nil, nil, err
	}
	finalBeta, setBeta := claudePlatformAWSFinalBetaHeader(account, clientHeaders)
	if sanitized, changed := sanitizeAnthropicBodyForBetaTokens(body, finalBeta); changed {
		body = sanitized
	}

	targetURL := strings.TrimRight(validation.Endpoint, "/") + claudePlatformAWSAllowedPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}

	if accept := strings.TrimSpace(getHeaderRaw(clientHeaders, "accept")); accept != "" {
		setHeaderRaw(req.Header, "accept", accept)
	}
	setHeaderRaw(req.Header, "content-type", "application/json")
	setHeaderRaw(req.Header, "anthropic-version", "2023-06-01")
	setHeaderRaw(req.Header, "anthropic-workspace-id", workspaceID)
	switch authScheme {
	case ClaudePlatformAWSAuthProfileXAPIKey:
		setHeaderRaw(req.Header, "x-api-key", apiKey)
	case ClaudePlatformAWSAuthProfileBearerAPIKey:
		setHeaderRaw(req.Header, "authorization", "Bearer "+apiKey)
	default:
		return nil, nil, fmt.Errorf("%s: unsupported auth scheme", ClaudePlatformAWSAuthProfileBlocked)
	}
	if setBeta {
		setHeaderRaw(req.Header, "anthropic-beta", finalBeta)
	}

	if err := VerifyClaudePlatformAWSFinalRequest(ClaudePlatformAWSFinalVerifierInput{
		FinalURL:            req.URL.String(),
		Headers:             req.Header,
		Body:                body,
		Region:              validation.Region,
		AuthScheme:          authScheme,
		WorkspaceFromServer: true,
		AuthFromServer:      true,
		AllowedPath:         claudePlatformAWSAllowedPath,
	}); err != nil {
		return nil, nil, err
	}
	return req, body, nil
}

func resolveClaudePlatformAWSBuilderAuthScheme(account *Account, validation ClaudePlatformAWSAccountValidation) (string, error) {
	if account == nil {
		return "", fmt.Errorf("claude-platform-aws account is required")
	}
	if err := validateClaudePlatformAWSStoredCP0Bindings(account, validation); err != nil {
		return "", err
	}
	selected := strings.TrimSpace(account.GetExtraString(ClaudePlatformAWSExtraAuthScheme))
	evidence := ClaudePlatformAWSAuthEvidence{
		XAPIKeyProven:    selected == ClaudePlatformAWSAuthProfileXAPIKey,
		BearerAPIProven:  selected == ClaudePlatformAWSAuthProfileBearerAPIKey,
		SelectedProfile:  selected,
		Endpoint:         validation.Endpoint,
		Region:           validation.Region,
		WorkspaceRef:     validation.WorkspaceRef,
		RequestShapePath: claudePlatformAWSAllowedPath,
	}
	return ResolveClaudePlatformAWSAuthProfile(evidence)
}

func claudePlatformAWSFinalBetaHeader(_ *Account, _ http.Header) (string, bool) {
	// Phase 1 uses a provider-owned strip profile: client beta tokens are observations only.
	return "", false
}

const claudePlatformAWSDirectBuilderDiagnosticAllowedKey = "claude_platform_aws_direct_builder_diagnostic_allowed"

func markClaudePlatformAWSDirectBuilderDiagnosticAllowed(c *gin.Context) {
	if c != nil {
		c.Set(claudePlatformAWSDirectBuilderDiagnosticAllowedKey, true)
	}
}

func claudePlatformAWSDirectBuilderDiagnosticAllowed(c *gin.Context) bool {
	if c == nil {
		return false
	}
	allowed, ok := c.Get(claudePlatformAWSDirectBuilderDiagnosticAllowedKey)
	if !ok {
		return false
	}
	allowedBool, ok := allowed.(bool)
	return ok && allowedBool
}

func (s *GatewayService) shouldUseCCGatewayClaudePlatformAWS(account *Account) bool {
	return account != nil && account.IsClaudePlatformAWS() && ccGatewayAnthropicEnabled(s.cfg)
}

func (s *GatewayService) buildUpstreamRequestClaudePlatformAWSCCGateway(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	modelID string,
) (*http.Request, []byte, error) {
	validation, err := ValidateClaudePlatformAWSAccount(account)
	if err != nil {
		return nil, nil, err
	}
	if err := validateClaudePlatformAWSStoredCP0Bindings(account, validation); err != nil {
		return nil, nil, err
	}
	if !IsClaudePlatformAWSFormalPoolAccount(account) {
		return nil, nil, fmt.Errorf("claude-platform-aws formal-pool account is not eligible for cc gateway")
	}
	authScheme := strings.TrimSpace(account.GetExtraString(ClaudePlatformAWSExtraAuthScheme))
	if authScheme != ClaudePlatformAWSAuthProfileXAPIKey && authScheme != ClaudePlatformAWSAuthProfileBearerAPIKey {
		return nil, nil, fmt.Errorf("%s: unsupported auth scheme", ClaudePlatformAWSAuthProfileBlocked)
	}
	body, err = applyClaudePlatformAWSRequestShapeProfile(body, account.GetExtraString(ClaudePlatformAWSExtraRequestShapeProfileRef))
	if err != nil {
		return nil, nil, err
	}
	if sanitized, changed := sanitizeAnthropicBodyForBetaTokens(body, ""); changed {
		body = sanitized
	}
	targetURL, err := s.ccGatewayAnthropicRequestURL(claudePlatformAWSAllowedPath)
	if err != nil {
		return nil, nil, err
	}
	clientHeaders := http.Header{}
	if c != nil && c.Request != nil {
		clientHeaders = SanitizeClaudePlatformAWSInboundHeaders(c.Request.Header)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	if accept := strings.TrimSpace(getHeaderRaw(clientHeaders, "accept")); accept != "" {
		setHeaderRaw(req.Header, "accept", accept)
	}
	setHeaderRaw(req.Header, "content-type", "application/json")
	setHeaderRaw(req.Header, "anthropic-version", "2023-06-01")
	setHeaderRaw(req.Header, "x-api-key", strings.TrimSpace(account.GetCredential("api_key")))
	ctx = withCCGatewayObservedClientProfileSeed(ctx, clientHeaders)
	if c != nil && c.Request != nil {
		seedCCGatewayClaudeCodeSessionMappingInput(ctx, req, c.Request.Header)
	}
	if err := applyCCGatewayAnthropicHeaders(req, s.cfg, account, "apikey"); err != nil {
		return nil, nil, err
	}
	attachCCGatewayObservedClientProfileSnapshot(req)
	applyCCGatewayAnthropicPolicyVersion(ctx, req, account)
	if err := ApplyClaudeCodePathAuditHeaders(req.Header, ctx); err != nil {
		return nil, nil, err
	}
	if mappedBody := claudeCodeReadRequestBody(req); len(mappedBody) > 0 {
		body = mappedBody
		claudeCodeReplaceRequestBody(req, body)
	}
	if err := applyCCGatewayFormalPoolAttestation(req, s.cfg, account); err != nil {
		return nil, nil, err
	}
	_ = modelID
	return req, body, nil
}
