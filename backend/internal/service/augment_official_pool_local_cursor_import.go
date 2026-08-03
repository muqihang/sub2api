package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"golang.org/x/crypto/pbkdf2"
	_ "modernc.org/sqlite"
)

const (
	defaultAugmentCursorSafeStorageService = "Cursor Safe Storage"
	defaultAugmentCursorSessionSecretKey   = `secret://{"extensionId":"augment.vscode-augment","key":"augment.sessions"}`
	chromiumSafeStoragePrefixV10           = "v10"
)

var (
	ErrAugmentLocalCursorSessionNotFound = infraerrors.NotFound(
		"AUGMENT_LOCAL_CURSOR_SESSION_NOT_FOUND",
		"augment local cursor official session was not found",
	)
	ErrAugmentLocalCursorSessionInvalid = infraerrors.BadRequest(
		"AUGMENT_LOCAL_CURSOR_SESSION_INVALID",
		"augment local cursor official session is invalid",
	)
)

type AugmentOfficialPoolLocalCursorImportRequest struct {
	Source           string `json:"source"`
	StateDBPath      string `json:"state_db_path,omitempty"`
	KeychainService  string `json:"keychain_service,omitempty"`
	SecretStorageKey string `json:"secret_storage_key,omitempty"`
}

type augmentLocalCursorImportedSession struct {
	AccessToken   string   `json:"accessToken"`
	TenantURL     string   `json:"tenantURL"`
	Scopes        []string `json:"scopes"`
	SessionSource string   `json:"sessionSource"`
	PatchMarker   string   `json:"patchMarker,omitempty"`
}

type augmentLocalCursorSessionReader interface {
	ReadCurrentSession(ctx context.Context, input AugmentOfficialPoolLocalCursorImportRequest) (*augmentLocalCursorImportedSession, error)
}

type augmentLocalCursorSessionReaderFunc func(context.Context, AugmentOfficialPoolLocalCursorImportRequest) (*augmentLocalCursorImportedSession, error)

func (f augmentLocalCursorSessionReaderFunc) ReadCurrentSession(ctx context.Context, input AugmentOfficialPoolLocalCursorImportRequest) (*augmentLocalCursorImportedSession, error) {
	return f(ctx, input)
}

type augmentLocalCursorKeychainReader interface {
	ReadSafeStoragePassword(ctx context.Context, serviceName string) (string, error)
}

type defaultAugmentLocalCursorSessionReader struct {
	keychain augmentLocalCursorKeychainReader
}

type defaultAugmentLocalCursorKeychainReader struct{}

func (s *AugmentOfficialPoolSessionService) SetLocalCursorSessionReader(reader augmentLocalCursorSessionReader) {
	if s == nil {
		return
	}
	s.localCursorReader = reader
}

func (s *AugmentOfficialPoolSessionService) ImportLocalCursorSessionForAdmin(ctx context.Context, adminUserID int64, input AugmentOfficialPoolLocalCursorImportRequest) (*AugmentOfficialPoolSessionAdminView, error) {
	if s == nil || s.store == nil || s.cipher == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_OFFICIAL_POOL_UNAVAILABLE", "augment official pool service is unavailable")
	}

	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = augmentOfficialSessionSourceOfficialQuickLogin
	}
	if err := ValidateAugmentOfficialSource(source, AugmentOfficialSourcePolicy{}); err != nil {
		return nil, err
	}

	reader := s.localCursorReader
	if reader == nil {
		reader = defaultAugmentLocalCursorSessionReader{
			keychain: defaultAugmentLocalCursorKeychainReader{},
		}
	}

	imported, err := reader.ReadCurrentSession(ctx, input)
	if err != nil {
		return nil, err
	}
	if imported == nil {
		return nil, ErrAugmentLocalCursorSessionNotFound
	}

	tenantOrigin, err := NormalizeAugmentOfficialOrigin(imported.TenantURL)
	if err != nil {
		return nil, err
	}
	accessToken := strings.TrimSpace(imported.AccessToken)
	if accessToken == "" {
		return nil, ErrAugmentLocalCursorSessionInvalid
	}
	scopes := dedupeAugmentLocalCursorScopes(imported.Scopes)
	if len(scopes) == 0 {
		scopes = append([]string(nil), defaultAugmentPluginScopes...)
	}

	secretData, err := json.Marshal(augmentOfficialEncryptedCredentialPayload{
		AccessToken: accessToken,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal augment local cursor session payload: %w", err)
	}
	encryptedPayload, err := s.cipher.Encrypt(secretData)
	if err != nil {
		return nil, err
	}
	keyVersion, err := encryptedPayloadKeyID(encryptedPayload)
	if err != nil {
		return nil, err
	}

	now := s.now()
	stored, err := s.store.UpsertPoolSession(ctx, AugmentOfficialPoolStoredSessionInput{
		Source:                     source,
		TenantOrigin:               tenantOrigin,
		Scopes:                     scopes,
		LastSuccessAt:              ptrTimeValue(now),
		Status:                     AugmentOfficialPoolSessionStatusActive,
		EncryptedCredentialPayload: encryptedPayload,
		CredentialSchemaVersion:    1,
		KeyVersion:                 keyVersion,
		Fingerprint:                buildAugmentOfficialFingerprint(source, tenantOrigin, accessToken, "", ""),
		CreatedByAdminID:           adminUserID,
		HealthScore:                augmentOfficialPoolSessionDefaultHealthScore,
	})
	if err != nil {
		return nil, err
	}
	view := storedPoolAdminViewToAdminView(stored)
	return &view, nil
}

func (r defaultAugmentLocalCursorSessionReader) ReadCurrentSession(ctx context.Context, input AugmentOfficialPoolLocalCursorImportRequest) (*augmentLocalCursorImportedSession, error) {
	dbPath := resolveAugmentLocalCursorStateDBPath(strings.TrimSpace(input.StateDBPath))
	serviceName := strings.TrimSpace(input.KeychainService)
	if serviceName == "" {
		serviceName = defaultAugmentCursorSafeStorageService
	}
	secretKey := strings.TrimSpace(input.SecretStorageKey)
	if secretKey == "" {
		secretKey = defaultAugmentCursorSessionSecretKey
	}

	encrypted, err := readAugmentLocalCursorSecretBlob(ctx, dbPath, secretKey)
	if err != nil {
		return nil, err
	}
	password, err := r.keychain.ReadSafeStoragePassword(ctx, serviceName)
	if err != nil {
		return nil, err
	}
	plaintext, err := decryptAugmentLocalCursorSecretBlob(encrypted, password)
	if err != nil {
		return nil, err
	}

	var session augmentLocalCursorImportedSession
	if err := json.Unmarshal([]byte(plaintext), &session); err != nil {
		return nil, fmt.Errorf("decode augment local cursor session: %w", err)
	}
	return &session, nil
}

func resolveAugmentLocalCursorStateDBPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb")
	}
	if strings.HasPrefix(raw, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			raw = filepath.Join(home, strings.TrimPrefix(raw, "~"))
		}
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return ""
	}
	return abs
}

func readAugmentLocalCursorSecretBlob(ctx context.Context, dbPath, secretKey string) ([]byte, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, ErrAugmentLocalCursorSessionNotFound
	}
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrAugmentLocalCursorSessionNotFound
		}
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open cursor state db: %w", err)
	}
	defer db.Close()

	var raw string
	if err := db.QueryRowContext(ctx, "SELECT value FROM ItemTable WHERE key = ?", secretKey).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAugmentLocalCursorSessionNotFound
		}
		return nil, fmt.Errorf("read cursor secret blob: %w", err)
	}

	var nodeBuffer struct {
		Type string `json:"type"`
		Data []byte `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &nodeBuffer); err != nil {
		return nil, fmt.Errorf("decode cursor secret blob envelope: %w", err)
	}
	if nodeBuffer.Type != "Buffer" || len(nodeBuffer.Data) < len(chromiumSafeStoragePrefixV10) {
		return nil, ErrAugmentLocalCursorSessionInvalid
	}
	return append([]byte(nil), nodeBuffer.Data...), nil
}

func (defaultAugmentLocalCursorKeychainReader) ReadSafeStoragePassword(ctx context.Context, serviceName string) (string, error) {
	cmd := exec.CommandContext(ctx, "security", "find-generic-password", "-s", serviceName, "-g")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("read safe storage password: %w", err)
	}
	match := regexp.MustCompile(`password: "([^"]+)"`).FindStringSubmatch(string(out))
	if len(match) != 2 {
		return "", ErrAugmentLocalCursorSessionInvalid
	}
	return match[1], nil
}

func decryptAugmentLocalCursorSecretBlob(blob []byte, safeStoragePassword string) (string, error) {
	if len(blob) <= len(chromiumSafeStoragePrefixV10) || string(blob[:3]) != chromiumSafeStoragePrefixV10 {
		return "", ErrAugmentLocalCursorSessionInvalid
	}
	key := pbkdf2.Key([]byte(safeStoragePassword), []byte("saltysalt"), 1003, 16, sha1.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cursor safe storage cipher: %w", err)
	}
	ciphertext := append([]byte(nil), blob[3:]...)
	if len(ciphertext)%aes.BlockSize != 0 {
		return "", ErrAugmentLocalCursorSessionInvalid
	}
	iv := []byte(strings.Repeat(" ", aes.BlockSize))
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(ciphertext, ciphertext)
	plaintext, err := pkcs7Unpad(ciphertext, aes.BlockSize)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || blockSize <= 0 || len(data)%blockSize != 0 {
		return nil, ErrAugmentLocalCursorSessionInvalid
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize || padding > len(data) {
		return nil, ErrAugmentLocalCursorSessionInvalid
	}
	for _, b := range data[len(data)-padding:] {
		if int(b) != padding {
			return nil, ErrAugmentLocalCursorSessionInvalid
		}
	}
	return data[:len(data)-padding], nil
}

func dedupeAugmentLocalCursorScopes(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
