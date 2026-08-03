package repository

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type ClaudeOAuthServiceSuite struct {
	suite.Suite
	client *claudeOAuthService
}

// requestCapture holds captured request data for assertions in the main goroutine.
type requestCapture struct {
	path        string
	method      string
	headers     http.Header
	cookies     []*http.Cookie
	body        []byte
	bodyJSON    map[string]any
	bodyForm    url.Values
	contentType string
}

func newTestReqClient(rt http.RoundTripper) *req.Client {
	c := req.C()
	c.GetClient().Transport = rt
	return c
}

func (s *ClaudeOAuthServiceSuite) TestGetOrganizationUUID() {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErr    bool
		errContain string
		wantUUID   string
		validate   func(captured requestCapture)
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[{"uuid":"org-1"}]`))
			},
			wantUUID: "org-1",
			validate: func(captured requestCapture) {
				require.Equal(s.T(), "/api/organizations", captured.path, "unexpected path")
				require.Len(s.T(), captured.cookies, 1, "expected 1 cookie")
				require.Equal(s.T(), "sessionKey", captured.cookies[0].Name)
				require.Equal(s.T(), "sess", captured.cookies[0].Value)
				require.Equal(s.T(), "application/json", captured.headers.Get("Accept"))
				require.Contains(s.T(), captured.headers.Get("User-Agent"), "Mozilla/5.0")
				require.Equal(s.T(), "https://claude.ai/new", captured.headers.Get("Referer"))
				require.Equal(s.T(), "https://claude.ai", captured.headers.Get("Origin"))
			},
		},
		{
			name: "non_200_returns_error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("unauthorized"))
			},
			wantErr:    true,
			errContain: "401",
		},
		{
			name: "invalid_json_returns_error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("not-json"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			var captured requestCapture

			rt := newInProcessTransport(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured.path = r.URL.Path
				captured.headers = r.Header.Clone()
				captured.cookies = r.Cookies()
				tt.handler(w, r)
			}), nil)

			client, ok := NewClaudeOAuthClient().(*claudeOAuthService)
			require.True(s.T(), ok, "type assertion failed")
			s.client = client
			s.client.baseURL = "http://in-process"
			s.client.clientFactory = func(string) (*req.Client, error) { return newTestReqClient(rt), nil }

			got, err := s.client.GetOrganizationUUID(context.Background(), "sess", "")

			if tt.wantErr {
				require.Error(s.T(), err)
				if tt.errContain != "" {
					require.ErrorContains(s.T(), err, tt.errContain)
				}
				return
			}

			require.NoError(s.T(), err)
			require.Equal(s.T(), tt.wantUUID, got)
			if tt.validate != nil {
				tt.validate(captured)
			}
		})
	}
}

func (s *ClaudeOAuthServiceSuite) TestGetAuthorizationCode() {
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		wantErr  bool
		wantCode string
		validate func(captured requestCapture)
	}{
		{
			name: "parses_redirect_uri",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{
					"redirect_uri": oauth.RedirectURI + "?code=AUTH&state=STATE",
				})
			},
			wantCode: "AUTH#STATE",
			validate: func(captured requestCapture) {
				require.True(s.T(), strings.HasPrefix(captured.path, "/v1/oauth/") && strings.HasSuffix(captured.path, "/authorize"), "unexpected path: %s", captured.path)
				require.Equal(s.T(), http.MethodPost, captured.method, "expected POST")
				require.Len(s.T(), captured.cookies, 1, "expected 1 cookie")
				require.Equal(s.T(), "sess", captured.cookies[0].Value)
				require.Equal(s.T(), "org-1", captured.bodyJSON["organization_uuid"])
				require.Equal(s.T(), oauth.ClientID, captured.bodyJSON["client_id"])
				require.Equal(s.T(), oauth.RedirectURI, captured.bodyJSON["redirect_uri"])
				require.Equal(s.T(), "st", captured.bodyJSON["state"])
			},
		},
		{
			name: "missing_code_returns_error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{
					"redirect_uri": oauth.RedirectURI + "?state=STATE", // no code
				})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			var captured requestCapture

			rt := newInProcessTransport(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured.path = r.URL.Path
				captured.method = r.Method
				captured.cookies = r.Cookies()
				captured.body, _ = io.ReadAll(r.Body)
				_ = json.Unmarshal(captured.body, &captured.bodyJSON)
				captured.bodyForm, _ = url.ParseQuery(string(captured.body))
				tt.handler(w, r)
			}), nil)

			client, ok := NewClaudeOAuthClient().(*claudeOAuthService)
			require.True(s.T(), ok, "type assertion failed")
			s.client = client
			s.client.baseURL = "http://in-process"
			s.client.clientFactory = func(string) (*req.Client, error) { return newTestReqClient(rt), nil }

			code, err := s.client.GetAuthorizationCode(context.Background(), "sess", "org-1", oauth.ScopeInference, "cc", "st", "")

			if tt.wantErr {
				require.Error(s.T(), err)
				return
			}

			require.NoError(s.T(), err)
			require.Equal(s.T(), tt.wantCode, code)
			if tt.validate != nil {
				tt.validate(captured)
			}
		})
	}
}

func (s *ClaudeOAuthServiceSuite) TestExchangeCodeForToken() {
	tests := []struct {
		name         string
		handler      http.HandlerFunc
		code         string
		isSetupToken bool
		wantErr      bool
		wantResp     *oauth.TokenResponse
		validate     func(captured requestCapture)
	}{
		{
			name: "sends_state_when_embedded",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(oauth.TokenResponse{
					AccessToken:  "at",
					TokenType:    "bearer",
					ExpiresIn:    3600,
					RefreshToken: "rt",
					Scope:        "s",
				})
			},
			code:         "AUTH#STATE2",
			isSetupToken: false,
			wantResp: &oauth.TokenResponse{
				AccessToken:  "at",
				RefreshToken: "rt",
			},
			validate: func(captured requestCapture) {
				require.Equal(s.T(), http.MethodPost, captured.method, "expected POST")
				require.True(s.T(), strings.HasPrefix(captured.contentType, "application/x-www-form-urlencoded"), "unexpected content-type")
				require.Equal(s.T(), "AUTH", captured.bodyForm.Get("code"))
				require.Equal(s.T(), "STATE2", captured.bodyForm.Get("state"))
				require.Equal(s.T(), oauth.ClientID, captured.bodyForm.Get("client_id"))
				require.Equal(s.T(), oauth.RedirectURI, captured.bodyForm.Get("redirect_uri"))
				require.Equal(s.T(), "ver", captured.bodyForm.Get("code_verifier"))
				// Regular OAuth should not include expires_in
				require.Empty(s.T(), captured.bodyForm.Get("expires_in"), "regular OAuth should not include expires_in")
			},
		},
		{
			name: "setup_token_omits_expires_in",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(oauth.TokenResponse{
					AccessToken: "at",
					TokenType:   "bearer",
					ExpiresIn:   31536000,
				})
			},
			code:         "AUTH",
			isSetupToken: true,
			wantResp: &oauth.TokenResponse{
				AccessToken: "at",
			},
			validate: func(captured requestCapture) {
				require.Empty(s.T(), captured.bodyForm.Get("expires_in"), "setup token should not include expires_in")
			},
		},
		{
			name: "non_200_returns_error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("bad request"))
			},
			code:         "AUTH",
			isSetupToken: false,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			var captured requestCapture

			rt := newInProcessTransport(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured.method = r.Method
				captured.contentType = r.Header.Get("Content-Type")
				captured.body, _ = io.ReadAll(r.Body)
				_ = json.Unmarshal(captured.body, &captured.bodyJSON)
				captured.bodyForm, _ = url.ParseQuery(string(captured.body))
				tt.handler(w, r)
			}), nil)

			client, ok := NewClaudeOAuthClient().(*claudeOAuthService)
			require.True(s.T(), ok, "type assertion failed")
			s.client = client
			s.client.tokenURL = "http://in-process/token"
			s.client.clientFactory = func(string) (*req.Client, error) { return newTestReqClient(rt), nil }

			resp, err := s.client.ExchangeCodeForToken(context.Background(), tt.code, "ver", "", "", tt.isSetupToken)

			if tt.wantErr {
				require.Error(s.T(), err)
				return
			}

			require.NoError(s.T(), err)
			require.Equal(s.T(), tt.wantResp.AccessToken, resp.AccessToken)
			require.Equal(s.T(), tt.wantResp.RefreshToken, resp.RefreshToken)
			if tt.validate != nil {
				tt.validate(captured)
			}
		})
	}
}

func (s *ClaudeOAuthServiceSuite) TestRefreshToken() {
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		wantErr  bool
		wantResp *oauth.TokenResponse
		validate func(captured requestCapture)
	}{
		{
			name: "sends_form_format",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(oauth.TokenResponse{
					AccessToken:  "new_access_token",
					TokenType:    "bearer",
					ExpiresIn:    28800,
					RefreshToken: "new_refresh_token",
					Scope:        "user:profile user:inference",
				})
			},
			wantResp: &oauth.TokenResponse{
				AccessToken:  "new_access_token",
				RefreshToken: "new_refresh_token",
			},
			validate: func(captured requestCapture) {
				require.Equal(s.T(), http.MethodPost, captured.method, "expected POST")
				require.True(s.T(), strings.HasPrefix(captured.contentType, "application/x-www-form-urlencoded"),
					"expected form content-type, got: %s", captured.contentType)
				require.Equal(s.T(), "refresh_token", captured.bodyForm.Get("grant_type"))
				require.Equal(s.T(), "rt", captured.bodyForm.Get("refresh_token"))
				require.Equal(s.T(), oauth.ClientID, captured.bodyForm.Get("client_id"))
			},
		},
		{
			name: "returns_new_refresh_token",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(oauth.TokenResponse{
					AccessToken:  "at",
					TokenType:    "bearer",
					ExpiresIn:    28800,
					RefreshToken: "rotated_rt", // Anthropic rotates refresh tokens
				})
			},
			wantResp: &oauth.TokenResponse{
				AccessToken:  "at",
				RefreshToken: "rotated_rt",
			},
		},
		{
			name: "non_200_returns_error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			var captured requestCapture

			rt := newInProcessTransport(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured.method = r.Method
				captured.contentType = r.Header.Get("Content-Type")
				captured.body, _ = io.ReadAll(r.Body)
				_ = json.Unmarshal(captured.body, &captured.bodyJSON)
				captured.bodyForm, _ = url.ParseQuery(string(captured.body))
				tt.handler(w, r)
			}), nil)

			client, ok := NewClaudeOAuthClient().(*claudeOAuthService)
			require.True(s.T(), ok, "type assertion failed")
			s.client = client
			s.client.tokenURL = "http://in-process/token"
			s.client.clientFactory = func(string) (*req.Client, error) { return newTestReqClient(rt), nil }

			resp, err := s.client.RefreshToken(context.Background(), "rt", "")

			if tt.wantErr {
				require.Error(s.T(), err)
				return
			}

			require.NoError(s.T(), err)
			require.Equal(s.T(), tt.wantResp.AccessToken, resp.AccessToken)
			require.Equal(s.T(), tt.wantResp.RefreshToken, resp.RefreshToken)
			if tt.validate != nil {
				tt.validate(captured)
			}
		})
	}
}

func (s *ClaudeOAuthServiceSuite) TestTokenExchangeProxyFailureDoesNotDirectConnect() {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(oauth.TokenResponse{AccessToken: "at", ExpiresIn: 3600})
	}))
	defer server.Close()

	client, ok := NewClaudeOAuthClient().(*claudeOAuthService)
	require.True(s.T(), ok, "type assertion failed")
	client.tokenURL = server.URL

	_, err := client.ExchangeCodeForToken(context.Background(), "AUTH", "ver", "", "http://127.0.0.1:1", false)
	require.Error(s.T(), err)
	require.Zero(s.T(), hits, "token exchange must not bypass unavailable proxy and direct-connect")
}

func (s *ClaudeOAuthServiceSuite) TestRefreshTokenProxyFailureDoesNotDirectConnect() {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(oauth.TokenResponse{AccessToken: "at", ExpiresIn: 3600})
	}))
	defer server.Close()

	client, ok := NewClaudeOAuthClient().(*claudeOAuthService)
	require.True(s.T(), ok, "type assertion failed")
	client.tokenURL = server.URL

	_, err := client.RefreshToken(context.Background(), "rt", "http://127.0.0.1:1")
	require.Error(s.T(), err)
	require.Zero(s.T(), hits, "refresh must not bypass unavailable proxy and direct-connect")
}

func (s *ClaudeOAuthServiceSuite) TestOAuthErrorBodiesAreRedacted() {
	secretBody := `{"error":"invalid_grant","code":"auth-secret","state":"state-secret","access_token":"access-secret","refresh_token":"refresh-secret","redirect_uri":"https://platform.claude.com/oauth/code/callback?code=query-code-secret&state=query-state-secret"}`

	s.Run("get_organization_uuid_redacts_error_body", func() {
		rt := newInProcessTransport(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(secretBody))
		}), nil)

		client, ok := NewClaudeOAuthClient().(*claudeOAuthService)
		require.True(s.T(), ok, "type assertion failed")
		client.baseURL = "http://in-process"
		client.clientFactory = func(string) (*req.Client, error) { return newTestReqClient(rt), nil }

		_, err := client.GetOrganizationUUID(context.Background(), "sess", "")
		require.Error(s.T(), err)
		assertNoOAuthSecretLeak(s.T(), err.Error())
	})

	s.Run("get_authorization_code_redacts_error_body", func() {
		rt := newInProcessTransport(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(secretBody))
		}), nil)

		client, ok := NewClaudeOAuthClient().(*claudeOAuthService)
		require.True(s.T(), ok, "type assertion failed")
		client.baseURL = "http://in-process"
		client.clientFactory = func(string) (*req.Client, error) { return newTestReqClient(rt), nil }

		_, err := client.GetAuthorizationCode(context.Background(), "sess", "org-1", oauth.ScopeInference, "cc", "st", "")
		require.Error(s.T(), err)
		assertNoOAuthSecretLeak(s.T(), err.Error())
	})

	s.Run("exchange_code_redacts_error_body", func() {
		rt := newInProcessTransport(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(secretBody))
		}), nil)

		client, ok := NewClaudeOAuthClient().(*claudeOAuthService)
		require.True(s.T(), ok, "type assertion failed")
		client.tokenURL = "http://in-process/token"
		client.clientFactory = func(string) (*req.Client, error) { return newTestReqClient(rt), nil }

		_, err := client.ExchangeCodeForToken(context.Background(), "AUTH", "verifier-secret", "", "", false)
		require.Error(s.T(), err)
		assertNoOAuthSecretLeak(s.T(), err.Error())
	})

	s.Run("refresh_token_redacts_error_body", func() {
		rt := newInProcessTransport(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(secretBody))
		}), nil)

		client, ok := NewClaudeOAuthClient().(*claudeOAuthService)
		require.True(s.T(), ok, "type assertion failed")
		client.tokenURL = "http://in-process/token"
		client.clientFactory = func(string) (*req.Client, error) { return newTestReqClient(rt), nil }

		_, err := client.RefreshToken(context.Background(), "refresh-secret", "")
		require.Error(s.T(), err)
		assertNoOAuthSecretLeak(s.T(), err.Error())
	})
}

func assertNoOAuthSecretLeak(t *testing.T, text string) {
	t.Helper()
	for _, secret := range []string{
		"auth-secret",
		"state-secret",
		"access-secret",
		"refresh-secret",
		"query-code-secret",
		"query-state-secret",
		"verifier-secret",
	} {
		require.NotContains(t, text, secret)
	}
	require.Contains(t, text, "***")
}

func TestClaudeOAuthRequestErrorRedactsAuthorizeURL(t *testing.T) {
	orgUUID := "11111111-2222-4333-8444-555555555555"
	raw := `Post "https://claude.ai/v1/oauth/` + orgUUID + `/authorize": dial tcp: connect: connection refused`

	redacted := redactClaudeOAuthLogText(raw)

	require.NotContains(t, redacted, orgUUID)
	require.NotContains(t, redacted, "/"+orgUUID+"/")
	require.Contains(t, redacted, "/v1/oauth/<redacted>/authorize")
}

func TestClaudeOAuthGetAuthorizationCodeRequestErrorRedactsAuthorizeURL(t *testing.T) {
	orgUUID := "22222222-3333-4444-8555-666666666666"
	client, ok := NewClaudeOAuthClient().(*claudeOAuthService)
	require.True(t, ok, "type assertion failed")
	client.baseURL = "https://claude.ai"
	client.clientFactory = func(string) (*req.Client, error) {
		return newTestReqClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return nil, &url.Error{Op: "Post", URL: r.URL.String(), Err: io.ErrUnexpectedEOF}
		})), nil
	}

	_, err := client.GetAuthorizationCode(context.Background(), "sess", orgUUID, oauth.ScopeInference, "cc", "st", "")

	require.Error(t, err)
	require.NotContains(t, err.Error(), orgUUID)
	require.NotContains(t, err.Error(), "/"+orgUUID+"/")
	require.Contains(t, err.Error(), "/v1/oauth/<redacted>/authorize")
}

func TestClaudeOAuthServiceSuite(t *testing.T) {
	suite.Run(t, new(ClaudeOAuthServiceSuite))
}

func TestClaudeOAuthLogRedactionRedactsOrganizationIdentityFields(t *testing.T) {
	payload := map[string]any{
		"organization_uuid": "11111111-2222-4333-8444-555555555555",
		"org_uuid":          "66666666-7777-4888-9999-aaaaaaaaaaaa",
		"account_uuid":      "bbbbbbbb-cccc-4ddd-eeee-ffffffffffff",
		"email":             "person@example.com",
		"name":              "Sensitive Org Name",
		"nested": map[string]any{
			"organization_uuid": "99999999-8888-4777-8666-555555555555",
			"email":             "nested@example.com",
		},
	}

	redacted := string(mustJSONBytes(t, redactClaudeOAuthLogMap(payload)))
	for _, secret := range []string{
		"11111111-2222-4333-8444-555555555555",
		"66666666-7777-4888-9999-aaaaaaaaaaaa",
		"bbbbbbbb-cccc-4ddd-eeee-ffffffffffff",
		"99999999-8888-4777-8666-555555555555",
		"person@example.com",
		"nested@example.com",
		"Sensitive Org Name",
	} {
		require.NotContains(t, redacted, secret)
	}
	require.Contains(t, redacted, "***")
}

func mustJSONBytes(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func TestClaudeOAuthAuthorizeURLLogStringRedactsOrganizationUUID(t *testing.T) {
	orgUUID := "11111111-2222-4333-8444-555555555555"
	logged := safeClaudeOAuthAuthorizeURLForLog("https://claude.ai", orgUUID)
	require.NotContains(t, logged, orgUUID)
	require.NotContains(t, logged, "/"+orgUUID+"/")
	require.Contains(t, logged, "<redacted>")
	require.Contains(t, logged, "/v1/oauth/")
}
