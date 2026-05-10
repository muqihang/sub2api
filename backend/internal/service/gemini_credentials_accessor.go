package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type GeminiCredentialsAccessor struct {
	cfg       *config.Config
	protector *GeminiSecretProtector
}

func NewGeminiCredentialsAccessor(cfg *config.Config, protector *GeminiSecretProtector) *GeminiCredentialsAccessor {
	return &GeminiCredentialsAccessor{cfg: cfg, protector: protector}
}

func (c *GeminiCredentialsAccessor) GeminiAccessToken(account *Account) (string, error) {
	if account == nil {
		return "", ErrAccountNilInput
	}
	if account.Platform != PlatformGemini || account.Type != AccountTypeOAuth {
		return "", errors.New("account is not a gemini oauth account")
	}
	return c.readCredential(account, "access_token")
}

func (c *GeminiCredentialsAccessor) GeminiRefreshToken(account *Account) (string, error) {
	if account == nil {
		return "", ErrAccountNilInput
	}
	if account.Platform != PlatformGemini || account.Type != AccountTypeOAuth {
		return "", errors.New("account is not a gemini oauth account")
	}
	return c.readCredential(account, "refresh_token")
}

func (c *GeminiCredentialsAccessor) GeminiAPIKey(account *Account) (string, error) {
	if account == nil {
		return "", ErrAccountNilInput
	}
	if account.Platform != PlatformGemini || account.Type != AccountTypeAPIKey {
		return "", errors.New("account is not a gemini api_key account")
	}
	return c.readCredential(account, "api_key")
}

func (c *GeminiCredentialsAccessor) GeminiProjectID(account *Account) (string, error) {
	if account == nil {
		return "", ErrAccountNilInput
	}
	if account.Platform != PlatformGemini {
		return "", errors.New("account is not a gemini account")
	}
	if raw := strings.TrimSpace(account.GetCredential("project_id")); raw != "" {
		return raw, nil
	}
	if account.Type == AccountTypeServiceAccount {
		key, err := parseVertexServiceAccountKeyWithAccessor(account, c)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(key.ProjectID), nil
	}
	return "", fmt.Errorf("project_id not found in credentials")
}

func (c *GeminiCredentialsAccessor) VertexServiceAccountJSON(account *Account) (string, error) {
	if account == nil {
		return "", ErrAccountNilInput
	}
	if account.Platform != PlatformGemini || account.Type != AccountTypeServiceAccount {
		return "", errors.New("account is not a vertex service account")
	}
	if account.Credentials == nil {
		return "", errors.New("service account credentials not configured")
	}
	for _, key := range []string{"service_account_json", "service_account"} {
		value, ok := account.Credentials[key]
		if !ok || value == nil {
			continue
		}
		return c.resolveStructuredCredential(value, key)
	}
	return "", errors.New("service_account_json not found in credentials")
}

func (c *GeminiCredentialsAccessor) readCredential(account *Account, key string) (string, error) {
	raw := strings.TrimSpace(account.GetCredential(key))
	if raw == "" {
		return "", fmt.Errorf("%s not found in credentials", key)
	}
	return c.resolveValue(raw, key)
}

func (c *GeminiCredentialsAccessor) resolveStructuredCredential(value any, key string) (string, error) {
	switch raw := value.(type) {
	case string:
		if strings.TrimSpace(raw) == "" {
			return "", fmt.Errorf("%s not found in credentials", key)
		}
		return c.resolveValue(raw, key)
	case []byte:
		if strings.TrimSpace(string(raw)) == "" {
			return "", fmt.Errorf("%s not found in credentials", key)
		}
		return c.resolveValue(string(raw), key)
	case map[string]any:
		if c != nil && c.cfg != nil && c.cfg.Gemini.ProductionMode && shouldProtectGeminiCredentialKey(key) {
			return "", fmt.Errorf("plaintext gemini credential %s is not allowed in production mode", key)
		}
		b, err := json.Marshal(raw)
		if err != nil {
			return "", fmt.Errorf("marshal %s: %w", key, err)
		}
		return string(b), nil
	default:
		return "", fmt.Errorf("%s not found in credentials", key)
	}
}

func (c *GeminiCredentialsAccessor) resolveValue(raw string, key string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%s not found in credentials", key)
	}
	if strings.HasPrefix(raw, geminiSecretProtectorPrefix) {
		protector, err := c.resolveProtector()
		if err != nil {
			return "", err
		}
		if protector == nil {
			return "", errGeminiEncryptedCredentialUnavailable
		}
		return protector.DecryptValue(raw)
	}
	if shouldProtectGeminiCredentialKey(key) &&
		c != nil &&
		c.cfg != nil &&
		c.cfg.Gemini.ProductionMode {
		return "", fmt.Errorf("plaintext gemini credential %s is not allowed in production mode", key)
	}
	return raw, nil
}

func (c *GeminiCredentialsAccessor) resolveProtector() (*GeminiSecretProtector, error) {
	if c == nil {
		return nil, nil
	}
	if c.protector != nil {
		return c.protector, nil
	}
	return ProvideGeminiSecretProtector(c.cfg)
}

func (c *GeminiCredentialsAccessor) ProtectCredentials(input map[string]any) (map[string]any, error) {
	protector, err := c.resolveProtector()
	if err != nil {
		return nil, err
	}
	if protector == nil {
		return cloneJSONMap(input), nil
	}
	return protector.ProtectCredentials(input)
}

func (c *GeminiCredentialsAccessor) HasUnsafePlaintextCredentials(account *Account) bool {
	if c == nil || c.cfg == nil || account == nil || !c.cfg.Gemini.ProductionMode {
		return false
	}
	for _, key := range geminiProtectedCredentialKeysForAccount(account) {
		value := account.Credentials[key]
		raw, ok, err := serializeGeminiCredentialValue(value)
		if err != nil || !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(raw), geminiSecretProtectorPrefix) {
			return true
		}
	}
	return false
}

func geminiProtectedCredentialKeysForAccount(account *Account) []string {
	if account == nil {
		return nil
	}
	switch account.Type {
	case AccountTypeOAuth:
		return []string{"access_token", "refresh_token"}
	case AccountTypeAPIKey:
		return []string{"api_key"}
	case AccountTypeServiceAccount:
		return []string{"service_account_json", "service_account"}
	default:
		return nil
	}
}

func MergeProtectedGeminiCredentials(existing, updated map[string]any, accessor *GeminiCredentialsAccessor) (map[string]any, error) {
	merged := MergeCredentials(existing, cloneJSONMap(updated))
	if accessor == nil {
		return merged, nil
	}
	return accessor.ProtectCredentials(merged)
}
