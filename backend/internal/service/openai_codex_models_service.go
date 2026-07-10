package service

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var chatgptCodexModelsURL = "https://chatgpt.com/backend-api/codex/models"

const codexModelsManifestBodyLimit int64 = 8 << 20

type CodexModelsManifest struct {
	Body        []byte
	ETag        string
	NotModified bool
}

// SelectCodexModelsAccount selects a schedulable OpenAI OAuth account whose credentials can fetch a manifest.
func (s *OpenAIGatewayService) SelectCodexModelsAccount(ctx context.Context, groupID *int64) (*Account, error) {
	excludedIDs := make(map[int64]struct{})
	for {
		account, err := s.SelectAccountForModelWithExclusions(ctx, groupID, "", "", excludedIDs)
		if err != nil {
			return nil, err
		}
		credentialAccount, credentialErr := resolveCredentialAccount(ctx, s.accountRepo, account)
		if account.IsOpenAIOAuth() && credentialErr == nil && credentialAccount.IsOpenAIOAuth() && strings.TrimSpace(credentialAccount.GetOpenAIAccessToken()) != "" {
			return account, nil
		}
		excludedIDs[account.ID] = struct{}{}
	}
}

func (s *OpenAIGatewayService) FetchCodexModelsManifest(ctx context.Context, account *Account, clientVersion, ifNoneMatch string) (*CodexModelsManifest, error) {
	if account == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_ACCOUNT_REQUIRED", "account is required")
	}
	if !account.IsOpenAIOAuth() {
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_CODEX_MODELS_OAUTH_REQUIRED", "a schedulable OpenAI OAuth account is required")
	}

	var accountRepo AccountRepository
	if s != nil {
		accountRepo = s.accountRepo
	}
	credentialAccount, err := resolveCredentialAccount(ctx, accountRepo, account)
	if err != nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_CREDENTIALS_FAILED", "could not resolve manifest credentials")
	}
	accessToken := credentialAccount.GetOpenAIAccessToken()
	if accessToken == "" {
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_CODEX_MODELS_TOKEN_MISSING", "account has no Codex backend access token")
	}

	clientVersion = strings.TrimSpace(clientVersion)
	if clientVersion == "" {
		clientVersion = openAICodexProbeVersion
	}

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, buildCodexModelsManifestURL(chatgptCodexModelsURL, clientVersion), nil)
	if err != nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_REQUEST_FAILED", "could not create manifest request")
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("Version", clientVersion)
	req.Header.Set("User-Agent", codexCLIUserAgent)
	if ifNoneMatch = strings.TrimSpace(ifNoneMatch); ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	setOpenAIChatGPTAccountHeaders(req.Header, credentialAccount)

	resp, err := s.sendOpenAIHTTPRequest(reqCtx, nil, req, account)
	if err != nil {
		if isOpenAIEgressPolicyError(err) {
			return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_CODEX_MODELS_EGRESS_REJECTED", "codex models manifest egress policy rejected").WithCause(err)
		}
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "codex models manifest request failed")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotModified {
		return &CodexModelsManifest{ETag: resp.Header.Get("ETag"), NotModified: true}, nil
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "codex models manifest upstream error: status=%d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, codexModelsManifestBodyLimit+1))
	if err != nil {
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "could not read codex models manifest response")
	}
	if int64(len(body)) > codexModelsManifestBodyLimit {
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "codex models manifest response exceeds size limit")
	}
	return &CodexModelsManifest{Body: body, ETag: resp.Header.Get("ETag")}, nil
}

func buildCodexModelsManifestURL(endpoint, clientVersion string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpoint + "?client_version=" + url.QueryEscape(clientVersion)
	}
	query := parsed.Query()
	query.Set("client_version", clientVersion)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
