package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/imroc/req/v3"
)

// ErrSparkShadowResetNotSupported is returned when ResetCredit is called on a
// spark shadow account. Shadow accounts do not hold credentials of their own;
// the caller must reset the parent account directly.
var ErrSparkShadowResetNotSupported = infraerrors.New(http.StatusConflict, "SPARK_SHADOW_RESET_NOT_SUPPORTED", "spark shadow account does not support credit reset; reset the parent account")

// Endpoints used by the OpenAI/ChatGPT/Codex quota query and reset feature.
const (
	chatGPTUsageURL             = "https://chatgpt.com/backend-api/wham/usage"
	chatGPTRateLimitCreditsURL  = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
	chatGPTRateLimitResetURL    = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume"
	openaiQuotaUpstreamTimeout  = 20 * time.Second
	openaiQuotaCodexBeta        = "codex-1"
	openaiQuotaCodexOriginator  = "Codex Desktop"
	openaiQuotaCodexLanguageTag = "zh-CN"
	openaiQuotaSecFetchSite     = "none"
	openaiQuotaSecFetchMode     = "no-cors"
	openaiQuotaSecFetchDest     = "empty"
)

// OpenAIRateLimitWindow describes one upstream rate-limit window. Only numeric
// quota fields are surfaced; raw upstream identifiers are deliberately omitted.
type OpenAIRateLimitWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAfterSeconds  int64   `json:"reset_after_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

type OpenAIRateLimit struct {
	Allowed         bool                   `json:"allowed"`
	LimitReached    bool                   `json:"limit_reached"`
	PrimaryWindow   *OpenAIRateLimitWindow `json:"primary_window,omitempty"`
	SecondaryWindow *OpenAIRateLimitWindow `json:"secondary_window,omitempty"`
}

type OpenAIAdditionalRateLimit struct {
	LimitName      string           `json:"limit_name"`
	MeteredFeature string           `json:"metered_feature"`
	RateLimit      *OpenAIRateLimit `json:"rate_limit,omitempty"`
}

// OpenAIRateLimitResetCreditDetail is sanitized metadata for one reset credit.
// Do not add upstream ids, user ids, account ids, tokens, or raw payload here.
type OpenAIRateLimitResetCreditDetail struct {
	ExpiresAt string `json:"expires_at,omitempty"`
}

type OpenAIRateLimitResetCredits struct {
	AvailableCount int                                `json:"available_count"`
	Credits        []OpenAIRateLimitResetCreditDetail `json:"credits,omitempty"`
}

// OpenAIQuotaUsage is a narrow admin-safe projection of /wham/usage.
type OpenAIQuotaUsage struct {
	PlanType              string                       `json:"plan_type,omitempty"`
	RateLimit             *OpenAIRateLimit             `json:"rate_limit,omitempty"`
	AdditionalRateLimits  []OpenAIAdditionalRateLimit  `json:"additional_rate_limits,omitempty"`
	RateLimitResetCredits *OpenAIRateLimitResetCredits `json:"rate_limit_reset_credits,omitempty"`
	FetchedAt             int64                        `json:"fetched_at"`
}

// OpenAIQuotaResetCredit omits upstream credit IDs and other identifiers.
type OpenAIQuotaResetCredit struct {
	ResetType       string `json:"reset_type,omitempty"`
	Status          string `json:"status,omitempty"`
	GrantedAt       string `json:"granted_at,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	RedeemStartedAt string `json:"redeem_started_at,omitempty"`
	RedeemedAt      string `json:"redeemed_at,omitempty"`
}

type OpenAIQuotaResetResult struct {
	Code         string                  `json:"code"`
	Credit       *OpenAIQuotaResetCredit `json:"credit,omitempty"`
	WindowsReset int                     `json:"windows_reset"`
}

type OpenAIQuotaService struct {
	accountRepo          AccountRepository
	proxyRepo            ProxyRepository
	tokenProvider        *OpenAITokenProvider
	privacyClientFactory PrivacyClientFactory
}

func NewOpenAIQuotaService(accountRepo AccountRepository, proxyRepo ProxyRepository, tokenProvider *OpenAITokenProvider, privacyClientFactory PrivacyClientFactory) *OpenAIQuotaService {
	return &OpenAIQuotaService{
		accountRepo:          accountRepo,
		proxyRepo:            proxyRepo,
		tokenProvider:        tokenProvider,
		privacyClientFactory: privacyClientFactory,
	}
}

func (s *OpenAIQuotaService) QueryUsage(ctx context.Context, accountID int64) (*OpenAIQuotaUsage, error) {
	accessToken, chatGPTAccountID, proxyURL, err := s.prepareUpstreamCall(ctx, accountID)
	if err != nil {
		return nil, err
	}
	client, err := s.privacyClientFactory(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_CLIENT_ERROR", "failed to build upstream client: %v", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()

	var payload OpenAIQuotaUsage
	resp, err := client.R().
		SetContext(callCtx).
		SetHeaders(buildOpenAIQuotaCodexHeaders(accessToken, chatGPTAccountID)).
		SetSuccessResult(&payload).
		Get(chatGPTUsageURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_REQUEST_FAILED", "upstream request failed: %v", err)
	}
	if !resp.IsSuccessState() {
		status := resp.StatusCode
		return nil, infraerrors.Newf(mapOpenAIQuotaUpstreamStatus(status), "OPENAI_QUOTA_UPSTREAM_ERROR", "upstream returned status %d", status)
	}

	payload.FetchedAt = time.Now().Unix()
	if payload.RateLimitResetCredits != nil && payload.RateLimitResetCredits.AvailableCount > 0 {
		payload.RateLimitResetCredits.Credits = s.queryResetCreditDetails(callCtx, client, accessToken, chatGPTAccountID)
	}
	return &payload, nil
}

func (s *OpenAIQuotaService) ResetCredit(ctx context.Context, accountID int64) (*OpenAIQuotaResetResult, error) {
	// Shadow guard: resetting credits via a shadow account would silently
	// operate on the parent's quota; callers must reset the parent directly.
	if s.accountRepo != nil {
		acc, loadErr := s.accountRepo.GetByID(ctx, accountID)
		if loadErr != nil {
			return nil, infraerrors.Newf(http.StatusNotFound, "OPENAI_QUOTA_ACCOUNT_NOT_FOUND", "account not found: %v", loadErr)
		}
		if acc != nil && acc.IsShadow() {
			return nil, ErrSparkShadowResetNotSupported
		}
	}

	accessToken, chatGPTAccountID, proxyURL, err := s.prepareUpstreamCall(ctx, accountID)
	if err != nil {
		return nil, err
	}
	redeemRequestID, err := generateOpenAIQuotaRedeemRequestID()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_QUOTA_REDEEM_ID_FAILED", "failed to generate redeem id: %v", err)
	}
	client, err := s.privacyClientFactory(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_CLIENT_ERROR", "failed to build upstream client: %v", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()

	headers := buildOpenAIQuotaCodexHeaders(accessToken, chatGPTAccountID)
	headers["content-type"] = "application/json"

	var payload OpenAIQuotaResetResult
	resp, err := client.R().
		SetContext(callCtx).
		SetHeaders(headers).
		SetBody(map[string]string{"redeem_request_id": redeemRequestID}).
		SetSuccessResult(&payload).
		Post(chatGPTRateLimitResetURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_RESET_REQUEST_FAILED", "upstream request failed: %v", err)
	}
	if !resp.IsSuccessState() {
		status := resp.StatusCode
		return nil, infraerrors.Newf(mapOpenAIQuotaUpstreamStatus(status), "OPENAI_QUOTA_RESET_UPSTREAM_ERROR", "upstream returned status %d", status)
	}
	return &payload, nil
}

func (s *OpenAIQuotaService) prepareUpstreamCall(ctx context.Context, accountID int64) (accessToken, chatGPTAccountID, proxyURL string, err error) {
	if s == nil || s.accountRepo == nil || s.tokenProvider == nil || s.privacyClientFactory == nil {
		return "", "", "", infraerrors.New(http.StatusInternalServerError, "OPENAI_QUOTA_NOT_CONFIGURED", "openai quota service is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return "", "", "", infraerrors.Newf(http.StatusNotFound, "OPENAI_QUOTA_ACCOUNT_NOT_FOUND", "account not found: %v", err)
	}
	if account == nil {
		return "", "", "", infraerrors.New(http.StatusNotFound, "OPENAI_QUOTA_ACCOUNT_NOT_FOUND", "account not found")
	}
	if account.Platform != PlatformOpenAI {
		return "", "", "", infraerrors.New(http.StatusBadRequest, "OPENAI_QUOTA_INVALID_PLATFORM", "account is not an OpenAI account")
	}
	if account.Type != AccountTypeOAuth {
		return "", "", "", infraerrors.New(http.StatusBadRequest, "OPENAI_QUOTA_INVALID_TYPE", "account is not an OAuth account")
	}

	// Spark shadows do not hold credentials; resolve to the parent before
	// reading chatgpt_account_id, token, or proxy settings.
	if account.IsShadow() {
		resolved, rerr := resolveCredentialAccount(ctx, s.accountRepo, account)
		if rerr != nil {
			return "", "", "", infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_SHADOW_RESOLVE_FAILED", "failed to resolve shadow account: %v", rerr)
		}
		account = resolved
	}

	chatGPTAccountID = strings.TrimSpace(account.GetChatGPTAccountID())
	if chatGPTAccountID == "" {
		chatGPTAccountID = strings.TrimSpace(account.GetOpenAIOrganizationID())
	}
	if chatGPTAccountID == "" {
		return "", "", "", infraerrors.New(http.StatusBadRequest, "OPENAI_QUOTA_MISSING_ACCOUNT_ID", "chatgpt account id is missing; please re-authorize this account")
	}

	accessToken, err = s.tokenProvider.GetAccessToken(ctx, account)
	if err != nil {
		return "", "", "", infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_TOKEN_UNAVAILABLE", "failed to acquire access token: %v", err)
	}
	if strings.TrimSpace(accessToken) == "" {
		return "", "", "", infraerrors.New(http.StatusBadGateway, "OPENAI_QUOTA_TOKEN_UNAVAILABLE", "access token is empty")
	}

	if account.ProxyID != nil {
		switch {
		case account.Proxy != nil:
			proxyURL = account.Proxy.URL()
		case s.proxyRepo != nil:
			if proxy, perr := s.proxyRepo.GetByID(ctx, *account.ProxyID); perr == nil && proxy != nil {
				proxyURL = proxy.URL()
			}
		}
	}
	return accessToken, chatGPTAccountID, proxyURL, nil
}

func (s *OpenAIQuotaService) queryResetCreditDetails(ctx context.Context, client *req.Client, accessToken, chatGPTAccountID string) []OpenAIRateLimitResetCreditDetail {
	if client == nil {
		return nil
	}
	resp, err := client.R().
		SetContext(ctx).
		SetHeaders(buildOpenAIQuotaCodexHeaders(accessToken, chatGPTAccountID)).
		Get(chatGPTRateLimitCreditsURL)
	if err != nil || !resp.IsSuccessState() {
		return nil
	}
	credits, err := parseOpenAIRateLimitResetCreditDetails(resp.Bytes())
	if err != nil {
		return nil
	}
	return credits
}

func buildOpenAIQuotaCodexHeaders(accessToken, chatGPTAccountID string) map[string]string {
	return map[string]string{
		"authorization":      "Bearer " + accessToken,
		"chatgpt-account-id": chatGPTAccountID,
		"openai-beta":        openaiQuotaCodexBeta,
		"oai-language":       openaiQuotaCodexLanguageTag,
		"originator":         openaiQuotaCodexOriginator,
		"accept":             "application/json",
		"sec-fetch-site":     openaiQuotaSecFetchSite,
		"sec-fetch-mode":     openaiQuotaSecFetchMode,
		"sec-fetch-dest":     openaiQuotaSecFetchDest,
		"priority":           "u=4, i",
	}
}

func generateOpenAIQuotaRedeemRequestID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	hexStr := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexStr[0:8], hexStr[8:12], hexStr[12:16], hexStr[16:20], hexStr[20:]), nil
}

// buildCodexSparkWindowExtraUpdates extracts Codex Spark usage windows from the
// /wham/usage response body's codex_bengalfox additional rate-limit entry.
func buildCodexSparkWindowExtraUpdates(usage *OpenAIQuotaUsage, now time.Time) map[string]any {
	if usage == nil {
		return nil
	}
	var spark *OpenAIRateLimit
	for i := range usage.AdditionalRateLimits {
		a := usage.AdditionalRateLimits[i]
		if a.MeteredFeature == "codex_bengalfox" {
			spark = a.RateLimit
			break
		}
	}
	if spark == nil {
		return nil
	}

	snap := &OpenAICodexUsageSnapshot{}
	if w := spark.PrimaryWindow; w != nil {
		p := w.UsedPercent
		snap.PrimaryUsedPercent = &p
		ra := int(w.ResetAfterSeconds)
		snap.PrimaryResetAfterSeconds = &ra
		wm := int(w.LimitWindowSeconds / 60)
		snap.PrimaryWindowMinutes = &wm
	}
	if w := spark.SecondaryWindow; w != nil {
		p := w.UsedPercent
		snap.SecondaryUsedPercent = &p
		ra := int(w.ResetAfterSeconds)
		snap.SecondaryResetAfterSeconds = &ra
		wm := int(w.LimitWindowSeconds / 60)
		snap.SecondaryWindowMinutes = &wm
	}

	normalized := snap.Normalize()
	if normalized == nil {
		return nil
	}

	updates := make(map[string]any)
	if normalized.Used5hPercent != nil {
		updates["codex_5h_used_percent"] = *normalized.Used5hPercent
	}
	if normalized.Reset5hSeconds != nil {
		updates["codex_5h_reset_after_seconds"] = *normalized.Reset5hSeconds
	}
	if normalized.Window5hMinutes != nil {
		updates["codex_5h_window_minutes"] = *normalized.Window5hMinutes
	}
	if normalized.Used7dPercent != nil {
		updates["codex_7d_used_percent"] = *normalized.Used7dPercent
	}
	if normalized.Reset7dSeconds != nil {
		updates["codex_7d_reset_after_seconds"] = *normalized.Reset7dSeconds
	}
	if normalized.Window7dMinutes != nil {
		updates["codex_7d_window_minutes"] = *normalized.Window7dMinutes
	}
	if r := codexResetAtRFC3339(now, normalized.Reset5hSeconds); r != nil {
		updates["codex_5h_reset_at"] = *r
	}
	if r := codexResetAtRFC3339(now, normalized.Reset7dSeconds); r != nil {
		updates["codex_7d_reset_at"] = *r
	}
	if len(updates) == 0 {
		return nil
	}
	updates["codex_usage_updated_at"] = now.Format(time.RFC3339)
	return updates
}

func mapOpenAIQuotaUpstreamStatus(status int) int {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return status
	case status == http.StatusTooManyRequests:
		return http.StatusTooManyRequests
	case status >= 400:
		return http.StatusBadGateway
	default:
		return http.StatusBadGateway
	}
}

type openAIRateLimitResetCreditDetailPayload struct {
	ExpiresAt      string `json:"expires_at,omitempty"`
	ExpiresAtCamel string `json:"expiresAt,omitempty"`
}

type openAIRateLimitResetCreditDetailsPayload struct {
	Credits               []openAIRateLimitResetCreditDetailPayload `json:"credits,omitempty"`
	RateLimitResetCredits []openAIRateLimitResetCreditDetailPayload `json:"rate_limit_reset_credits,omitempty"`
	Items                 []openAIRateLimitResetCreditDetailPayload `json:"items,omitempty"`
	Data                  []openAIRateLimitResetCreditDetailPayload `json:"data,omitempty"`
}

func parseOpenAIRateLimitResetCreditDetails(body []byte) ([]OpenAIRateLimitResetCreditDetail, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, nil
	}

	var rawCredits []openAIRateLimitResetCreditDetailPayload
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &rawCredits); err != nil {
			return nil, err
		}
	} else {
		var payload openAIRateLimitResetCreditDetailsPayload
		if err := json.Unmarshal(trimmed, &payload); err != nil {
			return nil, err
		}
		rawCredits = firstNonEmptyOpenAIQuotaResetCreditPayload(
			payload.Credits,
			payload.RateLimitResetCredits,
			payload.Items,
			payload.Data,
		)
	}

	credits := make([]OpenAIRateLimitResetCreditDetail, 0, len(rawCredits))
	for _, raw := range rawCredits {
		expiresAt := strings.TrimSpace(raw.ExpiresAt)
		if expiresAt == "" {
			expiresAt = strings.TrimSpace(raw.ExpiresAtCamel)
		}
		if expiresAt == "" {
			continue
		}
		credits = append(credits, OpenAIRateLimitResetCreditDetail{ExpiresAt: expiresAt})
	}
	return credits, nil
}

func firstNonEmptyOpenAIQuotaResetCreditPayload(lists ...[]openAIRateLimitResetCreditDetailPayload) []openAIRateLimitResetCreditDetailPayload {
	for _, list := range lists {
		if len(list) > 0 {
			return list
		}
	}
	return nil
}
