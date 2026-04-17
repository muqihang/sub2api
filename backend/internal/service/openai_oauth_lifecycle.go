package service

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const (
	OpenAIPoolRoleMain       = "main"
	OpenAIPoolRoleQuarantine = "quarantine"

	OpenAIAuthStateHealthy  = "healthy"
	OpenAIAuthStateCooling  = "cooling"
	OpenAIAuthStateTerminal = "terminal"
	OpenAIAuthStateATOnly   = "at_only"

	OpenAITokenSourceRTManaged = "rt_managed"
	OpenAITokenSourceATOnly    = "at_only"

	OpenAIValidationOutcomeRTValidated                  = "rt_validated"
	OpenAIValidationOutcomeRTValidationRetryableFailure = "rt_validation_retryable_failure"
	OpenAIValidationOutcomeRTValidationTerminalFailure  = "rt_validation_terminal_failure"
	OpenAIValidationOutcomeATOnlyQuarantined            = "at_only_quarantined"

	openAIAuthErrorCodeUnknown          = "oauth_refresh_failed"
	openAIAuthErrorCodeTokenInvalidated = "token_invalidated"
	openAIAuthErrorCodeTokenRevoked     = "token_revoked"
	openAIAuthErrorCodeWorkspaceDown    = "deactivated_workspace"
	openAIAuthErrorCodeRTExpired        = "refresh_token_expired"
	openAIAuthErrorCodeRTReused         = "refresh_token_reused"
	openAIAuthErrorCodeInvalidGrant     = "invalid_grant"
)

type OpenAIImportLifecycleDecision struct {
	PoolRole          string
	AuthState         string
	TokenSource       string
	ValidationOutcome string
	Status            string
	Schedulable       bool
	Credentials       map[string]any
	Extra             map[string]any
	RefreshErrorCode  string
}

func cloneJSONMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

func normalizeOpenAIImportedCredentials(credentials map[string]any) map[string]any {
	out := cloneJSONMap(credentials)
	if strings.TrimSpace(stringValue(out["client_id"])) == "" && strings.TrimSpace(stringValue(out["refresh_token"])) != "" {
		out["client_id"] = openai.ClientID
	}
	return out
}

func stringValue(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	default:
		return ""
	}
}

func EvaluateOpenAIImportLifecycle(
	ctx context.Context,
	openaiOAuthService *OpenAIOAuthService,
	proxyURL string,
	credentials map[string]any,
) (*OpenAIImportLifecycleDecision, error) {
	normalized := normalizeOpenAIImportedCredentials(credentials)
	now := time.Now().UTC().Format(time.RFC3339)

	if strings.TrimSpace(stringValue(normalized["refresh_token"])) == "" {
		return &OpenAIImportLifecycleDecision{
			PoolRole:          OpenAIPoolRoleQuarantine,
			AuthState:         OpenAIAuthStateATOnly,
			TokenSource:       OpenAITokenSourceATOnly,
			ValidationOutcome: OpenAIValidationOutcomeATOnlyQuarantined,
			Status:            StatusDisabled,
			Schedulable:       false,
			Credentials:       normalized,
			Extra: map[string]any{
				"openai_pool_role":               OpenAIPoolRoleQuarantine,
				"openai_auth_state":              OpenAIAuthStateATOnly,
				"openai_token_source":            OpenAITokenSourceATOnly,
				"openai_validation_outcome":      OpenAIValidationOutcomeATOnlyQuarantined,
				"openai_last_refresh_error_code": "",
				"openai_last_validated_at":       "",
			},
		}, nil
	}

	if openaiOAuthService == nil {
		return nil, ErrAccountNilInput
	}

	clientID := stringValue(normalized["client_id"])
	tokenInfo, err := openaiOAuthService.RefreshTokenWithClientID(ctx, stringValue(normalized["refresh_token"]), proxyURL, clientID)
	if err != nil {
		errorCode := classifyOpenAIRefreshError(err)
		decision := &OpenAIImportLifecycleDecision{
			PoolRole:         OpenAIPoolRoleQuarantine,
			TokenSource:      OpenAITokenSourceRTManaged,
			Credentials:      normalized,
			RefreshErrorCode: errorCode,
		}
		if isTerminalOpenAIAuthErrorCode(errorCode) {
			decision.AuthState = OpenAIAuthStateTerminal
			decision.ValidationOutcome = OpenAIValidationOutcomeRTValidationTerminalFailure
			decision.Status = StatusError
		} else {
			decision.AuthState = OpenAIAuthStateCooling
			decision.ValidationOutcome = OpenAIValidationOutcomeRTValidationRetryableFailure
			decision.Status = StatusDisabled
		}
		decision.Schedulable = false
		decision.Extra = map[string]any{
			"openai_pool_role":               decision.PoolRole,
			"openai_auth_state":              decision.AuthState,
			"openai_token_source":            decision.TokenSource,
			"openai_validation_outcome":      decision.ValidationOutcome,
			"openai_last_refresh_error_code": errorCode,
			"openai_last_validated_at":       "",
		}
		return decision, nil
	}

	validated := openaiOAuthService.BuildAccountCredentials(tokenInfo)
	validated = MergeCredentials(normalized, validated)

	return &OpenAIImportLifecycleDecision{
		PoolRole:          OpenAIPoolRoleMain,
		AuthState:         OpenAIAuthStateHealthy,
		TokenSource:       OpenAITokenSourceRTManaged,
		ValidationOutcome: OpenAIValidationOutcomeRTValidated,
		Status:            StatusActive,
		Schedulable:       true,
		Credentials:       validated,
		RefreshErrorCode:  "",
		Extra: map[string]any{
			"openai_pool_role":               OpenAIPoolRoleMain,
			"openai_auth_state":              OpenAIAuthStateHealthy,
			"openai_token_source":            OpenAITokenSourceRTManaged,
			"openai_validation_outcome":      OpenAIValidationOutcomeRTValidated,
			"openai_last_refresh_error_code": "",
			"openai_last_validated_at":       now,
		},
	}, nil
}

func classifyOpenAIRefreshError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, openAIAuthErrorCodeRTExpired):
		return openAIAuthErrorCodeRTExpired
	case strings.Contains(msg, openAIAuthErrorCodeRTReused):
		return openAIAuthErrorCodeRTReused
	case strings.Contains(msg, openAIAuthErrorCodeInvalidGrant):
		return openAIAuthErrorCodeInvalidGrant
	case strings.Contains(msg, openAIAuthErrorCodeTokenInvalidated):
		return openAIAuthErrorCodeTokenInvalidated
	case strings.Contains(msg, openAIAuthErrorCodeTokenRevoked):
		return openAIAuthErrorCodeTokenRevoked
	case strings.Contains(msg, openAIAuthErrorCodeWorkspaceDown):
		return openAIAuthErrorCodeWorkspaceDown
	default:
		return openAIAuthErrorCodeUnknown
	}
}

func isTerminalOpenAIAuthErrorCode(code string) bool {
	switch strings.TrimSpace(code) {
	case openAIAuthErrorCodeRTExpired,
		openAIAuthErrorCodeRTReused,
		openAIAuthErrorCodeInvalidGrant,
		openAIAuthErrorCodeTokenInvalidated,
		openAIAuthErrorCodeTokenRevoked,
		openAIAuthErrorCodeWorkspaceDown:
		return true
	default:
		return false
	}
}

func ApplyOpenAIImportLifecycle(account *Account, decision *OpenAIImportLifecycleDecision) {
	if account == nil || decision == nil {
		return
	}
	account.Credentials = cloneJSONMap(decision.Credentials)
	account.Extra = mergeMap(account.Extra, decision.Extra)
	account.Status = decision.Status
	account.Schedulable = decision.Schedulable
	if decision.Status == StatusError {
		account.ErrorMessage = "OpenAI OAuth validation failed: " + decision.RefreshErrorCode
	}
}

func (a *Account) GetOpenAIPoolRole() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.GetExtraString("openai_pool_role"))
}

func (a *Account) GetOpenAIAuthState() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.GetExtraString("openai_auth_state"))
}

func (a *Account) GetOpenAITokenSource() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.GetExtraString("openai_token_source"))
}

func (a *Account) IsOpenAIATOnly() bool {
	return a != nil && a.IsOpenAIOAuth() && a.GetOpenAITokenSource() == OpenAITokenSourceATOnly
}

func (a *Account) IsOpenAIRTManaged() bool {
	return a != nil && a.IsOpenAIOAuth() && a.GetOpenAITokenSource() != OpenAITokenSourceATOnly && strings.TrimSpace(a.GetOpenAIRefreshToken()) != ""
}

func (a *Account) ShouldParticipateInOpenAIManagedRefresh() bool {
	if a == nil || !a.IsOpenAIRTManaged() {
		return false
	}
	if a.GetOpenAIAuthState() == OpenAIAuthStateTerminal {
		return false
	}
	switch a.Status {
	case StatusActive:
		return true
	case StatusDisabled:
		return a.GetOpenAIPoolRole() == OpenAIPoolRoleQuarantine
	default:
		return false
	}
}

func FindMatchingOpenAIOAuthAccount(accounts []Account, credentials map[string]any) (*Account, string) {
	refreshToken := strings.TrimSpace(stringValue(credentials["refresh_token"]))
	if refreshToken != "" {
		for i := range accounts {
			if accounts[i].GetOpenAIRefreshToken() == refreshToken {
				return &accounts[i], "refresh_token"
			}
		}
	}

	accountID := strings.TrimSpace(stringValue(credentials["chatgpt_account_id"]))
	if accountID != "" {
		for i := range accounts {
			if accounts[i].GetChatGPTAccountID() == accountID {
				return &accounts[i], "chatgpt_account_id"
			}
		}
	}

	userID := strings.TrimSpace(stringValue(credentials["chatgpt_user_id"]))
	if userID != "" {
		for i := range accounts {
			if accounts[i].GetChatGPTUserID() == userID {
				return &accounts[i], "chatgpt_user_id"
			}
		}
	}

	email := strings.ToLower(strings.TrimSpace(stringValue(credentials["email"])))
	if email != "" {
		for i := range accounts {
			if strings.EqualFold(accounts[i].GetCredential("email"), email) {
				return &accounts[i], "email"
			}
		}
	}

	accessToken := strings.TrimSpace(stringValue(credentials["access_token"]))
	if accessToken != "" {
		for i := range accounts {
			if accounts[i].GetOpenAIAccessToken() == accessToken {
				return &accounts[i], "access_token"
			}
		}
	}

	return nil, ""
}

func ShouldOverwriteMatchedOpenAIAccount(existing *Account, matchKey string, decision *OpenAIImportLifecycleDecision) bool {
	if existing == nil || decision == nil {
		return false
	}

	if matchKey == "refresh_token" {
		return true
	}

	if decision.TokenSource == OpenAITokenSourceATOnly {
		return !existing.IsOpenAIRTManaged()
	}

	if decision.ValidationOutcome != OpenAIValidationOutcomeRTValidated {
		return false
	}

	return true
}
