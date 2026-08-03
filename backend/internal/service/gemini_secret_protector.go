package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const geminiSecretProtectorPrefix = "genc:v1:"

var errGeminiEncryptedCredentialUnavailable = errors.New("encrypted gemini credentials require credential protection to be configured")

var geminiProtectedCredentialFields = map[string]struct{}{
	"access_token":         {},
	"refresh_token":        {},
	"api_key":              {},
	"service_account_json": {},
	"service_account":      {},
}

type GeminiSecretProtector struct {
	key []byte
}

func ProvideGeminiSecretProtector(cfg *config.Config) (*GeminiSecretProtector, error) {
	if cfg == nil {
		return nil, nil
	}
	keyHex := strings.TrimSpace(cfg.Gateway.OpenAICore.CredentialEncryptionKey)
	if keyHex == "" {
		if cfg.Gemini.ProductionMode {
			return nil, errors.New("gateway.openai_core.credential_encryption_key is required when gemini.production_mode=true")
		}
		return nil, nil
	}
	return NewGeminiSecretProtectorFromHex(keyHex)
}

func NewGeminiSecretProtectorFromHex(keyHex string) (*GeminiSecretProtector, error) {
	key, err := hex.DecodeString(strings.TrimSpace(keyHex))
	if err != nil {
		return nil, errors.New("gateway.openai_core.credential_encryption_key must be valid hex")
	}
	return NewGeminiSecretProtector(key)
}

func NewGeminiSecretProtector(key []byte) (*GeminiSecretProtector, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("gateway.openai_core.credential_encryption_key must decode to 32 bytes")
	}
	copied := make([]byte, len(key))
	copy(copied, key)
	return &GeminiSecretProtector{key: copied}, nil
}

func (p *GeminiSecretProtector) EncryptValue(plaintext string) (string, error) {
	if p == nil {
		return plaintext, nil
	}
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return "", errors.New("failed to initialize gemini credential cipher")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", errors.New("failed to initialize gemini credential gcm")
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", errors.New("failed to generate gemini credential nonce")
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return geminiSecretProtectorPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (p *GeminiSecretProtector) DecryptValue(ciphertext string) (string, error) {
	if !strings.HasPrefix(strings.TrimSpace(ciphertext), geminiSecretProtectorPrefix) {
		return ciphertext, nil
	}
	if p == nil {
		return "", errGeminiEncryptedCredentialUnavailable
	}
	encoded := strings.TrimPrefix(strings.TrimSpace(ciphertext), geminiSecretProtectorPrefix)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", errors.New("invalid encrypted gemini credential")
	}
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return "", errors.New("failed to initialize gemini credential cipher")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", errors.New("failed to initialize gemini credential gcm")
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("invalid encrypted gemini credential")
	}
	nonce := data[:nonceSize]
	payload := data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", errors.New("invalid encrypted gemini credential")
	}
	return string(plaintext), nil
}

func (p *GeminiSecretProtector) ProtectCredentials(input map[string]any) (map[string]any, error) {
	if len(input) == 0 {
		return map[string]any{}, nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
		if !shouldProtectGeminiCredentialKey(key) {
			continue
		}
		raw, ok, err := serializeGeminiCredentialValue(value)
		if err != nil {
			return nil, fmt.Errorf("serialize gemini credential field %s: %w", key, err)
		}
		if !ok || strings.TrimSpace(raw) == "" || strings.HasPrefix(strings.TrimSpace(raw), geminiSecretProtectorPrefix) {
			continue
		}
		encrypted, err := p.EncryptValue(raw)
		if err != nil {
			return nil, fmt.Errorf("encrypt gemini credential field %s: %w", key, err)
		}
		out[key] = encrypted
	}
	return out, nil
}

func (p *GeminiSecretProtector) UnprotectCredentials(input map[string]any) (map[string]any, error) {
	if len(input) == 0 {
		return map[string]any{}, nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
		if !shouldProtectGeminiCredentialKey(key) {
			continue
		}
		raw, ok, err := serializeGeminiCredentialValue(value)
		if err != nil {
			return nil, fmt.Errorf("serialize gemini credential field %s: %w", key, err)
		}
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		decrypted, err := p.DecryptValue(raw)
		if err != nil {
			return nil, fmt.Errorf("decrypt gemini credential field %s: %w", key, err)
		}
		out[key] = decrypted
	}
	return out, nil
}

func shouldProtectGeminiCredentialKey(key string) bool {
	_, ok := geminiProtectedCredentialFields[strings.TrimSpace(strings.ToLower(key))]
	return ok
}

func serializeGeminiCredentialValue(value any) (string, bool, error) {
	switch raw := value.(type) {
	case string:
		return raw, true, nil
	case []byte:
		return string(raw), true, nil
	case map[string]any:
		b, err := json.Marshal(raw)
		if err != nil {
			return "", false, err
		}
		return string(b), true, nil
	default:
		return "", false, nil
	}
}
