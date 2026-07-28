package service

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type agentIdentityTestUpstream struct{}

func (agentIdentityTestUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return http.DefaultClient.Do(req)
}

func (u agentIdentityTestUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

func TestOpenAIAgentIdentityManagerApplyRegistersAndSigns(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	privateB64 := base64.StdEncoding.EncodeToString(privateDER)

	var registrations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		registrations.Add(1)
		require.Equal(t, "/v1/agent/agent-test/task/register", r.URL.Path)
		var payload openAIAgentTaskRegistrationRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		signature, err := base64.StdEncoding.DecodeString(payload.Signature)
		require.NoError(t, err)
		require.True(t, ed25519.Verify(publicKey, []byte("agent-test:"+payload.Timestamp), signature))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"task_id":"task-test"}`)
	}))
	defer server.Close()

	manager := newOpenAIAgentIdentityManager(agentIdentityTestUpstream{}, nil)
	manager.authAPIBaseURL = server.URL
	account := &Account{
		ID:          215,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 10,
		Credentials: map[string]any{
			"auth_mode":                  "agentIdentity",
			"agent_runtime_id":           "agent-test",
			"agent_private_key":          privateB64,
			"chatgpt_account_id":         "workspace-test",
			"chatgpt_account_is_fedramp": false,
		},
	}

	for range 2 {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{}`))
		require.NoError(t, err)
		require.NoError(t, manager.Apply(context.Background(), req, account))
		require.Equal(t, "workspace-test", req.Header.Get("ChatGPT-Account-ID"))

		assertion := strings.TrimPrefix(req.Header.Get("Authorization"), "AgentAssertion ")
		require.NotEmpty(t, assertion)
		encoded, err := base64.RawURLEncoding.DecodeString(assertion)
		require.NoError(t, err)
		var envelope openAIAgentAssertionEnvelope
		require.NoError(t, json.Unmarshal(encoded, &envelope))
		require.Equal(t, "agent-test", envelope.AgentRuntimeID)
		require.Equal(t, "task-test", envelope.TaskID)
		signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
		require.NoError(t, err)
		require.True(t, ed25519.Verify(publicKey, []byte("agent-test:task-test:"+envelope.Timestamp), signature))
	}
	require.Equal(t, int32(1), registrations.Load())
}

func TestOpenAIAgentIdentityUsesHeaderAuthAndHTTPTransport(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"auth_mode": "agentIdentity",
		},
	}
	svc := &OpenAIGatewayService{}

	token, tokenType, err := svc.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Empty(t, token)
	require.Equal(t, "agent_identity", tokenType)

	decision := NewOpenAIWSProtocolResolver(nil).Resolve(account)
	require.Equal(t, OpenAIUpstreamTransportHTTPSSE, decision.Transport)
	require.Equal(t, "agent_identity_http_only", decision.Reason)
}

func TestOpenAIAgentIdentityRealResponses(t *testing.T) {
	runtimeID := strings.TrimSpace(os.Getenv("SUB2API_TEST_AGENT_RUNTIME_ID"))
	privateKey := strings.TrimSpace(os.Getenv("SUB2API_TEST_AGENT_PRIVATE_KEY"))
	accountID := strings.TrimSpace(os.Getenv("SUB2API_TEST_AGENT_ACCOUNT_ID"))
	userID := strings.TrimSpace(os.Getenv("SUB2API_TEST_AGENT_USER_ID"))
	if runtimeID == "" || privateKey == "" || accountID == "" || userID == "" {
		t.Skip("real Agent Identity credentials are not configured")
	}

	account := &Account{
		ID:          1,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"auth_mode":          "agentIdentity",
			"agent_runtime_id":   runtimeID,
			"agent_private_key":  privateKey,
			"chatgpt_account_id": accountID,
			"chatgpt_user_id":    userID,
		},
	}
	manager := newOpenAIAgentIdentityManager(directOpenAIAgentIdentityUpstream{}, nil)
	body := `{"model":"gpt-5.6-sol","input":[{"role":"user","content":[{"type":"input_text","text":"Respond with OK."}]}],"stream":true,"store":false}`
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, chatgptCodexURL, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "codex_cli_rs/agent-identity-validation")
	req.Header.Set("originator", "codex_cli_rs")
	require.NoError(t, manager.Apply(context.Background(), req, account))

	response, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode, "upstream response: %s", responseBody)

	compactBody, err := json.Marshal(map[string]any{
		"model": "gpt-5.6-sol",
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{map[string]any{
					"type": "input_text",
					"text": strings.Repeat("0123456789abcdef ", (1<<20)/17+1)[:1<<20],
				}},
			},
			map[string]any{"type": "compaction_trigger"},
		},
	})
	require.NoError(t, err)
	compactReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, chatgptCodexURL+"/compact", bytes.NewReader(compactBody))
	require.NoError(t, err)
	compactReq.Header.Set("Content-Type", "application/json")
	compactReq.Header.Set("Accept", "application/json")
	compactReq.Header.Set("User-Agent", "codex_cli_rs/agent-identity-validation")
	compactReq.Header.Set("originator", "codex_cli_rs")
	require.NoError(t, manager.Apply(context.Background(), compactReq, account))
	compactResponse, err := http.DefaultClient.Do(compactReq)
	require.NoError(t, err)
	defer compactResponse.Body.Close()
	compactResponseBody, err := io.ReadAll(io.LimitReader(compactResponse.Body, 4<<20))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, compactResponse.StatusCode, "compact upstream response: %s", compactResponseBody)
	require.Contains(t, string(compactResponseBody), "compaction", "compact upstream response: %s", compactResponseBody)

	searchBody := `{"id":"agent-identity-validation","model":"gpt-5.6-sol","commands":{}}`
	searchReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, chatGPTNativeSearchURL, strings.NewReader(searchBody))
	require.NoError(t, err)
	searchReq.Header.Set("Content-Type", "application/json")
	searchReq.Header.Set("Accept", "application/json")
	searchReq.Header.Set("User-Agent", "codex_cli_rs/agent-identity-validation")
	searchReq.Header.Set("originator", "codex_cli_rs")
	require.NoError(t, manager.Apply(context.Background(), searchReq, account))
	searchResponse, err := http.DefaultClient.Do(searchReq)
	require.NoError(t, err)
	defer searchResponse.Body.Close()
	searchResponseBody, err := io.ReadAll(io.LimitReader(searchResponse.Body, 1<<20))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, searchResponse.StatusCode, "search upstream response: %s", searchResponseBody)
	var searchPayload map[string]any
	require.NoError(t, json.Unmarshal(searchResponseBody, &searchPayload))
	require.NotEmpty(t, strings.TrimSpace(stringValue(searchPayload["output"])))
}
