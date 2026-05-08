package service

import (
	"context"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	augmentOfficialSessionModeOfficialPassthrough = "official_passthrough"
	augmentOfficialSessionStatusActive            = "active"
	augmentOfficialSessionStatusRevoked           = "revoked"
	augmentOfficialFingerprintPrefixLength        = 12
)

var (
	ErrAugmentOfficialBindTokenMissing = infraerrors.Unauthorized(
		"AUGMENT_OFFICIAL_BIND_TOKEN_MISSING",
		"augment official bind token is required",
	)
	ErrAugmentOfficialBindTokenInvalid = infraerrors.Unauthorized(
		"AUGMENT_OFFICIAL_BIND_TOKEN_INVALID",
		"augment official bind token is invalid",
	)
	ErrAugmentOfficialCredentialSchemaInvalid = infraerrors.BadRequest(
		"AUGMENT_OFFICIAL_CREDENTIAL_SCHEMA_INVALID",
		"augment official credential payload contains unsupported fields",
	)
	ErrAugmentOfficialCredentialExpired = infraerrors.BadRequest(
		"AUGMENT_OFFICIAL_CREDENTIAL_EXPIRED",
		"augment official credential payload is already expired",
	)
	ErrAugmentOfficialSessionInactive = infraerrors.Unauthorized(
		"AUGMENT_OFFICIAL_SESSION_INACTIVE",
		"augment official session is not active",
	)
	ErrAugmentOfficialSessionBindIntentConsumed = infraerrors.Unauthorized(
		"AUGMENT_OFFICIAL_BIND_INTENT_CONSUMED",
		"augment official bind intent has already been used",
	)
)

type AugmentOfficialSessionStore interface {
	CreateBindIntent(ctx context.Context, input AugmentOfficialSessionBindIntentStoreCreateInput) (*AugmentOfficialSessionBindIntentStoreRecord, error)
	ConsumeBindIntent(ctx context.Context, bindIntentID string, userID int64) (*AugmentOfficialSessionBindIntentStoreRecord, error)
	UpsertActiveSession(ctx context.Context, input AugmentOfficialSessionStoredSessionInput) (*AugmentOfficialSessionStoredPublicView, error)
	GetActiveSessionPublicView(ctx context.Context, userID int64) (*AugmentOfficialSessionStoredPublicView, error)
	GetActiveSessionCredentialRow(ctx context.Context, userID int64) (*AugmentOfficialSessionStoredCredentialRow, error)
	RevokeActiveSession(ctx context.Context, userID int64) (*AugmentOfficialSessionStoredPublicView, error)
}

type AugmentOfficialSessionCipher interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
	ReencryptToActive(ciphertext []byte) ([]byte, error)
}

type AugmentOfficialSessionBindIntentStoreCreateInput struct {
	UserID          int64
	StateHash       string
	Mode            string
	Source          string
	TenantAllowlist []string
}

type AugmentOfficialSessionBindIntentStoreRecord struct {
	ID              int64
	UserID          int64
	BindIntentID    string
	StateHash       string
	Mode            string
	Source          string
	TenantAllowlist []string
	ExpiresAt       time.Time
	ConsumedAt      *time.Time
	CreatedAt       time.Time
}

type AugmentOfficialSessionStoredSessionInput struct {
	UserID                     int64
	Mode                       string
	Source                     string
	TenantOrigin               string
	PortalOrigin               *string
	Scopes                     []string
	ExpiresAt                  *time.Time
	LastRefreshAt              *time.Time
	LastSuccessAt              *time.Time
	LastErrorAt                *time.Time
	LastErrorCode              *string
	Status                     string
	EncryptedCredentialPayload []byte
	CredentialSchemaVersion    int
	KeyVersion                 string
	Fingerprint                string
}

type AugmentOfficialSessionStoredPublicView struct {
	UserID                  int64
	Mode                    string
	Source                  string
	TenantOrigin            string
	PortalOrigin            *string
	Scopes                  []string
	ExpiresAt               *time.Time
	LastRefreshAt           *time.Time
	LastSuccessAt           *time.Time
	LastErrorAt             *time.Time
	LastErrorCode           *string
	Status                  string
	CredentialSchemaVersion int
	KeyVersion              string
	Fingerprint             string
	CreatedAt               time.Time
	UpdatedAt               time.Time
	RevokedAt               *time.Time
}

type AugmentOfficialSessionStoredCredentialRow struct {
	UserID                     int64
	Mode                       string
	Source                     string
	TenantOrigin               string
	PortalOrigin               *string
	Scopes                     []string
	ExpiresAt                  *time.Time
	LastRefreshAt              *time.Time
	LastSuccessAt              *time.Time
	LastErrorAt                *time.Time
	LastErrorCode              *string
	Status                     string
	EncryptedCredentialPayload []byte
	CredentialSchemaVersion    int
	KeyVersion                 string
	Fingerprint                string
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
	RevokedAt                  *time.Time
}

type AugmentOfficialBindIntentRequest struct {
	Mode            string
	Source          string
	TenantAllowlist []string
}

type AugmentOfficialBindIntentResponse struct {
	BindIntentID string    `json:"bind_intent_id"`
	State        string    `json:"state"`
	ExpiresAt    time.Time `json:"expires_at"`
	BindToken    string    `json:"bind_token"`
}

type AugmentOfficialBindRequest struct {
	BindIntentID string         `json:"bind_intent_id"`
	State        string         `json:"state"`
	Mode         string         `json:"mode"`
	Source       string         `json:"source"`
	Payload      map[string]any `json:"payload"`
	RequestID    string         `json:"request_id,omitempty"`
}

type AugmentOfficialSessionPublicView struct {
	UserID            int64      `json:"user_id"`
	Mode              string     `json:"mode"`
	Source            string     `json:"source"`
	TenantOrigin      string     `json:"tenant_origin"`
	PortalOrigin      *string    `json:"portal_origin,omitempty"`
	Scopes            []string   `json:"scopes"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	LastRefreshAt     *time.Time `json:"last_refresh_at,omitempty"`
	LastSuccessAt     *time.Time `json:"last_success_at,omitempty"`
	LastErrorAt       *time.Time `json:"last_error_at,omitempty"`
	LastErrorCode     *string    `json:"last_error_code,omitempty"`
	Status            string     `json:"status"`
	FingerprintPrefix string     `json:"fingerprint_prefix"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
}

type AugmentOfficialSessionCredential struct {
	UserID                  int64
	Mode                    string
	Source                  string
	TenantOrigin            string
	PortalOrigin            *string
	Scopes                  []string
	ExpiresAt               *time.Time
	LastRefreshAt           *time.Time
	LastSuccessAt           *time.Time
	LastErrorAt             *time.Time
	LastErrorCode           *string
	Status                  string
	AccessToken             string
	RefreshToken            string
	OfficialSessionID       string
	CredentialSchemaVersion int
	KeyVersion              string
	Fingerprint             string
	CreatedAt               time.Time
	UpdatedAt               time.Time
	RevokedAt               *time.Time
}

type AugmentOfficialSessionAuditEvent struct {
	RecordedAt        time.Time `json:"recorded_at"`
	ActorUserID       int64     `json:"actor_user_id"`
	UserID            int64     `json:"user_id"`
	Source            string    `json:"source"`
	TenantHost        string    `json:"tenant_host"`
	FingerprintPrefix string    `json:"fingerprint_prefix"`
	Result            string    `json:"result"`
	RequestID         string    `json:"request_id,omitempty"`
	ErrorClass        string    `json:"error_class,omitempty"`
}

type augmentOfficialBindTokenClaims struct {
	BindIntentID string `json:"bid"`
	UserID       int64  `json:"uid"`
	ExpiresAt    int64  `json:"exp"`
	StateHash    string `json:"sh"`
	Mode         string `json:"mode"`
	Source       string `json:"source"`
}

type augmentOfficialNormalizedCredentialPayload struct {
	TenantOrigin            string
	PortalOrigin            *string
	AccessToken             string
	RefreshToken            string
	ExpiresAt               time.Time
	Scopes                  []string
	OfficialSessionID       string
	CredentialSchemaVersion int
}

type augmentOfficialEncryptedCredentialPayload struct {
	AccessToken       string `json:"access_token"`
	RefreshToken      string `json:"refresh_token,omitempty"`
	OfficialSessionID string `json:"official_session_id,omitempty"`
}

type augmentSessionVaultKeyEnvelope struct {
	KeyID string `json:"kid"`
}

type AugmentOfficialSessionService struct {
	store           AugmentOfficialSessionStore
	cipher          AugmentOfficialSessionCipher
	bindTokenSecret []byte
	now             func() time.Time
	newToken        func(int) (string, error)

	auditMu sync.Mutex
	audit   []AugmentOfficialSessionAuditEvent
}

func NewAugmentOfficialSessionService(store AugmentOfficialSessionStore, cipher AugmentOfficialSessionCipher, bindTokenSecret string) *AugmentOfficialSessionService {
	return &AugmentOfficialSessionService{
		store:           store,
		cipher:          cipher,
		bindTokenSecret: []byte(strings.TrimSpace(bindTokenSecret)),
		now: func() time.Time {
			return time.Now().UTC()
		},
		newToken: augmentOfficialRandomHexToken,
	}
}

func (s *AugmentOfficialSessionService) CreateBindIntent(ctx context.Context, actorUserID int64, input AugmentOfficialBindIntentRequest) (*AugmentOfficialBindIntentResponse, error) {
	if s == nil || s.store == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_OFFICIAL_SESSION_UNAVAILABLE", "augment official session service is unavailable")
	}
	mode, err := normalizeAugmentOfficialSessionMode(input.Mode)
	if err != nil {
		return nil, err
	}
	if err := ValidateAugmentOfficialSource(input.Source, AugmentOfficialSourcePolicy{}); err != nil {
		return nil, err
	}
	tenantAllowlist, err := normalizeTenantAllowlist(input.TenantAllowlist)
	if err != nil {
		return nil, err
	}
	state, err := s.newToken(16)
	if err != nil {
		return nil, fmt.Errorf("generate augment official bind state: %w", err)
	}
	stateHash := sha256HexString(state)
	record, err := s.store.CreateBindIntent(ctx, AugmentOfficialSessionBindIntentStoreCreateInput{
		UserID:          actorUserID,
		StateHash:       stateHash,
		Mode:            mode,
		Source:          strings.TrimSpace(input.Source),
		TenantAllowlist: tenantAllowlist,
	})
	if err != nil {
		return nil, err
	}
	claims := augmentOfficialBindTokenClaims{
		BindIntentID: record.BindIntentID,
		UserID:       actorUserID,
		ExpiresAt:    record.ExpiresAt.UTC().Unix(),
		StateHash:    stateHash,
		Mode:         mode,
		Source:       strings.TrimSpace(input.Source),
	}
	bindToken, err := s.signBindToken(claims)
	if err != nil {
		return nil, err
	}
	return &AugmentOfficialBindIntentResponse{
		BindIntentID: record.BindIntentID,
		State:        state,
		ExpiresAt:    record.ExpiresAt.UTC(),
		BindToken:    bindToken,
	}, nil
}

func (s *AugmentOfficialSessionService) BindOfficialSession(ctx context.Context, actorUserID int64, bindToken string, input AugmentOfficialBindRequest) (_ *AugmentOfficialSessionPublicView, err error) {
	if strings.TrimSpace(bindToken) == "" {
		return nil, ErrAugmentOfficialBindTokenMissing
	}

	claims, err := s.parseBindToken(bindToken)
	if err != nil {
		s.appendAudit(AugmentOfficialSessionAuditEvent{
			RecordedAt:  s.now(),
			ActorUserID: actorUserID,
			UserID:      actorUserID,
			Result:      "bind_rejected",
			RequestID:   strings.TrimSpace(input.RequestID),
			ErrorClass:  infraerrors.Reason(err),
		})
		return nil, err
	}
	if claims.UserID != actorUserID || claims.BindIntentID != strings.TrimSpace(input.BindIntentID) {
		err = ErrAugmentOfficialBindTokenInvalid
		s.appendAudit(AugmentOfficialSessionAuditEvent{
			RecordedAt:  s.now(),
			ActorUserID: actorUserID,
			UserID:      actorUserID,
			Result:      "bind_rejected",
			RequestID:   strings.TrimSpace(input.RequestID),
			ErrorClass:  infraerrors.Reason(err),
		})
		return nil, err
	}

	mode, err := normalizeAugmentOfficialSessionMode(input.Mode)
	if err != nil {
		return nil, err
	}
	source := strings.TrimSpace(input.Source)
	if err := ValidateAugmentOfficialSource(source, AugmentOfficialSourcePolicy{}); err != nil {
		return nil, err
	}
	if claims.Mode != mode || claims.Source != source || claims.StateHash != sha256HexString(strings.TrimSpace(input.State)) {
		err = ErrAugmentOfficialTenantSessionMismatch
		s.appendAudit(AugmentOfficialSessionAuditEvent{
			RecordedAt:  s.now(),
			ActorUserID: actorUserID,
			UserID:      actorUserID,
			Source:      source,
			Result:      "bind_rejected",
			RequestID:   strings.TrimSpace(input.RequestID),
			ErrorClass:  infraerrors.Reason(err),
		})
		return nil, err
	}

	normalizedPayload, err := normalizeAugmentOfficialCredentialPayload(input.Payload, s.now())
	if err != nil {
		s.appendAudit(AugmentOfficialSessionAuditEvent{
			RecordedAt:  s.now(),
			ActorUserID: actorUserID,
			UserID:      actorUserID,
			Source:      source,
			TenantHost:  hostFromRawPayload(input.Payload),
			Result:      "bind_rejected",
			RequestID:   strings.TrimSpace(input.RequestID),
			ErrorClass:  infraerrors.Reason(err),
		})
		return nil, err
	}

	intentRecord, err := s.store.ConsumeBindIntent(ctx, claims.BindIntentID, actorUserID)
	if err != nil {
		s.appendAudit(AugmentOfficialSessionAuditEvent{
			RecordedAt:  s.now(),
			ActorUserID: actorUserID,
			UserID:      actorUserID,
			Source:      source,
			TenantHost:  hostFromOrigin(normalizedPayload.TenantOrigin),
			Result:      "bind_rejected",
			RequestID:   strings.TrimSpace(input.RequestID),
			ErrorClass:  infraerrors.Reason(err),
		})
		return nil, err
	}
	if intentRecord.Mode != mode || intentRecord.Source != source || intentRecord.StateHash != claims.StateHash || intentRecord.UserID != actorUserID {
		err = ErrAugmentOfficialTenantSessionMismatch
		s.appendAudit(AugmentOfficialSessionAuditEvent{
			RecordedAt:  s.now(),
			ActorUserID: actorUserID,
			UserID:      actorUserID,
			Source:      source,
			TenantHost:  hostFromOrigin(normalizedPayload.TenantOrigin),
			Result:      "bind_rejected",
			RequestID:   strings.TrimSpace(input.RequestID),
			ErrorClass:  infraerrors.Reason(err),
		})
		return nil, err
	}
	if !originAllowed(intentRecord.TenantAllowlist, normalizedPayload.TenantOrigin) {
		err = ErrAugmentOfficialTenantNotAllowlisted
		s.appendAudit(AugmentOfficialSessionAuditEvent{
			RecordedAt:  s.now(),
			ActorUserID: actorUserID,
			UserID:      actorUserID,
			Source:      source,
			TenantHost:  hostFromOrigin(normalizedPayload.TenantOrigin),
			Result:      "bind_rejected",
			RequestID:   strings.TrimSpace(input.RequestID),
			ErrorClass:  infraerrors.Reason(err),
		})
		return nil, err
	}

	secretPayload := augmentOfficialEncryptedCredentialPayload{
		AccessToken:       normalizedPayload.AccessToken,
		RefreshToken:      normalizedPayload.RefreshToken,
		OfficialSessionID: normalizedPayload.OfficialSessionID,
	}
	secretData, err := json.Marshal(secretPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal augment official session credential payload: %w", err)
	}
	encryptedPayload, err := s.cipher.Encrypt(secretData)
	if err != nil {
		return nil, err
	}
	keyVersion, err := encryptedPayloadKeyID(encryptedPayload)
	if err != nil {
		return nil, err
	}

	fingerprint := buildAugmentOfficialFingerprint(source, normalizedPayload.TenantOrigin, normalizedPayload.AccessToken, normalizedPayload.RefreshToken, normalizedPayload.OfficialSessionID)
	now := s.now()
	storedView, err := s.store.UpsertActiveSession(ctx, AugmentOfficialSessionStoredSessionInput{
		UserID:                     actorUserID,
		Mode:                       mode,
		Source:                     source,
		TenantOrigin:               normalizedPayload.TenantOrigin,
		PortalOrigin:               normalizedPayload.PortalOrigin,
		Scopes:                     normalizedPayload.Scopes,
		ExpiresAt:                  ptrTimeValue(normalizedPayload.ExpiresAt),
		LastSuccessAt:              ptrTimeValue(now),
		Status:                     augmentOfficialSessionStatusActive,
		EncryptedCredentialPayload: encryptedPayload,
		CredentialSchemaVersion:    normalizedPayload.CredentialSchemaVersion,
		KeyVersion:                 keyVersion,
		Fingerprint:                fingerprint,
	})
	if err != nil {
		return nil, err
	}

	view := storedPublicViewToPublicView(storedView)
	s.appendAudit(AugmentOfficialSessionAuditEvent{
		RecordedAt:        now,
		ActorUserID:       actorUserID,
		UserID:            actorUserID,
		Source:            source,
		TenantHost:        hostFromOrigin(normalizedPayload.TenantOrigin),
		FingerprintPrefix: fingerprintPrefix(fingerprint),
		Result:            "bind_success",
		RequestID:         strings.TrimSpace(input.RequestID),
	})
	return view, nil
}

func (s *AugmentOfficialSessionService) GetOfficialSession(ctx context.Context, actorUserID int64) (*AugmentOfficialSessionPublicView, error) {
	if s == nil || s.store == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_OFFICIAL_SESSION_UNAVAILABLE", "augment official session service is unavailable")
	}
	stored, err := s.store.GetActiveSessionPublicView(ctx, actorUserID)
	if err != nil || stored == nil {
		return nil, err
	}
	return storedPublicViewToPublicView(stored), nil
}

func (s *AugmentOfficialSessionService) GetCredentialForRoute(ctx context.Context, actorUserID int64) (*AugmentOfficialSessionCredential, error) {
	if s == nil || s.store == nil || s.cipher == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_OFFICIAL_SESSION_UNAVAILABLE", "augment official session service is unavailable")
	}
	row, err := s.store.GetActiveSessionCredentialRow(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	if row == nil || row.Status != augmentOfficialSessionStatusActive || (row.ExpiresAt != nil && !row.ExpiresAt.After(s.now())) {
		return nil, ErrAugmentOfficialSessionInactive
	}
	plaintext, err := s.cipher.Decrypt(row.EncryptedCredentialPayload)
	if err != nil {
		return nil, err
	}
	var secretPayload augmentOfficialEncryptedCredentialPayload
	if err := json.Unmarshal(plaintext, &secretPayload); err != nil {
		return nil, fmt.Errorf("unmarshal augment official session credential payload: %w", err)
	}
	return &AugmentOfficialSessionCredential{
		UserID:                  row.UserID,
		Mode:                    row.Mode,
		Source:                  row.Source,
		TenantOrigin:            row.TenantOrigin,
		PortalOrigin:            cloneStringPtr(row.PortalOrigin),
		Scopes:                  cloneAugmentOfficialStringSlice(row.Scopes),
		ExpiresAt:               cloneTimePtr(row.ExpiresAt),
		LastRefreshAt:           cloneTimePtr(row.LastRefreshAt),
		LastSuccessAt:           cloneTimePtr(row.LastSuccessAt),
		LastErrorAt:             cloneTimePtr(row.LastErrorAt),
		LastErrorCode:           cloneStringPtr(row.LastErrorCode),
		Status:                  row.Status,
		AccessToken:             secretPayload.AccessToken,
		RefreshToken:            secretPayload.RefreshToken,
		OfficialSessionID:       secretPayload.OfficialSessionID,
		CredentialSchemaVersion: row.CredentialSchemaVersion,
		KeyVersion:              row.KeyVersion,
		Fingerprint:             row.Fingerprint,
		CreatedAt:               row.CreatedAt.UTC(),
		UpdatedAt:               row.UpdatedAt.UTC(),
		RevokedAt:               cloneTimePtr(row.RevokedAt),
	}, nil
}

func (s *AugmentOfficialSessionService) RevokeOfficialSession(ctx context.Context, actorUserID int64) (*AugmentOfficialSessionPublicView, error) {
	if s == nil || s.store == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_OFFICIAL_SESSION_UNAVAILABLE", "augment official session service is unavailable")
	}
	stored, err := s.store.RevokeActiveSession(ctx, actorUserID)
	if err != nil || stored == nil {
		return nil, err
	}
	view := storedPublicViewToPublicView(stored)
	s.appendAudit(AugmentOfficialSessionAuditEvent{
		RecordedAt:        s.now(),
		ActorUserID:       actorUserID,
		UserID:            actorUserID,
		Source:            stored.Source,
		TenantHost:        hostFromOrigin(stored.TenantOrigin),
		FingerprintPrefix: fingerprintPrefix(stored.Fingerprint),
		Result:            "revoke_success",
	})
	return view, nil
}

func (s *AugmentOfficialSessionService) AuditEvents() []AugmentOfficialSessionAuditEvent {
	if s == nil {
		return nil
	}
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	out := make([]AugmentOfficialSessionAuditEvent, len(s.audit))
	copy(out, s.audit)
	return out
}

func (s *AugmentOfficialSessionService) appendAudit(event AugmentOfficialSessionAuditEvent) {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	s.audit = append(s.audit, event)
}

func (s *AugmentOfficialSessionService) signBindToken(claims augmentOfficialBindTokenClaims) (string, error) {
	if len(s.bindTokenSecret) == 0 {
		return "", infraerrors.InternalServer("AUGMENT_OFFICIAL_BIND_TOKEN_CONFIG_INVALID", "augment official bind token secret is not configured")
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal augment official bind token: %w", err)
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.bindTokenSecret)
	_, _ = mac.Write([]byte(encodedPayload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encodedPayload + "." + signature, nil
}

func (s *AugmentOfficialSessionService) parseBindToken(token string) (*augmentOfficialBindTokenClaims, error) {
	if len(s.bindTokenSecret) == 0 {
		return nil, ErrAugmentOfficialBindTokenInvalid
	}
	payload, signature, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || payload == "" || signature == "" {
		return nil, ErrAugmentOfficialBindTokenInvalid
	}
	mac := hmac.New(sha256.New, s.bindTokenSecret)
	_, _ = mac.Write([]byte(payload))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return nil, ErrAugmentOfficialBindTokenInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, ErrAugmentOfficialBindTokenInvalid
	}
	var claims augmentOfficialBindTokenClaims
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, ErrAugmentOfficialBindTokenInvalid
	}
	if claims.UserID <= 0 || claims.BindIntentID == "" || claims.ExpiresAt <= 0 {
		return nil, ErrAugmentOfficialBindTokenInvalid
	}
	if time.Unix(claims.ExpiresAt, 0).UTC().Before(s.now()) {
		return nil, ErrAugmentOfficialBindTokenInvalid
	}
	return &claims, nil
}

func storedPublicViewToPublicView(stored *AugmentOfficialSessionStoredPublicView) *AugmentOfficialSessionPublicView {
	if stored == nil {
		return nil
	}
	return &AugmentOfficialSessionPublicView{
		UserID:            stored.UserID,
		Mode:              stored.Mode,
		Source:            stored.Source,
		TenantOrigin:      stored.TenantOrigin,
		PortalOrigin:      cloneStringPtr(stored.PortalOrigin),
		Scopes:            cloneAugmentOfficialStringSlice(stored.Scopes),
		ExpiresAt:         cloneTimePtr(stored.ExpiresAt),
		LastRefreshAt:     cloneTimePtr(stored.LastRefreshAt),
		LastSuccessAt:     cloneTimePtr(stored.LastSuccessAt),
		LastErrorAt:       cloneTimePtr(stored.LastErrorAt),
		LastErrorCode:     cloneStringPtr(stored.LastErrorCode),
		Status:            stored.Status,
		FingerprintPrefix: fingerprintPrefix(stored.Fingerprint),
		CreatedAt:         stored.CreatedAt.UTC(),
		UpdatedAt:         stored.UpdatedAt.UTC(),
		RevokedAt:         cloneTimePtr(stored.RevokedAt),
	}
}

func normalizeAugmentOfficialSessionMode(mode string) (string, error) {
	if strings.TrimSpace(mode) != augmentOfficialSessionModeOfficialPassthrough {
		return "", ErrAugmentOfficialTenantSessionMismatch
	}
	return augmentOfficialSessionModeOfficialPassthrough, nil
}

func normalizeTenantAllowlist(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, ErrAugmentOfficialInvalidTenantURL
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized, err := NormalizeAugmentOfficialOrigin(value)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out, nil
}

func normalizeAugmentOfficialCredentialPayload(raw map[string]any, now time.Time) (*augmentOfficialNormalizedCredentialPayload, error) {
	if len(raw) == 0 {
		return nil, ErrAugmentOfficialCredentialSchemaInvalid
	}
	allowedKeys := map[string]struct{}{
		"tenant_url":                {},
		"tenant_origin":             {},
		"portal_url":                {},
		"portal_origin":             {},
		"access_token":              {},
		"refresh_token":             {},
		"expires_at":                {},
		"scopes":                    {},
		"official_session_id":       {},
		"credential_schema_version": {},
	}
	for key := range raw {
		if _, ok := allowedKeys[key]; !ok {
			return nil, ErrAugmentOfficialCredentialSchemaInvalid
		}
	}

	tenantRaw := firstNonEmptyAugmentOfficialString(raw["tenant_origin"], raw["tenant_url"])
	tenantOrigin, err := NormalizeAugmentOfficialOrigin(tenantRaw)
	if err != nil {
		return nil, err
	}

	portalRaw := firstNonEmptyAugmentOfficialString(raw["portal_origin"], raw["portal_url"])
	var portalOrigin *string
	if portalRaw != "" {
		normalizedPortal, err := NormalizeAugmentOfficialOrigin(portalRaw)
		if err != nil {
			return nil, err
		}
		portalOrigin = &normalizedPortal
	}

	accessToken := augmentOfficialStringValue(raw["access_token"])
	if accessToken == "" {
		return nil, ErrAugmentOfficialCredentialSchemaInvalid
	}
	refreshToken := augmentOfficialStringValue(raw["refresh_token"])

	expiresAtRaw := augmentOfficialStringValue(raw["expires_at"])
	expiresAt, err := time.Parse(time.RFC3339, expiresAtRaw)
	if err != nil {
		return nil, ErrAugmentOfficialCredentialSchemaInvalid
	}
	expiresAt = expiresAt.UTC()
	if !expiresAt.After(now.UTC()) {
		return nil, ErrAugmentOfficialCredentialExpired
	}

	scopes, err := normalizeStringList(raw["scopes"])
	if err != nil {
		return nil, ErrAugmentOfficialCredentialSchemaInvalid
	}
	if err := ValidateAugmentOfficialScopes(scopes); err != nil {
		return nil, err
	}

	schemaVersion, err := intValue(raw["credential_schema_version"], 1)
	if err != nil || schemaVersion < 1 {
		return nil, ErrAugmentOfficialCredentialSchemaInvalid
	}

	return &augmentOfficialNormalizedCredentialPayload{
		TenantOrigin:            tenantOrigin,
		PortalOrigin:            portalOrigin,
		AccessToken:             accessToken,
		RefreshToken:            refreshToken,
		ExpiresAt:               expiresAt,
		Scopes:                  scopes,
		OfficialSessionID:       augmentOfficialStringValue(raw["official_session_id"]),
		CredentialSchemaVersion: schemaVersion,
	}, nil
}

func encryptedPayloadKeyID(ciphertext []byte) (string, error) {
	var envelope augmentSessionVaultKeyEnvelope
	if err := json.Unmarshal(ciphertext, &envelope); err != nil {
		return "", fmt.Errorf("unmarshal augment session vault key envelope: %w", err)
	}
	keyID := strings.TrimSpace(envelope.KeyID)
	if keyID == "" {
		return "", fmt.Errorf("augment session vault key id is missing")
	}
	return keyID, nil
}

func buildAugmentOfficialFingerprint(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func fingerprintPrefix(fingerprint string) string {
	fingerprint = strings.TrimSpace(fingerprint)
	if len(fingerprint) <= augmentOfficialFingerprintPrefixLength {
		return fingerprint
	}
	return fingerprint[:augmentOfficialFingerprintPrefixLength]
}

func hostFromOrigin(origin string) string {
	normalized, err := NormalizeAugmentOfficialOrigin(origin)
	if err != nil {
		return ""
	}
	trimmed := strings.TrimPrefix(normalized, "https://")
	return strings.TrimSpace(trimmed)
}

func hostFromRawPayload(payload map[string]any) string {
	origin := firstNonEmptyAugmentOfficialString(payload["tenant_origin"], payload["tenant_url"])
	if origin == "" {
		return ""
	}
	return hostFromOrigin(origin)
}

func originAllowed(allowlist []string, origin string) bool {
	for _, allowed := range allowlist {
		if allowed == origin {
			return true
		}
	}
	return false
}

func firstNonEmptyAugmentOfficialString(values ...any) string {
	for _, value := range values {
		if text := augmentOfficialStringValue(value); text != "" {
			return text
		}
	}
	return ""
}

func augmentOfficialStringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func normalizeStringList(value any) ([]string, error) {
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, entry := range typed {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				return nil, fmt.Errorf("empty string list entry")
			}
			out = append(out, entry)
		}
		return out, nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, entry := range typed {
			text, ok := entry.(string)
			if !ok {
				return nil, fmt.Errorf("non-string list entry")
			}
			text = strings.TrimSpace(text)
			if text == "" {
				return nil, fmt.Errorf("empty string list entry")
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported string list type")
	}
}

func intValue(value any, fallback int) (int, error) {
	switch typed := value.(type) {
	case nil:
		return fallback, nil
	case int:
		return typed, nil
	case int32:
		return int(typed), nil
	case int64:
		return int(typed), nil
	case float64:
		return int(typed), nil
	default:
		return 0, fmt.Errorf("unsupported int type")
	}
}

func ptrTimeValue(t time.Time) *time.Time {
	normalized := t.UTC()
	return &normalized
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneAugmentOfficialStringSlice(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func sha256HexString(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func augmentOfficialRandomHexToken(byteLength int) (string, error) {
	if byteLength <= 0 {
		byteLength = 16
	}
	buf := make([]byte, byteLength)
	if _, err := cryptorand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
