package xai

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAuthorizationInputTracksStateRequirement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		raw               string
		wantCode          string
		wantState         string
		wantRequiresState bool
	}{
		{
			name:              "full callback url",
			raw:               "http://127.0.0.1:56121/callback?code=abc123&state=state456",
			wantCode:          "abc123",
			wantState:         "state456",
			wantRequiresState: true,
		},
		{
			name:              "query string missing state still requires state validation",
			raw:               "code=abc123",
			wantCode:          "abc123",
			wantRequiresState: true,
		},
		{
			name:     "bare code remains accepted for manual fallback",
			raw:      "abc123",
			wantCode: "abc123",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ParseAuthorizationInput(tt.raw)
			require.Equal(t, tt.wantCode, got.Code)
			require.Equal(t, tt.wantState, got.State)
			require.Equal(t, tt.wantRequiresState, got.RequiresState)
		})
	}
}

func TestBuildAuthorizationURLAndXAIURLValidation(t *testing.T) {
	t.Setenv(EnvAuthorizeURL, "https://auth.example.test/oauth2/authorize")
	t.Setenv(EnvClientID, "client-id")
	t.Setenv(EnvScope, "openid profile offline_access api:access")
	t.Setenv(EnvAllowUnsafeURLOverrides, "true")

	authURL, err := BuildAuthorizationURL("state", "challenge", "http://127.0.0.1:56121/callback", "nonce")
	require.NoError(t, err)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)

	values := parsed.Query()
	require.Equal(t, "https", parsed.Scheme)
	require.Equal(t, "auth.example.test", parsed.Host)
	require.Equal(t, "/oauth2/authorize", parsed.Path)
	require.Equal(t, "code", values.Get("response_type"))
	require.Equal(t, "client-id", values.Get("client_id"))
	require.Equal(t, "http://127.0.0.1:56121/callback", values.Get("redirect_uri"))
	require.Equal(t, "openid profile offline_access api:access", values.Get("scope"))
	require.Equal(t, "state", values.Get("state"))
	require.Equal(t, "nonce", values.Get("nonce"))
	require.Equal(t, "challenge", values.Get("code_challenge"))
	require.Equal(t, "S256", values.Get("code_challenge_method"))
	require.Equal(t, "generic", values.Get("plan"))
	require.Equal(t, "sub2api", values.Get("referrer"))

	t.Setenv(EnvAllowUnsafeURLOverrides, "")
	_, err = ValidateOAuthEndpointURL("https://auth.example.test/oauth2/token")
	require.Error(t, err)
	_, err = ValidateBaseURL("http://127.0.0.1:8080/v1")
	require.Error(t, err)

	baseURL, err := ValidateBaseURL("https://api.x.ai")
	require.NoError(t, err)
	require.Equal(t, DefaultBaseURL, baseURL)
	cliURL, err := BuildChatCompletionsURL(DefaultCLIBaseURL + "/")
	require.NoError(t, err)
	require.Equal(t, DefaultCLIBaseURL+"/chat/completions", cliURL)
}

func TestRuntimeSanityRedactsUnsafeOverrideDetails(t *testing.T) {
	t.Setenv(EnvBaseURL, "http://127.0.0.1:8080/v1?access_token=secret")
	t.Setenv(EnvAuthorizeURL, "https://auth.example.test/oauth2/authorize")
	t.Setenv(EnvTokenURL, "https://auth.example.test/oauth2/token")
	t.Setenv(EnvRedirectURI, "not a url")
	t.Setenv(EnvClientID, "client-secret-like-value")
	t.Setenv(EnvAllowUnsafeURLOverrides, "")

	report := RuntimeSanity()
	require.False(t, report.BaseURL.Valid)
	require.False(t, report.BaseURL.IsDefault)
	require.NotContains(t, report.BaseURL.Value, "secret")
	require.NotContains(t, report.BaseURL.Error, "secret")
	require.False(t, report.OAuthAuthorizeURL.Valid)
	require.False(t, report.OAuthTokenURL.Valid)
	require.False(t, report.OAuthRedirectURI.Valid)
	require.NotContains(t, report.ProxyPolicy, "client-secret-like-value")
}

func TestParseQuotaHeadersAllowsOnlySafeQuotaHeaders(t *testing.T) {
	t.Parallel()

	headers := http.Header{}
	headers.Set("x-ratelimit-limit-requests", "100")
	headers.Set("x-ratelimit-remaining-requests", "25")
	headers.Set("x-ratelimit-reset-requests", "1893456000")
	headers.Set("x-ratelimit-limit-tokens", "1000000")
	headers.Set("x-ratelimit-remaining-tokens", "750000")
	headers.Set("retry-after", "60")
	headers.Set("xai-subscription-tier", "supergrok")
	headers.Set("xai-entitlement-status", "active")
	headers.Set("authorization", "should-not-be-copied")

	snapshot := ParseQuotaHeaders(headers, http.StatusTooManyRequests)
	require.NotNil(t, snapshot)
	require.Equal(t, http.StatusTooManyRequests, snapshot.StatusCode)
	require.True(t, snapshot.HeadersObserved)
	require.NotEmpty(t, snapshot.LastHeadersSeenAt)
	require.Equal(t, int64(100), *snapshot.Requests.Limit)
	require.Equal(t, int64(25), *snapshot.Requests.Remaining)
	require.Equal(t, int64(1893456000), *snapshot.Requests.ResetUnix)
	require.Equal(t, "2030-01-01T00:00:00Z", snapshot.Requests.ResetAt)
	require.Equal(t, int64(1000000), *snapshot.Tokens.Limit)
	require.Equal(t, int64(750000), *snapshot.Tokens.Remaining)
	require.Equal(t, 60, *snapshot.RetryAfterSeconds)
	require.Equal(t, "supergrok", snapshot.SubscriptionTier)
	require.Equal(t, "active", snapshot.EntitlementStatus)
	require.Contains(t, snapshot.Headers, "x-ratelimit-limit-requests")
	require.NotContains(t, snapshot.Headers, "authorization")
}

func TestObserveQuotaHeadersRecordsNoHeaderProbe(t *testing.T) {
	t.Parallel()

	require.Nil(t, ParseQuotaHeaders(http.Header{}, http.StatusOK))
	snapshot := ObserveQuotaHeaders(http.Header{}, http.StatusOK, "active_probe")
	require.NotNil(t, snapshot)
	require.False(t, snapshot.HeadersObserved)
	require.Equal(t, http.StatusOK, snapshot.StatusCode)
	require.Equal(t, "active_probe", snapshot.ObservationSource)
	require.NotEmpty(t, snapshot.LastProbeAt)
	require.Empty(t, snapshot.LastHeadersSeenAt)
	require.Empty(t, snapshot.Headers)
	require.Nil(t, snapshot.Requests)
	require.Nil(t, snapshot.Tokens)
}
