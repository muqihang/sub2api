package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const openAISecretProtectorPrefix = "enc:v1:"

var errOpenAIEncryptedCredentialUnavailable = errors.New("encrypted openai credentials require credential protection to be configured")

var openAIProtectedCredentialFields = map[string]struct{}{
	"access_token":  {},
	"refresh_token": {},
	"id_token":      {},
	"api_key":       {},
}

type OpenAISecretProtector struct {
	key []byte
}

func ProvideOpenAISecretProtector(cfg *config.Config) (*OpenAISecretProtector, error) {
	if cfg == nil {
		return nil, nil
	}
	keyHex := strings.TrimSpace(cfg.Gateway.OpenAICore.CredentialEncryptionKey)
	if keyHex == "" {
		if cfg.Gateway.OpenAICore.ProductionMode && cfg.Gateway.OpenAICore.RequireEncryptedCredentials {
			return nil, errors.New("gateway.openai_core.credential_encryption_key is required when production_mode=true and require_encrypted_credentials=true")
		}
		return nil, nil
	}
	return NewOpenAISecretProtectorFromHex(keyHex)
}

func NewOpenAISecretProtectorFromHex(keyHex string) (*OpenAISecretProtector, error) {
	key, err := hex.DecodeString(strings.TrimSpace(keyHex))
	if err != nil {
		return nil, errors.New("gateway.openai_core.credential_encryption_key must be valid hex")
	}
	return NewOpenAISecretProtector(key)
}

func NewOpenAISecretProtector(key []byte) (*OpenAISecretProtector, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("gateway.openai_core.credential_encryption_key must decode to 32 bytes")
	}
	copied := make([]byte, len(key))
	copy(copied, key)
	return &OpenAISecretProtector{key: copied}, nil
}

func (p *OpenAISecretProtector) EncryptValue(plaintext string) (string, error) {
	if p == nil {
		return plaintext, nil
	}
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return "", errors.New("failed to initialize openai credential cipher")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", errors.New("failed to initialize openai credential gcm")
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", errors.New("failed to generate openai credential nonce")
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return openAISecretProtectorPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (p *OpenAISecretProtector) DecryptValue(ciphertext string) (string, error) {
	if !strings.HasPrefix(strings.TrimSpace(ciphertext), openAISecretProtectorPrefix) {
		return ciphertext, nil
	}
	if p == nil {
		return "", errOpenAIEncryptedCredentialUnavailable
	}
	encoded := strings.TrimPrefix(strings.TrimSpace(ciphertext), openAISecretProtectorPrefix)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", errors.New("invalid encrypted openai credential")
	}
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return "", errors.New("failed to initialize openai credential cipher")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", errors.New("failed to initialize openai credential gcm")
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("invalid encrypted openai credential")
	}
	nonce := data[:nonceSize]
	payload := data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", errors.New("invalid encrypted openai credential")
	}
	return string(plaintext), nil
}

func (p *OpenAISecretProtector) ProtectCredentials(input map[string]any) (map[string]any, error) {
	if len(input) == 0 {
		return map[string]any{}, nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
		if !shouldProtectOpenAICredentialKey(key) {
			continue
		}
		raw, ok := value.(string)
		if !ok || strings.TrimSpace(raw) == "" || strings.HasPrefix(strings.TrimSpace(raw), openAISecretProtectorPrefix) {
			continue
		}
		encrypted, err := p.EncryptValue(raw)
		if err != nil {
			return nil, fmt.Errorf("encrypt openai credential field %s: %w", key, err)
		}
		out[key] = encrypted
	}
	return out, nil
}

func (p *OpenAISecretProtector) UnprotectCredentials(input map[string]any) (map[string]any, error) {
	if len(input) == 0 {
		return map[string]any{}, nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
		if !shouldProtectOpenAICredentialKey(key) {
			continue
		}
		raw, ok := value.(string)
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		decrypted, err := p.DecryptValue(raw)
		if err != nil {
			return nil, fmt.Errorf("decrypt openai credential field %s: %w", key, err)
		}
		out[key] = decrypted
	}
	return out, nil
}

func shouldProtectOpenAICredentialKey(key string) bool {
	_, ok := openAIProtectedCredentialFields[strings.TrimSpace(strings.ToLower(key))]
	return ok
}
