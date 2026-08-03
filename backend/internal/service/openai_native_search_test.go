package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type nativeSearchHTTPUpstream struct {
	request      *http.Request
	requestBody  []byte
	responseCode int
	responseBody []byte
	responseType string
	err          error
	requestCount int
}

func (u *nativeSearchHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.roundTrip(req)
}

func (u *nativeSearchHTTPUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.roundTrip(req)
}

func (u *nativeSearchHTTPUpstream) roundTrip(req *http.Request) (*http.Response, error) {
	u.requestCount++
	u.request = req.Clone(req.Context())
	if req.Body != nil {
		u.requestBody, _ = io.ReadAll(req.Body)
	}
	if u.err != nil {
		return nil, u.err
	}
	contentType := u.responseType
	if contentType == "" {
		contentType = "application/json"
	}
	return &http.Response{
		StatusCode: u.responseCode,
		Header:     http.Header{"Content-Type": []string{contentType}, "X-Request-Id": []string{"req_search_test"}},
		Body:       io.NopCloser(bytes.NewReader(u.responseBody)),
	}, nil
}

func newNativeSearchServiceForTest(t *testing.T, upstream *nativeSearchHTTPUpstream) *OpenAIGatewayService {
	t.Helper()
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAICore.Enabled = true
	return NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil,
		upstream, nil, nil,
	)
}

func nativeSearchOAuthAccountForTest() *Account {
	return &Account{
		ID:          808,
		Name:        "native-search-test",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":       "managed-token",
			"chatgpt_account_id": "acct-native-search",
		},
	}
}

func nativeSearchAPIKeyAccountForTest() *Account {
	return &Account{
		ID:          809,
		Name:        "native-search-api-key-test",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"api_key":             "provider-api-key",
			"base_url":            "https://api-pool.example/v1",
			"openai_capabilities": []any{"responses", "search"},
		},
	}
}

func TestForwardNativeSearch_ForwardsOAuthNativeContractUnchanged(t *testing.T) {
	requestBody := []byte(`{"id":"search-session","model":"gpt-5.4","commands":{}}`)
	responseBody := []byte("{\n  \"encrypted_output\": \"opaque\", \"output\": \"result\"\n}")
	upstream := &nativeSearchHTTPUpstream{responseCode: http.StatusOK, responseBody: responseBody}
	svc := newNativeSearchServiceForTest(t, upstream)

	got, err := svc.ForwardNativeSearch(
		context.Background(),
		nil,
		nativeSearchOAuthAccountForTest(),
		http.Header{
			"Authorization": []string{"Bearer client-secret"},
			"User-Agent":    []string{"codex_cli_rs/0.144.2"},
			"Originator":    []string{"codex_cli_rs"},
			"OpenAI-Beta":   []string{"responses=experimental"},
		},
		requestBody,
	)

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, http.StatusOK, got.StatusCode)
	require.Equal(t, responseBody, got.Body)
	require.Equal(t, requestBody, upstream.requestBody)
	require.Equal(t, "https", upstream.request.URL.Scheme)
	require.Equal(t, "chatgpt.com", upstream.request.URL.Host)
	require.Equal(t, "/backend-api/codex/alpha/search", upstream.request.URL.Path)
	require.Equal(t, "Bearer managed-token", upstream.request.Header.Get("Authorization"))
	require.NotEqual(t, "Bearer client-secret", upstream.request.Header.Get("Authorization"))
	require.Equal(t, "acct-native-search", upstream.request.Header.Get("chatgpt-account-id"))
	require.Equal(t, "codex_cli_rs", upstream.request.Header.Get("originator"))
	require.Contains(t, upstream.request.Header.Get("User-Agent"), "codex_cli_rs/")
	require.Empty(t, upstream.request.Header.Get("OpenAI-Beta"))
}

func TestForwardNativeSearch_ForwardsExplicitAPIKeyNativeContract(t *testing.T) {
	requestBody := []byte(`{"id":"search-session","model":"gpt-5.6-terra","commands":{}}`)
	responseBody := []byte(`{"encrypted_output":null,"output":"result"}`)
	upstream := &nativeSearchHTTPUpstream{responseCode: http.StatusOK, responseBody: responseBody}
	svc := newNativeSearchServiceForTest(t, upstream)

	got, err := svc.ForwardNativeSearch(
		context.Background(),
		nil,
		nativeSearchAPIKeyAccountForTest(),
		http.Header{
			"Authorization": []string{"Bearer client-secret"},
			"User-Agent":    []string{"codex_cli_rs/0.146.0"},
			"Originator":    []string{"codex_cli_rs"},
		},
		requestBody,
	)

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, http.StatusOK, got.StatusCode)
	require.Equal(t, responseBody, got.Body)
	require.Equal(t, requestBody, upstream.requestBody)
	require.Equal(t, "https", upstream.request.URL.Scheme)
	require.Equal(t, "api-pool.example", upstream.request.URL.Host)
	require.Equal(t, "/v1/alpha/search", upstream.request.URL.Path)
	require.Equal(t, "Bearer provider-api-key", upstream.request.Header.Get("Authorization"))
	require.NotEqual(t, "Bearer client-secret", upstream.request.Header.Get("Authorization"))
	require.Empty(t, upstream.request.Header.Get("chatgpt-account-id"))
	require.Equal(t, "codex_cli_rs", upstream.request.Header.Get("originator"))
}

func TestValidateOpenAINativeSearchResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
		ok   bool
	}{
		{name: "output only", body: `{"output":"ok"}`, ok: true},
		{name: "empty output string", body: `{"output":""}`, ok: true},
		{name: "encrypted absent", body: `{"output":"ok","other":true}`, ok: true},
		{name: "encrypted null", body: `{"output":"ok","encrypted_output":null}`, ok: true},
		{name: "encrypted string", body: `{"output":"ok","encrypted_output":"opaque"}`, ok: true},
		{name: "empty", body: ``, ok: false},
		{name: "html", body: `<!doctype html>`, ok: false},
		{name: "array", body: `[]`, ok: false},
		{name: "empty object", body: `{}`, ok: false},
		{name: "output null", body: `{"output":null}`, ok: false},
		{name: "output number", body: `{"output":1}`, ok: false},
		{name: "encrypted object", body: `{"output":"ok","encrypted_output":{}}`, ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateOpenAINativeSearchResponse([]byte(tc.body))
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestForwardNativeSearch_InvalidSuccessBecomesJSON502(t *testing.T) {
	for _, body := range []string{"", "<!doctype html>", "{}", `{"output":null}`} {
		t.Run(body, func(t *testing.T) {
			upstream := &nativeSearchHTTPUpstream{responseCode: http.StatusOK, responseBody: []byte(body)}
			svc := newNativeSearchServiceForTest(t, upstream)

			got, err := svc.ForwardNativeSearch(context.Background(), nil, nativeSearchOAuthAccountForTest(), nil, []byte(`{"id":"id","model":"gpt-5.4"}`))

			require.Nil(t, got)
			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
			require.JSONEq(t, `{"error":{"type":"upstream_error","message":"Native Search upstream returned an invalid response"}}`, string(failoverErr.ResponseBody))
		})
	}
}

func TestForwardNativeSearch_CallerCancellationDoesNotFailOver(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	upstream := &nativeSearchHTTPUpstream{err: context.Canceled}
	svc := newNativeSearchServiceForTest(t, upstream)

	got, err := svc.ForwardNativeSearch(ctx, nil, nativeSearchOAuthAccountForTest(), nil, []byte(`{"id":"id","model":"gpt-5.4"}`))

	require.Nil(t, got)
	require.ErrorIs(t, err, context.Canceled)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
}

func TestForwardNativeSearch_CallerDeadlineDoesNotFailOver(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(context.DeadlineExceeded)
	upstream := &nativeSearchHTTPUpstream{err: context.DeadlineExceeded}
	svc := newNativeSearchServiceForTest(t, upstream)

	got, err := svc.ForwardNativeSearch(ctx, nil, nativeSearchOAuthAccountForTest(), nil, []byte(`{"id":"id","model":"gpt-5.4"}`))

	require.Nil(t, got)
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
}

func TestForwardNativeSearch_TransportTimeoutWithLiveCallerCanFailOver(t *testing.T) {
	upstream := &nativeSearchHTTPUpstream{err: context.DeadlineExceeded}
	svc := newNativeSearchServiceForTest(t, upstream)

	got, err := svc.ForwardNativeSearch(context.Background(), nil, nativeSearchOAuthAccountForTest(), nil, []byte(`{"id":"id","model":"gpt-5.4"}`))

	require.Nil(t, got)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
}

func TestForwardNativeSearch_MissingTokenSendsNoRequest(t *testing.T) {
	upstream := &nativeSearchHTTPUpstream{}
	svc := newNativeSearchServiceForTest(t, upstream)
	account := nativeSearchOAuthAccountForTest()
	account.Credentials = map[string]any{"chatgpt_account_id": "acct-native-search"}

	got, err := svc.ForwardNativeSearch(context.Background(), nil, account, nil, []byte(`{"id":"id","model":"gpt-5.4"}`))

	require.Nil(t, got)
	require.Error(t, err)
	require.Zero(t, upstream.requestCount)
}

func TestForwardNativeSearch_Transient502DoesNotCreateGlobalRuntimeBlock(t *testing.T) {
	upstream := &nativeSearchHTTPUpstream{
		responseCode: http.StatusBadGateway,
		responseBody: []byte(`{"error":{"type":"server_error","message":"temporary upstream failure"}}`),
	}
	svc := newNativeSearchServiceForTest(t, upstream)
	account := nativeSearchOAuthAccountForTest()

	got, err := svc.ForwardNativeSearch(context.Background(), nil, account, nil, []byte(`{"id":"id","model":"gpt-5.4"}`))

	require.Nil(t, got)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
}

func TestForwardNativeSearch_InsufficientBalanceNeverRetriesSameOAuthAccount(t *testing.T) {
	upstream := &nativeSearchHTTPUpstream{
		responseCode: http.StatusForbidden,
		responseBody: []byte(`{"code":"INSUFFICIENT_BALANCE","message":"insufficient balance"}`),
	}
	svc := newNativeSearchServiceForTest(t, upstream)

	got, err := svc.ForwardNativeSearch(context.Background(), nil, nativeSearchOAuthAccountForTest(), nil, []byte(`{"id":"id","model":"gpt-5.4"}`))

	require.Nil(t, got)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.False(t, failoverErr.RetryableOnSameAccount)
}
