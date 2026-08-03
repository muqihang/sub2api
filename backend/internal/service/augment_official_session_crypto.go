package service

import (
	"crypto/aes"
	"crypto/cipher"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type AugmentSessionVaultKeyset struct {
	ActiveKeyID string
	Keys        map[string][]byte
}

type AugmentSessionVaultCipher struct {
	activeKeyID string
	keys        map[string][]byte
	rand        io.Reader
}

type augmentSessionVaultEnvelope struct {
	KeyID      string `json:"kid"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func NewAugmentSessionVaultCipher(keyset AugmentSessionVaultKeyset) (*AugmentSessionVaultCipher, error) {
	activeKeyID := strings.TrimSpace(keyset.ActiveKeyID)
	if activeKeyID == "" {
		return nil, fmt.Errorf("augment session vault active key id is required")
	}

	keys := make(map[string][]byte, len(keyset.Keys))
	for keyID, key := range keyset.Keys {
		trimmedKeyID := strings.TrimSpace(keyID)
		if trimmedKeyID == "" {
			return nil, fmt.Errorf("augment session vault key id is required")
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("augment session vault key %q must be 32 bytes", trimmedKeyID)
		}
		keys[trimmedKeyID] = append([]byte(nil), key...)
	}

	if _, ok := keys[activeKeyID]; !ok {
		return nil, fmt.Errorf("augment session vault active key %q is missing", activeKeyID)
	}

	return &AugmentSessionVaultCipher{
		activeKeyID: activeKeyID,
		keys:        keys,
		rand:        cryptorand.Reader,
	}, nil
}

func (c *AugmentSessionVaultCipher) Encrypt(plaintext []byte) ([]byte, error) {
	key, ok := c.keys[c.activeKeyID]
	if !ok {
		return nil, fmt.Errorf("augment session vault active key %q is unavailable", c.activeKeyID)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create augment session vault cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create augment session vault gcm: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(c.rand, nonce); err != nil {
		return nil, fmt.Errorf("generate augment session vault nonce: %w", err)
	}

	envelope := augmentSessionVaultEnvelope{
		KeyID:      c.activeKeyID,
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(aead.Seal(nil, nonce, plaintext, nil)),
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal augment session vault envelope: %w", err)
	}
	return data, nil
}

func (c *AugmentSessionVaultCipher) Decrypt(ciphertext []byte) ([]byte, error) {
	var envelope augmentSessionVaultEnvelope
	if err := json.Unmarshal(ciphertext, &envelope); err != nil {
		return nil, fmt.Errorf("unmarshal augment session vault envelope: %w", err)
	}

	keyID := strings.TrimSpace(envelope.KeyID)
	key, ok := c.keys[keyID]
	if !ok {
		return nil, fmt.Errorf("augment session vault key %q is unavailable", keyID)
	}

	nonce, err := base64.RawStdEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decode augment session vault nonce: %w", err)
	}
	encrypted, err := base64.RawStdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode augment session vault ciphertext: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create augment session vault cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create augment session vault gcm: %w", err)
	}

	plaintext, err := aead.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt augment session vault ciphertext: %w", err)
	}
	return plaintext, nil
}

func (c *AugmentSessionVaultCipher) ReencryptToActive(ciphertext []byte) ([]byte, error) {
	plaintext, err := c.Decrypt(ciphertext)
	if err != nil {
		return nil, err
	}
	return c.Encrypt(plaintext)
}
