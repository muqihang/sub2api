package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type OpenAIGatewayCredentials struct {
	cfg       *config.Config
	protector *OpenAISecretProtector
}

func NewOpenAIGatewayCredentials(cfg *config.Config, protector *OpenAISecretProtector) *OpenAIGatewayCredentials {
	return &OpenAIGatewayCredentials{cfg: cfg, protector: protector}
}

func (c *OpenAIGatewayCredentials) OpenAIAPIKey(account *Account) (string, error) {
	if account == nil {
		return "", ErrAccountNilInput
	}
	if !account.IsOpenAI() || (account.Type != AccountTypeAPIKey && account.Type != AccountTypeUpstream) {
		return "", errors.New("account is not an openai api_key or upstream account")
	}
	return c.readCredential(account, "api_key")
}

func (c *OpenAIGatewayCredentials) OpenAIRefreshToken(account *Account) (string, error) {
	if account == nil {
		return "", ErrAccountNilInput
	}
	if !account.IsOpenAIOAuth() {
		return "", errors.New("account is not an openai oauth account")
	}
	return c.readCredential(account, "refresh_token")
}

func (c *OpenAIGatewayCredentials) OpenAIAccessToken(account *Account) (string, error) {
	if account == nil {
		return "", ErrAccountNilInput
	}
	if !account.IsOpenAI() {
		return "", errors.New("account is not an openai account")
	}
	return c.readCredential(account, "access_token")
}

func (c *OpenAIGatewayCredentials) OpenAIClientID(account *Account) (string, error) {
	if account == nil {
		return "", ErrAccountNilInput
	}
	if !account.IsOpenAI() {
		return "", errors.New("account is not an openai account")
	}
	return c.readCredential(account, "client_id")
}

func (c *OpenAIGatewayCredentials) readCredential(account *Account, key string) (string, error) {
	raw := strings.TrimSpace(account.GetCredential(key))
	if raw == "" {
		return "", fmt.Errorf("%s not found in credentials", key)
	}
	return c.resolveValue(raw, key)
}

func (c *OpenAIGatewayCredentials) resolveValue(raw string, key string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%s not found in credentials", key)
	}
	if strings.HasPrefix(raw, openAISecretProtectorPrefix) {
		protector, err := c.resolveProtector()
		if err != nil {
			return "", err
		}
		if protector == nil {
			return "", errOpenAIEncryptedCredentialUnavailable
		}
		return protector.DecryptValue(raw)
	}
	if shouldProtectOpenAICredentialKey(key) &&
		c.cfg != nil &&
		c.cfg.Gateway.OpenAICore.ProductionMode &&
		c.cfg.Gateway.OpenAICore.RequireEncryptedCredentials {
		return "", fmt.Errorf("plaintext openai credential %s is not allowed in production mode", key)
	}
	return raw, nil
}

func (c *OpenAIGatewayCredentials) resolveProtector() (*OpenAISecretProtector, error) {
	if c == nil {
		return nil, nil
	}
	if c.protector != nil {
		return c.protector, nil
	}
	return ProvideOpenAISecretProtector(c.cfg)
}

func (c *OpenAIGatewayCredentials) ProtectCredentials(input map[string]any) (map[string]any, error) {
	protector, err := c.resolveProtector()
	if err != nil {
		return nil, err
	}
	if protector == nil {
		return cloneJSONMap(input), nil
	}
	return protector.ProtectCredentials(input)
}

func (c *OpenAIGatewayCredentials) HasUnsafePlaintextCredentials(account *Account) bool {
	if c == nil || c.cfg == nil || account == nil {
		return false
	}
	if !c.cfg.Gateway.OpenAICore.ProductionMode || !c.cfg.Gateway.OpenAICore.RequireEncryptedCredentials {
		return false
	}
	for _, key := range openAIProtectedCredentialKeysForAccount(account) {
		raw := strings.TrimSpace(account.GetCredential(key))
		if raw == "" {
			continue
		}
		if !strings.HasPrefix(raw, openAISecretProtectorPrefix) {
			return true
		}
	}
	return false
}

func openAIProtectedCredentialKeysForAccount(account *Account) []string {
	if account == nil {
		return nil
	}
	switch {
	case account.IsOpenAIOAuth():
		return []string{"access_token", "refresh_token", "id_token"}
	case account.IsOpenAIApiKey():
		return []string{"api_key"}
	default:
		return nil
	}
}

func MergeProtectedOpenAICredentials(existing, updated map[string]any, accessor *OpenAIGatewayCredentials) (map[string]any, error) {
	merged := MergeCredentials(existing, cloneJSONMap(updated))
	if accessor == nil {
		return merged, nil
	}
	return accessor.ProtectCredentials(merged)
}
