package service

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

const openAIAgentIdentityAuthAPIBaseURL = "https://auth.openai.com/api/accounts"

type openAIAgentTaskRegistrationRequest struct {
	Timestamp string `json:"timestamp"`
	Signature string `json:"signature"`
}

type openAIAgentTaskRegistrationResponse struct {
	TaskID               string `json:"task_id"`
	TaskIDCamel          string `json:"taskId"`
	EncryptedTaskID      string `json:"encrypted_task_id"`
	EncryptedTaskIDCamel string `json:"encryptedTaskId"`
}

type openAIAgentAssertionEnvelope struct {
	AgentRuntimeID string `json:"agent_runtime_id"`
	Signature      string `json:"signature"`
	TaskID         string `json:"task_id"`
	Timestamp      string `json:"timestamp"`
}

type openAIAgentIdentityManager struct {
	mu             sync.Mutex
	taskIDs        map[string]string
	httpUpstream   openAIAgentIdentityUpstream
	credentials    *OpenAIGatewayCredentials
	authAPIBaseURL string
}

type openAIAgentIdentityUpstream interface {
	Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error)
}

type directOpenAIAgentIdentityUpstream struct {
	proxyURL string
}

func (u directOpenAIAgentIdentityUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	client := http.DefaultClient
	if strings.TrimSpace(u.proxyURL) != "" {
		parsedProxy, err := url.Parse(u.proxyURL)
		if err != nil {
			return nil, fmt.Errorf("parse openai agent identity proxy: %w", err)
		}
		client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(parsedProxy)}}
	}
	return client.Do(req)
}

func newOpenAIAgentIdentityManager(httpUpstream openAIAgentIdentityUpstream, credentials *OpenAIGatewayCredentials) *openAIAgentIdentityManager {
	return &openAIAgentIdentityManager{
		taskIDs:        make(map[string]string),
		httpUpstream:   httpUpstream,
		credentials:    credentials,
		authAPIBaseURL: openAIAgentIdentityAuthAPIBaseURL,
	}
}

func (s *OpenAIGatewayService) getOpenAIAgentIdentityManager() *openAIAgentIdentityManager {
	if s == nil {
		return nil
	}
	s.openAIAgentIdentityOnce.Do(func() {
		s.openAIAgentIdentity = newOpenAIAgentIdentityManager(
			s.httpUpstream,
			NewOpenAIGatewayCredentials(s.cfg, nil),
		)
	})
	return s.openAIAgentIdentity
}

func (s *OpenAIGatewayService) applyOpenAIRequestAuth(ctx context.Context, req *http.Request, account *Account, token string) error {
	if account != nil && account.IsOpenAIAgentIdentity() {
		manager := s.getOpenAIAgentIdentityManager()
		if manager == nil {
			return errors.New("openai agent identity manager is not configured")
		}
		return manager.Apply(ctx, req, account)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

func (a *Account) IsOpenAIAgentIdentity() bool {
	if a == nil || !a.IsOpenAIOAuth() {
		return false
	}
	authMode := strings.ToLower(strings.TrimSpace(a.GetCredential("auth_mode")))
	return authMode == "agentidentity" || authMode == "agent_identity"
}

func (m *openAIAgentIdentityManager) Apply(ctx context.Context, req *http.Request, account *Account) error {
	if m == nil || m.httpUpstream == nil {
		return errors.New("openai agent identity upstream is not configured")
	}
	if req == nil || account == nil || !account.IsOpenAIAgentIdentity() {
		return errors.New("openai agent identity credentials are required")
	}

	runtimeID := strings.TrimSpace(account.GetCredential("agent_runtime_id"))
	accountID := strings.TrimSpace(account.GetCredential("chatgpt_account_id"))
	privateKey, err := m.readPrivateKey(account)
	if err != nil {
		return err
	}
	if runtimeID == "" || accountID == "" {
		return errors.New("openai agent identity runtime and account ids are required")
	}

	taskID, err := m.taskID(ctx, account, runtimeID, privateKey)
	if err != nil {
		return err
	}
	timestamp := time.Now().UTC().Format(time.RFC3339)
	signature, err := signOpenAIAgentIdentity(privateKey, runtimeID+":"+taskID+":"+timestamp)
	if err != nil {
		return err
	}
	envelope, err := json.Marshal(openAIAgentAssertionEnvelope{
		AgentRuntimeID: runtimeID,
		Signature:      signature,
		TaskID:         taskID,
		Timestamp:      timestamp,
	})
	if err != nil {
		return fmt.Errorf("serialize openai agent assertion: %w", err)
	}
	req.Header.Set("Authorization", "AgentAssertion "+base64.RawURLEncoding.EncodeToString(envelope))
	req.Header.Set("ChatGPT-Account-ID", accountID)
	if strings.EqualFold(strings.TrimSpace(account.GetCredential("chatgpt_account_is_fedramp")), "true") {
		req.Header.Set("X-OpenAI-Fedramp", "true")
	}
	return nil
}

func (m *openAIAgentIdentityManager) readPrivateKey(account *Account) (string, error) {
	raw := strings.TrimSpace(account.GetCredential("agent_private_key"))
	if raw == "" {
		return "", errors.New("agent_private_key not found in credentials")
	}
	if m.credentials == nil {
		return raw, nil
	}
	return m.credentials.resolveValue(raw, "agent_private_key")
}

func (m *openAIAgentIdentityManager) taskID(ctx context.Context, account *Account, runtimeID, privateKey string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if taskID := strings.TrimSpace(m.taskIDs[runtimeID]); taskID != "" {
		return taskID, nil
	}
	taskID, err := m.registerTask(ctx, account, runtimeID, privateKey)
	if err != nil {
		return "", err
	}
	m.taskIDs[runtimeID] = taskID
	return taskID, nil
}

func (m *openAIAgentIdentityManager) registerTask(ctx context.Context, account *Account, runtimeID, privateKey string) (string, error) {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	signature, err := signOpenAIAgentIdentity(privateKey, runtimeID+":"+timestamp)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(openAIAgentTaskRegistrationRequest{Timestamp: timestamp, Signature: signature})
	if err != nil {
		return "", fmt.Errorf("serialize openai agent task registration: %w", err)
	}
	url := strings.TrimRight(m.authAPIBaseURL, "/") + "/v1/agent/" + runtimeID + "/task/register"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	response, err := m.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return "", fmt.Errorf("register openai agent task: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read openai agent task registration: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("openai agent task registration returned status %d", response.StatusCode)
	}
	var result openAIAgentTaskRegistrationResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return "", fmt.Errorf("decode openai agent task registration: %w", err)
	}
	if taskID := strings.TrimSpace(firstNonEmptyAgentIdentity(result.TaskID, result.TaskIDCamel)); taskID != "" {
		return taskID, nil
	}
	encryptedTaskID := strings.TrimSpace(firstNonEmptyAgentIdentity(result.EncryptedTaskID, result.EncryptedTaskIDCamel))
	if encryptedTaskID == "" {
		return "", errors.New("openai agent task registration omitted task id")
	}
	return decryptOpenAIAgentTaskID(privateKey, encryptedTaskID)
}

func signOpenAIAgentIdentity(privateKeyPKCS8Base64, payload string) (string, error) {
	privateKey, err := parseOpenAIAgentIdentityPrivateKey(privateKeyPKCS8Base64)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(payload))), nil
}

func parseOpenAIAgentIdentityPrivateKey(encoded string) (ed25519.PrivateKey, error) {
	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, errors.New("stored openai agent identity key is not valid base64")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, errors.New("stored openai agent identity key is not valid PKCS#8")
	}
	privateKey, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("stored openai agent identity key is not Ed25519")
	}
	return privateKey, nil
}

func decryptOpenAIAgentTaskID(privateKeyPKCS8Base64, encrypted string) (string, error) {
	privateKey, err := parseOpenAIAgentIdentityPrivateKey(privateKeyPKCS8Base64)
	if err != nil {
		return "", err
	}
	digest := sha512.Sum512(privateKey.Seed())
	var curvePrivate [32]byte
	copy(curvePrivate[:], digest[:32])
	curvePrivate[0] &= 248
	curvePrivate[31] &= 127
	curvePrivate[31] |= 64
	publicBytes, err := curve25519.X25519(curvePrivate[:], curve25519.Basepoint)
	if err != nil {
		return "", fmt.Errorf("derive openai agent identity public key: %w", err)
	}
	var curvePublic [32]byte
	copy(curvePublic[:], publicBytes)
	ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", errors.New("encrypted openai agent task id is not valid base64")
	}
	plaintext, ok := box.OpenAnonymous(nil, ciphertext, &curvePublic, &curvePrivate)
	if !ok {
		return "", errors.New("failed to decrypt openai agent task id")
	}
	return string(plaintext), nil
}

func firstNonEmptyAgentIdentity(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
