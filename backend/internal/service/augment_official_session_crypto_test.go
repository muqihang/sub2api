package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAugmentSessionVaultEncryptsAndDecryptsWithActiveKey(t *testing.T) {
	vault, err := NewAugmentSessionVaultCipher(AugmentSessionVaultKeyset{
		ActiveKeyID: "key-active",
		Keys: map[string][]byte{
			"key-active": []byte("0123456789abcdef0123456789abcdef"),
		},
	})
	require.NoError(t, err)

	plaintext := []byte(`{"access_token":"access-plain","refresh_token":"refresh-plain"}`)

	ciphertext, err := vault.Encrypt(plaintext)
	require.NoError(t, err)
	require.NotEmpty(t, ciphertext)

	var envelope map[string]string
	require.NoError(t, json.Unmarshal(ciphertext, &envelope))
	require.Equal(t, "key-active", envelope["kid"])

	decrypted, err := vault.Decrypt(ciphertext)
	require.NoError(t, err)
	require.Equal(t, plaintext, decrypted)
}

func TestAugmentSessionVaultRejectsWrongKey(t *testing.T) {
	writerVault, err := NewAugmentSessionVaultCipher(AugmentSessionVaultKeyset{
		ActiveKeyID: "key-active",
		Keys: map[string][]byte{
			"key-active": []byte("0123456789abcdef0123456789abcdef"),
		},
	})
	require.NoError(t, err)

	readerVault, err := NewAugmentSessionVaultCipher(AugmentSessionVaultKeyset{
		ActiveKeyID: "key-active",
		Keys: map[string][]byte{
			"key-active": []byte("fedcba9876543210fedcba9876543210"),
		},
	})
	require.NoError(t, err)

	ciphertext, err := writerVault.Encrypt([]byte(`{"refresh_token":"wrong-key-check"}`))
	require.NoError(t, err)

	_, err = readerVault.Decrypt(ciphertext)
	require.Error(t, err)
}

func TestAugmentSessionVaultSupportsOldKeyReadNewKeyWrite(t *testing.T) {
	oldVault, err := NewAugmentSessionVaultCipher(AugmentSessionVaultKeyset{
		ActiveKeyID: "key-old",
		Keys: map[string][]byte{
			"key-old": []byte("0123456789abcdef0123456789abcdef"),
		},
	})
	require.NoError(t, err)

	rotatedVault, err := NewAugmentSessionVaultCipher(AugmentSessionVaultKeyset{
		ActiveKeyID: "key-new",
		Keys: map[string][]byte{
			"key-old": []byte("0123456789abcdef0123456789abcdef"),
			"key-new": []byte("abcdef0123456789abcdef0123456789"),
		},
	})
	require.NoError(t, err)

	plaintext := []byte(`{"refresh_token":"rotation-check"}`)
	oldCiphertext, err := oldVault.Encrypt(plaintext)
	require.NoError(t, err)

	decrypted, err := rotatedVault.Decrypt(oldCiphertext)
	require.NoError(t, err)
	require.Equal(t, plaintext, decrypted)

	newCiphertext, err := rotatedVault.Encrypt(plaintext)
	require.NoError(t, err)

	var envelope map[string]string
	require.NoError(t, json.Unmarshal(newCiphertext, &envelope))
	require.Equal(t, "key-new", envelope["kid"])

	rotatedCiphertext, err := rotatedVault.ReencryptToActive(oldCiphertext)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(rotatedCiphertext, &envelope))
	require.Equal(t, "key-new", envelope["kid"])
}

func TestAugmentSessionVaultCiphertextDoesNotContainSecret(t *testing.T) {
	vault, err := NewAugmentSessionVaultCipher(AugmentSessionVaultKeyset{
		ActiveKeyID: "key-active",
		Keys: map[string][]byte{
			"key-active": []byte("0123456789abcdef0123456789abcdef"),
		},
	})
	require.NoError(t, err)

	secretMarker := "super-secret-refresh-token"
	ciphertext, err := vault.Encrypt([]byte(`{"refresh_token":"` + secretMarker + `"}`))
	require.NoError(t, err)
	require.NotContains(t, string(ciphertext), secretMarker)
}
